// Command stage0 runs the arms of the stage 0 experiment and reports on finished
// runs. See experiments/stage0/HARNESS.md.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/arms"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/report"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s3"
	agentscope "github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier/jev"
	classifieropenai "github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier/openai"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/credential"
)

const usage = `usage:
  stage0 run --run NAME --arms A0,A3-8K,... --set pilot|acceptance|cpilot|cacceptance|s2|s2acceptance|s2long|s2long2|s3pilot|s3acceptance --n N
             --workers W --local-slots L --cloud-slots C --budget USD --spent USD [--limit-sessions K] [--repeat R]
             [--classifiers decisions,piccolo,jev --piccolo-url URL --piccolo-model NAME] [--routes FILE]
  stage0 route-tasks --run NAME --set s2|s2acceptance|s2long|s2long2 --classifiers decisions|piccolo|jev --theta T --budget USD --spent USD
  stage0 report --runs a,b --out dir [--theta decisions/R2=0.7,decisions/R3=0.8]
`

const defaultRunsDir = "experiments/stage0/runs"

func main() {
	os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr))
}

func realMain(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "run":
		var stopped bool
		stopped, err = cmdRun(args[1:], stdout)
		if err == nil && stopped {
			return 3
		}
	case "report":
		err = cmdReport(args[1:], stdout)
	case "route-tasks":
		err = cmdRouteTasks(args[1:], stdout)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "stage0: %v\n", err)
		return 1
	}
	return 0
}

func cmdReport(args []string, stdout io.Writer) error {
	out := make([]string, len(args))
	copy(out, args)
	for i, a := range out {
		if (a == "--runs" || a == "-runs") && i+1 < len(out) {
			out[i+1] = runDirs(out[i+1])
		}
		if name, ok := strings.CutPrefix(a, "--runs="); ok {
			out[i] = "--runs=" + runDirs(name)
		}
	}
	report.FrozenBudgets = arms.Budgets()
	return report.Main(out, stdout)
}

// runDirs lets --runs name a run instead of spelling out its directory.
func runDirs(list string) string {
	parts := strings.Split(list, ",")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			parts[i] = p
			continue
		}
		parts[i] = filepath.Join(defaultRunsDir, p)
	}
	return strings.Join(parts, ",")
}

type runFlags struct {
	run, armList, set, dir, envPath            string
	localURL, localModel, cloudURL, cloudModel string
	n, workers, localSlots, cloudSlots, limit  int
	repeat                                     int
	budget, spent                              float64
	seed                                       uint64
	sessionTimeout                             time.Duration
	dump                                       bool
	variant                                    arms.Variant
	routesPath, onlyPath                       string
	classifierList, piccoloURL, piccoloModel   string
	jevModel                                   string
	mtRoute                                    string
	classifierSlots                            int
}

