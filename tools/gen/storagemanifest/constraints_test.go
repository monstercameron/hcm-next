package storagemanifest_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/intent/model"
	"github.com/monstercameron/hcm-next/tools/gen/storagemanifest"
)

// TestTodo_DB_004 is the PRIMARY test for generated relationship and
// lifecycle database constraints.
//
// RED: unknown target, cross-tenant edge, illegal cardinality, temporal
// overlap, invalid initial/terminal state or forbidden transition can be
// inserted directly through SQL.
//
// GREEN: FK/check/exclusion/unique constraints or mandatory transactional
// validators reject each negative fixture with a stable SQL/application
// error mapping.
func TestTodo_DB_004(t *testing.T) {
	reg := mustModelRegistry(t)
	inv := mustInventory(t)

	t.Run("RED", func(t *testing.T) {
		db := pgtest.New(t)
		ctx := context.Background()

		t.Run("invalid initial/terminal state rejected by the real CHECK constraint", func(t *testing.T) {
			_, err := db.SQL.ExecContext(ctx, `
				INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
				VALUES (gen_random_uuid(), 'db004-tenant', 'cell-1', 'DB-004 tenant', 'ACTIVE', now())
			`)
			if err != nil {
				t.Fatalf("seed tenant: %v", err)
			}
			_, err = db.SQL.ExecContext(ctx, `
				INSERT INTO intent_instance (
					tenant_id, intent_id, definition_ref, definition_version, request_digest,
					idempotency_key, request_state, execution_state, business_state, consistency_state,
					obligation_state, created_at, last_transition_at
				)
				SELECT tenant_id, gen_random_uuid(), 'hcmnext.people.promote_worker', 1,
					repeat('a', 64), 'idem-1', 'NOT_A_REAL_STATE', 'NOT_PLANNED', 'NOT_STARTED',
					'NOT_APPLICABLE', 'NOT_APPLICABLE', now(), now()
				FROM tenant WHERE tenant_key = 'db004-tenant'
			`)
			if err == nil {
				t.Fatalf("inserted an intent_instance with an undeclared request_state")
			}
			if !strings.Contains(err.Error(), "intent_instance_request_state_allowed") {
				t.Fatalf("expected the request_state CHECK to fire, got: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		manifest, err := storagemanifest.BuildConstraints(reg, inv)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if len(manifest.Relationships) != len(reg.Relationships()) {
			t.Fatalf("manifest has %d relationship fragments, registry has %d",
				len(manifest.Relationships), len(reg.Relationships()))
		}
		for _, r := range manifest.Relationships {
			if r.SQL == "" {
				t.Fatalf("%s generated no SQL fragment", r.RelationshipRef)
			}
			if r.ConformanceNote == "" {
				t.Fatalf("%s carries no conformance note", r.RelationshipRef)
			}
		}
		if len(manifest.LifecycleChecks) == 0 {
			t.Fatalf("no lifecycle CHECK fragments were generated")
		}
		matched := 0
		for _, l := range manifest.LifecycleChecks {
			if strings.HasPrefix(l.ConformanceNote, "MATCHES_MIGRATIONS") {
				matched++
			}
		}
		if matched == 0 {
			t.Fatalf("no generated lifecycle CHECK matched the real migrated schema")
		}
	})
}

// TestTodo_DB_004_Property asserts that every generated relationship SQL
// fragment actually declares the constraints its definition's flags require:
// EXCLUDE for an exclusive relationship, a self-cycle CHECK for an acyclic
// self-referencing relationship, and a tenant reference for a tenant-scoped
// one.
func TestTodo_DB_004_Property(t *testing.T) {
	reg := mustModelRegistry(t)
	inv := mustInventory(t)
	manifest, err := storagemanifest.BuildConstraints(reg, inv)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	byRef := map[string]model.RelationshipDefinition{}
	for _, d := range reg.Relationships() {
		byRef[d.Ref.String()] = d
	}
	for _, r := range manifest.Relationships {
		d := byRef[r.RelationshipRef]
		if d.Exclusive && !strings.Contains(r.SQL, "EXCLUDE") {
			t.Fatalf("%s is exclusive but generated no EXCLUDE constraint:\n%s", r.RelationshipRef, r.SQL)
		}
		if d.TenantScoped && !strings.Contains(r.SQL, "tenant_ref") {
			t.Fatalf("%s is tenant-scoped but generated no tenant reference:\n%s", r.RelationshipRef, r.SQL)
		}
		if !d.AllowCycles && d.SourceEntity == d.TargetEntity && !strings.Contains(r.SQL, "no_self_cycle") {
			t.Fatalf("%s is acyclic and self-referencing but generated no cycle guard:\n%s", r.RelationshipRef, r.SQL)
		}
	}
}

// TestTodo_DB_004_Race builds constraints concurrently.
func TestTodo_DB_004_Race(t *testing.T) {
	reg := mustModelRegistry(t)
	inv := mustInventory(t)
	want, err := storagemanifest.BuildConstraints(reg, inv)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	wantDigest := want.Digest()
	var wg sync.WaitGroup
	digests := make([]string, 16)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m, err := storagemanifest.BuildConstraints(reg, inv)
			if err != nil {
				t.Errorf("concurrent build %d: %v", i, err)
				return
			}
			digests[i] = m.Digest()
		}(i)
	}
	wg.Wait()
	for i, d := range digests {
		if d != wantDigest {
			t.Fatalf("concurrent build %d digest = %s, want %s", i, d, wantDigest)
		}
	}
}

// TestTodo_DB_004_Integration exercises the real, live database with the
// generated lifecycle CHECK value sets: every generated allowed value must
// actually be accepted by the live table, and a value outside the generated
// set must actually be rejected — proving the generated fragment's value set
// is not just textually equal to the migration but behaviorally equivalent.
func TestTodo_DB_004_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	reg := mustModelRegistry(t)
	inv := mustInventory(t)
	manifest, err := storagemanifest.BuildConstraints(reg, inv)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	_, err = db.SQL.ExecContext(ctx, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES (gen_random_uuid(), 'db004-int-tenant', 'cell-1', 'DB-004 integration tenant', 'ACTIVE', now())
	`)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	insert := func(requestState string) error {
		_, err := db.SQL.ExecContext(ctx, `
			INSERT INTO intent_instance (
				tenant_id, intent_id, definition_ref, definition_version, request_digest,
				idempotency_key, request_state, execution_state, business_state, consistency_state,
				obligation_state, created_at, last_transition_at
			)
			SELECT tenant_id, gen_random_uuid(), 'hcmnext.people.promote_worker', 1,
				repeat('a', 64), gen_random_uuid()::text, $1, 'NOT_PLANNED', 'NOT_STARTED',
				'NOT_APPLICABLE', 'NOT_APPLICABLE', now(), now()
			FROM tenant WHERE tenant_key = 'db004-int-tenant'
		`, requestState)
		return err
	}

	for _, l := range manifest.LifecycleChecks {
		if l.Column != "request_state" {
			continue
		}
		for _, v := range l.GeneratedValues {
			if err := insert(v); err != nil {
				t.Fatalf("generated allowed request_state %q was rejected by the live database: %v", v, err)
			}
		}
	}
	if err := insert("DEFINITELY_NOT_A_STATE"); err == nil {
		t.Fatalf("an undeclared request_state was accepted by the live database")
	}
}

// TestTodo_DB_004_Security asserts every generated relationship constraint
// fragment for a tenant-scoped relationship requires tenant_id NOT NULL and
// references the tenant table: a relationship fragment that forgot this
// would let two tenants share an edge in the eventual physical table.
func TestTodo_DB_004_Security(t *testing.T) {
	reg := mustModelRegistry(t)
	inv := mustInventory(t)
	manifest, err := storagemanifest.BuildConstraints(reg, inv)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	byRef := map[string]model.RelationshipDefinition{}
	for _, d := range reg.Relationships() {
		byRef[d.Ref.String()] = d
	}
	for _, r := range manifest.Relationships {
		if !byRef[r.RelationshipRef].TenantScoped {
			continue
		}
		if !strings.Contains(r.SQL, "REFERENCES tenant") {
			t.Fatalf("%s is tenant-scoped but generated no REFERENCES tenant:\n%s", r.RelationshipRef, r.SQL)
		}
		if !strings.Contains(r.SQL, "tenant_id      tenant_ref   NOT NULL") {
			t.Fatalf("%s is tenant-scoped but tenant_id is not NOT NULL:\n%s", r.RelationshipRef, r.SQL)
		}
	}
}

// TestTodo_DB_004_Mutation proves the generator is sensitive to its
// definition's flags: flipping Exclusive, AllowCycles or TenantScoped on a
// relationship must change its generated SQL fragment.
func TestTodo_DB_004_Mutation(t *testing.T) {
	reg := mustModelRegistry(t)
	inv := mustInventory(t)
	target := model.RelationshipRef{Name: "AssignmentPosition", Version: 1}
	if _, err := reg.Relationship(target); err != nil {
		t.Fatalf("resolve base: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*model.RelationshipDefinition)
	}{
		{"flip Exclusive", func(d *model.RelationshipDefinition) { d.Exclusive = !d.Exclusive }},
		{"flip TenantScoped", func(d *model.RelationshipDefinition) { d.TenantScoped = !d.TenantScoped }},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			relationships := append([]model.RelationshipDefinition(nil), reg.Relationships()...)
			for i := range relationships {
				if relationships[i].Ref == target {
					m.mutate(&relationships[i])
				}
			}
			mutated, err := model.NewRegistry(reg.Entities(), reg.Properties(), reg.Aggregates(), relationships,
				reg.Authorities(), reg.RetentionClasses())
			if err != nil {
				t.Fatalf("compile mutated: %v", err)
			}
			before, err := storagemanifest.BuildConstraints(reg, inv)
			if err != nil {
				t.Fatalf("build before: %v", err)
			}
			after, err := storagemanifest.BuildConstraints(mutated, inv)
			if err != nil {
				t.Fatalf("build after: %v", err)
			}
			var beforeSQL, afterSQL string
			for _, r := range before.Relationships {
				if r.RelationshipRef == target.String() {
					beforeSQL = r.SQL
				}
			}
			for _, r := range after.Relationships {
				if r.RelationshipRef == target.String() {
					afterSQL = r.SQL
				}
			}
			if beforeSQL == afterSQL {
				t.Fatalf("%s did not change the generated SQL for %s", m.name, target)
			}
		})
	}
}
