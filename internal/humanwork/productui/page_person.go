package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// personPage is the route adapter. It resolves authorized projection data and
// passes presentation-only props into the reusable component tree.
func personPage(view View) ui.Node {
	returnHref := peopleReturnHref(view)
	props := PersonPageProps{I18nProps: I18nProps{Locale: view.Locale}, BackHref: returnHref, Navigate: view.Navigate}
	person, ok := exactPerson(view)
	if !ok {
		props.Unavailable = PersonUnavailableProps{DirectoryHref: returnHref, Navigate: view.Navigate}
		return ui.CreateElement(PersonPage, props)
	}

	profile := personProfileProps(view, person, PagePerson)
	props.Profile = &profile
	return ui.CreateElement(PersonPage, props)
}

// personProfileProps is the shared adapter from one authorized worker row to
// the narrow profile component contract. Person and self-service routes use
// the same facts and composition; only their navigation state differs.
func personProfileProps(view View, person Person, target PageID) PersonProfileProps {
	text := view.Locale.Text
	value := func(raw string) string { return valueOrUnavailableFor(view.Locale, raw) }
	return PersonProfileProps{
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
				{Label: text("person.bonus_target"), Value: percentage(view.Locale, person.BonusTarget)},
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
		Workflows: personWorkflowLauncherProps(view, person, target),
		History: workflowHistoryPropsForTarget(view, person.ID, target, text("work.past"),
			text("person.history_detail", map[string]string{"name": person.Name}), true),
	}
}

func personWorkflowLauncherProps(view View, person Person, target PageID) WorkflowLauncherProps {
	filtered := filteredPersonWorkflows(view)
	if len(view.EffectivePermissions) > 0 && !view.Can(PageJourneys, "create") {
		filtered = nil
	}
	workflows := make([]WorkflowCardProps, 0, len(filtered))
	for _, workflow := range filtered {
		href := workflow.Href
		if workflow.LaunchHref != nil {
			href = workflow.LaunchHref(person.ID)
		}
		workflows = append(workflows, WorkflowCardProps{
			Name: workflow.Name, Category: workflow.Category, Description: workflow.Description, Href: href, Navigate: view.Navigate,
		})
	}
	personID := person.ID
	if target == PageMyself {
		// The self-service route always derives its worker from Viewer.PersonID;
		// it does not accept an address-bar worker selector.
		personID = ""
	}
	filter := WorkflowFilterProps{
		Query: view.WorkflowQuery, Action: pageHref(target), PersonID: personID,
		DirectoryQuery: view.Query, DirectoryPage: view.PeoplePage, DirectoryTeam: view.PeopleTeam, DirectoryLocation: view.PeopleLocation,
		DirectorySort: view.PeopleSort, DirectoryDirection: view.PeopleDirection, NavCollapsed: view.NavCollapsed,
	}
	if view.Navigate != nil {
		filter.OnFilter = func(query string) {
			if target == PageMyself {
				view.Navigate(statefulHref(view, PageMyself, "workflow_q", query))
				return
			}
			view.Navigate(statefulHref(view, target, "person", person.ID, "q", view.Query,
				"team", view.PeopleTeam, "location", view.PeopleLocation, "sort", view.PeopleSort, "dir", view.PeopleDirection,
				"page", peoplePageValue(view.PeoplePage), "workflow_q", query))
		}
	}
	return WorkflowLauncherProps{
		PersonName: person.Name, TotalCount: len(filtered), Filter: filter, Workflows: workflows,
	}
}

func peopleReturnHref(view View) string {
	return peopleDirectoryHref(view, view.PeoplePage, view.Query, view.PeopleTeam, view.PeopleLocation, view.PeopleSort, view.PeopleDirection)
}
