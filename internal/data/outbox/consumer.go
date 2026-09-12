package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// DefaultLease is how long a claimed message stays IN_FLIGHT before another
// Poll may reclaim it. A consumer that crashes after claiming a batch but
// before acking it leaves those rows IN_FLIGHT; once the lease expires, the
// next Poll (from a restarted consumer, or another one) reclaims them - this
// is what makes dispatch restart-safe rather than merely "usually fine".
const DefaultLease = 30 * time.Second

// DefaultBatchSize bounds how many messages one Poll claims at a time.
const DefaultBatchSize = 32

// Beginner opens transactions. A pooled handle and a single connection both
// implement it.
type Beginner = dbport.Beginner

// Handler processes one dispatched message. It must be idempotent by message
// ID: at-least-once delivery means the same OutboxID can reach Handler more
// than once (DATA-008 GREEN: "duplicate consumer delivery is expected and
// idempotent").
type Handler func(ctx context.Context, msg Record) error

// Consumer claims and dispatches outbox messages for one tenant.
type Consumer struct {
	db        Beginner
	lease     time.Duration
	batchSize int
	now       func() time.Time
	// maxAttempts caps deliveries per message: once a message has been
	// claimed maxAttempts times, the next Fail parks it ABANDONED instead
	// of PENDING so a poison message stops spinning the sweep. Zero (the
	// default) means unlimited, preserving redeliver-forever behavior.
	maxAttempts int
	// retryAccount, when set via WithRetryAccounting, routes every failed
	// delivery through one externally-owned admission.Provisioner budget so
	// this consumer's own redelivery counts against the same
	// logical-operation budget any other layer consumes from (EVENT-003):
	// one token per logical attempt, never one per layer.
	retryAccount *RetryAccount
	retrySpec    func(Record) admission.ProvisionSpec
	retryFailure func(Record, error) admission.FailureClass
}

// ConsumerOption configures a Consumer.
type ConsumerOption func(*Consumer)

// WithLease overrides DefaultLease.
func WithLease(d time.Duration) ConsumerOption { return func(c *Consumer) { c.lease = d } }

// WithBatchSize overrides DefaultBatchSize.
func WithBatchSize(n int) ConsumerOption { return func(c *Consumer) { c.batchSize = n } }

// WithMaxAttempts parks a message ABANDONED after n failed deliveries
// instead of returning it to PENDING forever. Abandoned rows keep their
// last error as evidence, are never re-polled, and stay visible to the
// health probes that count terminal rows. Non-positive n means unlimited.
func WithMaxAttempts(n int) ConsumerOption {
	return func(c *Consumer) { c.maxAttempts = n }
}

// WithClock replaces the consumer's source of time, for deterministic lease
// expiry tests.
func WithClock(now func() time.Time) ConsumerOption { return func(c *Consumer) { c.now = now } }

// WithRetryAccounting shares one logical retry budget across every layer
// that might retry the same logical operation. On each failed delivery the
// consumer computes the record's attempt identity (AttemptIdentity: its
// LogicalOperationID when known, else its OutboxID, joined with the
// physical attempt number) and consumes one token from spec(record)'s
// budget via account. Because admission.Provisioner.Consume is replay-safe
// by attempt identity, a token another layer already consumed for the same
// logical attempt - a transaction coordinator's own retry callback, say -
// is not consumed a second time here; and once the shared budget is
// exhausted the message is parked ABANDONED even if this consumer's own
// maxAttempts would otherwise allow another try. failureOf classifies the
// delivery error into the admission.FailureClass vocabulary the budget's
// Retryable set was provisioned with; nil defaults every failure to
// admission.FailureTransient.
func WithRetryAccounting(account *RetryAccount, spec func(Record) admission.ProvisionSpec, failureOf func(Record, error) admission.FailureClass) ConsumerOption {
	return func(c *Consumer) {
		c.retryAccount = account
		c.retrySpec = spec
		c.retryFailure = failureOf
	}
}

// NewConsumer builds a Consumer over a connection or pool that can open
// transactions.
func NewConsumer(db Beginner, opts ...ConsumerOption) *Consumer {
	c := &Consumer{db: db, lease: DefaultLease, batchSize: DefaultBatchSize, now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Consumer) validate() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("outbox: consumer database is required")
	}
	if c.lease <= 0 {
		return fmt.Errorf("outbox: lease must be positive")
	}
	if c.batchSize <= 0 {
		return fmt.Errorf("outbox: batch size must be positive")
	}
	if c.now == nil {
		return fmt.Errorf("outbox: clock is required")
	}
	return nil
}

