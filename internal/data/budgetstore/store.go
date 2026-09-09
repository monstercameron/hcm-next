// Package budgetstore persists the compensation-reservation state machine from
// internal/domains/budget. A Store is scoped to one tenant and opens a short
// transaction for each domain operation; the transaction sets the session
// tenant before touching either row-level-security protected table.
package budgetstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	CodeDuplicate  = "BUDGET_RESERVATION_DUPLICATE"
	CodeNotFound   = "BUDGET_RESERVATION_NOT_FOUND"
	CodeStaleFence = "BUDGET_RESERVATION_STALE_FENCE"
	CodeTenant     = "BUDGET_RESERVATION_TENANT_MISMATCH"
	CodeSequence   = "BUDGET_RESERVATION_SEQUENCE_CONFLICT"
	CodeStorage    = "BUDGET_RESERVATION_STORAGE"
)

// CodedError preserves the domain error while exposing a stable storage
// disposition code to telemetry and fault tests.
type CodedError struct {
	code  string
	cause error
}

func (e *CodedError) Error() string { return e.code + ": " + e.cause.Error() }
func (e *CodedError) Unwrap() error { return e.cause }
func (e *CodedError) Code() string  { return e.code }

// CodeOf returns the first typed storage code carried by err, or an empty
// string when err is not a budgetstore-coded refusal.
func CodeOf(err error) string {
	if err == nil {
		return ""
	}
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

// Store is a tenant-scoped PostgreSQL implementation of budget.ReservationPort.
// The supplied Beginner must already be connected as a role that can assume
// hcmnext_app (the normal composition-root arrangement); Store itself only
// establishes the transaction-local tenant setting.
type Store struct {
	db     dbport.Beginner
	tenant uuid.UUID
}

var _ budget.ReservationPort = (*Store)(nil)

// New constructs a store bound to tenant. A nil UUID is rejected by every
// operation, keeping an accidentally unscoped repository fail closed.
func New(db dbport.Beginner, tenant uuid.UUID) *Store {
	return &Store{db: db, tenant: tenant}
}

// NewStore is the descriptive constructor spelling used by data packages.
func NewStore(db dbport.Beginner, tenant uuid.UUID) *Store { return New(db, tenant) }

func (s *Store) withTx(fn func(context.Context, dbport.Tx) error) error {
	if s == nil || s.db == nil || s.tenant == uuid.Nil {
		return &CodedError{code: CodeTenant, cause: errors.New("budgetstore: a non-nil tenant and database are required")}
	}
	ctx := context.Background()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return &CodedError{code: CodeStorage, cause: err}
	}
	if err := tenancy.WithTenant(ctx, tx, s.tenant); err != nil {
		_ = tx.Rollback(ctx)
		return &CodedError{code: CodeStorage, cause: err}
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return &CodedError{code: CodeStorage, cause: err}
	}
	return nil
}

func (s *Store) checkRequest(r budget.CompensationReservationRequest, now time.Time) error {
	if err := r.Validate(now); err != nil {
		return err
	}
	tenantID, err := uuid.Parse(r.TenantID)
	if err != nil || tenantID != s.tenant {
		return &CodedError{code: CodeTenant, cause: fmt.Errorf("budgetstore: request tenant %q is not this store's tenant", r.TenantID)}
	}
	return nil
}

func classifyStorage(err error, fallback error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return &CodedError{code: CodeDuplicate, cause: fallback}
		case "23514":
			return &CodedError{code: CodeStorage, cause: err}
		case "23503":
			return &CodedError{code: CodeStorage, cause: err}
		}
	}
	return &CodedError{code: CodeStorage, cause: err}
}

const reservationColumns = `row_id, tenant_id, budget_id, proposal_digest,
    amount::text, currency, authority_digest, idempotency_key, expires_at,
    state, fence, created_at, updated_at`

