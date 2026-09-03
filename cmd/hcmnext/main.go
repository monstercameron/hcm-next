// Command hcmnext is a composition root. It wires packages; it owns no
// semantics.
//
// With no arguments it prints its build identity and exits, which is what a
// deployment check calls.
//
//	hcmnext          print the build identity
//	hcmnext serve    run the P1A cell: gRPC on -grpc-listen, HTTP edge on -http-listen
//
// # serve
//
// serve composes one P1A cell (internal/intent/app.NewCell) over a PostgreSQL
// store and publishes it on both transports. The two are handed the same
// transport.Config, which is what makes their trusted context identical by
// construction rather than by review. The HTTP edge additionally serves the
// API-001 discovery document at /v1/discovery.
//
// Process lifecycle is not this command's business and is not implemented
// here: configuration precedence, the build banner, signal handling, the
// STARTING/READY/DRAINING/STOPPED health machine, the database pool, the
// run-group and the ordered deadline-bounded shutdown all come from
// internal/platform/bootstrap. What this file owns is the Spec: which
// configuration this role accepts, and which workloads it runs.
//
// The schema is applied before the listeners start, unless -migrate=false.
// This is the plain Goose apply; the DB-006 journaled apply - artifact digest,
// tool version, checksum verification, owner, start and finish - lives in
// cmd/migrate and is a separate deliberate step:
//
//	go run ./cmd/migrate up
//
// A release pipeline runs cmd/migrate and starts hcmnext with -migrate=false;
// -migrate exists so a developer can bring a scratch database up in one
// command.
//
// # Authentication
//
// P1A authenticates with the deterministic HMAC development verifier
// (internal/trust). -dev-hmac-key is the shared signing key and must be at
// least 32 bytes; there is no default, because a listener with a default
// signing key is a listener anyone can forge a principal against.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/intent/app/pgstore"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
	"github.com/monstercameron/hcm-next/internal/platform/logging"
	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/migrations"
)

// EnvDatabaseURL names the server this command connects to, matching
// cmd/migrate's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

// EnvDevHMACKey carries the development signing key, so it need not appear in
// a process listing.
const EnvDevHMACKey = "HCMNEXT_DEV_HMAC_KEY"

// Configuration field names. They are constants because the Spec declares them
// and the Build hook reads them back: a typo between the two is a panic at
// startup rather than a silently defaulted value.
const (
	fieldGRPCListen  = "grpc-listen"
	fieldHTTPListen  = "http-listen"
	fieldDatabaseURL = "database-url"
	fieldDevHMACKey  = "dev-hmac-key"
	fieldIssuer      = "issuer"
	fieldAudience    = "audience"
	fieldTenant      = "tenant"
	fieldCellID      = "cell-id"
	fieldMaxDeadline = "max-deadline"
	fieldMigrate     = "migrate"
)

// shutdownGrace bounds the whole ordered shutdown sequence.
const shutdownGrace = 20 * time.Second

// minimumHMACKeyBytes is the shortest development signing key this listener
// will start with.
const minimumHMACKeyBytes = 32

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		info := buildinfo.Current()
		fmt.Fprintf(os.Stdout, "hcmnext %s revision=%s modified=%t go=%s\n",
			info.Module, info.Revision, info.Modified, info.GoVersion)
		return
	}
	if args[0] != "serve" {
		fmt.Fprintf(os.Stderr, "hcmnext: unknown command %q; usage: hcmnext [serve]\n", args[0])
		os.Exit(1)
	}
	os.Exit(bootstrap.Run(context.Background(), serveSpec(args[1:])))
}

