// Package seed loads the Promotion fixture corpus (DB-019) and inserts it
// into the physical definition registry (migrations/00003_definition_registry.sql)
// idempotently, with a content digest that pins the exact plan.
//
// # Why the definition registry, not new tables
//
// DB-019 depends on domain physical tables (person, employment, position,
// compensation) this phase has not built yet; the only physical substrate
// available to seed into without adding a table -- which would break
// internal/data/schema's frozen TestTodo_DATA_001_Golden table inventory, a
// file outside this package's lane -- is the definition_version/
// definition_active_pointer registry migration 00003 already declares for
// exactly this purpose (platform-plane-model.md lists "reference data and
// mappings" under the Control Plane's definition resolution). Every
// registration below is therefore a definition_version row: an ENTITY for a
// person or a position, a RELATIONSHIP for an employment, a CONFIG for a
// compensation band or the band catalog as a whole, and one SCHEMA,
// CAPABILITY and INTENT row for the corpus's own registration. The kind and a
// namespaced definition_key (e.g. "person:jane-doe" vs "position:jane-doe")
// keep the eight categories DB-019 names distinguishable within
// definition_version's nine-value kind enum.
//
// # Why this is reproducible
//
// Plan reads only the embedded JSON: no wall clock, no random order, no
// database round trip. Two calls to Plan in the same build always return the
// same registrations in the same order, so ArtifactDigest never drifts, and
// Seed's ON CONFLICT DO NOTHING (keyed on definition_version's own primary
// key) makes inserting the plan a second time a no-op.
//
// This package does not import internal/domains/fixtures: its testdata is an
// independent copy of that package's workers.json and bands.json, kept
// verbatim so the fixture identities (worker keys, band ids) line up, but
// loaded and interpreted on its own.
package seed

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

//go:embed testdata/workers.json
var workersJSON []byte

//go:embed testdata/bands.json
var bandsJSON []byte

// Definition kinds this package uses, all already declared by migration
// 00003's definition_kind check constraint.
const (
	kindSchema       = "SCHEMA"
	kindCapability   = "CAPABILITY"
	kindIntent       = "INTENT"
	kindConfig       = "CONFIG"
	kindEntity       = "ENTITY"
	kindRelationship = "RELATIONSHIP"
)

// seedPublishedAt is the fixed business timestamp recorded against the four
// corpus-level registrations (schema, capability, intent, band catalog). It
// is a fixture constant, never time.Now(): the seed's reproducibility depends
// on every registration's content, including this field, being independent
// of when Seed happens to run.
const seedPublishedAt = "2026-01-01T00:00:00Z"

// PublishedBy is the recorded publisher of every registration this package
// writes.
const PublishedBy = "hcmnext.seed"

// workerFile is the on-disk shape of the copied worker corpus. Only the
// fields the four fixture categories (person/employment/position/reference)
// need are kept; internal/domains/fixtures/testdata/workers.json carries a
// few additional fields (revision/authority bookkeeping) this package does
// not seed.
type workerFile struct {
	Calendar struct {
		Ref     string `json:"ref"`
		Version string `json:"version"`
	} `json:"calendar"`
	Workers []workerRecord `json:"workers"`
}

type workerRecord struct {
	Key              string `json:"key"`
	ID               string `json:"id"`
	WorkerNumber     string `json:"worker_number"`
	LifecycleStatus  string `json:"lifecycle_status"`
	LegalName        string `json:"legal_name"`
	PreferredName    string `json:"preferred_name"`
	EmploymentID     string `json:"employment_id"`
	LegalEntity      string `json:"legal_entity"`
	WorkerType       string `json:"worker_type"`
	HireDate         string `json:"hire_date"`
	EmploymentStatus string `json:"employment_status"`
	AssignmentID     string `json:"assignment_id"`
	JobCode          string `json:"job_code"`
	Grade            string `json:"grade"`
	OrgUnit          string `json:"org_unit"`
	PositionID       string `json:"position_id"`
	Location         string `json:"location"`
	PayZone          string `json:"pay_zone"`
	RecordedAt       string `json:"recorded_at"`
}

// bandFile is the on-disk shape of the copied pay-band catalog.
type bandFile struct {
	CatalogVersion string       `json:"catalog_version"`
	SourceSystem   string       `json:"source_system"`
	RecordedAt     string       `json:"recorded_at"`
	Bands          []bandRecord `json:"bands"`
}

type bandRecord struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	JobCode  string `json:"job_code"`
	Grade    string `json:"grade"`
	PayZone  string `json:"pay_zone"`
	Currency string `json:"currency"`
	Minimum  string `json:"minimum"`
	Midpoint string `json:"midpoint"`
	Maximum  string `json:"maximum"`
}

