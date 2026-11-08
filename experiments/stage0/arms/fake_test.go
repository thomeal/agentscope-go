package arms

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
	agentscope "github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope"
)

func TestMain(m *testing.M) {
	// The agent logs every fallback, and these tests provoke them on purpose.
	agentscope.Log().SetOutput(io.Discard)
	os.Exit(m.Run())
}

type fakeMode int

const (
	modeOracle fakeMode = iota
	modeNull
)

// Kinds of request the fake recognizes.
const (
	kindMain     = "main"
	kindProbe    = "probe"
	kindCompress = "compress"
	kindFork     = "fork"
	kindForkA    = "fork_a"
	kindMerged   = "merged"
	kindBrief    = "brief"
	kindCold     = "cold"
	kindShorten  = "shorten"
	kindBounce   = "bounce"
)

type scriptTurn struct {
	User, Tool, RecordID, Reply string
	Calls                       []fakeToolCall
}

// fakeScript is what the oracle knows about one session or bench item.
type fakeScript struct {
	turns   []scriptTurn
	batches []corpus.ProbeBatch
}

// capture is one request as the server received it.
type capture struct {
	Side   string
	Kind   string
	Marker string
	Raw    []byte
	Status string
	Text   string // the text the fake answered with
}

type fakeToolCall struct{ Name, Args string }

type decision struct {
	kind   string
	text   string
	calls  []fakeToolCall
	status string
}

// fake is a loopback server with both provider APIs, /tokenize and /v1/models. It
// rejects what the real providers reject where the arms depend on it: a function_call
// item id not starting with "fc", a tool output with no call, and a prompt over the
// local window.
type fake struct {
	t      *testing.T
	mode   fakeMode
	window int
	srv    *httptest.Server

	mu       sync.Mutex
	scripts  map[string]*fakeScript
	calls    []capture
	called   map[string]bool
	prev     map[string][]byte
	seq      int
	forks    int
	rejected []string
	lost     []string
	tokenize int

	// forkStatus overrides the status of the nth Fork B, counting from 1.
	forkStatus func(n int) string
	// charsPerToken is the token density of /tokenize and of the window check. Real
	// codes tokenize at under 2 characters per token. The default is 4.
	charsPerToken int
	// toolPreface makes the oracle write a line of text before each tool call.
	toolPreface bool
	// briefText overrides the text of every brief.
	briefText string
	// compressText and coldText override the first field of a compression and the reply to a
	// cold summary.
	compressText, coldText string
	// bounceText overrides the reply to a rejected summary, which otherwise fits.
	bounceText func() string
	// shortenText overrides the reply to a shortening request, which otherwise fits.
	shortenText func() string
	// forkAText overrides the text of a Fork A.
	forkAText string
	// forkText overrides the text of the nth Fork B, counting from 1.
	forkText func(n int) string
	// readEvents makes a local probe search the event list for its questions first.
	readEvents bool
	// mainText overrides a scripted main turn's reply on the cloud.
	mainText func() string
	// bounceStatus overrides the status of a bounced reply.
	bounceStatus string
	// compressEmpty makes the oracle fail every compression call.
	compressEmpty bool
	// probeCalls makes every probe request answer with this many tool calls.
	probeCalls int
	probeTool  string
	// malformed makes the next n probe answers invalid JSON.
	malformed int
	// onRequest runs before each model request is answered.
	onRequest func()
	// failAfter makes every model request past this many answer with a plain 400,
	// which the transports do not retry.
	failAfter int
	// routeReply answers an S3 query, by side.
	routeReply func(side, lastUser string) (string, bool)
}

// failing reports whether this request is past failAfter.
func (f *fake) failing(w http.ResponseWriter) bool {
	f.mu.Lock()
	over := f.failAfter > 0 && f.seq >= f.failAfter
	f.mu.Unlock()
	if over {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": map[string]any{"message": "scripted failure"}})
	}
	return over
}

