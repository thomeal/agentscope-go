package report

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	scoresFile = "scores.jsonl"
	ledgerFile = "ledger.jsonl"

	sideCloud = "cloud"
	sideLocal = "local"
)

// Score is one row of scores.jsonl. Fields the gates depend on are pointers so
// that an absent or null value stays distinguishable from a real zero: missing
// data has to surface as a stated count, never as a silent zero.
type Score struct {
	Arm           string          `json:"arm"`
	Session       string          `json:"session"`
	Set           string          `json:"set"`
	Retention     *float64        `json:"retention"`
	OwnRetention  *float64        `json:"own_retention"`
	ExposureSeen  *int            `json:"exposure_seen"`
	ExposureTotal *int            `json:"exposure_total"`
	Counters      map[string]*int `json:"counters"`
	CanaryHits    *int            `json:"canary_hits"`
	Error         string          `json:"error"`
	// ByCategory holds [correct, total] per category, and ByRecipient the same per
	// answering side. S2 is set on rows of S2 tasks.
	ByCategory  map[string][2]int            `json:"by_category"`
	ByRecipient map[string]map[string][2]int `json:"by_recipient"`
	S2          *S2Outcome                   `json:"s2"`
	// ImputedMainUSD prices the scripted steps of a C-group row as main calls.
	ImputedMainUSD float64       `json:"imputed_main_usd"`
	Route          *RouteOutcome `json:"route"`
}

// S2Outcome is what the hidden tests said. A nonempty Infra means the scorer failed.
type S2Outcome struct {
	Passed      int      `json:"passed"`
	Total       int      `json:"total"`
	BuildFailed bool     `json:"build_failed"`
	Failed      []string `json:"failed"`
	Infra       string   `json:"infra"`
}

func (s Score) counter(name string) (int, bool) {
	v, ok := s.Counters[name]
	if !ok || v == nil {
		return 0, false
	}
	return *v, true
}

// LedgerRow mirrors lab.Row field for field. The report keeps its own copy so it
// compiles without the transport layer and its dependencies; a test compares this
// struct with the source of lab.Row to catch drift.
type LedgerRow struct {
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

// BadLine is a line that could not be used, kept so the report can state it.
type BadLine struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// LoadStats states what Load read, replaced and could not use.
type LoadStats struct {
	ScoreRowsRead  int `json:"score_rows_read"`
	LedgerRowsRead int `json:"ledger_rows_read"`
	// ScoreRowsReplaced counts score rows a later run directory replaced.
	ScoreRowsReplaced int `json:"score_rows_replaced"`
	// LedgerRowsReplaced counts ledger rows belonging to an arm and session that
	// a later run directory replaced as a group; LedgerSessionsReplaced counts
	// those groups.
	LedgerRowsReplaced     int `json:"ledger_rows_replaced"`
	LedgerSessionsReplaced int `json:"ledger_sessions_replaced"`
	// ReplacedCloudUSD is real money spent on replaced ledger rows. It is left out
	// of the arm totals, so it is what reconciles them with the actual spend.
	ReplacedCloudUSD float64 `json:"replaced_cloud_usd"`
	// ScoreDuplicatesInRun counts repeated keys inside one scores.jsonl. The last
	// row wins. It is not an explicit re-run, so it is not counted as replaced.
	ScoreDuplicatesInRun int       `json:"score_duplicates_in_run"`
	MissingFiles         []string  `json:"missing_files"`
	Malformed            []BadLine `json:"malformed"`
}

type sessionKey struct{ arm, session string }

func compareKeys(a, b sessionKey) int {
	return cmp.Or(cmp.Compare(a.arm, b.arm), cmp.Compare(a.session, b.session))
}

// Data holds the rows that survive replacement, indexed by arm and then session.
type Data struct {
	RunDirs []string
	Scores  map[string]map[string]Score
	Ledger  map[string]map[string][]LedgerRow
	Stats   LoadStats
	// Manifests are what the runs state about themselves, and NoManifest the runs that
	// state nothing.
	Manifests  []Manifest
	NoManifest []string

	scoreRun       map[sessionKey]int
	ledgerRun      map[sessionKey]int
	scoreReplaced  map[string]int
	ledgerReplaced map[string]int
	replacedHits   map[string]int
}

// Load reads scores.jsonl and ledger.jsonl from each run directory in order. A
// later directory replaces an earlier row with the same arm and session. A ledger
// holds many rows per session, so its replacement is by group: the later run's
// rows take the place of all the earlier run's rows for that arm and session.
//
// A directory that cannot be read, or that has neither file, is an error. A
// directory with one of the two files, and a line that cannot be parsed, are
// stated in Stats and the load continues.
func Load(runDirs ...string) (*Data, error) {
	if len(runDirs) == 0 {
		return nil, errors.New("report: Load needs at least one run directory")
	}
	d := &Data{
		RunDirs:        slices.Clone(runDirs),
		Scores:         map[string]map[string]Score{},
		Ledger:         map[string]map[string][]LedgerRow{},
		scoreRun:       map[sessionKey]int{},
		ledgerRun:      map[sessionKey]int{},
		scoreReplaced:  map[string]int{},
		ledgerReplaced: map[string]int{},
		replacedHits:   map[string]int{},
		Stats:          LoadStats{MissingFiles: []string{}, Malformed: []BadLine{}},
	}
	for run, dir := range runDirs {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("report: run directory: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("report: run directory %s is not a directory", dir)
		}
		scorePath := filepath.Join(dir, scoresFile)
		ledgerPath := filepath.Join(dir, ledgerFile)
		hasScores, err := d.loadScores(scorePath, run)
		if err != nil {
			return nil, err
		}
		hasLedger, err := d.loadLedger(ledgerPath, run)
		if err != nil {
			return nil, err
		}
		if !hasScores && !hasLedger {
			return nil, fmt.Errorf("report: %s has neither %s nor %s", dir, scoresFile, ledgerFile)
		}
		if err := d.loadManifest(dir); err != nil {
			return nil, err
		}
		if !hasScores {
			d.Stats.MissingFiles = append(d.Stats.MissingFiles, scorePath)
		}
		if !hasLedger {
			d.Stats.MissingFiles = append(d.Stats.MissingFiles, ledgerPath)
		}
	}
	return d, nil
}

func readLines(path string) ([][]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("report: read %s: %w", path, err)
	}
	return bytes.Split(raw, []byte("\n")), true, nil
}

