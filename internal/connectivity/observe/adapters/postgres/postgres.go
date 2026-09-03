// Package postgres is the PostgreSQL adapter for observation evidence.
//
// Semantic owner: connectivity. Phase: P1A.
//
// It implements [observe.ObservationStore] and [observe.CheckpointStore] over
// the external_observation and observation_checkpoint tables declared in
// migrations/00007_connectivity.sql. Business packages depend on the port
// interfaces in the observe package, never on this one.
//
// Two behaviours are enforced by the database rather than by this code, which
// is the point of putting them there: external_observation carries an
// append-only trigger, so no code path in any process can rewrite an
// observation; and its (tenant, connection, object, snapshot, page) uniqueness
// makes a re-read of a page land on the existing row instead of creating a
// second one.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/observe"
)

// Store is the PostgreSQL observation and checkpoint store.
type Store struct {
	db observe.Querier
}

// New returns a store over the given querier. The port is
// [observe.Querier]: the consumer package declares it, and this adapter only
// implements it.
func New(db observe.Querier) *Store { return &Store{db: db} }

var (
	_ observe.ObservationStore = (*Store)(nil)
	_ observe.CheckpointStore  = (*Store)(nil)
)

// ErrTenant reports a tenant id that is not a UUID. The schema scopes every
// authoritative row by a uuid tenant_ref, so a non-UUID tenant is a caller
// defect rather than a missing row.
var ErrTenant = errors.New("connectivity/postgres: tenant id is not a uuid")

const observationColumns = `
    tenant_id, observation_id, connection_id, connector_id, connector_version,
    source_ref, authority_ref, object_kind, schema_version, snapshot_id,
    page_sequence, start_cursor, next_cursor, record_count, complete,
    classification, freshness, retrieved_at, watermark_at,
    content_digest, digest_algorithm, canonical_length, payload, raw_artifact_ref`

// Append implements [observe.ObservationStore].
//
// The insert is ON CONFLICT DO NOTHING across both unique keys. A conflict is
// not an error: it means this page was already observed, and the stored row is
// then compared against the incoming one so that identical evidence reports as
// existing while different evidence reports as immutable.
func (s *Store) Append(ctx context.Context, obs observe.Observation) (observe.AppendResult, error) {
	const op = "connectivity/postgres.Append"
	if err := obs.Verify(); err != nil {
		return observe.AppendResult{}, err
	}
	tenant, err := parseTenant(op, obs.TenantID)
	if err != nil {
		return observe.AppendResult{}, err
	}
	algorithm, hexDigest, err := observe.SplitDigest(obs.ContentDigest)
	if err != nil {
		return observe.AppendResult{}, err
	}

	tag, err := s.db.Exec(ctx, `
        INSERT INTO external_observation (`+observationColumns+`)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
                $16, $17, $18, $19, $20, $21, $22, $23, $24)
        ON CONFLICT DO NOTHING`,
		tenant, obs.ObservationID, obs.ConnectionID, obs.ConnectorID, obs.ConnectorVersion,
		obs.SourceRef, obs.AuthorityRef, string(obs.Object), obs.SchemaVersion, obs.SnapshotID,
		int64(obs.PageSequence), obs.StartCursor, obs.NextCursor, obs.RecordCount, obs.Complete,
		string(obs.Classification), string(obs.Freshness), obs.RetrievedAt.UTC(), obs.Watermark.UTC(),
		hexDigest, algorithm, len(obs.Payload), obs.Payload, obs.RawArtifactRef)
	if err != nil {
		return observe.AppendResult{}, wrap(op, err, "insert observation %s", obs.ObservationID)
	}
	if tag.RowsAffected() == 1 {
		return observe.AppendResult{Observation: obs}, nil
	}

	existing, err := s.Get(ctx, obs.TenantID, obs.ObservationID)
	if err != nil {
		// The conflict was on the page-uniqueness key under a different
		// observation id, which can only happen if the identity derivation
		// changed. Report it as an immutability violation, not as a miss.
		if errors.Is(err, observe.ErrNotFound) {
			return observe.AppendResult{}, immutable(op,
				"page %d of snapshot %s is already observed under a different observation id",
				obs.PageSequence, obs.SnapshotID)
		}
		return observe.AppendResult{}, err
	}
	if !existing.SameContent(obs) {
		return observe.AppendResult{}, immutable(op,
			"observation %s is stored with digest %s; refusing to replace it with %s",
			obs.ObservationID, existing.ContentDigest, obs.ContentDigest)
	}
	return observe.AppendResult{Observation: existing, Existing: true}, nil
}

