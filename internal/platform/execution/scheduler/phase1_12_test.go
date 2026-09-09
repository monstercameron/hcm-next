package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	wfruntime "github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type fairnessReady struct {
	tenantID   uuid.UUID
	instanceID uuid.UUID
	readyID    uuid.UUID
	mode       workflow.ExecutionMode
	priority   int
}

func compilePromotionPlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("compile promotionexec plan: %v", err)
	}
	return plan
}

func enqueuePromotionReady(t *testing.T, db *pgtest.DB, tenantID uuid.UUID,
	plan *workflow.CompiledWorkflow, mode workflow.ExecutionMode, priority int, key string,
) fairnessReady {
	t.Helper()
	ctx := context.Background()
	instanceID := uuid.New()
	inst, err := wfruntime.NewInstance(tenantID, instanceID, "cell-local", plan, mode,
		"sha256:input-"+key, "corr:"+key, fixtureAt)
	if err != nil {
		t.Fatalf("new %s instance: %v", mode, err)
	}
	ready := fairnessReady{
		tenantID: tenantID, instanceID: instanceID, readyID: uuid.New(), mode: mode, priority: priority,
	}
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := (wfruntime.Store{}).CreateInstance(ctx, tx, inst); err != nil {
			return err
		}
		return (runtimestate.ReadyWorkStore{}).Enqueue(ctx, tx, runtimestate.ReadyWork{
			TenantID: tenantID, ReadyWorkID: ready.readyID, InstanceID: instanceID,
			NodeID: promotionexec.NodeSnapshotWorker, Attempt: 1, State: runtimestate.ReadyReady,
			Priority: priority, EligibleAt: fixtureAt, EnqueuedAt: fixtureAt,
		})
	})
	return ready
}

func fairnessClaim(tenantID uuid.UUID, holder string) lease.AcquireRequest {
	return lease.AcquireRequest{
		TenantID: tenantID,
		Resource: lease.Resource{Kind: lease.ResourceQueue, ID: testQueue},
		Holder:   lease.Identity{WorkloadRef: "workload:hcmnext-scheduler", InstanceRef: holder},
	}
}

func newFairnessScheduler(t *testing.T, db *pgtest.DB, claims []lease.AcquireRequest,
	batchSize, bulkShare int, dispatcher Dispatcher,
) *Scheduler {
	t.Helper()
	s, err := New(Config{
		DB:             appConn(t, db),
		Claims:         claims,
		Misfire:        catchUpOnce(),
		Dispatcher:     dispatcher,
		Clock:          func() time.Time { return fixtureAt },
		BatchSize:      batchSize,
		BulkBatchShare: bulkShare,
		QueueTTL:       30 * time.Second,
		Logger:         &recordingLogger{},
	})
	if err != nil {
		t.Fatalf("new fairness scheduler: %v", err)
	}
	return s
}

func TestNewFillsTheDeclaredBulkShare(t *testing.T) {
	s, err := New(validConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.cfg.BulkBatchShare != DefaultBulkBatchShare {
		t.Fatalf("bulk batch share = %d, want %d", s.cfg.BulkBatchShare, DefaultBulkBatchShare)
	}
}

func TestLivePilotWorkIsNotStarvedByBulkWork(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "phase1-12-live-pilot", fixtureAt)
	plan := compilePromotionPlan(t)
	live := enqueuePromotionReady(t, db, tenantID, plan, workflow.ModeExecute, PilotPriority, "live")
	const batchSize = 4
	const bulkShare = 1
	bulk := make(map[uuid.UUID]bool)
	for i := 0; i < 6; i++ {
		row := enqueuePromotionReady(t, db, tenantID, plan, workflow.ModeExecute, BulkPriority,
			fmt.Sprintf("bulk-%d", i))
		bulk[row.instanceID] = true
	}

	var mu sync.Mutex
	perTick := make([][]uuid.UUID, 0, 4)
	current := []uuid.UUID{}
	dispatcher := DispatcherFunc(func(_ context.Context, work Work) (Disposition, error) {
		mu.Lock()
		current = append(current, work.Row.InstanceID)
		mu.Unlock()
		return DispositionCompleted, nil
	})
	s := newFairnessScheduler(t, db, []lease.AcquireRequest{fairnessClaim(tenantID, "pilot")},
		batchSize, bulkShare, dispatcher)

	for tick := 0; tick < 4; tick++ {
		mu.Lock()
		current = nil
		mu.Unlock()
		if _, err := s.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		mu.Lock()
		got := append([]uuid.UUID(nil), current...)
		perTick = append(perTick, got)
		mu.Unlock()
		background := 0
		for _, instanceID := range got {
			if bulk[instanceID] {
				background++
			}
		}
		if background > bulkShare {
			t.Fatalf("tick %d admitted %d bulk rows, want at most declared share %d: %v",
				tick, background, bulkShare, got)
		}
	}

	if len(perTick) == 0 || len(perTick[0]) == 0 || perTick[0][0] != live.instanceID {
		t.Fatalf("live pilot was not first in the first tick: %v", perTick)
	}
}

