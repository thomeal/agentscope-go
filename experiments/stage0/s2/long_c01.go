package s2

func longC01() Task {
	return Task{
		ID:     "s2-long-c01-wal-store",
		Set:    "long",
		System: system("walstore, an embedded key-value store that keeps a write-ahead log on a simulated disk"),
		Files: map[string]string{
			"go.mod": goMod,
			"disk.go": `package walstore

import "sort"

// Disk is an in-memory stand-in for a directory of line-oriented files.
type Disk struct {
	files map[string][]string
}

// NewDisk returns an empty disk.
func NewDisk() *Disk { return &Disk{files: map[string][]string{}} }

// Append adds one line to the named file, creating the file if needed.
func (d *Disk) Append(name, line string) {
	d.files[name] = append(d.files[name], line)
}

// Lines returns a copy of the lines in the named file.
func (d *Disk) Lines(name string) []string {
	return append([]string(nil), d.files[name]...)
}

// Replace sets the whole contents of the named file.
func (d *Disk) Replace(name string, lines []string) {
	d.files[name] = append([]string(nil), lines...)
}

// Remove deletes the named file.
func (d *Disk) Remove(name string) { delete(d.files, name) }

// List returns the names of all files in sorted order.
func (d *Disk) List() []string {
	names := make([]string, 0, len(d.files))
	for name := range d.files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
`,
			"docs/format.md": `# walstore log format

The log is a series of segment files. A record is one line of text with six fields
separated by "|":

    seq|op|key|value|at|checksum

seq and at are decimal integers. op is "put" or "del". A del record has an empty value.

In key and value, "%" is written as %25, "|" as %7C and a newline as %0A, so a field never
contains a separator and a record never spans two lines. Nothing else is escaped.

checksum covers everything before the last "|": add up the bytes, modulo 65521, and write
the result as exactly four lower-case hexadecimal digits.

For example, record 1 putting the value "b" under the key "a" at clock reading 7 is

    1|put|a|b|7|0474

## Damage

A crash can leave a half-written line at the end of the newest segment. A line there that
does not decode is a torn write, and that is not an error: recovery keeps what came before
it, throws away that line and anything after it, in the file as well, and carries on.

Segments older than the newest were complete when the log moved on from them, so a line
in one of them that does not decode cannot be explained by a crash. Recovery must then fail
with an error wrapping ErrCorrupt and must not return a store.
`,
			"docs/operations.md": `# Running walstore

Notes from the people on call for it. Not everything here concerns the code.

## Disk use

A busy store writes a few thousand records a day. Segments pile up until compaction runs,
so disk use depends far more on how often compaction runs than on how much data is live.
We run it from cron and do not expect the store to schedule anything by itself.

## Compaction

The newest segment is still being written to, so compaction must leave it exactly as it
is, byte for byte. Every other segment is sealed and fair game.

Compaction is only worth running when there are at least two sealed segments to merge. With
fewer than that it must do nothing at all and report that it dropped 0 records.

## Snapshots

We take snapshots before risky maintenance and prune the old ones from cron. A store whose
snapshots have all gone has lost its fast way back, so pruning must never remove the newest
snapshot, even when it is asked to keep none.

## Backups

Backups copy the segment files while the store is running. That is safe because a sealed
segment only changes during compaction, and we never run the two at once. Restoring means
putting the files back and starting the store as usual.

## Monitoring

We graph the number of segments and the number of records on disk. An alert goes off above
500 segments, which in practice has always meant that cron stopped running compaction.
`,
			"metrics.go": `package walstore

import (
	"fmt"
	"sort"
	"strings"
)

// Metrics is a set of named counters for the monitoring page.
type Metrics struct {
	counters map[string]int64
}

// NewMetrics returns a set with no counters in it.
func NewMetrics() *Metrics { return &Metrics{counters: map[string]int64{}} }

// Inc adds n to the named counter, creating it if needed.
func (m *Metrics) Inc(name string, n int64) { m.counters[name] += n }

// Get returns the named counter, which is 0 if nothing was ever added to it.
func (m *Metrics) Get(name string) int64 { return m.counters[name] }

// Render writes the counters one per line, sorted by name, the way the scraper reads them.
func (m *Metrics) Render() string {
	names := make([]string, 0, len(m.counters))
	for name := range m.counters {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s %d\n", name, m.counters[name])
	}
	return b.String()
}
`,
			"record.go": `package walstore

import "errors"

// Op says what a record does to its key.
type Op string

const (
	OpPut Op = "put"
	OpDel Op = "del"
)

// ErrCorrupt reports damage to the log that recovery cannot explain away.
var ErrCorrupt = errors.New("corrupt log")

// Record is one entry in the log.
type Record struct {
	Seq        int64
	Op         Op
	Key, Value string
	At         int64 // clock reading when it was written
}
`,
			"store.go": `package walstore

// Clock reads the current time.
type Clock func() int64

// Store is a key-value store. So far it only keeps its data in memory.
type Store struct {
	disk  *Disk
	clock Clock
	data  map[string]string
}

// Open returns an empty store on the disk.
func Open(disk *Disk, clock Clock) *Store {
	return &Store{disk: disk, clock: clock, data: map[string]string{}}
}

// Get returns the value of key and whether it has one.
func (s *Store) Get(key string) (string, bool) {
	v, ok := s.data[key]
	return v, ok
}

// Put sets the value of key.
func (s *Store) Put(key, value string) error {
	s.data[key] = value
	return nil
}
`,
			"store_test.go": `package walstore

import (
	"strings"
	"testing"
)

func TestPutThenGet(t *testing.T) {
	s := Open(NewDisk(), func() int64 { return 100 })
	if err := s.Put("region", "emea"); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Get("region"); !ok || v != "emea" {
		t.Fatalf("Get = %q, %v", v, ok)
	}
	if _, ok := s.Get("missing"); ok {
		t.Fatal("Get found a key that was never put")
	}
}

func TestDiskKeepsLines(t *testing.T) {
	d := NewDisk()
	d.Append("b.log", "one")
	d.Append("b.log", "two")
	d.Append("a.log", "x")
	if got := d.List(); len(got) != 2 || got[0] != "a.log" {
		t.Fatalf("List = %v", got)
	}
	if got := d.Lines("b.log"); len(got) != 2 || got[1] != "two" {
		t.Fatalf("Lines = %v", got)
	}
	d.Replace("b.log", []string{"three"})
	if got := d.Lines("b.log"); len(got) != 1 || got[0] != "three" {
		t.Fatalf("after Replace, Lines = %v", got)
	}
	d.Remove("a.log")
	if got := d.List(); len(got) != 1 {
		t.Fatalf("after Remove, List = %v", got)
	}
}

func TestMetricsRender(t *testing.T) {
	m := NewMetrics()
	m.Inc("b", 2)
	m.Inc("a", 1)
	m.Inc("b", 3)
	if m.Get("b") != 5 || m.Get("none") != 0 {
		t.Fatalf("Get: %d, %d", m.Get("b"), m.Get("none"))
	}
	if got := strings.Split(m.Render(), "\n"); got[0] != "a 1" || got[1] != "b 5" {
		t.Fatalf("Render = %q", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package walstore_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	walstore "fixture"
)

const (
	second = int64(1000)
	minute = 60 * second
	hour   = 60 * minute
)

type rig struct {
	disk  *walstore.Disk
	store *walstore.Store
	now   int64
}

func (r *rig) clock() int64 { return r.now }

func newRig() *rig {
	r := &rig{disk: walstore.NewDisk(), now: 500 * hour}
	r.store = walstore.Open(r.disk, r.clock)
	return r
}

func (r *rig) put(t *testing.T, pairs ...string) {
	t.Helper()
	for i := 0; i < len(pairs); i += 2 {
		if err := r.store.Put(pairs[i], pairs[i+1]); err != nil {
			t.Fatal(err)
		}
	}
}

func (r *rig) del(t *testing.T, key string) {
	t.Helper()
	if err := r.store.Delete(key); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) lines(name string) []string { return r.disk.Lines(name) }

// fill writes unrelated keys until n more records are on disk.
func (r *rig) fill(t *testing.T, n int) {
	t.Helper()
	for i := range n {
		r.put(t, fmt.Sprintf("fill%03d", i), "x")
	}
}

// withInternal has a store whose log holds a bookkeeping key next to ordinary ones.
func withInternal(t *testing.T) *rig {
	t.Helper()
	r := &rig{disk: walstore.NewDisk(), now: 500 * hour}
	for i, kv := range [][2]string{{"a", "1"}, {"sys/owner", "x"}, {"b", "2"}} {
		line := walstore.EncodeRecord(walstore.Record{Seq: int64(i + 1), Op: walstore.OpPut, Key: kv[0], Value: kv[1], At: r.now})
		r.disk.Append("wal-000001.log", line)
	}
	s, err := walstore.Recover(r.disk, r.clock)
	if err != nil {
		t.Fatal(err)
	}
	r.store = s
	return r
}

func closed(t *testing.T) *walstore.Store {
	t.Helper()
	r := newRig()
	r.fill(t, 9)
	if err := r.store.Close(); err != nil {
		t.Fatal(err)
	}
	return r.store
}

func TestEncodeRecordFollowsFormat(t *testing.T) {
	got := walstore.EncodeRecord(walstore.Record{Seq: 1, Op: walstore.OpPut, Key: "a", Value: "b", At: 7})
	if got != "1|put|a|b|7|0474" {
		t.Errorf("plain record: %q", got)
	}
	got = walstore.EncodeRecord(walstore.Record{Seq: 2, Op: walstore.OpPut, Key: "x|y", Value: "5%\nz", At: 9})
	if !strings.HasPrefix(got, "2|put|x%7Cy|5%25%0Az|9|") || strings.Contains(got, "\n") {
		t.Errorf("escapes: %q", got)
	}
}

func TestDecodeRecordRoundTrip(t *testing.T) {
	want := walstore.Record{Seq: 12, Op: walstore.OpDel, Key: "x|%\ny", Value: "", At: 4000}
	got, err := walstore.DecodeRecord(walstore.EncodeRecord(want))
	if err != nil || got != want {
		t.Fatalf("DecodeRecord = %+v, %v", got, err)
	}
}

func TestDecodeRecordRefusesBadLines(t *testing.T) {
	good := walstore.EncodeRecord(walstore.Record{Seq: 3, Op: walstore.OpPut, Key: "k", Value: "v", At: 5})
	for _, line := range []string{"", "nonsense", good[:len(good)-2], strings.Replace(good, "k", "j", 1)} {
		if _, err := walstore.DecodeRecord(line); !errors.Is(err, walstore.ErrBadRecord) {
			t.Errorf("DecodeRecord(%q): %v", line, err)
		}
	}
}

func TestPutRefusesReservedKey(t *testing.T) {
	r := newRig()
	if err := r.store.Put("sys/owner", "me"); !errors.Is(err, walstore.ErrReserved) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := r.store.Get("sys/owner"); ok || len(r.disk.List()) != 0 {
		t.Fatal("the refused Put left something behind")
	}
}

func TestDeleteRefusesInternalKey(t *testing.T) {
	r := withInternal(t)
	if err := r.store.Delete("sys/owner"); !errors.Is(err, walstore.ErrReserved) {
		t.Fatalf("err = %v", err)
	}
}

func TestPutWritesARecordFirst(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1", "b", "2")
	lines := r.lines("wal-000001.log")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	got, err := walstore.DecodeRecord(lines[1])
	want := walstore.Record{Seq: 2, Op: walstore.OpPut, Key: "b", Value: "2", At: r.now}
	if err != nil || got != want {
		t.Fatalf("record = %+v, %v", got, err)
	}
}

func TestDeleteWritesADelRecord(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1")
	r.del(t, "a")
	r.del(t, "never there")
	lines := r.lines("wal-000001.log")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	got, err := walstore.DecodeRecord(lines[1])
	if err != nil || got.Op != walstore.OpDel || got.Seq != 2 || got.Key != "a" {
		t.Fatalf("record = %+v, %v", got, err)
	}
	if _, ok := r.store.Get("a"); ok {
		t.Fatal("the key is still there")
	}
}

func TestLogRollsAfterFourRecords(t *testing.T) {
	r := newRig()
	r.fill(t, 9)
	want := []string{"wal-000001.log", "wal-000002.log", "wal-000003.log"}
	if got := r.disk.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %v", got)
	}
	if n := len(r.lines("wal-000002.log")); n != 4 {
		t.Fatalf("%d lines in the second segment", n)
	}
}

func TestPutRefusesLargeValue(t *testing.T) {
	r := newRig()
	if err := r.store.Put("big", strings.Repeat("v", 1025)); !errors.Is(err, walstore.ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if len(r.disk.List()) != 0 {
		t.Fatal("the refused Put wrote something")
	}
	r.put(t, "fits", strings.Repeat("v", 1024))
}

func TestScanFiltersAndSorts(t *testing.T) {
	r := newRig()
	r.put(t, "b/2", "x", "a/1", "y", "b/1", "z")
	got, err := r.store.Scan("b/")
	want := []walstore.Entry{{Key: "b/1", Value: "z"}, {Key: "b/2", Value: "x"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan = %+v, %v", got, err)
	}
}

func TestScanHidesInternalKeys(t *testing.T) {
	r := withInternal(t)
	got, err := r.store.Scan("")
	want := []walstore.Entry{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan = %+v, %v", got, err)
	}
}

func TestCloseStopsTheStore(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1")
	if err := r.store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.store.Put("b", "2"); !errors.Is(err, walstore.ErrClosed) {
		t.Errorf("Put: %v", err)
	}
	if err := r.store.Delete("a"); !errors.Is(err, walstore.ErrClosed) {
		t.Errorf("Delete: %v", err)
	}
	if _, err := r.store.Scan(""); !errors.Is(err, walstore.ErrClosed) {
		t.Errorf("Scan: %v", err)
	}
	if v, ok := r.store.Get("a"); !ok || v != "1" {
		t.Errorf("Get: %q, %v", v, ok)
	}
}

func TestRecoverRestoresTheStore(t *testing.T) {
	r := newRig()
	r.fill(t, 5)
	r.put(t, "a", "1")
	r.del(t, "fill000")
	s, err := walstore.Recover(r.disk, r.clock)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Get("a"); !ok || v != "1" {
		t.Errorf("Get(a) = %q, %v", v, ok)
	}
	if _, ok := s.Get("fill000"); ok {
		t.Error("a deleted key came back")
	}
	if err := s.Put("after", "2"); err != nil {
		t.Fatal(err)
	}
	lines := r.lines("wal-000002.log")
	got, err := walstore.DecodeRecord(lines[len(lines)-1])
	if err != nil || got.Seq != 8 || got.Key != "after" || len(lines) != 4 {
		t.Errorf("the next record is %+v, %v, among %d lines", got, err, len(lines))
	}
}

func TestRecoverDropsTornTail(t *testing.T) {
	r := newRig()
	r.fill(t, 6)
	lines := r.lines("wal-000002.log")
	torn := append(lines[:1:1], lines[1][:5], "more after it")
	r.disk.Replace("wal-000002.log", torn)
	s, err := walstore.Recover(r.disk, r.clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("fill004"); !ok {
		t.Error("the record before the torn line is gone")
	}
	if _, ok := s.Get("fill005"); ok {
		t.Error("the torn record was applied")
	}
	if n := len(r.lines("wal-000002.log")); n != 1 {
		t.Errorf("%d lines left in the file", n)
	}
	if err := s.Put("next", "v"); err != nil {
		t.Fatal(err)
	}
	if _, err := walstore.Recover(r.disk, r.clock); err != nil {
		t.Errorf("recovering a second time: %v", err)
	}
}

func TestRecoverFailsOnOlderDamage(t *testing.T) {
	r := newRig()
	r.fill(t, 6)
	lines := r.lines("wal-000001.log")
	lines[3] = lines[3][:6]
	r.disk.Replace("wal-000001.log", lines)
	s, err := walstore.Recover(r.disk, r.clock)
	if !errors.Is(err, walstore.ErrCorrupt) || s != nil {
		t.Fatalf("Recover = %v, %v", s, err)
	}
}

func TestApplyWritesEveryOp(t *testing.T) {
	r := newRig()
	r.put(t, "gone", "x")
	err := r.store.Apply([]walstore.BatchOp{{Key: "a", Value: "1"}, {Key: "gone", Del: true}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := r.store.Get("a"); !ok || v != "1" {
		t.Errorf("Get(a) = %q, %v", v, ok)
	}
	if _, ok := r.store.Get("gone"); ok {
		t.Error("the deleted key is still there")
	}
	if n := len(r.lines("wal-000001.log")); n != 3 {
		t.Errorf("%d lines", n)
	}
}

func TestApplyRefusesInternalKey(t *testing.T) {
	r := newRig()
	err := r.store.Apply([]walstore.BatchOp{{Key: "a", Value: "1"}, {Key: "sys/x", Value: "2"}})
	if !errors.Is(err, walstore.ErrReserved) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := r.store.Get("a"); ok || len(r.disk.List()) != 0 {
		t.Fatal("the refused batch left something behind")
	}
}

func TestApplyRefusesLargeValue(t *testing.T) {
	r := newRig()
	big := strings.Repeat("v", 1025)
	err := r.store.Apply([]walstore.BatchOp{{Key: "a", Value: "1"}, {Key: "b", Value: big}})
	if !errors.Is(err, walstore.ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := r.store.Get("a"); ok || len(r.disk.List()) != 0 {
		t.Fatal("the refused batch left something behind")
	}
}

func TestApplyAfterClose(t *testing.T) {
	err := closed(t).Apply([]walstore.BatchOp{{Key: "a", Value: "1"}})
	if !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestBatchStaysInOneSegment(t *testing.T) {
	r := newRig()
	r.fill(t, 3)
	err := r.store.Apply([]walstore.BatchOp{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(r.lines("wal-000001.log")); n != 3 {
		t.Errorf("%d lines in the first segment", n)
	}
	if n := len(r.lines("wal-000002.log")); n != 2 {
		t.Errorf("%d lines in the second segment", n)
	}
}

func TestLargeBatchGetsItsOwnSegment(t *testing.T) {
	r := newRig()
	r.fill(t, 1)
	var ops []walstore.BatchOp
	for i := range 6 {
		ops = append(ops, walstore.BatchOp{Key: fmt.Sprint("k", i), Value: "v"})
	}
	if err := r.store.Apply(ops); err != nil {
		t.Fatal(err)
	}
	if n := len(r.lines("wal-000002.log")); n != 6 {
		t.Errorf("%d lines in the batch's segment", n)
	}
	r.put(t, "after", "v")
	if n := len(r.lines("wal-000003.log")); n != 1 {
		t.Errorf("the next write left %d lines in a new segment", n)
	}
}

func TestVerifyCounts(t *testing.T) {
	r := newRig()
	r.fill(t, 6)
	lines := r.lines("wal-000001.log")
	lines[0] = "garbage"
	r.disk.Replace("wal-000001.log", lines)
	got, err := r.store.Verify()
	want := walstore.Report{Segments: 2, Records: 5, Bad: 1}
	if err != nil || got != want {
		t.Fatalf("Verify = %+v, %v", got, err)
	}
}

func TestVerifyAfterClose(t *testing.T) {
	if _, err := closed(t).Verify(); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompactKeepsNewestOfEachKey(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1", "a", "2", "b", "1", "a", "3")
	r.put(t, "c", "1", "d", "1", "b", "2", "e", "1")
	r.put(t, "z", "1")
	dropped, err := r.store.Compact()
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 3 {
		t.Errorf("dropped = %d", dropped)
	}
	if !strings.HasPrefix(r.disk.List()[0], "wal-000001") {
		t.Errorf("files: %v", r.disk.List())
	}
	s, err := walstore.Recover(r.disk, r.clock)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get("a"); v != "3" {
		t.Errorf("a = %q", v)
	}
	if v, _ := s.Get("b"); v != "2" {
		t.Errorf("b = %q", v)
	}
}

func TestCompactLeavesNewestSegmentAlone(t *testing.T) {
	r := newRig()
	r.fill(t, 9)
	newest := r.lines("wal-000003.log")
	if _, err := r.store.Compact(); err != nil {
		t.Fatal(err)
	}
	if got := r.lines("wal-000003.log"); !reflect.DeepEqual(got, newest) {
		t.Fatalf("the newest segment became %q", got)
	}
	want := []string{"wal-000001.log", "wal-000003.log"}
	if got := r.disk.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %v", got)
	}
}

func TestCompactNeedsTwoSealedSegments(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1", "a", "2", "a", "3", "a", "4", "b", "1")
	before := r.lines("wal-000001.log")
	dropped, err := r.store.Compact()
	if err != nil || dropped != 0 {
		t.Fatalf("Compact = %d, %v", dropped, err)
	}
	if got := r.lines("wal-000001.log"); !reflect.DeepEqual(got, before) {
		t.Fatalf("the segment became %q", got)
	}
}

func TestCompactAfterClose(t *testing.T) {
	if _, err := closed(t).Compact(); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

// tombstone deletes a key, moves the clock on by age and rolls the log past the
// segment the del record is in.
func tombstone(t *testing.T, age int64) *rig {
	t.Helper()
	r := newRig()
	r.put(t, "gone", "x")
	r.del(t, "gone")
	r.now += age
	r.fill(t, 7)
	return r
}

func hasDel(r *rig) bool {
	for _, name := range r.disk.List() {
		for _, line := range r.lines(name) {
			if got, err := walstore.DecodeRecord(line); err == nil && got.Op == walstore.OpDel {
				return true
			}
		}
	}
	return false
}

func TestCompactDropsOldTombstone(t *testing.T) {
	r := tombstone(t, 7*hour)
	if _, err := r.store.Compact(); err != nil {
		t.Fatal(err)
	}
	if hasDel(r) {
		t.Fatal("a del record seven hours old is still there")
	}
}

func TestCompactKeepsYoungTombstone(t *testing.T) {
	r := tombstone(t, 30*minute)
	if _, err := r.store.Compact(); err != nil {
		t.Fatal(err)
	}
	if !hasDel(r) {
		t.Fatal("a del record half an hour old was dropped")
	}
}

func TestCompactKeepsTombstoneOfTwoHours(t *testing.T) {
	r := tombstone(t, 2*hour)
	if _, err := r.store.Compact(); err != nil {
		t.Fatal(err)
	}
	if !hasDel(r) {
		t.Fatal("a del record two hours old was dropped")
	}
}

func TestStatsCounts(t *testing.T) {
	r := newRig()
	r.fill(t, 5)
	r.del(t, "fill000")
	got, err := r.store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if got.Keys != 4 || got.Segments != 2 || got.Records != 6 {
		t.Fatalf("Stats = %+v", got)
	}
}

func TestStatsKeysHideInternal(t *testing.T) {
	got, err := withInternal(t).store.Stats()
	if err != nil || got.Keys != 2 {
		t.Fatalf("Stats = %+v, %v", got, err)
	}
}

func TestStatsAfterClose(t *testing.T) {
	if _, err := closed(t).Stats(); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestStatsOldestAgeInSeconds(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1")
	r.now += 90*second + 500
	r.put(t, "b", "2")
	got, err := r.store.Stats()
	if err != nil || got.OldestAge != 90 {
		t.Fatalf("Stats = %+v, %v", got, err)
	}
}

func TestStatsPendingTombstones(t *testing.T) {
	r := newRig()
	r.put(t, "old", "x")
	r.del(t, "old")
	r.now += 5 * hour
	r.put(t, "young", "x")
	r.del(t, "young")
	r.now += 2 * hour
	got, err := r.store.Stats()
	if err != nil || got.PendingTombstones != 1 {
		t.Fatalf("Stats = %+v, %v", got, err)
	}
}

func TestSinceReturnsNewerRecords(t *testing.T) {
	r := newRig()
	r.fill(t, 6)
	got, err := r.store.Since(4)
	if err != nil || len(got) != 2 || got[0].Seq != 5 || got[1].Seq != 6 {
		t.Fatalf("Since = %+v, %v", got, err)
	}
}

func TestSinceHidesInternal(t *testing.T) {
	got, err := withInternal(t).store.Since(0)
	if err != nil || len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
		t.Fatalf("Since = %+v, %v", got, err)
	}
}

func TestSinceAfterClose(t *testing.T) {
	if _, err := closed(t).Since(0); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestExportFormat(t *testing.T) {
	r := newRig()
	r.put(t, "b", "2", "a", "1")
	got, err := r.store.Export()
	if err != nil || got != "a\t1\nb\t2\n# 2 keys\n" {
		t.Fatalf("Export = %q, %v", got, err)
	}
}

func TestExportHidesInternal(t *testing.T) {
	got, err := withInternal(t).store.Export()
	if err != nil || strings.Contains(got, "sys/") || !strings.Contains(got, "# 2 keys") {
		t.Fatalf("Export = %q, %v", got, err)
	}
}

func TestExportEscapesTabs(t *testing.T) {
	r := newRig()
	r.put(t, "a", "x\ty")
	got, err := r.store.Export()
	if err != nil || !strings.HasPrefix(got, "a\tx\\ty\n") {
		t.Fatalf("Export = %q, %v", got, err)
	}
}

func TestExportAfterClose(t *testing.T) {
	if _, err := closed(t).Export(); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestHistoryOldestFirst(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1", "b", "1", "a", "2")
	r.del(t, "a")
	got, err := r.store.History("a")
	if err != nil || len(got) != 3 || got[0].Value != "1" || got[2].Op != walstore.OpDel {
		t.Fatalf("History = %+v, %v", got, err)
	}
}

func TestHistoryAfterClose(t *testing.T) {
	if _, err := closed(t).History("a"); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestRecordsWrittenAreCountedOneByOne(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1")
	r.del(t, "a")
	err := r.store.Apply([]walstore.BatchOp{{Key: "b", Value: "1"}, {Key: "c", Value: "1"}, {Key: "d", Value: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if n := r.store.Metrics().Get("walstore_records_written"); n != 5 {
		t.Fatalf("counted %d", n)
	}
}

func TestCompactCountsDroppedRecords(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1", "a", "2", "a", "3", "b", "1")
	r.fill(t, 5)
	dropped, err := r.store.Compact()
	if err != nil {
		t.Fatal(err)
	}
	if n := r.store.Metrics().Get("walstore_records_dropped"); n != int64(dropped) || n != 2 {
		t.Fatalf("counted %d, dropped %d", n, dropped)
	}
}

func TestSnapshotThenRecover(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1", "b", "2")
	name, err := r.store.Snapshot()
	if err != nil || name != "snap-000002.dat" {
		t.Fatalf("Snapshot = %q, %v", name, err)
	}
	r.put(t, "c", "3")
	r.del(t, "a")
	s, err := walstore.Recover(r.disk, r.clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("a"); ok {
		t.Error("a key deleted after the snapshot came back")
	}
	if v, _ := s.Get("b"); v != "2" {
		t.Errorf("b = %q", v)
	}
	if v, _ := s.Get("c"); v != "3" {
		t.Errorf("c = %q", v)
	}
}

func TestRecoverStartsFromSnapshot(t *testing.T) {
	r := newRig()
	r.fill(t, 5)
	if _, err := r.store.Snapshot(); err != nil {
		t.Fatal(err)
	}
	r.disk.Remove("wal-000001.log")
	r.disk.Remove("wal-000002.log")
	s, err := walstore.Recover(r.disk, r.clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("fill002"); !ok {
		t.Fatal("the key held only by the snapshot is gone")
	}
	if err := s.Put("next", "v"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Since(5)
	if err != nil || len(got) != 1 || got[0].Seq != 6 {
		t.Fatalf("the sequence number went back: %+v, %v", got, err)
	}
}

func TestSnapshotHoldsEveryKey(t *testing.T) {
	r := withInternal(t)
	name, err := r.store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(r.disk.Lines(name)); n != 3 {
		t.Fatalf("%d lines", n)
	}
	s, err := walstore.Recover(r.disk, r.clock)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Get("sys/owner"); !ok || v != "x" {
		t.Fatalf("Get = %q, %v", v, ok)
	}
}

func TestSnapshotIsCounted(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1")
	if _, err := r.store.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if n := r.store.Metrics().Get("walstore_snapshots_taken"); n != 1 {
		t.Fatalf("counted %d", n)
	}
}

func TestSnapshotAfterClose(t *testing.T) {
	if _, err := closed(t).Snapshot(); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

// threeSnapshots has a store with snapshots at sequence numbers 1, 2 and 3.
func threeSnapshots(t *testing.T) *rig {
	t.Helper()
	r := newRig()
	for i := range 3 {
		r.put(t, fmt.Sprint("k", i), "v")
		if _, err := r.store.Snapshot(); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestSnapshotsNewestFirst(t *testing.T) {
	got, err := threeSnapshots(t).store.Snapshots()
	want := []string{"snap-000003.dat", "snap-000002.dat", "snap-000001.dat"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshots = %v, %v", got, err)
	}
}

func TestSnapshotsAfterClose(t *testing.T) {
	if _, err := closed(t).Snapshots(); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestPruneSnapshotsKeepsNewest(t *testing.T) {
	r := threeSnapshots(t)
	n, err := r.store.PruneSnapshots(2)
	if err != nil || n != 1 {
		t.Fatalf("PruneSnapshots = %d, %v", n, err)
	}
	got, err := r.store.Snapshots()
	want := []string{"snap-000003.dat", "snap-000002.dat"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshots = %v, %v", got, err)
	}
}

func TestPruneKeepsNewestSnapshot(t *testing.T) {
	r := threeSnapshots(t)
	n, err := r.store.PruneSnapshots(0)
	if err != nil || n != 2 {
		t.Fatalf("PruneSnapshots = %d, %v", n, err)
	}
	got, err := r.store.Snapshots()
	if err != nil || !reflect.DeepEqual(got, []string{"snap-000003.dat"}) {
		t.Fatalf("Snapshots = %v, %v", got, err)
	}
}

func TestPruneSnapshotsIsCounted(t *testing.T) {
	r := threeSnapshots(t)
	if _, err := r.store.PruneSnapshots(1); err != nil {
		t.Fatal(err)
	}
	if n := r.store.Metrics().Get("walstore_snapshots_pruned"); n != 2 {
		t.Fatalf("counted %d", n)
	}
}

func TestPruneSnapshotsAfterClose(t *testing.T) {
	if _, err := closed(t).PruneSnapshots(1); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompactPlanChangesNothing(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1", "a", "2", "a", "3", "b", "1")
	r.fill(t, 5)
	before := r.lines("wal-000001.log")
	plan, err := r.store.CompactPlan()
	if err != nil {
		t.Fatal(err)
	}
	if got := r.lines("wal-000001.log"); !reflect.DeepEqual(got, before) || len(r.disk.List()) != 3 {
		t.Fatal("the plan changed the files")
	}
	dropped, err := r.store.Compact()
	if err != nil || plan.Drop != dropped || plan.Drop != 2 {
		t.Fatalf("plan %+v, Compact = %d, %v", plan, dropped, err)
	}
}

func TestCompactPlanNamesSealedSegments(t *testing.T) {
	r := newRig()
	r.fill(t, 9)
	plan, err := r.store.CompactPlan()
	want := []string{"wal-000001.log", "wal-000002.log"}
	if err != nil || !reflect.DeepEqual(plan.Merge, want) {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	few := newRig()
	few.fill(t, 5)
	plan, err = few.store.CompactPlan()
	if err != nil || len(plan.Merge) != 0 || plan.Drop != 0 {
		t.Fatalf("with one sealed segment: plan %+v, %v", plan, err)
	}
}

func TestCompactPlanAfterClose(t *testing.T) {
	if _, err := closed(t).CompactPlan(); !errors.Is(err, walstore.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestImportRoundTrip(t *testing.T) {
	r := newRig()
	r.put(t, "a", "1", "b", "x\ty")
	text, err := r.store.Export()
	if err != nil {
		t.Fatal(err)
	}
	other := newRig()
	n, err := other.store.Import(text)
	if err != nil || n != 2 {
		t.Fatalf("Import = %d, %v", n, err)
	}
	if v, _ := other.store.Get("b"); v != "x\ty" {
		t.Fatalf("b = %q", v)
	}
}

func TestImportRefusesBadLine(t *testing.T) {
	r := newRig()
	n, err := r.store.Import("a\t1\nno tab here\n# 2 keys\n")
	if !errors.Is(err, walstore.ErrBadRecord) || n != 0 {
		t.Fatalf("Import = %d, %v", n, err)
	}
	if _, ok := r.store.Get("a"); ok {
		t.Fatal("a failed import changed the data")
	}
}

func TestImportRefusesInternalKey(t *testing.T) {
	r := newRig()
	if _, err := r.store.Import("sys/x\t1\n# 1 keys\n"); !errors.Is(err, walstore.ErrReserved) {
		t.Fatalf("err = %v", err)
	}
}

func TestImportRefusesLargeValue(t *testing.T) {
	r := newRig()
	text := "a\t" + strings.Repeat("v", 1025) + "\n# 1 keys\n"
	if _, err := r.store.Import(text); !errors.Is(err, walstore.ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}
`},
		Solution: map[string]string{
			"compact.go": `package walstore

import "fmt"

const msPerSecond = 1000

// tombstoneGrace is how long a del record has to stay in the log for replicas.
const tombstoneGrace = 6 * 3600 * msPerSecond

// latestSeqs returns the sequence number of the newest record of each key on disk.
func (s *Store) latestSeqs() map[string]int64 {
	latest := map[string]int64{}
	s.log.each(func(_ string, r Record, err error) {
		if err == nil && r.Seq > latest[r.Key] {
			latest[r.Key] = r.Seq
		}
	})
	return latest
}

// sift decodes the named segments and returns the lines worth keeping and how many
// records it dropped.
func (s *Store) sift(names []string, latest map[string]int64) ([]string, int, error) {
	now := s.clock()
	var kept []string
	dropped := 0
	for _, name := range names {
		for _, line := range s.log.disk.Lines(name) {
			r, err := DecodeRecord(line)
			if err != nil {
				return nil, 0, fmt.Errorf("%s: %w", name, ErrCorrupt)
			}
			if r.Seq != latest[r.Key] {
				dropped++
			} else if r.Op == OpDel && now-r.At >= tombstoneGrace {
				dropped++
			} else {
				kept = append(kept, line)
			}
		}
	}
	return kept, dropped, nil
}

// mergeable returns the sealed segments, or nothing when there are too few of them to
// be worth merging. The newest segment is still being written to and is never among them.
func (s *Store) mergeable() []string {
	names := s.log.segments()
	if len(names) < 3 {
		return nil
	}
	return names[:len(names)-1]
}

// Compact merges the sealed segments into one under the oldest one's name, keeping
// only the newest record of each key, and returns how many records it dropped.
func (s *Store) Compact() (int, error) {
	if s.closed {
		return 0, ErrClosed
	}
	sealed := s.mergeable()
	if len(sealed) == 0 {
		return 0, nil
	}
	kept, dropped, err := s.sift(sealed, s.latestSeqs())
	if err != nil {
		return 0, err
	}
	for _, name := range sealed[1:] {
		s.log.disk.Remove(name)
	}
	s.log.disk.Replace(sealed[0], kept)
	s.metrics.Inc("walstore_records_dropped", int64(dropped))
	return dropped, nil
}

// Plan is what Compact would do right now.
type Plan struct {
	Merge []string // the segments it would merge, oldest first
	Drop  int      // the records it would drop
}

// CompactPlan works out what Compact would do without changing anything.
func (s *Store) CompactPlan() (Plan, error) {
	if s.closed {
		return Plan{}, ErrClosed
	}
	sealed := s.mergeable()
	if len(sealed) == 0 {
		return Plan{}, nil
	}
	_, dropped, err := s.sift(sealed, s.latestSeqs())
	if err != nil {
		return Plan{}, err
	}
	return Plan{Merge: sealed, Drop: dropped}, nil
}
`,
			"record.go": `package walstore

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Op says what a record does to its key.
type Op string

const (
	OpPut Op = "put"
	OpDel Op = "del"
)

// ErrCorrupt reports damage to the log that recovery cannot explain away.
var ErrCorrupt = errors.New("corrupt log")

// ErrBadRecord is for a line that does not decode.
var ErrBadRecord = errors.New("bad record")

// Record is one entry in the log.
type Record struct {
	Seq        int64
	Op         Op
	Key, Value string
	At         int64 // clock reading when it was written
}

var (
	escaper   = strings.NewReplacer("%", "%25", "|", "%7C", "\n", "%0A")
	unescaper = strings.NewReplacer("%25", "%", "%7C", "|", "%0A", "\n")
)

// checksum adds up the bytes of s, modulo 65521, as four hexadecimal digits.
func checksum(s string) string {
	sum := 0
	for i := 0; i < len(s); i++ {
		sum = (sum + int(s[i])) % 65521
	}
	return fmt.Sprintf("%04x", sum)
}

// EncodeRecord returns the log line for r, as docs/format.md describes it.
func EncodeRecord(r Record) string {
	body := fmt.Sprintf("%d|%s|%s|%s|%d", r.Seq, r.Op, escaper.Replace(r.Key), escaper.Replace(r.Value), r.At)
	return body + "|" + checksum(body)
}

// DecodeRecord is the inverse of EncodeRecord. It returns ErrBadRecord for a line
// that does not parse or whose checksum is wrong.
func DecodeRecord(line string) (Record, error) {
	cut := strings.LastIndex(line, "|")
	if cut < 0 || checksum(line[:cut]) != line[cut+1:] {
		return Record{}, ErrBadRecord
	}
	f := strings.Split(line[:cut], "|")
	if len(f) != 5 || (Op(f[1]) != OpPut && Op(f[1]) != OpDel) {
		return Record{}, ErrBadRecord
	}
	seq, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return Record{}, ErrBadRecord
	}
	at, err := strconv.ParseInt(f[4], 10, 64)
	if err != nil {
		return Record{}, ErrBadRecord
	}
	return Record{Seq: seq, Op: Op(f[1]), Key: unescaper.Replace(f[2]), Value: unescaper.Replace(f[3]), At: at}, nil
}
`,
			"recover.go": `package walstore

import (
	"fmt"
	"sort"
	"strings"
)

// Recover rebuilds a store from what is on disk: the newest snapshot if there is one,
// then the segments, so that writes carry on where they left off.
func Recover(disk *Disk, clock Clock) (*Store, error) {
	s := Open(disk, clock)
	base, err := s.loadSnapshot()
	if err != nil {
		return nil, err
	}
	names := s.log.segments()
	for i, name := range names {
		if err := s.replay(name, base, i == len(names)-1); err != nil {
			return nil, err
		}
	}
	if len(names) > 0 {
		last := names[len(names)-1]
		s.log.resume(last, len(disk.Lines(last)))
	}
	return s, nil
}

// replay applies one segment, skipping what the snapshot at base already holds. A line
// that does not decode in the newest segment is a torn write, cut off along with what
// follows it. In an older one it is corruption.
func (s *Store) replay(name string, base int64, newest bool) error {
	lines := s.log.disk.Lines(name)
	for i, line := range lines {
		r, err := DecodeRecord(line)
		if err != nil {
			if !newest {
				return fmt.Errorf("%s line %d: %w", name, i+1, ErrCorrupt)
			}
			s.log.disk.Replace(name, lines[:i])
			return nil
		}
		s.log.seq = max(s.log.seq, r.Seq)
		if r.Seq > base {
			s.change(r)
		}
	}
	return nil
}

// snapshotFiles returns the snapshot file names, oldest first.
func (s *Store) snapshotFiles() []string {
	var names []string
	for _, name := range s.log.disk.List() {
		if strings.HasPrefix(name, "snap-") {
			names = append(names, name)
		}
	}
	return names
}

// loadSnapshot fills the map from the newest snapshot and returns its sequence number.
func (s *Store) loadSnapshot() (int64, error) {
	names := s.snapshotFiles()
	if len(names) == 0 {
		return 0, nil
	}
	name := names[len(names)-1]
	var base int64
	if _, err := fmt.Sscanf(name, "snap-%d.dat", &base); err != nil {
		return 0, fmt.Errorf("%s: %w", name, ErrCorrupt)
	}
	for _, line := range s.log.disk.Lines(name) {
		r, err := DecodeRecord(line)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, ErrCorrupt)
		}
		s.change(r)
	}
	s.log.seq = base
	return base, nil
}

// Snapshot writes every key in the map to a new file and returns the file's name.
func (s *Store) Snapshot() (string, error) {
	if s.closed {
		return "", ErrClosed
	}
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, EncodeRecord(Record{Seq: s.log.seq, Op: OpPut, Key: k, Value: s.data[k], At: s.clock()}))
	}
	name := fmt.Sprintf("snap-%06d.dat", s.log.seq)
	s.log.disk.Replace(name, lines)
	s.metrics.Inc("walstore_snapshots_taken", 1)
	return name, nil
}

// Snapshots returns the snapshot file names, newest first.
func (s *Store) Snapshots() ([]string, error) {
	if s.closed {
		return nil, ErrClosed
	}
	names := s.snapshotFiles()
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// PruneSnapshots removes all but the newest keep snapshots and returns how many it
// removed. The newest one always stays, even when keep is 0.
func (s *Store) PruneSnapshots(keep int) (int, error) {
	if s.closed {
		return 0, ErrClosed
	}
	keep = max(keep, 1)
	names := s.snapshotFiles()
	removed := 0
	for i := 0; i < len(names)-keep; i++ {
		s.log.disk.Remove(names[i])
		removed++
	}
	s.metrics.Inc("walstore_snapshots_pruned", int64(removed))
	return removed, nil
}
`,
			"segment.go": `package walstore

import (
	"fmt"
	"strings"
)

// segmentRecords is how many records a segment holds before the log rolls.
const segmentRecords = 4

// wal writes records to the segment files on a disk.
type wal struct {
	disk   *Disk
	number int   // the active segment
	count  int   // records in the active segment
	seq    int64 // the last sequence number used
}

func newWal(disk *Disk) *wal { return &wal{disk: disk, number: 1} }

func segmentName(number int) string { return fmt.Sprintf("wal-%06d.log", number) }

// segments returns the names of the segment files, oldest first.
func (w *wal) segments() []string {
	var names []string
	for _, name := range w.disk.List() {
		if strings.HasPrefix(name, "wal-") {
			names = append(names, name)
		}
	}
	return names
}

// write numbers the records and appends them all to one segment, rolling first if
// they do not fit in what is left of the active one. A batch larger than a whole
// segment gets a segment of its own, over the limit.
func (w *wal) write(recs []Record, at int64) {
	if w.count > 0 && w.count+len(recs) > segmentRecords {
		w.number++
		w.count = 0
	}
	for _, r := range recs {
		w.seq++
		r.Seq, r.At = w.seq, at
		w.disk.Append(segmentName(w.number), EncodeRecord(r))
		w.count++
	}
}

// resume points the log at an existing segment after a restart.
func (w *wal) resume(name string, count int) {
	fmt.Sscanf(name, "wal-%d.log", &w.number)
	w.count = count
}

// each calls fn for every line on disk, oldest first.
func (w *wal) each(fn func(name string, r Record, err error)) {
	for _, name := range w.segments() {
		for _, line := range w.disk.Lines(name) {
			r, err := DecodeRecord(line)
			fn(name, r, err)
		}
	}
}
`,
			"store.go": `package walstore

import (
	"errors"
	"strings"
)

// Clock reads the current time in milliseconds.
type Clock func() int64

const (
	reservedPrefix = "sys/" // keys for the store's own bookkeeping
	maxValueLen    = 1024
)

var (
	// ErrReserved is for a caller writing a key under reservedPrefix.
	ErrReserved = errors.New("reserved key")
	// ErrTooLarge is for a value over maxValueLen bytes.
	ErrTooLarge = errors.New("value too large")
	// ErrClosed is what everything returns after Close.
	ErrClosed = errors.New("store is closed")
)

// Store is a key-value store that writes every change to its log first.
type Store struct {
	log     *wal
	clock   Clock
	data    map[string]string
	metrics *Metrics
	closed  bool
}

// Open returns an empty store on the disk.
func Open(disk *Disk, clock Clock) *Store {
	return &Store{log: newWal(disk), clock: clock, data: map[string]string{}, metrics: NewMetrics()}
}

// Metrics returns the store's counters.
func (s *Store) Metrics() *Metrics { return s.metrics }

func reserved(key string) bool { return strings.HasPrefix(key, reservedPrefix) }

// Get returns the value of key and whether it has one. It works after Close.
func (s *Store) Get(key string) (string, bool) {
	v, ok := s.data[key]
	return v, ok
}

// change applies one record to the map.
func (s *Store) change(r Record) {
	if r.Op == OpDel {
		delete(s.data, r.Key)
		return
	}
	s.data[r.Key] = r.Value
}

// commit writes the records to the log as one unit, then applies them.
func (s *Store) commit(recs []Record) {
	s.log.write(recs, s.clock())
	for _, r := range recs {
		s.change(r)
	}
	s.metrics.Inc("walstore_records_written", int64(len(recs)))
}

// Put sets the value of key.
func (s *Store) Put(key, value string) error {
	if s.closed {
		return ErrClosed
	}
	if reserved(key) {
		return ErrReserved
	}
	if len(value) > maxValueLen {
		return ErrTooLarge
	}
	s.commit([]Record{{Op: OpPut, Key: key, Value: value}})
	return nil
}

// Delete removes key. Deleting a key that is not there writes nothing.
func (s *Store) Delete(key string) error {
	if s.closed {
		return ErrClosed
	}
	if reserved(key) {
		return ErrReserved
	}
	if _, ok := s.data[key]; !ok {
		return nil
	}
	s.commit([]Record{{Op: OpDel, Key: key}})
	return nil
}

// Close stops the store. Everything but Get returns ErrClosed from then on.
func (s *Store) Close() error {
	s.closed = true
	return nil
}

// BatchOp is one change in a batch: a put, or a delete when Del is set.
type BatchOp struct {
	Key, Value string
	Del        bool
}

// Apply applies several changes as one unit, each as its own record. If it returns
// an error, nothing was written and the data is as it was.
func (s *Store) Apply(ops []BatchOp) error {
	if s.closed {
		return ErrClosed
	}
	recs := make([]Record, 0, len(ops))
	for _, op := range ops {
		if reserved(op.Key) {
			return ErrReserved
		}
		if len(op.Value) > maxValueLen {
			return ErrTooLarge
		}
		r := Record{Op: OpPut, Key: op.Key, Value: op.Value}
		if op.Del {
			r = Record{Op: OpDel, Key: op.Key}
		}
		recs = append(recs, r)
	}
	s.commit(recs)
	return nil
}

// Import reads text in the format Export writes and applies it as one batch. It
// returns how many keys it set. A line it cannot read fails the whole import.
func (s *Store) Import(text string) (int, error) {
	var ops []BatchOp
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		key, value, ok := strings.Cut(line, "\t")
		if !ok {
			return 0, ErrBadRecord
		}
		ops = append(ops, BatchOp{Key: key, Value: strings.ReplaceAll(value, "\\t", "\t")})
	}
	if err := s.Apply(ops); err != nil {
		return 0, err
	}
	return len(ops), nil
}
`,
			"view.go": `package walstore

import (
	"fmt"
	"sort"
	"strings"
)

// Entry is one key and its value.
type Entry struct{ Key, Value string }

// Scan returns the entries whose keys start with prefix, sorted by key.
func (s *Store) Scan(prefix string) ([]Entry, error) {
	if s.closed {
		return nil, ErrClosed
	}
	var out []Entry
	for k, v := range s.data {
		if strings.HasPrefix(k, prefix) && !reserved(k) {
			out = append(out, Entry{k, v})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Key < out[b].Key })
	return out, nil
}

// Report is what Verify found on disk.
type Report struct{ Segments, Records, Bad int }

// Verify counts the segments and the lines in them that do and do not decode. It
// changes nothing.
func (s *Store) Verify() (Report, error) {
	if s.closed {
		return Report{}, ErrClosed
	}
	rep := Report{Segments: len(s.log.segments())}
	s.log.each(func(_ string, _ Record, err error) {
		if err != nil {
			rep.Bad++
		} else {
			rep.Records++
		}
	})
	return rep, nil
}

// Stats describes the store.
type Stats struct {
	Keys, Segments, Records int
	OldestAge               int64 // seconds since the oldest record on disk was written
	PendingTombstones       int   // del records still too young for Compact to drop
}

// Stats reports on the store and its log.
func (s *Store) Stats() (Stats, error) {
	if s.closed {
		return Stats{}, ErrClosed
	}
	st := Stats{Segments: len(s.log.segments())}
	for k := range s.data {
		if !reserved(k) {
			st.Keys++
		}
	}
	now := s.clock()
	latest := s.latestSeqs()
	s.log.each(func(_ string, r Record, err error) {
		if err != nil {
			return
		}
		st.Records++
		st.OldestAge = max(st.OldestAge, (now-r.At)/msPerSecond)
		if r.Op == OpDel && r.Seq == latest[r.Key] && now-r.At < tombstoneGrace {
			st.PendingTombstones++
		}
	})
	return st, nil
}

// Since returns the records on disk after seq, oldest first, for the replication feed.
func (s *Store) Since(seq int64) ([]Record, error) {
	if s.closed {
		return nil, ErrClosed
	}
	var out []Record
	s.log.each(func(_ string, r Record, err error) {
		if err == nil && r.Seq > seq && !reserved(r.Key) {
			out = append(out, r)
		}
	})
	return out, nil
}

// History returns every record of key still on disk, oldest first.
func (s *Store) History(key string) ([]Record, error) {
	if s.closed {
		return nil, ErrClosed
	}
	var out []Record
	s.log.each(func(_ string, r Record, err error) {
		if err == nil && r.Key == key {
			out = append(out, r)
		}
	})
	return out, nil
}

// Export returns one line per key, sorted by key, then a line with the count. A tab
// in a value goes out as a backslash and a t, for the loader that reads this.
func (s *Store) Export() (string, error) {
	if s.closed {
		return "", ErrClosed
	}
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		if !reserved(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\t%s\n", k, strings.ReplaceAll(s.data[k], "\t", "\\t"))
	}
	fmt.Fprintf(&b, "# %d keys\n", len(keys))
	return b.String(), nil
}
`,
		},
		Turns: script(
			cloud("One rule for the whole project before we start: any key starting with `sys/` belongs to the store's own bookkeeping. Callers can't write or delete those keys, and nothing that lists or exports keys may ever show them. For now add `var ErrReserved = errors.New(\"reserved key\")` and make Put return it for such keys."),
			local("Read docs/format.md. In record.go add `func EncodeRecord(r Record) string`, following the file exactly."),
			local("In record.go add `func DecodeRecord(line string) (Record, error)`, the inverse of EncodeRecord. It returns `ErrBadRecord`, a new `errors.New(\"bad record\")`, for a line that doesn't parse or whose checksum is wrong."),
			cloud("Make the store durable. Put appends a put record to the log before it changes the map, and add `func (s *Store) Delete(key string) error`, which does the same with a del record. Deleting a key that isn't there writes nothing and returns nil. Sequence numbers start at 1 and go up by one per record. The log file is called wal-000001.log for now."),
			cloud("Stamp each record's At from the clock. For the record, our Clock reads milliseconds, but anything the public API reports as a duration is in whole seconds, rounded down. Nothing reports one yet."),
			cloud("Put refuses a value longer than 1024 bytes with `ErrTooLarge`, a new `errors.New(\"value too large\")`, and writes nothing. The limit comes from the replication protocol, so it isn't only about Put: no value over it may get into the store."),
			local("Add `type Entry struct{ Key, Value string }` and `func (s *Store) Scan(prefix string) ([]Entry, error)` to store.go: the entries whose keys start with prefix, sorted by key."),
			cloud("Heads-up for when we write an export, no code for it today: the loader that reads our exports chokes on tabs inside values, so a tab in a value has to go out as the two characters \\t. Today's job is to roll the log: a segment holds at most 4 records, then the next record goes to a new file, wal-000002.log and so on."),
			cloud("Add `func (s *Store) Close() error` and `ErrClosed`, a new `errors.New(\"store is closed\")`. After Close, Put, Delete and Scan return ErrClosed, and so does everything else we add from here on that can return an error. Get keeps working."),
			local("Read docs/operations.md and tell me in a few sentences what it says the code has to respect. No code changes."),
			cloud("Add `func Recover(disk *Disk, clock Clock) (*Store, error)` in a new recover.go. It replays the segments in order and returns a store with the data, the sequence number and the active segment back as they were, so that writes carry on where they left off."),
			cloud("store.go is getting crowded. Move everything about writing the log into a new segment.go, behind a small unexported `wal` type that Store holds, with the file naming and the rolling in there too. Recover should use it as well. No behaviour changes."),
			local("Something from the replication team to keep in mind, nothing to build yet: replicas need to see deletions, so a del record has to stay in the log until it is at least an hour old. Today, just move Scan and Entry out of store.go into a new view.go."),
			local("In store.go add `type BatchOp struct { Key, Value string; Del bool }` and `func (s *Store) Apply(ops []BatchOp) error`, which applies several changes as one unit, each as its own record. It's all or nothing: if it returns an error, nothing was written and the data is as it was."),
			local("Add `type Report struct{ Segments, Records, Bad int }` and `func (s *Store) Verify() (Report, error)` to view.go. It walks every segment on disk and counts the segments, the lines that decode and the lines that don't. It changes nothing."),
			local("Wire up metrics.go. Open creates a Metrics, `func (s *Store) Metrics() *Metrics` returns it, and every record written adds 1 to a counter called `walstore_records_written`. All our metric names start with `walstore_`, because that is what the dashboards select on."),
			cloud("A batch must not straddle segments, because a replica applies one segment at a time. If Apply's records don't fit in what's left of the active segment, roll first. A batch bigger than a whole segment gets a fresh segment of its own, over the limit, and the next record after it rolls again."),
			cloud("Add compact.go with `func (s *Store) Compact() (int, error)`. It merges the segments into one under the oldest one's name, keeping only the newest record of each key, and returns how many records it dropped, which also go into a counter called records_dropped. The map doesn't change."),
			local("Add `type Stats struct{ Keys, Segments, Records int }` and `func (s *Store) Stats() (Stats, error)` to view.go. Keys is how many entries Scan(\"\") would return, Segments the number of segment files and Records the number of lines in them that decode."),
			local("Add a field `OldestAge int64` to Stats: how long ago the oldest record still on disk was written."),
			local("The replication team called: what they told us about how long del records have to stay was too short. It's six hours now. Please make the change."),
			cloud("Add `func (s *Store) Since(seq int64) ([]Record, error)` to view.go, for the replication feed: the records on disk after seq, oldest first."),
			cloud("Add a field `PendingTombstones int` to Stats: how many keys have a del record as their newest record on disk that is still too young for Compact to drop."),
			cloud("Add `func (s *Store) Snapshot() (string, error)` to recover.go. It writes every key in the map, as put records, to a new file `snap-NNNNNN.dat`, NNNNNN being the current sequence number, returns the file's name and counts itself in a counter called snapshots_taken. Recover starts from the newest snapshot if there is one and applies only the records newer than it. The sequence number never goes back."),
			local("Add `func (s *Store) Export() (string, error)` to view.go: one line per key, sorted by key, with the key, a tab and the value, and then a last line `# N keys` with the count."),
			local("Add to recover.go `func (s *Store) Snapshots() ([]string, error)`, the snapshot file names, newest first, and `func (s *Store) PruneSnapshots(keep int) (int, error)`, which removes all but the newest keep of them and returns how many it removed. Those also go into a counter called snapshots_pruned."),
			cloud("Verify, Stats, Since and Compact each walk the segments in their own way. Give `wal` one method that walks every line on disk and move them onto it, as far as they can use it. No behaviour changes."),
			local("Add `func (s *Store) History(key string) ([]Record, error)` to view.go: every record of that key still on disk, oldest first."),
			cloud("Add `func (s *Store) Import(text string) (int, error)` to store.go. It reads text in the format Export writes, applies it as one batch and returns how many keys it set. A line it can't read fails the whole import with ErrBadRecord."),
			local("In compact.go add `type Plan struct { Merge []string; Drop int }` and `func (s *Store) CompactPlan() (Plan, error)`: which segments Compact would merge right now, oldest first, and how many records it would drop. It changes nothing, and Merge is empty when Compact would do nothing."),
		),
		Carries: []Carry{
			{
				What:        "keys under sys/ cannot be written by callers and are never listed or exported",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 6, 13, 18, 21, 24, 28},
				Tests:       []string{"TestDeleteRefusesInternalKey", "TestScanHidesInternalKeys", "TestApplyRefusesInternalKey", "TestStatsKeysHideInternal", "TestSinceHidesInternal", "TestExportHidesInternal", "TestImportRefusesInternalKey"},
				Absent:      []string{"sys/", "reserved", "bookkeeping"},
			},
			{
				What:        "a bad line is a torn write in the newest segment and corruption in an older one (docs/format.md)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{10},
				Source:      "docs/format.md",
				Tests:       []string{"TestRecoverDropsTornTail", "TestRecoverFailsOnOlderDamage"},
				Absent:      []string{"format.md", "torn", "half-written", "crash", "damage", "corrupt"},
			},
			{
				What:        "the clock reads milliseconds and durations in the public API are whole seconds",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{17, 19, 22},
				Tests:       []string{"TestStatsOldestAgeInSeconds", "TestCompactKeepsYoungTombstone", "TestCompactKeepsTombstoneOfTwoHours", "TestStatsPendingTombstones"},
				Absent:      []string{"milliseconds", "whole seconds", "rounded down", "in seconds", "1000"},
			},
			{
				What:        "no value over 1024 bytes gets into the store, however it is written",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{13, 28},
				Tests:       []string{"TestApplyRefusesLargeValue", "TestImportRefusesLargeValue"},
				Absent:      []string{"1024", "too large", "errtoolarge", "longer than", "limit comes"},
			},
			{
				What:        "a tab in an exported value goes out as a backslash and a t",
				Kind:        CarryUser,
				Established: 7,
				Needed:      []int{24},
				Tests:       []string{"TestExportEscapesTabs"},
				Absent:      []string{"loader", "chokes", "two characters", "\\t"},
			},
			{
				What:        "everything added after Close was introduced returns ErrClosed on a closed store",
				Kind:        CarryEmbodied,
				Established: 8,
				Needed:      []int{13, 14, 17, 18, 21, 23, 24, 25, 27, 29},
				Tests:       []string{"TestApplyAfterClose", "TestVerifyAfterClose", "TestCompactAfterClose", "TestStatsAfterClose", "TestSinceAfterClose", "TestSnapshotAfterClose", "TestExportAfterClose", "TestSnapshotsAfterClose", "TestPruneSnapshotsAfterClose", "TestHistoryAfterClose", "TestCompactPlanAfterClose"},
				Absent:      []string{"errclosed", "closed", "after close", "close("},
			},
			{
				What:        "compaction leaves the newest segment alone and needs two sealed segments, and pruning keeps the newest snapshot (docs/operations.md)",
				Kind:        CarryDiscovered,
				Established: 9,
				Needed:      []int{17, 25, 29},
				Source:      "docs/operations.md",
				Tests:       []string{"TestCompactLeavesNewestSegmentAlone", "TestCompactNeedsTwoSealedSegments", "TestPruneKeepsNewestSnapshot", "TestCompactPlanNamesSealedSegments"},
				Absent:      []string{"operations.md", "byte for byte", "newest segment", "sealed", "keep none", "keep nothing"},
			},
			{
				What:        "compaction keeps a del record until it has aged enough, an hour as first stated",
				Kind:        CarryUser,
				Established: 12,
				Needed:      []int{17},
				Tests:       []string{"TestCompactKeepsYoungTombstone", "TestCompactKeepsTombstoneOfTwoHours"},
				Absent:      []string{"an hour", "one hour", "see deletions", "hour old"},
			},
			{
				What:        "all metric names start with walstore_",
				Kind:        CarryUser,
				Established: 15,
				Needed:      []int{17, 23, 25},
				Tests:       []string{"TestCompactCountsDroppedRecords", "TestSnapshotIsCounted", "TestPruneSnapshotsIsCounted"},
				Absent:      []string{"walstore_", "dashboards", "select on"},
			},
			{
				What:        "revised: a del record stays for six hours",
				Kind:        CarryUser,
				Established: 20,
				Needed:      []int{22},
				Tests:       []string{"TestCompactKeepsTombstoneOfTwoHours", "TestStatsPendingTombstones"},
				Absent:      []string{"six hours", "6 hours", "21600"},
			},
		},
	}
}
