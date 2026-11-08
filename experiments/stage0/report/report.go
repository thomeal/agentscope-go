// Package report is the stage 0 analysis: it reads the scores.jsonl and
// ledger.jsonl files of finished runs and computes paired comparisons, gate
// verdicts and the cost, privacy and sample size sections.
//
// It knows the output format of a run and nothing about how the arms work. Rows
// are never filtered by exposure. Missing data surfaces as a stated count, and
// values are rounded only at display: report.json carries full precision.
package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Report holds everything the renderers print. Markdown and JSON read the same
// struct, so they carry the same numbers.
type Report struct {
	RunDirs    []string          `json:"run_dirs"`
	Seed       int64             `json:"seed"`
	Resamples  int               `json:"resamples"`
	Load       LoadStats         `json:"load"`
	Arms       []ArmSummary      `json:"arms"`
	G1         GateResult        `json:"g1"`
	G6         GateResult        `json:"g6"`
	Cost       CostSection       `json:"cost"`
	Privacy    PrivacySection    `json:"privacy"`
	SampleSize SampleSizeSection `json:"sample_size"`
	// SampleSizeRead is set on an S2 report, for the event-list form of T1.
	SampleSizeRead *SampleSizeSection `json:"sample_size_read,omitempty"`
	Limits         []string           `json:"limits"`
	// Secondary holds the registered analyses beside the gates. A report of S2 rows sets S2,
	// G2 and G2Read in place of G1, G6 and the privacy section: G2 is judged on hidden tests,
	// so only that report can decide it.
	Secondary  SecondarySection `json:"secondary"`
	S2         *S2Section       `json:"s2,omitempty"`
	G2         *GateResult      `json:"g2,omitempty"`
	G2Read     *GateResult      `json:"g2_read,omitempty"`
	Route      *RouteSection    `json:"route,omitempty"`
	Manifests  []Manifest       `json:"manifests"`
	NoManifest []string         `json:"no_manifest"`
	// NonPrimary names the runs that used budgets other than the frozen ones.
	NonPrimary []string `json:"non_primary,omitempty"`
}

// c7Limit restates the limit HARNESS.md requires the report to state.
const c7Limit = "gpt-6-luna is already the smallest listed model, so C7 tests only the reuse of the prefix and not a cheaper model."

// Build computes the whole report. The same data and seed give the same report.
func Build(d *Data, seed int64) *Report {
	return BuildWithThetas(d, seed, nil)
}

// BuildWithThetas is Build with the thresholds an S3 report applies.
func BuildWithThetas(d *Data, seed int64, thetas map[string]float64) *Report {
	limits := slices.Clone(standingLimits)
	for _, dir := range d.NoManifest {
		limits = append(limits, "Run "+dir+" has no manifest, so its budgets are not checked against the others.")
	}
	r := &Report{
		RunDirs:    d.RunDirs,
		Seed:       seed,
		Resamples:  Resamples,
		Load:       d.Stats,
		Arms:       d.Arms(),
		Cost:       d.Cost(),
		Limits:     limits,
		Manifests:  d.Manifests,
		NoManifest: d.NoManifest,
		NonPrimary: d.nonPrimary(),
	}
	if d.isS3() {
		route := d.Route(seed, thetas)
		r.Route = &route
		r.Limits = []string{
			"S3 restates labels word for word, so a literal rule can use recent text as well as any classifier. The corpus shows whether a router can use state, not how much state helps in real sessions.",
			"Local keeps LocalOwn in every world and imports at most 2,000 tokens of published text past its summary, so the local side here is not T1's clear rule.",
			"Each world has at most two forks on one line of history, so a summary here has one earlier summary to carry forward where a long T1 session has many.",
			"R3's state is the same for every classifier and holds only shared data. A locally deployed classifier could also see local notes, which the plan allows and this does not test.",
		}
		for _, name := range []string{"jev", "piccolo"} {
			if !slices.Contains(route.ClassifierNames(), name) {
				r.Limits = append(r.Limits, "Classifier "+name+" was not measured in these rows.")
			}
		}
		if slices.Contains(route.ClassifierNames(), "jev") {
			r.Limits = append(r.Limits, "Hosted Jev has no price in this harness, so its attempts show $0 in the ledger.")
		}
		for _, dir := range d.NoManifest {
			r.Limits = append(r.Limits, "Run "+dir+" has no manifest, so its budgets are not checked against the others.")
		}
		return r
	}
	if d.isS2() {
		s2 := d.S2(seed)
		g2, read := d.G2(seed), d.G2Read(seed)
		r.S2, r.G2, r.G2Read = &s2, &g2, &read
		r.SampleSize = d.TaskSampleSize()
		size := d.ReadSampleSize()
		r.SampleSizeRead = &size
		r.Limits = slices.Clone(s2Limits)
		if s2.Set == setS2 {
			r.Limits = append(r.Limits, s2TuningLimit)
		}
		for _, dir := range d.NoManifest {
			r.Limits = append(r.Limits, "Run "+dir+" has no manifest, so its budgets are not checked against the others.")
		}
		return r
	}
	r.G1, r.G6 = d.G1(seed), d.G6(seed)
	r.Secondary = d.Secondary(seed)
	r.Privacy = d.Privacy()
	r.SampleSize = d.SampleSize()
	return r
}