// storageDigest bridges the domain's tagged digest to migration 00002's
// content_digest domain, which stores bare lowercase hexadecimal text.
func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func scanReservation(row dbport.Row) (budget.CompensationReservation, error) {
	var (
		rowID, tenantID            uuid.UUID
		budgetID, currency         string
		proposalDigest, authority  *string
		amountText, idempotencyKey string
		expiresAt                  *time.Time
		state                      string
		fence                      int64
		createdAt, updatedAt       time.Time
	)
	if err := row.Scan(&rowID, &tenantID, &budgetID, &proposalDigest, &amountText, &currency,
		&authority, &idempotencyKey, &expiresAt, &state, &fence, &createdAt, &updatedAt); err != nil {
		return budget.CompensationReservation{}, err
	}
	if fence < 0 {
		return budget.CompensationReservation{}, fmt.Errorf("budgetstore: negative fence %d", fence)
	}
	amount, err := values.NewDecimal(amountText, 4, values.RoundingExactRequired)
	if err != nil {
		return budget.CompensationReservation{}, fmt.Errorf("budgetstore: decode amount: %w", err)
	}
	request := budget.CompensationReservationRequest{
		TenantID: tenantID.String(), BudgetID: budgetID, Amount: amount, Currency: currency,
		IdempotencyKey: idempotencyKey,
	}
	if proposalDigest != nil {
		request.ProposalDigest = domainDigest(*proposalDigest)
	}
	if authority != nil {
		request.AuthorityDigest = domainDigest(*authority)
	}
	if expiresAt != nil {
		request.ExpiresAt = expiresAt.UTC()
	}
	return budget.CompensationReservation{
		ID: rowID.String(), Request: request, State: budget.ReservationState(state),
		Fence: uint64(fence), CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}, nil
}

func (s *Store) loadByID(ctx context.Context, q dbport.Querier, id uuid.UUID, lock bool) (budget.CompensationReservation, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	row := q.QueryRow(ctx, `SELECT `+reservationColumns+`
        FROM compensation_reservation
        WHERE tenant_id=$1 AND row_id=$2`+suffix, s.tenant, id)
	return scanReservation(row)
}

func (s *Store) loadByKey(ctx context.Context, q dbport.Querier, key string) (budget.CompensationReservation, error) {
	row := q.QueryRow(ctx, `SELECT `+reservationColumns+`
        FROM compensation_reservation
        WHERE tenant_id=$1 AND idempotency_key=$2`, s.tenant, key)
	return scanReservation(row)
}

func sameRequest(a budget.CompensationReservation, r budget.CompensationReservationRequest) bool {
	return a.Request.TenantID == r.TenantID && a.Request.BudgetID == r.BudgetID &&
		a.Request.ProposalDigest == r.ProposalDigest && a.Request.Amount.Equal(r.Amount) &&
		a.Request.Currency == r.Currency && a.Request.AuthorityDigest == r.AuthorityDigest &&
		a.Request.IdempotencyKey == r.IdempotencyKey && a.Request.ExpiresAt.Equal(r.ExpiresAt)
}

func (s *Store) lockBudget(ctx context.Context, ex dbport.Execer, budgetID string) error {
	// PostgreSQL text values cannot contain NUL bytes. Use the two-key form of
	// the lock's input framing instead: each value is hex-encoded in SQL before
	// the pair is hashed, so tenant and budget remain separate without
	// constructing a NUL-delimited text value.
	_, err := ex.Exec(ctx, `
        SELECT pg_advisory_xact_lock(hashtextextended(
            encode(convert_to($1, 'UTF8'), 'hex') || ':' ||
            encode(convert_to($2, 'UTF8'), 'hex'), 0))`,
		s.tenant.String(), budgetID)
	return err
}

