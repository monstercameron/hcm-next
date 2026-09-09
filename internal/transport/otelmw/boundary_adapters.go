package otelmw

import (
	"context"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/boundary"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

// ProviderBoundarySink adapts the policy-owning provider to the generic
// payload-free boundary contract. The transport package does not inspect or
// serialize request, response, SQL, provider or worker payloads.
type ProviderBoundarySink struct {
	Provider   *hcmotel.Provider
	TracerName string
}

func (s ProviderBoundarySink) Start(ctx context.Context, name telemetry.SpanName, attrs map[string]string) (context.Context, boundary.Span) {
	if s.Provider == nil {
		return ctx, noopBoundarySpan{}
	}
	tracer := s.TracerName
	if tracer == "" {
		tracer = tracerName
	}
	child, span := s.Provider.StartExecutionSpan(ctx, tracer, string(name), attrs)
	return child, providerBoundarySpan{span: span}
}

type providerBoundarySpan struct{ span hcmotel.ExecutionSpan }

func (s providerBoundarySpan) End(outcome telemetry.Outcome, err error) {
	s.span.End(string(outcome), err != nil)
}

type noopBoundarySpan struct{}

func (noopBoundarySpan) End(telemetry.Outcome, error) {}

// InstrumentHTTPHandler adds one canonical edge span to a handler whose
// route template is supplied by the router. The template is validated by the
// boundary package and cannot be a raw URL containing query or fragment data.
func InstrumentHTTPHandler(provider *hcmotel.Provider, routeTemplate string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = boundary.Instrument(r.Context(), ProviderBoundarySink{Provider: provider}, boundary.Spec{
			Kind: boundary.KindHTTP, Operation: routeTemplate, RouteTemplate: routeTemplate,
		}, func(ctx context.Context) (struct{}, error) {
			next.ServeHTTP(w, r.WithContext(ctx))
			return struct{}{}, nil
		})
	})
}

// InstrumentBoundary is the shared seam for gRPC, database, worker and
// provider adapters that already own a callback boundary.
func InstrumentBoundary[T any](ctx context.Context, provider *hcmotel.Provider, spec boundary.Spec, fn func(context.Context) (T, error)) (T, error) {
	return boundary.Instrument(ctx, ProviderBoundarySink{Provider: provider}, spec, fn)
}