func TestReplayWorkIsAdmittedBelowLiveWork(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "phase1-12-replay", fixtureAt)
	plan := compilePromotionPlan(t)
	// Give replay an apparently urgent numeric priority. The durable mode is
	// the stronger boundary, so it still cannot pre-empt EXECUTE work.
	replay := enqueuePromotionReady(t, db, tenantID, plan, workflow.ModeReplay, PilotPriority, "replay")
	execute := enqueuePromotionReady(t, db, tenantID, plan, workflow.ModeExecute, BulkPriority, "execute")
	var order []uuid.UUID
	s := newFairnessScheduler(t, db, []lease.AcquireRequest{fairnessClaim(tenantID, "replay")}, 1, 1,
		DispatcherFunc(func(_ context.Context, work Work) (Disposition, error) {
			order = append(order, work.Row.InstanceID)
			return DispositionCompleted, nil
		}))

	for i := 0; i < 2; i++ {
		if _, err := s.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if len(order) != 2 || order[0] != execute.instanceID || order[1] != replay.instanceID {
		t.Fatalf("dispatch order = %v, want EXECUTE %s before REPLAY %s", order, execute.instanceID, replay.instanceID)
	}
}

func TestPerTenantBatchBoundIsHonoured(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	noisy := insertTenant(t, db, "phase1-12-noisy", fixtureAt)
	quiet := insertTenant(t, db, "phase1-12-quiet", fixtureAt)
	plan := compilePromotionPlan(t)
	noisyRows := make([]fairnessReady, 0, 5)
	for i := 0; i < cap(noisyRows); i++ {
		noisyRows = append(noisyRows, enqueuePromotionReady(t, db, noisy, plan, workflow.ModeExecute,
			BulkPriority, fmt.Sprintf("noisy-%d", i)))
	}
	quietRow := enqueuePromotionReady(t, db, quiet, plan, workflow.ModeExecute, BulkPriority, "quiet")

	var mu sync.Mutex
	seen := map[uuid.UUID]int{}
	s := newFairnessScheduler(t, db, []lease.AcquireRequest{
		fairnessClaim(noisy, "tenant-bound"), fairnessClaim(quiet, "tenant-bound"),
	}, 2, 2, DispatcherFunc(func(_ context.Context, work Work) (Disposition, error) {
		mu.Lock()
		seen[work.Row.TenantID]++
		mu.Unlock()
		return DispositionCompleted, nil
	}))
	if _, err := s.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if got := seen[noisy]; got != 2 {
		t.Fatalf("noisy tenant consumed %d claims, want its per-tenant batch bound 2", got)
	}
	if got := seen[quiet]; got != 1 {
		t.Fatalf("quiet tenant consumed %d claims, want its independent claim", got)
	}
	if state, _ := readyStateOf(t, db, quiet, quietRow.readyID); state != runtimestate.ReadyDone {
		t.Fatalf("quiet tenant's row is %s, want DONE", state)
	}
}

func TestTenantAndCriticalityClaimsRaceAcrossReplicas(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "phase1-12-race", fixtureAt)
	plan := compilePromotionPlan(t)
	const rows = 6
	readyIDs := make(map[uuid.UUID]bool, rows)
	for i := 0; i < rows; i++ {
		ready := enqueuePromotionReady(t, db, tenantID, plan, workflow.ModeExecute, BulkPriority,
			fmt.Sprintf("race-%d", i))
		readyIDs[ready.readyID] = true
	}

	var mu sync.Mutex
	dispatches := map[uuid.UUID]int{}
	dispatcher := DispatcherFunc(func(_ context.Context, work Work) (Disposition, error) {
		mu.Lock()
		dispatches[work.Row.ReadyWorkID]++
		mu.Unlock()
		return DispositionCompleted, nil
	})
	// These are separate PostgreSQL sessions, just as two scheduler processes
	// would be. The contention is on SKIP LOCKED, the instance lease, and the
	// ready-row CAS, not on a shared in-memory scheduler.
	replicaA := newFairnessScheduler(t, db, []lease.AcquireRequest{fairnessClaim(tenantID, "race-a")}, 2, 1, dispatcher)
	replicaB := newFairnessScheduler(t, db, []lease.AcquireRequest{fairnessClaim(tenantID, "race-b")}, 2, 1, dispatcher)

	for round := 0; round < 8; round++ {
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, replica := range []*Scheduler{replicaA, replicaB} {
			wg.Add(1)
			go func(s *Scheduler) {
				defer wg.Done()
				_, err := s.Tick(ctx)
				errs <- err
			}(replica)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("replica tick: %v", err)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(dispatches) != len(readyIDs) {
		t.Fatalf("dispatched %d distinct rows, want %d: %v", len(dispatches), len(readyIDs), dispatches)
	}
	for readyID := range readyIDs {
		if dispatches[readyID] != 1 {
			t.Errorf("ready row %s dispatched %d times, want exactly once", readyID, dispatches[readyID])
		}
	}
}
