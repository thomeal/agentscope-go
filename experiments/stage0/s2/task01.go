package s2

func task01() Task {
	return Task{
		ID:     "s2-01-job-queue",
		Set:    "tuning",
		System: system("a job queue for background work"),
		Files: map[string]string{
			"go.mod": goMod,
			"job.go": `package sched

// Job is one unit of scheduled work.
type Job struct {
	Name     string
	Priority int
	Attempts int
}
`,
			"queue.go": `package sched

import "sort"

// Queue holds jobs waiting to run.
type Queue struct {
	jobs []Job
}

// Add appends a job to the queue.
func (q *Queue) Add(j Job) {
	q.jobs = append(q.jobs, j)
}

// Len reports the number of queued jobs.
func (q *Queue) Len() int { return len(q.jobs) }

// Pop removes and returns the highest-priority job. ok is false when empty.
func (q *Queue) Pop() (j Job, ok bool) {
	if len(q.jobs) == 0 {
		return Job{}, false
	}
	sort.SliceStable(q.jobs, func(a, b int) bool { return q.jobs[a].Priority > q.jobs[b].Priority })
	j = q.jobs[0]
	q.jobs = q.jobs[1:]
	return j, true
}
`,
			"ops/console.txt": `Operations console notes

Operators type job names by hand, so the console forwards them in whatever
letter case was typed. "Nightly" and "NIGHTLY" name the same job, and a job must
be found either way.

The console polls the queue every few seconds and keeps no cache.
`,
			"queue_test.go": `package sched

import "testing"

func TestPopOrder(t *testing.T) {
	var q Queue
	q.Add(Job{Name: "low", Priority: 1})
	q.Add(Job{Name: "high", Priority: 9})
	j, ok := q.Pop()
	if !ok || j.Name != "high" {
		t.Fatalf("got %+v, %v", j, ok)
	}
	if q.Len() != 1 {
		t.Fatalf("len %d", q.Len())
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package sched_test

import (
	"errors"
	"testing"

	sched "fixture"
)

func queue() *sched.Queue {
	q := &sched.Queue{}
	q.Add(sched.Job{Name: "Nightly", Priority: 1})
	q.Add(sched.Job{Name: "hourly", Priority: 5})
	return q
}

func TestPeekLeavesJob(t *testing.T) {
	q := queue()
	j, ok := q.Peek()
	if !ok || j.Name != "hourly" {
		t.Fatalf("Peek = %+v, %v", j, ok)
	}
	if q.Len() != 2 {
		t.Fatalf("Peek removed a job: len %d", q.Len())
	}
	if _, ok := (&sched.Queue{}).Peek(); ok {
		t.Fatal("Peek on an empty queue reported a job")
	}
}

func TestRetryRequeues(t *testing.T) {
	q := &sched.Queue{}
	got := q.Retry(sched.Job{Name: "a", Attempts: 2})
	if got.Attempts != 3 {
		t.Fatalf("Attempts = %d", got.Attempts)
	}
	if j, ok := q.Pop(); !ok || j.Attempts != 3 {
		t.Fatalf("queued %+v, %v", j, ok)
	}
}

func TestBackoffShape(t *testing.T) {
	first := sched.Backoff(1)
	if first <= 0 || sched.Backoff(2) != 2*first || sched.Backoff(3) != 4*first {
		t.Fatalf("not doubling: %d %d %d", first, sched.Backoff(2), sched.Backoff(3))
	}
	if got := sched.Backoff(30); got != 30*first {
		t.Fatalf("Backoff(30) = %d, want the cap, 30 times the first wait %d", got, first)
	}
}

func TestBackoffMillis(t *testing.T) {
	for attempt, want := range map[int]int{1: 2000, 3: 8000, 6: 60000, 9: 60000} {
		if got := sched.Backoff(attempt); got != want {
			t.Errorf("Backoff(%d) = %d, want %d", attempt, got, want)
		}
	}
}

func TestGraceWindowMillis(t *testing.T) {
	if got := (sched.Job{}).GraceWindow(); got != 30000 {
		t.Errorf("no attempts: %d", got)
	}
	if got := (sched.Job{Attempts: 2}).GraceWindow(); got != 40000 {
		t.Errorf("two attempts: %d", got)
	}
}

func TestCancelRemoves(t *testing.T) {
	q := queue()
	if err := q.Cancel("hourly"); err != nil {
		t.Fatal(err)
	}
	if j, ok := q.Pop(); q.Len() != 0 || !ok || j.Name != "Nightly" {
		t.Fatalf("left %+v, len %d", j, q.Len())
	}
}

func TestCancelAnyCase(t *testing.T) {
	q := queue()
	if err := q.Cancel("NIGHTLY"); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 1 {
		t.Fatalf("len %d", q.Len())
	}
}

func TestCancelMissingSentinel(t *testing.T) {
	q := queue()
	if err := q.Cancel("ghost"); !errors.Is(err, sched.ErrNotQueued) {
		t.Fatalf("err = %v", err)
	}
	if q.Len() != 2 {
		t.Fatalf("len %d", q.Len())
	}
}

func TestFindExact(t *testing.T) {
	q := queue()
	j, err := q.Find("hourly")
	if err != nil || j.Priority != 5 {
		t.Fatalf("Find = %+v, %v", j, err)
	}
	if q.Len() != 2 {
		t.Fatalf("Find removed a job: len %d", q.Len())
	}
}

func TestFindAnyCase(t *testing.T) {
	j, err := queue().Find("nIGHTLY")
	if err != nil || j.Name != "Nightly" {
		t.Fatalf("Find = %+v, %v", j, err)
	}
}

func TestFindMissingSentinel(t *testing.T) {
	if _, err := queue().Find("ghost"); !errors.Is(err, sched.ErrNotQueued) {
		t.Fatalf("err = %v", err)
	}
}
`},
		Solution: map[string]string{
			"queue.go": `package sched

import (
	"errors"
	"sort"
	"strings"
)

// ErrNotQueued is returned when a named job is not in the queue.
var ErrNotQueued = errors.New("job not queued")

// Queue holds jobs waiting to run.
type Queue struct {
	jobs []Job
}

// Add appends a job to the queue.
func (q *Queue) Add(j Job) {
	q.jobs = append(q.jobs, j)
}

// Len reports the number of queued jobs.
func (q *Queue) Len() int { return len(q.jobs) }

// Pop removes and returns the highest-priority job. ok is false when empty.
func (q *Queue) Pop() (j Job, ok bool) {
	j, ok = q.Peek()
	if ok {
		q.jobs = q.jobs[1:]
	}
	return j, ok
}

// Peek returns what Pop would return without removing it.
func (q *Queue) Peek() (Job, bool) {
	if len(q.jobs) == 0 {
		return Job{}, false
	}
	sort.SliceStable(q.jobs, func(a, b int) bool { return q.jobs[a].Priority > q.jobs[b].Priority })
	return q.jobs[0], true
}

// Retry counts one more attempt and queues the job again.
func (q *Queue) Retry(j Job) Job {
	j.Attempts++
	q.Add(j)
	return j
}

func (q *Queue) index(name string) int {
	for i, j := range q.jobs {
		if strings.EqualFold(j.Name, name) {
			return i
		}
	}
	return -1
}

// Cancel takes the named job out of the queue.
func (q *Queue) Cancel(name string) error {
	i := q.index(name)
	if i < 0 {
		return ErrNotQueued
	}
	q.jobs = append(q.jobs[:i], q.jobs[i+1:]...)
	return nil
}

// Find looks a job up by name without removing it.
func (q *Queue) Find(name string) (Job, error) {
	i := q.index(name)
	if i < 0 {
		return Job{}, ErrNotQueued
	}
	return q.jobs[i], nil
}
`,
			"retry.go": `package sched

const maxBackoff = 60000

// Backoff returns the wait in milliseconds before retry number attempt.
func Backoff(attempt int) int {
	d := 2000
	for i := 1; i < attempt && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// GraceWindow returns how long the job may overrun, in milliseconds.
func (j Job) GraceWindow() int { return 30000 + 5000*j.Attempts }
`,
		},
		Turns: script(
			cloud("A house rule for this package before anything else: anything that represents a span of time is an int holding milliseconds, because outside tooling reads these numbers raw. Nothing needs it yet. First change: add `func (q *Queue) Peek() (Job, bool)`, which returns what Pop would return without removing it."),
			cloud("Add `var ErrNotQueued = errors.New(\"job not queued\")` to queue.go. Nothing returns it yet, but it is what anything in this package that names a job must return when that job isn't in the queue."),
			local("Add `func Backoff(attempt int) int` in a new file retry.go: the wait before retry number attempt, where 1 is the first retry. The first retry waits 2 seconds, each later retry waits twice as long as the one before, and the wait never exceeds 1 minute."),
			local("Read ops/console.txt and tell me in a sentence or two what it means for how we identify jobs. No code changes."),
			local("Add `func (q *Queue) Retry(j Job) Job`: it adds one to the job's Attempts, queues it again and returns the updated job."),
			cloud("Add `func (q *Queue) Cancel(name string) error`, which takes the named job out of the queue."),
			cloud("Add `func (j Job) GraceWindow() int`: how long a job may overrun before it is killed. That's 30 seconds, plus 5 more seconds for every attempt it has already made."),
			local("Add `func (q *Queue) Find(name string) (Job, error)`, which looks a job up by name without removing it."),
		),
		Carries: []Carry{
			{
				What:        "spans of time are int milliseconds",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2, 6},
				Tests:       []string{"TestBackoffMillis", "TestGraceWindowMillis"},
				Absent:      []string{"millisecond", "milli", " ms"},
			},
			{
				What:        "job names match in any letter case (ops/console.txt)",
				Kind:        CarryDiscovered,
				Established: 3,
				Needed:      []int{5, 7},
				Source:      "ops/console.txt",
				Tests:       []string{"TestCancelAnyCase", "TestFindAnyCase"},
				Absent:      []string{"letter case", "case-insensitive", "upper", "lower"},
			},
			{
				What:        "ErrNotQueued for any named job that isn't queued",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{5, 7},
				Tests:       []string{"TestCancelMissingSentinel", "TestFindMissingSentinel"},
				Absent:      []string{"errnotqueued", "sentinel", "isn't in the queue"},
			},
		},
	}
}
