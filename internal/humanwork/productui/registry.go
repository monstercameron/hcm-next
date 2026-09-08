package productui

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageDefinition is the stable application-level contract for one product
// surface. Routing, page identity, navigation eligibility, and rendering live
// together so new enterprise modules do not grow a second switch statement.
type PageDefinition struct {
	ID          PageID
	Route       string
	Label       string
	Icon        string
	Title       string
	Subtitle    string
	LabelKey    string
	TitleKey    string
	SubtitleKey string
	// SearchTerms are stable aliases and business-language concepts for the
	// navigation command palette. Label and localized subtitle are searched
	// automatically; these terms cover vocabulary customers commonly use.
	SearchTerms []string
	PrimaryNav  bool
	ParentNav   PageID
	RenderOrder int
	render      func(View) ui.Node
}

const RoleHCMAdmin = "hcm_admin"

// PageVisible reports product-route access for server-admitted roles. An
// empty role set receives only the safe shell baseline and cannot discover
// workforce or administrative surfaces.
func PageVisible(page PageID, roles []string) bool {
	if hasAnyProductRole(roles, RoleHCMAdmin, "comp_admin") {
		return true
	}
	switch page {
	case PageHome, PageHelp, PageSettings:
		return true
	case PageMyself, PageOrganization:
		return hasAnyProductRole(roles, "worker_self", "manager", "hr_partner", "hiring_manager", "payroll_manager")
	case PageJourneys, PageWork, PageHistory, PagePeople, PagePerson, PageHeadcount, PagePosition, PageRequisition, PageCandidates, PageCandidate, PageInsights:
		return hasAnyProductRole(roles, "manager", "hr_partner", "comp_admin", "hiring_manager", "payroll_manager")
	default:
		return false
	}
}

func hasAnyProductRole(roles []string, wanted ...string) bool {
	for _, role := range wanted {
		if hasProductRole(roles, role) {
			return true
		}
	}
	return false
}

func hasProductRole(roles []string, wanted string) bool {
	for _, role := range roles {
		if role == wanted {
			return true
		}
	}
	return false
}

