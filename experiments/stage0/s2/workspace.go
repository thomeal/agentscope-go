package s2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	moduleName = "fixture"

	runOutputLimit   = 1900 // RunTests output stays under 2,000 characters
	scoreOutputLimit = 4000 // Result.Output is for the harness's records
	captureLimit     = 16 << 10
	maxFileBytes     = 1 << 20
	maxEventLine     = 1 << 20

	// A test binary that does not finish stops itself at the binary timeout. The
	// overall timeout is far longer because it also covers compilation, and when
	// it fires it means the machine stalled, which is not the code's fault.
	defaultRunBinary    = 30 * time.Second
	defaultRunOverall   = 150 * time.Second
	defaultScoreBinary  = 60 * time.Second
	defaultScoreOverall = 240 * time.Second
)

// Result is the score of a workspace against the hidden tests of its task.
type Result struct {
	Passed, Total int
	Score         float64
	Output        string
	BuildFailed   bool

	// Failed names the hidden test functions that did not pass, in sorted order.
	// A test in a subpackage is written package.TestName.
	Failed []string
}

// Workspace is a task's working directory, reached through the tool methods.
type Workspace struct {
	mu     sync.Mutex
	dir    string
	hidden map[string]string
	goMod  string

	runBinary, runOverall, scoreBinary, scoreOverall time.Duration
}

// NewWorkspace materialises t.Files in dir, which must be empty or not yet exist.
// Hidden test files are never written to dir.
func NewWorkspace(t Task, dir string) (*Workspace, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("s2: workspace dir: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("s2: workspace dir: %w", err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("s2: workspace dir: %w", err)
	}
	if len(entries) > 0 {
		return nil, fmt.Errorf("s2: workspace dir %s is not empty", abs)
	}
	w := &Workspace{
		dir:          abs,
		hidden:       make(map[string]string, len(t.Hidden)),
		goMod:        t.Files["go.mod"],
		runBinary:    defaultRunBinary,
		runOverall:   defaultRunOverall,
		scoreBinary:  defaultScoreBinary,
		scoreOverall: defaultScoreOverall,
	}
	for name, src := range t.Hidden {
		w.hidden[name] = src
	}
	for name, content := range t.Files {
		if err := w.WriteFile(name, content); err != nil {
			return nil, fmt.Errorf("s2: task %s: %w", t.ID, err)
		}
	}
	return w, nil
}

// cleanPath turns a tool-supplied path into a clean slash-separated path inside
// the workspace, or rejects it. A backslash counts as a separator on every
// platform. Colons and trailing dots or spaces are rejected because Windows would
// resolve them to some other file (drive letters, alternate streams, name
// stripping).
func cleanPath(p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("invalid path %q", p)
	}
	s := strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(s, "/") || filepath.IsAbs(p) || strings.Contains(s, ":") {
		return "", fmt.Errorf("path %q must be relative to the workspace", p)
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return "", fmt.Errorf("path %q leaves the workspace", p)
		}
		if seg != "." && (strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ")) {
			return "", fmt.Errorf("invalid path %q", p)
		}
	}
	clean := path.Clean(s)
	if clean == "." || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", fmt.Errorf("invalid path %q", p)
	}
	return clean, nil
}

// isHidden reports whether clean is a hidden test path or lies under one, which
// would otherwise put a directory where Score has to write that file. It compares
// without regard to case because the workspace may be on a file system that does
// the same.
func (w *Workspace) isHidden(clean string) bool {
	for name := range w.hidden {
		if strings.EqualFold(name, clean) {
			return true
		}
		if under := name + "/"; len(clean) > len(under) && strings.EqualFold(under, clean[:len(under)]) {
			return true
		}
	}
	return false
}