// Registration is one canonical-registry or Promotion-fixture row to seed, as
// a definition_version insert.
type Registration struct {
	Kind        string
	Key         string
	Body        []byte
	SourceRef   string
	PublishedAt string
}

// personFields, employmentFields and positionFields project the sub-record
// of a worker each of the three worker-derived categories seeds; keeping them
// disjoint (rather than seeding the whole workerRecord three times) is what
// makes "person", "employment" and "position" independently recognisable
// registrations rather than three copies of the same blob.
type personFields struct {
	ID              string `json:"id"`
	WorkerNumber    string `json:"worker_number"`
	LegalName       string `json:"legal_name"`
	PreferredName   string `json:"preferred_name"`
	LifecycleStatus string `json:"lifecycle_status"`
}

type employmentFields struct {
	EmploymentID     string `json:"employment_id"`
	LegalEntity      string `json:"legal_entity"`
	WorkerType       string `json:"worker_type"`
	HireDate         string `json:"hire_date"`
	EmploymentStatus string `json:"employment_status"`
	AssignmentID     string `json:"assignment_id"`
}

type positionFields struct {
	PositionID string `json:"position_id"`
	JobCode    string `json:"job_code"`
	Grade      string `json:"grade"`
	OrgUnit    string `json:"org_unit"`
	Location   string `json:"location"`
	PayZone    string `json:"pay_zone"`
}

// Plan returns the full, deterministic set of registrations the Promotion
// fixture needs, derived only from the embedded corpus: one SCHEMA, one
// CAPABILITY and one INTENT registration for the corpus itself, one CONFIG
// registration for the pay-band catalog as a whole, and per-worker
// ENTITY/RELATIONSHIP/ENTITY registrations (person/employment/position) plus
// one CONFIG registration per pay band (compensation). The result is sorted
// by (Kind, Key) so ArtifactDigest is stable regardless of map/JSON array
// iteration order.
func Plan() ([]Registration, error) {
	var workers workerFile
	if err := json.Unmarshal(workersJSON, &workers); err != nil {
		return nil, fmt.Errorf("seed: parse workers.json: %w", err)
	}
	var bands bandFile
	if err := json.Unmarshal(bandsJSON, &bands); err != nil {
		return nil, fmt.Errorf("seed: parse bands.json: %w", err)
	}

	var regs []Registration

	schemaBody, err := marshal(map[string]string{
		"calendar_ref":     workers.Calendar.Ref,
		"calendar_version": workers.Calendar.Version,
	})
	if err != nil {
		return nil, err
	}
	regs = append(regs, Registration{
		Kind: kindSchema, Key: "hcmnext.fixtures.corpus", Body: schemaBody,
		SourceRef: "internal/data/seed/testdata/workers.json#calendar", PublishedAt: seedPublishedAt,
	})

	capabilityBody, err := marshal(map[string]any{
		"capability":   "people.promote-into-management",
		"worker_count": len(workers.Workers),
	})
	if err != nil {
		return nil, err
	}
	regs = append(regs, Registration{
		Kind: kindCapability, Key: "people.promote-into-management", Body: capabilityBody,
		SourceRef: "planning/reference-workflows/promote-into-management.md", PublishedAt: seedPublishedAt,
	})

	intentBody, err := marshal(map[string]string{
		"intent": "PromoteIntoManagement",
	})
	if err != nil {
		return nil, err
	}
	regs = append(regs, Registration{
		Kind: kindIntent, Key: "promote-into-management", Body: intentBody,
		SourceRef: "planning/reference-workflows/promote-into-management.md", PublishedAt: seedPublishedAt,
	})

	regs = append(regs, Registration{
		Kind: kindConfig, Key: "rewards.paybands.catalog", Body: append([]byte(nil), bandsJSON...),
		SourceRef: "internal/data/seed/testdata/bands.json", PublishedAt: bands.RecordedAt,
	})

	for _, w := range workers.Workers {
		personBody, err := marshal(personFields{
			ID: w.ID, WorkerNumber: w.WorkerNumber, LegalName: w.LegalName,
			PreferredName: w.PreferredName, LifecycleStatus: w.LifecycleStatus,
		})
		if err != nil {
			return nil, err
		}
		regs = append(regs, Registration{
			Kind: kindEntity, Key: "person:" + w.Key, Body: personBody,
			SourceRef: "internal/data/seed/testdata/workers.json#" + w.Key, PublishedAt: w.RecordedAt,
		})

		employmentBody, err := marshal(employmentFields{
			EmploymentID: w.EmploymentID, LegalEntity: w.LegalEntity, WorkerType: w.WorkerType,
			HireDate: w.HireDate, EmploymentStatus: w.EmploymentStatus, AssignmentID: w.AssignmentID,
		})
		if err != nil {
			return nil, err
		}
		regs = append(regs, Registration{
			Kind: kindRelationship, Key: "employment:" + w.Key, Body: employmentBody,
			SourceRef: "internal/data/seed/testdata/workers.json#" + w.Key, PublishedAt: w.RecordedAt,
		})

		positionBody, err := marshal(positionFields{
			PositionID: w.PositionID, JobCode: w.JobCode, Grade: w.Grade,
			OrgUnit: w.OrgUnit, Location: w.Location, PayZone: w.PayZone,
		})
		if err != nil {
			return nil, err
		}
		regs = append(regs, Registration{
			Kind: kindEntity, Key: "position:" + w.Key, Body: positionBody,
			SourceRef: "internal/data/seed/testdata/workers.json#" + w.Key, PublishedAt: w.RecordedAt,
		})
	}

	for _, b := range bands.Bands {
		bandBody, err := marshal(b)
		if err != nil {
			return nil, err
		}
		regs = append(regs, Registration{
			Kind: kindConfig, Key: "band:" + b.ID, Body: bandBody,
			SourceRef: "internal/data/seed/testdata/bands.json#" + b.ID, PublishedAt: bands.RecordedAt,
		})
	}

	sort.Slice(regs, func(i, j int) bool {
		if regs[i].Kind != regs[j].Kind {
			return regs[i].Kind < regs[j].Kind
		}
		return regs[i].Key < regs[j].Key
	})
	return regs, nil
}

