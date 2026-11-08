package s2

func accB05() Task {
	return Task{
		ID:     "s2-b05-template-renderer",
		Set:    "acceptance",
		System: system("a text template renderer"),
		Files: map[string]string{
			"go.mod": goMod,
			"vars.go": `package tmpl

// Vars holds the values a template can refer to.
type Vars map[string]string

// Has reports whether name has a value.
func (v Vars) Has(name string) bool {
	_, ok := v[name]
	return ok
}
`,
			"docs/mail-notes.txt": `Mail notes

Everything rendered by this package ends up in HTML mail, and the values come
from customer records. A substituted value is always HTML-escaped (the mail team
uses html.EscapeString), and that is the last thing done to it. The template's
own text is trusted and stays untouched.

Mail goes out as UTF-8.
`,
			"vars_test.go": `package tmpl

import "testing"

func TestHas(t *testing.T) {
	v := Vars{"a": ""}
	if !v.Has("a") || v.Has("b") {
		t.Fatal("Has is wrong")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package tmpl_test

import (
	"errors"
	"slices"
	"testing"

	tmpl "fixture"
)

func render(t *testing.T, template string, vars tmpl.Vars) string {
	t.Helper()
	got, err := tmpl.Render(template, vars)
	if err != nil {
		t.Fatalf("Render(%q): %v", template, err)
	}
	return got
}

func TestCount(t *testing.T) {
	if got := tmpl.Count("{{a}} and {{b}}"); got != 2 {
		t.Fatalf("Count = %d", got)
	}
}

func TestCheckFine(t *testing.T) {
	if err := tmpl.Check("{{a}} and {{b}}"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckUnclosed(t *testing.T) {
	if err := tmpl.Check("{{a}} and {{b"); !errors.Is(err, tmpl.ErrUnclosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestMissing(t *testing.T) {
	got := tmpl.Vars{"b": "1"}.Missing([]string{"c", "b", "a"})
	if !slices.Equal(got, []string{"c", "a"}) {
		t.Fatalf("Missing = %v", got)
	}
}

func TestRenderPlain(t *testing.T) {
	if got := render(t, "no placeholders", nil); got != "no placeholders" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderSubstitutes(t *testing.T) {
	if got := render(t, "Hi {{name}}, {{name}}!", tmpl.Vars{"name": "Ann"}); got != "Hi Ann, Ann!" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderSpacesInside(t *testing.T) {
	if got := render(t, "Hi {{ name }}!", tmpl.Vars{"name": "Ann"}); got != "Hi Ann!" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderUnclosedIsError(t *testing.T) {
	if _, err := tmpl.Render("Hi {{name", tmpl.Vars{"name": "Ann"}); err == nil {
		t.Fatal("no error")
	}
}

func TestRenderUnclosedError(t *testing.T) {
	if _, err := tmpl.Render("Hi {{name", tmpl.Vars{"name": "Ann"}); !errors.Is(err, tmpl.ErrUnclosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestRenderAllUnclosedError(t *testing.T) {
	_, err := tmpl.RenderAll([]string{"fine", "{{oops"}, tmpl.Vars{})
	if !errors.Is(err, tmpl.ErrUnclosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestRenderAll(t *testing.T) {
	got, err := tmpl.RenderAll([]string{"{{a}}", "x{{a}}"}, tmpl.Vars{"a": "1"})
	if err != nil || !slices.Equal(got, []string{"1", "x1"}) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestUnknownStaysAsWritten(t *testing.T) {
	if got := render(t, "Hi {{name}}!", tmpl.Vars{}); got != "Hi {{name}}!" {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownKeepsSpacing(t *testing.T) {
	got := render(t, "{{a}} {{  who }}", tmpl.Vars{"a": "1"})
	if got != "1 {{  who }}" {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownKeepsFilter(t *testing.T) {
	if got := render(t, "{{ who|upper }}", tmpl.Vars{}); got != "{{ who|upper }}" {
		t.Fatalf("got %q", got)
	}
}

func TestValuesEscaped(t *testing.T) {
	if got := render(t, "{{n}}", tmpl.Vars{"n": "<Bob> & co"}); got != "&lt;Bob&gt; &amp; co" {
		t.Fatalf("got %q", got)
	}
}

func TestEscapedAfterFilter(t *testing.T) {
	if got := render(t, "{{n|upper}}", tmpl.Vars{"n": "a&b"}); got != "A&amp;B" {
		t.Fatalf("got %q", got)
	}
}

func TestTemplateTextUntouched(t *testing.T) {
	if got := render(t, "<b>{{n}}</b>", tmpl.Vars{"n": "x"}); got != "<b>x</b>" {
		t.Fatalf("got %q", got)
	}
}

func TestFilters(t *testing.T) {
	vars := tmpl.Vars{"n": "aB"}
	if got := render(t, "{{n|upper}}", vars); got != "AB" {
		t.Errorf("upper: %q", got)
	}
	if got := render(t, "{{ n | lower }}", vars); got != "ab" {
		t.Errorf("lower: %q", got)
	}
}

func TestUnknownFilter(t *testing.T) {
	if _, err := tmpl.Render("{{n|nope}}", tmpl.Vars{"n": "x"}); !errors.Is(err, tmpl.ErrUnknownFilter) {
		t.Fatalf("err = %v", err)
	}
}

func TestNamesNone(t *testing.T) {
	if got := tmpl.Names("nothing here"); len(got) != 0 {
		t.Fatalf("Names = %v", got)
	}
}

func TestNamesPlainName(t *testing.T) {
	if got := tmpl.Names("{{ a|upper }}"); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("Names = %v", got)
	}
}

func TestNamesFirstAppearance(t *testing.T) {
	if got := tmpl.Names("{{z}} {{ a }} {{m}}"); !slices.Equal(got, []string{"z", "a", "m"}) {
		t.Fatalf("Names = %v", got)
	}
}

func TestNamesEachOnce(t *testing.T) {
	if got := tmpl.Names("{{a}} {{b}} {{ a }}"); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Names = %v", got)
	}
}
`},
		Solution: map[string]string{
			"vars.go": `package tmpl

import (
	"errors"
	"html"
	"strings"
)

// ErrUnclosed is returned for any template with a {{ that has no }} after it.
var ErrUnclosed = errors.New("unclosed placeholder")

// ErrUnknownFilter is returned for a filter that is not in Filters.
var ErrUnknownFilter = errors.New("unknown filter")

// Filters are the filters a placeholder can name.
var Filters = map[string]func(string) string{"upper": strings.ToUpper, "lower": strings.ToLower}

// Vars holds the values a template can refer to.
type Vars map[string]string

// Has reports whether name has a value.
func (v Vars) Has(name string) bool {
	_, ok := v[name]
	return ok
}

// Missing returns those of names that v lacks, keeping their order.
func (v Vars) Missing(names []string) []string {
	var out []string
	for _, n := range names {
		if !v.Has(n) {
			out = append(out, n)
		}
	}
	return out
}

// Count returns how many {{ the template contains.
func Count(template string) int { return strings.Count(template, "{{") }

// Check reports a {{ that has no }} after it.
func Check(template string) error {
	for {
		_, after, ok := strings.Cut(template, "{{")
		if !ok {
			return nil
		}
		_, rest, ok := strings.Cut(after, "}}")
		if !ok {
			return ErrUnclosed
		}
		template = rest
	}
}

// expand returns the text for one placeholder.
func expand(inner string, vars Vars) (string, error) {
	name, filter, filtered := strings.Cut(inner, "|")
	value, ok := vars[strings.TrimSpace(name)]
	if !ok {
		return "{{" + inner + "}}", nil
	}
	if filtered {
		f, ok := Filters[strings.TrimSpace(filter)]
		if !ok {
			return "", ErrUnknownFilter
		}
		value = f(value)
	}
	return html.EscapeString(value), nil
}

// Render fills in the placeholders of template.
func Render(template string, vars Vars) (string, error) {
	var out strings.Builder
	for {
		before, after, ok := strings.Cut(template, "{{")
		out.WriteString(before)
		if !ok {
			return out.String(), nil
		}
		inner, rest, ok := strings.Cut(after, "}}")
		if !ok {
			return "", ErrUnclosed
		}
		text, err := expand(inner, vars)
		if err != nil {
			return "", err
		}
		out.WriteString(text)
		template = rest
	}
}

// RenderAll renders each template and stops at the first error.
func RenderAll(templates []string, vars Vars) ([]string, error) {
	out := make([]string, 0, len(templates))
	for _, t := range templates {
		text, err := Render(t, vars)
		if err != nil {
			return nil, err
		}
		out = append(out, text)
	}
	return out, nil
}

// Names returns the placeholder names of a template, first appearance first.
func Names(template string) []string {
	var names []string
	seen := map[string]bool{}
	for _, part := range strings.Split(template, "{{")[1:] {
		inner, _, ok := strings.Cut(part, "}}")
		name, _, _ := strings.Cut(inner, "|")
		name = strings.TrimSpace(name)
		if ok && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}
`,
		},
		Turns: script(
			cloud("Some background: rendering happens in two passes. This module fills what it knows, and the mail system fills the rest afterwards. So a placeholder we have no value for has to come out exactly as written, spacing and all. Add `func Count(template string) int`: how many `{{` the template contains."),
			cloud("Add `var ErrUnclosed = errors.New(\"unclosed placeholder\")` and `func Check(template string) error`: ErrUnclosed if some `{{` has no `}}` after it, otherwise nil. Anything here that processes a template returns that same error in that situation."),
			cloud("Read docs/mail-notes.txt and tell me what it means for rendering. No code changes."),
			local("Add `func Render(template string, vars Vars) (string, error)`. Placeholders look like `{{name}}`, and `{{ name }}` with spaces inside means the same."),
			local("The template editor lists placeholders the way a writer meets them: in the order they first appear in the text, each once. Anything here that lists placeholder names does the same. Add `func (v Vars) Missing(names []string) []string`: those of names that v lacks, keeping their order."),
			cloud("Add `func Names(template string) []string`: the placeholder names a template uses, without the braces or spaces."),
			cloud("Time for filters. Add `var Filters = map[string]func(string) string{\"upper\": strings.ToUpper, \"lower\": strings.ToLower}` and `var ErrUnknownFilter = errors.New(\"unknown filter\")`. In Render, `{{name|upper}}` runs the value through that filter, and spaces around the name and the filter don't matter. A known variable with a filter that isn't in Filters makes Render return ErrUnknownFilter. Names should give just the name."),
			local("Add `func RenderAll(templates []string, vars Vars) ([]string, error)`: renders each template and stops at the first error."),
		),
		Carries: []Carry{
			{
				What:        "a placeholder with no value comes out exactly as written",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 6},
				Tests:       []string{"TestUnknownStaysAsWritten", "TestUnknownKeepsSpacing", "TestUnknownKeepsFilter"},
				Absent:      []string{"as written", "two passes", "second pass", "verbatim", "no value"},
			},
			{
				What:        "substituted values are HTML-escaped, as the last step (docs/mail-notes.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{3, 6},
				Source:      "docs/mail-notes.txt",
				Tests:       []string{"TestValuesEscaped", "TestEscapedAfterFilter"},
				Absent:      []string{"html", "escap", "markup"},
			},
			{
				What:        "listed placeholder names are in order of first appearance, each once",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{5},
				Tests:       []string{"TestNamesFirstAppearance", "TestNamesEachOnce"},
				Absent:      []string{"first appear", "each once", "in the order"},
			},
			{
				What:        "ErrUnclosed is what processing a template with an unclosed placeholder returns",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{3, 7},
				Tests:       []string{"TestRenderUnclosedError", "TestRenderAllUnclosedError"},
				Absent:      []string{"errunclosed", "unclosed", "has no"},
			},
		},
	}
}
