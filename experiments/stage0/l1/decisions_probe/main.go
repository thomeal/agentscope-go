// Command decisions_probe checks that this account can reach OpenAI Decisions through
// the repository's adapter, and how long a routing-shaped call takes. It uses synthetic
// text only. It is a protocol check, not a measure of routing accuracy.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier"
	oc "github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier/openai"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/credential"
)

var keyShape = regexp.MustCompile(`sk-[A-Za-z0-9_\-]+`)

func readKey(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "OPENAI_API_KEY="); ok {
			return strings.Trim(strings.TrimSpace(v), `"`), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no OPENAI_API_KEY in %s", path)
}

func main() {
	key, err := readKey(".env")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	client, err := oc.New(oc.Config{Credential: &credential.OpenAICredential{Key: key}, Model: "gpt-6-luna"})
	if err != nil {
		fmt.Println("constructor:", keyShape.ReplaceAllString(err.Error(), "sk-***"))
		os.Exit(1)
	}
	summary := "Owner: cloud. Summary: reviewing a synthetic platform incident; the user holds the mobile backend to access grant 9PW-7806."
	cases := []struct{ name, query string }{
		{"simple-followup", "Quick one, no tool: why should a failover be rehearsed?"},
		{"hard-reasoning", "Compare three rollback strategies for the billing cluster across consistency, downtime and blast radius, then design a migration plan with failure handling for each phase."},
		{"local-tool", "Read local record L-967 from this edge node and confirm you have read it."},
	}
	for i, c := range cases {
		state, err := json.Marshal(summary + " Latest user query: " + c.query)
		if err != nil {
			panic(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		start := time.Now()
		resp, err := client.Classify(ctx, classifier.Request{
			OperationID: fmt.Sprintf("stage0-decisions-probe-%d", i),
			State:       state,
			Questions: map[string]classifier.Question{
				"route": &classifier.ChoiceQuestion{
					Instructions: "Which assistant should handle the latest user query?",
					Choices: map[string]string{
						"cloud": "A large remote model with a very large context, best for hard reasoning.",
						"local": "A small on-device model with a narrow context, best for simple follow-ups and the only one with tools for local records.",
					},
				},
			},
		})
		cancel()
		fmt.Printf("%s latency=%s\n", c.name, time.Since(start).Round(time.Millisecond))
		if err != nil {
			fmt.Println("  error:", keyShape.ReplaceAllString(err.Error(), "sk-***"))
		}
		raw, jerr := json.Marshal(resp)
		if jerr != nil {
			fmt.Println("  marshal:", jerr)
			continue
		}
		fmt.Println(" ", keyShape.ReplaceAllString(string(raw), "sk-***"))
	}
}
