package s2

func accC10() Task {
	return Task{
		ID:     "s2-c10-text-tokenizer",
		Set:    "acceptance",
		System: system("a text tokenizer"),
		Files: map[string]string{
			"go.mod": goMod,
			"token.go": `package tokenize

// Token is one word found in a text. Start is where it begins in that text.
type Token struct {
	Text  string
	Start int
}

// Empty reports whether the token has no text.
func (t Token) Empty() bool { return t.Text == "" }
`,
			"docs/corpus_notes.txt": `Corpus notes

Stop words carry no signal: the, a, an, and, of, to, in. They stay in the token
stream, but frequency tables and rankings leave them out entirely.

The corpus is refreshed quarterly. Licensing questions go to the library desk.
`,
			"token_test.go": `package tokenize

import "testing"

func TestEmpty(t *testing.T) {
	if (Token{Text: "cat", Start: 4}).Empty() || !(Token{}).Empty() {
		t.Fatal("Empty is wrong")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package tokenize_test

import (
	"reflect"
	"testing"

	tokenize "fixture"
)

func words(text string) []string {
	var out []string
	for _, tok := range tokenize.Tokenize(text) {
		out = append(out, tok.Text)
	}
	return out
}

func TestLines(t *testing.T) {
	if got := tokenize.Lines("a b\nc"); !reflect.DeepEqual(got, []string{"a b", "c"}) {
		t.Fatalf("Lines = %q", got)
	}
}

func TestBlank(t *testing.T) {
	if !tokenize.Blank(" ,.-\n") || tokenize.Blank(" 7 ") || tokenize.Blank("a") {
		t.Fatal("Blank is wrong")
	}
}

func TestTokenizeWordsAndStarts(t *testing.T) {
	got := tokenize.Tokenize("the cat, 42 dogs!")
	want := []tokenize.Token{{Text: "the", Start: 0}, {Text: "cat", Start: 4}, {Text: "42", Start: 9}, {Text: "dogs", Start: 12}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokenize = %+v", got)
	}
}

func TestTokenizeAccentedWordIsOneWord(t *testing.T) {
	if got := words("Café Zoë"); !reflect.DeepEqual(got, []string{"Café", "Zoë"}) {
		t.Fatalf("words = %q", got)
	}
}

func TestTokenizeStartsCountCharacters(t *testing.T) {
	got := tokenize.Tokenize("café au lait")
	if len(got) != 3 || got[1].Start != 5 || got[2].Start != 8 {
		t.Fatalf("Tokenize = %+v", got)
	}
}

func TestTokenizeApostropheInside(t *testing.T) {
	if got := words("don't stop o'clock"); !reflect.DeepEqual(got, []string{"don't", "stop", "o'clock"}) {
		t.Fatalf("words = %q", got)
	}
}

func TestTokenizeApostropheOutside(t *testing.T) {
	if got := words("'quoted' dogs' 'tis"); !reflect.DeepEqual(got, []string{"quoted", "dogs", "tis"}) {
		t.Fatalf("words = %q", got)
	}
}

func TestTokenizeHyphenSplits(t *testing.T) {
	if got := words("well-known"); !reflect.DeepEqual(got, []string{"well", "known"}) {
		t.Fatalf("words = %q", got)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"Dog": "dog", "dog's": "dog", "DOG'S": "dog", "dogs": "dogs", "don't": "don't"} {
		if got := tokenize.Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q", in, got)
		}
	}
}

func TestCountPlain(t *testing.T) {
	if got := tokenize.Count("cat dog cat", "cat"); got != 2 {
		t.Fatalf("Count = %d", got)
	}
	if got := tokenize.Count("cat dog", "bird"); got != 0 {
		t.Fatalf("Count = %d", got)
	}
}

func TestCountSameWordAnyForm(t *testing.T) {
	if got := tokenize.Count("Dog dog's DOG cat", "dog"); got != 3 {
		t.Errorf("dog: %d", got)
	}
	if got := tokenize.Count("Dog dog's DOG cat", "DOG'S"); got != 3 {
		t.Errorf("DOG'S: %d", got)
	}
}

func TestCountKeepsWordsWhole(t *testing.T) {
	if got := tokenize.Count("don't don", "don"); got != 1 {
		t.Fatalf("Count = %d", got)
	}
}

func TestFrequenciesCounts(t *testing.T) {
	got := tokenize.Frequencies("cat dog cat")
	if !reflect.DeepEqual(got, map[string]int{"cat": 2, "dog": 1}) {
		t.Fatalf("Frequencies = %v", got)
	}
}

func TestFrequenciesLeaveOutStopWords(t *testing.T) {
	got := tokenize.Frequencies("the cat sat in a hat of an and to")
	if !reflect.DeepEqual(got, map[string]int{"cat": 1, "sat": 1, "hat": 1}) {
		t.Fatalf("Frequencies = %v", got)
	}
}

func TestFrequenciesSameWordAnyForm(t *testing.T) {
	got := tokenize.Frequencies("Dog dog's DOG cat")
	dogs := 0
	for word, n := range got {
		if tokenize.Normalize(word) == "dog" {
			dogs = n
		}
	}
	if len(got) != 2 || dogs != 3 {
		t.Fatalf("Frequencies = %v", got)
	}
}

func TestTopOrder(t *testing.T) {
	got := tokenize.Top("pear fig fig apple apple pear pear", 2)
	if !reflect.DeepEqual(got, []string{"pear", "apple"}) {
		t.Fatalf("Top = %q", got)
	}
	if got := tokenize.Top("fig", 5); !reflect.DeepEqual(got, []string{"fig"}) {
		t.Fatalf("Top = %q", got)
	}
}

func TestTopLeavesOutStopWords(t *testing.T) {
	got := tokenize.Top("the the the cat cat dog", 1)
	if !reflect.DeepEqual(got, []string{"cat"}) {
		t.Fatalf("Top = %q", got)
	}
}

func TestTopSameWordAnyForm(t *testing.T) {
	got := tokenize.Top("Dog dog DOG's cat cat", 1)
	if len(got) != 1 || tokenize.Normalize(got[0]) != "dog" {
		t.Fatalf("Top = %q", got)
	}
}

func TestSnippetPlain(t *testing.T) {
	got := tokenize.Snippet("one two three", tokenize.Token{Text: "two", Start: 4}, 2)
	if got != "e two t" {
		t.Fatalf("Snippet = %q", got)
	}
}

func TestSnippetClipsToText(t *testing.T) {
	got := tokenize.Snippet("one two", tokenize.Token{Text: "one", Start: 0}, 50)
	if got != "one two" {
		t.Fatalf("Snippet = %q", got)
	}
}

func TestSnippetCountsCharacters(t *testing.T) {
	got := tokenize.Snippet("café au lait", tokenize.Token{Text: "au", Start: 5}, 2)
	if got != "é au l" {
		t.Fatalf("Snippet = %q", got)
	}
}
`},
		Solution: map[string]string{
			"token.go": `package tokenize

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var stopWords = map[string]bool{"the": true, "a": true, "an": true, "and": true, "of": true, "to": true, "in": true}

// Token is one word found in a text. Start is where it begins in that text.
type Token struct {
	Text  string
	Start int
}

// Empty reports whether the token has no text.
func (t Token) Empty() bool { return t.Text == "" }

// Lines splits a text on newlines.
func Lines(text string) []string { return strings.Split(text, "\n") }

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// Blank reports whether a text has no letters or digits.
func Blank(text string) bool { return strings.IndexFunc(text, wordRune) < 0 }

// joins reports whether the mark at i continues the word it is inside.
func joins(rs []rune, i int) bool {
	return rs[i] == '\'' && i+1 < len(rs) && unicode.IsLetter(rs[i-1]) && unicode.IsLetter(rs[i+1])
}

// Tokenize returns the words of a text in order.
func Tokenize(text string) []Token {
	rs := []rune(text)
	var out []Token
	for i := 0; i < len(rs); {
		if !wordRune(rs[i]) {
			i++
			continue
		}
		start := i
		for i < len(rs) && (wordRune(rs[i]) || joins(rs, i)) {
			i++
		}
		out = append(out, Token{Text: string(rs[start:i]), Start: start})
	}
	return out
}

// Normalize returns the form two words are compared in.
func Normalize(word string) string {
	return strings.TrimSuffix(strings.ToLower(word), "'s")
}

// Count returns how many times a word occurs in a text.
func Count(text, word string) int {
	n := 0
	for _, t := range Tokenize(text) {
		if Normalize(t.Text) == Normalize(word) {
			n++
		}
	}
	return n
}

// Frequencies returns how often each word occurs.
func Frequencies(text string) map[string]int {
	out := map[string]int{}
	for _, t := range Tokenize(text) {
		key := Normalize(t.Text)
		if !stopWords[key] {
			out[key]++
		}
	}
	return out
}

// Top returns the n most frequent words, ties in alphabetical order.
func Top(text string, n int) []string {
	freq := Frequencies(text)
	words := make([]string, 0, len(freq))
	for w := range freq {
		words = append(words, w)
	}
	sort.Slice(words, func(a, b int) bool {
		if freq[words[a]] != freq[words[b]] {
			return freq[words[a]] > freq[words[b]]
		}
		return words[a] < words[b]
	})
	return words[:min(n, len(words))]
}

// Snippet returns a token with up to around characters on each side.
func Snippet(text string, t Token, around int) string {
	rs := []rune(text)
	from := max(t.Start-around, 0)
	to := min(t.Start+utf8.RuneCountInString(t.Text)+around, len(rs))
	return string(rs[from:to])
}
`,
		},
		Turns: script(
			cloud("Something the front end relies on: positions in a text count characters, not the encoded length, since the highlighter in the browser walks them that way, so the e-acute in \"café\" is one position. Nothing produces a position yet. Add `func Lines(text string) []string`, which splits a text on newlines."),
			cloud("What counts as a word here: a run of letters and digits, in any script. An apostrophe with a letter on both sides stays in the word, so don't is one word, and everything else ends a word, so well-known is two. Nothing splits words yet. Add `func Blank(text string) bool`, whether a text has no letters or digits at all."),
			local("Add `func Tokenize(text string) []Token`: the words of a text in order, each with its Start."),
			local("Add `func Normalize(word string) string`, which lowercases a word and drops a trailing possessive, so dog's becomes dog and dogs stays dogs. Two words are the same word when they normalize the same, and that goes for every comparison of words in this package from now on."),
			local("Read docs/corpus_notes.txt and tell me what's in there for us. No code changes."),
			cloud("Add `func Count(text, word string) int`: how many times the word occurs in the text."),
			cloud("Add `func Frequencies(text string) map[string]int`: how often each word occurs in the text."),
			local("Add `func Top(text string, n int) []string`: the n most frequent words in the text, or fewer if it has fewer, with the most frequent first and ties in alphabetical order."),
			cloud("Add `func Snippet(text string, t Token, around int) string`: the piece of the text holding the token, with up to around characters of the text on each side of it."),
		),
		Carries: []Carry{
			{
				What:        "positions count characters, not encoded length",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2, 8},
				Tests:       []string{"TestTokenizeStartsCountCharacters", "TestSnippetCountsCharacters"},
				Absent:      []string{"encoded", "rune", "byte", "highlighter", "e-acute", "positions"},
			},
			{
				What:        "a word is letters and digits, keeping an apostrophe between letters",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{2, 5},
				Tests:       []string{"TestTokenizeAccentedWordIsOneWord", "TestTokenizeApostropheInside", "TestTokenizeApostropheOutside", "TestTokenizeHyphenSplits", "TestCountKeepsWordsWhole", "TestFrequenciesSameWordAnyForm"},
				Absent:      []string{"apostrophe", "hyphen", "don't", "well-known", "letters and digits"},
			},
			{
				What:        "every comparison of words goes through Normalize",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{5, 6, 7},
				Tests:       []string{"TestCountSameWordAnyForm", "TestFrequenciesSameWordAnyForm", "TestTopSameWordAnyForm"},
				Absent:      []string{"normalize", "same word", "lowercase", "letter case"},
			},
			{
				What:        "stop words stay out of frequency tables and rankings (docs/corpus_notes.txt)",
				Kind:        CarryDiscovered,
				Established: 4,
				Needed:      []int{6, 7},
				Source:      "docs/corpus_notes.txt",
				Tests:       []string{"TestFrequenciesLeaveOutStopWords", "TestTopLeavesOutStopWords"},
				Absent:      []string{"stop word", "stopword", "corpus", "signal"},
			},
		},
	}
}
