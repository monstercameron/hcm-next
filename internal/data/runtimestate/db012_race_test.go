package runtimestate_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// TestTodo_DB_012_Race drives concurrent writers across all four stores on
// real, independent connections: exactly one winner per contended identity,
// and every loser refused rather than partially applied.
func TestTodo_DB_012_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	t.Run("CAS: concurrent RecordInstanceState calls at the same expected version, exactly one commits", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-race-cas")
		plan := referencePlan(t)
		setupConn := appConn(t, db)
		inst := createInstance(t, ctx, setupConn, tenant, plan)

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
					_, err := (runtime.Store{}).RecordInstanceState(ctx, tx, runtime.InstanceTransition{
						TenantID: tenant, InstanceID: inst.InstanceID, ExpectedVersion: inst.InstanceVersion,
						Status: runtime.InstanceRunning, CurrentNodeIDs: inst.CurrentNodeIDs, StartedAt: timePtr(fixedInstant),
					})
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
			case runtime.CodeOf(err) == runtime.CodeStaleInstance:
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
	})

	t.Run("claim exclusivity: concurrent claimants on an AVAILABLE item, exactly one wins", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-race-claim")
		plan := referencePlan(t)
		setupConn := appConn(t, db)
		inst := createInstance(t, ctx, setupConn, tenant, plan)
		store := workitem.Store{}
		item := multiCandidateAvailableItem(t, ctx, store, setupConn, tenant, inst.InstanceID, "approval_node",
			"principal:cand-1", "principal:cand-2", "principal:cand-3", "principal:cand-4", "principal:cand-5", "principal:cand-6")

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
					_, err := store.Claim(ctx, tx, workitem.ClaimInput{
						TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
						ClaimantPrincipalID: "principal:race-claimant", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
						Meta: workMeta("workitem.claimed"),
					})
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
			case workitem.CodeOf(err) == workitem.CodeAlreadyClaimed:
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
	})

	t.Run("idempotency: concurrent Guard calls under the same scope and digest run the effect exactly once", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-race-idem")
		scope := idempotency.Scope{Tenant: tenant, Capability: "db012.race.v1", EffectScope: "worker:race", Key: "key-race"}
		digest := digestOf("payload-race")

		const workers = 6
		conns := make([]*pgxadapter.Conn, workers)
		for i := range conns {
			conns[i] = appConn(t, db)
		}
		var ran int32
		results := make([]idempotency.Record, workers)
		errs := make([]error, workers)
		var startGate, done sync.WaitGroup
		startGate.Add(1)
		for i := 0; i < workers; i++ {
			done.Add(1)
			go func(i int) {
				defer done.Done()
				startGate.Wait()
				errs[i] = inTenantTxErr(conns[i], tenant, func(tx dbport.Tx) error {
					rec, err := idempotency.Guard(ctx, tx, idempotency.PostgresStore{}, scope, digest, defaultPolicy, fixedInstant,
						func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
							atomic.AddInt32(&ran, 1)
							return idempotency.ResultIdentity{ResultRef: "ref-race"}, nil
						})
					results[i] = rec
					return err
				})
			}(i)
		}
		startGate.Done()
		done.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("worker %d: %v", i, err)
			}
		}
		if ran != 1 {
			t.Fatalf("effect ran %d times under concurrent identical reservations, want exactly 1", ran)
		}
		for i := 1; i < workers; i++ {
			if results[i].Identity.ResultRef != results[0].Identity.ResultRef {
				t.Fatalf("worker %d saw identity %q, want the winner's %q", i, results[i].Identity.ResultRef, results[0].Identity.ResultRef)
			}
		}
	})

	t.Run("continuation dedupe: concurrent identical MarkReady writes leave exactly one row", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-race-cont")
		plan := referencePlan(t)
		setupConn := appConn(t, db)
		inst := createInstance(t, ctx, setupConn, tenant, plan)
		rec := runtime.ContinuationRecord{
			TenantID: tenant, InstanceID: inst.InstanceID,
			SourceNodeID: plan.StartNodeID, SourceAttempt: 1,
			TargetNodeID: plan.StartNodeID, Kind: frontier.IntentReady,
			RecordedAt: fixedInstant,
		}

		const workers = 6
		conns := make([]*pgxadapter.Conn, workers)
		for i := range conns {
			conns[i] = appConn(t, db)
		}
		errs := make([]error, workers)
		var startGate, done sync.WaitGroup
		startGate.Add(1)
		for i := 0; i < workers; i++ {
			done.Add(1)
			go func(i int) {
				defer done.Done()
				startGate.Wait()
				errs[i] = inTenantTxErr(conns[i], tenant, func(tx dbport.Tx) error {
					return (runtime.ContinuationStore{}).MarkReady(ctx, tx, rec)
				})
			}(i)
		}
		startGate.Done()
		done.Wait()

		// ContinuationStore's insert names an explicit ON CONFLICT arbiter --
		// (tenant_id, continuation_id), the primary key -- so a losing writer
		// among genuinely concurrent connections can surface a duplicate-key
		// error on migration 00018's OTHER unique constraint
		// (tenant_id, instance_id, source_node_id, source_attempt,
		// target_node_id, kind) instead of silently no-op'ing: Postgres only
		// suppresses a violation of the named arbiter index, not of a
		// different unique index that happens to reject the same logical
		// row. That is a real gap in continuation.go this suite may not fix
		// (out of scope), but it is unreachable through runtime.Advance's own
		// call path, where writes to one continuation identity are already
		// serialized by the instance-version CAS before they ever reach this
		// insert -- only a caller that (like this test) invokes
		// ContinuationStore directly from independent, unfenced connections
		// can hit it. What DB-012 requires either way -- no duplicate row --
		// still holds: a losing writer's statement is rejected outright, so
		// it commits nothing, rather than forking a second row.
		for i, err := range errs {
			if err != nil && !strings.Contains(err.Error(), "duplicate key") {
				t.Fatalf("worker %d: unexpected error: %v", i, err)
			}
		}
		var count int
		inTenantTx(t, setupConn, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM workflow_continuation WHERE tenant_id = $1 AND instance_id = $2 AND target_node_id = $3 AND kind = $4`,
				tenant, inst.InstanceID, plan.StartNodeID, string(frontier.IntentReady)).Scan(&count)
		})
		if count != 1 {
			t.Fatalf("continuation rows for one identity after concurrent writes = %d, want exactly 1", count)
		}
	})
}
