// Package pgxadapter is the single place where pgx/v5 becomes an
// [dbport] implementation (owner: data plane; LIB-004).
//
// Every other package speaks the driver-free port: it takes a [dbport.Tx] or a
// [dbport.Querier] and never names pgx in a signature. This package holds the
// driver handles - a connection, a pool, a transaction - and translates the
// three differences between pgx's shape and the port's:
//
//   - Exec returns pgx's command tag; the port returns the affected-row count,
//     which is the only field this repository reads.
//   - A single-row miss is pgx.ErrNoRows; the port's own [dbport.ErrNoRows] is
//     substituted at Scan so no caller imports pgx to recognise it.
//   - pgx's Rows already matches the port's cursor shape, so it is passed
//     through unchanged rather than re-wrapped per row.
//
// Constructors take a connection URL rather than an already-open pgx handle.
// That is deliberate: a constructor accepting *pgx.Conn would put the driver
// type back on this package's public surface and re-open the boundary this
// package exists to close.
package pgxadapter

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// row adapts a deferred pgx single-row result, substituting the port's own
// no-rows sentinel.
type row struct{ inner pgx.Row }

func (r row) Scan(dest ...any) error {
	err := r.inner.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbport.ErrNoRows
	}
	return err
}

// Tx is an open transaction on a pgx connection or pool. It satisfies
// [dbport.Tx]; the pgx transaction it holds is never handed out.
type Tx struct{ tx pgx.Tx }

// Exec implements [dbport.Execer].
func (t Tx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := t.tx.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

// Query implements [dbport.Querier].
func (t Tx) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

// QueryRow implements [dbport.Querier].
func (t Tx) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	return row{inner: t.tx.QueryRow(ctx, sql, args...)}
}

// Commit implements [dbport.Tx].
func (t Tx) Commit(ctx context.Context) error { return t.tx.Commit(ctx) }

// Rollback implements [dbport.Tx]. Rolling back a transaction that already
// committed reports pgx.ErrTxClosed, which callers guarding with a deferred
// rollback ignore.
func (t Tx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

// Conn is a single pgx connection exposed through the port. It is not safe for
// concurrent use; open one per goroutine, or use a [Pool].
type Conn struct{ conn *pgx.Conn }

// Connect opens one connection to url. runtimeParams are PostgreSQL run-time
// parameters set on the connection itself (search_path, for instance), which is
// how a caller pins a session to a schema without issuing a statement.
func Connect(ctx context.Context, url string, runtimeParams map[string]string) (*Conn, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	for k, v := range runtimeParams {
		cfg.RuntimeParams[k] = v
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Conn{conn: conn}, nil
}

// Exec implements [dbport.Execer].
func (c *Conn) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := c.conn.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

// Query implements [dbport.Querier].
func (c *Conn) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	return c.conn.Query(ctx, sql, args...)
}

// QueryRow implements [dbport.Querier].
func (c *Conn) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	return row{inner: c.conn.QueryRow(ctx, sql, args...)}
}

// Begin implements [dbport.Beginner].
func (c *Conn) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := c.conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return Tx{tx: tx}, nil
}

// Ping verifies the connection is still usable.
func (c *Conn) Ping(ctx context.Context) error { return c.conn.Ping(ctx) }

// Close closes the connection.
func (c *Conn) Close(ctx context.Context) error { return c.conn.Close(ctx) }

// Pool is a pgx connection pool exposed through the port. Unlike [Conn] it is
// safe for concurrent use, and every Exec/Query/QueryRow acquires and releases
// a connection of its own.
type Pool struct{ pool *pgxpool.Pool }

// NewPool opens a pool against url and pings it once, so a bad connection
// string or an unreachable server fails here rather than on first use.
// runtimeParams are PostgreSQL run-time parameters set on every connection the
// pool opens, which is how a caller pins the whole pool to a schema.
func NewPool(ctx context.Context, url string, runtimeParams map[string]string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	for k, v := range runtimeParams {
		cfg.ConnConfig.RuntimeParams[k] = v
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Pool{pool: pool}, nil
}

// Exec implements [dbport.Execer].
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := p.pool.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

// Query implements [dbport.Querier].
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow implements [dbport.Querier].
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	return row{inner: p.pool.QueryRow(ctx, sql, args...)}
}

// Begin implements [dbport.Beginner].
func (p *Pool) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return Tx{tx: tx}, nil
}

// Ping verifies the pool can still reach the server.
func (p *Pool) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// Close closes the pool and every connection in it.
func (p *Pool) Close() { p.pool.Close() }

// Saturation is a snapshot of the pool's connection accounting, reported
// without handing out pgxpool's own stat type.
type Saturation struct {
	AcquiredConns int32
	IdleConns     int32
	MaxConns      int32
	TotalConns    int32
}

// Stats reports the pool's current saturation.
func (p *Pool) Stats() Saturation {
	s := p.pool.Stat()
	return Saturation{
		AcquiredConns: s.AcquiredConns(),
		IdleConns:     s.IdleConns(),
		MaxConns:      s.MaxConns(),
		TotalConns:    s.TotalConns(),
	}
}

var (
	_ dbport.Tx       = Tx{}
	_ dbport.Conn     = (*Conn)(nil)
	_ dbport.Beginner = (*Conn)(nil)
	_ dbport.Conn     = (*Pool)(nil)
	_ dbport.Beginner = (*Pool)(nil)
)