func parseRunFlags(args []string, out io.Writer) (*runFlags, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(out)
	f := &runFlags{}
	fs.StringVar(&f.run, "run", "", "run name; files go to <dir>/<run>")
	fs.StringVar(&f.armList, "arms", "", "comma-separated arms")
	fs.StringVar(&f.set, "set", "", "pilot, acceptance, cpilot, cacceptance, s2, s2acceptance, s2long, s2long2, s3pilot or s3acceptance")
	fs.IntVar(&f.n, "n", 0, "number of sessions for an acceptance set")
	fs.IntVar(&f.workers, "workers", 1, "parallel sessions")
	fs.IntVar(&f.localSlots, "local-slots", 24, "in-flight local requests")
	fs.IntVar(&f.cloudSlots, "cloud-slots", 32, "in-flight cloud requests")
	fs.Float64Var(&f.budget, "budget", 0, "cloud hard stop in USD, across all runs")
	fs.Float64Var(&f.spent, "spent", 0, "USD already spent, including this run's ledger")
	fs.IntVar(&f.limit, "limit-sessions", 0, "run only the first K sessions")
	fs.IntVar(&f.repeat, "repeat", 1, "s2 only: runs of each task, which the report averages into one unit")
	fs.StringVar(&f.dir, "dir", defaultRunsDir, "directory of runs")
	fs.StringVar(&f.envPath, "env", ".env", "file holding OPENAI_API_KEY")
	fs.StringVar(&f.localURL, "local-url", "http://127.0.0.1:8000", "vLLM base URL")
	fs.StringVar(&f.localModel, "local-model", "Qwen3.5-4B", "served local model name")
	fs.StringVar(&f.cloudURL, "cloud-url", "https://api.openai.com", "Responses API base URL")
	fs.StringVar(&f.cloudModel, "cloud-model", "gpt-6-luna", "cloud model")
	fs.Uint64Var(&f.seed, "seed", 1, "seed of the job order")
	fs.DurationVar(&f.sessionTimeout, "session-timeout", 3*time.Hour, "timeout of one (arm, session) pair")
	fs.IntVar(&f.variant.Refresh, "fork-refresh", 0, "tuning: Fork B refresh in tokens of cloud growth")
	fs.IntVar(&f.variant.Target, "summary-target", 0, "tuning: target of Fork B and briefs in tokens")
	fs.BoolVar(&f.variant.NeverClear, "never-clear", false, "tuning: T1 keeps local's context at every takeover")
	fs.BoolVar(&f.variant.TaskTestRule, "task-test-rule", false, "s2 sensitivity: tell every arm to run the tests")
	fs.BoolVar(&f.variant.CanonicalReplay, "canonical-replay", false, "send local's call arguments to the cloud in the cloud's own form")
	fs.StringVar(&f.routesPath, "routes", "", "s2 only: follow the routes route-tasks wrote, not the set's own owners")
	fs.IntVar(&f.variant.RepairRounds, "repair-rounds", 0, "s2 only: send a session that does not compile back to its last owner, at most this many times")
	fs.StringVar(&f.onlyPath, "only", "", "s2 only: a JSON list of {arm, session}, the only pairs to run")
	fs.BoolVar(&f.dump, "dump", false, "write every request and response body to dump.jsonl, for diagnosis only")
	fs.StringVar(&f.classifierList, "classifiers", "", "S3 only: decisions, piccolo, jev")
	fs.StringVar(&f.jevModel, "jev-model", "", "S3 only: hosted Jev model, required with the jev classifier")
	fs.StringVar(&f.piccoloURL, "piccolo-url", "http://127.0.0.1:18604/v1/systemone", "S3 only: local /v1/systemone endpoint")
	fs.StringVar(&f.piccoloModel, "piccolo-model", "Piccolo-4B", "S3 only: the model name Piccolo serves")
	fs.IntVar(&f.classifierSlots, "classifier-slots", 8, "S3 only: in-flight calls per classifier")
	fs.StringVar(&f.mtRoute, "mt-route", "", "smoke runs on the s2 tuning set only: send every MT-hybrid query to edge or cloud")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	switch {
	case f.run == "":
		return nil, errors.New("--run is required")
	case f.armList == "":
		return nil, errors.New("--arms is required")
	case f.budget <= 0:
		return nil, errors.New("--budget must be positive: it is the hard stop on real spend")
	case f.spent < 0:
		return nil, errors.New("--spent cannot be negative")
	case f.repeat < 1:
		return nil, errors.New("--repeat must be at least 1")
	case f.repeat > 1 && !isS2(f.set):
		return nil, errors.New("--repeat is for the s2 sets: a session or an item has no repeats to average")
	case f.variant.TaskTestRule && !isS2(f.set):
		return nil, errors.New("--task-test-rule is for the s2 sets")
	case f.mtRoute != "" && f.set != "s2":
		return nil, errors.New("--mt-route is for smoke runs on the s2 tuning set only")
	}
	return f, nil
}

func parseArms(list string, bench, task, route bool) ([]string, error) {
	allowed := arms.SessionArms
	kind := "session"
	if bench {
		allowed, kind = arms.BenchArms, "bench"
	}
	if task {
		allowed, kind = arms.TaskArms, "task"
	}
	if route {
		allowed, kind = arms.RouteArms, "route"
	}
	var out []string
	for name := range strings.SplitSeq(list, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("arm %q cannot run on a %s set; allowed: %s (A4-abrupt and T1+brief come with A4 and T1)", name, kind, strings.Join(allowed, ","))
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no arms given")
	}
	return out, nil
}

