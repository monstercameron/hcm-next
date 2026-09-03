// Package agentsecurity contains the bounded agent tool boundary.
//
// The boundary is deliberately independent of providers, transports, and
// persistence. It accepts only an already authenticated delegation and
// invokes an explicitly registered, read-only tool.
package agentsecurity

import (
	"context"
	"errors"
	"fmt"
)

type Action string

const (
	ActionRead    Action = "read"
	ActionAnalyze Action = "analyze"
	ActionDraft   Action = "draft"
)

func (a Action) valid() bool { return a == ActionRead || a == ActionAnalyze || a == ActionDraft }

// Delegation is a reference to server-held authority. Raw credentials are
// intentionally not representable here.
type Delegation struct {
	ID      string
	Tenant  string
	Purpose string
	AgentID string
	Expires int64
}

type Budget struct {
	MaxCost int
}

type Request struct {
	Delegation Delegation
	Action     Action
	Tool       string
	Capability string
	Version    uint32
	Nonce      string
	Args       map[string]any
	Taint      []string
	Provenance []string
	Budget     Budget
	// RawCredentials is accepted only to make accidental plumbing fail closed;
	// callers must use the server-held Delegation reference instead.
	RawCredentials any
}

// TypedResult is the only result that crosses the gateway. Validated must be
// set by a deterministic validator owned by the tool, never by an agent.
type TypedResult struct {
	Schema     string
	Value      any
	Validated  bool
	Taint      []string
	Provenance []string
}

type Tool struct {
	Name       string
	Capability string
	Version    uint32
	Actions    []Action
	Cost       int
	ReadOnly   bool
	Schema     string
	Execute    func(context.Context, map[string]any) (TypedResult, error)
}

type Gateway struct{ tools map[string]Tool }

func NewGateway(tools []Tool) (*Gateway, error) {
	g := &Gateway{tools: make(map[string]Tool, len(tools))}
	for _, tool := range tools {
		if tool.Name == "" || tool.Capability == "" || tool.Version == 0 || tool.Schema == "" || tool.Cost < 0 || tool.Execute == nil || !tool.ReadOnly {
			return nil, errors.New("invalid agent tool manifest")
		}
		if _, exists := g.tools[tool.Name]; exists {
			return nil, fmt.Errorf("duplicate agent tool %q", tool.Name)
		}
		for _, action := range tool.Actions {
			if !action.valid() {
				return nil, fmt.Errorf("invalid action %q", action)
			}
		}
		g.tools[tool.Name] = tool
	}
	return g, nil
}

func (g *Gateway) Invoke(ctx context.Context, req Request) (TypedResult, error) {
	if g == nil {
		return TypedResult{}, errors.New("nil agent gateway")
	}
	if req.Delegation.ID == "" || req.Delegation.Tenant == "" || req.Delegation.Purpose == "" || req.Delegation.AgentID == "" || req.Nonce == "" || req.Capability == "" || req.Version == 0 {
		return TypedResult{}, errors.New("incomplete agent delegation")
	}
	if req.RawCredentials != nil {
		return TypedResult{}, errors.New("raw credentials are forbidden")
	}
	if !req.Action.valid() {
		return TypedResult{}, fmt.Errorf("action %q is not bounded", req.Action)
	}
	if len(req.Taint) == 0 || len(req.Provenance) == 0 {
		return TypedResult{}, errors.New("taint and provenance are required")
	}
	tool, ok := g.tools[req.Tool]
	if !ok || tool.Capability != req.Capability || tool.Version != req.Version || !contains(tool.Actions, req.Action) {
		return TypedResult{}, errors.New("tool or capability is not approved")
	}
	if req.Budget.MaxCost <= 0 || tool.Cost > req.Budget.MaxCost {
		return TypedResult{}, errors.New("budget exceeded")
	}
	args := cloneArgs(req.Args)
	result, err := tool.Execute(ctx, args)
	if err != nil {
		return TypedResult{}, err
	}
	if !result.Validated || result.Schema != tool.Schema || result.Value == nil || len(result.Taint) == 0 || len(result.Provenance) == 0 {
		return TypedResult{}, errors.New("tool returned an unvalidated or untyped result")
	}
	result.Taint = append([]string(nil), result.Taint...)
	result.Provenance = append([]string(nil), result.Provenance...)
	return result, nil
}

func cloneArgs(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneArgs(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}

func contains(actions []Action, want Action) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}
