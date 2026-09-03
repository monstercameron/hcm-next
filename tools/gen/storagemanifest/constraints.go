package storagemanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// RelationshipConstraint is one DB-004 generated fragment for a relationship
// definition: the SQL that would physically enforce its cardinality,
// exclusivity, cycle and tenant rules, plus whether the real migrated schema
// already carries a matching table.
type RelationshipConstraint struct {
	RelationshipRef string
	TableName       string
	SQL             string
	Comparable      bool
	ConformanceNote string
}

// LifecycleCheckConstraint is one DB-004 generated CHECK fragment for one
// lifecycle dimension column, compared against the real allow-list the
// migrated schema declares for that column, when one exists.
type LifecycleCheckConstraint struct {
	EntityRef       string
	Column          string
	GeneratedValues []string
	SQL             string
	ConformanceNote string
}

// ConstraintManifest is the full DB-004 output.
type ConstraintManifest struct {
	GeneratedBy     string
	RegistryDigest  string
	Relationships   []RelationshipConstraint
	LifecycleChecks []LifecycleCheckConstraint
}

// Digest returns a stable sha256 digest of the manifest's own content.
func (m ConstraintManifest) Digest() string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	w("REGISTRY", m.RegistryDigest)
	for _, r := range m.Relationships {
		w("REL", r.RelationshipRef, r.TableName, r.SQL, fmt.Sprint(r.Comparable), r.ConformanceNote)
	}
	for _, l := range m.LifecycleChecks {
		w("LIFECYCLE", l.EntityRef, l.Column, strings.Join(l.GeneratedValues, ","), l.SQL, l.ConformanceNote)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// dimensionColumn is the real, already-migrated column name each of the five
// lifecycle dimensions occupies on intent_instance
// (migrations/00004_intent_and_proposal.sql). It is hand-pinned rather than
// derived from a property's schema path, because the generic
// PropertyRef-derived column naming convention this package otherwise uses
// does not — and is not expected to — match a table that predates this
// generator.
var dimensionColumn = map[lifecycle.Dimension]string{
	lifecycle.DimensionRequest:     "request_state",
	lifecycle.DimensionExecution:   "execution_state",
	lifecycle.DimensionBusiness:    "business_state",
	lifecycle.DimensionConsistency: "consistency_state",
	lifecycle.DimensionObligation:  "obligation_state",
}

var pascalBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// snakeCase converts a PascalCase name to snake_case, e.g. "ManagerRelationship" -> "manager_relationship".
func snakeCase(name string) string {
	return strings.ToLower(pascalBoundary.ReplaceAllString(name, `${1}_${2}`))
}

// BuildConstraints generates the DB-004 manifest: one relationship
// constraint fragment per registered [model.RelationshipDefinition], and one
// lifecycle CHECK fragment per (dimension, entity) pair carried by a
// lifecycle.Dimensions-typed property, each compared against the real
// migrated schema where a comparison is possible.
func BuildConstraints(reg *model.Registry, inv MigrationInventory) (ConstraintManifest, error) {
	relOut := make([]RelationshipConstraint, 0, len(reg.Relationships()))
	for _, rel := range reg.Relationships() {
		table := "relationship_" + snakeCase(rel.Ref.Name)
		sqlText := generateRelationshipSQL(rel, table)
		comparable := inv.HasTable(table)
		note := fmt.Sprintf(
			"NO_PHYSICAL_TABLE: %q is not in the migrated schema; this fragment is a DB-004 migration proposal", table)
		if comparable {
			note = "MATCHES_MIGRATIONS: table " + table + " exists in the migrated schema"
		}
		relOut = append(relOut, RelationshipConstraint{
			RelationshipRef: rel.Ref.String(), TableName: table, SQL: sqlText,
			Comparable: comparable, ConformanceNote: note,
		})
	}

	var lifecycleOut []LifecycleCheckConstraint
	for _, p := range reg.Properties() {
		if p.GoType != "lifecycle.Dimensions" {
			continue
		}
		dims := lifecycle.AllDimensions()
		profiles := lifecycle.KernelProfiles()
		for _, dim := range dims {
			column := dimensionColumn[dim]
			generated := statesExcludingUnspecified(profiles[dim])
			sqlText := fmt.Sprintf("CHECK (%s IN (%s))", column, quotedList(generated))
			note := compareStateSets(column, generated, inv)
			lifecycleOut = append(lifecycleOut, LifecycleCheckConstraint{
				EntityRef: p.Entity.String(), Column: column, GeneratedValues: generated,
				SQL: sqlText, ConformanceNote: note,
			})
		}
	}
	sort.Slice(lifecycleOut, func(i, j int) bool {
		if lifecycleOut[i].EntityRef != lifecycleOut[j].EntityRef {
			return lifecycleOut[i].EntityRef < lifecycleOut[j].EntityRef
		}
		return lifecycleOut[i].Column < lifecycleOut[j].Column
	})

	return ConstraintManifest{
		GeneratedBy:     "tools/gen/storagemanifest (DB-004)",
		RegistryDigest:  reg.Digest(),
		Relationships:   relOut,
		LifecycleChecks: lifecycleOut,
	}, nil
}

func statesExcludingUnspecified(p lifecycle.Profile) []string {
	out := make([]string, 0, len(p.States))
	for _, s := range p.States {
		if s == "UNSPECIFIED" {
			continue
		}
		out = append(out, string(s))
	}
	sort.Strings(out)
	return out
}

func quotedList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + v + "'"
	}
	return strings.Join(quoted, ", ")
}

