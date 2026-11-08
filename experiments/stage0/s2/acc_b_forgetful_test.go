package s2

func init() {
	forgetful["s2-b01-rate-limiter"] = []variant{
		{0, "window from the first request", []edit{{
			"limiter.go",
			"start := now - now%l.window\n\tif l.start[key] != start {\n\t\tl.start[key] = start\n\t\tl.used[key] = 0\n\t}",
			"if s, ok := l.start[key]; !ok || now-s >= l.window {\n\t\tl.start[key] = now\n\t\tl.used[key] = 0\n\t}",
		}}},
		{1, "service keys limited", []edit{
			{"limiter.go", `return strings.HasPrefix(key, "svc-")`, "return false"},
			{"limiter.go", "import (\n\t\"errors\"\n\t\"strings\"\n)", `import "errors"`},
		}},
		{2, "milliseconds", []edit{{"limiter.go", "return int((l.start[key] + l.window - now + 999) / 1000)", "return int(l.start[key] + l.window - now)"}}},
		{2, "rounded down", []edit{{"limiter.go", "return int((l.start[key] + l.window - now + 999) / 1000)", "return int((l.start[key] + l.window - now) / 1000)"}}},
		{3, "fresh error", []edit{{"limiter.go", "return ErrLimited", `return errors.New("too many requests")`}}},
	}
	forgetful["s2-b02-log-rotation"] = []variant{
		{0, "bytes", []edit{{"policy.go", "return size >= p.MaxSize*1024", "return size >= p.MaxSize"}}},
		{1, "plain names only", []edit{{"policy.go", "\trest = strings.TrimSuffix(rest, \".gz\")\n", ""}}},
		{2, "Keep counts rotated files", []edit{{"policy.go", "ok && g >= p.Keep {", "ok && g > p.Keep {"}}},
		{3, "audit logs pruned", []edit{{"policy.go", "\tif strings.HasPrefix(base, \"audit\") {\n\t\treturn nil\n\t}\n", ""}}},
	}
	forgetful["s2-b03-url-router"] = []variant{
		{0, "first registered wins", []edit{{"router.go", "if ok && (!found || better(rt.Pattern, best.Pattern)) {", "if ok && !found {"}}},
		{1, "slash at the end significant", []edit{{"router.go", `strings.Split(strings.Trim(p, "/"), "/")`, `strings.Split(strings.TrimPrefix(p, "/"), "/")`}}},
		{2, "declared methods only", []edit{{"router.go", "\t\t\tif rt.Method == \"GET\" {\n\t\t\t\tseen[\"HEAD\"] = true\n\t\t\t}\n", ""}}},
		{3, "fresh errors", []edit{
			{"router.go", `return "", ErrNotFound`, `return "", errors.New("no route with that name")`},
			{"router.go", "\treturn ErrNotFound\n", "\treturn errors.New(\"no route with that name\")\n"},
		}},
	}
	forgetful["s2-b04-flag-parser"] = []variant{
		{0, "conventions ignored", []edit{
			{"flags.go", "\t\tif a == \"--\" {\n\t\t\tres.Args = append(res.Args, args[i+1:]...)\n\t\t\treturn res, nil\n\t\t}\n", ""},
			{"flags.go", `if a == "-" || !strings.HasPrefix(a, "-") {`, `if !strings.HasPrefix(a, "-") {`},
			{"flags.go", "\t\t\tif s.Bool && \"no-\"+s.Name == canon(name) {\n\t\t\t\treturn s, \"false\", true, nil\n\t\t\t}\n", ""},
		}},
		{1, "flags anywhere", []edit{{"flags.go", "res.Args = append(res.Args, args[i:]...)\n\t\t\treturn res, nil", "res.Args = append(res.Args, a)\n\t\t\tcontinue"}}},
		{2, "names as spelled", []edit{{"flags.go", `return strings.ReplaceAll(name, "_", "-")`, "return name"}}},
		{3, "fresh errors", []edit{
			{"flags.go", `fmt.Errorf("%w: %s", ErrUnknownFlag, arg)`, `fmt.Errorf("unknown flag: %s", arg)`},
			{"flags.go", `fmt.Errorf("%w: %s", ErrMissingValue, a)`, `fmt.Errorf("missing value: %s", a)`},
		}},
	}
	forgetful["s2-b05-template-renderer"] = []variant{
		{0, "unknown renders empty", []edit{{"vars.go", `return "{{" + inner + "}}", nil`, `return "", nil`}}},
		{1, "not escaped", []edit{
			{"vars.go", "return html.EscapeString(value), nil", "return value, nil"},
			{"vars.go", "\t\"html\"\n", ""},
		}},
		{2, "every occurrence listed", []edit{{"vars.go", "if ok && !seen[name] {", "if ok {"}}},
		{2, "sorted", []edit{
			{"vars.go", "return names\n", "slices.Sort(names)\n\treturn names\n"},
			{"vars.go", "\t\"strings\"\n", "\t\"slices\"\n\t\"strings\"\n"},
		}},
		{3, "fresh error", []edit{{"vars.go", `return "", ErrUnclosed`, `return "", errors.New("unterminated placeholder")`}}},
	}
	forgetful["s2-b06-feature-flags"] = []variant{
		{0, "hash of the user alone", []edit{{"store.go", `h.Write([]byte(name + "/" + user))`, "h.Write([]byte(user))"}}},
		{1, "allow list first", []edit{{
			"store.go",
			"if !ok || !f.Enabled {\n\t\treturn false\n\t}\n\tif slices.Contains(f.Allow, user) {\n\t\treturn true\n\t}\n",
			"if !ok {\n\t\treturn false\n\t}\n\tif slices.Contains(f.Allow, user) {\n\t\treturn true\n\t}\n\tif !f.Enabled {\n\t\treturn false\n\t}\n",
		}}},
		{2, "empty id bucketed", []edit{{"store.go", "\tif user == \"\" {\n\t\treturn f.Percent >= 100\n\t}\n", ""}}},
		{3, "no name check", []edit{
			{"store.go", "\tif !ValidName(f.Name) {\n\t\treturn ErrBadName\n\t}\n", ""},
			{"store.go", "\tif !ValidName(new) {\n\t\treturn ErrBadName\n\t}\n", ""},
		}},
	}
	forgetful["s2-b07-version-compare"] = []variant{
		{0, "pre-release as text", []edit{{"version.go", "return comparePre(a.Pre, b.Pre)", "return cmp.Compare(a.Pre, b.Pre)"}}},
		{1, "no leading v", []edit{{"version.go", "\ts = strings.TrimPrefix(s, \"v\")\n", ""}}},
		{1, "patch required", []edit{{"version.go", "\tif len(parts) == 2 {\n\t\tparts = append(parts, \"0\")\n\t}\n", ""}}},
		{2, "pre-release may win", []edit{{"version.go", "\tif a.IsPre() != b.IsPre() {\n\t\treturn b.IsPre()\n\t}\n", ""}}},
		{2, "bad tag fails", []edit{{"version.go", "\t\tif err != nil {\n\t\t\tcontinue\n\t\t}\n", "\t\tif err != nil {\n\t\t\treturn \"\", err\n\t\t}\n"}}},
		{3, "fresh error", []edit{{"version.go", "return Version{}, ErrInvalid", `return Version{}, errors.New("not a version")`}}},
	}
	forgetful["s2-b08-glob-matcher"] = []variant{
		{0, "at least one segment", []edit{{
			"glob.go",
			"if matchSegs(pat[1:], segs) {\n\t\t\treturn true\n\t\t}\n\t\treturn len(segs) > 0 && !hidden(segs[0]) && matchSegs(pat, segs[1:])",
			"return len(segs) > 0 && !hidden(segs[0]) && (matchSegs(pat[1:], segs[1:]) || matchSegs(pat, segs[1:]))",
		}}},
		{1, "dotfiles matched", []edit{{"glob.go", `return strings.HasPrefix(seg, ".")`, "return false"}}},
		{2, "plain pattern list", []edit{
			{"glob.go", `if line == "" || strings.HasPrefix(line, "#") {`, `if line == "" {`},
			{"glob.go", `pattern, negated := strings.CutPrefix(line, "!")`, "pattern, negated := line, false"},
		}},
		{2, "first match decides", []edit{{"glob.go", "ignored = !negated", "return !negated, nil"}}},
		{3, "patterns not checked", []edit{{"glob.go", "\t\tif err := Validate(pattern); err != nil {\n\t\t\treturn false, err\n\t\t}\n", ""}}},
	}
	forgetful["s2-b09-circuit-breaker"] = []variant{
		{0, "opens at Threshold", []edit{{"breaker.go", "if b.failures > b.Threshold {", "if b.failures >= b.Threshold {"}}},
		{1, "cancelled counted", []edit{
			{"breaker.go", "\tcase errors.Is(err, context.Canceled):\n\t\tb.trial = false\n", ""},
			{"breaker.go", "\t\"context\"\n", ""},
		}},
		{1, "other state names", []edit{
			{"breaker.go", `return "closed"`, `return "Closed"`},
			{"breaker.go", `return "open"`, `return "Open"`},
			{"breaker.go", `return "half-open"`, `return "HalfOpen"`},
		}},
		{2, "fixed wait", []edit{{"breaker.go", "b.open(now, min(2*b.wait, 8*b.Cooldown))", "b.open(now, b.Cooldown)"}}},
		{3, "fresh error", []edit{{"breaker.go", "return ErrOpen", `return errors.New("breaker open")`}}},
	}
	forgetful["s2-b10-dns-records"] = []variant{
		{0, "names as written", []edit{{"zone.go", `return strings.ToLower(strings.TrimSuffix(name, "."))`, "return name"}}},
		{1, "no floor", []edit{{"zone.go", "TTL:   max(ttl, 60),", "TTL:   ttl,"}}},
		{2, "format notes ignored", []edit{
			{"zone.go", "\t\tline, _, _ = strings.Cut(line, \";\")\n", ""},
			{"zone.go", "\t\tcase fields[0] == \"$ORIGIN\" && len(fields) == 2:\n\t\t\torigin = strings.TrimSuffix(fields[1], \".\")\n\t\t\tcontinue\n", ""},
		}},
		{3, "plain error", []edit{{"zone.go", "return nil, &ParseError{Line: i + 1, Msg: err.Error()}", `return nil, fmt.Errorf("line %d: %w", i+1, err)`}}},
		{4, "records in file order", []edit{{"zone.go", "\tif typ == \"MX\" {\n\t\tsort.SliceStable(out, func(a, b int) bool { return preference(out[a]) < preference(out[b]) })\n\t}\n", ""}}},
	}
}