func registeredPages() []PageDefinition {
	return []PageDefinition{
		{ID: PageHome, Route: "/workspace/app/home", Label: "Home", Icon: "home", Title: "Home", Subtitle: "Review live requests and keep your work moving.", LabelKey: "page.home.label", TitleKey: "page.home.title", SubtitleKey: "page.home.subtitle", SearchTerms: []string{"dashboard", "overview", "landing", "start"}, PrimaryNav: true, RenderOrder: 10, render: homePage},
		{ID: PageMyself, Route: "/workspace/app/myself", Label: "Myself", Icon: "people", Title: "Myself", Subtitle: "Your employment, organization, payroll, and workflow information.", LabelKey: "page.myself.label", TitleKey: "page.myself.title", SubtitleKey: "page.myself.subtitle", SearchTerms: []string{"me", "my profile", "self service", "employment", "payroll", "compensation", "salary", "payslip", "personal information"}, PrimaryNav: true, RenderOrder: 12, render: myselfPage},
		{ID: PageJourneys, Route: "/workspace/app/journeys", Label: "Journeys", Icon: "journeys", Title: "Journeys", Subtitle: "Start, follow, and complete governed employee workflows.", LabelKey: "page.journeys.label", TitleKey: "page.journeys.title", SubtitleKey: "page.journeys.subtitle", SearchTerms: []string{"workflow", "promotion", "request", "approval", "lifecycle"}, PrimaryNav: true, RenderOrder: 15, render: journeysPage},
		{ID: PageWork, Route: "/workspace/app/work", Label: "My Work", Icon: "work", Title: "My Work", Subtitle: "Live promotion journeys that need attention.", LabelKey: "page.work.label", TitleKey: "page.work.title", SubtitleKey: "page.work.subtitle", SearchTerms: []string{"tasks", "inbox", "queue", "assigned", "pending", "approvals"}, PrimaryNav: true, RenderOrder: 20, render: workPage},
		{ID: PageHistory, Route: "/workspace/app/history", Label: "Work History", Icon: "history", Title: "Workflow History", Subtitle: "Review completed, rejected, and failed workflow records.", LabelKey: "page.history.label", TitleKey: "page.history.title", SubtitleKey: "page.history.subtitle", SearchTerms: []string{"past", "completed", "rejected", "failed", "audit", "records"}, ParentNav: PageWork, RenderOrder: 25, render: historyPage},
		{ID: PagePeople, Route: "/workspace/app/people", Label: "People", Icon: "people", Title: "People", Subtitle: "Workers visible through the governed journey service.", LabelKey: "page.people.label", TitleKey: "page.people.title", SubtitleKey: "page.people.subtitle", SearchTerms: []string{"employees", "workers", "directory", "profiles", "staff", "team", "colleagues"}, PrimaryNav: true, RenderOrder: 30, render: peoplePage},
		{ID: PagePerson, Route: "/workspace/app/person", Label: "Person", Icon: "people", Title: "Person profile", Subtitle: "Worker facts and available governed workflows.", LabelKey: "page.person.label", TitleKey: "page.person.title", SubtitleKey: "page.person.subtitle", SearchTerms: []string{"employee", "worker", "profile", "employment"}, RenderOrder: 35, render: personPage},
		{ID: PageHeadcount, Route: "/workspace/app/headcount", Label: "Headcount", Icon: "people", Title: "Headcount requests", Subtitle: "Request headcount through the governed requisition service.", LabelKey: "page.headcount.label", TitleKey: "page.headcount.title", SubtitleKey: "page.headcount.subtitle", SearchTerms: []string{"headcount", "head count", "requisition", "hiring", "open roles"}, RenderOrder: 36, render: headcountPage},
		{ID: PagePosition, Route: "/workspace/app/position", Label: "Positions", Icon: "people", Title: "Position requests", Subtitle: "Request positions through the governed position service.", LabelKey: "page.position.label", TitleKey: "page.position.title", SubtitleKey: "page.position.subtitle", SearchTerms: []string{"positions", "roles", "job requisition", "openings", "vacancies"}, RenderOrder: 37, render: positionPage},
		{ID: PageRequisition, Route: "/workspace/app/requisition", Label: "Requisitions", Icon: "people", Title: "Requisition workspace", Subtitle: "Track requisitions through the governed requisition service.", LabelKey: "page.requisition.label", TitleKey: "page.requisition.title", SubtitleKey: "page.requisition.subtitle", SearchTerms: []string{"requisitions", "hiring workspace", "openings", "candidates", "interviews"}, RenderOrder: 38, render: requisitionPage},
		{ID: PageCandidates, Route: "/workspace/app/candidates", Label: "Candidates", Icon: "people", Title: "Candidate pipeline", Subtitle: "Follow candidates through the governed candidacy service.", LabelKey: "page.candidates.label", TitleKey: "page.candidates.title", SubtitleKey: "page.candidates.subtitle", SearchTerms: []string{"candidates", "pipeline", "applicants", "interviews", "hiring"}, RenderOrder: 39, render: candidatesPage},
		{ID: PageCandidate, Route: "/workspace/app/candidate", Label: "Candidate", Icon: "people", Title: "Candidate profile", Subtitle: "Candidate facts from the governed candidacy service.", LabelKey: "page.candidate.label", TitleKey: "page.candidate.title", SubtitleKey: "page.candidate.subtitle", SearchTerms: []string{"candidate", "applicant", "profile", "resume"}, RenderOrder: 40, render: candidatePage},
		{ID: PageOrganization, Route: "/workspace/app/organization", Label: "Organization", Icon: "organization", Title: "Organization", Subtitle: "Organization membership available from the live worker projection.", LabelKey: "page.organization.label", TitleKey: "page.organization.title", SubtitleKey: "page.organization.subtitle", SearchTerms: []string{"org chart", "departments", "teams", "structure", "hierarchy", "reporting"}, PrimaryNav: true, RenderOrder: 41, render: organizationPage},
		{ID: PageInsights, Route: "/workspace/app/insights", Label: "Insights", Icon: "insights", Title: "Insights", Subtitle: "Operational counts derived from live journey states.", LabelKey: "page.insights.label", TitleKey: "page.insights.title", SubtitleKey: "page.insights.subtitle", SearchTerms: []string{"analytics", "reports", "metrics", "trends", "workforce data"}, PrimaryNav: true, RenderOrder: 50, render: insightsPage},
		{ID: PageAdmin, Route: "/workspace/app/admin", Label: "Admin", Icon: "admin", Title: "Admin", Subtitle: "Published service capabilities and configuration availability.", LabelKey: "page.admin.label", TitleKey: "page.admin.title", SubtitleKey: "page.admin.subtitle", SearchTerms: []string{"administration", "configuration", "system", "capabilities", "manage"}, PrimaryNav: true, RenderOrder: 60, render: adminPage},
		{ID: PageWorkerIDs, Route: "/workspace/app/admin/worker-ids", Label: "Worker IDs", Icon: "people", Title: "Worker ID rules", Subtitle: "Configure how this organization issues unique worker numbers.", LabelKey: "page.worker_ids.label", TitleKey: "page.worker_ids.title", SubtitleKey: "page.worker_ids.subtitle", SearchTerms: []string{"worker number", "personnel number", "prefix", "sequence", "identifier", "numbering"}, ParentNav: PageAdmin, RenderOrder: 63, render: workerIDsPage},
		{ID: PageRoles, Route: "/workspace/app/admin/roles", Label: "Roles & access", Icon: "admin", Title: "Roles & access", Subtitle: "Create roles and assign one or more roles across the workforce.", LabelKey: "page.roles.label", TitleKey: "page.roles.title", SubtitleKey: "page.roles.subtitle", SearchTerms: []string{"authorization", "roles", "permissions", "workforce access", "assignment", "rbac"}, ParentNav: PageAdmin, RenderOrder: 64, render: rolesPage},
		{ID: PageOrganizationVisibility, Route: "/workspace/app/admin/organization-visibility", Label: "Organization visibility", Icon: "organization", Title: "Organization visibility", Subtitle: "Control which organization units each role can discover.", LabelKey: "page.organization_visibility.label", TitleKey: "page.organization_visibility.title", SubtitleKey: "page.organization_visibility.subtitle", SearchTerms: []string{"org chart access", "directory visibility", "role visibility", "allowlist", "denylist", "own team", "organization units"}, ParentNav: PageAdmin, RenderOrder: 65, render: organizationVisibilityPage},
		{ID: PageAppearance, Route: "/workspace/app/appearance", Label: "Brand & appearance", Icon: "palette", Title: "Brand & appearance", Subtitle: "Shape a consistent workspace identity with governed, accessible theme choices.", LabelKey: "page.appearance.label", TitleKey: "page.appearance.title", SubtitleKey: "page.appearance.subtitle", SearchTerms: []string{"branding", "theme", "colors", "logo", "dark mode", "styling", "shapes", "glyphs"}, ParentNav: PageAdmin, RenderOrder: 66, render: appearancePage},
		{ID: PageStudio, Route: "/workspace/app/studio", Label: "Experience Studio", Icon: "studio", Title: "Experience Studio", Subtitle: "Customer page configuration requires its governed service.", LabelKey: "page.studio.label", TitleKey: "page.studio.title", SubtitleKey: "page.studio.subtitle", SearchTerms: []string{"custom pages", "layout", "builder", "designer", "experience", "configuration"}, ParentNav: PageAdmin, RenderOrder: 70, render: studioPage},
		{ID: PageHelp, Route: "/workspace/app/help", Label: "Help", Icon: "help", Title: "Help center", Subtitle: "Guidance for the live promotion workflow.", LabelKey: "page.help.label", TitleKey: "page.help.title", SubtitleKey: "page.help.subtitle", SearchTerms: []string{"support", "guidance", "documentation", "docs", "assistance"}, RenderOrder: 80, render: helpPage},
		{ID: PageSettings, Route: "/workspace/app/settings", Label: "Settings", Icon: "settings", Title: "Settings", Subtitle: "Current authenticated session and available preferences.", LabelKey: "page.settings.label", TitleKey: "page.settings.title", SubtitleKey: "page.settings.subtitle", SearchTerms: []string{"preferences", "locale", "language", "accessibility", "account", "session"}, RenderOrder: 90, render: settingsPage},
	}
}

