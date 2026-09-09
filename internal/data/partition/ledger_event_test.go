package partition_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/partition"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// digest64 returns an exactly-64-hex-character content_digest domain value
// (migrations/00002_tenant_primitives.sql: `VALUE ~ '^[0-9a-f]{64}$'`), one
// distinguishable literal per test fixture, without hand-counting characters
// at every call site.
func digest64(hexDigit byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = hexDigit
	}
	return string(b)
}

// TestTodo_DATA_017 is DATA-017's primary proof: partitioning ledger_event
// (migrations/00005_ledger.sql, PARTITION BY HASH (tenant_id), four
// partitions) is invisible to every ordinary caller. It builds a
// non-partitioned shadow copy of ledger_event with the identical rows and
// shows, against a real embedded PostgreSQL server, that:
//
//   - an unrestricted scan returns the same rows through the partitioned
//     parent as through the shadow (partition pruning omits nothing);
//   - a tenant-scoped read returns that tenant's complete row set on both,
//     for tenants that hash to different physical partitions;
//   - reading a tenant's rows via the parent and via that tenant's own
//     physical partition, directly, returns identical row identity (event
//     id, stream key, sequence, digest) -- a row's identity does not change
//     depending on which relation a query happens to name;
//   - a transaction that writes to two tenants which hash to different
//     partitions is still all-or-nothing: rolling it back leaves neither
//     tenant's row behind, committing it leaves both, exactly as the same
//     two statements against the non-partitioned shadow would.
func TestTodo_DATA_017(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	kit := partition.Kit{Table: "ledger_event", ShadowTable: "ledger_event_shadow"}

	tenantA := insertTenant(t, db, "a")
	tenantB := insertTenant(t, db, "b")
	schemaA, authA := insertLedgerPrereqs(t, db, tenantA, "worker:a")
	schemaB, authB := insertLedgerPrereqs(t, db, tenantB, "worker:b")
	insertLedgerEvent(t, db, tenantA, "worker:a", schemaA, authA, 1, "idem-a-1")
	insertLedgerEvent(t, db, tenantA, "worker:a", schemaA, authA, 2, "idem-a-2")
	insertLedgerEvent(t, db, tenantB, "worker:b", schemaB, authB, 1, "idem-b-1")

	kit.Build(t, ctx, db.Conn, tenancy.AppRole)
	kit.Load(t, ctx, db.Conn)

	t.Run("an unrestricted scan omits nothing: partitioned parent and shadow agree row for row", func(t *testing.T) {
		kit.CompareRows(t, ctx, db.Conn, "full scan",
			`SELECT tenant_id, stream_key, sequence, event_id, digest, occurred_at, effective_at, idempotency_key
			 FROM {table} ORDER BY tenant_id, stream_key, sequence`)
	})

	t.Run("a tenant-scoped read through the app role returns the complete authorized set on both", func(t *testing.T) {
		app := appRoleConn(t, db)
		tx := scopedTx(t, ctx, app, tenantA)
		defer func() { _ = tx.Rollback(ctx) }()
		kit.CompareRows(t, ctx, tx, "tenant A scoped scan",
			`SELECT stream_key, sequence, event_id, idempotency_key FROM {table} ORDER BY sequence`)
	})

	t.Run("a row's identity is the same whether read through the parent or through its own physical partition", func(t *testing.T) {
		part := partitionOf(t, ctx, db.Conn, tenantA)
		directKit := partition.Kit{Table: "ledger_event", ShadowTable: part}
		directKit.CompareRows(t, ctx, db.Conn, "tenant A via parent vs via its own partition",
			`SELECT tenant_id, stream_key, sequence, event_id, digest, idempotency_key
			 FROM {table} WHERE tenant_id = $1 ORDER BY sequence`, tenantA)
	})

	t.Run("a transaction spanning two differently-hashed tenants is all-or-nothing, on both", func(t *testing.T) {
		tenantX, tenantY := pickTwoTenantsInDifferentPartitions(t, ctx, db)
		schemaX, authX := insertLedgerPrereqs(t, db, tenantX, "cross:x")
		schemaY, authY := insertLedgerPrereqs(t, db, tenantY, "cross:y")

		insertBoth := func(ctx context.Context, exec interface {
			Exec(ctx context.Context, sql string, args ...any) (int64, error)
		}) {
			const stmt = `
				INSERT INTO %s (
					tenant_id, stream_key, sequence, event_id, assertion_class, authority_ref,
					source_ref, schema_ref, payload, canonical_length, digest, digest_algorithm,
					occurred_at, effective_at, correlation_id, idempotency_key)
				VALUES ($1, $2, 1, $3, 'DOMAIN_FACT', $4, 'test', $5, $6, $7, $8, 'sha256',
					timestamptz '2026-02-01T00:00:00Z', timestamptz '2026-02-01T00:00:00Z', $3, $9)`
			// event id is generated once per logical row, outside the table
			// loop, and reused for both tables: CompareRows below asserts
			// row-for-row identity between the partitioned parent and the
			// shadow, which requires the same event_id to land in both, not
			// two independently random ones for what is meant to be one row
			// written twice.
			rows := []struct {
				tenant  uuid.UUID
				stream  string
				schema  string
				auth    string
				idem    string
				eventID uuid.UUID
			}{
				{tenantX, "cross:x", schemaX, authX, "cross-x", uuid.New()},
				{tenantY, "cross:y", schemaY, authY, "cross-y", uuid.New()},
			}
			for _, table := range []string{"ledger_event", "ledger_event_shadow"} {
				for _, row := range rows {
					sql := fmt.Sprintf(stmt, table)
					if _, err := exec.Exec(ctx, sql,
						row.tenant, row.stream, row.eventID, row.auth, row.schema,
						[]byte("x"), len("x"), digest64('9'), row.idem); err != nil {
						t.Fatalf("insert into %s for %s: %v", table, row.stream, err)
					}
				}
			}
		}

		countCross := func(t *testing.T, table string) int {
			t.Helper()
			var n int
			sql := fmt.Sprintf(`SELECT count(*) FROM %s WHERE (tenant_id = $1 AND stream_key = 'cross:x')
				OR (tenant_id = $2 AND stream_key = 'cross:y')`, table)
			if err := db.Conn.QueryRow(ctx, sql, tenantX, tenantY).Scan(&n); err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
			return n
		}

		t.Run("rollback leaves neither tenant's row on either table", func(t *testing.T) {
			tx, err := db.Conn.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			insertBoth(ctx, tx)
			if err := tx.Rollback(ctx); err != nil {
				t.Fatalf("rollback: %v", err)
			}
			if n := countCross(t, "ledger_event"); n != 0 {
				t.Fatalf("ledger_event holds %d cross-partition rows after rollback, want 0", n)
			}
			if n := countCross(t, "ledger_event_shadow"); n != 0 {
				t.Fatalf("ledger_event_shadow holds %d cross-partition rows after rollback, want 0", n)
			}
		})

		t.Run("commit leaves both tenants' rows on both tables, spanning two physical partitions", func(t *testing.T) {
			tx, err := db.Conn.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			insertBoth(ctx, tx)
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit: %v", err)
			}
			if n := countCross(t, "ledger_event"); n != 2 {
				t.Fatalf("ledger_event holds %d cross-partition rows after commit, want 2", n)
			}
			if n := countCross(t, "ledger_event_shadow"); n != 2 {
				t.Fatalf("ledger_event_shadow holds %d cross-partition rows after commit, want 2", n)
			}

			partX := partitionOf(t, ctx, db.Conn, tenantX)
			partY := partitionOf(t, ctx, db.Conn, tenantY)
			if partX == partY {
				t.Fatalf("tenants X (%s) and Y (%s) landed in the same partition %s; this subtest needs two different ones", tenantX, tenantY, partX)
			}

			kit.CompareRows(t, ctx, db.Conn, "cross-partition committed rows",
				`SELECT tenant_id, stream_key, sequence, event_id, idempotency_key
				 FROM {table} WHERE stream_key IN ('cross:x', 'cross:y') ORDER BY tenant_id, stream_key`)
		})
	})
}

