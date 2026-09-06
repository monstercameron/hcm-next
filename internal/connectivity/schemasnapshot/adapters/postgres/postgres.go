// Package postgres is the PostgreSQL adapter for INTG-004 schema-snapshot
// quarantine (owner: connectivity; phase: P1A).
//
// It implements [schemasnapshot.Store] over integration_schema_snapshot and
// integration_schema_snapshot_evidence, and [schemasnapshot.ArtifactStore]
// by wrapping internal/data/artifacts.Put -- both over one caller-owned
// [dbport.Tx], so a snapshot's bytes and its quarantine row commit or roll
// back together. Business packages depend on the two port interfaces
// declared in schemasnapshot, never on this one.
//
// Two behaviours are enforced by the database rather than by this code:
// integration_schema_snapshot carries a controlled-update trigger that
// permits only the QUARANTINED -> {ADMITTED, REJECTED} transition
// (migrations/00025_schema_snapshot.sql), and its evidence table carries a
// unique index limiting it to exactly one decision row per snapshot.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/connectivity/schemasnapshot"
	"github.com/monstercameron/hcm-next/internal/data/artifacts"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// ArtifactStore adapts internal/data/artifacts.Put to
// [schemasnapshot.ArtifactStore] over one caller-owned transaction.
type ArtifactStore struct {
	tx     dbport.Tx
	schema string
}

// NewArtifactStore returns an artifact store adapter. coreSchema is the
// schema the rest of the migration tree was applied into -- the same value
// [internal/data/artifacts.Schema] itself expects, computed once here rather
// than left to every caller.
func NewArtifactStore(tx dbport.Tx, coreSchema string) *ArtifactStore {
	return &ArtifactStore{tx: tx, schema: artifacts.Schema(coreSchema)}
}

var _ schemasnapshot.ArtifactStore = (*ArtifactStore)(nil)

// Put implements [schemasnapshot.ArtifactStore].
func (a *ArtifactStore) Put(ctx context.Context, req schemasnapshot.ArtifactPutRequest) (string, int64, bool, error) {
	const op = "schemasnapshot/postgres.ArtifactStore.Put"
	tenant, err := uuid.Parse(req.TenantID)
	if err != nil {
		return "", 0, false, invalidTenant(op, req.TenantID, err)
	}
	rec, created, err := artifacts.Put(ctx, a.tx, a.schema, artifacts.PutRequest{
		Tenant:              tenant,
		Content:             req.Raw,
		MediaType:           req.MediaType,
		Classification:      req.Classification,
		RetentionClass:      req.RetentionClass,
		CreatorPrincipalRef: req.CreatorPrincipalRef,
		EvidenceID:          req.EvidenceID,
	})
	if err != nil {
		return "", 0, false, storeErr(op, "put artifact for tenant %s: %v", req.TenantID, err)
	}
	return rec.ContentID, rec.ByteSize, created, nil
}

// Store is the PostgreSQL adapter for [schemasnapshot.Store].
type Store struct {
	db schemasnapshot.Querier
}

// NewStore returns a store over the given querier. A [dbport.Conn] and a
// [dbport.Tx] both satisfy [schemasnapshot.Querier]; passing the same
// transaction as [NewArtifactStore] is what makes one Ingest call atomic
// across both schemas.
func NewStore(db schemasnapshot.Querier) *Store { return &Store{db: db} }

var _ schemasnapshot.Store = (*Store)(nil)

const snapshotColumns = `
    tenant_id, snapshot_id, connector_id, connection_id, source_ref,
    captured_at, declared_format, digest_algorithm, canonical_digest,
    artifact_ref, byte_size, supersedes_ref, state, state_reason, decided_at, created_at`

