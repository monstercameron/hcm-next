package access

import (
	"errors"
	"testing"
	"time"
)

func lifecycleRequest(risk RiskClass) AccessRequestInput {
	return AccessRequestInput{RequestID: "request-1", Tenant: "tenant-1", Requester: "requester", Beneficiary: "worker", Application: "payroll", AccountID: "account-1", EntitlementID: "payroll-admin", Scope: "payroll:read", Purpose: "work", Justification: "on-call", Risk: risk, ProposalDigest: "proposal-v1", Snapshot: LifecycleSnapshot{RiskRevision: "risk-2", ManagerRevision: "manager-4", EntitlementRevision: "ent-9"}, RequestedAt: time.Unix(100, 0).UTC(), IdempotencyKey: "request-key"}
}

func approvals(req AccessRequest, names ...string) []Approval {
	out := make([]Approval, 0, len(names))
	for i, name := range names {
		out = append(out, Approval{ApprovalID: "approval-" + name, RequestID: req.RequestID, Approver: name, Role: "security", ProposalDigest: req.ProposalDigest, Snapshot: req.Snapshot, Approved: true, At: time.Unix(int64(110+i), 0).UTC()})
	}
	return out
}

func TestAccessDecisionLifecycleRequiresCurrentRiskApprovalAndSeparationOfDuties(t *testing.T) {
	req, err := RequestAccess(lifecycleRequest(RiskPrivileged))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decide(DecisionRequest{Request: req, Action: ActionGrant, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: approvals(req, "security-1"), At: time.Unix(120, 0), IdempotencyKey: "grant-1"}); !errors.Is(err, ErrInsufficientApproval) {
		t.Fatalf("quorum error = %v", err)
	}
	good := DecisionRequest{Request: req, Action: ActionGrant, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: approvals(req, "security-1", "security-2"), At: time.Unix(120, 0), IdempotencyKey: "grant-1"}
	decision, err := Decide(good)
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != StateGranted || decision.EffectKey == "" || decision.Digest == "" {
		t.Fatalf("decision = %#v", decision)
	}
	if _, err := Decide(DecisionRequest{Request: req, Action: ActionGrant, Actor: "workflow", CurrentSnapshot: LifecycleSnapshot{RiskRevision: "risk-3", ManagerRevision: "manager-4", EntitlementRevision: "ent-9"}, Approvals: approvals(req, "security-1", "security-2"), At: time.Unix(120, 0), IdempotencyKey: "grant-2"}); !errors.Is(err, ErrReplanRequired) {
		t.Fatalf("stale error = %v", err)
	}
}

func TestTodo_ACCESS_003_Property(t *testing.T) {
	req, err := RequestAccess(lifecycleRequest(RiskHigh))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		got, err := Decide(DecisionRequest{Request: req, Action: ActionGrant, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: approvals(req, "security"), At: time.Unix(120, 0), IdempotencyKey: "grant"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Digest == "" {
			t.Fatal("missing digest")
		}
	}
}

func TestTodo_ACCESS_003_Golden(t *testing.T) {
	req, _ := RequestAccess(lifecycleRequest(RiskHigh))
	d, err := Decide(DecisionRequest{Request: req, Action: ActionCertify, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: approvals(req, "reviewer"), CertifiedScope: req.Scope, At: time.Unix(120, 0), IdempotencyKey: "cert-1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != StateCertified || d.Explain() == "" {
		t.Fatalf("certification = %#v", d)
	}
}

func TestTodo_ACCESS_003_Security(t *testing.T) {
	req, _ := RequestAccess(lifecycleRequest(RiskHigh))
	bad := approvals(req, req.Requester)
	if _, err := Decide(DecisionRequest{Request: req, Action: ActionGrant, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: bad, At: time.Unix(120, 0), IdempotencyKey: "bad"}); !errors.Is(err, ErrInvalidLifecycle) {
		t.Fatalf("self approval = %v", err)
	}
	if _, err := Decide(DecisionRequest{Request: req, Action: ActionCertify, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: approvals(req, "reviewer"), CertifiedScope: "payroll:write", At: time.Unix(120, 0), IdempotencyKey: "bad-cert"}); !errors.Is(err, ErrCertificationMismatch) {
		t.Fatalf("scope = %v", err)
	}
}

func TestTodo_ACCESS_003_Integration(t *testing.T) {
	lifecycle := NewAccessDecisionLifecycle()
	req, err := lifecycle.SubmitRequest(lifecycleRequest(RiskHigh))
	if err != nil {
		t.Fatal(err)
	}
	in := DecisionRequest{Request: req, Action: ActionGrant, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: approvals(req, "security"), At: time.Unix(120, 0), IdempotencyKey: "grant-1"}
	a, err := lifecycle.AppendDecision(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := lifecycle.AppendDecision(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("idempotent replay changed decision")
	}
}

func TestTodo_ACCESS_003_Race(t *testing.T) {
	lifecycle := NewAccessDecisionLifecycle()
	req, err := lifecycle.SubmitRequest(lifecycleRequest(RiskHigh))
	if err != nil {
		t.Fatal(err)
	}
	in := DecisionRequest{Request: req, Action: ActionGrant, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: approvals(req, "security"), At: time.Unix(120, 0), IdempotencyKey: "grant-1"}
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { _, e := lifecycle.AppendDecision(in); done <- e }()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_ACCESS_003_Fault(t *testing.T) {
	req, _ := RequestAccess(lifecycleRequest(RiskHigh))
	in := DecisionRequest{Request: req, Action: ActionGrant, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: approvals(req, "security"), At: time.Unix(120, 0), IdempotencyKey: "grant-1"}
	in.Approvals[0].ProposalDigest = "moved"
	if _, err := Decide(in); !errors.Is(err, ErrReapprovalRequired) {
		t.Fatalf("proposal drift = %v", err)
	}
}

func TestTodo_ACCESS_003_Mutation(t *testing.T) {
	req, _ := RequestAccess(lifecycleRequest(RiskHigh))
	d, err := Decide(DecisionRequest{Request: req, Action: ActionRevoke, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: nil, At: time.Unix(120, 0), IdempotencyKey: "revoke-1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != StateRevoked {
		t.Fatalf("revoke = %#v", d)
	}
}
