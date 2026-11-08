package s2

func task13() Task {
	return Task{
		ID:     "s2-13-deploy-dry-run",
		Set:    "tuning",
		System: system("a deployment tool working on an in-memory target"),
		Files: map[string]string{
			"go.mod": goMod,
			"deployer.go": `package deploy

// File is one deployed file.
type File struct {
	Content string
	Exec    bool
}

// Deployer applies changes to an in-memory target.
type Deployer struct {
	Dry   bool
	Files map[string]File
	Plan  []string
}

// NewDeployer returns a Deployer with an empty target.
func NewDeployer() *Deployer { return &Deployer{Files: map[string]File{}} }

// Put creates or replaces a file.
func (d *Deployer) Put(path, content string) error {
	d.Files[path] = File{Content: content}
	return nil
}
`,
			"docs/runbook.txt": `Deployment runbook

Everything below secrets/ is managed by another team. A deploy must refuse any
change to a path below secrets/, as source or destination, dry run or not.

Deploys run at night. Rollbacks are done by redeploying the previous release.
`,
			"deployer_test.go": `package deploy

import "testing"

func TestPut(t *testing.T) {
	d := NewDeployer()
	if err := d.Put("app/x", "hello"); err != nil {
		t.Fatal(err)
	}
	if d.Files["app/x"].Content != "hello" {
		t.Fatalf("got %+v", d.Files["app/x"])
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package deploy_test

import (
	"errors"
	"strings"
	"testing"

	deploy "fixture"
)

func target(t *testing.T, paths ...string) *deploy.Deployer {
	t.Helper()
	d := deploy.NewDeployer()
	for _, p := range paths {
		d.Files[p] = deploy.File{Content: "content of " + p}
	}
	return d
}

func lastPlan(d *deploy.Deployer) string {
	if len(d.Plan) == 0 {
		return ""
	}
	return d.Plan[len(d.Plan)-1]
}

func TestPutDry(t *testing.T) {
	d := target(t)
	d.Dry = true
	if err := d.Put("app/x", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Files["app/x"]; ok || lastPlan(d) != "put app/x" {
		t.Fatalf("files %v, plan %v", d.Files, d.Plan)
	}
}

func TestPutBinExecutable(t *testing.T) {
	d := target(t)
	if err := d.Put("bin/tool", "x"); err != nil {
		t.Fatal(err)
	}
	if err := d.Put("app/x", "x"); err != nil {
		t.Fatal(err)
	}
	if !d.Files["bin/tool"].Exec || d.Files["app/x"].Exec {
		t.Fatalf("files %v", d.Files)
	}
}

func TestPutProtected(t *testing.T) {
	d := target(t)
	if err := d.Put("secrets/k", "x"); !errors.Is(err, deploy.ErrProtected) {
		t.Fatalf("err = %v", err)
	}
	if len(d.Files) != 0 {
		t.Fatalf("files %v", d.Files)
	}
}

func TestRemoveGone(t *testing.T) {
	d := target(t, "app/x")
	if err := d.Remove("app/x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Files["app/x"]; ok {
		t.Fatal("still there")
	}
}

func TestRemoveMissing(t *testing.T) {
	if err := target(t).Remove("app/x"); err == nil {
		t.Fatal("no error")
	}
}

func TestRemoveMovesToTrash(t *testing.T) {
	d := target(t, "app/x")
	if err := d.Remove("app/x"); err != nil {
		t.Fatal(err)
	}
	if got := d.Files["trash/app/x"].Content; got != "content of app/x" {
		t.Fatalf("trash/app/x holds %q, files %v", got, d.Files)
	}
}

func TestRemoveDry(t *testing.T) {
	d := target(t, "app/x")
	d.Dry = true
	if err := d.Remove("app/x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Files["app/x"]; !ok || len(d.Files) != 1 {
		t.Fatalf("a dry run changed the files: %v", d.Files)
	}
	if lastPlan(d) != "remove app/x" {
		t.Fatalf("plan %v", d.Plan)
	}
}

func TestRemoveProtected(t *testing.T) {
	d := target(t, "secrets/k")
	if err := d.Remove("secrets/k"); !errors.Is(err, deploy.ErrProtected) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := d.Files["secrets/k"]; !ok {
		t.Fatal("the file is gone")
	}
}

func TestProtectedEvenWhenDry(t *testing.T) {
	d := target(t, "secrets/k")
	d.Dry = true
	if err := d.Remove("secrets/k"); !errors.Is(err, deploy.ErrProtected) {
		t.Fatalf("err = %v", err)
	}
}

func TestRenameMoves(t *testing.T) {
	d := target(t, "app/a")
	if err := d.Rename("app/a", "app/b"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Files["app/a"]; ok {
		t.Error("the old path is still there")
	}
	if d.Files["app/b"].Content != "content of app/a" {
		t.Errorf("files %v", d.Files)
	}
}

func TestRenameMissing(t *testing.T) {
	if err := target(t).Rename("app/a", "app/b"); err == nil {
		t.Fatal("no error")
	}
}

func TestRenameDry(t *testing.T) {
	d := target(t, "app/a")
	d.Dry = true
	if err := d.Rename("app/a", "app/b"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Files["app/a"]; !ok || len(d.Files) != 1 {
		t.Fatalf("a dry run changed the files: %v", d.Files)
	}
	if p := lastPlan(d); !strings.HasPrefix(p, "rename ") || !strings.Contains(p, "app/a") {
		t.Fatalf("plan %v", d.Plan)
	}
}

func TestRenameProtected(t *testing.T) {
	d := target(t, "secrets/k", "app/a")
	if err := d.Rename("secrets/k", "app/k"); !errors.Is(err, deploy.ErrProtected) {
		t.Errorf("from secrets: %v", err)
	}
	if err := d.Rename("app/a", "secrets/a"); !errors.Is(err, deploy.ErrProtected) {
		t.Errorf("to secrets: %v", err)
	}
}

func TestRenameIntoBinExecutable(t *testing.T) {
	d := target(t, "app/tool")
	if err := d.Rename("app/tool", "bin/tool"); err != nil {
		t.Fatal(err)
	}
	if !d.Files["bin/tool"].Exec {
		t.Fatalf("files %v", d.Files)
	}
}

func TestCopyKeepsSource(t *testing.T) {
	d := target(t, "app/a")
	if err := d.Copy("app/a", "app/b"); err != nil {
		t.Fatal(err)
	}
	if d.Files["app/a"].Content != "content of app/a" || d.Files["app/b"].Content != "content of app/a" {
		t.Fatalf("files %v", d.Files)
	}
}

func TestCopyMissing(t *testing.T) {
	if err := target(t).Copy("app/a", "app/b"); err == nil {
		t.Fatal("no error")
	}
}

func TestCopyDry(t *testing.T) {
	d := target(t, "app/a")
	d.Dry = true
	if err := d.Copy("app/a", "app/b"); err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 {
		t.Fatalf("a dry run changed the files: %v", d.Files)
	}
	if p := lastPlan(d); !strings.HasPrefix(p, "copy ") || !strings.Contains(p, "app/a") {
		t.Fatalf("plan %v", d.Plan)
	}
}

func TestCopyProtected(t *testing.T) {
	d := target(t, "secrets/k", "app/a")
	if err := d.Copy("secrets/k", "app/k"); !errors.Is(err, deploy.ErrProtected) {
		t.Errorf("from secrets: %v", err)
	}
	if err := d.Copy("app/a", "secrets/a"); !errors.Is(err, deploy.ErrProtected) {
		t.Errorf("to secrets: %v", err)
	}
}

func TestCopyIntoBinExecutable(t *testing.T) {
	d := target(t, "app/tool")
	if err := d.Copy("app/tool", "bin/tool"); err != nil {
		t.Fatal(err)
	}
	if !d.Files["bin/tool"].Exec {
		t.Fatalf("files %v", d.Files)
	}
}

func TestTouchCreatesOnly(t *testing.T) {
	d := target(t, "app/a")
	if err := d.Touch("app/new"); err != nil {
		t.Fatal(err)
	}
	if err := d.Touch("app/a"); err != nil {
		t.Fatal(err)
	}
	if f, ok := d.Files["app/new"]; !ok || f.Content != "" {
		t.Errorf("app/new = %+v, %v", f, ok)
	}
	if d.Files["app/a"].Content != "content of app/a" {
		t.Errorf("an existing file changed: %+v", d.Files["app/a"])
	}
}

func TestTouchDry(t *testing.T) {
	d := target(t)
	d.Dry = true
	if err := d.Touch("app/new"); err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 0 || lastPlan(d) != "touch app/new" {
		t.Fatalf("files %v, plan %v", d.Files, d.Plan)
	}
}

func TestTouchProtected(t *testing.T) {
	d := target(t)
	if err := d.Touch("secrets/k"); !errors.Is(err, deploy.ErrProtected) {
		t.Fatalf("err = %v", err)
	}
}

func TestTouchBinExecutable(t *testing.T) {
	d := target(t)
	if err := d.Touch("bin/tool"); err != nil {
		t.Fatal(err)
	}
	if !d.Files["bin/tool"].Exec {
		t.Fatalf("files %v", d.Files)
	}
}
`},
		Solution: map[string]string{
			"deployer.go": `package deploy

import (
	"errors"
	"strings"
)

// ErrProtected refuses a change to a path the runbook protects.
var ErrProtected = errors.New("protected path")

// File is one deployed file.
type File struct {
	Content string
	Exec    bool
}

// Deployer applies changes to an in-memory target.
type Deployer struct {
	Dry   bool
	Files map[string]File
	Plan  []string
}

// NewDeployer returns a Deployer with an empty target.
func NewDeployer() *Deployer { return &Deployer{Files: map[string]File{}} }

func protected(path string) bool { return strings.HasPrefix(path, "secrets/") }

func placed(path string, f File) File {
	if strings.HasPrefix(path, "bin/") {
		f.Exec = true
	}
	return f
}

// Put creates or replaces a file.
func (d *Deployer) Put(path, content string) error {
	if protected(path) {
		return ErrProtected
	}
	if d.Dry {
		d.Plan = append(d.Plan, "put "+path)
		return nil
	}
	d.Files[path] = placed(path, File{Content: content})
	return nil
}

// Remove moves a file under trash/.
func (d *Deployer) Remove(path string) error {
	if protected(path) {
		return ErrProtected
	}
	f, ok := d.Files[path]
	if !ok {
		return errors.New("no such file")
	}
	if d.Dry {
		d.Plan = append(d.Plan, "remove "+path)
		return nil
	}
	delete(d.Files, path)
	d.Files["trash/"+path] = f
	return nil
}

// Rename moves a file.
func (d *Deployer) Rename(from, to string) error {
	if protected(from) || protected(to) {
		return ErrProtected
	}
	f, ok := d.Files[from]
	if !ok {
		return errors.New("no such file")
	}
	if d.Dry {
		d.Plan = append(d.Plan, "rename "+from)
		return nil
	}
	delete(d.Files, from)
	d.Files[to] = placed(to, f)
	return nil
}

// Copy duplicates a file.
func (d *Deployer) Copy(from, to string) error {
	if protected(from) || protected(to) {
		return ErrProtected
	}
	f, ok := d.Files[from]
	if !ok {
		return errors.New("no such file")
	}
	if d.Dry {
		d.Plan = append(d.Plan, "copy "+from)
		return nil
	}
	d.Files[to] = placed(to, f)
	return nil
}

// Touch creates an empty file if there is none.
func (d *Deployer) Touch(path string) error {
	if protected(path) {
		return ErrProtected
	}
	if _, ok := d.Files[path]; ok {
		return nil
	}
	if d.Dry {
		d.Plan = append(d.Plan, "touch "+path)
		return nil
	}
	d.Files[path] = placed(path, File{})
	return nil
}
`,
		},
		Turns: script(
			cloud("We're adding a dry-run mode. Rule: when Deployer.Dry is true the target's Files do not change, and the operation instead appends a plan line to Plan: the operation's name in lower case, a space, then the path, like \"put app/x\". Apply that to Put now. Every operation we add later works the same way."),
			cloud("Another rule: a file placed under bin/ must be executable. Make Put set File.Exec for those. The same goes for anything else that places a file."),
			local("Read docs/runbook.txt and tell me which paths we may touch. No code changes."),
			local("Add `var ErrProtected = errors.New(\"protected path\")` and make Put refuse paths covered by the runbook with it. One more thing from ops: we don't hard-delete on targets. Removing a file moves it under trash/, keeping its path, so app/x becomes trash/app/x. No code for that yet."),
			cloud("Add `func (d *Deployer) Remove(path string) error`. Removing a file that doesn't exist is an error."),
			local("Add `func (d *Deployer) Rename(from, to string) error`. A missing source is an error."),
			cloud("Add `func (d *Deployer) Copy(from, to string) error`. A missing source is an error."),
			local("Add `func (d *Deployer) Touch(path string) error`, which creates an empty file if there's none and otherwise leaves the file alone."),
		),
		Carries: []Carry{
			{
				What:        "with Dry set an operation changes nothing and appends a plan line",
				Kind:        CarryEmbodied,
				Established: 0,
				Needed:      []int{4, 5, 6, 7},
				Tests:       []string{"TestRemoveDry", "TestRenameDry", "TestCopyDry", "TestTouchDry"},
				Absent:      []string{"dry", "plan line"},
			},
			{
				What:        "anything that places a file under bin/ makes it executable",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{5, 6, 7},
				Tests:       []string{"TestRenameIntoBinExecutable", "TestCopyIntoBinExecutable", "TestTouchBinExecutable"},
				Absent:      []string{"bin/", "executable", "file.exec"},
			},
			{
				What:        "any change below secrets/ is refused, dry run or not (docs/runbook.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{4, 5, 6, 7},
				Source:      "docs/runbook.txt",
				Tests:       []string{"TestRemoveProtected", "TestProtectedEvenWhenDry", "TestRenameProtected", "TestCopyProtected", "TestTouchProtected"},
				Absent:      []string{"secrets", "another team"},
			},
			{
				What:        "removing a file moves it under trash/, keeping its path",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4},
				Tests:       []string{"TestRemoveMovesToTrash"},
				Absent:      []string{"trash", "hard-delete"},
			},
		},
	}
}
