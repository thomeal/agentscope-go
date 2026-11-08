package s2

func init() {
	forgetful["s2-a01-library-loans"] = []variant{
		{0, "two-week loans", []edit{{"loan.go", "const loanDays = 21", "const loanDays = 14"}}},
		{1, "renewal counts from today", []edit{{"loan.go", "d.loans[i].DueDay += loanDays", "d.loans[i].DueDay = today + loanDays"}}},
		{2, "fresh errors", []edit{
			{"loan.go", "return Loan{}, ErrNotOut", `return Loan{}, errors.New("no such loan")`},
			{"loan.go", "return 0, ErrNotOut", `return 0, errors.New("no such loan")`},
			{"loan.go", "return ErrNotOut", `return errors.New("no such loan")`},
		}},
		{3, "junior cards charged", []edit{
			{"fees.go", "import \"strings\"\n\n", ""},
			{"fees.go", "\tif strings.HasPrefix(l.Member, \"J-\") {\n\t\treturn 0\n\t}\n", ""},
		}},
	}
	forgetful["s2-a02-payroll-hours"] = []variant{
		{0, "truncating", []edit{{"pay.go", "(minutes*centsPerHour + 59) / 60", "minutes * centsPerHour / 60"}}},
		{1, "End minus Start", []edit{{"hours.go", "\tif sh.End < sh.Start {\n\t\treturn sh.End + dayMinutes - sh.Start\n\t}\n", ""}}},
		{2, "no error for an empty week", []edit{{"hours.go", "return 0, ErrNoShifts", "return 0, nil"}}},
		{3, "forty hours at time and a half", []edit{
			{"pay.go", "overtimeAfter = 38 * 60", "overtimeAfter = 40 * 60"},
			{"pay.go", "overtimeNum   = 5", "overtimeNum   = 3"},
			{"pay.go", "overtimeDen   = 4", "overtimeDen   = 2"},
		}},
	}
	forgetful["s2-a03-loyalty-points"] = []variant{
		{0, "one point per euro", []edit{{"earn.go", "const pointsPerEuro = 5", "const pointsPerEuro = 1"}}},
		{1, "no tiers", []edit{{"earn.go", "\tswitch tier {\n\tcase \"gold\":\n\t\treturn base * 2\n\tcase \"silver\":\n\t\treturn base * 3 / 2\n\t}\n", ""}}},
		{2, "gone on the day itself", []edit{{"account.go", "return today <= b.Expires", "return today < b.Expires"}}},
		{3, "fresh errors", []edit{{"account.go", "return ErrInsufficient", `return errors.New("insufficient balance")`}}},
		{4, "batches in the order earned", []edit{
			{"account.go", "import (\n\t\"errors\"\n\t\"sort\"\n)", `import "errors"`},
			{"account.go", "\tsort.SliceStable(a.batches, func(i, j int) bool { return a.batches[i].Expires < a.batches[j].Expires })\n", ""},
		}},
	}
	forgetful["s2-a04-coupon-redemption"] = []variant{
		{0, "zero means none", []edit{{"coupon.go", "return c.MaxUses > 0 && c.Uses >= c.MaxUses", "return c.Uses >= c.MaxUses"}}},
		{1, "case and spaces only", []edit{{"coupon.go", `return strings.NewReplacer("O", "0", "I", "1").Replace(s)`, "return s"}}},
		{1, "exact match", []edit{
			{"coupon.go", "import (\n\t\"errors\"\n\t\"strings\"\n)", `import "errors"`},
			{"coupon.go", "\ts := strings.ToUpper(strings.TrimSpace(typed))\n\treturn strings.NewReplacer(\"O\", \"0\", \"I\", \"1\").Replace(s)\n", "\treturn typed\n"},
		}},
		{2, "fresh errors", []edit{
			{"coupon.go", "return ErrUnknownCode", `return errors.New("no such coupon")`},
			{"coupon.go", "return 0, ErrUnknownCode", `return 0, errors.New("no such coupon")`},
		}},
		{3, "no minimum charge", []edit{{"price.go", "return min(subtotal, max(due, minCharge))", "return due"}}},
	}
	forgetful["s2-a05-expense-reports"] = []variant{
		{0, "zero accepted", []edit{{"report.go", "\tif l.Cents == 0 {\n\t\treturn errors.New(\"zero line\")\n\t}\n", ""}}},
		{0, "positive only", []edit{{"report.go", "if l.Cents == 0 {", "if l.Cents <= 0 {"}}},
		{1, "everything paid", []edit{{"pay.go", "total += paid(l)", "total += l.Cents"}}},
		{2, "lines removable in any state", []edit{{"report.go", "\tif r.state != \"draft\" {\n\t\treturn ErrLocked\n\t}\n\tif i < 0", "\tif i < 0"}}},
		{3, "own report allowed", []edit{{"report.go", "\tif approver == r.Owner {\n\t\treturn ErrOwnReport\n\t}\n", ""}}},
		{4, "exact distance", []edit{{"pay.go", "(meters + 999) / 1000 * centsPerKm", "meters * centsPerKm / 1000"}}},
		{4, "another rate", []edit{{"pay.go", "centsPerKm  = 42", "centsPerKm  = 30"}}},
	}
	forgetful["s2-a06-rent-ledger"] = []variant{
		{0, "owed is positive", []edit{
			{"ledger.go", "case \"payment\":\n\t\t\ttotal += e.Cents\n\t\tcase \"charge\":\n\t\t\ttotal -= e.Cents", "case \"payment\":\n\t\t\ttotal -= e.Cents\n\t\tcase \"charge\":\n\t\t\ttotal += e.Cents"},
			{"ledger.go", "if l.standing(tenant) < 0 {", "if l.standing(tenant) > 0 {"},
		}},
		{1, "fresh errors", []edit{{"ledger.go", "return 0, ErrUnknownTenant", `return 0, errors.New("no such tenant")`}}},
		{2, "guessed late fee", []edit{
			{"fees.go", "lastFreeDay = 5", "lastFreeDay = 1"},
			{"fees.go", "lateCents   = 4000", "lateCents   = 2500"},
		}},
		{3, "deposit counted as paid", []edit{{"ledger.go", `case "payment":`, `case "payment", "deposit":`}}},
		{4, "calendar month", []edit{
			{"prorate.go", "const flatMonth = 30\n\n", ""},
			{"prorate.go", "days := flatMonth - moveIn.Day() + 1", "flatMonth := time.Date(moveIn.Year(), moveIn.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()\n\tdays := flatMonth - moveIn.Day() + 1"},
		}},
		{4, "move-in day free", []edit{{"prorate.go", "- moveIn.Day() + 1", "- moveIn.Day()"}}},
	}
	forgetful["s2-a07-gym-waitlist"] = []variant{
		{0, "spot just stays open", []edit{
			{"class.go", "\t\tc.fill()\n", ""},
			{"class.go", "\tc.Capacity = n\n\tc.fill()\n", "\tc.Capacity = n\n"},
		}},
		{1, "no waiting list limit", []edit{{"class.go", "\tif len(c.waiting) >= maxWaiting {\n\t\treturn ErrListFull\n\t}\n", ""}}},
		{2, "a one-hour rule", []edit{{"class.go", "lateCancel = 120", "lateCancel = 60"}}},
		{2, "strike at the limit", []edit{{"class.go", "booked && minutesBefore < lateCancel", "booked && minutesBefore <= lateCancel"}}},
		{2, "waiting list strikes too", []edit{{"class.go", "if booked && minutesBefore", "_ = booked\n\tif minutesBefore"}}},
		{3, "line counted from zero", []edit{{"class.go", "return i + 1, nil", "return i, nil"}}},
		{4, "fresh errors", []edit{
			{"class.go", "return 0, ErrNotInClass", `return 0, errors.New("no such member")`},
			{"class.go", "return false, ErrNotInClass", `return false, errors.New("no such member")`},
		}},
	}
	forgetful["s2-a08-restaurant-tabs"] = []variant{
		{0, "tax added on top", []edit{{"tab.go", "return t.Subtotal() / 11", "return t.Subtotal() * 10 / 100"}}},
		{1, "only Add checks", []edit{
			{"tab.go", "\tif t.closed {\n\t\treturn ErrClosed\n\t}\n\tif strings.HasPrefix", "\tif strings.HasPrefix"},
			{"tab.go", "\tif t.closed {\n\t\treturn ErrClosed\n\t}\n\ti := t.find(name)", "\ti := t.find(name)"},
		}},
		{2, "drinks comped", []edit{
			{"tab.go", "import (\n\t\"errors\"\n\t\"strings\"\n)", `import "errors"`},
			{"tab.go", "\tif strings.HasPrefix(name, \"bar:\") {\n\t\treturn ErrNoComp\n\t}\n", ""},
		}},
		{3, "no service", []edit{{"tab.go", "\tif t.Party >= serviceFrom {\n\t\treturn sub + sub*servicePercent/100\n\t}\n", ""}}},
		{4, "leftover on the last share", []edit{{"split.go", "if i < total%ways {\n\t\t\tshares[i]++\n\t\t}", "if i == ways-1 {\n\t\t\tshares[i] += total % ways\n\t\t}"}}},
	}
	forgetful["s2-a09-event-ticketing"] = []variant{
		{0, "sorted as text", []edit{{"event.go", "sort.Slice(out, func(i, j int) bool { return seatLess(out[i], out[j]) })", "sort.Strings(out)"}}},
		{1, "no buyer limit", []edit{{"event.go", "\tif e.seatsOf(buyer) >= maxPerBuyer {\n\t\treturn ErrBuyerLimit\n\t}\n", ""}}},
		{2, "fresh errors", []edit{
			{"event.go", "sold {\n\t\treturn ErrSeatTaken", "sold {\n\t\treturn errors.New(\"seat unavailable\")"},
			{"event.go", "h.buyer != buyer {\n\t\treturn ErrSeatTaken", "h.buyer != buyer {\n\t\treturn errors.New(\"seat unavailable\")"},
		}},
		{3, "a fifteen-minute hold", []edit{{"event.go", "holdSeconds = 600", "holdSeconds = 900"}}},
		{4, "full price always", []edit{{"price.go", "\tif daysBefore >= earlyDays {\n\t\treturn baseCents * 80 / 100\n\t}\n", ""}}},
	}
	forgetful["s2-a10-parcel-lockers"] = []variant{
		{0, "first one that fits", []edit{{"locker.go", "l.Size >= size && (best < 0 || l.Size < b.lockers[best].Size)", "l.Size >= size && best < 0"}}},
		{1, "plain number", []edit{{"locker.go", `fmt.Sprintf("%06d", b.issued)`, "fmt.Sprint(b.issued)"}}},
		{2, "fresh error", []edit{{"locker.go", "return Slip{}, ErrNoLocker", `return Slip{}, errors.New("bank full")`}}},
		{3, "no charges and a longer stay", []edit{
			{"fees.go", "feePerBlock  = 200", "feePerBlock  = 0"},
			{"fees.go", "overdueAfter = 7 * 24 * hour", "overdueAfter = 30 * 24 * hour"},
		}},
		{4, "time counted in hours", []edit{{"fees.go", "hour         = 60", "hour         = 1"}}},
		{5, "fresh error", []edit{{"locker.go", `return "", ErrBadCode`, `return "", errors.New("no such code")`}}},
	}
}
