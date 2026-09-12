package positionguard

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// ErrInvalid means a required input was empty, zero, or malformed.
var ErrInvalid = errors.New("positionguard: invalid input")

// ErrActiveConflict is what [Admit] returns when a different proposal
// already holds the (position, effective date) window: the caller's
// idempotency key does not match the reservation already recorded for that
// window, so this is a genuinely different, conflicting proposal rather
// than a replay of the same one.
var ErrActiveConflict = errors.New("positionguard: another proposal already holds this position for this effective date")

// Decision is what [Admit] resolved.
type Decision struct {
	// GuardID is the reservation row's own identity: the caller's own
	// minted id on a fresh admission, or the id of the row a replay
	// matched.
	GuardID uuid.UUID
	// ProposalRef is the proposal the row protects: the caller's own on a
	// fresh admission, or the original proposal's on a replay or conflict
	// probe.
	ProposalRef string
	// Replay is true when this call matched a reservation already recorded
	// under the same idempotency key, rather than creating a new one.
	Replay bool
}

func requireField(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is empty", ErrInvalid, field)
	}
	return nil
}

// Admit is the one write that decides admission. See the package doc for
// the full argument; in one sentence, it is never a SELECT followed by a
// conditional INSERT -- the single statement below is itself the decision,
// and migrations/00287's partial unique index is what makes that decision
// safe under real concurrency (proven by TestTodo_PROMOUX_004_Race against
// real concurrent PostgreSQL sessions).
func Admit(
	ctx context.Context, ex Executor,
	tenantID, guardID uuid.UUID, positionRef, effectiveDate, proposalRef, idempotencyKey string,
) (Decision, error) {
	if tenantID == uuid.Nil {
		return Decision{}, fmt.Errorf("%w: tenant id is nil", ErrInvalid)
	}
	if guardID == uuid.Nil {
		return Decision{}, fmt.Errorf("%w: guard id is nil", ErrInvalid)
	}
	if err := requireField("position_ref", positionRef); err != nil {
		return Decision{}, err
	}
	if err := requireField("effective_date", effectiveDate); err != nil {
		return Decision{}, err
	}
	if err := requireField("proposal_ref", proposalRef); err != nil {
		return Decision{}, err
	}
	if err := requireField("idempotency_key", idempotencyKey); err != nil {
		return Decision{}, err
	}
	if ex == nil {
		return Decision{}, fmt.Errorf("%w: executor is nil", ErrInvalid)
	}

	var (
		rowGuardID     uuid.UUID
		rowProposalRef string
	)
	err := ex.QueryRow(ctx, `
		INSERT INTO promotion_target_position_guard
			(tenant_id, guard_id, position_ref, effective_date, proposal_ref, idempotency_key, status, opened_at)
		VALUES ($1, $2, $3, $4::date, $5, $6, 'ACTIVE', now())
		ON CONFLICT (tenant_id, position_ref, effective_date) WHERE status = 'ACTIVE'
		DO UPDATE SET opened_at = promotion_target_position_guard.opened_at
		WHERE promotion_target_position_guard.idempotency_key = EXCLUDED.idempotency_key
		RETURNING guard_id, proposal_ref`,
		tenantID, guardID, strings.TrimSpace(positionRef), effectiveDate, strings.TrimSpace(proposalRef), strings.TrimSpace(idempotencyKey),
	).Scan(&rowGuardID, &rowProposalRef)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			// The INSERT's own conflict path ran and its DO UPDATE's WHERE
			// clause matched nothing: a row already claims this window
			// under a different idempotency key. Nothing was written --
			// the statement above is the only mutating statement this
			// function issues, and it affected zero rows here.
			return Decision{}, ErrActiveConflict
		}
		return Decision{}, fmt.Errorf("positionguard: admit: %w", err)
	}

	return Decision{GuardID: rowGuardID, ProposalRef: rowProposalRef, Replay: rowGuardID != guardID}, nil
}

// Release closes the ACTIVE reservation for position and effective date,
// freeing the window for a future proposal. It is idempotent: closing an
// already-CLOSED or nonexistent reservation affects zero rows rather than
// erroring.
//
// Nothing in this package's own tests wires Release to fire automatically
// when a proposal reaches a terminal stage -- the same boundary
// promotionguard's own package doc names for its Release, and for the same
// reason: there is no single existing write-time "this proposal just went
// terminal" event in the workflow runtime to hook it to.
func Release(ctx context.Context, ex Executor, tenantID uuid.UUID, positionRef, effectiveDate string) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant id is nil", ErrInvalid)
	}
	if err := requireField("position_ref", positionRef); err != nil {
		return err
	}
	if err := requireField("effective_date", effectiveDate); err != nil {
		return err
	}
	if ex == nil {
		return fmt.Errorf("%w: executor is nil", ErrInvalid)
	}
	_, err := ex.Exec(ctx, `
		UPDATE promotion_target_position_guard
		SET status = 'CLOSED', closed_at = now()
		WHERE tenant_id = $1 AND position_ref = $2 AND effective_date = $3::date AND status = 'ACTIVE'`,
		tenantID, strings.TrimSpace(positionRef), effectiveDate,
	)
	if err != nil {
		return fmt.Errorf("positionguard: release: %w", err)
	}
	return nil
}
