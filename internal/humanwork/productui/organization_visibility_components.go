package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type OrganizationVisibilityPageProps struct {
	I18nProps
	Roles          []AccessRole
	Policies       []OrganizationVisibilityPolicy
	AvailableUnits []string
	// AvailableDomains bounds which data-domain names the editors may
	// render; anything outside it resolves to the honest empty state.
	AvailableDomains []string
	Back             ActionLinkProps
	RolesLink        ActionLinkProps
	Editable         bool
	OnSave           func(OrganizationVisibilityPolicy)
}

type organizationVisibilityMode struct{ Value, Label, Detail string }

func OrganizationVisibilityPage(props OrganizationVisibilityPageProps) ui.Node {
	policies := make(map[string]OrganizationVisibilityPolicy, len(props.Policies))
	for _, policy := range props.Policies {
		policies[policy.RoleID] = policy
	}
	editors := make([]ui.Node, 0, len(props.Roles))
	for index, role := range props.Roles {
		if !role.Active {
			continue
		}
		policy, ok := policies[role.ID]
		if !ok {
			policy = OrganizationVisibilityPolicy{RoleID: role.ID, Mode: "OWN_UNIT"}
		}
		editors = append(editors, roleVisibilityEditor(props, role, policy, index == 0))
	}
	if len(editors) == 0 {
		editors = append(editors, html.Div(html.Props{Class: "surface empty-state"}, html.H2(html.Props{}, ui.Text(props.Text("organization_visibility.no_roles"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("organization_visibility.no_roles_detail"))), ui.CreateElement(ActionLink, props.RolesLink)))
	}
	return html.Div(html.Props{Class: "organization-visibility-page"},
		html.Section(html.Props{Class: "surface organization-visibility-intro"},
			html.Div(html.Props{}, html.Span(html.Props{Class: "eyebrow"}, ui.Text(props.Text("organization_visibility.eyebrow"))), html.H2(html.Props{}, ui.Text(props.Text("organization_visibility.heading"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("organization_visibility.description")))),
			html.Div(html.Props{Class: "organization-visibility-nav"}, ui.CreateElement(ActionLink, props.RolesLink), ui.CreateElement(ActionLink, props.Back)),
		),
		html.Div(html.Props{Class: "role-visibility-list"}, editors...),
	)
}

func roleVisibilityEditor(props OrganizationVisibilityPageProps, role AccessRole, policy OrganizationVisibilityPolicy, expanded bool) ui.Node {
	draft := policy
	selected := map[string]bool{}
	for _, unit := range draft.OrganizationUnits {
		selected[strings.ToLower(strings.TrimSpace(unit))] = true
	}
	modes := []organizationVisibilityMode{
		{"ALL", props.Text("organization_visibility.mode_all"), props.Text("organization_visibility.mode_all_detail")},
		{"OWN_UNIT", props.Text("organization_visibility.mode_own"), props.Text("organization_visibility.mode_own_detail")},
		{"ALLOWLIST", props.Text("organization_visibility.mode_allow"), props.Text("organization_visibility.mode_allow_detail")},
		{"DENYLIST", props.Text("organization_visibility.mode_deny"), props.Text("organization_visibility.mode_deny_detail")},
	}
	modeChoices := make([]ui.Node, 0, len(modes))
	for _, mode := range modes {
		value := mode.Value
		input := html.Props{Type: "radio", Name: "organization-visibility-mode-" + role.ID, Value: value, Checked: strings.EqualFold(draft.Mode, value), Disabled: !props.Editable, Aria: map[string]string{"label": mode.Label}}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) { draft.Mode = value })
		modeChoices = append(modeChoices, html.Label(html.Props{Class: "organization-visibility-mode"}, html.Input(input), html.Span(html.Props{}, html.Strong(html.Props{}, ui.Text(mode.Label)), html.Small(html.Props{}, ui.Text(mode.Detail)))))
	}
	unitChoices := make([]ui.Node, 0, len(props.AvailableUnits))
	for _, unit := range props.AvailableUnits {
		value := unit
		input := html.Props{Type: "checkbox", Name: "organization-visibility-unit-" + role.ID, Value: value, Checked: selected[strings.ToLower(value)], Disabled: !props.Editable, Aria: map[string]string{"label": value}}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) {
			key := strings.ToLower(value)
			selected[key] = !selected[key]
			draft.OrganizationUnits = draft.OrganizationUnits[:0]
			for _, candidate := range props.AvailableUnits {
				if selected[strings.ToLower(candidate)] {
					draft.OrganizationUnits = append(draft.OrganizationUnits, candidate)
				}
			}
		})
		unitChoices = append(unitChoices, html.Label(html.Props{Class: "organization-visibility-unit"}, html.Input(input), html.Span(html.Props{}, ui.Text(unit))))
	}
	modeFieldset := append([]ui.Node{html.Legend(html.Props{}, ui.Text(props.Text("organization_visibility.scope_title")))}, modeChoices...)
	unitFieldset := append([]ui.Node{html.Legend(html.Props{}, ui.Text(props.Text("organization_visibility.units_title"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("organization_visibility.units_help")))}, html.Div(html.Props{Class: "organization-visibility-unit-grid"}, unitChoices...))
	modeLabels := make(map[string]string, len(modes))
	for _, mode := range modes {
		modeLabels[mode.Value] = mode.Label
	}
	scopeBlock := html.Div(html.Props{Class: "organization-visibility-scope"},
		organizationScopeResolution(props, "organization_visibility.current_scope", "organization-scope-current", policy.Mode, policy.OrganizationUnits, props.AvailableUnits, modeLabels),
		organizationScopeResolution(props, "organization_visibility.proposed_scope", "organization-scope-proposed", draft.Mode, draft.OrganizationUnits, props.AvailableUnits, modeLabels),
	)
	domainBlock := dataDomainScope(props, policy.DataDomains, props.AvailableDomains)
	detailsProps := html.Props{Class: "surface role-visibility-editor", Raw: map[string]any{"data-role-id": role.ID}}
	if expanded {
		detailsProps.Raw["open"] = true
	}
	return html.Tag("details", detailsProps,
		html.Tag("summary", html.Props{}, html.Div(html.Props{}, html.Strong(html.Props{}, ui.Text(role.Name)), html.Code(html.Props{}, ui.Text(role.ID))), html.Span(html.Props{Class: "status"}, ui.Text(visibilityModeLabel(policy.Mode)))),
		html.Form(html.Props{Class: "organization-visibility-form", OnSubmit: saveOrganizationVisibility(props.OnSave, &draft)},
			html.Fieldset(html.Props{Class: "organization-visibility-modes"}, modeFieldset...),
			html.Fieldset(html.Props{Class: "organization-visibility-units"}, unitFieldset...),
			scopeBlock,
			domainBlock,
			html.Div(html.Props{Class: "organization-visibility-boundary", Raw: map[string]any{"role": "note"}}, html.Strong(html.Props{}, ui.Text(props.Text("organization_visibility.boundary_title"))), html.P(html.Props{}, ui.Text(props.Text("organization_visibility.boundary_detail")))),
			saveActions(props, role),
		),
	)
}

