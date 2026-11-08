package s2

func init() {
	forgetful["s2-long2-c01-read-trimming"] = []variant{
		{0, "positions from zero", []edit{
			{"stats.go", "PositionStat{Pos: i + 1,", "PositionStat{Pos: i,"},
		}},
		{1, "fresh errors", []edit{
			{"fastq.go", `fmt.Errorf("%w: record %d has no ID", ErrMalformed, n)`, `fmt.Errorf("record %d has no ID", n)`},
			{"fastq.go", `fmt.Errorf("%w: record %d has %d bases and %d qualities", ErrMalformed, n, len(seq), len(qual))`, `fmt.Errorf("record %d has %d bases and %d qualities", n, len(seq), len(qual))`},
			{"fastq.go", `fmt.Errorf("%w: record %d has an unknown base", ErrMalformed, n)`, `fmt.Errorf("record %d has an unknown base", n)`},
			{"fastq.go", `fmt.Errorf("%w: %d records cannot form pairs", ErrMalformed, len(reads))`, `fmt.Errorf("%d records cannot form pairs", len(reads))`},
			{"fastq.go", `fmt.Errorf("%w: %s and %s are not mates", ErrMalformed, a.ID, b.ID)`, `fmt.Errorf("%s and %s are not mates", a.ID, b.ID)`},
		}},
		{2, "one encoding for every instrument", []edit{
			{"quality.go", "func offset(r Read) int {\n\tif strings.HasPrefix(r.ID, \"HWI-\") {\n\t\treturn 64\n\t}\n\treturn 33\n}", "func offset(r Read) int { return 33 }"},
			{"quality.go", "import (\n\t\"math\"\n\t\"strings\"\n)", `import "math"`},
		}},
		{3, "the usual sliding window", []edit{
			{"trim.go", "trimWindow  = 5\n\ttrimMinMean = 25", "trimWindow  = 4\n\ttrimMinMean = 20"},
		}},
		{4, "N's not counted", []edit{
			{"filter.go", "return r.Len() >= minKeptLength && CountN(r) <= maxKeptN", "return r.Len() >= minKeptLength"},
		}},
		{5, "the TruSeq adapter", []edit{
			{"trim.go", `const adapter = "CTGTCTCTTATACACATCT"`, `const adapter = "AGATCGGAAGAGC"`},
		}},
		{6, "the old length", []edit{
			{"filter.go", "minKeptLength = 36", "minKeptLength = 30"},
		}},
		{7, "a format of our own", []edit{
			{"report.go", `strings.Join(fields, "\t")`, `strings.Join(fields, ",")`},
			{"report.go", `"%.1f"`, `"%.2f"`},
		}},
		{8, "IDs compared as they are", []edit{
			{"fastq.go", "name, ok := strings.CutSuffix(first, \"/1\")\n\treturn ok && second == name+\"/2\"", "return first == second"},
		}},
		{9, "exact barcodes only", []edit{
			{"demux.go", "if len(code) == len(want) && Hamming(code, want) <= 1 {", "if code == want {"},
		}},
		{10, "barcode after a colon", []edit{
			{"demux.go", `_, code, ok := strings.Cut(r.ID, "#")`, `_, code, ok := strings.Cut(r.ID, ":")`},
		}},
	}
	forgetful["s2-long2-c02-tensile-tests"] = []variant{
		{0, "fresh errors", []edit{
			{"specimen.go", `fmt.Errorf("%w: %d fields in %q", ErrBadData, len(fields), line)`, `fmt.Errorf("%d fields in %q", len(fields), line)`},
			{"specimen.go", `fmt.Errorf("%w: no ID in %q", ErrBadData, line)`, `fmt.Errorf("no ID in %q", line)`},
			{"specimen.go", `fmt.Errorf("%w: %v", ErrBadData, err)`, "err"},
			{"import.go", `fmt.Errorf("%w: reading %q", ErrBadData, line)`, `fmt.Errorf("bad reading %q", line)`},
			{"props.go", `fmt.Errorf("%w: %d points to fit the modulus", ErrBadData, len(xs))`, `fmt.Errorf("%d points to fit the modulus", len(xs))`},
			{"props.go", `fmt.Errorf("%w: the curve never meets the offset line", ErrBadData)`, `fmt.Errorf("the curve never meets the offset line")`},
		}},
		{1, "every point usable", []edit{
			{"curve.go", "\tlimit := fractureShare * t.Points[peak].Force\n\tfor i := peak + 1; i < len(t.Points); i++ {\n\t\tif t.Points[i].Force < limit {\n\t\t\treturn t.Points[:i]\n\t\t}\n\t}\n", ""},
		}},
		{2, "decimal points and newtons", []edit{
			{"import.go", `strings.Replace(force, ",", ".", 1)`, "force"},
			{"import.go", `strings.Replace(ext, ",", ".", 1)`, "ext"},
			{"import.go", "kn * 1000", "kn"},
		}},
		{2, "summary read as data", []edit{
			{"import.go", "if line == \"\" {\n\t\t\tbreak\n\t\t}", "if line == \"\" {\n\t\t\tcontinue\n\t\t}"},
		}},
		{3, "every point before the peak", []edit{
			{"props.go", "curve, top, peak := t.Curve(), t.UTS(), t.PeakIndex()", "curve, peak := t.Curve(), t.PeakIndex()"},
			{"props.go", "if c := curve[i]; c.Stress >= fitLow*top && c.Stress <= fitHigh*top {", "if c := curve[i]; c.Stress >= 0 {"},
		}},
		{4, "heat before the first dash", []edit{
			{"heat.go", `strings.LastIndex(id, "-")`, `strings.Index(id, "-")`},
		}},
		{5, "tensile as a minimum only", []edit{
			{"spec.go", " || (req.tensileMax > 0 && p.Tensile > req.tensileMax)", ""},
		}},
		{6, "bare numbers", []edit{
			{"report.go", `fmt.Sprintf("%d MPa", int(math.Floor(v)))`, `fmt.Sprintf("%d", int(math.Floor(v)))`},
			{"report.go", `fmt.Sprintf("%.1f%%", v)`, `fmt.Sprintf("%.2f", v)`},
		}},
		{7, "any number of specimens", []edit{
			{"heat.go", "\tif len(tests) < minSpecimens {\n\t\treturn false, nil\n\t}\n", ""},
		}},
		{8, "the old rounding", []edit{
			{"report.go", "int(math.Floor(v))", "int(math.Round(v))"},
		}},
	}
	forgetful["s2-long2-c03-reaction-planner"] = []variant{
		{0, "alphabetical", []edit{
			{"formula.go", `if c["C"] == 0 || (el != "C" && el != "H") {`, "if true {"},
			{"formula.go", "\tif c[\"C\"] > 0 {\n\t\torder = append([]string{\"C\", \"H\"}, order...)\n\t}\n", ""},
		}},
		{0, "carbon and hydrogen always first", []edit{
			{"formula.go", `if c["C"] == 0 || (el != "C" && el != "H") {`, `if el != "C" && el != "H" {`},
			{"formula.go", "if c[\"C\"] > 0 {\n\t\torder = append", "if true {\n\t\torder = append"},
		}},
		{1, "fresh errors", []edit{
			{"parser.go", `fmt.Errorf("%w: unclosed parenthesis in %q", ErrParse, p.s)`, `fmt.Errorf("unclosed parenthesis in %q", p.s)`},
			{"reaction.go", `fmt.Errorf("%w: no -> in %q", ErrParse, s)`, `fmt.Errorf("no -> in %q", s)`},
			{"inventory.go", `fmt.Errorf("%w: inventory line %q", ErrParse, line)`, `fmt.Errorf("bad inventory line %q", line)`},
		}},
		{2, "no hydrates", []edit{
			{"formula.go", `for i, part := range strings.Split(f, "*") {`, "for i, part := range []string{f} {"},
		}},
		{3, "purity ignored", []edit{
			{"amounts.go", "return n * r.Purity / 100, err", "return n, err"},
			{"solution.go", "return g * 100 / b.Purity, nil", "return g, nil"},
		}},
		{4, "water counted", []edit{
			{"amounts.go", "\t\tif t.Formula == water {\n\t\t\tcontinue\n\t\t}\n", ""},
		}},
		{5, "a layout of our own", []edit{
			{"notebook.go", `"%s  %s g  %s mmol\n"`, `"%s: %s g, %s mmol\n"`},
			{"notebook.go", `"limiting: %s; yield: %s g of %s\n"`, `"limiting reagent %s, yield %s g of %s\n"`},
		}},
		{6, "amounts like masses", []edit{
			{"notebook.go", "strconv.FormatFloat(mmol, 'f', 2, 64)", "strconv.FormatFloat(mmol, 'f', 4, 64)"},
		}},
		{7, "the old balance", []edit{
			{"notebook.go", "strconv.FormatFloat(g, 'f', 4, 64)", "strconv.FormatFloat(g, 'f', 3, 64)"},
		}},
	}
	forgetful["s2-long2-c04-quake-catalog"] = []variant{
		{0, "fresh errors", []edit{
			{"catalog.go", `fmt.Errorf("%w: %d fields in %q", ErrBadEvent, len(f), line)`, `fmt.Errorf("%d fields in %q", len(f), line)`},
			{"catalog.go", `fmt.Errorf("%w: no magnitude type or agency in %q", ErrBadEvent, line)`, `fmt.Errorf("no magnitude type or agency in %q", line)`},
			{"catalog.go", `fmt.Errorf("%w: time: %v", ErrBadEvent, err)`, "err"},
			{"catalog.go", `fmt.Errorf("%w: number: %v", ErrBadEvent, err)`, "err"},
		}},
		{1, "depths in kilometres", []edit{
			{"catalog.go", "Depth: nums[2] / 1000,", "Depth: nums[2],"},
			{"catalog.go", "num(e.Depth * 1000)", "num(e.Depth)"},
		}},
		{2, "a tighter window", []edit{
			{"dedup.go", "sameSeconds = 16\n\tsameKm      = 100", "sameSeconds = 10\n\tsameKm      = 50"},
		}},
		{3, "every scale taken as Mw", []edit{
			{"magnitude.go", "\tswitch e.MagType {\n\tcase \"Mw\", \"Md\":\n\t\treturn e.Mag, true\n\tcase \"ML\":\n\t\treturn 0.67*e.Mag + 1.0, true\n\tcase \"mb\":\n\t\treturn 0.85*e.Mag + 1.03, true\n\t}\n\treturn 0, false", "\treturn e.Mag, true"},
		}},
		{4, "the larger magnitude", []edit{
			{"dedup.go", "return rank(a.Agency) <= rank(b.Agency)", "return a.Mag >= b.Mag"},
		}},
		{5, "a plain box", []edit{
			{"geo.go", "\tif r.West > r.East {\n\t\treturn lon >= r.West || lon <= r.East\n\t}\n", ""},
		}},
		{6, "time order", []edit{
			{"bulletin.go", "\t\tif rows[a].mw != rows[b].mw {\n\t\t\treturn rows[a].mw > rows[b].mw\n\t\t}\n", ""},
		}},
		{7, "the old threshold", []edit{
			{"bulletin.go", "bulletinMin = 3.0", "bulletinMin = 2.5"},
		}},
		{8, "a layout of our own", []edit{
			{"bulletin.go", `"LX bulletin %s to %s\n"`, `"Bulletin %s - %s\n"`},
			{"bulletin.go", `return "no earthquakes"`, `return "0 earthquakes"`},
		}},
	}
}
