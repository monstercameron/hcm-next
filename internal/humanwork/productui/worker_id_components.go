package productui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var workerIDValidationFieldIDs = []string{
	"worker-prefix", "worker-suffix", "worker-digits", "worker-start",
	"worker-increment", "worker-excluded",
}

type WorkerIDPageProps struct {
	I18nProps
	Policy     WorkerIDPolicy
	Validation ValidationState
	Back       ActionLinkProps
	Editable   bool
	OnSave     func(WorkerIDPolicy)
}

func WorkerIDPage(props WorkerIDPageProps) ui.Node {
	draft := props.Policy
	errors := props.Validation.Errors()
	summaryRef := ui.UseDOMRef()
	ui.UseAutoFocus(summaryRef, props.Validation.SubmissionAttempted && len(errors) > 0)
	return html.Div(html.Props{Class: "worker-id-page"},
		html.Section(html.Props{Class: "surface worker-id-intro"},
			html.Div(html.Props{}, html.Span(html.Props{Class: "eyebrow"}, ui.Text(props.Text("worker_ids.eyebrow"))), html.H2(html.Props{}, ui.Text(props.Text("worker_ids.heading"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("worker_ids.description")))),
			ui.CreateElement(ActionLink, props.Back),
		),
		html.Form(html.Props{Class: "worker-id-layout", OnSubmit: saveWorkerIDPolicy(props.OnSave, &draft)},
			ui.CreateElement(ValidationSummary, ValidationSummaryProps{I18nProps: props.I18nProps, ID: "worker-id-validation-summary", Issues: errors, FieldIDs: workerIDValidationFieldIDs, Ref: summaryRef}),
			html.Fieldset(html.Props{Class: "worker-id-edit-boundary", Disabled: !props.Editable},
				html.Section(html.Props{Class: "surface worker-id-rules"},
					html.Div(html.Props{Class: "section-head"}, html.Div(html.Props{}, html.H2(html.Props{}, ui.Text(props.Text("worker_ids.format_title"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("worker_ids.format_help"))))),
					html.Div(html.Props{Class: "worker-id-fields"},
						workerIDTextField(props.I18nProps, "worker-prefix", props.Text("worker_ids.prefix"), props.Text("worker_ids.prefix_help"), draft.Prefix, 12, props.Validation.ForField("worker-prefix"), func(v string) { draft.Prefix = v }),
						workerIDTextField(props.I18nProps, "worker-suffix", props.Text("worker_ids.suffix"), props.Text("worker_ids.suffix_help"), draft.Suffix, 12, props.Validation.ForField("worker-suffix"), func(v string) { draft.Suffix = v }),
						workerIDSelect("worker-separator", props.Text("worker_ids.separator"), draft.Separator, []workerIDOption{{"-", "Dash ( - )"}, {"/", "Slash ( / )"}, {".", "Dot ( . )"}, {"", "No separator"}}, func(v string) { draft.Separator = v }),
						workerIDNumberField(props.I18nProps, "worker-digits", props.Text("worker_ids.digits"), props.Text("worker_ids.digits_help"), int64(draft.SequenceDigits), 1, 12, props.Validation.ForField("worker-digits"), func(v int64) { draft.SequenceDigits = int(v) }),
						workerIDNumberField(props.I18nProps, "worker-start", props.Text("worker_ids.start"), props.Text("worker_ids.start_help"), draft.StartAt, 0, 999999999999, props.Validation.ForField("worker-start"), func(v int64) { draft.StartAt = v }),
						workerIDNumberField(props.I18nProps, "worker-increment", props.Text("worker_ids.increment"), props.Text("worker_ids.increment_help"), draft.IncrementBy, 1, 1000000, props.Validation.ForField("worker-increment"), func(v int64) { draft.IncrementBy = v }),
						workerIDSelect("worker-padding", props.Text("worker_ids.padding"), strconv.FormatBool(draft.ZeroPad), []workerIDOption{{"true", "Pad with leading zeroes"}, {"false", "Natural number width"}}, func(v string) { draft.ZeroPad = v == "true" }),
						workerIDSelect("worker-year", props.Text("worker_ids.year"), draft.YearFormat, []workerIDOption{{"NONE", "Do not include year"}, {"YY", "Two-digit year"}, {"YYYY", "Four-digit year"}}, func(v string) { draft.YearFormat = v }),
						workerIDSelect("worker-unit", props.Text("worker_ids.unit"), strconv.FormatBool(draft.IncludeUnitCode), []workerIDOption{{"false", "Do not include unit"}, {"true", "Include organization-unit code"}}, func(v string) { draft.IncludeUnitCode = v == "true" }),
						workerIDSelect("worker-check", props.Text("worker_ids.check"), draft.CheckDigit, []workerIDOption{{"NONE", "No check digit"}, {"LUHN_MOD10", "Luhn mod-10 check digit"}}, func(v string) { draft.CheckDigit = v }),
						workerIDTextField(props.I18nProps, "worker-excluded", props.Text("worker_ids.excluded"), props.Text("worker_ids.excluded_help"), draft.ExcludedRanges, 160, props.Validation.ForField("worker-excluded"), func(v string) { draft.ExcludedRanges = v }),
					),
					html.Div(html.Props{Class: "worker-id-actions"}, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(props.Text("worker_ids.save"))), html.P(html.Props{ID: "worker-id-status", Class: "muted", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(props.Text("worker_ids.status")))),
				),
			),
			ui.CreateElement(WorkerIDPreview, WorkerIDPreviewProps{I18nProps: props.I18nProps, Policy: props.Policy}),
		),
	)
}

type WorkerIDPreviewProps struct {
	I18nProps
	Policy WorkerIDPolicy
}

func WorkerIDPreview(props WorkerIDPreviewProps) ui.Node {
	examples := make([]ui.Node, 0, len(props.Policy.Previews))
	for _, value := range props.Policy.Previews {
		examples = append(examples, html.Li(html.Props{}, html.Code(html.Props{}, ui.Text(value))))
	}
	return html.Aside(html.Props{Class: "surface worker-id-preview"},
		html.Span(html.Props{Class: "status success"}, ui.Text(props.Text("worker_ids.unique"))),
		html.H2(html.Props{}, ui.Text(props.Text("worker_ids.preview"))),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Text("worker_ids.preview_help"))),
		html.Ul(html.Props{Class: "worker-id-examples"}, examples...),
		html.Tag("dl", html.Props{Class: "worker-id-state"},
			html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(props.Text("worker_ids.next"))), html.Tag("dd", html.Props{}, ui.Text(strconv.FormatInt(props.Policy.NextSequence, 10)))),
			html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(props.Text("worker_ids.issued"))), html.Tag("dd", html.Props{}, ui.Text(strconv.FormatInt(props.Policy.IssuedCount, 10)))),
		),
		html.P(html.Props{Class: "callout"}, ui.Text(props.Text("worker_ids.non_reuse"))),
	)
}

