// Command tokstat measures the corpus with the local model's real tokenizer.
// The corpus author had no tokenizer, so sizes in its report are estimates. This
// is the L1 check of the token estimate error.
package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
)

func main() {
	ctx := context.Background()
	tk := lab.NewTokenizer("http://127.0.0.1:8000", "Qwen3.5-4B")
	type agg struct {
		chars, tokens int
		sizes         []int
	}
	stats := map[string]*agg{}
	add := func(key, text string) int {
		a := stats[key]
		if a == nil {
			a = &agg{}
			stats[key] = a
		}
		n := tk.Count(ctx, text)
		a.chars += len(text)
		a.tokens += n
		a.sizes = append(a.sizes, n)
		return n
	}
	var sessionTotals = map[int][]int{}
	var longGrowth []int
	for _, s := range corpus.Pilot() {
		total, seg, last := 0, 0, corpus.Side("")
		maxSeg := 0
		for _, t := range s.Turns {
			if t.Owner != last {
				if last == corpus.SideCloud && seg > maxSeg {
					maxSeg = seg
				}
				seg, last = 0, t.Owner
			}
			n := add("user", t.User)
			if t.ReadRecord != "" {
				r := s.Records[t.ReadRecord]
				key := "record_" + string(r.Side)
				n += add(key, r.Text)
			}
			if t.ExpectedReply != "" {
				n += add("reply", t.ExpectedReply)
			}
			total += n
			seg += n
		}
		if last == corpus.SideCloud && seg > maxSeg {
			maxSeg = seg
		}
		longGrowth = append(longGrowth, maxSeg)
		sessionTotals[s.Length] = append(sessionTotals[s.Length], total)
		for _, b := range s.Probes {
			add("probe_prompt", b.Prompt)
		}
	}
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("kind | count | chars/token | p50 | max")
	for _, k := range keys {
		a := stats[k]
		sort.Ints(a.sizes)
		fmt.Printf("%s | %d | %.2f | %d | %d\n", k, len(a.sizes), float64(a.chars)/float64(a.tokens), a.sizes[len(a.sizes)/2], a.sizes[len(a.sizes)-1])
	}
	for l, v := range sessionTotals {
		sort.Ints(v)
		fmt.Printf("session length %d: real tokens min %d p50 %d max %d\n", l, v[0], v[len(v)/2], v[len(v)-1])
	}
	sort.Ints(longGrowth)
	fmt.Printf("largest cloud segment growth, real tokens: min %d p50 %d max %d\n", longGrowth[0], longGrowth[len(longGrowth)/2], longGrowth[len(longGrowth)-1])
	for i, c := range corpus.CBenchPilot() {
		if i > 1 {
			break
		}
		h, o := 0, 0
		for _, st := range c.History {
			h += tk.Count(ctx, st.User) + tk.Count(ctx, st.ToolResult) + tk.Count(ctx, st.Reply)
		}
		for _, st := range c.LocalOwn {
			o += tk.Count(ctx, st.User) + tk.Count(ctx, st.ToolResult) + tk.Count(ctx, st.Reply)
		}
		fmt.Printf("cbench %s: history %d, localown %d real tokens\n", c.ID, h, o)
	}
}
