package productui

import "github.com/monstercameron/hcm-next/internal/kernel/values"

// PageID identifies one stable product surface. The server resolves whether a
// page is discoverable before it includes the page in View.Navigation.
type PageID string

const (
	PageHome         PageID = "home"
	PageJourneys     PageID = "journeys"
	PageWork         PageID = "work"
	PageHistory      PageID = "history"
	PagePeople       PageID = "people"
	PagePerson       PageID = "person"
	PageOrganization PageID = "organization"
	PageInsights     PageID = "insights"
	PageAdmin        PageID = "admin"
	PageAppearance   PageID = "appearance"
	PageStudio       PageID = "studio"
	PageHelp         PageID = "help"
	PageSettings     PageID = "settings"
)

type NavItem struct {
	Page        PageID
	Label       string
	LabelKey    string
	Description string
	Keywords    []string
	Icon        string
	Count       int
	Children    []NavItem
}

type WorkItem struct {
	ID              string
	Initials        string
	PhotoURL        string
	Title           string
	Person          string
	PersonRef       string
	Summary         string
	Status          string
	Due             string
	Tone            string
	Href            string
	EffectiveDate   string
	CompletedAt     string
	InstanceID      string
	InstanceVersion int64
	MaterialDigest  string
	CurrentBase     values.Money
	ProposedBase    values.Money
	Terminal        bool
}

type Person struct {
	ID            string
	WorkerID      string
	Initials      string
	PhotoURL      string
	Name          string
	LegalName     string
	PreferredName string
	Role          string
	Team          string
	Manager       string
	Location      string
	WorkerNumber  string
	JobCode       string
	Grade         string
	PositionID    string
	PayZone       string
	BasePay       values.Money
	BonusTarget   string
	HireDate      string
	Source        string
	CreatedAt     string
}

// PersonWorkflow is a workflow launcher the live product adapter has made
// available for a person. It is presentation metadata, not action authority;
// the destination service still authorizes and validates every proposal.
type PersonWorkflow struct {
	ID          string
	Name        string
	Category    string
	Description string
	Href        string
}

// View is an already-authorized presentation projection. It contains no
// credential or raw sensitive record and grants no action authority.
type View struct {
	Page             PageID
	Title            string
	Subtitle         string
	Tenant           string
	Principal        string
	Scope            string
	Navigation       []NavItem
	Work             []WorkItem
	People           []Person
	PersonWorkflows  []PersonWorkflow
	SelectedWork     string
	SelectedPerson   string
	Query            string
	PeoplePage       int
	PeopleTeam       string
	PeopleLocation   string
	PeopleSort       string
	PeopleDirection  string
	WorkflowQuery    string
	HistoryQuery     string
	HistoryOutcome   string
	HistoryPerson    string
	HistoryYear      string
	HistorySort      string
	HistoryDirection string
	Mode             string
	WorkFilter       string
	JourneyID        string
	JourneyWorker    string
	JourneyMode      string
	NavCollapsed     bool
	MenuQuery        string
	FavoritePages    []PageID
	// NavigationGroupOpen contains the browser's tenant-scoped disclosure
	// preferences. Missing entries retain the page's contextual default.
	NavigationGroupOpen  map[PageID]bool
	Locale               LocaleContext
	Appearance           CustomerTheme
	Accessibility        AccessibilityPreferences
	PreviewTheme         func(CustomerTheme)
	SaveTheme            func(CustomerTheme)
	ResetTheme           func()
	PreviewAccessibility func(AccessibilityPreferences)
	SaveAccessibility    func(AccessibilityPreferences)
	ResetAccessibility   func()
	Source               string
	LoadError            string
	// Loading is presentation state set only while the route's authorized
	// database-backed projection is resolving. It never implies authority or
	// substitutes empty records for an answer from the service.
	Loading bool
	// Navigate is installed by the WASM history router. A nil callback keeps
	// server rendering and tests as ordinary progressive-enhancement links.
	Navigate                  func(string)
	NavigateDebounced         func(string)
	CancelDebouncedNavigation func()
}

// NewView creates an empty, honest presentation projection. Live adapters
// populate Work and People from server answers; the component library never
// manufactures business records to make a page look populated.
func NewView(page PageID, tenant, principal, scope string) View {
	if _, ok := LookupPage(page); !ok {
		page = PageHome
	}
	view := View{
		Page: page, Tenant: tenant,
		Principal: principal, Scope: scope, Source: "JourneyService",
		Locale: ResolveProductLocale(""), Accessibility: DefaultAccessibilityPreferences(),
	}
	return ApplyLocale(view, view.Locale)
}

// ApplyLocale resolves all shell and page-registry copy from one immutable
// presentation context. Business records remain untouched.
func ApplyLocale(view View, locale LocaleContext) View {
	locale = locale.normalized()
	view.Locale = locale
	definition, ok := LookupPage(view.Page)
	if !ok {
		definition, _ = LookupPage(PageHome)
	}
	view.Title = locale.Text(definition.TitleKey)
	view.Subtitle = locale.Text(definition.SubtitleKey)
	if view.Page == PageHome && view.Principal != "" {
		view.Title = locale.Text("page.home.greeting", map[string]string{"name": view.Principal})
	}
	if len(view.Navigation) == 0 {
		view.Navigation = defaultNavigation(locale)
	} else {
		view.Navigation = localizeNavigation(view.Navigation, locale)
	}
	return view
}

func localizeNavigation(items []NavItem, locale LocaleContext) []NavItem {
	result := append([]NavItem(nil), items...)
	for index := range result {
		if result[index].LabelKey != "" {
			result[index].Label = locale.Text(result[index].LabelKey)
		} else if definition, ok := LookupPage(result[index].Page); ok {
			result[index].Label = locale.Text(definition.LabelKey)
		}
		result[index].Children = localizeNavigation(result[index].Children, locale)
	}
	return result
}

func knownPage(page PageID) bool {
	_, ok := LookupPage(page)
	return ok
}
