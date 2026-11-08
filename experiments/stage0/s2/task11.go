package s2

func task11() Task {
	return Task{
		ID:     "s2-11-line-protocol",
		Set:    "tuning",
		System: system("a line protocol for talking to devices"),
		Files: map[string]string{
			"go.mod": goMod,
			"frame.go": `package wire

// Frame is one protocol message.
type Frame struct {
	Seq  int
	Kind string
	Body string
}

// Empty reports whether the frame has no body.
func (f Frame) Empty() bool { return f.Body == "" }
`,
			"checksum/checksum.go": `// Package checksum computes protocol checksums.
package checksum

// Sum returns the sum of the bytes of s, modulo 256.
func Sum(s string) int {
	total := 0
	for i := 0; i < len(s); i++ {
		total += int(s[i])
	}
	return total % 256
}
`,
			"checksum/checksum_test.go": `package checksum

import "testing"

func TestSum(t *testing.T) {
	if got := Sum("AB"); got != 131 {
		t.Fatalf("Sum = %d", got)
	}
}
`,
			"docs/protocol.txt": `Line protocol, revision 2

A frame is one line of text: SEQ|KIND|BODY|CK
SEQ is a decimal number. KIND is always upper case on the wire, whatever the
program calls it internally.
CK is the checksum (see package checksum) of everything before it, including the
last bar, written as exactly two upper-case hex digits.
`,
			"frame_test.go": `package wire

import "testing"

func TestEmpty(t *testing.T) {
	if !(Frame{Seq: 1}).Empty() || (Frame{Body: "x"}).Empty() {
		t.Fatal("wrong answer")
	}
}
`,
		},
		Hidden: map[string]string{
			"checksum/" + hiddenName: `package checksum_test

import (
	"testing"

	"fixture/checksum"
)

func TestSumWraps(t *testing.T) {
	if got := checksum.Sum("\xff\x02"); got != 1 {
		t.Fatalf("Sum = %d", got)
	}
}
`,
			hiddenName: `package wire_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	wire "fixture"
)

// line builds a correct wire line without using the code under test.
func line(seq int, kind, body string) string {
	s := fmt.Sprintf("%d|%s|%s|", seq, kind, body)
	sum := 0
	for i := 0; i < len(s); i++ {
		sum += int(s[i])
	}
	return s + fmt.Sprintf("%02X", sum%256)
}

func TestKnownKind(t *testing.T) {
	for kind, want := range map[string]bool{"PING": true, "data": true, "Ack": true, "NOPE": false, "": false} {
		if got := wire.KnownKind(kind); got != want {
			t.Errorf("KnownKind(%q) = %v", kind, got)
		}
	}
}

func TestEncodeFields(t *testing.T) {
	parts := strings.Split(wire.Encode(wire.Frame{Seq: 12, Kind: "DATA", Body: "hi"}), "|")
	if len(parts) != 4 || parts[0] != "12" || parts[1] != "DATA" || parts[2] != "hi" {
		t.Fatalf("parts = %q", parts)
	}
}

func TestEncodeChecksumDigits(t *testing.T) {
	for _, f := range []wire.Frame{{Seq: 1, Kind: "DATA", Body: "hi"}, {Seq: 7, Kind: "PING"}, {Seq: 33, Kind: "DATA", Body: "abc"}} {
		if got, want := wire.Encode(f), line(f.Seq, f.Kind, f.Body); got != want {
			t.Errorf("Encode(%+v) = %q, want %q", f, got, want)
		}
	}
}

func TestEncodeUpperKind(t *testing.T) {
	if got, want := wire.Encode(wire.Frame{Seq: 3, Kind: "data", Body: "x"}), line(3, "DATA", "x"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDecodeOK(t *testing.T) {
	f, err := wire.Decode(line(5, "DATA", "hello"))
	if err != nil || f != (wire.Frame{Seq: 5, Kind: "DATA", Body: "hello"}) {
		t.Fatalf("Decode = %+v, %v", f, err)
	}
}

func TestDecodeBadChecksum(t *testing.T) {
	good := line(5, "DATA", "hello")
	bad := good[:len(good)-2] + "ZZ"
	if good[len(good)-2:] != "00" {
		bad = good[:len(good)-2] + "00"
	}
	if _, err := wire.Decode(bad); !errors.Is(err, wire.ErrChecksum) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeMalformed(t *testing.T) {
	_, err := wire.Decode("nonsense")
	if err == nil || errors.Is(err, wire.ErrChecksum) {
		t.Fatalf("err = %v", err)
	}
}

func TestPing(t *testing.T) {
	parts := strings.Split(wire.Ping(9), "|")
	if len(parts) != 4 || parts[0] != "9" || parts[1] != "PING" || parts[2] != "" {
		t.Fatalf("parts = %q", parts)
	}
}

func TestAckForm(t *testing.T) {
	parts := strings.Split(wire.Ack(7), "|")
	if len(parts) != 4 || parts[0] != "0" || parts[1] != "ACK" || parts[2] != "7" {
		t.Fatalf("parts = %q", parts)
	}
}

func TestNextSeqSteps(t *testing.T) {
	if got := wire.NextSeq(5); got != 6 {
		t.Fatalf("NextSeq(5) = %d", got)
	}
}

func TestNextSeqWraps(t *testing.T) {
	if got := wire.NextSeq(999); got != 1 {
		t.Fatalf("NextSeq(999) = %d", got)
	}
}

func TestValidSeqRange(t *testing.T) {
	for seq, want := range map[int]bool{0: false, 1: true, 999: true, 1000: false, -1: false} {
		if got := wire.ValidSeq(seq); got != want {
			t.Errorf("ValidSeq(%d) = %v", seq, got)
		}
	}
}

func TestIsAckFor(t *testing.T) {
	ack := line(0, "ACK", "7")
	if !wire.IsAckFor(ack, 7) {
		t.Error("the acknowledgement of 7 was not recognised")
	}
	if wire.IsAckFor(ack, 8) {
		t.Error("the acknowledgement of 7 matched 8")
	}
	if wire.IsAckFor(line(7, "ACK", ""), 7) {
		t.Error("a frame numbered 7 with no body matched 7")
	}
}

func TestIsAckForRejectsOthers(t *testing.T) {
	if wire.IsAckFor("nonsense", 7) {
		t.Error("garbage matched")
	}
	if wire.IsAckFor(line(0, "DATA", "7"), 7) {
		t.Error("a data frame matched")
	}
	good := line(0, "ACK", "7")
	if wire.IsAckFor(good[:len(good)-2]+"ZZ", 7) {
		t.Error("a bad checksum matched")
	}
}
`,
		},
		Solution: map[string]string{
			"frame.go": `package wire

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"fixture/checksum"
)

// ErrChecksum reports a frame whose checksum does not match.
var ErrChecksum = errors.New("bad checksum")

// Frame is one protocol message.
type Frame struct {
	Seq  int
	Kind string
	Body string
}

// Empty reports whether the frame has no body.
func (f Frame) Empty() bool { return f.Body == "" }

// KnownKind reports whether kind is a protocol kind, ignoring letter case.
func KnownKind(kind string) bool {
	switch strings.ToUpper(kind) {
	case "PING", "DATA", "ACK":
		return true
	}
	return false
}

func withChecksum(s string) string {
	return s + fmt.Sprintf("%02X", checksum.Sum(s))
}

// Encode produces the frame's line for the wire.
func Encode(f Frame) string {
	kind := strings.ToUpper(f.Kind)
	return withChecksum(fmt.Sprintf("%d|%s|%s|", f.Seq, kind, f.Body))
}

// Decode parses a wire line.
func Decode(line string) (Frame, error) {
	parts := strings.Split(line, "|")
	if len(parts) != 4 {
		return Frame{}, errors.New("malformed frame")
	}
	seq, err := strconv.Atoi(parts[0])
	if err != nil {
		return Frame{}, errors.New("malformed frame")
	}
	if withChecksum(line[:strings.LastIndex(line, "|")+1]) != line {
		return Frame{}, ErrChecksum
	}
	return Frame{Seq: seq, Kind: parts[1], Body: parts[2]}, nil
}

// Ping returns the wire line for a PING frame with an empty body.
func Ping(seq int) string { return Encode(Frame{Seq: seq, Kind: "PING"}) }

// Ack returns the wire line acknowledging sequence number seq.
func Ack(seq int) string {
	return Encode(Frame{Seq: 0, Kind: "ACK", Body: strconv.Itoa(seq)})
}

// NextSeq returns the sequence number that follows seq.
func NextSeq(seq int) int {
	if seq >= 999 {
		return 1
	}
	return seq + 1
}

// ValidSeq reports whether seq is legal for a data frame.
func ValidSeq(seq int) bool { return seq >= 1 && seq <= 999 }

// IsAckFor reports whether line is a valid acknowledgement of seq.
func IsAckFor(line string, seq int) bool {
	f, err := Decode(line)
	if err != nil || f.Kind != "ACK" {
		return false
	}
	return f.Seq == 0 && f.Body == strconv.Itoa(seq)
}
`,
		},
		Turns: script(
			cloud("Read docs/protocol.txt and tell me the frame layout and how the checksum works. No code changes."),
			cloud("We also agreed with the device team that sequence numbers run from 1 to 999, and the number after 999 is 1. Nothing uses that yet. For now add `func KnownKind(kind string) bool`: true for PING, DATA and ACK, ignoring letter case."),
			local("Add `func Encode(f Frame) string`, which produces the frame's line for the wire, without a trailing newline."),
			local("Add `var ErrChecksum = errors.New(\"bad checksum\")` and `func Decode(line string) (Frame, error)`. A wrong checksum gives ErrChecksum, and any other malformed line gives some other error."),
			local("The device team told me ACK frames are special: their SEQ field is always 0 and the body holds the acknowledged number in decimal. We'll code that soon. For now add `func Ping(seq int) string`, the wire line for a PING frame with an empty body."),
			cloud("Add `func Ack(seq int) string`, the wire line acknowledging sequence number seq."),
			local("Add `func NextSeq(seq int) int`, the sequence number that follows seq."),
			cloud("Add `func ValidSeq(seq int) bool`: whether seq is a legal sequence number for a data frame."),
			local("Add `func IsAckFor(line string, seq int) bool`: whether a wire line is a valid acknowledgement of seq."),
		),
		Carries: []Carry{
			{
				What:        "kinds are upper case on the wire and the checksum is two upper-case hex digits (docs/protocol.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{2},
				Source:      "docs/protocol.txt",
				Tests:       []string{"TestEncodeChecksumDigits", "TestEncodeUpperKind"},
				Absent:      []string{"upper", "hex", "seq|kind"},
			},
			{
				What:        "sequence numbers run from 1 to 999 and wrap to 1",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{6, 7},
				Tests:       []string{"TestNextSeqWraps", "TestValidSeqRange"},
				Absent:      []string{"999", "wrap", "from 1"},
			},
			{
				What:        "ACK frames have SEQ 0 and carry the acknowledged number in the body",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{5, 8},
				Tests:       []string{"TestAckForm", "TestIsAckFor"},
				Absent:      []string{"always 0", "seq field", "the body holds"},
			},
		},
	}
}
