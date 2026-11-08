package arms

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/thread"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/thread/route"
)

// fixedRoute is a classifier that always picks target with the given confidence.
func fixedRoute(target, other string, confidence float64) func(string, string) (classifier.DispatchClassifier, error) {
	return func(string, string) (classifier.DispatchClassifier, error) {
		return classifier.NewFake(&classifier.FakeConfig{Response: classifier.Response{
			Model: "fake-router",
			Answers: map[string]classifier.Answer{route.QuestionID: &classifier.ChoiceAnswer{
				Choice: target, Confidence: confidence,
				Probabilities: map[string]float64{target: confidence, other: 1 - confidence},
			}},
		}}), nil
	}
}

func TestFixedThreadRouteRefusesOtherNames(t *testing.T) {
	if _, err := FixedThreadRoute("local"); err == nil {
		t.Error("a name other than edge or cloud was accepted")
	}
	for _, name := range []string{threadEdge, threadCloud} {
		if _, err := FixedThreadRoute(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestMTHybridRunsTheShippedThread drives one S2 task through the Coordinator with the
// router sending every turn to one side, and scores it from the hidden tests.
func TestMTHybridRunsTheShippedThread(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	for _, tc := range []struct {
		target, other, side string
	}{
		{threadEdge, threadCloud, lab.SideLocal},
		{threadCloud, threadEdge, lab.SideCloud},
	} {
		t.Run(tc.target, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.loadTask(task, true)
			env := f.env(0)
			env.ThreadClassifier = fixedRoute(tc.target, tc.other, 0.95)
			rows, err := env.Exec(context.Background(), Job{Arm: ArmMTHybrid, Task: &task})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("%d rows, want 1", len(rows))
			}
			row := rows[0]
			if row.Error != "" || row.S2 == nil || row.S2.Infra != "" {
				t.Fatalf("row = %+v", row)
			}
			if row.Retention != 1 || row.S2.Passed != row.S2.Total || row.S2.Total == 0 {
				t.Errorf("score %v, passed %d of %d, failed %v", row.Retention, row.S2.Passed, row.S2.Total, row.S2.Failed)
			}
			if got := row.Counters["mt_route_"+tc.target]; got != len(task.Turns) {
				t.Errorf("%d turns routed to %s, want %d; counters %v", got, tc.target, len(task.Turns), row.Counters)
			}
			if row.Counters["mt_route_by_classifier"] != len(task.Turns) {
				t.Errorf("counters %v", row.Counters)
			}
			if len(row.ThreadErrors) > 0 {
				t.Errorf("thread errors %v", row.ThreadErrors)
			}
			mains := 0
			for _, c := range f.captures() {
				if c.Side == tc.side {
					mains++
				}
			}
			if mains == 0 {
				t.Errorf("no %s request", tc.side)
			}
			if tc.side == lab.SideCloud && row.CostUSD <= 0 {
				t.Errorf("cloud turns cost %v", row.CostUSD)
			}
		})
	}
}

// TestMTHybridLowConfidenceGoesToTheCloud checks the hybrid default: an answer under the
// threshold sends the query to the cloud, even when it leaned towards the edge.
func TestMTHybridLowConfidenceGoesToTheCloud(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, true)
	env := f.env(0)
	env.ThreadClassifier = fixedRoute(threadEdge, threadCloud, 0.6)
	rows, err := env.Exec(context.Background(), Job{Arm: ArmMTHybrid, Task: &task})
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row.Error != "" || row.Retention != 1 {
		t.Fatalf("row = %+v", row)
	}
	if row.Counters["mt_route_"+threadCloud] != len(task.Turns) || row.Counters["mt_route_by_default"] != len(task.Turns) {
		t.Errorf("counters %v", row.Counters)
	}
	for _, c := range f.captures() {
		if c.Side == lab.SideLocal {
			t.Errorf("a local request with every turn on the cloud: %.200s", c.Raw)
		}
	}
}

// stateRecorder keeps every classifier state it forwards.
type stateRecorder struct {
	classifier.DispatchClassifier
	mu     sync.Mutex
	states []json.RawMessage
}

func (r *stateRecorder) ClassifyWithDispatch(ctx context.Context, req classifier.Request, limits classifier.DispatchLimits, begin func(classifier.AttemptKind) error) (*classifier.Response, error) {
	r.mu.Lock()
	r.states = append(r.states, slices.Clone(req.State))
	r.mu.Unlock()
	return r.DispatchClassifier.ClassifyWithDispatch(ctx, req, limits, begin)
}

