package productui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func organizationPage(view View) ui.Node {
	members := map[string][]OwnershipNodeProps{}
	locations := map[string]bool{}
	payZones := map[string]bool{}
	for _, person := range view.People {
		team := valueOrUnavailable(person.Team)
		members[team] = append(members[team], ownershipPerson(view, person))
		if value := strings.TrimSpace(person.Location); value != "" {
			locations[value] = true
		}
		if value := strings.TrimSpace(person.PayZone); value != "" {
			payZones[value] = true
		}
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	groups := make([]OrganizationGroupProps, 0, len(names))
	for _, name := range names {
		sort.SliceStable(members[name], func(i, j int) bool {
			return strings.ToLower(members[name][i].Name) < strings.ToLower(members[name][j].Name)
		})
		groups = append(groups, OrganizationGroupProps{Name: name, Count: len(members[name]), CountLabel: organizationCountLabel(view, "organization.members_count", len(members[name])), Members: members[name]})
	}
	visibleLocations := sortedOrganizationValues(locations)
	number := func(value int) string { return view.Locale.FormatNumber(strconv.Itoa(value), 0) }
	return ui.CreateElement(OrganizationPage, OrganizationPageProps{
		Title: view.Locale.Text("organization.structure_title"), Description: view.Locale.Text("organization.structure_description"), Groups: groups,
		ViewLabel: view.Locale.Text("organization.view_label"), TreeActive: view.OrganizationView == organizationViewTree,
		FlatAction: ActionLinkProps{Label: view.Locale.Text("organization.view_flat"), Href: statefulHref(view, PageOrganization, "org_view", organizationViewFlat), Class: "organization-view-option", Navigate: view.Navigate},
		TreeAction: ActionLinkProps{Label: view.Locale.Text("organization.view_tree"), Href: statefulHref(view, PageOrganization, "org_view", organizationViewTree), Class: "organization-view-option", Navigate: view.Navigate},
		Tree:       ownershipTree(view),
		Metadata: BusinessMetadataProps{
			Title: view.Locale.Text("organization.metadata_title"), Description: view.Locale.Text("organization.metadata_description"),
			Items: []BusinessMetadataItemProps{
				{Label: view.Locale.Text("organization.business_name"), Value: valueOrUnavailableFor(view.Locale, view.Tenant)},
				{Label: view.Locale.Text("organization.visible_workforce"), Value: number(len(view.People))},
				{Label: view.Locale.Text("organization.units"), Value: number(len(groups))},
				{Label: view.Locale.Text("organization.locations"), Value: number(len(locations))},
				{Label: view.Locale.Text("organization.pay_zones"), Value: number(len(payZones))},
				{Label: view.Locale.Text("organization.access_scope"), Value: valueOrUnavailableFor(view.Locale, view.Scope)},
			},
			FootprintLabel: view.Locale.Text("organization.footprint"), Footprint: visibleLocations,
			Boundary: view.Locale.Text("organization.metadata_boundary"),
		},
		Empty: EmptyStateProps{Title: view.Locale.Text("organization.empty_title"), Description: view.Locale.Text("organization.empty_description")},
	})
}

const (
	organizationViewFlat = "flat"
	organizationViewTree = "tree"
)

func normalizeOrganizationView(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), organizationViewTree) {
		return organizationViewTree
	}
	return organizationViewFlat
}

func ownershipTree(view View) []OwnershipNodeProps {
	people := append([]Person(nil), view.People...)
	sort.SliceStable(people, func(i, j int) bool { return strings.ToLower(people[i].Name) < strings.ToLower(people[j].Name) })
	byName := make(map[string]int, len(people))
	nameCount := make(map[string]int, len(people))
	for index, person := range people {
		name := strings.ToLower(strings.TrimSpace(person.Name))
		byName[name] = index
		nameCount[name]++
	}
	children := make(map[int][]int)
	roots := make([]int, 0)
	for index, person := range people {
		name := strings.ToLower(strings.TrimSpace(person.Manager))
		manager, ok := byName[name]
		if !ok || name == "" || nameCount[name] != 1 || manager == index {
			roots = append(roots, index)
			continue
		}
		children[manager] = append(children[manager], index)
	}
	visiting, emitted := map[int]bool{}, map[int]bool{}
	var build func(int) OwnershipNodeProps
	build = func(index int) OwnershipNodeProps {
		person := people[index]
		node := ownershipPerson(view, person)
		if visiting[index] {
			return node
		}
		visiting[index], emitted[index] = true, true
		for _, child := range children[index] {
			if !visiting[child] {
				node.Reports = append(node.Reports, build(child))
			}
		}
		delete(visiting, index)
		node.ReportsLabel = organizationCountLabel(view, "organization.reports_count", len(node.Reports))
		return node
	}
	result := make([]OwnershipNodeProps, 0, len(roots))
	for _, root := range roots {
		result = append(result, build(root))
	}
	// Corrupt cycles remain visible as extra roots; admitted people are never
	// silently dropped just because a reporting relationship is malformed.
	for index := range people {
		if !emitted[index] {
			result = append(result, build(index))
		}
	}
	return result
}

func ownershipPerson(view View, person Person) OwnershipNodeProps {
	current := person.ID != "" && person.ID == view.Viewer.PersonID
	href := ""
	if PageVisible(PagePerson, view.Roles) {
		href = statefulHref(view, PagePerson, "person", person.ID)
	} else if current {
		href = statefulHref(view, PageMyself)
	}
	return OwnershipNodeProps{Name: person.Name, WorkerNumber: person.WorkerNumber, Role: person.Role, Team: person.Team, Initials: person.Initials, PhotoURL: person.PhotoURL, Href: href, Navigate: view.Navigate, Current: current}
}

func organizationCountLabel(view View, key string, count int) string {
	return fmt.Sprintf(view.Locale.Text(key), view.Locale.FormatNumber(strconv.Itoa(count), 0))
}

func sortedOrganizationValues(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
