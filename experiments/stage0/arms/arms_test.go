package arms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/score"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

const (
	shortSeed = 7_100_000
	longSeed  = 7_100_002
	benchSeed = 8_100_000
)

func shortSession() corpus.Session { return corpus.Generate(shortSeed, 20, corpus.SetTuning) }
func longSession() corpus.Session  { return corpus.Generate(longSeed, 50, corpus.SetTuning) }
func benchItem() corpus.CBench     { return corpus.GenerateCBench(benchSeed, corpus.SetTuning) }

func windowOf(arm string) int { return max(LocalWindow(arm), Window8K) }

func runSession(t *testing.T, f *fake, env *Env, arm string, s corpus.Session) map[string]Row {
	t.Helper()
	f.loadSession(s)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rows, err := env.Exec(ctx, Job{Arm: arm, Session: &s})
	if err != nil {
		t.Fatalf("%s: %v", arm, err)
	}
	return byArm(rows)
}

func runBench(t *testing.T, f *fake, env *Env, arm string, b corpus.CBench) Row {
	t.Helper()
	f.loadBench(b)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rows, err := env.Exec(ctx, Job{Arm: arm, Bench: &b})
	if err != nil {
		t.Fatalf("%s: %v", arm, err)
	}
	if len(rows) != 1 {
		t.Fatalf("%s: %d rows", arm, len(rows))
	}
	return rows[0]
}

func byArm(rows []Row) map[string]Row {
	out := map[string]Row{}
	for _, r := range rows {
		out[r.Arm] = r
	}
	return out
}

func checkFull(t *testing.T, r Row) {
	t.Helper()
	if r.Error != "" {
		t.Errorf("%s: error %q", r.Arm, r.Error)
	}
	if r.Retention != 1 {
		t.Errorf("%s: retention = %v, want 1 (%v)", r.Arm, r.Retention, r.ByCategory)
	}
	for cat, c := range r.ByCategory {
		if c[0] != c[1] || c[1] == 0 {
			t.Errorf("%s: %s = %v", r.Arm, cat, c)
		}
	}
	for _, cat := range []corpus.Category{corpus.F1, corpus.F3, corpus.F6} {
		if r.ByCategory[cat][1] == 0 {
			t.Errorf("%s: no %s questions", r.Arm, cat)
		}
	}
	if r.F6Total == 0 || r.F6Correct != r.F6Total {
		t.Errorf("%s: F6 = %d/%d", r.Arm, r.F6Correct, r.F6Total)
	}
	if r.ParseFailures != 0 {
		t.Errorf("%s: %d parse failures", r.Arm, r.ParseFailures)
	}
	if r.Counters["session_errors"] != 0 || r.Counters["probe_errors"] != 0 {
		t.Errorf("%s: counters %v", r.Arm, r.Counters)
	}
	if r.ExposureTotal == 0 || r.ExposureSeen != r.ExposureTotal {
		t.Errorf("%s: exposure %d/%d", r.Arm, r.ExposureSeen, r.ExposureTotal)
	}
}

func checkZero(t *testing.T, r Row) {
	t.Helper()
	if r.Error != "" {
		t.Errorf("%s: error %q", r.Arm, r.Error)
	}
	if r.Retention != 0 {
		t.Errorf("%s: retention = %v", r.Arm, r.Retention)
	}
	if r.Questions == 0 || r.Batches == 0 {
		t.Errorf("%s: %d questions, %d batches", r.Arm, r.Questions, r.Batches)
	}
	for cat, c := range r.ByCategory {
		if c[0] != 0 {
			t.Errorf("%s: %s = %v", r.Arm, cat, c)
		}
	}
	if r.F6Total == 0 || r.F6Correct != 0 {
		t.Errorf("%s: F6 = %d/%d", r.Arm, r.F6Correct, r.F6Total)
	}
}

type sideRole struct{ side, role string }

func rolesOf(env *Env, arm string) map[sideRole]int {
	out := map[sideRole]int{}
	for _, r := range env.Ledger.Rows() {
		if r.Arm == arm {
			out[sideRole{r.Side, r.Role}]++
		}
	}
	return out
}

func requireRoles(t *testing.T, env *Env, arm string, want ...sideRole) {
	t.Helper()
	got := rolesOf(env, arm)
	for _, sr := range want {
		if got[sr] == 0 {
			t.Errorf("%s: no ledger row for %v; have %v", arm, sr, got)
		}
	}
}

func checkFakeClean(t *testing.T, f *fake) {
	t.Helper()
	if len(f.rejected) > 0 {
		t.Errorf("provider rejected %d requests, first: %s", len(f.rejected), f.rejected[0])
	}
	if len(f.lost) > 0 {
		t.Errorf("oracle could not place %d requests, first: %q", len(f.lost), f.lost[0])
	}
}

var (
	cloudMain     = sideRole{lab.SideCloud, lab.RoleMain}
	localMain     = sideRole{lab.SideLocal, lab.RoleMain}
	cloudProbe    = sideRole{lab.SideCloud, lab.RoleProbe}
	localProbe    = sideRole{lab.SideLocal, lab.RoleProbe}
	cloudCompress = sideRole{lab.SideCloud, lab.RoleCompress}
	cloudBrief    = sideRole{lab.SideCloud, lab.RoleBrief}
	localBrief    = sideRole{lab.SideLocal, lab.RoleBrief}
	cloudForkB    = sideRole{lab.SideCloud, lab.RoleForkB}
)

var sessionRoles = map[string][]sideRole{
	ArmA0:    {cloudMain, cloudProbe},
	ArmA38K:  {cloudMain, localMain, cloudProbe, localProbe, cloudCompress},
	ArmA332K: {cloudMain, localMain, cloudProbe, localProbe},
	ArmA6:    {cloudMain, localMain, cloudProbe, localProbe},
	ArmA4:    {cloudMain, localMain, cloudProbe, localProbe, cloudBrief, localBrief},
	ArmT1:    {cloudMain, localMain, cloudProbe, localProbe},
}

func TestOracleScoresFullOnEverySessionArm(t *testing.T) {
	for _, arm := range SessionArms {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, windowOf(arm))
			env := f.env(0)
			rows := runSession(t, f, env, arm, shortSession())
			checkFull(t, rows[arm])
			checkFakeClean(t, f)
			requireRoles(t, env, arm, sessionRoles[arm]...)
			if LocalWindow(arm) > 0 && arm != ArmA6 {
				if n := rows[arm].Counters["overflow_recoveries"]; n != 0 {
					t.Errorf("%d overflow recoveries with exact counting", n)
				}
			}
			switch arm {
			case ArmA4:
				checkFull(t, rows[ArmA4Abr])
				requireRoles(t, env, ArmA4Abr, cloudProbe, localProbe)
			case ArmT1:
				checkFull(t, rows[ArmT1Brief])
				requireRoles(t, env, ArmT1Brief, cloudProbe, localProbe, cloudBrief, localBrief)
			case ArmA6:
				if rows[arm].Counters["overflow_recoveries"] == 0 {
					t.Error("A6 never overflowed the 8K server")
				}
			}
		})
	}
}

func TestNullScoresZeroOnEverySessionArm(t *testing.T) {
	for _, arm := range SessionArms {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeNull, windowOf(arm))
			env := f.env(0)
			rows := runSession(t, f, env, arm, shortSession())
			checkZero(t, rows[arm])
			checkFakeClean(t, f)
			requireRoles(t, env, arm, cloudMain, cloudProbe)
			if rows[arm].Counters["empty_replies"] == 0 {
				t.Error("no empty replies from the null model")
			}
			switch arm {
			case ArmA4:
				checkZero(t, rows[ArmA4Abr])
				// An empty brief delivers nothing, and the row says so.
				if rows[arm].Counters["brief_failed"] == 0 {
					t.Error("empty briefs were not counted")
				}
			case ArmT1:
				checkZero(t, rows[ArmT1Brief])
				// The null model reads nothing, so the cloud never grows by 20K.
				if n := len(ofKind(f.captures(), lab.SideCloud, kindFork)); n != 0 {
					t.Errorf("%d forks with no growth", n)
				}
			}
		})
	}
}

var benchRoles = map[string][]sideRole{
	ArmC0:  {cloudMain, localProbe, cloudCompress},
	ArmC3:  {cloudMain, localProbe, cloudForkB},
	ArmC4:  {cloudMain, localProbe, {lab.SideCloud, lab.RoleMerged}},
	ArmC6:  {cloudMain, localProbe, cloudForkB},
	ArmC7:  {cloudMain, localProbe, {lab.SideCloud, lab.RoleSmall}},
	ArmC8:  {cloudMain, localProbe, cloudForkB},
	ArmC3A: {cloudMain, localProbe, cloudForkB, {lab.SideCloud, lab.RoleForkA}},
}

func TestOracleScoresFullOnEveryBenchArm(t *testing.T) {
	for _, arm := range BenchArms {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			env := f.env(0)
			item := benchItem()
			row := runBench(t, f, env, arm, item)
			checkFull(t, row)
			checkFakeClean(t, f)
			requireRoles(t, env, arm, benchRoles[arm]...)
			if row.Set != "c-tuning" || row.Length != len(item.History) || row.Batches != len(item.Probes) {
				t.Errorf("row = %s %d %d", row.Set, row.Length, row.Batches)
			}
			if arm == ArmC8 {
				if row.Counters["keeps"] != 1 || row.Counters["clears"] != 0 {
					t.Errorf("counters %v", row.Counters)
				}
			} else if row.Counters["clears"] != 1 || row.Counters["keeps"] != 0 {
				t.Errorf("counters %v", row.Counters)
			}
			if row.CanaryHits != 0 {
				t.Errorf("%d canary hits", row.CanaryHits)
			}
		})
	}
}

func TestNullScoresZeroOnEveryBenchArm(t *testing.T) {
	for _, arm := range BenchArms {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeNull, Window8K)
			env := f.env(0)
			row := runBench(t, f, env, arm, benchItem())
			checkZero(t, row)
			checkFakeClean(t, f)
			requireRoles(t, env, arm, cloudMain, localProbe)
			if row.Counters["fork_failed"] == 0 {
				t.Errorf("an empty summary was not counted: %v", row.Counters)
			}
			if row.ExposureTotal == 0 || row.ExposureSeen >= row.ExposureTotal {
				t.Errorf("exposure %d/%d: the null model read nothing at the live step", row.ExposureSeen, row.ExposureTotal)
			}
		})
	}
}

// bodyField returns one top-level JSON field exactly as it was sent.
func bodyField(t *testing.T, raw []byte, name string) string {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return string(body[name])
}