// TestMTHybridClassifierSeesHistory keeps the arm's recorded meaning: its router's
// classifier reads the thread state, history included, not the query alone.
func TestMTHybridClassifierSeesHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, true)
	env := f.env(0)
	rec := &stateRecorder{}
	env.ThreadClassifier = func(arm, session string) (classifier.DispatchClassifier, error) {
		c, err := fixedRoute(threadCloud, threadEdge, 0.95)(arm, session)
		rec.DispatchClassifier = c
		return rec, err
	}
	rows, err := env.Exec(context.Background(), Job{Arm: ArmMTHybrid, Task: &task})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Error != "" {
		t.Fatalf("row = %+v", rows[0])
	}
	if len(rec.states) != len(task.Turns) {
		t.Fatalf("%d classifier states for %d turns", len(rec.states), len(task.Turns))
	}
	for i, raw := range rec.states {
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("state %d: %v", i, err)
		}
		if _, ok := doc["recent"]; !ok {
			t.Errorf("state %d has no history: %.300s", i, raw)
		}
	}
	if last := string(rec.states[len(rec.states)-1]); !strings.Contains(last, `"lag"`) {
		t.Errorf("the last state carries no lag: %.300s", last)
	}
}

func TestFillClassifierRowPricesInputAndCache(t *testing.T) {
	var row lab.Row
	fillClassifierRow(&row, []byte(`{"model":"gpt-6-luna","answers":[],"usage":{"input_tokens":1000,"input_tokens_details":{"cached_tokens":400,"cache_write_tokens":100},"output_tokens":0,"total_tokens":1000}}`))
	if row.UsageMissing || row.Input != 1000 || row.Cached != 400 || row.Write != 100 || row.Output != 0 {
		t.Fatalf("row = %+v", row)
	}
	want := (500*0.10 + 400*0.01 + 100*0.125) / 1e6
	if diff := row.CostUSD - want; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("cost %v, want %v", row.CostUSD, want)
	}
	var missing lab.Row
	fillClassifierRow(&missing, []byte(`{"error":{"message":"x"}}`))
	if !missing.UsageMissing || missing.CostUSD != 0 {
		t.Errorf("row without usage = %+v", missing)
	}
	if strings.Contains(row.Model, "fake") {
		t.Error("model not read")
	}
}

// TestMTHybridRepairGoesThroughTheRouter: a session that does not compile gets the
// compiler's errors as further user messages, which the router sends like any other.
func TestMTHybridRepairGoesThroughTheRouter(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	SetVariant(Variant{RepairRounds: 2})
	t.Cleanup(func() { SetVariant(Variant{}) })
	task := s2.Tasks()[0]
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, false)
	env := f.env(0)
	env.ThreadClassifier = fixedRoute(threadEdge, threadCloud, 0.95)
	rows, err := env.Exec(context.Background(), Job{Arm: ArmMTHybrid, Task: &task})
	if err != nil {
		t.Fatal(err)
	}
	got := rows[0].S2
	if rows[0].Error != "" || got == nil || got.First == nil || !got.BuildFailed || got.RepairRounds != 2 {
		t.Fatalf("error %q, outcome = %+v", rows[0].Error, got)
	}
	if n := rows[0].Counters["mt_route_"+threadEdge]; n != len(task.Turns)+2 {
		t.Errorf("%d routed turns, want the %d scripted ones and 2 repairs", n, len(task.Turns))
	}
	asked := 0
	for _, c := range f.captures() {
		if strings.Contains(string(c.Raw), s2.HiddenName) {
			t.Errorf("a %s call names the hidden file", c.Side)
		}
		if c.Side == lab.SideLocal && strings.Contains(string(c.Raw), repairLead) {
			asked++
		}
	}
	if asked == 0 {
		t.Error("no edge call carried the compiler's errors")
	}
}

// routedTask is the first S2 task with turn 1 given to the cloud and every other turn to
// the edge, apart from the last one, which goes to the cloud when lastCloud is set. The
// split is lopsided so that a rule mapping the sides the wrong way round shows in counts.
func routedTask(lastCloud bool) (s2.Task, int) {
	task := s2.Tasks()[0]
	task.Turns = slices.Clone(task.Turns)
	cloud := 0
	for i := range task.Turns {
		task.Turns[i].Owner = corpus.SideLocal
		if i == 1 || (i == len(task.Turns)-1 && lastCloud) {
			task.Turns[i].Owner = corpus.SideCloud
			cloud++
		}
	}
	return task, cloud
}