// PageDefinitions returns a copy so callers can inspect the page inventory
// without mutating the application registry.
func PageDefinitions() []PageDefinition {
	return registeredPages()
}

// LookupPage resolves the canonical definition for a stable page identity.
func LookupPage(id PageID) (PageDefinition, bool) {
	for _, definition := range registeredPages() {
		if definition.ID == id {
			return definition, true
		}
	}
	return PageDefinition{}, false
}

// LookupRoute keeps transport routing coupled to the canonical page registry,
// not to assumptions about PageID spelling.
func LookupRoute(route string) (PageDefinition, bool) {
	for _, definition := range registeredPages() {
		if definition.Route == route {
			return definition, true
		}
	}
	return PageDefinition{}, false
}

func renderPage(view View) (ui.Node, error) {
	definition, ok := LookupPage(view.Page)
	if !ok || definition.render == nil {
		return nil, fmt.Errorf("productui: unknown page %q", view.Page)
	}
	return definition.render(view), nil
}

func defaultNavigation(locale LocaleContext) []NavItem {
	return navigationFor(locale, nil)
}

func navigationForRoles(locale LocaleContext, roles []string) []NavItem {
	restricted := roles != nil
	return navigationFor(locale, func(page PageID) bool { return !restricted || PageVisible(page, roles) })
}

