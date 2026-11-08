package report

import (
	"cmp"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
)

// The routing experiment's fixed quantities. RouteLambda is the price of a needless cloud
// call, as a share of a wrong answer. RouteDefaultTheta is the plan's starting threshold.
const (
	RouteLambda       = 0.25
	RouteDefaultTheta = 0.8
	G7MinReduction    = 0.20
	// routeMaxErrorRate is the failure share above which a classifier's row says more
	// about the classifier's availability than about what state does for routing.
	routeMaxErrorRate = 0.10

	routeWorldFresh = "r5k"
	routeRouterQ    = "R2"
	routeRouterS    = "R3"
	routeRouterRule = "R1"
	routeOracle     = "R4"

	routeSideCloud = "cloud"
	routeSideLocal = "local"
)

// RouteThetas is the calibration grid, and RouteLambdas the sensitivity to the price.
var (
	RouteThetas  = []float64{0.5, 0.6, 0.7, 0.8, 0.9, 0.95}
	RouteLambdas = []float64{0, 0.1, 0.25, 0.5, 1}
)

// RouteSide mirrors arms.SideResult.
type RouteSide struct {
	Correct bool   `json:"correct"`
	Reply   string `json:"reply"`
	Error   string `json:"error,omitempty"`
	// HoldsAnswer marks a reply that holds the answer and scored wrong.
	HoldsAnswer bool `json:"holds_answer,omitempty"`
}

// RouteWorld mirrors arms.WorldResult.
type RouteWorld struct {
	Local        RouteSide `json:"local"`
	HasSummary   bool      `json:"has_summary"`
	SummaryChars int       `json:"summary_chars"`
	BehindTokens int       `json:"behind_tokens"`
	Imported     int       `json:"imported"`
	Dropped      int       `json:"imports_dropped"`
}

