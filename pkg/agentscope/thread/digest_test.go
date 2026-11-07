package thread

import (
	"reflect"
	"strings"
	"testing"
)

func TestDigestBuilderEmpty(t *testing.T) {
	var b DigestBuilder
	if d := b.Digest(); d != nil {
		t.Fatalf("empty builder gave %+v, want nil", d)
	}
	b.Add("  ", map[string]any{"path": "a"})
	if d := b.Digest(); d != nil {
		t.Fatalf("a nameless call gave %+v, want nil", d)
	}
}

func TestDigestBuilderGroups(t *testing.T) {
	var b DigestBuilder
	b.Add("Read", map[string]any{"path": "a.go"})
	b.Add("Grep", map[string]any{"pattern": "TODO", "path": "src"})
	b.Add("Read", map[string]any{"path": "a.go"})
	b.Add("Bash", map[string]any{"command": "go test ./..."})
	b.Add("Grep", map[string]any{"pattern": "FIXME", "path": "src"})
	b.Add("Edit", map[string]any{"file_path": "b.go"})
	b.Add("Edit", map[string]any{"file_path": "c.go"})
	b.Add("Read", map[string]any{"path": "a.go"})
	b.Add("Bash", map[string]any{"cmd": "go vet"})
	b.Add("Fetch", map[string]any{"url": "https://example.com"})
	b.Add("Fetch", nil)
	got := b.Digest()
	want := &ToolDigest{Calls: []ToolCallDigest{
		{Name: "Read", Target: "a.go", Count: 3},
		{Name: "Grep", Target: "src", Count: 2},
		{Name: "Bash", Target: "go", Count: 2},
		{Name: "Edit", Count: 2},
		{Name: "Fetch", Count: 2},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("digest\n got %+v\nwant %+v", got, want)
	}
	if r := got.Render(); r != "Read a.go ×3, Grep src ×2, Bash go ×2, Edit ×2, Fetch ×2" {
		t.Errorf("render %q", r)
	}
	// A digest is a snapshot; later calls do not change it.
	b.Add("Read", map[string]any{"path": "z.go"})
	if got.Calls[0].Target != "a.go" || got.Calls[0].Count != 3 {
		t.Errorf("snapshot changed: %+v", got.Calls[0])
	}
}

func TestDigestTarget(t *testing.T) {
	long := strings.Repeat("é", 100)
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"nil args", nil, ""},
		{"path first", map[string]any{"query": "q", "path": "p"}, "p"},
		{"order", map[string]any{"query": "q", "directory": "d", "dir": "dd", "uri": "u"}, "u"},
		{"file_path", map[string]any{"file_path": "f"}, "f"},
		{"file", map[string]any{"file": "f"}, "f"},
		{"filename", map[string]any{"filename": "n"}, "n"},
		{"url", map[string]any{"url": "https://x"}, "https://x"},
		{"dir", map[string]any{"dir": "d"}, "d"},
		{"directory", map[string]any{"directory": "d"}, "d"},
		{"pattern", map[string]any{"pattern": "*.go"}, "*.go"},
		{"query", map[string]any{"query": "weather"}, "weather"},
		{"empty skipped", map[string]any{"path": "  ", "file": "f"}, "f"},
		{"non-string skipped", map[string]any{"path": 3, "url": "u"}, "u"},
		{"command first word", map[string]any{"command": "  git status --short"}, "git"},
		{"cmd", map[string]any{"cmd": "ls -la"}, "ls"},
		{"blank command", map[string]any{"command": "   ", "cmd": "pwd"}, "pwd"},
		{"named argument wins over command", map[string]any{"command": "cat x", "path": "y"}, "y"},
		{"whitespace folded", map[string]any{"query": "a\nb\t c"}, "a b c"},
		{"truncated", map[string]any{"path": long}, strings.Repeat("é", 79) + "…"},
		{"no target", map[string]any{"limit": 5}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := digestTarget(tc.args); got != tc.want {
				t.Errorf("target %q, want %q", got, tc.want)
			}
		})
	}
}
