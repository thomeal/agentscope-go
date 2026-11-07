package thread

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
)

// SeqMetadataKey is the Msg.Metadata key holding the entry seq. It equals
// agent.ThreadSeqMetadataKey.
const SeqMetadataKey = "thread_seq"

// KindMetadataKey is the Msg.Metadata key holding the kind of the entry a
// projected message renders. A message standing for withheld entries carries
// GapKind instead.
const KindMetadataKey = "thread_kind"

// GapKind is the KindMetadataKey value of a message that stands for a run of
// withheld entries.
const GapKind = string(MarkerGap)

// VisibilityMetadataKey is the Msg.Metadata key holding, as a string, the
// visibility of the entries a projected message stands for: the rendered
// entry's effective visibility, and EdgeOnly for a gap message (whose own
// text names only the withheld seqs). A participant reads it with
// MsgVisibility to tell edge-only imports apart without reading the log.
const VisibilityMetadataKey = "thread_visibility"

// MsgVisibility returns the visibility m carries under VisibilityMetadataKey.
// ok is false when m is nil or the key is missing or holds no known
// visibility; callers then look the entry up instead.
func MsgVisibility(m *message.Msg) (v Visibility, ok bool) {
	if m == nil {
		return "", false
	}
	switch x := m.Metadata[VisibilityMetadataKey].(type) {
	case string:
		v = Visibility(x)
	case Visibility:
		v = x
	default:
		return "", false
	}
	if v != Shared && v != EdgeOnly {
		return "", false
	}
	return v, true
}

// projectionSender is the name on every projected message.
const projectionSender = "thread"

// ProjectionSpec says what a participant imports from the log.
type ProjectionSpec struct {
	// Participant is the importing participant.
	Participant string
	// Zone is the participant's zone; entries it may not read are withheld.
	Zone Zone
	// After is the participant's cursor; entries at or before it are ignored.
	After uint64
	// Covered is the coverage of the newest summary the participant holds.
	// Entries it covers are skipped.
	Covered *Coverage
	// MaxTokens is the budget for the rendered messages; 0 means unlimited.
	MaxTokens int
	// Count counts a message's tokens; nil counts (runes+3)/4 of its text.
	Count func(*message.Msg) int
	// IncludeOwn renders entries this participant wrote. Only crash recovery
	// needs it; the participant otherwise already holds them.
	IncludeOwn bool
}

// Projection is the result of Project.
type Projection struct {
	// Messages are the rendered messages, oldest first.
	Messages []*message.Msg
	// Through is the last seq consumed (rendered, skipped or withheld): the
	// participant's new cursor. It equals After when nothing was consumed.
	Through uint64
	// Withheld lists the entries hidden because the zone may not read them.
	Withheld []uint64
	// Cut lists the entries rendered only in part because a first message
	// alone exceeded MaxTokens. The importer has consumed them without
	// seeing them in full, so a summary it writes must not claim them.
	Cut []uint64
	// Remaining is set when the projection stopped at MaxTokens with entries
	// left.
	Remaining bool
	// Tainted is set when some rendered entry was tainted.
	Tainted bool
}

// Project turns log entries into the text messages a participant imports.
// Entries must be in seq order. It is a pure function.
//
// Summary entries are consumed but never rendered; they belong in a separate
// summary slot. Entries covered by spec.Covered, and the participant's own
// entries unless IncludeOwn, are skipped. Entries the zone may not read are
// withheld: each run of consecutive withheld seqs becomes one gap message.
// When MaxTokens is set the projection stops before the first message that
// would exceed it; a first message that alone exceeds it is cut so the
// projection always makes progress.
func Project(entries []Entry, spec ProjectionSpec) Projection {
	p := projector{spec: spec, count: spec.Count, out: Projection{Through: spec.After}}
	if p.count == nil {
		p.count = estimateTokens
	}
	for i := range entries {
		e := &entries[i]
		if e.Seq <= spec.After {
			continue
		}
		skip := e.Kind == KindSummary ||
			spec.Covered.Covers(e.Seq) ||
			(!spec.IncludeOwn && spec.Participant != "" && e.Origin.Participant == spec.Participant)
		if !skip && !spec.Zone.CanRead(e.EffectiveVisibility()) {
			if n := len(p.run); n > 0 && p.run[n-1]+1 != e.Seq {
				if !p.flushRun() {
					return p.out
				}
			}
			p.run = append(p.run, e.Seq)
			continue
		}
		if !p.flushRun() {
			return p.out
		}
		if skip {
			p.out.Through = e.Seq
			continue
		}
		msg := projectedMsg(renderEntry(e), e.Seq, string(e.Kind), e.EffectiveVisibility())
		if !p.add(msg, e.Seq) {
			return p.out
		}
		p.out.Through = e.Seq
		if e.Tainted {
			p.out.Tainted = true
		}
	}
	p.flushRun()
	return p.out
}

