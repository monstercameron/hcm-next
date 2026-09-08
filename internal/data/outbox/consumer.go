package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
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
}

// ConsumerOption configures a Consumer.
type ConsumerOption func(*Consumer)

// WithLease overrides DefaultLease.
func WithLease(d time.Duration) ConsumerOption { return func(c *Consumer) { c.lease = d } }

// WithBatchSize overrides DefaultBatchSize.
func WithBatchSize(n int) ConsumerOption { return func(c *Consumer) { c.batchSize = n } }

// WithClock replaces the consumer's source of time, for deterministic lease
// expiry tests.
func WithClock(now func() time.Time) ConsumerOption { return func(c *Consumer) { c.now = now } }

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

// Ack marks a message DELIVERED. Acking an already-delivered message is a
// harmless no-op, matching Handler's own idempotency requirement.
func (c *Consumer) Ack(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID) error {
	_, err := execOn(ctx, c.db, `
		UPDATE outbox SET status = $3, updated_at = $4, lease_token = NULL, lease_until = NULL
		WHERE tenant_id = $1 AND outbox_id = $2 AND status = $5`,
		tenant, outboxID, StatusDelivered, c.now(), StatusInFlight)
	if err != nil {
		return fmt.Errorf("outbox: ack %s: %w", outboxID, err)
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
// (available immediately) so the next Poll retries it; a caller enforcing a
// maximum-attempts policy can transition to ABANDONED itself by reading
// Record.Attempts.
func (c *Consumer) Fail(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID, cause error) error {
	if cause == nil {
		return fmt.Errorf("outbox: fail %s: cause is required", outboxID)
	}
	_, err := execOn(ctx, c.db, `
		UPDATE outbox SET status = $3, last_error = $4, available_at = $5, updated_at = $5,
			lease_token = NULL, lease_until = NULL
		WHERE tenant_id = $1 AND outbox_id = $2 AND status = $6`,
		tenant, outboxID, StatusPending, cause.Error(), c.now(), StatusInFlight)
	if err != nil {
		return fmt.Errorf("outbox: fail %s: %w", outboxID, err)
	}
	return nil
}

// FailLease is the fenced form of Fail.
func (c *Consumer) FailLease(ctx context.Context, tenant, outboxID, token uuid.UUID, cause error) error {
	if token == uuid.Nil {
		return fmt.Errorf("outbox: fail %s: lease token is required", outboxID)
	}
	if cause == nil {
		return fmt.Errorf("outbox: fail %s: cause is required", outboxID)
	}
	affected, err := execOn(ctx, c.db, `
		UPDATE outbox SET status = $3, last_error = $4, available_at = $5, updated_at = $5,
			lease_token = NULL, lease_until = NULL
		WHERE tenant_id = $1 AND outbox_id = $2 AND status = $6 AND lease_token = $7
		  AND lease_until > $5`,
		tenant, outboxID, StatusPending, cause.Error(), c.now(), StatusInFlight, token)
	if err != nil {
		return fmt.Errorf("outbox: fail lease %s: %w", outboxID, err)
	}
	if affected != 1 {
		return fmt.Errorf("outbox: fail lease %s: %w", outboxID, ErrLeaseFence)
	}
	return nil
}

// AckClaim acknowledges the exact claim returned by Poll. Keeping the token
// on the returned record makes accidental unfenced acknowledgement harder at
// call sites that already pass records through a handler.
func (c *Consumer) AckClaim(ctx context.Context, msg Record) error {
	return c.AckLease(ctx, msg.Tenant, msg.OutboxID, msg.LeaseToken)
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
			if err := c.AckLease(ctx, tenant, msg.OutboxID, msg.LeaseToken); err != nil {
				return err
			}
		}
	}
}
