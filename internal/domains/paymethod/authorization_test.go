package paymethod

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

type authorizationStepUpPresenter struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (p *authorizationStepUpPresenter) Present(_ context.Context, _ stepup.Proof, _ stepup.Operation, _ *trust.Principal, _ stepup.Requirement) (stepup.Outcome, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.err != nil {
		return "", p.err
	}
	return stepup.OutcomeExecuted, nil
}

func authorizationPrincipal(t *testing.T, at time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-1"), Subject: "requester", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-1", IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour), CredentialDigest: "sha256:principal",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func authorizedDestination(t *testing.T, id, token string, verified bool) Destination {
	t.Helper()
	d := validDestination(t, id)
	d.GovernedRef = token
	d.ProviderRef = ""
	d.TokenRef = ""
	if verified {
		d.Verification = VerificationVerified
		d.VerificationState = VerificationVerified
		d.VerificationDigest = "sha256:" + strings.Repeat("a", 64)
	}
	d.CanonicalDigest = ""
	got, err := NewDestination(d)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func authorizationRequest(t *testing.T) (ChangeAuthorizationRequest, *authorizationStepUpPresenter, time.Time) {
	t.Helper()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	current := authorizedDestination(t, "destination-1", "vault-token:old", true)
	proposed, err := current.NewRevision(Destination{
		GovernedRef: "vault-token:new", DisplayHint: "••••5678", Currency: "USD", CountryCode: "US",
		Rail: RailACH, Risk: RiskMedium, Verification: VerificationVerified, VerificationState: VerificationVerified,
		VerificationDigest: "sha256:" + strings.Repeat("b", 64), Effective: current.Effective,
	})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := NewDestinationChange(current, proposed, "proposal-1", "requester", "approver", proposed.Effective)
	if err != nil {
		t.Fatal(err)
	}
	changeRequest := paymethodChangeRequest(t)
	changeRequest.BeforeDigest = current.CanonicalDigest
	changeRequest.AfterDigest = proposed.CanonicalDigest
	changeRequest.RequestedAt = at.Add(-48 * time.Hour)
	changeRequest.CoolingOff = 24 * time.Hour
	dispatcher := &paymethodConfirmationDispatcher{}
	started, err := StartBankDetailChange(changeRequest, paymethodContactSource{endpoint: paymethodContact(t)}, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := started.Confirm(at.Add(-36*time.Hour), "sha256:"+strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	proof := stepup.Proof{
		Tenant: values.TenantId("tenant-1"), Subject: "requester", SessionRef: "session-1", Assurance: trust.AssuranceHigh,
		Action: stepup.ActionApprove, ProposalID: proposal.CanonicalDigest, Scopes: []string{"payment_destination:" + proposal.DestinationID},
	}
	presenter := &authorizationStepUpPresenter{}
	return ChangeAuthorizationRequest{
		CurrentDestination: current, ProposedDestination: proposed, Proposal: proposal,
		CurrentProposal: ProposalRevision{ID: proposal.ID, Digest: proposal.CanonicalDigest, Revision: 1}, BankDetailChange: confirmed,
		Principal: authorizationPrincipal(t, at), StepUpProof: proof, StepUpPresenter: presenter,
		RiskDecision:           RiskDecision{Decision: RiskRelease, RuleVersion: "risk/2026.1", AssessmentDigest: "sha256:" + strings.Repeat("d", 64), ProposalDigest: proposal.CanonicalDigest, EvaluatedAt: at.Add(-time.Minute), ValidUntil: at.Add(time.Hour)},
		VerificationFreshUntil: at.Add(time.Hour), PayrollWindow: PayrollWindow{Reference: "payroll-window-1"}, At: at,
	}, presenter, at
}

func TestPayMethodChangeRequiresStepUpRiskDecisionAndCurrentProposal(t *testing.T) {
	req, presenter, _ := authorizationRequest(t)
	got, err := AuthorizeChange(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AuthorizationAuthorized || !got.Activated || got.Reason != ReasonAuthorizationReady || got.CanonicalDigest == "" {
		t.Fatalf("authorization = %+v", got)
	}
	if presenter.calls != 1 {
		t.Fatalf("step-up calls = %d, want 1", presenter.calls)
	}

	cases := []struct {
		name   string
		mutate func(*ChangeAuthorizationRequest)
		status AuthorizationStatus
		reason string
	}{
		{"missing step-up", func(r *ChangeAuthorizationRequest) { r.StepUpPresenter = nil }, AuthorizationStepUpRequired, ReasonStepUpMissing},
		{"risk review", func(r *ChangeAuthorizationRequest) { r.RiskDecision.Decision = RiskHold }, AuthorizationReviewRequired, ReasonRiskReview},
		{"fraud block", func(r *ChangeAuthorizationRequest) { r.RiskDecision.Decision = RiskReject }, AuthorizationBlocked, ReasonFraudBlocked},
		{"stale proposal", func(r *ChangeAuthorizationRequest) { r.CurrentProposal.Digest = "sha256:" + strings.Repeat("e", 64) }, AuthorizationReplanRequired, ReasonProposalNotCurrent},
		{"protected payroll window", func(r *ChangeAuthorizationRequest) { r.PayrollWindow.Protected = true }, AuthorizationBlocked, ReasonProtectedPayrollWindow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseReq, _, _ := authorizationRequest(t)
			tc.mutate(&caseReq)
			decision, err := AuthorizeChange(caseReq)
			if err != nil || decision.Status != tc.status || decision.Reason != tc.reason || decision.Activated {
				t.Fatalf("decision=%+v err=%v", decision, err)
			}
		})
	}
}

func TestTodo_PAYMETHOD_002_Property(t *testing.T) {
	req, _, at := authorizationRequest(t)
	for _, mutate := range []func(*ChangeAuthorizationRequest){
		func(r *ChangeAuthorizationRequest) { r.VerificationFreshUntil = at },
		func(r *ChangeAuthorizationRequest) { r.RiskDecision.ValidUntil = at },
		func(r *ChangeAuthorizationRequest) { r.BankDetailChange.AvailableAt = at.Add(time.Hour) },
	} {
		caseReq := req
		mutate(&caseReq)
		decision, err := AuthorizeChange(caseReq)
		if err != nil {
			continue
		}
		if decision.Activated {
			t.Fatalf("unsafe request activated: %+v", decision)
		}
	}
}

func TestTodo_PAYMETHOD_002_Golden(t *testing.T) {
	first, _, _ := authorizationRequest(t)
	a, err := AuthorizeChange(first)
	if err != nil {
		t.Fatal(err)
	}
	second, _, _ := authorizationRequest(t)
	b, err := AuthorizeChange(second)
	if err != nil {
		t.Fatal(err)
	}
	if a.CanonicalDigest == "" || a.CanonicalDigest != b.CanonicalDigest || a.Explain() != b.Explain() {
		t.Fatalf("non-deterministic authorization: a=%+v b=%+v", a, b)
	}
}

func TestTodo_PAYMETHOD_002_Race(t *testing.T) {
	req, presenter, _ := authorizationRequest(t)
	const workers = 12
	results := make(chan AuthorizationDecision, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			decision, err := AuthorizeChange(req)
			results <- decision
			errs <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if decision := <-results; !decision.Activated {
			t.Fatalf("concurrent authorization refused: %+v", decision)
		}
	}
	presenter.mu.Lock()
	defer presenter.mu.Unlock()
	if presenter.calls != workers {
		t.Fatalf("step-up calls = %d, want %d", presenter.calls, workers)
	}
}

func TestTodo_PAYMETHOD_002_Fault(t *testing.T) {
	req, _, _ := authorizationRequest(t)
	req.StepUpPresenter = &authorizationStepUpPresenter{err: errors.New("step-up unavailable")}
	decision, err := AuthorizeChange(req)
	if err != nil || decision.Status != AuthorizationStepUpRequired || decision.Activated {
		t.Fatalf("fault decision=%+v err=%v", decision, err)
	}
	bad := req
	bad.RiskDecision.Decision = "UNKNOWN"
	if _, err := AuthorizeChange(bad); !errors.Is(err, ErrInvalidAuthorization) {
		t.Fatalf("unknown risk error = %v", err)
	}
}

func TestTodo_PAYMETHOD_002_Security(t *testing.T) {
	req, _, _ := authorizationRequest(t)
	req.ProposedDestination.RawBankDetail = "123456789012"
	if _, err := AuthorizeChange(req); err == nil {
		t.Fatal("raw bank detail was accepted")
	}
	decision, err := AuthorizeChange(func() ChangeAuthorizationRequest { r, _, _ := authorizationRequest(t); return r }())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"requester", "approver", "worker-1", "destination-1", "123456789012"} {
		if strings.Contains(decision.Explain(), secret) {
			t.Fatalf("explanation leaked %q: %s", secret, decision.Explain())
		}
	}
}

func TestTodo_PAYMETHOD_002_Conformance(t *testing.T) {
	req, _, _ := authorizationRequest(t)
	op := StepUpOperation(req)
	if op.Action != stepup.ActionApprove || op.ProposalID != req.Proposal.CanonicalDigest || op.Tenant.String() != req.BankDetailChange.TenantID || op.Risk != stepup.RiskCritical {
		t.Fatalf("step-up operation = %+v", op)
	}
	if ExplainAuthorization() == "" {
		t.Fatal("authorization explanation is empty")
	}
}

func TestTodo_PAYMETHOD_002_Mutation(t *testing.T) {
	req, _, _ := authorizationRequest(t)
	before, err := AuthorizeChange(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Proposal.ProposedDigest = req.CurrentDestination.CanonicalDigest
	decision, err := AuthorizeChange(req)
	if err == nil || decision.Activated || before.CanonicalDigest == decision.CanonicalDigest {
		t.Fatalf("mutated proposal was accepted: before=%+v after=%+v err=%v", before, decision, err)
	}
}
