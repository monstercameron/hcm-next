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
	"sync"

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

// LoadingView builds the non-authoritative shell shown while Load is waiting
// for the cell. It shares the resolved view's humanized session labels and
// address state without manufacturing any business records or counts.
func LoadingView(session Session, state State) productui.View {
	view := productui.NewView(state.Page, displayLabel(session.Tenant), displayLabel(session.Principal), displayLabel(session.Scope))
	return productui.ApplyRequest(view, state.Request)
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
		PeoplePage: peoplePage, PeopleTeam: values.Get("team"), PeopleLocation: values.Get("location"), PeopleSort: values.Get("sort"), PeopleDirection: values.Get("dir"),
		WorkflowQuery: values.Get("workflow_q"), HistoryQuery: values.Get("history_q"), HistoryOutcome: values.Get("outcome"),
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
	view := LoadingView(session, state)
	var failures []error
	var journeysResponse *journeyv1.ListJourneysResponse
	var workersResponse *journeyv1.ListWorkersResponse
	var journeysErr, workersErr error
	var reads sync.WaitGroup
	if service.ListJourneys == nil {
		journeysErr = errors.New("JourneyService.ListJourneys is not connected")
	} else {
		reads.Add(1)
		go func() {
			defer reads.Done()
			journeysResponse, journeysErr = service.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
			if journeysErr != nil {
				journeysErr = fmt.Errorf("list journeys: %w", journeysErr)
			}
		}()
	}
	if service.ListWorkers == nil {
		workersErr = errors.New("JourneyService.ListWorkers is not connected")
	} else {
		reads.Add(1)
		go func() {
			defer reads.Done()
			workersResponse, workersErr = service.ListWorkers(ctx, &journeyv1.ListWorkersRequest{})
			if workersErr != nil {
				workersErr = fmt.Errorf("list workers: %w", workersErr)
			}
		}()
	}
	reads.Wait()

	if journeysErr != nil {
		failures = append(failures, journeysErr)
	} else {
		var projectionErr error
		view.Work, projectionErr = projectJourneys(journeysResponse.GetJourneys())
		if projectionErr != nil {
			failures = append(failures, projectionErr)
		}
	}
	if workersErr != nil {
		failures = append(failures, workersErr)
	} else {
		var projectionErr error
		view.People, projectionErr = projectWorkers(workersResponse.GetWorkers())
		if projectionErr != nil {
			failures = append(failures, projectionErr)
		}
	}
	photosByWorker := make(map[string]string, len(view.People))
	for _, person := range view.People {
		photosByWorker[person.ID] = person.PhotoURL
	}
	for index := range view.Work {
		if photo := photosByWorker[view.Work[index].PersonRef]; photo != "" {
			view.Work[index].PhotoURL = photo
		}
	}
	for index := range view.Navigation {
		if view.Navigation[index].Page == productui.PageWork {
			view.Navigation[index].Count = len(productui.OpenWorkItems(view.Work))
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
			currentJob = concisePlacementLabel(current.GetJobCode(), current.GetGrade())
		}
		if target != nil {
			targetJob = concisePlacementLabel(target.GetJobCode(), target.GetGrade())
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
	namesByRef := make(map[string]string, len(workers)*2)
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		name := workerDisplayName(worker)
		namesByRef[worker.GetWorkerRef()] = name
		namesByRef[worker.GetWorkerId()] = name
	}
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		name := workerDisplayName(worker)
		title := strings.TrimSpace(worker.GetJobTitle())
		if title == "" {
			// A job code is more truthful and legible than title-casing an acronym
			// when the authoritative service has not published a job title.
			title = strings.TrimSpace(worker.GetJobCode())
		}
		role := strings.TrimSpace(title + " · " + worker.GetGrade())
		photoURL := strings.TrimSpace(worker.GetProfilePhotoUrl())
		if photoURL == "" {
			photoURL = employeePhotoURL(worker.GetWorkerRef(), name)
		}
		basePay, err := moneyFromWire(worker.GetBasePay(), worker.GetCurrency())
		if err != nil {
			failures = append(failures, fmt.Errorf("project worker %s base pay: %w", worker.GetWorkerRef(), err))
		}
		people = append(people, productui.Person{
			ID: worker.GetWorkerRef(), WorkerID: worker.GetWorkerId(), Initials: uicomponents.Initials(name), PhotoURL: photoURL, Name: name,
			LegalName: worker.GetLegalName(), PreferredName: worker.GetPreferredName(), Role: role,
			Team: orgUnitLabel(worker.GetOrgUnit()), Manager: managerLabel(worker.GetManagerRef(), namesByRef), Location: worker.GetLocation(), WorkerNumber: worker.GetWorkerNumber(),
			JobCode: worker.GetJobCode(), Grade: worker.GetGrade(), PositionID: worker.GetPositionId(),
			PayZone: worker.GetPayZone(), BasePay: basePay,
			BonusTarget: worker.GetBonusTarget(), HireDate: worker.GetHireDate(), Source: worker.GetSource(),
			CreatedAt: timestampLabel(worker.GetCreatedAt()),
		})
	}
	return people, errors.Join(failures...)
}

func workerDisplayName(worker *journeyv1.Worker) string {
	if worker == nil {
		return ""
	}
	name := strings.TrimSpace(worker.GetPreferredName())
	if name == "" {
		name = strings.TrimSpace(worker.GetLegalName())
	}
	return name
}

func managerLabel(ref string, namesByRef map[string]string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if name := namesByRef[ref]; name != "" {
		return name
	}
	lowerRef := strings.ToLower(ref)
	if strings.HasPrefix(lowerRef, "board:") {
		return "HarborCare Board"
	}
	if strings.HasPrefix(lowerRef, "rel:") || strings.HasPrefix(lowerRef, "rel-") || strings.HasPrefix(lowerRef, "rel_") || strings.HasPrefix(lowerRef, "eref:") {
		return "Not available"
	}
	return displayLabel(ref)
}

func concisePlacementLabel(jobCode, grade string) string {
	jobCode = strings.TrimSpace(jobCode)
	for prefixLength := 1; prefixLength <= len(jobCode)/2; prefixLength++ {
		if len(jobCode)%prefixLength != 0 {
			continue
		}
		prefix := jobCode[:prefixLength]
		if strings.Repeat(prefix, len(jobCode)/prefixLength) == jobCode {
			jobCode = prefix
			break
		}
	}
	return strings.TrimSpace(jobCode + " " + strings.TrimSpace(grade))
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
		return "Status unavailable", "warning", false
	}
}

func displayLabel(value string) string {
	return productui.DisplayLabel(value)
}

func orgUnitLabel(code string) string {
	if label, ok := map[string]string{
		"care-operations":      "Care Operations",
		"data-analytics":       "Data & Analytics",
		"eng-platform":         "Engineering Platform",
		"engineering-platform": "Engineering Platform",
		"growth-customer":      "Growth & Customer",
		"legal-compliance":     "Legal & Compliance",
		"people-ops":           "People Operations",
		"people-operations":    "People Operations",
		"product-technology":   "Product & Technology",
		"quality-safety":       "Quality & Safety",
		"security-it":          "Security & IT",
	}[strings.ToLower(strings.TrimSpace(code))]; ok {
		return label
	}
	return displayLabel(code)
}