// Poll claims up to the batch size of due messages for tenant: PENDING
// messages whose available_at has arrived, plus IN_FLIGHT messages whose
// lease has expired (a prior claimer crashed or was killed before acking).
// Claimed messages are marked IN_FLIGHT with a fresh lease in the same
// transaction that selected them, so two concurrent Poll calls (or a Poll
// racing a not-yet-expired lease) never both claim the same row: FOR UPDATE
// SKIP LOCKED serializes claims and skips whatever another poller is already
// holding.
func (c *Consumer) Poll(ctx context.Context, tenant uuid.UUID) ([]Record, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	if tenant == uuid.Nil {
		return nil, fmt.Errorf("outbox: poll requires a tenant")
	}
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("outbox: poll: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	now := c.now()

	rows, err := tx.Query(ctx, `
		SELECT outbox_id FROM outbox
		WHERE tenant_id = $1
		  AND (
		    (status = $2 AND available_at <= $3)
			OR (status = $4 AND lease_until <= $5)
		  )
		ORDER BY
		  CASE criticality
		    WHEN 'P0' THEN 0
		    WHEN 'P1' THEN 1
		    WHEN 'P2' THEN 2
		    WHEN 'P3' THEN 3
		    WHEN 'P4' THEN 4
		    ELSE 5
		  END,
		  available_at, ordering_key, created_at, outbox_id
		FOR UPDATE SKIP LOCKED
		LIMIT $6`,
		tenant, StatusPending, now, StatusInFlight, now, c.batchSize)
	if err != nil {
		return nil, fmt.Errorf("outbox: poll: select: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("outbox: poll: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("outbox: poll: rows: %w", err)
	}
	rows.Close()
	if len(ids) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("outbox: poll: commit (empty): %w", err)
		}
		committed = true
		return nil, nil
	}

	type claim struct {
		id         uuid.UUID
		leaseToken uuid.UUID
		leaseUntil time.Time
	}
	claims := make([]claim, 0, len(ids))
	statements := make([]dbport.Statement, 0, len(ids))
	for _, id := range ids {
		leaseToken := uuid.New()
		leaseUntil := now.Add(c.lease)
		attemptID := uuid.New().String()
		claims = append(claims, claim{id: id, leaseToken: leaseToken, leaseUntil: leaseUntil})
		statements = append(statements, dbport.Statement{SQL: `
			UPDATE outbox SET status = $3, attempts = attempts + 1, updated_at = $4,
				lease_token = $5, lease_until = $6, lease_version = lease_version + 1,
				attempt_id = CASE WHEN logical_operation_id IS NULL THEN attempt_id ELSE $9 END
			WHERE tenant_id = $1 AND outbox_id = $2
			  AND ((status = $7 AND available_at <= $4) OR (status = $8 AND lease_until <= $4))`, Args: []any{
			tenant, id, StatusInFlight, now, leaseToken, leaseUntil, StatusPending, StatusInFlight, attemptID,
		}})
	}
	counts, err := dbport.ExecAll(ctx, tx, statements)
	if err != nil {
		index := dbport.FailedStatement(counts, len(claims))
		if index >= 0 {
			return nil, fmt.Errorf("outbox: poll: claim %s: %w", claims[index].id, err)
		}
		return nil, fmt.Errorf("outbox: poll: claims: %w", err)
	}
	for i, affected := range counts {
		if affected != 1 {
			return nil, fmt.Errorf("outbox: poll: claim %s: %d rows updated", claims[i].id, affected)
		}
	}

	claimed := make([]Record, 0, len(ids))
	for _, item := range claims {
		rec, err := Read(ctx, tx, tenant, item.id)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, rec)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("outbox: poll: commit: %w", err)
	}
	committed = true
	return claimed, nil
}

// Claim is the descriptive name for Poll. It is kept as a small alias so
// callers can use the queue vocabulary without creating a second protocol.
func (c *Consumer) Claim(ctx context.Context, tenant uuid.UUID) ([]Record, error) {
	return c.Poll(ctx, tenant)
}

// Ack marks a message DELIVERED. Only a live IN_FLIGHT claim is settled:
// anything else (never claimed, already delivered, failed, or abandoned)
// is [ErrLeaseFence], never a silent no-op, so callers can distinguish
// "settled" from "no such claim" without parsing driver output. It cannot
// verify claim ownership without the lease token — a live claim held by
// another worker settles the same as an owned one — so concurrent-worker
// call sites must use the token-fenced [Consumer.AckClaim], and this
// method is an operator-grade repair primitive.
func (c *Consumer) Ack(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID) error {
	affected, err := execOn(ctx, c.db, `
		UPDATE outbox SET status = $3, updated_at = $4, lease_token = NULL, lease_until = NULL
		WHERE tenant_id = $1 AND outbox_id = $2 AND status = $5`,
		tenant, outboxID, StatusDelivered, c.now(), StatusInFlight)
	if err != nil {
		return fmt.Errorf("outbox: ack %s: %w", outboxID, err)
	}
	if affected != 1 {
		return fmt.Errorf("outbox: ack %s: %w", outboxID, ErrLeaseFence)
	}
	return nil
}

