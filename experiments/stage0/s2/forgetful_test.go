package s2

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// edit replaces every occurrence of old in one solution file.
type edit struct{ file, old, new string }

// variant is the reference solution with one carried constraint forgotten: the
// later change is still made, the way someone would make it who never knew about
// the constraint.
type variant struct {
	carry int // index into Task.Carries
	name  string
	edits []edit
}

var forgetful = map[string][]variant{
	"s2-01-job-queue": {
		{0, "whole seconds", []edit{
			{"retry.go", "const maxBackoff = 60000", "const maxBackoff = 60"},
			{"retry.go", "d := 2000", "d := 2"},
			{"retry.go", "return 30000 + 5000*j.Attempts", "return 30 + 5*j.Attempts"},
		}},
		{1, "exact names", []edit{
			{"queue.go", "strings.EqualFold(j.Name, name)", "j.Name == name"},
			{"queue.go", "\"sort\"\n\t\"strings\"\n", "\"sort\"\n"},
		}},
		{2, "fresh error", []edit{
			{"queue.go", "return ErrNotQueued", `return errors.New("no such job")`},
			{"queue.go", "return Job{}, ErrNotQueued", `return Job{}, errors.New("no such job")`},
		}},
	},
	"s2-02-invoice-totals": {
		{0, "truncating", []edit{{"money.go", "return (2*n + d) / (2 * d)", "return n / d"}}},
		{1, "tax on the subtotal", []edit{{"invoice.go", "return net + Tax(net, taxPermille)", "return net + Tax(sub, taxPermille)"}}},
		{2, "fresh error", []edit{{"invoice.go", "return Line{}, ErrBadLine", `return Line{}, errors.New("malformed line")`}}},
	},
	"s2-03-catalog-listing": {
		{0, "pages from zero", []edit{
			{"list.go", "if page < 1 {\n\t\tpage = 1\n\t}", "if page < 0 {\n\t\tpage = 0\n\t}"},
			{"list.go", "start := (page - 1) * size", "start := page * size"},
		}},
		{1, "the old default", []edit{{"list.go", "const defaultPageSize = 25", "const defaultPageSize = 20"}}},
		{2, "insertion order", []edit{
			{"list.go", "import (\n\t\"sort\"\n\t\"strings\"\n)", `import "strings"`},
			{"list.go", "\tsort.Slice(out, func(a, b int) bool {\n\t\tif out[a].PriceCents != out[b].PriceCents {\n\t\t\treturn out[a].PriceCents < out[b].PriceCents\n\t\t}\n\t\treturn out[a].SKU < out[b].SKU\n\t})\n", ""},
		}},
	},
	"s2-04-settings-file": {
		{0, "strconv.ParseBool", []edit{{
			"config.go",
			"switch strings.ToLower(v) {\n\tcase \"on\", \"yes\", \"true\":\n\t\treturn true\n\t}\n\treturn false",
			"b, _ := strconv.ParseBool(v)\n\treturn b",
		}}},
		{1, "raw keys", []edit{
			{"config.go", "delete(c.values, norm(key))", "delete(c.values, key)"},
			{"config.go", "_, ok := c.values[norm(key)]", "_, ok := c.values[key]"},
		}},
		{2, "time.ParseDuration alone", []edit{{"config.go", "strconv.Atoi(v); err == nil {", "strconv.Atoi(v); err == nil && false {"}}},
	},
	"s2-05-warehouse-stock": {
		{0, "no floor", []edit{{"stock.go", "const safetyFloor = 5", "const safetyFloor = 0"}}},
		{1, "no entries", []edit{
			{"stock.go", "\ts.record(\"ship\", sku, qty)\n", ""},
			{"stock.go", "\ts.record(\"adjust\", sku, delta)\n", ""},
			{"stock.go", "\ts.record(\"scrap\", sku, qty)\n", ""},
		}},
		{2, "units and no back-orders", []edit{
			{"stock.go", ` || strings.HasPrefix(line, "!")`, ""},
			{"stock.go", "s.Receive(sku, n*12)", "s.Receive(sku, n)"},
		}},
	},
	"s2-06-room-bookings": {
		{0, "End occupied", []edit{
			{"booking.go", "b.Start < o.End && o.Start < b.End", "b.Start <= o.End && o.Start <= b.End"},
			{"booking.go", "day >= b.Start && day < b.End", "day >= b.Start && day <= b.End"},
		}},
		{1, "fresh errors", []edit{
			{"booking.go", "if c.clashes(b, -1) {\n\t\treturn ErrConflict", "if c.clashes(b, -1) {\n\t\treturn errors.New(\"room already taken\")"},
			{"booking.go", "if c.clashes(b, i) {\n\t\t\treturn ErrConflict", "if c.clashes(b, i) {\n\t\t\treturn errors.New(\"room already taken\")"},
		}},
		{2, "closed rooms bookable", []edit{
			{"booking.go", "import (\n\t\"errors\"\n\t\"strings\"\n)", `import "errors"`},
			{"booking.go", "\tif strings.HasPrefix(b.Room, \"X-\") {\n\t\treturn ErrConflict\n\t}\n", ""},
		}},
		{3, "no limit", []edit{{"booking.go", "const maxNights = 14", "const maxNights = 1 << 30"}}},
	},
	"s2-07-sales-export": {
		{0, "blank region", []edit{{"export.go", `return "UNASSIGNED"`, "return region"}}},
		{1, "plain CSV", []edit{
			{"export.go", `"%d,%02d"`, `"%d.%02d"`},
			{"export.go", `"region;rep;amount\n"`, `"region,rep,amount\n"`},
			{"export.go", `"%s;%s;%s\n"`, `"%s,%s,%s\n"`},
			{"export.go", `"region;amount\n"`, `"region,amount\n"`},
			{"export.go", `"%s;%s\n"`, `"%s,%s\n"`},
		}},
		{2, "zero sales counted", []edit{
			{"export.go", "if s.Cents != 0 {\n\t\t\tout[label(s.Region)] += s.Cents\n\t\t}", "out[label(s.Region)] += s.Cents"},
			{"export.go", "if s.Cents != 0 {\n\t\t\tout = append(out, s)\n\t\t}", "out = append(out, s)"},
		}},
	},
	"s2-08-member-signup": {
		{0, "bytes", []edit{
			{"directory.go", "n := utf8.RuneCountInString(name)", "n := len(name)"},
			{"directory.go", "\"strings\"\n\t\"unicode/utf8\"\n", "\"strings\"\n"},
		}},
		{0, "other bounds", []edit{{"directory.go", "n >= 3 && n <= 16", "n >= 1 && n <= 32"}}},
		{1, "reserved names accepted", []edit{{"directory.go", "if !LengthOK(name) || reserved(name) {", "if !LengthOK(name) {"}}},
		{2, "fresh error", []edit{{"directory.go", "return ErrInvalid", `return errors.New("unacceptable name")`}}},
		{3, "raw addresses", []edit{{"directory.go", "{ return strings.ToLower(strings.TrimSpace(email)) }", "{ return email }"}}},
	},
	"s2-09-cache-cleanup": {
		{0, "Sweep removed", []edit{{"stats.go", "// Sweep removes expired entries for the nightly job, which links against it.\nfunc (c *Cache) Sweep() int { return len(c.Expire()) }\n", ""}}},
		{0, "Stats removed", []edit{{"stats.go", "// Stats reports lookup counts for the nightly job, which links against it.\nfunc (c *Cache) Stats() (hits, misses int) { return c.hits, c.misses }\n", ""}}},
		{1, "ttl added as is", []edit{{"cache.go", "e.expires = c.deadline(ttl)", "e.expires = c.now() + int64(ttl)"}}},
		{2, "tmp keys exported", []edit{{"cache.go", `return !strings.HasPrefix(key, "tmp:")`, "return true"}}},
	},
	"s2-10-sensor-stats": {
		{0, "faults counted", []edit{{"stats.go", "if v >= 0 {", "if true {"}}},
		{1, "no empty handling", []edit{{"stats.go", "\tif len(v) == 0 {\n\t\treturn 0\n\t}\n", ""}}},
		{2, "interpolated", []edit{{
			"stats.go",
			"rank := int(math.Ceil(p / 100 * float64(len(v))))\n\treturn v[max(rank, 1)-1]",
			"pos := p / 100 * float64(len(v)-1)\n\tlo, hi := int(math.Floor(pos)), int(math.Ceil(pos))\n\treturn v[lo] + (v[hi]-v[lo])*(pos-float64(lo))",
		}}},
		{3, "sorted in place", []edit{
			{"stats.go", "out := slices.Clone(values)", "out := values"},
			{"stats.go", "\t\"slices\"\n", ""},
		}},
	},
	"s2-11-line-protocol": {
		{0, "lower-case digits", []edit{{
			"frame.go",
			`return withChecksum(fmt.Sprintf("%d|%s|%s|", f.Seq, kind, f.Body))`,
			"s := fmt.Sprintf(\"%d|%s|%s|\", f.Seq, kind, f.Body)\n\treturn s + fmt.Sprintf(\"%02x\", checksum.Sum(s))",
		}}},
		{0, "kind as given", []edit{{"frame.go", "kind := strings.ToUpper(f.Kind)", "kind := f.Kind"}}},
		{1, "no wrap", []edit{
			{"frame.go", "\tif seq >= 999 {\n\t\treturn 1\n\t}\n", ""},
			{"frame.go", "return seq >= 1 && seq <= 999", "return seq >= 0"},
		}},
		{2, "ordinary frame", []edit{
			{"frame.go", `Encode(Frame{Seq: 0, Kind: "ACK", Body: strconv.Itoa(seq)})`, `Encode(Frame{Seq: seq, Kind: "ACK"})`},
			{"frame.go", "return f.Seq == 0 && f.Body == strconv.Itoa(seq)", "return f.Seq == seq"},
		}},
	},
	"s2-12-access-rules": {
		{0, "last match wins", []edit{{"policy.go", "if !r.Allow {\n\t\t\t\treturn false\n\t\t\t}\n\t\t\tallowed = true", "allowed = r.Allow"}}},
		{1, "raw paths", []edit{{"policy.go", `return strings.TrimPrefix(resource, "/")`, "return resource"}}},
		{2, "no wildcards", []edit{{"policy.go", `strings.CutSuffix(pattern, "/*"); ok {`, `strings.CutSuffix(pattern, "/*"); ok && false {`}}},
		{2, "plain prefix", []edit{{"policy.go", `strings.HasPrefix(resource, prefix+"/")`, "strings.HasPrefix(resource, prefix)"}}},
		{3, "fresh error", []edit{{"policy.go", "if !found {\n\t\treturn ErrNoRule", "if !found {\n\t\treturn errors.New(\"rule not found\")"}}},
	},
	"s2-13-deploy-dry-run": {
		{0, "Dry ignored", []edit{
			{"deployer.go", "\tif d.Dry {\n\t\td.Plan = append(d.Plan, \"remove \"+path)\n\t\treturn nil\n\t}\n", ""},
			{"deployer.go", "\tif d.Dry {\n\t\td.Plan = append(d.Plan, \"rename \"+from)\n\t\treturn nil\n\t}\n", ""},
			{"deployer.go", "\tif d.Dry {\n\t\td.Plan = append(d.Plan, \"copy \"+from)\n\t\treturn nil\n\t}\n", ""},
			{"deployer.go", "\tif d.Dry {\n\t\td.Plan = append(d.Plan, \"touch \"+path)\n\t\treturn nil\n\t}\n", ""},
		}},
		{1, "Exec left alone", []edit{
			{"deployer.go", "d.Files[to] = placed(to, f)", "d.Files[to] = f"},
			{"deployer.go", "d.Files[path] = placed(path, File{})", "d.Files[path] = File{}"},
		}},
		{2, "only Put checks", []edit{
			{"deployer.go", "if protected(path) {\n\t\treturn ErrProtected\n\t}\n\tf, ok := d.Files[path]", "f, ok := d.Files[path]"},
			{"deployer.go", "\tif protected(from) || protected(to) {\n\t\treturn ErrProtected\n\t}\n", ""},
			{"deployer.go", "if protected(path) {\n\t\treturn ErrProtected\n\t}\n\tif _, ok := d.Files[path]; ok {", "if _, ok := d.Files[path]; ok {"},
		}},
		{3, "hard delete", []edit{{"deployer.go", "d.Files[\"trash/\"+path] = f", "_ = f"}}},
	},
	"s2-14-event-timeline": {
		{0, "RFC 3339", []edit{{"log.go", `Format("2006-01-02 15:04 UTC")`, "Format(time.RFC3339)"}}},
		{0, "local zone", []edit{{"log.go", "time.Unix(e.At, 0).UTC().Format(", "time.Unix(e.At, 0).Format("}}},
		{1, "the old order", []edit{
			{"log.go", "func newestFirst(events []Event) []Event {", "func oldestFirst(events []Event) []Event {\n\tsort.SliceStable(events, func(a, b int) bool {\n\t\tif events[a].At != events[b].At {\n\t\t\treturn events[a].At < events[b].At\n\t\t}\n\t\treturn events[a].ID < events[b].ID\n\t})\n\treturn events\n}\n\nfunc newestFirst(events []Event) []Event {"},
			{"log.go", "v := newestFirst(l.shown())", "v := oldestFirst(l.shown())"},
			{"log.go", "if e.Kind == kind {\n\t\t\tout = append(out, e)\n\t\t}\n\t}\n\treturn newestFirst(out)", "if e.Kind == kind {\n\t\t\tout = append(out, e)\n\t\t}\n\t}\n\treturn oldestFirst(out)"},
		}},
		{2, "debug shown", []edit{{"log.go", `if e.Kind != "debug" {`, "if true {"}}},
	},
	"s2-15-shipping-rates": {
		{0, "weights in kilos", []edit{
			{"rates.go", "gramsPerKilo = 1000", "gramsPerKilo = 1"},
			{"rates.go", "maxWeight    = 30000", "maxWeight    = 30"},
		}},
		{1, "the old threshold", []edit{{"rates.go", "freeFrom     = 7500", "freeFrom     = 5000"}}},
		{1, "never free", []edit{{"rates.go", "freeFrom     = 7500", "freeFrom     = 1 << 62"}}},
		{2, "no zone rules", []edit{
			{"rates.go", "if zone == \"A\" || zone == \"B\" {\n\t\treturn 0\n\t}\n\treturn 1200", "return 0"},
			{"rates.go", "if !ok {\n\t\trate = ratePerKilo[\"C\"]\n\t}", "if !ok {\n\t\treturn 0, errors.New(\"unknown zone\")\n\t}"},
		}},
		{3, "no weight limit", []edit{
			{"rates.go", "\tif !Accepts(p.Weight) {\n\t\treturn 0, ErrTooHeavy\n\t}\n", ""},
			{"rates.go", "return weight <= maxWeight", "return true"},
		}},
	},
}

