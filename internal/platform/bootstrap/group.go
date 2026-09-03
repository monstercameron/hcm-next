package bootstrap

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
)

// Workload is one concurrent unit of work a role runs under Run's run-group.
// Run implementations must return promptly once ctx is canceled; the
// run-group's only enforcement of that is the shutdown deadline applied
// around the whole shutdown sequence, not Workload.Run itself.
type Workload struct {
	// Name identifies this workload in shutdown-order logs and in any
	// error the group reports.
	Name string
	// Run performs the workload. Returning nil means "finished
	// successfully and will not be restarted"; returning a non-nil error
	// (including ctx.Err() from an ignored cancellation) is treated as a
	// role failure that cancels every sibling workload.
	Run func(ctx context.Context) error
}

// group runs a fixed set of Workloads concurrently, cancels every sibling
// on the first error (including a recovered panic), and never blocks
// Wait() past every goroutine actually returning — a panicking Workload
// converts to an error instead of hanging or crashing the process.
type group struct {
	ctx    context.Context
	cancel context.CancelFunc

	wg  sync.WaitGroup
	mu  sync.Mutex
	err error
}

// newGroup derives a cancelable context from parent for the group's
// workloads: canceling it (via cancel, or via parent itself being
// canceled) is how every sibling learns to stop.
func newGroup(parent context.Context) *group {
	ctx, cancel := context.WithCancel(parent)
	return &group{ctx: ctx, cancel: cancel}
}

// go starts w. It always calls wg.Done, even if w.Run panics, so Wait can
// never hang on a panicking workload.
func (g *group) goRun(w Workload) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				g.fail(fmt.Errorf("role %q panicked: %v\n%s", w.Name, r, debug.Stack()))
			}
		}()
		if err := w.Run(g.ctx); err != nil {
			g.fail(fmt.Errorf("role %q: %w", w.Name, err))
		}
	}()
}

// fail records err as the group's failure (first writer wins) and cancels
// every sibling workload's context.
func (g *group) fail(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.err == nil {
		g.err = err
		g.cancel()
	}
}

// wait blocks until every workload has returned (successfully, with an
// error, or via a recovered panic) and reports the first failure, if any.
func (g *group) wait() error {
	g.wg.Wait()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.err
}
