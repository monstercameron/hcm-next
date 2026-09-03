// Command hcmnext is a composition root. It wires packages; it owns no
// semantics.
//
// With no arguments it prints its build identity and exits, which is what a
// deployment check calls.
//
//	hcmnext          print the build identity
//	hcmnext serve    run the P1A cell: gRPC on -grpc-listen, HTTP edge on -http-listen
//	hcmnext token    mint a bearer credential the -dev-hmac-key verifier accepts
//
// # serve
//
// serve composes one P1A cell (internal/intent/app.NewCell) over a PostgreSQL
// store and publishes it on both transports. The two are handed the same
// transport.Config, which is what makes their trusted context identical by
// construction rather than by review. The HTTP edge additionally serves the
// API-001 discovery document at /v1/discovery and, unless -workspace=false,
// the human-facing Promotion workspace at /workspace/promotion. The workspace
// is admitted by the same bearer credential as the API and reads through the
// same governed capability gateway; -workspace=false publishes the API surface
// alone, and the discovery document then advertises no workspace route.
//
// -dev-browser-login=true additionally serves a dev-only pasted-token sign-in
// form at /workspace/login: off by default, because a workspace that is
// reachable with an Authorization header must not grow a second, cookie-based
// way in unless an operator says so explicitly. When it is on, this command
// prints the exact URL to open once the listeners are up.
//
// -otel-exporter selects none (the default), stdout or otlphttp; none means
// this cell publishes no spans or metrics at all. otlphttp requires
// -otel-endpoint.
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
//
// # token
//
// token mints a bearer credential with the same internal/trust.HMACVerifier
// serve authenticates with, under the same -dev-hmac-key (or
// HCMNEXT_DEV_HMAC_KEY), and prints it to stdout. It is the development
// counterpart of an identity provider: something to hand a curl command, the
// dev browser sign-in form, or a test, without hand-rolling the token format.
// See "hcmnext token -h" for its flags.
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
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/humanwork/workspace"
	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/hcm-next/internal/kernel/values"
	ledgerport "github.com/monstercameron/hcm-next/internal/ledger"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
	"github.com/monstercameron/hcm-next/internal/platform/logging"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
	"github.com/monstercameron/hcm-next/internal/transport"
	transportcell "github.com/monstercameron/hcm-next/internal/transport/cell"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/workflow/execute/effects"
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
	fieldGRPCListen      = "grpc-listen"
	fieldHTTPListen      = "http-listen"
	fieldDatabaseURL     = "database-url"
	fieldDevHMACKey      = "dev-hmac-key"
	fieldIssuer          = "issuer"
	fieldAudience        = "audience"
	fieldTenant          = "tenant"
	fieldCellID          = "cell-id"
	fieldMaxDeadline     = "max-deadline"
	fieldMigrate         = "migrate"
	fieldWorkspace       = "workspace"
	fieldDevBrowserLogin = "dev-browser-login"
	fieldOTelExporter    = "otel-exporter"
	fieldOTelEndpoint    = "otel-endpoint"

	// fieldExecutionAuthority is the P1B execution authority gate family.
	// Every field below defaults to off/empty; a cell composed with no
	// -execution-authority behaves byte-for-byte like the P1A cell of today
	// (internal/intent/app.ExecutionAuthority is nil, and ExecuteIntent
	// refuses exactly as every other governed write in this release does).
	fieldExecutionAuthority       = "execution-authority"
	fieldExecutionAuthorityDigest = "execution-authority-digest"
	fieldExecutionAuthorityRole   = "execution-authority-role"
	fieldExecutionApprover        = "execution-authority-approver"
)

// otelExporterNone, otelExporterStdout and otelExporterOTLPHTTP are the
// allowed values of -otel-exporter. otelExporterNone is the default: a
// listener started with no telemetry flag at all publishes no spans or
// metrics, rather than exporting to stdout by surprise.
const (
	otelExporterNone     = "none"
	otelExporterStdout   = "stdout"
	otelExporterOTLPHTTP = "otlphttp"
)

