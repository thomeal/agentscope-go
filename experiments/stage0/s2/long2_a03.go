package s2

func long2A03() Task {
	return Task{
		ID:     "s2-long2-a03-podcast-player",
		Set:    "long",
		System: system("a podcast player"),
		Files: map[string]string{
			"go.mod": goMod,
			"library.go": `package podcast

import (
	"errors"
	"sort"
)

// Day is a day number, counted from the day the app was installed.
type Day int

// Episode is one episode of a feed. Length and Position are in seconds, and
// Size is the size of its download in MB.
type Episode struct {
	ID        string
	Feed      string
	Title     string
	Published Day
	Length    int
	Position  int
	Size      int
}

// Feed is one subscription.
type Feed struct {
	Name string
	URL  string
}

// ErrUnknownFeed is returned for a feed name we are not subscribed to.
var ErrUnknownFeed = errors.New("unknown feed")

// ErrUnknownEpisode is returned for an episode ID we do not have.
var ErrUnknownEpisode = errors.New("unknown episode")

// ErrDuplicate is returned when a feed or episode is already there.
var ErrDuplicate = errors.New("already there")

// Library is the user's subscriptions, their episodes and the play queue.
type Library struct {
	feeds    []Feed
	episodes []Episode
	queue    []string
}

// feed returns the subscription with that name, or nil.
func (l *Library) feed(name string) *Feed {
	for i := range l.feeds {
		if l.feeds[i].Name == name {
			return &l.feeds[i]
		}
	}
	return nil
}

// find returns the index of the episode with that ID, or -1.
func (l *Library) find(id string) int {
	for i, e := range l.episodes {
		if e.ID == id {
			return i
		}
	}
	return -1
}

// Subscribe adds a feed.
func (l *Library) Subscribe(f Feed) error {
	if l.feed(f.Name) != nil {
		return ErrDuplicate
	}
	l.feeds = append(l.feeds, f)
	return nil
}

// Feeds returns the subscriptions sorted by name.
func (l *Library) Feeds() []Feed {
	out := append([]Feed(nil), l.feeds...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AddEpisode adds an episode of a feed we are subscribed to.
func (l *Library) AddEpisode(e Episode) error {
	if l.feed(e.Feed) == nil {
		return ErrUnknownFeed
	}
	if l.find(e.ID) >= 0 {
		return ErrDuplicate
	}
	l.episodes = append(l.episodes, e)
	return nil
}

// Episode returns the episode with that ID.
func (l *Library) Episode(id string) (Episode, error) {
	i := l.find(id)
	if i < 0 {
		return Episode{}, ErrUnknownEpisode
	}
	return l.episodes[i], nil
}
`,
			"queue.go": `package podcast

// Enqueue adds an episode to the end of the queue.
func (l *Library) Enqueue(id string) error {
	l.queue = append(l.queue, id)
	return nil
}

// Queue returns the IDs in the queue, front first.
func (l *Library) Queue() []string {
	return append([]string(nil), l.queue...)
}
`,
			"library_test.go": `package podcast

import "testing"

func TestSubscribeAndAdd(t *testing.T) {
	var l Library
	if err := l.Subscribe(Feed{Name: "Tech Talk", URL: "https://t.example/rss"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Subscribe(Feed{Name: "Tech Talk"}); err != ErrDuplicate {
		t.Fatalf("second Subscribe = %v", err)
	}
	if err := l.AddEpisode(Episode{ID: "t1", Feed: "Tech Talk", Title: "Chips", Length: 1800}); err != nil {
		t.Fatal(err)
	}
	if err := l.AddEpisode(Episode{ID: "x1", Feed: "Nope"}); err != ErrUnknownFeed {
		t.Fatalf("AddEpisode to an unknown feed = %v", err)
	}
	e, err := l.Episode("t1")
	if err != nil || e.Title != "Chips" {
		t.Fatalf("Episode = %+v, %v", e, err)
	}
}

func TestEnqueueKeepsOrder(t *testing.T) {
	var l Library
	l.Subscribe(Feed{Name: "Tech Talk"})
	l.AddEpisode(Episode{ID: "t1", Feed: "Tech Talk", Length: 600})
	l.AddEpisode(Episode{ID: "t2", Feed: "Tech Talk", Length: 600})
	l.Enqueue("t2")
	l.Enqueue("t1")
	if q := l.Queue(); len(q) != 2 || q[0] != "t2" || q[1] != "t1" {
		t.Fatalf("Queue = %v", q)
	}
}
`,
			"docs/cleanup.md": `# Download cleanup

Downloads take space on the phone, so the app clears some of them out.
Cleanup runs for a given day with a size limit in MB, and goes through the
downloaded episodes in two passes. Each pass takes them from the one
published longest ago to the newest; among episodes published on the same
day it takes the larger download first, then the lower ID.

## Pass 1: finished episodes

A downloaded episode that has been played, and that was published 7 days or
more before the cleanup day, loses its download.

## Pass 2: the size limit

If the downloads left add up to more MB than the limit, the app goes on
removing downloads, in the same order, until they fit.

## Never removed

An episode that is in the queue keeps its download in both passes, even if
the downloads then stay over the limit.

Cleanup reports the IDs of the episodes whose downloads it removed, in the
order it removed them.

## Example

Cleanup on day 20 with a limit of 300 MB finds these downloads:

- A: played, published on day 10, 100 MB
- B: played, published on day 15, 100 MB
- C: not played, published on day 2, 250 MB
- D: not played, published on day 3, 200 MB, in the queue

Pass 1 removes A; B is too recent. B, C and D come to 550 MB, so pass 2
removes C, which leaves 300 MB. D would have been next, but it is queued.
Cleanup reports A, then C.
`,
			"docs/feeds.md": `# Subscription lists

People move between phones, so the app can write its subscriptions to a
plain text list and read such a list back.

The first line of a list is exactly:

    #podcasts 1

Every other line describes one feed, with its fields separated by a single
tab character:

    name, url, speed

- name is the feed's name as the app shows it.
- url is where the feed is fetched from.
- speed is the feed's playback speed in percent: a whole number from 50 to
  300 that is a multiple of 10. A line may leave the speed out, together
  with its tab, and then the speed is 100.

## Reading

Blank lines are skipped. A line for a feed that is already subscribed
updates its url and speed. A list without the first line, or with a line
that does not have two or three fields or has a bad speed, is refused as a
whole and changes nothing.

## Writing

The app writes one line for every subscription, in alphabetical order of
name ignoring upper and lower case, and always writes the speed, even when
it is 100. Every line ends with a newline.

## Example

With TAB standing for the tab character, a phone subscribed to "news
daily" at normal speed and to "Morning Run" at 1.5 times normal speed
writes:

    #podcasts 1
    Morning Run TAB https://run.example/rss TAB 150
    news daily TAB https://news.example/feed TAB 100

without the spaces around each TAB.
`,
		},
		Hidden: map[string]string{hiddenName: `package podcast_test

import (
	"errors"
	"slices"
	"strconv"
	"testing"

	podcast "fixture"
)

// show returns a library subscribed to Tech Talk, with episodes t1 to tn
// published on days 1 to n, each 600 seconds long and 100 MB.
func show(t *testing.T, n int) *podcast.Library {
	t.Helper()
	l := &podcast.Library{}
	must(t, l.Subscribe(podcast.Feed{Name: "Tech Talk", URL: "https://t.example/rss"}))
	for i := 1; i <= n; i++ {
		e := podcast.Episode{ID: "t" + strconv.Itoa(i), Feed: "Tech Talk", Title: "Part " + strconv.Itoa(i), Published: podcast.Day(i), Length: 600, Size: 100}
		must(t, l.AddEpisode(e))
	}
	return l
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func queued(t *testing.T, l *podcast.Library, ids ...string) {
	t.Helper()
	for _, id := range ids {
		must(t, l.Enqueue(id))
	}
}

func downloaded(t *testing.T, l *podcast.Library, ids ...string) {
	t.Helper()
	for _, id := range ids {
		must(t, l.Download(id))
	}
}

func ep(t *testing.T, l *podcast.Library, id string) podcast.Episode {
	t.Helper()
	e, err := l.Episode(id)
	must(t, err)
	return e
}

func TestEnqueueNoDuplicates(t *testing.T) {
	l := show(t, 2)
	queued(t, l, "t1", "t2", "t1")
	if q := l.Queue(); !slices.Equal(q, []string{"t1", "t2"}) {
		t.Errorf("Queue = %v", q)
	}
	if err := l.Enqueue("zz"); !errors.Is(err, podcast.ErrUnknownEpisode) {
		t.Errorf("Enqueue(zz) = %v", err)
	}
}

func TestPosition(t *testing.T) {
	l := show(t, 3)
	queued(t, l, "t1", "t2")
	if p := l.Position("t2"); p != 2 {
		t.Errorf("Position(t2) = %d", p)
	}
	if p := l.Position("t3"); p != 0 {
		t.Errorf("Position(t3) = %d", p)
	}
}

func TestPlayNext(t *testing.T) {
	l := show(t, 4)
	queued(t, l, "t1", "t2", "t3")
	must(t, l.PlayNext("t3"))
	must(t, l.PlayNext("t4"))
	if q := l.Queue(); !slices.Equal(q, []string{"t4", "t3", "t1", "t2"}) {
		t.Errorf("Queue = %v", q)
	}
}

func TestMoveOneBased(t *testing.T) {
	l := show(t, 4)
	queued(t, l, "t1", "t2", "t3", "t4")
	must(t, l.Move(1, 3))
	if q := l.Queue(); !slices.Equal(q, []string{"t2", "t3", "t1", "t4"}) {
		t.Fatalf("after Move(1, 3): %v", q)
	}
	must(t, l.Move(4, 1))
	if q := l.Queue(); !slices.Equal(q, []string{"t4", "t2", "t3", "t1"}) {
		t.Fatalf("after Move(4, 1): %v", q)
	}
	if err := l.Move(0, 2); !errors.Is(err, podcast.ErrBadPosition) {
		t.Errorf("Move(0, 2) = %v", err)
	}
}

func TestAtOneBased(t *testing.T) {
	l := show(t, 3)
	queued(t, l, "t3", "t1", "t2")
	if e, err := l.At(1); err != nil || e.ID != "t3" {
		t.Errorf("At(1) = %v, %v", e.ID, err)
	}
	if e, err := l.At(3); err != nil || e.ID != "t2" {
		t.Errorf("At(3) = %v, %v", e.ID, err)
	}
	if _, err := l.At(0); !errors.Is(err, podcast.ErrBadPosition) {
		t.Errorf("At(0) = %v", err)
	}
}

func TestEpisodesNewestFirst(t *testing.T) {
	l := show(t, 2)
	must(t, l.AddEpisode(podcast.Episode{ID: "t0", Feed: "Tech Talk", Published: 2, Length: 600}))
	must(t, l.Subscribe(podcast.Feed{Name: "Zen"}))
	must(t, l.AddEpisode(podcast.Episode{ID: "z1", Feed: "Zen", Published: 9, Length: 600}))
	var ids []string
	for _, e := range l.Episodes("Tech Talk") {
		ids = append(ids, e.ID)
	}
	if !slices.Equal(ids, []string{"t0", "t2", "t1"}) {
		t.Fatalf("Episodes = %v", ids)
	}
}

func TestDequeue(t *testing.T) {
	l := show(t, 2)
	queued(t, l, "t1", "t2")
	must(t, l.Dequeue("t1"))
	if err := l.Dequeue("t1"); !errors.Is(err, podcast.ErrNotQueued) {
		t.Errorf("second Dequeue = %v", err)
	}
	if q := l.Queue(); !slices.Equal(q, []string{"t2"}) {
		t.Errorf("Queue = %v", q)
	}
}

func TestSetPositionAndPlayed(t *testing.T) {
	l := show(t, 1)
	must(t, l.SetPosition("t1", -5))
	if e := ep(t, l, "t1"); e.Position != 0 {
		t.Errorf("Position = %d", e.Position)
	}
	must(t, l.SetPosition("t1", 700))
	if e := ep(t, l, "t1"); e.Position != 600 {
		t.Errorf("Position = %d", e.Position)
	}
	must(t, l.SetPosition("t1", 569))
	if ep(t, l, "t1").Played() {
		t.Error("played with 31 seconds to go")
	}
	must(t, l.SetPosition("t1", 570))
	if !ep(t, l, "t1").Played() {
		t.Error("not played with 30 seconds to go")
	}
	if err := l.SetPosition("zz", 1); !errors.Is(err, podcast.ErrUnknownEpisode) {
		t.Errorf("SetPosition(zz) = %v", err)
	}
}

func TestSetSpeed(t *testing.T) {
	l := show(t, 0)
	must(t, l.SetSpeed("Tech Talk", 150))
	for _, p := range []int{155, 40, 310} {
		if err := l.SetSpeed("Tech Talk", p); !errors.Is(err, podcast.ErrBadSpeed) {
			t.Errorf("SetSpeed(%d) = %v", p, err)
		}
	}
	if err := l.SetSpeed("Zen", 100); !errors.Is(err, podcast.ErrUnknownFeed) {
		t.Errorf("SetSpeed(Zen) = %v", err)
	}
	if f := l.Feeds(); f[0].Speed != 150 {
		t.Errorf("Speed = %d", f[0].Speed)
	}
}

func TestNextPopsFront(t *testing.T) {
	l := show(t, 2)
	queued(t, l, "t2", "t1")
	for _, want := range []string{"t2", "t1"} {
		if e, err := l.Next(); err != nil || e.ID != want {
			t.Fatalf("Next = %v, %v; want %s", e.ID, err, want)
		}
	}
	if _, err := l.Next(); !errors.Is(err, podcast.ErrEmptyQueue) {
		t.Errorf("Next on an empty queue = %v", err)
	}
}

func TestMarkPlayed(t *testing.T) {
	l := show(t, 1)
	must(t, l.MarkPlayed("t1"))
	if e := ep(t, l, "t1"); e.Position != 600 || !e.Played() {
		t.Fatalf("after MarkPlayed: %+v", e)
	}
}

func TestContinueStarted(t *testing.T) {
	l := show(t, 3)
	must(t, l.SetPosition("t2", 100))
	must(t, l.SetPosition("t3", 200))
	for _, want := range []string{"t3", "t2", "t1"} {
		e, err := l.Continue("Tech Talk")
		if err != nil || e.ID != want {
			t.Fatalf("Continue = %v, %v; want %s", e.ID, err, want)
		}
		must(t, l.MarkPlayed(want))
	}
	if _, err := l.Continue("Tech Talk"); !errors.Is(err, podcast.ErrNothingToPlay) {
		t.Errorf("Continue with everything played = %v", err)
	}
}

func TestContinueSkipsNearlyFinished(t *testing.T) {
	l := show(t, 3)
	must(t, l.SetPosition("t2", 590))
	if e, err := l.Continue("Tech Talk"); err != nil || e.ID != "t1" {
		t.Fatalf("Continue = %v, %v", e.ID, err)
	}
}

func TestClock(t *testing.T) {
	cases := []struct {
		s    podcast.Seconds
		want string
	}{{245, "4:05"}, {3723, "1:02:03"}, {59, "0:59"}, {3600, "1:00:00"}}
	for _, c := range cases {
		if got := podcast.Clock(c.s); got != c.want {
			t.Errorf("Clock(%d) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestQueueSummary(t *testing.T) {
	l := show(t, 2)
	must(t, l.AddEpisode(podcast.Episode{ID: "t9", Feed: "Tech Talk", Length: 630}))
	must(t, l.SetPosition("t1", 300))
	queued(t, l, "t1", "t9")
	if got := l.QueueSummary(); got != "2 episodes, 15:30 left" {
		t.Fatalf("QueueSummary = %q", got)
	}
}

func TestCleanTitle(t *testing.T) {
	cases := []struct{ title, want string }{
		{"Tech Talk 42: Chips", "Chips"},
		{"tech talk - Interview", "Interview"},
		{"TECH TALK #7 Robots", "Robots"},
		{"Chips and more", "Chips and more"},
		{"Tech Talk 42", "Tech Talk 42"},
	}
	for _, c := range cases {
		if got := podcast.CleanTitle("Tech Talk", c.title); got != c.want {
			t.Errorf("CleanTitle(%q) = %q, want %q", c.title, got, c.want)
		}
	}
	l := show(t, 0)
	must(t, l.AddEpisode(podcast.Episode{ID: "t9", Feed: "Tech Talk", Title: "Tech Talk 9: Memory", Length: 600}))
	if e := ep(t, l, "t9"); e.Title != "Memory" {
		t.Errorf("stored title %q", e.Title)
	}
}

func TestArriveAutoQueue(t *testing.T) {
	l := show(t, 1)
	must(t, l.Subscribe(podcast.Feed{Name: "Zen", AutoQueue: true}))
	must(t, l.Arrive(podcast.Episode{ID: "z1", Feed: "Zen", Title: "Zen 1: Breath", Length: 600}))
	must(t, l.Arrive(podcast.Episode{ID: "t2", Feed: "Tech Talk", Length: 600}))
	if q := l.Queue(); !slices.Equal(q, []string{"z1"}) {
		t.Errorf("Queue = %v", q)
	}
	if e := ep(t, l, "z1"); e.Title != "Breath" {
		t.Errorf("title %q", e.Title)
	}
	if err := l.Arrive(podcast.Episode{ID: "z1", Feed: "Zen"}); !errors.Is(err, podcast.ErrDuplicate) {
		t.Errorf("Arrive twice = %v", err)
	}
}

func TestArriveSkipsExplicit(t *testing.T) {
	l := show(t, 0)
	must(t, l.Subscribe(podcast.Feed{Name: "Zen", AutoQueue: true}))
	must(t, l.Arrive(podcast.Episode{ID: "z1", Feed: "Zen", Length: 600, Explicit: true}))
	must(t, l.Arrive(podcast.Episode{ID: "z2", Feed: "Zen", Length: 600}))
	if q := l.Queue(); !slices.Equal(q, []string{"z2"}) {
		t.Fatalf("Queue = %v", q)
	}
}

func TestTimeLeftAtSpeed(t *testing.T) {
	l := &podcast.Library{}
	must(t, l.Subscribe(podcast.Feed{Name: "Fast", Speed: 150}))
	must(t, l.Subscribe(podcast.Feed{Name: "Odd", Speed: 130}))
	must(t, l.AddEpisode(podcast.Episode{ID: "f1", Feed: "Fast", Length: 1000}))
	must(t, l.AddEpisode(podcast.Episode{ID: "o1", Feed: "Odd", Length: 100}))
	must(t, l.SetPosition("f1", 100))
	queued(t, l, "f1", "o1")
	if got := l.TimeLeft(); got != 677 {
		t.Fatalf("TimeLeft = %d, want 677", got)
	}
}

func TestUnsubscribe(t *testing.T) {
	l := show(t, 2)
	must(t, l.Subscribe(podcast.Feed{Name: "Zen"}))
	must(t, l.AddEpisode(podcast.Episode{ID: "z1", Feed: "Zen", Length: 600}))
	queued(t, l, "t1", "z1", "t2")
	must(t, l.Unsubscribe("Tech Talk"))
	if q := l.Queue(); !slices.Equal(q, []string{"z1"}) {
		t.Errorf("Queue = %v", q)
	}
	if _, err := l.Episode("t1"); !errors.Is(err, podcast.ErrUnknownEpisode) {
		t.Errorf("Episode(t1) = %v", err)
	}
	if err := l.Unsubscribe("Tech Talk"); !errors.Is(err, podcast.ErrUnknownFeed) {
		t.Errorf("second Unsubscribe = %v", err)
	}
}

func TestPlayedLeavesQueue(t *testing.T) {
	l := show(t, 3)
	queued(t, l, "t1", "t2", "t3")
	must(t, l.SetPosition("t1", 590))
	must(t, l.MarkPlayed("t3"))
	must(t, l.SetPosition("t2", 100))
	if q := l.Queue(); !slices.Equal(q, []string{"t2"}) {
		t.Fatalf("Queue = %v", q)
	}
}

func TestDownloadsTotal(t *testing.T) {
	l := show(t, 3)
	downloaded(t, l, "t1", "t3")
	if n, mb := l.Downloads(); n != 2 || mb != 200 {
		t.Fatalf("Downloads = %d, %d", n, mb)
	}
	if err := l.Download("zz"); !errors.Is(err, podcast.ErrUnknownEpisode) {
		t.Errorf("Download(zz) = %v", err)
	}
}

func TestCleanupPlayedAfterAWeek(t *testing.T) {
	l := &podcast.Library{}
	must(t, l.Subscribe(podcast.Feed{Name: "Zen"}))
	for _, e := range []podcast.Episode{
		{ID: "a", Feed: "Zen", Published: 13, Length: 600, Size: 50},
		{ID: "b", Feed: "Zen", Published: 14, Length: 600, Size: 50},
		{ID: "c", Feed: "Zen", Published: 1, Length: 600, Size: 50},
	} {
		must(t, l.AddEpisode(e))
	}
	downloaded(t, l, "a", "b", "c")
	must(t, l.MarkPlayed("a"))
	must(t, l.MarkPlayed("b"))
	if got := l.Cleanup(20, 1000); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("Cleanup = %v", got)
	}
	if e := ep(t, l, "b"); !e.Downloaded {
		t.Error("b lost its download")
	}
}

func TestCleanupKeepsQueued(t *testing.T) {
	l := show(t, 2)
	downloaded(t, l, "t1", "t2")
	must(t, l.MarkPlayed("t1"))
	must(t, l.MarkPlayed("t2"))
	queued(t, l, "t1")
	if got := l.Cleanup(30, 0); !slices.Equal(got, []string{"t2"}) {
		t.Fatalf("Cleanup = %v", got)
	}
}

func TestCleanupLimit(t *testing.T) {
	l := &podcast.Library{}
	must(t, l.Subscribe(podcast.Feed{Name: "Zen"}))
	for _, e := range []podcast.Episode{
		{ID: "x", Feed: "Zen", Published: 1, Length: 600, Size: 300},
		{ID: "y", Feed: "Zen", Published: 1, Length: 600, Size: 500},
		{ID: "z", Feed: "Zen", Published: 2, Length: 600, Size: 200},
		{ID: "w", Feed: "Zen", Published: 3, Length: 600, Size: 100},
	} {
		must(t, l.AddEpisode(e))
	}
	downloaded(t, l, "x", "y", "z", "w")
	queued(t, l, "w")
	if got := l.Cleanup(5, 450); !slices.Equal(got, []string{"y", "x"}) {
		t.Fatalf("Cleanup = %v", got)
	}
}

func TestCleanupNearlyFinished(t *testing.T) {
	l := show(t, 1)
	downloaded(t, l, "t1")
	must(t, l.SetPosition("t1", 580))
	if got := l.Cleanup(20, 1000); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("Cleanup = %v", got)
	}
}

func TestAutoDownloadSkipsPlayed(t *testing.T) {
	l := show(t, 2)
	must(t, l.MarkPlayed("t2"))
	if got := l.AutoDownload(); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("AutoDownload = %v", got)
	}
}

func TestAutoDownloadFivePerFeed(t *testing.T) {
	l := show(t, 7)
	downloaded(t, l, "t7")
	if got := l.AutoDownload(); !slices.Equal(got, []string{"t3", "t4", "t5", "t6"}) {
		t.Fatalf("AutoDownload = %v", got)
	}
	if got := l.AutoDownload(); len(got) != 0 {
		t.Errorf("a second AutoDownload = %v", got)
	}
}

func TestAutoDownloadSkipsExplicit(t *testing.T) {
	l := show(t, 1)
	must(t, l.AddEpisode(podcast.Episode{ID: "t2", Feed: "Tech Talk", Published: 2, Length: 600, Explicit: true}))
	if got := l.AutoDownload(); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("AutoDownload = %v", got)
	}
}

func TestImportFeeds(t *testing.T) {
	l := &podcast.Library{}
	must(t, l.Subscribe(podcast.Feed{Name: "Tech Talk", URL: "https://old.example"}))
	n, err := l.ImportFeeds("#podcasts 1\nTech Talk\thttps://new.example\t120\n\nZen\thttps://z.example\n")
	if err != nil || n != 2 {
		t.Fatalf("ImportFeeds = %d, %v", n, err)
	}
	f := l.Feeds()
	if len(f) != 2 || f[0].URL != "https://new.example" || f[0].Speed != 120 || f[1].Name != "Zen" || f[1].Speed != 100 {
		t.Fatalf("Feeds = %+v", f)
	}
}

func TestImportFeedsRejectsBadList(t *testing.T) {
	for _, text := range []string{
		"Zen\thttps://z.example\n",
		"#podcasts 1\nZen\thttps://z.example\t35\n",
		"#podcasts 1\nArt\thttps://a.example\nZen\n",
	} {
		l := &podcast.Library{}
		if _, err := l.ImportFeeds(text); err == nil {
			t.Errorf("ImportFeeds(%q) took a bad list", text)
		}
		if f := l.Feeds(); len(f) != 0 {
			t.Errorf("a refused list added %+v", f)
		}
	}
}

func TestExportFeeds(t *testing.T) {
	l := &podcast.Library{}
	must(t, l.Subscribe(podcast.Feed{Name: "Tech Talk", URL: "https://t.example/rss"}))
	must(t, l.Subscribe(podcast.Feed{Name: "art hour", URL: "https://a.example/feed"}))
	must(t, l.Subscribe(podcast.Feed{Name: "Zen", URL: "https://z.example"}))
	must(t, l.SetSpeed("Tech Talk", 150))
	want := "#podcasts 1\nart hour\thttps://a.example/feed\t100\nTech Talk\thttps://t.example/rss\t150\nZen\thttps://z.example\t100\n"
	if got := l.ExportFeeds(); got != want {
		t.Fatalf("ExportFeeds = %q, want %q", got, want)
	}
}

func TestExportFeedsReadBack(t *testing.T) {
	l := &podcast.Library{}
	must(t, l.Subscribe(podcast.Feed{Name: "Tech Talk", URL: "https://t.example/rss"}))
	must(t, l.SetSpeed("Tech Talk", 150))
	other := &podcast.Library{}
	if n, err := other.ImportFeeds(l.ExportFeeds()); err != nil || n != 1 {
		t.Fatalf("ImportFeeds = %d, %v", n, err)
	}
	if f := other.Feeds(); len(f) != 1 || f[0].URL != "https://t.example/rss" || f[0].Speed != 150 {
		t.Fatalf("Feeds = %+v", f)
	}
}

// chaptered returns a library with z1, an episode with three chapters, and
// z2, one without any.
func chaptered(t *testing.T) *podcast.Library {
	t.Helper()
	l := &podcast.Library{}
	must(t, l.Subscribe(podcast.Feed{Name: "Zen"}))
	chapters := []podcast.Chapter{{Start: 0, Title: "Intro"}, {Start: 245, Title: "News"}, {Start: 3723, Title: "Outro"}}
	must(t, l.AddEpisode(podcast.Episode{ID: "z1", Feed: "Zen", Length: 4000, Chapters: chapters}))
	must(t, l.AddEpisode(podcast.Episode{ID: "z2", Feed: "Zen", Length: 600}))
	return l
}

func TestChapterAt(t *testing.T) {
	l := chaptered(t)
	cases := []struct {
		pos  podcast.Seconds
		want string
	}{{0, "Intro"}, {244, "Intro"}, {245, "News"}, {3800, "Outro"}}
	for _, c := range cases {
		must(t, l.SetPosition("z1", c.pos))
		if ch, err := l.ChapterAt("z1"); err != nil || ch.Title != c.want {
			t.Errorf("at %d: ChapterAt = %+v, %v; want %s", c.pos, ch, err, c.want)
		}
	}
	if _, err := l.ChapterAt("z2"); !errors.Is(err, podcast.ErrNoChapter) {
		t.Errorf("ChapterAt without chapters = %v", err)
	}
	if _, err := l.ChapterAt("zz"); !errors.Is(err, podcast.ErrUnknownEpisode) {
		t.Errorf("ChapterAt(zz) = %v", err)
	}
}

func TestChapterList(t *testing.T) {
	l := chaptered(t)
	if got := l.ChapterList("z1"); got != "0:00 Intro\n4:05 News\n1:02:03 Outro\n" {
		t.Fatalf("ChapterList = %q", got)
	}
}

func TestParseChapters(t *testing.T) {
	notes := "Today on Zen.\n0:00 Intro\n 4:05  News \nLunch at 12:30\n1:02:03 Outro\n"
	got, err := podcast.ParseChapters(notes)
	want := []podcast.Chapter{{Start: 0, Title: "Intro"}, {Start: 245, Title: "News"}, {Start: 3723, Title: "Outro"}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("ParseChapters = %+v, %v", got, err)
	}
}

func TestParseChaptersOrder(t *testing.T) {
	for _, notes := range []string{"1:00 Late\n2:00 More\n", "0:00 A\n5:00 B\n4:00 C\n", "0:00 A\n0:00 B\n"} {
		if _, err := podcast.ParseChapters(notes); !errors.Is(err, podcast.ErrBadChapters) {
			t.Errorf("ParseChapters(%q) = %v", notes, err)
		}
	}
}
`},
		Solution: map[string]string{
			"library.go": `package podcast

import (
	"errors"
	"slices"
	"sort"
	"strings"
)

// Day is a day number, counted from the day the app was installed.
type Day int

// Seconds is a length of time in whole seconds.
type Seconds int

// Episode is one episode of a feed. Size is the size of its download in MB.
type Episode struct {
	ID         string
	Feed       string
	Title      string
	Published  Day
	Length     Seconds
	Position   Seconds
	Size       int
	Downloaded bool
	Explicit   bool
	Chapters   []Chapter
}

// Feed is one subscription. Speed is its playback speed in percent.
type Feed struct {
	Name      string
	URL       string
	Speed     int
	AutoQueue bool
}

// ErrUnknownFeed is returned for a feed name we are not subscribed to.
var ErrUnknownFeed = errors.New("unknown feed")

// ErrUnknownEpisode is returned for an episode ID we do not have.
var ErrUnknownEpisode = errors.New("unknown episode")

// ErrDuplicate is returned when a feed or episode is already there.
var ErrDuplicate = errors.New("already there")

// ErrBadSpeed is returned for a speed that is not 50 to 300 in steps of 10.
var ErrBadSpeed = errors.New("bad speed")

// Library is the user's subscriptions, their episodes and the play queue.
type Library struct {
	feeds    []Feed
	episodes []Episode
	queue    []string
}

// feed returns the subscription with that name, or nil.
func (l *Library) feed(name string) *Feed {
	for i := range l.feeds {
		if l.feeds[i].Name == name {
			return &l.feeds[i]
		}
	}
	return nil
}

// find returns the index of the episode with that ID, or -1.
func (l *Library) find(id string) int {
	for i, e := range l.episodes {
		if e.ID == id {
			return i
		}
	}
	return -1
}

// Subscribe adds a feed. A feed without a speed gets 100.
func (l *Library) Subscribe(f Feed) error {
	if l.feed(f.Name) != nil {
		return ErrDuplicate
	}
	if f.Speed == 0 {
		f.Speed = 100
	}
	l.feeds = append(l.feeds, f)
	return nil
}

// Unsubscribe removes a feed, its episodes and their places in the queue.
func (l *Library) Unsubscribe(name string) error {
	if l.feed(name) == nil {
		return ErrUnknownFeed
	}
	l.queue = slices.DeleteFunc(l.queue, func(id string) bool { return l.episodes[l.find(id)].Feed == name })
	l.episodes = slices.DeleteFunc(l.episodes, func(e Episode) bool { return e.Feed == name })
	l.feeds = slices.DeleteFunc(l.feeds, func(f Feed) bool { return f.Name == name })
	return nil
}

// Feeds returns the subscriptions sorted by name.
func (l *Library) Feeds() []Feed {
	out := append([]Feed(nil), l.feeds...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func validSpeed(p int) bool { return p >= 50 && p <= 300 && p%10 == 0 }

// SetSpeed sets a feed's playback speed in percent.
func (l *Library) SetSpeed(feed string, percent int) error {
	f := l.feed(feed)
	if f == nil {
		return ErrUnknownFeed
	}
	if !validSpeed(percent) {
		return ErrBadSpeed
	}
	f.Speed = percent
	return nil
}

// CleanTitle drops a leading feed name, in any case, from an episode title,
// with the spaces, digits, '#', ':' and '-' right after it.
func CleanTitle(feed, title string) string {
	if len(title) < len(feed) || !strings.EqualFold(title[:len(feed)], feed) {
		return title
	}
	if rest := strings.TrimLeft(title[len(feed):], " 0123456789#:-"); rest != "" {
		return rest
	}
	return title
}

// AddEpisode adds an episode of a feed we are subscribed to, with its title
// cleaned.
func (l *Library) AddEpisode(e Episode) error {
	if l.feed(e.Feed) == nil {
		return ErrUnknownFeed
	}
	if l.find(e.ID) >= 0 {
		return ErrDuplicate
	}
	e.Title = CleanTitle(e.Feed, e.Title)
	l.episodes = append(l.episodes, e)
	return nil
}

// Episode returns the episode with that ID.
func (l *Library) Episode(id string) (Episode, error) {
	i := l.find(id)
	if i < 0 {
		return Episode{}, ErrUnknownEpisode
	}
	return l.episodes[i], nil
}

// Episodes returns a feed's episodes, newest published first, then by ID.
func (l *Library) Episodes(feed string) []Episode {
	var out []Episode
	for _, e := range l.episodes {
		if e.Feed == feed {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Published != out[j].Published {
			return out[i].Published > out[j].Published
		}
		return out[i].ID < out[j].ID
	})
	return out
}
`,
			"queue.go": `package podcast

import (
	"errors"
	"fmt"
	"slices"
)

// ErrNotQueued is returned for an episode that is not in the queue.
var ErrNotQueued = errors.New("not in the queue")

// ErrEmptyQueue is returned by Next when the queue is empty.
var ErrEmptyQueue = errors.New("queue is empty")

// ErrBadPosition is returned for a queue position outside the queue.
var ErrBadPosition = errors.New("no such queue position")

// Enqueue adds an episode to the end of the queue, unless it is queued.
func (l *Library) Enqueue(id string) error {
	if l.find(id) < 0 {
		return ErrUnknownEpisode
	}
	if !slices.Contains(l.queue, id) {
		l.queue = append(l.queue, id)
	}
	return nil
}

// PlayNext puts an episode at the front of the queue.
func (l *Library) PlayNext(id string) error {
	if l.find(id) < 0 {
		return ErrUnknownEpisode
	}
	l.queue = slices.DeleteFunc(l.queue, func(q string) bool { return q == id })
	l.queue = slices.Insert(l.queue, 0, id)
	return nil
}

// Dequeue takes an episode out of the queue.
func (l *Library) Dequeue(id string) error {
	i := slices.Index(l.queue, id)
	if i < 0 {
		return ErrNotQueued
	}
	l.queue = slices.Delete(l.queue, i, i+1)
	return nil
}

// Queue returns the IDs in the queue, front first.
func (l *Library) Queue() []string {
	return append([]string(nil), l.queue...)
}

// Position returns where an episode stands in the queue, the front being 1,
// or 0 when it is not queued.
func (l *Library) Position(id string) int { return slices.Index(l.queue, id) + 1 }

// Next takes the front episode off the queue and returns it.
func (l *Library) Next() (Episode, error) {
	if len(l.queue) == 0 {
		return Episode{}, ErrEmptyQueue
	}
	id := l.queue[0]
	l.queue = l.queue[1:]
	return l.Episode(id)
}

// Move moves the episode at queue position from to position to, the others
// shifting to make room.
func (l *Library) Move(from, to int) error {
	if from < 1 || from > len(l.queue) || to < 1 || to > len(l.queue) {
		return ErrBadPosition
	}
	id := l.queue[from-1]
	l.queue = slices.Delete(l.queue, from-1, from)
	l.queue = slices.Insert(l.queue, to-1, id)
	return nil
}

// At returns the episode at a queue position.
func (l *Library) At(pos int) (Episode, error) {
	if pos < 1 || pos > len(l.queue) {
		return Episode{}, ErrBadPosition
	}
	return l.Episode(l.queue[pos-1])
}

// QueueSummary says how many episodes are queued and how long is left in
// them at normal speed.
func (l *Library) QueueSummary() string {
	var left Seconds
	for _, id := range l.queue {
		e := l.episodes[l.find(id)]
		left += e.Length - e.Position
	}
	return fmt.Sprintf("%d episodes, %s left", len(l.queue), Clock(left))
}

// TimeLeft returns the listening time left in the queue: each episode's rest
// divided by its feed's speed, rounded up to a whole second.
func (l *Library) TimeLeft() Seconds {
	var total Seconds
	for _, id := range l.queue {
		e := l.episodes[l.find(id)]
		speed := Seconds(l.feed(e.Feed).Speed)
		total += ((e.Length-e.Position)*100 + speed - 1) / speed
	}
	return total
}
`,
			"playback.go": `package podcast

import (
	"errors"
	"fmt"
	"slices"
)

// nearEnd is how close to its end an episode counts as played.
const nearEnd Seconds = 30

// ErrNothingToPlay is returned by Continue when a feed has nothing left.
var ErrNothingToPlay = errors.New("nothing to play")

// Played reports whether the episode counts as played.
func (e Episode) Played() bool { return e.Position >= e.Length-nearEnd }

// SetPosition sets how far into an episode we are, kept within its length.
func (l *Library) SetPosition(id string, s Seconds) error {
	i := l.find(id)
	if i < 0 {
		return ErrUnknownEpisode
	}
	l.episodes[i].Position = min(max(s, 0), l.episodes[i].Length)
	l.leaveQueueIfPlayed(i)
	return nil
}

// MarkPlayed puts an episode's position at its end.
func (l *Library) MarkPlayed(id string) error {
	i := l.find(id)
	if i < 0 {
		return ErrUnknownEpisode
	}
	l.episodes[i].Position = l.episodes[i].Length
	l.leaveQueueIfPlayed(i)
	return nil
}

// leaveQueueIfPlayed takes the episode at index i out of the queue once it
// is played.
func (l *Library) leaveQueueIfPlayed(i int) {
	if e := l.episodes[i]; e.Played() {
		l.queue = slices.DeleteFunc(l.queue, func(id string) bool { return id == e.ID })
	}
}

// Clock writes a duration the way the app shows it.
func Clock(s Seconds) string {
	h, m, sec := s/3600, s%3600/60, s%60
	if h == 0 {
		return fmt.Sprintf("%d:%02d", m, sec)
	}
	return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
}

// Continue returns what to play next from a feed: the newest episode that is
// started but not played, or else the oldest one not played.
func (l *Library) Continue(feed string) (Episode, error) {
	eps := l.Episodes(feed)
	for _, e := range eps {
		if e.Position > 0 && !e.Played() {
			return e, nil
		}
	}
	for i := len(eps) - 1; i >= 0; i-- {
		if !eps[i].Played() {
			return eps[i], nil
		}
	}
	return Episode{}, ErrNothingToPlay
}
`,
			"downloads.go": `package podcast

import "sort"

// autoDownloads is how many of each feed's newest episodes auto-download
// keeps downloaded.
const autoDownloads = 5

// Download marks an episode downloaded.
func (l *Library) Download(id string) error {
	i := l.find(id)
	if i < 0 {
		return ErrUnknownEpisode
	}
	l.episodes[i].Downloaded = true
	return nil
}

// Downloads returns how many episodes are downloaded and their size in MB.
func (l *Library) Downloads() (count, mb int) {
	for _, e := range l.episodes {
		if e.Downloaded {
			count++
			mb += e.Size
		}
	}
	return count, mb
}

// AutoDownload downloads each feed's newest episodes that it should, and
// returns the IDs it downloaded, sorted.
func (l *Library) AutoDownload() []string {
	var got []string
	for _, f := range l.feeds {
		n := 0
		for _, e := range l.Episodes(f.Name) {
			if e.Explicit {
				continue
			}
			if e.Played() {
				continue
			}
			if n++; n > autoDownloads {
				break
			}
			if !e.Downloaded {
				l.episodes[l.find(e.ID)].Downloaded = true
				got = append(got, e.ID)
			}
		}
	}
	sort.Strings(got)
	return got
}

// cleanupOrder returns the indexes of the downloaded episodes in the order
// cleanup goes through them: published longest ago first, then the larger
// download, then the lower ID.
func (l *Library) cleanupOrder() []int {
	var idx []int
	for i, e := range l.episodes {
		if e.Downloaded {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(a, b int) bool {
		x, y := l.episodes[idx[a]], l.episodes[idx[b]]
		if x.Published != y.Published {
			return x.Published < y.Published
		}
		if x.Size != y.Size {
			return x.Size > y.Size
		}
		return x.ID < y.ID
	})
	return idx
}

// Cleanup removes downloads as docs/cleanup.md says and returns the IDs of
// the episodes that lost them, in that order.
func (l *Library) Cleanup(today Day, limitMB int) []string {
	queued := map[string]bool{}
	for _, id := range l.queue {
		queued[id] = true
	}
	var removed []string
	order := l.cleanupOrder()
	for _, i := range order {
		e := &l.episodes[i]
		finished := e.Played()
		old := today-e.Published >= 7
		if finished && old && !queued[e.ID] {
			e.Downloaded = false
			removed = append(removed, e.ID)
		}
	}
	_, total := l.Downloads()
	for _, i := range order {
		if total <= limitMB {
			break
		}
		if e := &l.episodes[i]; e.Downloaded && !queued[e.ID] {
			e.Downloaded = false
			total -= e.Size
			removed = append(removed, e.ID)
		}
	}
	return removed
}

// Arrive adds a newly published episode and queues it when its feed has
// AutoQueue on.
func (l *Library) Arrive(e Episode) error {
	if err := l.AddEpisode(e); err != nil {
		return err
	}
	if f := l.feed(e.Feed); f.AutoQueue && !e.Explicit {
		l.queue = append(l.queue, e.ID)
	}
	return nil
}
`,
			"feeds.go": `package podcast

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// listHeader is the first line of a subscription list.
const listHeader = "#podcasts 1"

// ImportFeeds reads a subscription list, as docs/feeds.md describes it, and
// returns how many feeds it read. A bad list changes nothing.
func (l *Library) ImportFeeds(text string) (int, error) {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if lines[0] != listHeader {
		return 0, errors.New("not a subscription list")
	}
	var feeds []Feed
	for n, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f, err := parseFeedLine(line)
		if err != nil {
			return 0, fmt.Errorf("line %d: %w", n+2, err)
		}
		feeds = append(feeds, f)
	}
	for _, f := range feeds {
		if old := l.feed(f.Name); old != nil {
			old.URL, old.Speed = f.URL, f.Speed
		} else {
			l.feeds = append(l.feeds, f)
		}
	}
	return len(feeds), nil
}

// parseFeedLine reads one feed's line of a subscription list.
func parseFeedLine(line string) (Feed, error) {
	parts := strings.Split(line, "\t")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
		return Feed{}, errors.New("want a name, a url and maybe a speed")
	}
	f := Feed{Name: parts[0], URL: parts[1], Speed: 100}
	if len(parts) == 3 {
		s, err := strconv.Atoi(parts[2])
		if err != nil || !validSpeed(s) {
			return Feed{}, ErrBadSpeed
		}
		f.Speed = s
	}
	return f, nil
}

// ExportFeeds writes the subscriptions as a list another phone can read.
func (l *Library) ExportFeeds() string {
	feeds := l.Feeds()
	sort.SliceStable(feeds, func(i, j int) bool {
		return strings.ToLower(feeds[i].Name) < strings.ToLower(feeds[j].Name)
	})
	var b strings.Builder
	b.WriteString(listHeader + "\n")
	for _, f := range feeds {
		b.WriteString(feedLine(f) + "\n")
	}
	return b.String()
}

// feedLine writes one feed's line of a subscription list.
func feedLine(f Feed) string {
	return f.Name + "\t" + f.URL + "\t" + strconv.Itoa(f.Speed)
}
`,
			"chapters.go": `package podcast

import (
	"errors"
	"strconv"
	"strings"
)

// Chapter is a part of an episode, starting Start seconds in.
type Chapter struct {
	Start Seconds
	Title string
}

// ErrNoChapter is returned when an episode's position is in no chapter.
var ErrNoChapter = errors.New("no chapter here")

// ErrBadChapters is returned for chapters that do not start at 0 and go up.
var ErrBadChapters = errors.New("chapters out of order")

// ChapterAt returns the chapter an episode's position is in: the last one
// starting at or before it.
func (l *Library) ChapterAt(id string) (Chapter, error) {
	e, err := l.Episode(id)
	if err != nil {
		return Chapter{}, err
	}
	for i := len(e.Chapters) - 1; i >= 0; i-- {
		if e.Chapters[i].Start <= e.Position {
			return e.Chapters[i], nil
		}
	}
	return Chapter{}, ErrNoChapter
}

// ChapterList writes an episode's chapters, one line each: the start as the
// app shows durations, a space and the title.
func (l *Library) ChapterList(id string) string {
	e, _ := l.Episode(id)
	var b strings.Builder
	for _, c := range e.Chapters {
		b.WriteString(Clock(c.Start) + " " + c.Title + "\n")
	}
	return b.String()
}

// ParseChapters reads chapters from episode notes: every line that starts
// with a duration, a space and a title. Other lines are skipped.
func ParseChapters(notes string) ([]Chapter, error) {
	var out []Chapter
	for _, line := range strings.Split(notes, "\n") {
		clock, title, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		start, ok := parseClock(clock)
		if !ok {
			continue
		}
		if len(out) == 0 && start != 0 || len(out) > 0 && start <= out[len(out)-1].Start {
			return nil, ErrBadChapters
		}
		out = append(out, Chapter{Start: start, Title: strings.TrimSpace(title)})
	}
	return out, nil
}

// parseClock reads a duration written the way Clock writes it.
func parseClock(s string) (Seconds, bool) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var total Seconds
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || i > 0 && (len(p) != 2 || n > 59) {
			return 0, false
		}
		total = total*60 + Seconds(n)
	}
	return total, true
}
`,
		},
		Turns: script(
			cloud("This week is the podcast player; start by reading library.go and queue.go. Fix Enqueue: an unknown ID gives ErrUnknownEpisode, and an episode that's already queued stays where it is. Then add `func (l *Library) Position(id string) int` to queue.go: where the episode stands in the queue, or 0 if it isn't queued. Positions are what people see in the app, so they count from 1, and every queue call that deals in positions uses those numbers."),
			local("Add `func (l *Library) PlayNext(id string) error` to queue.go: the episode goes to the front of the queue, moving there if it's queued already. An unknown ID gives ErrUnknownEpisode."),
			cloud("Add `func (l *Library) Episodes(feed string) []Episode` to library.go: that feed's episodes, newest published first, ties by ID. And so you know for when we show durations: we write them as h:mm:ss but leave the hours off when there are none, so 4:05 and 1:02:03."),
			cloud("Read docs/feeds.md. Add a `Speed int` field to Feed, in percent; Subscribe should give a feed without one a speed of 100. Then create feeds.go with `func (l *Library) ImportFeeds(text string) (int, error)`: it reads a subscription list and returns how many feeds it read."),
			cloud("Add `func (l *Library) Dequeue(id string) error` to queue.go: it takes the episode out of the queue, or returns a new ErrNotQueued if it isn't there. Also, for the auto-download work later on: auto-download should make sure the 3 newest unplayed episodes of every feed are downloaded."),
			local("Create playback.go with `func (l *Library) SetPosition(id string, seconds int) error`, which keeps seconds between 0 and the episode's Length, and `func (e Episode) Played() bool`. An episode counts as played once its position is within 30 seconds of its end."),
			local("Read docs/cleanup.md; we'll build cleanup later. Tell me in two or three lines which downloads it removes. No code changes."),
			local("Add `func (l *Library) SetSpeed(feed string, percent int) error` to library.go: 50 to 300 in steps of 10, anything else gives a new ErrBadSpeed, and a feed we don't have gives ErrUnknownFeed."),
			local("Add a `Downloaded bool` field to Episode, and `func (l *Library) Download(id string) error` in a new downloads.go that marks the episode downloaded, or returns ErrUnknownEpisode."),
			cloud("Add `func (l *Library) Next() (Episode, error)` to queue.go: it takes the front episode off the queue and returns it, or returns a new ErrEmptyQueue. And one change to the auto-download plan I gave you: make it 5 per feed instead."),
			local("Add `func Clock(seconds int) string` to playback.go: a duration in seconds written the way the app shows it."),
			local("Add an `Explicit bool` field to Episode, and `func (l *Library) MarkPlayed(id string) error` to playback.go, which puts the episode's position at its end. And from now on: my kids use the app at weekends, so anything that picks episodes by itself has to skip explicit ones."),
			cloud("Plain ints for seconds are getting confusing. Add `type Seconds int` and use it for Episode.Length and Position and for every function that takes or returns seconds, in every file."),
			local("Add `func (l *Library) Move(from, to int) error` to queue.go: the episode at position from moves to position to, the others shifting to make room. A position outside the queue gives a new ErrBadPosition."),
			cloud("Add `func (l *Library) Continue(feed string) (Episode, error)` to playback.go: what to play next from a feed. That's the newest episode that's started but not played, or if there's none, the oldest one not played; with nothing left, a new ErrNothingToPlay."),
			local("Add `func (l *Library) ExportFeeds() string` to feeds.go: our subscriptions written as a list that ImportFeeds on another phone can read."),
			cloud("Lots of feeds start every title with the show's name, like \"Tech Talk 42: Chips\". Add `func CleanTitle(feed, title string) string` to library.go: the title without a leading feed name in any case, nor the spaces, digits, '#', ':' and '-' right after it, so \"Chips\" there; if nothing would be left, keep the title as it was. AddEpisode should store titles cleaned."),
			local("Add `func (l *Library) AutoDownload() []string` to downloads.go: it downloads what auto-download should and returns the IDs it downloaded, sorted."),
			cloud("Now the download cleanup: add `func (l *Library) Cleanup(today Day, limitMB int) []string` to downloads.go."),
			local("Add `func (l *Library) At(pos int) (Episode, error)` to queue.go: the episode at that position in the queue, or ErrBadPosition."),
			cloud("Add an `AutoQueue bool` field to Feed, and `func (l *Library) Arrive(e Episode) error` to downloads.go for newly published episodes: it adds the episode as AddEpisode does, then puts it at the end of the queue if its feed has AutoQueue on."),
			local("Add `func (l *Library) QueueSummary() string` to queue.go, like \"3 episodes, 2:10:00 left\": how many episodes are queued and how long is left in them at normal speed."),
			cloud("Add `func (l *Library) TimeLeft() Seconds` to queue.go: the real listening time left in the queue, each episode's remaining time divided by its feed's speed and rounded up to a whole second."),
			local("Add `func (l *Library) Unsubscribe(name string) error` to library.go: it removes the feed, its episodes and their places in the queue, or returns ErrUnknownFeed."),
			cloud("Played episodes shouldn't hang around in the queue: when SetPosition or MarkPlayed leaves an episode played, take it out of the queue."),
			local("Add `func (l *Library) Downloads() (count, mb int)` to downloads.go: how many episodes are downloaded and their total size in MB."),
			local("Episodes can have chapters. Create chapters.go with `type Chapter struct { Start Seconds; Title string }`, add a `Chapters []Chapter` field to Episode, and add `func (l *Library) ChapterAt(id string) (Chapter, error)`: the chapter the episode's position is in, which is the last one starting at or before it. If there's no such chapter, return a new ErrNoChapter; an unknown ID gives ErrUnknownEpisode."),
			local("Add `func (l *Library) ChapterList(id string) string` to chapters.go: one line per chapter, each ending in a newline, with the chapter's start written the way the app writes durations, then a space and its title."),
			cloud("Feeds put chapters in the episode notes, written the way ChapterList writes them. Add `func ParseChapters(notes string) ([]Chapter, error)` to chapters.go: it takes every line that starts with a duration, a space and a title, trimming spaces, and skips the other lines. The first chapter must start at 0 and each later one after the one before; if not, return a new ErrBadChapters."),
		),
		Carries: []Carry{
			{
				What:        "queue positions count from 1",
				Kind:        CarryEmbodied,
				Established: 0,
				Needed:      []int{13, 19},
				Tests:       []string{"TestMoveOneBased", "TestAtOneBased"},
				Absent:      []string{"count from 1", "from 1", "1-based", "one-based", "zero-based", "0-based"},
			},
			{
				What:        "cleanup: played and a week old, then oldest and largest first down to the limit, never a queued episode (docs/cleanup.md)",
				Kind:        CarryDiscovered,
				Established: 6,
				Needed:      []int{18},
				Source:      "docs/cleanup.md",
				Tests:       []string{"TestCleanupPlayedAfterAWeek", "TestCleanupKeepsQueued", "TestCleanupLimit"},
				Absent:      []string{"7 days", "seven days", "longest ago", "larger download", "in the queue keeps", "two passes"},
			},
			{
				What:        "a written subscription list goes in name order ignoring case and always has the speed (docs/feeds.md)",
				Kind:        CarryDiscovered,
				Established: 3,
				Needed:      []int{15},
				Source:      "docs/feeds.md",
				Tests:       []string{"TestExportFeeds", "TestExportFeedsReadBack"},
				Absent:      []string{"#podcasts", "tabs", "a tab", "alphabetical", "always writes", "upper and lower"},
			},
			{
				What:        "an episode within 30 seconds of its end counts as played",
				Kind:        CarryEmbodied,
				Established: 5,
				Needed:      []int{14, 18, 24},
				Tests:       []string{"TestCleanupNearlyFinished", "TestContinueSkipsNearlyFinished", "TestPlayedLeavesQueue"},
				Absent:      []string{"30 seconds", "within 30", "thirty"},
			},
			{
				What:        "durations show as h:mm:ss without the hours when there are none",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{10, 21, 27},
				Tests:       []string{"TestClock", "TestQueueSummary", "TestChapterList"},
				Absent:      []string{"h:mm:ss", "4:05", "1:02:03", "hours off", "m:ss"},
			},
			{
				What:        "auto-download takes each feed's newest episodes that are not played",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{17},
				Tests:       []string{"TestAutoDownloadSkipsPlayed"},
				Absent:      []string{"3 newest", "three newest", "newest unplayed", "unplayed"},
			},
			{
				What:        "auto-download keeps 5 per feed (revises the 3 said before)",
				Kind:        CarryUser,
				Established: 9,
				Needed:      []int{17},
				Tests:       []string{"TestAutoDownloadFivePerFeed"},
				Absent:      []string{"5 per feed", "five per feed", "make it 5"},
			},
			{
				What:        "anything that picks episodes by itself skips explicit ones",
				Kind:        CarryUser,
				Established: 11,
				Needed:      []int{17, 20},
				Tests:       []string{"TestAutoDownloadSkipsExplicit", "TestArriveSkipsExplicit"},
				Absent:      []string{"explicit", "kids", "by itself"},
			},
		},
	}
}
