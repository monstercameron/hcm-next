package workspace

import (
	"sync"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// typedSheetMu serializes typed-stylesheet builds. The css package emits into
// a process-wide, content-deduped sink, so each stylesheet is built as one
// atomic Reset -> declare -> Harvest sequence; without the mutex, another
// package's declarations could land in this sheet depending on call order.
var typedSheetMu sync.Mutex

// buildTypedSheet runs declare and returns exactly the CSS it emitted, in
// declaration order. Declarations must be deterministic: same calls, same
// bytes, so the CSP hash over the final stylesheet is stable.
func buildTypedSheet(declare func()) string {
	typedSheetMu.Lock()
	defer typedSheetMu.Unlock()
	gwccss.Reset()
	declare()
	return gwccss.Harvest()
}

// declareGlobal emits one literal selector. Variant helpers (Hover, Media,
// …) return rule slices, so every call funnels through Rules, which accepts
// both single rules and slices.
func declareGlobal(selector string, parts ...any) {
	gwccss.Global(selector, gwccss.Rules(parts...)...)
}

// mediaRule scopes parts inside one @media query. The single-spread form
// keeps every call site clear of fixed-arg-plus-spread mixing.
func mediaRule(query gwccss.MediaQuery, parts ...any) []gwccss.Rule {
	return gwccss.Media(query, gwccss.Rules(parts...)...)
}

// hoverRule scopes parts to :hover on the enclosing selector.
func hoverRule(parts ...any) []gwccss.Rule {
	return gwccss.Hover(gwccss.Rules(parts...)...)
}

// atRule wraps harvested typed CSS in an at-rule header the GWC css API has
// no constructor for (@supports, @starting-style). The inner declarations are
// fully typed; only the header keyword is literal, pending upstream support.
func atRule(header, inner string) string {
	return header + "{" + inner + "}"
}

// loginSpecificStylesheet is the login page's own CSS, kept separate from
// tokens.WorkspaceCSS (owned by tools/uxqual/tokens) so this package converts
// only its own literal. loginStylesheet concatenates the two.
func loginSpecificStylesheet() string {
	return buildTypedSheet(declareLoginStyles)
}

func declareLoginStyles() {
	declareGlobal("body",
		gwccss.Bg(gwccss.Hex("f4f7f5")),
		gwccss.TextColor(gwccss.Hex("17231d")),
	)
	declareGlobal(".login-shell",
		gwccss.Display.Block,
		gwccss.W(gwccss.RawLength("min(60rem,calc(100% - 2rem))")),
		gwccss.MarginY(gwccss.Vh(7)), gwccss.MarginX(gwccss.RawLength("auto")),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".login-card",
		gwccss.Display.Block,
		gwccss.Bg(gwccss.Hex("fff")),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("dbe5df")),
		gwccss.Rounded(gwccss.Rem(1.125)),
		gwccss.Raw("box-shadow", "0 1.125rem 3.5rem rgba(24,57,40,.10)"),
		gwccss.Padding(gwccss.RawLength("clamp(1.5rem,4vw,3rem)")),
	)
	declareGlobal(".login-brand",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("margin-bottom", "1.75rem"),
	)
	declareGlobal(".login-mark",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Rem(2.375)),
		gwccss.H(gwccss.Rem(2.375)),
		gwccss.Rounded(gwccss.Rem(.6875)),
		gwccss.Bg(gwccss.Hex("147a4a")),
		gwccss.TextColor(gwccss.Hex("fff")),
		gwccss.Raw("font-weight", "800"),
	)
	declareGlobal(".login-card h1",
		gwccss.MarginY(gwccss.Rem(.15)), gwccss.MarginX(gwccss.Zero),
		gwccss.FontSize(gwccss.RawLength("clamp(1.8rem,4vw,2.5rem)")),
	)
	declareGlobal(".login-intro",
		gwccss.MaxWidth(gwccss.Ch(62)),
		gwccss.TextColor(gwccss.Hex("53645b")),
	)
	declareGlobal(".persona-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Rem(.875)),
		gwccss.MarginY(gwccss.Rem(1.75)), gwccss.MarginX(gwccss.Zero),
	)
	declareGlobal(".persona",
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Rem(.4375)),
		gwccss.Padding(gwccss.Rem(1.125)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("dbe5df")),
		gwccss.Rounded(gwccss.Rem(.875)),
		gwccss.Bg(gwccss.Hex("fbfcfb")),
	)
	declareGlobal(".persona strong",
		gwccss.FontSize(gwccss.Rem(1.05)),
	)
	declareGlobal(".persona span",
		gwccss.Display.Block,
	)
	declareGlobal(".persona-access",
		gwccss.TextColor(gwccss.Hex("147a4a")),
		gwccss.FontSize(gwccss.Rem(.78)),
		gwccss.Raw("font-weight", "800"),
		gwccss.Tracking(gwccss.Ems(.06)),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".persona button",
		gwccss.Raw("margin-top", "auto"),
		gwccss.W(gwccss.Percent(100)),
		gwccss.Bg(gwccss.Hex("147a4a")),
		gwccss.TextColor(gwccss.Hex("fff")),
	)
	declareGlobal(".persona button:hover",
		gwccss.Bg(gwccss.Hex("0e623a")),
	)
	declareGlobal(".advanced",
		gwccss.BorderTop(gwccss.Px(1), gwccss.Hex("e7eeea")),
		gwccss.Raw("padding-top", "1rem"),
		gwccss.TextColor(gwccss.Hex("53645b")),
	)
	declareGlobal(".advanced form",
		gwccss.Raw("margin-top", ".875rem"),
	)
	declareGlobal(".login-shell",
		mediaRule(gwccss.RawMedia("(max-width:42.5rem)"), gwccss.MarginY(gwccss.Rem(1)), gwccss.MarginX(gwccss.RawLength("auto"))),
	)
	declareGlobal(".persona-grid",
		mediaRule(gwccss.RawMedia("(max-width:42.5rem)"), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".login-card",
		mediaRule(gwccss.RawMedia("(max-width:42.5rem)"), gwccss.Padding(gwccss.Rem(1.375))),
	)
}
