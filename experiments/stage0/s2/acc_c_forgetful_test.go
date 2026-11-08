package s2

func init() {
	forgetful["s2-c01-unit-conversion"] = []variant{
		{0, "full precision", []edit{
			{"units.go", "func tidy(v float64) float64 { return math.Round(v*1000) / 1000 }", "func tidy(v float64) float64 { return v }"},
			{"units.go", "\t\"math\"\n", ""},
		}},
		{1, "fresh error", []edit{
			{"units.go", "return 0, ErrUnknownUnit", `return 0, errors.New("no such unit")`},
		}},
		{2, "names as given", []edit{
			{"units.go", "func normal(unit string) string { return strings.ToLower(strings.TrimSpace(unit)) }", "func normal(unit string) string { return unit }"},
		}},
	}
	forgetful["s2-c02-csv-merge"] = []variant{
		{0, "later table overwrites", []edit{
			{"table.go", "if _, dup := at[r[kb]]; dup {\n\t\t\tcontinue\n\t\t}", "if i, dup := at[r[kb]]; dup {\n\t\t\tout.Rows[i] = row\n\t\t\tcontinue\n\t\t}"},
		}},
		{1, "fresh error", []edit{
			{"table.go", "return nil, ErrNoKey", `return nil, errors.New("no such column")`},
			{"table.go", "return Table{}, ErrNoKey", `return Table{}, errors.New("no such column")`},
		}},
		{2, "totals lines kept", []edit{
			{"table.go", "if r[col] != \"\" {", "if true {"},
		}},
		{3, "commas", []edit{
			{"table.go", "\tw.Comma = ';'\n", ""},
		}},
	}
	forgetful["s2-c03-downsample"] = []variant{
		{0, "buckets from the first sample", []edit{
			{"series.go", "for _, p := range Sorted(points) {", "sorted := Sorted(points)\n\tfor _, p := range sorted {"},
			{"series.go", "if l := label(p.T, width); l != cur {", "if l := sorted[0].T + (p.T-sorted[0].T)/width*width; l != cur {"},
		}},
		{1, "fresh error", []edit{
			{"series.go", "return nil, ErrBadWidth", `return nil, errors.New("width must be positive")`},
		}},
		{2, "NaN treated as a value", []edit{
			{"series.go", "\t\tif math.IsNaN(p.V) {\n\t\t\tcontinue\n\t\t}\n", ""},
			{"series.go", "\t\"math\"\n", ""},
		}},
	}
	forgetful["s2-c04-gradebook"] = []variant{
		{0, "integer division", []edit{
			{"book.go", "return (200*earned + possible) / (2 * possible)", "return 100 * earned / possible"},
		}},
		{1, "every score counted", []edit{
			{"book.go", "func counts(s Score) bool { return s.Earned != -1 }", "func counts(s Score) bool { return true }"},
		}},
		{2, "the usual tens", []edit{
			{"book.go", "case p >= 92:", "case p >= 90:"},
			{"book.go", "case p >= 83:", "case p >= 80:"},
			{"book.go", "case p >= 72:", "case p >= 70:"},
		}},
	}
	forgetful["s2-c05-recipe-scaling"] = []variant{
		{0, "fractions of an egg", []edit{
			{"recipe.go", "return math.Ceil(it.Amount * factor)", "return it.Amount * factor"},
		}},
		{1, "writes through the shared slice", []edit{
			{"recipe.go", "out.Items = append([]Ingredient(nil), r.Items...)\n", ""},
			{"recipe.go", "out.Items = append([]Ingredient(nil), a.Items...)\n", ""},
		}},
		{2, "pinches scale", []edit{
			{"recipe.go", "\tcase \"pinch\":\n\t\treturn it.Amount\n", ""},
		}},
	}
	forgetful["s2-c06-seating-chart"] = []variant{
		{0, "every letter", []edit{
			{"chart.go", `const rowLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ"`, `const rowLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"`},
		}},
		{1, "fresh error", []edit{
			{"chart.go", "return ErrTaken", `return errors.New("seat unavailable")`},
		}},
		{2, "last row used", []edit{
			{"chart.go", "if row == c.Rows-1 {", "if false {"},
			{"chart.go", "return (c.Rows-1)*c.Cols - c.Occupied()", "return c.Rows*c.Cols - c.Occupied()"},
			{"chart.go", "for row := 0; row < c.Rows-1; row++ {", "for row := 0; row < c.Rows; row++ {"},
		}},
		{3, "walk-ins always get a new seat", []edit{
			{"chart.go", "\tif at, ok := c.find(name); ok {\n\t\treturn c.Label(at[0], at[1]), nil\n\t}\n", ""},
		}},
	}
	forgetful["s2-c07-income-tax"] = []variant{
		{0, "exact cents", []edit{
			{"brackets.go", "func wholeDollars(cents int64) int64 { return cents / 100 * 100 }", "func wholeDollars(cents int64) int64 { return cents }"},
		}},
		{0, "nearest dollar", []edit{
			{"brackets.go", "func wholeDollars(cents int64) int64 { return cents / 100 * 100 }", "func wholeDollars(cents int64) int64 { return (cents + 50) / 100 * 100 }"},
		}},
		{1, "no allowances", []edit{
			{"brackets.go", "max(income-allowance-perDependent*int64(dependents), 0)", "income"},
		}},
		{2, "permille as in the schedule", []edit{
			{"brackets.go", "basisPoints  = 10000", "basisPoints  = 1000"},
		}},
	}
	forgetful["s2-c08-calendar-recurrence"] = []variant{
		{0, "months added by the library", []edit{
			{"rule.go", "\ts := r.Start\n\tfirst := time.Date(s.Year(), s.Month()+time.Month(n*r.Interval), 1, s.Hour(), s.Minute(), s.Second(), 0, s.Location())\n\treturn first.AddDate(0, 0, min(s.Day(), DaysIn(first.Year(), first.Month()))-1)\n", "\treturn r.Start.AddDate(0, n*r.Interval, 0)\n"},
		}},
		{1, "fresh error", []edit{
			{"rule.go", "return time.Time{}, ErrBadRule", `return time.Time{}, errors.New("rule is not valid")`},
			{"rule.go", "return nil, ErrBadRule", `return nil, errors.New("rule is not valid")`},
		}},
		{2, "weekends kept", []edit{
			{"rule.go", "\tswitch t.Weekday() {\n\tcase time.Saturday:\n\t\treturn t.AddDate(0, 0, 2)\n\tcase time.Sunday:\n\t\treturn t.AddDate(0, 0, 1)\n\t}\n", ""},
		}},
		{3, "to date left out", []edit{
			{"rule.go", "if t.After(to) {", "if !t.Before(to) {"},
		}},
	}
	forgetful["s2-c09-reorder-forecast"] = []variant{
		{0, "whole history", []edit{
			{"item.go", "if len(sales) > demandDays {", "if false {"},
		}},
		{1, "zeros averaged in", []edit{
			{"item.go", "if s > 0 {\n\t\t\tsum += s\n\t\t\tdays++\n\t\t}", "sum += s\n\t\tdays++"},
		}},
		{2, "lead time alone", []edit{
			{"item.go", "float64(it.LeadDays+SafetyDays)", "float64(it.LeadDays)"},
		}},
		{3, "exact quantity", []edit{
			{"item.go", "return (need + casePack - 1) / casePack * casePack", "return need"},
		}},
	}
	forgetful["s2-c10-text-tokenizer"] = []variant{
		{0, "offsets from the encoded text", []edit{
			{"token.go", "Start: start})", "Start: len(string(rs[:start]))})"},
			{"token.go", "rs := []rune(text)\n\tfrom :=", "rs := []byte(text)\n\tfrom :="},
			{"token.go", "utf8.RuneCountInString(t.Text)", "len(t.Text)"},
			{"token.go", "\t\"unicode/utf8\"\n", ""},
		}},
		{1, "ASCII letters only", []edit{
			{"token.go", "func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }", "func wordRune(r rune) bool { return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }"},
		}},
		{1, "apostrophes end a word", []edit{
			{"token.go", "return rs[i] == '\\'' && i+1 < len(rs) && unicode.IsLetter(rs[i-1]) && unicode.IsLetter(rs[i+1])", "return false"},
		}},
		{1, "apostrophes always kept", []edit{
			{"token.go", "return rs[i] == '\\'' && i+1 < len(rs) && unicode.IsLetter(rs[i-1]) && unicode.IsLetter(rs[i+1])", "return rs[i] == '\\''"},
		}},
		{1, "hyphens kept", []edit{
			{"token.go", "return rs[i] == '\\'' && i+1 < len(rs) && unicode.IsLetter(rs[i-1]) && unicode.IsLetter(rs[i+1])", "return (rs[i] == '\\'' || rs[i] == '-') && i+1 < len(rs) && unicode.IsLetter(rs[i-1]) && unicode.IsLetter(rs[i+1])"},
		}},
		{2, "exact comparison", []edit{
			{"token.go", "if Normalize(t.Text) == Normalize(word) {", "if t.Text == word {"},
			{"token.go", "key := Normalize(t.Text)", "key := t.Text"},
		}},
		{3, "stop words counted", []edit{
			{"token.go", "if !stopWords[key] {", "if true {"},
		}},
	}
}