type projector struct {
	spec  ProjectionSpec
	count func(*message.Msg) int
	used  int
	run   []uint64 // pending withheld seqs
	out   Projection
}

// flushRun emits the gap message for the pending withheld run. It reports
// false when the budget stopped the projection.
func (p *projector) flushRun() bool {
	if len(p.run) == 0 {
		return true
	}
	first, last := p.run[0], p.run[len(p.run)-1]
	var text string
	if first == last {
		text = fmt.Sprintf("[#%d withheld: edge-only entry]", first)
	} else {
		text = fmt.Sprintf("[#%d–#%d withheld: edge-only entries]", first, last)
	}
	// A gap message is never cut: it is short and names no readable entry.
	if !p.add(projectedMsg(text, last, GapKind, EdgeOnly), 0) {
		return false
	}
	p.out.Withheld = append(p.out.Withheld, p.run...)
	p.out.Through = last
	p.run = p.run[:0]
	return true
}

// add appends msg when it fits the budget, or when it is the first message,
// cutting it first when cutSeq names its entry. It reports false, and sets
// Remaining, when the budget stopped the projection.
func (p *projector) add(msg *message.Msg, cutSeq uint64) bool {
	n := p.count(msg)
	if limit := p.spec.MaxTokens; limit > 0 && p.used+n > limit {
		if len(p.out.Messages) > 0 {
			p.out.Remaining = true
			return false
		}
		if cutSeq != 0 {
			n = p.cut(msg, cutSeq)
			p.out.Cut = append(p.out.Cut, cutSeq)
		}
	}
	p.out.Messages = append(p.out.Messages, msg)
	p.used += n
	return true
}

// cut shortens msg's text to the longest prefix that, with the cut notice
// appended, fits MaxTokens, and returns its new count. When even the notice
// alone does not fit, msg keeps only the notice.
func (p *projector) cut(msg *message.Msg, seq uint64) int {
	runes := []rune(msgText(msg))
	notice := fmt.Sprintf("… [cut; read #%d in full with thread_read]", seq)
	try := func(k int) int {
		setMsgText(msg, string(runes[:k])+notice)
		return p.count(msg)
	}
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if try(mid) <= p.spec.MaxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return try(lo)
}

func projectedMsg(text string, seq uint64, kind string, vis Visibility) *message.Msg {
	m := message.UserMsg(projectionSender, text)
	m.Metadata[SeqMetadataKey] = seq
	m.Metadata[KindMetadataKey] = kind
	m.Metadata[VisibilityMetadataKey] = string(vis)
	return m
}

func msgText(m *message.Msg) string {
	if t := m.GetTextContent("\n"); t != nil {
		return *t
	}
	return ""
}

func setMsgText(m *message.Msg, text string) {
	id := ""
	if len(m.Content) > 0 {
		id = m.Content[0].GetID()
	}
	m.Content = []message.ContentBlock{message.TextBlock{Type: string(message.ContentBlockText), ID: id, Text: text}}
}

// estimateTokens is the default counter: (runes+3)/4 of the text.
func estimateTokens(m *message.Msg) int {
	return (utf8.RuneCountInString(msgText(m)) + 3) / 4
}

