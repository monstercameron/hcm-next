package pgxadapter

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

type batchResults struct{ inner pgx.BatchResults }

type batchQueue struct{ inner *pgx.Batch }

func (b batchQueue) Queue(sql string, args ...any) { b.inner.Queue(sql, args...) }

func (r batchResults) Exec() (int64, error) {
	tag, err := r.inner.Exec()
	return tag.RowsAffected(), err
}

func (r batchResults) QueryRow() dbport.Row { return row{inner: r.inner.QueryRow()} }

func (r batchResults) Close() error { return r.inner.Close() }

// SendBatch implements the optional dbport batching capability for a single
// connection. pgx sends the queued commands in one protocol exchange and
// exposes their results in queue order.
func (c *Conn) SendBatch(ctx context.Context, fn func(dbport.Batch)) (dbport.BatchResults, error) {
	var batch pgx.Batch
	fn(batchQueue{inner: &batch})
	return batchResults{inner: c.conn.SendBatch(ctx, &batch)}, nil
}

// SendBatch implements the optional dbport batching capability for an open
// transaction. All queued commands remain part of the caller's transaction.
func (t Tx) SendBatch(ctx context.Context, fn func(dbport.Batch)) (dbport.BatchResults, error) {
	var batch pgx.Batch
	fn(batchQueue{inner: &batch})
	return batchResults{inner: t.tx.SendBatch(ctx, &batch)}, nil
}

var (
	_ dbport.Batcher = (*Conn)(nil)
	_ dbport.Batcher = Tx{}
)
