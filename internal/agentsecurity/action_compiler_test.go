package agentsecurity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type actionDraft struct {
	Fields []string
	Refs   []string
	Claims []string
}

func (d actionDraft) DraftFields() []string     { return d.Fields }
func (d actionDraft) DraftReferences() []string { return d.Refs }
func (d actionDraft) DraftClaims() []string     { return d.Claims }
func (d actionDraft) DraftCanonicalBytes() ([]byte, error) {
	return json.Marshal(struct {
		Fields []string `json:"fields"`
		Refs   []string `json:"refs"`
		Claims []string `json:"claims"`
	}{d.Fields, d.Refs, d.Claims})
}
func (d actionDraft) DetachDraft() (DraftValue, error) {
	return actionDraft{
		Fields: append([]string(nil), d.Fields...),
		Refs:   append([]string(nil), d.Refs...),
		Claims: append([]string(nil), d.Claims...),
	}, nil
}

func actionCompilerFixture(t *testing.T) (*ActionCompiler, Admission) {
	t.Helper()
	registry, admission := ingestionFixture(t)
	definitions := NewDefinitionRegistry()
	if err := definitions.Register(IntentDefinition{ID: "leave.accrue", Version: "v7", RequiresReview: true, RequiresSimulation: true, MaxBulk: 1}); err != nil {
		t.Fatal(err)
	}
	if err := definitions.Register(IntentDefinition{ID: "notice.send", Version: "v2", MaxBulk: 4}); err != nil {
		t.Fatal(err)
	}
	compiler, err := NewActionCompiler(definitions, registry)
	if err != nil {
		t.Fatal(err)
	}
	return compiler, admission
}

func validAction() ProposedAction {
	return ProposedAction{
		DefinitionID: "leave.accrue",
		Arguments:    map[string]string{"subject": "person:p1", "days": "3"},
		Sources:      []string{"person:p1"},
		Taint:        []string{"DERIVED"},
		Uncertainty:  "balance read is point-in-time",
		Bulk:         1,
	}
}

func validActionOutput() AgentOutput {
	return AgentOutput{
		Schema: "people.v3",
		Value: actionDraft{
			Fields: []string{"arguments", "definition"},
			Refs:   []string{"person:p1"},
			Claims: []string{"person-exists"},
		},
		References: []string{"person:p1"},
		Fields:     []string{"definition", "arguments"},
		Claims:     []string{"person-exists"},
		Narrative:  "Draft accrual for review.",
	}
}

func conciergeAttribution() Attribution {
	return Attribution{Agent: "concierge", Model: "m1", ModelDigest: "sha256:m", Delegation: "grant", Purpose: "triage", Cost: 1}
}

