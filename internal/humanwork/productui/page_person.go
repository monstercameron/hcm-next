package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// personPage is the route adapter. It resolves authorized projection data and
// passes presentation-only props into the reusable component tree.
func personPage(view View) ui.Node {
	returnHref := peopleReturnHref(view)
	props := PersonPageProps{I18nProps: I18nProps{Locale: view.Locale}, BackHref: returnHref, Navigate: view.Navigate}
	person, ok := exactPerson(view)
	if !ok || !DiscoveryAdmitted(person.ID, view.RecordVerdicts) {
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
	// field renders one fact through the record's field verdict once the
	// server sends verdicts; a silent server keeps the current values.
	// Allowed values project identically to value(), so governing changes
	// nothing until a verdict actually withholds.
	fields := view.RecordVerdicts[person.ID].Fields
	field := func(name, raw string) string {
		if len(view.RecordVerdicts) == 0 {
			return value(raw)
		}
		return ProjectAuthorizedValue(view.Locale, raw, fields[name]).Text
	}
	return PersonProfileProps{
		Hero: PersonHeroProps{
			Initials: person.Initials, PhotoURL: person.PhotoURL, Name: field("name", person.Name), Role: field("role", person.Role),
			Status: text("person.visible_scope"), Source: field("source", person.Source),
		},
		Details: EmploymentDetailsProps{
			Title: text("person.employment_overview"), Description: text("person.employment_overview_detail"),
			Facts: []ProfileFactProps{
				{Label: text("person.worker_number"), Value: field("worker_number", person.WorkerNumber)},
				{Label: text("person.job_code"), Value: field("job_code", person.JobCode)},
				{Label: text("person.job_level"), Value: field("job_level", person.Grade)},
				{Label: text("person.hire_date"), Value: field("hire_date", person.HireDate)},
				{Label: text("person.employment_type"), Value: field("employment_type", "")},
				{Label: text("person.time_type"), Value: field("time_type", "")},
				{Label: text("person.record_source"), Value: field("record_source", person.Source)},
				{Label: text("person.record_created"), Value: field("record_created", person.CreatedAt)},
			}},
		Organization: EmploymentDetailsProps{
			Title: text("person.organization"), Description: text("person.organization_detail"), Class: "organization-details",
			Facts: []ProfileFactProps{
				{Label: text("person.organization_unit"), Value: field("organization_unit", person.Team)},
				{Label: text("person.manager"), Value: field("manager", person.Manager)},
				{Label: text("person.position_id"), Value: field("position_id", person.PositionID)},
				{Label: text("person.work_location"), Value: field("work_location", person.Location)},
				{Label: text("person.company"), Value: field("company", "")},
				{Label: text("person.business_unit"), Value: field("business_unit", "")},
				{Label: text("person.cost_center"), Value: field("cost_center", "")},
				{Label: text("person.work_arrangement"), Value: field("work_arrangement", "")},
			},
		},
		Compensation: EmploymentDetailsProps{
			Title: text("person.compensation"), Description: text("person.compensation_detail"), Class: "compensation-details",
			Facts: []ProfileFactProps{
				{Label: text("person.base_pay"), Value: field("base_pay", money(view.Locale, person.BasePay))},
				{Label: text("person.bonus_target"), Value: field("bonus_target", percentage(view.Locale, person.BonusTarget))},
				{Label: text("person.pay_zone"), Value: field("pay_zone", person.PayZone)},
				{Label: text("person.pay_frequency"), Value: field("pay_frequency", "")},
			}},
		Personal: SensitiveDetailsProps{
			Title: text("person.personal_information"), Description: text("person.personal_hidden"), Badge: text("person.restricted"),
			Facts: []ProfileFactProps{
				{Label: text("person.legal_name"), Value: field("legal_name", person.LegalName)},
				{Label: text("person.preferred_name"), Value: field("preferred_name", person.PreferredName)},
				{Label: text("person.worker_id"), Value: field("worker_id", person.WorkerID)},
				{Label: text("person.worker_ref"), Value: field("record_id", person.ID)},
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
