package partnerapp

import (
	"errors"
	"testing"
	"time"
)

func securityReviewRequest(at time.Time) SecurityReviewRequest {
	return SecurityReviewRequest{
		Submitter: "requester-1", OwnerRef: "owner:payroll", Reviewer: "reviewer-2", SignatureRef: "signature:review-1", ReviewedAt: at, ValidFor: 24 * time.Hour,
		ScopeRefs: []string{"tenant-a/payroll"}, DestinationRefs: []string{"destination:payroll"}, ProcessorRefs: []string{"processor:acme"}, ResidencyRefs: []string{"region:us"},
		RetentionPolicyRef: "retention:30d", ThreatModelRef: "threat:app-payroll", VulnerabilityRef: "vuln:app-payroll",
		Items: []ReviewItem{
			{Control: ControlDataMinimization, Finding: FindingPass, EvidenceRef: "evidence:min"},
			{Control: ControlRetention, Finding: FindingPass, EvidenceRef: "evidence:retention"},
			{Control: ControlSubProcessors, Finding: FindingPass, EvidenceRef: "evidence:processors"},
			{Control: ControlBreachNotification, Finding: FindingPass, EvidenceRef: "evidence:breach"},
			{Control: ControlEncryptionTransit, Finding: FindingPass, EvidenceRef: "evidence:transit"},
			{Control: ControlEncryptionAtRest, Finding: FindingPass, EvidenceRef: "evidence:rest"},
		},
	}
}

func TestTodo_APP_002(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	app := partnerApplication()
	draft, err := NewDraft(app, partnerVersionRequest(), partnerAgreement())
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := draft.RecordReview("reviewer-1", true, "application review")
	if err != nil {
		t.Fatal(err)
	}
	review, err := NewSecurityReview(reviewed, securityReviewRequest(at))
	if err != nil {
		t.Fatal(err)
	}
	if review.Digest == "" || review.Explain() == "" {
		t.Fatalf("review=%+v", review)
	}
	if err := review.ValidAt(at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	checked, err := reviewed.ActivateWithSecurityReview(review, at.Add(time.Hour))
	if err != nil || checked.State != StateActive {
		t.Fatalf("checked=%+v err=%v", checked, err)
	}
}

func TestTodo_APP_002_Security(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	draft, err := NewDraft(partnerApplication(), partnerVersionRequest(), partnerAgreement())
	if err != nil {
		t.Fatal(err)
	}
	request := securityReviewRequest(at)
	request.Reviewer = request.Submitter
	if _, err := NewSecurityReview(draft, request); !errors.Is(err, ErrInvalidSecurityReview) {
		t.Fatalf("self-review error=%v", err)
	}
	request = securityReviewRequest(at)
	request.Items[0].Finding = FindingFail
	review, err := NewSecurityReview(draft, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.ActivateWithSecurityReview(review, at.Add(time.Hour)); !errors.Is(err, ErrMandatoryReviewFailed) {
		t.Fatalf("failed mandatory control error=%v", err)
	}
	request = securityReviewRequest(at)
	request.ValidFor = time.Hour
	review, err = NewSecurityReview(draft, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.ActivateWithSecurityReview(review, at.Add(time.Hour)); !errors.Is(err, ErrReviewExpired) {
		t.Fatalf("expired review error=%v", err)
	}
}
