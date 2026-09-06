package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// myselfPage resolves self-service data exclusively through the worker
// identity bound to the admitted principal. Query parameters cannot select a
// different person on this route.
func myselfPage(view View) ui.Node {
	props := MyselfPageProps{I18nProps: I18nProps{Locale: view.Locale}}
	person, ok := viewerPerson(view)
	if !ok {
		return ui.CreateElement(MyselfPage, props)
	}
	profile := personProfileProps(view, person, PageMyself)
	profile.Hero.Status = view.Locale.Text("myself.self_service")
	profile.Compensation.Title = view.Locale.Text("myself.payroll_title")
	profile.Compensation.Description = view.Locale.Text("myself.payroll_detail")
	profile.Compensation.Notice = view.Locale.Text("myself.payroll_boundary")
	profile.History.Title = view.Locale.Text("myself.history_title")
	profile.History.Description = view.Locale.Text("myself.history_detail")
	props.Profile = &profile
	props.OrganizationTitle = view.Locale.Text("myself.organization_title")
	props.OrganizationDescription = view.Locale.Text("myself.organization_detail")
	props.OrganizationTree = ownershipTree(view)
	return ui.CreateElement(MyselfPage, props)
}

func viewerPerson(view View) (Person, bool) {
	for _, person := range view.People {
		if person.ID != "" && person.ID == view.Viewer.PersonID {
			return person, true
		}
	}
	return Person{}, false
}
