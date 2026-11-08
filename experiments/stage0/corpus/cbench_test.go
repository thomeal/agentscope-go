package corpus

import (
	"slices"
	"strings"
	"testing"
)

func TestCBenchPilotValid(t *testing.T) {
	items := cbenches(t)
	if len(items) != 10 {
		t.Fatalf("CBenchPilot has %d items", len(items))
	}
	for _, c := range items {
		if c.Set != SetTuning {
			t.Errorf("%s has set %q", c.ID, c.Set)
		}
		reportProblems(t, c.ID, validateCBench(c))
	}
}

func TestCBenchAcceptanceValid(t *testing.T) {
	for _, c := range CBenchAcceptance(5) {
		if c.Set != SetAcceptance {
			t.Errorf("%s has set %q", c.ID, c.Set)
		}
		reportProblems(t, c.ID, validateCBench(c))
	}
}

func TestCBenchManySeedsValid(t *testing.T) {
	for seed := range int64(25) {
		c := GenerateCBench(seed*104729+3, SetTuning)
		reportProblems(t, c.ID, validateCBench(c))
	}
}

func TestCBenchRead(t *testing.T) {
	c := cbenches(t)[0]
	for _, st := range c.History {
		if st.RecordID == "" {
			continue
		}
		if got, ok := c.Read(SideCloud, st.RecordID); !ok || got != st.ToolResult {
			t.Errorf("Read(cloud, %s) = %v", st.RecordID, ok)
		}
		if _, ok := c.Read(SideLocal, st.RecordID); ok {
			t.Errorf("Read(local, %s) crossed sides", st.RecordID)
		}
	}
	for _, st := range c.LocalOwn {
		if st.RecordID == "" {
			continue
		}
		if got, ok := c.Read(SideLocal, st.RecordID); !ok || got != st.ToolResult {
			t.Errorf("Read(local, %s) = %v", st.RecordID, ok)
		}
		if _, ok := c.Read(SideCloud, st.RecordID); ok {
			t.Errorf("Read(cloud, %s) crossed sides", st.RecordID)
		}
	}
	if _, ok := c.Read(SideCloud, ""); ok {
		t.Error("an empty id read successfully")
	}
	if _, ok := c.Read(SideLocal, "L-0"); ok {
		t.Error("an unknown id read successfully")
	}
}

func TestCBenchValidatorCatchesCorruption(t *testing.T) {
	firstOf := func(c CBench, cat Category) Fact {
		ids := make([]string, 0, len(c.Facts))
		for id := range c.Facts {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if c.Facts[id].Category == cat {
				return c.Facts[id]
			}
		}
		t.Fatalf("no %s fact", cat)
		return Fact{}
	}
	for _, tc := range []struct {
		name, tag string
		mutate    func(c *CBench)
	}{
		{"history far too small", "cbench-size", func(c *CBench) { c.History = c.History[:10] }},
		{"local history too big", "cbench-size", func(c *CBench) {
			c.LocalOwn[0].User += strings.Repeat("x", 8000)
		}},
		{"F3 code in a user message", "f3-leak", func(c *CBench) {
			c.History[0].User += " " + firstOf(*c, F3).Value
		}},
		{"F3 code in a canned reply", "f3-leak", func(c *CBench) {
			c.History[0].Reply += " " + firstOf(*c, F3).Value
		}},
		{"F3 facts bunched at the start", "cbench-spread", func(c *CBench) {
			for id, f := range c.Facts {
				if f.Category == F3 {
					f.IntroducedTurn = 0
					c.Facts[id] = f
				}
			}
		}},
		{"too few F3 facts", "cbench-spread", func(c *CBench) {
			n := 0
			for id, f := range c.Facts {
				if f.Category == F3 {
					if n++; n > 39 {
						delete(c.Facts, id)
					}
				}
			}
		}},
		{"canary in cloud history", "canary", func(c *CBench) {
			c.History[0].ToolResult += c.Canaries[0] + "\n"
		}},
		{"local code not a canary", "canary", func(c *CBench) {
			c.Canaries = c.Canaries[1:]
		}},
		{"answer in a prompt", "batch-leak", func(c *CBench) {
			c.Probes[0].Prompt += firstOf(*c, F3).Value
		}},
		{"batch without F6", "batch-f6", func(c *CBench) {
			b := &c.Probes[0]
			b.Questions = slices.DeleteFunc(b.Questions, func(q Question) bool { return q.Category == F6 })
		}},
		{"cloud recipient", "probe-schedule", func(c *CBench) { c.Probes[0].Recipient = SideCloud }},
		{"a fact never asked", "batch-mix", func(c *CBench) { c.Probes = c.Probes[1:] }},
		{"F6 label in a record", "f6-leak", func(c *CBench) {
			f := firstOf(*c, F6)
			c.History[0].ToolResult += refLine(f.Kind, f.Subject, "K7P-2941") + "\n"
		}},
		{"tool step without a tool name", "cbench-step", func(c *CBench) { c.History[0].ToolName = "" }},
		{"wrong tool name", "cbench-step", func(c *CBench) { c.History[0].ToolName = "read_local" }},
		{"F2 fact the rule does not give", "f2", func(c *CBench) {
			f := firstOf(*c, F2)
			f.Value = "K7P-2941"
			c.Facts[f.ID] = f
		}},
		{"decisions bunched at the start", "cbench-spread", func(c *CBench) {
			for id, f := range c.Facts {
				if f.Category == F2 {
					f.IntroducedTurn = 1
					c.Facts[id] = f
				}
			}
		}},
		{"too few distractors", "distractors", func(c *CBench) {
			for i := range c.History {
				keep := []string{}
				for _, line := range strings.Split(c.History[i].ToolResult, "\n") {
					own := false
					for _, f := range c.Facts {
						own = own || strings.Contains(line, f.Value)
					}
					if own || len(Codes(line)) == 0 {
						keep = append(keep, line)
					}
				}
				c.History[i].ToolResult = strings.Join(keep, "\n")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := GenerateCBench(cbenchPilotSeedBase, SetTuning)
			if p := validateCBench(c); len(p) != 0 {
				t.Fatalf("the base item is already invalid: %v", p)
			}
			tc.mutate(&c)
			if p := validateCBench(c); !p.has(tc.tag) {
				t.Errorf("no %q problem; got %v", tc.tag, p)
			}
		})
	}
}
