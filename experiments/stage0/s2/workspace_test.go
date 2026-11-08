package s2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func assertVisiblePass(t *testing.T, w *Workspace) {
	t.Helper()
	out, err := w.RunTests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "FAIL") || strings.Contains(out, "panic") {
		t.Fatalf("visible tests fail:\n%s", out)
	}
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// assertHiddenUnreachable tries to get at the hidden tests through the tools.
func assertHiddenUnreachable(t *testing.T, w *Workspace, task Task) {
	t.Helper()
	listing, err := w.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := hiddenTests(task.Hidden)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range task.Hidden {
		if strings.Contains(listing, name) {
			t.Errorf("ListFiles shows %s", name)
		}
		for _, attempt := range []string{name, "./" + name, strings.ToUpper(name), strings.ReplaceAll(name, "/", `\`)} {
			if got, err := w.ReadFile(attempt); err == nil {
				t.Errorf("ReadFile(%q) returned %d bytes", attempt, len(got))
			}
			if err := w.WriteFile(attempt, "package x\n"); err == nil {
				t.Errorf("WriteFile(%q) succeeded", attempt)
			}
		}
		for _, visible := range strings.Split(listing, "\n") {
			content, err := w.ReadFile(visible)
			if err != nil {
				t.Errorf("ReadFile(%q): %v", visible, err)
				continue
			}
			if strings.Contains(content, src) {
				t.Errorf("%s contains the hidden file %s", visible, name)
			}
			for _, reported := range hidden {
				_, fn, ok := strings.Cut(reported, ".")
				if !ok {
					fn = reported
				}
				if strings.Contains(content, "func "+fn+"(") {
					t.Errorf("%s contains the hidden test %s", visible, fn)
				}
			}
		}
	}
	for path := range snapshot(t, w.dir) {
		for name := range task.Hidden {
			if strings.EqualFold(path, name) {
				t.Errorf("%s is on disk in the workspace", path)
			}
		}
	}
}

// TestEveryTask runs each task's required end-to-end properties.
func TestEveryTask(t *testing.T) {
	for _, task := range append(append(Tasks(), Long()...), Long2()...) {
		t.Run(task.ID, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			w, err := NewWorkspace(task, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}

			build := exec.CommandContext(ctx, "go", "build", "./...")
			build.Dir = w.dir
			build.Env = goEnv()
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("the initial workspace does not build: %v\n%s", err, out)
			}
			assertVisiblePass(t, w)
			assertHiddenUnreachable(t, w, task)

			before := snapshot(t, w.dir)
			initial, err := w.Score(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if initial.Passed == initial.Total || initial.Score >= 1.0 {
				t.Errorf("hidden tests pass on the initial files: %+v", initial)
			}
			if !maps.Equal(before, snapshot(t, w.dir)) {
				t.Error("Score changed the initial workspace")
			}

			for name, content := range task.Solution {
				if err := w.WriteFile(name, content); err != nil {
					t.Fatal(err)
				}
			}
			assertVisiblePass(t, w)
			before = snapshot(t, w.dir)
			solved, err := w.Score(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if solved.Score != 1.0 || solved.Passed != solved.Total || solved.BuildFailed || len(solved.Failed) != 0 {
				t.Errorf("the solution scores %+v", solved)
			}
			want, err := hiddenTests(task.Hidden)
			if err != nil {
				t.Fatal(err)
			}
			if solved.Total != len(want) || solved.Total < minHiddenTests {
				t.Errorf("Total = %d, want %d", solved.Total, len(want))
			}
			if !maps.Equal(before, snapshot(t, w.dir)) {
				t.Error("Score changed the solved workspace")
			}
			assertHiddenUnreachable(t, w, task)
		})
	}
}

func TestPathsCannotEscape(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "workspace")
	task := Tasks()[0]
	w, err := NewWorkspace(task, dir)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	escapes := []string{
		"..", "../outside.txt", "ops/../../outside.txt", `..\outside.txt`, `ops\..\..\outside.txt`,
		"/etc/hosts", outside, filepath.ToSlash(outside), `C:\Windows\win.ini`, "C:/Windows/win.ini", `\\host\share\x`,
		"job.go:stream", "job.go.", "job.go ", "", ".", "./", "x\x00y", "ops/..",
	}
	for _, p := range escapes {
		if got, err := w.ReadFile(p); err == nil {
			t.Errorf("ReadFile(%q) returned %q", p, got)
		}
		if err := w.WriteFile(p, "written"); err == nil {
			t.Errorf("WriteFile(%q) succeeded", p)
		}
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
		t.Errorf("the outside file became %q, %v", data, err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 2 {
		t.Errorf("entries next to the workspace: %v, %v", entries, err)
	}
	if !maps.Equal(snapshot(t, dir), task.Files) {
		t.Error("a rejected write changed the workspace")
	}
	// Harmless spellings of a path inside the workspace still work.
	for _, p := range []string{"job.go", "./job.go", `.\job.go`, "ops//console.txt", `ops\console.txt`} {
		if _, err := w.ReadFile(p); err != nil {
			t.Errorf("ReadFile(%q): %v", p, err)
		}
	}
}

func TestNewWorkspaceRefusesUsedDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stale.go"), []byte("package stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWorkspace(Tasks()[0], dir); err == nil {
		t.Fatal("no error for a directory with files in it")
	}
}

func TestRunTestsOutputIsBounded(t *testing.T) {
	w, err := NewWorkspace(Tasks()[0], t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = w.WriteFile("noisy_test.go", `package sched

import (
	"fmt"
	"testing"
)

func TestNoisy(t *testing.T) {
	for i := range 20000 {
		fmt.Println("line of noise", i)
	}
	t.Fatal("LAST-WORDS")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := w.RunTests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) >= 2000 {
		t.Errorf("%d characters of output", len(out))
	}
	if !strings.Contains(out, "omitted") || !strings.Contains(out, "line of noise 0") || !strings.HasSuffix(strings.TrimSpace(out), "FAIL") {
		t.Errorf("output lost its head or tail:\n%s", out)
	}
	if strings.Contains(out, os.TempDir()) {
		t.Errorf("output names the scratch directory:\n%s", out)
	}
}

func TestHeadTailKeepsBothEnds(t *testing.T) {
	h := &headTail{limit: 10}
	for range 100 {
		if _, err := h.Write([]byte("0123456789")); err != nil {
			t.Fatal(err)
		}
	}
	got := h.String()
	if !strings.HasPrefix(got, "0123456789") || !strings.HasSuffix(got, "0123456789") || len(got) > 60 || !strings.Contains(got, "980 bytes omitted") {
		t.Fatalf("got %q", got)
	}
	if clipped := clip(strings.Repeat("é", 2000), 100); len(clipped) > 100 || !strings.Contains(clipped, "omitted") {
		t.Fatalf("clip gave %d bytes: %q", len(clipped), clipped)
	}
}

func TestBuildFailureScoresZero(t *testing.T) {
	task := Tasks()[0]
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range task.Solution {
		if err := w.WriteFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.WriteFile("broken.go", "package sched\n\nfunc Broken() { undefinedName() }\n"); err != nil {
		t.Fatal(err)
	}
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.BuildFailed || res.Score != 0 || res.Passed != 0 || res.Total == 0 || len(res.Failed) != res.Total {
		t.Fatalf("got %+v", res)
	}
	if !strings.Contains(res.Output, "undefinedName") {
		t.Errorf("the output does not show the failure:\n%s", res.Output)
	}
}

// A broken package elsewhere in the module is a build failure too, even with every
// hidden test passing.
func TestBuildFailureInAnotherPackage(t *testing.T) {
	task := Tasks()[0]
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range task.Solution {
		if err := w.WriteFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.WriteFile("extra/extra.go", "package extra\n\nfunc Broken() { undefinedName() }\n"); err != nil {
		t.Fatal(err)
	}
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.BuildFailed || res.Score != 0 {
		t.Fatalf("got %+v", res)
	}
}

// Tests an assistant writes must not move the score in either direction, whether
// by adding passing functions, reusing a hidden name or failing to compile.
func TestWorkspaceTestsDoNotAffectScore(t *testing.T) {
	task := Tasks()[0]
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range task.Solution {
		if err := w.WriteFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	extra := map[string]string{
		"mine_test.go":  "package sched_test\n\nimport \"testing\"\n\nfunc TestPeekLeavesJob(t *testing.T) { t.Fatal(\"collides\") }\nfunc TestExtraOne(t *testing.T)    {}\nfunc TestExtraTwo(t *testing.T)    {}\n",
		"wrong_test.go": "package somethingelse\n\nfunc Broken() { undefinedName() }\n",
	}
	for name, content := range extra {
		if err := w.WriteFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want, err := hiddenTests(task.Hidden)
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != 1.0 || res.Total != len(want) || res.BuildFailed {
		t.Fatalf("got %+v", res)
	}
	if got := snapshot(t, w.dir); got["mine_test.go"] != extra["mine_test.go"] {
		t.Error("Score removed an assistant's test from the workspace")
	}
}

// Editing go.mod must have no effect on what runs, and cannot ask for a dependency.
func TestGoModIsFixed(t *testing.T) {
	task := Tasks()[0]
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range task.Solution {
		if err := w.WriteFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.WriteFile("go.mod", "module other\n\ngo 1.25\n\nrequire example.com/nothing v1.0.0\n"); err != nil {
		t.Fatal(err)
	}
	assertVisiblePass(t, w)
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != 1.0 {
		t.Fatalf("got %+v", res)
	}
}

// Asking for a module that isn't there must fail as a build failure straight away,
// with the go command refusing to look anything up. The module path is reserved,
// so this cannot succeed even if a lookup were to escape.
func TestExternalImportFailsOffline(t *testing.T) {
	w, err := NewWorkspace(Tasks()[0], t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := "package sched\n\nimport _ \"example.invalid/nothing\"\n"
	if err := w.WriteFile("external.go", src); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.BuildFailed || res.Score != 0 {
		t.Fatalf("got %+v", res)
	}
	if !strings.Contains(res.Output, "GOPROXY=off") && !strings.Contains(res.Output, "module lookup disabled") {
		t.Errorf("the go command did not report that lookups are off:\n%s", res.Output)
	}
	if took := time.Since(start); took > time.Minute {
		t.Errorf("took %v", took)
	}
	out, err := w.RunTests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "FAIL") && !strings.Contains(out, "GOPROXY=off") {
		t.Errorf("RunTests gave:\n%s", out)
	}
}

// A path under a hidden file would put a directory where Score writes that file.
func TestCannotBlockHiddenPath(t *testing.T) {
	task := Tasks()[0]
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name := range task.Hidden {
		for _, p := range []string{name + "/x.txt", strings.ToUpper(name) + "/x.txt", "./" + name + "/sub/x.txt"} {
			if err := w.WriteFile(p, "x"); err == nil {
				t.Errorf("WriteFile(%q) succeeded", p)
			}
		}
	}
	for name, content := range task.Solution {
		if err := w.WriteFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != 1.0 {
		t.Fatalf("got %+v", res)
	}
}

// Several tools may be called at once.
func TestConcurrentToolCalls(t *testing.T) {
	ctx := context.Background()
	task := Tasks()[0]
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range task.Solution {
		if err := w.WriteFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	errs := make(chan error, 40)
	for i := range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := w.WriteFile(fmt.Sprintf("note%d.txt", i), "note"); err != nil {
				errs <- err
			}
			if _, err := w.ListFiles(); err != nil {
				errs <- err
			}
			if _, err := w.ReadFile("queue.go"); err != nil {
				errs <- err
			}
			if i%4 == 0 {
				if _, err := w.RunTests(ctx); err != nil {
					errs <- err
				}
				res, err := w.Score(ctx)
				if err != nil {
					errs <- err
				} else if res.Score != 1.0 {
					errs <- fmt.Errorf("score %+v", res)
				}
			}
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func hangingTask() Task {
	return Task{
		ID:    "hang",
		Files: map[string]string{"go.mod": goMod, "spin.go": "package spin\n\nfunc Spin() {\n\tfor {\n\t}\n}\n"},
		Hidden: map[string]string{hiddenName: `package spin_test

import (
	"testing"

	spin "fixture"
)

func TestQuick(t *testing.T) {}

func TestSpins(t *testing.T) { spin.Spin() }

func TestAfterSpin(t *testing.T) {}
`},
	}
}

// A test that never returns must cost bounded time and leave a usable result, with
// the tests before and after it still counted.
func TestHangIsBounded(t *testing.T) {
	w, err := NewWorkspace(hangingTask(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w.scoreBinary = 3 * time.Second
	w.runBinary = 3 * time.Second
	if err := w.WriteFile("spin_test.go", "package spin\n\nimport \"testing\"\n\nfunc TestSpinVisible(t *testing.T) { Spin() }\n"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := w.RunTests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "FAIL") || len(out) >= 2000 {
		t.Errorf("RunTests gave:\n%s", out)
	}
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.Passed != 2 || res.Total != 3 || !slices.Equal(res.Failed, []string{"TestSpins"}) {
		t.Errorf("Score gave %+v", res)
	}
	if took := time.Since(start); took > 2*time.Minute {
		t.Errorf("took %v", took)
	}
}

// A panic ends the test binary, but what it costs is that one test, wherever it is
// declared. Otherwise a score would depend on the order of declaration.
func TestPanicCostsOnlyThatTest(t *testing.T) {
	task := Task{
		ID:    "panics",
		Files: map[string]string{"go.mod": goMod, "boom.go": "package boom\n\nfunc Boom() { panic(\"boom\") }\n"},
		Hidden: map[string]string{hiddenName: `package boom_test

import (
	"testing"

	boom "fixture"
)

func TestFirst(t *testing.T) {}

func TestBoomOne(t *testing.T) { boom.Boom() }

func TestMiddle(t *testing.T) {}

func TestBoomTwo(t *testing.T) { boom.Boom() }

func TestLast(t *testing.T) {}
`},
	}
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.Passed != 3 || res.Total != 5 || !slices.Equal(res.Failed, []string{"TestBoomOne", "TestBoomTwo"}) {
		t.Fatalf("got %+v", res)
	}
}

// A package that crashes before any test starts did not run at all, which counts
// the same as a build failure and must not loop.
func TestCrashDuringInitScoresZero(t *testing.T) {
	task := Task{
		ID:    "init",
		Files: map[string]string{"go.mod": goMod, "boom.go": "package boom\n\nfunc init() { panic(\"boom\") }\n\n// Fine does nothing.\nfunc Fine() {}\n"},
		Hidden: map[string]string{hiddenName: `package boom_test

import (
	"testing"

	boom "fixture"
)

func TestFine(t *testing.T) { boom.Fine() }
`},
	}
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.BuildFailed || res.Score != 0 || res.Total != 1 {
		t.Fatalf("got %+v", res)
	}
}

func TestCancelledContextIsAnError(t *testing.T) {
	w, err := NewWorkspace(Tasks()[0], t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.RunTests(ctx); err == nil {
		t.Error("RunTests gave no error")
	}
	if _, err := w.Score(ctx); err == nil {
		t.Error("Score gave no error")
	}
}

// Failures in a subpackage are named after it, and hidden tests there count.
func TestSubpackageHiddenTests(t *testing.T) {
	var task Task
	for _, candidate := range Tasks() {
		if candidate.ID == "s2-11-line-protocol" {
			task = candidate
		}
	}
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range task.Solution {
		if err := w.WriteFile(name, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.WriteFile("checksum/checksum.go", "package checksum\n\nfunc Sum(s string) int { return 131 }\n"); err != nil {
		t.Fatal(err)
	}
	res, err := w.Score(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Failed, "checksum.TestSumWraps") {
		t.Fatalf("got %+v", res)
	}
}

func TestToolSpecs(t *testing.T) {
	specs := ToolSpecs()
	var names []string
	for _, spec := range specs {
		names = append(names, spec.Name)
		if spec.Description == "" {
			t.Errorf("%s has no description", spec.Name)
		}
		var schema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(spec.Schema, &schema); err != nil {
			t.Errorf("%s: %v", spec.Name, err)
		}
		if schema.Type != "object" {
			t.Errorf("%s: type %q", spec.Name, schema.Type)
		}
		for _, required := range schema.Required {
			if _, ok := schema.Properties[required]; !ok {
				t.Errorf("%s requires the undeclared %s", spec.Name, required)
			}
		}
	}
	if !slices.Equal(names, []string{"list_files", "read_file", "write_file", "run_tests"}) {
		t.Errorf("tools: %v", names)
	}
}

func TestCall(t *testing.T) {
	ctx := context.Background()
	w, err := NewWorkspace(Tasks()[0], t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	listing, err := w.Call(ctx, "list_files", nil)
	if err != nil || !strings.Contains(listing, "queue.go") {
		t.Errorf("list_files: %q, %v", listing, err)
	}
	if got, err := w.Call(ctx, "read_file", map[string]any{"path": "job.go"}); err != nil || !strings.Contains(got, "type Job") {
		t.Errorf("read_file: %q, %v", got, err)
	}
	if _, err := w.Call(ctx, "write_file", map[string]any{"path": "sub/new.txt", "content": "hello"}); err != nil {
		t.Errorf("write_file: %v", err)
	}
	if got, err := w.ReadFile("sub/new.txt"); err != nil || got != "hello" {
		t.Errorf("read back: %q, %v", got, err)
	}
	if got, err := w.Call(ctx, "run_tests", map[string]any{}); err != nil || !strings.Contains(got, "ok") {
		t.Errorf("run_tests: %q, %v", got, err)
	}
	for name, args := range map[string]map[string]any{
		"delete_file": {},
		"read_file":   {"path": 3},
		"write_file":  {"path": "x.txt"},
	} {
		if _, err := w.Call(ctx, name, args); err == nil {
			t.Errorf("%s with %v gave no error", name, args)
		}
	}
	if _, err := w.Call(ctx, "read_file", nil); err == nil {
		t.Error("read_file without arguments gave no error")
	}
}

func TestViewRefusesAWriteOverAnUnseenVersion(t *testing.T) {
	task := Tasks()[0]
	w, err := NewWorkspace(task, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, b := w.View(), w.View()
	write := func(v *View, path, content string) error {
		_, err := v.Call(ctx, "write_file", map[string]any{"path": path, "content": content})
		return err
	}
	read := func(v *View, path string) {
		t.Helper()
		if _, err := v.Call(ctx, "read_file", map[string]any{"path": path}); err != nil {
			t.Fatal(err)
		}
	}
	const file = "queue.go"
	original := task.Files[file]

	if err := write(a, file, "new"); !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("a write before any read: %v", err)
	}
	if got, _ := w.ReadFile(file); got != original {
		t.Fatal("a refused write changed the file")
	}
	if err := write(a, "brand/new.go", "package brand\n"); err != nil {
		t.Fatalf("a new file: %v", err)
	}
	read(a, file)
	if err := write(a, file, original+"// a\n"); err != nil {
		t.Fatalf("after reading: %v", err)
	}
	if err := write(a, file, original+"// a2\n"); err != nil {
		t.Fatalf("over its own write: %v", err)
	}
	read(b, file)
	if err := write(a, file, original+"// a3\n"); err != nil {
		t.Fatal(err)
	}
	if err := write(b, file, original+"// b\n"); !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("over the other agent's change: %v", err)
	}
	if got, _ := w.ReadFile(file); got != original+"// a3\n" {
		t.Fatalf("a's change was lost: %q", got)
	}
	if err := write(b, file, original+"// a3\n"); err != nil {
		t.Fatalf("a write that changes nothing: %v", err)
	}
	read(b, file)
	if err := write(b, file, original+"// b\n"); err != nil {
		t.Fatalf("after reading again: %v", err)
	}
	if err := write(a, "../outside.go", "x"); err == nil || errors.Is(err, ErrStaleWrite) {
		t.Fatalf("a bad path keeps its own error: %v", err)
	}
	if err := write(a, hiddenName, "x"); err == nil || errors.Is(err, ErrStaleWrite) {
		t.Fatalf("a hidden path keeps its own error: %v", err)
	}
}

// Code a model wrote once took 58 GB in a test. The cap has to stop that, and the failure has
// to come back as output, so that it reads as the code failing and not the scorer.
func TestTestBinariesHaveAMemoryCap(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the cap is Windows-only")
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":      "module big\n\ngo 1.25\n",
		"big_test.go": "package big\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\nvar keep [][]byte\n\nfunc TestBig(t *testing.T) {\n\tfor i := 0; i < 5; i++ {\n\t\tb := make([]byte, 1<<30)\n\t\tfor j := 0; j < len(b); j += 4096 {\n\t\t\tb[j] = 1\n\t\t}\n\t\tkeep = append(keep, b)\n\t}\n\tfmt.Println(\"ALLOCATED\")\n}\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	err := goTest(context.Background(), dir, &out, "-count=1", "-timeout=60s", ".")
	if err == nil || strings.Contains(out.String(), "ALLOCATED") {
		t.Fatalf("the test allocated 5 GB: err %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "out of memory") {
		t.Errorf("the capped test did not run out of memory:\n%s", out.String())
	}
}

// A go command that cannot start prints nothing. That has to come back as the scorer's
// error, because a zero would blame the arm for it.
func TestSilentGoFailureIsNotAZero(t *testing.T) {
	src := t.TempDir()
	stub := "package main\n\nimport \"os\"\n\nfunc main() { os.Exit(1) }\n"
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}
	build := exec.Command("go", "build", "-o", filepath.Join(bin, name), "main.go")
	build.Dir = src
	build.Env = goEnv()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the stand-in: %v\n%s", err, out)
	}
	ws, err := NewWorkspace(Tasks()[0], t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if res, err := ws.Score(context.Background()); err == nil {
		t.Fatalf("Score returned %+v and no error", res)
	}
}
