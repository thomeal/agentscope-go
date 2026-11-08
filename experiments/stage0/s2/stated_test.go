package s2

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fixtureImporter resolves the fixture to its checked package and the rest from source.
type fixtureImporter struct {
	fixture *types.Package
	std     types.Importer
}

func (i fixtureImporter) Import(path string) (*types.Package, error) {
	if path == "fixture" {
		return i.fixture, nil
	}
	return i.std.Import(path)
}

// usedFromFixture type-checks a task's finished module and its hidden tests, and returns the
// fixture's exported objects those tests use, keyed by name.
func usedFromFixture(t *testing.T, task Task, std types.Importer) map[string]types.Object {
	t.Helper()
	fset := token.NewFileSet()
	finished := maps.Clone(task.Files)
	maps.Copy(finished, task.Solution)
	var files []*ast.File
	for name, src := range finished {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if strings.Contains(name, "/") {
			t.Skip("this check reads a module with a single package")
		}
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	fixture, err := (&types.Config{Importer: std}).Check("fixture", fset, files, nil)
	if err != nil {
		t.Fatal(err)
	}
	var hidden []*ast.File
	for name, src := range task.Hidden {
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		hidden = append(hidden, file)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	cfg := &types.Config{Importer: fixtureImporter{fixture: fixture, std: std}}
	if _, err := cfg.Check("fixture_test", fset, hidden, info); err != nil {
		t.Fatal(err)
	}
	used := map[string]types.Object{}
	for _, obj := range info.Uses {
		if obj.Pkg() == fixture && obj.Exported() {
			used[obj.Name()] = obj
		}
	}
	return used
}

// TestHiddenNamesAreStated checks what makes a task measure carrying rather than guessing. One
// missing name stops the hidden tests from building and scores the whole task 0, so everything
// they use from the fixture has to be named in a turn or in a file the assistants can read, and
// a new function or method has to be spelled as a call in a turn.
func TestHiddenNamesAreStated(t *testing.T) {
	std := importer.ForCompiler(token.NewFileSet(), "source", nil)
	for _, task := range allTasks() {
		t.Run(task.ID, func(t *testing.T) {
			var stated strings.Builder
			for _, turn := range task.Turns {
				stated.WriteString(turn.User + "\n")
			}
			turns := stated.String()
			var initial strings.Builder
			for name, src := range task.Files {
				if strings.HasSuffix(name, ".go") {
					initial.WriteString(src + "\n")
				}
			}
			for _, name := range slices.Sorted(maps.Keys(usedFromFixture(t, task, std))) {
				word := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
				if word.MatchString(initial.String()) {
					continue
				}
				if !word.MatchString(turns) {
					t.Errorf("the hidden tests use %s, which no turn or initial file names", name)
				}
			}
			for name, obj := range usedFromFixture(t, task, std) {
				if _, isFunc := obj.(*types.Func); !isFunc || strings.Contains(initial.String(), name+"(") {
					continue
				}
				if !strings.Contains(turns, name+"(") {
					t.Errorf("the hidden tests call %s, and no turn spells it as a call", name)
				}
			}
		})
	}
}
