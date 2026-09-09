package otel_test

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

func testAllowlist(t *testing.T) *telemetry.Allowlist {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist: %v", err)
	}
	return allow
}

func testEvaluator(t *testing.T) *telemetry.Evaluator {
	t.Helper()
	allow := testAllowlist(t)
	return telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
}

func testResource(t *testing.T) telemetry.Resource {
	t.Helper()
	res := telemetry.NewResourceFromBuild(
		buildinfo.Info{Revision: "abc123"},
		"hcm-otel-test", "instance-1", "test", "cell-p1a", "us-east-1",
		telemetry.ProcessRoleAPI, telemetry.TenantClassStandard,
	)
	if err := res.Validate(); err != nil {
		t.Fatalf("test resource does not validate: %v", err)
	}
	return res
}

// testHarness bundles one Provider with the in-memory trace exporter and
// manual metric reader it was built with, so a test can both drive the
// Provider through its public API and inspect exactly what an exporter
// would have received.
type testHarness struct {
	Provider     *hcmotel.Provider
	SpanExporter *tracetest.InMemoryExporter
	MetricReader *sdkmetric.ManualReader
}

func newTestHarness(t *testing.T, eval *telemetry.Evaluator) *testHarness {
	t.Helper()
	spanExporter := tracetest.NewInMemoryExporter()
	metricReader := sdkmetric.NewManualReader()

	p, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
		Resource:        testResource(t),
		Evaluator:       eval,
		ShutdownTimeout: 5 * time.Second,
		Trace: hcmotel.TraceConfig{
			Exporter: spanExporter,
		},
		Metric: hcmotel.MetricConfig{
			Reader: metricReader,
		},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return &testHarness{Provider: p, SpanExporter: spanExporter, MetricReader: metricReader}
}
