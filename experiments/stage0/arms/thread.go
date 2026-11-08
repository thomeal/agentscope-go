package arms

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// Log entry kinds from HARNESS.md.
const (
	kindUser        = "user"
	kindReply       = "reply"
	kindToolSummary = "tool_summary"
	kindSummary     = "summary"
	kindOwnerChange = "owner_change"
)

const (
	// sourceHarness marks entries no assistant wrote. Both sides import them.
	sourceHarness = "harness"
	threadSeqKey  = "thread_seq"
	// outlinePage keeps one outline read from filling an 8K window. HARNESS.md does
	// not bound it, and a 50-turn session's log has about 150 entries.
	outlinePage  = 40
	firstLineMax = 80
)

// logEntry is one entry of the in-memory log.
type logEntry struct {
	Seq        int
	Kind       string
	Source     string
	Visibility corpus.Disclosure
	Text       string
}

// threadLog is T1's log and the C group's. Sequence numbers start at 1.
type threadLog struct {
	entries []logEntry
}

func (l *threadLog) add(kind, source string, visibility corpus.Disclosure, text string) int {
	seq := len(l.entries) + 1
	l.entries = append(l.entries, logEntry{Seq: seq, Kind: kind, Source: source, Visibility: visibility, Text: text})
	return seq
}

func (l *threadLog) last() int { return len(l.entries) }

// addTurn writes a turn's user message, tool summaries and reply, which all take the
// turn's disclosure. It returns the last seq written.
func (l *threadLog) addTurn(source corpus.Side, visibility corpus.Disclosure, user string, calls []toolCall, reply string) int {
	l.add(kindUser, string(source), visibility, user)
	for _, c := range calls {
		l.add(kindToolSummary, string(source), visibility, c.Name+" "+clipArgs(c.Args))
	}
	return l.add(kindReply, string(source), visibility, reply)
}

// edgeSeqs returns the edge_only seqs up to seq: what a summary's coverage excludes.
func (l *threadLog) edgeSeqs(upTo int) []int {
	var out []int
	for _, e := range l.entries {
		if e.Seq <= upTo && e.Visibility == corpus.EdgeOnly {
			out = append(out, e.Seq)
		}
	}
	return out
}

// imports renders the shared entries past a cursor for one side, as plain text
// carrying thread_seq. A side's own entries are already in its context, and a summary
// reaches the local side through PeerSummary and is never replayed as text.
func (l *threadLog) imports(after int, to corpus.Side) []*message.Msg {
	var out []*message.Msg
	for _, e := range l.entries {
		if e.Seq <= after || e.Visibility != corpus.Shared || e.Source == string(to) || e.Kind == kindSummary {
			continue
		}
		msg := message.UserMsg("thread", renderEntry(e))
		msg.Metadata = types.JSONObject{threadSeqKey: strconv.Itoa(e.Seq)}
		out = append(out, msg)
	}
	return out
}

// argsMax bounds the arguments in a tool summary. A write_file call carries a whole
// file, which belongs in the workspace and would fill a local window as a log entry.
const argsMax = 200

func clipArgs(args string) string {
	if runes := []rune(args); len(runes) > argsMax {
		return string(runes[:argsMax]) + "..."
	}
	return args
}

func renderEntry(e logEntry) string {
	if e.Kind == kindOwnerChange {
		return fmt.Sprintf("[#%d owner_change] %s", e.Seq, e.Text)
	}
	return fmt.Sprintf("[#%d %s from %s] %s", e.Seq, e.Kind, e.Source, e.Text)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	line = strings.TrimSpace(line)
	if runes := []rune(line); len(runes) > firstLineMax {
		return string(runes[:firstLineMax]) + "..."
	}
	return line
}

