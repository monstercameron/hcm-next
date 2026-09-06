package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func homePage(view View) ui.Node {
	active, terminal := journeyCounts(view.Work)
	activities := make([]ActivityProps, 0)
	for _, item := range view.Work {
		if item.Terminal {
			activities = append(activities, ActivityProps{Title: item.Title, Detail: item.Person, When: item.Status})
		}
	}
	return ui.CreateElement(HomePage, HomePageProps{
		Work: workCollectionProps(view, workCollectionOptions{Title: "Open promotion work"}),
		Overview: SummaryCardProps{Title: "Promotion journeys", Facts: []FactProps{
			{Label: "Active", Value: fmt.Sprint(active)},
			{Label: "Terminal", Value: fmt.Sprint(terminal)},
			{Label: "Visible workers", Value: fmt.Sprint(len(view.People))},
		}},
		QuickStart: QuickActionsProps{Title: "Start something", Actions: []ActionLinkProps{
			{Label: "Start a promotion", Href: statefulHref(view, PageJourneys), Class: "button primary", Navigate: view.Navigate},
			{Label: "Choose a worker", Href: statefulHref(view, PagePeople), Class: "button secondary", Navigate: view.Navigate},
		}},
		Recent: RecentActivityProps{Title: "Recently completed", Items: activities, EmptyTitle: "No completed journeys", EmptyDescription: "Completed or rejected journeys will appear here after the server records them."},
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