func ofKind(caps []capture, side, kind string) []capture {
	var out []capture
	for _, c := range caps {
		if c.Side == side && c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func TestForkReplaysTheMainPrefixByteForByte(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	runSession(t, f, env, ArmT1, longSession())
	caps := f.captures()
	forks := 0
	var lastMain *capture
	for i := range caps {
		c := &caps[i]
		if c.Side != lab.SideCloud {
			continue
		}
		switch c.Kind {
		case kindMain:
			lastMain = c
		case kindFork:
			forks++
			if lastMain == nil {
				t.Fatal("fork before any main call")
			}
			ok, err := lab.PrefixEqual(lastMain.Raw, c.Raw)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Errorf("fork %d does not extend the last main request", forks)
			}
			for _, field := range []string{"instructions", "tools", "model"} {
				if bodyField(t, lastMain.Raw, field) != bodyField(t, c.Raw, field) {
					t.Errorf("fork %d changed %s", forks, field)
				}
			}
			if bodyField(t, c.Raw, "tool_choice") != `"required"` {
				t.Errorf("fork %d tool_choice = %s", forks, bodyField(t, c.Raw, "tool_choice"))
			}
			if limit := bodyField(t, c.Raw, "max_output_tokens"); limit != strconv.Itoa(forkBLimit) {
				t.Errorf("fork %d limit = %s", forks, limit)
			}
			prev := fmt.Sprintf("FORKTEXT-%d", forks-1)
			if forks > 1 && !bytes.Contains(c.Raw, []byte(prev)) {
				t.Errorf("fork %d does not carry the previous summary", forks)
			}
			if bytes.Contains(lastMain.Raw, []byte("FORKTEXT-")) {
				t.Error("a summary reached the cloud's main history")
			}
		}
	}
	if forks == 0 {
		t.Fatal("no fork fired on the long session")
	}
}

func TestForkBFiresOnGrowthAndNeverAtASwitch(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	forkCalls := func() int { return len(ofKind(f.captures(), lab.SideCloud, kindFork)) }
	var (
		atBegin int
		fired   int
	)
	env.Trace = func(ev TraceEvent) {
		switch ev.Kind {
		case "fork_b":
			fired++
			if ev.Growth < forkBRefresh {
				t.Errorf("fork after growth of only %d", ev.Growth)
			}
		case "switch_begin":
			atBegin = forkCalls()
		case "switch_end":
			if forkCalls() != atBegin {
				t.Errorf("a fork ran inside the switch after turn %d", ev.Turn)
			}
		}
	}
	rows := runSession(t, f, env, ArmT1, longSession())
	if fired < 2 {
		t.Fatalf("%d forks on the long session", fired)
	}
	if rows[ArmT1].Counters["fork_b"] != fired || rows[ArmT1].Counters["fork_failed"] != 0 {
		t.Errorf("counters %v, fired %d", rows[ArmT1].Counters, fired)
	}
	if rows[ArmT1].Retention != 1 {
		t.Errorf("retention %v", rows[ArmT1].Retention)
	}
}

func TestIncompleteForkIsDiscardedAndCounted(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.forkStatus = func(n int) string {
		if n == 1 {
			return "incomplete"
		}
		return ""
	}
	env := f.env(0)
	rows := runSession(t, f, env, ArmT1, longSession())
	c := rows[ArmT1].Counters
	if c["fork_failed"] != 1 || c["fork_b"] == 0 || c["lossy_events"] == 0 {
		t.Errorf("counters %v", c)
	}
	caps := f.captures()
	seenGood := false
	// With a short refresh, a later fork can replace the second before local takes over.
	laterFork := regexp.MustCompile(`FORKTEXT-([2-9]|\d\d)`)
	for _, cp := range caps {
		if strings.Contains(string(cp.Raw), "FORKTEXT-1\"") || strings.Contains(string(cp.Raw), "FORKTEXT-1\\n") {
			t.Fatalf("the incomplete fork's text reached a %s %s call", cp.Side, cp.Kind)
		}
		if cp.Side == lab.SideLocal && laterFork.Match(cp.Raw) {
			seenGood = true
		}
	}
	if !seenGood {
		t.Error("the next completed fork never reached local")
	}
}

func TestClearAndKeepBranches(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	s := longSession()
	type boundary struct {
		kind         string
		at           int
		turn, before int
	}
	var (
		boundaries []boundary
		lastSwitch int
	)
	env.Trace = func(ev TraceEvent) {
		switch ev.Kind {
		case "switch_begin":
			lastSwitch = ev.Turn
		case "clear":
			if len(ev.State.Context) != 0 {
				t.Errorf("clear left %d messages", len(ev.State.Context))
			}
			if !strings.Contains(ev.System, "[Summary from the other assistant") {
				t.Error("clear without a summary in the system prompt")
			}
			boundaries = append(boundaries, boundary{ev.Kind, len(f.captures()), lastSwitch, ev.Before})
			checkIndex(t, s, lastSwitch, ev.System)
		case "keep":
			boundaries = append(boundaries, boundary{ev.Kind, len(f.captures()), lastSwitch, ev.Before})
			checkIndex(t, s, lastSwitch, ev.System)
		}
	}
	rows := runSession(t, f, env, ArmT1, s)
	caps := f.captures()
	clears, keeps, kept := 0, 0, 0
	for _, b := range boundaries {
		var first *capture
		// A compression can come first, and its body leaves out the reserved tail
		// where the imports sit, so look at the first request that is not one.
		for i := b.at; i < len(caps); i++ {
			if caps[i].Side == lab.SideLocal && caps[i].Kind != kindCompress {
				first = &caps[i]
				break
			}
		}
		if first == nil {
			t.Fatalf("no local request after the switch at turn %d", b.turn)
		}
		ownEarlier := false
		for _, turn := range s.Turns[:b.turn+1] {
			if turn.Owner == corpus.SideLocal && bytes.Contains(first.Raw, needle(turn.User)) {
				ownEarlier = true
			}
		}
		switch b.kind {
		case "clear":
			clears++
			if ownEarlier {
				t.Errorf("local's own earlier text survived the clear at turn %d", b.turn)
			}
		case "keep":
			keeps++
			if b.before > 0 && ownEarlier {
				kept++
			}
			if !bytes.Contains(first.Raw, needle(s.Turns[b.turn].User)) {
				t.Errorf("the cloud turn before the keep at turn %d was not imported", b.turn)
			}
		}
	}
	if clears == 0 || kept == 0 {
		t.Fatalf("clears %d, keeps %d, keeps that held local's own text %d: this session does not exercise both branches", clears, keeps, kept)
	}
	c := rows[ArmT1].Counters
	if c["clears"] != clears || c["keeps"] != keeps {
		t.Errorf("counters %v, traced %d clears and %d keeps", c, clears, keeps)
	}
}

// checkIndex requires every edge-only turn so far to be in the index, by its first line.
func checkIndex(t *testing.T, s corpus.Session, upTo int, system string) {
	t.Helper()
	for i, turn := range s.Turns[:upTo+1] {
		if turn.Disclosure == corpus.EdgeOnly && !strings.Contains(system, firstLine(turn.User)) {
			t.Errorf("edge-only turn %d is not in the index", i)
		}
	}
}

func TestT1PublishesSharedTurnsAndWithholdsEverythingElse(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	s := longSession()
	runSession(t, f, f.env(0), ArmT1, s)
	caps := f.captures()
	mains := ofKind(caps, lab.SideCloud, kindMain)
	lastCloud := mains[len(mains)-1].Raw
	lastCloudTurn := 0
	for i, turn := range s.Turns {
		if turn.Owner == corpus.SideCloud {
			lastCloudTurn = i
		}
	}
	shared, edge := 0, 0
	for i, turn := range s.Turns {
		if turn.Owner != corpus.SideLocal {
			continue
		}
		switch turn.Disclosure {
		case corpus.Shared:
			if i > lastCloudTurn {
				continue
			}
			shared++
			if bytes.Count(lastCloud, needle(turn.User)) != 1 || bytes.Count(lastCloud, needle(turn.ExpectedReply)) != 1 {
				t.Errorf("shared local turn %d reached the cloud %d times, want once", i, bytes.Count(lastCloud, needle(turn.User)))
			}
			if turn.ReadRecord != "" && !bytes.Contains(lastCloud, needle(`read_local {"id":"`+turn.ReadRecord+`"}`)) {
				t.Errorf("the tool summary of shared local turn %d did not reach the cloud", i)
			}
		case corpus.EdgeOnly:
			edge++
			for _, c := range caps {
				if c.Side == lab.SideCloud && (bytes.Contains(c.Raw, needle(turn.User)) || bytes.Contains(c.Raw, needle(turn.ExpectedReply))) {
					t.Fatalf("edge-only turn %d reached a cloud %s call", i, c.Kind)
				}
			}
		}
	}
	if shared == 0 || edge == 0 {
		t.Fatalf("%d shared and %d edge-only local turns", shared, edge)
	}
	for _, c := range ofKind(caps, lab.SideLocal, kindMain) {
		for i, turn := range s.Turns {
			if n := bytes.Count(c.Raw, needle(turn.User)); n > 1 {
				t.Fatalf("turn %d is in a local request %d times", i, n)
			}
		}
	}
	for id, r := range s.Records {
		for _, c := range caps {
			// A side's own record stays on its side: a tool summary holds arguments only.
			if string(r.Side) != c.Side && bytes.Contains(c.Raw, needle(r.Text)) {
				t.Fatalf("record %s of the %s side reached a %s %s call", id, r.Side, c.Side, c.Kind)
			}
		}
	}
}

func TestCloudMainPrefixOnlyGrows(t *testing.T) {
	for _, arm := range []string{ArmA0, ArmT1, ArmA4} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			runSession(t, f, f.env(0), arm, longSession())
			mains := ofKind(f.captures(), lab.SideCloud, kindMain)
			for i := 1; i < len(mains); i++ {
				ok, err := lab.PrefixEqual(mains[i-1].Raw, mains[i].Raw)
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					t.Fatalf("main call %d does not extend the one before it", i)
				}
				if bodyField(t, mains[i-1].Raw, "instructions") != bodyField(t, mains[i].Raw, "instructions") {
					t.Fatalf("main call %d changed the system prompt", i)
				}
			}
		})
	}
}

func TestCheckpointClonesKeepTheCachedPrefix(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	runSession(t, f, f.env(0), ArmA0, longSession())
	var last *capture
	caps := f.captures()
	checked := 0
	for i := range caps {
		c := &caps[i]
		switch c.Kind {
		case kindMain:
			last = c
		case kindProbe:
			if last == nil {
				continue
			}
			ok, err := lab.PrefixEqual(last.Raw, c.Raw)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatal("a probe clone does not extend the last main request")
			}
			if bodyField(t, last.Raw, "tools") != bodyField(t, c.Raw, "tools") {
				t.Fatal("a probe clone changed the tool list")
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no probes")
	}
}

func TestOversizedForkIsTrimmedAndCounted(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	long := "HEAD\n" + strings.Repeat("x\n", 2*forkBTarget) + "TAIL"
	f.forkText = func(int) string { return long }
	// Every bounce comes back as long as it went in, so the cut is what is left.
	f.bounceText = func() string { return long }
	rows := runSession(t, f, f.env(0), ArmT1, longSession())
	if rows[ArmT1].Counters["lossy_events"] < rows[ArmT1].Counters["fork_b"] || rows[ArmT1].Counters["fork_b"] == 0 {
		t.Errorf("counters %v", rows[ArmT1].Counters)
	}
	if got := rows[ArmT1].Counters["summary_rewrites"]; got != summaryRounds*rows[ArmT1].Counters["fork_b"] {
		t.Errorf("rewrites = %d, want %d per fork", got, summaryRounds)
	}
	seen := false
	for _, c := range ofKind(f.captures(), lab.SideLocal, kindMain) {
		if bytes.Contains(c.Raw, []byte("HEAD")) {
			seen = true
			if bytes.Contains(c.Raw, []byte("TAIL")) {
				t.Error("the cut kept the end, which is what matters least")
			}
		}
	}
	if !seen {
		t.Error("the cut summary never reached local")
	}
}

func TestSummaryCoverageExcludesEdgeOnlySeqs(t *testing.T) {
	var log threadLog
	log.add(kindUser, string(corpus.SideCloud), corpus.Shared, "a")
	log.add(kindUser, string(corpus.SideLocal), corpus.EdgeOnly, "b")
	log.add(kindReply, string(corpus.SideLocal), corpus.EdgeOnly, "c")
	log.add(kindUser, string(corpus.SideCloud), corpus.Shared, "d")
	var c Counters
	got := applySummary(&log, "text", &c)
	if got.Coverage != 4 || !slices.Equal(got.Excluded, []int{2, 3}) {
		t.Errorf("summary = %+v", got)
	}
	if log.last() != 5 || log.entries[4].Kind != kindSummary {
		t.Errorf("log = %+v", log.entries)
	}
	if imports := log.imports(4, corpus.SideLocal); len(imports) != 0 {
		t.Errorf("a summary entry was imported as text: %d", len(imports))
	}
}

func TestKeepBranchEvictsCoveredImports(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	s := shortSession()
	run := &sessionRun{env: env, arm: ArmT1, sess: s, system: marker("test", ArmT1, s.ID) + "\n" + s.System,
		tr: newTracker(ArmT1, s.ID, s.Set, s.Length, s.Probes), rec: newToolRec(), replies: map[int]string{}}
	a, err := newT1Arm(run, ArmT1)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 9 {
		a.log.add(kindReply, string(corpus.SideCloud), corpus.Shared, fmt.Sprintf("entry %d", i+1))
	}
	imported := func(seq int) *message.Msg {
		m := message.UserMsg("thread", fmt.Sprintf("import %d", seq))
		m.Metadata = types.JSONObject{threadSeqKey: strconv.Itoa(seq)}
		return m
	}
	native := message.UserMsg(agentName, "local's own message")
	a.local.state.Context = []*message.Msg{imported(3), native, imported(7)}
	// The summary covers #5 but not local's activity at #6, so this is the keep branch.
	a.peer = &summary{Text: "PEER", Coverage: 5}
	a.localLast = 6
	a.localCursor = 7
	a.log.add(kindOwnerChange, sourceHarness, corpus.Shared, "cloud -> local")
	if err := a.takeLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if run.tr.c.Keeps != 1 || run.tr.c.Clears != 0 {
		t.Fatalf("branch: %+v", run.tr.c)
	}
	var texts []string
	for _, m := range a.local.state.Context {
		texts = append(texts, *m.GetTextContent(""))
	}
	want := []string{"local's own message", "import 7"}
	if len(texts) < 2 || !slices.Equal(texts[:2], want) {
		t.Fatalf("context = %q", texts)
	}
	for _, text := range texts[2:] {
		// Past the cursor at #7: #8, #9 and the owner change, and nothing older.
		if strings.Contains(text, "entry 1") || strings.Contains(text, "#7 ") {
			t.Errorf("imported %q", text)
		}
	}
	if len(texts) != 5 {
		t.Errorf("context = %q", texts)
	}
	if !strings.Contains(a.local.system, "PEER") {
		t.Error("the keep branch did not adopt the summary")
	}
	if a.local.cfg.SummaryMaxTokens != localSummaryTotal-len("PEER")/summaryBytesPerToken {
		t.Errorf("SummaryMaxTokens = %d", a.local.cfg.SummaryMaxTokens)
	}
}

func TestSummaryBudgetFloor(t *testing.T) {
	if got := summaryBudget(nil); got != localSummaryTotal {
		t.Errorf("no peer: %d", got)
	}
	// A full summary is trimmed at 3 characters per estimated token, and the agent elides
	// its own at 4. Counted one way, the two together are the same 12,000 bytes that an
	// arm with no summary outside Context may use.
	full := &summary{Text: strings.Repeat("x", 3*forkBTarget)}
	got := summaryBudget(full)
	if got != localSummaryTotal-3*forkBTarget/summaryBytesPerToken {
		t.Errorf("full peer: %d", got)
	}
	if got*summaryBytesPerToken+len(full.Text) != localSummaryTotal*summaryBytesPerToken {
		t.Errorf("peer %d bytes and own %d tokens do not add up to the total", len(full.Text), got)
	}
	huge := &summary{Text: strings.Repeat("x", 5*localSummaryTotal)}
	if got := summaryBudget(huge); got != summaryFloor {
		t.Errorf("huge peer: %d", got)
	}
}

func TestNoCanaryReachesTheCloudInT1(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	s := longSession()
	rows := runSession(t, f, env, ArmT1, s)
	if rows[ArmT1].CanaryHits != 0 || rows[ArmT1Brief].CanaryHits != 0 {
		// T1+brief is reported separately. Its briefs here carry no local codes.
		t.Errorf("canary hits: T1 %d, T1+brief %d", rows[ArmT1].CanaryHits, rows[ArmT1Brief].CanaryHits)
	}
	canaries := s.Canaries()
	if len(canaries) == 0 {
		t.Fatal("no canaries to look for")
	}
	cloudBodies := 0
	for _, c := range f.captures() {
		if c.Side != lab.SideCloud {
			continue
		}
		cloudBodies++
		for _, code := range canaries {
			if bytes.Contains(c.Raw, []byte(code)) {
				t.Fatalf("canary %s in a cloud %s body", code, c.Kind)
			}
		}
	}
	if cloudBodies == 0 {
		t.Fatal("no cloud bodies")
	}
	// The same scan must fire on an arm whose single host holds local material.
	g := newFake(t, modeOracle, Window8K)
	a0 := runSession(t, g, g.env(0), ArmA0, s)
	if a0[ArmA0].CanaryHits == 0 {
		t.Error("the scan found nothing on A0, so a zero on T1 proves nothing")
	}
}

func TestProbesNeverChangeTheOriginalState(t *testing.T) {
	for _, arm := range SessionArms {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, windowOf(arm))
			env := f.env(0)
			runSession(t, f, env, arm, longSessionFor(arm))
			// The fake calls any request holding a batch prompt a probe, and the ledger
			// knows the role the arm meant. A prompt left in an owner's state would turn
			// that owner's next main call into a probe here, and the counts would differ.
			probes := map[string]int{}
			mains := map[string]int{}
			for _, r := range env.Ledger.Rows() {
				if r.HTTP != 200 {
					continue
				}
				switch r.Role {
				case lab.RoleProbe:
					probes[r.Side]++
				case lab.RoleMain:
					mains[r.Side]++
				}
			}
			for _, side := range []string{lab.SideCloud, lab.SideLocal} {
				if got := len(ofKind(f.captures(), side, kindProbe)); got != probes[side] {
					t.Errorf("%s: fake saw %d probes, ledger has %d", side, got, probes[side])
				}
				if got := len(ofKind(f.captures(), side, kindMain)); got != mains[side] {
					t.Errorf("%s: fake saw %d main calls, ledger has %d", side, got, mains[side])
				}
			}
		})
	}
}

