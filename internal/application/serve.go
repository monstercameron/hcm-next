package application

// The serve role's composition.
//
// This file is the whole of what "hcmnext serve" is made of. It moved here
// from cmd/hcmnext/main.go unchanged in behaviour and deliberately changed in
// ownership: a command may decide *that* a role runs, this package decides
// *what that role is*. The practical difference is that a test can now
// compose the deployed wiring - the same store, the same verifier, the same
// gateway, the same two transports, the same ordered shutdown - and swap one
// adapter, without rebuilding a parallel version of it and hoping the two
// stayed in step.
//
// Nothing here registers anything at init time, reads a package-level
// variable, or looks a dependency up by name at runtime. Every value below is
// constructed from ServeConfig and Options and passed to whoever needs it.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/humanwork/workspace"
	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
	"github.com/monstercameron/hcm-next/internal/platform/logging"
	"github.com/monstercameron/hcm-next/internal/transport"
	transportcell "github.com/monstercameron/hcm-next/internal/transport/cell"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// Component names recorded in the composed [Graph]. They are constants so a
// test asserts on the same string the composition wrote, and so a rename is
// one edit rather than a silently diverging golden.
const (
	ComponentConfig                = "config"
	ComponentDatabasePool          = "database-pool"
	ComponentSchemaMigrator        = "schema-migrator"
	ComponentIntentStore           = "intent-store"
	ComponentCredentialVerifier    = "credential-verifier"
	ComponentTelemetryProvider     = "telemetry-provider"
	ComponentEvidenceSink          = "evidence-sink"
	ComponentDomainInputs          = "domain-inputs"
	ComponentWorkerFacts           = "worker-facts"
	ComponentPayBandCatalog        = "pay-band-catalog"
	ComponentTransactionHistory    = "transaction-history"
	ComponentIncumbentConnector    = "incumbent-connector"
	ComponentObservationStore      = "observation-store"
	ComponentTrustedClock          = "trusted-clock"
	ComponentDiscoveryDocument     = "discovery-document"
	ComponentExecutionAuthority    = "execution-authority"
	ComponentProposalExecutor      = "proposal-executor"
	ComponentWorkflowResolver      = "workflow-resolver"
	ComponentWorkflowVersions      = "workflow-versions"
	ComponentWorkflowInstanceRead  = "workflow-instance-reader"
	ComponentCell                  = "cell"
	ComponentIntentDefinitions     = "intent-definitions"
	ComponentCapabilityRegistry    = "capability-registry"
	ComponentCapabilityGateway     = "capability-gateway"
	ComponentIntentService         = "intent-service"
	ComponentJourneyEngine         = "journey-engine"
	ComponentGRPCSurface           = "grpc-surface"
	ComponentHTTPEdge              = "http-edge"
	ComponentWorkloadGRPC          = "workload:grpc-surface"
	ComponentWorkloadHTTP          = "workload:http-edge"
	ComponentShutdownHTTP          = "shutdown:stop-http-edge"
	ComponentShutdownGRPC          = "shutdown:stop-grpc-surface"
	ComponentShutdownTelemetry     = "shutdown:shutdown-telemetry"
	workloadNameGRPC               = "grpc-surface"
	workloadNameHTTP               = "http-edge"
	shutdownNameHTTP               = "stop-http-edge"
	shutdownNameGRPC               = "stop-grpc-surface"
	shutdownNameTelemetry          = "shutdown-telemetry"
	httpEdgeReadHeaderTimeoutValue = 10 * time.Second
)

// ServeInput is everything ComposeServe needs that is not a decision it makes
// itself: the validated configuration, the database pool bootstrap opened,
// the process logger and identity, and the explicit composition seams.
type ServeInput struct {
	Config ServeConfig
	// Pool is the one pool this process opened. It is required for the
	// PostgreSQL store, the operator surface's workflow-instance reader and
	// the P1B execution driver; a composition that supplies its own store,
	// leaves -execution-authority off and does not exercise the operator
	// read may pass nil.
	Pool     *pgxadapter.Pool
	Logger   bootstrap.Logger
	Identity string
	Options  Options
}

