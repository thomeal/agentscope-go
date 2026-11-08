package s3

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The independent solvers re-derive a reasoning answer from the query text alone.
// They share no code with reasoning.go and use a different algorithm for each
// family, so a bug in a generator's solver cannot blind them.

var (
	reTimer    = regexp.MustCompile(`(\d+) mod (\d+)`)
	reBefore   = regexp.MustCompile(`\b([A-F]) before ([A-F])\b`)
	reJob      = regexp.MustCompile(`\b([A-F]) (\d+)\b`)
	reNeeds    = regexp.MustCompile(`(\d+) needs (\d+)`)
	rePosition = regexp.MustCompile(`positions (\d+) and (\d+)\?$`)
	reTasks    = regexp.MustCompile(`^Tasks ([0-9, ]+)\.`)
	reHost     = regexp.MustCompile(`(?m)^([A-Z2-9]{3}) ([a-c]) ([1-3]) (\d+) (\d+)$`)
	reFloor    = regexp.MustCompile(`spare of at least (\d+)\.`)
)

// solveReasoning recognizes a problem by its wording and solves it.
func solveReasoning(query string) (string, error) {
	switch {
	case strings.HasPrefix(query, "Timer A"):
		return solveTimersSieve(query)
	case strings.HasPrefix(query, "Six steps"):
		return solveOrdersBrute(query)
	case strings.HasPrefix(query, "Jobs and hours"):
		return solveScheduleByTime(query)
	case strings.HasPrefix(query, "Hosts,"):
		return solveHostsBySort(query)
	case strings.HasPrefix(query, "Tasks "):
		return solvePriorityBrute(query)
	}
	return "", fmt.Errorf("no problem recognized in %q", query)
}

// solveTimersSieve walks the congruences one at a time, stepping by the running lcm,
// where the generator scans minute by minute.
func solveTimersSieve(query string) (string, error) {
	ms := reTimer.FindAllStringSubmatch(query, -1)
	if len(ms) != 3 {
		return "", fmt.Errorf("found %d congruences", len(ms))
	}
	x, step := 0, 1
	for _, m := range ms {
		r, _ := strconv.Atoi(m[1])
		mod, _ := strconv.Atoi(m[2])
		for tries := 0; x%mod != r; tries++ {
			if tries > mod {
				return "", fmt.Errorf("no solution at mod %d", mod)
			}
			x += step
		}
		step *= mod
	}
	return strconv.Itoa(x), nil
}

// permutations calls visit with every ordering of 0..n-1.
func permutations(n int, visit func([]int)) {
	p := make([]int, 0, n)
	used := make([]bool, n)
	var rec func()
	rec = func() {
		if len(p) == n {
			visit(p)
			return
		}
		for i := range n {
			if !used[i] {
				used[i] = true
				p = append(p, i)
				rec()
				p = p[:len(p)-1]
				used[i] = false
			}
		}
	}
	rec()
}

func parseBefore(query string) [][2]int {
	var out [][2]int
	for _, m := range reBefore.FindAllStringSubmatch(query, -1) {
		out = append(out, [2]int{int(m[1][0] - 'A'), int(m[2][0] - 'A')})
	}
	return out
}

// solveOrdersBrute tests every ordering, where the generator uses dynamic programming.
func solveOrdersBrute(query string) (string, error) {
	edges := parseBefore(query)
	if len(edges) == 0 {
		return "", fmt.Errorf("no constraints")
	}
	count := 0
	permutations(6, func(p []int) {
		pos := make([]int, 6)
		for i, v := range p {
			pos[v] = i
		}
		for _, e := range edges {
			if pos[e[0]] > pos[e[1]] {
				return
			}
		}
		count++
	})
	return strconv.Itoa(count), nil
}

