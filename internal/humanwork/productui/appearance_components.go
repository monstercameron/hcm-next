package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AppearancePageProps keeps the customer editor independent from storage and
// routing. The browser adapter supplies preview/save/reset behavior today; a
// BrandPack service can supply the same callbacks without changing the page.
type AppearancePageProps struct {
	I18nProps
	Theme      CustomerTheme
	Palettes   []AppearanceOption
	Shapes     []AppearanceOption
	Densities  []AppearanceOption
	Glyphs     []AppearanceOption
	Typefaces  []AppearanceOption
	Navigation []AppearanceOption
	Motions    []AppearanceOption
	OnPreview  func(CustomerTheme)
	OnSave     func(CustomerTheme)
	OnReset    func()
}

// AppearancePage is the composed administration surface for tenant branding.
func AppearancePage(props AppearancePageProps) ui.Node {
	draft := NormalizeCustomerTheme(props.Theme)
	return html.Div(html.Props{Class: "appearance-page"},
		html.Section(html.Props{Class: "surface appearance-intro"},
			html.Div(html.Props{Class: "appearance-intro-copy"},
				html.H2(html.Props{}, ui.Text(props.Text("appearance.intro_title"))),
				html.P(html.Props{Class: "muted"}, ui.Text(props.Text("appearance.intro_detail"))),
			),
			html.Span(html.Props{Class: "appearance-badge"}, navIcon("palette"), ui.Text(props.Text("appearance.tenant"))),
		),
		html.Form(html.Props{Class: "appearance-form", OnSubmit: preventFormSubmit(props.OnSave, &draft)},
			html.Div(html.Props{Class: "appearance-controls"},
				appearanceBrandSignature(props.I18nProps, draft, func(value string) {
					draft.BrandName = value
					previewAppearance(props.OnPreview, draft)
				}, func(value string) {
					draft.BrandMark = value
					previewAppearance(props.OnPreview, draft)
				}),
				appearanceChoices(props.Text("appearance.palette"), props.Text("appearance.palette_help"), "palette", draft.Palette, props.Palettes, "palette-choices", func(value string) {
					draft.Palette = value
					previewAppearance(props.OnPreview, draft)
				}),
				appearanceChoices(props.Text("appearance.shape"), props.Text("appearance.shape_help"), "shape", draft.Shape, props.Shapes, "", func(value string) {
					draft.Shape = value
					previewAppearance(props.OnPreview, draft)
				}),
				appearanceChoices(props.Text("appearance.density"), props.Text("appearance.density_help"), "density", draft.Density, props.Densities, "", func(value string) {
					draft.Density = value
					previewAppearance(props.OnPreview, draft)
				}),
				appearanceChoices(props.Text("appearance.glyphs"), props.Text("appearance.glyphs_help"), "glyphs", draft.Glyphs, props.Glyphs, "", func(value string) {
					draft.Glyphs = value
					previewAppearance(props.OnPreview, draft)
				}),
				appearanceChoices(props.Text("appearance.typeface"), props.Text("appearance.typeface_help"), "typeface", draft.Typeface, props.Typefaces, "", func(value string) {
					draft.Typeface = value
					previewAppearance(props.OnPreview, draft)
				}),
				appearanceChoices(props.Text("appearance.navigation"), props.Text("appearance.navigation_help"), "navigation", draft.Navigation, props.Navigation, "", func(value string) {
					draft.Navigation = value
					previewAppearance(props.OnPreview, draft)
				}),
				appearanceChoices(props.Text("appearance.motion"), props.Text("appearance.motion_help"), "motion", draft.Motion, props.Motions, "", func(value string) {
					draft.Motion = value
					previewAppearance(props.OnPreview, draft)
				}),
			),
			appearancePreview(props),
		),
	)
}

func appearanceBrandSignature(i18n I18nProps, theme CustomerTheme, onName, onMark func(string)) ui.Node {
	name := html.Props{ID: "appearance-brand-name", Type: "text", Name: "brand_name", Value: theme.BrandName, MaxLength: 40, AutoComplete: "off"}
	mark := html.Props{ID: "appearance-brand-mark", Type: "text", Name: "brand_mark", Value: theme.BrandMark, MaxLength: 3, AutoComplete: "off"}
	if onName != nil {
		name.OnInput = ui.UseEvent(func(event ui.InputEvent) { onName(event.GetValue()) })
	}
	if onMark != nil {
		mark.OnInput = ui.UseEvent(func(event ui.InputEvent) { onMark(event.GetValue()) })
	}
	return html.Fieldset(html.Props{Class: "surface appearance-group"},
		html.Legend(html.Props{}, ui.Text(i18n.Text("appearance.brand_signature"))),
		html.P(html.Props{Class: "muted appearance-group-help"}, ui.Text(i18n.Text("appearance.brand_help"))),
		html.Div(html.Props{Class: "appearance-brand-fields"},
			html.Label(html.Props{For: name.ID}, html.Span(html.Props{}, ui.Text(i18n.Text("appearance.workspace_name"))), html.Input(name), html.Small(html.Props{}, ui.Text(i18n.Text("appearance.workspace_name_help")))),
			html.Label(html.Props{For: mark.ID}, html.Span(html.Props{}, ui.Text(i18n.Text("appearance.short_mark"))), html.Input(mark), html.Small(html.Props{}, ui.Text(i18n.Text("appearance.short_mark_help")))),
		),
	)
}