func TestProbeLeavesStateByteIdentical(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	s := shortSession()
	f.loadSession(s)
	run := &sessionRun{env: env, arm: ArmA0, sess: s, system: marker("test", ArmA0, s.ID) + "\n" + s.System,
		tr: newTracker(ArmA0, s.ID, s.Set, s.Length, s.Probes), rec: newToolRec(), replies: map[int]string{}}
	a, err := newHostArm(ArmA0, run)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := a.host.reply(ctx, s.Turns[0].User); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(a.host.state)
	if err != nil {
		t.Fatal(err)
	}
	if err := run.probePrimary(ctx, a.host, 0); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(a.host.state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a probe changed the owner's state")
	}
}

func TestToolListsAreByteIdenticalAcrossMainCalls(t *testing.T) {
	for _, arm := range SessionArms {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, windowOf(arm))
			env := f.env(0)
			runSession(t, f, env, arm, longSessionFor(arm))
			for _, side := range []string{lab.SideCloud, lab.SideLocal} {
				mains := ofKind(f.captures(), side, kindMain)
				if arm == ArmA0 && side == lab.SideLocal {
					continue
				}
				if len(mains) < 2 {
					t.Fatalf("%s: %d main calls", side, len(mains))
				}
				first := bodyField(t, mains[0].Raw, "tools")
				if first == "" || first == "null" {
					t.Fatalf("%s: no tools", side)
				}
				for i, c := range mains {
					if got := bodyField(t, c.Raw, "tools"); got != first {
						t.Fatalf("%s main call %d changed the tool list:\n%s\n%s", side, i, first, got)
					}
				}
			}
		})
	}
}

func longSessionFor(arm string) corpus.Session {
	if arm == ArmT1 || arm == ArmA4 {
		return longSession()
	}
	return shortSession()
}

func TestT1CloudSeesOnlySharedEntries(t *testing.T) {
	var log threadLog
	log.add(kindUser, string(corpus.SideCloud), corpus.Shared, "shared question\nsecond line")
	log.add(kindReply, string(corpus.SideLocal), corpus.EdgeOnly, "edge secret")
	log.add(kindToolSummary, string(corpus.SideLocal), corpus.EdgeOnly, `read_local {"id":"L-1"}`)
	log.add(kindOwnerChange, sourceHarness, corpus.Shared, "local -> cloud")

	cloud, err := log.read(corpus.SideCloud, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cloud, "edge secret") || strings.Contains(cloud, "read_local") {
		t.Errorf("cloud outline leaks: %s", cloud)
	}
	if strings.Contains(cloud, "second line") || !strings.Contains(cloud, "#1 user cloud: shared question") {
		t.Errorf("cloud outline: %s", cloud)
	}
	if _, err := log.read(corpus.SideCloud, map[string]any{"seq": float64(2)}); err == nil {
		t.Error("cloud read an edge_only entry")
	}
	local, err := log.read(corpus.SideLocal, map[string]any{"seq": float64(2)})
	if err != nil || local != "edge secret" {
		t.Errorf("local read = %q, %v", local, err)
	}
	entry, err := log.read(corpus.SideCloud, map[string]any{"seq": float64(1)})
	if err != nil || entry != "shared question\nsecond line" {
		t.Errorf("entry mode = %q, %v", entry, err)
	}
	if got := log.imports(0, corpus.SideCloud); len(got) != 1 {
		t.Errorf("imports for cloud = %d, want the owner change alone", len(got))
	}
	if idx := log.edgeIndex(); !strings.Contains(idx, "#2 reply: edge secret") || strings.Contains(idx, "shared") {
		t.Errorf("index = %q", idx)
	}
}

func TestA2StaysOnTheCloudWhileItIsHealthy(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	runSession(t, f, f.env(0), ArmA2, shortSession())
	if got := ofKind(f.captures(), lab.SideLocal, kindMain); len(got) != 0 {
		t.Errorf("%d local calls while the cloud is up", len(got))
	}
	if got := ofKind(f.captures(), lab.SideCloud, kindMain); len(got) == 0 {
		t.Error("no cloud calls")
	}
}

func TestA3ReplaysLocalCallsWithoutItemIDAndCompressesOnTheCloud(t *testing.T) {
	f := newFake(t, modeOracle, Window32K)
	env := f.env(0)
	runSession(t, f, env, ArmA332K, shortSession())
	replayed := 0
	for _, c := range ofKind(f.captures(), lab.SideCloud, kindMain) {
		var body struct {
			Input []map[string]any `json:"input"`
		}
		if err := json.Unmarshal(c.Raw, &body); err != nil {
			t.Fatal(err)
		}
		for _, it := range body.Input {
			if it["type"] != "function_call" {
				continue
			}
			id, _ := it["call_id"].(string)
			if strings.HasPrefix(id, "chatcmpl-tool-") {
				replayed++
				if _, has := it["id"]; has {
					t.Fatalf("local call %s replayed with an item id: %v", id, it["id"])
				}
			}
		}
	}
	if replayed == 0 {
		t.Fatal("no local call was replayed to the cloud")
	}

	g := newFake(t, modeOracle, Window8K)
	env8 := g.env(0)
	rows := runSession(t, g, env8, ArmA38K, shortSession())
	cloud := ofKind(g.captures(), lab.SideCloud, kindCompress)
	if len(cloud) == 0 || rows[ArmA38K].Counters["compress"] == 0 {
		t.Fatalf("%d compression bodies on the cloud, counters %v", len(cloud), rows[ArmA38K].Counters)
	}
	if local := ofKind(g.captures(), lab.SideLocal, kindCompress); len(local) != 0 {
		t.Errorf("%d compression calls reached local", len(local))
	}
	if got := rolesOf(env8, ArmA38K)[sideRole{lab.SideLocal, lab.RoleCompress}]; got != 0 {
		t.Errorf("%d local compress rows", got)
	}
	if got := rolesOf(env8, ArmA38K)[cloudCompress]; got != len(cloud) {
		t.Errorf("ledger has %d compress rows, the fake saw %d", got, len(cloud))
	}
}

func TestLocalWindowsCountExactTokens(t *testing.T) {
	// At this density the agent's bytes/4 estimate would miss the server's window by
	// half, so any overflow here means an arm counted by bytes.
	for _, arm := range []string{ArmA38K, ArmA4, ArmT1} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.charsPerToken = 2
			env := f.env(0)
			rows := runSession(t, f, env, arm, shortSession())
			if n := len(ofKind(f.captures(), lab.SideLocal, "overflow")); n != 0 {
				t.Errorf("%d requests overflowed the server", n)
			}
			if rows[arm].Counters["overflow_recoveries"] != 0 || rows[arm].Error != "" {
				t.Errorf("row = %+v", rows[arm])
			}
		})
	}
}

func TestEveryLocalWindowHasTheCountingModel(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	s := shortSession()
	counted := func(p *participant) bool {
		_, ok := p.chat.(*lab.CountingModel)
		return ok
	}
	newRun := func(arm string) *sessionRun {
		return &sessionRun{env: env, arm: arm, sess: s, system: "x",
			tr: newTracker(arm, s.ID, s.Set, s.Length, s.Probes), rec: newToolRec(), replies: map[int]string{}}
	}
	for arm, want := range map[string]bool{ArmA0: false, ArmA38K: true, ArmA332K: true, ArmA6: false} {
		a, err := newHostArm(arm, newRun(arm))
		if err != nil {
			t.Fatal(err)
		}
		if counted(a.host) != want {
			t.Errorf("%s: counting model = %v", arm, !want)
		}
	}
	t1, err := newT1Arm(newRun(ArmT1), ArmT1)
	if err != nil {
		t.Fatal(err)
	}
	a4, err := newA4Arm(newRun(ArmA4), ArmA4)
	if err != nil {
		t.Fatal(err)
	}
	if !counted(t1.local) || counted(t1.cloud) || !counted(a4.local) || counted(a4.cloud) {
		t.Error("two-agent arms: the local agent counts exactly and the cloud agent does not")
	}
	for _, sec := range []*secondary{t1.withBrief, a4.abrupt} {
		if _, ok := sec.chat[corpus.SideLocal].(*lab.CountingModel); !ok {
			t.Errorf("%s: the local clone does not count exactly", sec.tr.arm)
		}
	}
	if err := t1.takeLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !counted(t1.local) {
		t.Error("the rebuilt local agent lost the counting model")
	}
	before := f.tokenizeCalls()
	runBench(t, f, env, ArmC3, benchItem())
	if f.tokenizeCalls() == before {
		t.Error("the C group's local agent never asked the tokenizer")
	}
}

func TestFailedBenchSummaryImportsOnlyTheDelta(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.forkStatus = func(int) string { return "incomplete" }
	item := benchItem()
	row := runBench(t, f, f.env(0), ArmC3, item)
	if row.Counters["fork_failed"] != 1 || row.Counters["fork_b"] != 0 || row.Counters["lossy_events"] == 0 {
		t.Errorf("counters %v", row.Counters)
	}
	probe := ofKind(f.captures(), lab.SideLocal, kindProbe)[0].Raw
	if bytes.Contains(probe, needle(item.History[0].User)) {
		t.Error("a failed summary gave local the whole history")
	}
	if !bytes.Contains(probe, needle(item.History[len(item.History)-1].User)) {
		t.Error("local did not get the delta")
	}
	if bytes.Contains(probe, []byte("FORKTEXT-")) {
		t.Error("the incomplete summary reached local")
	}
}

func TestPublishedTextIncludesEveryRound(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.toolPreface = true
	s := shortSession()
	runSession(t, f, f.env(0), ArmT1, s)
	first := -1
	for i, turn := range s.Turns {
		if turn.Owner == corpus.SideCloud && turn.ReadRecord != "" {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatal("no cloud read in this session")
	}
	want := []byte(fmt.Sprintf("PREFACE-%d", first))
	for _, c := range ofKind(f.captures(), lab.SideLocal, kindProbe) {
		if bytes.Contains(c.Raw, want) {
			return
		}
	}
	t.Errorf("the text before a tool call of turn %d was not published", first)
}

func TestElidedCompressionIsCountedAsLossy(t *testing.T) {
	long := strings.Repeat("x", 4*localSummaryTotal+400)
	args, err := json.Marshal(map[string]string{
		"task_overview": "o", "current_state": long, "important_discoveries": "d", "next_steps": "n", "context_to_preserve": "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := &fixedModel{responses: []*model.ChatResponse{
		{Content: []message.ContentBlock{message.ToolCallBlock{Type: "tool_call", ID: "c1", Name: structuredToolName, Input: string(args)}}, IsLast: true},
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "done"}}, IsLast: true},
	}}
	big := &bigCountModel{fixedModel: m}
	state := &agent.AgentState{SessionID: "elided", Context: []*message.Msg{
		message.UserMsg(agentName, "one"), message.AssistantMsg(agentName, []message.ContentBlock{message.TextBlock{Type: "text", Text: "two"}}),
		message.UserMsg(agentName, "three"), message.AssistantMsg(agentName, []message.ContentBlock{message.TextBlock{Type: "text", Text: "four"}}),
	}}
	var c Counters
	p := newParticipant(big, "s", tool.NewToolkit(), localConfig(Window8K, localSummaryTotal), state, &c, buildOpts{})
	if _, err := p.reply(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.Summary, elisionMarker) {
		t.Fatalf("summary was not elided: %.80q", state.Summary)
	}
	if c.Compress != 1 || c.LossyEvents != 1 {
		t.Errorf("counters %+v", c)
	}
}

