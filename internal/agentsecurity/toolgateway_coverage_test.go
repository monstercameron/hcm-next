package agentsecurity

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func refusalMust(t *testing.T, err error, code RefusalCode) *Refusal {
	t.Helper()
	var refusalErr *Refusal
	if err == nil || !asRefusal(err, &refusalErr) || refusalErr.Code != code {
		t.Fatalf("error = %v, want refusal code %s", err, code)
	}
	return refusalErr
}

func cloneToolCallForTest(call ToolCall) ToolCall {
	call.Agent.ToolSet = append([]string(nil), call.Agent.ToolSet...)
	call.Agent.DataScope = append([]string(nil), call.Agent.DataScope...)
	call.Delegation = append([]DelegationLink(nil), call.Delegation...)
	for i := range call.Delegation {
		call.Delegation[i].ToolSet = append([]string(nil), call.Delegation[i].ToolSet...)
		call.Delegation[i].DataScope = append([]string(nil), call.Delegation[i].DataScope...)
	}
	call.InputTaint = append([]string(nil), call.InputTaint...)
	call.Provenance = append([]string(nil), call.Provenance...)
	call.DataScope = append([]string(nil), call.DataScope...)
	return call
}

func TestToolGateway_DescriptorAndRefusalSurface(t *testing.T) {
	for _, class := range []ToolClass{ToolRead, ToolAnalyze, ToolDraft, ToolWrite, ToolSend, ToolExecute} {
		if !class.valid() {
			t.Errorf("valid class %q rejected", class)
		}
	}
	if ToolClass("UNKNOWN").valid() || ToolWrite.admitted() || ToolSend.admitted() || ToolExecute.admitted() || !ToolRead.admitted() {
		t.Fatal("tool class admission vocabulary is incorrect")
	}
	valid := ToolDescriptor{Name: "tool", Capability: "cap", Version: 1, Class: ToolRead, DataScope: []string{"scope"}, Cost: 1, Schema: "schema", Validate: func(value any) (TypedResult, error) {
		return TypedResult{Schema: "schema", Value: value, Validated: true, Taint: []string{"t"}, Provenance: []string{"p"}}, nil
	}}
	g, err := NewToolGateway([]ToolDescriptor{valid})
	if err != nil || g == nil {
		t.Fatalf("valid NewToolGateway = %v, %v", g, err)
	}
	valid.DataScope[0] = "caller-mutated"
	call := ToolCall{Agent: AgentIdentity{Identity: "id", AgentID: "agent", Tenant: "tenant", Purpose: "purpose", ToolSet: []string{"tool"}, DataScope: []string{"scope"}, Budget: 2}, Delegation: []DelegationLink{{GrantID: "grant", Delegator: "root", Delegate: "agent", Tenant: "tenant", Purpose: "purpose", ToolSet: []string{"tool"}, DataScope: []string{"scope"}, Budget: 2}}, Tenant: "tenant", Purpose: "purpose", Tool: "tool", Capability: "cap", Version: 1, Nonce: "nonce", InputTaint: []string{"t"}, Provenance: []string{"p"}, CostBudget: 1, DataScope: []string{"scope"}}
	call.ArgsDigest, err = DigestArguments(call.Args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Admit(call); err != nil {
		t.Fatalf("descriptor DataScope was aliased: %v", err)
	}
	if _, err := NewToolGateway([]ToolDescriptor{valid, valid}); refusalMust(t, err, RefusalInvalid).Field != "tool_descriptor.name" {
		t.Fatal("duplicate descriptor field was not identified")
	}
	invalids := []ToolDescriptor{
		{Name: "", Capability: "cap", Version: 1, Class: ToolRead, Cost: 1, Schema: "s", Validate: valid.Validate},
		{Name: "x", Capability: " ", Version: 1, Class: ToolRead, Cost: 1, Schema: "s", Validate: valid.Validate},
		{Name: "x", Capability: "cap", Class: ToolRead, Cost: 1, Schema: "s", Validate: valid.Validate},
		{Name: "x", Capability: "cap", Version: 1, Class: ToolClass("bad"), Cost: 1, Schema: "s", Validate: valid.Validate},
		{Name: "x", Capability: "cap", Version: 1, Class: ToolRead, Cost: 0, Schema: "s", Validate: valid.Validate},
		{Name: "x", Capability: "cap", Version: 1, Class: ToolRead, Cost: 1, Schema: "", Validate: valid.Validate},
		{Name: "x", Capability: "cap", Version: 1, Class: ToolRead, Cost: 1, Schema: "s"},
	}
	for i, descriptor := range invalids {
		if _, err := NewToolGateway([]ToolDescriptor{descriptor}); !errors.As(err, new(*Refusal)) {
			t.Errorf("invalid descriptor %d = %v", i, err)
		}
	}
	var nilRefusal *Refusal
	if nilRefusal.Error() != "<nil>" || nilRefusal.Explain() != "<nil>" {
		t.Fatal("nil refusal rendering changed")
	}
	refusalErr := &Refusal{Code: RefusalInvalid, Detail: "detail"}
	if refusalErr.Error() != "AGENT_INVALID_REQUEST: detail" || refusalErr.Explain() != refusalErr.Error() {
		t.Fatalf("refusal rendering = %q / %q", refusalErr.Error(), refusalErr.Explain())
	}
	refusalErr.Field = "field"
	if !strings.Contains(refusalErr.Error(), "field field") {
		t.Fatalf("field refusal rendering = %q", refusalErr.Error())
	}
}

func TestToolGateway_AdmitRejectsMalformedBindingsAndExpansion(t *testing.T) {
	g, base := testToolGateway(t)
	if _, err := (*ToolGateway)(nil).Admit(base); refusalMust(t, err, RefusalInvalid).Field != "gateway" {
		t.Fatal("nil gateway was not refused")
	}
	shapeCases := []struct {
		name   string
		mutate func(*ToolCall)
		field  string
	}{
		{"tenant", func(v *ToolCall) { v.Tenant = "" }, "binding"},
		{"purpose", func(v *ToolCall) { v.Purpose = " " }, "binding"},
		{"tool", func(v *ToolCall) { v.Tool = "" }, "binding"},
		{"capability", func(v *ToolCall) { v.Capability = "" }, "binding"},
		{"version", func(v *ToolCall) { v.Version = 0 }, "binding"},
		{"nonce", func(v *ToolCall) { v.Nonce = "" }, "binding"},
		{"budget", func(v *ToolCall) { v.CostBudget = 0 }, "binding"},
		{"blank taint", func(v *ToolCall) { v.InputTaint = []string{" "} }, "taint_provenance"},
		{"empty provenance", func(v *ToolCall) { v.Provenance = nil }, "taint_provenance"},
	}
	for _, tc := range shapeCases {
		t.Run(tc.name, func(t *testing.T) {
			bad := cloneToolCallForTest(base)
			tc.mutate(&bad)
			if refusalMust(t, gError(g, bad), RefusalInvalid).Field != tc.field {
				t.Fatalf("wrong refusal field")
			}
		})
	}
	identityCases := []struct {
		name   string
		mutate func(*ToolCall)
		code   RefusalCode
	}{
		{"missing identity", func(v *ToolCall) { v.Agent.Identity = "" }, RefusalIdentity},
		{"tenant mismatch", func(v *ToolCall) { v.Agent.Tenant = "other" }, RefusalAuthorityExpansion},
		{"purpose mismatch", func(v *ToolCall) { v.Purpose = "other" }, RefusalAuthorityExpansion},
		{"empty delegation", func(v *ToolCall) { v.Delegation = nil }, RefusalDelegation},
		{"incomplete link", func(v *ToolCall) { v.Delegation[0].GrantID = "" }, RefusalDelegation},
		{"delegation tenant", func(v *ToolCall) { v.Delegation[0].Tenant = "other" }, RefusalAuthorityExpansion},
		{"delegation purpose", func(v *ToolCall) { v.Delegation[0].Purpose = "other" }, RefusalAuthorityExpansion},
		{"delegation tool expansion", func(v *ToolCall) { v.Delegation[0].ToolSet = []string{"people.lookup", "payroll"} }, RefusalAuthorityExpansion},
		{"delegation scope expansion", func(v *ToolCall) { v.Delegation[0].DataScope = []string{"people.basic", "salary"} }, RefusalAuthorityExpansion},
		{"delegation budget expansion", func(v *ToolCall) { v.Delegation[0].Budget = 6 }, RefusalAuthorityExpansion},
		{"chain attribution", func(v *ToolCall) {
			v.Delegation = append(v.Delegation, DelegationLink{GrantID: "grant-2", Delegator: "wrong", Delegate: "agent-1", Tenant: "tenant-a", Purpose: "case-review", ToolSet: []string{"people.lookup"}, DataScope: []string{"people.basic"}, Budget: 3})
		}, RefusalDelegation},
		{"leaf binding", func(v *ToolCall) { v.Delegation[0].Delegate = "other" }, RefusalDelegation},
	}
	for _, tc := range identityCases {
		t.Run(tc.name, func(t *testing.T) {
			bad := cloneToolCallForTest(base)
			tc.mutate(&bad)
			refusalMust(t, gError(g, bad), tc.code)
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ToolCall)
		code   RefusalCode
	}{
		{"unregistered tool", func(v *ToolCall) { v.Tool = "missing" }, RefusalCapability},
		{"capability version", func(v *ToolCall) { v.Capability = "other" }, RefusalCapability},
		{"tool set", func(v *ToolCall) { v.Agent.ToolSet = []string{"other"} }, RefusalAuthorityExpansion},
		{"data scope", func(v *ToolCall) { v.DataScope = []string{"salary"} }, RefusalAuthorityExpansion},
		{"raw secret", func(v *ToolCall) {
			v.Args = map[string]any{"nested": []any{map[string]any{"api-key": "secret"}}}
			v.ArgsDigest, _ = DigestArguments(v.Args)
		}, RefusalCredential},
		{"digest", func(v *ToolCall) { v.ArgsDigest = "sha256:wrong" }, RefusalArgsDigest},
		{"budget", func(v *ToolCall) { v.CostBudget = 5 }, RefusalBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := cloneToolCallForTest(base)
			tc.mutate(&bad)
			refusalMust(t, gError(g, bad), tc.code)
		})
	}
	writeGateway, err := NewToolGateway([]ToolDescriptor{{Name: "write", Capability: "write", Version: 1, Class: ToolWrite, DataScope: []string{"people.basic"}, Cost: 1, Schema: "write", Validate: func(any) (TypedResult, error) { return TypedResult{}, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	writeCall := base
	writeCall.Tool, writeCall.Capability, writeCall.Version = "write", "write", 1
	refusalMust(t, gError(writeGateway, writeCall), RefusalEffectClass)
	badArgs := base
	badArgs.Args = map[string]any{"func": func() {}}
	badArgs.ArgsDigest = ""
	refusalMust(t, gError(g, badArgs), RefusalInvalid)
}

func gError(g *ToolGateway, call ToolCall) error {
	_, err := g.Admit(call)
	return err
}

func TestToolGateway_InvokeValidateDigestAndUtilities(t *testing.T) {
	g, call := testToolGateway(t)
	argsA := map[string]any{"b": 2, "a": 1}
	argsB := map[string]any{"a": 1, "b": 2}
	digestA, err := DigestArguments(argsA)
	if err != nil {
		t.Fatal(err)
	}
	digestB, err := DigestArguments(argsB)
	if err != nil || digestA != digestB || !strings.HasPrefix(digestA, "sha256:") {
		t.Fatalf("canonical digests = %q, %q, %v", digestA, digestB, err)
	}
	if _, err := DigestArguments(map[string]any{"bad": func() {}}); err == nil {
		t.Fatal("unsupported JSON argument was digested")
	}
	call.Args = argsA
	call.ArgsDigest = digestA
	admission, err := g.Admit(call)
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.ValidateOutput(admission, call.Tool, "value")
	if err != nil || !result.Validated || result.Value != "value" {
		t.Fatalf("ValidateOutput = %+v, %v", result, err)
	}
	result.Taint[0] = "mutated"
	if admission.InputTaint[0] == "mutated" {
		t.Fatal("ValidateOutput aliased admission taint")
	}
	if _, err := g.Invoke(call, "value"); err != nil {
		t.Fatalf("Invoke = %v", err)
	}
	for _, tc := range []struct {
		name      string
		admission Admission
		tool      string
	}{
		{"unknown tool", admission, "missing"},
		{"tool binding", func() Admission { a := admission; a.Tool = "other"; return a }(), call.Tool},
		{"version binding", func() Admission { a := admission; a.Version++; return a }(), call.Tool},
	} {
		t.Run(tc.name, func(t *testing.T) { refusalMust(t, gValidate(g, tc.admission, tc.tool, "value"), RefusalCapability) })
	}
	for name, output := range map[string]TypedResult{
		"validator error":              {},
		"unvalidated":                  {Schema: "people.v3", Value: "x", Validated: false, Taint: []string{"USER_DATA"}, Provenance: []string{"case.file"}},
		"schema":                       {Schema: "wrong", Value: "x", Validated: true, Taint: []string{"USER_DATA"}, Provenance: []string{"case.file"}},
		"nil value":                    {Schema: "people.v3", Validated: true, Taint: []string{"USER_DATA"}, Provenance: []string{"case.file"}},
		"missing inherited taint":      {Schema: "people.v3", Value: "x", Validated: true, Taint: []string{"DERIVED"}, Provenance: []string{"case.file"}},
		"missing inherited provenance": {Schema: "people.v3", Value: "x", Validated: true, Taint: []string{"USER_DATA"}, Provenance: []string{"validator"}},
	} {
		t.Run(name, func(t *testing.T) {
			badGateway, badCall := testToolGateway(t)
			badCall.Args = argsA
			badCall.ArgsDigest = digestA
			admit, err := badGateway.Admit(badCall)
			if err != nil {
				t.Fatal(err)
			}
			if name == "validator error" {
				badGateway.tools[badCall.Tool] = ToolDescriptor{Name: badCall.Tool, Capability: badCall.Capability, Version: badCall.Version, Class: ToolRead, Cost: 2, Schema: "people.v3", Validate: func(any) (TypedResult, error) { return TypedResult{}, errors.New("validator failed") }}
			} else {
				badGateway.tools[badCall.Tool] = ToolDescriptor{Name: badCall.Tool, Capability: badCall.Capability, Version: badCall.Version, Class: ToolRead, Cost: 2, Schema: "people.v3", Validate: func(any) (TypedResult, error) { return output, nil }}
			}
			refusalMust(t, gValidate(badGateway, admit, badCall.Tool, "value"), RefusalOutput)
		})
	}
	if refusalMust(t, gValidate((*ToolGateway)(nil), admission, call.Tool, "value"), RefusalInvalid).Field != "gateway" {
		t.Fatal("nil ValidateOutput field mismatch")
	}
	explanation := g.Explain(call)
	if strings.Contains(explanation, "person_id") || strings.Contains(explanation, "Ada") || !strings.Contains(explanation, "args_digest=") {
		t.Fatalf("Explain leaked or omitted safe fields: %s", explanation)
	}
	if cloneStrings(nil) != nil || !reflect.DeepEqual(cloneStrings([]string{"a"}), []string{"a"}) || containsString([]string{"a"}, "b") || !containsString([]string{"a"}, "a") || subset([]string{"a"}, []string{"a", "b"}) == false || subset([]string{"b"}, []string{"a"}) || !containsAll([]string{"a", "b"}, []string{"a"}) {
		t.Fatal("string set helpers are incorrect")
	}
	if digestText("nonce") == digestText("other") || joinPath("", "child") != "child" || joinPath("parent", "child") != "parent.child" {
		t.Fatal("digest/path helpers are incorrect")
	}
	nestedSecret := map[string]any{"nested": []any{map[string]any{"token": "secret"}}}
	if got := rawCredentialPath(nestedSecret); got != "nested[0].token" {
		t.Fatalf("nested raw credential path = %q", got)
	}
}

func gValidate(g *ToolGateway, admission Admission, tool string, output any) error {
	_, err := g.ValidateOutput(admission, tool, output)
	return err
}
