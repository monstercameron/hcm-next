package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type workCollectionOptions struct {
	Title      string
	ListDetail bool
}

// workPage is a route adapter: it resolves application state into immutable,
// purpose-built props and delegates all markup to the component layer.
func workPage(view View) ui.Node {
	return ui.CreateElement(WorkPage, WorkPageProps{
		I18nProps:  I18nProps{Locale: view.Locale},
		Collection: workCollectionProps(view, workCollectionOptions{Title: view.Locale.Text("work.promotion_journeys"), ListDetail: true}),
		Preview:    workPreviewProps(view, selectedWork(view)),
	})
}

func workCollectionProps(view View, options workCollectionOptions) WorkCollectionProps {
	tabs := []WorkTabProps{
		workTabProps(view, "", view.Locale.Text("work.all")),
		workTabProps(view, "review", view.Locale.Text("work.awaiting")),
		workTabProps(view, "blocked", view.Locale.Text("work.blocked")),
	}
	if options.ListDetail {
		tabs = append(tabs, WorkTabProps{Label: view.Locale.Text("work.past"), Href: statefulHref(view, PageHistory), Navigate: view.Navigate})
	}
	rows := make([]WorkRowProps, 0, len(view.Work))
	for _, item := range view.Work {
		rows = append(rows, WorkRowProps{
			Initials: item.Initials, PhotoURL: item.PhotoURL, Title: item.Title, Person: item.Person,
			Summary: item.Summary, Status: item.Status, Due: item.Due, Tone: item.Tone,
			Href:     statefulHref(view, PageWork, "filter", view.WorkFilter, "selected", item.ID),
			Selected: options.ListDetail && item.ID == view.SelectedWork, Navigate: view.Navigate,
		})
	}
	return WorkCollectionProps{
		Title: options.Title, CountLabel: view.Locale.Plural("work.item_count", int64(len(view.Work))), Tabs: tabs, Rows: rows,
		Footer: WorkCollectionFooterProps{Label: view.Locale.Text("work.authorized"), Action: ActionLinkProps{
			Label: view.Locale.Text("work.view"), Href: statefulHref(view, PageWork), Navigate: view.Navigate,
		}},
	}
}

func workTabProps(view View, filter, label string) WorkTabProps {
	return WorkTabProps{Label: label, Href: statefulHref(view, PageWork, "filter", filter), Active: view.WorkFilter == filter, Navigate: view.Navigate}
}

func workPreviewProps(view View, item WorkItem) WorkPreviewProps {
	if item.ID == "" {
		return WorkPreviewProps{
			Empty: true, EmptyTitle: view.Locale.Text("work.nothing_selected"), EmptyDetail: view.Locale.Text("work.nothing_detail"),
			Action: ActionLinkProps{Label: view.Locale.Text("work.show_all"), Href: statefulHref(view, PageWork), Class: "button secondary", Navigate: view.Navigate},
		}
	}
	return WorkPreviewProps{
		Initials: item.Initials, PhotoURL: item.PhotoURL, Title: item.Title, Person: item.Person,
		Summary: item.Summary, Status: item.Status, Tone: item.Tone, FactsTitle: view.Locale.Text("work.server_proposal"),
		Facts: []FactProps{
			{Label: view.Locale.Text("work.effective_date"), Value: valueOrUnavailableFor(view.Locale, item.EffectiveDate)},
			{Label: view.Locale.Text("work.current_base"), Value: money(view.Locale, item.CurrentBase)},
			{Label: view.Locale.Text("work.proposed_base"), Value: money(view.Locale, item.ProposedBase)},
			{Label: view.Locale.Text("work.journey_id"), Value: item.ID},
		},
		Action: ActionLinkProps{Label: view.Locale.Text("work.open_journey"), Href: item.Href, Class: "button primary full", Navigate: view.Navigate},
	}
}

func valueOrUnavailable(value string) string {
	if value == "" {
		return "Not reported"
	}
	return value
}

func valueOrUnavailableFor(locale LocaleContext, value string) string {
	if value == "" {
		return locale.Text("common.not_reported")
	}
	return value
}

func money(locale LocaleContext, value values.Money) string {
	if value.Validate() != nil {
		return locale.Text("common.not_disclosed")
	}
	return locale.FormatMoney(value.Amount().String(), value.Currency(), int(value.Amount().Scale()))
}
