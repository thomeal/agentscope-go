package s2

func longB02() Task {
	return Task{
		ID:     "s2-long-b02-po-catalog",
		Set:    "long",
		System: system("a translation catalog tool for gettext PO files"),
		Files: map[string]string{
			"go.mod": goMod,
			"entry.go": `package pocat

// Entry is one message of a catalog.
type Entry struct {
	Context      string   // msgctxt
	ID           string   // msgid
	Plural       string   // msgid_plural
	Translations []string // msgstr, or one per plural form
	Comments     []string // translator comments, without the leading "# "
	Refs         []string // source references, without the leading "#: "
	Fuzzy        bool
	Obsolete     bool
}

// Key identifies an entry: the same text in another context is another message.
func (e Entry) Key() string { return e.Context + "\x04" + e.ID }

// HasPlural reports whether the message has plural forms.
func (e Entry) HasPlural() bool { return e.Plural != "" }

// Catalog is a whole PO file.
type Catalog struct {
	Lang    string
	Entries []Entry
}

// Find returns the index of the entry with that key, or -1.
func (c *Catalog) Find(key string) int {
	for i, e := range c.Entries {
		if e.Key() == key {
			return i
		}
	}
	return -1
}
`,
			"parse.go": `package pocat

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrSyntax is the error for text that is not a valid catalog.
var ErrSyntax = errors.New("catalog syntax error")

// parser holds the entry being read.
type parser struct {
	cat   *Catalog
	cur   Entry
	open  bool
	inStr bool           // a msgstr was seen, so another keyword starts a new entry
	field func(s string) // where a continuation string goes
}

func (p *parser) flush() {
	if p.open {
		p.cat.Entries = append(p.cat.Entries, p.cur)
	}
	p.cur, p.open, p.inStr, p.field = Entry{}, false, false, nil
}

func (p *parser) comment(line string) {
	if p.inStr {
		p.flush()
	}
	switch {
	case strings.HasPrefix(line, "#,"):
		p.cur.Fuzzy = p.cur.Fuzzy || strings.Contains(line, "fuzzy")
	case strings.HasPrefix(line, "#:"):
		p.cur.Refs = append(p.cur.Refs, strings.Fields(line[2:])...)
	case strings.HasPrefix(line, "# "):
		p.cur.Comments = append(p.cur.Comments, line[2:])
	}
}

// start opens a keyword and returns where its strings go.
func (p *parser) start(word string) (func(string), error) {
	if p.inStr && !strings.HasPrefix(word, "msgstr") {
		p.flush()
	}
	p.open = true
	c := &p.cur
	switch {
	case word == "msgctxt":
		return func(s string) { c.Context += s }, nil
	case word == "msgid":
		return func(s string) { c.ID += s }, nil
	case word == "msgid_plural":
		return func(s string) { c.Plural += s }, nil
	case word == "msgstr" || strings.HasPrefix(word, "msgstr["):
		p.inStr = true
		i := len(c.Translations)
		c.Translations = append(c.Translations, "")
		return func(s string) { c.Translations[i] += s }, nil
	}
	return nil, fmt.Errorf("%w: unknown keyword %q", ErrSyntax, word)
}

func (p *parser) line(raw string) error {
	line := strings.TrimSpace(raw)
	rest, obsolete := strings.CutPrefix(line, "#~")
	if obsolete {
		line = strings.TrimSpace(rest)
	}
	switch {
	case line == "":
		p.flush()
		return nil
	case strings.HasPrefix(line, "#"):
		p.comment(line)
		return nil
	case !strings.HasPrefix(line, "\""):
		word, text, _ := strings.Cut(line, " ")
		field, err := p.start(word)
		if err != nil {
			return err
		}
		p.field, line = field, strings.TrimSpace(text)
	}
	s, err := strconv.Unquote(line)
	if err != nil || p.field == nil {
		return fmt.Errorf("%w: bad string %q", ErrSyntax, line)
	}
	p.field(s)
	p.cur.Obsolete = p.cur.Obsolete || obsolete
	return nil
}

// Parse reads a catalog.
func Parse(text string) (*Catalog, error) {
	p := &parser{cat: &Catalog{}}
	for _, raw := range strings.Split(text, "\n") {
		if err := p.line(raw); err != nil {
			return nil, err
		}
	}
	p.flush()
	return p.cat, nil
}
`,
			"write.go": `package pocat

import (
	"fmt"
	"strconv"
	"strings"
)

// Write writes a catalog as a PO file.
func Write(c *Catalog) string {
	var b strings.Builder
	for _, e := range c.Entries {
		for _, r := range e.Refs {
			fmt.Fprintf(&b, "#: %s\n", r)
		}
		fmt.Fprintf(&b, "msgid %s\n", strconv.Quote(e.ID))
		for _, t := range e.Translations {
			fmt.Fprintf(&b, "msgstr %s\n", strconv.Quote(t))
		}
		b.WriteString("\n")
	}
	return b.String()
}
`,
			"merge.go": `package pocat

// Keys returns the keys of the catalog's entries.
func (c *Catalog) Keys() map[string]bool {
	out := make(map[string]bool, len(c.Entries))
	for _, e := range c.Entries {
		out[e.Key()] = true
	}
	return out
}
`,
			"stats.go": `package pocat

// CountEntries returns how many entries the catalog has of any kind.
func (c *Catalog) CountEntries() int { return len(c.Entries) }
`,
			"check.go": `package pocat

import "fmt"

// Problem is one thing a reviewer should look at. Key is the Key of the entry.
type Problem struct {
	Key, Code string
}

// String writes the problem for a report.
func (p Problem) String() string { return fmt.Sprintf("%s: %q", p.Code, p.Key) }
`,
			"export.go": `package pocat

import "strings"

// splitLines cuts a file into lines, leaving out blank ones.
func splitLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
`,
			"docs/workflow.txt": `How our translators work

What counts as translated
- A message is translated when it is not fuzzy, not obsolete, and every one of
  its forms has text.
- Fuzzy means a draft that needs a look. A fuzzy message is never shipped, and
  reviewers do not check it.
- Obsolete messages are not part of any progress number.

When the source changes (merging with a new template)
- Translator comments belong to the catalog and are kept. Source references
  belong to the template and always come from it.
- A message that stays keeps its translation, and if it was fuzzy it stays fuzzy.
- If a message's source text changed, its old translation is offered when the
  two texts are at least 70% similar. It comes in fuzzy.
- A message gone from the template becomes obsolete and keeps its translation,
  so that it can come back. If it never had a translation it is dropped.

Who does what
- Translators work per language and hand files back through the web app.
- A reviewer signs off a language once a release, a week before it ships.
`,
			"docs/plural-rules.txt": `Plural forms per language

1 form: ja, ko, vi, zh
2 forms: da, de, en, es, fr, it, nl, pt, sv
3 forms: cs, pl, ru, uk
6 forms: ar

A language code may carry a region, as in pt_BR or zh_TW. The region never changes
the number of forms. A language that is not listed here has 2 forms.
`,
			"docs/export-format.txt": `Flat files for the web app

The web app loads one text file per language, with one line per string:

    key=value

- Only translated messages are in the file.
- A message with plural forms has one line per form, with the form's index
  added to its key: files[0]=..., files[1]=...
- Lines are sorted by key, in plain string order.
- In a value, a line break is written as \n and a \ is written as \\.
- In a key, an equals sign is written as \=. Nothing else in a key is escaped.

The file ends with a line break. An empty catalog gives an empty file.
`,
			"catalog_test.go": `package pocat

import "testing"

func TestParseSmallFile(t *testing.T) {
	c, err := Parse("#: a.c\nmsgid \"Hi\"\nmsgstr \"Hallo\"\n\nmsgctxt \"menu\"\nmsgid \"Open\"\nmsgstr \"\"\n\"Offnen\"\n")
	if err != nil || len(c.Entries) != 2 {
		t.Fatalf("Parse = %+v, %v", c, err)
	}
	if e := c.Entries[1]; e.Context != "menu" || e.ID != "Open" || e.Translations[0] != "Offnen" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestParseFlagsAndForms(t *testing.T) {
	c, err := Parse("#, fuzzy\nmsgid \"a\"\nmsgid_plural \"b\"\nmsgstr[0] \"x\"\nmsgstr[1] \"y\"\n\n#~ msgid \"gone\"\n#~ msgstr \"weg\"\n")
	if err != nil || len(c.Entries) != 2 {
		t.Fatalf("Parse = %+v, %v", c, err)
	}
	if e := c.Entries[0]; !e.Fuzzy || !e.HasPlural() || len(e.Translations) != 2 {
		t.Fatalf("entry = %+v", e)
	}
	if !c.Entries[1].Obsolete || c.Entries[1].ID != "gone" {
		t.Fatalf("entry = %+v", c.Entries[1])
	}
}

func TestFindByKey(t *testing.T) {
	c := &Catalog{Entries: []Entry{{ID: "a"}, {Context: "x", ID: "a"}}}
	if got := c.Find(Entry{Context: "x", ID: "a"}.Key()); got != 1 {
		t.Fatalf("Find = %d", got)
	}
	if got := c.Find("nothing"); got != -1 {
		t.Fatalf("Find = %d", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package pocat_test

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	pocat "fixture"
)

func msg(id string, forms ...string) pocat.Entry { return pocat.Entry{ID: id, Translations: forms} }

func header() pocat.Entry { return msg("", "Language: de\n") }

func catalog(lang string, entries ...pocat.Entry) *pocat.Catalog {
	return &pocat.Catalog{Lang: lang, Entries: entries}
}

func codes(problems []pocat.Problem) []string {
	var out []string
	for _, p := range problems {
		out = append(out, p.Code)
	}
	return out
}

func ids(entries []pocat.Entry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

func TestSyntaxErrorGivesLine(t *testing.T) {
	_, err := pocat.Parse("msgid \"a\"\nmsgstr \"b\"\n\nbogus \"c\"\n")
	var se *pocat.SyntaxError
	if !errors.As(err, &se) || se.Line != 4 {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, pocat.ErrSyntax) {
		t.Fatalf("%v does not match ErrSyntax", err)
	}
}

func TestSyntaxErrorOnBadString(t *testing.T) {
	for text, line := range map[string]int{"msgid \"a\nmsgstr \"b\"\n": 1, "\n\n\"stray\"\n": 3} {
		_, err := pocat.Parse(text)
		var se *pocat.SyntaxError
		if !errors.As(err, &se) || se.Line != line {
			t.Errorf("%q: %v", text, err)
		}
	}
}

func TestIsHeader(t *testing.T) {
	if !header().IsHeader() || msg("a").IsHeader() || (pocat.Entry{Context: "x"}).IsHeader() {
		t.Fatal("wrong")
	}
}

func TestLookup(t *testing.T) {
	c := catalog("de", pocat.Entry{ID: "a", Context: "x"}, pocat.Entry{ID: "b", Obsolete: true})
	e, ok := c.Lookup("x", "a")
	if !ok {
		t.Fatal("not found")
	}
	e.Fuzzy = true
	if !c.Entries[0].Fuzzy {
		t.Fatal("Lookup did not point into the catalog")
	}
	if _, ok := c.Lookup("", "a"); ok {
		t.Fatal("found in the wrong context")
	}
	if _, ok := c.Lookup("", "b"); ok {
		t.Fatal("found an obsolete entry")
	}
}

func TestLiveLeavesOutObsolete(t *testing.T) {
	got := catalog("de", msg("a"), pocat.Entry{ID: "b", Obsolete: true}, msg("c")).Live()
	if !slices.Equal(ids(got), []string{"a", "c"}) {
		t.Fatalf("Live = %v", ids(got))
	}
}

func TestLiveLeavesOutHeader(t *testing.T) {
	if got := catalog("de", header(), msg("a")).Live(); !slices.Equal(ids(got), []string{"a"}) {
		t.Fatalf("Live = %q", ids(got))
	}
}

func TestFilledForms(t *testing.T) {
	if got := msg("a", "x", "", "y").FilledForms(); got != 2 {
		t.Fatalf("FilledForms = %d", got)
	}
}

func TestSimilarity(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want float64
	}{{"", "", 1}, {"abc", "abc", 1}, {"abc", "", 0}, {"kitten", "sitting", 1 - 3.0/7}, {"naïve", "naive", 0.8}} {
		if got := pocat.Similarity(c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Similarity(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestPluralCount(t *testing.T) {
	for lang, want := range map[string]int{"ja": 1, "de": 2, "ru": 3, "ar": 6, "zh_TW": 1, "pt_BR": 2, "pl_PL": 3, "xx": 2} {
		if got := pocat.PluralCount(lang); got != want {
			t.Errorf("PluralCount(%s) = %d", lang, got)
		}
	}
}

func TestTranslatedMeansFinished(t *testing.T) {
	if !msg("a", "x").Translated() {
		t.Error("a plain translation is not translated")
	}
	if msg("a", "").Translated() || msg("a").Translated() {
		t.Error("an empty translation is translated")
	}
	if (pocat.Entry{ID: "a", Translations: []string{"x"}, Fuzzy: true}).Translated() {
		t.Error("a fuzzy message is translated")
	}
	if (pocat.Entry{ID: "a", Translations: []string{"x"}, Obsolete: true}).Translated() {
		t.Error("an obsolete message is translated")
	}
}

func TestTranslatedNeedsEveryForm(t *testing.T) {
	if msg("a", "x", "").Translated() {
		t.Error("a message with an empty form is translated")
	}
}

func TestPlaceholdersInOrder(t *testing.T) {
	got := pocat.Placeholders("%s of %(count)d, {name} at 100%% and %f")
	if !slices.Equal(got, []string{"%s", "%(count)d", "{name}", "%f"}) {
		t.Fatalf("Placeholders = %q", got)
	}
	if got := pocat.Placeholders("100%% done"); len(got) != 0 {
		t.Fatalf("Placeholders = %q", got)
	}
}

func TestMergeFollowsTemplate(t *testing.T) {
	cat := catalog("de", msg("a", "A"), msg("b", "B"))
	got := pocat.Merge(cat, catalog("", msg("b", ""), msg("new", ""), msg("a", "")))
	if !slices.Equal(ids(got.Entries), []string{"b", "new", "a"}) || got.Lang != "de" {
		t.Fatalf("Merge = %+v", got)
	}
	if got.Entries[0].Translations[0] != "B" || got.Entries[1].Translations[0] != "" || got.Entries[1].Fuzzy {
		t.Fatalf("Merge = %+v", got)
	}
}

func TestMergeRefsComeFromTemplate(t *testing.T) {
	old := msg("a", "A")
	old.Refs = []string{"old.c"}
	tmpl := msg("a", "")
	tmpl.Refs = []string{"new.c"}
	got := pocat.Merge(catalog("de", old), catalog("", tmpl))
	if !slices.Equal(got.Entries[0].Refs, []string{"new.c"}) {
		t.Fatalf("Refs = %v", got.Entries[0].Refs)
	}
}

func TestMergeKeepsTranslatorComments(t *testing.T) {
	old := msg("a", "A")
	old.Comments = []string{"from the glossary"}
	got := pocat.Merge(catalog("de", old), catalog("", msg("a", "")))
	if !slices.Equal(got.Entries[0].Comments, []string{"from the glossary"}) {
		t.Fatalf("Comments = %v", got.Entries[0].Comments)
	}
}

func TestMergeFuzzyStaysFuzzy(t *testing.T) {
	old := msg("a", "A")
	old.Fuzzy = true
	if got := pocat.Merge(catalog("de", old), catalog("", msg("a", ""))); !got.Entries[0].Fuzzy {
		t.Fatalf("Merge = %+v", got.Entries[0])
	}
}

func TestMergeGoneBecomesObsolete(t *testing.T) {
	got := pocat.Merge(catalog("de", msg("gone", "weg")), catalog("", msg("zzzzzzzzzzzz", "")))
	last := got.Entries[len(got.Entries)-1]
	if last.ID != "gone" || !last.Obsolete || last.Translations[0] != "weg" {
		t.Fatalf("Merge = %+v", got.Entries)
	}
}

func TestMergeDropsNeverTranslated(t *testing.T) {
	got := pocat.Merge(catalog("de", msg("gone", "")), catalog("", msg("zzzzzzzzzzzz", "")))
	if !slices.Equal(ids(got.Entries), []string{"zzzzzzzzzzzz"}) {
		t.Fatalf("Merge = %q", ids(got.Entries))
	}
}

func TestMergeHeaderStaysFirst(t *testing.T) {
	got := pocat.Merge(catalog("de", msg("a", "A"), header()), catalog("", msg("a", "")))
	if len(got.Entries) != 2 || !got.Entries[0].IsHeader() || got.Entries[0].Obsolete || got.Entries[0].Translations[0] != "Language: de\n" {
		t.Fatalf("Merge = %+v", got.Entries)
	}
}

func TestMergeNewMessageForms(t *testing.T) {
	tmpl := pocat.Entry{ID: "one file", Plural: "%d files"}
	got := pocat.Merge(catalog("ru"), catalog("", tmpl, msg("plain")))
	if len(got.Entries[0].Translations) != 3 || len(got.Entries[1].Translations) != 1 {
		t.Fatalf("Merge = %+v", got.Entries)
	}
}

func TestMergeOffersSimilar(t *testing.T) {
	got := pocat.Merge(catalog("de", msg("abcdefghij", "alt")), catalog("", msg("abcdefgxyz", "")))
	if len(got.Entries) != 1 || got.Entries[0].Translations[0] != "alt" || !got.Entries[0].Fuzzy {
		t.Fatalf("Merge = %+v", got.Entries)
	}
}

func TestMergeDoesNotOfferDissimilar(t *testing.T) {
	got := pocat.Merge(catalog("de", msg("abcdefghij", "alt")), catalog("", msg("abcdefwxyz", "")))
	if got.Entries[0].Translations[0] != "" || !got.Entries[1].Obsolete {
		t.Fatalf("Merge = %+v", got.Entries)
	}
}

func TestMergeOfferRules(t *testing.T) {
	got := pocat.Merge(catalog("de", msg("Save the file", "one"), msg("Save a file", "two")),
		catalog("", msg("Save a file", ""), msg("Save this file", ""), msg("Save that file", "")))
	if got.Entries[0].Translations[0] != "two" || got.Entries[0].Fuzzy {
		t.Fatalf("the exact match lost out: %+v", got.Entries[0])
	}
	if got.Entries[1].Translations[0] != "one" || !got.Entries[1].Fuzzy {
		t.Fatalf("the closest message was not offered: %+v", got.Entries[1])
	}
	if got.Entries[2].Translations[0] != "" {
		t.Fatalf("a message was offered twice: %+v", got.Entries[2])
	}
}

func TestMergeOfferSameContext(t *testing.T) {
	old := pocat.Entry{Context: "menu", ID: "Save the file", Translations: []string{"one"}}
	got := pocat.Merge(catalog("de", old), catalog("", msg("Save this file", "")))
	if got.Entries[0].Translations[0] != "" {
		t.Fatalf("offered across contexts: %+v", got.Entries[0])
	}
}

func TestWriteLayout(t *testing.T) {
	e := msg("Hi", "Hallo")
	e.Comments, e.Refs, e.Fuzzy, e.Context = []string{"note"}, []string{"a.c"}, true, "greeting"
	got := pocat.Write(catalog("de", e))
	want := "# note\n#: a.c\n#, fuzzy\nmsgctxt \"greeting\"\nmsgid \"Hi\"\nmsgstr \"Hallo\"\n"
	if strings.TrimRight(got, "\n") != strings.TrimRight(want, "\n") {
		t.Fatalf("Write = %q", got)
	}
}

func TestWriteHeaderFirst(t *testing.T) {
	got := pocat.Write(catalog("de", msg("a", "A"), header()))
	if !strings.HasPrefix(got, "msgid \"\"\n") {
		t.Fatalf("Write = %q", got)
	}
}

func TestWriteCleansReferences(t *testing.T) {
	e := msg("a", "A")
	e.Refs = []string{"src\\b.c:20", "src/a.c:7", "src/b.c:90"}
	got := pocat.Write(catalog("de", e))
	if !strings.HasPrefix(got, "#: src/a.c\n#: src/b.c\nmsgid") {
		t.Fatalf("Write = %q", got)
	}
}

func TestWriteMultiLine(t *testing.T) {
	got := pocat.Write(catalog("de", msg("a\nb\n", "x\ny")))
	want := "msgid \"\"\n\"a\\n\"\n\"b\\n\"\nmsgstr \"\"\n\"x\\n\"\n\"y\"\n"
	if strings.TrimRight(got, "\n") != strings.TrimRight(want, "\n") {
		t.Fatalf("Write = %q", got)
	}
	if one := pocat.Write(catalog("de", msg("a\n", "b"))); !strings.HasPrefix(one, "msgid \"a\\n\"\nmsgstr \"b\"") {
		t.Fatalf("Write = %q", one)
	}
}

func TestWriteObsoleteLast(t *testing.T) {
	got := pocat.Write(catalog("de", pocat.Entry{ID: "old", Translations: []string{"alt"}, Obsolete: true}, msg("a", "A")))
	if !strings.HasPrefix(got, "msgid \"a\"") || !strings.Contains(got, "#~ msgid \"old\"\n#~ msgstr \"alt\"") {
		t.Fatalf("Write = %q", got)
	}
}

func TestWriteParsesBack(t *testing.T) {
	in := catalog("de", header(),
		pocat.Entry{Context: "m", ID: "a\nb", Plural: "c", Translations: []string{"x\n", "y"}, Comments: []string{"n"}, Fuzzy: true},
		pocat.Entry{ID: "old", Translations: []string{"alt"}, Obsolete: true})
	got, err := pocat.Parse(pocat.Write(in))
	if err != nil || len(got.Entries) != 3 {
		t.Fatalf("Parse = %+v, %v", got, err)
	}
	e := got.Entries[1]
	if e.Context != "m" || e.ID != "a\nb" || e.Plural != "c" || !slices.Equal(e.Translations, []string{"x\n", "y"}) || !e.Fuzzy {
		t.Fatalf("entry = %+v", e)
	}
	if !got.Entries[2].Obsolete || got.Entries[2].ID != "old" {
		t.Fatalf("entry = %+v", got.Entries[2])
	}
}

func TestStatsBuckets(t *testing.T) {
	got := catalog("de", msg("a", "A"), msg("b", ""), pocat.Entry{ID: "c", Translations: []string{"C"}, Fuzzy: true},
		pocat.Entry{ID: "d", Translations: []string{"D"}, Obsolete: true}).Stats()
	if got != (pocat.Stats{Translated: 1, Fuzzy: 1, Untranslated: 1, Obsolete: 1}) {
		t.Fatalf("Stats = %+v", got)
	}
}

func TestStatsLeavesOutHeader(t *testing.T) {
	if got := catalog("de", header(), msg("a", "A")).Stats(); got != (pocat.Stats{Translated: 1}) {
		t.Fatalf("Stats = %+v", got)
	}
}

func TestTotalLeavesOutObsolete(t *testing.T) {
	if got := (pocat.Stats{Translated: 1, Fuzzy: 2, Untranslated: 3, Obsolete: 4}).Total(); got != 6 {
		t.Fatalf("Total = %d", got)
	}
}

func TestPercentPlain(t *testing.T) {
	if got := (pocat.Stats{Translated: 3, Fuzzy: 1}).Percent(); got != 75 {
		t.Fatalf("Percent = %d", got)
	}
	if got := (pocat.Stats{Obsolete: 3}).Percent(); got != 0 {
		t.Fatalf("Percent with nothing = %d", got)
	}
}

func TestPercentNeverRoundsUp(t *testing.T) {
	if got := (pocat.Stats{Translated: 199, Untranslated: 1}).Percent(); got != 99 {
		t.Fatalf("Percent = %d", got)
	}
	if got := (pocat.Stats{Translated: 2, Untranslated: 1}).Percent(); got != 66 {
		t.Fatalf("Percent = %d", got)
	}
}

func TestSetTranslation(t *testing.T) {
	c := catalog("de", pocat.Entry{ID: "a", Translations: []string{"x"}, Fuzzy: true})
	if !c.SetTranslation(c.Entries[0].Key(), "y") {
		t.Fatal("not found")
	}
	if e := c.Entries[0]; e.Fuzzy || e.Translations[0] != "y" {
		t.Fatalf("entry = %+v", e)
	}
	if c.SetTranslation("nothing", "y") {
		t.Fatal("found nothing")
	}
}

func TestCheckPlaceholderLost(t *testing.T) {
	got := pocat.Check(catalog("de", msg("%s saved", "gespeichert"), msg("%s ok", "%s gut")))
	if !slices.Equal(codes(got), []string{"placeholder"}) || got[0].Key != msg("%s saved").Key() {
		t.Fatalf("Check = %v", got)
	}
}

func TestCheckPositionalOrder(t *testing.T) {
	got := pocat.Check(catalog("de", msg("%s of %d", "%d von %s")))
	if !slices.Equal(codes(got), []string{"placeholder"}) {
		t.Fatalf("Check = %v", got)
	}
}

func TestCheckNamedMayMove(t *testing.T) {
	got := pocat.Check(catalog("de", msg("{a} and {b}", "{b} und {a}"), msg("%(x)s by %(y)d", "%(y)d von %(x)s")))
	if len(got) != 0 {
		t.Fatalf("Check = %v", got)
	}
}

func TestCheckNotFinishedDrafts(t *testing.T) {
	got := pocat.Check(catalog("de", pocat.Entry{ID: "%s saved", Translations: []string{"gespeichert"}, Fuzzy: true}))
	if len(got) != 0 {
		t.Fatalf("Check = %v", got)
	}
}

func TestCheckLeavesOutHeader(t *testing.T) {
	if got := pocat.Check(catalog("de", msg("", "%s\n"))); len(got) != 0 {
		t.Fatalf("Check = %v", got)
	}
}

func TestCheckPluralLastForm(t *testing.T) {
	ok := pocat.Entry{ID: "One file", Plural: "%d files", Translations: []string{"Eine Datei", "%d Dateien"}}
	bad := pocat.Entry{ID: "One page", Plural: "%d pages", Translations: []string{"%d Seite", "Seiten"}}
	if got := pocat.Check(catalog("de", ok)); len(got) != 0 {
		t.Fatalf("Check = %v", got)
	}
	if got := pocat.Check(catalog("de", bad)); !slices.Equal(codes(got), []string{"placeholder"}) {
		t.Fatalf("Check = %v", got)
	}
}

func TestCheckPluralCount(t *testing.T) {
	e := pocat.Entry{ID: "a", Plural: "b", Translations: []string{"x", "y"}}
	if got := pocat.Check(catalog("ru", e)); !slices.Equal(codes(got), []string{"plural-count"}) {
		t.Fatalf("ru: %v", got)
	}
	if got := pocat.Check(catalog("de_AT", e)); len(got) != 0 {
		t.Fatalf("de_AT: %v", got)
	}
}

func TestCheckNewlines(t *testing.T) {
	got := pocat.Check(catalog("de", msg("a\n", "x"), msg("\nb", "\ny"), msg("c", "z\n"), msg("d", "")))
	if !slices.Equal(codes(got), []string{"newline", "newline"}) {
		t.Fatalf("Check = %v", got)
	}
}

func TestCheckOrderWithinEntry(t *testing.T) {
	e := pocat.Entry{ID: "%d\n", Plural: "%d\n", Translations: []string{"x", "y", "z"}}
	got := pocat.Check(catalog("de", e))
	if !slices.Equal(codes(got), []string{"plural-count", "placeholder", "newline"}) {
		t.Fatalf("Check = %v", got)
	}
}

func TestFlatKeyPlain(t *testing.T) {
	if got := pocat.FlatKey(msg("Save")); got != "Save" {
		t.Fatalf("FlatKey = %q", got)
	}
}

func TestFlatKeyWithContext(t *testing.T) {
	if got := pocat.FlatKey(pocat.Entry{Context: "menu", ID: "Save"}); got != "menu::Save" {
		t.Fatalf("FlatKey = %q", got)
	}
}

func TestExportSortedLines(t *testing.T) {
	got := pocat.Export(catalog("de", msg("b", "B"), msg("a", "A"), pocat.Entry{ID: "c", Context: "z", Translations: []string{"C"}}))
	if got != "a=A\nb=B\nz::c=C\n" {
		t.Fatalf("Export = %q", got)
	}
	if got := pocat.Export(catalog("de")); got != "" {
		t.Fatalf("Export of nothing = %q", got)
	}
}

func TestExportEscapes(t *testing.T) {
	got := pocat.Export(catalog("de", msg("a=b", "x\ny\\z=1")))
	if got != "a\\=b=x\\ny\\\\z=1\n" {
		t.Fatalf("Export = %q", got)
	}
}

func TestExportPluralLines(t *testing.T) {
	got := pocat.Export(catalog("de", pocat.Entry{ID: "file", Plural: "files", Translations: []string{"Datei", "Dateien"}}))
	if got != "file[0]=Datei\nfile[1]=Dateien\n" {
		t.Fatalf("Export = %q", got)
	}
}

func TestExportOnlyFinished(t *testing.T) {
	got := pocat.Export(catalog("de", msg("a", "A"), msg("b", ""), pocat.Entry{ID: "c", Translations: []string{"C"}, Fuzzy: true},
		pocat.Entry{ID: "d", Plural: "ds", Translations: []string{"D", ""}}))
	if got != "a=A\n" {
		t.Fatalf("Export = %q", got)
	}
}

func TestExportLeavesOutHeader(t *testing.T) {
	if got := pocat.Export(catalog("de", header())); got != "" {
		t.Fatalf("Export = %q", got)
	}
}

func TestParseFlatUndoesEscapes(t *testing.T) {
	got, err := pocat.ParseFlat("a\\=b=x\\ny\\\\z=1\n\nz::c=C\n")
	if err != nil || len(got) != 2 || got["a=b"] != "x\ny\\z=1" || got["z::c"] != "C" {
		t.Fatalf("ParseFlat = %q, %v", got, err)
	}
}

func TestParseFlatBadLine(t *testing.T) {
	_, err := pocat.ParseFlat("a=1\nno separator\n")
	var se *pocat.SyntaxError
	if !errors.As(err, &se) || se.Line != 2 {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyFlatUpdates(t *testing.T) {
	c := catalog("de", msg("a", ""), pocat.Entry{ID: "s", Context: "m", Translations: []string{""}}, msg("b", "B"))
	n, err := pocat.ApplyFlat(c, "a=A\nm::s=S\nnobody=X\n")
	if err != nil || n != 2 {
		t.Fatalf("ApplyFlat = %d, %v", n, err)
	}
	if c.Entries[0].Translations[0] != "A" || c.Entries[1].Translations[0] != "S" || c.Entries[2].Translations[0] != "B" {
		t.Fatalf("entries = %+v", c.Entries)
	}
}

func TestApplyFlatPluralForms(t *testing.T) {
	c := catalog("de", pocat.Entry{ID: "file", Plural: "files", Translations: []string{"", ""}})
	n, err := pocat.ApplyFlat(c, "file[0]=Datei\nfile[1]=Dateien\n")
	if err != nil || n != 1 || !slices.Equal(c.Entries[0].Translations, []string{"Datei", "Dateien"}) {
		t.Fatalf("ApplyFlat = %d, %v, %+v", n, err, c.Entries[0])
	}
}

func TestApplyFlatFinishesDrafts(t *testing.T) {
	c := catalog("de", pocat.Entry{ID: "a", Translations: []string{"x"}, Fuzzy: true})
	if _, err := pocat.ApplyFlat(c, "a=y\n"); err != nil {
		t.Fatal(err)
	}
	if c.Entries[0].Fuzzy || !c.Entries[0].Translated() {
		t.Fatalf("entry = %+v", c.Entries[0])
	}
}

func TestSummaryLine(t *testing.T) {
	c := catalog("de", header(), msg("a", "A"), msg("b", "B"), pocat.Entry{ID: "c", Translations: []string{"C"}, Fuzzy: true},
		pocat.Entry{ID: "d", Translations: []string{"D"}, Obsolete: true})
	if got := pocat.Summary(c); got != "de: 2/3 translated (66%), 1 fuzzy, 0 untranslated, 1 obsolete" {
		t.Fatalf("Summary = %q", got)
	}
}

func TestUntranslatedList(t *testing.T) {
	c := catalog("de", msg("a", ""), msg("b", "B"), pocat.Entry{ID: "c", Translations: []string{"C"}, Fuzzy: true},
		pocat.Entry{ID: "d", Translations: []string{""}, Obsolete: true}, msg("e", "E", ""))
	if got := c.Untranslated(); !slices.Equal(ids(got), []string{"c", "a", "e"}) {
		t.Fatalf("Untranslated = %q", ids(got))
	}
}

func TestUntranslatedLeavesOutHeader(t *testing.T) {
	if got := catalog("de", msg("", "")).Untranslated(); len(got) != 0 {
		t.Fatalf("Untranslated = %+v", got)
	}
}

func TestRefFiles(t *testing.T) {
	a, b := msg("a"), msg("b")
	a.Refs = []string{"src\\z.c:3", "src/a.c:1"}
	b.Refs = []string{"src/a.c:9"}
	got := pocat.RefFiles(catalog("de", a, b, pocat.Entry{ID: "old", Refs: []string{"old.c:1"}, Obsolete: true}))
	if !slices.Equal(got, []string{"src/a.c", "src/z.c"}) {
		t.Fatalf("RefFiles = %q", got)
	}
}
`},
		Solution: map[string]string{
			"entry.go": `package pocat

// Entry is one message of a catalog.
type Entry struct {
	Context      string   // msgctxt
	ID           string   // msgid
	Plural       string   // msgid_plural
	Translations []string // msgstr, or one per plural form
	Comments     []string // translator comments, without the leading "# "
	Refs         []string // source references, without the leading "#: "
	Fuzzy        bool
	Obsolete     bool
}

// Key identifies an entry: the same text in another context is another message.
func (e Entry) Key() string { return e.Context + "\x04" + e.ID }

// HasPlural reports whether the message has plural forms.
func (e Entry) HasPlural() bool { return e.Plural != "" }

// IsHeader reports whether this is the file's header, which is not a message.
func (e Entry) IsHeader() bool { return e.ID == "" && e.Context == "" }

// FilledForms returns how many of the translations have text.
func (e Entry) FilledForms() int {
	n := 0
	for _, t := range e.Translations {
		if t != "" {
			n++
		}
	}
	return n
}

// Translated reports whether the message is finished.
func (e Entry) Translated() bool {
	if e.Fuzzy || e.Obsolete || len(e.Translations) == 0 {
		return false
	}
	return e.FilledForms() == len(e.Translations)
}

// Catalog is a whole PO file.
type Catalog struct {
	Lang    string
	Entries []Entry
}

// Find returns the index of the entry with that key, or -1.
func (c *Catalog) Find(key string) int {
	for i, e := range c.Entries {
		if e.Key() == key {
			return i
		}
	}
	return -1
}

// Lookup returns a pointer to the entry, so that a caller can change it. Obsolete entries
// are not found.
func (c *Catalog) Lookup(context, id string) (*Entry, bool) {
	i := c.Find(Entry{Context: context, ID: id}.Key())
	if i < 0 || c.Entries[i].Obsolete {
		return nil, false
	}
	return &c.Entries[i], true
}

// Live returns the messages that are not obsolete, in order.
func (c *Catalog) Live() []Entry {
	var out []Entry
	for _, e := range c.Entries {
		if !e.IsHeader() && !e.Obsolete {
			out = append(out, e)
		}
	}
	return out
}

// SetTranslation sets the translations of the entry with that key. It is no longer fuzzy.
func (c *Catalog) SetTranslation(key string, forms ...string) bool {
	i := c.Find(key)
	if i < 0 {
		return false
	}
	c.Entries[i].Translations = forms
	c.Entries[i].Fuzzy = false
	return true
}

// Untranslated returns the messages that still need work, fuzzy ones first.
func (c *Catalog) Untranslated() []Entry {
	var fuzzy, rest []Entry
	for _, e := range c.Live() {
		switch {
		case e.Translated():
		case e.Fuzzy:
			fuzzy = append(fuzzy, e)
		default:
			rest = append(rest, e)
		}
	}
	return append(fuzzy, rest...)
}
`,
			"parse.go": `package pocat

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrSyntax is the error for text that is not a valid catalog.
var ErrSyntax = errors.New("catalog syntax error")

// SyntaxError says on which line a catalog could not be read.
type SyntaxError struct {
	Line int // 1-based
	Msg  string
}

func (e *SyntaxError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// Unwrap makes a SyntaxError match ErrSyntax.
func (e *SyntaxError) Unwrap() error { return ErrSyntax }

// parser holds the entry being read.
type parser struct {
	cat   *Catalog
	cur   Entry
	open  bool
	inStr bool           // a msgstr was seen, so another keyword starts a new entry
	field func(s string) // where a continuation string goes
}

func (p *parser) flush() {
	if p.open {
		p.cat.Entries = append(p.cat.Entries, p.cur)
	}
	p.cur, p.open, p.inStr, p.field = Entry{}, false, false, nil
}

func (p *parser) comment(line string) {
	if p.inStr {
		p.flush()
	}
	switch {
	case strings.HasPrefix(line, "#,"):
		p.cur.Fuzzy = p.cur.Fuzzy || strings.Contains(line, "fuzzy")
	case strings.HasPrefix(line, "#:"):
		p.cur.Refs = append(p.cur.Refs, strings.Fields(line[2:])...)
	case strings.HasPrefix(line, "# "):
		p.cur.Comments = append(p.cur.Comments, line[2:])
	}
}

// start opens a keyword and returns where its strings go.
func (p *parser) start(word string) (func(string), error) {
	if p.inStr && !strings.HasPrefix(word, "msgstr") {
		p.flush()
	}
	p.open = true
	c := &p.cur
	switch {
	case word == "msgctxt":
		return func(s string) { c.Context += s }, nil
	case word == "msgid":
		return func(s string) { c.ID += s }, nil
	case word == "msgid_plural":
		return func(s string) { c.Plural += s }, nil
	case word == "msgstr" || strings.HasPrefix(word, "msgstr["):
		p.inStr = true
		i := len(c.Translations)
		c.Translations = append(c.Translations, "")
		return func(s string) { c.Translations[i] += s }, nil
	}
	return nil, fmt.Errorf("unknown keyword %q", word)
}

func (p *parser) line(raw string) error {
	line := strings.TrimSpace(raw)
	rest, obsolete := strings.CutPrefix(line, "#~")
	if obsolete {
		line = strings.TrimSpace(rest)
	}
	switch {
	case line == "":
		p.flush()
		return nil
	case strings.HasPrefix(line, "#"):
		p.comment(line)
		return nil
	case !strings.HasPrefix(line, "\""):
		word, text, _ := strings.Cut(line, " ")
		field, err := p.start(word)
		if err != nil {
			return err
		}
		p.field, line = field, strings.TrimSpace(text)
	}
	s, err := strconv.Unquote(line)
	if err != nil || p.field == nil {
		return fmt.Errorf("bad string %q", line)
	}
	p.field(s)
	p.cur.Obsolete = p.cur.Obsolete || obsolete
	return nil
}

// Parse reads a catalog.
func Parse(text string) (*Catalog, error) {
	p := &parser{cat: &Catalog{}}
	for i, raw := range strings.Split(text, "\n") {
		if err := p.line(raw); err != nil {
			return nil, &SyntaxError{Line: i + 1, Msg: err.Error()}
		}
	}
	p.flush()
	return p.cat, nil
}
`,
			"write.go": `package pocat

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// cleanRefs gives references the form they have in a file we write.
func cleanRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		r = strings.ReplaceAll(r, "\\", "/")
		if i := strings.LastIndexByte(r, ':'); i >= 0 {
			r = r[:i]
		}
		out = append(out, r)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// writeString writes one keyword and its text. Text that goes on after a line break takes
// one quoted line per line of text.
func writeString(b *strings.Builder, prefix, word, text string) {
	parts := strings.SplitAfter(text, "\n")
	if len(parts) == 1 || (len(parts) == 2 && parts[1] == "") {
		fmt.Fprintf(b, "%s%s %s\n", prefix, word, strconv.Quote(text))
		return
	}
	fmt.Fprintf(b, "%s%s \"\"\n", prefix, word)
	for _, part := range parts {
		if part != "" {
			fmt.Fprintf(b, "%s%s\n", prefix, strconv.Quote(part))
		}
	}
}

func writeEntry(b *strings.Builder, e Entry) {
	prefix := ""
	if e.Obsolete {
		prefix = "#~ "
	}
	for _, c := range e.Comments {
		fmt.Fprintf(b, "# %s\n", c)
	}
	for _, r := range cleanRefs(e.Refs) {
		fmt.Fprintf(b, "#: %s\n", r)
	}
	if e.Fuzzy {
		b.WriteString("#, fuzzy\n")
	}
	if e.Context != "" {
		writeString(b, prefix, "msgctxt", e.Context)
	}
	writeString(b, prefix, "msgid", e.ID)
	if e.HasPlural() {
		writeString(b, prefix, "msgid_plural", e.Plural)
	}
	for i, t := range e.Translations {
		word := "msgstr"
		if e.HasPlural() {
			word = fmt.Sprintf("msgstr[%d]", i)
		}
		writeString(b, prefix, word, t)
	}
}

// Write writes a catalog as a PO file: the header, the messages, then what is obsolete.
func Write(c *Catalog) string {
	var first, live, obsolete []Entry
	for _, e := range c.Entries {
		switch {
		case e.IsHeader():
			first = append(first, e)
		case e.Obsolete:
			obsolete = append(obsolete, e)
		default:
			live = append(live, e)
		}
	}
	var chunks []string
	for _, e := range slices.Concat(first, live, obsolete) {
		var b strings.Builder
		writeEntry(&b, e)
		chunks = append(chunks, b.String())
	}
	return strings.Join(chunks, "\n")
}

// RefFiles returns the source files that messages refer to, sorted and each once.
func RefFiles(c *Catalog) []string {
	var out []string
	for _, e := range c.Live() {
		out = append(out, cleanRefs(e.Refs)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}
`,
			"merge.go": `package pocat

// fuzzyThreshold is how similar a changed text has to be to keep its translation.
const fuzzyThreshold = 0.7

// Keys returns the keys of the catalog's entries.
func (c *Catalog) Keys() map[string]bool {
	out := make(map[string]bool, len(c.Entries))
	for _, e := range c.Entries {
		out[e.Key()] = true
	}
	return out
}

// distance returns the edit distance between a and b.
func distance(a, b []rune) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i, ra := range a {
		prev := row[0]
		row[0] = i + 1
		for j, rb := range b {
			cost := 1
			if ra == rb {
				cost = 0
			}
			prev, row[j+1] = row[j+1], min(row[j+1]+1, row[j]+1, prev+cost)
		}
	}
	return row[len(b)]
}

// Similarity returns how alike two texts are, from 0 to 1.
func Similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	longer := max(len(ra), len(rb))
	if longer == 0 {
		return 1
	}
	return 1 - float64(distance(ra, rb))/float64(longer)
}

// closest finds the unused catalog message most like t, if one is similar enough.
func closest(cat *Catalog, t Entry, used map[string]bool) (Entry, bool) {
	var best Entry
	score, found := 0.0, false
	for _, e := range cat.Entries {
		if e.IsHeader() || used[e.Key()] || e.Context != t.Context || e.FilledForms() == 0 {
			continue
		}
		if s := Similarity(e.ID, t.ID); s >= fuzzyThreshold && s > score {
			best, score, found = e, s, true
		}
	}
	return best, found
}

// mergeOne works out the entry for one template message.
func mergeOne(cat *Catalog, t Entry, used map[string]bool) Entry {
	e := t
	e.Comments, e.Fuzzy, e.Obsolete = nil, false, false
	if i := cat.Find(t.Key()); i >= 0 {
		old := cat.Entries[i]
		e.Comments, e.Translations, e.Fuzzy = old.Comments, old.Translations, old.Fuzzy
		return e
	}
	if old, ok := closest(cat, t, used); ok {
		used[old.Key()] = true
		e.Comments, e.Translations, e.Fuzzy = old.Comments, old.Translations, true
		return e
	}
	e.Translations = make([]string, 1)
	if t.HasPlural() {
		e.Translations = make([]string, PluralCount(cat.Lang))
	}
	return e
}

// Merge brings a catalog up to date with a template holding the current messages.
func Merge(cat, tmpl *Catalog) *Catalog {
	out := &Catalog{Lang: cat.Lang}
	used := map[string]bool{}
	for _, e := range cat.Entries {
		if e.IsHeader() {
			out.Entries = append(out.Entries, e)
		}
	}
	for _, t := range tmpl.Entries {
		if cat.Find(t.Key()) >= 0 {
			used[t.Key()] = true
		}
	}
	for _, t := range tmpl.Entries {
		if !t.IsHeader() {
			out.Entries = append(out.Entries, mergeOne(cat, t, used))
		}
	}
	for _, e := range cat.Entries {
		if !used[e.Key()] && !e.IsHeader() && e.FilledForms() > 0 {
			e.Obsolete = true
			out.Entries = append(out.Entries, e)
		}
	}
	return out
}
`,
			"stats.go": `package pocat

import (
	"fmt"
	"strings"
)

// pluralForms holds the languages that do not have 2 plural forms.
var pluralForms = map[string]int{
	"ja": 1, "ko": 1, "vi": 1, "zh": 1,
	"cs": 3, "pl": 3, "ru": 3, "uk": 3,
	"ar": 6,
}

// PluralCount returns the number of plural forms of a language.
func PluralCount(lang string) int {
	base, _, _ := strings.Cut(lang, "_")
	if n, ok := pluralForms[base]; ok {
		return n
	}
	return 2
}

// CountEntries returns how many entries the catalog has of any kind.
func (c *Catalog) CountEntries() int { return len(c.Entries) }

// Stats counts the messages of a catalog by state.
type Stats struct {
	Translated, Fuzzy, Untranslated, Obsolete int
}

// Stats works out the numbers for the catalog.
func (c *Catalog) Stats() Stats {
	var s Stats
	for _, e := range c.Entries {
		if e.IsHeader() {
			continue
		}
		switch {
		case e.Obsolete:
			s.Obsolete++
		case e.Fuzzy:
			s.Fuzzy++
		case e.Translated():
			s.Translated++
		default:
			s.Untranslated++
		}
	}
	return s
}

// Total returns how many messages progress is measured over.
func (s Stats) Total() int { return s.Translated + s.Fuzzy + s.Untranslated }

// Percent returns the share translated as a whole number. Only all of them gives the top.
func (s Stats) Percent() int {
	total := s.Total()
	if total == 0 {
		return 0
	}
	return s.Translated * 100 / total
}

// Summary describes a catalog's progress on one line.
func Summary(c *Catalog) string {
	s := c.Stats()
	return fmt.Sprintf("%s: %d/%d translated (%d%%), %d fuzzy, %d untranslated, %d obsolete",
		c.Lang, s.Translated, s.Total(), s.Percent(), s.Fuzzy, s.Untranslated, s.Obsolete)
}
`,
			"check.go": `package pocat

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Problem is one thing a reviewer should look at. Key is the Key of the entry.
type Problem struct {
	Key, Code string
}

// String writes the problem for a report.
func (p Problem) String() string { return fmt.Sprintf("%s: %q", p.Code, p.Key) }

var placeholder = regexp.MustCompile("%%|%\\([a-z_]+\\)[sdf]|%[sdf]|\\{[a-z_]+\\}")

// Placeholders returns the placeholders of s in the order they appear, as written.
func Placeholders(s string) []string {
	var out []string
	for _, p := range placeholder.FindAllString(s, -1) {
		if p != "%%" {
			out = append(out, p)
		}
	}
	return out
}

// placeholdersFit compares a translation with its source text. Positional placeholders
// must stay in order, and named ones may move.
func placeholdersFit(source, translation string) bool {
	split := func(s string) (positional, named []string) {
		for _, p := range Placeholders(s) {
			if len(p) == 2 {
				positional = append(positional, p)
			} else {
				named = append(named, p)
			}
		}
		slices.Sort(named)
		return positional, named
	}
	p1, n1 := split(source)
	p2, n2 := split(translation)
	return slices.Equal(p1, p2) && slices.Equal(n1, n2)
}

// placeholderProblem compares the form that has to carry all the placeholders.
func placeholderProblem(e Entry) bool {
	if len(e.Translations) == 0 {
		return false
	}
	last, source := e.Translations[len(e.Translations)-1], e.ID
	if e.HasPlural() {
		source = e.Plural
	}
	return last != "" && !placeholdersFit(source, last)
}

// newlineProblem reports a form that starts or ends differently from the source text.
func newlineProblem(e Entry) bool {
	for _, t := range e.Translations {
		if t != "" && (strings.HasPrefix(t, "\n") != strings.HasPrefix(e.ID, "\n") ||
			strings.HasSuffix(t, "\n") != strings.HasSuffix(e.ID, "\n")) {
			return true
		}
	}
	return false
}

// Check returns what a reviewer should look at in a catalog.
func Check(c *Catalog) []Problem {
	var out []Problem
	for _, e := range c.Entries {
		if e.IsHeader() || e.Obsolete || e.Fuzzy {
			continue
		}
		if e.HasPlural() && len(e.Translations) != PluralCount(c.Lang) {
			out = append(out, Problem{Key: e.Key(), Code: "plural-count"})
		}
		if placeholderProblem(e) {
			out = append(out, Problem{Key: e.Key(), Code: "placeholder"})
		}
		if newlineProblem(e) {
			out = append(out, Problem{Key: e.Key(), Code: "newline"})
		}
	}
	return out
}
`,
			"export.go": `package pocat

import (
	"fmt"
	"sort"
	"strings"
)

// flatSeparator joins context and ID in the web app's key.
const flatSeparator = "::"

// splitLines cuts a file into lines, leaving out blank ones.
func splitLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// FlatKey returns the web app's key for an entry.
func FlatKey(e Entry) string {
	if e.Context == "" {
		return e.ID
	}
	return e.Context + flatSeparator + e.ID
}

var (
	valueEscaper   = strings.NewReplacer("\\", "\\\\", "\n", "\\n")
	valueUnescaper = strings.NewReplacer("\\\\", "\\", "\\n", "\n")
)

// flatLine is one line of the web app's file.
type flatLine struct{ key, value string }

// Export writes the flat file the web app loads for this catalog.
func Export(c *Catalog) string {
	var lines []flatLine
	for _, e := range c.Entries {
		if e.IsHeader() || !e.Translated() {
			continue
		}
		key := strings.ReplaceAll(FlatKey(e), "=", "\\=")
		if !e.HasPlural() {
			lines = append(lines, flatLine{key, e.Translations[0]})
			continue
		}
		for i, form := range e.Translations {
			lines = append(lines, flatLine{fmt.Sprintf("%s[%d]", key, i), form})
		}
	}
	sort.SliceStable(lines, func(a, b int) bool { return lines[a].key < lines[b].key })
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.key + "=" + valueEscaper.Replace(l.value) + "\n")
	}
	return b.String()
}

// splitFlat cuts a line at the first equals sign that is not escaped.
func splitFlat(line string) (key, value string, ok bool) {
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++
		} else if line[i] == '=' {
			return strings.ReplaceAll(line[:i], "\\=", "="), valueUnescaper.Replace(line[i+1:]), true
		}
	}
	return "", "", false
}

// ParseFlat reads a flat file back into a map from key to value.
func ParseFlat(text string) (map[string]string, error) {
	out := map[string]string{}
	for i, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := splitFlat(line)
		if !ok {
			return nil, &SyntaxError{Line: i + 1, Msg: "no separator"}
		}
		out[key] = value
	}
	return out, nil
}

// flatForms collects an entry's forms from a flat file, or nil if it has none there.
func flatForms(e Entry, flat map[string]string) []string {
	key := FlatKey(e)
	if !e.HasPlural() {
		if value, ok := flat[key]; ok {
			return []string{value}
		}
		return nil
	}
	var forms []string
	for i := 0; ; i++ {
		value, ok := flat[fmt.Sprintf("%s[%d]", key, i)]
		if !ok {
			return forms
		}
		forms = append(forms, value)
	}
}

// ApplyFlat takes translations back from a flat file and returns how many entries changed.
func ApplyFlat(c *Catalog, text string) (int, error) {
	flat, err := ParseFlat(text)
	if err != nil {
		return 0, err
	}
	applied := 0
	for i := range c.Entries {
		e := &c.Entries[i]
		forms := flatForms(*e, flat)
		if e.IsHeader() || forms == nil {
			continue
		}
		c.SetTranslation(e.Key(), forms...)
		applied++
	}
	return applied, nil
}
`,
		},
		Turns: script(
			cloud("Read docs/workflow.txt, which is how our translators work, and tell me what it means for this tool. No code changes."),
			cloud("`Parse` should say where it failed. Add `type SyntaxError struct { Line int; Msg string }`, used as a pointer, with Line 1-based and matching ErrSyntax under errors.Is. Every failure of Parse returns one: an unknown keyword, a string it can't unquote, or a continuation string with nothing open. Keep accepting what it accepts now."),
			cloud("The entry with an empty ID and no context isn't a message: it's the file's header, with metadata in its translation. Anything that counts, checks, matches or exports messages has to skip it, and when we write a file it always comes first, whatever order the entries are in. Add `func (e Entry) IsHeader() bool`."),
			local("Add `func (c *Catalog) Lookup(context, id string) (*Entry, bool)` to entry.go. It returns a pointer into c.Entries, so that a caller can change the entry. Obsolete entries aren't found."),
			local("Reference comments churn our diffs, so let's agree this now: in any file we write, a reference has its line number cut off and backslashes turned into slashes, and an entry's references are sorted with duplicates removed. We aren't touching Write today, so just keep it in mind. Add `func (c *Catalog) Live() []Entry` to entry.go: the messages that aren't obsolete, in order."),
			local("Read docs/plural-rules.txt and tell me what it says. No code changes."),
			cloud("A complaint to deal with when we get to progress numbers: managers saw 99.6 shown as 100, took it to mean done, and shipped. A percentage in this tool rounds down, so that 100 only ever means everything is finished. No stats code yet. For now add `func (e Entry) FilledForms() int` to entry.go: how many of its translations have text."),
			cloud("For later: the web app will look up a message by a key, which is its ID, or `context|id` when it has a context. Nothing to code for that yet. Add `func Similarity(a, b string) float64` to merge.go: 1 minus the Levenshtein edit distance divided by the length of the longer string, counting characters rather than encoded length. Two empty strings give 1."),
			local("Add `func PluralCount(lang string) int` to stats.go, the number of plural forms of a language."),
			local("Add `func (e Entry) Translated() bool` to entry.go, using our translators' definition."),
			local("Placeholders: our strings use %s, %d and %f, named ones like %(count)d, and {name}. %% is a literal percent sign, not a placeholder. Translations may reorder the named kinds, %(x)d and {x}, but positional ones must stay in the same order. Add `func Placeholders(s string) []string` to check.go: the placeholders of s in the order they appear, each as written. Names are lower case letters and underscores."),
			cloud("Write `func Merge(cat, tmpl *Catalog) *Catalog` in merge.go, bringing a catalog up to date with a template that holds the current messages. The result has the catalog's Lang and the template's messages in the template's order, each with what the catalog has for it, by the rules our translators work to. Messages new in the template get empty translations, with the right number of forms. Exact keys only for now; we'll handle changed source text next."),
			cloud("Rewrite `Write` in write.go. For an entry: comments, then references, one per line, then the fuzzy flag, then its strings in the usual order, with plural forms as msgstr[0] and so on. Text that goes on after a line break takes multi-line form: `msgid \"\"`, then one quoted line per line of text, each keeping its \\n. Obsolete entries go at the end of the file with `#~ ` before each string line. One blank line between entries, and Parse has to read it all back."),
			cloud("Now let `Merge` handle changed source text. A template message with no exact match takes the translation of the most similar catalog message, if that's similar enough by our translators' rule, and comes in fuzzy. Only catalog messages in the same context, with a translation and with no exact match of their own qualify. Each can be used once, the first wins a tie, and one used this way doesn't also go on to become obsolete."),
			local("Add `type Stats struct { Translated, Fuzzy, Untranslated, Obsolete int }` and `func (c *Catalog) Stats() Stats` to stats.go. A message falls in exactly one of these: obsolete if it's obsolete, otherwise fuzzy if it's fuzzy."),
			local("Add `func (s Stats) Total() int` and `func (s Stats) Percent() int` to stats.go: how many messages progress is measured over, and the share of them translated as a whole number, or 0 if there are none."),
			local("Add `func (c *Catalog) SetTranslation(key string, forms ...string) bool` to entry.go: it sets the translations of the entry with that Key and returns whether there was one. Someone has now looked at that entry, so it's no longer fuzzy."),
			cloud("Read docs/export-format.txt, which is what the web app loads, and give me the main points. No code changes."),
			cloud("Add `func Check(c *Catalog) []Problem` to check.go. One code for now, \"placeholder\": a translation whose placeholders don't fit its source text. An entry without a plural compares its translation with ID. An entry with a plural compares only its last form with Plural, since earlier forms may leave the number out. Empty forms aren't compared. Check only what reviewers check."),
			cloud("Extend `Check` with \"plural-count\", for an entry with a plural whose number of forms is wrong for the catalog's Lang, and \"newline\", for a non-empty form that doesn't start and end with line breaks the way ID does. An entry gets each code at most once, in the order plural-count, placeholder, newline."),
			local("Add `func FlatKey(e Entry) string` to export.go: the web app's key for an entry."),
			local("Add `func Export(c *Catalog) string` to export.go: the file the web app loads for this catalog."),
			cloud("The web team has reserved the old separator for their routing, so from now on the web app's key joins context and ID with `::` instead. Update the code."),
			local("Add `func ParseFlat(text string) (map[string]string, error)` to export.go: it reads such a file back into a map from key to value, with its escapes undone and a plural's index left in the key. Blank lines are skipped. A line without a separator gives a *SyntaxError with that line's number."),
			cloud("Add `func ApplyFlat(c *Catalog, text string) (int, error)` to export.go: it takes translations back from such a file for the entries whose key is in it, and returns how many entries it updated. This is how translators' edits in the web app come home, so treat them like any other edit. Keys that match nothing are ignored."),
			local("Add `func Summary(c *Catalog) string` to stats.go: one line, exactly like this: de: 3/4 translated (75%), 1 fuzzy, 0 untranslated, 2 obsolete"),
			cloud("Add `func (c *Catalog) Untranslated() []Entry` to entry.go, the translators' to-do list: the messages that aren't finished yet, the fuzzy ones first and then the rest, each group in catalog order."),
			cloud("Add `func RefFiles(c *Catalog) []string` to write.go: the source files that non-obsolete messages refer to, in the form they take in a file we write, sorted, each once."),
		),
		Carries: []Carry{
			{
				What:        "what counts as translated, and what merging keeps, offers and makes obsolete (docs/workflow.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{9, 11, 13, 14, 15, 18, 21},
				Source:      "docs/workflow.txt",
				Tests: []string{
					"TestTranslatedMeansFinished", "TestTranslatedNeedsEveryForm", "TestMergeRefsComeFromTemplate", "TestMergeKeepsTranslatorComments",
					"TestMergeFuzzyStaysFuzzy", "TestMergeGoneBecomesObsolete", "TestMergeDropsNeverTranslated", "TestMergeOffersSimilar",
					"TestMergeDoesNotOfferDissimilar", "TestTotalLeavesOutObsolete", "TestCheckNotFinishedDrafts", "TestExportOnlyFinished", "TestUntranslatedList",
					"TestSummaryLine",
				},
				Absent: []string{"workflow", "70%", "0.7", "seventy"},
			},
			{
				What:        "the entry with an empty ID is a header: first in a file, skipped by anything that handles messages",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{4, 11, 12, 14, 18, 21},
				Tests: []string{
					"TestLiveLeavesOutHeader", "TestMergeHeaderStaysFirst", "TestWriteHeaderFirst", "TestStatsLeavesOutHeader",
					"TestCheckLeavesOutHeader", "TestExportLeavesOutHeader", "TestUntranslatedLeavesOutHeader", "TestSummaryLine",
				},
				Absent: []string{"header", "metadata"},
			},
			{
				What:        "references are written without line numbers, with slashes, sorted and without duplicates",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{12, 27},
				Tests:       []string{"TestWriteCleansReferences", "TestRefFiles"},
				Absent:      []string{"churn", "line number", "backslash", "duplicates"},
			},
			{
				What:        "plural forms per language (docs/plural-rules.txt)",
				Kind:        CarryDiscovered,
				Established: 5,
				Needed:      []int{8, 11, 19},
				Source:      "docs/plural-rules.txt",
				Tests:       []string{"TestPluralCount", "TestMergeNewMessageForms", "TestCheckPluralCount"},
				Absent:      []string{"plural-rules", "nplurals", "zh_tw", "pt_br"},
			},
			{
				What:        "percentages round down, so 100 means everything is finished",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{15, 25},
				Tests:       []string{"TestPercentNeverRoundsUp", "TestSummaryLine"},
				Absent:      []string{"rounds down", "round down", "99.6", "100", "managers", "floor"},
			},
			{
				What:        "the web app's key is the ID, or the context before the ID when there is a context",
				Kind:        CarryUser,
				Established: 7,
				Needed:      []int{20, 21},
				Tests: []string{
					"TestFlatKeyPlain", "TestFlatKeyWithContext", "TestExportSortedLines", "TestExportEscapes", "TestExportPluralLines",
					"TestExportOnlyFinished", "TestApplyFlatUpdates", "TestApplyFlatPluralForms", "TestApplyFlatFinishesDrafts",
				},
				Absent: []string{"context|id", "pipe"},
			},
			{
				What:        "named placeholders may move and positional ones may not",
				Kind:        CarryUser,
				Established: 10,
				Needed:      []int{18},
				Tests:       []string{"TestCheckPositionalOrder", "TestCheckNamedMayMove"},
				Absent:      []string{"reorder", "same order", "positional", "%(x)"},
			},
			{
				What:        "setting a translation ends its being fuzzy",
				Kind:        CarryEmbodied,
				Established: 16,
				Needed:      []int{24},
				Tests:       []string{"TestApplyFlatFinishesDrafts"},
				Absent:      []string{"no longer fuzzy", "settranslation"},
			},
			{
				What:        "the web app's file: finished messages, sorted keys, escapes and plural indexes (docs/export-format.txt)",
				Kind:        CarryDiscovered,
				Established: 17,
				Needed:      []int{21, 23, 24},
				Source:      "docs/export-format.txt",
				Tests:       []string{"TestExportSortedLines", "TestExportEscapes", "TestExportPluralLines", "TestApplyFlatPluralForms", "TestParseFlatUndoesEscapes"},
				Absent:      []string{"export-format", "sorted by key", "[0]", "\\="},
			},
			{
				What:        "the web app's key joins context and ID with ::, revised from |",
				Kind:        CarryUser,
				Established: 22,
				Needed:      []int{23, 25},
				Tests:       []string{"TestFlatKeyWithContext", "TestExportSortedLines", "TestApplyFlatUpdates"},
				Absent:      []string{"::", "double colon", "routing", "reserved"},
			},
		},
	}
}