func marshal(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("seed: marshal registration body: %w", err)
	}
	return body, nil
}

// digestOf returns the sha256 hex digest of body, in the form migration
// 00002's content_digest domain requires.
func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ArtifactDigest returns one digest over an already-Plan-ordered registration
// set, binding each registration's kind, key and content digest the same way
// migrations.ArtifactDigest binds each migration file's version, name and
// checksum. Two calls over the same Plan() output always agree; the digest
// changes only if the embedded corpus itself changes.
func ArtifactDigest(regs []Registration) string {
	h := sha256.New()
	for _, r := range regs {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", r.Kind, r.Key, digestOf(r.Body))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Summary reports what one Seed call did.
type Summary struct {
	TenantID uuid.UUID
	// Inserted is how many registrations were newly written.
	Inserted int
	// Skipped is how many registrations already existed with matching
	// content (the idempotent no-op case).
	Skipped int
	// Digest is ArtifactDigest of the plan Seed inserted from.
	Digest string
}

// Seed scopes tx to tenantID (via internal/data/tenancy.WithTenant, so
// migration 00008's row level security governs these writes the same as any
// other tenant-scoped write) and inserts the canonical registry and Promotion
// fixture registrations for tenantID, each as a definition_version at
// version 1. Running Seed again for the same tenant is a no-op: a
// registration whose (tenant, kind, key, version) already exists is left
// alone by ON CONFLICT DO NOTHING, and Seed then verifies the stored content
// digest still matches the plan's, refusing to report success over silently
// drifted seed content.
func Seed(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (Summary, error) {
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return Summary{}, err
	}
	regs, err := Plan()
	if err != nil {
		return Summary{}, err
	}

	summary := Summary{TenantID: tenantID, Digest: ArtifactDigest(regs)}
	for _, r := range regs {
		digest := digestOf(r.Body)
		tag, err := tx.Exec(ctx, `
			INSERT INTO definition_version (
				tenant_id, definition_kind, definition_key, version, definition_digest,
				source_ref, body, published_by, published_at)
			VALUES ($1, $2, $3, 1, $4, $5, $6, $7, $8)
			ON CONFLICT (tenant_id, definition_kind, definition_key, version) DO NOTHING`,
			tenantID, r.Kind, r.Key, digest, r.SourceRef, r.Body, PublishedBy, r.PublishedAt)
		if err != nil {
			return Summary{}, fmt.Errorf("seed: insert %s %q: %w", r.Kind, r.Key, err)
		}
		if tag.RowsAffected() == 1 {
			summary.Inserted++
			continue
		}

		// Already present: verify it is still this plan's content, not a
		// drifted row some other writer or an earlier corpus version left
		// behind under the same key.
		var existing string
		err = tx.QueryRow(ctx, `
			SELECT definition_digest FROM definition_version
			WHERE tenant_id = $1 AND definition_kind = $2 AND definition_key = $3 AND version = 1`,
			tenantID, r.Kind, r.Key).Scan(&existing)
		if err != nil {
			return Summary{}, fmt.Errorf("seed: verify existing %s %q: %w", r.Kind, r.Key, err)
		}
		if existing != digest {
			return Summary{}, fmt.Errorf(
				"seed: %s %q already exists with digest %s, plan expects %s (content drift, not a re-seed)",
				r.Kind, r.Key, existing, digest)
		}
		summary.Skipped++
	}
	return summary, nil
}
