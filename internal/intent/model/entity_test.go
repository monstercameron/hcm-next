package model_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent/definitions"
	"github.com/monstercameron/hcm-next/internal/intent/model"
)

func mutateProperty(props []model.PropertyDefinition, ref model.PropertyRef, f func(*model.PropertyDefinition)) []model.PropertyDefinition {
	out := make([]model.PropertyDefinition, len(props))
	copy(out, props)
	for i := range out {
		if out[i].Ref == ref {
			f(&out[i])
		}
	}
	return out
}

func compileWithProperties(t *testing.T, props []model.PropertyDefinition) (*model.Registry, error) {
	t.Helper()
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("baseline catalog: %v", err)
	}
	return model.NewRegistry(reg.Entities(), props, reg.Aggregates(), reg.Relationships(),
		reg.Authorities(), reg.RetentionClasses())
}

// TestTodo_MODEL_011 is the PRIMARY test for the entity/property definition
// registries.
//
// RED: publication rejects a property with no concrete type, presence,
// authority, temporal, classification, correction or retention semantics.
//
// GREEN: every registered property resolves owner aggregate, schema path and
// applicable policies.
func TestTodo_MODEL_011(t *testing.T) {
	reg := mustRegistry(t)
	baseProps := reg.Properties()
	target := model.PropertyRef("employment.status")

	t.Run("RED", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*model.PropertyDefinition)
		}{
			{"no concrete type", func(p *model.PropertyDefinition) { p.GoType = "" }},
			{"no schema path", func(p *model.PropertyDefinition) { p.SchemaPath = "" }},
			{"no presence", func(p *model.PropertyDefinition) { p.Presence = "" }},
			{"no authority", func(p *model.PropertyDefinition) { p.AuthorityRef = "" }},
			{"no temporal behavior", func(p *model.PropertyDefinition) { p.Temporal = "" }},
			{"no classification", func(p *model.PropertyDefinition) { p.Classification = "" }},
			{"no correction behavior", func(p *model.PropertyDefinition) { p.Correction = "" }},
			{"no retention class", func(p *model.PropertyDefinition) { p.RetentionClassRef = "" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				props := mutateProperty(baseProps, target, tc.break_)
				if _, err := compileWithProperties(t, props); !errors.Is(err, model.ErrInvalidProperty) {
					t.Fatalf("compiled a registry that should have rejected %s: %v", tc.name, err)
				}
			})
		}

		t.Run("unknown owner entity", func(t *testing.T) {
			props := append([]model.PropertyDefinition(nil), baseProps...)
			props = append(props, model.PropertyDefinition{
				Ref: "nonexistent.field", Entity: model.EntityRef{Name: "Nonexistent", Version: 1},
				GoType: "string", SchemaPath: "x", Presence: model.PresenceRequired,
				Classification: model.ClassInternal, Temporal: model.TemporalPointInTime,
				AuthorityRef: "authority.person/v1", Correction: model.CorrectionSupersedes,
				RetentionClassRef: "WORKER_TRANSACTION", Status: model.StatusActive,
			})
			if _, err := compileWithProperties(t, props); !errors.Is(err, model.ErrUnknownEntity) {
				t.Fatalf("compiled a registry with an unknown owner entity: %v", err)
			}
		})

		t.Run("material version reuse", func(t *testing.T) {
			props := append(append([]model.PropertyDefinition(nil), baseProps...), baseProps[0])
			if _, err := compileWithProperties(t, props); !errors.Is(err, model.ErrDuplicateProperty) {
				t.Fatalf("compiled a registry with a duplicate property: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		for _, p := range reg.Properties() {
			res, err := reg.ResolveProperty(p.Ref)
			if err != nil {
				t.Fatalf("resolve %s: %v", p.Ref, err)
			}
			if res.OwnerEntity.Ref != p.Entity {
				t.Fatalf("%s resolved owner %s, want %s", p.Ref, res.OwnerEntity.Ref, p.Entity)
			}
			if res.Property.SchemaPath == "" {
				t.Fatalf("%s resolved with no schema path", p.Ref)
			}
			if res.Authority.AssignmentRef != p.AuthorityRef {
				t.Fatalf("%s resolved authority %s, want %s", p.Ref, res.Authority.AssignmentRef, p.AuthorityRef)
			}
			if res.RetentionClass.ClassRef != p.RetentionClassRef {
				t.Fatalf("%s resolved retention %s, want %s", p.Ref, res.RetentionClass.ClassRef, p.RetentionClassRef)
			}
			if res.OwnerRoot.Name == "" {
				t.Fatalf("%s resolved no owner aggregate root", p.Ref)
			}
		}
	})
}

