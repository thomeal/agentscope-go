// Command canonical-replay replays stage 0's Amendment 8 prompts through the library's
// ConnectivityAwareModel, with and without WithCanonicalReplay, against the OpenAI
// Responses API (prereg.md, Amendment 22).
//
//	canonical-replay check  -prompts P [-out DIR]     offline fidelity check; sends nothing
//	canonical-replay run    -prompts P -out DIR -env F live replay, resumable, hard stop
//	canonical-replay report -out DIR                  readings from the ledger
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	defaultPrompts = `H:/Claude/Projects/agentscope-go/experiments/stage0/runs/replay-runaway/prompts.jsonl`
	defaultOut     = `H:/Claude/Projects/agentscope-go/experiments/stage0/runs/replay-canonical-lib`
	// hardStop is Amendment 22's cap on the whole experiment, reruns included.
	hardStop = 2.0
	// wantModel is the cloud model the recorded requests name.
	wantModel = "gpt-6-luna"
)

func main() {
	// The connectivity wrapper warns on every blocked cloud try; those are by design here.
	logrus.SetLevel(logrus.ErrorLevel)
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: canonical-replay check|run|report [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "check":
		err = cmdCheck(os.Args[2:], os.Stdout)
	case "run":
		err = cmdRun(os.Args[2:], os.Stdout)
	case "report":
		err = cmdReport(os.Args[2:], os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "canonical-replay:", err)
		os.Exit(1)
	}
}

func cmdCheck(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	promptsPath := fs.String("prompts", defaultPrompts, "stage 0's prompts.jsonl (read only)")
	out := fs.String("out", "", "write the bodies and summary.json under OUT/check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prompts, err := loadPrompts(*promptsPath)
	if err != nil {
		return err
	}
	if err := checkSet(prompts); err != nil {
		return err
	}
	dir := ""
	if *out != "" {
		dir = filepath.Join(*out, "check")
	}
	_, ok, err := checkAll(prompts, dir, w)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("fidelity check failed")
	}
	fmt.Fprintf(w, "fidelity check passed on %d prompts\n", len(prompts))
	return nil
}

// checkSet requires Amendment 8's 11 first-write prompts, on the expected model.
func checkSet(prompts []recorded) error {
	if len(prompts) != 11 {
		return fmt.Errorf("found %d first-write prompts, want 11", len(prompts))
	}
	for i, p := range prompts {
		if p.ID != i {
			return fmt.Errorf("prompt ids are not 0 to 10")
		}
		c, err := convert(p.Req)
		if err != nil {
			return fmt.Errorf("prompt %d: %w", p.ID, err)
		}
		if c.model != wantModel {
			return fmt.Errorf("prompt %d names model %q, want %q", p.ID, c.model, wantModel)
		}
	}
	return nil
}

type runConfig struct {
	prompts []recorded
	outDir  string
	apiKey  string
	baseURL string
	limit   float64
	workers int
	seed    int
	rerun   bool
	base    http.RoundTripper
	timeout time.Duration
	sleep   func(context.Context, time.Duration) error
}

func cmdRun(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	promptsPath := fs.String("prompts", defaultPrompts, "stage 0's prompts.jsonl (read only)")
	out := fs.String("out", defaultOut, "output directory; the ledger is OUT/replay.jsonl")
	envPath := fs.String("env", "", "file holding an OPENAI_API_KEY= line (required; never printed)")
	model := fs.String("model", wantModel, "cloud model; must match the recorded requests")
	limit := fs.Float64("budget", hardStop, "hard stop in USD over the whole ledger, at most 2")
	workers := fs.Int("workers", 8, "concurrent replays")
	seed := fs.Int("seed", seedDefault, "shuffle seed")
	rerun := fs.Bool("rerun", false, "run each job whose only row is an error once more")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *model != wantModel {
		return fmt.Errorf("model %q is not the recorded %q", *model, wantModel)
	}
	if *limit <= 0 || *limit > hardStop {
		return fmt.Errorf("budget must be in (0, %.2f]", hardStop)
	}
	if *envPath == "" {
		return errors.New("-env is required")
	}
	prompts, err := loadPrompts(*promptsPath)
	if err != nil {
		return err
	}
	key, err := readKey(*envPath)
	if err != nil {
		return err
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	return run(context.Background(), runConfig{
		prompts: prompts, outDir: *out, apiKey: key, baseURL: "https://api.openai.com",
		limit: *limit, workers: *workers, seed: *seed, rerun: *rerun, base: base, timeout: 10 * time.Minute,
	}, w)
}

// readKey reads the OPENAI_API_KEY line. The key is never printed, logged or written.
func readKey(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot open the key file")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		line = strings.TrimPrefix(line, "export ")
		if v, ok := strings.CutPrefix(line, "OPENAI_API_KEY="); ok {
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if v == "" {
				return "", errors.New("OPENAI_API_KEY is empty in the key file")
			}
			return v, nil
		}
	}
	return "", errors.New("no OPENAI_API_KEY= line in the key file")
}

