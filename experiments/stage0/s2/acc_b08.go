package s2

func accB08() Task {
	return Task{
		ID:     "s2-b08-glob-matcher",
		Set:    "acceptance",
		System: system("a path glob matcher"),
		Files: map[string]string{
			"go.mod": goMod,
			"glob.go": `package glob

import "strings"

// Segments splits a slash-separated path into its non-empty parts.
func Segments(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
}
`,
			"docs/ignore-format.txt": `Ignore file format

One pattern per line, each matched against the path by the package's Match.

Blank lines and lines starting with # are skipped and never looked at.

A line starting with ! re-includes: whatever the rest of the line matches is not
ignored.

When several lines match a path, the last of them decides.
`,
			"glob_test.go": `package glob

import (
	"slices"
	"testing"
)

func TestSegments(t *testing.T) {
	if got := Segments("x/y//z/"); !slices.Equal(got, []string{"x", "y", "z"}) {
		t.Fatalf("Segments = %v", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package glob_test

import (
	"errors"
	"slices"
	"testing"

	glob "fixture"
)

func check(t *testing.T, pattern, path string, want bool) {
	t.Helper()
	if got := glob.Match(pattern, path); got != want {
		t.Errorf("Match(%q, %q) = %v", pattern, path, got)
	}
}

func ignored(t *testing.T, lines []string, path string, want bool) {
	t.Helper()
	got, err := glob.Ignored(lines, path)
	if err != nil {
		t.Fatalf("Ignored(%q, %q): %v", lines, path, err)
	}
	if got != want {
		t.Errorf("Ignored(%q, %q) = %v", lines, path, got)
	}
}

func TestIsGlob(t *testing.T) {
	if !glob.IsGlob("a*") || !glob.IsGlob("a?") {
		t.Error("a glob was not recognised")
	}
	if glob.IsGlob("a/b.txt") {
		t.Error("a plain path was taken for a glob")
	}
}

func TestMatchLiteral(t *testing.T) {
	check(t, "x/y", "x/y", true)
	check(t, "x/y", "x/z", false)
	check(t, "x/y", "x/y/z", false)
	check(t, "x/y/z", "x/y", false)
}

func TestMatchStar(t *testing.T) {
	check(t, "*.txt", "note.txt", true)
	check(t, "*.txt", "note.md", false)
	check(t, "n*e.txt", "note.txt", true)
	check(t, "*.txt", "dir/note.txt", false)
	check(t, "dir/*", "dir/note.txt", true)
}

func TestMatchQuestion(t *testing.T) {
	check(t, "?.go", "a.go", true)
	check(t, "?.go", "ab.go", false)
	check(t, "?.go", ".go", false)
}

func TestDoubleStarSpans(t *testing.T) {
	check(t, "x/**/y", "x/p/y", true)
	check(t, "x/**/y", "x/p/q/y", true)
	check(t, "x/**/y", "x/p/q/z", false)
	check(t, "**", "p/q", true)
}

func TestDoubleStarZeroInside(t *testing.T) {
	check(t, "x/**/y", "x/y", true)
}

func TestDoubleStarZeroAtEnds(t *testing.T) {
	check(t, "**/y", "y", true)
	check(t, "x/**", "x", true)
}

func TestStarPassesOverDotfile(t *testing.T) {
	check(t, "*.txt", ".notes.txt", false)
	check(t, "*", ".env", false)
}

func TestQuestionPassesOverDotfile(t *testing.T) {
	check(t, "?env", ".env", false)
}

func TestDoubleStarStaysOutOfDotDirs(t *testing.T) {
	check(t, "**/x", ".git/x", false)
	check(t, "a/**/x", "a/.cache/b/x", false)
}

func TestDotPatternReachesDotfiles(t *testing.T) {
	check(t, ".*", ".env", true)
	check(t, ".git/**", ".git/p", true)
	check(t, ".env", ".env", true)
}

func TestValidateFine(t *testing.T) {
	for _, p := range []string{"x", "x/**/y", "*.txt", "**"} {
		if err := glob.Validate(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
}

func TestValidateBad(t *testing.T) {
	for _, p := range []string{"", "a**b", "x/**y"} {
		if err := glob.Validate(p); !errors.Is(err, glob.ErrBadPattern) {
			t.Errorf("%q: err = %v", p, err)
		}
	}
}

func TestIgnoredPlain(t *testing.T) {
	ignored(t, []string{"*.log"}, "x.log", true)
	ignored(t, []string{"*.log"}, "x.txt", false)
	ignored(t, nil, "x.log", false)
	ignored(t, []string{"", "*.log"}, "x.log", true)
}

func TestIgnoredSkipsCommentLines(t *testing.T) {
	ignored(t, []string{"#x.log"}, "#x.log", false)
	ignored(t, []string{"# a**b is not allowed", "*.log"}, "x.log", true)
}

func TestIgnoredReinclude(t *testing.T) {
	lines := []string{"*.log", "!keep.log"}
	ignored(t, lines, "keep.log", false)
	ignored(t, lines, "x.log", true)
}

func TestIgnoredLastLineDecides(t *testing.T) {
	ignored(t, []string{"!keep.log", "*.log"}, "keep.log", true)
	ignored(t, []string{"*.log", "!keep.log", "keep.*"}, "keep.log", true)
}

func TestIgnoredBadPatternError(t *testing.T) {
	if _, err := glob.Ignored([]string{"*.log", "a**b"}, "x.txt"); !errors.Is(err, glob.ErrBadPattern) {
		t.Fatalf("err = %v", err)
	}
}

func TestFilterKeepsOrder(t *testing.T) {
	got, err := glob.Filter([]string{"*.log"}, []string{"c.txt", "a.log", "b.txt"})
	if err != nil || !slices.Equal(got, []string{"c.txt", "b.txt"}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestFilterBadPatternError(t *testing.T) {
	if _, err := glob.Filter([]string{"a**b"}, []string{"x"}); !errors.Is(err, glob.ErrBadPattern) {
		t.Fatalf("err = %v", err)
	}
}
`},
		Solution: map[string]string{
			"glob.go": `package glob

import (
	"errors"
	"strings"
)

// ErrBadPattern is returned for any pattern that fails Validate.
var ErrBadPattern = errors.New("bad pattern")

// Segments splits a slash-separated path into its non-empty parts.
func Segments(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
}

// IsGlob reports whether the pattern contains a wildcard.
func IsGlob(pattern string) bool { return strings.ContainsAny(pattern, "*?") }

// Match reports whether path fits pattern, segment by segment.
func Match(pattern, path string) bool { return matchSegs(Segments(pattern), Segments(path)) }

func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		if matchSegs(pat[1:], segs) {
			return true
		}
		return len(segs) > 0 && !hidden(segs[0]) && matchSegs(pat, segs[1:])
	}
	if len(segs) == 0 || (hidden(segs[0]) && !hidden(pat[0])) || !wild(pat[0], segs[0]) {
		return false
	}
	return matchSegs(pat[1:], segs[1:])
}

func hidden(seg string) bool { return strings.HasPrefix(seg, ".") }

func wild(pat, s string) bool {
	if pat == "" {
		return s == ""
	}
	switch pat[0] {
	case '*':
		return wild(pat[1:], s) || (s != "" && wild(pat, s[1:]))
	case '?':
		return s != "" && wild(pat[1:], s[1:])
	}
	return s != "" && s[0] == pat[0] && wild(pat[1:], s[1:])
}

// Validate refuses an empty pattern, and ** sharing a segment with anything else.
func Validate(pattern string) error {
	if pattern == "" {
		return ErrBadPattern
	}
	for _, seg := range Segments(pattern) {
		if seg != "**" && strings.Contains(seg, "**") {
			return ErrBadPattern
		}
	}
	return nil
}

// Ignored reports whether path is ignored, given the lines of an ignore file.
func Ignored(lines []string, path string) (bool, error) {
	ignored := false
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pattern, negated := strings.CutPrefix(line, "!")
		if err := Validate(pattern); err != nil {
			return false, err
		}
		if Match(pattern, path) {
			ignored = !negated
		}
	}
	return ignored, nil
}

// Filter returns the paths that aren't ignored, keeping their order.
func Filter(lines, paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		ignored, err := Ignored(lines, p)
		if err != nil {
			return nil, err
		}
		if !ignored {
			out = append(out, p)
		}
	}
	return out, nil
}
`,
		},
		Turns: script(
			local("How `**` works in this package: it stands for zero or more whole path segments, so a/**/b matches a/b as well as a/x/y/b. Add `func IsGlob(pattern string) bool`: whether the pattern contains `*` or `?`."),
			local("Also, dotfiles stay out of it: `*`, `?` and `**` never match a segment that starts with a dot, unless the pattern segment itself starts with a dot. So *.txt doesn't match .notes.txt, and **/x doesn't reach into .git. There's no matching code yet. No code changes."),
			cloud("Add `func Match(pattern, path string) bool`. Both are slash-separated and compared segment by segment, using Segments. Within a segment `*` is any run of characters and `?` is exactly one. A segment that's exactly `**` spans path segments."),
			cloud("Read docs/ignore-format.txt and tell me what it implies for reading those files. No code changes."),
			cloud("Add `var ErrBadPattern = errors.New(\"bad pattern\")` and `func Validate(pattern string) error`, which returns it for an empty pattern, or when `**` shares a segment with other characters, as in x/**y. Anything here that takes patterns and can return an error gives this one for a pattern that fails Validate."),
			local("Add `func Ignored(lines []string, path string) (bool, error)`: whether path is ignored, given the lines of an ignore file."),
			cloud("Add `func Filter(lines, paths []string) ([]string, error)`: the paths that aren't ignored, in the order given."),
		),
		Carries: []Carry{
			{
				What:        "** also matches zero segments",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2},
				Tests:       []string{"TestDoubleStarZeroInside", "TestDoubleStarZeroAtEnds"},
				Absent:      []string{"zero or more", "zero segments", "a/b"},
			},
			{
				What:        "wildcards never match a segment starting with a dot unless the pattern segment does",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{2},
				Tests:       []string{"TestStarPassesOverDotfile", "TestQuestionPassesOverDotfile", "TestDoubleStarStaysOutOfDotDirs"},
				Absent:      []string{"dot", "hidden"},
			},
			{
				What:        "ignore files have # lines, ! re-includes, and the last matching line decides (docs/ignore-format.txt)",
				Kind:        CarryDiscovered,
				Established: 3,
				Needed:      []int{5},
				Source:      "docs/ignore-format.txt",
				Tests:       []string{"TestIgnoredSkipsCommentLines", "TestIgnoredReinclude", "TestIgnoredLastLineDecides"},
				Absent:      []string{"comment", "#", "!", "last line", "last of", "re-include"},
			},
			{
				What:        "taking a pattern that fails Validate gives ErrBadPattern",
				Kind:        CarryEmbodied,
				Established: 4,
				Needed:      []int{5, 6},
				Tests:       []string{"TestIgnoredBadPatternError", "TestFilterBadPatternError"},
				Absent:      []string{"errbadpattern", "validate", "sentinel"},
			},
		},
	}
}
