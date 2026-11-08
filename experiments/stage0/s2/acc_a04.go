package s2

func accA04() Task {
	return Task{
		ID:     "s2-a04-coupon-redemption",
		Set:    "acceptance",
		System: system("a coupon redemption service"),
		Files: map[string]string{
			"go.mod": goMod,
			"coupon.go": `package coupons

// Coupon is one discount code. Code is the printed form.
type Coupon struct {
	Code       string
	PercentOff int
	MaxUses    int
	Uses       int
}

// Book holds the coupons on offer, by printed code.
type Book struct {
	coupons map[string]*Coupon
}

// Add puts a coupon on offer.
func (b *Book) Add(c Coupon) {
	if b.coupons == nil {
		b.coupons = map[string]*Coupon{}
	}
	b.coupons[c.Code] = &c
}
`,
			"docs/codes.txt": `Coupon code notes

Printed codes are capitals and digits and never use the letters O or I, because
they get misread. Customers type what they see, in any case, so a typed O means
the digit 0 and a typed I means the digit 1. Pasted codes often pick up spaces at
either end, which don't count.

Codes are retired, never reissued.
`,
			"coupon_test.go": `package coupons

import "testing"

func TestAddKeepsCopy(t *testing.T) {
	var b Book
	c := Coupon{Code: "A1", PercentOff: 5}
	b.Add(c)
	c.PercentOff = 9
	if b.coupons["A1"].PercentOff != 5 {
		t.Fatalf("stored %+v", b.coupons["A1"])
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package coupons_test

import (
	"errors"
	"testing"

	coupons "fixture"
)

func book(c coupons.Coupon) *coupons.Book {
	b := &coupons.Book{}
	b.Add(c)
	return b
}

func save10() *coupons.Book {
	return book(coupons.Coupon{Code: "SAVE10", PercentOff: 10, MaxUses: 2})
}

func uses(t *testing.T, b *coupons.Book) int {
	t.Helper()
	c, err := b.Lookup("SAVE10")
	if err != nil {
		t.Fatal(err)
	}
	return c.Uses
}

func TestCountCoupons(t *testing.T) {
	if got := save10().Count(); got != 1 {
		t.Fatalf("Count = %d", got)
	}
}

func TestLookupExact(t *testing.T) {
	c, err := save10().Lookup("SAVE10")
	if err != nil {
		t.Fatal(err)
	}
	if c.Code != "SAVE10" || c.PercentOff != 10 {
		t.Fatalf("Lookup = %+v", c)
	}
}

func TestLookupUnknownSentinel(t *testing.T) {
	if _, err := save10().Lookup("NOPE"); !errors.Is(err, coupons.ErrUnknownCode) {
		t.Fatalf("err = %v", err)
	}
}

func TestLookupAnyCaseAndSpaces(t *testing.T) {
	c, err := save10().Lookup("  save10 ")
	if err != nil {
		t.Fatal(err)
	}
	if c.Code != "SAVE10" {
		t.Fatalf("Lookup = %+v", c)
	}
}

func TestLookupMisreadLetters(t *testing.T) {
	for _, typed := range []string{"SAVE1O", "SAVEI0", "saveio"} {
		if _, err := save10().Lookup(typed); err != nil {
			t.Errorf("%q: %v", typed, err)
		}
	}
}

func TestRedeemMisreadLetters(t *testing.T) {
	b := save10()
	if err := b.Redeem("save1o"); err != nil {
		t.Fatal(err)
	}
	if got := uses(t, b); got != 1 {
		t.Fatalf("Uses = %d", got)
	}
}

func TestRedeemCountsUse(t *testing.T) {
	b := save10()
	if err := b.Redeem("SAVE10"); err != nil {
		t.Fatal(err)
	}
	if got := uses(t, b); got != 1 {
		t.Fatalf("Uses = %d", got)
	}
}

func TestRedeemUsedUp(t *testing.T) {
	b := save10()
	for range 2 {
		if err := b.Redeem("SAVE10"); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Redeem("SAVE10"); !errors.Is(err, coupons.ErrUsedUp) {
		t.Fatalf("err = %v", err)
	}
	if got := uses(t, b); got != 2 {
		t.Fatalf("Uses = %d", got)
	}
}

func TestRedeemNoCap(t *testing.T) {
	b := book(coupons.Coupon{Code: "FREE5", PercentOff: 5})
	for i := range 5 {
		if err := b.Redeem("FREE5"); err != nil {
			t.Fatalf("redemption %d: %v", i+1, err)
		}
	}
}

func TestRedeemUnknownSentinel(t *testing.T) {
	if err := save10().Redeem("NOPE"); !errors.Is(err, coupons.ErrUnknownCode) {
		t.Fatalf("err = %v", err)
	}
}

func TestRefundGivesUseBack(t *testing.T) {
	b := save10()
	if err := b.Redeem("SAVE10"); err != nil {
		t.Fatal(err)
	}
	if err := b.Refund("SAVE10"); err != nil {
		t.Fatal(err)
	}
	if got := uses(t, b); got != 0 {
		t.Fatalf("Uses = %d", got)
	}
}

func TestRefundNothing(t *testing.T) {
	if err := save10().Refund("SAVE10"); !errors.Is(err, coupons.ErrNothingToRefund) {
		t.Fatalf("err = %v", err)
	}
}

func TestRefundUnknownSentinel(t *testing.T) {
	if err := save10().Refund("NOPE"); !errors.Is(err, coupons.ErrUnknownCode) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscounted(t *testing.T) {
	if got := coupons.Discounted(1000, 10); got != 900 {
		t.Errorf("10%% off 1000 = %d", got)
	}
	if got := coupons.Discounted(2000, 25); got != 1500 {
		t.Errorf("25%% off 2000 = %d", got)
	}
}

func TestDiscountedFloor(t *testing.T) {
	if got := coupons.Discounted(60, 50); got != 50 {
		t.Errorf("50%% off 60 = %d, want 50", got)
	}
	if got := coupons.Discounted(30, 50); got != 30 {
		t.Errorf("an order of 30 pays %d, want 30", got)
	}
}

func TestQuoteCountsNoUse(t *testing.T) {
	b := save10()
	got, err := b.Quote(1000, "SAVE10")
	if err != nil {
		t.Fatal(err)
	}
	if got != 900 {
		t.Fatalf("Quote = %d", got)
	}
	if n := uses(t, b); n != 0 {
		t.Fatalf("Uses = %d", n)
	}
}

func TestQuoteUsedUp(t *testing.T) {
	b := book(coupons.Coupon{Code: "LAST1", PercentOff: 10, MaxUses: 1, Uses: 1})
	if _, err := b.Quote(1000, "LAST1"); !errors.Is(err, coupons.ErrUsedUp) {
		t.Fatalf("err = %v", err)
	}
}

func TestQuoteNoCap(t *testing.T) {
	b := book(coupons.Coupon{Code: "FREE5", PercentOff: 5, Uses: 3})
	if _, err := b.Quote(1000, "FREE5"); err != nil {
		t.Fatal(err)
	}
}

func TestQuoteFloor(t *testing.T) {
	b := book(coupons.Coupon{Code: "MAX90", PercentOff: 90, MaxUses: 9})
	got, err := b.Quote(100, "MAX90")
	if err != nil {
		t.Fatal(err)
	}
	if got != 50 {
		t.Fatalf("Quote = %d, want 50", got)
	}
}

func TestQuoteUnknownSentinel(t *testing.T) {
	if _, err := save10().Quote(1000, "NOPE"); !errors.Is(err, coupons.ErrUnknownCode) {
		t.Fatalf("err = %v", err)
	}
}
`},
		Solution: map[string]string{
			"coupon.go": `package coupons

import (
	"errors"
	"strings"
)

// ErrUnknownCode is what any call gets for a code we don't have.
var ErrUnknownCode = errors.New("unknown code")

// ErrUsedUp refuses a coupon with no uses left.
var ErrUsedUp = errors.New("coupon used up")

// ErrNothingToRefund refuses a refund when no uses have been counted.
var ErrNothingToRefund = errors.New("nothing to refund")

// Coupon is one discount code. Code is the printed form.
type Coupon struct {
	Code       string
	PercentOff int
	MaxUses    int
	Uses       int
}

// Book holds the coupons on offer, by printed code.
type Book struct {
	coupons map[string]*Coupon
}

// Add puts a coupon on offer.
func (b *Book) Add(c Coupon) {
	if b.coupons == nil {
		b.coupons = map[string]*Coupon{}
	}
	b.coupons[c.Code] = &c
}

// Count returns the number of coupons on offer.
func (b *Book) Count() int { return len(b.coupons) }

// printed turns what a customer typed into the printed form of a code.
func printed(typed string) string {
	s := strings.ToUpper(strings.TrimSpace(typed))
	return strings.NewReplacer("O", "0", "I", "1").Replace(s)
}

func (c *Coupon) usedUp() bool { return c.MaxUses > 0 && c.Uses >= c.MaxUses }

// Lookup returns a copy of the coupon for what a customer typed.
func (b *Book) Lookup(typed string) (Coupon, error) {
	c, ok := b.coupons[printed(typed)]
	if !ok {
		return Coupon{}, ErrUnknownCode
	}
	return *c, nil
}

// Redeem counts one use of a coupon.
func (b *Book) Redeem(typed string) error {
	c, ok := b.coupons[printed(typed)]
	if !ok {
		return ErrUnknownCode
	}
	if c.usedUp() {
		return ErrUsedUp
	}
	c.Uses++
	return nil
}

// Refund gives one use back.
func (b *Book) Refund(typed string) error {
	c, ok := b.coupons[printed(typed)]
	if !ok {
		return ErrUnknownCode
	}
	if c.Uses == 0 {
		return ErrNothingToRefund
	}
	c.Uses--
	return nil
}

// Quote returns what the customer would pay, without counting a use.
func (b *Book) Quote(subtotal int, typed string) (int, error) {
	c, ok := b.coupons[printed(typed)]
	if !ok {
		return 0, ErrUnknownCode
	}
	if c.usedUp() {
		return 0, ErrUsedUp
	}
	return Discounted(subtotal, c.PercentOff), nil
}
`,
			"price.go": `package coupons

const minCharge = 50

// Discounted returns what's left to pay after taking percent off subtotal.
func Discounted(subtotal int, percent int) int {
	due := subtotal - subtotal*percent/100
	return min(subtotal, max(due, minCharge))
}
`,
		},
		Turns: script(
			cloud("Product decision from today: a coupon with MaxUses of 0 has no cap at all, and we have hundreds of those running. Nothing redeems coupons yet. For now add `func (b *Book) Count() int`, the number of coupons on offer."),
			local("Read docs/codes.txt and tell me what it says about what customers type. No code changes."),
			cloud("Add `var ErrUnknownCode = errors.New(\"unknown code\")` and `func (b *Book) Lookup(typed string) (Coupon, error)`, which finds the coupon for what a customer typed and returns a copy of it. That error is what any call gets for a code we don't have."),
			local("Add `var ErrUsedUp = errors.New(\"coupon used up\")` and `func (b *Book) Redeem(typed string) error`: counts one use of the coupon, or refuses with ErrUsedUp if it has no uses left."),
			cloud("Payments told us they bounce anything under 50 cents, so a discount can never take an order below that. An order that starts below it just pays what it is. Nothing prices orders yet. Add `var ErrNothingToRefund = errors.New(\"nothing to refund\")` and `func (b *Book) Refund(typed string) error`: gives one use back when an order is cancelled, or returns ErrNothingToRefund if no uses have been counted."),
			local("Add `func Discounted(subtotal int, percent int) int` in a new file price.go: what's left to pay, in cents, after taking percent off subtotal."),
			cloud("Add `func (b *Book) Quote(subtotal int, typed string) (int, error)`: what the customer would pay with this coupon, without counting a use. A coupon with no uses left gets ErrUsedUp."),
		),
		Carries: []Carry{
			{
				What:        "MaxUses of 0 means no cap",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 6},
				Tests:       []string{"TestRedeemNoCap", "TestQuoteNoCap"},
				Absent:      []string{"no cap", "uncapped", "unlimited", "maxuses of 0"},
			},
			{
				What:        "typed codes ignore case and outer spaces, and O and I read as 0 and 1 (docs/codes.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{2, 3},
				Source:      "docs/codes.txt",
				Tests:       []string{"TestLookupAnyCaseAndSpaces", "TestLookupMisreadLetters", "TestRedeemMisreadLetters"},
				Absent:      []string{"misread", "digit", "any case", "spaces"},
			},
			{
				What:        "ErrUnknownCode for any call with a code we don't have",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{3, 4, 6},
				Tests:       []string{"TestRedeemUnknownSentinel", "TestRefundUnknownSentinel", "TestQuoteUnknownSentinel"},
				Absent:      []string{"errunknowncode", "unknown code", "don't have"},
			},
			{
				What:        "a discount never takes an order below 50 cents",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{5, 6},
				Tests:       []string{"TestDiscountedFloor", "TestQuoteFloor"},
				Absent:      []string{"50 cents", "under 50", "bounce"},
			},
		},
	}
}
