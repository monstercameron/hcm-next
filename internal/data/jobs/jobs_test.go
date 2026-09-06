package jobs_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/jobs"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture in this package stamps.
var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// insertTenant registers one active tenant as the migration/admin role.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// digestOf returns a well-formed 64-character hex content digest over seed,
// the plain form migration 00002's content_digest domain requires (no
// "sha256:" prefix -- that prefix belongs to internal/engines/canonicalbytes
// callers, not to this table's own columns).
func digestOf(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// newDefinition builds a well-formed, unpublished JobDefinition for tenant.
func newDefinition(tenant uuid.UUID, jobID string, version uint64) jobs.JobDefinition {
	return jobs.JobDefinition{
		TenantID:                tenant,
		JobID:                   jobID,
		Version:                 version,
		DefinitionDigest:        digestOf(jobID + "/definition"),
		TriggerDigest:           digestOf(jobID + "/trigger"),
		TargetDefinitionRef:     "hcmnext.jobs.import_worker_records",
		TargetDefinitionVersion: 1,
		Body:                    []byte(`{"partitions":2}`),
		PublishedBy:             "workload:jobs-lane-test",
		PublishedAt:             fixedInstant,
	}
}

// publish publishes def and fails the test on error.
func publish(t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenant uuid.UUID, def jobs.JobDefinition) jobs.JobDefinition {
	t.Helper()
	var out jobs.JobDefinition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		out, err = (jobs.DefinitionStore{}).Publish(ctx, tx, def)
		return err
	})
	return out
}

// declareRun starts a run against def and fails the test on error.
func declareRun(t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenant uuid.UUID, def jobs.JobDefinition, runID uuid.UUID) jobs.JobRun {
	t.Helper()
	var out jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		out, err = (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID:   tenant,
			RunID:      runID,
			JobID:      def.JobID,
			JobVersion: def.Version,
			DeclaredBy: "workload:jobs-lane-test",
			DeclaredAt: fixedInstant,
		})
		return err
	})
	return out
}

// --- DefinitionStore --------------------------------------------------

func TestDefinitionStore_PublishIsImmutableAndVersioned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "def-immutable")
	conn := appConn(t, db)

	def := newDefinition(tenant, "job.import.workers", 1)
	published := publish(t, ctx, conn, tenant, def)
	if published.PublishedAt.IsZero() {
		t.Fatal("published definition carries no publish time")
	}

	// A second publish of the same (tenant, job_id, version) is a duplicate,
	// not an update -- RED: a mutable run definition is refused at its source.
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.DefinitionStore{}).Publish(ctx, tx, def)
		return err
	})
	if !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("republish same version: err = %v, want ErrDuplicate", err)
	}

	// A new version under the same job id is a distinct, independently
	// publishable row.
	v2 := newDefinition(tenant, "job.import.workers", 2)
	publish(t, ctx, conn, tenant, v2)

	loaded := jobs.JobDefinition{}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		loaded, err = (jobs.DefinitionStore{}).Load(ctx, tx, tenant, "job.import.workers", 1)
		return err
	})
	if loaded.DefinitionDigest != def.DefinitionDigest {
		t.Fatalf("loaded digest = %s, want %s", loaded.DefinitionDigest, def.DefinitionDigest)
	}
}

func TestDefinitionStore_PublishRejectsUnversionedOrMalformedInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "def-invalid")
	conn := appConn(t, db)

	for name, mutate := range map[string]func(*jobs.JobDefinition){
		"zero version":        func(d *jobs.JobDefinition) { d.Version = 0 },
		"short digest":        func(d *jobs.JobDefinition) { d.DefinitionDigest = "not-a-digest" },
		"short trigger":       func(d *jobs.JobDefinition) { d.TriggerDigest = "abc" },
		"empty target":        func(d *jobs.JobDefinition) { d.TargetDefinitionRef = "" },
		"zero target version": func(d *jobs.JobDefinition) { d.TargetDefinitionVersion = 0 },
		"empty body":          func(d *jobs.JobDefinition) { d.Body = nil },
	} {
		t.Run(name, func(t *testing.T) {
			def := newDefinition(tenant, "job.invalid."+name, 1)
			mutate(&def)
			err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				_, err := (jobs.DefinitionStore{}).Publish(ctx, tx, def)
				return err
			})
			if !errors.Is(err, jobs.ErrInvalid) {
				t.Fatalf("%s: err = %v, want ErrInvalid", name, err)
			}
		})
	}
}

