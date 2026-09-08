package productui

import (
	"fmt"
	"sort"
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const actionLauncherLimit = 10

// actionLauncherStylesheet builds the launcher styles from typed css rules.
// Raw covers only what has no typed constructor (var() fallbacks, logical
// inset properties, system colors, text alignment); everything else is typed.
func actionLauncherStylesheet() string {
	return buildTypedSheet(declareActionLauncherStyles)
}

func declareActionLauncherStyles() {
	declareGlobal(".action-launcher", gwccss.Position.Relative, gwccss.MinWidth(gwccss.Px(0)))
	declareGlobal(".action-launcher-trigger",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(7)),
		gwccss.MinHeight(gwccss.Px(42)), gwccss.MaxWidth(gwccss.Px(230)),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.78)), gwccss.Raw("font-weight", "700"), gwccss.Raw("text-align", "start"),
		hoverRule(
			gwccss.Raw("border-color", "var(--hcm-hover-border,var(--accent))"),
			gwccss.Raw("background", "var(--surface-hover,var(--soft))"),
			gwccss.TextColor(gwccss.Var("accent")),
		),
	)
	declareGlobal(".action-launcher-trigger .nav-icon", gwccss.Raw("flex", "none"))
	declareGlobal(".action-launcher-dialog",
		gwccss.Position.Absolute, gwccss.ZIndex(30),
		gwccss.Raw("inset-block-start", "48px"), gwccss.Raw("inset-inline-end", "0"),
		gwccss.W(gwccss.MinLen(gwccss.Px(360), gwccss.RawLength("calc(100vw - 28px)"))),
		gwccss.MaxHeight(gwccss.MinLen(gwccss.Vh(70), gwccss.Px(560))),
		gwccss.Raw("overflow", "auto"),
		gwccss.Padding(gwccss.Px(16)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Px(0), gwccss.Px(18), gwccss.Px(48), gwccss.Zero, gwccss.Hex("10223822"))),
	)
	declareGlobal(".action-launcher-dialog-hidden", gwccss.Display.None)
	declareGlobal(".action-launcher-head",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("margin-bottom", "12px"),
	)
	declareGlobal(".action-launcher-input",
		gwccss.MinHeight(gwccss.Px(42)),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.85)),
	)
	declareGlobal(".action-launcher-result",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(10)),
		gwccss.MinHeight(gwccss.Px(48)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Raw("border", "1px solid var(--line)"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("text-decoration", "none"),
		hoverRule(
			gwccss.Raw("border-color", "var(--accent)"),
			gwccss.Raw("background", "var(--surface-hover,var(--soft))"),
		),
	)
	declareGlobal(".action-launcher-result.active",
		gwccss.Raw("border-color", "var(--accent)"),
		gwccss.Raw("background", "var(--surface-hover,var(--soft))"),
	)
	declareGlobal(".action-launcher-result small",
		gwccss.Display.Block,
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.72)),
	)
	declareGlobal(".action-launcher-dialog",
		mediaRule(gwccss.MaxW(760),
			gwccss.Raw("inset-inline-start", "0"), gwccss.Raw("inset-inline-end", "auto"),
		),
	)
	declareGlobal(".action-launcher-trigger",
		mediaRule(gwccss.MaxW(760), gwccss.MaxWidth(gwccss.Px(190))),
		mediaRule(gwccss.MaxW(430), gwccss.MaxWidth(gwccss.RawLength("100%"))),
	)
	declareGlobal(".action-launcher-result",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("transition", "none")),
	)
	declareGlobal(".action-launcher-trigger",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("border-color", "CanvasText"), gwccss.Raw("background", "Canvas"), gwccss.Raw("color", "CanvasText"),
		),
	)
	declareGlobal(".action-launcher-dialog",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("border-color", "CanvasText"), gwccss.Raw("background", "Canvas"), gwccss.Raw("color", "CanvasText"),
		),
	)
	declareGlobal(".action-launcher-result",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("border-color", "CanvasText"), gwccss.Raw("background", "Canvas"), gwccss.Raw("color", "CanvasText"),
		),
	)
	declareGlobal(".action-launcher-result small",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("color", "GrayText")),
	)
	declareGlobal(".action-launcher",
		mediaRule(gwccss.RawMedia("print"), gwccss.MarkImportant(gwccss.Display.None)),
	)
}

// ActionLauncherItem is one authorized start the shell launcher may offer.
// Items are presentation-only destinations derived from the authorized View
// projection; the launcher navigates to start pages and never executes an
// intent, decision, or approval from a row.
type ActionLauncherItem struct {
	Page        PageID
	ID          string
	Label       string
	Description string
	Href        string
	Icon        string
	Keywords    []string
}