// Insert implements [schemasnapshot.Store].
//
// The insert is ON CONFLICT DO NOTHING on the primary key. A conflict is not
// an error: it means this exact snapshot identity is already on file, and
// the stored row is compared against the incoming one so identical identity
// reports as existing while different identity reports as immutable -- the
// same technique internal/connectivity/observe/adapters/postgres.Append uses
// for observation evidence.
func (s *Store) Insert(ctx context.Context, snap schemasnapshot.SchemaSnapshot) (schemasnapshot.SchemaSnapshot, bool, error) {
	const op = "schemasnapshot/postgres.Store.Insert"
	if snap.State != schemasnapshot.StateQuarantined {
		return schemasnapshot.SchemaSnapshot{}, false, invalidErr(op, "Insert only accepts a snapshot in StateQuarantined")
	}
	if err := snap.Validate(); err != nil {
		return schemasnapshot.SchemaSnapshot{}, false, err
	}
	tenant, err := uuid.Parse(snap.TenantID)
	if err != nil {
		return schemasnapshot.SchemaSnapshot{}, false, invalidTenant(op, snap.TenantID, err)
	}
	// Checked here in Go, ahead of the INSERT, so the error this method
	// returns classifies exactly like schemasnapshot.MemoryStore.Insert's
	// does; migrations/00025's schema_snapshot_supersedes_same_provider
	// trigger enforces the identical rule as a database-level backstop.
	if snap.Supersedes != nil {
		prior, err := s.Get(ctx, snap.TenantID, *snap.Supersedes)
		if err != nil {
			if errors.Is(err, schemasnapshot.ErrNotFound) {
				return schemasnapshot.SchemaSnapshot{}, false, incompleteErr(op,
					"supersedes_ref %s names no existing snapshot for this tenant", *snap.Supersedes)
			}
			return schemasnapshot.SchemaSnapshot{}, false, err
		}
		if !prior.Provider.Equal(snap.Provider) {
			return schemasnapshot.SchemaSnapshot{}, false, invalidErr(op,
				"snapshot %s cannot supersede %s of a different provider", snap.SnapshotID, *snap.Supersedes)
		}
	}

	var supersedes any
	if snap.Supersedes != nil {
		supersedes = *snap.Supersedes
	}
	affected, err := s.db.Exec(ctx, `
        INSERT INTO integration_schema_snapshot (
            tenant_id, snapshot_id, connector_id, connection_id, source_ref,
            captured_at, declared_format, digest_algorithm, canonical_digest,
            artifact_ref, byte_size, supersedes_ref, state
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'QUARANTINED')
        ON CONFLICT (tenant_id, snapshot_id) DO NOTHING`,
		tenant, snap.SnapshotID, snap.Provider.ConnectorID, snap.Provider.ConnectionID, snap.Provider.SourceRef,
		snap.CapturedAt.UTC(), string(snap.DeclaredFormat), snap.DigestAlgorithm, snap.CanonicalDigest,
		snap.ArtifactRef, snap.ByteSize, supersedes)
	if err != nil {
		return schemasnapshot.SchemaSnapshot{}, false, storeErr(op, "insert snapshot %s: %v", snap.SnapshotID, err)
	}

	existing, err := s.Get(ctx, snap.TenantID, snap.SnapshotID)
	if err != nil {
		return schemasnapshot.SchemaSnapshot{}, false, err
	}
	if affected == 1 {
		return existing, true, nil
	}
	if !sameStoredIdentity(existing, snap) {
		return schemasnapshot.SchemaSnapshot{}, false, immutableErr(op,
			"snapshot %s is already stored with different identity metadata", snap.SnapshotID)
	}
	return existing, false, nil
}

func sameStoredIdentity(existing, incoming schemasnapshot.SchemaSnapshot) bool {
	if !existing.Provider.Equal(incoming.Provider) {
		return false
	}
	if !existing.CapturedAt.Equal(incoming.CapturedAt) {
		return false
	}
	existingSupersedes, incomingSupersedes := "", ""
	if existing.Supersedes != nil {
		existingSupersedes = existing.Supersedes.String()
	}
	if incoming.Supersedes != nil {
		incomingSupersedes = incoming.Supersedes.String()
	}
	return existing.DeclaredFormat == incoming.DeclaredFormat &&
		existing.DigestAlgorithm == incoming.DigestAlgorithm &&
		existing.CanonicalDigest == incoming.CanonicalDigest &&
		existing.ArtifactRef == incoming.ArtifactRef &&
		existing.ByteSize == incoming.ByteSize &&
		existingSupersedes == incomingSupersedes
}

