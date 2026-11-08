package s2

func accA03() Task {
	return Task{
		ID:     "s2-a03-loyalty-points",
		Set:    "acceptance",
		System: system("a customer loyalty points account"),
		Files: map[string]string{
			"go.mod": goMod,
			"account.go": `package loyalty

// Batch is a lot of points earned together. Expires is a day number.
type Batch struct {
	Points  int
	Expires int
}

// Account holds a member's point batches.
type Account struct {
	batches []Batch
}

// Earn adds a batch of points.
func (a *Account) Earn(points, expires int) {
	a.batches = append(a.batches, Batch{Points: points, Expires: expires})
}
`,
			"docs/tiers.txt": `Membership tiers

A gold member earns double the base points on a purchase. A silver member earns
one and a half times the base points, with any half point dropped. Everyone else,
including a blank tier, earns the base points.

Tier reviews happen every January.
`,
			"account_test.go": `package loyalty

import "testing"

func TestEarnAddsBatch(t *testing.T) {
	var a Account
	a.Earn(10, 100)
	if len(a.batches) != 1 || a.batches[0].Points != 10 || a.batches[0].Expires != 100 {
		t.Fatalf("batches = %+v", a.batches)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package loyalty_test

import (
	"errors"
	"testing"

	loyalty "fixture"
)

func TestBatchesCount(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(1, 10)
	a.Earn(2, 20)
	if a.Batches() != 2 {
		t.Fatalf("Batches = %d", a.Batches())
	}
}

func TestPointsForBaseRate(t *testing.T) {
	if got := loyalty.PointsFor(1000, ""); got != 50 {
		t.Fatalf("PointsFor(1000) = %d, want 50", got)
	}
}

func TestPointsForDropsCents(t *testing.T) {
	if got := loyalty.PointsFor(199, ""); got != 5 {
		t.Fatalf("PointsFor(199) = %d, want 5", got)
	}
}

func TestPointsForNothing(t *testing.T) {
	if got := loyalty.PointsFor(0, "gold"); got != 0 {
		t.Fatalf("PointsFor(0) = %d", got)
	}
}

func TestPointsForGold(t *testing.T) {
	if base, got := loyalty.PointsFor(3000, ""), loyalty.PointsFor(3000, "gold"); got != 2*base {
		t.Fatalf("base %d, gold %d", base, got)
	}
}

func TestPointsForSilverDropsHalf(t *testing.T) {
	for _, cents := range []int{100, 300} {
		base, got := loyalty.PointsFor(cents, ""), loyalty.PointsFor(cents, "silver")
		if got != base*3/2 {
			t.Errorf("%d cents: base %d, silver %d", cents, base, got)
		}
	}
}

func TestBalanceOnExpiryDay(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(10, 100)
	if got := a.Balance(100); got != 10 {
		t.Fatalf("Balance = %d", got)
	}
}

func TestBalanceSumsLiveBatches(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(10, 100)
	a.Earn(20, 200)
	if got := a.Balance(50); got != 30 {
		t.Errorf("day 50: %d", got)
	}
	if got := a.Balance(150); got != 20 {
		t.Errorf("day 150: %d", got)
	}
	if got := a.Balance(201); got != 0 {
		t.Errorf("day 201: %d", got)
	}
}

func TestExpireKeepsExpiryDay(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(10, 100)
	if got := a.Expire(100); got != 0 {
		t.Fatalf("Expire on the day itself lost %d", got)
	}
	if got := a.Balance(100); got != 10 {
		t.Fatalf("Balance = %d", got)
	}
}

func TestExpireDropsLapsed(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(10, 100)
	a.Earn(20, 200)
	if got := a.Expire(150); got != 10 {
		t.Fatalf("Expire = %d", got)
	}
	if a.Batches() != 1 || a.Balance(150) != 20 {
		t.Fatalf("Batches %d, Balance %d", a.Batches(), a.Balance(150))
	}
}

func TestSpendOnExpiryDay(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(10, 100)
	if err := a.Spend(10, 100); err != nil {
		t.Fatal(err)
	}
	if got := a.Balance(100); got != 0 {
		t.Fatalf("Balance = %d", got)
	}
}

func TestSpendSoonestExpiryFirst(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(100, 500)
	a.Earn(50, 100)
	if err := a.Spend(60, 10); err != nil {
		t.Fatal(err)
	}
	if got := a.Balance(200); got != 90 {
		t.Fatalf("after the day 100 batch lapsed the balance is %d, want 90", got)
	}
}

func TestSpendReduces(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(100, 500)
	if err := a.Spend(30, 10); err != nil {
		t.Fatal(err)
	}
	if got := a.Balance(10); got != 70 {
		t.Fatalf("Balance = %d", got)
	}
}

func TestSpendIgnoresLapsed(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(100, 10)
	a.Earn(20, 100)
	if err := a.Spend(50, 50); err == nil {
		t.Fatal("spent lapsed points")
	}
	if got := a.Balance(50); got != 20 {
		t.Fatalf("a refused spend left %d", got)
	}
	if err := a.Spend(20, 50); err != nil {
		t.Fatal(err)
	}
}

func TestSpendTooMuchSentinel(t *testing.T) {
	a := &loyalty.Account{}
	a.Earn(10, 100)
	if err := a.Spend(11, 50); !errors.Is(err, loyalty.ErrInsufficient) {
		t.Fatalf("err = %v", err)
	}
}

func TestTransferTooMuchSentinel(t *testing.T) {
	a, b := &loyalty.Account{}, &loyalty.Account{}
	a.Earn(10, 100)
	if err := a.Transfer(b, 11, 50); !errors.Is(err, loyalty.ErrInsufficient) {
		t.Fatalf("err = %v", err)
	}
}

func TestTransferMoves(t *testing.T) {
	a, b := &loyalty.Account{}, &loyalty.Account{}
	a.Earn(100, 500)
	if err := a.Transfer(b, 30, 10); err != nil {
		t.Fatal(err)
	}
	if a.Balance(10) != 70 || b.Balance(10) != 30 {
		t.Fatalf("balances %d and %d", a.Balance(10), b.Balance(10))
	}
	if b.Balance(99) != 30 || b.Balance(101) != 0 {
		t.Fatalf("the new batch does not expire on day 100: %d, %d", b.Balance(99), b.Balance(101))
	}
}
`},
		Solution: map[string]string{
			"account.go": `package loyalty

import (
	"errors"
	"sort"
)

// ErrInsufficient is for using more points than are good on that day.
var ErrInsufficient = errors.New("not enough points")

// Batch is a lot of points earned together. Expires is a day number.
type Batch struct {
	Points  int
	Expires int
}

// Account holds a member's point batches.
type Account struct {
	batches []Batch
}

// Earn adds a batch of points.
func (a *Account) Earn(points, expires int) {
	a.batches = append(a.batches, Batch{Points: points, Expires: expires})
}

// Batches returns how many batches the account holds.
func (a *Account) Batches() int { return len(a.batches) }

func (b Batch) good(today int) bool { return today <= b.Expires }

// Balance returns the points the member can use on that day.
func (a *Account) Balance(today int) int {
	total := 0
	for _, b := range a.batches {
		if b.good(today) {
			total += b.Points
		}
	}
	return total
}

// Expire drops lapsed batches and returns how many points went with them.
func (a *Account) Expire(today int) int {
	lost := 0
	kept := a.batches[:0]
	for _, b := range a.batches {
		if b.good(today) {
			kept = append(kept, b)
		} else {
			lost += b.Points
		}
	}
	a.batches = kept
	return lost
}

// Spend takes points off the account, soonest expiry first, or nothing at all.
func (a *Account) Spend(points, today int) error {
	if points > a.Balance(today) {
		return ErrInsufficient
	}
	sort.SliceStable(a.batches, func(i, j int) bool { return a.batches[i].Expires < a.batches[j].Expires })
	for i := range a.batches {
		if a.batches[i].good(today) {
			take := min(points, a.batches[i].Points)
			a.batches[i].Points -= take
			points -= take
		}
	}
	return nil
}

// Transfer moves points to another account as one new batch.
func (a *Account) Transfer(to *Account, points, today int) error {
	if err := a.Spend(points, today); err != nil {
		return err
	}
	to.Earn(points, today+90)
	return nil
}
`,
			"earn.go": `package loyalty

const pointsPerEuro = 5

// PointsFor returns the points a purchase earns a member of that tier.
func PointsFor(cents int, tier string) int {
	base := cents / 100 * pointsPerEuro
	switch tier {
	case "gold":
		return base * 2
	case "silver":
		return base * 3 / 2
	}
	return base
}
`,
		},
		Turns: script(
			local("Marketing settled the earn rate: 5 points for each whole euro of a purchase, and leftover cents earn nothing. We'll build on that soon. For now add `func (a *Account) Batches() int`, how many batches the account holds."),
			local("Read docs/tiers.txt and tell me how a member's tier changes what they earn. No code changes."),
			cloud("Add `func PointsFor(cents int, tier string) int` in a new file earn.go: the points a purchase of that many cents earns a member of that tier."),
			cloud("Legal wants this honoured: points in a batch are good through its Expires day itself and gone the day after. Nothing reads expiry yet. Add `var ErrInsufficient = errors.New(\"not enough points\")` too. Nothing returns it yet, but it's what anyone gets when they try to use more points than they can use on that day."),
			local("Add `func (a *Account) Balance(today int) int`: the points the member can use on that day."),
			cloud("When a member spends points we'll draw on the batch that expires soonest, so they lose as little as possible when batches lapse. There's no spending code yet. Add `func (a *Account) Expire(today int) int`: drops the batches that have lapsed and returns how many points went with them."),
			local("Add `func (a *Account) Spend(points, today int) error`: takes that many points off the account, or nothing at all if it can't."),
			cloud("Add `func (a *Account) Transfer(to *Account, points, today int) error`: moves points from this account to another one, where they arrive as one new batch expiring 90 days after today."),
		),
		Carries: []Carry{
			{
				What:        "5 points per whole euro, cents earn nothing",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2},
				Tests:       []string{"TestPointsForBaseRate", "TestPointsForDropsCents"},
				Absent:      []string{"5 points", "five points", "whole euro", "leftover cents"},
			},
			{
				What:        "gold earns double, silver one and a half with the half dropped (docs/tiers.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{2},
				Source:      "docs/tiers.txt",
				Tests:       []string{"TestPointsForGold", "TestPointsForSilverDropsHalf"},
				Absent:      []string{"gold", "silver", "double"},
			},
			{
				What:        "points are good through the Expires day and gone the day after",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4, 5, 6},
				Tests:       []string{"TestBalanceOnExpiryDay", "TestExpireKeepsExpiryDay", "TestSpendOnExpiryDay"},
				Absent:      []string{"good through", "the day after", "expires day"},
			},
			{
				What:        "ErrInsufficient for using more points than are good that day",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{6, 7},
				Tests:       []string{"TestSpendTooMuchSentinel", "TestTransferTooMuchSentinel"},
				Absent:      []string{"errinsufficient", "not enough points", "more points than"},
			},
			{
				What:        "a spend draws on the batch that expires soonest",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{6},
				Tests:       []string{"TestSpendSoonestExpiryFirst"},
				Absent:      []string{"soonest", "earliest", "lose as little"},
			},
		},
	}
}
