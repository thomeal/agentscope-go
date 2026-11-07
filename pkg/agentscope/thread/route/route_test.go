package route

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/thread"
)

func TestNewValidatesConfig(t *testing.T) {
	var nilFake *classifier.Fake
	ok := Config{Mode: Hybrid, Edge: "edge", Cloud: "cloud"}
	cases := []struct {
		name string
		edit func(c *Config)
	}{
		{"blank edge", func(c *Config) { c.Edge = " " }},
		{"blank cloud", func(c *Config) { c.Cloud = "" }},
		{"slash in name", func(c *Config) { c.Edge = "edge/master" }},
		{"same names", func(c *Config) { c.Cloud = "edge" }},
		{"empty mode", func(c *Config) { c.Mode = "" }},
		{"unknown mode", func(c *Config) { c.Mode = "edge_first" }},
		{"negative threshold", func(c *Config) { c.Threshold = -0.1 }},
		{"threshold above one", func(c *Config) { c.Threshold = 1.5 }},
		{"NaN threshold", func(c *Config) { c.Threshold = math.NaN() }},
		{"negative budget", func(c *Config) { c.Budget.Recent = -1 }},
		{"budget without room", func(c *Config) { c.Budget = StateBudget{Query: 3000, Participants: 3000, Total: 6000} }},
		{"negative request limit", func(c *Config) { c.Limits.MaxRequestBytes = -1 }},
		{"negative response limit", func(c *Config) { c.Limits.MaxResponseBytes = -1 }},
		{"nil rule", func(c *Config) { c.Rules = []Rule{nil} }},
		{"nil classifier pointer", func(c *Config) { c.Classifier = nilFake }},
		{"remote classifier not hosted", func(c *Config) { c.Classifier = &stub{endpoint: "https://api.typesafe.ai/v1/systemone"} }},
		{"LAN classifier not hosted", func(c *Config) { c.Classifier = &stub{endpoint: "http://192.168.1.5:8080/v1/systemone"} }},
		{"unparsable endpoint not hosted", func(c *Config) { c.Classifier = &stub{endpoint: "http://[::1"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ok
			tc.edit(&cfg)
			if _, err := New(cfg); !errors.Is(err, ErrConfig) {
				t.Fatalf("New error = %v, want ErrConfig", err)
			}
		})
	}
}

func TestNewAcceptsOnDeviceEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"fake://classifier",
		"http://127.0.0.1:8080/v1/systemone",
		"https://127.0.0.2/v1/systemone",
		"http://localhost:8080/v1/systemone",
		"HTTP://LOCALHOST:8080/v1/systemone",
		"http://[::1]:8080/v1/systemone",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := New(Config{Mode: Hybrid, Edge: "edge", Cloud: "cloud", Classifier: &stub{endpoint: endpoint}}); err != nil {
				t.Fatalf("New: %v", err)
			}
		})
	}
	// A hosted classifier may be anywhere.
	if _, err := New(Config{Mode: Hybrid, Edge: "edge", Cloud: "cloud", Hosted: true, Classifier: &stub{endpoint: "https://api.typesafe.ai/v1/systemone"}}); err != nil {
		t.Fatalf("New hosted: %v", err)
	}
}

