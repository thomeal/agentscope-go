package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// maxListedSessions caps session ids printed per list; report.json has them all.
const maxListedSessions = 10

// fnum formats an optional value. This is the only place numbers are rounded.
func fnum(p *float64, prec int) string {
	if p == nil {
		return "n/a"
	}
	return strconv.FormatFloat(*p, 'f', prec, 64)
}

func inum(p *int) string {
	if p == nil {
		return "n/a"
	}
	return strconv.Itoa(*p)
}

func ciText(lo, hi *float64) string {
	if lo == nil || hi == nil {
		return "n/a"
	}
	return "[" + fnum(lo, 4) + ", " + fnum(hi, 4) + "]"
}

func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

func table(b *strings.Builder, header []string, rows [][]string) {
	line := func(cells []string) {
		b.WriteString("|")
		for _, c := range cells {
			b.WriteString(" " + cell(c) + " |")
		}
		b.WriteString("\n")
	}
	line(header)
	rule := make([]string, len(header))
	for i := range rule {
		rule[i] = "---"
	}
	line(rule)
	for _, r := range rows {
		line(r)
	}
	b.WriteString("\n")
}

func itoa(n int) string { return strconv.Itoa(n) }

// Markdown renders the report as plain Markdown tables.
func Markdown(w io.Writer, r *Report) error {
	var b strings.Builder
	b.WriteString("# Stage 0 report\n\n")
	fmt.Fprintf(&b, "Run directories, later replacing earlier: %s\n\n", strings.Join(r.RunDirs, ", "))
	level := "session"
	switch {
	case r.S2 != nil:
		level = "task"
	case r.Route != nil:
		level = "seed"
	}
	fmt.Fprintf(&b, "Bootstrap: percentile, %s level, %d resamples, seed %d. Differences are A - B. Values are rounded for display only; report.json has full precision.\n\n", level, r.Resamples, r.Seed)
	if len(r.NonPrimary) > 0 {
		fmt.Fprintf(&b, "**Not a primary result.** %s used budgets other than the frozen ones or uncommitted code, so these verdicts answer a different question from the gates.\n\n", strings.Join(r.NonPrimary, ", "))
	}
	writeManifests(&b, r)
	if r.Route != nil {
		writeLoad(&b, r)
		writeRoute(&b, r)
		writeCost(&b, r)
		writeLimits(&b, r)
		_, err := io.WriteString(w, b.String())
		return err
	}
	if r.S2 != nil {
		b.WriteString("## Verdicts\n\n")
		table(&b, []string{"Section", "Verdict", "Reason"}, [][]string{
			{r.G2.Gate, string(r.G2.Verdict), r.G2.Reason},
			{r.G2Read.Gate, string(r.G2Read.Verdict), r.G2Read.Reason},
		})
		if r.S2.Set == setS2 {
			b.WriteString("This is a pilot verdict: these tasks were available during tuning.\n\n")
		}
		b.WriteString("G2 reads T1. If T1 falls short and T1 with the event list clears the same terms against A4, A4s and A4+read, the plan keeps Main Thread and the list becomes part of T1. That is two tries at one gate, which raises the chance of a false pass, so acceptance runs only the form that passed.\n\n")
		fmt.Fprintf(&b, "%s.\n\n", r.SampleSize.Statement)
		writeLoad(&b, r)
		writeArms(&b, r)
		writeGate(&b, *r.G2, "Recorded rows, no effect on the verdict")
		writeGate(&b, *r.G2Read, "Recorded rows, no effect on the verdict")
		writeS2(&b, r)
		writeCost(&b, r)
		writeSampleSize(&b, r)
		writeLimits(&b, r)
		_, err := io.WriteString(w, b.String())
		return err
	}

	b.WriteString("## Verdicts\n\n")
	table(&b, []string{"Section", "Verdict", "Reason"}, [][]string{
		{"G1", string(r.G1.Verdict), r.G1.Reason},
		{"G6", string(r.G6.Verdict), r.G6.Reason},
		{"Privacy", string(r.Privacy.Verdict), r.Privacy.Reason},
	})
	b.WriteString("G2 is judged on S2's hidden tests, in the report of the S2 runs. The rows here on T1 are a diagnostic.\n\n")
	fmt.Fprintf(&b, "%s.\n\n", r.SampleSize.Statement)

	writeLoad(&b, r)
	writeArms(&b, r)
	writeGate(&b, r.G1, "Recorded rows, no effect on the verdict")
	writeGate(&b, r.G6, "Recorded rows, no effect on the verdict")
	writeG6Extras(&b, r)
	writeSecondary(&b, r)
	writeCost(&b, r)
	writePrivacy(&b, r)
	writeSampleSize(&b, r)
	writeLimits(&b, r)
	_, err := io.WriteString(w, b.String())
	return err
}

