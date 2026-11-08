package report

import (
	"cmp"
	"slices"
)

// TokenTotals sums the usage of ledger rows. Uncached is input - cached - write,
// clamped at 0 the way lab.Prices.Cost clamps it. Inconsistent counts rows where
// the clamp applied. A row with UsageMissing adds no tokens and is counted.
type TokenTotals struct {
	Calls        int `json:"calls"`
	Input        int `json:"input"`
	Cached       int `json:"cached"`
	Write        int `json:"write"`
	Uncached     int `json:"uncached"`
	Output       int `json:"output"`
	Reasoning    int `json:"reasoning"`
	UsageMissing int `json:"usage_missing"`
	Inconsistent int `json:"inconsistent_usage"`
}

func (t *TokenTotals) add(r LedgerRow) {
	t.Calls++
	t.Input += r.Input
	t.Cached += r.Cached
	t.Write += r.Write
	t.Output += r.Output
	t.Reasoning += r.Reasoning
	uncached := r.Input - r.Cached - r.Write
	if uncached < 0 {
		t.Inconsistent++
		uncached = 0
	}
	t.Uncached += uncached
	if r.UsageMissing {
		t.UsageMissing++
	}
}

// CacheHitRate is cached / (cached + write + uncached), nil without tokens.
func (t TokenTotals) CacheHitRate() *float64 {
	return ratio(float64(t.Cached), float64(t.Cached+t.Write+t.Uncached))
}

// sideCacheRate withholds a rate of 0 outside the cloud: the local transport
// records cached tokens only when the server reports them, so a total of 0 cannot
// tell "no cache hits" from "not reported".
func sideCacheRate(side string, t TokenTotals) *float64 {
	if side != sideCloud && t.Cached == 0 {
		return nil
	}
	return t.CacheHitRate()
}

func ratio(num, den float64) *float64 {
	if den == 0 {
		return nil
	}
	r := num / den
	return &r
}

func floatPtr(x float64) *float64 { return &x }

// isSummaryRole is any request whose whole purpose is writing a summary.
func isSummaryRole(role string) bool {
	return isForkRole(role) || role == "compress" || role == "small_summary" || role == "brief" || role == "shorten"
}

// RoleCost is one arm's spend and cache use for one side and role.
type RoleCost struct {
	Side string `json:"side"`
	Role string `json:"role"`
	TokenTotals
	USD           float64  `json:"usd"`
	USDPerSession *float64 `json:"usd_per_session"`
	CacheHitRate  *float64 `json:"cache_hit_rate"`
	LatencyP50    *float64 `json:"latency_ms_p50"`
	LatencyP95    *float64 `json:"latency_ms_p95"`
}

// SideStats is one arm's tokens and timing on one side. Percentiles are nil when
// the side has no rows.
type SideStats struct {
	TokenTotals
	CacheHitRate   *float64 `json:"cache_hit_rate"`
	ReasoningShare *float64 `json:"reasoning_share_of_output"`
	LatencyP50     *float64 `json:"latency_ms_p50"`
	LatencyP95     *float64 `json:"latency_ms_p95"`
	WaitP50        *float64 `json:"wait_ms_p50"`
	WaitP95        *float64 `json:"wait_ms_p95"`
}

// ArmCost is the ledger's account of one arm. USD fields are nil when the arm has
// no ledger rows at all, so that an absent ledger never reads as zero spend.
type ArmCost struct {
	Arm            string `json:"arm"`
	LedgerSessions int    `json:"ledger_sessions"`
	ScoredSessions int    `json:"scored_sessions"`
	Rows           int    `json:"rows"`
	// CloudUSDPerSession divides by LedgerSessions.
	CloudUSD           *float64 `json:"cloud_usd"`
	CloudUSDPerSession *float64 `json:"cloud_usd_per_session"`
	ForkUSD            *float64 `json:"fork_usd"`
	SummaryUSD         *float64 `json:"summary_usd"`
	MainUSD            *float64 `json:"main_usd"`
	// ImputedMainUSD is what a C-group arm's scripted steps would have cost as main
	// calls. Without it, main is two live calls and the ratio compares a fork with almost
	// nothing.
	ImputedMainUSD *float64 `json:"imputed_main_usd"`
	// ForkToMain is fork USD / (main + imputed) USD; ForkShare is fork / (fork + main + imputed).
	ForkToMain *float64   `json:"fork_to_main"`
	ForkShare  *float64   `json:"fork_share"`
	ByRole     []RoleCost `json:"by_role"`
	Cloud      SideStats  `json:"cloud"`
	Local      SideStats  `json:"local"`

	UsageMissing int `json:"usage_missing_rows"`
	// IncompleteRows has status "incomplete" or a recorded incomplete reason.
	// LocalLengthRows is the local counterpart, finish_reason "length"; it is
	// stated apart because the two statuses come from different APIs.
	IncompleteRows  int `json:"incomplete_rows"`
	LocalLengthRows int `json:"local_length_rows"`
	RetriedRows     int `json:"retried_rows"`
	OverflowRows    int `json:"overflow_rows"`
	// ErrorRows carry an error. A failed call has no usage and is not flagged
	// usage_missing by the transport, so its absent tokens show up only here.
	ErrorRows int `json:"error_rows"`
	// UnknownSideRows are neither cloud nor local. They appear in ByRole and in no
	// side or cloud total.
	UnknownSideRows int `json:"unknown_side_rows"`
}

