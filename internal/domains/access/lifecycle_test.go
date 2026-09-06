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

func TestLifecycleVocabulary_AndRequestValidation(t *testing.T) {
	for _, state := range []LifecycleState{StateRequested, StateApproved, StateGranted, StateRevoked, StateCertified, StateRejected} {
		if !state.Valid() {
			t.Errorf("state %q rejected", state)
		}
	}
	if LifecycleState("UNKNOWN").Valid() {
		t.Fatal("unknown lifecycle state accepted")
	}
	for _, action := range []DecisionAction{ActionApprove, ActionGrant, ActionRevoke, ActionCertify} {
		if !action.Valid() {
			t.Errorf("action %q rejected", action)
		}
	}
	if DecisionAction("UNKNOWN").Valid() {
		t.Fatal("unknown decision action accepted")
	}

	tests := []struct {
		name   string
		mutate func(*AccessRequestInput)
	}{
		{"padded field", func(in *AccessRequestInput) { in.Tenant = " tenant-1" }},
		{"empty field", func(in *AccessRequestInput) { in.Purpose = "" }},
		{"invalid risk", func(in *AccessRequestInput) { in.Risk = RiskUnspecified }},
		{"incomplete snapshot", func(in *AccessRequestInput) { in.Snapshot.ManagerRevision = "" }},
		{"missing time", func(in *AccessRequestInput) { in.RequestedAt = time.Time{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := lifecycleRequest(RiskHigh)
			tt.mutate(&in)
			if _, err := RequestAccess(in); !errors.Is(err, ErrInvalidLifecycle) {
				t.Fatalf("error = %v, want ErrInvalidLifecycle", err)
			}
		})
	}
	for _, risk := range []RiskClass{RiskLow, RiskModerate, RiskHigh} {
		req, err := RequestAccess(lifecycleRequest(risk))
		if err != nil || req.RequiredQuorum != 1 || req.State != StateRequested || req.Digest == "" {
			t.Fatalf("ordinary request = %#v, %v", req, err)
		}
	}
	req, err := RequestAccess(lifecycleRequest(RiskPrivileged))
	if err != nil || req.RequiredQuorum != 2 {
		t.Fatalf("privileged quorum = %#v, %v", req, err)
	}
}

func TestDecide_RejectsStaleMalformedAndInsufficientApprovals(t *testing.T) {
	req, err := RequestAccess(lifecycleRequest(RiskHigh))
	if err != nil {
		t.Fatal(err)
	}
	base := DecisionRequest{Request: req, Action: ActionGrant, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: approvals(req, "security"), At: time.Unix(120, 0), IdempotencyKey: "grant"}
	tests := []struct {
		name   string
		mutate func(*DecisionRequest)
		want   error
	}{
		{"non actionable state", func(in *DecisionRequest) { in.Request.State = StateRejected }, ErrDecisionConflict},
		{"invalid action", func(in *DecisionRequest) { in.Action = "bad" }, ErrInvalidLifecycle},
		{"missing actor", func(in *DecisionRequest) { in.Actor = "" }, ErrInvalidLifecycle},
		{"missing decision time", func(in *DecisionRequest) { in.At = time.Time{} }, ErrInvalidLifecycle},
		{"missing idempotency", func(in *DecisionRequest) { in.IdempotencyKey = "" }, ErrInvalidLifecycle},
		{"incomplete current snapshot", func(in *DecisionRequest) { in.CurrentSnapshot.RiskRevision = "" }, ErrReapprovalRequired},
		{"stale current snapshot", func(in *DecisionRequest) { in.CurrentSnapshot.RiskRevision = "other" }, ErrReplanRequired},
		{"self approval", func(in *DecisionRequest) { in.Approvals = approvals(req, req.Requester) }, ErrInvalidLifecycle},
		{"beneficiary approval", func(in *DecisionRequest) { in.Approvals = approvals(req, req.Beneficiary) }, ErrInvalidLifecycle},
		{"approval request mismatch", func(in *DecisionRequest) { in.Approvals[0].RequestID = "other" }, ErrReapprovalRequired},
		{"approval proposal mismatch", func(in *DecisionRequest) { in.Approvals[0].ProposalDigest = "other" }, ErrReapprovalRequired},
		{"approval snapshot mismatch", func(in *DecisionRequest) {
			in.Approvals[0].Snapshot = LifecycleSnapshot{RiskRevision: "other", ManagerRevision: "manager-4", EntitlementRevision: "ent-9"}
		}, ErrReapprovalRequired},
		{"approval not approved", func(in *DecisionRequest) { in.Approvals[0].Approved = false }, ErrInsufficientApproval},
		{"approval missing identity", func(in *DecisionRequest) { in.Approvals[0].Approver = "" }, ErrInvalidLifecycle},
		{"approval missing time", func(in *DecisionRequest) { in.Approvals[0].At = time.Time{} }, ErrInvalidLifecycle},
		{"duplicate approver", func(in *DecisionRequest) { in.Approvals = approvals(req, "security", "security") }, ErrInvalidLifecycle},
		{"insufficient quorum", func(in *DecisionRequest) {
			privileged, _ := RequestAccess(lifecycleRequest(RiskPrivileged))
			in.Request = privileged
			in.Approvals = approvals(privileged, "security")
		}, ErrInsufficientApproval},
		{"certification scope mismatch", func(in *DecisionRequest) { in.Action = ActionCertify; in.CertifiedScope = "other" }, ErrCertificationMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base
			in.Approvals = append([]Approval(nil), base.Approvals...)
			tt.mutate(&in)
			if _, got := Decide(in); !errors.Is(got, tt.want) {
				t.Fatalf("error = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecide_ActionsStatesAndExplain(t *testing.T) {
	req, err := RequestAccess(lifecycleRequest(RiskHigh))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		action    DecisionAction
		state     LifecycleState
		certified string
		approvals []Approval
	}{
		{ActionApprove, StateApproved, "", approvals(req, "approver")},
		{ActionGrant, StateGranted, "", approvals(req, "approver")},
		{ActionRevoke, StateRevoked, "", nil},
		{ActionCertify, StateCertified, req.Scope, approvals(req, "approver")},
	} {
		d, err := Decide(DecisionRequest{Request: req, Action: tc.action, Actor: "workflow", CurrentSnapshot: req.Snapshot, Approvals: tc.approvals, CertifiedScope: tc.certified, At: time.Unix(120, 0), IdempotencyKey: string(tc.action)})
		if err != nil || d.State != tc.state || len(d.ApprovalIDs) != len(tc.approvals) || d.Digest == "" || d.EffectKey == "" {
			t.Fatalf("%s decision = %#v, %v", tc.action, d, err)
		}
		if d.Explain() == "" || Explain(d) != d.Explain() {
			t.Fatalf("%s explanation = %q", tc.action, d.Explain())
		}
	}
}

func TestAccessDecisionLifecycle_AtomicIdempotencyAndEntryPoints(t *testing.T) {
	var nilLifecycle *AccessDecisionLifecycle
	if _, err := nilLifecycle.SubmitRequest(lifecycleRequest(RiskHigh)); !errors.Is(err, ErrInvalidLifecycle) {
		t.Fatalf("nil submit = %v", err)
	}
	if _, err := nilLifecycle.AppendDecision(DecisionRequest{}); !errors.Is(err, ErrInvalidLifecycle) {
		t.Fatalf("nil append = %v", err)
	}
	lifecycle := NewAccessDecisionLifecycle()
	first, err := lifecycle.SubmitRequest(lifecycleRequest(RiskHigh))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := lifecycle.SubmitRequest(lifecycleRequest(RiskHigh))
	if err != nil || replay.Digest != first.Digest {
		t.Fatalf("request replay = %#v, %v", replay, err)
	}
	conflicting := lifecycleRequest(RiskHigh)
	conflicting.Purpose = "different-purpose"
	if _, err := lifecycle.SubmitRequest(conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("request conflict = %v", err)
	}

	base := DecisionRequest{Request: first, Actor: "workflow", CurrentSnapshot: first.Snapshot, Approvals: approvals(first, "approver"), At: time.Unix(120, 0), IdempotencyKey: "decision-key"}
	grant := base
	grant.Action = ActionGrant
	a, err := lifecycle.AppendDecision(grant)
	if err != nil {
		t.Fatal(err)
	}
	b, err := lifecycle.AppendDecision(grant)
	if err != nil || b.Digest != a.Digest {
		t.Fatalf("decision replay = %#v, %v", b, err)
	}
	changedKey := grant
	changedKey.IdempotencyKey = "different-key"
	changedKey.At = time.Unix(121, 0)
	if _, err := lifecycle.AppendDecision(changedKey); !errors.Is(err, ErrDecisionConflict) {
		t.Fatalf("decision conflict = %v", err)
	}

	for _, action := range []DecisionAction{ActionApprove, ActionGrant, ActionRevoke, ActionCertify} {
		in := base
		in.IdempotencyKey = "entry-" + string(action)
		in.CertifiedScope = first.Scope
		var got Decision
		switch action {
		case ActionApprove:
			got, err = lifecycle.ApproveAccess(in)
		case ActionGrant:
			got, err = lifecycle.GrantEntitlement(in)
		case ActionRevoke:
			in.Approvals = nil
			got, err = lifecycle.RevokeEntitlement(in)
		case ActionCertify:
			got, err = lifecycle.CertifyAccess(in)
		}
		if err != nil || got.Action != action {
			t.Fatalf("entry point %s = %#v, %v", action, got, err)
		}
	}
}
