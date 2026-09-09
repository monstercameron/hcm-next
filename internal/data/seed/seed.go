// Package seed loads the Promotion fixture corpus (DB-019) into a tenant
// idempotently, with a content digest that pins the exact plan.
//
// # Two substrates, split by what each fixture category actually is
//
// The corpus's own registration (one SCHEMA row naming the fixture
// calendar), the capability/intent it exercises, and the pay-band catalog's
// own reference-data descriptor (CONFIG "rewards.paybands.catalog", the raw
// bands.json blob) are metadata *about* the corpus, not domain facts a
// caller would ever want to revise or supersede -- migration 00003's
// definition_version/definition_active_pointer registry (platform-plane-
// model.md's "reference data and mappings" under the Control Plane) is
// exactly the substrate for that, and stays that way.
//
// Every person/worker/employment/assignment/legal_entity/organization_unit/
// job/job_position/position_occupancy/compensation_* fixture, by contrast,
// now has a physical bitemporal home: migrations 00011-00013 (DB-008/009/
// 010), the tables internal/data/aggregates owns. Before those migrations
// existed this package published the same facts as namespaced
// definition_version rows instead (an ENTITY for a person or a position, a
// RELATIONSHIP for an employment, a CONFIG per pay band); DB-019's follow-up
// asked this package to re-point at the real tables now that they exist, so
// Seed calls aggregates.LoadFixtures -- the frozen package's own fixture
// loader, reusing the exact same embedded corpus rather than a hand-built
// second copy -- inside the caller's transaction instead.
//
// # Why this is still idempotent
//
// aggregates.LoadFixtures assigns every entity a fresh, random uuid on each
// call (aggregates is frozen: this package may only import it, not add a
// natural-key idempotency path to it), so calling it twice for the same
// tenant would leave two disjoint sets of otherwise-identical person/
// worker/... rows rather than a no-op. Seed instead gates the one
// LoadFixtures call behind an ordinary definition_version row: a SCHEMA
// registration (key aggregateCorpusKey) whose body is a digest of the
// embedded workers.json/bands.json bytes. That row goes through the exact
// same INSERT ... ON CONFLICT DO NOTHING plus digest-verify-on-conflict path
// as every other registration below, so it inherits the same reproducibility
// and concurrency-safety proofs (golden_test.go, race_test.go) without new
// machinery: LoadFixtures runs exactly when that marker is the row Seed
// itself just inserted, and never runs again for a tenant that already
// carries it (or reports content drift, exactly like any other registration
// here, if the corpus changed underneath an existing marker).
//
// # Why this is reproducible
//
// Plan reads only the embedded JSON: no wall clock, no random order, no
// database round trip. Two calls to Plan in the same build always return the
// same registrations in the same order, so ArtifactDigest never drifts for
// the definition_version half of the plan. (aggregates.LoadFixtures' own
// random entity ids are the one part of Seed's total effect Plan's digest
// does not -- and structurally cannot -- pin; the aggregateCorpusKey digest
// instead pins the corpus content that produced them.)
//
// This package does not import internal/domains/fixtures: its testdata is an
// independent copy of that package's workers.json and bands.json, kept
// verbatim -- byte-identical to internal/data/aggregates' own copy -- so the
// fixture identities (worker keys, band ids) line up across both packages
// without either importing the other.
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

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

//go:embed testdata/workers.json
var workersJSON []byte

//go:embed testdata/bands.json
var bandsJSON []byte

// Definition kinds this package uses, all already declared by migration
// 00003's definition_kind check constraint.
const (
	kindSchema     = "SCHEMA"
	kindCapability = "CAPABILITY"
	kindIntent     = "INTENT"
	kindConfig     = "CONFIG"
)

// aggregateCorpusKey is the definition_version marker Seed uses to tell
// whether this tenant's person/worker/employment/... aggregate rows already
// reflect the current embedded corpus. See the package doc's "Why this is
// still idempotent" section for why a marker row, rather than a natural-key
// check inside aggregates itself, is what makes the single
// aggregates.LoadFixtures call safe to gate.
const aggregateCorpusKey = "hcmnext.fixtures.aggregates.corpus"

// seedPublishedAt is the fixed business timestamp recorded against the
// corpus-level registrations (schema, capability, intent, aggregate-corpus
// marker). It is a fixture constant, never time.Now(): the seed's
// reproducibility depends on every registration's content, including this
// field, being independent of when Seed happens to run.
const seedPublishedAt = "2026-01-01T00:00:00Z"

// PublishedBy is the recorded publisher of every registration this package
// writes.
const PublishedBy = "hcmnext.seed"

