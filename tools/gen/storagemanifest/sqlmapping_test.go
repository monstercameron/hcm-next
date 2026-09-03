package storagemanifest_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/intent/model"
	"github.com/monstercameron/hcm-next/tools/gen/storagemanifest"
)

// TestTodo_DB_003 is the PRIMARY test for property-level SQL mappings
// generated from the model registry.
//
// RED: a property lacking SQL type, presence/null/default behavior,
// precision/unit/currency, temporal columns, classification, encryption/
// search policy or constraint mapping fails generation.
//
// GREEN: every persisted property resolves to a typed column/JSON boundary
// plus constraint/index/encryption policy; unsupported mappings fail with
// model file/path/property diagnostics.
func TestTodo_DB_003(t *testing.T) {
	reg := mustModelRegistry(t)

	t.Run("RED", func(t *testing.T) {
		t.Run("unsupported Go type fails with diagnostics", func(t *testing.T) {
			props := []model.PropertyDefinition{}
			for _, p := range reg.Properties() {
				if p.Ref == "employment.status" {
					p.GoType = "map[string]interface{}"
				}
				props = append(props, p)
			}
			broken, err := model.NewRegistry(reg.Entities(), props, reg.Aggregates(), reg.Relationships(),
				reg.Authorities(), reg.RetentionClasses())
			if err != nil {
				t.Fatalf("compile with an unsupported type: %v", err)
			}
			_, err = storagemanifest.BuildPropertyMappings(broken)
			if err == nil {
				t.Fatalf("generated a mapping for an unsupported Go type")
			}
			for _, want := range []string{"employment.status", "Employment/v1", "hcmnext.people.v1.Employment.status"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not name %q", err, want)
				}
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		manifest, err := storagemanifest.BuildPropertyMappings(reg)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if len(manifest.Properties) != len(reg.Properties()) {
			t.Fatalf("manifest has %d properties, registry has %d", len(manifest.Properties), len(reg.Properties()))
		}
		for _, p := range manifest.Properties {
			if len(p.Columns) == 0 {
				t.Fatalf("%s resolved no columns", p.PropertyRef)
			}
			for _, c := range p.Columns {
				if c.Name == "" || c.SQLType == "" {
					t.Fatalf("%s has an incomplete column mapping: %+v", p.PropertyRef, c)
				}
			}
			if p.DefaultBehavior == "" || p.DefaultBehavior == "UNKNOWN" {
				t.Fatalf("%s resolved no default behavior", p.PropertyRef)
			}
			if p.EncryptionPolicy == "" {
				t.Fatalf("%s resolved no encryption policy", p.PropertyRef)
			}
			if p.SearchPolicy == "" {
				t.Fatalf("%s resolved no search policy", p.PropertyRef)
			}
			if p.ConstraintNote == "" || p.ConstraintNote == "UNSPECIFIED" {
				t.Fatalf("%s resolved no constraint mapping", p.PropertyRef)
			}
		}
	})
}

// TestTodo_DB_003_Property asserts that every property's column set round
// trips its declared presence rule into NotNull, and that a compound Go type
// (EffectiveInterval, Dimensions) always produces more than one column while
// every primitive type produces exactly one.
func TestTodo_DB_003_Property(t *testing.T) {
	reg := mustModelRegistry(t)
	manifest, err := storagemanifest.BuildPropertyMappings(reg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	byRef := map[string]model.PropertyDefinition{}
	for _, p := range reg.Properties() {
		byRef[string(p.Ref)] = p
	}
	for _, m := range manifest.Properties {
		p := byRef[m.PropertyRef]
		if m.NotNull != (p.Presence == model.PresenceRequired) {
			t.Fatalf("%s NotNull=%v, presence=%s", m.PropertyRef, m.NotNull, p.Presence)
		}
		switch p.GoType {
		case "values.EffectiveInterval":
			if len(m.Columns) != 2 {
				t.Fatalf("%s (EffectiveInterval) has %d columns, want 2", m.PropertyRef, len(m.Columns))
			}
		case "lifecycle.Dimensions":
			if len(m.Columns) != 5 {
				t.Fatalf("%s (Dimensions) has %d columns, want 5", m.PropertyRef, len(m.Columns))
			}
		default:
			if len(m.Columns) != 1 {
				t.Fatalf("%s (%s) has %d columns, want 1", m.PropertyRef, p.GoType, len(m.Columns))
			}
		}
	}
}

// TestTodo_DB_003_Race builds property mappings concurrently.
func TestTodo_DB_003_Race(t *testing.T) {
	reg := mustModelRegistry(t)
	want, err := storagemanifest.BuildPropertyMappings(reg)
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
			m, err := storagemanifest.BuildPropertyMappings(reg)
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

// TestTodo_DB_003_Integration cross-checks generated column names for
// TABLE-backed entities against a real, freshly migrated PostgreSQL database:
// proposal_revision.material_digest must generate a column that actually
// exists on the live proposal_revision table.
func TestTodo_DB_003_Integration(t *testing.T) {
	db := pgtest.New(t)
	reg := mustModelRegistry(t)
	manifest, err := storagemanifest.BuildPropertyMappings(reg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	liveColumns := map[string]bool{}
	rows, err := db.SQL.Query(
		`SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'proposal_revision'`)
	if err != nil {
		t.Fatalf("query live columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		liveColumns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}

	for _, p := range manifest.Properties {
		if p.Entity != "ProposalRevision/v1" {
			continue
		}
		for _, c := range p.Columns {
			if !liveColumns[c.Name] {
				t.Fatalf("%s generated column %q, which does not exist on the live proposal_revision table (columns: %v)",
					p.PropertyRef, c.Name, liveColumns)
			}
		}
	}
}

// TestTodo_DB_003_Fault proves a failed generation never returns a partial
// manifest: BuildPropertyMappings returns a zero-value manifest alongside its
// error, never a manifest with a truncated Properties slice a caller might
// mistake for complete.
func TestTodo_DB_003_Fault(t *testing.T) {
	reg := mustModelRegistry(t)
	props := []model.PropertyDefinition{}
	for _, p := range reg.Properties() {
		if p.Ref == "employment.status" {
			p.GoType = "totally-bogus-type"
		}
		props = append(props, p)
	}
	broken, err := model.NewRegistry(reg.Entities(), props, reg.Aggregates(), reg.Relationships(),
		reg.Authorities(), reg.RetentionClasses())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	manifest, err := storagemanifest.BuildPropertyMappings(broken)
	if err == nil {
		t.Fatalf("expected an error for a bogus Go type")
	}
	if len(manifest.Properties) != 0 {
		t.Fatalf("a failed build returned %d partial properties", len(manifest.Properties))
	}
}

// TestTodo_DB_003_Security asserts that every property whose classification
// requires encryption never resolves to a plaintext, freely searchable
// column: a generation bug here would be a real data-exposure regression, not
// merely a cosmetic one.
func TestTodo_DB_003_Security(t *testing.T) {
	reg := mustModelRegistry(t)
	manifest, err := storagemanifest.BuildPropertyMappings(reg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	byRef := map[string]model.PropertyDefinition{}
	for _, p := range reg.Properties() {
		byRef[string(p.Ref)] = p
	}
	for _, m := range manifest.Properties {
		p := byRef[m.PropertyRef]
		sensitive := p.Classification != model.ClassPublic && p.Classification != model.ClassInternal
		if sensitive {
			if m.EncryptionPolicy != "ENCRYPTED_AT_REST" {
				t.Fatalf("%s classified %s resolved encryption policy %q", m.PropertyRef, p.Classification, m.EncryptionPolicy)
			}
			if m.SearchPolicy != "NONE" {
				t.Fatalf("%s classified %s resolved search policy %q, want NONE", m.PropertyRef, p.Classification, m.SearchPolicy)
			}
		}
	}
}
