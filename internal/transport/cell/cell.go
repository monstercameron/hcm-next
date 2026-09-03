// Package cell composes an application cell onto its published wire
// surfaces.
//
// It is intentionally the only adapter that knows both an app.Cell and the
// gRPC/Connect server constructors. Application code stays free of RPC
// runtime types; transport owns listener-facing composition and nothing
// semantic. That split is not a style preference here: LIB-003
// (definitions/architecture/library-firewall.yaml, protobuf_grpc) qualifies
// grpc-go, Connect and Protobuf as wire mechanics importable only from gen/,
// internal/transport, internal/intent/protomap, internal/engines/wire,
// tools/gen and the composition roots (cmd/*) - internal/intent/app is not on
// that list, so this package, not app.Cell, is what is allowed to call
// grpcserver.NewServer/edge.NewHandler, chain the otelmw interceptors, and
// render the discovery document's protojson error body.
//
// A composed *app.Cell already carries everything this package reads:
// Config (the shared admission configuration), Discovery (the rendered
// API-001 document), Telemetry (the OTel provider, or nil), and the
// WorkspaceEnabled/DevBrowserLogin decisions made at composition. This
// package adds no new decisions of its own; it only has the import rights
// app.Cell is not allowed to have.
package cell

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/monstercameron/hcm-next/internal/humanwork/workspace"
	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transport"
	transportadmin "github.com/monstercameron/hcm-next/internal/transport/admin"
	"github.com/monstercameron/hcm-next/internal/transport/edge"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/transport/grpcserver"
	"github.com/monstercameron/hcm-next/internal/transport/manifest"
	"github.com/monstercameron/hcm-next/internal/transport/otelmw"
)

// NewGRPCServer builds the canonical gRPC surface over an already composed
// application cell. grpcserver owns the required admission interceptor
// chain; opts can only add mechanics after it. When c was composed with a
// Telemetry provider, NewGRPCServer chains otelmw.UnaryServerInterceptor
// after admission and after opts, so every call - including one added
// through a caller's own opts - is instrumented; a cell composed with no
// Telemetry adds nothing here at all.
//
// It is exactly [NewGRPCServerWithWorkflowInspector] with no workflow
// executor: AdminService.GetWorkflowInstance (ADMIN-008) is then UNAVAILABLE,
// matching every other optional transportadmin.Dependencies port a caller
// does not wire.
func NewGRPCServer(c *app.Cell, opts ...grpc.ServerOption) (*grpc.Server, error) {
	return NewGRPCServerWithWorkflowInspector(c, nil, nil, opts...)
}

// NewGRPCServerWithWorkflowInspector is [NewGRPCServer] plus ADMIN-008's
// workflow-inspector wiring for AdminService.GetWorkflowInstance.
//
// workflowExecutor is the pooled database handle (a
// internal/data/pgxadapter.Pool in every real composition) transportadmin
// hands to internal/workflow/runtime.Store and internal/humanwork/workitem.Store
// to answer one GetWorkflowInstance call; tenantUUID maps the caller's
// resolved tenant key onto the string form of the uuid those tables key rows
// under (transportadmin parses it back through runtime.ParseUUID: LIB-002/
// LIB-004 does not admit internal/transport as an import root for
// "github.com/google/uuid"), the same mapping [app.CellConfig.TenantUUID]
// threads to caller-driven execution
// (internal/intent/app/pgstore.TenantID in every real composition). c itself
// carries neither: app.Cell has no workflow-runtime or work-item field to
// expose one from (LIB-003 keeps pgx/dbport composition out of internal/intent/app),
// so a composition root that wants GetWorkflowInstance served passes its own
// pool and mapping here instead. Either nil leaves GetWorkflowInstance
// UNAVAILABLE.
func NewGRPCServerWithWorkflowInspector(
	c *app.Cell, workflowExecutor transportadmin.Executor, tenantUUID func(values.TenantId) string,
	opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	if c == nil {
		return nil, fmt.Errorf("transport cell: application cell is required")
	}
	if c.Telemetry != nil {
		opts = append(opts, grpc.ChainUnaryInterceptor(otelmw.UnaryServerInterceptor(c.Telemetry)))
	}
	srv, err := grpcserver.NewServer(grpcserver.Options{
		Config: c.Config, Intent: c.Service, Registry: c.Service, ServerOptions: opts,
	})
	if err != nil {
		return nil, err
	}
	// The operator surface (SVC-011 / ADMIN-001) is hosted by the same
	// server under the same interceptor chain; its distinct trust policy is
	// the operator role check inside internal/operations/admin, not a
	// second listener.
	transportadmin.Register(srv, transportadmin.Dependencies{
		Intent:             c.Service,
		WorkerFacts:        c.Workers,
		TransactionHistory: c.Transactions,
		CapabilityRegistry: c.Capabilities,
		Now:                c.Config.Now,
		WorkflowExecutor:   workflowExecutor,
		TenantUUID:         tenantUUID,
	})
	return srv, nil
}

