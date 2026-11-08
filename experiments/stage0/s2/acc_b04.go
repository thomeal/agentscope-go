package s2

func accB04() Task {
	return Task{
		ID:     "s2-b04-flag-parser",
		Set:    "acceptance",
		System: system("a command-line flag parser"),
		Files: map[string]string{
			"go.mod": goMod,
			"flags.go": `package cli

// Spec declares one flag. Name is the long name without dashes. Short is the
// optional one-letter form, 0 if there is none. A Bool flag takes no value.
type Spec struct {
	Name  string
	Short byte
	Bool  bool
}

// Parser knows a set of flags.
type Parser struct {
	specs []Spec
}

// Define adds a flag.
func (p *Parser) Define(s Spec) { p.specs = append(p.specs, s) }

// Result is what a parse found.
type Result struct {
	Values map[string]string // by long name; a Bool flag that was given holds "true"
	Args   []string          // positional arguments
}
`,
			"docs/conventions.txt": `CLI conventions shared by all our tools

A double dash "--" ends the flags. It is dropped, and everything after it is
positional even if it looks like a flag.

A single "-" is a positional argument meaning standard input, not a flag.

Every Bool flag also has a negative form: --no-color sets the flag color to
"false".
`,
			"flags_test.go": `package cli

import "testing"

func TestDefine(t *testing.T) {
	var p Parser
	p.Define(Spec{Name: "out"})
	if len(p.specs) != 1 {
		t.Fatalf("%d specs", len(p.specs))
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package cli_test

import (
	"errors"
	"slices"
	"testing"

	cli "fixture"
)

func parser() *cli.Parser {
	p := &cli.Parser{}
	p.Define(cli.Spec{Name: "out", Short: 'o'})
	p.Define(cli.Spec{Name: "verbose", Short: 'v', Bool: true})
	p.Define(cli.Spec{Name: "dry-run", Bool: true})
	p.Define(cli.Spec{Name: "out-dir"})
	p.Define(cli.Spec{Name: "color", Bool: true})
	return p
}

func parse(t *testing.T, args ...string) cli.Result {
	t.Helper()
	res, err := parser().Parse(args)
	if err != nil {
		t.Fatalf("Parse(%v): %v", args, err)
	}
	return res
}

func TestNamesSorted(t *testing.T) {
	want := []string{"color", "dry-run", "out", "out-dir", "verbose"}
	if got := parser().Names(); !slices.Equal(got, want) {
		t.Fatalf("Names = %v", got)
	}
}

func TestGet(t *testing.T) {
	res := parse(t, "--out=x")
	if got := res.Get("out", "d"); got != "x" {
		t.Errorf("given: %q", got)
	}
	if got := res.Get("out-dir", "d"); got != "d" {
		t.Errorf("not given: %q", got)
	}
}

func TestLongEquals(t *testing.T) {
	if got := parse(t, "--out=a=b").Values["out"]; got != "a=b" {
		t.Fatalf("out = %q", got)
	}
}

func TestLongSpace(t *testing.T) {
	if got := parse(t, "--out", "x").Values["out"]; got != "x" {
		t.Fatalf("out = %q", got)
	}
}

func TestLongBool(t *testing.T) {
	res := parse(t, "--verbose", "file")
	if res.Values["verbose"] != "true" {
		t.Fatalf("verbose = %q", res.Values["verbose"])
	}
	if !slices.Equal(res.Args, []string{"file"}) {
		t.Fatalf("Args = %v", res.Args)
	}
}

func TestOnlyPositionals(t *testing.T) {
	if got := parse(t, "a", "b").Args; !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Args = %v", got)
	}
}

func TestUnknownIsError(t *testing.T) {
	if _, err := parser().Parse([]string{"--nope"}); err == nil {
		t.Fatal("no error")
	}
}

func TestMissingValueIsError(t *testing.T) {
	if _, err := parser().Parse([]string{"--out"}); err == nil {
		t.Fatal("no error")
	}
}

func TestShortBool(t *testing.T) {
	if got := parse(t, "-v").Values["verbose"]; got != "true" {
		t.Fatalf("verbose = %q", got)
	}
}

func TestShortValue(t *testing.T) {
	if got := parse(t, "-o", "x").Values["out"]; got != "x" {
		t.Fatalf("out = %q", got)
	}
}

func TestShortUnknownIsError(t *testing.T) {
	if _, err := parser().Parse([]string{"-z"}); err == nil {
		t.Fatal("no error")
	}
}

func TestStopsAtFirstPositional(t *testing.T) {
	res := parse(t, "--out=x", "sub", "--verbose", "-v")
	if res.Values["out"] != "x" {
		t.Fatalf("out = %q", res.Values["out"])
	}
	if _, ok := res.Values["verbose"]; ok {
		t.Fatal("a flag of the subcommand was taken")
	}
	if !slices.Equal(res.Args, []string{"sub", "--verbose", "-v"}) {
		t.Fatalf("Args = %v", res.Args)
	}
}

func TestUnknownAfterPositionalIsFine(t *testing.T) {
	res := parse(t, "sub", "--nope")
	if !slices.Equal(res.Args, []string{"sub", "--nope"}) {
		t.Fatalf("Args = %v", res.Args)
	}
}

func TestDoubleDash(t *testing.T) {
	res := parse(t, "--out=x", "--", "--verbose", "--nope")
	if _, ok := res.Values["verbose"]; ok {
		t.Fatal("a flag after the double dash was taken")
	}
	if !slices.Equal(res.Args, []string{"--verbose", "--nope"}) {
		t.Fatalf("Args = %v", res.Args)
	}
}

func TestSingleDash(t *testing.T) {
	if got := parse(t, "-").Args; !slices.Equal(got, []string{"-"}) {
		t.Fatalf("Args = %v", got)
	}
	res := parse(t, "-v", "-")
	if res.Values["verbose"] != "true" || !slices.Equal(res.Args, []string{"-"}) {
		t.Fatalf("got %+v", res)
	}
}

func TestNegativeForm(t *testing.T) {
	if got := parse(t, "--no-color").Values["color"]; got != "false" {
		t.Fatalf("color = %q", got)
	}
}

func TestUnderscoreBool(t *testing.T) {
	if got := parse(t, "--dry_run").Values["dry-run"]; got != "true" {
		t.Fatalf("dry-run = %q", got)
	}
}

func TestUnderscoreValue(t *testing.T) {
	if got := parse(t, "--out_dir=a_b").Values["out-dir"]; got != "a_b" {
		t.Fatalf("out-dir = %q", got)
	}
	if got := parse(t, "--out_dir", "a_b").Values["out-dir"]; got != "a_b" {
		t.Fatalf("out-dir = %q", got)
	}
}

func TestUnknownLongError(t *testing.T) {
	if _, err := parser().Parse([]string{"--nope"}); !errors.Is(err, cli.ErrUnknownFlag) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownShortError(t *testing.T) {
	if _, err := parser().Parse([]string{"-z"}); !errors.Is(err, cli.ErrUnknownFlag) {
		t.Fatalf("err = %v", err)
	}
}

func TestMissingValueError(t *testing.T) {
	for _, arg := range []string{"--out", "-o"} {
		if _, err := parser().Parse([]string{arg}); !errors.Is(err, cli.ErrMissingValue) {
			t.Errorf("%s: err = %v", arg, err)
		}
	}
}
`},
		Solution: map[string]string{
			"flags.go": `package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The two ways the parser refuses arguments. Callers use errors.Is.
var (
	ErrUnknownFlag  = errors.New("unknown flag")
	ErrMissingValue = errors.New("missing value")
)

// Spec declares one flag. Name is the long name without dashes. Short is the
// optional one-letter form, 0 if there is none. A Bool flag takes no value.
type Spec struct {
	Name  string
	Short byte
	Bool  bool
}

// Parser knows a set of flags.
type Parser struct {
	specs []Spec
}

// Define adds a flag.
func (p *Parser) Define(s Spec) { p.specs = append(p.specs, s) }

// Result is what a parse found.
type Result struct {
	Values map[string]string // by long name; a Bool flag that was given holds "true"
	Args   []string          // positional arguments
}

// Names returns the long names, sorted.
func (p *Parser) Names() []string {
	out := make([]string, 0, len(p.specs))
	for _, s := range p.specs {
		out = append(out, s.Name)
	}
	slices.Sort(out)
	return out
}

// Get returns a flag's value, or fallback if it wasn't given.
func (r Result) Get(name, fallback string) string {
	if v, ok := r.Values[name]; ok {
		return v
	}
	return fallback
}

// canon turns a spelling from the command line into a long name.
func canon(name string) string { return strings.ReplaceAll(name, "_", "-") }

// lookup finds the flag an argument refers to and any value attached to it.
func (p *Parser) lookup(arg string) (Spec, string, bool, error) {
	if long, ok := strings.CutPrefix(arg, "--"); ok {
		name, value, has := strings.Cut(long, "=")
		for _, s := range p.specs {
			if s.Name == canon(name) {
				return s, value, has, nil
			}
			if s.Bool && "no-"+s.Name == canon(name) {
				return s, "false", true, nil
			}
		}
	} else if len(arg) == 2 {
		for _, s := range p.specs {
			if s.Short == arg[1] {
				return s, "", false, nil
			}
		}
	}
	return Spec{}, "", false, fmt.Errorf("%w: %s", ErrUnknownFlag, arg)
}

// Parse reads flags up to the first positional argument.
func (p *Parser) Parse(args []string) (Result, error) {
	res := Result{Values: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			res.Args = append(res.Args, args[i+1:]...)
			return res, nil
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			res.Args = append(res.Args, args[i:]...)
			return res, nil
		}
		s, value, has, err := p.lookup(a)
		if err != nil {
			return Result{}, err
		}
		switch {
		case has:
		case s.Bool:
			value = "true"
		case i+1 == len(args):
			return Result{}, fmt.Errorf("%w: %s", ErrMissingValue, a)
		default:
			i++
			value = args[i]
		}
		res.Values[s.Name] = value
	}
	return res, nil
}
`,
		},
		Turns: script(
			cloud("Read docs/conventions.txt and tell me what the parser will have to honour. No code changes."),
			cloud("Add `var ErrUnknownFlag = errors.New(\"unknown flag\")` and `var ErrMissingValue = errors.New(\"missing value\")`. Those two are how the parser reports a flag it doesn't know and a flag that needs a value and has none. Wrapping them is fine, callers use errors.Is. Also add `func (p *Parser) Names() []string`, the long names sorted."),
			local("Our tools have subcommands, so flag parsing stops at the first positional argument. That argument and everything after it go to Args untouched, because they belong to the subcommand. Add `func (r Result) Get(name, fallback string) string`: a flag's value, or fallback if it wasn't given."),
			local("Some old wrapper scripts spell long flags with underscores and we can't change them, so --dry_run on the command line has to mean the flag dry-run. There's no parsing yet. No code changes."),
			cloud("Add `func (p *Parser) Parse(args []string) (Result, error)`, long flags only for now: --name=value, --name value, and plain --name for a Bool flag."),
			local("Add short flags to Parse: -v for a Bool flag and -o value for one that takes a value. Single letters only, no bundling."),
		),
		Carries: []Carry{
			{
				What:        "double dash, single dash and --no-NAME follow the shared conventions (docs/conventions.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{4, 5},
				Source:      "docs/conventions.txt",
				Tests:       []string{"TestDoubleDash", "TestSingleDash", "TestNegativeForm"},
				Absent:      []string{"double dash", "standard input", "--no-", "negative form"},
			},
			{
				What:        "flag parsing stops at the first positional argument",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{4},
				Tests:       []string{"TestStopsAtFirstPositional", "TestUnknownAfterPositionalIsFine"},
				Absent:      []string{"first positional", "subcommand", "untouched"},
			},
			{
				What:        "underscores in long flags on the command line mean hyphens",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4},
				Tests:       []string{"TestUnderscoreBool", "TestUnderscoreValue"},
				Absent:      []string{"underscore", "dry_run", "dry-run", "wrapper"},
			},
			{
				What:        "ErrUnknownFlag and ErrMissingValue are how the parser refuses arguments",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{4, 5},
				Tests:       []string{"TestUnknownLongError", "TestUnknownShortError", "TestMissingValueError"},
				Absent:      []string{"errunknownflag", "errmissingvalue", "errors.is"},
			},
		},
	}
}
