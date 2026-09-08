package cycle

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func restatementFixture(t *testing.T) RestatementRequest {
	t.Helper()
	return RestatementRequest{
		RestatementID: "restatement-1", Revision: 1, CorrectionAt: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
		Prior:                            PriorCycleClose{TenantID: "tenant-a", CycleID: "cycle-1", CycleRevisionDigest: "sha256:" + strings.Repeat("a", 64), CloseResultDigest: "sha256:" + strings.Repeat("b", 64), CloseEvidenceRef: "ledger:close/4", ClosedAt: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), Sequence: 4},
		AffectedPopulationManifestDigest: "sha256:" + strings.Repeat("e", 64),
		AffectedResultManifestDigest:     "sha256:" + strings.Repeat("f", 64),
		Impacts:                          []RestatementImpact{{ResultID: "result-1", PopulationID: "population-1", PriorResultDigest: "sha256:" + strings.Repeat("c", 64), CorrectedResultDigest: "sha256:" + strings.Repeat("d", 64)}},
		Approvals:                        []RestatementApproval{{ApprovalID: "approval-1", DecisionDigest: "sha256:" + strings.Repeat("9", 64), DecisionEvidenceRef: "ledger:approval/1", ApprovedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)}},
		Compensations:                    []RestatementCompensation{{CompensationID: "comp-1", Required: true, Planned: false}},
		Outcome:                          ReconciliationPendingCompensation,
	}
}

func TestTodo_CYCLE_008(t *testing.T) {
	request := restatementFixture(t)
	result, err := RestatePriorCycle(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest == "" || result.Prior != request.Prior || result.Impacts[0] != request.Impacts[0] || result.Outcome != ReconciliationPendingCompensation {
		t.Fatalf("restatement did not bind append-only basis: %+v", result)
	}
	request.Impacts[0].CorrectedResultDigest = "sha256:" + strings.Repeat("e", 64)
	request.Compensations[0].PlanDigest = "sha256:" + strings.Repeat("f", 64)
	request.Compensations[0].Planned = true
	if result.Impacts[0].CorrectedResultDigest == request.Impacts[0].CorrectedResultDigest {
		t.Fatal("returned restatement aliases mutable request impact")
	}
}

func TestTodo_CYCLE_008_Property(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RestatementRequest)
		want   error
	}{
		{"missing-impact", func(r *RestatementRequest) { r.Impacts = nil }, ErrRestatementImpact},
		{"forged-prior-digest", func(r *RestatementRequest) { r.Prior.CloseResultDigest = "sha256:forged" }, ErrRestatementPrior},
		{"sequence-zero", func(r *RestatementRequest) { r.Prior.Sequence = 0 }, ErrRestatementPrior},
		{"omitted-approval", func(r *RestatementRequest) { r.Approvals = nil }, ErrRestatementApproval},
		{"required-compensation-unplanned", func(r *RestatementRequest) {
			r.Compensations[0].Required = true
			r.Compensations[0].Planned = false
			r.Outcome = ReconciliationReconciled
		}, ErrRestatementOutcome},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := restatementFixture(t)
			tc.mutate(&request)
			if _, err := RestatePriorCycle(request); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestTodo_CYCLE_008_Golden(t *testing.T) {
	result, err := RestatePriorCycle(restatementFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Digest, "sha256:29227c29028ec37b515ec54eb0246815353991efef5b2b491250bb36a3cda2f9"; got != want {
		t.Fatalf("digest=%q, want %q", got, want)
	}
}

func TestRestatementCannotClaimReconciledFromAPlan(t *testing.T) {
	r := restatementFixture(t)
	r.Compensations[0].Planned = true
	r.Compensations[0].PlanDigest = "sha256:" + strings.Repeat("7", 64)
	r.Outcome = ReconciliationReconciled
	if _, err := RestatePriorCycle(r); !errors.Is(err, ErrRestatementOutcome) {
		t.Fatalf("planned-only reconciliation err=%v", err)
	}
	r.Compensations[0].Completed = true
	r.Compensations[0].ResultDigest = "sha256:" + strings.Repeat("8", 64)
	r.Compensations[0].EvidenceRef = "ledger:compensation/1"
	if _, err := RestatePriorCycle(r); err != nil {
		t.Fatalf("completed compensation: %v", err)
	}
}

func TestTodo_CYCLE_008_Mutation(t *testing.T) {
	request := restatementFixture(t)
	result, err := RestatePriorCycle(request)
	if err != nil {
		t.Fatal(err)
	}
	before := result.Digest
	request.Impacts[0].PriorResultDigest = "sha256:" + strings.Repeat("9", 64)
	request.Impacts = append(request.Impacts, RestatementImpact{ResultID: "other", PopulationID: "population-2", PriorResultDigest: "sha256:" + strings.Repeat("1", 64), CorrectedResultDigest: "sha256:" + strings.Repeat("2", 64)})
	if result.Digest != before || len(result.Impacts) != 1 {
		t.Fatal("prior restatement changed after request mutation")
	}
	if err := result.Verify(); err != nil {
		t.Fatalf("unchanged result Verify=%v", err)
	}
	result.Prior.Sequence++
	if err := result.Verify(); !errors.Is(err, ErrRestatementInvalid) {
		t.Fatalf("mutated sealed result Verify=%v", err)
	}
}
