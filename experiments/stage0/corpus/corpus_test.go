package corpus

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Generating is cheap but not free, so read-only tests share one copy.
var (
	sharedOnce   sync.Once
	sharedPilot  []Session
	sharedCBench []CBench
)

func pilot(t *testing.T) []Session {
	t.Helper()
	sharedOnce.Do(func() { sharedPilot, sharedCBench = Pilot(), CBenchPilot() })
	return sharedPilot
}

func cbenches(t *testing.T) []CBench {
	t.Helper()
	sharedOnce.Do(func() { sharedPilot, sharedCBench = Pilot(), CBenchPilot() })
	return sharedCBench
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func reportProblems(t *testing.T, id string, p problems) {
	t.Helper()
	const shown = 15
	for i, s := range p {
		if i == shown {
			t.Errorf("%s: and %d more", id, len(p)-shown)
			break
		}
		t.Errorf("%s: %s", id, s)
	}
}

func TestDeterminism(t *testing.T) {
	t.Run("session", func(t *testing.T) {
		for _, length := range []int{20, 50} {
			a, b := mustJSON(t, Generate(42, length, SetTuning)), mustJSON(t, Generate(42, length, SetTuning))
			if a != b {
				t.Errorf("length %d: two runs differ", length)
			}
		}
	})
	t.Run("different seeds differ", func(t *testing.T) {
		a, b := Generate(1, 20, SetTuning), Generate(2, 20, SetTuning)
		if reflect.DeepEqual(a.Turns, b.Turns) || reflect.DeepEqual(a.Records, b.Records) {
			t.Error("seeds 1 and 2 generated the same session")
		}
	})
	t.Run("pilot", func(t *testing.T) {
		if mustJSON(t, Pilot()) != mustJSON(t, Pilot()) {
			t.Error("two Pilot runs differ")
		}
	})
	t.Run("cbench", func(t *testing.T) {
		if mustJSON(t, GenerateCBench(9, SetTuning)) != mustJSON(t, GenerateCBench(9, SetTuning)) {
			t.Error("two GenerateCBench runs differ")
		}
		if Hash() != Hash() {
			t.Error("two Hash runs differ")
		}
	})
}

// TestHashPinned fails on any change to the generated corpus, intended or not.
// The corpus is meant to stay blind to the implementation, so a changed hash is
// something to explain in the experiment record, not just to update here.
func TestHashPinned(t *testing.T) {
	const (
		wantPilot  = "8753d4322d4e2229e02c65e99fe4be17132777901c4c8edae2962cd48b186729"
		wantCBench = "d288a81984bcb4f9f1d12305957856458f875be3131f395f2708fbe38fa62604"
		wantAll    = "bafc910e91ef96d98cc6cc2dd584f76ddd4bb997dd604be38d3143df9c7834e2"
	)
	if got := PilotHash(); got != wantPilot {
		t.Errorf("PilotHash = %s, want %s", got, wantPilot)
	}
	if got := CBenchHash(); got != wantCBench {
		t.Errorf("CBenchHash = %s, want %s", got, wantCBench)
	}
	if got := Hash(); got != wantAll {
		t.Errorf("Hash = %s, want %s", got, wantAll)
	}
}

func TestPilotAndAcceptanceComposition(t *testing.T) {
	sessions := pilot(t)
	if len(sessions) != 30 {
		t.Fatalf("Pilot has %d sessions", len(sessions))
	}
	count := map[int]int{}
	for _, s := range sessions {
		count[s.Length]++
		if s.Set != SetTuning {
			t.Errorf("%s has set %q", s.ID, s.Set)
		}
	}
	if count[20] != 20 || count[50] != 10 {
		t.Errorf("Pilot mix is %v", count)
	}
	// The harness prepends the session marker, so the system prompt must not hold
	// one of its own.
	for _, s := range sessions {
		if s.System != sessions[0].System || s.System == "" {
			t.Errorf("%s has a different system prompt", s.ID)
		}
		if strings.Contains(s.System, s.ID) || strings.Contains(s.System, fmt.Sprint(s.Seed)) || len(Codes(s.System)) > 0 {
			t.Errorf("%s: system prompt holds a session marker: %q", s.ID, s.System)
		}
	}

	all := Acceptance(30)
	count = map[int]int{}
	for _, s := range all {
		count[s.Length]++
		if s.Set != SetAcceptance {
			t.Errorf("%s has set %q", s.ID, s.Set)
		}
	}
	if count[20] != 20 || count[50] != 10 {
		t.Errorf("Acceptance mix is %v", count)
	}
	if got := Acceptance(0); len(got) != 0 {
		t.Errorf("Acceptance(0) has %d sessions", len(got))
	}
}

func TestAcceptanceStablePrefix(t *testing.T) {
	long := Acceptance(12)
	for _, j := range []int{1, 2, 3, 7, 12} {
		if mustJSON(t, Acceptance(j)) != mustJSON(t, long[:j]) {
			t.Errorf("Acceptance(12)[:%d] differs from Acceptance(%d)", j, j)
		}
	}
	benches := CBenchAcceptance(4)
	if mustJSON(t, CBenchAcceptance(2)) != mustJSON(t, benches[:2]) {
		t.Error("CBenchAcceptance(4)[:2] differs from CBenchAcceptance(2)")
	}
}

func TestSeedSetsDisjoint(t *testing.T) {
	seen := map[int64]string{}
	note := func(seed int64, what string) {
		t.Helper()
		if other, dup := seen[seed]; dup {
			t.Errorf("seed %d is used by %s and %s", seed, other, what)
		}
		seen[seed] = what
	}
	for _, s := range pilot(t) {
		note(s.Seed, "pilot "+s.ID)
	}
	for _, s := range Acceptance(100) {
		note(s.Seed, "acceptance "+s.ID)
	}
	for _, c := range cbenches(t) {
		note(c.Seed, "cbench pilot "+c.ID)
	}
	for _, c := range CBenchAcceptance(20) {
		note(c.Seed, "cbench acceptance "+c.ID)
	}
}

func TestPilotSessionsValid(t *testing.T) {
	for _, s := range pilot(t) {
		reportProblems(t, s.ID, validateSession(s))
	}
}

func TestAcceptanceSessionsValid(t *testing.T) {
	for _, s := range Acceptance(30) {
		reportProblems(t, s.ID, validateSession(s))
	}
}

// TestManySeedsValid looks for rare layouts that fixed seeds might miss.
func TestManySeedsValid(t *testing.T) {
	for seed := range int64(60) {
		for _, length := range []int{20, 50} {
			s := Generate(seed*7919+1, length, SetTuning)
			reportProblems(t, s.ID, validateSession(s))
		}
	}
}

// TestOtherLengthsValid covers Generate's whole supported range, including the
// lengths where the layout formula steps up.
func TestOtherLengthsValid(t *testing.T) {
	for _, length := range []int{MinLength, 21, 25, 29, 30, 34, 35, 44, 45, 49, 51, 69, 70, 74, 75, 80, 95, MaxLength} {
		for seed := range int64(4) {
			s := Generate(seed+1000, length, SetTuning)
			reportProblems(t, s.ID, validateSession(s))
		}
	}
}

func TestGenerateRejectsBadArguments(t *testing.T) {
	for name, call := range map[string]func(){
		"short":     func() { Generate(1, MinLength-1, SetTuning) },
		"long":      func() { Generate(1, MaxLength+1, SetTuning) },
		"set":       func() { Generate(1, 20, "other") },
		"cbench":    func() { GenerateCBench(1, "other") },
		"empty set": func() { Generate(1, 20, "") },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic")
				}
			}()
			call()
		})
	}
}

