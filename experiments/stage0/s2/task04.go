package s2

func task04() Task {
	return Task{
		ID:     "s2-04-settings-file",
		Set:    "tuning",
		System: system("a settings file library"),
		Files: map[string]string{
			"go.mod": goMod,
			"config.go": `package settings

// Config holds string settings.
type Config struct {
	values map[string]string
}

// New returns an empty Config.
func New() *Config { return &Config{values: map[string]string{}} }

// Set stores a value.
func (c *Config) Set(key, value string) { c.values[key] = value }

// Get returns a value and whether it exists.
func (c *Config) Get(key string) (string, bool) {
	v, ok := c.values[key]
	return v, ok
}
`,
			"parse.go": `package settings

import (
	"fmt"
	"strings"
)

// Parse reads key=value lines. Blank lines and # comments are skipped.
func Parse(text string) (*Config, error) {
	c := New()
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: missing =", i+1)
		}
		c.Set(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	return c, nil
}
`,
			"deploy/README.txt": `Deployment notes

Settings files are written by hand by the deploy team. Time values carry a unit
suffix such as 30s or 5m. A bare number, which older files still have, counts as
seconds.

Files are UTF-8 and are rotated weekly.
`,
			"parse_test.go": `package settings

import "testing"

func TestParse(t *testing.T) {
	c, err := Parse("# comment\n\nname = alpha\n")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := c.Get("name"); !ok || v != "alpha" {
		t.Fatalf("got %q, %v", v, ok)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse("nonsense"); err == nil {
		t.Fatal("no error")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package settings_test

import (
	"testing"
	"time"

	settings "fixture"
)

func TestKeysSorted(t *testing.T) {
	c := settings.New()
	c.Set("b", "1")
	c.Set("a", "2")
	if got := c.Keys(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Keys = %v", got)
	}
}

func TestSetGetIgnoreCase(t *testing.T) {
	c := settings.New()
	c.Set("Server.Port", "80")
	if v, ok := c.Get("server.PORT"); !ok || v != "80" {
		t.Fatalf("Get = %q, %v", v, ok)
	}
	c.Set("SERVER.port", "81")
	if v, _ := c.Get("server.port"); v != "81" || len(c.Keys()) != 1 {
		t.Fatalf("Get = %q, keys %v", v, c.Keys())
	}
}

func TestGetBoolWords(t *testing.T) {
	c := settings.New()
	for _, word := range []string{"on", "ON", "yes", "Yes", "true", "TRUE"} {
		c.Set("flag", word)
		if !c.GetBool("flag") {
			t.Errorf("%q should be true", word)
		}
	}
}

func TestGetBoolOtherValuesFalse(t *testing.T) {
	c := settings.New()
	for _, word := range []string{"1", "t", "T", "enabled", "off"} {
		c.Set("flag", word)
		if c.GetBool("flag") {
			t.Errorf("%q should be false", word)
		}
	}
}

func TestGetBoolMissing(t *testing.T) {
	if settings.New().GetBool("nope") {
		t.Fatal("missing key is true")
	}
}

func TestDeleteExact(t *testing.T) {
	c := settings.New()
	c.Set("a", "1")
	c.Delete("a")
	if _, ok := c.Get("a"); ok {
		t.Fatal("still there")
	}
}

func TestDeleteIgnoresCase(t *testing.T) {
	c := settings.New()
	c.Set("server.port", "1")
	c.Delete("Server.Port")
	if _, ok := c.Get("server.port"); ok {
		t.Fatal("still there")
	}
}

func TestHasExact(t *testing.T) {
	c := settings.New()
	c.Set("a", "1")
	if !c.Has("a") || c.Has("b") {
		t.Fatal("wrong answer")
	}
}

func TestHasIgnoresCase(t *testing.T) {
	c := settings.New()
	c.Set("server.port", "1")
	if !c.Has("SERVER.PORT") {
		t.Fatal("not found")
	}
}

func TestGetDurationSuffix(t *testing.T) {
	c := settings.New()
	c.Set("t", "2m")
	if d, err := c.GetDuration("t"); err != nil || d != 2*time.Minute {
		t.Fatalf("GetDuration = %v, %v", d, err)
	}
}

func TestGetDurationErrors(t *testing.T) {
	c := settings.New()
	c.Set("bad", "soon")
	if _, err := c.GetDuration("bad"); err == nil {
		t.Error("unparsable value gave no error")
	}
	if _, err := c.GetDuration("missing"); err == nil {
		t.Error("missing key gave no error")
	}
}

func TestGetDurationBareNumber(t *testing.T) {
	c := settings.New()
	c.Set("t", "45")
	if d, err := c.GetDuration("t"); err != nil || d != 45*time.Second {
		t.Fatalf("GetDuration = %v, %v", d, err)
	}
}

func TestMerge(t *testing.T) {
	a, b := settings.New(), settings.New()
	a.Set("x", "1")
	a.Set("y", "1")
	b.Set("Y", "2")
	b.Set("z", "2")
	a.Merge(b)
	for key, want := range map[string]string{"x": "1", "y": "2", "z": "2"} {
		if v, _ := a.Get(key); v != want {
			t.Errorf("%s = %q", key, v)
		}
	}
}
`},
		Solution: map[string]string{
			"config.go": `package settings

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config holds string settings. Keys ignore letter case.
type Config struct {
	values map[string]string
}

// New returns an empty Config.
func New() *Config { return &Config{values: map[string]string{}} }

func norm(key string) string { return strings.ToLower(key) }

// Set stores a value.
func (c *Config) Set(key, value string) { c.values[norm(key)] = value }

// Get returns a value and whether it exists.
func (c *Config) Get(key string) (string, bool) {
	v, ok := c.values[norm(key)]
	return v, ok
}

// Keys returns the keys sorted.
func (c *Config) Keys() []string {
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// GetBool is true only for on, yes or true, in any letter case.
func (c *Config) GetBool(key string) bool {
	v, _ := c.Get(key)
	switch strings.ToLower(v) {
	case "on", "yes", "true":
		return true
	}
	return false
}

// Delete removes a setting.
func (c *Config) Delete(key string) { delete(c.values, norm(key)) }

// Has reports whether a setting exists.
func (c *Config) Has(key string) bool {
	_, ok := c.values[norm(key)]
	return ok
}

// GetDuration reads a time value. A bare number counts as seconds.
func (c *Config) GetDuration(key string) (time.Duration, error) {
	v, ok := c.Get(key)
	if !ok {
		return 0, fmt.Errorf("settings: %s is not set", key)
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return time.ParseDuration(v)
}

// Merge copies other's settings over ours.
func (c *Config) Merge(other *Config) {
	for k, v := range other.values {
		c.values[k] = v
	}
}
`,
		},
		Turns: script(
			local("Add `func (c *Config) Keys() []string`, the keys sorted. And a rule for when we get to typed getters: a boolean setting is true only for on, yes or true, in any letter case. Anything else, 1 included, is false."),
			local("Make keys case-insensitive: Set and Get treat Server.Port and server.port as the same key. That holds for anything that takes a key."),
			cloud("Read deploy/README.txt and tell me what it says about how time values are written. No code changes."),
			cloud("Add `func (c *Config) GetBool(key string) bool`. A missing key is false."),
			cloud("Add `func (c *Config) Delete(key string)`."),
			local("Add `func (c *Config) GetDuration(key string) (time.Duration, error)`. A missing key or an unparsable value is an error."),
			cloud("Add `func (c *Config) Has(key string) bool`."),
			local("Add `func (c *Config) Merge(other *Config)`, which copies other's settings over ours. Other wins on conflicts."),
		),
		Carries: []Carry{
			{
				What:        "booleans are true only for on, yes or true in any case; 1 is false",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3},
				Tests:       []string{"TestGetBoolWords", "TestGetBoolOtherValuesFalse"},
				Absent:      []string{"on, yes", "1 included", "anything else"},
			},
			{
				What:        "anything that takes a key ignores letter case",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{4, 6},
				Tests:       []string{"TestDeleteIgnoresCase", "TestHasIgnoresCase"},
				Absent:      []string{"case-insensitive", "letter case", "server.port"},
			},
			{
				What:        "a bare number in a time value counts as seconds (deploy/README.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{5},
				Source:      "deploy/README.txt",
				Tests:       []string{"TestGetDurationBareNumber"},
				Absent:      []string{"bare number", "seconds", "suffix"},
			},
		},
	}
}