func loadSet(set string, n, limit int) ([]corpus.Session, []corpus.CBench, error) {
	var sessions []corpus.Session
	var benches []corpus.CBench
	switch set {
	case "pilot":
		sessions = corpus.Pilot()
	case "acceptance":
		if n <= 0 {
			return nil, nil, errors.New("--n must be positive for an acceptance set")
		}
		sessions = corpus.Acceptance(n)
	case "cpilot":
		benches = corpus.CBenchPilot()
	case "cacceptance":
		if n <= 0 {
			return nil, nil, errors.New("--n must be positive for an acceptance set")
		}
		benches = corpus.CBenchAcceptance(n)
	case "s2", "s2acceptance", "s2long", "s2long2", "s3pilot", "s3acceptance":
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown set %q", set)
	}
	if limit > 0 {
		sessions = sessions[:min(limit, len(sessions))]
		benches = benches[:min(limit, len(benches))]
	}
	return sessions, benches, nil
}

// tasks is what this run does on the s2 set, repeats included.
func (f *runFlags) tasks() []s2.Task {
	return arms.RepeatTasks(s2Tasks(f.set, f.limit), f.repeat)
}

func isS2(set string) bool {
	return set == "s2" || set == "s2acceptance" || set == "s2long" || set == "s2long2"
}

// s2Tasks returns the tasks of an s2 set, and nothing for any other set.
func s2Tasks(set string, limit int) []s2.Task {
	var tasks []s2.Task
	switch set {
	case "s2":
		tasks = s2.Tasks()
	case "s2acceptance":
		tasks = s2.Acceptance()
	case "s2long":
		tasks = s2.Long()
	case "s2long2":
		tasks = s2.Long2()
	}
	if limit > 0 {
		tasks = tasks[:min(limit, len(tasks))]
	}
	return tasks
}

// onlyJobs keeps the pairs a file lists. A pair the plan does not hold is an error, because
// a typo would otherwise shrink the run without a word.
func onlyJobs(jobs []arms.Job, path string) ([]arms.Job, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pairs []struct{ Arm, Session string }
	if err := json.Unmarshal(raw, &pairs); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	want := make(map[[2]string]bool, len(pairs))
	for _, p := range pairs {
		want[[2]string{p.Arm, p.Session}] = true
	}
	var kept []arms.Job
	for _, j := range jobs {
		key := [2]string{j.Arm, j.ID()}
		if want[key] {
			kept = append(kept, j)
			delete(want, key)
		}
	}
	if len(want) > 0 {
		return nil, fmt.Errorf("%s: %d pairs are not in the plan", path, len(want))
	}
	return kept, nil
}

// routedTasks is this run's tasks with the owners in --routes, and the hash that names them.
func routedTasks(f *runFlags) ([]s2.Task, string, error) {
	raw, err := os.ReadFile(f.routesPath)
	if err != nil {
		return nil, "", err
	}
	var routes arms.TaskRoutes
	if err := json.Unmarshal(raw, &routes); err != nil {
		return nil, "", fmt.Errorf("%s: %w", f.routesPath, err)
	}
	tasks, err := arms.ApplyRoutes(s2Tasks(f.set, f.limit), routes)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return arms.RepeatTasks(tasks, f.repeat), hex.EncodeToString(sum[:]), nil
}

