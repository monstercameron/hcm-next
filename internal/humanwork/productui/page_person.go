package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// personPage is the route adapter. It resolves authorized projection data and
// passes presentation-only props into the reusable component tree.
func personPage(view View) ui.Node {
	returnHref := peopleReturnHref(view)
	text := view.Locale.Text
	value := func(raw string) string { return valueOrUnavailableFor(view.Locale, raw) }
	props := PersonPageProps{I18nProps: I18nProps{Locale: view.Locale}, BackHref: returnHref, Navigate: view.Navigate}
	person, ok := exactPerson(view)
	if !ok {
		props.Unavailable = PersonUnavailableProps{DirectoryHref: returnHref, Navigate: view.Navigate}
		return ui.CreateElement(PersonPage, props)
	}

	props.Profile = &PersonProfileProps{
		Hero: PersonHeroProps{
			Initials: person.Initials, PhotoURL: person.PhotoURL, Name: person.Name, Role: value(person.Role),
			Status: text("person.visible_scope"), Source: value(person.Source),
		},
		Details: EmploymentDetailsProps{
			Title: text("person.employment_overview"), Description: text("person.employment_overview_detail"),
			Facts: []ProfileFactProps{
				{Label: text("person.worker_number"), Value: value(person.WorkerNumber)},
				{Label: text("person.job_code"), Value: value(person.JobCode)},
				{Label: text("person.job_level"), Value: value(person.Grade)},
				{Label: text("person.hire_date"), Value: value(person.HireDate)},
				{Label: text("person.employment_type"), Value: value("")},
				{Label: text("person.time_type"), Value: value("")},
				{Label: text("person.record_source"), Value: value(person.Source)},
				{Label: text("person.record_created"), Value: value(person.CreatedAt)},
			}},
		Organization: EmploymentDetailsProps{
			Title: text("person.organization"), Description: text("person.organization_detail"), Class: "organization-details",
			Facts: []ProfileFactProps{
				{Label: text("person.organization_unit"), Value: value(person.Team)},
				{Label: text("person.manager"), Value: value(person.Manager)},
				{Label: text("person.position_id"), Value: value(person.PositionID)},
				{Label: text("person.work_location"), Value: value(person.Location)},
				{Label: text("person.company"), Value: value("")},
				{Label: text("person.business_unit"), Value: value("")},
				{Label: text("person.cost_center"), Value: value("")},
				{Label: text("person.work_arrangement"), Value: value("")},
			},
		},
		Compensation: EmploymentDetailsProps{
			Title: text("person.compensation"), Description: text("person.compensation_detail"), Class: "compensation-details",
			Facts: []ProfileFactProps{
				{Label: text("person.base_pay"), Value: money(view.Locale, person.BasePay)},
				{Label: text("person.bonus_target"), Value: value(person.BonusTarget)},
				{Label: text("person.pay_zone"), Value: value(person.PayZone)},
				{Label: text("person.pay_frequency"), Value: value("")},
			}},
		Personal: SensitiveDetailsProps{
			Title: text("person.personal_information"), Description: text("person.personal_hidden"), Badge: text("person.restricted"),
			Facts: []ProfileFactProps{
				{Label: text("person.legal_name"), Value: value(person.LegalName)},
				{Label: text("person.preferred_name"), Value: value(person.PreferredName)},
				{Label: text("person.worker_id"), Value: value(person.WorkerID)},
				{Label: text("person.worker_ref"), Value: value(person.ID)},
			},
		},
		Workflows: personWorkflowLauncherProps(view, person),
		History: workflowHistoryProps(view, person.ID, text("work.past"),
			text("person.history_detail", map[string]string{"name": person.Name}), true),
	}
	return ui.CreateElement(PersonPage, props)
}

func personWorkflowLauncherProps(view View, person Person) WorkflowLauncherProps {
	filtered := filteredPersonWorkflows(view)
	workflows := make([]WorkflowCardProps, 0, len(filtered))
	for _, workflow := range filtered {
		workflows = append(workflows, WorkflowCardProps{
			Name: workflow.Name, Category: workflow.Category, Description: workflow.Description, Href: workflow.Href, Navigate: view.Navigate,
		})
	}
	filter := WorkflowFilterProps{
		Query: view.WorkflowQuery, Action: pageHref(PagePerson), PersonID: person.ID,
		DirectoryQuery: view.Query, DirectoryPage: view.PeoplePage, DirectoryTeam: view.PeopleTeam, DirectoryLocation: view.PeopleLocation,
		DirectorySort: view.PeopleSort, DirectoryDirection: view.PeopleDirection, NavCollapsed: view.NavCollapsed,
	}
	if view.Navigate != nil {
		filter.OnFilter = func(query string) {
			view.Navigate(statefulHref(view, PagePerson, "person", person.ID, "q", view.Query,
				"team", view.PeopleTeam, "location", view.PeopleLocation, "sort", view.PeopleSort, "dir", view.PeopleDirection,
				"page", peoplePageValue(view.PeoplePage), "workflow_q", query))
		}
	}
	return WorkflowLauncherProps{
		PersonName: person.Name, TotalCount: len(view.PersonWorkflows), Filter: filter, Workflows: workflows,
	}
}

func peopleReturnHref(view View) string {
	return peopleDirectoryHref(view, view.PeoplePage, view.Query, view.PeopleTeam, view.PeopleLocation, view.PeopleSort, view.PeopleDirection)
}