func TestRecordToolAndRead(t *testing.T) {
	if name, desc := RecordTool(SideCloud); name != "read_record" || !strings.Contains(desc, `"id"`) {
		t.Errorf("cloud tool is %q: %q", name, desc)
	}
	if name, desc := RecordTool(SideLocal); name != "read_local" || !strings.Contains(desc, `"id"`) {
		t.Errorf("local tool is %q: %q", name, desc)
	}

	s := pilot(t)[0]
	sawCloud, sawLocal := false, false
	for id, r := range s.Records {
		other := SideLocal
		if r.Side == SideLocal {
			other = SideCloud
			sawLocal = true
		} else {
			sawCloud = true
		}
		if got, ok := s.Read(r.Side, id); !ok || got != r.Text {
			t.Errorf("Read(%s, %s) = %v", r.Side, id, ok)
		}
		if _, ok := s.Read(other, id); ok {
			t.Errorf("Read(%s, %s) crossed sides", other, id)
		}
	}
	if !sawCloud || !sawLocal {
		t.Errorf("session has cloud records %v, local records %v", sawCloud, sawLocal)
	}
	if _, ok := s.Read(SideCloud, "R-0"); ok {
		t.Error("an unknown id read successfully")
	}
}

// TestLocalTurnVariety checks the turn mix the workload describes, over the pilot.
func TestLocalTurnVariety(t *testing.T) {
	reads, decisions, plain, edge, local := 0, 0, 0, 0, 0
	for _, s := range pilot(t) {
		for _, tn := range s.Turns {
			if tn.Owner != SideLocal {
				continue
			}
			local++
			if tn.Disclosure == EdgeOnly {
				edge++
			}
			switch {
			case tn.ReadRecord != "":
				reads++
			case strings.Contains(tn.User, "Rule: choose"):
				decisions++
			default:
				plain++
			}
		}
	}
	if reads == 0 || decisions == 0 || plain == 0 {
		t.Errorf("local turns: %d reads, %d decisions, %d plain", reads, decisions, plain)
	}
	if ratio := float64(edge) / float64(local); ratio < 0.28 || ratio > 0.40 {
		t.Errorf("edge-only ratio %.3f over %d local turns, want about a third", ratio, local)
	}
}

