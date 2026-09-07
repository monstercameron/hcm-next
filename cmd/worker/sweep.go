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
	return runOutboxLoopWithHandler(ctx, logger, tenants, disp, pollInterval, legacyMessageHandler(logger))
}

type messageHandler func(context.Context, outbox.Record) error

func runOutboxLoopWithHandler(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, disp dispatcher, pollInterval time.Duration, handler messageHandler) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		didWork, err := sweepWithHandler(ctx, logger, tenants, disp, handler)
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
	return sweepWithHandler(ctx, logger, tenants, disp, legacyMessageHandler(logger))
}

func sweepWithHandler(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, disp dispatcher, handler messageHandler) (bool, error) {
	if handler == nil {
		handler = legacyMessageHandler(logger)
	}
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
			if err := handler(ctx, msg); err != nil {
				logger.Error("worker.dispatch_failed", "outbox_id", msg.OutboxID.String(), "error", err.Error())
				if failErr := failClaim(ctx, disp, msg, err); failErr != nil {
					logger.Error("worker.fail_failed", "outbox_id", msg.OutboxID.String(), "error", failErr.Error())
				}
				continue
			}
			if err := ackClaim(ctx, disp, msg); err != nil {
				logger.Error("worker.ack_failed", "outbox_id", msg.OutboxID.String(), "error", err.Error())
			}
		}
	}
	return didWork, nil
}

func legacyMessageHandler(logger bootstrap.Logger) messageHandler {
	return func(_ context.Context, msg outbox.Record) error {
		return dispatch(logger, msg)
	}
}

type fencedDispatcher interface {
	AckClaim(context.Context, outbox.Record) error
	FailClaim(context.Context, outbox.Record, error) error
}

func ackClaim(ctx context.Context, disp dispatcher, msg outbox.Record) error {
	if fenced, ok := disp.(fencedDispatcher); ok {
		return fenced.AckClaim(ctx, msg)
	}
	return disp.Ack(ctx, msg.Tenant, msg.OutboxID)
}

func failClaim(ctx context.Context, disp dispatcher, msg outbox.Record, cause error) error {
	if fenced, ok := disp.(fencedDispatcher); ok {
		return fenced.FailClaim(ctx, msg, cause)
	}
	return disp.Fail(ctx, msg.Tenant, msg.OutboxID, cause)
}

// dispatch is the semantic delivery role's outbox boundary. The durable
// outbox lease and acknowledgement remain here; provider adapters are supplied
// behind internal/connectivity/delivery and never enter this command package.
func dispatch(logger bootstrap.Logger, msg outbox.Record) error {
	logger.Info("worker.messaging_intent_dispatched", "effect", msg.EffectIdentity, "schema", msg.SchemaRef, "bytes", len(msg.Payload))
	return nil
}