// Reserve records a HELD reservation and its first event atomically. The
// advisory lock serializes capacity checks for one tenant/budget pair; the
// unique idempotency key remains the durable race guard for retries.
func (s *Store) Reserve(r budget.CompensationReservationRequest, a budget.CompensationBudgetAuthority, now time.Time) (out budget.CompensationReservation, err error) {
	if err := s.checkRequest(r, now); err != nil {
		return budget.CompensationReservation{}, err
	}
	if err := a.Validate(); err != nil {
		return budget.CompensationReservation{}, err
	}
	if a.BudgetID != r.BudgetID || a.AuthorityDigest != r.AuthorityDigest {
		return budget.CompensationReservation{}, budget.ErrAuthorityMismatch
	}
	if a.Currency != r.Currency {
		return budget.CompensationReservation{}, budget.ErrCurrencyMismatch
	}
	if err := s.withTx(func(ctx context.Context, tx dbport.Tx) error {
		if err := s.lockBudget(ctx, tx, r.BudgetID); err != nil {
			return classifyStorage(err, err)
		}
		if existing, lookupErr := s.loadByKey(ctx, tx, r.IdempotencyKey); lookupErr == nil {
			if sameRequest(existing, r) {
				out = existing
				return nil
			}
			return &CodedError{code: CodeDuplicate, cause: budget.ErrReservationConflict}
		} else if !errors.Is(lookupErr, dbport.ErrNoRows) {
			return classifyStorage(lookupErr, lookupErr)
		}

		var over bool
		if err := tx.QueryRow(ctx, `
            SELECT COALESCE(SUM(amount), 0) + $3::numeric > $2::numeric
            FROM compensation_reservation
            WHERE tenant_id=$1 AND budget_id=$4
              AND state IN ('REQUESTED', 'HELD', 'RECONCILIATION_REQUIRED')`,
			s.tenant, a.Available.String(), r.Amount.String(), r.BudgetID).Scan(&over); err != nil {
			return classifyStorage(err, err)
		}
		if over {
			return budget.ErrInsufficientBudget
		}

		reservationID := uuid.New()
		at := now.UTC()
		row := tx.QueryRow(ctx, `
            INSERT INTO compensation_reservation (
                tenant_id, row_id, budget_id, proposal_digest, amount, currency,
                authority_digest, idempotency_key, expires_at, state, fence,
                created_at, updated_at)
            VALUES ($1,$2,$3,$4,$5::numeric,$6,$7,$8,$9,'HELD',1,$10,$10)
			ON CONFLICT DO NOTHING
			RETURNING `+reservationColumns, s.tenant, reservationID, r.BudgetID,
			storageDigest(r.ProposalDigest), r.Amount.String(), r.Currency, storageDigest(r.AuthorityDigest),
			r.IdempotencyKey, r.ExpiresAt.UTC(), at)
		var scanErr error
		out, scanErr = scanReservation(row)
		if scanErr != nil {
			if errors.Is(scanErr, dbport.ErrNoRows) {
				existing, lookupErr := s.loadByKey(ctx, tx, r.IdempotencyKey)
				if lookupErr == nil && sameRequest(existing, r) {
					out = existing
					return nil
				}
				return &CodedError{code: CodeDuplicate, cause: budget.ErrReservationConflict}
			}
			return classifyStorage(scanErr, scanErr)
		}
		if _, err := tx.Exec(ctx, `
            INSERT INTO reservation_event
                (tenant_id, row_id, reservation_id, sequence, from_state, to_state, fence, "at")
            VALUES ($1,$2,$3,1,NULL,'HELD',1,$4)`,
			s.tenant, uuid.New(), reservationID, at); err != nil {
			return classifyStorage(err, err)
		}
		return nil
	}); err != nil {
		return budget.CompensationReservation{}, err
	}
	return out, nil
}

func parseID(id string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, &CodedError{code: CodeNotFound, cause: budget.ErrReservationNotFound}
	}
	return parsed, nil
}

