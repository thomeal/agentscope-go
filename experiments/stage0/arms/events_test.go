package arms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
)

func TestSearchFindsTheLabelAndNotItsNearMisses(t *testing.T) {
	events := []toolEvent{
		{Turn: 0, Name: "read_record", Text: "RECORD R-1\nAudit reference for the warehouse sync: AAA-1111\nKey rotation ticket for the warehouse sync: BBB-2222"},
		{Turn: 1, Name: "read_record", Text: "RECORD R-2\nAudit reference for the ledger service: CCC-3333\n05:43:26 Synthetic check of the mobile backend passed"},
	}
	got := searchEvents(events, []string{"What is the audit reference for the warehouse sync?"})
	lines := strings.Split(got, "\n")
	if len(lines) < 2 || !strings.Contains(lines[1], "AAA-1111") {
		t.Fatalf("best match:\n%s", got)
	}
	if strings.Index(got, "AAA-1111") > strings.Index(got+"CCC-3333", "CCC-3333") {
		t.Errorf("a near miss outranked the label:\n%s", got)
	}
	if got := searchEvents(events, []string{"mobile gateway"}); !strings.Contains(got, "(no match)") {
		t.Errorf("one shared word found something:\n%s", got)
	}
	if got := searchEvents(events, []string{"mobile"}); !strings.Contains(got, "backend") {
		t.Errorf("a one-word query found nothing:\n%s", got)
	}
	if got := searchEvents(events, []string{"the of for"}); !strings.Contains(got, "(no match)") {
		t.Errorf("stop words found something:\n%s", got)
	}
}

func TestReadsStayBounded(t *testing.T) {
	var text []string
	for range 400 {
		text = append(text, "Audit reference for the warehouse sync: AAA-1111")
	}
	events := []toolEvent{{Name: "read_record", Text: strings.Join(text, "\n")}}
	var queries []any
	for range 200 {
		queries = append(queries, "audit reference warehouse sync")
	}
	for name, input := range map[string]map[string]any{
		"search": {"queries": queries},
		"page":   {"seq": 1},
	} {
		out, err := readEvents(events, input)
		if err != nil {
			t.Fatal(err)
		}
		if n := len([]rune(out)); n > eventOutputChars+100 {
			t.Errorf("%s returned %d characters", name, n)
		}
		if !strings.Contains(out, "(") {
			t.Errorf("%s did not say it stopped", name)
		}
	}
	out, err := readEvents(events, map[string]any{"seq": 1, "offset": 390})
	if err != nil || strings.Contains(out, "stopped") || strings.Count(out, "AAA-1111") != 10 {
		t.Errorf("the last page: %v\n%s", err, out)
	}
	if _, err := readEvents(events, map[string]any{"seq": 2}); err == nil {
		t.Error("a missing result was served")
	}
}

// What the list holds is the cloud's, and only the local clone can reach it.
func TestReadClonePullsTheCloudsResultsAndChangesNothingElse(t *testing.T) {
	for _, arm := range []string{ArmT1, ArmA4} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.readEvents = true
			env := f.env(0)
			rows := runSession(t, f, env, arm, shortSession())
			read := ArmT1Read
			if arm == ArmA4 {
				read = ArmA4Read
			}
			row, ok := rows[read]
			if !ok {
				t.Fatalf("no %s row: %v", read, rows)
			}
			if row.Error != "" || rows[arm].Error != "" {
				t.Fatalf("%s / %s", row.Error, rows[arm].Error)
			}
			if row.Counters["event_reads"] == 0 {
				t.Fatalf("counters: %v", row.Counters)
			}
			for _, lr := range env.Ledger.Rows() {
				if lr.Arm == read && lr.Side != lab.SideLocal {
					t.Errorf("%s called the %s side as %s", read, lr.Side, lr.Role)
				}
			}
			tool, answered := 0, 0
			for _, c := range f.captures() {
				if !bytes.Contains(c.Raw, []byte(`"`+eventTool+`"`)) {
					continue
				}
				tool++
				if c.Side != lab.SideLocal || c.Kind != kindProbe {
					t.Errorf("the tool was offered on %s in a %s request", c.Side, c.Kind)
				}
				if bytes.Contains(c.Raw, []byte(eventQueryPrefix)) {
					answered++
				}
				if !bytes.Contains(c.Raw, []byte("stored outside this conversation")) {
					t.Error("local has the tool and was not told about it")
				}
				if bytes.Contains(c.Raw, []byte("are not available to you")) {
					t.Error("the rules deny what the tool gives")
				}
				if arm == ArmT1 && !bytes.Contains(c.Raw, []byte(`"thread_read"`)) {
					t.Error("the clone lost the arm's other local tool")
				}
			}
			if tool == 0 || answered == 0 {
				t.Errorf("offered %d, searched %d", tool, answered)
			}
			// The primary's own clone never had it.
			if rows[arm].Counters["event_reads"] != 0 {
				t.Errorf("%s read events: %v", arm, rows[arm].Counters)
			}
			// A batch for the cloud is the primary's.
			if got, want := fmt.Sprint(row.ByRecipient[corpus.SideCloud]), fmt.Sprint(rows[arm].ByRecipient[corpus.SideCloud]); got != want || want == "map[]" {
				t.Errorf("cloud batches: %s, primary %s", got, want)
			}
		})
	}
}

