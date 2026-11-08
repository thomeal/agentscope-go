package s2

import (
	"encoding/hex"
	"encoding/json"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

const (
	maxFixtureTokens  = 1500
	maxFinishedTokens = 3000
	maxTurnChars      = 600
	maxChangeLines    = 30
	minHiddenTests    = 8
)

// estimateTokens is a deliberately pessimistic estimate for Go source, at three
// characters per token.
func estimateTokens(files map[string]string) int {
	chars := 0
	for _, content := range files {
		chars += len(content)
	}
	return chars / 3
}

// allTasks is every workload. Every rule on a task holds for each of them, within its limits.
func allTasks() []Task {
	return append(append(append(Tasks(), Acceptance()...), Long()...), Long2()...)
}

// taskLimits are the sizes a task must keep to.
type taskLimits struct {
	minTurns, maxTurns, minSwitches, minOwned int
	minFiles, maxFiles                        int
	maxFixture, maxFinished                   int
	minHidden, minCarries                     int
}

// limitsOf gives a long task room to run several times past a small window. A file has its
// own limit there, in TestLongTaskShape, so one file still fits a small window with room to work.
func limitsOf(task Task) taskLimits {
	if task.Set == "long" {
		return taskLimits{
			minTurns: 20, maxTurns: 32, minSwitches: 8, minOwned: 8,
			minFiles: 5, maxFiles: 14,
			maxFixture: 6000, maxFinished: longMaxFinishedChars / 3,
			minHidden: 20, minCarries: 6,
		}
	}
	return taskLimits{
		minTurns: 6, maxTurns: 10, minSwitches: 3, minOwned: 3,
		minFiles: 3, maxFiles: 8,
		maxFixture: maxFixtureTokens, maxFinished: maxFinishedTokens,
		minHidden: minHiddenTests, minCarries: 2,
	}
}

const (
	longMaxFileChars     = 4500
	longMinFinishedChars = 18000
	longMaxFinishedChars = 36000
	longMinGap           = 8
)

// TestLongTaskShape checks what makes a long task long. Whole-file writes are what grow a
// session, so the finished module has to be large, with no file too large for a small window
// to read and rewrite. A carried constraint has to be needed long after it was settled, on the
// other side, and the task has to be owned in stretches as well as single turns.
func TestLongTaskShape(t *testing.T) {
	for _, task := range append(Long(), Long2()...) {
		t.Run(task.ID, func(t *testing.T) {
			finished := maps.Clone(task.Files)
			maps.Copy(finished, task.Solution)
			chars := 0
			for name, content := range finished {
				chars += len(content)
				if len(content) > longMaxFileChars {
					t.Errorf("%s has %d characters", name, len(content))
				}
			}
			if chars < longMinFinishedChars || chars > longMaxFinishedChars {
				t.Errorf("the finished module has %d characters", chars)
			}
			far := 0
			for _, c := range task.Carries {
				from := task.Turns[c.Established].Owner
				for _, n := range c.Needed {
					if n-c.Established >= longMinGap && task.Turns[n].Owner != from {
						far++
						break
					}
				}
			}
			if far*2 < len(task.Carries) {
				t.Errorf("only %d of %d carries are needed on the other side %d or more turns later", far, len(task.Carries), longMinGap)
			}
			longest, run := 0, 0
			for i, turn := range task.Turns {
				if i > 0 && turn.Owner == task.Turns[i-1].Owner {
					run++
				} else {
					run = 1
				}
				longest = max(longest, run)
			}
			if longest < 3 {
				t.Errorf("the longest stretch on one side is %d turns", longest)
			}
		})
	}
}

// frozenAcceptanceHash is the held-out set as prereg.md froze it.
const frozenAcceptanceHash = "4bfe9974fe50d326b358112a36265d074dc4683921f39f18546089bf92788bbf"

func TestAcceptanceIsFrozen(t *testing.T) {
	if n := len(Acceptance()); n != 30 {
		t.Errorf("%d held-out tasks", n)
	}
	if got := AcceptanceHash(); got != frozenAcceptanceHash {
		t.Errorf("AcceptanceHash = %s, frozen at %s", got, frozenAcceptanceHash)
	}
}

// frozenLongHash is the long set as prereg.md froze it, before any model saw a task.
const frozenLongHash = "ab80952089cc26d136357d8517f01c7e12c9b6fb07a5f16f1c46789860d6dcbc"

func TestLongIsFrozen(t *testing.T) {
	if n := len(Long()); n != 12 {
		t.Errorf("%d long tasks", n)
	}
	if got := LongHash(); got != frozenLongHash {
		t.Errorf("LongHash = %s, frozen at %s", got, frozenLongHash)
	}
}

// frozenLong2Hash is the second long set as Amendment 20's freeze note froze it, before any
// model saw a task.
const frozenLong2Hash = "238247008249cee92547f386099210260b77b350a72d7875b87880c18ed2449d"

func TestLong2IsFrozen(t *testing.T) {
	if n := len(Long2()); n != 12 {
		t.Errorf("%d tasks in the second long set", n)
	}
	if got := Long2Hash(); got != frozenLong2Hash {
		t.Errorf("Long2Hash = %s, frozen at %s", got, frozenLong2Hash)
	}
}

func TestTaskSet(t *testing.T) {
	if n := len(Tasks()); n != 15 {
		t.Fatalf("%d tuning tasks", n)
	}
	sets := map[string]string{}
	for _, task := range Tasks() {
		sets[task.ID] = "tuning"
	}
	for _, task := range Acceptance() {
		sets[task.ID] = "acceptance"
	}
	for _, task := range append(Long(), Long2()...) {
		sets[task.ID] = "long"
	}
	if len(sets) != len(allTasks()) {
		t.Errorf("%d distinct IDs in %d tasks", len(sets), len(allTasks()))
	}
	ids := map[string]bool{}
	for _, task := range allTasks() {
		ids[task.ID] = true
		if task.Set != sets[task.ID] {
			t.Errorf("%s: set %q", task.ID, task.Set)
		}
		if task.System == "" {
			t.Errorf("%s: no system prompt", task.ID)
		}
	}
	for id := range forgetful {
		if !ids[id] {
			t.Errorf("forgetful variants for unknown task %s", id)
		}
	}
}

func TestTurnScripts(t *testing.T) {
	for _, task := range allTasks() {
		t.Run(task.ID, func(t *testing.T) {
			lim := limitsOf(task)
			if n := len(task.Turns); n < lim.minTurns || n > lim.maxTurns {
				t.Errorf("%d turns", n)
			}
			switches := 0
			owned := map[corpus.Side]int{}
			for i, turn := range task.Turns {
				if turn.Index != i {
					t.Errorf("turn %d has Index %d", i, turn.Index)
				}
				if turn.Owner != corpus.SideCloud && turn.Owner != corpus.SideLocal {
					t.Errorf("turn %d has owner %q", i, turn.Owner)
				}
				if i > 0 && turn.Owner != task.Turns[i-1].Owner {
					switches++
				}
				owned[turn.Owner]++
				if turn.User == "" || len(turn.User) > maxTurnChars {
					t.Errorf("turn %d has %d characters", i, len(turn.User))
				}
			}
			if switches < lim.minSwitches {
				t.Errorf("%d switches", switches)
			}
			if owned[corpus.SideCloud] < lim.minOwned || owned[corpus.SideLocal] < lim.minOwned {
				t.Errorf("turns owned: %v", owned)
			}
		})
	}
}

func TestFixtureLimits(t *testing.T) {
	for _, task := range allTasks() {
		t.Run(task.ID, func(t *testing.T) {
			lim := limitsOf(task)
			if n := len(task.Files); n < lim.minFiles || n > lim.maxFiles {
				t.Errorf("%d files", n)
			}
			if task.Files["go.mod"] != "module fixture\n\ngo 1.25\n" {
				t.Errorf("go.mod is %q", task.Files["go.mod"])
			}
			if n := estimateTokens(task.Files); n >= lim.maxFixture {
				t.Errorf("about %d tokens in the fixture", n)
			}
			finished := maps.Clone(task.Files)
			maps.Copy(finished, task.Solution)
			if n := estimateTokens(finished); n >= lim.maxFinished {
				t.Errorf("about %d tokens in the finished module", n)
			}
			visibleTest := false
			for name := range task.Files {
				visibleTest = visibleTest || strings.HasSuffix(name, "_test.go")
			}
			if !visibleTest {
				t.Error("no visible test")
			}
			if len(task.Solution) == 0 {
				t.Error("no solution")
			}
			for name := range task.Solution {
				if _, hidden := task.Hidden[name]; hidden || strings.HasSuffix(name, "_test.go") {
					t.Errorf("solution file %s is a test", name)
				}
			}
		})
	}
}

// TestChangeSizes checks the two measurable sides of "at most about 30 lines per
// turn". The solution is one final state, so a turn's change is not observable by
// itself: no function may exceed the limit and neither may the average per working
// turn.
func TestChangeSizes(t *testing.T) {
	for _, task := range allTasks() {
		t.Run(task.ID, func(t *testing.T) {
			added := 0
			for name, content := range task.Solution {
				before := strings.Split(task.Files[name], "\n")
				for _, line := range strings.Split(content, "\n") {
					if i := slices.Index(before, line); i >= 0 {
						before = slices.Delete(before, i, i+1)
					} else {
						added++
					}
				}
				if !strings.HasSuffix(name, ".go") {
					continue
				}
				fset := token.NewFileSet()
				file, err := parser.ParseFile(fset, name, content, parser.SkipObjectResolution)
				if err != nil {
					t.Fatal(err)
				}
				for _, decl := range file.Decls {
					if lines := fset.Position(decl.End()).Line - fset.Position(decl.Pos()).Line + 1; lines > maxChangeLines {
						t.Errorf("%s: a declaration at line %d has %d lines", name, fset.Position(decl.Pos()).Line, lines)
					}
				}
			}
			working := 0
			for _, turn := range task.Turns {
				if !strings.Contains(turn.User, "No code changes") {
					working++
				}
			}
			if added > working*maxChangeLines {
				t.Errorf("%d added lines over %d working turns", added, working)
			}
		})
	}
}

// TestFixtureSources checks every Go file a task carries: it parses, is formatted,
// uses only the standard library and the fixture itself, and hidden tests are
// external test packages, so that they see only exported names and cannot collide
// with anything an assistant declares.
func TestFixtureSources(t *testing.T) {
	for _, task := range allTasks() {
		t.Run(task.ID, func(t *testing.T) {
			check := func(kind string, files map[string]string) {
				for name, src := range files {
					if !strings.HasSuffix(name, ".go") {
						continue
					}
					formatted, err := format.Source([]byte(src))
					if err != nil {
						t.Errorf("%s %s: %v", kind, name, err)
						continue
					}
					if string(formatted) != src {
						t.Errorf("%s %s is not gofmt'd; want:\n%s", kind, name, formatted)
					}
					file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
					if err != nil {
						t.Errorf("%s %s: %v", kind, name, err)
						continue
					}
					for _, spec := range file.Imports {
						imported, _ := strconv.Unquote(spec.Path.Value)
						first, _, _ := strings.Cut(imported, "/")
						if strings.Contains(first, ".") {
							t.Errorf("%s %s imports %s", kind, name, imported)
						}
					}
					if kind == "hidden" && !strings.HasSuffix(file.Name.Name, "_test") {
						t.Errorf("hidden file %s is in package %s", name, file.Name.Name)
					}
					if strings.Contains(src, "`") {
						t.Errorf("%s %s contains a backtick", kind, name)
					}
				}
			}
			check("initial", task.Files)
			check("hidden", task.Hidden)
			check("solution", task.Solution)
			if len(task.Hidden) == 0 {
				t.Error("no hidden tests")
			}
			for name := range task.Hidden {
				if base := path.Base(name); base != hiddenName {
					t.Errorf("hidden file %s should be named %s", name, hiddenName)
				}
				if _, ok := task.Files[name]; ok {
					t.Errorf("hidden file %s is also in the initial files", name)
				}
			}
		})
	}
}

// TestCarries checks that each task really has the dependence it claims, and that
// no later turn restates what it depends on.
func TestCarries(t *testing.T) {
	for _, task := range allTasks() {
		t.Run(task.ID, func(t *testing.T) {
			hidden, err := hiddenTests(task.Hidden)
			if err != nil {
				t.Fatal(err)
			}
			lim := limitsOf(task)
			if len(hidden) < lim.minHidden {
				t.Errorf("%d hidden tests", len(hidden))
			}
			names := slices.Collect(maps.Values(hidden))
			checked := map[string]bool{}
			directions := map[[2]corpus.Side]bool{}
			userOnly := 0
			if len(task.Carries) < lim.minCarries {
				t.Errorf("%d carries", len(task.Carries))
			}
			for _, c := range task.Carries {
				if c.Established < 0 || c.Established >= len(task.Turns) {
					t.Errorf("%q: established at turn %d", c.What, c.Established)
					continue
				}
				from := task.Turns[c.Established].Owner
				otherSide := false
				for _, n := range c.Needed {
					if n <= c.Established || n >= len(task.Turns) {
						t.Errorf("%q: needed at turn %d", c.What, n)
						continue
					}
					if to := task.Turns[n].Owner; to != from {
						otherSide = true
						directions[[2]corpus.Side{from, to}] = true
					}
				}
				if !otherSide {
					t.Errorf("%q is never needed by the other side", c.What)
				}
				if len(c.Tests) == 0 || len(c.Absent) == 0 {
					t.Errorf("%q: %d tests, %d absent phrases", c.What, len(c.Tests), len(c.Absent))
				}
				for _, name := range c.Tests {
					checked[name] = true
					if !slices.Contains(names, name) {
						t.Errorf("%q names the unknown test %s", c.What, name)
					}
				}
				for _, turn := range task.Turns[c.Established+1:] {
					text := strings.ToLower(turn.User)
					for _, phrase := range c.Absent {
						if strings.Contains(text, phrase) {
							t.Errorf("turn %d restates %q: it contains %q", turn.Index, c.What, phrase)
						}
					}
				}
				switch c.Kind {
				case CarryUser:
					userOnly++
					for name, content := range task.Files {
						if name == "go.mod" {
							continue // the same in every task
						}
						text := strings.ToLower(content)
						for _, phrase := range c.Absent {
							if strings.Contains(text, phrase) {
								t.Errorf("%s gives away %q: it contains %q", name, c.What, phrase)
							}
						}
					}
				case CarryDiscovered:
					if _, ok := task.Files[c.Source]; !ok {
						t.Errorf("%q: no file %s", c.What, c.Source)
					}
					if !strings.Contains(task.Turns[c.Established].User, c.Source) {
						t.Errorf("%q: turn %d does not point at %s", c.What, c.Established, c.Source)
					}
				case CarryEmbodied:
				default:
					t.Errorf("%q has kind %q", c.What, c.Kind)
				}
			}
			if userOnly == 0 {
				t.Error("no constraint is carried only by the conversation")
			}
			if !directions[[2]corpus.Side{corpus.SideCloud, corpus.SideLocal}] || !directions[[2]corpus.Side{corpus.SideLocal, corpus.SideCloud}] {
				t.Errorf("not carried in both directions: %v", directions)
			}
			if float64(len(checked)) < float64(len(names))/3 {
				t.Errorf("only %d of %d hidden tests check a carried constraint", len(checked), len(names))
			}
			for i := range task.Carries {
				found := false
				for _, v := range forgetful[task.ID] {
					found = found || v.carry == i
				}
				if !found {
					t.Errorf("no forgetful variant for %q", task.Carries[i].What)
				}
			}
		})
	}
}

// TestWorkloadSummary logs one line per task, for reports.
func TestWorkloadSummary(t *testing.T) {
	kinds := map[string]int{}
	for _, task := range allTasks() {
		switches := 0
		owners := ""
		for i, turn := range task.Turns {
			owners += strings.ToUpper(string(turn.Owner)[:1])
			if i > 0 && turn.Owner != task.Turns[i-1].Owner {
				switches++
			}
		}
		hidden, err := hiddenTests(task.Hidden)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s turns=%d switches=%d owners=%s hidden=%d tokens=%d carries=%d",
			task.ID, len(task.Turns), switches, owners, len(hidden), estimateTokens(task.Files), len(task.Carries))
		for _, c := range task.Carries {
			kinds[c.Kind]++
			t.Logf("    %s %d->%v %s", c.Kind, c.Established, c.Needed, c.What)
		}
	}
	t.Logf("carries by kind: %v", kinds)
}

