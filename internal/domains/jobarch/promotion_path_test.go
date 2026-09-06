package jobarch

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func pathPercent(t *testing.T, text string) values.Percentage {
	t.Helper()
	p, err := values.NewPercentage(text, 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("percentage %q: %v", text, err)
	}
	return p
}

func testPromotionPath(t *testing.T) PromotionPathRevision {
	t.Helper()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return PromotionPathRevision{
		ID: "path-hrbp-2-3", PathID: "path-hrbp-2-3", Revision: "1",
		From:                       ProfileRevisionRef{ProfileID: "profile-2", Revision: "1"},
		To:                         ProfileRevisionRef{ProfileID: "profile-3", Revision: "1"},
		Kind:                       PromotionPathUpward,
		MinimumBaseIncrease:        pathPercent(t, "0.0300"),
		MaximumBaseIncrease:        pathPercent(t, "0.1500"),
		CompensationPolicyRef:      VersionedReference{Ref: "promotion-rules", Revision: "2026.1", Authority: "rewards", EffectiveFrom: at},
		BenefitEligibilityRuleRefs: []VersionedReference{{Ref: "benefit-eligibility", Revision: "2026.1", Authority: "benefits", EffectiveFrom: at}},
		Authority:                  "job-architecture", Lifecycle: LifecyclePublished,
		EffectiveFrom: at, KnownFrom: at,
	}
}

func testPathArchitecture(t *testing.T) ArchitectureRevision {
	t.Helper()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	family := JobFamilyRevision{ID: "family-hr", FamilyID: "family-hr", Revision: "1", Code: "HR", Name: "Human Resources", Lifecycle: LifecyclePublished, EffectiveFrom: at, KnownFrom: at}
	level2 := JobLevelRevision{ID: "level-2", LevelID: "level-2", Revision: "1", FamilyID: family.ID, Code: "P2", Title: "Professional 2", Rank: 2, Lifecycle: LifecyclePublished, EffectiveFrom: at, KnownFrom: at}
	level3 := JobLevelRevision{ID: "level-3", LevelID: "level-3", Revision: "1", FamilyID: family.ID, Code: "P3", Title: "Professional 3", Rank: 3, Lifecycle: LifecyclePublished, EffectiveFrom: at, KnownFrom: at}
	grade2 := JobGradeRevision{ID: "grade-2", GradeID: "grade-2", Revision: "1", LevelID: level2.ID, Code: "P2", Name: "P2", Lifecycle: LifecyclePublished, EffectiveFrom: at, KnownFrom: at}
	grade3 := JobGradeRevision{ID: "grade-3", GradeID: "grade-3", Revision: "1", LevelID: level3.ID, Code: "P3", Name: "P3", Lifecycle: LifecyclePublished, EffectiveFrom: at, KnownFrom: at}
	profiles := []JobProfileRevision{
		{ID: "profile-2", ProfileID: "profile-2", Revision: "1", FamilyID: family.ID, LevelID: level2.ID, GradeID: grade2.ID, JobCode: "OPS-HRBP2", Title: "HR Business Partner", Lifecycle: LifecyclePublished, EffectiveFrom: at, KnownFrom: at},
		{ID: "profile-3", ProfileID: "profile-3", Revision: "1", FamilyID: family.ID, LevelID: level3.ID, GradeID: grade3.ID, JobCode: "OPS-HRBP3", Title: "Senior HR Business Partner", Lifecycle: LifecyclePublished, EffectiveFrom: at, KnownFrom: at},
	}
	a, err := NewArchitectureRevision(ArchitectureRevision{ID: "northwind", Revision: "1", Families: []JobFamilyRevision{family}, Levels: []JobLevelRevision{level2, level3}, Grades: []JobGradeRevision{grade2, grade3}, Profiles: profiles})
	if err != nil {
		t.Fatalf("architecture: %v", err)
	}
	return a
}

func TestPromotionPathValidatesAgainstRankedArchitecture(t *testing.T) {
	p := testPromotionPath(t)
	if err := p.ValidateAgainst(testPathArchitecture(t)); err != nil {
		t.Fatalf("ValidateAgainst: %v", err)
	}
	first, err := p.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	second, _ := p.Digest()
	if first == "" || first != second {
		t.Fatalf("digest is not deterministic: %q / %q", first, second)
	}
}

func TestPromotionPathRejectsContradictoryShape(t *testing.T) {
	p := testPromotionPath(t)
	p.From, p.To = p.To, p.From
	if err := p.ValidateAgainst(testPathArchitecture(t)); !errors.Is(err, ErrPromotionPathShape) {
		t.Fatalf("error = %v, want ErrPromotionPathShape", err)
	}
}

func TestPromotionPathUsesExactBaseIncreaseGuardrails(t *testing.T) {
	p := testPromotionPath(t)
	if err := p.AllowsBaseIncrease(pathPercent(t, "0.0750")); err != nil {
		t.Fatalf("7.5 percent should be allowed: %v", err)
	}
	if err := p.AllowsBaseIncrease(pathPercent(t, "0.1501")); !errors.Is(err, ErrBaseIncreaseOutsidePath) {
		t.Fatalf("error = %v, want ErrBaseIncreaseOutsidePath", err)
	}
}

func TestPromotionPathRequiresVersionedPolicies(t *testing.T) {
	p := testPromotionPath(t)
	p.CompensationPolicyRef = VersionedReference{}
	if err := p.Validate(); !errors.Is(err, ErrInvalidPromotionPath) {
		t.Fatalf("error = %v, want ErrInvalidPromotionPath", err)
	}
}
