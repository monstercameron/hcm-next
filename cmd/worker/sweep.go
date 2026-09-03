package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/outbox"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

// tenantLister lists the tenants a sweep should check for due outbox work.
// pgxTenantLister (main.go) is the production implementation; tests supply
// their own, avoiding any dependency on a real database.
type tenantLister interface {
	ActiveTenants(ctx context.Context) ([]uuid.UUID, error)
}

// dispatcher claims and completes outbox work for one tenant.
// *outbox.Consumer satisfies this directly.
type dispatcher interface {
	Poll(ctx context.Context, tenant uuid.UUID) ([]outbox.Record, error)
	Ack(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID) error
	Fail(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID, cause error) error
}

// runOutboxLoop sweeps for due outbox work until ctx is canceled, sleeping
// pollInterval between sweeps that found nothing to do. It always returns
// nil: the loop's only exit is ctx being done, which is not itself a
// failure worth reporting to the run-group.
func runOutboxLoop(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, disp dispatcher, pollInterval time.Duration) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		didWork, err := sweep(ctx, logger, tenants, disp)
		if err != nil {
			logger.Error("worker.sweep_failed", "error", err.Error())
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

// sweep dispatches one batch of due messages for every active tenant, and
// reports whether any tenant had work.
func sweep(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, disp dispatcher) (bool, error) {
	ids, err := tenants.ActiveTenants(ctx)
	if err != nil {
		return false, fmt.Errorf("list tenants: %w", err)
	}

	didWork := false
	for _, tenant := range ids {
		batch, err := disp.Poll(ctx, tenant)
		if err != nil {
			logger.Error("worker.poll_failed", "tenant", tenant.String(), "error", err.Error())
			continue
		}
		for _, msg := range batch {
			didWork = true
			if err := dispatch(logger, msg); err != nil {
				logger.Error("worker.dispatch_failed", "outbox_id", msg.OutboxID.String(), "error", err.Error())
				if ackErr := disp.Fail(ctx, tenant, msg.OutboxID, err); ackErr != nil {
					logger.Error("worker.fail_failed", "outbox_id", msg.OutboxID.String(), "error", ackErr.Error())
				}
				continue
			}
			if err := disp.Ack(ctx, tenant, msg.OutboxID); err != nil {
				logger.Error("worker.ack_failed", "outbox_id", msg.OutboxID.String(), "error", err.Error())
			}
		}
	}
	return didWork, nil
}

// dispatch delivers one outbox message. P1A has no external distribution
// target wired yet (no funded consumer reads the outbox outside this
// process); this composition root logs the delivery so the at-least-once,
// restart-safe mechanics are exercised end to end in the real binary. A real
// destination (a queue, a webhook) plugs in here without changing
// internal/data/outbox. SVC-010 will eventually give worker a real
// messaging-delivery role; until that lands, this stays a plain, logging
// consumer rather than growing provider-specific behavior of its own.
func dispatch(logger bootstrap.Logger, msg outbox.Record) error {
	logger.Info("worker.dispatched", "effect", msg.EffectIdentity, "schema", msg.SchemaRef, "bytes", len(msg.Payload))
	return nil
}
