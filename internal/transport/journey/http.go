package journey

import (
	"context"
	"net/http"

	"connectrpc.com/connect"

	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
)

// ProposePromotionProcedure is the canonical procedure name shared by the
// native gRPC and Connect/HTTP transports.
const ProposePromotionProcedure = "/hcmnext.journey.v1.JourneyService/ProposePromotion"

// ProposeIntoManagementProcedure is the versioned semantic HTTP projection
// published for the typed promotion entry point. It deliberately points at
// the same handler as ProposePromotionProcedure: the route is a transport
// projection, not a second promotion implementation.
const ProposeIntoManagementProcedure = "/v1/promotions:proposeIntoManagement"

// NewProposePromotionHandler returns the typed HTTP handler for the
// no-effect promotion proposal. Admission is supplied by the edge through
// opts; this package intentionally does not inspect headers or authenticate
// a request a second time.
func NewProposePromotionHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	return newProposePromotionHandler(ProposePromotionProcedure, deps, opts...)
}

// NewProposeIntoManagementHandler returns the versioned HTTP projection for
// PromotionService.ProposeIntoManagement. The generated gRPC contract in this
// repository names the same operation JourneyService.ProposePromotion; both
// projections call the one application port and therefore have identical
// admission, validation, idempotency and no-effect semantics.
func NewProposeIntoManagementHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	return newProposePromotionHandler(ProposeIntoManagementProcedure, deps, opts...)
}

func newProposePromotionHandler(procedure string, deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{deps: deps}
	return connect.NewUnaryHandler(
		procedure,
		func(ctx context.Context, req *connect.Request[journeyv1.ProposePromotionRequest]) (*connect.Response[journeyv1.ProposePromotionResponse], error) {
			res, err := s.ProposePromotion(ctx, req.Msg)
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(res), nil
		},
		opts...,
	)
}
