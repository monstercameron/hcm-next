package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PolicySimulationProps carries the server-projected view-as policy
// simulation. Subject names the simulated identity, Allowed the simulated
// outcome, ReasonDetail the server's redaction-safe explanation, Rules the
// matched rule identifiers, Versions the evaluated policy versions, and
// EvidenceRef the durable evidence reference; ExitHref returns the viewer
// to their own view. Presentation never evaluates policy: it renders the
// projection and invents no outcome, rule, or version.
type PolicySimulationProps struct {
	I18nProps
	Subject      string
	Allowed      bool
	ReasonDetail string
	Rules        []string
	Versions     []string
	EvidenceRef  string
	ExitHref     string
	Navigate     func(string)
}

// PolicySimulation renders the view-as simulation panel: headlined as a
// simulation that assumes no authority, naming the simulated subject and
// outcome with the server's reason, rules, versions, and evidence, plus an
// exit link. Without a subject there is no panel; an unsafe exit yields no
// link. The panel is deliberately not locally dismissible: hiding it would
// pretend a simulation that is still in force has ended. Blank reason,
// rule, version, and evidence projections omit their rows.
func PolicySimulation(props PolicySimulationProps) ui.Node {
	if strings.TrimSpace(props.Subject) == "" {
		return nil
	}
	outcome := props.Text("view_as.denied")
	if props.Allowed {
		outcome = props.Text("view_as.allowed")
	}
	nodes := []ui.Node{
		html.P(html.Props{Class: "policy-simulation-notice"}, ui.Text(props.Text("view_as.notice"))),
		html.P(html.Props{Class: "policy-simulation-row"},
			html.Span(html.Props{Class: "policy-simulation-label"}, ui.Text(props.Text("view_as.subject"))),
			html.Span(html.Props{Class: "policy-simulation-value"}, ui.Text(strings.TrimSpace(props.Subject)))),
		html.P(html.Props{Class: "policy-simulation-row"},
			html.Span(html.Props{Class: "policy-simulation-label"}, ui.Text(props.Text("view_as.outcome"))),
			html.Span(html.Props{Class: "policy-simulation-value"}, ui.Text(outcome))),
	}
	if strings.TrimSpace(props.ReasonDetail) != "" {
		nodes = append(nodes, html.P(html.Props{Class: "policy-simulation-detail"}, ui.Text(props.ReasonDetail)))
	}
	var rules []string
	for _, rule := range props.Rules {
		if trimmed := strings.TrimSpace(rule); trimmed != "" {
			rules = append(rules, trimmed)
		}
	}
	if len(rules) > 0 {
		items := make([]ui.Node, 0, len(rules))
		for _, rule := range rules {
			items = append(items, html.Li(html.Props{Class: "policy-simulation-rule"}, ui.Text(rule)))
		}
		nodes = append(nodes, html.Ul(html.Props{Class: "policy-simulation-rules", Aria: map[string]string{"label": props.Text("view_as.rules")}}, items...))
	}
	var versions []string
	for _, version := range props.Versions {
		if trimmed := strings.TrimSpace(version); trimmed != "" {
			versions = append(versions, trimmed)
		}
	}
	if len(versions) > 0 {
		nodes = append(nodes, html.P(html.Props{Class: "policy-simulation-row"},
			html.Span(html.Props{Class: "policy-simulation-label"}, ui.Text(props.Text("view_as.versions"))),
			html.Span(html.Props{Class: "policy-simulation-value"}, ui.Text(strings.Join(versions, ", ")))))
	}
	if strings.TrimSpace(props.EvidenceRef) != "" {
		nodes = append(nodes, html.P(html.Props{Class: "policy-simulation-row"},
			html.Span(html.Props{Class: "policy-simulation-label"}, ui.Text(props.Text("view_as.evidence"))),
			html.Span(html.Props{Class: "policy-simulation-value"}, ui.Text(strings.TrimSpace(props.EvidenceRef)))))
	}
	if validRecoveryHref(props.ExitHref) {
		nodes = append(nodes, html.Div(html.Props{Class: "policy-simulation-actions"},
			softwareLink(props.Navigate, html.Props{Class: "policy-simulation-exit"}, props.ExitHref, ui.Text(props.Text("view_as.exit")))))
	}
	return html.Section(html.Props{ID: "policy-simulation", Class: "policy-simulation", Aria: map[string]string{"labelledby": "policy-simulation-title"}},
		append([]ui.Node{html.H2(html.Props{ID: "policy-simulation-title", Class: "policy-simulation-title"}, ui.Text(props.Text("view_as.title")))}, nodes...)...,
	)
}

func policySimulation(view View) ui.Node {
	if view.PolicySimulation == nil {
		return html.Fragment()
	}
	props := *view.PolicySimulation
	props.I18nProps = I18nProps{Locale: view.Locale}
	props.Navigate = view.Navigate
	return ui.CreateElement(PolicySimulation, props)
}
