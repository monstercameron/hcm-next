package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
	"github.com/monstercameron/hcm-next/internal/workflow/lease"
)

func TestTodo_SVC_005(t *testing.T) {
	roles := DefaultRoleConfig()
	if err := roles.Validate(); err != nil {
		t.Fatal(err)
	}
	roles.TimerEnabled = false
	if err := roles.Validate(); err != nil {
		t.Fatal(err)
	}
	if roles.SignalShard == "" {
		t.Fatal("signal role lost its independent shard")
	}
	roles.SignalEnabled = false
	if err := roles.Validate(); err == nil {
		t.Fatal("disabled both roles accepted")
	}

	role := &recordingSignalRole{}
	count, err := runSignalRole(context.Background(), role,
		lease.AcquireRequest{TenantID: uuid.New(), Resource: lease.Resource{Kind: lease.ResourceQueue, ID: "queue-a"}},
		lease.Fence{Token: 7}, time.Now().UTC(), "signal-west")
	role.mu.Lock()
	fenced, fenceToken := role.fenced, role.fenceToken
	role.mu.Unlock()
	if err != nil || count != 1 || fenced != 1 || fenceToken != 7 {
		t.Fatalf("fenced signal role = count %d, err %v, fenced %d, token %d; want 1, nil, 1, 7",
			count, err, fenced, fenceToken)
	}
}

func TestTodo_SVC_005_Race(t *testing.T) {
	role := &recordingSignalRole{}
	claim := lease.AcquireRequest{TenantID: uuid.New(), Resource: lease.Resource{Kind: lease.ResourceQueue, ID: "queue-a"}}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = role.RunSignalRole(context.Background(), claim, time.Now().UTC(), "shard-a")
		}()
	}
	wg.Wait()
	role.mu.Lock()
	calls := role.calls
	role.mu.Unlock()
	if calls != 4 {
		t.Fatalf("signal role calls = %d, want 4", calls)
	}
}

func TestTodo_SVC_005_Integration(t *testing.T) {
	fields := append(ConfigFields(), RoleConfigFields()...)
	v, err := bootstrap.ParseConfig([]string{"-database-url=postgres://example", "-tenant-id=" + uuid.NewString(), "-timer-role=false", "-signal-role=true", "-signal-shard=signal-west"}, nil, fields)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := RolesFrom(v)
	if err != nil {
		t.Fatal(err)
	}
	if roles.TimerEnabled || !roles.SignalEnabled || roles.SignalShard != "signal-west" {
		t.Fatalf("roles = %+v", roles)
	}
	if err := ValidateConfig(v); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_SVC_005_Integration_SignalRoleRequiresQueueLease pins the signal
// role at Scheduler.serve, where the queue fence is acquired. A signal role
// must not run for a refused queue, and must run after the same replica takes
// the queue on a later serve cycle.
func TestTodo_SVC_005_Integration_SignalRoleRequiresQueueLease(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "svc005-signal-lease", fixtureAt)
	claim := claimFixture(tenant, "replica:signal")

	blocker := db.NewConn(t)
	if _, err := blocker.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	blockedClaim := claim
	blockedClaim.Holder.InstanceRef = "replica:blocker"
	blockedClaim.Now = fixtureAt
	blockedClaim.TTL = time.Hour
	tx, err := blocker.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin blocker lease: %v", err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("scope blocker lease: %v", err)
	}
	grant, err := (lease.Manager{}).Acquire(context.Background(), tx, blockedClaim)
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("acquire blocker lease: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit blocker lease: %v", err)
	}

	role := &recordingSignalRole{}
	s, err := New(Config{
		DB:          appConn(t, db),
		Claims:      []lease.AcquireRequest{claim},
		Leases:      lease.Manager{},
		Misfire:     catchUpOnce(),
		Roles:       RoleConfig{TimerEnabled: false, SignalEnabled: true},
		SignalRole:  role,
		Clock:       func() time.Time { return fixtureAt },
		BatchSize:   1,
		QueueTTL:    time.Minute,
		InstanceTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	first, err := s.serve(context.Background(), claim, fixtureAt)
	if err != nil {
		t.Fatalf("serve while another holder owns queue: %v", err)
	}
	if first.Refused != 1 || first.Signals != 0 {
		t.Fatalf("refused serve = %+v, want one refusal and no signal", first)
	}

	releaseTx, err := blocker.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin blocker release: %v", err)
	}
	if err := tenancy.WithTenant(context.Background(), releaseTx, tenant); err != nil {
		_ = releaseTx.Rollback(context.Background())
		t.Fatalf("scope blocker release: %v", err)
	}
	if _, err := (lease.Manager{}).Release(context.Background(), releaseTx, grant.Fence, fixtureAt); err != nil {
		_ = releaseTx.Rollback(context.Background())
		t.Fatalf("release blocker lease: %v", err)
	}
	if err := releaseTx.Commit(context.Background()); err != nil {
		t.Fatalf("commit blocker release: %v", err)
	}

	second, err := s.serve(context.Background(), claim, fixtureAt.Add(time.Second))
	if err != nil {
		t.Fatalf("serve after queue lease became available: %v", err)
	}
	role.mu.Lock()
	fenced := role.fenced
	role.mu.Unlock()
	if second.Leased != 1 || second.Signals != 1 || fenced != 1 {
		t.Fatalf("leased serve = %+v, fenced signal calls = %d; want one lease and one signal", second, fenced)
	}
}

type recordingSignalRole struct {
	mu         sync.Mutex
	calls      int
	fenced     int
	fenceToken uint64
}

func (r *recordingSignalRole) RunSignalRole(context.Context, lease.AcquireRequest, time.Time, string) (int, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return 1, nil
}

func (r *recordingSignalRole) RunFencedSignalRole(_ context.Context, _ lease.AcquireRequest, fence lease.Fence, _ time.Time, _ string) (int, error) {
	r.mu.Lock()
	r.fenced++
	r.fenceToken = fence.Token
	r.mu.Unlock()
	return 1, nil
}