func newFake(t *testing.T, mode fakeMode, window int) *fake {
	t.Helper()
	f := &fake{
		t: t, mode: mode, window: window,
		scripts: map[string]*fakeScript{},
		called:  map[string]bool{},
		prev:    map[string][]byte{},
	}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) loadSession(s corpus.Session) {
	sc := &fakeScript{batches: s.Probes}
	for _, turn := range s.Turns {
		st := scriptTurn{User: turn.User, RecordID: turn.ReadRecord, Reply: turn.ExpectedReply}
		if turn.ReadRecord != "" {
			st.Tool, _ = corpus.RecordTool(turn.Owner)
		}
		sc.turns = append(sc.turns, st)
	}
	f.scripts[s.ID] = sc
}

// loadTask scripts an S2 task. With oracle set, the first turn writes the solution. The
// fake needs to see the turn's text, so it only serves arms that keep the turn in the
// request, and every arm does.
func (f *fake) loadTask(t s2.Task, oracle bool) {
	sc := &fakeScript{}
	for i, turn := range t.Turns {
		st := scriptTurn{User: turn.User, Reply: fmt.Sprintf("done %d", i)}
		if i == 0 && oracle {
			for path, content := range t.Solution {
				args, err := json.Marshal(map[string]string{"path": path, "content": content})
				if err != nil {
					panic(err)
				}
				if _, exists := t.Files[path]; exists {
					readArgs, err := json.Marshal(map[string]string{"path": path})
					if err != nil {
						panic(err)
					}
					st.Calls = append(st.Calls, fakeToolCall{Name: "read_file", Args: string(readArgs)})
				}
				st.Calls = append(st.Calls, fakeToolCall{Name: "write_file", Args: string(args)})
			}
		}
		sc.turns = append(sc.turns, st)
	}
	f.scripts[t.ID] = sc
}

func (f *fake) loadBench(b corpus.CBench) {
	sc := &fakeScript{batches: b.Probes}
	for _, st := range b.History {
		sc.turns = append(sc.turns, scriptTurn{User: st.User, Tool: st.ToolName, RecordID: st.RecordID, Reply: st.Reply})
	}
	f.scripts[b.ID] = sc
}

// env returns a run environment on this fake with an in-memory ledger.
func (f *fake) env(stopUSD float64) *Env {
	f.t.Helper()
	ledger, err := lab.NewLedger("test", "", stopUSD)
	if err != nil {
		f.t.Fatal(err)
	}
	return &Env{
		Run: "test", Ledger: ledger,
		CloudURL: f.srv.URL, CloudModel: "luna", APIKey: "sk-test-key",
		LocalURL: f.srv.URL, LocalModel: "qwen",
		Tokens: lab.NewTokenizer(f.srv.URL, "qwen"),
	}
}

func (f *fake) captures() []capture {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capture(nil), f.calls...)
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/responses":
		f.responses(w, raw)
	case "/v1/chat/completions":
		f.chat(w, raw)
	case "/tokenize":
		var body struct {
			Prompt string `json:"prompt"`
		}
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.tokenize++
		f.mu.Unlock()
		writeJSON(w, map[string]any{"count": len(body.Prompt)/f.density() + 1})
	case "/v1/models":
		writeJSON(w, map[string]any{"data": []any{map[string]any{"id": "qwen", "max_model_len": f.window}}})
	default:
		http.NotFound(w, r)
	}
}

func (f *fake) tokenizeCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenize
}

func (f *fake) density() int {
	if f.charsPerToken > 0 {
		return f.charsPerToken
	}
	return 4
}

// needle is the start of text as it appears inside a JSON body.
func needle(text string) []byte {
	runes := []rune(text)
	if len(runes) > 160 {
		runes = runes[:160]
	}
	raw, err := json.Marshal(string(runes))
	if err != nil {
		panic(err)
	}
	return raw[1 : len(raw)-1]
}

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

