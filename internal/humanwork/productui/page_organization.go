package productui

import (
	"sort"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func organizationPage(view View) ui.Node {
	counts := map[string]int{}
	for _, person := range view.People {
		counts[valueOrUnavailable(person.Team)]++
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	groups := make([]OrganizationGroupProps, 0, len(names))
	for _, name := range names {
		groups = append(groups, OrganizationGroupProps{Name: name, Count: counts[name]})
	}
	return ui.CreateElement(OrganizationPage, OrganizationPageProps{
		Title: "Workforce by organization", Description: "Counts use only workers returned by JourneyService.ListWorkers. Reporting lines are not inferred.", Groups: groups,
		Empty: EmptyStateProps{Title: "No organization members returned", Description: "The live worker projection returned no workers visible to this session."},
	})
}