type workerIDOption struct{ Value, Label string }

func workerIDTextField(i18n I18nProps, id, label, help, value string, max int, issue *ValidationIssue, update func(string)) ui.Node {
	return ValidationInput(ValidationInputProps{I18nProps: i18n, ID: id, Type: "text", Label: label, Help: help, Value: value, MaxLength: max, Issue: issue, OnInput: ui.UseEvent(func(event ui.InputEvent) { update(event.GetValue()) })})
}

func workerIDNumberField(i18n I18nProps, id, label, help string, value, min, max int64, issue *ValidationIssue, update func(int64)) ui.Node {
	return ValidationInput(ValidationInputProps{I18nProps: i18n, ID: id, Type: "number", Label: label, Help: help, Value: strconv.FormatInt(value, 10), Min: strconv.FormatInt(min, 10), Max: strconv.FormatInt(max, 10), Required: true, Issue: issue, OnInput: ui.UseEvent(func(event ui.InputEvent) {
		if parsed, err := strconv.ParseInt(event.GetValue(), 10, 64); err == nil {
			update(parsed)
		}
	})})
}

func workerIDSelect(id, label, selected string, values []workerIDOption, update func(string)) ui.Node {
	options := make([]ui.Node, 0, len(values))
	for _, value := range values {
		options = append(options, html.Option(html.Props{Value: value.Value, Selected: value.Value == selected}, ui.Text(value.Label)))
	}
	p := html.Props{ID: id}
	p.OnChange = ui.UseEvent(func(event ui.InputEvent) { update(event.GetValue()) })
	return html.Label(html.Props{For: id}, html.Span(html.Props{}, ui.Text(label)), html.Select(p, options...))
}

func saveWorkerIDPolicy(save func(WorkerIDPolicy), draft *WorkerIDPolicy) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(*draft) })
}
