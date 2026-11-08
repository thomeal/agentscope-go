package report

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadRequiresRealRunDirectory(t *testing.T) {
	if _, err := Load(); err == nil {
		t.Error("Load() with no directories succeeded")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "typo")); err == nil {
		t.Error("Load of an absent directory succeeded")
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("Load of a directory with neither file succeeded")
	}
}

func TestLoadStatesAMissingFile(t *testing.T) {
	dir := writeRun(t, []row{scoreRow("T1", sid(0), 0.5)}, nil)
	d := mustLoad(t, dir)
	want := []string{filepath.Join(dir, ledgerFile)}
	if !reflect.DeepEqual(d.Stats.MissingFiles, want) {
		t.Errorf("MissingFiles = %v, want %v", d.Stats.MissingFiles, want)
	}
}

func TestLoadStatesMalformedLinesAndKeepsTheRest(t *testing.T) {
	dir := writeRun(t, []row{scoreRow("T1", sid(0), 0.5)}, []row{ledgerRow("T1", sid(0), "cloud", "main", 1, 0, 0, 1, 0.01)})
	scores := filepath.Join(dir, scoresFile)
	f, err := os.OpenFile(scores, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"arm\":\"T1\",\"sess\n\n{\"arm\":\"T1\",\"retention\":0.5}\n{\"arm\":\"T1\",\"session\":\"x\",\"retention\":\"high\"}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	d := mustLoad(t, dir)
	if got := len(d.Stats.Malformed); got != 3 {
		t.Fatalf("Malformed = %d, want 3: %+v", got, d.Stats.Malformed)
	}
	// Lines 2, 4 and 5: the blank line 3 is not data.
	for i, line := range []int{2, 4, 5} {
		if d.Stats.Malformed[i].Line != line {
			t.Errorf("Malformed[%d].Line = %d, want %d", i, d.Stats.Malformed[i].Line, line)
		}
	}
	if d.Stats.ScoreRowsRead != 1 || len(d.Scores["T1"]) != 1 {
		t.Errorf("read %d rows, %d kept, want 1 and 1", d.Stats.ScoreRowsRead, len(d.Scores["T1"]))
	}
}

func TestLoadLaterRunReplacesScoresAndLedgerGroups(t *testing.T) {
	first := writeRun(t,
		[]row{scoreRow("T1", sid(0), 0.1), scoreRow("T1", sid(1), 0.2), scoreRow("A4", sid(0), 0.3)},
		[]row{
			ledgerRow("T1", sid(0), "cloud", "main", 10, 0, 0, 1, 1.0),
			ledgerRow("T1", sid(0), "cloud", "probe", 10, 0, 0, 1, 2.0),
			ledgerRow("T1", sid(0), "local", "main", 10, 0, 0, 1, 0),
			ledgerRow("T1", sid(1), "cloud", "main", 10, 0, 0, 1, 4.0),
		})
	second := writeRun(t,
		[]row{scoreRow("T1", sid(0), 0.9)},
		[]row{ledgerRow("T1", sid(0), "cloud", "main", 20, 0, 0, 2, 8.0)})

	d := mustLoad(t, first, second)
	if got := *d.Scores["T1"][sid(0)].Retention; got != 0.9 {
		t.Errorf("replaced retention = %v, want 0.9", got)
	}
	if got := *d.Scores["T1"][sid(1)].Retention; got != 0.2 {
		t.Errorf("untouched retention = %v, want 0.2", got)
	}
	if got := len(d.Ledger["T1"][sid(0)]); got != 1 {
		t.Errorf("replaced ledger group has %d rows, want 1", got)
	}
	s := d.Stats
	if s.ScoreRowsReplaced != 1 || s.LedgerRowsReplaced != 3 || s.LedgerSessionsReplaced != 1 {
		t.Errorf("replaced scores %d, ledger rows %d, groups %d; want 1, 3, 1", s.ScoreRowsReplaced, s.LedgerRowsReplaced, s.LedgerSessionsReplaced)
	}
	if !near(s.ReplacedCloudUSD, 3.0, 1e-12) {
		t.Errorf("ReplacedCloudUSD = %v, want 3 (local row excluded)", s.ReplacedCloudUSD)
	}
	if s.ScoreRowsRead != 4 || s.LedgerRowsRead != 5 {
		t.Errorf("read %d and %d, want 4 and 5", s.ScoreRowsRead, s.LedgerRowsRead)
	}
	// The arm total excludes replaced spend; the section reconciles it.
	cost := d.Cost()
	if !near(cost.TotalCloudUSD, 12.0, 1e-12) || !near(cost.ReplacedCloudUSD, 3.0, 1e-12) {
		t.Errorf("cost total %v replaced %v, want 12 and 3", cost.TotalCloudUSD, cost.ReplacedCloudUSD)
	}

	// Directory order decides, not anything inside the rows.
	reversed := mustLoad(t, second, first)
	if got := *reversed.Scores["T1"][sid(0)].Retention; got != 0.1 {
		t.Errorf("reversed retention = %v, want 0.1", got)
	}
}

func TestLoadStatesDuplicatesInsideARunApartFromReplacement(t *testing.T) {
	dir := writeRun(t, []row{scoreRow("T1", sid(0), 0.1), scoreRow("T1", sid(0), 0.7)}, []row{})
	d := mustLoad(t, dir)
	if d.Stats.ScoreDuplicatesInRun != 1 || d.Stats.ScoreRowsReplaced != 0 {
		t.Errorf("duplicates %d replaced %d, want 1 and 0", d.Stats.ScoreDuplicatesInRun, d.Stats.ScoreRowsReplaced)
	}
	if got := *d.Scores["T1"][sid(0)].Retention; got != 0.7 {
		t.Errorf("retention = %v, want the last row, 0.7", got)
	}
}

func TestArmsStateGapsBetweenScoresAndLedger(t *testing.T) {
	first := writeRun(t,
		[]row{scoreRow("T1", sid(0), 0.4), scoreRow("T1", sid(1), 0.6), scoreRow("T1", sid(2), 0.8)},
		[]row{
			ledgerRow("T1", sid(0), "cloud", "main", 1, 0, 0, 1, 0),
			ledgerRow("T1", sid(1), "cloud", "main", 1, 0, 0, 1, 0),
			ledgerRow("T1", sid(9), "cloud", "main", 1, 0, 0, 1, 0),
		})
	// A crashed re-run: new ledger rows for s000, no score row.
	second := writeRun(t, []row{}, []row{ledgerRow("T1", sid(0), "cloud", "main", 2, 0, 0, 2, 0)})
	arms := mustLoad(t, first, second).Arms()
	if len(arms) != 1 {
		t.Fatalf("arms = %d, want 1", len(arms))
	}
	a := arms[0]
	if a.Sessions != 3 || a.LedgerSessions != 3 {
		t.Errorf("sessions %d ledger %d, want 3 and 3", a.Sessions, a.LedgerSessions)
	}
	if a.ScoredWithoutLedger != 1 || a.LedgerWithoutScore != 1 || a.SourceRunDiffers != 1 {
		t.Errorf("scored-no-ledger %d, ledger-no-score %d, run differs %d; want 1, 1, 1", a.ScoredWithoutLedger, a.LedgerWithoutScore, a.SourceRunDiffers)
	}
	wantNear(t, "RetentionMean", a.RetentionMean, 0.6, 1e-12)
}

func TestArmsDoNotTreatAbsentValuesAsZero(t *testing.T) {
	bare := row{"arm": "T1", "session": sid(1), "error": "boom"}
	d := mustLoad(t, writeRun(t, []row{scoreRow("T1", sid(0), 0.5), bare}, []row{}))
	a := d.Arms()[0]
	wantNear(t, "RetentionMean", a.RetentionMean, 0.5, 1e-12)
	if a.RetentionN != 1 || a.RetentionMissing != 1 || a.OwnRetentionMissing != 1 || a.ExposureMissing != 1 || a.ErrorRows != 1 {
		t.Errorf("unexpected counts: %+v", a)
	}
}

// TestLedgerRowMatchesLabRow reads lab.Row from source, so the report keeps
// compiling without the transport layer while a change to the row still fails here.
func TestLedgerRowMatchesLabRow(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "lab", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	type field struct{ tag, typ string }
	var lab []field
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || spec.Name.Name != "Row" {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, f := range st.Fields.List {
				tag := reflect.StructTag(strings.Trim(f.Tag.Value, "`")).Get("json")
				lab = append(lab, field{tag, exprString(f.Type)})
			}
			return false
		})
	}
	if len(lab) == 0 {
		t.Fatal("lab.Row not found in ../lab")
	}
	var mine []field
	rt := reflect.TypeFor[LedgerRow]()
	for i := range rt.NumField() {
		f := rt.Field(i)
		mine = append(mine, field{f.Tag.Get("json"), f.Type.String()})
	}
	if !reflect.DeepEqual(lab, mine) {
		t.Errorf("LedgerRow drifted from lab.Row:\n lab  %v\n mine %v", lab, mine)
	}
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	}
	return "?"
}
