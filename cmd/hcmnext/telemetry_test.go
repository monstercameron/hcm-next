package main

import (
	"context"
	"testing"
)

// TestTodo_OBS_002_ServeTelemetryProviderOwnsAPIResource proves the API
// composition root builds the policy-enforced provider that the gRPC and HTTP
// interceptors receive. It deliberately inspects resource identity rather
// than exporter output: the transport qualification lives in otelmw, while
// this test guards the process-level ownership and bounded lifetime wiring.
func TestTodo_OBS_002_ServeTelemetryProviderOwnsAPIResource(t *testing.T) {
	provider, err := newServeTelemetryProvider(context.Background(), "instance-test-1", "cell-test-1", otelExporterStdout, "")
	if err != nil {
		t.Fatalf("newServeTelemetryProvider: %v", err)
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
		t.Fatalf("provider shutdown = %+v, want clean bounded shutdown", report)
	}
}

// TestTodo_OBS_002_ServeTelemetryProviderOffByDefault proves the other half
// of CellConfig.Telemetry's "nil = off" contract: -otel-exporter=none (serve's
// own default) builds no provider at all, so a cell composed from it adds no
// otelmw interceptor on either transport rather than silently defaulting to
// some exporter nobody asked for.
func TestTodo_OBS_002_ServeTelemetryProviderOffByDefault(t *testing.T) {
	provider, err := newServeTelemetryProvider(context.Background(), "instance-test-2", "cell-test-2", otelExporterNone, "")
	if err != nil {
		t.Fatalf("newServeTelemetryProvider(none): %v", err)
	}
	if provider != nil {
		t.Fatalf("newServeTelemetryProvider(none) = %v, want nil", provider)
	}
}
