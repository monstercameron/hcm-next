package career

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestCareerMobilityConformanceRequiresConsentEligibilityAndExplainableCandidateSet(t *testing.T) {
	worker := careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000201")
	preference, err := NewCareerPreference(CareerPreferenceProfileRevision{
		PreferenceID: careerRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000202"), Revision: careerRevision(t, "preference", 4), Worker: worker,
		Mobility: MobilityAny, TargetRoleRefs: []values.EntityRef{careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000203")}, Timeframe: careerInterval(t), Visibility: VisibilityWorkerAndAuthorized,
	})
	if err != nil {
		t.Fatal(err)
	}
	consent := MobilityConsent{ConsentRef: careerRef(values.Kind("consent"), "00000000-0000-0000-0000-000000000204"), Worker: worker, Purpose: "internal mobility recommendations", Effective: careerInterval(t)}
	good := MobilityOpportunity{OpportunityID: careerRef(values.Kind("mobility_opportunity"), "00000000-0000-0000-0000-000000000205"), Role: careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000203"), RoleRevision: careerRevision(t, "job", 9)}
	denied := good
	denied.OpportunityID = careerRef(values.Kind("mobility_opportunity"), "00000000-0000-0000-0000-000000000206")
	verifier := mobilityVerifierFor(t, good)
	a, err := EvaluateInternalMobility(context.Background(), verifier, worker, preference, consent, careerDate(t, "2026-06-01"), []MobilityOpportunity{good, denied}, careerRef(values.Kind("internal_mobility_assessment"), "00000000-0000-0000-0000-000000000207"), careerRevision(t, "mobility", 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Candidates) != 1 || a.Candidates[0].OpportunityID != good.OpportunityID || strings.TrimSpace(a.Candidates[0].MatchExplanation) == "" {
		t.Fatalf("candidate set = %+v", a.Candidates)
	}
	verifier.assessmentDigest = a.CanonicalDigest
	intent, err := CreateMobilityApplicationIntent(context.Background(), verifier, careerDate(t, "2026-06-01"), careerRef(values.Kind("application_intent"), "00000000-0000-0000-0000-000000000208"), a, good.OpportunityID)
	if err != nil || intent.AssessmentRevision != a.Revision {
		t.Fatalf("intent=%+v err=%v", intent, err)
	}
	if _, err := CreateMobilityApplicationIntent(context.Background(), verifier, careerDate(t, "2026-06-01"), careerRef(values.Kind("application_intent"), "00000000-0000-0000-0000-000000000209"), a, denied.OpportunityID); err == nil {
		t.Fatal("denied opportunity became an application intent")
	}
	withdrawn, err := consent.Withdraw()
	if err != nil {
		t.Fatal(err)
	}
	if active, _ := withdrawn.ActiveAt(careerDate(t, "2026-06-01")); active {
		t.Fatal("withdrawn consent remained active")
	}
	if _, err := EvaluateInternalMobility(context.Background(), verifier, worker, preference, withdrawn, careerDate(t, "2026-06-01"), []MobilityOpportunity{good}, a.AssessmentID, careerRevision(t, "mobility", 2)); err == nil {
		t.Fatal("withdrawn consent permitted evaluation")
	}
	corrected := a
	corrected.Candidates = append([]MobilityCandidate(nil), a.Candidates...)
	corrected.Revision = careerRevision(t, "mobility", 2)
	corrected.Candidates[0].MatchExplanation = "corrected explanation"
	corrected, err = CorrectInternalMobilityAssessment(a, corrected)
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Supersedes != a.Revision || !strings.Contains(corrected.Provenance, a.CanonicalDigest) {
		t.Fatalf("correction provenance = %+v", corrected)
	}
	if a.Candidates[0].MatchExplanation == corrected.Candidates[0].MatchExplanation {
		t.Fatal("correction rewrote prior assessment")
	}
}

func TestTodo_CAREER_002_Conformance(t *testing.T) {
	a, good, _ := mobilityAssessmentFixture(t)
	verifier := mobilityVerifierFor(t, good)
	verifier.assessmentDigest = a.CanonicalDigest
	intent, err := CreateMobilityApplicationIntent(context.Background(), verifier, careerDate(t, "2026-06-01"), careerRef(values.Kind("application_intent"), "00000000-0000-0000-0000-000000000219"), a, good.OpportunityID)
	if err != nil || intent.AssessmentID != a.AssessmentID || intent.AssessmentRevision != a.Revision {
		t.Fatalf("separate application intent = %+v, %v", intent, err)
	}
	bound := verifier.lastApplication
	if bound.AssessmentID != a.AssessmentID || bound.AssessmentDigest != a.CanonicalDigest || bound.Worker != a.Worker || bound.PreferenceDigest != a.PreferenceDigest || bound.OpportunityID != good.OpportunityID || bound.PopulationAuthority != a.Candidates[0].PopulationAuthority || !bound.EligibilityPolicyRevision.Equal(a.Candidates[0].EligibilityPolicyRevision) {
		t.Fatalf("application verification was not bound to exact assessment basis: %+v", bound)
	}
	if a.AssessmentID.Kind == values.Kind("application_intent") {
		t.Fatal("recommendation acquired application authority")
	}
	forged := a
	forged.Candidates = append([]MobilityCandidate(nil), a.Candidates...)
	forged.Candidates[0].OpportunityID = careerRef(values.Kind("mobility_opportunity"), "00000000-0000-0000-0000-000000000298")
	forged.CanonicalDigest = ""
	forged, err = NewInternalMobilityAssessment(forged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateMobilityApplicationIntent(context.Background(), verifier, careerDate(t, "2026-06-01"), careerRef(values.Kind("application_intent"), "00000000-0000-0000-0000-000000000222"), forged, forged.Candidates[0].OpportunityID); !errors.Is(err, ErrMobilityNotAuthorized) {
		t.Fatalf("forged public assessment error = %v", err)
	}
	verifier.applicationErr = errors.New("consent withdrawn after assessment")
	if _, err := CreateMobilityApplicationIntent(context.Background(), verifier, careerDate(t, "2026-06-02"), careerRef(values.Kind("application_intent"), "00000000-0000-0000-0000-000000000221"), a, good.OpportunityID); !errors.Is(err, ErrMobilityNotAuthorized) {
		t.Fatalf("post-assessment withdrawal error = %v", err)
	}
	verifier.applicationErr = errors.New("role revision withdrawn")
	if _, err := CreateMobilityApplicationIntent(context.Background(), verifier, careerDate(t, "2026-06-03"), careerRef(values.Kind("application_intent"), "00000000-0000-0000-0000-000000000223"), a, good.OpportunityID); !errors.Is(err, ErrMobilityNotAuthorized) {
		t.Fatalf("withdrawn role revision error = %v", err)
	}
}
func TestTodo_CAREER_002_Fault(t *testing.T) {
	a, good, consent := mobilityAssessmentFixture(t)
	verifier := mobilityVerifierFor(t, good)
	tampered := a
	tampered.Provenance = "forged"
	if !errors.Is(tampered.Validate(), ErrInvalidRevision) {
		t.Fatal("digest did not detect a forged recommendation")
	}
	withdrawn, err := consent.Withdraw()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateInternalMobility(context.Background(), verifier, a.Worker, mobilityPreferenceFixture(t, a.Worker), withdrawn, careerDate(t, "2026-06-01"), nil, a.AssessmentID, careerRevision(t, "mobility", 2)); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("withdrawn consent error = %v", err)
	}
	wrongPurpose := consent
	wrongPurpose.Purpose = "recruiting"
	if _, err := EvaluateInternalMobility(context.Background(), verifier, a.Worker, mobilityPreferenceFixture(t, a.Worker), wrongPurpose, careerDate(t, "2026-06-01"), nil, a.AssessmentID, careerRevision(t, "mobility", 2)); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("wrong-purpose consent error = %v", err)
	}
	ineligible := a
	ineligible.CanonicalDigest = ""
	ineligible.Candidates[0].Eligible = false
	if _, err := NewInternalMobilityAssessment(ineligible); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("ineligible disclosed candidate error = %v", err)
	}
	forgedVerifier := mobilityVerifierFor(t, good)
	forged := forgedVerifier.evidence[good.OpportunityID.String()]
	forged.PopulationAuthority.Tenant = "other-tenant"
	forgedVerifier.evidence[good.OpportunityID.String()] = forged
	if _, err := EvaluateInternalMobility(context.Background(), forgedVerifier, a.Worker, mobilityPreferenceFixture(t, a.Worker), consent, careerDate(t, "2026-06-01"), []MobilityOpportunity{good}, a.AssessmentID, careerRevision(t, "mobility", 2)); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("forged population authority error = %v", err)
	}
	staleVerifier := mobilityVerifierFor(t, good)
	staleVerifier.basisErr = errors.New("preference revision is stale")
	if _, err := EvaluateInternalMobility(context.Background(), staleVerifier, a.Worker, mobilityPreferenceFixture(t, a.Worker), consent, careerDate(t, "2026-06-01"), []MobilityOpportunity{good}, a.AssessmentID, careerRevision(t, "mobility", 2)); !errors.Is(err, ErrMobilityNotAuthorized) {
		t.Fatalf("stale preference error = %v", err)
	}
	verifier.assessmentDigest = a.CanonicalDigest
	if _, err := CreateMobilityApplicationIntent(context.Background(), verifier, values.LocalDate{}, careerRef(values.Kind("application_intent"), "00000000-0000-0000-0000-000000000224"), a, good.OpportunityID); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("invalid application as-of error = %v", err)
	}
}
func TestTodo_CAREER_002_Golden(t *testing.T) {
	a, _, _ := mobilityAssessmentFixture(t)
	const want = "sha256:2c8f20c06fd50ce3a9ae7358ccd91cbbedcba58bdf329231d575827e72f1f495"
	if a.CanonicalDigest != want {
		t.Fatalf("canonical bytes digest = %q, want %q", a.CanonicalDigest, want)
	}
}
func TestTodo_CAREER_002_Mutation(t *testing.T) {
	a, _, _ := mobilityAssessmentFixture(t)
	priorDigest, priorExplanation := a.CanonicalDigest, a.Candidates[0].MatchExplanation
	corrected := a
	corrected.Candidates = append([]MobilityCandidate(nil), a.Candidates...)
	corrected.Revision = careerRevision(t, "mobility", 2)
	corrected.Candidates[0].MatchExplanation = "corrected evidence"
	got, err := CorrectInternalMobilityAssessment(a, corrected)
	if err != nil {
		t.Fatal(err)
	}
	if a.CanonicalDigest != priorDigest || a.Candidates[0].MatchExplanation != priorExplanation {
		t.Fatal("correction mutated prior recommendation")
	}
	replay := corrected
	replay.Revision = a.Revision
	if _, err := CorrectInternalMobilityAssessment(a, replay); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("replayed revision error = %v", err)
	}
	if got.Supersedes != a.Revision || !strings.Contains(got.Provenance, priorDigest) {
		t.Fatalf("correction lineage = %+v", got)
	}
}
func TestTodo_CAREER_002_Property(t *testing.T) {
	a, _, _ := mobilityAssessmentFixture(t)
	for _, mutate := range []func(*InternalMobilityAssessment){
		func(x *InternalMobilityAssessment) { x.Provenance += "x" },
		func(x *InternalMobilityAssessment) { x.Candidates[0].Eligible = false },
		func(x *InternalMobilityAssessment) { x.Candidates[0].EligibilityReason += "x" },
		func(x *InternalMobilityAssessment) { x.Candidates[0].MatchExplanation += "x" },
	} {
		changed := a
		changed.Candidates = append([]MobilityCandidate(nil), a.Candidates...)
		mutate(&changed)
		if !errors.Is(changed.Validate(), ErrInvalidRevision) {
			t.Fatal("semantic mutation retained the canonical digest")
		}
	}
}
func TestTodo_CAREER_002_Race(t *testing.T) {
	a, good, consent := mobilityAssessmentFixture(t)
	verifier := mobilityVerifierFor(t, good)
	p := mobilityPreferenceFixture(t, a.Worker)
	const goroutines = 16
	digests := make(chan string, goroutines)
	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := EvaluateInternalMobility(context.Background(), verifier, a.Worker, p, consent, careerDate(t, "2026-06-01"), []MobilityOpportunity{good}, a.AssessmentID, a.Revision)
			if err != nil {
				errCh <- err
				return
			}
			digests <- got.CanonicalDigest
		}()
	}
	wg.Wait()
	close(errCh)
	close(digests)
	for err := range errCh {
		t.Fatal(err)
	}
	for digest := range digests {
		if digest != a.CanonicalDigest {
			t.Fatalf("concurrent digest = %q", digest)
		}
	}
}
func TestTodo_CAREER_002_Security(t *testing.T) {
	a, good, consent := mobilityAssessmentFixture(t)
	verifier := mobilityVerifierFor(t, good)
	otherWorker := careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000299")
	consent.Worker = otherWorker
	if _, err := EvaluateInternalMobility(context.Background(), verifier, a.Worker, mobilityPreferenceFixture(t, a.Worker), consent, careerDate(t, "2026-06-01"), []MobilityOpportunity{good}, a.AssessmentID, a.Revision); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("another worker's consent error = %v", err)
	}
	foreign := good.OpportunityID
	foreign.Tenant = values.TenantId("other-tenant")
	if _, err := CreateMobilityApplicationIntent(context.Background(), verifier, careerDate(t, "2026-06-01"), careerRef(values.Kind("application_intent"), "00000000-0000-0000-0000-000000000220"), a, foreign); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("foreign-tenant opportunity error = %v", err)
	}
}

