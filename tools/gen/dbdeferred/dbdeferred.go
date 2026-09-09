// Package dbdeferred generates review-only SQL for deferred SchemaFlux
// domains. It deliberately has no filesystem writer and never executes SQL.
package dbdeferred

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources"
)

// Table is the inert schema proposal for one deferred entity.
type Table struct {
	EntityRef string
	Domain    string
	Name      string
	SQL       string
}

// Plan is deterministic review data. SQL is never applied by this package.
type Plan struct {
	Family string
	Tables []Table
	SQL    string
	// PlanDigest is retained as data for manifest serializers; Digest returns
	// the same value and is the preferred API.
	PlanDigest string
}

// Digest returns a stable sha256 digest over the exact SQL preview.
func (p Plan) Digest() string {
	h := sha256.Sum256([]byte(p.SQL))
	return "sha256:" + hex.EncodeToString(h[:])
}

// Generate validates a deferred source bundle and returns a review-only plan.
// Deferred sources must remain DRAFT, uncovered read models with no business
// lifecycle and an observation boundary; these checks prevent accidental
// publication when a source is copied into a migration pipeline.
func Generate(bundle sources.Bundle) (Plan, error) {
	manifest, errs := sources.Compile(bundle)
	if len(errs) != 0 {
		return Plan{}, fmt.Errorf("dbdeferred: source validation failed: %v", errs)
	}
	tables := make([]Table, 0, len(manifest.Entities))
	for _, e := range manifest.Entities {
		if e.Family != "deferred" || e.Status != "DRAFT" || e.Covered || e.Class != "READ_MODEL" ||
			e.LifecycleAssignment != "NO_BUSINESS_LIFECYCLE" || e.CommandBoundary != "EXTERNAL_OBSERVATION" {
			return Plan{}, fmt.Errorf("dbdeferred: %s is not an inert deferred read model", e.Ref())
		}
		for _, p := range e.Properties {
			if p.Status != "DRAFT" {
				return Plan{}, fmt.Errorf("dbdeferred: %s.%s is not DRAFT", e.Ref(), p.Name)
			}
		}
		tables = append(tables, Table{EntityRef: e.Ref(), Domain: e.OwnerDomain, Name: e.Key, SQL: tableSQL(e)})
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	var b strings.Builder
	b.WriteString("-- SchemaFlux DB-016 deferred preview; review-only, not a migration.\n")
	for _, t := range tables {
		b.WriteString(t.SQL)
		b.WriteByte('\n')
	}
	sql := b.String()
	h := sha256.Sum256([]byte(sql))
	digest := "sha256:" + hex.EncodeToString(h[:])
	return Plan{Family: "deferred", Tables: tables, SQL: sql, PlanDigest: digest}, nil
}

func tableSQL(e sources.EntitySource) string {
	// Every proposal is explicitly read-model shaped. No INSERT/UPDATE/DELETE,
	// role, grant, trigger, or command surface is emitted.
	return fmt.Sprintf("CREATE TABLE %s (tenant_id uuid NOT NULL, source_key text NOT NULL, known_at timestamptz NOT NULL, recorded_at timestamptz NOT NULL, CONSTRAINT %s_deferred_preview UNIQUE (tenant_id, source_key, known_at)); -- DRAFT CONFORMANCE EXTERNAL_OBSERVATION", e.Key, e.Key)
}

// LoadAndGenerate loads the committed metamodel, registries, and deferred
// source directory beneath root, then generates the same inert plan.
func LoadAndGenerate(root string) (Plan, error) {
	mm, err := sources.LoadMetamodel(filepath.Join(root, "schema", "schemaflux", "metamodel", "v1", "metamodel.yaml"))
	if err != nil {
		return Plan{}, err
	}
	auth, ret, err := sources.LoadRegistries(filepath.Join(root, "schema", "schemaflux", "registries", "v1", "registries.yaml"))
	if err != nil {
		return Plan{}, err
	}
	ents, rels, err := sources.LoadEntityFamilies(filepath.Join(root, "schema", "schemaflux", "deferred", "v1"))
	if err != nil {
		return Plan{}, err
	}
	return Generate(sources.Bundle{Metamodel: mm, Authorities: auth, Retentions: ret, Entities: ents, Relationships: rels})
}

// Check rejects plans that could be mistaken for an authoritative migration.
func Check(p Plan) error {
	if p.Family != "deferred" || len(p.Tables) == 0 || p.PlanDigest == "" {
		return fmt.Errorf("dbdeferred: incomplete plan")
	}
	if !strings.HasPrefix(p.SQL, "-- SchemaFlux DB-016 deferred preview") {
		return fmt.Errorf("dbdeferred: missing review-only marker")
	}
	upper := strings.ToUpper(p.SQL)
	for _, forbidden := range []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "ALTER ", "GRANT ", "REVOKE ", "CREATE ROLE", "TRIGGER"} {
		if strings.Contains(upper, forbidden) {
			return fmt.Errorf("dbdeferred: forbidden SQL token %q", forbidden)
		}
	}
	if p.PlanDigest != p.Digest() {
		return fmt.Errorf("dbdeferred: digest mismatch")
	}
	for i, t := range p.Tables {
		if t.Name == "" || t.EntityRef == "" || t.SQL == "" {
			return fmt.Errorf("dbdeferred: table %d incomplete", i)
		}
	}
	return nil
}
