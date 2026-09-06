// Package wakeup provides PostgreSQL LISTEN/NOTIFY as a lossy hint channel.
//
// Durable work remains the authority. Callers commit their work row first and
// then call Notifier.Notify. Listener scans the caller-owned durable table
// after subscribing, after every accepted hint, on a bounded recovery interval
// and after reconnecting. It never exposes or interprets a notification
// payload, and it does not start a scheduler loop: the caller drives Wait.
package wakeup

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

const channelPrefix = "hcmnext_wakeup_"

var (
	// ErrInvalid reports a malformed wake-up configuration or tenant identity.
	ErrInvalid = errors.New("wakeup: invalid configuration")
	// ErrClosed reports an operation on a listener that has been closed.
	ErrClosed = errors.New("wakeup: listener closed")
)

// WakeReason describes why Wait returned. A hint and a recovery both require
// the caller to read its authoritative table; neither is accepted work.
type WakeReason string

const (
	// ReasonHint means a notification arrived on a configured tenant channel.
	ReasonHint WakeReason = "HINT"
	// ReasonRecovery means the periodic durable scan or reconnect catch-up ran.
	ReasonRecovery WakeReason = "RECOVERY"
	// ReasonSpurious means a notification was received on an unconfigured
	// channel. It causes no durable read.
	ReasonSpurious WakeReason = "SPURIOUS"
)

// Wake is the non-authoritative result of one Wait call. TenantID is set only
// for a configured tenant hint. There is intentionally no payload field:
// PostgreSQL payload bytes are never a work identity or durable record.
type Wake struct {
	TenantID uuid.UUID
	Reason   WakeReason
}

// ChannelForTenant returns the stable, non-sensitive PostgreSQL channel for a
// tenant. The channel contains only a namespace and the tenant UUID; callers
// must not put work IDs or business data in a notification.
func ChannelForTenant(tenantID uuid.UUID) string {
	if tenantID == uuid.Nil {
		return ""
	}
	return channelPrefix + strings.ReplaceAll(tenantID.String(), "-", "")
}

// Notifier sends a wake-up hint through a connection that cannot be a
// transaction. The caller must invoke Notify only after the transaction that
// inserted or changed the durable work row has committed.
type Notifier struct {
	conn dbport.Conn
}

// NewNotifier binds a notifier to a database connection or pool. The caller's
// sequencing contract is to invoke Notify only after committing the durable
// write; Go's structural interfaces cannot distinguish a transaction that
// also exposes the same query methods, so this boundary is documented and
// reviewed at the call site.
func NewNotifier(conn dbport.Conn) *Notifier {
	return &Notifier{conn: conn}
}

// Notify sends one empty-payload hint for tenantID. PostgreSQL delivers the
// hint only after this statement commits; because conn is a dbport.Conn, this
// statement is separate from the caller's durable transaction.
func (n *Notifier) Notify(ctx context.Context, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant id is nil", ErrInvalid)
	}
	if n == nil || n.conn == nil {
		return fmt.Errorf("%w: notifier has no connection", ErrInvalid)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	channel := ChannelForTenant(tenantID)
	if _, err := n.conn.Exec(ctx, `SELECT pg_notify($1, $2)`, channel, ""); err != nil {
		return fmt.Errorf("wakeup: notify tenant %s: %w", tenantID, err)
	}
	return nil
}

// NotifyCommitted is the explicit spelling for the post-commit call site. It
// is equivalent to Notify and exists to make the transaction boundary visible
// at callers that use a commit coordinator.
func (n *Notifier) NotifyCommitted(ctx context.Context, tenantID uuid.UUID) error {
	return n.Notify(ctx, tenantID)
}

// ScanFunc reads the authoritative durable table(s) for tenantID. It may claim
// work only according to the owning table's normal lease and idempotency
// contract; the Listener itself performs no claim and supplies no payload.
type ScanFunc func(context.Context, uuid.UUID) error

// ListenerConfig configures a caller-driven listener. TenantIDs are the only
// channels the listener subscribes to. Scan is called once per tenant after
// LISTEN commits, once for a matching hint, and once per tenant for periodic or
// reconnect recovery.
type ListenerConfig struct {
	URL              string
	RuntimeParams    map[string]string
	TenantIDs        []uuid.UUID
	RecoveryInterval time.Duration
	Scan             ScanFunc
}

type notificationConn interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	WaitForNotification(context.Context) (*pgconn.Notification, error)
	Close(context.Context) error
}

type dialFunc func(context.Context, string, map[string]string) (notificationConn, error)

// Listener is a single PostgreSQL LISTEN session. It is caller-driven and is
// not safe for concurrent Wait calls. No goroutine, ticker or scheduler loop
// is started by this type.
type Listener struct {
	mu sync.Mutex

	url              string
	runtimeParams    map[string]string
	tenantIDs        []uuid.UUID
	tenantsByChannel map[string]uuid.UUID
	recoveryInterval time.Duration
	scan             ScanFunc
	dial             dialFunc
	conn             notificationConn
	closed           bool
}

