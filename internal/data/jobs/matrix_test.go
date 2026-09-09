package jobs_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// TestTodo_JOB_001 walks the full governed lifecycle -- publish, declare,
// begin, partition, claim, checkpoint, complete -- across the four tables
// migration 00034 adds, then reopens the database on a fresh connection to
// prove the GREEN clause: attempts, partitions and checkpoints survive
// crash/restart with exact counts and digests. It also proves the RED
// clause directly: a mutable republish, a cross-tenant read and an
// unversioned run are each refused.
func TestTodo_JOB_001(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "job001-primary")
	conn := appConn(t, db)

	def := newDefinition(tenant, "job.reconciliation.workers", 1)
	published := publish(t, ctx, conn, tenant, def)

	// RED: mutable. A second publish of the same identity is refused, not
	// merged or overwritten.
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.DefinitionStore{}).Publish(ctx, tx, published)
		return err
	}); !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("republish: err = %v, want ErrDuplicate", err)
	}

	// RED: unversioned. A run cannot be declared against a job version this
	// tenant never published.
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID: tenant, RunID: uuid.New(), JobID: published.JobID, JobVersion: 99,
			DeclaredBy: "workload:job001", DeclaredAt: fixedInstant,
		})
		return err
	}); err == nil {
		t.Fatal("StartRun against an unpublished version succeeded, want a refusal")
	}

	runID := uuid.New()
	run := declareRun(t, ctx, conn, tenant, published, runID)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		run, err = (jobs.RunStore{}).Begin(ctx, tx, tenant, runID, run.Version, fixedInstant.Add(time.Minute))
		return err
	})

	partitionIDs := [2]uuid.UUID{uuid.New(), uuid.New()}
	keys := [2]string{"shard-0000", "shard-0001"}
	digests := [2]string{digestOf("job001-p0-final"), digestOf("job001-p1-final")}
	for i := range partitionIDs {
		var part jobs.JobPartition
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			part, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
				TenantID: tenant, PartitionID: partitionIDs[i], RunID: runID,
				PartitionKey: keys[i], CreatedAt: fixedInstant,
			})
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			part, err = (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, partitionIDs[i], part.Version, "worker:job001", fixedInstant.Add(2*time.Minute))
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, jobs.JobCheckpoint{
				TenantID: tenant, PartitionID: partitionIDs[i], Sequence: 1,
				StateDigest: digestOf("job001-p" + keys[i] + "-interim"), PartitionVersion: part.Version, TakenAt: fixedInstant.Add(3 * time.Minute),
			})
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			part, err = (jobs.PartitionStore{}).Complete(ctx, tx, tenant, partitionIDs[i], part.Version, fixedInstant.Add(4*time.Minute))
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, jobs.JobCheckpoint{
				TenantID: tenant, PartitionID: partitionIDs[i], Sequence: 2,
				StateDigest: digests[i], PartitionVersion: part.Version, TakenAt: fixedInstant.Add(5 * time.Minute),
			})
			return err
		})
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		run, err = (jobs.RunStore{}).Complete(ctx, tx, tenant, runID, run.Version, fixedInstant.Add(6*time.Minute))
		return err
	})
	if run.State != jobs.RunCompleted {
		t.Fatalf("run state = %s, want COMPLETED", run.State)
	}

	// Simulate crash/restart: a brand new connection, independent of every
	// handle used above, must recover exact counts and digests from durable
	// state alone.
	fresh := appConn(t, db)
	var reloadedRun jobs.JobRun
	var reloadedPartitions []jobs.JobPartition
	inTenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
		var err error
		if reloadedRun, err = (jobs.RunStore{}).Load(ctx, tx, tenant, runID); err != nil {
			return err
		}
		reloadedPartitions, err = (jobs.PartitionStore{}).ListByRun(ctx, tx, tenant, runID)
		return err
	})
	if reloadedRun.State != jobs.RunCompleted || reloadedRun.Attempt != 1 {
		t.Fatalf("reloaded run = %+v, want COMPLETED attempt=1", reloadedRun)
	}
	if len(reloadedPartitions) != 2 {
		t.Fatalf("reloaded partition count = %d, want exactly 2", len(reloadedPartitions))
	}
	for i, part := range reloadedPartitions {
		if part.State != jobs.PartitionCompleted {
			t.Fatalf("partition %s state = %s, want COMPLETED", part.PartitionKey, part.State)
		}
		var checkpoints []jobs.JobCheckpoint
		inTenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
			var err error
			checkpoints, err = (jobs.CheckpointStore{}).List(ctx, tx, tenant, part.PartitionID)
			return err
		})
		if len(checkpoints) != 2 {
			t.Fatalf("partition %s checkpoint count = %d, want exactly 2", part.PartitionKey, len(checkpoints))
		}
		if checkpoints[1].StateDigest != digests[i] {
			t.Fatalf("partition %s final checkpoint digest = %s, want %s", part.PartitionKey, checkpoints[1].StateDigest, digests[i])
		}
	}

	// RED: cross-tenant. A second tenant's connection, scoped to itself, sees
	// none of this tenant's rows even by exact id.
	other := insertTenant(t, db, "job001-primary-other")
	otherConn := appConn(t, db)
	var crossTenantCount int
	inTenantTx(t, otherConn, other, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM job_run WHERE run_id = $1`, runID).Scan(&crossTenantCount)
	})
	if crossTenantCount != 0 {
		t.Fatalf("tenant %s saw tenant %s's run via row level security bypass", other, tenant)
	}
}

// TestTodo_JOB_001_Golden freezes migration 00034's declared shape: the exact
// table set, each table's primary key columns, row level security enabled
// and forced on every table, and the append-only trigger present on exactly
// the two evidence tables.
func TestTodo_JOB_001_Golden(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	expectedPK := map[string][]string{
		"job_definition": {"tenant_id", "job_id", "version"},
		"job_run":        {"tenant_id", "run_id"},
		"job_partition":  {"tenant_id", "partition_id"},
		"job_checkpoint": {"tenant_id", "partition_id", "checkpoint_sequence"},
	}
	appendOnly := map[string]bool{
		"job_definition": true,
		"job_run":        false,
		"job_partition":  false,
		"job_checkpoint": true,
	}

	for table, wantPK := range expectedPK {
		t.Run(table, func(t *testing.T) {
			rows, err := db.Conn.Query(ctx, `
				SELECT kcu.column_name
				FROM information_schema.table_constraints tc
				JOIN information_schema.key_column_usage kcu
					ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
				WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_name = $1 AND tc.table_schema = current_schema()
				ORDER BY kcu.ordinal_position`, table)
			if err != nil {
				t.Fatalf("read primary key of %s: %v", table, err)
			}
			var gotPK []string
			for rows.Next() {
				var col string
				if err := rows.Scan(&col); err != nil {
					t.Fatalf("scan pk column of %s: %v", table, err)
				}
				gotPK = append(gotPK, col)
			}
			rows.Close()
			if len(gotPK) != len(wantPK) {
				t.Fatalf("%s primary key = %v, want %v", table, gotPK, wantPK)
			}
			for i := range wantPK {
				if gotPK[i] != wantPK[i] {
					t.Fatalf("%s primary key = %v, want %v", table, gotPK, wantPK)
				}
			}

			var relrowsecurity, relforcerowsecurity bool
			if err := db.Conn.QueryRow(ctx,
				`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = to_regclass($1)`,
				table).Scan(&relrowsecurity, &relforcerowsecurity); err != nil {
				t.Fatalf("pg_class for %s: %v", table, err)
			}
			if !relrowsecurity || !relforcerowsecurity {
				t.Fatalf("table %s row level security not enabled/forced", table)
			}

			var hasTrigger bool
			if err := db.Conn.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = to_regclass($1) AND tgname = $2)`,
				table, table+"_append_only").Scan(&hasTrigger); err != nil {
				t.Fatalf("pg_trigger for %s: %v", table, err)
			}
			if hasTrigger != appendOnly[table] {
				t.Fatalf("table %s append-only trigger present=%v, want %v", table, hasTrigger, appendOnly[table])
			}
		})
	}
}