// cmdRouteTasks asks one classifier for every turn's route and writes them. The file is
// written once, so that every arm of a comparison follows the same routes.
func cmdRouteTasks(args []string, stdout io.Writer) error {
	f := &runFlags{}
	var theta float64
	fs := flag.NewFlagSet("route-tasks", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.StringVar(&f.run, "run", "", "run name; files go to <dir>/<run>")
	fs.StringVar(&f.set, "set", "s2acceptance", "s2, s2acceptance, s2long or s2long2")
	fs.StringVar(&f.classifierList, "classifiers", "decisions", "the one classifier: decisions, piccolo or jev")
	fs.Float64Var(&theta, "theta", 0.8, "a local route below this confidence goes to the cloud")
	fs.Float64Var(&f.budget, "budget", 0, "cloud hard stop in USD, across all runs")
	fs.Float64Var(&f.spent, "spent", 0, "USD already spent")
	fs.StringVar(&f.dir, "dir", defaultRunsDir, "directory of runs")
	fs.StringVar(&f.envPath, "env", ".env", "file holding the keys")
	fs.StringVar(&f.cloudModel, "cloud-model", "gpt-6-luna", "the model OpenAI Decisions uses")
	fs.StringVar(&f.jevModel, "jev-model", "", "hosted Jev model, required with the jev classifier")
	fs.StringVar(&f.piccoloURL, "piccolo-url", "http://127.0.0.1:18604/v1/systemone", "local /v1/systemone endpoint")
	fs.StringVar(&f.piccoloModel, "piccolo-model", "Piccolo-4B", "the model name Piccolo serves")
	fs.IntVar(&f.classifierSlots, "classifier-slots", 8, "in-flight calls")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if f.run == "" || !isS2(f.set) || f.budget <= 0 {
		return errors.New("route-tasks needs --run, an s2 --set and a positive --budget")
	}
	key, err := arms.ReadAPIKey(f.envPath)
	if err != nil {
		return err
	}
	classifiers, err := buildClassifiers(f, key)
	if err != nil {
		return err
	}
	if len(classifiers) != 1 {
		return errors.New("route-tasks takes one classifier")
	}
	dir := filepath.Join(f.dir, f.run)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "routes.json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s exists; routes are written once", path)
	}
	ledger, err := lab.NewLedger(f.run, filepath.Join(dir, "ledger.jsonl"), f.budget)
	if err != nil {
		return err
	}
	defer func() { _ = ledger.Close() }()
	ledger.SetSpent(f.spent)
	env := &arms.Env{Run: f.run, Ledger: ledger, APIKey: key}
	tasks := s2Tasks(f.set, 0)
	routes, err := arms.RouteTasks(context.Background(), env, classifiers[0], tasks, theta)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	local, total, failed := 0, 0, 0
	for _, turns := range routes {
		for _, r := range turns {
			total++
			if r.Side == "local" {
				local++
			}
			if r.Error != "" {
				failed++
			}
		}
	}
	fmt.Fprintf(stdout, "wrote %s: %d of %d turns local, %d failed, cloud spend $%.4f\n", path, local, total, failed, ledger.Spent())
	return nil
}

// s3Items returns the items of an S3 set, and nothing for any other set.
func s3Items(set string, n, limit int) ([]s3.Item, error) {
	var items []s3.Item
	switch set {
	case "s3pilot":
		items = s3.Pilot()
	case "s3acceptance":
		if n <= 0 {
			return nil, errors.New("--n must be positive for an acceptance set")
		}
		items = s3.Acceptance(n)
	default:
		return nil, nil
	}
	if limit > 0 {
		items = items[:min(limit, len(items))]
	}
	return items, nil
}

// buildClassifiers makes the classifiers under test. Each runs at most --classifier-slots
// calls at once. OpenAI Decisions uses the model under test at its input and output prices.
func buildClassifiers(f *runFlags, key string) ([]arms.NamedClassifier, error) {
	var out []arms.NamedClassifier
	for name := range strings.SplitSeq(f.classifierList, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		named := arms.NamedClassifier{Name: name, Sem: make(chan struct{}, max(f.classifierSlots, 1))}
		switch name {
		case "decisions":
			c, err := classifieropenai.New(classifieropenai.Config{Credential: &credential.OpenAICredential{Key: key}, Model: f.cloudModel})
			if err != nil {
				return nil, err
			}
			named.C, named.Cloud, named.Prices = c, true, lab.Luna
		case "piccolo":
			// The plugin's launch script starts the server with this literal key.
			c, err := jev.New(jev.Config{APIKey: "EMPTY", Model: f.piccoloModel, Endpoint: f.piccoloURL})
			if err != nil {
				return nil, err
			}
			named.C = c
		case "jev":
			if f.jevModel == "" {
				return nil, errors.New("the jev classifier needs --jev-model")
			}
			hosted, err := arms.ReadEnvKey(f.envPath, "TYPESAFE_API_KEY")
			if err != nil {
				return nil, err
			}
			c, err := jev.New(jev.Config{APIKey: hosted, Model: f.jevModel})
			if err != nil {
				return nil, err
			}
			named.C, named.Cloud = c, true
		default:
			return nil, fmt.Errorf("unknown classifier %q; use decisions, piccolo or jev", name)
		}
		out = append(out, named)
	}
	return out, nil
}

