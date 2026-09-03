package tenancy_test

import (
	"context"
	"slices"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

// rlsGovernedRelations are every relation migration 00008 must enable and
// force row level security on: the twelve tenant-scoped base tables
// migrations 00001..00006 declare, plus ledger_event's four hash partitions
// (which do not inherit the parent relation's row security setting), plus
// external_observation and observation_checkpoint -- added later by
// migrations/00007_connectivity.sql, tenant-scoped the same way as everything
// else here.
var rlsGovernedRelations = []string{
	"tenant",
	"definition_version",
	"definition_active_pointer",
	"intent_instance",
	"proposal_revision",
	"payload_schema",
	"authority_assignment",
	"ledger_stream",
	"stream_head",
	"ledger_event",
	"ledger_event_p0",
	"ledger_event_p1",
	"ledger_event_p2",
	"ledger_event_p3",
	"projection_checkpoint",
	"outbox",
	"external_observation",
	"observation_checkpoint",
}

// TestTodo_DB_017_Integration proves the full migration tree leaves the
// database in the shape DB-017 promises: every governed relation actually has
// row level security enabled and forced, the app role's grants never include
// DELETE anywhere, and a query that names a partition directly is exactly as
// governed as one that goes through the partitioned parent.
func TestTodo_DB_017_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	t.Run("every governed relation has row level security enabled and forced", func(t *testing.T) {
		for _, rel := range rlsGovernedRelations {
			var enabled, forced bool
			err := db.Conn.QueryRow(ctx, `
				SELECT relrowsecurity, relforcerowsecurity
				FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = current_schema() AND c.relname = $1`, rel).Scan(&enabled, &forced)
			if err != nil {
				t.Fatalf("read pg_class for %s: %v", rel, err)
			}
			if !enabled {
				t.Errorf("%s does not have row level security enabled", rel)
			}
			if !forced {
				t.Errorf("%s does not force row level security", rel)
			}
		}
	})

	t.Run("the app role is never granted DELETE anywhere", func(t *testing.T) {
		rows, err := db.Conn.Query(ctx, `
			SELECT table_name, privilege_type FROM information_schema.role_table_grants
			WHERE grantee = $1 AND table_schema = current_schema()`, tenancy.AppRole)
		if err != nil {
			t.Fatalf("read role_table_grants: %v", err)
		}
		defer rows.Close()

		var seen int
		var deleteGrants []string
		for rows.Next() {
			var table, privilege string
			if err := rows.Scan(&table, &privilege); err != nil {
				t.Fatalf("scan grant: %v", err)
			}
			seen++
			if privilege == "DELETE" {
				deleteGrants = append(deleteGrants, table)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate grants: %v", err)
		}
		if seen == 0 {
			t.Fatal("hcmnext_app holds no table grants at all; the fixture query found nothing")
		}
		if len(deleteGrants) > 0 {
			t.Fatalf("hcmnext_app was granted DELETE on %v; this data plane has no delete semantics", deleteGrants)
		}
	})

	t.Run("append-only tables are never granted UPDATE for the app role", func(t *testing.T) {
		for _, table := range []string{"definition_version", "proposal_revision", "ledger_event", "external_observation"} {
			var count int
			if err := db.Conn.QueryRow(ctx, `
				SELECT count(*) FROM information_schema.role_table_grants
				WHERE grantee = $1 AND table_schema = current_schema()
				  AND table_name = $2 AND privilege_type = 'UPDATE'`,
				tenancy.AppRole, table).Scan(&count); err != nil {
				t.Fatalf("check UPDATE grant on %s: %v", table, err)
			}
			if count != 0 {
				t.Fatalf("hcmnext_app was granted UPDATE on append-only table %s", table)
			}
		}
	})

	t.Run("a query naming a hash partition directly is governed the same as the parent", func(t *testing.T) {
		tenantA := insertTenant(t, db, "tenant-a")
		insertLedgerEventFixture(t, db, tenantA, "a-payload")

		app := appRoleConn(t, db)
		tx, err := app.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantA); err != nil {
			t.Fatalf("scope: %v", err)
		}

		var viaParent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ledger_event`).Scan(&viaParent); err != nil {
			t.Fatalf("count via parent: %v", err)
		}
		if viaParent != 1 {
			t.Fatalf("querying the parent saw %d rows, want 1", viaParent)
		}

		var viaPartitions int
		for _, partition := range []string{"ledger_event_p0", "ledger_event_p1", "ledger_event_p2", "ledger_event_p3"} {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+partition).Scan(&n); err != nil {
				t.Fatalf("count via %s: %v", partition, err)
			}
			viaPartitions += n
		}
		if viaPartitions != viaParent {
			t.Fatalf("summed partition counts are %d, parent count is %d; a direct partition query is not equally governed",
				viaPartitions, viaParent)
		}
	})

	t.Run("the governed base tables are a superset of the frozen schema suite's tenant-scoped list", func(t *testing.T) {
		// internal/data/schema/schema_test.go's tenantScopedTables was the
		// authoritative list this migration was drafted against; it cannot be
		// imported here (its tests live in an internal _test package), so it
		// is reproduced locally. migrations/00007_connectivity.sql landed
		// after that draft and added two more tenant-scoped tables that this
		// frozen list does not yet name (internal/data/schema is out of this
		// migration's lane to edit), so this checks "every frozen-list table
		// is governed" and "the only extras are the known, accounted-for
		// ones" rather than exact equality.
		frozen := []string{
			"authority_assignment",
			"definition_active_pointer",
			"definition_version",
			"intent_instance",
			"ledger_event",
			"ledger_stream",
			"outbox",
			"payload_schema",
			"projection_checkpoint",
			"proposal_revision",
			"stream_head",
			"tenant",
		}
		knownUnlistedExtras := []string{"external_observation", "observation_checkpoint"}

		var got []string
		for _, rel := range rlsGovernedRelations {
			if !isPartition(rel) {
				got = append(got, rel)
			}
		}
		want := append(slices.Clone(frozen), knownUnlistedExtras...)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("governed base tables are %v, want %v", got, want)
		}
	})
}

func isPartition(relname string) bool {
	switch relname {
	case "ledger_event_p0", "ledger_event_p1", "ledger_event_p2", "ledger_event_p3":
		return true
	default:
		return false
	}
}
