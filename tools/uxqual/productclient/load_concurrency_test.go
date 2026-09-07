package productclient

import (
	"context"
	"errors"
	"sync/atomic"
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

func TestLoadWithBaselineSkipsUnusedPageDatasets(t *testing.T) {
	var journeyReads atomic.Int32
	var workerReads atomic.Int32
	var preferenceReads atomic.Int32
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			journeyReads.Add(1)
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			workerReads.Add(1)
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			preferenceReads.Add(1)
			return &journeyv1.GetProductPreferencesResponse{}, nil
		},
	}
	baseline := productui.NewView(productui.PageHome, "HarborCare", "worker-1", "employee")
	baseline.Work = []productui.WorkItem{{ID: "journey-1"}}
	baseline.People = []productui.Person{{ID: "worker-1", Name: "Rafael Torres"}}

	view, err := LoadWithBaseline(context.Background(), service, Session{Principal: "worker-1"}, State{
		Page: productui.PageSettings, Request: productui.PageRequest{Page: productui.PageSettings},
	}, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if journeyReads.Load() != 0 || workerReads.Load() != 0 || preferenceReads.Load() != 1 {
		t.Fatalf("reads = journeys %d workers %d preferences %d, want 0, 0, 1", journeyReads.Load(), workerReads.Load(), preferenceReads.Load())
	}
	if len(view.Work) != 1 || len(view.People) != 1 || view.Viewer.PersonID != "worker-1" {
		t.Fatalf("authorized shell projection was not retained: %+v", view)
	}
}

func TestLoadWithBaselineRefreshesOnlyPeopleForDirectory(t *testing.T) {
	var journeyReads atomic.Int32
	var workerReads atomic.Int32
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			journeyReads.Add(1)
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			workerReads.Add(1)
			return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{WorkerRef: "worker-new", PreferredName: "New Worker"}}}, nil
		},
	}
	baseline := productui.NewView(productui.PageHome, "HarborCare", "worker-1", "employee")
	baseline.Work = []productui.WorkItem{{ID: "journey-1"}}
	baseline.People = []productui.Person{{ID: "worker-old", Name: "Old Worker"}}

	view, err := LoadWithBaseline(context.Background(), service, Session{}, State{
		Page: productui.PagePeople, Request: productui.PageRequest{Page: productui.PagePeople},
	}, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if journeyReads.Load() != 0 || workerReads.Load() != 1 {
		t.Fatalf("reads = journeys %d workers %d, want 0, 1", journeyReads.Load(), workerReads.Load())
	}
	if len(view.Work) != 1 || view.Work[0].ID != "journey-1" || len(view.People) != 1 || view.People[0].ID != "worker-new" {
		t.Fatalf("selective projection = %+v", view)
	}
}

func TestLoadWithBaselineDoesNotReuseFilteredWorkAsShellData(t *testing.T) {
	var journeyReads atomic.Int32
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			journeyReads.Add(1)
			return &journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{{IntentId: "journey-complete", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED}}}, nil
		},
	}
	baseline := productui.NewView(productui.PageWork, "HarborCare", "worker-1", "employee")
	baseline.WorkFilter = "review"
	baseline.Work = []productui.WorkItem{{ID: "journey-review"}}

	view, err := LoadWithBaseline(context.Background(), service, Session{}, State{
		Page: productui.PageSettings, Request: productui.PageRequest{Page: productui.PageSettings},
	}, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if journeyReads.Load() != 1 {
		t.Fatalf("journey reads = %d, want 1", journeyReads.Load())
	}
	if len(view.Work) != 1 || view.Work[0].ID != "journey-complete" {
		t.Fatalf("filtered baseline leaked into destination shell: %+v", view.Work)
	}
}

func TestLoadWithBaselineFailsClosedWhenRequiredRefreshIsDenied(t *testing.T) {
	baseline := productui.NewView(productui.PageHome, "HarborCare", "worker-1", "employee")
	baseline.Work = []productui.WorkItem{{ID: "journey-old"}}
	baseline.People = []productui.Person{{ID: "worker-old", Name: "Old Worker"}}
	view, err := LoadWithBaseline(context.Background(), Service{
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return nil, errors.New("permission denied")
		},
	}, Session{}, State{Page: productui.PagePeople, Request: productui.PageRequest{Page: productui.PagePeople}}, baseline)
	if err == nil {
		t.Fatal("required refresh denial returned no error")
	}
	if len(view.People) != 0 {
		t.Fatalf("stale workforce remained visible after denial: %+v", view.People)
	}
	if len(view.Work) != 1 {
		t.Fatalf("unrelated authorized shell data was discarded: %+v", view.Work)
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
