package s2

func longA01() Task {
	return Task{
		ID:     "s2-long-a01-fleet-servicing",
		Set:    "long",
		System: system("a servicing tracker for a company's vehicle fleet office"),
		Files: map[string]string{
			"go.mod": goMod,
			"vehicle.go": `package fleet

import (
	"errors"
	"sort"
)

// ErrDuplicate is returned when a registration is already on the register.
var ErrDuplicate = errors.New("duplicate vehicle")

// Vehicle is one entry on the register. Odometer and LastServiceKm are in
// kilometres. Days are plain day numbers, the way the office systems count them,
// and they start at 1.
type Vehicle struct {
	Reg            string
	Class          string // "car", "van" or "truck"
	Odometer       int
	LastServiceKm  int
	LastServiceDay int
	Retired        bool
}

// Fleet is the register and the work orders recorded against it.
type Fleet struct {
	vehicles map[string]*Vehicle
	orders   []*WorkOrder
	nextID   int
	drivers  map[string]Driver
	assigned map[string]string // driver by registration
}

// New returns an empty register.
func New() *Fleet {
	return &Fleet{vehicles: map[string]*Vehicle{}, nextID: 1, drivers: map[string]Driver{}, assigned: map[string]string{}}
}

// Add puts a vehicle on the register.
func (f *Fleet) Add(v Vehicle) error {
	if _, ok := f.vehicles[v.Reg]; ok {
		return ErrDuplicate
	}
	f.vehicles[v.Reg] = &v
	return nil
}

// Get returns a copy of a vehicle.
func (f *Fleet) Get(reg string) (Vehicle, bool) {
	v, ok := f.vehicles[reg]
	if !ok {
		return Vehicle{}, false
	}
	return *v, true
}

// Regs returns every registration on the register, retired ones too, sorted.
func (f *Fleet) Regs() []string {
	out := make([]string, 0, len(f.vehicles))
	for reg := range f.vehicles {
		out = append(out, reg)
	}
	sort.Strings(out)
	return out
}

// Count returns the number of vehicles on the register, retired ones too.
func (f *Fleet) Count() int { return len(f.vehicles) }

// Classes returns how many vehicles of each class are on the register.
func (f *Fleet) Classes() map[string]int {
	out := map[string]int{}
	for _, v := range f.vehicles {
		out[v.Class]++
	}
	return out
}
`,
			"workorder.go": `package fleet

import "fmt"

// Line is one entry on a work order: either labour, with Part empty and Minutes
// set, or a part, with Part naming it and Cents its price before any charges.
type Line struct {
	Part    string
	Minutes int
	Cents   int
}

// WorkOrder is one job in the workshop. Opened and Closed are day numbers, and
// Closed stays 0 while the order is open.
type WorkOrder struct {
	ID     string
	Reg    string
	Kind   string // "service" or "repair"
	Opened int
	Closed int
	Lines  []Line
}

// orderID formats the ID of the n-th work order.
func orderID(n int) string { return fmt.Sprintf("WO-%04d", n) }

// order returns the work order with an ID, or nil.
func (f *Fleet) order(id string) *WorkOrder {
	for _, o := range f.orders {
		if o.ID == id {
			return o
		}
	}
	return nil
}

// Orders returns copies of a vehicle's work orders, oldest first.
func (f *Fleet) Orders(reg string) []WorkOrder {
	var out []WorkOrder
	for _, o := range f.orders {
		if o.Reg == reg {
			out = append(out, *o)
		}
	}
	return out
}
`,
			"service.go": `package fleet

// KmSinceService returns how far the vehicle has gone since its last service.
func (v Vehicle) KmSinceService() int { return v.Odometer - v.LastServiceKm }

// DaysSinceService returns the days from its last service to today.
func (v Vehicle) DaysSinceService(today int) int { return today - v.LastServiceDay }
`,
			"report.go": `package fleet

import (
	"fmt"
	"sort"
	"strings"
)

// Summary returns the short overview the office pins to its noticeboard.
func (f *Fleet) Summary() string {
	retired := 0
	for _, v := range f.vehicles {
		if v.Retired {
			retired++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "vehicles: %d\n", len(f.vehicles))
	fmt.Fprintf(&b, "retired: %d\n", retired)
	fmt.Fprintf(&b, "work orders: %d\n", len(f.orders))
	return b.String()
}

// ClassLines returns one "class: count" line for each class, sorted by class.
func (f *Fleet) ClassLines() []string {
	counts := f.Classes()
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("%s: %d", name, counts[name]))
	}
	return out
}
`,
			"drivers.go": `package fleet

import (
	"errors"
	"sort"
)

// ErrDuplicateDriver is returned when a driver's name is already listed.
var ErrDuplicateDriver = errors.New("duplicate driver")

// Driver is someone who can be given a vehicle. Licence "B" covers cars and vans,
// and licence "C" covers trucks as well.
type Driver struct {
	Name    string
	Licence string
}

// AddDriver puts a driver on the list.
func (f *Fleet) AddDriver(d Driver) error {
	if _, ok := f.drivers[d.Name]; ok {
		return ErrDuplicateDriver
	}
	f.drivers[d.Name] = d
	return nil
}

// Drivers returns the names of the listed drivers, sorted.
func (f *Fleet) Drivers() []string {
	out := make([]string, 0, len(f.drivers))
	for name := range f.drivers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
`,
			"docs/service-policy.txt": `Servicing policy, fleet office, revision 7

Every vehicle has two limits since its last service, one of distance and one of
time, and they depend on its class. Whichever is reached first makes the vehicle
due. Reaching a limit is enough, it does not have to be passed.

  class    distance    time
  car      15000 km    365 days
  van      20000 km    270 days
  truck    30000 km    180 days

Only a service resets the limits. When the work order of a service is closed, the
vehicle's last-service distance and day become the closing reading and the closing
day. A repair leaves them where they were, whatever the repair was.

A retired vehicle is never due and does not go on any list of due vehicles.

Tyre checks are done by the drivers and are not part of this policy. Brake
inspections on trucks are booked by the transport manager and are tracked in the
yard diary, not here. Hire vehicles stay on the hire company's own schedule.
`,
			"docs/leasing-export.txt": `Monthly file for the leasing company

We send them one text file a month. Every line, the last one too, ends in a line
feed. Fields are separated by a semicolon, because their import tool breaks on
commas.

The first line is always exactly:

  REG;CLASS;KM_THOUSANDS;STATE

Then comes one line for each vehicle on our register that is not retired, in
registration order:

  REG            the registration as we store it
  CLASS          the class in capital letters
  KM_THOUSANDS   the odometer in whole thousands, cut off and not rounded,
                 so 15999 km is 15
  STATE          DUE if the vehicle is due for service on the day of the file,
                 otherwise OK

Their contact is the contracts desk. They have asked for the file by the fifth
working day of the month. Send corrections as a whole new file, never as a patch.
`,
			"fleet_test.go": `package fleet

import (
	"errors"
	"slices"
	"testing"
)

func TestAddAndCount(t *testing.T) {
	f := New()
	if err := f.Add(Vehicle{Reg: "AA1", Class: "car"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Add(Vehicle{Reg: "AA1", Class: "van"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second Add: %v", err)
	}
	if f.Count() != 1 {
		t.Fatalf("Count = %d", f.Count())
	}
}

func TestRegsAreSorted(t *testing.T) {
	f := New()
	for _, reg := range []string{"CC3", "AA1", "BB2"} {
		if err := f.Add(Vehicle{Reg: reg, Class: "car"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.Regs(); !slices.Equal(got, []string{"AA1", "BB2", "CC3"}) {
		t.Fatalf("Regs = %v", got)
	}
}

func TestOrderIDFormat(t *testing.T) {
	if got := orderID(7); got != "WO-0007" {
		t.Fatalf("orderID = %q", got)
	}
}

func TestSummaryCountsVehicles(t *testing.T) {
	f := New()
	if err := f.Add(Vehicle{Reg: "AA1", Class: "car", Retired: true}); err != nil {
		t.Fatal(err)
	}
	if got := f.Summary(); got != "vehicles: 1\nretired: 1\nwork orders: 0\n" {
		t.Fatalf("Summary = %q", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package fleet_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	fleet "fixture"
)

func vehicle(reg, class string, km, lastDay int) fleet.Vehicle {
	return fleet.Vehicle{Reg: reg, Class: class, Odometer: km, LastServiceDay: lastDay}
}

func newFleet(t *testing.T, vs ...fleet.Vehicle) *fleet.Fleet {
	t.Helper()
	f := fleet.New()
	for _, v := range vs {
		if err := f.Add(v); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func open(t *testing.T, f *fleet.Fleet, reg, kind string, day int) string {
	t.Helper()
	id, err := f.Open(reg, kind, day)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func closed(t *testing.T, f *fleet.Fleet, id string, day, km int) {
	t.Helper()
	if err := f.Close(id, day, km); err != nil {
		t.Fatal(err)
	}
}

func checkDue(t *testing.T, class string, km, days int, want bool) {
	t.Helper()
	f := newFleet(t, vehicle("AA1", class, km, 100))
	got, err := f.Due("AA1", 100+days)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("%s at %d km and %d days: due = %v", class, km, days, got)
	}
}

func TestGetAnySpelling(t *testing.T) {
	f := newFleet(t, vehicle("ab12 cde", "car", 0, 1))
	for _, typed := range []string{"AB12CDE", "ab12cde", " Ab12 Cde"} {
		v, ok := f.Get(typed)
		if !ok || v.Reg != "AB12CDE" {
			t.Errorf("Get(%q) = %+v, %v", typed, v, ok)
		}
	}
	if err := f.Add(vehicle("AB12 CDE", "van", 0, 1)); !errors.Is(err, fleet.ErrDuplicate) {
		t.Errorf("second Add: %v", err)
	}
}

func TestActiveSortedWithoutRetired(t *testing.T) {
	f := newFleet(t, vehicle("CC3", "car", 0, 1), vehicle("AA1", "car", 0, 1), vehicle("BB2", "car", 0, 1))
	if err := f.Retire("BB2"); err != nil {
		t.Fatal(err)
	}
	if got := f.Active(); !slices.Equal(got, []string{"AA1", "CC3"}) {
		t.Errorf("Active = %v", got)
	}
}

func TestSetOdometerRules(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 5000, 1))
	if err := f.SetOdometer("AA1", 5000); err != nil {
		t.Errorf("same reading: %v", err)
	}
	if err := f.SetOdometer("AA1", 4999); !errors.Is(err, fleet.ErrOdometerBack) {
		t.Errorf("lower reading: %v", err)
	}
	if err := f.SetOdometer("AA1", 6000); err != nil {
		t.Errorf("higher reading: %v", err)
	}
	if v, _ := f.Get("AA1"); v.Odometer != 6000 {
		t.Errorf("Odometer = %d", v.Odometer)
	}
}

func TestSetOdometerAnySpelling(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 5000, 1))
	if err := f.SetOdometer(" aa 1", 6000); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.Get("AA1"); v.Odometer != 6000 {
		t.Errorf("Odometer = %d", v.Odometer)
	}
}

func TestRetireAnySpelling(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	if err := f.Retire("aa 1"); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.Get("AA1"); !v.Retired {
		t.Error("not retired")
	}
}

func TestOpenAnySpelling(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	open(t, f, "aa 1", "repair", 2)
	if got := f.Orders("AA1"); len(got) != 1 || got[0].Reg != "AA1" {
		t.Errorf("Orders = %+v", got)
	}
}

func TestOrdersAnySpelling(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	open(t, f, "AA1", "repair", 2)
	if got := f.Orders("aa 1"); len(got) != 1 {
		t.Errorf("Orders = %+v", got)
	}
}

func TestDueAnySpelling(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 500000, 1))
	if got, err := f.Due("aa 1", 1); err != nil || !got {
		t.Errorf("Due = %v, %v", got, err)
	}
}

func TestKmLeftAnySpelling(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 5000, 1))
	if _, err := f.KmLeft("aa 1"); err != nil {
		t.Errorf("KmLeft: %v", err)
	}
}

func TestSpendAnySpelling(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	id := open(t, f, "AA1", "repair", 2)
	if err := f.AddLabour(id, 60); err != nil {
		t.Fatal(err)
	}
	closed(t, f, id, 3, 0)
	if got, err := f.Spend("aa 1"); err != nil || got != 7800 {
		t.Errorf("Spend = %d, %v", got, err)
	}
}

func TestSetOdometerUnknown(t *testing.T) {
	if err := newFleet(t).SetOdometer("ZZ9", 1); !errors.Is(err, fleet.ErrUnknownVehicle) {
		t.Errorf("err = %v", err)
	}
}

func TestRetireUnknown(t *testing.T) {
	if err := newFleet(t).Retire("ZZ9"); !errors.Is(err, fleet.ErrUnknownVehicle) {
		t.Errorf("err = %v", err)
	}
}

func TestOpenUnknown(t *testing.T) {
	if _, err := newFleet(t).Open("ZZ9", "repair", 1); !errors.Is(err, fleet.ErrUnknownVehicle) {
		t.Errorf("err = %v", err)
	}
}

func TestDueUnknown(t *testing.T) {
	if _, err := newFleet(t).Due("ZZ9", 1); !errors.Is(err, fleet.ErrUnknownVehicle) {
		t.Errorf("err = %v", err)
	}
}

func TestKmLeftUnknown(t *testing.T) {
	if _, err := newFleet(t).KmLeft("ZZ9"); !errors.Is(err, fleet.ErrUnknownVehicle) {
		t.Errorf("err = %v", err)
	}
}

func TestSpendUnknown(t *testing.T) {
	if _, err := newFleet(t).Spend("ZZ9"); !errors.Is(err, fleet.ErrUnknownVehicle) {
		t.Errorf("err = %v", err)
	}
}

func TestOpenIDsRunInSequence(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1), vehicle("BB2", "van", 0, 1))
	if got := open(t, f, "AA1", "repair", 2); got != "WO-0001" {
		t.Errorf("first ID %q", got)
	}
	if got := open(t, f, "BB2", "service", 2); got != "WO-0002" {
		t.Errorf("second ID %q", got)
	}
	if got := f.OpenOrders(); got != 2 {
		t.Errorf("OpenOrders = %d", got)
	}
}

func TestOpenRefusals(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	if _, err := f.Open("AA1", "wash", 2); !errors.Is(err, fleet.ErrBadKind) {
		t.Errorf("bad kind: %v", err)
	}
	if err := f.Retire("AA1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Open("AA1", "repair", 2); !errors.Is(err, fleet.ErrRetired) {
		t.Errorf("retired: %v", err)
	}
}

func TestLinesNeedAnOpenOrder(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	if err := f.AddLabour("WO-0099", 60); !errors.Is(err, fleet.ErrNoOrder) {
		t.Errorf("AddLabour on a missing order: %v", err)
	}
	if err := f.AddPart("WO-0099", "belt", 100); !errors.Is(err, fleet.ErrNoOrder) {
		t.Errorf("AddPart on a missing order: %v", err)
	}
	id := open(t, f, "AA1", "repair", 2)
	closed(t, f, id, 3, 10)
	if err := f.AddLabour(id, 60); !errors.Is(err, fleet.ErrClosed) {
		t.Errorf("AddLabour on a closed order: %v", err)
	}
	if err := f.Close(id, 4, 10); !errors.Is(err, fleet.ErrClosed) {
		t.Errorf("second Close: %v", err)
	}
}

func TestCloseRecordsReading(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 5000, 1))
	id := open(t, f, "AA1", "repair", 2)
	closed(t, f, id, 3, 6000)
	if v, _ := f.Get("AA1"); v.Odometer != 6000 {
		t.Errorf("Odometer = %d", v.Odometer)
	}
	if got := f.OpenOrders(); got != 0 {
		t.Errorf("OpenOrders = %d", got)
	}
}

func TestCloseLowerReading(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 5000, 1))
	id := open(t, f, "AA1", "repair", 2)
	if err := f.Close(id, 3, 4000); !errors.Is(err, fleet.ErrOdometerBack) {
		t.Errorf("err = %v", err)
	}
	if v, _ := f.Get("AA1"); v.Odometer != 5000 {
		t.Errorf("Odometer = %d", v.Odometer)
	}
}

func TestServiceMovesLastService(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 5000, 1))
	id := open(t, f, "AA1", "service", 2)
	closed(t, f, id, 3, 6000)
	if v, _ := f.Get("AA1"); v.LastServiceKm != 6000 || v.LastServiceDay != 3 {
		t.Errorf("last service %d km, day %d", v.LastServiceKm, v.LastServiceDay)
	}
}

func TestRepairKeepsLastService(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 5000, 1))
	id := open(t, f, "AA1", "repair", 2)
	closed(t, f, id, 3, 6000)
	if v, _ := f.Get("AA1"); v.LastServiceKm != 0 || v.LastServiceDay != 1 {
		t.Errorf("last service %d km, day %d", v.LastServiceKm, v.LastServiceDay)
	}
}

func TestRetiredIsNotDue(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 500000, 1))
	if err := f.Retire("AA1"); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Due("AA1", 5000); err != nil || got {
		t.Errorf("Due = %v, %v", got, err)
	}
}

func TestDueListLeavesOutRetired(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 500000, 1), vehicle("BB2", "car", 500000, 1))
	if err := f.Retire("AA1"); err != nil {
		t.Fatal(err)
	}
	if got := f.DueList(1); !slices.Equal(got, []string{"BB2"}) {
		t.Errorf("DueList = %v", got)
	}
}

func TestForecastRetired(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 100))
	if err := f.Retire("AA1"); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Forecast("AA1", 100, 100); err != nil || got != -1 {
		t.Errorf("Forecast = %d, %v", got, err)
	}
}

func TestDueFreshVehicle(t *testing.T) {
	checkDue(t, "car", 0, 0, false)
	checkDue(t, "truck", 0, 0, false)
}

func TestDueCarDistance(t *testing.T) {
	checkDue(t, "car", 14999, 0, false)
	checkDue(t, "car", 15000, 0, true)
}

func TestDueVanDistance(t *testing.T) {
	checkDue(t, "van", 19999, 0, false)
	checkDue(t, "van", 20000, 0, true)
}

func TestDueTruckDistance(t *testing.T) {
	checkDue(t, "truck", 29999, 0, false)
	checkDue(t, "truck", 30000, 0, true)
}

func TestDueCarTime(t *testing.T) {
	checkDue(t, "car", 0, 364, false)
	checkDue(t, "car", 0, 365, true)
}

func TestDueVanTime(t *testing.T) {
	checkDue(t, "van", 0, 269, false)
	checkDue(t, "van", 0, 270, true)
}

func TestDueTruckTime(t *testing.T) {
	checkDue(t, "truck", 0, 179, false)
	checkDue(t, "truck", 0, 180, true)
}

func TestKmLeftVan(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "van", 5000, 1))
	if got, err := f.KmLeft("AA1"); err != nil || got != 15000 {
		t.Errorf("KmLeft = %d, %v", got, err)
	}
}

func TestKmLeftPastTheLimit(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "truck", 35000, 1))
	if got, err := f.KmLeft("AA1"); err != nil || got != -5000 {
		t.Errorf("KmLeft = %d, %v", got, err)
	}
}

func TestForecastDistanceFirst(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "van", 19050, 100))
	if got, err := f.Forecast("AA1", 100, 100); err != nil || got != 110 {
		t.Errorf("Forecast = %d, %v", got, err)
	}
}

func TestForecastTimeFirst(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "truck", 0, 100))
	if got, err := f.Forecast("AA1", 1, 100); err != nil || got != 280 {
		t.Errorf("Forecast = %d, %v", got, err)
	}
}

func TestForecastNotDriving(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "truck", 10, 100))
	if got, err := f.Forecast("AA1", 0, 120); err != nil || got != 280 {
		t.Errorf("Forecast = %d, %v", got, err)
	}
}

func TestForecastAlreadyDue(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 500000, 100))
	if got, err := f.Forecast("AA1", 50, 120); err != nil || got != 120 {
		t.Errorf("Forecast = %d, %v", got, err)
	}
}

func TestDueListMostOverdueFirst(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 16000, 100), vehicle("BB2", "truck", 45000, 100), vehicle("CC3", "car", 60000, 100), vehicle("DD4", "car", 0, 100))
	if got := f.DueList(100); !slices.Equal(got, []string{"CC3", "BB2", "AA1"}) {
		t.Errorf("DueList = %v", got)
	}
}

func TestDueListTiesByRegistration(t *testing.T) {
	f := newFleet(t, vehicle("BB2", "car", 90000, 100), vehicle("AA1", "car", 90000, 100))
	if got := f.DueList(100); !slices.Equal(got, []string{"AA1", "BB2"}) {
		t.Errorf("DueList = %v", got)
	}
}

func TestLabourWholeHours(t *testing.T) {
	for minutes, want := range map[int]int{0: 0, 60: 7800, 120: 15600} {
		if got := fleet.LabourCents(minutes); got != want {
			t.Errorf("LabourCents(%d) = %d", minutes, got)
		}
	}
}

func TestLabourMinimum(t *testing.T) {
	if got := fleet.LabourCents(10); got != 7800 {
		t.Errorf("LabourCents(10) = %d", got)
	}
}

func TestLabourStartedBlocks(t *testing.T) {
	if got := fleet.LabourCents(61); got != 11700 {
		t.Errorf("LabourCents(61) = %d", got)
	}
}

func TestCostTotalLabourTime(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	id := open(t, f, "AA1", "repair", 2)
	for _, minutes := range []int{40, 41} {
		if err := f.AddLabour(id, minutes); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := f.Cost(id); err != nil || got != 11700 {
		t.Errorf("Cost = %d, %v", got, err)
	}
}

func TestCostStartedBlocks(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	id := open(t, f, "AA1", "repair", 2)
	if err := f.AddLabour(id, 61); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Cost(id); err != nil || got != 11700 {
		t.Errorf("Cost = %d, %v", got, err)
	}
}

func TestCostMissingOrder(t *testing.T) {
	if _, err := newFleet(t).Cost("WO-0099"); !errors.Is(err, fleet.ErrNoOrder) {
		t.Errorf("err = %v", err)
	}
}

func TestCostPartCharge(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	id := open(t, f, "AA1", "repair", 2)
	if err := f.AddPart(id, "belt", 1000); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Cost(id); err != nil || got != 1150 {
		t.Errorf("Cost = %d, %v", got, err)
	}
}

func TestCostPartRounding(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	id := open(t, f, "AA1", "repair", 2)
	for range 2 {
		if err := f.AddPart(id, "clip", 10); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := f.Cost(id); err != nil || got != 24 {
		t.Errorf("Cost = %d, %v", got, err)
	}
}

func TestQuotePartCharge(t *testing.T) {
	if got := fleet.Quote(0, []int{10, 1000}); got != 1162 {
		t.Errorf("Quote = %d", got)
	}
}

func TestQuoteStartedBlocks(t *testing.T) {
	if got := fleet.Quote(61, nil); got != 11700 {
		t.Errorf("Quote = %d", got)
	}
}

func TestDaysOfAnOrder(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	first := open(t, f, "AA1", "repair", 10)
	second := open(t, f, "AA1", "repair", 10)
	closed(t, f, first, 10, 0)
	days := map[string]int{}
	for _, o := range f.Orders("AA1") {
		days[o.ID] = o.Days()
	}
	if days[first] != 1 || days[second] != 0 {
		t.Errorf("Days = %v", days)
	}
	closed(t, f, second, 12, 0)
	if got := f.Orders("AA1")[1].Days(); got != 3 {
		t.Errorf("Days = %d", got)
	}
}

func downtime(t *testing.T, spans ...[2]int) int {
	t.Helper()
	f := newFleet(t, vehicle("AA1", "car", 0, 1), vehicle("BB2", "car", 0, 1))
	for _, s := range spans {
		closed(t, f, open(t, f, "AA1", "repair", s[0]), s[1], 0)
	}
	closed(t, f, open(t, f, "BB2", "repair", 1), 90, 0)
	open(t, f, "AA1", "repair", 200)
	got, err := f.Downtime("AA1")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestDowntimeSingleOrder(t *testing.T) {
	if got := downtime(t, [2]int{10, 12}); got != 3 {
		t.Errorf("Downtime = %d", got)
	}
}

func TestDowntimeOpenedAndClosedAlike(t *testing.T) {
	if got := downtime(t, [2]int{10, 10}, [2]int{20, 20}); got != 2 {
		t.Errorf("Downtime = %d", got)
	}
}

func TestDowntimeOverlap(t *testing.T) {
	if got := downtime(t, [2]int{20, 24}, [2]int{10, 12}, [2]int{12, 14}, [2]int{21, 22}); got != 10 {
		t.Errorf("Downtime = %d", got)
	}
}

func TestStatusValues(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 100), vehicle("BB2", "car", 500000, 100), vehicle("CC3", "car", 500000, 100), vehicle("DD4", "car", 500000, 100))
	open(t, f, "CC3", "repair", 100)
	if err := f.Retire("DD4"); err != nil {
		t.Fatal(err)
	}
	for reg, want := range map[string]string{"AA1": "ok", "BB2": "due", "CC3": "workshop", "DD4": "retired"} {
		if got, err := f.Status(reg, 100); err != nil || got != want {
			t.Errorf("Status(%s) = %q, %v", reg, got, err)
		}
	}
}

func TestSpendClosedOrdersOnly(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1), vehicle("BB2", "car", 0, 1))
	for _, reg := range []string{"AA1", "AA1", "BB2"} {
		id := open(t, f, reg, "repair", 2)
		if err := f.AddLabour(id, 60); err != nil {
			t.Fatal(err)
		}
		closed(t, f, id, 3, 0)
	}
	id := open(t, f, "AA1", "repair", 4)
	if err := f.AddLabour(id, 120); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Spend("AA1"); err != nil || got != 15600 {
		t.Errorf("Spend = %d, %v", got, err)
	}
}

func TestDueLineCounts(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 100), vehicle("BB2", "car", 500000, 100), vehicle("CC3", "car", 0, 100))
	if err := f.Retire("CC3"); err != nil {
		t.Fatal(err)
	}
	if got := f.DueLine(100); got != "due: 1 of 2" {
		t.Errorf("DueLine = %q", got)
	}
}

func driverFleet(t *testing.T) *fleet.Fleet {
	t.Helper()
	f := newFleet(t, vehicle("AA1", "car", 0, 100), vehicle("BB2", "van", 0, 100), vehicle("TT9", "truck", 0, 100))
	for _, d := range []fleet.Driver{{Name: "ann", Licence: "B"}, {Name: "bob", Licence: "C"}, {Name: "cy", Licence: "B"}} {
		if err := f.AddDriver(d); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func driverOf(t *testing.T, f *fleet.Fleet, reg string) string {
	t.Helper()
	got, err := f.DriverOf(reg)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAssignReplacesDriver(t *testing.T) {
	f := driverFleet(t)
	if got := driverOf(t, f, "AA1"); got != "" {
		t.Errorf("before any Assign: %q", got)
	}
	for _, name := range []string{"ann", "bob"} {
		if err := f.Assign("AA1", name); err != nil {
			t.Fatal(err)
		}
	}
	if got := driverOf(t, f, "AA1"); got != "bob" {
		t.Errorf("DriverOf = %q", got)
	}
}

func TestAssignRefusals(t *testing.T) {
	f := driverFleet(t)
	if err := f.Assign("AA1", "zed"); !errors.Is(err, fleet.ErrNoDriver) {
		t.Errorf("unlisted driver: %v", err)
	}
	if err := f.Assign("TT9", "ann"); !errors.Is(err, fleet.ErrLicence) {
		t.Errorf("wrong licence: %v", err)
	}
	if err := f.Assign("TT9", "bob"); err != nil {
		t.Errorf("right licence: %v", err)
	}
}

func TestAssignAnySpelling(t *testing.T) {
	f := driverFleet(t)
	if err := f.Assign("aa 1", "ann"); err != nil {
		t.Fatal(err)
	}
	if got := driverOf(t, f, "AA1"); got != "ann" {
		t.Errorf("DriverOf = %q", got)
	}
}

func TestAssignUnknown(t *testing.T) {
	if err := driverFleet(t).Assign("ZZ9", "ann"); !errors.Is(err, fleet.ErrUnknownVehicle) {
		t.Errorf("err = %v", err)
	}
}

func TestDriverOfUnknown(t *testing.T) {
	if _, err := driverFleet(t).DriverOf("ZZ9"); !errors.Is(err, fleet.ErrUnknownVehicle) {
		t.Errorf("err = %v", err)
	}
}

func TestOneVehiclePerDriver(t *testing.T) {
	f := driverFleet(t)
	for _, reg := range []string{"AA1", "BB2"} {
		if err := f.Assign(reg, "ann"); err != nil {
			t.Fatal(err)
		}
	}
	if a, b := driverOf(t, f, "AA1"), driverOf(t, f, "BB2"); a != "" || b != "ann" {
		t.Errorf("AA1 has %q, BB2 has %q", a, b)
	}
}

func TestRetireTakesVehicleOffDriver(t *testing.T) {
	f := driverFleet(t)
	if err := f.Assign("AA1", "ann"); err != nil {
		t.Fatal(err)
	}
	if err := f.Retire("AA1"); err != nil {
		t.Fatal(err)
	}
	if got := driverOf(t, f, "AA1"); got != "" {
		t.Errorf("DriverOf = %q", got)
	}
}

func TestDriverSheetLines(t *testing.T) {
	f := driverFleet(t)
	if err := f.Assign("TT9", "bob"); err != nil {
		t.Fatal(err)
	}
	if err := f.Assign("AA1", "ann"); err != nil {
		t.Fatal(err)
	}
	open(t, f, "AA1", "repair", 100)
	want := []string{"ann: AA1 (workshop)", "bob: TT9 (ok)", "cy: none"}
	if got := f.DriverSheet(100); !slices.Equal(got, want) {
		t.Errorf("DriverSheet = %q", got)
	}
}

func TestInvoiceLines(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	id := open(t, f, "AA1", "repair", 2)
	if err := f.AddLabour(id, 60); err != nil {
		t.Fatal(err)
	}
	if err := f.AddPart(id, "belt", 1000); err != nil {
		t.Fatal(err)
	}
	want := []string{"belt: 1150", "labour: 7800", "total: 8950"}
	if got, err := f.Invoice(id); err != nil || !slices.Equal(got, want) {
		t.Errorf("Invoice = %q, %v", got, err)
	}
}

func TestInvoiceLabourOnly(t *testing.T) {
	f := newFleet(t, vehicle("AA1", "car", 0, 1))
	id := open(t, f, "AA1", "repair", 2)
	if err := f.AddLabour(id, 60); err != nil {
		t.Fatal(err)
	}
	want := []string{"labour: 7800", "total: 7800"}
	if got, err := f.Invoice(id); err != nil || !slices.Equal(got, want) {
		t.Errorf("Invoice = %q, %v", got, err)
	}
	if _, err := f.Invoice("WO-0099"); !errors.Is(err, fleet.ErrNoOrder) {
		t.Errorf("missing order: %v", err)
	}
}

func leaseFleet(t *testing.T) *fleet.Fleet {
	t.Helper()
	busy := vehicle("BB2", "van", 500999, 100)
	fresh := vehicle("AA1", "car", 15999, 100)
	fresh.LastServiceKm = 15999
	return newFleet(t, busy, fresh)
}

func TestLeaseExportHeader(t *testing.T) {
	first, _, _ := strings.Cut(leaseFleet(t).LeaseExport(100), "\n")
	if first != "REG;CLASS;KM_THOUSANDS;STATE" {
		t.Errorf("first line %q", first)
	}
}

func TestLeaseExportRows(t *testing.T) {
	want := "REG;CLASS;KM_THOUSANDS;STATE\nAA1;CAR;15;OK\nBB2;VAN;500;DUE\n"
	if got := leaseFleet(t).LeaseExport(100); got != want {
		t.Errorf("LeaseExport = %q", got)
	}
}

func TestLeaseExportLeavesOutRetired(t *testing.T) {
	f := leaseFleet(t)
	if err := f.Retire("AA1"); err != nil {
		t.Fatal(err)
	}
	if got := f.LeaseExport(100); strings.Contains(got, "AA1") {
		t.Errorf("LeaseExport = %q", got)
	}
}

func TestLeaseExportWholeThousands(t *testing.T) {
	got := leaseFleet(t).LeaseExport(100)
	if !strings.Contains(got, "15") || strings.Contains(got, "15999") || strings.Contains(got, "16") {
		t.Errorf("LeaseExport = %q", got)
	}
}
`},
		Solution: map[string]string{
			"vehicle.go": `package fleet

import (
	"errors"
	"sort"
	"strings"
)

var (
	// ErrDuplicate is returned when a registration is already on the register.
	ErrDuplicate = errors.New("duplicate vehicle")
	// ErrUnknownVehicle is returned by every call naming a vehicle that is missing.
	ErrUnknownVehicle = errors.New("unknown vehicle")
	// ErrOdometerBack is returned for a reading below the current one.
	ErrOdometerBack = errors.New("odometer went backwards")
)

// Vehicle is one entry on the register. Odometer and LastServiceKm are in
// kilometres. Days are plain day numbers, the way the office systems count them,
// and they start at 1.
type Vehicle struct {
	Reg            string
	Class          string // "car", "van" or "truck"
	Odometer       int
	LastServiceKm  int
	LastServiceDay int
	Retired        bool
}

// Fleet is the register and the work orders recorded against it.
type Fleet struct {
	vehicles map[string]*Vehicle
	orders   []*WorkOrder
	nextID   int
	drivers  map[string]Driver
	assigned map[string]string // driver by registration
}

// New returns an empty register.
func New() *Fleet {
	return &Fleet{vehicles: map[string]*Vehicle{}, nextID: 1, drivers: map[string]Driver{}, assigned: map[string]string{}}
}

// normReg is the form a registration is stored in.
func normReg(reg string) string {
	return strings.ToUpper(strings.ReplaceAll(reg, " ", ""))
}

// lookup finds a vehicle by any spelling of its registration.
func (f *Fleet) lookup(reg string) (*Vehicle, error) {
	v, ok := f.vehicles[normReg(reg)]
	if !ok {
		return nil, ErrUnknownVehicle
	}
	return v, nil
}

// Add puts a vehicle on the register.
func (f *Fleet) Add(v Vehicle) error {
	v.Reg = normReg(v.Reg)
	if _, ok := f.vehicles[v.Reg]; ok {
		return ErrDuplicate
	}
	f.vehicles[v.Reg] = &v
	return nil
}

// Get returns a copy of a vehicle.
func (f *Fleet) Get(reg string) (Vehicle, bool) {
	v, ok := f.vehicles[normReg(reg)]
	if !ok {
		return Vehicle{}, false
	}
	return *v, true
}

// SetOdometer records a new reading.
func (f *Fleet) SetOdometer(reg string, km int) error {
	v, err := f.lookup(reg)
	if err != nil {
		return err
	}
	if km < v.Odometer {
		return ErrOdometerBack
	}
	v.Odometer = km
	return nil
}

// Retire marks a vehicle as retired. Doing it twice changes nothing.
func (f *Fleet) Retire(reg string) error {
	v, err := f.lookup(reg)
	if err != nil {
		return err
	}
	v.Retired = true
	delete(f.assigned, v.Reg)
	return nil
}

// Regs returns every registration on the register, retired ones too, sorted.
func (f *Fleet) Regs() []string {
	out := make([]string, 0, len(f.vehicles))
	for reg := range f.vehicles {
		out = append(out, reg)
	}
	sort.Strings(out)
	return out
}

// Active returns the registrations of vehicles that are not retired, sorted.
func (f *Fleet) Active() []string {
	out := []string{}
	for _, reg := range f.Regs() {
		if !f.vehicles[reg].Retired {
			out = append(out, reg)
		}
	}
	return out
}

// Count returns the number of vehicles on the register, retired ones too.
func (f *Fleet) Count() int { return len(f.vehicles) }

// Classes returns how many vehicles of each class are on the register.
func (f *Fleet) Classes() map[string]int {
	out := map[string]int{}
	for _, v := range f.vehicles {
		out[v.Class]++
	}
	return out
}
`,
			"workorder.go": `package fleet

import (
	"errors"
	"fmt"
)

var (
	// ErrBadKind is returned for a work order kind we do not have.
	ErrBadKind = errors.New("unknown work order kind")
	// ErrRetired is returned when a retired vehicle would get a new work order.
	ErrRetired = errors.New("vehicle is retired")
	// ErrNoOrder is returned for an ID no work order has.
	ErrNoOrder = errors.New("no such work order")
	// ErrClosed is returned when a closed work order would change.
	ErrClosed = errors.New("work order is closed")
)

// Line is one entry on a work order: either labour, with Part empty and Minutes
// set, or a part, with Part naming it and Cents its price before any charges.
type Line struct {
	Part    string
	Minutes int
	Cents   int
}

// WorkOrder is one job in the workshop. Opened and Closed are day numbers, and
// Closed stays 0 while the order is open.
type WorkOrder struct {
	ID     string
	Reg    string
	Kind   string // "service" or "repair"
	Opened int
	Closed int
	Lines  []Line
}

// Days returns how many days the vehicle was off the road for this order.
func (o WorkOrder) Days() int {
	if o.Closed == 0 {
		return 0
	}
	return o.Closed - o.Opened + 1
}

// orderID formats the ID of the n-th work order.
func orderID(n int) string { return fmt.Sprintf("WO-%04d", n) }

// order returns the work order with an ID, or nil.
func (f *Fleet) order(id string) *WorkOrder {
	for _, o := range f.orders {
		if o.ID == id {
			return o
		}
	}
	return nil
}

// Orders returns copies of a vehicle's work orders, oldest first.
func (f *Fleet) Orders(reg string) []WorkOrder {
	key := normReg(reg)
	var out []WorkOrder
	for _, o := range f.orders {
		if o.Reg == key {
			out = append(out, *o)
		}
	}
	return out
}

// OpenOrders returns how many work orders are not closed.
func (f *Fleet) OpenOrders() int {
	n := 0
	for _, o := range f.orders {
		if o.Closed == 0 {
			n++
		}
	}
	return n
}

// Open opens a work order and returns its ID.
func (f *Fleet) Open(reg, kind string, day int) (string, error) {
	v, err := f.lookup(reg)
	if err != nil {
		return "", err
	}
	if kind != "service" && kind != "repair" {
		return "", ErrBadKind
	}
	if v.Retired {
		return "", ErrRetired
	}
	id := orderID(f.nextID)
	f.nextID++
	f.orders = append(f.orders, &WorkOrder{ID: id, Reg: v.Reg, Kind: kind, Opened: day})
	return id, nil
}

func (f *Fleet) addLine(id string, l Line) error {
	o := f.order(id)
	if o == nil {
		return ErrNoOrder
	}
	if o.Closed != 0 {
		return ErrClosed
	}
	o.Lines = append(o.Lines, l)
	return nil
}

// AddLabour adds labour time to an open work order.
func (f *Fleet) AddLabour(id string, minutes int) error {
	return f.addLine(id, Line{Minutes: minutes})
}

// AddPart adds a part to an open work order.
func (f *Fleet) AddPart(id, name string, cents int) error {
	return f.addLine(id, Line{Part: name, Cents: cents})
}

// Close closes a work order on a day with an odometer reading.
func (f *Fleet) Close(id string, day, km int) error {
	o := f.order(id)
	if o == nil {
		return ErrNoOrder
	}
	if o.Closed != 0 {
		return ErrClosed
	}
	if err := f.SetOdometer(o.Reg, km); err != nil {
		return err
	}
	o.Closed = day
	if o.Kind == "service" {
		f.vehicles[o.Reg].LastServiceKm = km
		f.vehicles[o.Reg].LastServiceDay = day
	}
	return nil
}
`,
			"service.go": `package fleet

import "sort"

// Limit is how far a vehicle may go between services.
type Limit struct {
	Km   int
	Days int
}

var limits = map[string]Limit{
	"car":   {15000, 365},
	"van":   {20000, 270},
	"truck": {30000, 180},
}

// KmSinceService returns how far the vehicle has gone since its last service.
func (v Vehicle) KmSinceService() int { return v.Odometer - v.LastServiceKm }

// DaysSinceService returns the days from its last service to today.
func (v Vehicle) DaysSinceService(today int) int { return today - v.LastServiceDay }

func isDue(v Vehicle, today int) bool {
	if v.Retired {
		return false
	}
	l := limits[v.Class]
	return v.KmSinceService() >= l.Km || v.DaysSinceService(today) >= l.Days
}

// Due reports whether a vehicle is due for service today.
func (f *Fleet) Due(reg string, today int) (bool, error) {
	v, err := f.lookup(reg)
	if err != nil {
		return false, err
	}
	return isDue(*v, today), nil
}

// usedUp returns the larger of the fractions a vehicle has used of its limits.
func usedUp(v Vehicle, today int) float64 {
	l := limits[v.Class]
	return max(float64(v.KmSinceService())/float64(l.Km), float64(v.DaysSinceService(today))/float64(l.Days))
}

// DueList returns the registrations that are due, most overdue first.
func (f *Fleet) DueList(today int) []string {
	out := []string{}
	for _, reg := range f.Regs() {
		if isDue(*f.vehicles[reg], today) {
			out = append(out, reg)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return usedUp(*f.vehicles[out[a]], today) > usedUp(*f.vehicles[out[b]], today)
	})
	return out
}

// KmLeft returns how many km are left until the distance limit, negative when past it.
func (f *Fleet) KmLeft(reg string) (int, error) {
	v, err := f.lookup(reg)
	if err != nil {
		return 0, err
	}
	return limits[v.Class].Km - v.KmSinceService(), nil
}

// Status returns "retired", "workshop", "due" or "ok", checked in that order.
func (f *Fleet) Status(reg string, today int) (string, error) {
	v, err := f.lookup(reg)
	if err != nil {
		return "", err
	}
	if v.Retired {
		return "retired", nil
	}
	for _, o := range f.orders {
		if o.Reg == v.Reg && o.Closed == 0 {
			return "workshop", nil
		}
	}
	if isDue(*v, today) {
		return "due", nil
	}
	return "ok", nil
}

// Forecast returns the day a vehicle becomes due if it drives kmPerDay from today
// on, or -1 if it will not.
func (f *Fleet) Forecast(reg string, kmPerDay, today int) (int, error) {
	v, err := f.lookup(reg)
	if err != nil {
		return 0, err
	}
	if v.Retired {
		return -1, nil
	}
	if isDue(*v, today) {
		return today, nil
	}
	l := limits[v.Class]
	day := v.LastServiceDay + l.Days
	if kmPerDay > 0 {
		left := l.Km - v.KmSinceService()
		day = min(day, today+(left+kmPerDay-1)/kmPerDay)
	}
	return day, nil
}
`,
			"drivers.go": `package fleet

import (
	"errors"
	"fmt"
	"sort"
)

var (
	// ErrDuplicateDriver is returned when a driver's name is already listed.
	ErrDuplicateDriver = errors.New("duplicate driver")
	// ErrNoDriver is returned for a name that is not on the list of drivers.
	ErrNoDriver = errors.New("no such driver")
	// ErrLicence is returned when a driver's licence does not cover the vehicle.
	ErrLicence = errors.New("licence does not cover this vehicle")
)

// Driver is someone who can be given a vehicle. Licence "B" covers cars and vans,
// and licence "C" covers trucks as well.
type Driver struct {
	Name    string
	Licence string
}

// AddDriver puts a driver on the list.
func (f *Fleet) AddDriver(d Driver) error {
	if _, ok := f.drivers[d.Name]; ok {
		return ErrDuplicateDriver
	}
	f.drivers[d.Name] = d
	return nil
}

// Drivers returns the names of the listed drivers, sorted.
func (f *Fleet) Drivers() []string {
	out := make([]string, 0, len(f.drivers))
	for name := range f.drivers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Assign gives a driver a vehicle, replacing whoever had it. A driver has one
// vehicle at a time, so this also takes them off any earlier one.
func (f *Fleet) Assign(reg, driver string) error {
	v, err := f.lookup(reg)
	if err != nil {
		return err
	}
	d, ok := f.drivers[driver]
	if !ok {
		return ErrNoDriver
	}
	if v.Class == "truck" && d.Licence != "C" {
		return ErrLicence
	}
	for held, name := range f.assigned {
		if name == driver {
			delete(f.assigned, held)
		}
	}
	f.assigned[v.Reg] = driver
	return nil
}

// DriverOf returns who has a vehicle, or an empty string if nobody does.
func (f *Fleet) DriverOf(reg string) (string, error) {
	v, err := f.lookup(reg)
	if err != nil {
		return "", err
	}
	return f.assigned[v.Reg], nil
}

// DriverSheet returns one line per driver, sorted by name, with the vehicle each
// one has and its status today.
func (f *Fleet) DriverSheet(today int) []string {
	held := map[string]string{}
	for reg, name := range f.assigned {
		held[name] = reg
	}
	out := []string{}
	for _, name := range f.Drivers() {
		reg, ok := held[name]
		if !ok {
			out = append(out, name+": none")
			continue
		}
		status, err := f.Status(reg, today)
		if err != nil {
			status = "unknown"
		}
		out = append(out, fmt.Sprintf("%s: %s (%s)", name, reg, status))
	}
	return out
}
`,
			"billing.go": `package fleet

import (
	"fmt"
	"strconv"
)

const (
	labourRate  = 7800 // cents an hour
	labourBlock = 30   // minutes, and a started block is charged whole
	minLabour   = 60   // minutes charged to any order with labour on it
	partCharge  = 15   // percent added to each part
)

// LabourCents returns what a work order's total labour time costs.
func LabourCents(minutes int) int {
	if minutes <= 0 {
		return 0
	}
	billed := max((minutes+labourBlock-1)/labourBlock*labourBlock, minLabour)
	return billed * labourRate / 60
}

// partCents returns what one part comes to with its charge.
func partCents(cents int) int { return cents + (cents*partCharge+50)/100 }

// jobCents prices a job from its total labour time and its part prices.
func jobCents(minutes int, parts []int) int {
	total := LabourCents(minutes)
	for _, cents := range parts {
		total += partCents(cents)
	}
	return total
}

// Cost returns what a work order comes to in cents, labour and parts.
func (f *Fleet) Cost(id string) (int, error) {
	o := f.order(id)
	if o == nil {
		return 0, ErrNoOrder
	}
	minutes, parts := 0, []int(nil)
	for _, l := range o.Lines {
		if l.Part == "" {
			minutes += l.Minutes
		} else {
			parts = append(parts, l.Cents)
		}
	}
	return jobCents(minutes, parts), nil
}

// Quote returns what a job would come to before any work order exists. It comes
// to exactly what Cost says for the same work.
func Quote(minutes int, parts []int) int { return jobCents(minutes, parts) }

// Invoice returns a work order's bill as lines: each part in the order added, then
// the labour if there was any, then the total.
func (f *Fleet) Invoice(id string) ([]string, error) {
	o := f.order(id)
	if o == nil {
		return nil, ErrNoOrder
	}
	out := []string{}
	minutes := 0
	for _, l := range o.Lines {
		if l.Part == "" {
			minutes += l.Minutes
			continue
		}
		out = append(out, fmt.Sprintf("%s: %d", l.Part, partCents(l.Cents)))
	}
	if minutes > 0 {
		out = append(out, "labour: "+strconv.Itoa(LabourCents(minutes)))
	}
	total, err := f.Cost(id)
	if err != nil {
		return nil, err
	}
	return append(out, "total: "+strconv.Itoa(total)), nil
}
`,
			"report.go": `package fleet

import (
	"fmt"
	"sort"
	"strings"
)

// Summary returns the short overview the office pins to its noticeboard.
func (f *Fleet) Summary() string {
	retired := 0
	for _, v := range f.vehicles {
		if v.Retired {
			retired++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "vehicles: %d\n", len(f.vehicles))
	fmt.Fprintf(&b, "retired: %d\n", retired)
	fmt.Fprintf(&b, "work orders: %d\n", len(f.orders))
	return b.String()
}

// ClassLines returns one "class: count" line for each class, sorted by class.
func (f *Fleet) ClassLines() []string {
	counts := f.Classes()
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("%s: %d", name, counts[name]))
	}
	return out
}

// DueLine returns how many vehicles are due out of those not retired.
func (f *Fleet) DueLine(today int) string {
	return fmt.Sprintf("due: %d of %d", len(f.DueList(today)), len(f.Active()))
}

// Downtime returns the days a vehicle was off the road across its closed orders.
// Orders can overlap, and a day is only counted once.
func (f *Fleet) Downtime(reg string) (int, error) {
	v, err := f.lookup(reg)
	if err != nil {
		return 0, err
	}
	var spans [][2]int
	for _, o := range f.orders {
		if o.Reg == v.Reg && o.Closed != 0 {
			spans = append(spans, [2]int{o.Opened, o.Closed})
		}
	}
	sort.Slice(spans, func(a, b int) bool { return spans[a][0] < spans[b][0] })
	total, end := 0, 0
	for _, s := range spans {
		from := max(s[0], end+1)
		if s[1] >= from {
			total += s[1] - from + 1
			end = s[1]
		}
	}
	return total, nil
}

// Spend returns the total cost in cents of a vehicle's closed work orders.
func (f *Fleet) Spend(reg string) (int, error) {
	v, err := f.lookup(reg)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, o := range f.orders {
		if o.Reg == v.Reg && o.Closed != 0 {
			cost, err := f.Cost(o.ID)
			if err != nil {
				return 0, err
			}
			total += cost
		}
	}
	return total, nil
}

// LeaseExport returns the monthly file for the leasing company.
func (f *Fleet) LeaseExport(today int) string {
	var b strings.Builder
	b.WriteString("REG;CLASS;KM_THOUSANDS;STATE\n")
	for _, reg := range f.Regs() {
		v := f.vehicles[reg]
		if v.Retired {
			continue
		}
		state := "OK"
		if isDue(*v, today) {
			state = "DUE"
		}
		fmt.Fprintf(&b, "%s;%s;%d;%s\n", v.Reg, strings.ToUpper(v.Class), v.Odometer/1000, state)
	}
	return b.String()
}
`,
		},
		Turns: script(
			local("Read docs/service-policy.txt and tell me in a few lines what decides when a vehicle is due and what changes that. No code changes."),
			local("In vehicle.go add `func (f *Fleet) Active() []string`: the registrations of vehicles that are not retired, sorted."),
			cloud("People type registrations any old way, so \"ab12 cde\", \"AB12CDE\" and \" Ab12 Cde\" have to be the same vehicle on every call that takes a registration, now and from here on. Make `Add` and `Get` work like that, and store Reg in the upper-case, no-spaces form."),
			cloud("Add `var ErrUnknownVehicle = errors.New(\"unknown vehicle\")` to vehicle.go. Every call that names a vehicle not on the register returns it, including all the calls we add later. First one: `func (f *Fleet) SetOdometer(reg string, km int) error`, which records a new reading."),
			cloud("Finance has settled labour billing. Labour on a work order is charged on its total time, in started quarter hours, at 7800 cents an hour, and an order with any labour at all pays for at least one hour. Nothing bills yet, we'll get there. For now add `func (f *Fleet) OpenOrders() int` to workorder.go: how many work orders are not closed."),
			local("Add `func (f *Fleet) Retire(reg string) error` to vehicle.go: it marks the vehicle retired. Retiring one twice is fine."),
			local("The workshop manager says a reading can't drop. Add `var ErrOdometerBack = errors.New(\"odometer went backwards\")` to vehicle.go and have `SetOdometer` return it for a smaller reading, changing nothing. The same value again is fine."),
			local("In workorder.go add `func (f *Fleet) Open(reg, kind string, day int) (string, error)`: opens a work order and returns its ID from `orderID`, numbering in sequence. Kind must be \"service\" or \"repair\", anything else returns a new `ErrBadKind`. A retired vehicle returns a new `ErrRetired`."),
			local("Add `func (f *Fleet) AddLabour(id string, minutes int) error` and `func (f *Fleet) AddPart(id, name string, cents int) error` to workorder.go. A missing ID returns a new `ErrNoOrder`, a closed order a new `ErrClosed`. While you're there: Finance says every part carries a 15 percent handling charge on its own price, with half a cent rounding up. Nothing adds that up yet."),
			cloud("The leasing company sent docs/leasing-export.txt. Read it and tell me whether what Vehicle holds is enough to produce that file. No code changes."),
			local("Add `func LabourCents(minutes int) int` in a new file billing.go: what that much labour time on one work order costs under Finance's rule."),
			cloud("Add `func (f *Fleet) Close(id string, day, km int) error` to workorder.go: closes the order on that day with that odometer reading, and leaves the vehicle's record correct afterwards. `ErrNoOrder` and `ErrClosed` as before."),
			cloud("Add `func (f *Fleet) Due(reg string, today int) (bool, error)` to service.go: whether the vehicle is due for service today. Put the numbers it needs in a small table there."),
			cloud("Add `func (f *Fleet) DueList(today int) []string` to service.go: the registrations that are due, most overdue first. How overdue a vehicle is means the larger of its two fractions: distance since service over its distance limit, and days since service over its days limit. Ties go by registration."),
			local("Finance changed the labour billing: blocks are now started half hours. Update `LabourCents` in billing.go."),
			local("Add `func (o WorkOrder) Days() int` to workorder.go: how many days the vehicle was off the road for that order. Opened and closed on the same day counts as one day, and so on upward. An open order gives 0."),
			cloud("Add `func (f *Fleet) Cost(id string) (int, error)` in billing.go: what the work order comes to in cents, labour and parts. A missing ID as before."),
			cloud("Every call that takes a registration still does its own map access. Add `func (f *Fleet) lookup(reg string) (*Vehicle, error)` to vehicle.go and move vehicle.go, service.go and workorder.go onto it, `Orders` included. No change in what they do."),
			local("Add `func (f *Fleet) KmLeft(reg string) (int, error)` to service.go: how many km are left until the distance limit, negative when past it."),
			cloud("Add `func (f *Fleet) Downtime(reg string) (int, error)` to report.go: days off the road across all its closed orders. Orders can overlap, for instance a repair during a service, and a day is only counted once."),
			local("Add `func (f *Fleet) Assign(reg, driver string) error` to drivers.go: gives that driver the vehicle, replacing whoever had it. A name not on the list of drivers returns a new `ErrNoDriver`. A truck needs a driver with licence \"C\", otherwise a new `ErrLicence`."),
			local("Add `func (f *Fleet) DriverOf(reg string) (string, error)` to drivers.go: who has the vehicle, or \"\" if nobody does."),
			local("Add `func (f *Fleet) LeaseExport(today int) string` to report.go: the monthly file for the leasing company."),
			local("Add `func (f *Fleet) Status(reg string, today int) (string, error)` to service.go: \"retired\" if retired, \"workshop\" if it has an order open, \"due\" if due, otherwise \"ok\", checked in that order."),
			local("Add `func (f *Fleet) Spend(reg string) (int, error)` to report.go: the total cost in cents of its closed work orders."),
			cloud("Two more rules for drivers, now that the calls exist. A driver has one vehicle at a time, so `Assign` also takes them off any earlier vehicle. And `Retire` takes the vehicle off its driver. This touches drivers.go and vehicle.go."),
			cloud("Add `func Quote(minutes int, parts []int) int` to billing.go: what a job would come to before any work order exists, from its labour minutes and part prices in cents. It has to come to exactly what `Cost` says for the same work, so share the code rather than copy it."),
			cloud("Add `func (f *Fleet) Forecast(reg string, kmPerDay, today int) (int, error)` to service.go: the first day the vehicle is due if it drives kmPerDay every day from today on, with kmPerDay possibly 0. If it is due already, that is today. If it will not ever become due, -1."),
			local("Add `func (f *Fleet) Invoice(id string) ([]string, error)` to billing.go: the bill as lines. First `name: cents` for each part in the order added, at what that part really comes to, then `labour: cents` if there was any labour, then `total: cents`. A missing ID as before."),
			local("Add `func (f *Fleet) DueLine(today int) string` to report.go: `due: N of M`, where N is how many vehicles are due and M how many are not retired."),
			local("Add `func (f *Fleet) DriverSheet(today int) []string` to drivers.go: one line per driver sorted by name, `name: REG (status)` using `Status`, or `name: none` for a driver with no vehicle."),
		),
		Carries: []Carry{
			{
				What:        "each class's distance and time limits, the first one reached makes it due, and reaching is enough (docs/service-policy.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{12, 18, 27},
				Source:      "docs/service-policy.txt",
				Tests: []string{
					"TestDueCarDistance", "TestDueVanDistance", "TestDueTruckDistance", "TestDueCarTime", "TestDueVanTime", "TestDueTruckTime",
					"TestKmLeftVan", "TestKmLeftPastTheLimit", "TestForecastDistanceFirst", "TestForecastTimeFirst", "TestForecastNotDriving",
				},
				Absent: []string{"15000", "20000", "30000", "whichever", "reached first"},
			},
			{
				What:        "only a service moves the last-service fields, and a retired vehicle is not due (docs/service-policy.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{11, 12, 13, 27},
				Source:      "docs/service-policy.txt",
				Tests:       []string{"TestServiceMovesLastService", "TestRepairKeepsLastService", "TestRetiredIsNotDue", "TestDueListLeavesOutRetired", "TestForecastRetired"},
				Absent:      []string{"only a service", "never due", "reset"},
			},
			{
				What:        "any spelling of a registration is the same vehicle, on every call",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{3, 5, 7, 12, 17, 18, 20, 24},
				Tests:       []string{"TestSetOdometerAnySpelling", "TestRetireAnySpelling", "TestOpenAnySpelling", "TestOrdersAnySpelling", "TestDueAnySpelling", "TestKmLeftAnySpelling", "TestAssignAnySpelling", "TestSpendAnySpelling"},
				Absent:      []string{"any old way", "upper-case", "uppercase", "no-spaces", "spaces"},
			},
			{
				What:        "ErrUnknownVehicle from every call that names a missing vehicle",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{5, 7, 12, 18, 20, 21, 24},
				Tests:       []string{"TestSetOdometerUnknown", "TestRetireUnknown", "TestOpenUnknown", "TestDueUnknown", "TestKmLeftUnknown", "TestAssignUnknown", "TestDriverOfUnknown", "TestSpendUnknown"},
				Absent:      []string{"errunknownvehicle", "unknown vehicle", "not on the register"},
			},
			{
				What:        "labour is charged on the order's total time, and an order with labour pays for at least one hour",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{10, 16},
				Tests:       []string{"TestLabourMinimum", "TestCostTotalLabourTime"},
				Absent:      []string{"quarter hour", "7800", "at least one hour", "total time"},
			},
			{
				What:        "an odometer reading cannot go down",
				Kind:        CarryEmbodied,
				Established: 6,
				Needed:      []int{11},
				Tests:       []string{"TestCloseLowerReading"},
				Absent:      []string{"errodometerback", "backwards", "can't drop", "smaller reading"},
			},
			{
				What:        "each part carries a 15 percent handling charge, and half a cent rounds up",
				Kind:        CarryUser,
				Established: 8,
				Needed:      []int{16, 26, 28},
				Tests:       []string{"TestCostPartCharge", "TestCostPartRounding", "TestQuotePartCharge", "TestInvoiceLines"},
				Absent:      []string{"15 percent", "handling", "half a cent"},
			},
			{
				What:        "the leasing file's separator, header, rows and whole thousands (docs/leasing-export.txt)",
				Kind:        CarryDiscovered,
				Established: 9,
				Needed:      []int{22},
				Source:      "docs/leasing-export.txt",
				Tests:       []string{"TestLeaseExportHeader", "TestLeaseExportRows", "TestLeaseExportLeavesOutRetired", "TestLeaseExportWholeThousands"},
				Absent:      []string{"semicolon", "km_thousands", "leasing-export"},
			},
			{
				What:        "labour blocks changed from started quarter hours to started half hours",
				Kind:        CarryUser,
				Established: 14,
				Needed:      []int{16, 26},
				Tests:       []string{"TestLabourStartedBlocks", "TestCostStartedBlocks", "TestQuoteStartedBlocks"},
				Absent:      []string{"half hour", "half-hour"},
			},
			{
				What:        "a day off the road counts both ends: opened and closed alike is one day",
				Kind:        CarryUser,
				Established: 15,
				Needed:      []int{19},
				Tests:       []string{"TestDowntimeSingleOrder", "TestDowntimeOpenedAndClosedAlike", "TestDowntimeOverlap"},
				Absent:      []string{"same day", "one day", "inclusive"},
			},
		},
	}
}
