package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func homePage(view View) ui.Node {
	// Counts, recent activity, and the collection draw from the admitted
	// population, so denied journeys appear nowhere on home.
	population := admittedWork(view)
	active, terminal := journeyCounts(population)
	activities := make([]ActivityProps, 0)
	for _, item := range population {
		if item.Terminal {
			activities = append(activities, ActivityProps{Title: item.Title, Detail: item.Person, When: item.Status})
		}
	}
	scoped := view
	scoped.Work = population
	work := workCollectionProps(scoped, workCollectionOptions{Title: "Open promotion work"})
	if !view.Allows(PageWork, "view") {
		work.Footer.Action = ActionLinkProps{}
	}
	actions := make([]ActionLinkProps, 0, 2)
	if view.Allows(PageJourneys, "create") {
		actions = append(actions, ActionLinkProps{Label: "Start a promotion", Href: statefulHref(view, PageJourneys), Class: "button primary", Navigate: view.Navigate})
	}
	if view.Allows(PagePeople, "view") {
		actions = append(actions, ActionLinkProps{Label: "Choose a worker", Href: statefulHref(view, PagePeople), Class: "button secondary", Navigate: view.Navigate})
	}
	return ui.CreateElement(HomePage, HomePageProps{
		Work:     work,
		ShowWork: view.Allows(PageWork, "view"),
		Overview: SummaryCardProps{Title: "Promotion journeys", Facts: []FactProps{
			{Label: "Active", Value: fmt.Sprint(active)},
			{Label: "Terminal", Value: fmt.Sprint(terminal)},
			{Label: "Visible workers", Value: fmt.Sprint(len(view.People))},
		}},
		QuickStart: QuickActionsProps{Title: "Start something", Actions: actions},
		Recent:     RecentActivityProps{Title: "Recently completed", Items: activities, EmptyTitle: "No completed journeys", EmptyDescription: "Completed or rejected journeys will appear here after the server records them."},
	})
}

func journeyCounts(items []WorkItem) (active, terminal int) {
	for _, item := range items {
		if item.Terminal {
			terminal++
		} else {
			active++
		}
	}
	return active, terminal
}