func TestAgentActionCreatesDraftIntentWithoutAuthorityExpansion(t *testing.T) {
	compiler, admission := actionCompilerFixture(t)
	// Concierge and domain agents share the one compiler.
	for _, agent := range []string{"concierge", "leave-agent"} {
		attribution := conciergeAttribution()
		attribution.Agent = agent
		draft, err := compiler.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), validAction(), attribution)
		if err != nil {
			t.Fatalf("CompileAction(%s): %v", agent, err)
		}
		if draft.DefinitionID != "leave.accrue" || draft.DefinitionVersion != "v7" {
			t.Fatalf("draft targets %s@%s", draft.DefinitionID, draft.DefinitionVersion)
		}
		if draft.Arguments["days"] != "3" || len(draft.Sources) != 1 || len(draft.Taint) != 1 {
			t.Fatalf("draft lost exact material: %+v", draft)
		}
		if draft.Attribution.Agent != agent || draft.Attribution.Cost != 1 || draft.ToolVersion != 3 {
			t.Fatalf("draft lost attribution: %+v", draft.Attribution)
		}
		if !draft.RequiresReview || !draft.RequiresSimulation {
			t.Fatal("compiler bypassed the review/simulation handoff")
		}
		if draft.DraftID == "" || draft.Receipt != draft.DraftID {
			t.Fatal("draft carries no receipt-bound identity")
		}
	}
	// RED: every authority expansion refuses with zero draft.
	cases := map[string]func(*ProposedAction, *AgentOutput, *Attribution){
		"invented definition": func(a *ProposedAction, _ *AgentOutput, _ *Attribution) { a.DefinitionID = "pay.raise" },
		"hidden uncertainty":  func(a *ProposedAction, _ *AgentOutput, _ *Attribution) { a.Uncertainty = "" },
		"missing evidence":    func(a *ProposedAction, _ *AgentOutput, _ *Attribution) { a.Sources = nil },
		"missing taint":       func(a *ProposedAction, _ *AgentOutput, _ *Attribution) { a.Taint = nil },
		"bulk loop":           func(a *ProposedAction, _ *AgentOutput, _ *Attribution) { a.Bulk = 64 },
		"unattributed model":  func(_ *ProposedAction, _ *AgentOutput, at *Attribution) { at.ModelDigest = "" },
		"unvalidated output":  func(_ *ProposedAction, o *AgentOutput, _ *Attribution) { o.Schema = "forged.v9" },
	}
	for name, mutate := range cases {
		mutatedAction, mutatedOutput, mutatedAttribution := validAction(), validActionOutput(), conciergeAttribution()
		mutate(&mutatedAction, &mutatedOutput, &mutatedAttribution)
		if _, err := compiler.CompileAction(context.Background(), admission, "people.lookup", mutatedOutput, mutatedAction, mutatedAttribution); err == nil {
			t.Fatalf("%s compiled", name)
		}
	}
	if _, err := NewActionCompiler(nil, nil); err == nil {
		t.Fatal("hollow compiler accepted")
	}
	var nilCompiler *ActionCompiler
	if _, err := nilCompiler.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), validAction(), conciergeAttribution()); err == nil {
		t.Fatal("nil compiler compiled")
	}
}

func actionBoolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestTodo_AGENT_005_Golden(t *testing.T) {
	compiler, admission := actionCompilerFixture(t)
	draft, err := compiler.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), validAction(), conciergeAttribution())
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"draft=" + draft.DraftID,
		"definition=" + draft.DefinitionID + "@" + draft.DefinitionVersion,
		"review=" + actionBoolString(draft.RequiresReview) + " simulation=" + actionBoolString(draft.RequiresSimulation),
		"receipt=" + draft.Receipt,
	}
	if _, err := compiler.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), ProposedAction{DefinitionID: "invented"}, conciergeAttribution()); err == nil {
		t.Fatal("invented definition compiled")
	} else {
		lines = append(lines, "refusal[invented]="+err.Error())
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "agent005_action.golden")
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

func TestTodo_AGENT_005_Mutation(t *testing.T) {
	compiler, admission := actionCompilerFixture(t)
	base, err := compiler.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), validAction(), conciergeAttribution())
	if err != nil {
		t.Fatal(err)
	}
	// Mutated arguments re-identify the draft.
	mutated := validAction()
	mutated.Arguments = map[string]string{"subject": "person:p1", "days": "4"}
	changed, err := compiler.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), mutated, conciergeAttribution())
	if err != nil {
		t.Fatal(err)
	}
	if changed.DraftID == base.DraftID {
		t.Fatal("argument mutation reused the draft identity")
	}
	// Mutated uncertainty re-identifies the draft too.
	hedged := validAction()
	hedged.Uncertainty = "balance read is stale"
	rehedged, err := compiler.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), hedged, conciergeAttribution())
	if err != nil {
		t.Fatal(err)
	}
	if rehedged.DraftID == base.DraftID {
		t.Fatal("uncertainty mutation reused the draft identity")
	}
	// A second definition version compiles to its own receipt.
	second := validAction()
	second.DefinitionID = "notice.send"
	second.Bulk = 2
	notice, err := compiler.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), second, conciergeAttribution())
	if err != nil {
		t.Fatal(err)
	}
	if notice.DefinitionVersion != "v2" || notice.RequiresReview || notice.DraftID == base.DraftID {
		t.Fatalf("second definition draft = %+v", notice)
	}
}