// ActionLauncherProps keeps the shell launcher independently composable and
// easy to exercise without passing the page-wide View into the component.
type ActionLauncherProps struct {
	I18nProps
	Items        []ActionLauncherItem
	Navigate     func(string)
	InitialQuery string
}

func actionLauncherProps(view View) ActionLauncherProps {
	items := make([]ActionLauncherItem, 0, 2)
	if view.Allows(PageJourneys, "create") {
		if definition, ok := LookupPage(PageJourneys); ok {
			items = append(items, ActionLauncherItem{
				Page: PageJourneys,
				ID:   "start:journeys", Label: view.Locale.Text(definition.LabelKey), Description: view.Locale.Text(definition.SubtitleKey),
				Href: statefulHref(view, PageJourneys), Icon: definition.Icon,
				Keywords: append(append([]string{"start", "new", "create"}, definition.SearchTerms...), view.Locale.Text(definition.TitleKey)),
			})
		}
	}
	if view.Allows(PagePeople, "view") {
		if definition, ok := LookupPage(PagePeople); ok {
			items = append(items, ActionLauncherItem{
				Page: PagePeople,
				ID:   "start:people", Label: view.Locale.Text(definition.LabelKey), Description: view.Locale.Text(definition.SubtitleKey),
				Href: statefulHref(view, PagePeople), Icon: definition.Icon,
				Keywords: append(append([]string{"find", "select", "choose"}, definition.SearchTerms...), view.Locale.Text(definition.TitleKey)),
			})
		}
	}
	return ActionLauncherProps{
		I18nProps: I18nProps{Locale: view.Locale}, Items: items, Navigate: view.Navigate,
	}
}

// RankActionLauncherItems performs deterministic typo-tolerant ranking over
// the local authorized starts. It never contacts a server and never reveals
// records outside the already-resolved projection.
func RankActionLauncherItems(items []ActionLauncherItem, query string, limit int) []ActionLauncherItem {
	if limit <= 0 {
		return nil
	}
	tokens := strings.Fields(normalizeNavigationSearch(query))
	if len(tokens) == 0 {
		results := make([]ActionLauncherItem, 0, minInt(limit, len(items)))
		for _, item := range items {
			results = append(results, item)
			if len(results) == limit {
				break
			}
		}
		return results
	}
	type scored struct {
		item  ActionLauncherItem
		score int
	}
	scoredItems := make([]scored, 0, len(items))
	for _, item := range items {
		if score := actionLauncherScore(item, tokens); score > 0 {
			scoredItems = append(scoredItems, scored{item: item, score: score})
		}
	}
	sort.SliceStable(scoredItems, func(left, right int) bool {
		if scoredItems[left].score != scoredItems[right].score {
			return scoredItems[left].score > scoredItems[right].score
		}
		return strings.ToLower(scoredItems[left].item.Label) < strings.ToLower(scoredItems[right].item.Label)
	})
	results := make([]ActionLauncherItem, 0, minInt(limit, len(scoredItems)))
	for _, candidate := range scoredItems {
		results = append(results, candidate.item)
		if len(results) == limit {
			break
		}
	}
	return results
}

func actionLauncherScore(item ActionLauncherItem, tokens []string) int {
	fields := []struct {
		value  string
		weight int
	}{
		{item.Label, 48}, {item.Description, 16},
	}
	for _, keyword := range item.Keywords {
		fields = append(fields, struct {
			value  string
			weight int
		}{keyword, 30})
	}
	total := 0
	for _, token := range tokens {
		best := 0
		for _, field := range fields {
			if score := fuzzyFieldScore(field.value, token); score > 0 && score+field.weight > best {
				best = score + field.weight
			}
		}
		if best == 0 {
			return 0
		}
		total += best
	}
	return total
}

