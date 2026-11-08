package s2

func accB06() Task {
	return Task{
		ID:     "s2-b06-feature-flags",
		Set:    "acceptance",
		System: system("a feature flag store"),
		Files: map[string]string{
			"go.mod": goMod,
			"store.go": `package feature

// Flag is one feature switch.
type Flag struct {
	Name    string
	Enabled bool
	Percent int      // share of users in the rollout, 0 to 100
	Allow   []string // user ids that get the feature whatever Percent says
}

// Store holds flags by name.
type Store struct {
	flags map[string]Flag
}

// NewStore returns an empty Store.
func NewStore() *Store { return &Store{flags: map[string]Flag{}} }

// Put adds a flag or replaces the flag of that name.
func (s *Store) Put(f Flag) { s.flags[f.Name] = f }
`,
			"docs/rollout.txt": `Rollout buckets (shared with the mobile SDK)

A user's bucket for a flag is the FNV-1a 32-bit hash (hash/fnv, New32a) of the
flag name, a "/" and the user id, taken modulo 100. A user is in the rollout
when their bucket is below Percent.

The SDK computes the same bucket offline, so this formula can't change.
`,
			"store_test.go": `package feature

import "testing"

func TestPut(t *testing.T) {
	s := NewStore()
	s.Put(Flag{Name: "a"})
	if len(s.flags) != 1 {
		t.Fatalf("%d flags", len(s.flags))
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package feature_test

import (
	"errors"
	"hash/fnv"
	"slices"
	"testing"

	feature "fixture"
)

func bucket(flag, user string) int {
	h := fnv.New32a()
	h.Write([]byte(flag + "/" + user))
	return int(h.Sum32() % 100)
}

func store(flags ...feature.Flag) *feature.Store {
	s := feature.NewStore()
	for _, f := range flags {
		s.Put(f)
	}
	return s
}

func TestBucketInRange(t *testing.T) {
	for _, user := range []string{"alice", "bob", "carol", "", "u-1"} {
		if got := feature.Bucket("beta", user); got < 0 || got > 99 {
			t.Errorf("%q: %d", user, got)
		}
	}
}

func TestBucketFormula(t *testing.T) {
	for _, flag := range []string{"beta", "new-nav"} {
		for _, user := range []string{"alice", "bob", "carol", "dave"} {
			if got, want := feature.Bucket(flag, user), bucket(flag, user); got != want {
				t.Errorf("%s, %s: %d, want %d", flag, user, got, want)
			}
		}
	}
}

func TestOnFollowsBucket(t *testing.T) {
	for _, user := range []string{"alice", "bob", "carol", "dave", "erin"} {
		b := bucket("beta", user)
		if store(feature.Flag{Name: "beta", Enabled: true, Percent: b}).On("beta", user) {
			t.Errorf("%s is in bucket %d, so not in at Percent %d", user, b, b)
		}
		if !store(feature.Flag{Name: "beta", Enabled: true, Percent: b + 1}).On("beta", user) {
			t.Errorf("%s is in bucket %d, so in at Percent %d", user, b, b+1)
		}
	}
}

func TestNamesSorted(t *testing.T) {
	s := store(feature.Flag{Name: "b"}, feature.Flag{Name: "a"}, feature.Flag{Name: "c"})
	if got := s.Names(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("Names = %v", got)
	}
}

func TestValidName(t *testing.T) {
	for _, good := range []string{"beta", "new-nav", "v2"} {
		if !feature.ValidName(good) {
			t.Errorf("%q refused", good)
		}
	}
	for _, bad := range []string{"", "Beta", "a b", "a_b", "a/b"} {
		if feature.ValidName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestHas(t *testing.T) {
	s := store(feature.Flag{Name: "a"})
	if !s.Has("a") || s.Has("b") {
		t.Fatal("Has is wrong")
	}
}

func TestOnUnknownFlag(t *testing.T) {
	if store().On("nope", "alice") {
		t.Fatal("an unknown flag is on")
	}
}

func TestOnFullAndZero(t *testing.T) {
	full := store(feature.Flag{Name: "x", Enabled: true, Percent: 100})
	none := store(feature.Flag{Name: "x", Enabled: true, Percent: 0})
	for _, user := range []string{"alice", "bob", "carol"} {
		if !full.On("x", user) {
			t.Errorf("%s is off at 100", user)
		}
		if none.On("x", user) {
			t.Errorf("%s is on at 0", user)
		}
	}
}

func TestOnAllowList(t *testing.T) {
	s := store(feature.Flag{Name: "x", Enabled: true, Percent: 0, Allow: []string{"alice"}})
	if !s.On("x", "alice") {
		t.Error("alice is on the list")
	}
	if s.On("x", "bob") {
		t.Error("bob is not on the list")
	}
}

func TestDisabledFullRollout(t *testing.T) {
	if store(feature.Flag{Name: "x", Enabled: false, Percent: 100}).On("x", "alice") {
		t.Fatal("a disabled flag is on")
	}
}

func TestDisabledBeatsAllowList(t *testing.T) {
	s := store(feature.Flag{Name: "x", Enabled: false, Percent: 0, Allow: []string{"alice"}})
	if s.On("x", "alice") {
		t.Fatal("a disabled flag is on for someone on the list")
	}
}

func TestDisabledBeatsAllowListAtFull(t *testing.T) {
	s := store(feature.Flag{Name: "x", Enabled: false, Percent: 100, Allow: []string{"alice"}})
	if s.On("x", "alice") {
		t.Fatal("a disabled flag is on for someone on the list")
	}
}

func TestAnonymousPartialRollout(t *testing.T) {
	for _, flag := range []string{"beta", "new-nav", "search", "v2"} {
		if store(feature.Flag{Name: flag, Enabled: true, Percent: 99}).On(flag, "") {
			t.Errorf("%s: anonymous traffic is on at 99", flag)
		}
	}
}

func TestAnonymousFullRollout(t *testing.T) {
	if !store(feature.Flag{Name: "x", Enabled: true, Percent: 100}).On("x", "") {
		t.Fatal("anonymous traffic is off at 100")
	}
}

func TestAddStores(t *testing.T) {
	s := feature.NewStore()
	if err := s.Add(feature.Flag{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if !s.Has("x") {
		t.Fatal("not stored")
	}
}

func TestAddDuplicate(t *testing.T) {
	s := store(feature.Flag{Name: "x", Percent: 7})
	if err := s.Add(feature.Flag{Name: "x", Percent: 100, Enabled: true}); err == nil {
		t.Fatal("no error")
	}
	if s.On("x", "alice") {
		t.Fatal("the duplicate replaced the flag")
	}
}

func TestAddRefusesBadName(t *testing.T) {
	s := feature.NewStore()
	if err := s.Add(feature.Flag{Name: "Bad Name"}); !errors.Is(err, feature.ErrBadName) {
		t.Fatalf("err = %v", err)
	}
	if s.Has("Bad Name") {
		t.Fatal("stored anyway")
	}
}

func TestRenameMoves(t *testing.T) {
	s := store(feature.Flag{Name: "old", Enabled: true, Percent: 100})
	if err := s.Rename("old", "new"); err != nil {
		t.Fatal(err)
	}
	if s.Has("old") || !s.On("new", "alice") {
		t.Fatal("not renamed")
	}
}

func TestRenameMissingOrTaken(t *testing.T) {
	s := store(feature.Flag{Name: "a"}, feature.Flag{Name: "b"})
	if err := s.Rename("nope", "c"); err == nil {
		t.Error("missing: no error")
	}
	if err := s.Rename("a", "b"); err == nil {
		t.Error("taken: no error")
	}
}

func TestRenameRefusesBadName(t *testing.T) {
	s := store(feature.Flag{Name: "a"})
	if err := s.Rename("a", "Bad Name"); !errors.Is(err, feature.ErrBadName) {
		t.Fatalf("err = %v", err)
	}
	if !s.Has("a") || s.Has("Bad Name") {
		t.Fatal("renamed anyway")
	}
}
`},
		Solution: map[string]string{
			"store.go": `package feature

import (
	"errors"
	"hash/fnv"
	"maps"
	"slices"
)

// ErrBadName refuses a flag name that fails ValidName.
var ErrBadName = errors.New("bad flag name")

// Flag is one feature switch.
type Flag struct {
	Name    string
	Enabled bool
	Percent int      // share of users in the rollout, 0 to 100
	Allow   []string // user ids that get the feature whatever Percent says
}

// Store holds flags by name.
type Store struct {
	flags map[string]Flag
}

// NewStore returns an empty Store.
func NewStore() *Store { return &Store{flags: map[string]Flag{}} }

// Put adds a flag or replaces the flag of that name.
func (s *Store) Put(f Flag) { s.flags[f.Name] = f }

// Bucket returns the rollout bucket of a user for the flag called name.
func Bucket(name, user string) int {
	h := fnv.New32a()
	h.Write([]byte(name + "/" + user))
	return int(h.Sum32() % 100)
}

// Names returns the flag names, sorted.
func (s *Store) Names() []string { return slices.Sorted(maps.Keys(s.flags)) }

// ValidName reports whether name is non-empty and only lower-case letters, digits and hyphens.
func ValidName(name string) bool {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return name != ""
}

// Has reports whether a flag of that name exists.
func (s *Store) Has(name string) bool {
	_, ok := s.flags[name]
	return ok
}

// On reports whether this user gets the feature.
func (s *Store) On(name, user string) bool {
	f, ok := s.flags[name]
	if !ok || !f.Enabled {
		return false
	}
	if slices.Contains(f.Allow, user) {
		return true
	}
	if user == "" {
		return f.Percent >= 100
	}
	return Bucket(name, user) < f.Percent
}

// Add stores a new flag.
func (s *Store) Add(f Flag) error {
	if !ValidName(f.Name) {
		return ErrBadName
	}
	if s.Has(f.Name) {
		return errors.New("flag already exists")
	}
	s.flags[f.Name] = f
	return nil
}

// Rename gives an existing flag a new name.
func (s *Store) Rename(old, new string) error {
	if !ValidName(new) {
		return ErrBadName
	}
	f, ok := s.flags[old]
	if !ok || s.Has(new) {
		return errors.New("no such flag, or the new name is taken")
	}
	delete(s.flags, old)
	f.Name = new
	s.flags[new] = f
	return nil
}
`,
		},
		Turns: script(
			cloud("Read docs/rollout.txt and tell me what it pins down. No code changes."),
			local("Add `func Bucket(name, user string) int`: the rollout bucket of this user for the flag called name."),
			local("Enabled is our kill switch. When it's false nobody gets the feature, not even people on the Allow list. Add `func (s *Store) Names() []string`, the flag names sorted."),
			cloud("Flag names end up in URLs and config keys, so add `func ValidName(name string) bool`: non-empty, and only lower-case letters, digits and hyphens. Put stays as it is for tests and imports, but every other call we add that brings a name into the store has to refuse a bad one with `var ErrBadName = errors.New(\"bad flag name\")`, so add that too."),
			local("Anonymous traffic comes through with an empty user id. They'd all land in one bucket, so a partial rollout would switch all of them on or off together. They only get a feature at Percent 100. Separately, add `func (s *Store) Has(name string) bool`: whether a flag of that name is in the store, on or off."),
			cloud("Add `func (s *Store) On(name, user string) bool`: whether this user gets the feature. An unknown flag is off."),
			local("Add `func (s *Store) Add(f Flag) error`, which stores a new flag. A name that's already in the store is an error."),
			cloud("Add `func (s *Store) Rename(old, new string) error`, which gives an existing flag a new name. It's an error if old isn't there or new is taken."),
		),
		Carries: []Carry{
			{
				What:        "the bucket is FNV-1a of flag/user modulo 100, and a user is in below Percent (docs/rollout.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{1, 5},
				Source:      "docs/rollout.txt",
				Tests:       []string{"TestBucketFormula", "TestOnFollowsBucket"},
				Absent:      []string{"fnv", "hash", "modulo", "mobile", "sdk"},
			},
			{
				What:        "a disabled flag is off for everyone, allow list included",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{5},
				Tests:       []string{"TestDisabledBeatsAllowList", "TestDisabledBeatsAllowListAtFull"},
				Absent:      []string{"kill switch", "not even", "nobody"},
			},
			{
				What:        "an empty user id only gets a feature at Percent 100",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{5},
				Tests:       []string{"TestAnonymousPartialRollout"},
				Absent:      []string{"anonymous", "empty user", "empty id", "percent 100"},
			},
			{
				What:        "calls that bring a name into the store refuse a bad one with ErrBadName",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{6, 7},
				Tests:       []string{"TestAddRefusesBadName", "TestRenameRefusesBadName"},
				Absent:      []string{"errbadname", "validname", "bad one", "refuse"},
			},
		},
	}
}
