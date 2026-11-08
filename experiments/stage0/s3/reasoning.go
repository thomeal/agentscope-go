package s3

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// reasoning draws one self-contained problem and solves it. Each family's solver
// here is one way of working the problem out; the package's tests re-derive every
// answer from the query text with a different algorithm. A draw is thrown away when
// its answer would appear in the query, so an answer never gives itself away.
func (g *gen) reasoning(f family) (query, answer string) {
	switch f {
	case famTimers:
		return g.timers()
	case famOrders:
		return g.orderCount()
	case famSchedule:
		return g.crewSchedule()
	case famTable:
		return g.hostTable()
	default:
		return g.priorityOrder()
	}
}

// timerModuli are pairwise coprime, so a common minute always exists.
var timerModuli = []int{7, 9, 11, 13, 16}

// timers is short: three congruences, and the answer is the first common minute.
func (g *gen) timers() (query, answer string) {
	for {
		mods := shuffled(g.r, slices.Clone(timerModuli))[:3]
		res := make([]int, 3)
		for i, m := range mods {
			res[i] = g.r.between(1, m-1)
		}
		n := firstCommonMinute(mods, res)
		answer = strconv.Itoa(n)
		query = fmt.Sprintf("Timer A fires on minutes %d mod %d, B on %d mod %d, C on %d mod %d. First minute all three fire?",
			res[0], mods[0], res[1], mods[1], res[2], mods[2])
		if n > 30 && !strings.Contains(query, answer) {
			return query, answer
		}
	}
}

func firstCommonMinute(mods, res []int) int {
	for n := 1; ; n++ {
		all := true
		for i, m := range mods {
			all = all && n%m == res[i]
		}
		if all {
			return n
		}
	}
}

// randomEdges draws "a before b" constraints consistent with a random order, each
// pair with probability 1 in oneIn, sorted so the text reads in letter order.
func (g *gen) randomEdges(n, oneIn int) [][2]int {
	perm := g.r.perm(n)
	var edges [][2]int
	for i := range n {
		for j := i + 1; j < n; j++ {
			if g.r.chance(1, oneIn) {
				edges = append(edges, [2]int{perm[i], perm[j]})
			}
		}
	}
	slices.SortFunc(edges, func(a, b [2]int) int {
		if a[0] != b[0] {
			return a[0] - b[0]
		}
		return a[1] - b[1]
	})
	return edges
}

func letter(i int) string { return string(rune('A' + i)) }

func beforeText(edges [][2]int) string {
	parts := make([]string, len(edges))
	for i, e := range edges {
		parts[i] = letter(e[0]) + " before " + letter(e[1])
	}
	return strings.Join(parts, ", ")
}

// orderCount is short: the answer is how many orders respect the constraints.
func (g *gen) orderCount() (query, answer string) {
	const n = 6
	for {
		edges := g.randomEdges(n, 3)
		if len(edges) < 4 || len(edges) > 7 {
			continue
		}
		count := countOrders(n, edges)
		if count < 8 || count > 120 {
			continue
		}
		answer = strconv.Itoa(count)
		query = fmt.Sprintf("Six steps, A to F. %s. How many valid orders?", beforeText(edges))
		if !strings.Contains(query, answer) {
			return query, answer
		}
	}
}

// countOrders counts valid orders by dynamic programming over sets of done steps.
func countOrders(n int, edges [][2]int) int {
	needs := make([]int, n)
	for _, e := range edges {
		needs[e[1]] |= 1 << e[0]
	}
	ways := make([]int, 1<<n)
	ways[0] = 1
	for done := range 1 << n {
		for s := range n {
			if done&(1<<s) == 0 && needs[s]&done == needs[s] {
				ways[done|1<<s] += ways[done]
			}
		}
	}
	return ways[(1<<n)-1]
}

// crewSchedule is a constrained schedule: the answer is the shortest finish time with
// two crews. A draw where the crews never matter is thrown away.
func (g *gen) crewSchedule() (query, answer string) {
	const n = 6
	for {
		edges := g.randomEdges(n, 4)
		if len(edges) < 3 || len(edges) > 5 {
			continue
		}
		dur := make([]int, n)
		parts := make([]string, n)
		for i := range dur {
			dur[i] = g.r.between(1, 6)
			parts[i] = fmt.Sprintf("%s %d", letter(i), dur[i])
		}
		best := shortestFinish(dur, edges, 2)
		if best <= longestChain(dur, edges) {
			continue
		}
		answer = strconv.Itoa(best)
		query = fmt.Sprintf("Jobs and hours: %s. %s. Two crews, each running one job at a time, with no job interrupted. Fewest hours to finish all jobs?",
			strings.Join(parts, ", "), beforeText(edges))
		if !strings.Contains(query, answer) {
			return query, answer
		}
	}
}

// shortestFinish tries every job order that respects the edges, putting each job on
// whichever crew is free first. Some such placement is always optimal: take any
// optimal schedule, sort its jobs by start time, and this placement starts each no
// later.
func shortestFinish(dur []int, edges [][2]int, crews int) int {
	n := len(dur)
	best := 0
	for _, d := range dur {
		best += d
	}
	finish := make([]int, n)
	free := make([]int, crews)
	placed := make([]bool, n)

	var place func(count, end int)
	place = func(count, end int) {
		if count == n {
			best = min(best, end)
			return
		}
		for j := range n {
			if placed[j] {
				continue
			}
			earliest, ready := 0, true
			for _, e := range edges {
				if e[1] == j {
					ready = ready && placed[e[0]]
					earliest = max(earliest, finish[e[0]])
				}
			}
			if !ready {
				continue
			}
			crew := 0
			for c := range free {
				if free[c] < free[crew] {
					crew = c
				}
			}
			was := free[crew]
			start := max(earliest, was)
			finish[j] = start + dur[j]
			free[crew] = finish[j]
			placed[j] = true
			place(count+1, max(end, finish[j]))
			placed[j] = false
			free[crew] = was
		}
	}
	place(0, 0)
	return best
}

