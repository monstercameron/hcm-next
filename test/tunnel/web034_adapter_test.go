package tunnel_test

import (
	"context"
	"strings"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/hcm-next/tools/uxqual/journeyclient"
	"google.golang.org/grpc/metadata"
)

// TestWEB034QualifiedAdapterRunsThroughRealTunnel proves the production
// browser boundary is not only compatible with a fake ClientConnInterface.
// The generated client runs through the bounded adapter, the real
// GoGRPCBridge websocket tunnel, the shared interceptors and the canonical
// JourneyService handler. Both unary and server-streaming shapes are covered.
func TestWEB034QualifiedAdapterRunsThroughRealTunnel(t *testing.T) {
	engine := newFakeJourneyEngine()
	cell := newCellWith(t, true, engine)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := cell.dial(ctx, cell.authorizedUpgrade())
	bearer := strings.TrimPrefix(cell.token, journeyclient.BearerScheme)
	adapter := journeyclient.NewRPCAdapter(conn, journeyclient.RPCAdapterConfig{Bearer: bearer})
	client := journeyv1.NewJourneyServiceClient(adapter)

	traceCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"x-request-id", "web034-real-tunnel",
		"traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	))
	listed, err := client.ListJourneys(traceCtx, &journeyv1.ListJourneysRequest{})
	if err != nil {
		t.Fatalf("ListJourneys through qualified adapter and tunnel: %v", err)
	}
	if len(listed.GetJourneys()) != 1 {
		t.Fatalf("journeys = %d, want one canonical projection", len(listed.GetJourneys()))
	}
	got := listed.GetJourneys()[0]
	if got.GetIntentId() != tunnelJourneyIntentID || got.GetWorkerName() != "Jordan Vega" || got.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL {
		t.Fatalf("journey = %+v, want the engine's exact authorized projection", got)
	}

	streamCtx, stopStream := context.WithCancel(ctx)
	stream, err := client.WatchJourney(streamCtx, &journeyv1.WatchJourneyRequest{IntentId: tunnelJourneyIntentID})
	if err != nil {
		t.Fatalf("WatchJourney through qualified adapter and tunnel: %v", err)
	}
	events := drain(stream)
	opening := nextWatchEvent(t, events, 20*time.Second)
	if opening.err != nil {
		t.Fatalf("opening stream projection: %v", opening.err)
	}
	if opening.msg.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL {
		t.Fatalf("opening stream projection = %+v, want awaiting approval", opening.msg.GetDetail())
	}
	stopStream()
	closed := nextWatchEvent(t, events, 20*time.Second)
	if closed.err == nil {
		t.Fatal("cancelled qualified stream remained open")
	}
}
