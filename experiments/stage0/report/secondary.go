package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ManifestFile names what a run states about itself.
const ManifestFile = "manifest.json"

// The sets of S2 rows, whose retention is a hidden-test score. L3 runs the tuning tasks, and
// the acceptance verdict stands on tasks the configuration never saw.
const (
	setS2           = "s2"
	setS2Acceptance = "s2-acceptance"
)

// Manifest is what a run's rows mean: its set, the budgets in effect and the commit. Runs at
// different budgets or different commits measure different conditions and cannot share a
// report.
type Manifest struct {
	Run     string         `json:"run"`
	Set     string         `json:"set"`
	Commit  string         `json:"commit"`
	Budgets map[string]any `json:"budgets"`
}

// SameBudgets compares the JSON forms, so a value read back from a file equals the int
// it was written from.
func (m Manifest) SameBudgets(other Manifest) bool {
	a, errA := json.Marshal(m.Budgets)
	b, errB := json.Marshal(other.Budgets)
	return errA == nil && errB == nil && string(a) == string(b)
}

func (d *Data) loadManifest(dir string) error {
	path := filepath.Join(dir, ManifestFile)
	raw, ok, err := readWhole(path)
	if err != nil {
		return err
	}
	if !ok {
		d.NoManifest = append(d.NoManifest, dir)
		return nil
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("report: %s: %w", path, err)
	}
	d.Manifests = append(d.Manifests, m)
	return nil
}

func readWhole(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("report: read %s: %w", path, err)
	}
	return raw, true, nil
}

// CheckPooling refuses data that a single report cannot honestly pool. A later run
// replacing rows, or two corpora sharing arm names, would otherwise form pairs and
// produce plausible numbers for no condition at all.
func (d *Data) CheckPooling() error {
	sets := map[string]bool{}
	for _, byID := range d.Scores {
		for _, s := range byID {
			sets[s.Set] = true
		}
	}
	if others := slices.DeleteFunc(otherSets(sets, setS2), func(s string) bool { return s == setS2Acceptance }); (sets[setS2] || sets[setS2Acceptance]) && len(others) > 0 {
		return fmt.Errorf("report: S2 rows cannot share a report with %s: S2 is a hidden-test score, so report it on its own", strings.Join(others, ", "))
	}
	if (sets["s3-tuning"] || sets["s3-acceptance"]) && len(sets) > 1 && !(len(sets) == 2 && sets["s3-tuning"] && sets["s3-acceptance"]) {
		return fmt.Errorf("report: S3 rows measure routing, so report them on their own, not with %s", strings.Join(otherSets(sets, ""), ", "))
	}
	for _, pair := range [][2]string{{"tuning", "acceptance"}, {"c-tuning", "c-acceptance"}, {"s3-tuning", "s3-acceptance"}, {setS2, setS2Acceptance}} {
		if sets[pair[0]] && sets[pair[1]] {
			return fmt.Errorf("report: sets %s and %s cannot be pooled: L4 stands on acceptance rows alone", pair[0], pair[1])
		}
	}
	for _, m := range d.Manifests[min(1, len(d.Manifests)):] {
		if !m.SameBudgets(d.Manifests[0]) {
			return fmt.Errorf("report: runs %s and %s have different budgets, so report them separately", d.Manifests[0].Run, m.Run)
		}
	}
	for _, m := range d.Manifests[min(1, len(d.Manifests)):] {
		if m.Commit != d.Manifests[0].Commit {
			return fmt.Errorf("report: runs %s and %s ran different code (%s and %s), so report them separately", d.Manifests[0].Run, m.Run, d.Manifests[0].Commit, m.Commit)
		}
	}
	for key, run := range d.scoreRun {
		if dir := d.RunDirs[run]; !noManifestArms[key.arm] && slices.Contains(d.NoManifest, dir) {
			return fmt.Errorf("report: run %s holds %s rows and no manifest, so nothing says what produced them", dir, key.arm)
		}
	}
	return nil
}

// noManifestArms may come from a run with no manifest. Their rows came before manifests
// existed, and Amendment 1 left the code behind them as it was.
var noManifestArms = map[string]bool{"A0": true, "A3-8K": true}

// FrozenBudgets are the budgets of a primary result. The command sets them; empty means
// nothing is checked.
var FrozenBudgets map[string]any

