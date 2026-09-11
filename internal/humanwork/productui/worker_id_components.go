package productui

import (
	"reflect"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"

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

type workerIDDraft struct {
	Source  WorkerIDPolicy
	Policy  WorkerIDPolicy
	At      time.Time
	Numbers [3]string
}

func newWorkerIDDraft(p WorkerIDPolicy, at time.Time) workerIDDraft {
	return workerIDDraft{Source: p, Policy: p, At: at, Numbers: [3]string{
		strconv.Itoa(p.SequenceDigits), strconv.FormatInt(p.StartAt, 10), strconv.FormatInt(p.IncrementBy, 10),
	}}
}

// Keep incomplete input as text, never substitute the last valid number.
func workerIDNumericDraft(raw string) int64 {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return -1
	}
	return v
}

func workerIDNumbersValid(p WorkerIDPolicy) bool {
	return p.SequenceDigits >= 1 && p.SequenceDigits <= 12 && p.IncrementBy >= 1 && p.IncrementBy <= 1000000 && p.StartAt >= 0 && p.StartAt <= 999999999999
}

func workerIDDraftExamples(p WorkerIDPolicy, at time.Time) ([]string, error) {
	if !workerIDNumbersValid(p) {
		return nil, workerids.ErrInvalid
	}
	next := p.NextSequence
	if p.Version == 0 {
		next = p.StartAt
	}
	return workerids.Preview(workerids.Policy{Prefix: p.Prefix, Suffix: p.Suffix, Separator: p.Separator,
		SequenceDigits: p.SequenceDigits, StartAt: p.StartAt, NextSequence: next, IncrementBy: p.IncrementBy,
		ZeroPad: p.ZeroPad, YearFormat: p.YearFormat, IncludeUnitCode: p.IncludeUnitCode,
		CheckDigit: p.CheckDigit, ExcludedRanges: p.ExcludedRanges}, workerids.FormatContext{At: at, UnitCode: "CARE"})
}

