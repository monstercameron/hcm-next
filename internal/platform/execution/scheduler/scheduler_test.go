package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

func validConfig() Config {
	return Config{
		DB:      failingBeginner{err: errors.New("no database in this test")},
		Claims:  []lease.AcquireRequest{claimFixture(uuid.New(), "replica:a")},
		Misfire: catchUpOnce(),
	}
}

// TestNewRefusesEveryUnusableConfiguration proves a misconfigured replica
// fails at composition rather than at its first tick, which is what keeps a
// bad deployment from silently holding leases and dispatching nothing.
func TestNewRefusesEveryUnusableConfiguration(t *testing.T) {
	tenantID := uuid.New()
	for name, mutate := range map[string]func(*Config){
		"no database": func(c *Config) { c.DB = nil },
		"no claims":   func(c *Config) { c.Claims = nil },
		"nil tenant": func(c *Config) {
			c.Claims = []lease.AcquireRequest{claimFixture(uuid.Nil, "replica:a")}
		},
		"claim is not a queue": func(c *Config) {
			c.Claims[0].Resource.Kind = lease.ResourceWorkflowInstance
		},
		"claim names no queue": func(c *Config) { c.Claims[0].Resource.ID = "" },
		"holder names no workload": func(c *Config) {
			c.Claims[0].Holder.WorkloadRef = ""
		},
		"holder names no replica": func(c *Config) {
			c.Claims[0].Holder.InstanceRef = ""
		},
		"repeated claim": func(c *Config) {
			c.Claims = []lease.AcquireRequest{claimFixture(tenantID, "replica:a"), claimFixture(tenantID, "replica:a")}
		},
		"no misfire policy": func(c *Config) { c.Misfire = schedule.MisfireConfig{} },
		"undeclared misfire policy": func(c *Config) {
			c.Misfire = schedule.MisfireConfig{Policy: schedule.MisfirePolicy("WHENEVER")}
		},
		"negative batch size":  func(c *Config) { c.BatchSize = -1 },
		"negative queue lease": func(c *Config) { c.QueueTTL = -time.Second },
		"negative claim lease": func(c *Config) { c.InstanceTTL = -time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := validConfig()
			mutate(&cfg)
			if _, err := New(cfg); !errors.Is(err, ErrConfig) {
				t.Fatalf("New with %s: err = %v, want ErrConfig", name, err)
			}
		})
	}
}