// ComposeServe builds the serve role: the store, the verifier, the telemetry
// provider, the evidence sink, the cell (its intent and capability
// registries, its governed gateway and its application service), the optional
// P1B execution authority, both transports, their listeners and the ordered
// shutdown that drains them.
func ComposeServe(ctx context.Context, in ServeInput) (*App, error) {
	cfg := in.Config
	options := in.Options
	logger := in.Logger
	if logger == nil {
		logger = discardLogger{}
	}
	graph := newGraphBuilder(RoleServe)
	graph.add(ComponentConfig, KindConfig, cfg)
	graph.add(ComponentDatabasePool, KindAdapter, in.Pool)

	graph.add(ComponentSchemaMigrator, KindAdapter, options.Migrate)
	if cfg.Migrate {
		if options.Migrate == nil {
			return nil, fmt.Errorf("application: -%s=true needs a schema migrator; the command supplies one", FieldMigrate)
		}
		if err := options.Migrate(ctx, cfg.DatabaseURL, logger); err != nil {
			return nil, err
		}
	}

	store, err := composeStore(in.Pool, cfg, options)
	if err != nil {
		return nil, err
	}
	graph.add(ComponentIntentStore, KindAdapter, store, ComponentDatabasePool, ComponentConfig)
	if cfg.Tenant != "" {
		if err := store.Bootstrap(ctx, cfg.Tenant); err != nil {
			return nil, err
		}
		logger.Info("hcmnext.tenant_registered", "tenant", cfg.Tenant, "cell_id", cfg.CellID)
	}

	verifier, err := composeVerifier(cfg, options)
	if err != nil {
		return nil, err
	}
	graph.add(ComponentCredentialVerifier, KindAdapter, verifier, ComponentConfig)

	newTelemetry := options.NewTelemetry
	if newTelemetry == nil {
		newTelemetry = NewTelemetryProvider
	}
	telemetryProvider, err := newTelemetry(ctx, in.Identity, cfg)
	if err != nil {
		return nil, fmt.Errorf("build telemetry provider: %w", err)
	}
	graph.add(ComponentTelemetryProvider, KindAdapter, telemetryProvider, ComponentConfig)
	telemetryCommitted := false
	defer func() {
		if telemetryCommitted || telemetryProvider == nil {
			return
		}
		LogTelemetryShutdown(logger, telemetryProvider.Shutdown(context.Background()))
	}()

	// One evidence sink for the whole process: the cell's gateway and gate
	// decisions and, when the execution authority is composed, the driver's
	// own execution evidence all land on it, so the journey's Inspect reads
	// one chronology.
	evidence := options.Evidence
	if evidence == nil {
		evidence = app.NewMemoryEvidenceSink()
	}
	graph.add(ComponentEvidenceSink, KindRegistry, evidence)

	workspaceEnabled := cfg.Workspace
	cellConfig := app.CellConfig{
		Store:           store,
		Verifier:        verifier,
		Audience:        cfg.Audience,
		MaxDeadline:     cfg.MaxDeadline,
		Logger:          transport.LoggerFunc(RequestLogger(logger)),
		Workspace:       &workspaceEnabled,
		DevBrowserLogin: cfg.DevBrowserLogin,
		Evidence:        evidence,
		Telemetry:       telemetryProvider,
		Inputs:          options.Inputs,
		Workers:         options.Workers,
		Bands:           options.Bands,
		Now:             options.Now,
		Clock:           options.Clock,
		IDs:             options.IDs,
	}
	graph.add(ComponentPayBandCatalog, KindPort, cellConfig.Bands)

	if cfg.ExecutionAuthority {
		composeExecution := options.ComposeExecution
		if composeExecution == nil {
			composeExecution = ComposeExecutionAuthority
		}
		if err := composeExecution(&cellConfig, in.Pool, evidence, cfg); err != nil {
			return nil, fmt.Errorf("compose the P1B execution authority: %w", err)
		}
		logger.Info("hcmnext.execution_authority_enabled",
			"role", cfg.ExecutionAuthorityRole, "cell_id", cfg.CellID)
	}
	graph.add(ComponentExecutionAuthority, KindGovernance, cellConfig.ExecutionAuthority, ComponentConfig, ComponentDatabasePool, ComponentEvidenceSink)
	graph.add(ComponentProposalExecutor, KindWorkflow, cellConfig.Executor, ComponentExecutionAuthority)
	graph.add(ComponentWorkflowResolver, KindWorkflow, cellConfig.ExecutionResolver, ComponentExecutionAuthority)
	graph.add(ComponentWorkflowVersions, KindWorkflow, cellConfig.ExecutionVersions, ComponentExecutionAuthority)

	cell, err := app.NewCell(cellConfig)
	if err != nil {
		return nil, err
	}
	graph.add(ComponentCell, KindRegistry, cell,
		ComponentIntentStore, ComponentCredentialVerifier, ComponentTelemetryProvider,
		ComponentEvidenceSink, ComponentPayBandCatalog, ComponentProposalExecutor)
	// The governed read ports, the connectivity plane and the trusted clock
	// are recorded as the cell resolved them, not as this root proposed them:
	// a seam left nil is a decision to take the cell's own default corpus,
	// and the graph should say which corpus that turned out to be.
	graph.add(ComponentDomainInputs, KindPort, cell.Inputs)
	graph.add(ComponentWorkerFacts, KindPort, cell.Workers)
	graph.add(ComponentTransactionHistory, KindPort, cell.Transactions)
	graph.add(ComponentIncumbentConnector, KindAdapter, cell.Incumbent)
	graph.add(ComponentObservationStore, KindAdapter, cell.Observations)
	graph.add(ComponentTrustedClock, KindEngine, cell.Clock)
	graph.add(ComponentDiscoveryDocument, KindRegistry, cell.Discovery, ComponentCell)
	graph.add(ComponentIntentDefinitions, KindRegistry, cell.Definitions, ComponentCell)
	graph.add(ComponentCapabilityRegistry, KindRegistry, cell.Capabilities, ComponentCell)
	graph.add(ComponentCapabilityGateway, KindGovernance, cell.Gateway, ComponentCapabilityRegistry)
	graph.add(ComponentIntentService, KindEngine, cell.Service, ComponentCell)
	graph.add(ComponentJourneyEngine, KindWorkflow, cell.Journey, ComponentCell)

	var schedulerWorkload bootstrap.Workload
	if cfg.Scheduler {
		schedulerWorkload, err = composeSchedulerWorkload(cfg, in.Pool, in.Identity, cell, logger)
		if err != nil {
			return nil, fmt.Errorf("compose workflow scheduler: %w", err)
		}
	}

	// internal/transport/cell chains the otelmw interceptors itself when this
	// cell was composed with a Telemetry provider (nil, when
	// -otel-exporter=none, means neither call adds one); no interceptor
	// options are passed here. It is the composition adapter, not app.Cell
	// directly, because only internal/transport may import grpc-go/Connect
	// (LIB-003).
	//
	// AdminService.GetWorkflowInstance (ADMIN-008) additionally needs the
	// application-side workflow instance reader, which app.Cell itself has no
	// field for because the operator surface is served whether or not the
	// execution authority is composed; the pool and the same tenant-key
	// derivation ComposeExecutionAuthority uses are in scope here, so this
	// composition root is what builds it.
	workflowInstanceReader := app.NewWorkflowInstanceReader(in.Pool,
		tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
	graph.add(ComponentWorkflowInstanceRead, KindPort, workflowInstanceReader, ComponentDatabasePool)

	grpcServer, err := transportcell.NewGRPCServerWithWorkflowInspector(cell, workflowInstanceReader)
	if err != nil {
		return nil, err
	}
	graph.add(ComponentGRPCSurface, KindTransport, grpcServer, ComponentCell, ComponentWorkflowInstanceRead)

	edgeHandler, err := transportcell.NewEdgeHandlerWithTunnel(cell, grpcServer)
	if err != nil {
		return nil, err
	}
	graph.add(ComponentHTTPEdge, KindTransport, edgeHandler, ComponentCell, ComponentGRPCSurface)

	listen := options.listen()
	grpcListener, err := listen("tcp", cfg.GRPCListen)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", cfg.GRPCListen, err)
	}
	httpListener, err := listen("tcp", cfg.HTTPListen)
	if err != nil {
		_ = grpcListener.Close()
		return nil, fmt.Errorf("listen on %s: %w", cfg.HTTPListen, err)
	}
	httpServer := &http.Server{Handler: edgeHandler, ReadHeaderTimeout: httpEdgeReadHeaderTimeoutValue}

	workspacePath := "disabled"
	if workspaceEnabled {
		workspacePath = workspace.PathPromotion
	}
	logger.Info("hcmnext.serving",
		"grpc", grpcListener.Addr().String(),
		"http", httpListener.Addr().String(),
		"discovery", app.DiscoveryPath,
		"tunnel", transportcell.TunnelPath,
		"workspace", workspacePath,
		"dev_browser_login", cfg.DevBrowserLogin,
		"otel_exporter", cfg.OTelExporter,
		"definitions", cell.Definitions.Len(),
		"capabilities", len(cell.Capabilities.List()))
	if cfg.DevBrowserLogin {
		loginURL := "http://" + httpListener.Addr().String() + workspace.PathLogin
		logger.Info("hcmnext.dev_browser_login_enabled", "url", loginURL)
		fmt.Fprintf(os.Stdout, "hcmnext: open %s and paste a bearer credential (see: hcmnext token) to sign in\n", loginURL)
	}

	workloads := []bootstrap.Workload{
		{
			Name: workloadNameGRPC,
			Run: func(context.Context) error {
				if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, net.ErrClosed) {
					return err
				}
				return nil
			},
		},
		{
			Name: workloadNameHTTP,
			Run: func(context.Context) error {
				if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				return nil
			},
		},
	}
	if cfg.Scheduler {
		workloads = append(workloads, schedulerWorkload)
	}
	// Both surfaces drain gracefully first: in-flight requests finish and new
	// ones are refused. A request that outlives the shutdown deadline is
	// stopped rather than allowed to hold the process open.
	shutdown := []bootstrap.ShutdownStep{
		{Name: shutdownNameHTTP, Run: httpServer.Shutdown},
		{
			Name: shutdownNameGRPC,
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
			Name: shutdownNameTelemetry,
			Run: func(stepCtx context.Context) error {
				if telemetryProvider == nil {
					return nil
				}
				LogTelemetryShutdown(logger, telemetryProvider.Shutdown(stepCtx))
				return nil
			},
		},
	}
	graph.add(ComponentWorkloadGRPC, KindWorkload, workloads[0].Run, ComponentGRPCSurface)
	graph.add(ComponentWorkloadHTTP, KindWorkload, workloads[1].Run, ComponentHTTPEdge)
	if cfg.Scheduler {
		graph.add(ComponentWorkloadScheduler, KindWorkload, schedulerWorkload.Run, ComponentCell, ComponentDatabasePool)
	}
	graph.add(ComponentShutdownHTTP, KindShutdown, shutdown[0].Run, ComponentHTTPEdge)
	graph.add(ComponentShutdownGRPC, KindShutdown, shutdown[1].Run, ComponentGRPCSurface)
	graph.add(ComponentShutdownTelemetry, KindShutdown, shutdown[2].Run, ComponentTelemetryProvider)

	// From here the caller's lifecycle owns the provider's lifetime through
	// the ordered shutdown step above. Earlier returns leave this function
	// responsible for cleaning up the partially composed provider.
	telemetryCommitted = true
	return &App{
		role:      RoleServe,
		graph:     graph.graph(),
		logger:    logger,
		cell:      cell,
		grpcAddr:  grpcListener.Addr().String(),
		httpAddr:  httpListener.Addr().String(),
		workloads: workloads,
		shutdown:  shutdown,
		listeners: []net.Listener{grpcListener, httpListener},
	}, nil
}

