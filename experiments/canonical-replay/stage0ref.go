package main

// This file holds verbatim copies of stage 0's transport helpers, from
// experiments/stage0/lab/cloud.go on eval/final (5289a2e). That package is
// not on this branch. TestStage0CopiesAreVerbatim compares each declaration
// with the source file when the eval/final checkout is on disk.
//
// prepareResponses is applied to every outbound request in both variants: it
// is the id drop the replay needs (main's Responses adapter sends the local
// call ID as the function_call item id, which the API rejects) plus the
// stateless settings stage 0 sent. canonicalReplay is only the reference the
// lib_on fidelity check compares against; it never touches a sent request.

import (
	"bytes"
	"encoding/json"
	"strings"
)

// prepareResponses applies the stateless settings every Responses call needs.
func prepareResponses(body map[string]any) {
	body["store"] = false
	body["include"] = []any{"reasoning.encrypted_content"}
	items, _ := body["input"].([]any)
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok || m["type"] != "function_call" {
			continue
		}
		// The adapter sends a call ID as the item id. The API only accepts ids
		// starting with "fc", so calls made by the local model would get a 400
		// when a single shared history returns to the cloud. Dropping the id is
		// deterministic, which keeps the cached prefix stable.
		if id, _ := m["id"].(string); !strings.HasPrefix(id, "fc") {
			delete(m, "id")
		}
	}
}

// localCallPrefix starts the call ids the local server makes.
const localCallPrefix = "chatcmpl"

// canonicalReplay rewrites the arguments of local's calls the way the cloud writes its own:
// compact JSON with sorted keys. Sent as local wrote them, they make the cloud run away on
// its next whole-file write (prereg.md, Amendment 8). The cloud's own calls stay as they
// are, and the rewrite is deterministic, so a cached prefix stays stable. Arguments that do
// not parse stay too, because they have no canonical form.
func canonicalReplay(body map[string]any) {
	items, _ := body["input"].([]any)
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok || m["type"] != "function_call" {
			continue
		}
		if id, _ := m["call_id"].(string); !strings.HasPrefix(id, localCallPrefix) {
			continue
		}
		args, _ := m["arguments"].(string)
		if out, ok := canonicalJSON(args); ok {
			m["arguments"] = out
		}
	}
}

// canonicalJSON re-encodes one JSON value with sorted keys and no spaces. It keeps numbers
// as written and does not escape HTML, so only the form changes.
func canonicalJSON(s string) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return "", false
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return strings.TrimSuffix(out.String(), "\n"), true
}