func writeLimits(b *strings.Builder, r *Report) {
	b.WriteString("## Limits\n\n")
	for _, l := range r.Limits {
		fmt.Fprintf(b, "- %s\n", l)
	}
}

func writeManifests(b *strings.Builder, r *Report) {
	if len(r.Manifests) == 0 {
		return
	}
	rows := [][]string{}
	for _, m := range r.Manifests {
		rows = append(rows, []string{m.Run, m.Set, m.Commit})
	}
	table(b, []string{"Run", "Set", "Commit"}, rows)
	if budgets, err := json.Marshal(r.Manifests[0].Budgets); err == nil {
		fmt.Fprintf(b, "Budgets, the same in every run with a manifest: `%s`\n\n", budgets)
	}
}

func writeS2(b *strings.Builder, r *Report) {
	b.WriteString("## S2 by arm\n\nHidden-test scores on the shared workspace. A scorer failure leaves a null score and is not a zero.\n\n")
	rows := [][]string{}
	for _, a := range r.S2.Arms {
		rows = append(rows, []string{a.Arm, itoa(a.Tasks), itoa(a.Runs), itoa(a.Scored), fnum(a.Mean, 4), fnum(a.AllPass, 4), itoa(a.BuildFailed), itoa(a.ScorerFailed), itoa(a.SessionErrors)})
	}
	table(b, []string{"Arm", "Tasks", "Runs", "Scored", "Mean score", "All pass", "Build failed", "Scorer failed", "Session errors"}, rows)
	b.WriteString("G1's comparison on the core metric. G1 stays as S1 decided it. A difference below 0.10 here means that the problem S1 showed does not show on tasks, which the plan has to answer.\n\n")
	comparisonTable(b, r.S2.Direction)
	b.WriteString("Counters, which say whether a mechanism ran at all. An unused event list is not the same as a useless one.\n\n")
	counterTable(b, r.S2.Counters)
}

func writeSecondary(b *strings.Builder, r *Report) {
	s := r.Secondary
	b.WriteString("## Secondary analyses\n\nRegistered in prereg.md. None of these decides a verdict.\n\n")
	comparisonTable(b, s.Comparisons)
	b.WriteString("### T1 on facts\n\nS1 shows which facts a path loses. Since Amendment 3 it decides nothing about G2.\n\n")
	comparisonTable(b, s.Thread)
	b.WriteString("### C group without F3\n\nF1, F2, F4 and F5, because F3 has a floor in CBench.\n\n")
	comparisonTable(b, s.WithoutF3)
	b.WriteString("### Without the sessions used for tuning\n\n")
	comparisonTable(b, s.ExcludingTuning)
	b.WriteString("### Retention by answering side and category\n\n")
	rows := [][]string{}
	for _, x := range s.ByRecipient {
		rows = append(rows, []string{x.Arm, x.Recipient, x.Category, fmt.Sprintf("%d/%d", x.Correct, x.Total), fnum(x.Rate, 4)})
	}
	table(b, []string{"Arm", "Side", "Category", "Correct/total", "Rate"}, rows)
	b.WriteString("### Counters\n\n")
	counterTable(b, s.Counters)
}

func counterTable(b *strings.Builder, counters []CounterRow) {
	header := append([]string{"Arm"}, explainingCounters...)
	rows := [][]string{}
	for _, c := range counters {
		row := []string{c.Arm}
		for _, name := range explainingCounters {
			row = append(row, itoa(c.Totals[name]))
		}
		rows = append(rows, row)
	}
	table(b, header, rows)
}

