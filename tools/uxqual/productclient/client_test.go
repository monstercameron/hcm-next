package productclient

import (
	"context"
	"errors"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
)

func TestLoadProjectsOnlyLiveServiceAnswers(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{{
				IntentId: "intent-live", WorkerName: "Riley Chen", EffectiveDate: "2026-10-01",
				InstanceId: "instance-live", InstanceVersion: 7, MaterialDigest: "sha256:proposal-live",
				Current: &journeyv1.Placement{JobCode: "ENG2", Grade: "G6"},
				Target:  &journeyv1.Placement{JobCode: "ENG3", Grade: "G7"},
				Stage:   journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL,
			}}}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{
				WorkerRef: "worker-live", WorkerId: "worker-id-live", LegalName: "Riley Morgan Chen", PreferredName: "Riley Chen", WorkerNumber: "NW-9", JobCode: "ENG2", Grade: "G6", OrgUnit: "Engineering",
				PositionId: "pos-9", PayZone: "US-1", BasePay: "120000", Currency: "USD", BonusTarget: "0.10", HireDate: "2020-02-03", Source: "CREATED",
			}}}, nil
		},
	}
	view, err := Load(context.Background(), service, Session{Tenant: "tenant-live", Principal: "Riley", Scope: "manager"}, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Work) != 1 || view.Work[0].ID != "intent-live" || len(view.People) != 1 || view.People[0].ID != "worker-live" {
		t.Fatalf("projection did not use server answers: %+v", view)
	}
	if view.Work[0].InstanceID != "instance-live" || view.Work[0].InstanceVersion != 7 || view.Work[0].MaterialDigest != "sha256:proposal-live" {
		t.Fatalf("durable history provenance was not preserved: %+v", view.Work[0])
	}
	if view.Title != "Good morning, Riley." || workNavigationCount(view.Navigation) != 1 {
		t.Fatalf("session/count projection = %+v", view)
	}
	person := view.People[0]
	if person.WorkerID != "worker-id-live" || person.LegalName != "Riley Morgan Chen" || person.PreferredName != "Riley Chen" || person.WorkerNumber != "NW-9" || person.PositionID != "pos-9" ||
		person.BasePay.Amount().String() != "120000" || person.BasePay.Currency() != "USD" || person.Source != "CREATED" {
		t.Fatalf("worker detail projection lost live facts: %+v", person)
	}
}

func TestMoneyProjectionRejectsFloatLikeAndCurrencylessAmounts(t *testing.T) {
	for _, tc := range []struct {
		name, amount, currency string
	}{
		{name: "binary-float spelling", amount: "1e3", currency: "USD"},
		{name: "non-finite", amount: "NaN", currency: "USD"},
		{name: "missing currency", amount: "1000.00"},
		{name: "noncanonical currency", amount: "1000.00", currency: "usd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := moneyFromWire(tc.amount, tc.currency); err == nil {
				t.Fatalf("moneyFromWire(%q, %q) accepted a non-money value", tc.amount, tc.currency)
			}
		})
	}
}

func TestMoneyProjectionPreservesExactDecimalScale(t *testing.T) {
	money, err := moneyFromWire("9007199254740993.01", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if got := money.Amount().String(); got != "9007199254740993.01" {
		t.Fatalf("exact amount = %q, want the cent above float64's safe integer range", got)
	}
}

func TestLoadNeverSubstitutesFixturesOnFailure(t *testing.T) {
	view, err := Load(context.Background(), Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return nil, errors.New("offline")
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return nil, errors.New("offline")
		},
	}, Session{}, State{Page: productui.PageHome})
	if err == nil || len(view.Work) != 0 || len(view.People) != 0 {
		t.Fatalf("failed live load invented records: view=%+v err=%v", view, err)
	}
}

