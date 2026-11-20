package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"slices"
	"sync"
	"unicode/utf8"
)

// replays is Amendment 8's count of replays per prompt and variant.
const replays = 30

// seedDefault names this pass. Amendment 8's ledger used seeds 1 to 3 for its passes;
// this is the fourth pass of the series, in its own file.
const seedDefault = 4

// scheduleStream is the fixed second PCG word for the shuffle; the seed is the first.
const scheduleStream = 0x43414e4f4e4c4942

// jobKey names a prompt and variant; job adds the replay index.
type jobKey struct {
	Prompt  int
	Variant string
}

type job struct {
	Prompt  int    `json:"prompt"`
	Variant string `json:"variant"`
	I       int    `json:"i"`
}

func (j job) key() jobKey { return jobKey{j.Prompt, j.Variant} }

// schedule is every prompt by variant by replay, shuffled together with the seed.
func schedule(prompts []int, seed uint64) []job {
	var jobs []job
	for _, p := range prompts {
		for _, v := range variants {
			for i := range replays {
				jobs = append(jobs, job{p, v, i})
			}
		}
	}
	rng := rand.New(rand.NewPCG(seed, scheduleStream))
	rng.Shuffle(len(jobs), func(a, b int) { jobs[a], jobs[b] = jobs[b], jobs[a] })
	return jobs
}

// Row is one ledger line, in the format of Amendment 8's replay.jsonl. A row with an error
// has no Outcome. Fields after Outcome are new here.
type Row struct {
	Prompt  int     `json:"prompt"`
	Group   string  `json:"group"`
	Session string  `json:"session"`
	Known   bool    `json:"known"`
	Variant string  `json:"variant"`
	I       int     `json:"i"`
	Seed    int     `json:"seed"`
	Error   *string `json:"error"`
	TS      float64 `json:"ts"`
	*Outcome
	HTTP     int    `json:"http"`
	BodySHA  string `json:"body_sha256,omitempty"`
	Attempts int    `json:"attempts"`
	// Rerun is 1 for the one rerun an error row gets.
	Rerun  int    `json:"rerun"`
	Detail string `json:"detail,omitempty"`
	// HeldUSD is charged to the hard stop at the worst case because the billed cost is
	// unknown: a dropped connection, or a 200 without usage.
	HeldUSD float64 `json:"held_usd,omitempty"`
}

// Outcome is a completed exchange: HTTP 200 with a usage block.
type Outcome struct {
	Status    string   `json:"status"`
	Reason    *string  `json:"reason"`
	Input     int      `json:"input"`
	Cached    int      `json:"cached"`
	Output    int      `json:"output"`
	Reasoning int      `json:"reasoning"`
	USD       float64  `json:"usd"`
	Tools     []string `json:"tools"`
	ArgsLen   int      `json:"args_len"`
	FirstKey  *string  `json:"first_key"`
	Head      string   `json:"head,omitempty"`
	Tail      string   `json:"tail,omitempty"`
	Write     int      `json:"write,omitempty"`
}

// runaway is Amendment 8's classification: a reply stopped at the output cap.
func (r Row) runaway() bool {
	return r.Outcome != nil && r.Status == "incomplete" && r.Reason != nil && *r.Reason == "max_output_tokens"
}

func (r Row) valid() bool { return r.Error == nil && r.Outcome != nil }

// charged is what the row counts against the hard stop.
func (r Row) charged() float64 {
	c := r.HeldUSD
	if r.Outcome != nil {
		c += r.USD
	}
	return c
}

// firstKeyPattern reads the first key as written, so a truncated argument string still
// has one.
var firstKeyPattern = regexp.MustCompile(`^\s*\{\s*"((?:[^"\\]|\\.)*)"`)

// headTail is how much of a runaway's arguments the row keeps at each end, in characters.
const headTail = 160