// Decide implements [schemasnapshot.Store].
//
// The UPDATE's WHERE clause carries state = 'QUARANTINED', so a concurrent
// or repeated call that loses the race affects zero rows instead of raising:
// this method then re-reads the row and, if the verdict already on file
// agrees, returns it unchanged; if it disagrees, that is
// [schemasnapshot.ErrImmutable]. The evidence insert happens only on the
// branch that actually performed the transition, in the same call, so a
// snapshot is never left decided without evidence.
func (s *Store) Decide(ctx context.Context, tenantID string, id uuid.UUID, verdict schemasnapshot.State, reason string, decidedAt time.Time, results []schemasnapshot.ValidatorResult) (schemasnapshot.SchemaSnapshot, schemasnapshot.Evidence, error) {
	const op = "schemasnapshot/postgres.Store.Decide"
	if !verdict.Terminal() {
		return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, invalidErr(op, "verdict %q is not a terminal state", string(verdict))
	}
	tenant, err := uuid.Parse(tenantID)
	if err != nil {
		return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, invalidTenant(op, tenantID, err)
	}

	affected, err := s.db.Exec(ctx, `
        UPDATE integration_schema_snapshot
        SET state = $3, state_reason = $4, decided_at = $5
        WHERE tenant_id = $1 AND snapshot_id = $2 AND state = 'QUARANTINED'`,
		tenant, id, string(verdict), reason, decidedAt.UTC())
	if err != nil {
		return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, storeErr(op, "update snapshot %s: %v", id, err)
	}

	if affected == 1 {
		payload, err := json.Marshal(results)
		if err != nil {
			return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, storeErr(op, "encode validator results for %s: %v", id, err)
		}
		evidenceID := uuid.New()
		if _, err := s.db.Exec(ctx, `
            INSERT INTO integration_schema_snapshot_evidence (
                tenant_id, evidence_id, snapshot_id, verdict, validator_results, decided_at
            ) VALUES ($1, $2, $3, $4, $5, $6)`,
			tenant, evidenceID, id, string(verdict), payload, decidedAt.UTC()); err != nil {
			return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, storeErr(op, "insert evidence for %s: %v", id, err)
		}
		snap, err := s.Get(ctx, tenantID, id)
		if err != nil {
			return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, err
		}
		ev, ok, err := s.Evidence(ctx, tenantID, id)
		if err != nil {
			return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, err
		}
		if !ok {
			return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, storeErr(op, "evidence for %s vanished immediately after insert", id)
		}
		return snap, ev, nil
	}

	// No row matched the QUARANTINED guard: either the snapshot does not
	// exist, or it was already decided (by this call racing another, or by a
	// resumed retry after a prior call's evidence insert failed).
	snap, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, err
	}
	if snap.State != verdict {
		return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, immutableErr(op,
			"snapshot %s is already decided %s; refusing to redecide as %s", id, snap.State, verdict)
	}
	ev, ok, err := s.Evidence(ctx, tenantID, id)
	if err != nil {
		return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, err
	}
	if !ok {
		return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, storeErr(op, "snapshot %s is decided %s but carries no evidence", id, snap.State)
	}
	return snap, ev, nil
}

// Get implements [schemasnapshot.Store].
func (s *Store) Get(ctx context.Context, tenantID string, id uuid.UUID) (schemasnapshot.SchemaSnapshot, error) {
	const op = "schemasnapshot/postgres.Store.Get"
	tenant, err := uuid.Parse(tenantID)
	if err != nil {
		return schemasnapshot.SchemaSnapshot{}, invalidTenant(op, tenantID, err)
	}
	row := s.db.QueryRow(ctx, `SELECT `+snapshotColumns+`
        FROM integration_schema_snapshot WHERE tenant_id = $1 AND snapshot_id = $2`, tenant, id)
	snap, err := scanSnapshot(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return schemasnapshot.SchemaSnapshot{}, notFoundErr(op, "tenant %s has no snapshot %s", tenantID, id)
	}
	if err != nil {
		return schemasnapshot.SchemaSnapshot{}, storeErr(op, "scan snapshot %s: %v", id, err)
	}
	return snap, nil
}

