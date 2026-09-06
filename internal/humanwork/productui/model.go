package productui

import "github.com/monstercameron/hcm-next/internal/kernel/values"

// PageID identifies one stable product surface. The server resolves whether a
// page is discoverable before it includes the page in View.Navigation.
type PageID string

const (
	PageHome                   PageID = "home"
	PageMyself                 PageID = "myself"
	PageJourneys               PageID = "journeys"
	PageWork                   PageID = "work"
	PageHistory                PageID = "history"
	PagePeople                 PageID = "people"
	PagePerson                 PageID = "person"
	PageOrganization           PageID = "organization"
	PageInsights               PageID = "insights"
	PageAdmin                  PageID = "admin"
	PageWorkerIDs              PageID = "worker-ids"
	PageRoles                  PageID = "roles"
	PageOrganizationVisibility PageID = "organization-visibility"
	PageAppearance             PageID = "appearance"
	PageStudio                 PageID = "studio"
	PageHelp                   PageID = "help"
	PageSettings               PageID = "settings"
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

// ViewerProfile is the presentation identity associated with the admitted
// application principal. Principal remains the security identity; Viewer is
// the authorized worker-facing profile used for account UI.
type ViewerProfile struct {
	// PersonID is the authorized worker projection bound to the admitted
	// principal. It is presentation identity, not action authority.
	PersonID string
	Name     string
	Initials string
	PhotoURL string
	Role     string
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
	UseCount    int64
	LaunchHref  func(string) string
}

type StoredTablePreferences struct {
	PageSize        int
	Filters         map[string]string
	Sort, Direction string
}
type StoredUserPreferences struct {
	Version          int64
	Locale           string
	Accessibility    AccessibilityPreferences
	NavCollapsed     bool
	NavigationGroups map[PageID]bool
	FavoritePages    []PageID
	Tables           map[string]StoredTablePreferences
	WorkflowUses     map[string]int64
}

// WorkerIDPolicy is the organization-admin projection of human-facing worker
// number rules. NextSequence and IssuedCount are read-only allocation state.
type WorkerIDPolicy struct {
	Version, StartAt, NextSequence, IncrementBy, IssuedCount          int64
	Prefix, Suffix, Separator, YearFormat, CheckDigit, ExcludedRanges string
	SequenceDigits                                                    int
	ZeroPad, IncludeUnitCode                                          bool
	Previews                                                          []string
}

type OrganizationVisibilityPolicy struct {
	Version           int64
	RoleID            string
	Mode              string
	OrganizationUnits []string
}

type AccessRole struct {
	Version     int64
	ID          string
	Name        string
	Description string
	System      bool
	Active      bool
}

type WorkerRoleAssignment struct {
	Version   int64
	WorkerRef string
	RoleIDs   []string
}

// RolePagePermission is one role's page-level CRUD boundary. This client
// projection controls affordances; the service independently enforces every
// operation from authenticated role state.
type RolePagePermission struct {
	Version int64
	RoleID  string
	Page    PageID
	View    bool
	Create  bool
	Update  bool
	Delete  bool
}

// View is an already-authorized presentation projection. It contains no
// credential or raw sensitive record and grants no action authority.
type View struct {
	Page                   PageID
	Title                  string
	Subtitle               string
	Tenant                 string
	Principal              string
	Viewer                 ViewerProfile
	Scope                  string
	Roles                  []string
	LogoutHref             string
	Navigation             []NavItem
	Work                   []WorkItem
	People                 []Person
	PersonWorkflows        []PersonWorkflow
	SelectedWork           string
	SelectedPerson         string
	Query                  string
	PeoplePage             int
	PeoplePageSize         int
	PeopleTeam             string
	PeopleLocation         string
	PeopleSort             string
	PeopleDirection        string
	OrganizationView       string
	WorkflowQuery          string
	HistoryQuery           string
	HistoryOutcome         string
	HistoryPerson          string
	HistoryYear            string
	HistorySort            string
	HistoryDirection       string
	HistoryPage            int
	HistoryPageSize        int
	WorkflowUses           map[string]int64
	PreferenceVersion      int64
	AppearanceVersion      int64
	WorkerIDPolicy         WorkerIDPolicy
	OrganizationVisibility OrganizationVisibilityPolicy
	AccessRoles            []AccessRole
	RoleAssignments        []WorkerRoleAssignment
	RoleVisibilityPolicies []OrganizationVisibilityPolicy
	RolePagePermissions    []RolePagePermission
	EffectivePermissions   []RolePagePermission
	StoredPreferences      StoredUserPreferences
	Mode                   string
	WorkFilter             string
	JourneyID              string
	JourneyWorker          string
	JourneyMode            string
	NavCollapsed           bool
	MenuQuery              string
	FavoritePages          []PageID
	// NavigationGroupOpen contains the authenticated user's server-side
	// disclosure preferences. Missing entries retain the contextual default.
	NavigationGroupOpen        map[PageID]bool
	Locale                     LocaleContext
	Appearance                 CustomerTheme
	Accessibility              AccessibilityPreferences
	PreviewTheme               func(CustomerTheme)
	SaveTheme                  func(CustomerTheme)
	ResetTheme                 func()
	SaveWorkerIDPolicy         func(WorkerIDPolicy)
	SaveOrganizationVisibility func(OrganizationVisibilityPolicy)
	SaveAccessRole             func(AccessRole)
	SaveWorkerRoleAssignment   func(WorkerRoleAssignment)
	SaveRoleVisibility         func(OrganizationVisibilityPolicy)
	SaveRolePagePermission     func(RolePagePermission)
	PreviewAccessibility       func(AccessibilityPreferences)
	SaveAccessibility          func(AccessibilityPreferences)
	ResetAccessibility         func()
	Source                     string
	LoadError                  string
	// Loading is presentation state set only while the route's authorized
	// database-backed projection is resolving. It never implies authority or
	// substitutes empty records for an answer from the service.
	Loading bool
	// Refreshing keeps an already-authorized page projection mounted while a
	// newer projection resolves. This prevents fast filter, sort, and paging
	// requests from replacing useful content with a one-frame loading proxy.
	Refreshing bool
	// Navigate is installed by the WASM history router. A nil callback keeps
	// server rendering and tests as ordinary progressive-enhancement links.
	Navigate                  func(string)
	NavigateDebounced         func(string)
	CancelDebouncedNavigation func()
	HistoryNavigation         HistoryNavigationProps
}

// Can reports whether the resolved role grants an operation on a page. An
// empty permission projection is the compatibility path for cells that have
// not composed role access yet; only legacy view visibility is retained.
func (view View) Can(page PageID, action string) bool {
	for _, permission := range view.EffectivePermissions {
		if permission.Page != page {
			continue
		}
		switch action {
		case "view":
			return permission.View
		case "create":
			return permission.Create
		case "update":
			return permission.Update
		case "delete":
			return permission.Delete
		}
	}
	return len(view.EffectivePermissions) == 0 && action == "view" && PageVisible(page, view.Roles)
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

// ApplyRoleVisibility limits discoverability to the server-admitted roles.
// Transport authorization remains the enforcement boundary; this projection
// prevents navigation, favorites, and global search from advertising routes
// the active identity cannot open.
func ApplyRoleVisibility(view View, roles []string) View {
	// Start from a non-nil slice so an admitted identity with zero roles stays
	// distinguishable from NewView's unrestricted component-preview default.
	view.Roles = append([]string{}, roles...)
	view.Navigation = navigationForRoles(view.Locale, view.Roles)
	return view
}

// ApplyPagePermissions replaces static navigation visibility with the
// effective union of the employee's durable role grants.
func ApplyPagePermissions(view View, permissions []RolePagePermission) View {
	view.EffectivePermissions = append([]RolePagePermission(nil), permissions...)
	view.Navigation = navigationForPermissions(view.Locale, view.EffectivePermissions)
	return view
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
