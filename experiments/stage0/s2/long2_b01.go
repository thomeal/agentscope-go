package s2

func long2B01() Task {
	return Task{
		ID:     "s2-long2-b01-elevator-bank",
		Set:    "long",
		System: system("an elevator bank simulator"),
		Files: map[string]string{
			"go.mod": goMod,
			"building.go": `package lift

// Building is one bank of cars: floors Lowest to Highest, both included, as
// building floors. Floor 0 is at street level and floors below it are
// basements. All code works in building floors; see docs/building.txt for
// what people in the building see.
type Building struct {
	Lowest, Highest int
	// Cars is kept in ascending ID order by AddCar.
	Cars []*Car
	// Waiting holds the riders waiting on each floor, in the order they came.
	Waiting map[int][]Rider
}

// NewBuilding returns a building with the given floors and no cars.
func NewBuilding(lowest, highest int) *Building {
	return &Building{Lowest: lowest, Highest: highest, Waiting: map[int][]Rider{}}
}

// Contains reports whether floor exists in the building.
func (b *Building) Contains(floor int) bool {
	return floor >= b.Lowest && floor <= b.Highest
}

// AddCar adds a car, keeping Cars in ascending ID order.
func (b *Building) AddCar(c *Car) {
	i := 0
	for i < len(b.Cars) && b.Cars[i].ID < c.ID {
		i++
	}
	b.Cars = append(b.Cars, nil)
	copy(b.Cars[i+1:], b.Cars[i:])
	b.Cars[i] = c
}

// Car returns the car with the given ID, or nil.
func (b *Building) Car(id int) *Car {
	for _, c := range b.Cars {
		if c.ID == id {
			return c
		}
	}
	return nil
}
`,
			"calls.go": `package lift

// HallCall is a call made on a landing: someone on Floor wants to travel in
// Dir. At is the tick the call was made.
type HallCall struct {
	Floor int
	Dir   Direction
	At    int
}

// Pending holds the hall calls no car has taken yet, oldest first.
type Pending struct {
	calls []HallCall
}

// Add queues a call. A call for a floor and direction that is already queued
// is dropped, since calling again changes nothing.
func (p *Pending) Add(c HallCall) {
	for _, q := range p.calls {
		if q.Floor == c.Floor && q.Dir == c.Dir {
			return
		}
	}
	p.calls = append(p.calls, c)
}

// Len returns the number of queued calls.
func (p *Pending) Len() int { return len(p.calls) }

// Calls returns a copy of the queued calls, oldest first.
func (p *Pending) Calls() []HallCall {
	return append([]HallCall(nil), p.calls...)
}
`,
			"car.go": `package lift

// Direction is where a car is heading.
type Direction int

// The directions a car can have. An Idle car is standing still.
const (
	Idle Direction = iota
	Up
	Down
)

// String returns "idle", "up" or "down".
func (d Direction) String() string {
	switch d {
	case Up:
		return "up"
	case Down:
		return "down"
	}
	return "idle"
}

// Rider is one passenger, travelling from one building floor to another.
type Rider struct {
	ID       int
	From, To int
}

// Going returns the direction of the rider's trip.
func (r Rider) Going() Direction {
	switch {
	case r.To > r.From:
		return Up
	case r.To < r.From:
		return Down
	}
	return Idle
}

// Car is one elevator car. Floor is the building floor it is at, and Stops
// holds the building floors it still has to stop at.
type Car struct {
	ID     int
	Floor  int
	Dir    Direction
	Stops  []int
	Riders []Rider
}

// Load returns how many riders are aboard.
func (c *Car) Load() int { return len(c.Riders) }

// HasStop reports whether floor is one of the car's stops.
func (c *Car) HasStop(floor int) bool {
	for _, s := range c.Stops {
		if s == floor {
			return true
		}
	}
	return false
}
`,
			"docs/building.txt": `Tower B: building notes for the lift simulator

Floors

The shaft runs from two basement levels up to building floor 20. Building
floor 0 is the street level and basement levels are negative: -1 and -2.
The simulator works in building floors throughout.

Labels

The displays and buttons in the cars and on the landings show labels, not
building floors. The street level is labelled G and the basements are B1
(floor -1) and B2 (floor -2). Floors 1 to 12 show their own number. There
is no label 13: building floor 13 is labelled 14, floor 14 is labelled 15,
and so on up, so floor 20 is labelled 21. Anything shown to people, such as
logs and exports, uses labels.

Calls

A hall call is made on a landing with the up or down button and only says
which way the caller wants to go. A car call is made inside a car with a
floor button and becomes one of that car's stops. Hall calls wait in a
queue until a car is assigned to them; the car then gets the call's floor
as a stop.

Cars

Cars 1 and 2 are passenger cars and serve every floor.

Car 3 is the freight car. It runs only on calls made from inside it (car
calls) and is never assigned a hall call, wherever it happens to be.

On a fire recall every passenger car goes to G, while the freight car goes
to B1, where the loading dock is.

Maintenance

Cars are serviced on the first Monday of each month, one at a time, and the
simulator does not model service visits. A car out of service is simply
left out of the building's cars for that run.
`,
			"docs/trace-format.txt": `Trace export format, version 2

An export is plain text, one event per line, in the order the events
happened. The first line is the header

  trace v2

and every line after it is one event with four fields separated by single
spaces:

  tick    the tick, zero-padded to five digits: 00042
  car     the letter C followed by the car ID: C2
  floor   the floor's label as shown in the building: G, B1, 14
  kind    arrive, open, close, board or leave

For example:

  trace v2
  00001 C1 1 arrive
  00002 C1 2 arrive
  00002 C1 2 open

Every line, the last one included, ends with a newline. A trace with no
events exports as the header line alone.

Version 1 wrote building floors and no header. Readers of version 1 files
are no longer supported.
`,
			"lift_test.go": `package lift

import "testing"

func TestDirectionNames(t *testing.T) {
	for d, want := range map[Direction]string{Idle: "idle", Up: "up", Down: "down"} {
		if got := d.String(); got != want {
			t.Errorf("%d: %q", d, got)
		}
	}
}

func TestRiderGoing(t *testing.T) {
	if (Rider{From: 1, To: 4}).Going() != Up || (Rider{From: 4, To: -1}).Going() != Down {
		t.Error("wrong direction")
	}
}

func TestContains(t *testing.T) {
	b := NewBuilding(-2, 20)
	if !b.Contains(-2) || !b.Contains(20) || b.Contains(21) || b.Contains(-3) {
		t.Error("wrong bounds")
	}
}

func TestAddCarKeepsOrder(t *testing.T) {
	b := NewBuilding(0, 5)
	b.AddCar(&Car{ID: 2})
	b.AddCar(&Car{ID: 1})
	b.AddCar(&Car{ID: 3})
	for i, c := range b.Cars {
		if c.ID != i+1 {
			t.Fatalf("car %d at %d", c.ID, i)
		}
	}
	if b.Car(2) == nil || b.Car(9) != nil {
		t.Error("lookup")
	}
}

func TestPendingDropsRepeats(t *testing.T) {
	var p Pending
	p.Add(HallCall{Floor: 3, Dir: Up})
	p.Add(HallCall{Floor: 3, Dir: Up, At: 5})
	p.Add(HallCall{Floor: 3, Dir: Down})
	if p.Len() != 2 {
		t.Fatalf("%d calls", p.Len())
	}
}

func TestTraceCount(t *testing.T) {
	var tr Trace
	tr.Add(Event{Tick: 1, Kind: "arrive"})
	tr.Add(Event{Tick: 2, Kind: "arrive"})
	tr.Add(Event{Tick: 2, Kind: "open"})
	var none *Trace
	none.Add(Event{Kind: "open"})
	if tr.Count("arrive") != 2 || tr.Count("open") != 1 || tr.Count("close") != 0 {
		t.Fatalf("events %v", tr.Events)
	}
}

func TestCarLoadAndStops(t *testing.T) {
	c := &Car{ID: 1, Stops: []int{2, 6}, Riders: []Rider{{ID: 1}, {ID: 2}}}
	if c.Load() != 2 || !c.HasStop(6) || c.HasStop(3) {
		t.Fatalf("car %+v", c)
	}
}
`,
			"trace.go": `package lift

// Event is one thing that happened to a car during a run. Floor is the
// building floor the car was at when it happened.
type Event struct {
	Tick  int
	Car   int
	Floor int
	Kind  string // "arrive", "open", "close", "board" or "leave"
}

// Trace collects events in the order they happen.
type Trace struct {
	Events []Event
}

// Add records an event. A nil trace records nothing, so callers that do not
// care about events can pass nil.
func (t *Trace) Add(e Event) {
	if t != nil {
		t.Events = append(t.Events, e)
	}
}

// Count returns how many events of the given kind were recorded.
func (t *Trace) Count(kind string) int {
	n := 0
	for _, e := range t.Events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}
`,
		},
		Hidden: map[string]string{hiddenName: `package lift_test

import (
	"errors"
	"slices"
	"testing"

	lift "fixture"
)

func ids(riders []lift.Rider) []int {
	var out []int
	for _, r := range riders {
		out = append(out, r.ID)
	}
	return out
}

func carWithStops(floor int, dir lift.Direction, stops ...int) *lift.Car {
	c := &lift.Car{ID: 1, Floor: floor, Dir: dir}
	for _, s := range stops {
		c.AddStop(s)
	}
	return c
}

func TestLabels(t *testing.T) {
	for floor, want := range map[int]string{0: "G", -1: "B1", -2: "B2", 1: "1", 12: "12", 13: "14", 20: "21"} {
		if got := lift.Label(floor); got != want {
			t.Errorf("Label(%d) = %q, want %q", floor, got, want)
		}
	}
}

func TestParseLabel(t *testing.T) {
	for label, want := range map[string]int{"G": 0, "B1": -1, "B2": -2, "7": 7, "12": 12, "14": 13, "21": 20} {
		if got, err := lift.ParseLabel(label); err != nil || got != want {
			t.Errorf("ParseLabel(%q) = %d, %v", label, got, err)
		}
	}
}

func TestParseLabelRejects(t *testing.T) {
	for _, label := range []string{"13", "0", "B0", "x", ""} {
		if got, err := lift.ParseLabel(label); !errors.Is(err, lift.ErrNoFloor) {
			t.Errorf("ParseLabel(%q) = %d, %v", label, got, err)
		}
	}
}

func TestAddStop(t *testing.T) {
	c := carWithStops(0, lift.Idle, 5, 2, 9, 2, 5)
	if !slices.Equal(c.Stops, []int{2, 5, 9}) {
		t.Fatalf("Stops = %v", c.Stops)
	}
}

func TestNextStopKeepsDirection(t *testing.T) {
	if got, ok := carWithStops(4, lift.Up, 2, 6, 9).NextStop(); !ok || got != 6 {
		t.Errorf("going up: %d, %v", got, ok)
	}
	if got, ok := carWithStops(4, lift.Down, 2, 3, 9).NextStop(); !ok || got != 3 {
		t.Errorf("going down: %d, %v", got, ok)
	}
}

func TestNextStopTurnsAround(t *testing.T) {
	if got, ok := carWithStops(4, lift.Up, 1, 2).NextStop(); !ok || got != 2 {
		t.Errorf("going up: %d, %v", got, ok)
	}
	if got, ok := carWithStops(4, lift.Down, 8, 6).NextStop(); !ok || got != 6 {
		t.Errorf("going down: %d, %v", got, ok)
	}
}

func TestNextStopIdle(t *testing.T) {
	if got, _ := carWithStops(5, lift.Idle, 3, 7).NextStop(); got != 3 {
		t.Errorf("equally near: %d", got)
	}
	if got, _ := carWithStops(5, lift.Idle, 2, 7).NextStop(); got != 7 {
		t.Errorf("nearer above: %d", got)
	}
	if _, ok := carWithStops(5, lift.Idle).NextStop(); ok {
		t.Error("a car with no stops has a next stop")
	}
}

func TestETAStraight(t *testing.T) {
	if got := carWithStops(0, lift.Idle).ETA(7); got != 7 {
		t.Errorf("idle car: %d", got)
	}
	c := carWithStops(3, lift.Idle)
	c.DoorTicks = 1
	if got := c.ETA(5); got != 3 {
		t.Errorf("doors still open: %d", got)
	}
}

func TestETAWithStops(t *testing.T) {
	if got := carWithStops(0, lift.Up, 3, 5).ETA(8); got != 12 {
		t.Fatalf("ETA = %d", got)
	}
}

func TestETAAroundTheTop(t *testing.T) {
	if got := carWithStops(4, lift.Up, 8).ETA(2); got != 12 {
		t.Fatalf("ETA = %d", got)
	}
}

func TestCost(t *testing.T) {
	call := lift.HallCall{Floor: 5, Dir: lift.Up}
	for _, tc := range []struct {
		car  *lift.Car
		want int
	}{
		{carWithStops(0, lift.Idle), 5},
		{carWithStops(2, lift.Up), 3},
		{carWithStops(2, lift.Down), 9},
	} {
		if got := lift.Cost(tc.car, call); got != tc.want {
			t.Errorf("%v: Cost = %d, want %d", tc.car.Dir, got, tc.want)
		}
	}
}

func TestUnload(t *testing.T) {
	c := &lift.Car{Floor: 3, Riders: []lift.Rider{{ID: 1, To: 3}, {ID: 2, To: 5}, {ID: 3, To: 3}}}
	off := c.Unload()
	if !slices.Equal(ids(off), []int{1, 3}) || !slices.Equal(ids(c.Riders), []int{2}) {
		t.Fatalf("off %v, aboard %v", ids(off), ids(c.Riders))
	}
}

func TestPendingRemove(t *testing.T) {
	var p lift.Pending
	p.Add(lift.HallCall{Floor: 3, Dir: lift.Up})
	p.Add(lift.HallCall{Floor: 5, Dir: lift.Down})
	p.Add(lift.HallCall{Floor: 3, Dir: lift.Down})
	if !p.Remove(5, lift.Down) || p.Remove(5, lift.Down) {
		t.Fatal("Remove reported the wrong thing")
	}
	want := []lift.HallCall{{Floor: 3, Dir: lift.Up}, {Floor: 3, Dir: lift.Down}}
	if got := p.Calls(); !slices.Equal(got, want) {
		t.Fatalf("Calls = %v", got)
	}
}

func TestCarString(t *testing.T) {
	if got := (&lift.Car{ID: 2, Floor: 13, Dir: lift.Up}).String(); got != "car 2 at 14 up" {
		t.Errorf("String = %q", got)
	}
	if got := (&lift.Car{ID: 1, Floor: -1}).String(); got != "car 1 at B1 idle" {
		t.Errorf("String = %q", got)
	}
}

func waitingAt(floor, n int) []lift.Rider {
	var out []lift.Rider
	for i := 1; i <= n; i++ {
		out = append(out, lift.Rider{ID: i, From: floor, To: floor + 4, CalledAt: i})
	}
	return out
}

func TestBoardCapacity(t *testing.T) {
	c := &lift.Car{ID: 1, Floor: 0}
	rest := c.Board(waitingAt(0, 10), 20)
	if c.Load() != 8 || !slices.Equal(ids(rest), []int{9, 10}) {
		t.Fatalf("aboard %v, left %v", ids(c.Riders), ids(rest))
	}
}

func TestBoardCallOrder(t *testing.T) {
	c := &lift.Car{ID: 1, Floor: 0}
	waiting := []lift.Rider{{ID: 1, To: 4, CalledAt: 9}, {ID: 2, To: 4, CalledAt: 4}, {ID: 3, To: 4, CalledAt: 6}}
	c.Board(waiting, 10)
	if !slices.Equal(ids(c.Riders), []int{2, 3, 1}) {
		t.Fatalf("aboard %v", ids(c.Riders))
	}
}

func TestBoardDirection(t *testing.T) {
	c := &lift.Car{ID: 1, Floor: 3, Dir: lift.Up}
	waiting := []lift.Rider{{ID: 1, From: 3, To: 1}, {ID: 2, From: 3, To: 7}, {ID: 3, From: 5, To: 9}}
	rest := c.Board(waiting, 1)
	if !slices.Equal(ids(c.Riders), []int{2}) || !slices.Equal(ids(rest), []int{1, 3}) {
		t.Fatalf("aboard %v, left %v", ids(c.Riders), ids(rest))
	}
}

func TestBoardSetsStops(t *testing.T) {
	c := &lift.Car{ID: 1, Floor: 0}
	c.Board([]lift.Rider{{ID: 1, To: 6, CalledAt: 1}, {ID: 2, To: 4, CalledAt: 2}}, 7)
	if !slices.Equal(c.Stops, []int{4, 6}) || c.Riders[0].BoardedAt != 7 || c.Riders[1].BoardedAt != 7 {
		t.Fatalf("stops %v, riders %+v", c.Stops, c.Riders)
	}
}

func kinds(events []lift.Event) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

func TestStepMovesAndOpens(t *testing.T) {
	c := carWithStops(0, lift.Idle, 2)
	var tr lift.Trace
	c.Step(1, &tr)
	c.Step(2, &tr)
	want := []lift.Event{{Tick: 1, Car: 1, Floor: 1, Kind: "arrive"}, {Tick: 2, Car: 1, Floor: 2, Kind: "arrive"}, {Tick: 2, Car: 1, Floor: 2, Kind: "open"}}
	if !slices.Equal(tr.Events, want) || len(c.Stops) != 0 || c.Dir != lift.Up {
		t.Fatalf("events %v, car %+v", tr.Events, c)
	}
}

func TestStepDoorCycle(t *testing.T) {
	c := carWithStops(0, lift.Idle, 2)
	var tr lift.Trace
	for tick := 1; tick <= 4; tick++ {
		c.Step(tick, &tr)
	}
	last := tr.Events[len(tr.Events)-1]
	if last != (lift.Event{Tick: 4, Car: 1, Floor: 2, Kind: "close"}) {
		t.Fatalf("events %v", tr.Events)
	}
	c.Step(5, &tr)
	if c.Dir != lift.Idle || len(tr.Events) != 4 {
		t.Fatalf("after closing: %v, %v", c.Dir, tr.Events)
	}
}

func TestStepIdle(t *testing.T) {
	c := &lift.Car{ID: 1, Floor: 4, Dir: lift.Down}
	var tr lift.Trace
	c.Step(1, &tr)
	if c.Dir != lift.Idle || len(tr.Events) != 0 || c.Floor != 4 {
		t.Fatalf("car %+v, events %v", c, tr.Events)
	}
}

func building(cars ...*lift.Car) *lift.Building {
	b := lift.NewBuilding(-2, 20)
	for _, c := range cars {
		b.AddCar(c)
	}
	return b
}

func TestAssignLowestCost(t *testing.T) {
	b := building(&lift.Car{ID: 1, Floor: 0}, &lift.Car{ID: 2, Floor: 8})
	c, err := b.Assign(lift.HallCall{Floor: 7, Dir: lift.Up})
	if err != nil || c.ID != 2 || !slices.Equal(c.Stops, []int{7}) {
		t.Fatalf("Assign = %+v, %v", c, err)
	}
}

func TestAssignTieFewerRiders(t *testing.T) {
	busy := &lift.Car{ID: 1, Floor: 2, Riders: []lift.Rider{{ID: 7, To: 2}, {ID: 8, To: 2}}}
	b := building(busy, &lift.Car{ID: 2, Floor: 6})
	if c, err := b.Assign(lift.HallCall{Floor: 4, Dir: lift.Up}); err != nil || c.ID != 2 {
		t.Fatalf("Assign = %+v, %v", c, err)
	}
}

func TestAssignTieLowerID(t *testing.T) {
	b := building(&lift.Car{ID: 2, Floor: 6}, &lift.Car{ID: 1, Floor: 2})
	if c, err := b.Assign(lift.HallCall{Floor: 4, Dir: lift.Down}); err != nil || c.ID != 1 {
		t.Fatalf("Assign = %+v, %v", c, err)
	}
}

func TestAssignPassesOverCarThree(t *testing.T) {
	b := building(&lift.Car{ID: 1, Floor: 10}, &lift.Car{ID: 3, Floor: 0})
	if c, err := b.Assign(lift.HallCall{Floor: 1, Dir: lift.Up}); err != nil || c.ID != 1 {
		t.Fatalf("Assign = %+v, %v", c, err)
	}
}

func TestAssignSkipsFull(t *testing.T) {
	full := &lift.Car{ID: 1, Floor: 0, Riders: waitingAt(0, 8)}
	b := building(full, &lift.Car{ID: 2, Floor: 9})
	if c, err := b.Assign(lift.HallCall{Floor: 1, Dir: lift.Up}); err != nil || c.ID != 2 {
		t.Fatalf("Assign = %+v, %v", c, err)
	}
}

func TestAssignNoCar(t *testing.T) {
	if _, err := building().Assign(lift.HallCall{Floor: 1, Dir: lift.Up}); !errors.Is(err, lift.ErrNoCar) {
		t.Fatalf("err = %v", err)
	}
}

func TestTickUnloadsThenBoards(t *testing.T) {
	c := carWithStops(0, lift.Idle, 2)
	c.Riders = []lift.Rider{{ID: 5, From: 0, To: 2}}
	b := building(c)
	b.Waiting[2] = []lift.Rider{{ID: 6, From: 2, To: 5, CalledAt: 1}}
	var tr lift.Trace
	b.Tick(1, &tr)
	b.Tick(2, &tr)
	if got := kinds(tr.Events); !slices.Equal(got, []string{"arrive", "arrive", "open", "leave", "board"}) {
		t.Fatalf("events %v", got)
	}
	if !slices.Equal(ids(c.Riders), []int{6}) || c.Riders[0].BoardedAt != 2 || len(b.Waiting[2]) != 0 {
		t.Fatalf("aboard %+v, waiting %v", c.Riders, b.Waiting[2])
	}
}

func TestRun(t *testing.T) {
	b := building(carWithStops(0, lift.Idle, 1))
	tr := b.Run(1, 4)
	want := []lift.Event{{Tick: 1, Car: 1, Floor: 1, Kind: "arrive"}, {Tick: 1, Car: 1, Floor: 1, Kind: "open"}, {Tick: 3, Car: 1, Floor: 1, Kind: "close"}}
	if !slices.Equal(tr.Events, want) {
		t.Fatalf("events %v", tr.Events)
	}
}

func TestExport(t *testing.T) {
	tr := lift.Trace{Events: []lift.Event{{Tick: 1, Car: 1, Floor: -1, Kind: "arrive"}, {Tick: 12, Car: 2, Floor: 13, Kind: "open"}}}
	want := "trace v2\n00001 C1 B1 arrive\n00012 C2 14 open\n"
	if got := tr.Export(); got != want {
		t.Fatalf("Export = %q", got)
	}
}

func TestExportEmpty(t *testing.T) {
	if got := (&lift.Trace{}).Export(); got != "trace v2\n" {
		t.Fatalf("Export = %q", got)
	}
}

func TestParseTraceLine(t *testing.T) {
	got, err := lift.ParseTraceLine("00042 C2 14 open")
	if err != nil || got != (lift.Event{Tick: 42, Car: 2, Floor: 13, Kind: "open"}) {
		t.Fatalf("ParseTraceLine = %+v, %v", got, err)
	}
	if _, err := lift.ParseTraceLine("trace v2"); !errors.Is(err, lift.ErrBadLine) {
		t.Errorf("header: %v", err)
	}
}

func TestWaitStats(t *testing.T) {
	riders := []lift.Rider{{CalledAt: 1, BoardedAt: 4}, {CalledAt: 2, BoardedAt: 6}}
	if avg, longest := lift.WaitStats(riders); avg != 4 || longest != 4 {
		t.Errorf("WaitStats = %d, %d", avg, longest)
	}
	riders = append(riders, lift.Rider{CalledAt: 5, BoardedAt: 5})
	if avg, longest := lift.WaitStats(riders); avg != 2 || longest != 4 {
		t.Errorf("WaitStats = %d, %d", avg, longest)
	}
	if avg, longest := lift.WaitStats(nil); avg != 0 || longest != 0 {
		t.Errorf("no riders: %d, %d", avg, longest)
	}
}

func TestIdleCars(t *testing.T) {
	b := building(&lift.Car{ID: 4, Stops: []int{3}}, &lift.Car{ID: 2, Dir: lift.Up}, &lift.Car{ID: 3, DoorTicks: 1}, &lift.Car{ID: 1}, &lift.Car{ID: 5})
	if got := b.IdleCars(); !slices.Equal(got, []int{1, 5}) {
		t.Fatalf("IdleCars = %v", got)
	}
}

func TestFireRecall(t *testing.T) {
	passenger := carWithStops(4, lift.Up, 5, 9)
	passenger.Riders = []lift.Rider{{ID: 1, From: 4, To: 9}}
	freight := &lift.Car{ID: 3, Floor: 2, Stops: []int{4}}
	b := building(passenger, freight)
	b.FireRecall()
	if !slices.Equal(passenger.Stops, []int{0}) || len(passenger.Riders) != 1 {
		t.Errorf("passenger car: %+v", passenger)
	}
	if !slices.Equal(freight.Stops, []int{-1}) {
		t.Errorf("car 3: %+v", freight)
	}
}

func TestDispatch(t *testing.T) {
	var p lift.Pending
	p.Add(lift.HallCall{Floor: 4, Dir: lift.Up, At: 1})
	p.Add(lift.HallCall{Floor: 2, Dir: lift.Down, At: 2})
	if n := building().Dispatch(&p); n != 0 || p.Len() != 2 {
		t.Fatalf("no cars: %d, %d left", n, p.Len())
	}
	c := &lift.Car{ID: 1, Floor: 0}
	if n := building(c).Dispatch(&p); n != 2 || p.Len() != 0 || !slices.Equal(c.Stops, []int{2, 4}) {
		t.Fatalf("Dispatch = %d, %d left, stops %v", n, p.Len(), c.Stops)
	}
}

func TestArrive(t *testing.T) {
	b := building()
	b.Arrive(lift.Rider{ID: 1, From: 3, To: 9}, 4)
	call := b.Arrive(lift.Rider{ID: 2, From: 3, To: -1}, 6)
	if call != (lift.HallCall{Floor: 3, Dir: lift.Down, At: 6}) {
		t.Errorf("call %+v", call)
	}
	got := b.Waiting[3]
	if !slices.Equal(ids(got), []int{1, 2}) || got[0].CalledAt != 4 || got[1].CalledAt != 6 {
		t.Fatalf("waiting %+v", got)
	}
}
`},
		Solution: map[string]string{
			"building.go": `package lift

import (
	"errors"
	"strconv"
	"strings"
)

// Building is one bank of cars: floors Lowest to Highest, both included, as
// building floors. Floor 0 is at street level and floors below it are
// basements. All code works in building floors; see docs/building.txt for
// what people in the building see.
type Building struct {
	Lowest, Highest int
	// Cars is kept in ascending ID order by AddCar.
	Cars []*Car
	// Waiting holds the riders waiting on each floor, in the order they came.
	Waiting map[int][]Rider
}

// NewBuilding returns a building with the given floors and no cars.
func NewBuilding(lowest, highest int) *Building {
	return &Building{Lowest: lowest, Highest: highest, Waiting: map[int][]Rider{}}
}

// Contains reports whether floor exists in the building.
func (b *Building) Contains(floor int) bool {
	return floor >= b.Lowest && floor <= b.Highest
}

// AddCar adds a car, keeping Cars in ascending ID order.
func (b *Building) AddCar(c *Car) {
	i := 0
	for i < len(b.Cars) && b.Cars[i].ID < c.ID {
		i++
	}
	b.Cars = append(b.Cars, nil)
	copy(b.Cars[i+1:], b.Cars[i:])
	b.Cars[i] = c
}

// Car returns the car with the given ID, or nil.
func (b *Building) Car(id int) *Car {
	for _, c := range b.Cars {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// ErrNoFloor is returned for a label that names no floor.
var ErrNoFloor = errors.New("no such floor")

// Label returns the label shown in the building for a building floor: G at
// street level, B1 and down for basements, and no 13, so that the floors
// from 13 up show one more than their number.
func Label(floor int) string {
	switch {
	case floor == 0:
		return "G"
	case floor < 0:
		return "B" + strconv.Itoa(-floor)
	case floor >= 13:
		return strconv.Itoa(floor + 1)
	}
	return strconv.Itoa(floor)
}

// ParseLabel returns the building floor a label stands for. A string that
// Label never returns gives ErrNoFloor.
func ParseLabel(label string) (int, error) {
	if label == "G" {
		return 0, nil
	}
	digits, basement := strings.CutPrefix(label, "B")
	n, err := strconv.Atoi(digits)
	if err != nil || n <= 0 {
		return 0, ErrNoFloor
	}
	floor := n
	switch {
	case basement:
		floor = -n
	case n > 13:
		floor = n - 1
	}
	if Label(floor) != label {
		return 0, ErrNoFloor
	}
	return floor, nil
}

// IdleCars returns the IDs of the cars that are idle, have no stops and have
// their doors shut, in ascending order.
func (b *Building) IdleCars() []int {
	var ids []int
	for _, c := range b.Cars {
		if c.Dir == Idle && len(c.Stops) == 0 && c.DoorTicks == 0 {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// freightCar is the ID of the freight car, which runs only on its own car
// calls and never takes a hall call (docs/building.txt).
const freightCar = 3

// FireRecall sends every car to its recall floor: it keeps its riders, but
// its stops are replaced by that one floor. Passenger cars go to street
// level and the freight car to the loading dock (docs/building.txt).
func (b *Building) FireRecall() {
	for _, c := range b.Cars {
		home := 0
		if c.ID == freightCar {
			home = -1
		}
		c.Stops = []int{home}
	}
}
`,
			"calls.go": `package lift

import "errors"

// HallCall is a call made on a landing: someone on Floor wants to travel in
// Dir. At is the tick the call was made.
type HallCall struct {
	Floor int
	Dir   Direction
	At    int
}

// Pending holds the hall calls no car has taken yet, oldest first.
type Pending struct {
	calls []HallCall
}

// Add queues a call. A call for a floor and direction that is already queued
// is dropped, since calling again changes nothing.
func (p *Pending) Add(c HallCall) {
	for _, q := range p.calls {
		if q.Floor == c.Floor && q.Dir == c.Dir {
			return
		}
	}
	p.calls = append(p.calls, c)
}

// Remove drops the queued call for a floor and direction and reports whether
// there was one. The other calls keep their order.
func (p *Pending) Remove(floor int, dir Direction) bool {
	for i, q := range p.calls {
		if q.Floor == floor && q.Dir == dir {
			p.calls = append(p.calls[:i], p.calls[i+1:]...)
			return true
		}
	}
	return false
}

// Len returns the number of queued calls.
func (p *Pending) Len() int { return len(p.calls) }

// Calls returns a copy of the queued calls, oldest first.
func (p *Pending) Calls() []HallCall {
	return append([]HallCall(nil), p.calls...)
}

// ErrNoCar is returned when no car can take a hall call.
var ErrNoCar = errors.New("no car can take the call")

// Cost is what sending c to a hall call costs: its ETA to the call's floor,
// plus oppositeExtra when it is moving the other way from the call.
func Cost(c *Car, call HallCall) int {
	cost := c.ETA(call.Floor)
	if c.Dir != Idle && c.Dir != call.Dir {
		cost += oppositeExtra
	}
	return cost
}

// Assign gives a hall call to the car that costs least, adds the call's
// floor to its stops and returns it. Full cars and the freight car are left
// out, and ErrNoCar means no car can take the call.
func (b *Building) Assign(call HallCall) (*Car, error) {
	var best *Car
	bestCost := 0
	for _, c := range b.Cars {
		if c.ID == freightCar || c.Load() >= capacity {
			continue
		}
		cost := Cost(c, call)
		if best == nil || cost < bestCost || cost == bestCost && better(c, best) {
			best, bestCost = c, cost
		}
	}
	if best == nil {
		return nil, ErrNoCar
	}
	best.AddStop(call.Floor)
	return best, nil
}

// better breaks a tie in cost between two cars: the one with fewer riders
// aboard wins, and then the one with the lower ID.
func better(c, than *Car) bool {
	if c.Load() != than.Load() {
		return c.Load() < than.Load()
	}
	return c.ID < than.ID
}

// Dispatch offers the queued calls to Assign, oldest first. A call that gets
// a car leaves the queue and the others stay in order. It returns how many
// calls got a car.
func (b *Building) Dispatch(p *Pending) int {
	var left []HallCall
	for _, call := range p.calls {
		if _, err := b.Assign(call); err != nil {
			left = append(left, call)
		}
	}
	n := len(p.calls) - len(left)
	p.calls = left
	return n
}
`,
			"car.go": `package lift

import (
	"fmt"
	"sort"
)

// Direction is where a car is heading.
type Direction int

// The directions a car can have. An Idle car is standing still.
const (
	Idle Direction = iota
	Up
	Down
)

// String returns "idle", "up" or "down".
func (d Direction) String() string {
	switch d {
	case Up:
		return "up"
	case Down:
		return "down"
	}
	return "idle"
}

// Rider is one passenger, travelling from one building floor to another.
type Rider struct {
	ID        int
	From, To  int
	CalledAt  int // the tick the rider called a car
	BoardedAt int // the tick the rider got on
}

// Going returns the direction of the rider's trip.
func (r Rider) Going() Direction {
	switch {
	case r.To > r.From:
		return Up
	case r.To < r.From:
		return Down
	}
	return Idle
}

// Car is one elevator car. Floor is the building floor it is at, and Stops
// holds the building floors it still has to stop at.
type Car struct {
	ID     int
	Floor  int
	Dir    Direction
	Stops  []int
	Riders []Rider
	// DoorTicks is how many more ticks the car's doors stay open.
	DoorTicks int
}

// Load returns how many riders are aboard.
func (c *Car) Load() int { return len(c.Riders) }

// HasStop reports whether floor is one of the car's stops.
func (c *Car) HasStop(floor int) bool {
	for _, s := range c.Stops {
		if s == floor {
			return true
		}
	}
	return false
}

// Unload removes the riders whose trip ends on the car's floor and returns
// them in the order they were aboard.
func (c *Car) Unload() []Rider {
	var off, stay []Rider
	for _, r := range c.Riders {
		if r.To == c.Floor {
			off = append(off, r)
		} else {
			stay = append(stay, r)
		}
	}
	c.Riders = stay
	return off
}

// String describes the car for logs, like "car 2 at 7 up", with the floor
// as the car's display shows it.
func (c *Car) String() string {
	return fmt.Sprintf("car %d at %s %s", c.ID, Label(c.Floor), c.Dir)
}

// capacity is the most riders a car takes.
const capacity = 8

// Board lets riders waiting on the car's floor get on, in the order they
// called a car. Nobody gets on a car heading against their trip, though an
// idle car takes anyone, and nobody gets on a full car. Each rider who gets
// on has BoardedAt set to tick and its To added to the car's stops. Board
// returns the riders who did not get on, in the order given.
func (c *Car) Board(waiting []Rider, tick int) []Rider {
	order := make([]int, len(waiting))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return waiting[order[a]].CalledAt < waiting[order[b]].CalledAt
	})
	on := make([]bool, len(waiting))
	for _, i := range order {
		r := waiting[i]
		if r.From != c.Floor || c.Load() >= capacity {
			continue
		}
		if c.Dir != Idle && r.Going() != c.Dir {
			continue
		}
		r.BoardedAt = tick
		c.Riders = append(c.Riders, r)
		c.AddStop(r.To)
		on[i] = true
	}
	var rest []Rider
	for i, r := range waiting {
		if !on[i] {
			rest = append(rest, r)
		}
	}
	return rest
}
`,
			"route.go": `package lift

import "slices"

// The numbers that tune how cars move and are chosen.
const (
	// doorTicks is how many ticks a stop keeps a car's doors open. Moving
	// one floor takes one tick.
	doorTicks = 2
	// oppositeExtra is added to the cost of a car that is moving in the
	// direction opposite to a call's.
	oppositeExtra = 6
)

// AddStop adds floor to the car's stops, which stay sorted from low to high
// with no floor in them twice.
func (c *Car) AddStop(floor int) {
	if i, found := slices.BinarySearch(c.Stops, floor); !found {
		c.Stops = slices.Insert(c.Stops, i, floor)
	}
}

// dropStop removes floor from the car's stops.
func (c *Car) dropStop(floor int) {
	if i, found := slices.BinarySearch(c.Stops, floor); found {
		c.Stops = slices.Delete(c.Stops, i, i+1)
	}
}

// NextStop returns the floor the car heads for next, and false when it has
// no stops. Going up it takes the nearest stop at or above its floor and
// turns around only when there is none; going down is the mirror image. An
// idle car takes the nearest stop, the lower one when two are equally near.
func (c *Car) NextStop() (int, bool) {
	if len(c.Stops) == 0 {
		return 0, false
	}
	i, found := slices.BinarySearch(c.Stops, c.Floor)
	if found {
		return c.Floor, true
	}
	// c.Stops[i] is the nearest stop above, c.Stops[i-1] the nearest below.
	above, below := i < len(c.Stops), i > 0
	switch {
	case !below:
		return c.Stops[i], true
	case !above:
		return c.Stops[i-1], true
	case c.Dir == Up:
		return c.Stops[i], true
	case c.Dir == Down:
		return c.Stops[i-1], true
	case c.Stops[i]-c.Floor < c.Floor-c.Stops[i-1]:
		return c.Stops[i], true
	}
	return c.Stops[i-1], true
}

// ETA returns the ticks until the car reaches floor, serving its stops in
// NextStop order: what is left of its door time, one tick a floor, and the
// door time of each stop it makes before floor. Once its stops run out it
// goes straight to floor.
func (c *Car) ETA(floor int) int {
	r := Car{Floor: c.Floor, Dir: c.Dir, Stops: slices.Clone(c.Stops)}
	ticks := c.DoorTicks
	for r.Floor != floor {
		next, ok := r.NextStop()
		if !ok || between(floor, r.Floor, next) {
			return ticks + abs(floor-r.Floor)
		}
		ticks += abs(next-r.Floor) + doorTicks
		r.Dir = heading(r.Floor, next, r.Dir)
		r.Floor = next
		r.dropStop(next)
	}
	return ticks
}

// Step runs one tick of the car and records what happened in t.
func (c *Car) Step(tick int, t *Trace) {
	if c.DoorTicks > 0 {
		c.DoorTicks--
		if c.DoorTicks == 0 {
			t.Add(Event{Tick: tick, Car: c.ID, Floor: c.Floor, Kind: "close"})
		}
		return
	}
	next, ok := c.NextStop()
	if !ok {
		c.Dir = Idle
		return
	}
	if next != c.Floor {
		c.Dir = heading(c.Floor, next, c.Dir)
		if c.Dir == Up {
			c.Floor++
		} else {
			c.Floor--
		}
		t.Add(Event{Tick: tick, Car: c.ID, Floor: c.Floor, Kind: "arrive"})
	}
	if c.Floor == next {
		c.dropStop(next)
		c.DoorTicks = doorTicks
		t.Add(Event{Tick: tick, Car: c.ID, Floor: c.Floor, Kind: "open"})
	}
}

func between(x, a, b int) bool { return min(a, b) <= x && x <= max(a, b) }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// heading is the direction from one floor to another, or d when they are the
// same floor.
func heading(from, to int, d Direction) Direction {
	switch {
	case to > from:
		return Up
	case to < from:
		return Down
	}
	return d
}
`,
			"sim.go": `package lift

// Arrive puts a rider who has just turned up on a landing into the building:
// CalledAt is set to tick and the rider joins the end of the queue waiting
// on its floor. It returns the hall call the rider makes.
func (b *Building) Arrive(r Rider, tick int) HallCall {
	r.CalledAt = tick
	b.Waiting[r.From] = append(b.Waiting[r.From], r)
	return HallCall{Floor: r.From, Dir: r.Going(), At: tick}
}

// Tick runs one tick of the building. Every car steps, in ascending ID
// order. Right after a car opens its doors it unloads, recording "leave" for
// each rider who gets off, and then boards from the riders waiting on its
// floor, recording "board" for each rider who gets on. Whoever is left stays
// waiting.
func (b *Building) Tick(tick int, t *Trace) {
	for _, c := range b.Cars {
		before := c.DoorTicks
		c.Step(tick, t)
		if before != 0 || c.DoorTicks == 0 {
			continue
		}
		for range c.Unload() {
			t.Add(Event{Tick: tick, Car: c.ID, Floor: c.Floor, Kind: "leave"})
		}
		aboard := c.Load()
		b.Waiting[c.Floor] = c.Board(b.Waiting[c.Floor], tick)
		for range c.Riders[aboard:] {
			t.Add(Event{Tick: tick, Car: c.ID, Floor: c.Floor, Kind: "board"})
		}
	}
}

// Run calls Tick for every tick from from to to, both included, and returns
// the trace they recorded.
func (b *Building) Run(from, to int) *Trace {
	t := &Trace{}
	for tick := from; tick <= to; tick++ {
		b.Tick(tick, t)
	}
	return t
}

// WaitStats returns the mean and the longest wait of riders who have all
// boarded. A wait runs from CalledAt to BoardedAt. The mean is rounded to the
// nearest tick, halves up, and no riders give 0, 0.
func WaitStats(riders []Rider) (avg, longest int) {
	if len(riders) == 0 {
		return 0, 0
	}
	total := 0
	for _, r := range riders {
		w := r.BoardedAt - r.CalledAt
		total += w
		longest = max(longest, w)
	}
	n := len(riders)
	return (2*total + n) / (2 * n), longest
}
`,
			"trace.go": `package lift

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Event is one thing that happened to a car during a run. Floor is the
// building floor the car was at when it happened.
type Event struct {
	Tick  int
	Car   int
	Floor int
	Kind  string // "arrive", "open", "close", "board" or "leave"
}

// Trace collects events in the order they happen.
type Trace struct {
	Events []Event
}

// Add records an event. A nil trace records nothing, so callers that do not
// care about events can pass nil.
func (t *Trace) Add(e Event) {
	if t != nil {
		t.Events = append(t.Events, e)
	}
}

// Count returns how many events of the given kind were recorded.
func (t *Trace) Count(kind string) int {
	n := 0
	for _, e := range t.Events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// ErrBadLine is returned for a line that is not an event line of an export.
var ErrBadLine = errors.New("bad trace line")

// traceHeader is the first line of an export (docs/trace-format.txt).
const traceHeader = "trace v2\n"

// Export writes the trace in the export format of docs/trace-format.txt:
// a header line, then one line per event with the tick padded to five
// digits, the car as C and its ID, and the floor's label.
func (t *Trace) Export() string {
	var sb strings.Builder
	sb.WriteString(traceHeader)
	for _, e := range t.Events {
		fmt.Fprintf(&sb, "%05d C%d %s %s\n", e.Tick, e.Car, Label(e.Floor), e.Kind)
	}
	return sb.String()
}

// ParseTraceLine reads one event line of an export back into an Event.
func ParseTraceLine(line string) (Event, error) {
	f := strings.Fields(line)
	if len(f) != 4 || len(f[0]) != 5 || !strings.HasPrefix(f[1], "C") {
		return Event{}, ErrBadLine
	}
	tick, err := strconv.Atoi(f[0])
	if err != nil {
		return Event{}, ErrBadLine
	}
	car, err := strconv.Atoi(f[1][1:])
	if err != nil {
		return Event{}, ErrBadLine
	}
	floor, err := ParseLabel(f[2])
	if err != nil {
		return Event{}, ErrBadLine
	}
	return Event{Tick: tick, Car: car, Floor: floor, Kind: f[3]}, nil
}
`,
		},
		Turns: script(
			cloud("Read docs/building.txt first. Then add `func Label(floor int) string` to building.go: the label the building shows for a building floor."),
			local("In building.go, add `var ErrNoFloor = errors.New(\"no such floor\")` and `func ParseLabel(label string) (int, error)`, the inverse of Label: the building floor a label stands for. A string that Label never returns gives ErrNoFloor."),
			local("Add `func (c *Car) AddStop(floor int)` to car.go. It puts floor into Stops, which stay sorted from low to high, with no floor in them twice."),
			cloud("Add `func (c *Car) NextStop() (int, bool)` to car.go: the floor the car heads for next, and false when it has no stops. A car going Up takes the nearest stop at or above its floor and turns around only when there is none; going Down is the mirror image. An Idle car takes the nearest stop, the lower one when two are equally near."),
			cloud("Timing: moving one floor takes one tick, and whenever a car stops, its doors stay open for 3 ticks. Add a `DoorTicks int` field to Car, the ticks its doors still stay open, and `func (c *Car) ETA(floor int) int`: the ticks until the car reaches floor, serving its stops in NextStop order. Count its remaining DoorTicks and the door time of every stop it makes before floor; once its stops run out it goes straight to floor."),
			cloud("Before we write boarding, its rules: a car takes at most 8 riders; riders get on in the order they called a car, not the order they are listed in; and nobody gets on a car heading the opposite way to their trip, while an Idle car takes everyone. For now just add `CalledAt` and `BoardedAt` int fields to Rider: the ticks when the rider called a car and when it got on."),
			local("Read docs/building.txt and tell me what in it we haven't built yet. No code changes."),
			local("Add `func (c *Car) Unload() []Rider` to car.go: it removes the riders whose To is the car's floor and returns them, in the order they were aboard. Also a rule for when we assign hall calls to cars later: if two cars cost the same, the one with fewer riders aboard gets the call, and if that's equal too, the lower ID."),
			local("Add `func (p *Pending) Remove(floor int, dir Direction) bool` to calls.go: it drops the queued call for that floor and direction and reports whether there was one. The other calls keep their order."),
			cloud("Add `func Cost(c *Car, call HallCall) int` to calls.go: what sending c to the call costs. That is the car's ETA to the call's floor, plus 6 when the car is moving (not Idle) in the direction opposite to the call's."),
			cloud("Read docs/trace-format.txt and tell me whether Event holds everything a line of it needs. No code changes."),
			cloud("car.go is getting crowded. Move AddStop, NextStop and ETA, with their helpers, into a new route.go, and give the 6 in Cost a named constant, in one const block with the door time at the top of route.go, so the tuning numbers live together. No behaviour changes."),
			local("Add `func (c *Car) String() string` to car.go for logs, like \"car 2 at 7 up\": the car's ID, its floor as the display in the car shows it, and its direction."),
			local("Add `func (c *Car) Board(waiting []Rider, tick int) []Rider` to car.go. Riders in waiting whose From is the car's floor get on as the boarding rules allow, each with BoardedAt set to tick and its To added to the car's stops. It returns the riders who did not get on, in the order given."),
			cloud("Facilities retimed the doors: from now on a stop holds a car for 2 ticks. Bring the code in line."),
			local("Add `func (c *Car) Step(tick int, t *Trace)` to route.go: one tick of the car, recording events in t. While DoorTicks is above zero, the tick only counts it down, recording \"close\" when it reaches zero. Otherwise a car with no stops goes Idle; else it sets Dir toward NextStop() and moves one floor that way, recording \"arrive\", unless it is already there. Once at that stop it drops the stop, records \"open\" and sets DoorTicks to the door time."),
			cloud("Add `var ErrNoCar = errors.New(\"no car can take the call\")` and `func (b *Building) Assign(call HallCall) (*Car, error)` to calls.go: it picks the car with the lowest Cost for the call, adds the call's floor to that car's stops and returns it. A full car can't take a call, and ErrNoCar means no car can."),
			cloud("Add `func (b *Building) Tick(tick int, t *Trace)` in a new sim.go: it steps every car in ascending ID order. Right after a car opens its doors, it unloads, recording \"leave\" for each rider off, then boards from b.Waiting on its floor, recording \"board\" for each rider on; b.Waiting keeps whoever is left there."),
			local("Add `func (t *Trace) Export() string` to trace.go: the whole trace as text, in our export format."),
			local("Add `var ErrBadLine = errors.New(\"bad trace line\")` and `func ParseTraceLine(line string) (Event, error)` to trace.go: it reads one event line of an export back into an Event. Any other line gives ErrBadLine."),
			cloud("Add `func (b *Building) Dispatch(p *Pending) int` to calls.go: it offers the queued calls to Assign, oldest first. A call that gets a car leaves the queue, and the others stay in order. It returns how many calls got a car."),
			cloud("Add `func WaitStats(riders []Rider) (avg, longest int)` to sim.go. Every rider given has boarded, and a rider's wait runs from CalledAt to BoardedAt. avg is the mean wait rounded to the nearest tick, halves up; with no riders both are 0."),
			local("Add `func (b *Building) Arrive(r Rider, tick int) HallCall` to sim.go, for a rider who turns up on a landing: set its CalledAt to tick, add it to the end of b.Waiting on its From floor, and return the hall call it makes: its floor and the direction of its trip, at tick."),
			local("Add `func (b *Building) IdleCars() []int` to building.go: the IDs of the cars that are Idle, have no stops and have their doors shut, in ascending order."),
			cloud("Add `func (b *Building) Run(from, to int) *Trace` to sim.go: it calls Tick for each tick from from to to, both included, and returns the trace they recorded."),
			local("Add `func (b *Building) FireRecall()` to building.go. On a fire recall, every car keeps its riders but drops all its stops and gets a single stop instead: the floor where a recall sends that car."),
		),
		Carries: []Carry{
			{
				What:        "labels: G at street level, B1 and B2 below, and no 13, so floors from 13 up show one more (docs/building.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{1, 12, 18, 19},
				Source:      "docs/building.txt",
				Tests:       []string{"TestParseLabel", "TestParseLabelRejects", "TestCarString", "TestExport", "TestParseTraceLine"},
				Absent:      []string{"thirteen", "13", "b1", "b2", "ground floor"},
			},
			{
				What:        "car 3 is the freight car: never assigned a hall call, and recalled to B1 (docs/building.txt)",
				Kind:        CarryDiscovered,
				Established: 6,
				Needed:      []int{16, 20, 25},
				Source:      "docs/building.txt",
				Tests:       []string{"TestAssignPassesOverCarThree", "TestFireRecall"},
				Absent:      []string{"freight", "car 3", "loading dock", "car calls"},
			},
			{
				What:        "a tie in cost goes to the car with fewer riders aboard, then to the lower ID",
				Kind:        CarryUser,
				Established: 7,
				Needed:      []int{16},
				Tests:       []string{"TestAssignTieFewerRiders", "TestAssignTieLowerID"},
				Absent:      []string{"fewer riders", "lower id", "cost the same", "equal too"},
			},
			{
				What:        "every stop costs door time, which ETA counts",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{9, 15},
				Tests:       []string{"TestETAWithStops", "TestETAAroundTheTop"},
				Absent:      []string{"3 ticks", "three ticks", "stay open", "stays open"},
			},
			{
				What:        "a car takes at most 8 riders",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{13, 16},
				Tests:       []string{"TestBoardCapacity", "TestAssignSkipsFull"},
				Absent:      []string{"8 riders", "at most 8", "takes at most"},
			},
			{
				What:        "riders board in the order they called, and never a car heading against their trip",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{13, 17},
				Tests:       []string{"TestBoardCallOrder", "TestBoardDirection"},
				Absent:      []string{"order they called", "opposite way", "takes everyone", "listed in"},
			},
			{
				What:        "the export has a trace v2 header, five-digit ticks and C before the car ID (docs/trace-format.txt)",
				Kind:        CarryDiscovered,
				Established: 10,
				Needed:      []int{18, 19},
				Source:      "docs/trace-format.txt",
				Tests:       []string{"TestExport", "TestExportEmpty", "TestParseTraceLine"},
				Absent:      []string{"trace v2", "zero-padded", "five digits", "header", "trace-format"},
			},
			{
				What:        "door time revised from 3 ticks to 2",
				Kind:        CarryUser,
				Established: 14,
				Needed:      []int{15, 24},
				Tests:       []string{"TestETAWithStops", "TestETAAroundTheTop", "TestStepDoorCycle", "TestRun"},
				Absent:      []string{"2 ticks", "two ticks", "retimed", "holds a car"},
			},
		},
	}
}
