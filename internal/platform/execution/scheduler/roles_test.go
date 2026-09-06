package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

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
	if role.calls != 4 {
		t.Fatalf("signal role calls = %d, want 4", role.calls)
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

type recordingSignalRole struct {
	mu    sync.Mutex
	calls int
}

func (r *recordingSignalRole) RunSignalRole(context.Context, lease.AcquireRequest, time.Time, string) (int, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return 1, nil
}
