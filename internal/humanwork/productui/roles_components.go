package productui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type RolesPageProps struct {
	I18nProps
	Roles            []AccessRole
	Assignments      []WorkerRoleAssignment
	PagePermissions  []RolePagePermission
	Pages            []RolePageOption
	People           []Person
	Query            string
	FilterHref       string
	Navigate         func(string)
	Back             ActionLinkProps
	CanCreate        bool
	CanUpdate        bool
	OnSaveRole       func(AccessRole)
	OnAssign         func(WorkerRoleAssignment)
	OnSavePermission func(RolePagePermission)
}

// RolePageOption is registry metadata for one configurable page. It keeps
// the reusable editor independent from the application page registry.
type RolePageOption struct {
	ID          PageID
	Label       string
	Description string
}

func RolesPage(props RolesPageProps) ui.Node {
	filterQuery := ui.UseState(props.Query)
	ui.UseEffectOf(func() func() { filterQuery.Set(props.Query); return nil }, props.Query)
	filterInput := html.Props{ID: "role-directory-query", Name: "q", Type: "search", Value: filterQuery.Get(), Placeholder: props.Text("roles.placeholder")}
	filterInput.OnInput = ui.UseEvent(func(event ui.InputEvent) { filterQuery.Set(event.GetValue()) })
	filterSubmit := submitRoleFilter(props.Navigate, props.FilterHref, filterQuery.Get)
	roleCards := make([]ui.Node, 0, len(props.Roles))
	for _, role := range props.Roles {
		kind := props.Text("roles.customer_role")
		if role.System {
			kind = props.Text("roles.system_role")
		}
		roleCards = append(roleCards, ui.CreateElement(RoleAccessCard, roleAccessCardProps{Page: props, Role: role, Kind: kind}))
	}

	assignments := make(map[string]WorkerRoleAssignment, len(props.Assignments))
	for _, assignment := range props.Assignments {
		assignments[strings.ToLower(strings.TrimSpace(assignment.WorkerRef))] = assignment
	}
	people := make([]ui.Node, 0, len(props.People))
	query := strings.ToLower(strings.TrimSpace(filterQuery.Get()))
	for _, person := range props.People {
		if query != "" && !strings.Contains(strings.ToLower(strings.Join([]string{person.Name, person.Role, person.Team, person.WorkerNumber}, " ")), query) {
			continue
		}
		assignment, ok := assignments[strings.ToLower(strings.TrimSpace(person.ID))]
		if !ok {
			assignment = WorkerRoleAssignment{WorkerRef: person.ID}
		}
		people = append(people, ui.CreateElement(WorkerRoleEditor, workerRoleEditorProps{I18n: props.I18nProps, Person: person, Roles: props.Roles, Assignment: assignment, Editable: props.CanUpdate, Save: props.OnAssign}))
	}
	if len(people) == 0 {
		people = append(people, html.Div(html.Props{Class: "empty-state"}, html.Strong(html.Props{}, ui.Text(props.Text("roles.no_match")))))
	}

	return html.Div(html.Props{Class: "roles-access-page"},
		html.Section(html.Props{Class: "surface roles-access-intro"},
			html.Div(html.Props{}, html.Span(html.Props{Class: "eyebrow"}, ui.Text(props.Text("roles.eyebrow"))), html.H2(html.Props{}, ui.Text(props.Text("roles.heading"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("roles.description")))),
			props.BackNode(),
		),
		html.Div(html.Props{Class: "roles-access-layout"},
			html.Section(html.Props{Class: "surface role-catalog"},
				html.Div(html.Props{Class: "section-head"}, html.Div(html.Props{}, html.H2(html.Props{}, ui.Text(props.Text("roles.catalog"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("roles.catalog_help"))))),
				html.Div(html.Props{Class: "access-role-grid"}, roleCards...),
				createRoleForm(props.I18nProps, props.CanCreate, props.OnSaveRole),
			),
			html.Section(html.Props{Class: "surface employee-role-directory"},
				html.Div(html.Props{Class: "section-head"}, html.Div(html.Props{}, html.H2(html.Props{}, ui.Text(props.Text("roles.assignments"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("roles.assignments_help"))))),
				html.Form(html.Props{Class: "role-directory-filter", Action: props.FilterHref, Method: "get", OnSubmit: filterSubmit}, html.Label(html.Props{For: "role-directory-query"}, ui.Text(props.Text("roles.find"))), html.Div(html.Props{}, html.Input(filterInput), html.Button(html.Props{Class: "button secondary", Type: "submit"}, ui.Text(props.Text("roles.filter"))))),
				html.Div(html.Props{Class: "employee-role-list"}, people...),
			),
		),
	)
}

type roleAccessCardProps struct {
	Page RolesPageProps
	Role AccessRole
	Kind string
}

func RoleAccessCard(input roleAccessCardProps) ui.Node {
	return roleAccessCard(input.Page, input.Role, input.Kind)
}

type workerRoleEditorProps struct {
	I18n       I18nProps
	Person     Person
	Roles      []AccessRole
	Assignment WorkerRoleAssignment
	Editable   bool
	Save       func(WorkerRoleAssignment)
}

func WorkerRoleEditor(input workerRoleEditorProps) ui.Node {
	return workerRoleAssignmentEditor(input.I18n, input.Person, input.Roles, input.Assignment, input.Editable, input.Save)
}

func roleAccessCard(props RolesPageProps, role AccessRole, kind string) ui.Node {
	permissions := make(map[PageID]RolePagePermission, len(props.PagePermissions))
	for _, permission := range props.PagePermissions {
		if permission.RoleID == role.ID {
			permissions[permission.Page] = permission
		}
	}
	rows := make([]ui.Node, 0, len(props.Pages))
	for _, page := range props.Pages {
		permission := permissions[page.ID]
		permission.RoleID = role.ID
		permission.Page = page.ID
		rows = append(rows, ui.CreateElement(RolePagePermissionRow, RolePagePermissionRowProps{
			I18nProps: props.I18nProps, Page: page, Permission: permission, Editable: props.CanUpdate, Save: props.OnSavePermission,
		}))
	}
	return html.Tag("details", html.Props{Class: "access-role-card"},
		html.Tag("summary", html.Props{},
			html.Div(html.Props{Class: "access-role-identity"}, html.Strong(html.Props{}, ui.Text(role.Name)), html.Code(html.Props{}, ui.Text(role.ID))),
			html.Span(html.Props{Class: "status"}, ui.Text(kind)),
			html.P(html.Props{Class: "muted"}, ui.Text(role.Description)),
			html.Small(html.Props{Class: "muted access-role-expand"}, ui.Text(props.Text("roles.page_access_open"))),
		),
		html.Div(html.Props{Class: "role-page-access"},
			html.Div(html.Props{Class: "role-page-access-intro"}, html.Strong(html.Props{}, ui.Text(props.Text("roles.page_access"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("roles.page_access_help")))),
			html.Div(html.Props{Class: "role-page-table-wrap", TabIndex: html.TabIndexZero, Raw: map[string]any{"role": "region", "aria-label": props.Text("roles.page_access")}},
				html.Table(html.Props{Class: "role-page-table"},
					html.Thead(html.Props{}, html.Tr(html.Props{}, html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.page"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.view"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.create_permission"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.update_permission"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.delete_permission"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(props.Text("roles.action"))))),
					html.Tbody(html.Props{}, rows...),
				),
			),
		),
	)
}

type RolePagePermissionRowProps struct {
	I18nProps
	Page       RolePageOption
	Permission RolePagePermission
	Editable   bool
	Save       func(RolePagePermission)
}

// rolePermissionDraftState is the row's client draft plus the server
// snapshot it was seeded from. Local edits live in the booleans; the
// key never carries edits, so re-renders keep the draft while drift
// resets it.
type rolePermissionDraftState struct {
	key                          RolePagePermission
	view, create, update, remove bool
}

// permissionSnapshot extracts the server-addressed grant snapshot a
// draft reconciles against: role, page, version, and the four grants.
func permissionSnapshot(permission RolePagePermission) RolePagePermission {
	return RolePagePermission{Version: permission.Version, RoleID: permission.RoleID, Page: permission.Page,
		View: permission.View, Create: permission.Create, Update: permission.Update, Delete: permission.Delete}
}

// reconcileRolePermissionDraft keeps local edits while the incoming
// projection matches the seeded snapshot and resets to the incoming
// grants on drift: a version bump, changed grants, or another role or
// page. The second result reports whether the draft was replaced, so
// the component can invalidate its client cache exactly on drift.
func reconcileRolePermissionDraft(incoming RolePagePermission, current rolePermissionDraftState) (rolePermissionDraftState, bool) {
	key := permissionSnapshot(incoming)
	if current.key == key {
		return current, false
	}
	return rolePermissionDraftState{key: key, view: incoming.View, create: incoming.Create, update: incoming.Update, remove: incoming.Delete}, true
}

// RolePagePermissionRow owns draft checkbox state so dependent CRUD controls
// update immediately and never submit a mutation grant without page access.
// The draft reconciles against the incoming projection: local edits survive
// re-renders, authority drift resets to the server grants.
func RolePagePermissionRow(props RolePagePermissionRowProps) ui.Node {
	seeded := rolePermissionDraftState{key: permissionSnapshot(props.Permission),
		view: props.Permission.View, create: props.Permission.Create, update: props.Permission.Update, remove: props.Permission.Delete}
	state := ui.UseState(seeded)
	draft, reset := reconcileRolePermissionDraft(props.Permission, state.Get())
	if reset {
		state.Set(draft)
	}
	toggle := func(action string) {
		next := state.Get()
		switch action {
		case "view":
			next.view = !next.view
			if !next.view {
				next.create, next.update, next.remove = false, false, false
			}
		case "create":
			next.create = !next.create
			if next.create {
				next.view = true
			}
		case "update":
			next.update = !next.update
			if next.update {
				next.view = true
			}
		case "delete":
			next.remove = !next.remove
			if next.remove {
				next.view = true
			}
		}
		state.Set(next)
	}
	checked := map[string]bool{"view": draft.view, "create": draft.create, "update": draft.update, "delete": draft.remove}
	id := func(action string) string {
		return "role-page-" + props.Permission.RoleID + "-" + string(props.Page.ID) + "-" + action
	}
	check := func(action, label string) ui.Node {
		input := html.Props{ID: id(action), Type: "checkbox", Checked: checked[action], Disabled: !props.Editable, Aria: map[string]string{"label": label + " — " + props.Page.Label}}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) { toggle(action) })
		return html.Label(html.Props{For: id(action), Class: "permission-check"}, html.Input(input), html.Span(html.Props{Class: "sr-only"}, ui.Text(label)))
	}
	saved := props.Permission
	saved.View, saved.Create, saved.Update, saved.Delete = draft.view, draft.create, draft.update, draft.remove
	return html.Tr(html.Props{},
		html.Th(html.Props{Raw: map[string]any{"scope": "row"}}, html.Strong(html.Props{}, ui.Text(props.Page.Label)), html.Small(html.Props{Class: "muted"}, ui.Text(props.Page.Description))),
		html.Td(html.Props{}, check("view", props.Text("roles.view"))),
		html.Td(html.Props{}, check("create", props.Text("roles.create_permission"))),
		html.Td(html.Props{}, check("update", props.Text("roles.update_permission"))),
		html.Td(html.Props{}, check("delete", props.Text("roles.delete_permission"))),
		html.Td(html.Props{}, permissionSaveButton(props, saved)),
	)
}

func permissionSaveButton(props RolePagePermissionRowProps, draft RolePagePermission) ui.Node {
	statusID := "role-page-permission-status-" + draft.RoleID + "-" + string(draft.Page)
	if !props.Editable || props.Save == nil {
		return html.Span(html.Props{Class: "muted permission-read-only"}, ui.Text(props.Text("roles.read_only")))
	}
	return html.Form(html.Props{OnSubmit: savePagePermission(props.Save, draft)}, html.Button(html.Props{Class: "button secondary compact", Type: "submit"}, ui.Text(props.Text("roles.save_page"))), html.Span(html.Props{ID: statusID, Class: "sr-only", Raw: map[string]any{"role": "status", "aria-live": "polite"}}))
}

func submitRoleFilter(navigate func(string), action string, query func() string) ui.Handler {
	if navigate == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if query == nil {
			return
		}
		navigate(roleFilterHref(action, query()))
	})
}

func roleFilterHref(action, query string) string {
	parsed, err := url.Parse(action)
	if err != nil {
		return action
	}
	values := parsed.Query()
	values.Set("q", strings.TrimSpace(query))
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func (props RolesPageProps) BackNode() ui.Node { return ui.CreateElement(ActionLink, props.Back) }

func createRoleForm(i18n I18nProps, editable bool, save func(AccessRole)) ui.Node {
	if !editable || save == nil {
		return html.Div(html.Props{Class: "create-role-form role-read-only"}, html.Strong(html.Props{}, ui.Text(i18n.Text("roles.read_only_heading"))), html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text("roles.read_only_help"))))
	}
	draft := AccessRole{Active: true}
	id := html.Props{ID: "new-role-id", Name: "role_id", Type: "text", Pattern: "[a-z][a-z0-9_]{1,62}", AutoComplete: "off", Required: true}
	id.OnInput = ui.UseEvent(func(event ui.InputEvent) { draft.ID = event.GetValue() })
	name := html.Props{ID: "new-role-name", Name: "role_name", Type: "text", MaxLength: 96, AutoComplete: "off", Required: true}
	name.OnInput = ui.UseEvent(func(event ui.InputEvent) { draft.Name = event.GetValue() })
	description := html.Props{ID: "new-role-description", Name: "role_description", MaxLength: 400}
	description.OnInput = ui.UseEvent(func(event ui.InputEvent) { draft.Description = event.GetValue() })
	return html.Form(html.Props{Class: "create-role-form", OnSubmit: saveRole(save, &draft)},
		html.H3(html.Props{}, ui.Text(i18n.Text("roles.create"))),
		html.Div(html.Props{Class: "create-role-fields"},
			html.Label(html.Props{For: "new-role-id"}, html.Span(html.Props{}, ui.Text(i18n.Text("roles.role_id"))), html.Input(id), html.Small(html.Props{}, ui.Text(i18n.Text("roles.role_id_help")))),
			html.Label(html.Props{For: "new-role-name"}, html.Span(html.Props{}, ui.Text(i18n.Text("roles.display_name"))), html.Input(name)),
			html.Label(html.Props{For: "new-role-description"}, html.Span(html.Props{}, ui.Text(i18n.Text("roles.description_label"))), html.Textarea(description)),
		),
		html.Div(html.Props{Class: "role-form-actions"}, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(i18n.Text("roles.create_action"))), html.P(html.Props{ID: "role-access-status", Class: "muted", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(i18n.Text("roles.status")))),
	)
}

func workerRoleAssignmentEditor(i18n I18nProps, person Person, roles []AccessRole, assignment WorkerRoleAssignment, editable bool, save func(WorkerRoleAssignment)) ui.Node {
	selected := make(map[string]bool, len(assignment.RoleIDs))
	for _, roleID := range assignment.RoleIDs {
		selected[roleID] = true
	}
	choices := make([]ui.Node, 0, len(roles))
	for _, role := range roles {
		if !role.Active {
			continue
		}
		roleID := role.ID
		input := html.Props{Type: "checkbox", Name: "role", Value: roleID, Checked: selected[roleID], Disabled: !editable, Aria: map[string]string{"label": role.Name}}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) {
			selected[roleID] = !selected[roleID]
			assignment.RoleIDs = assignment.RoleIDs[:0]
			for _, candidate := range roles {
				if selected[candidate.ID] {
					assignment.RoleIDs = append(assignment.RoleIDs, candidate.ID)
				}
			}
		})
		choices = append(choices, html.Label(html.Props{Class: "employee-role-choice"}, html.Input(input), html.Span(html.Props{}, ui.Text(role.Name))))
	}
	badges := make([]ui.Node, 0, len(assignment.RoleIDs))
	for _, roleID := range assignment.RoleIDs {
		label := roleID
		for _, role := range roles {
			if role.ID == roleID && strings.TrimSpace(role.Name) != "" {
				label = role.Name
				break
			}
		}
		badges = append(badges, html.Span(html.Props{Class: "status"}, ui.Text(label)))
	}
	if len(badges) == 0 {
		badges = append(badges, html.Span(html.Props{Class: "muted"}, ui.Text(i18n.Text("roles.no_explicit_assignment"))))
	}
	actions := []ui.Node{html.Small(html.Props{Class: "muted"}, ui.Text(i18n.Text("roles.read_only")))}
	if editable && save != nil {
		actions = []ui.Node{html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(i18n.Text("roles.save_employee"))), html.Small(html.Props{Class: "muted"}, ui.Text(i18n.Text("roles.at_least_one")))}
	}
	return html.Tag("details", html.Props{Class: "employee-role-editor"},
		html.Tag("summary", html.Props{}, personAvatar(person.Name, person.Name, person.PhotoURL, "small"), html.Div(html.Props{Class: "employee-role-identity"}, html.Strong(html.Props{}, ui.Text(person.Name)), html.Small(html.Props{Class: "muted"}, ui.Text(person.Role+" · "+person.Team))), html.Div(html.Props{Class: "employee-role-badges"}, badges...)),
		html.Form(html.Props{Class: "employee-role-form", OnSubmit: saveAssignment(save, &assignment)}, html.Fieldset(html.Props{}, html.Legend(html.Props{}, ui.Text(i18n.Text("roles.assigned_roles"))), html.Div(html.Props{Class: "employee-role-choices"}, choices...)), html.Div(html.Props{Class: "role-form-actions"}, actions...)),
	)
}

func savePagePermission(save func(RolePagePermission), draft RolePagePermission) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(draft) })
}

func saveRole(save func(AccessRole), draft *AccessRole) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(*draft) })
}

func saveAssignment(save func(WorkerRoleAssignment), draft *WorkerRoleAssignment) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(*draft) })
}
