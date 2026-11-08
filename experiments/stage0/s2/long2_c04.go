package s2

func long2C04() Task {
	return Task{
		ID:     "s2-long2-c04-quake-catalog",
		Set:    "long",
		System: system("an earthquake catalog tool for a seismic network"),
		Files: map[string]string{
			"go.mod": goMod,
			"event.go": `package quake

import (
	"sort"
	"time"
)

// Event is one earthquake as a catalog reports it.
type Event struct {
	ID      string
	Time    time.Time // UTC
	Lat     float64   // degrees, north positive
	Lon     float64   // degrees, east positive
	Depth   float64   // km below sea level
	Mag     float64   // as reported, on the scale MagType
	MagType string    // ML, mb, Mw, ...
	Agency  string    // the reporting agency's code
}

// ByTime sorts events by time, and events at the same time by ID.
func ByTime(events []Event) {
	sort.SliceStable(events, func(a, b int) bool {
		if !events[a].Time.Equal(events[b].Time) {
			return events[a].Time.Before(events[b].Time)
		}
		return events[a].ID < events[b].ID
	})
}
`,
			"geo.go": `package quake

import "math"

// earthRadius is the mean radius of the Earth in km.
const earthRadius = 6371.0

// radians converts degrees to radians.
func radians(d float64) float64 { return d * math.Pi / 180 }

// Distance returns the great-circle distance in km between two points given
// in decimal degrees, by the haversine formula.
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	dLat, dLon := radians(lat2-lat1), radians(lon2-lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(radians(lat1))*math.Cos(radians(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Sqrt(a))
}

// Bearing returns the initial bearing in degrees, clockwise from north, of
// the great circle from the first point to the second, in [0, 360).
func Bearing(lat1, lon1, lat2, lon2 float64) float64 {
	p1, p2, dLon := radians(lat1), radians(lat2), radians(lon2-lon1)
	y := math.Sin(dLon) * math.Cos(p2)
	x := math.Cos(p1)*math.Sin(p2) - math.Sin(p1)*math.Cos(p2)*math.Cos(dLon)
	deg := math.Atan2(y, x) * 180 / math.Pi
	return math.Mod(deg+360, 360)
}

// Destination returns the point km away from a start point along an initial
// bearing in degrees, its longitude in [-180, 180).
func Destination(lat, lon, bearing, km float64) (float64, float64) {
	d, b, p1, l1 := km/earthRadius, radians(bearing), radians(lat), radians(lon)
	p2 := math.Asin(math.Sin(p1)*math.Cos(d) + math.Cos(p1)*math.Sin(d)*math.Cos(b))
	l2 := l1 + math.Atan2(math.Sin(b)*math.Sin(d)*math.Cos(p1), math.Cos(d)-math.Sin(p1)*math.Sin(p2))
	deg := func(r float64) float64 { return r * 180 / math.Pi }
	return deg(p2), math.Mod(deg(l2)+540, 360) - 180
}
`,
			"quake_test.go": `package quake

import (
	"math"
	"testing"
	"time"
)

func TestDistance(t *testing.T) {
	if got := Distance(0, 0, 0, 1); math.Abs(got-111.195) > 0.01 {
		t.Errorf("Distance = %v", got)
	}
	if got := Distance(-18, 178, -18, 178); got != 0 {
		t.Errorf("Distance to itself = %v", got)
	}
}

func TestBearing(t *testing.T) {
	if got := Bearing(0, 0, 1, 0); math.Abs(got) > 1e-9 {
		t.Errorf("north: %v", got)
	}
	if got := Bearing(0, 0, 0, 1); math.Abs(got-90) > 1e-9 {
		t.Errorf("east: %v", got)
	}
	if got := Bearing(0, 0, 0, -1); math.Abs(got-270) > 1e-9 {
		t.Errorf("west: %v", got)
	}
}

func TestDestination(t *testing.T) {
	lat, lon := Destination(-18, 179.5, 90, Distance(-18, 179.5, -18, -179.5))
	if math.Abs(lat+18) > 0.01 || math.Abs(lon+179.5) > 0.01 {
		t.Errorf("Destination = %v, %v", lat, lon)
	}
}

func TestByTime(t *testing.T) {
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	events := []Event{{ID: "b", Time: at}, {ID: "c", Time: at.Add(-time.Hour)}, {ID: "a", Time: at}}
	ByTime(events)
	if events[0].ID != "c" || events[1].ID != "a" || events[2].ID != "b" {
		t.Fatalf("ByTime = %v", events)
	}
}
`,
			"docs/catalog-format.txt": `Catalog exchange format

Our own catalog and the ones we merge in from other agencies are exchanged
as plain text, one event per line, the fields separated by "|":

    id|time|latitude|longitude|depth|magnitude|magtype|agency

time is UTC, written like 2026-03-04T05:06:07.5Z; the fractional seconds
are optional. latitude and longitude are decimal degrees, north and east
positive. depth is in metres below sea level, because that is how the
network's database stores it, although everyone talks about depths in
kilometres. magnitude is the value on the agency's own scale, and magtype
names that scale (ML, mb, Mw and so on). agency is the two-letter code of
the reporting agency; LX is our own network.

Lines starting with "#" are comments; every file starts with one naming its
source. Numbers always use a decimal point. For example:

    # LX catalog, week 10
    lx2026e|2026-03-04T05:06:07.5Z|-17.8|178.1|13000|4.2|Mw|LX
    us7000a|2026-03-05T01:02:03Z|-20.1|-175.3|35000|5.1|mb|US

Agencies send corrected files from time to time. A corrected file replaces
the agency's earlier file for the same week as a whole; we never patch
single lines.
`,
			"docs/statistics.txt": `Magnitude statistics

Every statistic uses Mw, binned to the nearest 0.1 first.

The magnitude of completeness, Mc, is the smallest magnitude above which the
network records every earthquake in the region. We estimate it by maximum
curvature: it is the bin holding the most events.

The b-value is Aki's maximum-likelihood estimate with Utsu's correction for
binned magnitudes:

    b = log10(e) / (mean - (Mc - 0.05))

where mean is the mean of the binned magnitudes at or above Mc. With fewer
than two such magnitudes there is no estimate.

The a-value follows from the Gutenberg-Richter law, log10 N = a - b M, with
N the number of binned magnitudes at or above Mc:

    a = log10(N) + b Mc

A b-value near 1 is normal for tectonic earthquakes. A value well above 1.3
over a short window usually means a swarm rather than a main shock with
its aftershocks.
`,
			"docs/magnitudes.txt": `Converting magnitudes to Mw

The bulletin and every statistic we compute use moment magnitude, Mw.
Agencies report other scales too. Convert them like this, M being the
reported value:

    Mw    Mw = M
    ML    Mw = 0.67 M + 1.0     (local magnitude, our network)
    mb    Mw = 0.85 M + 1.03    (body-wave magnitude, other agencies)
    Md    Mw = M                (duration magnitude; no better relation)

A magnitude on any other scale cannot be converted: leave the event out of
statistics and of the bulletin rather than guess.

Round converted magnitudes to one decimal only when you print them.

Seismic moment M0, in N·m, and radiated energy E, in joules, follow from Mw:

    M0 = 10^(1.5 Mw + 9.1)
    E  = 10^(1.5 Mw + 4.8)

Moments add up and magnitudes do not: to combine earthquakes, add their
moments and convert the sum back to Mw.
`,
			"docs/bulletin.txt": `Weekly bulletin layout

The bulletin is plain text, every line ending with a newline. The first
line is

    LX bulletin <from> to <to>

with both dates written as YYYY-MM-DD. Then comes one line per earthquake:

    <date> <time>  Mw <magnitude>  <depth> km  <latitude> <longitude>  <id>

with the date and time in UTC as YYYY-MM-DD hh:mm:ss, the magnitude with
one decimal, the depth in whole kilometres, latitude and longitude with two
decimals each, and two spaces between those groups. The last line counts
the earthquakes listed: "3 earthquakes", "1 earthquake" or "no earthquakes".
For example:

    LX bulletin 2026-03-02 to 2026-03-09
    2026-03-04 05:06:07  Mw 5.4  13 km  -17.80 178.10  lx2026e
    2026-03-05 01:02:03  Mw 4.1  35 km  -20.10 -175.30  us7000a
    2 earthquakes

The bulletin goes out on Monday mornings to the civil protection office,
which forwards it to the island councils. Do not add headings, footnotes or
blank lines: their software reads it line by line.
`,
		},
		Hidden: map[string]string{hiddenName: `package quake_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	quake "fixture"
)

var t0 = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

func ev(id string, at time.Duration, lat, lon, mag float64, magType, agency string) quake.Event {
	return quake.Event{ID: id, Time: t0.Add(at), Lat: lat, Lon: lon, Depth: 10, Mag: mag, MagType: magType, Agency: agency}
}

func ids(events []quake.Event) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.ID)
	}
	return out
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

const line = "lx2026e|2026-03-04T05:06:07.5Z|-17.8|178.1|13000|4.2|Mw|LX"

func TestValidate(t *testing.T) {
	for _, e := range []quake.Event{{ID: "a", Lat: 91}, {ID: "b", Lon: -181}} {
		if err := e.Validate(); !errors.Is(err, quake.ErrBadEvent) {
			t.Errorf("%s: %v", e.ID, err)
		}
	}
	if err := (quake.Event{ID: "c", Lat: -90, Lon: 180}).Validate(); err != nil {
		t.Errorf("edges: %v", err)
	}
}

func TestWithin(t *testing.T) {
	e := ev("a", 0, 0, 0, 3, "Mw", "LX")
	if !e.Within(t0, t0.Add(time.Hour)) || e.Within(t0.Add(-time.Hour), t0) {
		t.Fatal("Within")
	}
}

func TestEnergy(t *testing.T) {
	if got := quake.Energy(6); !near(got/math.Pow(10, 13.8), 1, 1e-12) {
		t.Fatalf("Energy = %v", got)
	}
}

func TestBin(t *testing.T) {
	if quake.Bin(3.14) != 3.1 || quake.Bin(3.16) != 3.2 || quake.Bin(2.97) != 3.0 {
		t.Fatal("Bin")
	}
}

func TestReported(t *testing.T) {
	if got := ev("a", 0, 0, 0, 3.2, "ML", "LX").Reported(); got != "ML 3.2" {
		t.Errorf("Reported = %q", got)
	}
	if got := ev("b", 0, 0, 0, 4.97, "mb", "US").Reported(); got != "mb 5.0" {
		t.Errorf("Reported = %q", got)
	}
}

func TestByAgency(t *testing.T) {
	got := quake.ByAgency([]quake.Event{ev("a", 0, 0, 0, 3, "Mw", "US"), ev("b", 0, 0, 0, 3, "Mw", "LX"), ev("c", 0, 0, 0, 3, "Mw", "US")})
	if len(got) != 2 || !reflect.DeepEqual(ids(got["US"]), []string{"a", "c"}) || len(got["LX"]) != 1 {
		t.Fatalf("ByAgency = %v", got)
	}
}

func TestNearest(t *testing.T) {
	events := []quake.Event{ev("a", 0, 0, 0, 3, "Mw", "LX"), ev("b", 0, 0, 1, 3, "Mw", "LX"), ev("c", 0, 0, 2, 3, "Mw", "LX")}
	if got, ok := quake.Nearest(events, 0, 1.2); !ok || got.ID != "b" {
		t.Errorf("Nearest = %v, %v", got.ID, ok)
	}
	if _, ok := quake.Nearest(nil, 0, 0); ok {
		t.Error("Nearest of nothing")
	}
}

func TestHistogram(t *testing.T) {
	got := quake.Histogram([]float64{3.14, 3.06, 2.97, 3.31})
	if !reflect.DeepEqual(got, map[float64]int{3.1: 2, 3.0: 1, 3.3: 1}) {
		t.Fatalf("Histogram = %v", got)
	}
}

func TestParseEvent(t *testing.T) {
	got, err := quake.ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 3, 4, 5, 6, 7, 500000000, time.UTC)
	if got.ID != "lx2026e" || !got.Time.Equal(when) || got.Lat != -17.8 || got.Lon != 178.1 || got.Mag != 4.2 || got.MagType != "Mw" || got.Agency != "LX" {
		t.Fatalf("ParseEvent = %+v", got)
	}
}

func TestParseEventDepth(t *testing.T) {
	got, err := quake.ParseEvent(line)
	if err != nil || !near(got.Depth, 13, 1e-9) {
		t.Fatalf("Depth = %v, %v", got.Depth, err)
	}
}

func TestParseEventBadNumber(t *testing.T) {
	if _, err := quake.ParseEvent("x|2026-03-04T05:06:07Z|abc|178.1|13000|4.2|Mw|LX"); !errors.Is(err, quake.ErrBadEvent) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseEventFieldCount(t *testing.T) {
	if _, err := quake.ParseEvent("x|2026-03-04T05:06:07Z|-17.8|178.1|13000|4.2|Mw"); !errors.Is(err, quake.ErrBadEvent) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseEventOutOfRange(t *testing.T) {
	if _, err := quake.ParseEvent("x|2026-03-04T05:06:07Z|95|10|1000|3|Mw|LX"); !errors.Is(err, quake.ErrBadEvent) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseEventEmptyAgency(t *testing.T) {
	for _, l := range []string{"x|2026-03-04T05:06:07Z|-17.8|178.1|13000|4.2|Mw|", "x|2026-03-04T05:06:07Z|-17.8|178.1|13000|4.2||LX"} {
		if _, err := quake.ParseEvent(l); !errors.Is(err, quake.ErrBadEvent) {
			t.Errorf("%q: %v", l, err)
		}
	}
}

func TestParseCatalog(t *testing.T) {
	text := "# LX catalog, week 10\n" + line + "\n# relocated\nus7000a|2026-03-05T01:02:03Z|-20.1|-175.3|35000|5.1|mb|US\n"
	got, err := quake.ParseCatalog(text)
	if err != nil || !reflect.DeepEqual(ids(got), []string{"lx2026e", "us7000a"}) {
		t.Fatalf("ParseCatalog = %v, %v", ids(got), err)
	}
}

func TestParseCatalogDepths(t *testing.T) {
	got, err := quake.ParseCatalog("# source\nus7000a|2026-03-05T01:02:03Z|-20.1|-175.3|35000|5.1|mb|US\n")
	if err != nil || len(got) != 1 || !near(got[0].Depth, 35, 1e-9) {
		t.Fatalf("ParseCatalog = %+v, %v", got, err)
	}
}

func TestParseCatalogBadLine(t *testing.T) {
	if _, err := quake.ParseCatalog("# source\n" + line + "\nnot a line\n"); !errors.Is(err, quake.ErrBadEvent) {
		t.Fatalf("err = %v", err)
	}
}

func TestSameEventWindow(t *testing.T) {
	a := ev("a", 0, -18, 178, 4, "Mw", "LX")
	if !quake.SameEvent(a, ev("b", 14*time.Second, -18.27, 178, 4, "mb", "US")) {
		t.Error("14 s and 30 km apart")
	}
	if !quake.SameEvent(a, ev("c", -5*time.Second, -18.72, 178, 4, "mb", "EM")) {
		t.Error("5 s and 80 km apart")
	}
}

func TestSameEventApart(t *testing.T) {
	a := ev("a", 0, -18, 178, 4, "Mw", "LX")
	if quake.SameEvent(a, ev("b", 20*time.Second, -18.09, 178, 4, "mb", "US")) {
		t.Error("20 s apart")
	}
	if quake.SameEvent(a, ev("c", 5*time.Second, -19.35, 178, 4, "mb", "EM")) {
		t.Error("150 km apart")
	}
}

func TestMwConversions(t *testing.T) {
	if got, ok := ev("a", 0, 0, 0, 3, "ML", "LX").Mw(); !ok || !near(got, 3.01, 1e-9) {
		t.Errorf("ML: %v, %v", got, ok)
	}
	if got, ok := ev("b", 0, 0, 0, 5, "mb", "US").Mw(); !ok || !near(got, 5.28, 1e-9) {
		t.Errorf("mb: %v, %v", got, ok)
	}
}

func TestMwUnknownScale(t *testing.T) {
	if _, ok := ev("a", 0, 0, 0, 5, "Ms", "XX").Mw(); ok {
		t.Fatal("Ms converted")
	}
}

func TestMwAsReported(t *testing.T) {
	if got, ok := ev("a", 0, 0, 0, 4.4, "Mw", "LX").Mw(); !ok || got != 4.4 {
		t.Errorf("Mw: %v, %v", got, ok)
	}
	if got, ok := ev("b", 0, 0, 0, 2.6, "Md", "LX").Mw(); !ok || got != 2.6 {
		t.Errorf("Md: %v, %v", got, ok)
	}
}

func TestPreferred(t *testing.T) {
	lx, us, em, xx, yy := ev("lx", 0, 0, 0, 3.0, "Mw", "LX"), ev("us", 0, 0, 0, 3.2, "Mw", "US"), ev("em", 0, 0, 0, 3.6, "Mw", "EM"), ev("xx", 0, 0, 0, 3.9, "Mw", "GE"), ev("yy", 0, 0, 0, 3.1, "Mw", "IV")
	cases := []struct {
		a, b quake.Event
		want bool
	}{{lx, us, true}, {us, lx, false}, {us, em, true}, {em, us, false}, {em, xx, true}, {xx, em, false}, {yy, xx, true}, {xx, yy, true}}
	for _, c := range cases {
		if got := quake.Preferred(c.a, c.b); got != c.want {
			t.Errorf("Preferred(%s, %s) = %v", c.a.Agency, c.b.Agency, got)
		}
	}
}

func TestDedupWindow(t *testing.T) {
	got := quake.Dedup([]quake.Event{ev("b", 14*time.Second, -18.27, 178, 4, "mb", "EM"), ev("a", 0, -18, 178, 4, "Mw", "US")})
	if len(got) != 1 {
		t.Fatalf("Dedup = %v", ids(got))
	}
}

func TestDedupKeepsPreferred(t *testing.T) {
	events := []quake.Event{
		ev("em1", 0, -18, 178, 3.6, "Mw", "EM"),
		ev("lx1", 3*time.Second, -18.04, 178, 3.4, "Mw", "LX"),
		ev("us2", time.Hour, -21, -176, 5.1, "Mw", "US"),
		ev("em2", time.Hour+2*time.Second, -21.05, -176, 5.3, "Mw", "EM"),
	}
	if got := quake.Dedup(events); !reflect.DeepEqual(ids(got), []string{"lx1", "us2"}) {
		t.Fatalf("Dedup = %v", ids(got))
	}
}

func TestDedupFarApart(t *testing.T) {
	events := []quake.Event{ev("b", 30*time.Second, -18, 178, 4, "Mw", "US"), ev("a", 0, -18, 178, 4, "Mw", "LX")}
	if got := quake.Dedup(events); !reflect.DeepEqual(ids(got), []string{"a", "b"}) || events[0].ID != "b" {
		t.Fatalf("Dedup = %v", ids(got))
	}
}

func TestContainsPlain(t *testing.T) {
	r := quake.Region{South: 30, North: 40, West: 10, East: 20}
	if !r.Contains(35, 15) || r.Contains(35, 25) || r.Contains(45, 15) || !r.Contains(30, 10) {
		t.Fatal("Contains")
	}
}

func TestContainsAcross(t *testing.T) {
	r := quake.Region{South: -25, North: -10, West: 170, East: -170}
	if !r.Contains(-18, 178) || !r.Contains(-18, -175) || r.Contains(-18, 160) || r.Contains(-18, -160) {
		t.Fatal("Contains")
	}
}

func TestInRegionAcross(t *testing.T) {
	events := []quake.Event{ev("a", 0, -18, 178, 4, "Mw", "LX"), ev("b", 0, -18, 150, 4, "Mw", "LX"), ev("c", 0, -20, -175, 4, "Mw", "LX")}
	got := quake.InRegion(events, quake.Region{South: -25, North: -10, West: 170, East: -170})
	if !reflect.DeepEqual(ids(got), []string{"a", "c"}) {
		t.Fatalf("InRegion = %v", ids(got))
	}
}

func TestInRegionPlain(t *testing.T) {
	events := []quake.Event{ev("a", 0, 35, 15, 4, "Mw", "LX"), ev("b", 0, 35, 25, 4, "Mw", "LX")}
	if got := quake.InRegion(events, quake.Region{South: 30, North: 40, West: 10, East: 20}); !reflect.DeepEqual(ids(got), []string{"a"}) {
		t.Fatalf("InRegion = %v", ids(got))
	}
}

func TestMagnitudes(t *testing.T) {
	got := quake.Magnitudes([]quake.Event{ev("a", 0, 0, 0, 4, "Mw", "LX"), ev("b", 0, 0, 0, 5, "Ms", "XX"), ev("c", 0, 0, 0, 3.1, "Mw", "LX")})
	if !reflect.DeepEqual(got, []float64{4, 3.1}) {
		t.Fatalf("Magnitudes = %v", got)
	}
}

func TestCompleteness(t *testing.T) {
	if got := quake.Completeness([]float64{2.3, 2.4, 2.5, 2.5, 2.52, 2.6, 2.7, 3.1}); got != 2.5 {
		t.Errorf("Completeness = %v", got)
	}
	if got := quake.Completeness([]float64{3.0, 3.0, 2.0, 2.0}); got != 2.0 {
		t.Errorf("Completeness on a tie = %v", got)
	}
	if got := quake.Completeness(nil); got != 0 {
		t.Errorf("Completeness of nothing = %v", got)
	}
}

func TestBValue(t *testing.T) {
	got, err := quake.BValue([]float64{2.4, 3.0, 3.02, 3.1, 3.5}, 3.0)
	if want := math.Log10(math.E) / 0.2; err != nil || !near(got, want, 1e-6) {
		t.Fatalf("BValue = %v, %v; want %v", got, err, want)
	}
	if _, err := quake.BValue([]float64{2.4, 3.5}, 3.0); err == nil {
		t.Fatal("no error for one magnitude")
	}
}

func TestAValue(t *testing.T) {
	if got := quake.AValue([]float64{2.4, 3.0, 3.02, 3.1, 3.5}, 3.0, 1.0); !near(got, math.Log10(4)+3, 1e-9) {
		t.Fatalf("AValue = %v", got)
	}
}

func TestRate(t *testing.T) {
	var events []quake.Event
	for i := 0; i < 8; i++ {
		events = append(events, ev("e", time.Duration(i)*12*time.Hour, 0, 0, 3, "Mw", "LX"))
	}
	if got := quake.Rate(events, t0, t0.Add(72*time.Hour)); !near(got, 2, 1e-9) {
		t.Errorf("Rate = %v", got)
	}
	if got := quake.Rate(events, t0, t0); got != 0 {
		t.Errorf("Rate over nothing = %v", got)
	}
}

func TestAftershocks(t *testing.T) {
	main := ev("main", 0, -18, 178, 6.0, "Mw", "LX")
	events := []quake.Event{
		ev("late", 12*24*time.Hour, -18, 178, 3.0, "Mw", "LX"),
		ev("in2", 48*time.Hour, -18.1, 178, 4.4, "Mw", "LX"),
		ev("before", -24*time.Hour, -18, 178, 4.5, "Mw", "LX"),
		ev("bigger", 24*time.Hour, -18, 178, 6.5, "Mw", "LX"),
		ev("far", 24*time.Hour, -21, 178, 3.5, "Mw", "LX"),
		ev("in1", 24*time.Hour, -18.18, 178, 4.0, "Mw", "LX"),
	}
	if got := quake.Aftershocks(main, events, 10, 100); !reflect.DeepEqual(ids(got), []string{"in1", "in2"}) {
		t.Fatalf("Aftershocks = %v", ids(got))
	}
}

func TestMoment(t *testing.T) {
	if got := quake.Moment(6); !near(got/math.Pow(10, 18.1), 1, 1e-12) {
		t.Errorf("Moment = %v", got)
	}
	if got := quake.MomentMw(quake.Moment(5.5)); !near(got, 5.5, 1e-9) {
		t.Errorf("MomentMw = %v", got)
	}
}

func TestTotalMw(t *testing.T) {
	events := []quake.Event{ev("a", 0, 0, 0, 6, "Mw", "LX"), ev("b", time.Hour, 0, 0, 6, "Mw", "LX")}
	if got := quake.TotalMw(events); !near(got, 6+2*math.Log10(2)/3, 1e-9) {
		t.Errorf("TotalMw = %v", got)
	}
	if got := quake.TotalMw(nil); got != 0 {
		t.Errorf("TotalMw of nothing = %v", got)
	}
}

func TestFormatEvent(t *testing.T) {
	e := quake.Event{ID: "lx2026e", Time: t0.Add(500 * time.Millisecond), Lat: -17.8, Lon: 178.1, Depth: 13, Mag: 4.2, MagType: "Mw", Agency: "LX"}
	if got := quake.FormatEvent(e); got != line {
		t.Fatalf("FormatEvent = %q", got)
	}
}

func TestFormatEventTime(t *testing.T) {
	got := quake.FormatEvent(ev("us7000a", 0, -20.1, -175.3, 5.1, "mb", "US"))
	if !strings.HasPrefix(got, "us7000a|2026-03-04T05:06:07Z|-20.1|-175.3|") || !strings.HasSuffix(got, "|5.1|mb|US") {
		t.Fatalf("FormatEvent = %q", got)
	}
}

func TestFormatEventRoundTrip(t *testing.T) {
	e := quake.Event{ID: "em42", Time: t0.Add(250 * time.Millisecond), Lat: 38.25, Lon: 21.5, Depth: 7.5, Mag: 3.7, MagType: "ML", Agency: "EM"}
	got, err := quake.ParseEvent(quake.FormatEvent(e))
	if err != nil || got.ID != e.ID || !got.Time.Equal(e.Time) || got.Lat != e.Lat || got.Lon != e.Lon ||
		!near(got.Depth, e.Depth, 1e-9) || got.Mag != e.Mag || got.MagType != e.MagType || got.Agency != e.Agency {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
}

func TestWriteCatalog(t *testing.T) {
	events := []quake.Event{ev("b", time.Hour, -18, 178, 4, "Mw", "LX"), ev("a", 0, -20, -175, 5, "mb", "US")}
	text := quake.WriteCatalog("LX week 10", events)
	if !strings.HasPrefix(text, "# LX week 10\n") || !strings.HasSuffix(text, "\n") || strings.Count(text, "\n") != 3 {
		t.Fatalf("WriteCatalog = %q", text)
	}
	got, err := quake.ParseCatalog(text)
	if err != nil || !reflect.DeepEqual(ids(got), []string{"b", "a"}) {
		t.Fatalf("read back %v, %v", ids(got), err)
	}
}

func swarmEvents() []quake.Event {
	return []quake.Event{
		ev("lone", 3*time.Hour, -25, 170, 3, "Mw", "LX"),
		ev("p1", 0, -18, 178, 3, "Mw", "LX"),
		ev("q1", 10*time.Minute, -15, -173, 3, "Mw", "LX"),
		ev("p2", 30*time.Minute, -18.1, 178, 3, "Mw", "LX"),
		ev("q2", 40*time.Minute, -15.05, -173, 3, "Mw", "LX"),
		ev("p3", 2*time.Hour, -18.2, 178, 3, "Mw", "LX"),
		ev("p4", 9*time.Hour, -18, 178, 3, "Mw", "LX"),
	}
}

func TestSwarms(t *testing.T) {
	got := quake.Swarms(swarmEvents(), 50, 2*time.Hour, 2)
	if len(got) != 2 || !reflect.DeepEqual(ids(got[0]), []string{"p1", "p2", "p3"}) || !reflect.DeepEqual(ids(got[1]), []string{"q1", "q2"}) {
		t.Fatalf("Swarms = %v", got)
	}
}

func TestSwarmsSingles(t *testing.T) {
	got := quake.Swarms(swarmEvents(), 50, 2*time.Hour, 1)
	if len(got) != 4 || got[2][0].ID != "lone" || got[3][0].ID != "p4" {
		t.Fatalf("Swarms = %v", got)
	}
}

var week = [2]time.Time{time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)}

func bulletin(events ...quake.Event) string { return quake.Bulletin(events, week[0], week[1]) }

func TestBulletinLayout(t *testing.T) {
	e := quake.Event{ID: "lx2026e", Time: t0, Lat: -17.8, Lon: 178.1, Depth: 13, Mag: 4.2, MagType: "Mw", Agency: "LX"}
	want := "LX bulletin 2026-03-02 to 2026-03-09\n2026-03-04 05:06:07  Mw 4.2  13 km  -17.80 178.10  lx2026e\n1 earthquake\n"
	if got := bulletin(e); got != want {
		t.Fatalf("Bulletin = %q", got)
	}
}

func TestBulletinCountLine(t *testing.T) {
	if got := bulletin(); got != "LX bulletin 2026-03-02 to 2026-03-09\nno earthquakes\n" {
		t.Errorf("Bulletin = %q", got)
	}
	got := bulletin(ev("a", 0, -18, 178, 4, "Mw", "LX"), ev("b", time.Hour, -18, 178, 5, "Mw", "LX"), ev("c", 2*time.Hour, -18, 178, 4.5, "Mw", "LX"))
	if !strings.HasSuffix(got, "\n3 earthquakes\n") {
		t.Errorf("Bulletin = %q", got)
	}
}

func TestBulletinWindow(t *testing.T) {
	got := bulletin(ev("in", 0, -18, 178, 4, "Mw", "LX"), ev("early", -3*24*time.Hour, -18, 178, 4, "Mw", "LX"), ev("late", 5*24*time.Hour-5*time.Hour-6*time.Minute-7*time.Second, -18, 178, 4, "Mw", "LX"))
	if !strings.Contains(got, " in\n") || strings.Contains(got, "early") || strings.Contains(got, "late") {
		t.Fatalf("Bulletin = %q", got)
	}
}

func TestBulletinThreshold(t *testing.T) {
	got := bulletin(ev("small", 0, -18, 178, 2.7, "Mw", "LX"), ev("big", time.Hour, -18, 178, 3.4, "Mw", "LX"), ev("edge", 2*time.Hour, -18, 178, 3.0, "Mw", "LX"))
	if strings.Contains(got, "small") || !strings.Contains(got, "big") || !strings.Contains(got, "edge") {
		t.Fatalf("Bulletin = %q", got)
	}
}

func TestBulletinOrder(t *testing.T) {
	got := bulletin(ev("second", time.Hour, -18, 178, 3.2, "Mw", "LX"), ev("biggest", 2*time.Hour, -18, 178, 4.0, "Mw", "LX"), ev("first", 0, -18, 178, 3.2, "Mw", "LX"))
	b, f, s := strings.Index(got, "biggest"), strings.Index(got, "first"), strings.Index(got, "second")
	if b < 0 || f < 0 || s < 0 || !(b < f && f < s) {
		t.Fatalf("Bulletin = %q", got)
	}
}

func TestBulletinConverts(t *testing.T) {
	got := bulletin(ev("local", 0, -18, 178, 3.5, "ML", "LX"), ev("surface", time.Hour, -18, 178, 5.0, "Ms", "XX"))
	if !strings.Contains(got, "Mw 3.3") || strings.Contains(got, "surface") {
		t.Fatalf("Bulletin = %q", got)
	}
}
`},
		Solution: map[string]string{
			"event.go": `package quake

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// ErrBadEvent is returned, possibly wrapped, for any event or catalog line
// we cannot use.
var ErrBadEvent = errors.New("bad event")

// Event is one earthquake as a catalog reports it.
type Event struct {
	ID      string
	Time    time.Time // UTC
	Lat     float64   // degrees, north positive
	Lon     float64   // degrees, east positive
	Depth   float64   // km below sea level
	Mag     float64   // as reported, on the scale MagType
	MagType string    // ML, mb, Mw, ...
	Agency  string    // the reporting agency's code
}

// ByTime sorts events by time, and events at the same time by ID.
func ByTime(events []Event) {
	sort.SliceStable(events, func(a, b int) bool {
		if !events[a].Time.Equal(events[b].Time) {
			return events[a].Time.Before(events[b].Time)
		}
		return events[a].ID < events[b].ID
	})
}

// Validate reports ErrBadEvent for coordinates that are off the globe.
func (e Event) Validate() error {
	if e.Lat < -90 || e.Lat > 90 || e.Lon < -180 || e.Lon > 180 {
		return fmt.Errorf("%w: %s at %v, %v", ErrBadEvent, e.ID, e.Lat, e.Lon)
	}
	return nil
}

// Within reports whether the event happened at or after from and before to.
func (e Event) Within(from, to time.Time) bool {
	return !e.Time.Before(from) && e.Time.Before(to)
}

// Reported returns the magnitude as the agency reported it, like "ML 3.2".
func (e Event) Reported() string { return fmt.Sprintf("%s %.1f", e.MagType, e.Mag) }

// ByAgency returns the events keyed by agency, each agency's in input order.
func ByAgency(events []Event) map[string][]Event {
	out := map[string][]Event{}
	for _, e := range events {
		out[e.Agency] = append(out[e.Agency], e)
	}
	return out
}
`,
			"catalog.go": `package quake

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseEvent reads one catalog line,
// id|time|latitude|longitude|depth|magnitude|magtype|agency, the depth
// given in metres.
func ParseEvent(line string) (Event, error) {
	f := strings.Split(line, "|")
	if len(f) != 8 {
		return Event{}, fmt.Errorf("%w: %d fields in %q", ErrBadEvent, len(f), line)
	}
	if f[6] == "" || f[7] == "" {
		return Event{}, fmt.Errorf("%w: no magnitude type or agency in %q", ErrBadEvent, line)
	}
	at, err := time.Parse(time.RFC3339, f[1])
	if err != nil {
		return Event{}, fmt.Errorf("%w: time: %v", ErrBadEvent, err)
	}
	var nums [4]float64
	for i := range nums {
		if nums[i], err = strconv.ParseFloat(f[2+i], 64); err != nil {
			return Event{}, fmt.Errorf("%w: number: %v", ErrBadEvent, err)
		}
	}
	e := Event{ID: f[0], Time: at.UTC(), Lat: nums[0], Lon: nums[1], Depth: nums[2] / 1000,
		Mag: nums[3], MagType: f[6], Agency: f[7]}
	return e, e.Validate()
}

// ParseCatalog reads a catalog file: its events in order, without the
// comment lines.
func ParseCatalog(text string) ([]Event, error) {
	var out []Event
	for i, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		e, err := ParseEvent(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// FormatEvent writes the catalog line for an event, the inverse of
// ParseEvent.
func FormatEvent(e Event) string {
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return strings.Join([]string{e.ID, e.Time.UTC().Format(time.RFC3339Nano), num(e.Lat), num(e.Lon),
		num(e.Depth * 1000), num(e.Mag), e.MagType, e.Agency}, "|")
}

// WriteCatalog writes a catalog file for the events, in order, after a
// comment line naming its source.
func WriteCatalog(source string, events []Event) string {
	var b strings.Builder
	b.WriteString("# " + source + "\n")
	for _, e := range events {
		b.WriteString(FormatEvent(e) + "\n")
	}
	return b.String()
}
`,
			"geo.go": `package quake

import "math"

// earthRadius is the mean radius of the Earth in km.
const earthRadius = 6371.0

// radians converts degrees to radians.
func radians(d float64) float64 { return d * math.Pi / 180 }

// Distance returns the great-circle distance in km between two points given
// in decimal degrees, by the haversine formula.
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	dLat, dLon := radians(lat2-lat1), radians(lon2-lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(radians(lat1))*math.Cos(radians(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Sqrt(a))
}

// Bearing returns the initial bearing in degrees, clockwise from north, of
// the great circle from the first point to the second, in [0, 360).
func Bearing(lat1, lon1, lat2, lon2 float64) float64 {
	p1, p2, dLon := radians(lat1), radians(lat2), radians(lon2-lon1)
	y := math.Sin(dLon) * math.Cos(p2)
	x := math.Cos(p1)*math.Sin(p2) - math.Sin(p1)*math.Cos(p2)*math.Cos(dLon)
	deg := math.Atan2(y, x) * 180 / math.Pi
	return math.Mod(deg+360, 360)
}

// Destination returns the point km away from a start point along an initial
// bearing in degrees, its longitude in [-180, 180).
func Destination(lat, lon, bearing, km float64) (float64, float64) {
	d, b, p1, l1 := km/earthRadius, radians(bearing), radians(lat), radians(lon)
	p2 := math.Asin(math.Sin(p1)*math.Cos(d) + math.Cos(p1)*math.Sin(d)*math.Cos(b))
	l2 := l1 + math.Atan2(math.Sin(b)*math.Sin(d)*math.Cos(p1), math.Cos(d)-math.Sin(p1)*math.Sin(p2))
	deg := func(r float64) float64 { return r * 180 / math.Pi }
	return deg(p2), math.Mod(deg(l2)+540, 360) - 180
}

// Nearest returns the event whose epicentre is closest to a point, the
// first one on a tie, and false when there are no events.
func Nearest(events []Event, lat, lon float64) (Event, bool) {
	best, dist := -1, 0.0
	for i, e := range events {
		if d := Distance(lat, lon, e.Lat, e.Lon); best < 0 || d < dist {
			best, dist = i, d
		}
	}
	if best < 0 {
		return Event{}, false
	}
	return events[best], true
}

// Region is a box of latitudes and longitudes in degrees. A region with West
// greater than East crosses the 180° meridian.
type Region struct {
	South, North, West, East float64
}

// Contains reports whether a point lies in the region, its edges included.
func (r Region) Contains(lat, lon float64) bool {
	if lat < r.South || lat > r.North {
		return false
	}
	if r.West > r.East {
		return lon >= r.West || lon <= r.East
	}
	return lon >= r.West && lon <= r.East
}

// InRegion returns the events whose epicentres the region contains, in order.
func InRegion(events []Event, r Region) []Event {
	var out []Event
	for _, e := range events {
		if r.Contains(e.Lat, e.Lon) {
			out = append(out, e)
		}
	}
	return out
}
`,
			"magnitude.go": `package quake

import "math"

// Energy returns the radiated energy in joules for a moment magnitude.
func Energy(mw float64) float64 { return math.Pow(10, 1.5*mw+4.8) }

// Bin rounds a magnitude to the nearest 0.1, for magnitude statistics.
func Bin(m float64) float64 { return math.Round(m*10) / 10 }

// Mw returns the event's moment magnitude, converting the other scales as
// docs/magnitudes.txt says, and false for a scale that cannot be converted.
func (e Event) Mw() (float64, bool) {
	switch e.MagType {
	case "Mw", "Md":
		return e.Mag, true
	case "ML":
		return 0.67*e.Mag + 1.0, true
	case "mb":
		return 0.85*e.Mag + 1.03, true
	}
	return 0, false
}

// Magnitudes returns the Mw of each event that has one, in order.
func Magnitudes(events []Event) []float64 {
	var out []float64
	for _, e := range events {
		if mw, ok := e.Mw(); ok {
			out = append(out, mw)
		}
	}
	return out
}

// Moment returns the seismic moment in N·m for a moment magnitude.
func Moment(mw float64) float64 { return math.Pow(10, 1.5*mw+9.1) }

// MomentMw returns the moment magnitude for a seismic moment in N·m.
func MomentMw(m0 float64) float64 { return (math.Log10(m0) - 9.1) / 1.5 }

// TotalMw returns the Mw of one earthquake releasing the summed moment of
// the events that have an Mw, or 0 when none has.
func TotalMw(events []Event) float64 {
	sum := 0.0
	for _, mw := range Magnitudes(events) {
		sum += Moment(mw)
	}
	if sum == 0 {
		return 0
	}
	return MomentMw(sum)
}
`,
			"dedup.go": `package quake

import (
	"math"
	"slices"
)

// Two reports are of the same earthquake when they are this close.
const (
	sameSeconds = 16
	sameKm      = 100
)

// SameEvent reports whether two reports are of the same earthquake.
func SameEvent(a, b Event) bool {
	dt := math.Abs(a.Time.Sub(b.Time).Seconds())
	return dt <= sameSeconds && Distance(a.Lat, a.Lon, b.Lat, b.Lon) <= sameKm
}

// standing ranks the agencies whose reports we keep first: our own network,
// then the USGS, then the EMSC. Everyone else comes after them.
var standing = map[string]int{"LX": 0, "US": 1, "EM": 2}

// rank returns an agency's standing, lower being preferred.
func rank(agency string) int {
	if r, ok := standing[agency]; ok {
		return r
	}
	return len(standing)
}

// Preferred reports whether report a is the one to keep over report b, for
// two reports of the same earthquake, a being listed before b.
func Preferred(a, b Event) bool { return rank(a.Agency) <= rank(b.Agency) }

// Dedup keeps one report per earthquake, sorted with ByTime.
func Dedup(events []Event) []Event {
	sorted := slices.Clone(events)
	ByTime(sorted)
	var kept []Event
	for _, e := range sorted {
		i := slices.IndexFunc(kept, func(k Event) bool { return SameEvent(k, e) })
		switch {
		case i < 0:
			kept = append(kept, e)
		case !Preferred(kept[i], e):
			kept[i] = e
		}
	}
	ByTime(kept)
	return kept
}
`,
			"cluster.go": `package quake

import (
	"slices"
	"time"
)

// Aftershocks returns the events after a main shock, at most days later and
// km from its epicentre, whose Mw is lower than its own, sorted with ByTime.
func Aftershocks(main Event, events []Event, days, km float64) []Event {
	mainMw, ok := main.Mw()
	if !ok {
		return nil
	}
	var out []Event
	for _, e := range events {
		mw, ok := e.Mw()
		dt := e.Time.Sub(main.Time).Hours() / 24
		if ok && mw < mainMw && dt > 0 && dt <= days && Distance(main.Lat, main.Lon, e.Lat, e.Lon) <= km {
			out = append(out, e)
		}
	}
	ByTime(out)
	return out
}

// Swarms groups events close in time and place. Taken in time order, each
// event joins the first group whose latest event is at most gap before it
// and whose first event is at most km away, or starts a new group. Swarms
// returns the groups of at least minSize events, in the order they started.
func Swarms(events []Event, km float64, gap time.Duration, minSize int) [][]Event {
	sorted := slices.Clone(events)
	ByTime(sorted)
	var groups [][]Event
	for _, e := range sorted {
		i := slices.IndexFunc(groups, func(g []Event) bool {
			first, last := g[0], g[len(g)-1]
			return e.Time.Sub(last.Time) <= gap && Distance(first.Lat, first.Lon, e.Lat, e.Lon) <= km
		})
		if i < 0 {
			groups = append(groups, []Event{e})
			continue
		}
		groups[i] = append(groups[i], e)
	}
	return slices.DeleteFunc(groups, func(g []Event) bool { return len(g) < minSize })
}
`,
			"stats.go": `package quake

import (
	"errors"
	"maps"
	"math"
	"slices"
	"time"
)

// Histogram returns how many magnitudes fall in each Bin.
func Histogram(mags []float64) map[float64]int {
	out := map[float64]int{}
	for _, m := range mags {
		out[Bin(m)]++
	}
	return out
}

// Completeness returns the magnitude of completeness by maximum curvature:
// the Bin holding the most magnitudes, the lowest on a tie, 0 for none.
func Completeness(mags []float64) float64 {
	h := Histogram(mags)
	best, most := 0.0, 0
	for _, bin := range slices.Sorted(maps.Keys(h)) {
		if h[bin] > most {
			best, most = bin, h[bin]
		}
	}
	return best
}

// BValue returns Aki's maximum-likelihood b-value with Utsu's correction
// for 0.1 bins, from the Bins of the magnitudes at or above mc.
func BValue(mags []float64, mc float64) (float64, error) {
	sum, n := 0.0, 0
	for _, m := range mags {
		if b := Bin(m); b >= mc {
			sum += b
			n++
		}
	}
	if n < 2 {
		return 0, errors.New("too few magnitudes for a b-value")
	}
	return math.Log10(math.E) / (sum/float64(n) - (mc - 0.05)), nil
}

// AValue returns the Gutenberg-Richter a-value: log10 of how many
// magnitudes have a Bin at or above mc, plus b times mc.
func AValue(mags []float64, mc, b float64) float64 {
	n := 0
	for _, m := range mags {
		if Bin(m) >= mc {
			n++
		}
	}
	return math.Log10(float64(n)) + b*mc
}

// Rate returns how many events per day happened at or after from and before
// to, or 0 when to is not after from.
func Rate(events []Event, from, to time.Time) float64 {
	days := to.Sub(from).Hours() / 24
	if days <= 0 {
		return 0
	}
	n := 0
	for _, e := range events {
		if e.Within(from, to) {
			n++
		}
	}
	return float64(n) / days
}
`,
			"bulletin.go": `package quake

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// bulletinMin is the smallest Mw the bulletin lists.
const bulletinMin = 3.0

// listed is an earthquake in the bulletin, with its Mw.
type listed struct {
	e  Event
	mw float64
}

// Bulletin writes the weekly bulletin for the earthquakes that happened at
// or after from and before to.
func Bulletin(events []Event, from, to time.Time) string {
	var rows []listed
	for _, e := range events {
		if mw, ok := e.Mw(); ok && mw >= bulletinMin && e.Within(from, to) {
			rows = append(rows, listed{e, mw})
		}
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].mw != rows[b].mw {
			return rows[a].mw > rows[b].mw
		}
		return rows[a].e.Time.Before(rows[b].e.Time)
	})
	var b strings.Builder
	fmt.Fprintf(&b, "LX bulletin %s to %s\n", from.Format("2006-01-02"), to.Format("2006-01-02"))
	for _, r := range rows {
		e := r.e
		fmt.Fprintf(&b, "%s  Mw %.1f  %.0f km  %.2f %.2f  %s\n",
			e.Time.UTC().Format("2006-01-02 15:04:05"), r.mw, e.Depth, e.Lat, e.Lon, e.ID)
	}
	b.WriteString(count(len(rows)) + "\n")
	return b.String()
}

// count writes the bulletin's last line.
func count(n int) string {
	switch n {
	case 0:
		return "no earthquakes"
	case 1:
		return "1 earthquake"
	}
	return fmt.Sprintf("%d earthquakes", n)
}
`,
		},
		Turns: script(
			cloud("Add `var ErrBadEvent = errors.New(\"bad event\")` to event.go, and `func (e Event) Validate() error`, which returns it, wrapped with detail if you like, when the latitude is outside -90..90 or the longitude outside -180..180. From now on this is the error for any event or catalog line we cannot use."),
			cloud("Add `func (e Event) Within(from, to time.Time) bool` to event.go: whether the event happened at or after from and before to."),
			local("Read docs/catalog-format.txt and tell me briefly how catalog files are laid out. No code changes."),
			local("Start magnitude.go with `func Energy(mw float64) float64`: the radiated energy in joules for a moment magnitude, 10^(1.5·mw + 4.8)."),
			local("Background for merging catalogs later: two reports are the same earthquake when their times are no more than 16 seconds apart and their epicentres no more than 100 km apart. Nothing to code for that yet. Add `func Bin(m float64) float64` to magnitude.go: m rounded to the nearest 0.1, for magnitude statistics."),
			cloud("Read docs/magnitudes.txt; we will need it soon. Meanwhile add `func (e Event) Reported() string` to event.go: the magnitude as the agency reported it, the scale, a space and the value with one decimal, like \"ML 3.2\"."),
			cloud("When several agencies report the same earthquake, we keep one report: ours (LX) over the USGS (US), the USGS over the EMSC (EM), the EMSC over anyone else, and between two of the same standing, the one listed first. Nothing to code for that yet. Add `func ByAgency(events []Event) map[string][]Event` to event.go: the events keyed by agency, each agency's in input order."),
			local("Add `func Nearest(events []Event, lat, lon float64) (Event, bool)` to geo.go: the event whose epicentre is closest to that point by Distance, the first one on a tie, and false when there are no events."),
			cloud("Our network watches Fiji and Tonga, across the 180° meridian, so a region that crosses it is written with West greater than East, and then covers the longitudes from West up to 180 and from -180 up to East. Add `type Region struct { South, North, West, East float64 }` to geo.go, in degrees; nothing uses it yet."),
			cloud("For the weekly bulletin, which comes later: it lists the earthquakes of Mw 2.5 or more, largest first, and those of equal magnitude in time order. Nothing to code for that yet. Start stats.go with `func Histogram(mags []float64) map[float64]int`: how many magnitudes fall in each Bin."),
			cloud("Add `func ParseEvent(line string) (Event, error)` to event.go, for one line of a catalog file, and run Validate on the result."),
			local("Add `func ParseCatalog(text string) ([]Event, error)` to event.go: the events of a catalog file, in order, using ParseEvent. When a line fails, the error says which line it was, counting from 1."),
			cloud("Add `func SameEvent(a, b Event) bool` in a new dedup.go: whether two reports are of the same earthquake."),
			local("Add `func (e Event) Mw() (float64, bool)` to magnitude.go: the event's moment magnitude, and false when it has none we can use."),
			local("Add `func Preferred(a, b Event) bool` to dedup.go: for two reports of the same earthquake, a listed before b, whether a is the one to keep."),
			cloud("Add `func Dedup(events []Event) []Event` to dedup.go: one report per earthquake. Go through a copy of the events sorted with ByTime, keep each report unless it is the same earthquake as one already kept, and in that case keep whichever of the two Preferred picks, the kept one counting as listed first. Return the kept reports sorted with ByTime."),
			local("Add `func (r Region) Contains(lat, lon float64) bool` to geo.go: whether a point lies in the region, its edges included."),
			cloud("A change to the bulletin rule from earlier: seismology wants its lower bound at Mw 3.0 from now on; nothing else about it changes. Add `func Magnitudes(events []Event) []float64` to magnitude.go: the Mw of each event that has one, in order."),
			cloud("Read docs/bulletin.txt; that is how the bulletin must look. Then add `func Completeness(mags []float64) float64` to stats.go: the magnitude of completeness by maximum curvature, the Bin holding the most magnitudes, the lowest such Bin on a tie, or 0 for no magnitudes."),
			local("Add `func InRegion(events []Event, r Region) []Event` to geo.go: the events whose epicentres the region contains, in order."),
			cloud("Add `func BValue(mags []float64, mc float64) (float64, error)` to stats.go: the b-value of the magnitudes as docs/statistics.txt defines it, using Bin for the binning, and an error when there is no estimate."),
			local("event.go has grown. Move ParseEvent and ParseCatalog into a new file catalog.go, and leave the rest in event.go. No behaviour changes."),
			cloud("Add `func AValue(mags []float64, mc, b float64) float64` to stats.go: the a-value of the magnitudes as docs/statistics.txt defines it, again using Bin."),
			local("Add `func Rate(events []Event, from, to time.Time) float64` to stats.go: how many events per day happened at or after from and before to, or 0 when to is not after from."),
			cloud("Add `func Aftershocks(main Event, events []Event, days, km float64) []Event` in a new cluster.go: the events after the main shock, at most days later and at most km from its epicentre, whose Mw is lower than the main shock's, sorted with ByTime. Events without an Mw are left out."),
			local("ParseEvent should also refuse a line whose magnitude type or agency is empty."),
			local("Add `func Moment(mw float64) float64` to magnitude.go, the seismic moment in N·m, 10^(1.5·mw + 9.1), and `func MomentMw(m0 float64) float64`, its inverse."),
			cloud("Add `func TotalMw(events []Event) float64` to magnitude.go: the Mw of a single earthquake releasing the summed moment of all the events that have an Mw, or 0 when none has."),
			cloud("Add `func FormatEvent(e Event) string` to catalog.go: the catalog line for an event, such that ParseEvent reads the same event back. Write numbers in their shortest form, and the time in UTC with fractional seconds only when it has them."),
			local("Add `func WriteCatalog(source string, events []Event) string` to catalog.go: a catalog file holding the events in order, one FormatEvent line each, after a first line of \"# \" and the source. Every line ends with a newline."),
			cloud("Add `func Swarms(events []Event, km float64, gap time.Duration, minSize int) [][]Event` to cluster.go. Taken in time order (ByTime), each event joins the first group, in the order the groups started, whose latest event is at most gap before it and whose first event is at most km away; otherwise it starts a new group. Return the groups of at least minSize events, in the order they started."),
			local("Add `func Bulletin(events []Event, from, to time.Time) string` in a new bulletin.go: the bulletin for the earthquakes that happened at or after from and before to."),
		),
		Carries: []Carry{
			{
				What:        "ErrBadEvent for any event or catalog line we cannot use",
				Kind:        CarryEmbodied,
				Established: 0,
				Needed:      []int{10, 11, 25},
				Tests:       []string{"TestParseEventBadNumber", "TestParseEventFieldCount", "TestParseEventEmptyAgency", "TestParseCatalogBadLine"},
				Absent:      []string{"errbadevent", "bad event", "cannot use", "errors.is", "sentinel"},
			},
			{
				What:        "catalog depths are in metres (docs/catalog-format.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{10, 28},
				Source:      "docs/catalog-format.txt",
				Tests:       []string{"TestParseEventDepth", "TestParseCatalogDepths", "TestFormatEvent"},
				Absent:      []string{"metres", "meters", "1000", "catalog-format", "database stores"},
			},
			{
				What:        "two reports are the same earthquake within 16 s and 100 km",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{12},
				Tests:       []string{"TestSameEventWindow", "TestDedupWindow"},
				Absent:      []string{"16 seconds", "100 km", "no more than"},
			},
			{
				What:        "ML, mb and Md convert to Mw by the table, other scales are left out (docs/magnitudes.txt)",
				Kind:        CarryDiscovered,
				Established: 5,
				Needed:      []int{13, 31},
				Source:      "docs/magnitudes.txt",
				Tests:       []string{"TestMwConversions", "TestMwUnknownScale", "TestMagnitudes", "TestBulletinConverts"},
				Absent:      []string{"magnitudes.txt", "0.67", "0.85", "1.03", "body-wave", "cannot be converted"},
			},
			{
				What:        "keep LX over US over EM over anyone, the first listed between equals",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{14},
				Tests:       []string{"TestPreferred", "TestDedupKeepsPreferred"},
				Absent:      []string{"usgs", "emsc", "over anyone", "same standing", "(lx)"},
			},
			{
				What:        "a region with West greater than East crosses the 180° meridian",
				Kind:        CarryUser,
				Established: 8,
				Needed:      []int{16},
				Tests:       []string{"TestContainsAcross", "TestInRegionAcross"},
				Absent:      []string{"meridian", "west greater", "fiji", "tonga", "wraps"},
			},
			{
				What:        "the bulletin lists earthquakes largest first, equal ones by time (and from Mw 2.5, later revised)",
				Kind:        CarryUser,
				Established: 9,
				Needed:      []int{31},
				Tests:       []string{"TestBulletinOrder"},
				Absent:      []string{"2.5", "largest first", "equal magnitude"},
			},
			{
				What:        "the bulletin starts at Mw 3.0, revised from 2.5",
				Kind:        CarryUser,
				Established: 17,
				Needed:      []int{31},
				Tests:       []string{"TestBulletinThreshold"},
				Absent:      []string{"3.0", "lower bound", "seismology"},
			},
			{
				What:        "the bulletin layout (docs/bulletin.txt)",
				Kind:        CarryDiscovered,
				Established: 18,
				Needed:      []int{31},
				Source:      "docs/bulletin.txt",
				Tests:       []string{"TestBulletinLayout", "TestBulletinCountLine"},
				Absent:      []string{"bulletin.txt", "yyyy-mm-dd", "two decimals", "whole kilomet", "lx bulletin", "no earthquakes"},
			},
		},
	}
}
