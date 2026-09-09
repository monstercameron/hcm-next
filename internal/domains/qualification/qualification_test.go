package qualification

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func qualificationInterval(t *testing.T, startDay, endDay int) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(2026, time.January, startDay)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2026, time.January, endDay)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func validQualification(t *testing.T) QualificationRequirement {
	t.Helper()
	r, err := NewQualificationRequirement(QualificationRequirement{
		RequirementID: "qual-1", Revision: 1,
		SubjectScope:      SubjectScope{Kind: "role", Ref: "registered-nurse"},
		AssignmentContext: "clinical-assignment", AuthorizationRef: "policy:talent.read",
		Experience: "acute-care", EquivalencyRef: "equiv:none", PolicyRef: "talent-policy-v1",
		Availability: qualificationInterval(t, 1, 31), Validity: qualificationInterval(t, 1, 31),
		Credentials:      []CredentialRequirement{{Ref: "credential:nursing-license", Level: 2}},
		Skills:           []SkillRequirement{{Ref: "skill:triage", Level: 1}},
		AcceptedEvidence: []EvidenceKind{EvidenceVerifiedCredential, EvidenceAssessment},
		Renewal:          RenewalRule{Kind: RenewalBeforeExpiry},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func held(t *testing.T, end int, kind EvidenceKind) HeldCredential {
	t.Helper()
	return HeldCredential{
		CredentialRef: "credential:nursing-license", Level: 2, EvidenceKind: kind,
		EvidenceRef: "evidence:license-1", Validity: qualificationInterval(t, 1, end),
	}
}

func TestTodo_QUAL_001(t *testing.T) {
	requirement := validQualification(t)
	result, err := requirement.Evaluate([]HeldCredential{
		held(t, 31, EvidenceVerifiedCredential),
		{SkillRef: "skill:triage", Level: 1, EvidenceKind: EvidenceAssessment, EvidenceRef: "evidence:assessment-1", Validity: qualificationInterval(t, 1, 31)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || result.Results[0].Status != StatusSatisfied || result.Results[1].Status != StatusSatisfied {
		t.Fatalf("qualification result = %+v", result.Results)
	}
	if result.CanonicalDigest == "" {
		t.Fatal("evaluation did not receive a digest")
	}
	if got, err := requirement.Digest(); err != nil || got != requirement.CanonicalDigest {
		t.Fatalf("digest = %q, %v", got, err)
	}
}

func TestTodo_QUAL_001_Property(t *testing.T) {
	r := validQualification(t)
	copy := r
	copy.Credentials = append([]CredentialRequirement(nil), r.Credentials...)
	copy.Credentials[0].Ref = "credential:other"
	if r.Credentials[0].Ref == copy.Credentials[0].Ref {
		t.Fatal("requirement construction aliased credentials")
	}
	other := validQualification(t)
	if r.CanonicalDigest != other.CanonicalDigest {
		t.Fatal("equal requirements have different canonical digests")
	}
	if got := r.Canonical(); got == nil || len(got) == 0 {
		t.Fatal("valid requirement has no canonical bytes")
	}
}

func TestTodo_QUAL_001_Security(t *testing.T) {
	r := validQualification(t)
	bad := held(t, 31, EvidenceTrainingRecord)
	result, err := r.Evaluate([]HeldCredential{bad})
	if err != nil {
		t.Fatal(err)
	}
	if result.Results[0].Status != StatusUnsatisfied || result.Results[0].Gap == "" {
		t.Fatalf("disallowed evidence result = %+v", result.Results[0])
	}
}

func TestTodo_QUAL_001_Conformance(t *testing.T) {
	r := validQualification(t)
	for name, mutate := range map[string]func(*QualificationRequirement){
		"missing_scope":    func(r *QualificationRequirement) { r.SubjectScope = SubjectScope{} },
		"missing_validity": func(r *QualificationRequirement) { r.Validity = values.EffectiveInterval{} },
		"unknown_evidence": func(r *QualificationRequirement) { r.AcceptedEvidence = []EvidenceKind{"UNKNOWN"} },
		"missing_policy":   func(r *QualificationRequirement) { r.PolicyRef = "" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := r
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid requirement accepted")
			}
		})
	}
	var missing HeldCredential
	if _, err := r.Evaluate([]HeldCredential{missing}); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("invalid held credential error = %v", err)
	}
}

func TestTodo_QUAL_001_Mutation(t *testing.T) {
	r := validQualification(t)
	candidate := r
	candidate.CanonicalDigest = "sha256:tampered"
	if err := candidate.Validate(); err == nil {
		t.Fatal("tampered digest accepted")
	}
	explanation, err := Explain(r)
	if err != nil || explanation.Digest == "" {
		t.Fatalf("Explain = %+v, %v", explanation, err)
	}
}