// Get implements [observe.ObservationStore].
func (s *Store) Get(ctx context.Context, tenantID string, id uuid.UUID) (observe.Observation, error) {
	const op = "connectivity/postgres.Get"
	tenant, err := parseTenant(op, tenantID)
	if err != nil {
		return observe.Observation{}, err
	}
	rows, err := s.db.Query(ctx, `
        SELECT `+observationColumns+`
        FROM external_observation
        WHERE tenant_id = $1 AND observation_id = $2`, tenant, id)
	if err != nil {
		return observe.Observation{}, wrap(op, err, "select observation %s", id)
	}
	defer rows.Close()

	if !rows.Next() {
		if rowsErr := rows.Err(); rowsErr != nil {
			return observe.Observation{}, wrap(op, rowsErr, "read observation %s", id)
		}
		return observe.Observation{}, notFound(op, "tenant %s has no observation %s", tenantID, id)
	}
	obs, err := scanObservation(rows)
	if err != nil {
		return observe.Observation{}, wrap(op, err, "scan observation %s", id)
	}
	return obs, nil
}

// List implements [observe.ObservationStore]. Results come back in replay
// order: object, then snapshot, then page sequence.
func (s *Store) List(ctx context.Context, q observe.Query) ([]observe.Observation, error) {
	const op = "connectivity/postgres.List"
	tenant, err := parseTenant(op, q.TenantID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
        SELECT `+observationColumns+`
        FROM external_observation
        WHERE tenant_id = $1
          AND ($2 = '' OR connection_id = $2)
          AND ($3 = '' OR object_kind = $3)
          AND ($4 = '' OR snapshot_id = $4)
          AND ($5 = 0 OR page_sequence >= $5)
          AND ($6 = 0 OR page_sequence <= $6)
        ORDER BY object_kind, snapshot_id, page_sequence
        LIMIT CASE WHEN $7 = 0 THEN NULL ELSE $7 END`,
		tenant, q.ConnectionID, string(q.Object), q.SnapshotID,
		int64(q.FromPage), int64(q.ToPage), int64(q.Limit))
	if err != nil {
		return nil, wrap(op, err, "select observations for tenant %s", q.TenantID)
	}
	defer rows.Close()

	out := make([]observe.Observation, 0, 8)
	for rows.Next() {
		obs, scanErr := scanObservation(rows)
		if scanErr != nil {
			return nil, wrap(op, scanErr, "scan observation")
		}
		out = append(out, obs)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, wrap(op, rowsErr, "read observations for tenant %s", q.TenantID)
	}
	return out, nil
}

// Load implements [observe.CheckpointStore].
func (s *Store) Load(ctx context.Context, key observe.CheckpointKey) (observe.Checkpoint, bool, error) {
	const op = "connectivity/postgres.Load"
	tenant, err := parseTenant(op, key.TenantID)
	if err != nil {
		return observe.Checkpoint{}, false, err
	}
	var (
		cp        observe.Checkpoint
		fence     int64
		pages     int64
		records   int64
		updatedAt time.Time
	)
	cp.Key = key
	scanErr := s.db.QueryRow(ctx, `
        SELECT run_id, snapshot_id, cursor_token, fence, pages_committed,
               records_committed, complete, updated_at
        FROM observation_checkpoint
        WHERE tenant_id = $1 AND connection_id = $2 AND object_kind = $3`,
		tenant, key.ConnectionID, string(key.Object)).
		Scan(&cp.RunID, &cp.SnapshotID, &cp.CursorToken, &fence, &pages, &records,
			&cp.Complete, &updatedAt)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return observe.Checkpoint{}, false, nil
	}
	if scanErr != nil {
		return observe.Checkpoint{}, false, wrap(op, scanErr,
			"select checkpoint for %s/%s", key.ConnectionID, key.Object)
	}
	cp.Fence = uint64(fence)
	cp.PagesCommitted = uint64(pages)
	cp.RecordsCommitted = uint64(records)
	cp.UpdatedAt = updatedAt.UTC()
	return cp, true, nil
}

// Commit implements [observe.CheckpointStore].
//
// The fence comparison lives in the WHERE clause of the upsert rather than in a
// read-then-write, so two workers racing to commit cannot both believe they
// won: the database decides, in one statement.
func (s *Store) Commit(ctx context.Context, cp observe.Checkpoint) error {
	const op = "connectivity/postgres.Commit"
	if err := cp.Validate(); err != nil {
		return err
	}
	tenant, err := parseTenant(op, cp.Key.TenantID)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
        INSERT INTO observation_checkpoint (
            tenant_id, connection_id, object_kind, run_id, snapshot_id,
            cursor_token, fence, pages_committed, records_committed, complete, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        ON CONFLICT (tenant_id, connection_id, object_kind) DO UPDATE SET
            run_id            = EXCLUDED.run_id,
            snapshot_id       = EXCLUDED.snapshot_id,
            cursor_token      = EXCLUDED.cursor_token,
            fence             = EXCLUDED.fence,
            pages_committed   = EXCLUDED.pages_committed,
            records_committed = EXCLUDED.records_committed,
            complete          = EXCLUDED.complete,
            updated_at        = EXCLUDED.updated_at
        WHERE observation_checkpoint.fence < EXCLUDED.fence`,
		tenant, cp.Key.ConnectionID, string(cp.Key.Object), cp.RunID, cp.SnapshotID,
		cp.CursorToken, int64(cp.Fence), int64(cp.PagesCommitted), int64(cp.RecordsCommitted),
		cp.Complete, cp.UpdatedAt.UTC())
	if err != nil {
		return wrap(op, err, "commit checkpoint for %s/%s", cp.Key.ConnectionID, cp.Key.Object)
	}
	if tag.RowsAffected() == 0 {
		return fenced(op, "checkpoint for %s/%s refused a commit at fence %d",
			cp.Key.ConnectionID, cp.Key.Object, cp.Fence)
	}
	return nil
}

