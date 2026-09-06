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

// NewProposePromotionHandler returns the typed HTTP handler for the
// no-effect promotion proposal. Admission is supplied by the edge through
// opts; this package intentionally does not inspect headers or authenticate
// a request a second time.
func NewProposePromotionHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{deps: deps}
	return connect.NewUnaryHandler(
		ProposePromotionProcedure,
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
