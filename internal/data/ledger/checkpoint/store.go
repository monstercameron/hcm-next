package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// StoragePrecision is the time resolution a stored checkpoint keeps.
//
// PostgreSQL's timestamptz holds microseconds; the canonical projection a
// signature covers hashes nanoseconds. A manifest signed with nanosecond
// precision and then written to the database would come back with different
// bytes and its own signature would stop verifying - a failure that would
// look exactly like tampering. Every instant is therefore truncated to
// microseconds by [Truncate] before it is signed, so the value that is
// signed is the value that survives a round trip.
const StoragePrecision = time.Microsecond

// Truncate normalizes an instant to UTC at [StoragePrecision].
func Truncate(t time.Time) time.Time { return t.UTC().Truncate(StoragePrecision) }

// Store persists and reads signed checkpoint epochs. It holds no state; the
// value exists so the method set can be swapped at a composition root.
type Store struct{}

// NewStore returns a Store.
func NewStore() *Store { return &Store{} }

// Append writes one signed manifest and its stream heads inside the caller's
// transaction. It verifies the signature first: an unverifiable manifest is
// not persisted, because a stored checkpoint is evidence and evidence that
// was never checked is worse than none.
//
// A second Append for an epoch number the tenant already has fails with
// [ErrEpochAlreadyRecorded]. Epochs are append-only in the same sense ledger
// events are, and the table refuses UPDATE and DELETE through the same
// forbid_mutation trigger, so a signature can never be rewritten in place -
// a correction is a new epoch (see [Supersede]).
func (s *Store) Append(ctx context.Context, tx dbport.Tx, m Manifest, dir KeyDirectory) error {
	if err := Verify(m, dir); err != nil {
		return err
	}
	digest, err := m.CanonicalDigest()
	if err != nil {
		return err
	}

	var previousEpoch, correctsEpoch any
	var previousDigest, correctsReason any
	if m.PreviousEpochID != uuid.Nil {
		previousEpoch = m.PreviousEpochID
		previousDigest = m.PreviousManifestDigest
	}
	if m.CorrectsEpochID != uuid.Nil {
		correctsEpoch = m.CorrectsEpochID
		correctsReason = m.CorrectsReason
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_checkpoint_epoch (
			tenant_id, epoch_id, epoch_number, manifest_schema_version,
			previous_epoch_id, previous_manifest_digest, corrects_epoch_id, corrects_reason,
			schema_release_version, schema_release_digest,
			root_digest, root_digest_algorithm, manifest_digest,
			covers_from, covers_to, created_at,
			signature_algorithm, signing_key_id, signing_public_key, signature_value, signed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)`,
		m.Tenant, m.EpochID, m.EpochNumber, m.SchemaVersion,
		previousEpoch, previousDigest, correctsEpoch, correctsReason,
		m.Schema.Version, m.Schema.Digest,
		m.RootDigest, m.RootDigestAlgorithm, digest,
		m.CoversFrom, m.CoversTo, m.CreatedAt,
		m.Signature.Algorithm, m.Signature.KeyID, m.Signature.PublicKey, m.Signature.Value, m.Signature.SignedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrEpochAlreadyRecorded{Tenant: m.Tenant, EpochNumber: m.EpochNumber}
		}
		return fmt.Errorf("checkpoint: record epoch %d: %w", m.EpochNumber, err)
	}

	heads := m.OrderedStreams()
	statements := make([]dbport.Statement, 0, len(heads))
	for _, head := range heads {
		statements = append(statements, dbport.Statement{SQL: `
			INSERT INTO ledger_checkpoint_stream_head (
				tenant_id, epoch_id, stream_key, head_sequence, chain_hash, chain_algorithm)
			VALUES ($1, $2, $3, $4, $5, $6)`, Args: []any{
			m.Tenant, m.EpochID, head.StreamKey, head.Sequence, head.ChainHash, head.ChainAlgorithm,
		}})
	}
	counts, err := dbport.ExecAll(ctx, tx, statements)
	if err != nil {
		index := dbport.FailedStatement(counts, len(heads))
		if index >= 0 {
			return fmt.Errorf("checkpoint: record epoch %d head for stream %s: %w",
				m.EpochNumber, heads[index].StreamKey, err)
		}
		return fmt.Errorf("checkpoint: record epoch %d heads: %w", m.EpochNumber, err)
	}
	return nil
}

const selectEpochColumns = `
	tenant_id, epoch_id, epoch_number, manifest_schema_version,
	previous_epoch_id, previous_manifest_digest, corrects_epoch_id, corrects_reason,
	schema_release_version, schema_release_digest,
	root_digest, root_digest_algorithm,
	covers_from, covers_to, created_at,
	signature_algorithm, signing_key_id, signing_public_key, signature_value, signed_at`

func scanEpoch(row interface{ Scan(dest ...any) error }) (Manifest, error) {
	var (
		m              Manifest
		previousEpoch  *uuid.UUID
		previousDigest *string
		correctsEpoch  *uuid.UUID
		correctsReason *string
		sig            Signature
	)
	if err := row.Scan(
		&m.Tenant, &m.EpochID, &m.EpochNumber, &m.SchemaVersion,
		&previousEpoch, &previousDigest, &correctsEpoch, &correctsReason,
		&m.Schema.Version, &m.Schema.Digest,
		&m.RootDigest, &m.RootDigestAlgorithm,
		&m.CoversFrom, &m.CoversTo, &m.CreatedAt,
		&sig.Algorithm, &sig.KeyID, &sig.PublicKey, &sig.Value, &sig.SignedAt,
	); err != nil {
		return Manifest{}, err
	}
	if previousEpoch != nil {
		m.PreviousEpochID = *previousEpoch
	}
	if previousDigest != nil {
		m.PreviousManifestDigest = *previousDigest
	}
	if correctsEpoch != nil {
		m.CorrectsEpochID = *correctsEpoch
	}
	if correctsReason != nil {
		m.CorrectsReason = *correctsReason
	}
	// Times come back in whatever zone the driver chose; the canonical
	// projection is defined in UTC, so normalize here rather than letting a
	// session timezone change a digest.
	m.CoversFrom = Truncate(m.CoversFrom)
	m.CoversTo = Truncate(m.CoversTo)
	m.CreatedAt = Truncate(m.CreatedAt)
	sig.SignedAt = Truncate(sig.SignedAt)
	m.Signature = &sig
	return m, nil
}

// readHeads loads one epoch's stream heads, ordered by stream key.
func readHeads(ctx context.Context, q Querier, tenant, epochID uuid.UUID) ([]StreamHead, error) {
	rows, err := q.Query(ctx, `
		SELECT stream_key, head_sequence, chain_hash, chain_algorithm
		FROM ledger_checkpoint_stream_head
		WHERE tenant_id = $1 AND epoch_id = $2
		ORDER BY stream_key ASC`, tenant, epochID)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: read heads for epoch %s: %w", epochID, err)
	}
	defer rows.Close()

	var out []StreamHead
	for rows.Next() {
		var head StreamHead
		if err := rows.Scan(&head.StreamKey, &head.Sequence, &head.ChainHash, &head.ChainAlgorithm); err != nil {
			return nil, fmt.Errorf("checkpoint: scan head for epoch %s: %w", epochID, err)
		}
		out = append(out, head)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("checkpoint: read heads for epoch %s: %w", epochID, err)
	}
	return out, nil
}

// Read returns one epoch by number, with its stream heads.
func (s *Store) Read(ctx context.Context, q Querier, tenant uuid.UUID, epochNumber int64) (Manifest, error) {
	row := q.QueryRow(ctx, `
		SELECT `+selectEpochColumns+`
		FROM ledger_checkpoint_epoch
		WHERE tenant_id = $1 AND epoch_number = $2`, tenant, epochNumber)
	m, err := scanEpoch(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return Manifest{}, ErrEpochNotFound{Tenant: tenant, EpochNumber: epochNumber}
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("checkpoint: read epoch %d: %w", epochNumber, err)
	}
	heads, err := readHeads(ctx, q, tenant, m.EpochID)
	if err != nil {
		return Manifest{}, err
	}
	m.Streams = heads
	return m, nil
}

// Latest returns the tenant's highest-numbered epoch. ok is false when the
// tenant has no checkpoint yet, which is a legitimate state and not an
// error: it is what makes the next checkpoint epoch 1.
func (s *Store) Latest(ctx context.Context, q Querier, tenant uuid.UUID) (Manifest, bool, error) {
	row := q.QueryRow(ctx, `
		SELECT `+selectEpochColumns+`
		FROM ledger_checkpoint_epoch
		WHERE tenant_id = $1
		ORDER BY epoch_number DESC
		LIMIT 1`, tenant)
	m, err := scanEpoch(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, fmt.Errorf("checkpoint: read latest epoch: %w", err)
	}
	heads, err := readHeads(ctx, q, tenant, m.EpochID)
	if err != nil {
		return Manifest{}, false, err
	}
	m.Streams = heads
	return m, true, nil
}

// List returns every epoch a tenant has, ordered by epoch number ascending,
// each with its stream heads. It is what [VerifyChain] walks.
func (s *Store) List(ctx context.Context, q Querier, tenant uuid.UUID) ([]Manifest, error) {
	rows, err := q.Query(ctx, `
		SELECT `+selectEpochColumns+`
		FROM ledger_checkpoint_epoch
		WHERE tenant_id = $1
		ORDER BY epoch_number ASC`, tenant)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: list epochs: %w", err)
	}
	var epochs []Manifest
	func() {
		defer rows.Close()
		for rows.Next() {
			m, scanErr := scanEpoch(rows)
			if scanErr != nil {
				err = fmt.Errorf("checkpoint: scan epoch: %w", scanErr)
				return
			}
			epochs = append(epochs, m)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			err = fmt.Errorf("checkpoint: list epochs: %w", rowsErr)
		}
	}()
	if err != nil {
		return nil, err
	}

	// Heads are read after the epoch cursor is closed: dbport.Querier makes
	// no promise that a second query may run while a first result is still
	// open.
	for i := range epochs {
		heads, headErr := readHeads(ctx, q, tenant, epochs[i].EpochID)
		if headErr != nil {
			return nil, headErr
		}
		epochs[i].Streams = heads
	}
	return epochs, nil
}