// parsedRequest is a request reduced to what the oracle reads.
type parsedRequest struct {
	side     string
	marker   string
	session  string
	text     string
	lastUser string
	tools    []string
	replayed bool
	// submit is a checked fork, which answers through the tool it appended, and bounced
	// one whose last input is the host's reply to a rejected submission.
	submit, bounced bool
}

var markerShape = regexp.MustCompile(`\[stage0 run=\S+ arm=\S+ session=([^\]]+)\]`)

func (p *parsedRequest) setMarker(system string) {
	if m := markerShape.FindStringSubmatch(system); m != nil {
		p.marker, p.session = m[0], m[1]
	}
}

func collectStrings(v any, sb *strings.Builder) {
	switch x := v.(type) {
	case string:
		sb.WriteString(x)
		sb.WriteByte('\n')
	case []any:
		for _, e := range x {
			collectStrings(e, sb)
		}
	case map[string]any:
		for _, e := range x {
			collectStrings(e, sb)
		}
	}
}

func contentText(v any) string {
	var sb strings.Builder
	collectStrings(v, &sb)
	return sb.String()
}

func toolNames(v any) []string {
	var out []string
	items, _ := v.([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		if name, ok := m["name"].(string); ok {
			out = append(out, name)
		} else if fn, ok := m["function"].(map[string]any); ok {
			name, _ := fn["name"].(string)
			out = append(out, name)
		}
	}
	return out
}

// decide is the model. It looks at the request text and nothing else.
func (f *fake) decide(p parsedRequest) decision {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(p.tools) == 1 && p.tools[0] == structuredToolName {
		return f.compress(p)
	}
	switch {
	case p.bounced, strings.HasPrefix(p.lastUser, "Rejected: "):
		if f.bounceText != nil {
			return decision{kind: kindBounce, text: f.bounceText(), status: f.bounceStatus}
		}
		// A reply that fits. It keeps C4's delimiters when it is C4's loop.
		text := strings.Repeat("F", forkBTarget*24/10)
		if strings.Contains(p.lastUser, mergedLocalMark) {
			text = mergedLocalMark + "\n" + text + "\n" + mergedEndMark
		}
		return f.summaryDecision(kindBounce, text)
	case strings.HasPrefix(p.lastUser, shortenOpening):
		if f.shortenText != nil {
			return decision{kind: kindShorten, text: f.shortenText()}
		}
		return f.summaryDecision(kindShorten, strings.Repeat("S", forkBTarget*24/10))
	case strings.Contains(p.lastUser, mergedLocalMark):
		text := fmt.Sprintf("%s\nCLOUDPART-%d\n%s\nLOCALPART-%d\n%s", mergedCloudMark, f.seq, mergedLocalMark, f.seq, mergedEndMark)
		return f.summaryDecision(kindMerged, text)
	case strings.Contains(p.lastUser, ForkAPrompt):
		if f.forkAText != "" {
			return f.summaryDecision(kindForkA, f.forkAText)
		}
		return f.summaryDecision(kindForkA, fmt.Sprintf("FORKA-%d", f.seq))
	case strings.Contains(p.lastUser, HandoffPrompt):
		switch {
		case len(p.tools) == 0:
			if f.coldText != "" {
				return f.summaryDecision(kindCold, f.coldText)
			}
			return f.summaryDecision(kindCold, fmt.Sprintf("COLD-%d", f.seq))
		case p.replayed:
			f.forks++
			d := f.summaryDecision(kindFork, fmt.Sprintf("FORKTEXT-%d", f.forks))
			if f.forkText != nil {
				d.text = f.forkText(f.forks)
			}
			if f.forkStatus != nil {
				if s := f.forkStatus(f.forks); s != "" {
					d.status = s
				}
			}
			return d
		}
		if f.briefText != "" {
			return f.summaryDecision(kindBrief, f.briefText)
		}
		return f.summaryDecision(kindBrief, fmt.Sprintf("BRIEFTEXT-%d", f.seq))
	}
	if f.routeReply != nil {
		if text, ok := f.routeReply(p.side, p.lastUser); ok {
			return decision{kind: kindProbe, text: text}
		}
	}
	for _, sc := range f.scriptsFor(p) {
		for _, b := range sc.batches {
			if strings.Contains(p.text, b.Prompt) {
				if f.readEvents && slices.Contains(p.tools, eventTool) && !strings.Contains(p.text, eventQueryPrefix) {
					queries := make([]string, len(b.Questions))
					for i, q := range b.Questions {
						queries[i] = q.Text
					}
					args, _ := json.Marshal(map[string]any{"queries": queries})
					return decision{kind: kindProbe, calls: []fakeToolCall{{Name: eventTool, Args: string(args)}}}
				}
				return f.probe(b)
			}
		}
	}
	return f.mainTurn(p)
}

func (f *fake) summaryDecision(kind, text string) decision {
	if f.mode == modeNull {
		return decision{kind: kind}
	}
	return decision{kind: kind, text: text}
}

func (f *fake) scriptsFor(p parsedRequest) []*fakeScript {
	if sc, ok := f.scripts[p.session]; ok {
		return []*fakeScript{sc}
	}
	out := make([]*fakeScript, 0, len(f.scripts))
	for _, sc := range f.scripts {
		out = append(out, sc)
	}
	return out
}

func (f *fake) probe(b corpus.ProbeBatch) decision {
	if f.malformed > 0 {
		f.malformed--
		return decision{kind: kindProbe, text: `{"answers":[{"id":"q1":"not valid"}]}`}
	}
	if f.probeCalls > 0 {
		var calls []fakeToolCall
		for range f.probeCalls {
			calls = append(calls, fakeToolCall{Name: f.probeTool, Args: `{"id":"R-1"}`})
		}
		return decision{kind: kindProbe, calls: calls}
	}
	if f.mode == modeNull {
		return decision{kind: kindProbe, text: `{"answers":[]}`}
	}
	return decision{kind: kindProbe, text: oracleAnswers(b.Questions)}
}

func oracleAnswers(qs []corpus.Question) string {
	type entry struct {
		ID     string `json:"id"`
		Answer string `json:"answer"`
	}
	answers := make([]entry, 0, len(qs))
	for _, q := range qs {
		a := q.Answer
		if q.Category == corpus.F6 {
			a = corpus.Unknown
		}
		answers = append(answers, entry{q.ID, a})
	}
	raw, err := json.Marshal(map[string]any{"answers": answers})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// currentTurn is the highest turn whose user text is in the request. Older turns can
// be there too, as history or as imports, but later ones cannot.
func currentTurn(sc *fakeScript, text string) int {
	for i := len(sc.turns) - 1; i >= 0; i-- {
		if strings.Contains(text, sc.turns[i].User) {
			return i
		}
	}
	return -1
}

func (f *fake) mainTurn(p parsedRequest) decision {
	for _, sc := range f.scriptsFor(p) {
		i := currentTurn(sc, p.text)
		if i < 0 {
			continue
		}
		if f.mode == modeNull {
			return decision{kind: kindMain}
		}
		turn := sc.turns[i]
		key := fmt.Sprintf("%s#%s#%d", p.marker, p.side, i)
		if len(turn.Calls) > 0 && !f.called[key] {
			f.called[key] = true
			return decision{kind: kindMain, calls: turn.Calls}
		}
		if turn.Tool != "" && !f.called[key] {
			f.called[key] = true
			d := decision{kind: kindMain, calls: []fakeToolCall{{turn.Tool, toolArgs(turn.RecordID)}}}
			if f.toolPreface {
				d.text = fmt.Sprintf("PREFACE-%d", i)
			}
			return d
		}
		if f.mainText != nil && p.side == lab.SideCloud {
			return decision{kind: kindMain, text: f.mainText()}
		}
		return decision{kind: kindMain, text: turn.Reply}
	}
	f.lost = append(f.lost, firstLine(p.lastUser))
	return decision{kind: kindMain, text: "?"}
}

// compress carries the pending prompt or turn in next_steps. A forced compaction can
// fold the current message into the summary, and a real model would keep working on
// it from there.
func (f *fake) compress(p parsedRequest) decision {
	if f.mode == modeNull || f.compressEmpty {
		return decision{kind: kindCompress}
	}
	pending := ""
scan:
	for _, sc := range f.scriptsFor(p) {
		for _, b := range sc.batches {
			if strings.Contains(p.text, b.Prompt) {
				pending = b.Prompt
				break scan
			}
		}
		if i := currentTurn(sc, p.text); i >= 0 {
			pending = sc.turns[i].User
			break
		}
	}
	overview := "o"
	if f.compressText != "" {
		overview = f.compressText
	}
	args, err := json.Marshal(map[string]string{
		"task_overview": overview, "current_state": "s", "important_discoveries": "d",
		"next_steps": pending, "context_to_preserve": "c",
	})
	if err != nil {
		panic(err)
	}
	return decision{kind: kindCompress, calls: []fakeToolCall{{structuredToolName, string(args)}}}
}

func (f *fake) record(p parsedRequest, d decision, raw []byte, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, capture{Side: p.side, Kind: d.kind, Marker: p.marker, Raw: raw, Status: status, Text: d.text})
}

func (f *fake) reject(w http.ResponseWriter, why string) {
	f.mu.Lock()
	f.rejected = append(f.rejected, why)
	f.mu.Unlock()
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, map[string]any{"error": map[string]any{"message": why}})
}

