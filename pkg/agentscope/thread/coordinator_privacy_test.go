package thread

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// TestCoordinatorPrivacySession runs a session with edge-only inputs and
// private edge work, then checks that no edge-only text reached the cloud
// through a Sync, a Pending result, a Read, an activation input or an
// event page.
func TestCoordinatorPrivacySession(t *testing.T) {
	edge, cloud := edgeCloud()
	edgeErrs := map[string]error{}
	var recordID string
	edge.push(func(r *fakeRun) *ActivationEnd {
		h := r.host()
		recordID = h.RecordEvent(EventRecord{Tool: "read_contacts", Output: "SECRET-E3 alice 555"})
		_, _, edgeErrs["assign"] = r.assign("cloud", "look up SECRET-P1", false)
		_, edgeErrs["report"] = r.report("SECRET-R4", true, "cloud")
		if _, err := h.Publish(r.ctx, PublishRequest{From: Origin{Participant: "edge"}, Refs: []ArtifactRef{{URI: "file:///SECRET-A5"}}}); err != nil {
			t.Errorf("publish: %v", err)
		}
		r.say("Noted SECRET-R2")
		return &ActivationEnd{Reason: types.ReplyCompleted, Text: r.text(), Private: true}
	})
	var cloudErrs []error
	cloud.push(replyWith("Rome plan"), func(r *fakeRun) *ActivationEnd {
		r.pending()
		if _, err := r.read(ReadQuery{}); err != nil {
			cloudErrs = append(cloudErrs, err)
		}
		var all []uint64
		for i := uint64(1); i <= 20; i++ {
			all = append(all, i)
		}
		if _, err := r.read(ReadQuery{Seqs: all}); err != nil {
			cloudErrs = append(cloudErrs, err)
		}
		if got := r.host().EventOutline("cloud", 0); len(got) != 0 {
			t.Errorf("cloud outline = %+v", got)
		}
		if _, err := r.page(recordID); !errors.Is(err, ErrEventNotFound) {
			t.Errorf("cloud page error = %v", err)
		}
		return r.reply("flights booked")
	})
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.Router = zoneRouter() }, edge, cloud)
	env.say("plan a trip to Rome")
	env.idle()
	env.submit(Query{Text: "my passport is SECRET-P1", Disclosure: DiscloseEdgeOnly})
	env.idle()
	env.say("book flights")
	env.idle()

	for _, name := range []string{"assign", "report"} {
		if !errors.Is(edgeErrs[name], errPrivateBrief) {
			t.Errorf("private %s to the cloud: %v", name, edgeErrs[name])
		}
	}
	if len(cloudErrs) > 0 {
		t.Fatalf("cloud reads failed: %v", cloudErrs)
	}
	log := env.log()
	var edgeOnly int
	for i := range log {
		e := &log[i]
		if strings.Contains(renderEntry(e), "SECRET") && e.Visibility != EdgeOnly {
			t.Errorf("#%d %s holds private text but is %s", e.Seq, e.Kind, e.Visibility)
		}
		if e.Visibility == EdgeOnly {
			edgeOnly++
		}
	}
	if edgeOnly != 3 { // the input, the private reply and the artifact
		t.Fatalf("%d edge-only entries", edgeOnly)
	}
	seen := cloud.seenTexts()
	if containsText(seen, "SECRET") {
		t.Fatalf("edge-only text reached the cloud: %q", seen)
	}
	if !containsText(seen, "withheld: edge-only") {
		t.Fatalf("the cloud saw no gap: %q", seen)
	}
	for _, s := range cloud.pendingResults() {
		if containsText(syncTexts(s), "SECRET") {
			t.Fatal("a Pending result held edge-only text")
		}
	}
	env.c.mu.Lock()
	private := env.c.parts["edge"].private
	env.c.mu.Unlock()
	if !private {
		t.Fatal("the edge is not marked private")
	}
}

