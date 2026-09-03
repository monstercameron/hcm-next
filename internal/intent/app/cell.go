package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/hcm-next/internal/connectivity/observe"
	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/intelligence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/definitions"
	"github.com/monstercameron/hcm-next/internal/intent/protomap"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
	"github.com/monstercameron/hcm-next/internal/platform/timeauth"
	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/transport/manifest"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	workflowversion "github.com/monstercameron/hcm-next/internal/workflow/version"

	"github.com/google/uuid"
)

// ObservationFreshnessBudget is how old a page's watermark may be before the
// connectivity plane reports it as stale. It is the connector contract's own
// budget, and it is a different question from the comparison's freshness
// policy: one judges the page, the other judges whether a finding drawn from
// it may be called a mismatch.
const ObservationFreshnessBudget = 24 * time.Hour

// CellConfig is everything a P1A cell needs that it cannot decide for itself.
type CellConfig struct {
	// Store is the persistence port. Required.
	Store Store
	// Verifier turns a presented credential into a principal. Required: a
	// listener that cannot authenticate must not start.
	Verifier trust.Verifier
	// Audience is the audience this cell answers to.
	Audience string
	// MaxDeadline caps every request's deadline. Zero means the transport
	// default.
	MaxDeadline time.Duration
	// Logger receives one structured record per completed request.
	Logger transport.Logger
	// Inputs resolves the governed reads a P1A intent needs. Nil means
	// [NewFixtureInputs], and then Workers and Bands default to the same
	// corpus.
	Inputs DomainInputs
	// Workers is the governed worker read port the people capability answers
	// from. Nil is only valid when Inputs is nil too.
	Workers people.WorkerFacts
	// Bands is the pay band catalog the rewards and promotion capabilities
	// evaluate against. Nil is only valid when Inputs is nil too.
	Bands rewards.PayBandCatalog
	// Incumbent is the external system of record the cross-system
	// diagnostics observe. Nil means the in-memory fake incumbent, which is
	// the P1A design-partner stand-in until a real provider edition is
	// selected (planning/next-steps.md M1).
	Incumbent connectivity.Connector
	// Connection is the tenant-scoped connection Incumbent is read under. Nil
	// means a connection drafted and enabled against the fake incumbent's own
	// published definition.
	Connection *connectivity.ConnectorConnection
	// Observations persists the immutable observation evidence every
	// comparison rests on. Nil means an in-memory store.
	Observations observe.ObservationStore
	// IDs and Clock are seams for deterministic composition. Nil means
	// UUIDv7 and the trusted host clock.
	IDs   intent.IDSource
	Clock intent.Clock
	// Now supplies the capability gateway's evidence timestamps and the
	// trusted clock's readings. Nil means time.Now in UTC.
	Now func() time.Time
	// Workspace controls whether the HTTP edge serves the human-facing
	// Promotion workspace (internal/humanwork/workspace) alongside the RPC
	// projection. Nil means served.
	//
	// It is a pointer because the default is on and the zero value of a bool
	// is off: a deployment that wants the API surface without the human one
	// has to say so, rather than getting it by forgetting to set a field.
	Workspace *bool
	// DevBrowserLogin enables the workspace's dev-only pasted-token sign-in
	// flow (internal/humanwork/workspace's PathLogin/PathLogout). Off by
	// default. It is a plain value, read back off the composed Cell by
	// internal/transport/cell, which is the package that actually knows
	// about internal/humanwork/workspace.Options.DevBrowserLogin - this
	// package must not import anything transport-shaped to state it.
	DevBrowserLogin bool
	// Telemetry is the OTel provider every request is instrumented through.
	// Nil means off: a cell composed with no Telemetry publishes no spans or
	// metrics at all, rather than falling back to some default exporter a
	// caller did not ask for. Like DevBrowserLogin, this package only carries
	// the value; internal/transport/cell is what chains
	// otelmw.UnaryServerInterceptor/otelmw.NewConnectInterceptor from it,
	// because only internal/transport may import connect/grpc (LIB-003).
	Telemetry *hcmotel.Provider

	// Executor is the caller-driven workflow driver ExecuteIntent runs an
	// approved promotion proposal through. Nil (the default for every
	// composition today) leaves EXECUTE unavailable regardless of
	// ExecutionAuthority: P1A cells never execute.
	//
	// internal/transport/cell, not this package, is what may build a real
	// *internal/workflow/execute.Driver and adapt it to this port: only
	// internal/transport may own that composition (see this file's own
	// Telemetry/DevBrowserLogin split for why).
	Executor ProposalExecutor
	// ExecutionAuthority is the explicit P1B gate ExecuteIntent requires
	// before it will run Executor at all. Nil means this cell behaves
	// byte-for-byte like the P1A cell of today.
	ExecutionAuthority *ExecutionAuthority
	// ExecutionResolver and ExecutionVersions are Executor's own workflow
	// resolver and version store. Required together with Executor; either
	// missing leaves ExecuteIntent refusing as unavailable past the
	// authority gate.
	ExecutionResolver runtime.WorkflowResolver
	ExecutionVersions workflowversion.Store
	// ExecutionCellID names the cell runtime.StartRequest.CellID records.
	// Empty means "cell-local".
	ExecutionCellID string
	// TenantUUID maps a tenant key onto the uuid this cell's composed Store
	// uses for that tenant's row (internal/intent/app/pgstore.TenantID,
	// wrapped, in every real composition). Required together with Executor.
	TenantUUID func(values.TenantId) uuid.UUID
}

