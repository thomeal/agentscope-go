package thread

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
)

func at1(seq uint64, e Entry) Entry { //nolint:gocritic // test helper
	e.Seq = seq
	return e
}

func edgeOnly(e Entry) Entry { //nolint:gocritic // test helper
	e.Visibility = EdgeOnly
	return e
}

func tainted(e Entry) Entry { //nolint:gocritic // test helper
	e.Tainted = true
	return e
}

func texts(msgs []*message.Msg) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, msgText(m))
	}
	return out
}

func TestProjectRendersEveryKind(t *testing.T) {
	cloudMaster := Origin{Participant: "cloud", Path: "master"}
	cases := []struct {
		name  string
		entry Entry
		want  string
	}{
		{"user input", at1(12, userInput("hello")), "[#12 user] hello"},
		{"tainted user input", tainted(at1(12, userInput("hello"))), "[#12 user] (untrusted source) hello"},
		{"reply with digest", Entry{Seq: 13, Kind: KindReply, Origin: cloudMaster, Visibility: Shared, Content: textBlocks("done"),
			Digest: &ToolDigest{Calls: []ToolCallDigest{{Name: "Read", Target: "a.go", Count: 2}, {Name: "Grep", Count: 1}}}},
			"[#13 reply from cloud/master] done\n(tools: Read a.go ×2, Grep)"},
		{"reply without digest", Entry{Seq: 13, Kind: KindReply, Origin: cloudMaster, Visibility: Shared, Content: textBlocks("done")},
			"[#13 reply from cloud/master] done"},
		{"reply with only a digest", Entry{Seq: 13, Kind: KindReply, Origin: cloudMaster, Visibility: Shared,
			Digest: &ToolDigest{Calls: []ToolCallDigest{{Name: "Bash", Count: 1}}}},
			"[#13 reply from cloud/master]\n(tools: Bash)"},
		{"owner change with brief", Entry{Seq: 14, Kind: KindOwnerChange, Visibility: Shared,
			Owner: &OwnerChange{From: "edge", To: "cloud", Reason: ReasonTransfer, Brief: &Brief{Goal: "fix", Remaining: []string{"test"}}}},
			"[#14 foreground: edge → cloud, reason transfer]\nBrief:\nGoal: fix\nRemaining:\n- test"},
		{"first owner change", Entry{Seq: 14, Kind: KindOwnerChange, Visibility: Shared, Owner: &OwnerChange{To: "edge", Reason: ReasonRouter}},
			"[#14 foreground: none → edge, reason router]"},
		{"assign", Entry{Seq: 15, Kind: KindAssign, Origin: cloudMaster, Visibility: Shared, To: []string{"edge"},
			Task: &Task{ID: "t-3"}, Content: textBlocks("scan the disk")},
			"[#15 task t-3 from cloud/master to edge] scan the disk"},
		{"report", at1(16, report("edge", "found 3")), "[#16 report from edge] found 3"},
		{"report to peers", Entry{Seq: 16, Kind: KindReport, Origin: Origin{Participant: "edge"}, Visibility: Shared,
			To: []string{"cloud", "cloud2"}, Content: textBlocks("found 3")},
			"[#16 report from edge to cloud, cloud2] found 3"},
		{"tainted report", tainted(at1(16, report("edge", "found 3"))), "[#16 report from edge] (untrusted source) found 3"},
		{"artifact", Entry{Seq: 17, Kind: KindArtifact, Origin: cloudMaster, Visibility: Shared, Content: textBlocks("both ready"),
			Refs: []ArtifactRef{{URI: "s3://b/a.csv", Summary: "rows", Size: 2048}, {URI: "s3://b/b.csv"}}},
			"[#17 artifact from cloud/master] s3://b/a.csv — rows (2048 bytes)\ns3://b/b.csv\nboth ready"},
		{"artifact with text only", Entry{Seq: 17, Kind: KindArtifact, Origin: cloudMaster, Visibility: Shared, Content: textBlocks("pending")},
			"[#17 artifact from cloud/master] pending"},
		{"marker", Entry{Seq: 18, Kind: KindMarker, Visibility: Shared, Marker: &Marker{Kind: MarkerUnknownEffects, Detail: "rm may have run"}},
			"[#18 marker unknown_effects] rm may have run"},
		{"marker with text", Entry{Seq: 18, Kind: KindMarker, Visibility: Shared, Marker: &Marker{Kind: MarkerRejected}, Content: textBlocks("late")},
			"[#18 marker rejected_output]\nlate"},
		{"unknown kind", Entry{Seq: 19, Kind: "note", Origin: cloudMaster, Visibility: Shared, Content: textBlocks("x")},
			"[#19 note from cloud/master] x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Project([]Entry{tc.entry}, ProjectionSpec{Participant: "observer", Zone: ZoneEdge})
			if len(p.Messages) != 1 {
				t.Fatalf("%d messages, want 1", len(p.Messages))
			}
			m := p.Messages[0]
			if got := msgText(m); got != tc.want {
				t.Errorf("text\n got %q\nwant %q", got, tc.want)
			}
			if m.Name != "thread" || m.Role != message.RoleUser {
				t.Errorf("message from %q as %q, want a user message from thread", m.Name, m.Role)
			}
			if seq, ok := m.Metadata[SeqMetadataKey].(uint64); !ok || seq != tc.entry.Seq {
				t.Errorf("seq metadata %#v, want %d", m.Metadata[SeqMetadataKey], tc.entry.Seq)
			}
			if kind := m.Metadata[KindMetadataKey]; kind != string(tc.entry.Kind) {
				t.Errorf("kind metadata %#v, want %q", kind, tc.entry.Kind)
			}
			if v, ok := MsgVisibility(m); !ok || v != tc.entry.EffectiveVisibility() || m.Metadata[VisibilityMetadataKey] != string(v) {
				t.Errorf("visibility metadata %#v, want %q", m.Metadata[VisibilityMetadataKey], tc.entry.EffectiveVisibility())
			}
			if p.Through != tc.entry.Seq || p.Tainted != tc.entry.Tainted || p.Remaining {
				t.Errorf("projection %+v", p)
			}
		})
	}
}

