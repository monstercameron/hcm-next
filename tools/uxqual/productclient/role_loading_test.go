package productclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
)

func TestIndividualContributorLoadDoesNotRequestJourneyHistory(t *testing.T) {
	var journeyCalls, workerCalls int
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			journeyCalls++
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			workerCalls++
			return &journeyv1.ListWorkersResponse{}, nil
		},
	}
	_, err := Load(context.Background(), service, Session{Tenant: "tenant", Principal: "worker", Scope: "self_service_view", Roles: []string{"worker_self"}, EnforceRoleVisibility: true}, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	if err != nil {
		t.Fatal(err)
	}
	if journeyCalls != 0 || workerCalls != 1 {
		t.Fatalf("RPC calls = journeys %d workers %d, want 0/1", journeyCalls, workerCalls)
	}
}
