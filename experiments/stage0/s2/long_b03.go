package s2

func longB03() Task {
	return Task{
		ID:     "s2-long-b03-mainframe-extract",
		Set:    "long",
		System: system("a decoder for fixed-width extract files from a mainframe"),
		Files: map[string]string{
			"go.mod": goMod,
			"layout.go": `package extract

// RecordLength is the length of a record in characters.
const RecordLength = 64

// Field is one field of a record. First and Last are column numbers, as in the spec.
type Field struct {
	Name        string
	First, Last int
}

// Width returns how many columns the field takes.
func (f Field) Width() int { return f.Last - f.First + 1 }

// Layout is the fields of one record type.
type Layout []Field

// Find returns the field with that name.
func (l Layout) Find(name string) (Field, bool) {
	for _, f := range l {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// The layouts of the three record types, still to be filled in.
var (
	HeaderLayout  = Layout{}
	DetailLayout  = Layout{}
	TrailerLayout = Layout{}
)
`,
			"date.go": `package extract

// Date is a calendar date. The zero Date means no date.
type Date struct {
	Year, Month, Day int
}

// IsZero reports whether this is no date.
func (d Date) IsZero() bool { return d == Date{} }

// Before reports whether d comes before e.
func (d Date) Before(e Date) bool {
	if d.Year != e.Year {
		return d.Year < e.Year
	}
	if d.Month != e.Month {
		return d.Month < e.Month
	}
	return d.Day < e.Day
}
`,
			"decode.go": `package extract

import "errors"

// What can be wrong with a field.
var (
	ErrLength = errors.New("wrong record length")
	ErrNumber = errors.New("bad number")
	ErrDate   = errors.New("bad date")
	ErrStatus = errors.New("bad status")
)

// Detail is one detail record. Amount is in cents.
type Detail struct {
	Account, Name string
	Amount        int64
	Posted        Date
	Branch        int
	Status        string
}

// Header is the first record of a batch.
type Header struct {
	Batch string
	Date  Date
}

// Trailer is the last record of a batch. Total is in cents.
type Trailer struct {
	Count int
	Total int64
}
`,
			"batch.go": `package extract

import "errors"

// What can make a whole batch fail.
var (
	ErrStructure      = errors.New("batch has the wrong structure")
	ErrControl        = errors.New("control totals differ")
	ErrTooManyRejects = errors.New("too many rejected details")
)

// Reject is a detail line that was not accepted.
type Reject struct {
	Line   int
	Reason string
}

// Batch is a decoded file.
type Batch struct {
	Header  Header
	Trailer Trailer
	Details []Detail
	Rejects []Reject
}

// Accepted returns how many details were accepted.
func (b *Batch) Accepted() int { return len(b.Details) }
`,
			"report.go": `package extract

import "fmt"

// Plural writes a count with its noun, such as 1 item or 3 items.
func Plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
`,
			"docs/record-spec.txt": `Extract file specification, version 4

Each line of a file is one record of exactly 64 characters. Column numbers start
at 1, and a range includes both of its ends. Column 1 is the record type.

Header (H)
  2-9    batch   text
  10-15  date    date
  16-64  spaces

Detail (D)
  2-11   account  text, always ten digits; leading zeros matter
  12-31  name     text, left-justified and padded with spaces
  32-41  amount   signed number, in cents
  42-47  posted   date
  48-51  branch   number
  52     status   A active, C closed, H held
  53-64  spaces

Trailer (T)
  2-9    count   number
  10-21  total   signed number, in cents
  22-64  spaces

Numbers are zero-padded digits. Dates are YYMMDD.

Signed numbers are zero-padded digits with the sign overpunched on the last one,
so that the last character is both the digit and the sign:

  { A B C D E F G H I   are +0 +1 +2 +3 +4 +5 +6 +7 +8 +9
  } J K L M N O P Q R   are -0 -1 -2 -3 -4 -5 -6 -7 -8 -9

Zero is always written as +0.
`,
			"docs/reject-policy.txt": `What to do with bad records

A batch file is one header line, any number of detail lines and one trailer line,
in that order. Any other shape fails the whole batch, and so does a header or
trailer that cannot be decoded.

Bad details
- A detail that cannot be decoded is rejected and the batch goes on. The reason is
  "length", "number", "date" or "status", after what was wrong with it.
- A detail with status C and an amount other than zero is rejected too, with
  reason "closed".
- A rejected detail is recorded by its line number in the file. The header is line 1.
- If more than 10% of the detail lines are rejected, the batch fails. Exactly 10%
  is fine.

Control totals
- The trailer's count is the number of detail lines in the file, rejected or not.
- The trailer's total is the sum of all amounts in the file, so it can only be
  compared when nothing was rejected.
- Any difference fails the batch.

Who gets told
- Operations reads the reject list each morning and asks the sender for a new file
  when a batch fails. Nothing here sends mail or retries by itself.
`,
			"extract_test.go": `package extract

import "testing"

func TestFieldWidth(t *testing.T) {
	if got := (Field{Name: "x", First: 3, Last: 7}).Width(); got != 5 {
		t.Fatalf("Width = %d", got)
	}
}

func TestLayoutFind(t *testing.T) {
	l := Layout{{Name: "a", First: 1, Last: 1}, {Name: "b", First: 2, Last: 4}}
	if f, ok := l.Find("b"); !ok || f.Last != 4 {
		t.Fatalf("Find = %+v, %v", f, ok)
	}
	if _, ok := l.Find("c"); ok {
		t.Fatal("found c")
	}
}

func TestDateIsZero(t *testing.T) {
	if !(Date{}).IsZero() || (Date{Year: 2024, Month: 1, Day: 1}).IsZero() {
		t.Fatal("wrong")
	}
}

func TestPluralNouns(t *testing.T) {
	if Plural(1, "item") != "1 item" || Plural(3, "item") != "3 items" || Plural(0, "item") != "0 items" {
		t.Fatal("wrong")
	}
}

func TestAcceptedCounts(t *testing.T) {
	b := &Batch{Details: []Detail{{}, {}}, Rejects: []Reject{{Line: 2, Reason: "number"}}}
	if b.Accepted() != 2 {
		t.Fatalf("Accepted = %d", b.Accepted())
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package extract_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	extract "fixture"
)

// record pads a line to its full length.
func record(line string) string { return line + strings.Repeat(" ", extract.RecordLength-len(line)) }

func detail(account, name, amount, posted, branch, status string) string {
	return record("D" + account + fmt.Sprintf("%-20s", name) + amount + posted + branch + status)
}

const (
	headerLine  = "HBATCH001240115"
	plus100     = "000001000{" // 100.00
	minus25     = "000000250}" // -25.00
	plusTotal75 = "00000000750{"
	zeroTotal   = "00000000000{"
)

func good(amount string) string {
	return detail("0000012345", "SMITH J", amount, "240115", "0012", "A")
}

// batch assembles a file from details and a trailer.
func batch(count int, total string, details ...string) string {
	lines := append([]string{record(headerLine)}, details...)
	lines = append(lines, record(fmt.Sprintf("T%08d%s", count, total)))
	return strings.Join(lines, "\n") + "\n"
}

func TestLayoutColumns(t *testing.T) {
	for _, c := range []struct {
		layout      extract.Layout
		name        string
		first, last int
	}{
		{extract.HeaderLayout, "batch", 2, 9},
		{extract.HeaderLayout, "date", 10, 15},
		{extract.DetailLayout, "account", 2, 11},
		{extract.DetailLayout, "name", 12, 31},
		{extract.DetailLayout, "amount", 32, 41},
		{extract.DetailLayout, "posted", 42, 47},
		{extract.DetailLayout, "branch", 48, 51},
		{extract.DetailLayout, "status", 52, 52},
		{extract.TrailerLayout, "count", 2, 9},
		{extract.TrailerLayout, "total", 10, 21},
	} {
		f, ok := c.layout.Find(c.name)
		if !ok || f.First != c.first || f.Last != c.last {
			t.Errorf("%s: %+v, %v", c.name, f, ok)
		}
	}
}

func TestSliceTakesColumns(t *testing.T) {
	f := extract.Field{Name: "x", First: 2, Last: 4}
	if got := f.Slice("abcdef"); got != "bcd" {
		t.Fatalf("Slice = %q", got)
	}
}

func TestMask(t *testing.T) {
	if got := extract.Mask("0000012345"); got != "******2345" {
		t.Fatalf("Mask = %q", got)
	}
	if got := extract.Mask("123"); got != "123" {
		t.Fatalf("Mask = %q", got)
	}
	if got := extract.Mask("12345"); got != "*2345" {
		t.Fatalf("Mask = %q", got)
	}
}

func TestDecodeNumber(t *testing.T) {
	if got, err := extract.DecodeNumber("0012"); err != nil || got != 12 {
		t.Fatalf("DecodeNumber = %d, %v", got, err)
	}
	for _, s := range []string{"", "  12", "1 2", "-12", "12a"} {
		if _, err := extract.DecodeNumber(s); !errors.Is(err, extract.ErrNumber) {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestDecodeDateCalendar(t *testing.T) {
	got, err := extract.DecodeDate("240229")
	if err != nil || got != (extract.Date{Year: 2024, Month: 2, Day: 29}) {
		t.Fatalf("DecodeDate = %+v, %v", got, err)
	}
	for _, s := range []string{"230229", "241301", "240100", "24011", "24a115"} {
		if _, err := extract.DecodeDate(s); !errors.Is(err, extract.ErrDate) {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestDecodeDateNoDate(t *testing.T) {
	got, err := extract.DecodeDate("000000")
	if err != nil || !got.IsZero() {
		t.Fatalf("DecodeDate = %+v, %v", got, err)
	}
}

func TestDecodeDateYearWindow(t *testing.T) {
	for s, year := range map[string]int{"000101": 2000, "590101": 2059, "600101": 1960, "991231": 1999} {
		got, err := extract.DecodeDate(s)
		if err != nil || got.Year != year {
			t.Errorf("%s: %+v, %v", s, got, err)
		}
	}
}

func TestDateString(t *testing.T) {
	if got := (extract.Date{Year: 2024, Month: 3, Day: 7}).String(); got != "2024-03-07" {
		t.Fatalf("String = %q", got)
	}
	if got := (extract.Date{}).String(); got != "" {
		t.Fatalf("String = %q", got)
	}
}

func TestFieldErrorMatches(t *testing.T) {
	err := error(&extract.FieldError{Field: "amount", Err: extract.ErrNumber})
	if !errors.Is(err, extract.ErrNumber) || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("err = %v", err)
	}
}

func TestFormatCents(t *testing.T) {
	for v, want := range map[int64]string{0: "0.00", 5: "0.05", -1234: "-12.34", 123450: "1234.50"} {
		if got := extract.FormatCents(v); got != want {
			t.Errorf("%d: %q", v, got)
		}
	}
}

func TestDecodeSigned(t *testing.T) {
	for s, want := range map[string]int64{"000{": 0, "012A": 121, "001I": 19, "000}": 0, "012J": -121, "001R": -19, "000N": -5} {
		if got, err := extract.DecodeSigned(s); err != nil || got != want {
			t.Errorf("%q: %d, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "0123", "01X", "A12{"} {
		if _, err := extract.DecodeSigned(s); !errors.Is(err, extract.ErrNumber) {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestDecodeDetailFields(t *testing.T) {
	d, err := extract.DecodeDetail(good("000001234E"))
	if err != nil {
		t.Fatal(err)
	}
	want := extract.Detail{Account: "0000012345", Name: "SMITH J", Amount: 12345,
		Posted: extract.Date{Year: 2024, Month: 1, Day: 15}, Branch: 12, Status: "A"}
	if d != want {
		t.Fatalf("DecodeDetail = %+v", d)
	}
}

func TestDecodeDetailNamesField(t *testing.T) {
	for _, c := range []struct {
		line, field string
		kind        error
	}{
		{good("00000X234E"), "amount", extract.ErrNumber},
		{detail("0000012345", "A", plus100, "241301", "0012", "A"), "posted", extract.ErrDate},
		{detail("0000012345", "A", plus100, "240115", "00X2", "A"), "branch", extract.ErrNumber},
		{detail("0000012345", "A", plus100, "240115", "0012", "Z"), "status", extract.ErrStatus},
	} {
		_, err := extract.DecodeDetail(c.line)
		var fe *extract.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field || !errors.Is(err, c.kind) {
			t.Errorf("%s: %v", c.field, err)
		}
	}
}

func TestDecodeDetailStatusBlank(t *testing.T) {
	if _, err := extract.DecodeDetail(detail("0000012345", "A", plus100, "240115", "0012", " ")); !errors.Is(err, extract.ErrStatus) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeDetailNoPostedDate(t *testing.T) {
	d, err := extract.DecodeDetail(detail("0000012345", "A", plus100, "000000", "0012", "A"))
	if err != nil || !d.Posted.IsZero() {
		t.Fatalf("DecodeDetail = %+v, %v", d, err)
	}
}

func TestDecodeDetailTrimmedLine(t *testing.T) {
	d, err := extract.DecodeDetail(strings.TrimRight(good(plus100), " "))
	if err != nil || d.Amount != 10000 || d.Status != "A" {
		t.Fatalf("DecodeDetail = %+v, %v", d, err)
	}
}

func TestDecodeDetailOverlong(t *testing.T) {
	if _, err := extract.DecodeDetail(good(plus100) + "x"); !errors.Is(err, extract.ErrLength) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeHeader(t *testing.T) {
	h, err := extract.DecodeHeader(record(headerLine))
	if err != nil || h.Batch != "BATCH001" || h.Date != (extract.Date{Year: 2024, Month: 1, Day: 15}) {
		t.Fatalf("DecodeHeader = %+v, %v", h, err)
	}
}

func TestDecodeTrailer(t *testing.T) {
	tr, err := extract.DecodeTrailer(record("T00000002" + plusTotal75))
	if err != nil || tr.Count != 2 || tr.Total != 7500 {
		t.Fatalf("DecodeTrailer = %+v, %v", tr, err)
	}
	if _, err := extract.DecodeTrailer(record("T0000000X" + plusTotal75)); !errors.Is(err, extract.ErrNumber) {
		t.Fatalf("err = %v", err)
	}
}

func TestHeaderAndTrailerTrimmedLines(t *testing.T) {
	if h, err := extract.DecodeHeader(headerLine); err != nil || h.Batch != "BATCH001" {
		t.Fatalf("DecodeHeader = %+v, %v", h, err)
	}
	if tr, err := extract.DecodeTrailer("T00000002" + plusTotal75); err != nil || tr.Total != 7500 {
		t.Fatalf("DecodeTrailer = %+v, %v", tr, err)
	}
}

func TestHeaderAndTrailerOverlong(t *testing.T) {
	if _, err := extract.DecodeHeader(record(headerLine) + "x"); !errors.Is(err, extract.ErrLength) {
		t.Fatalf("err = %v", err)
	}
	if _, err := extract.DecodeTrailer(record("T00000002"+plusTotal75) + "x"); !errors.Is(err, extract.ErrLength) {
		t.Fatalf("err = %v", err)
	}
}

func TestEncodeTextAndNumber(t *testing.T) {
	if got, err := extract.EncodeText("ab", 4); err != nil || got != "ab  " {
		t.Fatalf("EncodeText = %q, %v", got, err)
	}
	if got, err := extract.EncodeNumber(12, 4); err != nil || got != "0012" {
		t.Fatalf("EncodeNumber = %q, %v", got, err)
	}
}

func TestEncodeNeverLoses(t *testing.T) {
	if _, err := extract.EncodeText("abc", 2); !errors.Is(err, extract.ErrOverflow) {
		t.Errorf("text: %v", err)
	}
	if _, err := extract.EncodeNumber(12345, 4); !errors.Is(err, extract.ErrOverflow) {
		t.Errorf("number: %v", err)
	}
	if _, err := extract.EncodeNumber(-1, 4); !errors.Is(err, extract.ErrOverflow) {
		t.Errorf("negative number: %v", err)
	}
}

func TestEncodeSignedDigits(t *testing.T) {
	for _, c := range []struct {
		v    int64
		want string
	}{{0, "000{"}, {121, "012A"}, {-121, "012J"}, {-5, "000N"}, {19, "001I"}} {
		if got, err := extract.EncodeSigned(c.v, 4); err != nil || got != c.want {
			t.Errorf("%d: %q, %v", c.v, got, err)
		}
	}
}

func TestEncodeSignedOverflow(t *testing.T) {
	if _, err := extract.EncodeSigned(-12345, 4); !errors.Is(err, extract.ErrOverflow) {
		t.Fatalf("err = %v", err)
	}
}

func TestEncodeDateFields(t *testing.T) {
	if got, err := extract.EncodeDate(extract.Date{Year: 2024, Month: 1, Day: 15}); err != nil || got != "240115" {
		t.Fatalf("EncodeDate = %q, %v", got, err)
	}
	if got, err := extract.EncodeDate(extract.Date{Year: 1999, Month: 12, Day: 31}); err != nil || got != "991231" {
		t.Fatalf("EncodeDate = %q, %v", got, err)
	}
}

func TestEncodeDateNoDate(t *testing.T) {
	if got, err := extract.EncodeDate(extract.Date{}); err != nil || got != "000000" {
		t.Fatalf("EncodeDate = %q, %v", got, err)
	}
}

func TestEncodeDateYearWindow(t *testing.T) {
	for _, year := range []int{1960, 2059} {
		if _, err := extract.EncodeDate(extract.Date{Year: year, Month: 1, Day: 1}); err != nil {
			t.Errorf("%d: %v", year, err)
		}
	}
	for _, year := range []int{1959, 2060} {
		if _, err := extract.EncodeDate(extract.Date{Year: year, Month: 1, Day: 1}); !errors.Is(err, extract.ErrOverflow) {
			t.Errorf("%d: %v", year, err)
		}
	}
}

func TestDetailStringHidesAccount(t *testing.T) {
	d := extract.Detail{Account: "0000012345", Name: "SMITH J", Amount: 12345, Posted: extract.Date{Year: 2024, Month: 1, Day: 15}}
	if got := d.String(); got != "******2345 SMITH J 123.45 2024-01-15" {
		t.Fatalf("String = %q", got)
	}
}

func TestPostedTotalLeavesOutHeld(t *testing.T) {
	got := extract.PostedTotal([]extract.Detail{{Amount: 100, Status: "A"}, {Amount: 50, Status: "H"}, {Amount: 0, Status: "C"}, {Amount: -30, Status: "A"}})
	if got != 70 {
		t.Fatalf("PostedTotal = %d", got)
	}
}

func TestLoadBatchGood(t *testing.T) {
	b, err := extract.LoadBatch(batch(2, plusTotal75, good(plus100), good(minus25)))
	if err != nil {
		t.Fatal(err)
	}
	if b.Header.Batch != "BATCH001" || b.Trailer.Count != 2 || len(b.Details) != 2 || len(b.Rejects) != 0 || b.Details[1].Amount != -2500 {
		t.Fatalf("LoadBatch = %+v", b)
	}
}

func TestLoadBatchWrongShape(t *testing.T) {
	trailer := record("T00000000" + zeroTotal)
	for name, text := range map[string]string{
		"no trailer":     record(headerLine) + "\n" + good(plus100) + "\n",
		"no header":      good(plus100) + "\n" + trailer + "\n",
		"empty":          "",
		"bad header":     record("HBATCH001991399") + "\n" + trailer + "\n",
		"second header":  record(headerLine) + "\n" + record(headerLine) + "\n" + trailer + "\n",
		"trailer inside": record(headerLine) + "\n" + trailer + "\n" + trailer + "\n",
	} {
		if _, err := extract.LoadBatch(text); !errors.Is(err, extract.ErrStructure) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLoadBatchRejectsAndGoesOn(t *testing.T) {
	details := slices.Repeat([]string{good(plus100)}, 9)
	details[3] = good("00000X000{")
	b, err := extract.LoadBatch(batch(9, zeroTotal, details...))
	if err == nil {
		t.Fatalf("one in nine went through: %+v", b)
	}
	details = slices.Repeat([]string{good(plus100)}, 10)
	details[3] = good("00000X000{")
	b, err = extract.LoadBatch(batch(10, zeroTotal, details...))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Details) != 9 || !slices.Equal(b.Rejects, []extract.Reject{{Line: 5, Reason: "number"}}) {
		t.Fatalf("LoadBatch = %+v", b)
	}
}

func TestLoadBatchRejectReasons(t *testing.T) {
	details := slices.Repeat([]string{good(plus100)}, 40)
	details[0] = good(plus100) + "xx"
	details[1] = detail("0000012345", "A", plus100, "241301", "0012", "A")
	details[2] = detail("0000012345", "A", plus100, "240115", "0012", "Z")
	details[3] = detail("0000012345", "A", plus100, "240115", "0012", "C")
	b, err := extract.LoadBatch(batch(40, zeroTotal, details...))
	if err != nil {
		t.Fatal(err)
	}
	want := []extract.Reject{{Line: 2, Reason: "length"}, {Line: 3, Reason: "date"}, {Line: 4, Reason: "status"}, {Line: 5, Reason: "closed"}}
	if !slices.Equal(b.Rejects, want) {
		t.Fatalf("Rejects = %+v", b.Rejects)
	}
}

func TestLoadBatchClosedWithZeroPasses(t *testing.T) {
	line := detail("0000012345", "A", "000000000{", "240115", "0012", "C")
	b, err := extract.LoadBatch(batch(1, zeroTotal, line))
	if err != nil || len(b.Details) != 1 {
		t.Fatalf("LoadBatch = %+v, %v", b, err)
	}
}

func TestLoadBatchTooManyRejects(t *testing.T) {
	details := slices.Repeat([]string{good(plus100)}, 8)
	details[0] = good("00000X000{")
	if _, err := extract.LoadBatch(batch(8, zeroTotal, details...)); !errors.Is(err, extract.ErrTooManyRejects) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadBatchTrailerCount(t *testing.T) {
	if _, err := extract.LoadBatch(batch(3, plusTotal75, good(plus100), good(minus25))); !errors.Is(err, extract.ErrControl) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadBatchCountIncludesRejected(t *testing.T) {
	details := slices.Repeat([]string{good(plus100)}, 10)
	details[0] = good("00000X000{")
	if _, err := extract.LoadBatch(batch(10, zeroTotal, details...)); err != nil {
		t.Fatalf("err = %v", err)
	}
	if _, err := extract.LoadBatch(batch(9, zeroTotal, details...)); !errors.Is(err, extract.ErrControl) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadBatchTrailerTotal(t *testing.T) {
	if _, err := extract.LoadBatch(batch(2, "00000000751{", good(plus100), good(minus25))); !errors.Is(err, extract.ErrControl) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadBatchTotalIncludesHeld(t *testing.T) {
	held := detail("0000012345", "A", minus25, "240115", "0012", "H")
	if _, err := extract.LoadBatch(batch(2, plusTotal75, good(plus100), held)); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadBatchTrimmedLines(t *testing.T) {
	text := strings.ReplaceAll(batch(2, plusTotal75, good(plus100), good(minus25)), " \n", "\n")
	for strings.Contains(text, " \n") {
		text = strings.ReplaceAll(text, " \n", "\n")
	}
	b, err := extract.LoadBatch(text)
	if err != nil || len(b.Details) != 2 {
		t.Fatalf("LoadBatch = %+v, %v", b, err)
	}
}

func TestEncodeDetailRoundTrip(t *testing.T) {
	want := good("000001234E")
	d, err := extract.DecodeDetail(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := extract.EncodeDetail(d)
	if err != nil || got != want {
		t.Fatalf("EncodeDetail = %q, %v", got, err)
	}
}

func TestEncodeDetailRefusesLongName(t *testing.T) {
	d := extract.Detail{Account: "0000012345", Name: strings.Repeat("N", 21), Status: "A"}
	if _, err := extract.EncodeDetail(d); !errors.Is(err, extract.ErrOverflow) {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectLines(t *testing.T) {
	b := &extract.Batch{Rejects: []extract.Reject{{Line: 3, Reason: "number"}, {Line: 9, Reason: "closed"}}}
	if got := extract.RejectLines(b); !slices.Equal(got, []string{"line 3: number", "line 9: closed"}) {
		t.Fatalf("RejectLines = %q", got)
	}
}

func TestEncodeHeaderAndTrailer(t *testing.T) {
	h, err := extract.EncodeHeader(extract.Header{Batch: "BATCH001", Date: extract.Date{Year: 2024, Month: 1, Day: 15}})
	if err != nil || h != record(headerLine) {
		t.Fatalf("EncodeHeader = %q, %v", h, err)
	}
	tr, err := extract.EncodeTrailer(extract.Trailer{Count: 2, Total: 7500})
	if err != nil || tr != record("T00000002"+plusTotal75) {
		t.Fatalf("EncodeTrailer = %q, %v", tr, err)
	}
}

func TestRewriteDropsRejects(t *testing.T) {
	details := slices.Repeat([]string{good(plus100)}, 10)
	details[0] = good("00000X000{")
	b, err := extract.LoadBatch(batch(10, zeroTotal, details...))
	if err != nil {
		t.Fatal(err)
	}
	text, err := extract.Rewrite(b)
	if err != nil {
		t.Fatal(err)
	}
	again, err := extract.LoadBatch(text)
	if err != nil || len(again.Details) != 9 || len(again.Rejects) != 0 || again.Trailer.Total != 90000 {
		t.Fatalf("LoadBatch = %+v, %v", again, err)
	}
	if !strings.HasSuffix(text, "\n") || strings.Count(text, "\n") != 11 {
		t.Fatalf("Rewrite = %q", text)
	}
}

func TestBranchTotalsSum(t *testing.T) {
	b := &extract.Batch{Details: []extract.Detail{{Branch: 7, Amount: 100, Status: "A"}, {Branch: 3, Amount: 50, Status: "A"}, {Branch: 7, Amount: -30, Status: "A"}}}
	want := []extract.BranchTotal{{Branch: 3, Items: 1, Total: 50}, {Branch: 7, Items: 2, Total: 70}}
	if got := extract.BranchTotals(b); !slices.Equal(got, want) {
		t.Fatalf("BranchTotals = %+v", got)
	}
}

func TestBranchTotalsLeavesOutHeld(t *testing.T) {
	b := &extract.Batch{Details: []extract.Detail{{Branch: 7, Amount: 100, Status: "A"}, {Branch: 7, Amount: 50, Status: "H"}, {Branch: 3, Amount: 9, Status: "H"}}}
	want := []extract.BranchTotal{{Branch: 7, Items: 1, Total: 100}}
	if got := extract.BranchTotals(b); !slices.Equal(got, want) {
		t.Fatalf("BranchTotals = %+v", got)
	}
}

func TestBranchTotalsHeadOfficeLast(t *testing.T) {
	b := &extract.Batch{Details: []extract.Detail{{Branch: 0, Amount: 1, Status: "A"}, {Branch: 9, Amount: 1, Status: "A"}, {Branch: 2, Amount: 1, Status: "A"}}}
	got := extract.BranchTotals(b)
	if len(got) != 3 || got[0].Branch != 2 || got[1].Branch != 9 || got[2].Branch != 0 {
		t.Fatalf("BranchTotals = %+v", got)
	}
}

func TestBranchReportLines(t *testing.T) {
	b := &extract.Batch{Details: []extract.Detail{{Branch: 0, Amount: 150, Status: "A"}, {Branch: 12, Amount: 12050, Status: "A"}, {Branch: 12, Amount: 0, Status: "A"}}}
	want := "branch 12: 2 items, 120.50\nbranch 0: 1 item, 1.50\n"
	if got := extract.BranchReport(b); got != want {
		t.Fatalf("BranchReport = %q", got)
	}
}

func TestLargestDetails(t *testing.T) {
	b := &extract.Batch{Details: []extract.Detail{
		{Account: "0000000001", Name: "A", Amount: 100},
		{Account: "0000000002", Name: "B", Amount: -900},
		{Account: "0000000003", Name: "C", Amount: 500},
		{Account: "0000000004", Name: "D", Amount: 500},
	}}
	got := extract.LargestDetails(b, 3)
	if len(got) != 3 || !strings.HasPrefix(got[0], "******0002 B -9.00") || !strings.HasPrefix(got[1], "******0003") || !strings.HasPrefix(got[2], "******0004") {
		t.Fatalf("LargestDetails = %q", got)
	}
	if got := extract.LargestDetails(b, 9); len(got) != 4 {
		t.Fatalf("LargestDetails = %q", got)
	}
}

func TestReconcile(t *testing.T) {
	before := &extract.Batch{Details: []extract.Detail{{Account: "2", Amount: 100}, {Account: "2", Amount: 50}, {Account: "1", Amount: 10}, {Account: "3", Amount: 5}}}
	after := &extract.Batch{Details: []extract.Detail{{Account: "2", Amount: 150}, {Account: "1", Amount: 12}, {Account: "4", Amount: 7}}}
	want := []extract.Change{{Account: "1", Old: 10, New: 12}, {Account: "3", Old: 5, New: 0}, {Account: "4", Old: 0, New: 7}}
	if got := extract.Reconcile(before, after); !slices.Equal(got, want) {
		t.Fatalf("Reconcile = %+v", got)
	}
}

func TestChangeStringHidesAccount(t *testing.T) {
	c := extract.Change{Account: "0000012345", Old: 1000, New: -1250}
	if got := c.String(); got != "******2345: 10.00 -> -12.50" {
		t.Fatalf("String = %q", got)
	}
}
`},
		Solution: map[string]string{
			"layout.go": `package extract

import "strings"

// RecordLength is the length of a record in characters.
const RecordLength = 64

// Field is one field of a record. First and Last are column numbers, as in the spec.
type Field struct {
	Name        string
	First, Last int
}

// Width returns how many columns the field takes.
func (f Field) Width() int { return f.Last - f.First + 1 }

// Slice returns the field's text from a full record.
func (f Field) Slice(line string) string { return line[f.First-1 : f.Last] }

// Layout is the fields of one record type.
type Layout []Field

// Find returns the field with that name.
func (l Layout) Find(name string) (Field, bool) {
	for _, f := range l {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// text returns the text of a named field of a full record.
func (l Layout) text(line, name string) string {
	f, _ := l.Find(name)
	return f.Slice(line)
}

// The layouts of the three record types.
var (
	HeaderLayout = Layout{{"batch", 2, 9}, {"date", 10, 15}}
	DetailLayout = Layout{
		{"account", 2, 11},
		{"name", 12, 31},
		{"amount", 32, 41},
		{"posted", 42, 47},
		{"branch", 48, 51},
		{"status", 52, 52},
	}
	TrailerLayout = Layout{{"count", 2, 9}, {"total", 10, 21}}
)

// Mask writes an account for people to read, with only its end showing.
func Mask(account string) string {
	if len(account) <= 4 {
		return account
	}
	return strings.Repeat("*", len(account)-4) + account[len(account)-4:]
}
`,
			"date.go": `package extract

import (
	"fmt"
	"time"
)

// yearSplit is the first two-digit year in the 1900s.
const yearSplit = 60

// Date is a calendar date. The zero Date means no date.
type Date struct {
	Year, Month, Day int
}

// IsZero reports whether this is no date.
func (d Date) IsZero() bool { return d == Date{} }

// Before reports whether d comes before e.
func (d Date) Before(e Date) bool {
	if d.Year != e.Year {
		return d.Year < e.Year
	}
	if d.Month != e.Month {
		return d.Month < e.Month
	}
	return d.Day < e.Day
}

// String writes the date as YYYY-MM-DD, and no date as nothing.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// DecodeDate reads YYMMDD. All zeros is no date.
func DecodeDate(s string) (Date, error) {
	if s == "000000" {
		return Date{}, nil
	}
	n, err := DecodeNumber(s)
	if err != nil || len(s) != 6 {
		return Date{}, fmt.Errorf("%w: %q", ErrDate, s)
	}
	d := Date{Year: int(2000 + n/10000), Month: int(n / 100 % 100), Day: int(n % 100)}
	if n/10000 >= yearSplit {
		d.Year -= 100
	}
	t := time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC)
	if t.Month() != time.Month(d.Month) || t.Day() != d.Day {
		return Date{}, fmt.Errorf("%w: %q", ErrDate, s)
	}
	return d, nil
}

// EncodeDate writes YYMMDD, which holds only a hundred years.
func EncodeDate(d Date) (string, error) {
	if d.IsZero() {
		return "000000", nil
	}
	if d.Year < 1900+yearSplit || d.Year >= 2000+yearSplit {
		return "", fmt.Errorf("%w: %d", ErrOverflow, d.Year)
	}
	return fmt.Sprintf("%02d%02d%02d", d.Year%100, d.Month, d.Day), nil
}
`,
			"decode.go": `package extract

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// What can be wrong with a field.
var (
	ErrLength = errors.New("wrong record length")
	ErrNumber = errors.New("bad number")
	ErrDate   = errors.New("bad date")
	ErrStatus = errors.New("bad status")
)

// The last character of a signed number, by digit.
const (
	positiveDigits = "{ABCDEFGHI"
	negativeDigits = "}JKLMNOPQR"
)

// FieldError says which field of a record could not be decoded.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Err.Error() }

// Unwrap gives what was wrong with the field.
func (e *FieldError) Unwrap() error { return e.Err }

// Detail is one detail record. Amount is in cents.
type Detail struct {
	Account, Name string
	Amount        int64
	Posted        Date
	Branch        int
	Status        string
}

// Header is the first record of a batch.
type Header struct {
	Batch string
	Date  Date
}

// Trailer is the last record of a batch. Total is in cents.
type Trailer struct {
	Count int
	Total int64
}

// DecodeNumber reads zero-padded digits.
func DecodeNumber(s string) (int64, error) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, fmt.Errorf("%w: %q", ErrNumber, s)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrNumber, s)
	}
	return n, nil
}

// DecodeSigned reads digits whose last character also carries the sign.
func DecodeSigned(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("%w: empty", ErrNumber)
	}
	last := s[len(s)-1]
	sign, digit := int64(1), strings.IndexByte(positiveDigits, last)
	if digit < 0 {
		sign, digit = -1, strings.IndexByte(negativeDigits, last)
	}
	if digit < 0 {
		return 0, fmt.Errorf("%w: %q", ErrNumber, s)
	}
	n, err := DecodeNumber(s[:len(s)-1] + strconv.Itoa(digit))
	return sign * n, err
}

// fit gives a line its full length, as lines may arrive without their last spaces.
func fit(line string) (string, error) {
	if len(line) > RecordLength {
		return "", &FieldError{Field: "record", Err: ErrLength}
	}
	return line + strings.Repeat(" ", RecordLength-len(line)), nil
}

// DecodeDetail reads a detail record.
func DecodeDetail(line string) (Detail, error) {
	line, err := fit(line)
	if err != nil {
		return Detail{}, err
	}
	fail := func(field string, err error) (Detail, error) {
		return Detail{}, &FieldError{Field: field, Err: err}
	}
	l, d := DetailLayout, Detail{}
	d.Account = strings.TrimRight(l.text(line, "account"), " ")
	d.Name = strings.TrimRight(l.text(line, "name"), " ")
	if d.Amount, err = DecodeSigned(l.text(line, "amount")); err != nil {
		return fail("amount", err)
	}
	if d.Posted, err = DecodeDate(l.text(line, "posted")); err != nil {
		return fail("posted", err)
	}
	branch, err := DecodeNumber(l.text(line, "branch"))
	if err != nil {
		return fail("branch", err)
	}
	d.Branch, d.Status = int(branch), l.text(line, "status")
	if !strings.Contains("ACH", d.Status) {
		return fail("status", ErrStatus)
	}
	return d, nil
}

// DecodeHeader reads a header record.
func DecodeHeader(line string) (Header, error) {
	line, err := fit(line)
	if err != nil {
		return Header{}, err
	}
	date, err := DecodeDate(HeaderLayout.text(line, "date"))
	if err != nil {
		return Header{}, &FieldError{Field: "date", Err: err}
	}
	return Header{Batch: strings.TrimRight(HeaderLayout.text(line, "batch"), " "), Date: date}, nil
}

// DecodeTrailer reads a trailer record.
func DecodeTrailer(line string) (Trailer, error) {
	line, err := fit(line)
	if err != nil {
		return Trailer{}, err
	}
	count, err := DecodeNumber(TrailerLayout.text(line, "count"))
	if err != nil {
		return Trailer{}, &FieldError{Field: "count", Err: err}
	}
	total, err := DecodeSigned(TrailerLayout.text(line, "total"))
	if err != nil {
		return Trailer{}, &FieldError{Field: "total", Err: err}
	}
	return Trailer{Count: int(count), Total: total}, nil
}
`,
			"encode.go": `package extract

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrOverflow refuses a value that cannot be written in its field without losing some of it.
var ErrOverflow = errors.New("value does not fit its field")

// EncodeText writes text left-justified and padded with spaces.
func EncodeText(s string, width int) (string, error) {
	if len(s) > width {
		return "", fmt.Errorf("%w: %q", ErrOverflow, s)
	}
	return s + strings.Repeat(" ", width-len(s)), nil
}

// EncodeNumber writes a number zero-padded.
func EncodeNumber(v int64, width int) (string, error) {
	s := strconv.FormatInt(v, 10)
	if v < 0 || len(s) > width {
		return "", fmt.Errorf("%w: %d", ErrOverflow, v)
	}
	return strings.Repeat("0", width-len(s)) + s, nil
}

// EncodeSigned writes a number with the sign on its last character.
func EncodeSigned(v int64, width int) (string, error) {
	digits := positiveDigits
	if v < 0 {
		digits, v = negativeDigits, -v
	}
	s, err := EncodeNumber(v, width)
	if err != nil {
		return "", err
	}
	return s[:width-1] + string(digits[s[width-1]-'0']), nil
}

// record joins encoded fields and pads the line to its full length.
func record(kind string, fields ...string) string {
	line := kind + strings.Join(fields, "")
	return line + strings.Repeat(" ", RecordLength-len(line))
}

// EncodeDetail writes a detail record.
func EncodeDetail(d Detail) (string, error) {
	account, err1 := EncodeText(d.Account, 10)
	name, err2 := EncodeText(d.Name, 20)
	amount, err3 := EncodeSigned(d.Amount, 10)
	posted, err4 := EncodeDate(d.Posted)
	branch, err5 := EncodeNumber(int64(d.Branch), 4)
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
		return "", err
	}
	return record("D", account, name, amount, posted, branch, d.Status), nil
}

// EncodeHeader writes a header record.
func EncodeHeader(h Header) (string, error) {
	batch, err1 := EncodeText(h.Batch, 8)
	date, err2 := EncodeDate(h.Date)
	if err := errors.Join(err1, err2); err != nil {
		return "", err
	}
	return record("H", batch, date), nil
}

// EncodeTrailer writes a trailer record.
func EncodeTrailer(t Trailer) (string, error) {
	count, err1 := EncodeNumber(int64(t.Count), 8)
	total, err2 := EncodeSigned(t.Total, 12)
	if err := errors.Join(err1, err2); err != nil {
		return "", err
	}
	return record("T", count, total), nil
}

// Rewrite writes a clean file with the accepted details and a trailer to match them.
func Rewrite(b *Batch) (string, error) {
	header, err := EncodeHeader(b.Header)
	if err != nil {
		return "", err
	}
	lines, total := []string{header}, int64(0)
	for _, d := range b.Details {
		line, err := EncodeDetail(d)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
		total += d.Amount
	}
	trailer, err := EncodeTrailer(Trailer{Count: len(b.Details), Total: total})
	if err != nil {
		return "", err
	}
	return strings.Join(append(lines, trailer), "\n") + "\n", nil
}
`,
			"batch.go": `package extract

import (
	"errors"
	"fmt"
	"strings"
)

// What can make a whole batch fail.
var (
	ErrStructure      = errors.New("batch has the wrong structure")
	ErrControl        = errors.New("control totals differ")
	ErrTooManyRejects = errors.New("too many rejected details")
)

// Reject is a detail line that was not accepted.
type Reject struct {
	Line   int
	Reason string
}

// Batch is a decoded file.
type Batch struct {
	Header  Header
	Trailer Trailer
	Details []Detail
	Rejects []Reject
}

// Accepted returns how many details were accepted.
func (b *Batch) Accepted() int { return len(b.Details) }

// rejectReason names what was wrong with a detail.
func rejectReason(err error) string {
	switch {
	case errors.Is(err, ErrLength):
		return "length"
	case errors.Is(err, ErrDate):
		return "date"
	case errors.Is(err, ErrStatus):
		return "status"
	}
	return "number"
}

// addDetail decodes one line of the file, which either goes in or is rejected.
func (b *Batch) addDetail(line string, number int) {
	d, err := DecodeDetail(line)
	switch {
	case err != nil:
		b.Rejects = append(b.Rejects, Reject{Line: number, Reason: rejectReason(err)})
	case d.Status == "C" && d.Amount != 0:
		b.Rejects = append(b.Rejects, Reject{Line: number, Reason: "closed"})
	default:
		b.Details = append(b.Details, d)
	}
}

// verify applies the limit on rejects and the control totals.
func (b *Batch) verify(lines int) error {
	if len(b.Rejects)*10 > lines {
		return fmt.Errorf("%w: %d of %d", ErrTooManyRejects, len(b.Rejects), lines)
	}
	if b.Trailer.Count != lines {
		return fmt.Errorf("%w: count %d, %d lines", ErrControl, b.Trailer.Count, lines)
	}
	total := int64(0)
	for _, d := range b.Details {
		total += d.Amount
	}
	if len(b.Rejects) == 0 && b.Trailer.Total != total {
		return fmt.Errorf("%w: total %d, sum %d", ErrControl, b.Trailer.Total, total)
	}
	return nil
}

// LoadBatch reads a whole file by the policy for bad records.
func LoadBatch(text string) (*Batch, error) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	last := len(lines) - 1
	if last < 1 || !strings.HasPrefix(lines[0], "H") || !strings.HasPrefix(lines[last], "T") {
		return nil, fmt.Errorf("%w: no header and trailer", ErrStructure)
	}
	b := &Batch{}
	var err error
	if b.Header, err = DecodeHeader(lines[0]); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStructure, err)
	}
	if b.Trailer, err = DecodeTrailer(lines[last]); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStructure, err)
	}
	for i, line := range lines[1:last] {
		if !strings.HasPrefix(line, "D") {
			return nil, fmt.Errorf("%w: line %d", ErrStructure, i+2)
		}
		b.addDetail(line, i+2)
	}
	if err := b.verify(last - 1); err != nil {
		return nil, err
	}
	return b, nil
}
`,
			"report.go": `package extract

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Plural writes a count with its noun, such as 1 item or 3 items.
func Plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// FormatCents writes cents as an amount of money.
func FormatCents(v int64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// String writes the detail for logs.
func (d Detail) String() string {
	return fmt.Sprintf("%s %s %s %s", Mask(d.Account), d.Name, FormatCents(d.Amount), d.Posted)
}

// PostedTotal returns the money total of details.
func PostedTotal(details []Detail) int64 {
	total := int64(0)
	for _, d := range details {
		if d.Status == "H" {
			continue
		}
		total += d.Amount
	}
	return total
}

// RejectLines describes the rejects, one line each.
func RejectLines(b *Batch) []string {
	out := make([]string, 0, len(b.Rejects))
	for _, r := range b.Rejects {
		out = append(out, fmt.Sprintf("line %d: %s", r.Line, r.Reason))
	}
	return out
}

// BranchTotal is what one branch adds up to.
type BranchTotal struct {
	Branch, Items int
	Total         int64
}

// branchLess is the order we list branches in. Head office is last.
func branchLess(a, b int) bool {
	if a == 0 || b == 0 {
		return b == 0 && a != 0
	}
	return a < b
}

// BranchTotals adds up the money per branch.
func BranchTotals(b *Batch) []BranchTotal {
	byBranch := map[int]BranchTotal{}
	for _, d := range b.Details {
		if d.Status == "H" {
			continue
		}
		t := byBranch[d.Branch]
		t.Branch, t.Items, t.Total = d.Branch, t.Items+1, t.Total+d.Amount
		byBranch[d.Branch] = t
	}
	out := make([]BranchTotal, 0, len(byBranch))
	for _, t := range byBranch {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return branchLess(out[i].Branch, out[j].Branch) })
	return out
}

// BranchReport writes one line per branch.
func BranchReport(b *Batch) string {
	var s strings.Builder
	for _, t := range BranchTotals(b) {
		fmt.Fprintf(&s, "branch %d: %s, %s\n", t.Branch, Plural(t.Items, "item"), FormatCents(t.Total))
	}
	return s.String()
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// LargestDetails describes the n details with the largest amounts of either sign.
func LargestDetails(b *Batch, n int) []string {
	sorted := slices.Clone(b.Details)
	sort.SliceStable(sorted, func(i, j int) bool { return abs(sorted[i].Amount) > abs(sorted[j].Amount) })
	out := []string{}
	for _, d := range sorted[:min(n, len(sorted))] {
		out = append(out, d.String())
	}
	return out
}

// Change is an account whose total differs between two batches.
type Change struct {
	Account  string
	Old, New int64
}

// String writes the change for people to read.
func (c Change) String() string {
	return fmt.Sprintf("%s: %s -> %s", Mask(c.Account), FormatCents(c.Old), FormatCents(c.New))
}

func accountTotals(b *Batch) map[string]int64 {
	out := map[string]int64{}
	for _, d := range b.Details {
		out[d.Account] += d.Amount
	}
	return out
}

// Reconcile returns the accounts whose totals differ, sorted by account.
func Reconcile(before, after *Batch) []Change {
	oldTotals, newTotals := accountTotals(before), accountTotals(after)
	var out []Change
	for account, v := range oldTotals {
		if newTotals[account] != v {
			out = append(out, Change{Account: account, Old: v, New: newTotals[account]})
		}
	}
	for account, v := range newTotals {
		if _, seen := oldTotals[account]; !seen && v != 0 {
			out = append(out, Change{Account: account, New: v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return out
}
`,
		},
		Turns: script(
			cloud("Read docs/record-spec.txt and give me the essentials of the format. No code changes."),
			cloud("In layout.go, add `func (f Field) Slice(line string) string`, the field's text from a full record, and fill in `HeaderLayout`, `DetailLayout` and `TrailerLayout` from the spec, using its field names. Leave out the record type and the spaces."),
			cloud("Compliance rule: an account number never appears whole in anything this package prints for people, whether a report, a log line or a description. Only its last 4 characters show, and each one before them is written as *. Add `func Mask(account string) string` to layout.go. One of 4 characters or fewer stays as it is."),
			local("Add `func DecodeNumber(s string) (int64, error)` to decode.go: digits only, and anything else, empty included, is ErrNumber."),
			local("Add `func DecodeDate(s string) (Date, error)` to date.go, for YYMMDD. Two-digit years: 00-49 are 20xx, 50-99 are 19xx. All zeros means no date, so the zero Date and no error. Anything else that isn't a real calendar date is ErrDate."),
			local("About the transfer tool: it strips trailing spaces, so a record may arrive short and that's fine, despite what the spec says. Treat it as if it had its spaces. Longer than a record should be is still ErrLength. Nothing to write for that yet. Add `func (d Date) String() string` to date.go: YYYY-MM-DD, and nothing at all for no date."),
			cloud("Two things. A held detail (status H) stays in the batch and counts for the trailer's checks, but it's not money yet: anything that adds up money for people leaves it out. And add `type FieldError struct { Field string; Err error }` to decode.go, used as a pointer, matching its Err under errors.Is: whatever fails to decode a field of a record gives one, with Field the spec's name for it."),
			cloud("Head office is branch 0. In any listing by branch it comes last, after all the others in ascending order, however they arrive. Nothing to list yet. Add `func FormatCents(v int64) string` to report.go: cents as money, like -12.34 or 0.05."),
			local("Add `func DecodeSigned(s string) (int64, error)` to decode.go, for signed numbers as the spec writes them. Anything else is ErrNumber."),
			local("Add `func DecodeDetail(line string) (Detail, error)` to decode.go. Text fields lose their padding and keep their leading zeros. A status the spec doesn't list, a blank one included, fails on ErrStatus."),
			local("Read docs/reject-policy.txt and tell me what it asks of us. No code changes."),
			cloud("We've found old accounts: the archive goes back to the 1960s, so the two-digit years have to move. From now on a year from 60 up is in the 1900s, and anything below 60 is in the 2000s. Update the code."),
			cloud("Start encode.go, for writing records back. One rule above all: encoding never loses data, so no value is ever cut off to make it fit. Add `var ErrOverflow = errors.New(\"value does not fit its field\")` for values that don't fit, then `func EncodeText(s string, width int) (string, error)`, left-justified and padded with spaces, and `func EncodeNumber(v int64, width int) (string, error)`, zero-padded. A negative number doesn't fit either."),
			cloud("Add `func DecodeHeader(line string) (Header, error)` and `func DecodeTrailer(line string) (Trailer, error)` to decode.go, for those two records, in the same style as DecodeDetail."),
			local("Add `func EncodeSigned(v int64, width int) (string, error)` to encode.go, the inverse of DecodeSigned."),
			local("Add `func EncodeDate(d Date) (string, error)` to date.go, the inverse of DecodeDate. A date that a two-digit year can't express under our rule gets the same refusal as any other value that can't be written."),
			local("Add `func (d Detail) String() string` to report.go, a line for the logs: the account, then Name, the amount with FormatCents and the Posted date, separated by single spaces."),
			local("Add `func PostedTotal(details []Detail) int64` to report.go: the money total of these details."),
			cloud("Write `func LoadBatch(text string) (*Batch, error)` in batch.go, following the policy we have for bad records, as far as the shape of the file and the rejecting of details. A shape that's wrong gives ErrStructure, and Reject holds the line number and the reason. A trailing line break on the text is normal. We'll do limits and totals next."),
			cloud("Finish `LoadBatch` with the rest of the policy: the limit on rejects gives ErrTooManyRejects, and the trailer's checks give ErrControl."),
			local("Add `func EncodeDetail(d Detail) (string, error)` to encode.go, the inverse of DecodeDetail, always a full record."),
			local("Add `func RejectLines(b *Batch) []string` to report.go, one string per reject, exactly like: line 3: number"),
			cloud("Add `func EncodeHeader(h Header) (string, error)` and `func EncodeTrailer(t Trailer) (string, error)` to encode.go, then `func Rewrite(b *Batch) (string, error)`: a clean file with b's header, its accepted details and a trailer worked out to match them, each line a full record, and each ending in a line break."),
			local("Add `type BranchTotal struct { Branch, Items int; Total int64 }` and `func BranchTotals(b *Batch) []BranchTotal` to report.go, adding up the money per branch. A branch with nothing to add up isn't listed."),
			cloud("Add `func BranchReport(b *Batch) string` to report.go, one line per branch from BranchTotals, exactly like: branch 12: 2 items, 120.50, with Plural for the items and every line ending in a line break."),
			local("Add `func LargestDetails(b *Batch, n int) []string` to report.go: the String of the n details with the largest amounts, whichever the sign, largest first, ties in file order. Fewer if there aren't n."),
			cloud("Add `type Change struct { Account string; Old, New int64 }` and `func Reconcile(before, after *Batch) []Change` to report.go: for two batches, the accounts whose total of Amount differs, sorted by account. An account in only one of them has a total of 0 in the other."),
			local("Add `func (c Change) String() string` to report.go, a line for people, exactly in this form, where 2345 ends the account: ...2345: 10.00 -> -12.50 and the amounts use FormatCents. Treat the account as we treat it elsewhere."),
		),
		Carries: []Carry{
			{
				What:        "the record layout, and how the last character of a signed number holds digit and sign (docs/record-spec.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{8, 9, 14, 20},
				Source:      "docs/record-spec.txt",
				Tests:       []string{"TestLayoutColumns", "TestDecodeSigned", "TestEncodeSignedDigits", "TestDecodeDetailFields", "TestEncodeDetailRoundTrip"},
				Absent:      []string{"record-spec", "overpunch", "column", "+0", "-0"},
			},
			{
				What:        "account numbers only ever show their last 4 characters to people",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{16, 25, 27},
				Tests:       []string{"TestDetailStringHidesAccount", "TestLargestDetails", "TestChangeStringHidesAccount"},
				Absent:      []string{"compliance", "last 4", "last four", "mask", "appears whole"},
			},
			{
				What:        "all zeros in a date means no date",
				Kind:        CarryEmbodied,
				Established: 4,
				Needed:      []int{9, 13, 15},
				Tests:       []string{"TestDecodeDateNoDate", "TestDecodeDetailNoPostedDate", "TestEncodeDateNoDate"},
				Absent:      []string{"all zeros", "20xx", "19xx", "00-49", "50-99"},
			},
			{
				What:        "a record short of its trailing spaces is fine, and an overlong one is not",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{9, 13, 18},
				Tests:       []string{"TestDecodeDetailTrimmedLine", "TestHeaderAndTrailerTrimmedLines", "TestLoadBatchTrimmedLines"},
				Absent:      []string{"transfer tool", "trailing spaces", "strips", "arrive short"},
			},
			{
				What:        "a held detail counts for the trailer but is left out of money totals",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{17, 19, 23},
				Tests:       []string{"TestPostedTotalLeavesOutHeld", "TestBranchTotalsLeavesOutHeld"},
				Absent:      []string{"not money", "money yet", "status h", "leaves it out"},
			},
			{
				What:        "a field that fails to decode gives a FieldError with the spec's name for it",
				Kind:        CarryEmbodied,
				Established: 6,
				Needed:      []int{9, 13},
				Tests:       []string{"TestDecodeDetailNamesField"},
				Absent:      []string{"fielderror", "field error"},
			},
			{
				What:        "head office, branch 0, is listed last",
				Kind:        CarryUser,
				Established: 7,
				Needed:      []int{23},
				Tests:       []string{"TestBranchTotalsHeadOfficeLast", "TestBranchReportLines"},
				Absent:      []string{"head office", "branch 0", "comes last"},
			},
			{
				What:        "what is rejected, the limit on rejects and the control totals (docs/reject-policy.txt)",
				Kind:        CarryDiscovered,
				Established: 10,
				Needed:      []int{18, 19},
				Source:      "docs/reject-policy.txt",
				Tests: []string{
					"TestLoadBatchWrongShape", "TestLoadBatchRejectsAndGoesOn", "TestLoadBatchRejectReasons", "TestLoadBatchClosedWithZeroPasses",
					"TestLoadBatchTooManyRejects", "TestLoadBatchCountIncludesRejected", "TestRewriteDropsRejects",
				},
				Absent: []string{"reject-policy", "10%", "ten percent", "closed"},
			},
			{
				What:        "two-digit years from 60 up are in the 1900s, revised from 50",
				Kind:        CarryUser,
				Established: 11,
				Needed:      []int{15},
				Tests:       []string{"TestDecodeDateYearWindow", "TestEncodeDateYearWindow"},
				Absent:      []string{"1960", "archive", "from 60", "below 60"},
			},
			{
				What:        "encoding never cuts a value off: what does not fit is ErrOverflow",
				Kind:        CarryUser,
				Established: 12,
				Needed:      []int{14, 15, 20},
				Tests:       []string{"TestEncodeNeverLoses", "TestEncodeSignedOverflow", "TestEncodeDateYearWindow", "TestEncodeDetailRefusesLongName"},
				Absent:      []string{"erroverflow", "never loses", "cut off", "truncate"},
			},
		},
	}
}
