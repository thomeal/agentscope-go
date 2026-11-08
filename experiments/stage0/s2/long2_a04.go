package s2

func long2A04() Task {
	return Task{
		ID:     "s2-long2-a04-address-book",
		Set:    "long",
		System: system("an address book app"),
		Files: map[string]string{
			"go.mod": goMod,
			"contact.go": `package contacts

import (
	"errors"
	"sort"
	"strings"
)

// Contact is one person in the address book.
type Contact struct {
	ID       int
	Name     string
	Phones   []string
	Emails   []string
	Birthday Date
	Groups   []string
}

// ErrNotFound is returned for an ID the book does not have.
var ErrNotFound = errors.New("no such contact")

// ErrNoName is returned for a contact without a name.
var ErrNoName = errors.New("a contact needs a name")

// Book is the address book.
type Book struct {
	contacts []Contact
	lastID   int
}

// find returns the index of the contact with that ID, or -1.
func (b *Book) find(id int) int {
	for i, c := range b.contacts {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// Add stores a new contact, with its name trimmed, and returns its ID.
func (b *Book) Add(c Contact) (int, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return 0, ErrNoName
	}
	b.lastID++
	c.ID = b.lastID
	b.contacts = append(b.contacts, c)
	return c.ID, nil
}

// Get returns the contact with that ID.
func (b *Book) Get(id int) (Contact, error) {
	i := b.find(id)
	if i < 0 {
		return Contact{}, ErrNotFound
	}
	return b.contacts[i], nil
}

// Delete removes a contact.
func (b *Book) Delete(id int) error {
	i := b.find(id)
	if i < 0 {
		return ErrNotFound
	}
	b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
	return nil
}

// All returns every contact, sorted by name.
func (b *Book) All() []Contact {
	out := append([]Contact(nil), b.contacts...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
`,
			"date.go": `package contacts

import "fmt"

// Date is a calendar date. Year 0 means the year is not known, as with a
// birthday someone shared without the year. The zero Date means no date.
type Date struct {
	Year, Month, Day int
}

// IsZero reports whether the date is unset.
func (d Date) IsZero() bool { return d == Date{} }

// String writes the date as 1990-03-15, or as --03-15 without a year.
func (d Date) String() string {
	if d.Year == 0 {
		return fmt.Sprintf("--%02d-%02d", d.Month, d.Day)
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// leap reports whether year is a leap year.
func leap(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
`,
			"contact_test.go": `package contacts

import "testing"

func TestAddGetDelete(t *testing.T) {
	var b Book
	id, err := b.Add(Contact{Name: "  Ann Lee "})
	if err != nil || id != 1 {
		t.Fatalf("Add = %d, %v", id, err)
	}
	c, err := b.Get(id)
	if err != nil || c.Name != "Ann Lee" {
		t.Fatalf("Get = %+v, %v", c, err)
	}
	if _, err := b.Add(Contact{Name: " "}); err != ErrNoName {
		t.Fatalf("Add without a name = %v", err)
	}
	if err := b.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get(id); err != ErrNotFound {
		t.Fatalf("Get after Delete = %v", err)
	}
}

func TestDateString(t *testing.T) {
	if s := (Date{Year: 1990, Month: 3, Day: 15}).String(); s != "1990-03-15" {
		t.Errorf("String = %q", s)
	}
	if s := (Date{Month: 3, Day: 15}).String(); s != "--03-15" {
		t.Errorf("String without a year = %q", s)
	}
}
`,
			"docs/phones.md": `# Phone numbers

People type phone numbers in all sorts of ways. The app stores every number
in one form, and compares numbers only in that form.

## Storing a number

1. Spaces, dashes, dots and round brackets are dropped.
2. A number that starts with 00 gets a + in place of the 00.
3. A number that starts with a single 0 is a number from home, the United
   Kingdom: the 0 is replaced by +44.
4. What is left must be a + and then 8 to 15 digits. Anything else is not
   a phone number, and the app refuses it.

So "020 7946 0018", "0044 20 7946 0018" and "+44 20 7946 0018" are all
stored as +442079460018, and "+1 (555) 010-9999" is stored as +15550109999.

## Showing a number

Numbers from home are shown the way people at home dial them: a 0 in place
of the +44, and a space after the fifth digit, as in 07700 900123 or
02079 460018. Numbers from abroad are shown just as they are stored.
`,
			"docs/vcard.md": `# Contact cards

The app reads and writes contacts as cards in a simple form of vCard, which
other address books understand. A card looks like this:

    BEGIN:VCARD
    VERSION:3.0
    FN:Ann Lee
    TEL;TYPE=mobile:+447700900123
    TEL:+33612345678
    EMAIL:ann@example.com
    BDAY:1990-03-15
    CATEGORIES:family,climbing
    END:VCARD

- FN is the contact's name.
- There is a TEL line for every phone number and an EMAIL line for every
  email, in the contact's order. A number with a label has ;TYPE= and the
  label after TEL; a number without one has neither.
- BDAY is the birthday written as the app writes dates, so --03-15 when the
  year is not known. A contact without a birthday has no BDAY line.
- CATEGORIES holds the contact's groups, separated by commas. A contact in
  no group has no CATEGORIES line.

## Writing

The app writes a card for every contact in the book, in ID order, with the
lines in the order shown above. Every line ends with a carriage return and
a line feed (CR LF), and there are no blank lines between cards.

## Reading

Lines may end with CR LF or just LF, and their names (BEGIN, FN, TEL and so
on) are matched ignoring case. Lines with other names are skipped, and so is
any text outside a card. The app refuses the whole text, adding nothing, if
a card has no END, no FN, a phone number it cannot store or a BDAY that is
not a date.
`,
		},
		Hidden: map[string]string{hiddenName: `package contacts_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	contacts "fixture"
)

// crlf ends every line of s with CR LF instead of LF.
func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\x0d\n") }

func add(t *testing.T, b *contacts.Book, c contacts.Contact) int {
	t.Helper()
	id, err := b.Add(c)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func get(t *testing.T, b *contacts.Book, id int) contacts.Contact {
	t.Helper()
	c, err := b.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func born(t *testing.T, b *contacts.Book, name string, d contacts.Date) int {
	t.Helper()
	return add(t, b, contacts.Contact{Name: name, Birthday: d})
}

func day(y, m, d int) contacts.Date { return contacts.Date{Year: y, Month: m, Day: d} }

func names(cs []contacts.Contact) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func phones(numbers ...string) []contacts.Phone {
	var out []contacts.Phone
	for _, n := range numbers {
		out = append(out, contacts.Phone{Number: n})
	}
	return out
}

func TestAllByFamilyName(t *testing.T) {
	var b contacts.Book
	for _, n := range []string{"Zoe Adams", "ann lee", "Bea Adams", "Al Zane", "Ann Lee"} {
		add(t, &b, contacts.Contact{Name: n})
	}
	want := []string{"Bea Adams", "Zoe Adams", "ann lee", "Ann Lee", "Al Zane"}
	if got := names(b.All()); !slices.Equal(got, want) {
		t.Fatalf("All = %v", got)
	}
}

func TestUpdate(t *testing.T) {
	var b contacts.Book
	id := add(t, &b, contacts.Contact{Name: "Ann Lee"})
	if err := b.Update(contacts.Contact{ID: id, Name: " Ann Ray ", Phones: phones("07700 900123")}); err != nil {
		t.Fatal(err)
	}
	if c := get(t, &b, id); c.Name != "Ann Ray" || !slices.Equal(c.Phones, phones("+447700900123")) {
		t.Fatalf("after Update: %+v", c)
	}
	if err := b.Update(contacts.Contact{ID: id, Name: "Ann", Phones: phones("12345")}); !errors.Is(err, contacts.ErrBadPhone) {
		t.Errorf("Update with a bad phone = %v", err)
	}
	if get(t, &b, id).Name != "Ann Ray" {
		t.Error("a refused Update changed the contact")
	}
	if err := b.Update(contacts.Contact{ID: 99, Name: "X"}); !errors.Is(err, contacts.ErrNotFound) {
		t.Errorf("Update(99) = %v", err)
	}
}

func TestNormalizePhone(t *testing.T) {
	good := []struct{ in, want string }{
		{"020 7946 0018", "+442079460018"},
		{"0044 20-7946-0018", "+442079460018"},
		{"+44 20 7946 0018", "+442079460018"},
		{"+1 (555) 010.9999", "+15550109999"},
	}
	for _, c := range good {
		if got, err := contacts.NormalizePhone(c.in); err != nil || got != c.want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, in := range []string{"7946 0018", "+44 123", "07700 9001x3", "+1234567890123456", ""} {
		if _, err := contacts.NormalizePhone(in); !errors.Is(err, contacts.ErrBadPhone) {
			t.Errorf("NormalizePhone(%q) = %v", in, err)
		}
	}
}

func TestAddNormalizesPhones(t *testing.T) {
	var b contacts.Book
	id := add(t, &b, contacts.Contact{Name: "Ann Lee", Phones: []contacts.Phone{{Label: "mobile", Number: "07700 900123"}}})
	if c := get(t, &b, id); !slices.Equal(c.Phones, []contacts.Phone{{Label: "mobile", Number: "+447700900123"}}) {
		t.Fatalf("Phones = %+v", c.Phones)
	}
	if _, err := b.Add(contacts.Contact{Name: "Bo Ray", Phones: phones("555")}); !errors.Is(err, contacts.ErrBadPhone) {
		t.Errorf("Add with a bad phone = %v", err)
	}
	if n := len(b.All()); n != 1 {
		t.Errorf("%d contacts after a refused Add", n)
	}
}

func TestFindByPhone(t *testing.T) {
	var b contacts.Book
	add(t, &b, contacts.Contact{Name: "Cy Tan", Phones: phones("+447700900123")})
	add(t, &b, contacts.Contact{Name: "Bo Ray", Phones: phones("+33612345678")})
	add(t, &b, contacts.Contact{Name: "Ann Tan", Phones: phones("+33 6 12 34 56 78", "07700 900123")})
	if got := names(b.FindByPhone("0044 7700 900 123")); !slices.Equal(got, []string{"Ann Tan", "Cy Tan"}) {
		t.Errorf("FindByPhone = %v", got)
	}
	if got := b.FindByPhone("123"); len(got) != 0 {
		t.Errorf("FindByPhone(123) = %v", names(got))
	}
}

func TestValid(t *testing.T) {
	cases := []struct {
		d    contacts.Date
		want bool
	}{
		{day(2023, 2, 28), true},
		{day(2023, 2, 29), false},
		{day(2024, 2, 29), true},
		{day(1900, 2, 29), false},
		{day(2000, 2, 29), true},
		{day(0, 2, 29), true},
		{day(2023, 4, 31), false},
		{day(2023, 13, 1), false},
		{day(2023, 1, 0), false},
		{day(2023, 12, 31), true},
	}
	for _, c := range cases {
		if got := c.d.Valid(); got != c.want {
			t.Errorf("%v.Valid() = %v", c.d, got)
		}
	}
}

func TestSetBirthday(t *testing.T) {
	var b contacts.Book
	id := add(t, &b, contacts.Contact{Name: "Ann Lee"})
	if err := b.SetBirthday(id, day(2023, 2, 29)); !errors.Is(err, contacts.ErrBadDate) {
		t.Errorf("SetBirthday(2023-02-29) = %v", err)
	}
	if err := b.SetBirthday(id, day(0, 2, 29)); err != nil {
		t.Fatal(err)
	}
	if c := get(t, &b, id); c.Birthday != day(0, 2, 29) {
		t.Errorf("Birthday = %v", c.Birthday)
	}
	if err := b.SetBirthday(id, contacts.Date{}); err != nil || !get(t, &b, id).Birthday.IsZero() {
		t.Errorf("clearing the birthday: %v", err)
	}
	if err := b.SetBirthday(99, day(1990, 1, 1)); !errors.Is(err, contacts.ErrNotFound) {
		t.Errorf("SetBirthday(99) = %v", err)
	}
}

func TestAge(t *testing.T) {
	var b contacts.Book
	ann := born(t, &b, "Ann Lee", day(1990, 3, 15))
	cases := []struct {
		today contacts.Date
		want  int
	}{{day(2026, 3, 14), 35}, {day(2026, 3, 15), 36}, {day(2026, 12, 1), 36}}
	for _, c := range cases {
		if got, err := b.Age(ann, c.today); err != nil || got != c.want {
			t.Errorf("Age on %v = %d, %v; want %d", c.today, got, err, c.want)
		}
	}
	bo := born(t, &b, "Bo Ray", day(0, 7, 1))
	if _, err := b.Age(bo, day(2026, 1, 1)); !errors.Is(err, contacts.ErrNoYear) {
		t.Errorf("Age without a year = %v", err)
	}
}

func TestAgeLeapDay(t *testing.T) {
	var b contacts.Book
	lea := born(t, &b, "Lea Day", day(2000, 2, 29))
	if got, _ := b.Age(lea, day(2001, 2, 27)); got != 0 {
		t.Errorf("Age on 2001-02-27 = %d", got)
	}
	if got, _ := b.Age(lea, day(2001, 2, 28)); got != 1 {
		t.Errorf("Age on 2001-02-28 = %d", got)
	}
}

func TestImportVCards(t *testing.T) {
	text := crlf("Exported by Phone\nBEGIN:VCARD\nVERSION:3.0\nFN:Ann Lee\ntel;TYPE=mobile:07700 900123\nEMAIL:ann@example.com\n"+
		"BDAY:--03-15\nCATEGORIES:family,climbing\nNOTE:likes tea\nEND:VCARD\n") + "BEGIN:VCARD\nFN:Bo Ray\nTEL:+33 6 12 34 56 78\nEND:VCARD\n"
	var b contacts.Book
	if n, err := b.ImportVCards(text); err != nil || n != 2 {
		t.Fatalf("ImportVCards = %d, %v", n, err)
	}
	ann := get(t, &b, 1)
	if ann.Name != "Ann Lee" || !slices.Equal(ann.Phones, []contacts.Phone{{Label: "mobile", Number: "+447700900123"}}) ||
		!slices.Equal(ann.Emails, []string{"ann@example.com"}) || ann.Birthday != day(0, 3, 15) ||
		!slices.Equal(ann.Groups, []string{"family", "climbing"}) {
		t.Fatalf("Ann = %+v", ann)
	}
	if bo := get(t, &b, 2); bo.Name != "Bo Ray" || !slices.Equal(bo.Phones, phones("+33612345678")) {
		t.Fatalf("Bo = %+v", bo)
	}
}

func TestImportVCardsRefusesBadCard(t *testing.T) {
	for _, text := range []string{
		"BEGIN:VCARD\nTEL:+33612345678\nEND:VCARD\n",
		"BEGIN:VCARD\nFN:Ann Lee\n",
		"BEGIN:VCARD\nFN:Ann Lee\nEND:VCARD\nBEGIN:VCARD\nFN:Bo Ray\nTEL:12345\nEND:VCARD\n",
		"BEGIN:VCARD\nFN:Ann Lee\nBDAY:1990-02-30\nEND:VCARD\n",
	} {
		var b contacts.Book
		if _, err := b.ImportVCards(text); err == nil {
			t.Errorf("ImportVCards(%q) took a bad card", text)
		}
		if n := len(b.All()); n != 0 {
			t.Errorf("a refused text added %d contacts", n)
		}
	}
}

func TestAddEmail(t *testing.T) {
	var b contacts.Book
	id := add(t, &b, contacts.Contact{Name: "Ann Lee"})
	if err := b.AddEmail(id, "Ann.Lee@Example.com"); err != nil {
		t.Fatal(err)
	}
	if err := b.AddEmail(id, "ann.lee@example.COM"); !errors.Is(err, contacts.ErrDuplicateEmail) {
		t.Errorf("second AddEmail = %v", err)
	}
	if c := get(t, &b, id); !slices.Equal(c.Emails, []string{"Ann.Lee@Example.com"}) {
		t.Errorf("Emails = %v", c.Emails)
	}
	if err := b.AddEmail(99, "x@example.com"); !errors.Is(err, contacts.ErrNotFound) {
		t.Errorf("AddEmail(99) = %v", err)
	}
}

func TestArchive(t *testing.T) {
	var b contacts.Book
	ann := add(t, &b, contacts.Contact{Name: "Ann Lee"})
	add(t, &b, contacts.Contact{Name: "Bo Ray"})
	if err := b.Archive(ann); err != nil {
		t.Fatal(err)
	}
	if got := names(b.All()); !slices.Equal(got, []string{"Bo Ray"}) {
		t.Errorf("All = %v", got)
	}
	if c := get(t, &b, ann); !c.Archived {
		t.Error("Get lost the archived contact")
	}
	if err := b.Archive(99); !errors.Is(err, contacts.ErrNotFound) {
		t.Errorf("Archive(99) = %v", err)
	}
}

func TestFormatPhoneHome(t *testing.T) {
	for _, c := range []struct{ in, want string }{{"+447700900123", "07700 900123"}, {"+442079460018", "02079 460018"}} {
		if got := contacts.FormatPhone(c.in); got != c.want {
			t.Errorf("FormatPhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatPhoneAbroad(t *testing.T) {
	if got := contacts.FormatPhone("+33612345678"); got != "+33612345678" {
		t.Errorf("FormatPhone = %q", got)
	}
}

func TestGroupOrder(t *testing.T) {
	var b contacts.Book
	for _, n := range []string{"Zoe Adams", "ann lee", "Bea Adams", "Al Zane", "Cy Tan"} {
		id := add(t, &b, contacts.Contact{Name: n})
		if n == "Cy Tan" {
			continue
		}
		if err := b.AddToGroup(id, "climbing"); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"Bea Adams", "Zoe Adams", "ann lee", "Al Zane"}
	if got := names(b.Group("climbing")); !slices.Equal(got, want) {
		t.Fatalf("Group = %v", got)
	}
}

func TestAddToGroupOnce(t *testing.T) {
	var b contacts.Book
	id := add(t, &b, contacts.Contact{Name: "Ann Lee"})
	for i := 0; i < 2; i++ {
		if err := b.AddToGroup(id, "family"); err != nil {
			t.Fatal(err)
		}
	}
	if c := get(t, &b, id); !slices.Equal(c.Groups, []string{"family"}) {
		t.Errorf("Groups = %v", c.Groups)
	}
	if err := b.AddToGroup(99, "family"); !errors.Is(err, contacts.ErrNotFound) {
		t.Errorf("AddToGroup(99) = %v", err)
	}
}

func TestGroups(t *testing.T) {
	var b contacts.Book
	add(t, &b, contacts.Contact{Name: "Ann Lee", Groups: []string{"work", "family"}})
	add(t, &b, contacts.Contact{Name: "Bo Ray", Groups: []string{"climbing", "work"}})
	add(t, &b, contacts.Contact{Name: "Cy Tan"})
	if got := b.Groups(); !slices.Equal(got, []string{"climbing", "family", "work"}) {
		t.Fatalf("Groups = %v", got)
	}
}

func TestUpcoming(t *testing.T) {
	var b contacts.Book
	born(t, &b, "Ann Lee", day(1990, 1, 3))
	born(t, &b, "Bo Ray", day(1985, 12, 28))
	born(t, &b, "Cy Tan", day(0, 12, 31))
	born(t, &b, "Di Lu", day(1970, 1, 8))
	add(t, &b, contacts.Contact{Name: "Ed Fox"})
	born(t, &b, "Fay Wu", day(1992, 12, 27))
	if got := names(b.Upcoming(day(2026, 12, 28), 10)); !slices.Equal(got, []string{"Bo Ray", "Cy Tan", "Ann Lee"}) {
		t.Fatalf("Upcoming = %v", got)
	}
}

func TestUpcomingLeapDay(t *testing.T) {
	var b contacts.Book
	born(t, &b, "Lea Day", day(2000, 2, 29))
	if got := names(b.Upcoming(day(2026, 2, 25), 3)); !slices.Equal(got, []string{"Lea Day"}) {
		t.Errorf("Upcoming in 2026 = %v", got)
	}
	if got := names(b.Upcoming(day(2028, 2, 27), 2)); !slices.Equal(got, []string{"Lea Day"}) {
		t.Errorf("Upcoming in 2028 = %v", got)
	}
}

func TestUpcomingSkipsArchived(t *testing.T) {
	var b contacts.Book
	ann := born(t, &b, "Ann Lee", day(1990, 5, 1))
	born(t, &b, "Bo Ray", day(1991, 5, 1))
	if err := b.Archive(ann); err != nil {
		t.Fatal(err)
	}
	if got := names(b.Upcoming(day(2026, 5, 1), 0)); !slices.Equal(got, []string{"Bo Ray"}) {
		t.Fatalf("Upcoming = %v", got)
	}
}

func TestFindByEmailCase(t *testing.T) {
	var b contacts.Book
	add(t, &b, contacts.Contact{Name: "Ann Lee", Emails: []string{"Ann.Lee@Example.com"}})
	add(t, &b, contacts.Contact{Name: "Bo Ray", Emails: []string{"bo@example.com"}})
	if got := names(b.FindByEmail("ann.lee@example.COM")); !slices.Equal(got, []string{"Ann Lee"}) {
		t.Fatalf("FindByEmail = %v", got)
	}
}

func TestSearch(t *testing.T) {
	var b contacts.Book
	add(t, &b, contacts.Contact{Name: "Ann Lee", Emails: []string{"ann@climb.example"}})
	add(t, &b, contacts.Contact{Name: "Bo Ray", Emails: []string{"bo@CLIMB.example"}})
	add(t, &b, contacts.Contact{Name: "Cy Tan"})
	if got := names(b.Search("Climb")); !slices.Equal(got, []string{"Ann Lee", "Bo Ray"}) {
		t.Errorf("Search(Climb) = %v", got)
	}
	if got := names(b.Search("TAN")); !slices.Equal(got, []string{"Cy Tan"}) {
		t.Errorf("Search(TAN) = %v", got)
	}
}

func TestSearchOrder(t *testing.T) {
	var b contacts.Book
	for _, n := range []string{"Lee Zhou", "bo lee", "Ann Lee", "Cy Tan"} {
		add(t, &b, contacts.Contact{Name: n})
	}
	if got := names(b.Search("lee")); !slices.Equal(got, []string{"Ann Lee", "bo lee", "Lee Zhou"}) {
		t.Fatalf("Search = %v", got)
	}
}

func TestSearchSkipsArchived(t *testing.T) {
	var b contacts.Book
	ann := add(t, &b, contacts.Contact{Name: "Ann Lee"})
	add(t, &b, contacts.Contact{Name: "Bo Lee"})
	if err := b.Archive(ann); err != nil {
		t.Fatal(err)
	}
	if got := names(b.Search("lee")); !slices.Equal(got, []string{"Bo Lee"}) {
		t.Fatalf("Search = %v", got)
	}
}

func TestDuplicatesChain(t *testing.T) {
	var b contacts.Book
	add(t, &b, contacts.Contact{Name: "Ann Lee", Phones: phones("07700 900123")})
	add(t, &b, contacts.Contact{Name: "A. Lee", Phones: phones("+44 7700 900123"), Emails: []string{"ann@example.com"}})
	add(t, &b, contacts.Contact{Name: "Annie", Emails: []string{"ann@example.com"}})
	add(t, &b, contacts.Contact{Name: "Bo Ray", Phones: phones("+15550109999")})
	add(t, &b, contacts.Contact{Name: "Cy Tan", Phones: phones("0033 6 12 34 56 78")})
	add(t, &b, contacts.Contact{Name: "C. Tan", Phones: phones("+33612345678")})
	got := b.Duplicates()
	if len(got) != 2 || !slices.Equal(got[0], []int{1, 2, 3}) || !slices.Equal(got[1], []int{5, 6}) {
		t.Fatalf("Duplicates = %v", got)
	}
}

func TestDuplicatesEmailCase(t *testing.T) {
	var b contacts.Book
	add(t, &b, contacts.Contact{Name: "Kim Ito", Emails: []string{"Kim@Mail.example"}})
	add(t, &b, contacts.Contact{Name: "Bo Ray"})
	add(t, &b, contacts.Contact{Name: "K. Ito", Emails: []string{"kim@mail.EXAMPLE"}})
	if got := b.Duplicates(); len(got) != 1 || !slices.Equal(got[0], []int{1, 3}) {
		t.Fatalf("Duplicates = %v", got)
	}
}

func TestCard(t *testing.T) {
	var b contacts.Book
	id := add(t, &b, contacts.Contact{
		Name:   "Ann Lee",
		Phones: []contacts.Phone{{Label: "mobile", Number: "07700 900123"}, {Number: "+33612345678"}},
		Emails: []string{"ann@example.com"},
	})
	got, err := b.Card(id)
	if err != nil || got != "Ann Lee\nmobile: 07700 900123\n+33612345678\nann@example.com\n" {
		t.Fatalf("Card = %q, %v", got, err)
	}
	if _, err := b.Card(99); !errors.Is(err, contacts.ErrNotFound) {
		t.Errorf("Card(99) = %v", err)
	}
}

// cardBook returns a book with a contact that has every kind of card line,
// and one with a birthday but no year.
func cardBook(t *testing.T) *contacts.Book {
	t.Helper()
	b := &contacts.Book{}
	add(t, b, contacts.Contact{
		Name:     "Ann Lee",
		Phones:   []contacts.Phone{{Label: "mobile", Number: "07700 900123"}, {Number: "+33612345678"}},
		Emails:   []string{"ann@example.com"},
		Birthday: day(1990, 3, 15),
		Groups:   []string{"family", "climbing"},
	})
	born(t, b, "Bea Adams", day(0, 7, 4))
	return b
}

func TestExportVCard(t *testing.T) {
	want := crlf("BEGIN:VCARD\nVERSION:3.0\nFN:Ann Lee\nTEL;TYPE=mobile:+447700900123\nTEL:+33612345678\n" +
		"EMAIL:ann@example.com\nBDAY:1990-03-15\nCATEGORIES:family,climbing\nEND:VCARD\n" +
		"BEGIN:VCARD\nVERSION:3.0\nFN:Bea Adams\nBDAY:--07-04\nEND:VCARD\n")
	if got := cardBook(t).ExportVCards(); got != want {
		t.Fatalf("ExportVCards = %q", got)
	}
}

func TestExportReadBack(t *testing.T) {
	var other contacts.Book
	if n, err := other.ImportVCards(cardBook(t).ExportVCards()); err != nil || n != 2 {
		t.Fatalf("ImportVCards = %d, %v", n, err)
	}
	want, got := get(t, cardBook(t), 1), get(t, &other, 1)
	if got.Name != want.Name || !slices.Equal(got.Phones, want.Phones) || !slices.Equal(got.Emails, want.Emails) ||
		got.Birthday != want.Birthday || !slices.Equal(got.Groups, want.Groups) {
		t.Fatalf("read back %+v, want %+v", got, want)
	}
}

func TestMerge(t *testing.T) {
	var b contacts.Book
	keep := add(t, &b, contacts.Contact{Name: "Ann Lee", Phones: phones("07700 900123"), Groups: []string{"family"}})
	drop := add(t, &b, contacts.Contact{
		Name:     "A. Lee",
		Phones:   phones("+447700900123", "+33612345678"),
		Emails:   []string{"ann@example.com"},
		Birthday: day(1990, 3, 15),
		Groups:   []string{"family", "climbing"},
	})
	if err := b.Merge(keep, drop); err != nil {
		t.Fatal(err)
	}
	c := get(t, &b, keep)
	if !slices.Equal(c.Phones, phones("+447700900123", "+33612345678")) || !slices.Equal(c.Emails, []string{"ann@example.com"}) ||
		c.Birthday != day(1990, 3, 15) || !slices.Equal(c.Groups, []string{"family", "climbing"}) {
		t.Fatalf("after Merge: %+v", c)
	}
	if _, err := b.Get(drop); !errors.Is(err, contacts.ErrNotFound) {
		t.Errorf("Get(drop) = %v", err)
	}
	if err := b.Merge(keep, 99); !errors.Is(err, contacts.ErrNotFound) {
		t.Errorf("Merge(keep, 99) = %v", err)
	}
	if err := b.Merge(keep, keep); err != nil || len(b.All()) != 1 {
		t.Errorf("Merge(keep, keep) = %v, leaving %d contacts", err, len(b.All()))
	}
}

func TestMergeEmailCase(t *testing.T) {
	var b contacts.Book
	keep := add(t, &b, contacts.Contact{Name: "Ann Lee", Emails: []string{"Ann@Example.com"}})
	drop := add(t, &b, contacts.Contact{Name: "A. Lee", Emails: []string{"ann@example.COM", "ann@work.example"}})
	if err := b.Merge(keep, drop); err != nil {
		t.Fatal(err)
	}
	if c := get(t, &b, keep); !slices.Equal(c.Emails, []string{"Ann@Example.com", "ann@work.example"}) {
		t.Fatalf("Emails = %v", c.Emails)
	}
}

func TestMergeDuplicates(t *testing.T) {
	var b contacts.Book
	add(t, &b, contacts.Contact{Name: "Ann Lee", Phones: phones("07700 900123")})
	add(t, &b, contacts.Contact{Name: "A. Lee", Phones: phones("+447700900123"), Emails: []string{"ann@example.com"}})
	add(t, &b, contacts.Contact{Name: "Bo Ray"})
	add(t, &b, contacts.Contact{Name: "Annie", Emails: []string{"ann@example.com"}, Groups: []string{"family"}})
	if n := b.MergeDuplicates(); n != 2 {
		t.Fatalf("MergeDuplicates = %d", n)
	}
	if got := names(b.All()); !slices.Equal(got, []string{"Ann Lee", "Bo Ray"}) {
		t.Errorf("All = %v", got)
	}
	if c := get(t, &b, 1); !slices.Equal(c.Emails, []string{"ann@example.com"}) || !slices.Equal(c.Groups, []string{"family"}) {
		t.Errorf("merged contact %+v", c)
	}
}

func TestRemindersOnTheDay(t *testing.T) {
	var b contacts.Book
	born(t, &b, "Ann Lee", day(1990, 3, 15))
	born(t, &b, "Cy Tan", day(1980, 3, 20))
	if got := b.Reminders(day(2026, 3, 15)); !slices.Equal(got, []string{"Ann Lee: 2026-03-15"}) {
		t.Fatalf("Reminders = %q", got)
	}
}

func TestRemindersThreeDaysAhead(t *testing.T) {
	var b contacts.Book
	born(t, &b, "Bo Ray", day(0, 3, 18))
	born(t, &b, "Di Lu", day(1975, 3, 22))
	if got := b.Reminders(day(2026, 3, 15)); !slices.Equal(got, []string{"Bo Ray: 2026-03-18"}) {
		t.Fatalf("Reminders = %q", got)
	}
}

func TestRemindersLeapDay(t *testing.T) {
	var b contacts.Book
	born(t, &b, "Lea Day", day(2000, 2, 29))
	if got := b.Reminders(day(2027, 2, 28)); !slices.Equal(got, []string{"Lea Day: 2027-02-28"}) {
		t.Fatalf("Reminders = %q", got)
	}
}

func TestRemindersSkipArchived(t *testing.T) {
	var b contacts.Book
	ann := born(t, &b, "Ann Lee", day(1990, 3, 15))
	if err := b.Archive(ann); err != nil {
		t.Fatal(err)
	}
	if got := b.Reminders(day(2026, 3, 15)); len(got) != 0 {
		t.Fatalf("Reminders = %q", got)
	}
}

func TestRemoveFromGroup(t *testing.T) {
	var b contacts.Book
	id := add(t, &b, contacts.Contact{Name: "Ann Lee", Groups: []string{"family", "work"}})
	if err := b.RemoveFromGroup(id, "family"); err != nil {
		t.Fatal(err)
	}
	if c := get(t, &b, id); !slices.Equal(c.Groups, []string{"work"}) {
		t.Errorf("Groups = %v", c.Groups)
	}
	if err := b.RemoveFromGroup(id, "family"); !errors.Is(err, contacts.ErrNotInGroup) {
		t.Errorf("second RemoveFromGroup = %v", err)
	}
	if err := b.RemoveFromGroup(99, "work"); !errors.Is(err, contacts.ErrNotFound) {
		t.Errorf("RemoveFromGroup(99) = %v", err)
	}
}

func TestBirthdaysInMonth(t *testing.T) {
	var b contacts.Book
	born(t, &b, "Zoe Adams", day(0, 5, 10))
	born(t, &b, "ann lee", day(1990, 5, 2))
	born(t, &b, "Bea Adams", day(1980, 5, 10))
	born(t, &b, "Cy Tan", day(1980, 6, 1))
	if got := names(b.Birthdays(5)); !slices.Equal(got, []string{"ann lee", "Bea Adams", "Zoe Adams"}) {
		t.Fatalf("Birthdays = %v", got)
	}
}
`},
		Solution: map[string]string{
			"contact.go": `package contacts

import (
	"errors"
	"slices"
	"strings"
)

// Phone is one of a contact's phone numbers, with a label such as "mobile"
// or none.
type Phone struct {
	Label  string
	Number string
}

// Contact is one person in the address book.
type Contact struct {
	ID       int
	Name     string
	Phones   []Phone
	Emails   []string
	Birthday Date
	Groups   []string
	Archived bool
}

// ErrNotFound is returned for an ID the book does not have.
var ErrNotFound = errors.New("no such contact")

// ErrNoName is returned for a contact without a name.
var ErrNoName = errors.New("a contact needs a name")

// ErrDuplicateEmail is returned for an email the contact already has.
var ErrDuplicateEmail = errors.New("the contact already has that email")

// Book is the address book.
type Book struct {
	contacts []Contact
	lastID   int
}

// find returns the index of the contact with that ID, or -1.
func (b *Book) find(id int) int {
	for i, c := range b.contacts {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// clean trims a contact's name and stores its phones normalized.
func clean(c *Contact) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return ErrNoName
	}
	var phones []Phone
	for _, p := range c.Phones {
		n, err := NormalizePhone(p.Number)
		if err != nil {
			return err
		}
		phones = append(phones, Phone{Label: p.Label, Number: n})
	}
	c.Phones = phones
	return nil
}

// Add stores a new contact and returns its ID.
func (b *Book) Add(c Contact) (int, error) {
	if err := clean(&c); err != nil {
		return 0, err
	}
	b.lastID++
	c.ID = b.lastID
	b.contacts = append(b.contacts, c)
	return c.ID, nil
}

// Update replaces the contact that has c's ID.
func (b *Book) Update(c Contact) error {
	i := b.find(c.ID)
	if i < 0 {
		return ErrNotFound
	}
	if err := clean(&c); err != nil {
		return err
	}
	b.contacts[i] = c
	return nil
}

// Get returns the contact with that ID.
func (b *Book) Get(id int) (Contact, error) {
	i := b.find(id)
	if i < 0 {
		return Contact{}, ErrNotFound
	}
	return b.contacts[i], nil
}

// Delete removes a contact.
func (b *Book) Delete(id int) error {
	i := b.find(id)
	if i < 0 {
		return ErrNotFound
	}
	b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
	return nil
}

// Archive keeps a contact in the book but out of its lists.
func (b *Book) Archive(id int) error {
	i := b.find(id)
	if i < 0 {
		return ErrNotFound
	}
	b.contacts[i].Archived = true
	return nil
}

// hasEmail reports whether emails holds e, whatever the case.
func hasEmail(emails []string, e string) bool {
	return slices.ContainsFunc(emails, func(f string) bool { return strings.EqualFold(f, e) })
}

// AddEmail gives a contact another email, kept as typed.
func (b *Book) AddEmail(id int, email string) error {
	i := b.find(id)
	if i < 0 {
		return ErrNotFound
	}
	if hasEmail(b.contacts[i].Emails, email) {
		return ErrDuplicateEmail
	}
	b.contacts[i].Emails = append(b.contacts[i].Emails, email)
	return nil
}

// FindByEmail returns the contacts with that email.
func (b *Book) FindByEmail(email string) []Contact {
	var owners []Contact
	for _, c := range b.visible() {
		if hasEmail(c.Emails, email) {
			owners = append(owners, c)
		}
	}
	sortPeople(owners)
	return owners
}
`,
			"lists.go": `package contacts

import (
	"sort"
	"strings"
)

// familyKey returns what people are sorted by: the last word of the name,
// then the whole name, both ignoring case.
func familyKey(name string) (string, string) {
	lower := strings.ToLower(name)
	words := strings.Fields(lower)
	return words[len(words)-1], lower
}

// sortPeople sorts contacts the way every list in the app shows them.
func sortPeople(cs []Contact) {
	sort.SliceStable(cs, func(i, j int) bool {
		fi, ni := familyKey(cs[i].Name)
		fj, nj := familyKey(cs[j].Name)
		if fi != fj {
			return fi < fj
		}
		if ni != nj {
			return ni < nj
		}
		return cs[i].ID < cs[j].ID
	})
}

// visible returns the contacts that lists show: all but the archived ones.
func (b *Book) visible() []Contact {
	var out []Contact
	for _, c := range b.contacts {
		if !c.Archived {
			out = append(out, c)
		}
	}
	return out
}

// All returns every contact that is not archived, in the app's order.
func (b *Book) All() []Contact {
	all := b.visible()
	sortPeople(all)
	return all
}

// Search returns the contacts whose name or one of whose emails contains
// text, ignoring case.
func (b *Book) Search(text string) []Contact {
	text = strings.ToLower(text)
	var found []Contact
	pool := b.visible()
	for _, c := range pool {
		hit := strings.Contains(strings.ToLower(c.Name), text)
		for _, e := range c.Emails {
			hit = hit || strings.Contains(strings.ToLower(e), text)
		}
		if hit {
			found = append(found, c)
		}
	}
	sortPeople(found)
	return found
}

// Card shows a contact: its name, then its phones and its emails, a line
// each.
func (b *Book) Card(id int) (string, error) {
	c, err := b.Get(id)
	if err != nil {
		return "", err
	}
	lines := []string{c.Name}
	for _, p := range c.Phones {
		if p.Label == "" {
			lines = append(lines, FormatPhone(p.Number))
		} else {
			lines = append(lines, p.Label+": "+FormatPhone(p.Number))
		}
	}
	lines = append(lines, c.Emails...)
	return strings.Join(lines, "\n") + "\n", nil
}
`,
			"phone.go": `package contacts

import (
	"errors"
	"strings"
)

// ErrBadPhone is returned for something that is not a phone number.
var ErrBadPhone = errors.New("not a phone number")

// homeCode is the country code of numbers from home.
const homeCode = "+44"

// NormalizePhone returns a number in the form the app stores, as
// docs/phones.md describes.
func NormalizePhone(s string) (string, error) {
	s = strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "").Replace(s)
	switch {
	case strings.HasPrefix(s, "00"):
		s = "+" + s[2:]
	case strings.HasPrefix(s, "0"):
		s = homeCode + s[1:]
	}
	digits, ok := strings.CutPrefix(s, "+")
	if !ok || len(digits) < 8 || len(digits) > 15 {
		return "", ErrBadPhone
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", ErrBadPhone
		}
	}
	return s, nil
}

// FormatPhone shows a stored number: one from home the way people at home
// dial it, any other just as it is stored.
func FormatPhone(number string) string {
	rest, ok := strings.CutPrefix(number, homeCode)
	if !ok || len(rest) < 4 {
		return number
	}
	national := "0" + rest
	return national[:5] + " " + national[5:]
}

// FindByPhone returns the contacts with that number, however it is written.
func (b *Book) FindByPhone(number string) []Contact {
	n, err := NormalizePhone(number)
	if err != nil {
		return nil
	}
	var callers []Contact
	for _, c := range b.contacts {
		for _, p := range c.Phones {
			if p.Number == n {
				callers = append(callers, c)
				break
			}
		}
	}
	sortPeople(callers)
	return callers
}
`,
			"date.go": `package contacts

import (
	"fmt"
	"strings"
	"time"
)

// Date is a calendar date. Year 0 means the year is not known, as with a
// birthday someone shared without the year. The zero Date means no date.
type Date struct {
	Year, Month, Day int
}

// IsZero reports whether the date is unset.
func (d Date) IsZero() bool { return d == Date{} }

// String writes the date as 1990-03-15, or as --03-15 without a year.
func (d Date) String() string {
	if d.Year == 0 {
		return fmt.Sprintf("--%02d-%02d", d.Month, d.Day)
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// leap reports whether year is a leap year.
func leap(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// Valid reports whether the date exists. 29 February exists only in leap
// years, or when the year is not known.
func (d Date) Valid() bool {
	if d.Month < 1 || d.Month > 12 || d.Day < 1 {
		return false
	}
	days := [13]int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[d.Month]
	if d.Month == 2 && (d.Year == 0 || leap(d.Year)) {
		days = 29
	}
	return d.Day <= days
}

// on returns the day a birthday d is celebrated in year: 28 February for
// 29 February when year is not a leap year.
func (d Date) on(year int) Date {
	if d.Month == 2 && d.Day == 29 && !leap(year) {
		return Date{Year: year, Month: 2, Day: 28}
	}
	return Date{Year: year, Month: d.Month, Day: d.Day}
}

// before reports whether d comes before e.
func (d Date) before(e Date) bool {
	if d.Year != e.Year {
		return d.Year < e.Year
	}
	if d.Month != e.Month {
		return d.Month < e.Month
	}
	return d.Day < e.Day
}

// daysBetween returns how many days after a b is.
func daysBetween(a, b Date) int {
	t := func(d Date) time.Time { return time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC) }
	return int(t(b).Sub(t(a)).Hours() / 24)
}

// parseDate reads a date the way String writes it.
func parseDate(s string) (Date, error) {
	var d Date
	var err error
	if strings.HasPrefix(s, "--") {
		_, err = fmt.Sscanf(s, "--%2d-%2d", &d.Month, &d.Day)
	} else {
		_, err = fmt.Sscanf(s, "%4d-%2d-%2d", &d.Year, &d.Month, &d.Day)
	}
	if err != nil || !d.Valid() || d.String() != s {
		return Date{}, ErrBadDate
	}
	return d, nil
}
`,
			"birthdays.go": `package contacts

import (
	"errors"
	"sort"
)

// ErrBadDate is returned for a date that does not exist.
var ErrBadDate = errors.New("no such date")

// ErrNoYear is returned for an age without a birth year to count from.
var ErrNoYear = errors.New("birth year not known")

// earlyReminder is how many days ahead the early birthday reminder goes out.
const earlyReminder = 3

// SetBirthday sets a contact's birthday; the zero Date clears it.
func (b *Book) SetBirthday(id int, d Date) error {
	i := b.find(id)
	if i < 0 {
		return ErrNotFound
	}
	if !d.IsZero() && !d.Valid() {
		return ErrBadDate
	}
	b.contacts[i].Birthday = d
	return nil
}

// Age returns how old a contact is on today.
func (b *Book) Age(id int, today Date) (int, error) {
	c, err := b.Get(id)
	if err != nil {
		return 0, err
	}
	if c.Birthday.Year == 0 {
		return 0, ErrNoYear
	}
	age := today.Year - c.Birthday.Year
	if today.before(c.Birthday.on(today.Year)) {
		age--
	}
	return age, nil
}

// nextBirthday returns the first day on or after today that a birthday is
// celebrated.
func nextBirthday(bday, today Date) Date {
	next := bday.on(today.Year)
	if next.before(today) {
		next = bday.on(today.Year + 1)
	}
	return next
}

// Upcoming returns the contacts whose next birthday is at most days days
// away, soonest first.
func (b *Book) Upcoming(today Date, days int) []Contact {
	var soon []Contact
	people := b.visible()
	for _, c := range people {
		if !c.Birthday.IsZero() && daysBetween(today, nextBirthday(c.Birthday, today)) <= days {
			soon = append(soon, c)
		}
	}
	sortPeople(soon)
	sort.SliceStable(soon, func(i, j int) bool {
		return nextBirthday(soon[i].Birthday, today).before(nextBirthday(soon[j].Birthday, today))
	})
	return soon
}

// Reminders returns the birthday reminders that go out today, soonest
// birthday first: one on the day and one earlyReminder days ahead.
func (b *Book) Reminders(today Date) []string {
	var lines []string
	for _, c := range b.Upcoming(today, earlyReminder) {
		next := nextBirthday(c.Birthday, today)
		if d := daysBetween(today, next); d == 0 || d == earlyReminder {
			lines = append(lines, c.Name+": "+next.String())
		}
	}
	return lines
}

// Birthdays returns the contacts with a birthday in month, by day.
func (b *Book) Birthdays(month int) []Contact {
	var born []Contact
	for _, c := range b.visible() {
		if c.Birthday.Month == month {
			born = append(born, c)
		}
	}
	sortPeople(born)
	sort.SliceStable(born, func(i, j int) bool { return born[i].Birthday.Day < born[j].Birthday.Day })
	return born
}
`,
			"groups.go": `package contacts

import (
	"errors"
	"slices"
	"sort"
)

// ErrNotInGroup is returned when a contact is not in the group.
var ErrNotInGroup = errors.New("not in that group")

// AddToGroup puts a contact in a group, once.
func (b *Book) AddToGroup(id int, group string) error {
	i := b.find(id)
	if i < 0 {
		return ErrNotFound
	}
	if !slices.Contains(b.contacts[i].Groups, group) {
		b.contacts[i].Groups = append(b.contacts[i].Groups, group)
	}
	return nil
}

// RemoveFromGroup takes a contact out of a group.
func (b *Book) RemoveFromGroup(id int, group string) error {
	i := b.find(id)
	if i < 0 {
		return ErrNotFound
	}
	j := slices.Index(b.contacts[i].Groups, group)
	if j < 0 {
		return ErrNotInGroup
	}
	b.contacts[i].Groups = slices.Delete(b.contacts[i].Groups, j, j+1)
	return nil
}

// Group returns a group's members.
func (b *Book) Group(name string) []Contact {
	var members []Contact
	for _, c := range b.visible() {
		if slices.Contains(c.Groups, name) {
			members = append(members, c)
		}
	}
	sortPeople(members)
	return members
}

// Groups returns every group name some contact is in, sorted, each once.
func (b *Book) Groups() []string {
	var names []string
	for _, c := range b.contacts {
		for _, g := range c.Groups {
			if !slices.Contains(names, g) {
				names = append(names, g)
			}
		}
	}
	sort.Strings(names)
	return names
}
`,
			"merge.go": `package contacts

import (
	"slices"
	"sort"
	"strings"
)

// matchKeys returns what contacts are linked on: their phone numbers and
// their emails, which match whatever their case.
func matchKeys(c Contact) []string {
	var keys []string
	for _, p := range c.Phones {
		keys = append(keys, "tel:"+p.Number)
	}
	for _, e := range c.Emails {
		keys = append(keys, "email:"+strings.ToLower(e))
	}
	return keys
}

// leaders links the contacts that share a key and returns a function giving
// the leader of each contact's set.
func (b *Book) leaders() func(int) int {
	parent := map[int]int{}
	var root func(int) int
	root = func(id int) int {
		if p, ok := parent[id]; ok && p != id {
			return root(p)
		}
		return id
	}
	owner := map[string]int{}
	for _, c := range b.contacts {
		for _, k := range matchKeys(c) {
			if o, ok := owner[k]; ok {
				parent[root(c.ID)] = root(o)
			} else {
				owner[k] = c.ID
			}
		}
	}
	return root
}

// Duplicates returns the sets of linked contacts, each set's IDs ascending
// and the sets by their first ID.
func (b *Book) Duplicates() [][]int {
	root := b.leaders()
	sets := map[int][]int{}
	for _, c := range b.contacts {
		sets[root(c.ID)] = append(sets[root(c.ID)], c.ID)
	}
	var out [][]int
	for _, s := range sets {
		if len(s) > 1 {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// absorb gives c each phone number, email and group of o's that it lacks,
// and o's birthday if c has none.
func (c *Contact) absorb(o Contact) {
	for _, p := range o.Phones {
		if !slices.ContainsFunc(c.Phones, func(q Phone) bool { return q.Number == p.Number }) {
			c.Phones = append(c.Phones, p)
		}
	}
	for _, e := range o.Emails {
		if !hasEmail(c.Emails, e) {
			c.Emails = append(c.Emails, e)
		}
	}
	for _, g := range o.Groups {
		if !slices.Contains(c.Groups, g) {
			c.Groups = append(c.Groups, g)
		}
	}
	if c.Birthday.IsZero() {
		c.Birthday = o.Birthday
	}
}

// Merge gives keep what drop has that keep lacks, then deletes drop.
func (b *Book) Merge(keep, drop int) error {
	k, d := b.find(keep), b.find(drop)
	if k < 0 || d < 0 {
		return ErrNotFound
	}
	if keep == drop {
		return nil
	}
	b.contacts[k].absorb(b.contacts[d])
	return b.Delete(drop)
}

// MergeDuplicates merges every set Duplicates finds into its lowest ID and
// returns how many contacts it merged away.
func (b *Book) MergeDuplicates() int {
	n := 0
	for _, set := range b.Duplicates() {
		for _, id := range set[1:] {
			if b.Merge(set[0], id) == nil {
				n++
			}
		}
	}
	return n
}
`,
			"vcard.go": `package contacts

import (
	"errors"
	"sort"
	"strings"
)

// errUnended is returned for a card without an END line.
var errUnended = errors.New("card without an END line")

// crlf ends every line of a card: a carriage return and a line feed.
const crlf = "\x0d\n"

// ImportVCards adds the contacts on the cards in text, as docs/vcard.md
// describes, and returns how many it added. A bad card adds nothing.
func (b *Book) ImportVCards(text string) (int, error) {
	cards, err := readCards(text)
	if err != nil {
		return 0, err
	}
	for _, c := range cards {
		b.Add(c)
	}
	return len(cards), nil
}

// readCards reads each card in text into a checked contact. Text outside a
// card is skipped.
func readCards(text string) ([]Contact, error) {
	var cards []Contact
	var card *Contact
	for _, line := range strings.Split(strings.ReplaceAll(text, crlf, "\n"), "\n") {
		name, value, _ := strings.Cut(line, ":")
		key, params, _ := strings.Cut(name, ";")
		key = strings.ToUpper(key)
		switch {
		case key == "BEGIN" && strings.EqualFold(value, "VCARD"):
			card = &Contact{}
		case card == nil:
		case key == "END":
			if err := clean(card); err != nil {
				return nil, err
			}
			cards = append(cards, *card)
			card = nil
		default:
			if err := readLine(card, key, params, value); err != nil {
				return nil, err
			}
		}
	}
	if card != nil {
		return nil, errUnended
	}
	return cards, nil
}

// readLine reads one line of a card into c.
func readLine(c *Contact, key, params, value string) error {
	switch key {
	case "FN":
		c.Name = value
	case "TEL":
		label, _ := strings.CutPrefix(params, "TYPE=")
		c.Phones = append(c.Phones, Phone{Label: label, Number: value})
	case "EMAIL":
		c.Emails = append(c.Emails, value)
	case "BDAY":
		d, err := parseDate(value)
		if err != nil {
			return err
		}
		c.Birthday = d
	case "CATEGORIES":
		c.Groups = strings.Split(value, ",")
	}
	return nil
}

// ExportVCards writes every contact in the book as a card, in ID order.
func (b *Book) ExportVCards() string {
	cs := append([]Contact(nil), b.contacts...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].ID < cs[j].ID })
	var sb strings.Builder
	for _, c := range cs {
		writeCard(&sb, c)
	}
	return sb.String()
}

// writeCard writes one contact's card.
func writeCard(sb *strings.Builder, c Contact) {
	lines := []string{"BEGIN:VCARD", "VERSION:3.0", "FN:" + c.Name}
	for _, p := range c.Phones {
		if p.Label != "" {
			lines = append(lines, "TEL;TYPE="+p.Label+":"+p.Number)
		} else {
			lines = append(lines, "TEL:"+p.Number)
		}
	}
	for _, e := range c.Emails {
		lines = append(lines, "EMAIL:"+e)
	}
	if !c.Birthday.IsZero() {
		lines = append(lines, "BDAY:"+c.Birthday.String())
	}
	if len(c.Groups) > 0 {
		lines = append(lines, "CATEGORIES:"+strings.Join(c.Groups, ","))
	}
	for _, l := range append(lines, "END:VCARD") {
		sb.WriteString(l + crlf)
	}
}
`,
		},
		Turns: script(
			cloud("Start by reading contact.go. Whenever the app lists people, they go by family name: the last word of the name, then the whole name, both ignoring case, then ID. Move All into a new lists.go, where the app's lists will live, and make it sort that way."),
			local("Add `func (b *Book) Update(c Contact) error` to contact.go: it replaces the contact that has c's ID, checking and trimming the name as Add does. An unknown ID gives ErrNotFound."),
			cloud("Read docs/phones.md. Create phone.go with `func NormalizePhone(s string) (string, error)`, which returns a number in the form the app stores, or a new ErrBadPhone. Then make Add and Update store every phone that way and refuse a contact with a bad one."),
			cloud("Add `func (b *Book) FindByPhone(number string) []Contact` to phone.go: the contacts with that number, however it's written."),
			local("Add `func (d Date) Valid() bool` to date.go: the month is 1 to 12 and the day exists in that month. 29 February only exists in leap years, but a date without a year may have it."),
			local("Create birthdays.go with `func (b *Book) SetBirthday(id int, d Date) error`: a date that isn't Valid gives a new ErrBadDate, and the zero Date clears the birthday. One thing for everything about birthdays from now on: people born on 29 February celebrate on 28 February in years that aren't leap years."),
			local("Add `func (b *Book) Age(id int, today Date) (int, error)` to birthdays.go: how old the contact is on that day. A birthday without a year, or no birthday at all, gives a new ErrNoYear. And for later, when we get to birthday reminders: one goes out a week ahead and another on the day itself."),
			cloud("Read docs/vcard.md, then add `func (b *Book) ImportVCards(text string) (int, error)` in a new vcard.go: it adds the contacts on the cards in text and returns how many it added."),
			local("Add `func (b *Book) AddEmail(id int, email string) error` to contact.go. It keeps the email as typed, but a contact can't have the same email twice; that gives a new ErrDuplicateEmail. Email addresses count as the same whatever their case, everywhere in the app."),
			local("Add an `Archived bool` field to Contact and `func (b *Book) Archive(id int) error`. Archived contacts stay in the book and Get still finds them, but All leaves them out, and so must every list, search or reminder we add later."),
			local("Add `func FormatPhone(number string) string` to phone.go: a stored number the way the app shows it."),
			local("Create groups.go with `func (b *Book) AddToGroup(id int, group string) error`, which puts a contact in a group just once however often it's called, and `func (b *Book) Group(name string) []Contact`: that group's members."),
			cloud("Phones get labels such as \"mobile\" or \"home\". Add `type Phone struct { Label, Number string }`, make Contact.Phones a []Phone, and update every file that deals in phones. ImportVCards should take each phone's label from its card."),
			local("Add `func (b *Book) Groups() []string` to groups.go: every group name that some contact is in, sorted, each once. Also a change of plan to pass on for birthday reminders: the early one goes out 3 days ahead now, not 7. Nothing to build for that yet."),
			cloud("Add `func (b *Book) Upcoming(today Date, days int) []Contact` to birthdays.go: the contacts whose next birthday is at most days days away, today's being 0 days away, soonest first."),
			cloud("Add `func (b *Book) FindByEmail(email string) []Contact` to contact.go: the contacts that have that email."),
			local("Add `func (b *Book) Search(text string) []Contact` to lists.go: the contacts whose name, or one of whose emails, contains text, ignoring case."),
			cloud("Create merge.go with `func (b *Book) Duplicates() [][]int`: sets of contacts that look like one person. Two contacts are linked when they share a phone number or an email, and links chain: if A is linked to B and B to C, all three are one set. List each set's IDs in ascending order and the sets by their first ID, leaving out contacts with no link."),
			local("Add `func (b *Book) Card(id int) (string, error)` to lists.go: the contact as the app shows it, every line ending in a newline. First the name, then a line per phone with its label, a colon and a space before the number as the app shows numbers (just the number if there's no label), then a line per email. An unknown ID gives ErrNotFound."),
			local("Add `func (b *Book) ExportVCards() string` to vcard.go: every contact in the book written as cards that ImportVCards, or another address book, can read."),
			cloud("Add `func (b *Book) Merge(keep, drop int) error` to merge.go: keep gets each phone number, email and group of drop's that it doesn't have yet, and drop's birthday if it has none; then drop is deleted. An unknown ID gives ErrNotFound, and merging a contact with itself does nothing."),
			cloud("Add `func (b *Book) MergeDuplicates() int` to merge.go: it merges every set that Duplicates finds into the set's lowest ID, and returns how many contacts were merged away."),
			cloud("Add `func (b *Book) Reminders(today Date) []string` to birthdays.go: the birthday reminders that go out today, soonest birthday first, one line each with the name, a colon, a space and the date of the birthday this time, like \"Ann Lee: 2026-03-15\"."),
			local("Add `func (b *Book) RemoveFromGroup(id int, group string) error` to groups.go: it takes the contact out of the group. An unknown contact gives ErrNotFound, and one that isn't in the group gives a new ErrNotInGroup."),
			local("Add `func (b *Book) Birthdays(month int) []Contact` to birthdays.go: the contacts with a birthday in that month, by day of the month."),
		),
		Carries: []Carry{
			{
				What:        "people are listed by family name (the last word), then the whole name ignoring case, then ID",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{11, 16, 24},
				Tests:       []string{"TestGroupOrder", "TestSearchOrder", "TestBirthdaysInMonth"},
				Absent:      []string{"family name", "last word", "surname"},
			},
			{
				What:        "a number from home shows with a 0 for +44 and a space after the fifth digit (docs/phones.md)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{10, 18},
				Source:      "docs/phones.md",
				Tests:       []string{"TestFormatPhoneHome", "TestCard"},
				Absent:      []string{"fifth digit", "a space after", "in place of the", "dial"},
			},
			{
				What:        "someone born on 29 February celebrates on 28 February outside leap years",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{14, 22},
				Tests:       []string{"TestUpcomingLeapDay", "TestRemindersLeapDay"},
				Absent:      []string{"28 february", "celebrate"},
			},
			{
				What:        "cards are written in ID order with VERSION, TYPE labels and CR LF line ends (docs/vcard.md)",
				Kind:        CarryDiscovered,
				Established: 7,
				Needed:      []int{19},
				Source:      "docs/vcard.md",
				Tests:       []string{"TestExportVCard", "TestExportReadBack"},
				Absent:      []string{"version:3.0", "cr lf", "type=", "id order", "carriage return"},
			},
			{
				What:        "email addresses are the same whatever their case",
				Kind:        CarryUser,
				Established: 8,
				Needed:      []int{15, 17, 20},
				Tests:       []string{"TestFindByEmailCase", "TestDuplicatesEmailCase", "TestMergeEmailCase"},
				Absent:      []string{"whatever their case", "count as the same", "same whatever"},
			},
			{
				What:        "birthday reminders go out ahead of the birthday and on the day itself",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{22},
				Tests:       []string{"TestRemindersOnTheDay", "TestRemindersLeapDay"},
				Absent:      []string{"a week ahead", "on the day", "day itself"},
			},
			{
				What:        "the early birthday reminder goes out 3 days ahead (revises a week)",
				Kind:        CarryUser,
				Established: 13,
				Needed:      []int{22},
				Tests:       []string{"TestRemindersThreeDaysAhead"},
				Absent:      []string{"3 days ahead", "three days", "early one"},
			},
			{
				What:        "archived contacts stay out of every list, search and reminder added later",
				Kind:        CarryUser,
				Established: 9,
				Needed:      []int{14, 16, 22},
				Tests:       []string{"TestUpcomingSkipsArchived", "TestSearchSkipsArchived", "TestRemindersSkipArchived"},
				Absent:      []string{"leaves them out", "every list", "or reminder we add"},
			},
		},
	}
}
