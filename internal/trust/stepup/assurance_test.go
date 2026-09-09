package stepup_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

func assuranceRule() stepup.ObligationRule {
	return stepup.ObligationRule{
		RuleID: "assurance-sensitive-write", Capability: stepup.ActionApprove,
		Purposes: []string{stepup.PurposeHCMOperations}, MinRisk: stepup.RiskElevated,
		MinAssurance: trust.AssuranceHigh, Recency: time.Minute,
	}
}

func assuranceOperation() stepup.Operation {
	return stepup.Operation{
		Action: stepup.ActionApprove, Tenant: tenantAcme, ProposalID: proposalID,
		Scopes: testScopes, Purpose: stepup.PurposeHCMOperations,
		Capability: stepup.ActionApprove, Risk: stepup.RiskCritical,
	}
}

// TestTodo_SECARCH_001 is the primary assurance contract test.
func TestTodo_SECARCH_001(t *testing.T) {
	table := stepup.DefaultCredentialAssuranceTable()
	if table.Version != 1 || table.Digest == "" || len(table.Mappings) != 6 {
		t.Fatalf("default assurance table = %+v, want six versioned mappings", table)
	}
	sink := stepup.NewMemoryAssuranceEvidenceStore()
	policy, err := stepup.NewAssuranceObligationPolicy(table, sink, assuranceRule())
	if err != nil {
		t.Fatalf("NewAssuranceObligationPolicy: %v", err)
	}
	decision, err := policy.ResolveAssurance(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical, stepup.CredentialEvidence{Kind: stepup.EvidenceHardwareKey})
	if err != nil {
		t.Fatalf("ResolveAssurance: %v", err)
	}
	if !decision.Accepted || decision.Mapped.AAL != stepup.AAL3 || decision.Required.AAL != stepup.AAL3 {
		t.Fatalf("hardware-key decision = %+v, want accepted AAL3 mapping", decision)
	}
	if len(sink.Decisions()) != 1 {
		t.Fatalf("durable mapping evidence count = %d, want 1", len(sink.Decisions()))
	}

	low, err := policy.ResolveAssurance(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical, stepup.CredentialEvidence{Kind: stepup.EvidencePassword})
	if err != nil {
		t.Fatalf("ResolveAssurance password: %v", err)
	}
	if low.Accepted || low.Reason != stepup.ReasonAssuranceInsufficient {
		t.Fatalf("password decision = %+v, want insufficient assurance", low)
	}
	recovered, err := policy.ResolveAssurance(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical, stepup.CredentialEvidence{Kind: stepup.EvidenceHardwareKey, Recovered: true})
	if err != nil {
		t.Fatalf("ResolveAssurance recovered: %v", err)
	}
	if recovered.Accepted || recovered.Reason != stepup.ReasonRecoveryProofRequired {
		t.Fatalf("recovered decision = %+v, want new-proof refusal", recovered)
	}
}

// TestTodo_SECARCH_001_Golden pins the closed evidence vocabulary and the
// deterministic table digest used in audit records.
func TestTodo_SECARCH_001_Golden(t *testing.T) {
	a := stepup.DefaultCredentialAssuranceTable()
	b := stepup.DefaultCredentialAssuranceTable()
	if a.Digest != b.Digest {
		t.Fatalf("default table digest changed between constructions: %q != %q", a.Digest, b.Digest)
	}
	for _, kind := range []stepup.EvidenceKind{stepup.EvidencePasskey, stepup.EvidenceHardwareKey, stepup.EvidenceTOTP, stepup.EvidenceSMSOTP, stepup.EvidenceRecoveryCode, stepup.EvidencePassword} {
		if _, ok := a.Lookup(kind); !ok {
			t.Fatalf("Lookup(%q) missing from versioned table", kind)
		}
	}
}

// TestTodo_SECARCH_001_Security verifies unknown evidence, insufficient
// assurance, recovery reuse, and audit-safe explanations fail closed.
func TestTodo_SECARCH_001_Security(t *testing.T) {
	sink := stepup.NewMemoryAssuranceEvidenceStore()
	policy, err := stepup.NewAssuranceObligationPolicy(stepup.DefaultCredentialAssuranceTable(), sink, assuranceRule())
	if err != nil {
		t.Fatalf("NewAssuranceObligationPolicy: %v", err)
	}
	unknown, err := policy.ResolveAssurance(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical, stepup.CredentialEvidence{Kind: "PRIVATE_KEY"})
	if err != nil || unknown.Accepted || unknown.Reason != stepup.ReasonUnknownEvidenceKind {
		t.Fatalf("unknown evidence decision = %+v, err=%v", unknown, err)
	}
	recovered, err := policy.ResolveAssurance(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical, stepup.CredentialEvidence{Kind: stepup.EvidenceHardwareKey, Recovered: true, NewProofDigest: "proof-digest"})
	if err != nil || !recovered.Accepted {
		t.Fatalf("recovered evidence with new proof = %+v, err=%v", recovered, err)
	}
	if strings.Contains(recovered.Explain(), "proof-digest") || strings.Contains(recovered.Explain(), "PRIVATE_KEY") {
		t.Fatal("assurance explanation exposes credential or proof material")
	}
}