// AckLease marks a message delivered only when token still owns the current
// lease. A worker that wakes after its lease was reclaimed cannot acknowledge
// the newer worker's delivery.
func (c *Consumer) AckLease(ctx context.Context, tenant, outboxID, token uuid.UUID) error {
	if token == uuid.Nil {
		return fmt.Errorf("outbox: ack %s: lease token is required", outboxID)
	}
	affected, err := execOn(ctx, c.db, `
		UPDATE outbox SET status = $3, updated_at = $4, lease_token = NULL, lease_until = NULL
		WHERE tenant_id = $1 AND outbox_id = $2 AND status = $5 AND lease_token = $6
		  AND lease_until > $4`,
		tenant, outboxID, StatusDelivered, c.now(), StatusInFlight, token)
	if err != nil {
		return fmt.Errorf("outbox: ack lease %s: %w", outboxID, err)
	}
	if affected != 1 {
		return fmt.Errorf("outbox: ack lease %s: %w", outboxID, ErrLeaseFence)
	}
	return nil
}

// Fail records a delivery attempt's failure. The message returns to PENDING
// (available immediately) so the next Poll retries it, unless the consumer
// was built with [WithMaxAttempts] and the message has exhausted its
// deliveries — then it is parked ABANDONED with its last error as
// evidence. Like [Consumer.Ack], failing anything but a live IN_FLIGHT
// claim is [ErrLeaseFence], never a silent no-op.
func (c *Consumer) Fail(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID, cause error) error {
	if cause == nil {
		return fmt.Errorf("outbox: fail %s: cause is required", outboxID)
	}
	return c.failRow(ctx, tenant, outboxID, uuid.Nil, false, cause)
}

// FailLease is the fenced form of Fail.
func (c *Consumer) FailLease(ctx context.Context, tenant, outboxID, token uuid.UUID, cause error) error {
	if token == uuid.Nil {
		return fmt.Errorf("outbox: fail %s: lease token is required", outboxID)
	}
	if cause == nil {
		return fmt.Errorf("outbox: fail %s: cause is required", outboxID)
	}
	return c.failRow(ctx, tenant, outboxID, token, true, cause)
}

// failRow settles one failed claim inside a single transaction: it reads
// the row locked, applies the max-attempts policy, and returns it to
// PENDING — or parks it ABANDONED once exhausted. Anything but a matching
// live claim (missing row, wrong status, or, when fenced, a foreign or
// expired lease) is [ErrLeaseFence].
func (c *Consumer) failRow(ctx context.Context, tenant, outboxID, token uuid.UUID, fenced bool, cause error) error {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("outbox: fail %s: begin: %w", outboxID, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	now := c.now()
	var attempts int
	var status string
	var leaseToken *uuid.UUID
	var leaseUntil *time.Time
	var logicalOperationID *string
	err = tx.QueryRow(ctx, `
		SELECT attempts, status, lease_token, lease_until, logical_operation_id FROM outbox
		WHERE tenant_id = $1 AND outbox_id = $2 FOR UPDATE`,
		tenant, outboxID).Scan(&attempts, &status, &leaseToken, &leaseUntil, &logicalOperationID)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("outbox: fail %s: %w", outboxID, ErrLeaseFence)
		}
		return fmt.Errorf("outbox: fail %s: read: %w", outboxID, err)
	}
	if status != StatusInFlight {
		return fmt.Errorf("outbox: fail %s: %w", outboxID, ErrLeaseFence)
	}
	if fenced {
		if leaseToken == nil || *leaseToken != token || leaseUntil == nil || !leaseUntil.After(now) {
			return fmt.Errorf("outbox: fail %s: %w", outboxID, ErrLeaseFence)
		}
	}
	target := StatusPending
	if c.maxAttempts > 0 && attempts >= c.maxAttempts {
		target = StatusAbandoned
	}
	if c.retryAccount != nil && c.retrySpec != nil && target != StatusAbandoned {
		identityRecord := Record{OutboxID: outboxID}
		if logicalOperationID != nil && *logicalOperationID != "" {
			identityRecord.Causal = &CausalMetadata{LogicalOperationID: *logicalOperationID}
		}
		rec := Record{Tenant: tenant, OutboxID: outboxID}
		spec := c.retrySpec(rec)
		failureClass := admission.FailureTransient
		if c.retryFailure != nil {
			failureClass = c.retryFailure(rec, cause)
		}
		attemptID := AttemptIdentity(identityRecord, attempts)
		receipt, rErr := c.retryAccount.Consume(spec, attemptID, admission.RetryAttempt{
			LogicalOperationID: spec.LogicalOperationID,
			OperationKind:      spec.OperationKind,
			TenantID:           spec.TenantID,
			Dependency:         spec.Dependency,
			Failure:            failureClass,
			Attempt:            attempts,
		})
		if rErr != nil {
			return fmt.Errorf("outbox: fail %s: retry accounting: %w", outboxID, rErr)
		}
		if receipt.Disposition != admission.RetryAllowed {
			target = StatusAbandoned
			cause = fmt.Errorf("%w: %s", cause, receipt.Reason)
		}
	}
	affected, err := tx.Exec(ctx, `
		UPDATE outbox SET status = $3, last_error = $4, available_at = $5, updated_at = $5,
			lease_token = NULL, lease_until = NULL
		WHERE tenant_id = $1 AND outbox_id = $2 AND status = $6`,
		tenant, outboxID, target, cause.Error(), now, StatusInFlight)
	if err != nil {
		return fmt.Errorf("outbox: fail %s: %w", outboxID, err)
	}
	if affected != 1 {
		return fmt.Errorf("outbox: fail %s: %w", outboxID, ErrLeaseFence)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("outbox: fail %s: commit: %w", outboxID, err)
	}
	committed = true
	return nil
}

