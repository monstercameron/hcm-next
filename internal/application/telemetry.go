package application

// NewTelemetryProvider is the provider half of the composition root. It moved
// here from cmd/hcmnext so that the process's observability plane is composed
// where every other adapter is composed, and so that the same construction a
// deployed binary uses is the one a test can assert on without rebuilding it.

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

// NewTelemetryProvider constructs the bounded, policy-enforced provider for
// an API process, or returns (nil, nil) when cfg.OTelExporter is
// OTelExporterNone: app.CellConfig.Telemetry treats nil as off, and a
// listener started with no -otel-exporter flag should publish no spans or
// metrics at all rather than defaulting to some exporter nobody asked for.
//
// The resource deliberately contains only service and deployment identity;
// request-specific identifiers are admitted and filtered later by otelmw and
// the telemetry evaluator.
func NewTelemetryProvider(ctx context.Context, instanceID string, cfg ServeConfig) (*hcmotel.Provider, error) {
	if cfg.OTelExporter == OTelExporterNone {
		return nil, nil
	}

	var trace hcmotel.TraceConfig
	var metric hcmotel.MetricConfig
	switch cfg.OTelExporter {
	case OTelExporterStdout:
		trace = hcmotel.TraceConfig{Kind: hcmotel.ExporterKindStdout}
		metric = hcmotel.MetricConfig{Kind: hcmotel.ExporterKindStdout}
	case OTelExporterOTLPHTTP:
		trace = hcmotel.TraceConfig{Kind: hcmotel.ExporterKindOTLP, Endpoint: cfg.OTelEndpoint}
		metric = hcmotel.MetricConfig{Kind: hcmotel.ExporterKindOTLP, Endpoint: cfg.OTelEndpoint}
	default:
		// ServeConfig.Validate already rejects any other value before the
		// composition runs; this default only guards a future caller of this
		// function that skipped that gate.
		return nil, fmt.Errorf("application: unknown -%s %q", FieldOTelExporter, cfg.OTelExporter)
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
			buildinfo.Current(), "hcmnext", instanceID, "development", cfg.CellID, "",
			telemetry.ProcessRoleAPI, telemetry.TenantClassStandard,
		),
		Evaluator:       evaluator,
		ShutdownTimeout: TelemetryShutdownGrace,
		Trace:           trace,
		Metric:          metric,
	})
}

// LogTelemetryShutdown records exporter degradation independently. Telemetry
// is observational: a failed flush must never rewrite the API's result or
// turn an otherwise clean process shutdown into a business failure.
func LogTelemetryShutdown(logger bootstrap.Logger, report hcmotel.ShutdownReport) {
	if logger == nil {
		return
	}
	if report.Err() == nil && !report.DeadlineExceeded {
		return
	}
	logger.Error("hcmnext.telemetry_shutdown_degraded",
		"error", report.Err(),
		"deadline_exceeded", report.DeadlineExceeded)
}
