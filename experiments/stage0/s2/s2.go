// Package s2 is the second evaluation workload of experiment stage 0: fifteen
// small multi-turn coding tasks on fixture Go modules, served in alternation by
// a large-window assistant and a small-window one sharing one workspace.
//
// What each task measures is whether the code is right at the end, which
// depends on something established in an earlier turn and needed in a later one
// on the other side. The later turn never restates it. The workload assumes
// nothing about how a strategy carries context: everything it knows about a
// session is in the fields of Task, and hidden tests decide the score.
//
// All fixture text lives in Go raw string literals, which the language defines
// to be free of carriage returns, so Hash does not depend on checkout line
// endings.
package s2

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

// Turn is one scripted user message and the side that owns it. Index is the
// turn's zero-based position in Task.Turns.
type Turn struct {
	Index int
	Owner corpus.Side
	User  string
}

// Task is one fixture module and its script.
type Task struct {
	ID       string
	Set      string // "tuning" for all 15
	System   string
	Files    map[string]string // initial workspace
	Hidden   map[string]string // hidden test files, never exposed through tools
	Solution map[string]string // files that, placed over Files, make every hidden test pass
	Turns    []Turn

	// Carries lists what the script expects to be carried across turns, so that
	// results can be broken down per constraint using Result.Failed. It is
	// analysis metadata; nothing in it may reach an assistant.
	Carries []Carry
}

// The ways a carried constraint can be available when it is needed.
const (
	// CarryUser is stated by the user in chat. When it is first needed, it is in
	// no workspace file unless an assistant chose to write it down. Once a turn
	// has applied it, later turns may be able to read it back from the code.
	CarryUser = "user"
	// CarryEmbodied is stated or decided in chat and is visible in the code by the
	// time it is first needed, provided an earlier turn was done correctly.
	CarryEmbodied = "embodied"
	// CarryDiscovered is in a workspace file from the start, and an earlier turn
	// asks one side to read that file.
	CarryDiscovered = "discovered"
)

// Carry describes one constraint established early and needed later.
type Carry struct {
	What        string
	Kind        string
	Established int      // Turn.Index that establishes it
	Needed      []int    // later Turn.Index values that need it
	Source      string   // for CarryDiscovered, the workspace file holding it
	Tests       []string // hidden test functions that check it, as named in Result.Failed
	Absent      []string // lower-case phrases that no turn after Established may contain
}

// Tasks returns the workload in a stable order. Each call builds fresh values,
// so a caller may modify what it receives.
func Tasks() []Task {
	return []Task{
		task01(), task02(), task03(), task04(), task05(),
		task06(), task07(), task08(), task09(), task10(),
		task11(), task12(), task13(), task14(), task15(),
	}
}

// Hash returns the SHA-256 of the canonical JSON of Tasks, for freezing the
// workload. encoding/json writes map keys in sorted order, which makes the
// encoding canonical given the stable order of Tasks.
func Hash() string { return hashTasks(Tasks()) }

func hashTasks(tasks []Task) string {
	data, err := json.Marshal(tasks)
	if err != nil {
		panic("s2: task data cannot be encoded: " + err.Error()) // only strings, ints and slices
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ToolSpec describes one workspace tool.
type ToolSpec struct {
	Name, Description string
	Schema            json.RawMessage
}

// ToolSpecs returns the tools a Workspace dispatches through Call.
func ToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			Name:        "list_files",
			Description: "List the files in the workspace, one relative path per line.",
			Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		{
			Name:        "read_file",
			Description: "Return the contents of one workspace file.",
			Schema: json.RawMessage(`{"type":"object","properties":{` +
				`"path":{"type":"string","description":"Path relative to the workspace root, with forward slashes."}` +
				`},"required":["path"],"additionalProperties":false}`),
		},
		{
			Name:        "write_file",
			Description: "Create a workspace file or replace its whole contents. Missing directories are created. A file that already exists must be read first, and read again if it may have changed since.",
			Schema: json.RawMessage(`{"type":"object","properties":{` +
				`"path":{"type":"string","description":"Path relative to the workspace root, with forward slashes."},` +
				`"content":{"type":"string","description":"The complete new contents of the file."}` +
				`},"required":["path","content"],"additionalProperties":false}`),
		},
		{
			Name:        "run_tests",
			Description: "Run the tests in the workspace with go test and return the output, shortened if it is long.",
			Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
	}
}

// HiddenName is the file the hidden tests are written to when a task is scored.
const HiddenName = "zz_hidden_test.go"

const (
	goMod = "module fixture\n\ngo 1.25\n"

	// hiddenName is the same for every task, so the name says nothing about one.
	hiddenName = HiddenName
)

// system returns a task's system prompt. It describes the workspace and says
// that earlier instructions remain binding, and does not mention any particular
// way of carrying them.
func system(project string) string {
	return "You are a coding assistant working on " + project + ", a small Go module in a workspace " +
		"you reach through tools: list_files, read_file, write_file (which replaces a whole file) and " +
		"run_tests. The module uses only the standard library and go.mod is fixed. Use exactly the names " +
		"and signatures the user asks for. Anything the user has told you in this session stays in force " +
		"until they change it. When the work for a turn is done, reply briefly."
}

func cloud(user string) Turn { return Turn{Owner: corpus.SideCloud, User: user} }
func local(user string) Turn { return Turn{Owner: corpus.SideLocal, User: user} }

// script numbers a task's turns.
func script(turns ...Turn) []Turn {
	for i := range turns {
		turns[i].Index = i
	}
	return turns
}
