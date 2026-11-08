package arms

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s3"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/score"
)

// Job is one (arm, session) pair. Exactly one of Session, Bench, Task and Route is set.
type Job struct {
	Arm     string
	Session *corpus.Session
	Bench   *corpus.CBench
	Task    *s2.Task
	Route   *s3.Item
}

// ID returns the session or item id.
func (j Job) ID() string {
	if j.Bench != nil {
		return j.Bench.ID
	}
	if j.Task != nil {
		return j.Task.ID
	}
	if j.Route != nil {
		return j.Route.ID
	}
	return j.Session.ID
}

// Exec runs a job and returns its rows. Its only error is the budget stop.
func (e *Env) Exec(ctx context.Context, j Job) ([]Row, error) {
	if j.Bench != nil {
		return e.execBench(ctx, j.Arm, *j.Bench)
	}
	if j.Task != nil {
		return e.execTask(ctx, j.Arm, *j.Task)
	}
	if j.Route != nil {
		return e.execRoute(ctx, *j.Route)
	}
	return e.execSession(ctx, j.Arm, *j.Session)
}

// Plan orders jobs: session by session, with each session's arms in a seeded shuffle.
// A session's arms therefore run close together in time, which keeps load drift out of
// the paired comparison, and no arm always goes first.
func Plan(arms []string, sessions []corpus.Session, benches []corpus.CBench, seed uint64) []Job {
	var jobs []Job
	shuffled := func(id string) []string {
		h := fnv.New64a()
		_, _ = h.Write([]byte(id))
		rng := rand.New(rand.NewPCG(seed, h.Sum64()))
		out := append([]string(nil), arms...)
		rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}
	for i := range sessions {
		for _, arm := range shuffled(sessions[i].ID) {
			jobs = append(jobs, Job{Arm: arm, Session: &sessions[i]})
		}
	}
	for i := range benches {
		for _, arm := range shuffled(benches[i].ID) {
			jobs = append(jobs, Job{Arm: arm, Bench: &benches[i]})
		}
	}
	return jobs
}

// zeroRow is the row of a session that produced none of its own: every batch unreached
// and the failure in error.
func (e *Env) zeroRow(j Job, msg string) Row {
	var t *tracker
	seen, total := 0, 0
	if j.Route != nil {
		t = newTracker(j.Arm, j.Route.ID, "s3-"+j.Route.Set, len(j.Route.History), nil)
		t.err = errors.New(msg)
		t.c.SessionErrors = 1
		row := t.row(e, 0, 0)
		row.Route = newRouteOutcome(*j.Route)
		return row
	}
	if j.Task != nil {
		t = newTracker(j.Arm, j.Task.ID, taskSession(*j.Task).Set, len(j.Task.Turns), nil)
	} else if j.Bench != nil {
		t = newTracker(j.Arm, j.Bench.ID, "c-"+j.Bench.Set, len(j.Bench.History), j.Bench.Probes)
		for _, f := range j.Bench.Facts {
			if f.Category != corpus.F6 {
				total++
			}
		}
	} else {
		t = newTracker(j.Arm, j.Session.ID, j.Session.Set, j.Session.Length, j.Session.Probes)
		total = len(score.ExposureOf(*j.Session, nil, nil))
	}
	t.err = errors.New(msg)
	t.c.SessionErrors = 1
	row := t.row(e, seen, total)
	if j.Task != nil {
		// No hidden test ran, so this row has no score, which is not a score of zero.
		row.S2 = &S2Outcome{Infra: scrub(msg, e.APIKey)}
	}
	return row
}

// Runner executes jobs on a worker pool and appends rows as sessions finish.
type Runner struct {
	Env        *Env
	ScoresPath string
	Workers    int
	Timeout    time.Duration
	Out        io.Writer
	// Exec defaults to Env.Exec. Tests replace it.
	Exec func(ctx context.Context, j Job) ([]Row, error)
}

// Summary is what a finished Run did.
type Summary struct {
	Finished, Skipped, Errors int
	BudgetStop                bool
}

// Run executes the jobs not already in the scores file, so a restarted run does not
// pay twice. A failed session is a scored row. The budget stop ends the run, and the
// sessions it interrupted leave no row, so resuming with more budget runs them again.
func (r *Runner) Run(ctx context.Context, jobs []Job) (Summary, error) {
	done, err := finishedPairs(r.ScoresPath)
	if err != nil {
		return Summary{}, err
	}
	f, err := openScores(r.ScoresPath)
	if err != nil {
		return Summary{}, err
	}
	defer func() { _ = f.Close() }()

	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var (
		mu  sync.Mutex
		sum Summary
		wg  sync.WaitGroup
	)
	writeRows := func(rows []Row) error {
		mu.Lock()
		defer mu.Unlock()
		for _, row := range rows {
			raw, err := json.Marshal(row)
			if err != nil {
				return err
			}
			if _, err := f.Write(append(raw, '\n')); err != nil {
				return err
			}
			r.progress(row)
			if row.Error != "" {
				sum.Errors++
			}
		}
		sum.Finished++
		return nil
	}
	budgetStop := func(j Job) {
		mu.Lock()
		first := !sum.BudgetStop
		sum.BudgetStop = true
		if first {
			fmt.Fprintf(r.Out, "ERROR budget stop at $%.4f, seen by %s %s; no further sessions start\n", r.spent(), j.Arm, j.ID())
		}
		mu.Unlock()
		stop()
	}
	var writeErr error
	queue := make(chan Job)
	for range max(r.Workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				if ctx.Err() != nil {
					continue
				}
				rows, err := r.exec(ctx, j)
				if errors.Is(err, lab.ErrBudget) {
					budgetStop(j)
					continue
				}
				if ctx.Err() != nil {
					continue // interrupted, so it is not a result
				}
				if err := writeRows(rows); err != nil {
					mu.Lock()
					writeErr = err
					mu.Unlock()
					stop()
					continue
				}
				if r.Env != nil && r.Env.Ledger != nil && r.Env.Ledger.OverBudget() {
					budgetStop(j)
				}
			}
		}()
	}