func TestCoordinatorDisclosurePolicyDeclassifies(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(string) (Visibility, error)
		want Visibility
	}{
		{"shared", func(string) (Visibility, error) { return Shared, nil }, Shared},
		{"edge only", func(string) (Visibility, error) { return EdgeOnly, nil }, EdgeOnly},
		{"error", func(string) (Visibility, error) { return "", errors.New("down") }, EdgeOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edge, cloud := edgeCloud()
			policy := &fakePolicy{fn: tc.fn}
			edge.push(func(r *fakeRun) *ActivationEnd {
				r.say("the weather is fine")
				return &ActivationEnd{Reason: types.ReplyCompleted, Text: r.text(), Private: true,
					Digest: &ToolDigest{Calls: []ToolCallDigest{{Name: "Read", Target: "notes.txt", Count: 1}}}}
			})
			env := newCoEnv(t, nil, func(cfg *Config) { cfg.Disclosure = policy }, edge, cloud)
			env.say("weather?")
			env.idle()
			reps := findEntries(env.log(), KindReply, "")
			if len(reps) != 1 || reps[0].Visibility != tc.want {
				t.Fatalf("reply = %+v", reps)
			}
			if len(policy.calls) != 1 || !strings.Contains(policy.calls[0], "notes.txt") {
				t.Fatalf("classified %q; the digest must be classified with the text", policy.calls)
			}
		})
	}
}

func TestCoordinatorTaintPropagation(t *testing.T) {
	edge, cloud := edgeCloud()
	edge.push(func(r *fakeRun) *ActivationEnd {
		r.say("from the web")
		return &ActivationEnd{Reason: types.ReplyCompleted, Text: r.text(), Tainted: true}
	}, func(r *fakeRun) *ActivationEnd {
		seq, err := r.host().Transfer(r.ctx, TransferRequest{From: Origin{Participant: "edge"}, Epoch: r.act.Epoch,
			To: "cloud", Brief: testBrief(), Text: "handing over", Tainted: true})
		if err != nil || seq == 0 {
			t.Errorf("transfer: %v", err)
		}
		return &ActivationEnd{Reason: types.ReplyHandedOff}
	})
	env := newCoEnv(t, nil, nil, edge, cloud)
	env.say("search")
	env.idle()
	env.say("continue in the cloud")
	env.idle()
	log := env.log()
	reps := findEntries(log, KindReply, "")
	if len(reps) < 2 || !reps[0].Tainted || !reps[1].Tainted {
		t.Fatalf("replies = %+v", reps)
	}
	ocs := findEntries(log, KindOwnerChange, "")
	if last := ocs[len(ocs)-1]; last.Owner.Reason != ReasonTransfer || !last.Tainted {
		t.Fatalf("transfer = %+v", last)
	}
	acts := cloud.activations()
	if len(acts) != 1 || acts[0].Sync == nil || acts[0].Sync.Taint == nil || !*acts[0].Sync.Taint {
		t.Fatalf("cloud import taint = %+v", acts[0].Sync)
	}
	if !strings.Contains(strings.Join(syncTexts(acts[0].Sync), "\n"), "(untrusted source)") {
		t.Fatal("the tainted reply is not marked in the projection")
	}
}