// shutdownGrace bounds the whole ordered shutdown sequence.
const shutdownGrace = 20 * time.Second

// telemetryShutdownGrace bounds only flushing the process-local OTel
// provider. It remains inside the whole service shutdown deadline above, and
// failed export is intentionally reported without changing the process's
// business shutdown outcome.
const telemetryShutdownGrace = 5 * time.Second

// minimumHMACKeyBytes is the shortest development signing key this listener
// will start with.
const minimumHMACKeyBytes = 32

// defaultIssuer and defaultAudience are serve's own -issuer/-audience
// defaults (see serveSpec's ConfigFields below). token shares these same
// constants for its own -issuer/-audience defaults, so a credential minted
// with no flags beyond -dev-hmac-key/-tenant/-subject verifies against a
// serve process started with no flags beyond its own -dev-hmac-key: the two
// commands cannot drift apart by one of them changing a literal the other
// did not.
const (
	defaultIssuer   = "https://issuer.local.hcm-next.invalid"
	defaultAudience = "hcm-next-api"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		info := buildinfo.Current()
		fmt.Fprintf(os.Stdout, "hcmnext %s revision=%s modified=%t go=%s\n",
			info.Module, info.Revision, info.Modified, info.GoVersion)
		return
	}
	switch args[0] {
	case "serve":
		os.Exit(bootstrap.Run(context.Background(), serveSpec(args[1:])))
	case "token":
		os.Exit(runToken(args[1:], os.Stdout, os.Stderr, time.Now))
	default:
		fmt.Fprintf(os.Stderr, "hcmnext: unknown command %q; usage: hcmnext [serve|token]\n", args[0])
		os.Exit(1)
	}
}

