package otelmw_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/transport/edge"
	"github.com/monstercameron/hcm-next/internal/transport/grpcserver"
	"github.com/monstercameron/hcm-next/internal/transport/otelmw"
	"github.com/monstercameron/hcm-next/internal/transport/transporttest"
)

// fixedRequestID is the correlation identifier every fixture request is told
// to use, so that two equivalent requests differ in nothing observable —
// mirrors internal/transport/edge and internal/transport/clients' own
// fixture constant of the same name and purpose.
const fixedRequestID = "req-otelmw-fixture-0001"

// baseTime pins the fixture clock.
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// testEvaluator returns the frozen, unmodified OBS-004 policy evaluator —
// the same one production wires — over the compiled-in default allow-list,
// export policy and sampling policy. This test never constructs its own
// looser or stricter policy: the whole point is proving this package's
// interceptors are governed by the real one.
func testEvaluator(t testing.TB) *telemetry.Evaluator {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist: %v", err)
	}
	return telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
}

// testResource returns a valid P1A resource for the Provider under test.
func testResource(t testing.TB) telemetry.Resource {
	t.Helper()
	res := telemetry.NewResourceFromBuild(
		buildinfo.Info{Revision: "otelmw-test"},
		"hcm-otelmw-test", "instance-1", "test", "cell-p1a", "us-east-1",
		telemetry.ProcessRoleAPI, telemetry.TenantClassStandard,
	)
	if err := res.Validate(); err != nil {
		t.Fatalf("test resource does not validate: %v", err)
	}
	return res
}

// newTestProvider builds a Provider wired to an in-memory span exporter and
// a manual metric reader, so a test can inspect exactly what a real
// exporter would have received without a network or a background flush.
func newTestProvider(t testing.TB) (*hcmotel.Provider, *tracetest.InMemoryExporter) {
	t.Helper()
	spanExporter := tracetest.NewInMemoryExporter()
	provider, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
		Resource:        testResource(t),
		Evaluator:       testEvaluator(t),
		ShutdownTimeout: 5 * time.Second,
		Trace:           hcmotel.TraceConfig{Exporter: spanExporter},
		Metric:          hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider, spanExporter
}

// fakeClock is a fixed clock for the fixture's credential window.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

// harness stands both transports up in-process, each wired with the same
// admission chain plus this package's own interceptor composed after it —
// exactly the ordering doc.go requires — over one shared Provider.
type harness struct {
	t testing.TB

	provider     *hcmotel.Provider
	spanExporter *tracetest.InMemoryExporter

	token string

	intent   *transporttest.IntentHandler
	registry *transporttest.RegistryHandler

	grpcIntent   intentsv1.IntentServiceClient
	grpcRegistry registryv1.RegistryServiceClient

	edgeURL      string
	httpClient   *http.Client
	edgeIntent   *edge.IntentClient
	edgeRegistry *edge.RegistryClient
}

// newHarness builds the shared fixture. Every request goes through the real
// grpcserver / edge admission chain first, so this package's interceptor is
// always exercised in its documented position, never standalone.
func newHarness(t testing.TB) *harness {
	t.Helper()

	provider, spanExporter := newTestProvider(t)
	h := &harness{
		t:            t,
		provider:     provider,
		spanExporter: spanExporter,
		intent:       &transporttest.IntentHandler{},
		registry:     &transporttest.RegistryHandler{},
	}

	clock := &fakeClock{now: baseTime}
	verifier, err := transporttest.NewVerifier(clock.Now)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(baseTime))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	h.token = token

	cfg := transporttest.Config(verifier, clock.Now, fixedRequestID, nil)

	grpcServer, err := grpcserver.NewServer(grpcserver.Options{
		Config:   cfg,
		Intent:   h.intent,
		Registry: h.registry,
		ServerOptions: []grpc.ServerOption{
			grpc.ChainUnaryInterceptor(otelmw.UnaryServerInterceptor(h.provider)),
		},
	})
	if err != nil {
		t.Fatalf("grpcserver.NewServer: %v", err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	h.grpcIntent = intentsv1.NewIntentServiceClient(conn)
	h.grpcRegistry = registryv1.NewRegistryServiceClient(conn)

	edgeHandler, err := edge.NewHandler(edge.Options{
		Config:   cfg,
		Intent:   h.intent,
		Registry: h.registry,
		HandlerOptions: []connect.HandlerOption{
			connect.WithInterceptors(otelmw.NewConnectInterceptor(h.provider)),
		},
	})
	if err != nil {
		t.Fatalf("edge.NewHandler: %v", err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)
	h.edgeURL = httpServer.URL
	h.httpClient = httpServer.Client()
	h.edgeIntent = edge.NewIntentClient(h.httpClient, h.edgeURL)
	h.edgeRegistry = edge.NewRegistryClient(h.httpClient, h.edgeURL)

	return h
}

// grpcContext attaches the fixture credential and request identifier to a
// gRPC call, plus any extra caller-supplied metadata pairs.
func (h *harness) grpcContext(ctx context.Context, extra ...string) context.Context {
	pairs := append([]string{
		transport.AuthorizationMetadataKey, h.token,
		transport.RequestIDMetadataKey, fixedRequestID,
	}, extra...)
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// edgeRequest wraps msg with the fixture credential and request identifier,
// plus any extra caller-supplied headers.
func edgeRequest[T any](h *harness, msg *T, extra ...string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(transport.AuthorizationMetadataKey, h.token)
	req.Header().Set(transport.RequestIDMetadataKey, fixedRequestID)
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header().Set(extra[i], extra[i+1])
	}
	return req
}