// Cell is one composed P1A application cell: the registries, the governed
// gateway and the application service. It carries everything a transport
// composition needs to publish it, but does not publish it itself -
// internal/transport/cell attaches the gRPC server and the HTTP/Connect edge
// (including the OTel interceptors and the human-facing workspace), because
// only internal/transport may import grpc-go/Connect/protobuf directly
// (LIB-003; internal/intent/app is not on that qualification's allowed-roots
// list).
//
// Composition lives here rather than in cmd/hcmnext so that the bootstrap test
// runs the same wiring the binary runs. A cell a test assembles differently
// from the way the process assembles it proves nothing about the process.
type Cell struct {
	Service      *IntentService
	Definitions  *intent.Registry
	Capabilities *capability.Registry
	// Workers and Transactions are the governed read ports the operator
	// surface (internal/transport/admin) forwards to. They are the same
	// values the capability handlers answer from, so an operator never
	// reads through a second path.
	Workers      people.WorkerFacts
	Transactions intelligence.TransactionHistory
	Gateway      *capability.Gateway
	Evidence     *MemoryEvidenceSink
	Controls     Controls
	Config       transport.Config
	Inputs       DomainInputs

	// Incumbent, Connection and Observations are the connectivity plane this
	// cell observes the external system of record through. They are exposed so
	// an operator surface - and the conformance suite - can read the call log
	// and the recorded evidence without a second connection.
	Incumbent    connectivity.Connector
	Connection   *connectivity.ConnectorConnection
	Observations observe.ObservationStore

	// Clock is the temporal-evidence monitor behind the recording clock.
	Clock *timeauth.Monitor
	// Discovery is the rendered API-001 served shape.
	Discovery *manifest.DiscoveryDocument
	// Telemetry is the OTel provider from CellConfig, or nil. Exported so
	// internal/transport/cell can read it without this package exposing any
	// transport-shaped composition of its own.
	Telemetry *hcmotel.Provider

	// workspaceEnabled records whether the edge publishes the human-facing
	// workspace. It is not exported: whether a surface is served is decided
	// at composition, and a handler that could be switched on afterwards
	// would be a served shape the discovery document had already denied.
	// [Cell.WorkspaceEnabled] is the read-only accessor.
	workspaceEnabled bool
	// devBrowserLogin records whether the workspace's dev-only sign-in flow
	// is enabled. Same reasoning as workspaceEnabled: fixed at composition,
	// read through [Cell.DevBrowserLogin].
	devBrowserLogin bool
}