// ledgerSpend sums the cloud cost a run's ledger already holds. The ledger only counts
// its own rows from a clean start, so a restart that forgot them could overspend.
func ledgerSpend(path string) (float64, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	total := 0.0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var row struct {
			Side    string  `json:"side"`
			CostUSD float64 `json:"cost_usd"`
		}
		if json.Unmarshal(sc.Bytes(), &row) == nil && row.Side == lab.SideCloud {
			total += row.CostUSD
		}
	}
	return total, sc.Err()
}

// dumpWriter appends one JSON line per model call. The key never appears in a body the
// transports build, but a provider's error can quote it, so each line is scrubbed.
func dumpWriter(w io.Writer, key string) func(arm, session, side, role string, req, resp []byte) {
	var mu sync.Mutex
	return func(arm, session, side, role string, req, resp []byte) {
		line, err := json.Marshal(map[string]any{
			"arm": arm, "session": session, "side": side, "role": role,
			"req": json.RawMessage(req), "resp": string(resp),
		})
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write(append(bytes.ReplaceAll(line, []byte(key), []byte("sk-***")), '\n'))
	}
}

// writeManifest records what a run's rows mean: its set and the effective budgets. A
// resumed run must keep the budgets it started with, or one run would hold two conditions.
func writeManifest(dir, run, set string) error {
	path := filepath.Join(dir, report.ManifestFile)
	m := report.Manifest{Run: run, Set: set, Commit: gitCommit(), Budgets: arms.Budgets()}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	switch old, err := os.ReadFile(path); {
	case err == nil:
		var prev report.Manifest
		if err := json.Unmarshal(old, &prev); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return checkResume(prev, m, path)
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// checkResume refuses to add rows to a run from other budgets or other code. A manifest
// states one commit, so a resume on another would make it false for the rows added.
func checkResume(prev, next report.Manifest, path string) error {
	if prev.Set != next.Set {
		return fmt.Errorf("run %s started on set %s and this is %s (%s); use a new run name", next.Run, prev.Set, next.Set, path)
	}
	if !prev.SameBudgets(next) {
		return fmt.Errorf("run %s started with other budgets (%s); use a new run name", next.Run, path)
	}
	if prev.Commit != next.Commit {
		return fmt.Errorf("run %s started at commit %s and this is %s (%s); use a new run name", next.Run, prev.Commit, next.Commit, path)
	}
	return nil
}

// gitCommit names the code a run used. A binary built elsewhere still states it, because
// this reads the checkout it runs in, which is where the run's rows go.
func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(out))
	// The pathspec is from the repository's root, and any line counts: an untracked or
	// deleted file changes what ran as much as an edited one.
	if status, err := exec.Command("git", "status", "--porcelain", "--", ":/experiments/stage0").Output(); err == nil && strings.TrimSpace(string(status)) != "" {
		return commit + "-dirty"
	}
	return commit
}

