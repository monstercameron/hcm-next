package a11y

import (
	"errors"
	"testing"
	"time"
)

// TestCustomer004AdoptionMatrix is an isolated release-evidence check for the
// CUSTOMER-004 adoption package. The role labels intentionally live in the
// flow names: the a11y matrix is the qualification boundary, while adoption
// owns which journeys must be demonstrated for each role.
func TestCustomer004AdoptionMatrix(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	flows := []Flow{
		Flow("administrator_setup"),
		Flow("approver_decision"),
		Flow("employee_request"),
		Flow("support_triage"),
		Flow("help_recovery"),
		Flow("training_evidence"),
	}
	environments := []Environment{
		{AssistiveTechnology: "NVDA", Browser: "Firefox", OS: "Windows", Locale: "en-US", Direction: "ltr", ZoomPercent: 200, InputMode: "keyboard"},
		{AssistiveTechnology: "VoiceOver", Browser: "Safari", OS: "macOS", Locale: "en-US", Direction: "ltr", ZoomPercent: 400, InputMode: "voice"},
		{AssistiveTechnology: "TalkBack", Browser: "Chrome", OS: "Android", Locale: "en-US", Direction: "ltr", ZoomPercent: 200, InputMode: "touch"},
	}
	evidence := make([]Evidence, 0, len(environments)*len(flows))
	for _, environment := range environments {
		for _, flow := range flows {
			evidence = append(evidence, Evidence{
				EnvironmentKey: environment.Key(), Flow: flow,
				TaskDigest: "training:customer-004/v1", ResultDigest: "training:customer-004/v1",
				FocusPreserved: true, StateAnnounced: true, ErrorAssociated: true,
				RecordedAt: now,
			})
		}
	}

	matrix := Matrix{
		ID: "customer-004-adoption", Version: "2026.09",
		Environments: environments, Flows: flows, Evidence: evidence,
		ContinuityRoute: "help-center-to-support-with-preserved-identity-and-deadline",
	}
	report, err := matrix.Evaluate(now.Add(time.Minute))
	if err != nil || !report.Passed || len(report.Missing) != 0 || report.MatrixDigest == "" {
		t.Fatalf("CUSTOMER-004 matrix did not qualify: report=%+v err=%v", report, err)
	}

	// Expiry is a hard release gate: a waiver may be used only while active.
	matrix.Evidence[0].Waiver = "customer-004-training-catch-up"
	matrix.Evidence[0].WaiverExpiresAt = now.Add(30 * time.Second)
	if _, err := matrix.Evaluate(now.Add(time.Minute)); !errors.Is(err, ErrInvalidMatrix) {
		t.Fatalf("expired adoption waiver accepted: %v", err)
	}

	// Training evidence must be complete for every role/environment pair.
	matrix.Evidence[0].Waiver = ""
	matrix.Evidence[0].WaiverExpiresAt = time.Time{}
	matrix.Evidence[0].ResultDigest = "training:customer-004/mismatch"
	report, err = matrix.Evaluate(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || len(report.Missing) == 0 {
		t.Fatalf("incomplete training evidence passed adoption gate: %+v", report)
	}
}