func (s *Store) transition(id string, fence uint64, to budget.ReservationState, now time.Time) (out budget.CompensationReservation, err error) {
	reservationID, err := parseID(id)
	if err != nil {
		return budget.CompensationReservation{}, err
	}
	if fence == ^uint64(0) {
		return budget.CompensationReservation{}, &CodedError{code: CodeStaleFence, cause: budget.ErrStaleFence}
	}
	err = s.withTx(func(ctx context.Context, tx dbport.Tx) error {
		current, loadErr := s.loadByID(ctx, tx, reservationID, true)
		if loadErr != nil {
			if errors.Is(loadErr, dbport.ErrNoRows) {
				return &CodedError{code: CodeNotFound, cause: budget.ErrReservationNotFound}
			}
			return classifyStorage(loadErr, loadErr)
		}
		if current.Fence != fence {
			return &CodedError{code: CodeStaleFence, cause: budget.ErrStaleFence}
		}
		if current.State == to {
			out = current
			return nil
		}
		allowed := (current.State == budget.Held && (to == budget.Committed || to == budget.Released || to == budget.ReconciliationRequired)) ||
			(current.State == budget.ReconciliationRequired && (to == budget.Committed || to == budget.Released))
		if !allowed {
			return budget.ErrInvalidTransition
		}
		nextFence := fence + 1
		at := now.UTC()
		affected, updateErr := tx.Exec(ctx, `
            UPDATE compensation_reservation
            SET state=$3, fence=$4, updated_at=$5
            WHERE tenant_id=$1 AND row_id=$2 AND fence=$6`,
			s.tenant, reservationID, string(to), int64(nextFence), at, int64(fence))
		if updateErr != nil {
			return classifyStorage(updateErr, updateErr)
		}
		if affected != 1 {
			return &CodedError{code: CodeStaleFence, cause: budget.ErrStaleFence}
		}
		var sequence int64
		if scanErr := tx.QueryRow(ctx, `
            SELECT COALESCE(MAX(sequence), 0) + 1
            FROM reservation_event
            WHERE tenant_id=$1 AND reservation_id=$2`, s.tenant, reservationID).Scan(&sequence); scanErr != nil {
			return classifyStorage(scanErr, scanErr)
		}
		if _, insertErr := tx.Exec(ctx, `
            INSERT INTO reservation_event
                (tenant_id, row_id, reservation_id, sequence, from_state, to_state, fence, "at")
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			s.tenant, uuid.New(), reservationID, sequence, string(current.State), string(to), int64(nextFence), at); insertErr != nil {
			return &CodedError{code: CodeSequence, cause: insertErr}
		}
		current.State = to
		current.Fence = nextFence
		current.UpdatedAt = at
		out = current
		return nil
	})
	return out, err
}

func (s *Store) Commit(id string, fence uint64, now time.Time) (budget.CompensationReservation, error) {
	return s.transition(id, fence, budget.Committed, now)
}

func (s *Store) Release(id string, fence uint64, now time.Time) (budget.CompensationReservation, error) {
	return s.transition(id, fence, budget.Released, now)
}

func (s *Store) MarkAmbiguous(id string, fence uint64, now time.Time) error {
	_, err := s.transition(id, fence, budget.ReconciliationRequired, now)
	if err != nil {
		return err
	}
	return budget.ErrExternalAmbiguous
}

func (s *Store) Reconcile(id string, fence uint64, outcome budget.ReservationState, now time.Time) (budget.CompensationReservation, error) {
	return s.transition(id, fence, outcome, now)
}

func (s *Store) Expire(now time.Time) (out []budget.CompensationReservation) {
	if err := s.withTx(func(ctx context.Context, tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+reservationColumns+`
            FROM compensation_reservation
            WHERE tenant_id=$1 AND state='HELD' AND expires_at IS NOT NULL AND expires_at <= $2
            ORDER BY row_id FOR UPDATE`, s.tenant, now.UTC())
		if err != nil {
			return classifyStorage(err, err)
		}
		defer rows.Close()
		for rows.Next() {
			current, scanErr := scanReservation(rows)
			if scanErr != nil {
				return classifyStorage(scanErr, scanErr)
			}
			nextFence := current.Fence + 1
			at := now.UTC()
			if _, updateErr := tx.Exec(ctx, `
                UPDATE compensation_reservation
                SET state='EXPIRED', fence=$3, updated_at=$4
                WHERE tenant_id=$1 AND row_id=$2 AND fence=$5`,
				s.tenant, uuid.MustParse(current.ID), int64(nextFence), at, int64(current.Fence)); updateErr != nil {
				return classifyStorage(updateErr, updateErr)
			}
			var sequence int64
			if scanErr := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM reservation_event WHERE tenant_id=$1 AND reservation_id=$2`, s.tenant, uuid.MustParse(current.ID)).Scan(&sequence); scanErr != nil {
				return classifyStorage(scanErr, scanErr)
			}
			if _, insertErr := tx.Exec(ctx, `
                INSERT INTO reservation_event
                    (tenant_id, row_id, reservation_id, sequence, from_state, to_state, fence, "at")
                VALUES ($1,$2,$3,$4,'HELD','EXPIRED',$5,$6)`,
				s.tenant, uuid.New(), uuid.MustParse(current.ID), sequence, int64(nextFence), at); insertErr != nil {
				return &CodedError{code: CodeSequence, cause: insertErr}
			}
			current.State = budget.Expired
			current.Fence = nextFence
			current.UpdatedAt = at
			out = append(out, current)
		}
		return rows.Err()
	}); err != nil {
		return nil
	}
	return out
}