func TestCloudEventsLeaveOutLocalTurnsAndTheLog(t *testing.T) {
	s := shortSession()
	var cloud, local int
	for i, turn := range s.Turns {
		if turn.Owner == corpus.SideCloud && cloud == 0 {
			cloud = i
		}
		if turn.Owner == corpus.SideLocal && local == 0 {
			local = i
		}
	}
	r := &sessionRun{sess: s, rec: newToolRec()}
	for _, e := range []struct {
		turn       int
		name, text string
	}{
		{cloud, "read_record", "CLOUD RESULT"},
		{cloud, "thread_read", "LOG COPY"},
		{local, "read_local", "LOCAL RESULT"},
	} {
		r.rec.begin(e.turn, 0)
		r.rec.record(e.name, map[string]any{}, e.text, nil)
		r.rec.end()
	}
	events := r.cloudEvents()
	if len(events) != 1 || events[0].Text != "CLOUD RESULT" {
		t.Errorf("events = %+v", events)
	}
}

// S1 restates each label word for word, so search should find nearly all of them. This is a
// check on the search code. It says nothing about a workload whose questions are not literal.
func TestSearchRecallOnTheTuningCorpus(t *testing.T) {
	total, found := 0, 0
	for _, s := range corpus.Pilot()[:5] {
		var events []toolEvent
		for _, turn := range s.Turns {
			if turn.ReadRecord == "" || s.Records[turn.ReadRecord].Side != corpus.SideCloud {
				continue
			}
			events = append(events, toolEvent{Turn: turn.Index, Name: "read_record", Text: s.Records[turn.ReadRecord].Text})
		}
		for _, b := range s.Probes {
			for _, q := range b.Questions {
				if q.Category != corpus.F3 {
					continue
				}
				total++
				if strings.Contains(strings.Join(matchEvents(events, q.Text), "\n"), q.Answer) {
					found++
				}
			}
		}
	}
	if total == 0 || float64(found)/float64(total) < 0.9 {
		t.Errorf("search found %d of %d", found, total)
	}
	t.Logf("search found %d of %d", found, total)
}

// On S2 a read arm is a whole arm, so the list has to reach local's own requests and nothing
// else, and plain arms must not have it.
func TestTaskReadArmsOfferTheListToLocalAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	for arm, wants := range map[string]bool{ArmT1Read: true, ArmA4Read: true, ArmT1: false, ArmA4: false} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.loadTask(task, true)
			rows, err := f.env(0).Exec(context.Background(), Job{Arm: arm, Task: &task})
			if err != nil {
				t.Fatal(err)
			}
			if rows[0].Error != "" {
				t.Fatal(rows[0].Error)
			}
			offered := 0
			for _, c := range f.captures() {
				if !bytes.Contains(c.Raw, []byte(`"`+eventTool+`"`)) {
					continue
				}
				if c.Side != lab.SideLocal {
					t.Errorf("the tool was offered on %s", c.Side)
				}
				offered++
				if !bytes.Contains(c.Raw, []byte("stored outside this conversation")) {
					t.Error("local has the tool and was not told about it")
				}
				if bytes.Contains(c.Raw, []byte("are not available to you")) {
					t.Error("the rules deny what the tool gives")
				}
			}
			if (offered > 0) != wants {
				t.Errorf("offered in %d requests, want %v", offered, wants)
			}
		})
	}
}