func writeLoad(b *strings.Builder, r *Report) {
	l := r.Load
	b.WriteString("## Data\n\n")
	table(b, []string{"Item", "Count"}, [][]string{
		{"Score rows read", itoa(l.ScoreRowsRead)},
		{"Ledger rows read", itoa(l.LedgerRowsRead)},
		{"Score rows replaced by a later run", itoa(l.ScoreRowsReplaced)},
		{"Ledger rows replaced by a later run", itoa(l.LedgerRowsReplaced)},
		{"Ledger arm and session groups replaced", itoa(l.LedgerSessionsReplaced)},
		{"Cloud USD spent on replaced ledger rows", strconv.FormatFloat(l.ReplacedCloudUSD, 'f', 4, 64)},
		{"Repeated score keys inside one run (last row kept)", itoa(l.ScoreDuplicatesInRun)},
		{"Missing files", itoa(len(l.MissingFiles))},
		{"Malformed lines", itoa(len(l.Malformed))},
	})
	for _, f := range l.MissingFiles {
		fmt.Fprintf(b, "- Missing file: %s\n", f)
	}
	for _, m := range l.Malformed {
		fmt.Fprintf(b, "- Malformed: %s:%d: %s\n", m.File, m.Line, cell(m.Reason))
	}
	if len(l.MissingFiles)+len(l.Malformed) > 0 {
		b.WriteString("\n")
	}
}

func writeArms(b *strings.Builder, r *Report) {
	b.WriteString("## Arms\n\n")
	rows := [][]string{}
	for _, a := range r.Arms {
		rows = append(rows, []string{
			a.Arm, itoa(a.Sessions), strings.Join(a.Sets, ", "),
			fmt.Sprintf("%s (n=%d)", fnum(a.RetentionMean, 4), a.RetentionN), itoa(a.RetentionMissing),
			fmt.Sprintf("%s (n=%d)", fnum(a.OwnRetentionMean, 4), a.OwnRetentionN), itoa(a.OwnRetentionMissing),
			itoa(a.ErrorRows),
			fmt.Sprintf("%d/%d", a.ExposureSeen, a.ExposureTotal), itoa(a.ExposureMissing),
			itoa(a.LedgerSessions), itoa(a.ScoredWithoutLedger), itoa(a.LedgerWithoutScore), itoa(a.SourceRunDiffers),
			itoa(a.ScoresReplaced), itoa(a.LedgerRowsReplaced),
		})
	}
	table(b, []string{
		"Arm", "Sessions", "Sets", "Retention mean", "Retention missing", "Own retention mean", "Own missing",
		"Error rows", "Exposure seen/total", "Exposure missing", "Ledger sessions", "Scored, no ledger",
		"Ledger, no score", "Source run differs", "Scores replaced", "Ledger rows replaced",
	}, rows)
}

// nText is the number of pairs, with the rows behind them when a task ran more than once.
func nText(p PairedResult) string {
	if p.Rows > p.N {
		return fmt.Sprintf("%d (%d rows)", p.N, p.Rows)
	}
	return itoa(p.N)
}

func comparisonTable(b *strings.Builder, ps []PairedResult) {
	rows := [][]string{}
	for _, p := range ps {
		rows = append(rows, []string{
			p.Label(), p.Metric, nText(p), fnum(p.Mean, 4), fnum(p.SD, 4), ciText(p.CILow, p.CIHigh),
			itoa(len(p.MissingFromA)), itoa(len(p.MissingFromB)),
			itoa(len(p.MetricMissingA)), itoa(len(p.MetricMissingB)),
			itoa(p.ErrorsA), itoa(p.ErrorsB),
		})
	}
	table(b, []string{
		"Comparison", "Metric", "n", "Mean diff", "SD diff", "95% CI",
		"Missing from A", "Missing from B", "Metric missing A", "Metric missing B", "Errors A", "Errors B",
	}, rows)
	for _, p := range ps {
		listSessions(b, p, "missing from "+p.A, p.MissingFromA)
		listSessions(b, p, "missing from "+p.B, p.MissingFromB)
		listSessions(b, p, p.Metric+" missing in "+p.A, p.MetricMissingA)
		listSessions(b, p, p.Metric+" missing in "+p.B, p.MetricMissingB)
	}
	if hasUnpaired(ps) {
		b.WriteString("\n")
	}
}

func hasUnpaired(ps []PairedResult) bool {
	for _, p := range ps {
		if p.Unpaired() > 0 {
			return true
		}
	}
	return false
}

func listSessions(b *strings.Builder, p PairedResult, what string, ids []string) {
	if len(ids) == 0 {
		return
	}
	shown := ids[:min(len(ids), maxListedSessions)]
	fmt.Fprintf(b, "- %s, %s: %d: %s", p.Label(), what, len(ids), strings.Join(shown, ", "))
	if rest := len(ids) - len(shown); rest > 0 {
		fmt.Fprintf(b, " and %d more", rest)
	}
	b.WriteString("\n")
}

