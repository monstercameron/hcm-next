package qualification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type memoryCredentialFacts struct {
	set   CredentialFactSet
	calls int
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

func TestTodo_QUAL_002(t *testing.T) {
	req := qualificationResolutionRequest(t)
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
	if result.Evaluation.Results[0].Status != StatusSatisfied || result.Evaluation.Results[1].Status != StatusSatisfied {
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
	req := qualificationResolutionRequest(t)
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
	req := qualificationResolutionRequest(t)
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
	req := qualificationResolutionRequest(t)
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

func FuzzTodo_QUAL_002(f *testing.F) {
	f.Add("qualification.read")
	f.Fuzz(func(t *testing.T, scope string) {
		req := qualificationResolutionRequest(t)
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