func TestParseStateUsesProductionRoutes(t *testing.T) {
	state, err := ParseState("/workspace/app/work", "nav=collapsed&selected=intent-1&page=3&menu_q=work&favorites=history,people,history")
	if err != nil {
		t.Fatal(err)
	}
	if state.Page != productui.PageWork || !state.Request.NavCollapsed || state.Request.SelectedWork != "intent-1" || state.Request.PeoplePage != 3 ||
		state.Request.MenuQuery != "work" || len(state.Request.FavoritePages) != 3 || state.Request.FavoritePages[0] != productui.PageHistory {
		t.Fatalf("state = %+v", state)
	}
	if _, err := ParseState("/app/work", ""); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("legacy mock route unexpectedly accepted: %v", err)
	}
	state, err = ParseState("/workspace/app/people", "page=invalid")
	if err != nil || state.Request.PeoplePage != 1 {
		t.Fatalf("invalid people page was not normalized: state=%+v err=%v", state, err)
	}
	state, err = ParseState("/workspace/app/history", "history_q=Avery&outcome=completed&history_person=worker-avery&history_year=2026&history_sort=person&history_dir=asc&nav=collapsed")
	if err != nil || state.Page != productui.PageHistory || state.Request.HistoryQuery != "Avery" || state.Request.HistoryOutcome != "completed" ||
		state.Request.HistoryPerson != "worker-avery" || state.Request.HistoryYear != "2026" || state.Request.HistorySort != "person" ||
		state.Request.HistoryDirection != "asc" || !state.Request.NavCollapsed {
		t.Fatalf("history route state was not preserved: state=%+v err=%v", state, err)
	}
}

func TestPersonRouteProjectsOnlySupportedWorkflowForSelectedWorker(t *testing.T) {
	state, err := ParseState("/workspace/app/person", "person=worker-live&workflow_q=promo&nav=collapsed")
	if err != nil {
		t.Fatal(err)
	}
	if state.Page != productui.PagePerson || state.Request.SelectedPerson != "worker-live" || state.Request.WorkflowQuery != "promo" || !state.Request.NavCollapsed {
		t.Fatalf("person route state = %+v", state)
	}
	view, err := Load(context.Background(), Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{WorkerRef: "worker-live", PreferredName: "Riley Chen"}}}, nil
		},
	}, Session{}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.PersonWorkflows) != 1 || view.PersonWorkflows[0].ID != "promotion" || view.PersonWorkflows[0].Href != "/workspace/app/journeys?mode=new&nav=collapsed&worker=worker-live" {
		t.Fatalf("person workflow projection = %+v", view.PersonWorkflows)
	}
}

func TestSessionTokensBecomeReadableLabelsWithoutChangingRPCState(t *testing.T) {
	if got := displayLabel("compensation_review"); got != "Compensation Review" {
		t.Fatalf("display label = %q", got)
	}
	if got := displayLabel("local-developer"); got != "Local Developer" {
		t.Fatalf("principal label = %q", got)
	}
}

func TestPromotionWorkflowHrefEscapesReservedWorkerReferences(t *testing.T) {
	workflows := projectPersonWorkflows(productui.NewView(productui.PagePerson, "", "", ""), "worker/a+b & c")
	if len(workflows) != 1 || workflows[0].Href != "/workspace/app/journeys?mode=new&worker=worker%2Fa%2Bb+%26+c" {
		t.Fatalf("workflow hrefs = %+v", workflows)
	}
}

func workNavigationCount(items []productui.NavItem) int {
	for _, item := range items {
		if item.Page == productui.PageWork {
			return item.Count
		}
	}
	return -1
}

func TestEmployeePhotoURLUsesKnownIdentityAndUnknownFallback(t *testing.T) {
	for _, test := range []struct {
		ref, name, want string
	}{
		{"eref:v1:demo:worker:444", "Noor Haddad", "/workspace/assets/person-noor-small.jpg"},
		{"priya-01a07058", "", "/workspace/assets/person-priya-small.jpg"},
		{"worker-live", "Riley Chen", ""},
	} {
		if got := employeePhotoURL(test.ref, test.name); got != test.want {
			t.Errorf("employeePhotoURL(%q, %q) = %q, want %q", test.ref, test.name, got, test.want)
		}
	}
}
