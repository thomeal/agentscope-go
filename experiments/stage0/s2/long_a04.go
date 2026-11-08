package s2

func longA04() Task {
	return Task{
		ID:     "s2-long-a04-claims-desk",
		Set:    "long",
		System: system("a claims handling desk for an insurance back office"),
		Files: map[string]string{
			"go.mod": goMod,
			"book.go": `package claims

import (
	"errors"
	"sort"
)

var (
	// ErrDuplicatePolicy is returned when a policy number is already in the book.
	ErrDuplicatePolicy = errors.New("duplicate policy")
	// ErrUnknownPolicy is returned for a number no policy in the book has.
	ErrUnknownPolicy = errors.New("unknown policy")
)

// Policy is one insurance contract. Amounts are in cents and days are plain day
// numbers, the way the policy system counts them. Start and End are the first and
// last day of cover.
type Policy struct {
	Number      string
	Holder      string
	Cover       string // "contents", "buildings" or "motor"
	LimitCents  int
	ExcessCents int
	Start       int
	End         int
}

// Book holds the policies and the claims made on them, in the order notified.
type Book struct {
	policies map[string]Policy
	claims   []*Claim
	next     int
	panel    map[string]bool
}

// New returns an empty book.
func New() *Book {
	return &Book{policies: map[string]Policy{}, next: 1, panel: map[string]bool{}}
}

// AddPolicy puts a policy in the book.
func (b *Book) AddPolicy(p Policy) error {
	if _, ok := b.policies[p.Number]; ok {
		return ErrDuplicatePolicy
	}
	b.policies[p.Number] = p
	return nil
}

// Policy returns a policy by number.
func (b *Book) Policy(number string) (Policy, error) {
	p, ok := b.policies[number]
	if !ok {
		return Policy{}, ErrUnknownPolicy
	}
	return p, nil
}

// Numbers returns every policy number, sorted.
func (b *Book) Numbers() []string {
	out := make([]string, 0, len(b.policies))
	for number := range b.policies {
		out = append(out, number)
	}
	sort.Strings(out)
	return out
}
`,
			"claim.go": `package claims

import "fmt"

// Claim is one request for payment. Event names the incident behind it, and all
// claims from one incident on a policy share it. AgeYears is the age of the damaged
// goods in full years. Status starts as "notified".
type Claim struct {
	ID            string
	Policy        string
	Event         string
	LossDay       int
	NotifiedDay   int
	ClaimedCents  int
	AgeYears      int
	Status        string
	AssessedCents int
	PaidCents     int
	PaidDay       int
	Recovered     int
	Assessor      string
}

// Notify records a new claim and returns its ID.
func (b *Book) Notify(policy, event string, lossDay, notifiedDay, claimedCents, ageYears int) (string, error) {
	if _, err := b.Policy(policy); err != nil {
		return "", err
	}
	c := &Claim{
		ID: fmt.Sprintf("C-%04d", b.next), Policy: policy, Event: event, LossDay: lossDay, NotifiedDay: notifiedDay,
		ClaimedCents: claimedCents, AgeYears: ageYears, Status: "notified",
	}
	b.next++
	b.claims = append(b.claims, c)
	return c.ID, nil
}

// find returns the claim with an ID, or nil.
func (b *Book) find(id string) *Claim {
	for _, c := range b.claims {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// Get returns a copy of a claim.
func (b *Book) Get(id string) (Claim, bool) {
	c := b.find(id)
	if c == nil {
		return Claim{}, false
	}
	return *c, true
}
`,
			"money.go": `package claims

import "fmt"

// Euros writes an amount in cents as euros with two decimals, like 1234.50.
func Euros(cents int) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// Cents is the reverse of Euros for a whole number of euros.
func Cents(euros int) int { return euros * 100 }
`,
			"report.go": `package claims

import (
	"fmt"
	"strings"
)

// Overview returns the short text at the top of the desk's morning sheet.
func (b *Book) Overview() string {
	var w strings.Builder
	fmt.Fprintf(&w, "policies: %d\n", len(b.policies))
	fmt.Fprintf(&w, "claims: %d\n", len(b.claims))
	return w.String()
}

// ByCover returns how many policies there are of each kind of cover.
func (b *Book) ByCover() map[string]int {
	out := map[string]int{}
	for _, p := range b.policies {
		out[p.Cover]++
	}
	return out
}
`,
			"docs/underwriting-guide.txt": `Underwriting guide, extract for claims staff

Cover period. A loss is covered if it falls on any day from the policy's Start to
its End, both days included.

Notice. A claim has to be notified within 30 days of the loss, and day 30 itself is
still in time. Buildings claims get 60 days on the same terms.

Depreciation. Damaged goods are paid at their depreciated value, which depends on
the cover and the goods' age in full years:

  contents    10 percent per full year, at most 50 percent
  motor       15 percent per full year, at most 60 percent
  buildings   none

Glass breakage on motor policies is handled by the windscreen partner and does not
come through this desk. Legal expenses cover is written by another company. Valuers
are appointed by the regional offices and their fees are not part of a claim.
`,
			"docs/bordereau-format.txt": `Bordereau for the reinsurer

Each month we send the reinsurer a text file of the large claims we have paid.
Every line, the last one too, ends in a line feed. The first line is:

  BDX,<first day>,<last day>

Then there is one line for each claim paid on a day in that range, both days
included, whose paid amount is above our retention of 25,000.00:

  <claim id>,<policy number>,"<holder>",<paid>,<ceded>

The holder is in double quotes. Ceded is the part of the paid amount above the
retention. Amounts are written the way Euros writes them. Lines are in order of paid
amount, the largest first, and ties go by claim ID. The last line is:

  TOTAL,<number of claims listed>,<sum of ceded>

The reinsurer pays on the total line, so a file without one gets rejected at their
end. Smaller claims stay with us and are never mentioned in this file.
`,
			"claims_test.go": `package claims

import (
	"errors"
	"slices"
	"testing"
)

func testPolicy(number string) Policy {
	return Policy{Number: number, Holder: "Ann Lee", Cover: "contents", LimitCents: 100000, Start: 1, End: 400}
}

func TestAddPolicyRefusesDuplicate(t *testing.T) {
	b := New()
	if err := b.AddPolicy(testPolicy("P1")); err != nil {
		t.Fatal(err)
	}
	if err := b.AddPolicy(testPolicy("P1")); !errors.Is(err, ErrDuplicatePolicy) {
		t.Fatalf("second AddPolicy: %v", err)
	}
}

func TestNumbersAreSorted(t *testing.T) {
	b := New()
	for _, number := range []string{"P2", "P1"} {
		if err := b.AddPolicy(testPolicy(number)); err != nil {
			t.Fatal(err)
		}
	}
	if got := b.Numbers(); !slices.Equal(got, []string{"P1", "P2"}) {
		t.Fatalf("Numbers = %v", got)
	}
}

func TestNotifyNumbersClaims(t *testing.T) {
	b := New()
	if err := b.AddPolicy(testPolicy("P1")); err != nil {
		t.Fatal(err)
	}
	id, err := b.Notify("P1", "storm", 10, 11, 1000, 0)
	if err != nil || id != "C-0001" {
		t.Fatalf("Notify = %q, %v", id, err)
	}
	if _, err := b.Notify("P9", "storm", 10, 11, 1000, 0); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatalf("unknown policy: %v", err)
	}
}

func TestEurosFormat(t *testing.T) {
	if got := Euros(123450); got != "1234.50" {
		t.Fatalf("Euros = %q", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package claims_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	claims "fixture"
)

// home returns a buildings policy, which has no depreciation, with a large limit.
func home(number string, excess int) claims.Policy {
	return claims.Policy{Number: number, Holder: "Ann Lee", Cover: "buildings", LimitCents: 100000000, ExcessCents: excess, Start: 100, End: 465}
}

func book(t *testing.T, policies ...claims.Policy) *claims.Book {
	t.Helper()
	b := claims.New()
	for _, p := range policies {
		if err := b.AddPolicy(p); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func notify(t *testing.T, b *claims.Book, policy, event string, cents int) string {
	t.Helper()
	id, err := b.Notify(policy, event, 200, 201, cents, 0)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// assessed returns a new claim that has been assessed at the amount claimed.
func assessed(t *testing.T, b *claims.Book, policy, event string, cents int) string {
	t.Helper()
	id := notify(t, b, policy, event, cents)
	if err := b.Assess(id, cents); err != nil {
		t.Fatal(err)
	}
	return id
}

// paid returns a new claim taken all the way through to being paid on a day.
func paid(t *testing.T, b *claims.Book, policy string, cents, day int) string {
	t.Helper()
	id := assessed(t, b, policy, "event-"+policy+"-"+claims.Euros(cents), cents)
	if err := b.Approve(id, "manager"); err != nil {
		t.Fatal(err)
	}
	if err := b.Pay(id, day); err != nil {
		t.Fatal(err)
	}
	return id
}

func payout(t *testing.T, b *claims.Book, id string) int {
	t.Helper()
	got, err := b.Payout(id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPolicyCountCounts(t *testing.T) {
	if got := book(t, home("P1", 0), home("P2", 0)).PolicyCount(); got != 2 {
		t.Errorf("PolicyCount = %d", got)
	}
}

func TestOpenClaimsCount(t *testing.T) {
	b := book(t, home("P1", 0))
	notify(t, b, "P1", "a", 100)
	assessed(t, b, "P1", "b", 100)
	paid(t, b, "P1", 100, 300)
	if err := b.Decline(notify(t, b, "P1", "c", 100)); err != nil {
		t.Fatal(err)
	}
	if got := b.OpenClaims(); got != 2 {
		t.Errorf("OpenClaims = %d", got)
	}
}

func TestClaimsOfAPolicy(t *testing.T) {
	b := book(t, home("P1", 0), home("P2", 0))
	first := notify(t, b, "P1", "a", 100)
	notify(t, b, "P2", "a", 100)
	second := notify(t, b, "P1", "b", 100)
	got, err := b.Claims("P1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != first || got[1].ID != second {
		t.Errorf("Claims = %+v", got)
	}
	if _, err := b.Claims("P9"); !errors.Is(err, claims.ErrUnknownPolicy) {
		t.Errorf("unknown policy: %v", err)
	}
}

func TestHoldersOnceSorted(t *testing.T) {
	a, c, d := home("P1", 0), home("P2", 0), home("P3", 0)
	a.Holder, c.Holder, d.Holder = "Zed", "Ann", "Zed"
	if got := book(t, a, c, d).Holders(); !slices.Equal(got, []string{"Ann", "Zed"}) {
		t.Errorf("Holders = %v", got)
	}
}

func TestTermsOfAPolicy(t *testing.T) {
	b := book(t, home("P1", 300))
	if limit, excess, err := b.Terms("P1"); err != nil || limit != 100000000 || excess != 300 {
		t.Errorf("Terms = %d, %d, %v", limit, excess, err)
	}
	if _, _, err := b.Terms("P9"); !errors.Is(err, claims.ErrUnknownPolicy) {
		t.Errorf("unknown policy: %v", err)
	}
}

func TestAssessRecords(t *testing.T) {
	b := book(t, home("P1", 0))
	id := notify(t, b, "P1", "a", 1000)
	if err := b.Assess(id, 800); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Get(id); got.Status != "assessed" || got.AssessedCents != 800 {
		t.Errorf("claim = %+v", got)
	}
}

func TestAssessOnlyFromNotified(t *testing.T) {
	b := book(t, home("P1", 0))
	id := assessed(t, b, "P1", "a", 1000)
	if err := b.Assess(id, 900); !errors.Is(err, claims.ErrBadMove) {
		t.Errorf("second Assess: %v", err)
	}
}

func TestDeclineRules(t *testing.T) {
	b := book(t, home("P1", 0))
	first := notify(t, b, "P1", "a", 1000)
	second := assessed(t, b, "P1", "b", 1000)
	for _, id := range []string{first, second} {
		if err := b.Decline(id); err != nil {
			t.Errorf("Decline(%s): %v", id, err)
		}
		if got, _ := b.Get(id); got.Status != "declined" {
			t.Errorf("Status = %q", got.Status)
		}
	}
	third := assessed(t, b, "P1", "c", 1000)
	if err := b.Approve(third, "manager"); err != nil {
		t.Fatal(err)
	}
	if err := b.Decline(third); !errors.Is(err, claims.ErrBadMove) {
		t.Errorf("after approval: %v", err)
	}
}

func TestAssessUnknown(t *testing.T) {
	if err := book(t).Assess("C-9", 100); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestDeclineUnknown(t *testing.T) {
	if err := book(t).Decline("C-9"); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestPayoutUnknown(t *testing.T) {
	if _, err := book(t).Payout("C-9"); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestApproveUnknown(t *testing.T) {
	if err := book(t).Approve("C-9", "manager"); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestPayUnknown(t *testing.T) {
	if err := book(t).Pay("C-9", 300); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestReserveUnknown(t *testing.T) {
	if _, err := book(t).Reserve("C-9"); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestShortfallUnknown(t *testing.T) {
	if _, err := book(t).Shortfall("C-9"); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestFlaggedUnknown(t *testing.T) {
	if _, err := book(t).Flagged("C-9"); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestRecoverUnknown(t *testing.T) {
	if _, _, err := book(t).Recover("C-9", 100); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestPayoutTakesExcess(t *testing.T) {
	b := book(t, home("P1", 300))
	if got := payout(t, b, assessed(t, b, "P1", "a", 1000)); got != 700 {
		t.Errorf("Payout = %d", got)
	}
}

func TestPayoutNotBelowZero(t *testing.T) {
	b := book(t, home("P1", 300))
	if got := payout(t, b, assessed(t, b, "P1", "a", 100)); got != 0 {
		t.Errorf("Payout = %d", got)
	}
}

func TestExcessSeparatePerIncident(t *testing.T) {
	b := book(t, home("P1", 300))
	assessed(t, b, "P1", "a", 1000)
	if got := payout(t, b, assessed(t, b, "P1", "b", 1000)); got != 700 {
		t.Errorf("Payout = %d", got)
	}
}

func TestExcessOncePerIncident(t *testing.T) {
	b := book(t, home("P1", 300))
	assessed(t, b, "P1", "a", 1000)
	if got := payout(t, b, assessed(t, b, "P1", "a", 1000)); got != 1000 {
		t.Errorf("Payout = %d", got)
	}
}

func TestExcessCarriesToNextClaim(t *testing.T) {
	b := book(t, home("P1", 300))
	assessed(t, b, "P1", "a", 200)
	if got := payout(t, b, assessed(t, b, "P1", "a", 1000)); got != 900 {
		t.Errorf("Payout = %d", got)
	}
}

func TestDeclinedClaimTakesNoExcess(t *testing.T) {
	b := book(t, home("P1", 300))
	if err := b.Decline(assessed(t, b, "P1", "a", 1000)); err != nil {
		t.Fatal(err)
	}
	if got := payout(t, b, assessed(t, b, "P1", "a", 1000)); got != 700 {
		t.Errorf("Payout = %d", got)
	}
}

func TestExcessLeftAfterClaim(t *testing.T) {
	b := book(t, home("P1", 300))
	if got, err := b.ExcessLeft("P1", "a"); err != nil || got != 300 {
		t.Errorf("before any claim: %d, %v", got, err)
	}
	assessed(t, b, "P1", "a", 200)
	if got, err := b.ExcessLeft("P1", "a"); err != nil || got != 100 {
		t.Errorf("after a claim: %d, %v", got, err)
	}
	if got, err := b.ExcessLeft("P1", "b"); err != nil || got != 300 {
		t.Errorf("another incident: %d, %v", got, err)
	}
}

func TestIncidentTotalOneExcess(t *testing.T) {
	b := book(t, home("P1", 300))
	assessed(t, b, "P1", "a", 1000)
	assessed(t, b, "P1", "a", 500)
	assessed(t, b, "P1", "b", 9000)
	if got, err := b.IncidentTotal("P1", "a"); err != nil || got != 1200 {
		t.Errorf("IncidentTotal = %d, %v", got, err)
	}
}

func TestPayoutLimitBeforeExcess(t *testing.T) {
	p := home("P1", 500)
	p.LimitCents = 10000
	b := book(t, p)
	if got := payout(t, b, assessed(t, b, "P1", "a", 12000)); got != 9500 {
		t.Errorf("Payout = %d", got)
	}
}

func TestEstimateBelowLimit(t *testing.T) {
	if got, err := book(t, home("P1", 300)).Estimate("P1", 1000); err != nil || got != 700 {
		t.Errorf("Estimate = %d, %v", got, err)
	}
}

func TestEstimateAboveLimit(t *testing.T) {
	p := home("P1", 500)
	p.LimitCents = 10000
	if got, err := book(t, p).Estimate("P1", 12000); err != nil || got != 9500 {
		t.Errorf("Estimate = %d, %v", got, err)
	}
}

func TestMaxPayoutTakesOffExcess(t *testing.T) {
	p := home("P1", 500)
	p.LimitCents = 10000
	if got, err := book(t, p).MaxPayout("P1"); err != nil || got != 9500 {
		t.Errorf("MaxPayout = %d, %v", got, err)
	}
}

func TestNotifyInsideCover(t *testing.T) {
	b := book(t, home("P1", 0))
	for _, day := range []int{100, 465} {
		if _, err := b.Notify("P1", "a", day, day, 100, 0); err != nil {
			t.Errorf("loss on day %d: %v", day, err)
		}
	}
}

func TestNotifyOutsideCover(t *testing.T) {
	b := book(t, home("P1", 0))
	for _, day := range []int{99, 466} {
		if _, err := b.Notify("P1", "a", day, day, 100, 0); !errors.Is(err, claims.ErrNotCovered) {
			t.Errorf("loss on day %d: %v", day, err)
		}
	}
}

func TestNotifyNoticeContents(t *testing.T) {
	p := home("P1", 0)
	p.Cover = "contents"
	b := book(t, p)
	if _, err := b.Notify("P1", "a", 200, 230, 100, 0); err != nil {
		t.Errorf("day 30: %v", err)
	}
	if _, err := b.Notify("P1", "a", 200, 231, 100, 0); !errors.Is(err, claims.ErrLateNotice) {
		t.Errorf("day 31: %v", err)
	}
}

func TestNotifyNoticeBuildings(t *testing.T) {
	b := book(t, home("P1", 0))
	if _, err := b.Notify("P1", "a", 200, 260, 100, 0); err != nil {
		t.Errorf("day 60: %v", err)
	}
	if _, err := b.Notify("P1", "a", 200, 261, 100, 0); !errors.Is(err, claims.ErrLateNotice) {
		t.Errorf("day 61: %v", err)
	}
}

func TestDepreciationContents(t *testing.T) {
	if got := claims.Depreciated("contents", 2, 10000); got != 8000 {
		t.Errorf("Depreciated = %d", got)
	}
}

func TestDepreciationMotor(t *testing.T) {
	if got := claims.Depreciated("motor", 2, 10000); got != 7000 {
		t.Errorf("Depreciated = %d", got)
	}
}

func TestDepreciationCapped(t *testing.T) {
	if got := claims.Depreciated("contents", 9, 10000); got != 5000 {
		t.Errorf("contents = %d", got)
	}
	if got := claims.Depreciated("motor", 9, 10000); got != 4000 {
		t.Errorf("motor = %d", got)
	}
}

func TestDepreciationNoneOnBuildings(t *testing.T) {
	if got := claims.Depreciated("buildings", 9, 10000); got != 10000 {
		t.Errorf("Depreciated = %d", got)
	}
}

func TestPayoutDepreciatesFirst(t *testing.T) {
	p := home("P1", 300)
	p.Cover = "contents"
	b := book(t, p)
	id, err := b.Notify("P1", "a", 200, 201, 10000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Assess(id, 10000); err != nil {
		t.Fatal(err)
	}
	if got := payout(t, b, id); got != 7700 {
		t.Errorf("Payout = %d", got)
	}
}

func TestDepreciationRounding(t *testing.T) {
	if got := claims.Depreciated("contents", 1, 105); got != 95 {
		t.Errorf("Depreciated = %d", got)
	}
}

func TestPayoutKeepsTheRounding(t *testing.T) {
	p := home("P1", 0)
	p.Cover = "contents"
	b := book(t, p)
	id, err := b.Notify("P1", "a", 200, 201, 105, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Assess(id, 105); err != nil {
		t.Fatal(err)
	}
	if got := payout(t, b, id); got != 95 {
		t.Errorf("Payout = %d", got)
	}
}

func TestApproveNeedsAssessment(t *testing.T) {
	b := book(t, home("P1", 0))
	id := notify(t, b, "P1", "a", 100)
	if err := b.Approve(id, "manager"); !errors.Is(err, claims.ErrBadMove) {
		t.Errorf("err = %v", err)
	}
	if got, _ := b.Get(id); got.Status != "notified" {
		t.Errorf("Status = %q", got.Status)
	}
}

func TestPayNeedsApproval(t *testing.T) {
	b := book(t, home("P1", 0))
	id := assessed(t, b, "P1", "a", 100)
	if err := b.Pay(id, 300); !errors.Is(err, claims.ErrBadMove) {
		t.Errorf("err = %v", err)
	}
	if got, _ := b.Get(id); got.Status != "assessed" || got.PaidCents != 0 {
		t.Errorf("claim = %+v", got)
	}
}

func TestPayRecords(t *testing.T) {
	b := book(t, home("P1", 300))
	id := paid(t, b, "P1", 1000, 300)
	if got, _ := b.Get(id); got.Status != "paid" || got.PaidCents != 700 || got.PaidDay != 300 {
		t.Errorf("claim = %+v", got)
	}
}

func TestHandlerLimit(t *testing.T) {
	b := book(t, home("P1", 0))
	if err := b.Approve(assessed(t, b, "P1", "a", 500000), "handler"); err != nil {
		t.Errorf("at the limit: %v", err)
	}
	if err := b.Approve(assessed(t, b, "P1", "b", 500001), "handler"); !errors.Is(err, claims.ErrAuthority) {
		t.Errorf("over the limit: %v", err)
	}
}

func TestSeniorLimit(t *testing.T) {
	b := book(t, home("P1", 0))
	if err := b.Approve(assessed(t, b, "P1", "a", 5000000), "senior"); err != nil {
		t.Errorf("at the limit: %v", err)
	}
	if err := b.Approve(assessed(t, b, "P1", "b", 5000001), "senior"); !errors.Is(err, claims.ErrAuthority) {
		t.Errorf("over the limit: %v", err)
	}
}

func TestManagerApprovesAnything(t *testing.T) {
	b := book(t, home("P1", 0))
	if err := b.Approve(assessed(t, b, "P1", "a", 90000000), "manager"); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestApprovalGoesByPayout(t *testing.T) {
	b := book(t, home("P1", 0))
	id := notify(t, b, "P1", "a", 9000000)
	if err := b.Assess(id, 400000); err != nil {
		t.Fatal(err)
	}
	if err := b.Approve(id, "handler"); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestReserveBeforeAssessment(t *testing.T) {
	b := book(t, home("P1", 300))
	if got, err := b.Reserve(notify(t, b, "P1", "a", 1000)); err != nil || got != 1000 {
		t.Errorf("Reserve = %d, %v", got, err)
	}
}

func TestReserveAfterAssessment(t *testing.T) {
	b := book(t, home("P1", 300))
	id := notify(t, b, "P1", "a", 5000)
	if err := b.Assess(id, 1000); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Reserve(id); err != nil || got != 700 {
		t.Errorf("Reserve = %d, %v", got, err)
	}
}

func TestReserveGoneWhenDeclined(t *testing.T) {
	b := book(t, home("P1", 0))
	id := assessed(t, b, "P1", "a", 1000)
	if err := b.Decline(id); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Reserve(id); err != nil || got != 0 {
		t.Errorf("Reserve = %d, %v", got, err)
	}
}

func TestReserveGoneWhenPaid(t *testing.T) {
	b := book(t, home("P1", 0))
	if got, err := b.Reserve(paid(t, b, "P1", 1000, 300)); err != nil || got != 0 {
		t.Errorf("Reserve = %d, %v", got, err)
	}
}

func TestTotalReserveAddsUp(t *testing.T) {
	b := book(t, home("P1", 0))
	notify(t, b, "P1", "a", 1000)
	id := notify(t, b, "P1", "b", 5000)
	if err := b.Assess(id, 2000); err != nil {
		t.Fatal(err)
	}
	paid(t, b, "P1", 700, 300)
	if got := b.TotalReserve(); got != 3000 {
		t.Errorf("TotalReserve = %d", got)
	}
}

func TestShortfallClaimedLessPayout(t *testing.T) {
	b := book(t, home("P1", 100))
	id := notify(t, b, "P1", "a", 1000)
	if err := b.Assess(id, 800); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Shortfall(id); err != nil || got != 300 {
		t.Errorf("Shortfall = %d, %v", got, err)
	}
}

func TestPaidTotalAddsUp(t *testing.T) {
	b := book(t, home("P1", 0))
	paid(t, b, "P1", 1000, 300)
	paid(t, b, "P1", 500, 300)
	assessed(t, b, "P1", "z", 9000)
	if got := b.PaidTotal(); got != 1500 {
		t.Errorf("PaidTotal = %d", got)
	}
}

func TestClaimsByStatusCounts(t *testing.T) {
	b := book(t, home("P1", 0))
	notify(t, b, "P1", "a", 100)
	notify(t, b, "P1", "b", 100)
	paid(t, b, "P1", 100, 300)
	got := b.ClaimsByStatus()
	if got["notified"] != 2 || got["paid"] != 1 || len(got) != 2 {
		t.Errorf("ClaimsByStatus = %v", got)
	}
}

func TestAgedOpenOldestFirst(t *testing.T) {
	b := book(t, home("P1", 0))
	mid, err := b.Notify("P1", "a", 220, 220, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	old, err := b.Notify("P1", "b", 210, 210, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Notify("P1", "c", 250, 250, 100, 0); err != nil {
		t.Fatal(err)
	}
	gone, err := b.Notify("P1", "d", 205, 205, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Decline(gone); err != nil {
		t.Fatal(err)
	}
	if got := b.AgedOpen(270); !slices.Equal(got, []string{old, mid}) {
		t.Errorf("AgedOpen = %v", got)
	}
}

func TestRecoverRepaysUsFirst(t *testing.T) {
	b := book(t, home("P1", 300))
	id := paid(t, b, "P1", 1000, 300)
	if us, holder, err := b.Recover(id, 500); err != nil || us != 500 || holder != 0 {
		t.Errorf("first Recover = %d, %d, %v", us, holder, err)
	}
	if us, holder, err := b.Recover(id, 500); err != nil || us != 200 || holder != 300 {
		t.Errorf("second Recover = %d, %d, %v", us, holder, err)
	}
}

func TestRecoverNeedsPaidClaim(t *testing.T) {
	b := book(t, home("P1", 0))
	if _, _, err := b.Recover(assessed(t, b, "P1", "a", 100), 50); !errors.Is(err, claims.ErrBadMove) {
		t.Errorf("err = %v", err)
	}
}

func TestFlaggedEarlyLoss(t *testing.T) {
	b := book(t, home("P1", 0))
	early, err := b.Notify("P1", "a", 114, 114, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	later, err := b.Notify("P1", "b", 115, 115, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.Flagged(early); err != nil || !got {
		t.Errorf("14 days in: %v, %v", got, err)
	}
	if got, err := b.Flagged(later); err != nil || got {
		t.Errorf("15 days in: %v, %v", got, err)
	}
}

func TestFlaggedLargeClaim(t *testing.T) {
	p := home("P1", 0)
	p.LimitCents = 10000
	b := book(t, p)
	large := notify(t, b, "P1", "a", 8000)
	smaller := notify(t, b, "P1", "b", 7999)
	if got, err := b.Flagged(large); err != nil || !got {
		t.Errorf("at the line: %v, %v", got, err)
	}
	if got, err := b.Flagged(smaller); err != nil || got {
		t.Errorf("under the line: %v, %v", got, err)
	}
}

func TestReviewQueueOrder(t *testing.T) {
	p := home("P1", 0)
	p.LimitCents = 10000
	b := book(t, p)
	small := notify(t, b, "P1", "a", 8000)
	large := notify(t, b, "P1", "b", 9000)
	notify(t, b, "P1", "c", 100)
	if err := b.Decline(notify(t, b, "P1", "d", 9500)); err != nil {
		t.Fatal(err)
	}
	if got := b.ReviewQueue(); !slices.Equal(got, []string{large, small}) {
		t.Errorf("ReviewQueue = %v", got)
	}
}

func panelBook(t *testing.T) *claims.Book {
	t.Helper()
	b := book(t, home("P1", 0))
	b.AddAssessor("lou")
	b.AddAssessor("kim")
	return b
}

func TestAssessorsSorted(t *testing.T) {
	if got := panelBook(t).Assessors(); !slices.Equal(got, []string{"kim", "lou"}) {
		t.Errorf("Assessors = %v", got)
	}
}

func TestAssignAssessorRecords(t *testing.T) {
	b := panelBook(t)
	id := notify(t, b, "P1", "a", 100)
	if err := b.AssignAssessor(id, "kim"); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Get(id); got.Assessor != "kim" {
		t.Errorf("Assessor = %q", got.Assessor)
	}
	if err := b.AssignAssessor(id, "zed"); !errors.Is(err, claims.ErrNoAssessor) {
		t.Errorf("not on the panel: %v", err)
	}
}

func TestAssignAssessorUnknown(t *testing.T) {
	if err := panelBook(t).AssignAssessor("C-9", "kim"); !errors.Is(err, claims.ErrUnknownClaim) {
		t.Errorf("err = %v", err)
	}
}

func TestIncidentStaysWithOneAssessor(t *testing.T) {
	b := panelBook(t)
	first := notify(t, b, "P1", "a", 100)
	second := notify(t, b, "P1", "a", 100)
	if err := b.AssignAssessor(first, "kim"); err != nil {
		t.Fatal(err)
	}
	if err := b.AssignAssessor(second, "lou"); !errors.Is(err, claims.ErrAssessorTaken) {
		t.Errorf("err = %v", err)
	}
	if got, _ := b.Get(second); got.Assessor != "" {
		t.Errorf("Assessor = %q", got.Assessor)
	}
}

func TestOtherIncidentsAreFree(t *testing.T) {
	b := panelBook(t)
	first := notify(t, b, "P1", "a", 100)
	other := notify(t, b, "P1", "b", 100)
	same := notify(t, b, "P1", "a", 100)
	if err := b.AssignAssessor(first, "kim"); err != nil {
		t.Fatal(err)
	}
	if err := b.AssignAssessor(other, "lou"); err != nil {
		t.Errorf("another incident: %v", err)
	}
	if err := b.AssignAssessor(same, "kim"); err != nil {
		t.Errorf("same assessor: %v", err)
	}
}

func TestWorkloadCounts(t *testing.T) {
	b := panelBook(t)
	for _, event := range []string{"a", "b", "c"} {
		if err := b.AssignAssessor(notify(t, b, "P1", event, 100), "kim"); err != nil {
			t.Fatal(err)
		}
	}
	gone := notify(t, b, "P1", "d", 100)
	if err := b.AssignAssessor(gone, "kim"); err != nil {
		t.Fatal(err)
	}
	if err := b.Decline(gone); err != nil {
		t.Fatal(err)
	}
	if got := b.Workload(); got["kim"] != 3 || len(got) != 1 {
		t.Errorf("Workload = %v", got)
	}
}

func bordereauBook(t *testing.T) (*claims.Book, string, string) {
	t.Helper()
	b := book(t, home("P1", 0))
	smaller := paid(t, b, "P1", 3000000, 300)
	larger := paid(t, b, "P1", 4000050, 305)
	paid(t, b, "P1", 2500000, 301)
	paid(t, b, "P1", 9000000, 500)
	return b, smaller, larger
}

func TestBordereauHeader(t *testing.T) {
	b, _, _ := bordereauBook(t)
	first, _, _ := strings.Cut(b.Bordereau(300, 330), "\n")
	if first != "BDX,300,330" {
		t.Errorf("first line %q", first)
	}
}

func TestBordereauRows(t *testing.T) {
	b, smaller, larger := bordereauBook(t)
	want := "BDX,300,330\n" +
		larger + ",P1,\"Ann Lee\",40000.50,15000.50\n" +
		smaller + ",P1,\"Ann Lee\",30000.00,5000.00\n" +
		"TOTAL,2,20000.50\n"
	if got := b.Bordereau(300, 330); got != want {
		t.Errorf("Bordereau = %q", got)
	}
}

func TestBordereauLeavesOutSmallClaims(t *testing.T) {
	b := book(t, home("P1", 0))
	paid(t, b, "P1", 2500000, 300)
	if got := b.Bordereau(300, 330); strings.Contains(got, "C-") {
		t.Errorf("Bordereau = %q", got)
	}
}

func TestBordereauRangeBothEnds(t *testing.T) {
	b, smaller, larger := bordereauBook(t)
	got := b.Bordereau(300, 305)
	if !strings.Contains(got, smaller) || !strings.Contains(got, larger) {
		t.Errorf("Bordereau = %q", got)
	}
}

func TestBordereauEndsWithTotal(t *testing.T) {
	b, _, _ := bordereauBook(t)
	if got := b.Bordereau(300, 330); !strings.HasSuffix(got, "TOTAL,2,20000.50\n") {
		t.Errorf("Bordereau = %q", got)
	}
}
`},
		Solution: map[string]string{
			"book.go": `package claims

import (
	"errors"
	"sort"
)

var (
	// ErrDuplicatePolicy is returned when a policy number is already in the book.
	ErrDuplicatePolicy = errors.New("duplicate policy")
	// ErrUnknownPolicy is returned for a number no policy in the book has.
	ErrUnknownPolicy = errors.New("unknown policy")
)

// Policy is one insurance contract. Amounts are in cents and days are plain day
// numbers, the way the policy system counts them. Start and End are the first and
// last day of cover.
type Policy struct {
	Number      string
	Holder      string
	Cover       string // "contents", "buildings" or "motor"
	LimitCents  int
	ExcessCents int
	Start       int
	End         int
}

// Book holds the policies and the claims made on them, in the order notified.
type Book struct {
	policies map[string]Policy
	claims   []*Claim
	next     int
	panel    map[string]bool
}

// New returns an empty book.
func New() *Book {
	return &Book{policies: map[string]Policy{}, next: 1, panel: map[string]bool{}}
}

// AddPolicy puts a policy in the book.
func (b *Book) AddPolicy(p Policy) error {
	if _, ok := b.policies[p.Number]; ok {
		return ErrDuplicatePolicy
	}
	b.policies[p.Number] = p
	return nil
}

// Policy returns a policy by number.
func (b *Book) Policy(number string) (Policy, error) {
	p, ok := b.policies[number]
	if !ok {
		return Policy{}, ErrUnknownPolicy
	}
	return p, nil
}

// Terms returns a policy's limit and excess.
func (b *Book) Terms(policy string) (limit, excess int, err error) {
	p, err := b.Policy(policy)
	if err != nil {
		return 0, 0, err
	}
	return p.LimitCents, p.ExcessCents, nil
}

// PolicyCount returns how many policies are in the book.
func (b *Book) PolicyCount() int { return len(b.policies) }

// Numbers returns every policy number, sorted.
func (b *Book) Numbers() []string {
	out := make([]string, 0, len(b.policies))
	for number := range b.policies {
		out = append(out, number)
	}
	sort.Strings(out)
	return out
}

// Holders returns each holder's name once, sorted.
func (b *Book) Holders() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range b.policies {
		if !seen[p.Holder] {
			seen[p.Holder] = true
			out = append(out, p.Holder)
		}
	}
	sort.Strings(out)
	return out
}
`,
			"claim.go": `package claims

import (
	"errors"
	"fmt"
)

var (
	// ErrUnknownClaim is returned by every call naming a claim that does not exist.
	ErrUnknownClaim = errors.New("unknown claim")
	// ErrBadMove is returned for a change of status that is not allowed.
	ErrBadMove = errors.New("bad move")
	// ErrNotCovered is returned for a loss outside the policy's cover.
	ErrNotCovered = errors.New("loss not covered")
	// ErrLateNotice is returned for a claim notified too long after the loss.
	ErrLateNotice = errors.New("notified too late")
)

// Claim is one request for payment. Event names the incident behind it, and all
// claims from one incident on a policy share it. AgeYears is the age of the damaged
// goods in full years. Status starts as "notified".
type Claim struct {
	ID            string
	Policy        string
	Event         string
	LossDay       int
	NotifiedDay   int
	ClaimedCents  int
	AgeYears      int
	Status        string
	AssessedCents int
	PaidCents     int
	PaidDay       int
	Recovered     int
	Assessor      string
}

// noticeDays returns how long after the loss a claim may be notified.
func noticeDays(cover string) int {
	if cover == "buildings" {
		return 60
	}
	return 30
}

// Notify records a new claim and returns its ID.
func (b *Book) Notify(policy, event string, lossDay, notifiedDay, claimedCents, ageYears int) (string, error) {
	p, err := b.Policy(policy)
	if err != nil {
		return "", err
	}
	if lossDay < p.Start || lossDay > p.End {
		return "", ErrNotCovered
	}
	if notifiedDay-lossDay > noticeDays(p.Cover) {
		return "", ErrLateNotice
	}
	c := &Claim{
		ID: fmt.Sprintf("C-%04d", b.next), Policy: policy, Event: event, LossDay: lossDay, NotifiedDay: notifiedDay,
		ClaimedCents: claimedCents, AgeYears: ageYears, Status: "notified",
	}
	b.next++
	b.claims = append(b.claims, c)
	return c.ID, nil
}

// find returns the claim with an ID, or nil.
func (b *Book) find(id string) *Claim {
	for _, c := range b.claims {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// claim returns the claim with an ID, or the error for a missing one.
func (b *Book) claim(id string) (*Claim, error) {
	c := b.find(id)
	if c == nil {
		return nil, ErrUnknownClaim
	}
	return c, nil
}

// Get returns a copy of a claim.
func (b *Book) Get(id string) (Claim, bool) {
	c := b.find(id)
	if c == nil {
		return Claim{}, false
	}
	return *c, true
}

// Claims returns copies of a policy's claims, in ID order.
func (b *Book) Claims(policy string) ([]Claim, error) {
	if _, err := b.Policy(policy); err != nil {
		return nil, err
	}
	out := []Claim{}
	for _, c := range b.claims {
		if c.Policy == policy {
			out = append(out, *c)
		}
	}
	return out, nil
}

// OpenClaims returns how many claims are neither declined nor paid.
func (b *Book) OpenClaims() int {
	n := 0
	for _, c := range b.claims {
		if c.Status != "declined" && c.Status != "paid" {
			n++
		}
	}
	return n
}

// Assess records the assessed amount of a notified claim.
func (b *Book) Assess(id string, cents int) error {
	c, err := b.claim(id)
	if err != nil {
		return err
	}
	if c.Status != "notified" {
		return ErrBadMove
	}
	c.AssessedCents = cents
	c.Status = "assessed"
	return nil
}

// Decline declines a claim that is still notified or assessed.
func (b *Book) Decline(id string) error {
	c, err := b.claim(id)
	if err != nil {
		return err
	}
	if c.Status != "notified" && c.Status != "assessed" {
		return ErrBadMove
	}
	c.Status = "declined"
	return nil
}
`,
			"settle.go": `package claims

// depreciation holds, by cover, the percentage off per full year and the most that
// can come off. A cover not listed here has none.
var depreciation = map[string]struct{ perYear, most int }{
	"contents": {10, 50},
	"motor":    {15, 60},
}

// Depreciated returns what goods of an age are worth for a claim. The deduction
// rounds down to the whole cent.
func Depreciated(cover string, ageYears, cents int) int {
	d := depreciation[cover]
	pct := min(ageYears*d.perYear, d.most)
	return cents - cents*pct/100
}

// worth returns a claim's assessed amount after depreciation.
func (b *Book) worth(c *Claim) int {
	return Depreciated(b.policies[c.Policy].Cover, c.AgeYears, c.AssessedCents)
}

// applyTerms returns what is paid on an amount: the limit applies first and what
// is left of the excess comes off what that leaves.
func applyTerms(worth, excess, limit int) int {
	return max(min(worth, limit)-excess, 0)
}

// excessTaken returns how much of the excess left a claim of that worth takes.
func excessTaken(worth, left, limit int) int {
	return min(min(worth, limit), left)
}

// excessLeft returns what is left of a policy's excess for an incident once the
// claims before stop, in ID order, have taken theirs. A nil stop means all of them.
func (b *Book) excessLeft(policy, event string, stop *Claim) int {
	p := b.policies[policy]
	left := p.ExcessCents
	for _, o := range b.claims {
		if o == stop {
			break
		}
		if o.Policy == policy && o.Event == event && o.Status != "declined" {
			left -= excessTaken(b.worth(o), left, p.LimitCents)
		}
	}
	return left
}

// payout returns what a claim would pay.
func (b *Book) payout(c *Claim) int {
	limit := b.policies[c.Policy].LimitCents
	return applyTerms(b.worth(c), b.excessLeft(c.Policy, c.Event, c), limit)
}

// Payout returns what a claim would pay given its assessed amount, the age of the
// goods and the policy's terms.
func (b *Book) Payout(id string) (int, error) {
	c, err := b.claim(id)
	if err != nil {
		return 0, err
	}
	return b.payout(c), nil
}

// ExcessLeft returns how much of a policy's excess is still to be taken for an incident.
func (b *Book) ExcessLeft(policy, event string) (int, error) {
	if _, err := b.Policy(policy); err != nil {
		return 0, err
	}
	return b.excessLeft(policy, event, nil), nil
}

// Estimate returns what a first claim of a new incident would pay for an amount,
// with no age on the goods.
func (b *Book) Estimate(policy string, cents int) (int, error) {
	p, err := b.Policy(policy)
	if err != nil {
		return 0, err
	}
	return applyTerms(cents, p.ExcessCents, p.LimitCents), nil
}

// MaxPayout returns the most any single claim under a policy can pay out.
func (b *Book) MaxPayout(policy string) (int, error) {
	p, err := b.Policy(policy)
	if err != nil {
		return 0, err
	}
	return p.LimitCents - p.ExcessCents, nil
}

// IncidentTotal returns what the claims of an incident on a policy pay together.
func (b *Book) IncidentTotal(policy, event string) (int, error) {
	if _, err := b.Policy(policy); err != nil {
		return 0, err
	}
	total := 0
	for _, c := range b.claims {
		if c.Policy == policy && c.Event == event && c.Status != "declined" {
			total += b.payout(c)
		}
	}
	return total, nil
}
`,
			"assessors.go": `package claims

import (
	"errors"
	"sort"
)

var (
	// ErrNoAssessor is returned for a name that is not on the panel of assessors.
	ErrNoAssessor = errors.New("no such assessor")
	// ErrAssessorTaken is returned when an incident's claims are with someone else.
	ErrAssessorTaken = errors.New("incident is with another assessor")
)

// AddAssessor puts an assessor on the panel.
func (b *Book) AddAssessor(name string) { b.panel[name] = true }

// Assessors returns the names on the panel, sorted.
func (b *Book) Assessors() []string {
	out := make([]string, 0, len(b.panel))
	for name := range b.panel {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// AssignAssessor gives a claim to an assessor. All claims of one incident on a
// policy stay with one assessor.
func (b *Book) AssignAssessor(id, name string) error {
	c, err := b.claim(id)
	if err != nil {
		return err
	}
	if !b.panel[name] {
		return ErrNoAssessor
	}
	for _, o := range b.claims {
		if o.Policy == c.Policy && o.Event == c.Event && o.Assessor != "" && o.Assessor != name {
			return ErrAssessorTaken
		}
	}
	c.Assessor = name
	return nil
}

// Workload returns how many claims neither declined nor paid each assessor has.
func (b *Book) Workload() map[string]int {
	out := map[string]int{}
	for _, c := range b.claims {
		if c.Assessor != "" && c.Status != "declined" && c.Status != "paid" {
			out[c.Assessor]++
		}
	}
	return out
}
`,
			"workflow.go": `package claims

import (
	"errors"
	"math"
	"sort"
)

// ErrAuthority is returned when a role may not approve a claim.
var ErrAuthority = errors.New("not within authority")

// authority holds the largest payout each role may approve.
var authority = map[string]int{"handler": 500000, "senior": 5000000, "manager": math.MaxInt}

const (
	flagDays = 14
	flagPct  = 80
)

// Approve approves an assessed claim if the role may.
func (b *Book) Approve(id, role string) error {
	c, err := b.claim(id)
	if err != nil {
		return err
	}
	if c.Status != "assessed" {
		return ErrBadMove
	}
	if b.payout(c) > authority[role] {
		return ErrAuthority
	}
	c.Status = "approved"
	return nil
}

// Pay pays an approved claim on a day.
func (b *Book) Pay(id string, day int) error {
	c, err := b.claim(id)
	if err != nil {
		return err
	}
	if c.Status != "approved" {
		return ErrBadMove
	}
	c.PaidCents = b.payout(c)
	c.PaidDay = day
	c.Status = "paid"
	return nil
}

// Recover splits money recovered from a third party on a paid claim: it repays us
// up to what we paid and have not had back, and the rest goes to the holder.
func (b *Book) Recover(id string, cents int) (insurer, holder int, err error) {
	c, err := b.claim(id)
	if err != nil {
		return 0, 0, err
	}
	if c.Status != "paid" {
		return 0, 0, ErrBadMove
	}
	insurer = min(cents, c.PaidCents-c.Recovered)
	c.Recovered += insurer
	return insurer, cents - insurer, nil
}

// flagged reports whether a claim goes to the fraud team.
func (b *Book) flagged(c *Claim) bool {
	p := b.policies[c.Policy]
	return c.LossDay-p.Start <= flagDays || c.ClaimedCents*100 >= p.LimitCents*flagPct
}

// Flagged reports whether a claim goes to the fraud team.
func (b *Book) Flagged(id string) (bool, error) {
	c, err := b.claim(id)
	if err != nil {
		return false, err
	}
	return b.flagged(c), nil
}

// ReviewQueue returns the flagged claims neither declined nor paid, the largest
// claimed amount first, ties by ID.
func (b *Book) ReviewQueue() []string {
	var list []*Claim
	for _, c := range b.claims {
		if c.Status != "declined" && c.Status != "paid" && b.flagged(c) {
			list = append(list, c)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].ClaimedCents > list[j].ClaimedCents })
	out := []string{}
	for _, c := range list {
		out = append(out, c.ID)
	}
	return out
}
`,
			"report.go": `package claims

import (
	"fmt"
	"sort"
	"strings"
)

const retention = 2500000

// Overview returns the short text at the top of the desk's morning sheet.
func (b *Book) Overview() string {
	var w strings.Builder
	fmt.Fprintf(&w, "policies: %d\n", len(b.policies))
	fmt.Fprintf(&w, "claims: %d\n", len(b.claims))
	return w.String()
}

// ByCover returns how many policies there are of each kind of cover.
func (b *Book) ByCover() map[string]int {
	out := map[string]int{}
	for _, p := range b.policies {
		out[p.Cover]++
	}
	return out
}

// ClaimsByStatus returns how many claims are in each status.
func (b *Book) ClaimsByStatus() map[string]int {
	out := map[string]int{}
	for _, c := range b.claims {
		out[c.Status]++
	}
	return out
}

// reserve returns what we hold for a claim.
func (b *Book) reserve(c *Claim) int {
	switch c.Status {
	case "notified":
		return c.ClaimedCents
	case "declined", "paid":
		return 0
	}
	return b.payout(c)
}

// Reserve returns what we hold for a claim.
func (b *Book) Reserve(id string) (int, error) {
	c, err := b.claim(id)
	if err != nil {
		return 0, err
	}
	return b.reserve(c), nil
}

// TotalReserve returns what we hold across all claims.
func (b *Book) TotalReserve() int {
	total := 0
	for _, c := range b.claims {
		total += b.reserve(c)
	}
	return total
}

// Shortfall returns the claimed amount less what the claim would pay.
func (b *Book) Shortfall(id string) (int, error) {
	c, err := b.claim(id)
	if err != nil {
		return 0, err
	}
	return c.ClaimedCents - b.payout(c), nil
}

// PaidTotal returns what has been paid across all claims.
func (b *Book) PaidTotal() int {
	total := 0
	for _, c := range b.claims {
		total += c.PaidCents
	}
	return total
}

// AgedOpen returns claims neither declined nor paid that were notified more than
// 45 days before today, those notified first at the front.
func (b *Book) AgedOpen(today int) []string {
	var list []*Claim
	for _, c := range b.claims {
		if c.Status != "declined" && c.Status != "paid" && today-c.NotifiedDay > 45 {
			list = append(list, c)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].NotifiedDay < list[j].NotifiedDay })
	out := []string{}
	for _, c := range list {
		out = append(out, c.ID)
	}
	return out
}

// Bordereau returns the reinsurer's file for claims paid from one day to another.
func (b *Book) Bordereau(from, to int) string {
	var rows []*Claim
	for _, c := range b.claims {
		if c.Status == "paid" && c.PaidDay >= from && c.PaidDay <= to && c.PaidCents > retention {
			rows = append(rows, c)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].PaidCents > rows[j].PaidCents })
	var w strings.Builder
	fmt.Fprintf(&w, "BDX,%d,%d\n", from, to)
	ceded := 0
	for _, c := range rows {
		over := c.PaidCents - retention
		ceded += over
		fmt.Fprintf(&w, "%s,%s,%q,%s,%s\n", c.ID, c.Policy, b.policies[c.Policy].Holder, Euros(c.PaidCents), Euros(over))
	}
	fmt.Fprintf(&w, "TOTAL,%d,%s\n", len(rows), Euros(ceded))
	return w.String()
}
`,
		},
		Turns: script(
			cloud("The claims manager's rule for payouts, which holds from here on: a policy's excess is taken once per incident, however many claims that incident brings, and what one claim doesn't use carries over to the next claim of the same incident, in ID order. It comes off before the limit is applied. Nothing pays yet. For now add `func (b *Book) PolicyCount() int` to book.go."),
			cloud("Add `var ErrUnknownClaim = errors.New(\"unknown claim\")` to claim.go. Every call that names a claim that doesn't exist returns it, including all the calls we add later. First one: `func (b *Book) Assess(id string, cents int) error`, which sets AssessedCents and moves Status to \"assessed\"."),
			cloud("The reinsurer sent docs/bordereau-format.txt. Read it and tell me whether Claim holds what that file needs. No code changes."),
			cloud("Finance on reserves: a claim holds its claimed amount until it is assessed, then what it would pay out, and nothing once it is declined or paid. Nothing computes reserves yet. For now add `func (b *Book) OpenClaims() int` to claim.go: how many claims are neither declined nor paid."),
			local("Read docs/underwriting-guide.txt and tell me in a few lines what it says about which claims we take and what they are worth. No code changes."),
			local("Add `func (b *Book) Claims(policy string) ([]Claim, error)` to claim.go: copies of that policy's claims in ID order, with `ErrUnknownPolicy` when there is no such policy."),
			local("Finance says a depreciation deduction rounds down to the whole cent, in the customer's favour. Nothing depreciates yet. For now add `func (b *Book) Holders() []string` to book.go: each holder's name once, sorted."),
			local("Status rules from the manager: a claim moves notified, assessed, approved, paid, in that order and no other, and only one still notified or assessed can be declined. Add `var ErrBadMove = errors.New(\"bad move\")` to claim.go for anything else, make `Assess` follow that, and add `func (b *Book) Decline(id string) error`, which moves Status to \"declined\"."),
			local("The manager's approval limits, for when we get to approvals: a handler up to 5,000.00, a senior up to 50,000.00, a manager anything. They go by what the claim would pay out, not what was claimed. For now add `func (b *Book) Terms(policy string) (limit, excess int, err error)` to book.go: that policy's limit and excess, with `ErrUnknownPolicy` when there is no such policy."),
			cloud("Add `func Depreciated(cover string, ageYears, cents int) int` in a new file settle.go: what goods of that cover and age are worth for a claim."),
			cloud("Add `func (b *Book) Payout(id string) (int, error)` to settle.go: what the claim would pay given its assessed amount, the age of the goods and the policy's terms, never below 0. A declined claim plays no part in what other claims of its incident pay."),
			local("Add `func (b *Book) ExcessLeft(policy, event string) (int, error)` to settle.go: how much of the policy's excess is still to be taken for that incident, going by the assessed claims so far. No such policy as before."),
			local("Add `func (b *Book) Estimate(policy string, cents int) (int, error)` to settle.go: what a first claim of a new incident would pay for that many cents, with no age on the goods. No such policy as before."),
			cloud("Make `Notify` in claim.go refuse what we can't take: add `var ErrNotCovered = errors.New(\"loss not covered\")` and `var ErrLateNotice = errors.New(\"notified too late\")` for that."),
			cloud("Add `func (b *Book) Approve(id, role string) error` in a new file workflow.go, where role is \"handler\", \"senior\" or \"manager\". It approves the claim, moving Status to \"approved\", if that role may. Otherwise it returns a new `ErrAuthority`, which also goes for any other role."),
			cloud("Add `func (b *Book) Pay(id string, day int) error` to workflow.go: pays the claim, setting PaidCents to what it pays, PaidDay to that day and Status to \"paid\"."),
			local("Add `func (b *Book) Reserve(id string) (int, error)` to report.go: what we hold for that claim."),
			local("Add `func (b *Book) TotalReserve() int` to report.go: what we hold across all claims."),
			cloud("Legal changed the order of the policy terms: the limit now applies first, and the excess comes off what that leaves. Update `Payout`, `ExcessLeft` and `Estimate` in settle.go."),
			local("The fraud team wants a claim flagged if its loss falls 14 days or fewer after the policy's Start, or if it claims 80 percent or more of the limit. Nothing flags yet. For now add `func (b *Book) PaidTotal() int` to report.go: what has been paid across all claims."),
			local("Add `func (b *Book) Bordereau(from, to int) string` to report.go: the reinsurer's file for claims paid from day from to day to."),
			local("Add `func (b *Book) Shortfall(id string) (int, error)` to report.go: the claimed amount less what the claim would pay."),
			cloud("Add `func (b *Book) Flagged(id string) (bool, error)` to workflow.go: whether the claim is flagged. Also `func (b *Book) ReviewQueue() []string`: the IDs of flagged claims that are neither declined nor paid, the largest claimed amount first, ties by ID."),
			cloud("`Payout`, `ExcessLeft`, `Reserve` and `Shortfall` each find their way to the same numbers separately. Have settle.go hold one unexported function each for a claim's worth, what is left of the excess and what a claim pays, and put settle.go and report.go on top of them. No change in what they return."),
			cloud("Add `func (b *Book) Recover(id string, cents int) (insurer, holder int, err error)` to workflow.go: money recovered from a third party on a paid claim. It repays us up to what we paid and haven't had back, and the rest goes to the holder. Record what we've had back in Recovered. A claim that isn't paid returns `ErrBadMove`."),
			cloud("Add `func (b *Book) AddAssessor(name string)` and `func (b *Book) Assessors() []string` in a new file assessors.go: the panel of assessors, with names sorted. The manager's rule for when we assign them: all claims of one incident on a policy stay with one assessor, and giving one to anybody else fails with `var ErrAssessorTaken = errors.New(\"incident is with another assessor\")`, which you can add now."),
			local("Add `func (b *Book) AgedOpen(today int) []string` to report.go: the IDs of claims neither declined nor paid that were notified more than 45 days before today, earliest notified first, ties by ID."),
			local("Add `func (b *Book) MaxPayout(policy string) (int, error)` to settle.go: the most any single claim under that policy can pay out. No such policy as before."),
			cloud("Add `func (b *Book) IncidentTotal(policy, event string) (int, error)` to settle.go: what the claims of that incident on that policy pay together, leaving out declined ones. No such policy as before."),
			local("Add `func (b *Book) AssignAssessor(id, name string) error` to assessors.go: gives the claim to that assessor, recorded in Assessor. A name not on the panel returns a new `ErrNoAssessor`."),
			local("Add `func (b *Book) ClaimsByStatus() map[string]int` to report.go: how many claims are in each status, with no entry for a status nobody is in."),
			local("Add `func (b *Book) Workload() map[string]int` to assessors.go: how many claims neither declined nor paid each assessor has."),
		),
		Carries: []Carry{
			{
				What:        "the excess is taken once per incident, and what one claim doesn't use carries over to the next",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{10, 11, 28},
				Tests:       []string{"TestExcessOncePerIncident", "TestExcessCarriesToNextClaim", "TestExcessLeftAfterClaim", "TestIncidentTotalOneExcess"},
				Absent:      []string{"once per incident", "carries over", "before the limit"},
			},
			{
				What:        "ErrUnknownClaim from every call that names a claim that does not exist",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{7, 10, 14, 15, 16, 21, 22, 24, 29},
				Tests: []string{
					"TestAssessUnknown", "TestDeclineUnknown", "TestPayoutUnknown", "TestApproveUnknown", "TestPayUnknown",
					"TestReserveUnknown", "TestShortfallUnknown", "TestFlaggedUnknown", "TestRecoverUnknown", "TestAssignAssessorUnknown",
				},
				Absent: []string{"errunknownclaim", "unknown claim", "doesn't exist"},
			},
			{
				What:        "the bordereau's header, retention, quoted holder, order and total line (docs/bordereau-format.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{20},
				Source:      "docs/bordereau-format.txt",
				Tests:       []string{"TestBordereauHeader", "TestBordereauRows", "TestBordereauLeavesOutSmallClaims", "TestBordereauRangeBothEnds", "TestBordereauEndsWithTotal"},
				Absent:      []string{"bordereau-format", "bdx", "25,000", "retention"},
			},
			{
				What:        "a reserve is the claimed amount until assessed, then the payout, then nothing once declined or paid",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{16, 17},
				Tests:       []string{"TestReserveBeforeAssessment", "TestReserveAfterAssessment", "TestReserveGoneWhenDeclined", "TestReserveGoneWhenPaid", "TestTotalReserveAddsUp"},
				Absent:      []string{"until it is assessed", "once it is declined", "on reserves"},
			},
			{
				What:        "the cover period, notice periods and depreciation by cover (docs/underwriting-guide.txt)",
				Kind:        CarryDiscovered,
				Established: 4,
				Needed:      []int{9, 10, 13},
				Source:      "docs/underwriting-guide.txt",
				Tests: []string{
					"TestNotifyOutsideCover", "TestNotifyInsideCover", "TestNotifyNoticeContents", "TestNotifyNoticeBuildings", "TestDepreciationContents",
					"TestDepreciationMotor", "TestDepreciationCapped", "TestDepreciationNoneOnBuildings", "TestPayoutDepreciatesFirst", "TestDepreciationRounding", "TestPayoutKeepsTheRounding",
				},
				Absent: []string{"30 days", "60 days", "10 percent", "15 percent", "50 percent", "60 percent"},
			},
			{
				What:        "a depreciation deduction rounds down to the whole cent",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{9, 10},
				Tests:       []string{"TestDepreciationRounding", "TestPayoutKeepsTheRounding"},
				Absent:      []string{"rounds down", "whole cent", "customer's favour"},
			},
			{
				What:        "a claim moves notified, assessed, approved, paid, in that order, and ErrBadMove for anything else",
				Kind:        CarryEmbodied,
				Established: 7,
				Needed:      []int{14, 15},
				Tests:       []string{"TestApproveNeedsAssessment", "TestPayNeedsApproval"},
				Absent:      []string{"in that order and no other", "still notified"},
			},
			{
				What:        "approval limits by role, which go by the payout and not the claimed amount",
				Kind:        CarryUser,
				Established: 8,
				Needed:      []int{14},
				Tests:       []string{"TestHandlerLimit", "TestSeniorLimit", "TestApprovalGoesByPayout"},
				Absent:      []string{" 5,000.00", "50,000.00", "not what was claimed"},
			},
			{
				What:        "the order of the terms changed: the limit now applies first and the excess comes off what that leaves",
				Kind:        CarryUser,
				Established: 18,
				Needed:      []int{23, 27},
				Tests:       []string{"TestPayoutLimitBeforeExcess", "TestEstimateAboveLimit", "TestMaxPayoutTakesOffExcess"},
				Absent:      []string{"limit now applies first", "what that leaves", "limit first"},
			},
			{
				What:        "a claim is flagged 14 days or fewer after the policy's start, or at 80 percent of the limit",
				Kind:        CarryUser,
				Established: 19,
				Needed:      []int{22},
				Tests:       []string{"TestFlaggedEarlyLoss", "TestFlaggedLargeClaim", "TestReviewQueueOrder"},
				Absent:      []string{"14 days", "80 percent", "fraud team"},
			},
			{
				What:        "all claims of one incident on a policy stay with one assessor",
				Kind:        CarryUser,
				Established: 25,
				Needed:      []int{29},
				Tests:       []string{"TestIncidentStaysWithOneAssessor"},
				Absent:      []string{"errassessortaken", "one assessor", "anybody else"},
			},
		},
	}
}