// ListFiles returns the workspace's files, one sorted relative path per line.
func (w *Workspace) ListFiles() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var names []string
	err := filepath.WalkDir(w.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(w.dir, p)
		if err != nil {
			return err
		}
		if name := filepath.ToSlash(rel); !w.isHidden(name) {
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("list files: %w", err)
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}

// ReadFile returns one file's contents. It rejects paths outside the workspace
// and hidden test paths.
func (w *Workspace) ReadFile(p string) (string, error) {
	clean, err := cleanPath(p)
	if err != nil {
		return "", err
	}
	if w.isHidden(clean) {
		return "", fmt.Errorf("read %s: not available", p)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(w.dir, filepath.FromSlash(clean)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("read %s: no such file", p)
		}
		return "", fmt.Errorf("read %s: %w", p, err)
	}
	return string(data), nil
}

// WriteFile creates a file or replaces its contents. It rejects paths outside the
// workspace and refuses hidden test paths.
func (w *Workspace) WriteFile(p, content string) error {
	clean, err := cleanPath(p)
	if err != nil {
		return err
	}
	if w.isHidden(clean) {
		return fmt.Errorf("write %s: path is reserved", p)
	}
	if len(content) > maxFileBytes {
		return fmt.Errorf("write %s: content is larger than %d bytes", p, maxFileBytes)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	full := filepath.Join(w.dir, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

// Call dispatches one of the tools named by ToolSpecs.
func (w *Workspace) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	switch name {
	case "list_files":
		return w.ListFiles()
	case "read_file":
		p, err := stringArg(args, "path")
		if err != nil {
			return "", err
		}
		return w.ReadFile(p)
	case "write_file":
		p, err := stringArg(args, "path")
		if err != nil {
			return "", err
		}
		content, err := stringArg(args, "content")
		if err != nil {
			return "", err
		}
		if err := w.WriteFile(p, content); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %s (%d bytes)", p, len(content)), nil
	case "run_tests":
		return w.RunTests(ctx)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// ErrStaleWrite is the refusal a View gives to a write over a version its agent has not seen.
var ErrStaleWrite = errors.New("this file has changed since you last read it, or you have not read it; read it, then write the whole file again")

// View is one agent's access to a Workspace. write_file replaces a whole file, so an agent
// writing from an old copy deletes whatever else has gone in since, and a shared workspace
// needs what a single agent's does not. A View refuses that write and the agent can read the
// file and try again. It compares content, and it knows nothing about what an agent remembers:
// a file it read before its context was cleared still counts as seen.
type View struct {
	w    *Workspace
	mu   sync.Mutex
	seen map[string]string
}

// View returns a new agent's access to w, which has seen nothing yet.
func (w *Workspace) View() *View { return &View{w: w, seen: map[string]string{}} }

// Call dispatches like Workspace.Call and adds the check on writes.
func (v *View) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	p, _ := args["path"].(string)
	key, keyErr := cleanPath(p)
	switch name {
	case "read_file":
		text, err := v.w.Call(ctx, name, args)
		if err == nil && keyErr == nil {
			v.saw(key, text)
		}
		return text, err
	case "write_file":
		content, _ := args["content"].(string)
		// A path ReadFile cannot read is new or invalid, and WriteFile deals with both.
		if keyErr == nil {
			if current, err := v.w.ReadFile(p); err == nil && current != content && !v.has(key, current) {
				return "", fmt.Errorf("write %s: %w", p, ErrStaleWrite)
			}
		}
		text, err := v.w.Call(ctx, name, args)
		if err == nil && keyErr == nil {
			v.saw(key, content)
		}
		return text, err
	}
	return v.w.Call(ctx, name, args)
}

func (v *View) saw(key, content string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.seen[key] = content
}

func (v *View) has(key, content string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	seen, ok := v.seen[key]
	return ok && seen == content
}

func stringArg(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", key)
	}
	return s, nil
}

// RunTests runs the workspace's own tests in a scratch copy and returns their
// output, bounded to its head and tail. Failing tests are a normal result. An
// error means the run could not be carried out or ctx ended.
func (w *Workspace) RunTests(ctx context.Context) (string, error) {
	scratch, err := w.scratchCopy(true)
	if err != nil {
		return "", err
	}
	defer removeScratch(scratch)

	runCtx, cancel := context.WithTimeout(ctx, w.runOverall)
	defer cancel()
	capture := &headTail{limit: captureLimit}
	runErr := goTest(runCtx, scratch, capture, "-count=1", "-vet=off", "-timeout="+w.runBinary.String(), "./...")
	if err := ctx.Err(); err != nil {
		return "", err
	}
	text := unscratch(capture.String(), scratch)
	switch {
	case runCtx.Err() != nil:
		text += "\nrun_tests: stopped after " + w.runOverall.String()
	case runErr != nil && !isTestExit(runErr):
		return "", fmt.Errorf("run tests: %w", runErr)
	}
	return clip(text, runOutputLimit), nil
}

// Score runs the hidden tests against a scratch copy and counts the hidden test
// functions that pass. The workspace's own _test.go files are left out of the
// copy, so tests an assistant wrote can neither raise the score nor collide with
// a hidden name. Any build failure scores 0. An error means no score could be
// determined: ctx ended, the run stalled, or the go tool could not start.
func (w *Workspace) Score(ctx context.Context) (Result, error) {
	want, err := hiddenTests(w.hidden)
	if err != nil {
		return Result{}, err
	}
	if len(want) == 0 {
		return Result{}, errors.New("s2: task has no hidden test functions")
	}
	scratch, err := w.scratchCopy(false)
	if err != nil {
		return Result{}, err
	}
	defer removeScratch(scratch)
	for name, src := range w.hidden {
		full := filepath.Join(scratch, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return Result{}, fmt.Errorf("s2: scratch: %w", err)
		}
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			return Result{}, fmt.Errorf("s2: scratch: %w", err)
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, w.scoreOverall)
	defer cancel()
	sink := &eventSink{passed: map[string]bool{}, started: map[string]bool{}, output: &headTail{limit: captureLimit}}
	buildFailed := false
	// A panic or a hang ends the test binary, and without another pass every test
	// after that one would count as failed, making the score depend on the order
	// of declaration. So tests that were never reached run again, until a pass
	// reaches none of them.
	var unreached []string
	for pass := 0; ; pass++ {
		args := []string{"-json", "-count=1", "-vet=off", "-timeout=" + w.scoreBinary.String()}
		if pass > 0 {
			args = append(args, "-run", "^("+strings.Join(unreached, "|")+")$")
		}
		reached := len(sink.started)
		runErr := goTest(runCtx, scratch, sink, append(args, "./...")...)
		sink.flush()
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("s2: score did not finish: %w", err)
		}
		if err := runCtx.Err(); err != nil {
			if pass == 0 {
				return Result{}, fmt.Errorf("s2: score did not finish: %w", err)
			}
			break // the tests still unreached count as failed
		}
		if runErr != nil && !isTestExit(runErr) {
			return Result{}, fmt.Errorf("s2: score: %w", runErr)
		}
		// A non-zero exit with no test having started means the package never
		// ran: a go.mod error, a failing setup or a crash during init.
		buildFailed = sink.buildFailed || (runErr != nil && len(sink.started) == 0)
		unreached = unreached[:0]
		for id, name := range want {
			if !sink.started[id] {
				unreached = append(unreached, name[strings.LastIndex(name, ".")+1:])
			}
		}
		if buildFailed || len(unreached) == 0 || len(sink.started) == reached {
			break
		}
	}

	// A go command that dies before doing anything prints nothing and looks like a build
	// failure. A real compile error always prints, so this is the scorer failing, and it
	// must not read as a zero for the arm.
	if buildFailed && strings.TrimSpace(sink.output.String()) == "" {
		return Result{}, errors.New("s2: score: go test failed with no output")
	}

	res := Result{
		Total:       len(want),
		Output:      clip(unscratch(sink.output.String(), scratch), scoreOutputLimit),
		BuildFailed: buildFailed,
	}
	for _, id := range sortedKeys(want) {
		if !res.BuildFailed && sink.passed[id] {
			res.Passed++
		} else {
			res.Failed = append(res.Failed, want[id])
		}
	}
	sort.Strings(res.Failed)
	res.Score = float64(res.Passed) / float64(res.Total)
	return res, nil
}

// hiddenTests maps "package path\x00function" to the name reported in
// Result.Failed, for every test function in the hidden files.
func hiddenTests(hidden map[string]string) (map[string]string, error) {
	want := map[string]string{}
	fset := token.NewFileSet()
	for name, src := range hidden {
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("s2: hidden file %s: %w", name, err)
		}
		pkg, prefix := moduleName, ""
		if dir := path.Dir(name); dir != "." {
			pkg, prefix = moduleName+"/"+dir, path.Base(dir)+"."
		}
		for _, fn := range testFuncs(file) {
			want[pkg+"\x00"+fn] = prefix + fn
		}
	}
	return want, nil
}

// testFuncs returns the functions in file that go test would run as tests.
func testFuncs(file *ast.File) []string {
	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		rest, ok := strings.CutPrefix(fn.Name.Name, "Test")
		if !ok {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(rest); rest == "" || !unicode.IsLower(r) {
			names = append(names, fn.Name.Name)
		}
	}
	return names
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// scratchCopy copies the workspace to a new temporary directory. The task's go.mod
// always wins there, so that editing go.mod changes nothing about what runs and no
// dependency can be requested.
func (w *Workspace) scratchCopy(withTests bool) (string, error) {
	scratch, err := os.MkdirTemp("", "s2-run-*")
	if err != nil {
		return "", fmt.Errorf("s2: scratch: %w", err)
	}
	w.mu.Lock()
	err = copyTree(w.dir, scratch, withTests)
	w.mu.Unlock()
	if err == nil {
		err = os.WriteFile(filepath.Join(scratch, "go.mod"), []byte(w.goMod), 0o644)
	}
	if err != nil {
		removeScratch(scratch)
		return "", fmt.Errorf("s2: scratch: %w", err)
	}
	return scratch, nil
}

func copyTree(src, dst string, withTests bool) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() || (!withTests && strings.HasSuffix(d.Name(), "_test.go")) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// removeScratch retries because Windows keeps a file locked for a moment after a
// test binary exits.
func removeScratch(dir string) {
	for i := range 5 {
		if os.RemoveAll(dir) == nil {
			return
		}
		time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
	}
}

// goTest runs go test in dir with the environment that keeps it offline and on the
// local toolchain.
func goTest(ctx context.Context, dir string, out io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", append([]string{"test"}, args...)...)
	cmd.Dir = dir
	cmd.Env = goEnv()
	cmd.Stdout = out
	cmd.Stderr = out
	// A killed go command can leave a test binary holding the output pipe.
	cmd.WaitDelay = 5 * time.Second
	// Code a model wrote can allocate without bound. One test binary took 58 GB and the
	// local model server died with it, so every go test runs under a memory cap.
	done, err := startCapped(cmd)
	if err != nil {
		return err
	}
	defer done()
	return cmd.Wait()
}

// The memory caps of one go test: each process, and the command with everything it runs.
// Building and testing a task's module needs well under a gigabyte.
const (
	processMemoryCap = 2 << 30
	jobMemoryCap     = 4 << 30
)

// secretEnv matches variable names that must not reach code a model wrote. The
// go tool needs the rest (paths, caches, system directories) to work on Windows.
var secretEnv = regexp.MustCompile(`(?i)(key|token|secret|password|credential|auth|proxy)`)

func goEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !secretEnv.MatchString(name) {
			env = append(env, kv)
		}
	}
	return append(env,
		"GOFLAGS=-mod=mod",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOWORK=off",
		"GO111MODULE=on",
		"CGO_ENABLED=0",
	)
}

