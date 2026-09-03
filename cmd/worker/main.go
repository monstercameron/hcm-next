// Command worker is a thin composition root built on
// internal/platform/bootstrap.Run: it resolves this role's typed
// configuration, opens the database pool, and hands bootstrap one Workload
// (the outbox sweep loop in sweep.go). It owns no dispatch semantics of its
// own beyond that wiring.
//
// The Workload runs the transactional outbox consumer (internal/data/outbox)
// across every active tenant: each sweep claims due messages (PENDING, or
// IN_FLIGHT past their lease) and dispatches them. A message failing
// dispatch returns to PENDING for retry; the target database's own restart
// safety is Consumer.Poll's lease, not anything this process remembers, so
// killing and restarting worker loses nothing and never double-applies a
// delivered message (DATA-008).
//
// SVC-010 eventually hosts messaging delivery as a cmd/worker role; this
// composition root keeps worker's current role - a plain, logging outbox
// consumer - and adds no provider/messaging behavior of its own.
//
// The target server is HCMNEXT_DATABASE_URL, overridable with -database-url.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/monstercameron/hcm-next/internal/data/outbox"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

// EnvDatabaseURL names the server this command connects to, matching
// cmd/migrate's and cmd/projector's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

// EnvHealthAddr, if set, is the loopback host:port the health/readiness
// endpoint is served on; empty (the default) disables it.
const EnvHealthAddr = "HCMNEXT_WORKER_HEALTH_ADDR"

func main() {
	os.Exit(bootstrap.Run(context.Background(), spec(os.Args[1:])))
}

// workerConfigFields declares every flag/env-backed value this role
// accepts. It is a function rather than a package variable so callers never
// share (and risk mutating) one backing array.
func workerConfigFields() []bootstrap.Field {
	return []bootstrap.Field{
		{
			Name:   "database-url",
			Env:    EnvDatabaseURL,
			Usage:  "PostgreSQL connection URL (" + EnvDatabaseURL + " if unset)",
			Kind:   bootstrap.KindString,
			Secret: true,
		},
		{
			Name:    "poll-interval",
			Env:     "HCMNEXT_WORKER_POLL_INTERVAL",
			Usage:   "how long to sleep between sweeps that found no due work",
			Default: "2s",
			Kind:    bootstrap.KindDuration,
		},
		{
			Name:    "lease",
			Env:     "HCMNEXT_WORKER_LEASE",
			Usage:   "how long a claimed message stays IN_FLIGHT before another sweep may reclaim it",
			Default: outbox.DefaultLease.String(),
			Kind:    bootstrap.KindDuration,
		},
		{
			Name:    "batch-size",
			Env:     "HCMNEXT_WORKER_BATCH_SIZE",
			Usage:   "maximum messages one sweep claims per tenant",
			Default: fmt.Sprintf("%d", outbox.DefaultBatchSize),
			Kind:    bootstrap.KindInt,
		},
		{
			Name:  "health-addr",
			Env:   EnvHealthAddr,
			Usage: "loopback host:port (127.0.0.1, localhost or ::1) to serve the health/readiness endpoint on; empty disables it",
			Kind:  bootstrap.KindString,
		},
	}
}

// spec builds the full worker Spec for args. It pre-resolves health-addr
// with the same precedence bootstrap.Run itself applies (flag > env >
// default) because Spec.HealthAddr, unlike every other worker setting, is a
// plain field bootstrap.Run reads before it ever parses Spec.ConfigFields;
// the pre-parse below is a pure, side-effect-free rerun of exactly the parse
// Run performs moments later, so a bad flag here is simply reported again
// (correctly, with the banner and full error) by Run's own parse.
func spec(args []string) bootstrap.Spec {
	fields := workerConfigFields()
	healthAddr := ""
	if values, err := bootstrap.ParseConfig(args, nil, fields); err == nil {
		healthAddr = values.String("health-addr")
	}

	return bootstrap.Spec{
		Role:             bootstrap.RoleWorker,
		Args:             args,
		ConfigFields:     fields,
		Validate:         validateConfig,
		DatabaseURLField: "database-url",
		DBPoolFactory:    pgxDBPoolFactory,
		HealthAddr:       healthAddr,
		Build:            build,
	}
}

// validateConfig fails config resolution (before any listener or workload
// starts) on a missing database URL or an unparsable typed field.
func validateConfig(v *bootstrap.Values) error {
	if v.String("database-url") == "" {
		return fmt.Errorf("%s is not set; pass -database-url or set the environment variable", EnvDatabaseURL)
	}
	if _, err := v.Duration("poll-interval"); err != nil {
		return err
	}
	if _, err := v.Duration("lease"); err != nil {
		return err
	}
	if _, err := v.Int("batch-size"); err != nil {
		return err
	}
	return nil
}

// build resolves the sweep loop's dependencies from deps and returns it as
// this role's single Workload. It is the one place worker's Spec touches
// outbox-specific types.
func build(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
	pool, ok := deps.DB.(workerPool)
	if !ok {
		return bootstrap.Runtime{}, fmt.Errorf("worker: database pool %T does not support outbox operations (Begin/Query)", deps.DB)
	}

	pollInterval, err := deps.Values.Duration("poll-interval")
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	lease, err := deps.Values.Duration("lease")
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	batchSize, err := deps.Values.Int("batch-size")
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	consumer := outbox.NewConsumer(pool, outbox.WithLease(lease), outbox.WithBatchSize(batchSize))
	tenants := pgxTenantLister{pool: pool}
	logger := deps.Logger

	logger.Info("worker.outbox_consumer_configured",
		"poll_interval", pollInterval.String(),
		"lease", lease.String(),
		"batch_size", batchSize,
	)

	wl := bootstrap.Workload{
		Name: "outbox-consumer",
		Run: func(ctx context.Context) error {
			return runOutboxLoop(ctx, logger, tenants, consumer, pollInterval)
		},
	}
	return bootstrap.Runtime{Workloads: []bootstrap.Workload{wl}}, nil
}

// workerPool is the subset of a *pgxpool.Pool this role needs beyond
// bootstrap's own Ping/Close DBPool port: outbox.NewConsumer needs Begin,
// and activeTenants listing needs Query. bootstrap's default DBPoolFactory
// (PgxPoolFactory) returns an unexported type exposing only Ping/Close, so
// this role supplies pgxDBPoolFactory instead, whose *dbPool return value
// satisfies this interface too.
type workerPool interface {
	bootstrap.DBPool
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// dbPool adapts a *pgxpool.Pool to workerPool.
type dbPool struct{ pool *pgxpool.Pool }

func (d *dbPool) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }
func (d *dbPool) Close()                         { d.pool.Close() }
func (d *dbPool) Begin(ctx context.Context) (pgx.Tx, error) {
	return d.pool.Begin(ctx)
}
func (d *dbPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return d.pool.Query(ctx, sql, args...)
}

// pgxDBPoolFactory opens a pgxpool.Pool against url and pings it once, so a
// bad connection string or unreachable server fails Run before any workload
// starts, matching bootstrap.PgxPoolFactory's own contract.
func pgxDBPoolFactory(ctx context.Context, url string) (bootstrap.DBPool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &dbPool{pool: pool}, nil
}

// pgxTenantLister lists active tenants over any pool that can Query.
type pgxTenantLister struct {
	pool interface {
		Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	}
}

func (l pgxTenantLister) ActiveTenants(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := l.pool.Query(ctx, `SELECT tenant_id FROM tenant WHERE status = 'ACTIVE'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
