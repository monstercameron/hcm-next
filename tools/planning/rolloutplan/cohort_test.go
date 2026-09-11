package rolloutplan

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
)

func cohortPopulation() population.Snapshot {
	return population.Snapshot{
		DefinitionID:     "pop/reviewers",
		DefinitionDigest: "sha256:popdef",
		RevisionVersion:  "7",
		SubjectIDs:       []string{"sub:1", "sub:2", "sub:3", "sub:4"},
		Digest:           "sha256:popsnap",
	}
}

func cohortPlacements() []MemberPlacement {
	return []MemberPlacement{
		{SubjectID: "sub:1", Tenant: "tenant-1", Org: "org:acme", Residency: "EU"},
		{SubjectID: "sub:2", Tenant: "tenant-1", Org: "org:acme", Residency: "US"},
		{SubjectID: "sub:3", Tenant: "tenant-2", Org: "org:acme", Residency: "EU"},
		{SubjectID: "sub:4", Tenant: "tenant-1", Org: "org:other", Residency: "EU"},
	}
}

func cohortTargets() []StageTarget {
	return []StageTarget{
		{Stage: "canary", Tenants: []string{"tenant-1"}, Orgs: []string{"org:acme"}, Residencies: []string{"EU", "US"}},
		{Stage: "broad", Tenants: []string{"tenant-1", "tenant-2"}, Orgs: []string{"org:acme"}, Residencies: []string{"EU"}},
	}
}

func cohortRequest() CohortRequest {
	return CohortRequest{
		Plan:       validPlan(),
		Targets:    cohortTargets(),
		Population: cohortPopulation(),
		Placements: cohortPlacements(),
	}
}

func TestTodo_ROLLOUT_002(t *testing.T) {
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatalf("valid cohort request rejected: %v", err)
	}
	if len(set.Cohorts) != 2 {
		t.Fatalf("cohorts = %d, want one per stage", len(set.Cohorts))
	}
	canary := set.Cohorts[0]
	if len(canary.Members) != 2 || canary.Members[0] != "sub:1" || canary.Members[1] != "sub:2" {
		t.Fatalf("canary resolved %+v, want [sub:1 sub:2]", canary.Members)
	}
	broad := set.Cohorts[1]
	if len(broad.Members) != 2 || broad.Members[0] != "sub:1" || broad.Members[1] != "sub:3" {
		t.Fatalf("broad resolved %+v, want [sub:1 sub:3]", broad.Members)
	}
	if canary.Digest == broad.Digest || !strings.HasPrefix(canary.Digest, "sha256:") {
		t.Fatal("cohort digests not stage-bound")
	}
	again, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != set.Digest {
		t.Fatal("duplicate freeze created a different identity")
	}
	if set.Owner == "" || set.PlanDigest == "" || set.PopulationDigest != "sha256:popsnap" {
		t.Fatalf("cohort set lost its bindings: %+v", set)
	}

	failures := []struct {
		name   string
		mutate func(*CohortRequest)
		code   string
	}{
		{"invalid plan", func(r *CohortRequest) { r.Plan.Owner = "" }, InvalidPlan},
		{"unresolved population", func(r *CohortRequest) { r.Population.Digest = "" }, MissingPopulation},
		{"unknown stage", func(r *CohortRequest) { r.Targets[0].Stage = "void" }, UnknownStage},
		{"empty target", func(r *CohortRequest) { r.Targets[0].Tenants = nil }, EmptyTarget},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			req := cohortRequest()
			tc.mutate(&req)
			if _, err := FreezeCohorts(req); !HasCohortCode(err, tc.code) {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}

	t.Run("unplaced subjects excluded with evidence", func(t *testing.T) {
		req := cohortRequest()
		req.Placements = req.Placements[:2]
		set, err := FreezeCohorts(req)
		if err != nil {
			t.Fatal(err)
		}
		if set.Cohorts[1].ExcludedCount != 2 {
			t.Fatalf("excluded = %d, want 2: %+v", set.Cohorts[1].ExcludedCount, set.Cohorts[1])
		}
	})
}
