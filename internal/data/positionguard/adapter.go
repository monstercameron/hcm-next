package positionguard

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Adapter is the concrete internal/domains/promotion.PositionReservationAdmitter
// this package supplies: PROMOUX-004's ground five (reservation ownership),
// backed by the real database guarantee [Admit] documents.
type Adapter struct {
	// DB opens the tenant-scoped transaction each admission runs inside.
	DB dbport.Beginner
	// TenantUUID resolves the domain's logical tenant key to the physical
	// tenant UUID this cell's tables key on, mirroring
	// internal/data/positionstore.Store's identical need.
	TenantUUID func(values.TenantId) uuid.UUID
}

var _ promotion.PositionReservationAdmitter = Adapter{}

// AdmitPositionSlot implements promotion.PositionReservationAdmitter. It
// opens its own tenant-scoped transaction, commits (or lets a conflict roll
// back) before returning, and reports the ownership decision as a plain
// boolean: true for a fresh admission or a replay of the caller's own
// idempotency key, false when a different proposal already holds the slot.
// Only a genuine failure to decide -- a malformed request, an unconfigured
// adapter, or a database error -- is returned as an error.
func (a Adapter) AdmitPositionSlot(ctx context.Context, req promotion.PositionSlotAdmitRequest) (bool, error) {
	if err := req.Validate(); err != nil {
		return false, err
	}
	if a.DB == nil || a.TenantUUID == nil {
		return false, fmt.Errorf("%w: adapter has no database and tenant resolver configured", ErrInvalid)
	}
	tenantID := a.TenantUUID(req.Tenant)
	if tenantID == uuid.Nil {
		return false, fmt.Errorf("%w: tenant %s has no uuid", ErrInvalid, req.Tenant)
	}

	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("positionguard: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return false, fmt.Errorf("positionguard: scope tenant: %w", err)
	}

	guardID := uuid.New()
	_, admitErr := Admit(ctx, tx, tenantID, guardID, req.Position.String(), req.EffectiveDate.String(), req.ProposalRef, req.IdempotencyKey)
	if admitErr != nil {
		if errors.Is(admitErr, ErrActiveConflict) {
			// The rollback in the deferred func above is what keeps this
			// refusal free of side effects: the reservation attempt itself
			// never commits.
			return false, nil
		}
		return false, fmt.Errorf("positionguard: admit position slot: %w", admitErr)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("positionguard: commit: %w", err)
	}
	committed = true
	return true, nil
}