// TestTodo_JOB_001_Race drives concurrent ClaimPartition calls against one
// PENDING partition on independent connections: exactly one holder wins, and
// every loser is refused rather than partially applied.
func TestTodo_JOB_001_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "job001-race")
	setupConn := appConn(t, db)
	def := publish(t, ctx, setupConn, tenant, newDefinition(tenant, "job.race.claim", 1))
	runID := uuid.New()
	declareRun(t, ctx, setupConn, tenant, def, runID)

	partitionID := uuid.New()
	var created jobs.JobPartition
	inTenantTx(t, setupConn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: partitionID, RunID: runID,
			PartitionKey: "shard-race", CreatedAt: fixedInstant,
		})
		return err
	})

	const workers = 6
	conns := make([]*pgxadapter.Conn, workers)
	for i := range conns {
		conns[i] = appConn(t, db)
	}
	results := make([]error, workers)
	var startGate, done sync.WaitGroup
	startGate.Add(1)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			startGate.Wait()
			results[i] = inTenantTxErr(conns[i], tenant, func(tx dbport.Tx) error {
				_, err := (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, partitionID, created.Version,
					"worker:race-claimant", fixedInstant)
				return err
			})
		}(i)
	}
	startGate.Done()
	done.Wait()

	wins, conflicts := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, jobs.ErrVersionConflict), errors.Is(err, jobs.ErrIllegalTransition):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1 (conflicts=%d)", wins, conflicts)
	}
	if wins+conflicts != workers {
		t.Fatalf("wins(%d) + conflicts(%d) != workers(%d)", wins, conflicts, workers)
	}

	var holder string
	inTenantTx(t, setupConn, tenant, func(tx dbport.Tx) error {
		p, err := (jobs.PartitionStore{}).Load(ctx, tx, tenant, partitionID)
		holder = p.ClaimedBy
		return err
	})
	if holder != "worker:race-claimant" {
		t.Fatalf("holder = %q, want the single winning claimant", holder)
	}
}

