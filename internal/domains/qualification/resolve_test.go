package qualification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/skill"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type memoryCredentialFacts struct {
	set   CredentialFactSet
	calls int
}

type qualificationSkillAuthority struct{}

func (qualificationSkillAuthority) VerifyPinnedSkillAuthority(_ context.Context, claim PinnedSkillAuthorityClaim) error {
	if claim.AuthorityRef != "skill-registry/v1" || claim.Tenant != "tenant-a" || claim.Worker != qualificationWorker() || claim.Purpose != "staffing-preflight" || claim.OntologyDigest == "" || claim.EvidenceDigest == "" || claim.EquivalenceDigest == "" {
		return errors.New("untrusted pinned skill claim")
	}
	return nil
}

func (m *memoryCredentialFacts) CredentialFactsAt(_ context.Context, q CredentialFactsQuery) (CredentialFactSet, error) {
	m.calls++
	if q.Worker != m.set.Worker {
		return CredentialFactSet{}, errors.New("wrong worker")
	}
	return m.set, nil
}

func qualificationWorker() values.EntityRef {
	return values.EntityRef{Tenant: "tenant-a", Kind: people.KindWorker, Id: "00000000-0000-0000-0000-000000000001"}
}

func qualificationAsOf() values.Instant {
	return values.NewInstant(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
}

func qualificationFact(t *testing.T, worker values.EntityRef, ref, evidenceRef string) CredentialFact {
	t.Helper()
	verifiedAt := values.NewInstant(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	freshUntil := values.NewInstant(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	known, err := values.NewKnownAt(verifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(verifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("credential-stream", 1)
	if err != nil {
		t.Fatal(err)
	}
	return CredentialFact{
		Worker: worker, CredentialRef: ref, Issuer: "issuer:nursing-board", Level: 2,
		EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: evidenceRef,
		Validity: qualificationInterval(t, 1, 31), Verified: true, Trusted: true,
		VerifiedAt: verifiedAt, FreshUntil: freshUntil, KnownAt: known, Revision: revision,
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityExternalObservation, System: "nursing-board", PolicyRef: "credential-policy/v1"},
		Provenance: evidence.Provenance{Source: "nursing-board", EvidenceRef: evidenceRef, RecordedAt: recorded},
	}
}

func qualificationResolutionRequest(t *testing.T) QualificationResolutionRequest {
	t.Helper()
	return QualificationResolutionRequest{
		Tenant: "tenant-a", Worker: qualificationWorker(), AsOf: qualificationAsOf(), Requirement: validQualification(t),
		Authorization: QualificationAuthorization{PolicyVersion: "authz/2026.1", Purpose: "staffing-preflight", SubjectDisclosable: true, Scopes: []string{QualificationReadScope}},
	}
}

func qualificationCredentialOnlyRequest(t *testing.T) QualificationResolutionRequest {
	t.Helper()
	req := qualificationResolutionRequest(t)
	req.Requirement.Skills = nil
	req.Requirement.CanonicalDigest = ""
	var err error
	req.Requirement, err = NewQualificationRequirement(req.Requirement)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestTodo_QUAL_002(t *testing.T) {
	req := qualificationCredentialOnlyRequest(t)
	worker := req.Worker
	reader := &memoryCredentialFacts{set: CredentialFactSet{Worker: worker, Exists: true, Watermark: mustCredentialRevision(t, 9), Facts: []CredentialFact{
		qualificationFact(t, worker, "credential:nursing-license", "evidence:license-1"),
		func() CredentialFact {
			fact := qualificationFact(t, worker, "credential:triage-assessment", "evidence:assessment-1")
			fact.EvidenceKind = EvidenceAssessment
			fact.CredentialRef = ""
			fact.SkillRef = "skill:triage"
			return fact
		}(),
	}}}
	result, err := ResolveWorkerQualification(context.Background(), reader, req)
	if err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 || result.Disclosure != people.DisclosureFull || len(result.Credentials) != 2 {
		t.Fatalf("resolution disclosure=%s credentials=%d calls=%d", result.Disclosure, len(result.Credentials), reader.calls)
	}
	if result.Evaluation.Results[0].Status != StatusSatisfied {
		t.Fatalf("evaluation = %+v", result.Evaluation.Results)
	}
	if result.ResultDigest == "" || result.Evaluation.CanonicalDigest == "" {
		t.Fatal("resolution and evaluation must be digested")
	}
	explanation, err := result.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explanation.CredentialCount != 2 || strings.Contains(explanation.StringForTest(), "evidence:license-1") {
		t.Fatalf("explanation leaked evidence content: %+v", explanation)
	}
}

func (e QualificationResolutionExplanation) StringForTest() string {
	return e.RequirementID + e.WithheldReason + e.EvaluationDigest + e.ResultDigest
}

func TestTodo_QUAL_002_Property(t *testing.T) {
	req := qualificationCredentialOnlyRequest(t)
	worker := req.Worker
	a := qualificationFact(t, worker, "credential:nursing-license", "evidence:license-1")
	b := qualificationFact(t, worker, "credential:triage-assessment", "evidence:assessment-1")
	b.EvidenceKind = EvidenceAssessment
	b.CredentialRef = ""
	b.SkillRef = "skill:triage"
	forward := &memoryCredentialFacts{set: CredentialFactSet{Worker: worker, Exists: true, Watermark: mustCredentialRevision(t, 9), Facts: []CredentialFact{a, b}}}
	reverse := &memoryCredentialFacts{set: CredentialFactSet{Worker: worker, Exists: true, Watermark: mustCredentialRevision(t, 9), Facts: []CredentialFact{b, a}}}
	first, err := Resolve(context.Background(), forward, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(context.Background(), reverse, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.ResultDigest != second.ResultDigest || string(first.Canonical()) != string(second.Canonical()) {
		t.Fatal("credential input order changed the resolution digest")
	}
	if got := first.Credentials; len(got) != 2 || got[0].EvidenceRef != "evidence:assessment-1" {
		t.Fatalf("credentials were not canonically ordered: %+v", got)
	}
}

func TestTodo_QUAL_002_Mutation(t *testing.T) {
	req := qualificationCredentialOnlyRequest(t)
	worker := req.Worker
	reader := &memoryCredentialFacts{set: CredentialFactSet{Worker: worker, Exists: true, Watermark: mustCredentialRevision(t, 1), Facts: []CredentialFact{qualificationFact(t, worker, "credential:nursing-license", "evidence:license-1")}}}
	result, err := Resolve(context.Background(), reader, req)
	if err != nil {
		t.Fatal(err)
	}
	result.ResultDigest = "sha256:tampered"
	if _, err := result.Explain(); err == nil {
		t.Fatal("tampered result remained explainable")
	}
}

func TestTodo_QUAL_002_Security(t *testing.T) {
	req := qualificationCredentialOnlyRequest(t)
	worker := req.Worker
	untrusted := qualificationFact(t, worker, "credential:nursing-license", "evidence:untrusted-secret")
	untrusted.Trusted = false
	expired := qualificationFact(t, worker, "credential:nursing-license", "evidence:expired-secret")
	expired.FreshUntil = values.NewInstant(time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))
	reader := &memoryCredentialFacts{set: CredentialFactSet{Worker: worker, Exists: true, Watermark: mustCredentialRevision(t, 2), Facts: []CredentialFact{untrusted, expired}}}
	result, err := Resolve(context.Background(), reader, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Credentials) != 0 || result.Evaluation.Results[0].Status != StatusUnsatisfied {
		t.Fatalf("restricted credentials counted: %+v", result)
	}
	withoutScope := req
	withoutScope.Authorization.Scopes = nil
	withheldReader := &memoryCredentialFacts{set: reader.set}
	withheld, err := Resolve(context.Background(), withheldReader, withoutScope)
	if err != nil {
		t.Fatal(err)
	}
	if withheld.Disclosure != people.DisclosureWithheld || withheld.WithheldReason != "missing_scope:qualification.read" || withheldReader.calls != 0 || len(withheld.Credentials) != 0 {
		t.Fatalf("missing scope was not withheld: %+v calls=%d", withheld, withheldReader.calls)
	}
}

func TestQualificationUsesPinnedSkillDecisionAndHistoricalBasis(t *testing.T) {
	req := qualificationResolutionRequest(t)
	worker := req.Worker
	ref := values.EntityRef{Tenant: worker.Tenant, Kind: skill.KindSkill, Id: "00000000-0000-0000-0000-000000000002"}
	definition, err := skill.NewSkillDefinition(skill.SkillDefinitionRevision{SkillRef: ref, Revision: mustCredentialRevision(t, 1), Name: "triage", ProficiencyScale: skill.DefaultProficiencyScale()})
	if err != nil {
		t.Fatal(err)
	}
	ontology, err := skill.NewSkillOntology(skill.SkillOntologyRevision{OntologyID: values.EntityRef{Tenant: worker.Tenant, Kind: values.Kind("skill_ontology"), Id: "00000000-0000-0000-0000-000000000010"}, Revision: mustCredentialRevision(t, 1), Skills: []skill.SkillDefinitionRevision{definition}})
	if err != nil {
		t.Fatal(err)
	}
	evidenceID := values.EntityRef{Tenant: worker.Tenant, Kind: values.Kind("skill_evidence"), Id: "00000000-0000-0000-0000-000000000020"}
	start, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2026-01-31")
	if err != nil {
		t.Fatal(err)
	}
	activeInterval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	original, err := skill.NewWorkerSkillEvidence(skill.WorkerSkillEvidence{EvidenceID: evidenceID, Worker: worker, SkillRef: ref, Level: 1, EvidenceKind: skill.EvidenceAssessment, EvidenceRef: "private-original", Verified: true, Effective: activeInterval})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := skill.NewEvidenceRevision(1, []skill.WorkerSkillEvidence{original})
	if err != nil {
		t.Fatal(err)
	}
	reader := &memoryCredentialFacts{set: CredentialFactSet{Worker: worker, Exists: true, Watermark: mustCredentialRevision(t, 1), Facts: []CredentialFact{qualificationFact(t, worker, "credential:nursing-license", "license")}}}
	got, err := ResolveWorkerQualificationWithPinnedSkills(context.Background(), reader, req, PinnedSkillInput{Ontology: ontology, Evidence: snapshot, AuthorityRef: "skill-registry/v1", AuthorityVerifier: qualificationSkillAuthority{}, Purpose: req.Authorization.Purpose, EquivalenceDigest: equivalenceDigest(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Evaluation.Results[1].Status != StatusSatisfied || got.SkillEvidenceDigest != snapshot.Digest || got.SkillOntologyDigest != ontology.CanonicalDigest {
		t.Fatalf("pinned skill was not part of qualification decision: %+v", got)
	}
	if got.SkillAuthority != "skill-registry/v1" || got.SkillPurpose != req.Authorization.Purpose || got.SkillEquivalenceDigest != equivalenceDigest(nil) {
		t.Fatalf("resolution did not retain its complete pinned authority: %+v", got)
	}
	historical := got
	if strings.Contains(string(got.Canonical()), original.EvidenceRef) {
		t.Fatal("purpose-safe qualification result leaked hidden skill evidence")
	}
	if _, err := ResolveWorkerQualification(context.Background(), reader, req); !errors.Is(err, ErrPinnedSkillInput) {
		t.Fatalf("skill requirement fell back to an unpinned credential fact: %v", err)
	}
	baseInput := PinnedSkillInput{Ontology: ontology, Evidence: snapshot, AuthorityRef: "skill-registry/v1", AuthorityVerifier: qualificationSkillAuthority{}, Purpose: req.Authorization.Purpose, EquivalenceDigest: equivalenceDigest(nil)}
	for name, mutate := range map[string]func(*PinnedSkillInput){
		"forged authority":        func(input *PinnedSkillInput) { input.AuthorityRef = "caller-asserted/v1" },
		"missing verifier":        func(input *PinnedSkillInput) { input.AuthorityVerifier = nil },
		"wrong purpose":           func(input *PinnedSkillInput) { input.Purpose = "candidate-ranking" },
		"missing equivalence pin": func(input *PinnedSkillInput) { input.EquivalenceDigest = "" },
		"forged ontology digest":  func(input *PinnedSkillInput) { input.Ontology.CanonicalDigest = "sha256:forged" },
		"forged evidence digest":  func(input *PinnedSkillInput) { input.Evidence.Digest = "sha256:forged" },
	} {
		t.Run(name, func(t *testing.T) {
			input := baseInput
			mutate(&input)
			if _, err := ResolveAuthorizedQualificationWithPinnedSkills(context.Background(), reader, req, input); !errors.Is(err, ErrPinnedSkillInput) {
				t.Fatalf("forged or unpinned input was accepted: %v", err)
			}
		})
	}
	otherWorker := worker
	otherWorker.Id = "00000000-0000-0000-0000-000000000099"
	foreign := original
	foreign.Worker = otherWorker
	foreign.EvidenceID.Id = "00000000-0000-0000-0000-000000000098"
	foreign, err = skill.NewWorkerSkillEvidence(foreign)
	if err != nil {
		t.Fatal(err)
	}
	foreignSnapshot, err := skill.NewEvidenceRevision(2, []skill.WorkerSkillEvidence{foreign})
	if err != nil {
		t.Fatal(err)
	}
	foreignInput := baseInput
	foreignInput.Evidence = foreignSnapshot
	if _, err := ResolveAuthorizedQualificationWithPinnedSkills(context.Background(), reader, req, foreignInput); !errors.Is(err, ErrPinnedSkillInput) {
		t.Fatalf("cross-worker evidence was accepted: %v", err)
	}
	hiddenReq := req
	hiddenReq.Authorization.Scopes = nil
	hiddenReader := &memoryCredentialFacts{set: reader.set}
	hidden, err := ResolveAuthorizedQualificationWithPinnedSkills(context.Background(), hiddenReader, hiddenReq, PinnedSkillInput{})
	if err != nil || hidden.Disclosure != people.DisclosureWithheld || hiddenReader.calls != 0 || strings.Contains(string(hidden.Canonical()), original.EvidenceRef) {
		t.Fatalf("withheld request touched or disclosed hidden evidence: result=%+v calls=%d err=%v", hidden, hiddenReader.calls, err)
	}
	// An expired pinned revision is fail-closed even when the credential port
	// contains a similarly shaped, trusted-looking skill fact.
	expired := original
	expiredEnd, err := values.ParseLocalDate("2026-01-10")
	if err != nil {
		t.Fatal(err)
	}
	expired.Effective, err = values.NewLocalDateInterval(start, expiredEnd, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	expired, err = skill.NewWorkerSkillEvidence(expired)
	if err != nil {
		t.Fatal(err)
	}
	expiredSnapshot, err := skill.NewEvidenceRevision(2, []skill.WorkerSkillEvidence{expired})
	if err != nil {
		t.Fatal(err)
	}
	got, err = ResolveWorkerQualificationWithPinnedSkills(context.Background(), reader, req, PinnedSkillInput{Ontology: ontology, Evidence: expiredSnapshot, AuthorityRef: "skill-registry/v1", AuthorityVerifier: qualificationSkillAuthority{}, Purpose: req.Authorization.Purpose, EquivalenceDigest: equivalenceDigest(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Evaluation.Results[1].Status != StatusUnsatisfied {
		t.Fatalf("expired pinned skill counted: %+v", got.Evaluation.Results)
	}
	// A correction remains append-only. The successor changes a current
	// decision, while the previously returned decision retains its exact basis.
	successor := original
	successor.EvidenceID.Id = "00000000-0000-0000-0000-000000000021"
	successor.Supersedes = original.EvidenceID
	successor.Disputed = true
	successor, err = skill.NewWorkerSkillEvidence(successor)
	if err != nil {
		t.Fatal(err)
	}
	correctedSnapshot, err := skill.NewEvidenceRevision(3, []skill.WorkerSkillEvidence{original, successor})
	if err != nil {
		t.Fatal(err)
	}
	correctedInput := baseInput
	correctedInput.Evidence = correctedSnapshot
	corrected, err := ResolveAuthorizedQualificationWithPinnedSkills(context.Background(), reader, req, correctedInput)
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Evaluation.Results[1].Status != StatusUnsatisfied || corrected.SkillEvidenceDigest == snapshot.Digest {
		t.Fatalf("correction did not trigger a newly pinned decision: %+v", corrected)
	}
	if historical.SkillEvidenceDigest != snapshot.Digest || historical.Evaluation.Results[1].Status != StatusSatisfied {
		t.Fatal("retained historical resolution basis was mutated")
	}
}

func FuzzTodo_QUAL_002(f *testing.F) {
	f.Add("qualification.read")
	f.Fuzz(func(t *testing.T, scope string) {
		req := qualificationCredentialOnlyRequest(t)
		req.Authorization.Scopes = []string{scope}
		if scope == "" {
			return
		}
		if err := req.Authorization.Validate(); err != nil {
			return
		}
		worker := req.Worker
		reader := &memoryCredentialFacts{set: CredentialFactSet{Worker: worker, Exists: true, Watermark: mustCredentialRevision(t, 3), Facts: nil}}
		if _, err := Resolve(context.Background(), reader, req); err != nil {
			t.Fatalf("valid authorization scope caused resolution error: %v", err)
		}
	})
}

func mustCredentialRevision(t *testing.T, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision("credential-watermark", sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}
