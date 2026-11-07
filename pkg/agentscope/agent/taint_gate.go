package agent

import (
	"context"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
)

// WithTaintGate (EXPERIMENTAL) guards the listed tools, named by tool name or
// by the toolkit group they belong to, once the agent is tainted
// (AgentState.Tainted, set through ApplyThreadUpdate, boundary input or
// SetTainted after the agent has read untrusted content). While tainted, a
// call to a gated tool that the permission engine would allow is turned into
// an ask: the user must confirm it through the usual confirmation path (the
// same RequireUserConfirmEvent and wait as an Ask decision). A Deny decision
// stays Deny, and an ask stays an ask.
//
// The permission mode decides whether that ask can be answered:
//   - permission.ModeDontAsk promises that no call ever waits for a user, so
//     there a gated call is denied instead, the same way the engine turns its
//     own asks into denials in that mode.
//   - In every other mode the gated call asks. ModeDefault, ModeAcceptEdits
//     and ModeExplore ask through rules and defaults already, and ModeBypass
//     still honors explicit ask rules; the gate acts like such an ask rule. A
//     host running those modes must answer confirmation requests.
//
// The gate works through the permission engine: without WithPermissionContext
// it does nothing. The loop bridge (UnifiedAgentRunner) cannot ask for
// confirmation, so it refuses gated calls while the agent is tainted, in
// every mode.
func WithTaintGate(toolNamesOrGroups ...string) AgentOption {
	return func(a *UnifiedAgent) {
		a.taintGate = append([]string(nil), toolNamesOrGroups...)
	}
}

// SetTainted sets or clears AgentState.Tainted between replies. It is a
// convenience over ApplyThreadUpdate and fails, changing nothing, while a
// reply is in flight (including a reply core still running after its stream
// closed) or a Conversation owns the agent; it also returns
// ApplyThreadUpdate's persistence error.
func (a *UnifiedAgent) SetTainted(v bool) error {
	return a.ApplyThreadUpdate(context.Background(), ThreadUpdate{Taint: &v})
}

// taintGated reports whether t is gated and the agent is tainted.
func (a *UnifiedAgent) taintGated(t tool.Tool) bool {
	if len(a.taintGate) == 0 || t == nil {
		return false
	}
	a.mu.Lock()
	tainted := a.state.Tainted
	a.mu.Unlock()
	if !tainted {
		return false
	}
	name := t.Name()
	for _, g := range a.taintGate {
		if g == name {
			return true
		}
		if a.toolkit != nil && a.toolkit.HasGroup(g) {
			for _, s := range a.toolkit.GetToolSchemasForGroups(g) {
				if s.Function.Name == name {
					return true
				}
			}
		}
	}
	return false
}

// applyTaintGate turns an Allow decision for a gated tool into an Ask while
// the agent is tainted, or into a Deny when the engine runs in ModeDontAsk
// (no user answers asks there; see WithTaintGate). No rule is suggested: the
// call is already allowed by rule, and the confirmation is about this
// tainted call only.
func (a *UnifiedAgent) applyTaintGate(t tool.Tool, d *permission.Decision) {
	if d.Behavior != permission.BehaviorAllow || !a.taintGated(t) {
		return
	}
	d.SuggestedRules = nil
	if a.engine != nil && a.engine.Context != nil && a.engine.Context.Mode == permission.ModeDontAsk {
		// The engine's own rule for this mode: an ask becomes a deny.
		d.Behavior = permission.BehaviorDeny
		d.Message = "Permission denied for " + t.Name() +
			" (dont_ask mode - the agent has read untrusted content and this call needs confirmation, user not available)"
		d.DecisionReason = "DONT_ASK mode converted the taint gate's ASK to DENY"
		return
	}
	d.Behavior = permission.BehaviorAsk
	d.Message = "the agent has read untrusted content; confirm this call to " + t.Name()
}