func (f *fake) nextID() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	return f.seq
}

func commonPrefix(a, b []byte) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func (f *fake) cachedAgainstPrevious(key string, raw []byte) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := commonPrefix(f.prev[key], raw)
	f.prev[key] = raw
	return n
}

func (f *fake) responses(w http.ResponseWriter, raw []byte) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		f.reject(w, "bad json")
		return
	}
	p := parsedRequest{side: lab.SideCloud, tools: toolNames(body["tools"])}
	system, _ := body["instructions"].(string)
	p.setMarker(system)
	var sb strings.Builder
	sb.WriteString(system + "\n")
	callIDs := map[string]bool{}
	items, _ := body["input"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		switch m["type"] {
		case "function_call":
			if id, ok := m["id"].(string); ok && !strings.HasPrefix(id, "fc") {
				f.reject(w, "Invalid 'input[].id': expected an ID that begins with 'fc'.")
				return
			}
			id, _ := m["call_id"].(string)
			callIDs[id] = true
		case "function_call_output":
			id, _ := m["call_id"].(string)
			if !callIDs[id] {
				f.reject(w, "No tool call found for function call output with call_id "+id)
				return
			}
		case "message":
			p.replayed = true
		case "tool_search_output":
			p.submit = true
		}
		if role, _ := m["role"].(string); role == "user" {
			p.lastUser = contentText(m["content"])
		}
		p.bounced = p.submit && m["type"] == "function_call_output"
		collectStrings(m, &sb)
	}
	p.text = sb.String()
	if f.onRequest != nil {
		f.onRequest()
	}
	if f.failing(w) {
		return
	}
	d := f.decide(p)
	if p.submit && len(d.calls) == 0 {
		args, err := json.Marshal(map[string]string{"summary": d.text})
		if err != nil {
			panic(err)
		}
		d.calls, d.text = []fakeToolCall{{Name: lab.SubmitTool, Args: string(args)}}, ""
	}
	id := f.nextID()
	output := []any{map[string]any{"type": "reasoning", "id": fmt.Sprintf("rs_%d", id), "summary": []any{}, "encrypted_content": fmt.Sprintf("enc-%d", id)}}
	for i, c := range d.calls {
		output = append(output, map[string]any{
			"type": "function_call", "id": fmt.Sprintf("fc_%d_%d", id, i), "call_id": fmt.Sprintf("call_%d_%d", id, i),
			"name": c.Name, "arguments": c.Args, "status": "completed",
		})
	}
	if len(d.calls) == 0 || d.text != "" {
		output = append(output, map[string]any{
			"type": "message", "id": fmt.Sprintf("msg_%d", id), "role": "assistant", "status": "completed",
			"content": []any{map[string]any{"type": "output_text", "text": d.text, "annotations": nil}},
		})
	}
	status := d.status
	if status == "" {
		status = "completed"
	}
	f.record(p, d, raw, status)
	input := len(raw) / 4
	cached := f.cachedAgainstPrevious(p.marker, raw) / 4
	out := (len(d.text) + 3) / 4
	resp := map[string]any{
		"id": fmt.Sprintf("resp_%d", id), "status": status, "output": output,
		"usage": map[string]any{
			"input_tokens":          input,
			"input_tokens_details":  map[string]any{"cached_tokens": cached, "cache_write_tokens": input - cached},
			"output_tokens":         out + 10,
			"output_tokens_details": map[string]any{"reasoning_tokens": 10},
		},
	}
	if status == "incomplete" {
		resp["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
	}
	writeJSON(w, resp)
}

