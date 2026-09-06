package journeyclient

import (
	"context"

	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Service is the page's whole view of the cell: the eight operations
// hcmnext.journey.v1.JourneyService publishes, with the call options and the
// credential already dealt with.
//
// It exists as an interface for one reason -- so [App] can be driven by a
// fake in an ordinary `go test` with no server, no tunnel and no browser.
// The production implementation ([NewGRPCService]) is thin enough to read in
// one screen precisely because everything worth testing is on the other side
// of this seam.
type Service interface {
	ListJourneys(ctx context.Context, in *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error)
	ProposeJourney(ctx context.Context, in *journeyv1.ProposeJourneyRequest) (*journeyv1.ProposeJourneyResponse, error)
	InspectJourney(ctx context.Context, in *journeyv1.InspectJourneyRequest) (*journeyv1.InspectJourneyResponse, error)
	ExecuteJourney(ctx context.Context, in *journeyv1.ExecuteJourneyRequest) (*journeyv1.ExecuteJourneyResponse, error)
	DecideJourney(ctx context.Context, in *journeyv1.DecideJourneyRequest) (*journeyv1.DecideJourneyResponse, error)
	// WatchJourney opens the server-streaming change feed. The stream is
	// returned rather than a channel so cancellation stays where it belongs:
	// the caller's context ends the stream, and Recv reports that as an
	// error the caller can distinguish from a refusal.
	WatchJourney(ctx context.Context, in *journeyv1.WatchJourneyRequest) (WatchStream, error)
	// ListWorkers reads the population a journey can be proposed for -- the
	// release's fixed corpus plus this tenant's created employees -- and, in
	// the same answer, the closed set of placements a new employee may be
	// given. The two travel together because a client holding one and not
	// the other can draw a picker but not a form.
	ListWorkers(ctx context.Context, in *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error)
	// CreateWorker records one new employee as a durable, append-only fact.
	// It is the one write on this page that is not about a journey, and it
	// runs behind the same P1B execution authority the execution and
	// decision writes do.
	CreateWorker(ctx context.Context, in *journeyv1.CreateWorkerRequest) (*journeyv1.CreateWorkerResponse, error)
}

// WatchStream is the receiving half of one WatchJourney call: the generated
// grpc.ServerStreamingClient narrowed to the one method this client uses, so
// a fake stream in a test is four lines rather than an embedded generated
// type.
type WatchStream interface {
	Recv() (*journeyv1.WatchJourneyResponse, error)
}

// AuthorizationHeader is the metadata key every RPC carries the page's
// credential in.
//
// This is not belt-and-braces over an already-authenticated socket: the
// tunnel forwards only diagnostic headers (x-request-id, x-correlation-id,
// traceparent) from the WebSocket upgrade into gRPC metadata, never
// Authorization. A call that omits this is admitted as an anonymous call and
// refused UNAUTHENTICATED, no matter how the socket was opened.
const AuthorizationHeader = "authorization"

// BearerScheme is the credential's scheme, as
// internal/humanwork/workspace normalizes it away before writing the island.
const BearerScheme = "Bearer "

// grpcService is [Service] over one client connection.
type grpcService struct {
	client journeyv1.JourneyServiceClient
	bearer string
}

// NewGRPCService returns the production [Service]: the generated client over
// conn, with "authorization: Bearer <bearer>" attached to every call.
//
// The credential is attached per RPC rather than by a connection-level
// PerRPCCredentials for one reason worth stating: PerRPCCredentials refuse to
// send over a connection they consider insecure, and this connection is
// insecure by construction -- transport security is the browser's wss://
// socket, which gRPC inside a WASM page cannot see. Appending the metadata
// directly is the same wire result without asking gRPC to certify a
// transport it is not looking at.
func NewGRPCService(conn grpc.ClientConnInterface, bearer string) Service {
	return &grpcService{client: journeyv1.NewJourneyServiceClient(conn), bearer: bearer}
}

// authorize returns ctx carrying the page's credential.
func (s *grpcService) authorize(ctx context.Context) context.Context {
	if s.bearer == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, AuthorizationHeader, BearerScheme+s.bearer)
}

func (s *grpcService) ListJourneys(ctx context.Context, in *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
	return s.client.ListJourneys(s.authorize(ctx), in)
}

func (s *grpcService) ProposeJourney(ctx context.Context, in *journeyv1.ProposeJourneyRequest) (*journeyv1.ProposeJourneyResponse, error) {
	return s.client.ProposeJourney(s.authorize(ctx), in)
}

func (s *grpcService) InspectJourney(ctx context.Context, in *journeyv1.InspectJourneyRequest) (*journeyv1.InspectJourneyResponse, error) {
	return s.client.InspectJourney(s.authorize(ctx), in)
}

func (s *grpcService) ExecuteJourney(ctx context.Context, in *journeyv1.ExecuteJourneyRequest) (*journeyv1.ExecuteJourneyResponse, error) {
	return s.client.ExecuteJourney(s.authorize(ctx), in)
}

func (s *grpcService) DecideJourney(ctx context.Context, in *journeyv1.DecideJourneyRequest) (*journeyv1.DecideJourneyResponse, error) {
	return s.client.DecideJourney(s.authorize(ctx), in)
}

func (s *grpcService) ListWorkers(ctx context.Context, in *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
	return s.client.ListWorkers(s.authorize(ctx), in)
}

func (s *grpcService) CreateWorker(ctx context.Context, in *journeyv1.CreateWorkerRequest) (*journeyv1.CreateWorkerResponse, error) {
	return s.client.CreateWorker(s.authorize(ctx), in)
}

func (s *grpcService) WatchJourney(ctx context.Context, in *journeyv1.WatchJourneyRequest) (WatchStream, error) {
	stream, err := s.client.WatchJourney(s.authorize(ctx), in)
	if err != nil {
		return nil, err
	}
	return stream, nil
}
