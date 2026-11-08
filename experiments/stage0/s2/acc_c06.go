package s2

func accC06() Task {
	return Task{
		ID:     "s2-c06-seating-chart",
		Set:    "acceptance",
		System: system("a seating chart for a hall"),
		Files: map[string]string{
			"go.mod": goMod,
			"chart.go": `package seating

// Chart is a rectangular room of seats. Row 0 is the front row and column 0 is
// the left end of a row.
type Chart struct {
	Rows, Cols int
	seats      map[[2]int]string // guest name by row and column
}

// New returns an empty chart.
func New(rows, cols int) *Chart {
	return &Chart{Rows: rows, Cols: cols, seats: map[[2]int]string{}}
}

// Capacity returns how many seats the room has.
func (c *Chart) Capacity() int { return c.Rows * c.Cols }
`,
			"venue/fire_rules.txt": `Fire marshal's conditions for the hall

The last row of any room is kept clear for access. Nobody may be given a seat in
it, a request for one is refused, and it does not count as available seating.

The certificate runs until March. Exit signs are checked monthly.
`,
			"chart_test.go": `package seating

import "testing"

func TestCapacity(t *testing.T) {
	if got := New(3, 4).Capacity(); got != 12 {
		t.Fatalf("Capacity = %d", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package seating_test

import (
	"errors"
	"testing"

	seating "fixture"
)

func TestOccupied(t *testing.T) {
	c := seating.New(3, 3)
	if c.Occupied() != 0 {
		t.Fatalf("Occupied = %d", c.Occupied())
	}
	if err := c.Assign("ann", 0, 0); err != nil {
		t.Fatal(err)
	}
	if c.Occupied() != 1 {
		t.Fatalf("Occupied = %d", c.Occupied())
	}
}

func TestLabelFrontRows(t *testing.T) {
	c := seating.New(5, 5)
	if got := c.Label(0, 0); got != "A1" {
		t.Errorf("Label(0, 0) = %q", got)
	}
	if got := c.Label(1, 2); got != "B3" {
		t.Errorf("Label(1, 2) = %q", got)
	}
}

func TestLabelSkipsLetters(t *testing.T) {
	c := seating.New(20, 5)
	for row, want := range map[int]string{7: "H1", 8: "J1", 12: "N1", 13: "P1"} {
		if got := c.Label(row, 0); got != want {
			t.Errorf("Label(%d, 0) = %q, want %q", row, got, want)
		}
	}
}

func TestAssignRejectsOutside(t *testing.T) {
	c := seating.New(3, 3)
	if err := c.Assign("ann", 5, 0); err == nil {
		t.Error("row outside")
	}
	if err := c.Assign("ann", 0, 3); err == nil {
		t.Error("column outside")
	}
	if c.Occupied() != 0 {
		t.Fatalf("Occupied = %d", c.Occupied())
	}
}

func TestAssignMoves(t *testing.T) {
	c := seating.New(3, 3)
	if err := c.Assign("ann", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Assign("ann", 1, 1); err != nil {
		t.Fatal(err)
	}
	if c.Occupied() != 1 {
		t.Fatalf("Occupied = %d", c.Occupied())
	}
	if err := c.Assign("bo", 0, 0); err != nil {
		t.Fatalf("the seat ann left: %v", err)
	}
}

func TestAssignTakenSentinel(t *testing.T) {
	c := seating.New(3, 3)
	if err := c.Assign("ann", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Assign("bo", 0, 0); !errors.Is(err, seating.ErrTaken) {
		t.Fatalf("err = %v", err)
	}
}

func TestAssignFireRow(t *testing.T) {
	c := seating.New(3, 3)
	if err := c.Assign("ann", 2, 0); err == nil {
		t.Fatal("seated in the last row")
	}
	if c.Occupied() != 0 {
		t.Fatalf("Occupied = %d", c.Occupied())
	}
}

func TestFreeFallsAsSeatsFill(t *testing.T) {
	c := seating.New(3, 3)
	before := c.Free()
	if err := c.Assign("ann", 0, 0); err != nil {
		t.Fatal(err)
	}
	if c.Free() != before-1 {
		t.Fatalf("Free went from %d to %d", before, c.Free())
	}
}

func TestFreeExcludesFireRow(t *testing.T) {
	if got := seating.New(3, 4).Free(); got != 8 {
		t.Fatalf("Free = %d", got)
	}
}

func TestSeatFrontToBack(t *testing.T) {
	c := seating.New(3, 2)
	for _, want := range []string{"A1", "A2", "B1"} {
		got, err := c.Seat(want + "-guest")
		if err != nil || got != want {
			t.Fatalf("Seat = %q, %v, want %q", got, err, want)
		}
	}
}

func TestSeatSkipsLetters(t *testing.T) {
	c := seating.New(10, 1)
	var last string
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} {
		got, err := c.Seat(name)
		if err != nil {
			t.Fatal(err)
		}
		last = got
	}
	if last != "J1" {
		t.Fatalf("ninth seat = %q", last)
	}
}

func TestSeatFireRow(t *testing.T) {
	c := seating.New(2, 2)
	for _, name := range []string{"a", "b"} {
		if _, err := c.Seat(name); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := c.Seat("c"); err == nil {
		t.Fatalf("third guest got %q", got)
	}
}

func TestSeatOneSeatPerGuest(t *testing.T) {
	c := seating.New(4, 4)
	if _, err := c.Seat("ann"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Seat("ann"); err != nil {
		t.Fatal(err)
	}
	if c.Occupied() != 1 {
		t.Fatalf("ann holds %d seats", c.Occupied())
	}
}

func TestParseLabelBasic(t *testing.T) {
	row, col, err := seating.ParseLabel("B3")
	if err != nil || row != 1 || col != 2 {
		t.Fatalf("ParseLabel = %d, %d, %v", row, col, err)
	}
}

func TestParseLabelRejects(t *testing.T) {
	for _, s := range []string{"", "3", "A", "A0", "Ax", "3A"} {
		if _, _, err := seating.ParseLabel(s); err == nil {
			t.Errorf("ParseLabel(%q) gave no error", s)
		}
	}
}

func TestParseLabelSkippedLetters(t *testing.T) {
	row, col, err := seating.ParseLabel("J4")
	if err != nil || row != 8 || col != 3 {
		t.Fatalf("J4 = %d, %d, %v", row, col, err)
	}
	for _, s := range []string{"I1", "O1"} {
		if _, _, err := seating.ParseLabel(s); err == nil {
			t.Errorf("ParseLabel(%q) gave no error", s)
		}
	}
}
`},
		Solution: map[string]string{
			"chart.go": `package seating

import (
	"errors"
	"strconv"
	"strings"
)

// ErrTaken is what a guest gets for a seat they can't have.
var ErrTaken = errors.New("seat taken")

const rowLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ"

// Chart is a rectangular room of seats. Row 0 is the front row and column 0 is
// the left end of a row.
type Chart struct {
	Rows, Cols int
	seats      map[[2]int]string // guest name by row and column
}

// New returns an empty chart.
func New(rows, cols int) *Chart {
	return &Chart{Rows: rows, Cols: cols, seats: map[[2]int]string{}}
}

// Capacity returns how many seats the room has.
func (c *Chart) Capacity() int { return c.Rows * c.Cols }

// Occupied returns how many seats have a guest.
func (c *Chart) Occupied() int { return len(c.seats) }

// Label returns the printed label of a seat.
func (c *Chart) Label(row, col int) string {
	return string(rowLetters[row]) + strconv.Itoa(col+1)
}

func (c *Chart) find(name string) ([2]int, bool) {
	for at, guest := range c.seats {
		if guest == name {
			return at, true
		}
	}
	return [2]int{}, false
}

// Assign gives a guest the seat at that position, moving them if already seated.
func (c *Chart) Assign(name string, row, col int) error {
	if row < 0 || col < 0 || row >= c.Rows || col >= c.Cols {
		return errors.New("no such seat")
	}
	if row == c.Rows-1 {
		return ErrTaken
	}
	if guest, ok := c.seats[[2]int{row, col}]; ok && guest != name {
		return ErrTaken
	}
	if at, ok := c.find(name); ok {
		delete(c.seats, at)
	}
	c.seats[[2]int{row, col}] = name
	return nil
}

// Free returns how many seats a guest could still be given.
func (c *Chart) Free() int { return (c.Rows-1)*c.Cols - c.Occupied() }

// Seat gives a guest the next free seat and returns its label.
func (c *Chart) Seat(name string) (string, error) {
	if at, ok := c.find(name); ok {
		return c.Label(at[0], at[1]), nil
	}
	for row := 0; row < c.Rows-1; row++ {
		for col := 0; col < c.Cols; col++ {
			if _, ok := c.seats[[2]int{row, col}]; !ok {
				c.seats[[2]int{row, col}] = name
				return c.Label(row, col), nil
			}
		}
	}
	return "", errors.New("no seat left")
}

// ParseLabel is the reverse of Label.
func ParseLabel(s string) (int, int, error) {
	if len(s) < 2 {
		return 0, 0, errors.New("not a seat label")
	}
	row := strings.IndexByte(rowLetters, s[0])
	n, err := strconv.Atoi(s[1:])
	if row < 0 || err != nil || n < 1 {
		return 0, 0, errors.New("not a seat label")
	}
	return row, n - 1, nil
}
`,
		},
		Turns: script(
			cloud("So we agree on how seats are named: the front row is A, and row letters leave out I and O, as they do in most theatres, because people misread them as 1 and 0. Seat numbers start at 1 from the left. Nothing prints a name yet. Add `func (c *Chart) Occupied() int`, how many seats have a guest."),
			local("Add `var ErrTaken = errors.New(\"seat taken\")`. Nothing returns it yet. It's what someone gets for a seat that already has another guest in it."),
			local("Read venue/fire_rules.txt and tell me what in it we have to honour. No code changes."),
			local("Add `func (c *Chart) Label(row, col int) string`: the printed label of the seat at that zero-based row and column, such as B3."),
			cloud("Add `func (c *Chart) Assign(name string, row, col int) error`, which gives a guest the seat at that zero-based row and column. A position outside the room is an error. A guest only ever holds one seat, so assigning someone already seated moves them."),
			local("Add `func (c *Chart) Free() int`: how many seats a guest could still be given."),
			local("Add `func (c *Chart) Seat(name string) (string, error)`, for the walk-in desk: it gives the guest the next free seat, working from the front of the room and from the left within a row, and returns its label. When nothing is left it's an error."),
			cloud("Add `func ParseLabel(s string) (int, int, error)`: the reverse of Label, returning the zero-based row and column. Anything that isn't a label we could print is an error."),
		),
		Carries: []Carry{
			{
				What:        "row letters leave out I and O",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 6, 7},
				Tests:       []string{"TestLabelSkipsLetters", "TestSeatSkipsLetters", "TestParseLabelSkippedLetters"},
				Absent:      []string{"leave out", "misread", "theatre", "skip", "i and o"},
			},
			{
				What:        "ErrTaken for a seat another guest holds",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{4},
				Tests:       []string{"TestAssignTakenSentinel"},
				Absent:      []string{"errtaken", "seat taken", "another guest", "sentinel"},
			},
			{
				What:        "the last row is kept clear and is not seating (venue/fire_rules.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{4, 5, 6},
				Source:      "venue/fire_rules.txt",
				Tests:       []string{"TestAssignFireRow", "TestFreeExcludesFireRow", "TestSeatFireRow"},
				Absent:      []string{"fire", "last row", "back row", "marshal", "kept clear"},
			},
			{
				What:        "a guest only ever holds one seat",
				Kind:        CarryEmbodied,
				Established: 4,
				Needed:      []int{6},
				Tests:       []string{"TestSeatOneSeatPerGuest"},
				Absent:      []string{"one seat", "already seated", "moves"},
			},
		},
	}
}
