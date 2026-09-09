package otelmw

import (
	"context"

	"google.golang.org/grpc"

	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

// StreamServerInterceptor returns a grpc.StreamServerInterceptor that starts
// one OTel span per streaming call, named by the call's manifest-format gRPC
// procedure (info.FullMethod), and ends it with the stream's outcome. It is
// [UnaryServerInterceptor] for the other cardinality: one span per call, the
// same name, the same outcome attributes, the same context derivation.
//
// It must be chained strictly after grpcserver.StreamInterceptor (see
// doc.go's "Ordering requirement"), exactly as the unary one is chained
// after grpcserver.UnaryInterceptor:
//
//	grpc.ChainStreamInterceptor(
//	    grpcserver.StreamInterceptor(cfg),
//	    otelmw.StreamServerInterceptor(provider),
//	)
//
// The span covers the whole stream, so its duration is the stream's lifetime
// rather than one round trip's, and the handler receives a stream whose
// Context carries the span (the way it already carries admission's
// invocation and principal) so anything it logs inside is correlated.
//
// A stream whose handler returned nil after its context was cancelled - a
// client that went away, a deadline that expired - is recorded as that
// outcome, not as a success: the handler returning cleanly is how a
// server-streaming method ends when its client disconnects (nothing is left
// to send), and internal/transport/grpcserver's own stream boundary makes
// the same judgement for the record it emits. What is returned to grpc-go
// is still the handler's own result.
func StreamServerInterceptor(provider *hcmotel.Provider) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		var handlerErr error
		_, _ = instrument(ss.Context(), provider, info.FullMethod, func(ctx context.Context) (struct{}, error) {
			handlerErr = handler(srv, &instrumentedStream{ServerStream: ss, ctx: ctx})
			outcome := handlerErr
			if outcome == nil {
				outcome = ctx.Err()
			}
			return struct{}{}, outcome
		})
		return handlerErr
	}
}

// instrumentedStream is the stream the handler receives: the transport's own
// stream in every respect except its context, which carries the span. Every
// other method forwards unchanged, so this wrapper cannot become a place
// where streaming behaviour diverges from the transport's.
type instrumentedStream struct {
	grpc.ServerStream

	ctx context.Context
}

// Context returns the instrumented context: the admitted one, plus the span.
func (s *instrumentedStream) Context() context.Context { return s.ctx }