func writeGate(b *strings.Builder, g GateResult, recordedTitle string) {
	fmt.Fprintf(b, "## %s: %s\n\n%s.\n\n", g.Gate, g.Verdict, g.Reason)
	rows := [][]string{}
	var compared []PairedResult
	for _, c := range g.Conditions {
		rows = append(rows, []string{c.Name, string(c.Verdict), fnum(c.Value, 4), strconv.FormatFloat(c.Threshold, 'f', 2, 64), c.Reason})
		if c.Paired != nil {
			compared = append(compared, *c.Paired)
		}
	}
	table(b, []string{"Condition", "Verdict", "Value", "Threshold", "Reason"}, rows)
	if len(compared) > 0 {
		comparisonTable(b, compared)
	}
	if len(g.Recorded) > 0 {
		fmt.Fprintf(b, "%s:\n\n", recordedTitle)
		comparisonTable(b, g.Recorded)
	}
}

func writeG6Extras(b *strings.Builder, r *Report) {
	g := r.G6
	if t := g.LossyEvents; t != nil {
		fmt.Fprintf(b, "%s %s: total %d over %d rows, %d rows lack the counter.\n\n", t.Arm, t.Counter, t.Total, t.Rows, t.MissingRows)
	}
	if f := g.ForkCache; f != nil {
		b.WriteString("Fork cache on cloud fork_a, fork_b and fork_merged rows, cached / (cached + write + uncached):\n\n")
		table(b, []string{"Arm", "Rows", "Cached", "Write", "Uncached", "Rate", "Usage missing", "Inconsistent usage"}, [][]string{{
			f.Arm, itoa(f.Calls), itoa(f.Cached), itoa(f.Write), itoa(f.Uncached), fnum(f.Rate, 4), itoa(f.UsageMissing), itoa(f.Inconsistent),
		}})
	}
	b.WriteString("Other summary methods against C3, no effect on the verdict:\n\n")
	rows := [][]string{}
	for _, a := range g.Alternatives {
		row := []string{a.Arm, "reference", "", "", "", ""}
		if a.Paired != nil {
			p := a.Paired
			row = []string{a.Arm, p.Label(), itoa(p.N), fnum(p.Mean, 4), fnum(p.SD, 4), ciText(p.CILow, p.CIHigh)}
		}
		rows = append(rows, append(row, itoa(a.LedgerRows), fnum(a.CloudUSD, 4), fnum(a.CloudUSDPerSession, 4), fnum(a.SummaryUSD, 4), fnum(a.ForkUSD, 4), fnum(a.ForkCacheRate, 4)))
	}
	table(b, []string{"Arm", "Comparison", "n", "Mean diff", "SD diff", "95% CI", "Ledger rows", "Cloud USD", "USD/session", "Summary USD", "Fork and shorten USD", "Fork cache rate"}, rows)
	fmt.Fprintf(b, "Limit: %s\n\n", c7Limit)
}