func TestDefinitionStore_LoadUnknownVersionIsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "def-notfound")
	conn := appConn(t, db)

	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.DefinitionStore{}).Load(ctx, tx, tenant, "job.never.published", 1)
		return err
	})
	if !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// --- RunStore -----------------------------------------------------------

func TestRunStore_LifecycleIsCASFencedWithAttemptCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "run-lifecycle")
	conn := appConn(t, db)
	def := publish(t, ctx, conn, tenant, newDefinition(tenant, "job.run.lifecycle", 1))

	runID := uuid.New()
	run := declareRun(t, ctx, conn, tenant, def, runID)
	if run.State != jobs.RunDeclared || run.Attempt != 1 || run.Version != 1 {
		t.Fatalf("declared run = %+v, want DECLARED/attempt=1/version=1", run)
	}

	var begun jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		begun, err = (jobs.RunStore{}).Begin(ctx, tx, tenant, runID, run.Version, fixedInstant.Add(time.Minute))
		return err
	})
	if begun.State != jobs.RunRunning || begun.Version != 2 {
		t.Fatalf("begun run = %+v, want RUNNING/version=2", begun)
	}

	// A stale expected version is refused rather than applied.
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).Complete(ctx, tx, tenant, runID, run.Version, fixedInstant.Add(2*time.Minute))
		return err
	})
	if !errors.Is(err, jobs.ErrVersionConflict) {
		t.Fatalf("complete with stale version: err = %v, want ErrVersionConflict", err)
	}

	var failed jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		failed, err = (jobs.RunStore{}).Fail(ctx, tx, tenant, runID, begun.Version, fixedInstant.Add(3*time.Minute), "connector timeout")
		return err
	})
	if failed.State != jobs.RunFailed || failed.FailureDetail != "connector timeout" {
		t.Fatalf("failed run = %+v, want FAILED with detail", failed)
	}

	var retried jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		retried, err = (jobs.RunStore{}).Retry(ctx, tx, tenant, runID, failed.Version, fixedInstant.Add(4*time.Minute))
		return err
	})
	if retried.State != jobs.RunDeclared || retried.Attempt != 2 || retried.FailureDetail != "" {
		t.Fatalf("retried run = %+v, want DECLARED/attempt=2 with no failure detail", retried)
	}

	// A terminal FAILED run cannot be failed again without an intervening
	// transition: the lifecycle graph refuses it outright.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).Begin(ctx, tx, tenant, runID, retried.Version, fixedInstant.Add(5*time.Minute))
		return err
	})
	var completed jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		completed, err = (jobs.RunStore{}).Complete(ctx, tx, tenant, runID, retried.Version+1, fixedInstant.Add(6*time.Minute))
		return err
	})
	err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).Complete(ctx, tx, tenant, runID, completed.Version, fixedInstant.Add(7*time.Minute))
		return err
	})
	if !errors.Is(err, jobs.ErrIllegalTransition) {
		t.Fatalf("complete an already-COMPLETED run: err = %v, want ErrIllegalTransition", err)
	}
}

func TestRunStore_StartRunRejectsUnpublishedVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "run-unpublished")
	conn := appConn(t, db)

	// RED: a run declared against a job version that was never published has
	// no row to satisfy the foreign key.
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID: tenant, RunID: uuid.New(), JobID: "job.never.published", JobVersion: 1,
			DeclaredBy: "workload:jobs-lane-test", DeclaredAt: fixedInstant,
		})
		return err
	})
	if err == nil {
		t.Fatal("StartRun against an unpublished version succeeded, want a foreign key refusal")
	}
}

func TestRunStore_StartRunRejectsZeroVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "run-zero-version")
	conn := appConn(t, db)

	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID: tenant, RunID: uuid.New(), JobID: "job.x", JobVersion: 0,
			DeclaredBy: "workload:jobs-lane-test", DeclaredAt: fixedInstant,
		})
		return err
	})
	if !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

// --- PartitionStore -------------------------------------------------------

