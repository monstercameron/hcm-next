package jobarch

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/governance/decision"
)

func governedRequirements() JobProfileRequirements {
	from := archAt(1)
	return JobProfileRequirements{
		Classification: VersionedReference{Ref: "classification:engineering", Revision: "v3", Authority: "authority:taxonomy", EffectiveFrom: from},
		Qualifications: []QualificationReference{{VersionedReference: VersionedReference{Ref: "qualification:engineering", Revision: "v2", Authority: "authority:qualification", EffectiveFrom: from}}},
		Skills:         []SkillRequirementReference{{VersionedReference: VersionedReference{Ref: "skill:systems", Revision: "v4", Authority: "authority:skills", EffectiveFrom: from}, Proficiency: 3}},
		Credentials:    []CredentialRequirementReference{{VersionedReference: VersionedReference{Ref: "credential:license", Revision: "v1", Authority: "authority:credentials", EffectiveFrom: from}, Level: 1, ValidFrom: from, ValidTo: archAt(365)}},
		Compensation:   CompensationReference{GradeRef: "grade-1", GradeRevision: "g1", BandRef: "band:engineering", BandRevision: "v7", Currency: "USD", Authority: "authority:rewards", EffectiveFrom: from},
	}
}

func governedCatalog() ReferenceCatalog {
	from := archAt(1)
	return ReferenceCatalog{Records: []ReferenceRecord{
		{Kind: ReferenceClassification, Ref: "classification:engineering", Revision: "v3", Authority: "authority:taxonomy", EffectiveFrom: from},
		{Kind: ReferenceQualification, Ref: "qualification:engineering", Revision: "v2", Authority: "authority:qualification", EffectiveFrom: from},
		{Kind: ReferenceSkill, Ref: "skill:systems", Revision: "v4", Authority: "authority:skills", EffectiveFrom: from},
		{Kind: ReferenceCredential, Ref: "credential:license", Revision: "v1", Authority: "authority:credentials", EffectiveFrom: from},
		{Kind: ReferenceGrade, Ref: "grade-1", Revision: "g1", Authority: "authority:rewards", EffectiveFrom: from},
		{Kind: ReferenceBand, Ref: "band:engineering", Revision: "v7", Authority: "authority:rewards", EffectiveFrom: from, Currency: "USD"},
	}}
}

func governedCandidate(t *testing.T) (ArchitectureRevision, ArchitectureRevision) {
	t.Helper()
	current := validArchitecture(t)
	candidate := current
	candidate.Revision = "r2"
	candidate.SupersedesRevision = current.Revision
	candidate.Profiles = cloneProfiles(current.Profiles)
	candidate.Profiles[0].Revision = "p2"
	candidate.Profiles[0].Lifecycle = LifecycleDraft
	candidate.Profiles[0].Requirements = governedRequirements()
	var err error
	candidate, err = NewArchitectureRevision(candidate)
	if err != nil {
		t.Fatalf("governed candidate: %v", err)
	}
	return current, candidate
}

func publicationRequest(candidate ArchitectureRevision) PublicationRequest {
	return PublicationRequest{
		Candidate: candidate, ProfileID: "profile-1", ConfigurationDigest: "sha256:configuration-v1", References: governedCatalog(), EffectiveAt: archAt(5),
		Approval:      decision.Decision{State: decision.Allow, ProposalRevisionDigest: candidate.CanonicalDigest},
		Compatibility: CompatibilityReport{RevisionDigest: candidate.CanonicalDigest, Compatible: true},
	}
}

func TestJobProfileRequirementsBindClassificationQualificationAndRewardReferences(t *testing.T) {
	current, candidate := governedCandidate(t)
	published, receipt, err := current.PublishProfileRevision(publicationRequest(candidate))
	if err != nil {
		t.Fatalf("PublishProfileRevision: %v", err)
	}
	profile := published.Profiles[0]
	if profile.Lifecycle != LifecyclePublished || profile.Requirements.Compensation.BandRef != "band:engineering" || profile.Requirements.Skills[0].Proficiency != 3 {
		t.Fatalf("published profile lost governed requirements: %+v", profile)
	}
	if receipt.ConfigurationDigest == "" || receipt.ReferenceCatalogHash == "" || receipt.PublishedRevision == "" {
		t.Fatalf("publication receipt is incomplete: %+v", receipt)
	}
}