// workerFile is the on-disk shape of the copied worker corpus that Plan
// itself still reads directly: the fixture calendar the SCHEMA registration
// names, and a worker count for the CAPABILITY registration's body. Every
// other field (legal name, job code, assignment, ...) now lands in the
// aggregate tables via aggregates.LoadFixtures, which parses this same
// corpus independently through its own richer types.
type workerFile struct {
	Calendar struct {
		Ref     string `json:"ref"`
		Version string `json:"version"`
	} `json:"calendar"`
	Workers []struct {
		Key string `json:"key"`
	} `json:"workers"`
}

// bandFile is the on-disk shape of the copied pay-band catalog that Plan
// itself still reads directly, for the catalog-level CONFIG registration's
// published_at. The individual bands (also parsed here by
// aggregates.LoadFixtures) now land in the compensation_band table instead
// of a per-band definition_version row.
type bandFile struct {
	CatalogVersion string `json:"catalog_version"`
	SourceSystem   string `json:"source_system"`
	RecordedAt     string `json:"recorded_at"`
}

// Registration is one canonical-registry row to seed, as a definition_version
// insert.
type Registration struct {
	Kind        string
	Key         string
	Body        []byte
	SourceRef   string
	PublishedAt string
}

// Plan returns the full, deterministic set of definition_version
// registrations the Promotion fixture needs, derived only from the embedded
// corpus: one SCHEMA and one CAPABILITY and one INTENT registration for the
// corpus itself, one CONFIG registration for the pay-band catalog as a
// whole, and one further SCHEMA registration (aggregateCorpusKey) marking
// the corpus content Seed loads through aggregates.LoadFixtures. The result
// is sorted by (Kind, Key) so ArtifactDigest is stable regardless of map/JSON
// array iteration order.
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

	aggregateCorpusBody, err := marshal(map[string]string{
		"workers_digest": digestOf(workersJSON),
		"bands_digest":   digestOf(bandsJSON),
	})
	if err != nil {
		return nil, err
	}
	regs = append(regs, Registration{
		Kind: kindSchema, Key: aggregateCorpusKey, Body: aggregateCorpusBody,
		SourceRef: "internal/data/seed/testdata/workers.json+bands.json", PublishedAt: seedPublishedAt,
	})

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
	// Inserted is how many definition_version registrations were newly
	// written.
	Inserted int
	// Skipped is how many registrations already existed with matching
	// content (the idempotent no-op case).
	Skipped int
	// Digest is ArtifactDigest of the plan Seed inserted from.
	Digest string
	// Aggregates holds the identifiers aggregates.LoadFixtures assigned when
	// this Seed call was the one that actually populated the person/worker/
	// employment/.../compensation_* aggregate tables for tenantID -- i.e.
	// when the aggregateCorpusKey marker was newly inserted, not already
	// present. It is nil on every call that found the corpus already seeded.
	Aggregates *aggregates.LoadedFixtures
}

// Seed scopes tx to tenantID (via internal/data/tenancy.WithTenant, so
// migration 00008's row level security governs these writes the same as any
// other tenant-scoped write), inserts the canonical registry registrations
// for tenantID (each as a definition_version at version 1), and -- exactly
// once per tenant -- loads the person/worker/employment/assignment/
// legal_entity/organization_unit/job/job_position/position_occupancy/
// compensation_* fixtures into their aggregate tables via
// aggregates.LoadFixtures. Running Seed again for the same tenant is a
// no-op: a definition_version registration whose (tenant, kind, key,
// version) already exists is left alone by ON CONFLICT DO NOTHING (and Seed
// then verifies the stored content digest still matches the plan's,
// refusing to report success over silently drifted seed content), and the
// aggregateCorpusKey marker being already present is what tells Seed not to
// call LoadFixtures again.
func Seed(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID) (Summary, error) {
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
		affected, err := tx.Exec(ctx, `
			INSERT INTO definition_version (
				tenant_id, definition_kind, definition_key, version, definition_digest,
				source_ref, body, published_by, published_at)
			VALUES ($1, $2, $3, 1, $4, $5, $6, $7, $8)
			ON CONFLICT (tenant_id, definition_kind, definition_key, version) DO NOTHING`,
			tenantID, r.Kind, r.Key, digest, r.SourceRef, r.Body, PublishedBy, r.PublishedAt)
		if err != nil {
			return Summary{}, fmt.Errorf("seed: insert %s %q: %w", r.Kind, r.Key, err)
		}
		if affected == 1 {
			summary.Inserted++
			if r.Kind == kindSchema && r.Key == aggregateCorpusKey {
				// This tenant has never carried the aggregate-corpus marker
				// before: this transaction is the one that gets to populate
				// the physical aggregate tables, exactly once.
				loaded, err := aggregates.LoadFixtures(ctx, tx, tenantID)
				if err != nil {
					return Summary{}, fmt.Errorf("seed: load aggregate fixtures: %w", err)
				}
				summary.Aggregates = loaded
			}
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
