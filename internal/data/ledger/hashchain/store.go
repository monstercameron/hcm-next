package hashchain

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
)

// Querier is the minimal pgx surface a hash-chain read needs. It matches
// internal/data/ledger.Querier's shape exactly so the same *pgx.Conn,
// pgx.Tx, or *pgxpool.Pool a caller already holds satisfies both.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Appender extends a stream's hash chain by one link per appended event.
type Appender struct {
	digester *Digester
}

// NewAppender builds an Appender that mints chain-link digests through
// digester (ordinarily [NewDigester] over [NewRegistry]).
func NewAppender(digester *Digester) *Appender {
	return &Appender{digester: digester}
}

// Append records the chain link for one just-appended ledger event.
//
// It must run in the same transaction as the internal/data/ledger.Append
// call that produced receipt, after that call returns: internal/data/ledger
// already serializes every appender to a stream by locking stream_head
// FOR UPDATE for the lifetime of the transaction (append.go), so by the time
// Append observes receipt.Sequence no concurrent appender can be extending
// the same stream underneath it. Append does not itself take any lock.
//
// A second Append for a (stream, sequence) already linked fails with
// [ErrLinkAlreadyRecorded]: chain links are append-only, exactly like the
// events they extend.
func (a *Appender) Append(ctx context.Context, tx pgx.Tx, receipt datalogger.AppendReceipt) (ChainedLink, error) {
	if receipt.Digest == "" {
		return ChainedLink{}, fmt.Errorf("hashchain: append receipt for stream %s carries no digest", receipt.StreamKey)
	}

	prevHash := GenesisHash
	if receipt.Sequence > 1 {
		err := tx.QueryRow(ctx, `
			SELECT chain_hash FROM ledger_hash_chain_link
			WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
			receipt.Tenant, receipt.StreamKey, receipt.Sequence-1).Scan(&prevHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ChainedLink{}, ErrMissingPredecessor{StreamKey: receipt.StreamKey, Sequence: receipt.Sequence}
		}
		if err != nil {
			return ChainedLink{}, fmt.Errorf("hashchain: read predecessor link for stream %s@%d: %w",
				receipt.StreamKey, receipt.Sequence-1, err)
		}
	}

	algorithm, chainHash, err := a.digester.Link(prevHash, receipt.Digest)
	if err != nil {
		return ChainedLink{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_hash_chain_link (
			tenant_id, stream_key, sequence, event_id, prev_hash, chain_hash, chain_algorithm)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		receipt.Tenant, receipt.StreamKey, receipt.Sequence, receipt.EventID, prevHash, chainHash, algorithm)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ChainedLink{}, ErrLinkAlreadyRecorded{StreamKey: receipt.StreamKey, Sequence: receipt.Sequence}
		}
		return ChainedLink{}, fmt.Errorf("hashchain: record chain link for stream %s@%d: %w",
			receipt.StreamKey, receipt.Sequence, err)
	}

	return ChainedLink{
		StreamKey: receipt.StreamKey,
		Sequence:  receipt.Sequence,
		EventID:   receipt.EventID,
		PrevHash:  prevHash,
		ChainHash: chainHash,
		Algorithm: algorithm,
	}, nil
}

// ReadLinks returns every chain link recorded for a stream, ordered by
// sequence ascending.
func ReadLinks(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string) ([]ChainedLink, error) {
	rows, err := q.Query(ctx, `
		SELECT sequence, event_id, prev_hash, chain_hash, chain_algorithm, recorded_at
		FROM ledger_hash_chain_link
		WHERE tenant_id = $1 AND stream_key = $2
		ORDER BY sequence ASC`, tenant, streamKey)
	if err != nil {
		return nil, fmt.Errorf("hashchain: read links for stream %s: %w", streamKey, err)
	}
	defer rows.Close()

	var out []ChainedLink
	for rows.Next() {
		var link ChainedLink
		link.StreamKey = streamKey
		if err := rows.Scan(&link.Sequence, &link.EventID, &link.PrevHash, &link.ChainHash, &link.Algorithm, &link.RecordedAt); err != nil {
			return nil, fmt.Errorf("hashchain: scan link on stream %s: %w", streamKey, err)
		}
		out = append(out, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hashchain: read links for stream %s: %w", streamKey, err)
	}
	return out, nil
}

// ReadEventDigests returns every event's sequence, ID and digest for a
// stream, ordered by sequence ascending. It reads only the columns Verify
// needs, rather than the full internal/data/ledger.Reader envelope.
func ReadEventDigests(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string) ([]EventDigest, error) {
	rows, err := q.Query(ctx, `
		SELECT sequence, event_id, digest
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2
		ORDER BY sequence ASC`, tenant, streamKey)
	if err != nil {
		return nil, fmt.Errorf("hashchain: read event digests for stream %s: %w", streamKey, err)
	}
	defer rows.Close()

	var out []EventDigest
	for rows.Next() {
		var ev EventDigest
		if err := rows.Scan(&ev.Sequence, &ev.EventID, &ev.Digest); err != nil {
			return nil, fmt.Errorf("hashchain: scan event digest on stream %s: %w", streamKey, err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hashchain: read event digests for stream %s: %w", streamKey, err)
	}
	return out, nil
}

// CurrentHead returns the chain head checkpoint recorded at a stream's
// highest linked sequence. ok is false when the stream has no chain links
// yet.
func CurrentHead(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string) (head Head, ok bool, err error) {
	var (
		sequence  int64
		chainHash string
		algorithm string
	)
	row := q.QueryRow(ctx, `
		SELECT sequence, chain_hash, chain_algorithm
		FROM ledger_hash_chain_link
		WHERE tenant_id = $1 AND stream_key = $2
		ORDER BY sequence DESC
		LIMIT 1`, tenant, streamKey)
	if scanErr := row.Scan(&sequence, &chainHash, &algorithm); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return Head{}, false, nil
		}
		return Head{}, false, fmt.Errorf("hashchain: read head for stream %s: %w", streamKey, scanErr)
	}
	return Head{StreamKey: streamKey, Sequence: sequence, ChainHash: chainHash, Algorithm: algorithm}, true, nil
}

// Verify reads a stream's events and recorded chain links back from
// PostgreSQL and folds them through [VerifyLinks]. It is read-only: it
// performs no write of any kind, so it is safe to run continuously as a
// background integrity check (specs/transaction-ledger-reconciliation-and-repair.md
// 8.12) without competing with appenders for locks.
func (d *Digester) Verify(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string) (Head, error) {
	events, err := ReadEventDigests(ctx, q, tenant, streamKey)
	if err != nil {
		return Head{}, err
	}
	links, err := ReadLinks(ctx, q, tenant, streamKey)
	if err != nil {
		return Head{}, err
	}
	return d.VerifyLinks(streamKey, events, links)
}
