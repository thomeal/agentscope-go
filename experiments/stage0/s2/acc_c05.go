package s2

func accC05() Task {
	return Task{
		ID:     "s2-c05-recipe-scaling",
		Set:    "acceptance",
		System: system("a recipe scaling library"),
		Files: map[string]string{
			"go.mod": goMod,
			"recipe.go": `package recipe

// Ingredient is one line of a recipe. Unit is "g", "ml", "each" or "pinch".
type Ingredient struct {
	Name   string
	Amount float64
	Unit   string
}

// Recipe is a dish and the number of people it serves.
type Recipe struct {
	Name   string
	Serves int
	Items  []Ingredient
}

// Count reports how many ingredients the recipe has.
func (r Recipe) Count() int { return len(r.Items) }
`,
			"docs/kitchen.txt": `Kitchen notes

Anything measured in pinches is seasoning, done by feel. However big the batch
gets, an amount in pinches stays exactly as written in the recipe.

Prep lists are printed at six each morning and initialled by the sous chef.
`,
			"recipe_test.go": `package recipe

import "testing"

func TestCount(t *testing.T) {
	r := Recipe{Name: "soup", Serves: 2, Items: []Ingredient{{Name: "leek", Amount: 2, Unit: "each"}}}
	if r.Count() != 1 {
		t.Fatalf("Count = %d", r.Count())
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package recipe_test

import (
	"math"
	"testing"

	recipe "fixture"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func cake() recipe.Recipe {
	return recipe.Recipe{Name: "cake", Serves: 4, Items: []recipe.Ingredient{
		{Name: "flour", Amount: 200, Unit: "g"},
		{Name: "eggs", Amount: 3, Unit: "each"},
		{Name: "salt", Amount: 1, Unit: "pinch"},
		{Name: "milk", Amount: 50, Unit: "ml"},
	}}
}

func amount(t *testing.T, r recipe.Recipe, name string) float64 {
	t.Helper()
	got, ok := r.Find(name)
	if !ok {
		t.Fatalf("no %s in %+v", name, r)
	}
	return got.Amount
}

func untouched(t *testing.T, r recipe.Recipe) {
	t.Helper()
	want := cake()
	for i := range want.Items {
		if r.Items[i] != want.Items[i] {
			t.Fatalf("the recipe passed in now has %+v", r.Items[i])
		}
	}
}

func TestFind(t *testing.T) {
	if got, ok := cake().Find("milk"); !ok || got.Amount != 50 {
		t.Fatalf("Find = %+v, %v", got, ok)
	}
	if _, ok := cake().Find("ghost"); ok {
		t.Fatal("found a ghost")
	}
}

func TestTotalByUnit(t *testing.T) {
	r := cake()
	r.Items = append(r.Items, recipe.Ingredient{Name: "sugar", Amount: 25, Unit: "g"})
	if got := r.Total("g"); !near(got, 225) {
		t.Fatalf("Total = %v", got)
	}
	if got := r.Total("kg"); got != 0 {
		t.Fatalf("Total = %v", got)
	}
}

func TestScaleMultiplies(t *testing.T) {
	got := cake().Scale(2)
	if !near(amount(t, got, "flour"), 400) || !near(amount(t, got, "milk"), 100) || !near(amount(t, got, "eggs"), 6) {
		t.Fatalf("Scale = %+v", got)
	}
	if got.Serves != 8 {
		t.Fatalf("Serves = %d", got.Serves)
	}
}

func TestScaleEachGoesUp(t *testing.T) {
	if got := amount(t, cake().Scale(1.5), "eggs"); !near(got, 5) {
		t.Fatalf("eggs = %v", got)
	}
	if got := amount(t, cake().Scale(0.5), "eggs"); !near(got, 2) {
		t.Fatalf("eggs = %v", got)
	}
}

func TestScaleKeepsPinch(t *testing.T) {
	if got := amount(t, cake().Scale(3), "salt"); !near(got, 1) {
		t.Fatalf("salt = %v", got)
	}
}

func TestScaleLeavesInput(t *testing.T) {
	r := cake()
	r.Scale(2)
	untouched(t, r)
}

func TestAmountForPlain(t *testing.T) {
	got, err := cake().AmountFor("flour", 6)
	if err != nil || !near(got, 300) {
		t.Fatalf("AmountFor = %v, %v", got, err)
	}
}

func TestAmountForRejects(t *testing.T) {
	if _, err := cake().AmountFor("ghost", 6); err == nil {
		t.Error("unknown ingredient")
	}
	if _, err := cake().AmountFor("flour", 0); err == nil {
		t.Error("no servings")
	}
}

func TestAmountForEachGoesUp(t *testing.T) {
	got, err := cake().AmountFor("eggs", 6)
	if err != nil || !near(got, 5) {
		t.Fatalf("AmountFor = %v, %v", got, err)
	}
}

func TestAmountForKeepsPinch(t *testing.T) {
	got, err := cake().AmountFor("salt", 12)
	if err != nil || !near(got, 1) {
		t.Fatalf("AmountFor = %v, %v", got, err)
	}
}

func TestForServingsScales(t *testing.T) {
	got, err := cake().ForServings(10)
	if err != nil || got.Serves != 10 || !near(amount(t, got, "flour"), 500) {
		t.Fatalf("ForServings = %+v, %v", got, err)
	}
	if _, err := cake().ForServings(0); err == nil {
		t.Fatal("no error for zero servings")
	}
}

func TestForServingsEachGoesUp(t *testing.T) {
	got, err := cake().ForServings(6)
	if err != nil || !near(amount(t, got, "eggs"), 5) {
		t.Fatalf("ForServings = %+v, %v", got, err)
	}
}

func TestForServingsLeavesInput(t *testing.T) {
	r := cake()
	if _, err := r.ForServings(8); err != nil {
		t.Fatal(err)
	}
	untouched(t, r)
}

func TestCombineAdds(t *testing.T) {
	other := recipe.Recipe{Name: "glaze", Serves: 9, Items: []recipe.Ingredient{
		{Name: "flour", Amount: 5, Unit: "g"},
		{Name: "flour", Amount: 1, Unit: "pinch"},
		{Name: "sugar", Amount: 30, Unit: "g"},
	}}
	got := recipe.Combine(cake(), other)
	if got.Name != "cake" || got.Serves != 4 || got.Count() != 6 {
		t.Fatalf("Combine = %+v", got)
	}
	if !near(got.Items[0].Amount, 205) || got.Items[4].Unit != "pinch" || got.Items[5].Name != "sugar" {
		t.Fatalf("Combine = %+v", got)
	}
}

func TestCombineLeavesInput(t *testing.T) {
	r := cake()
	recipe.Combine(r, recipe.Recipe{Items: []recipe.Ingredient{{Name: "flour", Amount: 5, Unit: "g"}}})
	untouched(t, r)
}
`},
		Solution: map[string]string{
			"recipe.go": `package recipe

import (
	"errors"
	"math"
)

// Ingredient is one line of a recipe. Unit is "g", "ml", "each" or "pinch".
type Ingredient struct {
	Name   string
	Amount float64
	Unit   string
}

// Recipe is a dish and the number of people it serves.
type Recipe struct {
	Name   string
	Serves int
	Items  []Ingredient
}

// Count reports how many ingredients the recipe has.
func (r Recipe) Count() int { return len(r.Items) }

// Find returns the named ingredient.
func (r Recipe) Find(name string) (Ingredient, bool) {
	for _, it := range r.Items {
		if it.Name == name {
			return it, true
		}
	}
	return Ingredient{}, false
}

// Total adds up the amounts given in one unit.
func (r Recipe) Total(unit string) float64 {
	sum := 0.0
	for _, it := range r.Items {
		if it.Unit == unit {
			sum += it.Amount
		}
	}
	return sum
}

func scaled(it Ingredient, factor float64) float64 {
	switch it.Unit {
	case "pinch":
		return it.Amount
	case "each":
		return math.Ceil(it.Amount * factor)
	}
	return it.Amount * factor
}

// Scale returns the recipe with every amount multiplied by factor.
func (r Recipe) Scale(factor float64) Recipe {
	out := r
	out.Serves = int(math.Round(float64(r.Serves) * factor))
	out.Items = append([]Ingredient(nil), r.Items...)
	for i := range out.Items {
		out.Items[i].Amount = scaled(out.Items[i], factor)
	}
	return out
}

// AmountFor returns how much of one ingredient that many servings need.
func (r Recipe) AmountFor(name string, servings int) (float64, error) {
	it, ok := r.Find(name)
	if !ok || servings < 1 {
		return 0, errors.New("no such ingredient or no servings")
	}
	return scaled(it, float64(servings)/float64(r.Serves)), nil
}

// ForServings returns the recipe scaled to serve n people.
func (r Recipe) ForServings(n int) (Recipe, error) {
	if n < 1 {
		return Recipe{}, errors.New("servings must be at least 1")
	}
	out := r.Scale(float64(n) / float64(r.Serves))
	out.Serves = n
	return out, nil
}

// Combine adds the ingredients of b to a's, under a's name and servings.
func Combine(a, b Recipe) Recipe {
	out := a
	out.Items = append([]Ingredient(nil), a.Items...)
next:
	for _, it := range b.Items {
		for i := range out.Items {
			if out.Items[i].Name == it.Name && out.Items[i].Unit == it.Unit {
				out.Items[i].Amount += it.Amount
				continue next
			}
		}
		out.Items = append(out.Items, it)
	}
	return out
}
`,
		},
		Turns: script(
			local("One rule from the kitchen: eggs, lemons and anything else counted as each can't be split, so an amount in that unit that we work out always goes up to the next whole number. Nothing computes amounts yet. Add `func (r Recipe) Find(name string) (Ingredient, bool)`."),
			cloud("The menu planner hands the same Recipe values to many callers at once, so nothing in this package may modify a recipe it's given, Items included. Add `func (r Recipe) Total(unit string) float64`: the sum of the amounts in that unit."),
			cloud("Read docs/kitchen.txt and tell me what in it affects how we work out amounts. No code changes."),
			local("Add `func (r Recipe) Scale(factor float64) Recipe`, which returns the recipe with every amount multiplied by factor, and Serves multiplied by it too."),
			cloud("Add `func (r Recipe) AmountFor(name string, servings int) (float64, error)`: how much of that one ingredient the recipe needs to serve that many people. An ingredient it doesn't have, or servings below 1, is an error."),
			local("Add `func (r Recipe) ForServings(n int) (Recipe, error)`: the recipe scaled to serve n people, with Serves exactly n. An n below 1 is an error."),
			cloud("Add `func Combine(a, b Recipe) Recipe`: a's name and servings, and a's items in order, where an item of b with the same Name and Unit as one of a's is added to it, and b's other items follow in b's order."),
		),
		Carries: []Carry{
			{
				What:        "amounts in unit each go up to the next whole number",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 4, 5},
				Tests:       []string{"TestScaleEachGoesUp", "TestAmountForEachGoesUp", "TestForServingsEachGoesUp"},
				Absent:      []string{"whole", "round", "split", "ceil", "goes up"},
			},
			{
				What:        "nothing may modify a recipe it is given",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{3, 5, 6},
				Tests:       []string{"TestScaleLeavesInput", "TestForServingsLeavesInput", "TestCombineLeavesInput"},
				Absent:      []string{"modify", "mutate", "planner", "unchanged", "original", "copy"},
			},
			{
				What:        "amounts in pinches never scale (docs/kitchen.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{3, 4},
				Source:      "docs/kitchen.txt",
				Tests:       []string{"TestScaleKeepsPinch", "TestAmountForKeepsPinch"},
				Absent:      []string{"pinch", "seasoning", "by feel", "kitchen"},
			},
		},
	}
}
