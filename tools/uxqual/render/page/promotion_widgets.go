package page

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/render/journey"
)

// PromotionWidgetRegistry returns the in-memory [Registry] that resolves
// every widget ref tools/uxqual/pagedef's two real Promotion PageDefinitions
// (PromotionListPageDefinition, PromotionDetailPageDefinition) declare.
//
// Every constructor's content is sourced from tools/uxqual/render/journey's
// own exported fixtures (journey.SampleListPage, journey.SampleDetailPage) --
// the same reference data that package's own tests render and assert
// against -- rather than from a second, hand-invented data set. This
// package does not edit render/journey and needs no additive export from it
// to do this: every field this file reads (Page, ListView, PeopleView,
// DetailView and their nested types) is already exported by
// tools/uxqual/render/journey's contract.
//
// The fixtures carry no clock read, no random id, and no I/O (see
// journey.SampleListPage's own doc comment), so every constructor here is a
// pure function of its [WidgetContext] and the whole tree this registry
// helps build is deterministic -- see the package doc comment and this
// package's golden test.
//
// This is deliberately Gate A/B reference wiring, not the production widget
// registry: a real deployment's widget registry (WEB-005) resolves a widget
// ref to a component that reads its region's declared [pagedef.DataBinding]s
// through the authorized RPC surface, which this package does not attempt --
// see doc.go's "What this package does and does not own".
func PromotionWidgetRegistry() *Registry {
	reg := NewRegistry()
	register := func(ref string, ctor Widget) {
		if err := reg.Register(ref, ctor); err != nil {
			// Every ref below is a distinct literal; a duplicate here is a
			// programming error in this file, not a runtime condition a
			// caller can recover from.
			panic(err)
		}
	}

	register("widget.table.workforce.v1", workforceTableWidget)
	register("widget.form.create-worker.v1", createWorkerFormWidget)
	register("widget.list.journeys.v1", journeysListWidget)
	register("widget.form.propose-journey.v1", proposeJourneyFormWidget)
	register("widget.stepper.journey-stage.v1", stepperWidget)
	register("widget.table.comparison.v1", comparisonTableWidget)
	register("widget.gauge.pay-band.v1", payBandGaugeWidget)
	register("widget.gauge.budget.v1", budgetGaugeWidget)
	register("widget.factlist.v1", factListWidget)
	register("widget.table.work-items.v1", workItemsTableWidget)
	register("widget.timeline.v1", timelineWidget)

	return reg
}

// labelledItem is the small building block every list-shaped widget below
// uses: one <li> with a bold label and its value as plain text.
func labelledItem(label, value string) ui.Node {
	return html.Li(html.Props{},
		html.Strong(html.Props{}, ui.Text(label+": ")),
		ui.Text(value),
	)
}

// --- List page widgets -----------------------------------------------

func workforceTableWidget(WidgetContext) ui.Node {
	workers := journey.SampleListPage().List.People.Workers
	rows := make([]ui.Node, 0, len(workers)+1)
	rows = append(rows, html.Tr(html.Props{},
		html.Th(html.Props{}, ui.Text("Name")),
		html.Th(html.Props{}, ui.Text("Title")),
		html.Th(html.Props{}, ui.Text("Grade")),
		html.Th(html.Props{}, ui.Text("Pay")),
	))
	for _, w := range workers {
		rows = append(rows, html.Tr(html.Props{Data: map[string]string{"worker-ref": w.Ref}},
			html.Td(html.Props{}, ui.Text(w.Name)),
			html.Td(html.Props{}, ui.Text(w.Title)),
			html.Td(html.Props{}, ui.Text(w.Grade)),
			html.Td(html.Props{}, ui.Text(w.PayLine)),
		))
	}
	return scrollableTable("workforce-table", "Workforce", html.Table(html.Props{Aria: map[string]string{"label": "Workforce"}}, rows...))
}

func createWorkerFormWidget(WidgetContext) ui.Node {
	form := journey.SampleListPage().List.People.Form
	return fieldsForm("New employee", form.Fields, form.Submit)
}

func journeysListWidget(WidgetContext) ui.Node {
	journeys := journey.SampleListPage().List.Journeys
	items := make([]ui.Node, 0, len(journeys))
	for _, j := range journeys {
		items = append(items, html.Li(html.Props{Data: map[string]string{"intent-id": j.IntentID}},
			html.Strong(html.Props{}, ui.Text(j.WorkerName+" — ")),
			ui.Text(j.Headline+" ("+j.StageLabel+")"),
		))
	}
	return html.Ul(html.Props{Aria: map[string]string{"label": "Promotion journeys"}}, items...)
}

func proposeJourneyFormWidget(WidgetContext) ui.Node {
	form := journey.SampleListPage().List.Form
	return fieldsForm("Propose a promotion", form.Fields, form.Submit)
}

// fieldsForm renders one form's fields as a labelled list plus its submit
// button's own label, without wiring any submission: this registry mounts
// widgets for a deterministic rendering proof, not a live form (see this
// file's doc comment).
func fieldsForm(label string, fields []journey.Field, submit string) ui.Node {
	items := make([]ui.Node, 0, len(fields))
	for _, f := range fields {
		value := f.Value
		if value == "" {
			value = f.Placeholder
		}
		items = append(items, labelledItem(f.Label, value))
	}
	return html.Form(html.Props{Aria: map[string]string{"label": label}},
		html.Ul(html.Props{}, items...),
		html.Button(html.Props{Type: "submit"}, ui.Text(submit)),
	)
}

