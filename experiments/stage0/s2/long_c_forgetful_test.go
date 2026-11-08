package s2

func init() {
	forgetful["s2-long-c01-wal-store"] = []variant{
		{0, "internal keys like any other", []edit{
			{"store.go", "if reserved(key) {\n\t\treturn ErrReserved\n\t}\n\tif _, ok := s.data[key]; !ok {", "if _, ok := s.data[key]; !ok {"},
			{"store.go", "\t\tif reserved(op.Key) {\n\t\t\treturn ErrReserved\n\t\t}\n", ""},
			{"view.go", "strings.HasPrefix(k, prefix) && !reserved(k)", "strings.HasPrefix(k, prefix)"},
			{"view.go", "for k := range s.data {\n\t\tif !reserved(k) {\n\t\t\tst.Keys++\n\t\t}\n\t}", "st.Keys = len(s.data)"},
			{"view.go", "r.Seq > seq && !reserved(r.Key)", "r.Seq > seq"},
			{"view.go", "if !reserved(k) {\n\t\t\tkeys = append(keys, k)\n\t\t}", "keys = append(keys, k)"},
		}},
		{1, "any bad line is an error", []edit{
			{"recover.go", "if !newest {\n\t\t\t\treturn fmt.Errorf(\"%s line %d: %w\", name, i+1, ErrCorrupt)\n\t\t\t}\n\t\t\ts.log.disk.Replace(name, lines[:i])\n\t\t\treturn nil", "return fmt.Errorf(\"%s line %d: %w\", name, i+1, ErrCorrupt)"},
		}},
		{1, "any bad line stops quietly", []edit{
			{"recover.go", "if !newest {\n\t\t\t\treturn fmt.Errorf(\"%s line %d: %w\", name, i+1, ErrCorrupt)\n\t\t\t}\n", ""},
		}},
		{2, "the clock taken for seconds", []edit{
			{"compact.go", "const msPerSecond = 1000", "const msPerSecond = 1"},
		}},
		{3, "only Put checks the size", []edit{
			{"store.go", "\t\tif len(op.Value) > maxValueLen {\n\t\t\treturn ErrTooLarge\n\t\t}\n", ""},
		}},
		{4, "tabs left as they are", []edit{
			{"view.go", `strings.ReplaceAll(s.data[k], "\t", "\\t")`, "s.data[k]"},
		}},
		{5, "no check in what came later", []edit{
			{"store.go", "if s.closed {\n\t\treturn ErrClosed\n\t}\n\trecs := make", "recs := make"},
			{"view.go", "if s.closed {\n\t\treturn Report{}, ErrClosed\n\t}\n", ""},
			{"view.go", "if s.closed {\n\t\treturn Stats{}, ErrClosed\n\t}\n", ""},
			{"view.go", "Since(seq int64) ([]Record, error) {\n\tif s.closed {\n\t\treturn nil, ErrClosed\n\t}\n", "Since(seq int64) ([]Record, error) {\n"},
			{"view.go", "History(key string) ([]Record, error) {\n\tif s.closed {\n\t\treturn nil, ErrClosed\n\t}\n", "History(key string) ([]Record, error) {\n"},
			{"view.go", "if s.closed {\n\t\treturn \"\", ErrClosed\n\t}\n", ""},
			{"compact.go", "if s.closed {\n\t\treturn 0, ErrClosed\n\t}\n", ""},
			{"compact.go", "if s.closed {\n\t\treturn Plan{}, ErrClosed\n\t}\n", ""},
			{"recover.go", "if s.closed {\n\t\treturn \"\", ErrClosed\n\t}\n", ""},
			{"recover.go", "if s.closed {\n\t\treturn nil, ErrClosed\n\t}\n", ""},
			{"recover.go", "if s.closed {\n\t\treturn 0, ErrClosed\n\t}\n", ""},
		}},
		{6, "the newest segment merged too", []edit{
			{"compact.go", "return names[:len(names)-1]", "return names"},
		}},
		{6, "one sealed segment is enough", []edit{
			{"compact.go", "if len(names) < 3 {", "if len(names) < 2 {"},
		}},
		{6, "pruned down to nothing", []edit{
			{"recover.go", "\tkeep = max(keep, 1)\n", ""},
		}},
		{7, "del records dropped at once", []edit{
			{"compact.go", "\tnow := s.clock()\n\tvar kept", "\tvar kept"},
			{"compact.go", "} else if r.Op == OpDel && now-r.At >= tombstoneGrace {", "} else if r.Op == OpDel {"},
		}},
		{8, "names as they were given", []edit{
			{"compact.go", `"walstore_records_dropped"`, `"records_dropped"`},
			{"recover.go", `"walstore_snapshots_taken"`, `"snapshots_taken"`},
			{"recover.go", `"walstore_snapshots_pruned"`, `"snapshots_pruned"`},
		}},
		{9, "the old retention", []edit{
			{"compact.go", "6 * 3600 * msPerSecond", "1 * 3600 * msPerSecond"},
		}},
	}
	forgetful["s2-long-c02-lease-manager"] = []variant{
		{0, "TTLs taken as asked", []edit{
			{"manager.go", "l.Expires = m.clock().Add(clampTTL(ttl))", "l.Expires = m.clock().Add(ttl)"},
			{"waiters.go", "waiter{owner: owner, ttl: clampTTL(ttl)}", "waiter{owner: owner, ttl: ttl}"},
			{"ownership.go", "m.grant(name, owner, clampTTL(ttl))", "m.grant(name, owner, ttl)"},
		}},
		{1, "owner names as given", []edit{
			{"waiters.go", "Enqueue(name, owner string, ttl time.Duration) (int, error) {\n\towner = normalOwner(owner)\n", "Enqueue(name, owner string, ttl time.Duration) (int, error) {\n"},
			{"waiters.go", "Cancel(name, owner string) error {\n\towner = normalOwner(owner)\n", "Cancel(name, owner string) error {\n"},
			{"waiters.go", "Position(name, owner string) int {\n\towner = normalOwner(owner)\n", "Position(name, owner string) int {\n"},
			{"ownership.go", "Transfer(name string, token int64, owner string) (Lease, error) {\n\towner = normalOwner(owner)\n", "Transfer(name string, token int64, owner string) (Lease, error) {\n"},
			{"ownership.go", "Preempt(name, owner string, ttl time.Duration) (Lease, error) {\n\towner = normalOwner(owner)\n", "Preempt(name, owner string, ttl time.Duration) (Lease, error) {\n"},
			{"ownership.go", "AcquireAll(names []string, owner string, ttl time.Duration) ([]Lease, error) {\n\towner = normalOwner(owner)\n", "AcquireAll(names []string, owner string, ttl time.Duration) ([]Lease, error) {\n"},
			{"report.go", "OwnerLeases(owner string) []Lease {\n\towner = normalOwner(owner)\n", "OwnerLeases(owner string) []Lease {\n"},
			{"report.go", "m.held(normalOwner(owner))", "m.held(owner)"},
		}},
		{2, "a renewal draws a new token", []edit{
			{"manager.go", "\tm.record(\"renew\", name, l.Owner)\n", "\tl.Token = m.next\n\tm.next++\n\tm.highest[name] = l.Token\n\tm.record(\"renew\", name, l.Owner)\n"},
		}},
		{2, "a transfer keeps the token", []edit{
			{"ownership.go", "\tl.Token = m.next\n\tm.next++\n\tm.highest[name] = l.Token\n", ""},
		}},
		{2, "a preemption keeps the token", []edit{
			{"ownership.go", "\tm.record(\"preempt\", name, owner)\n", "\tl.Token = m.next - 2\n\tm.leases[name].Token = l.Token\n\tm.record(\"preempt\", name, owner)\n"},
		}},
		{3, "still held at the instant of expiry", []edit{
			{"manager.go", "if !ok || !m.clock().Before(l.Expires) {\n\t\treturn ErrNotHeld", "if !ok || m.clock().After(l.Expires) {\n\t\treturn ErrNotHeld"},
			{"manager.go", "if !ok || !m.clock().Before(l.Expires) {\n\t\treturn Lease{}, ErrNotHeld", "if !ok || m.clock().After(l.Expires) {\n\t\treturn Lease{}, ErrNotHeld"},
			{"ownership.go", "if !ok || !m.clock().Before(l.Expires) {\n\t\treturn Lease{}, ErrNotHeld", "if !ok || m.clock().After(l.Expires) {\n\t\treturn Lease{}, ErrNotHeld"},
			{"report.go", "if now.Before(l.Expires) {", "if !now.After(l.Expires) {"},
			{"snapshot.go", "now.Before(l.Expires) {", "!now.After(l.Expires) {"},
		}},
		{4, "listed by name", []edit{
			{"report.go", "\t\tif !out[a].Expires.Equal(out[b].Expires) {\n\t\t\treturn out[a].Expires.Before(out[b].Expires)\n\t\t}\n", ""},
		}},
		{5, "fresh errors", []edit{
			{"manager.go", "return Lease{}, ErrStaleToken", `return Lease{}, errors.New("wrong token")`},
			{"ownership.go", "import \"time\"", "import (\n\t\"errors\"\n\t\"time\"\n)"},
			{"ownership.go", "return Lease{}, ErrStaleToken", `return Lease{}, errors.New("wrong token")`},
			{"ownership.go", "if token != high {\n\t\treturn ErrStaleToken", "if token != high {\n\t\treturn errors.New(\"wrong token\")"},
		}},
		{6, "not journaled", []edit{
			{"ownership.go", "\tm.record(\"transfer\", name, owner)\n", ""},
			{"ownership.go", "\tm.record(\"preempt\", name, owner)\n", ""},
		}},
		{7, "owner names in full", []edit{
			{"journal.go", "maskOwner(e.Owner)", "e.Owner"},
			{"report.go", "maskOwner(l.Owner)", "l.Owner"},
			{"report.go", "maskOwner(r.Owner)", "r.Owner"},
		}},
		{8, "only Acquire checks the limit", []edit{
			{"ownership.go", "\tif m.held(owner) >= maxPerOwner {\n\t\treturn Lease{}, ErrOwnerLimit\n\t}\n\tl.Owner = owner", "\tl.Owner = owner"},
			{"ownership.go", "\tif m.held(owner)+len(names) > maxPerOwner {\n\t\treturn nil, ErrOwnerLimit\n\t}\n", ""},
			{"ownership.go", "\tif m.held(owner) >= maxPerOwner {\n\t\treturn Lease{}, ErrOwnerLimit\n\t}\n\tl := m.grant", "\tl := m.grant"},
		}},
		{9, "the old cap", []edit{
			{"policy.go", "const maxPerOwner = 5", "const maxPerOwner = 3"},
		}},
	}
	forgetful["s2-long-c03-alert-pipeline"] = []variant{
		{0, "any sample counts", []edit{
			{"evaluate.go", "if ok && now-s.At <= staleAfter {", "if ok {"},
			{"report.go", "\tnow := e.clock()\n\tvar out []string\n\tfor _, r := range e.Rules() {", "\tvar out []string\n\tfor _, r := range e.Rules() {"},
			{"report.go", "if ok && now-s.At <= staleAfter && r.Breached(s.Value) {", "if ok && r.Breached(s.Value) {"},
			{"report.go", "s, ok := e.samples.Latest(r.Metric, instance)\n\tif !ok || e.clock()-s.At > staleAfter {", "_, ok = e.samples.Latest(r.Metric, instance)\n\tif !ok {"},
			{"report.go", "e.samples.Trim(e.clock() - staleAfter)", "e.samples.Trim(e.clock() - 3600)"},
		}},
		{1, "fresh errors", []edit{
			{"engine.go", "var ErrUnknownRule = errors.New(\"unknown rule\")", "var ErrUnknownRule = errors.New(\"unknown rule\")\n\nvar errNoRule = errors.New(\"no rule by that name\")"},
			{"engine.go", "DisableRule(name string) error {\n\tif _, ok := e.rules[name]; !ok {\n\t\treturn ErrUnknownRule", "DisableRule(name string) error {\n\tif _, ok := e.rules[name]; !ok {\n\t\treturn errNoRule"},
			{"engine.go", "EnableRule(name string) error {\n\tif _, ok := e.rules[name]; !ok {\n\t\treturn ErrUnknownRule", "EnableRule(name string) error {\n\tif _, ok := e.rules[name]; !ok {\n\t\treturn errNoRule"},
			{"silence.go", "\tif !strings.HasSuffix(s.Rule, \"*\") {\n\t\tif _, ok := e.rules[s.Rule]; !ok {\n\t\t\treturn 0, ErrUnknownRule\n\t\t}\n\t}\n", ""},
			{"evaluate.go", "return ErrUnknownRule", "return errNoRule"},
			{"report.go", "return \"\", ErrUnknownRule", "return \"\", errNoRule"},
		}},
		{2, "everything to the team's chat", []edit{
			{"notify.go", "team := r.Team\n\tif team == \"\" {\n\t\tteam = \"oncall\"\n\t}\n\tswitch {\n\tcase r.Severity == \"info\":\n\t\treturn nil\n\tcase kind == \"resolved\" || r.Severity == \"warning\":\n\t\treturn []string{\"chat-\" + team}\n\t}\n\treturn []string{\"pager-\" + team, \"chat-\" + team}", "return []string{\"chat-\" + r.Team}"},
		}},
		{2, "resolved goes where firing went", []edit{
			{"notify.go", "case kind == \"resolved\" || r.Severity == \"warning\":", "case r.Severity == \"warning\":"},
		}},
		{3, "a bare number taken for seconds", []edit{
			{"silence.go", "d, err := parseDuration(value)", "d, err := parseDuration(value + \"s\")"},
		}},
		{4, "RFC 3339", []edit{
			{"report.go", `Format("02 Jan 15:04 UTC")`, "Format(time.RFC3339)"},
		}},
		{5, "by rule name alone", []edit{
			{"report.go", "\t\tif severityRank[a.Severity] != severityRank[b.Severity] {\n\t\t\treturn severityRank[a.Severity] < severityRank[b.Severity]\n\t\t}\n", ""},
		}},
		{6, "pager texts left whole", []edit{
			{"notify.go", "\t\tif strings.HasPrefix(channel, \"pager-\") {\n\t\t\ttext = cutForPager(text)\n\t\t}\n", ""},
		}},
		{7, "every evaluation notifies", []edit{
			{"notify.go", "\tif a.notified && !a.held && now-a.lastSent < repeatEvery(r.Severity) {\n\t\treturn\n\t}\n", ""},
		}},
		{8, "a silence stops the alert too", []edit{
			{"evaluate.go", "e.step(r, instance, s.Value, now)", "if !e.silenced(r.Name, instance, now) {\n\t\t\t\t\te.step(r, instance, s.Value, now)\n\t\t\t\t}"},
		}},
		{8, "the end of a silence waits its turn", []edit{
			{"notify.go", "a.notified && !a.held && now-a.lastSent", "a.notified && now-a.lastSent"},
		}},
		{8, "a resolved notice for everybody", []edit{
			{"evaluate.go", "if a.State == StateFiring && a.notified && !e.silenced(r.Name, a.Instance, now) {", "if a.State == StateFiring && !e.silenced(r.Name, a.Instance, now) {"},
		}},
		{9, "one interval for all", []edit{
			{"notify.go", "if severity == \"critical\" {\n\t\treturn 10 * 60\n\t}\n\treturn 60 * 60", "return 30 * 60"},
		}},
	}
	forgetful["s2-long-c04-process-supervisor"] = []variant{
		{0, "only Get knows the error", []edit{
			{"supervisor.go", "return nil, ErrUnknownUnit", "return nil, errors.New(\"no such unit\")"},
		}},
		{1, "any name can be cloned", []edit{
			{"supervisor.go", "if !validName(newName) {\n\t\treturn ErrBadUnit\n\t}\n\t", ""},
		}},
		{2, "every bad exit is a crash", []edit{
			{"restart.go", "case code == exitTerminated:\n\t\te.state = StateStopped\n\tcase code == exitBadConfig:\n\t\ts.fail(e)\n\t", ""},
		}},
		{3, "a flat delay", []edit{
			{"restart.go", "if streak > 10 {\n\t\treturn maxBackoff\n\t}\n\treturn min(firstBackoff<<streak, maxBackoff)", "return firstBackoff"},
		}},
		{4, "stop in start order", []edit{
			{"stop.go", "import \"slices\"\n\n", ""},
			{"stop.go", "\tslices.Reverse(order)\n", ""},
			{"restart.go", "\t\"slices\"\n", ""},
			{"restart.go", "\tslices.Reverse(dependents)\n", ""},
		}},
		{5, "durations as Go prints them", []edit{
			{"report.go", "total := int64(d / time.Second)\n\treturn fmt.Sprintf(\"%02d:%02d:%02d\", total/3600, total/60%60, total%60)", "return d.String()"},
		}},
		{6, "secrets in the clear", []edit{
			{"report.go", "if strings.HasSuffix(key, \"_TOKEN\") || strings.HasSuffix(key, \"_SECRET\") {\n\t\t\tvalue = \"<hidden>\"\n\t\t}\n\t\t", ""},
		}},
		{7, "every crash ever counted", []edit{
			{"report.go", "return recentCrashes(e, s.clock()), nil", "return len(e.crashes), nil"},
			{"report.go", "clockFormat(s.uptime(e)), recentCrashes(e, s.clock()))", "clockFormat(s.uptime(e)), len(e.crashes))"},
		}},
		{8, "the old cap", []edit{
			{"restart.go", "maxBackoff   = 60 * time.Second", "maxBackoff   = 32 * time.Second"},
		}},
	}
}
