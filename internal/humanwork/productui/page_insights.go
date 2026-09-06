package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func insightsPage(view View) ui.Node {
	active, terminal := journeyCounts(view.Work)
	attention := 0
	for _, item := range view.Work {
		if item.Status == "Awaiting approval" || item.Status == "Blocked" {
			attention++
		}
	}
	action := ActionLinkProps{}
	if view.Allows(PageWork, "view") {
		action = ActionLinkProps{Label: "Open live work", Href: statefulHref(view, PageWork), Class: "button secondary", Navigate: view.Navigate}
	}
	return ui.CreateElement(InsightsPage, InsightsPageProps{
		Metrics: []MetricProps{
			{Label: "Visible journeys", Value: fmt.Sprint(len(view.Work)), Note: "Live governed workflow data"},
			{Label: "In progress", Value: fmt.Sprint(active), Note: "Derived from current stage"},
			{Label: "Terminal", Value: fmt.Sprint(terminal), Note: "Completed, rejected, or failed"},
		},
		Attention: AttentionPanelProps{
			Title: "Operational attention", CountLabel: "Needs attention", CountValue: fmt.Sprint(attention),
			Description: "No trend, benchmark, or certification is shown because the server has not published an analytics capability for it.",
			Action:      action,
		},
	})
}
