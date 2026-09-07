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
  font:1rem/1.5 system-ui,sans-serif;max-width:100%;overflow-wrap:anywhere}
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
.finding,.check{border-inline-start:.25rem solid var(--color-border);padding:.25rem .75rem}
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
img,svg,video,canvas{max-inline-size:100%;height:auto}
:where(input,select,textarea,button){max-inline-size:100%}
pre{max-inline-size:100%;overflow:auto}
.widget-slot{min-inline-size:0;max-inline-size:100%}
.table-container,.table-scroll{min-inline-size:0;max-inline-size:100%}
.table-scroll{overflow-x:auto;overscroll-behavior-inline:contain;scrollbar-width:thin;touch-action:pan-x pan-y}
.table-scroll table{inline-size:max-content;min-inline-size:100%;table-layout:auto;border-collapse:collapse}
.table-scroll :where(th,td){min-inline-size:8rem;overflow-wrap:normal;word-break:normal;white-space:nowrap}
.table-scroll-cue{color:var(--color-text-muted);font-size:.875rem;margin-block:.25rem}
` + responsiveLayoutCSS() + modeContractsCSS()
}

// modeContractsCSS contains renderer-owned presentation contracts for user
// agents that replace the normal colour scheme, and for printed evidence.
// System colours and important declarations keep these safety rules outside
// the customer-token cascade. State words and evidence remain real DOM text;
// CSS generated content is deliberately not used because user agents expose
// it inconsistently to assistive technology and document exporters.
func modeContractsCSS() string {
	return `
@media (prefers-contrast:more){
  body,section,.workspace-header,.field :where(input,select,textarea),.status-banner,.table-scroll :where(th,td){color:CanvasText!important;background:Canvas!important}
  section,.workspace-header,.field :where(input,select,textarea),.status-banner,.table-scroll :where(th,td){border-color:CanvasText!important}
  .field .error,.table-scroll-cue{color:CanvasText!important}
  a{color:LinkText!important}
  button{color:ButtonText!important;background:ButtonFace!important;border-color:ButtonText!important}
  :where(a[href],input,select,textarea,button,summary):focus-visible{outline:3px solid Highlight!important;outline-offset:2px;box-shadow:0 0 0 1px Canvas!important}
  .finding,.check{border-inline-start:.35rem solid CanvasText!important}
  .status-banner{border-width:2px!important}
}
@media (forced-colors:active){
  body{background:Canvas!important;color:CanvasText!important}
  section,.workspace-header,.field :where(input,select,textarea),.table-scroll :where(th,td),.provenance{
    background:Canvas!important;color:CanvasText!important;border:1px solid CanvasText!important
  }
  a{color:LinkText!important}
  button{background:ButtonFace!important;color:ButtonText!important;border:1px solid ButtonText!important}
  button[data-variant="primary"],button[data-variant="danger"]{background:ButtonFace!important;color:ButtonText!important}
  .status-banner{background:Canvas!important;color:CanvasText!important;border:2px solid CanvasText!important}
  .field .error,.table-scroll-cue{color:CanvasText!important}
  .finding,.check{border-inline-start:.35rem solid CanvasText!important}
  :where(a[href],input,select,textarea,button,summary):focus-visible{outline:3px solid Highlight!important;outline-offset:2px;box-shadow:0 0 0 1px Canvas!important;forced-color-adjust:auto}
}
@media print{
  @page{margin:1.5cm}
  html,body{background:#fff!important;color:#000!important}
  body{font-size:10.5pt;line-height:1.35;max-width:none;overflow:visible}
  .workspace{max-width:none;padding:0}
  header.workspace-header,main,section,footer,.finding,.check,.status-banner,.provenance,.simulation-generated,.table-scroll :where(th,td){
    color:#000!important;background:#fff!important;border-color:#000!important
  }
  section{break-inside:avoid;page-break-inside:avoid}
  nav,.skip-link,.actions,.interactive-only,[data-print="interactive-only"]{display:none!important}
  .print-evidence{position:static!important;width:auto!important;height:auto!important;margin:.5rem 0!important;overflow:visible!important;clip:auto!important;white-space:normal!important}
  :where(input,select,textarea){color:#000!important;background:#fff!important;border-color:#000!important}
  .table-scroll{overflow:visible!important}
  .table-scroll table{inline-size:100%!important;min-inline-size:0!important}
  .table-scroll :where(th,td){min-inline-size:0!important;white-space:normal!important;overflow-wrap:anywhere!important}
}
`
}

// responsiveLayoutCSS is the renderer-owned mapping from the closed
// floorplan projection to CSS. The narrow rules are unconditional; wider
// rules only add capability through fixed min-width queries. No page,
// request, locale, or user-agent value is interpolated into this stylesheet.
func responsiveLayoutCSS() string {
	var b strings.Builder
	b.WriteString(".layout-region{min-inline-size:0;gap:.5rem}\n")
	b.WriteString(".layout-region>:where(h1,h2,h3,h4,h5,h6){grid-column:1/-1}\n")
	writeResponsiveTier(&b, "narrow", "")
	writeResponsiveTier(&b, "compact", "40rem")
	writeResponsiveTier(&b, "standard", "60rem")
	writeResponsiveTier(&b, "wide", "80rem")
	return b.String()
}

func writeResponsiveTier(b *strings.Builder, breakpoint, minWidth string) {
	if minWidth != "" {
		fmt.Fprintf(b, "@media (min-width:%s){\n", minWidth)
	}
	prefix := `.layout-region[data-layout-` + breakpoint
	fmt.Fprintf(b, "%s-mode=\"flow\"]{display:flex;flex-direction:column}\n", prefix)
	fmt.Fprintf(b, "%s-mode=\"grid\"]{display:grid}\n", prefix)
	for columns := 1; columns <= 12; columns++ {
		fmt.Fprintf(b, "%s-mode=\"grid\"][data-layout-%s-columns=\"%d\"]{grid-template-columns:repeat(%d,minmax(0,1fr))}\n", prefix, breakpoint, columns, columns)
	}
	fmt.Fprintf(b, "%s-mode=\"grid\"][data-layout-%s-stacked=\"true\"]{grid-template-columns:1fr}\n", prefix, breakpoint)
	if minWidth != "" {
		b.WriteString("}\n")
	}
}