// renderEntry formats one entry: a header bracket, inline text, then any
// further lines.
func renderEntry(e *Entry) string {
	var head, inline string
	var extra []string
	text := e.Text()
	switch e.Kind {
	case KindUserInput:
		head = fmt.Sprintf("[#%d user]", e.Seq)
		inline = text
	case KindReply:
		head = fmt.Sprintf("[#%d reply from %s]", e.Seq, e.Origin)
		inline = text
		if d := e.Digest.Render(); d != "" {
			extra = append(extra, "(tools: "+d+")")
		}
	case KindOwnerChange:
		var o OwnerChange
		if e.Owner != nil {
			o = *e.Owner
		}
		head = fmt.Sprintf("[#%d foreground: %s → %s", e.Seq, nameOrNone(o.From), nameOrNone(o.To))
		if o.Reason != "" {
			head += ", reason " + string(o.Reason)
		}
		head += "]"
		inline = text
		if o.Brief != nil {
			extra = append(extra, "Brief:\n"+o.Brief.Render())
		}
	case KindAssign:
		id := ""
		if e.Task != nil {
			id = e.Task.ID
		}
		head = fmt.Sprintf("[#%d task %s from %s%s]", e.Seq, id, e.Origin, toSuffix(e.To))
		inline = text
	case KindReport:
		head = fmt.Sprintf("[#%d report from %s%s]", e.Seq, e.Origin, toSuffix(e.To))
		inline = text
	case KindArtifact:
		head = fmt.Sprintf("[#%d artifact from %s]", e.Seq, e.Origin)
		for _, r := range e.Refs {
			extra = append(extra, renderRef(r))
		}
		if text != "" {
			extra = append(extra, text)
		}
		if len(extra) > 0 {
			inline, extra = extra[0], extra[1:]
		}
	case KindMarker:
		kind := ""
		if e.Marker != nil {
			kind = string(e.Marker.Kind)
			inline = e.Marker.Detail
		}
		head = fmt.Sprintf("[#%d marker %s]", e.Seq, kind)
		if text != "" {
			extra = append(extra, text)
		}
	default:
		head = fmt.Sprintf("[#%d %s from %s]", e.Seq, e.Kind, e.Origin)
		inline = text
	}
	if e.Tainted {
		head += " (untrusted source)"
	}
	var sb strings.Builder
	sb.WriteString(head)
	if inline != "" {
		sb.WriteString(" ")
		sb.WriteString(inline)
	}
	for _, x := range extra {
		sb.WriteString("\n")
		sb.WriteString(x)
	}
	return sb.String()
}

func renderRef(r ArtifactRef) string {
	s := r.URI
	if r.Summary != "" {
		s += " — " + r.Summary
	}
	if r.Size > 0 {
		s += fmt.Sprintf(" (%d bytes)", r.Size)
	}
	return s
}

func nameOrNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func toSuffix(to []string) string {
	if len(to) == 0 {
		return ""
	}
	return " to " + strings.Join(to, ", ")
}

// edgeIndexLineRunes bounds the text part of one EdgeIndex line.
const edgeIndexLineRunes = 80

// EdgeIndex is a deterministic short index of entries a summary cannot hold:
// entries an edge participant may read that are EdgeOnly, newest last, one
// line each: "#101 user_input: <first line, at most 80 runes>". It keeps the
// newest lines that fit in maxRunes (0 = unlimited) and starts with
// "(N older entries omitted)" when it dropped some. Summary entries are not
// listed. The result never exceeds maxRunes; it is empty when not even the
// omission line fits.
func EdgeIndex(entries []Entry, maxRunes int) string {
	var lines []string
	for i := range entries {
		e := &entries[i]
		if e.Kind == KindSummary || e.EffectiveVisibility() != EdgeOnly {
			continue
		}
		lines = append(lines, edgeIndexLine(e))
	}
	if len(lines) == 0 {
		return ""
	}
	if maxRunes <= 0 {
		return strings.Join(lines, "\n")
	}
	// total counts the kept lines and the newlines between them.
	kept, total := 0, 0
	for k := len(lines) - 1; k >= 0; k-- {
		n := utf8.RuneCountInString(lines[k])
		if kept > 0 {
			n++
		}
		if total+n > maxRunes {
			break
		}
		total += n
		kept++
	}
	for kept < len(lines) {
		header := fmt.Sprintf("(%d older entries omitted)", len(lines)-kept)
		need := utf8.RuneCountInString(header)
		if kept > 0 {
			need++
		}
		if total+need <= maxRunes {
			return strings.Join(append([]string{header}, lines[len(lines)-kept:]...), "\n")
		}
		if kept == 0 {
			return ""
		}
		total -= utf8.RuneCountInString(lines[len(lines)-kept])
		if kept > 1 {
			total--
		}
		kept--
	}
	return strings.Join(lines, "\n")
}

func edgeIndexLine(e *Entry) string {
	text := firstLine(e.Text())
	if text == "" {
		switch {
		case e.Marker != nil:
			text = strings.TrimSpace(string(e.Marker.Kind) + " " + firstLine(e.Marker.Detail))
		case e.Owner != nil:
			text = nameOrNone(e.Owner.From) + " → " + nameOrNone(e.Owner.To)
		case len(e.Refs) > 0:
			text = e.Refs[0].URI
		case e.Task != nil:
			text = "task " + e.Task.ID
		}
	}
	text = truncateRunes(text, edgeIndexLineRunes)
	if text == "" {
		return fmt.Sprintf("#%d %s", e.Seq, e.Kind)
	}
	return fmt.Sprintf("#%d %s: %s", e.Seq, e.Kind, text)
}

// firstLine returns the first non-blank line of s, trimmed.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// truncateRunes cuts s to at most n runes, ending a cut string with "…".
func truncateRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
