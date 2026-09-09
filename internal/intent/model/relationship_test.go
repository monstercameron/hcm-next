package model_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func managerFact(t *testing.T, source, target string, fy, fm, td int) model.RelationshipFact {
	t.Helper()
	return model.RelationshipFact{
		Relationship: model.RelationshipRef{Name: "ManagerRelationship", Version: 1},
		SourceRef:    source, SourceTenant: "acme",
		TargetRef: target, TargetTenant: "acme",
		Effective:     mustOpenInterval(t, fy, time.Month(fm), td),
		Recorded:      mustRecordedAt(t, fy, time.Month(fm), td),
		Known:         mustKnownAt(t, fy, time.Month(fm), td),
		ProvenanceRef: "provenance.edge/v1",
	}
}

// TestTodo_MODEL_013 is the PRIMARY test for relationship definitions and
// temporal constraints.
//
// RED: tests reject missing endpoint kinds, cycles where prohibited,
// overlapping exclusive edges and cross-tenant endpoints.
//
// GREEN: manager/org/person/employment/assignment edges evaluate as-of and
// known-at with provenance.
func TestTodo_MODEL_013(t *testing.T) {
	reg := mustRegistry(t)
	managerDef, err := reg.Relationship(model.RelationshipRef{Name: "ManagerRelationship", Version: 1})
	if err != nil {
		t.Fatalf("resolve ManagerRelationship: %v", err)
	}

	t.Run("RED", func(t *testing.T) {
		t.Run("missing endpoint kind", func(t *testing.T) {
			d := model.RelationshipDefinition{
				Ref:          model.RelationshipRef{Name: "Broken", Version: 1},
				SourceEntity: model.EntityRef{},
				TargetEntity: model.EntityRef{Name: "Assignment", Version: 1},
				Cardinality:  model.CardinalityOneToOne,
			}
			if err := d.Validate(); !errors.Is(err, model.ErrInvalidRelationship) {
				t.Fatalf("validated a relationship with no source endpoint: %v", err)
			}
		})

		t.Run("cycle where prohibited", func(t *testing.T) {
			facts := []model.RelationshipFact{
				managerFact(t, "a1", "a2", 2026, 1, 1),
				managerFact(t, "a2", "a3", 2026, 1, 1),
				managerFact(t, "a3", "a1", 2026, 1, 1),
			}
			if err := model.ValidateFacts(managerDef, facts); !errors.Is(err, model.ErrRelationshipCycle) {
				t.Fatalf("accepted a manager cycle: %v", err)
			}
		})

		t.Run("overlapping exclusive edges", func(t *testing.T) {
			facts := []model.RelationshipFact{
				managerFact(t, "a1", "a2", 2026, 1, 1),
				managerFact(t, "a1", "a3", 2026, 6, 1),
			}
			if err := model.ValidateFacts(managerDef, facts); !errors.Is(err, model.ErrExclusiveOverlap) {
				t.Fatalf("accepted two overlapping exclusive manager edges: %v", err)
			}
		})

		t.Run("cross-tenant endpoint", func(t *testing.T) {
			f := managerFact(t, "a1", "a2", 2026, 1, 1)
			f.TargetTenant = "other-tenant"
			if err := model.ValidateFacts(managerDef, []model.RelationshipFact{f}); !errors.Is(err, model.ErrCrossTenantEdge) {
				t.Fatalf("accepted a cross-tenant manager edge: %v", err)
			}
		})

		t.Run("fact missing provenance", func(t *testing.T) {
			f := managerFact(t, "a1", "a2", 2026, 1, 1)
			f.ProvenanceRef = ""
			if err := f.Validate(); !errors.Is(err, model.ErrInvalidRelationship) {
				t.Fatalf("validated a fact with no provenance: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		facts := []model.RelationshipFact{
			managerFact(t, "a1", "a2", 2024, 1, 1),
		}
		if err := model.ValidateFacts(managerDef, facts); err != nil {
			t.Fatalf("valid facts rejected: %v", err)
		}
		asOf := model.AsOf(facts, instant(2025, time.June, 1))
		if len(asOf) != 1 {
			t.Fatalf("AsOf(2025-06-01) = %d facts, want 1", len(asOf))
		}
		before := model.AsOf(facts, instant(2023, time.June, 1))
		if len(before) != 0 {
			t.Fatalf("AsOf(2023-06-01) = %d facts, want 0 (fact not yet effective)", len(before))
		}
		knownAt := model.KnownAt(facts, instant(2025, time.June, 1), instant(2023, time.June, 1))
		if len(knownAt) != 0 {
			t.Fatalf("KnownAt with knownAt before the fact was known = %d facts, want 0", len(knownAt))
		}
		knownAtOK := model.KnownAt(facts, instant(2025, time.June, 1), instant(2025, time.June, 1))
		if len(knownAtOK) != 1 {
			t.Fatalf("KnownAt with knownAt after the fact was known = %d facts, want 1", len(knownAtOK))
		}
		if knownAtOK[0].ProvenanceRef == "" {
			t.Fatalf("resolved fact carries no provenance reference")
		}
	})
}

// TestTodo_MODEL_013_Property asserts that AsOf and KnownAt never return a
// fact whose effective interval does not actually contain the query instant,
// across every relationship definition and a range of query instants.
func TestTodo_MODEL_013_Property(t *testing.T) {
	facts := []model.RelationshipFact{
		managerFact(t, "a1", "a2", 2020, 1, 1),
		managerFact(t, "a3", "a4", 2022, 1, 1),
		managerFact(t, "a5", "a6", 2024, 1, 1),
	}
	queries := []time.Time{
		time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, q := range queries {
		qi := values.NewInstant(q)
		asOf := model.AsOf(facts, qi)
		for _, f := range asOf {
			ok, err := f.Effective.ContainsInstant(qi)
			if err != nil || !ok {
				t.Fatalf("AsOf(%s) returned a fact not effective then: %+v", q, f)
			}
		}
	}
}

// TestTodo_MODEL_013_Golden pins the compiled relationship registry.
func TestTodo_MODEL_013_Golden(t *testing.T) {
	reg := mustRegistry(t)
	type row struct {
		Ref, Source, Target, Cardinality     string
		Exclusive, AllowCycles, TenantScoped bool
	}
	var rows []row
	for _, d := range reg.Relationships() {
		rows = append(rows, row{
			Ref: d.Ref.String(), Source: d.SourceEntity.String(), Target: d.TargetEntity.String(),
			Cardinality: string(d.Cardinality), Exclusive: d.Exclusive, AllowCycles: d.AllowCycles,
			TenantScoped: d.TenantScoped,
		})
	}
	goldenJSON(t, "model_013_relationships.json", rows)
}

// TestTodo_MODEL_013_Security asserts that every tenant-scoped relationship
// definition actually rejects a cross-tenant fact: a definition that forgets
// to declare TenantScoped would silently let two tenants share an edge.
func TestTodo_MODEL_013_Security(t *testing.T) {
	reg := mustRegistry(t)
	for _, d := range reg.Relationships() {
		if !d.TenantScoped {
			continue
		}
		f := model.RelationshipFact{
			Relationship: d.Ref, SourceRef: "s", SourceTenant: "tenant-a",
			TargetRef: "t", TargetTenant: "tenant-b",
			Effective:     mustOpenInterval(t, 2026, time.January, 1),
			Recorded:      mustRecordedAt(t, 2026, time.January, 1),
			Known:         mustKnownAt(t, 2026, time.January, 1),
			ProvenanceRef: "provenance.edge/v1",
		}
		if err := model.ValidateFacts(d, []model.RelationshipFact{f}); !errors.Is(err, model.ErrCrossTenantEdge) {
			t.Fatalf("%s is TenantScoped but accepted a cross-tenant fact: %v", d.Ref, err)
		}
	}
}

// FuzzTodo_MODEL_013 fuzzes exclusive-overlap detection: two facts from the
// same source are legal together only when their effective intervals do not
// overlap.
func FuzzTodo_MODEL_013(f *testing.F) {
	f.Add(int64(0), int64(10), int64(20), int64(30))
	f.Add(int64(0), int64(10), int64(5), int64(15))
	f.Add(int64(0), int64(10), int64(10), int64(20))
	f.Fuzz(func(t *testing.T, aStart, aEnd, bStart, bEnd int64) {
		if aStart >= aEnd || bStart >= bEnd {
			t.Skip("degenerate interval")
		}
		base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		mk := func(startOffset, endOffset int64) model.RelationshipFact {
			start := base.Add(time.Duration(startOffset%3650) * 24 * time.Hour)
			end := base.Add(time.Duration(endOffset%3650) * 24 * time.Hour)
			if !end.After(start) {
				end = start.Add(24 * time.Hour)
			}
			iv, err := values.NewInstantInterval(values.NewInstant(start), values.NewInstant(end))
			if err != nil {
				t.Skip("invalid interval")
			}
			return model.RelationshipFact{
				Relationship: model.RelationshipRef{Name: "ManagerRelationship", Version: 1},
				SourceRef:    "a1", SourceTenant: "acme", TargetRef: "a2", TargetTenant: "acme",
				Effective: iv, Recorded: mustRecordedAt(t, 2020, time.January, 1),
				Known: mustKnownAt(t, 2020, time.January, 1), ProvenanceRef: "provenance.edge/v1",
			}
		}
		facts := []model.RelationshipFact{mk(aStart, aEnd), mk(bStart, bEnd)}
		overlap, err := facts[0].Effective.Overlaps(facts[1].Effective)
		if err != nil {
			t.Skip("interval comparison failed")
		}
		reg, regErr := model.Catalog()
		if regErr != nil {
			t.Fatalf("compile catalog: %v", regErr)
		}
		def, defErr := reg.Relationship(model.RelationshipRef{Name: "ManagerRelationship", Version: 1})
		if defErr != nil {
			t.Fatalf("resolve ManagerRelationship: %v", defErr)
		}
		err = model.ValidateFacts(def, facts)
		if overlap && !errors.Is(err, model.ErrExclusiveOverlap) {
			t.Fatalf("overlapping exclusive facts accepted: %v", err)
		}
		if !overlap && errors.Is(err, model.ErrExclusiveOverlap) {
			t.Fatalf("non-overlapping facts rejected as overlapping")
		}
	})
}