func (s *Store) Get(id string) (out budget.CompensationReservation, ok bool) {
	reservationID, err := parseID(id)
	if err != nil {
		return budget.CompensationReservation{}, false
	}
	err = s.withTx(func(ctx context.Context, tx dbport.Tx) error {
		var loadErr error
		out, loadErr = s.loadByID(ctx, tx, reservationID, false)
		if errors.Is(loadErr, dbport.ErrNoRows) {
			return nil
		}
		return loadErr
	})
	return out, err == nil && out.ID != ""
}

func (s *Store) Events(id string) (out []budget.ReservationEvent) {
	reservationID, err := parseID(id)
	if err != nil {
		return nil
	}
	_ = s.withTx(func(ctx context.Context, tx dbport.Tx) error {
		rows, queryErr := tx.Query(ctx, `
            SELECT row_id, reservation_id, sequence, from_state, to_state, fence, "at"
            FROM reservation_event
            WHERE tenant_id=$1 AND reservation_id=$2 ORDER BY sequence`, s.tenant, reservationID)
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		for rows.Next() {
			var rowID, eventReservationID uuid.UUID
			var sequence, fence int64
			var fromState *string
			var toState string
			var at time.Time
			if scanErr := rows.Scan(&rowID, &eventReservationID, &sequence, &fromState, &toState, &fence, &at); scanErr != nil {
				return scanErr
			}
			if sequence < 0 || fence < 0 {
				return fmt.Errorf("budgetstore: negative event counter")
			}
			event := budget.ReservationEvent{Sequence: uint64(sequence), ReservationID: eventReservationID.String(), To: budget.ReservationState(toState), Fence: uint64(fence), At: at.UTC()}
			if fromState != nil {
				event.From = budget.ReservationState(*fromState)
			}
			out = append(out, event)
		}
		return rows.Err()
	})
	return out
}

func (s *Store) Evidence(id string) (out budget.ReservationEvidence, ok bool) {
	reservationID, err := parseID(id)
	if err != nil {
		return budget.ReservationEvidence{}, false
	}
	err = s.withTx(func(ctx context.Context, tx dbport.Tx) error {
		reservation, loadErr := s.loadByID(ctx, tx, reservationID, false)
		if errors.Is(loadErr, dbport.ErrNoRows) {
			return nil
		}
		if loadErr != nil {
			return loadErr
		}
		rows, queryErr := tx.Query(ctx, `
            SELECT reservation_id, sequence, from_state, to_state, fence, "at"
            FROM reservation_event
            WHERE tenant_id=$1 AND reservation_id=$2 ORDER BY sequence`, s.tenant, reservationID)
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		out = budget.ReservationEvidence{
			ReservationID: reservation.ID, TenantID: reservation.Request.TenantID,
			BudgetID: reservation.Request.BudgetID, ProposalDigest: reservation.Request.ProposalDigest,
			AuthorityDigest: reservation.Request.AuthorityDigest, IdempotencyKey: reservation.Request.IdempotencyKey,
			State: reservation.State, Fence: reservation.Fence,
		}
		for rows.Next() {
			var eventReservationID uuid.UUID
			var sequence, eventFence int64
			var fromState *string
			var toState string
			var at time.Time
			if scanErr := rows.Scan(&eventReservationID, &sequence, &fromState, &toState, &eventFence, &at); scanErr != nil {
				return scanErr
			}
			event := budget.ReservationEvent{Sequence: uint64(sequence), ReservationID: eventReservationID.String(), To: budget.ReservationState(toState), Fence: uint64(eventFence), At: at.UTC()}
			if fromState != nil {
				event.From = budget.ReservationState(*fromState)
			}
			out.Events = append(out.Events, event)
		}
		return rows.Err()
	})
	return out, err == nil && out.ReservationID != ""
}
