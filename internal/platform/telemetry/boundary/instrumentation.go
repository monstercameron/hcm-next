package boundary

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// Kind identifies a physical boundary. Payloads are intentionally absent
// from this contract; boundary telemetry describes mechanics, not business
// data.
type Kind string

const (
	KindHTTP     Kind = "http"
	KindGRPC     Kind = "grpc"
	KindDatabase Kind = "database"
	KindWorker   Kind = "worker"
	KindProvider Kind = "provider"
)

var (
	ErrBoundaryOperation = errors.New("telemetry boundary: operation must be a bounded registry name")
	ErrBoundaryPayload   = errors.New("telemetry boundary: payload-shaped value is prohibited")
)

// Span is the small sink seam used by HTTP, gRPC, pgx, worker and provider
// adapters. Implementations may bridge it to OpenTelemetry or a deterministic
// test harness without changing the business callback.
type Span interface {
	End(outcome telemetry.Outcome, err error)
}

// Sink starts a policy-filtered span and returns the context to pass to the
// actual boundary operation.
type Sink interface {
	Start(context.Context, telemetry.SpanName, map[string]string) (context.Context, Span)
}

// Spec is the approved, payload-free attribute set for a boundary call.
// RouteTemplate and Operation must be stable names, never a raw URL, SQL
// statement, header, request body, provider response or worker payload.
type Spec struct {
	Kind          Kind
	Operation     string
	RouteTemplate string
	Dependency    string
	Status        string
	SizeClass     string
	Retry         int
}

func (s Spec) spanName() telemetry.SpanName {
	switch s.Kind {
	case KindHTTP:
		return telemetry.SpanHTTPServer
	case KindGRPC:
		return telemetry.SpanGRPCServer
	case KindDatabase:
		return telemetry.SpanDBOperation
	case KindWorker:
		return telemetry.SpanJobPartition
	case KindProvider:
		return telemetry.SpanProviderCall
	default:
		return ""
	}
}

func (s Spec) validate() error {
	if s.spanName() == "" || strings.TrimSpace(s.Operation) == "" || s.Retry < 0 {
		return ErrBoundaryOperation
	}
	for _, value := range []string{s.Operation, s.RouteTemplate, s.Dependency, s.Status, s.SizeClass} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return ErrBoundaryPayload
		}
	}
	if strings.ContainsAny(s.RouteTemplate, "?#") || strings.Contains(s.RouteTemplate, "://") {
		return ErrBoundaryPayload
	}
	if strings.ContainsAny(s.Operation, " ?&=,") || strings.ContainsAny(s.Dependency, " ?&=,") {
		return ErrBoundaryPayload
	}
	return nil
}

func (s Spec) attributes() map[string]string {
	attrs := map[string]string{"operation": s.Operation}
	if s.RouteTemplate != "" {
		attrs["route"] = s.RouteTemplate
	}
	if s.Dependency != "" {
		attrs["dependency"] = s.Dependency
	}
	if s.Status != "" {
		attrs["status"] = s.Status
	}
	if s.SizeClass != "" {
		attrs["size_class"] = s.SizeClass
	}
	if s.Retry > 0 {
		attrs["retry"] = "true"
	}
	return attrs
}

// Instrument executes fn with a child boundary context and ends exactly one
// span. It returns the callback's result and error unchanged.
func Instrument[T any](ctx context.Context, sink Sink, spec Spec, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := spec.validate(); err != nil {
		return zero, err
	}
	if sink == nil {
		return fn(ctx)
	}
	child, span := sink.Start(ctx, spec.spanName(), spec.attributes())
	result, err := fn(child)
	if err != nil {
		span.End(telemetry.OutcomeFailure, err)
	} else {
		span.End(telemetry.OutcomeSuccess, nil)
	}
	return result, err
}