// outcomeOf reads a 200 response as Amendment 8's ledger did: tools are the names of the
// output's calls, args_len the total length of their arguments in characters, first_key
// the first key any of them writes, and a cut reply keeps the ends of its longest call.
func outcomeOf(body []byte) (*Outcome, error) {
	var parsed struct {
		Status     string `json:"status"`
		Incomplete *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("response is not JSON")
	}
	u, ok := parseUsage(body)
	if !ok {
		return nil, fmt.Errorf("response has no usage")
	}
	o := &Outcome{
		Status: parsed.Status, Input: u.input, Cached: u.cached, Write: u.write, Output: u.output,
		Reasoning: u.reasoning, USD: luna.cost(u.input, u.cached, u.write, u.output), Tools: []string{},
	}
	if parsed.Incomplete != nil && parsed.Incomplete.Reason != "" {
		reason := parsed.Incomplete.Reason
		o.Reason = &reason
	}
	longest, text := "", ""
	for _, it := range parsed.Output {
		switch it.Type {
		case "function_call":
			o.Tools = append(o.Tools, it.Name)
			o.ArgsLen += utf8.RuneCountInString(it.Arguments)
			if o.FirstKey == nil {
				if m := firstKeyPattern.FindStringSubmatch(it.Arguments); m != nil {
					k := m[1]
					o.FirstKey = &k
				}
			}
			if utf8.RuneCountInString(it.Arguments) > utf8.RuneCountInString(longest) {
				longest = it.Arguments
			}
		case "message":
			for _, c := range it.Content {
				text += c.Text
			}
		}
	}
	if o.Status == "incomplete" {
		src := longest
		if src == "" {
			src = text
		}
		r := []rune(src)
		o.Head = string(r[:min(headTail, len(r))])
		o.Tail = string(r[max(0, len(r)-headTail):])
	}
	return o, nil
}

// readLedger reads every row; a missing file is an empty ledger.
func readLedger(path string) ([]Row, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []Row
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Row
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, n, err)
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}

// ledgerWriter appends rows, one line each, synced after every write so an interrupted
// run loses at most the row being written.
type ledgerWriter struct {
	mu sync.Mutex
	f  *os.File
}

func openLedger(path string) (*ledgerWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &ledgerWriter{f: f}, nil
}

func (w *ledgerWriter) add(r Row) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.f.Write(append(b, '\n')); err != nil {
		return err
	}
	return w.f.Sync()
}

func (w *ledgerWriter) close() error { return w.f.Close() }

// errCanceled marks a row whose request was cut by an interrupt. It holds the charge, and
// the job stays pending.
const errCanceled = "canceled"

func (r Row) canceled() bool { return r.Error != nil && *r.Error == errCanceled }

// pending returns the jobs still to run, in schedule order. Without rerun that is every job
// with no row. With rerun it is every job whose only row is an error, run once more.
// Interrupted rows do not count as run.
func pending(jobs []job, rows []Row, rerun bool) []job {
	type state struct{ first, rerun, firstErr bool }
	seen := map[job]*state{}
	for _, r := range rows {
		if r.canceled() {
			continue
		}
		j := job{r.Prompt, r.Variant, r.I}
		s := seen[j]
		if s == nil {
			s = &state{}
			seen[j] = s
		}
		if r.Rerun > 0 {
			s.rerun = true
		} else {
			s.first, s.firstErr = true, r.Error != nil
		}
	}
	var out []job
	for _, j := range jobs {
		s := seen[j]
		switch {
		case !rerun && (s == nil || !s.first):
			out = append(out, j)
		case rerun && s != nil && s.first && s.firstErr && !s.rerun:
			out = append(out, j)
		}
	}
	return out
}

// final picks the row that stands for each job: the rerun when there is one.
func final(rows []Row) map[job]Row {
	out := map[job]Row{}
	for _, r := range rows {
		if r.canceled() {
			continue
		}
		j := job{r.Prompt, r.Variant, r.I}
		if prev, ok := out[j]; ok && prev.Rerun > r.Rerun {
			continue
		}
		out[j] = r
	}
	return out
}

func sortedKeys[K comparable, V any](m map[K]V, less func(a, b K) int) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, less)
	return keys
}