func (d *Data) bad(path string, line int, reason string) {
	d.Stats.Malformed = append(d.Stats.Malformed, BadLine{File: path, Line: line, Reason: reason})
}

func (d *Data) loadScores(path string, run int) (bool, error) {
	lines, ok, err := readLines(path)
	if err != nil || !ok {
		return ok, err
	}
	thisRun := map[sessionKey]Score{}
	for i, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var s Score
		if err := json.Unmarshal(line, &s); err != nil {
			d.bad(path, i+1, err.Error())
			continue
		}
		if s.Arm == "" || s.Session == "" {
			d.bad(path, i+1, "score row has no arm or no session")
			continue
		}
		d.Stats.ScoreRowsRead++
		k := sessionKey{s.Arm, s.Session}
		if _, dup := thisRun[k]; dup {
			d.Stats.ScoreDuplicatesInRun++
		}
		thisRun[k] = s
	}
	for k, s := range thisRun {
		if _, replaced := d.scoreRun[k]; replaced {
			d.Stats.ScoreRowsReplaced++
			d.scoreReplaced[k.arm]++
			if old := d.Scores[k.arm][k.session].CanaryHits; old != nil {
				d.replacedHits[k.arm] += *old
			}
		}
		if d.Scores[k.arm] == nil {
			d.Scores[k.arm] = map[string]Score{}
		}
		d.Scores[k.arm][k.session] = s
		d.scoreRun[k] = run
	}
	return true, nil
}

func (d *Data) loadLedger(path string, run int) (bool, error) {
	lines, ok, err := readLines(path)
	if err != nil || !ok {
		return ok, err
	}
	thisRun := map[sessionKey][]LedgerRow{}
	for i, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var r LedgerRow
		if err := json.Unmarshal(line, &r); err != nil {
			d.bad(path, i+1, err.Error())
			continue
		}
		if r.Arm == "" || r.Session == "" {
			d.bad(path, i+1, fmt.Sprintf("ledger row has no arm or no session (side %q, cost_usd %g)", r.Side, r.CostUSD))
			continue
		}
		d.Stats.LedgerRowsRead++
		k := sessionKey{r.Arm, r.Session}
		thisRun[k] = append(thisRun[k], r)
	}
	// Sorted, so the replaced USD sum does not depend on map order.
	for _, k := range slices.SortedFunc(maps.Keys(thisRun), compareKeys) {
		if _, replaced := d.ledgerRun[k]; replaced {
			old := d.Ledger[k.arm][k.session]
			d.Stats.LedgerSessionsReplaced++
			d.Stats.LedgerRowsReplaced += len(old)
			d.ledgerReplaced[k.arm] += len(old)
			for _, r := range old {
				if r.Side == sideCloud {
					d.Stats.ReplacedCloudUSD += r.CostUSD
				}
				d.replacedHits[k.arm] += r.CanaryHits
			}
		}
		if d.Ledger[k.arm] == nil {
			d.Ledger[k.arm] = map[string][]LedgerRow{}
		}
		d.Ledger[k.arm][k.session] = thisRun[k]
		d.ledgerRun[k] = run
	}
	return true, nil
}