// A4-abrupt on S2 is A4 with nothing carried at a switch, and A4 is the control that shows the
// test can see a brief.
func TestTaskAbruptArmCarriesNoBrief(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	for arm, wants := range map[string]bool{ArmA4Abr: false, ArmA4: true} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.loadTask(task, true)
			if _, err := f.env(0).Exec(context.Background(), Job{Arm: arm, Task: &task}); err != nil {
				t.Fatal(err)
			}
			got := false
			for _, c := range f.captures() {
				got = got || bytes.Contains(c.Raw, []byte(briefPrefix))
			}
			if got != wants {
				t.Errorf("a brief in a request: %v, want %v", got, wants)
			}
		})
	}
}

func TestRepeatTasksKeepTheFirstIDSoARunCanGrow(t *testing.T) {
	tasks := []s2.Task{{ID: "a"}, {ID: "b"}}
	var ids []string
	for _, x := range RepeatTasks(tasks, 2) {
		ids = append(ids, x.ID)
	}
	if strings.Join(ids, ",") != "a,b,a~r2,b~r2" {
		t.Errorf("ids = %v", ids)
	}
	if len(RepeatTasks(tasks, 1)) != 2 || len(RepeatTasks(nil, 3)) != 0 {
		t.Error("the plain case changed")
	}
	if tasks[0].ID != "a" {
		t.Errorf("the input changed: %v", tasks)
	}
}

func TestClonesAreNotWholeArmsOnSessions(t *testing.T) {
	for _, arm := range []string{ArmA4Abr, ArmA4Read, ArmT1Read} {
		if _, err := newArm(arm, &sessionRun{sess: shortSession()}); err == nil {
			t.Errorf("%s ran whole on a session set", arm)
		}
	}
}

// A session error used to leave a read clone with no cloud batches, which scored them 0 where
// its primary scored them whole.
func TestReadCloneKeepsCloudBatchesAfterASessionError(t *testing.T) {
	s := shortSession()
	clean := newFake(t, modeOracle, Window8K)
	runSession(t, clean, clean.env(0), ArmT1, s)
	for after := len(clean.captures()) - 1; after > 5; after-- {
		f := newFake(t, modeOracle, Window8K)
		f.failAfter = after
		rows := runSession(t, f, f.env(0), ArmT1, s)
		p, r := rows[ArmT1], rows[ArmT1Read]
		if p.Error == "" {
			continue
		}
		if got, want := fmt.Sprint(r.ByRecipient[corpus.SideCloud]), fmt.Sprint(p.ByRecipient[corpus.SideCloud]); got != want || want == "map[]" {
			t.Errorf("after a session error the clone has %s and the primary %s", got, want)
		}
		return
	}
	t.Fatal("no session error reached")
}

func TestOutlineShowsTheNewest(t *testing.T) {
	var events []toolEvent
	for i := range 80 {
		events = append(events, toolEvent{Name: "read_file", Text: fmt.Sprintf("result %d %s", i+1, strings.Repeat("x", 100))})
	}
	out, err := readEvents(events, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "#80 ") || strings.Contains(out, "#1 ") || !strings.Contains(out, "older results are not shown") {
		t.Errorf("outline:\n%s", out)
	}
	if n := len([]rune(out)); n > eventOutputChars+100 {
		t.Errorf("outline has %d characters", n)
	}
}