func TestBaselinesGetTheSameSummaryBudget(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	s := shortSession()
	for arm, window := range map[string]int{ArmA38K: Window8K, ArmA332K: Window32K, ArmA6: CloudWindow} {
		run := &sessionRun{env: env, arm: arm, sess: s, system: "x",
			tr: newTracker(arm, s.ID, s.Set, s.Length, s.Probes), rec: newToolRec(), replies: map[int]string{}}
		a, err := newHostArm(arm, run)
		if err != nil {
			t.Fatal(err)
		}
		if a.host.cfg.ContextSize != window || a.host.cfg.SummaryMaxTokens != localSummaryTotal {
			t.Errorf("%s: %+v", arm, a.host.cfg)
		}
	}
}

func TestA6RecoversFromServerOverflowOnly(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	rows := runSession(t, f, env, ArmA6, shortSession())
	if len(ofKind(f.captures(), lab.SideLocal, "overflow")) == 0 {
		t.Error("the server never rejected a request")
	}
	if rows[ArmA6].Counters["overflow_recoveries"] == 0 {
		t.Error("no recovery counted")
	}
	overflow := 0
	for _, r := range env.Ledger.Rows() {
		if r.Overflow {
			overflow++
		}
	}
	if overflow == 0 {
		t.Error("no overflow row in the ledger")
	}
}

func TestA4WritesBriefOnACloneAndReceiverGrowsOnlyByIt(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	briefs := 0
	env.Trace = func(ev TraceEvent) {
		if ev.Kind != "brief" {
			return
		}
		briefs++
		if ev.After-ev.Before != 1 {
			t.Errorf("receiver grew by %d messages", ev.After-ev.Before)
		}
		last := ev.State.Context[len(ev.State.Context)-1]
		if text := last.GetTextContent(""); text == nil || !strings.Contains(*text, "BRIEFTEXT-") {
			t.Errorf("last message is not the brief")
		}
	}
	s := shortSession()
	runSession(t, f, env, ArmA4, s)
	if briefs == 0 {
		t.Fatal("no briefs")
	}
	caps := f.captures()
	for _, c := range caps {
		if c.Kind == kindMain && bytes.Contains(c.Raw, []byte("Write a handoff summary")) {
			t.Fatal("the handoff prompt reached a main call, so the brief did not run on a clone")
		}
	}
	// In each switch the abrupt clone answers first, without the new brief, and the
	// primary answers next, with it. Compare the two probes of each brief.
	for i, c := range caps {
		if c.Kind != kindBrief {
			continue
		}
		var probes []capture
		for _, later := range caps[i+1:] {
			if later.Kind == kindProbe {
				probes = append(probes, later)
			}
			if later.Kind == kindMain || len(probes) == 2 {
				break
			}
		}
		if len(probes) != 2 {
			t.Fatalf("%d probes after a brief", len(probes))
		}
		if bytes.Contains(probes[0].Raw, []byte(c.Text)) {
			t.Error("the abrupt clone saw the brief")
		}
		if !bytes.Contains(probes[1].Raw, []byte(c.Text)) {
			t.Error("the primary probe did not see the brief")
		}
	}
	// A brief has Fork B's limit on the cloud, and on the local side, which has no
	// reasoning to make room for, twice the target.
	for _, c := range caps {
		if c.Kind != kindBrief {
			continue
		}
		field, want := "max_output_tokens", forkBLimit
		if c.Side == lab.SideLocal {
			field, want = "max_completion_tokens", localBriefLimit
		}
		if got := bodyField(t, c.Raw, field); got != strconv.Itoa(want) {
			t.Errorf("%s brief: %s = %s", c.Side, field, got)
		}
		if got := bodyField(t, c.Raw, "tool_choice"); got != `"none"` {
			t.Errorf("%s brief: tool_choice = %s", c.Side, got)
		}
	}
}

func TestSamePromptForForkAndBrief(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	runSession(t, f, f.env(0), ArmT1, longSession())
	raw, err := json.Marshal(HandoffPrompt)
	if err != nil {
		t.Fatal(err)
	}
	text := raw[1 : len(raw)-1]
	kinds := map[string]bool{}
	for _, c := range f.captures() {
		if bytes.Contains(c.Raw, text) {
			kinds[c.Kind] = true
		}
	}
	if !kinds[kindFork] || !kinds[kindBrief] {
		t.Errorf("the prompt reached %v", kinds)
	}
	for _, want := range []string{"ignore any earlier instruction about reply style", "exact code", "newest value", "tool results", "2,500 tokens"} {
		if !strings.Contains(HandoffPrompt, want) {
			t.Errorf("HandoffPrompt lacks %q", want)
		}
	}
}

func TestProbeToolCallsAreBounded(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.probeCalls = 2
	f.probeTool = "read_record"
	env := f.env(0)
	s := shortSession()
	f.loadSession(s)
	run := &sessionRun{env: env, arm: ArmA0, sess: s, system: marker("test", ArmA0, s.ID) + "\n" + s.System,
		tr: newTracker(ArmA0, s.ID, s.Set, s.Length, s.Probes), rec: newToolRec(), replies: map[int]string{}}
	calls := 0
	read := func(corpus.Side, string) (string, bool) {
		calls++
		return "record text", true
	}
	tk := tool.NewToolkit(run.rec.recordTool(corpus.SideCloud, read))
	cloud := env.cloudTransport(ArmA0, s.ID, nil)
	chat, err := cloud.ChatModel()
	if err != nil {
		t.Fatal(err)
	}
	p := newParticipant(chat, run.system, tk, agent.ContextConfig{ContextSize: CloudWindow},
		&agent.AgentState{SessionID: "bound"}, &run.tr.c, buildOpts{})
	res, err := run.probe(context.Background(), p, chat, &run.tr.c, s.Probes[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != probeToolCalls {
		t.Errorf("tool ran %d times, want %d", calls, probeToolCalls)
	}
	if res == nil {
		t.Fatal("no result")
	}
	// The fake never answers, so this is the worst case: one reply and one repair, each
	// with its rounds, and the tool bound above holds across both.
	if got := len(ofKind(f.captures(), lab.SideCloud, kindProbe)); got > 2*(probeToolCalls+2) {
		t.Errorf("%d model rounds for one probe", got)
	}
	if run.rec.limit != 0 {
		t.Error("the bound stayed in place after the probe")
	}
}

// fixedModel returns scripted responses, for tests of what reply does with rounds.
type fixedModel struct {
	responses []*model.ChatResponse
	n         int
}

func (m *fixedModel) Chat(context.Context, []*message.Msg, ...model.CallOption) (*model.ChatResponse, error) {
	if m.n >= len(m.responses) {
		return nil, errors.New("script exhausted")
	}
	m.n++
	return m.responses[m.n-1], nil
}

func (m *fixedModel) ChatStream(context.Context, []*message.Msg, ...model.CallOption) (<-chan model.ChatResponse, error) {
	return nil, model.ErrStreamNotSupported
}

func (m *fixedModel) CountTokens([]*message.Msg, []model.ToolSchema) int { return 10 }

func TestReplyFinalIsTheLastRoundAndAllIsEveryRound(t *testing.T) {
	rec := newToolRec()
	tk := tool.NewToolkit(rec.recordTool(corpus.SideCloud, func(corpus.Side, string) (string, bool) { return "r", true }))
	m := &fixedModel{responses: []*model.ChatResponse{
		{Content: []message.ContentBlock{
			message.TextBlock{Type: "text", Text: "let me look"},
			message.ToolCallBlock{Type: "tool_call", ID: "c1", Name: "read_record", Input: `{"id":"x"}`},
		}, IsLast: true},
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: `{"answers":[]}`}}, IsLast: true},
	}}
	var c Counters
	p := newParticipant(m, "s", tk, agent.ContextConfig{ContextSize: CloudWindow}, &agent.AgentState{SessionID: "x"}, &c, buildOpts{})
	out, err := p.reply(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != `{"answers":[]}` {
		t.Errorf("Final = %q", out.Final)
	}
	if !strings.Contains(out.All, "let me look") || !strings.Contains(out.All, `{"answers":[]}`) {
		t.Errorf("All = %q", out.All)
	}
}

func TestNullCompressionIsCountedAsLossy(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.compressEmpty = true
	env := f.env(0)
	rows := runSession(t, f, env, ArmA38K, shortSession())
	c := rows[ArmA38K].Counters
	if c["compress"] == 0 || c["lossy_events"] == 0 {
		t.Errorf("counters %v", c)
	}
	if len(ofKind(f.captures(), lab.SideCloud, kindCompress)) == 0 {
		t.Error("no compression calls reached the cloud")
	}
}

func TestNullModelCountsLossyCompression(t *testing.T) {
	// Pins the agent's truncation text, which watchModel recognizes by value.
	m := &fixedModel{responses: []*model.ChatResponse{
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: ""}}, IsLast: true},
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: ""}}, IsLast: true},
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: ""}}, IsLast: true},
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "done"}}, IsLast: true},
	}}
	big := &bigCountModel{fixedModel: m}
	state := &agent.AgentState{SessionID: "lossy", Context: []*message.Msg{
		message.UserMsg(agentName, "one"), message.AssistantMsg(agentName, []message.ContentBlock{message.TextBlock{Type: "text", Text: "two"}}),
		message.UserMsg(agentName, "three"), message.AssistantMsg(agentName, []message.ContentBlock{message.TextBlock{Type: "text", Text: "four"}}),
	}}
	var c Counters
	tk := tool.NewToolkit()
	p := newParticipant(big, "s", tk, agent.ContextConfig{ContextSize: Window8K}, state, &c, buildOpts{})
	if _, err := p.reply(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if c.Compress != 1 || c.LossyEvents != 1 {
		t.Errorf("counters %+v", c)
	}
	if state.Summary != truncationNotice {
		t.Errorf("summary = %q, want the notice the classifier looks for", state.Summary)
	}
}

// bigCountModel reports a full window once, then a small one, so exactly one
// compression fires.
type bigCountModel struct {
	*fixedModel
	counted int
}

func (m *bigCountModel) CountTokens([]*message.Msg, []model.ToolSchema) int {
	m.counted++
	if m.counted == 1 {
		return Window8K
	}
	return 10
}

func TestSessionErrorScoresUnreachedBatchesZero(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	s := shortSession()
	f.loadSession(s)
	// A 400 is not retried, so the session stops on the first failed turn. By then the
	// first batches have run, and the rest stay in the denominator at zero.
	mainTurns := 0
	for i, turn := range s.Turns {
		if i > 0 && turn.Owner != s.Turns[i-1].Owner {
			f.failAfter = len(ofKind(f.captures(), lab.SideCloud, kindMain)) + 30
			break
		}
		mainTurns++
	}
	if mainTurns == 0 || f.failAfter == 0 {
		t.Fatal("the session never switches")
	}
	rows, err := env.Exec(context.Background(), Job{Arm: ArmA0, Session: &s})
	if err != nil {
		t.Fatal(err)
	}
	row := byArm(rows)[ArmA0]
	if row.Error == "" || row.Counters["session_errors"] != 1 {
		t.Fatalf("row = %+v", row)
	}
	var total, full int
	for _, b := range s.Probes {
		total += len(b.Questions)
	}
	if row.Batches != len(s.Probes) || row.Questions != total {
		t.Errorf("%d batches, %d questions; want all %d and %d in the denominator", row.Batches, row.Questions, len(s.Probes), total)
	}
	for _, c := range row.ByCategory {
		full += c[0]
	}
	if full == 0 || full == total {
		t.Errorf("%d of %d right: want the batches before the failure to count and the rest to be zero", full, total)
	}
}

func TestSecondaryRowsComeBeforeThePrimary(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	s := shortSession()
	f.loadSession(s)
	rows, err := env.Exec(context.Background(), Job{Arm: ArmT1, Session: &s})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Arm != ArmT1Brief || rows[1].Arm != ArmT1Read || rows[2].Arm != ArmT1 {
		t.Fatalf("rows = %s ... %s, %d of them", rows[0].Arm, rows[len(rows)-1].Arm, len(rows))
	}
}

func TestExposureReportsWhatTheOwnerSaw(t *testing.T) {
	f := newFake(t, modeNull, Window8K)
	rows := runSession(t, f, f.env(0), ArmA0, shortSession())
	r := rows[ArmA0]
	if r.ExposureTotal == 0 || r.ExposureSeen >= r.ExposureTotal {
		t.Errorf("exposure %d/%d: a null model reads nothing", r.ExposureSeen, r.ExposureTotal)
	}
}

