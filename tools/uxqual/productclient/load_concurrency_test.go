package productclient

import (
	"context"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
)

func TestLoadStartsIndependentNetworkReadsTogether(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			started <- "journeys"
			<-release
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			started <- "workers"
			<-release
			return &journeyv1.ListWorkersResponse{}, nil
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := Load(context.Background(), service, Session{}, State{Page: productui.PageHome})
		done <- err
	}()

	for index := 0; index < 2; index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("the second independent service read did not start while the first was in flight")
		}
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Load did not finish after both service reads resolved")
	}
}

func TestLoadingViewHumanizesSessionWithoutInventingRemoteCounts(t *testing.T) {
	view := LoadingView(Session{
		Tenant: "harborcare-demo", Principal: "local-developer", Scope: "compensation_review",
	}, State{Page: productui.PagePeople, Request: productui.PageRequest{Page: productui.PagePeople}})
	if view.Tenant != "Harborcare Demo" || view.Principal != "Local Developer" || view.Scope != "Compensation Review" {
		t.Fatalf("loading session labels = tenant %q principal %q scope %q", view.Tenant, view.Principal, view.Scope)
	}
	for _, item := range view.Navigation {
		if item.Page == productui.PageWork && item.Count != 0 {
			t.Fatalf("loading view invented a work count of %d", item.Count)
		}
	}
}