// TestNewRefusesEndpointsNotShownOnDevice covers the on-device check failing
// closed: a classifier that is not Hosted must name an http or https URL on a
// loopback host, or be the offline fake. A target without a scheme or host,
// a resolver URL and any other scheme may reach another machine.
func TestNewRefusesEndpointsNotShownOnDevice(t *testing.T) {
	for _, endpoint := range []string{
		"api.example.com",
		"api.example.com:443",
		"dns:///classifier.example.com:443",
		"classifier.example.com/v1/systemone",
		"",
		"in-process",
		"unix:///run/piccolo.sock",
		"ftp://127.0.0.1/classifier",
		"//api.example.com/v1",
		"http:///v1/systemone",
		"http://localhost.example.com/v1",
		"http://127.0.0.1.nip.io/v1",
		"http://localhost@api.example.com/v1",
		"http://10.0.0.8:8080/v1",
	} {
		t.Run(endpoint, func(t *testing.T) {
			_, err := New(Config{Mode: Hybrid, Edge: "edge", Cloud: "cloud", Classifier: &stub{endpoint: endpoint}})
			if !errors.Is(err, ErrConfig) || !strings.Contains(err.Error(), "set Hosted") {
				t.Fatalf("New error = %v, want ErrConfig with the hint to set Hosted", err)
			}
			if _, err := New(Config{Mode: Hybrid, Edge: "edge", Cloud: "cloud", Hosted: true, Classifier: &stub{endpoint: endpoint}}); err != nil {
				t.Fatalf("the same classifier configured as Hosted: %v", err)
			}
		})
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	rules := []Rule{func(thread.RouteRequest) (string, bool) { return "", false }}
	r := newRouter(t, Config{Rules: rules, Budget: StateBudget{Total: 7000}})
	if r.cfg.Threshold != DefaultThreshold {
		t.Errorf("threshold = %v, want %v", r.cfg.Threshold, DefaultThreshold)
	}
	want := DefaultBudget()
	want.Total = 7000
	if r.cfg.Budget != want {
		t.Errorf("budget = %+v, want %+v", r.cfg.Budget, want)
	}
	if r.cfg.Count == nil || r.cfg.Count("abcde") != 2 {
		t.Error("the default counter should count (runes+3)/4")
	}
	rules[0] = nil
	if r.cfg.Rules[0] == nil {
		t.Error("New should copy the rules")
	}
}

func TestRouteMatchesDecide(t *testing.T) {
	r := newRouter(t, Config{Mode: CloudBased})
	d1, err := r.Route(context.Background(), request())
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	d2, resp, err := r.Decide(context.Background(), request())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d1 != d2 || resp != nil {
		t.Fatalf("Route = %+v, Decide = %+v (resp %v)", d1, d2, resp)
	}
	if d1.Target != "cloud" || d1.Source != thread.RouteDefault || !d1.UsageKnown || d1.ClassifierCalls != 0 {
		t.Fatalf("decision = %+v", d1)
	}
}

func TestContextChecks(t *testing.T) {
	r := newRouter(t, Config{})
	//nolint:staticcheck // a nil context is the case under test
	if _, _, err := r.Decide(nil, request()); err == nil {
		t.Error("a nil context should be an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Route(ctx, request()); !errors.Is(err, context.Canceled) {
		t.Errorf("Route on a canceled context = %v, want context.Canceled", err)
	}
}

func TestMalformedParticipants(t *testing.T) {
	r := newRouter(t, Config{})
	in := request()
	in.Participants = append(in.Participants, edgeCand(true))
	if _, err := r.Route(context.Background(), in); !errors.Is(err, ErrBadRequest) {
		t.Errorf("duplicate participant: %v, want ErrBadRequest", err)
	}
	in = request()
	in.Participants = append(in.Participants, namedCand(" ", thread.ZoneEdge, true))
	if _, err := r.Route(context.Background(), in); !errors.Is(err, ErrBadRequest) {
		t.Errorf("blank participant: %v, want ErrBadRequest", err)
	}
}

func TestNoCandidate(t *testing.T) {
	r := newRouter(t, Config{})
	cases := []struct {
		name string
		edit func(in *thread.RouteRequest)
		want string
	}{
		{"no participants", func(in *thread.RouteRequest) { in.Participants = nil }, "no participants"},
		{"all offline", func(in *thread.RouteRequest) {
			in.Participants = []thread.RouteCandidate{edgeCand(false), cloudCand(false)}
		}, "unavailable: cloud, edge"},
		{"edge offline and query edge-only", func(in *thread.RouteRequest) {
			in.Participants = []thread.RouteCandidate{edgeCand(false), cloudCand(true)}
			in.Disclosure = thread.DiscloseEdgeOnly
		}, "withheld because the query is edge-only: cloud"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := request()
			tc.edit(&in)
			_, err := r.Route(context.Background(), in)
			if !errors.Is(err, thread.ErrNoCandidate) {
				t.Fatalf("err = %v, want ErrNoCandidate", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestDisclosureRemovesCloud(t *testing.T) {
	// CloudBased defaults to the cloud, so a hard edge decision shows that
	// the cloud was removed.
	r := newRouter(t, Config{Mode: CloudBased})
	cases := []struct {
		name       string
		disclosure thread.Disclosure
		tainted    bool
		want       string
		source     thread.RouteSource
	}{
		{"shared", thread.DiscloseShared, false, "cloud", thread.RouteDefault},
		{"shared and tainted", thread.DiscloseShared, true, "cloud", thread.RouteDefault},
		{"unclassified", thread.DiscloseUnclassified, false, "cloud", thread.RouteDefault},
		{"unclassified and tainted", thread.DiscloseUnclassified, true, "edge", thread.RouteHard},
		{"empty and tainted", "", true, "edge", thread.RouteHard},
		{"empty", "", false, "cloud", thread.RouteDefault},
		{"edge-only", thread.DiscloseEdgeOnly, false, "edge", thread.RouteHard},
		{"unknown value fails closed", "secret", false, "edge", thread.RouteHard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := request()
			in.Disclosure = tc.disclosure
			in.EdgeTainted = tc.tainted
			d, err := r.Route(context.Background(), in)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if d.Target != tc.want || d.Source != tc.source {
				t.Fatalf("decision = %+v, want %s from %s", d, tc.want, tc.source)
			}
			if d.Source == thread.RouteHard && !strings.Contains(d.Reason, "withheld cloud") {
				t.Fatalf("reason %q should say the cloud was withheld", d.Reason)
			}
		})
	}
}

func TestDisclosureRemovesEveryCloudZoneParticipant(t *testing.T) {
	r := newRouter(t, Config{Mode: CloudBased})
	in := request()
	in.Disclosure = thread.DiscloseEdgeOnly
	in.Participants = append(in.Participants, namedCand("lab", thread.ZoneCloud, true))
	d, err := r.Route(context.Background(), in)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d.Target != "edge" || d.Source != thread.RouteHard {
		t.Fatalf("decision = %+v, want edge from hard", d)
	}
}

func TestExplicit(t *testing.T) {
	r := newRouter(t, Config{Mode: LocalBased})
	t.Run("names a candidate", func(t *testing.T) {
		in := request()
		in.Explicit = "cloud"
		d, err := r.Route(context.Background(), in)
		if err != nil || d.Target != "cloud" || d.Source != thread.RouteExplicit {
			t.Fatalf("decision = %+v, err %v", d, err)
		}
	})
	t.Run("names the cloud for an edge-only query", func(t *testing.T) {
		in := request()
		in.Explicit = "cloud"
		in.Disclosure = thread.DiscloseEdgeOnly
		_, err := r.Route(context.Background(), in)
		if !errors.Is(err, thread.ErrNotAllowed) || !strings.Contains(err.Error(), "edge-only") {
			t.Fatalf("err = %v, want ErrNotAllowed naming the reason", err)
		}
	})
	t.Run("names the cloud while the edge is tainted", func(t *testing.T) {
		in := request()
		in.Explicit = "cloud"
		in.Disclosure = thread.DiscloseUnclassified
		in.EdgeTainted = true
		if _, err := r.Route(context.Background(), in); !errors.Is(err, thread.ErrNotAllowed) {
			t.Fatalf("err = %v, want ErrNotAllowed", err)
		}
	})
	t.Run("names an unavailable participant", func(t *testing.T) {
		in := request()
		in.Explicit = "cloud"
		in.Participants = []thread.RouteCandidate{edgeCand(true), cloudCand(false)}
		_, err := r.Route(context.Background(), in)
		if !errors.Is(err, thread.ErrNotAllowed) || !strings.Contains(err.Error(), "not available") {
			t.Fatalf("err = %v, want ErrNotAllowed for an unavailable participant", err)
		}
	})
	t.Run("names an unknown participant", func(t *testing.T) {
		in := request()
		in.Explicit = "phone"
		if _, err := r.Route(context.Background(), in); !errors.Is(err, ErrUnknownParticipant) {
			t.Fatalf("err = %v, want ErrUnknownParticipant", err)
		}
	})
	t.Run("names the only candidate", func(t *testing.T) {
		in := request()
		in.Explicit = "edge"
		in.Disclosure = thread.DiscloseEdgeOnly
		d, err := r.Route(context.Background(), in)
		if err != nil || d.Target != "edge" || d.Source != thread.RouteHard {
			t.Fatalf("decision = %+v, err %v", d, err)
		}
	})
}

func TestExplicitBeatsLaterSteps(t *testing.T) {
	fake := choiceFake("cloud", 0.99)
	r := newRouter(t, Config{
		Mode:       LocalBased,
		Classifier: fake,
		Rules:      []Rule{func(thread.RouteRequest) (string, bool) { return "cloud", true }},
	})
	in := request()
	in.Explicit = "edge"
	in.NeedsGroups = []string{"web"}
	d, resp, err := r.Decide(context.Background(), in)
	if err != nil || d.Target != "edge" || d.Source != thread.RouteExplicit || resp != nil {
		t.Fatalf("decision = %+v, resp %v, err %v", d, resp, err)
	}
}

func TestHardNeeds(t *testing.T) {
	// CloudBased defaults to the cloud; LocalBased to the edge. Each case
	// uses the mode whose default differs from the expected hard choice.
	cases := []struct {
		name   string
		mode   Mode
		edge   []string
		cloud  []string
		needs  []string
		want   string
		source thread.RouteSource
	}{
		{"device only on the edge", CloudBased, []string{"device"}, []string{"web"}, []string{"device"}, "edge", thread.RouteHard},
		{"web only on the cloud", LocalBased, []string{"device"}, []string{"web"}, []string{"web"}, "cloud", thread.RouteHard},
		{"group on both sides", CloudBased, []string{"files"}, []string{"files"}, []string{"files"}, "cloud", thread.RouteDefault},
		{"needs split across sides", CloudBased, []string{"device"}, []string{"web"}, []string{"device", "web"}, "cloud", thread.RouteDefault},
		{"exclusive group with a shared one", CloudBased, []string{"device", "files"}, []string{"files"}, []string{"files", "device"}, "edge", thread.RouteHard},
		{"group nobody holds", LocalBased, []string{"device"}, []string{"web"}, []string{"gpu", ""}, "edge", thread.RouteDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(t, Config{Mode: tc.mode})
			in := request()
			in.Participants = []thread.RouteCandidate{edgeCand(true, tc.edge...), cloudCand(true, tc.cloud...)}
			in.NeedsGroups = tc.needs
			d, err := r.Route(context.Background(), in)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if d.Target != tc.want || d.Source != tc.source {
				t.Fatalf("decision = %+v, want %s from %s", d, tc.want, tc.source)
			}
		})
	}
}

func TestRules(t *testing.T) {
	var seen []string
	rule := func(name, target string, ok bool) Rule {
		return func(in thread.RouteRequest) (string, bool) {
			seen = append(seen, name+":"+in.OperationID)
			return target, ok
		}
	}
	r := newRouter(t, Config{
		Mode: LocalBased,
		Rules: []Rule{
			rule("unknown", "phone", true),
			rule("pass", "cloud", false),
			rule("cloud", "cloud", true),
			rule("never", "edge", true),
		},
	})
	d, err := r.Route(context.Background(), request())
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d.Target != "cloud" || d.Source != thread.RouteRule || !strings.HasPrefix(d.Reason, "rule 3") {
		t.Fatalf("decision = %+v", d)
	}
	if strings.Join(seen, ",") != "unknown:op-1,pass:op-1,cloud:op-1" {
		t.Fatalf("rules ran as %v", seen)
	}

	// A rule that names a withheld participant is ignored too.
	r = newRouter(t, Config{
		Mode:  LocalBased,
		Rules: []Rule{rule("offline", "cloud", true)},
	})
	in := request()
	in.Participants = append(in.Participants, namedCand("lab", thread.ZoneEdge, true))
	in.Participants[1].Available = false
	d, err = r.Route(context.Background(), in)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d.Target != "edge" || d.Source != thread.RouteDefault || !strings.Contains(d.Reason, `rule 1 chose "cloud", which is not a candidate`) {
		t.Fatalf("decision = %+v", d)
	}
}

func TestClassifierAccept(t *testing.T) {
	var kinds []classifier.AttemptKind
	rec := &recorder{inner: choiceFake("cloud", 0.9)}
	r := newRouter(t, Config{
		Mode:       Hybrid,
		Classifier: rec,
		Limits:     classifier.DispatchLimits{MaxRequestBytes: 1 << 20},
		Authorize: func(_ context.Context, k classifier.AttemptKind) error {
			kinds = append(kinds, k)
			return nil
		},
	})
	in := request()
	in.Owner = "edge"
	d, resp, err := r.Decide(context.Background(), in)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Target != "cloud" || d.Source != thread.RouteClassifier || d.Confidence != 0.9 {
		t.Fatalf("decision = %+v", d)
	}
	if resp == nil || len(resp.Attempts) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if d.ClassifierCalls != 1 || !d.UsageKnown || d.ClassifierUsage == nil ||
		d.ClassifierUsage.InputTokens != 120 || d.ClassifierUsage.OutputTokens != 3 {
		t.Fatalf("accounting = %d calls, %+v, known %v", d.ClassifierCalls, d.ClassifierUsage, d.UsageKnown)
	}
	if len(kinds) != 1 || kinds[0] != classifier.SimulatedAttempt {
		t.Fatalf("authorize saw %v", kinds)
	}
	req := rec.last(t)
	if req.OperationID != "op-1/route" {
		t.Errorf("operation ID = %q", req.OperationID)
	}
	if rec.limits[0].MaxRequestBytes != 1<<20 {
		t.Errorf("limits = %+v", rec.limits[0])
	}
	q, ok := req.Questions[QuestionID].(*classifier.ChoiceQuestion)
	if !ok || len(req.Questions) != 1 {
		t.Fatalf("questions = %#v", req.Questions)
	}
	if len(q.Choices) != 2 || q.Choices["edge"] != edgeDesc || q.Choices["cloud"] != cloudDesc {
		t.Errorf("choices = %v", q.Choices)
	}
	if !strings.Contains(q.Instructions, "weighing quality against cloud cost") {
		t.Errorf("instructions = %q", q.Instructions)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(req.State, &state); err != nil {
		t.Fatalf("state is not a JSON object: %v", err)
	}
	if string(state["query"]) != `"What is the weather like?"` {
		t.Errorf("state query = %s", state["query"])
	}
}

func TestClassifierOperationIDWithoutRequestID(t *testing.T) {
	rec := &recorder{inner: choiceFake("cloud", 0.9)}
	r := newRouter(t, Config{Classifier: rec})
	in := request()
	in.OperationID = ""
	if _, err := r.Route(context.Background(), in); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if got := rec.last(t).OperationID; got != "route" {
		t.Fatalf("operation ID = %q, want route", got)
	}
}

func TestClassifierLowConfidence(t *testing.T) {
	r := newRouter(t, Config{Mode: Hybrid, Classifier: choiceFake("cloud", 0.6)})
	// Hybrid stays with the owner on low confidence, even when the
	// classifier leaned towards the other participant.
	in := request()
	in.Owner = "edge"
	d, resp, err := r.Decide(context.Background(), in)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Target != "edge" || d.Source != thread.RouteDefault || d.Confidence != 0 {
		t.Fatalf("decision = %+v", d)
	}
	if !strings.Contains(d.Reason, "below the threshold 0.80") || !strings.Contains(d.Reason, "stay with the current owner") {
		t.Fatalf("reason = %q", d.Reason)
	}
	if resp == nil || d.ClassifierCalls != 1 || !d.UsageKnown {
		t.Fatalf("accounting = %+v, resp %v", d, resp)
	}
}

func TestClassifierThresholdIsConfigurable(t *testing.T) {
	r := newRouter(t, Config{Mode: LocalBased, Classifier: choiceFake("cloud", 0.6), Threshold: 0.5})
	d, err := r.Route(context.Background(), request())
	if err != nil || d.Target != "cloud" || d.Source != thread.RouteClassifier {
		t.Fatalf("decision = %+v, err %v", d, err)
	}
}

func TestClassifierError(t *testing.T) {
	fake := classifier.NewFake(&classifier.FakeConfig{
		Response: classifier.Response{Attempts: []classifier.Attempt{{Kind: classifier.SimulatedAttempt, Usage: &classifier.Usage{InputTokens: i64(80)}}}},
		Error:    errors.New("service unavailable"),
	})
	r := newRouter(t, Config{Mode: CloudBased, Classifier: fake})
	d, resp, err := r.Decide(context.Background(), request())
	if err != nil {
		t.Fatalf("a classifier error must not be returned: %v", err)
	}
	if d.Target != "cloud" || d.Source != thread.RouteDefault || !strings.Contains(d.Reason, "classifier failed") {
		t.Fatalf("decision = %+v", d)
	}
	if resp == nil || d.ClassifierCalls != 1 || d.UsageKnown || d.ClassifierUsage.InputTokens != 80 {
		t.Fatalf("accounting = %+v (usage %+v), resp %v", d, d.ClassifierUsage, resp)
	}
}

func TestClassifierInvalidAnswer(t *testing.T) {
	t.Run("choice is not the most probable", func(t *testing.T) {
		fake := classifier.NewFake(&classifier.FakeConfig{Response: classifier.Response{
			Model: "jev-test",
			Answers: map[string]classifier.Answer{QuestionID: &classifier.ChoiceAnswer{
				Choice: "cloud", Confidence: 0.95, Probabilities: map[string]float64{"cloud": 0.2, "edge": 0.8},
			}},
		}})
		r := newRouter(t, Config{Mode: LocalBased, Classifier: fake})
		d, resp, err := r.Decide(context.Background(), request())
		if err != nil || d.Target != "edge" || d.Source != thread.RouteDefault || resp == nil || d.ClassifierCalls != 1 {
			t.Fatalf("decision = %+v, resp %v, err %v", d, resp, err)
		}
	})
	t.Run("missing answer without an error", func(t *testing.T) {
		s := &stub{endpoint: "fake://stub", resp: &classifier.Response{OperationID: "op-1/route", Model: "stub-model"}}
		r := newRouter(t, Config{Mode: LocalBased, Classifier: s})
		d, resp, err := r.Decide(context.Background(), request())
		if err != nil || d.Target != "edge" || d.Source != thread.RouteDefault || resp != s.resp {
			t.Fatalf("decision = %+v, resp %v, err %v", d, resp, err)
		}
		if !strings.Contains(d.Reason, "classifier answer is invalid") {
			t.Fatalf("reason = %q", d.Reason)
		}
	})
	t.Run("nil response without an error", func(t *testing.T) {
		r := newRouter(t, Config{Mode: CloudBased, Classifier: &stub{endpoint: "fake://stub"}})
		d, resp, err := r.Decide(context.Background(), request())
		if err != nil || d.Target != "cloud" || d.Source != thread.RouteDefault || resp != nil || d.ClassifierCalls != 0 {
			t.Fatalf("decision = %+v, resp %v, err %v", d, resp, err)
		}
	})
}

func TestClassifierCanceled(t *testing.T) {
	fake := classifier.NewFake(&classifier.FakeConfig{
		Response:      classifier.Response{Attempts: []classifier.Attempt{{Kind: classifier.SimulatedAttempt, Usage: &classifier.Usage{InputTokens: i64(10), OutputTokens: i64(0)}}}},
		WaitForCancel: true,
	})
	r := newRouter(t, Config{Classifier: fake})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-fake.Started()
		cancel()
	}()
	d, resp, err := r.Decide(ctx, request())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if d.Target != "" || resp == nil || d.ClassifierCalls != 1 || !d.UsageKnown || d.ClassifierUsage.InputTokens != 10 {
		t.Fatalf("decision = %+v, resp %v", d, resp)
	}
}

func TestAuthorizeDenied(t *testing.T) {
	fake := choiceFake("cloud", 0.99)
	denied := errors.New("over the classifier budget")
	r := newRouter(t, Config{
		Mode:       LocalBased,
		Classifier: fake,
		Authorize:  func(context.Context, classifier.AttemptKind) error { return denied },
	})
	d, resp, err := r.Decide(context.Background(), request())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Target != "edge" || d.Source != thread.RouteDefault || !strings.Contains(d.Reason, denied.Error()) {
		t.Fatalf("decision = %+v", d)
	}
	if resp == nil || len(resp.Attempts) != 0 || d.ClassifierCalls != 0 {
		t.Fatalf("a denied dispatch has no attempt: %+v", resp)
	}
	select {
	case <-fake.Started():
		t.Fatal("the fake dispatched despite the denial")
	default:
	}
}

func TestLagLimit(t *testing.T) {
	for _, tc := range []struct {
		lag    uint64
		want   string
		source thread.RouteSource
	}{
		{lag: 50, want: "edge", source: thread.RouteDefault},
		{lag: 10, want: "cloud", source: thread.RouteClassifier},
	} {
		r := newRouter(t, Config{Mode: LocalBased, Classifier: choiceFake("cloud", 0.95), MaxLag: 10})
		in := request()
		in.Participants[1].Lag = tc.lag
		d, err := r.Route(context.Background(), in)
		if err != nil {
			t.Fatalf("Route: %v", err)
		}
		if d.Target != tc.want || d.Source != tc.source {
			t.Fatalf("lag %d: decision = %+v", tc.lag, d)
		}
		if tc.source == thread.RouteDefault && !strings.Contains(d.Reason, "50 entries behind (limit 10)") {
			t.Fatalf("reason = %q", d.Reason)
		}
	}
	// Without a limit, lag does not matter.
	r := newRouter(t, Config{Mode: LocalBased, Classifier: choiceFake("cloud", 0.95)})
	in := request()
	in.Participants[1].Lag = 1000
	if d, err := r.Route(context.Background(), in); err != nil || d.Target != "cloud" {
		t.Fatalf("decision = %+v, err %v", d, err)
	}
}

func TestHostedClassifierNotAskedAboutEdgeOnlyQuery(t *testing.T) {
	rec := &recorder{inner: choiceFake("edge", 0.99)}
	r := newRouter(t, Config{Mode: Hybrid, Classifier: rec, Hosted: true})
	in := request()
	in.Disclosure = thread.DiscloseEdgeOnly
	// Two edge participants stay candidates, so only the hosted check stops
	// the classifier.
	in.Participants = append(in.Participants, namedCand("tablet", thread.ZoneEdge, true))
	in.Owner = "tablet"
	d, resp, err := r.Decide(context.Background(), in)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if rec.calls() != 0 || resp != nil {
		t.Fatalf("the hosted classifier was asked about an edge-only query")
	}
	if d.Target != "tablet" || d.Source != thread.RouteDefault || !strings.Contains(d.Reason, "hosted classifier is not asked") {
		t.Fatalf("decision = %+v", d)
	}

	// A local classifier is asked.
	rec = &recorder{inner: choiceFake("edge", 0.99)}
	r = newRouter(t, Config{Mode: Hybrid, Classifier: rec})
	if _, err := r.Route(context.Background(), in); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if rec.calls() != 1 {
		t.Fatalf("a local classifier should be asked, got %d calls", rec.calls())
	}
}

func TestHostedClassifierNeverSeesEdgeOnlyEntries(t *testing.T) {
	recent := []thread.Entry{
		{Seq: 1, Kind: thread.KindUserInput, Visibility: thread.Shared, Content: textBlocks("public question")},
		{Seq: 2, Kind: thread.KindReply, Origin: thread.Origin{Participant: "edge", Path: "master"}, Visibility: thread.EdgeOnly, Content: textBlocks("secret diagnosis")},
		{Seq: 3, Kind: thread.KindReport, Origin: thread.Origin{Participant: "edge"}, Content: textBlocks("unset visibility secret")},
		{Seq: 4, Kind: thread.KindReply, Origin: thread.Origin{Participant: "cloud", Path: "master"}, Visibility: thread.Shared, Content: textBlocks("public answer")},
	}
	for _, hosted := range []bool{true, false} {
		rec := &recorder{inner: choiceFake("cloud", 0.99)}
		r := newRouter(t, Config{Classifier: rec, Hosted: hosted})
		in := request()
		in.Recent = recent
		if _, err := r.Route(context.Background(), in); err != nil {
			t.Fatalf("Route: %v", err)
		}
		state := string(rec.last(t).State)
		if !strings.Contains(state, "public question") || !strings.Contains(state, "public answer") {
			t.Fatalf("hosted %v: shared entries missing from %s", hosted, state)
		}
		if got := strings.Contains(state, "secret"); got == hosted {
			t.Fatalf("hosted %v: state %s, edge-only content present = %v", hosted, state, got)
		}
	}
}

func TestModeDefaults(t *testing.T) {
	both := []thread.RouteCandidate{edgeCand(true), cloudCand(true)}
	cases := []struct {
		name  string
		mode  Mode
		owner string
		parts []thread.RouteCandidate
		want  string
		why   string
	}{
		{"hybrid stays with the cloud owner", Hybrid, "cloud", both, "cloud", "stay with the current owner"},
		{"hybrid stays with the edge owner", Hybrid, "edge", both, "edge", "stay with the current owner"},
		{"hybrid without owner takes the edge", Hybrid, "", both, "edge", "the edge"},
		{"hybrid owner not a candidate", Hybrid, "lab", append([]thread.RouteCandidate{namedCand("lab", thread.ZoneCloud, false)}, both...), "edge", "the edge"},
		{"hybrid without edge takes the cloud", Hybrid, "", []thread.RouteCandidate{edgeCand(false), cloudCand(true), namedCand("lab", thread.ZoneCloud, true)}, "cloud", "the cloud"},
		{"hybrid first by name", Hybrid, "", []thread.RouteCandidate{edgeCand(false), cloudCand(false), namedCand("lab", thread.ZoneCloud, true), namedCand("beta", thread.ZoneEdge, true)}, "beta", "first candidate by name"},
		{"local based goes home", LocalBased, "cloud", both, "edge", "home is the edge"},
		{"local based without edge", LocalBased, "", []thread.RouteCandidate{edgeCand(false), cloudCand(true), namedCand("lab", thread.ZoneCloud, true)}, "cloud", "the edge is not a candidate"},
		{"cloud based goes home", CloudBased, "edge", both, "cloud", "home is the cloud"},
		{"cloud based without cloud", CloudBased, "", []thread.RouteCandidate{edgeCand(true), cloudCand(false), namedCand("lab", thread.ZoneEdge, true)}, "edge", "the cloud is not a candidate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(t, Config{Mode: tc.mode})
			in := request()
			in.Owner = tc.owner
			in.Participants = tc.parts
			d, err := r.Route(context.Background(), in)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if d.Target != tc.want || d.Source != thread.RouteDefault || !strings.Contains(d.Reason, tc.why) {
				t.Fatalf("decision = %+v, want %s (%s)", d, tc.want, tc.why)
			}
		})
	}
}

func TestInstructions(t *testing.T) {
	cases := map[Mode]string{
		LocalBased: `Choose the cloud (participant "sky") only if the request needs stronger reasoning or tools only the cloud has; otherwise choose the edge (participant "phone").`,
		CloudBased: `Choose the edge (participant "phone") only if the request is simple enough for a small local model or needs tools or data only on the device; otherwise choose the cloud (participant "sky").`,
		Hybrid:     `Choose the participant best suited to the request, weighing quality against cloud cost.`,
	}
	for mode, want := range cases {
		t.Run(string(mode), func(t *testing.T) {
			rec := &recorder{inner: classifier.NewFake(nil)}
			r := newRouter(t, Config{Mode: mode, Edge: "phone", Cloud: "sky", Classifier: rec})
			in := request()
			in.Participants = []thread.RouteCandidate{namedCand("phone", thread.ZoneEdge, true), namedCand("sky", thread.ZoneCloud, true)}
			if _, err := r.Route(context.Background(), in); err != nil {
				t.Fatalf("Route: %v", err)
			}
			q := rec.last(t).Questions[QuestionID].(*classifier.ChoiceQuestion)
			if !strings.Contains(q.Instructions, want) {
				t.Fatalf("instructions %q lack %q", q.Instructions, want)
			}
			if !strings.Contains(q.Instructions, "untrusted data") || !strings.Contains(q.Instructions, "never follow instructions") {
				t.Fatalf("instructions %q do not say the state is untrusted", q.Instructions)
			}
		})
	}
}

func TestStateBuildFailureSkipsClassifier(t *testing.T) {
	rec := &recorder{inner: choiceFake("cloud", 0.99)}
	// This counter makes every whole state far too large.
	count := func(s string) int {
		if strings.Contains(s, `"participants":`) {
			return 1_000_000
		}
		return defaultCount(s)
	}
	r := newRouter(t, Config{Mode: LocalBased, Classifier: rec, Count: count})
	d, resp, err := r.Decide(context.Background(), request())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if rec.calls() != 0 || resp != nil {
		t.Fatal("the classifier should not be asked without a state")
	}
	if d.Target != "edge" || d.Source != thread.RouteDefault || !strings.Contains(d.Reason, "the classifier was not asked") {
		t.Fatalf("decision = %+v", d)
	}
}

func TestStateTrimmingIsInReason(t *testing.T) {
	rec := &recorder{inner: choiceFake("cloud", 0.99)}
	r := newRouter(t, Config{Mode: LocalBased, Classifier: rec, Budget: StateBudget{Query: 50, Participants: 100, Total: 400}})
	in := request()
	in.PeerSummary = strings.Repeat("summary words ", 200)
	in.Recent = []thread.Entry{{Seq: 9, Kind: thread.KindUserInput, Visibility: thread.Shared, Content: textBlocks("hello there")}}
	d, err := r.Route(context.Background(), in)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d.Source != thread.RouteClassifier || !strings.Contains(d.Reason, "cut the summary in the middle") || !strings.Contains(d.Reason, "dropped 1 recent entries") {
		t.Fatalf("decision = %+v", d)
	}
}
