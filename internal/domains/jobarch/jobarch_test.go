package jobarch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func archAt(day int) time.Time { return time.Date(2026, time.January, day, 0, 0, 0, 0, time.UTC) }

func architectureFixture() ArchitectureRevision {
	return ArchitectureRevision{
		ID: "work-arch", Revision: "r1",
		Families: []JobFamilyRevision{{ID: "family-1", FamilyID: "family-1", Revision: "f1", Code: "ENG", Name: "Engineering", Lifecycle: LifecyclePublished, EffectiveFrom: archAt(1), KnownFrom: archAt(1)}},
		Levels:   []JobLevelRevision{{ID: "level-1", LevelID: "level-1", Revision: "l1", FamilyID: "family-1", Code: "IC1", Title: "Individual Contributor 1", Rank: 1, Lifecycle: LifecyclePublished, EffectiveFrom: archAt(1), KnownFrom: archAt(1)}},
		Grades:   []JobGradeRevision{{ID: "grade-1", GradeID: "grade-1", Revision: "g1", LevelID: "level-1", Code: "G1", Name: "Grade 1", Lifecycle: LifecyclePublished, EffectiveFrom: archAt(1), KnownFrom: archAt(1)}},
		Profiles: []JobProfileRevision{{ID: "profile-1", ProfileID: "profile-1", Revision: "p1", FamilyID: "family-1", LevelID: "level-1", GradeID: "grade-1", JobCode: "ENG", Title: "Engineer", Description: "sensitive responsibilities", Lifecycle: LifecyclePublished, EffectiveFrom: archAt(1), KnownFrom: archAt(1), Lineage: RevisionLineage{RootID: "profile-1"}}},
	}
}

func validArchitecture(t *testing.T) ArchitectureRevision {
	t.Helper()
	a, err := NewArchitectureRevision(architectureFixture())
	if err != nil {
		t.Fatalf("NewArchitectureRevision: %v", err)
	}
	return a
}

func TestJobArchitectureGraphRejectsCyclesMissingLevelsAndInPlaceMeaningChange(t *testing.T) {
	if _, err := NewArchitectureRevision(ArchitectureRevision{}); err == nil {
		t.Fatal("expected empty architecture refusal")
	}
	cycle := architectureFixture()
	cycle.Families[0].ParentID = "family-1"
	if _, err := NewArchitectureRevision(cycle); !errors.Is(err, ErrFamilyCycle) {
		t.Fatalf("cycle err=%v", err)
	}
	missing := architectureFixture()
	missing.Profiles[0].LevelID = "missing-level"
	if _, err := NewArchitectureRevision(missing); err == nil {
		t.Fatal("expected missing level refusal")
	}
	old := validArchitecture(t)
	next := old
	next.Profiles = cloneProfiles(old.Profiles)
	next.Profiles[0].Title = "Principal Engineer"
	next.Revision = "r2"
	next.SupersedesRevision = old.Revision
	if _, err := old.Revise(next); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("in-place meaning edit err=%v", err)
	}
}

func TestTodo_JOBARCH_001_Property(t *testing.T) {
	a := validArchitecture(t)
	one, err := a.Digest()
	if err != nil {
		t.Fatal(err)
	}
	two, err := a.Digest()
	if err != nil || one != two {
		t.Fatalf("digest=%q/%q err=%v", one, two, err)
	}
	if got := a.SortedProfileIDs(); len(got) != 1 || got[0] != "profile-1" {
		t.Fatalf("profile ids=%v", got)
	}
}

func TestTodo_JOBARCH_001_Golden(t *testing.T) {
	a := validArchitecture(t)
	got := a.Explain()
	for _, want := range []string{"work-arch", "revision=r1", "families=1", "profiles=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain=%q missing %q", got, want)
		}
	}
}

func TestTodo_JOBARCH_001_Race(t *testing.T) {
	a := validArchitecture(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
			}
		}()
	}
	wg.Wait()
}

type archPositionReader struct {
	refs []PositionReference
	err  error
}

func (r archPositionReader) ActivePositionsReferencingProfile(context.Context, string) ([]PositionReference, error) {
	return append([]PositionReference(nil), r.refs...), r.err
}

func bandCatalog(t *testing.T) payband.Catalog {
	t.Helper()
	minimum, err := values.NewMoney("50000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	midpoint, err := values.NewMoney("70000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := values.NewMoney("90000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := payband.Compile([]payband.Band{{ID: "band-1", Version: "v1", Scope: payband.Scope{JobCode: "ENG", Grade: "G1", PayZone: "US"}, Minimum: minimum, Midpoint: midpoint, Maximum: maximum}})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestTodo_JOBARCH_001_Fault(t *testing.T) {
	_, err := validArchitecture(t).RetireProfile(context.Background(), "profile-1", archPositionReader{err: errors.New("position reader down")})
	if !errors.Is(err, ErrPositionPortFailed) {
		t.Fatalf("err=%v", err)
	}
}

func TestTodo_JOBARCH_001_Security(t *testing.T) {
	a := validArchitecture(t)
	if strings.Contains(a.Explain(), "sensitive responsibilities") {
		t.Fatalf("Explain disclosed profile description: %q", a.Explain())
	}
	result, err := CheckCompatibility(a, PositionReference{ID: "pos-1", ProfileID: "profile-1", JobCode: "ENG", GradeCode: "G1", PayZone: "US", Lifecycle: position.LifecycleOpen}, bandCatalog(t), archAt(5))
	if err != nil || result.Outcome != Compatible {
		t.Fatalf("compatibility=%+v err=%v", result, err)
	}
}

func TestTodo_JOBARCH_001_Conformance(t *testing.T) {
	a := validArchitecture(t)
	result, err := CheckPositionCompatibility(a, PositionReference{ID: "pos-1", ProfileID: "profile-1", JobCode: "ENG", GradeCode: "G1", PayZone: "US", Lifecycle: position.LifecycleOpen}, bandCatalog(t), archAt(5))
	if err != nil || result.BandID != "band-1" || result.BandVersion != "v1" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	_, err = a.RetireProfile(context.Background(), "profile-1", archPositionReader{refs: []PositionReference{{ID: "pos-1", Lifecycle: position.LifecycleOpen}}})
	if !errors.Is(err, ErrProfileReferencedByActivePosition) {
		t.Fatalf("active position retirement err=%v", err)
	}
	retired, err := a.RetireProfile(context.Background(), "profile-1", archPositionReader{})
	if err != nil {
		t.Fatalf("RetireProfile: %v", err)
	}
	if retired.Profiles[0].Lifecycle != LifecycleRetired || a.Profiles[0].Lifecycle != LifecyclePublished {
		t.Fatalf("retirement mutated lineage: old=%+v new=%+v", a.Profiles[0], retired.Profiles[0])
	}
}

func TestTodo_JOBARCH_001_Mutation(t *testing.T) {
	a := validArchitecture(t)
	retired, err := a.RetireProfile(context.Background(), "profile-1", archPositionReader{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Profiles[0].Lifecycle != LifecyclePublished || retired.Profiles[0].Lifecycle != LifecycleRetired {
		t.Fatalf("successor operation changed original: old=%s new=%s", a.Profiles[0].Lifecycle, retired.Profiles[0].Lifecycle)
	}
	if _, err := CheckCompatibility(a, PositionReference{ID: "pos-1", ProfileID: "profile-1", JobCode: "ENG", GradeCode: "G2", PayZone: "US", Lifecycle: position.LifecycleOpen}, bandCatalog(t), archAt(5)); err != nil {
		t.Fatal(err)
	}
}