func TestCoordinatorRecovery(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("answered"))
	mustAppend(t, store, &Guard{}, ownerChange("", "cloud"))
	rep := replyFrom("cloud", "done")
	rep.BasedOn = 1
	mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, rep)
	mustAppend(t, store, nil,
		userInput("unanswered"),
		Entry{Kind: KindAssign, Origin: Origin{Participant: "cloud"}, To: []string{"edge"}, Visibility: Shared,
			Task: &Task{ID: "t-7"}, Content: textBlocks("do it")},
		Entry{Kind: KindArtifact, Visibility: EdgeOnly, Refs: []ArtifactRef{{URI: "file:///private"}}},
	)
	edge, cloud := edgeCloud()
	edge.setState(func(s *ParticipantState) { s.Cursor = 6 })
	var taskID string
	cloud.push(func(r *fakeRun) *ActivationEnd {
		taskID, _, _ = r.assign("edge", "next task", false)
		return r.reply("ok")
	})
	env := newCoEnv(t, store, nil, edge, cloud)

	log := env.log()
	if got := kindsOf(log[6:]); !reflect.DeepEqual(got, []string{"marker:recovered", "report"}) {
		t.Fatalf("recovery entries = %v", got)
	}
	if log[6].BasedOn != 4 || !strings.Contains(log[6].Marker.Detail, "#4") {
		t.Fatalf("recovered marker = %+v", log[6].Marker)
	}
	orphan := log[7]
	if orphan.Task == nil || orphan.Task.ID != "t-7" || !reflect.DeepEqual(orphan.To, []string{"cloud"}) || orphan.BasedOn != 5 {
		t.Fatalf("orphan report = %+v", orphan)
	}
	if owner, epoch := env.c.Owner(); owner != "cloud" || epoch != 1 {
		t.Fatalf("owner = %s %d", owner, epoch)
	}
	var shown []string
	for _, ev := range env.events() {
		if ev.Kind == EventEntry {
			shown = append(shown, kindsOf([]Entry{*ev.Entry})...)
		}
	}
	if !reflect.DeepEqual(shown, []string{"marker:recovered", "report"}) {
		t.Fatalf("recovery events = %v", shown)
	}
	env.c.mu.Lock()
	parked := len(env.c.inputs) == 1 && env.c.inputs[0].parked && env.c.inputs[0].entry.Seq == 4
	private := env.c.parts["edge"].private
	env.c.mu.Unlock()
	if !parked || !private {
		t.Fatalf("parked %v private %v", parked, private)
	}

	// The recovered input is not re-run; the next query imports it.
	env.say("new question")
	env.idle()
	acts := cloud.activations()
	if len(acts) != 1 || acts[0].Input != "new question" || !containsText(syncTexts(acts[0].Sync), "unanswered") {
		t.Fatalf("cloud activation = %+v", acts)
	}
	if taskID != "t-8" {
		t.Fatalf("task id after recovery = %q", taskID)
	}

	// A second restart finds nothing new to recover.
	edge2, cloud2 := edgeCloud()
	before := len(env.log())
	c2, err := NewCoordinator(t.Context(), Config{Store: store, Participants: []Participant{edge2, cloud2}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()
	if after, _ := store.Head(t.Context()); after.LastSeq != uint64(before) {
		t.Fatalf("second recovery appended %d entries", after.LastSeq-uint64(before))
	}
}

func TestCoordinatorHostCalls(t *testing.T) {
	edge, cloud := edgeCloud()
	type result struct {
		name string
		err  error
	}
	var results []result
	check := func(name string, err error) { results = append(results, result{name, err}) }
	edge.push(func(r *fakeRun) *ActivationEnd {
		h := r.host()
		id := h.RecordEvent(EventRecord{Origin: Origin{Participant: "cloud"}, Zone: ZoneCloud, Tool: "camera", Output: "jpeg bytes", Visibility: Shared})
		h.RecordUsage(LedgerEntry{Participant: "cloud", Kind: UsageMain, Usage: &model.ChatUsage{InputTokens: 3}})
		if d := h.Descriptors(); len(d) != 2 || d[0].Name != "edge" || d[1].Name != "cloud" {
			t.Errorf("descriptors = %+v", d)
		}
		if o, e := h.Owner(); o != "edge" || e != 1 {
			t.Errorf("owner = %s %d", o, e)
		}
		_, err := h.Read(r.ctx, "cloud", ReadQuery{})
		check("read as another", err)
		_, _, err = h.EventPage("cloud", id, 0, 0)
		check("page as another", err)
		if h.EventOutline("cloud", 0) != nil || h.EventSearch("cloud", "", 0) != nil {
			t.Error("event lists answered another reader")
		}
		_, err = h.Pending(r.ctx, "cloud", 0)
		check("pending as another", err)
		_, err = h.Publish(r.ctx, PublishRequest{From: Origin{Participant: "edge"}})
		check("publish without refs", err)
		_, err = h.Publish(r.ctx, PublishRequest{From: Origin{Participant: "cloud"}, Refs: []ArtifactRef{{URI: "x"}}})
		check("publish as another", err)
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, To: []string{"cloud"}})
		check("empty report", err)
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, Text: "x", To: []string{"nobody"}})
		check("report to unknown", err)
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, Text: "x", To: []string{"edge"}})
		check("report to self", err)
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "cloud"}, Text: "x"})
		check("report as another", err)
		if _, err := h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, Text: "fyi", To: []string{"cloud", "cloud"}, TaskID: "t-9"}); err != nil {
			t.Errorf("report: %v", err)
		}
		if _, err := h.Publish(r.ctx, PublishRequest{From: Origin{Participant: "edge"}, Refs: []ArtifactRef{{URI: "file:///plan.md"}},
			Text: "the plan", Private: true}); err != nil {
			t.Errorf("publish: %v", err)
		}
		// The edge-only artifact made the edge private: a report to the cloud
		// now needs the disclosure policy, and there is none.
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, Text: "fyi", To: []string{"cloud"}})
		check("private report to the cloud", err)
		got, err := h.Read(r.ctx, "edge", ReadQuery{Seqs: []uint64{1, 99}, Kinds: []Kind{KindUserInput}, Limit: 1})
		if err != nil || len(got) != 1 || got[0].Seq != 1 {
			t.Errorf("read seqs = %+v %v", got, err)
		}
		return r.reply("done")
	})
	cloud.push(func(r *fakeRun) *ActivationEnd {
		h := r.host()
		if got := h.EventOutline("cloud", 0); len(got) != 1 || got[0].Tool != "camera" || got[0].Origin.Participant != "edge" {
			t.Errorf("cloud outline = %+v", got)
		}
		if got := h.EventSearch("cloud", "jpeg", 0); len(got) != 1 {
			t.Errorf("cloud search = %+v", got)
		}
		if text, _, err := h.EventPage("cloud", "ev-1", 0, 0); err != nil || text != "jpeg bytes" {
			t.Errorf("cloud page = %q %v", text, err)
		}
		got, err := r.read(ReadQuery{Seqs: []uint64{4, 1}})
		if err != nil || len(got) != 1 || got[0].Seq != 1 {
			t.Errorf("cloud read = %+v %v", got, err)
		}
		return r.reply("seen")
	})
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.CloudReadsEdge = true }, edge, cloud)
	env.say("hello") // #1, owner_change #2; the edge reports #3 and publishes #4
	env.idle()
	env.submit(Query{Text: "shared", Explicit: "cloud"})
	env.idle()

	for _, r := range results {
		if r.err == nil {
			t.Errorf("%s: no error", r.name)
		}
	}
	if len(results) != 10 {
		t.Fatalf("%d checks ran", len(results))
	}
	if tot := env.c.Ledger().Totals()["edge"]; tot.Calls != 1 || tot.InputTokens != 3 {
		t.Fatalf("usage recorded as %+v", env.c.Ledger().Totals())
	}
	log := env.log()
	arts := findEntries(log, KindArtifact, "")
	reps := findEntries(log, KindReport, "")
	if len(arts) != 1 || arts[0].Seq != 4 || arts[0].Visibility != EdgeOnly || len(reps) != 1 || !reflect.DeepEqual(reps[0].To, []string{"cloud"}) ||
		reps[0].Task == nil || reps[0].Task.ID != "t-9" || reps[0].Visibility != Shared {
		t.Fatalf("artifact %+v report %+v", arts, reps)
	}
}
