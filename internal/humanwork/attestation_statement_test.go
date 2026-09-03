package humanwork

import (
	"github.com/monstercameron/hcm-next/internal/trust"
	"testing"
	"time"
)

func validAttestation() AttestationStatement {
	return AttestationStatement{ID: "policy-attestation", Version: "1", Text: "I attest that the recorded hours are accurate.", Locale: "en-US", SubjectRef: "worker:123", Facts: []AttestationFact{{Ref: "timecard:123", Version: "7"}}, Period: AttestationPeriod{From: time.Unix(0, 0), To: time.Unix(3600, 0)}, Purpose: "payroll certification", RequiredAssurance: trust.AssuranceSubstantial, ResponseOptions: []AttestationResponseOption{{Code: "affirm", Text: "Yes"}, {Code: "refuse", Text: "No"}}, Evidence: []AttestationEvidenceRequirement{{Kind: "timecard", Ref: "timecard:123", Required: true}}, CorrectionPolicy: AttestationCorrectionPolicy{Allowed: true, PolicyRef: "correction/v1"}, RefusalPolicy: AttestationRefusalPolicy{Allowed: true, PolicyRef: "refusal/v1"}}
}

func TestAttestationStatementPublishAndTamper(t *testing.T) {
	s, err := validAttestation().Publish()
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != AttestationPublished || !s.VerifyDigest() {
		t.Fatalf("published statement did not verify: %+v", s)
	}
	s.Text = "tampered"
	if s.VerifyDigest() {
		t.Fatal("tampered statement verified")
	}
}

func TestAttestationStatementRequiresCompleteContract(t *testing.T) {
	s := validAttestation()
	cases := []struct {
		name   string
		mutate func(*AttestationStatement)
	}{{"text", func(s *AttestationStatement) { s.Text = "" }}, {"locale", func(s *AttestationStatement) { s.Locale = "" }}, {"facts", func(s *AttestationStatement) { s.Facts = nil }}, {"purpose", func(s *AttestationStatement) { s.Purpose = "" }}, {"assurance", func(s *AttestationStatement) { s.RequiredAssurance = trust.AssuranceUnspecified }}, {"responses", func(s *AttestationStatement) { s.ResponseOptions = nil }}, {"evidence", func(s *AttestationStatement) { s.Evidence = nil }}, {"correction policy", func(s *AttestationStatement) { s.CorrectionPolicy = AttestationCorrectionPolicy{Allowed: true} }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := s
			tc.mutate(&x)
			if err := x.Validate(); err == nil {
				t.Fatal("Validate succeeded")
			}
		})
	}
}
