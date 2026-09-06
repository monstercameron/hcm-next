package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/intent/app"
)

// BindOutcome implements app.OutcomeBinder. The projection update is a
// compare-and-swap and is idempotent for the exact tuple already stored; a
// different tuple never overwrites the first terminal fact.
func (s *Store) BindOutcome(ctx context.Context, in app.OutcomeBinding) error {
	if in.Tenant == "" {
		return fmt.Errorf("pgstore: bind outcome requires a tenant")
	}
	if in.ExpectedInstanceVersion == 0 {
		return fmt.Errorf("pgstore: bind outcome requires an instance version")
	}
	if err := in.Receipt.Validate(); err != nil {
		return err
	}
	intentID, err := uuid.Parse(in.Receipt.IntentID)
	if err != nil {
		return fmt.Errorf("pgstore: bind outcome intent id: %w", err)
	}
	tenantID := TenantID(in.Tenant)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: begin bind outcome: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	var (
		request, execution, business, consistency, obligation string
		version                                               int64
	)
	err = tx.QueryRow(ctx, `
		SELECT request_state, execution_state, business_state, consistency_state,
			obligation_state, instance_version
		FROM intent_instance
		WHERE tenant_id = $1 AND intent_id = $2
		FOR UPDATE`, tenantID, intentID).Scan(&request, &execution, &business, &consistency, &obligation, &version)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("pgstore: %w", app.ErrIntentNotFound)
		}
		return fmt.Errorf("pgstore: load intent outcome projection: %w", err)
	}
	current, err := app.LifecycleFromColumns(request, execution, business, consistency, obligation)
	if err != nil {
		return err
	}
	if current == in.Receipt.Dimensions {
		return nil
	}
	if uint64(version) != in.ExpectedInstanceVersion {
		return fmt.Errorf("%w: stored instance version %d, expected %d", app.ErrOutcomeProjectionConflict, version, in.ExpectedInstanceVersion)
	}
	nextRequest, nextExecution, nextBusiness, nextConsistency, nextObligation := app.LifecycleColumns(in.Receipt.Dimensions)
	updated, err := tx.Exec(ctx, `
		UPDATE intent_instance
		SET request_state = $3, execution_state = $4, business_state = $5,
			consistency_state = $6, obligation_state = $7,
			instance_version = instance_version + 1,
			recorded_at = $8, last_transition_at = $8,
			commit_receipt_ref = NULLIF($10, ''), repair_ref = NULLIF($11, '')
		WHERE tenant_id = $1 AND intent_id = $2 AND instance_version = $9`,
		tenantID, intentID, nextRequest, nextExecution, nextBusiness, nextConsistency,
		nextObligation, in.Receipt.RecordedAt.UTC(), int64(in.ExpectedInstanceVersion),
		in.Receipt.CommitReceiptRef, in.Receipt.RepairRef)
	if err != nil {
		return fmt.Errorf("pgstore: bind intent outcome: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("%w: concurrent outcome update", app.ErrOutcomeProjectionConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgstore: commit intent outcome: %w", err)
	}
	return nil
}