// nonPrimary names the runs that used other budgets, such as a sensitivity run, or code
// with uncommitted changes. Their
// verdicts answer another question, so the report has to say so where the verdicts are.
func (d *Data) nonPrimary() []string {
	var out []string
	frozen := Manifest{Budgets: FrozenBudgets}
	for _, m := range d.Manifests {
		if (len(FrozenBudgets) > 0 && !m.SameBudgets(frozen)) || strings.HasSuffix(m.Commit, "-dirty") || m.Commit == "unknown" {
			out = append(out, m.Run)
		}
	}
	return out
}

func otherSets(sets map[string]bool, not string) []string {
	var out []string
	for _, s := range sortedKeys(sets) {
		if s != not {
			out = append(out, s)
		}
	}
	return out
}

// isS2 reports whether the loaded rows are S2's.
func (d *Data) isS2() bool {
	for _, byID := range d.Scores {
		for _, s := range byID {
			return s.Set == setS2 || s.Set == setS2Acceptance
		}
	}
	return false
}

// S2ArmRow is one arm on S2. Runs counts rows and Tasks the tasks behind them. Scored excludes
// scorer failures, which are not zeros. AllPass is the share of scored runs that pass whole.
type S2ArmRow struct {
	Arm           string   `json:"arm"`
	Tasks         int      `json:"tasks"`
	Runs          int      `json:"runs"`
	Scored        int      `json:"scored"`
	Mean          *float64 `json:"mean_score"`
	AllPass       *float64 `json:"all_pass"`
	BuildFailed   int      `json:"build_failed"`
	ScorerFailed  int      `json:"scorer_failed"`
	SessionErrors int      `json:"session_errors"`
}

// S2Section is what an S2 report holds beside G2. Direction is G1's comparison on the core
// metric, recorded next to the S1 verdict and never replacing it.
type S2Section struct {
	Set       string         `json:"set"`
	Arms      []S2ArmRow     `json:"arms"`
	Direction []PairedResult `json:"direction"`
	Counters  []CounterRow   `json:"counters"`
}

// S2 computes the S2 section.
func (d *Data) S2(seed int64) S2Section {
	s := S2Section{Arms: []S2ArmRow{}}
	for _, arm := range sortedKeys(d.Scores) {
		row := S2ArmRow{Arm: arm, Runs: len(d.Scores[arm])}
		tasks := map[string]bool{}
		scores, whole := map[string][]float64{}, map[string][]float64{}
		for id, sc := range d.Scores[arm] {
			s.Set = sc.Set
			unit, _, _ := strings.Cut(id, repeatSep)
			tasks[unit] = true
			if v, ok := TaskScore.Of(sc); ok {
				row.Scored++
				scores[unit] = append(scores[unit], v)
			}
			if v, ok := AllPass.Of(sc); ok {
				whole[unit] = append(whole[unit], v)
			}
			if sc.S2 != nil && sc.S2.BuildFailed {
				row.BuildFailed++
			}
			if sc.S2 != nil && sc.S2.Infra != "" {
				row.ScorerFailed++
			}
			if sc.Error != "" {
				row.SessionErrors++
			}
		}
		row.Tasks = len(tasks)
		row.Mean, row.AllPass = meanOfTasks(scores), meanOfTasks(whole)
		s.Arms = append(s.Arms, row)
		s.Counters = append(s.Counters, d.counterRow(arm))
	}
	s.Direction = []PairedResult{
		d.Paired("A0", "A3-8K", TaskScore, seed),
		d.Paired("A0", "A3-8K", AllPass, seed),
	}
	// A0 - A3-8K mixes local's window with local's ability, and A3-32K takes the window out.
	if _, ok := d.Scores["A3-32K"]; ok {
		s.Direction = append(s.Direction,
			d.Paired("A0", "A3-32K", TaskScore, seed),
			d.Paired("A0", "A3-32K", AllPass, seed),
			d.Paired("A3-32K", "A3-8K", TaskScore, seed),
			d.Paired("T1", "A3-32K", TaskScore, seed),
		)
	}
	// A2 is the repository's Agent hybrid, and A8 is its managed check put in front of an Agent.
	for _, arm := range []string{"A2", "A8"} {
		if _, ok := d.Scores[arm]; ok {
			s.Direction = append(s.Direction,
				d.Paired("T1", arm, TaskScore, seed),
				d.Paired("T1", arm, AllPass, seed),
				d.Paired("A0", arm, TaskScore, seed),
				d.Paired("A0", arm, AllPass, seed),
			)
		}
	}
	return s
}

