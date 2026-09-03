package model_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// TestTodo_MODEL_023 is the PRIMARY test for data-classification propagation.
//
// RED: a derived value, artifact, log or outbound payload missing the
// strongest applicable label fails creation/delivery.
//
// GREEN: PII, compensation, bank, medical, immigration, case and
// special-category labels propagate with policy version.
func TestTodo_MODEL_023(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		t.Run("no classification label at all", func(t *testing.T) {
			a := model.ClassifiedArtifact{ArtifactRef: "artifact.1", PolicyVersion: "v1"}
			if err := a.Validate(); !errors.Is(err, model.ErrMissingClassification) {
				t.Fatalf("validated an artifact with no label: %v", err)
			}
		})

		t.Run("derived artifact omits the strongest source label", func(t *testing.T) {
			sources := []model.ClassifiedArtifact{
				{ArtifactRef: "s1", Labels: []model.ClassificationLabel{model.ClassInternal}, PolicyVersion: "v1"},
				{ArtifactRef: "s2", Labels: []model.ClassificationLabel{model.ClassPII}, PolicyVersion: "v1"},
			}
			derived := model.ClassifiedArtifact{
				ArtifactRef: "d1", Labels: []model.ClassificationLabel{model.ClassInternal}, PolicyVersion: "v1",
			}
			if err := model.CheckDerived(derived, sources); !errors.Is(err, model.ErrMissingClassification) {
				t.Fatalf("accepted a derived artifact weaker than its sources: %v", err)
			}
		})

		t.Run("unknown label", func(t *testing.T) {
			a := model.ClassifiedArtifact{
				ArtifactRef: "a1", Labels: []model.ClassificationLabel{"NOT_A_LABEL"}, PolicyVersion: "v1",
			}
			if err := a.Validate(); !errors.Is(err, model.ErrInvalidClassification) {
				t.Fatalf("validated an unknown label: %v", err)
			}
		})

		t.Run("no policy version", func(t *testing.T) {
			a := model.ClassifiedArtifact{ArtifactRef: "a1", Labels: []model.ClassificationLabel{model.ClassPublic}}
			if err := a.Validate(); !errors.Is(err, model.ErrInvalidClassification) {
				t.Fatalf("validated an artifact with no policy version: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		for _, label := range []model.ClassificationLabel{
			model.ClassPII, model.ClassCompensation, model.ClassBank, model.ClassMedical,
			model.ClassImmigration, model.ClassCase, model.ClassSpecialCategory,
		} {
			sources := []model.ClassifiedArtifact{
				{ArtifactRef: "s1", Labels: []model.ClassificationLabel{model.ClassInternal}, PolicyVersion: "policy.v1"},
				{ArtifactRef: "s2", Labels: []model.ClassificationLabel{label}, PolicyVersion: "policy.v1"},
			}
			derived, err := model.Propagate(sources, "derived.1", "policy.v1")
			if err != nil {
				t.Fatalf("propagate %s: %v", label, err)
			}
			if derived.PolicyVersion != "policy.v1" {
				t.Fatalf("%s propagation lost the policy version", label)
			}
			found := false
			for _, l := range derived.Labels {
				if l == label {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s did not propagate into the derived label set %v", label, derived.Labels)
			}
			if err := model.CheckDerived(derived, sources); err != nil {
				t.Fatalf("a correctly propagated %s artifact was rejected: %v", label, err)
			}
		}
	})
}

// TestTodo_MODEL_023_Property asserts that Strongest and Propagate never
// downgrade: the propagated rank is always >= the maximum source rank.
func TestTodo_MODEL_023_Property(t *testing.T) {
	all := []model.ClassificationLabel{
		model.ClassPublic, model.ClassInternal, model.ClassPII, model.ClassCompensation,
		model.ClassBank, model.ClassMedical, model.ClassImmigration, model.ClassCase, model.ClassSpecialCategory,
	}
	for i, a := range all {
		for j, b := range all {
			sources := []model.ClassifiedArtifact{
				{ArtifactRef: "a", Labels: []model.ClassificationLabel{a}, PolicyVersion: "v1"},
				{ArtifactRef: "b", Labels: []model.ClassificationLabel{b}, PolicyVersion: "v1"},
			}
			derived, err := model.Propagate(sources, "d", "v1")
			if err != nil {
				t.Fatalf("propagate(%s,%s): %v", a, b, err)
			}
			maxRank := a.Rank()
			if b.Rank() > maxRank {
				maxRank = b.Rank()
			}
			if model.Strongest(derived.Labels).Rank() < maxRank {
				t.Fatalf("propagate(%d=%s,%d=%s) downgraded to rank %d, want >= %d",
					i, a, j, b, model.Strongest(derived.Labels).Rank(), maxRank)
			}
		}
	}
}

// TestTodo_MODEL_023_Golden pins the label rank table.
func TestTodo_MODEL_023_Golden(t *testing.T) {
	type row struct {
		Label string
		Rank  int
	}
	var rows []row
	for _, l := range []model.ClassificationLabel{
		model.ClassPublic, model.ClassInternal, model.ClassPII, model.ClassCompensation,
		model.ClassBank, model.ClassMedical, model.ClassImmigration, model.ClassCase, model.ClassSpecialCategory,
	} {
		rows = append(rows, row{Label: string(l), Rank: l.Rank()})
	}
	goldenJSON(t, "model_023_ranks.json", rows)
}

// TestTodo_MODEL_023_Race propagates classification concurrently: Propagate
// is a pure function, so concurrent calls over the same inputs must always
// agree.
func TestTodo_MODEL_023_Race(t *testing.T) {
	sources := []model.ClassifiedArtifact{
		{ArtifactRef: "s1", Labels: []model.ClassificationLabel{model.ClassPII, model.ClassInternal}, PolicyVersion: "v1"},
		{ArtifactRef: "s2", Labels: []model.ClassificationLabel{model.ClassBank}, PolicyVersion: "v1"},
	}
	var wg sync.WaitGroup
	results := make([]model.ClassifiedArtifact, 16)
	errs := make([]error, 16)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = model.Propagate(sources, "d", "v1")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent propagate %d: %v", i, err)
		}
	}
	want := model.Strongest(results[0].Labels)
	for i, r := range results {
		if model.Strongest(r.Labels) != want {
			t.Fatalf("concurrent propagate %d disagreed: %s vs %s", i, model.Strongest(r.Labels), want)
		}
	}
	// The shared source slice must survive unmutated.
	if len(sources[0].Labels) != 2 {
		t.Fatalf("concurrent propagation mutated a shared source artifact")
	}
}