// TestHashIgnoresLineEndings builds this package from copies of its sources with
// CRLF line endings, as a Windows checkout may have them, and compares the hash.
// A frozen hash must not depend on how the repository was checked out.
func TestHashIgnoresLineEndings(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil || len(sources) == 0 {
		t.Fatalf("sources: %v, %v", sources, err)
	}
	dir := t.TempDir()
	replace := map[string]string{}
	for _, name := range sources {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		target := filepath.Join(dir, name)
		if err := os.WriteFile(target, []byte(strings.ReplaceAll(text, "\n", "\r\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		abs, err := filepath.Abs(name)
		if err != nil {
			t.Fatal(err)
		}
		replace[abs] = target
	}
	overlay, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-overlay="+overlayPath, "-run", "^TestDeterminism$", "-v", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hash "+Hash()) {
		t.Fatalf("the CRLF build reports another hash than %s:\n%s", Hash(), out)
	}
}

func TestDeterminism(t *testing.T) {
	if !reflect.DeepEqual(Tasks(), Tasks()) {
		t.Fatal("Tasks differs between calls")
	}
	if !reflect.DeepEqual(Acceptance(), Acceptance()) {
		t.Fatal("Acceptance differs between calls")
	}
	t.Logf("acceptance hash %s", AcceptanceHash())
	first := Hash()
	if raw, err := hex.DecodeString(first); err != nil || len(raw) != 32 {
		t.Fatalf("Hash = %q", first)
	}
	changed := Tasks()
	changed[0].Files["go.mod"] = "changed"
	changed[0].Turns[0].User = "changed"
	if Hash() != first {
		t.Fatal("changing a returned task changed the workload")
	}
	a, err := json.Marshal(allTasks())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(a), `\r`) {
		t.Fatal("task text contains a carriage return")
	}
	t.Logf("hash %s", first)
}
