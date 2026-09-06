package fixtures

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/jobarch"
)

func TestPublishedPromotionPathsPinTheArchitectureAndExactRules(t *testing.T) {
	architecture, err := JobArchitecture()
	if err != nil {
		t.Fatalf("JobArchitecture: %v", err)
	}
	paths, err := PromotionPaths()
	if err != nil {
		t.Fatalf("PromotionPaths: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %d, want 2", len(paths))
	}
	want := paths[0]
	if want.SourceJobCode != "OPS-HRBP2" || want.SourceGrade != "P2" || want.TargetJobCode != "OPS-HRBP3" || want.TargetGrade != "P3" {
		t.Fatalf("unexpected HR ladder edge: %+v", want)
	}
	if want.Path.MinimumBaseIncrease.String() != "0.0500" || want.Path.MaximumBaseIncrease.String() != "0.1500" {
		t.Fatalf("base rules = %s..%s", want.Path.MinimumBaseIncrease.String(), want.Path.MaximumBaseIncrease.String())
	}
	if want.Path.Kind != jobarch.PromotionPathUpward || len(want.Path.BenefitEligibilityRuleRefs) != 1 {
		t.Fatalf("governance metadata was lost: %+v", want.Path)
	}
	if err := want.Path.ValidateAgainst(architecture); err != nil {
		t.Fatalf("path no longer validates against its catalog: %v", err)
	}
}

func TestEveryPublishedJobProfileHasAnExactPayBand(t *testing.T) {
	architecture, err := JobArchitecture()
	if err != nil {
		t.Fatal(err)
	}
	bands, err := BandScopes()
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range architecture.Profiles {
		grade := ""
		for _, candidate := range architecture.Grades {
			if candidate.GradeIDOrID() == profile.GradeIDOrRef() {
				grade = candidate.Code
				break
			}
		}
		covered := false
		for _, band := range bands {
			if band.JobCode == profile.JobCode && band.Grade == grade {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("published profile %s %s has no exact pay band", profile.JobCode, grade)
		}
	}
}