// NewCell composes a cell.
func NewCell(cfg CellConfig) (*Cell, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("app: a Store is required to compose a cell")
	case cfg.Verifier == nil:
		return nil, errors.New("app: a trust.Verifier is required to compose a cell")
	}

	defs, err := definitions.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("app: compile the definition catalog: %w", err)
	}
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		return nil, fmt.Errorf("app: build the canonical digester: %w", err)
	}

	inputs, workers, bands := cfg.Inputs, cfg.Workers, cfg.Bands
	var fixtureBacked *FixtureInputs
	if inputs == nil {
		fixtureBacked, err = NewFixtureInputs()
		if err != nil {
			return nil, err
		}
		inputs = fixtureBacked
		if workers == nil {
			workers = fixtureBacked.Workers()
		}
		if bands == nil {
			bands = fixtureBacked.Bands()
		}
	}
	if workers == nil || bands == nil {
		return nil, errors.New("app: a cell with its own DomainInputs must also supply Workers and Bands")
	}

	incumbent, connection, err := resolveConnectivity(cfg)
	if err != nil {
		return nil, err
	}
	observations := cfg.Observations
	if observations == nil {
		observations = observe.NewMemoryStore()
	}
	external, err := NewExternalObservations(incumbent, connection, observations, ObservationFreshnessBudget)
	if err != nil {
		return nil, err
	}
	if fixtureBacked != nil {
		fixtureBacked.BindExternalSource(external.SourceRef())
	}

	handlers := &domainHandlers{
		workers:      workers,
		bands:        bands,
		history:      &workerFieldHistory{workers: workers},
		observations: external,
		transactions: &ledgerTransactions{store: cfg.Store, defs: defs},
	}

	caps, err := newCapabilityRegistry(handlers)
	if err != nil {
		return nil, err
	}
	sink := NewMemoryEvidenceSink()
	var gatewayOptions []capability.GatewayOption
	if cfg.Now != nil {
		gatewayOptions = append(gatewayOptions, capability.WithClock(cfg.Now))
	}
	gateway := capability.NewGateway(caps, sink, gatewayOptions...)
	controls := NewControls(defs, caps)

	// The recording clock is monitored, not read directly: every instant this
	// cell stamps onto the chronology has passed a drift and uncertainty check
	// first. A caller-supplied Clock still wins, because a deterministic
	// composition (a fixture, a replay) is pinning the time on purpose.
	monitor, err := timeauth.NewMonitor(newHostClock(cfg.Now), timeauth.Options{})
	if err != nil {
		return nil, fmt.Errorf("app: build the temporal-evidence monitor: %w", err)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = NewTrustedClock(monitor)
	}

	svc, err := NewIntentService(Options{
		Definitions:  defs,
		Capabilities: caps,
		Gateway:      gateway,
		Store:        cfg.Store,
		Inputs:       inputs,
		Digester:     digester,
		Controls:     controls,
		IDs:          cfg.IDs,
		Clock:        clock,

		ProposalExecutor:   cfg.Executor,
		ExecutionAuthority: cfg.ExecutionAuthority,
		ExecutionResolver:  cfg.ExecutionResolver,
		ExecutionVersions:  cfg.ExecutionVersions,
		ExecutionCellID:    cfg.ExecutionCellID,
		TenantUUID:         cfg.TenantUUID,
	})
	if err != nil {
		return nil, err
	}

	endpoints, err := manifest.Build()
	if err != nil {
		return nil, fmt.Errorf("app: build the endpoint manifest: %w", err)
	}
	discovery, err := manifest.RenderDiscoveryDocument(endpoints, defs.Definitions(), caps.List())
	if err != nil {
		return nil, fmt.Errorf("app: render the discovery document: %w", err)
	}

	workspaceEnabled := cfg.Workspace == nil || *cfg.Workspace

	return &Cell{
		workspaceEnabled: workspaceEnabled,
		devBrowserLogin:  cfg.DevBrowserLogin,

		Service:      svc,
		Definitions:  defs,
		Capabilities: caps,
		Workers:      workers,
		Transactions: handlers.transactions,
		Gateway:      gateway,
		Evidence:     sink,
		Controls:     controls,
		Inputs:       inputs,
		Incumbent:    incumbent,
		Connection:   connection,
		Observations: observations,
		Clock:        monitor,
		Discovery:    discovery,
		Telemetry:    cfg.Telemetry,
		Config: transport.Config{
			Verifier:    cfg.Verifier,
			Audience:    cfg.Audience,
			Now:         cfg.Now,
			MaxDeadline: cfg.MaxDeadline,
			Logger:      cfg.Logger,
		},
	}, nil
}