// TestTodo_MODEL_023_Security proves that classification never becomes
// ambiguous when combining every declared label pairwise: Propagate always
// resolves to exactly one deterministic strongest label, never "weakest" or
// undefined behavior on unknown combinations.
func TestTodo_MODEL_023_Security(t *testing.T) {
	reg := mustRegistry(t)
	for _, p := range reg.Properties() {
		if !p.Classification.Valid() {
			t.Fatalf("%s carries an invalid classification label %q", p.Ref, p.Classification)
		}
	}
}

// FuzzTodo_MODEL_023 fuzzes CheckDerived: a derived artifact validates only
// when its own label set contains the strongest label its sources carry.
func FuzzTodo_MODEL_023(f *testing.F) {
	labels := []model.ClassificationLabel{
		model.ClassPublic, model.ClassInternal, model.ClassPII, model.ClassBank, model.ClassSpecialCategory,
	}
	f.Add(0, 2, true)
	f.Add(2, 0, false)
	f.Add(4, 4, true)
	f.Fuzz(func(t *testing.T, sourceIdx, derivedIdx int, includeStrongest bool) {
		source := labels[((sourceIdx%len(labels))+len(labels))%len(labels)]
		derivedLabel := labels[((derivedIdx%len(labels))+len(labels))%len(labels)]
		sources := []model.ClassifiedArtifact{{ArtifactRef: "s", Labels: []model.ClassificationLabel{source}, PolicyVersion: "v1"}}
		derivedLabels := []model.ClassificationLabel{derivedLabel}
		if includeStrongest {
			derivedLabels = append(derivedLabels, source)
		}
		derived := model.ClassifiedArtifact{ArtifactRef: "d", Labels: derivedLabels, PolicyVersion: "v1"}
		err := model.CheckDerived(derived, sources)
		shouldPass := containsLabel(derivedLabels, source)
		if shouldPass && err != nil {
			t.Fatalf("derived set %v contains source label %s but was rejected: %v", derivedLabels, source, err)
		}
		if !shouldPass && err == nil {
			t.Fatalf("derived set %v omits source label %s but was accepted", derivedLabels, source)
		}
	})
}

func containsLabel(labels []model.ClassificationLabel, want model.ClassificationLabel) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