// ActionLauncher is the shell "Start an action" control: a trigger button
// opening a modal dialog that filters the authorized starts locally.
func ActionLauncher(props ActionLauncherProps) ui.Node {
	query := ui.UseState(props.InitialQuery)
	open := ui.UseState(strings.TrimSpace(props.InitialQuery) != "")
	active := ui.UseState(0)
	results := RankActionLauncherItems(props.Items, query.Get(), actionLauncherLimit)
	activeIndex := active.Get()
	if activeIndex >= len(results) && len(results) > 0 {
		activeIndex = len(results) - 1
	}

	navigate := func(item ActionLauncherItem) {
		query.Set("")
		open.Set(false)
		active.Set(0)
		if props.Navigate != nil {
			props.Navigate(item.Href)
		}
	}
	trigger := html.Button(html.Props{
		ID: "action-launcher-trigger", Class: "action-launcher-trigger", Type: "button",
		Aria: map[string]string{
			"label": props.Text("action_launcher.trigger"), "haspopup": "dialog",
			"expanded": fmt.Sprint(open.Get()), "controls": "action-launcher-dialog",
		},
		OnClick: ui.UseEvent(func(ui.MouseEvent) {
			if open.Get() {
				open.Set(false)
				return
			}
			open.Set(true)
			active.Set(0)
		}),
	}, navIcon("actions"), ui.Text(props.Text("action_launcher.trigger")))

	// The dialog stays in the document while closed so its destinations and
	// accessible name resolve without client state; hidden keeps it out of
	// the accessibility tree and layout until the trigger opens it.
	dialogHidden := !open.Get()
	inputAria := map[string]string{
		"label": props.Text("action_launcher.filter_label"), "autocomplete": "list", "controls": "action-launcher-results",
		"expanded": fmt.Sprint(strings.TrimSpace(query.Get()) != ""),
	}
	if len(results) > 0 {
		inputAria["activedescendant"] = "action-launcher-result-" + fmt.Sprint(activeIndex)
	}
	dialogProps := html.Props{
		ID: "action-launcher-dialog", Class: "action-launcher-dialog",
		Raw: map[string]any{"role": "dialog", "aria-modal": "true", "aria-label": props.Text("action_launcher.dialog_title")},
	}
	if dialogHidden {
		dialogProps.Class += " action-launcher-dialog-hidden"
		dialogProps.Raw["hidden"] = "hidden"
		dialogProps.Raw["aria-hidden"] = "true"
	}
	dialog := html.Div(dialogProps,
		html.Div(html.Props{Class: "action-launcher-head"},
			html.Strong(html.Props{}, ui.Text(props.Text("action_launcher.dialog_title"))),
			html.Tag("input", html.Props{
				ID: "action-launcher-input", Name: "action", Value: query.Get(), Class: "action-launcher-input", Aria: inputAria,
				Raw: map[string]any{"type": "search", "role": "combobox", "placeholder": props.Text("action_launcher.filter_placeholder"), "autocomplete": "off", "spellcheck": "false"},
				OnInput: ui.UseEvent(func(event ui.InputEvent) {
					query.Set(event.GetValue())
					active.Set(0)
				}),
				OnKeyDown: ui.UseEvent(func(event ui.KeyboardEvent) {
					switch event.GetKey() {
					case "Escape":
						open.Set(false)
					case "ArrowDown":
						if len(results) > 0 {
							event.PreventDefault()
							active.Set((active.Get() + 1) % len(results))
						}
					case "ArrowUp":
						if len(results) > 0 {
							event.PreventDefault()
							active.Set((active.Get() - 1 + len(results)) % len(results))
						}
					case "Enter":
						if len(results) > 0 {
							event.PreventDefault()
							navigate(results[activeIndex])
						}
					}
				}),
			}),
		),
		actionLauncherResults(props, results, activeIndex, navigate),
	)
	class := "action-launcher"
	if !dialogHidden {
		class += " action-launcher-open"
	}
	return html.Div(html.Props{ID: "action-launcher", Class: class}, trigger, dialog)
}

func actionLauncherResults(props ActionLauncherProps, results []ActionLauncherItem, active int, navigate func(ActionLauncherItem)) ui.Node {
	if len(results) == 0 {
		return unavailablePanel(props.Text("action_launcher.empty_title"), props.Text("action_launcher.empty_description"))
	}
	children := make([]ui.Node, 0, len(results))
	for index, result := range results {
		item := result
		class := "action-launcher-result"
		selected := index == active
		if selected {
			class += " active"
		}
		children = append(children, softwareLink(func(href string) { navigate(item) }, html.Props{
			ID: "action-launcher-result-" + fmt.Sprint(index), Class: class,
			Raw: map[string]any{"role": "option", "aria-selected": fmt.Sprint(selected)},
		}, item.Href,
			navIcon(item.Icon),
			html.Span(html.Props{Class: "action-launcher-copy"},
				html.Strong(html.Props{}, ui.Text(item.Label)),
				html.Small(html.Props{}, ui.Text(item.Description)),
			),
		))
	}
	return ui.CreateElement(PopoverSurface, PopoverSurfaceProps{
		ID: "action-launcher-results", Class: "action-launcher-panel",
		Raw: map[string]any{"role": "listbox", "aria-label": props.Text("action_launcher.dialog_title")}, Children: children,
	})
}
