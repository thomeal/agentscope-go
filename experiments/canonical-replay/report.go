package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// bootstrapStream is stage 0's fixed second PCG word (report/paired.go); the seed is the
// first. Seed 1 and 10,000 resamples are stage 0's conventions.
const bootstrapStream = 0x5354414745304253

// percentile interpolates linearly between closest ranks (R type 7), as stage 0's report.
func percentile(sorted []float64, p float64) float64 {
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)
	return sorted[lo] + (pos-float64(lo))*(sorted[hi]-sorted[lo])
}

// bootstrapCI is stage 0's percentile bootstrap of the mean.
func bootstrapCI(diffs []float64, seed int64, resamples int) (lo, hi float64) {
	rng := rand.New(rand.NewPCG(uint64(seed), bootstrapStream))
	n := len(diffs)
	means := make([]float64, resamples)
	for i := range means {
		sum := 0.0
		for range n {
			sum += diffs[rng.IntN(n)]
		}
		means[i] = sum / float64(n)
	}
	slices.Sort(means)
	return percentile(means, 0.025), percentile(means, 0.975)
}

// cell is one prompt and variant: valid replays and runaways among them.
type cell struct{ valid, runaways int }

func (c cell) rate() float64 { return float64(c.runaways) / float64(c.valid) }

// paired is a variant against a baseline by prompt: the mean over prompts of the
// difference in runaway rate, in points, with its bootstrap CI over prompts.
type paired struct {
	prompts       []int
	diffs         []float64
	mean, lo, hi  float64
	left, outside int // prompts without valid rows in both variants
}

// byPrompt is Amendment 8's reading: per-prompt rates, differenced, averaged over prompts.
func byPrompt(cells map[jobKey]cell, prompts []int, variant, baseline string, seed int64, resamples int) paired {
	var p paired
	for _, id := range prompts {
		a, b := cells[jobKey{id, variant}], cells[jobKey{id, baseline}]
		if a.valid == 0 || b.valid == 0 {
			p.left++
			continue
		}
		p.prompts = append(p.prompts, id)
		p.diffs = append(p.diffs, 100*(a.rate()-b.rate()))
	}
	if len(p.diffs) == 0 {
		return p
	}
	sum := 0.0
	for _, d := range p.diffs {
		sum += d
	}
	p.mean = sum / float64(len(p.diffs))
	p.lo, p.hi = bootstrapCI(p.diffs, seed, resamples)
	return p
}

// tally builds cells from the rows that stand for each job.
func tally(rows []Row) (map[jobKey]cell, []int) {
	cells := map[jobKey]cell{}
	ids := map[int]bool{}
	for _, r := range final(rows) {
		ids[r.Prompt] = true
		if !r.valid() {
			continue
		}
		k := jobKey{r.Prompt, r.Variant}
		c := cells[k]
		c.valid++
		if r.runaway() {
			c.runaways++
		}
		cells[k] = c
	}
	var prompts []int
	for id := range ids {
		prompts = append(prompts, id)
	}
	slices.Sort(prompts)
	return cells, prompts
}

func cmdReport(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	out := fs.String("out", defaultOut, "output directory holding replay.jsonl")
	seed := fs.Int64("seed", 1, "bootstrap seed")
	resamples := fs.Int("resamples", 10000, "bootstrap resamples")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rows, err := readLedger(filepath.Join(*out, "replay.jsonl"))
	if err != nil {
		return err
	}
	checked := map[jobKey]string{}
	if b, err := os.ReadFile(filepath.Join(*out, "check", "summary.json")); err == nil {
		var results []checkResult
		if json.Unmarshal(b, &results) == nil {
			for _, r := range results {
				for v, h := range r.Hashes {
					checked[jobKey{r.Prompt, v}] = h
				}
			}
		}
	}
	report(w, rows, checked, *seed, *resamples)
	return nil
}