// --- Detail page widgets -----------------------------------------------

func stepperWidget(WidgetContext) ui.Node {
	steps := journey.SampleDetailPage().Detail.Steps
	items := make([]ui.Node, 0, len(steps))
	for _, s := range steps {
		items = append(items, html.Li(html.Props{Data: map[string]string{"step-id": s.ID, "state": s.State}},
			html.Strong(html.Props{}, ui.Text(s.Label+" ")),
			ui.Text("("+s.State+")"),
		))
	}
	return html.Ol(html.Props{Aria: map[string]string{"label": "Journey stage"}}, items...)
}

func comparisonTableWidget(WidgetContext) ui.Node {
	rows := journey.SampleDetailPage().Detail.Comparison
	trs := make([]ui.Node, 0, len(rows)+1)
	trs = append(trs, html.Tr(html.Props{},
		html.Th(html.Props{}, ui.Text("Field")),
		html.Th(html.Props{}, ui.Text("Current")),
		html.Th(html.Props{}, ui.Text("Proposed")),
		html.Th(html.Props{}, ui.Text("Delta")),
	))
	for _, r := range rows {
		trs = append(trs, html.Tr(html.Props{Data: map[string]string{"changed": strconv.FormatBool(r.Changed)}},
			html.Td(html.Props{}, ui.Text(r.Label)),
			html.Td(html.Props{}, ui.Text(r.Current)),
			html.Td(html.Props{}, ui.Text(r.Proposed)),
			html.Td(html.Props{}, ui.Text(r.Delta)),
		))
	}
	return scrollableTable("comparison-table", "Comparison", html.Table(html.Props{Aria: map[string]string{"label": "Comparison"}}, trs...))
}

func payBandGaugeWidget(WidgetContext) ui.Node {
	pb := journey.SampleDetailPage().Detail.PayBand
	if pb == nil {
		return html.Ul(html.Props{Aria: map[string]string{"label": "Pay band"}})
	}
	return html.Ul(html.Props{Aria: map[string]string{"label": "Pay band"}},
		labelledItem("Minimum", pb.Min),
		labelledItem("Midpoint", pb.Mid),
		labelledItem("Maximum", pb.Max),
		labelledItem("Current", pb.Current),
		labelledItem("Proposed", pb.Proposed),
		labelledItem("Note", pb.Note),
	)
}

func budgetGaugeWidget(WidgetContext) ui.Node {
	b := journey.SampleDetailPage().Detail.Budget
	if b == nil {
		return html.Ul(html.Props{Aria: map[string]string{"label": "Budget"}})
	}
	return html.Ul(html.Props{Aria: map[string]string{"label": "Budget"}},
		labelledItem("Available", b.Available),
		labelledItem("Committed", b.Committed),
		labelledItem("Requested", b.Requested),
		labelledItem("Note", b.Note),
	)
}

func factListWidget(WidgetContext) ui.Node {
	facts := journey.SampleDetailPage().Detail.Engine
	items := make([]ui.Node, 0, len(facts))
	for _, f := range facts {
		items = append(items, labelledItem(f.Label, f.Value))
	}
	return html.Ul(html.Props{Aria: map[string]string{"label": "Engine facts"}}, items...)
}

func workItemsTableWidget(WidgetContext) ui.Node {
	items := journey.SampleDetailPage().Detail.WorkItems
	trs := make([]ui.Node, 0, len(items)+1)
	trs = append(trs, html.Tr(html.Props{},
		html.Th(html.Props{}, ui.Text("ID")),
		html.Th(html.Props{}, ui.Text("Kind")),
		html.Th(html.Props{}, ui.Text("Status")),
		html.Th(html.Props{}, ui.Text("Owner")),
	))
	for _, w := range items {
		trs = append(trs, html.Tr(html.Props{Data: map[string]string{"work-item-id": w.ID}},
			html.Td(html.Props{}, ui.Text(w.ID)),
			html.Td(html.Props{}, ui.Text(w.Kind)),
			html.Td(html.Props{}, ui.Text(w.Status)),
			html.Td(html.Props{}, ui.Text(w.Owner)),
		))
	}
	return scrollableTable("work-items-table", "Work items", html.Table(html.Props{Aria: map[string]string{"label": "Work items"}}, trs...))
}

// scrollableTable preserves native table semantics while containing intrinsic
// two-dimensional overflow. The focusable named region gives keyboard and
// screen-reader users an operable viewport; the visible cue also covers touch
// and magnification users without relying on a scrollbar being visible.
func scrollableTable(id, label string, table ui.Node) ui.Node {
	cueID := id + "-scroll-cue"
	return html.Div(html.Props{Class: "table-container"},
		html.P(html.Props{ID: cueID, Class: "table-scroll-cue"}, ui.Text("Scroll horizontally to see all columns.")),
		html.Div(html.Props{
			ID:       id,
			Class:    "table-scroll",
			Role:     "region",
			TabIndex: html.TabIndexZero,
			Aria:     map[string]string{"label": label + " table", "describedby": cueID},
		}, table),
	)
}

func timelineWidget(WidgetContext) ui.Node {
	events := journey.SampleDetailPage().Detail.Timeline
	items := make([]ui.Node, 0, len(events))
	for _, e := range events {
		items = append(items, html.Li(html.Props{Data: map[string]string{"actor": e.Actor}},
			html.Strong(html.Props{}, ui.Text(e.At+" — ")),
			ui.Text(e.Title),
		))
	}
	return html.Ol(html.Props{Aria: map[string]string{"label": "Timeline"}}, items...)
}
