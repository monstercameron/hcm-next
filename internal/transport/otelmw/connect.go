package otelmw

import (
	"context"

	"connectrpc.com/connect"

	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
)

// connectInterceptor implements connect.Interceptor, starting one OTel span
// per unary call, named by the call's manifest-format connect procedure
// (connect.Spec.Procedure — identical to the gRPC full method path), and
// ending it with the call's outcome.
type connectInterceptor struct {
	provider *hcmotel.Provider
}

// NewConnectInterceptor returns the connect.Interceptor for the HTTP edge.
//
// It must be composed strictly after edge's own admission interceptor (see
// doc.go's "Ordering requirement"). edge.NewHandler installs its admission
// interceptor unconditionally and forwards edge.Options.HandlerOptions
// after it, so wiring this in through HandlerOptions is what preserves the
// required order:
//
//	edge.NewHandler(edge.Options{
//	    Config:         cfg,
//	    ...
//	    HandlerOptions: []connect.HandlerOption{
//	        connect.WithInterceptors(otelmw.NewConnectInterceptor(provider)),
//	    },
//	})
//
// connect.WithInterceptors' own documented onion (connectrpc.com/connect's
// option.go) makes the first interceptor supplied outermost, so an
// interceptor supplied ahead of edge's own via a second WithInterceptors
// call would still see admission's context — but edge.NewHandler gives no
// seam to do that; HandlerOptions is appended, not prepended.
func NewConnectInterceptor(provider *hcmotel.Provider) connect.Interceptor {
	return &connectInterceptor{provider: provider}
}

// WrapUnary implements connect.Interceptor.
func (i *connectInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		return instrument(ctx, i.provider, req.Spec().Procedure, func(ctx context.Context) (connect.AnyResponse, error) {
			return next(ctx, req)
		})
	}
}

// WrapStreamingClient implements connect.Interceptor. Streaming is out of
// scope for P1A (TOOL-009 owns it; internal/transport/edge/admission.go
// leaves it as a pass-through for the same reason), so this interceptor
// does too.
func (i *connectInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler implements connect.Interceptor. See
// WrapStreamingClient.
func (i *connectInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