// RouteDecision mirrors arms.RouteDecision.
type RouteDecision struct {
	Router        string             `json:"router"`
	World         string             `json:"world,omitempty"`
	Classifier    string             `json:"classifier"`
	Choice        string             `json:"choice,omitempty"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	LatencyMS     int64              `json:"latency_ms"`
	StateBytes    int                `json:"state_bytes"`
	InputTokens   int64              `json:"input_tokens"`
	CanaryHits    int                `json:"canary_hits,omitempty"`
	Error         string             `json:"error,omitempty"`
}

// RouteOutcome mirrors arms.RouteOutcome. A test in cmd/stage0 catches drift.
type RouteOutcome struct {
	Kind          string                 `json:"kind"`
	PairID        string                 `json:"pair_id,omitempty"`
	Seed          int64                  `json:"seed"`
	Owner         string                 `json:"owner"`
	Disclosure    string                 `json:"disclosure"`
	CloudEligible bool                   `json:"cloud_eligible"`
	Cloud         RouteSide              `json:"cloud"`
	Worlds        map[string]*RouteWorld `json:"worlds"`
	Rules         map[string]string      `json:"rules"`
	Decisions     []RouteDecision        `json:"decisions"`
}

// routeItem is one scored item.
type routeItem struct {
	id  string
	out *RouteOutcome
}

func (d *Data) routeItems() []routeItem {
	var items []routeItem
	for _, byID := range d.Scores {
		for id, s := range byID {
			if s.Route != nil {
				items = append(items, routeItem{id, s.Route})
			}
		}
	}
	slices.SortFunc(items, func(a, b routeItem) int { return cmp.Compare(a.id, b.id) })
	return items
}

func (d *Data) isS3() bool { return len(d.routeItems()) > 0 }

// utility is what a route is worth: 1 for a right answer, less the price of using the
// cloud. A route the disclosure rules forbid is worth nothing, and no router can take it.
func (r routeItem) utility(world, side string, lambda float64) float64 {
	if side == routeSideCloud {
		if !r.out.CloudEligible {
			return -lambda
		}
		if r.out.Cloud.Correct {
			return 1 - lambda
		}
		return -lambda
	}
	if w := r.out.Worlds[world]; w != nil && w.Local.Correct {
		return 1
	}
	return 0
}

// oracle is R4: both sides ran, so take the better one, and the local side on a tie.
func (r routeItem) oracle(world string, lambda float64) string {
	if r.utility(world, routeSideCloud, lambda) > r.utility(world, routeSideLocal, lambda) {
		return routeSideCloud
	}
	return routeSideLocal
}

func (r routeItem) correct(world, side string) bool {
	return r.utility(world, side, 0) == 1
}

// routerSpec names one routing policy.
type routerSpec struct {
	Router, Classifier string
	World              string // the summary a classifier saw, when it saw one
	Theta              float64
}

func (s routerSpec) label() string {
	if s.Classifier == "" {
		return s.Router
	}
	if s.World == "" {
		return fmt.Sprintf("%s %s", s.Router, s.Classifier)
	}
	return fmt.Sprintf("%s %s@%s", s.Router, s.Classifier, s.World)
}

// choose applies a policy to an item. A classifier below the threshold, or one that
// failed, stays with the owner. The disclosure rule comes first for every router.
func (r routeItem) choose(s routerSpec, world string, lambda float64) (side string, failed bool) {
	if !r.out.CloudEligible {
		return routeSideLocal, false
	}
	switch s.Router {
	case routeOracle:
		return r.oracle(world, lambda), false
	case routeRouterQ, routeRouterS:
		for _, dec := range r.out.Decisions {
			if dec.Router != s.Router || dec.Classifier != s.Classifier || dec.World != s.World {
				continue
			}
			if dec.Error != "" || dec.Choice == "" {
				return r.out.Owner, true
			}
			conf := dec.Confidence
			if conf == 0 {
				conf = dec.Probabilities[dec.Choice]
			}
			if conf+epsilon >= s.Theta {
				return dec.Choice, false
			}
			return r.out.Owner, false
		}
		return r.out.Owner, true
	default:
		if side, ok := r.out.Rules[s.Router]; ok {
			return side, false
		}
		return r.out.Owner, true
	}
}

// RouterRow is one policy's account in one world.
type RouterRow struct {
	Router     string  `json:"router"`
	Classifier string  `json:"classifier,omitempty"`
	World      string  `json:"world"`
	SawSummary string  `json:"saw_summary,omitempty"`
	Theta      float64 `json:"theta,omitempty"`
	N          int     `json:"n"`
	Regret     float64 `json:"regret"`
	// Accuracy is how often the chosen side was right. MissedAnswer is how often a right
	// answer was available and the chosen side lacked it. NeedlessCloud is how often the
	// router paid for a cloud call the oracle would not have made. Regret is built from
	// these two: an item can count in both, when the cloud was wrong and local was right.
	Accuracy      float64 `json:"accuracy"`
	MissedAnswer  float64 `json:"missed_answer"`
	NeedlessCloud float64 `json:"needless_cloud"`
	CloudShare    float64 `json:"cloud_share"`
	Switches      float64 `json:"switch_rate"`
	Failed        int     `json:"failed_decisions"`
}

func (d *Data) routerRow(items []routeItem, s routerSpec, world string, lambda float64) RouterRow {
	return routerRowOf(items, s, world, lambda)
}

func routerRowOf(items []routeItem, s routerSpec, world string, lambda float64) RouterRow {
	row := RouterRow{Router: s.Router, Classifier: s.Classifier, World: world, SawSummary: s.World, Theta: s.Theta, N: len(items)}
	if len(items) == 0 {
		return row
	}
	var regret, right, missed, needless, cloud, switched float64
	for _, it := range items {
		side, failed := it.choose(s, world, lambda)
		if failed {
			row.Failed++
		}
		best := it.oracle(world, lambda)
		regret += it.utility(world, best, lambda) - it.utility(world, side, lambda)
		if it.correct(world, side) {
			right++
		} else if it.correct(world, best) {
			missed++
		}
		if side == routeSideCloud {
			cloud++
			if best != routeSideCloud {
				needless++
			}
		}
		if side != it.out.Owner {
			switched++
		}
	}
	n := float64(len(items))
	row.Regret, row.Accuracy, row.MissedAnswer = regret/n, right/n, missed/n
	row.NeedlessCloud, row.CloudShare, row.Switches = needless/n, cloud/n, switched/n
	return row
}

// KindRow says how each side and policy did on one kind of query.
type KindRow struct {
	Kind         string      `json:"kind"`
	N            int         `json:"n"`
	CloudCorrect float64     `json:"cloud_correct"`
	LocalFresh   float64     `json:"local_correct_r5k"`
	LocalStale   float64     `json:"local_correct_r20k"`
	LocalNone    float64     `json:"local_correct_none"`
	Rows         []RouterRow `json:"rows"`
}

// ClassifierStats is one classifier's operational account.
type ClassifierStats struct {
	Classifier string   `json:"classifier"`
	Calls      int      `json:"calls"`
	Errors     int      `json:"errors"`
	LatencyP50 *float64 `json:"latency_ms_p50"`
	LatencyP95 *float64 `json:"latency_ms_p95"`
	StateP95   *float64 `json:"state_bytes_p95"`
}

// RouteCalibration is the threshold a policy would pick on this data.
type RouteCalibration struct {
	Router     string             `json:"router"`
	Classifier string             `json:"classifier"`
	World      string             `json:"world"`
	ByTheta    map[string]float64 `json:"regret_by_theta"`
	Best       float64            `json:"best_theta"`
	// Freeze marks the row a threshold comes from: R2's, and R3's in the r5k world, where
	// G7 judges it. The other worlds are for reading, and use R3's threshold from r5k.
	Freeze bool `json:"freeze"`
}

// RouteComparison is a paired comparison of regret, resampling a pair's two items together.
type RouteComparison struct {
	A         string   `json:"a"`
	B         string   `json:"b"`
	RegretA   float64  `json:"regret_a"`
	RegretB   float64  `json:"regret_b"`
	Reduction *float64 `json:"reduction"`
	CILow     *float64 `json:"ci_low"`
	CIHigh    *float64 `json:"ci_high"`
	N         int      `json:"n"`
}

// RouteSection is the routing experiment. It has one gate per classifier, because a gate
// answers whether state helps that classifier.
type RouteSection struct {
	Set         string             `json:"set"`
	Thetas      map[string]float64 `json:"thetas"`
	Lambda      float64            `json:"lambda"`
	Worlds      []RouteWorldStat   `json:"worlds"`
	Rows        []RouterRow        `json:"rows"`
	Kinds       []KindRow          `json:"kinds"`
	Calibration []RouteCalibration `json:"calibration"`
	Sensitivity []RouterRow        `json:"sensitivity"`
	Classifiers []ClassifierStats  `json:"classifiers"`
	Gates       []GateResult       `json:"gates"`
	Comparisons []RouteComparison  `json:"comparisons"`
	Privacy     RoutePrivacy       `json:"privacy"`
	Strictness  []RouteStrictness  `json:"strictness"`
}

// RoutePrivacy counts what crossed a line it should not have, and what failed.
type RoutePrivacy struct {
	CloudHits      int `json:"cloud_canary_hits"`
	ClassifierHits int `json:"classifier_canary_hits"`
	ErrorRows      int `json:"error_rows"`
}

// RouteStrictness says how many wrong answers held the answer, by side.
type RouteStrictness struct {
	Side         string `json:"side"`
	Wrong        int    `json:"wrong"`
	HoldsAnswer  int    `json:"holds_answer"`
	AnsweredByIt int    `json:"answered"`
}

// ClassifierNames lists the classifiers these rows measured.
func (s RouteSection) ClassifierNames() []string {
	var out []string
	for _, c := range s.Classifiers {
		out = append(out, c.Classifier)
	}
	return out
}

// RouteWorldStat states how fresh each world's summary was.
type RouteWorldStat struct {
	World           string  `json:"world"`
	WithSummary     int     `json:"with_summary"`
	N               int     `json:"n"`
	MeanBehind      float64 `json:"mean_behind_tokens"`
	MeanChars       float64 `json:"mean_summary_chars"`
	LocalCorrect    float64 `json:"local_correct"`
	OracleRight     float64 `json:"oracle_right"`
	ImportsDropped  float64 `json:"mean_imports_dropped"`
	CloudCorrectAll float64 `json:"cloud_correct"`
}

var routeWorldOrder = []string{routeWorldFresh, "r20k", "none"}

func (d *Data) routeClassifiers(items []routeItem) []string {
	seen := map[string]bool{}
	for _, it := range items {
		for _, dec := range it.out.Decisions {
			seen[dec.Classifier] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func thetaKey(classifier, router string) string { return classifier + "/" + router }

const setS3Acceptance = "s3-acceptance"

// CheckThetas refuses thresholds a report could not honestly use. An acceptance report needs
// R2's and R3's frozen threshold for each classifier, because a default there would be a
// choice nobody made. A name that matches nothing is a typo on any set.
func (d *Data) CheckThetas(thetas map[string]float64) error {
	items := d.routeItems()
	known := map[string]bool{}
	for _, name := range d.routeClassifiers(items) {
		for _, router := range []string{routeRouterQ, routeRouterS} {
			key := thetaKey(name, router)
			known[key] = true
			if _, ok := thetas[key]; !ok && d.routeSet(items) == setS3Acceptance {
				return fmt.Errorf("report: an acceptance report needs --theta for %s, frozen on the pilot", key)
			}
		}
	}
	for key := range thetas {
		if !known[key] {
			return fmt.Errorf("report: --theta names %s, and these rows have no such classifier and router", key)
		}
	}
	return nil
}

func (d *Data) routeSet(items []routeItem) string {
	if len(items) == 0 {
		return ""
	}
	return d.Scores["S3"][items[0].id].Set
}

// Route computes the routing section. Thetas gives each threshold, frozen on the pilot
// before an acceptance run. On the pilot a missing one takes the plan's starting value.
func (d *Data) Route(seed int64, thetas map[string]float64) RouteSection {
	items := d.routeItems()
	s := RouteSection{Thetas: map[string]float64{}, Lambda: RouteLambda}
	s.Set = d.routeSet(items)
	classifiers := d.routeClassifiers(items)
	for _, name := range classifiers {
		for _, router := range []string{routeRouterQ, routeRouterS} {
			key := thetaKey(name, router)
			theta, ok := thetas[key]
			if !ok {
				theta = RouteDefaultTheta
			}
			s.Thetas[key] = theta
		}
	}
	for _, world := range routeWorldOrder {
		s.Worlds = append(s.Worlds, routeWorldStat(items, world))
		for _, spec := range routePolicies(classifiers, s.Thetas, world) {
			s.Rows = append(s.Rows, d.routerRow(items, spec, world, RouteLambda))
		}
	}
	for _, lambda := range RouteLambdas {
		if lambda == RouteLambda {
			continue
		}
		for _, spec := range routePolicies(classifiers, s.Thetas, routeWorldFresh) {
			row := d.routerRow(items, spec, routeWorldFresh, lambda)
			row.Router += fmt.Sprintf(" (lambda %g)", lambda)
			s.Sensitivity = append(s.Sensitivity, row)
		}
	}
	s.Kinds = routeKinds(items, classifiers, s.Thetas)
	s.Calibration = d.routeCalibration(items, classifiers)
	s.Classifiers = routeClassifierStats(items, classifiers)
	for _, name := range classifiers {
		g, comparisons := d.g7(items, name, s.Thetas, seed)
		s.Gates = append(s.Gates, g)
		s.Comparisons = append(s.Comparisons, comparisons...)
	}
	s.Privacy = d.routePrivacy(items)
	s.Strictness = routeStrictness(items)
	return s
}

func (d *Data) routePrivacy(items []routeItem) RoutePrivacy {
	var p RoutePrivacy
	for _, it := range items {
		sc := d.Scores["S3"][it.id]
		if sc.CanaryHits != nil {
			p.CloudHits += *sc.CanaryHits
		}
		if sc.Error != "" {
			p.ErrorRows++
		}
		for _, dec := range it.out.Decisions {
			p.ClassifierHits += dec.CanaryHits
		}
	}
	return p
}

func routeStrictness(items []routeItem) []RouteStrictness {
	cloud := RouteStrictness{Side: "cloud"}
	byWorld := map[string]*RouteStrictness{}
	for _, world := range routeWorldOrder {
		byWorld[world] = &RouteStrictness{Side: "local " + world}
	}
	count := func(st *RouteStrictness, side RouteSide) {
		st.AnsweredByIt++
		if !side.Correct {
			st.Wrong++
		}
		if side.HoldsAnswer {
			st.HoldsAnswer++
		}
	}
	for _, it := range items {
		if it.out.CloudEligible {
			count(&cloud, it.out.Cloud)
		}
		for world, st := range byWorld {
			if w := it.out.Worlds[world]; w != nil {
				count(st, w.Local)
			}
		}
	}
	out := []RouteStrictness{cloud}
	for _, world := range routeWorldOrder {
		out = append(out, *byWorld[world])
	}
	return out
}

// routePolicies are the policies evaluated in a world. R3 sees that world's summary.
func routePolicies(classifiers []string, thetas map[string]float64, world string) []routerSpec {
	specs := []routerSpec{{Router: "R0"}, {Router: routeRouterRule}, {Router: "R1s"}}
	for _, name := range classifiers {
		specs = append(specs,
			routerSpec{Router: routeRouterQ, Classifier: name, Theta: thetas[thetaKey(name, routeRouterQ)]},
			routerSpec{Router: routeRouterS, Classifier: name, World: world, Theta: thetas[thetaKey(name, routeRouterS)]},
		)
	}
	return append(specs, routerSpec{Router: routeOracle})
}

func routeWorldStat(items []routeItem, world string) RouteWorldStat {
	st := RouteWorldStat{World: world, N: len(items)}
	if len(items) == 0 {
		return st
	}
	var behind, chars, local, oracle, dropped, cloud float64
	for _, it := range items {
		if w := it.out.Worlds[world]; w != nil {
			if w.HasSummary {
				st.WithSummary++
				chars += float64(w.SummaryChars)
			}
			behind += float64(w.BehindTokens)
			dropped += float64(w.Dropped)
		}
		if it.correct(world, routeSideLocal) {
			local++
		}
		if it.correct(world, routeSideCloud) {
			cloud++
		}
		if it.correct(world, routeSideLocal) || it.correct(world, routeSideCloud) {
			oracle++
		}
	}
	n := float64(len(items))
	st.MeanBehind, st.LocalCorrect, st.OracleRight = behind/n, local/n, oracle/n
	st.ImportsDropped, st.CloudCorrectAll = dropped/n, cloud/n
	if st.WithSummary > 0 {
		st.MeanChars = chars / float64(st.WithSummary)
	}
	return st
}

func routeKinds(items []routeItem, classifiers []string, thetas map[string]float64) []KindRow {
	byKind := map[string][]routeItem{}
	for _, it := range items {
		byKind[it.out.Kind] = append(byKind[it.out.Kind], it)
	}
	var out []KindRow
	for _, kind := range slices.Sorted(maps.Keys(byKind)) {
		list := byKind[kind]
		n := float64(len(list))
		row := KindRow{Kind: kind, N: len(list)}
		for _, it := range list {
			if it.correct(routeWorldFresh, routeSideCloud) {
				row.CloudCorrect += 1 / n
			}
			if it.correct(routeWorldFresh, routeSideLocal) {
				row.LocalFresh += 1 / n
			}
			if it.correct("r20k", routeSideLocal) {
				row.LocalStale += 1 / n
			}
			if it.correct("none", routeSideLocal) {
				row.LocalNone += 1 / n
			}
		}
		for _, spec := range routePolicies(classifiers, thetas, routeWorldFresh) {
			row.Rows = append(row.Rows, routerRowOf(list, spec, routeWorldFresh, RouteLambda))
		}
		out = append(out, row)
	}
	return out
}

func (d *Data) routeCalibration(items []routeItem, classifiers []string) []RouteCalibration {
	var out []RouteCalibration
	calibrate := func(router, name, world, stateWorld string, freeze bool) {
		c := RouteCalibration{Router: router, Classifier: name, World: world, ByTheta: map[string]float64{}, Freeze: freeze}
		best := -1.0
		for _, theta := range RouteThetas {
			row := d.routerRow(items, routerSpec{Router: router, Classifier: name, World: stateWorld, Theta: theta}, world, RouteLambda)
			c.ByTheta[strconv.FormatFloat(theta, 'f', -1, 64)] = row.Regret
			// A tie goes to the higher threshold, which stays with the owner more often.
			if best < 0 || row.Regret <= best+epsilon {
				best, c.Best = row.Regret, theta
			}
		}
		out = append(out, c)
	}
	for _, name := range classifiers {
		calibrate(routeRouterQ, name, routeWorldFresh, "", true)
		for _, world := range routeWorldOrder {
			calibrate(routeRouterS, name, world, world, world == routeWorldFresh)
		}
	}
	return out
}

func routeClassifierStats(items []routeItem, classifiers []string) []ClassifierStats {
	var out []ClassifierStats
	for _, name := range classifiers {
		st := ClassifierStats{Classifier: name}
		var latency, state []float64
		for _, it := range items {
			for _, dec := range it.out.Decisions {
				if dec.Classifier != name {
					continue
				}
				st.Calls++
				if dec.Error != "" {
					st.Errors++
				}
				latency = append(latency, float64(dec.LatencyMS))
				state = append(state, float64(dec.StateBytes))
			}
		}
		st.LatencyP50, st.LatencyP95 = p50p95(latency)
		_, st.StateP95 = p50p95(state)
		out = append(out, st)
	}
	return out
}

// regretDiffs returns per-item regret differences, grouped by what a resample must keep
// together: a pair's two halves come from one draw.
func regretDiffs(items []routeItem, a, b routerSpec, world string, lambda float64) map[int64][]float64 {
	groups := map[int64][]float64{}
	for _, it := range items {
		sa, _ := it.choose(a, world, lambda)
		sb, _ := it.choose(b, world, lambda)
		groups[it.out.Seed] = append(groups[it.out.Seed], it.utility(world, sa, lambda)-it.utility(world, sb, lambda))
	}
	return groups
}

// clusterCI is a percentile bootstrap over groups of a mean difference. For regretDiffs the
// difference is utility(a) - utility(b), which equals regret(b) - regret(a), so a positive
// value means policy a has the lower regret.
func clusterCI(groups map[int64][]float64, seed int64) (lo, hi float64) {
	keys := slices.Sorted(maps.Keys(groups))
	rng := rand.New(rand.NewPCG(uint64(seed), bootstrapStream))
	means := make([]float64, Resamples)
	for i := range means {
		sum, n := 0.0, 0
		for range keys {
			for _, v := range groups[keys[rng.IntN(len(keys))]] {
				sum += v
				n++
			}
		}
		means[i] = sum / float64(n)
	}
	slices.Sort(means)
	return percentile(means, 0.025), percentile(means, 0.975)
}

// g7 tests R3 on the fresh summary against R2 and R1: regret at least 20% lower and, on an
// acceptance set, the CI of the absolute difference above 0. A pilot cannot be expected to
// clear that CI, so there it is only reported. With a regret of 0 on the other side there
// is nothing to reduce, which fails the gate. A classifier that failed too often is not
// judged.
func (d *Data) g7(items []routeItem, name string, thetas map[string]float64, seed int64) (GateResult, []RouteComparison) {
	acceptance := d.routeSet(items) == setS3Acceptance
	r3 := routerSpec{Router: routeRouterS, Classifier: name, World: routeWorldFresh, Theta: thetas[thetaKey(name, routeRouterS)]}
	others := []routerSpec{{Router: routeRouterQ, Classifier: name, Theta: thetas[thetaKey(name, routeRouterQ)]}, {Router: routeRouterRule}}
	g := GateResult{Gate: "G7 " + name, Recorded: []PairedResult{}}
	var comparisons []RouteComparison
	r3Row := d.routerRow(items, r3, routeWorldFresh, RouteLambda)
	for _, other := range others {
		row := d.routerRow(items, other, routeWorldFresh, RouteLambda)
		cmpRow := RouteComparison{A: r3.label(), B: other.label(), RegretA: r3Row.Regret, RegretB: row.Regret, N: len(items)}
		cond := Condition{Name: fmt.Sprintf("%s regret at least 20%% below %s", r3.label(), other.label()), Threshold: G7MinReduction}
		if acceptance {
			cond.Name += ", CI lower > 0"
		}
		// An edge_only item has no decision to fail, so it is not in the denominator.
		eligible := 0
		for _, it := range items {
			if it.out.CloudEligible {
				eligible++
			}
		}
		errRate := 0.0
		if eligible > 0 {
			errRate = float64(max(r3Row.Failed, row.Failed)) / float64(eligible)
		}
		switch {
		case len(items) < MinPairs:
			cond.Verdict = Insufficient
			cond.Reason = fmt.Sprintf("%d items, need at least %d", len(items), MinPairs)
		case errRate > routeMaxErrorRate:
			cond.Verdict = Insufficient
			cond.Reason = fmt.Sprintf("%d failed decisions over %d eligible items on one router, above the %.0f%% that lets a verdict stand", max(r3Row.Failed, row.Failed), eligible, 100*routeMaxErrorRate)
		default:
			lo, hi := clusterCI(regretDiffs(items, r3, other, routeWorldFresh, RouteLambda), seed)
			cmpRow.CILow, cmpRow.CIHigh = &lo, &hi
			if row.Regret <= epsilon {
				cond.Verdict = Fail
				cond.Reason = fmt.Sprintf("%s already has no regret, so state has nothing to reduce", other.label())
				break
			}
			reduction := (row.Regret - r3Row.Regret) / row.Regret
			cmpRow.Reduction = &reduction
			cond.Value = &reduction
			detail := fmt.Sprintf("regret %.4f against %.4f, reduction %.1f%%, CI of the difference [%.4f, %.4f] over %d items", r3Row.Regret, row.Regret, 100*reduction, lo, hi, len(items))
			switch {
			case atLeast(reduction, G7MinReduction) && (above(lo, 0) || !acceptance):
				cond.Verdict, cond.Reason = Pass, detail
			case atLeast(reduction, G7MinReduction):
				cond.Verdict, cond.Reason = Fail, detail+": the CI reaches 0"
			default:
				cond.Verdict, cond.Reason = Fail, detail+": below 20%"
			}
		}
		g.Conditions = append(g.Conditions, cond)
		comparisons = append(comparisons, cmpRow)
	}
	g.Verdict, g.Reason = rollup(g.Conditions)
	return g, comparisons
}

// ParseThetas reads "decisions/R2=0.7,decisions/R3=0.8".
func ParseThetas(list string) (map[string]float64, error) {
	out := map[string]float64{}
	for part := range strings.SplitSeq(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("report: --theta wants name=value, got %q", part)
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || v < 0 || v > 1 {
			return nil, fmt.Errorf("report: --theta %q is not a probability", part)
		}
		out[strings.TrimSpace(name)] = v
	}
	return out, nil
}

func writeRoute(b *strings.Builder, r *Report) {
	s := r.Route
	fmt.Fprintf(b, "## S3 routing, set %s\n\nThe routers only record a decision. Both sides answered every item except that an edge_only query never goes to the cloud, and each router is scored on the side it chose. Regret is the oracle's utility less the router's, where a right answer is worth 1 and a cloud call costs %.2f. R1s is recorded only. S3 restates labels word for word, so its result shows whether a router can use state, not how much state helps in real sessions.\n\n", s.Set, s.Lambda)

	rows := [][]string{}
	for _, g := range s.Gates {
		rows = append(rows, []string{g.Gate, string(g.Verdict), g.Reason})
		for _, c := range g.Conditions {
			rows = append(rows, []string{"", string(c.Verdict), c.Name + ": " + c.Reason})
		}
	}
	table(b, []string{"Gate", "Verdict", "Reason"}, rows)

	b.WriteString("### Thresholds in use\n\n")
	rows = [][]string{}
	for _, name := range slices.Sorted(maps.Keys(s.Thetas)) {
		rows = append(rows, []string{name, strconv.FormatFloat(s.Thetas[name], 'f', -1, 64)})
	}
	table(b, []string{"Classifier and router", "Theta"}, rows)

	b.WriteString("### What crossed a line\n\n")
	table(b, []string{"Local-only text on the cloud", "Local-only text in a classifier's state", "Rows with an error"}, [][]string{{itoa(s.Privacy.CloudHits), itoa(s.Privacy.ClassifierHits), itoa(s.Privacy.ErrorRows)}})

	b.WriteString("### How strict the score is\n\nWrong answers that still hold the answer. A high count on one side means the strict score understates it.\n\n")
	rows = [][]string{}
	for _, st := range s.Strictness {
		rows = append(rows, []string{st.Side, itoa(st.AnsweredByIt), itoa(st.Wrong), itoa(st.HoldsAnswer)})
	}
	table(b, []string{"Side", "Answered", "Wrong", "Wrong, holding the answer"}, rows)

	b.WriteString("### Each side, by summary freshness\n\n")
	rows = [][]string{}
	for _, w := range s.Worlds {
		rows = append(rows, []string{
			w.World, fmt.Sprintf("%d/%d", w.WithSummary, w.N),
			strconv.FormatFloat(w.MeanBehind, 'f', 0, 64), strconv.FormatFloat(w.MeanChars, 'f', 0, 64),
			strconv.FormatFloat(w.ImportsDropped, 'f', 1, 64),
			strconv.FormatFloat(w.CloudCorrectAll, 'f', 3, 64), strconv.FormatFloat(w.LocalCorrect, 'f', 3, 64), strconv.FormatFloat(w.OracleRight, 'f', 3, 64),
		})
	}
	table(b, []string{"World", "With summary", "Mean tokens behind", "Mean summary chars", "Mean imports dropped", "Cloud right", "Local right", "Either right"}, rows)

	b.WriteString("### Routers\n\n")
	table(b, []string{"World", "Router", "Classifier", "Summary seen", "Theta", "n", "Regret", "Accuracy", "Missed answer", "Needless cloud", "Cloud share", "Switch rate", "Failed"}, routerRows(s.Rows))

	b.WriteString("### By kind, in the r5k world\n\n")
	rows = [][]string{}
	for _, k := range s.Kinds {
		rows = append(rows, []string{
			k.Kind, itoa(k.N), strconv.FormatFloat(k.CloudCorrect, 'f', 3, 64),
			strconv.FormatFloat(k.LocalFresh, 'f', 3, 64), strconv.FormatFloat(k.LocalStale, 'f', 3, 64), strconv.FormatFloat(k.LocalNone, 'f', 3, 64),
		})
	}
	table(b, []string{"Kind", "n", "Cloud right", "Local right r5k", "Local right r20k", "Local right none"}, rows)
	for _, k := range s.Kinds {
		fmt.Fprintf(b, "**%s**\n\n", k.Kind)
		table(b, []string{"World", "Router", "Classifier", "Summary seen", "Theta", "n", "Regret", "Accuracy", "Missed answer", "Needless cloud", "Cloud share", "Switch rate", "Failed"}, routerRows(k.Rows))
	}

	b.WriteString("### Threshold calibration\n\nRegret at each threshold on this data, at the price of 0.25. On the pilot, the lowest regret on a row marked to freeze gives that router's threshold, and a tie goes to the higher one. R2 and R3 are tuned apart, and R3 on r5k alone. On an acceptance set this is for reading only.\n\n")
	header := []string{"Router", "Classifier", "World", "Freeze"}
	for _, t := range RouteThetas {
		header = append(header, strconv.FormatFloat(t, 'f', -1, 64))
	}
	header = append(header, "Best")
	rows = [][]string{}
	for _, c := range s.Calibration {
		freeze := ""
		if c.Freeze {
			freeze = "yes"
		}
		row := []string{c.Router, c.Classifier, c.World, freeze}
		for _, t := range RouteThetas {
			row = append(row, strconv.FormatFloat(c.ByTheta[strconv.FormatFloat(t, 'f', -1, 64)], 'f', 4, 64))
		}
		rows = append(rows, append(row, strconv.FormatFloat(c.Best, 'f', -1, 64)))
	}
	table(b, header, rows)

	b.WriteString("### Other prices for a cloud call\n\n")
	table(b, []string{"World", "Router", "Classifier", "Summary seen", "Theta", "n", "Regret", "Accuracy", "Missed answer", "Needless cloud", "Cloud share", "Switch rate", "Failed"}, routerRows(s.Sensitivity))

	b.WriteString("### Classifiers\n\n")
	rows = [][]string{}
	for _, c := range s.Classifiers {
		rows = append(rows, []string{c.Classifier, itoa(c.Calls), itoa(c.Errors), fnum(c.LatencyP50, 0), fnum(c.LatencyP95, 0), fnum(c.StateP95, 0)})
	}
	table(b, []string{"Classifier", "Calls", "Errors", "Latency P50 ms", "Latency P95 ms", "State bytes P95"}, rows)
}

func routerRows(list []RouterRow) [][]string {
	rows := [][]string{}
	for _, x := range list {
		rows = append(rows, []string{
			x.World, x.Router, x.Classifier, x.SawSummary, strconv.FormatFloat(x.Theta, 'f', -1, 64), itoa(x.N),
			strconv.FormatFloat(x.Regret, 'f', 4, 64), strconv.FormatFloat(x.Accuracy, 'f', 3, 64),
			strconv.FormatFloat(x.MissedAnswer, 'f', 3, 64), strconv.FormatFloat(x.NeedlessCloud, 'f', 3, 64),
			strconv.FormatFloat(x.CloudShare, 'f', 3, 64), strconv.FormatFloat(x.Switches, 'f', 3, 64), itoa(x.Failed),
		})
	}
	return rows
}
