package report

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type row = map[string]any

const seed = int64(42)

func sid(i int) string { return fmt.Sprintf("s%03d", i) }

// scoreRow is a complete row in the shape HARNESS.md defines.
func scoreRow(arm, session string, retention float64) row {
	return row{
		"arm": arm, "session": session, "set": "tuning", "length": 20,
		"retention": retention, "own_retention": retention,
		"by_category": row{"F1": []int{8, 10}}, "f6_correct": 4, "f6_total": 5,
		"stale": 1, "parse_failures": 0, "batches": 4, "questions": 38,
		"exposure_seen": 30, "exposure_total": 32,
		"counters": row{
			"clears": 2, "keeps": 1, "fork_b": 2, "fork_failed": 0, "compress": 3,
			"overflow_recoveries": 0, "lossy_events": 0, "session_errors": 0,
		},
		"canary_hits": 0, "seconds": 412.5, "cost_usd": 0.21, "error": "",
	}
}

// armRows gives n sessions a retention from f.
func armRows(arm string, n int, f func(i int) float64) []row {
	out := make([]row, 0, n)
	for i := range n {
		out = append(out, scoreRow(arm, sid(i), f(i)))
	}
	return out
}

// taskRows is armRows for an S2 task, whose retention is its hidden-test score.
func taskRows(arm string, n int, f func(i int) float64) []row {
	rows := armRows(arm, n, f)
	for _, r := range rows {
		r["set"], r["s2"] = "s2", row{"passed": 1, "total": 1}
	}
	return rows
}

func constant(v float64) func(int) float64 { return func(int) float64 { return v } }

func ledgerRow(arm, session, side, role string, input, cached, write, output int, usd float64) row {
	return row{
		"ts": "2026-11-06T00:00:00Z", "run": "r", "arm": arm, "session": session, "side": side, "role": role,
		"model": "m", "http": 200, "status": "completed",
		"input": input, "cached": cached, "write": write, "output": output, "reasoning": 0,
		"wait_ms": 0, "latency_ms": 0, "cost_usd": usd, "attempt": 1,
	}
}

func writeJSONL(t *testing.T, path string, rows []row) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
}

// writeRun writes a run directory with both files and a manifest; a nil slice leaves a
// file out.
func writeRun(t *testing.T, scores, ledger []row) string {
	t.Helper()
	dir := writeBareRun(t, scores, ledger)
	raw, err := json.Marshal(Manifest{Run: filepath.Base(dir), Set: "test", Budgets: map[string]any{"fork_b_target": 2500}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeBareRun is writeRun with no manifest.
func writeBareRun(t *testing.T, scores, ledger []row) string {
	t.Helper()
	dir := t.TempDir()
	if scores != nil {
		writeJSONL(t, filepath.Join(dir, scoresFile), scores)
	}
	if ledger != nil {
		writeJSONL(t, filepath.Join(dir, ledgerFile), ledger)
	}
	return dir
}

func mustLoad(t *testing.T, dirs ...string) *Data {
	t.Helper()
	d, err := Load(dirs...)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func loadScores(t *testing.T, groups ...[]row) *Data {
	t.Helper()
	var all []row
	for _, g := range groups {
		all = append(all, g...)
	}
	return mustLoad(t, writeRun(t, all, []row{}))
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func wantNear(t *testing.T, name string, got *float64, want, tol float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %v", name, want)
		return
	}
	if !near(*got, want, tol) {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func wantVerdict(t *testing.T, name string, got, want Verdict, reason string) {
	t.Helper()
	if got != want {
		t.Errorf("%s verdict = %s, want %s (reason: %s)", name, got, want, reason)
	}
}
