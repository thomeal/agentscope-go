package s2

func init() {
	forgetful["s2-long2-a01-plant-care"] = []variant{
		{0, "exact names", []edit{{"plant.go", "if key(p.Name) == key(name) {\n\t\t\treturn i", "if p.Name == name {\n\t\t\treturn i"}}},
		{1, "monthly and all year", []edit{
			{"care.go", "next = p.LastFed.AddDate(0, 0, 28)", "next = p.LastFed.AddDate(0, 1, 0)"},
			{"care.go", "if m := next.Month(); m < time.March || m > time.September {", "if m := next.Month(); false && m < time.March {"},
		}},
		{2, "from the day we leave to the day we return", []edit{{
			"vacation.go",
			"for d := t.Leave.AddDate(0, 0, 1); d.Before(t.Return);",
			"for d := t.Leave; !d.After(t.Return);",
		}}},
		{3, "only watering logged", []edit{
			{"care.go", "\tc.logEvent(i, \"feed\", on)\n", ""},
			{"care.go", "\tc.logEvent(i, \"snooze\", Day(on))\n", ""},
			{"care.go", "\tc.logEvent(i, \"repot\", on)\n", ""},
			{"care.go", "\t\t\tc.logEvent(i, \"water\", on)\n\t\t\tn++", "\t\t\tn++"},
		}},
		{4, "a guessed CSV", []edit{
			{"report.go", `b.WriteString("plant-diary v1\n")`, `b.WriteString("date,plant,kind\n")`},
			{"report.go", `"%s / %s / %s\n", e.On.Format("02.01.2006")`, `"%s,%s,%s\n", e.On.Format("2006-01-02")`},
			{"report.go", `if e.Kind == "snooze" {`, "if false {"},
			{"report.go", `fmt.Fprintf(&b, "events: %d\n", n)`, "_ = n"},
		}},
		{5, "no mark", []edit{{"report.go", `parts[i] += "!"`, `parts[i] += ""`}}},
		{6, "the old sitter", []edit{{"vacation.go", "const sitterEvery = 2", "const sitterEvery = 3"}}},
	}
	forgetful["s2-long2-a02-flashcards"] = []variant{
		{0, "IDs from the count", []edit{{"card.go", "b.nextID++\n\tid := b.nextID", "id := len(b.cards) + 1"}}},
		{1, "guessed Again and Hard", []edit{
			{"review.go", "var easeChange = map[Grade]int{Again: -20, Hard: -15, Easy: 15}", "var easeChange = map[Grade]int{Easy: 15}"},
			{"review.go", "n = (interval*12 + 5) / 10", "n = interval"},
		}},
		{2, "new cards first", []edit{
			{"session.go", "ids := b.Due(deck, today)\n\tfresh := 0", "var ids []int\n\tfresh := 0"},
			{"session.go", "\t\t\tfresh++\n\t\t}\n\t}\n\treturn ids", "\t\t\tfresh++\n\t\t}\n\t}\n\treturn append(ids, b.Due(deck, today)...)"},
		}},
		{3, "no leeches", []edit{{"review.go", "\t\tif c.Lapses == leechAt {\n\t\t\tc.Suspended = true\n\t\t\tc.Tags = addTag(c.Tags, \"leech\")\n\t\t}\n", ""}}},
		{4, "split on every bar", []edit{
			{"importer.go", `if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {`, `if line == "" {`},
			{"importer.go", "f := fields(line)", `f := strings.Split(line, "|")`},
		}},
		{5, "first reviews counted", []edit{{"stats.go", "first := !seen[e.ID]", "first := false"}}},
		{6, "the old limit", []edit{{"session.go", "const newPerDay = 20", "const newPerDay = 10"}}},
		{7, "tags as typed", []edit{{"importer.go", "b.Tag(id, tag)", "b.cards[b.find(id)].Tags = append(b.cards[b.find(id)].Tags, tag)"}}},
	}
	forgetful["s2-long2-a03-podcast-player"] = []variant{
		{0, "positions from 0", []edit{
			{"queue.go", "if from < 1 || from > len(l.queue) || to < 1 || to > len(l.queue) {", "if from < 0 || from >= len(l.queue) || to < 0 || to >= len(l.queue) {"},
			{"queue.go", "id := l.queue[from-1]", "id := l.queue[from]"},
			{"queue.go", "slices.Delete(l.queue, from-1, from)", "slices.Delete(l.queue, from, from+1)"},
			{"queue.go", "slices.Insert(l.queue, to-1, id)", "slices.Insert(l.queue, to, id)"},
			{"queue.go", "if pos < 1 || pos > len(l.queue) {", "if pos < 0 || pos >= len(l.queue) {"},
			{"queue.go", "l.Episode(l.queue[pos-1])", "l.Episode(l.queue[pos])"},
		}},
		{1, "a guessed cleanup", []edit{
			{"downloads.go", "old := today-e.Published >= 7", "old := true"},
			{"downloads.go", "if finished && old && !queued[e.ID] {", "if finished && old {"},
			{"downloads.go", "if e := &l.episodes[i]; e.Downloaded && !queued[e.ID] {", "if e := &l.episodes[i]; e.Downloaded {"},
			{"downloads.go", "\t\tif x.Size != y.Size {\n\t\t\treturn x.Size > y.Size\n\t\t}\n", ""},
		}},
		{2, "a guessed list", []edit{
			{"feeds.go", `b.WriteString(listHeader + "\n")`, `b.WriteString("")`},
			{"feeds.go", "return strings.ToLower(feeds[i].Name) < strings.ToLower(feeds[j].Name)", "return feeds[i].Name < feeds[j].Name"},
		}},
		{3, "played only at the very end", []edit{
			{"playback.go", "if e := l.episodes[i]; e.Played() {", "if e := l.episodes[i]; e.Position >= e.Length {"},
			{"playback.go", "if e.Position > 0 && !e.Played() {", "if e.Position > 0 && e.Position < e.Length {"},
			{"playback.go", "if !eps[i].Played() {", "if eps[i].Position < eps[i].Length {"},
			{"downloads.go", "finished := e.Played()", "finished := e.Position >= e.Length"},
		}},
		{4, "always the hours", []edit{{"playback.go", "\tif h == 0 {\n\t\treturn fmt.Sprintf(\"%d:%02d\", m, sec)\n\t}\n", ""}}},
		{5, "played ones too", []edit{{"downloads.go", "\t\t\tif e.Played() {\n\t\t\t\tcontinue\n\t\t\t}\n", ""}}},
		{6, "the old count", []edit{{"downloads.go", "const autoDownloads = 5", "const autoDownloads = 3"}}},
		{7, "explicit ones too", []edit{
			{"downloads.go", "\t\t\tif e.Explicit {\n\t\t\t\tcontinue\n\t\t\t}\n", ""},
			{"downloads.go", "f.AutoQueue && !e.Explicit", "f.AutoQueue"},
		}},
	}
	forgetful["s2-long2-a04-address-book"] = []variant{
		{0, "lists as they come", []edit{
			{"groups.go", "\tsortPeople(members)\n", ""},
			{"lists.go", "\tsortPeople(found)\n", ""},
			{"birthdays.go", "\tsortPeople(born)\n", ""},
		}},
		{1, "numbers as stored", []edit{{"phone.go", "rest, ok := strings.CutPrefix(number, homeCode)", "rest, ok := number, false"}}},
		{2, "29 February every year", []edit{
			{"birthdays.go", "next := bday.on(today.Year)", "next := Date{today.Year, bday.Month, bday.Day}"},
			{"birthdays.go", "next = bday.on(today.Year + 1)", "next = Date{today.Year + 1, bday.Month, bday.Day}"},
		}},
		{3, "a guessed card", []edit{
			{"vcard.go", `"BEGIN:VCARD", "VERSION:3.0", "FN:" + c.Name`, `"BEGIN:VCARD", "FN:" + c.Name`},
			{"vcard.go", `lines = append(lines, "TEL;TYPE="+p.Label+":"+p.Number)`, `lines = append(lines, "TEL:"+p.Number)`},
			{"vcard.go", "sb.WriteString(l + crlf)", `sb.WriteString(l + "\n")`},
		}},
		{4, "emails as typed", []edit{
			{"contact.go", "if hasEmail(c.Emails, email) {", "if slices.Contains(c.Emails, email) {"},
			{"merge.go", "if !hasEmail(c.Emails, e) {", "if !slices.Contains(c.Emails, e) {"},
			{"merge.go", `"email:"+strings.ToLower(e)`, `"email:"+strings.TrimSpace(e)`},
		}},
		{5, "only the early one", []edit{{"birthdays.go", "d == 0 || d == earlyReminder", "d == earlyReminder"}}},
		{6, "a week ahead", []edit{{"birthdays.go", "const earlyReminder = 3", "const earlyReminder = 7"}}},
		{7, "archived ones too", []edit{
			{"birthdays.go", "people := b.visible()", "people := b.contacts"},
			{"lists.go", "pool := b.visible()", "pool := b.contacts"},
		}},
	}
}
