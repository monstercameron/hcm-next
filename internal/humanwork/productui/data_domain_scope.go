package productui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AvailableDataDomains returns the closed data-domain taxonomy the
// presentation layer may name, grounded in the owning domain specs:
// People (people-employment-assignment-domain), Position
// (position-and-headcount-domain), Organization
// (organization-and-relationship-domain), and Compensation
// (compensation-domain). It returns a fresh slice on every call so
// callers cannot mutate shared presentation metadata.
//
// Units are tenant data (an open set), but data domains are a closed
// spec taxonomy: a policy naming anything outside this list withholds
// that name from presentation instead of rendering it as a domain.
// Authorization stays server-side; this list only bounds what the page
// may display.
func AvailableDataDomains() []string {
	return []string{"Compensation", "Organization", "People", "Position"}
}

// ResolveDataDomainScope maps the granted domain names to the concrete
// sorted domain list the presentation layer may render, intersected
// against the available domains. It is display resolution only: it never
// authorizes anything and never invents scope.
//
// Unknown names (anything outside available) and blank names resolve to
// nothing. Matching is case-insensitive, output carries
// available-casing, and results are sorted and de-duplicated. Inputs are
// never mutated.
func ResolveDataDomainScope(granted, available []string) []string {
	known := make(map[string]string, len(available))
	order := make([]string, 0, len(available))
	for _, domain := range available {
		key := strings.ToLower(strings.TrimSpace(domain))
		if key == "" {
			continue
		}
		if _, ok := known[key]; !ok {
			known[key] = strings.TrimSpace(domain)
			order = append(order, key)
		}
	}
	admitted := make(map[string]bool, len(granted))
	for _, domain := range granted {
		if key := strings.ToLower(strings.TrimSpace(domain)); key != "" {
			admitted[key] = true
		}
	}
	var keys []string
	for _, key := range order {
		if admitted[key] {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, known[key])
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// dataDomainScope renders one role's admitted data-domain block: a
// heading, the admitted domain chips, or an honest empty state when the
// policy grants no available domain. Non-admitted domains are withheld
// from the markup entirely, never rendered-then-hidden. Callers pass the
// policy's granted domains and the page's available domains; both
// resolve through ResolveDataDomainScope, so the display can never drift
// from the resolution contract.
func dataDomainScope(props OrganizationVisibilityPageProps, granted, available []string) ui.Node {
	children := []ui.Node{
		html.H3(html.Props{Class: "data-domain-scope-title"}, ui.Text(props.Text("data_domain_scope.title"))),
	}
	if resolved := ResolveDataDomainScope(granted, available); len(resolved) > 0 {
		chips := make([]ui.Node, 0, len(resolved))
		for _, domain := range resolved {
			chips = append(chips, html.Li(html.Props{Class: "data-domain-scope-domain"}, ui.Text(domain)))
		}
		children = append(children, html.Ul(html.Props{Class: "data-domain-scope-domains"}, chips...))
	} else {
		children = append(children, html.P(html.Props{Class: "data-domain-scope-empty"}, ui.Text(props.Text("data_domain_scope.empty"))))
	}
	return html.Div(html.Props{Class: "data-domain-scope"}, children...)
}
