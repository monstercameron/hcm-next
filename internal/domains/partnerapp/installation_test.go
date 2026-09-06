package partnerapp

import (
	"errors"
	"testing"
	"time"
)

func app003ActiveVersion(t *testing.T) ApplicationVersion {
	t.Helper()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	draft, err := NewDraft(partnerApplication(), partnerVersionRequest(), partnerAgreement())
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := draft.RecordReview("app-reviewer", true, "application version reviewed")
	if err != nil {
		t.Fatal(err)
	}
	reviewReq := securityReviewRequest(at)
	reviewReq.Reviewer = "security-reviewer"
	reviewReq.ScopeRefs = []string{"tenant-a", "org:finance", "population:payroll", "PAYROLL", "payroll.amount", "purpose:payroll", "payroll.read"}
	securityReview, err := NewSecurityReview(reviewed, reviewReq)
	if err != nil {
		t.Fatal(err)
	}
	active, err := reviewed.ActivateWithSecurityReview(securityReview, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func app003Review(t *testing.T, version ApplicationVersion, at time.Time) DataProcessingReview {
	t.Helper()
	req := securityReviewRequest(at)
	req.Reviewer = "installation-security-reviewer"
	req.ScopeRefs = []string{"tenant-a", "org:finance", "population:payroll", "PAYROLL", "payroll.amount", "purpose:payroll", "payroll.read"}
	review, err := NewSecurityReview(version, req)
	if err != nil {
		t.Fatal(err)
	}
	return review
}

func app003Request() InstallationRequest {
	return InstallationRequest{
		InstallationID:    "install-payroll-1",
		TenantScope:       "tenant-a",
		OrganizationScope: "org:finance",
		PopulationScope:   "population:payroll",
		DataClasses:       []string{"PAYROLL"},
		FieldScopes:       []string{"payroll.amount"},
		Purpose:           "purpose:payroll",
		Capabilities:      []string{"payroll.read"},
		Requester:         "installer-1",
	}
}

func TestTodo_APP_003(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	version := app003ActiveVersion(t)
	requested, requestedEvent, err := RequestGrant(version, app003Request())
	if err != nil {
		t.Fatal(err)
	}
	if requested.State != GrantRequested || requestedEvent.State != GrantRequested || requestedEvent.Digest == "" {
		t.Fatalf("requested=%+v event=%+v", requested, requestedEvent)
	}
	if err := requestedEvent.Verify(); err != nil {
		t.Fatalf("requested event: %v", err)
	}

	store := NewInstallationStore()
	if err := store.Append(requested, requestedEvent); err != nil {
		t.Fatal(err)
	}
	approved, approvedEvent, err := requested.Approve("approver-1", "evidence:approval-1")
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != GrantApproved || approved.ApproverEvidence.Approver != "approver-1" {
		t.Fatalf("approved=%+v", approved)
	}
	if err := store.Append(approved, approvedEvent); err != nil {
		t.Fatal(err)
	}

	review := app003Review(t, version, at)
	active, activeEvent, err := approved.ActivateWithReview(review, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if active.State != GrantActive || active.ReviewRef != review.Digest || activeEvent.State != GrantActive {
		t.Fatalf("active=%+v event=%+v", active, activeEvent)
	}
	if err := store.Append(active, activeEvent); err != nil {
		t.Fatal(err)
	}

	decision, err := active.CheckScope(ScopeQuery{
		TenantScope: "tenant-a", OrganizationScope: "org:finance", PopulationScope: "population:payroll",
		FieldScopes: []string{"payroll.amount"}, Purpose: "purpose:payroll", Capabilities: []string{"payroll.read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.InstallationDigest != active.Digest || decision.Digest == "" {
		t.Fatalf("decision=%+v", decision)
	}
	if decision.Explain() == "" || active.Explain().InstallationDigest != active.Digest {
		t.Fatal("audit explanations are empty or unbound")
	}

	history := store.Revisions(requested.InstallationID)
	if len(history) != 3 || history[2].State != GrantActive || len(store.Events(requested.InstallationID)) != 3 {
		t.Fatalf("history=%+v events=%+v", history, store.Events(requested.InstallationID))
	}
}

func TestTodo_APP_003_Security(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	version := app003ActiveVersion(t)
	request := app003Request()
	requested, _, err := RequestGrant(version, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := requested.Approve(request.Requester, "evidence:self"); !errors.Is(err, ErrGrantApproval) {
		t.Fatalf("self approval error=%v", err)
	}
	approved, _, err := requested.Approve("approver-1", "evidence:approval-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := approved.Activate(DataProcessingReview{}, at); !errors.Is(err, ErrInstallationReview) {
		t.Fatalf("missing review error=%v", err)
	}

	wider := request
	wider.InstallationID = "install-payroll-wide"
	wider.FieldScopes = []string{"payroll.amount", "payroll.bank_account"}
	widerRequested, _, err := RequestGrant(version, wider)
	if err != nil {
		t.Fatal(err)
	}
	widerApproved, _, err := widerRequested.Approve("approver-2", "evidence:approval-2")
	if err != nil {
		t.Fatal(err)
	}
	review := app003Review(t, version, at)
	if _, _, err := widerApproved.Activate(review, at.Add(time.Hour)); !errors.Is(err, ErrReviewScope) {
		t.Fatalf("wider scope error=%v", err)
	}

	mutated := approved
	mutated.FieldScopes[0] = "payroll.bank_account"
	if err := mutated.Verify(); !errors.Is(err, ErrInvalidInstallation) {
		t.Fatalf("mutation verification error=%v", err)
	}
	denied, err := CheckScope(requested, ScopeQuery{TenantScope: request.TenantScope, OrganizationScope: request.OrganizationScope, PopulationScope: request.PopulationScope, FieldScopes: request.FieldScopes, Purpose: request.Purpose})
	if err != nil {
		t.Fatal(err)
	}
	if denied.Allowed || denied.Reason != "installation_not_active" || denied.InstallationDigest != requested.Digest {
		t.Fatalf("requested scope decision=%+v", denied)
	}
}