// run is the live replay. It checks fidelity offline first and refuses to start on any
// difference, then sends each pending job once, appending one row per job. The hard stop
// counts every row already in the ledger.
func run(parent context.Context, cfg runConfig, w io.Writer) error {
	if err := checkSet(cfg.prompts); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.outDir, 0o755); err != nil {
		return err
	}
	expect, ok, err := checkAll(cfg.prompts, filepath.Join(cfg.outDir, "check"), io.Discard)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("fidelity check failed; nothing sent (run `check` for the differences)")
	}
	byID := map[int]recorded{}
	var ids []int
	for _, p := range cfg.prompts {
		byID[p.ID] = p
		ids = append(ids, p.ID)
	}
	jobs := schedule(ids, uint64(cfg.seed))
	ledgerPath := filepath.Join(cfg.outDir, "replay.jsonl")
	rows, err := readLedger(ledgerPath)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Seed != cfg.seed {
			return fmt.Errorf("ledger holds seed %d rows; this run uses seed %d", r.Seed, cfg.seed)
		}
	}
	b := &budget{limit: cfg.limit}
	for _, r := range rows {
		b.spent += r.charged()
	}
	todo := pending(jobs, rows, cfg.rerun)
	fmt.Fprintf(w, "fidelity check passed; ledger has %d rows, $%.4f spent; %d jobs to run (rerun=%v), hard stop $%.2f\n",
		len(rows), b.spent, len(todo), cfg.rerun, cfg.limit)
	if len(todo) == 0 {
		return nil
	}
	lw, err := openLedger(ledgerPath)
	if err != nil {
		return err
	}
	defer lw.close()

	ctx, stop := signal.NotifyContext(parent, os.Interrupt)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ct := &cloudTransport{base: cfg.base, budget: b, expect: expect, sleep: cfg.sleep}
	rerun := 0
	if cfg.rerun {
		rerun = 1
	}

	if err := writeManifest(cfg, len(rows), b.spent, len(todo)); err != nil {
		return err
	}

	var (
		mu       sync.Mutex
		done     int
		runaways = map[string]int{}
		valid    = map[string]int{}
		stopWhy  string
		stopped  = make(chan struct{})
		stopOnce sync.Once
	)
	// halt stops dispatching. A hard stop lets requests in flight finish; any other reason
	// also cancels them.
	halt := func(why string, cancelInFlight bool) {
		mu.Lock()
		if stopWhy == "" {
			stopWhy = why
		}
		mu.Unlock()
		stopOnce.Do(func() { close(stopped) })
		if cancelInFlight {
			cancel()
		}
	}
	queue := make(chan job)
	var wg sync.WaitGroup
	for range max(1, cfg.workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				select {
				case <-stopped:
					continue
				default:
				}
				p := byID[j.Prompt]
				row, why := runJob(ctx, cfg, ct, p, j, rerun)
				if why != "" {
					halt(why, why != whyHardStop)
				}
				if row == nil {
					continue
				}
				if err := lw.add(*row); err != nil {
					halt("ledger write failed: "+err.Error(), true)
					continue
				}
				mu.Lock()
				done++
				if row.valid() {
					valid[j.Variant]++
					if row.runaway() {
						runaways[j.Variant]++
					}
				}
				if done%20 == 0 || done == len(todo) {
					fmt.Fprintf(w, "%d/%d rows  lib_off %d/%d runaways  lib_on %d/%d  spent $%.4f\n",
						done, len(todo), runaways[libOff], valid[libOff], runaways[libOn], valid[libOn], b.total())
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, j := range todo {
		select {
		case queue <- j:
		case <-stopped:
			break feed
		case <-ctx.Done():
			break feed
		}
	}
	close(queue)
	wg.Wait()
	fmt.Fprintf(w, "wrote %d rows; ledger spend $%.4f\n", done, b.total())
	if stopWhy != "" {
		return fmt.Errorf("stopped: %s", stopWhy)
	}
	if ctx.Err() != nil {
		return errors.New("interrupted; the same command resumes")
	}
	return nil
}

const whyHardStop = "hard stop reached"

// writeManifest appends one line per invocation: the build, the inputs and the state of
// the ledger when it started.
func writeManifest(cfg runConfig, rows int, spent float64, todo int) error {
	h := sha256.New()
	for _, p := range cfg.prompts {
		h.Write(p.Req)
	}
	m := map[string]any{
		"ts": float64(time.Now().UnixNano()) / 1e9, "seed": cfg.seed, "workers": cfg.workers,
		"limit_usd": cfg.limit, "rows_before": rows, "spent_before": spent, "todo": todo,
		"rerun": cfg.rerun, "model": wantModel, "max_output_tokens": maxOutputTokens,
		"prompts_req_sha256": hex.EncodeToString(h.Sum(nil)),
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		m["go"] = bi.GoVersion
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				m["revision"] = s.Value
			case "vcs.modified":
				m["modified"] = s.Value
			}
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(cfg.outDir, "manifest.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// runJob replays one job and returns its row, or nil when no row is written: the hard
// stop refused it, or the run was interrupted before anything was sent. A non-empty
// reason stops the run.
func runJob(ctx context.Context, cfg runConfig, ct *cloudTransport, p recorded, j job, rerun int) (*Row, string) {
	if ctx.Err() != nil {
		return nil, ""
	}
	c := &capture{key: j.key()}
	jctx, cancel := context.WithTimeout(withCapture(ctx, c), cfg.timeout)
	defer cancel()
	res := replayOne(jctx, p.Req, j.Variant, cfg.apiKey, cfg.baseURL, ct)

	row := &Row{
		Prompt: p.ID, Group: p.Group, Session: p.Session, Known: p.Known, Variant: j.Variant, I: j.I,
		Seed: cfg.seed, TS: float64(time.Now().UnixNano()) / 1e9, Rerun: rerun,
	}
	c.mu.Lock()
	requests, cerr := c.requests, c.err
	row.HTTP, row.BodySHA, row.Attempts = c.http, c.sha, c.attempts
	respBody, charged, held := c.respBody, c.charged, c.held
	c.mu.Unlock()
	fail := func(msg string) {
		row.Error = &msg
	}
	switch {
	case cerr == "budget":
		return nil, whyHardStop
	case requests == 0:
		if ctx.Err() != nil {
			return nil, ""
		}
		// Nothing reached the transport: the harness failed before the final call.
		fail("harness")
		row.Detail = redact(fmt.Sprint(res.err))
		return row, "harness failure before any request: " + row.Detail
	case strings.HasPrefix(cerr, "outbound body differs"):
		fail("body mismatch")
		return row, "outbound body differs from the checked body"
	}
	if held {
		row.HeldUSD = charged
	}
	switch {
	case cerr != "":
		if ctx.Err() != nil {
			fail(errCanceled)
		} else {
			fail(cerr)
		}
	case row.HTTP != http.StatusOK:
		fail(fmt.Sprintf("http %d", row.HTTP))
		row.Detail = redact(string(respBody))
	default:
		o, err := outcomeOf(respBody)
		if err != nil {
			fail("response: " + err.Error())
			break
		}
		row.Outcome = o
		if held {
			// Charged at the worst case for a lost attempt before this one.
			row.HeldUSD = charged - o.USD
		}
		if o.Status != "completed" && o.Status != "incomplete" {
			// A failed response is the API's failure, not the model's reply. Its usage is
			// kept for the spend.
			fail("status " + o.Status)
		}
		if res.err != nil && res.resp == nil {
			row.Detail = redact(res.err.Error())
		}
	}
	return row, ""
}
