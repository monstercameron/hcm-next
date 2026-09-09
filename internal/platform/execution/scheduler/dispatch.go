package scheduler

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// Work is one claimed unit of durable ready work, handed to a [Dispatcher].
//
// It is the durable row plus the fence the claim was taken under, and nothing
// else: no plan, no node, no outcome. Everything a dispatcher needs to know
// about *what* to run it reads from Row (the instance, the node, the attempt);
// everything it needs to *be allowed* to run it is Fence.
type Work struct {
	// Row is the workflow_ready_work row, exactly as the durable store spells
	// it, at the version the claim moved it to (DISPATCHED).
	Row runtimestate.ReadyWork
	// Fence is the WORKFLOW_INSTANCE lease this replica took before it moved
	// the row. A dispatcher advancing the instance presents it -- through
	// [lease.Fence.RuntimeFence] into execute.Options -- so a replica that
	// stalled and lost the instance completes nothing.
	Fence lease.Fence
}

// Disposition is what a dispatcher did with one unit of work, and it decides
// the durable state the row is settled into.
type Disposition string

const (
	// DispositionCompleted settles the row DONE. The work was carried out;
	// nothing will run it again.
	DispositionCompleted Disposition = "COMPLETED"
	// DispositionRetry returns the row to READY so a later tick (this replica
	// or another) picks it up again. It is the disposition for a transient
	// failure, and it is also what a returned error produces.
	DispositionRetry Disposition = "RETRY"
	// DispositionAbandoned settles the row CANCELLED. The work is not to be
	// attempted again -- the dispatcher decided it is permanently
	// undispatchable, and leaving it READY would be an infinite loop.
	DispositionAbandoned Disposition = "ABANDONED"
)

// Valid reports a declared disposition.
func (d Disposition) Valid() bool {
	switch d {
	case DispositionCompleted, DispositionRetry, DispositionAbandoned:
		return true
	default:
		return false
	}
}

// settleState maps a disposition onto the durable ready-work state it settles
// into. The second result reports whether that state is terminal, which is
// what decides whether the row carries a completion instant.
func (d Disposition) settleState() (state string, terminal bool, err error) {
	switch d {
	case DispositionCompleted:
		return runtimestate.ReadyDone, true, nil
	case DispositionRetry:
		return runtimestate.ReadyReady, false, nil
	case DispositionAbandoned:
		return runtimestate.ReadyCancelled, true, nil
	default:
		return "", false, fmt.Errorf("%w: disposition %q is not COMPLETED, RETRY or ABANDONED", ErrConfig, string(d))
	}
}

// Dispatcher runs one claimed unit of work.
//
// It is a port, not an implementation, and that is the whole boundary this
// package draws: turning a ready-work row into an
// internal/workflow/execute.Driver call needs the pinned compiled plan and the
// typed step resolution the node produced, which are workflow semantics this
// package must not contain (SVC-004's REFACTOR clause). A composition root
// implements it over a real driver.
//
// It is called outside the transaction that claimed the work, so a dispatcher
// opens and commits its own transactions -- exactly what execute.Driver does.
// It must be safe to call concurrently only if the Scheduler running it was
// configured to dispatch concurrently; the loop in this package is serial.
//
// A returned error is treated as [DispositionRetry]: the row goes back to
// READY and the instance lease is released, so nothing is stranded by a
// dispatcher that failed.
type Dispatcher interface {
	Dispatch(ctx context.Context, work Work) (Disposition, error)
}

// DispatcherFunc adapts a function to [Dispatcher].
type DispatcherFunc func(ctx context.Context, work Work) (Disposition, error)

// Dispatch calls f.
func (f DispatcherFunc) Dispatch(ctx context.Context, work Work) (Disposition, error) {
	return f(ctx, work)
}

var _ Dispatcher = DispatcherFunc(nil)