// TestDecisionsRequireTheRule checks the chosen code's position in the list gives
// no clue, so an assistant cannot do well by always taking one position.
func TestDecisionsRequireTheRule(t *testing.T) {
	total, first := 0, 0
	for _, s := range pilot(t) {
		for _, f := range s.Facts {
			if f.Category != F2 && f.Where != "edge_reply" {
				continue
			}
			cands := candidateRe.FindAllStringSubmatch(s.Turns[f.IntroducedTurn].User, -1)
			total++
			if cands[0][1] == f.Value {
				first++
			}
		}
	}
	if total < 50 {
		t.Fatalf("only %d decisions in the pilot", total)
	}
	if share := float64(first) / float64(total); share > 0.45 {
		t.Errorf("the first candidate is right in %.0f%% of %d decisions", share*100, total)
	}
}

// TestFactPositionsSpread checks facts sit throughout their records, so keeping
// only the head or tail of a record gets no built-in advantage.
func TestFactPositionsSpread(t *testing.T) {
	thirds := [3]int{}
	for _, s := range pilot(t) {
		for _, f := range s.Facts {
			if f.Category != F3 {
				continue
			}
			text := s.Records[s.Turns[f.IntroducedTurn].ReadRecord].Text
			thirds[min(strings.Index(text, f.Value)*3/len(text), 2)]++
		}
	}
	total := thirds[0] + thirds[1] + thirds[2]
	for i, n := range thirds {
		if share := float64(n) / float64(total); share < 0.25 || share > 0.42 {
			t.Errorf("third %d of records holds %.0f%% of %d F3 facts", i, share*100, total)
		}
	}
}

