package timer

import (
	"context"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Reader is the production [execute.TimerReader]: it loads the durable timer
// a resumed WAIT node advances from, through this package's own [Scheduler.Load],
// inside the caller's transaction.
//
// It lives here rather than in internal/platform/execution because the port's
// signature names the identifier type this package already owns the right to
// import (definitions/architecture/dependency-roles.yaml confines that module
// to the workflow roots, not to internal/platform); the platform composition
// root only forwards a value of it.
type Reader struct {
	Scheduler Scheduler
}

var _ execute.TimerReader = Reader{}

// LoadTimer projects the durable row onto the shape the driver compares
// against its pinned plan. Every field the driver's drift check reads
// (instance, node, key, state, fire instant) is copied from the row, never
// from the request.
func (r Reader) LoadTimer(ctx context.Context, ex runtime.Executor, tenantID, timerID uuid.UUID) (execute.FiredTimer, error) {
	row, err := r.Scheduler.Load(ctx, ex, tenantID, timerID)
	if err != nil {
		return execute.FiredTimer{}, err
	}
	return execute.FiredTimer{
		TimerID:    row.TimerID,
		InstanceID: row.InstanceID,
		NodeID:     row.NodeID,
		Key:        row.Key,
		State:      row.State,
		FiresAt:    row.FiresAt,
	}, nil
}
