package s2

func task06() Task {
	return Task{
		ID:     "s2-06-room-bookings",
		Set:    "tuning",
		System: system("a room booking calendar"),
		Files: map[string]string{
			"go.mod": goMod,
			"booking.go": `package booking

// Booking is one stay in a room. Start and End are day numbers.
type Booking struct {
	Room  string
	Start int
	End   int
}

// Valid reports whether the booking names a room and ends after it starts.
func (b Booking) Valid() bool { return b.Room != "" && b.End > b.Start }

// Calendar holds accepted bookings.
type Calendar struct {
	bookings []Booking
}

// All returns the accepted bookings.
func (c *Calendar) All() []Booking { return append([]Booking(nil), c.bookings...) }
`,
			"docs/rooms.txt": `Room notes

Rooms are named by the front desk. A room whose name starts with X- is out of
service, and a booking for it is refused the same way as a clash.

Cleaning happens between stays and does not affect availability.
`,
			"booking_test.go": `package booking

import "testing"

func TestValid(t *testing.T) {
	if !(Booking{Room: "a", Start: 1, End: 2}).Valid() {
		t.Fatal("should be valid")
	}
	if (Booking{Room: "a", Start: 2, End: 2}).Valid() {
		t.Fatal("should not be valid")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package booking_test

import (
	"errors"
	"testing"

	booking "fixture"
)

func stay(room string, start, end int) booking.Booking {
	return booking.Booking{Room: room, Start: start, End: end}
}

func TestCount(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 2)); err != nil {
		t.Fatal(err)
	}
	if c.Count() != 1 {
		t.Fatalf("Count = %d", c.Count())
	}
}

func TestBookOtherRoomAllowed(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 5)); err != nil {
		t.Fatal(err)
	}
	if err := c.Book(stay("s", 1, 5)); err != nil {
		t.Fatal(err)
	}
}

func TestBookRejectsInvalid(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 5, 5)); err == nil {
		t.Fatal("no error")
	}
	if c.Count() != 0 {
		t.Fatalf("Count = %d", c.Count())
	}
}

func TestBookClashIsError(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 5)); err != nil {
		t.Fatal(err)
	}
	if err := c.Book(stay("r", 3, 8)); err == nil {
		t.Fatal("no error")
	}
	if c.Count() != 1 {
		t.Fatalf("Count = %d", c.Count())
	}
}

func TestStaffBookUnlimited(t *testing.T) {
	if err := (&booking.Calendar{}).Book(stay("r", 1, 40)); err != nil {
		t.Fatal(err)
	}
}

func TestBackToBackAllowed(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 3)); err != nil {
		t.Fatal(err)
	}
	if err := c.Book(stay("r", 3, 5)); err != nil {
		t.Fatalf("checking in on another guest's checkout day: %v", err)
	}
}

func TestFreeOccupied(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 3)); err != nil {
		t.Fatal(err)
	}
	if c.Free("r", 1) || c.Free("r", 2) {
		t.Error("an occupied day is free")
	}
	if !c.Free("r", 9) || !c.Free("s", 1) {
		t.Error("an unoccupied day is not free")
	}
}

func TestFreeOnCheckoutDay(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 3)); err != nil {
		t.Fatal(err)
	}
	if !c.Free("r", 3) {
		t.Fatal("the checkout day is not free")
	}
}

func TestReserveBooksNights(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Reserve("r", 10, 3); err != nil {
		t.Fatal(err)
	}
	all := c.All()
	if len(all) != 1 || all[0] != stay("r", 10, 13) {
		t.Fatalf("All = %+v", all)
	}
}

func TestReserveLastNight(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Reserve("r", 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := c.Book(stay("r", 3, 4)); err != nil {
		t.Fatalf("day after the last night: %v", err)
	}
}

func TestExtendGrows(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 3)); err != nil {
		t.Fatal(err)
	}
	if err := c.Book(stay("r", 20, 22)); err != nil {
		t.Fatal(err)
	}
	if err := c.Extend("r", 1, 2); err != nil {
		t.Fatal(err)
	}
	if c.Free("r", 4) {
		t.Fatal("the extra nights are free")
	}
}

func TestExtendMissing(t *testing.T) {
	if err := (&booking.Calendar{}).Extend("r", 1, 1); err == nil {
		t.Fatal("no error")
	}
}

func TestBookConflictSentinel(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 5)); err != nil {
		t.Fatal(err)
	}
	if err := c.Book(stay("r", 3, 8)); !errors.Is(err, booking.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
}

func TestReserveConflictSentinel(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 5)); err != nil {
		t.Fatal(err)
	}
	if err := c.Reserve("r", 3, 2); !errors.Is(err, booking.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
}

func TestExtendConflictSentinel(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 3)); err != nil {
		t.Fatal(err)
	}
	if err := c.Book(stay("r", 5, 8)); err != nil {
		t.Fatal(err)
	}
	if err := c.Extend("r", 1, 5); !errors.Is(err, booking.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if !c.Free("r", 4) {
		t.Fatal("a refused extension changed the stay")
	}
}

func TestClosedRoomRefused(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("X-9", 1, 2)); !errors.Is(err, booking.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if c.Count() != 0 {
		t.Fatalf("Count = %d", c.Count())
	}
}

func TestReserveTooLong(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Reserve("r", 1, 15); !errors.Is(err, booking.ErrTooLong) {
		t.Fatalf("15 nights: %v", err)
	}
	if c.Count() != 0 {
		t.Fatalf("Count = %d", c.Count())
	}
	if err := c.Reserve("r", 1, 14); err != nil {
		t.Fatalf("14 nights: %v", err)
	}
}

func TestExtendTooLong(t *testing.T) {
	c := &booking.Calendar{}
	if err := c.Book(stay("r", 1, 11)); err != nil {
		t.Fatal(err)
	}
	if err := c.Extend("r", 1, 5); !errors.Is(err, booking.ErrTooLong) {
		t.Fatalf("15 nights: %v", err)
	}
	if !c.Free("r", 12) {
		t.Fatal("a refused extension changed the stay")
	}
	if err := c.Extend("r", 1, 4); err != nil {
		t.Fatalf("14 nights: %v", err)
	}
}
`},
		Solution: map[string]string{
			"booking.go": `package booking

import (
	"errors"
	"strings"
)

// ErrConflict is what callers check for when a room can't be had.
var ErrConflict = errors.New("booking conflict")

// ErrTooLong refuses a guest stay beyond the limit.
var ErrTooLong = errors.New("stay too long")

const maxNights = 14

// Booking is one stay in a room. Start and End are day numbers.
type Booking struct {
	Room  string
	Start int
	End   int
}

// Valid reports whether the booking names a room and ends after it starts.
func (b Booking) Valid() bool { return b.Room != "" && b.End > b.Start }

// Calendar holds accepted bookings.
type Calendar struct {
	bookings []Booking
}

// All returns the accepted bookings.
func (c *Calendar) All() []Booking { return append([]Booking(nil), c.bookings...) }

// Count returns the number of accepted bookings.
func (c *Calendar) Count() int { return len(c.bookings) }

// clashes reports whether b overlaps a stay in its room, other than stay skip.
func (c *Calendar) clashes(b Booking, skip int) bool {
	for i, o := range c.bookings {
		if i != skip && o.Room == b.Room && b.Start < o.End && o.Start < b.End {
			return true
		}
	}
	return false
}

// Book accepts a booking unless it clashes with one held for that room.
func (c *Calendar) Book(b Booking) error {
	if !b.Valid() {
		return errors.New("invalid booking")
	}
	if strings.HasPrefix(b.Room, "X-") {
		return ErrConflict
	}
	if c.clashes(b, -1) {
		return ErrConflict
	}
	c.bookings = append(c.bookings, b)
	return nil
}

// Free reports whether the room is unoccupied on that day.
func (c *Calendar) Free(room string, day int) bool {
	for _, b := range c.bookings {
		if b.Room == room && day >= b.Start && day < b.End {
			return false
		}
	}
	return true
}

// Reserve books that many nights from day start for a guest.
func (c *Calendar) Reserve(room string, start, nights int) error {
	if nights > maxNights {
		return ErrTooLong
	}
	return c.Book(Booking{Room: room, Start: start, End: start + nights})
}

// Extend adds nights to the guest's stay that begins on day start.
func (c *Calendar) Extend(room string, start, extra int) error {
	for i, b := range c.bookings {
		if b.Room != room || b.Start != start {
			continue
		}
		b.End += extra
		if b.End-b.Start > maxNights {
			return ErrTooLong
		}
		if c.clashes(b, i) {
			return ErrConflict
		}
		c.bookings[i] = b
		return nil
	}
	return errors.New("no such stay")
}
`,
		},
		Turns: script(
			local("Ground rule for this booking code: a stay's End is the checkout day and the room is not occupied that day, so one guest can check out on the day the next checks in. Nothing to code for it yet. Add `func (c *Calendar) Count() int`."),
			cloud("Add `var ErrConflict = errors.New(\"booking conflict\")`. It's the one error callers check for when a room can't be had. Nothing returns it yet."),
			cloud("Read docs/rooms.txt and summarise anything that affects booking. No code changes."),
			local("Add `func (c *Calendar) Book(b Booking) error`, which accepts a booking unless it clashes with one already held for that room. An invalid booking, as defined by Valid, is also an error."),
			cloud("Add `func (c *Calendar) Free(room string, day int) bool`: whether the room is unoccupied on that day."),
			cloud("The owner decided guests can't stay longer than 14 nights. Add `var ErrTooLong = errors.New(\"stay too long\")` for that. Book is only used by staff and stays unlimited. The limit is for the guest-facing calls we add next, which refuse a longer stay with ErrTooLong."),
			local("Add `func (c *Calendar) Reserve(room string, start, nights int) error`, the guest-facing call that books that many nights from day start."),
			cloud("Add `func (c *Calendar) Extend(room string, start, extra int) error`, also guest-facing: it adds extra nights to the stay in that room that begins on day start. With no such stay return any error, and leave the stay unchanged if the extension is refused."),
		),
		Carries: []Carry{
			{
				What:        "End is the checkout day and is not occupied",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 4, 6},
				Tests:       []string{"TestBackToBackAllowed", "TestFreeOnCheckoutDay", "TestReserveLastNight"},
				Absent:      []string{"checkout", "check out", "half-open", "exclusive"},
			},
			{
				What:        "ErrConflict is the one error for a room that can't be had",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{3, 6, 7},
				Tests:       []string{"TestBookConflictSentinel", "TestReserveConflictSentinel", "TestExtendConflictSentinel"},
				Absent:      []string{"errconflict", "sentinel"},
			},
			{
				What:        "X- rooms are out of service and refused like a clash (docs/rooms.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{3},
				Source:      "docs/rooms.txt",
				Tests:       []string{"TestClosedRoomRefused"},
				Absent:      []string{"x-", "out of service"},
			},
			{
				What:        "guest-facing stays are at most 14 nights, or ErrTooLong",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{6, 7},
				Tests:       []string{"TestReserveTooLong", "TestExtendTooLong"},
				Absent:      []string{"14", "errtoolong", "limit"},
			},
		},
	}
}
