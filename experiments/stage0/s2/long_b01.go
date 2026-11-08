package s2

func longB01() Task {
	return Task{
		ID:     "s2-long-b01-subtitle-tool",
		Set:    "long",
		System: system("a subtitle file tool"),
		Files: map[string]string{
			"go.mod": goMod,
			"cue.go": `package subtitle

import "strings"

// Cue is one subtitle: what is shown and when. Times are in milliseconds.
type Cue struct {
	Start, End int
	Lines      []string
}

// Duration returns how long the cue is on screen.
func (c Cue) Duration() int { return c.End - c.Start }

// Text returns the cue's lines joined with newlines.
func (c Cue) Text() string { return strings.Join(c.Lines, "\n") }
`,
			"srt.go": `package subtitle

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrMalformed is the error for text that cannot be read as subtitles.
var ErrMalformed = errors.New("malformed subtitles")

// ParseTime reads a timecode such as 00:01:02,500 into milliseconds.
func ParseTime(s string) (int, error) {
	var h, m, sec, ms int
	if n, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d:%d,%d", &h, &m, &sec, &ms); err != nil || n != 4 {
		return 0, fmt.Errorf("%w: time %q", ErrMalformed, s)
	}
	return (h*3600+m*60+sec)*1000 + ms, nil
}

// FormatTime writes milliseconds as a timecode.
func FormatTime(ms int) string {
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

func parseBlock(block string) (Cue, error) {
	lines := strings.Split(block, "\n")
	if len(lines) < 2 {
		return Cue{}, fmt.Errorf("%w: short block", ErrMalformed)
	}
	if _, err := strconv.Atoi(strings.TrimSpace(lines[0])); err != nil {
		return Cue{}, fmt.Errorf("%w: cue number %q", ErrMalformed, lines[0])
	}
	from, to, ok := strings.Cut(lines[1], " --> ")
	if !ok {
		return Cue{}, fmt.Errorf("%w: timing %q", ErrMalformed, lines[1])
	}
	start, err := ParseTime(from)
	if err != nil {
		return Cue{}, err
	}
	end, err := ParseTime(to)
	if err != nil {
		return Cue{}, err
	}
	return Cue{Start: start, End: end, Lines: lines[2:]}, nil
}

// ParseSRT reads a whole file.
func ParseSRT(text string) ([]Cue, error) {
	var cues []Cue
	for _, block := range strings.Split(strings.TrimSpace(text), "\n\n") {
		c, err := parseBlock(block)
		if err != nil {
			return nil, err
		}
		cues = append(cues, c)
	}
	return cues, nil
}

// WriteSRT writes cues as a file.
func WriteSRT(cues []Cue) string {
	var b strings.Builder
	for i, c := range cues {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, FormatTime(c.Start), FormatTime(c.End), c.Text())
	}
	return b.String()
}
`,
			"timing.go": `package subtitle

import "sort"

// Shift moves every cue by delta milliseconds.
func Shift(cues []Cue, delta int) []Cue {
	for i := range cues {
		cues[i].Start += delta
		cues[i].End += delta
	}
	return cues
}

// SortByStart orders cues by when they appear, keeping ties as they are.
func SortByStart(cues []Cue) {
	sort.SliceStable(cues, func(a, b int) bool { return cues[a].Start < cues[b].Start })
}
`,
			"layout.go": `package subtitle

import "strings"

// Words returns the words of a cue, whatever its line breaks.
func (c Cue) Words() []string { return strings.Fields(strings.Join(c.Lines, " ")) }

// LineTotal returns how many text lines the cues have between them.
func LineTotal(cues []Cue) int {
	total := 0
	for _, c := range cues {
		total += len(c.Lines)
	}
	return total
}
`,
			"lint.go": `package subtitle

import "fmt"

// Issue is one problem found in a list of cues. Cue is an index into that list.
type Issue struct {
	Cue  int
	Code string
}

// String writes the issue for a report.
func (i Issue) String() string { return fmt.Sprintf("cue %d: %s", i.Cue, i.Code) }

// CountCodes returns how many issues have each code.
func CountCodes(issues []Issue) map[string]int {
	out := map[string]int{}
	for _, i := range issues {
		out[i.Code]++
	}
	return out
}
`,
			"report.go": `package subtitle

// LastEnd returns the latest End among the cues, or 0 if there are none.
func LastEnd(cues []Cue) int {
	last := 0
	for _, c := range cues {
		last = max(last, c.End)
	}
	return last
}
`,
			"docs/style-guide.txt": `Client style guide, revision 3

Layout
- A cue has at most 2 lines.
- A line has at most 42 characters.

Timing
- A cue stays on screen for at least 1000 ms and at most 7000 ms.
- Consecutive cues are at least 80 ms apart.

Reading speed
- At most 20 characters per second.
- Reading speed is a cue's characters divided by its duration in seconds. Spaces
  count as characters. Line breaks do not count.

Our checker reports any cue that breaks one of these and rejects the delivery.

Delivery
- One file per language, named title.language.srt, for example harbour.de.srt.
- Italics mark voices from off screen. Keep the tags where they were set.
- Numbers up to ten are spelled out. The checker does not look at wording, but
  our reviewers do, and a reviewer can reject a file that passes the checker.
- Corrections after delivery go in a new file with a higher revision number.
`,
			"docs/vtt-export.txt": `Import format of the streaming platform (WebVTT subset)

File
- The first line is WEBVTT, followed by one blank line.
- Each cue is its timing line, then its text lines, then one blank line.
- No cue numbers. Line endings are LF.

Timing line
- 00:00:01.500 --> 00:00:03.000
- Hours are always written, and there is a dot before the milliseconds.

Cue text
- Each & becomes &amp; and each < becomes &lt;, except that the tags <i> </i> <b>
  </b> <u> </u> are kept as they are.
- {\an8} at the very start of a cue's first line means top of the screen. Remove
  it and put " line:10%" at the end of the timing line, after the end time.
- Any other {...} tag is removed.
- A cue with no text left after that is left out of the file.

Size
- Files over 2 MB are refused at upload. A feature film is usually under 200 KB.
- The platform keeps every version uploaded, and shows viewers the newest.
`,
			"subtitle_test.go": `package subtitle

import "testing"

func TestTimeRoundTrip(t *testing.T) {
	ms, err := ParseTime("02:03:04,005")
	if err != nil || ms != 7384005 {
		t.Fatalf("ParseTime = %d, %v", ms, err)
	}
	if got := FormatTime(ms); got != "02:03:04,005" {
		t.Fatalf("FormatTime = %q", got)
	}
}

func TestParseOneCue(t *testing.T) {
	cues, err := ParseSRT("1\n00:00:01,000 --> 00:00:02,500\nHello\nthere\n")
	if err != nil || len(cues) != 1 {
		t.Fatalf("ParseSRT = %v, %v", cues, err)
	}
	if cues[0].Start != 1000 || cues[0].Duration() != 1500 || cues[0].Text() != "Hello\nthere" {
		t.Fatalf("cue = %+v", cues[0])
	}
}

func TestShiftMovesBothEnds(t *testing.T) {
	got := Shift([]Cue{{Start: 1000, End: 2000, Lines: []string{"a"}}}, 500)
	if len(got) != 1 || got[0].Start != 1500 || got[0].End != 2500 {
		t.Fatalf("Shift = %+v", got)
	}
}

func TestWordsIgnoreBreaks(t *testing.T) {
	if got := (Cue{Lines: []string{"one two", "three"}}).Words(); len(got) != 3 {
		t.Fatalf("Words = %v", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package subtitle_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	subtitle "fixture"
)

func cue(start, end int, lines ...string) subtitle.Cue {
	return subtitle.Cue{Start: start, End: end, Lines: lines}
}

func codes(issues []subtitle.Issue, index int) []string {
	var out []string
	for _, i := range issues {
		if i.Cue == index {
			out = append(out, i.Code)
		}
	}
	return out
}

func TestParseToleratesWildFiles(t *testing.T) {
	cues, err := subtitle.ParseSRT(strings.ReplaceAll("\xef\xbb\xbf1\n00:00:01,000 --> 00:00:02,000\nHi\n\n00:00:03,000 --> 00:00:04,000\nBye\n", "\n", "\x0d\n"))
	if err != nil || len(cues) != 2 {
		t.Fatalf("ParseSRT = %+v, %v", cues, err)
	}
	if cues[0].Text() != "Hi" || cues[1].Start != 3000 || cues[1].Text() != "Bye" {
		t.Fatalf("cues = %+v", cues)
	}
}

func TestParseErrorNamesBlock(t *testing.T) {
	_, err := subtitle.ParseSRT("1\n00:00:01,000 --> 00:00:02,000\nok\n\n2\nnot a timing line\nbad\n")
	var pe *subtitle.ParseError
	if !errors.As(err, &pe) || pe.Block != 2 {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, subtitle.ErrMalformed) {
		t.Fatalf("%v does not match ErrMalformed", err)
	}
}

func TestParseLenientCollectsErrors(t *testing.T) {
	cues, errs := subtitle.ParseLenient("1\n00:00:01,000 --> 00:00:02,000\nok\n\n2\nbad\nbad\n\n3\n00:00:03,000 --> 00:00:04,000\nfine\n")
	if len(cues) != 2 || len(errs) != 1 {
		t.Fatalf("ParseLenient = %+v, %v", cues, errs)
	}
	var pe *subtitle.ParseError
	if !errors.As(errs[0], &pe) || pe.Block != 2 {
		t.Fatalf("err = %v", errs[0])
	}
}

func TestShiftClampsAndDrops(t *testing.T) {
	got := subtitle.Shift([]subtitle.Cue{cue(100, 200, "gone"), cue(400, 900, "clamped"), cue(1000, 1500, "kept")}, -500)
	if len(got) != 2 || got[0].Start != 0 || got[0].End != 400 || got[1].Start != 500 {
		t.Fatalf("Shift = %+v", got)
	}
}

func TestShiftLeavesInput(t *testing.T) {
	in := []subtitle.Cue{cue(1000, 2000, "a")}
	got := subtitle.Shift(in, 500)
	got[0].Lines[0] = "changed"
	if in[0].Start != 1000 || in[0].Lines[0] != "a" {
		t.Fatalf("input became %+v", in)
	}
}

func TestContainsExcludesEnd(t *testing.T) {
	c := cue(1000, 2000, "a")
	if !c.Contains(1000) || !c.Contains(1999) || c.Contains(2000) || c.Contains(999) {
		t.Fatal("wrong bounds")
	}
}

func TestCueAtBoundary(t *testing.T) {
	cues := []subtitle.Cue{cue(0, 1000, "a"), cue(1000, 2000, "b")}
	if got, ok := subtitle.CueAt(cues, 1000); !ok || got.Text() != "b" {
		t.Fatalf("CueAt(1000) = %+v, %v", got, ok)
	}
	if _, ok := subtitle.CueAt(cues, 2000); ok {
		t.Fatal("something on screen at 2000")
	}
}

func TestOverlapsTouching(t *testing.T) {
	if subtitle.Overlaps(cue(0, 1000), cue(1000, 2000)) {
		t.Fatal("touching cues overlap")
	}
	if !subtitle.Overlaps(cue(0, 1001), cue(1000, 2000)) {
		t.Fatal("overlap missed")
	}
}

func TestOverlapPairsTouching(t *testing.T) {
	got := subtitle.OverlapPairs([]subtitle.Cue{cue(0, 1000), cue(1000, 2000), cue(1500, 2500)})
	if !slices.Equal(got, [][2]int{{1, 2}}) {
		t.Fatalf("OverlapPairs = %v", got)
	}
}

func TestEmpty(t *testing.T) {
	if !cue(0, 1, "  ", "").Empty() || cue(0, 1, "", "x").Empty() {
		t.Fatal("wrong")
	}
}

func TestIsDialogue(t *testing.T) {
	if !cue(0, 1, "Who?", "- Me.").IsDialogue() || cue(0, 1, "a - b", "-c").IsDialogue() {
		t.Fatal("wrong")
	}
}

func TestTotalShown(t *testing.T) {
	if got := subtitle.TotalShown([]subtitle.Cue{cue(0, 1000), cue(5000, 5500)}); got != 1500 {
		t.Fatalf("TotalShown = %d", got)
	}
}

func TestWriteSRTOrdersAndNumbers(t *testing.T) {
	got := subtitle.WriteSRT([]subtitle.Cue{cue(3000, 4000, "b"), cue(5000, 5000, " "), cue(1000, 2000, "a")})
	want := "1\n00:00:01,000 --> 00:00:02,000\na\n\n2\n00:00:03,000 --> 00:00:04,000\nb\n\n"
	if got != want {
		t.Fatalf("WriteSRT = %q", got)
	}
}

func TestReflowFitsAtLimit(t *testing.T) {
	got := subtitle.Reflow(cue(0, 1, strings.Repeat("a", 18), strings.Repeat("b", 18)))
	if len(got.Lines) != 1 || got.Lines[0] != strings.Repeat("a", 18)+" "+strings.Repeat("b", 18) {
		t.Fatalf("Reflow = %q", got.Lines)
	}
}

func TestReflowBreaksPastLimit(t *testing.T) {
	got := subtitle.Reflow(cue(0, 1, strings.Repeat("a", 18)+" "+strings.Repeat("b", 19)))
	if len(got.Lines) != 2 {
		t.Fatalf("Reflow = %q", got.Lines)
	}
}

func TestReflowCountsCharacters(t *testing.T) {
	got := subtitle.Reflow(cue(0, 1, strings.Repeat("é", 18), strings.Repeat("字", 18)))
	if len(got.Lines) != 1 {
		t.Fatalf("Reflow = %q", got.Lines)
	}
}

func TestReflowLeavesDialogue(t *testing.T) {
	got := subtitle.Reflow(cue(0, 1, "- Who is it?", "- Me."))
	if !slices.Equal(got.Lines, []string{"- Who is it?", "- Me."}) {
		t.Fatalf("Reflow = %q", got.Lines)
	}
}

func TestReflowLeavesInput(t *testing.T) {
	in := cue(0, 1, "- Who is it?", "- Me.")
	got := subtitle.Reflow(in)
	got.Lines[0] = "changed"
	if in.Lines[0] != "- Who is it?" {
		t.Fatalf("input became %q", in.Lines)
	}
}

func TestSplitTwoLinesPerCue(t *testing.T) {
	word := strings.Repeat("w", 25)
	got := subtitle.Split(cue(0, 10000, strings.Join(slices.Repeat([]string{word}, 5), " ")))
	if len(got) != 3 || len(got[0].Lines) != 2 || len(got[2].Lines) != 1 {
		t.Fatalf("Split = %+v", got)
	}
	if got[0].End != 4000 || got[1].Start != 4000 || got[1].End != 8000 || got[2].End != 10000 {
		t.Fatalf("Split times = %+v", got)
	}
}

func TestSplitNewLimit(t *testing.T) {
	pair := strings.Repeat("a", 20) + " " + strings.Repeat("b", 20)
	got := subtitle.Split(cue(0, 9000, pair, pair, pair))
	if len(got) != 3 {
		t.Fatalf("Split made %d cues: %+v", len(got), got)
	}
}

func TestSplitLeavesDialogue(t *testing.T) {
	got := subtitle.Split(cue(0, 3000, "- One", "- Two", "- Three"))
	if len(got) != 1 || len(got[0].Lines) != 3 {
		t.Fatalf("Split = %+v", got)
	}
}

func TestEnforceGapsPullsBack(t *testing.T) {
	got := subtitle.EnforceGaps([]subtitle.Cue{cue(1050, 2000, "b"), cue(0, 1000, "a")})
	if got[0].Text() != "a" || got[0].End != 970 || got[1].Start != 1050 {
		t.Fatalf("EnforceGaps = %+v", got)
	}
	kept := subtitle.EnforceGaps([]subtitle.Cue{cue(0, 1000, "a"), cue(1080, 2000, "b")})
	if kept[0].End != 1000 {
		t.Fatalf("EnforceGaps moved a good cue: %+v", kept)
	}
}

func TestEnforceGapsLeavesInput(t *testing.T) {
	in := []subtitle.Cue{cue(1050, 2000, "b"), cue(0, 1000, "a")}
	subtitle.EnforceGaps(in)
	if in[0].Start != 1050 || in[1].End != 1000 {
		t.Fatalf("input became %+v", in)
	}
}

func TestMergeShortAbsorbsNext(t *testing.T) {
	got := subtitle.MergeShort([]subtitle.Cue{cue(0, 500, "a"), cue(600, 1200, "b"), cue(1300, 3000, "c")})
	if len(got) != 2 || got[0].End != 1200 || !slices.Equal(got[0].Lines, []string{"a", "b"}) {
		t.Fatalf("MergeShort = %+v", got)
	}
	kept := subtitle.MergeShort([]subtitle.Cue{cue(0, 1000, "a"), cue(1100, 3000, "b")})
	if len(kept) != 2 {
		t.Fatalf("MergeShort merged a cue long enough: %+v", kept)
	}
}

func TestMergeShortRespectsLines(t *testing.T) {
	got := subtitle.MergeShort([]subtitle.Cue{cue(0, 500, "a", "b"), cue(600, 2000, "c")})
	if len(got) != 2 {
		t.Fatalf("MergeShort = %+v", got)
	}
}

func TestMergeShortLeavesDialogue(t *testing.T) {
	got := subtitle.MergeShort([]subtitle.Cue{cue(0, 500, "- Hi"), cue(600, 2000, "there")})
	if len(got) != 2 {
		t.Fatalf("MergeShort = %+v", got)
	}
}

func TestMergeShortLeavesInput(t *testing.T) {
	in := []subtitle.Cue{cue(0, 2000, "a")}
	got := subtitle.MergeShort(in)
	got[0].Lines[0] = "changed"
	if in[0].Lines[0] != "a" {
		t.Fatalf("input became %+v", in)
	}
}

func TestRetimeScales(t *testing.T) {
	in := []subtitle.Cue{cue(10000, 20000, "a")}
	got := subtitle.Retime(in, 23.976, 25)
	if got[0].Start != 9590 || got[0].End != 19181 {
		t.Fatalf("Retime = %+v", got)
	}
}

func TestRetimeLeavesInput(t *testing.T) {
	in := []subtitle.Cue{cue(10000, 20000, "a")}
	got := subtitle.Retime(in, 23.976, 25)
	got[0].Lines[0] = "changed"
	if in[0].Start != 10000 || in[0].Lines[0] != "a" {
		t.Fatalf("input became %+v", in)
	}
}

func TestCPSCountsSpacesNotBreaks(t *testing.T) {
	if got := cue(0, 2000, "ab cd", "ef").CPS(); got != 3.5 {
		t.Fatalf("CPS = %v", got)
	}
	if got := cue(5, 5, "ab").CPS(); got != 0 {
		t.Fatalf("CPS with no duration = %v", got)
	}
}

func TestCPSCountsCharacters(t *testing.T) {
	if got := cue(0, 1000, "éé字").CPS(); got != 3 {
		t.Fatalf("CPS = %v", got)
	}
}

func TestCPSSkipsSoundNotes(t *testing.T) {
	if got := cue(0, 1000, "[door slams]", "go").CPS(); got != 2 {
		t.Fatalf("CPS = %v", got)
	}
	if got := cue(0, 1000, "[bang] go").CPS(); got != 3 {
		t.Fatalf("CPS = %v", got)
	}
}

func TestFastest(t *testing.T) {
	i, cps := subtitle.Fastest([]subtitle.Cue{cue(0, 1000, "ab"), cue(2000, 3000, "abcd"), cue(4000, 5000, "a")})
	if i != 1 || cps != 4 {
		t.Fatalf("Fastest = %d, %v", i, cps)
	}
	if i, cps := subtitle.Fastest(nil); i != -1 || cps != 0 {
		t.Fatalf("Fastest(nil) = %d, %v", i, cps)
	}
}

func TestLintCleanCue(t *testing.T) {
	if got := subtitle.Lint([]subtitle.Cue{cue(0, 2000, "All good here")}); len(got) != 0 {
		t.Fatalf("Lint = %v", got)
	}
}

func TestLintTooManyLines(t *testing.T) {
	got := subtitle.Lint([]subtitle.Cue{cue(0, 2000, "a", "b", "c")})
	if !slices.Equal(codes(got, 0), []string{"lines"}) {
		t.Fatalf("Lint = %v", got)
	}
}

func TestLintLineLength(t *testing.T) {
	got := subtitle.Lint([]subtitle.Cue{cue(0, 5000, strings.Repeat("a", 38))})
	if !slices.Equal(codes(got, 0), []string{"length"}) {
		t.Fatalf("Lint = %v", got)
	}
}

func TestLintLengthCountsCharacters(t *testing.T) {
	if got := subtitle.Lint([]subtitle.Cue{cue(0, 5000, strings.Repeat("é", 37))}); len(got) != 0 {
		t.Fatalf("Lint = %v", got)
	}
}

func TestLintDurations(t *testing.T) {
	if got := subtitle.Lint([]subtitle.Cue{cue(0, 999, "a")}); !slices.Equal(codes(got, 0), []string{"short"}) {
		t.Fatalf("999 ms: %v", got)
	}
	if got := subtitle.Lint([]subtitle.Cue{cue(0, 7001, "a")}); !slices.Equal(codes(got, 0), []string{"long"}) {
		t.Fatalf("7001 ms: %v", got)
	}
	if got := subtitle.Lint([]subtitle.Cue{cue(0, 1000, "a"), cue(2000, 9000, "b")}); len(got) != 0 {
		t.Fatalf("1000 and 7000 ms: %v", got)
	}
}

func TestLintGapToNext(t *testing.T) {
	got := subtitle.Lint([]subtitle.Cue{cue(0, 1000, "a"), cue(1079, 2500, "b"), cue(2580, 4000, "c")})
	if !slices.Equal(got, []subtitle.Issue{{Cue: 0, Code: "gap"}}) {
		t.Fatalf("Lint = %v", got)
	}
}

func TestLintTouchingIsNoOverlap(t *testing.T) {
	got := subtitle.Lint([]subtitle.Cue{cue(0, 1000, "a"), cue(1000, 2000, "b")})
	if !slices.Equal(got, []subtitle.Issue{{Cue: 0, Code: "gap"}}) {
		t.Fatalf("Lint = %v", got)
	}
}

func TestLintOverlapNotAlsoGap(t *testing.T) {
	got := subtitle.Lint([]subtitle.Cue{cue(0, 2000, "a"), cue(1500, 3000, "b")})
	if !slices.Equal(got, []subtitle.Issue{{Cue: 0, Code: "overlap"}}) {
		t.Fatalf("Lint = %v", got)
	}
}

func TestLintTooFast(t *testing.T) {
	if got := subtitle.Lint([]subtitle.Cue{cue(0, 1000, strings.Repeat("a", 21))}); !slices.Equal(codes(got, 0), []string{"fast"}) {
		t.Fatalf("21 characters: %v", got)
	}
	if got := subtitle.Lint([]subtitle.Cue{cue(0, 1000, strings.Repeat("a", 20))}); len(got) != 0 {
		t.Fatalf("20 characters: %v", got)
	}
}

func TestLintFastCountsCharacters(t *testing.T) {
	if got := subtitle.Lint([]subtitle.Cue{cue(0, 1000, strings.Repeat("é", 20))}); len(got) != 0 {
		t.Fatalf("Lint = %v", got)
	}
}

func TestLintFastSkipsSoundNotes(t *testing.T) {
	got := subtitle.Lint([]subtitle.Cue{cue(0, 1000, "[a very loud door slams]", "Run")})
	if len(got) != 0 {
		t.Fatalf("Lint = %v", got)
	}
}

func TestLintOrderWithinCue(t *testing.T) {
	got := subtitle.Lint([]subtitle.Cue{cue(0, 500, "a", "b", strings.Repeat("c", 60)), cue(400, 3000, "d")})
	if !slices.Equal(codes(got, 0), []string{"lines", "length", "short", "fast", "overlap"}) {
		t.Fatalf("Lint = %v", got)
	}
}

func TestVTTLayout(t *testing.T) {
	got := subtitle.WriteVTT([]subtitle.Cue{cue(1500, 3000, "Hi", "there"), cue(3600000, 3601000, "Bye")})
	want := "WEBVTT\n\n00:00:01.500 --> 00:00:03.000\nHi\nthere\n\n01:00:00.000 --> 01:00:01.000\nBye\n\n"
	if got != want {
		t.Fatalf("WriteVTT = %q", got)
	}
}

func TestVTTEscapes(t *testing.T) {
	got := subtitle.WriteVTT([]subtitle.Cue{cue(0, 1000, "a & b < c <i>d</i> <b>e</b>")})
	if !strings.Contains(got, "\na &amp; b &lt; c <i>d</i> <b>e</b>\n") {
		t.Fatalf("WriteVTT = %q", got)
	}
}

func TestVTTTopOfScreen(t *testing.T) {
	got := subtitle.WriteVTT([]subtitle.Cue{cue(0, 1000, "{\\an8}Up here", "and here")})
	if !strings.Contains(got, "00:00:01.000 line:10%\nUp here\nand here\n") {
		t.Fatalf("WriteVTT = %q", got)
	}
}

func TestVTTOtherTagsRemoved(t *testing.T) {
	got := subtitle.WriteVTT([]subtitle.Cue{cue(0, 1000, "{\\pos(1,2)}x{\\b1}y")})
	if !strings.Contains(got, "\nxy\n") || strings.Contains(got, "line:") {
		t.Fatalf("WriteVTT = %q", got)
	}
}

func TestVTTLeavesOutEmptied(t *testing.T) {
	if got := subtitle.WriteVTT([]subtitle.Cue{cue(0, 1000, "{\\an3}")}); got != "WEBVTT\n\n" {
		t.Fatalf("WriteVTT = %q", got)
	}
}

func TestHearingCutRemovesSoundNotes(t *testing.T) {
	got := subtitle.HearingCut([]subtitle.Cue{cue(0, 1000, "[door slams]", "[bang] Go now")})
	if len(got) != 1 || !slices.Equal(got[0].Lines, []string{"Go now"}) {
		t.Fatalf("HearingCut = %+v", got)
	}
}

func TestHearingCutDropsEmptied(t *testing.T) {
	got := subtitle.HearingCut([]subtitle.Cue{cue(0, 1000, "[music]"), cue(2000, 3000, "words")})
	if len(got) != 1 || got[0].Start != 2000 {
		t.Fatalf("HearingCut = %+v", got)
	}
}

func TestBuildReport(t *testing.T) {
	r := subtitle.BuildReport([]subtitle.Cue{cue(0, 2000, "hello"), cue(3000, 5000, "world")})
	if r.Cues != 2 || r.Issues != 0 || r.Shown != 4000 || r.Fastest != 2.5 {
		t.Fatalf("BuildReport = %+v", r)
	}
	if got := r.String(); got != "cues=2 issues=0 shown=00:00:04,000 fastest=2.5" {
		t.Fatalf("String = %q", got)
	}
}

func TestClipCutsAndMoves(t *testing.T) {
	got := subtitle.Clip([]subtitle.Cue{cue(500, 1500, "a"), cue(2000, 3000, "b"), cue(3500, 5000, "c")}, 1000, 4000)
	if len(got) != 3 || got[0].Start != 0 || got[0].End != 500 || got[1].Start != 1000 || got[2].End != 3000 {
		t.Fatalf("Clip = %+v", got)
	}
}

func TestClipEndExcluded(t *testing.T) {
	got := subtitle.Clip([]subtitle.Cue{cue(0, 1000, "before"), cue(1000, 2000, "in"), cue(3000, 4000, "after")}, 1000, 3000)
	if len(got) != 1 || got[0].Text() != "in" {
		t.Fatalf("Clip = %+v", got)
	}
}

func TestClipLeavesInput(t *testing.T) {
	in := []subtitle.Cue{cue(1000, 2000, "a")}
	got := subtitle.Clip(in, 0, 5000)
	got[0].Lines[0] = "changed"
	if in[0].Lines[0] != "a" {
		t.Fatalf("input became %+v", in)
	}
}

func TestReadVTTRoundTrip(t *testing.T) {
	in := []subtitle.Cue{cue(1500, 3000, "a & b <c", "<i>d</i>"), cue(4000, 5000, "{\\an8}Top", "row")}
	got, err := subtitle.ReadVTT(subtitle.WriteVTT(in))
	if err != nil || len(got) != 2 {
		t.Fatalf("ReadVTT = %+v, %v", got, err)
	}
	for i := range in {
		if got[i].Start != in[i].Start || got[i].End != in[i].End || !slices.Equal(got[i].Lines, in[i].Lines) {
			t.Errorf("cue %d: %+v", i, got[i])
		}
	}
}

func TestReadVTTRefusesOtherFiles(t *testing.T) {
	for _, text := range []string{"1\n00:00:01,000 --> 00:00:02,000\nHi\n", "WEBVTT\n\nnonsense\nmore\n"} {
		if _, err := subtitle.ReadVTT(text); !errors.Is(err, subtitle.ErrMalformed) {
			t.Errorf("%q: %v", text, err)
		}
	}
}

func TestReviewSheet(t *testing.T) {
	got := subtitle.ReviewSheet([]subtitle.Cue{cue(0, 2000, "ab", "cd")})
	want := "no\tstart\tend\tcps\ttext\n1\t00:00:00,000\t00:00:02,000\t2.0\tab / cd\n"
	if got != want {
		t.Fatalf("ReviewSheet = %q", got)
	}
}

func TestFindTextAcrossBreak(t *testing.T) {
	got := subtitle.FindText([]subtitle.Cue{cue(0, 1, "no"), cue(2, 3, "Run for", "THE hills")}, "for the")
	if !slices.Equal(got, []int{1}) {
		t.Fatalf("FindText = %v", got)
	}
}

func TestPrepareLintsClean(t *testing.T) {
	word := strings.Repeat("w", 25)
	long := strings.Join(slices.Repeat([]string{word}, 6), " ")
	got := subtitle.Prepare([]subtitle.Cue{cue(20000, 22000, "later"), cue(19000, 19000, ""), cue(0, 9000, long)})
	if issues := subtitle.Lint(got); len(issues) != 0 {
		t.Fatalf("Lint = %v of %+v", issues, got)
	}
	if got[len(got)-1].Text() != "later" {
		t.Fatalf("Prepare = %+v", got)
	}
}
`},
		Solution: map[string]string{
			"cue.go": `package subtitle

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Cue is one subtitle: what is shown and when. Times are in milliseconds.
type Cue struct {
	Start, End int
	Lines      []string
}

// soundNote matches text in square brackets, which describes a sound for deaf viewers.
var soundNote = regexp.MustCompile("\\[[^\\]]*\\]")

// Duration returns how long the cue is on screen.
func (c Cue) Duration() int { return c.End - c.Start }

// Text returns the cue's lines joined with newlines.
func (c Cue) Text() string { return strings.Join(c.Lines, "\n") }

// Empty reports whether all the cue's lines are blank.
func (c Cue) Empty() bool { return strings.TrimSpace(c.Text()) == "" }

// Contains reports whether the cue is on screen at time t. At End it has gone.
func (c Cue) Contains(t int) bool { return t >= c.Start && t < c.End }

// CueAt returns the first cue in the slice that is on screen at time t.
func CueAt(cues []Cue, t int) (Cue, bool) {
	for _, c := range cues {
		if c.Contains(t) {
			return c, true
		}
	}
	return Cue{}, false
}

// FindText returns the indexes of the cues whose text contains query in any case. A
// query can run across a line break.
func FindText(cues []Cue, query string) []int {
	query = strings.ToLower(query)
	var out []int
	for i, c := range cues {
		if strings.Contains(strings.ToLower(strings.Join(c.Lines, " ")), query) {
			out = append(out, i)
		}
	}
	return out
}

// Overlaps reports whether two cues are on screen at the same time.
func Overlaps(a, b Cue) bool { return a.Start < b.End && b.Start < a.End }

// OverlapPairs returns the index pairs of the cues that overlap, in order.
func OverlapPairs(cues []Cue) [][2]int {
	var out [][2]int
	for i := range cues {
		for j := i + 1; j < len(cues); j++ {
			if Overlaps(cues[i], cues[j]) {
				out = append(out, [2]int{i, j})
			}
		}
	}
	return out
}

// IsDialogue reports whether the cue has two speakers.
func (c Cue) IsDialogue() bool {
	for _, line := range c.Lines {
		if strings.HasPrefix(line, "- ") {
			return true
		}
	}
	return false
}

// lineChars returns the length of a line as a viewer sees it.
func lineChars(line string) int { return utf8.RuneCountInString(line) }

// readingChars returns the characters a viewer has to read in the cue.
func (c Cue) readingChars() int {
	total := 0
	for _, line := range c.Lines {
		line = soundNote.ReplaceAllString(line, "")
		total += lineChars(line)
	}
	return total
}

// CPS returns the cue's reading speed in characters per second.
func (c Cue) CPS() float64 {
	if c.Duration() <= 0 {
		return 0
	}
	return float64(c.readingChars()) * 1000 / float64(c.Duration())
}
`,
			"srt.go": `package subtitle

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ErrMalformed is the error for text that cannot be read as subtitles.
var ErrMalformed = errors.New("malformed subtitles")

// ParseError says which block of a file could not be read.
type ParseError struct {
	Block int // 1-based
	Msg   string
}

func (e *ParseError) Error() string { return fmt.Sprintf("block %d: %s", e.Block, e.Msg) }

// Unwrap makes a ParseError match ErrMalformed.
func (e *ParseError) Unwrap() error { return ErrMalformed }

// ParseTime reads a timecode such as 00:01:02,500 into milliseconds.
func ParseTime(s string) (int, error) {
	var h, m, sec, ms int
	if n, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d:%d,%d", &h, &m, &sec, &ms); err != nil || n != 4 {
		return 0, fmt.Errorf("%w: time %q", ErrMalformed, s)
	}
	return (h*3600+m*60+sec)*1000 + ms, nil
}

// FormatTime writes milliseconds as a timecode.
func FormatTime(ms int) string {
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// parseBlock reads one block. Its number line is optional.
func parseBlock(block string) (Cue, error) {
	lines := strings.Split(block, "\n")
	if _, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return Cue{}, fmt.Errorf("%w: short block", ErrMalformed)
	}
	from, to, ok := strings.Cut(lines[0], " --> ")
	if !ok {
		return Cue{}, fmt.Errorf("%w: timing %q", ErrMalformed, lines[0])
	}
	start, err := ParseTime(from)
	if err != nil {
		return Cue{}, err
	}
	end, err := ParseTime(to)
	if err != nil {
		return Cue{}, err
	}
	return Cue{Start: start, End: end, Lines: lines[1:]}, nil
}

// blocks cuts a file into its blocks, whatever its line endings.
func blocks(text string) []string {
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	text = strings.ReplaceAll(text, "\x0d\n", "\n")
	return strings.Split(strings.TrimSpace(text), "\n\n")
}

// ParseSRT reads a whole file.
func ParseSRT(text string) ([]Cue, error) {
	var cues []Cue
	for i, block := range blocks(text) {
		c, err := parseBlock(block)
		if err != nil {
			return nil, &ParseError{Block: i + 1, Msg: err.Error()}
		}
		cues = append(cues, c)
	}
	return cues, nil
}

// ParseLenient reads what it can, and returns an error for each block it cannot.
func ParseLenient(text string) ([]Cue, []error) {
	var cues []Cue
	var errs []error
	for i, block := range blocks(text) {
		c, err := parseBlock(block)
		if err != nil {
			errs = append(errs, &ParseError{Block: i + 1, Msg: err.Error()})
			continue
		}
		cues = append(cues, c)
	}
	return cues, errs
}

// WriteSRT writes cues as a file, in Start order and numbered from 1. Empty cues are left out.
func WriteSRT(cues []Cue) string {
	var kept []Cue
	for _, c := range cues {
		if !c.Empty() {
			kept = append(kept, c)
		}
	}
	sort.SliceStable(kept, func(a, b int) bool { return kept[a].Start < kept[b].Start })
	var b strings.Builder
	for i, c := range kept {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, FormatTime(c.Start), FormatTime(c.End), c.Text())
	}
	return b.String()
}
`,
			"timing.go": `package subtitle

import (
	"math"
	"slices"
	"sort"
)

func cloneCue(c Cue) Cue {
	c.Lines = slices.Clone(c.Lines)
	return c
}

func cloneCues(cues []Cue) []Cue {
	out := make([]Cue, len(cues))
	for i, c := range cues {
		out[i] = cloneCue(c)
	}
	return out
}

// Shift moves cues by delta milliseconds. A cue pushed before zero starts at 0, and
// one with nothing left after zero is dropped.
func Shift(cues []Cue, delta int) []Cue {
	out := make([]Cue, 0, len(cues))
	for _, c := range cues {
		c = cloneCue(c)
		c.Start += delta
		c.End += delta
		if c.End <= 0 {
			continue
		}
		c.Start = max(c.Start, 0)
		out = append(out, c)
	}
	return out
}

// SortByStart orders cues by when they appear, keeping ties as they are.
func SortByStart(cues []Cue) {
	sort.SliceStable(cues, func(a, b int) bool { return cues[a].Start < cues[b].Start })
}

// EnforceGaps returns the cues in Start order, with a cue's End pulled back wherever it
// is too close to the next cue.
func EnforceGaps(cues []Cue) []Cue {
	out := cloneCues(cues)
	SortByStart(out)
	for i := 0; i+1 < len(out); i++ {
		if out[i+1].Start-out[i].End < minGap {
			out[i].End = max(out[i+1].Start-minGap, out[i].Start)
		}
	}
	return out
}

// MergeShort lets a cue on screen too briefly absorb the cues after it, as long as the
// result has few enough lines. Cues with two speakers stay as they are.
func MergeShort(cues []Cue) []Cue {
	var out []Cue
	for i := 0; i < len(cues); i++ {
		c := cloneCue(cues[i])
		for c.Duration() < minDuration && i+1 < len(cues) {
			next := cues[i+1]
			if c.IsDialogue() || next.IsDialogue() || len(c.Lines)+len(next.Lines) > maxLines {
				break
			}
			c.End = next.End
			c.Lines = append(c.Lines, next.Lines...)
			i++
		}
		out = append(out, c)
	}
	return out
}

// Clip returns the part of the film between from and to, moved so that from is time
// zero. A cue running past either end is cut off there.
func Clip(cues []Cue, from, to int) []Cue {
	var out []Cue
	for _, c := range cues {
		if c.End <= from || c.Start >= to {
			continue
		}
		c = cloneCue(c)
		c.Start = max(c.Start, from) - from
		c.End = min(c.End, to) - from
		out = append(out, c)
	}
	return out
}

// Retime converts cues from one frame rate to another, to the nearest millisecond.
func Retime(cues []Cue, from, to float64) []Cue {
	out := cloneCues(cues)
	for i := range out {
		out[i].Start = int(math.Round(float64(out[i].Start) * from / to))
		out[i].End = int(math.Round(float64(out[i].End) * from / to))
	}
	return out
}
`,
			"layout.go": `package subtitle

import (
	"slices"
	"strings"
)

// Words returns the words of a cue, whatever its line breaks.
func (c Cue) Words() []string { return strings.Fields(strings.Join(c.Lines, " ")) }

// LineTotal returns how many text lines the cues have between them.
func LineTotal(cues []Cue) int {
	total := 0
	for _, c := range cues {
		total += len(c.Lines)
	}
	return total
}

// Reflow fills lines with the cue's words up to the line limit. A cue with two speakers
// keeps the lines it has.
func Reflow(c Cue) Cue {
	c = cloneCue(c)
	if c.IsDialogue() {
		return c
	}
	var lines []string
	line := ""
	for _, word := range c.Words() {
		switch {
		case line == "":
			line = word
		case lineChars(line)+1+lineChars(word) <= maxLineChars:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	c.Lines = lines
	return c
}

func charTotal(lines []string) int {
	total := 0
	for _, line := range lines {
		total += lineChars(line)
	}
	return total
}

// Split reflows a cue and, if it then has too many lines, makes several consecutive cues
// of it. Time is divided by characters, and each boundary is rounded down.
func Split(c Cue) []Cue {
	r := Reflow(c)
	if r.IsDialogue() || len(r.Lines) <= maxLines {
		return []Cue{r}
	}
	total := charTotal(r.Lines)
	var out []Cue
	start, done := c.Start, 0
	for chunk := range slices.Chunk(r.Lines, maxLines) {
		done += charTotal(chunk)
		end := c.Start + c.Duration()*done/total
		out = append(out, Cue{Start: start, End: end, Lines: slices.Clone(chunk)})
		start = end
	}
	return out
}

// HearingCut returns the cues without what is only there for viewers who cannot hear.
func HearingCut(cues []Cue) []Cue {
	var out []Cue
	for _, c := range cues {
		var lines []string
		for _, line := range c.Lines {
			if kept := strings.TrimSpace(soundNote.ReplaceAllString(line, "")); kept != "" {
				lines = append(lines, kept)
			}
		}
		if len(lines) > 0 {
			out = append(out, Cue{Start: c.Start, End: c.End, Lines: lines})
		}
	}
	return out
}

// Prepare makes cues ready for delivery to the client.
func Prepare(cues []Cue) []Cue {
	var parts []Cue
	for _, c := range cues {
		if !c.Empty() {
			parts = append(parts, Split(c)...)
		}
	}
	SortByStart(parts)
	return EnforceGaps(MergeShort(parts))
}
`,
			"lint.go": `package subtitle

import (
	"fmt"
	"slices"
)

// The client's limits. Times are in milliseconds.
const (
	maxLines     = 2
	maxLineChars = 37
	minDuration  = 1000
	maxDuration  = 7000
	minGap       = 80
	maxCPS       = 20.0
)

// Issue is one problem found in a list of cues. Cue is an index into that list.
type Issue struct {
	Cue  int
	Code string
}

// String writes the issue for a report.
func (i Issue) String() string { return fmt.Sprintf("cue %d: %s", i.Cue, i.Code) }

// CountCodes returns how many issues have each code.
func CountCodes(issues []Issue) map[string]int {
	out := map[string]int{}
	for _, i := range issues {
		out[i.Code]++
	}
	return out
}

// cueCodes checks one cue on its own.
func cueCodes(c Cue) []string {
	var codes []string
	if len(c.Lines) > maxLines {
		codes = append(codes, "lines")
	}
	if slices.ContainsFunc(c.Lines, func(l string) bool { return lineChars(l) > maxLineChars }) {
		codes = append(codes, "length")
	}
	if c.Duration() < minDuration {
		codes = append(codes, "short")
	}
	if c.Duration() > maxDuration {
		codes = append(codes, "long")
	}
	if c.CPS() > maxCPS {
		codes = append(codes, "fast")
	}
	return codes
}

// Lint checks cues against the client's limits, comparing each with the next in the slice.
func Lint(cues []Cue) []Issue {
	var out []Issue
	for i, c := range cues {
		for _, code := range cueCodes(c) {
			out = append(out, Issue{Cue: i, Code: code})
		}
		if i+1 == len(cues) {
			continue
		}
		switch next := cues[i+1]; {
		case Overlaps(c, next):
			out = append(out, Issue{Cue: i, Code: "overlap"})
		case next.Start-c.End < minGap:
			out = append(out, Issue{Cue: i, Code: "gap"})
		}
	}
	return out
}
`,
			"report.go": `package subtitle

import (
	"fmt"
	"strings"
)

// LastEnd returns the latest End among the cues, or 0 if there are none.
func LastEnd(cues []Cue) int {
	last := 0
	for _, c := range cues {
		last = max(last, c.End)
	}
	return last
}

// TotalShown returns the sum of the cues' durations.
func TotalShown(cues []Cue) int {
	total := 0
	for _, c := range cues {
		total += c.Duration()
	}
	return total
}

// Fastest returns the index and reading speed of the fastest cue, or -1 and 0 if there
// are none.
func Fastest(cues []Cue) (int, float64) {
	index, fastest := -1, 0.0
	for i, c := range cues {
		if cps := c.CPS(); index < 0 || cps > fastest {
			index, fastest = i, cps
		}
	}
	return index, fastest
}

// Report sums up a list of cues.
type Report struct {
	Cues, Issues, Shown int
	Fastest             float64
}

// BuildReport works out the report for cues.
func BuildReport(cues []Cue) Report {
	_, fastest := Fastest(cues)
	return Report{Cues: len(cues), Issues: len(Lint(cues)), Shown: TotalShown(cues), Fastest: fastest}
}

// String writes the report on one line.
func (r Report) String() string {
	return fmt.Sprintf("cues=%d issues=%d shown=%s fastest=%.1f", r.Cues, r.Issues, FormatTime(r.Shown), r.Fastest)
}

// ReviewSheet writes a tab-separated sheet for reviewers, one row per cue.
func ReviewSheet(cues []Cue) string {
	var b strings.Builder
	b.WriteString("no\tstart\tend\tcps\ttext\n")
	for i, c := range cues {
		text := strings.Join(c.Lines, " / ")
		fmt.Fprintf(&b, "%d\t%s\t%s\t%.1f\t%s\n", i+1, FormatTime(c.Start), FormatTime(c.End), c.CPS(), text)
	}
	return b.String()
}
`,
			"vtt.go": `package subtitle

import (
	"fmt"
	"regexp"
	"strings"
)

var ssaTag = regexp.MustCompile("\\{[^}]*\\}")

const topOfScreen = "{\\an8}"

// vttTime writes milliseconds the way the platform wants them.
func vttTime(ms int) string { return strings.Replace(FormatTime(ms), ",", ".", 1) }

// vttLine converts one line of cue text.
func vttLine(line string) string {
	line = ssaTag.ReplaceAllString(line, "")
	line = strings.ReplaceAll(line, "&", "&amp;")
	line = strings.ReplaceAll(line, "<", "&lt;")
	for _, tag := range []string{"i", "b", "u"} {
		line = strings.ReplaceAll(line, "&lt;"+tag+">", "<"+tag+">")
		line = strings.ReplaceAll(line, "&lt;/"+tag+">", "</"+tag+">")
	}
	return line
}

// vttCue writes one cue, or nothing if it has no text left.
func vttCue(c Cue) string {
	settings := ""
	if len(c.Lines) > 0 && strings.HasPrefix(c.Lines[0], topOfScreen) {
		settings = " line:10%"
	}
	var text []string
	for _, line := range c.Lines {
		if converted := vttLine(line); strings.TrimSpace(converted) != "" {
			text = append(text, converted)
		}
	}
	if len(text) == 0 {
		return ""
	}
	return fmt.Sprintf("%s --> %s%s\n%s\n\n", vttTime(c.Start), vttTime(c.End), settings, strings.Join(text, "\n"))
}

// WriteVTT writes cues in the streaming platform's import format.
func WriteVTT(cues []Cue) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, c := range cues {
		b.WriteString(vttCue(c))
	}
	return b.String()
}

// readVTTBlock reads one cue of the platform's format.
func readVTTBlock(block string) (Cue, error) {
	lines := strings.Split(block, "\n")
	timing, top := strings.CutSuffix(lines[0], " line:10%")
	c, err := parseBlock(strings.ReplaceAll(timing, ".", ",") + "\n" + strings.Join(lines[1:], "\n"))
	if err != nil {
		return Cue{}, err
	}
	for i, line := range c.Lines {
		line = strings.ReplaceAll(line, "&lt;", "<")
		c.Lines[i] = strings.ReplaceAll(line, "&amp;", "&")
	}
	if top && len(c.Lines) > 0 {
		c.Lines[0] = topOfScreen + c.Lines[0]
	}
	return c, nil
}

// ReadVTT reads what WriteVTT wrote back into cues.
func ReadVTT(text string) ([]Cue, error) {
	head, body, _ := strings.Cut(text, "\n\n")
	if head != "WEBVTT" {
		return nil, fmt.Errorf("%w: no WEBVTT line", ErrMalformed)
	}
	var cues []Cue
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		if block == "" {
			continue
		}
		c, err := readVTTBlock(block)
		if err != nil {
			return nil, err
		}
		cues = append(cues, c)
	}
	return cues, nil
}
`,
		},
		Turns: script(
			cloud("Read docs/style-guide.txt, the client's rules for subtitles, and tell me in a few lines what limits it sets. No code changes."),
			cloud("A rule for everything in this package that edits cues: a function never changes the caller's cues and never hands back Lines slices that share memory with theirs, because callers keep their originals for undo. Rewrite `Shift` in timing.go to follow it. While you're there: a cue pushed before zero gets its Start clamped to 0, and a cue with nothing left after zero is dropped."),
			cloud("Harden `ParseSRT` for files from the wild: ignore a leading byte order mark, accept CRLF line endings, and treat a block's number line as optional. When a block can't be read, return a `*ParseError`, a new type with `Block` (its 1-based position in the file) and `Msg` that matches ErrMalformed under errors.Is."),
			local("Get the timing semantics straight: End is the first millisecond a cue is no longer on screen, so one ending at 5000 and one starting at 5000 are never on screen together. Add `func (c Cue) Contains(t int) bool` and `func CueAt(cues []Cue, t int) (Cue, bool)`, the first cue in the slice on screen at t."),
			local("Something from the client to keep in mind: a character limit counts what the viewer sees, so é or 字 is one character however it's stored. Nothing to code for that today. Add `func (c Cue) Empty() bool`: true when all its lines are blank."),
			cloud("Two-speaker cues: a cue with any line starting with \"- \" is dialogue. The client wants those lines left exactly as the translator broke them, so nothing in this package may re-wrap them, split them up or merge them into a neighbour. Add `func (c Cue) IsDialogue() bool`."),
			cloud("Text in square brackets, like [door slams], is a sound annotation for deaf viewers. It takes room on screen but it isn't reading, so reading speed leaves out the brackets and what's in them and counts everything else as usual. We have no reading speed code yet, so nothing to do about it now. Add `func TotalShown(cues []Cue) int` to report.go, the sum of their durations."),
			local("Rewrite `WriteSRT`: cues come out sorted by Start, keeping ties in order, and numbered from 1 in that order. Empty cues are left out, with no hole in the numbering."),
			local("Add `func Reflow(c Cue) Cue` to layout.go: join the cue's lines and fill new lines word by word up to the client's line length, starting a new line when the next word wouldn't fit. A word longer than the limit gets a line of its own. Don't worry here about how many lines that makes."),
			cloud("Add `func Split(c Cue) []Cue` to layout.go. Reflow the cue. If that makes more lines than a cue may have, make several consecutive cues out of it, filling each in turn. Divide the cue's time in proportion to each part's characters, rounding each boundary down, so the last part ends exactly at End. A cue that fits comes back as one reflowed cue."),
			local("Read docs/vtt-export.txt, the streaming platform's import format, and tell me the main points. No code changes."),
			local("Add `func EnforceGaps(cues []Cue) []Cue` to timing.go. It returns the cues in Start order, and wherever a cue ends too close to the start of the next by the client's rules, it pulls that cue's End back just far enough, though never before its own Start."),
			cloud("Add `func Overlaps(a, b Cue) bool` and `func OverlapPairs(cues []Cue) [][2]int` to cue.go. The second returns the index pairs i < j of all cues that overlap, ordered by i, then j."),
			cloud("Time for lint.go. Add `func Lint(cues []Cue) []Issue`, checking against the client's limits. Codes: \"lines\" for too many lines, \"short\" and \"long\" for time on screen. Then compare each cue with the next one in the slice: \"overlap\" if they overlap, otherwise \"gap\" if they're too close. Both go on the earlier cue. Issues are in cue order and, within a cue, in the order I listed them."),
			local("News from the client: their new player is narrower, so the line limit is now 37 characters, for everything from here on. Their written guide is behind on this, so go by what I'm telling you. Change the code accordingly."),
			local("Add `func (c Cue) CPS() float64` to cue.go: the cue's reading speed as the client defines it, in characters per second. A cue with no duration reads 0."),
			local("Add `func Fastest(cues []Cue) (int, float64)` to report.go: the index and CPS of the fastest cue, with the earliest winning a tie, or -1 and 0 if there are no cues."),
			cloud("Extend `Lint` with two more codes for a single cue: \"length\" for a line that's too long, right after \"lines\", and \"fast\" for a cue read too fast, right after \"long\"."),
			cloud("In a new file vtt.go, write `func WriteVTT(cues []Cue) string`, the export for the streaming platform. Follow its file layout and timing line. Leave the cue text unconverted for now."),
			cloud("Finish `WriteVTT` by converting cue text as the platform requires, including the special handling of markup and of tags in braces."),
			local("Add `func HearingCut(cues []Cue) []Cue` to layout.go: the version for audiences who can hear the sound, without what's only there for viewers who can't. Trim the spaces this leaves at the ends of a line, drop lines left blank, and drop cues left with no lines."),
			local("Add `func MergeShort(cues []Cue) []Cue` to timing.go. The input is in order. A cue on screen for less than the client's minimum absorbs the next cue, taking over its End and appending its lines, again and again until it's long enough, but only as long as the result doesn't have more lines than a cue may have."),
			cloud("Add `func Retime(cues []Cue, from, to float64) []Cue` to timing.go, for frame rate conversion: both times of each cue are multiplied by from/to and rounded to the nearest millisecond."),
			local("Add `func ParseLenient(text string) ([]Cue, []error)` to srt.go: like ParseSRT, but it keeps going past blocks it can't read, returning the same errors for them that ParseSRT would."),
			cloud("Add to report.go `type Report struct { Cues, Issues, Shown int; Fastest float64 }`, `func BuildReport(cues []Cue) Report`, where Shown is TotalShown, Issues counts Lint's issues and Fastest is the fastest CPS, and `func (r Report) String() string`, which gives exactly: cues=2 issues=0 shown=00:00:04,000 fastest=2.5"),
			cloud("Add `func Prepare(cues []Cue) []Cue` to layout.go, the last step before delivery: leave out Empty cues, run the rest through Split, put them in Start order, then MergeShort, then EnforceGaps."),
			cloud("Add `func Clip(cues []Cue, from, to int) []Cue` to timing.go, for making trailers: the cues on screen at some point between from and to, moved so that from becomes time zero. A cue running past either end is cut off there. A cue with no time left in the stretch isn't included."),
			cloud("Add `func ReadVTT(text string) ([]Cue, error)` to vtt.go, the inverse of WriteVTT: what it wrote comes back as the same cues, apart from the tags it removed for good, and a cue placed at the top of the screen gets its tag back. A file without the proper first line, or with a block that can't be read, gives an error matching ErrMalformed."),
			local("Add `func ReviewSheet(cues []Cue) string` to report.go, tab-separated: a first row no, start, end, cps, text, then one row per cue. The number counts from 1, times are written with FormatTime, cps has one decimal and lines of text are joined with \" / \"."),
			local("Add `func FindText(cues []Cue, query string) []int` to cue.go: the indexes of cues whose text contains query, ignoring case. A phrase can run across a line break, so treat the break as a space when searching."),
		),
		Carries: []Carry{
			{
				What:        "the client's limits: 2 lines, 1000 to 7000 ms on screen, 80 ms apart, 20 characters per second with spaces counted (docs/style-guide.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{8, 11, 13, 15, 17, 21},
				Source:      "docs/style-guide.txt",
				Tests: []string{
					"TestSplitTwoLinesPerCue", "TestEnforceGapsPullsBack", "TestMergeShortAbsorbsNext", "TestMergeShortRespectsLines",
					"TestCPSCountsSpacesNotBreaks", "TestCPSSkipsSoundNotes", "TestSplitNewLimit", "TestLintTooManyLines", "TestLintDurations",
					"TestLintGapToNext", "TestLintTooFast", "TestLintOrderWithinCue",
				},
				Absent: []string{"style-guide", "style guide", "2 lines", "two lines", "80 ms", "1000 ms", "7000 ms", "20 characters", "42"},
			},
			{
				What:        "edits never change the caller's cues or share Lines with them",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{8, 11, 21, 22, 26},
				Tests:       []string{"TestReflowLeavesInput", "TestEnforceGapsLeavesInput", "TestMergeShortLeavesInput", "TestRetimeLeavesInput", "TestClipLeavesInput"},
				Absent:      []string{"caller", "undo", "share", "originals", "mutate", "in place", "copy", "clone"},
			},
			{
				What:        "a ParseError with a 1-based Block is what a bad block gives",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{23},
				Tests:       []string{"TestParseLenientCollectsErrors"},
				Absent:      []string{"parseerror", "1-based"},
			},
			{
				What:        "End is excluded, so touching cues do not overlap",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{12, 13, 26},
				Tests:       []string{"TestOverlapsTouching", "TestOverlapPairsTouching", "TestLintTouchingIsNoOverlap", "TestClipEndExcluded"},
				Absent:      []string{"first millisecond", "no longer on screen", "on screen together", "exclusive", "half-open"},
			},
			{
				What:        "character limits count code points, not bytes",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{8, 15, 17},
				Tests:       []string{"TestReflowCountsCharacters", "TestCPSCountsCharacters", "TestLintLengthCountsCharacters", "TestLintFastCountsCharacters"},
				Absent:      []string{"however it's stored", "one character", "rune", "code point", "byte", "é", "字"},
			},
			{
				What:        "dialogue cues are never re-wrapped, split or merged",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{8, 9, 21},
				Tests:       []string{"TestReflowLeavesDialogue", "TestSplitLeavesDialogue", "TestMergeShortLeavesDialogue"},
				Absent:      []string{"dialogue", "speaker", "translator", "neighbour", "re-wrap"},
			},
			{
				What:        "square brackets hold sound notes, left out of reading speed and of the version for hearing audiences",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{15, 17, 20},
				Tests:       []string{"TestCPSSkipsSoundNotes", "TestLintFastSkipsSoundNotes", "TestHearingCutRemovesSoundNotes", "TestHearingCutDropsEmptied"},
				Absent:      []string{"square bracket", "bracket", "annotation", "deaf", "door slams"},
			},
			{
				What:        "the streaming platform's file layout, timing line and text conversion (docs/vtt-export.txt)",
				Kind:        CarryDiscovered,
				Established: 10,
				Needed:      []int{18, 19, 27},
				Source:      "docs/vtt-export.txt",
				Tests:       []string{"TestVTTLayout", "TestVTTEscapes", "TestVTTTopOfScreen", "TestVTTOtherTagsRemoved", "TestVTTLeavesOutEmptied", "TestReadVTTRoundTrip"},
				Absent:      []string{"vtt-export", "line:10%", "&amp;", "&lt;", "an8", "no cue numbers"},
			},
			{
				What:        "the line limit is 37 characters, revised from the guide's 42",
				Kind:        CarryUser,
				Established: 14,
				Needed:      []int{17, 25},
				Tests:       []string{"TestReflowBreaksPastLimit", "TestSplitNewLimit", "TestLintLineLength"},
				Absent:      []string{"37", "thirty-seven", "narrower", "new player"},
			},
		},
	}
}