func writeCost(b *strings.Builder, r *Report) {
	b.WriteString("## Cost\n\n")
	fmt.Fprintf(b, "Computed from the ledger. Cloud USD on surviving rows: %.4f. On rows replaced by a later run: %.4f. Sessions per arm are the distinct sessions in its ledger.\n\n",
		r.Cost.TotalCloudUSD, r.Cost.ReplacedCloudUSD)

	b.WriteString("### Cloud spend by arm\n\n")
	rows := [][]string{}
	for _, c := range r.Cost.Arms {
		rows = append(rows, []string{
			c.Arm, itoa(c.LedgerSessions), itoa(c.ScoredSessions), itoa(c.Rows),
			fnum(c.CloudUSD, 4), fnum(c.CloudUSDPerSession, 4), fnum(c.ForkUSD, 4), fnum(c.MainUSD, 4),
			fnum(c.ForkToMain, 4), fnum(c.ForkShare, 4),
		})
	}
	table(b, []string{"Arm", "Ledger sessions", "Scored sessions", "Rows", "Cloud USD", "USD/session", "Fork USD", "Main USD", "Fork/main", "Fork share of fork+main"}, rows)

	b.WriteString("### Spend and cache by role\n\n")
	rows = [][]string{}
	for _, c := range r.Cost.Arms {
		for _, rc := range c.ByRole {
			rows = append(rows, []string{
				c.Arm, rc.Side, rc.Role, itoa(rc.Calls),
				strconv.FormatFloat(rc.USD, 'f', 4, 64), fnum(rc.USDPerSession, 4), fnum(rc.CacheHitRate, 4),
				itoa(rc.Input), itoa(rc.Cached), itoa(rc.Write), itoa(rc.Uncached), itoa(rc.Output), itoa(rc.Reasoning), itoa(rc.UsageMissing),
				fnum(rc.LatencyP50, 1), fnum(rc.LatencyP95, 1),
			})
		}
	}
	table(b, []string{"Arm", "Side", "Role", "Calls", "USD", "USD/session", "Cache hit rate", "Input", "Cached", "Write", "Uncached", "Output", "Reasoning", "Usage missing", "Latency P50 ms", "Latency P95 ms"}, rows)

	b.WriteString("### Tokens, latency and wait by side\n\n")
	rows = [][]string{}
	side := func(arm, name string, s SideStats) []string {
		return []string{
			arm, name, itoa(s.Calls), itoa(s.Input), itoa(s.Cached), itoa(s.Write), itoa(s.Uncached), itoa(s.Output), itoa(s.Reasoning),
			fnum(s.CacheHitRate, 4), fnum(s.ReasoningShare, 4),
			fnum(s.LatencyP50, 1), fnum(s.LatencyP95, 1), fnum(s.WaitP50, 1), fnum(s.WaitP95, 1),
		}
	}
	for _, c := range r.Cost.Arms {
		rows = append(rows, side(c.Arm, sideCloud, c.Cloud), side(c.Arm, sideLocal, c.Local))
	}
	table(b, []string{"Arm", "Side", "Calls", "Input", "Cached", "Write", "Uncached", "Output", "Reasoning", "Cache hit rate", "Reasoning share", "Latency P50 ms", "Latency P95 ms", "Wait P50 ms", "Wait P95 ms"}, rows)

	b.WriteString("### Rows needing attention\n\n")
	rows = [][]string{}
	for _, c := range r.Cost.Arms {
		rows = append(rows, []string{
			c.Arm, itoa(c.UsageMissing), itoa(c.IncompleteRows), itoa(c.LocalLengthRows), itoa(c.RetriedRows), itoa(c.OverflowRows),
			itoa(c.ErrorRows), itoa(c.Cloud.Inconsistent + c.Local.Inconsistent), itoa(c.UnknownSideRows),
		})
	}
	table(b, []string{"Arm", "Usage missing", "Incomplete status", "Local finish length", "Retried (attempt > 1)", "Overflow", "Errors", "Inconsistent usage", "Unknown side"}, rows)
}

func writePrivacy(b *strings.Builder, r *Report) {
	fmt.Fprintf(b, "## Privacy: %s\n\n%s.\n\n", r.Privacy.Verdict, r.Privacy.Reason)
	rows := [][]string{}
	for _, p := range r.Privacy.Rows {
		rows = append(rows, []string{p.Arm, itoa(p.ScoreRows), itoa(p.ScoreHits), itoa(p.ScoreRowsMissing), itoa(p.LedgerRows), itoa(p.LedgerHits), itoa(p.ReplacedHits)})
	}
	table(b, []string{"Arm", "Score rows", "Score hits", "Score rows without a count", "Ledger rows", "Ledger hits", "Hits on replaced rows"}, rows)
}

func writeSampleSize(b *strings.Builder, r *Report) {
	b.WriteString("## Sample size\n\n")
	b.WriteString("n = (1.96+0.84)^2 sd^2 / delta^2, rounded up, using the observed SD of paired differences. This is the normal approximation and treats the SD as known.\n\n")
	writeSizeTable(b, r.SampleSize)
	if r.SampleSizeRead != nil {
		writeSizeTable(b, *r.SampleSizeRead)
	}
}

func writeSizeTable(b *strings.Builder, size SampleSizeSection) {
	rows := [][]string{}
	for _, s := range size.Rows {
		drives := "no"
		if s.DrivesL4 {
			drives = "yes"
		}
		rows = append(rows, []string{s.Comparison, s.Metric, itoa(s.Pairs), fnum(s.SD, 4), inum(s.NeedLow), inum(s.NeedHigh), drives})
	}
	table(b, []string{"Comparison", "Metric", "Pairs", "SD diff", "n for 0.05", "n for 0.10", "Sets the L4 size"}, rows)
	fmt.Fprintf(b, "%s.\n\n", size.Statement)
}
