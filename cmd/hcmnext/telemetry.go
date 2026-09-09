// telemetry.go is this command's view of the serve role's telemetry
// exporter selection.
//
// The provider itself is composed by internal/application (ARCH-GO-020): this
// file only names the three allowed -otel-exporter values the command's help
// text and its existing OBS-002 tests refer to, and forwards to the
// application root's constructor so there is exactly one implementation of
// what "the API process's telemetry provider" is.

package main

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

// otelExporterNone, otelExporterStdout and otelExporterOTLPHTTP are the
// allowed values of -otel-exporter. otelExporterNone is the default: a
// listener started with no telemetry flag at all publishes no spans or
// metrics, rather than exporting to stdout by surprise.
const (
	otelExporterNone     = application.OTelExporterNone
	otelExporterStdout   = application.OTelExporterStdout
	otelExporterOTLPHTTP = application.OTelExporterOTLPHTTP
)

// newServeTelemetryProvider forwards to the application root's provider
// construction. It exists so this command's documented exporter selection and
// the composed provider cannot describe two different things.
func newServeTelemetryProvider(ctx context.Context, instanceID, cellID, exporter, endpoint string) (*hcmotel.Provider, error) {
	return application.NewTelemetryProvider(ctx, instanceID, application.ServeConfig{
		CellID:       cellID,
		OTelExporter: exporter,
		OTelEndpoint: endpoint,
	})
}