feed:
	for _, j := range jobs {
		if done[pairKey(j.Arm, j.ID())] {
			mu.Lock()
			sum.Skipped++
			mu.Unlock()
			continue
		}
		select {
		case queue <- j:
		case <-ctx.Done():
			break feed
		}
	}
	close(queue)
	wg.Wait()
	if writeErr != nil {
		return sum, writeErr
	}
	return sum, nil
}

// exec runs one job under the session timeout. Any panic on this goroutine and any
// unexpected error becomes an error row, so one session cannot end the run. A panic
// in a goroutine the agent owns cannot be caught here; model and tool calls recover
// on their own, in watchModel and toolRec.
func (r *Runner) exec(ctx context.Context, j Job) (rows []Row, err error) {
	defer func() {
		if p := recover(); p != nil {
			rows, err = []Row{r.zero(j, fmt.Sprintf("panic: %v", p))}, nil
		}
	}()
	sctx := ctx
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		sctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	run := r.Exec
	if run == nil {
		run = r.Env.Exec
	}
	rows, err = run(sctx, j)
	if err != nil && !errors.Is(err, lab.ErrBudget) && ctx.Err() == nil {
		return []Row{r.zero(j, err.Error())}, nil
	}
	return rows, err
}

func (r *Runner) zero(j Job, msg string) Row {
	if r.Env != nil {
		return r.Env.zeroRow(j, msg)
	}
	return (&Env{}).zeroRow(j, msg)
}

func (r *Runner) spent() float64 {
	if r.Env != nil && r.Env.Ledger != nil {
		return r.Env.Ledger.Spent()
	}
	return 0
}

func (r *Runner) progress(row Row) {
	fmt.Fprintf(r.Out, "%s %s retention=%.3f secs=%.1f usd=%.4f\n", row.Arm, row.Session, row.Retention, row.Seconds, r.spent())
	if row.Error != "" {
		fmt.Fprintf(r.Out, "ERROR %s %s: %s\n", row.Arm, row.Session, row.Error)
	}
}

func pairKey(arm, session string) string { return arm + "\x00" + session }

// finishedPairs reads the pairs already scored. A torn last line from a crash is not
// a finished pair.
func finishedPairs(path string) (map[string]bool, error) {
	done := map[string]bool{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return done, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var row struct{ Arm, Session string }
		if json.Unmarshal(sc.Bytes(), &row) == nil && row.Arm != "" && row.Session != "" {
			done[pairKey(row.Arm, row.Session)] = true
		}
	}
	return done, sc.Err()
}

// openScores opens the file for append, ending a torn last line first so the next
// row stays on a line of its own.
func openScores(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if size := info.Size(); size > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, size-1); err != nil {
			_ = f.Close()
			return nil, err
		}
		if last[0] != '\n' {
			if _, err := f.Write([]byte("\n")); err != nil {
				_ = f.Close()
				return nil, err
			}
		}
	}
	return f, nil
}

// CheckLocalWindow refuses arms whose window is not what the server enforces. A wrong
// window would produce plausible numbers for the wrong condition.
func CheckLocalWindow(ctx context.Context, baseURL, modelName string, arms []string) error {
	need := map[int][]string{}
	for _, arm := range arms {
		if w := LocalWindow(arm); w > 0 {
			need[w] = append(need[w], arm)
		}
	}
	if len(need) == 0 {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/models", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("local server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var models struct {
		Data []struct {
			ID          string `json:"id"`
			MaxModelLen int    `json:"max_model_len"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return fmt.Errorf("local server /v1/models: %w", err)
	}
	for _, m := range models.Data {
		if m.ID != modelName {
			continue
		}
		for w, names := range need {
			if m.MaxModelLen != w {
				return fmt.Errorf("arms %s need a server window of %d, and %s reports max_model_len %d", strings.Join(names, ","), w, modelName, m.MaxModelLen)
			}
		}
		return nil
	}
	return fmt.Errorf("local server does not serve model %q", modelName)
}

// ReadAPIKey reads OPENAI_API_KEY from a .env file. Its errors never include a value.
func ReadAPIKey(path string) (string, error) { return ReadEnvKey(path, "OPENAI_API_KEY") }

// ReadEnvKey reads one key from the env file. The error names the key and never its value.
func ReadEnvKey(path, key string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(name) == key {
			value = strings.Trim(strings.TrimSpace(value), `"'`)
			if value == "" {
				return "", fmt.Errorf("%s is empty in %s", key, path)
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("no %s in %s", key, path)
}

// ScrubWriter removes the API key from what passes through it. The CLI sends the
// agent's log through it, because that log quotes provider error bodies.
type ScrubWriter struct {
	W   io.Writer
	Key string
}

// Write implements io.Writer. It reports the input length, so a shorter scrubbed line
// is not read as a short write.
func (s *ScrubWriter) Write(p []byte) (int, error) {
	text := string(p)
	if s.Key != "" {
		text = strings.ReplaceAll(text, s.Key, "sk-***")
	}
	text = keyShape.ReplaceAllString(text, "sk-***")
	if _, err := io.WriteString(s.W, text); err != nil {
		return 0, err
	}
	return len(p), nil
}