// applyEdits returns files with the edits made, failing if an edit no longer
// matches, which would mean a variant silently stopped testing anything.
func applyEdits(t *testing.T, files map[string]string, edits []edit) map[string]string {
	t.Helper()
	out := make(map[string]string, len(files))
	for name, content := range files {
		out[name] = content
	}
	for _, e := range edits {
		content, ok := out[e.file]
		if !ok {
			t.Fatalf("edit of %s: no such solution file", e.file)
		}
		if !strings.Contains(content, e.old) {
			t.Fatalf("edit of %s: %q not found", e.file, e.old)
		}
		out[e.file] = strings.ReplaceAll(content, e.old, e.new)
	}
	return out
}

func scoreFiles(t *testing.T, task Task, files map[string]string) Result {
	t.Helper()
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := w.WriteFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	assertVisiblePass(t, w)
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed {
		t.Fatalf("does not build:\n%s", res.Output)
	}
	return res
}

// TestForgetfulSolutions proves that the dependence across turns is really
// checked. For each variant: the visible tests still pass, so running them cannot
// reveal the constraint; the hidden score falls below 1.0; and the hidden tests
// that fail are ones the carry names, so that a failure can be attributed.
// Together a carry's variants must fail every test it names.
func TestForgetfulSolutions(t *testing.T) {
	for _, task := range allTasks() {
		t.Run(task.ID, func(t *testing.T) {
			t.Parallel()
			variants := forgetful[task.ID]
			failedBy := make([]map[string]bool, len(task.Carries))
			for i := range failedBy {
				failedBy[i] = map[string]bool{}
			}
			for _, v := range variants {
				t.Run(v.name, func(t *testing.T) {
					res := scoreFiles(t, task, applyEdits(t, task.Solution, v.edits))
					if res.Score >= 1.0 {
						t.Fatalf("forgetting %q still scores %v", task.Carries[v.carry].What, res.Score)
					}
					for _, name := range res.Failed {
						failedBy[v.carry][name] = true
						if !slices.Contains(task.Carries[v.carry].Tests, name) {
							t.Errorf("%s failed, but the carry %q does not name it", name, task.Carries[v.carry].What)
						}
					}
					t.Logf("score %.2f, failed %v", res.Score, res.Failed)
				})
			}
			for i, c := range task.Carries {
				for _, name := range c.Tests {
					if !failedBy[i][name] {
						t.Errorf("no variant of %q fails %s", c.What, name)
					}
				}
			}
		})
	}
}