// armNames returns every arm seen in either file, sorted.
func (d *Data) armNames() []string {
	set := map[string]bool{}
	for arm := range d.Scores {
		set[arm] = true
	}
	for arm := range d.Ledger {
		set[arm] = true
	}
	return slices.Sorted(maps.Keys(set))
}

// ArmSummary states what each arm has, including what it lacks. Exposure is
// reported here and never used to filter a row.
type ArmSummary struct {
	Arm                 string   `json:"arm"`
	Sessions            int      `json:"sessions"`
	Sets                []string `json:"sets"`
	RetentionMean       *float64 `json:"retention_mean"`
	RetentionN          int      `json:"retention_n"`
	RetentionMissing    int      `json:"retention_missing"`
	OwnRetentionMean    *float64 `json:"own_retention_mean"`
	OwnRetentionN       int      `json:"own_retention_n"`
	OwnRetentionMissing int      `json:"own_retention_missing"`
	ErrorRows           int      `json:"error_rows"`
	ExposureSeen        int      `json:"exposure_seen"`
	ExposureTotal       int      `json:"exposure_total"`
	ExposureMissing     int      `json:"exposure_missing"`
	LedgerSessions      int      `json:"ledger_sessions"`
	// ScoredWithoutLedger and LedgerWithoutScore count sessions present in only
	// one of the two files. SourceRunDiffers counts sessions whose score and
	// ledger rows came from different run directories after replacement.
	ScoredWithoutLedger int `json:"scored_without_ledger"`
	LedgerWithoutScore  int `json:"ledger_without_score"`
	SourceRunDiffers    int `json:"source_run_differs"`
	ScoresReplaced      int `json:"scores_replaced"`
	LedgerRowsReplaced  int `json:"ledger_rows_replaced"`
}

// Arms summarizes every arm seen in either file, sorted by name.
func (d *Data) Arms() []ArmSummary {
	out := []ArmSummary{}
	for _, arm := range d.armNames() {
		scores, ledger := d.Scores[arm], d.Ledger[arm]
		a := ArmSummary{
			Arm:                arm,
			Sessions:           len(scores),
			LedgerSessions:     len(ledger),
			ScoresReplaced:     d.scoreReplaced[arm],
			LedgerRowsReplaced: d.ledgerReplaced[arm],
		}
		sets := map[string]bool{}
		var retention, own []float64
		for _, id := range slices.Sorted(maps.Keys(scores)) {
			s := scores[id]
			if s.Set != "" {
				sets[s.Set] = true
			}
			if s.Retention != nil {
				retention = append(retention, *s.Retention)
			} else {
				a.RetentionMissing++
			}
			if s.OwnRetention != nil {
				own = append(own, *s.OwnRetention)
			} else {
				a.OwnRetentionMissing++
			}
			if s.Error != "" {
				a.ErrorRows++
			}
			if s.ExposureSeen != nil && s.ExposureTotal != nil {
				a.ExposureSeen += *s.ExposureSeen
				a.ExposureTotal += *s.ExposureTotal
			} else {
				a.ExposureMissing++
			}
			if _, ok := ledger[id]; !ok {
				a.ScoredWithoutLedger++
			} else if d.scoreRun[sessionKey{arm, id}] != d.ledgerRun[sessionKey{arm, id}] {
				a.SourceRunDiffers++
			}
		}
		for id := range ledger {
			if _, ok := scores[id]; !ok {
				a.LedgerWithoutScore++
			}
		}
		a.Sets = slices.Sorted(maps.Keys(sets))
		if a.Sets == nil {
			a.Sets = []string{}
		}
		a.RetentionN, a.OwnRetentionN = len(retention), len(own)
		a.RetentionMean, a.OwnRetentionMean = meanOf(retention), meanOf(own)
		out = append(out, a)
	}
	return out
}
