package schemafluxsql

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/intent/model"
	"github.com/monstercameron/hcm-next/tools/gen/storagemanifest"
)

// MigrationInput is a reviewable, non-authoritative migration proposal. SQL
// is intentionally returned as data so a separate migration owner can review
// and apply it; Generate never emits a file or executes SQL.
type MigrationInput struct {
	Kind      string
	Target    string
	SourceRef string
	Reason    string
	SQL       string
}

// Plan is the complete deterministic output for one registry and migration
// inventory. Disposition/Property/Constraint retain the DB-002..004 manifests
// and Inputs contain only missing physical materializations or mismatches.
type Plan struct {
	GeneratedBy    string
	RegistryDigest string
	Disposition    storagemanifest.DispositionManifest
	Properties     storagemanifest.PropertyMappingManifest
	Constraints    storagemanifest.ConstraintManifest
	Inputs         []MigrationInput
}

// Digest returns a stable digest over all plan content, including proposed SQL.
func (p Plan) Digest() string {
	h := sha256.New()
	write := func(v ...string) {
		for _, s := range v {
			_, _ = h.Write([]byte(s))
			_, _ = h.Write([]byte{0})
		}
	}
	write("REGISTRY", p.RegistryDigest, p.Disposition.Digest(), p.Properties.Digest(), p.Constraints.Digest())
	for _, in := range p.Inputs {
		write("INPUT", in.Kind, in.Target, in.SourceRef, in.Reason, in.SQL)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Generate builds a SQL review plan from the canonical registry and scanned
// migration inventory. It rejects no physical mismatch: absence is valuable
// review input and must not be silently treated as an already-applied change.
func Generate(reg *model.Registry, inv storagemanifest.MigrationInventory) (Plan, error) {
	if reg == nil {
		return Plan{}, fmt.Errorf("schemafluxsql: nil registry")
	}
	d, err := storagemanifest.BuildDispositionManifest(reg, inv)
	if err != nil {
		return Plan{}, err
	}
	p, err := storagemanifest.BuildPropertyMappings(reg)
	if err != nil {
		return Plan{}, err
	}
	c, err := storagemanifest.BuildConstraints(reg, inv)
	if err != nil {
		return Plan{}, err
	}
	inputs := make([]MigrationInput, 0)
	for _, e := range d.Entities {
		if e.Disposition != storagemanifest.DispositionMismatch {
			continue
		}
		inputs = append(inputs, MigrationInput{Kind: "DISPOSITION", Target: e.Key, SourceRef: e.EntityRef, Reason: e.MismatchDetail, SQL: dispositionSQL(e.Key)})
	}
	for _, r := range c.Relationships {
		if r.Comparable {
			continue
		}
		inputs = append(inputs, MigrationInput{Kind: "RELATIONSHIP_CONSTRAINT", Target: r.TableName, SourceRef: r.RelationshipRef, Reason: r.ConformanceNote, SQL: r.SQL})
	}
	for _, l := range c.LifecycleChecks {
		if strings.HasPrefix(l.ConformanceNote, "MATCHES_MIGRATIONS:") {
			continue
		}
		inputs = append(inputs, MigrationInput{Kind: "LIFECYCLE_CONSTRAINT", Target: l.Column, SourceRef: l.EntityRef, Reason: l.ConformanceNote, SQL: "ALTER TABLE <review-target> ADD " + l.SQL + ";"})
	}
	for _, e := range reg.Entities() {
		for _, prop := range reg.Properties() {
			if prop.Entity != e.Ref || prop.Temporal != model.TemporalEffectiveDated && prop.Temporal != model.TemporalPointInTime {
				continue
			}
			// Temporal metadata is part of the model contract and must not be
			// dropped even when a pre-existing table is reused.
			inputs = append(inputs, MigrationInput{Kind: "TEMPORAL_METADATA", Target: e.Key, SourceRef: prop.SchemaPath, Reason: string(prop.Temporal) + " requires known_at and recorded_at", SQL: fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS known_at timestamptz NOT NULL, ADD COLUMN IF NOT EXISTS recorded_at timestamptz NOT NULL;", e.Key)})
			break
		}
	}
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Kind != inputs[j].Kind {
			return inputs[i].Kind < inputs[j].Kind
		}
		if inputs[i].Target != inputs[j].Target {
			return inputs[i].Target < inputs[j].Target
		}
		return inputs[i].SourceRef < inputs[j].SourceRef
	})
	return Plan{GeneratedBy: "tools/gen/schemafluxsql (MSRC-008)", RegistryDigest: reg.Digest(), Disposition: d, Properties: p, Constraints: c, Inputs: inputs}, nil
}

func dispositionSQL(key string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (tenant_id tenant_ref NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now());", key)
}
