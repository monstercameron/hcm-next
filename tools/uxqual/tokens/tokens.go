// Package tokens defines the semantic design tokens shared by both
// Promotion workspace renderers (the GWC/WASM renderer and the Go SSR
// fallback), and the WCAG 2.2 AA contrast math UX-QUAL-001 checks them with.
//
// Both renderers import this package instead of hard-coding colors, so a
// contrast fix made here applies to whichever renderer ships.
package tokens

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Color is a semantic token name paired with its sRGB hex value.
type Color struct {
	Name string
	Hex  string // "#rrggbb"
}

// Palette is the full set of tokens the workspace uses. Every color a
// renderer emits must come from here (checked by
// qual.CheckNoUntokenizedColor) so the contrast fixture has a closed set of
// pairs to evaluate.
var Palette = struct {
	Text        Color
	TextMuted   Color
	Background  Color
	Surface     Color
	Border      Color
	Accent      Color
	AccentText  Color
	Success     Color
	Warning     Color
	WarningText Color
	Danger      Color
	DangerText  Color
	Info        Color
}{
	Text:        Color{"text", "#1a1d29"},
	TextMuted:   Color{"text-muted", "#4a4f63"},
	Background:  Color{"background", "#ffffff"},
	Surface:     Color{"surface", "#f4f5f8"},
	Border:      Color{"border", "#c7cad6"},
	Accent:      Color{"accent", "#1d4f91"},
	AccentText:  Color{"accent-text", "#ffffff"},
	Success:     Color{"success", "#166534"},
	Warning:     Color{"warning", "#7a4a00"},
	WarningText: Color{"warning-text", "#fff6e5"},
	Danger:      Color{"danger", "#a3123a"},
	DangerText:  Color{"danger-text", "#ffffff"},
	Info:        Color{"info", "#1a4f66"},
}

// TextPairs enumerates every (foreground, background) pair the workspace
// actually renders text or icons over. UX-QUAL-001's contrast check scores
// exactly this list; adding a new foreground/background combination to a
// renderer without adding it here is a gap the Conformance test catches
// (see qual.CheckContrastAA).
func TextPairs() []struct{ Foreground, Background Color } {
	p := Palette
	return []struct{ Foreground, Background Color }{
		{p.Text, p.Background},
		{p.Text, p.Surface},
		{p.TextMuted, p.Background},
		{p.TextMuted, p.Surface},
		{p.AccentText, p.Accent},
		{p.Success, p.Surface},
		{p.Warning, p.Surface},
		{p.WarningText, p.Warning},
		{p.Danger, p.Surface},
		{p.DangerText, p.Danger},
		{p.Info, p.Surface},
	}
}

// ContrastRatio computes the WCAG relative-luminance contrast ratio between
// two sRGB hex colors, per
// https://www.w3.org/TR/WCAG22/#dfn-relative-luminance and
// https://www.w3.org/TR/WCAG22/#dfn-contrast-ratio. The result is always in
// [1, 21].
func ContrastRatio(a, b string) (float64, error) {
	la, err := relativeLuminance(a)
	if err != nil {
		return 0, fmt.Errorf("foreground %q: %w", a, err)
	}
	lb, err := relativeLuminance(b)
	if err != nil {
		return 0, fmt.Errorf("background %q: %w", b, err)
	}
	lighter, darker := la, lb
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05), nil
}

func relativeLuminance(hex string) (float64, error) {
	r, g, b, err := parseHex(hex)
	if err != nil {
		return 0, err
	}
	lin := func(c float64) float64 {
		c /= 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b), nil
}

func parseHex(hex string) (r, g, b float64, err error) {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 {
		return 0, 0, 0, fmt.Errorf("expected #rrggbb, got %q", hex)
	}
	rv, err := strconv.ParseInt(h[0:2], 16, 32)
	if err != nil {
		return 0, 0, 0, err
	}
	gv, err := strconv.ParseInt(h[2:4], 16, 32)
	if err != nil {
		return 0, 0, 0, err
	}
	bv, err := strconv.ParseInt(h[4:6], 16, 32)
	if err != nil {
		return 0, 0, 0, err
	}
	return float64(rv), float64(gv), float64(bv), nil
}

