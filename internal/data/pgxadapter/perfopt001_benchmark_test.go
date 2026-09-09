package pgxadapter_test

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

var perfoptBenchmarkURL string

func TestTodo_PERFOPT_001_BenchmarkFixture(t *testing.T) {
	db := pgtest.New(t)
	perfoptBenchmarkURL = db.URL
}

// BenchmarkTodo_PERFOPT_001 records the cost of the two pool surfaces that
// pay BeforeAcquire hygiene: an empty checkout and one-row query.
func BenchmarkTodo_PERFOPT_001(b *testing.B) {
	url := perfoptBenchmarkURL
	if url == "" {
		url = os.Getenv(pgtest.EnvDatabaseURL)
	}
	if url == "" {
		b.Skip("run the benchmark with TestTodo_PERFOPT_001_BenchmarkFixture or HCMNEXT_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxadapter.NewPool(ctx, url+"&pool_max_conns=1", map[string]string{"search_path": "public"})
	if err != nil {
		b.Fatalf("NewPool: %v", err)
	}
	b.Cleanup(pool.Close)
	legacy, err := legacyPool(ctx, url+"&pool_max_conns=1")
	if err != nil {
		b.Fatalf("legacy pool: %v", err)
	}
	b.Cleanup(legacy.Close)

	benchmarkAcquire := func(b *testing.B, acquire func() error) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := acquire(); err != nil {
				b.Fatal(err)
			}
		}
	}
	benchmarkQueryRow := func(b *testing.B, queryRow func() error) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := queryRow(); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.Run("Before_AcquireRelease", func(b *testing.B) {
		benchmarkAcquire(b, func() error {
			conn, err := legacy.Acquire(ctx)
			if err != nil {
				return err
			}
			conn.Release()
			return nil
		})
	})
	b.Run("After_AcquireRelease", func(b *testing.B) {
		benchmarkAcquire(b, func() error {
			return pool.WithConn(ctx, func(dbport.Conn) error { return nil })
		})
	})

	b.Run("Before_QueryRow", func(b *testing.B) {
		benchmarkQueryRow(b, func() error {
			var got int
			if err := legacy.QueryRow(ctx, "SELECT 1").Scan(&got); err != nil {
				return err
			}
			if got != 1 {
				return fmt.Errorf("SELECT 1 = %d, want 1", got)
			}
			return nil
		})
	})
	b.Run("After_QueryRow", func(b *testing.B) {
		benchmarkQueryRow(b, func() error {
			var got int
			if err := pool.QueryRow(ctx, "SELECT 1").Scan(&got); err != nil {
				return err
			}
			if got != 1 {
				return fmt.Errorf("SELECT 1 = %d, want 1", got)
			}
			return nil
		})
	})
}

func legacyPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	var failures atomic.Int64
	cfg.BeforeAcquire = func(ctx context.Context, conn *pgx.Conn) bool {
		for _, statement := range []string{"RESET ROLE", "SELECT pg_advisory_unlock_all()", "DISCARD ALL"} {
			if _, err := conn.Exec(ctx, statement, pgx.QueryExecModeSimpleProtocol); err != nil {
				failures.Add(1)
				return false
			}
		}
		if _, err := conn.Exec(ctx, "SELECT set_config($1, $2, false)", pgx.QueryExecModeExec, "search_path", "public"); err != nil {
			failures.Add(1)
			return false
		}
		return true
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}