// NewEdgeHandler builds the HTTP/Connect edge over an already composed
// application cell. It serves the non-RPC discovery and optional workspace
// routes beside the RPC projection under the same admission configuration.
// When c was composed with a Telemetry provider, NewEdgeHandler adds
// connect.WithInterceptors(otelmw.NewConnectInterceptor(provider)) through
// edge's HandlerOptions, mirroring NewGRPCServer exactly.
func NewEdgeHandler(c *app.Cell, opts ...connect.HandlerOption) (http.Handler, error) {
	if c == nil {
		return nil, fmt.Errorf("transport cell: application cell is required")
	}
	if c.Telemetry != nil {
		opts = append(opts, connect.WithInterceptors(otelmw.NewConnectInterceptor(c.Telemetry)))
	}
	rpc, err := edge.NewHandler(edge.Options{
		Config: c.Config, Intent: c.Service, Registry: c.Service, HandlerOptions: opts,
	})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	var routes []workspace.Route
	if c.WorkspaceEnabled() {
		ws, wsErr := workspace.NewHandler(workspace.Options{
			Cell:            c.WorkspacePort(),
			Config:          c.Config,
			Now:             c.Config.Now,
			DevBrowserLogin: c.DevBrowserLogin(),
		})
		if wsErr != nil {
			return nil, fmt.Errorf("transport cell: compose the promotion workspace: %w", wsErr)
		}
		mux.Handle(app.WorkspacePath, ws)
		routes = workspace.Routes()
	}
	discovery, err := newDiscoveryHandler(c.Config, c.Discovery, routes)
	if err != nil {
		return nil, fmt.Errorf("transport cell: render the discovery document: %w", err)
	}
	mux.Handle(app.DiscoveryPath, discovery)
	mux.Handle("/", rpc)
	return mux, nil
}

// discoveryHandler serves the pre-rendered API-001 discovery document to an
// authenticated caller. It is rendered once, at composition, rather than on
// every request: the served shape is fixed by what was composed, not by
// anything a request could influence.
type discoveryHandler struct {
	config   transport.Config
	document []byte
}

// newDiscoveryHandler pre-renders doc, splicing in the workspace's own
// published routes when the workspace is served, so the discovery document
// never advertises a route this build does not actually mount.
func newDiscoveryHandler(cfg transport.Config, doc *manifest.DiscoveryDocument, routes []workspace.Route) (*discoveryHandler, error) {
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if len(routes) > 0 {
		body, err = spliceWorkspaceRoutes(body, routes)
		if err != nil {
			return nil, err
		}
	}
	return &discoveryHandler{config: cfg, document: body}, nil
}

// spliceWorkspaceRoutes adds the workspace's routes to the rendered discovery
// document under [app.WorkspaceRoutesKey], without decoding and re-encoding
// the document manifest.Build already rendered.
func spliceWorkspaceRoutes(document []byte, routes []workspace.Route) ([]byte, error) {
	encoded, err := json.Marshal(routes)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimRight(document, " \t\r\n")
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("transport cell: the discovery document is not a JSON object")
	}
	out := make([]byte, 0, len(trimmed)+len(encoded)+len(app.WorkspaceRoutesKey)+8)
	out = append(out, trimmed[:len(trimmed)-1]...)
	if len(bytes.TrimSpace(trimmed[1:len(trimmed)-1])) > 0 {
		out = append(out, ',')
	}
	out = append(out, '"')
	out = append(out, app.WorkspaceRoutesKey...)
	out = append(out, '"', ':')
	out = append(out, encoded...)
	return append(out, '}'), nil
}

// ServeHTTP implements [http.Handler].
func (h *discoveryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDiscoveryError(w, envelope.New(envelope.CodeInvalidArgument,
			"discovery.method_not_allowed", "the discovery document is read with GET"))
		return
	}
	if _, _, ownedErr := transport.PreAdmit(r.Context(), h.config,
		transport.MapMetadata(r.Header), app.DiscoveryPath); ownedErr != nil {
		writeDiscoveryError(w, ownedErr)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.document)
}

// writeDiscoveryError projects an admission refusal onto the discovery
// route's own plain-JSON error shape.
func writeDiscoveryError(w http.ResponseWriter, ownedErr *envelope.Error) {
	body, err := protojson.Marshal(ownedErr.Detail())
	if err != nil {
		http.Error(w, "{}", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ownedErr.HTTPStatus())
	_, _ = w.Write(body)
}
