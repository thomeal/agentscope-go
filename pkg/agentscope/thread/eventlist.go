package thread

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Event list defaults and limits.
const (
	// defaultEventMaxRecords is the record cap when EventListOptions leaves
	// MaxRecords unset.
	defaultEventMaxRecords = 1000
	// defaultEventMaxBytes is the output byte cap when EventListOptions
	// leaves MaxBytes unset.
	defaultEventMaxBytes = 8 << 20
	// MaxEventPageRunes caps the runes one Page call returns.
	MaxEventPageRunes = 4000
	// eventFirstLineRunes bounds EventSummary.FirstLine.
	eventFirstLineRunes = 120
)

// ErrEventNotFound is returned by Page for an unknown or dropped record, or
// one the reader may not read.
var ErrEventNotFound = errors.New("thread: event not found")

// EventRecord is one tool output a participant produced. Records are kept
// outside the log and outside every context; a peer reads them on demand.
type EventRecord struct {
	// ID is assigned by Add: "ev-1", "ev-2", ...
	ID     string
	Origin Origin
	// Zone is the zone of the participant whose tool produced the output.
	Zone Zone
	// Visibility says whether a cloud reader may ever read the output. An
	// empty value is Shared; any value other than Shared or empty is treated
	// as EdgeOnly. A cloud reader never sees an EdgeOnly record, whatever
	// the CloudReadsEdge switch says. Edge participants set EdgeOnly for
	// outputs of private tools and of activations whose context holds
	// edge-only material.
	Visibility Visibility
	Tool       string
	Target     string
	Output     string
	Tainted    bool
	Time       time.Time
}

// effectiveVisibility treats an empty visibility as Shared and any unknown
// one as EdgeOnly.
func (r *EventRecord) effectiveVisibility() Visibility {
	if r.Visibility == "" || r.Visibility == Shared {
		return Shared
	}
	return EdgeOnly
}

// readableBy reports whether a reader in zone z may read the record.
func (r *EventRecord) readableBy(z Zone, cloudReadsEdge bool) bool {
	return Readable(z, r.Zone, cloudReadsEdge) && z.CanRead(r.effectiveVisibility())
}

// EventSummary describes a record without its output.
type EventSummary struct {
	ID, Tool, Target string
	// FirstLine is the first non-blank line of the output, at most 120 runes.
	FirstLine string
	Origin    Origin
	// Size is the output length in runes, the unit Page uses.
	Size    int
	Tainted bool
}

// EventListOptions bounds an EventList.
type EventListOptions struct {
	// MaxRecords drops the oldest records beyond this; default 1000.
	MaxRecords int
	// MaxBytes is the total output bytes kept; the oldest records are
	// dropped beyond it. Default 8 MiB. A single output larger than this is
	// cut to fit.
	MaxBytes int
}

// EventList keeps the tool outputs of participants so a peer in another
// zone can read them on demand. Nothing is pushed. It is safe for concurrent
// use.
type EventList struct {
	mu      sync.Mutex
	opts    EventListOptions
	records []eventEntry
	bytes   int
	next    int
}

type eventEntry struct {
	rec   EventRecord
	runes int
}

// NewEventList returns an empty list. Non-positive options take their
// defaults.
func NewEventList(opts EventListOptions) *EventList {
	if opts.MaxRecords <= 0 {
		opts.MaxRecords = defaultEventMaxRecords
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultEventMaxBytes
	}
	return &EventList{opts: opts}
}

// Add stores a record and returns its ID. Any caller-set ID is replaced, a
// zero Time is set to the current time and the visibility is stored as
// Shared or EdgeOnly. The oldest records are dropped when the list exceeds
// its caps.
func (l *EventList) Add(r EventRecord) string { //nolint:gocritic // value parameter keeps the documented API
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	r.Visibility = r.effectiveVisibility()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(r.Output) > l.opts.MaxBytes {
		r.Output = cutBytes(r.Output, l.opts.MaxBytes)
	}
	l.next++
	r.ID = fmt.Sprintf("ev-%d", l.next)
	l.records = append(l.records, eventEntry{rec: r, runes: utf8.RuneCountInString(r.Output)})
	l.bytes += len(r.Output)
	drop := 0
	for len(l.records)-drop > l.opts.MaxRecords || l.bytes > l.opts.MaxBytes {
		l.bytes -= len(l.records[drop].rec.Output)
		drop++
	}
	if drop > 0 {
		clear(l.records[:drop])
		l.records = l.records[drop:]
	}
	return r.ID
}