// NewListener connects, commits LISTEN for every configured tenant and then
// performs the required post-subscription durable scan before returning. That
// ordering closes PostgreSQL's initial LISTEN/startup race: work committed
// concurrently with LISTEN is found by the scan even if its notification was
// delivered before the session began waiting.
func NewListener(ctx context.Context, cfg ListenerConfig) (*Listener, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("%w: database URL is empty", ErrInvalid)
	}
	if cfg.Scan == nil {
		return nil, fmt.Errorf("%w: durable scan is nil", ErrInvalid)
	}
	if cfg.RecoveryInterval <= 0 {
		return nil, fmt.Errorf("%w: recovery interval must be positive", ErrInvalid)
	}
	tenants, byChannel, err := normalizeTenants(cfg.TenantIDs)
	if err != nil {
		return nil, err
	}

	l := &Listener{
		url:              cfg.URL,
		runtimeParams:    cloneParams(cfg.RuntimeParams),
		tenantIDs:        tenants,
		tenantsByChannel: byChannel,
		recoveryInterval: cfg.RecoveryInterval,
		scan:             cfg.Scan,
		dial:             dial,
	}
	conn, err := l.dial(ctx, l.url, l.runtimeParams)
	if err != nil {
		return nil, fmt.Errorf("wakeup: connect listener: %w", err)
	}
	l.conn = conn
	if err := l.listen(ctx, conn); err != nil {
		_ = conn.Close(context.Background())
		return nil, err
	}
	if err := l.scanAll(ctx); err != nil {
		_ = conn.Close(context.Background())
		return nil, fmt.Errorf("wakeup: startup durable scan: %w", err)
	}
	return l, nil
}

// Wait waits for one hint, or until RecoveryInterval elapses, and then reads
// durable state through Scan. A non-context connection error is recovered by
// opening a new session, re-issuing LISTEN, and performing a catch-up scan;
// the recovery result is returned to the caller, which decides when to call
// Wait again.
func (l *Listener) Wait(ctx context.Context) (Wake, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil {
		return Wake{}, ErrClosed
	}
	if l.isClosed() {
		return Wake{}, ErrClosed
	}

	waitCtx, cancel := context.WithTimeout(ctx, l.recoveryInterval)
	defer cancel()
	conn := l.currentConn()
	if conn == nil {
		return Wake{}, ErrClosed
	}
	notification, err := conn.WaitForNotification(waitCtx)
	if err == nil {
		if notification == nil {
			return Wake{}, fmt.Errorf("wakeup: notification session returned no notification")
		}
		tenantID, ok := l.tenantsByChannel[notification.Channel]
		if !ok {
			return Wake{Reason: ReasonSpurious}, nil
		}
		if err := l.scan(ctx, tenantID); err != nil {
			return Wake{}, fmt.Errorf("wakeup: durable hint scan for tenant %s: %w", tenantID, err)
		}
		return Wake{TenantID: tenantID, Reason: ReasonHint}, nil
	}
	if ctx.Err() != nil {
		return Wake{}, ctx.Err()
	}
	if waitCtx.Err() != nil {
		if err := l.scanAll(ctx); err != nil {
			return Wake{}, fmt.Errorf("wakeup: periodic durable scan: %w", err)
		}
		return Wake{Reason: ReasonRecovery}, nil
	}
	return l.reconnect(ctx)
}

// Close stops the listener session. It is safe to call more than once.
func (l *Listener) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	conn := l.conn
	l.conn = nil
	l.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close(ctx)
}

func (l *Listener) currentConn() notificationConn {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.conn
}

func (l *Listener) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

func (l *Listener) listen(ctx context.Context, conn notificationConn) error {
	for _, tenantID := range l.tenantIDs {
		channel := ChannelForTenant(tenantID)
		if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
			return fmt.Errorf("wakeup: listen on tenant %s: %w", tenantID, err)
		}
	}
	return nil
}

func (l *Listener) scanAll(ctx context.Context) error {
	for _, tenantID := range l.tenantIDs {
		if err := l.scan(ctx, tenantID); err != nil {
			return fmt.Errorf("tenant %s: %w", tenantID, err)
		}
	}
	return nil
}

func (l *Listener) reconnect(ctx context.Context) (Wake, error) {
	if l.isClosed() {
		return Wake{}, ErrClosed
	}
	old := l.currentConn()
	if old != nil {
		_ = old.Close(context.Background())
	}
	conn, err := l.dial(ctx, l.url, l.runtimeParams)
	if err != nil {
		return Wake{}, fmt.Errorf("wakeup: reconnect listener: %w", err)
	}
	if err := l.listen(ctx, conn); err != nil {
		_ = conn.Close(context.Background())
		return Wake{}, err
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = conn.Close(context.Background())
		return Wake{}, ErrClosed
	}
	l.conn = conn
	l.mu.Unlock()
	if err := l.scanAll(ctx); err != nil {
		return Wake{}, fmt.Errorf("wakeup: reconnect durable scan: %w", err)
	}
	return Wake{Reason: ReasonRecovery}, nil
}

func dial(ctx context.Context, url string, runtimeParams map[string]string) (notificationConn, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	for key, value := range runtimeParams {
		cfg.RuntimeParams[key] = value
	}
	return pgx.ConnectConfig(ctx, cfg)
}

func normalizeTenants(input []uuid.UUID) ([]uuid.UUID, map[string]uuid.UUID, error) {
	if len(input) == 0 {
		return nil, nil, fmt.Errorf("%w: no tenant channels configured", ErrInvalid)
	}
	tenants := append([]uuid.UUID(nil), input...)
	sort.Slice(tenants, func(i, j int) bool { return tenants[i].String() < tenants[j].String() })
	byChannel := make(map[string]uuid.UUID, len(tenants))
	for _, tenantID := range tenants {
		if tenantID == uuid.Nil {
			return nil, nil, fmt.Errorf("%w: tenant id is nil", ErrInvalid)
		}
		channel := ChannelForTenant(tenantID)
		if _, exists := byChannel[channel]; exists {
			continue
		}
		byChannel[channel] = tenantID
	}
	unique := make([]uuid.UUID, 0, len(byChannel))
	for _, tenantID := range byChannel {
		unique = append(unique, tenantID)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].String() < unique[j].String() })
	return unique, byChannel, nil
}

func cloneParams(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