// TestTodo_JOB_001_Fault proves a mid-transaction failure after several
// writes across all four tables leaves nothing durable: the write path never
// partially commits a run, a partition or a checkpoint.
func TestTodo_JOB_001_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "job001-fault")
	conn := appConn(t, db)

	def := newDefinition(tenant, "job.fault.rollback", 1)
	runID := uuid.New()
	partitionID := uuid.New()

	sentinel := errors.New("sink failed after the writes")
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		if _, err := (jobs.DefinitionStore{}).Publish(ctx, tx, def); err != nil {
			return err
		}
		if _, err := (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID: tenant, RunID: runID, JobID: def.JobID, JobVersion: def.Version,
			DeclaredBy: "workload:job001-fault", DeclaredAt: fixedInstant,
		}); err != nil {
			return err
		}
		if _, err := (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: partitionID, RunID: runID,
			PartitionKey: "shard-fault", CreatedAt: fixedInstant,
		}); err != nil {
			return err
		}
		if _, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, jobs.JobCheckpoint{
			TenantID: tenant, PartitionID: partitionID, Sequence: 1,
			StateDigest: digestOf("job001-fault"), PartitionVersion: 1, TakenAt: fixedInstant,
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction returned %v, want the sink failure", err)
	}

	reader := appConn(t, db)
	for _, probe := range []struct {
		name string
		load func(tx dbport.Tx) error
	}{
		{"job_definition", func(tx dbport.Tx) error {
			_, e := (jobs.DefinitionStore{}).Load(ctx, tx, tenant, def.JobID, def.Version)
			return e
		}},
		{"job_run", func(tx dbport.Tx) error {
			_, e := (jobs.RunStore{}).Load(ctx, tx, tenant, runID)
			return e
		}},
		{"job_partition", func(tx dbport.Tx) error {
			_, e := (jobs.PartitionStore{}).Load(ctx, tx, tenant, partitionID)
			return e
		}},
		{"job_checkpoint", func(tx dbport.Tx) error {
			_, e := (jobs.CheckpointStore{}).Latest(ctx, tx, tenant, partitionID)
			return e
		}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			err := inTenantTxErr(reader, tenant, probe.load)
			if !errors.Is(err, jobs.ErrNotFound) {
				t.Fatalf("%s survived a rolled-back transaction: err = %v, want ErrNotFound", probe.name, err)
			}
		})
	}
}