func (f *fake) chat(w http.ResponseWriter, raw []byte) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		f.reject(w, "bad json")
		return
	}
	p := parsedRequest{side: lab.SideLocal, tools: toolNames(body["tools"])}
	var sb strings.Builder
	callIDs := map[string]bool{}
	msgs, _ := body["messages"].([]any)
	for _, it := range msgs {
		m, _ := it.(map[string]any)
		role, _ := m["role"].(string)
		switch role {
		case "system":
			p.setMarker(contentText(m["content"]))
		case "user":
			p.lastUser = contentText(m["content"])
		case "tool":
			id, _ := m["tool_call_id"].(string)
			if !callIDs[id] {
				f.reject(w, "tool message without a preceding tool call: "+id)
				return
			}
		case "assistant":
			calls, _ := m["tool_calls"].([]any)
			for _, c := range calls {
				cm, _ := c.(map[string]any)
				id, _ := cm["id"].(string)
				callIDs[id] = true
			}
		}
		collectStrings(m, &sb)
	}
	p.text = sb.String()
	toolsRaw, err := json.Marshal(body["tools"])
	if err != nil {
		panic(err)
	}
	prompt := (len(p.text) + len(toolsRaw)) / f.density()
	if f.window > 0 && prompt > f.window {
		f.mu.Lock()
		f.calls = append(f.calls, capture{Side: p.side, Kind: "overflow", Marker: p.marker, Raw: raw})
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": map[string]any{
			"message": fmt.Sprintf("This model's maximum context length is %d tokens. However, your request has %d input tokens.", f.window, prompt),
			"type":    "BadRequestError",
		}})
		return
	}
	if f.onRequest != nil {
		f.onRequest()
	}
	if f.failing(w) {
		return
	}
	d := f.decide(p)
	id := f.nextID()
	message := map[string]any{"role": "assistant", "content": d.text}
	if len(d.calls) > 0 {
		var calls []any
		for i, c := range d.calls {
			calls = append(calls, map[string]any{
				"id": fmt.Sprintf("chatcmpl-tool-%d-%d", id, i), "type": "function",
				"function": map[string]any{"name": c.Name, "arguments": c.Args},
			})
		}
		message["tool_calls"] = calls
		if d.text == "" {
			message["content"] = nil
		}
	}
	finish := "stop"
	if d.status != "" {
		finish = d.status
	} else if len(d.calls) > 0 {
		finish = "tool_calls"
	}
	f.record(p, d, raw, finish)
	out := (len(d.text) + 3) / 4
	writeJSON(w, map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%d", id),
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": prompt, "completion_tokens": out, "total_tokens": prompt + out},
	})
}
