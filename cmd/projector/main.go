// Command projector is a thin composition root built on
// internal/platform/bootstrap.Run: it resolves this role's typed
// configuration, opens the database pool, and hands bootstrap one Workload
// (the reconcile loop in reconcile.go). It owns no projection semantics of
// its own beyond that wiring.
//
// The Workload runs the projection reconciler (internal/data/projection):
// each sweep finds every checkpoint behind its stream's current head and
// replays the missing ledger events to catch it up (DATA-010). This is the
// out-of-band complement to internal/data/outbox.Commit's synchronous,
// same-transaction advance - a projection that ever falls behind (a missed
// synchronous commit, or a projection registered after events already
// existed) catches up here instead of staying stuck. Restart safety needs
// nothing special: ReconcileOne recomputes "how far behind" from the
// database on every call, so killing and restarting projector mid-sweep
// just repeats whatever the last sweep had not finished.
//
// -rebuild (or HCMNEXT_PROJECTOR_REBUILD) switches the sweep from
// "checkpoints currently behind their stream head" (the default,
// inexpensive filter) to every registered checkpoint, unconditionally.
// ReconcileOne's own currency check makes revisiting an already-current
// checkpoint a safe, side-effect-free no-op, so -rebuild trades sweep cost
// for a full re-verification pass - useful right after registering a new
// projection consumer, or recovering from suspected checkpoint drift - as
// DATA-010's "rebuild a projection from canonical sources" without needing
// any new primitive from internal/data/projection.
//
// The target server is HCMNEXT_DATABASE_URL, overridable with -database-url.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// EnvDatabaseURL names the server this command connects to, matching
// cmd/worker's and cmd/migrate's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

// EnvHealthAddr, if set, is the loopback host:port the health/readiness
// endpoint is served on; empty (the default) disables it.
const EnvHealthAddr = "HCMNEXT_PROJECTOR_HEALTH_ADDR"

func main() {
	os.Exit(bootstrap.Run(context.Background(), spec(os.Args[1:])))
}

// projectorConfigFields declares every flag/env-backed value this role
// accepts. It is a function rather than a package variable so callers never
// share (and risk mutating) one backing array.
func projectorConfigFields() []bootstrap.Field {
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
			Env:     "HCMNEXT_PROJECTOR_POLL_INTERVAL",
			Usage:   "how long to sleep between sweeps that found nothing behind",
			Default: "2s",
			Kind:    bootstrap.KindDuration,
		},
		{
			Name:    "rebuild",
			Env:     "HCMNEXT_PROJECTOR_REBUILD",
			Usage:   "reconcile every registered checkpoint every sweep instead of only those currently behind",
			Default: "false",
			Kind:    bootstrap.KindBool,
		},
		{
			Name:  "health-addr",
			Env:   EnvHealthAddr,
			Usage: "loopback host:port (127.0.0.1, localhost or ::1) to serve the health/readiness endpoint on; empty disables it",
			Kind:  bootstrap.KindString,
		},
	}
}

// spec builds the full projector Spec for args. It pre-resolves health-addr
// with the same precedence bootstrap.Run itself applies (flag > env >
// default) because Spec.HealthAddr, unlike every other projector setting,
// is a plain field bootstrap.Run reads before it ever parses
// Spec.ConfigFields; the pre-parse below is a pure, side-effect-free rerun
// of exactly the parse Run performs moments later, so a bad flag here is
// simply reported again (correctly, with the banner and full error) by
// Run's own parse.
func spec(args []string) bootstrap.Spec {
	fields := projectorConfigFields()
	healthAddr := ""
	if values, err := bootstrap.ParseConfig(args, nil, fields); err == nil {
		healthAddr = values.String("health-addr")
	}

	return bootstrap.Spec{
		Role:             bootstrap.RoleProjector,
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
	if _, err := v.Bool("rebuild"); err != nil {
		return err
	}
	return nil
}

// build resolves the reconcile loop's dependencies from deps and returns it
// as this role's single Workload. It is the one place projector's Spec
// touches projection-specific types.
func build(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
	pool, ok := deps.DB.(projectorPool)
	if !ok {
		return bootstrap.Runtime{}, fmt.Errorf("projector: database pool %T does not support projection reconciliation (Begin/Query/QueryRow)", deps.DB)
	}

	pollInterval, err := deps.Values.Duration("poll-interval")
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	rebuild, err := deps.Values.Bool("rebuild")
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	reconciler := projection.NewReconciler(pool, datalogger.NewReader())
	lister := pgxProjectionLister{pool: pool, rebuild: rebuild}
	logger := deps.Logger

	logger.Info("projector.reconciler_configured", "poll_interval", pollInterval.String(), "rebuild", rebuild)

	wl := bootstrap.Workload{
		Name: "projection-reconciler",
		Run: func(ctx context.Context) error {
			return runReconcileLoop(ctx, logger, lister, reconciler, pollInterval)
		},
	}
	return bootstrap.Runtime{Workloads: []bootstrap.Workload{wl}}, nil
}

// projectorPool is the database capability this role needs beyond bootstrap's
// own Ping/Close DBPool port: projection.NewReconciler needs Begin, and both
// the normal and -rebuild sweep queries need Query/QueryRow
// (datalogger.Querier). bootstrap's default DBPoolFactory (PgxPoolFactory)
// returns an unexported type exposing only Ping/Close, so this role supplies
// pgxDBPoolFactory instead, whose *pgxadapter.Pool return value satisfies this
// interface too. Every method here is stated in [dbport] terms; the driver
// itself is reached only through the adapter this composition root constructs.
type projectorPool interface {
	bootstrap.DBPool
	dbport.Beginner
	dbport.Querier
}

// pgxDBPoolFactory opens a pgx pool against url and pings it once, so a bad
// connection string or unreachable server fails Run before any workload
// starts, matching bootstrap.PgxPoolFactory's own contract.
func pgxDBPoolFactory(ctx context.Context, url string) (bootstrap.DBPool, error) {
	return pgxadapter.NewPool(ctx, url, nil)
}

// pgxProjectionLister lists the (tenant, projection, stream) checkpoints one
// sweep should reconcile: ReconcileDue's cheap "currently behind" filter by
// default, or every registered checkpoint when rebuild is set.
type pgxProjectionLister struct {
	pool interface {
		dbport.Querier
	}
	rebuild bool
}

func (l pgxProjectionLister) Due(ctx context.Context) ([]projection.StreamProjection, error) {
	if l.rebuild {
		return allProjections(ctx, l.pool)
	}
	return projection.ReconcileDue(ctx, l.pool)
}

// allProjections lists every registered (tenant, projection, stream)
// checkpoint regardless of whether it is currently behind its stream head -
// the query -rebuild substitutes for ReconcileDue's narrower one.
func allProjections(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error)
}) ([]projection.StreamProjection, error) {
	rows, err := q.Query(ctx, `SELECT tenant_id, projection_name, stream_key FROM projection_checkpoint`)
	if err != nil {
		return nil, fmt.Errorf("projector: list all projections: %w", err)
	}
	defer rows.Close()

	var out []projection.StreamProjection
	for rows.Next() {
		var sp projection.StreamProjection
		if err := rows.Scan(&sp.Tenant, &sp.ProjectionName, &sp.StreamKey); err != nil {
			return nil, fmt.Errorf("projector: list all projections: scan: %w", err)
		}
		out = append(out, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projector: list all projections: %w", err)
	}
	return out, nil
}