func TestMsgVisibility(t *testing.T) {
	msg := func(v any) *message.Msg {
		m := message.UserMsg("thread", "x")
		if v != nil {
			m.Metadata[VisibilityMetadataKey] = v
		}
		return m
	}
	for _, tc := range []struct {
		name string
		m    *message.Msg
		want Visibility
		ok   bool
	}{
		{"nil message", nil, "", false},
		{"missing", msg(nil), "", false},
		{"shared string", msg("shared"), Shared, true},
		{"edge-only string", msg("edge_only"), EdgeOnly, true},
		{"typed value", msg(EdgeOnly), EdgeOnly, true},
		{"empty", msg(""), "", false},
		{"unknown", msg("public"), "", false},
		{"wrong type", msg(1), "", false},
	} {
		if v, ok := MsgVisibility(tc.m); v != tc.want || ok != tc.ok {
			t.Errorf("%s: %q %v, want %q %v", tc.name, v, ok, tc.want, tc.ok)
		}
	}
}

func TestProjectWithheldRuns(t *testing.T) {
	summary := Entry{Kind: KindSummary, Visibility: Shared, Coverage: &Coverage{}, Summary: &SummaryMeta{Target: "cloud"}}
	entries := []Entry{
		at1(1, userInput("a")),
		edgeOnly(at1(2, report("edge", "secret"))),
		at1(3, Entry{Kind: KindUserInput, Content: textBlocks("unset visibility")}),
		at1(4, report("edge", "b")),
		edgeOnly(at1(5, report("edge", "secret"))),
		at1(6, summary),
		tainted(edgeOnly(at1(7, report("edge", "secret")))),
		edgeOnly(at1(8, report("edge", "secret"))),
	}
	p := Project(entries, ProjectionSpec{Participant: "cloud", Zone: ZoneCloud})
	want := []string{
		"[#1 user] a",
		"[#2–#3 withheld: edge-only entries]",
		"[#4 report from edge] b",
		"[#5 withheld: edge-only entry]",
		"[#7–#8 withheld: edge-only entries]",
	}
	if got := texts(p.Messages); !reflect.DeepEqual(got, want) {
		t.Fatalf("messages\n got %q\nwant %q", got, want)
	}
	for i, wantSeq := range []uint64{1, 3, 4, 5, 8} {
		if got := p.Messages[i].Metadata[SeqMetadataKey]; got != wantSeq {
			t.Errorf("message %d seq %v, want %d", i, got, wantSeq)
		}
	}
	if kind := p.Messages[1].Metadata[KindMetadataKey]; kind != GapKind {
		t.Errorf("gap kind %v, want %q", kind, GapKind)
	}
	// Every message says what it stands for: gaps are edge-only, the
	// rendered entries shared.
	for i, want := range []Visibility{Shared, EdgeOnly, Shared, EdgeOnly, EdgeOnly} {
		if v, ok := MsgVisibility(p.Messages[i]); !ok || v != want {
			t.Errorf("message %d visibility %q %v, want %q", i, v, ok, want)
		}
	}

	// The edge reads every entry; edge-only ones say so, including the one
	// with no visibility set.
	for _, m := range Project(entries, ProjectionSpec{Participant: "edge2", Zone: ZoneEdge}).Messages {
		seq := m.Metadata[SeqMetadataKey].(uint64)
		want := entries[seq-1].EffectiveVisibility()
		if v, ok := MsgVisibility(m); !ok || v != want {
			t.Errorf("edge message #%d visibility %q %v, want %q", seq, v, ok, want)
		}
	}
	if !reflect.DeepEqual(p.Withheld, []uint64{2, 3, 5, 7, 8}) || p.Through != 8 || p.Tainted || p.Remaining {
		t.Errorf("projection withheld %v through %d tainted %v remaining %v", p.Withheld, p.Through, p.Tainted, p.Remaining)
	}
	for _, m := range p.Messages {
		if strings.Contains(msgText(m), "secret") || strings.Contains(msgText(m), "unset") {
			t.Fatalf("edge-only text reached the cloud: %q", msgText(m))
		}
	}

	// The edge reads every entry.
	edge := Project(entries, ProjectionSpec{Participant: "edge2", Zone: ZoneEdge})
	if len(edge.Messages) != 7 || len(edge.Withheld) != 0 || !edge.Tainted {
		t.Errorf("edge projection: %d messages, withheld %v, tainted %v", len(edge.Messages), edge.Withheld, edge.Tainted)
	}

	// A hole in the input splits the run.
	holes := Project([]Entry{edgeOnly(at1(2, userInput("x"))), edgeOnly(at1(4, userInput("y")))}, ProjectionSpec{Zone: ZoneCloud})
	if got := texts(holes.Messages); !reflect.DeepEqual(got, []string{"[#2 withheld: edge-only entry]", "[#4 withheld: edge-only entry]"}) {
		t.Errorf("runs across a hole: %q", got)
	}
}

