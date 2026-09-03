package sources_test

import (
	"sort"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/tools/gen/schemaflux/sources"
)

// TestTodo_MSRC_004 is the MSRC-004 primary test: it compiles the checked-in
// people/position/compensation/budget family sources and asserts the exact
// fifteen Phase 1 aggregates internal/intent/model.Catalog() registers for
// this cluster (Person, Worker, Employment, Assignment, OrganizationUnit,
// OrganizationRelationship, LegalEntity, WorkerSummary, Position, Job,
// PositionOccupancy, CompensationGrade, CompensationPackage,
// CompensationComponent, BudgetReservation) plus the four relationships
// (ManagerRelationship, OrganizationHierarchy, AssignmentPosition,
// EmploymentLegalEntity, PositionOccupant) round-trip with zero mismatches
// against the compiled Go registry, matching the database mapping
// definitions/model/storage-disposition.yaml already reports for the same
// entity set.
func TestTodo_MSRC_004(t *testing.T) {
	manifest, bundle := compileValid(t)

	wantCoveredEntities := []string{
		"Person/v1", "Worker/v1", "Employment/v1", "Assignment/v1",
		"OrganizationUnit/v1", "OrganizationRelationship/v1", "LegalEntity/v1", "WorkerSummary/v1",
		"Position/v1", "Job/v1", "PositionOccupancy/v1",
		"CompensationGrade/v1", "CompensationPackage/v1", "CompensationComponent/v1",
		"BudgetReservation/v1",
	}
	families := map[string]bool{"people": true, "position": true, "compensation": true, "budget": true}
	byRef := map[string]sources.EntitySource{}
	for _, e := range bundle.Entities {
		if families[e.Family] {
			byRef[e.Ref()] = e
		}
	}
	if len(wantCoveredEntities) != 15 {
		t.Fatalf("test setup error: wantCoveredEntities has %d entries, want 15", len(wantCoveredEntities))
	}
	for _, ref := range wantCoveredEntities {
		e, ok := byRef[ref]
		if !ok {
			t.Errorf("%s not found in people/position/compensation/budget family sources", ref)
			continue
		}
		if !e.Covered {
			t.Errorf("%s: covered = false, want true", ref)
		}
	}

	wantRelationships := []string{
		"ManagerRelationship/v1", "OrganizationHierarchy/v1", "AssignmentPosition/v1",
		"EmploymentLegalEntity/v1", "PositionOccupant/v1",
	}
	relByRef := map[string]sources.RelationshipSource{}
	for _, r := range bundle.Relationships {
		relByRef[r.Ref()] = r
	}
	for _, ref := range wantRelationships {
		r, ok := relByRef[ref]
		if !ok {
			t.Errorf("relationship %s not found", ref)
			continue
		}
		if !r.Covered {
			t.Errorf("relationship %s: covered = false, want true", ref)
		}
	}

	mismatches, err := sources.CrossCheckModel(manifest)
	if err != nil {
		t.Fatalf("CrossCheckModel: %v", err)
	}
	for _, m := range mismatches {
		t.Errorf("cross-check mismatch: %s", m)
	}
}

// TestTodo_MSRC_004_Golden pins the exact sorted entity-name list for each of
// the four families.
func TestTodo_MSRC_004_Golden(t *testing.T) {
	bundle := loadValidBundle(t)

	cases := []struct {
		family string
		want   []string
	}{
		{"people", []string{
			"Address", "Assignment", "AssignmentRevision", "ContactPoint", "Employment",
			"EmploymentStatusRevision", "IdentityClaim", "LegalEntity", "OrganizationRelationship",
			"OrganizationUnit", "Person", "PersonNameRevision", "PersonRole", "Worker",
			"WorkerLifecycleTransition", "WorkerSummary",
		}},
		{"position", []string{
			"Job", "JobFamily", "JobLevel", "Position", "PositionOccupancy", "PositionRevision",
			"PositionReservation", "VacancyProjection", "WorkforcePlan", "HeadcountPlanLine",
		}},
		{"compensation", []string{
			"BandPositionResult", "CompensationBand", "CompensationChangeProposal", "CompensationComponent",
			"CompensationGrade", "CompensationPackage", "CompensationSimulation", "PayBasis", "RewardGrant",
		}},
		{"budget", []string{
			"BudgetReservation", "CompensationBudget", "HeadcountBudgetAuthorization",
		}},
	}
	for _, c := range cases {
		var got []string
		for _, e := range entitiesInFamily(bundle, c.family) {
			got = append(got, e.Name)
		}
		sort.Strings(got)
		want := append([]string(nil), c.want...)
		sort.Strings(want)
		assertStringSlicesEqual(t, c.family, got, want)
	}
}

// TestTodo_MSRC_004_Race compiles and cross-checks the same loaded bundle
// concurrently from many goroutines. Compile and CrossCheckModel only read
// their inputs and build fresh local maps/slices, so this must be race-free
// under `go test -race`; the test exists to prove that property rather than
// assume it, per DB-010's TEST MATRIX RACE requirement this todo shares.
func TestTodo_MSRC_004_Race(t *testing.T) {
	bundle := loadValidBundle(t)

	const goroutines = 16
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manifest, errs := sources.Compile(bundle)
			if len(errs) != 0 {
				errCh <- errsToOne(errs)
				return
			}
			if _, err := sources.CrossCheckModel(manifest); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent Compile/CrossCheckModel failed: %v", err)
	}
}

func errsToOne(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errs[0]
}