// serveSpec declares the serve role: its configuration, its validation, its
// database dependency and the workloads bootstrap runs and drains.
func serveSpec(args []string) bootstrap.Spec {
	// The store needs a pool it can Begin and Query on, and bootstrap's DBPool
	// port is deliberately narrower than that. The factory therefore keeps the
	// adapter for Build while still handing bootstrap the port it owns, so
	// there is exactly one pool, opened and closed once, on bootstrap's
	// schedule rather than on this file's.
	var pool *pgxadapter.Pool

	return bootstrap.Spec{
		Role:   bootstrap.RoleHCMNext,
		Args:   args,
		Logger: slog.New(logging.NewHandler(os.Stdout, logging.WithService("hcmnext"))),
		ConfigFields: []bootstrap.Field{
			{Name: fieldGRPCListen, Usage: "address the canonical gRPC surface listens on", Default: "127.0.0.1:8443"},
			{Name: fieldHTTPListen, Usage: "address the HTTP edge listens on", Default: "127.0.0.1:8080"},
			{Name: fieldDatabaseURL, Env: EnvDatabaseURL, Usage: "PostgreSQL connection URL"},
			{Name: fieldDevHMACKey, Env: EnvDevHMACKey, Usage: "development HMAC signing key, at least 32 bytes", Secret: true},
			{Name: fieldIssuer, Usage: "the only credential issuer this listener accepts", Default: defaultIssuer},
			{Name: fieldAudience, Usage: "the audience this listener answers to", Default: defaultAudience},
			{Name: fieldTenant, Usage: "tenant slug to register on start; empty registers none"},
			{Name: fieldCellID, Usage: "cell identifier a registered tenant is bound to", Default: "cell-local"},
			{Name: fieldMaxDeadline, Usage: "server-imposed cap on every request deadline", Default: "30s", Kind: bootstrap.KindDuration},
			{Name: fieldMigrate, Usage: "apply pending migrations before the listeners start", Default: "true", Kind: bootstrap.KindBool},
			{Name: fieldWorkspace, Usage: "serve the human-facing Promotion workspace on the HTTP edge", Default: "true", Kind: bootstrap.KindBool},
			{Name: fieldDevBrowserLogin, Usage: "dev-only: serve a pasted-token sign-in form for the workspace at " + workspace.PathLogin, Default: "false", Kind: bootstrap.KindBool},
			{Name: fieldOTelExporter, Usage: "OTel exporter: none, stdout, or otlphttp", Default: otelExporterNone},
			{Name: fieldOTelEndpoint, Usage: "OTLP/HTTP collector endpoint; required when -" + fieldOTelExporter + "=" + otelExporterOTLPHTTP},
			{Name: fieldExecutionAuthority, Usage: "P1B gate: compose this cell with the caller-driven promotion execution driver, so ExecuteIntent can run instead of refusing (planning/next-steps.md \"P1B exists only after a signed Gate A PROCEED\")", Default: "false", Kind: bootstrap.KindBool},
			{Name: fieldExecutionAuthorityDigest, Usage: "the signed P1B authority amendment digest this cell asserts; carried through as evidence, never verified by this process"},
			{Name: fieldExecutionAuthorityRole, Usage: "the principal role ExecuteIntent additionally requires under -" + fieldExecutionAuthority, Default: "promotion_operator"},
			{Name: fieldExecutionApprover, Usage: "the principal the composed promotion approval workflow routes its one approval WorkItem to", Default: "principal:promotion-approver"},
		},
		Validate:         validateServeConfig,
		DatabaseURLField: fieldDatabaseURL,
		DBPoolFactory: func(ctx context.Context, url string) (bootstrap.DBPool, error) {
			opened, err := pgxadapter.NewPool(ctx, url, nil)
			if err != nil {
				return nil, fmt.Errorf("connect: %w", err)
			}
			pool = opened
			return opened, nil
		},
		Build: func(ctx context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			return buildServe(ctx, deps, pool)
		},
		ShutdownDeadline: shutdownGrace,
	}
}

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
	if _, err := values.Bool(fieldWorkspace); err != nil {
		return err
	}
	if _, err := values.Bool(fieldDevBrowserLogin); err != nil {
		return err
	}
	switch exporter := values.String(fieldOTelExporter); exporter {
	case otelExporterNone, otelExporterStdout:
	case otelExporterOTLPHTTP:
		if values.String(fieldOTelEndpoint) == "" {
			return fmt.Errorf("-%s is required when -%s=%s", fieldOTelEndpoint, fieldOTelExporter, otelExporterOTLPHTTP)
		}
	default:
		return fmt.Errorf("-%s must be one of %s, %s, %s; got %q",
			fieldOTelExporter, otelExporterNone, otelExporterStdout, otelExporterOTLPHTTP, exporter)
	}
	executionAuthority, err := values.Bool(fieldExecutionAuthority)
	if err != nil {
		return err
	}
	if executionAuthority {
		if values.String(fieldExecutionAuthorityDigest) == "" {
			return fmt.Errorf("-%s is required when -%s=true", fieldExecutionAuthorityDigest, fieldExecutionAuthority)
		}
		if values.String(fieldExecutionAuthorityRole) == "" {
			return fmt.Errorf("-%s is required when -%s=true", fieldExecutionAuthorityRole, fieldExecutionAuthority)
		}
		if values.String(fieldExecutionApprover) == "" {
			return fmt.Errorf("-%s is required when -%s=true", fieldExecutionApprover, fieldExecutionAuthority)
		}
	}
	return nil
}

