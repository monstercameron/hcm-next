package model_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_MODEL_021 is the PRIMARY test for source-authority assignments.
//
// RED: authority resolution blocks no-owner, overlapping-exclusive owner,
// stale source and writer outside effective scope.
//
// GREEN: decision returns authoritative system, field/domain scope, effective
// interval, freshness, merge/conflict policy and evidence.
func TestTodo_MODEL_021(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		t.Run("no owner", func(t *testing.T) {
			_, err := model.ResolveAuthority(nil, "employment.status", instant(2026, time.January, 1), nil)
			if !errors.Is(err, model.ErrNoAuthority) {
				t.Fatalf("resolved a scope with no assignment: %v", err)
			}
		})

		t.Run("overlapping exclusive owners", func(t *testing.T) {
			a1 := model.SourceAuthorityAssignment{
				AssignmentRef: "a1", Kind: model.AuthorityInternal, DomainScope: "employment.status",
				Effective: mustOpenInterval(t, 2026, time.January, 1), Exclusive: true,
				Merge: model.MergeAuthorityPrecedence, EvidenceRef: "e1",
			}
			a2 := model.SourceAuthorityAssignment{
				AssignmentRef: "a2", Kind: model.AuthorityExternalSystem, DomainScope: "employment.status",
				Effective: mustOpenInterval(t, 2026, time.January, 1), Exclusive: true,
				Merge: model.MergeAuthorityPrecedence, EvidenceRef: "e2",
			}
			_, err := model.ResolveAuthority([]model.SourceAuthorityAssignment{a1, a2},
				"employment.status", instant(2026, time.June, 1), nil)
			if !errors.Is(err, model.ErrInvalidAuthorityAssignment) {
				t.Fatalf("resolved through two overlapping exclusive owners: %v", err)
			}
		})

		t.Run("stale source", func(t *testing.T) {
			a := model.SourceAuthorityAssignment{
				AssignmentRef: "a1", Kind: model.AuthorityExternalSystem, DomainScope: "connector_operation",
				Effective: mustOpenInterval(t, 2026, time.January, 1), Exclusive: true,
				FreshnessSeconds: 60, Merge: model.MergeManualReconciliation, EvidenceRef: "e1",
			}
			last := instant(2026, time.January, 1)
			asOf := values.NewInstant(last.Time().Add(2 * time.Hour))
			_, err := model.ResolveAuthority([]model.SourceAuthorityAssignment{a}, "connector_operation", asOf, &last)
			if !errors.Is(err, model.ErrAuthorityOutOfScope) {
				t.Fatalf("resolved a stale source: %v", err)
			}
		})

		t.Run("writer outside effective scope", func(t *testing.T) {
			a := model.SourceAuthorityAssignment{
				AssignmentRef: "a1", Kind: model.AuthorityInternal, DomainScope: "employment.status",
				Effective: mustInterval(t, 2020, time.January, 1, 2025, time.January, 1),
				Merge:     model.MergeAuthorityPrecedence, EvidenceRef: "e1",
			}
			if err := model.CheckWriterScope(a, instant(2026, time.January, 1)); !errors.Is(err, model.ErrAuthorityOutOfScope) {
				t.Fatalf("a writer outside the effective interval was allowed: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		a := model.SourceAuthorityAssignment{
			AssignmentRef: "authority.employment/v1", Kind: model.AuthorityInternal,
			DomainScope: "employment.status", Effective: mustOpenInterval(t, 2020, time.January, 1),
			Exclusive: true, FreshnessSeconds: 3600, Merge: model.MergeAuthorityPrecedence,
			EvidenceRef: "evidence.authority_registration/v1",
		}
		decision, err := model.ResolveAuthority([]model.SourceAuthorityAssignment{a},
			"employment.status", instant(2026, time.January, 1), nil)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if decision.AssignmentRef != a.AssignmentRef {
			t.Fatalf("decision system = %s, want %s", decision.AssignmentRef, a.AssignmentRef)
		}
		if decision.Scope != a.DomainScope {
			t.Fatalf("decision scope = %s, want %s", decision.Scope, a.DomainScope)
		}
		if decision.Merge != a.Merge {
			t.Fatalf("decision merge policy = %s, want %s", decision.Merge, a.Merge)
		}
		if decision.EvidenceRef == "" {
			t.Fatalf("decision carries no evidence")
		}
		if decision.FreshnessSeconds != a.FreshnessSeconds {
			t.Fatalf("decision freshness = %d, want %d", decision.FreshnessSeconds, a.FreshnessSeconds)
		}
		if err := model.CheckWriterScope(a, instant(2026, time.January, 1)); err != nil {
			t.Fatalf("a writer inside scope was rejected: %v", err)
		}
	})
}

// TestTodo_MODEL_021_Property asserts that every compiled authority
// assignment in the catalog resolves for its own scope at its own effective
// start, and that resolving twice never yields two different assignment refs.
func TestTodo_MODEL_021_Property(t *testing.T) {
	reg := mustRegistry(t)
	all := reg.Authorities()
	for _, a := range all {
		start, ok := a.Effective.StartInstant()
		if !ok {
			t.Fatalf("%s has no start instant", a.AssignmentRef)
		}
		d1, err := model.ResolveAuthority(all, a.DomainScope, start, nil)
		if err != nil {
			t.Fatalf("%s did not resolve at its own start: %v", a.AssignmentRef, err)
		}
		d2, err := model.ResolveAuthority(all, a.DomainScope, start, nil)
		if err != nil || d1.AssignmentRef != d2.AssignmentRef {
			t.Fatalf("%s resolved inconsistently across calls", a.AssignmentRef)
		}
	}
}

// TestTodo_MODEL_021_Golden pins the compiled authority-assignment table.
func TestTodo_MODEL_021_Golden(t *testing.T) {
	reg := mustRegistry(t)
	type row struct {
		Ref, Kind, Scope, Merge, Evidence string
		Exclusive                         bool
		FreshnessSeconds                  uint32
	}
	var rows []row
	for _, a := range reg.Authorities() {
		rows = append(rows, row{
			Ref: a.AssignmentRef, Kind: string(a.Kind), Scope: a.DomainScope, Merge: string(a.Merge),
			Evidence: a.EvidenceRef, Exclusive: a.Exclusive, FreshnessSeconds: a.FreshnessSeconds,
		})
	}
	goldenJSON(t, "model_021_authorities.json", rows)
}

// FuzzTodo_MODEL_021 fuzzes freshness-based staleness: ResolveAuthority must
// report ErrAuthorityOutOfScope exactly when the observed age exceeds the
// declared freshness window.
func FuzzTodo_MODEL_021(f *testing.F) {
	f.Add(uint32(60), int64(30))
	f.Add(uint32(60), int64(90))
	f.Add(uint32(0), int64(1000000))
	f.Fuzz(func(t *testing.T, freshness uint32, ageSeconds int64) {
		if ageSeconds < 0 {
			ageSeconds = -ageSeconds
		}
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		last := values.NewInstant(base)
		asOf := values.NewInstant(base.Add(time.Duration(ageSeconds) * time.Second))
		iv, err := values.NewOpenInstantInterval(last)
		if err != nil {
			t.Fatalf("build interval: %v", err)
		}
		a := model.SourceAuthorityAssignment{
			AssignmentRef: "a1", Kind: model.AuthorityInternal, DomainScope: "scope",
			Effective: iv, FreshnessSeconds: freshness,
			Merge: model.MergeAuthorityPrecedence, EvidenceRef: "e1",
		}
		_, resolveErr := model.ResolveAuthority([]model.SourceAuthorityAssignment{a}, "scope", asOf, &last)
		wantStale := freshness > 0 && float64(ageSeconds) > float64(freshness)
		if wantStale && !errors.Is(resolveErr, model.ErrAuthorityOutOfScope) {
			t.Fatalf("age %ds exceeds freshness %ds but resolution succeeded: %v", ageSeconds, freshness, resolveErr)
		}
		if !wantStale && resolveErr != nil {
			t.Fatalf("age %ds within freshness %ds but resolution failed: %v", ageSeconds, freshness, resolveErr)
		}
	})
}