// TestF6LooksLikeOtherQuestions checks F6 uses the same forms as real questions
// and is not given away by its position or its share of a batch.
func TestF6LooksLikeOtherQuestions(t *testing.T) {
	formsF6, formsOther := map[int]int{}, map[int]int{}
	lastPosition, total := 0, 0
	for _, s := range pilot(t) {
		for _, b := range s.Probes {
			for i, q := range b.Questions {
				form := slices.IndexFunc(questionForms, func(re *regexp.Regexp) bool {
					return re.MatchString(q.Text)
				})
				if q.Category == F6 {
					formsF6[form]++
					total++
					if i == len(b.Questions)-1 && len(b.Questions) > 1 {
						lastPosition++
					}
				} else {
					formsOther[form]++
				}
			}
		}
	}
	for form := range formsOther {
		if formsF6[form] == 0 {
			t.Errorf("no F6 question uses form %d", form)
		}
	}
	if share := float64(lastPosition) / float64(total); share > 0.3 {
		t.Errorf("F6 is the last question in %.0f%% of batches", share*100)
	}
}

// TestVocabulary guards the label check: if one kind ended another, or one subject
// began another, "<kind> for the <subject>" could match the wrong label.
func TestVocabulary(t *testing.T) {
	check := func(name string, words []string, related func(a, b string) bool) {
		seen := map[string]bool{}
		for _, a := range words {
			if seen[a] {
				t.Errorf("%s: %q repeats", name, a)
			}
			seen[a] = true
			for _, b := range words {
				if a != b && related(a, b) {
					t.Errorf("%s: %q overlaps %q", name, a, b)
				}
			}
		}
	}
	check("kinds", kinds, func(a, b string) bool { return strings.HasSuffix(b, " "+a) })
	check("subjects", subjects, func(a, b string) bool { return strings.HasPrefix(b, a+" ") })
	if len(kinds)*len(subjects) < 3000 {
		t.Errorf("only %d labels", len(kinds)*len(subjects))
	}
	for _, f := range fillers {
		r := newPRNG(1, 1)
		line := f(r, "cache tier")
		if len(Codes(line)) > 0 || !isASCII(line) {
			t.Errorf("bad filler line %q", line)
		}
	}
}

// TestSharedHelpers pins the few production helpers the validators rely on, with
// literal values, so a bug in one cannot quietly weaken every check built on it.
func TestSharedHelpers(t *testing.T) {
	for text, want := range map[string][]string{
		"K7P-2941 and 9WC-0374.":       {"K7P-2941", "9WC-0374"},
		"(ABC-1234), DEF-5678: ok":     {"ABC-1234", "DEF-5678"},
		"record R-417, local L-283":    nil,
		"node-1234, call_0001":         nil,
		"ABCD-1234 is too long":        nil,
		"ABC-12345 has too many":       nil,
		"AB-1234 is too short":         nil,
		"abc-1234 is lower case":       nil,
		"12:30:45 ran for 2941 ms":     nil,
		"<code or UNKNOWN> is no code": nil,
	} {
		if got := Codes(text); !slices.Equal(got, want) {
			t.Errorf("Codes(%q) = %v, want %v", text, got, want)
		}
	}
	for text, want := range map[string]int{"": 0, "a": 1, "abcd": 1, "abcde": 2, strings.Repeat("x", 4000): 1000} {
		if got := EstimateTokens(text); got != want {
			t.Errorf("EstimateTokens of %d characters = %d, want %d", len(text), got, want)
		}
	}
	if got, want := refLine("rollback token", "payments service", "K7P-2941"), "Rollback token for the payments service: K7P-2941"; got != want {
		t.Errorf("refLine = %q, want %q", got, want)
	}
	if got := article("escalation bridge"); got != "an" {
		t.Errorf("article(escalation bridge) = %q", got)
	}
	if got := article("rollback token"); got != "a" {
		t.Errorf("article(rollback token) = %q", got)
	}
	// One word, then digits, then punctuation, charged at their pessimistic rates.
	if got, want := pessimisticTokens("abcdef 1234 -"), 2+2+1; got != want {
		t.Errorf("pessimisticTokens = %d, want %d", got, want)
	}
}

