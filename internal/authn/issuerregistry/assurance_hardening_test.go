package issuerregistry

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/stepup"
)

var assuranceTestAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func assuranceTestIssuer(contract AssuranceContract) Issuer {
	return Issuer{AssuranceContract: contract}
}

func TestAssuranceContract_TierUsesRequiredValuesForUnspecifiedLevels(t *testing.T) {
	contract := AssuranceContract{
		RequiredIAL: stepup.IAL2,
		RequiredAAL: stepup.AAL2,
		RequiredFAL: stepup.FAL1,
	}
	if got := contract.tier(); got != (stepup.AssuranceTier{IAL: stepup.IAL2, AAL: stepup.AAL2, FAL: stepup.FAL1}) {
		t.Fatalf("tier() = %+v, want required levels", got)
	}
	if !contract.Configured() {
		t.Fatal("contract with required levels is not configured")
	}

	direct := AssuranceContract{TableVersion: 7, IAL: stepup.IAL3, AAL: stepup.AAL3, FAL: stepup.FAL2, RequiredIAL: stepup.IAL1, RequiredAAL: stepup.AAL1, RequiredFAL: stepup.FAL1}
	if got := direct.tier(); got != (stepup.AssuranceTier{IAL: stepup.IAL3, AAL: stepup.AAL3, FAL: stepup.FAL2}) {
		t.Fatalf("tier() = %+v, want explicitly declared levels", got)
	}
	zero := AssuranceContract{}
	if !zero.tier().Valid() && zero.Configured() {
		t.Fatal("zero contract unexpectedly configured")
	}
}

func TestIssuerResolveAssurance_RejectsMissingMalformedAndWrongTables(t *testing.T) {
	table := stepup.DefaultCredentialAssuranceTable()
	tests := []struct {
		name     string
		issuer   Issuer
		resolved *stepup.CredentialAssuranceTable
		wantErr  error
	}{
		{name: "missing contract", issuer: assuranceTestIssuer(AssuranceContract{}), resolved: table, wantErr: ErrAssuranceContract},
		{name: "invalid table version", issuer: assuranceTestIssuer(AssuranceContract{TableVersion: 0, IAL: stepup.IAL2, AAL: stepup.AAL2, FAL: stepup.FAL1}), resolved: table, wantErr: ErrInvalidIssuer},
		{name: "invalid assurance tier", issuer: assuranceTestIssuer(AssuranceContract{TableVersion: table.Version}), resolved: table, wantErr: ErrInvalidIssuer},
		{name: "nil table", issuer: assuranceTestIssuer(AssuranceContract{TableVersion: table.Version, IAL: stepup.IAL2, AAL: stepup.AAL2, FAL: stepup.FAL1}), wantErr: ErrAssuranceContract},
		{name: "wrong table version", issuer: assuranceTestIssuer(AssuranceContract{TableVersion: table.Version, IAL: stepup.IAL2, AAL: stepup.AAL2, FAL: stepup.FAL1}), resolved: &stepup.CredentialAssuranceTable{Version: table.Version + 1}, wantErr: ErrAssuranceContract},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.issuer.ResolveAssurance(tt.resolved)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResolveAssurance error = %v, want errors.Is(..., %v)", err, tt.wantErr)
			}
		})
	}

	valid := assuranceTestIssuer(AssuranceContract{TableVersion: table.Version, IAL: stepup.IAL2, AAL: stepup.AAL3, FAL: stepup.FAL2})
	got, err := valid.ResolveAssurance(table)
	if err != nil || got != (stepup.AssuranceTier{IAL: stepup.IAL2, AAL: stepup.AAL3, FAL: stepup.FAL2}) {
		t.Fatalf("valid ResolveAssurance = %+v, err=%v", got, err)
	}
}

type rejectingAccessSink struct{ err error }

func (s rejectingAccessSink) RecordAccessDecision(AccessDecision) error { return s.err }

