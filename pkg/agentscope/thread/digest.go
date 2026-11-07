package thread

import (
	"strings"
)

// digestTargetKeys are the arguments that name what a tool acted on, in the
// order they are tried.
var digestTargetKeys = []string{
	"path", "file_path", "file", "filename", "url", "uri", "dir", "directory", "pattern", "query",
}

// digestCommandKeys are the arguments whose first word names the target
// when no digestTargetKeys argument is present.
var digestCommandKeys = []string{"command", "cmd"}

// digestTargetRunes bounds a digest target.
const digestTargetRunes = 80

// DigestBuilder collects the tool calls of one reply into a ToolDigest. The
// zero value is ready to use. It is not safe for concurrent use.
type DigestBuilder struct {
	order []string
	calls map[string]*digestCall
}

type digestCall struct {
	target string
	mixed  bool
	count  int
}

// Add records one tool call. The target is the first present, non-empty
// string argument among: path, file_path, file, filename, url, uri, dir,
// directory, pattern, query; for command or cmd, the first word. At most 80
// runes. A call with an empty name is ignored.
func (b *DigestBuilder) Add(name string, args map[string]any) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	target := digestTarget(args)
	if b.calls == nil {
		b.calls = make(map[string]*digestCall)
	}
	c, ok := b.calls[name]
	if !ok {
		b.calls[name] = &digestCall{target: target, count: 1}
		b.order = append(b.order, name)
		return
	}
	c.count++
	if c.target != target {
		c.mixed = true
	}
}

// Digest groups calls by tool name in first-seen order. Target is kept only
// when every call to that tool had the same target. Nil when nothing was
// added.
func (b *DigestBuilder) Digest() *ToolDigest {
	if len(b.order) == 0 {
		return nil
	}
	d := &ToolDigest{Calls: make([]ToolCallDigest, 0, len(b.order))}
	for _, name := range b.order {
		c := b.calls[name]
		call := ToolCallDigest{Name: name, Count: c.count}
		if !c.mixed {
			call.Target = c.target
		}
		d.Calls = append(d.Calls, call)
	}
	return d
}

func digestTarget(args map[string]any) string {
	for _, k := range digestTargetKeys {
		if s, ok := args[k].(string); ok {
			if t := oneLine(s); t != "" {
				return truncateRunes(t, digestTargetRunes)
			}
		}
	}
	for _, k := range digestCommandKeys {
		if s, ok := args[k].(string); ok {
			if f := strings.Fields(s); len(f) > 0 {
				return truncateRunes(f[0], digestTargetRunes)
			}
		}
	}
	return ""
}

// oneLine trims s and folds its whitespace runs into single spaces, so a
// target never breaks the line it is rendered on.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
