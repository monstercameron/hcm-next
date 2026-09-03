package agentsecurity

import (
	"context"
	"testing"
)

func fixture(t *testing.T, execute func(context.Context, map[string]any) (TypedResult, error)) (*Gateway, Request) {
	t.Helper()
	g, err := NewGateway([]Tool{{Name: "people.read", Capability: "people.lookup", Version: 2, Actions: []Action{ActionRead, ActionAnalyze, ActionDraft}, Cost: 2, ReadOnly: true, Schema: "people.v2", Execute: execute}})
	if err != nil {
		t.Fatal(err)
	}
	return g, Request{Delegation: Delegation{ID: "delegation-1", Tenant: "tenant-a", Purpose: "case-review", AgentID: "agent-a"}, Action: ActionRead, Tool: "people.read", Capability: "people.lookup", Version: 2, Nonce: "nonce-1", Args: map[string]any{"person_id": "p1"}, Taint: []string{"USER_DATA"}, Provenance: []string{"people-store:v2"}, Budget: Budget{MaxCost: 2}}
}

// TestTodo_AGENT_001 proves each bounded mode reaches only an approved tool
// and returns a typed result.
func TestTodo_AGENT_001(t *testing.T) {
	calls := 0
	g, req := fixture(t, func(_ context.Context, args map[string]any) (TypedResult, error) {
		calls++
		return TypedResult{Schema: "people.v2", Value: args, Validated: true, Taint: []string{"DERIVED"}, Provenance: []string{"deterministic-validator"}}, nil
	})
	for _, action := range []Action{ActionRead, ActionAnalyze, ActionDraft} {
		req.Action = action
		if _, err := g.Invoke(context.Background(), req); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
}

// TestTodo_AGENT_001_Security covers missing binding, unapproved tools,
// missing taint/provenance, budget expansion, writes, and unvalidated output.
func TestTodo_AGENT_001_Security(t *testing.T) {
	g, req := fixture(t, func(context.Context, map[string]any) (TypedResult, error) {
		return TypedResult{Schema: "people.v2", Value: "raw", Validated: true}, nil
	})
	cases := []struct {
		name   string
		mutate func(*Request)
	}{
		{"missing tenant", func(r *Request) { r.Delegation.Tenant = "" }},
		{"unknown tool", func(r *Request) { r.Tool = "write.payroll" }},
		{"capability expansion", func(r *Request) { r.Capability = "payroll.write" }},
		{"missing taint", func(r *Request) { r.Taint = nil }},
		{"missing provenance", func(r *Request) { r.Provenance = nil }},
		{"budget expansion", func(r *Request) { r.Budget.MaxCost = 1 }},
		{"raw credentials", func(r *Request) { r.RawCredentials = "secret" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := req
			tc.mutate(&bad)
			if _, err := g.Invoke(context.Background(), bad); err == nil {
				t.Fatal("expected refusal")
			}
		})
	}
	bad := req
	g, _ = fixture(t, func(context.Context, map[string]any) (TypedResult, error) {
		return TypedResult{Schema: "people.v2", Value: "raw", Validated: false}, nil
	})
	if _, err := g.Invoke(context.Background(), bad); err == nil {
		t.Fatal("unvalidated result was forwarded")
	}
}

// TestTodo_AGENT_001_Mutation ensures an agent cannot register a write tool or
// mutate the gateway's exact argument snapshot.
func TestTodo_AGENT_001_Mutation(t *testing.T) {
	if _, err := NewGateway([]Tool{{Name: "write", Capability: "write", Version: 1, Actions: []Action{ActionDraft}, Cost: 1, ReadOnly: false, Schema: "x", Execute: func(context.Context, map[string]any) (TypedResult, error) { return TypedResult{}, nil }}}); err == nil {
		t.Fatal("write tool accepted")
	}
	seen := ""
	g, req := fixture(t, func(_ context.Context, args map[string]any) (TypedResult, error) {
		seen = args["person_id"].(string)
		args["person_id"] = "tampered"
		return TypedResult{Schema: "people.v2", Value: "ok", Validated: true, Taint: []string{"DERIVED"}, Provenance: []string{"validator"}}, nil
	})
	if _, err := g.Invoke(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if seen != "p1" || req.Args["person_id"] != "p1" {
		t.Fatal("gateway leaked mutable argument state")
	}
}