func navigationForPermissions(locale LocaleContext, permissions []RolePagePermission) []NavItem {
	allowed := make(map[PageID]bool, len(permissions))
	for _, permission := range permissions {
		allowed[permission.Page] = allowed[permission.Page] || permission.View
	}
	return navigationFor(locale, func(page PageID) bool { return allowed[page] })
}

// authorizedNavigationForRoles preserves the established role adapter while
// marking its result as an authoritative projection. Production adapters can
// replace this compatibility constructor with a resolver-owned projection;
// presentation never derives access after this boundary.
func authorizedNavigationForRoles(locale LocaleContext, roles []string) AuthorizedNavigationProjection {
	return authorizedNavigationProjection(locale, navigationForRoles(locale, roles), func(page PageID) bool { return PageVisible(page, roles) })
}

func authorizedNavigationForPermissions(locale LocaleContext, permissions []RolePagePermission) AuthorizedNavigationProjection {
	allowed := make(map[PageID]bool, len(permissions))
	for _, permission := range permissions {
		allowed[permission.Page] = allowed[permission.Page] || permission.View
	}
	return authorizedNavigationProjection(locale, navigationForPermissions(locale, permissions), func(page PageID) bool { return allowed[page] })
}

func authorizedNavigationProjection(locale LocaleContext, items []NavItem, supportAllowed func(PageID) bool) AuthorizedNavigationProjection {
	// Version one is the compatibility adapter's schema version. Production
	// resolvers supply their own positive version; zero is never an admitted
	// answer because it cannot be distinguished from an unversioned payload.
	projection := AuthorizedNavigationProjection{Version: 1}
	for _, item := range items {
		projection.Items = append(projection.Items, authorizedNavigationItem(item))
	}
	for _, page := range []PageID{PageHelp, PageSettings} {
		item, ok := navigationItemForPage(page, locale)
		if ok && supportAllowed(page) {
			projection.Support = append(projection.Support, authorizedNavigationItem(item))
		}
	}
	return projection
}

func authorizedNavigationItem(item NavItem) AuthorizedNavigationItem {
	result := AuthorizedNavigationItem{
		Page: item.Page, Label: item.Label, LabelKey: item.LabelKey, Description: item.Description,
		Keywords: append([]string(nil), item.Keywords...), Icon: item.Icon, Href: pageHref(item.Page), Count: item.Count, Authorized: true,
	}
	for _, child := range item.Children {
		result.Children = append(result.Children, authorizedNavigationItem(child))
	}
	return result
}

