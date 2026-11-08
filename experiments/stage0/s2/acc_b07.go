package s2

func accB07() Task {
	return Task{
		ID:     "s2-b07-version-compare",
		Set:    "acceptance",
		System: system("a version number library"),
		Files: map[string]string{
			"go.mod": goMod,
			"version.go": `package version

// Version is a parsed version number.
type Version struct {
	Major, Minor, Patch int
	Pre                 string // pre-release part, without the "-"
	Build               string // build part, without the "+"
}

// IsPre reports whether this is a pre-release.
func (v Version) IsPre() bool { return v.Pre != "" }
`,
			"docs/release-policy.txt": `Release policy

Versions are major.minor.patch, with an optional pre-release after a "-" and an
optional build part after a "+".

Order: compare major, minor and patch as numbers. If those are equal, a
pre-release comes before the release itself, so 2.0.0-rc.1 is before 2.0.0.

Two pre-releases compare dot-separated identifier by identifier. Numeric
identifiers compare as numbers, so rc.10 is after rc.9. Other identifiers compare
as text, and a numeric one is before a text one. If all shared identifiers are
equal, the one with fewer identifiers is first.

The build part plays no role in the order.
`,
			"version_test.go": `package version

import "testing"

func TestIsPre(t *testing.T) {
	if !(Version{Pre: "rc.1"}).IsPre() || (Version{Major: 1}).IsPre() {
		t.Fatal("IsPre is wrong")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package version_test

import (
	"errors"
	"testing"

	version "fixture"
)

func parse(t *testing.T, s string) version.Version {
	t.Helper()
	v, err := version.Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return v
}

func before(t *testing.T, a, b string) {
	t.Helper()
	x, y := parse(t, a), parse(t, b)
	if c := version.Compare(x, y); c >= 0 {
		t.Errorf("Compare(%s, %s) = %d", a, b, c)
	}
	if c := version.Compare(y, x); c <= 0 {
		t.Errorf("Compare(%s, %s) = %d", b, a, c)
	}
}

func TestCore(t *testing.T) {
	v := version.Version{Major: 1, Minor: 4, Patch: 2, Pre: "rc.1", Build: "b"}
	if got := v.Core(); got != "1.4.2" {
		t.Fatalf("Core = %q", got)
	}
}

func TestParsePlain(t *testing.T) {
	v := parse(t, "1.4.2")
	if v.Major != 1 || v.Minor != 4 || v.Patch != 2 || v.Pre != "" || v.Build != "" {
		t.Fatalf("got %+v", v)
	}
}

func TestParseFull(t *testing.T) {
	v := parse(t, "1.4.2-rc.1+build.7")
	if v.Major != 1 || v.Minor != 4 || v.Patch != 2 || v.Pre != "rc.1" || v.Build != "build.7" {
		t.Fatalf("got %+v", v)
	}
}

func TestParseBuildOnly(t *testing.T) {
	v := parse(t, "1.4.2+build.7")
	if v.Pre != "" || v.Build != "build.7" {
		t.Fatalf("got %+v", v)
	}
}

func TestParseInvalidIsError(t *testing.T) {
	for _, s := range []string{"", "abc", "1.x.3", "1.2.3.4"} {
		if _, err := version.Parse(s); err == nil {
			t.Errorf("%q: no error", s)
		}
	}
}

func TestParseInvalidError(t *testing.T) {
	for _, s := range []string{"", "abc", "1.x.3", "1.2.3.4"} {
		if _, err := version.Parse(s); !errors.Is(err, version.ErrInvalid) {
			t.Errorf("%q: err = %v", s, err)
		}
	}
}

func TestParseTagPrefix(t *testing.T) {
	v := parse(t, "v1.4.2")
	if v.Major != 1 || v.Minor != 4 || v.Patch != 2 {
		t.Fatalf("got %+v", v)
	}
}

func TestParseNoPatch(t *testing.T) {
	v := parse(t, "v1.4")
	if v.Major != 1 || v.Minor != 4 || v.Patch != 0 {
		t.Fatalf("got %+v", v)
	}
}

func TestCompareNumbers(t *testing.T) {
	before(t, "1.2.3", "1.2.4")
	before(t, "1.9.0", "1.10.0")
	before(t, "9.9.9", "10.0.0")
}

func TestCompareBuildIgnored(t *testing.T) {
	if c := version.Compare(parse(t, "1.0.0+a"), parse(t, "1.0.0+b")); c != 0 {
		t.Fatalf("Compare = %d", c)
	}
}

func TestPreBeforeRelease(t *testing.T) {
	before(t, "2.0.0-rc.1", "2.0.0")
}

func TestPreNumericIdentifiers(t *testing.T) {
	before(t, "1.0.0-rc.9", "1.0.0-rc.10")
}

func TestPreTextIdentifiers(t *testing.T) {
	before(t, "1.0.0-alpha", "1.0.0-beta")
	before(t, "1.0.0-1", "1.0.0-alpha")
	before(t, "1.0.0-rc", "1.0.0-rc.1")
}

func TestString(t *testing.T) {
	v := version.Version{Major: 1, Minor: 4, Patch: 2, Pre: "rc.1", Build: "build.7"}
	if got := v.String(); got != "1.4.2-rc.1+build.7" {
		t.Errorf("full: %q", got)
	}
	if got := (version.Version{Major: 1, Minor: 4}).String(); got != "1.4.0" {
		t.Errorf("plain: %q", got)
	}
}

func TestLatestPicksHighest(t *testing.T) {
	got, err := version.Latest([]string{"1.2.0", "1.10.0", "1.9.0"})
	if err != nil || got != "1.10.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLatestAllPre(t *testing.T) {
	got, err := version.Latest([]string{"1.3.0-rc.1", "1.3.0-rc.2"})
	if err != nil || got != "1.3.0-rc.2" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLatestEmpty(t *testing.T) {
	if _, err := version.Latest(nil); err == nil {
		t.Fatal("no error")
	}
}

func TestLatestPassesOverPre(t *testing.T) {
	got, err := version.Latest([]string{"1.2.0", "1.3.0-rc.1"})
	if err != nil || got != "1.2.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLatestSkipsUnparsable(t *testing.T) {
	got, err := version.Latest([]string{"nightly", "1.2.0", "latest"})
	if err != nil || got != "1.2.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLatestAsGiven(t *testing.T) {
	got, err := version.Latest([]string{"v1.2.0", "v1.10.0"})
	if err != nil || got != "v1.10.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLess(t *testing.T) {
	got, err := version.Less("1.2.0", "1.10.0")
	if err != nil || !got {
		t.Fatalf("got %v, %v", got, err)
	}
	got, err = version.Less("1.10.0", "1.2.0")
	if err != nil || got {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestLessInvalidError(t *testing.T) {
	if _, err := version.Less("abc", "1.0.0"); !errors.Is(err, version.ErrInvalid) {
		t.Errorf("first: err = %v", err)
	}
	if _, err := version.Less("1.0.0", "abc"); !errors.Is(err, version.ErrInvalid) {
		t.Errorf("second: err = %v", err)
	}
}
`},
		Solution: map[string]string{
			"version.go": `package version

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalid is returned for any string that isn't a version.
var ErrInvalid = errors.New("invalid version")

// Version is a parsed version number.
type Version struct {
	Major, Minor, Patch int
	Pre                 string // pre-release part, without the "-"
	Build               string // build part, without the "+"
}

// IsPre reports whether this is a pre-release.
func (v Version) IsPre() bool { return v.Pre != "" }

// Core returns major.minor.patch.
func (v Version) Core() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// String returns the full form.
func (v Version) String() string {
	s := v.Core()
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// Parse reads a version, or a git tag with a leading v and maybe no patch.
func Parse(s string) (Version, error) {
	var v Version
	s = strings.TrimPrefix(s, "v")
	s, v.Build, _ = strings.Cut(s, "+")
	s, v.Pre, _ = strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) == 2 {
		parts = append(parts, "0")
	}
	if len(parts) != 3 {
		return Version{}, ErrInvalid
	}
	for i, field := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return Version{}, ErrInvalid
		}
		*field = n
	}
	return v, nil
}

// Compare orders two versions as docs/release-policy.txt says.
func Compare(a, b Version) int {
	c := cmp.Or(cmp.Compare(a.Major, b.Major), cmp.Compare(a.Minor, b.Minor), cmp.Compare(a.Patch, b.Patch))
	if c != 0 {
		return c
	}
	return comparePre(a.Pre, b.Pre)
}

func comparePre(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(as), len(bs)) {
		if c := compareID(as[i], bs[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(as), len(bs))
}

func compareID(a, b string) int {
	x, aerr := strconv.Atoi(a)
	y, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		return cmp.Compare(x, y)
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	}
	return cmp.Compare(a, b)
}

// offerBefore reports whether a is a better version to offer than b.
func offerBefore(a, b Version) bool {
	if a.IsPre() != b.IsPre() {
		return b.IsPre()
	}
	return Compare(a, b) > 0
}

// Latest returns the tag to offer, as it was given.
func Latest(tags []string) (string, error) {
	best, bestV, found := "", Version{}, false
	for _, t := range tags {
		v, err := Parse(t)
		if err != nil {
			continue
		}
		if !found || offerBefore(v, bestV) {
			best, bestV, found = t, v, true
		}
	}
	if !found {
		return "", errors.New("nothing to offer")
	}
	return best, nil
}

// Less reports whether version a comes before version b.
func Less(a, b string) (bool, error) {
	x, err := Parse(a)
	if err != nil {
		return false, err
	}
	y, err := Parse(b)
	if err != nil {
		return false, err
	}
	return Compare(x, y) < 0, nil
}
`,
		},
		Turns: script(
			local("Our git tags look like v1.4.2, and some older ones leave off the patch: v1.4. Anything that reads a version has to take those like any other, with a missing patch meaning 0. Add `func (v Version) Core() string`, which gives just major.minor.patch, like 1.4.2."),
			local("Add `var ErrInvalid = errors.New(\"invalid version\")`. Whenever something here is handed a string that isn't a version, that's the error it returns. Nothing uses it yet."),
			cloud("Add `func Parse(s string) (Version, error)`, for strings like 1.4.2, 1.4.2-rc.1, 1.4.2+build.7 or 1.4.2-rc.1+build.7."),
			cloud("Read docs/release-policy.txt and tell me how it says versions are ordered. No code changes."),
			local("Add `func Compare(a, b Version) int`: negative if a comes before b, positive if it comes after, and 0 if they're level."),
			cloud("When we pick a release to offer users, a pre-release only counts if there's nothing but pre-releases, and tags that don't parse are just skipped, because old repos are full of junk tags. Add `func (v Version) String() string`, giving the full form like 1.4.2-rc.1+build.7."),
			local("Add `func Latest(tags []string) (string, error)`: the tag to offer, returned exactly as it was given. If there's nothing to offer, return an error."),
			cloud("Add `func Less(a, b string) (bool, error)`: whether version a comes before version b."),
		),
		Carries: []Carry{
			{
				What:        "pre-releases come before their release, and numeric identifiers compare as numbers (docs/release-policy.txt)",
				Kind:        CarryDiscovered,
				Established: 3,
				Needed:      []int{4},
				Source:      "docs/release-policy.txt",
				Tests:       []string{"TestPreBeforeRelease", "TestPreNumericIdentifiers"},
				Absent:      []string{"numeric", "identifier", "as numbers", "rc.10"},
			},
			{
				What:        "git tags with a leading v and no patch read like any other version",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2, 6},
				Tests:       []string{"TestParseTagPrefix", "TestParseNoPatch", "TestLatestAsGiven"},
				Absent:      []string{"v1.", "leave off", "missing patch"},
			},
			{
				What:        "a pre-release is only offered if there is nothing else, and unparsable tags are skipped",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{6},
				Tests:       []string{"TestLatestPassesOverPre", "TestLatestSkipsUnparsable"},
				Absent:      []string{"only counts", "nothing but", "junk", "skipped"},
			},
			{
				What:        "ErrInvalid is returned for any string that isn't a version",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{2, 7},
				Tests:       []string{"TestParseInvalidError", "TestLessInvalidError"},
				Absent:      []string{"errinvalid", "sentinel", "isn't a version"},
			},
		},
	}
}
