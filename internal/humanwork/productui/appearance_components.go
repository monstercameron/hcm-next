package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AppearancePageProps keeps the customer editor independent from storage and
// routing. The browser adapter supplies preview behavior and saves through the
// BrandPack service can supply the same callbacks without changing the page.
type AppearancePageProps struct {
	I18nProps
	Theme      CustomerTheme
	ColorModes []AppearanceOption
	Palettes   []AppearanceOption
	Shapes     []AppearanceOption
	Densities  []AppearanceOption
	Glyphs     []AppearanceOption
	Typefaces  []AppearanceOption
	Navigation []AppearanceOption
	Motions    []AppearanceOption
	Editable   bool
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
		appearanceScopeGuidance(props.I18nProps),
		html.Form(html.Props{Class: "appearance-form", OnSubmit: preventFormSubmit(props.OnSave, &draft)},
			html.Fieldset(html.Props{Class: "appearance-edit-boundary", Disabled: !props.Editable},
				html.Div(html.Props{Class: "appearance-controls"},
					appearanceChoices(props.Text("appearance.color_mode"), props.Text("appearance.color_mode_help"), "color_mode", draft.ColorMode, props.ColorModes, "color-mode-choices", func(value string) {
						draft.ColorMode = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceBrandSignature(props.I18nProps, draft, func(value string) {
						draft.BrandName = value
						previewAppearance(props.OnPreview, draft)
					}, func(value string) {
						draft.BrandMark = value
						previewAppearance(props.OnPreview, draft)
					}, func(value string) {
						draft.BrandLogoURL = value
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
		),
	)
}

func appearanceBrandSignature(i18n I18nProps, theme CustomerTheme, onName, onMark, onLogo func(string)) ui.Node {
	name := html.Props{ID: "appearance-brand-name", Type: "text", Name: "brand_name", Value: theme.BrandName, MaxLength: 40, AutoComplete: "off"}
	mark := html.Props{ID: "appearance-brand-mark", Type: "text", Name: "brand_mark", Value: theme.BrandMark, MaxLength: 3, AutoComplete: "off"}
	logoHelpID := "appearance-brand-logo-help"
	logo := html.Props{ID: "appearance-brand-logo", Type: "text", Name: "brand_logo_url", Value: theme.BrandLogoURL, MaxLength: 240, AutoComplete: "off", Aria: map[string]string{"describedby": logoHelpID}, Raw: map[string]any{"placeholder": i18n.Text("appearance.company_logo_placeholder"), "inputmode": "url"}}
	if onName != nil {
		name.OnInput = ui.UseEvent(func(event ui.InputEvent) { onName(event.GetValue()) })
	}
	if onMark != nil {
		mark.OnInput = ui.UseEvent(func(event ui.InputEvent) { onMark(event.GetValue()) })
	}
	if onLogo != nil {
		logo.OnInput = ui.UseEvent(func(event ui.InputEvent) { onLogo(event.GetValue()) })
	}
	return html.Fieldset(html.Props{Class: "surface appearance-group"},
		html.Legend(html.Props{}, ui.Text(i18n.Text("appearance.brand_signature"))),
		html.P(html.Props{Class: "muted appearance-group-help"}, ui.Text(i18n.Text("appearance.brand_help"))),
		html.Div(html.Props{Class: "appearance-brand-fields"},
			html.Label(html.Props{For: name.ID}, html.Span(html.Props{}, ui.Text(i18n.Text("appearance.workspace_name"))), html.Input(name), html.Small(html.Props{}, ui.Text(i18n.Text("appearance.workspace_name_help")))),
			html.Label(html.Props{For: mark.ID}, html.Span(html.Props{}, ui.Text(i18n.Text("appearance.short_mark"))), html.Input(mark), html.Small(html.Props{}, ui.Text(i18n.Text("appearance.short_mark_help")))),
			html.Label(html.Props{Class: "appearance-brand-logo-field", For: logo.ID}, html.Span(html.Props{}, ui.Text(i18n.Text("appearance.company_logo"))), html.Input(logo), html.Small(html.Props{ID: logoHelpID}, ui.Text(i18n.Text("appearance.company_logo_help"))), html.Small(html.Props{Class: "appearance-logo-action-help"}, ui.Text(appearanceCopy(i18n, "appearance.company_logo_action_help", "Enter an approved image path supplied by your workspace administrator. This page does not upload or choose files.")))),
		),
	)
}

func appearanceScopeGuidance(i18n I18nProps) ui.Node {
	return html.Section(html.Props{Class: "surface appearance-scope-guidance", Aria: map[string]string{"labelledby": "appearance-scope-title"}},
		html.H2(html.Props{ID: "appearance-scope-title"}, ui.Text(appearanceCopy(i18n, "appearance.scope_title", "Organization-wide appearance"))),
		html.P(html.Props{Class: "muted"}, ui.Text(appearanceCopy(i18n, "appearance.scope_detail", "Saved appearance settings apply to this organization and every signed-in user. Use system setting lets each device resolve light or dark from its own system preference; Light and Dark are organization-wide choices."))),
	)
}

func appearanceCopy(i18n I18nProps, key, fallback string) string {
	value := i18n.Text(key)
	if strings.HasPrefix(value, "⟦") && strings.HasSuffix(value, "⟧") {
		return fallback
	}
	return value
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
		choices = append(choices, html.Label(html.Props{Class: "appearance-choice appearance-choice-" + name + "-" + option.ID}, content...))
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
	reset := html.Props{Class: "button", Type: "button", Disabled: !props.Editable}
	if props.OnReset != nil {
		reset.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnReset() })
	}
	return html.Aside(html.Props{Class: "surface appearance-preview", Aria: map[string]string{"label": props.Text("appearance.preview_aria")}},
		html.Div(html.Props{}, html.H2(html.Props{}, ui.Text(props.Text("appearance.preview"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("appearance.preview_help")))),
		html.Div(html.Props{Class: "appearance-preview-window", Aria: map[string]string{"hidden": "true"}},
			html.Div(html.Props{Class: "appearance-preview-bar"},
				ui.CreateElement(BrandLogo, BrandLogoProps{Name: theme.BrandName, Mark: theme.BrandMark, LogoURL: theme.BrandLogoURL, Class: "appearance-preview-logo"}),
			),
			html.Div(html.Props{Class: "appearance-preview-body"},
				html.Div(html.Props{Class: "appearance-preview-nav"}, navIcon("home"), navIcon("people"), navIcon("journeys")),
				html.Div(html.Props{Class: "appearance-preview-content"}, html.Span(html.Props{}), html.Div(html.Props{Class: "appearance-preview-card"}, html.I(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{}))),
			),
		),
		html.P(html.Props{Class: "callout"}, ui.Text(props.Text("appearance.protected"))),
		html.Div(html.Props{Class: "appearance-actions"}, html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: !props.Editable}, ui.Text(props.Text("appearance.save"))), html.Button(reset, ui.Text(props.Text("appearance.restore")))),
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