// TestMTRoutedFollowsTheRoutes: every turn goes to the owner the task's routes give it, by
// the host rule, and no classifier is asked.
func TestMTRoutedFollowsTheRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task, cloud := routedTask(true)
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, true)
	env := f.env(0)
	env.ThreadClassifier = func(string, string) (classifier.DispatchClassifier, error) {
		t.Fatal("MT-routed asked for a classifier")
		return nil, nil
	}
	rows, err := env.Exec(context.Background(), Job{Arm: ArmMTRouted, Task: &task})
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row.Error != "" || row.S2 == nil || row.Retention != 1 || len(row.ThreadErrors) > 0 {
		t.Fatalf("row = %+v", row)
	}
	c := row.Counters
	if c["mt_route_cloud"] != cloud || c["mt_route_edge"] != len(task.Turns)-cloud || c["mt_route_by_rule"] != len(task.Turns) || c["mt_classifier_calls"] != 0 {
		t.Errorf("want %d cloud turns of %d, all by rule; counters %v", cloud, len(task.Turns), c)
	}
	sides := map[string]int{}
	for _, cp := range f.captures() {
		sides[cp.Side]++
	}
	if sides[lab.SideLocal] == 0 || sides[lab.SideCloud] == 0 {
		t.Errorf("requests by side %v, want both", sides)
	}
}

// TestMTRoutedRepairGoesToTheLastOwner: repair turns go where A3-8K sends them, to the
// owner of the last scripted turn.
func TestMTRoutedRepairGoesToTheLastOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	SetVariant(Variant{RepairRounds: 2})
	t.Cleanup(func() { SetVariant(Variant{}) })
	for _, lastCloud := range []bool{true, false} {
		task, cloud := routedTask(lastCloud)
		f := newFake(t, modeOracle, Window8K)
		f.loadTask(task, false)
		rows, err := f.env(0).Exec(context.Background(), Job{Arm: ArmMTRouted, Task: &task})
		if err != nil {
			t.Fatal(err)
		}
		got := rows[0]
		if got.Error != "" || got.S2 == nil || got.S2.RepairRounds != 2 {
			t.Fatalf("last cloud %v: row = %+v", lastCloud, got)
		}
		want := cloud
		if lastCloud {
			want += 2
		}
		if n := got.Counters["mt_route_cloud"]; n != want || got.Counters["mt_route_by_rule"] != len(task.Turns)+2 {
			t.Errorf("last cloud %v: %d cloud turns, want %d, all by rule; counters %v", lastCloud, n, want, got.Counters)
		}
	}
}

func TestScriptedRoute(t *testing.T) {
	turns := []corpus.Turn{{Owner: corpus.SideLocal}, {Owner: corpus.SideCloud}}
	rule := scriptedRoute(turns)
	for id, want := range map[string]string{"turn-0": threadEdge, "turn-1": threadCloud, "turn-5": threadCloud} {
		if got, ok := rule(thread.RouteRequest{OperationID: id}); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", id, got, ok, want)
		}
	}
	for _, id := range []string{"", "repair", "turn--1"} {
		if got, ok := rule(thread.RouteRequest{OperationID: id}); ok {
			t.Errorf("%q decided %q", id, got)
		}
	}
	if _, ok := scriptedRoute(nil)(thread.RouteRequest{OperationID: "turn-0"}); ok {
		t.Error("a rule without turns decided")
	}
}

// roleModel records the ledger role each call would be booked under.
type roleModel struct {
	model.ChatModel
	roles []string
}

func (r *roleModel) Chat(ctx context.Context, _ []*message.Msg, _ ...model.CallOption) (*model.ChatResponse, error) {
	r.roles = append(r.roles, lab.RoleFrom(ctx))
	return &model.ChatResponse{}, nil
}

func TestForkTaggerMarksOnlyToolChoiceNone(t *testing.T) {
	inner := &roleModel{}
	f := forkTagger{inner}
	ctx := context.Background()
	_, _ = f.Chat(ctx, nil)
	_, _ = f.Chat(ctx, nil, model.WithToolChoice(&model.ToolChoice{Mode: "auto"}))
	_, _ = f.Chat(ctx, nil, model.WithToolChoice(&model.ToolChoice{Mode: "none"}))
	want := []string{"", "", roleThreadFork}
	if strings.Join(inner.roles, ",") != strings.Join(want, ",") {
		t.Errorf("roles %q, want %q", inner.roles, want)
	}
}