// longestChain is the finish time with unlimited crews.
func longestChain(dur []int, edges [][2]int) int {
	n := len(dur)
	finish := make([]int, n)
	for range n {
		for j := range n {
			start := 0
			for _, e := range edges {
				if e[1] == j {
					start = max(start, finish[e[0]])
				}
			}
			finish[j] = start + dur[j]
		}
	}
	return slices.Max(finish)
}

// hostRow is one row of a hostTable problem.
type hostRow struct {
	tag   string
	zone  byte
	tier  int
	spare int
	cost  int
}

// hostTable is a multi-step deduction over a small table. The answer is built from
// the surviving row rather than copied from it, so the query cannot contain it.
func (g *gen) hostTable() (query, answer string) {
	const rows = 7
	for {
		floor := g.r.between(15, 30)
		table := make([]hostRow, rows)
		tags := map[string]bool{}
		costs := map[int]bool{}
		for i := range table {
			tag := g.tag()
			for tags[tag] {
				tag = g.tag()
			}
			tags[tag] = true
			cost := g.r.between(100, 400)
			for costs[cost] {
				cost = g.r.between(100, 400)
			}
			costs[cost] = true
			table[i] = hostRow{tag, byte('a' + g.r.intn(3)), g.r.between(1, 3), g.r.between(5, 60), cost}
		}
		answer, ok := solveHostTable(table, floor)
		if !ok || g.codes[answer] {
			continue
		}
		var b strings.Builder
		b.WriteString("Hosts, one per line as tag, zone, tier, spare, cost:\n")
		for _, r := range table {
			fmt.Fprintf(&b, "%s %c %d %d %d\n", r.tag, r.zone, r.tier, r.spare, r.cost)
		}
		fmt.Fprintf(&b, "Keep only hosts with tier below 3 and spare of at least %d. Among the kept hosts, find the zone with the largest total spare, then that zone's kept host with the lowest cost. Answer with its tag, a hyphen, and its cost plus the number of kept hosts, padded to four digits.", floor)
		return b.String(), answer
	}
}

func (g *gen) tag() string {
	var b [3]byte
	for i := range b {
		b[i] = codeAlphabet[g.r.intn(len(codeAlphabet))]
	}
	return string(b[:])
}

// solveHostTable reports false when the draw would have no single answer or barely
// any deduction: too few hosts kept, a tie for the zone, or a one-host zone.
func solveHostTable(table []hostRow, floor int) (string, bool) {
	var kept []hostRow
	var total [3]int
	var count [3]int
	for _, r := range table {
		if r.tier < 3 && r.spare >= floor {
			kept = append(kept, r)
			total[r.zone-'a'] += r.spare
			count[r.zone-'a']++
		}
	}
	if len(kept) < 4 {
		return "", false
	}
	top := 0
	for z := range total {
		if total[z] > total[top] {
			top = z
		}
	}
	for z := range total {
		if z != top && total[z] == total[top] {
			return "", false
		}
	}
	if count[top] < 2 {
		return "", false
	}
	var chosen hostRow
	for _, r := range kept {
		if int(r.zone-'a') == top && (chosen.tag == "" || r.cost < chosen.cost) {
			chosen = r
		}
	}
	return fmt.Sprintf("%s-%04d", chosen.tag, chosen.cost+len(kept)), true
}

// priorityOrder is a dependency order with a tie rule: the answer is the sum of the
// ids at two positions of the resulting order.
func (g *gen) priorityOrder() (query, answer string) {
	const n = 7
	for {
		seen := map[int]bool{}
		ids := make([]int, 0, n)
		for len(ids) < n {
			v := g.r.between(11, 98)
			if !seen[v] {
				seen[v] = true
				ids = append(ids, v)
			}
		}
		slices.Sort(ids)

		edges := g.randomEdges(n, 4)
		if len(edges) < 4 || len(edges) > 7 {
			continue
		}
		order := runLowestFirst(ids, edges)
		if slices.IsSorted(order) {
			continue
		}
		p1 := g.r.between(2, 4)
		p2 := g.r.between(p1+1, n)
		sum := order[p1-1] + order[p2-1]
		answer = strconv.Itoa(sum)

		idText := make([]string, n)
		for i, v := range ids {
			idText[i] = strconv.Itoa(v)
		}
		needText := make([]string, len(edges))
		for i, e := range edges {
			needText[i] = fmt.Sprintf("%d needs %d", ids[e[1]], ids[e[0]])
		}
		query = fmt.Sprintf("Tasks %s. %s. One runs at a time, and when several are ready the lowest id goes first. What is the sum of the ids in positions %d and %d?",
			strings.Join(idText, ", "), strings.Join(needText, ", "), p1, p2)
		if !strings.Contains(query, answer) {
			return query, answer
		}
	}
}

// runLowestFirst plays the tie rule forward. edges index into ids.
func runLowestFirst(ids []int, edges [][2]int) []int {
	n := len(ids)
	done := make([]bool, n)
	order := make([]int, 0, n)
	for range n {
		for j := range n {
			if done[j] {
				continue
			}
			ready := true
			for _, e := range edges {
				if e[1] == j && !done[e[0]] {
					ready = false
				}
			}
			if ready {
				done[j] = true
				order = append(order, ids[j])
				break
			}
		}
	}
	return order
}
