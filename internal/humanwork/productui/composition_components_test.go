package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestWorkComponentsRenderWithoutPageProjection(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(WorkPage, WorkPageProps{
		Collection: WorkCollectionProps{
			Title: "Promotion journeys", CountLabel: "1 item",
			Tabs:   []WorkTabProps{{Label: "All work", Href: "/work", Active: true}},
			Rows:   []WorkRowProps{{Initials: "AP", Title: "Promotion", Person: "Avery Patel", Summary: "Director", Href: "/work/one", Selected: true}},
			Footer: WorkCollectionFooterProps{Label: "Authorized work", Action: ActionLinkProps{Label: "View My Work →", Href: "/work"}},
		},
		Preview: WorkPreviewProps{
			Initials: "AP", Title: "Promotion", Person: "Avery Patel", Summary: "Director", FactsTitle: "Proposal",
			Facts: []FactProps{{Label: "Effective date", Value: "2026-10-01"}}, Action: ActionLinkProps{Label: "Open live journey", Href: "/journey", Class: "button primary full"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Promotion journeys", "1 item", "Avery Patel", "Lifecycle status is not available in this view", "Effective date", "2026-10-01", `href="/journey"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("standalone Work composition missing %q", want)
		}
	}
}

func TestSharedActionLinkUsesSoftwareNavigationOnlyInsideTheProduct(t *testing.T) {
	navigate := func(string) {}
	internal := ActionLink(ActionLinkProps{Label: "People", Href: "/workspace/app/people", Navigate: navigate})
	if internal == nil || internal.Props["onclick"] == nil {
		t.Fatal("shared action link did not install software navigation for an internal product route")
	}
	external := ActionLink(ActionLinkProps{Label: "Journey", Href: "/workspace/journey#/journeys", Navigate: navigate})
	if external != nil && external.Props["onclick"] != nil {
		t.Fatal("shared action link intercepted a cross-application route")
	}
}

func TestEnterprisePageCompositionsRenderFromNarrowProps(t *testing.T) {
	nodes := []ui.Node{
		ui.CreateElement(HomePage, HomePageProps{Work: WorkCollectionProps{Footer: WorkCollectionFooterProps{}}, Overview: SummaryCardProps{Title: "Overview"}, QuickStart: QuickActionsProps{Title: "Start"}, Recent: RecentActivityProps{Title: "Recent"}}),
		ui.CreateElement(OrganizationPage, OrganizationPageProps{Title: "Organization", Groups: []OrganizationGroupProps{{Name: "Product", Count: 3}}}),
		ui.CreateElement(InsightsPage, InsightsPageProps{Metrics: []MetricProps{{Label: "Active", Value: "3", Note: "Live"}}, Attention: AttentionPanelProps{Title: "Attention"}}),
		ui.CreateElement(AdminPage, AdminPageProps{Hero: AdminHeroProps{Title: "Cell"}, Capabilities: []CapabilityCardProps{{Title: "Journey service", State: "Connected"}}}),
		ui.CreateElement(HelpPage, HelpPageProps{Guidance: QuickActionsProps{Title: "Guidance"}, Support: InformationalPanelProps{Title: "Support"}}),
		ui.CreateElement(SettingsPage, SettingsPageProps{Access: AccessContextProps{Title: "Access", Facts: []FactProps{{Label: "Principal", Value: "Taylor"}}}, Preferences: EmptyStateProps{Title: "Preferences"}}),
		ui.CreateElement(StudioPage, StudioPageProps{Back: ActionLinkProps{Label: "Back", Href: "/admin"}, State: EmptyStateProps{Title: "Unavailable"}}),
	}
	for index, node := range nodes {
		if _, err := ui.RenderToString(node); err != nil {
			t.Fatalf("composition %d did not render independently: %v", index, err)
		}
	}
}