const (
	maxAuthorizedNavigationItems    = 64
	maxAuthorizedNavigationDepth    = 2
	maxAuthorizedNavigationText     = 256
	maxAuthorizedNavigationKeywords = 24
)

// ApplyNavigationProjection adopts a complete server answer. Validation is a
// safety boundary only; it does not grant access. Invalid answers remain
// authoritative and render no destinations, rather than reintroducing the
// registry's default catalogue.
func ApplyNavigationProjection(view View, projection AuthorizedNavigationProjection) View {
	// Validate the caller-owned graph before allocating or recursively copying
	// any of it. The validator charges each node before descending and rejects
	// excess depth, fan-out, and total size, so hostile graphs cannot turn this
	// display boundary into unbounded stack or heap work.
	if err := validateAuthorizedNavigationProjection(projection); err != nil {
		projection = AuthorizedNavigationProjection{Version: projection.Version}
	} else {
		projection = cloneAuthorizedNavigationProjection(projection, view.Locale)
	}
	view.NavigationProjection = &projection
	view.Navigation = navigationItemsFromProjection(projection.Items, view.Locale)
	view.NavigationSupport = navigationItemsFromProjection(projection.Support, view.Locale)
	return view
}

func cloneAuthorizedNavigationProjection(projection AuthorizedNavigationProjection, locale LocaleContext) AuthorizedNavigationProjection {
	copy := AuthorizedNavigationProjection{Version: projection.Version}
	copy.Items = make([]AuthorizedNavigationItem, 0, len(projection.Items))
	for _, item := range projection.Items {
		copy.Items = append(copy.Items, cloneAuthorizedNavigationItem(item, locale))
	}
	copy.Support = make([]AuthorizedNavigationItem, 0, len(projection.Support))
	for _, item := range projection.Support {
		copy.Support = append(copy.Support, cloneAuthorizedNavigationItem(item, locale))
	}
	return copy
}

func cloneAuthorizedNavigationItem(item AuthorizedNavigationItem, locale LocaleContext) AuthorizedNavigationItem {
	definition, _ := LookupPage(item.Page)
	copy := AuthorizedNavigationItem{
		Page: item.Page, Label: locale.Text(item.LabelKey), LabelKey: item.LabelKey,
		Description: locale.Text(definition.SubtitleKey), Keywords: append([]string(nil), definition.SearchTerms...),
		Icon: definition.Icon, Href: definition.Route, Count: item.Count, Authorized: true,
	}
	copy.Children = make([]AuthorizedNavigationItem, 0, len(item.Children))
	for _, child := range item.Children {
		copy.Children = append(copy.Children, cloneAuthorizedNavigationItem(child, locale))
	}
	return copy
}

func navigationItemsFromProjection(items []AuthorizedNavigationItem, locale LocaleContext) []NavItem {
	result := make([]NavItem, 0, len(items))
	for _, item := range items {
		result = append(result, navigationItemFromProjection(item, locale))
	}
	return result
}

func navigationItemFromProjection(item AuthorizedNavigationItem, locale LocaleContext) NavItem {
	definition, _ := LookupPage(item.Page)
	result := NavItem{
		Page: item.Page, Label: locale.Text(item.LabelKey), LabelKey: item.LabelKey,
		Description: locale.Text(definition.SubtitleKey), Keywords: append([]string(nil), definition.SearchTerms...),
		Icon: definition.Icon, Href: definition.Route, Count: item.Count,
	}
	for _, child := range item.Children {
		result.Children = append(result.Children, navigationItemFromProjection(child, locale))
	}
	return result
}