func TestALongLineCanBeReadPast(t *testing.T) {
	events := []toolEvent{{Name: "read_file", Text: strings.Repeat("z", 2*eventOutputChars) + "\nsecond"}}
	first, err := readEvents(events, map[string]any{"seq": 1})
	if err != nil || !strings.Contains(first, "zzz") || !strings.Contains(first, "offset 1") {
		t.Fatalf("first page: %v\n%.200s", err, first)
	}
	if n := len([]rune(first)); n > eventOutputChars+100 {
		t.Errorf("first page has %d characters", n)
	}
	second, err := readEvents(events, map[string]any{"seq": 1, "offset": 1})
	if err != nil || second != "second" {
		t.Errorf("second page: %v, %q", err, second)
	}
	end, err := readEvents(events, map[string]any{"seq": 1, "offset": 2})
	if err != nil || !strings.Contains(end, "end of this result") {
		t.Errorf("past the end: %v, %q", err, end)
	}
}

func TestFailedCallsStayOutOfTheList(t *testing.T) {
	r := newToolRec()
	r.begin(0, 0)
	r.record("read_file", map[string]any{}, "boom", errors.New("no such file"))
	r.record("read_file", map[string]any{}, "fine", nil)
	r.end()
	if len(r.events) != 1 || r.events[0].Text != "fine" {
		t.Errorf("events = %+v", r.events)
	}
}

// A whole read arm has its own ledger label, and its fork reads its own status.
func TestWholeReadArmForksUnderItsOwnLabel(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	old := forkBRefresh
	forkBRefresh = 1
	t.Cleanup(func() { forkBRefresh = old })
	task := s2.Tasks()[0]
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, true)
	env := f.env(0)
	rows, err := env.Exec(context.Background(), Job{Arm: ArmT1Read, Task: &task})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Counters["fork_b"] == 0 {
		t.Errorf("no fork ran: %v", rows[0].Counters)
	}
	for _, lr := range env.Ledger.Rows() {
		if lr.Arm != ArmT1Read {
			t.Errorf("a %s row under %s", lr.Role, lr.Arm)
		}
	}
}

// A task that opens on local reaches localBase and not the takeover path.
func TestReadArmThatOpensOnLocalIsToldTheSame(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	task.Turns[0].Owner = corpus.SideLocal
	for _, arm := range []string{ArmT1Read, ArmA4Read} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.loadTask(task, true)
			if _, err := f.env(0).Exec(context.Background(), Job{Arm: arm, Task: &task}); err != nil {
				t.Fatal(err)
			}
			offered := 0
			for _, c := range f.captures() {
				if !bytes.Contains(c.Raw, []byte(`"`+eventTool+`"`)) {
					continue
				}
				offered++
				if !bytes.Contains(c.Raw, []byte("stored outside this conversation")) || bytes.Contains(c.Raw, []byte("are not available to you")) {
					t.Errorf("request %s: the rules do not match the tool", c.Kind)
				}
			}
			if offered == 0 {
				t.Error("the tool was never offered")
			}
		})
	}
}

func TestAnAcceptanceTaskKeepsItsSetOnEveryPath(t *testing.T) {
	task := s2.Task{ID: "x", Set: "acceptance", Turns: []s2.Turn{{Owner: corpus.SideCloud}}}
	if got := taskSession(task).Set; got != "s2-acceptance" {
		t.Errorf("session set = %q", got)
	}
	e := &Env{APIKey: "sk-secretkey"}
	row := e.zeroRow(Job{Arm: ArmT1, Task: &task}, "boom sk-secretkey")
	if row.Set != "s2-acceptance" {
		t.Errorf("row set = %q", row.Set)
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("sk-secretkey")) {
		t.Errorf("the key reached the row: %s", raw)
	}
}