// CostSection is computed from the ledger alone.
type CostSection struct {
	Arms []ArmCost `json:"arms"`
	// TotalCloudUSD sums the rows that survived replacement. Add ReplacedCloudUSD
	// to reconcile with what the runs actually spent.
	TotalCloudUSD    float64 `json:"total_cloud_usd"`
	ReplacedCloudUSD float64 `json:"replaced_cloud_usd"`
}

// Cost computes the cost section for every arm seen in either file, so an arm with
// scores and no ledger appears with stated gaps.
func (d *Data) Cost() CostSection {
	s := CostSection{Arms: []ArmCost{}, ReplacedCloudUSD: d.Stats.ReplacedCloudUSD}
	for _, arm := range d.armNames() {
		c := d.armCost(arm)
		if c.CloudUSD != nil {
			s.TotalCloudUSD += *c.CloudUSD
		}
		s.Arms = append(s.Arms, c)
	}
	return s
}

type sideTimes struct{ latency, wait []float64 }

func (t *sideTimes) add(r LedgerRow) {
	t.latency = append(t.latency, float64(r.LatencyMS))
	t.wait = append(t.wait, float64(r.WaitMS))
}

func p50p95(xs []float64) (*float64, *float64) {
	if len(xs) == 0 {
		return nil, nil
	}
	sorted := slices.Sorted(slices.Values(xs))
	return floatPtr(percentile(sorted, 0.50)), floatPtr(percentile(sorted, 0.95))
}

func finishSide(side string, s *SideStats, t sideTimes) {
	s.CacheHitRate = sideCacheRate(side, s.TokenTotals)
	s.ReasoningShare = ratio(float64(s.Reasoning), float64(s.Output))
	s.LatencyP50, s.LatencyP95 = p50p95(t.latency)
	s.WaitP50, s.WaitP95 = p50p95(t.wait)
}

func (d *Data) armCost(arm string) ArmCost {
	c := ArmCost{Arm: arm, ScoredSessions: len(d.Scores[arm]), ByRole: []RoleCost{}}
	sessions := sortedKeys(d.Ledger[arm])
	c.LedgerSessions = len(sessions)
	roles := map[[2]string]*RoleCost{}
	roleTimes := map[[2]string]*sideTimes{}
	var cloudTimes, localTimes sideTimes
	var cloudUSD, forkUSD, summaryUSD, mainUSD float64
	// Sessions in sorted order and rows in file order, so USD sums are stable.
	for _, id := range sessions {
		for _, r := range d.Ledger[arm][id] {
			c.Rows++
			k := [2]string{r.Side, r.Role}
			if roles[k] == nil {
				roles[k] = &RoleCost{Side: r.Side, Role: r.Role}
				roleTimes[k] = &sideTimes{}
			}
			roles[k].add(r)
			roleTimes[k].add(r)
			roles[k].USD += r.CostUSD
			switch r.Side {
			case sideCloud:
				c.Cloud.add(r)
				cloudTimes.add(r)
				cloudUSD += r.CostUSD
				if isForkRole(r.Role) || r.Role == roleShorten {
					forkUSD += r.CostUSD
				}
				if isSummaryRole(r.Role) {
					summaryUSD += r.CostUSD
				}
				if r.Role == roleMain {
					mainUSD += r.CostUSD
				}
			case sideLocal:
				c.Local.add(r)
				localTimes.add(r)
				if r.Status == "length" {
					c.LocalLengthRows++
				}
			default:
				c.UnknownSideRows++
			}
			if r.UsageMissing {
				c.UsageMissing++
			}
			if r.Status == statusIncomplete || r.Incomplete != "" {
				c.IncompleteRows++
			}
			if r.Attempt > 1 {
				c.RetriedRows++
			}
			if r.Overflow {
				c.OverflowRows++
			}
			if r.Error != "" {
				c.ErrorRows++
			}
		}
	}
	finishSide(sideCloud, &c.Cloud, cloudTimes)
	finishSide(sideLocal, &c.Local, localTimes)
	for k, rc := range roles {
		rc.USDPerSession = ratio(rc.USD, float64(c.LedgerSessions))
		rc.CacheHitRate = sideCacheRate(rc.Side, rc.TokenTotals)
		rc.LatencyP50, rc.LatencyP95 = p50p95(roleTimes[k].latency)
		c.ByRole = append(c.ByRole, *rc)
	}
	slices.SortFunc(c.ByRole, func(a, b RoleCost) int {
		return cmp.Or(cmp.Compare(a.Side, b.Side), cmp.Compare(a.Role, b.Role))
	})
	imputed := 0.0
	for _, s := range d.Scores[arm] {
		imputed += s.ImputedMainUSD
	}
	if c.Rows > 0 {
		c.CloudUSD = floatPtr(cloudUSD)
		c.CloudUSDPerSession = ratio(cloudUSD, float64(c.LedgerSessions))
		c.ForkUSD = floatPtr(forkUSD)
		c.SummaryUSD = floatPtr(summaryUSD)
		c.MainUSD = floatPtr(mainUSD)
		c.ImputedMainUSD = floatPtr(imputed)
		c.ForkToMain = ratio(forkUSD, mainUSD+imputed)
		c.ForkShare = ratio(forkUSD, forkUSD+mainUSD+imputed)
	}
	return c
}
