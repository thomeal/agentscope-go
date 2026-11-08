// Package lab is the transport layer of the stage 0 experiment: it injects
// provider fields, records every model call in a ledger, bounds concurrency on
// each side and performs forks at the HTTP boundary. It is experiment code and
// uses only the public agent and model APIs.
package lab

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Roles recorded in the ledger. RoleMain is the only one that moves a snapshot.
const (
	RoleMain     = "main"
	RoleCompress = "compress"
	RoleProbe    = "probe"
	RoleBrief    = "brief"
	RoleForkA    = "fork_a"
	RoleForkB    = "fork_b"
	RoleMerged   = "fork_merged"
	RoleSmall    = "small_summary"
	RoleShorten  = "shorten"
)

// Sides of the experiment.
const (
	SideCloud = "cloud"
	SideLocal = "local"
)

// ErrBudget is returned once the ledger's hard stop has been crossed.
var ErrBudget = errors.New("lab: cloud budget hard stop reached")

// Prices are USD per million tokens.
type Prices struct{ Input, Cached, Write, Output float64 }

// Luna holds the published gpt-6-luna prices for the default 30 minute tier.
var Luna = Prices{Input: 0.10, Cached: 0.01, Write: 0.125, Output: 0.50}

// Cost prices one call. Plain input excludes cached and written tokens.
func (p Prices) Cost(input, cached, write, output int) float64 {
	plain := max(input-cached-write, 0)
	return (float64(plain)*p.Input + float64(cached)*p.Cached + float64(write)*p.Write + float64(output)*p.Output) / 1e6
}

