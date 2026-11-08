package s2

func accA07() Task {
	return Task{
		ID:     "s2-a07-gym-waitlist",
		Set:    "acceptance",
		System: system("a gym class booking system"),
		Files: map[string]string{
			"go.mod": goMod,
			"class.go": `package gym

// Class is one scheduled class. Booked members have a spot and the rest wait in
// the order they joined.
type Class struct {
	Capacity int
	booked   []string
	waiting  []string
}

// NewClass returns an empty class of that size.
func NewClass(capacity int) *Class { return &Class{Capacity: capacity} }

// Booked returns the members with a spot.
func (c *Class) Booked() []string { return append([]string(nil), c.booked...) }

// Waiting returns the members on the waiting list, in order.
func (c *Class) Waiting() []string { return append([]string(nil), c.waiting...) }
`,
			"docs/class-rules.txt": `Class rules from the studio manager

The waiting list for a class never holds more than 5 people.

Cancelling a booked spot less than 120 minutes before the start is a strike
against the member. Leaving the waiting list never is.

Towels are provided. Bring your own water.
`,
			"class_test.go": `package gym

import "testing"

func TestNewClassIsEmpty(t *testing.T) {
	c := NewClass(3)
	if c.Capacity != 3 || len(c.Booked()) != 0 || len(c.Waiting()) != 0 {
		t.Fatalf("class = %+v", c)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package gym_test

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	gym "fixture"
)

func class(t *testing.T, capacity int, members ...string) *gym.Class {
	t.Helper()
	c := gym.NewClass(capacity)
	for _, m := range members {
		if err := c.Join(m); err != nil {
			t.Fatalf("Join(%s): %v", m, err)
		}
	}
	return c
}

func equal(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestFreeSpots(t *testing.T) {
	c := class(t, 3, "a")
	if got := c.Free(); got != 2 {
		t.Fatalf("Free = %d", got)
	}
}

func TestJoinBooksThenWaits(t *testing.T) {
	c := class(t, 1, "a", "b", "c")
	equal(t, "Booked", c.Booked(), "a")
	equal(t, "Waiting", c.Waiting(), "b", "c")
}

func TestJoinAlreadyIn(t *testing.T) {
	c := class(t, 1, "a", "b")
	for _, m := range []string{"a", "b"} {
		if err := c.Join(m); !errors.Is(err, gym.ErrAlreadyIn) {
			t.Errorf("%s: %v", m, err)
		}
	}
}

func TestJoinWaitingListLimit(t *testing.T) {
	c := class(t, 1, "a")
	for i := range 5 {
		if err := c.Join("w" + strconv.Itoa(i)); err != nil {
			t.Fatalf("waiting member %d: %v", i+1, err)
		}
	}
	if err := c.Join("late"); !errors.Is(err, gym.ErrListFull) {
		t.Fatalf("err = %v", err)
	}
	if got := len(c.Waiting()); got != 5 {
		t.Fatalf("%d waiting", got)
	}
}

func TestPositionCountsFromOne(t *testing.T) {
	c := class(t, 1, "a", "b", "c")
	for member, want := range map[string]int{"b": 1, "c": 2} {
		got, err := c.Position(member)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s is at %d, want %d", member, got, want)
		}
	}
}

func TestPositionAfterLeaving(t *testing.T) {
	c := class(t, 1, "a", "b", "c")
	if err := c.Cancel("b", 1000); err != nil {
		t.Fatal(err)
	}
	got, err := c.Position("c")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("Position = %d", got)
	}
}

func TestPositionBooked(t *testing.T) {
	if _, err := class(t, 1, "a").Position("a"); !errors.Is(err, gym.ErrNotWaiting) {
		t.Fatalf("err = %v", err)
	}
}

func TestPositionStrangerSentinel(t *testing.T) {
	if _, err := class(t, 1, "a").Position("ghost"); !errors.Is(err, gym.ErrNotInClass) {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelStrangerSentinel(t *testing.T) {
	if err := class(t, 1, "a").Cancel("ghost", 1000); !errors.Is(err, gym.ErrNotInClass) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveStrangerSentinel(t *testing.T) {
	if err := class(t, 1, "a").Remove("ghost"); !errors.Is(err, gym.ErrNotInClass) {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelFreesSpot(t *testing.T) {
	c := class(t, 2, "a", "b")
	if err := c.Cancel("a", 1000); err != nil {
		t.Fatal(err)
	}
	equal(t, "Booked", c.Booked(), "b")
}

func TestCancelFromWaitingList(t *testing.T) {
	c := class(t, 1, "a", "b", "c")
	if err := c.Cancel("b", 1000); err != nil {
		t.Fatal(err)
	}
	equal(t, "Booked", c.Booked(), "a")
	equal(t, "Waiting", c.Waiting(), "c")
}

func TestCancelMovesFirstWaiterUp(t *testing.T) {
	c := class(t, 1, "a", "b", "c")
	if err := c.Cancel("a", 1000); err != nil {
		t.Fatal(err)
	}
	equal(t, "Booked", c.Booked(), "b")
	equal(t, "Waiting", c.Waiting(), "c")
}

func TestRemoveMovesFirstWaiterUp(t *testing.T) {
	c := class(t, 1, "a", "b", "c")
	if err := c.Remove("a"); err != nil {
		t.Fatal(err)
	}
	equal(t, "Booked", c.Booked(), "b")
	equal(t, "Waiting", c.Waiting(), "c")
}

func TestLargerCapacityMovesWaitersUp(t *testing.T) {
	c := class(t, 1, "a", "b", "c", "d")
	c.SetCapacity(3)
	equal(t, "Booked", c.Booked(), "a", "b", "c")
	equal(t, "Waiting", c.Waiting(), "d")
}

func TestSmallerCapacityKeepsBooked(t *testing.T) {
	c := class(t, 2, "a", "b")
	c.SetCapacity(1)
	equal(t, "Booked", c.Booked(), "a", "b")
	if c.Capacity != 1 {
		t.Fatalf("Capacity = %d", c.Capacity)
	}
}

func TestStrikeJustInsideLimit(t *testing.T) {
	c := class(t, 2, "a")
	if err := c.Cancel("a", 119); err != nil {
		t.Fatal(err)
	}
	if got := c.Strikes("a"); got != 1 {
		t.Fatalf("Strikes = %d", got)
	}
}

func TestNoStrikeAtLimit(t *testing.T) {
	c := class(t, 2, "a")
	if err := c.Cancel("a", 120); err != nil {
		t.Fatal(err)
	}
	if got := c.Strikes("a"); got != 0 {
		t.Fatalf("Strikes = %d", got)
	}
}

func TestLeavingWaitingListNoStrike(t *testing.T) {
	c := class(t, 1, "a", "b")
	if err := c.Cancel("b", 5); err != nil {
		t.Fatal(err)
	}
	if got := c.Strikes("b"); got != 0 {
		t.Fatalf("Strikes = %d", got)
	}
}

func TestNoStrikeWhenEarly(t *testing.T) {
	c := class(t, 2, "a")
	if err := c.Cancel("a", 5000); err != nil {
		t.Fatal(err)
	}
	if got := c.Strikes("a") + c.Strikes("nobody"); got != 0 {
		t.Fatalf("Strikes = %d", got)
	}
}

func TestRemoveIsNeverAStrike(t *testing.T) {
	c := class(t, 2, "a")
	if err := c.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if got := c.Strikes("a"); got != 0 {
		t.Fatalf("Strikes = %d", got)
	}
}
`},
		Solution: map[string]string{
			"class.go": `package gym

import (
	"errors"
	"slices"
)

// ErrAlreadyIn refuses a member already on either list.
var ErrAlreadyIn = errors.New("already in class")

// ErrListFull refuses a member the waiting list can't take.
var ErrListFull = errors.New("waiting list full")

// ErrNotInClass is for any call naming a member on neither list.
var ErrNotInClass = errors.New("not in class")

// ErrNotWaiting is for asking the place in line of a booked member.
var ErrNotWaiting = errors.New("not waiting")

const (
	maxWaiting = 5
	lateCancel = 120
)

// Class is one scheduled class. Booked members have a spot and the rest wait in
// the order they joined.
type Class struct {
	Capacity int
	booked   []string
	waiting  []string
	strikes  map[string]int
}

// NewClass returns an empty class of that size.
func NewClass(capacity int) *Class {
	return &Class{Capacity: capacity, strikes: map[string]int{}}
}

// Booked returns the members with a spot.
func (c *Class) Booked() []string { return append([]string(nil), c.booked...) }

// Waiting returns the members on the waiting list, in order.
func (c *Class) Waiting() []string { return append([]string(nil), c.waiting...) }

// Free returns the number of open spots.
func (c *Class) Free() int { return max(c.Capacity-len(c.booked), 0) }

// Join books the member if a spot is open and otherwise adds them to the line.
func (c *Class) Join(member string) error {
	if slices.Contains(c.booked, member) || slices.Contains(c.waiting, member) {
		return ErrAlreadyIn
	}
	if c.Free() > 0 {
		c.booked = append(c.booked, member)
		return nil
	}
	if len(c.waiting) >= maxWaiting {
		return ErrListFull
	}
	c.waiting = append(c.waiting, member)
	return nil
}

// Position returns the member's place in line.
func (c *Class) Position(member string) (int, error) {
	if i := slices.Index(c.waiting, member); i >= 0 {
		return i + 1, nil
	}
	if slices.Contains(c.booked, member) {
		return 0, ErrNotWaiting
	}
	return 0, ErrNotInClass
}

// fill gives open spots to the people waiting, in order.
func (c *Class) fill() {
	for c.Free() > 0 && len(c.waiting) > 0 {
		c.booked = append(c.booked, c.waiting[0])
		c.waiting = c.waiting[1:]
	}
}

// drop takes member off a list and reports whether they held a spot.
func (c *Class) drop(member string) (bool, error) {
	if i := slices.Index(c.booked, member); i >= 0 {
		c.booked = slices.Delete(c.booked, i, i+1)
		c.fill()
		return true, nil
	}
	if i := slices.Index(c.waiting, member); i >= 0 {
		c.waiting = slices.Delete(c.waiting, i, i+1)
		return false, nil
	}
	return false, ErrNotInClass
}

// Cancel takes the member off whichever list they're on.
func (c *Class) Cancel(member string, minutesBefore int) error {
	booked, err := c.drop(member)
	if err != nil {
		return err
	}
	if booked && minutesBefore < lateCancel {
		c.strikes[member]++
	}
	return nil
}

// Strikes returns how many strikes the member has.
func (c *Class) Strikes(member string) int { return c.strikes[member] }

// SetCapacity changes the class size.
func (c *Class) SetCapacity(n int) {
	c.Capacity = n
	c.fill()
}

// Remove is for staff taking someone off a class.
func (c *Class) Remove(member string) error {
	_, err := c.drop(member)
	return err
}
`,
		},
		Turns: script(
			local("How the front desk wants spots handled: when a booked spot opens up, the first person waiting gets it straight away, with nothing for them to do. No cancelling exists yet. Add `func (c *Class) Free() int`, the number of open spots."),
			local("Read docs/class-rules.txt and tell me what limits it puts on us. No code changes."),
			cloud("Add `var ErrAlreadyIn = errors.New(\"already in class\")`, `var ErrListFull = errors.New(\"waiting list full\")` and `func (c *Class) Join(member string) error`. Join books the member if a spot is open and otherwise puts them at the back of the waiting list. Someone already on either list gets ErrAlreadyIn, and ErrListFull is for when the waiting list can't take them."),
			cloud("Add `var ErrNotInClass = errors.New(\"not in class\")`. Nothing returns it yet. It's for any call naming a member who is on neither list. And people keep asking what number they are in line, so let's settle what we show: the person next in line is number 1, not 0."),
			local("Add `var ErrNotWaiting = errors.New(\"not waiting\")` and `func (c *Class) Position(member string) (int, error)`: the member's place in line. A booked member gets ErrNotWaiting."),
			cloud("Add `func (c *Class) Cancel(member string, minutesBefore int) error`: takes the member off whichever list they're on. minutesBefore is how long before the start they cancelled."),
			local("Add `func (c *Class) Strikes(member string) int`: how many strikes the member has picked up so far."),
			cloud("Add `func (c *Class) SetCapacity(n int)`: changes the class size. Anyone already booked keeps their spot even if the class ends up over capacity."),
			local("Add `func (c *Class) Remove(member string) error`, for staff taking someone off a class, say for not paying. It's never a strike."),
		),
		Carries: []Carry{
			{
				What:        "an opened spot goes to the first person waiting at once",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{5, 7, 8},
				Tests:       []string{"TestCancelMovesFirstWaiterUp", "TestRemoveMovesFirstWaiterUp", "TestLargerCapacityMovesWaitersUp"},
				Absent:      []string{"first person waiting", "straight away", "promot", "moves up"},
			},
			{
				What:        "the waiting list holds at most 5 people (docs/class-rules.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{2},
				Source:      "docs/class-rules.txt",
				Tests:       []string{"TestJoinWaitingListLimit"},
				Absent:      []string{"more than 5", "5 people", "never holds"},
			},
			{
				What:        "a strike is cancelling a booked spot under 120 minutes before the start (docs/class-rules.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{5, 6},
				Source:      "docs/class-rules.txt",
				Tests:       []string{"TestStrikeJustInsideLimit", "TestNoStrikeAtLimit", "TestLeavingWaitingListNoStrike"},
				Absent:      []string{"120", "two hours", "leaving the waiting list"},
			},
			{
				What:        "the person next in line is number 1",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4},
				Tests:       []string{"TestPositionCountsFromOne", "TestPositionAfterLeaving"},
				Absent:      []string{"number 1", "not 0", "1-based", "zero-based"},
			},
			{
				What:        "ErrNotInClass for any call naming a member on neither list",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{4, 5, 8},
				Tests:       []string{"TestPositionStrangerSentinel", "TestCancelStrangerSentinel", "TestRemoveStrangerSentinel"},
				Absent:      []string{"errnotinclass", "not in class", "neither list"},
			},
		},
	}
}