// composeStore builds the persistence adapter, or takes the supplied one.
func composeStore(pool *pgxadapter.Pool, cfg ServeConfig, options Options) (app.Store, error) {
	if options.NewStore != nil {
		store, err := options.NewStore(pool, cfg)
		if err != nil {
			return nil, err
		}
		if store == nil {
			return nil, fmt.Errorf("application: the supplied store factory returned no store")
		}
		return store, nil
	}
	return pgstore.New(pool, pgstore.WithCellID(cfg.CellID))
}

// composeVerifier builds the credential verifier, or takes the supplied one.
func composeVerifier(cfg ServeConfig, options Options) (trust.Verifier, error) {
	if options.NewVerifier != nil {
		verifier, err := options.NewVerifier(cfg)
		if err != nil {
			return nil, err
		}
		if verifier == nil {
			return nil, fmt.Errorf("application: the supplied verifier factory returned no verifier")
		}
		return verifier, nil
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(cfg.DevHMACKey),
		Issuer:   cfg.Issuer,
		Audience: cfg.Audience,
	})
	if err != nil {
		return nil, fmt.Errorf("build the credential verifier: %w", err)
	}
	return verifier, nil
}

// ServeSpec declares the serve role for internal/platform/bootstrap: its
// configuration, its validation, its database dependency and the workloads
// bootstrap runs and drains. It is the one call a command makes.
func ServeSpec(args []string, opts ...Option) bootstrap.Spec {
	options := Options{}.Apply(opts...)

	// The store needs a pool it can Begin and Query on, and bootstrap's
	// DBPool port is deliberately narrower than that. The factory therefore
	// keeps the adapter for Build while still handing bootstrap the port it
	// owns, so there is exactly one pool, opened and closed once, on
	// bootstrap's schedule rather than on this file's.
	var pool *pgxadapter.Pool

	logger := options.Logger
	if logger == nil {
		logger = slog.New(logging.NewHandler(os.Stdout, logging.WithService("hcmnext")))
	}

	return bootstrap.Spec{
		Role:             bootstrap.RoleHCMNext,
		Args:             args,
		Logger:           logger,
		ConfigFields:     ServeConfigFields(),
		Validate:         ValidateServeValues,
		DatabaseURLField: FieldDatabaseURL,
		HealthAddr:       HealthAddrOf(args),
		DBPoolFactory: func(ctx context.Context, url string) (bootstrap.DBPool, error) {
			opened, err := pgxadapter.NewPool(ctx, url, nil)
			if err != nil {
				return nil, fmt.Errorf("connect: %w", err)
			}
			pool = opened
			return opened, nil
		},
		Build: func(ctx context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			cfg, err := ServeConfigFromValues(deps.Values)
			if err != nil {
				return bootstrap.Runtime{}, err
			}
			composed, err := ComposeServe(ctx, ServeInput{
				Config:   cfg,
				Pool:     pool,
				Logger:   deps.Logger,
				Identity: deps.Identity,
				Options:  options,
			})
			if err != nil {
				return bootstrap.Runtime{}, err
			}
			return composed.Runtime(), nil
		},
		ShutdownDeadline: ShutdownGrace,
	}
}

// HealthAddrOf pre-resolves -health-addr from args with the same precedence
// bootstrap.Run applies (flag, then environment, then default). Spec.HealthAddr
// is the one setting Run reads before it parses Spec.ConfigFields, so the
// serve role resolves it the way cmd/worker and cmd/scheduler do: a pure,
// side-effect-free rerun of the parse Run performs moments later. A bad flag
// here yields an empty address and is reported, correctly, by Run's own parse.
func HealthAddrOf(args []string) string {
	values, err := bootstrap.ParseConfig(args, nil, ServeConfigFields())
	if err != nil {
		return ""
	}
	return values.String(FieldHealthAddr)
}

// RequestLogger emits one structured record per completed request. It is
// deliberately field-by-field rather than a formatted blob: the fields are
// the contract, and the redacting handler behind them is what keeps a request
// log from becoming an export channel.
func RequestLogger(logger bootstrap.Logger) func(transport.LogRecord) {
	return func(record transport.LogRecord) {
		if logger == nil {
			return
		}
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

// discardLogger is the composition's own no-op logger. It exists so
// ComposeServe never has to branch on "are we under test": a caller that has
// no logger gets one that discards, and every log call site below stays
// unconditional.
type discardLogger struct{}

func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Error(string, ...any) {}