// MinRatioNormalText is the WCAG 2.2 AA minimum contrast ratio (1.4.3) for
// text under 18pt (or under 14pt bold).
const MinRatioNormalText = 4.5

// MinRatioLargeText is the WCAG 2.2 AA minimum contrast ratio (1.4.3) for
// text at or above 18pt (or 14pt bold).
const MinRatioLargeText = 3.0

// CSSVariables renders the palette as a `:root { --token: #hex; ... }`
// declaration block both renderers can embed verbatim.
func CSSVariables() string {
	p := Palette
	all := []Color{p.Text, p.TextMuted, p.Background, p.Surface, p.Border, p.Accent, p.AccentText, p.Success, p.Warning, p.WarningText, p.Danger, p.DangerText, p.Info}
	var b strings.Builder
	b.WriteString(":root{")
	for _, c := range all {
		fmt.Fprintf(&b, "--color-%s:%s;", c.Name, c.Hex)
	}
	b.WriteString("}")
	return b.String()
}

// WorkspaceCSS is the one Promotion workspace stylesheet both renderers
// (tools/uxqual/render/ssr and tools/uxqual/render/gwc) embed verbatim, so a
// layout or contrast fix made here applies to whichever renderer ships.
//
// Every rule uses relative units (rem/%/ch) or content-based sizing;
// nothing sets a fixed pixel width wider than the 320px reflow floor
// (UX-QUAL-001's reflow criterion), and the one grid breakpoint only ADDS
// columns above 640px -- it never requires horizontal scrolling below it.
func WorkspaceCSS() string {
	return CSSVariables() + `
*{box-sizing:border-box}
body{margin:0;background:var(--color-background);color:var(--color-text);
  font:1rem/1.5 system-ui,sans-serif;max-width:100%;overflow-x:hidden}
.workspace{max-width:60rem;margin:0 auto;padding:1rem}
header.workspace-header{padding:1rem;border-bottom:1px solid var(--color-border)}
main{display:flex;flex-direction:column;gap:1.5rem;padding:1rem}
@media (min-width:640px){
  main{display:grid;grid-template-columns:1fr 1fr;gap:1.5rem}
  section.timeline,section.actions{grid-column:1 / -1}
}
section{background:var(--color-surface);border:1px solid var(--color-border);
  border-radius:.5rem;padding:1rem;min-width:0}
.field{display:flex;flex-direction:column;gap:.25rem;margin-bottom:1rem;min-width:0}
.field label{font-weight:600}
.field input,.field textarea{font:inherit;padding:.5rem;border:1px solid var(--color-border);
  border-radius:.25rem;width:100%;max-width:100%}
.field .field-static{margin:0;padding:.5rem 0}
.field .error{color:var(--color-danger);font-size:.875rem}
ul.findings,ol.timeline{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:.5rem}
.finding,.check{border-left:.25rem solid var(--color-border);padding:.25rem .75rem}
.finding[data-severity="blocking"],.check[data-severity="blocking"]{border-color:var(--color-danger)}
.finding[data-severity="warning"],.check[data-severity="warning"]{border-color:var(--color-warning)}
.finding[data-severity="success"],.check[data-severity="success"]{border-color:var(--color-success)}
.finding[data-severity="info"],.check[data-severity="info"]{border-color:var(--color-info)}
.status-banner{padding:.75rem 1rem;border-radius:.25rem;font-weight:600}
.status-banner[data-status="ready"]{background:var(--color-success);color:var(--color-accent-text)}
.status-banner[data-status="needs_review"],.status-banner[data-status="pending"]{
  background:var(--color-warning);color:var(--color-warning-text)}
.status-banner[data-status="failed"]{background:var(--color-danger);color:var(--color-danger-text)}
.actions{display:flex;flex-wrap:wrap;gap:.75rem}
button{font:inherit;padding:.6rem 1.2rem;border-radius:.25rem;border:1px solid transparent;cursor:pointer}
button[data-variant="primary"]{background:var(--color-accent);color:var(--color-accent-text)}
button[data-variant="secondary"]{background:var(--color-surface);color:var(--color-text);
  border-color:var(--color-border)}
button[data-variant="danger"]{background:var(--color-danger);color:var(--color-danger-text)}
.visually-hidden{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;
  clip:rect(0,0,0,0);white-space:nowrap;border:0}
`
}