// A target past the limit would be refused if it were obeyed, whatever the last attempt did.
func TestFitSummaryOnlyAsksForWhatItWouldAccept(t *testing.T) {
	stated := regexp.MustCompile(`which is ([0-9,]+) characters`)
	for _, tc := range []struct{ draft, reply int }{{14000, 3400}, {8000, 5000}, {9000, 3000}, {20000, 5400}, {14000, 9000}} {
		var prompts []string
		rewrite := func(_ context.Context, p string) (string, error) {
			prompts = append(prompts, p)
			if len(prompts) == 1 {
				return strings.Repeat("y", tc.reply), nil
			}
			return strings.Repeat("y", summaryAim()), nil
		}
		if _, err := fitSummary(context.Background(), rewrite, strings.Repeat("x", tc.draft), &Counters{}); err != nil {
			t.Fatal(err)
		}
		if len(prompts) < 2 {
			t.Errorf("draft %d, reply %d: %d prompts", tc.draft, tc.reply, len(prompts))
		}
		for _, p := range prompts {
			m := stated.FindStringSubmatch(p)
			if m == nil {
				t.Fatalf("no target in %.200s", p)
			}
			n, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
			if err != nil {
				t.Fatal(err)
			}
			if n < rewriteFloor() || n > summaryCut() {
				t.Errorf("draft %d, reply %d: stated %d, outside %d to %d", tc.draft, tc.reply, n, rewriteFloor(), summaryCut())
			}
		}
	}
}

func TestStrayCallsCountOnAFailedFork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "r", "status": "completed", "usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
			"output": []any{map[string]any{"type": "function_call", "id": "fc_1", "call_id": "c1", "name": "read_record", "arguments": `{"id":"R-1"}`}}})
	}))
	t.Cleanup(srv.Close)
	cloud := lab.NewCloud(lab.Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: ArmT1, Session: "s"})
	body, err := json.Marshal(map[string]any{"model": "m", "instructions": "s", "tools": []any{}, "input": []any{map[string]any{"role": "user", "content": "q"}}})
	if err != nil {
		t.Fatal(err)
	}
	c := &Counters{}
	next, err := forkB(context.Background(), cloud, &lab.Snapshot{Body: body}, &threadLog{}, nil, c)
	if err != nil || next != nil {
		t.Fatalf("next %v, err %v", next, err)
	}
	if c.ForkFailed != 1 || c.ForkStrays == 0 {
		t.Errorf("fork_failed %d, fork_strays %d", c.ForkFailed, c.ForkStrays)
	}
}

// Only the local section is delivered, so only it is checked.
func TestMergedCheckReadsTheLocalSection(t *testing.T) {
	fits := strings.Repeat("a", summaryAim())
	over := strings.Repeat("b", summaryCut()+1)
	merged := func(local, cloud string) string { return mergedLocalMark + local + mergedEndMark + "\n" + cloud }
	if ok, why := checkMerged(merged(fits, over), true); !ok {
		t.Errorf("a long cloud section was refused: %s", why)
	}
	if ok, _ := checkMerged(merged(over, fits), true); ok {
		t.Error("a long local section passed")
	}
	if ok, _ := checkMerged(fits, true); ok {
		t.Error("a reply with no local section passed")
	}
}

// Fork A's prompt puts what matters first, so a cut drops the end and keeps the start.
func TestForkACutKeepsTheStart(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.forkAText = "FORKAHEAD\n" + strings.Repeat("x\n", 40*forkATarget) + "FORKATAIL"
	row := runBench(t, f, f.env(0), ArmC3A, benchItem())
	if row.Error != "" {
		t.Fatal(row.Error)
	}
	if row.Counters["summary_trims"] == 0 {
		t.Fatalf("no cut: %v", row.Counters)
	}
	seen := false
	for _, c := range f.captures() {
		if c.Kind == kindForkA || !bytes.Contains(c.Raw, []byte("FORKAHEAD")) {
			continue
		}
		seen = true
		if bytes.Contains(c.Raw, []byte("FORKATAIL")) {
			t.Errorf("a %s request holds the end of the cut text", c.Kind)
		}
	}
	if !seen {
		t.Error("the summary was never used")
	}
}