func appearanceChoices(title, help, name, selected string, options []AppearanceOption, class string, onChange func(string)) ui.Node {
	choices := make([]ui.Node, 0, len(options))
	for _, option := range options {
		option := option
		input := html.Props{Type: "radio", Name: name, Value: option.ID, Checked: option.ID == selected, Aria: map[string]string{"label": option.Label}}
		if onChange != nil {
			input.OnChange = ui.UseEvent(func(ui.InputEvent) { onChange(option.ID) })
		}
		content := []ui.Node{html.Input(input), html.Strong(html.Props{}, ui.Text(option.Label)), html.Small(html.Props{}, ui.Text(option.Description))}
		if len(option.Swatches) > 0 {
			swatches := make([]ui.Node, 0, len(option.Swatches))
			for index := range option.Swatches {
				swatches = append(swatches, html.Span(html.Props{Class: fmt.Sprintf("appearance-swatch swatch-%s-%d", option.ID, index+1), Aria: map[string]string{"hidden": "true"}}))
			}
			content = append(content, html.Span(html.Props{Class: "appearance-swatches", Aria: map[string]string{"hidden": "true"}}, swatches...))
		}
		choices = append(choices, html.Label(html.Props{Class: "appearance-choice"}, content...))
	}
	choiceClass := "appearance-choices"
	if class != "" {
		choiceClass += " " + class
	}
	return html.Fieldset(html.Props{Class: "surface appearance-group"},
		html.Legend(html.Props{}, ui.Text(title)),
		html.P(html.Props{Class: "muted appearance-group-help"}, ui.Text(help)),
		html.Div(html.Props{Class: choiceClass}, choices...),
	)
}

func appearancePreview(props AppearancePageProps) ui.Node {
	theme := NormalizeCustomerTheme(props.Theme)
	reset := html.Props{Class: "button", Type: "button"}
	if props.OnReset != nil {
		reset.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnReset() })
	}
	return html.Aside(html.Props{Class: "surface appearance-preview", Aria: map[string]string{"label": props.Text("appearance.preview_aria")}},
		html.Div(html.Props{}, html.H2(html.Props{}, ui.Text(props.Text("appearance.preview"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("appearance.preview_help")))),
		html.Div(html.Props{Class: "appearance-preview-window", Aria: map[string]string{"hidden": "true"}},
			html.Div(html.Props{Class: "appearance-preview-bar"},
				html.Span(html.Props{ID: "appearance-preview-brand-mark", Class: "appearance-preview-mark", Data: map[string]string{"hcm-brand-mark": ""}}, ui.Text(theme.BrandMark)),
				html.Strong(html.Props{ID: "appearance-preview-brand-name", Data: map[string]string{"hcm-brand-name": ""}}, ui.Text(theme.BrandName)),
			),
			html.Div(html.Props{Class: "appearance-preview-body"},
				html.Div(html.Props{Class: "appearance-preview-nav"}, navIcon("home"), navIcon("people"), navIcon("journeys")),
				html.Div(html.Props{Class: "appearance-preview-content"}, html.Span(html.Props{}), html.Div(html.Props{Class: "appearance-preview-card"}, html.I(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{}))),
			),
		),
		html.P(html.Props{Class: "callout"}, ui.Text(props.Text("appearance.protected"))),
		html.Div(html.Props{Class: "appearance-actions"}, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(props.Text("appearance.save"))), html.Button(reset, ui.Text(props.Text("appearance.restore")))),
		html.P(html.Props{ID: "appearance-status", Class: "appearance-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(props.Text("appearance.status"))),
	)
}

func preventFormSubmit(save func(CustomerTheme), draft *CustomerTheme) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		save(NormalizeCustomerTheme(*draft))
	})
}

func previewAppearance(preview func(CustomerTheme), theme CustomerTheme) {
	if preview != nil {
		preview(NormalizeCustomerTheme(theme))
	}
}