// TestTodo_DATA_017_Security is the matrix's SECURITY case: row level
// security denies exactly the same rows on the partitioned parent and on
// the non-partitioned shadow, and every one of ledger_event's four physical
// partitions actually carries its own copy of the tenant_isolation policy
// migrations/00008_tenant_isolation.sql declares -- not merely the parent
// relation a query is normally written against.
func TestTodo_DATA_017_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	kit := partition.Kit{Table: "ledger_event", ShadowTable: "ledger_event_shadow"}

	tenantA := insertTenant(t, db, "a")
	tenantB := insertTenant(t, db, "b")
	schemaA, authA := insertLedgerPrereqs(t, db, tenantA, "worker:a")
	schemaB, authB := insertLedgerPrereqs(t, db, tenantB, "worker:b")
	insertLedgerEvent(t, db, tenantA, "worker:a", schemaA, authA, 1, "idem-a-1")
	insertLedgerEvent(t, db, tenantB, "worker:b", schemaB, authB, 1, "idem-b-1")

	kit.Build(t, ctx, db.Conn, tenancy.AppRole)
	kit.Load(t, ctx, db.Conn)

	t.Run("every physical partition carries the parent's own tenant_isolation policy, verbatim", func(t *testing.T) {
		kit.CheckPartitionPolicies(t, ctx, db.Conn)
	})

	t.Run("an unscoped app-role connection sees zero rows on both, not just the parent", func(t *testing.T) {
		app := appRoleConn(t, db)
		kit.CompareRows(t, ctx, app, "unscoped count", `SELECT count(*) AS n FROM {table}`)
	})

	t.Run("a tenant-scoped read returns only that tenant's rows on both", func(t *testing.T) {
		app := appRoleConn(t, db)
		tx := scopedTx(t, ctx, app, tenantA)
		defer func() { _ = tx.Rollback(ctx) }()
		kit.CompareRows(t, ctx, tx, "tenant A scoped read", `SELECT stream_key, sequence FROM {table} ORDER BY sequence`)
	})

	t.Run("a forged cross-tenant insert is refused identically on both, citing row level security", func(t *testing.T) {
		// Two independent scoped transactions, not one shared between the
		// two targets: the first refusal aborts whichever transaction it ran
		// on, so the second target needs a transaction of its own to prove
		// anything about its own refusal wording rather than inheriting
		// "transaction is aborted" from the first.
		app := appRoleConn(t, db)
		txParent := scopedTx(t, ctx, app, tenantA)
		defer func() { _ = txParent.Rollback(ctx) }()
		txShadow := scopedTx(t, ctx, appRoleConn(t, db), tenantA)
		defer func() { _ = txShadow.Rollback(ctx) }()

		const forgedInsert = `
			INSERT INTO {table} (
				tenant_id, stream_key, sequence, event_id, assertion_class, authority_ref,
				source_ref, schema_ref, payload, canonical_length, digest, digest_algorithm,
				occurred_at, effective_at, correlation_id, idempotency_key)
			VALUES ($1, 'worker:b', 99, gen_random_uuid(), 'DOMAIN_FACT', $2,
				'test', $3, $4, $5, $6, 'sha256',
				timestamptz '2026-02-01T00:00:00Z', timestamptz '2026-02-01T00:00:00Z', gen_random_uuid(), 'forged')`
		kit.CompareRefusal(t, ctx, txParent, txShadow, "forged cross-tenant insert", "row-level security", forgedInsert,
			tenantB, authB, schemaB, []byte("x"), len("x"), digest64('2'))
	})
}

