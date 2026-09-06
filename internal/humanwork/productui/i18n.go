package productui

import (
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/experience/localize"
	"golang.org/x/text/language"
)

const (
	DefaultProductLocale  = "en-US"
	productCatalogVersion = "product-ui.v1"
)

// LocaleFallback makes an unsupported locale request visible without treating
// language as legal, payroll, tax, or authorization authority.
type LocaleFallback string

const (
	LocaleFallbackNone        LocaleFallback = ""
	LocaleFallbackUnsupported LocaleFallback = "unsupported_locale"
)

// LocaleContext is immutable presentation state shared by every component.
// It deliberately carries no jurisdiction or policy authority.
type LocaleContext struct {
	Requested      string
	Resolved       string
	TimeZone       string
	Calendar       string
	CatalogVersion string
	Direction      localize.Direction
	Fallback       LocaleFallback
}

// I18nProps is embedded in component props that own product copy. The zero
// value intentionally resolves to the reviewed English catalog, keeping
// isolated component tests and progressive SSR deterministic.
type I18nProps struct{ Locale LocaleContext }

func (p I18nProps) Text(key string, vars ...map[string]string) string {
	return p.Locale.Text(key, vars...)
}

func ResolveProductLocale(requested string) LocaleContext {
	requested = strings.ReplaceAll(strings.TrimSpace(requested), "_", "-")
	if requested != "" {
		if tag, err := language.Parse(requested); err == nil && tag != language.Und {
			requested = tag.String()
		}
	}
	locale := requested
	fallback := LocaleFallbackNone
	if locale == "" {
		locale = DefaultProductLocale
	}
	if _, ok := productMessages[locale]; !ok {
		locale = DefaultProductLocale
		fallback = LocaleFallbackUnsupported
	}
	return LocaleContext{
		Requested: requested, Resolved: locale, TimeZone: "UTC", Calendar: "gregorian",
		CatalogVersion: productCatalogVersion, Direction: localize.DirectionFor(locale), Fallback: fallback,
	}
}

func SupportedProductLocales() []string { return []string{"en-US", "de-DE", "ar"} }

