package bootstrap

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBPool is the narrow database-pool port bootstrap depends on. Production
// code uses PgxPoolFactory (backed by pgx/v5's pgxpool.Pool, matching
// cmd/worker's and cmd/projector's existing convention); tests use
// NewFakeDBPool so no suite in this repository needs a real PostgreSQL
// server to exercise bootstrap's wiring.
type DBPool interface {
	Ping(ctx context.Context) error
	Close()
}

// DBPoolFactory constructs a DBPool for the given connection URL. Spec
// calls it (once) during Run, before any workload starts, so a connection
// failure is a startup failure rather than something a workload discovers
// on its own.
type DBPoolFactory func(ctx context.Context, url string) (DBPool, error)

// pgxPoolAdapter adapts *pgxpool.Pool to DBPool. pgxpool.Pool already has
// exactly this Ping/Close shape; the adapter exists only so this package's
// exported surface names its own interface rather than pgxpool's.
type pgxPoolAdapter struct{ pool *pgxpool.Pool }

func (a pgxPoolAdapter) Ping(ctx context.Context) error { return a.pool.Ping(ctx) }
func (a pgxPoolAdapter) Close()                         { a.pool.Close() }

// PgxPoolFactory is the default DBPoolFactory: it opens a pgxpool.Pool
// against url and pings it once so a bad connection string or unreachable
// server fails Run before any workload starts, then closes the pool it
// opened rather than leaking it.
func PgxPoolFactory(ctx context.Context, url string) (DBPool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pgxPoolAdapter{pool: pool}, nil
}

// FakeDBPool is an in-memory DBPool for tests: this package's own, and any
// downstream composition-root command's (cmd/worker, cmd/projector,
// cmd/hcmnext, cmd/migrate) tests that exercise bootstrap.Run without a
// real PostgreSQL server.
type FakeDBPool struct {
	// PingErr, if non-nil, is returned by every Ping call.
	PingErr error

	mu     sync.Mutex
	pings  int
	closed atomic.Bool
}

// NewFakeDBPool returns a FakeDBPool whose Ping succeeds until PingErr is
// set on the returned value.
func NewFakeDBPool() *FakeDBPool { return &FakeDBPool{} }

// NewFakeDBPoolFactory returns a DBPoolFactory that mirrors
// PgxPoolFactory's contract — it pings once before returning, surfacing
// pool.PingErr as the factory's own error — but always resolves to pool,
// ignoring the requested URL. Callers inspect pool's own counters
// (Pings, Closed) to assert on factory behavior.
func NewFakeDBPoolFactory(pool *FakeDBPool) DBPoolFactory {
	return func(ctx context.Context, _ string) (DBPool, error) {
		if err := pool.Ping(ctx); err != nil {
			return nil, err
		}
		return pool, nil
	}
}

func (f *FakeDBPool) Ping(_ context.Context) error {
	f.mu.Lock()
	f.pings++
	f.mu.Unlock()
	return f.PingErr
}

func (f *FakeDBPool) Close() { f.closed.Store(true) }

// Closed reports whether Close has been called.
func (f *FakeDBPool) Closed() bool { return f.closed.Load() }

// Pings reports how many times Ping has been called.
func (f *FakeDBPool) Pings() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pings
}