// TestTodo_SECARCH_001_Integration wires the real table, policy, and memory
// durable-evidence adapter through their exported constructors.
func TestTodo_SECARCH_001_Integration(t *testing.T) {
	sink := stepup.NewMemoryAssuranceEvidenceStore()
	policy, err := stepup.NewAssuranceObligationPolicy(stepup.DefaultCredentialAssuranceTable(), sink, assuranceRule())
	if err != nil {
		t.Fatalf("NewAssuranceObligationPolicy: %v", err)
	}
	ob := stepup.EvaluateObligationWithAssurance(policy, stepup.ObligationRequest{Operation: assuranceOperation(), Stage: stepup.StageDecision, At: baseTime}, mustPrincipal(t, trust.AssuranceHigh, sessionRef), nil, stepup.CredentialEvidence{Kind: stepup.EvidencePassword})
	if ob.Satisfied || ob.Reason != stepup.ReasonAssuranceInsufficient {
		t.Fatalf("assurance-aware obligation = %+v, want refused password mapping", ob)
	}
	if len(sink.Decisions()) != 1 {
		t.Fatalf("integration evidence count = %d, want one durable decision", len(sink.Decisions()))
	}
}

// TestTodo_SECARCH_001_Mutation ensures a mapping decision changes when its
// security-relevant inputs change and never silently accepts a downgraded kind.
func TestTodo_SECARCH_001_Mutation(t *testing.T) {
	table := stepup.DefaultCredentialAssuranceTable()
	sink := stepup.NewMemoryAssuranceEvidenceStore()
	policy, err := stepup.NewAssuranceObligationPolicy(table, sink, assuranceRule())
	if err != nil {
		t.Fatalf("NewAssuranceObligationPolicy: %v", err)
	}
	good, err := policy.ResolveAssurance(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical, stepup.CredentialEvidence{Kind: stepup.EvidenceHardwareKey})
	if err != nil || !good.Accepted {
		t.Fatalf("good mapping = %+v, err=%v", good, err)
	}
	mutated, err := policy.ResolveAssurance(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical, stepup.CredentialEvidence{Kind: stepup.EvidenceSMSOTP})
	if err != nil || mutated.Accepted || mutated.Digest == good.Digest {
		t.Fatalf("mutated mapping = %+v, err=%v, want distinct refused decision", mutated, err)
	}
}

func TestAssuranceTier_ValidityAndOrdering(t *testing.T) {
	valid := stepup.AssuranceTier{IAL: stepup.IAL2, AAL: stepup.AAL2, FAL: stepup.FAL2}
	if !valid.Valid() || !valid.AtLeast(stepup.AssuranceTier{IAL: stepup.IAL1, AAL: stepup.AAL2, FAL: stepup.FAL1}) {
		t.Fatalf("valid tier ordering failed: %+v", valid)
	}
	for _, bad := range []stepup.AssuranceTier{{}, {IAL: 9, AAL: stepup.AAL1, FAL: stepup.FAL1}, {IAL: stepup.IAL1, AAL: 9, FAL: stepup.FAL1}, {IAL: stepup.IAL1, AAL: stepup.AAL1, FAL: 9}} {
		if bad.Valid() || bad.AtLeast(valid) {
			t.Fatalf("invalid tier %+v was accepted", bad)
		}
	}
	if valid.AtLeast(stepup.AssuranceTier{IAL: stepup.IAL3, AAL: stepup.AAL1, FAL: stepup.FAL1}) {
		t.Fatal("tier with insufficient IAL was accepted")
	}
}

