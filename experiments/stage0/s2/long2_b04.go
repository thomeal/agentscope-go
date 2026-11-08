package s2

func long2B04() Task {
	return Task{
		ID:     "s2-long2-b04-wildfire-grid",
		Set:    "long",
		System: system("a grid simulator of wildfire spread for fire crews"),
		Files: map[string]string{
			"go.mod": goMod,
			"docs/crew-orders.txt": `Standing orders for line crews

1. A crew digs a firebreak one cell at a time. Digging turns the cell into
   firebreak, whatever grew there, and the cell can no longer burn.

2. Never dig in water, on a cell that is burning, or on ash.

3. Never dig next to a fire. The four cells that share a side with a
   burning cell are off limits. A cell that only touches a burning cell at
   a corner may be dug.

4. A cell that is already a firebreak needs no digging. Leave it as it is;
   that counts as done, not as refused.

5. Crews never leave the map.

Crew leaders sign off each shift's work with the incident commander before
the next shift starts.
`,
			"docs/crew-radio.txt": `Crew orders over the radio

The incident commander sends each line crew its orders for a shift as a
small text file. The crew leader reads it out and the crew carries the
orders out from the top down, one order per line:

  dig R C       dig a firebreak at row R, column C
  douse R C     drop water on row R, column C and the cells around it

R and C count from 0 at the north-west corner of the map, as on the fire
map itself, and are written as plain numbers. The words are separated by
one space each, with nothing before the first word and nothing after the
last one.

Blank lines are allowed anywhere. A line whose first character is # is a
note for the crew leader and is not an order. Nothing else may appear in
an order file.

Example:

  # shift 2, east flank
  dig 4 7
  dig 5 7

  douse 3 6

Orders the standing orders forbid are not carried out. The crew leader
radios each one back to the incident commander at the end of the shift.
`,
			"docs/map-format.txt": `Fire map format, version 2

Rangers draw maps as plain text. A map file has these lines, in this order,
each ending with a newline:

  firemap 2
  size W H       the width in cells (columns), then the height (rows)
  wind D         D is N, E, S or W, or calm when there is no wind
  H rows         each exactly W characters, the north row first
  fire R C       any number of these: cell R C is on fire at the start

Rows and columns count from 0 at the north-west corner, so R is the row and
C the column. A fire line has to name a cell on the map that can burn, and
a cell starts burning for its fuel's full time. Nothing else may appear in
a map file.

One character per cell:

  .   bare ground, nothing to burn
  ,   grass
  %   shrub
  T   trees
  #   firebreak
  ~   water

Viewers draw a running simulation with the same characters, except that a
burning cell is drawn as * and a cell that has burnt out as _.

Example:

  firemap 2
  size 4 3
  wind calm
  ,,TT
  .%#T
  ~,,T
  fire 0 0

Version 1 maps had no size line and are no longer read.
`,
			"fire_test.go": `package wildfire

import "testing"

func TestNewGridIsBare(t *testing.T) {
	g := NewGrid(3, 2)
	if len(g.Cells) != 6 || g.Wind != "" || g.Tick != 0 {
		t.Fatalf("grid %+v", g)
	}
	for _, c := range g.Cells {
		if c != (Cell{}) {
			t.Fatalf("cell %+v", c)
		}
	}
}

func TestAtAndIn(t *testing.T) {
	g := NewGrid(3, 2)
	g.At(1, 2).Fuel = Tree
	if g.Cells[5].Fuel != Tree || g.At(2, 0) != nil || g.At(0, -1) != nil || !g.In(1, 2) || g.In(1, 3) {
		t.Fatal("wrong cell")
	}
}

func TestFill(t *testing.T) {
	g := NewGrid(3, 2)
	g.Fill(1, Grass, Water, Tree, Shrub)
	if g.At(1, 0).Fuel != Grass || g.At(1, 1).Fuel != Water || g.At(1, 2).Fuel != Tree || g.At(0, 0).Fuel != Bare {
		t.Fatalf("cells %+v", g.Cells)
	}
}

func TestFillPartRows(t *testing.T) {
	g := NewGrid(4, 2)
	g.Fill(0, Tree, Tree, Tree, Tree)
	g.Fill(0, Water, Grass)
	g.Fill(1, Shrub, Shrub, Shrub, Shrub, Shrub, Shrub)
	g.Fill(2, Grass)
	want := []Fuel{Water, Grass, Tree, Tree, Shrub, Shrub, Shrub, Shrub}
	for i, f := range want {
		if g.Cells[i].Fuel != f {
			t.Fatalf("cell %d has fuel %d, want %d", i, g.Cells[i].Fuel, f)
		}
	}
}
`,
			"grid.go": `package wildfire

// Fuel is what a cell of the map holds.
type Fuel int

// The kinds of cell. Only Grass, Shrub and Tree can burn.
const (
	Bare Fuel = iota // bare ground
	Grass
	Shrub
	Tree
	Break // a firebreak
	Water
)

// Cell is one square of the map. Burning is how many more ticks the cell
// burns, and 0 when it is not on fire. Ash is set once it has burnt out.
// Fuel never changes when a cell burns.
type Cell struct {
	Fuel    Fuel
	Burning int
	Ash     bool
}

// Grid is the map: H rows of W cells. Row 0 is the north edge and column 0
// the west edge. Wind is "N", "E", "S" or "W", or "" when it is calm, and
// Tick counts the steps run so far.
type Grid struct {
	W, H  int
	Cells []Cell // row by row, north row first
	Wind  string
	Tick  int
}

// NewGrid returns a calm grid of bare ground.
func NewGrid(w, h int) *Grid {
	return &Grid{W: w, H: h, Cells: make([]Cell, w*h)}
}

// In reports whether row r, column c is on the map.
func (g *Grid) In(r, c int) bool {
	return r >= 0 && r < g.H && c >= 0 && c < g.W
}

// At returns the cell at row r, column c, or nil when it is off the map.
func (g *Grid) At(r, c int) *Cell {
	if !g.In(r, c) {
		return nil
	}
	return &g.Cells[r*g.W+c]
}

// Fill sets the fuels of the cells in row r from a list of fuels, one per
// column, for building grids in code. Missing columns are left alone.
func (g *Grid) Fill(r int, fuels ...Fuel) {
	for c, f := range fuels {
		if cell := g.At(r, c); cell != nil {
			cell.Fuel = f
		}
	}
}
`,
			"maps/ridge.map": `firemap 2
size 8 5
wind W
,,,,%%TT
,,.,%TTT
~~~#%%TT
,,,#,%%T
,,,#,,,%
fire 1 0
`,
		},
		Hidden: map[string]string{hiddenName: `package wildfire_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	wildfire "fixture"
)

var fuelOf = map[rune]wildfire.Fuel{
	'.': wildfire.Bare, ',': wildfire.Grass, '%': wildfire.Shrub,
	'T': wildfire.Tree, '#': wildfire.Break, '~': wildfire.Water,
}

// build makes a calm grid at Tick 0 from rows of map characters.
func build(rows ...string) *wildfire.Grid {
	g := wildfire.NewGrid(len(rows[0]), len(rows))
	for r, row := range rows {
		for c, ch := range row {
			g.At(r, c).Fuel = fuelOf[ch]
		}
	}
	return g
}

func burning(g *wildfire.Grid, r, c int) bool { return g.At(r, c).Burning > 0 }

func TestNeighbors(t *testing.T) {
	g := wildfire.NewGrid(3, 3)
	cases := []struct {
		r, c int
		want [][2]int
	}{
		{1, 1, [][2]int{{0, 1}, {1, 2}, {2, 1}, {1, 0}}},
		{0, 1, [][2]int{{0, 2}, {1, 1}, {0, 0}}},
		{2, 2, [][2]int{{1, 2}, {2, 1}}},
		{0, 0, [][2]int{{0, 1}, {1, 0}}},
	}
	for _, tc := range cases {
		if got := g.Neighbors(tc.r, tc.c); !slices.Equal(got, tc.want) {
			t.Errorf("Neighbors(%d, %d) = %v, want %v", tc.r, tc.c, got, tc.want)
		}
	}
}

func TestBurnable(t *testing.T) {
	cases := []struct {
		cell wildfire.Cell
		want bool
	}{
		{wildfire.Cell{Fuel: wildfire.Grass}, true},
		{wildfire.Cell{Fuel: wildfire.Shrub}, true},
		{wildfire.Cell{Fuel: wildfire.Tree}, true},
		{wildfire.Cell{Fuel: wildfire.Bare}, false},
		{wildfire.Cell{Fuel: wildfire.Break}, false},
		{wildfire.Cell{Fuel: wildfire.Water}, false},
		{wildfire.Cell{Fuel: wildfire.Grass, Burning: 1}, false},
		{wildfire.Cell{Fuel: wildfire.Tree, Ash: true}, false},
	}
	for _, tc := range cases {
		if got := tc.cell.Burnable(); got != tc.want {
			t.Errorf("%+v: Burnable() = %v", tc.cell, got)
		}
	}
}

func TestClone(t *testing.T) {
	g := build(",T")
	g.Wind, g.Tick = "S", 4
	c := g.Clone()
	if c.W != 2 || c.H != 1 || c.Wind != "S" || c.Tick != 4 || c.At(0, 1).Fuel != wildfire.Tree {
		t.Fatalf("clone %+v", c)
	}
	c.At(0, 0).Fuel = wildfire.Water
	c.At(0, 1).Burning = 2
	c.Tick = 9
	if g.At(0, 0).Fuel != wildfire.Grass || g.At(0, 1).Burning != 0 || g.Tick != 4 {
		t.Fatalf("changing the clone changed the grid: %+v", g)
	}
}

func TestRegion(t *testing.T) {
	g := build(",,T", "T,T", ",TT")
	cases := []struct {
		r, c int
		want [][2]int
	}{
		{0, 0, [][2]int{{0, 0}, {0, 1}, {1, 1}}},
		{1, 1, [][2]int{{0, 0}, {0, 1}, {1, 1}}},
		{2, 2, [][2]int{{0, 2}, {1, 2}, {2, 1}, {2, 2}}},
		{1, 0, [][2]int{{1, 0}}},
		{2, 0, [][2]int{{2, 0}}},
	}
	for _, tc := range cases {
		if got := g.Region(tc.r, tc.c); !slices.Equal(got, tc.want) {
			t.Errorf("Region(%d, %d) = %v, want %v", tc.r, tc.c, got, tc.want)
		}
	}
	if got := g.Region(3, 0); got != nil {
		t.Errorf("off the map: %v", got)
	}
}

func TestPatches(t *testing.T) {
	g := build(",,T", "T,T", ",TT")
	if g.Patches(wildfire.Grass) != 2 || g.Patches(wildfire.Tree) != 2 || g.Patches(wildfire.Water) != 0 {
		t.Fatalf("patches %d %d %d", g.Patches(wildfire.Grass), g.Patches(wildfire.Tree), g.Patches(wildfire.Water))
	}
	if n := build("~,~,~").Patches(wildfire.Water); n != 3 {
		t.Fatalf("water patches %d", n)
	}
}

func TestBurning(t *testing.T) {
	g := build(",,,,")
	g.At(0, 0).Burning = 2
	g.At(0, 1).Burning = 1
	g.At(0, 2).Ash = true
	if n := g.Burning(); n != 2 {
		t.Fatalf("Burning() = %d", n)
	}
	if n := build(",,").Burning(); n != 0 {
		t.Fatalf("nothing burning: %d", n)
	}
}

func TestAsh(t *testing.T) {
	g := build(",,,,")
	g.At(0, 0).Burning = 2
	g.At(0, 2).Ash = true
	g.At(0, 3).Ash = true
	if n := g.Ash(); n != 2 {
		t.Fatalf("Ash() = %d", n)
	}
}

func TestFront(t *testing.T) {
	g := build(",,,", "~~~", ",,,")
	g.At(0, 0).Burning = 1
	g.At(0, 1).Burning = 1
	g.At(2, 1).Burning = 1
	g.At(2, 0).Ash = true
	want := [][2]int{{0, 1}, {2, 1}}
	if got := g.Front(); !slices.Equal(got, want) {
		t.Fatalf("Front() = %v, want %v", got, want)
	}
	if got := build(",,").Front(); len(got) != 0 {
		t.Fatalf("no fire: %v", got)
	}
}

func TestString(t *testing.T) {
	g := build(",%T", "#~.")
	g.At(0, 0).Burning = 1
	g.At(0, 2).Ash = true
	if got := g.String(); got != "*%_\n#~.\n" {
		t.Fatalf("String() = %q", got)
	}
}

func TestBurnTime(t *testing.T) {
	want := map[wildfire.Fuel]int{
		wildfire.Grass: 1, wildfire.Shrub: 2, wildfire.Tree: 5,
		wildfire.Bare: 0, wildfire.Break: 0, wildfire.Water: 0,
	}
	for f, n := range want {
		if got := wildfire.BurnTime(f); got != n {
			t.Errorf("BurnTime(%d) = %d, want %d", f, got, n)
		}
	}
}

func TestIgnite(t *testing.T) {
	g := build(",T~.")
	if !g.Ignite(0, 0) || g.At(0, 0).Burning != 1 {
		t.Fatalf("grass: %+v", g.At(0, 0))
	}
	if !g.Ignite(0, 1) || g.At(0, 1).Burning != wildfire.BurnTime(wildfire.Tree) {
		t.Fatalf("trees: %+v", g.At(0, 1))
	}
	g.At(0, 1).Burning = 1
	if g.Ignite(0, 1) || g.At(0, 1).Burning != 1 {
		t.Fatal("lit a burning cell again")
	}
	if g.Ignite(0, 2) || g.Ignite(0, 3) || g.Ignite(1, 0) || g.Ignite(0, -1) {
		t.Fatal("lit water, bare ground or a cell off the map")
	}
	if g.At(0, 2).Burning != 0 || g.At(0, 3).Burning != 0 {
		t.Fatalf("cells %+v", g.Cells)
	}
	ash := build(",")
	ash.At(0, 0).Ash = true
	if ash.Ignite(0, 0) || ash.At(0, 0).Burning != 0 {
		t.Fatal("lit ash")
	}
}

const example = "firemap 2\nsize 4 3\nwind calm\n,,TT\n.%#T\n~,,T\nfire 0 0\n"

func TestParseMap(t *testing.T) {
	g, err := wildfire.ParseMap(example)
	if err != nil {
		t.Fatal(err)
	}
	if g.W != 4 || g.H != 3 || g.Wind != "" || g.Tick != 0 {
		t.Fatalf("grid %d x %d, wind %q, tick %d", g.W, g.H, g.Wind, g.Tick)
	}
	want := []wildfire.Fuel{
		wildfire.Grass, wildfire.Grass, wildfire.Tree, wildfire.Tree,
		wildfire.Bare, wildfire.Shrub, wildfire.Break, wildfire.Tree,
		wildfire.Water, wildfire.Grass, wildfire.Grass, wildfire.Tree,
	}
	for i, f := range want {
		if g.Cells[i].Fuel != f {
			t.Errorf("cell %d has fuel %d, want %d", i, g.Cells[i].Fuel, f)
		}
	}
	if g.At(0, 0).Burning != 1 || g.Burning() != 1 {
		t.Fatalf("fires %+v", g.Cells)
	}
	g, err = wildfire.ParseMap("firemap 2\nsize 2 2\nwind E\nT,\n%~\nfire 0 0\nfire 1 0\n")
	if err != nil {
		t.Fatal(err)
	}
	if g.Wind != "E" || g.At(0, 0).Burning != wildfire.BurnTime(wildfire.Tree) ||
		g.At(1, 0).Burning != wildfire.BurnTime(wildfire.Shrub) || burning(g, 0, 1) {
		t.Fatalf("grid %+v", g)
	}
}

func TestParseMapRejects(t *testing.T) {
	bad := []string{
		"",
		"firemap 1\nsize 2 1\nwind N\n,,\n",
		"size 2 1\nwind N\n,,\n",
		"firemap 2\nsize 2\nwind N\n,,\n",
		"firemap 2\nsize 2 1\nwind NE\n,,\n",
		"firemap 2\nsize 2 1\n,,\n",
		"firemap 2\nsize 2 1\nwind N\n,\n",
		"firemap 2\nsize 2 1\nwind N\n,,,\n",
		"firemap 2\nsize 2 1\nwind N\n,x\n",
		"firemap 2\nsize 2 1\nwind N\n,*\n",
		"firemap 2\nsize 2 2\nwind N\n,,\n",
		"firemap 2\nsize 2 1\nwind N\n,~\nfire 0 1\n",
		"firemap 2\nsize 2 1\nwind N\n,.\nfire 0 1\n",
		"firemap 2\nsize 2 1\nwind N\n,,\nfire 1 0\n",
		"firemap 2\nsize 2 1\nwind N\n,,\nsmoke 0 0\n",
	}
	for _, text := range bad {
		if g, err := wildfire.ParseMap(text); !errors.Is(err, wildfire.ErrBadMap) {
			t.Errorf("%q: got %v, %v", text, g, err)
		}
	}
}

func TestParseMapCRLF(t *testing.T) {
	text := "firemap 2\nsize 3 2\nwind W\n,%T\n~#.\nfire 0 2\n"
	g, err := wildfire.ParseMap(strings.ReplaceAll(text, "\n", "\x0d\n"))
	if err != nil {
		t.Fatal(err)
	}
	if g.W != 3 || g.H != 2 || g.Wind != "W" || g.At(0, 1).Fuel != wildfire.Shrub ||
		g.At(1, 1).Fuel != wildfire.Break || g.At(0, 2).Burning != wildfire.BurnTime(wildfire.Tree) {
		t.Fatalf("grid %+v", g)
	}
}

func TestStepSpreads(t *testing.T) {
	g := build(",,,", ",,,", ",,,")
	g.Ignite(1, 1)
	g.Step()
	for _, p := range [][2]int{{0, 1}, {1, 2}, {2, 1}, {1, 0}} {
		if c := g.At(p[0], p[1]); c.Burning != 1 || c.Ash {
			t.Errorf("side %v: %+v", p, c)
		}
	}
	for _, p := range [][2]int{{0, 0}, {0, 2}, {2, 0}, {2, 2}} {
		if burning(g, p[0], p[1]) {
			t.Errorf("corner %v caught fire", p)
		}
	}
	if c := g.At(1, 1); c.Burning != 0 || !c.Ash || g.Tick != 1 {
		t.Fatalf("centre %+v, tick %d", c, g.Tick)
	}
}

func TestStepSimultaneous(t *testing.T) {
	g := build(",,,,")
	g.Ignite(0, 0)
	g.Step()
	if !g.At(0, 0).Ash || g.At(0, 1).Burning != 1 || burning(g, 0, 2) || g.At(0, 2).Ash {
		t.Fatalf("after one step: %+v", g.Cells)
	}
	g.Step()
	if !g.At(0, 1).Ash || g.At(0, 2).Burning != 1 || burning(g, 0, 3) || g.Tick != 2 {
		t.Fatalf("after two steps: %+v", g.Cells)
	}
	h := build(",,,,")
	h.Ignite(0, 3)
	h.Step()
	if !h.At(0, 3).Ash || h.At(0, 2).Burning != 1 || burning(h, 0, 1) {
		t.Fatalf("westward: %+v", h.Cells)
	}
}

func TestStepBurnsOut(t *testing.T) {
	g := build("T")
	g.Ignite(0, 0)
	for i := 1; i <= 4; i++ {
		g.Step()
		if c := g.At(0, 0); c.Burning != 5-i || c.Ash {
			t.Fatalf("trees after %d steps: %+v", i, c)
		}
	}
	g.Step()
	g.Step()
	if c := g.At(0, 0); c.Burning != 0 || !c.Ash {
		t.Fatalf("trees burnt out: %+v", c)
	}
	s := build("%")
	s.Ignite(0, 0)
	s.Step()
	if c := s.At(0, 0); c.Burning != 1 || c.Ash {
		t.Fatalf("shrub after one step: %+v", c)
	}
	s.Step()
	if c := s.At(0, 0); c.Burning != 0 || !c.Ash {
		t.Fatalf("shrub after two steps: %+v", c)
	}
}

func TestDownwind(t *testing.T) {
	cases := map[string][2]int{"N": {1, 0}, "S": {-1, 0}, "E": {0, -1}, "W": {0, 1}, "": {0, 0}, "NE": {0, 0}}
	for wind, want := range cases {
		if dr, dc := wildfire.Downwind(wind); dr != want[0] || dc != want[1] {
			t.Errorf("Downwind(%q) = %d, %d, want %v", wind, dr, dc, want)
		}
	}
}

func TestStepWind(t *testing.T) {
	g := build(",,,,,", ",,,,,", ",,,,,", ",,~,,", ",,,,,")
	g.Wind = "N"
	g.Ignite(2, 2)
	g.Step()
	if !burning(g, 4, 2) || burning(g, 0, 2) || !burning(g, 1, 2) {
		t.Fatalf("north wind:\n%v", g.Cells)
	}
	h := build(",,,,,", ",,,,,", ",,,,,", ",,,,,", ",,,,,")
	h.Wind = "E"
	h.Ignite(2, 2)
	h.Step()
	if !burning(h, 2, 0) || burning(h, 2, 4) || burning(h, 0, 2) || burning(h, 4, 2) {
		t.Fatalf("east wind:\n%v", h.Cells)
	}
}

func TestBurntPercent(t *testing.T) {
	g := build(",,%~")
	g.At(0, 0).Ash = true
	g.At(0, 1).Burning = 1
	if p := g.BurntPercent(); p != 66 {
		t.Fatalf("BurntPercent() = %d, want 66", p)
	}
	h := build("T~.#")
	h.At(0, 0).Burning = 3
	if p := h.BurntPercent(); p != 100 {
		t.Fatalf("all burning: %d", p)
	}
	if p := build("~.#").BurntPercent(); p != 0 {
		t.Fatalf("no fuel: %d", p)
	}
}

func TestReport(t *testing.T) {
	g := build(",,,")
	g.At(0, 0).Ash = true
	g.At(0, 1).Burning = 1
	g.Tick = 3
	if got := g.Report(); got != "tick 3: 1 burning, 66% burnt" {
		t.Fatalf("Report() = %q", got)
	}
	if got := build("~,").Report(); got != "tick 0: 0 burning, 0% burnt" {
		t.Fatalf("fresh grid: %q", got)
	}
}

func TestRun(t *testing.T) {
	g := build("T")
	g.Ignite(0, 0)
	if n := g.Run(100); n != 5 || g.Burning() != 0 || g.Tick != 5 {
		t.Fatalf("Run(100) = %d, tick %d", n, g.Tick)
	}
	h := build("T")
	h.Ignite(0, 0)
	if n := h.Run(2); n != 2 || !burning(h, 0, 0) {
		t.Fatalf("Run(2) = %d", n)
	}
	if n := build(",,,").Run(10); n != 0 {
		t.Fatalf("nothing burning: %d", n)
	}
}

func TestDigAllowed(t *testing.T) {
	g := build(",%T.", ",,,,", ",,,,")
	g.Ignite(2, 0)
	for c := 0; c < 4; c++ {
		if err := g.Dig(0, c); err != nil || g.At(0, c).Fuel != wildfire.Break {
			t.Errorf("Dig(0, %d): %v, %+v", c, err, g.At(0, c))
		}
	}
	h := build("#,")
	h.Ignite(0, 1)
	if err := h.Dig(0, 0); err != nil || h.At(0, 0).Fuel != wildfire.Break || !burning(h, 0, 1) {
		t.Fatalf("an existing firebreak next to the fire: %v", err)
	}
}

func TestDigRefused(t *testing.T) {
	g := build("~,,,,", ",,,,,", ",,,,,")
	g.Ignite(2, 4)
	g.At(0, 2).Ash = true
	before := slices.Clone(g.Cells)
	for _, p := range [][2]int{{0, 0}, {2, 4}, {0, 2}, {1, 4}, {2, 3}, {3, 0}, {-1, 1}, {0, 5}} {
		if err := g.Dig(p[0], p[1]); !errors.Is(err, wildfire.ErrCannotDig) {
			t.Errorf("Dig%v: %v", p, err)
		}
	}
	if !slices.Equal(g.Cells, before) {
		t.Fatalf("a refused dig changed the grid: %+v", g.Cells)
	}
}

func TestDigCorner(t *testing.T) {
	g := build(",,", ",,")
	g.Ignite(0, 0)
	if err := g.Dig(1, 1); err != nil || g.At(1, 1).Fuel != wildfire.Break {
		t.Fatalf("corner: %v", err)
	}
}

func TestArrival(t *testing.T) {
	g := build(",,,,,")
	g.Ignite(0, 0)
	cases := []struct{ c, limit, want int }{
		{0, 10, 0}, {1, 10, 1}, {3, 10, 3}, {4, 4, 4}, {4, 3, -1},
	}
	for _, tc := range cases {
		if got := g.Arrival(0, tc.c, tc.limit); got != tc.want {
			t.Errorf("Arrival(0, %d, %d) = %d, want %d", tc.c, tc.limit, got, tc.want)
		}
	}
	if g.Tick != 0 || burning(g, 0, 1) || !burning(g, 0, 0) {
		t.Fatalf("Arrival changed the grid: %+v", g)
	}
	w := build(",~,")
	w.Ignite(0, 0)
	if got := w.Arrival(0, 2, 10); got != -1 {
		t.Fatalf("across water: %d", got)
	}
}

func TestBestBreak(t *testing.T) {
	g := build("~~~~~~", ",,,,,,", "~~~~~~")
	g.Ignite(1, 0)
	p, ok := g.BestBreak([][2]int{{1, 1}, {1, 4}, {1, 3}}, 20)
	if !ok || p != [2]int{1, 3} {
		t.Fatalf("BestBreak = %v, %v", p, ok)
	}
	if g.Tick != 0 || g.At(1, 3).Fuel != wildfire.Grass || g.Burning() != 1 {
		t.Fatalf("BestBreak changed the grid: %+v", g)
	}
	if _, ok := g.BestBreak([][2]int{{1, 1}, {0, 2}}, 20); ok {
		t.Fatal("found a cell the crews may not dig")
	}
	h := build(",,,,,")
	h.Ignite(0, 2)
	if p, ok := h.BestBreak([][2]int{{0, 4}, {0, 0}}, 20); !ok || p != [2]int{0, 4} {
		t.Fatalf("tie: %v, %v", p, ok)
	}
}

func TestDouse(t *testing.T) {
	g := build(",,,", ",,,", ",,,")
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			g.Ignite(r, c)
		}
	}
	if n := g.Douse(0, 0); n != 3 {
		t.Fatalf("Douse(0, 0) = %d", n)
	}
	for _, p := range [][2]int{{0, 0}, {0, 1}, {1, 0}} {
		if c := g.At(p[0], p[1]); c.Burning != 0 || c.Ash {
			t.Errorf("%v: %+v", p, c)
		}
	}
	if !burning(g, 1, 1) || g.Douse(0, 0) != 0 {
		t.Fatal("doused too much")
	}
	if n := g.Douse(1, 1); n != 3 {
		t.Fatalf("Douse(1, 1) = %d", n)
	}
}

func TestMapFile(t *testing.T) {
	g := build(",%T", "#~.")
	g.Wind = "W"
	g.Ignite(0, 0)
	g.Ignite(0, 2)
	got, err := g.MapFile()
	if want := "firemap 2\nsize 3 2\nwind W\n,%T\n#~.\nfire 0 0\nfire 0 2\n"; err != nil || got != want {
		t.Fatalf("MapFile() = %q, %v", got, err)
	}
	back, err := wildfire.ParseMap(got)
	if err != nil || back.Wind != "W" || !slices.Equal(back.Cells, g.Cells) {
		t.Fatalf("read back: %+v, %v", back, err)
	}
	if got, err := build(",.").MapFile(); err != nil || got != "firemap 2\nsize 2 1\nwind calm\n,.\n" {
		t.Fatalf("no wind: %q, %v", got, err)
	}
}

func TestMapFileRefuses(t *testing.T) {
	g := build(",,")
	g.At(0, 0).Ash = true
	if _, err := g.MapFile(); !errors.Is(err, wildfire.ErrBadMap) {
		t.Fatalf("ash: %v", err)
	}
	h := build("T,")
	h.At(0, 0).Burning = wildfire.BurnTime(wildfire.Tree) - 1
	if _, err := h.MapFile(); !errors.Is(err, wildfire.ErrBadMap) {
		t.Fatalf("part burnt: %v", err)
	}
}

func TestArrivalMap(t *testing.T) {
	g := build(",,~,", ",,,,")
	g.Ignite(0, 0)
	if got, want := g.ArrivalMap(10), []int{0, 1, -1, 5, 1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Fatalf("ArrivalMap(10) = %v, want %v", got, want)
	}
	if got, want := g.ArrivalMap(4), []int{0, 1, -1, -1, 1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Fatalf("ArrivalMap(4) = %v, want %v", got, want)
	}
	if g.Tick != 0 || burning(g, 0, 1) {
		t.Fatalf("ArrivalMap changed the grid: %+v", g)
	}
}

func TestTimeline(t *testing.T) {
	g := build(",,,")
	g.Ignite(0, 0)
	want := []string{
		"tick 0: 1 burning, 33% burnt",
		"tick 1: 1 burning, 66% burnt",
		"tick 2: 1 burning, 100% burnt",
		"tick 3: 0 burning, 100% burnt",
	}
	if got := g.Timeline(10); !slices.Equal(got, want) {
		t.Fatalf("Timeline(10) = %q", got)
	}
	if got := g.Timeline(1); !slices.Equal(got, want[:2]) {
		t.Fatalf("Timeline(1) = %q", got)
	}
	if g.Tick != 0 || !burning(g, 0, 0) {
		t.Fatalf("Timeline changed the grid: %+v", g)
	}
}

func TestParseOrders(t *testing.T) {
	got, err := wildfire.ParseOrders("# shift 2, east flank\ndig 4 7\n\ndouse 3 6\n#dig 1 1\ndig 0 0")
	want := []wildfire.Order{{Kind: "dig", R: 4, C: 7}, {Kind: "douse", R: 3, C: 6}, {Kind: "dig", R: 0, C: 0}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("ParseOrders = %v, %v", got, err)
	}
	if got, err := wildfire.ParseOrders(""); err != nil || len(got) != 0 {
		t.Fatalf("empty file: %v, %v", got, err)
	}
}

func TestParseOrdersRejects(t *testing.T) {
	for _, line := range []string{"dig 1", "burn 1 2", "dig 1 2 3", "dig  1 2", " dig 1 2", "Dig 1 2", "douse 1 2 ", "dig a b"} {
		if got, err := wildfire.ParseOrders(line + "\n"); !errors.Is(err, wildfire.ErrBadOrder) {
			t.Errorf("%q: got %v, %v", line, got, err)
		}
	}
	_, err := wildfire.ParseOrders("dig 1 2\n# next\nfell 1 1\n")
	if !errors.Is(err, wildfire.ErrBadOrder) || !strings.HasPrefix(err.Error(), "line 3: ") {
		t.Fatalf("error %v", err)
	}
}

func TestParseOrdersCRLF(t *testing.T) {
	text := "# east flank\ndig 4 7\n\ndouse 3 6\n"
	got, err := wildfire.ParseOrders(strings.ReplaceAll(text, "\n", "\x0d\n"))
	want := []wildfire.Order{{Kind: "dig", R: 4, C: 7}, {Kind: "douse", R: 3, C: 6}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("ParseOrders = %v, %v", got, err)
	}
}

func TestExecute(t *testing.T) {
	g := build(",,,,", ",,,,")
	g.Ignite(0, 0)
	orders := []wildfire.Order{
		{Kind: "dig", R: 0, C: 1},
		{Kind: "douse", R: 0, C: 0},
		{Kind: "dig", R: 0, C: 1},
		{Kind: "dig", R: 1, C: 3},
		{Kind: "dig", R: 5, C: 5},
	}
	want := []string{"dig 0 1: cannot dig there", "dig 5 5: cannot dig there"}
	if got := g.Execute(orders); !slices.Equal(got, want) {
		t.Fatalf("Execute = %q", got)
	}
	if g.At(0, 1).Fuel != wildfire.Break || g.At(1, 3).Fuel != wildfire.Break || burning(g, 0, 0) || g.At(0, 0).Ash {
		t.Fatalf("grid %+v", g.Cells)
	}
}
`},
		Solution: map[string]string{
			"cell.go": `package wildfire

// Fuel is what a cell of the map holds.
type Fuel int

// The kinds of cell. Only Grass, Shrub and Tree can burn.
const (
	Bare Fuel = iota // bare ground
	Grass
	Shrub
	Tree
	Break // a firebreak
	Water
)

// Cell is one square of the map. Burning is how many more ticks the cell
// burns, and 0 when it is not on fire. Ash is set once it has burnt out.
// Fuel never changes when a cell burns.
type Cell struct {
	Fuel    Fuel
	Burning int
	Ash     bool
}

// burns reports whether a fuel can burn at all.
func (f Fuel) burns() bool { return f == Grass || f == Shrub || f == Tree }

// Burnable reports whether the cell can still catch fire: grass, shrub or
// trees that are neither burning nor ash.
func (c Cell) Burnable() bool {
	return c.Fuel.burns() && c.Burning == 0 && !c.Ash
}

// How many ticks each fuel burns once it has caught fire.
const (
	grassTicks = 1
	shrubTicks = 2
	treeTicks  = 5
)

// BurnTime returns how many ticks a cell of fuel f burns once it catches
// fire, and 0 for anything that cannot burn.
func BurnTime(f Fuel) int {
	switch f {
	case Grass:
		return grassTicks
	case Shrub:
		return shrubTicks
	case Tree:
		return treeTicks
	}
	return 0
}
`,
			"crew.go": `package wildfire

import "errors"

// ErrCannotDig is returned when the standing orders forbid digging a cell.
var ErrCannotDig = errors.New("cannot dig there")

// Dig makes (r, c) a firebreak under the standing orders of
// docs/crew-orders.txt: never off the map, in water, on a burning cell or on
// ash, and never on a cell that shares a side with a burning cell. A cell
// that already is a firebreak is left as it is.
func (g *Grid) Dig(r, c int) error {
	cell := g.At(r, c)
	if cell != nil && cell.Fuel == Break {
		return nil
	}
	if cell == nil || cell.Fuel == Water || cell.Burning > 0 || cell.Ash {
		return ErrCannotDig
	}
	for _, n := range g.Neighbors(r, c) {
		if g.At(n[0], n[1]).Burning > 0 {
			return ErrCannotDig
		}
	}
	cell.Fuel = Break
	return nil
}

// Douse puts out the fire at (r, c) and in its Neighbors: each of them that
// is burning stops, without turning to ash. It returns how many fires it put
// out.
func (g *Grid) Douse(r, c int) int {
	out := 0
	for _, p := range append([][2]int{{r, c}}, g.Neighbors(r, c)...) {
		if cell := g.At(p[0], p[1]); cell != nil && cell.Burning > 0 {
			cell.Burning = 0
			out++
		}
	}
	return out
}

// BestBreak returns which of cells a crew should dig now: the one that,
// dug on a Clone that then runs for limit steps, leaves the lowest
// BurntPercent, the earliest listed on a tie. Cells that cannot be dug are
// skipped, and false means none can.
func (g *Grid) BestBreak(cells [][2]int, limit int) ([2]int, bool) {
	var best [2]int
	bestPct, found := 0, false
	for _, p := range cells {
		sim := g.Clone()
		if sim.Dig(p[0], p[1]) != nil {
			continue
		}
		sim.Run(limit)
		if pct := sim.BurntPercent(); !found || pct < bestPct {
			best, bestPct, found = p, pct, true
		}
	}
	return best, found
}
`,
			"grid.go": `package wildfire

import "slices"

// Grid is the map: H rows of W cells. Row 0 is the north edge and column 0
// the west edge. Wind is "N", "E", "S" or "W", or "" when it is calm, and
// Tick counts the steps run so far.
type Grid struct {
	W, H  int
	Cells []Cell // row by row, north row first
	Wind  string
	Tick  int
}

// NewGrid returns a calm grid of bare ground.
func NewGrid(w, h int) *Grid {
	return &Grid{W: w, H: h, Cells: make([]Cell, w*h)}
}

// In reports whether row r, column c is on the map.
func (g *Grid) In(r, c int) bool {
	return r >= 0 && r < g.H && c >= 0 && c < g.W
}

// At returns the cell at row r, column c, or nil when it is off the map.
func (g *Grid) At(r, c int) *Cell {
	if !g.In(r, c) {
		return nil
	}
	return &g.Cells[r*g.W+c]
}

// Fill sets the fuels of the cells in row r from a list of fuels, one per
// column, for building grids in code. Missing columns are left alone.
func (g *Grid) Fill(r int, fuels ...Fuel) {
	for c, f := range fuels {
		if cell := g.At(r, c); cell != nil {
			cell.Fuel = f
		}
	}
}

// Neighbors returns the cells fire can spread to from row r, column c, as
// {row, col} pairs: the four cells sharing a side, in the order north, east,
// south, west, leaving out any off the map. Nothing wraps around the edges.
func (g *Grid) Neighbors(r, c int) [][2]int {
	var out [][2]int
	for _, d := range [][2]int{{-1, 0}, {0, 1}, {1, 0}, {0, -1}} {
		if g.In(r+d[0], c+d[1]) {
			out = append(out, [2]int{r + d[0], c + d[1]})
		}
	}
	return out
}

// Clone returns a deep copy of the grid.
func (g *Grid) Clone() *Grid {
	out := *g
	out.Cells = slices.Clone(g.Cells)
	return &out
}

// Front returns the burning cells that have a Burnable cell among their
// Neighbors, north to south and west to east within a row.
func (g *Grid) Front() [][2]int {
	var out [][2]int
	for r := 0; r < g.H; r++ {
		for c := 0; c < g.W; c++ {
			if g.At(r, c).Burning == 0 {
				continue
			}
			for _, n := range g.Neighbors(r, c) {
				if g.At(n[0], n[1]).Burnable() {
					out = append(out, [2]int{r, c})
					break
				}
			}
		}
	}
	return out
}

// Ignite sets a cell burning for its fuel's time, if it is on the map and
// Burnable, and reports whether it did.
func (g *Grid) Ignite(r, c int) bool {
	cell := g.At(r, c)
	if cell == nil || !cell.Burnable() {
		return false
	}
	cell.Burning = BurnTime(cell.Fuel)
	return true
}
`,
			"mapfile.go": `package wildfire

import (
	"errors"
	"fmt"
	"strings"
)

// ErrBadMap is wrapped by every error about text that is not a map file.
var ErrBadMap = errors.New("bad fire map")

// legend is the character of each Fuel in a map file, indexed by Fuel
// (docs/map-format.txt).
const legend = ".,%T#~"

// ParseMap returns the grid a map file describes, at Tick 0, with its fires
// lit. Map files from Windows, with a carriage return before each newline,
// are read as well.
func ParseMap(text string) (*Grid, error) {
	text = strings.ReplaceAll(text, "\x0d\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) < 3 || lines[0] != "firemap 2" {
		return nil, fmt.Errorf("%w: no firemap 2 header", ErrBadMap)
	}
	var w, h int
	if _, err := fmt.Sscanf(lines[1], "size %d %d", &w, &h); err != nil || w <= 0 || h <= 0 ||
		lines[1] != fmt.Sprintf("size %d %d", w, h) || len(lines) < 3+h {
		return nil, fmt.Errorf("%w: bad size line %q", ErrBadMap, lines[1])
	}
	g := NewGrid(w, h)
	if err := g.readWind(lines[2]); err != nil {
		return nil, err
	}
	for r, row := range lines[3 : 3+h] {
		if err := g.readRow(r, row); err != nil {
			return nil, err
		}
	}
	for _, line := range lines[3+h:] {
		var r, c int
		if _, err := fmt.Sscanf(line, "fire %d %d", &r, &c); err != nil || line != fmt.Sprintf("fire %d %d", r, c) || !g.Ignite(r, c) {
			return nil, fmt.Errorf("%w: bad fire line %q", ErrBadMap, line)
		}
	}
	return g, nil
}

// readWind sets the wind from the wind line of a map file.
func (g *Grid) readWind(line string) error {
	switch line {
	case "wind N", "wind E", "wind S", "wind W":
		g.Wind = line[5:]
	case "wind calm":
	default:
		return fmt.Errorf("%w: bad wind line %q", ErrBadMap, line)
	}
	return nil
}

// readRow sets the fuels of row r from one row of a map file.
func (g *Grid) readRow(r int, row string) error {
	if len(row) != g.W {
		return fmt.Errorf("%w: row %d has %d cells", ErrBadMap, r, len(row))
	}
	for c := range len(row) {
		f := strings.IndexByte(legend, row[c])
		if f < 0 {
			return fmt.Errorf("%w: unknown cell %q", ErrBadMap, row[c])
		}
		g.At(r, c).Fuel = Fuel(f)
	}
	return nil
}

// drawn is the character a viewer draws for a cell: the map's character,
// or * while it burns and _ once it is ash.
func drawn(c Cell) byte {
	switch {
	case c.Burning > 0:
		return '*'
	case c.Ash:
		return '_'
	}
	return legend[c.Fuel]
}

// String draws the grid as the rows of a map file, one line per row.
func (g *Grid) String() string {
	var sb strings.Builder
	for r := 0; r < g.H; r++ {
		for c := 0; c < g.W; c++ {
			sb.WriteByte(drawn(*g.At(r, c)))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// MapFile writes g as a map file, the inverse of ParseMap, with a fire line
// for each burning cell. A grid with ash, or with a cell that has burnt part
// of its time, cannot be written as one.
func (g *Grid) MapFile() (string, error) {
	wind := g.Wind
	if wind == "" {
		wind = "calm"
	}
	var sb, fires strings.Builder
	fmt.Fprintf(&sb, "firemap 2\nsize %d %d\nwind %s\n", g.W, g.H, wind)
	for r := 0; r < g.H; r++ {
		for c := 0; c < g.W; c++ {
			cell := g.At(r, c)
			if cell.Ash || cell.Burning > 0 && cell.Burning != BurnTime(cell.Fuel) {
				return "", fmt.Errorf("%w: cell %d %d has burnt", ErrBadMap, r, c)
			}
			if cell.Burning > 0 {
				fmt.Fprintf(&fires, "fire %d %d\n", r, c)
			}
			sb.WriteByte(legend[cell.Fuel])
		}
		sb.WriteByte('\n')
	}
	return sb.String() + fires.String(), nil
}
`,
			"orders.go": `package wildfire

import (
	"errors"
	"fmt"
	"strings"
)

// Order is one line of an order file (docs/crew-radio.txt): Kind is "dig"
// or "douse", at row R, column C.
type Order struct {
	Kind string
	R, C int
}

// ErrBadOrder is wrapped by every error about a line that is not an order.
var ErrBadOrder = errors.New("bad order")

// ParseOrders returns the orders in an order file, in order. Notes and
// blank lines are skipped, and files with a carriage return before each
// newline are read as well.
func ParseOrders(text string) ([]Order, error) {
	text = strings.ReplaceAll(text, "\x0d\n", "\n")
	var out []Order
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var o Order
		_, err := fmt.Sscanf(line, "%s %d %d", &o.Kind, &o.R, &o.C)
		if err != nil || o.Kind != "dig" && o.Kind != "douse" || line != fmt.Sprintf("%s %d %d", o.Kind, o.R, o.C) {
			return nil, fmt.Errorf("line %d: %w: %q", i+1, ErrBadOrder, line)
		}
		out = append(out, o)
	}
	return out, nil
}

// Execute carries out orders in turn, digging with Dig and dousing with
// Douse, and returns a line for each dig that was refused, like
// "dig 1 2: cannot dig there".
func (g *Grid) Execute(orders []Order) []string {
	var refused []string
	for _, o := range orders {
		if o.Kind == "douse" {
			g.Douse(o.R, o.C)
			continue
		}
		if err := g.Dig(o.R, o.C); err != nil {
			refused = append(refused, fmt.Sprintf("dig %d %d: %v", o.R, o.C, err))
		}
	}
	return refused
}
`,
			"region.go": `package wildfire

import "slices"

// Region returns the cells joined to (r, c) through Neighbors that have the
// same Fuel, (r, c) included, north to south and west to east within a row.
// It returns nil when (r, c) is off the map.
func (g *Grid) Region(r, c int) [][2]int {
	start := g.At(r, c)
	if start == nil {
		return nil
	}
	seen := map[[2]int]bool{{r, c}: true}
	queue := [][2]int{{r, c}}
	for i := 0; i < len(queue); i++ {
		for _, n := range g.Neighbors(queue[i][0], queue[i][1]) {
			if !seen[n] && g.At(n[0], n[1]).Fuel == start.Fuel {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	slices.SortFunc(queue, func(a, b [2]int) int {
		if a[0] != b[0] {
			return a[0] - b[0]
		}
		return a[1] - b[1]
	})
	return queue
}

// Patches returns how many separate Regions of fuel f the grid has.
func (g *Grid) Patches(f Fuel) int {
	seen := map[[2]int]bool{}
	n := 0
	for r := 0; r < g.H; r++ {
		for c := 0; c < g.W; c++ {
			if g.At(r, c).Fuel != f || seen[[2]int{r, c}] {
				continue
			}
			n++
			for _, p := range g.Region(r, c) {
				seen[p] = true
			}
		}
	}
	return n
}
`,
			"spread.go": `package wildfire

// Step runs one tick, worked out from the grid as it was at the start of the
// tick: burning cells burn down, the fire spreads to the cells around them
// and with the wind, and Tick goes up by one.
func (g *Grid) Step() {
	start := g.Clone()
	g.burnDown(start)
	for r := 0; r < g.H; r++ {
		for c := 0; c < g.W; c++ {
			if start.At(r, c).Burning > 0 {
				g.spreadAround(start, r, c)
				g.spreadDownwind(start, r, c)
			}
		}
	}
	g.Tick++
}

// burnDown takes a tick off every cell that was burning at the start, and
// turns the ones that reach 0 to ash.
func (g *Grid) burnDown(start *Grid) {
	for i := range g.Cells {
		if start.Cells[i].Burning > 0 {
			g.Cells[i].Burning--
			g.Cells[i].Ash = g.Cells[i].Burning == 0
		}
	}
}

// catch sets a cell on fire if it was Burnable at the start of the tick.
func (g *Grid) catch(start *Grid, r, c int) {
	if s := start.At(r, c); s != nil && s.Burnable() {
		g.Ignite(r, c)
	}
}

// spreadAround sets fire to the Neighbors of a cell that was burning.
func (g *Grid) spreadAround(start *Grid, r, c int) {
	for _, n := range g.Neighbors(r, c) {
		g.catch(start, n[0], n[1])
	}
}

// spreadDownwind sets fire to the cell two steps downwind of a cell that was
// burning, whatever lies in between.
func (g *Grid) spreadDownwind(start *Grid, r, c int) {
	if dr, dc := Downwind(g.Wind); dr != 0 || dc != 0 {
		g.catch(start, r+2*dr, c+2*dc)
	}
}

// Downwind returns the step, in rows and columns, along which a wind carries
// fire. A wind is named for where it blows from, so a north wind carries
// fire south. There is no step without a wind.
func Downwind(wind string) (dr, dc int) {
	switch wind {
	case "N":
		return 1, 0
	case "S":
		return -1, 0
	case "E":
		return 0, -1
	case "W":
		return 0, 1
	}
	return 0, 0
}

// Run calls Step until no cell is burning or limit steps have run, and
// returns how many steps it ran.
func (g *Grid) Run(limit int) int {
	steps := 0
	for steps < limit && g.Burning() > 0 {
		g.Step()
		steps++
	}
	return steps
}

// Arrival returns in how many steps fire reaches (r, c), worked out on a
// Clone: 0 when it is burning now and -1 when it is not burning within limit
// steps.
func (g *Grid) Arrival(r, c, limit int) int {
	sim := g.Clone()
	for steps := 0; ; steps++ {
		if cell := sim.At(r, c); cell != nil && cell.Burning > 0 {
			return steps
		}
		if steps == limit {
			return -1
		}
		sim.Step()
	}
}

// ArrivalMap returns Arrival for every cell, indexed like Cells, from a
// single run on a Clone: -1 for cells fire does not reach within limit
// steps.
func (g *Grid) ArrivalMap(limit int) []int {
	sim := g.Clone()
	out := make([]int, len(g.Cells))
	for i := range out {
		out[i] = -1
	}
	for steps := 0; ; steps++ {
		for i, c := range sim.Cells {
			if out[i] < 0 && c.Burning > 0 {
				out[i] = steps
			}
		}
		if steps == limit {
			return out
		}
		sim.Step()
	}
}
`,
			"stats.go": `package wildfire

import "fmt"

// Burning returns how many cells are on fire.
func (g *Grid) Burning() int {
	n := 0
	for _, c := range g.Cells {
		if c.Burning > 0 {
			n++
		}
	}
	return n
}

// Ash returns how many cells are ash.
func (g *Grid) Ash() int {
	n := 0
	for _, c := range g.Cells {
		if c.Ash {
			n++
		}
	}
	return n
}

// BurntPercent is the damage figure: the cells that are burning or ash, out
// of the cells whose fuel can burn, as a whole percent rounded down. It is 0
// when no cell has fuel.
func (g *Grid) BurntPercent() int {
	fuel, burnt := 0, 0
	for _, c := range g.Cells {
		if !c.Fuel.burns() {
			continue
		}
		fuel++
		if c.Burning > 0 || c.Ash {
			burnt++
		}
	}
	if fuel == 0 {
		return 0
	}
	return burnt * 100 / fuel
}

// Report is one line about the grid, like "tick 12: 3 burning, 40% burnt".
func (g *Grid) Report() string {
	return fmt.Sprintf("tick %d: %d burning, %d%% burnt", g.Tick, g.Burning(), g.BurntPercent())
}

// Timeline returns the Report line before the first step and after each
// step of a Run of up to limit steps, worked out on a Clone of g.
func (g *Grid) Timeline(limit int) []string {
	sim := g.Clone()
	out := []string{sim.Report()}
	for i := 0; i < limit && sim.Burning() > 0; i++ {
		sim.Step()
		out = append(out, sim.Report())
	}
	return out
}
`,
		},
		Turns: script(
			cloud("Burn times, for when fire spreads later: once a cell catches fire, grass burns for 1 tick, shrub for 2 and trees for 3. For now add `func (g *Grid) Neighbors(r, c int) [][2]int` to grid.go: the cells fire can spread to from row r, column c, as {row, col} pairs. Those are only the four cells sharing a side, never diagonals, in the order north, east, south, west, leaving out any that are off the map."),
			local("Read docs/map-format.txt and tell me whether Grid and Cell can hold everything a map file describes. No code changes."),
			local("Add `func (c Cell) Burnable() bool` to grid.go: whether the cell can catch fire, which means grass, shrub or trees that are neither burning nor ash. And something to keep in mind for later: the rangers' map and order files come off their laptops with Windows line endings, and whatever reads our files has to take those as well as plain ones."),
			cloud("How wind works here, for later: a wind is named for the direction it blows from, as in forecasts, so a \"N\" wind carries fire toward the south. For now add `func (g *Grid) Clone() *Grid` to grid.go: a deep copy of g, so that changing the copy's cells never changes g's."),
			cloud("Add `func (g *Grid) Region(r, c int) [][2]int` in a new region.go: the cells you can reach from (r, c) by moving to Neighbors without leaving cells of the same Fuel, (r, c) included, sorted by row and then by column. nil when (r, c) is off the map."),
			local("Add `func (g *Grid) Burning() int` in a new stats.go: how many cells of g are on fire."),
			local("Add `func (g *Grid) Front() [][2]int` to grid.go: the burning cells that have a Burnable cell among their Neighbors, sorted by row and then by column."),
			local("Add `func (g *Grid) String() string` to grid.go: the grid the way viewers draw a running simulation, one line per row from north to south, each line ending in a newline."),
			cloud("Read docs/crew-orders.txt and tell me what it will mean for the code that plans firebreaks. No code changes."),
			cloud("For the damage reports we'll need later: the burnt share counts the cells that are burning or ash, out of the cells whose Fuel is grass, shrub or trees, as a whole percent rounded down, and it is 0 when there are no such cells. For now a tidy-up: move Fuel with its constants, Cell and Burnable out of grid.go into a new cell.go. No behaviour changes."),
			local("Add `func BurnTime(f Fuel) int` to cell.go: how many ticks a cell of fuel f burns once it catches fire, using the figures I gave you at the start, and 0 for fuels that cannot burn. Then add `func (g *Grid) Ignite(r, c int) bool` to grid.go: if (r, c) is on the map and Burnable, it starts burning for BurnTime of its fuel and Ignite returns true; otherwise nothing changes and it returns false."),
			cloud("Add `var ErrBadMap = errors.New(\"bad fire map\")` and `func ParseMap(text string) (*Grid, error)` in a new mapfile.go: the grid a map file describes, at Tick 0, with its fires lit by Ignite. Any text that is not a valid map file gives an error wrapping ErrBadMap."),
			cloud("Tidy-up across grid.go and mapfile.go: move String into mapfile.go, and keep the characters of the map in one table there that both ParseMap and String use. No behaviour changes."),
			local("Add `func (g *Grid) Ash() int` to stats.go: how many cells of g are ash."),
			cloud("Add `func (g *Grid) Step()` in a new spread.go: one tick of the fire, all worked out from the grid as it was at the start of the tick. Every cell that was burning burns one tick less and turns to ash when that reaches 0. Every cell that was Burnable and is among the Neighbors of a cell that was burning is lit as Ignite does. Then Tick goes up by one. No wind yet."),
			local("Add `func Downwind(wind string) (dr, dc int)` to spread.go: the step in rows and columns from a cell to the next cell downwind of it, for a Grid's Wind. 0, 0 when there is no wind or the wind is not one we know."),
			cloud("Add wind to Step: besides spreading to its Neighbors, every cell that was burning at the start of the tick also lights the cell two steps downwind of it, if that cell was Burnable then, whatever lies in between."),
			local("Add `func (g *Grid) BurntPercent() int` to stats.go: our damage figure for the grid, as I described it before."),
			cloud("Field data from last season: trees burn for 5 ticks, not the figure we had. Grass and shrub stay as they were. Update the code."),
			local("Add `func (g *Grid) Run(limit int) int` to spread.go: it calls Step until no cell is burning or it has run limit steps, and returns how many steps it ran."),
			local("Add `var ErrCannotDig = errors.New(\"cannot dig there\")` and `func (g *Grid) Dig(r, c int) error` in a new crew.go: a crew digs a firebreak at (r, c), as far as the standing orders allow. When they forbid it, Dig returns ErrCannotDig and changes nothing."),
			cloud("Add `func (g *Grid) Arrival(r, c, limit int) int` to spread.go: in how many steps the fire reaches (r, c), meaning the cell is burning, worked out without changing g. 0 when it is burning already, and -1 when it is still not burning after limit steps."),
			local("Add `func (g *Grid) Patches(f Fuel) int` to region.go: how many separate Regions of fuel f the grid has."),
			cloud("Tidy-up in spread.go: Step has grown. Split it into small helpers, one that burns down the cells that were burning, one that spreads fire to a cell's Neighbors and one for the wind, so that Step itself is a short loop. No behaviour changes."),
			local("Add `func (g *Grid) Report() string` to stats.go: one line for the radio log, like \"tick 12: 3 burning, 40% burnt\", with the Tick, how many cells are burning and BurntPercent()."),
			cloud("Add `func (g *Grid) BestBreak(cells [][2]int, limit int) ([2]int, bool)` to crew.go: which of cells a crew should dig now. For each cell the crew may dig, dig it on a Clone and Run that for limit steps; the cell that leaves the lowest BurntPercent wins, the earliest in cells on a tie. false when the crew may dig none of them."),
			local("Add `func (g *Grid) Douse(r, c int) int` to crew.go: a water drop on (r, c) puts out that cell and its Neighbors. Each of them that is burning stops burning without becoming ash, and Douse returns how many fires it put out."),
			cloud("Add `func (g *Grid) MapFile() (string, error)` to mapfile.go: g written as a map file that ParseMap reads back to the same grid, with a fire line for each burning cell, in row and then column order. A grid with ash, or with a cell burning for less than BurnTime of its fuel, cannot be written so: return an error wrapping ErrBadMap."),
			local("Add `func (g *Grid) ArrivalMap(limit int) []int` to spread.go: Arrival for every cell at once, indexed like Cells, from one run on a Clone rather than a run per cell. Cells the fire does not reach within limit steps get -1."),
			cloud("Read docs/crew-radio.txt. Add `type Order struct { Kind string; R, C int }`, `var ErrBadOrder = errors.New(\"bad order\")` and `func ParseOrders(text string) ([]Order, error)` in a new orders.go: the orders in an order file, in order. A line that is not allowed there gives an error wrapping ErrBadOrder that starts with its line number, counted from 1, like \"line 3: \"."),
			cloud("Add `func (g *Grid) Execute(orders []Order) []string` to orders.go: it carries out orders one after another, a dig with Dig and a douse with Douse, and returns a line for each dig that was refused, the order and Dig's error, like \"dig 1 2: cannot dig there\"."),
			local("Add `func (g *Grid) Timeline(limit int) []string` to stats.go: the Report line before the first step and after each step of a Run of up to limit steps, worked out on a Clone so that g is left as it is."),
		),
		Carries: []Carry{
			{
				What:        "a wind is named for where it blows from, so a N wind carries fire south",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{15, 16},
				Tests:       []string{"TestDownwind", "TestStepWind"},
				Absent:      []string{"blows from", "forecasts", "toward the south", "named for"},
			},
			{
				What:        "burn times: grass 1 tick, shrub 2, trees 3",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{10, 14},
				Tests:       []string{"TestBurnTime", "TestStepBurnsOut"},
				Absent:      []string{"for 1 tick", "for 2 and", "for 3", "burn times"},
			},
			{
				What:        "burn time revised: trees burn for 5 ticks, grass and shrub unchanged",
				Kind:        CarryUser,
				Established: 18,
				Needed:      []int{19, 21, 25},
				Tests:       []string{"TestBurnTime", "TestStepBurnsOut", "TestRun"},
				Absent:      []string{"for 5", "5 ticks", "field data", "last season"},
			},
			{
				What:        "the map file format: firemap 2 header, size and wind lines with calm for no wind, the cell legend, fire lines, and * and _ for viewers (docs/map-format.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{7, 11, 12, 27},
				Source:      "docs/map-format.txt",
				Tests:       []string{"TestParseMap", "TestParseMapRejects", "TestString", "TestMapFile"},
				Absent:      []string{"firemap", "calm", "size w h", "map-format", "drawn as"},
			},
			{
				What:        "the standing orders: no digging in water, on burning cells or ash, or beside a burning cell; corners are fine and an existing firebreak counts as done (docs/crew-orders.txt)",
				Kind:        CarryDiscovered,
				Established: 8,
				Needed:      []int{20, 25, 30},
				Source:      "docs/crew-orders.txt",
				Tests:       []string{"TestDigAllowed", "TestDigRefused", "TestDigCorner", "TestBestBreak", "TestExecute"},
				Absent:      []string{"off limits", "share a side", "at a corner", "crew-orders", "on ash"},
			},
			{
				What:        "the damage figure: burning or ash cells out of grass, shrub and tree cells, a whole percent rounded down, 0 with no such cells",
				Kind:        CarryUser,
				Established: 9,
				Needed:      []int{17, 24, 31},
				Tests:       []string{"TestBurntPercent", "TestReport", "TestTimeline"},
				Absent:      []string{"rounded down", "whole percent", "burnt share", "or ash", "out of the cells"},
			},
			{
				What:        "map and order files may have Windows line endings",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{11, 29},
				Tests:       []string{"TestParseMapCRLF", "TestParseOrdersCRLF"},
				Absent:      []string{"line endings", "windows", "laptops", "carriage"},
			},
		},
	}
}