func validateAuthorizedNavigationProjection(projection AuthorizedNavigationProjection) error {
	if projection.Version <= 0 {
		return fmt.Errorf("productui: navigation projection has no positive version")
	}
	if len(projection.Items) > maxAuthorizedNavigationItems || len(projection.Support) > maxAuthorizedNavigationItems-len(projection.Items) {
		return fmt.Errorf("productui: navigation projection exceeds item limit")
	}
	seen := make(map[PageID]bool)
	overviews := make(map[PageID]bool)
	count := 0
	for _, item := range projection.Items {
		if err := validateAuthorizedNavigationItem(item, true, 0, "", seen, overviews, &count); err != nil {
			return err
		}
	}
	for _, item := range projection.Support {
		if err := validateAuthorizedNavigationItem(item, false, 0, "", seen, overviews, &count); err != nil {
			return err
		}
	}
	return nil
}

func validateAuthorizedNavigationItem(item AuthorizedNavigationItem, primary bool, depth int, parent PageID, seen, overviews map[PageID]bool, count *int) error {
	if depth > maxAuthorizedNavigationDepth || *count >= maxAuthorizedNavigationItems {
		return fmt.Errorf("productui: navigation projection exceeds structural limit")
	}
	(*count)++
	if len(item.Children) > maxAuthorizedNavigationItems-*count {
		return fmt.Errorf("productui: navigation projection exceeds item limit")
	}
	definition, ok := LookupPage(item.Page)
	if !ok || !item.Authorized || item.Count < 0 || len(item.Keywords) > maxAuthorizedNavigationKeywords {
		return fmt.Errorf("productui: malformed navigation projection item")
	}
	if !validNavigationText(item.Label, true) || !validNavigationText(item.LabelKey, true) || !validNavigationText(item.Icon, true) ||
		!validNavigationText(item.Description, false) {
		return fmt.Errorf("productui: unsafe navigation projection text")
	}
	if item.Icon != definition.Icon || !validNavigationHref(item.Page, item.Href) {
		return fmt.Errorf("productui: malformed navigation projection route")
	}
	for _, keyword := range item.Keywords {
		if !validNavigationText(keyword, true) {
			return fmt.Errorf("productui: malformed navigation projection keyword")
		}
	}
	if seen[item.Page] {
		// Only a direct, leaf overview may repeat its containing group's page,
		// and it may do so once. A repeated leaf elsewhere cannot smuggle an
		// unrelated route through duplicate handling or evade the item budget.
		if depth != 1 || parent == "" || item.Page != parent || len(item.Children) > 0 || overviews[item.Page] || item.LabelKey != navigationOverviewLabelKey(item.Page) {
			return fmt.Errorf("productui: duplicate navigation projection page %q", item.Page)
		}
		overviews[item.Page] = true
		return nil
	}
	if item.LabelKey != definition.LabelKey {
		return fmt.Errorf("productui: navigation projection label key is not canonical")
	}
	if parent != "" {
		if definition.ParentNav != parent {
			return fmt.Errorf("productui: navigation projection child has wrong parent")
		}
	} else if primary {
		if !definition.PrimaryNav {
			return fmt.Errorf("productui: navigation projection page is not primary navigation")
		}
	} else if item.Page != PageHelp && item.Page != PageSettings || len(item.Children) > 0 {
		return fmt.Errorf("productui: navigation projection page is not in its allowed region")
	}
	seen[item.Page] = true
	for _, child := range item.Children {
		if err := validateAuthorizedNavigationItem(child, false, depth+1, item.Page, seen, overviews, count); err != nil {
			return err
		}
	}
	return nil
}