func cmdRun(args []string, stdout io.Writer) (bool, error) {
	f, err := parseRunFlags(args, stdout)
	if err != nil {
		return false, err
	}
	sessions, benches, err := loadSet(f.set, f.n, f.limit)
	if err != nil {
		return false, err
	}
	tasks := f.tasks()
	if f.routesPath != "" {
		if !isS2(f.set) {
			return false, errors.New("--routes is for the s2 sets")
		}
		if tasks, f.variant.Routes, err = routedTasks(f); err != nil {
			return false, err
		}
	}
	items, err := s3Items(f.set, f.n, f.limit)
	if err != nil {
		return false, err
	}
	armNames, err := parseArms(f.armList, len(benches) > 0, len(tasks) > 0, len(items) > 0)
	if err != nil {
		return false, err
	}
	// MT-routed exists to follow master's routes; without them it would quietly follow the
	// set's own owners. The tuning set has no routes, so its smoke runs use those.
	if slices.Contains(armNames, arms.ArmMTRouted) && f.routesPath == "" && f.set != "s2" {
		return false, errors.New("MT-routed needs --routes outside the s2 tuning set")
	}
	arms.SetVariant(f.variant)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := arms.CheckLocalWindow(ctx, f.localURL, f.localModel, armNames); err != nil {
		return false, err
	}
	key, err := arms.ReadAPIKey(f.envPath)
	if err != nil {
		return false, err
	}

	dir := filepath.Join(f.dir, f.run)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	already, err := ledgerSpend(ledgerPath)
	if err != nil {
		return false, err
	}
	if f.spent < already {
		return false, fmt.Errorf("--spent %.4f is below the $%.4f this run's ledger already holds", f.spent, already)
	}
	if err := writeManifest(dir, f.run, f.set); err != nil {
		return false, err
	}
	ledger, err := lab.NewLedger(f.run, ledgerPath, f.budget)
	if err != nil {
		return false, err
	}
	defer func() { _ = ledger.Close() }()
	ledger.SetSpent(f.spent)

	logFile, err := os.OpenFile(filepath.Join(dir, "agent.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer func() { _ = logFile.Close() }()
	agentscope.Log().SetOutput(&arms.ScrubWriter{W: logFile, Key: key})

	env := &arms.Env{
		Run: f.run, Ledger: ledger,
		CloudURL: f.cloudURL, CloudModel: f.cloudModel, APIKey: key,
		LocalURL: f.localURL, LocalModel: f.localModel,
		CloudSlots: lab.NewSlots(f.cloudSlots), LocalSlots: lab.NewSlots(f.localSlots),
		Tokens: lab.NewTokenizer(f.localURL, f.localModel),
	}
	if f.mtRoute != "" {
		if env.ThreadClassifier, err = arms.FixedThreadRoute(f.mtRoute); err != nil {
			return false, err
		}
	}
	if len(items) > 0 {
		if env.Classifiers, err = buildClassifiers(f, key); err != nil {
			return false, err
		}
		if err := arms.CheckRouteEnv(env); err != nil {
			return false, err
		}
	}
	if f.dump {
		dumpFile, err := os.OpenFile(filepath.Join(dir, "dump.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return false, err
		}
		defer func() { _ = dumpFile.Close() }()
		env.Dump = dumpWriter(dumpFile, key)
	}
	runner := &arms.Runner{
		Env: env, ScoresPath: filepath.Join(dir, "scores.jsonl"),
		Workers: f.workers, Timeout: f.sessionTimeout, Out: stdout,
	}
	jobs := arms.Plan(armNames, sessions, benches, f.seed)
	if len(tasks) > 0 {
		jobs = arms.PlanTasks(armNames, tasks, f.seed)
		if f.onlyPath != "" {
			if jobs, err = onlyJobs(jobs, f.onlyPath); err != nil {
				return false, err
			}
		}
	}
	if len(items) > 0 {
		jobs = arms.PlanRoute(items)
	}
	fmt.Fprintf(stdout, "run %s: %d pairs, %d workers, spent $%.4f of $%.2f\n", f.run, len(jobs), f.workers, f.spent, f.budget)
	sum, err := runner.Run(ctx, jobs)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(stdout, "done: %d finished, %d skipped as already scored, %d rows with errors, cloud spend $%.4f\n", sum.Finished, sum.Skipped, sum.Errors, ledger.Spent())
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("interrupted: %w", err)
	}
	return sum.BudgetStop, nil
}
