// Package otelmw is the transport-level composition root
// internal/platform/telemetry/otel/doc.go's "Out of scope" section names:
// the gRPC unary interceptor and connect/HTTP middleware that turn one
// completed request, on either transport, into one redacted OTel span.
//
// It is the only place in the module allowed to import both
// internal/platform/telemetry/otel and the transport layer
// (internal/transport, internal/transport/grpcserver,
// internal/transport/edge) — doc.go's own words. Nothing here decides an
// HCM business result; it only observes a call transport.Admit already
// admitted.
//
// # Ordering requirement
//
// Every exported interceptor in this package must run strictly after
// transport's own admission interceptor (grpcserver.UnaryInterceptor /
// edge's internal admissionInterceptor) in the chain, never before and
// never standalone. Admission is what populates ctx with the immutable
// *transport.Invocation and the verified *trust.Principal; this package
// reads those two values out of ctx and never re-derives them from raw
// metadata or headers itself, and never trusts a caller-supplied principal
// — only the reference the trust layer already placed in context. Wiring
// this package ahead of admission, or wiring it as a replacement for
// admission, would mean either no trusted context to read yet, or an
// instrumented call that was never actually screened.
//
// For native gRPC:
//
//	grpc.ChainUnaryInterceptor(
//	    grpcserver.UnaryInterceptor(cfg),        // admission first
//	    otelmw.UnaryServerInterceptor(provider), // instrumentation second
//	)
//
// For the connect/HTTP edge, the same ordering rule applies via
// connect.WithInterceptors' documented onion (the first interceptor is
// outermost — connectrpc.com/connect's option.go):
//
//	connect.WithInterceptors(
//	    <edge's own admission interceptor, installed by edge.NewHandler>,
//	    otelmw.NewConnectInterceptor(provider),
//	)
//
// edge.NewHandler installs its admission interceptor unconditionally and
// exposes no seam to insert another interceptor ahead of it, so this
// package's connect.Interceptor is composed in through
// edge.Options.HandlerOptions, which the package doc guarantees is
// "appended after the options this package sets" — i.e. after admission,
// exactly the required order.
//
// # Attributes and metrics
//
// This package attaches no span attribute directly beyond a bounded,
// allow-listed "outcome" and "error_type" pair (both registered in
// definitions/telemetry/resource-contract.yaml's attribute_allowlist for
// signal span) describing the completed call's own outcome. Every other
// attribute a span carries — correlation_id, and the redacted attempts at
// request_id/evidence_ref/principal_ref — comes from
// internal/platform/telemetry/otel's own automatic context-to-span-attribute
// propagation (trace.go's contextAttributes), which reads exactly the
// internal/platform/logging context values this package sets. The frozen
// telemetry.Evaluator, not this package, decides what survives; see
// internal/platform/telemetry/otel/doc.go's "Policy enforcement" section.
//
// The P1A metric catalog's edge.parity instrument
// (telemetry.MetricCatalog(), internal/platform/telemetry/otel/metrics.go)
// records whether two transports agree on one comparison outcome; it is not
// a per-request transport metric a single interceptor invocation can
// produce on its own (a lone call, on one transport, has no second
// transport's outcome to compare against). This package therefore records
// no metric: recording an edge.parity value of 1 (or, worse, a fabricated
// comparison) from inside a single-transport interceptor would misrepresent
// what edge.parity means rather than instrument it. Nothing else in
// telemetry.MetricCatalog() names a transport-interceptor-shaped metric.
package otelmw
