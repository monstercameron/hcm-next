package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/jobs"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

func TestDurableAsyncContinuationCreatesExactSpanLinksWithoutOpenParentSpan(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "obs013-causal")
	conn := appConn(t, db)
	def := publish(t, ctx, conn, tenant, newDefinition(tenant, "job.obs013", 1))

	trace := &jobs.TraceLinkMetadata{
		TraceID:    "0123456789abcdef0123456789abcdef",
		SpanID:     "0123456789abcdef",
		TraceFlags: 1,
		TraceState: "vendor=value",
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}
	causal := &jobs.CausalMetadata{
		CorrelationID:      "correlation-1",
		CausationID:        "cause-1",
		LogicalOperationID: "logical-1",
		AttemptID:          "attempt-1",
		TraceLink:          trace,
	}
	runID := uuid.New()
	var run jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		run, err = (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID: tenant, RunID: runID, JobID: def.JobID, JobVersion: def.Version,
			DeclaredBy: "workload:obs013", DeclaredAt: fixedInstant, Causal: causal,
		})
		return err
	})

	// Redelivery cannot fork the logical operation or manufacture a second run.
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID: tenant, RunID: runID, JobID: def.JobID, JobVersion: def.Version,
			DeclaredBy: "workload:obs013", DeclaredAt: fixedInstant, Causal: &jobs.CausalMetadata{LogicalOperationID: "forged-redelivery"},
		})
		return err
	})
	if !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("redelivery err = %v, want ErrDuplicate", err)
	}

	partitionID := uuid.New()
	var partition jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		partition, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: partitionID, RunID: runID, PartitionKey: "fanout-0",
			CreatedAt: fixedInstant, Causal: causal,
		})
		return err
	})
	initialPartitionAttempt := partition.Causal.AttemptID
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		partition, err = (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, partitionID, partition.Version, "worker:obs013", fixedInstant.Add(time.Minute))
		return err
	})
	if partition.Attempt != 1 || partition.Causal.AttemptID == initialPartitionAttempt {
		t.Fatalf("claim attempt = %d/%q, want first claim and a fresh attempt id", partition.Attempt, partition.Causal.AttemptID)
	}

	checkpoint := jobs.JobCheckpoint{
		TenantID: tenant, PartitionID: partitionID, Sequence: 1, StateDigest: digestOf("obs013"),
		PartitionVersion: partition.Version, TakenAt: fixedInstant.Add(2 * time.Minute), Causal: causal,
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, checkpoint)
		return err
	})
	malformedSQL := []string{
		`UPDATE job_run SET trace_id=NULL, trace_span_id=NULL, trace_flags=1, trace_state=NULL, trace_link_expires_at=NULL WHERE tenant_id=$1 AND run_id=$2`,
		`UPDATE job_partition SET trace_id=NULL, trace_span_id=NULL, trace_flags=1, trace_state=NULL, trace_link_expires_at=NULL WHERE tenant_id=$1 AND partition_id=$2`,
		`UPDATE job_checkpoint_trace_link SET trace_id=NULL, trace_span_id=NULL, trace_flags=1 WHERE tenant_id=$1 AND partition_id=$2`,
	}
	for i, statement := range malformedSQL {
		id := runID
		if i > 0 {
			id = partitionID
		}
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, statement, tenant, id)
			return err
		}); err == nil {
			t.Fatalf("malformed direct SQL tuple %d succeeded", i)
		}
	}

	// A fresh connection proves all three durable envelope kinds round-trip.
	fresh := appConn(t, db)
	inTenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
		loadedRun, err := (jobs.RunStore{}).Load(ctx, tx, tenant, runID)
		if err != nil {
			return err
		}
		loadedPartition, err := (jobs.PartitionStore{}).Load(ctx, tx, tenant, partitionID)
		if err != nil {
			return err
		}
		loadedCheckpoint, err := (jobs.CheckpointStore{}).Latest(ctx, tx, tenant, partitionID)
		if err != nil {
			return err
		}
		assertCausalRoundTrip(t, loadedRun.Causal, "attempt-1", true)
		assertCausalRoundTrip(t, loadedPartition.Causal, partition.Causal.AttemptID, true)
		assertCausalRoundTrip(t, loadedCheckpoint.Causal, "attempt-1", true)
		return nil
	})

	var failed jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		begun, err := (jobs.RunStore{}).Begin(ctx, tx, tenant, runID, run.Version, fixedInstant.Add(3*time.Minute))
		if err != nil {
			return err
		}
		failed, err = (jobs.RunStore{}).Fail(ctx, tx, tenant, runID, begun.Version, fixedInstant.Add(4*time.Minute), "retryable")
		return err
	})
	priorAttempt := failed.Causal.AttemptID
	var retried jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		retried, err = (jobs.RunStore{}).Retry(ctx, tx, tenant, runID, failed.Version, fixedInstant.Add(5*time.Minute))
		return err
	})
	if retried.Causal.LogicalOperationID != "logical-1" || retried.Causal.AttemptID == priorAttempt {
		t.Fatalf("retry causal = %+v, want stable logical id and fresh attempt id", retried.Causal)
	}
}

