// Package score grades probe batches from the stage 0 corpus. It is deterministic
// and knows nothing about how an answer was produced.
package score

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

// Answer is the grade of one question. Missing means no usable answer was given:
// the id was absent, or the answer was null or blank. Raw holds the answer as
// given.
type Answer struct {
	QuestionID, Raw         string
	Correct, Stale, Missing bool
}

// BatchResult is the grade of one batch. Answers follows question order.
// ByCategory holds {correct, total}, and total always counts every question.
type BatchResult struct {
	Parsed     bool
	Answers    []Answer
	ByCategory map[corpus.Category][2]int
	Stale      int
}

// Batch grades raw model output against a batch's questions.
//
// Output must be one JSON object with an "answers" array, optionally wrapped in
// surrounding whitespace and a code fence. Anything else, including prose around
// the JSON, is a parse failure: Parsed is false and every question is wrong. If
// an id appears more than once, its first entry counts. An answer that is not a
// string is wrong, but does not fail the batch.
//
// An answer is right when it equals the expected value after trimming and
// case-folding. An F6 question is right only on UNKNOWN, whatever its Answer
// field says. A wrong answer equal to a superseded value is stale.
func Batch(qs []corpus.Question, raw string) BatchResult {
	got, parsed := parseAnswers(raw)
	res := BatchResult{
		Parsed:     parsed,
		Answers:    make([]Answer, 0, len(qs)),
		ByCategory: map[corpus.Category][2]int{},
	}
	for _, q := range qs {
		a := grade(q, got)
		res.Answers = append(res.Answers, a)
		counts := res.ByCategory[q.Category]
		counts[1]++
		if a.Correct {
			counts[0]++
		}
		res.ByCategory[q.Category] = counts
		if a.Stale {
			res.Stale++
		}
	}
	return res
}

func grade(q corpus.Question, got map[string]string) Answer {
	a := Answer{QuestionID: q.ID}
	raw, ok := got[q.ID]
	if !ok {
		a.Missing = true
		return a
	}
	a.Raw = raw
	given := strings.TrimSpace(raw)
	if given == "" {
		a.Missing = true
		return a
	}
	want := q.Answer
	if q.Category == corpus.F6 {
		want = corpus.Unknown
	}
	if strings.EqualFold(given, want) {
		a.Correct = true
		return a
	}
	a.Stale = slices.ContainsFunc(q.Stale, func(old string) bool {
		return strings.EqualFold(given, old)
	})
	return a
}

// parseAnswers returns answers by id, leaving out entries whose answer is null or
// absent, which then grade as Missing.
func parseAnswers(raw string) (map[string]string, bool) {
	var wire struct {
		Answers *[]struct {
			ID     string          `json:"id"`
			Answer json.RawMessage `json:"answer"`
		} `json:"answers"`
	}
	if err := json.Unmarshal([]byte(stripFence(raw)), &wire); err != nil || wire.Answers == nil {
		return nil, false
	}
	got := make(map[string]string, len(*wire.Answers))
	seen := map[string]bool{}
	for _, e := range *wire.Answers {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		if len(e.Answer) == 0 || string(e.Answer) == "null" {
			continue
		}
		var s string
		if err := json.Unmarshal(e.Answer, &s); err != nil {
			// A number or object: keep its text so it grades as a wrong answer.
			var compact bytes.Buffer
			if json.Compact(&compact, e.Answer) == nil {
				s = compact.String()
			} else {
				s = string(e.Answer)
			}
		}
		got[e.ID] = s
	}
	return got, true
}

// stripFence removes surrounding whitespace and one code fence, with or without a
// json tag. An unclosed fence is left in place, so it fails to parse.
func stripFence(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) < 6 || !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") {
		return s
	}
	inner := s[3 : len(s)-3]
	if len(inner) >= 4 && strings.EqualFold(inner[:4], "json") {
		inner = inner[4:]
	}
	return strings.TrimSpace(inner)
}

// SessionScore pools a session's batches. Retention is correct over total for
// F1-F5, and 0 when there are no such questions. F6 is reported apart from
// Retention, and also appears in ByCategory.
type SessionScore struct {
	SessionID     string
	Retention     float64
	ByCategory    map[corpus.Category][2]int
	F6Correct     int
	F6Total       int
	Stale         int
	ParseFailures int
	Batches       int
	Questions     int
}

// Session pools graded batches.
func Session(sessionID string, batches []BatchResult) SessionScore {
	out := SessionScore{
		SessionID:  sessionID,
		ByCategory: map[corpus.Category][2]int{},
		Batches:    len(batches),
	}
	for _, b := range batches {
		if !b.Parsed {
			out.ParseFailures++
		}
		out.Stale += b.Stale
		for cat, c := range b.ByCategory {
			sum := out.ByCategory[cat]
			sum[0] += c[0]
			sum[1] += c[1]
			out.ByCategory[cat] = sum
			out.Questions += c[1]
		}
	}
	right, total := 0, 0
	for cat, c := range out.ByCategory {
		if cat == corpus.F6 {
			out.F6Correct, out.F6Total = c[0], c[1]
			continue
		}
		right += c[0]
		total += c[1]
	}
	if total > 0 {
		out.Retention = float64(right) / float64(total)
	}
	return out
}

// Exposure says whether a fact's value reached the side that owned the fact.
type Exposure struct {
	FactID string
	Seen   bool
}

// ExposureOf reports, for each F1-F5 fact in id order, whether its newest value
// appeared in material the owner actually saw by the turn that introduced it
// (for F4, the turn that stated the newest value). F6 has nothing to see and is
// left out. A failed fact still counts against the arm; this only separates
// "never saw it" from "saw it and lost it".
//
// replies and toolResults are keyed by turn index. What counts depends on where
// the fact lives:
//   - "user": the user message of a turn that ran, meaning one with an entry in
//     either map.
//   - "reply" and "edge_reply": the reply, because the user message holds the
//     value only as one unmarked candidate. A reply that lists every candidate
//     still counts as seen.
//   - "cloud_tool" and "local_tool": the tool results, so skipping the read
//     leaves the fact unseen.
//
// Matching ignores case, like the scorer.
func ExposureOf(s corpus.Session, replies map[int]string, toolResults map[int][]string) []Exposure {
	ids := make([]string, 0, len(s.Facts))
	for id, f := range s.Facts {
		if f.Category != corpus.F6 {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)

	out := make([]Exposure, 0, len(ids))
	for _, id := range ids {
		f := s.Facts[id]
		last := max(f.IntroducedTurn, f.SupersededTurn)
		seen := false
		for t := 0; t <= last && t < len(s.Turns) && !seen; t++ {
			seen = seenAt(f, s.Turns[t], replies, toolResults)
		}
		out = append(out, Exposure{FactID: id, Seen: seen})
	}
	return out
}

func seenAt(f corpus.Fact, t corpus.Turn, replies map[int]string, toolResults map[int][]string) bool {
	switch f.Where {
	case "user":
		_, replied := replies[t.Index]
		_, used := toolResults[t.Index]
		return (replied || used) && containsFold(t.User, f.Value)
	case "reply", "edge_reply":
		return containsFold(replies[t.Index], f.Value)
	case "cloud_tool", "local_tool":
		return slices.ContainsFunc(toolResults[t.Index], func(text string) bool {
			return containsFold(text, f.Value)
		})
	}
	return false
}

func containsFold(text, value string) bool {
	return strings.Contains(strings.ToUpper(text), strings.ToUpper(value))
}