func validNavigationHref(page PageID, href string) bool {
	if href == "" || strings.TrimSpace(href) != href || len(href) > maxAuthorizedNavigationText || !validNavigationText(href, true) {
		return false
	}
	parsed, err := url.Parse(href)
	definition, ok := LookupPage(page)
	return err == nil && ok && !parsed.IsAbs() && parsed.Opaque == "" && parsed.Scheme == "" && parsed.User == nil && parsed.Host == "" &&
		parsed.Path == definition.Route && parsed.RawPath == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validNavigationText(value string, required bool) bool {
	if value == "" {
		return !required
	}
	if len(value) > maxAuthorizedNavigationText || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func navigationOverviewLabelKey(page PageID) string {
	switch page {
	case PageWork:
		return "nav.work_queue"
	case PageAdmin:
		return "nav.admin_overview"
	default:
		return "nav.overview"
	}
}

func navigationFor(locale LocaleContext, visible func(PageID) bool) []NavItem {
	pages := registeredPages()
	items := make([]NavItem, 0, len(pages))
	indexes := make(map[PageID]int)
	for _, definition := range pages {
		if !definition.PrimaryNav || visible != nil && !visible(definition.ID) {
			continue
		}
		item := navigationItemFromDefinition(definition, locale)
		items = append(items, item)
		indexes[definition.ID] = len(items) - 1
	}
	for _, definition := range pages {
		if definition.ParentNav == "" || visible != nil && !visible(definition.ID) {
			continue
		}
		index, ok := indexes[definition.ParentNav]
		if !ok {
			continue
		}
		parent := &items[index]
		if len(parent.Children) == 0 {
			label := locale.Text("nav.overview")
			switch parent.Page {
			case PageWork:
				label = locale.Text("nav.work_queue")
			case PageAdmin:
				label = locale.Text("nav.admin_overview")
			}
			labelKey := navigationOverviewLabelKey(parent.Page)
			overview := NavItem{Page: parent.Page, Label: label, LabelKey: labelKey, Description: parent.Description, Keywords: append([]string(nil), parent.Keywords...), Icon: parent.Icon}
			parent.Children = append(parent.Children, overview)
		}
		parent.Children = append(parent.Children, navigationItemFromDefinition(definition, locale))
	}
	return items
}

func navigationItemFromDefinition(definition PageDefinition, locale LocaleContext) NavItem {
	return NavItem{
		Page: definition.ID, Label: locale.Text(definition.LabelKey), LabelKey: definition.LabelKey,
		Description: locale.Text(definition.SubtitleKey), Keywords: append([]string(nil), definition.SearchTerms...), Icon: definition.Icon,
	}
}

func pageHref(page PageID) string {
	if definition, ok := LookupPage(page); ok {
		return definition.Route
	}
	return "/workspace/app/home"
}

// Path returns the canonical history-router path for a product page.
func Path(page PageID) string { return pageHref(page) }

// JourneyDetailHref returns a software-routed link to one journey while
// retaining the reader's navigation preferences.
func JourneyDetailHref(view View, intentID string) string {
	return statefulHref(view, PageJourneys, "journey", intentID)
}

// JourneyProposalHref returns a software-routed link to the focused workflow
// launcher for workerRef while retaining navigation preferences.
func JourneyProposalHref(view View, workerRef string) string {
	return statefulHref(view, PageJourneys, "mode", "new", "worker", workerRef)
}

func statefulHref(view View, page PageID, keyValues ...string) string {
	values := url.Values{}
	if view.NavCollapsed {
		values.Set("nav", "collapsed")
	}
	setMenuAddressState(values, view)
	for index := 0; index+1 < len(keyValues); index += 2 {
		if keyValues[index+1] != "" {
			values.Set(keyValues[index], keyValues[index+1])
		}
	}
	href := pageHref(page)
	if query := values.Encode(); query != "" {
		return href + "?" + query
	}
	return href
}

// withExplicitEmptyQuery distinguishes a deliberate clear from an absent
// address value. Absence asks the server-backed preference layer for its
// stored default; an encoded empty value replaces that stored default.
func withExplicitEmptyQuery(href string, names ...string) string {
	return withExplicitQueryValue(href, names, "")
}

func withExplicitQueryValue(href string, names []string, value string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	values := parsed.Query()
	for _, name := range names {
		values.Set(name, value)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func setMenuAddressState(values url.Values, view View) {
	if locale := view.Locale.normalized(); locale.Resolved != DefaultProductLocale || locale.Requested != "" {
		values.Set("locale", locale.Resolved)
	}
	if view.MenuQuery != "" {
		values.Set("menu_q", view.MenuQuery)
	}
	if favorites := authorizedFavoritePages(view.Navigation, view.FavoritePages); len(favorites) > 0 {
		pages := make([]string, 0, len(favorites))
		for _, page := range favorites {
			pages = append(pages, string(page))
		}
		values.Set("favorites", strings.Join(pages, ","))
	}
}