// isTestExit reports whether err is go test finishing with a non-zero status,
// which is how it reports failing tests, as opposed to it failing to run.
func isTestExit(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) || errors.Is(err, exec.ErrWaitDelay)
}

// unscratch removes the scratch path from output, so that the same code produces
// the same text in every run.
func unscratch(text, scratch string) string {
	text = strings.ReplaceAll(text, scratch, ".")
	return strings.ReplaceAll(text, filepath.ToSlash(scratch), ".")
}

// clip keeps the head and tail of s within limit bytes.
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	marker := fmt.Sprintf("\n... [%d bytes omitted] ...\n", len(s)-limit)
	keep := limit - len(marker)
	head := keep / 3
	tail := keep - head
	return strings.ToValidUTF8(s[:head], "") + marker + strings.ToValidUTF8(s[len(s)-tail:], "")
}

// headTail keeps the first and last limit bytes written to it, so that a test
// printing without end cannot exhaust memory.
type headTail struct {
	limit   int
	head    []byte
	tail    []byte
	dropped int
}

func (h *headTail) Write(p []byte) (int, error) {
	n := len(p)
	if room := h.limit - len(h.head); room > 0 {
		take := min(room, len(p))
		h.head = append(h.head, p[:take]...)
		p = p[take:]
	}
	h.tail = append(h.tail, p...)
	if excess := len(h.tail) - h.limit; excess > h.limit {
		h.tail = append(h.tail[:0], h.tail[excess:]...)
		h.dropped += excess
	}
	return n, nil
}

