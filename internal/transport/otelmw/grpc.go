package otelmw

import (
	"context"

	"google.golang.org/grpc"

	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
)

// UnaryServerInterceptor returns a grpc.UnaryServerInterceptor that starts
// one OTel span per unary call, named by the call's manifest-format gRPC
// procedure (info.FullMethod), and ends it with the call's outcome.
//
// It must be chained strictly after grpcserver.UnaryInterceptor (see
// doc.go's "Ordering requirement"):
//
//	grpc.ChainUnaryInterceptor(
//	    grpcserver.UnaryInterceptor(cfg),
//	    otelmw.UnaryServerInterceptor(provider),
//	)
//
// grpc.ChainUnaryInterceptor executes its interceptors in the order given —
// the first is outermost — so admission's own *transport.Invocation and
// verified *trust.Principal are already in ctx by the time this
// interceptor's handler receives it.
func UnaryServerInterceptor(provider *hcmotel.Provider) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return instrument(ctx, provider, info.FullMethod, func(ctx context.Context) (any, error) {
			return handler(ctx, req)
		})
	}
}