// amnesiaFiles returns the solution with every carried constraint forgotten. A
// variant that an earlier one has overtaken, such as a second wrong value for the
// same constant, is skipped.
func amnesiaFiles(t *testing.T, task Task) map[string]string {
	t.Helper()
	files := task.Solution
	seen := map[int]bool{}
	for _, v := range forgetful[task.ID] {
		applies := true
		for _, e := range v.edits {
			applies = applies && strings.Contains(files[e.file], e.old)
		}
		if !applies && !seen[v.carry] {
			t.Fatalf("variant %q no longer applies", v.name)
		}
		if applies {
			files = applyEdits(t, files, v.edits)
			seen[v.carry] = true
		}
	}
	return files
}

// TestTotalAmnesia forgets every carried constraint at once. It is the floor a
// strategy that carries nothing could expect, given that it still does each
// turn's stated work correctly.
func TestTotalAmnesia(t *testing.T) {
	for _, task := range allTasks() {
		t.Run(task.ID, func(t *testing.T) {
			t.Parallel()
			res := scoreFiles(t, task, amnesiaFiles(t, task))
			if res.Score >= 0.8 {
				t.Errorf("forgetting everything still scores %.2f, so this task hardly measures carrying", res.Score)
			}
			checked := map[string]bool{}
			for _, c := range task.Carries {
				for _, name := range c.Tests {
					checked[name] = true
				}
			}
			for _, name := range res.Failed {
				if !checked[name] {
					t.Errorf("%s failed, but no carry names it", name)
				}
			}
			t.Logf("amnesia score %.2f (%d of %d)", res.Score, res.Passed, res.Total)
		})
	}
}
