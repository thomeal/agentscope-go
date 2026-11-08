package s2

func long2C01() Task {
	return Task{
		ID:     "s2-long2-c01-read-trimming",
		Set:    "long",
		System: system("a sequencing read QC tool"),
		Files: map[string]string{
			"go.mod": goMod,
			"read.go": `package readqc

import (
	"errors"
	"strings"
)

// Read is one sequencing read from a FASTQ file.
type Read struct {
	ID   string // the header line without its leading @
	Seq  string // the bases
	Qual string // one quality character per base
}

// Len returns the number of bases in the read.
func (r Read) Len() int { return len(r.Seq) }

// ParseFASTQ reads four-line FASTQ records: a header line starting with @,
// the bases, a separator line starting with +, and the quality characters.
func ParseFASTQ(text string) ([]Read, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines)%4 != 0 {
		return nil, errors.New("truncated record")
	}
	var out []Read
	for i := 0; i < len(lines); i += 4 {
		head, plus := lines[i], lines[i+2]
		if !strings.HasPrefix(head, "@") || !strings.HasPrefix(plus, "+") {
			return nil, errors.New("bad record")
		}
		out = append(out, Read{ID: head[1:], Seq: lines[i+1], Qual: lines[i+3]})
	}
	return out, nil
}
`,
			"quality.go": `package readqc

// Quality decodes one Phred+33 quality character.
func Quality(c byte) int { return int(c) - 33 }

// QualityChar encodes a quality as a Phred+33 character, for reads we write.
func QualityChar(q int) byte { return byte(q + 33) }

// MeanQuality returns the mean quality of a read's bases, or 0 for an empty read.
func MeanQuality(r Read) float64 {
	if r.Len() == 0 {
		return 0
	}
	sum := 0
	for i := 0; i < len(r.Qual); i++ {
		sum += Quality(r.Qual[i])
	}
	return float64(sum) / float64(len(r.Qual))
}
`,
			"stats.go": `package readqc

import "sort"

// BaseCounts counts each base character across the reads.
func BaseCounts(reads []Read) map[byte]int {
	out := map[byte]int{}
	for _, r := range reads {
		for i := 0; i < len(r.Seq); i++ {
			out[r.Seq[i]]++
		}
	}
	return out
}

// MeanLength returns the mean read length, or 0 when there are no reads.
func MeanLength(reads []Read) float64 {
	if len(reads) == 0 {
		return 0
	}
	total := 0
	for _, r := range reads {
		total += r.Len()
	}
	return float64(total) / float64(len(reads))
}

// N50 returns the length L such that the reads of length L or more hold at
// least half of all the bases. It is 0 when there are no bases.
func N50(reads []Read) int {
	lengths := make([]int, len(reads))
	total := 0
	for i, r := range reads {
		lengths[i] = r.Len()
		total += r.Len()
	}
	sort.Sort(sort.Reverse(sort.IntSlice(lengths)))
	sum := 0
	for _, n := range lengths {
		sum += n
		if total > 0 && 2*sum >= total {
			return n
		}
	}
	return 0
}
`,
			"readqc_test.go": `package readqc

import "testing"

func TestParseFASTQ(t *testing.T) {
	reads, err := ParseFASTQ("@r1\nACGT\n+\nIIII\n@r2\nGG\n+\n##\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(reads) != 2 || reads[0].ID != "r1" || reads[1].Seq != "GG" || reads[1].Qual != "##" {
		t.Fatalf("got %+v", reads)
	}
}

func TestQuality(t *testing.T) {
	if Quality('I') != 40 || Quality('#') != 2 {
		t.Fatalf("Quality: %d, %d", Quality('I'), Quality('#'))
	}
}

func TestMeanQuality(t *testing.T) {
	if got := MeanQuality(Read{ID: "r1", Seq: "AC", Qual: "I+"}); got != 25 {
		t.Fatalf("MeanQuality = %v", got)
	}
}

func TestBaseCounts(t *testing.T) {
	got := BaseCounts([]Read{{Seq: "ACCA"}, {Seq: "NC"}})
	if got['A'] != 2 || got['C'] != 3 || got['N'] != 1 {
		t.Fatalf("BaseCounts = %v", got)
	}
}

func TestLengths(t *testing.T) {
	reads := []Read{{Seq: "ACGTACGT"}, {Seq: "ACG"}, {Seq: "AC"}, {Seq: "A"}}
	if got := MeanLength(reads); got != 3.5 {
		t.Errorf("MeanLength = %v", got)
	}
	if got := N50(reads); got != 8 {
		t.Errorf("N50 = %d", got)
	}
	if MeanLength(nil) != 0 || N50(nil) != 0 {
		t.Error("no reads")
	}
}

func TestQualityChar(t *testing.T) {
	if QualityChar(40) != 'I' || Quality(QualityChar(17)) != 17 {
		t.Fatalf("QualityChar(40) = %q", QualityChar(40))
	}
}
`,
			"docs/core-notes.txt": `Notes from the sequencing core

Files arrive as plain four-line FASTQ, one record after another, with the
bases in upper case. We never send gzipped files to the pipeline; the
download script unpacks them first.

Quality characters are Phred+33 on every current instrument. The exception is
the old GA-II, which still runs reprocessing jobs for us: its read IDs begin
with "HWI-" and its quality characters are Phred+64. Reads from both kinds of
instrument can turn up in the same file, because reruns are merged into the
original run's delivery.

Paired-end runs are delivered interleaved: each first mate is followed
directly by its second mate. Mate IDs are the fragment name followed by "/1"
or "/2", so "frag7/1" and "frag7/2" are the two mates of fragment frag7.

Pooled runs carry the sample barcode in the read ID, after a "#" and before
any mate suffix: "frag7#ACGTAC/1" is the first mate of fragment frag7#ACGTAC,
from the sample with barcode ACGTAC. Reads from unpooled runs have no "#" in
their IDs.

Run folders are kept for ninety days. Ask the core before deleting anything.
`,
			"docs/summary-format.txt": `QC summary format, as the core's dashboard reads it

The summary is one line per item: the name, a single tab, the value. The
items come in exactly this order:

reads_in     reads before trimming and filtering
reads_kept   reads that passed
kept_pct     reads_kept as a percentage of reads_in
bases_kept   bases in the reads that passed
gc_pct       GC percentage of the bases that passed
worst_pos    the position with the lowest mean quality among the reads that
             passed, the earliest one on a tie; a dash when none passed

Percentages are written with exactly one decimal. When reads_in is 0,
kept_pct is 0.0. Every line ends with a newline, the last one included.

The quality profile goes to the dashboard as its own text: a header line
with the fields pos, mean_q and reads, then one line per position, in order,
with the same three fields. Fields are tab-separated and mean_q has one
decimal.

Pooled runs also get a per-sample table: a header line with the fields
sample, reads_in, reads_kept and kept_pct, then one line per sample in order
of sample name, and last a line for the reads not assigned to any sample,
named unassigned. Fields are tab-separated and kept_pct is written as in the
summary.
`,
		},
		Hidden: map[string]string{hiddenName: `package readqc_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	readqc "fixture"
)

const nextera = "CTGTCTCTTATACACATCT"

func rec(id, seq, qual string) string { return "@" + id + "\n" + seq + "\n+\n" + qual + "\n" }

func good(n int) string { return strings.Repeat("ACGTTGCA", n/8+1)[:n] }

func fill(c byte, n int) string { return strings.Repeat(string(c), n) }

func read(id, seq string) readqc.Read {
	return readqc.Read{ID: id, Seq: seq, Qual: fill('I', len(seq))}
}

func phred33(qs ...int) string {
	b := make([]byte, len(qs))
	for i, q := range qs {
		b[i] = byte(q + 33)
	}
	return string(b)
}

func TestParseUsesSentinel(t *testing.T) {
	if _, err := readqc.ParseFASTQ("@r1\nACGT\n+\n"); !errors.Is(err, readqc.ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseLengthMismatch(t *testing.T) {
	if _, err := readqc.ParseFASTQ(rec("r1", "ACGT", "III")); !errors.Is(err, readqc.ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseUnknownBase(t *testing.T) {
	if _, err := readqc.ParseFASTQ(rec("r1", "ACXT", "IIII")); !errors.Is(err, readqc.ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseEmptyID(t *testing.T) {
	if _, err := readqc.ParseFASTQ(rec("", "ACGT", "IIII")); !errors.Is(err, readqc.ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
}

func TestSub(t *testing.T) {
	got := readqc.Read{ID: "r1", Seq: "ACGTA", Qual: "ABCDE"}.Sub(1, 3)
	if got != (readqc.Read{ID: "r1", Seq: "CG", Qual: "BC"}) {
		t.Fatalf("Sub = %+v", got)
	}
}

func TestFormat(t *testing.T) {
	got := readqc.Format([]readqc.Read{{ID: "r1", Seq: "AC", Qual: "II"}, {ID: "r2", Seq: "G", Qual: "#"}})
	if got != "@r1\nAC\n+\nII\n@r2\nG\n+\n#\n" {
		t.Fatalf("Format = %q", got)
	}
}

func TestCountN(t *testing.T) {
	if n := readqc.CountN(readqc.Read{ID: "r1", Seq: "NANCN", Qual: "IIIII"}); n != 3 {
		t.Fatalf("CountN = %d", n)
	}
}

func TestQualitiesCurrent(t *testing.T) {
	got := readqc.Read{ID: "M00412:7:1101", Seq: "ACG", Qual: "I#5"}.Qualities()
	if !reflect.DeepEqual(got, []int{40, 2, 20}) {
		t.Fatalf("Qualities = %v", got)
	}
}

func TestQualitiesOldInstrument(t *testing.T) {
	got := readqc.Read{ID: "HWI-ST330:4:1101", Seq: "ACG", Qual: "hBT"}.Qualities()
	if !reflect.DeepEqual(got, []int{40, 2, 20}) {
		t.Fatalf("Qualities = %v", got)
	}
}

func TestMeanQualityOldInstrument(t *testing.T) {
	if got := readqc.MeanQuality(readqc.Read{ID: "HWI-ST330:4:1102", Seq: "AC", Qual: "hT"}); got != 30 {
		t.Fatalf("MeanQuality = %v", got)
	}
}

func TestExpectedErrors(t *testing.T) {
	got := readqc.ExpectedErrors(readqc.Read{ID: "r1", Seq: "AC", Qual: phred33(10, 20)})
	if math.Abs(got-0.11) > 1e-9 {
		t.Fatalf("ExpectedErrors = %v", got)
	}
}

func TestGCPercent(t *testing.T) {
	if got := readqc.GCPercent([]readqc.Read{read("a", "GGCA"), read("b", "NNAT")}); got != 50 {
		t.Fatalf("GCPercent = %v", got)
	}
	if got := readqc.GCPercent([]readqc.Read{read("c", "NN")}); got != 0 {
		t.Fatalf("GCPercent of N's = %v", got)
	}
}

func TestLengthHistogram(t *testing.T) {
	got := readqc.LengthHistogram([]readqc.Read{read("a", "ACG"), read("b", "TTT"), read("c", "A")})
	if !reflect.DeepEqual(got, map[int]int{3: 2, 1: 1}) {
		t.Fatalf("LengthHistogram = %v", got)
	}
}

func TestProfileMeans(t *testing.T) {
	reads := []readqc.Read{
		{ID: "a", Seq: "ACG", Qual: phred33(40, 20, 10)},
		{ID: "b", Seq: "AC", Qual: phred33(10, 20)},
	}
	got := readqc.Profile(reads)
	if len(got) != 3 || got[0].Mean != 25 || got[0].Reads != 2 || got[1].Mean != 20 || got[2].Mean != 10 || got[2].Reads != 1 {
		t.Fatalf("Profile = %+v", got)
	}
}

func TestProfilePositionsFromOne(t *testing.T) {
	got := readqc.Profile([]readqc.Read{read("a", "ACGT")})
	if len(got) != 4 || got[0].Pos != 1 || got[3].Pos != 4 {
		t.Fatalf("Profile = %+v", got)
	}
}

func TestTrimQualityCut(t *testing.T) {
	q := phred33(30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 22, 22, 22, 22, 22, 30, 30, 30, 30, 30)
	if got := readqc.TrimQuality(readqc.Read{ID: "r1", Seq: good(20), Qual: q}); got.Len() != 9 || got.Seq != good(9) {
		t.Fatalf("TrimQuality = %+v", got)
	}
}

func TestTrimQualityWholeRead(t *testing.T) {
	if got := readqc.TrimQuality(readqc.Read{ID: "r1", Seq: good(10), Qual: fill('6', 10)}); got.Len() != 0 {
		t.Fatalf("TrimQuality = %+v", got)
	}
}

func TestTrimQualityKeepsGoodRead(t *testing.T) {
	r := read("r1", good(30))
	if got := readqc.TrimQuality(r); got != r {
		t.Fatalf("TrimQuality = %+v", got)
	}
}

func TestTrimQualityOldInstrument(t *testing.T) {
	r := readqc.Read{ID: "HWI-ST330:4:1103", Seq: good(12), Qual: "hhhhhhhJJJJJ"}
	if got := readqc.TrimQuality(r); got.Len() != 5 {
		t.Fatalf("TrimQuality = %+v", got)
	}
}

func TestTrimAdapterWhole(t *testing.T) {
	got := readqc.TrimAdapter(read("r1", good(20)+nextera+"ACGTA"))
	if got.Seq != good(20) || got.Qual != fill('I', 20) {
		t.Fatalf("TrimAdapter = %+v", got)
	}
}

func TestTrimAdapterPartial(t *testing.T) {
	if got := readqc.TrimAdapter(read("r1", good(24)+nextera[:7])); got.Seq != good(24) {
		t.Fatalf("TrimAdapter = %+v", got)
	}
}

func TestTrimAdapterShortTail(t *testing.T) {
	r := read("r1", good(24)+nextera[:4])
	if got := readqc.TrimAdapter(r); got != r {
		t.Fatalf("TrimAdapter = %+v", got)
	}
}

func TestTrimEnds(t *testing.T) {
	got := readqc.TrimEnds(readqc.Read{ID: "r1", Seq: "NNACGTN", Qual: "ABCDEFG"})
	if got.Seq != "ACGT" || got.Qual != "CDEF" {
		t.Fatalf("TrimEnds = %+v", got)
	}
}

func TestTrimRemovesEndN(t *testing.T) {
	if got := readqc.Trim(read("r1", "NN"+good(30)+"N")); got.Seq != good(30) {
		t.Fatalf("Trim = %+v", got)
	}
}

func TestKeepTooManyN(t *testing.T) {
	seq := good(40)
	if readqc.Keep(read("r1", seq[:12]+"NNNN"+seq[16:])) {
		t.Error("four N's kept")
	}
	if !readqc.Keep(read("r2", seq[:12]+"NNN"+seq[15:])) {
		t.Error("three N's dropped")
	}
}

func TestKeepLength(t *testing.T) {
	if readqc.Keep(read("r1", good(35))) || !readqc.Keep(read("r2", good(36))) {
		t.Fatal("wrong length bar")
	}
}

func TestFilterCounts(t *testing.T) {
	kept, dropped := readqc.Filter([]readqc.Read{read("a", good(40)), read("b", good(10)), read("c", "NN"+good(40))})
	if dropped != 1 || len(kept) != 2 || kept[0].ID != "a" || kept[1].Seq != good(40) {
		t.Fatalf("Filter = %+v, %d", kept, dropped)
	}
}

func TestFilterDropsShort(t *testing.T) {
	kept, dropped := readqc.Filter([]readqc.Read{read("a", good(33)), read("b", good(40))})
	if dropped != 1 || len(kept) != 1 || kept[0].ID != "b" {
		t.Fatalf("Filter = %+v, %d", kept, dropped)
	}
}

func TestFilterDropsAmbiguous(t *testing.T) {
	seq := good(40)
	kept, dropped := readqc.Filter([]readqc.Read{read("a", seq[:12]+"NNNNN"+seq[17:])})
	if dropped != 1 || len(kept) != 0 {
		t.Fatalf("Filter = %+v, %d", kept, dropped)
	}
}

func TestFilterPairs(t *testing.T) {
	pairs := []readqc.Pair{
		{R1: read("f1/1", good(40)), R2: read("f1/2", "N"+good(40))},
		{R1: read("f2/1", good(40)), R2: read("f2/2", good(12))},
	}
	got := readqc.FilterPairs(pairs)
	if len(got) != 1 || got[0].R1.ID != "f1/1" || got[0].R2.Seq != good(40) {
		t.Fatalf("FilterPairs = %+v", got)
	}
}

func TestDedup(t *testing.T) {
	got := readqc.Dedup([]readqc.Read{read("a", "ACGTT"), read("b", "GGGAA"), read("c", "ACGTT")})
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("Dedup = %+v", got)
	}
}

func TestReverseComplement(t *testing.T) {
	got := readqc.Read{ID: "r1", Seq: "AACGN", Qual: "ABCDE"}.ReverseComplement()
	if got.Seq != "NCGTT" || got.Qual != "EDCBA" || got.ID != "r1" {
		t.Fatalf("ReverseComplement = %+v", got)
	}
}

func TestDedupReverseComplement(t *testing.T) {
	got := readqc.Dedup([]readqc.Read{read("a", "AACGG"), read("b", "CCGTT"), read("c", "GATTA")})
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("Dedup = %+v", got)
	}
}

func TestParseInterleavedPairs(t *testing.T) {
	text := rec("frag1/1", "ACGT", "IIII") + rec("frag1/2", "TTGA", "IIII") + rec("frag2/1", "GG", "II") + rec("frag2/2", "CC", "II")
	got, err := readqc.ParseInterleaved(text)
	if err != nil || len(got) != 2 || got[0].R1.ID != "frag1/1" || got[1].R2.ID != "frag2/2" {
		t.Fatalf("ParseInterleaved = %+v, %v", got, err)
	}
}

func TestParseInterleavedStrangers(t *testing.T) {
	text := rec("frag1/1", "ACGT", "IIII") + rec("frag9/2", "TTGA", "IIII")
	if _, err := readqc.ParseInterleaved(text); !errors.Is(err, readqc.ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseInterleavedOdd(t *testing.T) {
	text := rec("frag1/1", "ACGT", "IIII") + rec("frag1/2", "TTGA", "IIII") + rec("frag2/1", "GG", "II")
	if _, err := readqc.ParseInterleaved(text); !errors.Is(err, readqc.ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
}

func TestSummarizeCounts(t *testing.T) {
	s := readqc.Summarize([]readqc.Read{read("a", good(40)), read("b", good(10)), read("c", good(38)+"NN")})
	if s.ReadsIn != 3 || s.ReadsKept != 2 || s.BasesKept != 78 || s.GC != 50 {
		t.Fatalf("Summarize = %+v", s)
	}
}

func TestSummarizeWorstPosition(t *testing.T) {
	q := []byte(fill('I', 40))
	q[2] = '5'
	reads := []readqc.Read{{ID: "a", Seq: good(40), Qual: string(q)}, {ID: "b", Seq: good(40), Qual: string(q)}}
	if s := readqc.Summarize(reads); s.WorstPos != 3 {
		t.Fatalf("WorstPos = %d", s.WorstPos)
	}
}

func TestRenderLines(t *testing.T) {
	s := readqc.Summary{ReadsIn: 3, ReadsKept: 2, BasesKept: 80, GC: 41.27, WorstPos: 7}
	want := "reads_in\t3\nreads_kept\t2\nkept_pct\t66.7\nbases_kept\t80\ngc_pct\t41.3\nworst_pos\t7\n"
	if got := s.Render(); got != want {
		t.Fatalf("Render = %q", got)
	}
}

func TestRenderNothingIn(t *testing.T) {
	want := "reads_in\t0\nreads_kept\t0\nkept_pct\t0.0\nbases_kept\t0\ngc_pct\t0.0\nworst_pos\t-\n"
	if got := (readqc.Summary{}).Render(); got != want {
		t.Fatalf("Render = %q", got)
	}
}

func TestRenderProfile(t *testing.T) {
	stats := []readqc.PositionStat{{Pos: 1, Mean: 37.4, Reads: 4}, {Pos: 2, Mean: 30, Reads: 3}}
	want := "pos\tmean_q\treads\n1\t37.4\t4\n2\t30.0\t3\n"
	if got := readqc.RenderProfile(stats); got != want {
		t.Fatalf("RenderProfile = %q", got)
	}
}

func TestQualityHistogram(t *testing.T) {
	got := readqc.QualityHistogram([]readqc.Read{{ID: "a", Seq: "ACG", Qual: "II#"}, {ID: "b", Seq: "A", Qual: "#"}})
	if !reflect.DeepEqual(got, map[int]int{40: 2, 2: 2}) {
		t.Fatalf("QualityHistogram = %v", got)
	}
}

func TestQualityHistogramOldInstrument(t *testing.T) {
	got := readqc.QualityHistogram([]readqc.Read{{ID: "HWI-ST330:4:1104", Seq: "AC", Qual: "hh"}})
	if !reflect.DeepEqual(got, map[int]int{40: 2}) {
		t.Fatalf("QualityHistogram = %v", got)
	}
}

func TestHamming(t *testing.T) {
	if readqc.Hamming("ACGT", "ACGT") != 0 || readqc.Hamming("ACGT", "TCGA") != 2 {
		t.Fatal("Hamming")
	}
}

func TestBarcode(t *testing.T) {
	for id, want := range map[string]string{"frag7#ACGTAC/1": "ACGTAC", "frag8#GGTTCA": "GGTTCA", "frag9/2": ""} {
		if got := readqc.Barcode(read(id, "ACGT")); got != want {
			t.Errorf("Barcode(%s) = %q", id, got)
		}
	}
}

func TestDemuxExact(t *testing.T) {
	samples := map[string]string{"alpha": "ACGTAC", "beta": "TTGGCC"}
	reads := []readqc.Read{read("f1#ACGTAC/1", "ACGT"), read("f2#TTGGCC/1", "ACGT"), read("f3/1", "ACGT"), read("f4#ACGTAC/1", "GGCC")}
	got, rest := readqc.Demux(reads, samples)
	a, b := got["alpha"], got["beta"]
	if len(a) != 2 || a[1].ID != "f4#ACGTAC/1" || len(b) != 1 || len(rest) != 1 || rest[0].ID != "f3/1" {
		t.Fatalf("Demux = %+v, %+v", got, rest)
	}
}

func TestDemuxOneMismatch(t *testing.T) {
	samples := map[string]string{"alpha": "ACGTAC", "beta": "TTGGCC"}
	got, rest := readqc.Demux([]readqc.Read{read("f1#ACGTAA/1", "ACGT"), read("f2#TTGGAA/1", "ACGT")}, samples)
	if len(got["alpha"]) != 1 || len(got["beta"]) != 0 || len(rest) != 1 || rest[0].ID != "f2#TTGGAA/1" {
		t.Fatalf("Demux = %+v, %+v", got, rest)
	}
}

func TestDemuxAmbiguous(t *testing.T) {
	samples := map[string]string{"alpha": "ACGTAC", "beta": "ACGTAG", "gamma": "TTGGCC"}
	got, rest := readqc.Demux([]readqc.Read{read("f1#ACGTAC/1", "ACGT"), read("f2#TTGGCC/1", "ACGT")}, samples)
	if len(got["alpha"]) != 0 || len(got["beta"]) != 0 || len(got["gamma"]) != 1 || len(rest) != 1 || rest[0].ID != "f1#ACGTAC/1" {
		t.Fatalf("Demux = %+v, %+v", got, rest)
	}
}

func TestSummarizeSamples(t *testing.T) {
	samples := map[string]string{"alpha": "ACGTAC", "beta": "TTGGCC", "gamma": "CCCCCC"}
	reads := []readqc.Read{read("f1#ACGTAC/1", good(40)), read("f2#ACGTAC/1", good(10)), read("f3#TTGGCC/1", good(40)), read("f4#GGGGGG/1", good(40))}
	got := readqc.SummarizeSamples(reads, samples)
	a, b, g, u := got["alpha"], got["beta"], got["gamma"], got["unassigned"]
	if len(got) != 4 || a.ReadsIn != 2 || a.ReadsKept != 1 || b.ReadsIn != 1 || g.ReadsIn != 0 || u.ReadsIn != 1 || u.ReadsKept != 1 {
		t.Fatalf("SummarizeSamples = %+v", got)
	}
	if _, ok := got["gamma"]; !ok {
		t.Fatal("no entry for a sample without reads")
	}
}

func TestRenderSamples(t *testing.T) {
	got := readqc.RenderSamples(map[string]readqc.Summary{
		"unassigned": {ReadsIn: 3, ReadsKept: 1},
		"beta":       {ReadsIn: 4, ReadsKept: 4},
		"alpha":      {},
	})
	want := "sample\treads_in\treads_kept\tkept_pct\nalpha\t0\t0\t0.0\nbeta\t4\t4\t100.0\nunassigned\t3\t1\t33.3\n"
	if got != want {
		t.Fatalf("RenderSamples = %q", got)
	}
}
`},
		Solution: map[string]string{
			"read.go": `package readqc

import (
	"errors"
	"strings"
)

// ErrMalformed is returned, possibly wrapped, for any input we cannot parse.
var ErrMalformed = errors.New("malformed record")

// Read is one sequencing read from a FASTQ file.
type Read struct {
	ID   string // the header line without its leading @
	Seq  string // the bases
	Qual string // one quality character per base
}

// Pair is the two mates of one paired-end fragment.
type Pair struct{ R1, R2 Read }

// Len returns the number of bases in the read.
func (r Read) Len() int { return len(r.Seq) }

// Sub returns the bases from..to-1 with their quality characters, keeping
// the ID. The indexes are ordinary Go ones.
func (r Read) Sub(from, to int) Read {
	return Read{ID: r.ID, Seq: r.Seq[from:to], Qual: r.Qual[from:to]}
}

// CountN returns how many of the read's bases are N.
func CountN(r Read) int { return strings.Count(r.Seq, "N") }

// complement maps each base to its partner on the other strand.
var complement = map[byte]byte{'A': 'T', 'T': 'A', 'C': 'G', 'G': 'C', 'N': 'N'}

// ReverseComplement returns the read as the other strand shows it: the bases
// complemented in reverse order, and the quality characters reversed.
func (r Read) ReverseComplement() Read {
	n := r.Len()
	seq, qual := make([]byte, n), make([]byte, n)
	for i := 0; i < n; i++ {
		seq[n-1-i] = complement[r.Seq[i]]
		qual[n-1-i] = r.Qual[i]
	}
	return Read{ID: r.ID, Seq: string(seq), Qual: string(qual)}
}
`,
			"fastq.go": `package readqc

import (
	"fmt"
	"strings"
)

// ParseFASTQ reads four-line FASTQ records: a header line starting with @,
// the bases, a separator line starting with +, and the quality characters.
// Every problem it finds is reported as ErrMalformed.
func ParseFASTQ(text string) ([]Read, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines)%4 != 0 {
		return nil, fmt.Errorf("%w: truncated record", ErrMalformed)
	}
	var out []Read
	for i := 0; i < len(lines); i += 4 {
		head, seq, plus, qual := lines[i], lines[i+1], lines[i+2], lines[i+3]
		n := i/4 + 1
		if !strings.HasPrefix(head, "@") || !strings.HasPrefix(plus, "+") {
			return nil, fmt.Errorf("%w: record %d has no header or separator", ErrMalformed, n)
		}
		if len(head) == 1 {
			return nil, fmt.Errorf("%w: record %d has no ID", ErrMalformed, n)
		}
		if len(seq) != len(qual) {
			return nil, fmt.Errorf("%w: record %d has %d bases and %d qualities", ErrMalformed, n, len(seq), len(qual))
		}
		if !knownBases(seq) {
			return nil, fmt.Errorf("%w: record %d has an unknown base", ErrMalformed, n)
		}
		out = append(out, Read{ID: head[1:], Seq: seq, Qual: qual})
	}
	return out, nil
}

// knownBases reports whether seq holds only A, C, G, T and N.
func knownBases(seq string) bool {
	for i := 0; i < len(seq); i++ {
		if !strings.ContainsRune("ACGTN", rune(seq[i])) {
			return false
		}
	}
	return true
}

// ParseInterleaved reads the core's paired-end files, in which each first
// mate is followed directly by its second mate, and returns the pairs in
// file order.
func ParseInterleaved(text string) ([]Pair, error) {
	reads, err := ParseFASTQ(text)
	if err != nil {
		return nil, err
	}
	if len(reads)%2 != 0 {
		return nil, fmt.Errorf("%w: %d records cannot form pairs", ErrMalformed, len(reads))
	}
	pairs := make([]Pair, 0, len(reads)/2)
	for i := 0; i < len(reads); i += 2 {
		a, b := reads[i], reads[i+1]
		if !mates(a.ID, b.ID) {
			return nil, fmt.Errorf("%w: %s and %s are not mates", ErrMalformed, a.ID, b.ID)
		}
		pairs = append(pairs, Pair{R1: a, R2: b})
	}
	return pairs, nil
}

// mates reports whether two IDs name the first and the second mate of one
// fragment: the fragment name followed by /1 and by /2.
func mates(first, second string) bool {
	name, ok := strings.CutSuffix(first, "/1")
	return ok && second == name+"/2"
}

// Format writes reads as four-line FASTQ records.
func Format(reads []Read) string {
	var b strings.Builder
	for _, r := range reads {
		fmt.Fprintf(&b, "@%s\n%s\n+\n%s\n", r.ID, r.Seq, r.Qual)
	}
	return b.String()
}
`,
			"quality.go": `package readqc

import (
	"math"
	"strings"
)

// Quality decodes one Phred+33 quality character.
func Quality(c byte) int { return int(c) - 33 }

// QualityChar encodes a quality as a Phred+33 character, for reads we write.
func QualityChar(q int) byte { return byte(q + 33) }

// offset is the quality encoding of a read: Phred+64 for the old GA-II,
// whose read IDs start with HWI-, and Phred+33 for every other instrument.
func offset(r Read) int {
	if strings.HasPrefix(r.ID, "HWI-") {
		return 64
	}
	return 33
}

// Qualities returns the decoded quality of each of the read's bases.
func (r Read) Qualities() []int {
	off := offset(r)
	out := make([]int, len(r.Qual))
	for i := 0; i < len(r.Qual); i++ {
		out[i] = int(r.Qual[i]) - off
	}
	return out
}

// MeanQuality returns the mean quality of a read's bases, or 0 for an empty read.
func MeanQuality(r Read) float64 {
	q := r.Qualities()
	if len(q) == 0 {
		return 0
	}
	sum := 0
	for _, v := range q {
		sum += v
	}
	return float64(sum) / float64(len(q))
}

// ExpectedErrors returns how many miscalled bases the read's qualities
// predict: the sum over its bases of 10^(-q/10).
func ExpectedErrors(r Read) float64 {
	sum := 0.0
	for _, q := range r.Qualities() {
		sum += math.Pow(10, -float64(q)/10)
	}
	return sum
}

// QualityHistogram returns how many bases across the reads have each quality.
func QualityHistogram(reads []Read) map[int]int {
	out := map[int]int{}
	for _, r := range reads {
		for _, q := range r.Qualities() {
			out[q]++
		}
	}
	return out
}
`,
			"trim.go": `package readqc

import "strings"

// The lab's quality trimming: the read is cut just before the first window
// of trimWindow bases whose mean quality is below trimMinMean.
const (
	trimWindow  = 5
	trimMinMean = 25
)

// adapter is the Nextera adapter our libraries are made with.
const adapter = "CTGTCTCTTATACACATCT"

// minPartial is the shortest start of the adapter cut from the end of a read.
const minPartial = 5

// TrimQuality applies the lab's quality trimming. A read with nothing to
// trim comes back unchanged.
func TrimQuality(r Read) Read {
	q := r.Qualities()
	for i := 0; i+trimWindow <= len(q); i++ {
		sum := 0
		for _, v := range q[i : i+trimWindow] {
			sum += v
		}
		if sum < trimMinMean*trimWindow {
			return r.Sub(0, i)
		}
	}
	return r
}

// TrimAdapter cuts the read where the adapter starts. When the read ends
// with the first minPartial or more bases of the adapter, those are cut.
func TrimAdapter(r Read) Read {
	if i := strings.Index(r.Seq, adapter); i >= 0 {
		return r.Sub(0, i)
	}
	for k := min(len(adapter)-1, r.Len()); k >= minPartial; k-- {
		if strings.HasSuffix(r.Seq, adapter[:k]) {
			return r.Sub(0, r.Len()-k)
		}
	}
	return r
}

// TrimEnds removes N bases from both ends of a read.
func TrimEnds(r Read) Read {
	from, to := 0, r.Len()
	for from < to && r.Seq[from] == 'N' {
		from++
	}
	for to > from && r.Seq[to-1] == 'N' {
		to--
	}
	return r.Sub(from, to)
}

// Trim applies TrimAdapter, then TrimQuality, then TrimEnds.
func Trim(r Read) Read { return TrimEnds(TrimQuality(TrimAdapter(r))) }
`,
			"filter.go": `package readqc

// What a trimmed read needs for us to keep it.
const (
	minKeptLength = 36
	maxKeptN      = 3
)

// Dedup drops every read whose sequence repeats an earlier read's, or is the
// reverse complement of one, keeping the first, in input order.
func Dedup(reads []Read) []Read {
	seen := map[string]bool{}
	var out []Read
	for _, r := range reads {
		if seen[r.Seq] {
			continue
		}
		seen[r.Seq] = true
		seen[r.ReverseComplement().Seq] = true
		out = append(out, r)
	}
	return out
}

// Keep reports whether a read that has already been through Trim is kept.
func Keep(r Read) bool {
	return r.Len() >= minKeptLength && CountN(r) <= maxKeptN
}

// Filter puts every read through Trim and returns the trimmed reads that
// are kept, in their order, and how many reads were dropped.
func Filter(reads []Read) ([]Read, int) {
	var kept []Read
	for _, r := range reads {
		if t := Trim(r); Keep(t) {
			kept = append(kept, t)
		}
	}
	return kept, len(reads) - len(kept)
}

// FilterPairs trims both reads of each pair and keeps the pair, trimmed,
// only when both of them are kept.
func FilterPairs(pairs []Pair) []Pair {
	var out []Pair
	for _, p := range pairs {
		a, b := Trim(p.R1), Trim(p.R2)
		if Keep(a) && Keep(b) {
			out = append(out, Pair{R1: a, R2: b})
		}
	}
	return out
}
`,
			"stats.go": `package readqc

import "sort"

// BaseCounts counts each base character across the reads.
func BaseCounts(reads []Read) map[byte]int {
	out := map[byte]int{}
	for _, r := range reads {
		for i := 0; i < len(r.Seq); i++ {
			out[r.Seq[i]]++
		}
	}
	return out
}

// MeanLength returns the mean read length, or 0 when there are no reads.
func MeanLength(reads []Read) float64 {
	if len(reads) == 0 {
		return 0
	}
	total := 0
	for _, r := range reads {
		total += r.Len()
	}
	return float64(total) / float64(len(reads))
}

// N50 returns the length L such that the reads of length L or more hold at
// least half of all the bases. It is 0 when there are no bases.
func N50(reads []Read) int {
	lengths := make([]int, len(reads))
	total := 0
	for i, r := range reads {
		lengths[i] = r.Len()
		total += r.Len()
	}
	sort.Sort(sort.Reverse(sort.IntSlice(lengths)))
	sum := 0
	for _, n := range lengths {
		sum += n
		if total > 0 && 2*sum >= total {
			return n
		}
	}
	return 0
}

// GCPercent returns G and C bases as a percentage of all A, C, G and T bases
// across the reads. N's count on neither side; with no such bases it is 0.
func GCPercent(reads []Read) float64 {
	counts := BaseCounts(reads)
	gc := counts['G'] + counts['C']
	all := gc + counts['A'] + counts['T']
	if all == 0 {
		return 0
	}
	return 100 * float64(gc) / float64(all)
}

// LengthHistogram returns how many reads there are of each length.
func LengthHistogram(reads []Read) map[int]int {
	out := map[int]int{}
	for _, r := range reads {
		out[r.Len()]++
	}
	return out
}

// PositionStat is the mean quality at one position of the reads, over the
// reads long enough to have that position. Pos is shown to people, so the
// first base is position 1.
type PositionStat struct {
	Pos   int
	Mean  float64
	Reads int
}

// Profile returns one PositionStat per position, in order, up to the
// longest read.
func Profile(reads []Read) []PositionStat {
	var sums, counts []int
	for _, r := range reads {
		for i, q := range r.Qualities() {
			if i == len(sums) {
				sums = append(sums, 0)
				counts = append(counts, 0)
			}
			sums[i] += q
			counts[i]++
		}
	}
	out := make([]PositionStat, len(sums))
	for i := range sums {
		out[i] = PositionStat{Pos: i + 1, Mean: float64(sums[i]) / float64(counts[i]), Reads: counts[i]}
	}
	return out
}
`,
			"demux.go": `package readqc

import "strings"

// Hamming returns the number of positions at which two equally long strings
// differ.
func Hamming(a, b string) int {
	n := 0
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}

// Barcode returns the sample barcode carried in a read's ID: what follows
// the # up to any mate suffix. It is "" for a read that has none.
func Barcode(r Read) string {
	_, code, ok := strings.Cut(r.ID, "#")
	if !ok {
		return ""
	}
	code, _, _ = strings.Cut(code, "/")
	return code
}

// sampleOf returns the one sample whose barcode is within a single base of
// code. A barcode close to no sample, or to several, belongs to none.
func sampleOf(code string, samples map[string]string) (string, bool) {
	match, found := "", 0
	for name, want := range samples {
		if len(code) == len(want) && Hamming(code, want) <= 1 {
			match = name
			found++
		}
	}
	return match, found == 1
}

// Demux sorts reads into samples by barcode. samples maps each sample's name
// to its barcode. It returns each sample's reads in input order, keyed by
// sample name, and separately the reads not assigned to any sample.
func Demux(reads []Read, samples map[string]string) (map[string][]Read, []Read) {
	out := map[string][]Read{}
	var rest []Read
	for _, r := range reads {
		if name, ok := sampleOf(Barcode(r), samples); ok {
			out[name] = append(out[name], r)
		} else {
			rest = append(rest, r)
		}
	}
	return out, rest
}
`,
			"report.go": `package readqc

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Summary holds the numbers of the QC summary the core's dashboard reads.
type Summary struct {
	ReadsIn, ReadsKept, BasesKept, WorstPos int
	GC                                      float64
}

// Summarize runs the reads through Filter and summarises the result.
func Summarize(reads []Read) Summary {
	kept, _ := Filter(reads)
	s := Summary{ReadsIn: len(reads), ReadsKept: len(kept), GC: GCPercent(kept)}
	for _, r := range kept {
		s.BasesKept += r.Len()
	}
	var worst float64
	for i, p := range Profile(kept) {
		if i == 0 || p.Mean < worst {
			s.WorstPos, worst = p.Pos, p.Mean
		}
	}
	return s
}

// field writes one line of tab-separated fields.
func field(b *strings.Builder, fields ...string) {
	b.WriteString(strings.Join(fields, "\t") + "\n")
}

// keptPct is the share of reads kept, as the dashboard prints it.
func (s Summary) keptPct() string {
	pct := 0.0
	if s.ReadsIn > 0 {
		pct = 100 * float64(s.ReadsKept) / float64(s.ReadsIn)
	}
	return fmt.Sprintf("%.1f", pct)
}

// Render writes the summary as text for the core's dashboard.
func (s Summary) Render() string {
	worst := "-"
	if s.ReadsKept > 0 && s.WorstPos > 0 {
		worst = strconv.Itoa(s.WorstPos)
	}
	var b strings.Builder
	field(&b, "reads_in", strconv.Itoa(s.ReadsIn))
	field(&b, "reads_kept", strconv.Itoa(s.ReadsKept))
	field(&b, "kept_pct", s.keptPct())
	field(&b, "bases_kept", strconv.Itoa(s.BasesKept))
	field(&b, "gc_pct", fmt.Sprintf("%.1f", s.GC))
	field(&b, "worst_pos", worst)
	return b.String()
}

// RenderProfile writes the quality profile as text for the core's dashboard.
func RenderProfile(stats []PositionStat) string {
	var b strings.Builder
	field(&b, "pos", "mean_q", "reads")
	for _, p := range stats {
		field(&b, strconv.Itoa(p.Pos), fmt.Sprintf("%.1f", p.Mean), strconv.Itoa(p.Reads))
	}
	return b.String()
}

// SummarizeSamples demultiplexes the reads and summarises each sample's
// share. Every sample has an entry, and the reads not assigned to any
// sample are summarised under "unassigned".
func SummarizeSamples(reads []Read, samples map[string]string) map[string]Summary {
	bySample, rest := Demux(reads, samples)
	out := map[string]Summary{"unassigned": Summarize(rest)}
	for name := range samples {
		out[name] = Summarize(bySample[name])
	}
	return out
}

// RenderSamples writes the per-sample table for the core's dashboard: the
// samples by name, then the unassigned reads.
func RenderSamples(summaries map[string]Summary) string {
	var names []string
	for name := range summaries {
		if name != "unassigned" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if _, ok := summaries["unassigned"]; ok {
		names = append(names, "unassigned")
	}
	var b strings.Builder
	field(&b, "sample", "reads_in", "reads_kept", "kept_pct")
	for _, name := range names {
		s := summaries[name]
		field(&b, name, strconv.Itoa(s.ReadsIn), strconv.Itoa(s.ReadsKept), s.keptPct())
	}
	return b.String()
}
`,
		},
		Turns: script(
			cloud("One house rule before anything else: a position within a read that we show people, in reports, profiles or anything printed, counts the first base as 1, the way the lab does. Indexes in code stay ordinary Go ones. To begin, add `func (r Read) Sub(from, to int) Read` to read.go: the bases from..to-1 with their quality characters, keeping the ID."),
			cloud("Add `var ErrMalformed = errors.New(\"malformed record\")` to read.go and make ParseFASTQ return it for each problem it finds, wrapped with detail if you like, as long as errors.Is matches it. From now on this is the error for any input we cannot parse."),
			local("Read docs/core-notes.txt and tell me in a few lines what it says we must handle when reading the core's files. No code changes."),
			local("In read.go, add `func Format(reads []Read) string`, which writes reads back as four-line FASTQ records: @ followed by the ID, the bases, a line holding only +, then the quality characters, every line ending in a newline."),
			local("Background for the trimming we will write later: the lab looks at each 5-base window from the start of a read and cuts the read just before the first window whose mean quality is below 25. Nothing to code for that yet. Today, add `func ExpectedErrors(r Read) float64` to quality.go: the sum over the read's bases of 10^(-q/10), q being the base's quality."),
			cloud("Background for the filter step, nothing to code for it yet: the lab keeps a trimmed read only if it has 30 or more bases and no more than 3 N's. For now, add `func CountN(r Read) int` to read.go: how many of the read's bases are N."),
			cloud("Our libraries are made with the Nextera kit, so the adapter we will be trimming is CTGTCTCTTATACACATCT. No trimming yet. Start filter.go with `func Dedup(reads []Read) []Read`: drop every read whose sequence repeats an earlier read's, keeping the first, in input order."),
			local("ParseFASTQ must also reject a record whose quality line is not exactly as long as its sequence line, and a record whose sequence holds anything other than A, C, G, T or N."),
			cloud("A change to the filter background from earlier: the aligner team now needs 36 bases in a trimmed read before we keep it, not 30. Still no filter code. Add `func LengthHistogram(reads []Read) map[int]int` to stats.go: how many reads there are of each length."),
			cloud("Read docs/summary-format.txt. Then start report.go with `type Summary struct` holding ReadsIn, ReadsKept, BasesKept and WorstPos (int) and GC (float64), the numbers that summary needs. No functions yet."),
			cloud("Background for demultiplexing, which comes later: a read belongs to a sample when its barcode differs from the sample's in at most one base, and a read that would belong to more than one sample is left unassigned rather than guessed. For now, start demux.go with `func Hamming(a, b string) int`: the number of positions at which two equally long strings differ."),
			local("Add `func GCPercent(reads []Read) float64` to stats.go: G and C bases as a percentage of all the A, C, G and T bases across the reads. N's count on neither side, and the result is 0 when there are no such bases."),
			local("In stats.go, add `type PositionStat struct { Pos int; Mean float64; Reads int }` and `func Profile(reads []Read) []PositionStat`: one entry per position, in order, up to the longest read, with the mean quality at that position over the reads long enough to have it, and how many reads that is."),
			local("Add `func QualityHistogram(reads []Read) map[int]int` to quality.go: how many bases across the reads have each quality."),
			cloud("Add `func (r Read) Qualities() []int` to quality.go: the decoded quality of each of the read's bases. Then make MeanQuality, ExpectedErrors, QualityHistogram and Profile use it instead of decoding quality characters themselves."),
			cloud("Start trim.go with `func TrimQuality(r Read) Read`, the lab's quality trimming, built on Qualities and Sub. A read with nothing to trim comes back unchanged."),
			local("In trim.go, add `func TrimAdapter(r Read) Read`: cut the read where our adapter starts. And when the read ends with the first 5 or more bases of the adapter, cut that partial adapter off."),
			local("Add `func TrimEnds(r Read) Read` to trim.go, which removes N bases from both ends of a read, and `func Trim(r Read) Read`, which applies TrimAdapter, then TrimQuality, then TrimEnds."),
			local("Add `func Keep(r Read) bool` to filter.go: whether a read that has already been through Trim is one we keep."),
			cloud("Add `func Filter(reads []Read) ([]Read, int)` to filter.go: put every read through Trim and return the trimmed reads we keep, in their order, and how many reads were dropped."),
			cloud("Add `type Pair struct { R1, R2 Read }` to read.go, and `func ParseInterleaved(text string) ([]Pair, error)` for the core's paired-end files: it returns the pairs in file order, and fails when the records do not come as proper mates."),
			local("Add `func FilterPairs(pairs []Pair) []Pair` to filter.go: trim both reads of each pair with Trim and keep the pair, trimmed, only when Keep holds for both."),
			local("read.go has grown. Move ParseFASTQ, ParseInterleaved and Format into a new file fastq.go, and leave the types and Read's methods in read.go. No behaviour changes."),
			cloud("In report.go, add `func Summarize(reads []Read) Summary`: run the reads through Filter and fill in every field from the result, taking the worst position from Profile."),
			local("Add `func (s Summary) Render() string` to report.go: the summary as text for the core's dashboard."),
			cloud("Add `func Barcode(r Read) string` to demux.go: the sample barcode carried in a read's ID, or \"\" for a read that has none."),
			local("Add `func Demux(reads []Read, samples map[string]string) (map[string][]Read, []Read)` to demux.go. samples maps each sample's name to its barcode. Return each sample's reads in input order, keyed by sample name, and separately the reads not assigned to any sample, also in input order."),
			cloud("Add `func (r Read) ReverseComplement() Read` to read.go: the bases complemented (A and T swap, C and G swap, N stays) in reverse order, the quality characters reversed, and the same ID. Then make Dedup also drop a read that is the reverse complement of an earlier one."),
			cloud("Add `func SummarizeSamples(reads []Read, samples map[string]string) map[string]Summary` to report.go: split the reads with Demux and Summarize each sample's share. Every sample gets an entry, even one with no reads, and the reads not assigned to any sample get one under the key \"unassigned\"."),
			local("Add `func RenderProfile(stats []PositionStat) string` to report.go: the quality profile as the dashboard expects it."),
			local("One more case for ParseFASTQ: a header line that is just @, with no ID after it, is invalid too."),
			local("Add `func RenderSamples(summaries map[string]Summary) string` to report.go: the per-sample table for the dashboard, from what SummarizeSamples returns."),
		),
		Carries: []Carry{
			{
				What:        "positions shown to people count the first base as 1",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{12, 23},
				Tests:       []string{"TestProfilePositionsFromOne", "TestSummarizeWorstPosition"},
				Absent:      []string{"first base as 1", "the way the lab does", "1-based", "one-based", "0-based", "zero-based", "start at 1", "starts at 1"},
			},
			{
				What:        "ErrMalformed for any input we cannot parse",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{7, 20, 30},
				Tests:       []string{"TestParseLengthMismatch", "TestParseUnknownBase", "TestParseEmptyID", "TestParseInterleavedStrangers", "TestParseInterleavedOdd"},
				Absent:      []string{"errmalformed", "malformed record", "errors.is", "sentinel"},
			},
			{
				What:        "reads whose IDs start with HWI- are Phred+64 (docs/core-notes.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{14},
				Source:      "docs/core-notes.txt",
				Tests:       []string{"TestQualitiesOldInstrument", "TestMeanQualityOldInstrument", "TestTrimQualityOldInstrument", "TestQualityHistogramOldInstrument"},
				Absent:      []string{"hwi", "+64", "ga-ii", "phred+"},
			},
			{
				What:        "quality trimming cuts before the first 5-base window with mean quality below 25",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{15},
				Tests:       []string{"TestTrimQualityCut", "TestTrimQualityWholeRead", "TestTrimQualityOldInstrument"},
				Absent:      []string{"5-base", "window", "below 25", "mean quality is below"},
			},
			{
				What:        "a trimmed read is kept only with no more than 3 N's (and 30 bases, later revised)",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{18},
				Tests:       []string{"TestKeepTooManyN", "TestFilterDropsAmbiguous"},
				Absent:      []string{"no more than 3", "3 n's", "30 or more"},
			},
			{
				What:        "the Nextera adapter CTGTCTCTTATACACATCT",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{16},
				Tests:       []string{"TestTrimAdapterWhole", "TestTrimAdapterPartial"},
				Absent:      []string{"nextera", "ctgtctc"},
			},
			{
				What:        "a kept read needs 36 bases, revised from 30",
				Kind:        CarryUser,
				Established: 8,
				Needed:      []int{18},
				Tests:       []string{"TestKeepLength", "TestFilterDropsShort"},
				Absent:      []string{"36", "aligner"},
			},
			{
				What:        "the summary, profile and per-sample text formats (docs/summary-format.txt)",
				Kind:        CarryDiscovered,
				Established: 9,
				Needed:      []int{24, 29, 31},
				Source:      "docs/summary-format.txt",
				Tests:       []string{"TestRenderLines", "TestRenderNothingIn", "TestRenderProfile", "TestRenderSamples"},
				Absent:      []string{"summary-format", "one decimal", "kept_pct", "reads_in", "worst_pos", "mean_q", "tab-separated", "a dash"},
			},
			{
				What:        "mates are the fragment name with /1 and /2 (docs/core-notes.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{20},
				Source:      "docs/core-notes.txt",
				Tests:       []string{"TestParseInterleavedPairs"},
				Absent:      []string{"/2", "fragment name", "mate ids"},
			},
			{
				What:        "a barcode within one base of exactly one sample belongs to it; ambiguous reads stay unassigned",
				Kind:        CarryUser,
				Established: 10,
				Needed:      []int{26},
				Tests:       []string{"TestDemuxOneMismatch", "TestDemuxAmbiguous"},
				Absent:      []string{"at most one", "one base", "more than one sample", "mismatch", "rather than guessed"},
			},
			{
				What:        "the barcode follows # in the read ID, before any mate suffix (docs/core-notes.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{25, 28},
				Source:      "docs/core-notes.txt",
				Tests:       []string{"TestBarcode", "TestDemuxExact", "TestDemuxOneMismatch", "TestDemuxAmbiguous", "TestSummarizeSamples"},
				Absent:      []string{"#", "pooled"},
			},
		},
	}
}