func TestTodo_OBS_013_Recovery(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "obs013-optional")
	conn := appConn(t, db)
	def := publish(t, ctx, conn, tenant, newDefinition(tenant, "job.obs013.optional", 1))

	cases := []struct {
		name   string
		causal *jobs.CausalMetadata
	}{
		{name: "nil"},
		{name: "expired", causal: &jobs.CausalMetadata{LogicalOperationID: "logical-expired", TraceLink: &jobs.TraceLinkMetadata{TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef", ExpiresAt: time.Now().Add(-time.Hour)}}},
		{name: "malformed", causal: &jobs.CausalMetadata{LogicalOperationID: "logical-malformed", CorrelationID: strings.Repeat("x", 129), TraceLink: &jobs.TraceLinkMetadata{TraceID: "not-a-trace", SpanID: "bad"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runID := uuid.New()
			var run jobs.JobRun
			inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
				var err error
				run, err = (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{TenantID: tenant, RunID: runID, JobID: def.JobID, JobVersion: def.Version, DeclaredBy: "workload:obs013", DeclaredAt: fixedInstant, Causal: tc.causal})
				return err
			})
			if run.State != jobs.RunDeclared {
				t.Fatalf("state = %s, want DECLARED regardless of optional telemetry", run.State)
			}
			inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
				loaded, err := (jobs.RunStore{}).Load(ctx, tx, tenant, runID)
				if err != nil {
					return err
				}
				if loaded.Causal != nil && loaded.Causal.TraceLink != nil {
					t.Fatalf("optional %s trace link survived normalization: %+v", tc.name, loaded.Causal.TraceLink)
				}
				return nil
			})
		})
	}
}