func TestCredentialAssuranceTable_RejectsMalformedAndFreezesInput(t *testing.T) {
	valid := []stepup.AssuranceMapping{
		{EvidenceKind: stepup.EvidencePassword, IAL: stepup.IAL1, AAL: stepup.AAL1, FAL: stepup.FAL1},
		{Kind: stepup.EvidencePasskey, EvidenceKind: stepup.EvidencePasskey, IAL: stepup.IAL2, AAL: stepup.AAL2, FAL: stepup.FAL2},
	}
	table, err := stepup.NewCredentialAssuranceTable(2, valid)
	if err != nil {
		t.Fatalf("NewCredentialAssuranceTable: %v", err)
	}
	valid[0].Kind = stepup.EvidenceHardwareKey
	if table.Mappings[0].Kind != stepup.EvidencePasskey || table.Mappings[0].EvidenceKind != stepup.EvidencePasskey || table.Digest == "" {
		t.Fatalf("table did not canonicalize and copy mappings: %+v", table)
	}
	for _, tc := range []struct {
		name string
		v    int
		ms   []stepup.AssuranceMapping
	}{
		{"zero version", 0, valid},
		{"empty mappings", 1, nil},
		{"duplicate kinds", 1, []stepup.AssuranceMapping{
			{Kind: stepup.EvidencePassword, IAL: stepup.IAL1, AAL: stepup.AAL1, FAL: stepup.FAL1},
			{Kind: stepup.EvidencePassword, IAL: stepup.IAL1, AAL: stepup.AAL1, FAL: stepup.FAL1},
		}},
		{"inconsistent aliases", 1, []stepup.AssuranceMapping{{Kind: stepup.EvidencePassword, EvidenceKind: stepup.EvidencePasskey, IAL: stepup.IAL1, AAL: stepup.AAL1, FAL: stepup.FAL1}}},
		{"unknown kind", 1, []stepup.AssuranceMapping{{Kind: "private-key", IAL: stepup.IAL1, AAL: stepup.AAL1, FAL: stepup.FAL1}}},
		{"invalid IAL", 1, []stepup.AssuranceMapping{{Kind: stepup.EvidencePassword, IAL: 0, AAL: stepup.AAL1, FAL: stepup.FAL1}}},
		{"invalid AAL", 1, []stepup.AssuranceMapping{{Kind: stepup.EvidencePassword, IAL: stepup.IAL1, AAL: 0, FAL: stepup.FAL1}}},
		{"invalid FAL", 1, []stepup.AssuranceMapping{{Kind: stepup.EvidencePassword, IAL: stepup.IAL1, AAL: stepup.AAL1, FAL: 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := stepup.NewCredentialAssuranceTable(tc.v, tc.ms); err == nil {
				t.Fatal("malformed table was accepted")
			}
		})
	}
}

type failingAssuranceSink struct{}

func (failingAssuranceSink) RecordAssuranceDecision(stepup.MappingDecision) error {
	return errors.New("sink unavailable")
}

func TestAssuranceEvidenceStore_ValidatesAndCopies(t *testing.T) {
	var nilStore *stepup.MemoryAssuranceEvidenceStore
	if err := nilStore.RecordAssuranceDecision(stepup.MappingDecision{}); err == nil {
		t.Fatal("nil evidence store accepted an incomplete decision")
	}
	store := stepup.NewMemoryAssuranceEvidenceStore()
	if err := store.RecordAssuranceDecision(stepup.MappingDecision{}); err == nil {
		t.Fatal("incomplete decision was accepted")
	}
	d := stepup.MappingDecision{EvidenceID: "ev:1", Digest: "digest:1", Accepted: true}
	if err := store.RecordAssuranceDecision(d); err != nil {
		t.Fatalf("RecordAssuranceDecision: %v", err)
	}
	decisions := store.Decisions()
	if len(decisions) != 1 || decisions[0].EvidenceID != d.EvidenceID {
		t.Fatalf("Decisions = %+v", decisions)
	}
	decisions[0].EvidenceID = "mutated"
	if store.Decisions()[0].EvidenceID != d.EvidenceID {
		t.Fatal("Decisions exposed mutable store state")
	}
}

func TestAssurancePolicy_ErrorsAndLowRiskDefault(t *testing.T) {
	sink := stepup.NewMemoryAssuranceEvidenceStore()
	if _, err := stepup.NewAssuranceObligationPolicy(nil, sink); err == nil {
		t.Fatal("nil assurance table was accepted")
	}
	if _, err := stepup.NewAssuranceObligationPolicy(stepup.DefaultCredentialAssuranceTable(), nil); err == nil {
		t.Fatal("nil assurance evidence sink was accepted")
	}
	policy, err := stepup.NewAssuranceObligationPolicy(stepup.DefaultCredentialAssuranceTable(), sink, assuranceRule())
	if err != nil {
		t.Fatalf("NewAssuranceObligationPolicy: %v", err)
	}
	decision, err := policy.ResolveAssurance("unmatched", stepup.PurposeHCMOperations, stepup.RiskRoutine, stepup.CredentialEvidence{Kind: stepup.EvidencePassword})
	if err != nil || !decision.Accepted || decision.Required != (stepup.AssuranceTier{IAL: stepup.IAL1, AAL: stepup.AAL1, FAL: stepup.FAL1}) {
		t.Fatalf("unmatched routine assurance = %+v, err=%v", decision, err)
	}
	var nilPolicy *stepup.ObligationPolicy
	if decision, err := nilPolicy.ResolveAssurance("x", "y", stepup.RiskRoutine, stepup.CredentialEvidence{}); err == nil || decision.Reason != stepup.ReasonAssurancePolicyMissing {
		t.Fatalf("nil ResolveAssurance = %+v, %v", decision, err)
	}
	badPolicy, err := stepup.NewAssuranceObligationPolicy(stepup.DefaultCredentialAssuranceTable(), failingAssuranceSink{}, assuranceRule())
	if err != nil {
		t.Fatalf("bad sink policy construction: %v", err)
	}
	if _, err := badPolicy.ResolveAssurance(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical, stepup.CredentialEvidence{Kind: stepup.EvidenceHardwareKey}); err == nil {
		t.Fatal("failing assurance sink error was swallowed")
	}
}
