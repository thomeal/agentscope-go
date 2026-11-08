package arms

import (
	"context"
	"errors"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

type countingChat struct {
	model.ChatModel
	calls int
}

func (c *countingChat) Chat(context.Context, []*message.Msg, ...model.CallOption) (*model.ChatResponse, error) {
	c.calls++
	return &model.ChatResponse{}, nil
}

func TestHardLimitSendsWhatDoesNotFitToTheCloud(t *testing.T) {
	cloud, local := &countingChat{}, &countingChat{}
	tokens := 100
	s := &sharedHost{cloud: cloud, local: local, owner: corpus.SideLocal, limit: 100,
		count: func(context.Context, []*message.Msg, []model.ToolSchema) int { return tokens }}
	if _, err := s.Chat(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if local.calls != 1 || cloud.calls != 0 {
		t.Fatalf("at the limit: local %d, cloud %d", local.calls, cloud.calls)
	}
	tokens = 101
	if _, err := s.Chat(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if local.calls != 1 || cloud.calls != 1 || s.spilled.Load() != 1 {
		t.Fatalf("over the limit: local %d, cloud %d, spilled %d", local.calls, cloud.calls, s.spilled.Load())
	}
	s.owner = corpus.SideCloud
	if _, err := s.Chat(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if s.spilled.Load() != 1 {
		t.Errorf("a cloud turn counted as spilled: %d", s.spilled.Load())
	}
}

func TestA8RunsOnATask(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, true)
	rows, err := f.env(0).Exec(context.Background(), Job{Arm: ArmA8, Task: &task})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Error != "" || rows[0].Retention != 1 {
		t.Errorf("row = %+v", rows[0])
	}
}

func TestRouteTasksGoesToTheCloudUnlessSureOfLocal(t *testing.T) {
	task := s2.Task{ID: "t", Turns: []s2.Turn{{User: "one"}, {User: "two"}}}
	env := newFake(t, modeOracle, Window8K).env(0)
	// The stub answers at a confidence of 0.9.
	cases := []struct {
		name  string
		stub  *stubClassifier
		theta float64
		want  corpus.Side
	}{
		{"sure of local", &stubClassifier{choice: "local"}, .8, corpus.SideLocal},
		{"unsure of local", &stubClassifier{choice: "local"}, .95, corpus.SideCloud},
		{"sure of cloud", &stubClassifier{choice: "cloud"}, .8, corpus.SideCloud},
		{"failed", &stubClassifier{err: errors.New("refused")}, .8, corpus.SideCloud},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl := NamedClassifier{Name: "stub", C: c.stub, Cloud: true, Prices: lab.Luna}
			routes, err := RouteTasks(context.Background(), env, cl, []s2.Task{task}, c.theta)
			if err != nil {
				t.Fatal(err)
			}
			for i, r := range routes["t"] {
				if r.Side != c.want {
					t.Errorf("turn %d: %+v", i, r)
				}
			}
		})
	}
}

func TestApplyRoutesLeavesTheSetAlone(t *testing.T) {
	task := s2.Task{ID: "t", Turns: []s2.Turn{{Owner: corpus.SideCloud}, {Owner: corpus.SideCloud}}}
	got, err := ApplyRoutes([]s2.Task{task}, TaskRoutes{"t": {{Side: corpus.SideLocal}, {Side: corpus.SideCloud}}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Turns[0].Owner != corpus.SideLocal || task.Turns[0].Owner != corpus.SideCloud {
		t.Errorf("routed %v, set %v", got[0].Turns[0].Owner, task.Turns[0].Owner)
	}
	if _, err := ApplyRoutes([]s2.Task{task}, TaskRoutes{"t": {{Side: corpus.SideLocal}}}); err == nil {
		t.Error("routes of the wrong length were accepted")
	}
}
