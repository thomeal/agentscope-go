package s2

func long2C03() Task {
	return Task{
		ID:     "s2-long2-c03-reaction-planner",
		Set:    "long",
		System: system("a reaction planning tool for a chemistry lab"),
		Files: map[string]string{
			"go.mod": goMod,
			"elements.go": `package chem

import "sort"

// atomicMass holds the standard atomic weights we use, in g/mol, abridged.
var atomicMass = map[string]float64{
	"H": 1.008, "He": 4.0026, "Li": 6.94, "Be": 9.0122, "B": 10.81,
	"C": 12.011, "N": 14.007, "O": 15.999, "F": 18.998, "Na": 22.990,
	"Mg": 24.305, "Al": 26.982, "Si": 28.085, "P": 30.974, "S": 32.06,
	"Cl": 35.45, "K": 39.098, "Ca": 40.078, "Cr": 51.996, "Mn": 54.938,
	"Fe": 55.845, "Co": 58.933, "Ni": 58.693, "Cu": 63.546, "Zn": 65.38,
	"Br": 79.904, "Ag": 107.87, "Sn": 118.71, "I": 126.90, "Ba": 137.33,
	"Pb": 207.2,
}

// AtomicMass returns the standard atomic weight of an element in g/mol, and
// whether we know the element.
func AtomicMass(symbol string) (float64, bool) {
	m, ok := atomicMass[symbol]
	return m, ok
}

// Elements returns the symbols of the elements we know, sorted.
func Elements() []string {
	out := make([]string, 0, len(atomicMass))
	for sym := range atomicMass {
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}
`,
			"formula.go": `package chem

import (
	"errors"
	"unicode"
)

// ParseFormula counts the atoms of each element in a simple formula such as
// H2SO4: element symbols, each followed by an optional count.
func ParseFormula(f string) (map[string]int, error) {
	counts := map[string]int{}
	rs := []rune(f)
	for i := 0; i < len(rs); {
		if !unicode.IsUpper(rs[i]) {
			return nil, errors.New("bad formula")
		}
		sym := string(rs[i])
		i++
		if i < len(rs) && unicode.IsLower(rs[i]) {
			sym += string(rs[i])
			i++
		}
		n := 0
		for i < len(rs) && unicode.IsDigit(rs[i]) {
			n = n*10 + int(rs[i]-'0')
			i++
		}
		counts[sym] += max(n, 1)
	}
	if len(counts) == 0 {
		return nil, errors.New("empty formula")
	}
	return counts, nil
}
`,
			"chem_test.go": `package chem

import "testing"

func TestAtomicMass(t *testing.T) {
	if m, ok := AtomicMass("O"); !ok || m != 15.999 {
		t.Errorf("AtomicMass(O) = %v, %v", m, ok)
	}
	if _, ok := AtomicMass("Xx"); ok {
		t.Error("Xx is not an element")
	}
	if got := Elements(); len(got) < 30 || got[0] != "Ag" {
		t.Errorf("Elements = %v", got)
	}
}

func TestParseFormula(t *testing.T) {
	got, err := ParseFormula("H2SO4")
	if err != nil || len(got) != 3 || got["H"] != 2 || got["S"] != 1 || got["O"] != 4 {
		t.Fatalf("ParseFormula = %v, %v", got, err)
	}
	for _, bad := range []string{"", "h2o", "2H"} {
		if _, err := ParseFormula(bad); err == nil {
			t.Errorf("ParseFormula(%q) succeeded", bad)
		}
	}
}
`,
			"docs/inventory.txt": `Chemical inventory export

The stores system exports one bottle per line, the fields separated by "|":

    code|name|formula|purity|grams

code is the bottle's barcode, name is free text, formula is the reagent's
formula, purity is the assay on the label as a percentage (99.5 means
99.5 %), and grams is what is left in the bottle. Lines that start with ";"
are comments.

Hydrates are written with an asterisk between the salt and its water of
crystallisation, with the number of waters straight after the asterisk:
copper sulfate pentahydrate is CuSO4*5H2O, one CuSO4 and five H2O. Any
formula may also contain parentheses, as in Ca(OH)2.

The purity matters whenever a reagent is weighed out: only that share of
what is on the balance is the reagent itself. The amount of the reagent in
moles is grams times purity/100, divided by the molar mass.

A code belongs to one bottle, not to one substance. Stores often hold
several bottles of the same reagent, and over the years they have been
entered under different formulas: ethanol appears as C2H5OH and as
CH3CH2OH, for instance. Treat formulas with the same atoms as the same
substance.

Bottles are checked against this export twice a year; tell stores about
any bottle that is missing from it rather than adding it yourself.
`,
			"docs/notebook.txt": `Electronic lab notebook: reaction plan entries

A plan entry is plain text, one item per line, and every line ends with a
newline. The first line is "reaction: " followed by the reaction exactly as
the chemist wrote it.

Then comes one line for each reagent that was weighed out, in the order in
which the reaction lists them:

    <formula>  <grams> g  <amount> mmol

with exactly two spaces between the three parts. The amount is the
reagent's own amount, in millimoles.

The last line names the limiting reagent and the theoretical yield, in
grams, of the first product of the reaction:

    limiting: <formula>; yield: <grams> g of <formula>

For example, a plan for making water from hydrogen and oxygen has these
lines, with the numbers filled in:

    reaction: 2H2 + O2 -> 2H2O
    H2  <grams> g  <amount> mmol
    O2  <grams> g  <amount> mmol
    limiting: H2; yield: <grams> g of H2O

The notebook rejects entries that do not follow this layout.
`,
		},
		Hidden: map[string]string{hiddenName: `package chem_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	chem "fixture"
)

func mm(t *testing.T, f string) float64 {
	t.Helper()
	m, err := chem.MolarMass(f)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func counts(t *testing.T, f string) map[string]int {
	t.Helper()
	c, err := chem.ParseFormula(f)
	if err != nil {
		t.Fatalf("ParseFormula(%q): %v", f, err)
	}
	out := map[string]int{}
	for el, n := range c {
		out[el] = n
	}
	return out
}

func reaction(t *testing.T, s string) chem.Reaction {
	t.Helper()
	r, err := chem.ParseReaction(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func water() chem.Reaction {
	return chem.Reaction{Reactants: []chem.Term{{1, "CaO"}, {1, "H2O"}}, Products: []chem.Term{{1, "Ca(OH)2"}}}
}

func lines(s string) []string { return strings.Split(strings.TrimSuffix(s, "\n"), "\n") }

func TestMolarMass(t *testing.T) {
	if got := mm(t, "H2O"); !near(got, 18.015, 1e-9) {
		t.Errorf("MolarMass(H2O) = %v", got)
	}
	if got := mm(t, "H2SO4"); !near(got, 98.072, 1e-9) {
		t.Errorf("MolarMass(H2SO4) = %v", got)
	}
}

func TestParseUnknownElement(t *testing.T) {
	if _, err := chem.ParseFormula("Xx2O"); !errors.Is(err, chem.ErrParse) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseParentheses(t *testing.T) {
	cases := map[string]map[string]int{
		"Ca(OH)2":     {"Ca": 1, "O": 2, "H": 2},
		"Mg3(PO4)2":   {"Mg": 3, "P": 2, "O": 8},
		"K4(Fe(CN)6)": {"K": 4, "Fe": 1, "C": 6, "N": 6},
	}
	for f, want := range cases {
		if got := counts(t, f); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseFormula(%s) = %v", f, got)
		}
	}
}

func TestParseUnbalancedParentheses(t *testing.T) {
	for _, f := range []string{"Ca(OH2", "CaOH)2", "Mg3((PO4)2"} {
		if _, err := chem.ParseFormula(f); !errors.Is(err, chem.ErrParse) {
			t.Errorf("ParseFormula(%s): %v", f, err)
		}
	}
}

func TestParseHydrate(t *testing.T) {
	want := map[string]int{"Cu": 1, "S": 1, "O": 9, "H": 10}
	if got := counts(t, "CuSO4*5H2O"); !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseFormula = %v", got)
	}
}

func TestMolarMassHydrate(t *testing.T) {
	if got := mm(t, "CuSO4*5H2O"); !near(got, 249.677, 1e-6) {
		t.Fatalf("MolarMass = %v", got)
	}
}

func TestMolesAndGrams(t *testing.T) {
	n, err := chem.Moles("H2O", 36.03)
	if err != nil || !near(n, 2, 1e-9) {
		t.Errorf("Moles = %v, %v", n, err)
	}
	g, err := chem.Grams("O2", 0.5)
	if err != nil || !near(g, 15.999, 1e-9) {
		t.Errorf("Grams = %v, %v", g, err)
	}
}

func TestPercentYield(t *testing.T) {
	if chem.PercentYield(4, 5) != 80 || chem.PercentYield(1, 0) != 0 {
		t.Fatal("PercentYield")
	}
}

func TestMolarity(t *testing.T) {
	got, err := chem.Molarity("NaCl", 5.844, 100)
	if err != nil || !near(got, 1, 1e-9) {
		t.Fatalf("Molarity = %v, %v", got, err)
	}
}

func TestDilute(t *testing.T) {
	if got := chem.Dilute(2, 50, 0.5); !near(got, 200, 1e-9) {
		t.Fatalf("Dilute = %v", got)
	}
}

func TestParseReaction(t *testing.T) {
	got := reaction(t, "2H2 + O2 -> 2H2O")
	want := chem.Reaction{Reactants: []chem.Term{{2, "H2"}, {1, "O2"}}, Products: []chem.Term{{2, "H2O"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseReaction = %+v", got)
	}
}

func TestParseReactionNoArrow(t *testing.T) {
	if _, err := chem.ParseReaction("2H2 + O2 = 2H2O"); !errors.Is(err, chem.ErrParse) {
		t.Fatalf("err = %v", err)
	}
}

func TestBalanced(t *testing.T) {
	if ok, err := reaction(t, "2H2 + O2 -> 2H2O").Balanced(); err != nil || !ok {
		t.Errorf("balanced: %v, %v", ok, err)
	}
	if ok, err := reaction(t, "H2 + O2 -> H2O").Balanced(); err != nil || ok {
		t.Errorf("unbalanced: %v, %v", ok, err)
	}
}

func TestFormulaStringCarbonFirst(t *testing.T) {
	if got := (chem.Counts{"Br": 1, "C": 1, "H": 3}).String(); got != "CH3Br" {
		t.Errorf("String = %q", got)
	}
	if got := (chem.Counts{"O": 1, "C": 2, "H": 6}).String(); got != "C2H6O" {
		t.Errorf("String = %q", got)
	}
}

func TestFormulaStringWithoutCarbon(t *testing.T) {
	if got := (chem.Counts{"H": 1, "Cl": 1}).String(); got != "ClH" {
		t.Errorf("String = %q", got)
	}
	if got := (chem.Counts{"S": 1, "H": 2, "O": 4}).String(); got != "H2O4S" {
		t.Errorf("String = %q", got)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"CH3Br": "CH3Br", "C2H5OH": "C2H6O", "HCl": "ClH", "Ca(OH)2": "CaH2O2"} {
		if got, err := chem.Normalize(in); err != nil || got != want {
			t.Errorf("Normalize(%s) = %q, %v", in, got, err)
		}
	}
}

func TestReagentMoles(t *testing.T) {
	got, err := chem.Reagent{Formula: "NaCl", Grams: 11.688, Purity: 50}.Moles()
	if err != nil || !near(got, 0.1, 1e-9) {
		t.Fatalf("Moles = %v, %v", got, err)
	}
}

func TestComposition(t *testing.T) {
	got, err := chem.Composition("H2O")
	if err != nil || len(got) != 2 || !near(got["H"], 11.1907, 1e-3) || !near(got["O"], 88.8093, 1e-3) {
		t.Fatalf("Composition = %v, %v", got, err)
	}
}

func TestParseInventory(t *testing.T) {
	text := "; stores export\nB1001|sodium chloride|NaCl|99.5|480.2\n\nB1002|calcium hydroxide|Ca(OH)2|96|120\n"
	got, err := chem.ParseInventory(text)
	want := []chem.Bottle{
		{Code: "B1001", Name: "sodium chloride", Formula: "NaCl", Purity: 99.5, Grams: 480.2},
		{Code: "B1002", Name: "calcium hydroxide", Formula: "Ca(OH)2", Purity: 96, Grams: 120},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseInventory = %+v, %v", got, err)
	}
}

func TestParseInventoryBadLine(t *testing.T) {
	if _, err := chem.ParseInventory("B1003|salt|NaCl|abc|10\n"); !errors.Is(err, chem.ErrParse) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseInventoryPurityOver100(t *testing.T) {
	if _, err := chem.ParseInventory("B1004|salt|NaCl|100.5|10\n"); !errors.Is(err, chem.ErrParse) {
		t.Fatalf("err = %v", err)
	}
}

func TestLimitingReagent(t *testing.T) {
	got, err := chem.LimitingReagent(reaction(t, "2H2 + O2 -> 2H2O"), []chem.Reagent{{"O2", 8, 100}, {"H2", 0.5, 100}})
	if err != nil || got != "H2" {
		t.Fatalf("LimitingReagent = %q, %v", got, err)
	}
}

func TestLimitingReagentTie(t *testing.T) {
	reagents := []chem.Reagent{{"O2", mm(t, "O2"), 100}, {"H2", 2 * mm(t, "H2"), 100}}
	got, err := chem.LimitingReagent(reaction(t, "2H2 + O2 -> 2H2O"), reagents)
	if err != nil || got != "H2" {
		t.Fatalf("LimitingReagent = %q, %v", got, err)
	}
}

func TestLimitingReagentPurity(t *testing.T) {
	got, err := chem.LimitingReagent(reaction(t, "2H2 + O2 -> 2H2O"), []chem.Reagent{{"H2", 0.5, 100}, {"O2", 4.5, 50}})
	if err != nil || got != "O2" {
		t.Fatalf("LimitingReagent = %q, %v", got, err)
	}
}

func TestLimitingReagentWater(t *testing.T) {
	got, err := chem.LimitingReagent(water(), []chem.Reagent{{"CaO", 5.6, 100}})
	if err != nil || got != "CaO" {
		t.Fatalf("LimitingReagent = %q, %v", got, err)
	}
}

func TestLimitingReagentMissing(t *testing.T) {
	if _, err := chem.LimitingReagent(reaction(t, "2H2 + O2 -> 2H2O"), []chem.Reagent{{"H2", 1, 100}}); !errors.Is(err, chem.ErrMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheoreticalYield(t *testing.T) {
	got, err := chem.TheoreticalYield(reaction(t, "2H2 + O2 -> 2H2O"), []chem.Reagent{{"H2", 0.5, 100}, {"O2", 8, 100}}, "H2O")
	if want := 0.5 / mm(t, "H2") * mm(t, "H2O"); err != nil || !near(got, want, 1e-9) {
		t.Fatalf("TheoreticalYield = %v, %v; want %v", got, err, want)
	}
}

func TestTheoreticalYieldWater(t *testing.T) {
	got, err := chem.TheoreticalYield(water(), []chem.Reagent{{"CaO", 5.6, 100}}, "Ca(OH)2")
	if want := 5.6 / mm(t, "CaO") * mm(t, "Ca(OH)2"); err != nil || !near(got, want, 1e-9) {
		t.Fatalf("TheoreticalYield = %v, %v; want %v", got, err, want)
	}
}

func TestTheoreticalYieldNotAProduct(t *testing.T) {
	if _, err := chem.TheoreticalYield(reaction(t, "2H2 + O2 -> 2H2O"), []chem.Reagent{{"H2", 1, 100}, {"O2", 8, 100}}, "H2O2"); err == nil {
		t.Fatal("no error for a substance that is not a product")
	}
}

func notebook(t *testing.T, written string, reagents []chem.Reagent) string {
	t.Helper()
	got, err := chem.NotebookEntry(written, reagents)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestNotebookLayout(t *testing.T) {
	got := notebook(t, "2H2 + O2 -> 2H2O", []chem.Reagent{{"H2", 0.5, 100}, {"O2", 8, 100}})
	l := lines(got)
	if len(l) != 5 || l[0] != "reaction: 2H2 + O2 -> 2H2O" || !strings.HasSuffix(got, "\n") {
		t.Fatalf("NotebookEntry = %q", got)
	}
	for i, f := range []string{"H2", "O2"} {
		parts := strings.Split(l[2+i], "  ")
		if len(parts) != 3 || parts[0] != f || !strings.HasSuffix(parts[1], " g") || !strings.HasSuffix(parts[2], " mmol") {
			t.Errorf("reagent line %q", l[2+i])
		}
	}
	if !strings.HasPrefix(l[4], "limiting: H2; yield: ") || !strings.HasSuffix(l[4], " g of H2O") {
		t.Errorf("last line %q", l[4])
	}
}

func TestNotebookAmounts(t *testing.T) {
	got := notebook(t, "2H2 + O2 -> 2H2O", []chem.Reagent{{"H2", 0.5, 100}, {"O2", 8, 100}})
	if !strings.Contains(got, " 248.02 mmol\n") || !strings.Contains(got, " 250.02 mmol\n") {
		t.Fatalf("NotebookEntry = %q", got)
	}
}

func TestNotebookMasses(t *testing.T) {
	got := notebook(t, "2H2 + O2 -> 2H2O", []chem.Reagent{{"H2", 0.5, 100}, {"O2", 8, 100}})
	if !strings.Contains(got, " 0.5000 g") || !strings.Contains(got, " 8.0000 g") || !strings.Contains(got, " 4.4680 g") {
		t.Fatalf("NotebookEntry = %q", got)
	}
}

func TestNotebookPurity(t *testing.T) {
	got := notebook(t, "2H2 + O2 -> 2H2O", []chem.Reagent{{"H2", 0.5, 50}, {"O2", 8, 100}})
	if !strings.Contains(got, " 2.234") {
		t.Fatalf("NotebookEntry = %q", got)
	}
}

func TestNotebookSkipsWater(t *testing.T) {
	got, err := chem.NotebookEntry("CaO + H2O -> Ca(OH)2", []chem.Reagent{{"CaO", 5.6, 100}})
	if l := lines(got); err != nil || len(l) != 4 || !strings.HasPrefix(l[2], "CaO") {
		t.Fatalf("NotebookEntry = %q, %v", got, err)
	}
}

func TestNotebookBalancedLine(t *testing.T) {
	if l := lines(notebook(t, "2H2 + O2 -> 2H2O", []chem.Reagent{{"H2", 0.5, 100}, {"O2", 8, 100}})); l[1] != "balanced: yes" {
		t.Errorf("line %q", l[1])
	}
	if l := lines(notebook(t, "H2 + O2 -> H2O", []chem.Reagent{{"H2", 0.5, 100}, {"O2", 8, 100}})); l[1] != "balanced: no" {
		t.Errorf("line %q", l[1])
	}
}

func TestAtomEconomy(t *testing.T) {
	got, err := chem.AtomEconomy(reaction(t, "CaCO3 -> CaO + CO2"), "CaO")
	if want := 100 * mm(t, "CaO") / mm(t, "CaCO3"); err != nil || !near(got, want, 1e-9) {
		t.Fatalf("AtomEconomy = %v, %v", got, err)
	}
	if _, err := chem.AtomEconomy(reaction(t, "CaCO3 -> CaO + CO2"), "O2"); err == nil {
		t.Fatal("no error for a substance that is not a product")
	}
}

func TestTake(t *testing.T) {
	b := chem.Bottle{Code: "B1", Name: "salt", Formula: "NaCl", Purity: 99.5, Grams: 10}
	got, err := b.Take(4)
	if err != nil || got != (chem.Reagent{Formula: "NaCl", Grams: 4, Purity: 99.5}) {
		t.Errorf("Take = %+v, %v", got, err)
	}
	if _, err := b.Take(12); !errors.Is(err, chem.ErrNotEnough) {
		t.Errorf("err = %v", err)
	}
}

func TestRestock(t *testing.T) {
	bottles := []chem.Bottle{{Code: "C3", Grams: 5}, {Code: "A1", Grams: 50}, {Code: "B2", Grams: 2}}
	if got := chem.Restock(bottles, 10); !reflect.DeepEqual(got, []string{"B2", "C3"}) {
		t.Fatalf("Restock = %v", got)
	}
}

func TestReactionString(t *testing.T) {
	if got := water().String(); got != "CaO + H2O -> Ca(OH)2" {
		t.Errorf("String = %q", got)
	}
	r := chem.Reaction{Reactants: []chem.Term{{2, "H2"}, {1, "O2"}}, Products: []chem.Term{{2, "H2O"}}}
	if got := r.String(); got != "2H2 + O2 -> 2H2O" {
		t.Errorf("String = %q", got)
	}
}

func TestWeighOut(t *testing.T) {
	got, err := chem.WeighOut(chem.Bottle{Code: "B9", Formula: "NaCl", Purity: 50, Grams: 500}, 1, 100)
	if err != nil || !near(got, 11.688, 1e-9) {
		t.Fatalf("WeighOut = %v, %v", got, err)
	}
}

func TestSerialDilution(t *testing.T) {
	got := chem.SerialDilution(1, 10, 3)
	if len(got) != 3 || !near(got[0], 0.1, 1e-12) || !near(got[2], 0.001, 1e-12) {
		t.Fatalf("SerialDilution = %v", got)
	}
}

func TestFindByFormula(t *testing.T) {
	bottles := []chem.Bottle{{Code: "E1", Formula: "C2H5OH"}, {Code: "S1", Formula: "NaCl"}, {Code: "E2", Formula: "CH3CH2OH"}}
	got, err := chem.FindByFormula(bottles, "C2H6O")
	if err != nil || len(got) != 2 || got[0].Code != "E1" || got[1].Code != "E2" {
		t.Fatalf("FindByFormula = %+v, %v", got, err)
	}
}

func TestStock(t *testing.T) {
	bottles := []chem.Bottle{{Code: "A", Formula: "C2H5Br", Grams: 10}, {Code: "B", Formula: "CH3CH2Br", Grams: 5}, {Code: "C", Formula: "HCl", Grams: 20}}
	got, err := chem.Stock(bottles)
	if err != nil || !reflect.DeepEqual(got, map[string]float64{"C2H5Br": 15, "ClH": 20}) {
		t.Fatalf("Stock = %v, %v", got, err)
	}
}
`},
		Solution: map[string]string{
			"formula.go": `package chem

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

// ErrParse is returned, possibly wrapped, for any input we cannot parse.
var ErrParse = errors.New("cannot parse")

// Counts is how many atoms of each element a formula has.
type Counts map[string]int

// ParseFormula counts the atoms of each element in a formula such as H2SO4,
// Ca(OH)2 or the hydrate CuSO4*5H2O, whose waters follow the *.
func ParseFormula(f string) (Counts, error) {
	counts := Counts{}
	for i, part := range strings.Split(f, "*") {
		p := &parser{s: part}
		mult := 1
		if i > 0 {
			mult = p.count()
		}
		group, err := p.group()
		if err != nil {
			return nil, err
		}
		if p.i < len(p.s) || len(group) == 0 {
			return nil, fmt.Errorf("%w: formula %q", ErrParse, f)
		}
		for el, n := range group {
			counts[el] += mult * n
		}
	}
	return counts, nil
}

// MolarMass returns the molar mass of a formula in g/mol.
func MolarMass(formula string) (float64, error) {
	counts, err := ParseFormula(formula)
	if err != nil {
		return 0, err
	}
	m := 0.0
	for _, el := range slices.Sorted(maps.Keys(counts)) {
		m += atomicMass[el] * float64(counts[el])
	}
	return m, nil
}

// Composition returns each element's share of the molar mass, in percent.
func Composition(formula string) (map[string]float64, error) {
	counts, err := ParseFormula(formula)
	if err != nil {
		return nil, err
	}
	total, _ := MolarMass(formula)
	out := map[string]float64{}
	for el, n := range counts {
		out[el] = 100 * atomicMass[el] * float64(n) / total
	}
	return out, nil
}

// String writes the counts as a formula in Hill order: C first, then H,
// then the other elements alphabetically; without C, all of them
// alphabetically. A count of 1 is left out.
func (c Counts) String() string {
	var order []string
	for el := range c {
		if c["C"] == 0 || (el != "C" && el != "H") {
			order = append(order, el)
		}
	}
	sort.Strings(order)
	if c["C"] > 0 {
		order = append([]string{"C", "H"}, order...)
	}
	var b strings.Builder
	for _, el := range order {
		if n := c[el]; n == 1 {
			b.WriteString(el)
		} else if n > 1 {
			fmt.Fprintf(&b, "%s%d", el, n)
		}
	}
	return b.String()
}

// Normalize parses a formula and writes it out again with String.
func Normalize(formula string) (string, error) {
	counts, err := ParseFormula(formula)
	if err != nil {
		return "", err
	}
	return counts.String(), nil
}
`,
			"parser.go": `package chem

import "fmt"

// parser reads one part of a formula, without any hydrate waters.
type parser struct {
	s string
	i int
}

// group reads elements and parenthesised groups up to a ")" or the end.
func (p *parser) group() (Counts, error) {
	counts := Counts{}
	for p.i < len(p.s) && p.s[p.i] != ')' {
		part, err := p.unit()
		if err != nil {
			return nil, err
		}
		n := p.count()
		for el, k := range part {
			counts[el] += k * n
		}
	}
	return counts, nil
}

// unit reads one element or one parenthesised group.
func (p *parser) unit() (Counts, error) {
	if p.s[p.i] != '(' {
		el, err := p.element()
		return Counts{el: 1}, err
	}
	p.i++
	inner, err := p.group()
	if err != nil {
		return nil, err
	}
	if p.i == len(p.s) {
		return nil, fmt.Errorf("%w: unclosed parenthesis in %q", ErrParse, p.s)
	}
	p.i++
	return inner, nil
}

// element reads one element symbol we know: an upper-case letter and an
// optional lower-case one.
func (p *parser) element() (string, error) {
	start := p.i
	if c := p.s[p.i]; c < 'A' || c > 'Z' {
		return "", fmt.Errorf("%w: unexpected %q in %q", ErrParse, c, p.s)
	}
	p.i++
	if p.i < len(p.s) && p.s[p.i] >= 'a' && p.s[p.i] <= 'z' {
		p.i++
	}
	sym := p.s[start:p.i]
	if _, ok := atomicMass[sym]; !ok {
		return "", fmt.Errorf("%w: unknown element %s", ErrParse, sym)
	}
	return sym, nil
}

// count reads an optional count, 1 when there is none.
func (p *parser) count() int {
	n, start := 0, p.i
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		n = n*10 + int(p.s[p.i]-'0')
		p.i++
	}
	if p.i == start {
		return 1
	}
	return n
}
`,
			"reaction.go": `package chem

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
)

// Term is one substance of a reaction with its coefficient.
type Term struct {
	Coef    int
	Formula string
}

// Reaction is a chemical reaction, its terms in the order written.
type Reaction struct {
	Reactants, Products []Term
}

// ParseReaction reads a reaction written like "2H2 + O2 -> 2H2O".
func ParseReaction(s string) (Reaction, error) {
	left, right, ok := strings.Cut(s, "->")
	if !ok {
		return Reaction{}, fmt.Errorf("%w: no -> in %q", ErrParse, s)
	}
	reactants, err := terms(left)
	if err != nil {
		return Reaction{}, err
	}
	products, err := terms(right)
	if err != nil {
		return Reaction{}, err
	}
	return Reaction{Reactants: reactants, Products: products}, nil
}

// terms reads one side of a reaction: terms separated by +, each an
// optional coefficient and a formula.
func terms(side string) ([]Term, error) {
	var out []Term
	for _, t := range strings.Split(side, "+") {
		t = strings.TrimSpace(t)
		digits := len(t) - len(strings.TrimLeft(t, "0123456789"))
		coef := 1
		if digits > 0 {
			coef, _ = strconv.Atoi(t[:digits])
		}
		if _, err := ParseFormula(t[digits:]); err != nil {
			return nil, err
		}
		out = append(out, Term{Coef: coef, Formula: t[digits:]})
	}
	return out, nil
}

// atoms counts the atoms on one side of a reaction.
func atoms(side []Term) (Counts, error) {
	out := Counts{}
	for _, t := range side {
		c, err := ParseFormula(t.Formula)
		if err != nil {
			return nil, err
		}
		for el, n := range c {
			out[el] += t.Coef * n
		}
	}
	return out, nil
}

// Balanced reports whether every element has as many atoms on both sides.
func (r Reaction) Balanced() (bool, error) {
	left, err := atoms(r.Reactants)
	if err != nil {
		return false, err
	}
	right, err := atoms(r.Products)
	if err != nil {
		return false, err
	}
	return maps.Equal(left, right), nil
}

// AtomEconomy returns the mass of a product times its coefficient as a
// percentage of the mass of all the reactants times their coefficients.
func AtomEconomy(r Reaction, product string) (float64, error) {
	in := 0.0
	for _, t := range r.Reactants {
		m, err := MolarMass(t.Formula)
		if err != nil {
			return 0, err
		}
		in += m * float64(t.Coef)
	}
	for _, t := range r.Products {
		if t.Formula == product {
			m, err := MolarMass(product)
			return 100 * m * float64(t.Coef) / in, err
		}
	}
	return 0, fmt.Errorf("%s is not a product of the reaction", product)
}

// String writes the reaction out, leaving out coefficients of 1.
func (r Reaction) String() string {
	side := func(terms []Term) string {
		parts := make([]string, len(terms))
		for i, t := range terms {
			parts[i] = t.Formula
			if t.Coef != 1 {
				parts[i] = strconv.Itoa(t.Coef) + t.Formula
			}
		}
		return strings.Join(parts, " + ")
	}
	return side(r.Reactants) + " -> " + side(r.Products)
}
`,
			"amounts.go": `package chem

import (
	"errors"
	"fmt"
)

// ErrMissing is returned when a reactant that counts has no reagent.
var ErrMissing = errors.New("missing reagent")

// water is never weighed: it is the solvent of all our reactions and always
// in excess, so it never limits one.
const water = "H2O"

// Moles returns how many moles grams of a formula are.
func Moles(formula string, grams float64) (float64, error) {
	m, err := MolarMass(formula)
	if err != nil {
		return 0, err
	}
	return grams / m, nil
}

// Grams returns the mass of moles of a formula.
func Grams(formula string, moles float64) (float64, error) {
	m, err := MolarMass(formula)
	if err != nil {
		return 0, err
	}
	return moles * m, nil
}

// PercentYield returns actual as a percentage of theoretical, or 0 when
// theoretical is 0.
func PercentYield(actual, theoretical float64) float64 {
	if theoretical == 0 {
		return 0
	}
	return 100 * actual / theoretical
}

// Reagent is a reagent weighed out for a reaction. Purity is the percentage
// on its label.
type Reagent struct {
	Formula       string
	Grams, Purity float64
}

// Moles returns the moles of the reagent itself in what was weighed out.
func (r Reagent) Moles() (float64, error) {
	n, err := Moles(r.Formula, r.Grams)
	return n * r.Purity / 100, err
}

// limit returns the limiting reactant and its moles over its coefficient.
func limit(r Reaction, reagents []Reagent) (string, float64, error) {
	best, ratio := "", 0.0
	for _, t := range r.Reactants {
		if t.Formula == water {
			continue
		}
		g, ok := find(reagents, t.Formula)
		if !ok {
			return "", 0, fmt.Errorf("%w: %s", ErrMissing, t.Formula)
		}
		n, err := g.Moles()
		if err != nil {
			return "", 0, err
		}
		if q := n / float64(t.Coef); best == "" || q < ratio {
			best, ratio = t.Formula, q
		}
	}
	return best, ratio, nil
}

// find returns the reagent with a formula.
func find(reagents []Reagent, formula string) (Reagent, bool) {
	for _, g := range reagents {
		if g.Formula == formula {
			return g, true
		}
	}
	return Reagent{}, false
}

// LimitingReagent returns the formula of the reactant that runs out first.
func LimitingReagent(r Reaction, reagents []Reagent) (string, error) {
	best, _, err := limit(r, reagents)
	return best, err
}

// TheoreticalYield returns the grams of a product the limiting reagent allows.
func TheoreticalYield(r Reaction, reagents []Reagent, product string) (float64, error) {
	_, ratio, err := limit(r, reagents)
	if err != nil {
		return 0, err
	}
	for _, t := range r.Products {
		if t.Formula == product {
			return Grams(product, ratio*float64(t.Coef))
		}
	}
	return 0, fmt.Errorf("%s is not a product of the reaction", product)
}
`,
			"solution.go": `package chem

// Molarity returns the concentration in mol/L of grams of a formula made up
// to mL millilitres.
func Molarity(formula string, grams, mL float64) (float64, error) {
	n, err := Moles(formula, grams)
	if err != nil {
		return 0, err
	}
	return n / (mL / 1000), nil
}

// Dilute returns the volume that v1 of a solution at concentration c1 must
// be made up to for concentration c2, in the units of v1.
func Dilute(c1, v1, c2 float64) float64 { return c1 * v1 / c2 }

// WeighOut returns the grams to weigh out of a bottle to make mL millilitres
// of its reagent at a molarity, allowing for the bottle's purity.
func WeighOut(b Bottle, molarity, mL float64) (float64, error) {
	g, err := Grams(b.Formula, molarity*mL/1000)
	if err != nil {
		return 0, err
	}
	return g * 100 / b.Purity, nil
}

// SerialDilution returns the concentration after each of steps successive
// dilutions by factor, starting from c0, which is not in the list.
func SerialDilution(c0, factor float64, steps int) []float64 {
	out := make([]float64, steps)
	for i := range out {
		c0 /= factor
		out[i] = c0
	}
	return out
}
`,
			"inventory.go": `package chem

import (
	"errors"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
)

// ErrNotEnough is returned when a bottle holds less than is asked for.
var ErrNotEnough = errors.New("not enough in the bottle")

// Bottle is one reagent bottle in the stores system.
type Bottle struct {
	Code, Name, Formula string
	Purity, Grams       float64
}

// ParseInventory reads the stores system's export into bottles, in order.
func ParseInventory(text string) ([]Bottle, error) {
	var out []Bottle
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		b, err := bottle(line)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// bottle reads one line: code|name|formula|purity|grams.
func bottle(line string) (Bottle, error) {
	f := strings.Split(line, "|")
	if len(f) != 5 {
		return Bottle{}, fmt.Errorf("%w: inventory line %q", ErrParse, line)
	}
	purity, err1 := strconv.ParseFloat(f[3], 64)
	grams, err2 := strconv.ParseFloat(f[4], 64)
	if err1 != nil || err2 != nil || purity > 100 {
		return Bottle{}, fmt.Errorf("%w: inventory line %q", ErrParse, line)
	}
	return Bottle{Code: f[0], Name: f[1], Formula: f[2], Purity: purity, Grams: grams}, nil
}

// Take returns a reagent of grams from the bottle.
func (b Bottle) Take(grams float64) (Reagent, error) {
	if grams > b.Grams {
		return Reagent{}, fmt.Errorf("%w: %s holds %v g", ErrNotEnough, b.Code, b.Grams)
	}
	return Reagent{Formula: b.Formula, Grams: grams, Purity: b.Purity}, nil
}

// Restock returns the codes of the bottles holding less than minGrams, sorted.
func Restock(bottles []Bottle, minGrams float64) []string {
	var out []string
	for _, b := range bottles {
		if b.Grams < minGrams {
			out = append(out, b.Code)
		}
	}
	sort.Strings(out)
	return out
}

// FindByFormula returns the bottles holding the substance with a formula,
// however either formula is written, in order.
func FindByFormula(bottles []Bottle, formula string) ([]Bottle, error) {
	want, err := ParseFormula(formula)
	if err != nil {
		return nil, err
	}
	var out []Bottle
	for _, b := range bottles {
		have, err := ParseFormula(b.Formula)
		if err != nil {
			return nil, err
		}
		if maps.Equal(have, want) {
			out = append(out, b)
		}
	}
	return out, nil
}

// Stock returns the grams held of each substance, keyed by its formula as
// Normalize writes it.
func Stock(bottles []Bottle) (map[string]float64, error) {
	out := map[string]float64{}
	for _, b := range bottles {
		key, err := Normalize(b.Formula)
		if err != nil {
			return nil, err
		}
		out[key] += b.Grams
	}
	return out, nil
}
`,
			"notebook.go": `package chem

import (
	"fmt"
	"strconv"
	"strings"
)

// mass writes grams for the notebook.
func mass(g float64) string { return strconv.FormatFloat(g, 'f', 4, 64) }

// amount writes an amount in mmol for the notebook.
func amount(mmol float64) string { return strconv.FormatFloat(mmol, 'f', 2, 64) }

// NotebookEntry writes the plan entry for a reaction as the chemist wrote
// it, with the reagents weighed out for it.
func NotebookEntry(written string, reagents []Reagent) (string, error) {
	r, err := ParseReaction(written)
	if err != nil {
		return "", err
	}
	balanced, err := r.Balanced()
	if err != nil {
		return "", err
	}
	limiting, err := LimitingReagent(r, reagents)
	if err != nil {
		return "", err
	}
	product := r.Products[0].Formula
	grams, err := TheoreticalYield(r, reagents, product)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "reaction: %s\n", written)
	fmt.Fprintf(&b, "balanced: %s\n", map[bool]string{true: "yes", false: "no"}[balanced])
	if err := reagentLines(&b, r, reagents); err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "limiting: %s; yield: %s g of %s\n", limiting, mass(grams), product)
	return b.String(), nil
}

// reagentLines writes a line for each weighed reagent, in reaction order.
func reagentLines(b *strings.Builder, r Reaction, reagents []Reagent) error {
	for _, t := range r.Reactants {
		g, ok := find(reagents, t.Formula)
		if !ok {
			continue
		}
		n, err := g.Moles()
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "%s  %s g  %s mmol\n", t.Formula, mass(g.Grams), amount(1000*n))
	}
	return nil
}
`,
		},
		Turns: script(
			cloud("A convention first: when we write a formula out, we use Hill order. Carbon first, then hydrogen, then every other element alphabetically; a formula without carbon is written fully alphabetically, hydrogen included. A count of 1 is left out. Nothing writes formulas yet. Add `func MolarMass(formula string) (float64, error)` to formula.go: the formula's molar mass in g/mol, from ParseFormula and the atomic masses."),
			cloud("Add `var ErrParse = errors.New(\"cannot parse\")` to formula.go and make ParseFormula return it, wrapped with detail if you like, for everything it rejects; make it reject element symbols that are not in our table too. From now on this is the error for any input we cannot parse."),
			local("Read docs/inventory.txt and tell me briefly what it says about how reagents are recorded. No code changes."),
			local("Start amounts.go with `func Moles(formula string, grams float64) (float64, error)`, how many moles that many grams of the formula are, and `func Grams(formula string, moles float64) (float64, error)`, the other way round. Both use MolarMass."),
			local("Background for finding the limiting reagent later: we never weigh water. It is the solvent in all our reactions and always in excess, so it is left out when working out which reagent runs out first. Nothing to code for that yet. Add `func PercentYield(actual, theoretical float64) float64` to amounts.go: actual as a percentage of theoretical, or 0 when theoretical is 0."),
			cloud("Read docs/notebook.txt; we will write those entries later. For now add `type Reagent struct { Formula string; Grams, Purity float64 }` to amounts.go, for a reagent weighed out for a reaction, Purity being the percentage on its label."),
			cloud("For anything we write into the notebook: masses in grams with 3 decimals, which is what the balance shows, and amounts with 2 decimals. Nothing writes to it yet. Start solution.go with `func Molarity(formula string, grams, mL float64) (float64, error)`: the concentration in mol/L of that many grams made up to mL millilitres."),
			local("Add `func Dilute(c1, v1, c2 float64) float64` to solution.go: the volume that v1 of a solution at concentration c1 must be made up to for concentration c2, in the units of v1 (c1·v1 = c2·v2)."),
			cloud("ParseFormula should handle parentheses followed by an optional count, nested ones too, as in Ca(OH)2, Mg3(PO4)2 or K4(Fe(CN)6). Unbalanced parentheses are rejected."),
			local("Add `type Term struct { Coef int; Formula string }`, `type Reaction struct { Reactants, Products []Term }` and `func ParseReaction(s string) (Reaction, error)` in a new reaction.go, for reactions written like \"2H2 + O2 -> 2H2O\": sides split at ->, terms at +, a coefficient of 1 when none is written, and every formula has to parse. A reaction without -> is rejected."),
			cloud("The lab has a new balance that reads to 0.1 mg, so masses written into the notebook get 4 decimals from now on; nothing else about it changes. Add `func (r Reaction) Balanced() (bool, error)` to reaction.go: whether every element has as many atoms on both sides."),
			local("Add `func FormatFormula(counts map[string]int) string` to formula.go: the counts written out as a formula, the way we write formulas."),
			cloud("ParseFormula should also read hydrates, written the way our inventory writes them, with the water counted."),
			cloud("Add `func (r Reagent) Moles() (float64, error)` to amounts.go: the moles of the reagent itself in what was weighed out."),
			local("Add `func Composition(formula string) (map[string]float64, error)` to formula.go: each element's share of the formula's molar mass, as a percentage."),
			local("Add `type Bottle struct { Code, Name, Formula string; Purity, Grams float64 }` and `func ParseInventory(text string) ([]Bottle, error)` in a new inventory.go, which reads the stores system's export into bottles, in order."),
			cloud("Add `var ErrMissing = errors.New(\"missing reagent\")` and `func LimitingReagent(r Reaction, reagents []Reagent) (string, error)` to amounts.go: the formula of the reactant that runs out first, going by the moles of its Reagent over its coefficient, and on a tie the one that comes first in the reaction. A reactant with no reagent of its formula gives ErrMissing, unless it is one we agreed to leave out."),
			cloud("Add `func TheoreticalYield(r Reaction, reagents []Reagent, product string) (float64, error)` to amounts.go: the grams of that product the limiting reagent allows. It is an error when product is not among the reaction's products."),
			cloud("Refactor: add `type Counts map[string]int` to formula.go, make ParseFormula return Counts, and turn FormatFormula into the method `func (c Counts) String() string`. Update everything that uses them."),
			local("Add `func NotebookEntry(written string, reagents []Reagent) (string, error)` in a new notebook.go: the plan entry for a reaction as the chemist wrote it, with the reagents weighed out for it. Errors from parsing or the calculations are returned."),
			local("formula.go has grown. Move the parser that ParseFormula uses into a new file parser.go, and keep ParseFormula, MolarMass and the rest in formula.go. No behaviour changes."),
			cloud("Add `func AtomEconomy(r Reaction, product string) (float64, error)` to reaction.go: the product's molar mass times its coefficient, as a percentage of the molar masses of all the reactants times their coefficients. An error when product is not a product of r."),
			local("Add `func Normalize(formula string) (string, error)` to formula.go: the formula parsed and written out again with String."),
			cloud("Add `var ErrNotEnough = errors.New(\"not enough in the bottle\")` and `func (b Bottle) Take(grams float64) (Reagent, error)` to inventory.go: a Reagent of that many grams with the bottle's formula and purity, or ErrNotEnough when the bottle holds less."),
			local("NotebookEntry should add a line \"balanced: yes\" or \"balanced: no\" straight after the reaction line, going by Balanced."),
			cloud("Add `func Restock(bottles []Bottle, minGrams float64) []string` to inventory.go: the codes of the bottles holding less than minGrams, sorted."),
			local("ParseInventory should skip blank lines as well, and refuse a line whose purity is above 100."),
			local("Add `func (r Reaction) String() string` to reaction.go: the reaction written out with \" + \" between terms and \" -> \" between the sides, coefficients of 1 left out."),
			cloud("Add `func WeighOut(b Bottle, molarity, mL float64) (float64, error)` to solution.go: how many grams to weigh out of that bottle to make mL millilitres of its reagent at that molarity."),
			local("Add `func SerialDilution(c0, factor float64, steps int) []float64` to solution.go: the concentration after each of steps successive dilutions by factor, starting from c0, which itself is not in the list."),
			local("Add `func FindByFormula(bottles []Bottle, formula string) ([]Bottle, error)` to inventory.go: the bottles that hold the substance with that formula, however either formula is written, in order."),
			cloud("Add `func Stock(bottles []Bottle) (map[string]float64, error)` to inventory.go: the grams held of each substance, keyed by its formula as Normalize writes it."),
		),
		Carries: []Carry{
			{
				What:        "formulas are written in Hill order",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{11},
				Tests:       []string{"TestFormulaStringCarbonFirst", "TestFormulaStringWithoutCarbon", "TestNormalize", "TestStock"},
				Absent:      []string{"hill", "alphabetical", "carbon first", "hydrogen included"},
			},
			{
				What:        "ErrParse for any input we cannot parse",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{8, 9, 15, 26},
				Tests:       []string{"TestParseUnbalancedParentheses", "TestParseReactionNoArrow", "TestParseInventoryBadLine", "TestParseInventoryPurityOver100"},
				Absent:      []string{"errparse", "cannot parse", "errors.is", "sentinel"},
			},
			{
				What:        "hydrates are written with * and the number of waters (docs/inventory.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{12},
				Source:      "docs/inventory.txt",
				Tests:       []string{"TestParseHydrate", "TestMolarMassHydrate"},
				Absent:      []string{"*", "asterisk", "pentahydrate", "cuso4", "5h2o"},
			},
			{
				What:        "a weighed reagent's moles are corrected by its purity (docs/inventory.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{13, 28},
				Source:      "docs/inventory.txt",
				Tests:       []string{"TestReagentMoles", "TestLimitingReagentPurity", "TestNotebookPurity", "TestWeighOut"},
				Absent:      []string{"assay", "times purity", "purity/100", "only that share", "on the balance"},
			},
			{
				What:        "water is never weighed and never limits a reaction",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{16},
				Tests:       []string{"TestLimitingReagentWater", "TestTheoreticalYieldWater", "TestNotebookSkipsWater"},
				Absent:      []string{"never weigh", "solvent", "in excess", "left out when"},
			},
			{
				What:        "the notebook's plan entry layout (docs/notebook.txt)",
				Kind:        CarryDiscovered,
				Established: 5,
				Needed:      []int{19},
				Source:      "docs/notebook.txt",
				Tests:       []string{"TestNotebookLayout"},
				Absent:      []string{"notebook.txt", "two spaces", "limiting: ", "mmol", "first product"},
			},
			{
				What:        "notebook amounts have 2 decimals (and masses 3, later revised)",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{19},
				Tests:       []string{"TestNotebookAmounts"},
				Absent:      []string{"3 decimals", "2 decimals", "three decimals", "two decimals", "what the balance shows"},
			},
			{
				What:        "notebook masses have 4 decimals, revised from 3",
				Kind:        CarryUser,
				Established: 10,
				Needed:      []int{19},
				Tests:       []string{"TestNotebookMasses"},
				Absent:      []string{"4 decimals", "four decimals", "0.1 mg", "new balance"},
			},
		},
	}
}
