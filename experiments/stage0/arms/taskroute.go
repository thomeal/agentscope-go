package arms

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

// taskRouteInstructions is the question a host would ask about a coding session. Like
// routeInstructions, it states the cost of each side and no rule from the set.
const taskRouteInstructions = `Two assistants serve this session and share one workspace. Decide which one should handle the user's new request. Prefer the local assistant whenever it can do the work correctly, because it is cheaper and keeps data on the device. Choose the cloud assistant when the work is too hard for a small model.`

var taskRouteChoices = map[string]string{
	string(corpus.SideCloud): "Large model. It handles hard reasoning and larger changes well.",
	string(corpus.SideLocal): "Small model with an 8K window. It can read and edit workspace files and run the tests.",
}

// TurnRoute is one turn's route. Side is what a run uses: the choice at or above the
// threshold, and otherwise the cloud, which is the explicit default a managed route needs.
type TurnRoute struct {
	Side       corpus.Side `json:"side"`
	Choice     string      `json:"choice,omitempty"`
	Confidence float64     `json:"confidence,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// TaskRoutes holds each task's routes by turn.
type TaskRoutes map[string][]TurnRoute

// RouteTasks asks a classifier about every turn from its query alone, which is all the managed
// path gives a classifier. It runs before the arms, so that every arm follows one set of routes.
func RouteTasks(ctx context.Context, e *Env, cl NamedClassifier, tasks []s2.Task, theta float64) (TaskRoutes, error) {
	routes := make(TaskRoutes, len(tasks))
	var (
		mu    sync.Mutex
		first error
		wg    sync.WaitGroup
	)
	for _, t := range tasks {
		routes[t.ID] = make([]TurnRoute, len(t.Turns))
		for i, turn := range t.Turns {
			wg.Go(func() {
				r, err := routeTurn(ctx, e, cl, t.ID, i, turn.User, theta)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if first == nil {
						first = err
					}
					return
				}
				routes[t.ID][i] = r
			})
		}
	}
	wg.Wait()
	return routes, first
}

func routeTurn(ctx context.Context, e *Env, cl NamedClassifier, task string, index int, query string, theta float64) (TurnRoute, error) {
	route := TurnRoute{Side: corpus.SideCloud}
	if cl.Cloud && e.overBudget() {
		return route, lab.ErrBudget
	}
	if cl.Sem != nil {
		select {
		case cl.Sem <- struct{}{}:
			defer func() { <-cl.Sem }()
		case <-ctx.Done():
			return route, ctx.Err()
		}
	}
	state, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		return route, err
	}
	session := task + "/" + strconv.Itoa(index)
	var (
		resp    *classifier.Response
		callErr error
		d       RouteDecision
	)
	for try := 1; ; try++ {
		begin := time.Now()
		resp, callErr = cl.C.Classify(ctx, classifier.Request{
			OperationID: opaqueID(e.Run, session) + "/" + cl.Name,
			State:       state,
			Questions:   map[string]classifier.Question{"route": &classifier.ChoiceQuestion{Instructions: taskRouteInstructions, Choices: taskRouteChoices}},
		})
		d.LatencyMS = time.Since(begin).Milliseconds()
		if ctx.Err() != nil {
			return route, ctx.Err()
		}
		chargeClassifier(e, session, cl, resp, callErr, &d, try)
		if callErr == nil || try == routeClassifyTries || !model.IsRetryableError(callErr) {
			break
		}
		select {
		case <-time.After(routeClassifyBackoff * time.Duration(try)):
		case <-ctx.Done():
			return route, ctx.Err()
		}
	}
	if callErr != nil {
		route.Error = scrub(callErr.Error(), e.APIKey)
		return route, nil
	}
	a, ok := resp.Answers["route"].(*classifier.ChoiceAnswer)
	if !ok {
		route.Error = "no choice in the answers"
		return route, nil
	}
	route.Choice, route.Confidence = a.Choice, a.Confidence
	if a.Confidence >= theta && a.Choice == string(corpus.SideLocal) {
		route.Side = corpus.SideLocal
	}
	return route, nil
}

// ApplyRoutes gives tasks the owners in routes, without touching the set's own turns.
func ApplyRoutes(tasks []s2.Task, routes TaskRoutes) ([]s2.Task, error) {
	out := make([]s2.Task, len(tasks))
	for i, t := range tasks {
		r, ok := routes[t.ID]
		if !ok || len(r) != len(t.Turns) {
			return nil, fmt.Errorf("routes for %s have %d turns and the task has %d", t.ID, len(r), len(t.Turns))
		}
		turns := slices.Clone(t.Turns)
		for j := range turns {
			turns[j].Owner = r[j].Side
		}
		t.Turns = turns
		out[i] = t
	}
	return out, nil
}