// Row is one model call.
type Row struct {
	TS           time.Time `json:"ts"`
	Run          string    `json:"run"`
	Arm          string    `json:"arm"`
	Session      string    `json:"session"`
	Side         string    `json:"side"`
	Role         string    `json:"role"`
	Model        string    `json:"model"`
	HTTP         int       `json:"http"`
	Status       string    `json:"status,omitempty"`
	Incomplete   string    `json:"incomplete,omitempty"`
	Input        int       `json:"input"`
	Cached       int       `json:"cached"`
	Write        int       `json:"write"`
	Output       int       `json:"output"`
	Reasoning    int       `json:"reasoning"`
	UsageMissing bool      `json:"usage_missing,omitempty"`
	WaitMS       int64     `json:"wait_ms"`
	LatencyMS    int64     `json:"latency_ms"`
	CostUSD      float64   `json:"cost_usd"`
	Attempt      int       `json:"attempt"`
	Overflow     bool      `json:"overflow,omitempty"`
	CanaryHits   int       `json:"canary_hits,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// Ledger appends rows to a JSONL file and tracks cloud spend. It stays usable
// with an empty path, which keeps tests free of files.
type Ledger struct {
	mu    sync.Mutex
	f     *os.File
	w     *bufio.Writer
	rows  []Row
	spent float64
	stop  float64
	run   string
}

// NewLedger opens path for append. stopUSD <= 0 disables the hard stop.
func NewLedger(run, path string, stopUSD float64) (*Ledger, error) {
	l := &Ledger{run: run, stop: stopUSD}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("lab: open ledger: %w", err)
		}
		l.f, l.w = f, bufio.NewWriter(f)
	}
	return l, nil
}

// SetSpent seeds cloud spend from earlier runs, so the hard stop stays global.
func (l *Ledger) SetSpent(usd float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.spent = usd
}

// Add records a row and flushes it, so a crash cannot hide spend.
func (l *Ledger) Add(r Row) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r.Run = l.run
	if r.TS.IsZero() {
		r.TS = time.Now().UTC()
	}
	l.rows = append(l.rows, r)
	if r.Side == SideCloud {
		l.spent += r.CostUSD
	}
	if l.w != nil {
		b, err := json.Marshal(r)
		if err == nil {
			_, _ = l.w.Write(append(b, '\n'))
			_ = l.w.Flush()
		}
	}
}

// Spent returns cumulative cloud spend.
func (l *Ledger) Spent() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spent
}

// OverBudget reports whether the hard stop has been crossed.
func (l *Ledger) OverBudget() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stop > 0 && l.spent >= l.stop
}

// Rows returns a copy of the rows recorded by this process.
func (l *Ledger) Rows() []Row {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Row(nil), l.rows...)
}

// Scan visits rows newest first until visit returns false. It copies nothing, because
// a long run asks often and its ledger holds many rows. visit runs under the ledger's
// lock and must not call the ledger.
func (l *Ledger) Scan(visit func(*Row) bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.rows) - 1; i >= 0; i-- {
		if !visit(&l.rows[i]) {
			return
		}
	}
}

// Close flushes and closes the file.
func (l *Ledger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return nil
	}
	if err := l.w.Flush(); err != nil {
		return err
	}
	return l.f.Close()
}

// Slots bounds in-flight requests on one side. A nil Slots never blocks.
type Slots struct{ ch chan struct{} }

// NewSlots returns a limiter with n slots; n <= 0 means unlimited.
func NewSlots(n int) *Slots {
	if n <= 0 {
		return nil
	}
	return &Slots{ch: make(chan struct{}, n)}
}

func (s *Slots) acquire(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, nil
	}
	start := time.Now()
	select {
	case s.ch <- struct{}{}:
		return time.Since(start).Milliseconds(), nil
	case <-ctx.Done():
		return time.Since(start).Milliseconds(), ctx.Err()
	}
}

func (s *Slots) release() {
	if s != nil {
		<-s.ch
	}
}

type ctxKey int

const (
	roleKey ctxKey = iota
	limitKey
	effortKey
	schemaKey
)

// WithRole tags calls made under ctx. Untagged calls are RoleMain.
func WithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, roleKey, role)
}

// RoleFrom returns the role WithRole put on ctx, or "" when none was set.
func RoleFrom(ctx context.Context) string {
	role, _ := ctx.Value(roleKey).(string)
	return role
}

// WithLimit overrides the output limit of calls made under ctx.
func WithLimit(ctx context.Context, limit int) context.Context {
	return context.WithValue(ctx, limitKey, limit)
}

// WithEffort sets the reasoning effort of cloud calls made under ctx by appending a
// configuration_update item after the history, so the cached prefix stays intact. The
// local transport ignores it.
func WithEffort(ctx context.Context, effort string) context.Context {
	return context.WithValue(ctx, effortKey, effort)
}

// WithAnswerSchema forces local calls made under ctx to produce JSON matching schema,
// with no tool calls. The cloud transport ignores it: changing the cloud's text format
// would change a request whose prefix is cached, and its replies have not needed it.
func WithAnswerSchema(ctx context.Context, schema map[string]any) context.Context {
	return context.WithValue(ctx, schemaKey, schema)
}

func roleOf(ctx context.Context, body map[string]any) string {
	role, _ := ctx.Value(roleKey).(string)
	if forcedStructured(body) {
		// The forced schema call is the existing compression path. Probe and
		// brief clones can compress too; that is still compression spend.
		return RoleCompress
	}
	if role == "" {
		return RoleMain
	}
	return role
}

func forcedStructured(body map[string]any) bool {
	tc, ok := body["tool_choice"].(map[string]any)
	if !ok {
		return false
	}
	if name, _ := tc["name"].(string); name != "" {
		return true
	}
	fn, _ := tc["function"].(map[string]any)
	name, _ := fn["name"].(string)
	return name != ""
}

// Options configures both transports.
type Options struct {
	Ledger  *Ledger
	Arm     string
	Session string
	BaseURL string
	APIKey  string
	Model   string
	Slots   *Slots
	Prices  Prices
	// Canaries are scanned for in every outbound cloud body. Ignored on the
	// local side, whose traffic stays on this machine.
	Canaries []string
	// CanonicalReplay rewrites local's call arguments before they reach the cloud. See
	// canonicalReplay.
	CanonicalReplay bool
	// MaxAttempts bounds transport retries on 429, 5xx and a dropped connection. Default 9.
	MaxAttempts int
	// Next sends the request; nil means http.DefaultTransport.
	Next http.RoundTripper
	// Dump, when set, sees every request and response body. It is for diagnosis, and
	// nothing in a scored run depends on it.
	Dump func(arm, session, side, role string, req, resp []byte)
}

func (o *Options) next() http.RoundTripper {
	if o.Next != nil {
		return o.Next
	}
	return http.DefaultTransport
}

func (o *Options) attempts() int {
	if o.MaxAttempts > 0 {
		return o.MaxAttempts
	}
	return defaultAttempts
}

const (
	// defaultAttempts and backoffCap ride out about 90 seconds of retries. A session that
	// errors stays in the denominator, so a short network outage would otherwise decide a row.
	defaultAttempts = 9
	backoffCap      = 30 * time.Second
)

type exchange struct {
	status int
	header http.Header
	body   []byte
	wait   int64
	ms     int64
	tries  int
}

// post sends body with retries. It holds a slot only while a request is in
// flight, so a backoff never occupies capacity.
func post(ctx context.Context, o *Options, url string, hdr http.Header, body []byte) (exchange, error) {
	var ex exchange
	backoff := 500 * time.Millisecond
	for try := 1; ; try++ {
		ex.tries = try
		wait, err := o.Slots.acquire(ctx)
		ex.wait += wait
		if err != nil {
			return ex, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			o.Slots.release()
			return ex, err
		}
		for k, vs := range hdr {
			req.Header[k] = append([]string(nil), vs...)
		}
		req.ContentLength = int64(len(body))
		start := time.Now()
		resp, err := o.next().RoundTrip(req)
		if err == nil {
			ex.body, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			ex.status, ex.header = resp.StatusCode, resp.Header
		}
		ex.ms = time.Since(start).Milliseconds()
		o.Slots.release()
		if err == nil && !retryable(ex.status) {
			return ex, nil
		}
		if try >= o.attempts() {
			return ex, err
		}
		delay := backoff
		if err == nil {
			if s, perr := strconv.Atoi(ex.header.Get("Retry-After")); perr == nil && s > 0 {
				delay = time.Duration(s) * time.Second
			}
		}
		backoff = nextBackoff(backoff)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ex, ctx.Err()
		}
	}
}

func nextBackoff(d time.Duration) time.Duration { return min(2*d, backoffCap) }

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func rebuilt(req *http.Request, ex exchange) *http.Response {
	return &http.Response{
		Status:        strconv.Itoa(ex.status) + " " + http.StatusText(ex.status),
		StatusCode:    ex.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        ex.header,
		Body:          io.NopCloser(bytes.NewReader(ex.body)),
		ContentLength: int64(len(ex.body)),
		Request:       req,
	}
}

var keyPattern = regexp.MustCompile(`sk-[A-Za-z0-9_\-]+`)

func redact(text string) string {
	text = keyPattern.ReplaceAllString(text, "sk-***")
	if len(text) > 400 {
		text = text[:400]
	}
	return text
}

func intOf(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func countCanaries(body []byte, canaries []string) int {
	n := 0
	for _, c := range canaries {
		if c != "" && bytes.Contains(body, []byte(c)) {
			n++
		}
	}
	return n
}

// EstimateTokens is a deliberately conservative estimate for budget decisions.
// Random codes tokenize badly, so it uses three characters per token.
func EstimateTokens(text string) int {
	n := len([]rune(text))
	if n == 0 {
		return 0
	}
	return n/3 + 1
}

// CutLines drops whole lines from the end of text to fit the estimated budget. It suits a
// text written in priority order, where the end is what matters least. A first line
// longer than the budget is cut where the budget falls.
func CutLines(text string, budget int) (string, bool) {
	if budget <= 0 || EstimateTokens(text) <= budget {
		return text, false
	}
	limit := 3*budget - 1
	runes := []rune(text)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	kept := string(runes)
	if i := strings.LastIndexByte(kept, '\n'); i > 0 {
		kept = kept[:i]
	}
	return strings.ToValidUTF8(strings.TrimRight(kept, " \n"), ""), true
}

// TrimTokens elides the middle of text to fit the estimated budget. The head
// and tail survive because summaries lead with goals and end with next steps.
func TrimTokens(text string, budget int) (string, bool) {
	if budget <= 0 || EstimateTokens(text) <= budget {
		return text, false
	}
	const marker = "\n[...elided to fit budget...]\n"
	runes := []rune(text)
	keep := (budget-1)*3 - len([]rune(marker))
	if keep < 2 {
		return "", true
	}
	head := keep * 2 / 3
	tail := keep - head
	return strings.ToValidUTF8(string(runes[:head])+marker+string(runes[len(runes)-tail:]), ""), true
}