// serveSpec declares the serve role: its configuration, its validation, its
// database dependency and the workloads bootstrap runs and drains.
func serveSpec(args []string) bootstrap.Spec {
	// The pool the store needs is *pgxpool.Pool, and bootstrap's DBPool port
	// is deliberately narrower than that. The factory therefore keeps the
	// concrete pool for Build while still handing bootstrap the port it owns,
	// so there is exactly one pool, opened and closed once, on bootstrap's
	// schedule rather than on this file's.
	var pool *pgxpool.Pool

	return bootstrap.Spec{
		Role:   bootstrap.RoleHCMNext,
		Args:   args,
		Logger: slog.New(logging.NewHandler(os.Stdout, logging.WithService("hcmnext"))),
		ConfigFields: []bootstrap.Field{
			{Name: fieldGRPCListen, Usage: "address the canonical gRPC surface listens on", Default: "127.0.0.1:8443"},
			{Name: fieldHTTPListen, Usage: "address the HTTP edge listens on", Default: "127.0.0.1:8080"},
			{Name: fieldDatabaseURL, Env: EnvDatabaseURL, Usage: "PostgreSQL connection URL"},
			{Name: fieldDevHMACKey, Env: EnvDevHMACKey, Usage: "development HMAC signing key, at least 32 bytes", Secret: true},
			{Name: fieldIssuer, Usage: "the only credential issuer this listener accepts", Default: "https://issuer.local.hcm-next.invalid"},
			{Name: fieldAudience, Usage: "the audience this listener answers to", Default: "hcm-next-api"},
			{Name: fieldTenant, Usage: "tenant slug to register on start; empty registers none"},
			{Name: fieldCellID, Usage: "cell identifier a registered tenant is bound to", Default: "cell-local"},
			{Name: fieldMaxDeadline, Usage: "server-imposed cap on every request deadline", Default: "30s", Kind: bootstrap.KindDuration},
			{Name: fieldMigrate, Usage: "apply pending migrations before the listeners start", Default: "true", Kind: bootstrap.KindBool},
		},
		Validate:         validateServeConfig,
		DatabaseURLField: fieldDatabaseURL,
		DBPoolFactory: func(ctx context.Context, url string) (bootstrap.DBPool, error) {
			opened, err := pgxpool.New(ctx, url)
			if err != nil {
				return nil, fmt.Errorf("connect: %w", err)
			}
			if err := opened.Ping(ctx); err != nil {
				opened.Close()
				return nil, fmt.Errorf("ping: %w", err)
			}
			pool = opened
			return poolPort{pool: opened}, nil
		},
		Build: func(ctx context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			return buildServe(ctx, deps, pool)
		},
		ShutdownDeadline: shutdownGrace,
	}
}

// poolPort adapts the concrete pool to bootstrap's narrow database port.
type poolPort struct{ pool *pgxpool.Pool }

func (p poolPort) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p poolPort) Close()                         { p.pool.Close() }

// validateServeConfig rejects a configuration a listener must not start on.
func validateServeConfig(values *bootstrap.Values) error {
	if values.String(fieldDatabaseURL) == "" {
		return fmt.Errorf("%s is not set; pass -%s or set the environment variable",
			EnvDatabaseURL, fieldDatabaseURL)
	}
	if len(values.String(fieldDevHMACKey)) < minimumHMACKeyBytes {
		return fmt.Errorf("-%s must be at least %d bytes; a listener that cannot authenticate must not start",
			fieldDevHMACKey, minimumHMACKeyBytes)
	}
	if _, err := values.Duration(fieldMaxDeadline); err != nil {
		return err
	}
	if _, err := values.Bool(fieldMigrate); err != nil {
		return err
	}
	return nil
}