func WorkerIDPage(props WorkerIDPageProps) ui.Node {
	state := ui.UseState(newWorkerIDDraft(props.Policy, time.Now().UTC()))
	current := state.Get()
	if !reflect.DeepEqual(current.Source, props.Policy) {
		current = newWorkerIDDraft(props.Policy, current.At)
		state.Set(current)
	}
	draft := current.Policy
	refresh := func() { current.Policy = draft; state.Set(current) }
	previewPolicy := draft
	previewPolicy.Previews, _ = workerIDDraftExamples(draft, current.At)
	statusText := props.Text("worker_ids.status")
	if !workerIDNumbersValid(draft) {
		statusText = props.Text("worker_ids.numeric_required")
	}
	errors := props.Validation.Errors()
	summaryRef := ui.UseDOMRef()
	ui.UseAutoFocus(summaryRef, props.Validation.SubmissionAttempted && len(errors) > 0)
	return html.Div(html.Props{Class: "worker-id-page"},
		html.Section(html.Props{Class: "surface worker-id-intro"},
			html.Div(html.Props{}, html.Span(html.Props{Class: "eyebrow"}, ui.Text(props.Text("worker_ids.eyebrow"))), html.H2(html.Props{}, ui.Text(props.Text("worker_ids.heading"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("worker_ids.description")))),
			ui.CreateElement(ActionLink, props.Back),
		),
		html.Form(html.Props{Class: "worker-id-layout", OnSubmit: saveWorkerIDPolicy(func(p WorkerIDPolicy) {
			if workerIDNumbersValid(p) && props.OnSave != nil {
				props.OnSave(p)
			}
		}, &draft)},
			ui.CreateElement(ValidationSummary, ValidationSummaryProps{I18nProps: props.I18nProps, ID: "worker-id-validation-summary", Issues: errors, FieldIDs: workerIDValidationFieldIDs, Ref: summaryRef}),
			html.Fieldset(html.Props{Class: "worker-id-edit-boundary", Disabled: !props.Editable},
				html.Section(html.Props{Class: "surface worker-id-rules"},
					html.Div(html.Props{Class: "section-head"}, html.Div(html.Props{}, html.H2(html.Props{}, ui.Text(props.Text("worker_ids.format_title"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("worker_ids.format_help"))))),
					html.Div(html.Props{Class: "worker-id-fields"},
						workerIDTextField(props.I18nProps, "worker-prefix", props.Text("worker_ids.prefix"), props.Text("worker_ids.prefix_help"), draft.Prefix, 12, props.Validation.ForField("worker-prefix"), func(v string) { draft.Prefix = v; refresh() }),
						workerIDTextField(props.I18nProps, "worker-suffix", props.Text("worker_ids.suffix"), props.Text("worker_ids.suffix_help"), draft.Suffix, 12, props.Validation.ForField("worker-suffix"), func(v string) { draft.Suffix = v; refresh() }),
						workerIDSelect("worker-separator", props.Text("worker_ids.separator"), draft.Separator, []workerIDOption{{"-", "Dash ( - )"}, {"/", "Slash ( / )"}, {".", "Dot ( . )"}, {"", "No separator"}}, func(v string) { draft.Separator = v; refresh() }),
						workerIDNumberField(props.I18nProps, "worker-digits", props.Text("worker_ids.digits"), props.Text("worker_ids.digits_help"), current.Numbers[0], 1, 12, props.Validation.ForField("worker-digits"), func(v string) { current.Numbers[0] = v; draft.SequenceDigits = int(workerIDNumericDraft(v)); refresh() }),
						workerIDNumberField(props.I18nProps, "worker-start", props.Text("worker_ids.start"), props.Text("worker_ids.start_help"), current.Numbers[1], 0, 999999999999, props.Validation.ForField("worker-start"), func(v string) { current.Numbers[1] = v; draft.StartAt = workerIDNumericDraft(v); refresh() }),
						workerIDNumberField(props.I18nProps, "worker-increment", props.Text("worker_ids.increment"), props.Text("worker_ids.increment_help"), current.Numbers[2], 1, 1000000, props.Validation.ForField("worker-increment"), func(v string) { current.Numbers[2] = v; draft.IncrementBy = workerIDNumericDraft(v); refresh() }),
						workerIDSelect("worker-padding", props.Text("worker_ids.padding"), strconv.FormatBool(draft.ZeroPad), []workerIDOption{{"true", "Pad with leading zeroes"}, {"false", "Natural number width"}}, func(v string) { draft.ZeroPad = v == "true"; refresh() }),
						workerIDSelect("worker-year", props.Text("worker_ids.year"), draft.YearFormat, []workerIDOption{{"NONE", "Do not include year"}, {"YY", "Two-digit year"}, {"YYYY", "Four-digit year"}}, func(v string) { draft.YearFormat = v; refresh() }),
						workerIDSelect("worker-unit", props.Text("worker_ids.unit"), strconv.FormatBool(draft.IncludeUnitCode), []workerIDOption{{"false", "Do not include unit"}, {"true", "Include organization-unit code"}}, func(v string) { draft.IncludeUnitCode = v == "true"; refresh() }),
						workerIDSelect("worker-check", props.Text("worker_ids.check"), draft.CheckDigit, []workerIDOption{{"NONE", "No check digit"}, {"LUHN_MOD10", "Luhn mod-10 check digit"}}, func(v string) { draft.CheckDigit = v; refresh() }),
						workerIDTextField(props.I18nProps, "worker-excluded", props.Text("worker_ids.excluded"), props.Text("worker_ids.excluded_help"), draft.ExcludedRanges, 160, props.Validation.ForField("worker-excluded"), func(v string) { draft.ExcludedRanges = v; refresh() }),
					),
					html.Div(html.Props{Class: "worker-id-actions"}, html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: !workerIDNumbersValid(draft), Raw: map[string]any{"aria-describedby": "worker-id-status"}}, ui.Text(props.Text("worker_ids.save"))), html.P(html.Props{ID: "worker-id-status", Class: "muted", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(statusText))),
				),
			),
			ui.CreateElement(WorkerIDPreview, WorkerIDPreviewProps{I18nProps: props.I18nProps, Policy: previewPolicy}),
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
	if len(examples) == 0 {
		examples = append(examples, html.Li(html.Props{}, ui.Text(props.Text("worker_ids.preview_unavailable"))))
	}
	return html.Aside(html.Props{Class: "surface worker-id-preview"},
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

func workerIDNumberField(i18n I18nProps, id, label, help, value string, min, max int64, issue *ValidationIssue, update func(string)) ui.Node {
	return ValidationInput(ValidationInputProps{I18nProps: i18n, ID: id, Type: "number", Label: label, Help: help, Value: value, Min: strconv.FormatInt(min, 10), Max: strconv.FormatInt(max, 10), Required: true, Issue: issue, OnInput: ui.UseEvent(func(event ui.InputEvent) {
		update(event.GetValue())
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