// cutBytes returns the longest prefix of s within n bytes that ends on a
// rune boundary.
func cutBytes(s string, n int) string {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// Readable reports whether a reader in zone z may read records produced in
// zone from. Edge readers read cloud records. Cloud readers read edge
// records only when cloudReadsEdge is true (a host-held switch, default
// off). Records from the reader's own zone are not offered (the reader has
// them). The lists also apply each record's Visibility: a cloud reader never
// reads an EdgeOnly record.
func Readable(z, from Zone, cloudReadsEdge bool) bool {
	switch {
	case z == ZoneEdge && from == ZoneCloud:
		return true
	case z == ZoneCloud && from == ZoneEdge:
		return cloudReadsEdge
	default:
		return false
	}
}

// Outline lists the records the reader may read, newest last. A positive
// limit keeps only the newest limit records.
func (l *EventList) Outline(reader Zone, cloudReadsEdge bool, limit int) []EventSummary {
	return l.list(reader, cloudReadsEdge, limit, func(*EventRecord) bool { return true })
}

// Search lists the readable records whose tool, target or output contains
// query, ignoring case, newest last. A positive limit keeps only the newest
// limit matches. An empty query matches every readable record.
func (l *EventList) Search(reader Zone, cloudReadsEdge bool, query string, limit int) []EventSummary {
	q := strings.ToLower(query)
	return l.list(reader, cloudReadsEdge, limit, func(r *EventRecord) bool {
		return strings.Contains(strings.ToLower(r.Tool), q) ||
			strings.Contains(strings.ToLower(r.Target), q) ||
			strings.Contains(strings.ToLower(r.Output), q)
	})
}

func (l *EventList) list(reader Zone, cloudReadsEdge bool, limit int, match func(*EventRecord) bool) []EventSummary {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []EventSummary
	for i := len(l.records) - 1; i >= 0; i-- {
		if limit > 0 && len(out) >= limit {
			break
		}
		e := &l.records[i]
		if !e.rec.readableBy(reader, cloudReadsEdge) || !match(&e.rec) {
			continue
		}
		out = append(out, EventSummary{
			ID:        e.rec.ID,
			Tool:      e.rec.Tool,
			Target:    e.rec.Target,
			FirstLine: truncateRunes(firstLine(e.rec.Output), eventFirstLineRunes),
			Origin:    e.rec.Origin,
			Size:      e.runes,
			Tainted:   e.rec.Tainted,
		})
	}
	// Collected newest first; return newest last.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Page returns limit runes of a readable record's output from offset, with
// the output's total length in runes. A non-positive limit, or one above
// MaxEventPageRunes, reads MaxEventPageRunes runes; a negative offset reads
// from the start. An unknown, dropped or unreadable record is
// ErrEventNotFound.
func (l *EventList) Page(reader Zone, cloudReadsEdge bool, id string, offset, limit int) (text string, total int, err error) {
	l.mu.Lock()
	var output string
	found := false
	for i := range l.records {
		r := &l.records[i].rec
		if r.ID == id {
			if r.readableBy(reader, cloudReadsEdge) {
				output, found = r.Output, true
			}
			break
		}
	}
	l.mu.Unlock()
	if !found {
		return "", 0, fmt.Errorf("%w: %q", ErrEventNotFound, id)
	}
	if limit <= 0 || limit > MaxEventPageRunes {
		limit = MaxEventPageRunes
	}
	offset = max(offset, 0)
	runes := []rune(output)
	total = len(runes)
	if offset >= total {
		return "", total, nil
	}
	end := min(offset+limit, total)
	return string(runes[offset:end]), total, nil
}