// meanOfTasks gives each task one vote, so a task with more repeats does not count for more.
func meanOfTasks(byTask map[string][]float64) *float64 {
	var means []float64
	for _, unit := range sortedKeys(byTask) {
		means = append(means, *meanOf(byTask[unit]))
	}
	return meanOf(means)
}

func (d *Data) counterRow(arm string) CounterRow {
	totals := map[string]int{}
	for _, sc := range d.Scores[arm] {
		for _, name := range explainingCounters {
			if v, ok := sc.counter(name); ok {
				totals[name] += v
			}
		}
	}
	return CounterRow{Arm: arm, Totals: totals}
}

// s2Limits are what an S2 report states every time.
var s2Limits = []string{
	"A run's score is the share of hidden tests that pass when its turns are over, whatever state they left, including after a session error.",
	"Repeats of a task are not independent, so n counts tasks and the CI resamples tasks. Repeats only reduce the noise inside a task.",
	"Each agent's write is refused over a version of a file it has not seen, on every arm. It can still read the file and write its old copy anyway, so this makes losing the other side's work harder and does not rule it out. stale_writes counts the refusals.",
	"The workspace is shared, so whatever one side wrote to a file reaches the other side on every two-agent arm. S2 measures what a path loses beyond that.",
	"A4-abrupt, A4+read and T1+read are whole arms here, with their own cloud calls. On S1 they are clones of the receiver at a probe.",
	"The CI is a percentile bootstrap, which undercovers at small n. The floor of 10 pairs is what makes a gate decidable, not a claim of power.",
}

// s2TuningLimit marks a verdict on the tasks the harness was built beside.
const s2TuningLimit = "These tasks were available while the harness was tuned, and two of them ran in a smoke test on older code. This is a pilot verdict, and acceptance stands on held-out tasks."

// TuningSessions are the sessions the configuration was fixed on: the first five of the
// pilot and the first two CBench items. The report knows nothing of how the corpus is
// generated, so a test in cmd/stage0 pins this list to corpus.Pilot and corpus.CBenchPilot.
var TuningSessions = []string{
	"tuning-7100000-L20", "tuning-7100001-L20", "tuning-7100002-L50", "tuning-7100003-L20", "tuning-7100004-L20",
	"cbench-tuning-8100000", "cbench-tuning-8100001",
}

// without returns the same data minus the given sessions. It shares rows with d.
func (d *Data) without(ids []string) *Data {
	out := *d
	out.Scores = map[string]map[string]Score{}
	for arm, byID := range d.Scores {
		kept := map[string]Score{}
		for id, s := range byID {
			if !slices.Contains(ids, id) {
				kept[id] = s
			}
		}
		out.Scores[arm] = kept
	}
	return &out
}

// RetentionWithoutF3 pools F1, F2, F4 and F5. F3 has a floor in CBench, because 855 code
// lines cannot fit in any summary tried here.
var RetentionWithoutF3 = Metric{Name: "retention_without_f3", Of: func(s Score) (float64, bool) {
	correct, total := 0, 0
	for _, cat := range []string{"F1", "F2", "F4", "F5"} {
		v := s.ByCategory[cat]
		correct += v[0]
		total += v[1]
	}
	if total == 0 {
		return 0, false
	}
	return float64(correct) / float64(total), true
}}

// RecipientRow is one arm's retention for one answering side and category. A handoff
// method shows on the side that received it: the cloud keeps its whole history in every
// two-agent arm.
type RecipientRow struct {
	Arm       string   `json:"arm"`
	Recipient string   `json:"recipient"`
	Category  string   `json:"category"`
	Correct   int      `json:"correct"`
	Total     int      `json:"total"`
	Rate      *float64 `json:"rate"`
}

// CounterRow sums the counters that explain an arm's score.
type CounterRow struct {
	Arm    string         `json:"arm"`
	Totals map[string]int `json:"totals"`
}

// explainingCounters are the columns of the counter table, in order.
var explainingCounters = []string{
	"compress", "overflow_recoveries", "lossy_events", "summary_rewrites", "summary_trims", "fork_strays", "stale_writes", "stale_local", "hard_limit_cloud", "max_iters", "empty_replies", "event_reads", "probe_repairs",
	"briefs_compressed", "brief_failed", "brief_incomplete", "fork_b", "fork_failed", "clears", "keeps", "session_errors",
}