// buildServe applies the schema, composes the cell and returns the two
// listeners as workloads plus their graceful-stop steps.
func buildServe(ctx context.Context, deps bootstrap.Deps, pool *pgxadapter.Pool) (bootstrap.Runtime, error) {
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
	workspaceEnabled, err := values.Bool(fieldWorkspace)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	devBrowserLogin, err := values.Bool(fieldDevBrowserLogin)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	telemetryProvider, err := newServeTelemetryProvider(ctx, deps.Identity, values.String(fieldCellID),
		values.String(fieldOTelExporter), values.String(fieldOTelEndpoint))
	if err != nil {
		return bootstrap.Runtime{}, fmt.Errorf("build telemetry provider: %w", err)
	}
	telemetryCommitted := false
	defer func() {
		if telemetryCommitted || telemetryProvider == nil {
			return
		}
		logTelemetryShutdown(deps.Logger, telemetryProvider.Shutdown(context.Background()))
	}()

	cellConfig := app.CellConfig{
		Store:           store,
		Verifier:        verifier,
		Audience:        values.String(fieldAudience),
		MaxDeadline:     maxDeadline,
		Logger:          transport.LoggerFunc(requestLogger(deps.Logger)),
		Workspace:       &workspaceEnabled,
		DevBrowserLogin: devBrowserLogin,
		Telemetry:       telemetryProvider,
	}
	executionAuthorityEnabled, err := values.Bool(fieldExecutionAuthority)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	if executionAuthorityEnabled {
		if err := composeExecutionAuthority(&cellConfig, pool, values); err != nil {
			return bootstrap.Runtime{}, fmt.Errorf("compose the P1B execution authority: %w", err)
		}
		deps.Logger.Info("hcmnext.execution_authority_enabled",
			"role", values.String(fieldExecutionAuthorityRole), "cell_id", values.String(fieldCellID))
	}

	cell, err := app.NewCell(cellConfig)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	// internal/transport/cell chains the otelmw interceptors itself when this
	// cell was composed with a Telemetry provider (nil, when
	// -otel-exporter=none, means neither call adds one); no interceptor
	// options are passed here. It is the composition adapter, not app.Cell
	// directly, because only internal/transport may import grpc-go/Connect
	// (LIB-003).
	grpcServer, err := transportcell.NewGRPCServer(cell)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	edgeHandler, err := transportcell.NewEdgeHandler(cell)
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

	workspacePath := "disabled"
	if workspaceEnabled {
		workspacePath = workspace.PathPromotion
	}
	deps.Logger.Info("hcmnext.serving",
		"grpc", grpcListener.Addr().String(),
		"http", httpListener.Addr().String(),
		"discovery", app.DiscoveryPath,
		"workspace", workspacePath,
		"dev_browser_login", devBrowserLogin,
		"otel_exporter", values.String(fieldOTelExporter),
		"definitions", cell.Definitions.Len(),
		"capabilities", len(cell.Capabilities.List()))
	if devBrowserLogin {
		loginURL := "http://" + httpListener.Addr().String() + workspace.PathLogin
		deps.Logger.Info("hcmnext.dev_browser_login_enabled", "url", loginURL)
		fmt.Fprintf(os.Stdout, "hcmnext: open %s and paste a bearer credential (see: hcmnext token) to sign in\n", loginURL)
	}

	// From here bootstrap owns the provider's lifetime through the ordered
	// shutdown step below. Earlier returns leave this function responsible for
	// cleaning up the partially composed provider.
	telemetryCommitted = true
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
			{
				Name: "shutdown-telemetry",
				Run: func(stepCtx context.Context) error {
					if telemetryProvider == nil {
						return nil
					}
					logTelemetryShutdown(deps.Logger, telemetryProvider.Shutdown(stepCtx))
					return nil
				},
			},
		},
	}, nil
}

