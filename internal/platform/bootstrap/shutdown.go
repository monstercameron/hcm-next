package bootstrap

import (
	"context"
	"fmt"
	"time"
)

// DefaultShutdownDeadline bounds Run's shutdown sequence when Spec leaves
// ShutdownDeadline unset or non-positive.
const DefaultShutdownDeadline = 30 * time.Second

// ShutdownStep is one named, ordered action in the graceful-shutdown
// sequence. Steps run strictly in the order they appear, never
// concurrently, so a later step (for example closing a database pool) can
// safely assume every earlier step (for example draining workloads that
// still use that pool) has already finished.
type ShutdownStep struct {
	Name string
	Run  func(ctx context.Context) error
}

// ErrShutdownDeadlineExceeded is returned by runShutdown when deadline
// elapses before every step finished. Steps already completed stay
// completed; any step in flight when the deadline hit is abandoned (its
// goroutine keeps running against a canceled context, which every Workload
// and built-in step in this package respects).
type ErrShutdownDeadlineExceeded struct {
	Deadline time.Duration
	AtStep   string // name of the step that was running, or about to run, when the deadline hit; "" if unknown
}

func (e *ErrShutdownDeadlineExceeded) Error() string {
	if e.AtStep == "" {
		return fmt.Sprintf("bootstrap: shutdown deadline (%s) exceeded", e.Deadline)
	}
	return fmt.Sprintf("bootstrap: shutdown deadline (%s) exceeded at step %q", e.Deadline, e.AtStep)
}

// runShutdown runs steps in order under one overall deadline. It returns
// promptly at the deadline even if the step currently running ignores its
// context and never returns — the deadline is enforced by this function
// returning, not by forcibly stopping the step's goroutine (Go has no such
// mechanism), so a hard-deadline caller must still treat the process as
// shutting down once runShutdown returns.
//
// parent should not itself already be in the process of being canceled for
// an unrelated reason (Run detaches from the triggering signal/role-failure
// context with context.WithoutCancel before calling this, so the shutdown
// deadline is the only thing that can end this call early).
func runShutdown(parent context.Context, logger Logger, steps []ShutdownStep, deadline time.Duration) error {
	if logger == nil {
		logger = defaultLogger()
	}
	if deadline <= 0 {
		deadline = DefaultShutdownDeadline
	}

	deadlineCtx, cancel := context.WithTimeout(parent, deadline)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		for _, step := range steps {
			select {
			case <-deadlineCtx.Done():
				done <- &ErrShutdownDeadlineExceeded{Deadline: deadline, AtStep: step.Name}
				return
			default:
			}
			if err := step.Run(deadlineCtx); err != nil {
				done <- fmt.Errorf("bootstrap: shutdown step %q: %w", step.Name, err)
				return
			}
			logger.Info("bootstrap.shutdown.step", "step", step.Name)
		}
		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-deadlineCtx.Done():
		return &ErrShutdownDeadlineExceeded{Deadline: deadline}
	}
}