// solveScheduleByTime searches forward in time over which ready jobs to start at each
// completion, deliberately including leaving a crew idle, where the generator tries
// job orders and places each on the first free crew.
func solveScheduleByTime(query string) (string, error) {
	head, _, ok := strings.Cut(query, ". ")
	if !ok {
		return "", fmt.Errorf("no job list")
	}
	dur := make([]int, 6)
	jobs := reJob.FindAllStringSubmatch(head, -1)
	if len(jobs) != 6 {
		return "", fmt.Errorf("found %d jobs", len(jobs))
	}
	for _, m := range jobs {
		d, _ := strconv.Atoi(m[2])
		dur[m[1][0]-'A'] = d
	}
	edges := parseBefore(query)
	needs := make([]int, 6)
	for _, e := range edges {
		needs[e[1]] |= 1 << e[0]
	}

	const crews = 2
	type state struct {
		done, started int
		left          [6]int
	}
	memo := map[state]int{}
	var search func(s state) int
	search = func(s state) int {
		if s.done == 63 {
			return 0
		}
		if v, ok := memo[s]; ok {
			return v
		}
		running := 0
		for j := range 6 {
			if s.left[j] > 0 {
				running++
			}
		}
		var ready []int
		for j := range 6 {
			if s.started&(1<<j) == 0 && needs[j]&s.done == needs[j] {
				ready = append(ready, j)
			}
		}
		best := 1 << 30
		for mask := range 1 << len(ready) {
			n := 0
			next := s
			for b, j := range ready {
				if mask&(1<<b) != 0 {
					n++
					next.started |= 1 << j
					next.left[j] = dur[j]
				}
			}
			if running+n > crews || running+n == 0 {
				continue
			}
			step := 1 << 30
			for j := range 6 {
				if next.left[j] > 0 {
					step = min(step, next.left[j])
				}
			}
			for j := range 6 {
				if next.left[j] > 0 {
					next.left[j] -= step
					if next.left[j] == 0 {
						next.done |= 1 << j
					}
				}
			}
			best = min(best, step+search(next))
		}
		memo[s] = best
		return best
	}
	return strconv.Itoa(search(state{})), nil
}

type parsedHost struct {
	tag         string
	zone        string
	tier, spare int
	cost        int
}

// solveHostsBySort groups kept hosts by zone in a map and sorts, where the generator
// folds over arrays.
func solveHostsBySort(query string) (string, error) {
	rows := reHost.FindAllStringSubmatch(query, -1)
	floorM := reFloor.FindStringSubmatch(query)
	if len(rows) != 7 || floorM == nil {
		return "", fmt.Errorf("found %d rows and floor %v", len(rows), floorM)
	}
	floor, _ := strconv.Atoi(floorM[1])
	byZone := map[string][]parsedHost{}
	kept := 0
	for _, m := range rows {
		tier, _ := strconv.Atoi(m[3])
		spare, _ := strconv.Atoi(m[4])
		cost, _ := strconv.Atoi(m[5])
		if tier >= 3 || spare < floor {
			continue
		}
		byZone[m[2]] = append(byZone[m[2]], parsedHost{m[1], m[2], tier, spare, cost})
		kept++
	}
	type zoneTotal struct {
		zone  string
		total int
	}
	var totals []zoneTotal
	for z, hosts := range byZone {
		t := 0
		for _, h := range hosts {
			t += h.spare
		}
		totals = append(totals, zoneTotal{z, t})
	}
	slices.SortFunc(totals, func(a, b zoneTotal) int { return b.total - a.total })
	if len(totals) == 0 {
		return "", fmt.Errorf("nothing kept")
	}
	if len(totals) > 1 && totals[0].total == totals[1].total {
		return "", fmt.Errorf("tie between zones")
	}
	hosts := byZone[totals[0].zone]
	slices.SortFunc(hosts, func(a, b parsedHost) int { return a.cost - b.cost })
	if len(hosts) > 1 && hosts[0].cost == hosts[1].cost {
		return "", fmt.Errorf("tie on cost")
	}
	return fmt.Sprintf("%s-%04d", hosts[0].tag, hosts[0].cost+kept), nil
}

// solvePriorityBrute picks the lexicographically smallest valid ordering out of all
// of them, where the generator plays the tie rule forward.
func solvePriorityBrute(query string) (string, error) {
	tasksM := reTasks.FindStringSubmatch(query)
	posM := rePosition.FindStringSubmatch(query)
	if tasksM == nil || posM == nil {
		return "", fmt.Errorf("unparsed task list or positions")
	}
	var ids []int
	for _, s := range strings.Split(tasksM[1], ", ") {
		v, err := strconv.Atoi(s)
		if err != nil {
			return "", err
		}
		ids = append(ids, v)
	}
	needs := reNeeds.FindAllStringSubmatch(query, -1)
	var best []int
	permutations(len(ids), func(p []int) {
		order := make([]int, len(p))
		pos := map[int]int{}
		for i, v := range p {
			order[i] = ids[v]
			pos[ids[v]] = i
		}
		for _, m := range needs {
			a, _ := strconv.Atoi(m[1])
			b, _ := strconv.Atoi(m[2])
			if pos[b] > pos[a] {
				return
			}
		}
		if best == nil || slices.Compare(order, best) < 0 {
			best = order
		}
	})
	if best == nil {
		return "", fmt.Errorf("no valid order")
	}
	p1, _ := strconv.Atoi(posM[1])
	p2, _ := strconv.Atoi(posM[2])
	return strconv.Itoa(best[p1-1] + best[p2-1]), nil
}
