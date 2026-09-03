package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DefaultLease is how long a claimed message stays IN_FLIGHT before another
// Poll may reclaim it. A consumer that crashes after claiming a batch but
// before acking it leaves those rows IN_FLIGHT; once the lease expires, the
// next Poll (from a restarted consumer, or another one) reclaims them - this
// is what makes dispatch restart-safe rather than merely "usually fine".
const DefaultLease = 30 * time.Second

// DefaultBatchSize bounds how many messages one Poll claims at a time.
const DefaultBatchSize = 32

// Beginner opens transactions. *pgxpool.Pool and *pgx.Conn both implement it.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

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

// Poll claims up to the batch size of due messages for tenant: PENDING
// messages whose available_at has arrived, plus IN_FLIGHT messages whose
// lease has expired (a prior claimer crashed or was killed before acking).
// Claimed messages are marked IN_FLIGHT with a fresh lease in the same
// transaction that selected them, so two concurrent Poll calls (or a Poll
// racing a not-yet-expired lease) never both claim the same row: FOR UPDATE
// SKIP LOCKED serializes claims and skips whatever another poller is already
// holding.
func (c *Consumer) Poll(ctx context.Context, tenant uuid.UUID) ([]Record, error) {
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
	leaseExpiry := now.Add(-c.lease)

	rows, err := tx.Query(ctx, `
		SELECT outbox_id FROM outbox
		WHERE tenant_id = $1
		  AND (
		    (status = $2 AND available_at <= $3)
		    OR (status = $4 AND updated_at <= $5)
		  )
		ORDER BY available_at
		FOR UPDATE SKIP LOCKED
		LIMIT $6`,
		tenant, StatusPending, now, StatusInFlight, leaseExpiry, c.batchSize)
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

	claimed := make([]Record, 0, len(ids))
	for _, id := range ids {
		tag, err := tx.Exec(ctx, `
			UPDATE outbox SET status = $3, attempts = attempts + 1, updated_at = $4
			WHERE tenant_id = $1 AND outbox_id = $2`,
			tenant, id, StatusInFlight, now)
		if err != nil {
			return nil, fmt.Errorf("outbox: poll: claim %s: %w", id, err)
		}
		if tag.RowsAffected() != 1 {
			return nil, fmt.Errorf("outbox: poll: claim %s: %d rows updated", id, tag.RowsAffected())
		}
		rec, err := Read(ctx, tx, tenant, id)
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

// Ack marks a message DELIVERED. Acking an already-delivered message is a
// harmless no-op, matching Handler's own idempotency requirement.
func (c *Consumer) Ack(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID) error {
	_, err := execOn(ctx, c.db, `
		UPDATE outbox SET status = $3, updated_at = $4
		WHERE tenant_id = $1 AND outbox_id = $2`,
		tenant, outboxID, StatusDelivered, c.now())
	if err != nil {
		return fmt.Errorf("outbox: ack %s: %w", outboxID, err)
	}
	return nil
}

// Fail records a delivery attempt's failure. The message returns to PENDING
// (available immediately) so the next Poll retries it; a caller enforcing a
// maximum-attempts policy can transition to ABANDONED itself by reading
// Record.Attempts.
func (c *Consumer) Fail(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID, cause error) error {
	_, err := execOn(ctx, c.db, `
		UPDATE outbox SET status = $3, last_error = $4, available_at = $5, updated_at = $5
		WHERE tenant_id = $1 AND outbox_id = $2`,
		tenant, outboxID, StatusPending, cause.Error(), c.now())
	if err != nil {
		return fmt.Errorf("outbox: fail %s: %w", outboxID, err)
	}
	return nil
}

// execOn opens a short-lived transaction to run one statement. Ack/Fail are
// single-row, single-statement updates; a dedicated transaction keeps
// Consumer's exported surface free of a bare-connection Exec assumption.
func execOn(ctx context.Context, db Beginner, sql string, args ...any) (pgconn.CommandTag, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return pgconn.CommandTag{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return pgconn.CommandTag{}, err
	}
	return tag, nil
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
				_ = c.Fail(ctx, tenant, msg.OutboxID, err)
				continue
			}
			if err := c.Ack(ctx, tenant, msg.OutboxID); err != nil {
				return err
			}
		}
	}
}
