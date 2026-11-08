package s2

func long2A01() Task {
	return Task{
		ID:     "s2-long2-a01-plant-care",
		Set:    "long",
		System: system("a houseplant care app"),
		Files: map[string]string{
			"go.mod": goMod,
			"plant.go": `package plants

import (
	"errors"
	"sort"
	"time"
)

// Plant is one houseplant. EveryDays is how often it wants water in the
// growing season. Dates are at midnight UTC, and a zero date means never.
type Plant struct {
	Name        string
	Species     string
	Room        string
	EveryDays   int
	LastWatered time.Time
}

// ErrUnknownPlant is returned when a name matches no plant.
var ErrUnknownPlant = errors.New("unknown plant")

// ErrDuplicate is returned when a plant with the same name is already there.
var ErrDuplicate = errors.New("duplicate plant")

// Collection holds the user's plants.
type Collection struct {
	plants []Plant
}

// Add adds a plant. Its name must not be taken.
func (c *Collection) Add(p Plant) error {
	for _, q := range c.plants {
		if q.Name == p.Name {
			return ErrDuplicate
		}
	}
	c.plants = append(c.plants, p)
	return nil
}

// Get returns the plant with that name.
func (c *Collection) Get(name string) (Plant, error) {
	for _, p := range c.plants {
		if p.Name == name {
			return p, nil
		}
	}
	return Plant{}, ErrUnknownPlant
}

// All returns a copy of the plants, sorted by name.
func (c *Collection) All() []Plant {
	out := append([]Plant(nil), c.plants...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
`,
			"schedule.go": `package plants

import "time"

// day is one calendar day. Every date in the app is midnight UTC, so a day
// is always this long.
const day = 24 * time.Hour

// Date returns midnight UTC on the given day.
func Date(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
}

// DaysBetween returns the whole days from a to b, negative when b is earlier.
func DaysBetween(a, b time.Time) int {
	return int(b.Sub(a) / day)
}

// NextWatering returns the date the plant is next due for water.
func NextWatering(p Plant) time.Time {
	return p.LastWatered.AddDate(0, 0, p.EveryDays)
}
`,
			"store.go": `package plants

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// dateLayout is how dates are written in a saved collection.
const dateLayout = "2006-01-02"

// Save writes the collection as text, one plant per line in the order they
// were added:
//
//	name;species;room;every;last watered
//
// A date that never happened is left empty.
func (c *Collection) Save() string {
	var b strings.Builder
	for _, p := range c.plants {
		fmt.Fprintf(&b, "%s;%s;%s;%d;%s\n", p.Name, p.Species, p.Room, p.EveryDays, saveDate(p.LastWatered))
	}
	return b.String()
}

func saveDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

func loadDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(dateLayout, s)
}

// Load reads a collection written by Save.
func Load(text string) (*Collection, error) {
	c := &Collection{}
	for n, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, ";")
		if len(f) != 5 {
			return nil, fmt.Errorf("line %d: %d fields", n+1, len(f))
		}
		every, err := strconv.Atoi(f[3])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		watered, err := loadDate(f[4])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		p := Plant{Name: f[0], Species: f[1], Room: f[2], EveryDays: every, LastWatered: watered}
		if err := c.Add(p); err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
	}
	return c, nil
}
`,
			"plant_test.go": `package plants

import "testing"

func TestAddGetAll(t *testing.T) {
	var c Collection
	for _, p := range []Plant{
		{Name: "Pothos", Room: "Hall", EveryDays: 5, LastWatered: Date(2026, 6, 1)},
		{Name: "Monstera", Room: "Lounge", EveryDays: 7, LastWatered: Date(2026, 6, 2)},
	} {
		if err := c.Add(p); err != nil {
			t.Fatal(err)
		}
	}
	if p, err := c.Get("Monstera"); err != nil || p.Room != "Lounge" {
		t.Fatalf("Get(Monstera) = %+v, %v", p, err)
	}
	if _, err := c.Get("Fig"); err != ErrUnknownPlant {
		t.Fatalf("Get(Fig) error = %v", err)
	}
	if all := c.All(); len(all) != 2 || all[0].Name != "Monstera" {
		t.Fatalf("All = %+v", all)
	}
}

func TestNextWateringInSummer(t *testing.T) {
	p := Plant{Name: "Pothos", EveryDays: 5, LastWatered: Date(2026, 6, 1)}
	if got := NextWatering(p); !got.Equal(Date(2026, 6, 6)) {
		t.Fatalf("NextWatering = %v", got)
	}
	if n := DaysBetween(Date(2026, 6, 6), Date(2026, 6, 1)); n != -5 {
		t.Fatalf("DaysBetween = %d", n)
	}
}

func TestSaveThenLoad(t *testing.T) {
	var c Collection
	c.Add(Plant{Name: "Pothos", Species: "vine", Room: "Hall", EveryDays: 5, LastWatered: Date(2026, 6, 1)})
	c.Add(Plant{Name: "Fig", EveryDays: 9})
	text := c.Save()
	back, err := Load(text)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Save(); got != text {
		t.Fatalf("round trip:\n%s\nbecame\n%s", text, got)
	}
}
`,
			"docs/care-guide.md": `# Care guide

These are the rules the app follows. The interval entered for a plant is
for the growing season; the app adjusts it as below, in this order.

## Winter

Winter is November, December, January and February, going by the day the
plant was last watered. In winter a plant goes half as long again between
waterings: multiply its interval by 3 and divide by 2, rounding a half day
up, so an interval of 5 days becomes 8.

Cacti and succulents (species "cactus" and "succulent") go dormant instead:
their interval doubles in winter.

## Ferns

Ferns (species "fern") must never dry out. Whatever the season, a fern goes
at most 7 days between waterings.

## Feeding

Feeding happens once in 28 days, and only in the feeding months, March to
September. A feeding that would fall from October to February moves to the
1 March after it. A plant that has never been fed is due for feeding on the
day you ask, if that day is in the feeding months, and otherwise on the
next 1 March.

## Light

Most houseplants want bright light without direct midday sun. The app does
not track light; this section is here for people reading the guide.

## Humidity

Central heating dries the air in winter. Grouping plants together, or
standing them on a tray of wet pebbles, helps more than misting does. The
app does not track humidity either.

## Pots

A pot needs a drainage hole. When roots come out of the bottom, move the
plant to a pot one size up, in fresh compost, and water it well.
`,
			"docs/export.md": `# Exporting the care log

The plant swap forum has a diary importer, and it rejects anything that is
not exactly in this format.

The export is plain text. Its first line is:

    plant-diary v1

After it comes one line per event in the care log, oldest first. Events on
the same day stay in the order they happened. A line has three fields
separated by a space, a slash and a space:

    05.03.2026 / Monstera / water

1. The date: day, month and year separated by dots, with two digits for the
   day and the month.
2. The plant's name, as the app stores it.
3. The kind of event, as the app names it.

Snoozes are not care, and they are left out.

The last line gives the number of event lines above it:

    events: 1

Every line ends with a newline, the last one too.

## Example

A Monstera watered and fed on 5 March and a fern watered on 9 March:

    plant-diary v1
    05.03.2026 / Monstera / water
    05.03.2026 / Monstera / feed
    09.03.2026 / Fern / water
    events: 3

The importer checks the count against the lines it read and refuses the
whole file when they differ.
`,
		},
		Hidden: map[string]string{hiddenName: `package plants_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	plants "fixture"
)

// on returns a day in 2026.
func on(month time.Month, day int) time.Time { return plants.Date(2026, month, day) }

func plant(name, room string, every int, watered time.Time) plants.Plant {
	return plants.Plant{Name: name, Room: room, EveryDays: every, LastWatered: watered}
}

func garden(t *testing.T, ps ...plants.Plant) *plants.Collection {
	t.Helper()
	c := &plants.Collection{}
	for _, p := range ps {
		if err := c.Add(p); err != nil {
			t.Fatalf("Add(%q): %v", p.Name, err)
		}
	}
	return c
}

func names(ps []plants.Plant) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestGetMatchesLooseName(t *testing.T) {
	c := garden(t, plant("Monstera", "Lounge", 7, on(6, 1)))
	p, err := c.Get("  MONSTERA ")
	if err != nil || p.Name != "Monstera" {
		t.Fatalf("Get = %+v, %v", p, err)
	}
}

func TestAddTrimsAndRejectsDuplicate(t *testing.T) {
	c := garden(t, plant(" Pothos  ", "Hall", 5, on(6, 1)))
	if got := c.All()[0].Name; got != "Pothos" {
		t.Errorf("stored name %q", got)
	}
	if err := c.Add(plant("pothos", "", 5, on(6, 1))); !errors.Is(err, plants.ErrDuplicate) {
		t.Errorf("Add(pothos) = %v", err)
	}
}

func TestRemoveLooseName(t *testing.T) {
	c := garden(t, plant("Monstera", "Lounge", 7, on(6, 1)), plant("Pothos", "Hall", 5, on(6, 1)))
	if err := c.Remove(" monstera"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("Monstera"); !errors.Is(err, plants.ErrUnknownPlant) {
		t.Fatalf("Monstera is still there: %v", err)
	}
}

func TestWaterLooseName(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)))
	if err := c.Water("BASIL ", on(6, 3)); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.Get("Basil"); !p.LastWatered.Equal(on(6, 3)) {
		t.Fatalf("LastWatered = %v", p.LastWatered)
	}
}

func TestRenameLooseName(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)), plant("Mint", "Kitchen", 3, on(6, 1)))
	if err := c.Rename("basil ", "MINT"); !errors.Is(err, plants.ErrDuplicate) {
		t.Errorf("renaming onto Mint = %v", err)
	}
	if err := c.Rename(" BASIL", " Thai Basil "); err != nil {
		t.Fatal(err)
	}
	if p, err := c.Get("Thai Basil"); err != nil || p.Name != "Thai Basil" {
		t.Fatalf("Get = %+v, %v", p, err)
	}
}

func TestIntervalWinter(t *testing.T) {
	cases := []struct {
		every   int
		watered time.Time
		want    int
	}{
		{5, on(12, 1), 8}, {4, on(2, 10), 6}, {5, on(11, 1), 8}, {5, on(3, 1), 5}, {5, on(10, 31), 5},
	}
	for _, c := range cases {
		p := plants.Plant{Name: "Pothos", EveryDays: c.every, LastWatered: c.watered}
		if got := plants.Interval(p); got != c.want {
			t.Errorf("Interval(%d days, watered %s) = %d, want %d", c.every, c.watered.Format("2 Jan"), got, c.want)
		}
	}
}

func TestIntervalCactusAndFern(t *testing.T) {
	cases := []struct {
		species string
		every   int
		watered time.Time
		want    int
	}{
		{"cactus", 10, on(1, 5), 20},
		{"succulent", 10, on(7, 1), 10},
		{"fern", 6, on(12, 1), 7},
		{"fern", 10, on(7, 1), 7},
		{"fern", 4, on(12, 1), 6},
	}
	for _, c := range cases {
		p := plants.Plant{Name: "x", Species: c.species, EveryDays: c.every, LastWatered: c.watered}
		if got := plants.Interval(p); got != c.want {
			t.Errorf("Interval(%s, %d days, watered %s) = %d, want %d", c.species, c.every, c.watered.Format("2 Jan"), got, c.want)
		}
	}
}

func TestNextWateringCountsSnoozes(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 3, on(6, 1)))
	for range 2 {
		if err := c.Snooze("Basil", on(6, 4)); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := c.Get("Basil")
	if got := plants.NextWatering(p); !got.Equal(on(6, 6)) {
		t.Fatalf("after two snoozes NextWatering = %v", got)
	}
	if err := c.Water("Basil", on(6, 6)); err != nil {
		t.Fatal(err)
	}
	p, _ = c.Get("Basil")
	if got := plants.NextWatering(p); !got.Equal(on(6, 9)) {
		t.Fatalf("after watering NextWatering = %v", got)
	}
}

func TestDueByOrder(t *testing.T) {
	c := garden(t,
		plant("Mint", "Kitchen", 4, on(6, 1)),
		plant("Basil", "Kitchen", 2, on(6, 1)),
		plant("Aloe", "Hall", 3, on(6, 2)),
		plant("Ivy", "Hall", 8, on(6, 1)),
	)
	if got, want := names(c.DueBy(on(6, 5))), []string{"Basil", "Aloe", "Mint"}; !slices.Equal(got, want) {
		t.Fatalf("DueBy = %v, want %v", got, want)
	}
}

func TestStatusOn(t *testing.T) {
	p := plant("Basil", "Kitchen", 3, on(6, 7))
	cases := []struct {
		day  int
		want plants.Status
	}{{9, plants.OK}, {10, plants.Due}, {11, plants.Overdue}}
	for _, c := range cases {
		if got := plants.StatusOn(p, on(6, c.day)); got != c.want {
			t.Errorf("StatusOn(%d June) = %v, want %v", c.day, got, c.want)
		}
	}
}

func TestStatusString(t *testing.T) {
	for s, want := range map[plants.Status]string{plants.OK: "ok", plants.Due: "due", plants.Overdue: "overdue"} {
		if got := s.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}

func TestBadInterval(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 60, on(6, 1)))
	for _, every := range []int{0, 61, -3} {
		if err := c.Add(plant("Mint", "Kitchen", every, on(6, 1))); !errors.Is(err, plants.ErrBadInterval) {
			t.Errorf("Add with %d days = %v", every, err)
		}
	}
	if err := c.SetInterval("Basil", 0); !errors.Is(err, plants.ErrBadInterval) {
		t.Errorf("SetInterval(0) = %v", err)
	}
	if err := c.SetInterval("Fig", 5); !errors.Is(err, plants.ErrUnknownPlant) {
		t.Errorf("SetInterval(Fig) = %v", err)
	}
	if err := c.SetInterval("Basil", 9); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.Get("Basil"); p.EveryDays != 9 {
		t.Errorf("EveryDays = %d", p.EveryDays)
	}
}

func TestNextFeedingFourWeeks(t *testing.T) {
	p := plants.Plant{Name: "Basil", EveryDays: 2, LastFed: on(4, 1)}
	if got := plants.NextFeeding(p, on(4, 10)); !got.Equal(on(4, 29)) {
		t.Errorf("fed 1 April: NextFeeding = %v", got)
	}
	p.LastFed = on(9, 1)
	if got := plants.NextFeeding(p, on(9, 5)); !got.Equal(on(9, 29)) {
		t.Errorf("fed 1 September: NextFeeding = %v", got)
	}
}

func TestNextFeedingWaitsForSpring(t *testing.T) {
	p := plants.Plant{Name: "Basil", EveryDays: 2, LastFed: on(9, 20)}
	if got := plants.NextFeeding(p, on(9, 25)); !got.Equal(plants.Date(2027, 3, 1)) {
		t.Fatalf("fed 20 September: NextFeeding = %v", got)
	}
}

func TestNextFeedingNeverFed(t *testing.T) {
	p := plants.Plant{Name: "Basil", EveryDays: 2}
	cases := []struct{ today, want time.Time }{
		{on(7, 15), on(7, 15)},
		{on(11, 3), plants.Date(2027, 3, 1)},
		{plants.Date(2027, 1, 10), plants.Date(2027, 3, 1)},
	}
	for _, c := range cases {
		if got := plants.NextFeeding(p, c.today); !got.Equal(c.want) {
			t.Errorf("never fed, asked %s: NextFeeding = %v", c.today.Format("2 Jan 2006"), got)
		}
	}
}

func TestSitterFirstVisit(t *testing.T) {
	got := plants.SitterDays(plants.Trip{Leave: on(3, 1), Return: on(3, 3)})
	if len(got) != 1 || !got[0].Equal(on(3, 2)) {
		t.Fatalf("SitterDays = %v", got)
	}
}

func TestSitterNeverOnReturn(t *testing.T) {
	trip := plants.Trip{Leave: on(3, 1), Return: on(3, 5)}
	got := plants.SitterDays(trip)
	if len(got) == 0 {
		t.Fatal("no visits")
	}
	for _, d := range got {
		if !d.After(trip.Leave) || !d.Before(trip.Return) {
			t.Errorf("a visit on %s", d.Format("2 Jan"))
		}
	}
}

func TestSitterEveryTwoDays(t *testing.T) {
	got := plants.SitterDays(plants.Trip{Leave: plants.Date(2027, 1, 10), Return: plants.Date(2027, 1, 20)})
	if len(got) < 3 {
		t.Fatalf("SitterDays = %v", got)
	}
	for i := 1; i < len(got); i++ {
		if n := plants.DaysBetween(got[i-1], got[i]); n != 2 {
			t.Errorf("%d days between visits %d and %d", n, i, i+1)
		}
	}
}

func TestPlanWatersDuePlants(t *testing.T) {
	c := garden(t,
		plant("Mint", "Kitchen", 1, on(5, 20)),
		plant("Basil", "Kitchen", 1, on(5, 28)),
		plant("Ivy", "Hall", 3, on(5, 30)),
	)
	got := c.Plan(plants.Trip{Leave: on(6, 1), Return: on(6, 4)})
	if len(got) != 1 || !got[0].On.Equal(on(6, 2)) || !slices.Equal(got[0].Plants, []string{"Basil", "Mint"}) {
		t.Fatalf("Plan = %+v", got)
	}
}

func TestSitterNote(t *testing.T) {
	c := garden(t, plant("Mint", "Kitchen", 1, on(5, 20)), plant("Basil", "Kitchen", 1, on(5, 28)))
	trip := plants.Trip{Leave: on(6, 1), Return: on(6, 4)}
	if got := c.SitterNote(trip); got != "Tuesday 2 June: Basil, Mint\n" {
		t.Errorf("SitterNote = %q", got)
	}
	idle := garden(t, plant("Ivy", "Hall", 9, on(5, 30)))
	if got := idle.SitterNote(trip); got != "Tuesday 2 June: nothing to water\n" {
		t.Errorf("SitterNote = %q", got)
	}
}

func TestSnoozeIsLogged(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)))
	if err := c.Snooze("Basil", on(6, 3)); err != nil {
		t.Fatal(err)
	}
	if log := c.Log(); len(log) != 1 || log[0].Plant != "Basil" || !log[0].On.Equal(on(6, 3)) {
		t.Fatalf("Log = %+v", log)
	}
}

func TestRepotIsLogged(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)))
	if err := c.Repot("Basil", on(6, 3)); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.Get("Basil"); !p.Repotted.Equal(on(6, 3)) {
		t.Errorf("Repotted = %v", p.Repotted)
	}
	if log := c.Log(); len(log) != 1 || log[0].Plant != "Basil" || !log[0].On.Equal(on(6, 3)) {
		t.Fatalf("Log = %+v", log)
	}
}

func TestWaterRoomIsLogged(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)), plant("Mint", "Kitchen", 3, on(6, 1)), plant("Ivy", "Hall", 5, on(6, 1)))
	if n := c.WaterRoom("Kitchen", on(6, 3)); n != 2 {
		t.Errorf("WaterRoom = %d", n)
	}
	if p, _ := c.Get("Mint"); !p.LastWatered.Equal(on(6, 3)) {
		t.Errorf("Mint LastWatered = %v", p.LastWatered)
	}
	log := c.Log()
	if len(log) != 2 {
		t.Fatalf("Log = %+v", log)
	}
	for _, e := range log {
		if e.Plant == "Ivy" || !e.On.Equal(on(6, 3)) {
			t.Errorf("entry %+v", e)
		}
	}
}

func diary(t *testing.T) *plants.Collection {
	t.Helper()
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)), plant("Mint", "Kitchen", 3, on(6, 1)))
	if err := c.Water("Mint", on(6, 6)); err != nil {
		t.Fatal(err)
	}
	if err := c.Water("Basil", on(6, 5)); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestExportHeaderAndCount(t *testing.T) {
	out := diary(t).Export()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("Export does not end with a newline: %q", out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 4 || lines[0] != "plant-diary v1" || lines[3] != "events: 2" {
		t.Fatalf("Export =\n%s", out)
	}
}

func TestExportLine(t *testing.T) {
	lines := strings.Split(diary(t).Export(), "\n")
	if len(lines) < 3 || lines[1] != "05.06.2026 / Basil / water" || lines[2] != "06.06.2026 / Mint / water" {
		t.Fatalf("lines = %q", lines)
	}
}

func TestExportSkipsSnoozes(t *testing.T) {
	c := diary(t)
	if err := c.Snooze("Basil", on(6, 7)); err != nil {
		t.Fatal(err)
	}
	if out := c.Export(); strings.Count(out, "\n") != 4 || !strings.HasSuffix(out, "events: 2\n") {
		t.Fatalf("Export =\n%s", out)
	}
}

func TestRemindersMarkOverdue(t *testing.T) {
	c := garden(t, plant("Mint", "Kitchen", 3, on(6, 7)), plant("Basil", "Kitchen", 2, on(6, 5)))
	if got, want := c.Reminders(on(6, 10)), []string{"Kitchen: Basil!, Mint"}; !slices.Equal(got, want) {
		t.Fatalf("Reminders = %q, want %q", got, want)
	}
}

func TestRemindersByRoom(t *testing.T) {
	c := garden(t,
		plant("Mint", "Kitchen", 3, on(6, 7)),
		plant("Basil", "Kitchen", 2, on(6, 8)),
		plant("Ivy", "Hall", 5, on(6, 5)),
		plant("Aloe", "", 4, on(6, 6)),
		plant("Palm", "Lounge", 7, on(6, 8)),
	)
	want := []string{"Hall: Ivy", "Kitchen: Basil, Mint", "Elsewhere: Aloe"}
	if got := c.Reminders(on(6, 10)); !slices.Equal(got, want) {
		t.Fatalf("Reminders = %q, want %q", got, want)
	}
}

func TestLogOldestFirst(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)), plant("Mint", "Kitchen", 3, on(6, 1)), plant("Ivy", "Hall", 5, on(6, 1)))
	c.Water("Basil", on(6, 9))
	c.Water("Mint", on(6, 3))
	c.Water("Ivy", on(6, 9))
	var got []string
	for _, e := range c.Log() {
		got = append(got, e.Plant+" "+e.Kind)
	}
	if want := []string{"Mint water", "Basil water", "Ivy water"}; !slices.Equal(got, want) {
		t.Fatalf("Log = %q, want %q", got, want)
	}
}

func TestRenameKeepsHistory(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)))
	if err := c.Water("Basil", on(6, 3)); err != nil {
		t.Fatal(err)
	}
	if err := c.Rename("Basil", "Thai Basil"); err != nil {
		t.Fatal(err)
	}
	h, err := c.History("Thai Basil")
	if err != nil || len(h) != 1 || h[0].Plant != "Thai Basil" || h[0].Kind != "water" {
		t.Fatalf("History = %+v, %v", h, err)
	}
	if _, err := c.History("Basil"); !errors.Is(err, plants.ErrUnknownPlant) {
		t.Errorf("History(Basil) = %v", err)
	}
}

func TestSummary(t *testing.T) {
	c := garden(t, plant("Mint", "Kitchen", 3, on(6, 7)), plant("Basil", "Kitchen", 2, on(6, 5)), plant("Ivy", "Hall", 9, on(6, 5)))
	if got := c.Summary(on(6, 10)); got != "3 plants, 1 due, 1 overdue" {
		t.Fatalf("Summary = %q", got)
	}
}

func TestFeedingsOutOfSeason(t *testing.T) {
	c := garden(t, plant("Fern", "Hall", 5, on(6, 1)), plant("Palm", "Lounge", 7, on(6, 1)), plant("Ivy", "Hall", 3, on(6, 1)))
	if err := c.Feed("Fern", on(9, 20)); err != nil {
		t.Fatal(err)
	}
	if err := c.Feed("Ivy", on(7, 1)); err != nil {
		t.Fatal(err)
	}
	if got := c.Feedings(on(11, 10)); !slices.Equal(got, []string{"Ivy"}) {
		t.Errorf("Feedings in November = %q", got)
	}
	if got := c.Feedings(on(7, 15)); !slices.Equal(got, []string{"Palm"}) {
		t.Errorf("Feedings in July = %q", got)
	}
}

func TestOverdueOrder(t *testing.T) {
	c := garden(t,
		plant("Basil", "Kitchen", 2, on(6, 5)),
		plant("Mint", "Kitchen", 3, on(6, 5)),
		plant("Aloe", "", 1, on(6, 6)),
		plant("Ivy", "Hall", 3, on(6, 7)),
	)
	if got, want := c.Overdue(on(6, 10)), []string{"Aloe", "Basil", "Mint"}; !slices.Equal(got, want) {
		t.Fatalf("Overdue = %q, want %q", got, want)
	}
}

func TestMoveAndRooms(t *testing.T) {
	c := garden(t, plant("Mint", "Kitchen", 3, on(6, 7)), plant("Basil", "Kitchen", 2, on(6, 5)), plant("Ivy", "", 9, on(6, 5)))
	if err := c.Move("Basil", "Hall"); err != nil {
		t.Fatal(err)
	}
	if err := c.Move("Fig", "Hall"); !errors.Is(err, plants.ErrUnknownPlant) {
		t.Errorf("Move(Fig) = %v", err)
	}
	if got := c.Rooms(); !slices.Equal(got, []string{"Hall", "Kitchen"}) {
		t.Fatalf("Rooms = %q", got)
	}
}

func TestCareActionsDropTheClock(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)))
	evening := time.Date(2026, 6, 3, 18, 30, 0, 0, time.UTC)
	if err := c.Water("Basil", evening); err != nil {
		t.Fatal(err)
	}
	if err := c.Feed("Basil", evening); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.Get("Basil"); !p.LastWatered.Equal(on(6, 3)) || !p.LastFed.Equal(on(6, 3)) {
		t.Fatalf("LastWatered %v, LastFed %v", p.LastWatered, p.LastFed)
	}
}

func TestSaveKeepsFeedingAndRepotting(t *testing.T) {
	c := garden(t, plant("Basil", "Kitchen", 2, on(6, 1)))
	if err := c.Feed("Basil", on(6, 2)); err != nil {
		t.Fatal(err)
	}
	if err := c.Repot("Basil", on(6, 3)); err != nil {
		t.Fatal(err)
	}
	back, err := plants.Load(c.Save())
	if err != nil {
		t.Fatal(err)
	}
	p, err := back.Get("Basil")
	if err != nil || !p.LastWatered.Equal(on(6, 1)) || !p.LastFed.Equal(on(6, 2)) || !p.Repotted.Equal(on(6, 3)) {
		t.Fatalf("loaded %+v, %v", p, err)
	}
}

func TestLoadOldLines(t *testing.T) {
	c, err := plants.Load("Basil;herb;Kitchen;2;2026-06-01\nFig;;;9;\n")
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Get("Basil")
	if err != nil || !p.LastWatered.Equal(on(6, 1)) || !p.LastFed.IsZero() || p.Room != "Kitchen" {
		t.Fatalf("Basil = %+v, %v", p, err)
	}
	if _, err := c.Get("Fig"); err != nil {
		t.Fatal(err)
	}
}
`},
		Solution: map[string]string{
			"plant.go": `package plants

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// Plant is one houseplant. EveryDays is how often it wants water in the
// growing season. Dates are at midnight UTC, and a zero date means never.
type Plant struct {
	Name        string
	Species     string
	Room        string
	EveryDays   int
	LastWatered time.Time
	LastFed     time.Time
	Repotted    time.Time
	Snoozes     int
}

// ErrUnknownPlant is returned when a name matches no plant.
var ErrUnknownPlant = errors.New("unknown plant")

// ErrDuplicate is returned when a plant with the same name is already there.
var ErrDuplicate = errors.New("duplicate plant")

// ErrBadInterval is returned for a watering interval outside 1 to 60 days.
var ErrBadInterval = errors.New("bad interval")

// Collection holds the user's plants and their care log.
type Collection struct {
	plants []Plant
	log    []Event
}

// key is the form in which plant names are compared.
func key(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// find returns the index of the named plant, or -1.
func (c *Collection) find(name string) int {
	for i, p := range c.plants {
		if key(p.Name) == key(name) {
			return i
		}
	}
	return -1
}

// validInterval reports whether a watering interval is within bounds.
func validInterval(days int) bool { return days >= 1 && days <= 60 }

// Add adds a plant under its name without the spaces around it. The name
// must not be taken.
func (c *Collection) Add(p Plant) error {
	if !validInterval(p.EveryDays) {
		return ErrBadInterval
	}
	p.Name = strings.TrimSpace(p.Name)
	for _, q := range c.plants {
		if key(q.Name) == key(p.Name) {
			return ErrDuplicate
		}
	}
	c.plants = append(c.plants, p)
	return nil
}

// Get returns the plant with that name.
func (c *Collection) Get(name string) (Plant, error) {
	for _, p := range c.plants {
		if key(p.Name) == key(name) {
			return p, nil
		}
	}
	return Plant{}, ErrUnknownPlant
}

// Remove deletes the named plant. Its log entries stay.
func (c *Collection) Remove(name string) error {
	i := c.find(name)
	if i < 0 {
		return ErrUnknownPlant
	}
	c.plants = append(c.plants[:i], c.plants[i+1:]...)
	return nil
}

// Rename gives a plant a name no other plant has, and moves its log entries
// over to the new name.
func (c *Collection) Rename(oldName, newName string) error {
	i := c.find(oldName)
	if i < 0 {
		return ErrUnknownPlant
	}
	if j := c.find(newName); j >= 0 && j != i {
		return ErrDuplicate
	}
	old, name := c.plants[i].Name, strings.TrimSpace(newName)
	for k := range c.log {
		if c.log[k].Plant == old {
			c.log[k].Plant = name
		}
	}
	c.plants[i].Name = name
	return nil
}

// SetInterval changes how often the plant wants water.
func (c *Collection) SetInterval(name string, days int) error {
	i := c.find(name)
	if i < 0 {
		return ErrUnknownPlant
	}
	if !validInterval(days) {
		return ErrBadInterval
	}
	c.plants[i].EveryDays = days
	return nil
}

// Move puts the plant in another room.
func (c *Collection) Move(name, room string) error {
	i := c.find(name)
	if i < 0 {
		return ErrUnknownPlant
	}
	c.plants[i].Room = room
	return nil
}

// All returns a copy of the plants, sorted by name.
func (c *Collection) All() []Plant {
	out := append([]Plant(nil), c.plants...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Rooms returns the rooms in use, each once and sorted, without the empty one.
func (c *Collection) Rooms() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range c.plants {
		if p.Room != "" && !seen[p.Room] {
			seen[p.Room] = true
			out = append(out, p.Room)
		}
	}
	sort.Strings(out)
	return out
}
`,
			"schedule.go": `package plants

import (
	"sort"
	"time"
)

// day is one calendar day. Every date in the app is midnight UTC, so a day
// is always this long.
const day = 24 * time.Hour

// Date returns midnight UTC on the given day.
func Date(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
}

// Day returns midnight UTC on t's calendar day.
func Day(t time.Time) time.Time {
	return Date(t.Year(), t.Month(), t.Day())
}

// DaysBetween returns the whole days from a to b, negative when b is earlier.
func DaysBetween(a, b time.Time) int {
	return int(b.Sub(a) / day)
}

// winter reports whether t falls in the care guide's winter months.
func winter(t time.Time) bool {
	m := t.Month()
	return m >= time.November || m <= time.February
}

// Interval returns the plant's days between waterings, adjusted for the
// season of its last watering and for its species as the care guide says.
func Interval(p Plant) int {
	n := p.EveryDays
	if winter(p.LastWatered) {
		switch p.Species {
		case "cactus", "succulent":
			n *= 2
		default:
			n = (3*n + 1) / 2
		}
	}
	if p.Species == "fern" {
		n = min(n, 7)
	}
	return n
}

// NextWatering returns the date the plant is next due for water, snoozes
// included.
func NextWatering(p Plant) time.Time {
	return p.LastWatered.AddDate(0, 0, Interval(p)+p.Snoozes)
}

// Status is where a plant stands with its watering on a given day.
type Status int

// The statuses, from fine to late.
const (
	OK Status = iota
	Due
	Overdue
)

// String returns the status as a word.
func (s Status) String() string {
	switch s {
	case Due:
		return "due"
	case Overdue:
		return "overdue"
	}
	return "ok"
}

// StatusOn returns the plant's status on a day.
func StatusOn(p Plant, today time.Time) Status {
	next := NextWatering(p)
	switch {
	case today.Before(next):
		return OK
	case today.Equal(next):
		return Due
	}
	return Overdue
}

// DueBy returns the plants whose next watering is on or before a day,
// soonest first and then by name.
func (c *Collection) DueBy(on time.Time) []Plant {
	var out []Plant
	for _, p := range c.plants {
		if !NextWatering(p).After(on) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := NextWatering(out[i]), NextWatering(out[j])
		if !a.Equal(b) {
			return a.Before(b)
		}
		return out[i].Name < out[j].Name
	})
	return out
}
`,
			"store.go": `package plants

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// dateLayout is how dates are written in a saved collection.
const dateLayout = "2006-01-02"

// Save writes the collection as text, one plant per line in the order they
// were added:
//
//	name;species;room;every;last watered;last fed;repotted
//
// A date that never happened is left empty.
func (c *Collection) Save() string {
	var b strings.Builder
	for _, p := range c.plants {
		fmt.Fprintf(&b, "%s;%s;%s;%d;%s;%s;%s\n", p.Name, p.Species, p.Room, p.EveryDays,
			saveDate(p.LastWatered), saveDate(p.LastFed), saveDate(p.Repotted))
	}
	return b.String()
}

func saveDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

func loadDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(dateLayout, s)
}

// Load reads a collection written by Save. Lines saved before feeding and
// repotting were kept have five fields, and they still load.
func Load(text string) (*Collection, error) {
	c := &Collection{}
	for n, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if line == "" {
			continue
		}
		p, err := loadPlant(strings.Split(line, ";"))
		if err == nil {
			err = c.Add(p)
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
	}
	return c, nil
}

// loadPlant reads the fields of one saved line.
func loadPlant(f []string) (Plant, error) {
	if len(f) == 5 {
		f = append(f, "", "")
	}
	if len(f) != 7 {
		return Plant{}, fmt.Errorf("%d fields", len(f))
	}
	every, err := strconv.Atoi(f[3])
	if err != nil {
		return Plant{}, err
	}
	p := Plant{Name: f[0], Species: f[1], Room: f[2], EveryDays: every}
	for i, at := range []*time.Time{&p.LastWatered, &p.LastFed, &p.Repotted} {
		if *at, err = loadDate(f[4+i]); err != nil {
			return Plant{}, err
		}
	}
	return p, nil
}
`,
			"care.go": `package plants

import (
	"sort"
	"time"
)

// Event is one entry in the care log.
type Event struct {
	On    time.Time
	Plant string
	Kind  string
}

// logEvent adds a care action for the plant at index i to the log, under the
// plant's stored name.
func (c *Collection) logEvent(i int, kind string, on time.Time) {
	c.log = append(c.log, Event{On: on, Plant: c.plants[i].Name, Kind: kind})
}

// Log returns a copy of the care log, oldest first and in the order things
// happened within a day.
func (c *Collection) Log() []Event {
	out := append([]Event(nil), c.log...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].On.Before(out[j].On) })
	return out
}

// History returns the plant's entries in the care log, oldest first.
func (c *Collection) History(name string) ([]Event, error) {
	i := c.find(name)
	if i < 0 {
		return nil, ErrUnknownPlant
	}
	var out []Event
	for _, e := range c.Log() {
		if e.Plant == c.plants[i].Name {
			out = append(out, e)
		}
	}
	return out, nil
}

// Water notes that the plant was watered on a day, which clears its snoozes.
func (c *Collection) Water(name string, on time.Time) error {
	i := c.find(name)
	if i < 0 {
		return ErrUnknownPlant
	}
	on = Day(on)
	c.plants[i].LastWatered = on
	c.plants[i].Snoozes = 0
	c.logEvent(i, "water", on)
	return nil
}

// WaterRoom waters every plant in a room on a day and returns how many.
func (c *Collection) WaterRoom(room string, on time.Time) int {
	on = Day(on)
	n := 0
	for i := range c.plants {
		if c.plants[i].Room == room {
			c.plants[i].LastWatered = on
			c.plants[i].Snoozes = 0
			c.logEvent(i, "water", on)
			n++
		}
	}
	return n
}

// Feed notes that the plant was fed on a day.
func (c *Collection) Feed(name string, on time.Time) error {
	i := c.find(name)
	if i < 0 {
		return ErrUnknownPlant
	}
	on = Day(on)
	c.plants[i].LastFed = on
	c.logEvent(i, "feed", on)
	return nil
}

// Snooze pushes the plant's next watering back one more day.
func (c *Collection) Snooze(name string, on time.Time) error {
	i := c.find(name)
	if i < 0 {
		return ErrUnknownPlant
	}
	c.plants[i].Snoozes++
	c.logEvent(i, "snooze", Day(on))
	return nil
}

// Repot notes that the plant got a new pot on a day.
func (c *Collection) Repot(name string, on time.Time) error {
	i := c.find(name)
	if i < 0 {
		return ErrUnknownPlant
	}
	on = Day(on)
	c.plants[i].Repotted = on
	c.logEvent(i, "repot", on)
	return nil
}

// NextFeeding returns the day the plant is next due for feeding, following
// the care guide.
func NextFeeding(p Plant, today time.Time) time.Time {
	next := today
	if !p.LastFed.IsZero() {
		next = p.LastFed.AddDate(0, 0, 28)
	}
	if m := next.Month(); m < time.March || m > time.September {
		year := next.Year()
		if m > time.September {
			year++
		}
		next = Date(year, time.March, 1)
	}
	return next
}
`,
			"report.go": `package plants

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Reminders returns one line per room with plants due or overdue today,
// rooms in alphabetical order, then a line for the plants without a room.
func (c *Collection) Reminders(today time.Time) []string {
	rooms := map[string][]Plant{}
	for _, p := range c.plants {
		if StatusOn(p, today) != OK {
			rooms[p.Room] = append(rooms[p.Room], p)
		}
	}
	var order []string
	for room := range rooms {
		if room != "" {
			order = append(order, room)
		}
	}
	sort.Strings(order)
	if _, ok := rooms[""]; ok {
		order = append(order, "")
	}
	var out []string
	for _, room := range order {
		out = append(out, reminderLine(room, rooms[room], today))
	}
	return out
}

// reminderLine writes one room's reminder, marking the overdue plants.
func reminderLine(room string, ps []Plant, today time.Time) string {
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.Name
		if StatusOn(p, today) == Overdue {
			parts[i] += "!"
		}
	}
	if room == "" {
		room = "Elsewhere"
	}
	return room + ": " + strings.Join(parts, ", ")
}

// Summary counts the plants and how many of them are due and overdue today.
func (c *Collection) Summary(today time.Time) string {
	due, overdue := 0, 0
	for _, p := range c.plants {
		switch StatusOn(p, today) {
		case Due:
			due++
		case Overdue:
			overdue++
		}
	}
	return fmt.Sprintf("%d plants, %d due, %d overdue", len(c.plants), due, overdue)
}

// Feedings returns the names of the plants whose next feeding is on or
// before today, sorted.
func (c *Collection) Feedings(today time.Time) []string {
	var out []string
	for _, p := range c.plants {
		if !NextFeeding(p, today).After(today) {
			out = append(out, p.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Overdue returns the names of the plants overdue today, the longest overdue
// first and then by name.
func (c *Collection) Overdue(today time.Time) []string {
	var late []Plant
	for _, p := range c.plants {
		if StatusOn(p, today) == Overdue {
			late = append(late, p)
		}
	}
	sort.Slice(late, func(i, j int) bool {
		a, b := NextWatering(late[i]), NextWatering(late[j])
		if !a.Equal(b) {
			return a.Before(b)
		}
		return late[i].Name < late[j].Name
	})
	out := make([]string, len(late))
	for i, p := range late {
		out[i] = p.Name
	}
	return out
}

// Export writes the care log in the forum's diary format, docs/export.md.
func (c *Collection) Export() string {
	var b strings.Builder
	b.WriteString("plant-diary v1\n")
	n := 0
	for _, e := range c.Log() {
		if e.Kind == "snooze" {
			continue
		}
		fmt.Fprintf(&b, "%s / %s / %s\n", e.On.Format("02.01.2006"), e.Plant, e.Kind)
		n++
	}
	fmt.Fprintf(&b, "events: %d\n", n)
	return b.String()
}
`,
			"vacation.go": `package plants

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Trip is time away: Leave is the day we go and Return the day we are home
// again.
type Trip struct {
	Leave, Return time.Time
}

// sitterEvery is the days from one sitter visit to the next.
const sitterEvery = 2

// SitterDays returns the days the sitter comes during a trip, in order.
func SitterDays(t Trip) []time.Time {
	var out []time.Time
	for d := t.Leave.AddDate(0, 0, 1); d.Before(t.Return); d = d.AddDate(0, 0, sitterEvery) {
		out = append(out, d)
	}
	return out
}

// Visit is one sitter visit and the plants to water on it.
type Visit struct {
	On     time.Time
	Plants []string
}

// Plan works out what the sitter waters on each visit of a trip, given that
// we water every plant ourselves the day we leave.
func (c *Collection) Plan(t Trip) []Visit {
	ps := append([]Plant(nil), c.plants...)
	for i := range ps {
		ps[i].LastWatered, ps[i].Snoozes = t.Leave, 0
	}
	var out []Visit
	for _, d := range SitterDays(t) {
		v := Visit{On: d}
		for i := range ps {
			if !NextWatering(ps[i]).After(d) {
				ps[i].LastWatered = d
				v.Plants = append(v.Plants, ps[i].Name)
			}
		}
		sort.Strings(v.Plants)
		out = append(out, v)
	}
	return out
}

// SitterNote writes a trip's plan as a note for the sitter, one line per
// visit.
func (c *Collection) SitterNote(t Trip) string {
	var b strings.Builder
	for _, v := range c.Plan(t) {
		what := "nothing to water"
		if len(v.Plants) > 0 {
			what = strings.Join(v.Plants, ", ")
		}
		fmt.Fprintf(&b, "%s: %s\n", v.On.Format("Monday 2 January"), what)
	}
	return b.String()
}
`,
		},
		Turns: script(
			cloud("This week we're building out the plant app, so start by reading plant.go and schedule.go. One rule from me about plant names: people type them sloppily, so a name given to the app matches a plant whatever its upper or lower case and any spaces around it, and Add stores a name with the spaces around it taken off. Make `Get` and `Add`, including its duplicate check, work that way."),
			local("Add `func (c *Collection) Remove(name string) error` to plant.go. It deletes the named plant, or returns ErrUnknownPlant when there is no such plant."),
			cloud("Read docs/care-guide.md. Then add `func Interval(p Plant) int` to schedule.go: the plant's days between waterings after the guide's watering adjustments, and make `NextWatering` use it. The rest of the guide can wait."),
			cloud("Add `func (c *Collection) DueBy(on time.Time) []Plant` to schedule.go: the plants whose next watering is on or before that day, soonest first, then by name. And so you know for the vacation work later this week: the sitter's first visit is the day after we leave, then one every 3 days, and never on the day we're back or after it."),
			local("Add `func Status(p Plant, today time.Time) string` to schedule.go: \"ok\" before the plant's next watering day, \"due\" on that day and \"overdue\" after it. And for when we write reminders: in a reminder, an overdue plant's name gets a \"!\" right after it."),
			local("Create care.go with `func (c *Collection) Water(name string, on time.Time) error`. It sets the plant's LastWatered to that day, or returns ErrUnknownPlant."),
			local("Add `func (c *Collection) Rename(oldName, newName string) error` to plant.go. It returns ErrUnknownPlant when oldName matches no plant and ErrDuplicate when newName is already another plant's name; otherwise it stores the new name the way Add stores names."),
			cloud("We need a care log. In care.go add `type Event struct { On time.Time; Plant, Kind string }`, keep the log in Collection, and add `func (c *Collection) Log() []Event`: a copy, oldest first, in the order things happened within a day. Water adds an Event with Kind \"water\" under the plant's stored name. From now on every care action we add gets logged the same way, with a kind of its own."),
			local("Read docs/export.md. The forum's diary importer will take our care log in that format later. Tell me what in it our Event already covers and what it doesn't. No code changes."),
			local("Add a `LastFed time.Time` field to Plant, and `func (c *Collection) Feed(name string, on time.Time) error` to care.go, which sets it to that day."),
			cloud("Add `func (c *Collection) Snooze(name string, on time.Time) error` to care.go. Each snooze pushes the plant's next watering back one more day, and watering clears them: keep the count in a new `Snoozes int` field on Plant and make NextWatering include it."),
			cloud("Intervals need bounds. `Add` should refuse an EveryDays below 1 or above 60 with a new `ErrBadInterval`, and add `func (c *Collection) SetInterval(name string, days int) error` to plant.go, which changes it within the same bounds."),
			local("Add `func NextFeeding(p Plant, today time.Time) time.Time` to care.go: the day the plant is next due for feeding."),
			local("Create vacation.go with `type Trip struct { Leave, Return time.Time }`, the day we go and the day we're home again, and `func SitterDays(t Trip) []time.Time`: the days the sitter comes, in order."),
			cloud("Create report.go with `func (c *Collection) Reminders(today time.Time) []string`: one line per room that has plants due or overdue that day, like \"Kitchen: Basil, Mint\", rooms in alphabetical order and names sorted within each room. Plants without a room come after all the rooms, on a line headed \"Elsewhere\"."),
			local("Add `func (c *Collection) Move(name, room string) error` to plant.go: it puts the plant in that room, or returns ErrUnknownPlant."),
			local("Add `func (c *Collection) History(name string) ([]Event, error)` to care.go: that plant's entries from Log(), oldest first, or ErrUnknownPlant."),
			cloud("Status strings are compared in several places now. Make it `type Status int` with constants OK, Due and Overdue and `func (s Status) String() string` giving the old words, rename the function to `StatusOn(p Plant, today time.Time) Status`, and update every caller."),
			cloud("Add `func (c *Collection) Export() string` to report.go: the care log in the forum importer's format. Separately, and don't change anything for it yet: our plant sitter changes next month, and the new one comes every second day."),
			local("Add a `Repotted time.Time` field to Plant, and `func (c *Collection) Repot(name string, on time.Time) error` to care.go, which sets it to that day."),
			local("Add `func (c *Collection) WaterRoom(room string, on time.Time) int` to care.go: it waters every plant in that room on that day and returns how many it watered."),
			cloud("Add to vacation.go `type Visit struct { On time.Time; Plants []string }` and `func (c *Collection) Plan(t Trip) []Visit`: one Visit per sitter day, in order, with the names of the plants the sitter waters then, sorted. We water every plant ourselves the day we leave, and on a visit the sitter waters each plant that is due or overdue that day."),
			local("Add `func (c *Collection) Summary(today time.Time) string` to report.go, worded like \"4 plants, 1 due, 2 overdue\"."),
			local("Add `func (c *Collection) Feedings(today time.Time) []string` to report.go: the names of the plants whose next feeding is on or before today, sorted."),
			cloud("Renaming loses a plant's past: History under the new name comes back empty. Make Rename move the plant's earlier log entries over to the new name."),
			local("Add `func (c *Collection) Rooms() []string` to plant.go: the rooms in use, each once and sorted, leaving out plants with no room."),
			local("Add `func (c *Collection) Overdue(today time.Time) []string` to report.go: the names of the plants overdue today, the longest overdue first, ties by name."),
			cloud("The sitter wants a note on the fridge. Add `func (c *Collection) SitterNote(t Trip) string` to vacation.go: one line per visit of the plan, like \"Tuesday 2 June: Basil, Mint\", or \"Tuesday 2 June: nothing to water\" when a visit has nothing, each line ending in a newline."),
			cloud("Save and Load in store.go still drop LastFed and Repotted. Save them as two more fields at the end of each line, empty when never, and make sure Load still reads lines saved in the old five-field form."),
			cloud("Some callers pass a time with a clock part. Add `func Day(t time.Time) time.Time` to schedule.go, midnight UTC on t's calendar day, and make Water, Feed, Snooze, Repot and WaterRoom work with Day(on) instead of on."),
			local("Now update the vacation code for the new sitter."),
		),
		Carries: []Carry{
			{
				What:        "a plant name matches whatever its case and surrounding spaces",
				Kind:        CarryEmbodied,
				Established: 0,
				Needed:      []int{1, 5, 6, 10, 15, 16},
				Tests:       []string{"TestRemoveLooseName", "TestWaterLooseName", "TestRenameLooseName"},
				Absent:      []string{"upper", "lower case", "spaces around", "sloppil"},
			},
			{
				What:        "feeding every 28 days, March to September only, never fed means now or 1 March (docs/care-guide.md)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{12, 23},
				Source:      "docs/care-guide.md",
				Tests:       []string{"TestNextFeedingFourWeeks", "TestNextFeedingWaitsForSpring", "TestNextFeedingNeverFed", "TestFeedingsOutOfSeason"},
				Absent:      []string{"28", "march", "september", "october", "four weeks", "4 weeks", "feeding months"},
			},
			{
				What:        "the sitter first comes the day after we leave and never on the return day or after",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{13, 21, 27},
				Tests:       []string{"TestSitterFirstVisit", "TestSitterNeverOnReturn", "TestPlanWatersDuePlants", "TestSitterNote"},
				Absent:      []string{"first visit", "day after we leave", "every 3", "3 days", "three days", "we're back"},
			},
			{
				What:        "every care action goes in the log",
				Kind:        CarryUser,
				Established: 7,
				Needed:      []int{9, 10, 19, 20},
				Tests:       []string{"TestSnoozeIsLogged", "TestRepotIsLogged", "TestWaterRoomIsLogged"},
				Absent:      []string{"logged", "every care action", "log it", "record"},
			},
			{
				What:        "the diary export format: header, dotted dates, slash fields, no snoozes, a count line (docs/export.md)",
				Kind:        CarryDiscovered,
				Established: 8,
				Needed:      []int{18},
				Source:      "docs/export.md",
				Tests:       []string{"TestExportHeaderAndCount", "TestExportLine", "TestExportSkipsSnoozes"},
				Absent:      []string{"plant-diary", "events:", "slash", "dots", "two digits"},
			},
			{
				What:        "an overdue plant is marked with ! in a reminder",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{14},
				Tests:       []string{"TestRemindersMarkOverdue"},
				Absent:      []string{"\"!\"", "exclamation", "right after it"},
			},
			{
				What:        "the new sitter comes every second day (revises the earlier spacing)",
				Kind:        CarryUser,
				Established: 18,
				Needed:      []int{30},
				Tests:       []string{"TestSitterEveryTwoDays"},
				Absent:      []string{"second day", "2 days", "two days", "every other"},
			},
		},
	}
}
