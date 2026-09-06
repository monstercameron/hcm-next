package employeerelations

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func erCompartment() []Participant {
	return []Participant{
		{Role: RoleReporter, Ref: "person-reporter"},
		{Role: RoleSubject, Ref: "person-subject"},
		{Role: RoleInvestigator, Ref: "person-investigator"},
		{Role: RoleDecisionMaker, Ref: "person-decision-maker"},
	}
}

func TestEmployeeRelationsDomainRequiresAllegationAuthorityRecusalEvidenceAndDisposition(t *testing.T) {
	base := func() AllegationRevision {
		return AllegationRevision{ID: "allegation-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), ReporterRef: "person-reporter", SubjectRef: "person-subject", Summary: "protected allegation summary", Status: AllegationOpen, RetaliationSafeguard: true, Revision: 1}
	}
	allegation, err := NewAllegationRevision(base())
	if err != nil {
		t.Fatal(err)
	}
	investigation, err := NewInvestigationRevision(InvestigationRevision{ID: "investigation-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), AllegationRef: allegation.CanonicalDigest, InvestigatorRef: "person-investigator", AuthorityRef: "authority-er-1", Purpose: "establish facts", Scope: "the reported conduct", Status: InvestigationActive, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	finding, err := NewFindingRevision(FindingRevision{ID: "finding-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), InvestigationRef: investigation.CanonicalDigest, InvestigatorRef: "person-investigator", SubjectRef: "person-subject", ReporterRef: "person-reporter", EvidenceStandard: EvidenceMoreLikelyThanNot, EvidenceRefs: []string{"evidence-1"}, Disposition: FindingSubstantiated, Rationale: "evidence supports the finding", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDisciplineRevision(DisciplineRevision{ID: "discipline-bad", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), SubjectRef: "person-subject", Action: "written warning", Status: DisciplineProposed, Revision: 1}); !errors.Is(err, ErrFindingRequired) {
		t.Fatalf("discipline error = %v, want finding refusal", err)
	}
	discipline, err := NewDisciplineRevision(DisciplineRevision{ID: "discipline-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), FindingRef: finding.CanonicalDigest, SubjectRef: "person-subject", Action: "written warning", LegalReviewRef: "legal-review-1", RepresentationReviewRef: "representation-review-1", Status: DisciplineApproved, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	grievance, err := NewGrievanceRevision(GrievanceRevision{ID: "grievance-1", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), DecisionRef: discipline.CanonicalDigest, GrievantRef: "person-subject", Grounds: "decision is contested", Status: GrievanceOpen, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if grievance.CanonicalDigest == "" || finding.CanonicalDigest == "" {
		t.Fatal("expected canonical digests")
	}

	conflict := FindingRevision{ID: "finding-conflict", CaseRef: "case-1", CompartmentRef: "er-confidential", Participants: erCompartment(), InvestigationRef: investigation.CanonicalDigest, InvestigatorRef: "person-reporter", SubjectRef: "person-subject", ReporterRef: "person-reporter", EvidenceStandard: EvidenceMoreLikelyThanNot, EvidenceRefs: []string{"evidence-1"}, Disposition: FindingSubstantiated, Rationale: "reason", Revision: 1}
	if _, err := NewFindingRevision(conflict); !errors.Is(err, ErrInvestigatorConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	if strings.Contains(finding.Explain(), "person-reporter") || strings.Contains(Explain(finding), "person-reporter") {
		t.Fatalf("reporter identity leaked: %q", finding.Explain())
	}
}

func TestTodo_ER_001_Property(t *testing.T) {
	participants := erCompartment()
	r, err := NewAllegationRevision(AllegationRevision{ID: "a", CaseRef: "c", CompartmentRef: "x", Participants: participants, ReporterRef: "person-reporter", SubjectRef: "person-subject", Summary: "summary", Status: AllegationOpen, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	participants[0].Ref = "changed"
	if r.Participants[0].Ref == "changed" {
		t.Fatal("constructor retained caller slice")
	}
	if _, err := NewAllegationRevision(AllegationRevision{ID: "a", CaseRef: "c", CompartmentRef: "x", Participants: erCompartment(), ReporterRef: "person-reporter", SubjectRef: "person-subject", Summary: "summary", Status: AllegationOpen, Revision: 2, ParentRevision: 1, ParentDigest: r.CanonicalDigest}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_ER_001_Golden(t *testing.T) {
	r, err := NewInterviewRevision(InterviewRevision{ID: "interview", CaseRef: "case", CompartmentRef: "x", Participants: erCompartment(), InvestigationRef: "investigation", InterviewerRef: "person-investigator", IntervieweeRef: "person-subject", Statement: "confidential statement", Status: InterviewSealed, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := r.Digest()
	if err != nil || digest != r.CanonicalDigest {
		t.Fatalf("digest = %q, %v", digest, err)
	}
	if !strings.Contains(r.Explain(), "statement sealed") {
		t.Fatalf("explain = %q", r.Explain())
	}
}

func TestTodo_ER_001_Race(t *testing.T) {
	store := NewMemoryStore()
	r, err := NewAllegationRevision(AllegationRevision{ID: "a", CaseRef: "c", CompartmentRef: "x", Participants: erCompartment(), ReporterRef: "person-reporter", SubjectRef: "person-subject", Summary: "summary", Status: AllegationOpen, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.SaveAllegation(r); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, ok := store.GetAllegation("a", 1); !ok {
		t.Fatal("saved allegation not found")
	}
}

func TestTodo_ER_001_Fault(t *testing.T) {
	bad := AllegationRevision{ID: "a", CaseRef: "c", CompartmentRef: "x", Participants: erCompartment(), ReporterRef: "person-reporter", SubjectRef: "person-subject", Summary: "summary", Status: AllegationOpen, Revision: 2, ParentRevision: 1}
	var field *ValidationError
	if _, err := NewAllegationRevision(bad); !errors.As(err, &field) || field.Field != "parent_digest" {
		t.Fatalf("error = %v, field = %#v", err, field)
	}
}

func TestTodo_ER_001_Security(t *testing.T) {
	r, err := NewFindingRevision(FindingRevision{ID: "f", CaseRef: "case", CompartmentRef: "x", Participants: erCompartment(), InvestigationRef: "i", InvestigatorRef: "person-investigator", SubjectRef: "person-subject", ReporterRef: "secret-reporter", EvidenceStandard: EvidenceMoreLikelyThanNot, EvidenceRefs: []string{"e"}, Disposition: FindingSubstantiated, Rationale: "private rationale", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-reporter", "private rationale"} {
		if strings.Contains(r.Explain(), secret) {
			t.Fatalf("secret %q leaked by Explain: %q", secret, r.Explain())
		}
	}
}

func TestTodo_ER_001_Conformance(t *testing.T) {
	if !RoleReporter.Valid() || RoleParticipantInvalid().Valid() {
		t.Fatal("participant vocabulary is not closed")
	}
}

func TestTodo_ER_001_Mutation(t *testing.T) {
	r, err := NewAllegationRevision(AllegationRevision{ID: "a", CaseRef: "c", CompartmentRef: "x", Participants: erCompartment(), ReporterRef: "person-reporter", SubjectRef: "person-subject", Summary: "summary", Status: AllegationOpen, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	r.CanonicalDigest = "sha256:forged"
	if err := r.Validate(); err == nil {
		t.Fatal("forged digest accepted")
	}
}

func RoleParticipantInvalid() ParticipantRole { return ParticipantRole("NOT_A_ROLE") }
