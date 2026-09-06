// Package productclient projects canonical JourneyService responses onto the
// reusable product UI components. It owns no business facts and has no
// transport runtime dependency: the WASM composition supplies two RPC
// closures backed by the generated gRPC client.
package productclient

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
	"github.com/monstercameron/hcm-next/internal/humanwork/uicomponents"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service is the read-only portion of JourneyService needed by the product
// shell. Writes stay on the dedicated journey workflow surface.
type Service struct {
	ListJourneys func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error)
	ListWorkers  func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error)
}

// Session contains display facts from the server-admitted configuration
// island. The server authorizes every RPC independently; these values never
// grant authority.
type Session struct {
	Tenant    string
	Principal string
	Scope     string
}

// State is presentation-only address-bar state.
type State struct {
	Page    productui.PageID
	Request productui.PageRequest
}

// ParseState resolves a production product route and its presentation query.
func ParseState(pathname, rawQuery string) (State, error) {
	definition, ok := productui.LookupRoute(pathname)
	if !ok {
		return State{}, fmt.Errorf("productclient: unknown product route %q", pathname)
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return State{}, fmt.Errorf("productclient: parse route state: %w", err)
	}
	peoplePage, _ := strconv.Atoi(values.Get("page"))
	if peoplePage < 1 {
		peoplePage = 1
	}
	return State{Page: definition.ID, Request: productui.PageRequest{
		Page: definition.ID, Locale: values.Get("locale"), Query: values.Get("q"), Mode: values.Get("mode"),
		SelectedWork: values.Get("selected"), SelectedPerson: values.Get("person"),
		PeoplePage: peoplePage, WorkflowQuery: values.Get("workflow_q"), HistoryQuery: values.Get("history_q"), HistoryOutcome: values.Get("outcome"),
		HistoryPerson: values.Get("history_person"), HistoryYear: values.Get("history_year"), HistorySort: values.Get("history_sort"), HistoryDirection: values.Get("history_dir"),
		WorkFilter: values.Get("filter"), NavCollapsed: values.Get("nav") == "collapsed",
		JourneyID: values.Get("journey"), JourneyWorker: values.Get("worker"), JourneyMode: values.Get("mode"),
		MenuQuery: values.Get("menu_q"), FavoritePages: parseFavoritePages(values.Get("favorites")),
	}}, nil
}

func parseFavoritePages(raw string) []productui.PageID {
	parts := strings.Split(raw, ",")
	pages := make([]productui.PageID, 0, len(parts))
	for _, part := range parts {
		if page := productui.PageID(strings.TrimSpace(part)); page != "" {
			pages = append(pages, page)
		}
	}
	return pages
}

// Load calls the live service and returns an authorized component view. It
// preserves partial successful answers and joins failures so the UI can show
// an honest degraded state instead of substituting sample records.
func Load(ctx context.Context, service Service, session Session, state State) (productui.View, error) {
	view := productui.NewView(state.Page, displayLabel(session.Tenant), displayLabel(session.Principal), displayLabel(session.Scope))
	var failures []error
	if service.ListJourneys == nil {
		failures = append(failures, errors.New("JourneyService.ListJourneys is not connected"))
	} else if response, err := service.ListJourneys(ctx, &journeyv1.ListJourneysRequest{}); err != nil {
		failures = append(failures, fmt.Errorf("list journeys: %w", err))
	} else {
		var projectionErr error
		view.Work, projectionErr = projectJourneys(response.GetJourneys())
		if projectionErr != nil {
			failures = append(failures, projectionErr)
		}
	}
	if service.ListWorkers == nil {
		failures = append(failures, errors.New("JourneyService.ListWorkers is not connected"))
	} else if response, err := service.ListWorkers(ctx, &journeyv1.ListWorkersRequest{}); err != nil {
		failures = append(failures, fmt.Errorf("list workers: %w", err))
	} else {
		var projectionErr error
		view.People, projectionErr = projectWorkers(response.GetWorkers())
		if projectionErr != nil {
			failures = append(failures, projectionErr)
		}
	}
	for index := range view.Navigation {
		if view.Navigation[index].Page == productui.PageWork {
			view.Navigation[index].Count = len(view.Work)
		}
	}
	view = productui.ApplyRequest(view, state.Request)
	for index := range view.Work {
		view.Work[index].Href = productui.JourneyDetailHref(view, view.Work[index].ID)
	}
	view.PersonWorkflows = projectPersonWorkflows(view, view.SelectedPerson)
	return view, errors.Join(failures...)
}

func projectJourneys(journeys []*journeyv1.Journey) ([]productui.WorkItem, error) {
	items := make([]productui.WorkItem, 0, len(journeys))
	var failures []error
	for _, journey := range journeys {
		if journey == nil {
			continue
		}
		status, tone, terminal := stagePresentation(journey.GetStage())
		current, target := journey.GetCurrent(), journey.GetTarget()
		currentJob, targetJob := "", ""
		if current != nil {
			currentJob = strings.TrimSpace(current.GetJobCode() + " " + current.GetGrade())
		}
		if target != nil {
			targetJob = strings.TrimSpace(target.GetJobCode() + " " + target.GetGrade())
		}
		summary := strings.TrimSpace(currentJob + " → " + targetJob)
		currentBase, err := moneyFromWire(journey.GetCurrentBase(), journey.GetCurrency())
		if err != nil {
			failures = append(failures, fmt.Errorf("project journey %s current base: %w", journey.GetIntentId(), err))
		}
		proposedBase, err := moneyFromWire(journey.GetProposedBase(), journey.GetCurrency())
		if err != nil {
			failures = append(failures, fmt.Errorf("project journey %s proposed base: %w", journey.GetIntentId(), err))
		}
		items = append(items, productui.WorkItem{
			ID: journey.GetIntentId(), Initials: uicomponents.Initials(journey.GetWorkerName()), PhotoURL: employeePhotoURL(journey.GetWorkerRef(), journey.GetWorkerName()), Title: "Promotion journey",
			Person: journey.GetWorkerName(), PersonRef: journey.GetWorkerRef(), Summary: summary, Status: status, Tone: tone, Terminal: terminal,
			Due: journey.GetEffectiveDate(), EffectiveDate: journey.GetEffectiveDate(), CompletedAt: timestampLabel(journey.GetUpdatedAt()),
			InstanceID: journey.GetInstanceId(), InstanceVersion: journey.GetInstanceVersion(), MaterialDigest: journey.GetMaterialDigest(),
			CurrentBase: currentBase, ProposedBase: proposedBase,
		})
	}
	return items, errors.Join(failures...)
}

