package s2

func task12() Task {
	return Task{
		ID:     "s2-12-access-rules",
		Set:    "tuning",
		System: system("an access rule engine"),
		Files: map[string]string{
			"go.mod": goMod,
			"rule.go": `package access

// Rule allows or denies a role access to a resource.
type Rule struct {
	Role     string
	Resource string
	Allow    bool
}
`,
			"policy.go": `package access

// Policy is a set of rules.
type Policy struct {
	rules []Rule
}

// Add appends a rule.
func (p *Policy) Add(r Rule) { p.rules = append(p.rules, r) }

// Rules returns a copy of the rules.
func (p *Policy) Rules() []Rule { return append([]Rule(nil), p.rules...) }
`,
			"docs/security-review.txt": `Security review notes

A rule resource ending in /* covers every path below that prefix, at any depth,
but not the prefix itself. There are no other wildcards. A resource with no
matching rule is denied.
`,
			"policy_test.go": `package access

import "testing"

func TestRules(t *testing.T) {
	var p Policy
	p.Add(Rule{Role: "dev", Resource: "a", Allow: true})
	if got := p.Rules(); len(got) != 1 || got[0].Role != "dev" {
		t.Fatalf("got %+v", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package access_test

import (
	"errors"
	"testing"

	access "fixture"
)

func allow(role, resource string) access.Rule {
	return access.Rule{Role: role, Resource: resource, Allow: true}
}

func deny(role, resource string) access.Rule {
	return access.Rule{Role: role, Resource: resource, Allow: false}
}

func policy(rules ...access.Rule) *access.Policy {
	p := &access.Policy{}
	for _, r := range rules {
		p.Add(r)
	}
	return p
}

func TestLen(t *testing.T) {
	if got := policy(allow("a", "x"), allow("b", "x")).Len(); got != 2 {
		t.Fatalf("Len = %d", got)
	}
}

func TestRoles(t *testing.T) {
	got := policy(allow("b", "x"), allow("a", "x"), deny("b", "y")).Roles()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Roles = %v", got)
	}
}

func TestCanPlainAllow(t *testing.T) {
	p := policy(allow("dev", "docs/a"))
	if !p.Can("dev", "docs/a") {
		t.Error("an allowed role was refused")
	}
	if p.Can("ops", "docs/a") || p.Can("dev", "docs/b") {
		t.Error("access without a matching rule was allowed")
	}
}

func TestRolesForPlain(t *testing.T) {
	got := policy(allow("b", "x"), allow("a", "x"), allow("c", "y")).RolesFor("x")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("RolesFor = %v", got)
	}
}

func TestCanDenyBeatsAllow(t *testing.T) {
	if policy(allow("dev", "x"), deny("dev", "x")).Can("dev", "x") {
		t.Error("deny added last lost")
	}
	if policy(deny("dev", "x"), allow("dev", "x")).Can("dev", "x") {
		t.Error("deny added first lost")
	}
}

func TestRolesForDenyBeatsAllow(t *testing.T) {
	got := policy(deny("dev", "x"), allow("dev", "x"), allow("ops", "x")).RolesFor("x")
	if len(got) != 1 || got[0] != "ops" {
		t.Fatalf("RolesFor = %v", got)
	}
}

func TestCanWildcard(t *testing.T) {
	p := policy(allow("dev", "docs/*"))
	for resource, want := range map[string]bool{"docs/a": true, "docs/a/b": true, "docs": false, "docsx/a": false} {
		if got := p.Can("dev", resource); got != want {
			t.Errorf("Can(%q) = %v", resource, got)
		}
	}
}

func TestRolesForWildcard(t *testing.T) {
	p := policy(allow("dev", "docs/*"), allow("ops", "docs/a"))
	if got := p.RolesFor("docs/a"); len(got) != 2 {
		t.Errorf("RolesFor(docs/a) = %v", got)
	}
	if got := p.RolesFor("docs"); len(got) != 0 {
		t.Errorf("RolesFor(docs) = %v", got)
	}
}

func TestCanLeadingSlash(t *testing.T) {
	if !policy(allow("dev", "docs/a")).Can("dev", "/docs/a") {
		t.Error("a query with a slash did not match a rule without one")
	}
	if !policy(allow("dev", "/docs/a")).Can("dev", "docs/a") {
		t.Error("a query without a slash did not match a rule with one")
	}
}

func TestRolesForLeadingSlash(t *testing.T) {
	if got := policy(allow("dev", "docs/a")).RolesFor("/docs/a"); len(got) != 1 {
		t.Fatalf("RolesFor = %v", got)
	}
}

func TestRemoveExisting(t *testing.T) {
	p := policy(allow("dev", "x"), allow("dev", "y"), allow("ops", "x"))
	if err := p.Remove("dev", "x"); err != nil {
		t.Fatal(err)
	}
	if p.Can("dev", "x") || !p.Can("dev", "y") || !p.Can("ops", "x") {
		t.Fatal("removed the wrong rules")
	}
}

func TestRemoveMissing(t *testing.T) {
	if err := policy(allow("dev", "x")).Remove("dev", "y"); !errors.Is(err, access.ErrNoRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveLeadingSlash(t *testing.T) {
	p := policy(allow("dev", "docs/a"))
	if err := p.Remove("dev", "/docs/a"); err != nil {
		t.Fatal(err)
	}
	if p.Len() != 0 {
		t.Fatalf("Len = %d", p.Len())
	}
}

func TestReplaceExisting(t *testing.T) {
	p := policy(allow("dev", "x"), allow("ops", "x"))
	if err := p.Replace("dev", "x", false); err != nil {
		t.Fatal(err)
	}
	if p.Can("dev", "x") || !p.Can("ops", "x") || p.Len() != 2 {
		t.Fatal("wrong rules after Replace")
	}
}

func TestReplaceLeadingSlash(t *testing.T) {
	p := policy(allow("dev", "docs/a"))
	if err := p.Replace("dev", "/docs/a", false); err != nil {
		t.Fatal(err)
	}
	if p.Can("dev", "docs/a") {
		t.Fatal("rule was not changed")
	}
}

func TestReplaceNoRuleSentinel(t *testing.T) {
	if err := policy(allow("dev", "x")).Replace("dev", "y", true); !errors.Is(err, access.ErrNoRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestVisible(t *testing.T) {
	got := policy(allow("dev", "b"), allow("dev", "a")).Visible("dev", []string{"b", "c", "a"})
	if len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("Visible = %v", got)
	}
}
`},
		Solution: map[string]string{
			"policy.go": `package access

import (
	"errors"
	"slices"
	"strings"
)

// ErrNoRule is what callers check for when a rule they name doesn't exist.
var ErrNoRule = errors.New("no such rule")

// Policy is a set of rules.
type Policy struct {
	rules []Rule
}

// Add appends a rule.
func (p *Policy) Add(r Rule) { p.rules = append(p.rules, r) }

// Rules returns a copy of the rules.
func (p *Policy) Rules() []Rule { return append([]Rule(nil), p.rules...) }

// Len returns the number of rules.
func (p *Policy) Len() int { return len(p.rules) }

// Roles returns the distinct roles, sorted.
func (p *Policy) Roles() []string {
	var out []string
	for _, r := range p.rules {
		if !slices.Contains(out, r.Role) {
			out = append(out, r.Role)
		}
	}
	slices.Sort(out)
	return out
}

func clean(resource string) string { return strings.TrimPrefix(resource, "/") }

func matches(pattern, resource string) bool {
	pattern, resource = clean(pattern), clean(resource)
	if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
		return strings.HasPrefix(resource, prefix+"/")
	}
	return pattern == resource
}

// Can reports whether the role may access the resource.
func (p *Policy) Can(role, resource string) bool {
	allowed := false
	for _, r := range p.rules {
		if r.Role == role && matches(r.Resource, resource) {
			if !r.Allow {
				return false
			}
			allowed = true
		}
	}
	return allowed
}

// RolesFor returns the roles that can access the resource, sorted.
func (p *Policy) RolesFor(resource string) []string {
	var out []string
	for _, role := range p.Roles() {
		if p.Can(role, resource) {
			out = append(out, role)
		}
	}
	return out
}

// Remove removes that role's rules for that resource.
func (p *Policy) Remove(role, resource string) error {
	kept := p.rules[:0:0]
	for _, r := range p.rules {
		if r.Role != role || clean(r.Resource) != clean(resource) {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(p.rules) {
		return ErrNoRule
	}
	p.rules = kept
	return nil
}

// Replace sets Allow on that role's rules for that resource.
func (p *Policy) Replace(role, resource string, allow bool) error {
	found := false
	for i, r := range p.rules {
		if r.Role == role && clean(r.Resource) == clean(resource) {
			p.rules[i].Allow = allow
			found = true
		}
	}
	if !found {
		return ErrNoRule
	}
	return nil
}

// Visible returns the given resources that the role can access.
func (p *Policy) Visible(role string, resources []string) []string {
	var out []string
	for _, resource := range resources {
		if p.Can(role, resource) {
			out = append(out, resource)
		}
	}
	return out
}
`,
		},
		Turns: script(
			local("Add `func (p *Policy) Len() int`. A principle for when we write the evaluator: an explicit deny beats any allow, whatever order the rules were added in."),
			local("Also keep in mind that the gateway sends resource paths with or without a leading slash: /docs/a and docs/a are the same resource, in rules and in queries alike. No code for that yet. Add `func (p *Policy) Roles() []string`: the distinct roles, sorted."),
			cloud("Read docs/security-review.txt and tell me what it requires of rule matching. No code changes."),
			cloud("Add `func (p *Policy) Can(role, resource string) bool`."),
			local("Add `func (p *Policy) RolesFor(resource string) []string`: the roles that can access the resource, sorted."),
			cloud("Add `var ErrNoRule = errors.New(\"no such rule\")`, which is what callers check for whenever a rule they name doesn't exist, and `func (p *Policy) Remove(role, resource string) error`, which removes that role's rules for that resource."),
			local("Add `func (p *Policy) Replace(role, resource string, allow bool) error`, which sets Allow on that role's rules for that resource."),
			cloud("Add `func (p *Policy) Visible(role string, resources []string) []string`: the given resources that the role can access, in the given order and spelled as given."),
		),
		Carries: []Carry{
			{
				What:        "an explicit deny beats any allow, in any order",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 4},
				Tests:       []string{"TestCanDenyBeatsAllow", "TestRolesForDenyBeatsAllow"},
				Absent:      []string{"deny", "beats"},
			},
			{
				What:        "resources with and without a leading slash are the same",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{3, 4, 5, 6},
				Tests:       []string{"TestCanLeadingSlash", "TestRolesForLeadingSlash", "TestRemoveLeadingSlash", "TestReplaceLeadingSlash"},
				Absent:      []string{"slash", "gateway"},
			},
			{
				What:        "/* covers paths below the prefix at any depth but not the prefix (docs/security-review.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{3, 4},
				Source:      "docs/security-review.txt",
				Tests:       []string{"TestCanWildcard", "TestRolesForWildcard"},
				Absent:      []string{"wildcard", "/*", "prefix"},
			},
			{
				What:        "ErrNoRule whenever a named rule doesn't exist",
				Kind:        CarryEmbodied,
				Established: 5,
				Needed:      []int{6},
				Tests:       []string{"TestReplaceNoRuleSentinel"},
				Absent:      []string{"errnorule", "doesn't exist"},
			},
		},
	}
}
