package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func helpPage(view View) ui.Node {
	actions := make([]ActionLinkProps, 0, 6)
	for _, task := range []struct {
		page PageID
		key  string
	}{
		{PageMyself, "help.my_profile"},
		{PageOrganization, "help.organization"},
		{PagePeople, "help.choose_employee"},
		{PageJourneys, "help.review_requests"},
		{PageHistory, "help.past_decisions"},
		{PageSettings, "help.account_settings"},
	} {
		if view.Allows(task.page, "view") {
			actions = append(actions, ActionLinkProps{Label: view.Locale.Text(task.key), Href: statefulHref(view, task.page), Navigate: view.Navigate})
		}
	}
	support := InformationalPanelProps{Title: view.Locale.Text("help.access_title"), Class: "support-summary", Description: view.Locale.Text("help.access_detail")}
	if view.Allows(PageJourneys, "create") {
		support.Title = view.Locale.Text("help.promotion_title")
		support.Description = view.Locale.Text("help.promotion_detail")
	}
	return ui.CreateElement(HelpPage, HelpPageProps{
		Guidance: QuickActionsProps{Title: view.Locale.Text("help.tasks"), Actions: actions},
		Support:  support,
	})
}
