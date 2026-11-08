package s2

func long2A02() Task {
	return Task{
		ID:     "s2-long2-a02-flashcards",
		Set:    "long",
		System: system("a flashcard study app"),
		Files: map[string]string{
			"go.mod": goMod,
			"card.go": `package cards

import "errors"

// Day is a day number: day 1 is the first day the app was used.
type Day int

// Card is one flashcard in a deck. Interval is the days until it comes up
// again and Due the day it does; a new card has Interval 0.
type Card struct {
	ID       int
	Deck     string
	Front    string
	Back     string
	Interval int
	Due      Day
}

// ErrNoCard is returned for an ID that matches no card.
var ErrNoCard = errors.New("no such card")

// Box holds every card, across all decks.
type Box struct {
	cards []Card
}

// find returns the index of the card with that ID, or -1.
func (b *Box) find(id int) int {
	for i, c := range b.cards {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// Add adds a new card to a deck and returns its ID.
func (b *Box) Add(deck, front, back string) (int, error) {
	id := len(b.cards) + 1
	b.cards = append(b.cards, Card{ID: id, Deck: deck, Front: front, Back: back})
	return id, nil
}

// Get returns the card with that ID.
func (b *Box) Get(id int) (Card, error) {
	i := b.find(id)
	if i < 0 {
		return Card{}, ErrNoCard
	}
	return b.cards[i], nil
}
`,
			"deck.go": `package cards

// Cards returns a copy of a deck's cards in ID order.
func (b *Box) Cards(deck string) []Card {
	var out []Card
	for _, c := range b.cards {
		if c.Deck == deck {
			out = append(out, c)
		}
	}
	return out
}
`,
			"review.go": `package cards

// Review records whether a card was remembered on a day. A remembered card
// waits twice as long as before, at least a day; a forgotten one comes back
// the next day.
func (b *Box) Review(id int, remembered bool, today Day) error {
	i := b.find(id)
	if i < 0 {
		return ErrNoCard
	}
	c := &b.cards[i]
	if remembered {
		c.Interval = max(2*c.Interval, 1)
	} else {
		c.Interval = 1
	}
	c.Due = today + Day(c.Interval)
	return nil
}
`,
			"card_test.go": `package cards

import "testing"

func TestAddGetCards(t *testing.T) {
	var b Box
	first, err := b.Add("Spanish", "hola", "hello")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := b.Add("French", "oui", "yes")
	third, _ := b.Add("Spanish", "adiós", "goodbye")
	if first == second || second == third || first == third {
		t.Fatalf("IDs %d, %d, %d", first, second, third)
	}
	c, err := b.Get(third)
	if err != nil || c.Front != "adiós" || c.Deck != "Spanish" || c.Interval != 0 {
		t.Fatalf("Get = %+v, %v", c, err)
	}
	if got := b.Cards("Spanish"); len(got) != 2 || got[0].ID != first || got[1].ID != third {
		t.Fatalf("Cards = %+v", got)
	}
}

func TestGetUnknown(t *testing.T) {
	var b Box
	if _, err := b.Get(7); err == nil {
		t.Fatal("Get(7) found a card in an empty box")
	}
}
`,
			"docs/scheduling.md": `# Scheduling

Every card has an interval in days and an ease, kept as a whole percentage:
an ease of 250 makes each Good answer stretch the interval 2.5 times. A new
card has interval 0 and ease 250, and it is not due for review until it has
been answered once.

## Grades

A review gives one of four grades: Again, Hard, Good or Easy. The new
interval uses the ease the card had before the review.

- Good: the interval is multiplied by ease / 100.
- Easy: the interval is multiplied by ease / 100 and then by 13 / 10, and
  the ease goes up by 15.
- Hard: the interval is multiplied by 12 / 10, and the ease goes down by 15.
- Again: the card has lapsed. Its interval goes back to 1 day and its ease
  goes down by 20.

The ease never goes below 130.

## New cards

A new card cannot lapse. Answered Again, Hard or Good it gets an interval of
1 day, and Easy gives it 4 days. Its ease changes as above.

## Rounding and limits

Intervals are whole days: round the result to the nearest day, a half day
up. After Hard, Good or Easy on a card that is not new, the interval is at
least one day longer than it was, and never longer than 365 days.

## Due day

A card reviewed on day D with a new interval of N days is due on day D + N.

## Example

A new card answered Good on day 10 gets an interval of 1 day and is due on
day 11. Answered Good again on day 11, its interval becomes 1 x 250 / 100,
which is 2.5 and rounds to 3, so it is due on day 14. Its ease stays 250
all the while.
`,
			"docs/import-format.md": `# Deck import format

People keep decks in plain text files, one card per line:

    front | back | tags

- Fields are separated by a vertical bar. Spaces around a field do not
  count, so "hola|hello" and "hola  |  hello" are the same card.
- The tags field is optional. Tags in it are separated by spaces.
- A blank line, or a line starting with "#", is skipped. People use "#"
  lines for notes.
- To put a vertical bar inside a field, write it as "\|".

A line with an empty front or back, or with more than three fields, is an
error. An import with an error adds no cards at all, and its error message
says which line is wrong, counting from 1, in the form "line 7".

## Writing the format

A program writing a deck in this format puts " | " between fields, leaves
out the tags field when a card has no tags, separates tags with single
spaces, writes a bar inside a field as "\|", and ends every line with a
newline.

## Example

    # Spanish, week 1
    hola | hello | greeting
    adiós | goodbye | greeting
    sí|yes

    a \| b | either a or b | logic

This file holds four cards. The last one's front is "a | b".
`,
		},
		Hidden: map[string]string{hiddenName: `package cards_test

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	cards "fixture"
)

// box returns a box with n new cards in a deck, with IDs 1 to n.
func box(t *testing.T, deck string, n int) *cards.Box {
	t.Helper()
	b := &cards.Box{}
	add(t, b, deck, n)
	return b
}

func add(t *testing.T, b *cards.Box, deck string, n int) {
	t.Helper()
	for i := range n {
		if _, err := b.Add(deck, "front "+strconv.Itoa(i), "back "+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
}

func review(t *testing.T, b *cards.Box, id int, day cards.Day, grades ...cards.Grade) {
	t.Helper()
	for _, g := range grades {
		if err := b.Review(id, g, day); err != nil {
			t.Fatalf("Review(%d): %v", id, err)
		}
	}
}

func card(t *testing.T, b *cards.Box, id int) cards.Card {
	t.Helper()
	c, err := b.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAddRejectsEmpty(t *testing.T) {
	b := &cards.Box{}
	if _, err := b.Add("Spanish", "  ", "hello"); !errors.Is(err, cards.ErrEmpty) {
		t.Errorf("blank front: %v", err)
	}
	if _, err := b.Add("Spanish", "hola", ""); !errors.Is(err, cards.ErrEmpty) {
		t.Errorf("empty back: %v", err)
	}
	if id, err := b.Add("Spanish", "hola", "hello"); err != nil || id != 1 {
		t.Errorf("Add = %d, %v", id, err)
	}
}

func TestIDsNotReused(t *testing.T) {
	b := box(t, "Spanish", 3)
	if err := b.Delete(2); err != nil {
		t.Fatal(err)
	}
	id, err := b.Add("Spanish", "hola", "hello")
	if err != nil || id != 4 {
		t.Fatalf("Add after Delete = %d, %v", id, err)
	}
	if _, err := b.Get(2); !errors.Is(err, cards.ErrNoCard) {
		t.Errorf("Get(2) = %v", err)
	}
}

func TestIDsNotReusedAfterLast(t *testing.T) {
	b := box(t, "Spanish", 2)
	if err := b.Delete(2); err != nil {
		t.Fatal(err)
	}
	if id, _ := b.Add("Spanish", "hola", "hello"); id != 3 {
		t.Fatalf("Add after deleting the newest card = %d", id)
	}
}

func TestDecksAndRename(t *testing.T) {
	b := box(t, "Spanish", 2)
	add(t, b, "French", 1)
	add(t, b, "German", 1)
	if got := b.Decks(); !slices.Equal(got, []string{"French", "German", "Spanish"}) {
		t.Errorf("Decks = %q", got)
	}
	if n := b.RenameDeck("German", "Deutsch"); n != 1 {
		t.Errorf("RenameDeck = %d", n)
	}
	if err := b.Move(1, "French"); err != nil {
		t.Fatal(err)
	}
	if err := b.Move(99, "French"); !errors.Is(err, cards.ErrNoCard) {
		t.Errorf("Move(99) = %v", err)
	}
	var ids []int
	for _, c := range b.Cards("French") {
		ids = append(ids, c.ID)
	}
	if !slices.Equal(ids, []int{1, 3}) || !slices.Equal(b.Decks(), []string{"Deutsch", "French", "Spanish"}) {
		t.Fatalf("French %v, decks %q", ids, b.Decks())
	}
}

func TestReviewGoodAndEasy(t *testing.T) {
	b := box(t, "Spanish", 2)
	review(t, b, 1, 10, cards.Good)
	review(t, b, 2, 10, cards.Easy)
	if c := card(t, b, 1); c.Interval != 1 || c.Ease != 250 || c.Due != 11 {
		t.Errorf("new card, Good: %+v", c)
	}
	if c := card(t, b, 2); c.Interval != 4 || c.Ease != 265 || c.Due != 14 {
		t.Errorf("new card, Easy: %+v", c)
	}
	review(t, b, 1, 11, cards.Good)
	review(t, b, 2, 14, cards.Easy)
	if c := card(t, b, 1); c.Interval != 3 || c.Ease != 250 || c.Due != 14 {
		t.Errorf("second Good: %+v", c)
	}
	if c := card(t, b, 2); c.Interval != 14 || c.Ease != 280 || c.Due != 28 {
		t.Errorf("second Easy: %+v", c)
	}
}

func TestIntervalCap(t *testing.T) {
	b := box(t, "Spanish", 1)
	for i, want := range []int{4, 14, 51, 196, 365} {
		review(t, b, 1, 1, cards.Easy)
		if c := card(t, b, 1); c.Interval != want {
			t.Fatalf("after %d Easy answers the interval is %d, want %d", i+1, c.Interval, want)
		}
	}
}

func TestBadGrade(t *testing.T) {
	b := box(t, "Spanish", 1)
	for _, g := range []cards.Grade{0, 5} {
		if err := b.Review(1, g, 1); !errors.Is(err, cards.ErrBadGrade) {
			t.Errorf("Review(grade %d) = %v", g, err)
		}
	}
	if cards.Again != 1 || cards.Easy != 4 {
		t.Errorf("Again = %d, Easy = %d", cards.Again, cards.Easy)
	}
}

func TestReviewHard(t *testing.T) {
	b := box(t, "Spanish", 2)
	review(t, b, 1, 1, cards.Good, cards.Good, cards.Good)
	review(t, b, 1, 9, cards.Hard)
	if c := card(t, b, 1); c.Interval != 10 || c.Ease != 235 || c.Due != 19 {
		t.Errorf("Hard on an 8-day card: %+v", c)
	}
	review(t, b, 2, 1, cards.Hard)
	if c := card(t, b, 2); c.Interval != 1 || c.Ease != 235 {
		t.Errorf("Hard on a new card: %+v", c)
	}
}

func TestReviewAgain(t *testing.T) {
	b := box(t, "Spanish", 1)
	review(t, b, 1, 1, cards.Good, cards.Good, cards.Good)
	review(t, b, 1, 9, cards.Again)
	if c := card(t, b, 1); c.Interval != 1 || c.Ease != 230 || c.Due != 10 || c.Lapses != 1 {
		t.Fatalf("Again on an 8-day card: %+v", c)
	}
}

func TestEaseFloor(t *testing.T) {
	b := box(t, "Spanish", 1)
	review(t, b, 1, 1, cards.Again, cards.Again, cards.Again, cards.Again, cards.Hard, cards.Hard, cards.Hard)
	if c := card(t, b, 1); c.Ease != 130 || c.Lapses != 3 {
		t.Fatalf("after many failures: %+v", c)
	}
}

func TestDueOrder(t *testing.T) {
	b := box(t, "Spanish", 4)
	review(t, b, 1, 1, cards.Good)
	review(t, b, 2, 1, cards.Good)
	review(t, b, 3, 0, cards.Good)
	if got := b.Due("Spanish", 3); !slices.Equal(got, []int{3, 1, 2}) {
		t.Errorf("Due(3) = %v", got)
	}
	if got := b.Due("Spanish", 1); !slices.Equal(got, []int{3}) {
		t.Errorf("Due(1) = %v", got)
	}
}

func TestSuspendLeavesDue(t *testing.T) {
	b := box(t, "Spanish", 2)
	review(t, b, 1, 1, cards.Good)
	if err := b.Suspend(1); err != nil {
		t.Fatal(err)
	}
	if got := b.Due("Spanish", 2); len(got) != 0 {
		t.Errorf("Due while suspended = %v", got)
	}
	if err := b.Unsuspend(1); err != nil {
		t.Fatal(err)
	}
	if got := b.Due("Spanish", 2); !slices.Equal(got, []int{1}) {
		t.Errorf("Due after Unsuspend = %v", got)
	}
	if err := b.Suspend(9); !errors.Is(err, cards.ErrNoCard) {
		t.Errorf("Suspend(9) = %v", err)
	}
}

func TestLeechAtFourthLapse(t *testing.T) {
	b := box(t, "Spanish", 1)
	review(t, b, 1, 1, cards.Good, cards.Again, cards.Again, cards.Again)
	if c := card(t, b, 1); c.Suspended || slices.Contains(c.Tags, "leech") {
		t.Fatalf("a leech after three lapses: %+v", c)
	}
	review(t, b, 1, 2, cards.Again)
	if c := card(t, b, 1); !c.Suspended || !slices.Contains(c.Tags, "leech") || c.Lapses != 4 {
		t.Fatalf("after the fourth lapse: %+v", c)
	}
}

func TestLeechLeavesDue(t *testing.T) {
	b := box(t, "Spanish", 2)
	review(t, b, 1, 1, cards.Good, cards.Again, cards.Again, cards.Again, cards.Again)
	review(t, b, 2, 1, cards.Good)
	if got := b.Due("Spanish", 5); !slices.Equal(got, []int{2}) {
		t.Fatalf("Due = %v", got)
	}
}

func TestEditAndUnknownCard(t *testing.T) {
	b := box(t, "Spanish", 1)
	if err := b.Edit(1, "hola", " "); !errors.Is(err, cards.ErrEmpty) {
		t.Errorf("Edit with a blank back = %v", err)
	}
	if err := b.Edit(1, "hola", "hi"); err != nil {
		t.Fatal(err)
	}
	if c := card(t, b, 1); c.Front != "hola" || c.Back != "hi" {
		t.Errorf("after Edit: %+v", c)
	}
	if err := b.Edit(7, "a", "b"); !errors.Is(err, cards.ErrNoCard) {
		t.Errorf("Edit(7) = %v", err)
	}
	if err := b.Delete(7); !errors.Is(err, cards.ErrNoCard) {
		t.Errorf("Delete(7) = %v", err)
	}
	if err := b.Tag(7, "verb"); !errors.Is(err, cards.ErrNoCard) {
		t.Errorf("Tag(7) = %v", err)
	}
}

func TestTagOnceLowerCase(t *testing.T) {
	b := box(t, "Spanish", 1)
	for _, tag := range []string{"Verb", "verb", "Irregular"} {
		if err := b.Tag(1, tag); err != nil {
			t.Fatal(err)
		}
	}
	if c := card(t, b, 1); !slices.Equal(c.Tags, []string{"verb", "irregular"}) {
		t.Fatalf("Tags = %q", c.Tags)
	}
}

func TestHistoryOldestFirst(t *testing.T) {
	b := box(t, "Spanish", 2)
	review(t, b, 1, 1, cards.Good)
	review(t, b, 2, 1, cards.Easy)
	review(t, b, 1, 2, cards.Easy)
	h := b.History(1)
	if len(h) != 2 || h[0].Day != 1 || h[0].Grade != cards.Good || h[1].Day != 2 || h[1].Grade != cards.Easy || h[1].ID != 1 {
		t.Fatalf("History(1) = %+v", h)
	}
}

func TestCardErrorNamesCard(t *testing.T) {
	b := box(t, "Spanish", 1)
	_, err := b.Get(42)
	var ce *cards.CardError
	if !errors.As(err, &ce) || ce.ID != 42 || !errors.Is(err, cards.ErrNoCard) {
		t.Errorf("Get(42) = %v", err)
	}
	err = b.Review(43, cards.Good, 1)
	if !errors.As(err, &ce) || ce.ID != 43 {
		t.Errorf("Review(43) = %v", err)
	}
}

func TestUndo(t *testing.T) {
	b := box(t, "Spanish", 1)
	review(t, b, 1, 1, cards.Good)
	review(t, b, 1, 2, cards.Easy)
	if err := b.Undo(); err != nil {
		t.Fatal(err)
	}
	if c := card(t, b, 1); c.Interval != 1 || c.Ease != 250 || c.Due != 2 || len(b.History(1)) != 1 {
		t.Errorf("after one Undo: %+v", c)
	}
	if err := b.Undo(); err != nil {
		t.Fatal(err)
	}
	if c := card(t, b, 1); c.Interval != 0 || c.Due != 0 || len(b.History(1)) != 0 {
		t.Errorf("after two: %+v", c)
	}
	if err := b.Undo(); !errors.Is(err, cards.ErrNothingToUndo) {
		t.Errorf("third Undo = %v", err)
	}
}

func TestSessionReviewsThenNew(t *testing.T) {
	b := box(t, "Spanish", 4)
	review(t, b, 3, 1, cards.Good)
	review(t, b, 4, 1, cards.Good)
	if got := b.Session("Spanish", 2); !slices.Equal(got, []int{3, 4, 1, 2}) {
		t.Fatalf("Session = %v", got)
	}
}

func TestSessionNewLimit(t *testing.T) {
	b := box(t, "Spanish", 25)
	got := b.Session("Spanish", 1)
	if len(got) != 20 || got[0] != 1 || got[19] != 20 {
		t.Fatalf("Session = %v", got)
	}
}

func TestTodayCounts(t *testing.T) {
	b := box(t, "Spanish", 27)
	review(t, b, 26, 1, cards.Good)
	review(t, b, 27, 1, cards.Good)
	if got := b.Today("Spanish", 2); got != "Spanish: 2 due, 20 new" {
		t.Fatalf("Today = %q", got)
	}
}

func TestForecast(t *testing.T) {
	b := box(t, "Spanish", 6)
	review(t, b, 1, 0, cards.Good)
	review(t, b, 2, 1, cards.Good)
	review(t, b, 3, 1, cards.Easy)
	review(t, b, 4, 1, cards.Good)
	review(t, b, 4, 2, cards.Good)
	review(t, b, 6, 1, cards.Good)
	if err := b.Suspend(6); err != nil {
		t.Fatal(err)
	}
	if got := b.Forecast("Spanish", 2, 4); !slices.Equal(got, []int{2, 0, 0, 2}) {
		t.Fatalf("Forecast = %v", got)
	}
}

func TestSearch(t *testing.T) {
	b := &cards.Box{}
	b.Add("Spanish", "Hola", "hello")
	b.Add("Spanish", "adiós", "goodbye")
	b.Add("French", "bonjour", "Hello there")
	if got := b.Search("HELLO"); !slices.Equal(got, []int{1, 3}) {
		t.Fatalf("Search = %v", got)
	}
}

func TestLeechesList(t *testing.T) {
	b := box(t, "Spanish", 3)
	add(t, b, "French", 1)
	b.Tag(2, "leech")
	b.Tag(4, "leech")
	b.Tag(1, "verb")
	if got := b.Leeches("Spanish"); !slices.Equal(got, []int{2}) {
		t.Fatalf("Leeches = %v", got)
	}
}

func TestImportSkipsCommentsAndBlank(t *testing.T) {
	b := &cards.Box{}
	n, err := b.Import("Spanish", "# Spanish basics\n\nhola | hello\n   \nadiós|goodbye\n")
	if err != nil || n != 2 {
		t.Fatalf("Import = %d, %v", n, err)
	}
	got := b.Cards("Spanish")
	if len(got) != 2 || got[0].Front != "hola" || got[0].Back != "hello" || got[1].Front != "adiós" || got[1].Back != "goodbye" {
		t.Fatalf("cards %+v", got)
	}
}

func TestImportEscapedBar(t *testing.T) {
	b := &cards.Box{}
	if _, err := b.Import("Logic", "a \\| b | c\n"); err != nil {
		t.Fatal(err)
	}
	if got := b.Cards("Logic"); len(got) != 1 || got[0].Front != "a | b" || got[0].Back != "c" {
		t.Fatalf("cards %+v", got)
	}
}

func TestImportFailsWhole(t *testing.T) {
	b := box(t, "Spanish", 1)
	n, err := b.Import("Spanish", "uno | one\n | two\ntres | three\n")
	if err == nil || !strings.Contains(err.Error(), "line 2") || n != 0 {
		t.Fatalf("Import = %d, %v", n, err)
	}
	if got := b.Cards("Spanish"); len(got) != 1 {
		t.Fatalf("%d cards after a failed import", len(got))
	}
}

func TestImportTags(t *testing.T) {
	b := &cards.Box{}
	if _, err := b.Import("Spanish", "hola | hello | Greeting Basics greeting\n"); err != nil {
		t.Fatal(err)
	}
	if got := b.Cards("Spanish"); len(got) != 1 || !slices.Equal(got[0].Tags, []string{"greeting", "basics"}) {
		t.Fatalf("cards %+v", got)
	}
}

func TestExport(t *testing.T) {
	b := &cards.Box{}
	b.Add("Spanish", "hola", "hello")
	b.Tag(1, "Greeting")
	b.Add("Spanish", "adiós", "goodbye")
	b.Add("French", "oui", "yes")
	b.Add("Spanish", "a | b", "c")
	want := "hola | hello | greeting\nadiós | goodbye\na \\| b | c\n"
	if got := b.Export("Spanish"); got != want {
		t.Fatalf("Export = %q, want %q", got, want)
	}
}

func TestRetentionSkipsFirstReview(t *testing.T) {
	b := box(t, "Spanish", 2)
	add(t, b, "French", 1)
	review(t, b, 1, 1, cards.Again)
	review(t, b, 2, 1, cards.Good)
	review(t, b, 1, 2, cards.Good)
	review(t, b, 2, 2, cards.Again)
	review(t, b, 3, 2, cards.Again)
	review(t, b, 1, 3, cards.Good)
	review(t, b, 3, 3, cards.Again)
	if got := b.Retention("Spanish", 1, 3); got != 66 {
		t.Errorf("Retention = %d, want 66", got)
	}
	if got := b.Retention("Spanish", 3, 3); got != 100 {
		t.Errorf("Retention on day 3 = %d", got)
	}
}

func TestRetentionOnlyFirstReviews(t *testing.T) {
	b := box(t, "Spanish", 2)
	review(t, b, 1, 1, cards.Good)
	review(t, b, 2, 1, cards.Easy)
	if got := b.Retention("Spanish", 1, 5); got != 0 {
		t.Fatalf("Retention = %d", got)
	}
}

func TestMature(t *testing.T) {
	b := box(t, "Spanish", 3)
	review(t, b, 1, 1, cards.Easy, cards.Easy, cards.Easy)
	review(t, b, 2, 1, cards.Good, cards.Good, cards.Good, cards.Good)
	if n := b.Mature("Spanish"); n != 1 {
		t.Errorf("Mature = %d", n)
	}
	review(t, b, 2, 2, cards.Good)
	if n := b.Mature("Spanish"); n != 2 {
		t.Errorf("Mature = %d", n)
	}
}

func TestCram(t *testing.T) {
	b := box(t, "Spanish", 5)
	review(t, b, 1, 1, cards.Easy)
	review(t, b, 2, 1, cards.Good)
	review(t, b, 4, 1, cards.Easy, cards.Easy)
	for id := 1; id <= 4; id++ {
		b.Tag(id, "verb")
	}
	b.Suspend(2)
	if got := b.Cram("Spanish", "verb"); !slices.Equal(got, []int{2, 3, 1, 4}) {
		t.Fatalf("Cram = %v", got)
	}
}

func TestReviewSuspendedFails(t *testing.T) {
	b := box(t, "Spanish", 1)
	review(t, b, 1, 1, cards.Good)
	b.Suspend(1)
	if err := b.Review(1, cards.Easy, 2); !errors.Is(err, cards.ErrSuspended) {
		t.Fatalf("Review = %v", err)
	}
	if c := card(t, b, 1); c.Interval != 1 || c.Due != 2 || len(b.History(1)) != 1 {
		t.Fatalf("card %+v, history %d", c, len(b.History(1)))
	}
}

func TestPauseDeck(t *testing.T) {
	b := box(t, "Spanish", 2)
	add(t, b, "French", 1)
	review(t, b, 1, 1, cards.Good)
	b.PauseDeck("Spanish")
	if due, s := b.Due("Spanish", 2), b.Session("Spanish", 2); len(due) != 0 || len(s) != 0 {
		t.Errorf("paused: Due %v, Session %v", due, s)
	}
	if got := b.Session("French", 2); !slices.Equal(got, []int{3}) {
		t.Errorf("French Session = %v", got)
	}
	b.ResumeDeck("Spanish")
	if got := b.Due("Spanish", 2); !slices.Equal(got, []int{1}) {
		t.Errorf("Due after ResumeDeck = %v", got)
	}
}

func TestReverse(t *testing.T) {
	b := box(t, "Spanish", 1)
	b.Edit(1, "hola", "hello")
	b.Tag(1, "verb")
	id, err := b.Reverse(1)
	if err != nil || id != 2 {
		t.Fatalf("Reverse = %d, %v", id, err)
	}
	if c := card(t, b, 2); c.Front != "hello" || c.Back != "hola" || c.Deck != "Spanish" || !slices.Equal(c.Tags, []string{"verb"}) {
		t.Fatalf("reversed card %+v", c)
	}
	if _, err := b.Reverse(9); !errors.Is(err, cards.ErrNoCard) {
		t.Errorf("Reverse(9) = %v", err)
	}
}

func TestDuplicates(t *testing.T) {
	b := &cards.Box{}
	for _, front := range []string{"Hola", "adiós", " hola ", "Adiós", "gracias"} {
		b.Add("Spanish", front, "x")
	}
	b.Add("French", "hola", "x")
	got := b.Duplicates("Spanish")
	if len(got) != 2 || !slices.Equal(got[0], []int{1, 3}) || !slices.Equal(got[1], []int{2, 4}) {
		t.Fatalf("Duplicates = %v", got)
	}
}

func TestSaveLoad(t *testing.T) {
	b := box(t, "Spanish", 3)
	add(t, b, "French", 1)
	review(t, b, 1, 1, cards.Good, cards.Easy)
	b.Tag(2, "verb")
	b.Suspend(3)
	b.PauseDeck("French")
	data, err := b.Save()
	if err != nil {
		t.Fatal(err)
	}
	back, err := cards.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	if c := card(t, back, 1); c.Interval != 3 || c.Ease != 265 || c.Due != 4 {
		t.Errorf("card 1 after Load: %+v", c)
	}
	if c := card(t, back, 2); !slices.Equal(c.Tags, []string{"verb"}) {
		t.Errorf("card 2 after Load: %+v", c)
	}
	if got := back.Due("Spanish", 5); !slices.Equal(got, []int{1}) {
		t.Errorf("Due after Load = %v", got)
	}
	if got := back.Session("Spanish", 5); len(got) != 2 {
		t.Errorf("Session after Load = %v", got)
	}
	if got := back.Session("French", 5); len(got) != 0 {
		t.Errorf("French is no longer paused: %v", got)
	}
}

func TestIDsNotReusedAfterLoad(t *testing.T) {
	b := box(t, "Spanish", 3)
	if err := b.Delete(3); err != nil {
		t.Fatal(err)
	}
	data, err := b.Save()
	if err != nil {
		t.Fatal(err)
	}
	back, err := cards.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := back.Add("Spanish", "hola", "hello"); id != 4 {
		t.Fatalf("Add after Load = %d", id)
	}
}

func TestReset(t *testing.T) {
	b := box(t, "Spanish", 1)
	review(t, b, 1, 1, cards.Good, cards.Easy)
	b.Tag(1, "leech")
	b.Tag(1, "verb")
	b.Suspend(1)
	if err := b.Reset(1); err != nil {
		t.Fatal(err)
	}
	c := card(t, b, 1)
	if c.Interval != 0 || c.Ease != 250 || c.Due != 0 || c.Lapses != 0 || c.Suspended || !slices.Equal(c.Tags, []string{"verb"}) {
		t.Fatalf("after Reset: %+v", c)
	}
	if err := b.Reset(9); !errors.Is(err, cards.ErrNoCard) {
		t.Errorf("Reset(9) = %v", err)
	}
}
`},
		Solution: map[string]string{
			"card.go": `package cards

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Day is a day number: day 1 is the first day the app was used.
type Day int

// Card is one flashcard in a deck. Interval is the days until it comes up
// again and Due the day it does; a new card has Interval 0. Ease and Lapses
// follow docs/scheduling.md.
type Card struct {
	ID        int
	Deck      string
	Front     string
	Back      string
	Tags      []string
	Interval  int
	Ease      int
	Due       Day
	Lapses    int
	Suspended bool
}

// ErrNoCard is what every error for an ID that matches no card matches.
var ErrNoCard = errors.New("no such card")

// ErrEmpty is returned for a card side that is empty or only spaces.
var ErrEmpty = errors.New("empty card side")

// CardError is the error for an ID that matches no card.
type CardError struct {
	ID int
}

// Error says which card is missing.
func (e *CardError) Error() string { return fmt.Sprintf("no card %d", e.ID) }

// Is makes a CardError match ErrNoCard.
func (e *CardError) Is(target error) bool { return target == ErrNoCard }

func noCard(id int) error { return &CardError{ID: id} }

// Box holds every card, across all decks.
type Box struct {
	cards   []Card
	nextID  int
	history []Entry
	paused  map[string]bool
}

// find returns the index of the card with that ID, or -1.
func (b *Box) find(id int) int {
	for i, c := range b.cards {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// blank reports whether a card side has nothing but spaces.
func blank(s string) bool { return strings.TrimSpace(s) == "" }

// Add adds a new card to a deck and returns its ID. An ID is given out only
// once, even after its card is deleted.
func (b *Box) Add(deck, front, back string) (int, error) {
	if blank(front) || blank(back) {
		return 0, ErrEmpty
	}
	b.nextID++
	id := b.nextID
	b.cards = append(b.cards, Card{ID: id, Deck: deck, Front: front, Back: back, Ease: startEase})
	return id, nil
}

// Get returns the card with that ID.
func (b *Box) Get(id int) (Card, error) {
	i := b.find(id)
	if i < 0 {
		return Card{}, noCard(id)
	}
	return b.cards[i], nil
}

// Edit replaces both sides of a card.
func (b *Box) Edit(id int, front, back string) error {
	i := b.find(id)
	if i < 0 {
		return noCard(id)
	}
	if blank(front) || blank(back) {
		return ErrEmpty
	}
	b.cards[i].Front, b.cards[i].Back = front, back
	return nil
}

// Delete removes a card.
func (b *Box) Delete(id int) error {
	i := b.find(id)
	if i < 0 {
		return noCard(id)
	}
	b.cards = slices.Delete(b.cards, i, i+1)
	return nil
}

// Suspend keeps a card out of reviews until it is unsuspended.
func (b *Box) Suspend(id int) error { return b.setSuspended(id, true) }

// Unsuspend lets a suspended card come up for review again.
func (b *Box) Unsuspend(id int) error { return b.setSuspended(id, false) }

func (b *Box) setSuspended(id int, on bool) error {
	i := b.find(id)
	if i < 0 {
		return noCard(id)
	}
	b.cards[i].Suspended = on
	return nil
}

// Tag gives a card a tag, kept lower-case and only once.
func (b *Box) Tag(id int, tag string) error {
	i := b.find(id)
	if i < 0 {
		return noCard(id)
	}
	b.cards[i].Tags = addTag(b.cards[i].Tags, tag)
	return nil
}

// addTag returns tags with tag added in lower case, unless it is there.
func addTag(tags []string, tag string) []string {
	tag = strings.ToLower(tag)
	if slices.Contains(tags, tag) {
		return tags
	}
	return append(tags, tag)
}

// Reverse adds a card to the same deck with the sides of another swapped,
// and the same tags, and returns the new card's ID.
func (b *Box) Reverse(id int) (int, error) {
	i := b.find(id)
	if i < 0 {
		return 0, noCard(id)
	}
	c := b.cards[i]
	rid, err := b.Add(c.Deck, c.Back, c.Front)
	if err != nil {
		return 0, err
	}
	b.cards[len(b.cards)-1].Tags = slices.Clone(c.Tags)
	return rid, nil
}
`,
			"deck.go": `package cards

import (
	"sort"
	"strings"
)

// Cards returns a copy of a deck's cards in ID order.
func (b *Box) Cards(deck string) []Card {
	var out []Card
	for _, c := range b.cards {
		if c.Deck == deck {
			out = append(out, c)
		}
	}
	return out
}

// Decks returns the names of the decks that have cards, sorted.
func (b *Box) Decks() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range b.cards {
		if !seen[c.Deck] {
			seen[c.Deck] = true
			out = append(out, c.Deck)
		}
	}
	sort.Strings(out)
	return out
}

// RenameDeck moves every card of one deck to another and returns how many
// moved.
func (b *Box) RenameDeck(from, to string) int {
	n := 0
	for i := range b.cards {
		if b.cards[i].Deck == from {
			b.cards[i].Deck = to
			n++
		}
	}
	return n
}

// Move puts one card in another deck.
func (b *Box) Move(id int, deck string) error {
	i := b.find(id)
	if i < 0 {
		return noCard(id)
	}
	b.cards[i].Deck = deck
	return nil
}

// Search returns the IDs of the cards in any deck whose front or back
// contains query, ignoring case, in ID order.
func (b *Box) Search(query string) []int {
	q := strings.ToLower(query)
	var out []int
	for _, c := range b.cards {
		if strings.Contains(strings.ToLower(c.Front), q) || strings.Contains(strings.ToLower(c.Back), q) {
			out = append(out, c.ID)
		}
	}
	return out
}

// PauseDeck keeps a deck out of Due and Session until it is resumed.
func (b *Box) PauseDeck(deck string) {
	if b.paused == nil {
		b.paused = map[string]bool{}
	}
	b.paused[deck] = true
}

// ResumeDeck undoes PauseDeck.
func (b *Box) ResumeDeck(deck string) { delete(b.paused, deck) }

// Duplicates returns the groups of a deck's cards whose fronts match,
// ignoring case and the spaces around them. Each group is in ID order, the
// groups go by their first ID, and only groups of two or more count.
func (b *Box) Duplicates(deck string) [][]int {
	groups := map[string][]int{}
	var order []string
	for _, c := range b.cards {
		if c.Deck != deck {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(c.Front))
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], c.ID)
	}
	var out [][]int
	for _, k := range order {
		if len(groups[k]) > 1 {
			out = append(out, groups[k])
		}
	}
	return out
}
`,
			"store.go": `package cards

import (
	"encoding/json"
	"sort"
)

// saved is what Save writes: everything a Box holds except the review
// history.
type saved struct {
	Cards  []Card
	NextID int
	Paused []string
}

// Save writes the box as JSON.
func (b *Box) Save() ([]byte, error) {
	s := saved{Cards: b.cards, NextID: b.nextID}
	for deck := range b.paused {
		s.Paused = append(s.Paused, deck)
	}
	sort.Strings(s.Paused)
	return json.Marshal(s)
}

// Load reads a box written by Save.
func Load(data []byte) (*Box, error) {
	var s saved
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	b := &Box{cards: s.Cards, nextID: s.NextID}
	for _, deck := range s.Paused {
		b.PauseDeck(deck)
	}
	return b, nil
}
`,
			"review.go": `package cards

import (
	"errors"
	"slices"
)

// Grade is how well a card was remembered at a review.
type Grade int

// The grades, from worst to best.
const (
	Again Grade = iota + 1
	Hard
	Good
	Easy
)

// ErrBadGrade is returned for a grade that is not one of the four.
var ErrBadGrade = errors.New("bad grade")

// ErrSuspended is returned for a review of a suspended card.
var ErrSuspended = errors.New("card is suspended")

// ErrNothingToUndo is returned by Undo when there is no review to take back.
var ErrNothingToUndo = errors.New("nothing to undo")

const (
	startEase   = 250
	minEase     = 130
	maxInterval = 365
	leechAt     = 4
)

// easeChange is how much each grade moves a card's ease.
var easeChange = map[Grade]int{Again: -20, Hard: -15, Easy: 15}

// Entry is one review in the history.
type Entry struct {
	ID    int
	Day   Day
	Grade Grade

	before Card // the card as it was before the review, for Undo
}

// next works out a reviewed card's new interval and ease, as
// docs/scheduling.md says.
func next(interval, ease int, g Grade) (int, int) {
	n := 1
	if interval == 0 && g == Easy {
		n = 4
	} else if interval > 0 && g != Again {
		n = stretch(interval, ease, g)
	}
	return n, max(ease+easeChange[g], minEase)
}

// stretch is the new interval of a card that is not new after Hard, Good or
// Easy, using the ease it had before the review.
func stretch(interval, ease int, g Grade) int {
	var n int
	switch g {
	case Hard:
		n = (interval*12 + 5) / 10
	case Good:
		n = (interval*ease + 50) / 100
	case Easy:
		n = (interval*ease*13 + 500) / 1000
	}
	return min(max(n, interval+1), maxInterval)
}

// Review records a review of a card on a day and schedules its next one.
func (b *Box) Review(id int, g Grade, today Day) error {
	if g < Again || g > Easy {
		return ErrBadGrade
	}
	i := b.find(id)
	if i < 0 {
		return noCard(id)
	}
	c := &b.cards[i]
	if c.Suspended {
		return ErrSuspended
	}
	before := *c
	before.Tags = slices.Clone(c.Tags)
	b.history = append(b.history, Entry{ID: id, Day: today, Grade: g, before: before})
	if g == Again && c.Interval > 0 {
		c.Lapses++
		if c.Lapses == leechAt {
			c.Suspended = true
			c.Tags = addTag(c.Tags, "leech")
		}
	}
	c.Interval, c.Ease = next(c.Interval, c.Ease, g)
	c.Due = today + Day(c.Interval)
	return nil
}

// History returns a card's reviews, oldest first.
func (b *Box) History(id int) []Entry {
	var out []Entry
	for _, e := range b.history {
		if e.ID == id {
			out = append(out, e)
		}
	}
	return out
}

// Undo takes back the most recent review: its card goes back to how it was
// and the review leaves the history.
func (b *Box) Undo() error {
	if len(b.history) == 0 {
		return ErrNothingToUndo
	}
	last := b.history[len(b.history)-1]
	b.history = b.history[:len(b.history)-1]
	if i := b.find(last.ID); i >= 0 {
		b.cards[i] = last.before
	}
	return nil
}

// Reset makes a card new again, keeping its tags other than "leech".
func (b *Box) Reset(id int) error {
	i := b.find(id)
	if i < 0 {
		return noCard(id)
	}
	c := &b.cards[i]
	c.Interval, c.Ease, c.Due, c.Lapses, c.Suspended = 0, startEase, 0, 0, false
	c.Tags = slices.DeleteFunc(c.Tags, func(t string) bool { return t == "leech" })
	return nil
}
`,
			"session.go": `package cards

import (
	"fmt"
	"slices"
	"sort"
)

// newPerDay is the most new cards a session takes in a day.
const newPerDay = 20

// reviewable reports whether a card of a deck takes part in reviews: it has
// been answered before, and neither it nor its deck is held back.
func (b *Box) reviewable(c Card, deck string) bool {
	return c.Deck == deck && c.Interval > 0 && !c.Suspended && !b.paused[deck]
}

// idsOf returns the IDs of cs, in order.
func idsOf(cs []Card) []int {
	ids := make([]int, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	return ids
}

// Due returns the IDs of a deck's cards due for review on or before today,
// most overdue first and then by ID.
func (b *Box) Due(deck string, today Day) []int {
	var due []Card
	for _, c := range b.cards {
		if b.reviewable(c, deck) && c.Due <= today {
			due = append(due, c)
		}
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].Due < due[j].Due })
	return idsOf(due)
}

// Session returns the IDs to study in a deck today: the due reviews, then
// new cards, lowest ID first, up to the daily limit.
func (b *Box) Session(deck string, today Day) []int {
	ids := b.Due(deck, today)
	fresh := 0
	for _, c := range b.cards {
		if fresh < newPerDay && c.Deck == deck && c.Interval == 0 && !c.Suspended && !b.paused[deck] {
			ids = append(ids, c.ID)
			fresh++
		}
	}
	return ids
}

// Today describes a deck's session for today, like "Spanish: 3 due, 7 new".
func (b *Box) Today(deck string, today Day) string {
	due := len(b.Due(deck, today))
	return fmt.Sprintf("%s: %d due, %d new", deck, due, len(b.Session(deck, today))-due)
}

// Forecast returns how many of a deck's reviews fall on each day from today
// to today+days-1. Overdue cards count on today.
func (b *Box) Forecast(deck string, today Day, days int) []int {
	out := make([]int, days)
	for _, c := range b.cards {
		if !b.reviewable(c, deck) {
			continue
		}
		if at := int(max(c.Due, today) - today); at < days {
			out[at]++
		}
	}
	return out
}

// Cram returns every card of a deck with a tag, suspended ones too, lowest
// ease first and then by ID, for study outside the schedule.
func (b *Box) Cram(deck, tag string) []int {
	var hits []Card
	for _, c := range b.cards {
		if c.Deck == deck && slices.Contains(c.Tags, tag) {
			hits = append(hits, c)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Ease < hits[j].Ease })
	return idsOf(hits)
}
`,
			"stats.go": `package cards

import "slices"

// Leeches returns the IDs of a deck's cards tagged "leech", in ID order.
func (b *Box) Leeches(deck string) []int {
	var out []int
	for _, c := range b.cards {
		if c.Deck == deck && slices.Contains(c.Tags, "leech") {
			out = append(out, c.ID)
		}
	}
	return out
}

// inDeck reports whether the card with that ID is in a deck.
func (b *Box) inDeck(id int, deck string) bool {
	i := b.find(id)
	return i >= 0 && b.cards[i].Deck == deck
}

// Retention returns the percentage, rounded down, of the reviews of a deck's
// cards from day from to day to that were not Again. A card's first review
// is left out, and with no reviews left it returns 0.
func (b *Box) Retention(deck string, from, to Day) int {
	seen := map[int]bool{}
	total, kept := 0, 0
	for _, e := range b.history {
		first := !seen[e.ID]
		seen[e.ID] = true
		if first || e.Day < from || e.Day > to || !b.inDeck(e.ID, deck) {
			continue
		}
		total++
		if e.Grade != Again {
			kept++
		}
	}
	if total == 0 {
		return 0
	}
	return kept * 100 / total
}

// Mature counts a deck's cards with an interval of 21 days or more.
func (b *Box) Mature(deck string) int {
	n := 0
	for _, c := range b.cards {
		if c.Deck == deck && c.Interval >= 21 {
			n++
		}
	}
	return n
}
`,
			"importer.go": `package cards

import (
	"fmt"
	"strings"
)

// Import adds the cards written in text, in the format of
// docs/import-format.md, to a deck and returns how many it added. A bad
// line fails the whole import, which then adds nothing.
func (b *Box) Import(deck, text string) (int, error) {
	var lines [][]string
	for n, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := fields(line)
		if len(f) < 2 || len(f) > 3 || f[0] == "" || f[1] == "" {
			return 0, fmt.Errorf("line %d: want front | back | tags", n+1)
		}
		lines = append(lines, f)
	}
	for _, f := range lines {
		id, err := b.Add(deck, f[0], f[1])
		if err != nil {
			return 0, err
		}
		if len(f) == 3 {
			for _, tag := range strings.Fields(f[2]) {
				b.Tag(id, tag)
			}
		}
	}
	return len(lines), nil
}

// fields splits an import line at the bars that are not escaped, trimming
// the spaces around each field.
func fields(line string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(out, strings.TrimSpace(cur.String()))
}

// Export writes a deck's cards in the import format, in ID order.
func (b *Box) Export(deck string) string {
	esc := strings.NewReplacer("|", "\\|")
	var sb strings.Builder
	for _, c := range b.Cards(deck) {
		sb.WriteString(esc.Replace(c.Front) + " | " + esc.Replace(c.Back))
		if len(c.Tags) > 0 {
			sb.WriteString(" | " + strings.Join(c.Tags, " "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
`,
		},
		Turns: script(
			cloud("Today we start on the flashcard app; read card.go, deck.go and review.go first. `Add` should refuse a card whose front or back is empty or only spaces, returning a new `ErrEmpty`. And one rule from me for the whole app: a card ID is handed out once and never again, even after we add deleting cards."),
			local("Add `func (b *Box) Decks() []string` to deck.go: the names of the decks that have cards, each once, sorted."),
			cloud("Read docs/scheduling.md. Replace the old Review with `type Grade int`, constants Again, Hard, Good and Easy (Again is 1), and `func (b *Box) Review(id int, g Grade, today Day) error`, which schedules the card as the doc says for Good and Easy; give Card the fields that needs. Any other grade returns a new `ErrBadGrade`. Again and Hard come later; for now they leave the card as it is."),
			cloud("Create session.go with `func (b *Box) Due(deck string, today Day) []int`: the IDs of the deck's cards due on or before today, most overdue first, then by ID. New cards haven't been answered yet, so they aren't due. For the session work later: a session takes the due reviews first, then new cards, lowest ID first, at most 10 new cards a day."),
			cloud("Add a `Suspended bool` field to Card, with `func (b *Box) Suspend(id int) error` and `func (b *Box) Unsuspend(id int) error` in card.go, and make Due leave suspended cards out. One more rule, for when lapses come in: a card's 4th lapse makes it a leech, which means it gets the tag \"leech\" and is suspended."),
			local("Add `func (b *Box) Edit(id int, front, back string) error` to card.go: it replaces both sides of the card, with the same checks as Add, or returns ErrNoCard."),
			local("Cards get tags: add a `Tags []string` field to Card and `func (b *Box) Tag(id int, tag string) error` to card.go. Tags are stored lower-case, and a tag the card already has isn't added again."),
			local("Read docs/import-format.md; later on we'll import decks people write that way. Tell me in a few lines what a valid line looks like. No code changes."),
			local("Keep a review history: add `type Entry struct { ID int; Day Day; Grade Grade }` and `func (b *Box) History(id int) []Entry` to review.go, giving a card's reviews oldest first, and have Review add an Entry for each review. A note for the stats later: a card's first review never counts toward retention."),
			local("Add `func (b *Box) Delete(id int) error` to card.go: it removes the card, or returns ErrNoCard."),
			cloud("Add `func (b *Box) RenameDeck(from, to string) int` to deck.go: every card in deck from moves to deck to, and it returns how many moved. Add `func (b *Box) Move(id int, deck string) error` there too, for one card."),
			local("Add `func (b *Box) Session(deck string, today Day) []int` to session.go: the IDs of the cards to study in that deck today, in order."),
			cloud("An ErrNoCard doesn't say which card. Add `type CardError struct { ID int }` to card.go with an `Error() string` method and an `Is(target error) bool` method that makes errors.Is(err, ErrNoCard) true, and return a *CardError everywhere we return ErrNoCard today, in every file."),
			local("Finish Review in review.go: Again and Hard schedule the card too now, and each lapse adds one to a new `Lapses int` field on Card."),
			cloud("Add `func (b *Box) Undo() error` to review.go: it takes back the most recent review, putting that card back exactly as it was before and taking the review out of the history. When there is nothing to take back it returns a new `ErrNothingToUndo`."),
			local("Add `func (b *Box) Forecast(deck string, today Day, days int) []int` to session.go: for each day from today through today+days-1, how many of the deck's cards are due that day, with overdue cards counted on today. New and suspended cards don't count, as in Due."),
			local("Add `func (b *Box) Search(query string) []int` to deck.go: the IDs, in order, of the cards in any deck whose front or back contains query, ignoring case."),
			local("Add `func (b *Box) Reverse(id int) (int, error)` to card.go: it adds a new card to the same deck with the front and back swapped and the same tags, and returns the new card's ID."),
			cloud("Create stats.go with `func (b *Box) Leeches(deck string) []int`: the IDs of the deck's cards tagged \"leech\", in order. Something else while I think of it: I get through cards faster these days, so new cards go up to 20 a day from now on."),
			cloud("Create importer.go with `func (b *Box) Import(deck, text string) (int, error)`: it adds the cards written in text in the import format to deck, and returns how many it added."),
			local("In importer.go add `func (b *Box) Export(deck string) string`: the deck's cards in ID order, written in the import format so Import can read them back."),
			cloud("Add `func (b *Box) Retention(deck string, from, to Day) int` to stats.go: the percentage, rounded down, of the reviews of the deck's cards from day from to day to, inclusive, that were not Again. If no review counts, it's 0."),
			local("Add `func (b *Box) Mature(deck string) int` to stats.go: how many of the deck's cards have an interval of 21 days or more."),
			cloud("Add `func (b *Box) Cram(deck, tag string) []int` to session.go, for studying outside the schedule: every card in the deck with that tag, suspended ones too, lowest Ease first, then by ID."),
			local("Add `func (b *Box) Reset(id int) error` to review.go: the card becomes new again, with interval 0, ease 250, Due 0, no lapses, not suspended and without the \"leech\" tag. Its other tags stay."),
			cloud("Reviewing a suspended card should fail with a new `ErrSuspended` and change nothing, the history included."),
			local("Add `func (b *Box) PauseDeck(deck string)` and `func (b *Box) ResumeDeck(deck string)` to deck.go. While a deck is paused, Due and Session give nothing for it."),
			cloud("Add `func (b *Box) Today(deck string, today Day) string` to session.go, worded like \"Spanish: 3 due, 7 new\": how many due reviews and how many new cards today's session for that deck holds."),
			cloud("Add `func (b *Box) Duplicates(deck string) [][]int` to deck.go: the groups of the deck's cards whose fronts match, ignoring case and the spaces around them, each group as IDs in order. Order the groups by their first ID, and leave out groups of one."),
			cloud("Create store.go with `func (b *Box) Save() ([]byte, error)` and `func Load(data []byte) (*Box, error)`, using encoding/json. A loaded box must behave just like the box that was saved, paused decks included; only the review history needn't be kept."),
		),
		Carries: []Carry{
			{
				What:        "a card ID is never given out again, even after a delete",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{9, 29},
				Tests:       []string{"TestIDsNotReused", "TestIDsNotReusedAfterLast", "TestIDsNotReusedAfterLoad"},
				Absent:      []string{"handed out once", "never again", "reuse", "reused", "same id"},
			},
			{
				What:        "Again and Hard as docs/scheduling.md sets them: ease down by 20 and 15, floor 130, Hard times 12/10",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{13},
				Source:      "docs/scheduling.md",
				Tests:       []string{"TestReviewHard", "TestReviewAgain", "TestEaseFloor"},
				Absent:      []string{"130", "12 / 10", "1.2", "down by"},
			},
			{
				What:        "a session puts the due reviews before the new cards",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{11},
				Tests:       []string{"TestSessionReviewsThenNew"},
				Absent:      []string{"reviews first", "lowest id", "10 new", "ten new", "new cards a day"},
			},
			{
				What:        "the fourth lapse makes a leech: tagged and suspended",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{13},
				Tests:       []string{"TestLeechAtFourthLapse", "TestLeechLeavesDue"},
				Absent:      []string{"4th", "fourth", "four lapses", "4 lapses", "is suspended"},
			},
			{
				What:        "the deck import format: notes and blank lines skipped, escaped bars, all or nothing with the line (docs/import-format.md)",
				Kind:        CarryDiscovered,
				Established: 7,
				Needed:      []int{19},
				Source:      "docs/import-format.md",
				Tests:       []string{"TestImportSkipsCommentsAndBlank", "TestImportEscapedBar", "TestImportFailsWhole"},
				Absent:      []string{"vertical bar", "\\|", "#", "comment", "line number"},
			},
			{
				What:        "a card's first review does not count toward retention",
				Kind:        CarryUser,
				Established: 8,
				Needed:      []int{21},
				Tests:       []string{"TestRetentionSkipsFirstReview", "TestRetentionOnlyFirstReviews"},
				Absent:      []string{"first review", "never counts", "toward retention"},
			},
			{
				What:        "new cards go up to 20 a day (revises the limit of 10)",
				Kind:        CarryUser,
				Established: 18,
				Needed:      []int{26, 27},
				Tests:       []string{"TestSessionNewLimit", "TestTodayCounts"},
				Absent:      []string{"20 a day", "up to 20", "twenty", "faster"},
			},
			{
				What:        "tags are kept lower-case and only once",
				Kind:        CarryEmbodied,
				Established: 6,
				Needed:      []int{19},
				Tests:       []string{"TestImportTags"},
				Absent:      []string{"lower-case", "lower case", "lowercase", "already has"},
			},
		},
	}
}