func scanObservation(rows pgx.Rows) (observe.Observation, error) {
	var (
		obs           observe.Observation
		tenant        uuid.UUID
		objectKind    string
		pageSequence  int64
		class         string
		freshness     string
		hexDigest     string
		algorithm     string
		canonicalLen  int32
		retrievedAt   time.Time
		watermarkAt   time.Time
		rawArtifactRf *string
	)
	if err := rows.Scan(
		&tenant, &obs.ObservationID, &obs.ConnectionID, &obs.ConnectorID, &obs.ConnectorVersion,
		&obs.SourceRef, &obs.AuthorityRef, &objectKind, &obs.SchemaVersion, &obs.SnapshotID,
		&pageSequence, &obs.StartCursor, &obs.NextCursor, &obs.RecordCount, &obs.Complete,
		&class, &freshness, &retrievedAt, &watermarkAt,
		&hexDigest, &algorithm, &canonicalLen, &obs.Payload, &rawArtifactRf,
	); err != nil {
		return observe.Observation{}, err
	}
	obs.TenantID = tenant.String()
	obs.Object = connectivity.ObjectKind(objectKind)
	obs.PageSequence = uint64(pageSequence)
	obs.Classification = observe.Classification(class)
	obs.Freshness = observe.Freshness(freshness)
	obs.ContentDigest = observe.JoinDigest(algorithm, hexDigest)
	obs.RetrievedAt = retrievedAt.UTC()
	obs.Watermark = watermarkAt.UTC()
	obs.RawArtifactRef = rawArtifactRf
	return obs, nil
}

func parseTenant(op, tenantID string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(tenantID)
	if err != nil {
		return uuid.Nil, &observe.Error{
			Op:     op,
			Cause:  ErrTenant,
			Detail: fmt.Sprintf("%q: %v", tenantID, err),
		}
	}
	return parsed, nil
}

func wrap(op string, cause error, format string, args ...any) error {
	return &observe.Error{
		Op:     op,
		Cause:  observe.ErrStore,
		Detail: fmt.Sprintf(format, args...) + ": " + cause.Error(),
	}
}

func notFound(op, format string, args ...any) error {
	return &observe.Error{Op: op, Cause: observe.ErrNotFound, Detail: fmt.Sprintf(format, args...)}
}

func immutable(op, format string, args ...any) error {
	return &observe.Error{Op: op, Cause: observe.ErrImmutable, Detail: fmt.Sprintf(format, args...)}
}

func fenced(op, format string, args ...any) error {
	return &observe.Error{Op: op, Cause: observe.ErrFenced, Detail: fmt.Sprintf(format, args...)}
}