func mobilityPreferenceFixture(t *testing.T, worker values.EntityRef) CareerPreferenceProfileRevision {
	t.Helper()
	p, err := NewCareerPreference(CareerPreferenceProfileRevision{
		PreferenceID: careerRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000212"), Revision: careerRevision(t, "preference", 4), Worker: worker,
		Mobility: MobilityAny, TargetRoleRefs: []values.EntityRef{careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000213")}, Timeframe: careerInterval(t), Visibility: VisibilityWorkerAndAuthorized,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mobilityAssessmentFixture(t *testing.T) (InternalMobilityAssessment, MobilityOpportunity, MobilityConsent) {
	t.Helper()
	worker := careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000211")
	p := mobilityPreferenceFixture(t, worker)
	consent := MobilityConsent{ConsentRef: careerRef(values.Kind("consent"), "00000000-0000-0000-0000-000000000214"), Worker: worker, Purpose: "internal mobility recommendations", Effective: careerInterval(t)}
	good := MobilityOpportunity{OpportunityID: careerRef(values.Kind("mobility_opportunity"), "00000000-0000-0000-0000-000000000215"), Role: careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000213"), RoleRevision: careerRevision(t, "job", 9)}
	a, err := EvaluateInternalMobility(context.Background(), mobilityVerifierFor(t, good), worker, p, consent, careerDate(t, "2026-06-01"), []MobilityOpportunity{good}, careerRef(values.Kind("internal_mobility_assessment"), "00000000-0000-0000-0000-000000000217"), careerRevision(t, "mobility", 1))
	if err != nil {
		t.Fatal(err)
	}
	return a, good, consent
}

type testMobilityVerifier struct {
	basisErr, applicationErr error
	assessmentDigest         string
	opportunityID            values.EntityRef
	role                     values.EntityRef
	roleRevision             values.RevisionToken
	lastApplication          MobilityApplicationRequest
	evidence                 map[string]MobilityEligibilityEvidence
}

func (v *testMobilityVerifier) VerifyBasis(context.Context, MobilityBasisRequest) error {
	return v.basisErr
}
func (v *testMobilityVerifier) VerifyApplication(_ context.Context, r MobilityApplicationRequest) error {
	v.lastApplication = r
	if v.applicationErr != nil {
		return v.applicationErr
	}
	if v.assessmentDigest == "" || r.AssessmentDigest != v.assessmentDigest || r.OpportunityID != v.opportunityID || r.Role != v.role || !r.RoleRevision.Equal(v.roleRevision) {
		return errors.New("assessment or opportunity is not authenticated")
	}
	return nil
}
func (v *testMobilityVerifier) VerifyOpportunity(_ context.Context, r MobilityOpportunityRequest) (MobilityEligibilityEvidence, error) {
	return v.evidence[r.OpportunityID.String()], nil
}

func mobilityVerifierFor(t *testing.T, opportunity MobilityOpportunity) *testMobilityVerifier {
	t.Helper()
	return &testMobilityVerifier{opportunityID: opportunity.OpportunityID, role: opportunity.Role, roleRevision: opportunity.RoleRevision, evidence: map[string]MobilityEligibilityEvidence{
		opportunity.OpportunityID.String(): {PopulationAuthority: careerRef(values.Kind("population_authority"), "00000000-0000-0000-0000-000000000216"), EligibilityPolicyRevision: careerRevision(t, "eligibility-policy", 3), Eligible: true, EligibilityReason: "authorized eligibility policy passed", MatchExplanation: "selected target role and skill threshold"},
	}}
}