// MissingProductTranslations returns stable semantic keys that will use the
// reviewed English fallback for locale. This makes partial catalog rollout
// observable to tests and telemetry instead of silently hiding it.
func MissingProductTranslations(locale string) []string {
	context := ResolveProductLocale(locale)
	if context.Resolved == DefaultProductLocale {
		return nil
	}
	localized := productMessages[context.Resolved]
	missing := make([]string, 0)
	for key := range productMessages[DefaultProductLocale] {
		if _, ok := localized[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func (c LocaleContext) normalized() LocaleContext {
	if c.Resolved == "" || c.CatalogVersion == "" {
		return ResolveProductLocale(c.Requested)
	}
	if c.TimeZone == "" {
		c.TimeZone = "UTC"
	}
	if c.Calendar == "" {
		c.Calendar = "gregorian"
	}
	if c.Direction == "" {
		c.Direction = localize.DirectionFor(c.Resolved)
	}
	return c
}

// Text resolves one stable semantic message key. Unknown keys stay visible so
// an untranslated control cannot silently ship as an empty accessible name.
func (c LocaleContext) Text(key string, vars ...map[string]string) string {
	c = c.normalized()
	options := localize.ResolveOptions{Fallback: []string{DefaultProductLocale}}
	if len(vars) > 0 {
		options.Vars = vars[0]
	}
	result, err := productMessageRegistry.Resolve(localize.Context{
		Locale: c.Resolved, TimeZone: c.TimeZone, Calendar: c.Calendar, CatalogVersion: c.CatalogVersion,
	}, key, options)
	if err != nil {
		return "⟦" + key + "⟧"
	}
	return result.Text
}

func (c LocaleContext) FormatDate(value time.Time) string {
	c = c.normalized()
	formatted, err := localize.FormatDate(localize.Context{Locale: c.Resolved, TimeZone: c.TimeZone, Calendar: c.Calendar, CatalogVersion: c.CatalogVersion}, value)
	if err != nil {
		return value.UTC().Format("2006-01-02")
	}
	return formatted
}

func (c LocaleContext) FormatNumber(decimal string, fraction int) string {
	c = c.normalized()
	formatted, err := localize.FormatNumber(c.Resolved, decimal, fraction)
	if err != nil {
		return decimal
	}
	return formatted
}

func (c LocaleContext) FormatMoney(decimal, currency string, fraction int) string {
	c = c.normalized()
	// Preserve the established en-US product projection while other locales
	// use the reviewed deterministic formatter. The value remains exact text.
	if c.Resolved == DefaultProductLocale {
		return strings.TrimSpace(currency + " " + decimal)
	}
	formatted, err := localize.FormatMoney(c.Resolved, decimal, currency, fraction)
	if err != nil {
		return strings.TrimSpace(currency + " " + decimal)
	}
	return formatted
}

func (c LocaleContext) Plural(key string, count int64) string {
	c = c.normalized()
	n := big.NewRat(count, 1)
	result, err := productMessageRegistry.Resolve(localize.Context{Locale: c.Resolved, TimeZone: c.TimeZone, Calendar: c.Calendar, CatalogVersion: c.CatalogVersion}, key, localize.ResolveOptions{
		Count: n, Fallback: []string{DefaultProductLocale}, Vars: map[string]string{"count": c.FormatNumber(n.RatString(), 0)},
	})
	if err != nil {
		return "⟦" + key + "⟧"
	}
	return result.Text
}

var productMessages = map[string]map[string]localize.Message{
	"en-US": {
		"common.previous": {Text: "Previous"}, "common.next": {Text: "Next"}, "common.not_reported": {Text: "Not reported"}, "common.not_disclosed": {Text: "Not disclosed"},
		"shell.skip_main":           {Text: "Skip to main content"},
		"shell.search_employees":    {Text: "Search employees"},
		"shell.work_overview":       {Text: "Work overview"},
		"shell.open_work":           {Text: "Open My Work"},
		"shell.acting_self":         {Text: "Acting as yourself"},
		"shell.live_source":         {Text: "Live source · {source}"},
		"shell.authenticated_scope": {Text: "Authenticated scope"},
		"shell.no_source":           {Text: "No service response"},
		"shell.live_unavailable":    {Text: "Live data unavailable"},
		"shell.locale":              {Text: "Language"},
		"shell.locale_en":           {Text: "English (US)"},
		"shell.locale_de":           {Text: "Deutsch"},
		"shell.locale_ar":           {Text: "العربية"},
		"shell.connecting":          {Text: "Connecting to HCM Next"},
		"shell.loading_authorized":  {Text: "Loading authorized data from the live cell…"},
		"shell.page_loaded":         {Text: "{title} page loaded"},
		"shell.work_count":          {Plural: map[string]string{"one": "{count} promotion journey is visible in this scope.", "other": "{count} promotion journeys are visible in this scope."}},
		"nav.collapse":              {Text: "Collapse navigation"}, "nav.expand": {Text: "Expand navigation"},
		"nav.favorites": {Text: "Favorites"}, "nav.all": {Text: "All navigation"}, "nav.none": {Text: "No menus match"},
		"nav.main": {Text: "Main"}, "nav.support": {Text: "Support and preferences"}, "nav.workspace": {Text: "Workspace navigation"},
		"nav.filter": {Text: "Filter navigation menu"}, "nav.filter_placeholder": {Text: "Filter menu"}, "nav.filter_apply": {Text: "Apply menu filter"}, "nav.filter_clear": {Text: "Clear menu filter"},
		"nav.favorite_add": {Text: "Add {label} to favorites"}, "nav.favorite_remove": {Text: "Remove {label} from favorites"},
		"nav.work_queue": {Text: "Work queue"}, "nav.admin_overview": {Text: "Admin overview"}, "nav.overview": {Text: "Overview"},
		"page.home.label": {Text: "Home"}, "page.home.title": {Text: "Home"}, "page.home.subtitle": {Text: "Review live requests and keep your work moving."},
		"page.home.greeting":  {Text: "Good morning, {name}."},
		"page.journeys.label": {Text: "Journeys"}, "page.journeys.title": {Text: "Journeys"}, "page.journeys.subtitle": {Text: "Start, follow, and complete governed employee workflows."},
		"page.work.label": {Text: "My Work"}, "page.work.title": {Text: "My Work"}, "page.work.subtitle": {Text: "Live promotion journeys that need attention."},
		"page.history.label": {Text: "Work History"}, "page.history.title": {Text: "Workflow History"}, "page.history.subtitle": {Text: "Review completed, rejected, and failed workflow records."},
		"page.people.label": {Text: "People"}, "page.people.title": {Text: "People"}, "page.people.subtitle": {Text: "Workers visible through the governed journey service."},
		"page.person.label": {Text: "Person"}, "page.person.title": {Text: "Person profile"}, "page.person.subtitle": {Text: "Worker facts and available governed workflows."},
		"page.organization.label": {Text: "Organization"}, "page.organization.title": {Text: "Organization"}, "page.organization.subtitle": {Text: "Organization membership available from the live worker projection."},
		"page.insights.label": {Text: "Insights"}, "page.insights.title": {Text: "Insights"}, "page.insights.subtitle": {Text: "Operational counts derived from live journey states."},
		"page.admin.label": {Text: "Admin"}, "page.admin.title": {Text: "Admin"}, "page.admin.subtitle": {Text: "Published service capabilities and configuration availability."},
		"page.appearance.label": {Text: "Brand & appearance"}, "page.appearance.title": {Text: "Brand & appearance"}, "page.appearance.subtitle": {Text: "Shape a consistent workspace identity with governed, accessible theme choices."},
		"page.studio.label": {Text: "Experience Studio"}, "page.studio.title": {Text: "Experience Studio"}, "page.studio.subtitle": {Text: "Customer page configuration requires its governed service."},
		"page.help.label": {Text: "Help"}, "page.help.title": {Text: "Help center"}, "page.help.subtitle": {Text: "Guidance for the live promotion workflow."},
		"page.settings.label": {Text: "Settings"}, "page.settings.title": {Text: "Settings"}, "page.settings.subtitle": {Text: "Current authenticated session and available preferences."},
		"work.empty_title": {Text: "No work in this view"}, "work.empty_detail": {Text: "Try another filter or return to all work."},
		"work.collection_label": {Text: "Promotion journeys"}, "work.filter_label": {Text: "Filter work"}, "work.selected_label": {Text: "Selected assignment"}, "work.selected_summary": {Text: "Selected work summary"},
		"work.promotion_journeys": {Text: "Promotion journeys"}, "work.all": {Text: "Open work"}, "work.awaiting": {Text: "Awaiting approval"}, "work.blocked": {Text: "Blocked"}, "work.past": {Text: "Past workflows"}, "work.item_count": {Plural: map[string]string{"one": "{count} item", "other": "{count} items"}},
		"work.authorized": {Text: "Your authorized work"}, "work.view": {Text: "View My Work →"}, "work.nothing_selected": {Text: "Nothing selected"}, "work.nothing_detail": {Text: "Choose another filter or return to all assigned work."}, "work.show_all": {Text: "Show all work"},
		"work.server_proposal": {Text: "Server-reported proposal"}, "work.effective_date": {Text: "Effective date"}, "work.current_base": {Text: "Current base"}, "work.proposed_base": {Text: "Proposed base"}, "work.journey_id": {Text: "Journey ID"}, "work.open_journey": {Text: "Open live journey"},
		"people.filter_placeholder": {Text: "Filter by name, role, team, location, or worker number"}, "people.filter_aria": {Text: "Filter employees"},
		"people.filter": {Text: "Filter"}, "people.clear": {Text: "Clear"}, "people.find": {Text: "Find an employee"},
		"people.all_teams": {Text: "All teams"}, "people.all_locations": {Text: "All locations"}, "people.team_aria": {Text: "Filter employees by team"}, "people.location_aria": {Text: "Filter employees by location"}, "people.sort_by": {Text: "Sort by"},
		"people.table_aria": {Text: "Authorized people"}, "people.column.person": {Text: "Person"}, "people.column.role": {Text: "Role"}, "people.column.team": {Text: "Team"}, "people.column.manager": {Text: "Manager"}, "people.column.location": {Text: "Location"}, "people.column.actions": {Text: "Actions"},
		"people.promote": {Text: "Promote"}, "people.promote_aria": {Text: "Start a promotion for {name}"},
		"people.pages": {Text: "People pages"}, "people.range": {Text: "{first}–{last} of {total}"}, "people.page_count": {Text: "Page {page} of {pages}"},
		"people.empty_title": {Text: "No people found"}, "people.empty_detail": {Text: "Try a name, role, team, location, worker number, or job code within your current scope."}, "people.clear_filter": {Text: "Clear filter"},
		"people.scope": {Text: "Authorized worker directory · {scope}"}, "people.filtered_count": {Text: "{filtered} of {total} people"}, "people.count": {Plural: map[string]string{"one": "{count} person", "other": "{count} people"}},
		"person.back": {Text: "← Back to People"}, "person.unavailable": {Text: "Person not available"}, "person.unavailable_detail": {Text: "This person is not present in the worker records visible to your current scope."}, "person.return_directory": {Text: "Return to the directory"},
		"person.summary": {Text: "Person summary"}, "person.worker_profile": {Text: "Worker profile"}, "person.source": {Text: "Source · {source}"},
		"person.employment_details": {Text: "Employment details"}, "person.employment_detail": {Text: "Current server-reported worker facts."}, "person.sensitive": {Text: "Sensitive"},
		"person.private_data": {Text: "Private data"}, "person.private_detail": {Text: "Only identifiers authorized for this purpose are available. Contact, government ID, banking, and home-address data are not exposed by the Journey service."},
		"person.visible_scope": {Text: "Visible in scope"}, "person.employment_overview": {Text: "Employment overview"}, "person.employment_overview_detail": {Text: "Core employment facts from the authorized worker record."},
		"person.worker_number": {Text: "Worker number"}, "person.job_code": {Text: "Job code"}, "person.job_level": {Text: "Job level"}, "person.hire_date": {Text: "Hire date"}, "person.employment_type": {Text: "Employment type"}, "person.time_type": {Text: "Time type"}, "person.record_source": {Text: "Record source"}, "person.record_created": {Text: "Record created"},
		"person.organization": {Text: "Organization"}, "person.organization_detail": {Text: "Reporting line, placement, and organizational context."}, "person.organization_unit": {Text: "Organization unit"}, "person.manager": {Text: "Manager"}, "person.position_id": {Text: "Position ID"}, "person.work_location": {Text: "Work location"}, "person.company": {Text: "Company"}, "person.business_unit": {Text: "Business unit"}, "person.cost_center": {Text: "Cost center"}, "person.work_arrangement": {Text: "Work arrangement"},
		"person.compensation": {Text: "Compensation"}, "person.compensation_detail": {Text: "Compensation facts disclosed for the current purpose."}, "person.base_pay": {Text: "Base pay"}, "person.bonus_target": {Text: "Bonus target"}, "person.pay_zone": {Text: "Pay zone"}, "person.pay_frequency": {Text: "Pay frequency"},
		"person.personal_information": {Text: "Personal information"}, "person.personal_hidden": {Text: "Personal identifiers · hidden by default"}, "person.restricted": {Text: "Restricted"}, "person.legal_name": {Text: "Legal name"}, "person.preferred_name": {Text: "Preferred name"}, "person.worker_id": {Text: "Internal worker ID"}, "person.worker_ref": {Text: "Stable worker reference"}, "person.history_detail": {Text: "Completed, rejected, and failed workflows recorded for {name}."},
		"workflow.none": {Text: "No matching workflows"}, "workflow.none_detail": {Text: "Try a workflow name, category, or outcome."}, "workflow.start": {Text: "Start a workflow"}, "workflow.choose": {Text: "Choose a governed workflow for {name}."},
		"workflow.available_aria": {Text: "Available workflows"}, "workflow.available_count": {Plural: map[string]string{"one": "{count} available", "other": "{count} available"}},
		"workflow.filter_placeholder": {Text: "Filter workflows"}, "workflow.filter_aria": {Text: "Filter available workflows"}, "workflow.find": {Text: "Find a workflow"}, "workflow.filter": {Text: "Filter"}, "workflow.start_named": {Text: "Start {name}"},
		"history.none": {Text: "No workflow records found"}, "history.record": {Text: "Record"}, "history.search_aria": {Text: "Search workflow history"}, "history.outcome_aria": {Text: "Filter workflow history by outcome"},
		"history.none_detail": {Text: "No past workflows match this view."}, "history.search_placeholder": {Text: "Search person, workflow, or change"},
		"history.all_people": {Text: "All employees"}, "history.any_year": {Text: "Any effective year"}, "history.person_aria": {Text: "Filter workflow history by employee"}, "history.year_aria": {Text: "Filter workflow history by effective year"},
		"history.all_outcomes": {Text: "All outcomes"}, "history.completed": {Text: "Completed"}, "history.rejected": {Text: "Rejected"}, "history.failed": {Text: "Failed"}, "history.apply": {Text: "Apply filters"}, "history.find": {Text: "Find past workflows"}, "history.clear": {Text: "Clear filters"}, "history.open": {Text: "Open record"},
		"history.closed": {Text: "Closed · {value}"}, "history.effective": {Text: "Effective · {value}"},
		"history.global_title": {Text: "Global workflow history"}, "history.global_detail": {Text: "Review terminal workflow outcomes across every worker visible to your current scope."}, "history.promotion": {Text: "Promotion"}, "history.empty_terminal": {Text: "Completed, rejected, and failed workflows will appear here after the service records a terminal outcome."}, "history.search_person_placeholder": {Text: "Search workflow, change, or outcome"}, "history.filtered_count": {Text: "{filtered} of {total} records"}, "history.count": {Plural: map[string]string{"one": "{count} record", "other": "{count} records"}},
		"appearance.intro_title": {Text: "Make the workspace feel like your organization"}, "appearance.intro_detail": {Text: "Choose a coordinated visual system for every product page. Accessibility colors, focus indicators, and workflow status meanings remain protected."}, "appearance.tenant": {Text: "Tenant appearance"},
		"appearance.color_mode": {Text: "Color mode"}, "appearance.color_mode_help": {Text: "Follow the device setting or choose a consistent light or dark workspace."},
		"appearance.color_mode_system": {Text: "Use system setting"}, "appearance.color_mode_system_help": {Text: "Follow this device and update automatically"}, "appearance.color_mode_light": {Text: "Light"}, "appearance.color_mode_light_help": {Text: "Use the light workspace on this device"}, "appearance.color_mode_dark": {Text: "Dark"}, "appearance.color_mode_dark_help": {Text: "Use the low-light workspace on this device"},
		"appearance.brand_signature": {Text: "Brand signature"}, "appearance.brand_help": {Text: "Give the workspace a recognizable customer identity with a governed logo and resilient text fallback."}, "appearance.workspace_name": {Text: "Workspace name"}, "appearance.workspace_name_help": {Text: "Accessible name and fallback shown in the global product header"}, "appearance.short_mark": {Text: "Short mark"}, "appearance.short_mark_help": {Text: "Compact-navigation fallback; up to three letters, numbers, or &"}, "appearance.company_logo": {Text: "Company logo asset"}, "appearance.company_logo_help": {Text: "Optional allowlisted image under /workspace/assets/. Leave blank to use the text signature."}, "appearance.company_logo_placeholder": {Text: "/workspace/assets/company-logo.svg"},
		"appearance.palette": {Text: "Color palette"}, "appearance.palette_help": {Text: "A coordinated accessible palette updates navigation, actions, supporting surfaces, and text."},
		"appearance.shape": {Text: "Surface shape"}, "appearance.shape_help": {Text: "Choose the corner character shared by controls, cards, panels, and previews."},
		"appearance.density": {Text: "Information density"}, "appearance.density_help": {Text: "Tune workspace spacing without shrinking readable type or interaction targets."},
		"appearance.glyphs": {Text: "Glyph set"}, "appearance.glyphs_help": {Text: "Select the weight and terminal style used by navigation and product symbols."},
		"appearance.typeface": {Text: "Typography character"}, "appearance.typeface_help": {Text: "Choose the typographic voice used throughout the workspace without loading third-party font assets."},
		"appearance.navigation": {Text: "Navigation treatment"}, "appearance.navigation_help": {Text: "Decide how prominently the primary brand color appears in the workspace frame."},
		"appearance.motion": {Text: "Motion"}, "appearance.motion_help": {Text: "Set the workspace pace. System reduced-motion preferences always take priority."},
		"appearance.preview": {Text: "Workspace preview"}, "appearance.preview_help": {Text: "Selections preview immediately across the product shell."}, "appearance.preview_aria": {Text: "Theme preview and actions"},
		"appearance.protected": {Text: "Protected: focus visibility, contrast thresholds, workflow status colors, and reduced-motion preferences."}, "appearance.save": {Text: "Save appearance"}, "appearance.restore": {Text: "Restore defaults"}, "appearance.status": {Text: "Changes are saved for this browser until the tenant BrandPack service is published."},
		"accessibility.title": {Text: "Accessibility preferences"}, "accessibility.description": {Text: "Adjust the workspace for comfortable reading and interaction. These choices apply to every page in this browser."},
		"accessibility.text_size": {Text: "Text size"}, "accessibility.text_size_help": {Text: "Increase product text without hiding information or changing browser zoom."}, "accessibility.text_standard": {Text: "Standard"}, "accessibility.text_large": {Text: "Large"}, "accessibility.text_larger": {Text: "Larger"},
		"accessibility.contrast": {Text: "Contrast"}, "accessibility.contrast_help": {Text: "Use the operating-system preference or strengthen visual boundaries."}, "accessibility.system": {Text: "Use system setting"}, "accessibility.contrast_system_help": {Text: "Follow the device contrast preference."}, "accessibility.contrast_more": {Text: "More contrast"}, "accessibility.contrast_more_help": {Text: "Strengthen text, controls, borders, and state indicators."},
		"accessibility.motion": {Text: "Motion"}, "accessibility.motion_help": {Text: "Reduce non-essential movement across pages and workflows."}, "accessibility.motion_system_help": {Text: "Follow the device reduced-motion preference."}, "accessibility.motion_limited": {Text: "Limited motion"}, "accessibility.motion_limited_help": {Text: "Keep brief state cues while removing travel, stagger, and decorative movement."}, "accessibility.motion_reduce": {Text: "Reduce motion"}, "accessibility.motion_reduce_help": {Text: "Disable animation and shorten state transitions."},
		"accessibility.links": {Text: "Link visibility"}, "accessibility.links_help": {Text: "Choose whether inline links always have a non-color cue."}, "accessibility.links_standard": {Text: "Standard links"}, "accessibility.links_standard_help": {Text: "Use context, focus, and hover indicators."}, "accessibility.links_underlined": {Text: "Always underline links"}, "accessibility.links_underlined_help": {Text: "Underline inline links throughout the product."},
		"accessibility.save": {Text: "Save preferences"}, "accessibility.reset": {Text: "Use defaults"}, "accessibility.status": {Text: "Preferences are stored in this browser until the governed account-preference service is available."},
		"settings.locale_title": {Text: "Language & region"}, "settings.locale_description": {Text: "Choose the language used for navigation, labels, dates, numbers, and currency formatting."},
		"settings.locale_option_detail": {Text: "{code} · {direction}"}, "settings.locale_ltr": {Text: "Left to right"}, "settings.locale_rtl": {Text: "Right to left"},
		"settings.locale_current": {Text: "Current"}, "settings.locale_status": {Text: "Language changes apply immediately and remain active as you move between pages."},
		"settings.access_title": {Text: "Access context"}, "settings.access_description": {Text: "Facts carried by the server-admitted session."},
		"settings.organization": {Text: "Organization"}, "settings.principal": {Text: "Principal"}, "settings.purpose_scope": {Text: "Purpose / scope"}, "settings.data_source": {Text: "Data source"},
		"settings.access_callout": {Text: "These display facts grant no authority; every RPC is authorized again by the server."},
	},
	"de-DE": {
		"common.previous": {Text: "Zurück"}, "common.next": {Text: "Weiter"}, "common.not_reported": {Text: "Nicht gemeldet"}, "common.not_disclosed": {Text: "Nicht offengelegt"},
		"shell.skip_main": {Text: "Zum Hauptinhalt springen"}, "shell.search_employees": {Text: "Mitarbeitende suchen"}, "shell.work_overview": {Text: "Arbeitsübersicht"}, "shell.open_work": {Text: "Meine Aufgaben öffnen"}, "shell.acting_self": {Text: "Sie handeln als Sie selbst"}, "shell.live_source": {Text: "Live-Quelle · {source}"}, "shell.authenticated_scope": {Text: "Autorisierter Bereich"}, "shell.no_source": {Text: "Keine Dienstantwort"}, "shell.live_unavailable": {Text: "Live-Daten nicht verfügbar"}, "shell.locale": {Text: "Sprache"},
		"shell.connecting": {Text: "Verbindung zu HCM Next wird hergestellt"}, "shell.loading_authorized": {Text: "Autorisierte Daten werden aus der Live-Zelle geladen…"},
		"shell.page_loaded": {Text: "Seite {title} geladen"},
		"nav.collapse":      {Text: "Navigation einklappen"}, "nav.expand": {Text: "Navigation ausklappen"}, "nav.favorites": {Text: "Favoriten"}, "nav.all": {Text: "Alle Bereiche"}, "nav.none": {Text: "Keine passenden Menüs"}, "nav.main": {Text: "Hauptnavigation"}, "nav.support": {Text: "Hilfe und Einstellungen"}, "nav.workspace": {Text: "Arbeitsbereich-Navigation"}, "nav.filter": {Text: "Navigation filtern"}, "nav.filter_placeholder": {Text: "Menü filtern"}, "nav.filter_apply": {Text: "Menüfilter anwenden"}, "nav.filter_clear": {Text: "Menüfilter löschen"}, "nav.favorite_add": {Text: "{label} zu Favoriten hinzufügen"}, "nav.favorite_remove": {Text: "{label} aus Favoriten entfernen"}, "nav.work_queue": {Text: "Aufgabenliste"}, "nav.admin_overview": {Text: "Admin-Übersicht"}, "nav.overview": {Text: "Übersicht"},
		"page.home.label": {Text: "Start"}, "page.home.title": {Text: "Start"}, "page.home.subtitle": {Text: "Prüfen Sie offene Anfragen und halten Sie Ihre Arbeit in Bewegung."}, "page.home.greeting": {Text: "Guten Morgen, {name}."},
		"page.journeys.label": {Text: "Abläufe"}, "page.journeys.title": {Text: "Abläufe"}, "page.journeys.subtitle": {Text: "Geregelte Personalabläufe starten, verfolgen und abschließen."},
		"page.work.label": {Text: "Meine Aufgaben"}, "page.work.title": {Text: "Meine Aufgaben"}, "page.work.subtitle": {Text: "Laufende Beförderungsabläufe, die Aufmerksamkeit benötigen."},
		"page.history.label": {Text: "Aufgabenverlauf"}, "page.history.title": {Text: "Ablaufverlauf"}, "page.history.subtitle": {Text: "Abgeschlossene, abgelehnte und fehlgeschlagene Abläufe prüfen."},
		"page.people.label": {Text: "Mitarbeitende"}, "page.people.title": {Text: "Mitarbeitende"}, "page.people.subtitle": {Text: "Mitarbeitende, die über den geregelten Journey-Dienst sichtbar sind."},
		"page.person.label": {Text: "Person"}, "page.person.title": {Text: "Personenprofil"}, "page.person.subtitle": {Text: "Beschäftigtendaten und verfügbare geregelte Abläufe."},
		"page.organization.label": {Text: "Organisation"}, "page.organization.title": {Text: "Organisation"}, "page.insights.label": {Text: "Einblicke"}, "page.insights.title": {Text: "Einblicke"}, "page.admin.label": {Text: "Administration"}, "page.admin.title": {Text: "Administration"}, "page.appearance.label": {Text: "Marke & Darstellung"}, "page.appearance.title": {Text: "Marke & Darstellung"}, "page.studio.label": {Text: "Experience Studio"}, "page.studio.title": {Text: "Experience Studio"}, "page.help.label": {Text: "Hilfe"}, "page.help.title": {Text: "Hilfe-Center"}, "page.settings.label": {Text: "Einstellungen"}, "page.settings.title": {Text: "Einstellungen"}, "page.settings.subtitle": {Text: "Aktuelle authentifizierte Sitzung und verfügbare Einstellungen."},
		"work.empty_title": {Text: "Keine Aufgaben in dieser Ansicht"}, "work.empty_detail": {Text: "Wählen Sie einen anderen Filter oder kehren Sie zu allen Aufgaben zurück."}, "work.collection_label": {Text: "Beförderungsabläufe"}, "work.filter_label": {Text: "Aufgaben filtern"}, "work.selected_label": {Text: "Ausgewählte Aufgabe"}, "work.selected_summary": {Text: "Zusammenfassung der ausgewählten Aufgabe"},
		"work.promotion_journeys": {Text: "Beförderungsabläufe"}, "work.all": {Text: "Offene Aufgaben"}, "work.awaiting": {Text: "Wartet auf Genehmigung"}, "work.blocked": {Text: "Blockiert"}, "work.past": {Text: "Vergangene Abläufe"}, "work.item_count": {Plural: map[string]string{"one": "{count} Eintrag", "other": "{count} Einträge"}}, "work.authorized": {Text: "Ihre autorisierten Aufgaben"}, "work.view": {Text: "Meine Aufgaben anzeigen →"}, "work.nothing_selected": {Text: "Nichts ausgewählt"}, "work.show_all": {Text: "Alle Aufgaben anzeigen"}, "work.server_proposal": {Text: "Vom Server gemeldeter Vorschlag"}, "work.effective_date": {Text: "Wirksamkeitsdatum"}, "work.current_base": {Text: "Aktuelles Grundgehalt"}, "work.proposed_base": {Text: "Vorgeschlagenes Grundgehalt"}, "work.journey_id": {Text: "Ablauf-ID"}, "work.open_journey": {Text: "Live-Ablauf öffnen"},
		"people.filter_placeholder": {Text: "Nach Name, Rolle, Team, Standort oder Personalnummer filtern"}, "people.filter_aria": {Text: "Mitarbeitende filtern"}, "people.filter": {Text: "Filtern"}, "people.clear": {Text: "Löschen"}, "people.find": {Text: "Mitarbeitende suchen"}, "people.all_teams": {Text: "Alle Teams"}, "people.all_locations": {Text: "Alle Standorte"}, "people.team_aria": {Text: "Mitarbeitende nach Team filtern"}, "people.location_aria": {Text: "Mitarbeitende nach Standort filtern"}, "people.sort_by": {Text: "Sortieren nach"}, "people.table_aria": {Text: "Autorisierte Mitarbeitende"}, "people.column.person": {Text: "Person"}, "people.column.role": {Text: "Rolle"}, "people.column.team": {Text: "Team"}, "people.column.manager": {Text: "Führungskraft"}, "people.column.location": {Text: "Standort"}, "people.column.actions": {Text: "Aktionen"}, "people.promote": {Text: "Befördern"}, "people.promote_aria": {Text: "Beförderung für {name} starten"}, "people.pages": {Text: "Mitarbeitendenseiten"}, "people.range": {Text: "{first}–{last} von {total}"}, "people.page_count": {Text: "Seite {page} von {pages}"}, "people.empty_title": {Text: "Keine Mitarbeitenden gefunden"}, "people.clear_filter": {Text: "Filter löschen"},
		"people.scope": {Text: "Autorisiertes Beschäftigtenverzeichnis · {scope}"}, "people.filtered_count": {Text: "{filtered} von {total} Personen"}, "people.count": {Plural: map[string]string{"one": "{count} Person", "other": "{count} Personen"}},
		"person.back": {Text: "← Zurück zu Mitarbeitende"}, "person.unavailable": {Text: "Person nicht verfügbar"}, "person.return_directory": {Text: "Zurück zum Verzeichnis"}, "person.summary": {Text: "Personenzusammenfassung"}, "person.worker_profile": {Text: "Beschäftigtenprofil"}, "person.source": {Text: "Quelle · {source}"}, "person.employment_details": {Text: "Beschäftigungsdetails"}, "person.sensitive": {Text: "Sensibel"}, "person.private_data": {Text: "Private Daten"},
		"person.visible_scope": {Text: "Im Bereich sichtbar"}, "person.employment_overview": {Text: "Beschäftigungsübersicht"}, "person.worker_number": {Text: "Personalnummer"}, "person.job_code": {Text: "Stellencode"}, "person.job_level": {Text: "Stellenebene"}, "person.hire_date": {Text: "Eintrittsdatum"}, "person.employment_type": {Text: "Beschäftigungsart"}, "person.time_type": {Text: "Arbeitszeitart"}, "person.record_source": {Text: "Datensatzquelle"}, "person.record_created": {Text: "Datensatz erstellt"},
		"person.organization": {Text: "Organisation"}, "person.organization_unit": {Text: "Organisationseinheit"}, "person.manager": {Text: "Führungskraft"}, "person.position_id": {Text: "Positions-ID"}, "person.work_location": {Text: "Arbeitsort"}, "person.company": {Text: "Unternehmen"}, "person.business_unit": {Text: "Geschäftsbereich"}, "person.cost_center": {Text: "Kostenstelle"}, "person.work_arrangement": {Text: "Arbeitsmodell"},
		"person.compensation": {Text: "Vergütung"}, "person.base_pay": {Text: "Grundgehalt"}, "person.bonus_target": {Text: "Bonusziel"}, "person.pay_zone": {Text: "Vergütungszone"}, "person.pay_frequency": {Text: "Zahlungsfrequenz"}, "person.personal_information": {Text: "Persönliche Informationen"}, "person.personal_hidden": {Text: "Persönliche Kennungen · standardmäßig verborgen"}, "person.restricted": {Text: "Eingeschränkt"}, "person.legal_name": {Text: "Amtlicher Name"}, "person.preferred_name": {Text: "Bevorzugter Name"}, "person.worker_id": {Text: "Interne Beschäftigten-ID"}, "person.worker_ref": {Text: "Stabile Beschäftigtenreferenz"}, "person.history_detail": {Text: "Für {name} erfasste abgeschlossene, abgelehnte und fehlgeschlagene Abläufe."},
		"workflow.none": {Text: "Keine passenden Abläufe"}, "workflow.start": {Text: "Ablauf starten"}, "workflow.choose": {Text: "Wählen Sie einen geregelten Ablauf für {name}."}, "workflow.filter_placeholder": {Text: "Abläufe filtern"}, "workflow.filter_aria": {Text: "Verfügbare Abläufe filtern"}, "workflow.find": {Text: "Ablauf suchen"}, "workflow.filter": {Text: "Filtern"}, "workflow.start_named": {Text: "{name} starten"},
		"workflow.available_aria": {Text: "Verfügbare Abläufe"}, "workflow.available_count": {Plural: map[string]string{"one": "{count} verfügbar", "other": "{count} verfügbar"}},
		"history.none": {Text: "Keine Ablaufdatensätze gefunden"}, "history.record": {Text: "Datensatz"}, "history.search_aria": {Text: "Ablaufverlauf durchsuchen"}, "history.outcome_aria": {Text: "Ablaufverlauf nach Ergebnis filtern"}, "history.all_people": {Text: "Alle Mitarbeitenden"}, "history.any_year": {Text: "Beliebiges Wirksamkeitsjahr"}, "history.person_aria": {Text: "Ablaufverlauf nach Person filtern"}, "history.year_aria": {Text: "Ablaufverlauf nach Jahr filtern"}, "history.all_outcomes": {Text: "Alle Ergebnisse"}, "history.completed": {Text: "Abgeschlossen"}, "history.rejected": {Text: "Abgelehnt"}, "history.failed": {Text: "Fehlgeschlagen"}, "history.apply": {Text: "Filter anwenden"}, "history.find": {Text: "Vergangene Abläufe suchen"}, "history.clear": {Text: "Filter löschen"}, "history.open": {Text: "Datensatz öffnen"}, "history.closed": {Text: "Geschlossen · {value}"}, "history.effective": {Text: "Wirksam · {value}"},
		"history.none_detail": {Text: "Keine vergangenen Abläufe entsprechen dieser Ansicht."}, "history.search_placeholder": {Text: "Person, Ablauf oder Änderung suchen"},
		"history.global_title": {Text: "Globaler Ablaufverlauf"}, "history.global_detail": {Text: "Prüfen Sie terminale Ablaufergebnisse für alle Mitarbeitenden in Ihrem sichtbaren Bereich."}, "history.promotion": {Text: "Beförderung"}, "history.empty_terminal": {Text: "Abgeschlossene, abgelehnte und fehlgeschlagene Abläufe erscheinen hier nach der Erfassung durch den Dienst."}, "history.search_person_placeholder": {Text: "Ablauf, Änderung oder Ergebnis suchen"}, "history.filtered_count": {Text: "{filtered} von {total} Datensätzen"}, "history.count": {Plural: map[string]string{"one": "{count} Datensatz", "other": "{count} Datensätze"}},
		"appearance.intro_title": {Text: "Der Arbeitsbereich soll sich wie Ihre Organisation anfühlen"}, "appearance.tenant": {Text: "Mandantendarstellung"}, "appearance.brand_signature": {Text: "Markensignatur"}, "appearance.workspace_name": {Text: "Name des Arbeitsbereichs"}, "appearance.short_mark": {Text: "Kurzzeichen"}, "appearance.company_logo": {Text: "Unternehmenslogo"}, "appearance.company_logo_help": {Text: "Optionales freigegebenes Bild unter /workspace/assets/. Leer lassen, um die Textsignatur zu verwenden."}, "appearance.company_logo_placeholder": {Text: "/workspace/assets/firmenlogo.svg"}, "appearance.preview": {Text: "Arbeitsbereich-Vorschau"}, "appearance.save": {Text: "Darstellung speichern"}, "appearance.restore": {Text: "Standardeinstellungen wiederherstellen"},
		"appearance.color_mode": {Text: "Farbmodus"}, "appearance.color_mode_help": {Text: "Der Geräteeinstellung folgen oder einen durchgehend hellen oder dunklen Arbeitsbereich wählen."},
		"appearance.color_mode_system": {Text: "Systemeinstellung verwenden"}, "appearance.color_mode_system_help": {Text: "Diesem Gerät folgen und automatisch aktualisieren"}, "appearance.color_mode_light": {Text: "Hell"}, "appearance.color_mode_light_help": {Text: "Auf diesem Gerät den hellen Arbeitsbereich verwenden"}, "appearance.color_mode_dark": {Text: "Dunkel"}, "appearance.color_mode_dark_help": {Text: "Auf diesem Gerät den dunklen Arbeitsbereich verwenden"},
		"appearance.palette": {Text: "Farbpalette"}, "appearance.shape": {Text: "Oberflächenform"}, "appearance.density": {Text: "Informationsdichte"}, "appearance.glyphs": {Text: "Symbolsatz"}, "appearance.typeface": {Text: "Typografischer Charakter"}, "appearance.navigation": {Text: "Navigationsdarstellung"}, "appearance.motion": {Text: "Bewegung"},
		"accessibility.title": {Text: "Barrierefreiheit"}, "accessibility.description": {Text: "Passen Sie den Arbeitsbereich für angenehmes Lesen und Bedienen an. Diese Auswahl gilt in diesem Browser für jede Seite."},
		"accessibility.text_size": {Text: "Textgröße"}, "accessibility.text_size_help": {Text: "Produkttext vergrößern, ohne Informationen auszublenden."}, "accessibility.text_standard": {Text: "Standard"}, "accessibility.text_large": {Text: "Groß"}, "accessibility.text_larger": {Text: "Größer"},
		"accessibility.contrast": {Text: "Kontrast"}, "accessibility.contrast_help": {Text: "Systemeinstellung verwenden oder visuelle Grenzen verstärken."}, "accessibility.system": {Text: "Systemeinstellung verwenden"}, "accessibility.contrast_system_help": {Text: "Der Kontrasteinstellung des Geräts folgen."}, "accessibility.contrast_more": {Text: "Mehr Kontrast"}, "accessibility.contrast_more_help": {Text: "Text, Bedienelemente, Rahmen und Statusanzeigen verstärken."},
		"accessibility.motion": {Text: "Bewegung"}, "accessibility.motion_help": {Text: "Nicht notwendige Bewegung auf Seiten und in Abläufen reduzieren."}, "accessibility.motion_system_help": {Text: "Der Einstellung für reduzierte Bewegung folgen."}, "accessibility.motion_limited": {Text: "Begrenzte Bewegung"}, "accessibility.motion_limited_help": {Text: "Kurze Statushinweise beibehalten und räumliche sowie dekorative Bewegung entfernen."}, "accessibility.motion_reduce": {Text: "Bewegung reduzieren"}, "accessibility.motion_reduce_help": {Text: "Animationen deaktivieren und Übergänge verkürzen."},
		"accessibility.links": {Text: "Linksichtbarkeit"}, "accessibility.links_help": {Text: "Festlegen, ob Links immer zusätzlich unterstrichen werden."}, "accessibility.links_standard": {Text: "Standardlinks"}, "accessibility.links_standard_help": {Text: "Kontext-, Fokus- und Hover-Anzeigen verwenden."}, "accessibility.links_underlined": {Text: "Links immer unterstreichen"}, "accessibility.links_underlined_help": {Text: "Links im gesamten Produkt unterstreichen."},
		"accessibility.save": {Text: "Einstellungen speichern"}, "accessibility.reset": {Text: "Standard verwenden"}, "accessibility.status": {Text: "Die Einstellungen werden in diesem Browser gespeichert, bis der geregelte Kontodienst verfügbar ist."},
		"settings.locale_title": {Text: "Sprache & Region"}, "settings.locale_description": {Text: "Wählen Sie die Sprache für Navigation, Beschriftungen, Datumsangaben, Zahlen und Währungsformate."},
		"settings.locale_option_detail": {Text: "{code} · {direction}"}, "settings.locale_ltr": {Text: "Von links nach rechts"}, "settings.locale_rtl": {Text: "Von rechts nach links"},
		"settings.locale_current": {Text: "Aktuell"}, "settings.locale_status": {Text: "Sprachänderungen gelten sofort und bleiben beim Wechsel zwischen Seiten aktiv."},
		"settings.access_title": {Text: "Zugriffskontext"}, "settings.access_description": {Text: "Fakten aus der vom Server zugelassenen Sitzung."},
		"settings.organization": {Text: "Organisation"}, "settings.principal": {Text: "Identität"}, "settings.purpose_scope": {Text: "Zweck / Bereich"}, "settings.data_source": {Text: "Datenquelle"},
		"settings.access_callout": {Text: "Diese Anzeigefakten erteilen keine Berechtigung; jeder RPC wird erneut vom Server autorisiert."},
	},
	"ar": {
		"shell.skip_main": {Text: "الانتقال إلى المحتوى الرئيسي"}, "shell.search_employees": {Text: "البحث عن الموظفين"}, "shell.work_overview": {Text: "نظرة عامة على العمل"}, "shell.open_work": {Text: "فتح عملي"}, "shell.acting_self": {Text: "أنت تتصرف بصفتك"}, "shell.locale": {Text: "اللغة"},
		"shell.connecting": {Text: "جارٍ الاتصال بـ HCM Next"}, "shell.loading_authorized": {Text: "جارٍ تحميل البيانات المصرح بها من الخلية المباشرة…"},
		"shell.page_loaded": {Text: "تم تحميل صفحة {title}"},
		"nav.collapse":      {Text: "طي التنقل"}, "nav.expand": {Text: "توسيع التنقل"}, "nav.favorites": {Text: "المفضلة"}, "nav.all": {Text: "كل التنقل"}, "nav.none": {Text: "لا توجد قوائم مطابقة"}, "nav.main": {Text: "الرئيسية"}, "nav.workspace": {Text: "التنقل في مساحة العمل"}, "nav.filter": {Text: "تصفية التنقل"}, "nav.filter_placeholder": {Text: "تصفية القائمة"}, "nav.filter_apply": {Text: "تطبيق عامل التصفية"}, "nav.filter_clear": {Text: "مسح عامل التصفية"},
		"page.home.label": {Text: "الرئيسية"}, "page.home.title": {Text: "الرئيسية"}, "page.journeys.label": {Text: "الرحلات"}, "page.journeys.title": {Text: "الرحلات"}, "page.work.label": {Text: "عملي"}, "page.work.title": {Text: "عملي"}, "page.history.label": {Text: "سجل العمل"}, "page.history.title": {Text: "سجل سير العمل"}, "page.people.label": {Text: "الأشخاص"}, "page.people.title": {Text: "الأشخاص"}, "page.person.label": {Text: "الشخص"}, "page.person.title": {Text: "ملف الشخص"}, "page.organization.label": {Text: "المؤسسة"}, "page.organization.title": {Text: "المؤسسة"}, "page.insights.label": {Text: "الرؤى"}, "page.insights.title": {Text: "الرؤى"}, "page.admin.label": {Text: "الإدارة"}, "page.admin.title": {Text: "الإدارة"}, "page.appearance.label": {Text: "العلامة التجارية والمظهر"}, "page.appearance.title": {Text: "العلامة التجارية والمظهر"}, "page.help.label": {Text: "المساعدة"}, "page.help.title": {Text: "مركز المساعدة"}, "page.settings.label": {Text: "الإعدادات"}, "page.settings.title": {Text: "الإعدادات"}, "page.settings.subtitle": {Text: "الجلسة المصادق عليها حاليًا والتفضيلات المتاحة."},
		"people.all_teams": {Text: "كل الفرق"}, "people.all_locations": {Text: "كل المواقع"}, "people.team_aria": {Text: "تصفية الموظفين حسب الفريق"}, "people.location_aria": {Text: "تصفية الموظفين حسب الموقع"}, "people.sort_by": {Text: "الترتيب حسب"}, "people.column.actions": {Text: "الإجراءات"}, "people.promote": {Text: "ترقية"}, "people.promote_aria": {Text: "بدء ترقية لـ {name}"},
		"accessibility.title": {Text: "تفضيلات إمكانية الوصول"}, "accessibility.description": {Text: "اضبط مساحة العمل للقراءة والتفاعل براحة. تنطبق هذه الخيارات على كل صفحة في هذا المتصفح."},
		"accessibility.text_size": {Text: "حجم النص"}, "accessibility.text_size_help": {Text: "كبّر نص المنتج من دون إخفاء المعلومات."}, "accessibility.text_standard": {Text: "قياسي"}, "accessibility.text_large": {Text: "كبير"}, "accessibility.text_larger": {Text: "أكبر"},
		"accessibility.contrast": {Text: "التباين"}, "accessibility.contrast_help": {Text: "استخدم إعداد النظام أو عزز الحدود المرئية."}, "accessibility.system": {Text: "استخدام إعداد النظام"}, "accessibility.contrast_system_help": {Text: "اتبع تفضيل التباين في الجهاز."}, "accessibility.contrast_more": {Text: "تباين أعلى"}, "accessibility.contrast_more_help": {Text: "عزز النص وعناصر التحكم والحدود ومؤشرات الحالة."},
		"accessibility.motion": {Text: "الحركة"}, "accessibility.motion_help": {Text: "قلل الحركة غير الضرورية في الصفحات ومسارات العمل."}, "accessibility.motion_system_help": {Text: "اتبع تفضيل تقليل الحركة في الجهاز."}, "accessibility.motion_limited": {Text: "حركة محدودة"}, "accessibility.motion_limited_help": {Text: "احتفظ بإشارات الحالة القصيرة وأزل الحركة الانتقالية والزخرفية."}, "accessibility.motion_reduce": {Text: "تقليل الحركة"}, "accessibility.motion_reduce_help": {Text: "عطل الرسوم المتحركة واختصر الانتقالات."},
		"accessibility.links": {Text: "وضوح الروابط"}, "accessibility.links_help": {Text: "اختر ما إذا كانت الروابط تعرض دائمًا إشارة غير لونية."}, "accessibility.links_standard": {Text: "روابط قياسية"}, "accessibility.links_standard_help": {Text: "استخدم مؤشرات السياق والتركيز والمرور."}, "accessibility.links_underlined": {Text: "تسطير الروابط دائمًا"}, "accessibility.links_underlined_help": {Text: "سطّر الروابط في جميع أنحاء المنتج."},
		"accessibility.save": {Text: "حفظ التفضيلات"}, "accessibility.reset": {Text: "استخدام الإعدادات الافتراضية"}, "accessibility.status": {Text: "تُحفظ التفضيلات في هذا المتصفح حتى تتوفر خدمة تفضيلات الحساب المُدارة."},
		"settings.locale_title": {Text: "اللغة والمنطقة"}, "settings.locale_description": {Text: "اختر اللغة المستخدمة للتنقل والتسميات والتواريخ والأرقام وتنسيق العملات."},
		"settings.locale_option_detail": {Text: "{code} · {direction}"}, "settings.locale_ltr": {Text: "من اليسار إلى اليمين"}, "settings.locale_rtl": {Text: "من اليمين إلى اليسار"},
		"settings.locale_current": {Text: "الحالية"}, "settings.locale_status": {Text: "تُطبق تغييرات اللغة فورًا وتظل نشطة عند التنقل بين الصفحات."},
		"settings.access_title": {Text: "سياق الوصول"}, "settings.access_description": {Text: "حقائق تحملها الجلسة التي أجازها الخادم."},
		"settings.organization": {Text: "المؤسسة"}, "settings.principal": {Text: "الهوية"}, "settings.purpose_scope": {Text: "الغرض / النطاق"}, "settings.data_source": {Text: "مصدر البيانات"},
		"settings.access_callout": {Text: "لا تمنح حقائق العرض هذه أي صلاحية؛ يعيد الخادم تخويل كل استدعاء RPC."},
		"appearance.color_mode":   {Text: "نمط الألوان"}, "appearance.color_mode_help": {Text: "اتبع إعداد الجهاز أو اختر مساحة عمل فاتحة أو داكنة باستمرار."},
		"appearance.color_mode_system": {Text: "استخدام إعداد النظام"}, "appearance.color_mode_system_help": {Text: "اتبع هذا الجهاز وحدّث تلقائيًا"}, "appearance.color_mode_light": {Text: "فاتح"}, "appearance.color_mode_light_help": {Text: "استخدم مساحة العمل الفاتحة على هذا الجهاز"}, "appearance.color_mode_dark": {Text: "داكن"}, "appearance.color_mode_dark_help": {Text: "استخدم مساحة العمل الداكنة على هذا الجهاز"},
	},
}

var productMessageRegistry = func() *localize.Registry {
	registry := localize.NewRegistry()
	for locale, messages := range productMessages {
		if err := registry.Register(localize.Catalog{Locale: locale, Version: productCatalogVersion, Messages: messages}); err != nil {
			panic(err)
		}
	}
	return registry
}()