func TestReadViewDropsTheDenialEverywhereAndTheArmKeepsIt(t *testing.T) {
	if !strings.Contains(logRules, "are not available to you") || !strings.Contains(logRules, toolsDenied) {
		t.Errorf("the primary's rules lost the denial: %s", logRules)
	}
	receiver := &participant{system: "base " + logRules, compressSystem: "compress " + logRules}
	view := readView(receiver, &secondary{})
	if strings.Contains(view.system, toolsDenied) || strings.Contains(view.compressSystem, toolsDenied) {
		t.Errorf("the view denies what its tool gives: %s / %s", view.system, view.compressSystem)
	}
	if !strings.Contains(view.system, "stored outside this conversation") {
		t.Errorf("the view was not told about the list: %s", view.system)
	}
	if !strings.Contains(receiver.system, toolsDenied) || !strings.Contains(receiver.compressSystem, toolsDenied) {
		t.Error("the view changed the receiver")
	}
}

// A writer that came up short is asked for more, and one that ran long for less.
func TestFitSummaryMovesItsTargetWithTheLastAttempt(t *testing.T) {
	stated := regexp.MustCompile(`which is ([0-9,]+) characters`)
	second := func(reply int) int {
		var prompts []string
		rewrite := func(_ context.Context, p string) (string, error) {
			prompts = append(prompts, p)
			if len(prompts) == 1 {
				return strings.Repeat("y", reply), nil
			}
			return strings.Repeat("y", summaryAim()), nil
		}
		if _, err := fitSummary(context.Background(), rewrite, strings.Repeat("x", 20000), &Counters{}); err != nil {
			t.Fatal(err)
		}
		if len(prompts) < 2 {
			t.Fatalf("reply %d: %d prompts", reply, len(prompts))
		}
		n, err := strconv.Atoi(strings.ReplaceAll(stated.FindStringSubmatch(prompts[1])[1], ",", ""))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	long, short := second(9000), second(3000)
	if long >= summaryAim() || short <= summaryAim() {
		t.Errorf("after a long attempt %d, after a short one %d, aim %d", long, short, summaryAim())
	}
}

// A baseline told less about the workspace than T1 would lose for that alone.
func TestEveryTwoAgentLocalIsToldAboutTheWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	opensOn := func(side corpus.Side) s2.Task {
		for _, task := range s2.Tasks() {
			if task.Turns[0].Owner == side {
				return task
			}
		}
		t.Fatalf("no task opens on %s", side)
		return s2.Task{}
	}
	for _, task := range []s2.Task{opensOn(corpus.SideCloud), opensOn(corpus.SideLocal)} {
		for _, arm := range []string{ArmA4, ArmA4S, ArmA4Abr, ArmA4Read, ArmT1, ArmT1Read} {
			t.Run(task.ID+"/"+arm, func(t *testing.T) {
				f := newFake(t, modeOracle, Window8K)
				f.loadTask(task, true)
				if _, err := f.env(0).Exec(context.Background(), Job{Arm: arm, Task: &task}); err != nil {
					t.Fatal(err)
				}
				local := 0
				for _, c := range f.captures() {
					told := bytes.Count(c.Raw, []byte("share one workspace"))
					if c.Side == lab.SideLocal && c.Kind != kindCompress {
						local++
						if told != 1 {
							t.Errorf("a local %s request was told %d times", c.Kind, told)
						}
					}
					if c.Side != lab.SideLocal && told != 0 {
						t.Errorf("a cloud %s request has a local rule", c.Kind)
					}
				}
				if local == 0 {
					t.Error("no local request")
				}
			})
		}
	}
}

// The rule goes to every side of every arm, so it cannot favor one.
func TestTaskTestRuleReachesEveryArm(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	old := taskTestRule
	task := s2.Tasks()[0]
	for _, on := range []bool{false, true} {
		taskTestRule = on
		t.Cleanup(func() { taskTestRule = old })
		for _, arm := range []string{ArmA0, ArmA38K, ArmA4, ArmT1} {
			f := newFake(t, modeOracle, Window8K)
			f.loadTask(task, true)
			if _, err := f.env(0).Exec(context.Background(), Job{Arm: arm, Task: &task}); err != nil {
				t.Fatal(err)
			}
			for _, c := range f.captures() {
				if c.Kind == kindCompress || c.Kind == kindFork || c.Kind == kindBounce {
					continue
				}
				if got := bytes.Contains(c.Raw, []byte(taskTestText)); got != on {
					t.Errorf("%s %s/%s: rule present %v, want %v", arm, c.Side, c.Kind, got, on)
					break
				}
			}
		}
	}
}