func TestTodo_OBS_013_Fault(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "obs013-retention")
	other := insertTenant(t, db, "obs013-retention-other")
	now := time.Now().UTC()
	create := func(tenantID uuid.UUID, suffix string, expiries ...time.Time) (uuid.UUID, uuid.UUID, *pgxadapter.Conn) {
		conn := appConn(t, db)
		def := publish(t, ctx, conn, tenantID, newDefinition(tenantID, "job.obs013.retention."+suffix, 1))
		run := declareRun(t, ctx, conn, tenantID, def, uuid.New())
		partitionID := uuid.New()
		var partition jobs.JobPartition
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			partition, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{TenantID: tenantID, PartitionID: partitionID, RunID: run.RunID, PartitionKey: suffix, CreatedAt: fixedInstant})
			return err
		})
		for i, expiry := range expiries {
			inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
				_, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, jobs.JobCheckpoint{TenantID: tenantID, PartitionID: partitionID, Sequence: uint64(i + 1), StateDigest: digestOf(fmt.Sprintf("%s-%d", suffix, i)), PartitionVersion: partition.Version, TakenAt: fixedInstant, Causal: &jobs.CausalMetadata{LogicalOperationID: "logical-" + suffix, TraceLink: &jobs.TraceLinkMetadata{TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef", ExpiresAt: expiry}}})
				return err
			})
		}
		return run.RunID, partitionID, conn
	}
	runID, partitionID, conn := create(tenant, "owned", now.Add(time.Hour), now.Add(3*time.Hour))
	_, otherPartitionID, otherConn := create(other, "other", now.Add(time.Hour))
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		for _, statement := range []string{
			`UPDATE job_run SET correlation_id='correlation-retained', logical_operation_id='logical-retained', trace_id='0123456789abcdef0123456789abcdef', trace_span_id='0123456789abcdef', trace_flags=1, trace_link_expires_at=$3 WHERE tenant_id=$1 AND run_id=$2`,
			`UPDATE job_partition SET correlation_id='correlation-retained', logical_operation_id='logical-retained', trace_id='0123456789abcdef0123456789abcdef', trace_span_id='0123456789abcdef', trace_flags=1, trace_link_expires_at=$3 WHERE tenant_id=$1 AND partition_id=$2`,
		} {
			id := runID
			if strings.Contains(statement, "job_partition") {
				id = partitionID
			}
			if _, err := tx.Exec(ctx, statement, tenant, id, now.Add(-time.Hour)); err != nil {
				return err
			}
		}
		return nil
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		n, err := (jobs.CheckpointStore{}).PruneExpiredTraceLinks(ctx, tx, tenant, now.Add(-2*time.Hour), 10)
		if err == nil && n != 0 {
			t.Fatalf("early purge removed %d links", n)
		}
		return err
	})

	sentinel := errors.New("rollback purge")
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		if n, err := (jobs.CheckpointStore{}).PruneExpiredTraceLinks(ctx, tx, tenant, now.Add(2*time.Hour), 3); err != nil || n != 3 {
			t.Fatalf("rollback purge = %d, %v", n, err)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("rollback err = %v", err)
	}
	var removed int64
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		removed, err = (jobs.CheckpointStore{}).PruneExpiredTraceLinks(ctx, tx, tenant, now.Add(2*time.Hour), 3)
		return err
	})
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		run, err := (jobs.RunStore{}).Load(ctx, tx, tenant, runID)
		if err != nil {
			return err
		}
		partition, err := (jobs.PartitionStore{}).Load(ctx, tx, tenant, partitionID)
		if err != nil {
			return err
		}
		for name, value := range map[string]struct {
			version uint64
			causal  *jobs.CausalMetadata
		}{"run": {run.Version, run.Causal}, "partition": {partition.Version, partition.Causal}} {
			if value.version != 1 || value.causal == nil || value.causal.LogicalOperationID != "logical-retained" || value.causal.CorrelationID != "correlation-retained" || value.causal.TraceLink != nil {
				t.Fatalf("%s changed business state during trace purge: version=%d causal=%+v", name, value.version, value.causal)
			}
		}
		return nil
	})
	assertCounts := func(conn *pgxadapter.Conn, tenantID, partID uuid.UUID, links, checkpoints int) {
		t.Helper()
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var gotLinks, gotCheckpoints int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM job_checkpoint_trace_link WHERE partition_id=$1`, partID).Scan(&gotLinks); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM job_checkpoint WHERE partition_id=$1`, partID).Scan(&gotCheckpoints); err != nil {
				return err
			}
			if gotLinks != links || gotCheckpoints != checkpoints {
				t.Fatalf("counts = links %d checkpoints %d, want %d/%d", gotLinks, gotCheckpoints, links, checkpoints)
			}
			return nil
		})
	}
	assertCounts(conn, tenant, partitionID, 1, 2)
	assertCounts(otherConn, other, otherPartitionID, 1, 1)

	// Hold the row lock while cleanup takes its snapshot, then refresh the
	// tuple before cleanup acquires the lock. PostgreSQL must recheck expiry.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE job_run SET trace_id='0123456789abcdef0123456789abcdef', trace_span_id='0123456789abcdef', trace_flags=1, trace_link_expires_at=$3 WHERE tenant_id=$1 AND run_id=$2`, tenant, runID, now.Add(-time.Hour))
		return err
	})
	locker, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin locker: %v", err)
	}
	if err := tenancy.WithTenant(ctx, locker, tenant); err != nil {
		t.Fatalf("scope locker: %v", err)
	}
	if _, err := locker.Exec(ctx, `SELECT 1 FROM job_run WHERE tenant_id=$1 AND run_id=$2 FOR UPDATE`, tenant, runID); err != nil {
		t.Fatalf("lock run: %v", err)
	}
	cleaner := appConn(t, db)
	var cleanerPID int
	if err := cleaner.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&cleanerPID); err != nil {
		t.Fatalf("cleaner pid: %v", err)
	}
	cleanupDone := make(chan struct {
		n   int64
		err error
	}, 1)
	go func() {
		var result struct {
			n   int64
			err error
		}
		result.err = inTenantTxErr(cleaner, tenant, func(tx dbport.Tx) error {
			result.n, result.err = (jobs.CheckpointStore{}).PruneExpiredTraceLinks(ctx, tx, tenant, now, 1)
			return result.err
		})
		cleanupDone <- result
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := db.Conn.QueryRow(ctx, `SELECT wait_event_type = 'Lock' FROM pg_stat_activity WHERE pid=$1`, cleanerPID).Scan(&waiting); err == nil && waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup never blocked on the row lock")
		}
		runtime.Gosched()
	}
	if _, err := locker.Exec(ctx, `UPDATE job_run SET trace_id='fedcba9876543210fedcba9876543210', trace_span_id='fedcba9876543210', trace_link_expires_at=$3 WHERE tenant_id=$1 AND run_id=$2`, tenant, runID, now.Add(time.Hour)); err != nil {
		t.Fatalf("refresh trace: %v", err)
	}
	if err := locker.Commit(ctx); err != nil {
		t.Fatalf("commit refresh: %v", err)
	}
	result := <-cleanupDone
	if result.err != nil || result.n != 0 {
		t.Fatalf("concurrent cleanup = %d, %v; want refreshed tuple preserved", result.n, result.err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		run, err := (jobs.RunStore{}).Load(ctx, tx, tenant, runID)
		if err != nil {
			return err
		}
		if run.Causal == nil || run.Causal.TraceLink == nil || run.Causal.TraceLink.TraceID != "fedcba9876543210fedcba9876543210" {
			t.Fatalf("fresh trace was lost: %+v", run.Causal)
		}
		return nil
	})
}

func assertCausalRoundTrip(t *testing.T, got *jobs.CausalMetadata, attempt string, wantTrace bool) {
	t.Helper()
	if got == nil || got.CorrelationID != "correlation-1" || got.CausationID != "cause-1" || got.LogicalOperationID != "logical-1" || got.AttemptID != attempt {
		t.Fatalf("causal metadata = %+v, want exact business tuple and attempt %q", got, attempt)
	}
	if (got.TraceLink != nil) != wantTrace {
		t.Fatalf("trace link present = %v, want %v", got.TraceLink != nil, wantTrace)
	}
}
