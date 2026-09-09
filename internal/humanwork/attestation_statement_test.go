package humanwork

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
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

// TestTodo_ATTEST_001 is the PRIMARY matrix clause for the versioned
// statement definition. Publication is the only operation that creates an
// authoritative version: it validates the complete contract and records a
// digest over the exact statement content.
func TestTodo_ATTEST_001(t *testing.T) {
	s, err := validAttestation().Publish()
	if err != nil {
		t.Fatalf("ATTEST_001_REJECTED: valid statement was rejected: %v", err)
	}
	if s.Status != AttestationPublished {
		t.Fatalf("published statement has status %q, want %q", s.Status, AttestationPublished)
	}
	if s.Digest == "" || !s.VerifyDigest() {
		t.Fatalf("published statement has no verifiable content digest: %+v", s)
	}

	// A failed publication returns no statement that could accidentally be
	// selected as an authoritative version, and its typed error names the
	// offending field/state.
	bad := validAttestation()
	bad.Text = ""
	_, err = bad.Publish()
	if err == nil {
		t.Fatal("ATTEST_001_REJECTED: incomplete statement was published")
	}
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("publication error is not ErrInvalidAttestation: %v", err)
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Field != "text" {
		t.Fatalf("publication error does not identify text field: %v", err)
	}
}

// TestTodo_ATTEST_001_Mutation is the MUTATION matrix clause. Every content
// component participates in the digest, and every required publication field
// is independently fail-closed when removed or malformed.
func TestTodo_ATTEST_001_Mutation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AttestationStatement)
		field  string
	}{
		{"id", func(s *AttestationStatement) { s.ID = "" }, "id"},
		{"version", func(s *AttestationStatement) { s.Version = "" }, "version"},
		{"text", func(s *AttestationStatement) { s.Text = "" }, "text"},
		{"locale", func(s *AttestationStatement) { s.Locale = "" }, "locale"},
		{"subject", func(s *AttestationStatement) { s.SubjectRef = "" }, "subject_ref"},
		{"fact reference", func(s *AttestationStatement) { s.Facts[0].Ref = "" }, "facts[0]"},
		{"fact version", func(s *AttestationStatement) { s.Facts[0].Version = "" }, "facts[0]"},
		{"period", func(s *AttestationStatement) { s.Period.To = s.Period.From }, "period"},
		{"purpose", func(s *AttestationStatement) { s.Purpose = "" }, "purpose"},
		{"assurance", func(s *AttestationStatement) { s.RequiredAssurance = trust.AssuranceUnspecified }, "required_assurance"},
		{"response option text", func(s *AttestationStatement) { s.ResponseOptions[0].Text = "" }, "response_options[0]"},
		{"duplicate response code", func(s *AttestationStatement) { s.ResponseOptions[1].Code = s.ResponseOptions[0].Code }, "response_options[1].code"},
		{"evidence kind", func(s *AttestationStatement) { s.Evidence[0].Kind = "" }, "evidence[0].kind"},
		{"required evidence reference", func(s *AttestationStatement) { s.Evidence[0].Ref = "" }, "evidence[0].ref"},
		{"correction policy reference", func(s *AttestationStatement) { s.CorrectionPolicy.PolicyRef = "" }, "correction_policy.policy_ref"},
		{"refusal policy reference", func(s *AttestationStatement) { s.RefusalPolicy.PolicyRef = "" }, "refusal_policy.policy_ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validAttestation()
			tc.mutate(&s)
			_, err := s.Publish()
			if err == nil {
				t.Fatal("ATTEST_001_REJECTED: mutated statement was published")
			}
			var typed *Error
			if !errors.As(err, &typed) || typed.Field != tc.field {
				t.Fatalf("mutation error field = %q, want %q: %v", typedField(err), tc.field, err)
			}
		})
	}

	// Once published, changing any bound content invalidates the recorded
	// digest; lifecycle status itself is intentionally not digest content.
	published, err := validAttestation().Publish()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*AttestationStatement)
	}{
		{"text", func(s *AttestationStatement) { s.Text += " changed" }},
		{"fact version", func(s *AttestationStatement) { s.Facts[0].Version = "8" }},
		{"evidence required", func(s *AttestationStatement) { s.Evidence[0].Required = false }},
		{"refusal allowed", func(s *AttestationStatement) { s.RefusalPolicy.Allowed = false }},
	} {
		t.Run("digest "+tc.name, func(t *testing.T) {
			s := published
			tc.mutate(&s)
			if s.VerifyDigest() {
				t.Fatal("ATTEST_001_REJECTED: tampered published statement still verifies")
			}
		})
	}
}

func typedField(err error) string {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Field
	}
	return ""
}
