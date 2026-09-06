package pgxadapter_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
)

// TestPoolReusesAConnectionAcrossItsOwnHygiene pins the interaction between
// NewPool's BeforeAcquire hygiene and pgx's client-side statement cache.
//
// DISCARD ALL deallocates every server-side prepared statement on the
// session. pgx's default query mode (QueryExecModeCacheStatement) prepares
// each distinct SQL text once per connection under a generated name and
// remembers it client-side, so the second time the same connection is
// acquired the client binds a statement the server no longer has and
// PostgreSQL answers SQLSTATE 26000 ("prepared statement ... does not
// exist"). The live cell hit exactly this on its second read of an intent.
//
// With one connection in the pool every query after the first reuses the
// same session, so the failure is deterministic rather than a race.
func TestPoolReusesAConnectionAcrossItsOwnHygiene(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()

	pool, err := pgxadapter.NewPool(ctx, db.URL+"&pool_max_conns=1", map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `CREATE TABLE hygiene_probe (id int PRIMARY KEY, label text NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO hygiene_probe (id, label) VALUES ($1, $2)`, 1, "first"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// The same parameterised text, on the same (only) connection, across
	// three separate acquisitions. Each acquisition runs DISCARD ALL first.
	for attempt := 1; attempt <= 3; attempt++ {
		var label string
		if err := pool.QueryRow(ctx, `SELECT label FROM hygiene_probe WHERE id = $1`, 1).Scan(&label); err != nil {
			t.Fatalf("attempt %d: the pool could not reuse its own connection after hygiene: %v", attempt, err)
		}
		if label != "first" {
			t.Fatalf("attempt %d: label = %q, want first", attempt, label)
		}
	}

	// A transaction is the other shape every store uses: SET LOCAL context,
	// a read, a commit, then the same read again on the recycled session.
	for attempt := 1; attempt <= 2; attempt++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("attempt %d: begin: %v", attempt, err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM hygiene_probe WHERE label = $1`, "first").Scan(&n); err != nil {
			t.Fatalf("attempt %d: query in tx: %v", attempt, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("attempt %d: commit: %v", attempt, err)
		}
		if n != 1 {
			t.Fatalf("attempt %d: count = %d, want 1", attempt, n)
		}
	}
	if got := pool.HygieneFailures(); got != 0 {
		t.Fatalf("hygiene failures = %d, want 0", got)
	}
	// The one connection must have survived every acquisition: a hygiene
	// step that fails makes pgxpool destroy the session and open another,
	// which hides the defect behind connection churn.
	if stats := pool.Stats(); stats.TotalConns != 1 {
		t.Fatalf("total connections = %d, want the single pooled session reused throughout", stats.TotalConns)
	}
}
