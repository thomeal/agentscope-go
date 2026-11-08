package s2

func accB10() Task {
	return Task{
		ID:     "s2-b10-dns-records",
		Set:    "acceptance",
		System: system("a parser for DNS-style zone records"),
		Files: map[string]string{
			"go.mod": goMod,
			"zone.go": `package zone

// Record is one resource record.
type Record struct {
	Name  string
	TTL   int // seconds
	Type  string
	Value string
}

// IsAddress reports whether the record holds an IP address.
func (r Record) IsAddress() bool { return r.Type == "A" || r.Type == "AAAA" }
`,
			"docs/zone-format.txt": `Zone file format

Blank lines are skipped. A ";" starts a comment that runs to the end of the line.

A line "$ORIGIN example.com." sets the origin for the lines below it. A record
name that does not end in a dot is relative to the origin, and the name "@" is
the origin itself. A name ending in a dot is complete as it stands. With no
$ORIGIN line above it, a name is taken as it stands.
`,
			"zone_test.go": `package zone

import "testing"

func TestIsAddress(t *testing.T) {
	if !(Record{Type: "A"}).IsAddress() || (Record{Type: "TXT"}).IsAddress() {
		t.Fatal("IsAddress is wrong")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package zone_test

import (
	"errors"
	"slices"
	"testing"

	zone "fixture"
)

func line(t *testing.T, s string) zone.Record {
	t.Helper()
	r, err := zone.ParseLine(s)
	if err != nil {
		t.Fatalf("ParseLine(%q): %v", s, err)
	}
	return r
}

func parse(t *testing.T, text string) []zone.Record {
	t.Helper()
	rs, err := zone.Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return rs
}

func values(rs []zone.Record) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Value)
	}
	return out
}

func names(rs []zone.Record) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestRecordString(t *testing.T) {
	r := zone.Record{Name: "www", TTL: 300, Type: "A", Value: "1.2.3.4"}
	if got := r.String(); got != "www 300 A 1.2.3.4" {
		t.Fatalf("String = %q", got)
	}
}

func TestParseLineFields(t *testing.T) {
	want := zone.Record{Name: "www", TTL: 300, Type: "A", Value: "1.2.3.4"}
	if got := line(t, "www 300 A 1.2.3.4"); got != want {
		t.Fatalf("got %+v", got)
	}
	if got := line(t, "www\t300\tA\t1.2.3.4"); got != want {
		t.Fatalf("tabs: got %+v", got)
	}
}

func TestParseLineValueWithSpaces(t *testing.T) {
	if got := line(t, "note 300 TXT hello big world").Value; got != "hello big world" {
		t.Fatalf("Value = %q", got)
	}
}

func TestParseLineTTLUnits(t *testing.T) {
	for ttl, want := range map[string]int{"2m": 120, "1h": 3600, "1d": 86400, "90": 90} {
		if got := line(t, "www "+ttl+" A 1.2.3.4").TTL; got != want {
			t.Errorf("%s: %d", ttl, got)
		}
	}
}

func TestParseLineBad(t *testing.T) {
	for _, s := range []string{"www 300", "www abc A 1.2.3.4", ""} {
		if _, err := zone.ParseLine(s); err == nil {
			t.Errorf("%q: no error", s)
		}
	}
}

func TestParseLineCanonicalName(t *testing.T) {
	if got := line(t, "WWW.Example.COM. 300 A 1.2.3.4").Name; got != "www.example.com" {
		t.Fatalf("Name = %q", got)
	}
}

func TestParseLineFloor(t *testing.T) {
	for _, s := range []string{"www 5 A 1.2.3.4", "www 59 A 1.2.3.4", "www 0 A 1.2.3.4"} {
		if got := line(t, s).TTL; got != 60 {
			t.Errorf("%q: TTL %d", s, got)
		}
	}
}

func TestParseRecords(t *testing.T) {
	rs := parse(t, "a 300 A 1.1.1.1\nb 300 A 2.2.2.2\n")
	if !slices.Equal(names(rs), []string{"a", "b"}) {
		t.Fatalf("got %+v", rs)
	}
}

func TestParseBlankLines(t *testing.T) {
	rs := parse(t, "\na 300 A 1.1.1.1\n\n   \nb 300 A 2.2.2.2\n\n")
	if len(rs) != 2 {
		t.Fatalf("got %+v", rs)
	}
}

func TestParseFloor(t *testing.T) {
	rs := parse(t, "a 5 A 1.1.1.1\n")
	if len(rs) != 1 || rs[0].TTL != 60 {
		t.Fatalf("got %+v", rs)
	}
}

func TestParseComments(t *testing.T) {
	rs := parse(t, "; the web tier\na 300 A 1.1.1.1 ; primary\n")
	if len(rs) != 1 || rs[0].Value != "1.1.1.1" {
		t.Fatalf("got %+v", rs)
	}
}

func TestParseOrigin(t *testing.T) {
	rs := parse(t, "$ORIGIN example.com.\nwww 300 A 1.1.1.1\n$ORIGIN example.org.\nwww 300 A 2.2.2.2\n")
	if !slices.Equal(names(rs), []string{"www.example.com", "www.example.org"}) {
		t.Fatalf("got %+v", rs)
	}
}

func TestParseAt(t *testing.T) {
	rs := parse(t, "$ORIGIN example.com.\n@ 300 A 1.1.1.1\n")
	if !slices.Equal(names(rs), []string{"example.com"}) {
		t.Fatalf("got %+v", rs)
	}
}

func TestParseOriginCompleteName(t *testing.T) {
	rs := parse(t, "$ORIGIN example.com.\nmail.other.org. 300 A 1.1.1.1\n")
	if !slices.Equal(names(rs), []string{"mail.other.org"}) {
		t.Fatalf("got %+v", rs)
	}
}

func TestParseCanonicalName(t *testing.T) {
	rs := parse(t, "Mail.Other.ORG. 300 A 1.1.1.1\n")
	if !slices.Equal(names(rs), []string{"mail.other.org"}) {
		t.Fatalf("got %+v", rs)
	}
}

func TestParseBadLineIsError(t *testing.T) {
	if _, err := zone.Parse("a 300 A 1.1.1.1\nbroken\n"); err == nil {
		t.Fatal("no error")
	}
}

func TestParseErrorLine(t *testing.T) {
	_, err := zone.Parse("a 300 A 1.1.1.1\n\nbroken\n")
	var pe *zone.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v", err)
	}
	if pe.Line != 3 {
		t.Fatalf("Line = %d", pe.Line)
	}
}

func TestNames(t *testing.T) {
	rs := []zone.Record{{Name: "b"}, {Name: "a"}, {Name: "b"}}
	if got := zone.Names(rs); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Names = %v", got)
	}
}

func TestLookupFindsByNameAndType(t *testing.T) {
	rs := []zone.Record{
		{Name: "a", Type: "A", Value: "1"},
		{Name: "a", Type: "TXT", Value: "2"},
		{Name: "b", Type: "A", Value: "3"},
		{Name: "a", Type: "A", Value: "4"},
	}
	if got := values(zone.Lookup(rs, "a", "A")); !slices.Equal(got, []string{"1", "4"}) {
		t.Fatalf("Lookup = %v", got)
	}
	if got := zone.Lookup(rs, "a", "AAAA"); len(got) != 0 {
		t.Fatalf("Lookup = %v", got)
	}
}

func TestLookupQueryForm(t *testing.T) {
	rs := []zone.Record{{Name: "www.example.com", Type: "A", Value: "1"}}
	if got := values(zone.Lookup(rs, "WWW.Example.com.", "A")); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("Lookup = %v", got)
	}
}

func TestLookupMXOrder(t *testing.T) {
	rs := []zone.Record{
		{Name: "a", Type: "MX", Value: "20 second.example"},
		{Name: "a", Type: "MX", Value: "10 first.example"},
	}
	want := []string{"10 first.example", "20 second.example"}
	if got := values(zone.Lookup(rs, "a", "MX")); !slices.Equal(got, want) {
		t.Fatalf("Lookup = %v", got)
	}
}

func TestLookupMXNumberAndTies(t *testing.T) {
	rs := []zone.Record{
		{Name: "a", Type: "MX", Value: "100 big.example"},
		{Name: "a", Type: "MX", Value: "20 tie-b.example"},
		{Name: "a", Type: "MX", Value: "20 tie-a.example"},
	}
	want := []string{"20 tie-b.example", "20 tie-a.example", "100 big.example"}
	if got := values(zone.Lookup(rs, "a", "MX")); !slices.Equal(got, want) {
		t.Fatalf("Lookup = %v", got)
	}
}
`},
		Solution: map[string]string{
			"zone.go": `package zone

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Record is one resource record.
type Record struct {
	Name  string
	TTL   int // seconds
	Type  string
	Value string
}

// IsAddress reports whether the record holds an IP address.
func (r Record) IsAddress() bool { return r.Type == "A" || r.Type == "AAAA" }

// String gives name, ttl, type and value separated by single spaces.
func (r Record) String() string {
	return fmt.Sprintf("%s %d %s %s", r.Name, r.TTL, r.Type, r.Value)
}

// ParseError reports a line of a zone that can't be read.
type ParseError struct {
	Line int
	Msg  string
}

// Error describes the line.
func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// canon returns the canonical form of a name.
func canon(name string) string { return strings.ToLower(strings.TrimSuffix(name, ".")) }

func parseTTL(s string) (int, error) {
	unit := map[byte]int{'m': 60, 'h': 3600, 'd': 86400}[s[len(s)-1]]
	if unit > 0 {
		s = s[:len(s)-1]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, errors.New("bad ttl")
	}
	return n * max(unit, 1), nil
}

// ParseLine reads one record.
func ParseLine(line string) (Record, error) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return Record{}, errors.New("want name, ttl, type and value")
	}
	ttl, err := parseTTL(fields[1])
	if err != nil {
		return Record{}, err
	}
	return Record{
		Name:  canon(fields[0]),
		TTL:   max(ttl, 60),
		Type:  fields[2],
		Value: strings.Join(fields[3:], " "),
	}, nil
}

// qualify applies the origin to a name as it was written.
func qualify(name, origin string) string {
	switch {
	case name == "@":
		return origin
	case strings.HasSuffix(name, ".") || origin == "":
		return name
	}
	return name + "." + origin
}

// Parse reads a whole zone file.
func Parse(text string) ([]Record, error) {
	var out []Record
	origin := ""
	for i, line := range strings.Split(text, "\n") {
		line, _, _ = strings.Cut(line, ";")
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
			continue
		case fields[0] == "$ORIGIN" && len(fields) == 2:
			origin = strings.TrimSuffix(fields[1], ".")
			continue
		}
		rec, err := ParseLine(line)
		if err != nil {
			return nil, &ParseError{Line: i + 1, Msg: err.Error()}
		}
		rec.Name = canon(qualify(fields[0], origin))
		out = append(out, rec)
	}
	return out, nil
}

// Names returns the distinct names, sorted.
func Names(records []Record) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range records {
		if !seen[r.Name] {
			seen[r.Name] = true
			out = append(out, r.Name)
		}
	}
	sort.Strings(out)
	return out
}

// preference is the first word of an MX value.
func preference(r Record) int {
	first, _, _ := strings.Cut(r.Value, " ")
	n, _ := strconv.Atoi(first)
	return n
}

// Lookup returns the records with that name and type.
func Lookup(records []Record, name, typ string) []Record {
	var out []Record
	for _, r := range records {
		if r.Name == canon(name) && r.Type == typ {
			out = append(out, r)
		}
	}
	if typ == "MX" {
		sort.SliceStable(out, func(a, b int) bool { return preference(out[a]) < preference(out[b]) })
	}
	return out
}
`,
		},
		Turns: script(
			local("House rule for names in this package: they're held in canonical form, lower-case and with no trailing dot, because DNS doesn't care about either and we want plain string comparison to work. Add `func (r Record) String() string`, giving name, ttl, type and value separated by single spaces."),
			local("Also, some of our resolvers misbehave on very short TTLs, so any TTL under 60 seconds gets raised to 60 when we read it. There's no reading code yet. No code changes."),
			cloud("Add `func ParseLine(line string) (Record, error)`. A line is name, ttl, type and value, separated by spaces or tabs, and the value is everything after the type, with single spaces between its words. ttl is a number of seconds, or a number with an m, h or d suffix. A line of the wrong shape or with a bad ttl is an error."),
			cloud("Read docs/zone-format.txt and tell me what reading a whole file has to do beyond single lines. No code changes."),
			cloud("Add `type ParseError struct { Line int; Msg string }` with `func (e *ParseError) Error() string`. Line counts from 1 and counts every line of the input, including ones with no record on them. Whenever something reads a whole zone and hits a line it can't read, a *ParseError is what it returns."),
			local("Add `func Parse(text string) ([]Record, error)`, which reads a whole zone file."),
			cloud("For mail routing: when several MX records answer a query, the lowest preference number goes first, and equal preferences stay in the order they were given. The number is the first word of Value. Add `func Names(records []Record) []string`: the distinct names, sorted."),
			local("Add `func Lookup(records []Record, name, typ string) []Record`: the records with that name and type."),
		),
		Carries: []Carry{
			{
				What:        "names are held and compared lower-case with no trailing dot",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2, 7},
				Tests:       []string{"TestParseLineCanonicalName", "TestParseCanonicalName", "TestParseOriginCompleteName", "TestLookupQueryForm"},
				Absent:      []string{"canonical", "lower-case", "lowercase", "trailing dot"},
			},
			{
				What:        "any TTL under 60 seconds is raised to 60 when read",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{2},
				Tests:       []string{"TestParseLineFloor", "TestParseFloor"},
				Absent:      []string{"60", "raised", "short ttl", "resolver"},
			},
			{
				What:        "zone files have ; comments, $ORIGIN and @ (docs/zone-format.txt)",
				Kind:        CarryDiscovered,
				Established: 3,
				Needed:      []int{5},
				Source:      "docs/zone-format.txt",
				Tests:       []string{"TestParseComments", "TestParseOrigin", "TestParseAt", "TestParseOriginCompleteName"},
				Absent:      []string{"$origin", "comment", "relative"},
			},
			{
				What:        "reading a whole zone reports a bad line with a *ParseError counting every line",
				Kind:        CarryEmbodied,
				Established: 4,
				Needed:      []int{5},
				Tests:       []string{"TestParseErrorLine"},
				Absent:      []string{"parseerror", "counts from 1"},
			},
			{
				What:        "MX records come back lowest preference first, ties in the order given",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{7},
				Tests:       []string{"TestLookupMXOrder", "TestLookupMXNumberAndTies"},
				Absent:      []string{"mx", "preference", "lowest", "mail routing"},
			},
		},
	}
}
