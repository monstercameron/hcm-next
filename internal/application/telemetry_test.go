package application

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
)

// TestNewTelemetryProviderOwnsTheAPIResource proves the composition root
// builds the policy-enforced provider both transports receive. It inspects
// resource identity rather than exporter output: the transport qualification
// lives in otelmw, while this is the process-level ownership and bounded
// lifetime wiring.
func TestNewTelemetryProviderOwnsTheAPIResource(t *testing.T) {
	provider, err := NewTelemetryProvider(context.Background(), "instance-test-1",
		ServeConfig{CellID: "cell-test-1", OTelExporter: OTelExporterStdout})
	if err != nil {
		t.Fatalf("NewTelemetryProvider: %v", err)
	}
	if provider == nil {
		t.Fatal("NewTelemetryProvider(stdout) returned no provider")
	}
	attributes := map[string]string{}
	for _, attribute := range provider.Resource().Attributes() {
		attributes[string(attribute.Key)] = attribute.Value.AsString()
	}
	for key, want := range map[string]string{
		"service.name":        "hcmnext",
		"service.instance.id": "instance-test-1",
		"cell.id":             "cell-test-1",
		"process.role":        "api",
	} {
		if got := attributes[key]; got != want {
			t.Errorf("resource %s = %q, want %q", key, got, want)
		}
	}
	if report := provider.Shutdown(context.Background()); report.Err() != nil || report.DeadlineExceeded {
		t.Fatalf("provider shutdown = %+v, want a clean bounded shutdown", report)
	}
}

// TestNewTelemetryProviderIsOffByDefault is the other half of
// CellConfig.Telemetry's "nil = off" contract: -otel-exporter=none (the
// serve role's own default) builds no provider at all, so a composed cell
// adds no otelmw interceptor on either transport rather than silently
// defaulting to some exporter nobody asked for.
func TestNewTelemetryProviderIsOffByDefault(t *testing.T) {
	provider, err := NewTelemetryProvider(context.Background(), "instance-test-2",
		ServeConfig{CellID: "cell-test-2", OTelExporter: OTelExporterNone})
	if err != nil {
		t.Fatalf("NewTelemetryProvider(none): %v", err)
	}
	if provider != nil {
		t.Fatalf("NewTelemetryProvider(none) = %v, want nil", provider)
	}
}

// TestNewTelemetryProviderRefusesAnUnvalidatedExporter guards the path a
// future caller could take around ServeConfig.Validate.
func TestNewTelemetryProviderRefusesAnUnvalidatedExporter(t *testing.T) {
	_, err := NewTelemetryProvider(context.Background(), "instance-test-3",
		ServeConfig{OTelExporter: "jaeger"})
	if err == nil {
		t.Fatal("NewTelemetryProvider accepted an exporter the configuration would reject")
	}
	if !strings.Contains(err.Error(), FieldOTelExporter) {
		t.Errorf("error = %q, want it to name -%s", err, FieldOTelExporter)
	}
}

// TestComposedTelemetryReachesTheCell proves the provider the root builds is
// the one the cell carries, and that a composition failing after the provider
// was built does not leave it running.
func TestComposedTelemetryReachesTheCell(t *testing.T) {
	provider, err := NewTelemetryProvider(context.Background(), "instance-test-4",
		ServeConfig{CellID: "cell-test-4", OTelExporter: OTelExporterStdout})
	if err != nil {
		t.Fatalf("NewTelemetryProvider: %v", err)
	}
	composed, _, _ := composeStub(t, stubServeConfig(), WithTelemetryProvider(provider))
	if composed.Cell().Telemetry != provider {
		t.Error("the composed cell carries a different telemetry provider than the root built")
	}
	entry, ok := composed.Graph().Component(ComponentTelemetryProvider)
	if !ok || entry.Impl == "<nil>" {
		t.Errorf("the graph records the telemetry provider as %+v, want the composed one", entry)
	}

	// A composition that fails after the provider was built owns cleaning it
	// up: nothing downstream has taken responsibility for it yet, and a
	// half-composed process must not leave an exporter running.
	abandoned, err := NewTelemetryProvider(context.Background(), "instance-test-5",
		ServeConfig{CellID: "cell-test-5", OTelExporter: OTelExporterStdout})
	if err != nil {
		t.Fatalf("NewTelemetryProvider: %v", err)
	}
	sentinel := errors.New("listener refused")
	_, err = ComposeServe(context.Background(), ServeInput{
		Config: stubServeConfig(),
		Options: Options{}.Apply(WithStore(&stubStore{}), WithVerifier(stubVerifier{}),
			WithTelemetryProvider(abandoned),
			WithListener(func(string, string) (net.Listener, error) { return nil, sentinel })),
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ComposeServe = %v, want the listener failure %v", err, sentinel)
	}
	// Shutting it down again is what proves the failed composition already
	// did: a provider that was never flushed would shut down cleanly here.
	if report := abandoned.Shutdown(context.Background()); report.Err() == nil {
		t.Error("the abandoned provider was still running after the composition failed")
	}
}

// TestLogTelemetryShutdownReportsOnlyDegradation pins the rule that telemetry
// is observational: a clean flush is silent, and a failed one is reported
// without changing anybody's business outcome.
func TestLogTelemetryShutdownReportsOnlyDegradation(t *testing.T) {
	logger := &recordingLogger{}
	LogTelemetryShutdown(logger, hcmotel.ShutdownReport{})
	if logger.saw("hcmnext.telemetry_shutdown_degraded") {
		t.Error("a clean telemetry shutdown was reported as degradation")
	}
	LogTelemetryShutdown(logger, hcmotel.ShutdownReport{DeadlineExceeded: true})
	if !logger.saw("hcmnext.telemetry_shutdown_degraded") {
		t.Error("an exceeded telemetry deadline was not reported")
	}
	// A composition with no logger still must not panic on the shutdown path.
	LogTelemetryShutdown(nil, hcmotel.ShutdownReport{DeadlineExceeded: true})
}