// TestNewFillsItsDeclaredDefaults keeps the defaults in one place: a Config
// that names none of the bounds still gets the documented ones, so a replica
// never runs with a zero batch size or a zero lease window.
func TestNewFillsItsDeclaredDefaults(t *testing.T) {
	s, err := New(validConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.cfg.BatchSize != DefaultBatchSize {
		t.Errorf("batch size = %d, want %d", s.cfg.BatchSize, DefaultBatchSize)
	}
	if s.cfg.QueueTTL != DefaultQueueTTL {
		t.Errorf("queue lease = %s, want %s", s.cfg.QueueTTL, DefaultQueueTTL)
	}
	if s.cfg.InstanceTTL != DefaultInstanceTTL {
		t.Errorf("instance lease = %s, want %s", s.cfg.InstanceTTL, DefaultInstanceTTL)
	}
	if s.cfg.Logger == nil || s.clock == nil {
		t.Fatal("New left the logger or the clock unset")
	}
	if got := s.clock(); got.IsZero() {
		t.Fatal("default clock returned the zero instant")
	}
}

// TestNewAcceptsSeveralQueuesForOneTenant is the sharding shape: one replica
// may serve more than one queue, and two claims differ by their queue, not
// only by their tenant.
func TestNewAcceptsSeveralQueuesForOneTenant(t *testing.T) {
	tenantID := uuid.New()
	first := claimFixture(tenantID, "replica:a")
	second := claimFixture(tenantID, "replica:a")
	second.Resource.ID = "queue:workflow-runtime-shard-2"
	cfg := validConfig()
	cfg.Claims = []lease.AcquireRequest{first, second}
	if _, err := New(cfg); err != nil {
		t.Fatalf("New with two queues: %v", err)
	}
}

// TestTickReportsAFailureWithoutStoppingTheOtherClaims proves one tenant's
// unreachable state does not stop another tenant's work: both claims are
// attempted, both failures are logged, and the tick still returns.
func TestTickReportsAFailureWithoutStoppingTheOtherClaims(t *testing.T) {
	logger := &recordingLogger{}
	cfg := validConfig()
	cfg.Claims = []lease.AcquireRequest{claimFixture(uuid.New(), "replica:a"), claimFixture(uuid.New(), "replica:a")}
	cfg.Logger = logger
	cfg.Clock = func() time.Time { return fixtureAt }
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := s.Tick(context.Background())
	if err == nil {
		t.Fatal("Tick reported no error although every claim failed")
	}
	if !result.At.Equal(fixtureAt) {
		t.Fatalf("tick instant = %s, want the configured clock reading %s", result.At, fixtureAt)
	}
	if result.Leased != 0 || result.Claimed != 0 {
		t.Fatalf("a tick against an unreachable database leased %d and claimed %d", result.Leased, result.Claimed)
	}
	if got := logger.count("scheduler.claim_failed"); got != 2 {
		t.Fatalf("%d claim failures logged, want one per claim", got)
	}
}

// TestRunReturnsWhenItsContextIsDone proves the workload contract bootstrap
// depends on: cancellation is a shutdown, not a role failure, so Run returns
// nil promptly and never reports ctx.Err().
func TestRunReturnsWhenItsContextIsDone(t *testing.T) {
	cfg := validConfig()
	cfg.Clock = func() time.Time { return fixtureAt }
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, 5*time.Millisecond) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after its context was canceled")
	}
}

// TestTickResultIdle pins what "nothing to do" means, because it is what
// decides whether Run sleeps: work selected but contended away by another
// replica is still idle for this one, while a recovered row is not.
func TestTickResultIdle(t *testing.T) {
	for name, tc := range map[string]struct {
		result TickResult
		idle   bool
	}{
		"empty":                {TickResult{}, true},
		"leased only":          {TickResult{Leased: 1}, true},
		"refused":              {TickResult{Refused: 1}, true},
		"selected, contended":  {TickResult{Selected: 3, Contended: 3}, true},
		"deferred timer only":  {TickResult{Deferred: 1}, true},
		"fired a timer":        {TickResult{Fired: 1}, false},
		"skipped a timer":      {TickResult{Skipped: 1}, false},
		"recovered a claim":    {TickResult{Recovered: 1}, false},
		"claimed one and dis.": {TickResult{Claimed: 1, Completed: 1}, false},
	} {
		if got := tc.result.Idle(); got != tc.idle {
			t.Errorf("%s: Idle() = %v, want %v", name, got, tc.idle)
		}
	}
}

func TestTickResultAddSumsEveryCount(t *testing.T) {
	total := TickResult{At: fixtureAt}
	one := TickResult{
		Leased: 1, Refused: 2, Fired: 3, Skipped: 4, Deferred: 5, Recovered: 6,
		Selected: 7, Claimed: 8, Contended: 9, Completed: 10, Retried: 11, Abandoned: 12,
	}
	total.add(one)
	total.add(one)
	want := TickResult{
		At: fixtureAt, Leased: 2, Refused: 4, Fired: 6, Skipped: 8, Deferred: 10, Recovered: 12,
		Selected: 14, Claimed: 16, Contended: 18, Completed: 20, Retried: 22, Abandoned: 24,
	}
	if total != want {
		t.Fatalf("summed result = %+v, want %+v", total, want)
	}
}