// report writes the readings Amendment 22 names.
func report(w io.Writer, rows []Row, checked map[jobKey]string, seed int64, resamples int) {
	cells, prompts := tally(rows)
	fin := final(rows)
	spent, canceled, reruns := 0.0, 0, 0
	for _, r := range rows {
		spent += r.charged()
		if r.canceled() {
			canceled++
		}
		if r.Rerun > 0 {
			reruns++
		}
	}
	fmt.Fprintf(w, "Ledger: %d rows (%d reruns, %d interrupted), $%.4f charged to the hard stop.\n\n", len(rows), reruns, canceled, spent)

	fmt.Fprintln(w, "| Variant | Runaways | Valid calls | Rate | Infra rows | Cost $ | Max ordinary output |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|")
	for _, v := range variants {
		var c cell
		infra, cost, maxOrd := 0, 0.0, 0
		for _, r := range fin {
			if r.Variant != v {
				continue
			}
			if !r.valid() {
				infra++
				continue
			}
			c.valid++
			cost += r.USD
			if r.runaway() {
				c.runaways++
			} else {
				maxOrd = max(maxOrd, r.Output)
			}
		}
		rate := 0.0
		if c.valid > 0 {
			rate = 100 * c.rate()
		}
		fmt.Fprintf(w, "| %s | %d | %d | %.2f%% | %d | %.4f | %d |\n", v, c.runaways, c.valid, rate, infra, cost, maxOrd)
	}

	primary := byPrompt(cells, prompts, libOn, libOff, seed, resamples)
	fmt.Fprintf(w, "\nPrimary, lib_on − lib_off by prompt: %+.1f points [%+.1f, %+.1f] on %d prompts", primary.mean, primary.lo, primary.hi, len(primary.diffs))
	if primary.left > 0 {
		fmt.Fprintf(w, " (%d left out: no valid rows in one variant)", primary.left)
	}
	fmt.Fprintf(w, " — %s.\n", verdict(primary, cells))
	var rest []int
	for _, id := range prompts {
		if id != 0 {
			rest = append(rest, id)
		}
	}
	sec := byPrompt(cells, rest, libOn, libOff, seed, resamples)
	fmt.Fprintf(w, "Without prompt 0 (chosen by its outcome): %+.1f points [%+.1f, %+.1f] on %d prompts.\n\n", sec.mean, sec.lo, sec.hi, len(sec.diffs))

	fmt.Fprintln(w, "| Prompt | lib_off | lib_on | Body hashes (off, on) |")
	fmt.Fprintln(w, "|---|---|---|---|")
	for _, id := range prompts {
		var cols []string
		for _, v := range variants {
			c := cells[jobKey{id, v}]
			cols = append(cols, fmt.Sprintf("%d/%d", c.runaways, c.valid))
		}
		fmt.Fprintf(w, "| %d | %s | %s | %s, %s |\n", id, cols[0], cols[1], hashState(rows, checked, jobKey{id, libOff}), hashState(rows, checked, jobKey{id, libOn}))
	}

	// Who writes path first: the mechanism Amendment 8 found.
	fmt.Fprintln(w, "\nFirst key of the cloud's write_file replies:")
	for _, v := range variants {
		keys := map[string]int{}
		for _, r := range fin {
			if r.Variant == v && r.valid() && slices.Contains(r.Tools, "write_file") && r.FirstKey != nil {
				keys[*r.FirstKey]++
			}
		}
		var parts []string
		for _, k := range sortedKeys(keys, strings.Compare) {
			parts = append(parts, fmt.Sprintf("%s %d", k, keys[k]))
		}
		fmt.Fprintf(w, "  %s: %s\n", v, strings.Join(parts, ", "))
	}
	errs := map[string]int{}
	for _, r := range fin {
		if r.Error != nil {
			errs[*r.Error]++
		}
	}
	if len(errs) > 0 {
		fmt.Fprintln(w, "\nInfra rows that stand after reruns:")
		for _, k := range sortedKeys(errs, strings.Compare) {
			fmt.Fprintf(w, "  %s: %d\n", k, errs[k])
		}
	}
}

// verdict applies Amendment 22's reading rule.
func verdict(p paired, cells map[jobKey]cell) string {
	off := 0
	for k, c := range cells {
		if k.Variant == libOff {
			off += c.runaways
		}
	}
	switch {
	case len(p.diffs) == 0:
		return "no reading"
	case off < 10:
		return fmt.Sprintf("lib_off gave %d runaways, under 10: the library path does not reproduce the effect, so this cannot test the option", off)
	case p.hi < 0:
		return "confirmed: the CI lies below 0"
	case p.lo > 0:
		return "reversed: the CI lies above 0"
	}
	return "not confirmed: the CI includes 0"
}

// hashState says whether every row of a prompt and variant sent the same body, and
// whether it is the body the offline check produced.
func hashState(rows []Row, checked map[jobKey]string, k jobKey) string {
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Prompt == k.Prompt && r.Variant == k.Variant && r.BodySHA != "" {
			seen[r.BodySHA] = true
		}
	}
	switch {
	case len(seen) == 0:
		return "none"
	case len(seen) > 1:
		return fmt.Sprintf("%d distinct", len(seen))
	}
	for h := range seen {
		if want, ok := checked[k]; ok {
			if want == h {
				return "1, as checked"
			}
			return "1, NOT the checked body"
		}
	}
	return "1"
}
