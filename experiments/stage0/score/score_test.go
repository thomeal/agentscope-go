package score

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

func answerJSON(t *testing.T, pairs ...string) string {
	t.Helper()
	type entry struct {
		ID     string `json:"id"`
		Answer string `json:"answer"`
	}
	entries := []entry{}
	for i := 0; i < len(pairs); i += 2 {
		entries = append(entries, entry{pairs[i], pairs[i+1]})
	}
	b, err := json.Marshal(map[string]any{"answers": entries})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// sample has one question of each behaviour the scorer distinguishes.
func sample() []corpus.Question {
	return []corpus.Question{
		{ID: "q1", Category: corpus.F1, Answer: "K7P-2941"},
		{ID: "q2", Category: corpus.F3, Answer: "Q2M-1842"},
		{ID: "q3", Category: corpus.F4, Answer: "X9R-5521", Stale: []string{"B4T-1100", "C5U-2200"}},
		{ID: "q4", Category: corpus.F6, Answer: corpus.Unknown},
	}
}

func byID(t *testing.T, r BatchResult) map[string]Answer {
	t.Helper()
	out := map[string]Answer{}
	for _, a := range r.Answers {
		out[a.QuestionID] = a
	}
	return out
}

func TestBatchRightAndWrong(t *testing.T) {
	qs := sample()
	r := Batch(qs, answerJSON(t, "q1", "K7P-2941", "q2", "WRONG-1", "q3", "X9R-5521", "q4", "UNKNOWN"))
	if !r.Parsed {
		t.Fatal("not parsed")
	}
	got := byID(t, r)
	for id, want := range map[string]bool{"q1": true, "q2": false, "q3": true, "q4": true} {
		if got[id].Correct != want {
			t.Errorf("%s: Correct = %v, want %v", id, got[id].Correct, want)
		}
		if got[id].Missing || got[id].Stale {
			t.Errorf("%s: Missing %v, Stale %v", id, got[id].Missing, got[id].Stale)
		}
	}
	if got["q2"].Raw != "WRONG-1" {
		t.Errorf("Raw = %q", got["q2"].Raw)
	}
	want := map[corpus.Category][2]int{corpus.F1: {1, 1}, corpus.F3: {0, 1}, corpus.F4: {1, 1}, corpus.F6: {1, 1}}
	for cat, c := range want {
		if r.ByCategory[cat] != c {
			t.Errorf("ByCategory[%s] = %v, want %v", cat, r.ByCategory[cat], c)
		}
	}
	if r.Stale != 0 {
		t.Errorf("Stale = %d", r.Stale)
	}
}

func TestBatchKeepsQuestionOrder(t *testing.T) {
	r := Batch(sample(), answerJSON(t, "q4", "UNKNOWN", "q1", "K7P-2941"))
	for i, id := range []string{"q1", "q2", "q3", "q4"} {
		if r.Answers[i].QuestionID != id {
			t.Errorf("Answers[%d] is %s", i, r.Answers[i].QuestionID)
		}
	}
}

func TestBatchTrimsAndFolds(t *testing.T) {
	qs := sample()
	r := Batch(qs, answerJSON(t, "q1", "  k7p-2941\n", "q2", "q2m-1842", "q3", "\tX9R-5521 ", "q4", " unknown "))
	for _, a := range r.Answers {
		if !a.Correct {
			t.Errorf("%s (%q) should be right after trimming and folding", a.QuestionID, a.Raw)
		}
	}
}

func TestBatchToleratesFences(t *testing.T) {
	qs := sample()
	body := answerJSON(t, "q1", "K7P-2941")
	for name, raw := range map[string]string{
		"plain":         body,
		"whitespace":    "\n\n  " + body + "  \n",
		"json fence":    "```json\n" + body + "\n```",
		"upper tag":     "```JSON\n" + body + "\n```",
		"untagged":      "```\n" + body + "\n```",
		"crlf":          "```json\r\n" + body + "\r\n```\r\n",
		"one line":      "```json" + body + "```",
		"outer padding": "  \n```json\n" + body + "\n```\n  ",
	} {
		t.Run(name, func(t *testing.T) {
			r := Batch(qs, raw)
			if !r.Parsed {
				t.Fatalf("not parsed: %q", raw)
			}
			if !byID(t, r)["q1"].Correct {
				t.Error("q1 should be right")
			}
		})
	}
}

func TestBatchParseFailures(t *testing.T) {
	qs := sample()
	body := answerJSON(t, "q1", "K7P-2941", "q2", "Q2M-1842", "q3", "X9R-5521", "q4", "UNKNOWN")
	for name, raw := range map[string]string{
		"empty":           "",
		"blank":           "  \n ",
		"prose":           "I don't know.",
		"prose before":    "Here are the answers: " + body,
		"prose after":     body + " Hope that helps.",
		"prose and fence": "Sure:\n```json\n" + body + "\n```",
		"unclosed fence":  "```json\n" + body,
		"truncated":       body[:len(body)-5],
		"bare array":      `[{"id":"q1","answer":"K7P-2941"}]`,
		"wrong key":       `{"answer":[{"id":"q1","answer":"K7P-2941"}]}`,
		"no key":          `{}`,
		"null array":      `{"answers":null}`,
		"null document":   `null`,
		"string answers":  `{"answers":"K7P-2941"}`,
		"numeric id":      `{"answers":[{"id":1,"answer":"K7P-2941"}]}`,
		"two documents":   body + body,
	} {
		t.Run(name, func(t *testing.T) {
			r := Batch(qs, raw)
			if r.Parsed {
				t.Fatalf("parsed %q", raw)
			}
			if len(r.Answers) != len(qs) {
				t.Fatalf("%d answers for %d questions", len(r.Answers), len(qs))
			}
			for _, a := range r.Answers {
				if a.Correct {
					t.Errorf("%s is right despite the parse failure", a.QuestionID)
				}
				if !a.Missing {
					t.Errorf("%s is not missing despite the parse failure", a.QuestionID)
				}
			}
			for _, cat := range []corpus.Category{corpus.F1, corpus.F3, corpus.F4, corpus.F6} {
				if r.ByCategory[cat] != [2]int{0, 1} {
					t.Errorf("ByCategory[%s] = %v", cat, r.ByCategory[cat])
				}
			}
		})
	}
}

func TestBatchStale(t *testing.T) {
	qs := sample()
	r := Batch(qs, answerJSON(t, "q3", "B4T-1100"))
	got := byID(t, r)
	if got["q3"].Correct || !got["q3"].Stale || got["q3"].Missing {
		t.Errorf("first superseded value: %+v", got["q3"])
	}
	if r.Stale != 1 {
		t.Errorf("Stale = %d", r.Stale)
	}

	r = Batch(qs, answerJSON(t, "q3", " c5u-2200 "))
	if got := byID(t, r)["q3"]; got.Correct || !got.Stale {
		t.Errorf("second superseded value, folded: %+v", got)
	}

	r = Batch(qs, answerJSON(t, "q3", "X9R-5521"))
	if got := byID(t, r)["q3"]; !got.Correct || got.Stale {
		t.Errorf("newest value: %+v", got)
	}

	r = Batch(qs, answerJSON(t, "q3", "Z1Z-9999"))
	if got := byID(t, r)["q3"]; got.Correct || got.Stale {
		t.Errorf("unrelated value: %+v", got)
	}

	// The value of another question is wrong, but not stale.
	r = Batch(qs, answerJSON(t, "q3", "K7P-2941"))
	if got := byID(t, r)["q3"]; got.Correct || got.Stale {
		t.Errorf("another question's value: %+v", got)
	}
}

func TestBatchUnknownRule(t *testing.T) {
	qs := sample()
	t.Run("all UNKNOWN passes only F6", func(t *testing.T) {
		r := Batch(qs, answerJSON(t, "q1", "UNKNOWN", "q2", "UNKNOWN", "q3", "UNKNOWN", "q4", "UNKNOWN"))
		got := byID(t, r)
		for _, id := range []string{"q1", "q2", "q3"} {
			if got[id].Correct || got[id].Missing || got[id].Stale {
				t.Errorf("%s: %+v", id, got[id])
			}
		}
		if !got["q4"].Correct {
			t.Errorf("q4: %+v", got["q4"])
		}
	})
	t.Run("a code on F6 is fabrication", func(t *testing.T) {
		if got := byID(t, Batch(qs, answerJSON(t, "q4", "K7P-2941")))["q4"]; got.Correct || got.Missing {
			t.Errorf("q4: %+v", got)
		}
	})
	t.Run("empty on F6 is not UNKNOWN", func(t *testing.T) {
		if got := byID(t, Batch(qs, answerJSON(t, "q4", "")))["q4"]; got.Correct || !got.Missing {
			t.Errorf("q4: %+v", got)
		}
	})
	t.Run("near misses of UNKNOWN fail", func(t *testing.T) {
		for _, raw := range []string{"N/A", "none", "unknown.", "I do not know", "UNKNOWN UNKNOWN"} {
			if got := byID(t, Batch(qs, answerJSON(t, "q4", raw)))["q4"]; got.Correct {
				t.Errorf("%q passed", raw)
			}
		}
	})
	t.Run("F6 ignores a wrong answer key", func(t *testing.T) {
		q := []corpus.Question{{ID: "q1", Category: corpus.F6, Answer: "K7P-2941"}}
		if got := byID(t, Batch(q, answerJSON(t, "q1", "K7P-2941")))["q1"]; got.Correct {
			t.Error("a code passed on F6")
		}
		if got := byID(t, Batch(q, answerJSON(t, "q1", "UNKNOWN")))["q1"]; !got.Correct {
			t.Error("UNKNOWN failed on F6")
		}
	})
}

func TestBatchOddShapes(t *testing.T) {
	qs := sample()
	t.Run("empty array is parsed and all missing", func(t *testing.T) {
		r := Batch(qs, `{"answers":[]}`)
		if !r.Parsed {
			t.Fatal("not parsed")
		}
		for _, a := range r.Answers {
			if a.Correct || !a.Missing {
				t.Errorf("%+v", a)
			}
		}
	})
	t.Run("null and absent answers are missing", func(t *testing.T) {
		r := Batch(qs, `{"answers":[{"id":"q1","answer":null},{"id":"q2"},{"id":"q3","answer":"   "}]}`)
		if !r.Parsed {
			t.Fatal("not parsed")
		}
		got := byID(t, r)
		for _, id := range []string{"q1", "q2", "q3", "q4"} {
			if got[id].Correct || !got[id].Missing {
				t.Errorf("%s: %+v", id, got[id])
			}
		}
	})
	t.Run("non-string answer is wrong but does not fail the batch", func(t *testing.T) {
		r := Batch(qs, `{"answers":[{"id":"q1","answer":2941},{"id":"q2","answer":"Q2M-1842"},{"id":"q3","answer":{"code":"X9R-5521"}}]}`)
		if !r.Parsed {
			t.Fatal("not parsed")
		}
		got := byID(t, r)
		if got["q1"].Correct || got["q1"].Missing || got["q1"].Raw != "2941" {
			t.Errorf("q1: %+v", got["q1"])
		}
		if !got["q2"].Correct {
			t.Errorf("q2: %+v", got["q2"])
		}
		if got["q3"].Correct || got["q3"].Missing {
			t.Errorf("q3: %+v", got["q3"])
		}
	})
	t.Run("first entry wins for a repeated id", func(t *testing.T) {
		r := Batch(qs, answerJSON(t, "q1", "K7P-2941", "q1", "WRONG-1"))
		if got := byID(t, r)["q1"]; !got.Correct {
			t.Errorf("right then wrong: %+v", got)
		}
		r = Batch(qs, answerJSON(t, "q1", "WRONG-1", "q1", "K7P-2941"))
		if got := byID(t, r)["q1"]; got.Correct {
			t.Errorf("wrong then right: %+v", got)
		}
	})
	t.Run("unknown ids are ignored", func(t *testing.T) {
		r := Batch(qs, answerJSON(t, "q99", "K7P-2941", "q1", "K7P-2941"))
		if !r.Parsed || len(r.Answers) != len(qs) || !byID(t, r)["q1"].Correct {
			t.Errorf("%+v", r)
		}
	})
	t.Run("ids do not match by position", func(t *testing.T) {
		if got := byID(t, Batch(qs, answerJSON(t, "q2", "K7P-2941")))["q1"]; got.Correct {
			t.Errorf("q1: %+v", got)
		}
	})
	t.Run("no questions", func(t *testing.T) {
		r := Batch(nil, `{"answers":[]}`)
		if !r.Parsed || len(r.Answers) != 0 || len(r.ByCategory) != 0 {
			t.Errorf("%+v", r)
		}
	})
}

func oracle(t *testing.T, qs []corpus.Question) string {
	t.Helper()
	pairs := make([]string, 0, 2*len(qs))
	for _, q := range qs {
		pairs = append(pairs, q.ID, q.Answer)
	}
	return answerJSON(t, pairs...)
}

// allBatches gives every probe batch in the pilot and in the benchmark items.
func allBatches() map[string][]corpus.ProbeBatch {
	out := map[string][]corpus.ProbeBatch{}
	for _, s := range corpus.Pilot() {
		out[s.ID] = s.Probes
	}
	for _, c := range corpus.CBenchPilot() {
		out[c.ID] = c.Probes
	}
	return out
}

func TestOracleAndBlankOnRealCorpus(t *testing.T) {
	for id, probes := range allBatches() {
		var right, blank, unknown []BatchResult
		f6, facts := 0, 0
		for _, b := range probes {
			for _, q := range b.Questions {
				if q.Category == corpus.F6 {
					f6++
				} else {
					facts++
				}
			}
			right = append(right, Batch(b.Questions, oracle(t, b.Questions)))
			blank = append(blank, Batch(b.Questions, `{"answers":[]}`))
			pairs := []string{}
			for _, q := range b.Questions {
				pairs = append(pairs, q.ID, corpus.Unknown)
			}
			unknown = append(unknown, Batch(b.Questions, answerJSON(t, pairs...)))
		}

		s := Session(id, right)
		if s.Retention != 1 || s.F6Correct != f6 || s.F6Total != f6 || s.Stale != 0 || s.ParseFailures != 0 {
			t.Errorf("%s: oracle scored %+v", id, s)
		}
		if s.Questions != f6+facts || s.Batches != len(probes) {
			t.Errorf("%s: oracle counted %d questions in %d batches", id, s.Questions, s.Batches)
		}

		s = Session(id, blank)
		if s.Retention != 0 || s.F6Correct != 0 || s.F6Total != f6 || s.ParseFailures != 0 {
			t.Errorf("%s: blank answer scored %+v", id, s)
		}

		s = Session(id, unknown)
		if s.Retention != 0 || s.F6Correct != f6 {
			t.Errorf("%s: all UNKNOWN scored %+v", id, s)
		}
	}
}

// TestStaleAnswersOnRealCorpus answers every F4 question with its oldest value.
func TestStaleAnswersOnRealCorpus(t *testing.T) {
	total := 0
	for id, probes := range allBatches() {
		for _, b := range probes {
			pairs := []string{}
			want := 0
			for _, q := range b.Questions {
				if q.Category == corpus.F4 {
					pairs = append(pairs, q.ID, q.Stale[0])
					want++
				}
			}
			r := Batch(b.Questions, answerJSON(t, pairs...))
			if r.Stale != want {
				t.Errorf("%s: %d stale, want %d", id, r.Stale, want)
			}
			total += want
		}
	}
	if total == 0 {
		t.Error("no F4 questions in the corpus")
	}
}

func TestSessionPools(t *testing.T) {
	a := BatchResult{
		Parsed:     true,
		ByCategory: map[corpus.Category][2]int{corpus.F1: {1, 2}, corpus.F3: {2, 3}, corpus.F6: {1, 1}},
		Stale:      1,
	}
	b := BatchResult{
		Parsed:     false,
		ByCategory: map[corpus.Category][2]int{corpus.F1: {0, 1}, corpus.F5: {0, 2}, corpus.F6: {0, 1}},
	}
	c := BatchResult{
		Parsed:     true,
		ByCategory: map[corpus.Category][2]int{corpus.F3: {3, 3}, corpus.F4: {0, 1}, corpus.F6: {1, 1}},
		Stale:      1,
	}
	s := Session("s1", []BatchResult{a, b, c})
	if s.SessionID != "s1" || s.Batches != 3 || s.ParseFailures != 1 || s.Stale != 2 {
		t.Errorf("%+v", s)
	}
	// Pooled, not averaged per batch: 6 right out of 12 F1-F5 questions.
	if want := 0.5; math.Abs(s.Retention-want) > 1e-12 {
		t.Errorf("Retention = %v, want %v", s.Retention, want)
	}
	if s.F6Correct != 2 || s.F6Total != 3 {
		t.Errorf("F6 = %d/%d", s.F6Correct, s.F6Total)
	}
	if s.Questions != 15 {
		t.Errorf("Questions = %d", s.Questions)
	}
	if s.ByCategory[corpus.F1] != [2]int{1, 3} || s.ByCategory[corpus.F3] != [2]int{5, 6} ||
		s.ByCategory[corpus.F4] != [2]int{0, 1} || s.ByCategory[corpus.F5] != [2]int{0, 2} ||
		s.ByCategory[corpus.F6] != [2]int{2, 3} {
		t.Errorf("ByCategory = %v", s.ByCategory)
	}
}

func TestSessionEdgeCases(t *testing.T) {
	s := Session("empty", nil)
	if s.Retention != 0 || s.Batches != 0 || s.Questions != 0 || s.ByCategory == nil {
		t.Errorf("no batches: %+v", s)
	}
	// F6 alone gives no retention figure, and its answers must not inflate one.
	s = Session("f6", []BatchResult{{Parsed: true, ByCategory: map[corpus.Category][2]int{corpus.F6: {1, 1}}}})
	if s.Retention != 0 || s.F6Correct != 1 {
		t.Errorf("F6 only: %+v", s)
	}
	// A parse failure counts every question against retention.
	qs := []corpus.Question{{ID: "q1", Category: corpus.F1, Answer: "K7P-2941"}, {ID: "q2", Category: corpus.F6, Answer: corpus.Unknown}}
	s = Session("bad", []BatchResult{Batch(qs, "not json")})
	if s.Retention != 0 || s.ParseFailures != 1 || s.Questions != 2 || s.F6Correct != 0 || s.F6Total != 1 {
		t.Errorf("parse failure: %+v", s)
	}
}

// liveRun plays a session from its canned replies and real tool results.
func liveRun(s corpus.Session) (map[int]string, map[int][]string) {
	replies := map[int]string{}
	tools := map[int][]string{}
	for _, tn := range s.Turns {
		replies[tn.Index] = tn.ExpectedReply
		if tn.ReadRecord != "" {
			text, ok := s.Read(tn.Owner, tn.ReadRecord)
			if !ok {
				panic("scripted read failed: " + tn.ReadRecord)
			}
			tools[tn.Index] = []string{text}
		}
	}
	return replies, tools
}

func seenMap(e []Exposure) map[string]bool {
	out := map[string]bool{}
	for _, x := range e {
		out[x.FactID] = x.Seen
	}
	return out
}

func TestExposureOf(t *testing.T) {
	s := corpus.Pilot()[2]
	replies, tools := liveRun(s)

	t.Run("a faithful run saw everything", func(t *testing.T) {
		got := ExposureOf(s, replies, tools)
		n := 0
		for _, f := range s.Facts {
			if f.Category != corpus.F6 {
				n++
			}
		}
		if len(got) != n {
			t.Errorf("%d entries for %d F1-F5 facts", len(got), n)
		}
		for _, e := range got {
			if !e.Seen {
				t.Errorf("%s (%s, %s) not seen", e.FactID, s.Facts[e.FactID].Category, s.Facts[e.FactID].Where)
			}
		}
		for i := 1; i < len(got); i++ {
			if got[i-1].FactID >= got[i].FactID {
				t.Errorf("not sorted at %d", i)
			}
		}
	})

	t.Run("a run that never started saw nothing", func(t *testing.T) {
		for _, e := range ExposureOf(s, nil, nil) {
			if e.Seen {
				t.Errorf("%s seen with no material", e.FactID)
			}
		}
	})

	t.Run("a skipped read hides only its record's facts", func(t *testing.T) {
		var target corpus.Fact
		for _, id := range sortedFactIDs(s) {
			if f := s.Facts[id]; f.Category == corpus.F3 {
				target = f
				break
			}
		}
		less := map[int][]string{}
		for k, v := range tools {
			if k != target.IntroducedTurn {
				less[k] = v
			}
		}
		got := seenMap(ExposureOf(s, replies, less))
		if got[target.ID] {
			t.Errorf("%s seen though its read was skipped", target.ID)
		}
		for _, id := range s.Turns[target.IntroducedTurn].FactIDs {
			if f := s.Facts[id]; f.Where != "cloud_tool" && !got[id] {
				t.Errorf("%s (%s) lost by skipping a read", id, f.Where)
			}
		}
	})

	t.Run("a wrong decision reply does not expose F2", func(t *testing.T) {
		var target corpus.Fact
		for _, id := range sortedFactIDs(s) {
			if f := s.Facts[id]; f.Category == corpus.F2 {
				target = f
				break
			}
		}
		// The code is in the user message as a candidate, but that is not the decision.
		if !strings.Contains(s.Turns[target.IntroducedTurn].User, target.Value) {
			t.Fatalf("%s is not among its candidates", target.ID)
		}
		wrong := map[int]string{}
		for k, v := range replies {
			wrong[k] = v
		}
		wrong[target.IntroducedTurn] = "Chosen: Z1Z-9999."
		if seenMap(ExposureOf(s, wrong, tools))[target.ID] {
			t.Errorf("%s seen though the reply chose something else", target.ID)
		}
	})

	t.Run("a user fact needs its turn to have run", func(t *testing.T) {
		var target corpus.Fact
		for _, id := range sortedFactIDs(s) {
			if f := s.Facts[id]; f.Category == corpus.F1 && f.IntroducedTurn > 0 {
				target = f
				break
			}
		}
		early := map[int]string{}
		earlyTools := map[int][]string{}
		for i := range target.IntroducedTurn {
			early[i] = replies[i]
			if v, ok := tools[i]; ok {
				earlyTools[i] = v
			}
		}
		if seenMap(ExposureOf(s, early, earlyTools))[target.ID] {
			t.Errorf("%s seen before its turn ran", target.ID)
		}
		// A turn that ran but produced no reply still delivered the user message.
		early[target.IntroducedTurn] = ""
		if !seenMap(ExposureOf(s, early, earlyTools))[target.ID] {
			t.Errorf("%s not seen though its turn ran", target.ID)
		}
	})

	t.Run("F4 needs the newest value, not the old one", func(t *testing.T) {
		var target corpus.Fact
		for _, id := range sortedFactIDs(s) {
			if f := s.Facts[id]; f.Category == corpus.F4 {
				target = f
				break
			}
		}
		// Replay only up to the turn before the correction.
		less := map[int]string{}
		lessTools := map[int][]string{}
		for i := range target.SupersededTurn {
			less[i] = replies[i]
			if v, ok := tools[i]; ok {
				lessTools[i] = v
			}
		}
		if seenMap(ExposureOf(s, less, lessTools))[target.ID] {
			t.Errorf("%s seen before its correction", target.ID)
		}
	})

	t.Run("reads after the introducing turn do not count", func(t *testing.T) {
		var target corpus.Fact
		for _, id := range sortedFactIDs(s) {
			if f := s.Facts[id]; f.Category == corpus.F3 {
				target = f
				break
			}
		}
		moved := map[int][]string{}
		for k, v := range tools {
			if k != target.IntroducedTurn {
				moved[k] = v
			}
		}
		moved[target.IntroducedTurn+1] = append(moved[target.IntroducedTurn+1], tools[target.IntroducedTurn]...)
		if seenMap(ExposureOf(s, replies, moved))[target.ID] {
			t.Errorf("%s seen by a read after its turn", target.ID)
		}
	})
}

func sortedFactIDs(s corpus.Session) []string {
	ids := make([]string, 0, len(s.Facts))
	for i := 1; i <= len(s.Facts); i++ {
		id := fmt.Sprintf("f%03d", i)
		if _, ok := s.Facts[id]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}