// saveActions renders the editor's save row in its semantic state: an
// editable policy gets the live save button; without the update grant
// the button stays disabled with the reason and the recovery link, so
// the viewer can resolve the condition instead of guessing.
func saveActions(props OrganizationVisibilityPageProps, role AccessRole) ui.Node {
	save := html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: !props.Editable}, ui.Text(props.Text("organization_visibility.save")))
	status := html.P(html.Props{ID: "organization-visibility-status-" + role.ID, Class: "muted", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(props.Text("organization_visibility.status")))
	if props.Editable {
		return html.Div(html.Props{Class: "organization-visibility-actions"}, save, status)
	}
	return html.Div(html.Props{Class: "organization-visibility-actions", Raw: map[string]any{"data-action-state": "unavailable"}},
		save, status,
		html.P(html.Props{Class: "muted"}, ui.Text(props.Text("organization_visibility.save_unavailable"))),
		ui.CreateElement(ActionLink, props.RolesLink),
	)
}

func visibilityModeLabel(mode string) string {
	switch strings.ToUpper(mode) {
	case "ALL":
		return "Everyone"
	case "ALLOWLIST":
		return "Selected units"
	case "DENYLIST":
		return "Except selected units"
	default:
		return "Own unit"
	}
}

func saveOrganizationVisibility(save func(OrganizationVisibilityPolicy), draft *OrganizationVisibilityPolicy) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(*draft) })
}