// The key exists only when the rule is on, or every earlier manifest would stop matching.
func TestBudgetsNameTheTestRuleOnlyWhenItIsOn(t *testing.T) {
	old := taskTestRule
	t.Cleanup(func() { taskTestRule = old })
	taskTestRule = false
	if _, ok := Budgets()["task_test_rule"]; ok {
		t.Error("off, the key is present")
	}
	taskTestRule = true
	if Budgets()["task_test_rule"] != true {
		t.Error("on, the key is missing")
	}
}

// Every arm's writes go through a view, and a refusal is counted and leaves the file alone.
func TestAWriteWithoutReadingIsRefusedOnEveryArm(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	task := s2.Tasks()[0]
	args, err := json.Marshal(map[string]string{"path": "queue.go", "content": "package sched\n"})
	if err != nil {
		t.Fatal(err)
	}
	for _, arm := range []string{ArmA0, ArmA38K, ArmA4, ArmA4S, ArmT1} {
		t.Run(arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			f.loadTask(task, false)
			f.scripts[task.ID].turns[0].Calls = []fakeToolCall{{Name: "write_file", Args: string(args)}}
			rows, err := f.env(0).Exec(context.Background(), Job{Arm: arm, Task: &task})
			if err != nil {
				t.Fatal(err)
			}
			if rows[0].Error != "" {
				t.Fatal(rows[0].Error)
			}
			if got := rows[0].Counters["stale_writes"]; got != 1 {
				t.Errorf("stale_writes = %d, want 1", got)
			}
			told := false
			for _, c := range f.captures() {
				told = told || bytes.Contains(c.Raw, []byte("changed since you last read it"))
				if c.Side == lab.SideLocal && bytes.Contains(c.Raw, []byte("write_file {")) {
					t.Error("the log told local about a write that was refused")
				}
			}
			if !told {
				t.Error("the refusal never reached the model")
			}
		})
	}
}

// A log entry keeps only the start of the arguments, which has to name the file.
func TestToolSummaryNamesTheFileItWrote(t *testing.T) {
	r := newToolRec()
	r.begin(0, 0)
	r.record("write_file", map[string]any{"path": "loan.go", "content": strings.Repeat("x", 5000)}, "wrote", nil)
	log := &threadLog{}
	log.addTurn(corpus.SideCloud, corpus.Shared, "u", r.calls, "done")
	if got := log.entries[1].Text; !strings.Contains(got, "loan.go") || len(got) > argsMax+len("write_file ...") {
		t.Errorf("summary %q", got)
	}
	var back map[string]any
	if err := json.Unmarshal(marshalArgs(map[string]any{"path": "a", "content": "b"}), &back); err != nil || back["path"] != "a" || back["content"] != "b" {
		t.Errorf("%v, %v", back, err)
	}
	if got := string(marshalArgs(map[string]any{"path": "a"})); got != `{"path":"a"}` {
		t.Error(got)
	}
	if got := string(marshalArgs(map[string]any{})); got != "{}" {
		t.Error(got)
	}
}

func TestARefusedWriteIsCountedBySide(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on fixtures")
	}
	var task s2.Task
	for _, candidate := range s2.Tasks() {
		if candidate.Turns[0].Owner == corpus.SideLocal {
			task = candidate
			break
		}
	}
	var path string
	for name := range task.Files {
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			path = name
			break
		}
	}
	args, err := json.Marshal(map[string]string{"path": path, "content": "package x\n"})
	if err != nil {
		t.Fatal(err)
	}
	f := newFake(t, modeOracle, Window8K)
	f.loadTask(task, false)
	f.scripts[task.ID].turns[0].Calls = []fakeToolCall{{Name: "write_file", Args: string(args)}}
	rows, err := f.env(0).Exec(context.Background(), Job{Arm: ArmT1, Task: &task})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Counters["stale_writes"] != 1 || rows[0].Counters["stale_local"] != 1 {
		t.Errorf("counters %v", rows[0].Counters)
	}
}