// TestTodo_DATA_017_Mutation is the matrix's MUTATION case: the append-only
// forbid_mutation trigger refuses UPDATE and DELETE with the same wording on
// the partitioned parent, on the non-partitioned shadow, and on a physical
// partition addressed directly -- and every physical partition actually
// carries the trigger (PostgreSQL clones a partitioned parent's triggers
// automatically, so this is expected to hold, but DATA-017 exists to verify
// that against a real database rather than take it on faith).
func TestTodo_DATA_017_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	kit := partition.Kit{Table: "ledger_event", ShadowTable: "ledger_event_shadow"}

	tenantA := insertTenant(t, db, "a")
	schemaA, authA := insertLedgerPrereqs(t, db, tenantA, "worker:a")
	insertLedgerEvent(t, db, tenantA, "worker:a", schemaA, authA, 1, "idem-a-1")

	kit.Build(t, ctx, db.Conn, tenancy.AppRole)
	kit.Load(t, ctx, db.Conn)

	t.Run("every physical partition carries the parent's append-only trigger", func(t *testing.T) {
		kit.CheckPartitionTriggers(t, ctx, db.Conn)
	})

	t.Run("UPDATE is refused identically, citing append-only", func(t *testing.T) {
		kit.CompareRefusal(t, ctx, db.Conn, db.Conn, "update", "is append-only",
			`UPDATE {table} SET canonical_length = canonical_length + 1 WHERE tenant_id = $1`, tenantA)
	})

	t.Run("DELETE is refused identically, citing append-only", func(t *testing.T) {
		kit.CompareRefusal(t, ctx, db.Conn, db.Conn, "delete", "is append-only",
			`DELETE FROM {table} WHERE tenant_id = $1`, tenantA)
	})

	t.Run("UPDATE addressed directly at one physical partition is refused the same way", func(t *testing.T) {
		part := partitionOf(t, ctx, db.Conn, tenantA)
		directKit := partition.Kit{Table: "ledger_event", ShadowTable: part}
		directKit.CompareRefusal(t, ctx, db.Conn, db.Conn, "update via partition", "is append-only",
			`UPDATE {table} SET canonical_length = canonical_length + 1 WHERE tenant_id = $1`, tenantA)
	})
}