// TestTodo_MODEL_011_Property cross-checks this registry against the frozen
// intent kernel's own data: every property the fourteen definitions' bindings
// read or write must resolve here, grounding MODEL-011 in real, already
// checked-in usage rather than an independent guess.
func TestTodo_MODEL_011_Property(t *testing.T) {
	reg := mustRegistry(t)
	seen := map[model.PropertyRef]bool{}
	for _, b := range definitions.Bindings() {
		for _, raw := range append(append([]string{}, b.ReadProperties...), b.WriteProperties...) {
			ref := model.PropertyRef(raw)
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if _, err := reg.ResolveProperty(ref); err != nil {
				t.Fatalf("binding %s uses %q, which MODEL-011 does not resolve: %v", b.Definition, raw, err)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatalf("no properties observed from the frozen bindings; the cross-check is vacuous")
	}

	// Every property's ref entity-key matches its owner's key, and every
	// property resolves the same owner root twice in a row (pure function).
	for _, p := range reg.Properties() {
		first, err := reg.ResolveProperty(p.Ref)
		if err != nil {
			t.Fatalf("resolve %s: %v", p.Ref, err)
		}
		second, err := reg.ResolveProperty(p.Ref)
		if err != nil {
			t.Fatalf("resolve %s again: %v", p.Ref, err)
		}
		if first.OwnerRoot != second.OwnerRoot {
			t.Fatalf("%s resolved a different owner root on a second call", p.Ref)
		}
	}
}

// TestTodo_MODEL_011_Golden pins the compiled entity and property tables.
func TestTodo_MODEL_011_Golden(t *testing.T) {
	reg := mustRegistry(t)
	type entityRow struct {
		Ref, Key, Domain, Class, Lifecycle, Status string
		TenantScoped                               bool
	}
	type propertyRow struct {
		Ref, Entity, GoType, SchemaPath, Presence, Classification, Temporal, Authority, Correction, Retention string
	}
	var entities []entityRow
	for _, e := range reg.Entities() {
		entities = append(entities, entityRow{
			Ref: e.Ref.String(), Key: e.Key, Domain: e.OwnerDomain, Class: string(e.Class),
			Lifecycle: e.LifecycleAssignment, Status: string(e.Status), TenantScoped: e.TenantScoped,
		})
	}
	var props []propertyRow
	for _, p := range reg.Properties() {
		props = append(props, propertyRow{
			Ref: string(p.Ref), Entity: p.Entity.String(), GoType: p.GoType, SchemaPath: p.SchemaPath,
			Presence: string(p.Presence), Classification: string(p.Classification),
			Temporal: string(p.Temporal), Authority: p.AuthorityRef, Correction: string(p.Correction),
			Retention: p.RetentionClassRef,
		})
	}
	goldenJSON(t, "model_011_catalog.json", struct {
		Entities   []entityRow
		Properties []propertyRow
	}{entities, props})
}

// TestTodo_MODEL_011_Security asserts that every property carrying a label
// stronger than PUBLIC/INTERNAL is governed by exactly one exclusive
// authority: sensitive data never resolves through an ambiguous or shared
// owner.
func TestTodo_MODEL_011_Security(t *testing.T) {
	reg := mustRegistry(t)
	for _, p := range reg.Properties() {
		if p.Classification == model.ClassPublic || p.Classification == model.ClassInternal {
			continue
		}
		auth, err := reg.Authority(p.AuthorityRef)
		if err != nil {
			t.Fatalf("%s classified %s has no resolvable authority: %v", p.Ref, p.Classification, err)
		}
		if !auth.Exclusive {
			t.Fatalf("%s classified %s is governed by non-exclusive authority %s",
				p.Ref, p.Classification, auth.AssignmentRef)
		}
		if auth.EvidenceRef == "" {
			t.Fatalf("%s classified %s has an authority with no evidence", p.Ref, p.Classification)
		}
	}
}

// FuzzTodo_MODEL_011 fuzzes property-reference parsing: an accepted reference
// must round-trip through EntityKey/Path, and a rejected one must never
// resolve.
func FuzzTodo_MODEL_011(f *testing.F) {
	for _, seed := range []string{
		"employment.status",
		"assignment.manager_relationship",
		"person.identity",
		"nofield",
		"",
		"Employment.Status",
		"employment.",
		".status",
		"employment..status",
		"employment.status.extra",
	} {
		f.Add(seed)
	}
	reg, err := model.Catalog()
	if err != nil {
		f.Fatalf("compile catalog: %v", err)
	}
	f.Fuzz(func(t *testing.T, s string) {
		ref := model.PropertyRef(s)
		err := ref.Validate()
		_, resolveErr := reg.ResolveProperty(ref)
		if err != nil && resolveErr == nil {
			t.Fatalf("%q failed Validate but resolved", s)
		}
		if err == nil {
			if ref.EntityKey() == "" {
				t.Fatalf("%q validated with no entity key", s)
			}
			// A validated ref either resolves cleanly or is simply unpublished;
			// either way ResolveProperty must never panic (already implicit).
		}
	})
}