// SecondarySection holds what prereg.md registers beside the gates. None of it decides a
// verdict.
type SecondarySection struct {
	Comparisons     []PairedResult `json:"comparisons"`
	Thread          []PairedResult `json:"thread"`
	WithoutF3       []PairedResult `json:"without_f3"`
	ExcludingTuning []PairedResult `json:"excluding_tuning"`
	ByRecipient     []RecipientRow `json:"by_recipient"`
	Counters        []CounterRow   `json:"counters"`
}

// Secondary computes the registered secondary analyses.
func (d *Data) Secondary(seed int64) SecondarySection {
	s := SecondarySection{
		Comparisons: []PairedResult{
			d.Paired("T1+brief", "T1", Retention, seed),
			d.Paired("T1+read", "T1", Retention, seed),
			d.Paired("A4+read", "A4", Retention, seed),
			d.Paired("A4s", "A4", Retention, seed),
			d.Paired("T1", "A4-abrupt", Retention, seed),
		},
		Thread: []PairedResult{
			d.Paired("T1", "A4", Retention, seed),
			d.Paired("T1", "A4s", Retention, seed),
			d.Paired("T1+brief", "A4", Retention, seed),
			d.Paired("T1+read", "A4", Retention, seed),
			d.Paired("T1+read", "A4+read", Retention, seed),
			d.Paired("T1", "A4", RetentionWithoutF3, seed),
			d.Paired("T1", "A4s", RetentionWithoutF3, seed),
			d.Paired("T1+read", "A4+read", RetentionWithoutF3, seed),
		},
		WithoutF3: []PairedResult{
			d.Paired("C3", "C0", RetentionWithoutF3, seed),
			d.Paired("C3", "C0h", RetentionWithoutF3, seed),
			d.Paired("C3", "C6", RetentionWithoutF3, seed),
			d.Paired("C3", "C8", RetentionWithoutF3, seed),
			d.Paired("C4", "C3", RetentionWithoutF3, seed),
			d.Paired("C7", "C3", RetentionWithoutF3, seed),
		},
		ByRecipient: []RecipientRow{},
		Counters:    []CounterRow{},
	}
	rest := d.without(TuningSessions)
	s.ExcludingTuning = []PairedResult{
		rest.Paired("A0", "A3-8K", Retention, seed),
		rest.Paired("T1", "A4", Retention, seed),
		rest.Paired("T1", "A4s", Retention, seed),
		rest.Paired("C3", "C0", Retention, seed),
		rest.Paired("C3", "C6", Retention, seed),
		rest.Paired("C3", "C8", OwnRetention, seed),
	}
	for _, arm := range sortedKeys(d.Scores) {
		pooled := map[[2]string][2]int{}
		for _, sc := range d.Scores[arm] {
			for side, byCat := range sc.ByRecipient {
				for cat, v := range byCat {
					k := [2]string{side, cat}
					sum := pooled[k]
					sum[0] += v[0]
					sum[1] += v[1]
					pooled[k] = sum
				}
			}
		}
		keys := make([][2]string, 0, len(pooled))
		for k := range pooled {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b [2]string) int {
			return strings.Compare(a[0]+"/"+a[1], b[0]+"/"+b[1])
		})
		for _, k := range keys {
			v := pooled[k]
			s.ByRecipient = append(s.ByRecipient, RecipientRow{
				Arm: arm, Recipient: k[0], Category: k[1], Correct: v[0], Total: v[1],
				Rate: ratio(float64(v[0]), float64(v[1])),
			})
		}
		s.Counters = append(s.Counters, d.counterRow(arm))
	}
	return s
}

// Limits the report states every time, so a reader of one report meets them.
var standingLimits = []string{
	c7Limit,
	"A3 and A6 are one host with both record tools, so their local owner can re-read a cloud record. That makes A0 - A3 conservative. A6 is not comparable with T1 or A4, and A6 near A0 does not refute G1: it holds a cloud-sized window on one history.",
	"C3 - C0 compares the prompt, the budget and the path together. C3 - C0h has the same prompt and effort, so it compares the path alone. C7 is the same-prompt control without the prefix.",
	"A4's brief sits in Context, where compression can rewrite it. T1's summary sits in the system prompt, out of reach. A4s moves A4's local brief there too, and T1 must beat both. A brief in Context is not charged to the local summary total.",
	"The local model in A4, A4s, T1 and the C group is told how to use its record tool. T1 and the C group also get the log rules, because only they have log entries.",
	"The CI is a percentile bootstrap, which undercovers at small n. The floor of 10 pairs is what makes a gate decidable, not a claim of power.",
	"Main calls and probes send no effort field, so they use the API's default for the model.",
}
