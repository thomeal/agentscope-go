package s2

func accC04() Task {
	return Task{
		ID:     "s2-c04-gradebook",
		Set:    "acceptance",
		System: system("a class gradebook"),
		Files: map[string]string{
			"go.mod": goMod,
			"book.go": `package grades

// Score is one graded piece of work, in points.
type Score struct {
	Student string
	Item    string
	Earned  int
	Max     int
}

// Book holds all the scores of a class.
type Book struct {
	scores []Score
}

// Add records a score.
func (b *Book) Add(s Score) { b.scores = append(b.scores, s) }

// Len reports how many scores are recorded.
func (b *Book) Len() int { return len(b.scores) }
`,
			"syllabus.txt": `Course syllabus, grading section

Letter grades follow the department's scale, not the usual tens:
A from 92, B from 83, C from 72, D from 60, F below that.
The scale is applied to the student's course percentage as a whole number.

Office hours are on Tuesdays. Late work goes through the course office.
`,
			"book_test.go": `package grades

import "testing"

func TestAdd(t *testing.T) {
	var b Book
	b.Add(Score{Student: "ann", Item: "q1", Earned: 1, Max: 2})
	if b.Len() != 1 {
		t.Fatalf("Len = %d", b.Len())
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package grades_test

import (
	"reflect"
	"testing"

	grades "fixture"
)

func book(scores ...grades.Score) *grades.Book {
	b := &grades.Book{}
	for _, s := range scores {
		b.Add(s)
	}
	return b
}

func on(student string, earned, possible int) grades.Score {
	return grades.Score{Student: student, Item: "final", Earned: earned, Max: possible}
}

func letter(t *testing.T, earned int) string {
	t.Helper()
	got, err := book(on("ann", earned, 100)).Letter("ann")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestStudentsSorted(t *testing.T) {
	got := book(on("cy", 1, 2), on("ann", 1, 2), on("cy", 1, 2)).Students()
	if !reflect.DeepEqual(got, []string{"ann", "cy"}) {
		t.Fatalf("Students = %v", got)
	}
}

func TestHas(t *testing.T) {
	b := book(on("ann", 1, 2))
	if !b.Has("ann") || b.Has("bo") {
		t.Fatal("Has is wrong")
	}
}

func TestTotalAdds(t *testing.T) {
	earned, possible, err := book(on("ann", 3, 10), on("ann", 4, 5), on("bo", 1, 1)).Total("ann")
	if err != nil || earned != 7 || possible != 15 {
		t.Fatalf("Total = %d, %d, %v", earned, possible, err)
	}
}

func TestTotalSkipsExcused(t *testing.T) {
	earned, possible, err := book(on("ann", 3, 10), on("ann", -1, 50)).Total("ann")
	if err != nil || earned != 3 || possible != 10 {
		t.Fatalf("Total = %d, %d, %v", earned, possible, err)
	}
}

func TestPercentExact(t *testing.T) {
	got, err := book(on("ann", 40, 80)).Percent("ann")
	if err != nil || got != 50 {
		t.Fatalf("Percent = %d, %v", got, err)
	}
}

func TestPercentRoundsHalfUp(t *testing.T) {
	cases := map[[2]int]int{{895, 1000}: 90, {894, 1000}: 89, {2, 3}: 67, {1, 8}: 13}
	for in, want := range cases {
		got, err := book(on("ann", in[0], in[1])).Percent("ann")
		if err != nil || got != want {
			t.Errorf("%d of %d: %d, %v, want %d", in[0], in[1], got, err, want)
		}
	}
}

func TestPercentSkipsExcused(t *testing.T) {
	got, err := book(on("ann", 40, 80), on("ann", -1, 20)).Percent("ann")
	if err != nil || got != 50 {
		t.Fatalf("Percent = %d, %v", got, err)
	}
}

func TestLetterExtremes(t *testing.T) {
	if got := letter(t, 100); got != "A" {
		t.Errorf("100: %q", got)
	}
	if got := letter(t, 10); got != "F" {
		t.Errorf("10: %q", got)
	}
}

func TestLetterUpperScale(t *testing.T) {
	for earned, want := range map[int]string{92: "A", 91: "B", 83: "B", 82: "C"} {
		if got := letter(t, earned); got != want {
			t.Errorf("%d: %q, want %q", earned, got, want)
		}
	}
}

func TestLetterLowerScale(t *testing.T) {
	for earned, want := range map[int]string{72: "C", 71: "D", 60: "D", 59: "F"} {
		if got := letter(t, earned); got != want {
			t.Errorf("%d: %q, want %q", earned, got, want)
		}
	}
}

func TestLetterUsesRoundedPercent(t *testing.T) {
	got, err := book(on("ann", 915, 1000)).Letter("ann")
	if err != nil || got != "A" {
		t.Fatalf("91.5%%: %q, %v", got, err)
	}
}

func TestItemAverageExact(t *testing.T) {
	b := book(on("ann", 10, 20), on("bo", 30, 60), grades.Score{Student: "ann", Item: "other", Earned: 0, Max: 9})
	got, err := b.ItemAverage("final")
	if err != nil || got != 50 {
		t.Fatalf("ItemAverage = %d, %v", got, err)
	}
}

func TestItemAverageRoundsHalfUp(t *testing.T) {
	got, err := book(on("ann", 445, 500), on("bo", 450, 500)).ItemAverage("final")
	if err != nil || got != 90 {
		t.Fatalf("ItemAverage = %d, %v", got, err)
	}
}

func TestItemAverageSkipsExcused(t *testing.T) {
	got, err := book(on("ann", 10, 20), on("bo", -1, 80)).ItemAverage("final")
	if err != nil || got != 50 {
		t.Fatalf("ItemAverage = %d, %v", got, err)
	}
}

func TestRankHighestFirst(t *testing.T) {
	got := book(on("ann", 10, 100), on("bo", 90, 100), on("cy", 50, 100)).Rank()
	if !reflect.DeepEqual(got, []string{"bo", "cy", "ann"}) {
		t.Fatalf("Rank = %v", got)
	}
}

func TestRankTiesByName(t *testing.T) {
	got := book(on("cy", 50, 100), on("ann", 50, 100)).Rank()
	if !reflect.DeepEqual(got, []string{"ann", "cy"}) {
		t.Fatalf("Rank = %v", got)
	}
}

func TestRankTiesOnRoundedPercent(t *testing.T) {
	got := book(on("zed", 90, 100), on("ann", 895, 1000)).Rank()
	if !reflect.DeepEqual(got, []string{"ann", "zed"}) {
		t.Fatalf("Rank = %v", got)
	}
}
`},
		Solution: map[string]string{
			"book.go": `package grades

import "sort"

// Score is one graded piece of work, in points.
type Score struct {
	Student string
	Item    string
	Earned  int
	Max     int
}

// Book holds all the scores of a class.
type Book struct {
	scores []Score
}

// Add records a score.
func (b *Book) Add(s Score) { b.scores = append(b.scores, s) }

// Len reports how many scores are recorded.
func (b *Book) Len() int { return len(b.scores) }

// Students returns the distinct student names in alphabetical order.
func (b *Book) Students() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range b.scores {
		if !seen[s.Student] {
			seen[s.Student] = true
			out = append(out, s.Student)
		}
	}
	sort.Strings(out)
	return out
}

// Has reports whether a student has any score.
func (b *Book) Has(student string) bool {
	for _, s := range b.scores {
		if s.Student == student {
			return true
		}
	}
	return false
}

// counts reports whether a score counts, which an excused one does not.
func counts(s Score) bool { return s.Earned != -1 }

// Total returns the points a student earned and the points possible.
func (b *Book) Total(student string) (int, int, error) {
	earned, possible := 0, 0
	for _, s := range b.scores {
		if s.Student == student && counts(s) {
			earned += s.Earned
			possible += s.Max
		}
	}
	return earned, possible, nil
}

func percent(earned, possible int) int {
	if possible == 0 {
		return 0
	}
	return (200*earned + possible) / (2 * possible)
}

// Percent returns a student's course percentage as a whole number.
func (b *Book) Percent(student string) (int, error) {
	earned, possible, err := b.Total(student)
	if err != nil {
		return 0, err
	}
	return percent(earned, possible), nil
}

// Letter returns a student's letter grade.
func (b *Book) Letter(student string) (string, error) {
	p, err := b.Percent(student)
	if err != nil {
		return "", err
	}
	switch {
	case p >= 92:
		return "A", nil
	case p >= 83:
		return "B", nil
	case p >= 72:
		return "C", nil
	case p >= 60:
		return "D", nil
	}
	return "F", nil
}

// ItemAverage returns the class percentage on one item.
func (b *Book) ItemAverage(item string) (int, error) {
	earned, possible := 0, 0
	for _, s := range b.scores {
		if s.Item == item && counts(s) {
			earned += s.Earned
			possible += s.Max
		}
	}
	return percent(earned, possible), nil
}

// Rank returns the students, highest percentage first and ties by name.
func (b *Book) Rank() []string {
	names := b.Students()
	sort.SliceStable(names, func(i, j int) bool {
		pi, _ := b.Percent(names[i])
		pj, _ := b.Percent(names[j])
		return pi > pj
	})
	return names
}
`,
		},
		Turns: script(
			cloud("How we report percentages, for whatever comes later: they're whole numbers rounded to the nearest, with a half going up, so 89.5 reports as 90. Nothing uses that yet. Add `func (b *Book) Students() []string`: the distinct student names in alphabetical order."),
			local("Something about the data from the registrar: an Earned of -1 means the student was excused from that item, and an excused item doesn't count towards either the points earned or the points possible in any figure we produce. Nothing reads scores yet. For now add `func (b *Book) Has(student string) bool`, which says whether a student has any score."),
			cloud("Read syllabus.txt and tell me what in it we'll have to implement. No code changes."),
			cloud("Add `func (b *Book) Total(student string) (int, int, error)`, which returns the points that student earned and the points possible, in that order. A student with no scores gets zeros, no error."),
			local("Add `func (b *Book) Percent(student string) (int, error)`: the student's course percentage, earned over possible, as a whole number. With nothing possible it's 0."),
			local("Add `func (b *Book) Letter(student string) (string, error)`: the student's letter grade, one of A, B, C, D or F."),
			cloud("Add `func (b *Book) ItemAverage(item string) (int, error)`: the class percentage on one item, which is the total earned across all students on it over the total possible, as a whole number. With nothing possible it's 0."),
			local("Add `func (b *Book) Rank() []string`: the students with the highest course percentage first, and those on the same percentage in alphabetical order."),
		),
		Carries: []Carry{
			{
				What:        "percentages are whole numbers with a half rounding up",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{4, 5, 6, 7},
				Tests:       []string{"TestPercentRoundsHalfUp", "TestLetterUsesRoundedPercent", "TestItemAverageRoundsHalfUp", "TestRankTiesOnRoundedPercent"},
				Absent:      []string{"round", "half", "89.5", "nearest"},
			},
			{
				What:        "an Earned of -1 is excused and counts on neither side",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{3, 4, 6},
				Tests:       []string{"TestTotalSkipsExcused", "TestPercentSkipsExcused", "TestItemAverageSkipsExcused"},
				Absent:      []string{"excused", "-1", "registrar"},
			},
			{
				What:        "letter grades use the department's scale (syllabus.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{5},
				Source:      "syllabus.txt",
				Tests:       []string{"TestLetterUpperScale", "TestLetterLowerScale"},
				Absent:      []string{"syllabus", "department", "scale", "92", "83", "72"},
			},
		},
	}
}
