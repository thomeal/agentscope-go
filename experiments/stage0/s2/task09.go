package s2

func task09() Task {
	return Task{
		ID:     "s2-09-cache-cleanup",
		Set:    "tuning",
		System: system("an in-memory cache"),
		Files: map[string]string{
			"go.mod": goMod,
			"cache.go": `package cache

type entry struct {
	value   string
	expires int64
}

// Cache maps strings to strings with per-entry expiry. now is the clock, in seconds.
type Cache struct {
	entries map[string]entry
	now     func() int64
	hits    int
	misses  int
}

// New returns an empty cache using the given clock.
func New(now func() int64) *Cache {
	return &Cache{entries: map[string]entry{}, now: now}
}

// Set stores a value for ttl seconds.
func (c *Cache) Set(key, value string, ttl int) {
	c.entries[key] = entry{value: value, expires: c.now() + int64(ttl)}
}

// Get returns a live value and counts the lookup.
func (c *Cache) Get(key string) (string, bool) {
	e, ok := c.entries[key]
	if !ok || c.now() >= e.expires {
		c.misses++
		return "", false
	}
	c.hits++
	return e.value, true
}
`,
			"stats.go": `package cache

// Stats reports lookup counts.
func (c *Cache) Stats() (hits, misses int) { return c.hits, c.misses }

// Sweep removes expired entries and returns how many it removed.
func (c *Cache) Sweep() int {
	removed := 0
	for key, e := range c.entries {
		if c.now() >= e.expires {
			delete(c.entries, key)
			removed++
		}
	}
	return removed
}
`,
			"docs/integration.txt": `Integration notes

The nightly maintenance job lives in another repository and links against this
package. It calls Cache.Sweep() and Cache.Stats() directly, so those two methods
must stay exported with their current names, signatures and results.
It uses nothing else from this package.
`,
			"cache_test.go": `package cache

import "testing"

func TestExpiry(t *testing.T) {
	now := int64(100)
	c := New(func() int64 { return now })
	c.Set("a", "1", 10)
	if v, ok := c.Get("a"); !ok || v != "1" {
		t.Fatalf("got %q, %v", v, ok)
	}
	now = 110
	if _, ok := c.Get("a"); ok {
		t.Fatal("expired entry is still live")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package cache_test

import (
	"reflect"
	"testing"

	cache "fixture"
)

type clock struct{ t int64 }

func (c *clock) now() int64 { return c.t }

func started() (*cache.Cache, *clock) {
	clk := &clock{t: 100}
	return cache.New(clk.now), clk
}

func TestLenCountsExpired(t *testing.T) {
	c, clk := started()
	c.Set("a", "1", 5)
	c.Set("b", "2", 50)
	clk.t = 110
	if c.Len() != 2 {
		t.Fatalf("Len = %d", c.Len())
	}
}

func TestSetZeroNeverExpires(t *testing.T) {
	c, clk := started()
	c.Set("a", "1", 0)
	c.Set("b", "2", -5)
	clk.t = 1 << 40
	if _, ok := c.Get("a"); !ok {
		t.Error("ttl 0 expired")
	}
	if _, ok := c.Get("b"); !ok {
		t.Error("negative ttl expired")
	}
}

func TestTouchExtends(t *testing.T) {
	c, clk := started()
	c.Set("a", "1", 5)
	if !c.Touch("a", 50) {
		t.Fatal("Touch = false")
	}
	clk.t = 120
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expired despite the new ttl")
	}
}

func TestTouchMissingOrExpired(t *testing.T) {
	c, clk := started()
	if c.Touch("nope", 5) {
		t.Error("Touch on a missing key = true")
	}
	c.Set("a", "1", 5)
	clk.t = 110
	if c.Touch("a", 5) {
		t.Error("Touch on an expired key = true")
	}
}

func TestTouchZeroNeverExpires(t *testing.T) {
	c, clk := started()
	c.Set("a", "1", 5)
	if !c.Touch("a", 0) {
		t.Fatal("Touch = false")
	}
	clk.t = 1 << 40
	if _, ok := c.Get("a"); !ok {
		t.Fatal("ttl 0 expired")
	}
}

func TestDelete(t *testing.T) {
	c, _ := started()
	c.Set("a", "1", 5)
	c.Delete("a")
	if _, ok := c.Get("a"); ok || c.Len() != 0 {
		t.Fatal("still there")
	}
}

func TestSnapshotLive(t *testing.T) {
	c, clk := started()
	c.Set("a", "1", 5)
	c.Set("b", "2", 50)
	clk.t = 110
	got := c.Snapshot()
	if len(got) != 1 || got["b"] != "2" {
		t.Fatalf("Snapshot = %v", got)
	}
}

func TestSnapshotOmitsTmp(t *testing.T) {
	c, _ := started()
	c.Set("tmp:x", "1", 50)
	c.Set("keep", "2", 50)
	got := c.Snapshot()
	if len(got) != 1 || got["keep"] != "2" {
		t.Fatalf("Snapshot = %v", got)
	}
}

func TestExportForm(t *testing.T) {
	c, _ := started()
	c.Set("b", "2", 50)
	c.Set("a", "1", 50)
	if got := c.Export(); got != "a=1\nb=2\n" {
		t.Fatalf("Export = %q", got)
	}
}

func TestExportOmitsTmp(t *testing.T) {
	c, _ := started()
	c.Set("tmp:x", "1", 50)
	c.Set("keep", "2", 50)
	if got := c.Export(); got != "keep=2\n" {
		t.Fatalf("Export = %q", got)
	}
}

func TestExpireReturnsKeys(t *testing.T) {
	c, clk := started()
	c.Set("b", "1", 5)
	c.Set("a", "1", 5)
	c.Set("live", "1", 50)
	c.Set("forever", "1", 0)
	clk.t = 110
	got := c.Expire()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Expire = %v", got)
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d", c.Len())
	}
}

func TestMetrics(t *testing.T) {
	c, _ := started()
	c.Set("a", "1", 50)
	c.Get("a")
	c.Get("a")
	c.Get("nope")
	if got := c.Metrics(); got.Hits != 2 || got.Misses != 1 {
		t.Fatalf("Metrics = %+v", got)
	}
}

// The nightly job's methods are looked up by name, so that removing one fails
// this test and not the build.
func TestSweepKept(t *testing.T) {
	c, clk := started()
	c.Set("a", "1", 5)
	clk.t = 110
	m := reflect.ValueOf(c).MethodByName("Sweep")
	if !m.IsValid() {
		t.Fatal("Cache.Sweep no longer exists")
	}
	if m.Type().NumIn() != 0 || m.Type().NumOut() != 1 || m.Type().Out(0).Kind() != reflect.Int {
		t.Fatalf("Cache.Sweep has the signature %v", m.Type())
	}
	if got := m.Call(nil)[0].Int(); got != 1 {
		t.Fatalf("Sweep = %d", got)
	}
	if c.Len() != 0 {
		t.Fatalf("Len = %d", c.Len())
	}
}

func TestStatsKept(t *testing.T) {
	c, _ := started()
	c.Set("a", "1", 50)
	c.Get("a")
	c.Get("nope")
	c.Get("nope")
	m := reflect.ValueOf(c).MethodByName("Stats")
	if !m.IsValid() {
		t.Fatal("Cache.Stats no longer exists")
	}
	if m.Type().NumIn() != 0 || m.Type().NumOut() != 2 || m.Type().Out(0).Kind() != reflect.Int || m.Type().Out(1).Kind() != reflect.Int {
		t.Fatalf("Cache.Stats has the signature %v", m.Type())
	}
	out := m.Call(nil)
	if out[0].Int() != 1 || out[1].Int() != 2 {
		t.Fatalf("Stats = %d, %d", out[0].Int(), out[1].Int())
	}
}
`},
		Solution: map[string]string{
			"cache.go": `package cache

import (
	"sort"
	"strings"
)

type entry struct {
	value   string
	expires int64 // 0 means never
}

// Cache maps strings to strings with per-entry expiry. now is the clock, in seconds.
type Cache struct {
	entries map[string]entry
	now     func() int64
	hits    int
	misses  int
}

// New returns an empty cache using the given clock.
func New(now func() int64) *Cache {
	return &Cache{entries: map[string]entry{}, now: now}
}

func (c *Cache) deadline(ttl int) int64 {
	if ttl <= 0 {
		return 0
	}
	return c.now() + int64(ttl)
}

func (c *Cache) live(e entry) bool { return e.expires == 0 || c.now() < e.expires }

// Set stores a value for ttl seconds. Zero or less means it never expires.
func (c *Cache) Set(key, value string, ttl int) {
	c.entries[key] = entry{value: value, expires: c.deadline(ttl)}
}

// Get returns a live value and counts the lookup.
func (c *Cache) Get(key string) (string, bool) {
	e, ok := c.entries[key]
	if !ok || !c.live(e) {
		c.misses++
		return "", false
	}
	c.hits++
	return e.value, true
}

// Len returns the number of entries, expired ones included.
func (c *Cache) Len() int { return len(c.entries) }

// Touch gives an existing, unexpired entry a new ttl.
func (c *Cache) Touch(key string, ttl int) bool {
	e, ok := c.entries[key]
	if !ok || !c.live(e) {
		return false
	}
	e.expires = c.deadline(ttl)
	c.entries[key] = e
	return true
}

// Delete removes an entry.
func (c *Cache) Delete(key string) { delete(c.entries, key) }

func exportable(key string) bool { return !strings.HasPrefix(key, "tmp:") }

// Snapshot returns live pairs for writing to disk.
func (c *Cache) Snapshot() map[string]string {
	out := map[string]string{}
	for key, e := range c.entries {
		if c.live(e) && exportable(key) {
			out[key] = e.value
		}
	}
	return out
}

// Export returns live entries as sorted key=value lines.
func (c *Cache) Export() string {
	snap := c.Snapshot()
	keys := make([]string, 0, len(snap))
	for key := range snap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key + "=" + snap[key] + "\n")
	}
	return b.String()
}
`,
			"stats.go": `package cache

import "sort"

// Metrics holds lookup counts.
type Metrics struct {
	Hits, Misses int
}

// Metrics reports lookup counts.
func (c *Cache) Metrics() Metrics { return Metrics{Hits: c.hits, Misses: c.misses} }

// Stats reports lookup counts for the nightly job, which links against it.
func (c *Cache) Stats() (hits, misses int) { return c.hits, c.misses }

// Expire removes expired entries and returns their keys, sorted.
func (c *Cache) Expire() []string {
	var keys []string
	for key, e := range c.entries {
		if !c.live(e) {
			delete(c.entries, key)
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// Sweep removes expired entries for the nightly job, which links against it.
func (c *Cache) Sweep() int { return len(c.Expire()) }
`,
		},
		Turns: script(
			cloud("Add `func (c *Cache) Len() int`: the number of entries, expired ones included."),
			cloud("Read docs/integration.txt and tell me what it means for this package's API. No code changes."),
			cloud("A decision on TTLs: zero or negative means an entry never expires. That goes for anything that sets a ttl. Make Set work this way."),
			local("Add `func (c *Cache) Touch(key string, ttl int) bool`, which gives an existing, unexpired entry a new ttl and returns false if there is no such entry."),
			local("A rule from security: keys starting with tmp: hold throwaway data and must never leave the process in any persisted or exported form. Nothing to code for that yet. Add `func (c *Cache) Delete(key string)`."),
			cloud("Add `func (c *Cache) Snapshot() map[string]string`: the live key/value pairs, for writing to disk."),
			local("Add `func (c *Cache) Expire() []string`, which removes expired entries and returns their keys sorted. It replaces the existing expiry-removal code, so clean up what that makes redundant: the package should end up with one implementation of that logic."),
			cloud("Add `type Metrics struct { Hits, Misses int }` and `func (c *Cache) Metrics() Metrics`. It's how our own code reads the counters from now on, so clean up what that makes redundant."),
			local("Add `func (c *Cache) Export() string`: the live entries as sorted key=value lines, each ending in a newline, for the backup tool."),
		),
		Carries: []Carry{
			{
				What:        "Cache.Sweep and Cache.Stats must stay for an external job (docs/integration.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{6, 7},
				Source:      "docs/integration.txt",
				Tests:       []string{"TestSweepKept", "TestStatsKept"},
				Absent:      []string{"sweep", "stats", "nightly", "another repository"},
			},
			{
				What:        "anything that sets a ttl treats zero or negative as never expiring",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{3},
				Tests:       []string{"TestTouchZeroNeverExpires"},
				Absent:      []string{"never expires", "negative", "zero"},
			},
			{
				What:        "tmp: keys never leave the process in persisted or exported form",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{5, 8},
				Tests:       []string{"TestSnapshotOmitsTmp", "TestExportOmitsTmp"},
				Absent:      []string{"tmp:", "throwaway", "security"},
			},
		},
	}
}