// TestTodo_JOB_001_Security proves tenant isolation and the exact privilege
// boundary migration 00034 grants hcmnext_app: UPDATE only on the live
// tables, never DELETE anywhere, and RLS the app role cannot switch off.
func TestTodo_JOB_001_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantA := insertTenant(t, db, "job001-sec-a")
	tenantB := insertTenant(t, db, "job001-sec-b")
	connA := appConn(t, db)
	defA := publish(t, ctx, connA, tenantA, newDefinition(tenantA, "job.security.a", 1))
	runA := declareRun(t, ctx, connA, tenantA, defA, uuid.New())

	connB := appConn(t, db)
	publish(t, ctx, connB, tenantB, newDefinition(tenantB, "job.security.b", 1))

	t.Run("missing tenant context sees nothing", func(t *testing.T) {
		app := appConn(t, db)
		var count int
		if err := app.QueryRow(ctx, `SELECT count(*) FROM job_definition`).Scan(&count); err != nil {
			t.Fatalf("count without tenant: %v", err)
		}
		if count != 0 {
			t.Fatalf("unscoped app saw %d job definitions, want 0", count)
		}
	})

	t.Run("cross-tenant read returns nothing", func(t *testing.T) {
		app := appConn(t, db)
		tx, err := app.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantB); err != nil {
			t.Fatalf("withTenant: %v", err)
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM job_run WHERE run_id = $1`, runA.RunID).Scan(&count); err != nil {
			t.Fatalf("cross-tenant count: %v", err)
		}
		if count != 0 {
			t.Fatal("tenant B saw tenant A's run")
		}
	})

	t.Run("RLS enforced for app role", func(t *testing.T) {
		var rolsuper, rolbypassrls bool
		if err := db.Conn.QueryRow(ctx, `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = $1`, tenancy.AppRole).Scan(&rolsuper, &rolbypassrls); err != nil {
			t.Fatalf("pg_roles: %v", err)
		}
		if rolsuper || rolbypassrls {
			t.Fatal("app role has superuser or bypassrls")
		}
		app := appConn(t, db)
		if _, err := app.Exec(ctx, `ALTER TABLE job_definition DISABLE ROW LEVEL SECURITY`); err == nil {
			t.Fatal("app role disabled row level security")
		}
	})

	t.Run("append-only tables refuse UPDATE and DELETE for the app role", func(t *testing.T) {
		err := inTenantTxErr(connA, tenantA, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE job_definition SET published_by = 'tampered' WHERE tenant_id = $1 AND job_id = $2 AND version = $3`,
				tenantA, defA.JobID, defA.Version)
			return err
		})
		if err == nil {
			t.Fatal("app role updated job_definition, want a privilege refusal")
		}
		err = inTenantTxErr(connA, tenantA, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM job_definition WHERE tenant_id = $1 AND job_id = $2 AND version = $3`,
				tenantA, defA.JobID, defA.Version)
			return err
		})
		if err == nil {
			t.Fatal("app role deleted job_definition, want a privilege refusal")
		}
	})

	t.Run("live tables allow UPDATE but never DELETE for the app role", func(t *testing.T) {
		err := inTenantTxErr(connA, tenantA, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE job_run SET declared_by = 'still-scoped' WHERE tenant_id = $1 AND run_id = $2`,
				tenantA, runA.RunID)
			return err
		})
		if err != nil {
			t.Fatalf("app role could not update job_run: %v", err)
		}
		err = inTenantTxErr(connA, tenantA, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM job_run WHERE tenant_id = $1 AND run_id = $2`, tenantA, runA.RunID)
			return err
		})
		if err == nil {
			t.Fatal("app role deleted job_run, want a privilege refusal")
		}
	})
}