// AckClaim acknowledges the exact claim returned by Poll. Keeping the token
// on the returned record makes accidental unfenced acknowledgement harder at
// call sites that already pass records through a handler.
func (c *Consumer) AckClaim(ctx context.Context, msg Record) error {
	return c.AckLease(ctx, msg.Tenant, msg.OutboxID, msg.LeaseToken)
}

// ackDelivered settles a handled message without halting on an expired
// lease the consumer still owns. The fresh-lease path (AckLease) wins when
// the claim is live; when the lease lapsed but no other poller reclaimed
// the row — the lease token still names this claim — the delivery is
// settled under the token fence instead of reported as a failure. A row
// reclaimed by another poller keeps its new owner: the token predicate
// matches zero rows and the caller gets [ErrLeaseFence].
func (c *Consumer) ackDelivered(ctx context.Context, msg Record) error {
	if err := c.AckLease(ctx, msg.Tenant, msg.OutboxID, msg.LeaseToken); err == nil {
		return nil
	} else if !errors.Is(err, ErrLeaseFence) {
		return err
	}
	affected, err := execOn(ctx, c.db, `
		UPDATE outbox SET status = $3, updated_at = $4, lease_token = NULL, lease_until = NULL
		WHERE tenant_id = $1 AND outbox_id = $2 AND status = $5 AND lease_token = $6`,
		msg.Tenant, msg.OutboxID, StatusDelivered, c.now(), StatusInFlight, msg.LeaseToken)
	if err != nil {
		return fmt.Errorf("outbox: ack expired lease %s: %w", msg.OutboxID, err)
	}
	if affected != 1 {
		return fmt.Errorf("outbox: ack expired lease %s: %w", msg.OutboxID, ErrLeaseFence)
	}
	return nil
}

// FailClaim returns the exact claim to the pending queue, fenced by its lease.
func (c *Consumer) FailClaim(ctx context.Context, msg Record, cause error) error {
	return c.FailLease(ctx, msg.Tenant, msg.OutboxID, msg.LeaseToken, cause)
}

// execOn opens a short-lived transaction to run one statement. Ack/Fail are
// single-row, single-statement updates; a dedicated transaction keeps
// Consumer's exported surface free of a bare-connection Exec assumption.
func execOn(ctx context.Context, db Beginner, sql string, args ...any) (int64, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	affected, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return affected, nil
}

// Run polls and dispatches in a loop until ctx is cancelled, sleeping
// pollInterval between empty polls. It is restart-safe by construction: Run
// itself holds no state across restarts other than what Poll's lease-based
// reclaim already provides, so starting a fresh Consumer (a fresh process)
// against the same tenant picks up exactly where a killed one left off.
func (c *Consumer) Run(ctx context.Context, tenant uuid.UUID, handler Handler, pollInterval time.Duration) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		batch, err := c.Poll(ctx, tenant)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pollInterval):
			}
			continue
		}
		for _, msg := range batch {
			if err := handler(ctx, msg); err != nil {
				_ = c.FailLease(ctx, tenant, msg.OutboxID, msg.LeaseToken, err)
				continue
			}
			// A slow handler may outrun its own lease; settling under the
			// still-owned token keeps one slow dispatch from halting the
			// loop. Only a row reclaimed by another poller is skipped, and
			// its new owner redelivers it.
			if err := c.ackDelivered(ctx, msg); err != nil {
				if errors.Is(err, ErrLeaseFence) {
					continue
				}
				return err
			}
		}
	}
}