func TestOwnRetentionIsPooledF5(t *testing.T) {
	s := shortSession()
	tr := newTracker(ArmT1, s.ID, s.Set, s.Length, s.Probes)
	for i, b := range s.Probes {
		res := score.Batch(b.Questions, oracleAnswers(b.Questions))
		tr.results[i] = &res
	}
	env := &Env{}
	row := tr.row(env, 1, 2)
	if row.OwnRetention == nil || *row.OwnRetention != 1 {
		t.Fatalf("own retention = %v", row.OwnRetention)
	}
	noF5 := []corpus.ProbeBatch{{Questions: []corpus.Question{{ID: "q", Category: corpus.F1, Answer: "A"}}}}
	tr = newTracker(ArmT1, s.ID, s.Set, s.Length, noF5)
	row = tr.row(env, 0, 0)
	if row.OwnRetention != nil {
		t.Errorf("own retention = %v with no F5 asked", *row.OwnRetention)
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"own_retention":null`) {
		t.Errorf("row json: %s", raw)
	}
	for _, key := range []string{"clears", "keeps", "fork_b", "fork_failed", "compress", "overflow_recoveries", "lossy_events", "session_errors"} {
		if !strings.Contains(string(raw), `"`+key+`":0`) {
			t.Errorf("row json lacks %s: %s", key, raw)
		}
	}
}

func TestC6ImportsToolResultsWithinTheDeltaBudget(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	item := benchItem()
	runBench(t, f, env, ArmC6, item)
	g := newFake(t, modeOracle, Window8K)
	runBench(t, g, g.env(0), ArmC3, item)

	k := forkPoint(len(item.History))
	var newest string
	for _, st := range slices.Backward(item.History[k+1:]) {
		if st.ToolName != "" {
			newest = st.ToolResult
			break
		}
	}
	if newest == "" {
		t.Fatal("no tool result in the delta")
	}
	probeOf := func(ff *fake) []byte { return ofKind(ff.captures(), lab.SideLocal, kindProbe)[0].Raw }
	c6, c3 := probeOf(f), probeOf(g)
	if !bytes.Contains(c6, needle(newest)) {
		t.Error("C6 did not import the newest tool result")
	}
	if bytes.Contains(c3, needle(newest)) {
		t.Error("C3 imported a tool result")
	}
}

func TestC6DeltaResultsFitTheBudgetNewestFirst(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	b := &benchRun{sessionRun: sessionRun{env: env}}
	ctx := context.Background()
	record := func(tag string) string { return tag + strings.Repeat(" padding", 600) }
	delta := []corpus.CStep{
		{User: "u1", ToolName: "read_record", RecordID: "R-1", ToolResult: record("OLDEST")},
		{User: "u2"},
		{User: "u3", ToolName: "read_record", RecordID: "R-2", ToolResult: record("MIDDLE")},
		{User: "u4", ToolName: "read_record", RecordID: "R-3", ToolResult: record("NEWEST")},
	}
	published := []*message.Msg{message.UserMsg("thread", strings.Repeat("published ", 80))}
	spent := env.Tokens.Count(ctx, *published[0].GetTextContent(""))
	got := b.deltaResults(ctx, published, delta)
	if len(got) != 2 {
		t.Fatalf("%d results", len(got))
	}
	first, second := *got[0].GetTextContent(""), *got[1].GetTextContent("")
	if !strings.Contains(first, "NEWEST") || !strings.Contains(second, "MIDDLE") {
		t.Errorf("order: %.60q then %.60q", first, second)
	}
	if !strings.Contains(second, "cut to the delta budget") {
		t.Error("the last result fits only in part and was not marked as cut")
	}
	total := spent + env.Tokens.Count(ctx, first) + env.Tokens.Count(ctx, second)
	if total > c6DeltaBudget {
		t.Errorf("%d tokens against a budget of %d", total, c6DeltaBudget)
	}
	if total < c6DeltaBudget*9/10 {
		t.Errorf("only %d of %d tokens used", total, c6DeltaBudget)
	}
}

func TestC8KeepsLocalOwnAndOthersClearIt(t *testing.T) {
	item := benchItem()
	own := item.LocalOwn[0].User
	raw, err := json.Marshal(own)
	if err != nil {
		t.Fatal(err)
	}
	needle := raw[1 : len(raw)-1]
	for arm, want := range map[string]bool{ArmC8: true, ArmC3: false} {
		f := newFake(t, modeOracle, Window8K)
		runBench(t, f, f.env(0), arm, item)
		got := bytes.Contains(ofKind(f.captures(), lab.SideLocal, kindProbe)[0].Raw, needle)
		if got != want {
			t.Errorf("%s: local's own turn in the probe = %v", arm, got)
		}
	}
}

func TestC7DoesNotReuseThePrefixAndC3Does(t *testing.T) {
	item := benchItem()
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	runBench(t, f, env, ArmC7, item)
	cold := ofKind(f.captures(), lab.SideCloud, kindCold)
	if len(cold) != 1 {
		t.Fatalf("%d cold requests", len(cold))
	}
	if tools := bodyField(t, cold[0].Raw, "tools"); tools != "" {
		t.Errorf("cold request carries tools: %s", tools)
	}
	// The trace reaches the model as one user message rendered from the same history.
	for _, st := range item.History[:forkPoint(len(item.History))] {
		if !bytes.Contains(cold[0].Raw, needle(st.User)) || !bytes.Contains(cold[0].Raw, needle(st.Reply)) {
			t.Fatal("the trace is not all in the cold request")
		}
		if st.ToolResult != "" && !bytes.Contains(cold[0].Raw, needle(st.ToolResult)) {
			t.Fatal("a tool result is not in the cold request")
		}
	}
	for _, r := range env.Ledger.Rows() {
		if r.Role == lab.RoleSmall && float64(r.Cached) > 0.5*float64(r.Input) {
			t.Errorf("cold request reused %d of %d tokens", r.Cached, r.Input)
		}
	}
	g := newFake(t, modeOracle, Window8K)
	envC3 := g.env(0)
	runBench(t, g, envC3, ArmC3, item)
	for _, r := range envC3.Ledger.Rows() {
		if r.Role == lab.RoleForkB && float64(r.Cached) < 0.5*float64(r.Input) {
			t.Errorf("fork reused only %d of %d tokens", r.Cached, r.Input)
		}
	}
}

func TestC4UsesTheLocalSectionOnly(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	runBench(t, f, f.env(0), ArmC4, benchItem())
	probe := ofKind(f.captures(), lab.SideLocal, kindProbe)[0].Raw
	if !bytes.Contains(probe, []byte("LOCALPART-")) {
		t.Error("the local section did not reach local")
	}
	if bytes.Contains(probe, []byte("CLOUDPART-")) {
		t.Error("the cloud section reached local")
	}
	if _, ok := localSection("no delimiters"); ok {
		t.Error("a reply without delimiters parsed")
	}
	if text, ok := localSection(mergedCloudMark + "\nc\n" + mergedLocalMark + "\nl\n" + mergedEndMark); !ok || text != "l" {
		t.Errorf("section = %q, %v", text, ok)
	}
}

func TestC3AAppliesForkAAndForksAgain(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	item := benchItem()
	env := f.env(0)
	row := runBench(t, f, env, ArmC3A, item)
	if row.Counters["fork_b"] != 2 || row.Counters["fork_a"] != 1 {
		t.Fatalf("counters %v", row.Counters)
	}
	caps := f.captures()
	forkA := ofKind(caps, lab.SideCloud, kindForkA)
	mains := ofKind(caps, lab.SideCloud, kindMain)
	if len(forkA) != 1 || len(mains) < 2 {
		t.Fatalf("%d Fork A, %d main calls", len(forkA), len(mains))
	}
	secondMain := mains[len(mains)-1]
	if !bytes.Contains(secondMain.Raw, []byte(forkA[0].Text)) {
		t.Error("the continuing cloud does not carry Fork A's text")
	}
	if len(secondMain.Raw) >= len(mains[0].Raw) {
		t.Errorf("replacement did not shrink the history: %d then %d", len(mains[0].Raw), len(secondMain.Raw))
	}
	forkBs := ofKind(caps, lab.SideCloud, kindFork)
	if ok, err := lab.PrefixEqual(secondMain.Raw, forkBs[1].Raw); err != nil || !ok {
		t.Errorf("second fork prefix: %v, %v", ok, err)
	}
	if !bytes.Contains(forkBs[1].Raw, []byte("FORKTEXT-1")) {
		t.Error("the second fork did not get the first summary")
	}
	probe := ofKind(caps, lab.SideLocal, kindProbe)[0].Raw
	if !bytes.Contains(probe, []byte("FORKTEXT-2")) {
		t.Error("local did not get the newest summary")
	}
}

func TestHistoryTailKeepsBothMinimums(t *testing.T) {
	long := strings.Repeat("x", 3*forkATailTokens)
	msgs := seedMsgs([]corpus.CStep{{User: "u0", Reply: long}, {User: "u1", Reply: "r1"}, {User: "u2", Reply: "r2"}, {User: "u3", Reply: "r3"}})
	tail := historyTail(msgs)
	if len(tail) != 7 {
		t.Errorf("tail of %d messages from %d", len(tail), len(msgs))
	}
}

func TestSeededHistoryHasTheAgentShape(t *testing.T) {
	msgs := seedMsgs([]corpus.CStep{{User: "u", ToolName: "read_record", ToolID: "call_1", ToolResult: "res", RecordID: "R-1", Reply: "r"}})
	if len(msgs) != 2 {
		t.Fatalf("%d messages", len(msgs))
	}
	blocks := msgs[1].GetContentBlocks()
	if len(blocks) != 3 {
		t.Fatalf("%d blocks", len(blocks))
	}
	call, ok := blocks[0].(message.ToolCallBlock)
	if !ok || call.ID != "call_1" || call.Input != `{"id":"R-1"}` {
		t.Errorf("call = %+v", blocks[0])
	}
	res, ok := blocks[1].(message.ToolResultBlock)
	if !ok || res.ID != "call_1" || res.Output != "res" {
		t.Errorf("result = %+v", blocks[1])
	}
}

func TestMalformedProbeGetsOneRepair(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.malformed = 1
	row := runSession(t, f, f.env(0), ArmA0, shortSession())[ArmA0]
	if row.ParseFailures != 0 {
		t.Errorf("parse failures = %d after a repair", row.ParseFailures)
	}
	if row.Counters["probe_repairs"] != 1 {
		t.Errorf("repairs = %d, want 1", row.Counters["probe_repairs"])
	}
	if row.Retention != 1 {
		t.Errorf("retention = %v, want 1", row.Retention)
	}
}

func TestSecondMalformedProbeStaysAParseFailure(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.malformed = 2
	row := runSession(t, f, f.env(0), ArmA0, shortSession())[ArmA0]
	if row.ParseFailures != 1 {
		t.Errorf("parse failures = %d, want 1", row.ParseFailures)
	}
	if row.Counters["probe_repairs"] != 1 {
		t.Errorf("repairs = %d, want 1", row.Counters["probe_repairs"])
	}
	if row.Retention == 1 {
		t.Error("a batch that never parsed still scored")
	}
}

func TestSummaryRequestsLowerTheEffortAfterThePrefix(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	runSession(t, f, f.env(0), ArmT1, longSession())
	type item struct {
		Type      string `json:"type"`
		Reasoning *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	seen := map[string]bool{}
	for _, c := range f.captures() {
		if c.Side != lab.SideCloud {
			continue
		}
		var body struct {
			Input []item `json:"input"`
		}
		if err := json.Unmarshal(c.Raw, &body); err != nil {
			t.Fatal(err)
		}
		at := slices.IndexFunc(body.Input, func(it item) bool { return it.Type == "configuration_update" })
		switch c.Kind {
		case kindFork, kindBrief:
			seen[c.Kind] = true
			if at != len(body.Input)-2 {
				t.Errorf("%s: configuration update at %d of %d", c.Kind, at, len(body.Input))
			} else if body.Input[at].Reasoning == nil || body.Input[at].Reasoning.Effort != forkEffort {
				t.Errorf("%s: wrong effort in %+v", c.Kind, body.Input[at])
			}
		default:
			if at >= 0 {
				t.Errorf("%s carries a configuration update", c.Kind)
			}
		}
	}
	if !seen[kindFork] || !seen[kindBrief] {
		t.Errorf("saw %v", seen)
	}
}

func TestLocalSystemExplainsTheLog(t *testing.T) {
	sys := localSystem("base", logRules, nil, "")
	if !strings.Contains(sys, logRules) || !strings.HasPrefix(sys, "base") {
		t.Errorf("system = %q", sys)
	}
}

// lastLocalMain returns the newest local request of a kind for an arm.
func lastLocal(f *fake, kind string) (capture, bool) {
	caps := f.captures()
	for i := len(caps) - 1; i >= 0; i-- {
		if caps[i].Side == lab.SideLocal && caps[i].Kind == kind {
			return caps[i], true
		}
	}
	return capture{}, false
}

func TestRecordRulesReachEveryLocalModelAndLogRulesOnlyLogArms(t *testing.T) {
	cases := []struct {
		arm           string
		record, logs  bool
		secondaryRows int
	}{
		{ArmA4, true, false, 1},
		{ArmA4S, true, false, 0},
		{ArmT1, true, true, 1},
	}
	for _, c := range cases {
		t.Run(c.arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			s := shortSession()
			f.loadSession(s)
			runSession(t, f, f.env(0), c.arm, s)
			probe, ok := lastLocal(f, kindProbe)
			if !ok {
				t.Fatal("no local probe")
			}
			if got := bytes.Contains(probe.Raw, []byte("[Records]")); got != c.record {
				t.Errorf("record rules = %v", got)
			}
			if got := bytes.Contains(probe.Raw, []byte("[How this session works]")); got != c.logs {
				t.Errorf("log rules = %v", got)
			}
			cloud := 0
			for _, cap := range f.captures() {
				if cap.Side != lab.SideCloud {
					continue
				}
				cloud++
				if bytes.Contains(cap.Raw, []byte("[Records]")) || bytes.Contains(cap.Raw, []byte("[How this session works]")) {
					t.Errorf("a %s cloud request holds local rules", cap.Kind)
				}
			}
			if cloud == 0 {
				t.Error("no cloud requests")
			}
		})
	}
	t.Run("the A3 baseline has none, A3-8K-h the part true of a host", func(t *testing.T) {
		f := newFake(t, modeOracle, Window8K)
		s := shortSession()
		f.loadSession(s)
		runSession(t, f, f.env(0), ArmA38K, s)
		probe, ok := lastLocal(f, kindProbe)
		if !ok || bytes.Contains(probe.Raw, []byte("[Records]")) {
			t.Errorf("A3-8K: rules reached the baseline (probe found: %v)", ok)
		}
		f = newFake(t, modeOracle, Window8K)
		f.loadSession(s)
		runSession(t, f, f.env(0), ArmA38KH, s)
		probe, ok = lastLocal(f, kindProbe)
		if !ok || !bytes.Contains(probe.Raw, []byte("[Records]")) || bytes.Contains(probe.Raw, []byte("reads only local records")) {
			t.Errorf("A3-8K-h: probe found %v", ok)
		}
	})
}

func TestA4sHoldsTheNewestLocalBriefInTheSystemPrompt(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	var systems []string
	env.Trace = func(ev TraceEvent) {
		if ev.Kind == "brief" && ev.System != "" {
			systems = append(systems, ev.System)
			if ev.After != ev.Before {
				t.Errorf("the slot changed the context: %d to %d", ev.Before, ev.After)
			}
			for _, m := range ev.State.Context {
				if txt := m.GetTextContent(""); txt != nil && strings.Contains(*txt, "BRIEFTEXT-") && strings.HasPrefix(*txt, "[Handoff brief from the cloud") {
					t.Errorf("a cloud brief reached the local context: %.60s", *txt)
				}
			}
		}
	}
	s := shortSession()
	f.loadSession(s)
	rows := runSession(t, f, env, ArmA4S, s)
	if _, extra := rows[ArmA4Abr]; extra || len(rows) != 1 {
		t.Errorf("rows = %d: A4s has no abrupt row", len(rows))
	}
	if len(systems) == 0 {
		t.Fatal("no local brief went to the slot")
	}
	for _, sys := range systems {
		if strings.Count(sys, briefPrefix) != 1 {
			t.Errorf("%d briefs in the slot, want the newest only", strings.Count(sys, briefPrefix))
		}
		if !strings.Contains(sys, "BRIEFTEXT-") || !strings.Contains(sys, recordRules) {
			t.Errorf("system = %.200s", sys)
		}
		if strings.Contains(sys, fmt.Sprintf(briefHeader, corpus.SideLocal)) {
			t.Errorf("local's own brief, written for the cloud, went to its slot: %.200s", sys)
		}
	}
}

func TestCompressionNeverSeesThePeerSummaryOrTheSlot(t *testing.T) {
	for _, arm := range []string{ArmT1, ArmA4S} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			env := f.env(0)
			var a armImpl
			s := shortSession()
			f.loadSession(s)
			run := &sessionRun{env: env, arm: arm, sess: s, system: "base", tr: newTracker(arm, s.ID, s.Set, s.Length, s.Probes), rec: newToolRec(), replies: map[int]string{}}
			a, err := newArm(arm, run)
			if err != nil {
				t.Fatal(err)
			}
			var p *participant
			switch x := a.(type) {
			case *t1Arm:
				x.peer = &summary{Text: "PEERSUMMARY", Coverage: 0}
				x.localLast = 1
				if err := x.takeLocal(context.Background()); err != nil {
					t.Fatal(err)
				}
				p = x.local
			case *a4Arm:
				if err := x.slotBrief(context.Background(), corpus.SideCloud, "SLOTBRIEF", true); err != nil {
					t.Fatal(err)
				}
				p = x.local
			}
			if !strings.Contains(p.system, "SLOTBRIEF") && !strings.Contains(p.system, "PEERSUMMARY") {
				t.Fatalf("the summary is not in the system prompt: %q", p.system)
			}
			if p.cfg.SummaryMaxTokens != summaryBudget(&summary{Text: "SLOTBRIEF"}) && p.cfg.SummaryMaxTokens != summaryBudget(&summary{Text: "PEERSUMMARY"}) {
				t.Errorf("SummaryMaxTokens = %d, which ignores the summary", p.cfg.SummaryMaxTokens)
			}
			if p.cfg.ReserveRatio <= localReserveRatio {
				t.Errorf("reserve = %v, which gives the summary no room", p.cfg.ReserveRatio)
			}
			msgs := []*message.Msg{message.SystemMsg("system", p.system), message.UserMsg("user", "hi")}
			out := p.watch.forCompression(msgs)
			txt := out[0].GetTextContent("")
			if txt == nil || strings.Contains(*txt, "SLOTBRIEF") || strings.Contains(*txt, "PEERSUMMARY") {
				t.Errorf("compression system = %v", txt)
			}
			if txt := msgs[0].GetTextContent(""); txt == nil || (!strings.Contains(*txt, "SLOTBRIEF") && !strings.Contains(*txt, "PEERSUMMARY")) {
				t.Error("forCompression changed the caller's message")
			}
			clone, err := p.clone(context.Background(), p.chat, &run.tr.c, buildOpts{})
			if err != nil {
				t.Fatal(err)
			}
			if clone.watch.compressSystem != p.compressSystem || clone.watch.compressSystem == "" {
				t.Error("a clone lost the compression system prompt")
			}
		})
	}
}

// captureChat records what a watchModel passes on.
type captureChat struct {
	model.ChatModel
	chat, count [][]*message.Msg
}

func (c *captureChat) Chat(_ context.Context, msgs []*message.Msg, _ ...model.CallOption) (*model.ChatResponse, error) {
	c.chat = append(c.chat, msgs)
	return &model.ChatResponse{}, nil
}

func (c *captureChat) CountTokens(msgs []*message.Msg, _ []model.ToolSchema) int {
	c.count = append(c.count, msgs)
	return 1
}

func TestWatchModelReplacesTheSystemOfCompressionCallsOnly(t *testing.T) {
	inner := &captureChat{}
	w := &watchModel{inner: inner, c: &Counters{}, state: &agent.AgentState{}, compressSystem: "CLEAN"}
	msgs := []*message.Msg{message.SystemMsg("system", "WITH PEER"), message.UserMsg("user", "hi")}
	compress := []model.ToolSchema{{Function: model.ToolFunction{Name: structuredToolName}}}
	other := []model.ToolSchema{{Function: model.ToolFunction{Name: "read_record"}}}
	system := func(m []*message.Msg) string {
		txt := m[0].GetTextContent("")
		if txt == nil {
			return ""
		}
		return *txt
	}
	if _, err := w.Chat(context.Background(), msgs, model.WithTools(compress)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Chat(context.Background(), msgs, model.WithTools(other)); err != nil {
		t.Fatal(err)
	}
	if got := system(inner.chat[0]); got != "CLEAN" {
		t.Errorf("compression Chat system = %q", got)
	}
	if got := system(inner.chat[1]); got != "WITH PEER" {
		t.Errorf("ordinary Chat system = %q", got)
	}
	w.CountTokens(msgs, compress)
	w.CountTokens(msgs, other)
	if got := system(inner.count[0]); got != "CLEAN" {
		t.Errorf("compression CountTokens system = %q", got)
	}
	if got := system(inner.count[1]); got != "WITH PEER" {
		t.Errorf("ordinary CountTokens system = %q", got)
	}
	if got := system(msgs); got != "WITH PEER" {
		t.Errorf("the caller's message changed to %q", got)
	}
}

// Compression on the wire, on every owner that holds a summary outside Context.
func TestLocalCompressionGoesOutWithItsBudgetAndWithoutThePeer(t *testing.T) {
	const peerText = "PEERMARK"
	check := func(t *testing.T, f *fake) {
		t.Helper()
		compressions, held := 0, 0
		for _, c := range f.captures() {
			if c.Side != lab.SideLocal {
				continue
			}
			if c.Kind != kindCompress {
				if bytes.Contains(c.Raw, []byte(peerText)) {
					held++
				}
				continue
			}
			compressions++
			if bytes.Contains(c.Raw, []byte(peerText)) {
				t.Errorf("a compression request holds the peer: %.200s", c.Raw)
			}
			if !bytes.Contains(c.Raw, []byte("[Summary budget]")) {
				t.Error("a compression request states no budget")
			}
		}
		if held == 0 {
			t.Fatal("the peer never reached a local request, so this proves nothing")
		}
		if compressions == 0 {
			t.Fatal("no local compression, so this proves nothing")
		}
	}
	for _, arm := range []string{ArmT1, ArmA4S} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.charsPerToken = 2
			f.forkText = func(int) string { return peerText }
			f.briefText = peerText
			runSession(t, f, f.env(0), arm, longSession())
			check(t, f)
		})
	}
	t.Run(ArmC8, func(t *testing.T) {
		f := newFake(t, modeOracle, Window8K)
		f.charsPerToken = 2
		f.forkText = func(int) string { return peerText }
		runBench(t, f, f.env(0), ArmC8, benchItem())
		check(t, f)
	})
}

func TestOwnerConfigGivesPeerSummaryItsOwnRoom(t *testing.T) {
	chat := &captureChat{}
	if c := ownerConfig(chat, nil); c.ReserveRatio != localReserveRatio || c.SummaryMaxTokens != localSummaryTotal {
		t.Errorf("no peer: %+v", c)
	}
	peer := &summary{Text: strings.Repeat("x", 6000)}
	c := ownerConfig(chat, peer)
	// captureChat counts every text as one token.
	if c.ReserveRatio != localReserveRatio+1.0/float64(Window8K) || c.SummaryMaxTokens != summaryBudget(peer) {
		t.Errorf("peer: %+v", c)
	}
	if len(chat.count) != 1 {
		t.Errorf("counted %d texts", len(chat.count))
	}
	f := newFake(t, modeOracle, Window8K)
	f.charsPerToken = 1
	chatCounted, err := f.env(0).localChat(ArmT1, "s")
	if err != nil {
		t.Fatal(err)
	}
	if c := ownerConfig(f.env(0).counted(chatCounted), &summary{Text: strings.Repeat("x", 20000)}); c.ReserveRatio != maxOwnerReserve {
		t.Errorf("a huge peer: reserve %v, want the cap", c.ReserveRatio)
	}
	sys := compressSystemFor("BASE", peer)
	if !strings.HasPrefix(sys, "BASE\n\n[Summary budget]") || !strings.Contains(sys, commas(summaryBudget(peer)*summaryBytesPerToken*8/10)) {
		t.Errorf("system = %q", sys)
	}
}

// A full PeerSummary takes window, not tail: the same history keeps about the same raw
// turns with one as without.
func TestPeerSummaryDoesNotShrinkTheRawTail(t *testing.T) {
	var steps []corpus.CStep
	for i := range 30 {
		steps = append(steps, corpus.CStep{User: fmt.Sprintf("step %d %s", i, strings.Repeat("u", 200)), Reply: strings.Repeat("r", 200)})
	}
	kept := func(peer *summary, oldReserve bool) int {
		f := newFake(t, modeOracle, Window8K)
		f.charsPerToken = 2
		env := f.env(0)
		chat, err := env.localChat(ArmT1, "s")
		if err != nil {
			t.Fatal(err)
		}
		base := "base"
		counted := env.counted(chat)
		cfg := ownerConfig(counted, peer)
		if oldReserve {
			cfg.ReserveRatio = localReserveRatio
		}
		st := &agent.AgentState{SessionID: "x", Context: seedMsgs(steps)}
		p := newParticipant(counted, localSystem(base, s1LogRules, peer, ""), tool.NewToolkit(), cfg, st, &Counters{},
			buildOpts{noSaver: true, compressSystem: compressSystemFor(localSystem(base, s1LogRules, nil, ""), peer)})
		if _, err := p.agent.Compact(context.Background()); err != nil {
			t.Fatal(err)
		}
		return len(st.Context)
	}
	full := &summary{Text: strings.Repeat("x", 3*forkBTarget-10)}
	without, with, old := kept(nil, false), kept(full, false), kept(full, true)
	if without == 0 {
		t.Fatal("the baseline kept nothing, so this proves nothing")
	}
	if with < without-2 {
		t.Errorf("kept %d messages with a peer, %d without", with, without)
	}
	if old >= with {
		t.Errorf("the plain reserve kept %d and the larger one %d: the larger one does nothing", old, with)
	}
}

func TestAnOldSummaryIsHeldToTheNewBudget(t *testing.T) {
	oldSummary := strings.Repeat("y", 12000)
	peerText := strings.Repeat("b", 7000)
	for _, arm := range []string{ArmT1, ArmA4S} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			s := shortSession()
			f.loadSession(s)
			run := &sessionRun{env: f.env(0), arm: arm, sess: s, system: "base", tr: newTracker(arm, s.ID, s.Set, s.Length, s.Probes), rec: newToolRec(), replies: map[int]string{}}
			a, err := newArm(arm, run)
			if err != nil {
				t.Fatal(err)
			}
			var p *participant
			switch x := a.(type) {
			case *t1Arm:
				x.local.state.Summary = oldSummary
				x.peer = &summary{Text: peerText, Coverage: 0}
				x.localLast = 1 // nothing new, so the arm keeps its context
				if err := x.takeLocal(context.Background()); err != nil {
					t.Fatal(err)
				}
				p = x.local
			case *a4Arm:
				x.local.state.Summary = oldSummary
				if err := x.slotBrief(context.Background(), corpus.SideCloud, peerText, true); err != nil {
					t.Fatal(err)
				}
				p = x.local
			}
			budget := summaryBudget(&summary{Text: peerText}) * summaryBytesPerToken
			if arm == ArmT1 {
				budget = summaryBudget(&summary{Text: peerText}) * summaryBytesPerToken
			}
			if got := len(p.state.Summary); got > budget || got == 0 {
				t.Errorf("own summary = %d bytes, budget %d", got, budget)
			}
			if run.tr.c.SummaryTrims != 1 || run.tr.c.LossyEvents != 1 {
				t.Errorf("counters = %+v", run.tr.c)
			}
		})
	}
}

func TestScriptedStepsArePricedAsMainCalls(t *testing.T) {
	b := &benchRun{live: map[int]bool{1: true}}
	b.replies = map[int]string{}
	b.prefix = 100
	step := corpus.CStep{User: strings.Repeat("u", 300), ToolResult: strings.Repeat("t", 600), Reply: strings.Repeat("r", 150)}
	added := lab.EstimateTokens(step.User) + lab.EstimateTokens(step.ToolResult)
	reply := lab.EstimateTokens(step.Reply)
	b.note(0, step)
	if want := lab.Luna.Cost(100+added, 100, added, reply); b.imputed != want {
		t.Errorf("imputed = %v, want %v", b.imputed, want)
	}
	first, prefix := b.imputed, b.prefix
	if prefix != 100+added+reply {
		t.Errorf("prefix = %d", prefix)
	}
	b.note(1, step)
	if b.imputed != first {
		t.Error("a live step was imputed, and the ledger has it as well")
	}
	if b.prefix != prefix+added+reply {
		t.Error("a live step did not grow the prefix")
	}

	f := newFake(t, modeOracle, Window8K)
	item := benchItem()
	c3 := runBench(t, f, f.env(0), ArmC3, item)
	f = newFake(t, modeOracle, Window8K)
	both := runBench(t, f, f.env(0), ArmC3A, item)
	if c3.ImputedMainUSD <= 0 || both.ImputedMainUSD <= 0 {
		t.Errorf("imputed: C3 %v, C3+A %v", c3.ImputedMainUSD, both.ImputedMainUSD)
	}
}

func TestAnS2RowWithNoScoreRunHasNoRetention(t *testing.T) {
	task := s2.Tasks()[0]
	row := (&Env{}).zeroRow(Job{Arm: ArmT1, Task: &task}, "boom")
	if row.S2 == nil || row.S2.Infra != "boom" {
		t.Fatalf("S2 = %+v", row.S2)
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["retention"]; !ok || v != nil {
		t.Errorf("retention = %v, want null", v)
	}
}

func TestACanceledTaskIsStillScored(t *testing.T) {
	task := s2.Tasks()[0]
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rows, err := f.env(0).execTask(ctx, ArmT1, task)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].S2 == nil || rows[0].S2.Infra != "" {
		t.Errorf("a canceled session lost its score: %+v", rows[0].S2)
	}
	if rows[0].S2.Total == 0 && !rows[0].S2.BuildFailed {
		t.Errorf("no hidden test ran: %+v", rows[0].S2)
	}
}

func TestHandoffVariantsAskThroughTheExistingCompactionPath(t *testing.T) {
	t.Run("C0h", func(t *testing.T) {
		f := newFake(t, modeOracle, Window8K)
		item := benchItem()
		f.loadBench(item)
		runBench(t, f, f.env(0), ArmC0H, item)
		found := false
		for _, c := range f.captures() {
			if c.Kind == kindCompress && c.Side == lab.SideCloud {
				found = true
				if !bytes.Contains(c.Raw, []byte("Write a handoff summary")) {
					t.Error("C0h did not use the handoff text")
				}
				if !bytes.Contains(c.Raw, []byte(`"effort":"low"`)) {
					t.Error("C0h did not lower the effort")
				}
			}
		}
		if !found {
			t.Fatal("no compression request")
		}
	})
	t.Run("C0 stays as it ships", func(t *testing.T) {
		f := newFake(t, modeOracle, Window8K)
		item := benchItem()
		f.loadBench(item)
		runBench(t, f, f.env(0), ArmC0, item)
		for _, c := range f.captures() {
			if c.Kind == kindCompress && c.Side == lab.SideCloud && bytes.Contains(c.Raw, []byte("Write a handoff summary")) {
				t.Error("C0 used the handoff text")
			}
		}
	})
}

func TestHandoffAllowanceLeavesRoomBeforeTheCut(t *testing.T) {
	if !strings.Contains(handoffPrompt(2500), "6,000 characters") {
		t.Errorf("prompt = %.200s", handoffPrompt(2500))
	}
	cut := 3 * forkBTarget
	if allowance := forkBTarget * 24 / 10; allowance*5/4 != cut {
		t.Errorf("allowance %d, cut %d", allowance, cut)
	}
	c := &Counters{}
	applySummary(&threadLog{}, strings.Repeat("x", cut-1), c)
	if c.SummaryTrims != 0 || c.LossyEvents != 0 {
		t.Errorf("a text at the cut was trimmed: %+v", c)
	}
	applySummary(&threadLog{}, strings.Repeat("x", 2*cut), c)
	if c.SummaryTrims != 1 || c.LossyEvents != 1 {
		t.Errorf("a trim counts once each way: %+v", c)
	}
}

func TestCheckSummaryMeasuresWhatAModelCannot(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n"
	text := strings.Repeat(line, 100) // 10,000 characters
	ok, fb := checkSummary(text, true)
	if ok {
		t.Fatal("too long passed")
	}
	for _, want := range []string{"10,000 characters", "7,500", "Line 75 of 101", "68% of this length"} {
		if !strings.Contains(fb, want) {
			t.Errorf("feedback lacks %q: %s", want, fb)
		}
	}
	if ok, _ := checkSummary(strings.Repeat("x", summaryCut()), true); !ok {
		t.Error("a text at the cut was rejected")
	}
	if ok, _ := checkSummary(strings.Repeat("x", summaryCut()+1), true); ok {
		t.Error("a text past the cut passed")
	}
	short := strings.Repeat("x", rewriteFloor()-1)
	if ok, _ := checkSummary(short, true); !ok {
		t.Error("a short first draft was rejected")
	}
	if ok, fb := checkSummary(short, false); ok || !strings.Contains(fb, "restored") {
		t.Errorf("a short rewrite: %v, %s", ok, fb)
	}
}

func TestFitSummaryBouncesFromTheDraftUpToTheLimit(t *testing.T) {
	over := strings.Repeat("x\n", 2*forkBTarget)
	fits := strings.Repeat("y", summaryAim())
	tooShort := strings.Repeat("z", rewriteFloor()-1)
	ctx := context.Background()
	script := func(replies ...string) (rewriter, *[]string) {
		var prompts []string
		i := 0
		return func(_ context.Context, prompt string) (string, error) {
			prompts = append(prompts, prompt)
			i++
			return replies[min(i, len(replies))-1], nil
		}, &prompts
	}

	rw, prompts := script("unused")
	c := &Counters{}
	if got, err := fitSummary(ctx, rw, "short", c); err != nil || got != "short" || len(*prompts) != 0 || c.SummaryRewrites != 0 {
		t.Errorf("a draft that fits: %q, %v, %d calls", got, err, len(*prompts))
	}

	rw, prompts = script(over+"more", tooShort, fits)
	c = &Counters{}
	got, err := fitSummary(ctx, rw, over, c)
	if err != nil || got != fits || c.SummaryRewrites != 3 {
		t.Errorf("got %d chars, %v, %d rewrites", len(got), err, c.SummaryRewrites)
	}
	for i, p := range *prompts {
		if !strings.HasSuffix(p, over) {
			t.Errorf("round %d did not work from the original draft", i)
		}
	}
	if !strings.Contains((*prompts)[2], commas(rewriteFloor()-1)+" characters") {
		t.Errorf("round 3 does not state the last attempt: %.300s", (*prompts)[2])
	}
	applySummary(&threadLog{}, got, c)
	if c.LossyEvents != 0 || c.SummaryTrims != 0 {
		t.Errorf("a rewrite that fits counted as lossy: %+v", c)
	}

	// Past the limit, the last attempt goes on to the cut, which counts as lossy.
	rw, prompts = script(over)
	c = &Counters{}
	got, err = fitSummary(ctx, rw, over, c)
	if err != nil || len(*prompts) != summaryRounds || c.SummaryRewrites != summaryRounds {
		t.Errorf("%v, %d calls, %d rewrites", err, len(*prompts), c.SummaryRewrites)
	}
	applySummary(&threadLog{}, got, c)
	if c.SummaryTrims != 1 || c.LossyEvents != 1 {
		t.Errorf("the cut: %+v", c)
	}

	// A failed or empty rewrite keeps what there is.
	for name, rw := range map[string]rewriter{
		"failed": func(context.Context, string) (string, error) { return "", errors.New("boom") },
		"empty":  func(context.Context, string) (string, error) { return "  ", nil },
	} {
		c := &Counters{}
		if got, err := fitSummary(ctx, rw, over, c); err != nil || got != over || c.SummaryRewrites != 1 {
			t.Errorf("%s: %d chars, %v, %+v", name, len(got), err, c)
		}
	}

	if _, err := fitSummary(ctx, func(context.Context, string) (string, error) { return "", lab.ErrBudget }, over, &Counters{}); !errors.Is(err, lab.ErrBudget) {
		t.Errorf("budget stop: %v", err)
	}
}

func TestForkBBouncesInItsOwnContextThroughTheAppendedTool(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	over := strings.Repeat("x\n", 2*forkBTarget)
	f.forkText = func(int) string { return over }
	env := f.env(0)
	rows := runSession(t, f, env, ArmT1, longSession())
	row := rows[ArmT1]
	if row.Error != "" {
		t.Fatal(row.Error)
	}
	if row.Counters["fork_b"] == 0 || row.Counters["summary_rewrites"] != row.Counters["fork_b"] {
		t.Fatalf("counters: %v", row.Counters)
	}
	if row.Counters["summary_trims"] != 0 || row.Counters["lossy_events"] != 0 {
		t.Errorf("a bounce that fits ended in a cut: %v", row.Counters)
	}
	var lastMain *capture
	caps := f.captures()
	bounces := 0
	for i := range caps {
		c := caps[i]
		if c.Side != lab.SideCloud {
			continue
		}
		switch c.Kind {
		case kindMain:
			lastMain = &caps[i]
		case kindBounce:
			bounces++
			if lastMain == nil {
				t.Fatal("a bounce before any main call")
			}
			if bodyField(t, lastMain.Raw, "tools") != bodyField(t, c.Raw, "tools") {
				t.Error("the appended tool changed the top-level list")
			}
			if !bytes.Contains(c.Raw, []byte(`"tool_search_output"`)) || !bytes.Contains(c.Raw, []byte("Rejected: ")) {
				t.Errorf("a bounce lacks the tool or the feedback: %.200s", c.Raw)
			}
			if got := strings.Count(string(c.Raw), `"configuration_update"`); got != 1 {
				t.Errorf("%d effort updates in one request", got)
			}
		}
	}
	if bounces == 0 {
		t.Fatal("no bounce reached the cloud")
	}
	for _, lr := range env.Ledger.Rows() {
		if lr.Arm == ArmT1 && lr.Role == lab.RoleShorten {
			t.Error("a fork used the draft-only path")
		}
	}
}

func TestBriefsBounceOnTheirOwnSide(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.briefText = strings.Repeat("x\n", 2*forkBTarget)
	env := f.env(0)
	rows := runSession(t, f, env, ArmA4, shortSession())
	row := rows[ArmA4]
	if row.Error != "" {
		t.Fatal(row.Error)
	}
	if row.Counters["summary_rewrites"] == 0 {
		t.Fatalf("counters: %v", row.Counters)
	}
	cloud, local := 0, 0
	for _, c := range f.captures() {
		switch {
		case c.Side == lab.SideCloud && c.Kind == kindBounce:
			cloud++
			if bytes.Contains(c.Raw, []byte(shortenOpening)) {
				t.Error("a cloud brief used the draft-only path")
			}
		case c.Side == lab.SideLocal && (c.Kind == kindShorten || c.Kind == kindBounce):
			local++
		}
	}
	if cloud == 0 {
		t.Error("a cloud brief did not bounce")
	}
	// A local brief goes to the cut, and the cut is lossy. Nothing checks a 4B rewrite.
	if local != 0 {
		t.Errorf("a local brief was rewritten %d times", local)
	}
	if row.Counters["summary_trims"] == 0 {
		t.Errorf("the local brief was not cut: %v", row.Counters)
	}
}

func TestFitSummaryRefusesARewriteHoldingANewCode(t *testing.T) {
	line := "restore point for the ledger service: AAA-1111\n"
	draft := strings.Repeat(line, summaryCut()/len(line)+50)
	calls := 0
	rewrite := func(context.Context, string) (string, error) {
		calls++
		return strings.Repeat(line, summaryAim()/len(line)) + "backup manifest for the ledger service: ZZZ-9999\n", nil
	}
	var c Counters
	got, err := fitSummary(context.Background(), rewrite, draft, &c)
	if err != nil {
		t.Fatal(err)
	}
	if got != draft || calls != summaryRounds {
		t.Errorf("kept %d characters after %d calls", len(got), calls)
	}

	honest := func(context.Context, string) (string, error) {
		return strings.Repeat(line, summaryAim()/len(line)), nil
	}
	got, err = fitSummary(context.Background(), honest, draft, &c)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) >= len(draft) {
		t.Errorf("an honest rewrite was refused: %d characters", len(got))
	}
}

func TestCompressionBudgetStopsWhereTheOutputLimitWould(t *testing.T) {
	got := compressSystemFor("base", nil)
	if !strings.Contains(got, commas(maxCompressChars)) {
		t.Errorf("no peer: %s", got)
	}
	peer := &summary{Text: strings.Repeat("x", 7497)}
	if want := summaryBudget(peer) * summaryBytesPerToken * 8 / 10; want >= maxCompressChars || !strings.Contains(compressSystemFor("base", peer), commas(want)) {
		t.Errorf("full peer wants %d: %s", want, compressSystemFor("base", peer))
	}
}

func TestEveryForkSummaryArmUsesTheCheckedFork(t *testing.T) {
	for _, arm := range []string{ArmC3, ArmC3A, ArmC4, ArmC6, ArmC8} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			row := runBench(t, f, f.env(0), arm, benchItem())
			if row.Error != "" {
				t.Fatal(row.Error)
			}
			submit := 0
			for _, c := range f.captures() {
				if c.Side == lab.SideCloud && bytes.Contains(c.Raw, []byte(`"tool_search_output"`)) {
					submit++
				}
			}
			if submit == 0 {
				t.Error("no checked fork")
			}
		})
	}
}

func TestShortenPromptStatesOneTarget(t *testing.T) {
	draft := strings.Repeat("x", 14000)
	if got := shortenPrompt(draft, 36, 0, false); !strings.Contains(got, "about 36% of its length, which is 5,040 characters") {
		t.Errorf("prompt = %.300s", got)
	}
	if got := shortenPrompt(draft, 36, 9000, true); !strings.Contains(got, "9,000") || !strings.Contains(got, "refused") {
		t.Errorf("prompt = %.500s", got)
	}
}

func TestRewriteUnderTheFloorGoesBack(t *testing.T) {
	over := strings.Repeat("x\n", 2*forkBTarget)
	short := strings.Repeat("y", rewriteFloor()/2)
	fits := strings.Repeat("z", rewriteFloor()+100)

	t.Run("fork", func(t *testing.T) {
		f := newFake(t, modeOracle, Window8K)
		f.forkText = func(int) string { return over }
		replies := []string{short, fits}
		f.bounceText = func() string {
			next := replies[0]
			if len(replies) > 1 {
				replies = replies[1:]
			}
			return next
		}
		row := runBench(t, f, f.env(0), ArmC3, benchItem())
		if row.Error != "" {
			t.Fatal(row.Error)
		}
		if row.Counters["summary_rewrites"] < 2 {
			t.Errorf("counters: %v", row.Counters)
		}
	})
	t.Run("clone", func(t *testing.T) {
		f := newFake(t, modeOracle, Window8K)
		f.briefText = over
		count := 0
		f.bounceText = func() string {
			count++
			if count == 1 {
				return short
			}
			return fits
		}
		env := f.env(0)
		row := runSession(t, f, env, ArmA4S, shortSession())[ArmA4S]
		if row.Error != "" {
			t.Fatal(row.Error)
		}
		if count < 2 {
			t.Errorf("rewrites asked for: %d", count)
		}
		// Counting rewrites spans several briefs, so what shows the floor held is that the
		// short one never reached the receiver.
		for _, c := range f.captures() {
			if c.Side == lab.SideLocal && bytes.Contains(c.Raw, []byte(short)) {
				t.Fatalf("a %s request holds the rewrite that was under the floor", c.Kind)
			}
		}
	})
}

// C0's template ends in the next steps. It alone is cut in the middle, and its rewrite goes
// through the same loop as the others.
func TestC0KeepsTheEndOfItsTemplate(t *testing.T) {
	head, tail := "HEADLINE\n", "\nNEXTSTEPS"
	over := head + strings.Repeat("x\n", 2*forkBTarget) + tail
	for arm, keepsEnd := range map[string]bool{ArmC0: true, ArmC0H: false} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.compressText = over
			f.shortenText = func() string { return over }
			env := f.env(0)
			row := runBench(t, f, env, arm, benchItem())
			if row.Error != "" {
				t.Fatal(row.Error)
			}
			if row.Counters["summary_trims"] == 0 {
				t.Fatalf("no cut: %v", row.Counters)
			}
			seen := false
			for _, c := range f.captures() {
				if c.Side == lab.SideLocal && bytes.Contains(c.Raw, []byte("HEADLINE")) {
					seen = true
					if bytes.Contains(c.Raw, []byte("NEXTSTEPS")) != keepsEnd {
						t.Errorf("end kept = %v", !keepsEnd)
					}
				}
			}
			if !seen {
				t.Fatal("the summary never reached local")
			}
		})
	}
}

func TestBriefRewriteThatDidNotCompleteLeavesTheEarlierText(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.briefText = strings.Repeat("x\n", 2*forkBTarget)
	f.bounceText = func() string { return "TRUNCATED-REWRITE" }
	f.bounceStatus = "incomplete"
	env := f.env(0)
	row := runSession(t, f, env, ArmA4S, shortSession())[ArmA4S]
	if row.Error != "" {
		t.Fatal(row.Error)
	}
	for _, c := range f.captures() {
		if c.Side == lab.SideLocal && bytes.Contains(c.Raw, []byte("TRUNCATED-REWRITE")) {
			t.Fatalf("a truncated rewrite reached local: %.200s", c.Raw)
		}
	}
	if row.Counters["summary_trims"] == 0 {
		t.Errorf("the earlier text should have been cut: %v", row.Counters)
	}
}

func TestDraftOnlyArmsBounceOnTheCloud(t *testing.T) {
	for _, arm := range []string{ArmC0, ArmC0H, ArmC7} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			over := strings.Repeat("x\n", 2*forkBTarget)
			f.compressText = over
			f.coldText = over
			env := f.env(0)
			row := runBench(t, f, env, arm, benchItem())
			if row.Error != "" {
				t.Fatal(row.Error)
			}
			if row.Counters["summary_rewrites"] == 0 || row.Counters["summary_trims"] != 0 {
				t.Errorf("counters: %v", row.Counters)
			}
		})
	}
}

func TestScorerFailureWritesANullRetention(t *testing.T) {
	raw, err := json.Marshal(Row{Arm: "T1", Session: "s2-01", Set: "s2", S2: &S2Outcome{Infra: "go: not found"}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["retention"]; !ok || v != nil {
		t.Errorf("retention = %v, present %v", v, ok)
	}
	if got["arm"] != "T1" || got["s2"] == nil {
		t.Errorf("row lost its fields: %s", raw)
	}
	raw, err = json.Marshal(Row{Arm: "T1", Retention: 0.5, S2: &S2Outcome{Passed: 1, Total: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["retention"] != 0.5 {
		t.Errorf("a scored row lost its retention: %s", raw)
	}
}

func TestA4CountsBriefsCompressionFolded(t *testing.T) {
	a := &a4Arm{
		run:       &sessionRun{tr: &tracker{}},
		cloud:     &participant{state: &agent.AgentState{}},
		local:     &participant{state: &agent.AgentState{Context: []*message.Msg{briefMsg(corpus.SideCloud, "kept"), message.UserMsg("u", "other")}}},
		delivered: map[corpus.Side]int{corpus.SideLocal: 3, corpus.SideCloud: 1},
	}
	if err := a.afterReply(context.Background(), corpus.Turn{}, replyOut{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := a.run.tr.c.BriefsCompressed; got != 3 {
		t.Errorf("briefs compressed = %d, want 3", got)
	}
}

func TestRepairForcesTheFormOnTheLocalSideOnly(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.malformed = 1
	s := shortSession()
	f.loadSession(s)
	schema := answerSchema(s.Probes[0].Questions)
	props := schema["properties"].(map[string]any)["answers"].(map[string]any)
	if props["minItems"] != len(s.Probes[0].Questions) {
		t.Errorf("minItems = %v", props["minItems"])
	}
	row := runSession(t, f, f.env(0), ArmA38K, s)[ArmA38K]
	if row.Counters["probe_repairs"] != 1 {
		t.Fatalf("repairs = %d, want 1", row.Counters["probe_repairs"])
	}
	forced := 0
	for _, c := range f.captures() {
		if !bytes.Contains(c.Raw, []byte(`"response_format"`)) {
			continue
		}
		forced++
		if c.Side != lab.SideLocal {
			t.Errorf("a %s call carried a response format", c.Side)
		}
		if got := bodyField(t, c.Raw, "tool_choice"); got != `"none"` {
			t.Errorf("tool_choice = %s", got)
		}
	}
	// The malformed answer may land on either side, because the fake answers the
	// first probe it sees. Only a local repair is forced.
	if forced > 1 {
		t.Errorf("%d forced calls", forced)
	}
}

func TestRowSplitsScoresByRecipient(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	row := runSession(t, f, f.env(0), ArmA4, shortSession())[ArmA4]
	pooled := map[corpus.Category][2]int{}
	for _, cats := range row.ByRecipient {
		for c, n := range cats {
			sum := pooled[c]
			sum[0] += n[0]
			sum[1] += n[1]
			pooled[c] = sum
		}
	}
	if !maps.Equal(pooled, row.ByCategory) {
		t.Errorf("split %v does not pool to %v", pooled, row.ByCategory)
	}
}

func TestTaskScoresComeFromTheHiddenTests(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	for _, arm := range TaskArms {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.loadTask(task, true)
			rows, err := f.env(0).Exec(context.Background(), Job{Arm: arm, Task: &task})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("%d rows, want 1: S2 has no secondary arms", len(rows))
			}
			row := rows[0]
			if row.Error != "" || row.S2 == nil || row.S2.Infra != "" {
				t.Fatalf("row = %+v", row)
			}
			if row.Retention != 1 || row.S2.Passed != row.S2.Total || row.S2.Total == 0 {
				t.Errorf("score %v, passed %d of %d, failed %v", row.Retention, row.S2.Passed, row.S2.Total, row.S2.Failed)
			}
		})
	}
}

func TestTaskWithNoWorkDoesNotPass(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, false)
	rows, err := f.env(0).Exec(context.Background(), Job{Arm: ArmT1, Task: &task})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].S2 == nil || rows[0].Retention == 1 {
		t.Errorf("row = %+v", rows[0])
	}
}

func TestRepairRoundsGoBackToTheLastOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	SetVariant(Variant{RepairRounds: 2})
	t.Cleanup(func() { SetVariant(Variant{}) })
	task := s2.Tasks()[0]
	owner := lab.SideLocal
	if task.Turns[len(task.Turns)-1].Owner == corpus.SideCloud {
		owner = lab.SideCloud
	}
	for _, arm := range []string{ArmA38K, ArmA4, ArmT1, ArmT1Read} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.loadTask(task, false)
			rows, err := f.env(0).Exec(context.Background(), Job{Arm: arm, Task: &task})
			if err != nil {
				t.Fatal(err)
			}
			got := rows[0].S2
			if rows[0].Error != "" || got == nil || got.First == nil || !got.BuildFailed || got.RepairRounds != 2 {
				t.Fatalf("error %q, outcome = %+v", rows[0].Error, got)
			}
			if got.First.Total == 0 || got.First.BuildOutput == "" || got.First.At == "" {
				t.Errorf("first = %+v", got.First)
			}
			asked := 0
			for _, c := range f.captures() {
				if bytes.Contains(c.Raw, []byte(s2.HiddenName)) {
					t.Errorf("a %s call names the hidden file", c.Side)
				}
				if c.Side == owner && bytes.Contains(c.Raw, []byte(repairLead)) {
					asked++
				}
			}
			if asked == 0 {
				t.Errorf("no %s call carried the compiler's errors", owner)
			}
		})
	}
}

func TestSessionsThatCompileGetNoRepairRound(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	SetVariant(Variant{RepairRounds: 2})
	t.Cleanup(func() { SetVariant(Variant{}) })
	task := s2.Tasks()[0]
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, true)
	rows, err := f.env(0).Exec(context.Background(), Job{Arm: ArmA38K, Task: &task})
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[0].S2; got == nil || got.First != nil || got.RepairRounds != 0 || got.Passed != got.Total {
		t.Fatalf("outcome = %+v", got)
	}
}

func TestRepairTextKeepsTheErrorAndHidesTheFile(t *testing.T) {
	got := repairText("# fixture_test [fixture.test]\n.\\zz_hidden_test.go:219:16: r.e.Breaching undefined (type *alerts.Engine has no field or method Breaching)\n.\\engine.go:3:1: undefined: fmt\nFAIL\tfixture [build failed]\n")
	for _, want := range []string{"acceptance check: r.e.Breaching undefined", `.\engine.go:3:1: undefined: fmt`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, not := range []string{"zz_hidden", "219:16", "build failed", "fixture_test"} {
		if strings.Contains(got, not) {
			t.Errorf("found %q in:\n%s", not, got)
		}
	}
}

func TestToolSummaryClipsLongArguments(t *testing.T) {
	log := &threadLog{}
	long := strings.Repeat("x", 3*argsMax)
	log.addTurn(corpus.SideCloud, corpus.Shared, "u", []toolCall{{Name: "write_file", Args: long}}, "r")
	if got := log.entries[1].Text; len(got) > argsMax+len("write_file ...") {
		t.Errorf("tool summary has %d characters", len(got))
	}
}

func TestCanonicalReplayVariantReachesTheCloudTransport(t *testing.T) {
	const local = `{"path": "a.go", "content": "x"}`
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	send := func() string {
		t.Helper()
		l, err := lab.NewLedger("r", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		e := &Env{Ledger: l, CloudURL: srv.URL, APIKey: "k", CloudModel: "m"}
		body := `{"model":"m","input":[{"type":"function_call","call_id":"chatcmpl-tool-1","name":"write_file","arguments":` + strconv.Quote(local) + `}]}`
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/responses", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := e.cloudTransport(ArmA8, "s", nil).RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		var sent struct {
			Input []struct {
				Arguments string `json:"arguments"`
			} `json:"input"`
		}
		if err := json.Unmarshal(got, &sent); err != nil {
			t.Fatal(err)
		}
		return sent.Input[0].Arguments
	}
	if a := send(); a != local {
		t.Errorf("default sent %s", a)
	}
	if _, on := Budgets()["canonical_replay"]; on {
		t.Error("the manifest claims canonical replay by default")
	}
	SetVariant(Variant{CanonicalReplay: true})
	t.Cleanup(func() { SetVariant(Variant{}) })
	if a := send(); a != `{"content":"x","path":"a.go"}` {
		t.Errorf("with the variant sent %s", a)
	}
	if Budgets()["canonical_replay"] != true {
		t.Error("the manifest does not record canonical replay")
	}
}
