package agentsecurity

import (
	"strings"
	"testing"
)

func testToolGateway(t *testing.T) (*ToolGateway, ToolCall) {
	t.Helper()
	g, err := NewToolGateway([]ToolDescriptor{{
		Name: "people.lookup", Capability: "people.read", Version: 3, Class: ToolRead,
		DataScope: []string{"people.basic"}, Cost: 2, Schema: "people.v3",
		Validate: func(value any) (TypedResult, error) {
			return TypedResult{Schema: "people.v3", Value: value, Validated: true, Taint: []string{"USER_DATA", "DERIVED"}, Provenance: []string{"case.file", "people.store", "validator.v1"}}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"person_id": "p-1", "fields": []any{"name"}}
	digest, err := DigestArguments(args)
	if err != nil {
		t.Fatal(err)
	}
	call := ToolCall{
		Agent:      AgentIdentity{Identity: "wlid:agent", AgentID: "agent-1", Tenant: "tenant-a", Purpose: "case-review", ToolSet: []string{"people.lookup"}, DataScope: []string{"people.basic"}, Budget: 5},
		Delegation: []DelegationLink{{GrantID: "grant-1", Delegator: "root", Delegate: "agent-1", Tenant: "tenant-a", Purpose: "case-review", ToolSet: []string{"people.lookup"}, DataScope: []string{"people.basic"}, Budget: 4}},
		Tenant:     "tenant-a", Purpose: "case-review", Tool: "people.lookup", Capability: "people.read", Version: 3, Nonce: "nonce-1", Args: args, ArgsDigest: digest, InputTaint: []string{"USER_DATA"}, Provenance: []string{"case.file"}, CostBudget: 3, DataScope: []string{"people.basic"},
	}
	return g, call
}

func TestToolGatewayAdmitsBoundedReadAndValidatesTypedOutput(t *testing.T) {
	g, call := testToolGateway(t)
	result, err := g.Invoke(call, map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Validated || result.Schema != "people.v3" || !containsString(result.Taint, "USER_DATA") || !containsString(result.Provenance, "case.file") {
		t.Fatalf("unexpected typed result: %#v", result)
	}
	if explanation := g.Explain(call); strings.Contains(explanation, "Ada") || strings.Contains(explanation, "person_id") {
		t.Fatalf("explanation leaked payload: %s", explanation)
	}
}

func TestToolGatewayRefusesAuthorityExpansionAndNamesField(t *testing.T) {
	g, call := testToolGateway(t)
	call.Delegation[0].DataScope = []string{"people.basic", "people.salary"}
	_, err := g.Admit(call)
	var refusalErr *Refusal
	if err == nil || !asRefusal(err, &refusalErr) || refusalErr.Code != RefusalAuthorityExpansion || refusalErr.Field != "delegation[0].data_scope" {
		t.Fatalf("got %v, want data-scope expansion refusal", err)
	}
}

func TestToolGatewaySecurityRefusesCredentialWriteAndInvalidOutput(t *testing.T) {
	g, call := testToolGateway(t)
	call.Args = map[string]any{"person_id": "p-1", "access_token": "secret"}
	_, err := DigestArguments(call.Args)
	if err != nil {
		t.Fatal(err)
	}
	call.ArgsDigest, _ = DigestArguments(call.Args)
	if _, err := g.Admit(call); refusalCode(err) != RefusalCredential {
		t.Fatalf("credential was not refused: %v", err)
	}
	call, _ = validCallForTest(t, g)
	call.CostBudget = 1
	if _, err := g.Admit(call); refusalCode(err) != RefusalBudget {
		t.Fatalf("budget exhaustion was not typed: %v", err)
	}
	call, _ = validCallForTest(t, g)
	invalidGateway, err := NewToolGateway([]ToolDescriptor{{Name: "people.lookup", Capability: "people.read", Version: 3, Class: ToolRead, DataScope: []string{"people.basic"}, Cost: 2, Schema: "people.v3", Validate: func(any) (TypedResult, error) {
		return TypedResult{Schema: "people.v3", Value: "unvalidated", Validated: false}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalidGateway.Invoke(call, "unvalidated"); refusalCode(err) != RefusalOutput {
		t.Fatalf("unvalidated output was not refused: %v", err)
	}
	writeGateway, err := NewToolGateway([]ToolDescriptor{{Name: "payroll.write", Capability: "payroll.write", Version: 1, Class: ToolWrite, Cost: 1, Schema: "write.v1", Validate: func(any) (TypedResult, error) {
		t.Fatal("validator called for refused write")
		return TypedResult{}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	writeCall := call
	writeCall.Tool, writeCall.Capability, writeCall.Version = "payroll.write", "payroll.write", 1
	if _, err := writeGateway.Admit(writeCall); refusalCode(err) != RefusalEffectClass {
		t.Fatalf("write class was not refused: %v", err)
	}
}

func validCallForTest(t *testing.T, g *ToolGateway) (ToolCall, Admission) {
	_, call := testToolGateway(t)
	argsDigest, _ := DigestArguments(call.Args)
	call.ArgsDigest = argsDigest
	admission, err := g.Admit(call)
	if err != nil {
		t.Fatal(err)
	}
	return call, admission
}

func asRefusal(err error, target **Refusal) bool {
	value, ok := err.(*Refusal)
	if ok {
		*target = value
	}
	return ok
}

func refusalCode(err error) RefusalCode {
	if value, ok := err.(*Refusal); ok {
		return value.Code
	}
	return ""
}
