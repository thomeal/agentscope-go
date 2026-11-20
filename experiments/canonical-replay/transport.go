package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// maxOutputTokens is Amendment 8's cap. A reply stopped there is a runaway.
const maxOutputTokens = 8000

// prices are gpt-6-luna's per-million prices for the default 30 minute tier, as stage 0's
// lab.Luna. Cost treats input as plain input minus cached and written tokens.
type prices struct{ Input, Cached, Write, Output float64 }

var luna = prices{Input: 0.10, Cached: 0.01, Write: 0.125, Output: 0.50}

func (p prices) cost(input, cached, write, output int) float64 {
	plain := max(input-cached-write, 0)
	return (float64(plain)*p.Input + float64(cached)*p.Cached + float64(write)*p.Write + float64(output)*p.Output) / 1e6
}

// worst is the most one request can cost: every input token uncached (estimated at two
// bytes of request body per token, well above the recorded bodies' ratio) and the whole
// output cap.
func (p prices) worst(body []byte) float64 {
	return (float64(len(body)/2)*p.Input + float64(maxOutputTokens)*p.Output) / 1e6
}

// errBudget refuses a request the hard stop does not leave room for.
var errBudget = errors.New("hard stop: the spend cap leaves no room for this request")

// budget is the hard stop. Each request reserves its worst case before it is sent and
// settles to its billed cost afterwards, so concurrent requests cannot pass the cap
// together.
type budget struct {
	mu    sync.Mutex
	limit float64
	spent float64
	held  float64
}

func (b *budget) reserve(x float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spent+b.held+x > b.limit {
		return false
	}
	b.held += x
	return true
}

func (b *budget) settle(reserved, charged float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.held -= reserved
	b.spent += charged
}

func (b *budget) total() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spent
}

// capture is what the cloud transport saw for one job. The job passes it in the request
// context.
type capture struct {
	key      jobKey // prompt and variant, for the expected body hash
	keepBody bool

	mu       sync.Mutex
	requests int
	body     []byte // outbound bytes, when keepBody
	sha      string
	http     int
	attempts int
	respBody []byte
	err      string // transport-level failure, redacted
	charged  float64
	held     bool // charged at the reservation, because the billed cost is unknown
}

type captureKey struct{}

func withCapture(ctx context.Context, c *capture) context.Context {
	return context.WithValue(ctx, captureKey{}, c)
}

// cloudTransport shapes every request the same way in both variants and sends it. The
// shaping is stage 0's prepareResponses (store false, include encrypted reasoning, drop
// function_call item ids that do not start with "fc") plus max_output_tokens. The body is
// re-encoded from a decoded map, as stage 0's transport did, so keys are sorted at every
// level, tool parameters included.
type cloudTransport struct {
	base   http.RoundTripper
	budget *budget
	// expect maps a prompt and variant to the outbound body hash the offline check
	// produced. A request whose hash differs is refused before it is sent.
	expect      map[jobKey]string
	maxAttempts int
	sleep       func(context.Context, time.Duration) error
}

func (t *cloudTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()
	c, _ := ctx.Value(captureKey{}).(*capture)
	if c == nil {
		return nil, errors.New("cloud transport: request outside a replay job")
	}
	raw, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}
	out, err := shape(raw)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(out)
	c.mu.Lock()
	c.requests++
	c.sha = hex.EncodeToString(sum[:])
	if c.keepBody {
		c.body = out
	}
	c.mu.Unlock()
	if t.expect != nil {
		if want := t.expect[c.key]; want != c.sha {
			c.setErr("outbound body differs from the checked body")
			return nil, errors.New("cloud transport: outbound body differs from the checked body; not sent")
		}
	}
	reserve := luna.worst(out)
	if !t.budget.reserve(reserve) {
		c.setErr("budget")
		return nil, errBudget
	}
	ex, sendErr := t.send(ctx, r, out)
	// An attempt whose connection failed may still have been billed, so each one is
	// charged at the worst case, as is a 200 without usage.
	charged, held := float64(ex.lost)*reserve, ex.lost > 0
	if ex.status == http.StatusOK && sendErr == nil {
		if cost, ok := billed(ex.body); ok {
			charged += cost
		} else {
			charged, held = charged+reserve, true
		}
	}
	t.budget.settle(reserve, charged)
	c.mu.Lock()
	c.http, c.attempts, c.respBody, c.charged, c.held = ex.status, ex.tries, ex.body, charged, held
	c.mu.Unlock()
	if sendErr != nil {
		c.setErr("network: " + redact(sendErr.Error()))
		return nil, sendErr
	}
	return &http.Response{
		Status:        strconv.Itoa(ex.status) + " " + http.StatusText(ex.status),
		StatusCode:    ex.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        ex.header,
		Body:          io.NopCloser(bytes.NewReader(ex.body)),
		ContentLength: int64(len(ex.body)),
		Request:       r,
	}, nil
}

