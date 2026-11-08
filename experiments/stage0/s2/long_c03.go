package s2

func longC03() Task {
	return Task{
		ID:     "s2-long-c03-alert-pipeline",
		Set:    "long",
		System: system("alertd, an in-process alert evaluation pipeline with an injected clock"),
		Files: map[string]string{
			"go.mod": goMod,
			"docs/routing.md": `# Where alerts go

Agreed with the on-call teams at the spring review.

Every rule belongs to a team, and each team has two channels: pager-<team> wakes somebody up
and chat-<team> is the team's room. A rule with no team belongs to the team called oncall.

- critical goes to the pager channel and then to the chat channel, in that order.
- warning goes to the chat channel only.
- info goes nowhere. Info alerts exist to show up in reports, and they never notify anybody,
  whether firing or resolved.

When an alert resolves, the notice goes to the chat channel only, whatever the severity was.
Nobody should be woken for good news.

## Not our concern

The paging provider handles escalation and rotas. We hand over one message per channel and
stop thinking about it. If a team wants other channels, that is a conversation for the review.
`,
			"docs/rules-format.md": `# Rule files

One rule per line, with fields separated by spaces. The first four are positional, the rest
are key=value and may come in any order:

    <name> <metric> <op> <threshold> [for=<duration>] [severity=<severity>] [team=<team>]

op is > or <, and threshold is any number. severity is critical, warning or info.

Left out, for is 0, so the rule fires at once, severity is warning and team is empty.

## Durations

A duration is a whole number followed by s, m or h: for=90s, for=5m, for=1h. A bare number is
minutes, for historic reasons: for=5 means five minutes, and that has caught people out. All
durations in text we read, from any source, are written this way.

## Mistakes

A line with too few fields, an unknown op, a threshold that is not a number, an unknown key, an
unknown severity or a duration that cannot be read is a mistake in the file. Stop and report it.
Do not guess what somebody meant, because a rule that is silently wrong is worse than none.
`,
			"engine.go": `package alerts

import "sort"

// Clock reads the current time in Unix seconds.
type Clock func() int64

// Engine holds rules and samples. It does not evaluate anything yet.
type Engine struct {
	clock   Clock
	samples *SampleStore
	rules   map[string]Rule
}

// NewEngine returns an engine with no rules.
func NewEngine(clock Clock) *Engine {
	return &Engine{clock: clock, samples: NewSampleStore(), rules: map[string]Rule{}}
}

// Samples returns the store the engine reads from.
func (e *Engine) Samples() *SampleStore { return e.samples }

// AddRule adds a rule or replaces the rule of that name.
func (e *Engine) AddRule(r Rule) { e.rules[r.Name] = r }

// Rules returns the rules in order of name.
func (e *Engine) Rules() []Rule {
	out := make([]Rule, 0, len(e.rules))
	for _, r := range e.rules {
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}
`,
			"engine_test.go": `package alerts

import "testing"

func TestRulesInNameOrder(t *testing.T) {
	e := NewEngine(func() int64 { return 1000 })
	e.AddRule(Rule{Name: "b", Metric: "m", Op: ">", Threshold: 1})
	e.AddRule(Rule{Name: "a", Metric: "m", Op: "<", Threshold: 1})
	got := e.Rules()
	if len(got) != 2 || got[0].Name != "a" {
		t.Fatalf("Rules = %+v", got)
	}
	if !got[0].Breached(0) || got[1].Breached(1) {
		t.Fatal("Breached is wrong")
	}
}

func TestSampleStoreBasics(t *testing.T) {
	s := NewSampleStore()
	s.Add("cpu", "web2", 10, 1)
	s.Add("cpu", "web1", 20, 2)
	s.Add("cpu", "web1", 30, 3)
	if got, ok := s.Latest("cpu", "web1"); !ok || got.Value != 3 {
		t.Fatalf("Latest = %+v, %v", got, ok)
	}
	if got := s.Instances("cpu"); len(got) != 2 || got[0] != "web1" {
		t.Fatalf("Instances = %v", got)
	}
	if n := s.Trim(25); n != 2 {
		t.Fatalf("Trim = %d", n)
	}
	if got := s.Instances("cpu"); len(got) != 1 {
		t.Fatalf("after Trim, Instances = %v", got)
	}
}
`,
			"rule.go": `package alerts

// Rule watches one metric. For is how long a breach has to last, in seconds, before the
// alert fires. Severity is critical, warning or info.
type Rule struct {
	Name, Metric string
	Op           string // ">" or "<"
	Threshold    float64
	For          int64
	Severity     string
	Team         string
}

// Breached reports whether a value breaks the rule.
func (r Rule) Breached(v float64) bool {
	if r.Op == "<" {
		return v < r.Threshold
	}
	return v > r.Threshold
}
`,
			"samples.go": `package alerts

import "sort"

// Sample is one reading of a metric.
type Sample struct {
	At    int64
	Value float64
}

// SampleStore keeps readings per metric and per instance, oldest first.
type SampleStore struct {
	series map[string]map[string][]Sample
}

// NewSampleStore returns an empty store.
func NewSampleStore() *SampleStore {
	return &SampleStore{series: map[string]map[string][]Sample{}}
}

// Add records a reading. Readings of one instance must arrive in time order.
func (s *SampleStore) Add(metric, instance string, at int64, value float64) {
	if s.series[metric] == nil {
		s.series[metric] = map[string][]Sample{}
	}
	s.series[metric][instance] = append(s.series[metric][instance], Sample{At: at, Value: value})
}

// Latest returns the newest reading of an instance.
func (s *SampleStore) Latest(metric, instance string) (Sample, bool) {
	samples := s.series[metric][instance]
	if len(samples) == 0 {
		return Sample{}, false
	}
	return samples[len(samples)-1], true
}

// Instances returns the instances of a metric that have readings, sorted.
func (s *SampleStore) Instances(metric string) []string {
	names := make([]string, 0, len(s.series[metric]))
	for name, samples := range s.series[metric] {
		if len(samples) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Trim removes readings taken before t and returns how many it removed.
func (s *SampleStore) Trim(t int64) int {
	removed := 0
	for _, instances := range s.series {
		for name, samples := range instances {
			i := sort.Search(len(samples), func(i int) bool { return samples[i].At >= t })
			instances[name] = samples[i:]
			removed += i
		}
	}
	return removed
}
`,
		},
		Hidden: map[string]string{hiddenName: `package alerts_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	alerts "fixture"
)

const minute = int64(60)

var startAt = time.Date(2030, 3, 4, 5, 6, 0, 0, time.UTC).Unix()

const startStamp = "04 Mar 05:06 UTC"

type rig struct {
	now int64
	e   *alerts.Engine
}

func newRig(rules ...alerts.Rule) *rig {
	r := &rig{now: startAt}
	r.e = alerts.NewEngine(func() int64 { return r.now })
	for _, rule := range rules {
		r.e.AddRule(rule)
	}
	return r
}

func cpu(severity string) alerts.Rule {
	return alerts.Rule{Name: "cpu", Metric: "cpu", Op: ">", Threshold: 90, Severity: severity, Team: "db"}
}

// see adds a fresh sample and evaluates.
func (r *rig) see(metric, instance string, v float64) {
	r.e.Samples().Add(metric, instance, r.now, v)
	r.e.Evaluate()
}

// firing has one firing alert on cpu/web1, with its notifications drained.
func firing(severity string) *rig {
	r := newRig(cpu(severity))
	r.see("cpu", "web1", 97.5)
	r.e.Drain()
	return r
}

// clear evaluates good samples until a firing alert has gone away.
func (r *rig) clear(instance string) {
	for range 3 {
		r.see("cpu", instance, 10)
	}
}

func channels(ns []alerts.Notification) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Channel)
	}
	return out
}

func TestParseRuleAllFields(t *testing.T) {
	got, err := alerts.ParseRule("disk-full disk.used > 92.5 severity=critical team=storage for=5m")
	want := alerts.Rule{Name: "disk-full", Metric: "disk.used", Op: ">", Threshold: 92.5, For: 300, Severity: "critical", Team: "storage"}
	if err != nil || got != want {
		t.Fatalf("ParseRule = %+v, %v", got, err)
	}
}

func TestParseRuleDefaults(t *testing.T) {
	got, err := alerts.ParseRule("low mem < 5")
	want := alerts.Rule{Name: "low", Metric: "mem", Op: "<", Threshold: 5, Severity: "warning"}
	if err != nil || got != want {
		t.Fatalf("ParseRule = %+v, %v", got, err)
	}
}

func TestParseRuleDurations(t *testing.T) {
	for text, want := range map[string]int64{"90s": 90, "5m": 300, "1h": 3600, "5": 300} {
		got, err := alerts.ParseRule("r m > 1 for=" + text)
		if err != nil || got.For != want {
			t.Errorf("for=%s: %+v, %v", text, got, err)
		}
	}
}

func TestParseRuleRefusesMistakes(t *testing.T) {
	for _, line := range []string{"", "a b >", "a b = 1", "a b > x", "a b > 1 color=red", "a b > 1 severity=huge", "a b > 1 for=soon", "a b > 1 stray"} {
		if _, err := alerts.ParseRule(line); !errors.Is(err, alerts.ErrBadRule) {
			t.Errorf("%q: %v", line, err)
		}
	}
}

func TestRemoveRuleGoesAway(t *testing.T) {
	r := firing("warning")
	if err := r.e.RemoveRule("cpu"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.e.Get("cpu", "web1"); ok {
		t.Error("its alert is still there")
	}
	if len(r.e.Rules()) != 0 {
		t.Error("the rule is still there")
	}
	if err := r.e.RemoveRule("cpu"); !errors.Is(err, alerts.ErrUnknownRule) {
		t.Errorf("second time: %v", err)
	}
}

func TestPendingThenFiring(t *testing.T) {
	rule := cpu("warning")
	rule.For = 5 * minute
	r := newRig(rule)
	r.see("cpu", "web1", 95)
	a, ok := r.e.Get("cpu", "web1")
	if !ok || a.State != alerts.StatePending || a.Since != startAt {
		t.Fatalf("Get = %+v, %v", a, ok)
	}
	r.now += 4 * minute
	r.see("cpu", "web1", 96)
	if a, _ := r.e.Get("cpu", "web1"); a.State != alerts.StatePending {
		t.Fatalf("too early: %+v", a)
	}
	r.now += minute
	r.see("cpu", "web1", 97)
	a, _ = r.e.Get("cpu", "web1")
	if a.State != alerts.StateFiring || a.FiringSince != r.now || a.Since != startAt || a.Value != 97 || a.Severity != "warning" {
		t.Fatalf("Get = %+v", a)
	}
}

func TestRuleWithoutForFiresAtOnce(t *testing.T) {
	r := newRig(cpu("warning"))
	r.see("cpu", "web1", 95)
	if a, _ := r.e.Get("cpu", "web1"); a.State != alerts.StateFiring {
		t.Fatalf("Get = %+v", a)
	}
	if _, ok := r.e.Get("cpu", "web2"); ok {
		t.Fatal("an instance nobody saw has an alert")
	}
}

func TestPendingClearsAtOnce(t *testing.T) {
	rule := cpu("warning")
	rule.For = 5 * minute
	r := newRig(rule)
	r.see("cpu", "web1", 95)
	r.see("cpu", "web1", 10)
	if _, ok := r.e.Get("cpu", "web1"); ok {
		t.Fatal("still there")
	}
}

func TestFiringClearsAfterThreeGood(t *testing.T) {
	r := firing("warning")
	r.see("cpu", "web1", 10)
	r.see("cpu", "web1", 10)
	if _, ok := r.e.Get("cpu", "web1"); !ok {
		t.Fatal("gone after two good evaluations")
	}
	r.see("cpu", "web1", 95)
	r.see("cpu", "web1", 10)
	r.see("cpu", "web1", 10)
	if _, ok := r.e.Get("cpu", "web1"); !ok {
		t.Fatal("the bad value did not start the count over")
	}
	r.see("cpu", "web1", 10)
	if _, ok := r.e.Get("cpu", "web1"); ok {
		t.Fatal("still there after three in a row")
	}
}

func TestOldSampleDoesNotFire(t *testing.T) {
	r := newRig(cpu("warning"))
	r.e.Samples().Add("cpu", "web1", r.now, 99)
	r.now += 11 * minute
	r.e.Evaluate()
	if _, ok := r.e.Get("cpu", "web1"); ok {
		t.Fatal("an old sample started an alert")
	}
}

func TestOldSampleDoesNotClear(t *testing.T) {
	r := firing("warning")
	r.now += minute
	r.e.Samples().Add("cpu", "web1", r.now, 10)
	r.now += 11 * minute
	for range 4 {
		r.e.Evaluate()
	}
	if _, ok := r.e.Get("cpu", "web1"); !ok {
		t.Fatal("an old sample cleared the alert")
	}
}

func TestSampleWithinWindowCounts(t *testing.T) {
	r := newRig(cpu("warning"))
	r.e.Samples().Add("cpu", "web1", r.now, 99)
	r.now += 9 * minute
	r.e.Evaluate()
	if _, ok := r.e.Get("cpu", "web1"); !ok {
		t.Fatal("a nine-minute-old sample did not count")
	}
}

func TestBreachingLists(t *testing.T) {
	r := newRig(cpu("warning"), alerts.Rule{Name: "a-mem", Metric: "mem", Op: "<", Threshold: 5})
	r.e.Samples().Add("cpu", "web2", r.now, 99)
	r.e.Samples().Add("cpu", "web1", r.now, 99)
	r.e.Samples().Add("cpu", "web3", r.now, 1)
	r.e.Samples().Add("mem", "web1", r.now, 1)
	want := []string{"a-mem/web1", "cpu/web1", "cpu/web2"}
	if got := r.e.Breaching(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Breaching = %v", got)
	}
	if len(r.e.Active()) != 0 {
		t.Fatal("Breaching changed something")
	}
}

func TestBreachingSkipsOldSamples(t *testing.T) {
	r := newRig(cpu("warning"))
	r.e.Samples().Add("cpu", "web1", r.now, 99)
	r.now += 11 * minute
	if got := r.e.Breaching(); len(got) != 0 {
		t.Fatalf("Breaching = %v", got)
	}
}

func TestDisabledRule(t *testing.T) {
	r := firing("warning")
	if err := r.e.DisableRule("cpu"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.e.Get("cpu", "web1"); ok {
		t.Fatal("the alert survived")
	}
	if got := r.e.Drain(); len(got) != 0 {
		t.Fatalf("it notified: %+v", got)
	}
	r.see("cpu", "web1", 99)
	if _, ok := r.e.Get("cpu", "web1"); ok {
		t.Fatal("a disabled rule was evaluated")
	}
	if err := r.e.EnableRule("cpu"); err != nil {
		t.Fatal(err)
	}
	r.see("cpu", "web1", 99)
	if _, ok := r.e.Get("cpu", "web1"); !ok {
		t.Fatal("an enabled rule was not evaluated")
	}
}

func TestDisableUnknownRule(t *testing.T) {
	r := newRig()
	if err := r.e.DisableRule("nope"); !errors.Is(err, alerts.ErrUnknownRule) {
		t.Errorf("DisableRule: %v", err)
	}
	if err := r.e.EnableRule("nope"); !errors.Is(err, alerts.ErrUnknownRule) {
		t.Errorf("EnableRule: %v", err)
	}
}

func TestActiveAndPending(t *testing.T) {
	slow := cpu("warning")
	slow.Name, slow.For = "slow", 5*minute
	r := newRig(cpu("warning"), slow)
	r.see("cpu", "web1", 99)
	active, pending := r.e.Active(), r.e.Pending()
	if len(active) != 1 || active[0].Rule != "cpu" || len(pending) != 1 || pending[0].Rule != "slow" {
		t.Fatalf("Active = %+v, Pending = %+v", active, pending)
	}
}

func severities() *rig {
	var rules []alerts.Rule
	for _, s := range []struct{ name, severity string }{{"a-info", "info"}, {"b-warn", "warning"}, {"c-crit", "critical"}, {"d-crit", "critical"}} {
		rules = append(rules, alerts.Rule{Name: s.name, Metric: "cpu", Op: ">", Threshold: 90, Severity: s.severity})
	}
	r := newRig(rules...)
	r.see("cpu", "web2", 99)
	r.see("cpu", "web1", 99)
	return r
}

func TestActiveCriticalFirst(t *testing.T) {
	var got []string
	for _, a := range severities().e.Active() {
		got = append(got, a.Rule+"/"+a.Instance)
	}
	want := []string{"c-crit/web1", "c-crit/web2", "d-crit/web1", "d-crit/web2", "b-warn/web1", "b-warn/web2", "a-info/web1", "a-info/web2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v", got)
	}
}

func TestFiringGoesToChat(t *testing.T) {
	r := newRig(cpu("warning"))
	r.see("cpu", "web1", 97.5)
	got := r.e.Drain()
	if len(got) != 1 || got[0].Channel != "chat-db" || got[0].Kind != "firing" || got[0].Rule != "cpu" || got[0].Instance != "web1" || got[0].At != startAt {
		t.Fatalf("Drain = %+v", got)
	}
	if again := r.e.Drain(); len(again) != 0 {
		t.Fatalf("second Drain = %+v", again)
	}
}

func TestCriticalGoesToPagerThenChat(t *testing.T) {
	r := newRig(cpu("critical"))
	r.see("cpu", "web1", 97.5)
	if got := channels(r.e.Drain()); !reflect.DeepEqual(got, []string{"pager-db", "chat-db"}) {
		t.Fatalf("channels %v", got)
	}
}

func TestInfoNeverNotifies(t *testing.T) {
	r := newRig(cpu("info"))
	r.see("cpu", "web1", 97.5)
	r.clear("web1")
	if got := r.e.Drain(); len(got) != 0 {
		t.Fatalf("Drain = %+v", got)
	}
}

func TestRuleWithoutTeamGoesToOncall(t *testing.T) {
	rule := cpu("warning")
	rule.Team = ""
	r := newRig(rule)
	r.see("cpu", "web1", 97.5)
	if got := channels(r.e.Drain()); !reflect.DeepEqual(got, []string{"chat-oncall"}) {
		t.Fatalf("channels %v", got)
	}
}

func TestResolvedNotice(t *testing.T) {
	r := firing("warning")
	r.clear("web1")
	got := r.e.Drain()
	if len(got) != 1 || got[0].Kind != "resolved" || got[0].Channel != "chat-db" || got[0].Text != "[RESOLVED] cpu on web1" {
		t.Fatalf("Drain = %+v", got)
	}
}

func TestResolvedGoesOnlyToChat(t *testing.T) {
	r := firing("critical")
	r.clear("web1")
	if got := channels(r.e.Drain()); !reflect.DeepEqual(got, []string{"chat-db"}) {
		t.Fatalf("channels %v", got)
	}
}

func TestFiringText(t *testing.T) {
	r := newRig(cpu("warning"))
	r.see("cpu", "web1", 97.5)
	got := r.e.Drain()[0].Text
	if !strings.HasPrefix(got, "[WARNING] cpu on web1: 97.50 since ") {
		t.Fatalf("Text = %q", got)
	}
}

func TestFiringTextTime(t *testing.T) {
	r := newRig(cpu("warning"))
	r.see("cpu", "web1", 97.5)
	if got := r.e.Drain()[0].Text; !strings.HasSuffix(got, " since "+startStamp) {
		t.Fatalf("Text = %q", got)
	}
}

func longRule(severity string) alerts.Rule {
	rule := cpu(severity)
	rule.Name = strings.Repeat("very-long-rule-name-", 12)
	return rule
}

func TestPagerTextIsCut(t *testing.T) {
	r := newRig(longRule("critical"))
	r.see("cpu", "web1", 97.5)
	var pager string
	for _, n := range r.e.Drain() {
		if n.Channel == "pager-db" {
			pager = n.Text
		}
	}
	if len(pager) != 160 || !strings.HasSuffix(pager, "...") || !strings.HasPrefix(pager, "[CRITICAL] very-long-rule-name-") {
		t.Fatalf("pager text %q has %d bytes", pager, len(pager))
	}
}

func TestChatTextIsNotCut(t *testing.T) {
	r := newRig(longRule("warning"))
	r.see("cpu", "web1", 97.5)
	if got := r.e.Drain()[0].Text; len(got) <= 240 || strings.HasSuffix(got, "...") {
		t.Fatalf("chat text %q", got)
	}
}

func repeated(r *rig, after int64) bool {
	r.now += after
	r.see("cpu", "web1", 97.5)
	return len(r.e.Drain()) > 0
}

func TestNoRepeatInsideInterval(t *testing.T) {
	if repeated(firing("critical"), 5*minute) {
		t.Fatal("a critical alert repeated after five minutes")
	}
	if repeated(firing("warning"), 5*minute) {
		t.Fatal("a warning repeated after five minutes")
	}
}

func TestCriticalRepeatsAfterTenMinutes(t *testing.T) {
	if !repeated(firing("critical"), 11*minute) {
		t.Fatal("a critical alert did not repeat after eleven minutes")
	}
}

func TestWarningWaitsAnHour(t *testing.T) {
	r := firing("warning")
	if repeated(r, 45*minute) {
		t.Fatal("a warning repeated after 45 minutes")
	}
	if !repeated(r, 16*minute) {
		t.Fatal("a warning did not repeat after 61 minutes")
	}
}

func TestAddSilenceGivesIDs(t *testing.T) {
	r := newRig(cpu("warning"))
	first, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now, End: r.now + 60})
	if err != nil || first != 1 {
		t.Fatalf("first = %d, %v", first, err)
	}
	second, err := r.e.AddSilence(alerts.Silence{Rule: "db-*", Start: r.now, End: r.now + 60})
	if err != nil || second != 2 {
		t.Fatalf("second = %d, %v", second, err)
	}
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now, End: r.now}); !errors.Is(err, alerts.ErrBadSilence) {
		t.Fatalf("empty span: %v", err)
	}
}

func TestAddSilenceUnknownRule(t *testing.T) {
	r := newRig()
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "nope", Start: r.now, End: r.now + 60}); !errors.Is(err, alerts.ErrUnknownRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestSilencesInForce(t *testing.T) {
	r := newRig(cpu("warning"))
	id, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now + 10, End: r.now + 20})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.e.ActiveSilences(); len(got) != 0 {
		t.Fatalf("before Start: %+v", got)
	}
	r.now += 10
	if got := r.e.ActiveSilences(); len(got) != 1 || got[0].ID != id {
		t.Fatalf("at Start: %+v", got)
	}
	r.now += 10
	if got := r.e.ActiveSilences(); len(got) != 0 {
		t.Fatalf("at End: %+v", got)
	}
}

func TestRemoveSilence(t *testing.T) {
	r := newRig(cpu("warning"))
	id, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now, End: r.now + 60})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.e.RemoveSilence(id); err != nil {
		t.Fatal(err)
	}
	if got := r.e.ActiveSilences(); len(got) != 0 {
		t.Fatalf("ActiveSilences = %+v", got)
	}
	if err := r.e.RemoveSilence(id); !errors.Is(err, alerts.ErrNoSilence) {
		t.Fatalf("second time: %v", err)
	}
}

func TestSilenceHoldsBackMatchingAlerts(t *testing.T) {
	db := alerts.Rule{Name: "db-slow", Metric: "cpu", Op: ">", Threshold: 90, Severity: "warning", Team: "db"}
	r := newRig(db, cpu("warning"))
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "db-*", Start: r.now, End: r.now + 60}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Instance: "web2", Start: r.now, End: r.now + 60}); err != nil {
		t.Fatal(err)
	}
	r.see("cpu", "web2", 99)
	r.see("cpu", "web1", 99)
	got := r.e.Drain()
	if len(got) != 1 || got[0].Rule != "cpu" || got[0].Instance != "web1" {
		t.Fatalf("Drain = %+v", got)
	}
}

func TestSilencedAlertStillFires(t *testing.T) {
	rule := cpu("warning")
	rule.For = minute
	r := newRig(rule)
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now, End: r.now + 10*minute}); err != nil {
		t.Fatal(err)
	}
	r.see("cpu", "web1", 99)
	if a, ok := r.e.Get("cpu", "web1"); !ok || a.State != alerts.StatePending {
		t.Fatalf("Get = %+v, %v", a, ok)
	}
	r.now += 2 * minute
	r.see("cpu", "web1", 99)
	if a, ok := r.e.Get("cpu", "web1"); !ok || a.State != alerts.StateFiring {
		t.Fatalf("Get = %+v, %v", a, ok)
	}
	if got := r.e.Drain(); len(got) != 0 {
		t.Fatalf("it notified: %+v", got)
	}
}

func TestSilenceEndNotifiesAtOnce(t *testing.T) {
	r := firing("warning")
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now, End: r.now + 10*minute}); err != nil {
		t.Fatal(err)
	}
	r.now += 5 * minute
	r.see("cpu", "web1", 99)
	if got := r.e.Drain(); len(got) != 0 {
		t.Fatalf("during the silence: %+v", got)
	}
	r.now += 5 * minute
	r.see("cpu", "web1", 99)
	if got := r.e.Drain(); len(got) != 1 {
		t.Fatalf("when it ended: %+v", got)
	}
}

func TestNoResolvedWithoutNotice(t *testing.T) {
	r := newRig(cpu("warning"))
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now, End: r.now + 10*minute}); err != nil {
		t.Fatal(err)
	}
	r.see("cpu", "web1", 99)
	r.now += 11 * minute
	r.clear("web1")
	if got := r.e.Drain(); len(got) != 0 {
		t.Fatalf("Drain = %+v", got)
	}
}

func TestAckStopsRepeats(t *testing.T) {
	r := firing("warning")
	if err := r.e.Ack("cpu", "web1"); err != nil {
		t.Fatal(err)
	}
	if a, _ := r.e.Get("cpu", "web1"); !a.Acked {
		t.Fatal("Acked is not set")
	}
	if repeated(r, 61*minute) {
		t.Fatal("an acknowledged alert repeated")
	}
	r.clear("web1")
	got := r.e.Drain()
	if len(got) != 1 || got[0].Kind != "resolved" {
		t.Fatalf("Drain = %+v", got)
	}
}

func TestAckNeedsAFiringAlert(t *testing.T) {
	r := newRig(cpu("warning"))
	if err := r.e.Ack("cpu", "web1"); !errors.Is(err, alerts.ErrNoAlert) {
		t.Fatalf("err = %v", err)
	}
}

func TestAckUnknownRule(t *testing.T) {
	r := newRig()
	if err := r.e.Ack("nope", "web1"); !errors.Is(err, alerts.ErrUnknownRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestExportFormat(t *testing.T) {
	slow := cpu("warning")
	slow.Name, slow.For = "a-slow", 5*minute
	r := newRig(cpu("warning"), slow)
	r.see("cpu", "web1", 99)
	lines := strings.Split(strings.TrimSpace(r.e.Export()), "\n")
	if len(lines) != 3 || lines[0] != "rule,instance,severity,state,since" {
		t.Fatalf("Export = %q", lines)
	}
	if !strings.HasPrefix(lines[1], "cpu,web1,warning,firing,") || !strings.HasPrefix(lines[2], "a-slow,web1,warning,pending,") {
		t.Fatalf("Export = %q", lines)
	}
}

func TestExportTime(t *testing.T) {
	r := firing("warning")
	want := "cpu,web1,warning,firing," + startStamp + "\n"
	if got := r.e.Export(); !strings.HasSuffix(got, want) {
		t.Fatalf("Export = %q", got)
	}
}

func TestExportCriticalFirst(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(severities().e.Export()), "\n")
	if len(lines) != 9 || !strings.HasPrefix(lines[1], "c-crit,web1,") || !strings.HasPrefix(lines[8], "a-info,web2,") {
		t.Fatalf("Export = %q", lines)
	}
}

func TestExplainStates(t *testing.T) {
	slow := cpu("warning")
	slow.Name, slow.For = "slow", 5*minute
	r := newRig(cpu("warning"), slow)
	r.see("cpu", "web1", 10)
	if got, err := r.e.Explain("cpu", "web1"); err != nil || got != "ok" {
		t.Errorf("no alert: %q, %v", got, err)
	}
	if got, err := r.e.Explain("cpu", "web9"); err != nil || got != "no data" {
		t.Errorf("no samples: %q, %v", got, err)
	}
	r.see("cpu", "web1", 99)
	if got, err := r.e.Explain("cpu", "web1"); err != nil || !strings.HasPrefix(got, "firing since ") {
		t.Errorf("firing: %q, %v", got, err)
	}
	if got, err := r.e.Explain("slow", "web1"); err != nil || !strings.HasPrefix(got, "pending since ") {
		t.Errorf("pending: %q, %v", got, err)
	}
}

func TestExplainOldSampleIsNoData(t *testing.T) {
	r := firing("warning")
	r.now += 11 * minute
	if got, err := r.e.Explain("cpu", "web1"); err != nil || got != "no data" {
		t.Fatalf("Explain = %q, %v", got, err)
	}
}

func TestExplainTime(t *testing.T) {
	r := firing("warning")
	if got, err := r.e.Explain("cpu", "web1"); err != nil || got != "firing since "+startStamp {
		t.Fatalf("Explain = %q, %v", got, err)
	}
}

func TestExplainUnknownRule(t *testing.T) {
	r := newRig()
	if _, err := r.e.Explain("nope", "web1"); !errors.Is(err, alerts.ErrUnknownRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestNextRepeatDue(t *testing.T) {
	r := firing("critical")
	if got, ok := r.e.NextRepeat("cpu", "web1"); !ok || got != startAt+10*minute {
		t.Errorf("critical: %d, %v", got, ok)
	}
	w := firing("warning")
	if got, ok := w.e.NextRepeat("cpu", "web1"); !ok || got != startAt+60*minute {
		t.Errorf("warning: %d, %v", got, ok)
	}
}

func TestNextRepeatNeedsANotifiedAlert(t *testing.T) {
	r := newRig(cpu("warning"))
	if _, ok := r.e.NextRepeat("cpu", "web1"); ok {
		t.Error("no alert at all")
	}
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now, End: r.now + 60}); err != nil {
		t.Fatal(err)
	}
	r.see("cpu", "web1", 99)
	if _, ok := r.e.NextRepeat("cpu", "web1"); ok {
		t.Error("firing, but nobody has been told")
	}
}

func TestSummaryCounts(t *testing.T) {
	slow := cpu("warning")
	slow.Name, slow.For = "slow", 5*minute
	r := newRig(cpu("warning"), slow)
	r.see("cpu", "web1", 99)
	lines := strings.Split(strings.TrimSpace(r.e.Summary()), "\n")
	if len(lines) != 2 || lines[0] != "1 firing, 1 pending" || !strings.HasPrefix(lines[1], "WARNING cpu web1 ") {
		t.Fatalf("Summary = %q", lines)
	}
}

func TestSummaryTime(t *testing.T) {
	r := firing("warning")
	if got := r.e.Summary(); !strings.HasSuffix(got, "WARNING cpu web1 "+startStamp+"\n") {
		t.Fatalf("Summary = %q", got)
	}
}

func TestSummaryCriticalFirst(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(severities().e.Summary()), "\n")
	if len(lines) != 9 || !strings.HasPrefix(lines[1], "CRITICAL c-crit web1 ") || !strings.HasPrefix(lines[8], "INFO a-info web2 ") {
		t.Fatalf("Summary = %q", lines)
	}
}

func TestParseSilenceFields(t *testing.T) {
	r := newRig(cpu("warning"))
	got, err := r.e.ParseSilence("for=2h instance=web1 rule=db-*")
	want := alerts.Silence{Rule: "db-*", Instance: "web1", Start: r.now, End: r.now + 7200}
	if err != nil || got != want {
		t.Fatalf("ParseSilence = %+v, %v", got, err)
	}
	if n := len(r.e.ActiveSilences()); n != 0 {
		t.Fatalf("it added the silence: %d", n)
	}
}

func TestParseSilenceBareNumberIsMinutes(t *testing.T) {
	r := newRig()
	got, err := r.e.ParseSilence("rule=cpu for=30")
	if err != nil || got.End != r.now+30*minute {
		t.Fatalf("ParseSilence = %+v, %v", got, err)
	}
}

func TestParseSilenceRefusesMistakes(t *testing.T) {
	r := newRig()
	for _, line := range []string{"", "rule=cpu", "for=5m", "rule=cpu for=soon", "rule=cpu for=5m color=red", "stray rule=cpu for=5m"} {
		if _, err := r.e.ParseSilence(line); !errors.Is(err, alerts.ErrBadSilence) {
			t.Errorf("%q: %v", line, err)
		}
	}
}

func TestHousekeepKeepsWhatCounts(t *testing.T) {
	r := newRig(cpu("warning"))
	r.e.Samples().Add("cpu", "web1", r.now, 1)
	r.now += 9 * minute
	if n := r.e.Housekeep(); n != 0 {
		t.Fatalf("Housekeep = %d", n)
	}
	if _, ok := r.e.Samples().Latest("cpu", "web1"); !ok {
		t.Fatal("the sample went away")
	}
}

func TestHousekeepDropsOldSamples(t *testing.T) {
	r := newRig(cpu("warning"))
	r.e.Samples().Add("cpu", "web1", r.now, 1)
	r.now += 5 * minute
	r.e.Samples().Add("cpu", "web1", r.now, 2)
	r.now += 6 * minute
	if n := r.e.Housekeep(); n != 1 {
		t.Fatalf("Housekeep = %d", n)
	}
	if got, ok := r.e.Samples().Latest("cpu", "web1"); !ok || got.Value != 2 {
		t.Fatalf("Latest = %+v, %v", got, ok)
	}
}

func TestStatsCounts(t *testing.T) {
	slow := cpu("warning")
	slow.Name, slow.For = "slow", 5*minute
	r := newRig(cpu("warning"), slow)
	r.see("cpu", "web1", 99)
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now, End: r.now + 60}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now - 60, End: r.now}); err != nil {
		t.Fatal(err)
	}
	want := alerts.Stats{Rules: 2, Pending: 1, Firing: 1, Silences: 1}
	if got := r.e.Stats(); got != want {
		t.Fatalf("Stats = %+v", got)
	}
}

func TestDigestsByChannel(t *testing.T) {
	other := cpu("warning")
	other.Name, other.Team = "a-other", "apps"
	r := newRig(cpu("warning"), other)
	r.e.Samples().Add("cpu", "web2", r.now, 99)
	r.e.Samples().Add("cpu", "web1", r.now, 99)
	r.e.Evaluate()
	got := r.e.DrainDigests()
	if len(got) != 2 || got[0].Channel != "chat-apps" || got[1].Channel != "chat-db" {
		t.Fatalf("DrainDigests = %+v", got)
	}
	lines := strings.Split(got[1].Text, "\n")
	if got[1].Count != 2 || len(lines) != 2 || !strings.Contains(lines[0], "web1") || !strings.Contains(lines[1], "web2") {
		t.Fatalf("digest %+v", got[1])
	}
	if again := r.e.Drain(); len(again) != 0 {
		t.Fatalf("the queue still has %+v", again)
	}
}

func TestPruneSilences(t *testing.T) {
	r := newRig(cpu("warning"))
	for i, span := range [][2]int64{{-60, -30}, {-60, 0}, {-60, 60}} {
		if _, err := r.e.AddSilence(alerts.Silence{Rule: "cpu", Start: r.now + span[0], End: r.now + span[1]}); err != nil {
			t.Fatal(fmt.Errorf("silence %d: %w", i, err))
		}
	}
	if n := r.e.PruneSilences(); n != 2 {
		t.Fatalf("PruneSilences = %d", n)
	}
	if got := r.e.ActiveSilences(); len(got) != 1 {
		t.Fatalf("ActiveSilences = %+v", got)
	}
}

func TestReloadDropsAndKeeps(t *testing.T) {
	other := cpu("warning")
	other.Name = "other"
	r := newRig(cpu("warning"), other)
	r.see("cpu", "web1", 99)
	r.e.Drain()
	n := r.e.Reload([]alerts.Rule{cpu("critical")})
	if n != 1 {
		t.Fatalf("Reload = %d", n)
	}
	if _, ok := r.e.Get("other", "web1"); ok {
		t.Fatal("the alert of a removed rule survived")
	}
	a, ok := r.e.Get("cpu", "web1")
	if !ok || a.Severity != "critical" || a.State != alerts.StateFiring {
		t.Fatalf("Get = %+v, %v", a, ok)
	}
	if got := r.e.Drain(); len(got) != 0 {
		t.Fatalf("Reload notified: %+v", got)
	}
}
`},
		Solution: map[string]string{
			"engine.go": `package alerts

import (
	"errors"
	"sort"
)

// ErrUnknownRule is for a rule name the engine does not have.
var ErrUnknownRule = errors.New("unknown rule")

// Clock reads the current time in Unix seconds.
type Clock func() int64

// Engine holds rules, samples and the alerts it derives from them.
type Engine struct {
	clock    Clock
	samples  *SampleStore
	rules    map[string]Rule
	disabled map[string]bool
	alerts   map[string]*Alert
	silences []Silence
	outbox   []Notification
	lastID   int
}

// NewEngine returns an engine with no rules.
func NewEngine(clock Clock) *Engine {
	return &Engine{
		clock:    clock,
		samples:  NewSampleStore(),
		rules:    map[string]Rule{},
		disabled: map[string]bool{},
		alerts:   map[string]*Alert{},
	}
}

// Samples returns the store the engine reads from.
func (e *Engine) Samples() *SampleStore { return e.samples }

// AddRule adds a rule or replaces the rule of that name.
func (e *Engine) AddRule(r Rule) { e.rules[r.Name] = r }

// Rules returns the rules in order of name.
func (e *Engine) Rules() []Rule {
	out := make([]Rule, 0, len(e.rules))
	for _, r := range e.rules {
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// dropAlerts removes the alerts of a rule without notifying anybody.
func (e *Engine) dropAlerts(name string) int {
	dropped := 0
	for key, a := range e.alerts {
		if a.Rule == name {
			delete(e.alerts, key)
			dropped++
		}
	}
	return dropped
}

// RemoveRule removes a rule and its alerts.
func (e *Engine) RemoveRule(name string) error {
	if _, ok := e.rules[name]; !ok {
		return ErrUnknownRule
	}
	delete(e.rules, name)
	delete(e.disabled, name)
	e.dropAlerts(name)
	return nil
}

// DisableRule stops evaluating a rule. Its alerts go away without a word.
func (e *Engine) DisableRule(name string) error {
	if _, ok := e.rules[name]; !ok {
		return ErrUnknownRule
	}
	e.disabled[name] = true
	e.dropAlerts(name)
	return nil
}

// EnableRule starts evaluating a rule again.
func (e *Engine) EnableRule(name string) error {
	if _, ok := e.rules[name]; !ok {
		return ErrUnknownRule
	}
	delete(e.disabled, name)
	return nil
}

// Reload replaces all the rules. Alerts of rules that are gone are dropped without a
// word, and their number returned. The others carry on under the new severity.
func (e *Engine) Reload(rules []Rule) int {
	e.rules = map[string]Rule{}
	for _, r := range rules {
		e.rules[r.Name] = r
	}
	dropped := 0
	for key, a := range e.alerts {
		if r, ok := e.rules[a.Rule]; ok {
			a.Severity = r.Severity
		} else {
			delete(e.alerts, key)
			dropped++
		}
	}
	return dropped
}
`,
			"evaluate.go": `package alerts

import "errors"

// ErrNoAlert is for an alert that is not firing.
var ErrNoAlert = errors.New("no firing alert")

const (
	// staleAfter is how old a sample can be, in seconds, and still count as data.
	staleAfter = 600
	// clearAfter is how many good evaluations in a row clear a firing alert.
	clearAfter = 3
)

// State is where an alert stands.
type State string

const (
	StatePending State = "pending"
	StateFiring  State = "firing"
)

// Alert is a rule in breach on one instance.
type Alert struct {
	Rule, Instance, Severity string
	State                    State
	Since, FiringSince       int64
	Value                    float64
	Acked                    bool

	clean    int   // good evaluations in a row
	notified bool  // somebody has been told about this alert
	held     bool  // a silence held back a notification
	lastSent int64 // when the last notification went out
}

func alertKey(rule, instance string) string { return rule + "/" + instance }

// Get returns one alert.
func (e *Engine) Get(rule, instance string) (Alert, bool) {
	a, ok := e.alerts[alertKey(rule, instance)]
	if !ok {
		return Alert{}, false
	}
	return *a, true
}

// Evaluate checks every rule against the latest sample of each instance of its metric. A
// sample too old to count is no data: it neither breaches a rule nor clears an alert.
func (e *Engine) Evaluate() {
	now := e.clock()
	for _, r := range e.Rules() {
		if e.disabled[r.Name] {
			continue
		}
		for _, instance := range e.samples.Instances(r.Metric) {
			s, ok := e.samples.Latest(r.Metric, instance)
			if ok && now-s.At <= staleAfter {
				e.step(r, instance, s.Value, now)
			}
		}
	}
}

// step moves one alert along given a fresh value.
func (e *Engine) step(r Rule, instance string, v float64, now int64) {
	key := alertKey(r.Name, instance)
	a := e.alerts[key]
	if !r.Breached(v) {
		if a != nil {
			e.clearing(r, a, key, now)
		}
		return
	}
	if a == nil {
		a = &Alert{Rule: r.Name, Instance: instance, Severity: r.Severity, State: StatePending, Since: now}
		e.alerts[key] = a
	}
	a.Value, a.clean = v, 0
	if a.State == StatePending && now-a.Since >= r.For {
		a.State, a.FiringSince = StateFiring, now
	}
	if a.State == StateFiring {
		e.notifyFiring(r, a, now)
	}
}

// clearing counts one good evaluation of an alert and ends it when that is enough.
func (e *Engine) clearing(r Rule, a *Alert, key string, now int64) {
	a.clean++
	if a.State == StateFiring && a.clean < clearAfter {
		return
	}
	if a.State == StateFiring && a.notified && !e.silenced(r.Name, a.Instance, now) {
		e.send("resolved", r, a, now)
	}
	delete(e.alerts, key)
}

// Ack stops an alert repeating. It still gets its resolved notice.
func (e *Engine) Ack(rule, instance string) error {
	if _, ok := e.rules[rule]; !ok {
		return ErrUnknownRule
	}
	a, ok := e.alerts[alertKey(rule, instance)]
	if !ok || a.State != StateFiring {
		return ErrNoAlert
	}
	a.Acked = true
	return nil
}
`,
			"notify.go": `package alerts

import (
	"fmt"
	"sort"
	"strings"
)

// Notification is one message for one channel.
type Notification struct {
	Channel, Kind, Rule, Instance string
	At                            int64
	Text                          string
}

// route returns where a notification goes, as docs/routing.md lays it down.
func route(r Rule, kind string) []string {
	team := r.Team
	if team == "" {
		team = "oncall"
	}
	switch {
	case r.Severity == "info":
		return nil
	case kind == "resolved" || r.Severity == "warning":
		return []string{"chat-" + team}
	}
	return []string{"pager-" + team, "chat-" + team}
}

// repeatEvery returns how long a firing alert waits between notifications, in seconds.
func repeatEvery(severity string) int64 {
	if severity == "critical" {
		return 10 * 60
	}
	return 60 * 60
}

// cutForPager fits a text into what the pager gateway can take.
func cutForPager(text string) string {
	if len(text) <= 160 {
		return text
	}
	return text[:157] + "..."
}

func notificationText(kind string, a *Alert) string {
	if kind == "resolved" {
		return fmt.Sprintf("[RESOLVED] %s on %s", a.Rule, a.Instance)
	}
	return fmt.Sprintf("[%s] %s on %s: %.2f since %s", strings.ToUpper(a.Severity), a.Rule, a.Instance, a.Value, stamp(a.FiringSince))
}

// send queues a notification for every channel it goes to.
func (e *Engine) send(kind string, r Rule, a *Alert, now int64) {
	for _, channel := range route(r, kind) {
		text := notificationText(kind, a)
		if strings.HasPrefix(channel, "pager-") {
			text = cutForPager(text)
		}
		e.outbox = append(e.outbox, Notification{Channel: channel, Kind: kind, Rule: a.Rule, Instance: a.Instance, At: now, Text: text})
	}
}

// notifyFiring sends what a firing alert is due. A silence holds it back, and when the
// silence is over it goes out at once, whatever went out last.
func (e *Engine) notifyFiring(r Rule, a *Alert, now int64) {
	if a.Acked {
		return
	}
	if e.silenced(r.Name, a.Instance, now) {
		a.held = true
		return
	}
	if a.notified && !a.held && now-a.lastSent < repeatEvery(r.Severity) {
		return
	}
	a.notified, a.held, a.lastSent = true, false, now
	e.send("firing", r, a, now)
}

// Drain returns what is queued and empties the queue.
func (e *Engine) Drain() []Notification {
	out := e.outbox
	e.outbox = nil
	return out
}

// Digest is one channel's notifications as one message.
type Digest struct {
	Channel string
	Count   int
	Text    string
}

// DrainDigests empties the queue into one digest per channel, sorted by channel, with
// the texts of its notifications joined by newlines, oldest first.
func (e *Engine) DrainDigests() []Digest {
	texts := map[string][]string{}
	for _, n := range e.Drain() {
		texts[n.Channel] = append(texts[n.Channel], n.Text)
	}
	out := make([]Digest, 0, len(texts))
	for channel, lines := range texts {
		out = append(out, Digest{Channel: channel, Count: len(lines), Text: strings.Join(lines, "\n")})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Channel < out[b].Channel })
	return out
}
`,
			"report.go": `package alerts

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

var severityRank = map[string]int{"critical": 0, "warning": 1, "info": 2}

// stamp prints a clock reading the way everything we print shows it.
func stamp(at int64) string { return time.Unix(at, 0).UTC().Format("02 Jan 15:04 UTC") }

// sortAlerts puts alerts in the order every list of them uses.
func sortAlerts(out []Alert) {
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] < severityRank[b.Severity]
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Instance < b.Instance
	})
}

func (e *Engine) inState(state State) []Alert {
	var out []Alert
	for _, a := range e.alerts {
		if a.State == state {
			out = append(out, *a)
		}
	}
	sortAlerts(out)
	return out
}

// Active returns the firing alerts.
func (e *Engine) Active() []Alert { return e.inState(StateFiring) }

// Pending returns the pending alerts.
func (e *Engine) Pending() []Alert { return e.inState(StatePending) }

// Breaching returns rule/instance for every instance whose latest sample breaks its rule
// right now, sorted. It looks only at samples.
func (e *Engine) Breaching() []string {
	now := e.clock()
	var out []string
	for _, r := range e.Rules() {
		for _, instance := range e.samples.Instances(r.Metric) {
			s, ok := e.samples.Latest(r.Metric, instance)
			if ok && now-s.At <= staleAfter && r.Breached(s.Value) {
				out = append(out, alertKey(r.Name, instance))
			}
		}
	}
	sort.Strings(out)
	return out
}

// Export returns the alerts as CSV, firing ones first.
func (e *Engine) Export() string {
	var b strings.Builder
	b.WriteString("rule,instance,severity,state,since\n")
	for _, a := range e.Active() {
		fmt.Fprintf(&b, "%s,%s,%s,%s,%s\n", a.Rule, a.Instance, a.Severity, a.State, stamp(a.FiringSince))
	}
	for _, a := range e.Pending() {
		fmt.Fprintf(&b, "%s,%s,%s,%s,%s\n", a.Rule, a.Instance, a.Severity, a.State, stamp(a.Since))
	}
	return b.String()
}

// Explain says where one alert stands, for the console.
func (e *Engine) Explain(rule, instance string) (string, error) {
	r, ok := e.rules[rule]
	if !ok {
		return "", ErrUnknownRule
	}
	s, ok := e.samples.Latest(r.Metric, instance)
	if !ok || e.clock()-s.At > staleAfter {
		return "no data", nil
	}
	a, ok := e.alerts[alertKey(rule, instance)]
	switch {
	case !ok:
		return "ok", nil
	case a.State == StatePending:
		return "pending since " + stamp(a.Since), nil
	}
	return "firing since " + stamp(a.FiringSince), nil
}

// NextRepeat returns when the next repeat of a firing alert's notification is due, or
// false if it is not firing or has had none yet.
func (e *Engine) NextRepeat(rule, instance string) (int64, bool) {
	a, ok := e.alerts[alertKey(rule, instance)]
	if !ok || a.State != StateFiring || !a.notified {
		return 0, false
	}
	return a.lastSent + repeatEvery(a.Severity), true
}

// Summary returns a line of counts and then a line per firing alert.
func (e *Engine) Summary() string {
	active := e.Active()
	var b strings.Builder
	fmt.Fprintf(&b, "%d firing, %d pending\n", len(active), len(e.Pending()))
	for _, a := range active {
		fmt.Fprintf(&b, "%s %s %s %s\n", strings.ToUpper(a.Severity), a.Rule, a.Instance, stamp(a.FiringSince))
	}
	return b.String()
}

// Stats says how much the engine has going on.
type Stats struct{ Rules, Pending, Firing, Silences int }

// Stats reports on the engine. Silences counts those in force.
func (e *Engine) Stats() Stats {
	return Stats{
		Rules:    len(e.rules),
		Pending:  len(e.Pending()),
		Firing:   len(e.Active()),
		Silences: len(e.ActiveSilences()),
	}
}

// Housekeep throws away samples too old to count for anything and returns how many.
func (e *Engine) Housekeep() int { return e.samples.Trim(e.clock() - staleAfter) }
`,
			"rule.go": `package alerts

import (
	"errors"
	"strconv"
	"strings"
)

// ErrBadRule is for a line of a rule file that cannot be read.
var ErrBadRule = errors.New("bad rule")

// Rule watches one metric. For is how long a breach has to last, in seconds, before the
// alert fires. Severity is critical, warning or info.
type Rule struct {
	Name, Metric string
	Op           string // ">" or "<"
	Threshold    float64
	For          int64
	Severity     string
	Team         string
}

// Breached reports whether a value breaks the rule.
func (r Rule) Breached(v float64) bool {
	if r.Op == "<" {
		return v < r.Threshold
	}
	return v > r.Threshold
}

// parseDuration reads a duration into seconds. A bare number is minutes.
func parseDuration(s string) (int64, error) {
	unit := int64(60)
	switch {
	case strings.HasSuffix(s, "s"):
		unit, s = 1, s[:len(s)-1]
	case strings.HasSuffix(s, "m"):
		unit, s = 60, s[:len(s)-1]
	case strings.HasSuffix(s, "h"):
		unit, s = 3600, s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, ErrBadRule
	}
	return n * unit, nil
}

// setOption applies one key=value field of a rule line.
func (r *Rule) setOption(field string) error {
	key, value, ok := strings.Cut(field, "=")
	if !ok {
		return ErrBadRule
	}
	switch key {
	case "for":
		d, err := parseDuration(value)
		if err != nil {
			return err
		}
		r.For = d
	case "severity":
		if value != "critical" && value != "warning" && value != "info" {
			return ErrBadRule
		}
		r.Severity = value
	case "team":
		r.Team = value
	default:
		return ErrBadRule
	}
	return nil
}

// ParseRule reads one line of a rule file, as docs/rules-format.md describes it.
func ParseRule(line string) (Rule, error) {
	f := strings.Fields(line)
	if len(f) < 4 || (f[2] != ">" && f[2] != "<") {
		return Rule{}, ErrBadRule
	}
	threshold, err := strconv.ParseFloat(f[3], 64)
	if err != nil {
		return Rule{}, ErrBadRule
	}
	r := Rule{Name: f[0], Metric: f[1], Op: f[2], Threshold: threshold, Severity: "warning"}
	for _, field := range f[4:] {
		if err := r.setOption(field); err != nil {
			return Rule{}, err
		}
	}
	return r, nil
}
`,
			"silence.go": `package alerts

import (
	"errors"
	"slices"
	"strings"
)

var (
	// ErrBadSilence is for a silence that makes no sense.
	ErrBadSilence = errors.New("bad silence")
	// ErrNoSilence is for an ID the engine does not have.
	ErrNoSilence = errors.New("no such silence")
)

// Silence holds back notifications. Rule is a rule's name or a prefix ending in *, and
// an empty Instance means all of them. It is in force from Start up to but not
// including End.
type Silence struct {
	ID             int
	Rule, Instance string
	Start, End     int64
}

func (s Silence) inForce(now int64) bool { return now >= s.Start && now < s.End }

func (s Silence) matches(rule, instance string) bool {
	if s.Instance != "" && s.Instance != instance {
		return false
	}
	if prefix, ok := strings.CutSuffix(s.Rule, "*"); ok {
		return strings.HasPrefix(rule, prefix)
	}
	return s.Rule == rule
}

func (e *Engine) silenced(rule, instance string, now int64) bool {
	for _, s := range e.silences {
		if s.inForce(now) && s.matches(rule, instance) {
			return true
		}
	}
	return false
}

// AddSilence keeps a silence under the next ID and returns the ID.
func (e *Engine) AddSilence(s Silence) (int, error) {
	if s.End <= s.Start {
		return 0, ErrBadSilence
	}
	if !strings.HasSuffix(s.Rule, "*") {
		if _, ok := e.rules[s.Rule]; !ok {
			return 0, ErrUnknownRule
		}
	}
	e.lastID++
	s.ID = e.lastID
	e.silences = append(e.silences, s)
	return s.ID, nil
}

// RemoveSilence removes a silence by ID.
func (e *Engine) RemoveSilence(id int) error {
	for i, s := range e.silences {
		if s.ID == id {
			e.silences = slices.Delete(e.silences, i, i+1)
			return nil
		}
	}
	return ErrNoSilence
}

// ActiveSilences returns the silences in force now.
func (e *Engine) ActiveSilences() []Silence {
	now := e.clock()
	var out []Silence
	for _, s := range e.silences {
		if s.inForce(now) {
			out = append(out, s)
		}
	}
	return out
}

// PruneSilences removes the silences whose time is over and returns how many.
func (e *Engine) PruneSilences() int {
	now := e.clock()
	before := len(e.silences)
	e.silences = slices.DeleteFunc(e.silences, func(s Silence) bool { return now >= s.End })
	return before - len(e.silences)
}

// setSilenceField applies one field of a console line.
func setSilenceField(s *Silence, field string, now int64) error {
	key, value, ok := strings.Cut(field, "=")
	if !ok || value == "" {
		return ErrBadSilence
	}
	switch key {
	case "rule":
		s.Rule = value
	case "instance":
		s.Instance = value
	case "for":
		d, err := parseDuration(value)
		if err != nil || d == 0 {
			return ErrBadSilence
		}
		s.End = now + d
	default:
		return ErrBadSilence
	}
	return nil
}

// ParseSilence reads a console line. Start is now. It does not add the silence.
func (e *Engine) ParseSilence(line string) (Silence, error) {
	now := e.clock()
	s := Silence{Start: now}
	for _, field := range strings.Fields(line) {
		if err := setSilenceField(&s, field, now); err != nil {
			return Silence{}, err
		}
	}
	if s.Rule == "" || s.End == 0 {
		return Silence{}, ErrBadSilence
	}
	return s, nil
}
`,
		},
		Turns: script(
			cloud("Some ground rules. A sample older than 10 minutes is no data: it can't breach a rule, and it can't clear an alert either. Nothing evaluates yet, so that's for later. For today, add `func (e *Engine) RemoveRule(name string) error`, returning `ErrUnknownRule`, a new `errors.New(\"unknown rule\")`, when there's no such rule. Anything else that takes a rule name returns that too."),
			local("Read docs/routing.md and tell me in a few sentences where alerts go, resolved ones included. No code changes."),
			local("Read docs/rules-format.md and add `func ParseRule(line string) (Rule, error)` to rule.go, returning `ErrBadRule`, a new `errors.New(\"bad rule\")`, for the mistakes it lists."),
			cloud("Add evaluation. `type State string` with `StatePending` and `StateFiring`, `type Alert struct { Rule, Instance, Severity string; State State; Since, FiringSince int64; Value float64 }`, and `func (e *Engine) Evaluate()`, which checks every rule against the latest sample of each instance of its metric. A breach starts a pending alert with Since set. Once it has lasted the rule's For the alert is firing. `func (e *Engine) Get(rule, instance string) (Alert, bool)` reads one."),
			cloud("Alerts need to clear, without flapping. A pending alert goes away at the first good value. A firing one goes away only after 3 evaluations in a row found the value good, and a bad value in between starts that count over."),
			cloud("Two things for reports, no code for them today: any time we print looks like `02 Jan 15:04 UTC`, and any list of alerts has critical first, then warning, then info, and within that goes by rule name and then instance. Today's job is that engine.go is getting long, so move Evaluate, Alert and their helpers to a new evaluate.go. No behaviour changes."),
			local("Add a new report.go with `func (e *Engine) Breaching() []string`: `rule/instance` for every instance whose latest sample breaks its rule right now, sorted. It looks only at rules and samples, and changes nothing."),
			local("A note for later, no code for it yet: the pager gateway cuts messages off at 160 bytes and mangles the end, so we cut pager messages over that ourselves, to 157 bytes plus `...`. Today, add `func (e *Engine) DisableRule(name string) error` and `func (e *Engine) EnableRule(name string) error` to engine.go. A disabled rule isn't evaluated and its alerts go away without a word."),
			local("Add to report.go `func (e *Engine) Active() []Alert`, the firing alerts, and `func (e *Engine) Pending() []Alert`, the pending ones."),
			cloud("Add a new notify.go with `type Notification struct { Channel, Kind, Rule, Instance string; At int64; Text string }` and `func (e *Engine) Drain() []Notification`, which returns what's queued and empties the queue. When an alert starts firing, queue one notification of Kind firing per channel it goes to. When a firing alert goes away, the same with Kind resolved. Leave Text empty for now."),
			cloud("Fill in Text. Firing is `[SEVERITY] rule on instance: value since time`, with the severity in capitals, the value to two decimals and the time it started firing. Resolved is `[RESOLVED] rule on instance`."),
			cloud("An alert that stays firing should be notified again, but not more than once every 30 minutes."),
			local("Add a new silence.go with `type Silence struct { ID int; Rule, Instance string; Start, End int64 }` and `func (e *Engine) AddSilence(s Silence) (int, error)`, which gives it the next ID, counting from 1, and keeps it. `ErrBadSilence`, a new `errors.New(\"bad silence\")`, is for End not after Start. Rule is a rule's name or a prefix ending in *, and an empty Instance means all of them. It has no effect on alerts yet."),
			local("Add `func (e *Engine) RemoveSilence(id int) error`, with `ErrNoSilence`, a new `errors.New(\"no such silence\")`, and `func (e *Engine) ActiveSilences() []Silence`, those in force now, from Start up to but not including End. For when they get wired in: a silence only holds back notifications. Alerts move through their states as if there were no silence. When a silence ends on a firing alert it notifies right away, and an alert nobody was told about gets no resolved notice."),
			cloud("Wire the silences into notifications."),
			cloud("Add `func (e *Engine) Ack(rule, instance string) error` and a field `Acked bool` on Alert. An acknowledged alert stops repeating but still gets its resolved notice. `ErrNoAlert`, a new `errors.New(\"no firing alert\")`, is for an alert that isn't firing."),
			local("Add to report.go `func (e *Engine) Export() string`: CSV with the header `rule,instance,severity,state,since` and a line per alert, firing ones before pending ones. since is when it started firing, or for a pending one when the breach began."),
			local("Add to report.go `func (e *Engine) Explain(rule, instance string) (string, error)` for the console: `no data` if there's nothing to go on for that instance, `ok` if it has no alert, otherwise `pending since <time>` or `firing since <time>`."),
			cloud("On-call says the repeats are wrong: a critical alert should repeat every 10 minutes and anything else every 60 minutes. Please change it."),
			local("Add to report.go `func (e *Engine) NextRepeat(rule, instance string) (int64, bool)`: the clock reading at which the next repeat of a firing alert's notification is due, or false if it isn't firing or has had no notification yet."),
			cloud("Add to report.go `func (e *Engine) Summary() string`. The first line is `N firing, M pending`. Then a line per firing alert, with the severity in capitals, the rule, the instance and the time it started firing, separated by single spaces."),
			cloud("Add to silence.go `func (e *Engine) ParseSilence(line string) (Silence, error)` for the console. A line has `rule=<pattern>` and `for=<duration>` and optionally `instance=<name>`, in any order. Start is now and End follows from the duration. A line it can't read gets ErrBadSilence. It doesn't add the silence."),
			local("Add to report.go `func (e *Engine) Housekeep() int`: it throws away samples too old to count for anything, using Trim, and returns how many went."),
			local("Add to report.go `type Stats struct{ Rules, Pending, Firing, Silences int }` and `func (e *Engine) Stats() Stats`. Silences counts those in force."),
			cloud("Add to notify.go `type Digest struct { Channel string; Count int; Text string }` and `func (e *Engine) DrainDigests() []Digest`, for teams that want one message: one digest per channel, sorted by channel, with Text the texts of its notifications joined by newlines, oldest first. It empties the queue just as Drain does."),
			local("Add to silence.go `func (e *Engine) PruneSilences() int`: it removes the silences whose time is over and returns how many."),
			cloud("Add `func (e *Engine) Reload(rules []Rule) int` to engine.go, for when the rule file changes: it replaces all the rules. Alerts of rules that are gone are dropped without a word, and it returns how many. The others carry on where they are, under the rule's new severity."),
		),
		Carries: []Carry{
			{
				What:        "a sample older than 10 minutes is no data: it neither breaches nor clears",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 4, 6, 17, 22},
				Tests:       []string{"TestOldSampleDoesNotFire", "TestOldSampleDoesNotClear", "TestBreachingSkipsOldSamples", "TestExplainOldSampleIsNoData", "TestHousekeepDropsOldSamples"},
				Absent:      []string{"older than", "10 minutes old", "ten minutes old", "stale", "600"},
			},
			{
				What:        "anything taking a rule name returns ErrUnknownRule when there is no such rule",
				Kind:        CarryEmbodied,
				Established: 0,
				Needed:      []int{7, 12, 15, 17},
				Tests:       []string{"TestDisableUnknownRule", "TestAddSilenceUnknownRule", "TestAckUnknownRule", "TestExplainUnknownRule"},
				Absent:      []string{"errunknownrule", "unknown rule", "no such rule"},
			},
			{
				What:        "critical goes to pager then chat, warning to chat, info nowhere, resolved only to chat, default team oncall (docs/routing.md)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{9},
				Source:      "docs/routing.md",
				Tests:       []string{"TestCriticalGoesToPagerThenChat", "TestInfoNeverNotifies", "TestRuleWithoutTeamGoesToOncall", "TestResolvedGoesOnlyToChat", "TestPagerTextIsCut"},
				Absent:      []string{"routing.md", "pager-", "chat-", "oncall", "woken"},
			},
			{
				What:        "a bare number in a duration means minutes, in any text we read (docs/rules-format.md)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{21},
				Source:      "docs/rules-format.md",
				Tests:       []string{"TestParseSilenceBareNumberIsMinutes", "TestParseSilenceFields"},
				Absent:      []string{"rules-format.md", "bare number", "historic", "means five"},
			},
			{
				What:        "any time we print looks like 02 Jan 15:04 UTC",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{10, 16, 17, 20},
				Tests:       []string{"TestFiringTextTime", "TestExportTime", "TestExplainTime", "TestSummaryTime"},
				Absent:      []string{"02 jan", "15:04", "rfc"},
			},
			{
				What:        "any list of alerts has critical first, then warning, then info, then by rule and instance",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{8, 16, 20},
				Tests:       []string{"TestActiveCriticalFirst", "TestExportCriticalFirst", "TestSummaryCriticalFirst"},
				Absent:      []string{"critical first", "then warning", "then info"},
			},
			{
				What:        "pager messages over 160 bytes are cut to 157 bytes plus ...",
				Kind:        CarryUser,
				Established: 7,
				Needed:      []int{10},
				Tests:       []string{"TestPagerTextIsCut"},
				Absent:      []string{"160", "157", "gateway", "cuts"},
			},
			{
				What:        "a firing alert repeats, but within a limit, 30 minutes as first stated",
				Kind:        CarryUser,
				Established: 11,
				Needed:      []int{19},
				Tests:       []string{"TestNoRepeatInsideInterval", "TestWarningWaitsAnHour"},
				Absent:      []string{"30 minutes", "thirty", "once every", "notified again"},
			},
			{
				What:        "a silence only holds back notifications, notifies when it ends, and an untold alert gets no resolved notice",
				Kind:        CarryUser,
				Established: 13,
				Needed:      []int{14},
				Tests:       []string{"TestSilencedAlertStillFires", "TestSilenceEndNotifiesAtOnce", "TestNoResolvedWithoutNotice"},
				Absent:      []string{"holds back", "hold back", "as if there were", "right away", "nobody was told"},
			},
			{
				What:        "revised: critical repeats every 10 minutes and anything else every 60",
				Kind:        CarryUser,
				Established: 18,
				Needed:      []int{19},
				Tests:       []string{"TestCriticalRepeatsAfterTenMinutes", "TestWarningWaitsAnHour", "TestNextRepeatDue"},
				Absent:      []string{"every 10 minutes", "every 60 minutes", "on-call says"},
			},
		},
	}
}