// edgeIndex is the deterministic short index of edge_only entries for the local
// system prompt.
func (l *threadLog) edgeIndex() string {
	var b strings.Builder
	for _, e := range l.entries {
		if e.Visibility == corpus.EdgeOnly {
			fmt.Fprintf(&b, "#%d %s: %s\n", e.Seq, e.Kind, firstLine(e.Text))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// visibleTo is the only visibility rule: local sees everything, cloud sees shared.
func visibleTo(e logEntry, viewer corpus.Side) bool {
	return viewer == corpus.SideLocal || e.Visibility == corpus.Shared
}

// read implements thread_read.
func (l *threadLog) read(viewer corpus.Side, input map[string]any) (string, error) {
	if seq, ok := intArg(input, "seq"); ok {
		if seq < 1 || seq > len(l.entries) {
			return "", fmt.Errorf("no log entry #%d", seq)
		}
		e := l.entries[seq-1]
		if !visibleTo(e, viewer) {
			return "", fmt.Errorf("no log entry #%d", seq)
		}
		return e.Text, nil
	}
	before, hasBefore := intArg(input, "before")
	var lines []string
	for _, e := range l.entries {
		if visibleTo(e, viewer) && (!hasBefore || e.Seq < before) {
			lines = append(lines, fmt.Sprintf("#%d %s %s: %s", e.Seq, e.Kind, e.Source, firstLine(e.Text)))
		}
	}
	if len(lines) == 0 {
		return "(no entries)", nil
	}
	older := len(lines) - outlinePage
	if older > 0 {
		lines = lines[older:]
		return fmt.Sprintf("(%d older entries not shown)\n%s", older, strings.Join(lines, "\n")), nil
	}
	return strings.Join(lines, "\n"), nil
}

func intArg(input map[string]any, key string) (int, bool) {
	switch v := input[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	}
	return 0, false
}

// threadSeqOf returns the seq an imported message carries.
func threadSeqOf(m *message.Msg) (int, bool) {
	raw, ok := m.Metadata[threadSeqKey]
	if !ok {
		return 0, false
	}
	s, ok := raw.(string)
	if !ok {
		return 0, false
	}
	seq, err := strconv.Atoi(s)
	return seq, err == nil
}

// dropCovered removes imported messages a summary now covers. Imports are standalone
// text messages, so removing one cannot split a tool pair.
func dropCovered(msgs []*message.Msg, coverage int) []*message.Msg {
	return slices.DeleteFunc(slices.Clone(msgs), func(m *message.Msg) bool {
		seq, ok := threadSeqOf(m)
		return ok && seq <= coverage
	})
}

// summary is a Fork B result with its coverage: everything up to the snapshot,
// excluding edge_only seqs.
type summary struct {
	Text     string
	Coverage int
	Excluded []int
}

// peerText is safe on a nil summary.
func (s *summary) peerText() string {
	if s == nil {
		return ""
	}
	return s.Text
}

// toolsDenied is the part of logRules that a read clone cannot be told, because it can read
// those results.
const toolsDenied = ", and the results of the other assistant's tools are not available to you"

// logRules explains the imported entries, which only a log arm has. Without it, a 4B
// model that had just seen "[#43 reply from cloud] I've read record R-727" answered a
// request to read a local record by claiming to have read it, with no tool call.
const logRules = `[How this session works]
Another assistant and you take turns on this session. Messages that start with "[#" are entries from the session log. They record what the user and the other assistant did. They are not your own actions` + toolsDenied + `. A log entry saying a record was read does not mean you have read it.`

// recordRules is what every local model on S1 is told about records, with or without a
// log. It coaches the use of the tool, so no arm may have it alone.
const recordRules = `[Records]
When the user asks you to read a record, call your tool, and say you have read it only after the tool has returned. Your record tool reads only local records, whose ids start with "L-". It cannot read the other assistant's records. If a question is about a local record whose contents you cannot see here, read it again with your tool.`

// hostRecordRule is the part of recordRules true of a host with both record tools.
const hostRecordRule = `[Records]
When the user asks you to read a record, call your tool, and say you have read it only after the tool has returned.`

// s1LogRules is the rule text of a log arm on S1.
const s1LogRules = logRules + "\n\n" + recordRules

// localSystem places the peer summary and the index in the system prompt, the
// prototype's stand-in for a separate state slot: neither enters Context, so local
// compression cannot rewrite them.
func localSystem(base, rules string, peer *summary, index string) string {
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n" + rules)
	if peer != nil {
		fmt.Fprintf(&b, "\n\n[Summary from the other assistant, covering log entries through #%d]\n%s", peer.Coverage, peer.Text)
	}
	if index != "" {
		fmt.Fprintf(&b, "\n\n[Local-only log entries, not in any summary. Read one in full with thread_read.]\n%s", index)
	}
	return b.String()
}

// summaryBytesPerToken is the unit the agent elides its own summary in. A summary held
// outside Context counts against the same total in the same unit, or the rule below
// would not bound anything.
const summaryBytesPerToken = 4

// boundOwnSummary holds an own summary to the budget a new PeerSummary leaves. The agent
// applies SummaryMaxTokens only when it writes a summary, so one written under a larger
// budget would otherwise sit past the total until the next compression.
func boundOwnSummary(st *agent.AgentState, peer *summary, c *Counters) {
	text, cut := lab.TrimTokens(st.Summary, summaryBudget(peer)*summaryBytesPerToken/3)
	if cut {
		st.Summary = text
		c.LossyEvents++
		c.SummaryTrims++
	}
}

// summaryBudget is SummaryMaxTokens for the local side: PeerSummary plus the agent's
// own summary stay within localSummaryTotal. An arm with no summary outside Context,
// such as A4, has the whole total, and a brief in its Context is not charged to it.
func summaryBudget(peer *summary) int {
	return max(localSummaryTotal-len(peer.peerText())/summaryBytesPerToken, summaryFloor)
}