func (c *capture) setErr(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = s
}

// shape applies the shaping both variants share and returns the outbound bytes.
func shape(raw []byte) ([]byte, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("cloud transport: request is not JSON: %w", err)
	}
	prepareResponses(body)
	body["max_output_tokens"] = maxOutputTokens
	return json.Marshal(body)
}

type exchange struct {
	status int
	header http.Header
	body   []byte
	tries  int
	lost   int // attempts that ended in a connection error
}

// send posts out, retrying 429, 5xx and dropped connections as stage 0's transport did:
// up to nine attempts, backoff from 500 ms doubling to 30 s, Retry-After honored.
func (t *cloudTransport) send(ctx context.Context, r *http.Request, out []byte) (exchange, error) {
	var ex exchange
	backoff := 500 * time.Millisecond
	attempts := t.maxAttempts
	if attempts <= 0 {
		attempts = 9
	}
	sleep := t.sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	for try := 1; ; try++ {
		ex.tries = try
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL.String(), bytes.NewReader(out))
		if err != nil {
			return ex, err
		}
		req.Header = r.Header.Clone()
		req.ContentLength = int64(len(out))
		resp, err := t.base.RoundTrip(req)
		if err == nil {
			ex.body, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			ex.status, ex.header = resp.StatusCode, resp.Header
		}
		if err != nil {
			ex.status, ex.header, ex.body = 0, nil, nil
			ex.lost++
		}
		retry := err != nil || ex.status == http.StatusTooManyRequests || ex.status >= 500
		if !retry {
			return ex, nil
		}
		if ctx.Err() != nil {
			return ex, ctx.Err()
		}
		if try >= attempts {
			if err == nil {
				return ex, nil
			}
			return ex, err
		}
		delay := backoff
		if err == nil {
			if s, perr := strconv.Atoi(ex.header.Get("Retry-After")); perr == nil && s > 0 {
				delay = time.Duration(min(s, 30)) * time.Second
			}
		}
		backoff = min(2*backoff, 30*time.Second)
		if err := sleep(ctx, delay); err != nil {
			return ex, err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// billed prices a 200 response from its usage block.
func billed(body []byte) (float64, bool) {
	u, ok := parseUsage(body)
	if !ok {
		return 0, false
	}
	return luna.cost(u.input, u.cached, u.write, u.output), true
}

type usage struct{ input, cached, write, output, reasoning int }

func parseUsage(body []byte) (usage, bool) {
	var parsed struct {
		Usage *struct {
			Input   int `json:"input_tokens"`
			Output  int `json:"output_tokens"`
			Details struct {
				Cached int `json:"cached_tokens"`
				Write  int `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
			OutDetails struct {
				Reasoning int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &parsed) != nil || parsed.Usage == nil {
		return usage{}, false
	}
	u := parsed.Usage
	return usage{input: u.Input, cached: u.Details.Cached, write: u.Details.Write, output: u.Output, reasoning: u.OutDetails.Reasoning}, true
}

var keyPattern = regexp.MustCompile(`sk-[A-Za-z0-9_\-]+`)

// redact removes anything shaped like a key and bounds the length, as stage 0's lab did.
func redact(text string) string {
	text = keyPattern.ReplaceAllString(text, "sk-***")
	if len(text) > 400 {
		text = text[:400]
	}
	return text
}