func (h *headTail) String() string {
	tail := h.tail
	dropped := h.dropped
	if excess := len(tail) - h.limit; excess > 0 {
		tail = tail[excess:]
		dropped += excess
	}
	if dropped == 0 {
		return string(h.head) + string(tail)
	}
	return string(h.head) + fmt.Sprintf("\n... [%d bytes omitted] ...\n", dropped) + string(tail)
}

// eventSink consumes go test -json output as it arrives.
type eventSink struct {
	partial     []byte
	oversized   bool
	passed      map[string]bool // top-level tests, by package and name
	started     map[string]bool // likewise
	buildFailed bool
	output      *headTail
}

func (s *eventSink) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.buffer(p)
			break
		}
		s.buffer(p[:i])
		s.flush()
		p = p[i+1:]
	}
	return n, nil
}

func (s *eventSink) buffer(p []byte) {
	if len(s.partial)+len(p) > maxEventLine {
		s.oversized = true
		s.partial = s.partial[:0]
		return
	}
	s.partial = append(s.partial, p...)
}

// flush handles the line buffered so far.
func (s *eventSink) flush() {
	line := s.partial
	s.partial = s.partial[:0]
	oversized := s.oversized
	s.oversized = false
	if oversized || len(line) == 0 {
		return
	}
	var ev struct {
		Action      string
		Package     string
		Test        string
		Output      string
		FailedBuild string
	}
	if json.Unmarshal(line, &ev) != nil || ev.Action == "" {
		// The go command's own diagnostics are not JSON.
		s.output.Write(line)
		s.output.Write([]byte("\n"))
		return
	}
	topLevel := ev.Test != "" && !strings.Contains(ev.Test, "/")
	if topLevel {
		s.started[ev.Package+"\x00"+ev.Test] = true
	}
	switch ev.Action {
	case "pass":
		if topLevel {
			s.passed[ev.Package+"\x00"+ev.Test] = true
		}
	case "build-fail":
		s.buildFailed = true
	case "output", "build-output":
		s.output.Write([]byte(ev.Output))
		if strings.Contains(ev.Output, "[build failed]") || strings.Contains(ev.Output, "[setup failed]") {
			s.buildFailed = true
		}
	}
	if ev.FailedBuild != "" {
		s.buildFailed = true
	}
}