// resolveConnectivity returns the connector and the usable connection the
// cross-system diagnostics read through.
//
// The default is the in-memory fake incumbent, and that is a deployment fact
// rather than a test convenience: P1A has selected no provider edition
// (planning/next-steps.md M1), so the alternative to a declared stand-in would
// be a cell that cannot answer three of its eight intents at all.
func resolveConnectivity(cfg CellConfig) (connectivity.Connector, *connectivity.ConnectorConnection, error) {
	incumbent := cfg.Incumbent
	if incumbent == nil {
		built, err := fakeincumbent.New(fakeincumbent.Options{})
		if err != nil {
			return nil, nil, fmt.Errorf("app: build the stand-in incumbent: %w", err)
		}
		incumbent = built
	}
	if cfg.Connection != nil {
		return incumbent, cfg.Connection, nil
	}
	connection, err := defaultConnection(incumbent)
	if err != nil {
		return nil, nil, err
	}
	return incumbent, connection, nil
}

// defaultConnection drafts and enables a connection against the connector's
// own published definition.
//
// It walks the real lifecycle - DRAFT, VALIDATING, READY, ACTIVE - with
// recorded evidence at every step rather than constructing an ACTIVE
// connection directly, because the lifecycle is the thing that makes a
// connection auditable and skipping it here would mean the composed cell never
// exercises it.
func defaultConnection(incumbent connectivity.Connector) (*connectivity.ConnectorConnection, error) {
	descriptor := incumbent.Descriptor()
	registry := connectivity.NewRegistry()
	definition := fakeincumbent.DefaultDefinition()
	definition.ConnectorID = descriptor.ConnectorID
	definition.Version = descriptor.Version
	definition.Bounds = incumbent.Bounds()

	published, err := registry.Publish(definition, connectivity.PublicationMeta{
		PublishedBy: "build:hcmnext",
		PublishedAt: connectorPublishedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("app: publish the incumbent connector definition: %w", err)
	}
	credential, err := connectivity.ParseCredentialRef("secretref://hcmnext/incumbent/client")
	if err != nil {
		return nil, err
	}
	connection, err := connectivity.NewConnection(published, connectivity.ConnectionSpec{
		ConnectionID:     descriptor.ConnectionID,
		TenantID:         string(defaultConnectionTenant),
		OrgID:            "org-default",
		SystemID:         "sys-incumbent",
		Environment:      connectivity.EnvironmentSandbox,
		Residency:        "us-east",
		ConnectorID:      published.Definition.ConnectorID,
		ConnectorVersion: published.Definition.Version,
		AuthMode:         connectivity.AuthOAuth2ClientCredentials,
		CredentialRef:    credential,
		Scopes:           []string{"worker.read", "position.read", "compensation.read"},
		EndpointPolicy: connectivity.EndpointPolicy{
			AllowedHosts:  []string{"incumbent.invalid"},
			RequireTLS:    true,
			EgressProfile: "cell-egress/us-east",
		},
		Capabilities: connectivity.ReadCapabilities(connectivity.ObjectKinds()...),
		Bounds:       published.Definition.Bounds,
		CreatedAt:    connectorPublishedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("app: draft the incumbent connection: %w", err)
	}
	for i, step := range []connectivity.LifecycleState{
		connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive,
	} {
		if err := connection.Transition(step, connectivity.TransitionEvidence{
			Reason:      "cell_composition",
			ActorRef:    "service:hcmnext",
			EvidenceRef: "evd:connection:" + string(step),
			OccurredAt:  connectorPublishedAt.Add(time.Duration(i+1) * time.Minute),
		}); err != nil {
			return nil, fmt.Errorf("app: enable the incumbent connection: %w", err)
		}
	}
	return connection, nil
}

// connectorPublishedAt is the publication and drafting instant of the
// compiled-in connector definition. It is a build constant rather than a clock
// read so that composing this cell twice produces the same connector digest,
// and therefore the same pinned control context.
var connectorPublishedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// defaultConnectionTenant is the tenant the compiled-in connection is scoped
// to. It is the design-partner corpus tenant, because that is the only
// population this release observes.
const defaultConnectionTenant = fixtures.Tenant

// WorkspaceEnabled reports the immutable serving decision made while the cell
// was composed. internal/transport/cell reads it to decide whether to mount
// the HTML workspace beside the RPC edge.
func (c *Cell) WorkspaceEnabled() bool { return c.workspaceEnabled }

// DevBrowserLogin reports the immutable dev-only workspace sign-in decision
// made while the cell was composed. internal/transport/cell reads it when
// building the workspace handler, the same way it reads WorkspaceEnabled.
func (c *Cell) DevBrowserLogin() bool { return c.devBrowserLogin }
