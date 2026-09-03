package main

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/projection"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

// dueLister lists the (tenant, projection, stream) checkpoints one sweep
// should attempt to catch up. pgxProjectionLister (main.go) is the
// production implementation; tests supply their own, avoiding any
// dependency on a real database.
type dueLister interface {
	Due(ctx context.Context) ([]projection.StreamProjection, error)
}

// oneReconciler catches up a single checkpoint. *projection.Reconciler
// satisfies this directly.
type oneReconciler interface {
	ReconcileOne(ctx context.Context, target projection.StreamProjection) (int, error)
}

// runReconcileLoop sweeps for projections to catch up until ctx is
// canceled, sleeping pollInterval between sweeps that found nothing to do.
// It always returns nil: the loop's only exit is ctx being done, which is
// not itself a failure worth reporting to the run-group.
func runReconcileLoop(ctx context.Context, logger bootstrap.Logger, lister dueLister, reconciler oneReconciler, pollInterval time.Duration) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		didWork, err := reconcileSweep(ctx, logger, lister, reconciler)
		if err != nil {
			logger.Error("projector.sweep_failed", "error", err.Error())
		}
		if didWork {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
}

// reconcileSweep catches up every projection lister reports as due, and
// reports whether any work was found.
func reconcileSweep(ctx context.Context, logger bootstrap.Logger, lister dueLister, reconciler oneReconciler) (bool, error) {
	due, err := lister.Due(ctx)
	if err != nil {
		return false, fmt.Errorf("list due: %w", err)
	}

	didWork := false
	for _, target := range due {
		applied, err := reconciler.ReconcileOne(ctx, target)
		if err != nil {
			logger.Error("projector.reconcile_failed", "projection", target.ProjectionName, "stream", target.StreamKey, "error", err.Error())
			continue
		}
		if applied > 0 {
			didWork = true
			logger.Info("projector.caught_up", "projection", target.ProjectionName, "stream", target.StreamKey, "applied", applied)
		}
	}
	return didWork, nil
}