// TestDispatchTreatsAFailureAsARetry proves a dispatcher that failed never
// strands work: the disposition is RETRY, which returns the row to READY.
func TestDispatchTreatsAFailureAsARetry(t *testing.T) {
	logger := &recordingLogger{}
	cfg := validConfig()
	cfg.Logger = logger
	cfg.Dispatcher = DispatcherFunc(func(context.Context, Work) (Disposition, error) {
		return DispositionCompleted, errors.New("the driver refused")
	})
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := s.dispatch(context.Background(), Work{Row: readyWorkFixture()}); got != DispositionRetry {
		t.Fatalf("disposition after a dispatcher failure = %q, want RETRY", got)
	}
	if logger.count("scheduler.dispatch_failed") != 1 {
		t.Fatal("a dispatcher failure was not reported")
	}
}

// TestDispatchTreatsAnUndeclaredDispositionAsARetry closes the other half of
// the same hole: a dispatcher returning something outside the vocabulary must
// not settle a row into a state nobody declared.
func TestDispatchTreatsAnUndeclaredDispositionAsARetry(t *testing.T) {
	logger := &recordingLogger{}
	cfg := validConfig()
	cfg.Logger = logger
	cfg.Dispatcher = DispatcherFunc(func(context.Context, Work) (Disposition, error) {
		return Disposition("SORT_OF_DONE"), nil
	})
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := s.dispatch(context.Background(), Work{Row: readyWorkFixture()}); got != DispositionRetry {
		t.Fatalf("disposition = %q, want RETRY", got)
	}
	if logger.count("scheduler.dispatch_undeclared_disposition") != 1 {
		t.Fatal("an undeclared disposition was not reported")
	}
}

// TestAbandonedClaimIsReturnedToTheReadyPool is SVC-004's restart clause: a
// replica that died holding a claim loses nothing, because the recovery is
// driven entirely by durable rows.
//
// The abandoned state is manufactured exactly as a crash leaves it -- a
// DISPATCHED ready-work row plus a WORKFLOW_INSTANCE lease whose window has
// passed -- and no process memory anywhere knows about it. A live replica's
// next tick observes the lapsed lease, retires it, returns the row to READY and
// (in the same tick) claims and runs it.
func TestAbandonedClaimIsReturnedToTheReadyPool(t *testing.T) {
	ctx := context.Background()
	db, tenantID, cell := newWorld(t, "svc004-recovery")
	clock := newStepClock(fixtureAt)

	instance := park(t, db, tenantID, cell, "svc004-recovery-1", fixtureAt)

	publisher, _ := newReplica(t, db, tenantID, "replica:publisher", testQueue, nil, clock)
	clock.set(fixtureFireAt)
	if _, err := publisher.Tick(ctx); err != nil {
		t.Fatalf("publisher tick: %v", err)
	}

	var readyID uuid.UUID
	var readyVersion int64
	if err := db.QueryRow(ctx, `
		SELECT ready_work_id, ready_version FROM workflow_ready_work
		WHERE tenant_id = $1 AND instance_id = $2`, tenantID, instance.instanceID).Scan(&readyID, &readyVersion); err != nil {
		t.Fatalf("read the woken ready work: %v", err)
	}

	// --- A replica that is about to die claims the work. ---
	dead := lease.Identity{WorkloadRef: "workload:hcmnext-scheduler", InstanceRef: "replica:dead"}
	resource := lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: instance.instanceID.String()}
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := cell.leases.Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: tenantID, Resource: resource, Holder: dead, Now: fixtureFireAt, TTL: time.Minute,
		}); err != nil {
			return err
		}
		return runtimestate.ReadyWorkStore{}.Transition(ctx, tx, tenantID, readyID,
			uint64(readyVersion), runtimestate.ReadyDispatched, time.Time{})
	})
	if state, _ := readyStateOf(t, db, tenantID, readyID); state != runtimestate.ReadyDispatched {
		t.Fatalf("the abandoned row is %s, want DISPATCHED", state)
	}

	// --- It never comes back. An hour later its lease has lapsed. ---
	clock.set(fixtureFireAt.Add(time.Hour))
	var claims int
	survivor, logger := newReplica(t, db, tenantID, "replica:survivor", testQueue,
		DispatcherFunc(func(context.Context, Work) (Disposition, error) {
			claims++
			return DispositionCompleted, nil
		}), clock)

	result, err := survivor.Tick(ctx)
	if err != nil {
		t.Fatalf("survivor tick: %v", err)
	}
	if result.Recovered != 1 {
		t.Fatalf("the survivor recovered %d abandoned claims, want 1", result.Recovered)
	}
	if result.Claimed != 1 || claims != 1 {
		t.Fatalf("recovered work was claimed %d times and dispatched %d times, want 1 and 1", result.Claimed, claims)
	}
	if state, _ := readyStateOf(t, db, tenantID, readyID); state != runtimestate.ReadyDone {
		t.Fatalf("recovered work settled as %s, want DONE", state)
	}
	if logger.count("scheduler.ready_work_recovered") != 1 {
		t.Fatal("the recovery was not reported")
	}

	// The dead replica's lease is retired rather than left as a live claim on
	// an instance nobody is advancing.
	var expired int
	if err := db.QueryRow(ctx, `
		SELECT count(*) FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = 'WORKFLOW_INSTANCE' AND resource_id = $2
		  AND holder_id = $3 AND lease_state = 'EXPIRED'`,
		tenantID, instance.instanceID.String(), dead.HolderID()).Scan(&expired); err != nil {
		t.Fatalf("read the dead replica's lease: %v", err)
	}
	if expired != 1 {
		t.Fatalf("%d expired leases for the dead replica, want 1", expired)
	}
}

