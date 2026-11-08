package s2

func longB04() Task {
	return Task{
		ID:     "s2-long-b04-mail-digest",
		Set:    "long",
		System: system("a tool that turns a mailing list's mbox file into a threaded digest"),
		Files: map[string]string{
			"go.mod": goMod,
			"message.go": `package mailthread

import (
	"errors"
	"strings"
	"time"
)

// ErrMalformed is the error for text that cannot be read as a message.
var ErrMalformed = errors.New("malformed message")

// Message is one mail. Header holds the raw header values.
type Message struct {
	ID         string
	InReplyTo  string
	References []string
	From       Address
	Subject    string
	Date       time.Time
	Header     map[string]string
	Body       string
}

// ParseHeaders cuts a raw message into its header values and body.
func ParseHeaders(raw string) (map[string]string, string, error) {
	head, body, _ := strings.Cut(raw, "\n\n")
	headers := map[string]string{}
	for _, line := range strings.Split(head, "\n") {
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, "", ErrMalformed
		}
		headers[name] = strings.TrimSpace(value)
	}
	return headers, body, nil
}
`,
			"fields.go": `package mailthread

// Address is who a mail came from.
type Address struct {
	Name, Email string
}

// String writes the address the way a header would.
func (a Address) String() string {
	if a.Name == "" {
		return "<" + a.Email + ">"
	}
	return a.Name + " <" + a.Email + ">"
}
`,
			"mbox.go": `package mailthread

import "strings"

// CountLines returns how many lines a text has.
func CountLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
}
`,
			"thread.go": `package mailthread

// Messages returns all the messages of some threads, thread by thread.
func Messages(threads []Thread) []Message {
	var out []Message
	for _, t := range threads {
		out = append(out, t.Messages...)
	}
	return out
}

// Thread is one conversation.
type Thread struct {
	Subject  string
	Messages []Message
	Pinned   bool
}
`,
			"digest.go": `package mailthread

import "fmt"

// plural writes a count of messages.
func plural(n int) string {
	if n == 1 {
		return "1 message"
	}
	return fmt.Sprintf("%d messages", n)
}
`,
			"stats.go": `package mailthread

// TotalMessages returns how many messages some threads have between them.
func TotalMessages(threads []Thread) int {
	total := 0
	for _, t := range threads {
		total += len(t.Messages)
	}
	return total
}
`,
			"docs/list-policy.txt": `What goes in the digest

These messages are left out:
- Automatic mail: an Auto-Submitted header with any value other than "no".
- Bulk mail: a Precedence header of bulk, junk or auto_reply.
- Spam: an X-Spam-Flag header of YES.
- Mail a moderator rejected: an X-Moderation header of rejected.
Header values are compared without regard to case.

Announcements
A thread whose first message has a subject starting with the tag [announce] is
pinned: it comes before all other threads in the digest, whatever its dates. The
tag is matched without regard to case.

Who reads the digest
It goes out once a day at 06:00 to the subscribers who asked for it. Moderators
get the full list traffic and do not read the digest.
`,
			"docs/digest-format.txt": `Digest format

A thread is written like this, with the blank lines exactly as shown:

    == <subject> (<n> messages)

    <sender>, <YYYY-MM-DD HH:MM>
    <body>

    <sender>, <YYYY-MM-DD HH:MM>
    <body>

A thread of one message says (1 message). Times are in UTC. Every line ends with a
newline, the last one included.

A digest starts with one line and one blank line, then its threads, with one blank
line between threads:

    Digest of <YYYY-MM-DD>: <t> threads, <m> messages

Bodies
- Everything from a line that is exactly "-- " on is a signature, and is cut.
- Lines starting with > are quotes, and are dropped.
- A line ending in "wrote:" just before a quote is dropped with it.
- Runs of blank lines become one, and there are none at the start or the end.
- The result is wrapped to 72 characters.
- If that is more than 20 lines, the first 20 are kept and followed by one line
  saying [...]
`,
			"mail_test.go": `package mailthread

import "testing"

func TestParseHeadersPlain(t *testing.T) {
	headers, body, err := ParseHeaders("a: 1\nb: 2\n\nthe body\n")
	if err != nil || body != "the body\n" || len(headers) != 2 {
		t.Fatalf("ParseHeaders = %v, %q, %v", headers, body, err)
	}
}

func TestParseHeadersBad(t *testing.T) {
	if _, _, err := ParseHeaders("no colon here\n\nbody"); err == nil {
		t.Fatal("no error")
	}
}

func TestAddressString(t *testing.T) {
	if got := (Address{Name: "Al", Email: "al@x"}).String(); got != "Al <al@x>" {
		t.Fatalf("String = %q", got)
	}
}

func TestCountLines(t *testing.T) {
	if CountLines("") != 0 || CountLines("a\nb\n") != 2 || CountLines("a") != 1 {
		t.Fatal("wrong")
	}
}

func TestTotalMessages(t *testing.T) {
	threads := []Thread{{Messages: []Message{{}, {}}}, {Messages: []Message{{}}}}
	if TotalMessages(threads) != 3 || len(Messages(threads)) != 3 {
		t.Fatal("wrong")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package mailthread_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	mailthread "fixture"
)

func at(day, hour int) time.Time { return time.Date(2024, 1, day, hour, 0, 0, 0, time.UTC) }

func subjects(threads []mailthread.Thread) []string {
	var out []string
	for _, t := range threads {
		out = append(out, t.Subject)
	}
	return out
}

func ids(msgs []mailthread.Message) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

func sent(id, subject string, date time.Time, refs ...string) mailthread.Message {
	return mailthread.Message{ID: id, Subject: subject, Date: date, References: refs}
}

func TestHeadersUnfoldAndLowerCase(t *testing.T) {
	headers, body, err := mailthread.ParseHeaders(strings.ReplaceAll("Subject: a long\n  subject\nFROM: x\nSubject: second\n\nbody\n", "\n", "\x0d\n"))
	if err != nil || headers["subject"] != "a long subject" || headers["from"] != "x" {
		t.Fatalf("ParseHeaders = %v, %v", headers, err)
	}
	if body != "body\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestHeaderErrorGivesLine(t *testing.T) {
	_, _, err := mailthread.ParseHeaders("a: 1\nno colon\n\nbody")
	var he *mailthread.HeaderError
	if !errors.As(err, &he) || he.Line != 2 || !errors.Is(err, mailthread.ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := mailthread.ParseHeaders("  continuation\n\nbody"); !errors.As(err, &he) || he.Line != 1 {
		t.Fatalf("err = %v", err)
	}
}

func TestParseIDsKeepsThemAsTheyAre(t *testing.T) {
	got := mailthread.ParseIDs("<Ab.1@X.org> junk <cd@y>\n <EF@z>")
	if !slices.Equal(got, []string{"Ab.1@X.org", "cd@y", "EF@z"}) {
		t.Fatalf("ParseIDs = %q", got)
	}
	if got := mailthread.ParseIDs("no ids here"); len(got) != 0 {
		t.Fatalf("ParseIDs = %q", got)
	}
}

func TestParseAddress(t *testing.T) {
	for in, want := range map[string]mailthread.Address{
		"Alice Smith <Alice@Example.org>": {Name: "Alice Smith", Email: "alice@example.org"},
		"\"Smith, Bob\" <bob@x.org>":      {Name: "Smith, Bob", Email: "bob@x.org"},
		"Carol@X.org":                     {Email: "carol@x.org"},
	} {
		got, err := mailthread.ParseAddress(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v, %v", in, got, err)
		}
	}
	if _, err := mailthread.ParseAddress("nobody at all"); !errors.Is(err, mailthread.ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
}

func TestSubjectStripsReplyMarks(t *testing.T) {
	for in, want := range map[string]string{
		"Hello":             "Hello",
		"Re: Hello":         "Hello",
		"RE: re: Hello":     "Hello",
		"AW: Hello":         "Hello",
		"aw: Re: Hello":     "Hello",
		"  Re:   Hello  ":   "Hello",
		"Regarding: Hello":  "Regarding: Hello",
		"Hello Re: goodbye": "Hello Re: goodbye",
	} {
		if got := mailthread.NormalizeSubject(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestSubjectStripsListTags(t *testing.T) {
	for in, want := range map[string]string{
		"[dev] Hello":           "Hello",
		"Re: [dev] Hello":       "Hello",
		"[dev] Re: [ops] Hello": "Hello",
		"Hello [dev] there":     "Hello [dev] there",
	} {
		if got := mailthread.NormalizeSubject(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestSubjectKeepsForwardMarks(t *testing.T) {
	for in, want := range map[string]string{
		"Fwd: Hello":     "Fwd: Hello",
		"FW: Hello":      "FW: Hello",
		"Re: Fwd: Hello": "Fwd: Hello",
	} {
		if got := mailthread.NormalizeSubject(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if got := (mailthread.Address{Name: "Al", Email: "al@x.org"}).Display(); got != "Al" {
		t.Fatalf("Display = %q", got)
	}
	if got := (mailthread.Address{Email: "carol@x.org"}).Display(); got != "carol" {
		t.Fatalf("Display = %q", got)
	}
}

func TestParseDate(t *testing.T) {
	got, ok := mailthread.ParseDate("Mon, 15 Jan 2024 10:30:00 +0100")
	if !ok || !got.Equal(time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)) || got.Location() != time.UTC {
		t.Fatalf("ParseDate = %v, %v", got, ok)
	}
	if _, ok := mailthread.ParseDate("next Tuesday"); ok {
		t.Fatal("next Tuesday is a date")
	}
}

func TestParseMessageFields(t *testing.T) {
	m, err := mailthread.ParseMessage("From: Alice <Al@X.org>\nSubject: =?UTF-8?Q?Gr=C3=BC=C3=9Fe?=\nMessage-ID: <M1@x>\nIn-Reply-To: <M0@x>\nReferences: <R1@x> <M0@x>\nDate: Mon, 15 Jan 2024 10:00:00 +0000\nX-Thing: yes\n\nHi\n")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "M1@x" || m.InReplyTo != "M0@x" || !slices.Equal(m.References, []string{"R1@x", "M0@x"}) {
		t.Fatalf("ids: %+v", m)
	}
	if m.From.Email != "al@x.org" || m.Subject != "Grüße" || m.Body != "Hi\n" || m.Header["x-thing"] != "yes" {
		t.Fatalf("fields: %+v", m)
	}
	if !m.Date.Equal(at(15, 10)) {
		t.Fatalf("Date = %v", m.Date)
	}
}

func TestParseMessageTolerant(t *testing.T) {
	m, err := mailthread.ParseMessage("From: not an address at all\nDate: whenever\nSubject: s\n\nb")
	if err != nil || !m.Date.IsZero() || m.ID != "" || m.From.Email != "" {
		t.Fatalf("ParseMessage = %+v, %v", m, err)
	}
}

func TestSplitMbox(t *testing.T) {
	got := mailthread.SplitMbox("From a@b Mon Jan 15\nSubject: x\n\nbody\nFrom me to you\n>From here\n\nFrom c@d Tue Jan 16\nSubject: y\n\nend\n")
	if len(got) != 2 {
		t.Fatalf("SplitMbox = %q", got)
	}
	if !strings.HasPrefix(got[0], "Subject: x\n") || !strings.Contains(got[0], "\nFrom me to you\nFrom here") {
		t.Fatalf("first = %q", got[0])
	}
	if !strings.HasPrefix(got[1], "Subject: y\n") || !strings.Contains(got[1], "end") {
		t.Fatalf("second = %q", got[1])
	}
}

func TestExcludedAutomatic(t *testing.T) {
	for _, h := range []map[string]string{
		{"auto-submitted": "auto-replied"}, {"precedence": "Bulk"}, {"precedence": "junk"},
		{"precedence": "AUTO_REPLY"}, {"x-spam-flag": "yes"}, {"x-moderation": "Rejected"},
	} {
		if !mailthread.Excluded(mailthread.Message{Header: h}) {
			t.Errorf("%v was kept", h)
		}
	}
}

func TestExcludedKeepsOrdinary(t *testing.T) {
	for _, h := range []map[string]string{
		nil, {"auto-submitted": "No"}, {"precedence": "list"}, {"x-spam-flag": "NO"}, {"x-moderation": "approved"},
	} {
		if mailthread.Excluded(mailthread.Message{Header: h}) {
			t.Errorf("%v was left out", h)
		}
	}
}

func TestFilter(t *testing.T) {
	got := mailthread.Filter([]mailthread.Message{
		{ID: "a"}, {ID: "b", Header: map[string]string{"precedence": "bulk"}}, {ID: "c"},
	})
	if !slices.Equal(ids(got), []string{"a", "c"}) {
		t.Fatalf("Filter = %q", ids(got))
	}
}

func TestParseMboxOrderAndErrors(t *testing.T) {
	msgs, errs := mailthread.ParseMbox("From a\nMessage-ID: <1@x>\n\none\n\nFrom b\nno colon\n\ntwo\n\nFrom c\nMessage-ID: <3@x>\n\nthree\n")
	if !slices.Equal(ids(msgs), []string{"1@x", "3@x"}) || len(errs) != 1 {
		t.Fatalf("ParseMbox = %q, %v", ids(msgs), errs)
	}
}

func TestParseMboxInheritsDates(t *testing.T) {
	msgs, errs := mailthread.ParseMbox("From a\nMessage-ID: <0@x>\n\nz\n\nFrom b\nMessage-ID: <1@x>\nDate: Mon, 15 Jan 2024 10:00:00 +0000\n\none\n\nFrom c\nMessage-ID: <2@x>\nDate: whenever\n\ntwo\n")
	if len(msgs) != 3 || len(errs) != 0 {
		t.Fatalf("ParseMbox = %+v, %v", msgs, errs)
	}
	if !msgs[0].Date.IsZero() || !msgs[2].Date.Equal(at(15, 10)) {
		t.Fatalf("dates: %v, %v", msgs[0].Date, msgs[2].Date)
	}
}

func TestFindByIDExact(t *testing.T) {
	msgs := []mailthread.Message{{ID: "ABC@x", Subject: "upper"}, {ID: "abc@x", Subject: "lower"}}
	got, ok := mailthread.FindByID(msgs, "abc@x")
	if !ok || got.Subject != "lower" {
		t.Fatalf("FindByID = %+v, %v", got, ok)
	}
	if _, ok := mailthread.FindByID(msgs, "Abc@X"); ok {
		t.Fatal("found by another case")
	}
}

func TestWrap(t *testing.T) {
	if got := mailthread.Wrap("aaa bbb ccc ddd", 7); got != "aaa bbb\nccc ddd" {
		t.Fatalf("Wrap = %q", got)
	}
	if got := mailthread.Wrap("short\n\nlonger than this", 8); got != "short\n\nlonger\nthan\nthis" {
		t.Fatalf("Wrap = %q", got)
	}
	if got := mailthread.Wrap("aaaaaaaaaaaa b", 5); got != "aaaaaaaaaaaa\nb" {
		t.Fatalf("Wrap = %q", got)
	}
	if got := mailthread.Wrap("    code stays as it is", 5); got != "    code stays as it is" {
		t.Fatalf("Wrap = %q", got)
	}
	if got := mailthread.Wrap("éééé éééé", 9); got != "éééé éééé" {
		t.Fatalf("Wrap = %q", got)
	}
}

func TestThreadsFollowReferences(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{
		sent("a", "First", at(1, 8)),
		sent("b", "Other", at(1, 9)),
		sent("c", "Re: First", at(1, 10), "a"),
		sent("d", "Re: First", at(1, 11), "gone", "c", "missing"),
	})
	if len(got) != 2 {
		t.Fatalf("BuildThreads = %+v", got)
	}
	var first mailthread.Thread
	for _, th := range got {
		if th.Subject == "First" {
			first = th
		}
	}
	if !slices.Equal(ids(first.Messages), []string{"a", "c", "d"}) {
		t.Fatalf("First = %q", ids(first.Messages))
	}
}

func TestThreadsInReplyToWithoutReferences(t *testing.T) {
	reply := sent("b", "another subject", at(1, 9))
	reply.InReplyTo = "a"
	got := mailthread.BuildThreads([]mailthread.Message{sent("a", "First", at(1, 8)), reply})
	if len(got) != 1 || len(got[0].Messages) != 2 {
		t.Fatalf("BuildThreads = %+v", got)
	}
}

func TestThreadMessagesInDateOrder(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{
		sent("late", "First", at(3, 8), "early"), sent("early", "First", at(1, 8)), sent("same", "First", at(3, 8), "early"),
	})
	if len(got) != 1 || !slices.Equal(ids(got[0].Messages), []string{"early", "late", "same"}) {
		t.Fatalf("BuildThreads = %+v", got)
	}
}

func TestThreadsSurviveCycles(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{sent("a", "x", at(1, 8), "b"), sent("b", "x", at(1, 9), "a"), sent("c", "y", at(1, 10), "c")})
	if len(got) != 2 {
		t.Fatalf("BuildThreads = %+v", got)
	}
}

func TestThreadIDsKeepCase(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{sent("ABC@x", "One", at(1, 8)), sent("abc@x", "Two", at(1, 9))})
	if len(got) != 2 {
		t.Fatalf("BuildThreads = %+v", got)
	}
}

func TestThreadsDropRepeatedID(t *testing.T) {
	copyOf := sent("a", "First", at(5, 8))
	copyOf.Body = "the list's copy"
	first := sent("a", "First", at(1, 8))
	first.Body = "the direct copy"
	got := mailthread.BuildThreads([]mailthread.Message{first, copyOf})
	if len(got) != 1 || len(got[0].Messages) != 1 || got[0].Messages[0].Body != "the direct copy" {
		t.Fatalf("BuildThreads = %+v", got)
	}
}

func TestThreadsKeepAllWithoutID(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{sent("", "One", at(1, 8)), sent("", "Two", at(1, 9))})
	if len(got) != 2 {
		t.Fatalf("BuildThreads = %+v", got)
	}
}

func TestThreadsJoinBySubject(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{
		sent("a", "[dev] Hello", at(1, 8)), sent("b", "RE: HELLO", at(1, 9)), sent("c", "Hello", at(1, 10)),
	})
	if len(got) != 2 {
		t.Fatalf("BuildThreads = %+v", got)
	}
	for _, th := range got {
		if slices.Contains(ids(th.Messages), "a") && !slices.Contains(ids(th.Messages), "b") {
			t.Fatalf("the reply did not join: %+v", got)
		}
		if slices.Contains(ids(th.Messages), "c") && len(th.Messages) != 1 {
			t.Fatalf("a fresh message joined: %+v", got)
		}
	}
}

func TestThreadsForwardStartsNew(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{sent("a", "Hello", at(1, 8)), sent("b", "Fwd: Hello", at(1, 9)), sent("c", "Re: Fwd: Hello", at(1, 10))})
	if len(got) != 2 || !slices.Contains(subjects(got), "Fwd: Hello") {
		t.Fatalf("BuildThreads = %+v", got)
	}
	for _, th := range got {
		if th.Subject == "Fwd: Hello" && !slices.Equal(ids(th.Messages), []string{"b", "c"}) {
			t.Fatalf("the forward's thread = %q", ids(th.Messages))
		}
	}
}

func TestThreadsNewestFirst(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{
		sent("a", "Old start", at(1, 8)), sent("b", "Middle", at(2, 8)), sent("c", "Re: Old start", at(3, 8), "a"), sent("d", "Oldest", at(1, 7)),
	})
	if !slices.Equal(subjects(got), []string{"Old start", "Middle", "Oldest"}) {
		t.Fatalf("BuildThreads = %q", subjects(got))
	}
}

func TestThreadsTieBySubject(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{sent("a", "Zebra", at(1, 8)), sent("b", "Apple", at(1, 8))})
	if !slices.Equal(subjects(got), []string{"Apple", "Zebra"}) {
		t.Fatalf("BuildThreads = %q", subjects(got))
	}
}

func TestThreadsPinAnnouncements(t *testing.T) {
	got := mailthread.BuildThreads([]mailthread.Message{sent("a", "[Announce] Release", at(1, 8)), sent("b", "Chat", at(5, 8)), sent("c", "Re: [dev] Fixes", at(6, 8))})
	if !slices.Equal(subjects(got), []string{"Release", "Fixes", "Chat"}) || !got[0].Pinned || got[1].Pinned {
		t.Fatalf("BuildThreads = %+v", got)
	}
}

func TestCleanBodyFollowsFormat(t *testing.T) {
	got := mailthread.CleanBody("\nHi all,\n\n\n\nsee below.\n\nOn Mon, Bob wrote:\n> quoted\n> more\n\nThanks\n-- \nAlice\nthe end\n")
	if got != "Hi all,\n\nsee below.\n\nThanks" {
		t.Fatalf("CleanBody = %q", got)
	}
}

func TestCleanBodyKeepsOtherWrote(t *testing.T) {
	got := mailthread.CleanBody("Bob wrote:\nnothing quoted here\n--\nnot a signature\n")
	if got != "Bob wrote:\nnothing quoted here\n--\nnot a signature" {
		t.Fatalf("CleanBody = %q", got)
	}
}

func TestCleanBodyHidesAddresses(t *testing.T) {
	got := mailthread.CleanBody("Mail Bob.Smith+x@example.org today, or a@b.co.uk.\nA @ alone stays.\n")
	if got != "Mail [address removed] today, or [address removed].\nA @ alone stays." {
		t.Fatalf("CleanBody = %q", got)
	}
}

func TestLatest(t *testing.T) {
	th := mailthread.Thread{Messages: []mailthread.Message{{Date: at(1, 8)}, {Date: at(3, 8)}, {Date: at(2, 8)}}}
	if got := th.Latest(); !got.Equal(at(3, 8)) {
		t.Fatalf("Latest = %v", got)
	}
}

func TestParticipants(t *testing.T) {
	th := mailthread.Thread{Messages: []mailthread.Message{
		{From: mailthread.Address{Name: "Zed", Email: "z@x"}},
		{From: mailthread.Address{Email: "carol@x"}},
		{From: mailthread.Address{Name: "Zed Again", Email: "z@x"}},
	}}
	if got := mailthread.Participants(th); !slices.Equal(got, []string{"Zed", "carol"}) && !slices.Equal(got, []string{"carol", "Zed"}) {
		t.Fatalf("Participants = %q", got)
	}
}

func TestTopSenders(t *testing.T) {
	from := func(name string) mailthread.Message {
		return mailthread.Message{From: mailthread.Address{Name: name, Email: name + "@x"}}
	}
	threads := []mailthread.Thread{{Messages: []mailthread.Message{from("B"), from("A"), from("B")}}, {Messages: []mailthread.Message{from("C"), from("A"), from("D")}}}
	got := mailthread.TopSenders(threads, 3)
	if !slices.Equal(got, []string{"A: 2", "B: 2", "C: 1"}) {
		t.Fatalf("TopSenders = %q", got)
	}
}

func oneThread(body string) mailthread.Thread {
	return mailthread.Thread{Subject: "Hello", Messages: []mailthread.Message{{
		From: mailthread.Address{Name: "Alice", Email: "alice@x.org"},
		Date: time.Date(2024, 1, 15, 23, 30, 0, 0, time.FixedZone("", -3600)),
		Body: body,
	}}}
}

func TestRenderThreadLayout(t *testing.T) {
	want := "== Hello (1 message)\n\nAlice, 2024-01-16 00:30\nHi there\n"
	if got := mailthread.RenderThread(oneThread("Hi there\n")); got != want {
		t.Fatalf("RenderThread = %q", got)
	}
}

func TestRenderThreadSeveral(t *testing.T) {
	th := oneThread("One\n")
	th.Messages = append(th.Messages, mailthread.Message{From: mailthread.Address{Email: "bob@x.org"}, Date: at(20, 9), Body: "Two\n"})
	want := "== Hello (2 messages)\n\nAlice, 2024-01-16 00:30\nOne\n\nbob, 2024-01-20 09:00\nTwo\n"
	if got := mailthread.RenderThread(th); got != want {
		t.Fatalf("RenderThread = %q", got)
	}
}

func TestRenderThreadCleansBody(t *testing.T) {
	got := mailthread.RenderThread(oneThread("Ask bob@x.org\nBob wrote:\n> no\n-- \nsig\n"))
	if !strings.HasSuffix(got, "00:30\nAsk [address removed]\n") {
		t.Fatalf("RenderThread = %q", got)
	}
}

func TestRenderThreadWrapsBody(t *testing.T) {
	a, b := strings.Repeat("a", 35), strings.Repeat("b", 36)
	got := mailthread.RenderThread(oneThread(a + " " + b + "\n" + b + " " + b + "\n"))
	if !strings.HasSuffix(got, "00:30\n"+a+" "+b+"\n"+b+"\n"+b+"\n") {
		t.Fatalf("RenderThread = %q", got)
	}
}

func TestRenderThreadLongBody(t *testing.T) {
	got := mailthread.RenderThread(oneThread(strings.Repeat("line\n", 25)))
	want := "00:30\n" + strings.Repeat("line\n", 20) + "[...]\n"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("RenderThread = %q", got)
	}
	exact := mailthread.RenderThread(oneThread(strings.Repeat("line\n", 20)))
	if strings.Contains(exact, "[...]") {
		t.Fatalf("RenderThread = %q", exact)
	}
}

func TestRenderDigest(t *testing.T) {
	a, b := oneThread("One\n"), oneThread("Two\n")
	b.Subject = "Other"
	want := "Digest of 2024-01-15: 2 threads, 2 messages\n\n" + mailthread.RenderThread(a) + "\n" + mailthread.RenderThread(b)
	if got := mailthread.RenderDigest(at(15, 6), []mailthread.Thread{a, b}); got != want {
		t.Fatalf("RenderDigest = %q", got)
	}
}

func TestIndexLine(t *testing.T) {
	th := mailthread.Thread{Subject: "Hello", Messages: []mailthread.Message{{Date: at(1, 8)}, {Date: time.Date(2024, 1, 20, 23, 30, 0, 0, time.FixedZone("", -3600))}}}
	if got := mailthread.IndexLine(th); got != "2024-01-21 Hello (2 messages)" {
		t.Fatalf("IndexLine = %q", got)
	}
}

func TestRelated(t *testing.T) {
	threads := []mailthread.Thread{{Subject: "Hello"}, {Subject: "Fwd: Hello"}, {Subject: "HELLO"}, {Subject: "Other"}}
	got := mailthread.Related(threads, "[dev] Re: hello")
	if !slices.Equal(subjects(got), []string{"Hello", "HELLO"}) {
		t.Fatalf("Related = %q", subjects(got))
	}
	if got := mailthread.Related(threads, "Re: Fwd: hello"); !slices.Equal(subjects(got), []string{"Fwd: Hello"}) {
		t.Fatalf("Related = %q", subjects(got))
	}
}

func TestDecodeBody(t *testing.T) {
	for _, c := range [][3]string{
		{"caf=C3=A9 =\nbar\n", "Quoted-Printable", "café bar\n"},
		{"aGVs\nbG8=\n", "BASE64", "hello"},
		{"as =C3 is", "8bit", "as =C3 is"},
		{"as =C3 is", "", "as =C3 is"},
	} {
		got, err := mailthread.DecodeBody(c[0], c[1])
		if err != nil || got != c[2] {
			t.Errorf("%q, %q: %q, %v", c[0], c[1], got, err)
		}
	}
	if _, err := mailthread.DecodeBody("!!!", "base64"); !errors.Is(err, mailthread.ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseMessageDecodesBody(t *testing.T) {
	m, err := mailthread.ParseMessage("Content-Transfer-Encoding: base64\n\naGVsbG8=\n")
	if err != nil || m.Body != "hello" {
		t.Fatalf("ParseMessage = %+v, %v", m, err)
	}
	m, err = mailthread.ParseMessage("Content-Transfer-Encoding: base64\n\n!!!\n")
	if err != nil || m.Body != "!!!\n" {
		t.Fatalf("ParseMessage = %+v, %v", m, err)
	}
}

func TestWeeklyCounts(t *testing.T) {
	threads := []mailthread.Thread{
		{Messages: []mailthread.Message{{Date: at(15, 8)}, {Date: at(1, 8)}, {}}},
		{Messages: []mailthread.Message{{Date: at(16, 8)}, {Date: time.Date(2023, 12, 31, 8, 0, 0, 0, time.UTC)}}},
		{Messages: []mailthread.Message{{Date: time.Date(2024, 1, 7, 23, 30, 0, 0, time.FixedZone("", -3600))}}},
	}
	want := []string{"2023-W52: 1", "2024-W01: 1", "2024-W02: 1", "2024-W03: 2"}
	if got := mailthread.WeeklyCounts(threads); !slices.Equal(got, want) {
		t.Fatalf("WeeklyCounts = %q", got)
	}
}

func TestWithoutSender(t *testing.T) {
	from := func(email string) mailthread.Message {
		return mailthread.Message{From: mailthread.Address{Email: email}}
	}
	threads := []mailthread.Thread{
		{Subject: "Keep", Messages: []mailthread.Message{from("a@x"), from("b@x")}},
		{Subject: "Gone", Messages: []mailthread.Message{from("b@x")}},
	}
	got := mailthread.WithoutSender(threads, "B@X")
	if !slices.Equal(subjects(got), []string{"Keep"}) || len(got[0].Messages) != 1 || got[0].Messages[0].From.Email != "a@x" {
		t.Fatalf("WithoutSender = %+v", got)
	}
	if len(threads[0].Messages) != 2 {
		t.Fatal("the threads passed in changed")
	}
}

func TestBuildDigest(t *testing.T) {
	text := "From a\nFrom: Alice <Alice@X.org>\nSubject: [dev] Plan\nMessage-ID: <Root@x>\nDate: Mon, 15 Jan 2024 10:00:00 +0000\n\nHello all.\n-- \nAlice\n\n" +
		"From b\nFrom: bob@x.org\nSubject: Re: [dev] Plan\nMessage-ID: <r1@x>\nReferences: <Root@x>\nDate: Mon, 15 Jan 2024 11:00:00 +0000\n\nAlice wrote:\n> Hello all.\n\nAsk carol@x.org.\n\n" +
		"From c\nFrom: robot@x.org\nSubject: Build\nMessage-ID: <b1@x>\nPrecedence: bulk\nDate: Mon, 15 Jan 2024 12:00:00 +0000\n\nnoise\n\n" +
		"From d\nFrom: Alice <alice@x.org>\nSubject: Fwd: Plan\nMessage-ID: <root@x>\nDate: whenever\n\nSee this.\n\n" +
		"From e\nFrom: bob@x.org\nSubject: Re: [dev] Plan\nMessage-ID: <r1@x>\nReferences: <Root@x>\nDate: Tue, 16 Jan 2024 11:00:00 +0000\n\nthe list's copy\n\n" +
		"From f\nno colon here\n\nlost\n"
	want := "Digest of 2024-01-16: 2 threads, 3 messages\n\n" +
		"== Fwd: Plan (1 message)\n\nAlice, 2024-01-15 12:00\nSee this.\n\n" +
		"== Plan (2 messages)\n\nAlice, 2024-01-15 10:00\nHello all.\n\nbob, 2024-01-15 11:00\nAsk [address removed].\n"
	got, errs := mailthread.BuildDigest(text, at(16, 6))
	if got != want || len(errs) != 1 {
		t.Fatalf("BuildDigest = %q, %v", got, errs)
	}
}

func TestThreadsSince(t *testing.T) {
	threads := []mailthread.Thread{
		{Subject: "New", Messages: []mailthread.Message{{Date: at(1, 8)}, {Date: at(9, 8)}}},
		{Subject: "Exactly", Messages: []mailthread.Message{{Date: at(5, 8)}}},
		{Subject: "Old", Messages: []mailthread.Message{{Date: at(4, 8)}}},
	}
	if got := mailthread.ThreadsSince(threads, at(5, 8)); !slices.Equal(subjects(got), []string{"New", "Exactly"}) {
		t.Fatalf("ThreadsSince = %q", subjects(got))
	}
}
`},
		Solution: map[string]string{
			"message.go": `package mailthread

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// ErrMalformed is the error for text that cannot be read as a message.
var ErrMalformed = errors.New("malformed message")

// HeaderError says which line of a header block could not be read.
type HeaderError struct {
	Line int // 1-based
	Text string
}

func (e *HeaderError) Error() string { return fmt.Sprintf("header line %d: %q", e.Line, e.Text) }

// Unwrap makes a HeaderError match ErrMalformed.
func (e *HeaderError) Unwrap() error { return ErrMalformed }

// Message is one mail. Header holds the raw header values.
type Message struct {
	ID         string
	InReplyTo  string
	References []string
	From       Address
	Subject    string
	Date       time.Time
	Header     map[string]string
	Body       string
}

// ParseHeaders cuts a raw message into its header values, by lower-case name, and body.
// Folded lines are joined, and the first of two values for a name wins.
func ParseHeaders(raw string) (map[string]string, string, error) {
	raw = strings.ReplaceAll(raw, "\x0d\n", "\n")
	head, body, _ := strings.Cut(raw, "\n\n")
	headers := map[string]string{}
	name, keep := "", false
	for i, line := range strings.Split(head, "\n") {
		switch {
		case line == "":
		case line[0] == ' ' || line[0] == '\t':
			if name == "" {
				return nil, "", &HeaderError{Line: i + 1, Text: line}
			}
			if keep {
				headers[name] += " " + strings.TrimSpace(line)
			}
		default:
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				return nil, "", &HeaderError{Line: i + 1, Text: line}
			}
			name = strings.ToLower(strings.TrimSpace(key))
			_, seen := headers[name]
			if keep = !seen; keep {
				headers[name] = strings.TrimSpace(value)
			}
		}
	}
	return headers, body, nil
}

var messageID = regexp.MustCompile("<([^<>\\s]+)>")

// ParseIDs returns the message IDs in a header value, as they came, without their brackets.
func ParseIDs(s string) []string {
	var out []string
	for _, match := range messageID.FindAllStringSubmatch(s, -1) {
		out = append(out, match[1])
	}
	return out
}

// ParseDate reads a Date header, giving the time in UTC.
func ParseDate(s string) (time.Time, bool) {
	t, err := mail.ParseDate(s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// DecodeBody undoes a Content-Transfer-Encoding. Others leave the body as it is.
func DecodeBody(body, encoding string) (string, error) {
	var decoded []byte
	var err error
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		decoded, err = io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	case "base64":
		decoded, err = base64.StdEncoding.DecodeString(body)
	default:
		return body, nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s body: %v", ErrMalformed, encoding, err)
	}
	return string(decoded), nil
}

// ParseMessage reads one mail. A From or Date it cannot read stays empty, and a
// body it cannot decode stays raw.
func ParseMessage(raw string) (Message, error) {
	headers, body, err := ParseHeaders(raw)
	if err != nil {
		return Message{}, err
	}
	m := Message{Header: headers, Body: body, References: ParseIDs(headers["references"])}
	if ids := ParseIDs(headers["message-id"]); len(ids) > 0 {
		m.ID = ids[0]
	}
	if ids := ParseIDs(headers["in-reply-to"]); len(ids) > 0 {
		m.InReplyTo = ids[0]
	}
	if decoded, err := DecodeBody(body, headers["content-transfer-encoding"]); err == nil {
		m.Body = decoded
	}
	m.From, _ = ParseAddress(headers["from"])
	m.Date, _ = ParseDate(headers["date"])
	m.Subject = headers["subject"]
	if decoded, err := new(mime.WordDecoder).DecodeHeader(m.Subject); err == nil {
		m.Subject = decoded
	}
	return m, nil
}
`,
			"fields.go": `package mailthread

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

// Address is who a mail came from.
type Address struct {
	Name, Email string
}

// String writes the address the way a header would.
func (a Address) String() string {
	if a.Name == "" {
		return "<" + a.Email + ">"
	}
	return a.Name + " <" + a.Email + ">"
}

// Display is how a sender is shown to people.
func (a Address) Display() string {
	if a.Name != "" {
		return a.Name
	}
	local, _, _ := strings.Cut(a.Email, "@")
	return local
}

// ParseAddress reads a From header. The email is kept in lower case.
func ParseAddress(s string) (Address, error) {
	parsed, err := mail.ParseAddress(s)
	if err != nil {
		return Address{}, fmt.Errorf("%w: address %q", ErrMalformed, s)
	}
	return Address{Name: parsed.Name, Email: strings.ToLower(parsed.Address)}, nil
}

var (
	replyMark = regexp.MustCompile("(?i)^(re|aw):\\s*")
	listTag   = regexp.MustCompile("^\\[[^\\]]*\\]\\s*")
)

// NormalizeSubject returns what a subject is about, without reply marks or list tags.
func NormalizeSubject(s string) string {
	s = strings.TrimSpace(s)
	for {
		next := listTag.ReplaceAllString(replyMark.ReplaceAllString(s, ""), "")
		if next == s {
			return s
		}
		s = next
	}
}

// isReply reports whether a raw subject carries a reply mark, under any list tags.
func isReply(s string) bool {
	return replyMark.MatchString(listTag.ReplaceAllString(strings.TrimSpace(s), ""))
}
`,
			"mbox.go": `package mailthread

import (
	"fmt"
	"strings"
	"time"
)

// CountLines returns how many lines a text has.
func CountLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
}

// SplitMbox cuts an mbox file into its raw messages.
func SplitMbox(text string) []string {
	var out []string
	var cur []string
	lines := strings.Split(strings.ReplaceAll(text, "\x0d\n", "\n"), "\n")
	flush := func() {
		if cur != nil {
			out = append(out, strings.TrimRight(strings.Join(cur, "\n"), "\n")+"\n")
		}
	}
	for i, line := range lines {
		if strings.HasPrefix(line, "From ") && (i == 0 || lines[i-1] == "") {
			flush()
			cur = []string{}
			continue
		}
		if rest, ok := strings.CutPrefix(line, ">From "); ok {
			line = "From " + rest
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

// ParseMbox reads a whole file, going on past messages it cannot read.
func ParseMbox(text string) ([]Message, []error) {
	var msgs []Message
	var errs []error
	var last time.Time
	for i, raw := range SplitMbox(text) {
		m, err := ParseMessage(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("message %d: %w", i+1, err))
			continue
		}
		if m.Date.IsZero() {
			m.Date = last
		}
		last = m.Date
		msgs = append(msgs, m)
	}
	return msgs, errs
}
`,
			"thread.go": `package mailthread

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// Messages returns all the messages of some threads, thread by thread.
func Messages(threads []Thread) []Message {
	var out []Message
	for _, t := range threads {
		out = append(out, t.Messages...)
	}
	return out
}

// Thread is one conversation.
type Thread struct {
	Subject  string
	Messages []Message
	Pinned   bool
}

// Latest returns the date of the thread's newest message.
func (t Thread) Latest() time.Time {
	var latest time.Time
	for _, m := range t.Messages {
		if m.Date.After(latest) {
			latest = m.Date
		}
	}
	return latest
}

// oncePerID leaves out a message whose ID was already seen. Without an ID, it stays.
func oncePerID(msgs []Message) []Message {
	seen := map[string]bool{}
	var out []Message
	for _, m := range msgs {
		if m.ID != "" && seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out
}

// forest records which message each one hangs under.
type forest []int

func (f forest) root(i int) int {
	for f[i] != i {
		i = f[i]
	}
	return i
}

// link hangs i under parent, unless i would become its own ancestor.
func (f forest) link(i, parent int) {
	if f.root(parent) != f.root(i) {
		f[f.root(i)] = f.root(parent)
	}
}

// linkByHeaders uses the last known reference, or else In-Reply-To.
func (f forest) linkByHeaders(msgs []Message) {
	index := map[string]int{}
	for i, m := range msgs {
		if m.ID != "" {
			index[m.ID] = i
		}
	}
	for i, m := range msgs {
		for _, id := range slices.Backward(slices.Concat([]string{m.InReplyTo}, m.References)) {
			if parent, ok := index[id]; ok && id != m.ID {
				f.link(i, parent)
				break
			}
		}
	}
}

// linkBySubject lets a reply with nothing to go on join the first thread on its subject.
func (f forest) linkBySubject(msgs []Message) {
	first := map[string]int{}
	for i, m := range msgs {
		if f.root(i) != i && !isReply(m.Subject) {
			continue
		}
		key := strings.ToLower(NormalizeSubject(m.Subject))
		parent, known := first[key]
		switch {
		case !known:
			first[key] = f.root(i)
		case f.root(i) == i && isReply(m.Subject):
			f.link(i, parent)
		}
	}
}

// newThread makes a thread of messages in date order.
func newThread(msgs []Message) Thread {
	sort.SliceStable(msgs, func(a, b int) bool { return msgs[a].Date.Before(msgs[b].Date) })
	first := msgs[0].Subject
	return Thread{Subject: NormalizeSubject(first), Messages: msgs, Pinned: isAnnouncement(first)}
}

// BuildThreads groups messages into threads, in the order a digest lists them.
func BuildThreads(msgs []Message) []Thread {
	msgs = oncePerID(msgs)
	f := make(forest, len(msgs))
	for i := range f {
		f[i] = i
	}
	f.linkByHeaders(msgs)
	f.linkBySubject(msgs)
	groups := map[int][]Message{}
	for i, m := range msgs {
		groups[f.root(i)] = append(groups[f.root(i)], m)
	}
	out := make([]Thread, 0, len(groups))
	for _, group := range groups {
		out = append(out, newThread(group))
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Pinned != out[b].Pinned {
			return out[a].Pinned
		}
		if !out[a].Latest().Equal(out[b].Latest()) {
			return out[a].Latest().After(out[b].Latest())
		}
		return out[a].Subject < out[b].Subject
	})
	return out
}

// FindByID returns the first message with that ID.
func FindByID(msgs []Message, id string) (Message, bool) {
	for _, m := range msgs {
		if m.ID == id {
			return m, true
		}
	}
	return Message{}, false
}
`,
			"digest.go": `package mailthread

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	digestWidth = 72
	digestLines = 20
)

var emailAddress = regexp.MustCompile("[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\\.[A-Za-z0-9-]+)*\\.[A-Za-z]{2,}")

// plural writes a count of messages.
func plural(n int) string {
	if n == 1 {
		return "1 message"
	}
	return fmt.Sprintf("%d messages", n)
}

// wrapLine breaks one long line at spaces.
func wrapLine(line string, width int) []string {
	var out []string
	cur := ""
	for _, word := range strings.Fields(line) {
		switch {
		case cur == "":
			cur = word
		case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(word) <= width:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	return append(out, cur)
}

// Wrap breaks lines longer than width at spaces. Indented code is left alone.
func Wrap(text string, width int) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "    ") || utf8.RuneCountInString(line) <= width {
			out = append(out, line)
		} else {
			out = append(out, wrapLine(line, width)...)
		}
	}
	return strings.Join(out, "\n")
}

// withoutQuotes drops quoted lines, and the line introducing them.
func withoutQuotes(lines []string) []string {
	var out []string
	for i, line := range lines {
		quoted := strings.HasPrefix(line, ">")
		intro := strings.HasSuffix(line, "wrote:") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], ">")
		if !quoted && !intro {
			out = append(out, line)
		}
	}
	return out
}

// CleanBody returns a message's text as a digest shows it, before wrapping.
func CleanBody(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if line == "-- " {
			lines = lines[:i]
			break
		}
	}
	var kept []string
	for _, line := range withoutQuotes(lines) {
		if line != "" || (len(kept) > 0 && kept[len(kept)-1] != "") {
			kept = append(kept, line)
		}
	}
	text := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	return emailAddress.ReplaceAllString(text, "[address removed]")
}

// renderBody prepares a body for a thread: cleaned, wrapped and not too long.
func renderBody(body string) string {
	lines := strings.Split(Wrap(CleanBody(body), digestWidth), "\n")
	if len(lines) > digestLines {
		lines = append(lines[:digestLines:digestLines], "[...]")
	}
	return strings.Join(lines, "\n")
}

// RenderThread writes one thread.
func RenderThread(t Thread) string {
	var b strings.Builder
	fmt.Fprintf(&b, "== %s (%s)\n", t.Subject, plural(len(t.Messages)))
	for _, m := range t.Messages {
		when := m.Date.UTC().Format("2006-01-02 15:04")
		fmt.Fprintf(&b, "\n%s, %s\n%s\n", m.From.Display(), when, renderBody(m.Body))
	}
	return b.String()
}

// RenderDigest writes a whole digest.
func RenderDigest(date time.Time, threads []Thread) string {
	parts := make([]string, 0, len(threads))
	for _, t := range threads {
		parts = append(parts, RenderThread(t))
	}
	head := fmt.Sprintf("Digest of %s: %d threads, %d messages\n\n", date.Format("2006-01-02"), len(threads), TotalMessages(threads))
	return head + strings.Join(parts, "\n")
}

// BuildDigest turns a whole mbox file into a digest. The errors are those from reading it.
func BuildDigest(text string, date time.Time) (string, []error) {
	msgs, errs := ParseMbox(text)
	return RenderDigest(date, BuildThreads(Filter(msgs))), errs
}

// IndexLine describes a thread on one line for the archive's index.
func IndexLine(t Thread) string {
	return fmt.Sprintf("%s %s (%s)", t.Latest().UTC().Format("2006-01-02"), t.Subject, plural(len(t.Messages)))
}
`,
			"policy.go": `package mailthread

import "strings"

// Excluded reports whether list policy leaves a message out of the digest.
func Excluded(m Message) bool {
	auto := strings.ToLower(m.Header["auto-submitted"])
	if auto != "" && auto != "no" {
		return true
	}
	switch strings.ToLower(m.Header["precedence"]) {
	case "bulk", "junk", "auto_reply":
		return true
	}
	return strings.EqualFold(m.Header["x-spam-flag"], "yes") || strings.EqualFold(m.Header["x-moderation"], "rejected")
}

// Filter returns the messages that are not left out, in order.
func Filter(msgs []Message) []Message {
	var out []Message
	for _, m := range msgs {
		if !Excluded(m) {
			out = append(out, m)
		}
	}
	return out
}

// isAnnouncement reports whether a raw subject starts with the announcement tag.
func isAnnouncement(s string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "[announce]")
}
`,
			"stats.go": `package mailthread

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// TotalMessages returns how many messages some threads have between them.
func TotalMessages(threads []Thread) int {
	total := 0
	for _, t := range threads {
		total += len(t.Messages)
	}
	return total
}

// Participants returns who took part in a thread, as people see them, once per address.
func Participants(t Thread) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range t.Messages {
		if !seen[m.From.Email] {
			seen[m.From.Email] = true
			out = append(out, m.From.Display())
		}
	}
	slices.Sort(out)
	return out
}

// TopSenders lists the n who sent the most, as "name: count", ties by name.
func TopSenders(threads []Thread, n int) []string {
	counts := map[string]int{}
	for _, m := range Messages(threads) {
		counts[m.From.Display()]++
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(a, b int) bool {
		if counts[names[a]] != counts[names[b]] {
			return counts[names[a]] > counts[names[b]]
		}
		return names[a] < names[b]
	})
	out := []string{}
	for _, name := range names[:min(n, len(names))] {
		out = append(out, fmt.Sprintf("%s: %d", name, counts[name]))
	}
	return out
}

// Related returns the threads about the same thing as a raw subject, in order.
func Related(threads []Thread, subject string) []Thread {
	var out []Thread
	for _, t := range threads {
		if strings.EqualFold(t.Subject, NormalizeSubject(subject)) {
			out = append(out, t)
		}
	}
	return out
}

// ThreadsSince returns the threads with a message at or after a time, in order.
func ThreadsSince(threads []Thread, since time.Time) []Thread {
	var out []Thread
	for _, t := range threads {
		if !t.Latest().Before(since) {
			out = append(out, t)
		}
	}
	return out
}

// WeeklyCounts counts messages per ISO week in UTC, oldest week first.
func WeeklyCounts(threads []Thread) []string {
	counts := map[string]int{}
	for _, m := range Messages(threads) {
		if !m.Date.IsZero() {
			year, week := m.Date.UTC().ISOWeek()
			counts[fmt.Sprintf("%d-W%02d", year, week)]++
		}
	}
	weeks := make([]string, 0, len(counts))
	for week := range counts {
		weeks = append(weeks, week)
	}
	slices.Sort(weeks)
	out := make([]string, 0, len(weeks))
	for _, week := range weeks {
		out = append(out, fmt.Sprintf("%s: %d", week, counts[week]))
	}
	return out
}

// WithoutSender returns the threads without one sender's messages. A thread with
// nothing left goes, and the threads passed in stay as they were.
func WithoutSender(threads []Thread, email string) []Thread {
	out := []Thread{}
	for _, t := range threads {
		var kept []Message
		for _, m := range t.Messages {
			if !strings.EqualFold(m.From.Email, email) {
				kept = append(kept, m)
			}
		}
		if len(kept) > 0 {
			t.Messages = kept
			out = append(out, t)
		}
	}
	return out
}
`,
		},
		Turns: script(
			cloud("Read docs/list-policy.txt, which says what goes in the digest, and tell me what it asks for. No code changes."),
			cloud("Harden `ParseHeaders` in message.go. Treat CRLF line endings as plain newlines, in the body too. Header names come back in lower case. A line starting with a space or tab continues the one before it, joined with one space. If a name appears twice the first value wins. Add `type HeaderError struct { Line int; Text string }`, used as a pointer, with a 1-based Line and matching ErrMalformed under errors.Is, for a line without a colon or a continuation with nothing to continue."),
			cloud("Message IDs are opaque. We strip the angle brackets, and everything else stays exactly as it came, so two IDs that differ only in case are different IDs, and nothing here ever lower-cases or otherwise tidies one. Add `func ParseIDs(s string) []string` to message.go: all the IDs in a header value, in order, without brackets. Anything outside brackets is ignored."),
			local("Add `func ParseAddress(s string) (Address, error)` to fields.go, for a From header such as Alice <alice@x.org>, a quoted name, or a bare address. Email is always kept in lower case, and a failure matches ErrMalformed."),
			local("Add `func NormalizeSubject(s string) string` to fields.go: what a subject is about, for matching. Take off Re:, Aw:, Fwd: and Fw: in any case, and list tags like [dev] at the start, repeatedly and in any mix, along with spaces around them, and trim what's left. Only at the start: anything later in the subject stays."),
			local("Our digests end up in a public archive and spammers harvest those, so no email address may appear in anything a digest prints, the bodies of messages included: any address in a body is replaced by [address removed]. A sender is shown by their name, or without one by what comes before the @. No body code yet. Add `func (a Address) Display() string` to fields.go for how a sender is shown."),
			cloud("Dates in the wild are often garbage. A message with no usable Date isn't dropped: it takes the date of the message before it in the file, or stays at the zero time if there's none. Nothing to do for that until we read whole files. Add `func ParseDate(s string) (time.Time, bool)` to message.go, for a Date header value, giving the time in UTC."),
			cloud("Add `func ParseMessage(raw string) (Message, error)` to message.go. ID, InReplyTo and References come from their headers through ParseIDs, the first ID where there's one field. Subject has any encoded words decoded. Header and Body are as ParseHeaders gives them. A From or Date it can't read stays empty and isn't an error, and only a bad header block is."),
			cloud("Bodies can be encoded. Add `func DecodeBody(body, encoding string) (string, error)` to message.go, where encoding is a Content-Transfer-Encoding value in upper or lower case: quoted-printable and base64 are decoded, and anything else, or nothing, leaves the body as it is. A body that can't be decoded gives an error matching ErrMalformed. `ParseMessage` should decode its Body this way, and keep the raw body if that fails, which isn't an error for the message."),
			local("One more fact about our archives: lists deliver the same mail twice, so a Message-ID can repeat in a file. Whenever we group messages, keep the first in file order and ignore later copies. A message with no ID at all is never such a copy. Nothing to do for that yet. Add `func SplitMbox(text string) []string` to mbox.go: the raw messages. One starts at a line beginning with From followed by a space, at the start of the file or after a blank line, and that line isn't part of it. In a message, a line starting >From followed by a space loses the >."),
			local("Add `func Excluded(m Message) bool` to mbox.go: whether a message stays out of the digest under list policy."),
			local("Read docs/digest-format.txt, the layout of the digest, and tell me the main points. No code changes."),
			local("Add `func ParseMbox(text string) ([]Message, []error)` to mbox.go: the messages of a file in order, with one error for each message that can't be read, going on past it."),
			cloud("Add `func Wrap(text string, width int) string` to digest.go. Each line of text longer than width, counted in characters, is broken at spaces and filled word by word. A word longer than width gets its own line. Lines starting with four spaces are code and stay as they are, and so do lines within the width."),
			local("We've decided a forwarded mail starts a new conversation, so from now on a subject keeps its Fwd: or Fw:, and those no longer count as marks to take off. Everything else about it stays. Update the code."),
			local("Add `func Filter(msgs []Message) []Message` to mbox.go: the messages that aren't left out, in order."),
			local("Add `func FindByID(msgs []Message, id string) (Message, bool)` to mbox.go: the first message with that ID."),
			cloud("Add `func BuildThreads(msgs []Message) []Thread` to thread.go. A message hangs under the last of its References that's in the file, or otherwise its InReplyTo. Messages hanging together make a thread, with its Messages in date order, ties in file order, and Subject the normalized subject of its first. A message can't become its own ancestor: skip a link that would make a cycle. Order the threads however you like for now."),
			cloud("In `BuildThreads`, a message that hangs under nothing and whose subject carries a reply mark joins the first thread with the same normalized subject, ignoring case. A message with no reply mark starts its own thread even if the subject matches."),
			cloud("Finish `BuildThreads`: threads come in the order a digest needs. Newest activity first, by the date of a thread's latest message, ties by Subject. Set Pinned, and treat those threads the way list policy says."),
			local("Tidy up. mbox.go should only be about the file format and reading it. Make a new file policy.go and move `Excluded` and `Filter` into it, along with whatever decides that a thread is pinned. `FindByID` moves to thread.go. Behaviour doesn't change."),
			cloud("Add `func CleanBody(body string) string` to digest.go: a message's text as the digest shows it, as far as everything before wrapping."),
			local("Add `func (t Thread) Latest() time.Time` to thread.go: the date of its newest message."),
			local("Add to stats.go `func Participants(t Thread) []string`: who took part in a thread, as we show them, once per Email, sorted. And `func TopSenders(threads []Thread, n int) []string`: the n who sent the most, as we show them, each like A: 2, most first, ties by name. Fewer if there aren't n."),
			local("Add `func WeeklyCounts(threads []Thread) []string` to stats.go: how many messages each ISO week has, in UTC, oldest week first, each like 2024-W03: 5. A message still at the zero time isn't counted."),
			cloud("Add `func RenderThread(t Thread) string` to digest.go: one thread in the digest's layout, with its bodies prepared as the layout says."),
			cloud("Add `func RenderDigest(date time.Time, threads []Thread) string` to digest.go: a whole digest of those threads, as given, with date as its date."),
			local("Add `func IndexLine(t Thread) string` to digest.go, a line for the archive's index, exactly like: 2024-01-21 Hello (2 messages), where the date is its Latest as shown in a digest, and a single message says (1 message)."),
			local("Add `func WithoutSender(threads []Thread, email string) []Thread` to stats.go, for people who mute someone: the threads without that sender's messages. The email may come in upper or lower case. A thread with nothing left goes, the order stays, and the threads passed in aren't changed."),
			cloud("Add `func Related(threads []Thread, subject string) []Thread` to stats.go: the threads about the same thing as a raw subject, ignoring case, in order. A thread's Subject is already normalized."),
			cloud("Add `func BuildDigest(text string, date time.Time) (string, []error)` to digest.go: the digest of a whole mbox file, from reading it to the finished text, with everything we've settled applied along the way. The errors are those from reading it."),
			local("Add `func ThreadsSince(threads []Thread, since time.Time) []Thread` to stats.go: the threads with a message at or after since, in order."),
		),
		Carries: []Carry{
			{
				What:        "what list policy leaves out, and that announcements are pinned (docs/list-policy.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{10, 15, 19, 30},
				Source:      "docs/list-policy.txt",
				Tests:       []string{"TestExcludedAutomatic", "TestExcludedKeepsOrdinary", "TestFilter", "TestThreadsPinAnnouncements", "TestBuildDigest"},
				Absent:      []string{"list-policy", "auto-submitted", "precedence", "x-spam", "x-moderation", "[announce]"},
			},
			{
				What:        "message IDs stay exactly as they came, so case matters",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{16, 17, 30},
				Tests:       []string{"TestParseIDsKeepsThemAsTheyAre", "TestParseMessageFields", "TestFindByIDExact", "TestThreadIDsKeepCase", "TestBuildDigest"},
				Absent:      []string{"opaque", "angle bracket", "lower-cases", "differ only in case"},
			},
			{
				What:        "subjects lose Re: and Aw: in any case, and list tags, in any mix",
				Kind:        CarryEmbodied,
				Established: 4,
				Needed:      []int{14, 18, 29},
				Tests:       []string{"TestSubjectStripsReplyMarks", "TestSubjectStripsListTags", "TestThreadsJoinBySubject", "TestRelated", "TestThreadsPinAnnouncements", "TestBuildDigest"},
				Absent:      []string{"aw:", "[dev]", "in any case", "any mix"},
			},
			{
				What:        "no email address in anything a digest prints, including bodies",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{21, 25, 30},
				Tests:       []string{"TestCleanBodyHidesAddresses", "TestRenderThreadCleansBody", "TestRenderThreadSeveral", "TestRenderThreadLayout", "TestBuildDigest"},
				Absent:      []string{"public archive", "harvest", "address removed", "no email address"},
			},
			{
				What:        "a message with no usable Date takes the previous message's date",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{12, 30},
				Tests:       []string{"TestParseMboxInheritsDates", "TestBuildDigest"},
				Absent:      []string{"garbage", "no usable date", "takes the date", "message before it"},
			},
			{
				What:        "a repeated Message-ID keeps only its first copy, and a message without an ID is never a copy",
				Kind:        CarryUser,
				Established: 9,
				Needed:      []int{17, 30},
				Tests:       []string{"TestThreadsDropRepeatedID", "TestThreadsKeepAllWithoutID", "TestBuildDigest"},
				Absent:      []string{"deliver the same", "later copies", "first in file order", "no id at all"},
			},
			{
				What:        "the digest's bodies, layout and time zone (docs/digest-format.txt)",
				Kind:        CarryDiscovered,
				Established: 11,
				Needed:      []int{21, 25, 26, 27},
				Source:      "docs/digest-format.txt",
				Tests: []string{
					"TestCleanBodyFollowsFormat", "TestCleanBodyKeepsOtherWrote", "TestRenderThreadLayout", "TestRenderThreadSeveral",
					"TestRenderThreadCleansBody", "TestRenderThreadWrapsBody", "TestRenderThreadLongBody", "TestRenderDigest",
					"TestIndexLine", "TestBuildDigest",
				},
				Absent: []string{"digest-format", "72", "20 lines", "signature", "wrote:", "[...]"},
			},
			{
				What:        "a subject keeps its Fwd: or Fw:, revised from taking them off",
				Kind:        CarryUser,
				Established: 14,
				Needed:      []int{18, 29},
				Tests:       []string{"TestSubjectKeepsForwardMarks", "TestThreadsForwardStartsNew", "TestRelated", "TestBuildDigest"},
				Absent:      []string{"forward", "fwd", "fw:"},
			},
		},
	}
}
