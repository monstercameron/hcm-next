package productclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

func TestServerPreferencesBecomeDefaultsButExplicitURLStateWins(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			return &journeyv1.GetProductPreferencesResponse{User: &journeyv1.UserPreferences{Version: 7, Locale: "de-DE", NavCollapsed: true,
				Tables:       map[string]*journeyv1.TablePreferences{"people": {PageSize: 50, Filters: map[string]string{"team": "Care Operations"}, Sort: "role", Direction: "desc"}},
				WorkflowUses: map[string]int64{"promotion": 9}}, Theme: &journeyv1.CustomerTheme{Version: 3, BrandName: "HarborCare", BrandMark: "HC", Palette: "ocean", ColorMode: "dark", Shape: "rounded", Density: "compact", Glyphs: "bold-line", Typeface: "modern", Navigation: "brand", Motion: "brisk"}}, nil
		},
	}
	state, err := ParseState("/workspace/app/people", "page_size=10&team=")
	if err != nil {
		t.Fatal(err)
	}
	view, err := Load(context.Background(), service, Session{Tenant: "tenant", Principal: "alice"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if view.PeoplePageSize != 10 || view.PeopleTeam != "" {
		t.Fatalf("explicit URL state lost: size=%d team=%q", view.PeoplePageSize, view.PeopleTeam)
	}
	if view.PeopleSort != "role" || view.PeopleDirection != "desc" {
		t.Fatalf("stored sort not applied: %s %s", view.PeopleSort, view.PeopleDirection)
	}
	if view.Locale.Resolved != "de-DE" || !view.NavCollapsed || view.Appearance.BrandName != "HarborCare" {
		t.Fatalf("stored presentation not applied: locale=%s nav=%v theme=%+v", view.Locale.Resolved, view.NavCollapsed, view.Appearance)
	}
	if view.WorkflowUses["promotion"] != 9 || view.StoredPreferences.Version != 7 {
		t.Fatalf("usage/baseline missing: %+v", view)
	}
}

func TestExplicitSortColumnDoesNotInheritStaleSavedDirection(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			return &journeyv1.GetProductPreferencesResponse{User: &journeyv1.UserPreferences{Tables: map[string]*journeyv1.TablePreferences{
				"people":  {Sort: "manager", Direction: "desc"},
				"history": {Sort: "closed", Direction: "desc"},
			}}}, nil
		},
	}

	peopleState, err := ParseState("/workspace/app/people", "sort=role")
	if err != nil {
		t.Fatal(err)
	}
	people, err := Load(context.Background(), service, Session{}, peopleState)
	if err != nil {
		t.Fatal(err)
	}
	if people.PeopleSort != "role" || people.PeopleDirection != "asc" {
		t.Fatalf("first people sort click inherited saved direction: sort=%q direction=%q", people.PeopleSort, people.PeopleDirection)
	}

	historyState, err := ParseState("/workspace/app/history", "history_sort=person")
	if err != nil {
		t.Fatal(err)
	}
	history, err := Load(context.Background(), service, Session{}, historyState)
	if err != nil {
		t.Fatal(err)
	}
	if history.HistorySort != "person" || history.HistoryDirection == "desc" {
		t.Fatalf("first history sort click inherited saved direction: sort=%q direction=%q", history.HistorySort, history.HistoryDirection)
	}
}
