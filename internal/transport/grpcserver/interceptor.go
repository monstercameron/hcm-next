package grpcserver

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
)

// UnaryInterceptor is the whole trusted request boundary for native gRPC.
//
// It is exported so a process that composes its own *grpc.Server still gets
// exactly this chain rather than an approximation of it.
func UnaryInterceptor(cfg transport.Config) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		// A server-capped deadline applies to every request, including one
		// that arrived without any deadline at all. Cancellation propagates
		// through the same context, so a caller that goes away stops the work.
		ctx, cancel := transport.CapDeadline(ctx, cfg)
		defer cancel()

		md, _ := metadata.FromIncomingContext(ctx)
		message, _ := req.(proto.Message)

		admitted, inv, admitErr := transport.Admit(ctx, cfg, transport.AdmissionRequest{
			Metadata: transport.MapMetadata(md),
			Method:   info.FullMethod,
			Kind:     transport.KindGRPC,
			Message:  message,
		})
		if admitErr != nil {
			return nil, finish(cfg, info.FullMethod, nil, admitErr.CorrelationID(), start, admitErr)
		}

		resp, err := handler(admitted, req)
		if err != nil {
			return nil, finish(cfg, info.FullMethod, inv, inv.RequestID(), start,
				transport.OwnedError(err, inv))
		}
		if ctxErr := admitted.Err(); ctxErr != nil {
			return nil, finish(cfg, info.FullMethod, inv, inv.RequestID(), start,
				transport.OwnedError(ctxErr, inv))
		}
		finish(cfg, info.FullMethod, inv, inv.RequestID(), start, nil)
		return resp, nil
	}
}

// finish emits the structured record for a completed request and returns the
// owned error to hand back to grpc-go. *envelope.Error implements grpc-go's
// status interface, so returning it directly produces the projected status
// code plus the canonical hcmnext.common.v1.ErrorDetail.
func finish(cfg transport.Config, method string, inv *transport.Invocation, requestID string, start time.Time, err *envelope.Error) error {
	if cfg.Logger != nil {
		cfg.Logger.LogRequest(transport.NewLogRecord(method, transport.KindGRPC, inv, requestID, time.Since(start), err))
	}
	if err == nil {
		return nil
	}
	return err
}
