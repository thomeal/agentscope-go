package s2

func long2B03() Task {
	return Task{
		ID:     "s2-long2-b03-greenhouse-rules",
		Set:    "long",
		System: system("a forward-chaining rules engine for a greenhouse controller"),
		Files: map[string]string{
			"go.mod": goMod,
			"docs/controller.txt": `Greenhouse controller: site notes

Facts the engine sees

Sensor readings are written by the controller before each run and rules
must never set them:

  temp       air temperature, degrees C
  humidity   relative humidity, percent
  light      light level, lux
  hour       hour of the day, 0 to 23

Actuators are what rules are for:

  heater     on or off
  vent       open or closed
  mist       on or off
  fan        speed 0, 1, 2 or 3

Interlocks

Two combinations can damage the house or the crop:

  - the heater must never be on while the vent is open;
  - the misting must never be on while humidity is 85 or more.

A rule whose actions would leave the facts in either combination must not
fire. The engine passes over it and tries the next rule on the agenda, as if
the rule did not match.

Runs

The controller writes a fresh set of readings every minute and then runs the
rules until they settle. Actuator facts carry over from one run to the next,
so a rule only needs to fire when something has to change. The controller
applies the actuator facts to the hardware after the run, not after each
rule, so the facts between two rules never reach the house.

Wiring

The fan relay is on circuit B and the heater on circuit C. Both are fused
separately, so a fan fault does not stop the heater.
`,
			"docs/rule-syntax.txt": `Rule file syntax, version 2

A rule file holds one rule per line. Blank lines, and lines whose first
non-blank character is #, are ignored. Words are separated by one or more
spaces, and the keywords rule, priority, when, and and then are lower case.

  rule NAME [priority N] when CONDITION [and CONDITION ...] then ACTION [and ACTION ...]

NAME is made of lower-case letters, digits and dashes, and starts with a
letter. The priority is optional: an integer, possibly negative, and 0 when
it is left out.

A CONDITION is three words: a fact name, an operator and a value. The
operators are = != < <= > and >=.

An ACTION is three words as well: a fact name, = and the value the fact is
set to.

Fact names and values are single words, and values are kept as written. A
rule needs at least one condition and at least one action, and nothing may
follow its last action.

Examples:

  rule vent-hot priority 5 when temp > 30 and vent = closed then vent = open and fan = 2
  rule heat-night when hour >= 20 and temp < 14 then heater = on

Version 1 files began conditions with "if" instead of "when". They are no
longer accepted and have to be converted.

Style

Name a rule after what it does, not after when it runs: vent-hot rather than
rule-12. Keep a rule to one concern, and leave a comment above a group of
rules that belong together. Growers edit these files by hand, so keep lines
short enough to read without scrolling where you can.
`,
			"facts.go": `package rules

import (
	"fmt"
	"sort"
	"strings"
)

// Facts is what the engine knows: sensor readings such as temp, and the
// state of the actuators such as vent. Every value is kept as text, exactly
// as it was written.
type Facts map[string]string

// Clone returns a copy of f that can be changed without changing f.
func (f Facts) Clone() Facts {
	out := make(Facts, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

// String writes the facts as name=value pairs sorted by name and separated
// by single spaces, like "temp=31 vent=closed".
func (f Facts) String() string {
	names := make([]string, 0, len(f))
	for k := range f {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = k + "=" + f[k]
	}
	return strings.Join(parts, " ")
}

// ParseFacts reads facts written the way String writes them. The pairs may
// come in any order; a name given twice keeps its last value.
func ParseFacts(text string) (Facts, error) {
	f := Facts{}
	for _, pair := range strings.Fields(text) {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || name == "" || value == "" {
			return nil, fmt.Errorf("facts: bad pair %q", pair)
		}
		f[name] = value
	}
	return f, nil
}
`,
			"rule.go": `package rules

// Cond is one condition of a rule: a fact, an operator and a value, such as
// temp > 30. Op is one of = != < <= > >=.
type Cond struct {
	Fact, Op, Value string
}

// Action is one action of a rule: it sets Fact to Value.
type Action struct {
	Fact, Value string
}

// Rule is one rule of the greenhouse controller. When all of its conditions
// hold, it may fire, and firing applies its actions in order.
type Rule struct {
	Name     string
	Priority int
	When     []Cond
	Then     []Action
}

// Uses reports whether one of r's conditions reads fact.
func (r Rule) Uses(fact string) bool {
	for _, c := range r.When {
		if c.Fact == fact {
			return true
		}
	}
	return false
}
`,
			"rules/greenhouse.rules": `# Greenhouse 2, default rules. Edited by the growers; the engine reads this
# file at start-up and again whenever it changes.

# Keep the house from overheating.
rule vent-hot priority 5 when temp > 30 and vent = closed then vent = open and fan = 2
rule fan-up priority 1 when vent = open and fan < 3 and temp > 33 then fan = 3
rule vent-cool when temp < 24 and vent = open then vent = closed and fan = 0

# Nights are cold in spring.
rule heat-night when hour >= 20 and temp < 14 then heater = on
rule heat-off when temp > 18 and heater = on then heater = off

# Seedlings dry out fast under the lamps.
rule mist-dry when humidity < 40 and light > 20000 then mist = on
rule mist-off when humidity > 70 and mist = on then mist = off
`,
			"rules_test.go": `package rules

import "testing"

func TestFactsString(t *testing.T) {
	f := Facts{"vent": "closed", "temp": "31", "fan": "0"}
	if got := f.String(); got != "fan=0 temp=31 vent=closed" {
		t.Fatalf("String = %q", got)
	}
}

func TestParseFactsRoundTrip(t *testing.T) {
	f, err := ParseFacts("temp=31  vent=closed fan=0 temp=29")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.String(); got != "fan=0 temp=29 vent=closed" {
		t.Fatalf("String = %q", got)
	}
	for _, bad := range []string{"temp", "=3", "temp="} {
		if _, err := ParseFacts(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestCloneIsSeparate(t *testing.T) {
	f := Facts{"vent": "closed"}
	g := f.Clone()
	g["vent"] = "open"
	if f["vent"] != "closed" {
		t.Fatal("Clone shares its map")
	}
}

func TestRuleUses(t *testing.T) {
	r := Rule{Name: "vent-hot", When: []Cond{{Fact: "temp", Op: ">", Value: "30"}, {Fact: "vent", Op: "=", Value: "closed"}}}
	if !r.Uses("temp") || !r.Uses("vent") || r.Uses("fan") {
		t.Fatal("wrong")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package rules_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	rules "fixture"
)

func facts(t *testing.T, text string) rules.Facts {
	t.Helper()
	f, err := rules.ParseFacts(text)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func rule(t *testing.T, line string) rules.Rule {
	t.Helper()
	r, err := rules.ParseRule(line)
	if err != nil {
		t.Fatalf("%q: %v", line, err)
	}
	return r
}

func ruleNames(rs []rules.Rule) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestFactsNum(t *testing.T) {
	f := rules.Facts{"temp": "31", "dew": "-2.5", "vent": "open"}
	if n, ok := f.Num("temp"); !ok || n != 31 {
		t.Errorf("temp: %v, %v", n, ok)
	}
	if n, ok := f.Num("dew"); !ok || n != -2.5 {
		t.Errorf("dew: %v, %v", n, ok)
	}
	if _, ok := f.Num("vent"); ok {
		t.Error("vent is a number")
	}
	if _, ok := f.Num("light"); ok {
		t.Error("light is set")
	}
}

func TestCondAndActionStrings(t *testing.T) {
	if got := (rules.Cond{Fact: "temp", Op: ">=", Value: "30"}).String(); got != "temp >= 30" {
		t.Errorf("Cond = %q", got)
	}
	if got := (rules.Action{Fact: "vent", Value: "open"}).String(); got != "vent=open" {
		t.Errorf("Action = %q", got)
	}
}

func TestChanges(t *testing.T) {
	r := rules.Rule{Name: "x", Then: []rules.Action{{Fact: "vent", Value: "open"}, {Fact: "fan", Value: "2"}}}
	if !r.Changes(rules.Facts{"vent": "closed", "fan": "2"}) || r.Changes(rules.Facts{"vent": "open", "fan": "2"}) {
		t.Error("wrong for set facts")
	}
	if !r.Changes(rules.Facts{"vent": "open"}) {
		t.Error("a fact not yet set counts as a change")
	}
}

func TestApply(t *testing.T) {
	f := rules.Facts{"vent": "open", "fan": "1"}
	got := f.Apply([]rules.Action{{Fact: "vent", Value: "open"}, {Fact: "mist", Value: "on"}, {Fact: "fan", Value: "2"}})
	if !slices.Equal(got, []string{"mist", "fan"}) || f.String() != "fan=2 mist=on vent=open" {
		t.Fatalf("changed %v, facts %s", got, f)
	}
}

func TestRuleString(t *testing.T) {
	r := rules.Rule{
		Name: "vent-hot", Priority: 5,
		When: []rules.Cond{{Fact: "temp", Op: ">", Value: "30"}, {Fact: "vent", Op: "=", Value: "closed"}},
		Then: []rules.Action{{Fact: "vent", Value: "open"}, {Fact: "fan", Value: "2"}},
	}
	want := "rule vent-hot priority 5 when temp > 30 and vent = closed then vent = open and fan = 2"
	if got := r.String(); got != want {
		t.Errorf("String = %q", got)
	}
	r.Priority = 0
	want = "rule vent-hot when temp > 30 and vent = closed then vent = open and fan = 2"
	if got := r.String(); got != want {
		t.Errorf("String = %q", got)
	}
}

func TestSets(t *testing.T) {
	r := rules.Rule{Name: "x", Then: []rules.Action{{Fact: "vent", Value: "open"}}}
	if !r.Sets("vent") || r.Sets("fan") {
		t.Fatal("wrong")
	}
}

func holds(t *testing.T, f rules.Facts, fact, op, value string) bool {
	t.Helper()
	return rules.Cond{Fact: fact, Op: op, Value: value}.Holds(f)
}

func TestHoldsNumbers(t *testing.T) {
	f := rules.Facts{"temp": "31", "hour": "9"}
	if !holds(t, f, "temp", ">", "30") || !holds(t, f, "temp", ">=", "31.0") || !holds(t, f, "temp", "=", "31.0") {
		t.Error("31 compared wrongly")
	}
	if holds(t, f, "hour", ">", "10") || !holds(t, f, "hour", "<", "10") || holds(t, f, "temp", "!=", "31") {
		t.Error("compared as text")
	}
}

func TestHoldsText(t *testing.T) {
	f := rules.Facts{"vent": "closed"}
	if !holds(t, f, "vent", "=", "closed") || !holds(t, f, "vent", "!=", "open") || holds(t, f, "vent", "=", "Closed") {
		t.Error("equality on text")
	}
	if holds(t, f, "vent", "<", "zzz") || holds(t, f, "vent", ">=", "a") {
		t.Error("ordering on text")
	}
}

func TestHoldsMixed(t *testing.T) {
	f := rules.Facts{"temp": "hot"}
	if holds(t, f, "temp", ">", "30") || holds(t, f, "temp", "<", "30") || !holds(t, f, "temp", "!=", "30") {
		t.Error("a number against text")
	}
}

func TestHoldsMissing(t *testing.T) {
	f := rules.Facts{"temp": "20"}
	for _, op := range []string{"=", "!=", "<", ">="} {
		if holds(t, f, "vent", op, "open") || holds(t, f, "light", op, "5") {
			t.Errorf("%s holds on a fact that is not set", op)
		}
	}
}

func TestMatches(t *testing.T) {
	r := rule(t, "rule vent-hot when temp > 30 and vent = closed then vent = open")
	if !r.Matches(facts(t, "temp=31 vent=closed")) || r.Matches(facts(t, "temp=29 vent=closed")) {
		t.Fatal("wrong")
	}
}

func TestParseRule(t *testing.T) {
	r := rule(t, "rule vent-hot priority 5 when temp > 30 and vent = closed then vent = open and fan = 2")
	want := rules.Rule{
		Name: "vent-hot", Priority: 5,
		When: []rules.Cond{{Fact: "temp", Op: ">", Value: "30"}, {Fact: "vent", Op: "=", Value: "closed"}},
		Then: []rules.Action{{Fact: "vent", Value: "open"}, {Fact: "fan", Value: "2"}},
	}
	if r.Name != want.Name || r.Priority != 5 || !slices.Equal(r.When, want.When) || !slices.Equal(r.Then, want.Then) {
		t.Fatalf("got %+v", r)
	}
}

func TestParseRuleDefaultsAndSpacing(t *testing.T) {
	r := rule(t, "  rule heat-night   when hour >= 20 and  temp < 14 then heater = on")
	if r.Priority != 0 || len(r.When) != 2 || r.Then[0] != (rules.Action{Fact: "heater", Value: "on"}) {
		t.Fatalf("got %+v", r)
	}
	if r := rule(t, "rule cool2 priority -3 when temp > 35 then fan = 3"); r.Priority != -3 {
		t.Fatalf("priority %d", r.Priority)
	}
}

func TestParseRuleRejects(t *testing.T) {
	for _, line := range []string{
		"rule x if a = 1 then b = 2",
		"rule x when a = 1",
		"rule x when then b = 2",
		"rule Vent when a = 1 then b = 2",
		"rule x when a ~ 1 then b = 2",
		"rule x when a = 1 then b > 2",
		"rule x when a = 1 then b = 2 and",
		"# rule x when a = 1 then b = 2",
	} {
		if _, err := rules.ParseRule(line); !errors.Is(err, rules.ErrSyntax) {
			t.Errorf("%q: %v", line, err)
		}
	}
}

const ruleFile = "# greenhouse defaults\n\n" +
	"rule vent-hot priority 5 when temp > 30 and vent = closed then vent = open and fan = 2\n" +
	"   # night\n" +
	"rule heat-night when hour >= 20 and temp < 14 then heater = on\n"

func TestParseRules(t *testing.T) {
	rs, err := rules.ParseRules(ruleFile)
	if err != nil || !slices.Equal(ruleNames(rs), []string{"vent-hot", "heat-night"}) || rs[0].Priority != 5 {
		t.Fatalf("%v, %v", ruleNames(rs), err)
	}
}

func TestParseRulesNamesTheLine(t *testing.T) {
	_, err := rules.ParseRules("rule a when x = 1 then y = 2\n\nrule b when x = 1\n")
	if !errors.Is(err, rules.ErrSyntax) || !strings.HasPrefix(err.Error(), "line 3: ") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewEngine(t *testing.T) {
	e, err := rules.NewEngine(ruleFile)
	if err != nil || len(e.Rules) != 2 || e.MaxCycles != 0 || len(e.Trace) != 0 {
		t.Fatalf("%+v, %v", e, err)
	}
	if _, err := rules.NewEngine("rule x\n"); !errors.Is(err, rules.ErrSyntax) {
		t.Fatalf("err = %v", err)
	}
}

func TestAgendaPriority(t *testing.T) {
	rs := []rules.Rule{
		rule(t, "rule low when temp > 20 then fan = 1"),
		rule(t, "rule high priority 4 when temp > 20 then fan = 3"),
		rule(t, "rule mid priority 2 when temp > 20 then mist = on"),
	}
	if got := ruleNames(rules.Agenda(rs, facts(t, "temp=25"))); !slices.Equal(got, []string{"high", "mid", "low"}) {
		t.Fatalf("Agenda = %v", got)
	}
}

func TestAgendaTies(t *testing.T) {
	rs := []rules.Rule{
		rule(t, "rule first priority 2 when temp > 20 then fan = 1"),
		rule(t, "rule second priority 2 when temp > 20 then fan = 2"),
		rule(t, "rule third priority 2 when temp > 20 then fan = 3"),
	}
	if got := ruleNames(rules.Agenda(rs, facts(t, "temp=25"))); !slices.Equal(got, []string{"third", "second", "first"}) {
		t.Fatalf("Agenda = %v", got)
	}
}

func TestAgendaLeavesOut(t *testing.T) {
	rs := []rules.Rule{
		rule(t, "rule done when temp > 20 then fan = 1"),
		rule(t, "rule cold when temp < 10 then heater = on"),
		rule(t, "rule mist when temp > 20 then mist = on"),
	}
	if got := ruleNames(rules.Agenda(rs, facts(t, "temp=25 fan=1"))); !slices.Equal(got, []string{"mist"}) {
		t.Fatalf("Agenda = %v", got)
	}
}

func TestOrder(t *testing.T) {
	rs := []rules.Rule{
		rule(t, "rule a when x = 1 then y = 1"),
		rule(t, "rule b priority 5 when x = 1 then y = 2"),
		rule(t, "rule c when x = 1 then y = 3"),
		rule(t, "rule d priority 5 when x = 1 then y = 4"),
	}
	if got := ruleNames(rules.Order(rs)); !slices.Equal(got, []string{"d", "b", "c", "a"}) {
		t.Fatalf("Order = %v", got)
	}
	if rs[0].Name != "a" || rs[3].Name != "d" {
		t.Error("Order changed its argument")
	}
}

func TestAllowedHeaterAndVent(t *testing.T) {
	heat := rule(t, "rule heat when temp < 14 then heater = on")
	if rules.Allowed(facts(t, "temp=12 vent=open heater=off"), heat) {
		t.Error("heater on with the vent open")
	}
	if !rules.Allowed(facts(t, "temp=12 vent=closed heater=off"), heat) {
		t.Error("heater on with the vent closed")
	}
	both := rule(t, "rule both when temp < 14 then vent = closed and heater = on")
	if !rules.Allowed(facts(t, "temp=12 vent=open heater=off"), both) {
		t.Error("the rule closes the vent itself")
	}
}

func TestAllowedMist(t *testing.T) {
	mist := rule(t, "rule mist when temp > 28 then mist = on")
	if rules.Allowed(facts(t, "temp=30 humidity=85"), mist) {
		t.Error("misting at 85")
	}
	if !rules.Allowed(facts(t, "temp=30 humidity=84.5"), mist) {
		t.Error("no misting at 84.5")
	}
}

func TestStep(t *testing.T) {
	e, err := rules.NewEngine("rule vent-hot priority 5 when temp > 30 and vent = closed then vent = open and fan = 2\n")
	if err != nil {
		t.Fatal(err)
	}
	e.Trace = []string{"1 earlier: fan=0"}
	f := facts(t, "temp=31 vent=closed")
	name, ok := e.Step(f)
	if !ok || name != "vent-hot" || f.String() != "fan=2 temp=31 vent=open" {
		t.Fatalf("%q, %v, facts %s", name, ok, f)
	}
	if !slices.Equal(e.Trace, []string{"1 earlier: fan=0", "2 vent-hot: vent=open fan=2"}) {
		t.Fatalf("trace %q", e.Trace)
	}
}

func TestStepPassesOverForbidden(t *testing.T) {
	e, err := rules.NewEngine("rule heat priority 9 when temp < 14 then heater = on\nrule close when temp < 14 then vent = closed\n")
	if err != nil {
		t.Fatal(err)
	}
	f := facts(t, "temp=10 vent=open heater=off")
	if name, ok := e.Step(f); !ok || name != "close" || f["heater"] != "off" {
		t.Fatalf("%q, %v, facts %s", name, ok, f)
	}
}

func TestStepNothing(t *testing.T) {
	e, err := rules.NewEngine("rule heat when temp < 14 then heater = on\n")
	if err != nil {
		t.Fatal(err)
	}
	if name, ok := e.Step(facts(t, "temp=20")); ok || len(e.Trace) != 0 {
		t.Fatalf("%q, %v", name, ok)
	}
}

const cycleRules = "rule vent-hot when temp > 30 and vent = closed then vent = open and fan = 2\n" +
	"rule fan-up priority 1 when vent = open and fan < 3 then fan = 3\n" +
	"rule mist-dry when humidity < 40 then mist = on\n"

func TestExplain(t *testing.T) {
	e, err := rules.NewEngine(cycleRules)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(facts(t, "temp=33 vent=closed humidity=30")); err != nil {
		t.Fatal(err)
	}
	if got := e.Explain("fan"); !slices.Equal(got, []string{"vent-hot", "fan-up"}) {
		t.Errorf("fan: %v", got)
	}
	if got := e.Explain("mist"); !slices.Equal(got, []string{"mist-dry"}) {
		t.Errorf("mist: %v", got)
	}
}

func TestRunSettles(t *testing.T) {
	e, err := rules.NewEngine(cycleRules)
	if err != nil {
		t.Fatal(err)
	}
	f := facts(t, "temp=33 vent=closed humidity=30")
	if n, err := e.Run(f); n != 3 || err != nil || f.String() != "fan=3 humidity=30 mist=on temp=33 vent=open" {
		t.Fatalf("%d, %v, facts %s", n, err, f)
	}
	if n, err := e.Run(f); n != 0 || err != nil || len(e.Trace) != 3 {
		t.Fatalf("second run: %d, %v", n, err)
	}
}

const flipRules = "rule on when lamp = off then lamp = on\nrule off when lamp = on then lamp = off\n"

func TestRunLoop(t *testing.T) {
	e, err := rules.NewEngine(flipRules)
	if err != nil {
		t.Fatal(err)
	}
	n, err := e.Run(facts(t, "lamp=off"))
	if !errors.Is(err, rules.ErrLoop) || n != 50 || len(e.Trace) != 50 {
		t.Fatalf("%d, %v, %d traced", n, err, len(e.Trace))
	}
}

func TestRunMaxCycles(t *testing.T) {
	e, err := rules.NewEngine(flipRules)
	if err != nil {
		t.Fatal(err)
	}
	e.MaxCycles = 3
	if n, err := e.Run(facts(t, "lamp=off")); !errors.Is(err, rules.ErrLoop) || n != 3 || len(e.Trace) != 3 {
		t.Fatalf("%d, %v", n, err)
	}
	once, err := rules.NewEngine("rule on when lamp = off then lamp = on\n")
	if err != nil {
		t.Fatal(err)
	}
	once.MaxCycles = 1
	if n, err := once.Run(facts(t, "lamp=off")); err != nil || n != 1 {
		t.Fatalf("settled at the limit: %d, %v", n, err)
	}
}

func TestCheck(t *testing.T) {
	rs := []rules.Rule{
		rule(t, "rule a when x = 1 then temp = 5 and vent = open"),
		rule(t, "rule b when x = 1 then fan = 1"),
		rule(t, "rule a when x = 2 then humidity = 50 and hour = 3"),
		rule(t, "rule c when x = 3 then light = 0"),
	}
	want := []string{
		"rule a sets reading temp",
		"rule a sets reading humidity",
		"rule a sets reading hour",
		"rule a is defined twice",
		"rule c sets reading light",
	}
	if got := rules.Check(rs); !slices.Equal(got, want) {
		t.Fatalf("Check = %q", got)
	}
}

func TestSettle(t *testing.T) {
	e, err := rules.NewEngine("rule open when temp > 30 then vent = open\nrule shut when temp < 25 then vent = closed\n")
	if err != nil {
		t.Fatal(err)
	}
	state := facts(t, "vent=closed")
	got, err := e.Settle(state, []rules.Facts{{"temp": "31"}, {"temp": "20"}})
	if err != nil || len(got) != 2 || got[0].String() != "temp=31 vent=open" || got[1].String() != "temp=20 vent=closed" {
		t.Fatalf("%v, %v", got, err)
	}
}

func TestSettleStopsAtError(t *testing.T) {
	e, err := rules.NewEngine(flipRules)
	if err != nil {
		t.Fatal(err)
	}
	e.MaxCycles = 4
	got, err := e.Settle(rules.Facts{}, []rules.Facts{{"lamp": "dim"}, {"lamp": "off"}, {"lamp": "dim"}})
	if !errors.Is(err, rules.ErrLoop) || len(got) != 1 || got[0]["lamp"] != "dim" {
		t.Fatalf("%v, %v", got, err)
	}
}

func TestChanged(t *testing.T) {
	f := rules.Facts{"a": "1", "b": "2", "c": "3"}
	g := rules.Facts{"a": "1", "b": "5", "d": "4"}
	if got := f.Changed(g); !slices.Equal(got, []string{"b", "c", "d"}) {
		t.Fatalf("Changed = %v", got)
	}
}

func TestNext(t *testing.T) {
	e, err := rules.NewEngine(cycleRules)
	if err != nil {
		t.Fatal(err)
	}
	f := facts(t, "temp=33 vent=closed humidity=50")
	if name, ok := e.Next(f); !ok || name != "vent-hot" || f["vent"] != "closed" || len(e.Trace) != 0 {
		t.Fatalf("%q, %v", name, ok)
	}
}

func TestSummary(t *testing.T) {
	e, err := rules.NewEngine(cycleRules)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(facts(t, "temp=33 vent=closed humidity=30")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(facts(t, "temp=35 vent=closed fan=0 humidity=60")); err != nil {
		t.Fatal(err)
	}
	if got := e.Summary(); !slices.Equal(got, []string{"fan-up 2", "vent-hot 2", "mist-dry 1"}) {
		t.Fatalf("Summary = %q", got)
	}
}

func TestLoad(t *testing.T) {
	e, err := rules.NewEngine("rule a when x = 1 then y = 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Load("# site\nrule b when x = 1 then y = 2\nrule c when x = 1 then y = 3\n"); err != nil {
		t.Fatal(err)
	}
	if got := ruleNames(e.Rules); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("rules %v", got)
	}
	if err := e.Load("rule d when x = 1 then y = 4\nrule bad\n"); !errors.Is(err, rules.ErrSyntax) || len(e.Rules) != 3 {
		t.Fatalf("err %v, %d rules", err, len(e.Rules))
	}
}

func TestDiff(t *testing.T) {
	before := []rules.Rule{
		rule(t, "rule a when x = 1 then y = 1"),
		rule(t, "rule b when x = 1 then y = 2"),
		rule(t, "rule c when x = 1 then y = 3"),
	}
	after := []rules.Rule{
		rule(t, "rule d when x = 1 then y = 4"),
		rule(t, "rule c when x = 2 then y = 3"),
		rule(t, "rule b priority 3 when x = 1 then y = 2"),
		rule(t, "rule c when x = 1 then y = 3"),
	}
	if got := rules.Diff(before, after); !slices.Equal(got, []string{"- a", "~ b", "+ d"}) {
		t.Fatalf("Diff = %q", got)
	}
}
`},
		Solution: map[string]string{
			"agenda.go": `package rules

import (
	"cmp"
	"slices"
)

// Matches reports whether every one of r's conditions holds on f.
func (r Rule) Matches(f Facts) bool {
	for _, c := range r.When {
		if !c.Holds(f) {
			return false
		}
	}
	return true
}

// Changes reports whether firing r on f would change any fact, giving a
// value to a fact f does not have included.
func (r Rule) Changes(f Facts) bool {
	for _, a := range r.Then {
		if v, ok := f[a.Fact]; !ok || v != a.Value {
			return true
		}
	}
	return false
}

// byPriority puts higher priorities first.
func byPriority(a, b Rule) int { return cmp.Compare(b.Priority, a.Priority) }

// Order returns a copy of rules, given in rule-file order, in the order they
// would get to fire if all of them could: the highest priority first, and
// between equal priorities the one later in the file.
func Order(rules []Rule) []Rule {
	out := slices.Clone(rules)
	slices.Reverse(out)
	slices.SortStableFunc(out, byPriority)
	return out
}

// Agenda returns the rules that match f and would change something by
// firing, in the order they get to fire.
func Agenda(rules []Rule, f Facts) []Rule {
	var out []Rule
	for _, r := range Order(rules) {
		if r.Matches(f) && r.Changes(f) {
			out = append(out, r)
		}
	}
	return out
}

// Allowed reports whether r may fire on f: its actions must not leave the
// facts breaking an interlock of docs/controller.txt.
func Allowed(f Facts, r Rule) bool {
	after := f.Clone()
	after.Apply(r.Then)
	if after["heater"] == "on" && after["vent"] == "open" {
		return false
	}
	if h, ok := after.Num("humidity"); ok && h >= 85 && after["mist"] == "on" {
		return false
	}
	return true
}
`,
			"check.go": `package rules

import "sort"

// readings are the facts written by the controller's sensors, which rules
// must never set (docs/controller.txt).
var readings = map[string]bool{"temp": true, "humidity": true, "light": true, "hour": true}

// Check returns the problems in a rule set, rule by rule in order: each
// action that sets a reading, then a rule whose name an earlier rule has.
func Check(rules []Rule) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range rules {
		for _, a := range r.Then {
			if readings[a.Fact] {
				out = append(out, "rule "+r.Name+" sets reading "+a.Fact)
			}
		}
		if seen[r.Name] {
			out = append(out, "rule "+r.Name+" is defined twice")
		}
		seen[r.Name] = true
	}
	return out
}

// byName indexes rules by name; the last rule with a name counts.
func byName(rules []Rule) map[string]Rule {
	out := make(map[string]Rule, len(rules))
	for _, r := range rules {
		out[r.Name] = r
	}
	return out
}

// Diff compares two rule sets by name, for reviewing edits to a rule file:
// "+ name" for a rule only in after, "- name" for one only in before, and
// "~ name" for one in both whose String differs, sorted by name.
func Diff(before, after []Rule) []string {
	old, cur := byName(before), byName(after)
	var names []string
	for n := range old {
		names = append(names, n)
	}
	for n := range cur {
		if _, ok := old[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		b, inOld := old[n]
		a, inCur := cur[n]
		switch {
		case !inOld:
			out = append(out, "+ "+n)
		case !inCur:
			out = append(out, "- "+n)
		case a.String() != b.String():
			out = append(out, "~ "+n)
		}
	}
	return out
}
`,
			"cond.go": `package rules

import (
	"cmp"
	"strconv"
	"strings"
)

// Cond is one condition of a rule: a fact, an operator and a value, such as
// temp > 30. Op is one of = != < <= > >=.
type Cond struct {
	Fact, Op, Value string
}

// Action is one action of a rule: it sets Fact to Value.
type Action struct {
	Fact, Value string
}

// String writes the condition as its three parts, like "temp > 30".
func (c Cond) String() string { return c.Fact + " " + c.Op + " " + c.Value }

// String writes the action like "vent=open".
func (a Action) String() string { return a.Fact + "=" + a.Value }

// Holds reports whether the condition is true of f. A condition on a fact
// f does not have is false, whatever its operator. When both values are
// numbers they compare as numbers; otherwise only = and != compare, on the
// text, and every other operator is false.
func (c Cond) Holds(f Facts) bool {
	v, ok := f[c.Fact]
	if !ok {
		return false
	}
	if x, y, ok := numbers(v, c.Value); ok {
		return holds(c.Op, cmp.Compare(x, y))
	}
	if c.Op == "=" || c.Op == "!=" {
		return holds(c.Op, strings.Compare(v, c.Value))
	}
	return false
}

// numbers reads both values as numbers, if they both are.
func numbers(a, b string) (x, y float64, ok bool) {
	x, errX := strconv.ParseFloat(a, 64)
	y, errY := strconv.ParseFloat(b, 64)
	return x, y, errX == nil && errY == nil
}

// holds reports whether an operator holds for the result of a comparison.
func holds(op string, n int) bool {
	switch op {
	case "=":
		return n == 0
	case "!=":
		return n != 0
	case "<":
		return n < 0
	case "<=":
		return n <= 0
	case ">":
		return n > 0
	case ">=":
		return n >= 0
	}
	return false
}
`,
			"engine.go": `package rules

import (
	"errors"
	"strconv"
	"strings"
)

// ErrLoop is returned by Run when the rules keep firing.
var ErrLoop = errors.New("rules did not settle")

// Engine runs a rule set over facts. Trace has one line for every rule fired,
// oldest first.
type Engine struct {
	Rules []Rule
	// MaxCycles is how many rules a run may fire; 0 means defaultCycles.
	MaxCycles int
	Trace     []string
}

// defaultCycles is the limit of a run when MaxCycles is 0.
const defaultCycles = 50

// NewEngine returns an engine for the rules of a rule file.
func NewEngine(text string) (*Engine, error) {
	rules, err := ParseRules(text)
	if err != nil {
		return nil, err
	}
	return &Engine{Rules: rules}, nil
}

// pick returns the first rule on the agenda that is allowed to fire.
func (e *Engine) pick(f Facts) (Rule, bool) {
	for _, r := range Agenda(e.Rules, f) {
		if Allowed(f, r) {
			return r, true
		}
	}
	return Rule{}, false
}

// Next returns the name of the rule Step would fire on f, without firing it.
func (e *Engine) Next(f Facts) (string, bool) {
	r, ok := e.pick(f)
	return r.Name, ok
}

// Step fires the first allowed rule on the agenda, applying its actions to f
// and adding its trace line, and returns its name. It returns false when no
// rule can fire.
func (e *Engine) Step(f Facts) (string, bool) {
	r, ok := e.pick(f)
	if !ok {
		return "", false
	}
	f.Apply(r.Then)
	e.Trace = append(e.Trace, traceLine(len(e.Trace)+1, r))
	return r.Name, true
}

// traceLine is the trace line for the n-th firing, counted over the whole
// trace: "3 vent-hot: vent=open fan=2".
func traceLine(n int, r Rule) string {
	parts := []string{strconv.Itoa(n), r.Name + ":"}
	for _, a := range r.Then {
		parts = append(parts, a.String())
	}
	return strings.Join(parts, " ")
}

// Run fires rules until none can and returns how many fired in this run.
// After MaxCycles firings, or defaultCycles when it is 0, it stops with
// ErrLoop if another rule could still fire.
func (e *Engine) Run(f Facts) (int, error) {
	limit := e.MaxCycles
	if limit == 0 {
		limit = defaultCycles
	}
	for fired := 0; ; fired++ {
		if fired == limit {
			if _, more := e.Next(f); more {
				return fired, ErrLoop
			}
			return fired, nil
		}
		if _, ok := e.Step(f); !ok {
			return fired, nil
		}
	}
}

// Settle copies each set of readings into state in turn, runs the rules and
// keeps a copy of state. At the first error it stops and returns the copies
// made so far with the error.
func (e *Engine) Settle(state Facts, readings []Facts) ([]Facts, error) {
	var out []Facts
	for _, r := range readings {
		for k, v := range r {
			state[k] = v
		}
		if _, err := e.Run(state); err != nil {
			return out, err
		}
		out = append(out, state.Clone())
	}
	return out, nil
}

// Load parses another rule file and appends its rules after e.Rules, as if
// that file came after the first one. On a parse error e.Rules is left as
// it was.
func (e *Engine) Load(text string) error {
	more, err := ParseRules(text)
	if err != nil {
		return err
	}
	e.Rules = append(e.Rules, more...)
	return nil
}
`,
			"facts.go": `package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Facts is what the engine knows: sensor readings such as temp, and the
// state of the actuators such as vent. Every value is kept as text, exactly
// as it was written.
type Facts map[string]string

// Clone returns a copy of f that can be changed without changing f.
func (f Facts) Clone() Facts {
	out := make(Facts, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

// String writes the facts as name=value pairs sorted by name and separated
// by single spaces, like "temp=31 vent=closed".
func (f Facts) String() string {
	names := make([]string, 0, len(f))
	for k := range f {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = k + "=" + f[k]
	}
	return strings.Join(parts, " ")
}

// ParseFacts reads facts written the way String writes them. The pairs may
// come in any order; a name given twice keeps its last value.
func ParseFacts(text string) (Facts, error) {
	f := Facts{}
	for _, pair := range strings.Fields(text) {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || name == "" || value == "" {
			return nil, fmt.Errorf("facts: bad pair %q", pair)
		}
		f[name] = value
	}
	return f, nil
}

// Num returns a fact read as a number, and false when it is unset or not a
// number.
func (f Facts) Num(name string) (float64, bool) {
	v, ok := f[name]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseFloat(v, 64)
	return n, err == nil
}

// Apply sets the fact of each action in turn and returns the names of the
// facts that changed, in action order.
func (f Facts) Apply(acts []Action) []string {
	var changed []string
	for _, a := range acts {
		if old, ok := f[a.Fact]; !ok || old != a.Value {
			f[a.Fact] = a.Value
			changed = append(changed, a.Fact)
		}
	}
	return changed
}

// Changed returns the sorted names of the facts that differ between f and
// g, a fact set in only one of them included.
func (f Facts) Changed(g Facts) []string {
	var names []string
	for k, v := range f {
		if w, ok := g[k]; !ok || w != v {
			names = append(names, k)
		}
	}
	for k := range g {
		if _, ok := f[k]; !ok {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return names
}
`,
			"parse.go": `package rules

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ErrSyntax is wrapped by every error about a line that is not a rule.
var ErrSyntax = errors.New("rule syntax")

// operators are the operators a condition may use.
var operators = map[string]bool{"=": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

// ParseRule reads one rule from a line of the rule syntax of
// docs/rule-syntax.txt.
func ParseRule(line string) (Rule, error) {
	w := strings.Fields(line)
	if len(w) < 2 || w[0] != "rule" || !validName(w[1]) {
		return Rule{}, fmt.Errorf("%w: %q is not a rule", ErrSyntax, line)
	}
	r := Rule{Name: w[1]}
	w = w[2:]
	if len(w) >= 2 && w[0] == "priority" {
		p, err := strconv.Atoi(w[1])
		if err != nil {
			return Rule{}, fmt.Errorf("%w: bad priority %q", ErrSyntax, w[1])
		}
		r.Priority, w = p, w[2:]
	}
	then := slices.Index(w, "then")
	if len(w) == 0 || w[0] != "when" || then < 0 {
		return Rule{}, fmt.Errorf("%w: rule %s needs when and then", ErrSyntax, r.Name)
	}
	conds, errWhen := triples(w[1:then], false)
	acts, errThen := triples(w[then+1:], true)
	if errWhen != nil || errThen != nil {
		return Rule{}, fmt.Errorf("%w: rule %s has a bad condition or action", ErrSyntax, r.Name)
	}
	r.When = conds
	for _, a := range acts {
		r.Then = append(r.Then, Action{Fact: a.Fact, Value: a.Value})
	}
	return r, nil
}

// triples reads FACT OP VALUE groups joined by "and"; there has to be at
// least one. Actions allow only =.
func triples(w []string, actions bool) ([]Cond, error) {
	var out []Cond
	for {
		if len(w) < 3 {
			return nil, ErrSyntax
		}
		c := Cond{Fact: w[0], Op: w[1], Value: w[2]}
		if !operators[c.Op] || actions && c.Op != "=" {
			return nil, ErrSyntax
		}
		out = append(out, c)
		if w = w[3:]; len(w) == 0 {
			return out, nil
		}
		if w[0] != "and" {
			return nil, ErrSyntax
		}
		w = w[1:]
	}
}

// validName reports whether s is a rule name: lower-case letters, digits
// and dashes, starting with a letter.
func validName(s string) bool {
	for i, c := range s {
		if !(c >= 'a' && c <= 'z' || i > 0 && (c >= '0' && c <= '9' || c == '-')) {
			return false
		}
	}
	return s != ""
}

// ParseRules reads every rule of a rule file, in file order. An error names
// the line it is about.
func ParseRules(text string) ([]Rule, error) {
	var out []Rule
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		r, err := ParseRule(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// String writes r back as one line of the rule syntax, with single spaces
// and no priority when it is 0.
func (r Rule) String() string {
	w := []string{"rule", r.Name}
	if r.Priority != 0 {
		w = append(w, "priority", strconv.Itoa(r.Priority))
	}
	w = append(w, "when")
	for i, c := range r.When {
		if i > 0 {
			w = append(w, "and")
		}
		w = append(w, c.String())
	}
	w = append(w, "then")
	for i, a := range r.Then {
		if i > 0 {
			w = append(w, "and")
		}
		w = append(w, a.Fact, "=", a.Value)
	}
	return strings.Join(w, " ")
}
`,
			"report.go": `package rules

import (
	"sort"
	"strconv"
	"strings"
)

// fired splits a trace line into the name of the rule that fired and its
// actions as fact=value.
func fired(line string) (name string, acts []string) {
	w := strings.Fields(line)
	if len(w) < 2 {
		return "", nil
	}
	return strings.TrimSuffix(w[1], ":"), w[2:]
}

// Explain returns, from the trace, the names of the rules whose firing set
// fact, one per firing, oldest first.
func (e *Engine) Explain(fact string) []string {
	var names []string
	for _, line := range e.Trace {
		name, acts := fired(line)
		for _, a := range acts {
			if strings.HasPrefix(a, fact+"=") {
				names = append(names, name)
				break
			}
		}
	}
	return names
}

// Summary returns one line per rule that has fired, its name and how many
// times the trace shows it firing, like "vent-hot 3": most firings first,
// then by name.
func (e *Engine) Summary() []string {
	counts := map[string]int{}
	for _, line := range e.Trace {
		if name, _ := fired(line); name != "" {
			counts[name]++
		}
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + " " + strconv.Itoa(counts[n])
	}
	return out
}
`,
			"rule.go": `package rules

// Rule is one rule of the greenhouse controller. When all of its conditions
// hold, it may fire, and firing applies its actions in order.
type Rule struct {
	Name     string
	Priority int
	When     []Cond
	Then     []Action
}

// Sets reports whether one of r's actions sets fact.
func (r Rule) Sets(fact string) bool {
	for _, a := range r.Then {
		if a.Fact == fact {
			return true
		}
	}
	return false
}

// Uses reports whether one of r's conditions reads fact.
func (r Rule) Uses(fact string) bool {
	for _, c := range r.When {
		if c.Fact == fact {
			return true
		}
	}
	return false
}
`,
		},
		Turns: script(
			cloud("How conditions compare, for when we write them: when both the fact's value and the condition's value read as numbers, compare them as numbers; otherwise only = and != work, on the exact text, and the ordering operators are false. A condition on a fact that isn't set is false whatever its operator, != included. For now add `func (f Facts) Num(name string) (float64, bool)` to facts.go: the fact read as a number, false when it is unset or not a number."),
			local("Read docs/rule-syntax.txt and tell me whether Rule, Cond and Action can hold everything it describes. No code changes."),
			local("Add `func (c Cond) String() string` and `func (a Action) String() string` to rule.go: a Cond reads like \"temp > 30\", its three parts with single spaces, and an Action like \"vent=open\"."),
			cloud("Add `func (r Rule) Changes(f Facts) bool` to rule.go: whether firing r on f would change any fact, which includes giving a value to a fact f doesn't have yet."),
			cloud("When the engine fires a rule it will log one trace line: the number of the firing, counted from 1 over the engine's whole trace, then the rule's name and a colon, then every action as fact=value, all with single spaces, like \"3 vent-hot: vent=open fan=2\". For now add `func (f Facts) Apply(acts []Action) []string` to rule.go: it sets each action's fact and returns the names of the facts that changed, in action order."),
			local("Add `func (r Rule) String() string` to rule.go: r written back as one line of the rule syntax, with single spaces and no priority when it is 0. And a rule for the agenda later: when several rules can fire, the one with the highest Priority goes first, and between equal priorities the one that comes first in the rule file."),
			local("Add `func (r Rule) Sets(fact string) bool` to rule.go: whether one of r's actions sets fact."),
			cloud("Read docs/controller.txt and tell me what it means for the engine. No code changes."),
			cloud("Tidy-up: move Apply into facts.go next to Num, and Cond and Action, with their String methods, into a new cond.go. No behaviour changes."),
			local("Add `func (c Cond) Holds(f Facts) bool` to cond.go: whether the condition is true of f, under our comparison rules."),
			local("Add `func (r Rule) Matches(f Facts) bool` to rule.go: whether every one of r's conditions holds on f."),
			cloud("Add `func ParseRule(line string) (Rule, error)` in a new parse.go: one rule from a line in the rule syntax. Anything else gives an error wrapping `var ErrSyntax = errors.New(\"rule syntax\")`, which you add there too."),
			cloud("Add `func ParseRules(text string) ([]Rule, error)` to parse.go: every rule in a rule file, in file order. An error says which line it is about, starting \"line 3: \", and still wraps ErrSyntax."),
			local("Add `type Engine struct { Rules []Rule; MaxCycles int; Trace []string }` and `var ErrLoop = errors.New(\"rules did not settle\")` in a new engine.go. A MaxCycles of 0 means 50; a run will stop with ErrLoop once that many rules have fired in it and another one still could."),
			local("Add `func NewEngine(text string) (*Engine, error)` to engine.go: an engine for the rules in text, which is a rule file. Parse errors come back unchanged."),
			cloud("Add `func Agenda(rules []Rule, f Facts) []Rule` in a new agenda.go: the rules that match f and would change something by firing, in the order they get to fire. rules is in rule-file order."),
			cloud("Tidy-up: move Changes and Matches from rule.go into agenda.go next to Agenda, and Rule.String into parse.go next to the parser, so that each file does one job. No behaviour changes."),
			local("Add `func Allowed(f Facts, r Rule) bool` to agenda.go: whether r may fire on f under the site's rules for the controller."),
			local("Change of plan for ties: when two rules have the same Priority, the one later in the rule file now wins, so that site rules appended after the defaults override them. Bring the code in line."),
			local("Add `func (e *Engine) Step(f Facts) (string, bool)` to engine.go: it fires the first rule on the agenda for e.Rules that Allowed() lets fire, applying its actions to f and adding its trace line, and returns the rule's name; false if no rule can fire."),
			local("Add `func (e *Engine) Explain(fact string) []string` in a new report.go: from the trace, the names of the rules whose firing set fact, one per firing, oldest first."),
			cloud("Add `func (e *Engine) Run(f Facts) (int, error)` to engine.go: it fires rules with Step until none can fire and returns how many fired in this run, or stops with ErrLoop when the rules don't settle."),
			local("Add `func Check(rules []Rule) []string` in a new check.go: the problems in a rule set, rule by rule in order: \"rule NAME sets reading FACT\" for each action that sets one of the sensor readings, then \"rule NAME is defined twice\" if an earlier rule has the same name."),
			cloud("Add `func (e *Engine) Settle(state Facts, readings []Facts) ([]Facts, error)` to engine.go: for each set of readings in turn it copies them into state, runs the rules and keeps a copy of state. It returns the copies; at the first error it stops and returns the copies so far with the error."),
			local("Add `func (f Facts) Changed(g Facts) []string` to facts.go: the sorted names of the facts that differ between f and g, counting a fact set in only one of them."),
			cloud("Add `func (e *Engine) Next(f Facts) (string, bool)` to engine.go: the name of the rule Step would fire on f, without firing it or touching the trace. Make Step use it."),
			cloud("Add `func Order(rules []Rule) []Rule` to agenda.go: a copy of rules in the order they would get to fire if all of them could. Make Agenda use it."),
			cloud("Add `func (e *Engine) Summary() []string` to report.go: one line per rule that has fired, its name and how many times the trace shows it firing, like \"vent-hot 3\", most firings first and then by name."),
			local("Add `func (e *Engine) Load(text string) error` to engine.go: it parses another rule file and appends its rules after e.Rules, as if that file came after the first one. On a parse error it returns the error and leaves e.Rules as it was."),
			cloud("Add `func Diff(before, after []Rule) []string` to check.go, for reviewing edits to a rule file: \"+ name\" for a rule only in after, \"- name\" for one only in before, and \"~ name\" for one in both whose String() differs, sorted by name. When a set has a name twice, its last rule with that name counts."),
		),
		Carries: []Carry{
			{
				What:        "the rule syntax: when/then, lower-case names, optional priority that may be negative, no version 1 if (docs/rule-syntax.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{5, 11, 12, 28},
				Source:      "docs/rule-syntax.txt",
				Tests:       []string{"TestRuleString", "TestParseRuleRejects", "TestParseRuleDefaultsAndSpacing"},
				Absent:      []string{"lower-case", "rule-syntax", "version 1", "keywords", "priority n"},
			},
			{
				What:        "a condition on a fact that is not set is false, != included",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{9},
				Tests:       []string{"TestHoldsMissing"},
				Absent:      []string{"isn't set", "!= included", "whatever its operator", "missing fact"},
			},
			{
				What:        "numbers compare as numbers; text compares only with = and !=",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{9},
				Tests:       []string{"TestHoldsNumbers", "TestHoldsText", "TestHoldsMixed"},
				Absent:      []string{"as numbers", "exact text", "ordering operators", "read as numbers"},
			},
			{
				What:        "the highest priority fires first",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{15, 26},
				Tests:       []string{"TestAgendaPriority", "TestOrder"},
				Absent:      []string{"highest priority", "comes first in the rule", "equal priorities", "goes first"},
			},
			{
				What:        "ties in priority revised: the rule later in the file now wins",
				Kind:        CarryUser,
				Established: 18,
				Needed:      []int{19, 26},
				Tests:       []string{"TestAgendaTies", "TestOrder"},
				Absent:      []string{"later in the rule file", "same priority", "change of plan", "override", "appended"},
			},
			{
				What:        "the interlocks and the sensor readings rules must not set (docs/controller.txt)",
				Kind:        CarryDiscovered,
				Established: 7,
				Needed:      []int{17, 22},
				Source:      "docs/controller.txt",
				Tests:       []string{"TestAllowedHeaterAndVent", "TestAllowedMist", "TestStepPassesOverForbidden", "TestCheck"},
				Absent:      []string{"heater", "humidity", "interlock", "85", "mist"},
			},
			{
				What:        "MaxCycles 0 means 50, and a run past the limit ends with ErrLoop",
				Kind:        CarryUser,
				Established: 13,
				Needed:      []int{21, 23},
				Tests:       []string{"TestRunLoop", "TestRunMaxCycles", "TestSettleStopsAtError"},
				Absent:      []string{"means 50", "50", "that many", "maxcycles of 0"},
			},
			{
				What:        "a trace line is the firing number, the name and a colon, then fact=value for each action",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{19, 20, 27},
				Tests:       []string{"TestStep", "TestExplain", "TestSummary"},
				Absent:      []string{"number of the firing", "counted from 1", "and a colon", "3 vent-hot"},
			},
		},
	}
}