// JSON writes the report as indented JSON with full precision.
func JSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// Main is the entry point for a CLI: --runs a,b --seed N --out dir. It writes
// report.md and report.json into dir and prints the verdicts. A gate that fails
// is a result, not an error.
func Main(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stdout)
	runs := fs.String("runs", "", "comma-separated run directories; a later one replaces earlier rows")
	seed := fs.Int64("seed", 1, "bootstrap seed")
	out := fs.String("out", "", "directory for report.md and report.json")
	thetaList := fs.String("theta", "", "S3 only: thresholds frozen on the pilot, per classifier and router, such as decisions/R2=0.7,decisions/R3=0.8")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("report: unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *out == "" {
		return errors.New("report: --out is required")
	}
	var dirs []string
	for dir := range strings.SplitSeq(*runs, ",") {
		if dir = strings.TrimSpace(dir); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return errors.New("report: --runs is required")
	}
	data, err := Load(dirs...)
	if err != nil {
		return err
	}
	if err := data.CheckPooling(); err != nil {
		return err
	}
	thetas, err := ParseThetas(*thetaList)
	if err != nil {
		return err
	}
	if err := data.CheckThetas(thetas); err != nil {
		return err
	}
	r := BuildWithThetas(data, *seed, thetas)
	var md, js bytes.Buffer
	if err := Markdown(&md, r); err != nil {
		return err
	}
	if err := JSON(&js, r); err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return fmt.Errorf("report: output directory: %w", err)
	}
	mdPath, jsPath := filepath.Join(*out, "report.md"), filepath.Join(*out, "report.json")
	if err := os.WriteFile(mdPath, md.Bytes(), 0o644); err != nil {
		return fmt.Errorf("report: write report.md: %w", err)
	}
	if err := os.WriteFile(jsPath, js.Bytes(), 0o644); err != nil {
		return fmt.Errorf("report: write report.json: %w", err)
	}
	if len(r.NonPrimary) > 0 {
		fmt.Fprintf(stdout, "NOT A PRIMARY RESULT: %s used budgets other than the frozen ones or uncommitted code\n", strings.Join(r.NonPrimary, ", "))
	}
	if r.Route != nil {
		for _, g := range r.Route.Gates {
			fmt.Fprintf(stdout, "%s %s: %s\n", g.Gate, g.Verdict, g.Reason)
		}
	}
	if r.S2 != nil {
		for _, g := range []*GateResult{r.G2, r.G2Read} {
			fmt.Fprintf(stdout, "%s %s: %s\n", g.Gate, g.Verdict, g.Reason)
		}
		fmt.Fprintf(stdout, "%s\n%s\n", r.SampleSize.Statement, r.SampleSizeRead.Statement)
	}
	if r.S2 == nil && r.Route == nil {
		for _, g := range []GateResult{r.G1, r.G6} {
			fmt.Fprintf(stdout, "%s %s: %s\n", g.Gate, g.Verdict, g.Reason)
		}
		fmt.Fprintf(stdout, "Privacy %s: %s\n", r.Privacy.Verdict, r.Privacy.Reason)
		fmt.Fprintf(stdout, "%s\n", r.SampleSize.Statement)
	}
	fmt.Fprintf(stdout, "wrote %s and %s\n", mdPath, jsPath)
	return nil
}
