package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	executionscheduler "github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
)

func TestTodo_PROMO_EXEC_TIMERDISPATCH_TimerDispatchDisposition(t *testing.T) {
	if got := timerDispatchDisposition(app.ExecutionResult{InstanceID: "i"}, nil); got != executionscheduler.DispositionCompleted {
		t.Fatalf("success disposition = %q", got)
	}
	if got := timerDispatchDisposition(app.ExecutionResult{}, errors.New("transient")); got != executionscheduler.DispositionRetry {
		t.Fatalf("error disposition = %q", got)
	}
	if got := timerDispatchDisposition(app.ExecutionResult{}, nil); got != executionscheduler.DispositionAbandoned {
		t.Fatalf("empty result disposition = %q", got)
	}
}

func TestTodo_PROMO_EXEC_TIMERDISPATCH_TimerDispatcherRejectsForeignTenant(t *testing.T) {
	configured := pgstore.TenantID("tenant-a")
	foreign := pgstore.TenantID("tenant-b")
	called := false
	dispatcher := timerDispatcher(configured.String(), "tenant-a", func(context.Context, string, string, int) (app.ExecutionResult, error) {
		called = true
		return app.ExecutionResult{InstanceID: "unexpected"}, nil
	})
	disposition, err := dispatcher.Dispatch(context.Background(), executionscheduler.Work{Row: runtimestate.ReadyWork{TenantID: foreign}})
	if err != nil || disposition != executionscheduler.DispositionAbandoned {
		t.Fatalf("foreign dispatch = %q, %v; want ABANDONED without calling the resumer", disposition, err)
	}
	if called {
		t.Fatal("foreign tenant reached timer resumer")
	}
}
