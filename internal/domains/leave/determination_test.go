package leave

import (
	"strings"
	"testing"

	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

func determinationInput() DeterminationInput {
	return DeterminationInput{
		ProposalDigest: "sha256:proposal", LegalRelease: "ca-2026.1",
		Programs: []ProgramResult{
			{ProgramID: "fmla", Authority: AuthorityStatutory, Release: "fmla-2026.1", RuleID: "tenure-12mo", RuleVersion: "v3", Result: ProgramEligible},
		},
		Interval: "2026-09-01/2026-09-08", PaidHours: 32, UnpaidHours: 8,
		Rights: []string{"job-protection", "benefit-continuation"}, Obligations: []string{"notice:return-date"},
		Explanation: "Eight-day leave: 32 paid hours and 8 unpaid under FMLA.",
		TemplateID:  "determination-letter", TemplateVersion: "v4", TemplateApproved: true,
		Locale: "en-US", Recipient: "worker:w1", BlockingNoticeID: "notice-1",
	}
}

func satisfiedAssessment() delivery.NoticeAssessment {
	assessment, err := delivery.AssessRequirement(delivery.NoticeRequirement{
		ID: "notice-1", Recipient: "worker:w1",
		RecipientVerified: true, RecipientProof: "verify:kyc-9",
		ContentDigest: "sha256:determination", ContentVersion: "v4",
		Timestamp: 1700000000, JurisdictionRule: "ca-notice/v1",
		AckProof: "ack:w1", SignatureDigest: "sha256:ceremony",
	}, map[string]bool{"ca-notice/v1": true})
	if err != nil {
		panic(err)
	}
	return assessment
}

func TestTodo_LEAVE_014(t *testing.T) {
	determination, err := RenderDetermination(determinationInput())
	if err != nil {
		t.Fatalf("RenderDetermination: %v", err)
	}
	// The artifact binds every required dimension.
	for _, want := range []string{"programs:fmla:ELIGIBLE", "interval:2026-09-01/2026-09-08", "paid:32 unpaid:8", "rights:job-protection", "obligations:notice:return-date", "template:determination-letter@v4", "legal:ca-2026.1"} {
		if !strings.Contains(determination.Artifact, want) {
			t.Fatalf("artifact misses %q:\n%s", want, determination.Artifact)
		}
	}
	// No medical evidence reaches the worker artifact or the manager copy.
	for _, forbidden := range []string{"J06", "diagnosis", "medical-document"} {
		if strings.Contains(determination.Artifact, forbidden) || strings.Contains(determination.ManagerCopy, forbidden) {
			t.Fatalf("medical detail %q exposed", forbidden)
		}
	}
	// Satisfied blocking notice with acknowledgement opens leave start.
	delivered, err := DeliverDetermination(determination, determinationInput(), satisfiedAssessment(), true, true)
	if err != nil {
		t.Fatalf("DeliverDetermination: %v", err)
	}
	if delivered.DeliveryState != DeterminationAcknowledged || !delivered.LeaveStartAllowed {
		t.Fatalf("delivered=%+v", delivered)
	}
	if err := delivered.Verify(determinationInput()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: mutable/unapproved templates, hollow versions and unsatisfied
	// blocking notices refuse leave start.
	unapproved := determinationInput()
	unapproved.TemplateApproved = false
	if _, err := RenderDetermination(unapproved); err == nil {
		t.Fatal("unapproved template rendered")
	}
	unversioned := determinationInput()
	unversioned.TemplateVersion = ""
	if _, err := RenderDetermination(unversioned); err == nil {
		t.Fatal("unversioned template rendered")
	}
	rightless := determinationInput()
	rightless.Rights = nil
	if _, err := RenderDetermination(rightless); err == nil {
		t.Fatal("rightless determination rendered")
	}
	blocked, err := DeliverDetermination(determination, determinationInput(), delivery.NoticeAssessment{RequirementID: "notice-1", Outcome: delivery.NoticeUnsatisfied}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.LeaveStartAllowed || blocked.DeliveryState != DeterminationFailed {
		t.Fatalf("blocked=%+v", blocked)
	}
}

func TestTodo_LEAVE_014_Property(t *testing.T) {
	first, err := RenderDetermination(determinationInput())
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderDetermination(determinationInput())
	if err != nil || first.Digest != second.Digest {
		t.Fatal("determination is not deterministic")
	}
	// Delivery without acknowledgement holds leave start closed.
	pending, err := DeliverDetermination(first, determinationInput(), satisfiedAssessment(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if pending.DeliveryState != DeterminationDelivered || pending.LeaveStartAllowed {
		t.Fatalf("pending=%+v", pending)
	}
	// Provider acceptance alone changes nothing: the assessment decides.
	accepted, err := DeliverDetermination(first, determinationInput(), satisfiedAssessment(), true, false)
	if err != nil || accepted.DeliveryState != DeterminationDelivered {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
}

func TestTodo_LEAVE_014_Integration(t *testing.T) {
	determination, err := RenderDetermination(determinationInput())
	if err != nil {
		t.Fatal(err)
	}
	// The blocking notice resolves through the real assessment boundary,
	// and a delivery failure reconciles to bounded fallback work.
	assessment, err := delivery.AssessRequirement(delivery.NoticeRequirement{
		ID: "notice-1", Recipient: "worker:w1",
		RecipientVerified: true, RecipientProof: "verify:kyc-9",
		ContentDigest: "sha256:determination", ContentVersion: "v4",
		Timestamp: 1700000000, JurisdictionRule: "ca-notice/v1",
		SignatureDigest: "sha256:ceremony",
	}, map[string]bool{"ca-notice/v1": true})
	if err != nil {
		t.Fatalf("AssessRequirement: %v", err)
	}
	if assessment.Outcome != delivery.NoticeRepairRequired {
		t.Fatalf("ackless assessment = %q", assessment.Outcome)
	}
	held, err := DeliverDetermination(determination, determinationInput(), assessment, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if held.LeaveStartAllowed {
		t.Fatal("unacknowledged notice opened leave start")
	}
	reconciliation, err := delivery.Reconcile("intent-14", "worker:w1", "INTERNAL", "determination", delivery.FailureOutage, 2, 100, delivery.RetryPolicy{MaxAttempts: 5, BackoffTicks: 30, FallbackAfter: 3})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if reconciliation.Outcome != delivery.ReconcileFallbackInbox || reconciliation.Obligation.Satisfied {
		t.Fatalf("reconciliation=%+v", reconciliation)
	}
}

func TestTodo_LEAVE_014_Security(t *testing.T) {
	determination, err := RenderDetermination(determinationInput())
	if err != nil {
		t.Fatal(err)
	}
	// The manager copy carries operational facts only.
	if strings.Contains(determination.ManagerCopy, "fmla") || strings.Contains(determination.ManagerCopy, "obligation") {
		t.Fatalf("manager copy leaks program detail: %q", determination.ManagerCopy)
	}
	// Forged determinations never verify.
	forged := determination
	forged.LeaveStartAllowed = true
	if err := forged.Verify(determinationInput()); err == nil {
		t.Fatal("forged determination verified")
	}
}

func TestTodo_LEAVE_014_Mutation(t *testing.T) {
	base, err := RenderDetermination(determinationInput())
	if err != nil {
		t.Fatal(err)
	}
	// Program change re-identifies the artifact.
	changed := determinationInput()
	changed.Programs = []ProgramResult{{ProgramID: "fmla", Authority: AuthorityStatutory, Release: "fmla-2026.1", RuleID: "tenure-12mo", RuleVersion: "v3", Result: ProgramConditional}}
	rebuilt, err := RenderDetermination(changed)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Digest == base.Digest {
		t.Fatal("program mutation kept the determination digest")
	}
	// Satisfying the notice flips the obligation signal.
	delivered, err := DeliverDetermination(base, determinationInput(), satisfiedAssessment(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !delivered.LeaveStartAllowed || delivered.Digest == base.Digest {
		t.Fatalf("delivered=%+v", delivered)
	}
}