func timestampLabel(stamp *timestamppb.Timestamp) string {
	if stamp == nil || stamp.CheckValid() != nil {
		return ""
	}
	return stamp.AsTime().UTC().Format("2 Jan 2006 · 15:04 UTC")
}

func projectWorkers(workers []*journeyv1.Worker) ([]productui.Person, error) {
	people := make([]productui.Person, 0, len(workers))
	var failures []error
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		name := strings.TrimSpace(worker.GetPreferredName())
		if name == "" {
			name = strings.TrimSpace(worker.GetLegalName())
		}
		role := strings.TrimSpace(worker.GetJobCode() + " · " + worker.GetGrade())
		basePay, err := moneyFromWire(worker.GetBasePay(), worker.GetCurrency())
		if err != nil {
			failures = append(failures, fmt.Errorf("project worker %s base pay: %w", worker.GetWorkerRef(), err))
		}
		people = append(people, productui.Person{
			ID: worker.GetWorkerRef(), WorkerID: worker.GetWorkerId(), Initials: uicomponents.Initials(name), PhotoURL: employeePhotoURL(worker.GetWorkerRef(), name), Name: name,
			LegalName: worker.GetLegalName(), PreferredName: worker.GetPreferredName(), Role: role,
			Team: worker.GetOrgUnit(), Location: worker.GetLocation(), WorkerNumber: worker.GetWorkerNumber(),
			JobCode: worker.GetJobCode(), Grade: worker.GetGrade(), PositionID: worker.GetPositionId(),
			PayZone: worker.GetPayZone(), BasePay: basePay,
			BonusTarget: worker.GetBonusTarget(), HireDate: worker.GetHireDate(), Source: worker.GetSource(),
			CreatedAt: timestampLabel(worker.GetCreatedAt()),
		})
	}
	return people, errors.Join(failures...)
}

// employeePhotoURL binds the stable demo employees exposed by the seeded cell
// to same-origin product assets. Unknown workers retain the shared initials
// fallback instead of receiving a misleading stock portrait.
func employeePhotoURL(workerRef, name string) string {
	identity := strings.ToLower(strings.TrimSpace(workerRef + " " + name))
	for _, employee := range []struct {
		key, asset string
	}{
		{"priya", "person-priya-small.jpg"},
		{"jane", "person-jane-small.jpg"},
		{"omar", "person-omar-small.jpg"},
		{"lena", "person-lena-small.jpg"},
		{"noor", "person-noor-small.jpg"},
	} {
		if strings.Contains(identity, employee.key) {
			return "/workspace/assets/" + employee.asset
		}
	}
	return ""
}

// moneyFromWire is the only product-UI boundary that accepts the journey
// service's decimal text plus currency pair. It immediately binds them into
// the shared exact Money value object; neither the component model nor any
// renderer can subsequently perform binary floating-point money arithmetic.
func moneyFromWire(amount, currency string) (values.Money, error) {
	amount = strings.TrimSpace(amount)
	currency = strings.TrimSpace(currency)
	if amount == "" {
		return values.Money{}, nil
	}
	scale := int32(0)
	if dot := strings.IndexByte(amount, '.'); dot >= 0 {
		fractionalDigits := len(amount) - dot - 1
		if fractionalDigits > int(values.MaxScale) {
			return values.Money{}, fmt.Errorf("decimal scale %d exceeds %d", fractionalDigits, values.MaxScale)
		}
		scale = int32(fractionalDigits)
	}
	return values.NewMoney(amount, currency, scale, values.RoundingExactRequired)
}

func projectPersonWorkflows(view productui.View, workerRef string) []productui.PersonWorkflow {
	workerRef = strings.TrimSpace(workerRef)
	if workerRef == "" {
		return nil
	}
	return []productui.PersonWorkflow{{
		ID: "promotion", Name: "Promotion", Category: "Career & compensation",
		Description: "Propose a governed job, grade, position, and compensation change.",
		Href:        productui.JourneyProposalHref(view, workerRef),
	}}
}

func stagePresentation(stage journeyv1.JourneyStage) (status, tone string, terminal bool) {
	switch stage {
	case journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED:
		return "Ready to execute", "neutral", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED:
		return "Blocked", "warning", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL:
		return "Awaiting approval", "warning", false
	case journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED:
		return "Completed", "success", true
	case journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED:
		return "Rejected", "neutral", true
	case journeyv1.JourneyStage_JOURNEY_STAGE_FAILED:
		return "Failed", "danger", true
	default:
		return "Unknown stage", "warning", false
	}
}

func displayLabel(value string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(strings.TrimSpace(value)))
	for index, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) > 0 {
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		}
		words[index] = string(runes)
	}
	return strings.Join(words, " ")
}
