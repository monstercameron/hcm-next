package health

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestHealthGRPCContractKeepsLivenessReadinessAndUnknownServicesSeparate(t *testing.T) {
	server := New(Dependencies{
		Live:       func() bool { return true },
		ReadyCheck: func(context.Context) error { return errors.New("private dependency reason") },
	})

	live, err := server.Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil || live.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("overall health = %v, %v; want SERVING", live, err)
	}
	ready, err := server.Check(context.Background(), &healthpb.HealthCheckRequest{Service: ReadyService})
	if err != nil || ready.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("ready health = %v, %v; want NOT_SERVING", ready, err)
	}
	_, err = server.Check(context.Background(), &healthpb.HealthCheckRequest{Service: "tenant/database"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("unknown health service error = %v, want NotFound", err)
	}

	listed, err := server.List(context.Background(), &healthpb.HealthListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed.GetStatuses()) != 2 || listed.GetStatuses()[ReadyService].GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("listed statuses = %v, want only overall and ready with ready NOT_SERVING", listed.GetStatuses())
	}
}
