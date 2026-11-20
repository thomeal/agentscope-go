package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

// The two variants. Both build the history through a ConnectivityAwareModel; lib_on
// adds WithCanonicalReplay and nothing else.
const (
	libOff = "lib_off"
	libOn  = "lib_on"
)

var variants = []string{libOff, libOn}

const agentName = "assistant"

// errScriptDone answers a local call after the script has run out. The final call reaches
// the local model only when the cloud call failed, and that must surface as an error.
var errScriptDone = errors.New("scripted local model: no recorded response left")

// scriptedLocal answers each call with the next recorded local response.
type scriptedLocal struct {
	mu        sync.Mutex
	responses [][]message.ContentBlock
	next      int
	histLens  []int // len(msgs) at each call
}

func (s *scriptedLocal) Chat(_ context.Context, msgs []*message.Msg, _ ...model.CallOption) (*model.ChatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.histLens = append(s.histLens, len(msgs))
	if s.next >= len(s.responses) {
		return nil, errScriptDone
	}
	blocks := append([]message.ContentBlock(nil), s.responses[s.next]...)
	s.next++
	return &model.ChatResponse{Content: blocks, IsLast: true, ModelName: "scripted-local"}, nil
}

func (s *scriptedLocal) ChatStream(context.Context, []*message.Msg, ...model.CallOption) (<-chan model.ChatResponse, error) {
	return nil, errors.New("scripted local model: streaming is not used")
}

func (s *scriptedLocal) CountTokens([]*message.Msg, []model.ToolSchema) int { return 0 }

func (s *scriptedLocal) served() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next
}

// errCloudDown is what the cloud's transport returns while the local turns are produced.
var errCloudDown = errors.New("cloud forced unavailable while local turns are produced")

// gate makes the cloud unreachable until it is opened. A blocked request never leaves the
// process.
type gate struct {
	next    http.RoundTripper
	down    atomic.Bool
	blocked atomic.Int32
	passed  atomic.Int32
}

func (g *gate) RoundTrip(r *http.Request) (*http.Response, error) {
	if g.down.Load() {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		g.blocked.Add(1)
		return nil, errCloudDown
	}
	g.passed.Add(1)
	return g.next.RoundTrip(r)
}

// jobResult is what one replay produced, besides the transport's capture.
type jobResult struct {
	resp     *model.ChatResponse
	err      error
	history  []*message.Msg
	blocked  int // cloud requests the gate refused while local turns were produced
	passed   int // cloud requests the gate let through; one per job
	served   int // local responses the scripted model returned
	expected int // local responses in the recording
}

// replayOne rebuilds one recorded session through a fresh ConnectivityAwareModel and sends
// its final request to the cloud. Local turns come from the scripted local model while the
// cloud is unreachable, so the model's own local path produces (and, with canonical replay,
// marks) them. The cloud's recorded turns are spliced in as parseResponse would have
// stored them. Every block of a reply goes into one assistant message, as UnifiedAgent's
// saveToContext stores it.
func replayOne(ctx context.Context, raw []byte, variant, apiKey, baseURL string, cloudRT http.RoundTripper) jobResult {
	conv, err := convert(raw)
	if err != nil {
		return jobResult{err: fmt.Errorf("convert: %w", err)}
	}
	g := &gate{next: cloudRT}
	g.down.Store(true)
	cloud, err := model.NewOpenAIResponseModel(&model.OpenAIResponseConfig{
		APIKey:     apiKey,
		Model:      conv.model,
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Transport: g},
	})
	if err != nil {
		return jobResult{err: err}
	}
	local := &scriptedLocal{responses: conv.localResponses()}
	opts := []model.ConnectivityOption{
		model.WithFailureThreshold(1),
		model.WithRecoveryTimeout(time.Millisecond),
	}
	switch variant {
	case libOff:
	case libOn:
		opts = append(opts, model.WithCanonicalReplay())
	default:
		return jobResult{err: fmt.Errorf("unknown variant %q", variant)}
	}
	cam := model.NewConnectivityAwareModel(local, cloud, opts...)
	callOpts := []model.CallOption{model.WithTools(conv.tools)}

	res := jobResult{expected: len(local.responses)}
	hist := []*message.Msg{message.SystemMsg("system", conv.instructions)}
	appendReply := func(blocks []message.ContentBlock) {
		blocks = append([]message.ContentBlock(nil), blocks...)
		if last := hist[len(hist)-1]; last.Role == message.RoleAssistant && last.Name == agentName {
			last.Content = append(last.Content, blocks...)
			return
		}
		hist = append(hist, message.AssistantMsg(agentName, blocks))
	}
	for _, s := range conv.steps {
		switch s.kind {
		case stepUser:
			hist = append(hist, message.UserMsg("user", s.text))
		case stepResults:
			appendReply(s.blocks)
		case stepResponse:
			if s.author == authorCloud {
				appendReply(s.blocks)
				continue
			}
			before := local.served()
			resp, err := cam.Chat(ctx, hist, callOpts...)
			if err != nil {
				res.err = fmt.Errorf("local turn: %w", err)
				return res
			}
			if local.served() != before+1 {
				res.err = fmt.Errorf("local turn was not answered by the local model")
				return res
			}
			appendReply(resp.Content)
		}
	}
	res.served = local.served()
	res.blocked = int(g.blocked.Load())
	if res.served != res.expected {
		res.err = fmt.Errorf("served %d of %d local responses", res.served, res.expected)
		return res
	}

	// Restore the cloud and wait out the breaker's recovery timeout, so the final call
	// is the half-open probe.
	g.down.Store(false)
	deadline := time.Now().Add(2 * time.Second)
	for cam.ActiveModel() != "cloud" {
		if time.Now().After(deadline) {
			res.err = fmt.Errorf("breaker did not reach half-open")
			return res
		}
		time.Sleep(time.Millisecond)
	}
	res.resp, res.err = cam.Chat(ctx, hist, callOpts...)
	res.passed = int(g.passed.Load())
	res.history = hist
	if res.err == nil && res.passed != 1 {
		res.err = fmt.Errorf("final call sent %d cloud requests, want 1", res.passed)
	}
	return res
}