func TestProjectSkips(t *testing.T) {
	entries := []Entry{
		at1(1, userInput("old")),
		at1(2, userInput("old")),
		at1(3, userInput("covered")),
		at1(4, userInput("excluded from the summary")),
		at1(5, replyFrom("edge", "mine")),
		at1(6, Entry{Kind: KindSummary, Visibility: EdgeOnly, Coverage: &Coverage{Through: 4}, Summary: &SummaryMeta{Target: "edge"}}),
		at1(7, userInput("new")),
		at1(8, report("cloud", "result")),
	}
	spec := ProjectionSpec{Participant: "edge", Zone: ZoneEdge, After: 2, Covered: &Coverage{Through: 4, Excluded: []uint64{4}}}
	cases := []struct {
		name   string
		mutate func(*ProjectionSpec)
		want   []string
	}{
		{"own and covered skipped", func(*ProjectionSpec) {}, []string{
			"[#4 user] excluded from the summary", "[#7 user] new", "[#8 report from cloud] result",
		}},
		{"include own", func(s *ProjectionSpec) { s.IncludeOwn = true }, []string{
			"[#4 user] excluded from the summary", "[#5 reply from edge/master] mine", "[#7 user] new", "[#8 report from cloud] result",
		}},
		{"no coverage", func(s *ProjectionSpec) { s.Covered = nil }, []string{
			"[#3 user] covered", "[#4 user] excluded from the summary", "[#7 user] new", "[#8 report from cloud] result",
		}},
		{"anonymous participant skips nothing", func(s *ProjectionSpec) { s.Participant = "" }, []string{
			"[#4 user] excluded from the summary", "[#5 reply from edge/master] mine", "[#7 user] new", "[#8 report from cloud] result",
		}},
		{"cursor at the end", func(s *ProjectionSpec) { s.After = 8 }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := spec
			tc.mutate(&s)
			p := Project(entries, s)
			if got := texts(p.Messages); !reflect.DeepEqual(got, tc.want) && (len(got) > 0 || len(tc.want) > 0) {
				t.Fatalf("messages\n got %q\nwant %q", got, tc.want)
			}
			if p.Through != 8 || p.Remaining || len(p.Withheld) != 0 {
				t.Errorf("through %d remaining %v withheld %v", p.Through, p.Remaining, p.Withheld)
			}
		})
	}

	// Skipped entries at the end still move the cursor.
	p := Project(entries[:6], ProjectionSpec{Participant: "edge", Zone: ZoneEdge, After: 3})
	if p.Through != 6 || len(p.Messages) != 1 {
		t.Errorf("trailing skips: through %d with %d messages, want 6 with 1", p.Through, len(p.Messages))
	}
}