func TestEvaluateAccess_RejectsInvalidRequestsAndPreservesEvidenceState(t *testing.T) {
	table := stepup.DefaultCredentialAssuranceTable()
	issuer := assuranceTestIssuer(AssuranceContract{TableVersion: table.Version, IAL: stepup.IAL2, AAL: stepup.AAL2, FAL: stepup.FAL1})
	good := issuer.AssuranceContractTier()

	for _, tt := range []struct {
		name string
		call func(AccessDecisionSink) (AccessDecision, error)
	}{
		{name: "nil sink", call: func(AccessDecisionSink) (AccessDecision, error) {
			return EvaluateAccess(AccessRequest{Issuer: issuer, Table: table, Presented: good, At: assuranceTestAt}, nil)
		}},
		{name: "missing time", call: func(s AccessDecisionSink) (AccessDecision, error) {
			return EvaluateAccess(AccessRequest{Issuer: issuer, Table: table, Presented: good}, s)
		}},
		{name: "missing table", call: func(s AccessDecisionSink) (AccessDecision, error) {
			return EvaluateAccess(AccessRequest{Issuer: issuer, Presented: good, At: assuranceTestAt}, s)
		}},
		{name: "missing contract", call: func(s AccessDecisionSink) (AccessDecision, error) {
			return EvaluateAccess(AccessRequest{Issuer: Issuer{}, Table: table, Presented: good, At: assuranceTestAt}, s)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var sink MemoryAccessEvidenceStore
			_, err := tt.call(&sink)
			if err == nil {
				t.Fatal("EvaluateAccess succeeded, want refusal")
			}
			if len(sink.Decisions()) != 0 {
				t.Fatalf("invalid request recorded evidence: %+v", sink.Decisions())
			}
		})
	}

	downgrade, err := EvaluateAccess(AccessRequest{Issuer: issuer, Table: table, Presented: stepup.AssuranceTier{}, At: assuranceTestAt}, &MemoryAccessEvidenceStore{})
	if err != nil || downgrade.Allowed || !downgrade.Revoked || downgrade.ReauthenticationNeeded || downgrade.Reason != ReasonAssuranceDowngrade {
		t.Fatalf("non-sensitive downgrade = %+v, err=%v", downgrade, err)
	}

	deprovisioned, err := EvaluateAccess(AccessRequest{Issuer: issuer, Table: table, Presented: good, Deprovisioned: true, At: assuranceTestAt}, &MemoryAccessEvidenceStore{})
	if err != nil || deprovisioned.Allowed || !deprovisioned.Revoked || deprovisioned.Reason != ReasonExternalDeprovisioned {
		t.Fatalf("deprovisioned access = %+v, err=%v", deprovisioned, err)
	}

	sentinel := errors.New("sink failed")
	sinkErr := &MemoryAccessEvidenceStore{}
	decision, err := EvaluateAccess(AccessRequest{Issuer: issuer, Table: table, Presented: good, At: assuranceTestAt}, rejectingAccessSink{err: sentinel})
	if !errors.Is(err, sentinel) || decision.Digest == "" || len(sinkErr.Decisions()) != 0 {
		t.Fatalf("sink failure decision=%+v err=%v state=%+v", decision, err, sinkErr.Decisions())
	}
}

func TestMemoryAccessEvidenceStore_RecordAndDecisionsAreValidatedAndCopied(t *testing.T) {
	decision := AccessDecision{TableVersion: 1, At: assuranceTestAt, Reason: ReasonAccessAllowed, Digest: "digest", EvidenceID: "evidence"}
	store := NewMemoryAccessEvidenceStore()
	if err := store.RecordAccessDecision(decision); err != nil {
		t.Fatalf("RecordAccessDecision: %v", err)
	}
	copyOfDecisions := store.Decisions()
	copyOfDecisions[0].Reason = "mutated"
	if got := store.Decisions()[0].Reason; got != ReasonAccessAllowed {
		t.Fatalf("Decisions returned mutable storage, reason=%q", got)
	}
	var nilStore *MemoryAccessEvidenceStore
	if err := nilStore.RecordAccessDecision(decision); err == nil {
		t.Fatal("nil store accepted a decision")
	}
	for _, incomplete := range []AccessDecision{{}, {Digest: "digest"}, {EvidenceID: "evidence"}} {
		if err := store.RecordAccessDecision(incomplete); err == nil {
			t.Fatalf("incomplete decision %+v was accepted", incomplete)
		}
	}
	if got := len(store.Decisions()); got != 1 {
		t.Fatalf("stored decision count = %d, want 1", got)
	}
}

func TestAccessDecision_ExplainAndValidateRejectIncompleteEvidence(t *testing.T) {
	valid := AccessDecision{TableVersion: 3, At: assuranceTestAt, Reason: ReasonAccessAllowed, Digest: "digest", EvidenceID: "evidence"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate(valid): %v", err)
	}
	if got := valid.Explain(); !strings.Contains(got, ReasonAccessAllowed) || strings.Contains(got, "digest") {
		t.Fatalf("Explain = %q, want reason without digest", got)
	}
	for _, incomplete := range []AccessDecision{
		{At: assuranceTestAt, Reason: ReasonAccessAllowed, Digest: "digest", EvidenceID: "evidence"},
		{TableVersion: 1, Reason: ReasonAccessAllowed, Digest: "digest", EvidenceID: "evidence"},
		{TableVersion: 1, At: assuranceTestAt, Digest: "digest", EvidenceID: "evidence"},
		{TableVersion: 1, At: assuranceTestAt, Reason: ReasonAccessAllowed, EvidenceID: "evidence"},
		{TableVersion: 1, At: assuranceTestAt, Reason: ReasonAccessAllowed, Digest: "digest"},
	} {
		if err := incomplete.Validate(); err == nil {
			t.Fatalf("Validate(%+v) succeeded, want an error", incomplete)
		}
	}
}