// TestQueueLeaseAdmitsOneHolderAndIsRenewed proves the epoch fence behaves as
// an epoch: a second replica is refused rather than given a second live claim,
// and the holder's own later ticks renew the claim it already has instead of
// fighting itself for it (which lease.Manager.Acquire would refuse as ErrHeld
// and which would burn a fence token every tick).
func TestQueueLeaseAdmitsOneHolderAndIsRenewed(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "svc004-queue-lease", fixtureAt)
	clock := newStepClock(fixtureAt)

	first, _ := newReplica(t, db, tenantID, "replica:a", testQueue, nil, clock)
	second, _ := newReplica(t, db, tenantID, "replica:b", testQueue, nil, clock)

	one, err := first.Tick(ctx)
	if err != nil {
		t.Fatalf("first replica tick: %v", err)
	}
	if one.Leased != 1 || one.Refused != 0 {
		t.Fatalf("the first replica leased %d and was refused %d", one.Leased, one.Refused)
	}
	two, err := second.Tick(ctx)
	if err != nil {
		t.Fatalf("second replica tick: %v", err)
	}
	if two.Leased != 0 || two.Refused != 1 {
		t.Fatalf("the second replica leased %d and was refused %d", two.Leased, two.Refused)
	}

	var held, token int64
	if err := db.QueryRow(ctx, `
		SELECT count(*), max(fence_token) FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = 'QUEUE' AND lease_state = 'HELD'`,
		tenantID).Scan(&held, &token); err != nil {
		t.Fatalf("read queue leases: %v", err)
	}
	if held != 1 {
		t.Fatalf("%d live queue leases, want exactly 1", held)
	}

	// A later tick renews rather than re-acquires: the token does not move.
	clock.set(fixtureAt.Add(10 * time.Second))
	again, err := first.Tick(ctx)
	if err != nil {
		t.Fatalf("holder's second tick: %v", err)
	}
	if again.Leased != 1 {
		t.Fatalf("the holder's second tick leased %d, want 1", again.Leased)
	}
	var renewedToken, leases int64
	if err := db.QueryRow(ctx, `
		SELECT count(*), max(fence_token) FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = 'QUEUE'`, tenantID).Scan(&leases, &renewedToken); err != nil {
		t.Fatalf("read queue leases: %v", err)
	}
	if leases != 1 || renewedToken != token {
		t.Fatalf("after a renewal there are %d lease rows at token %d; want 1 row still at token %d",
			leases, renewedToken, token)
	}
}