func TestProjectBudget(t *testing.T) {
	ten := func(*message.Msg) int { return 10 }
	entries := []Entry{
		at1(1, userInput("a")),
		at1(2, userInput("b")),
		at1(3, Entry{Kind: KindSummary, Visibility: Shared, Coverage: &Coverage{}, Summary: &SummaryMeta{Target: "x"}}),
		at1(4, userInput("c")),
	}
	cases := []struct {
		max       int
		messages  int
		through   uint64
		remaining bool
	}{
		{max: 0, messages: 3, through: 4},
		{max: 25, messages: 2, through: 3, remaining: true},
		{max: 20, messages: 2, through: 3, remaining: true},
		{max: 30, messages: 3, through: 4},
		{max: 10, messages: 1, through: 1, remaining: true},
	}
	for _, tc := range cases {
		p := Project(entries, ProjectionSpec{Zone: ZoneEdge, MaxTokens: tc.max, Count: ten})
		if len(p.Messages) != tc.messages || p.Through != tc.through || p.Remaining != tc.remaining {
			t.Errorf("max %d: %d messages through %d remaining %v; want %d through %d remaining %v",
				tc.max, len(p.Messages), p.Through, p.Remaining, tc.messages, tc.through, tc.remaining)
		}
	}

	// A gap that does not fit is not consumed.
	gap := Project([]Entry{at1(1, userInput("a")), edgeOnly(at1(2, userInput("b"))), at1(3, userInput("c"))},
		ProjectionSpec{Zone: ZoneCloud, MaxTokens: 15, Count: ten})
	if len(gap.Messages) != 1 || gap.Through != 1 || len(gap.Withheld) != 0 || !gap.Remaining {
		t.Errorf("gap over budget: %d messages through %d withheld %v remaining %v", len(gap.Messages), gap.Through, gap.Withheld, gap.Remaining)
	}
	trailing := Project([]Entry{at1(1, userInput("a")), edgeOnly(at1(2, userInput("b")))},
		ProjectionSpec{Zone: ZoneCloud, MaxTokens: 15, Count: ten})
	if trailing.Through != 1 || !trailing.Remaining {
		t.Errorf("trailing gap over budget: through %d remaining %v", trailing.Through, trailing.Remaining)
	}
	// A first gap message over budget is still emitted whole.
	first := Project([]Entry{edgeOnly(at1(2, userInput("b")))}, ProjectionSpec{Zone: ZoneCloud, MaxTokens: 1})
	if got := texts(first.Messages); len(got) != 1 || got[0] != "[#2 withheld: edge-only entry]" || first.Through != 2 {
		t.Errorf("first gap over budget: %q through %d", got, first.Through)
	}
}

func TestProjectCutsOversizeFirstMessage(t *testing.T) {
	long := strings.Repeat("x", 400) + "TAIL"
	entries := []Entry{at1(9, userInput(long)), at1(10, userInput("short"))}
	p := Project(entries, ProjectionSpec{Zone: ZoneEdge, MaxTokens: 30})
	if len(p.Messages) != 1 || p.Through != 9 || !p.Remaining {
		t.Fatalf("%d messages through %d remaining %v; want the cut entry alone", len(p.Messages), p.Through, p.Remaining)
	}
	if !reflect.DeepEqual(p.Cut, []uint64{9}) {
		t.Fatalf("Cut = %v, want [9]", p.Cut)
	}
	if whole := Project(entries[1:], ProjectionSpec{Zone: ZoneEdge, MaxTokens: 30}); len(whole.Cut) != 0 {
		t.Fatalf("an entry that fits is reported cut: %v", whole.Cut)
	}
	got := msgText(p.Messages[0])
	notice := "… [cut; read #9 in full with thread_read]"
	if !strings.HasPrefix(got, "[#9 user] xxx") || !strings.HasSuffix(got, notice) || strings.Contains(got, "TAIL") {
		t.Fatalf("cut text %q", got)
	}
	// The default counter allows (runes+3)/4 <= 30, so the cut keeps 120 runes.
	if n := utf8.RuneCountInString(got); n != 120 {
		t.Errorf("cut text has %d runes, want 120", n)
	}
	if n := estimateTokens(p.Messages[0]); n > 30 {
		t.Errorf("cut message counts %d tokens, budget 30", n)
	}

	tiny := Project(entries, ProjectionSpec{Zone: ZoneEdge, MaxTokens: 1})
	if got := texts(tiny.Messages); len(got) != 1 || got[0] != notice || tiny.Through != 9 {
		t.Errorf("budget below the notice: %q through %d", got, tiny.Through)
	}
}

