package productclient

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
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
	baseline := productui.NewView(productui.PageHome, "Harborcare", "Worker 1", "Employee")
	baseline.Work = []productui.WorkItem{{ID: "journey-1"}}
	baseline.People = []productui.Person{{ID: "worker-1", Name: "Rafael Torres"}}

	view, err := LoadWithBaseline(context.Background(), service, Session{Tenant: "harborcare", Principal: "worker-1", Scope: "employee"}, State{
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

// TestLoadWithBaselineRefreshesPeopleAndJourneysForDirectory proves the
// People page's targeted refresh set. It used to read only workers
// (requirementsForPage's original PagePeople case); PROMOUX-001 added
// journeys to that set because the directory's per-worker promotion
// availability needs to know about a nonterminal journey already in flight
// for that worker, and a targeted People-only refresh must not answer that
// question with stale or absent journey data.
func TestLoadWithBaselineRefreshesPeopleAndJourneysForDirectory(t *testing.T) {
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
	baseline := productui.NewView(productui.PageHome, "Harborcare", "Worker 1", "Employee")
	baseline.Work = []productui.WorkItem{{ID: "journey-1"}}
	baseline.People = []productui.Person{{ID: "worker-old", Name: "Old Worker"}}

	view, err := LoadWithBaseline(context.Background(), service, Session{Tenant: "harborcare", Principal: "worker-1", Scope: "employee"}, State{
		Page: productui.PagePeople, Request: productui.PageRequest{Page: productui.PagePeople},
	}, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if journeyReads.Load() != 1 || workerReads.Load() != 1 {
		t.Fatalf("reads = journeys %d workers %d, want 1, 1", journeyReads.Load(), workerReads.Load())
	}
	if len(view.Work) != 0 || len(view.People) != 1 || view.People[0].ID != "worker-new" {
		t.Fatalf("refreshed projection = %+v", view)
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
	baseline := productui.NewView(productui.PageWork, "Harborcare", "Worker 1", "Employee")
	baseline.WorkFilter = "review"
	baseline.Work = []productui.WorkItem{{ID: "journey-review"}}

	view, err := LoadWithBaseline(context.Background(), service, Session{Tenant: "harborcare", Principal: "worker-1", Scope: "employee"}, State{
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

// TestLoadWithBaselineFailsClosedWhenRequiredRefreshIsDenied proves a denied
// required resource fails closed (stale data does not linger) without
// corrupting a sibling required resource's own successful refresh. Since
// PROMOUX-001, PagePeople requires both workers and journeys, so this now
// exercises ListWorkers failing while ListJourneys succeeds: the stale
// People baseline must be discarded, and the freshly authorized journeys
// answer must still land uncorrupted rather than being wiped as collateral
// damage from the unrelated denial.
func TestLoadWithBaselineFailsClosedWhenRequiredRefreshIsDenied(t *testing.T) {
	baseline := productui.NewView(productui.PageHome, "Harborcare", "Worker 1", "Employee")
	baseline.Work = []productui.WorkItem{{ID: "journey-old"}}
	baseline.People = []productui.Person{{ID: "worker-old", Name: "Old Worker"}}
	view, err := LoadWithBaseline(context.Background(), Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{{IntentId: "journey-fresh"}}}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return nil, errors.New("permission denied")
		},
	}, Session{Tenant: "harborcare", Principal: "worker-1", Scope: "employee"}, State{Page: productui.PagePeople, Request: productui.PageRequest{Page: productui.PagePeople}}, baseline)
	if err == nil {
		t.Fatal("required refresh denial returned no error")
	}
	if len(view.People) != 0 {
		t.Fatalf("stale workforce remained visible after denial: %+v", view.People)
	}
	if len(view.Work) != 1 || view.Work[0].ID != "journey-fresh" {
		t.Fatalf("the sibling required resource's successful refresh was discarded or corrupted: %+v", view.Work)
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
