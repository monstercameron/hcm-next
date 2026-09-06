package agentsecurity

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestAgent001_NewGatewayManifestValidation(t *testing.T) {
	validExecute := func(context.Context, map[string]any) (TypedResult, error) {
		return TypedResult{Schema: "schema", Value: "value", Validated: true, Taint: []string{"taint"}, Provenance: []string{"source"}}, nil
	}
	valid := Tool{Name: "tool", Capability: "capability", Version: 1, Actions: []Action{ActionRead}, Cost: 1, ReadOnly: true, Schema: "schema", Execute: validExecute}
	if g, err := NewGateway([]Tool{valid}); err != nil || g == nil {
		t.Fatalf("valid NewGateway = %v, %v", g, err)
	}
	cases := []struct {
		name   string
		mutate func(*Tool)
	}{
		{"name", func(v *Tool) { v.Name = "" }},
		{"capability", func(v *Tool) { v.Capability = "" }},
		{"version", func(v *Tool) { v.Version = 0 }},
		{"schema", func(v *Tool) { v.Schema = "" }},
		{"negative cost", func(v *Tool) { v.Cost = -1 }},
		{"nil execute", func(v *Tool) { v.Execute = nil }},
		{"write tool", func(v *Tool) { v.ReadOnly = false }},
		{"invalid action", func(v *Tool) { v.Actions = []Action{"write"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := valid
			tc.mutate(&bad)
			if _, err := NewGateway([]Tool{bad}); err == nil {
				t.Fatal("invalid tool manifest was accepted")
			}
		})
	}
	duplicate := valid
	duplicate.Name = valid.Name
	if _, err := NewGateway([]Tool{valid, duplicate}); err == nil {
		t.Fatal("duplicate tool was accepted")
	}
	if _, err := NewGateway(nil); err != nil {
		t.Fatalf("empty tool set = %v", err)
	}
}

func TestAgent001_InvokeRefusesEveryBindingExpansion(t *testing.T) {
	called := 0
	g, request := fixture(t, func(_ context.Context, args map[string]any) (TypedResult, error) {
		called++
		return TypedResult{Schema: "people.v2", Value: args, Validated: true, Taint: []string{"derived"}, Provenance: []string{"validator"}}, nil
	})
	if _, err := (*Gateway)(nil).Invoke(context.Background(), request); err == nil {
		t.Fatal("nil gateway was accepted")
	}
	cases := []struct {
		name   string
		mutate func(*Request)
	}{
		{"delegation id", func(v *Request) { v.Delegation.ID = "" }},
		{"agent id", func(v *Request) { v.Delegation.AgentID = "" }},
		{"nonce", func(v *Request) { v.Nonce = "" }},
		{"capability", func(v *Request) { v.Capability = "" }},
		{"version", func(v *Request) { v.Version = 0 }},
		{"raw credentials", func(v *Request) { v.RawCredentials = "secret" }},
		{"invalid action", func(v *Request) { v.Action = "write" }},
		{"missing taint", func(v *Request) { v.Taint = nil }},
		{"missing provenance", func(v *Request) { v.Provenance = nil }},
		{"unknown tool", func(v *Request) { v.Tool = "unknown" }},
		{"budget", func(v *Request) { v.Budget.MaxCost = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := request
			tc.mutate(&bad)
			if _, err := g.Invoke(context.Background(), bad); err == nil {
				t.Fatal("invalid request was accepted")
			}
		})
	}
	for _, action := range []Action{ActionRead, ActionAnalyze, ActionDraft} {
		good := request
		good.Action = action
		if _, err := g.Invoke(context.Background(), good); err != nil {
			t.Fatalf("valid action %q = %v", action, err)
		}
	}
	if called != 3 {
		t.Fatalf("execute calls=%d, want 3 after refusing invalid calls", called)
	}
}

func TestAgent001_InvokeOutputAndCloneBranches(t *testing.T) {
	args := map[string]any{"nested": map[string]any{"list": []any{"original"}}}
	seen := ""
	g, request := fixture(t, func(_ context.Context, got map[string]any) (TypedResult, error) {
		seen = got["nested"].(map[string]any)["list"].([]any)[0].(string)
		got["nested"].(map[string]any)["list"].([]any)[0] = "changed"
		return TypedResult{Schema: "people.v2", Value: "ok", Validated: true, Taint: []string{"result"}, Provenance: []string{"validator"}}, nil
	})
	request.Args = args
	if _, err := g.Invoke(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if seen != "original" || args["nested"].(map[string]any)["list"].([]any)[0] != "original" {
		t.Fatal("nested argument map was not cloned")
	}
	errorGateway, request := fixture(t, func(context.Context, map[string]any) (TypedResult, error) {
		return TypedResult{}, errors.New("tool failed")
	})
	if _, err := errorGateway.Invoke(context.Background(), request); err == nil || err.Error() != "tool failed" {
		t.Fatalf("tool error = %v", err)
	}
	for name, result := range map[string]TypedResult{
		"invalid":    {Schema: "people.v2", Value: "x", Validated: false, Taint: []string{"x"}, Provenance: []string{"p"}},
		"schema":     {Schema: "wrong", Value: "x", Validated: true, Taint: []string{"x"}, Provenance: []string{"p"}},
		"nil value":  {Schema: "people.v2", Validated: true, Taint: []string{"x"}, Provenance: []string{"p"}},
		"taint":      {Schema: "people.v2", Value: "x", Validated: true, Provenance: []string{"p"}},
		"provenance": {Schema: "people.v2", Value: "x", Validated: true, Taint: []string{"x"}},
	} {
		t.Run(name, func(t *testing.T) {
			badGateway, badRequest := fixture(t, func(context.Context, map[string]any) (TypedResult, error) { return result, nil })
			if _, err := badGateway.Invoke(context.Background(), badRequest); err == nil {
				t.Fatal("invalid output was forwarded")
			}
		})
	}
	if cloneArgs(nil) != nil || !reflect.DeepEqual(cloneArgs(map[string]any{"x": []any{map[string]any{"y": 1}}}), map[string]any{"x": []any{map[string]any{"y": 1}}}) {
		t.Fatal("cloneArgs did not preserve nested values")
	}
	if contains([]Action{ActionRead}, ActionDraft) || !contains([]Action{ActionRead}, ActionRead) {
		t.Fatal("contains action behavior is incorrect")
	}
}