func TestPartitionStore_LifecycleTracksADeterministicKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "partition-lifecycle")
	conn := appConn(t, db)
	def := publish(t, ctx, conn, tenant, newDefinition(tenant, "job.partition.lifecycle", 1))
	runID := uuid.New()
	declareRun(t, ctx, conn, tenant, def, runID)

	partitionID := uuid.New()
	var created jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: partitionID, RunID: runID,
			PartitionKey: "shard-0000", CreatedAt: fixedInstant,
		})
		return err
	})
	if created.State != jobs.PartitionPending {
		t.Fatalf("created partition state = %s, want PENDING", created.State)
	}

	// Replaying the same deterministic key for the same run after a simulated
	// crash is a duplicate, not a second partition.
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: uuid.New(), RunID: runID,
			PartitionKey: "shard-0000", CreatedAt: fixedInstant,
		})
		return err
	})
	if !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("recreate the same partition key: err = %v, want ErrDuplicate", err)
	}

	var claimed jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		claimed, err = (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, partitionID, created.Version, "worker:1", fixedInstant.Add(time.Minute))
		return err
	})
	if claimed.State != jobs.PartitionClaimed || claimed.ClaimedBy != "worker:1" {
		t.Fatalf("claimed partition = %+v, want CLAIMED by worker:1", claimed)
	}

	var completed jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		completed, err = (jobs.PartitionStore{}).Complete(ctx, tx, tenant, partitionID, claimed.Version, fixedInstant.Add(2*time.Minute))
		return err
	})
	if completed.State != jobs.PartitionCompleted {
		t.Fatalf("completed partition state = %s, want COMPLETED", completed.State)
	}

	var list []jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		list, err = (jobs.PartitionStore{}).ListByRun(ctx, tx, tenant, runID)
		return err
	})
	if len(list) != 1 || list[0].PartitionKey != "shard-0000" {
		t.Fatalf("ListByRun = %+v, want exactly one shard-0000 partition", list)
	}
}

func TestPartitionStore_ClaimOnAlreadyClaimedIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "partition-reclaim")
	conn := appConn(t, db)
	def := publish(t, ctx, conn, tenant, newDefinition(tenant, "job.partition.reclaim", 1))
	runID := uuid.New()
	declareRun(t, ctx, conn, tenant, def, runID)
	partitionID := uuid.New()
	var created jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: partitionID, RunID: runID,
			PartitionKey: "shard-0001", CreatedAt: fixedInstant,
		})
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, partitionID, created.Version, "worker:1", fixedInstant)
		return err
	})

	// The partition is already CLAIMED: PENDING -> CLAIMED is no longer a
	// transition this row can make, refused before any statement runs.
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, partitionID, created.Version, "worker:2", fixedInstant)
		return err
	})
	if !errors.Is(err, jobs.ErrIllegalTransition) {
		t.Fatalf("reclaim an already-claimed partition: err = %v, want ErrIllegalTransition", err)
	}
}

// --- CheckpointStore --------------------------------------------------

func TestCheckpointStore_IsAppendOnlyAndNumbered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "checkpoint-append")
	conn := appConn(t, db)
	def := publish(t, ctx, conn, tenant, newDefinition(tenant, "job.checkpoint.append", 1))
	runID := uuid.New()
	declareRun(t, ctx, conn, tenant, def, runID)
	partitionID := uuid.New()
	var created jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: partitionID, RunID: runID,
			PartitionKey: "shard-cp", CreatedAt: fixedInstant,
		})
		return err
	})

	cp1 := jobs.JobCheckpoint{
		TenantID: tenant, PartitionID: partitionID, Sequence: 1,
		StateDigest: digestOf("checkpoint-1"), PartitionVersion: created.Version, TakenAt: fixedInstant,
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, cp1)
		return err
	})

	// A repeated sequence is a duplicate: a rewritable safe point is not one.
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, cp1)
		return err
	})
	if !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("repeat sequence 1: err = %v, want ErrDuplicate", err)
	}

	cp2 := cp1
	cp2.Sequence = 2
	cp2.StateDigest = digestOf("checkpoint-2")
	cp2.TakenAt = fixedInstant.Add(time.Minute)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, cp2)
		return err
	})

	var latest jobs.JobCheckpoint
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		latest, err = (jobs.CheckpointStore{}).Latest(ctx, tx, tenant, partitionID)
		return err
	})
	if latest.Sequence != 2 || latest.StateDigest != cp2.StateDigest {
		t.Fatalf("latest checkpoint = %+v, want sequence 2 with cp2's digest", latest)
	}

	var all []jobs.JobCheckpoint
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		all, err = (jobs.CheckpointStore{}).List(ctx, tx, tenant, partitionID)
		return err
	})
	if len(all) != 2 || all[0].Sequence != 1 || all[1].Sequence != 2 {
		t.Fatalf("List = %+v, want two checkpoints in sequence order", all)
	}

	// The append-only trigger refuses a rewrite even attempted directly, not
	// only through the store's own API.
	err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE job_checkpoint SET state_digest = $1 WHERE tenant_id = $2 AND partition_id = $3 AND checkpoint_sequence = 1`,
			digestOf("tampered"), tenant, partitionID)
		return err
	})
	if err == nil {
		t.Fatal("direct UPDATE on job_checkpoint succeeded, want the forbid_mutation trigger to refuse it")
	}
}