// Evidence implements [schemasnapshot.Store].
func (s *Store) Evidence(ctx context.Context, tenantID string, id uuid.UUID) (schemasnapshot.Evidence, bool, error) {
	const op = "schemasnapshot/postgres.Store.Evidence"
	tenant, err := uuid.Parse(tenantID)
	if err != nil {
		return schemasnapshot.Evidence{}, false, invalidTenant(op, tenantID, err)
	}
	var (
		ev        schemasnapshot.Evidence
		verdict   string
		payload   []byte
		decidedAt time.Time
		createdAt time.Time
	)
	err = s.db.QueryRow(ctx, `
        SELECT evidence_id, snapshot_id, verdict, validator_results, decided_at, created_at
        FROM integration_schema_snapshot_evidence
        WHERE tenant_id = $1 AND snapshot_id = $2`, tenant, id).
		Scan(&ev.EvidenceID, &ev.SnapshotID, &verdict, &payload, &decidedAt, &createdAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return schemasnapshot.Evidence{}, false, nil
	}
	if err != nil {
		return schemasnapshot.Evidence{}, false, storeErr(op, "scan evidence for %s: %v", id, err)
	}
	if err := json.Unmarshal(payload, &ev.Results); err != nil {
		return schemasnapshot.Evidence{}, false, storeErr(op, "decode validator results for %s: %v", id, err)
	}
	ev.TenantID = tenantID
	ev.Verdict = schemasnapshot.State(verdict)
	ev.DecidedAt = decidedAt.UTC()
	ev.RecordedAt = createdAt.UTC()
	return ev, true, nil
}

func scanSnapshot(row dbport.Row) (schemasnapshot.SchemaSnapshot, error) {
	var (
		snap        schemasnapshot.SchemaSnapshot
		tenant      uuid.UUID
		format      string
		state       string
		capturedAt  time.Time
		createdAt   time.Time
		stateReason *string
		decidedAt   *time.Time
		supersedes  *uuid.UUID
	)
	if err := row.Scan(
		&tenant, &snap.SnapshotID, &snap.Provider.ConnectorID, &snap.Provider.ConnectionID, &snap.Provider.SourceRef,
		&capturedAt, &format, &snap.DigestAlgorithm, &snap.CanonicalDigest,
		&snap.ArtifactRef, &snap.ByteSize, &supersedes, &state, &stateReason, &decidedAt, &createdAt,
	); err != nil {
		return schemasnapshot.SchemaSnapshot{}, err
	}
	snap.TenantID = tenant.String()
	snap.CapturedAt = capturedAt.UTC()
	snap.DeclaredFormat = schemasnapshot.DeclaredFormat(format)
	snap.State = schemasnapshot.State(state)
	snap.Supersedes = supersedes
	snap.RecordedAt = createdAt.UTC()
	if stateReason != nil {
		snap.StateReason = *stateReason
	}
	if decidedAt != nil {
		snap.DecidedAt = decidedAt.UTC()
	}
	return snap, nil
}

func invalidTenant(op, tenantID string, cause error) error {
	return &schemasnapshot.Error{Op: op, Cause: schemasnapshot.ErrInvalid,
		Detail: fmt.Sprintf("tenant id %q is not a uuid: %v", tenantID, cause)}
}

func incompleteErr(op, format string, args ...any) error {
	return &schemasnapshot.Error{Op: op, Cause: schemasnapshot.ErrIncomplete, Detail: fmt.Sprintf(format, args...)}
}

func invalidErr(op, format string, args ...any) error {
	return &schemasnapshot.Error{Op: op, Cause: schemasnapshot.ErrInvalid, Detail: fmt.Sprintf(format, args...)}
}

func notFoundErr(op, format string, args ...any) error {
	return &schemasnapshot.Error{Op: op, Cause: schemasnapshot.ErrNotFound, Detail: fmt.Sprintf(format, args...)}
}

func immutableErr(op, format string, args ...any) error {
	return &schemasnapshot.Error{Op: op, Cause: schemasnapshot.ErrImmutable, Detail: fmt.Sprintf(format, args...)}
}

func storeErr(op, format string, args ...any) error {
	return &schemasnapshot.Error{Op: op, Cause: schemasnapshot.ErrStore, Detail: fmt.Sprintf(format, args...)}
}
