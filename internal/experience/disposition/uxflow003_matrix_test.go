package disposition

import (
	"slices"
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/definitions"
)

func TestBusinessIntentUserFlowDispositionIsCompleteUniqueAndExposureSafe(t *testing.T) {
	r, err := NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	if r.Len() != len(definitions.All()) {
		t.Fatalf("entries=%d want %d", r.Len(), len(definitions.All()))
	}
	seen := map[string]bool{}
	for _, e := range r.Entries() {
		if e.DefinitionRef == "" || seen[e.DefinitionRef] {
			t.Fatalf("duplicate/empty ref: %#v", e)
		}
		seen[e.DefinitionRef] = true
		if !e.Kind.Valid() || len(e.FlowIDs) == 0 || e.Delta == "" {
			t.Fatalf("incomplete disposition: %#v", e)
		}
		if e.Kind == ParticipantChild || e.Kind == VisibleSystemStage {
			if e.Discoverable {
				t.Errorf("unsafe independent discovery: %s", e.DefinitionRef)
			}
		}
	}
	if got := r.Report(); got.Baseline != 14 || got.Extension != 0 || got.SourceUnbound != 0 {
		t.Fatalf("report=%+v", got)
	}
}

func TestTodo_UXFLOW_003_Golden(t *testing.T) {
	r, err := NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Kind{
		"hcmnext.people.change_manager/v1":         ParticipantRoot,
		"hcmnext.people.promote_worker/v1":         ParticipantRoot,
		"hcmnext.rewards.change_base_pay/v1":       ParticipantRoot,
		"hcmnext.work.approve_proposal/v1":         ParticipantChild,
		"hcmnext.work.reject_proposal/v1":          ParticipantChild,
		"hcmnext.operations.detect_drift/v1":       VisibleSystemStage,
		"hcmnext.operations.create_repair_plan/v1": AdminOperator,
		"hcmnext.operations.simulate_repair/v1":    AdminOperator,
	}
	for ref, kind := range want {
		e, ok := r.Lookup(ref)
		if !ok || e.Kind != kind {
			t.Errorf("%s => %#v, want %s", ref, e, kind)
		}
	}
}

func TestTodo_UXFLOW_003_Property(t *testing.T) {
	r, err := NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Entries() {
		for _, flow := range e.FlowIDs {
			if !slices.Contains(r.ForFlow(flow), e.DefinitionRef) {
				t.Errorf("reverse index misses %s in %s", e.DefinitionRef, flow)
			}
		}
	}
	for flow, refs := range r.ReverseIndex() {
		if !slices.IsSorted(refs) {
			t.Errorf("%s reverse index is not canonical: %v", flow, refs)
		}
	}
}

func TestTodo_UXFLOW_003_Security(t *testing.T) {
	r, err := NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	entries := r.Entries()
	entries[0].FlowIDs[0] = "UF-FORGED"
	e, _ := r.Lookup(entries[0].DefinitionRef)
	if e.FlowIDs[0] == "UF-FORGED" || slices.Contains(r.ForFlow("UF-FORGED"), entries[0].DefinitionRef) {
		t.Fatal("caller mutation changed registry")
	}
	idx := r.ReverseIndex()
	idx["UF-017"][0] = "forged"
	if r.ForFlow("UF-017")[0] == "forged" {
		t.Fatal("reverse index leaked internal slice")
	}
}

func TestTodo_UXFLOW_003_Conformance(t *testing.T) {
	defs := definitions.All()
	defs[0].DisplayName = "Renamed presentation"
	r, err := New(defs)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Lookup("Renamed presentation"); ok {
		t.Fatal("display name became identity")
	}
	if _, ok := r.Lookup(defs[0].Ref.String()); !ok {
		t.Fatal("stable ref missing")
	}
}

func TestTodo_UXFLOW_003_Mutation(t *testing.T) {
	defs := definitions.All()
	if _, err := New(append(defs, defs[0])); err == nil {
		t.Fatal("duplicate accepted")
	}
	unknown := intent.Definition{Ref: intent.Ref{TypeID: "hcmnext.unknown", Version: 1}, DisplayName: "Unknown", Maturity: intent.MaturityDraftContract}
	if _, err := New(append(defs, unknown)); err == nil {
		t.Fatal("source-unbound accepted intent without explicit disposition")
	}
}
