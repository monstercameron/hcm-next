package main

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

const workerTelemetryShutdownGrace = 5 * time.Second

func workerTelemetryFields() []bootstrap.Field {
	return []bootstrap.Field{
		{Name: "otel-exporter", Env: "HCMNEXT_WORKER_OTEL_EXPORTER", Usage: "OTel exporter: none, stdout, or otlphttp", Default: "none", Kind: bootstrap.KindString},
		{Name: "otel-endpoint", Env: "HCMNEXT_WORKER_OTEL_ENDPOINT", Usage: "OTLP/HTTP collector endpoint; required for otlphttp", Kind: bootstrap.KindString},
		{Name: "cell-id", Env: "HCMNEXT_CELL_ID", Usage: "cell identifier for the worker telemetry resource", Default: "cell-local", Kind: bootstrap.KindString},
		{Name: "deployment-environment", Env: "HCMNEXT_DEPLOYMENT_ENVIRONMENT", Usage: "deployment environment for the worker telemetry resource", Default: "development", Kind: bootstrap.KindString},
	}
}

func newWorkerTelemetryProvider(ctx context.Context, instanceID string, values *bootstrap.Values) (*hcmotel.Provider, error) {
	exporter := values.String("otel-exporter")
	if exporter == "none" {
		return nil, nil
	}
	var trace hcmotel.TraceConfig
	var metric hcmotel.MetricConfig
	switch exporter {
	case "stdout":
		trace.Kind, metric.Kind = hcmotel.ExporterKindStdout, hcmotel.ExporterKindStdout
	case "otlphttp":
		endpoint := values.String("otel-endpoint")
		if endpoint == "" {
			return nil, fmt.Errorf("worker: -otel-endpoint is required when -otel-exporter=otlphttp")
		}
		trace = hcmotel.TraceConfig{Kind: hcmotel.ExporterKindOTLP, Endpoint: endpoint}
		metric = hcmotel.MetricConfig{Kind: hcmotel.ExporterKindOTLP, Endpoint: endpoint}
	default:
		return nil, fmt.Errorf("worker: unknown -otel-exporter %q", exporter)
	}
	allowlist, err := telemetry.DefaultAllowlist()
	if err != nil {
		return nil, fmt.Errorf("worker: compile telemetry allowlist: %w", err)
	}
	return hcmotel.NewProvider(ctx, hcmotel.Config{
		Resource:        telemetry.NewResourceFromBuild(buildinfo.Current(), "hcmnext-worker", instanceID, values.String("deployment-environment"), values.String("cell-id"), "", telemetry.ProcessRoleWorker, telemetry.TenantClassStandard),
		Evaluator:       telemetry.NewEvaluator(allowlist, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy()),
		ShutdownTimeout: workerTelemetryShutdownGrace,
		Trace:           trace,
		Metric:          metric,
	})
}
