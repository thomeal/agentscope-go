package s2

func long2B02() Task {
	return Task{
		ID:     "s2-long2-b02-skirmish-rules",
		Set:    "long",
		System: system("the rules engine of a turn-based skirmish game"),
		Files: map[string]string{
			"go.mod": goMod,
			"docs/dice.txt": `Dice

A battle has to replay exactly from its seed, on every platform, so the
game does not use math/rand. Its dice are a small linear congruential
generator instead.

The generator keeps one number, its state, which starts as the seed. To
roll a die with n sides:

  1. state = (state * 1103515245 + 12345) mod 2147483648
  2. the roll is 1 + (state / 65536) mod n

Do the arithmetic on unsigned 64-bit integers so that step 1 cannot
overflow. The division in step 2 is integer division. Every roll advances
the state once, whatever the number of sides.

Seed 42 gives these rolls for a die with 20 sides: 2, 14, 10.
`,
			"docs/rulebook.txt": `Skirmish rulebook (designer's copy, v3)

1. Sides

Every unit is on one of two teams, "hero" or "foe". A unit with 0 HP is
out: it stays in the battle but takes no further part in it.

2. Elements

Every unit has an element: plain, fire, frost or storm, and every attack
carries the attacker's element. Fire beats frost, frost beats storm and
storm beats fire.

An attack does 2 times the damage against an element it beats. Against the
element that beats it, or against its own element, it does 1/2 the damage.
Plain attacks, and attacks on plain units, are never changed.

The element factor applies to the damage of a hit once defense has been
taken off, and the result is rounded down.

3. Effects

poison  At the end of each of the poisoned unit's turns it loses an eighth
        of its MaxHP, rounded down but never under 1.
stun    A stunned unit does not act on its turn.
guard   The next hit the unit takes does half its damage, rounded down,
        and the guard is used up.

An effect lasts for a number of the unit's own turns. At the end of each of
its turns, after poison has done its work, every effect's count goes down
by one, and an effect whose count reaches zero ends.

4. Healing and revival

Healing never raises HP above MaxHP, and a unit that is out cannot be
healed at all. A revive brings a unit that is out back with a quarter of
its MaxHP, rounded down but never under 1. Reviving a unit that is not out
does nothing.

5. Levels

Every unit starts at level 1 with 0 XP. A unit reaches level 2 at 100 XP,
level 3 at 250 XP, level 4 at 450 XP and level 5 at 700 XP. Level 5 is the
highest there is.

6. Items (planned, not in this version)

A potion heals 20 HP and an ether removes every effect from one unit. Using
an item takes the unit's turn. Items are shared by the whole party and are
bought between battles, never during one.

7. Changes since v2

Storm no longer beats frost. Guard used to last until the end of the battle;
it now ends after one hit.
`,
			"docs/save-format.txt": `Battle save format, version 1

A save lets a player stop in the middle of a battle and carry on later, with
the dice exactly where they were. It is plain text, one record per line, and
every line ends with a newline, the last one included.

  line 1     skirmish save 1
  line 2     dice STATE, with the dice's current state in decimal
  the rest   one line per unit, in the battle's Units order

A unit line is the unit's roster line, as FormatUnit writes it, then a space,
a vertical bar and a space, then three fields separated by single spaces:
its current HP, its XP and its effects. Effects are written as kind:turns,
joined by commas in the unit's own order, or as a single - when it has none.

  skirmish save 1
  dice 1250496027
  Ayla hero fire 30 9 4 7 2 | 25 140 poison:2,guard:1
  Grub foe plain 20 6 2 5 1 | 0 0 -

The log is not saved, so a battle loaded from a save starts with an empty
log. Saves from other versions are refused.
`,
			"roster.go": `package skirmish

import (
	"fmt"
	"strconv"
	"strings"
)

// elementNames maps the words used in rosters to elements.
var elementNames = map[string]Element{
	"plain": Plain,
	"fire":  Fire,
	"frost": Frost,
	"storm": Storm,
}

// ParseUnit reads one roster line: name, team, element, MaxHP, Atk, Def, Spd
// and Level, separated by spaces, like "Ayla hero fire 30 9 4 7 1". The unit
// starts with full HP, no XP and no effects.
func ParseUnit(line string) (*Unit, error) {
	f := strings.Fields(line)
	if len(f) != 8 {
		return nil, fmt.Errorf("roster line %q: want 8 fields, got %d", line, len(f))
	}
	if f[1] != "hero" && f[1] != "foe" {
		return nil, fmt.Errorf("roster line %q: unknown team %q", line, f[1])
	}
	elem, ok := elementNames[f[2]]
	if !ok {
		return nil, fmt.Errorf("roster line %q: unknown element %q", line, f[2])
	}
	var n [5]int
	for i := range n {
		v, err := strconv.Atoi(f[3+i])
		if err != nil || v < 0 {
			return nil, fmt.Errorf("roster line %q: bad number %q", line, f[3+i])
		}
		n[i] = v
	}
	return &Unit{
		Name: f[0], Team: f[1], Elem: elem,
		HP: n[0], MaxHP: n[0], Atk: n[1], Def: n[2], Spd: n[3], Level: n[4],
	}, nil
}

// FormatUnit writes a unit's roster line, the inverse of ParseUnit. Only the
// fields a roster holds are written: current HP, XP and effects are not.
func FormatUnit(u *Unit) string {
	elem := "plain"
	for name, e := range elementNames {
		if e == u.Elem {
			elem = name
		}
	}
	return fmt.Sprintf("%s %s %s %d %d %d %d %d", u.Name, u.Team, elem, u.MaxHP, u.Atk, u.Def, u.Spd, u.Level)
}

// ParseRoster reads one unit per line. Blank lines and lines starting with #
// are skipped.
func ParseRoster(text string) ([]*Unit, error) {
	var units []*Unit
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := ParseUnit(line)
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
	return units, nil
}
`,
			"skirmish_test.go": `package skirmish

import "testing"

func TestParseUnitLine(t *testing.T) {
	u, err := ParseUnit("Ayla hero fire 30 9 4 7 2")
	if err != nil {
		t.Fatal(err)
	}
	want := Unit{Name: "Ayla", Team: "hero", Elem: Fire, HP: 30, MaxHP: 30, Atk: 9, Def: 4, Spd: 7, Level: 2}
	if u.Name != want.Name || u.Team != want.Team || u.Elem != want.Elem || u.HP != 30 || u.MaxHP != 30 ||
		u.Atk != 9 || u.Def != 4 || u.Spd != 7 || u.Level != 2 || !u.IsHero() {
		t.Fatalf("got %+v", u)
	}
}

func TestParseUnitRejects(t *testing.T) {
	for _, line := range []string{"", "Ayla hero fire 30 9 4 7", "Ayla ally fire 30 9 4 7 1", "Ayla hero mud 30 9 4 7 1", "Ayla hero fire x 9 4 7 1"} {
		if _, err := ParseUnit(line); err == nil {
			t.Errorf("%q accepted", line)
		}
	}
}

func TestParseRosterSkips(t *testing.T) {
	units, err := ParseRoster("# party\nAyla hero fire 30 9 4 7 1\n\nGrub foe plain 20 6 2 5 1\n")
	if err != nil || len(units) != 2 || units[1].IsHero() {
		t.Fatalf("%v, %v", units, err)
	}
}

func TestFormatUnitRoundTrip(t *testing.T) {
	for _, line := range []string{"Ayla hero fire 30 9 4 7 2", "Grub foe plain 20 6 2 5 1", "Mira hero storm 24 8 3 9 4"} {
		u, err := ParseUnit(line)
		if err != nil {
			t.Fatal(err)
		}
		u.HP, u.XP = 1, 50
		if got := FormatUnit(u); got != line {
			t.Errorf("FormatUnit = %q, want %q", got, line)
		}
	}
}
`,
			"unit.go": `package skirmish

// Element is what a unit fights with. Every attack carries the element of
// the unit making it.
type Element int

// The elements. Plain is the element of units without any.
const (
	Plain Element = iota
	Fire
	Frost
	Storm
)

// Effect is a lasting effect on a unit, such as "poison", "stun" or "guard".
// Turns is how many of the unit's own turns it has left.
type Effect struct {
	Kind  string
	Turns int
}

// Unit is one fighter on either side of a battle.
type Unit struct {
	Name  string
	Team  string // "hero" or "foe"
	Level int
	XP    int

	HP, MaxHP     int
	Atk, Def, Spd int
	Elem          Element

	Effects []Effect
}

// IsHero reports whether the unit fights on the heroes' side.
func (u *Unit) IsHero() bool { return u.Team == "hero" }
`,
		},
		Hidden: map[string]string{hiddenName: `package skirmish_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	skirmish "fixture"
)

func hero(name string, hp, atk, def int) *skirmish.Unit {
	return &skirmish.Unit{Name: name, Team: "hero", Level: 1, HP: hp, MaxHP: hp, Atk: atk, Def: def, Spd: 5}
}

func foe(name string, hp, atk, def int) *skirmish.Unit {
	return &skirmish.Unit{Name: name, Team: "foe", Level: 1, HP: hp, MaxHP: hp, Atk: atk, Def: def, Spd: 5}
}

// seedRolling finds a seed whose first roll of a die with 20 sides is want.
func seedRolling(t *testing.T, want int) uint32 {
	t.Helper()
	for s := uint32(1); s < 100000; s++ {
		if skirmish.NewDice(s).Roll(20) == want {
			return s
		}
	}
	t.Fatalf("no seed rolls %d", want)
	return 0
}

func names(units []*skirmish.Unit) []string {
	var out []string
	for _, u := range units {
		out = append(out, u.Name)
	}
	return out
}

func TestElementNames(t *testing.T) {
	for e, want := range map[skirmish.Element]string{skirmish.Plain: "plain", skirmish.Fire: "fire", skirmish.Frost: "frost", skirmish.Storm: "storm"} {
		if got := e.String(); got != want {
			t.Errorf("%d: %q", e, got)
		}
	}
}

func TestAliveAndTakeHit(t *testing.T) {
	u := hero("Ayla", 10, 1, 1)
	u.HP = 3
	if lost := u.TakeHit(5); lost != 3 || u.HP != 0 || u.Alive() {
		t.Fatalf("lost %d, HP %d", lost, u.HP)
	}
	u.HP = 9
	if lost := u.TakeHit(4); lost != 4 || u.HP != 5 || !u.Alive() {
		t.Fatalf("lost %d, HP %d", lost, u.HP)
	}
}

func TestDiceSequence(t *testing.T) {
	d := skirmish.NewDice(42)
	var got []int
	for _, sides := range []int{20, 20, 20, 6, 6, 100} {
		got = append(got, d.Roll(sides))
	}
	if !slices.Equal(got, []int{2, 14, 10, 4, 3, 94}) {
		t.Fatalf("rolls %v", got)
	}
}

func TestInitiative(t *testing.T) {
	ayla, dax, eve := hero("Ayla", 9, 1, 1), hero("Dax", 9, 1, 1), hero("Eve", 9, 1, 1)
	grub, bog := foe("Grub", 9, 1, 1), foe("Bog", 9, 1, 1)
	grub.Spd, eve.Spd, eve.HP = 7, 9, 0
	in := []*skirmish.Unit{dax, bog, eve, grub, ayla}
	got := skirmish.Initiative(in)
	if !slices.Equal(names(got), []string{"Grub", "Ayla", "Dax", "Bog"}) {
		t.Fatalf("order %v", names(got))
	}
	if in[0] != dax || in[4] != ayla {
		t.Error("Initiative reordered its argument")
	}
}

func TestAddEffectKeepsLonger(t *testing.T) {
	u := hero("Ayla", 9, 1, 1)
	u.AddEffect(skirmish.Effect{Kind: "poison", Turns: 2})
	u.AddEffect(skirmish.Effect{Kind: "stun", Turns: 1})
	u.AddEffect(skirmish.Effect{Kind: "poison", Turns: 4})
	u.AddEffect(skirmish.Effect{Kind: "stun", Turns: 0})
	want := []skirmish.Effect{{Kind: "poison", Turns: 4}, {Kind: "stun", Turns: 1}}
	if !slices.Equal(u.Effects, want) || !u.HasEffect("stun") || u.HasEffect("guard") {
		t.Fatalf("effects %v", u.Effects)
	}
}

func TestHealCap(t *testing.T) {
	u := hero("Ayla", 30, 1, 1)
	u.HP = 25
	if got := u.Heal(10); got != 5 || u.HP != 30 {
		t.Fatalf("gained %d, HP %d", got, u.HP)
	}
}

func TestHealOut(t *testing.T) {
	u := hero("Ayla", 30, 1, 1)
	u.HP = 0
	if got := u.Heal(10); got != 0 || u.HP != 0 {
		t.Fatalf("gained %d, HP %d", got, u.HP)
	}
}

func TestMultiplier(t *testing.T) {
	type pair struct{ a, b skirmish.Element }
	for p, want := range map[pair][2]int{
		{skirmish.Fire, skirmish.Frost}:  {2, 1},
		{skirmish.Storm, skirmish.Fire}:  {2, 1},
		{skirmish.Frost, skirmish.Fire}:  {1, 2},
		{skirmish.Storm, skirmish.Storm}: {1, 2},
		{skirmish.Plain, skirmish.Fire}:  {1, 1},
		{skirmish.Frost, skirmish.Plain}: {1, 1},
	} {
		if num, den := skirmish.Multiplier(p.a, p.b); num != want[0] || den != want[1] {
			t.Errorf("%v against %v: %d/%d", p.a, p.b, num, den)
		}
	}
}

func TestDamagePlain(t *testing.T) {
	if got := skirmish.Damage(hero("Ayla", 9, 10, 0), foe("Grub", 9, 1, 5)); got != 8 {
		t.Fatalf("Damage = %d", got)
	}
}

func TestDamageAtLeastOne(t *testing.T) {
	if got := skirmish.Damage(hero("Ayla", 9, 3, 0), foe("Grub", 9, 1, 10)); got != 1 {
		t.Fatalf("Damage = %d", got)
	}
}

func TestDamageElements(t *testing.T) {
	att, target := hero("Ayla", 9, 10, 0), foe("Grub", 9, 1, 4)
	att.Elem, target.Elem = skirmish.Fire, skirmish.Frost
	if got := skirmish.Damage(att, target); got != 16 {
		t.Errorf("fire on frost: %d", got)
	}
	att.Elem, target.Elem, target.Def = skirmish.Storm, skirmish.Storm, 3
	if got := skirmish.Damage(att, target); got != 4 {
		t.Errorf("storm on storm: %d", got)
	}
}

func TestLevelFor(t *testing.T) {
	for xp, want := range map[int]int{0: 1, 99: 1, 100: 2, 249: 2, 250: 3, 449: 3, 450: 4, 699: 4, 700: 5, 5000: 5} {
		if got := skirmish.LevelFor(xp); got != want {
			t.Errorf("LevelFor(%d) = %d, want %d", xp, got, want)
		}
	}
}

func TestGain(t *testing.T) {
	u := hero("Ayla", 9, 1, 1)
	if !u.Gain(300) || u.Level != 3 || u.XP != 300 {
		t.Fatalf("level %d, XP %d", u.Level, u.XP)
	}
	if u.Gain(10) || u.Level != 3 {
		t.Fatalf("level %d after 10 more", u.Level)
	}
}

func TestNewBattle(t *testing.T) {
	a, g := hero("Ayla", 9, 1, 1), foe("Grub", 9, 1, 1)
	b := skirmish.NewBattle(42, a, g)
	if len(b.Units) != 2 || b.Units[0] != a || b.Dice == nil || b.Dice.Roll(20) != 2 {
		t.Fatalf("battle %+v", b)
	}
}

func TestAttackHit(t *testing.T) {
	a, g := hero("Ayla", 9, 10, 0), foe("Grub", 40, 1, 5)
	b := skirmish.NewBattle(seedRolling(t, 10), a, g)
	if lost := b.Attack(a, g); lost != 8 || g.HP != 32 || !slices.Equal(b.Log, []string{"Ayla hits Grub for 8"}) {
		t.Fatalf("lost %d, HP %d, log %q", lost, g.HP, b.Log)
	}
}

func TestAttackMiss(t *testing.T) {
	a, g := hero("Ayla", 9, 10, 0), foe("Grub", 40, 1, 5)
	b := skirmish.NewBattle(seedRolling(t, 1), a, g)
	if lost := b.Attack(a, g); lost != 0 || g.HP != 40 || !slices.Equal(b.Log, []string{"Ayla misses Grub"}) {
		t.Fatalf("lost %d, HP %d, log %q", lost, g.HP, b.Log)
	}
}

func TestAttackCrit(t *testing.T) {
	a, g := hero("Ayla", 9, 10, 0), foe("Grub", 40, 1, 6)
	b := skirmish.NewBattle(seedRolling(t, 20), a, g)
	if lost := b.Attack(a, g); lost != 15 || g.HP != 25 || !slices.Equal(b.Log, []string{"Ayla crits Grub for 15"}) {
		t.Fatalf("lost %d, HP %d, log %q", lost, g.HP, b.Log)
	}
}

func TestAttackGuard(t *testing.T) {
	a, g := hero("Ayla", 9, 10, 0), foe("Grub", 40, 1, 0)
	g.AddEffect(skirmish.Effect{Kind: "guard", Turns: 3})
	b := skirmish.NewBattle(seedRolling(t, 10), a, g)
	if lost := b.Attack(a, g); lost != 5 || g.HasEffect("guard") {
		t.Fatalf("lost %d, effects %v", lost, g.Effects)
	}
}

func TestAttackLogsHPLost(t *testing.T) {
	a, g := hero("Ayla", 9, 10, 0), foe("Grub", 40, 1, 4)
	g.HP = 3
	b := skirmish.NewBattle(seedRolling(t, 10), a, g)
	if lost := b.Attack(a, g); lost != 3 || !slices.Equal(b.Log, []string{"Ayla hits Grub for 3"}) {
		t.Fatalf("lost %d, log %q", lost, b.Log)
	}
}

func TestEndTurnPoison(t *testing.T) {
	g := foe("Grub", 30, 1, 1)
	g.HP = 20
	g.AddEffect(skirmish.Effect{Kind: "poison", Turns: 2})
	b := skirmish.NewBattle(1, g)
	b.EndTurn(g)
	want := []skirmish.Effect{{Kind: "poison", Turns: 1}}
	if g.HP != 17 || !slices.Equal(g.Effects, want) || !slices.Equal(b.Log, []string{"Grub takes 3 from poison"}) {
		t.Fatalf("HP %d, effects %v, log %q", g.HP, g.Effects, b.Log)
	}
}

func TestEndTurnPoisonAtLeastOne(t *testing.T) {
	g := foe("Grub", 5, 1, 1)
	g.AddEffect(skirmish.Effect{Kind: "poison", Turns: 3})
	b := skirmish.NewBattle(1, g)
	b.EndTurn(g)
	if g.HP != 4 {
		t.Fatalf("HP %d", g.HP)
	}
}

func TestEffectsRunOut(t *testing.T) {
	g := foe("Grub", 30, 1, 1)
	g.AddEffect(skirmish.Effect{Kind: "stun", Turns: 1})
	g.AddEffect(skirmish.Effect{Kind: "guard", Turns: 3})
	b := skirmish.NewBattle(1, g)
	b.EndTurn(g)
	want := []skirmish.Effect{{Kind: "guard", Turns: 2}}
	if !slices.Equal(g.Effects, want) || g.HP != 30 || len(b.Log) != 0 {
		t.Fatalf("effects %v, HP %d, log %q", g.Effects, g.HP, b.Log)
	}
}

func TestCanActStunned(t *testing.T) {
	g := foe("Grub", 30, 1, 1)
	g.AddEffect(skirmish.Effect{Kind: "stun", Turns: 1})
	if skirmish.NewBattle(1, g).CanAct(g) {
		t.Fatal("a stunned unit acts")
	}
}

func TestCanActOut(t *testing.T) {
	g, a := foe("Grub", 30, 1, 1), hero("Ayla", 9, 1, 1)
	g.HP = 0
	b := skirmish.NewBattle(1, g, a)
	if b.CanAct(g) || !b.CanAct(a) {
		t.Fatal("wrong")
	}
}

func TestWinner(t *testing.T) {
	a, g := hero("Ayla", 9, 1, 1), foe("Grub", 9, 1, 1)
	b := skirmish.NewBattle(1, a, g)
	if w := b.Winner(); w != "" {
		t.Errorf("both standing: %q", w)
	}
	g.HP = 0
	if w := b.Winner(); w != "hero" {
		t.Errorf("foe out: %q", w)
	}
	a.HP = 0
	if w := b.Winner(); w != "" {
		t.Errorf("both out: %q", w)
	}
}

func TestRevive(t *testing.T) {
	a, c, g := hero("Ayla", 30, 1, 1), hero("Cid", 3, 1, 1), foe("Grub", 9, 1, 1)
	a.HP, c.HP = 0, 0
	b := skirmish.NewBattle(1, a, c, g)
	if !b.Revive(a) || a.HP != 7 || !b.Revive(c) || c.HP != 1 {
		t.Fatalf("HP %d and %d", a.HP, c.HP)
	}
	if b.Revive(g) || g.HP != 9 {
		t.Fatalf("revived a standing unit: HP %d", g.HP)
	}
	if !slices.Equal(b.Log, []string{"Ayla is back", "Cid is back"}) {
		t.Fatalf("log %q", b.Log)
	}
}

func TestPickTargetHurtsMost(t *testing.T) {
	sturdy, open := hero("Bo", 5, 1, 8), hero("Ayla", 30, 1, 2)
	g := foe("Grub", 9, 10, 1)
	b := skirmish.NewBattle(1, sturdy, g, open)
	if got := b.PickTarget(g); got != open {
		t.Fatalf("picked %v", got)
	}
}

func TestPickTargetTies(t *testing.T) {
	x, y, z := hero("Xan", 20, 1, 2), hero("Yul", 12, 1, 2), hero("Zed", 12, 1, 2)
	g := foe("Grub", 9, 10, 1)
	b := skirmish.NewBattle(1, x, y, z, g)
	if got := b.PickTarget(g); got != y {
		t.Fatalf("picked %v", got)
	}
	x.HP, y.HP, z.HP = 0, 0, 0
	if got := b.PickTarget(g); got != nil {
		t.Fatalf("picked %v with no hero standing", got)
	}
}

func TestStanding(t *testing.T) {
	a, c, g := hero("Ayla", 9, 1, 1), hero("Cid", 9, 1, 1), foe("Grub", 9, 1, 1)
	c.HP = 0
	b := skirmish.NewBattle(1, g, c, a)
	if got := names(b.Standing("hero")); !slices.Equal(got, []string{"Ayla"}) {
		t.Fatalf("Standing = %v", got)
	}
}

func TestAward(t *testing.T) {
	a, c, d := hero("Ayla", 9, 1, 1), hero("Cid", 9, 1, 1), hero("Dax", 9, 1, 1)
	g := foe("Grub", 9, 1, 1)
	g.Level, c.HP = 3, 0
	b := skirmish.NewBattle(1, a, c, d, g)
	b.Award(g)
	if a.XP != 15 || c.XP != 0 || d.XP != 15 || !slices.Equal(b.Log, []string{"Ayla gains 15 XP", "Dax gains 15 XP"}) {
		t.Fatalf("XP %d %d %d, log %q", a.XP, c.XP, d.XP, b.Log)
	}
}

func TestAwardRemainder(t *testing.T) {
	a, c, d := hero("Ayla", 9, 1, 1), hero("Cid", 9, 1, 1), hero("Dax", 9, 1, 1)
	g := foe("Grub", 9, 1, 1)
	g.Level = 2
	b := skirmish.NewBattle(1, g, a, c, d)
	b.Award(g)
	if a.XP != 8 || c.XP != 6 || d.XP != 6 {
		t.Fatalf("XP %d %d %d", a.XP, c.XP, d.XP)
	}
}

func TestTakeTurnKnockout(t *testing.T) {
	a, g := hero("Ayla", 9, 10, 1), foe("Grub", 3, 1, 0)
	g.Level = 2
	b := skirmish.NewBattle(seedRolling(t, 10), a, g)
	b.TakeTurn(a, g)
	want := []string{"Ayla hits Grub for 3", "Grub is out", "Ayla gains 20 XP"}
	if !slices.Equal(b.Log, want) || a.XP != 20 {
		t.Fatalf("log %q, XP %d", b.Log, a.XP)
	}
}

func TestTakeTurnStunned(t *testing.T) {
	a, g := hero("Ayla", 9, 10, 1), foe("Grub", 30, 1, 0)
	a.AddEffect(skirmish.Effect{Kind: "stun", Turns: 1})
	b := skirmish.NewBattle(seedRolling(t, 10), a, g)
	b.TakeTurn(a, g)
	if g.HP != 30 || len(b.Log) != 0 || a.HasEffect("stun") {
		t.Fatalf("HP %d, log %q, effects %v", g.HP, b.Log, a.Effects)
	}
}

func TestPreview(t *testing.T) {
	a, g := hero("Ayla", 9, 10, 0), foe("Grub", 40, 1, 6)
	g.AddEffect(skirmish.Effect{Kind: "guard", Turns: 1})
	b := skirmish.NewBattle(42, a, g)
	hit, crit := b.Preview(a, g)
	if hit != 7 || crit != 15 || g.HP != 40 || !g.HasEffect("guard") || b.Dice.Roll(20) != 2 {
		t.Fatalf("Preview = %d, %d", hit, crit)
	}
}

func TestUnitString(t *testing.T) {
	a := hero("Ayla", 30, 1, 1)
	a.HP = 25
	if got := a.String(); got != "Ayla 25/30" {
		t.Errorf("String = %q", got)
	}
	a.HP = 0
	if got := a.String(); got != "Ayla 0/30 (out)" {
		t.Errorf("String = %q", got)
	}
}

func savedBattle(t *testing.T) *skirmish.Battle {
	t.Helper()
	a, err := skirmish.ParseUnit("Ayla hero fire 30 9 4 7 2")
	if err != nil {
		t.Fatal(err)
	}
	g, err := skirmish.ParseUnit("Grub foe plain 20 6 2 5 1")
	if err != nil {
		t.Fatal(err)
	}
	a.HP, a.XP, g.HP = 25, 140, 0
	a.Effects = []skirmish.Effect{{Kind: "poison", Turns: 2}, {Kind: "guard", Turns: 1}}
	return skirmish.NewBattle(42, a, g)
}

const wantSave = "skirmish save 1\ndice 42\nAyla hero fire 30 9 4 7 2 | 25 140 poison:2,guard:1\nGrub foe plain 20 6 2 5 1 | 0 0 -\n"

func TestSave(t *testing.T) {
	if got := savedBattle(t).Save(); got != wantSave {
		t.Fatalf("Save = %q", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	b := savedBattle(t)
	b.Dice.Roll(20)
	loaded, err := skirmish.Load(b.Save())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Save() != b.Save() || loaded.Dice.Roll(20) != b.Dice.Roll(20) {
		t.Fatalf("loaded %q", loaded.Save())
	}
}

func TestLoad(t *testing.T) {
	b, err := skirmish.Load(wantSave)
	if err != nil {
		t.Fatal(err)
	}
	a := b.Units[0]
	want := []skirmish.Effect{{Kind: "poison", Turns: 2}, {Kind: "guard", Turns: 1}}
	if len(b.Units) != 2 || a.Name != "Ayla" || a.HP != 25 || a.MaxHP != 30 || a.XP != 140 || a.Level != 2 ||
		!slices.Equal(a.Effects, want) || b.Units[1].HP != 0 || len(b.Log) != 0 || b.Dice.Roll(20) != 2 {
		t.Fatalf("loaded %+v", b.Units)
	}
}

func TestLoadRejects(t *testing.T) {
	for _, text := range []string{"", "skirmish save 2\ndice 42\n", "skirmish save 1\ndice x\n", "skirmish save 1\ndice 42\nAyla hero fire 30 9 4 7 2\n"} {
		if _, err := skirmish.Load(text); !errors.Is(err, skirmish.ErrBadSave) {
			t.Errorf("%q: %v", text, err)
		}
	}
}

// seedRollingTwoHits finds a seed whose first two rolls of a die with 20
// sides are ordinary hits.
func seedRollingTwoHits(t *testing.T) uint32 {
	t.Helper()
	for s := uint32(1); s < 100000; s++ {
		d := skirmish.NewDice(s)
		if r1, r2 := d.Roll(20), d.Roll(20); r1 > 1 && r1 < 20 && r2 > 1 && r2 < 20 {
			return s
		}
	}
	t.Fatal("no seed")
	return 0
}

func TestRound(t *testing.T) {
	a, g := hero("Ayla", 30, 10, 0), foe("Grub", 30, 6, 0)
	a.Spd = 7
	b := skirmish.NewBattle(seedRollingTwoHits(t), g, a)
	b.Round(func(*skirmish.Unit) *skirmish.Unit { return g })
	if want := []string{"Ayla hits Grub for 10", "Grub hits Ayla for 6"}; !slices.Equal(b.Log, want) {
		t.Fatalf("log %q", b.Log)
	}
}

func TestRoundStopsAtWinner(t *testing.T) {
	a, bo, g := hero("Ayla", 30, 10, 0), hero("Bo", 30, 4, 0), foe("Grub", 5, 6, 0)
	a.Spd, bo.Spd = 9, 3
	b := skirmish.NewBattle(seedRollingTwoHits(t), a, bo, g)
	b.Round(func(*skirmish.Unit) *skirmish.Unit { return g })
	for _, line := range b.Log {
		if strings.HasPrefix(line, "Bo ") && !strings.HasPrefix(line, "Bo gains") || strings.HasPrefix(line, "Grub ") && line != "Grub is out" {
			t.Fatalf("log %q", b.Log)
		}
	}
	if g.Alive() || b.Winner() != "hero" || bo.HP != 30 {
		t.Fatalf("log %q", b.Log)
	}
}
`},
		Solution: map[string]string{
			"battle.go": `package skirmish

import (
	"fmt"
	"sort"
)

// Battle is one fight between heroes and foes. Log holds one line for each
// thing that happened, oldest first.
type Battle struct {
	Units []*Unit
	Dice  *Dice
	Log   []string
}

// NewBattle returns a battle between the given units, with dice seeded by
// seed.
func NewBattle(seed uint32, units ...*Unit) *Battle {
	return &Battle{Units: units, Dice: NewDice(seed)}
}

func (b *Battle) logf(format string, args ...any) {
	b.Log = append(b.Log, fmt.Sprintf(format, args...))
}

// Initiative returns the units still standing in the order they act: highest
// Spd first, heroes before foes on equal Spd, and then by Name.
func Initiative(units []*Unit) []*Unit {
	var out []*Unit
	for _, u := range units {
		if u.Alive() {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if a.Spd != c.Spd {
			return a.Spd > c.Spd
		}
		if a.IsHero() != c.IsHero() {
			return a.IsHero()
		}
		return a.Name < c.Name
	})
	return out
}

// Standing returns the units of a team that are still standing, in Units
// order.
func (b *Battle) Standing(team string) []*Unit {
	var out []*Unit
	for _, u := range b.Units {
		if u.Team == team && u.Alive() {
			out = append(out, u)
		}
	}
	return out
}

// Winner returns the team of the one side that still has a unit standing,
// and "" while both sides do, or neither does.
func (b *Battle) Winner() string {
	heroes, foes := len(b.Standing("hero")) > 0, len(b.Standing("foe")) > 0
	switch {
	case heroes && !foes:
		return "hero"
	case foes && !heroes:
		return "foe"
	}
	return ""
}

// PickTarget returns the hero foe attacks: the standing hero its ordinary
// hit hurts most, then the one with less HP, then the first in Units. It
// returns nil when no hero is standing.
func (b *Battle) PickTarget(foe *Unit) *Unit {
	var best *Unit
	bestDmg := 0
	for _, h := range b.Standing("hero") {
		dmg := Damage(foe, h)
		if best == nil || dmg > bestDmg || dmg == bestDmg && h.HP < best.HP {
			best, bestDmg = h, dmg
		}
	}
	return best
}

// Award hands out the XP for a defeated foe: 10 per Level of the foe, split
// evenly between the heroes still standing, with the remainder going to the
// first of them. Each hero who gets XP is logged, in Units order.
func (b *Battle) Award(defeated *Unit) {
	heroes := b.Standing("hero")
	if len(heroes) == 0 {
		return
	}
	total := 10 * defeated.Level
	share, rest := total/len(heroes), total%len(heroes)
	for i, h := range heroes {
		xp := share
		if i == 0 {
			xp += rest
		}
		if xp > 0 {
			h.Gain(xp)
			b.logf("%s gains %d XP", h.Name, xp)
		}
	}
}
`,
			"damage.go": `package skirmish

// A critical does critNum/critDen of what an ordinary hit would do to a
// target without Def, rounded down.
const critNum, critDen = 3, 2

// atLeastOne keeps damage at 1 or more: whatever changes it, a hit always
// does at least 1.
func atLeastOne(n int) int { return max(n, 1) }

// Damage returns what an ordinary hit from att does to target: Atk minus
// half the target's Def, the half rounded down, then the element factor,
// rounded down.
func Damage(att, target *Unit) int {
	num, den := Multiplier(att.Elem, target.Elem)
	base := att.Atk - target.Def/2
	return atLeastOne(base * num / den)
}

// critDamage returns what a critical from att does to target, before any
// guard.
func critDamage(att, target *Unit) int {
	bare := *target
	bare.Def = 0
	return atLeastOne(Damage(att, &bare) * critNum / critDen)
}

// Preview returns what an ordinary hit and a critical from att would do to
// target before any guard, without rolling or changing anything.
func (b *Battle) Preview(att, target *Unit) (hit, crit int) {
	return Damage(att, target), critDamage(att, target)
}
`,
			"dice.go": `package skirmish

// Dice is the battle's random source: the linear congruential generator of
// docs/dice.txt, so that a battle replays exactly from its seed.
type Dice struct {
	state uint64
}

// NewDice returns dice whose state starts as seed.
func NewDice(seed uint32) *Dice { return &Dice{state: uint64(seed)} }

// Roll rolls a die with the given number of sides, from 1 to sides.
func (d *Dice) Roll(sides int) int {
	d.state = (d.state*1103515245 + 12345) % 2147483648
	return 1 + int((d.state/65536)%uint64(sides))
}
`,
			"effects.go": `package skirmish

// Effect is a lasting effect on a unit, such as "poison", "stun" or "guard".
// Turns is how many of the unit's own turns it has left.
type Effect struct {
	Kind  string
	Turns int
}

// HasEffect reports whether the unit has an effect of the given kind.
func (u *Unit) HasEffect(kind string) bool {
	for _, e := range u.Effects {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// AddEffect gives the unit an effect. A unit holds one effect of each kind:
// when it already has one, the one with more Turns is kept.
func (u *Unit) AddEffect(e Effect) {
	for i, have := range u.Effects {
		if have.Kind == e.Kind {
			u.Effects[i].Turns = max(have.Turns, e.Turns)
			return
		}
	}
	u.Effects = append(u.Effects, e)
}

// removeEffect drops the unit's effect of the given kind, if it has one.
func (u *Unit) removeEffect(kind string) {
	for i, e := range u.Effects {
		if e.Kind == kind {
			u.Effects = append(u.Effects[:i], u.Effects[i+1:]...)
			return
		}
	}
}

// EndTurn applies the end of u's turn (docs/rulebook.txt): poison takes an
// eighth of MaxHP, at least 1, and then every effect counts down one turn,
// ending when it reaches zero.
func (b *Battle) EndTurn(u *Unit) {
	if u.HasEffect("poison") {
		lost := u.TakeHit(atLeastOne(u.MaxHP / 8))
		b.logf("%s takes %d from poison", u.Name, lost)
	}
	kept := u.Effects[:0]
	for _, e := range u.Effects {
		if e.Turns--; e.Turns > 0 {
			kept = append(kept, e)
		}
	}
	u.Effects = kept
}

// CanAct reports whether u gets to act on its turn: it has to be standing
// and not stunned.
func (b *Battle) CanAct(u *Unit) bool {
	return u.Alive() && !u.HasEffect("stun")
}

// Revive brings a unit that is out back with a quarter of its MaxHP, at
// least 1, and logs it. It reports whether it did anything.
func (b *Battle) Revive(u *Unit) bool {
	if u.Alive() {
		return false
	}
	u.HP = atLeastOne(u.MaxHP / 4)
	b.logf("%s is back", u.Name)
	return true
}
`,
			"element.go": `package skirmish

// Element is what a unit fights with. Every attack carries the element of
// the unit making it.
type Element int

// The elements. Plain is the element of units without any.
const (
	Plain Element = iota
	Fire
	Frost
	Storm
)

// String returns "plain", "fire", "frost" or "storm".
func (e Element) String() string {
	switch e {
	case Fire:
		return "fire"
	case Frost:
		return "frost"
	case Storm:
		return "storm"
	}
	return "plain"
}

// beats reports whether attacks of element a do extra damage to element b:
// fire beats frost, frost beats storm and storm beats fire.
func beats(a, b Element) bool {
	return a == Fire && b == Frost || a == Frost && b == Storm || a == Storm && b == Fire
}

// Multiplier returns the factor by which an attack of one element changes
// damage against a target of another, as a fraction in lowest terms. Plain
// is never changed; an element doubles against the one it beats and halves
// against the one that beats it and against itself (docs/rulebook.txt).
func Multiplier(attack, target Element) (num, den int) {
	switch {
	case attack == Plain || target == Plain:
		return 1, 1
	case beats(attack, target):
		return 2, 1
	}
	return 1, 2
}
`,
			"save.go": `package skirmish

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrBadSave is returned by Load for text that is not a battle save.
var ErrBadSave = errors.New("not a battle save")

// saveHeader is the first line of a save (docs/save-format.txt).
const saveHeader = "skirmish save 1"

// Save writes the battle in the save format of docs/save-format.txt: a
// header, the dice state and one line per unit, in Units order.
func (b *Battle) Save() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\ndice %d\n", saveHeader, b.Dice.state)
	for _, u := range b.Units {
		fmt.Fprintf(&sb, "%s | %d %d %s\n", FormatUnit(u), u.HP, u.XP, formatEffects(u.Effects))
	}
	return sb.String()
}

// formatEffects writes effects as kind:turns joined by commas, or "-".
func formatEffects(effects []Effect) string {
	if len(effects) == 0 {
		return "-"
	}
	parts := make([]string, len(effects))
	for i, e := range effects {
		parts[i] = e.Kind + ":" + strconv.Itoa(e.Turns)
	}
	return strings.Join(parts, ",")
}

// Load reads a battle back from a save, the inverse of Save. Anything that
// is not a save gives ErrBadSave.
func Load(save string) (*Battle, error) {
	lines := strings.Split(strings.TrimSuffix(save, "\n"), "\n")
	if len(lines) < 2 || lines[0] != saveHeader {
		return nil, ErrBadSave
	}
	digits, ok := strings.CutPrefix(lines[1], "dice ")
	state, err := strconv.ParseUint(digits, 10, 64)
	if !ok || err != nil {
		return nil, ErrBadSave
	}
	b := &Battle{Dice: &Dice{state: state}}
	for _, line := range lines[2:] {
		u, err := loadUnit(line)
		if err != nil {
			return nil, ErrBadSave
		}
		b.Units = append(b.Units, u)
	}
	return b, nil
}

// loadUnit reads one unit line of a save.
func loadUnit(line string) (*Unit, error) {
	roster, rest, ok := strings.Cut(line, " | ")
	f := strings.Fields(rest)
	if !ok || len(f) != 3 {
		return nil, ErrBadSave
	}
	u, err := ParseUnit(roster)
	if err != nil {
		return nil, err
	}
	if u.HP, err = strconv.Atoi(f[0]); err != nil {
		return nil, err
	}
	if u.XP, err = strconv.Atoi(f[1]); err != nil {
		return nil, err
	}
	if f[2] == "-" {
		return u, nil
	}
	for _, part := range strings.Split(f[2], ",") {
		kind, turns, ok := strings.Cut(part, ":")
		n, err := strconv.Atoi(turns)
		if !ok || err != nil {
			return nil, ErrBadSave
		}
		u.Effects = append(u.Effects, Effect{Kind: kind, Turns: n})
	}
	return u, nil
}
`,
			"turn.go": `package skirmish

// Attack makes one attack. It rolls a die with 20 sides: a 1 misses, a 20 is
// a critical and anything else is an ordinary hit. A guard on the target
// halves the damage and is used up. The damage is applied with TakeHit, one
// line is logged and the HP lost is returned.
func (b *Battle) Attack(att, target *Unit) int {
	roll := b.Dice.Roll(20)
	verb := "hits"
	dmg := Damage(att, target)
	switch roll {
	case 1:
		b.logf("%s misses %s", att.Name, target.Name)
		return 0
	case 20:
		verb, dmg = "crits", critDamage(att, target)
	}
	if target.HasEffect("guard") {
		dmg = atLeastOne(dmg / 2)
		target.removeEffect("guard")
	}
	lost := target.TakeHit(dmg)
	b.logf("%s %s %s for %d", att.Name, verb, target.Name, lost)
	return lost
}

// TakeTurn plays u's whole turn. If u can act, it attacks target, and when
// that puts target out it is logged and, for a foe, its XP is awarded.
// Either way u's turn then ends.
func (b *Battle) TakeTurn(u, target *Unit) {
	if b.CanAct(u) {
		b.Attack(u, target)
		if !target.Alive() {
			b.logf("%s is out", target.Name)
			if !target.IsHero() {
				b.Award(target)
			}
		}
	}
	b.EndTurn(u)
}

// Round plays one round. The units standing when it starts take their turns
// in Initiative order, skipping any put out meanwhile: a hero attacks
// heroTarget(hero) and a foe attacks PickTarget(foe), and a unit without a
// target only ends its turn. The round stops as soon as there is a Winner.
func (b *Battle) Round(heroTarget func(*Unit) *Unit) {
	for _, u := range Initiative(b.Units) {
		if b.Winner() != "" {
			return
		}
		if !u.Alive() {
			continue
		}
		target := b.PickTarget(u)
		if u.IsHero() {
			target = heroTarget(u)
		}
		if target == nil {
			b.EndTurn(u)
			continue
		}
		b.TakeTurn(u, target)
	}
}
`,
			"unit.go": `package skirmish

import "fmt"

// Unit is one fighter on either side of a battle.
type Unit struct {
	Name  string
	Team  string // "hero" or "foe"
	Level int
	XP    int

	HP, MaxHP     int
	Atk, Def, Spd int
	Elem          Element

	Effects []Effect
}

// IsHero reports whether the unit fights on the heroes' side.
func (u *Unit) IsHero() bool { return u.Team == "hero" }

// Alive reports whether the unit is still standing.
func (u *Unit) Alive() bool { return u.HP > 0 }

// String describes the unit as Name and HP over MaxHP, like "Ayla 25/30",
// with " (out)" after it when the unit is out.
func (u *Unit) String() string {
	s := fmt.Sprintf("%s %d/%d", u.Name, u.HP, u.MaxHP)
	if !u.Alive() {
		s += " (out)"
	}
	return s
}

// TakeHit lowers HP by dmg, never below 0, and returns the HP actually lost.
func (u *Unit) TakeHit(dmg int) int {
	lost := min(dmg, u.HP)
	u.HP -= lost
	return lost
}

// Heal raises HP by n, never above MaxHP, and returns the HP actually
// gained. A unit that is out cannot be healed (docs/rulebook.txt).
func (u *Unit) Heal(n int) int {
	if !u.Alive() {
		return 0
	}
	gained := min(n, u.MaxHP-u.HP)
	u.HP += gained
	return gained
}

// levelXP is the XP at which each level starts: level i+1 at levelXP[i].
var levelXP = []int{0, 100, 250, 450, 700}

// LevelFor returns the level a unit with that much XP has reached.
func LevelFor(xp int) int {
	level := 0
	for level < len(levelXP) && xp >= levelXP[level] {
		level++
	}
	return max(level, 1)
}

// Gain adds xp to the unit's XP and raises its Level to LevelFor(XP) when
// that is higher. It reports whether the level went up.
func (u *Unit) Gain(xp int) bool {
	u.XP += xp
	if l := LevelFor(u.XP); l > u.Level {
		u.Level = l
		return true
	}
	return false
}
`,
		},
		Turns: script(
			cloud("The damage rule we agreed on, for later: a hit does the attacker's Atk minus half the target's Def, the half rounded down, and whatever else changes damage later, a hit never does less than 1. Before any of that, add `func (u *Unit) Alive() bool`, true while HP is above zero, and `func (u *Unit) TakeHit(dmg int) int`, which lowers HP by dmg, never below 0, and returns the HP actually lost."),
			local("Read docs/rulebook.txt and tell me which parts of it we haven't built yet. No code changes."),
			local("Add `func (e Element) String() string` to unit.go: \"plain\", \"fire\", \"frost\" or \"storm\"."),
			cloud("Read docs/dice.txt and add a new dice.go with `type Dice`, `func NewDice(seed uint32) *Dice` and `func (d *Dice) Roll(sides int) int`, exactly as it describes, so that a battle replays from its seed."),
			cloud("Add `func Initiative(units []*Unit) []*Unit` in a new battle.go: a new slice of the units still standing, highest Spd first. On equal Spd heroes go before foes, and after that it goes by Name."),
			cloud("Attack rolls, for when we write attacks: every attack rolls a die with 20 sides from the battle's dice. A 1 misses outright. A 20 is a critical, which does double what an ordinary hit would do to that target if it had no Def. Anything else is an ordinary hit. For now add `func (u *Unit) HasEffect(kind string) bool` to unit.go."),
			local("Add `func (u *Unit) AddEffect(e Effect)` to unit.go: it appends e to Effects, but when the unit already has an effect of that Kind it keeps a single one, with the larger Turns."),
			local("Add `func (u *Unit) Heal(n int) int` to unit.go: it raises HP by n and returns the HP actually gained, following the rulebook."),
			local("Add `type Battle struct { Units []*Unit; Dice *Dice; Log []string }` and `func NewBattle(seed uint32, units ...*Unit) *Battle`, with Dice made from seed, to battle.go. And a rule for the enemy AI later on: a foe always goes after the standing hero its ordinary hit would hurt most; between equals, the one with less HP, and then whoever comes first in Units."),
			cloud("Add `func Multiplier(attack, target Element) (num, den int)` to unit.go: the factor by which an attack of one element changes damage against a target of another, as a fraction in lowest terms."),
			local("Add `func Damage(att, target *Unit) int` in a new damage.go: what an ordinary hit from att does to target, under our damage rule and the element factor."),
			cloud("Add `func LevelFor(xp int) int` to unit.go: the level a unit with that much XP has reached. And for when we hand out XP: a defeated foe is worth 10 XP per Level, split evenly between the heroes still standing, with whatever doesn't divide going to the first of them in Units."),
			cloud("unit.go is getting long. Move Element, its String and Multiplier into a new element.go, and Effect, HasEffect and AddEffect into a new effects.go. No behaviour changes."),
			cloud("Read docs/save-format.txt and tell me whether Battle and Dice keep everything a save needs. No code changes."),
			local("Add `func (u *Unit) Gain(xp int) bool` to unit.go: it adds xp to XP, raises Level to LevelFor(XP) when that is higher, and reports whether Level went up."),
			local("Add `func (b *Battle) Attack(att, target *Unit) int` in a new turn.go: one attack under our rules, including any guard the target has. It applies the damage with TakeHit, returns the HP lost and logs one line, \"Ayla hits Grub for 7\", \"Ayla crits Grub for 12\" or \"Ayla misses Grub\", the number being the HP lost."),
			cloud("Add `func (b *Battle) EndTurn(u *Unit)` to effects.go: whatever the rulebook says happens at the end of u's turn. When poison bites, log \"Grub takes 3 from poison\" with the HP lost."),
			cloud("Add `func (b *Battle) CanAct(u *Unit) bool` to effects.go: whether u gets to act on its turn."),
			local("Add `func (b *Battle) Winner() string` to battle.go: the Team of the one side that still has a unit standing, or \"\" while both sides do, or neither does."),
			local("Add `func (b *Battle) Revive(u *Unit) bool` to battle.go: it revives u the way the rulebook says and reports whether that did anything; when it did, log \"Ayla is back\"."),
			cloud("Balance pass: criticals are too swingy. From now on a critical does 3/2 of that same base instead, rounded down. Bring the code in line."),
			cloud("Add `func (b *Battle) PickTarget(foe *Unit) *Unit` to battle.go: the hero foe attacks on its turn, or nil when no hero is standing."),
			local("Add `func (b *Battle) Award(defeated *Unit)` to battle.go: it hands out the XP a defeated foe is worth, through Gain, logging \"Ayla gains 15 XP\" for each hero who gets some, in Units order."),
			local("Add `func (b *Battle) Standing(team string) []*Unit` to battle.go: the units of that team still standing, in Units order."),
			cloud("Add `func (b *Battle) TakeTurn(u, target *Unit)` to turn.go, u's whole turn: if u can act it attacks target, and when that puts target out, log \"Grub is out\" and, if target is a foe, award its XP. Either way u's turn then ends."),
			local("Add `func (u *Unit) String() string` to unit.go: the unit's Name and its HP over MaxHP, like \"Ayla 25/30\", with \" (out)\" after it when the unit is out."),
			local("Add `func (b *Battle) Save() string` in a new save.go: the battle written out in our save format."),
			cloud("Add `var ErrBadSave = errors.New(\"not a battle save\")` and `func Load(save string) (*Battle, error)` to save.go, the inverse of Save. Anything that is not a save gives ErrBadSave."),
			cloud("Add `func (b *Battle) Round(heroTarget func(*Unit) *Unit)` to turn.go, one round: the units standing at its start take their turns in Initiative order, skipping any put out meanwhile. A hero attacks heroTarget(hero), a foe attacks PickTarget(foe), and a unit with a nil target just ends its turn. The round stops as soon as Winner() is not \"\"."),
			cloud("battle.go is getting long. Make Winner, PickTarget and Award use Standing, and move Revive into effects.go next to the other rulebook code. No behaviour changes."),
			local("Add `func (b *Battle) Preview(att, target *Unit) (hit, crit int)` to damage.go for the UI: what an ordinary hit and a critical from att would do to target before any guard, without rolling or changing anything."),
		),
		Carries: []Carry{
			{
				What:        "a hit does Atk minus half the target's Def, rounded down, and never less than 1",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{10, 30},
				Tests:       []string{"TestDamagePlain", "TestDamageAtLeastOne", "TestDamageElements", "TestAttackHit", "TestPreview"},
				Absent:      []string{"half the target", "minus half", "less than 1", "atk minus"},
			},
			{
				What:        "element factors: fire beats frost beats storm beats fire, 2 against what an element beats, 1/2 against what beats it and against itself (docs/rulebook.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{9, 10},
				Source:      "docs/rulebook.txt",
				Tests:       []string{"TestMultiplier", "TestDamageElements"},
				Absent:      []string{"beats", "twice the damage", "2 times", "half the damage"},
			},
			{
				What:        "effects and healing: poison an eighth of MaxHP, stun, guard, countdown, no healing when out, revive to a quarter (docs/rulebook.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{7, 15, 16, 17, 19},
				Source:      "docs/rulebook.txt",
				Tests: []string{"TestHealOut", "TestAttackGuard", "TestEndTurnPoison", "TestEndTurnPoisonAtLeastOne",
					"TestEffectsRunOut", "TestCanActStunned", "TestRevive", "TestTakeTurnStunned"},
				Absent: []string{"eighth", "1/8", "a quarter", "stunned", "used up"},
			},
			{
				What:        "a roll of 1 misses, a 20 is a critical against the target as if it had no Def",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{15, 30},
				Tests:       []string{"TestAttackMiss", "TestAttackCrit", "TestPreview"},
				Absent:      []string{"outright", "a 20", "double", "no def"},
			},
			{
				What:        "criticals revised from double to 3/2, rounded down",
				Kind:        CarryUser,
				Established: 20,
				Needed:      []int{30},
				Tests:       []string{"TestAttackCrit", "TestPreview"},
				Absent:      []string{"3/2", "half again", "one and a half", "balance pass", "swingy"},
			},
			{
				What:        "a foe targets the hero it hurts most, then the one with less HP, then the first in Units",
				Kind:        CarryUser,
				Established: 8,
				Needed:      []int{21, 28},
				Tests:       []string{"TestPickTargetHurtsMost", "TestPickTargetTies"},
				Absent:      []string{"hurt most", "less hp", "comes first", "enemy ai"},
			},
			{
				What:        "a defeated foe is worth 10 XP a level, split among standing heroes, remainder to the first",
				Kind:        CarryUser,
				Established: 11,
				Needed:      []int{22, 24},
				Tests:       []string{"TestAward", "TestAwardRemainder"},
				Absent:      []string{"10 xp", "per level", "split evenly", "doesn't divide", "first of them"},
			},
			{
				What:        "levels start at 0, 100, 250, 450 and 700 XP, with 5 the highest (docs/rulebook.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{11, 14},
				Source:      "docs/rulebook.txt",
				Tests:       []string{"TestLevelFor", "TestGain"},
				Absent:      []string{"250", "450", "700", "level 5"},
			},
			{
				What:        "the save format: header, dice line, roster line, a bar, HP, XP and kind:turns effects (docs/save-format.txt)",
				Kind:        CarryDiscovered,
				Established: 13,
				Needed:      []int{26, 27},
				Source:      "docs/save-format.txt",
				Tests:       []string{"TestSave", "TestSaveLoadRoundTrip"},
				Absent:      []string{"skirmish save", "vertical bar", "kind:turns", "save-format", "dice state"},
			},
		},
	}
}