// TestValidatorCatchesCorruption shows each check can fail. Every mutation breaks
// one promise on a known-good session, and the matching tag must appear.
func TestValidatorCatchesCorruption(t *testing.T) {
	firstFact := func(s Session, keep func(Fact) bool) Fact {
		ids := make([]string, 0, len(s.Facts))
		for id := range s.Facts {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if keep(s.Facts[id]) {
				return s.Facts[id]
			}
		}
		t.Fatal("no matching fact")
		return Fact{}
	}
	isCat := func(c Category) func(Fact) bool {
		return func(f Fact) bool { return f.Category == c }
	}
	for _, tc := range []struct {
		name, tag string
		mutate    func(s *Session)
	}{
		{"F3 code in a user message", "f3-leak", func(s *Session) {
			s.Turns[0].User += " " + firstFact(*s, isCat(F3)).Value
		}},
		{"F3 code in an expected reply", "f3-leak", func(s *Session) {
			s.Turns[1].ExpectedReply += " " + firstFact(*s, isCat(F3)).Value
		}},
		{"answer in a prompt", "batch-leak", func(s *Session) {
			b := &s.Probes[len(s.Probes)-1]
			b.Prompt += s.Facts[b.Questions[0].FactID].Kind + " K7P-2941"
		}},
		{"answer in question text", "batch-leak", func(s *Session) {
			q := &s.Probes[len(s.Probes)-1].Questions[0]
			q.Text += " (K7P-2941)"
		}},
		{"F6 label in a record", "f6-leak", func(s *Session) {
			f := firstFact(*s, isCat(F6))
			for id, r := range s.Records {
				r.Text += refLine(f.Kind, f.Subject, "K7P-2941") + "\n"
				s.Records[id] = r
				break
			}
		}},
		{"the word UNKNOWN in a user message", "f6-leak", func(s *Session) {
			s.Turns[0].User += " " + Unknown
		}},
		{"question before its fact exists", "batch-early", func(s *Session) {
			s.Probes[0].Questions[0].FactID = firstFact(*s, func(f Fact) bool {
				return f.Category == F1 && f.IntroducedTurn > s.Probes[0].AfterTurn
			}).ID
		}},
		{"too few switches", "switches", func(s *Session) {
			for i := range s.Turns {
				s.Turns[i].Owner = SideCloud
			}
		}},
		{"oversized record", "record-size", func(s *Session) {
			for id, r := range s.Records {
				r.Text += strings.Repeat("x", 13000)
				s.Records[id] = r
				break
			}
		}},
		{"undersized record", "record-size", func(s *Session) {
			for id, r := range s.Records {
				r.Text = "too short"
				s.Records[id] = r
				break
			}
		}},
		{"no long cloud segment", "long-segment", func(s *Session) {
			for id, r := range s.Records {
				if r.Side == SideCloud {
					r.Text = r.Text[:min(len(r.Text), 2000)]
					s.Records[id] = r
				}
			}
		}},
		{"no short cloud segment", "short-segment", func(s *Session) {
			for id, r := range s.Records {
				if r.Side == SideCloud {
					r.Text += strings.Repeat("x", 20000)
					s.Records[id] = r
				}
			}
		}},
		{"repeated code", "code-unique", func(s *Session) {
			s.Turns[0].User += " " + firstFact(*s, isCat(F1)).Value
		}},
		{"oversized batch", "batch-size", func(s *Session) {
			b := &s.Probes[len(s.Probes)-1]
			for len(b.Questions) <= MaxBatchQuestions {
				b.Questions = append(b.Questions, b.Questions[0])
			}
		}},
		{"batch without F6", "batch-f6", func(s *Session) {
			b := &s.Probes[len(s.Probes)-1]
			b.Questions = slices.DeleteFunc(b.Questions, func(q Question) bool { return q.Category == F6 })
		}},
		{"F5 in a cloud batch", "batch-f5", func(s *Session) {
			f := firstFact(*s, isCat(F5))
			for i := range s.Probes {
				if s.Probes[i].Recipient == SideCloud {
					s.Probes[i].Questions = append(s.Probes[i].Questions, Question{ID: "qx", Category: F5, FactID: f.ID, Answer: f.Value})
					return
				}
			}
		}},
		{"wrong answer key", "batch-answer", func(s *Session) {
			q := &s.Probes[len(s.Probes)-1].Questions[0]
			q.Answer = "K7P-2941"
		}},
		{"prompt without the JSON instruction", "batch-prompt", func(s *Session) {
			b := &s.Probes[0]
			b.Prompt = strings.ReplaceAll(b.Prompt, "only JSON", "some text")
		}},
		{"prompt missing a question", "batch-prompt", func(s *Session) {
			b := &s.Probes[0]
			b.Prompt = strings.ReplaceAll(b.Prompt, b.Questions[0].Text, "")
		}},
		{"question in a free form", "batch-form", func(s *Session) {
			q := &s.Probes[len(s.Probes)-1].Questions[0]
			q.Text = "Tell me anything you remember."
		}},
		{"edge-only cloud turn", "edge", func(s *Session) {
			for i := range s.Turns {
				if s.Turns[i].Owner == SideCloud {
					s.Turns[i].Disclosure = EdgeOnly
					return
				}
			}
		}},
		{"non-F5 fact on an edge-only turn", "edge", func(s *Session) {
			for i := range s.Turns {
				if s.Turns[i].Disclosure == EdgeOnly {
					s.Turns[i].FactIDs = append(s.Turns[i].FactIDs, firstFact(*s, isCat(F1)).ID)
					return
				}
			}
		}},
		{"F2 fact the rule does not give", "f2", func(s *Session) {
			f := firstFact(*s, isCat(F2))
			f.Value = "K7P-2941"
			s.Facts[f.ID] = f
		}},
		{"F4 without a superseded value", "f4", func(s *Session) {
			f := firstFact(*s, isCat(F4))
			f.Superseded = nil
			s.Facts[f.ID] = f
		}},
		{"F4 newest value stated early", "f4", func(s *Session) {
			f := firstFact(*s, isCat(F4))
			s.Turns[0].User += " " + f.Value
		}},
		{"code in a label", "label", func(s *Session) {
			f := firstFact(*s, isCat(F1))
			f.Label = "code K7P-2941"
			s.Facts[f.ID] = f
		}},
		{"non-ASCII text", "ascii", func(s *Session) {
			s.Turns[0].User += " café"
		}},
		{"missing batch", "probe-schedule", func(s *Session) {
			s.Probes = s.Probes[1:]
		}},
		{"wrong recipient", "probe-schedule", func(s *Session) {
			s.Probes[0].Recipient = s.Turns[s.Probes[0].AfterTurn].Owner
		}},
		{"local code in a cloud record", "canary", func(s *Session) {
			var code string
			for _, c := range s.Canaries() {
				code = c
				break
			}
			for id, r := range s.Records {
				if r.Side == SideCloud {
					r.Text += code + "\n"
					s.Records[id] = r
					break
				}
			}
		}},
		{"batch with only F6", "batch-empty", func(s *Session) {
			b := &s.Probes[len(s.Probes)-1]
			b.Questions = slices.DeleteFunc(b.Questions, func(q Question) bool { return q.Category != F6 })
		}},
		{"F4 asked before its correction", "batch-early", func(s *Session) {
			f := firstFact(*s, isCat(F4))
			for i := range s.Probes {
				if s.Probes[i].AfterTurn < f.SupersededTurn {
					s.Probes[i].Questions[0].FactID = f.ID
					return
				}
			}
		}},
		{"turn 0 without an F1 fact", "structure", func(s *Session) {
			s.Turns[0].FactIDs = nil
		}},
		{"no F4 corrected on the other side", "f4-cross", func(s *Session) {
			for _, f := range s.Facts {
				if f.Category == F4 {
					s.Turns[f.SupersededTurn].Owner = s.Turns[f.IntroducedTurn].Owner
				}
			}
		}},
		{"no F4 with two superseded values", "f4-chain", func(s *Session) {
			for id, f := range s.Facts {
				if f.Category == F4 {
					f.Superseded = f.Superseded[:1]
					s.Facts[id] = f
				}
			}
		}},
		{"no distractors", "distractors", func(s *Session) {
			for id, r := range s.Records {
				keep := []string{}
				for _, line := range strings.Split(r.Text, "\n") {
					own := slices.ContainsFunc(r.FactIDs, func(fid string) bool { return strings.Contains(line, s.Facts[fid].Value) })
					if own || len(Codes(line)) == 0 {
						keep = append(keep, line)
					}
				}
				r.Text = strings.Join(keep, "\n")
				s.Records[id] = r
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Generate(pilotSeedBase+2, 50, SetTuning)
			if p := validateSession(s); len(p) != 0 {
				t.Fatalf("the base session is already invalid: %v", p)
			}
			tc.mutate(&s)
			if p := validateSession(s); !p.has(tc.tag) {
				t.Errorf("no %q problem; got %v", tc.tag, p)
			}
		})
	}
}

// TestReportStats logs the corpus statistics for the experiment record.
func TestReportStats(t *testing.T) {
	type summary struct{ min, max, sum, n int }
	add := func(s *summary, v int) {
		if s.n == 0 || v < s.min {
			s.min = v
		}
		if s.n == 0 || v > s.max {
			s.max = v
		}
		s.sum += v
		s.n++
	}
	line := func(s summary) string {
		if s.n == 0 {
			return "none"
		}
		return fmt.Sprintf("min %d, mean %d, max %d (n=%d)", s.min, s.sum/s.n, s.max, s.n)
	}
	for _, length := range []int{20, 50} {
		var tokens, switches, cloudRec, localRec, longGrowth, shortGrowth, medium, batches, questions, cloudTurns, localTurns, cloudWorst, localWorst, batchSize summary
		cats := map[Category]*summary{}
		for _, c := range []Category{F1, F2, F3, F4, F5, F6} {
			cats[c] = &summary{}
		}
		asked, facts, codes, keepAll, edge, local := 0, 0, 0, 0, 0, 0
		for _, s := range pilot(t) {
			if s.Length != length {
				continue
			}
			n, sw := EstimateTokens(s.System), 0
			nc, nl := 0, 0
			for i, tn := range s.Turns {
				n += EstimateTokens(tn.User) + EstimateTokens(tn.ExpectedReply)
				if i > 0 && s.Turns[i-1].Owner != tn.Owner {
					sw++
				}
				if tn.Owner == SideCloud {
					nc++
				} else {
					nl++
					local++
					if tn.Disclosure == EdgeOnly {
						edge++
					}
				}
			}
			add(&cloudTurns, nc)
			add(&localTurns, nl)
			for _, r := range s.Records {
				rt := EstimateTokens(r.Text)
				n += rt
				codes += len(Codes(r.Text))
				for _, l := range strings.Split(r.Text, "\n") {
					if len(Codes(l)) > 0 {
						keepAll += pessimisticTokens(l)
					}
				}
				if r.Side == SideCloud {
					add(&cloudRec, rt)
					add(&cloudWorst, pessimisticTokens(r.Text))
				} else {
					add(&localRec, rt)
					add(&localWorst, pessimisticTokens(r.Text))
				}
			}
			add(&tokens, n)
			add(&switches, sw)
			for _, seg := range cloudSegments(s) {
				switch {
				case !seg.bounded:
				case seg.growth > 20000:
					add(&longGrowth, seg.growth)
				case seg.growth < 5000:
					add(&shortGrowth, seg.growth)
				default:
					add(&medium, seg.growth)
				}
			}
			counts := map[Category]int{}
			for _, f := range s.Facts {
				counts[f.Category]++
			}
			for c, sum := range cats {
				add(sum, counts[c])
			}
			add(&batches, len(s.Probes))
			seen := map[string]bool{}
			nq := 0
			for _, b := range s.Probes {
				nq += len(b.Questions)
				add(&batchSize, len(b.Questions))
				for _, q := range b.Questions {
					if q.Category != F6 {
						seen[q.FactID] = true
					}
				}
			}
			add(&questions, nq)
			asked += len(seen)
			facts += len(s.Facts) - counts[F6]
		}
		t.Logf("length %d: tokens per session (user, records, canned replies): %s", length, line(tokens))
		t.Logf("length %d: turns cloud %s; local %s", length, line(cloudTurns), line(localTurns))
		t.Logf("length %d: switches: %s", length, line(switches))
		t.Logf("length %d: cloud record tokens: %s", length, line(cloudRec))
		t.Logf("length %d: local record tokens: %s", length, line(localRec))
		t.Logf("length %d: pessimistic record tokens, cloud %s; local %s", length, line(cloudWorst), line(localWorst))
		t.Logf("length %d: bounded cloud growth, long %s; medium %s; short %s", length, line(longGrowth), line(medium), line(shortGrowth))
		for _, c := range []Category{F1, F2, F3, F4, F5, F6} {
			t.Logf("length %d: facts per session %s: %s", length, c, line(*cats[c]))
		}
		t.Logf("length %d: batches %s; questions per session %s; questions per batch %s", length, line(batches), line(questions), line(batchSize))
		t.Logf("length %d: F1-F5 facts asked at least once: %d of %d; edge-only local turns %d of %d; codes in records per session %d",
			length, asked, facts, edge, local, codes/max(tokens.n, 1))
		t.Logf("length %d: tokens to keep every labelled code line, per session, pessimistic: %d", length, keepAll/max(tokens.n, 1))
	}

	var history, worstHistory, localOwn, steps, f3, all, qs, nb, codes, keepHistory summary
	for _, c := range cbenches(t) {
		add(&history, stepTokens(c.History))
		worst := 0
		for _, st := range c.History {
			worst += pessimisticTokens(st.User) + pessimisticTokens(st.ToolResult) + pessimisticTokens(st.Reply)
		}
		add(&worstHistory, worst)
		add(&localOwn, stepTokens(c.LocalOwn))
		add(&steps, len(c.History))
		nf3, nall := 0, 0
		for _, f := range c.Facts {
			if f.Category == F3 {
				nf3++
			}
			if f.Category != F6 {
				nall++
			}
		}
		add(&f3, nf3)
		add(&all, nall)
		add(&nb, len(c.Probes))
		nq := 0
		for _, b := range c.Probes {
			nq += len(b.Questions)
		}
		add(&qs, nq)
		nc, keep := 0, 0
		for _, st := range c.History {
			nc += len(Codes(st.ToolResult))
			for _, l := range strings.Split(st.ToolResult, "\n") {
				if len(Codes(l)) > 0 {
					keep += pessimisticTokens(l)
				}
			}
		}
		add(&codes, nc)
		add(&keepHistory, keep)
	}
	t.Logf("cbench: history tokens %s; pessimistic %s", line(history), line(worstHistory))
	t.Logf("cbench: local tokens %s", line(localOwn))
	t.Logf("cbench: history steps %s; codes in tool results %s", line(steps), line(codes))
	t.Logf("cbench: tokens to keep every labelled code line, pessimistic: %s", line(keepHistory))
	t.Logf("cbench: F3 facts %s; F1-F5 facts %s", line(f3), line(all))
	t.Logf("cbench: batches %s; questions %s", line(nb), line(qs))
	t.Logf("hash all %s", Hash())
	t.Logf("hash pilot %s", PilotHash())
	t.Logf("hash cbench %s", CBenchHash())
}