func compareStateSets(column string, generated []string, inv MigrationInventory) string {
	real, ok := inv.ColumnCheck(column)
	if !ok {
		return "NOT_IN_MIGRATIONS: no CHECK constraint on column " + column + " was found"
	}
	genSet := map[string]bool{}
	for _, v := range generated {
		genSet[v] = true
	}
	var missingFromReal, extraInReal []string
	for _, v := range generated {
		if !real[v] {
			missingFromReal = append(missingFromReal, v)
		}
	}
	for v := range real {
		if !genSet[v] {
			extraInReal = append(extraInReal, v)
		}
	}
	if len(missingFromReal) == 0 && len(extraInReal) == 0 {
		return "MATCHES_MIGRATIONS: allowed value set is identical"
	}
	sort.Strings(missingFromReal)
	sort.Strings(extraInReal)
	return fmt.Sprintf("MISMATCH: generated-only=%v migrations-only=%v", missingFromReal, extraInReal)
}

// generateRelationshipSQL renders the proposed physical constraint fragment
// for one relationship definition. It is a literal DDL proposal: a dedicated
// relationship-materialization table plus the exclusion/uniqueness/cycle/
// tenant constraints the definition's own flags require.
func generateRelationshipSQL(rel model.RelationshipDefinition, table string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "-- %s: %s -> %s, %s%s%s%s\n",
		rel.Ref, rel.SourceEntity, rel.TargetEntity, rel.Cardinality,
		boolNote(rel.Exclusive, ", EXCLUSIVE"), boolNote(!rel.AllowCycles, ", ACYCLIC"),
		boolNote(rel.TenantScoped, ", TENANT_SCOPED"))
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %s (\n", table)
	b.WriteString("    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),\n")
	b.WriteString("    source_ref     semantic_key NOT NULL,\n")
	b.WriteString("    target_ref     semantic_key NOT NULL,\n")
	b.WriteString("    effective_from timestamptz  NOT NULL,\n")
	b.WriteString("    effective_to   timestamptz,\n")
	b.WriteString("    recorded_at    timestamptz  NOT NULL DEFAULT now()")
	if !rel.AllowCycles && rel.SourceEntity == rel.TargetEntity {
		fmt.Fprintf(&b, ",\n    CONSTRAINT %s_no_self_cycle CHECK (source_ref <> target_ref)", table)
	}
	b.WriteString("\n);\n")
	if rel.Exclusive {
		fmt.Fprintf(&b,
			"ALTER TABLE %s ADD CONSTRAINT %s_exclusive\n"+
				"    EXCLUDE USING gist (tenant_id WITH =, source_ref WITH =,\n"+
				"        tstzrange(effective_from, effective_to) WITH &&);\n", table, table)
	}
	if rel.Cardinality == model.CardinalityOneToOne && !rel.Exclusive {
		fmt.Fprintf(&b, "CREATE UNIQUE INDEX %s_one_to_one ON %s (tenant_id, source_ref, target_ref);\n", table, table)
	}
	return b.String()
}

func boolNote(cond bool, note string) string {
	if cond {
		return note
	}
	return ""
}
