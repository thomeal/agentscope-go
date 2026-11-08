package s2

func accB02() Task {
	return Task{
		ID:     "s2-b02-log-rotation",
		Set:    "acceptance",
		System: system("a log rotation helper"),
		Files: map[string]string{
			"go.mod": goMod,
			"policy.go": `package logrotate

// Policy says when a live log rolls over and how many files stay around.
type Policy struct {
	MaxSize int64 // rollover threshold
	Keep    int   // files to keep
}

// File is one log file on disk.
type File struct {
	Name string
	Size int64 // bytes
}

// Empty reports whether the file has no content.
func (f File) Empty() bool { return f.Size == 0 }
`,
			"docs/retention.txt": `Retention notes

Compliance requires that rotated logs whose base name starts with "audit" are
never deleted by rotation. A separate job moves them to cold storage. They still
roll over like any other log.

Other logs have no retention requirement beyond disk space.
`,
			"policy_test.go": `package logrotate

import "testing"

func TestEmpty(t *testing.T) {
	if !(File{Name: "x"}).Empty() {
		t.Fatal("should be empty")
	}
	if (File{Name: "x", Size: 1}).Empty() {
		t.Fatal("should not be empty")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package logrotate_test

import (
	"slices"
	"testing"

	logrotate "fixture"
)

func TestRotatedNames(t *testing.T) {
	for gen, want := range map[int]string{1: "app.log.1", 2: "app.log.2.gz", 11: "app.log.11.gz"} {
		if got := logrotate.RotatedName("app.log", gen); got != want {
			t.Errorf("generation %d: %q", gen, got)
		}
	}
}

func TestParsePlainGeneration(t *testing.T) {
	if gen, ok := logrotate.ParseGeneration("app.log", "app.log.1"); !ok || gen != 1 {
		t.Fatalf("got %d, %v", gen, ok)
	}
}

func TestParseRejects(t *testing.T) {
	for _, name := range []string{"app.log", "other.log.1", "app.log.x", "app.log."} {
		if gen, ok := logrotate.ParseGeneration("app.log", name); ok {
			t.Errorf("%q: generation %d", name, gen)
		}
	}
}

func TestParseGzipped(t *testing.T) {
	for name, want := range map[string]int{"app.log.2.gz": 2, "app.log.12.gz": 12} {
		if gen, ok := logrotate.ParseGeneration("app.log", name); !ok || gen != want {
			t.Errorf("%q: got %d, %v", name, gen, ok)
		}
	}
}

func TestValidPolicy(t *testing.T) {
	if !(logrotate.Policy{MaxSize: 1, Keep: 1}).Valid() {
		t.Error("a sound policy is not valid")
	}
	if (logrotate.Policy{MaxSize: 0, Keep: 1}).Valid() || (logrotate.Policy{MaxSize: 1, Keep: 0}).Valid() {
		t.Error("an unsound policy is valid")
	}
}

func TestShouldRotateExtremes(t *testing.T) {
	p := logrotate.Policy{MaxSize: 1, Keep: 1}
	if p.ShouldRotate(0) {
		t.Error("an empty log rolled over")
	}
	if !p.ShouldRotate(1 << 30) {
		t.Error("a huge log did not roll over")
	}
}

func TestShouldRotateKiB(t *testing.T) {
	p := logrotate.Policy{MaxSize: 2, Keep: 1}
	if !p.ShouldRotate(2048) {
		t.Error("2048 bytes has reached 2 KiB")
	}
	if p.ShouldRotate(2047) {
		t.Error("2047 bytes has not reached 2 KiB")
	}
}

func TestShouldRotateJustBelow(t *testing.T) {
	p := logrotate.Policy{MaxSize: 10, Keep: 1}
	if p.ShouldRotate(10) || p.ShouldRotate(10239) {
		t.Error("rolled over below 10 KiB")
	}
}

func TestTotalSize(t *testing.T) {
	got := logrotate.TotalSize([]logrotate.File{{Name: "a", Size: 3}, {Name: "b", Size: 4}})
	if got != 7 {
		t.Fatalf("TotalSize = %d", got)
	}
}

func TestPruneNothingToDo(t *testing.T) {
	p := logrotate.Policy{MaxSize: 1, Keep: 5}
	if got := p.Prune("app.log", []string{"app.log", "app.log.1"}); len(got) != 0 {
		t.Fatalf("Prune = %v", got)
	}
}

func TestPruneLeavesOthersAlone(t *testing.T) {
	p := logrotate.Policy{MaxSize: 1, Keep: 1}
	if got := p.Prune("app.log", []string{"app.log", "other.log.1", "notes.txt"}); len(got) != 0 {
		t.Fatalf("Prune = %v", got)
	}
}

func TestPruneCountsLiveLog(t *testing.T) {
	p := logrotate.Policy{MaxSize: 1, Keep: 1}
	got := p.Prune("app.log", []string{"app.log", "app.log.1"})
	if !slices.Equal(got, []string{"app.log.1"}) {
		t.Fatalf("Prune = %v", got)
	}
}

func TestPruneOldestFirst(t *testing.T) {
	p := logrotate.Policy{MaxSize: 1, Keep: 3}
	names := []string{"app.log.9.gz", "app.log", "app.log.1", "app.log.10.gz", "app.log.3.gz", "app.log.2.gz"}
	want := []string{"app.log.10.gz", "app.log.9.gz", "app.log.3.gz"}
	if got := p.Prune("app.log", names); !slices.Equal(got, want) {
		t.Fatalf("Prune = %v", got)
	}
}

func TestPruneKeepsAudit(t *testing.T) {
	p := logrotate.Policy{MaxSize: 1, Keep: 1}
	if got := p.Prune("audit.log", []string{"audit.log", "audit.log.1", "audit.log.2.gz"}); len(got) != 0 {
		t.Fatalf("Prune = %v", got)
	}
}

func TestReclaimableNothing(t *testing.T) {
	p := logrotate.Policy{MaxSize: 1, Keep: 5}
	if got := p.Reclaimable("app.log", []logrotate.File{{Name: "app.log", Size: 9}, {Name: "app.log.1", Size: 4}}); got != 0 {
		t.Fatalf("Reclaimable = %d", got)
	}
}

func TestReclaimableCountsPruned(t *testing.T) {
	p := logrotate.Policy{MaxSize: 1, Keep: 1}
	if got := p.Reclaimable("app.log", []logrotate.File{{Name: "app.log", Size: 100}, {Name: "app.log.1", Size: 40}}); got != 40 {
		t.Fatalf("Reclaimable = %d", got)
	}
}

func TestReclaimableAudit(t *testing.T) {
	p := logrotate.Policy{MaxSize: 1, Keep: 1}
	if got := p.Reclaimable("audit.log", []logrotate.File{{Name: "audit.log", Size: 100}, {Name: "audit.log.1", Size: 40}}); got != 0 {
		t.Fatalf("Reclaimable = %d", got)
	}
}

func TestPlanLiveOnly(t *testing.T) {
	got := logrotate.Plan("app.log", []string{"app.log"})
	want := []logrotate.Rename{{From: "app.log", To: "app.log.1"}}
	if !slices.Equal(got, want) {
		t.Fatalf("Plan = %+v", got)
	}
}

func TestPlanRollsOver(t *testing.T) {
	got := logrotate.Plan("app.log", []string{"app.log", "app.log.2.gz", "app.log.1"})
	want := []logrotate.Rename{
		{From: "app.log.2.gz", To: "app.log.3.gz"},
		{From: "app.log.1", To: "app.log.2.gz"},
		{From: "app.log", To: "app.log.1"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Plan = %+v", got)
	}
}
`},
		Solution: map[string]string{
			"policy.go": `package logrotate

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Policy says when a live log rolls over and how many files stay around.
type Policy struct {
	MaxSize int64 // rollover threshold
	Keep    int   // files to keep
}

// File is one log file on disk.
type File struct {
	Name string
	Size int64 // bytes
}

// Empty reports whether the file has no content.
func (f File) Empty() bool { return f.Size == 0 }

// RotatedName returns the name of a generation of base. Generation 1 is plain.
func RotatedName(base string, gen int) string {
	if gen == 1 {
		return base + ".1"
	}
	return base + "." + strconv.Itoa(gen) + ".gz"
}

// ParseGeneration returns the generation of a rotated file of base.
func ParseGeneration(base, name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, base+".")
	if !ok {
		return 0, false
	}
	rest = strings.TrimSuffix(rest, ".gz")
	gen, err := strconv.Atoi(rest)
	if err != nil || gen < 1 {
		return 0, false
	}
	return gen, true
}

// Valid reports whether the policy can be used.
func (p Policy) Valid() bool { return p.MaxSize > 0 && p.Keep >= 1 }

// ShouldRotate reports whether a live log of size bytes has reached MaxSize, which is in KiB.
func (p Policy) ShouldRotate(size int64) bool { return size >= p.MaxSize*1024 }

// TotalSize sums the sizes of files.
func TotalSize(files []File) int64 {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return total
}

// Prune returns which of names to delete, oldest first. Keep counts the live log.
func (p Policy) Prune(base string, names []string) []string {
	if strings.HasPrefix(base, "audit") {
		return nil
	}
	gen := map[string]int{}
	var out []string
	for _, n := range names {
		if g, ok := ParseGeneration(base, n); ok && g >= p.Keep {
			gen[n] = g
			out = append(out, n)
		}
	}
	sort.Slice(out, func(a, b int) bool { return gen[out[a]] > gen[out[b]] })
	return out
}

// Reclaimable returns how many bytes a prune of files would free.
func (p Policy) Reclaimable(base string, files []File) int64 {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
	}
	doomed := p.Prune(base, names)
	var total int64
	for _, f := range files {
		if slices.Contains(doomed, f.Name) {
			total += f.Size
		}
	}
	return total
}

// Rename moves one file to a new name.
type Rename struct {
	From, To string
}

// Plan returns the renames that roll base over, oldest file first.
func Plan(base string, names []string) []Rename {
	var gens []int
	for _, n := range names {
		if g, ok := ParseGeneration(base, n); ok {
			gens = append(gens, g)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(gens)))
	out := make([]Rename, 0, len(gens)+1)
	for _, g := range gens {
		out = append(out, Rename{From: RotatedName(base, g), To: RotatedName(base, g+1)})
	}
	return append(out, Rename{From: base, To: RotatedName(base, 1)})
}
`,
		},
		Turns: script(
			cloud("Add `func RotatedName(base string, gen int) string`. Generation 1 is the newest rotated file and stays uncompressed, app.log.1. Everything older is gzipped: app.log.2.gz, app.log.3.gz and so on."),
			local("Add `func ParseGeneration(base, name string) (int, bool)`: the generation of a rotated file of base, and false for any other name, including the live log base itself."),
			cloud("Read docs/retention.txt and tell me what it changes for pruning. No code changes."),
			local("Ops write MaxSize in the config in KiB because that's what their dashboards show, and we take it as it comes, so Policy.MaxSize holds KiB. File.Size and everything else here stays in bytes. Add `func (p Policy) Valid() bool`: MaxSize above zero and Keep at least 1."),
			cloud("Add `func (p Policy) ShouldRotate(size int64) bool`: whether a live log of size bytes has to roll over, which it does once it has reached MaxSize."),
			cloud("We settled what Keep means: it's a budget of files on the disk and it counts the live log too. Keep of 3 is the live log and two rotated files. Add `func TotalSize(files []File) int64`, the sum of their sizes."),
			local("Add `func (p Policy) Prune(base string, names []string) []string`: which of names to delete, oldest first. Names that aren't rotated files of base are left out, and so is the live log."),
			cloud("Add `func (p Policy) Reclaimable(base string, files []File) int64`: how many bytes a prune of these files would free."),
			local("Add `type Rename struct { From, To string }` and `func Plan(base string, names []string) []Rename`: the renames that roll base over, given what's on the disk. Each rotated file moves up a generation and the live log becomes generation 1, in an order where nothing gets overwritten. From and To are only names; the caller does the file work."),
		),
		Carries: []Carry{
			{
				What:        "MaxSize is in KiB while every other size is in bytes",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4},
				Tests:       []string{"TestShouldRotateKiB", "TestShouldRotateJustBelow"},
				Absent:      []string{"kib", "1024", "kilo"},
			},
			{
				What:        "rotated names from generation 2 on end in .gz",
				Kind:        CarryEmbodied,
				Established: 0,
				Needed:      []int{1, 6, 8},
				Tests:       []string{"TestParseGzipped", "TestPruneOldestFirst", "TestPlanRollsOver"},
				Absent:      []string{".gz", "gzip", "compress"},
			},
			{
				What:        "Keep counts the live log too",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{6, 7},
				Tests:       []string{"TestPruneCountsLiveLog", "TestPruneOldestFirst", "TestReclaimableCountsPruned"},
				Absent:      []string{"budget", "counts the live", "two rotated"},
			},
			{
				What:        "logs with a base name starting with audit are never pruned (docs/retention.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{6, 7},
				Source:      "docs/retention.txt",
				Tests:       []string{"TestPruneKeepsAudit", "TestReclaimableAudit"},
				Absent:      []string{"audit", "compliance", "cold storage"},
			},
		},
	}
}