// composeExecutionAuthority builds the P1B execution-authority wiring
// -execution-authority=true asks for: the caller-driven promotion execution
// driver (internal/transport/cell.NewPromotionExecution) over pool, its
// governed terminal write (internal/workflow/execute/effects.LedgerTerminalWriter,
// never a second implementation of that write), and the exact tenant-key-to-
// uuid derivation the composed pgstore.Store's own tenant table uses. It
// fills cfg's execution-shaped fields in place; every other field cfg
// already carries is untouched.
func composeExecutionAuthority(cfg *app.CellConfig, pool *pgxadapter.Pool, values *bootstrap.Values) error {
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		return fmt.Errorf("build the ledger event digest registry: %w", err)
	}
	terminal := &effects.LedgerTerminalWriter{
		Appender:       ledgerport.NewAppender(registry),
		ProjectionName: "workflow.promotion_outcome",
		SourceRef:      "cmd/hcmnext:execution-authority",
		Authority:      "authority:execution-authority-flag",
	}
	execution, err := transportcell.NewPromotionExecution(transportcell.PromotionExecutionConfig{
		DB:                  pool,
		Terminal:            terminal,
		CellID:              values.String(fieldCellID),
		ApproverPrincipalID: values.String(fieldExecutionApprover),
		AuthorityDigest:     values.String(fieldExecutionAuthorityDigest),
		RequiredRole:        values.String(fieldExecutionAuthorityRole),
	})
	if err != nil {
		return fmt.Errorf("build the promotion execution driver: %w", err)
	}
	cfg.Executor = execution.Executor
	cfg.ExecutionAuthority = execution.Authority
	cfg.ExecutionResolver = execution.Resolver
	cfg.ExecutionVersions = execution.Versions
	cfg.ExecutionCellID = values.String(fieldCellID)
	cfg.TenantUUID = func(tenant kernelvalues.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) }
	return nil
}

// newServeTelemetryProvider constructs the bounded, policy-enforced provider
// for this API process, or returns (nil, nil) when exporter is
// otelExporterNone: CellConfig.Telemetry treats nil as off, and a listener
// started with no -otel-exporter flag should publish no spans or metrics at
// all rather than defaulting to some exporter nobody asked for.
//
// The resource deliberately contains only service and deployment identity;
// request-specific identifiers are admitted and filtered later by otelmw and
// the telemetry evaluator.
func newServeTelemetryProvider(ctx context.Context, instanceID, cellID, exporter, endpoint string) (*hcmotel.Provider, error) {
	if exporter == otelExporterNone {
		return nil, nil
	}

	var trace hcmotel.TraceConfig
	var metric hcmotel.MetricConfig
	switch exporter {
	case otelExporterStdout:
		trace = hcmotel.TraceConfig{Kind: hcmotel.ExporterKindStdout}
		metric = hcmotel.MetricConfig{Kind: hcmotel.ExporterKindStdout}
	case otelExporterOTLPHTTP:
		trace = hcmotel.TraceConfig{Kind: hcmotel.ExporterKindOTLP, Endpoint: endpoint}
		metric = hcmotel.MetricConfig{Kind: hcmotel.ExporterKindOTLP, Endpoint: endpoint}
	default:
		// validateServeConfig already rejects any other value before Build
		// runs; this default only guards a future caller of this function
		// that skipped that gate.
		return nil, fmt.Errorf("newServeTelemetryProvider: unknown -%s %q", fieldOTelExporter, exporter)
	}

	allowlist, err := telemetry.DefaultAllowlist()
	if err != nil {
		return nil, fmt.Errorf("compile telemetry allowlist: %w", err)
	}
	evaluator := telemetry.NewEvaluator(
		allowlist,
		telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion),
		telemetry.DefaultSamplingPolicy(),
	)
	return hcmotel.NewProvider(ctx, hcmotel.Config{
		Resource: telemetry.NewResourceFromBuild(
			buildinfo.Current(), "hcmnext", instanceID, "development", cellID, "",
			telemetry.ProcessRoleAPI, telemetry.TenantClassStandard,
		),
		Evaluator:       evaluator,
		ShutdownTimeout: telemetryShutdownGrace,
		Trace:           trace,
		Metric:          metric,
	})
}

// logTelemetryShutdown records exporter degradation independently. Telemetry
// is observational: a failed flush must never rewrite the API's result or
// turn an otherwise clean process shutdown into a business failure.
func logTelemetryShutdown(logger bootstrap.Logger, report hcmotel.ShutdownReport) {
	if report.Err() == nil && !report.DeadlineExceeded {
		return
	}
	logger.Error("hcmnext.telemetry_shutdown_degraded",
		"error", report.Err(),
		"deadline_exceeded", report.DeadlineExceeded)
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