// buildServe applies the schema, composes the cell and returns the two
// listeners as workloads plus their graceful-stop steps.
func buildServe(ctx context.Context, deps bootstrap.Deps, pool *pgxpool.Pool) (bootstrap.Runtime, error) {
	values := deps.Values

	migrate, err := values.Bool(fieldMigrate)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	if migrate {
		if err := migrateUp(ctx, values.String(fieldDatabaseURL), deps.Logger); err != nil {
			return bootstrap.Runtime{}, err
		}
	}

	store, err := pgstore.New(pool, pgstore.WithCellID(values.String(fieldCellID)))
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	if tenant := values.String(fieldTenant); tenant != "" {
		if err := store.Bootstrap(ctx, tenant); err != nil {
			return bootstrap.Runtime{}, err
		}
		deps.Logger.Info("hcmnext.tenant_registered", "tenant", tenant, "cell_id", values.String(fieldCellID))
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(values.String(fieldDevHMACKey)),
		Issuer:   values.String(fieldIssuer),
		Audience: values.String(fieldAudience),
	})
	if err != nil {
		return bootstrap.Runtime{}, fmt.Errorf("build the credential verifier: %w", err)
	}

	maxDeadline, err := values.Duration(fieldMaxDeadline)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	cell, err := app.NewCell(app.CellConfig{
		Store:       store,
		Verifier:    verifier,
		Audience:    values.String(fieldAudience),
		MaxDeadline: maxDeadline,
		Logger:      transport.LoggerFunc(requestLogger(deps.Logger)),
	})
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	grpcServer, err := cell.GRPCServer()
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	edgeHandler, err := cell.EdgeHandler()
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	grpcListener, err := net.Listen("tcp", values.String(fieldGRPCListen))
	if err != nil {
		return bootstrap.Runtime{}, fmt.Errorf("listen on %s: %w", values.String(fieldGRPCListen), err)
	}
	httpListener, err := net.Listen("tcp", values.String(fieldHTTPListen))
	if err != nil {
		_ = grpcListener.Close()
		return bootstrap.Runtime{}, fmt.Errorf("listen on %s: %w", values.String(fieldHTTPListen), err)
	}
	httpServer := &http.Server{Handler: edgeHandler, ReadHeaderTimeout: 10 * time.Second}

	deps.Logger.Info("hcmnext.serving",
		"grpc", grpcListener.Addr().String(),
		"http", httpListener.Addr().String(),
		"discovery", app.DiscoveryPath,
		"definitions", cell.Definitions.Len(),
		"capabilities", len(cell.Capabilities.List()))

	return bootstrap.Runtime{
		Workloads: []bootstrap.Workload{
			{
				Name: "grpc-surface",
				Run: func(context.Context) error {
					if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, net.ErrClosed) {
						return err
					}
					return nil
				},
			},
			{
				Name: "http-edge",
				Run: func(context.Context) error {
					if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
						return err
					}
					return nil
				},
			},
		},
		// Both surfaces drain gracefully first: in-flight requests finish and
		// new ones are refused. A request that outlives bootstrap's shutdown
		// deadline is stopped rather than allowed to hold the process open.
		Shutdown: []bootstrap.ShutdownStep{
			{Name: "stop-http-edge", Run: httpServer.Shutdown},
			{
				Name: "stop-grpc-surface",
				Run: func(stepCtx context.Context) error {
					stopped := make(chan struct{})
					go func() {
						grpcServer.GracefulStop()
						close(stopped)
					}()
					select {
					case <-stopped:
						return nil
					case <-stepCtx.Done():
						grpcServer.Stop()
						<-stopped
						return stepCtx.Err()
					}
				},
			},
		},
	}, nil
}

// requestLogger emits one structured record per completed request. It is
// deliberately field-by-field rather than a formatted blob: the fields are the
// contract, and the redacting handler behind them is what keeps a request log
// from becoming an export channel.
func requestLogger(logger bootstrap.Logger) func(transport.LogRecord) {
	return func(record transport.LogRecord) {
		outcome := "OK"
		if !record.Succeeded() {
			outcome = record.Code.String() + " " + record.ReasonRef
		}
		logger.Info("hcmnext.request",
			"method", record.Method,
			"transport", record.Transport,
			"request_id", record.RequestID,
			"tenant", record.TenantID,
			"purpose", record.Purpose,
			"outcome", outcome,
			"duration", record.Duration.String())
	}
}

// migrateUp applies every pending migration with Goose over the embedded tree.
//
// It is deliberately the plain apply. cmd/migrate owns the journaled one
// (DB-006: artifact digest, tool version, per-migration checksum verification,
// owner, start and finish), and duplicating that here would create a second
// migration authority - exactly the "competing migration roots" NEXT-004 names
// as a failure.
func migrateUp(ctx context.Context, url string, logger bootstrap.Logger) error {
	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse the database URL: %w", err)
	}
	db := stdlib.OpenDB(*connCfg)
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(
		goose.DialectPostgres, db, migrations.FS,
		goose.WithVerbose(false),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("build the migration provider: %w", err)
	}
	applied, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	target, err := migrations.TargetVersion()
	if err != nil {
		return err
	}
	logger.Info("hcmnext.schema_applied", "version", target, "applied_this_start", len(applied))
	return nil
}
