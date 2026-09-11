package agentsecurity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ingestionFixture(t *testing.T) (*OwnerRegistry, Admission) {
	t.Helper()
	g, admission := outputValidationFixture(t)
	registry, err := NewOwnerRegistry(g)
	if err != nil {
		t.Fatal(err)
	}
	err = registry.Register("people.lookup", DraftOwners{
		References: outputRefs{"person:p1": true},
		Fields:     outputFields{allow: true},
		Claims:     outputClaims{"person-exists": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry, admission
}

func validIngestOutput() AgentOutput {
	value := validPersonDraft()
	return AgentOutput{Schema: "people.v3", Value: value, References: value.ReferenceSet, Fields: value.FieldSet, Claims: value.ClaimSet, Narrative: "A supported lookup result."}
}

func TestOwnerRegistryIngestDraft(t *testing.T) {
	registry, admission := ingestionFixture(t)
	draft, err := registry.IngestDraft(context.Background(), admission, "people.lookup", validIngestOutput())
	if err != nil || draft.Result.Value.(personDraft).Name != "Ada" {
		t.Fatalf("draft=%+v err=%v", draft, err)
	}
	// Unregistered tools never reach the validator.
	if _, err := registry.IngestDraft(context.Background(), admission, "people.delete", validIngestOutput()); err == nil {
		t.Fatal("unregistered tool ingested")
	}
	// Registration faults are typed refusals with zero effect.
	g, _ := outputValidationFixture(t)
	if _, err := NewOwnerRegistry(nil); err == nil {
		t.Fatal("nil gateway registry accepted")
	}
	empty, err := NewOwnerRegistry(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Register("missing.tool", DraftOwners{References: outputRefs{}, Fields: outputFields{}, Claims: outputClaims{}}); err == nil {
		t.Fatal("unknown tool owners accepted")
	}
	if err := empty.Register("people.lookup", DraftOwners{}); err == nil {
		t.Fatal("nil owners accepted")
	}
	if err := registry.Register("people.lookup", DraftOwners{References: outputRefs{}, Fields: outputFields{}, Claims: outputClaims{}}); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	var nilRegistry *OwnerRegistry
	if _, err := nilRegistry.IngestDraft(context.Background(), admission, "people.lookup", validIngestOutput()); err == nil {
		t.Fatal("nil registry ingested")
	}
}

func TestOwnerRegistryIngestDraftGolden(t *testing.T) {
	registry, admission := ingestionFixture(t)
	draft, err := registry.IngestDraft(context.Background(), admission, "people.lookup", validIngestOutput())
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	lines = append(lines, "draft-schema="+draft.Result.Schema)
	lines = append(lines, "draft-receipt="+draft.Result.semanticReceipt)
	for _, probe := range []struct {
		tool   string
		output AgentOutput
	}{{"people.delete", validIngestOutput()}, {"people.lookup", AgentOutput{Schema: "wrong", Value: validPersonDraft()}}} {
		_, err := registry.IngestDraft(context.Background(), admission, probe.tool, probe.output)
		if err == nil {
			t.Fatalf("%s ingested unexpectedly", probe.tool)
		}
		lines = append(lines, "refusal["+probe.tool+"]="+err.Error())
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "agent003_ingestion.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func FuzzOwnerRegistryIngestDraft(f *testing.F) {
	f.Add("people.lookup", "people.v3")
	f.Fuzz(func(t *testing.T, tool, schema string) {
		registry, admission := ingestionFixture(t)
		draft, err := registry.IngestDraft(context.Background(), admission, tool, AgentOutput{Schema: schema, Value: validPersonDraft(), References: []string{"person:p1"}, Fields: []string{"name"}, Claims: []string{"person-exists"}})
		if tool == "people.lookup" && schema == "people.v3" {
			if err != nil || draft.Result.Schema != schema {
				t.Fatalf("registered ingestion = %+v, %v", draft, err)
			}
		} else if err == nil {
			t.Fatalf("unregistered ingestion accepted: tool=%q schema=%q", tool, schema)
		}
	})
}

func TestOwnerRegistryIngestDraftSecurity(t *testing.T) {
	registry, admission := ingestionFixture(t)
	// An admission minted by a foreign gateway never validates here.
	foreign, _ := NewToolGateway([]ToolDescriptor{{Name: "people.lookup", Capability: "people.read", Version: 3, Class: ToolDraft, DataScope: []string{"people.basic"}, Cost: 1, Schema: "people.v3", Validate: func(v any) (TypedResult, error) {
		return TypedResult{Schema: "people.v3", Value: v, Validated: true, Taint: []string{"DERIVED"}, Provenance: []string{"validator"}}, nil
	}}})
	foreignCall := ToolCall{Agent: AgentIdentity{Identity: "identity", AgentID: "agent", Tenant: "tenant", Purpose: "purpose", ToolSet: []string{"people.lookup"}, DataScope: []string{"people.basic"}, Budget: 2}, Delegation: []DelegationLink{{GrantID: "grant", Delegator: "root", Delegate: "agent", Tenant: "tenant", Purpose: "purpose", ToolSet: []string{"people.lookup"}, DataScope: []string{"people.basic"}, Budget: 2}}, Tenant: "tenant", Purpose: "purpose", Tool: "people.lookup", Capability: "people.read", Version: 3, Nonce: "nonce", Args: map[string]any{"id": "p1"}, InputTaint: []string{"DERIVED"}, Provenance: []string{"validator"}, CostBudget: 1, DataScope: []string{"people.basic"}}
	foreignCall.ArgsDigest, _ = DigestArguments(foreignCall.Args)
	foreignAdmission, err := foreign.Admit(foreignCall)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.IngestDraft(context.Background(), foreignAdmission, "people.lookup", validIngestOutput()); err == nil {
		t.Fatal("foreign admission ingested")
	}
	// Registered owners deny unauthorized fields through the same path.
	denying, _ := NewOwnerRegistry(registry.gateway)
	if err := denying.Register("people.lookup", DraftOwners{References: outputRefs{"person:p1": true}, Fields: outputFields{allow: false}, Claims: outputClaims{"person-exists": true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := denying.IngestDraft(context.Background(), admission, "people.lookup", validIngestOutput()); err == nil {
		t.Fatal("unauthorized field ingested through registered owners")
	}
}

func TestOwnerRegistryIngestDraftMutation(t *testing.T) {
	registry, admission := ingestionFixture(t)
	draft, err := registry.IngestDraft(context.Background(), admission, "people.lookup", validIngestOutput())
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the admitted tool binding after registration refuses.
	mutated := admission
	mutated.Tool = "people.delete"
	if _, err := registry.IngestDraft(context.Background(), mutated, "people.lookup", validIngestOutput()); err == nil {
		t.Fatal("rebound admission ingested")
	}
	// The ingested draft is detached from later caller mutation.
	before := draft.Result.semanticReceipt
	output := validIngestOutput()
	output.References[0] = "mutated"
	if _, err := registry.IngestDraft(context.Background(), admission, "people.lookup", output); err == nil {
		t.Fatal("mutated references ingested")
	}
	if draft.Result.semanticReceipt != before {
		t.Fatal("earlier draft receipt shifted under later mutation")
	}
}
