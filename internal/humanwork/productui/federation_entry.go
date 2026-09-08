package productui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// FederationEntry is one server-composed tenant-federation sign-in option.
// Entries arrive as data from the AUTHN-001 issuer registry projection;
// presentation renders what it is given and never authors issuers,
// protocols, assurance levels, or destinations.
type FederationEntry struct {
	Tenant    string
	Issuer    string
	Protocol  string
	Assurance string
	Href      string
}

// RecoveryOption is one server-composed sign-in recovery destination: a
// password-reset flow, a helpdesk contact, or an administrator address.
// Options arrive as data; presentation validates the destination scheme
// and drops anything else without inventing a fallback.
type RecoveryOption struct {
	Label       string
	Description string
	Href        string
}

// FederationEntryProps keeps the entry gate independently composable
// without the page-wide View.
type FederationEntryProps struct {
	I18nProps
	Entries  []FederationEntry
	Recovery []RecoveryOption
	Navigate func(string)
}

// validRecoveryHref admits only destinations a sign-in recovery link may
// carry: workspace-relative starts, https flows, and mail contacts.
// Everything else — script, data, network-relative, plain http — fails
// closed so a compromised or misconfigured server answer cannot turn the
// gate into an attack surface.
func validRecoveryHref(href string) bool {
	href = strings.TrimSpace(href)
	if href == "" {
		return false
	}
	if strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") {
		return true
	}
	lower := strings.ToLower(href)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:")
}

// federationRecoverySection renders the labelled recovery options. No safe
// option means no section: the gate never shows a control that leads
// nowhere.
func federationRecoverySection(props FederationEntryProps) ui.Node {
	options := make([]RecoveryOption, 0, len(props.Recovery))
	for _, option := range props.Recovery {
		if validRecoveryHref(option.Href) {
			options = append(options, option)
		}
	}
	if len(options) == 0 {
		return ui.Text("")
	}
	items := make([]ui.Node, 0, len(options))
	for _, option := range options {
		option := option
		link := html.A(html.Props{Href: option.Href},
			html.Span(html.Props{Class: "federation-entry-issuer"}, ui.Text(option.Label)),
			html.Span(html.Props{Class: "federation-entry-meta"}, ui.Text(option.Description)))
		items = append(items, html.Li(html.Props{Class: "federation-entry-item"}, link))
	}
	return html.Section(html.Props{ID: "federation-entry-recovery", Class: "federation-entry-recovery", Aria: map[string]string{"labelledby": "federation-entry-recovery-title"}},
		html.H2(html.Props{ID: "federation-entry-recovery-title", Class: "federation-entry-tenant"}, ui.Text(props.Text("federation_entry.recovery_title"))),
		html.Ul(html.Props{Class: "federation-entry-recovery-list"}, items...),
	)
}

func federationEntryProps(view View) FederationEntryProps {
	return FederationEntryProps{
		I18nProps: I18nProps{Locale: view.Locale}, Entries: view.FederationEntries, Recovery: view.RecoveryOptions, Navigate: view.Navigate,
	}
}

// FederationEntryList renders the tenant-federation entry gate: issuer
// options grouped by tenant in deterministic order, each link carrying its
// protocol metadata. No configured issuer is an honest empty state, never
// an empty page or an invented destination.
func FederationEntryList(props FederationEntryProps) ui.Node {
	if len(props.Entries) == 0 {
		return html.Section(html.Props{ID: "federation-entry", Class: "federation-entry", Aria: map[string]string{"labelledby": "page-title"}},
			html.H1(html.Props{ID: "page-title", Raw: map[string]any{"tabindex": "-1"}}, ui.Text(props.Text("federation_entry.title"))),
			html.Div(html.Props{Class: "federation-entry-empty"},
				unavailablePanel(props.Text("federation_entry.empty_title"), props.Text("federation_entry.empty_description"))),
			federationRecoverySection(props),
		)
	}
	groups := federationEntryGroups(props.Entries)
	sections := make([]ui.Node, 0, len(groups))
	for _, tenant := range groups {
		items := make([]ui.Node, 0, len(tenant.entries))
		for _, entry := range tenant.entries {
			entry := entry
			meta := entry.Protocol
			if assurance := strings.TrimSpace(entry.Assurance); assurance != "" {
				meta += " · " + assurance
			}
			link := softwareLink(props.Navigate, html.Props{}, entry.Href,
				html.Span(html.Props{Class: "federation-entry-issuer"}, ui.Text(entry.Issuer)),
				html.Span(html.Props{Class: "federation-entry-meta"}, ui.Text(meta)))
			items = append(items, html.Li(html.Props{Class: "federation-entry-item"}, link))
		}
		sections = append(sections, html.Section(html.Props{Class: "federation-entry-group", Aria: map[string]string{"label": tenant.name}},
			html.H2(html.Props{Class: "federation-entry-tenant"}, ui.Text(tenant.name)),
			html.Ul(html.Props{Class: "federation-entry-list"}, items...)))
	}
	children := append([]ui.Node{
		html.H1(html.Props{ID: "page-title", Raw: map[string]any{"tabindex": "-1"}}, ui.Text(props.Text("federation_entry.title"))),
		html.P(html.Props{Class: "federation-entry-description"}, ui.Text(props.Text("federation_entry.description"))),
	}, sections...)
	children = append(children, federationRecoverySection(props))
	return html.Section(html.Props{ID: "federation-entry", Class: "federation-entry", Aria: map[string]string{"labelledby": "page-title"}}, children...)
}

type federationTenantGroup struct {
	name    string
	entries []FederationEntry
}

func federationEntryGroups(entries []FederationEntry) []federationTenantGroup {
	index := map[string]int{}
	groups := make([]federationTenantGroup, 0)
	for _, entry := range entries {
		at, ok := index[entry.Tenant]
		if !ok {
			at = len(groups)
			index[entry.Tenant] = at
			groups = append(groups, federationTenantGroup{name: entry.Tenant})
		}
		groups[at].entries = append(groups[at].entries, entry)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].name < groups[j].name })
	return groups
}
