package sources_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources"
)

// TestTodo_MSRC_003 is the MSRC-003 primary test: it compiles the checked-in
// kernel/governance/evidence family sources and asserts the eight kernel
// aggregates internal/intent/model.Catalog() already registers for this
// family cluster (IntentInstance, ProposalRevision, TransactionPlan,
// ApprovalBinding, ExecutionBinding, EvidenceRecord, Observation, RepairPlan)
// round-trip through the source with zero cross-check mismatches.
func TestTodo_MSRC_003(t *testing.T) {
	manifest, bundle := compileValid(t)

	wantCovered := []string{
		"IntentInstance/v1", "ProposalRevision/v1", "TransactionPlan/v1", // kernel
		"ApprovalBinding/v1", "ExecutionBinding/v1", // governance
		"EvidenceRecord/v1", "Observation/v1", "RepairPlan/v1", // evidence
	}
	byRef := map[string]sources.EntitySource{}
	for _, e := range bundle.Entities {
		if e.Family == "kernel" || e.Family == "governance" || e.Family == "evidence" {
			byRef[e.Ref()] = e
		}
	}
	for _, ref := range wantCovered {
		e, ok := byRef[ref]
		if !ok {
			t.Errorf("%s not found in kernel/governance/evidence family sources", ref)
			continue
		}
		if !e.Covered {
			t.Errorf("%s: covered = false, want true", ref)
		}
	}

	mismatches, err := sources.CrossCheckModel(manifest)
	if err != nil {
		t.Fatalf("CrossCheckModel: %v", err)
	}
	for _, m := range mismatches {
		t.Errorf("cross-check mismatch: %s", m)
	}
}

// TestTodo_MSRC_003_Property asserts, for every covered:true entity in the
// kernel/governance/evidence families, that every ACTIVE property it
// declares carries a non-empty authority_ref, retention_class_ref and
// schema_path — the exact fields MSRC-003's RED clause calls out
// ("BusinessIntent families, proposal/decision/transaction/observation/
// outcome... cannot round-trip from source" if any of these is missing).
func TestTodo_MSRC_003_Property(t *testing.T) {
	bundle := loadValidBundle(t)
	checked := 0
	for _, e := range bundle.Entities {
		if e.Family != "kernel" && e.Family != "governance" && e.Family != "evidence" {
			continue
		}
		for _, p := range e.Properties {
			if p.Status != "ACTIVE" {
				continue
			}
			checked++
			if p.AuthorityRef == "" {
				t.Errorf("%s.%s: ACTIVE property has no authority_ref", e.Ref(), p.Name)
			}
			if p.RetentionClassRef == "" {
				t.Errorf("%s.%s: ACTIVE property has no retention_class_ref", e.Ref(), p.Name)
			}
			if p.SchemaPath == "" {
				t.Errorf("%s.%s: ACTIVE property has no schema_path", e.Ref(), p.Name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no ACTIVE kernel/governance/evidence properties were checked")
	}
}

// TestTodo_MSRC_003_Golden pins the exact sorted entity-name list for each of
// the three families, so a silent addition or removal changes this test
// before it changes anything downstream.
func TestTodo_MSRC_003_Golden(t *testing.T) {
	bundle := loadValidBundle(t)

	cases := []struct {
		family string
		want   []string
	}{
		{"kernel", []string{
			"CancellationDecision", "ClosureRecord", "CorrectionReference", "IntentInstance",
			"IntentRelationship", "IntentResult", "ProposalRevision", "SupersessionLink",
			"TransactionAbortRecord", "TransactionCommitReceipt", "TransactionPlan",
		}},
		{"governance", []string{
			"ApprovalBinding", "ApprovalCertificate", "ApprovalRequirement", "AuthoritySource",
			"AuthorizationDecision", "CapabilityDefinition", "DelegationGrant", "ExecutionBinding",
			"GovernanceDecisionBundle", "Obligation", "Principal", "WorkflowDefinition",
		}},
		{"evidence", []string{
			"EvidenceManifest", "EvidenceRecord", "EvidenceRequirement", "EvidenceSatisfaction",
			"Incident", "LedgerEvent", "Observation", "OutboxEffect", "OutcomeObservation",
			"ReconciliationResult", "RepairPlan",
		}},
	}
	for _, c := range cases {
		var got []string
		for _, e := range entitiesInFamily(bundle, c.family) {
			got = append(got, e.Name)
		}
		sort.Strings(got)
		want := append([]string(nil), c.want...)
		sort.Strings(want)
		assertStringSlicesEqual(t, c.family, got, want)
	}
}

// TestTodo_MSRC_003_Mutation mutates one covered kernel-family entity's
// active property (ProposalRevision.material_digest's go_type) and asserts
// [sources.CrossCheckModel] reports exactly the expected mismatch — proving
// the cross-check actually inspects field content, not merely presence.
func TestTodo_MSRC_003_Mutation(t *testing.T) {
	bundle := loadValidBundle(t)
	mutateFirstProperty(&bundle, "ProposalRevision", func(p *sources.PropertySource) {
		p.GoType = "values.EffectiveInterval" // real model.go type is "string"
	})
	manifest, errs := sources.Compile(bundle)
	if len(errs) != 0 {
		t.Fatalf("Compile of the mutated bundle returned errors (expected a clean compile, mismatch only at cross-check): %v", errs)
	}
	mismatches, err := sources.CrossCheckModel(manifest)
	if err != nil {
		t.Fatalf("CrossCheckModel: %v", err)
	}
	if len(mismatches) == 0 {
		t.Fatal("CrossCheckModel reported zero mismatches after mutating ProposalRevision.material_digest's go_type")
	}
	found := false
	for _, m := range mismatches {
		if containsAll(m, "ProposalRevision/v1", "material_digest", "go_type mismatch") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a material_digest go_type mismatch among: %v", mismatches)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