func TestProjectTaint(t *testing.T) {
	entries := []Entry{tainted(edgeOnly(at1(1, userInput("x")))), at1(2, userInput("y"))}
	if p := Project(entries, ProjectionSpec{Zone: ZoneCloud}); p.Tainted {
		t.Error("a withheld tainted entry tainted the projection")
	}
	if p := Project(entries, ProjectionSpec{Zone: ZoneEdge}); !p.Tainted {
		t.Error("a rendered tainted entry did not taint the projection")
	}
	covered := Project(entries, ProjectionSpec{Zone: ZoneEdge, Covered: &Coverage{Through: 1}})
	if covered.Tainted {
		t.Error("a covered tainted entry tainted the projection")
	}
}

func TestEstimateTokens(t *testing.T) {
	for text, want := range map[string]int{"": 0, "a": 1, "abcd": 1, "abcde": 2, "日本語です": 2} {
		if got := estimateTokens(message.UserMsg("u", text)); got != want {
			t.Errorf("estimateTokens(%q) = %d, want %d", text, got, want)
		}
	}
	if got := estimateTokens(message.UserMsg("u", []message.ContentBlock{message.DataBlock{}})); got != 0 {
		t.Errorf("message without text counts %d", got)
	}
}

func edgeIndexEntries() []Entry {
	return []Entry{
		at1(1, userInput("shared")),
		edgeOnly(at1(2, userInput("first line\nsecond line"))),
		edgeOnly(at1(3, Entry{Kind: KindSummary, Coverage: &Coverage{Through: 2}, Summary: &SummaryMeta{Target: "edge"}})),
		edgeOnly(at1(4, Entry{Kind: KindMarker, Marker: &Marker{Kind: MarkerInterrupted}})),
		edgeOnly(at1(5, ownerChange("edge", "cloud"))),
		edgeOnly(at1(6, report("edge", strings.Repeat("y", 100)))),
		edgeOnly(at1(7, Entry{Kind: KindArtifact, Refs: []ArtifactRef{{URI: "file:///x"}}})),
		edgeOnly(at1(8, Entry{Kind: KindAssign, Task: &Task{ID: "t-1"}})),
		edgeOnly(at1(9, Entry{Kind: KindReport})),
	}
}

func TestEdgeIndex(t *testing.T) {
	entries := edgeIndexEntries()
	all := strings.Join([]string{
		"#2 user_input: first line",
		"#4 marker: interrupted",
		"#5 owner_change: edge → cloud",
		"#6 report: " + strings.Repeat("y", 79) + "…",
		"#7 artifact: file:///x",
		"#8 assign: task t-1",
		"#9 report",
	}, "\n")
	cases := []struct {
		max  int
		want string
	}{
		{0, all},
		{1000, all},
		{55, "(5 older entries omitted)\n#8 assign: task t-1\n#9 report"},
		{54, "(6 older entries omitted)\n#9 report"},
		{10, ""},
	}
	for _, tc := range cases {
		if got := EdgeIndex(entries, tc.max); got != tc.want {
			t.Errorf("EdgeIndex(max %d)\n got %q\nwant %q", tc.max, got, tc.want)
		}
	}
	for limit := 1; limit < 400; limit++ {
		got := EdgeIndex(entries, limit)
		if n := utf8.RuneCountInString(got); n > limit {
			t.Fatalf("EdgeIndex(max %d) has %d runes", limit, n)
		}
		headerOnly := got == "(7 older entries omitted)"
		if got != "" && !headerOnly && !strings.HasSuffix(got, "#9 report") {
			t.Fatalf("EdgeIndex(max %d) dropped the newest entry: %q", limit, got)
		}
		if dropped := got != all; got != "" && dropped && !strings.HasPrefix(got, "(") {
			t.Fatalf("EdgeIndex(max %d) dropped entries without saying so: %q", limit, got)
		}
	}
	if got := EdgeIndex(entries[:1], 0); got != "" {
		t.Errorf("index of shared entries: %q", got)
	}
	first := EdgeIndex(entries, 120)
	for range 3 {
		if again := EdgeIndex(entries, 120); again != first {
			t.Fatalf("EdgeIndex is not deterministic: %q then %q", first, again)
		}
	}
}