func TestTodo_JOBARCH_002_Property(t *testing.T) {
	_, candidate := governedCandidate(t)
	one := candidate.Profiles[0].Canonical()
	second := candidate.Profiles[0].Canonical()
	if string(one) != string(second) || len(one) == 0 {
		t.Fatal("requirements canonical encoding is not deterministic")
	}
	if err := candidate.Profiles[0].ValidateGoverned(governedCatalog(), archAt(5)); err != nil {
		t.Fatalf("ValidateGoverned: %v", err)
	}
}

func TestTodo_JOBARCH_002_Golden(t *testing.T) {
	_, candidate := governedCandidate(t)
	text := candidate.Explain()
	for _, want := range []string{"requirements=1", "classifications=1", "qualifications=1", "skills=1", "credentials=1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Explain=%q missing %q", text, want)
		}
	}
}

func TestTodo_JOBARCH_002_Race(t *testing.T) {
	_, candidate := governedCandidate(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := candidate.Profiles[0].ValidateGoverned(governedCatalog(), archAt(5)); err != nil {
				t.Errorf("ValidateGoverned: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_JOBARCH_002_Fault(t *testing.T) {
	_, candidate := governedCandidate(t)
	catalog := governedCatalog()
	catalog.Records[0].Revision = "missing"
	if !errors.Is(candidate.Profiles[0].ValidateGoverned(catalog, archAt(5)), ErrReferenceUnresolved) {
		t.Fatal("unresolved classification reference was accepted")
	}
	bad := candidate.Profiles[0]
	bad.Requirements.Skills[0].Proficiency = 0
	if !errors.Is(bad.Validate(), ErrRequirementsIncomplete) {
		t.Fatal("skill without proficiency was accepted")
	}
}

func TestTodo_JOBARCH_002_Security(t *testing.T) {
	_, candidate := governedCandidate(t)
	text := candidate.Explain()
	if strings.Contains(text, "sensitive responsibilities") || strings.Contains(text, "authority:rewards") {
		t.Fatalf("Explain disclosed profile details: %q", text)
	}
}

func TestTodo_JOBARCH_002_Conformance(t *testing.T) {
	_, candidate := governedCandidate(t)
	profile := candidate.Profiles[0]
	if profile.ClassificationRef.Ref != "" || profile.CompensationRef.BandRef != "" {
		t.Fatal("direct aliases unexpectedly populated")
	}
	if err := profile.Requirements.ValidateGoverned(governedCatalog(), profile.EffectiveFrom, profile.EffectiveTo, profile.EffectiveFrom); err != nil {
		t.Fatalf("requirements conformance: %v", err)
	}
}

func TestTodo_JOBARCH_002_Mutation(t *testing.T) {
	_, candidate := governedCandidate(t)
	copy := cloneProfiles(candidate.Profiles)
	digest := string(copy[0].Canonical())
	copy[0].Requirements.Skills[0].Proficiency = 5
	if string(candidate.Profiles[0].Canonical()) != digest {
		t.Fatal("detached profile mutation changed source")
	}
}

func impactFixture(t *testing.T) ImpactAnalysis {
	t.Helper()
	impact, err := AnalyzeImpact("profile-1", "p1", "p2", ImpactInput{
		Positions:      []ImpactSubject{{ID: "position-1", ProfileID: "profile-1", HistoricalRevision: "p1", WorkerID: "worker-1"}},
		Workers:        []ImpactSubject{{ID: "worker-1", ProfileID: "profile-1", HistoricalRevision: "p1"}},
		Requisitions:   []ImpactSubject{{ID: "req-1", ProfileID: "profile-1", HistoricalRevision: "p1"}},
		Compensation:   []ImpactDependency{{Kind: ImpactCompensation, ID: "band:engineering", Revision: "v7"}},
		Qualifications: []ImpactDependency{{Kind: ImpactQualification, ID: "qualification:engineering", Revision: "v2"}},
		Access:         []ImpactDependency{{Kind: ImpactAccess, ID: "access:engineering", Revision: "v1"}},
		Talent:         []ImpactDependency{{Kind: ImpactTalent, ID: "talent:engineering", Revision: "v1"}},
	})
	if err != nil {
		t.Fatalf("AnalyzeImpact: %v", err)
	}
	return impact
}

func TestJobRevisionPublicationAndRetirementIdentifyAffectedPositionsWorkersAndIntents(t *testing.T) {
	current, candidate := governedCandidate(t)
	published, _, err := current.PublishProfileRevision(publicationRequest(candidate))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	impact := impactFixture(t)
	retired, err := published.RetireRevision(RetirementRequest{ProfileID: "profile-1", Approval: decision.Decision{State: decision.Allow}, Impact: impact, PositionReader: archPositionReader{}})
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if retired.Profiles[0].Lifecycle != LifecycleRetired {
		t.Fatalf("profile lifecycle=%s", retired.Profiles[0].Lifecycle)
	}
	if len(impact.Positions) != 1 || len(impact.Workers) != 1 || len(impact.Requisitions) != 1 || len(impact.ReviewIntents) != 7 || len(impact.MigrationIntents) != 7 {
		t.Fatalf("impact omitted a dependency class: %+v", impact)
	}
	if impact.Positions[0].HistoricalRevision != "p1" {
		t.Fatal("impact did not retain historical assignment revision")
	}
}

func TestTodo_JOBARCH_003_Property(t *testing.T) {
	one := impactFixture(t)
	two := impactFixture(t)
	if one.CanonicalDigest != two.CanonicalDigest || one.CanonicalDigest == "" {
		t.Fatal("impact digest is not deterministic")
	}
}

func TestTodo_JOBARCH_003_Golden(t *testing.T) {
	impact := impactFixture(t)
	for _, want := range []string{"review:position:position-1", "migrate:worker:worker-1", "review:compensation:band:engineering", "migrate:talent:talent:engineering"} {
		found := false
		for _, intent := range append(append([]ImpactIntent{}, impact.ReviewIntents...), impact.MigrationIntents...) {
			if intent.ID == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("impact intents missing %q", want)
		}
	}
}

func TestTodo_JOBARCH_003_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := AnalyzeImpact("profile-1", "p1", "p2", ImpactInput{Compensation: []ImpactDependency{{Kind: ImpactCompensation, ID: "band", Revision: "v1"}}}); err != nil || !got.Frozen {
				t.Errorf("impact=%+v err=%v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_JOBARCH_003_Fault(t *testing.T) {
	_, candidate := governedCandidate(t)
	req := publicationRequest(candidate)
	req.Approval.State = decision.Deny
	if !errors.Is(func() error { _, _, err := validArchitecture(t).PublishProfileRevision(req); return err }(), ErrPublicationNotApproved) {
		t.Fatal("denied publication was accepted")
	}
	if _, err := AnalyzeImpact("profile-1", "p1", "p1", ImpactInput{}); !errors.Is(err, ErrImpactIncomplete) {
		t.Fatal("same revision impact was accepted")
	}
}

func TestTodo_JOBARCH_003_Security(t *testing.T) {
	impact := impactFixture(t)
	for _, intent := range append(impact.ReviewIntents, impact.MigrationIntents...) {
		if strings.Contains(intent.ID, "salary") || strings.Contains(intent.ID, "secret") {
			t.Fatalf("impact leaked protected value: %+v", intent)
		}
	}
}

func TestTodo_JOBARCH_003_Conformance(t *testing.T) {
	impact := impactFixture(t)
	seen := map[ImpactKind]bool{}
	for _, d := range impact.Dependencies {
		seen[d.Kind] = true
	}
	for _, kind := range []ImpactKind{ImpactCompensation, ImpactQualification, ImpactAccess, ImpactTalent} {
		if !seen[kind] {
			t.Fatalf("missing impact kind %s", kind)
		}
	}
	if !impact.Frozen || impact.CanonicalDigest == "" {
		t.Fatal("impact is not frozen evidence")
	}
}

func TestTodo_JOBARCH_003_Mutation(t *testing.T) {
	in := ImpactInput{Positions: []ImpactSubject{{ID: "position-1", ProfileID: "profile-1", HistoricalRevision: "p1"}}}
	impact, err := AnalyzeImpact("profile-1", "p1", "p2", in)
	if err != nil {
		t.Fatal(err)
	}
	in.Positions[0].HistoricalRevision = "p9"
	if impact.Positions[0].HistoricalRevision != "p1" {
		t.Fatal("frozen impact changed after input mutation")
	}
}

// Keep the explicit time import tied to this file's fixture contract when
// tests are generated with a reduced matrix by downstream lanes.
var _ = time.UTC
