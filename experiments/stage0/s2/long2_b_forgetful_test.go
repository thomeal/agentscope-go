package s2

func init() {
	forgetful["s2-long2-b01-elevator-bank"] = []variant{
		{0, "building floors in String and the export", []edit{
			{"car.go", `"car %d at %s %s", c.ID, Label(c.Floor), c.Dir`, `"car %d at %d %s", c.ID, c.Floor, c.Dir`},
			{"trace.go", "Label(e.Floor)", "strconv.Itoa(e.Floor)"},
			{"trace.go", "floor, err := ParseLabel(f[2])", "floor, err := strconv.Atoi(f[2])"},
		}},
		{0, "ParseLabel without the gap", []edit{
			{"building.go", "\tcase n > 13:\n\t\tfloor = n - 1\n", ""},
			{"building.go", "\tif Label(floor) != label {\n\t\treturn 0, ErrNoFloor\n\t}\n", ""},
		}},
		{1, "car 3 dispatched", []edit{{"calls.go", "if c.ID == freightCar || c.Load() >= capacity {", "if c.Load() >= capacity {"}}},
		{1, "every car recalled to street level", []edit{{
			"building.go",
			"\t\thome := 0\n\t\tif c.ID == freightCar {\n\t\t\thome = -1\n\t\t}\n\t\tc.Stops = []int{home}",
			"\t\tc.Stops = []int{0}",
		}}},
		{2, "first car wins a tie", []edit{{"calls.go", "cost < bestCost || cost == bestCost && better(c, best)", "cost < bestCost"}}},
		{2, "last car wins a tie", []edit{{"calls.go", "cost < bestCost || cost == bestCost && better(c, best)", "cost <= bestCost"}}},
		{3, "no door time in ETA", []edit{{"route.go", "ticks += abs(next-r.Floor) + doorTicks", "ticks += abs(next-r.Floor)"}}},
		{4, "no capacity", []edit{{"car.go", "const capacity = 8", "const capacity = 1 << 20"}}},
		{5, "boarding in list order", []edit{
			{"car.go", "\tsort.SliceStable(order, func(a, b int) bool {\n\t\treturn waiting[order[a]].CalledAt < waiting[order[b]].CalledAt\n\t})\n", ""},
			{"car.go", "import (\n\t\"fmt\"\n\t\"sort\"\n)", `import "fmt"`},
		}},
		{5, "boarding in any direction", []edit{{"car.go", "\t\tif c.Dir != Idle && r.Going() != c.Dir {\n\t\t\tcontinue\n\t\t}\n", ""}}},
		{6, "plain export lines", []edit{
			{"trace.go", "\tsb.WriteString(traceHeader)\n", ""},
			{"trace.go", `"%05d C%d %s %s\n"`, `"%d %d %s %s\n"`},
			{"trace.go", `len(f) != 4 || len(f[0]) != 5 || !strings.HasPrefix(f[1], "C")`, "len(f) != 4"},
			{"trace.go", "strconv.Atoi(f[1][1:])", "strconv.Atoi(f[1])"},
		}},
		{7, "the old door time", []edit{{"route.go", "doorTicks = 2", "doorTicks = 3"}}},
	}
	forgetful["s2-long2-b02-skirmish-rules"] = []variant{
		{0, "Atk minus the whole Def", []edit{{"damage.go", "base := att.Atk - target.Def/2", "base := att.Atk - target.Def"}}},
		{0, "no floor on damage", []edit{{"damage.go", "\treturn atLeastOne(base * num / den)", "\treturn base * num / den"}}},
		{1, "no element factors", []edit{{"element.go", "case attack == Plain || target == Plain:", "case true:"}}},
		{2, "units that are out healed", []edit{{"unit.go", "\tif !u.Alive() {\n\t\treturn 0\n\t}\n\tgained", "\tgained"}}},
		{2, "guard ignored", []edit{{"turn.go", "\tif target.HasEffect(\"guard\") {\n\t\tdmg = atLeastOne(dmg / 2)\n\t\ttarget.removeEffect(\"guard\")\n\t}\n", ""}}},
		{2, "poison takes 1", []edit{{"effects.go", "u.TakeHit(atLeastOne(u.MaxHP / 8))", "u.TakeHit(1)"}}},
		{2, "poison without a floor", []edit{{"effects.go", "u.TakeHit(atLeastOne(u.MaxHP / 8))", "u.TakeHit(u.MaxHP / 8)"}}},
		{2, "effects never run out", []edit{{"effects.go", "\tkept := u.Effects[:0]\n\tfor _, e := range u.Effects {\n\t\tif e.Turns--; e.Turns > 0 {\n\t\t\tkept = append(kept, e)\n\t\t}\n\t}\n\tu.Effects = kept\n", ""}}},
		{2, "stun ignored", []edit{{"effects.go", "return u.Alive() && !u.HasEffect(\"stun\")", "return u.Alive()"}}},
		{2, "revived to full", []edit{{"effects.go", "u.HP = atLeastOne(u.MaxHP / 4)", "u.HP = u.MaxHP"}}},
		{3, "attack rolls ignored", []edit{
			{"turn.go", "\troll := b.Dice.Roll(20)\n", "\tb.Dice.Roll(20)\n"},
			{"turn.go", "\tswitch roll {\n\tcase 1:\n\t\tb.logf(\"%s misses %s\", att.Name, target.Name)\n\t\treturn 0\n\tcase 20:\n\t\tverb, dmg = \"crits\", critDamage(att, target)\n\t}\n", ""},
			{"damage.go", "\tbare := *target\n\tbare.Def = 0\n\treturn atLeastOne(Damage(att, &bare) * critNum / critDen)", "\treturn Damage(att, target)"},
		}},
		{4, "the old critical", []edit{{"damage.go", "const critNum, critDen = 3, 2", "const critNum, critDen = 2, 1"}}},
		{5, "weakest hero first", []edit{{"battle.go", "dmg > bestDmg || dmg == bestDmg && h.HP < best.HP", "h.HP < best.HP || h.HP == best.HP && dmg > bestDmg"}}},
		{5, "ties to the first hero", []edit{{"battle.go", "dmg > bestDmg || dmg == bestDmg && h.HP < best.HP", "dmg > bestDmg"}}},
		{6, "the whole XP to every hero", []edit{{"battle.go", "share, rest := total/len(heroes), total%len(heroes)", "share, rest := total, 0"}}},
		{6, "remainder dropped", []edit{{"battle.go", "share, rest := total/len(heroes), total%len(heroes)", "share, rest := total/len(heroes), 0"}}},
		{7, "a level every 100 XP", []edit{{"unit.go", "\tlevel := 0\n\tfor level < len(levelXP) && xp >= levelXP[level] {\n\t\tlevel++\n\t}\n\treturn max(level, 1)", "\treturn min(1+xp/100, 5)"}}},
		{8, "a save of its own", []edit{
			{"save.go", `fmt.Fprintf(&sb, "%s\ndice %d\n", saveHeader, b.Dice.state)`, `fmt.Fprintf(&sb, "dice %d\n", b.Dice.state)`},
			{"save.go", `"%s | %d %d %s\n"`, `"%s %d %d %s\n"`},
		}},
	}
	forgetful["s2-long2-b03-greenhouse-rules"] = []variant{
		{0, "version 1 rules accepted", []edit{{"parse.go", `w[0] != "when" || then < 0`, `w[0] != "when" && w[0] != "if" || then < 0`}}},
		{0, "priority always written", []edit{{"parse.go", "\tif r.Priority != 0 {\n\t\tw = append(w, \"priority\", strconv.Itoa(r.Priority))\n\t}\n", "\tw = append(w, \"priority\", strconv.Itoa(r.Priority))\n"}}},
		{0, "no negative priorities", []edit{{"parse.go", "\t\tif err != nil {\n\t\t\treturn Rule{}, fmt.Errorf(\"%w: bad priority %q\"", "\t\tif err != nil || p < 0 {\n\t\t\treturn Rule{}, fmt.Errorf(\"%w: bad priority %q\""}}},
		{1, "missing facts read as empty text", []edit{{"cond.go", "\tv, ok := f[c.Fact]\n\tif !ok {\n\t\treturn false\n\t}\n", "\tv := f[c.Fact]\n"}}},
		{2, "everything compared as text", []edit{
			{"cond.go", "\tif x, y, ok := numbers(v, c.Value); ok {\n\t\treturn holds(c.Op, cmp.Compare(x, y))\n\t}\n\tif c.Op == \"=\" || c.Op == \"!=\" {\n\t\treturn holds(c.Op, strings.Compare(v, c.Value))\n\t}\n\treturn false", "\treturn holds(c.Op, strings.Compare(v, c.Value))"},
			{"cond.go", "import (\n\t\"cmp\"\n\t\"strconv\"\n\t\"strings\"\n)", "import (\n\t\"strconv\"\n\t\"strings\"\n)"},
		}},
		{3, "file order only", []edit{{"agenda.go", "\tslices.SortStableFunc(out, byPriority)\n", ""}}},
		{4, "the first rule in the file wins a tie", []edit{{"agenda.go", "\tslices.Reverse(out)\n", ""}}},
		{5, "no interlocks", []edit{{
			"agenda.go",
			"\tafter := f.Clone()\n\tafter.Apply(r.Then)\n\tif after[\"heater\"] == \"on\" && after[\"vent\"] == \"open\" {\n\t\treturn false\n\t}\n\tif h, ok := after.Num(\"humidity\"); ok && h >= 85 && after[\"mist\"] == \"on\" {\n\t\treturn false\n\t}\n\treturn true",
			"\treturn true",
		}}},
		{5, "readings not known", []edit{{"check.go", `var readings = map[string]bool{"temp": true, "humidity": true, "light": true, "hour": true}`, "var readings = map[string]bool{}"}}},
		{6, "a default of 100", []edit{{"engine.go", "const defaultCycles = 50", "const defaultCycles = 100"}}},
		{6, "a quiet stop at the limit", []edit{{"engine.go", "\t\t\t\treturn fired, ErrLoop\n", "\t\t\t\treturn fired, nil\n"}}},
		{7, "trace lines without the firing number", []edit{
			{"engine.go", `parts := []string{strconv.Itoa(n), r.Name + ":"}`, `parts := []string{r.Name + ":"}`},
			{"engine.go", "import (\n\t\"errors\"\n\t\"strconv\"\n\t\"strings\"\n)", "import (\n\t\"errors\"\n\t\"strings\"\n)"},
		}},
	}
	forgetful["s2-long2-b04-wildfire-grid"] = []variant{
		{0, "a wind named for where it blows to", []edit{{"spread.go", "\tcase \"N\":\n\t\treturn 1, 0\n\tcase \"S\":\n\t\treturn -1, 0\n\tcase \"E\":\n\t\treturn 0, -1\n\tcase \"W\":\n\t\treturn 0, 1\n", "\tcase \"N\":\n\t\treturn -1, 0\n\tcase \"S\":\n\t\treturn 1, 0\n\tcase \"E\":\n\t\treturn 0, 1\n\tcase \"W\":\n\t\treturn 0, -1\n"}}},
		{1, "shrub burns for one tick", []edit{{"cell.go", "shrubTicks = 2", "shrubTicks = 1"}}},
		{2, "trees still burn for three ticks", []edit{{"cell.go", "treeTicks  = 5", "treeTicks  = 3"}}},
		{3, "calm kept as a wind", []edit{{"mapfile.go", "\tcase \"wind N\", \"wind E\", \"wind S\", \"wind W\":\n\t\tg.Wind = line[5:]\n\tcase \"wind calm\":\n", "\tcase \"wind N\", \"wind E\", \"wind S\", \"wind W\", \"wind calm\":\n\t\tg.Wind = line[5:]\n"}}},
		{3, "the header not checked", []edit{{"mapfile.go", "if len(lines) < 3 || lines[0] != \"firemap 2\" {", "if len(lines) < 3 {"}}},
		{3, "fire and ash drawn as letters", []edit{{"mapfile.go", "\t\treturn '*'\n\tcase c.Ash:\n\t\treturn '_'\n", "\t\treturn 'F'\n\tcase c.Ash:\n\t\treturn 'A'\n"}}},
		{3, "no wind written blank", []edit{{"mapfile.go", "\tif wind == \"\" {\n\t\twind = \"calm\"\n\t}\n", ""}}},
		{4, "the standing orders ignored", []edit{{"crew.go", "\tif cell == nil || cell.Fuel == Water || cell.Burning > 0 || cell.Ash {\n\t\treturn ErrCannotDig\n\t}\n\tfor _, n := range g.Neighbors(r, c) {\n\t\tif g.At(n[0], n[1]).Burning > 0 {\n\t\t\treturn ErrCannotDig\n\t\t}\n\t}\n", "\tif cell == nil {\n\t\treturn ErrCannotDig\n\t}\n"}}},
		{4, "corners off limits too", []edit{{"crew.go", "\tfor _, n := range g.Neighbors(r, c) {\n\t\tif g.At(n[0], n[1]).Burning > 0 {\n\t\t\treturn ErrCannotDig\n\t\t}\n\t}\n", "\tfor dr := -1; dr <= 1; dr++ {\n\t\tfor dc := -1; dc <= 1; dc++ {\n\t\t\tif n := g.At(r+dr, c+dc); n != nil && n.Burning > 0 {\n\t\t\t\treturn ErrCannotDig\n\t\t\t}\n\t\t}\n\t}\n"}}},
		{4, "an existing firebreak refused", []edit{{"crew.go", "\tif cell != nil && cell.Fuel == Break {\n\t\treturn nil\n\t}\n", "\tif cell != nil && cell.Fuel == Break {\n\t\treturn ErrCannotDig\n\t}\n"}}},
		{5, "rounded to the nearest percent", []edit{{"stats.go", "\treturn burnt * 100 / fuel\n", "\treturn (burnt*100 + fuel/2) / fuel\n"}}},
		{5, "only ash counts as burnt", []edit{{"stats.go", "\t\tif c.Burning > 0 || c.Ash {\n\t\t\tburnt++", "\t\tif c.Ash {\n\t\t\tburnt++"}}},
		{6, "only plain newlines read", []edit{
			{"mapfile.go", `text = strings.ReplaceAll(text, "\x0d\n", "\n")`, ""},
			{"orders.go", `text = strings.ReplaceAll(text, "\x0d\n", "\n")`, ""},
		}},
	}
}
