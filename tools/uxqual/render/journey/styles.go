package journey

// This file holds the Promotion journey page's design tokens twice over: as
// Go values (Palette, Swatches, TextPairs, UIPairs) that styles_test.go
// scores with tokens.ContrastRatio, and as the literal CSS custom
// properties at the top of stylesheet. They are deliberately two
// representations of one truth rather than one generated from the other,
// because the server pins the sha256 of Stylesheet() in its
// content-security-policy: the stylesheet has to be a compile-time
// constant, not a string built at run time. TestStylesheetDeclaresEverySwatch
// closes the gap by asserting every Go swatch appears verbatim in the CSS.

// Swatch is one named color and its sRGB hex value. Name is the CSS custom
// property's suffix: Swatch{"accent", "#2b3a8f"} is --jn-accent:#2b3a8f.
type Swatch struct {
	Name string
	Hex  string // "#rrggbb"
}

// ColorPair is one foreground/background combination the page actually
// renders, with the reason it exists. Purpose is what a failing contrast
// assertion prints, so it names the element, not the color.
type ColorPair struct {
	Purpose    string
	Foreground Swatch
	Background Swatch
}

// Palette is every color the journey page uses. Nothing in stylesheet may
// name a color that is not here (a raw hex outside the :root block is a
// defect TestStylesheetUsesOnlyPaletteHexes catches).
var Palette = struct {
	Canvas         Swatch
	Surface        Swatch
	SurfaceSunk    Swatch
	SurfaceMuted   Swatch
	Hairline       Swatch
	ControlBorder  Swatch
	Ink            Swatch
	InkMuted       Swatch
	Masthead       Swatch
	MastheadInk    Swatch
	MastheadMuted  Swatch
	MastheadChip   Swatch
	MastheadChipIn Swatch
	Accent         Swatch
	AccentStrong   Swatch
	AccentInk      Swatch
	AccentSoft     Swatch
	Info           Swatch
	InfoSoft       Swatch
	Success        Swatch
	SuccessSoft    Swatch
	Warning        Swatch
	WarningSoft    Swatch
	Danger         Swatch
	DangerInk      Swatch
	DangerSoft     Swatch
	Neutral        Swatch
	NeutralSoft    Swatch
}{
	Canvas:         Swatch{"canvas", "#f6f7fb"},
	Surface:        Swatch{"surface", "#ffffff"},
	SurfaceSunk:    Swatch{"surface-sunk", "#eef0f7"},
	SurfaceMuted:   Swatch{"surface-muted", "#eceef5"},
	Hairline:       Swatch{"hairline", "#dfe3ee"},
	ControlBorder:  Swatch{"control-border", "#7d859c"},
	Ink:            Swatch{"ink", "#16192a"},
	InkMuted:       Swatch{"ink-muted", "#545a70"},
	Masthead:       Swatch{"masthead", "#10142b"},
	MastheadInk:    Swatch{"masthead-ink", "#ffffff"},
	MastheadMuted:  Swatch{"masthead-muted", "#b9c0d8"},
	MastheadChip:   Swatch{"masthead-chip", "#232a4a"},
	MastheadChipIn: Swatch{"masthead-chip-ink", "#ccd3e8"},
	Accent:         Swatch{"accent", "#2b3a8f"},
	AccentStrong:   Swatch{"accent-strong", "#1f2c73"},
	AccentInk:      Swatch{"accent-ink", "#ffffff"},
	AccentSoft:     Swatch{"accent-soft", "#e4ecfb"},
	Info:           Swatch{"info", "#14448f"},
	InfoSoft:       Swatch{"info-soft", "#e4ecfb"},
	Success:        Swatch{"success", "#0f6136"},
	SuccessSoft:    Swatch{"success-soft", "#dff3e6"},
	Warning:        Swatch{"warning", "#7a4a00"},
	WarningSoft:    Swatch{"warning-soft", "#fdeed3"},
	Danger:         Swatch{"danger", "#9b1130"},
	DangerInk:      Swatch{"danger-ink", "#ffffff"},
	DangerSoft:     Swatch{"danger-soft", "#fde6ea"},
	Neutral:        Swatch{"neutral", "#3f465e"},
	NeutralSoft:    Swatch{"neutral-soft", "#eceef5"},
}

// Swatches returns the palette in the order the :root block declares it.
func Swatches() []Swatch {
	p := Palette
	return []Swatch{
		p.Canvas, p.Surface, p.SurfaceSunk, p.SurfaceMuted, p.Hairline, p.ControlBorder,
		p.Ink, p.InkMuted,
		p.Masthead, p.MastheadInk, p.MastheadMuted, p.MastheadChip, p.MastheadChipIn,
		p.Accent, p.AccentStrong, p.AccentInk, p.AccentSoft,
		p.Info, p.InfoSoft, p.Success, p.SuccessSoft, p.Warning, p.WarningSoft,
		p.Danger, p.DangerInk, p.DangerSoft, p.Neutral, p.NeutralSoft,
	}
}

// TextPairs enumerates every foreground/background combination the page
// renders text over. Each must reach WCAG 2.2 AA for normal text (4.5:1);
// none of them is declared large-text-only, so the stricter threshold
// applies to all of them and a later type-scale change cannot silently
// invalidate the list.
//
// Gradients are scored at both ends: the masthead brand well runs
// masthead to masthead-chip and the primary button runs accent to
// accent-strong, so the pairs below cover the lightest stop of each, which
// is the one that could fail.
func TextPairs() []ColorPair {
	p := Palette
	return []ColorPair{
		{"body text on the page canvas", p.Ink, p.Canvas},
		{"body text on a card", p.Ink, p.Surface},
		{"body text on a sunk panel", p.Ink, p.SurfaceSunk},
		{"secondary text on the page canvas", p.InkMuted, p.Canvas},
		{"secondary text on a card", p.InkMuted, p.Surface},
		{"secondary text on a sunk panel", p.InkMuted, p.SurfaceSunk},
		{"table header text", p.InkMuted, p.SurfaceMuted},
		{"zebra row text", p.Ink, p.SurfaceSunk},
		{"masthead text", p.MastheadInk, p.Masthead},
		{"masthead text over the brand gradient's light stop", p.MastheadInk, p.MastheadChip},
		{"masthead secondary text", p.MastheadMuted, p.Masthead},
		{"masthead role chip text", p.MastheadChipIn, p.MastheadChip},
		{"primary button label", p.AccentInk, p.Accent},
		{"primary button label over the gradient's dark stop", p.AccentInk, p.AccentStrong},
		{"danger button label", p.DangerInk, p.Danger},
		{"link and accent text on a card", p.Accent, p.Surface},
		{"link and accent text on the canvas", p.Accent, p.Canvas},
		{"accent text on a tinted panel", p.Accent, p.AccentSoft},
		{"info chip text", p.Info, p.InfoSoft},
		{"info text on a card", p.Info, p.Surface},
		{"success chip text", p.Success, p.SuccessSoft},
		{"success text on a card", p.Success, p.Surface},
		{"warning chip text", p.Warning, p.WarningSoft},
		{"warning text on a card", p.Warning, p.Surface},
		{"danger chip text", p.Danger, p.DangerSoft},
		{"danger text on a card", p.Danger, p.Surface},
		{"neutral chip text", p.Neutral, p.NeutralSoft},
		{"neutral text on a card", p.Neutral, p.Surface},
		{"secondary text inside a success chip", p.InkMuted, p.SuccessSoft},
		{"secondary text inside a warning chip", p.InkMuted, p.WarningSoft},
		{"secondary text inside a danger chip", p.InkMuted, p.DangerSoft},
		{"secondary text inside an info chip", p.InkMuted, p.InfoSoft},
		{"gauge scale labels", p.InkMuted, p.SurfaceSunk},
		{"people table text on the selected row", p.Ink, p.AccentSoft},
		{"people table secondary text on the selected row", p.InkMuted, p.AccentSoft},
		{"the selected row's name", p.AccentStrong, p.AccentSoft},
	}
}

// UIPairs enumerates the non-text boundaries that carry meaning on their
// own -- form control edges, the focus ring, the stepper's state markers,
// the gauge and meter fills -- and so must reach WCAG 2.2 AA for non-text
// contrast (1.4.11, 3:1). The purely decorative hairline between cards is
// deliberately absent: the card is distinguished from the canvas by its own
// surface color, so the rule does not apply to it.
func UIPairs() []ColorPair {
	p := Palette
	return []ColorPair{
		{"input border on a card", p.ControlBorder, p.Surface},
		{"input border on the canvas", p.ControlBorder, p.Canvas},
		{"input border on a sunk panel", p.ControlBorder, p.SurfaceSunk},
		{"focus ring on the canvas", p.Accent, p.Canvas},
		{"focus ring on a card", p.Accent, p.Surface},
		{"focus ring on the masthead", p.MastheadInk, p.Masthead},
		{"completed step marker", p.Accent, p.Surface},
		{"failed step marker", p.Danger, p.Surface},
		{"timeline dot on a card", p.Neutral, p.Surface},
		{"pay band fill on its track", p.Accent, p.SurfaceMuted},
		{"pay band current marker", p.Neutral, p.SurfaceMuted},
		{"budget meter fill, healthy", p.Success, p.SurfaceMuted},
		{"budget meter fill, tight", p.Warning, p.SurfaceMuted},
		{"budget meter fill, over", p.Danger, p.SurfaceMuted},
	}
}

// Stylesheet returns the page's CSS as one literal string so the server can
// pin its sha256 in the content-security-policy's style-src. It is a
// constant for exactly that reason: a stylesheet assembled at run time
// could not be hashed at build time.
func Stylesheet() string { return stylesheet }

// stylesheet is the whole page's CSS. Constraints it is written under:
//
//   - No backtick and no closing style tag may appear in it (Go raw string
//     delimiter; HTML parsing of the inline block).
//   - System fonts only. The CSP forbids external font, image and script
//     loads, so every icon is inline SVG and every face is a system stack.
//     That also rules out the usual custom <select> chevron: a
//     background-image data: URI is an image load, which `default-src
//     'none'` blocks, so the select keeps its native arrow rather than
//     losing its affordance to an appearance:none that nothing can replace.
//   - No rule may depend on a style="" attribute. style-src-attr falls back
//     to style-src, which is a hash, so inline style attributes are blocked
//     outright. Everything data-positioned is SVG geometry instead.
//   - Sizes are rem/ch/%; nothing is wider than the 320px reflow floor, and
//     every wide artifact (tables, mono identifiers) either scrolls inside
//     its own container or wraps.
//   - Media queries only ever ADD columns above a breakpoint, so the narrow
//     layout is the base case rather than a fallback.
//   - Every animation is decoration over an already-complete static state,
//     and the whole set is switched off under prefers-reduced-motion.
const stylesheet = `:root{
--jn-canvas:#f6f7fb;
--jn-surface:#ffffff;
--jn-surface-sunk:#eef0f7;
--jn-surface-muted:#eceef5;
--jn-hairline:#dfe3ee;
--jn-control-border:#7d859c;
--jn-ink:#16192a;
--jn-ink-muted:#545a70;
--jn-masthead:#10142b;
--jn-masthead-ink:#ffffff;
--jn-masthead-muted:#b9c0d8;
--jn-masthead-chip:#232a4a;
--jn-masthead-chip-ink:#ccd3e8;
--jn-accent:#2b3a8f;
--jn-accent-strong:#1f2c73;
--jn-accent-ink:#ffffff;
--jn-accent-soft:#e4ecfb;
--jn-info:#14448f;
--jn-info-soft:#e4ecfb;
--jn-success:#0f6136;
--jn-success-soft:#dff3e6;
--jn-warning:#7a4a00;
--jn-warning-soft:#fdeed3;
--jn-danger:#9b1130;
--jn-danger-ink:#ffffff;
--jn-danger-soft:#fde6ea;
--jn-neutral:#3f465e;
--jn-neutral-soft:#eceef5;
--jn-font:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;
--jn-mono:ui-monospace,"Cascadia Mono",Consolas,monospace;
--jn-s1:0.5rem;--jn-s2:1rem;--jn-s3:1.5rem;--jn-s4:2rem;--jn-s5:3rem;
--jn-r1:0.375rem;--jn-r2:0.625rem;--jn-r3:0.75rem;--jn-r4:1rem;
--jn-shadow:0 1px 2px rgba(16,20,43,.05),0 1px 1px rgba(16,20,43,.04);
--jn-shadow-raised:0 1px 3px rgba(16,20,43,.09),0 6px 18px rgba(16,20,43,.06);
--jn-shadow-lift:0 2px 6px rgba(16,20,43,.10),0 14px 32px rgba(16,20,43,.09);
--jn-ring:0 0 0 3px rgba(43,58,143,.28);
--jn-ease:cubic-bezier(.22,.61,.36,1)
}
*,*::before,*::after{box-sizing:border-box}
html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--jn-canvas);color:var(--jn-ink);
font-family:var(--jn-font);font-size:1rem;line-height:1.55;
overflow-wrap:break-word;text-rendering:optimizeLegibility;
font-feature-settings:"cv05" 1,"ss01" 1}
h1,h2,h3,h4,p,ul,ol,dl,dd,figure,table{margin:0}
ul,ol{padding:0;list-style:none}
svg{display:block}
a{color:var(--jn-accent);text-decoration-thickness:1px;text-underline-offset:.15em;
transition:color .15s var(--jn-ease)}
a:hover{color:var(--jn-accent-strong)}
:focus-visible{outline:3px solid var(--jn-accent);outline-offset:2px;border-radius:.25rem}
.jn-masthead :focus-visible{outline-color:var(--jn-masthead-ink)}
.jn-visually-hidden{position:absolute;width:1px;height:1px;padding:0;margin:-1px;
overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
.jn-skip{position:absolute;left:var(--jn-s1);top:var(--jn-s1);z-index:20;
transform:translateY(-200%);background:var(--jn-surface);color:var(--jn-accent);
border:1px solid var(--jn-control-border);border-radius:var(--jn-r1);
padding:.5rem .875rem;font-weight:600;text-decoration:none;box-shadow:var(--jn-shadow-lift)}
.jn-skip:focus{transform:translateY(0)}
.jn-page{min-height:100vh;display:flex;flex-direction:column;min-width:0}
.jn-shell{width:100%;max-width:80rem;margin:0 auto;padding:0 var(--jn-s2)}

/* Masthead ---------------------------------------------------------- */
.jn-masthead{background:var(--jn-masthead);color:var(--jn-masthead-ink);
border-bottom:1px solid var(--jn-masthead-chip)}
.jn-masthead a{color:var(--jn-masthead-ink)}
.jn-masthead-inner{display:flex;flex-direction:column;gap:var(--jn-s2);
padding-top:.875rem;padding-bottom:.875rem}
.jn-brandbar{display:flex;align-items:center;gap:.75rem;min-width:0}
.jn-markwell{flex:none;display:flex;align-items:center;justify-content:center;
width:2.5rem;height:2.5rem;border-radius:var(--jn-r2);
background:linear-gradient(140deg,var(--jn-masthead-chip),var(--jn-accent-strong));
border:1px solid var(--jn-masthead-chip);color:var(--jn-masthead-ink);
box-shadow:inset 0 1px 0 rgba(255,255,255,.10)}
.jn-brandtext{display:flex;flex-direction:column;min-width:0;line-height:1.25}
.jn-brand-name{font-size:1rem;font-weight:660;letter-spacing:-.014em}
.jn-tenant{font-size:.75rem;color:var(--jn-masthead-muted);letter-spacing:.005em}
.jn-nav ul{display:flex;flex-wrap:wrap;gap:.25rem}
.jn-nav a{display:inline-block;padding:.375rem .75rem;border-radius:var(--jn-r1);
font-size:.875rem;font-weight:550;text-decoration:none;color:var(--jn-masthead-muted);
transition:background-color .15s var(--jn-ease),color .15s var(--jn-ease)}
.jn-nav a:hover{color:var(--jn-masthead-ink);background:var(--jn-masthead-chip)}
.jn-nav a[aria-current="page"]{color:var(--jn-masthead-ink);background:var(--jn-masthead-chip);
box-shadow:inset 0 -2px 0 var(--jn-accent-soft)}
.jn-principal{display:flex;align-items:center;gap:.5rem;flex-wrap:wrap;
font-size:.8125rem;color:var(--jn-masthead-muted);min-width:0}
.jn-principal-subject{color:var(--jn-masthead-ink);font-weight:600}
.jn-rolechip{display:inline-block;background:var(--jn-masthead-chip);
color:var(--jn-masthead-chip-ink);border-radius:999px;padding:.0625rem .5rem;
font-size:.75rem;font-family:var(--jn-mono);letter-spacing:-.01em}
.jn-logout{font-size:.8125rem;color:var(--jn-masthead-muted)}
@media (min-width:64rem){
.jn-masthead-inner{flex-direction:row;align-items:center;justify-content:space-between}
.jn-principal{justify-content:flex-end;max-width:34rem}
}

/* Notice ------------------------------------------------------------ */
.jn-noticeband{padding-top:var(--jn-s2)}
.jn-notice{display:flex;gap:.75rem;align-items:flex-start;
border:1px solid var(--jn-hairline);border-radius:var(--jn-r2);
padding:.875rem 1rem;background:var(--jn-surface);box-shadow:var(--jn-shadow);
animation:jn-slidein .32s var(--jn-ease) both}
.jn-notice-icon{flex:none;margin-top:.125rem}
.jn-notice-title{font-weight:650;letter-spacing:-.01em}
.jn-notice-detail{font-size:.875rem;color:var(--jn-ink-muted);margin-top:.125rem}
.jn-notice[data-tone="info"]{background:var(--jn-info-soft);border-color:var(--jn-info)}
.jn-notice[data-tone="info"] .jn-notice-title,.jn-notice[data-tone="info"] .jn-notice-icon{color:var(--jn-info)}
.jn-notice[data-tone="success"]{background:var(--jn-success-soft);border-color:var(--jn-success)}
.jn-notice[data-tone="success"] .jn-notice-title,.jn-notice[data-tone="success"] .jn-notice-icon{color:var(--jn-success)}
.jn-notice[data-tone="warning"]{background:var(--jn-warning-soft);border-color:var(--jn-warning)}
.jn-notice[data-tone="warning"] .jn-notice-title,.jn-notice[data-tone="warning"] .jn-notice-icon{color:var(--jn-warning)}
.jn-notice[data-tone="danger"]{background:var(--jn-danger-soft);border-color:var(--jn-danger)}
.jn-notice[data-tone="danger"] .jn-notice-title,.jn-notice[data-tone="danger"] .jn-notice-icon{color:var(--jn-danger)}

/* Type scale and layout --------------------------------------------- */
.jn-main{flex:1 1 auto;padding-top:var(--jn-s3);padding-bottom:var(--jn-s5)}
.jn-stack{display:flex;flex-direction:column;gap:var(--jn-s3);min-width:0}
.jn-pagehead{max-width:46rem}
h1{font-size:1.75rem;line-height:1.18;letter-spacing:-.024em;font-weight:680}
.jn-display{font-size:2.125rem;line-height:1.1;letter-spacing:-.03em;font-weight:700}
.jn-lead{margin-top:.5rem;color:var(--jn-ink-muted);font-size:1rem}
h2{font-size:1.25rem;line-height:1.3;letter-spacing:-.018em;font-weight:660;
display:flex;align-items:center;gap:.5rem}
h3{font-size:1rem;line-height:1.35;letter-spacing:-.012em;font-weight:640}
.jn-headicon{color:var(--jn-ink-muted)}
.jn-eyebrow{font-size:.75rem;font-weight:660;letter-spacing:.07em;
text-transform:uppercase;color:var(--jn-ink-muted)}
.jn-sectionhead{display:flex;align-items:baseline;justify-content:space-between;
gap:var(--jn-s1);flex-wrap:wrap;margin-bottom:var(--jn-s2)}
.jn-count{font-size:.8125rem;color:var(--jn-ink-muted);font-variant-numeric:tabular-nums}
.jn-card{background:var(--jn-surface);border:1px solid var(--jn-hairline);
border-radius:var(--jn-r3);box-shadow:var(--jn-shadow);padding:var(--jn-s2);min-width:0}
.jn-panel{background:var(--jn-surface);border:1px solid var(--jn-hairline);
border-radius:var(--jn-r4);box-shadow:var(--jn-shadow);padding:var(--jn-s3);min-width:0}
.jn-mono{font-family:var(--jn-mono);font-size:.8125rem;letter-spacing:-.01em;
overflow-wrap:anywhere}
.jn-num{font-variant-numeric:tabular-nums}
.jn-muted{color:var(--jn-ink-muted)}

/* Focused proposal ------------------------------------------------- */
.jn-proposal-view{max-width:64rem}
.jn-proposal-head{max-width:52rem}
.jn-context-actions{display:flex;flex-wrap:wrap;gap:.5rem 1.25rem;margin-top:var(--jn-s2)}
.jn-context-link{font-size:.875rem;font-weight:620}
.jn-subject-card{position:relative;overflow:hidden}
.jn-subject-card::before{content:"";position:absolute;inset:0 auto 0 0;width:4px;background:var(--jn-accent)}
.jn-subject-identity{display:flex;align-items:center;gap:var(--jn-s2);flex-wrap:wrap}
.jn-subject-identity>div:nth-child(2){min-width:12rem;flex:1}
.jn-subject-avatar{width:3rem;height:3rem;border-radius:50%;display:flex;align-items:center;
justify-content:center;flex:none;background:var(--jn-accent-soft);color:var(--jn-accent-strong);
font-weight:720;letter-spacing:.02em}
.jn-subject-title{color:var(--jn-ink-muted);font-size:.875rem;margin-top:.125rem}
.jn-subject-facts{margin-top:var(--jn-s2);padding-top:var(--jn-s2);border-top:1px solid var(--jn-hairline)}

/* Chips ------------------------------------------------------------- */
.jn-chip{display:inline-flex;align-items:center;gap:.3125rem;border-radius:999px;
padding:.1875rem .5625rem;font-size:.75rem;font-weight:620;letter-spacing:-.005em;
background:var(--jn-neutral-soft);color:var(--jn-neutral);white-space:nowrap;
box-shadow:inset 0 0 0 1px rgba(22,25,42,.05)}
.jn-chip[data-tone="info"]{background:var(--jn-info-soft);color:var(--jn-info)}
.jn-chip[data-tone="success"]{background:var(--jn-success-soft);color:var(--jn-success)}
.jn-chip[data-tone="warning"]{background:var(--jn-warning-soft);color:var(--jn-warning)}
.jn-chip[data-tone="danger"]{background:var(--jn-danger-soft);color:var(--jn-danger)}
.jn-chip-dot{width:.4375rem;height:.4375rem;border-radius:50%;background:currentColor;flex:none}
.jn-chip-icon{flex:none}

/* Journey cards ----------------------------------------------------- */
.jn-grid{display:grid;gap:var(--jn-s2);grid-template-columns:repeat(auto-fill,minmax(min(19rem,100%),1fr))}
.jn-journey{position:relative;display:flex;flex-direction:column;gap:.625rem;
transition:box-shadow .2s var(--jn-ease),transform .2s var(--jn-ease),border-color .2s var(--jn-ease)}
.jn-journey::before{content:"";position:absolute;left:0;right:0;top:0;height:3px;
border-radius:var(--jn-r3) var(--jn-r3) 0 0;background:var(--jn-hairline)}
.jn-journey[data-stage="AWAITING_APPROVAL"]::before{background:var(--jn-warning)}
.jn-journey[data-stage="COMPLETED"]::before{background:var(--jn-success)}
.jn-journey[data-stage="BLOCKED"]::before,.jn-journey[data-stage="REJECTED"]::before,
.jn-journey[data-stage="FAILED"]::before{background:var(--jn-danger)}
.jn-journey[data-stage="PROPOSED"]::before{background:var(--jn-info)}
.jn-journey:hover{box-shadow:var(--jn-shadow-lift);border-color:var(--jn-control-border);
transform:translateY(-2px)}
.jn-journey:focus-within{box-shadow:var(--jn-shadow-lift)}
.jn-journey-top{display:flex;align-items:flex-start;justify-content:space-between;gap:.75rem}
.jn-journey h3{margin:0}
.jn-journey h3 a{text-decoration:none;color:var(--jn-ink)}
.jn-journey h3 a::after{content:"";position:absolute;inset:0;border-radius:inherit}
.jn-journey h3 a:hover{color:var(--jn-accent)}
.jn-journey-headline{font-size:.875rem;color:var(--jn-ink-muted)}
.jn-journey-pay{font-size:1.0625rem;font-weight:640;font-variant-numeric:tabular-nums;
letter-spacing:-.016em}
.jn-meta{display:flex;flex-wrap:wrap;gap:.25rem 1rem;font-size:.8125rem;color:var(--jn-ink-muted)}
.jn-meta-key{font-weight:600;color:var(--jn-ink-muted)}
.jn-journey-foot{display:flex;align-items:center;gap:.375rem;margin-top:auto;
padding-top:.625rem;border-top:1px solid var(--jn-hairline);
font-size:.8125rem;font-weight:620;color:var(--jn-accent)}
.jn-journey-arrow{transition:transform .2s var(--jn-ease)}
.jn-journey:hover .jn-journey-arrow{transform:translateX(3px)}

/* People table ------------------------------------------------------ */
.jn-people-note{display:flex;align-items:flex-start;gap:.375rem;
margin-top:calc(var(--jn-s2) * -0.5);margin-bottom:var(--jn-s2);
font-size:.8125rem;color:var(--jn-ink-muted)}
.jn-people-note-icon{flex:none;color:var(--jn-info);margin-top:.0625rem}
.jn-peoplewrap{max-height:34rem;overflow-y:auto;overscroll-behavior:contain}
table.jn-people{font-size:.8125rem}
.jn-people thead th{z-index:2}
.jn-people tbody th,.jn-people tbody td{height:2.75rem;vertical-align:middle;
padding-top:.375rem;padding-bottom:.375rem;white-space:nowrap}
.jn-people tbody th{font-weight:inherit}
.jn-people-idcell{position:relative}
.jn-people-idbox{display:flex;flex-direction:column;align-items:flex-start;
gap:.0625rem;min-width:0;text-align:left;line-height:1.3}
.jn-people-pick{font:inherit;color:inherit;background:none;border:0;padding:0;
cursor:pointer;border-radius:var(--jn-r1);width:100%}
.jn-people-pick:hover .jn-people-name{color:var(--jn-accent)}
.jn-people-name{font-size:.9375rem;font-weight:660;letter-spacing:-.014em;color:var(--jn-ink)}
.jn-people-title{font-size:.75rem;color:var(--jn-ink-muted)}
.jn-people-number{color:var(--jn-ink-muted);font-size:.6875rem}
.jn-people-selected{display:inline-flex;align-items:center;gap:.25rem;margin-top:.125rem;
font-size:.6875rem;font-weight:680;letter-spacing:.04em;text-transform:uppercase;
color:var(--jn-accent)}
.jn-people-selected-icon{flex:none}
.jn-people-job{font-variant-numeric:tabular-nums;font-weight:620}
.jn-people-pay{font-weight:620}
.jn-people-count{font-weight:660}
.jn-people-journeys{color:var(--jn-ink-muted)}
.jn-people-action{text-align:right}
.jn-source{font-weight:640}
.jn-people tbody tr[data-tone="info"] .jn-people-idcell{box-shadow:inset 3px 0 0 var(--jn-info)}
.jn-people tbody tr[data-tone="success"] .jn-people-idcell{box-shadow:inset 3px 0 0 var(--jn-success)}
.jn-people tbody tr[data-tone="warning"] .jn-people-idcell{box-shadow:inset 3px 0 0 var(--jn-warning)}
.jn-people tbody tr[data-tone="danger"] .jn-people-idcell{box-shadow:inset 3px 0 0 var(--jn-danger)}
.jn-people tbody tr[data-selected="true"]{background:var(--jn-accent-soft)}
.jn-people tbody tr[data-selected="true"] .jn-people-idcell{box-shadow:inset 3px 0 0 var(--jn-accent)}
.jn-people tbody tr[data-selected="true"] .jn-people-name{color:var(--jn-accent-strong)}

.jn-empty{text-align:center;padding:var(--jn-s4) var(--jn-s2);color:var(--jn-ink-muted)}
.jn-empty.jn-empty-inset{padding:var(--jn-s3) var(--jn-s2);
border:1px dashed var(--jn-control-border);border-radius:var(--jn-r2);
background:var(--jn-surface-sunk)}
.jn-empty-mark{color:var(--jn-control-border);margin:0 auto var(--jn-s1)}
.jn-empty-title{color:var(--jn-ink);font-weight:640;font-size:1rem}
.jn-callout{display:flex;gap:.75rem;align-items:flex-start;
border:1px solid var(--jn-info);background:var(--jn-info-soft);
border-radius:var(--jn-r2);padding:.875rem 1rem}
.jn-callout-icon{flex:none;color:var(--jn-info);margin-top:.125rem}
.jn-callout-title{font-weight:640;color:var(--jn-info)}
.jn-callout-detail{font-size:.875rem;margin-top:.125rem}

/* Forms ------------------------------------------------------------- */
.jn-fieldgrid{display:grid;gap:var(--jn-s2);grid-template-columns:1fr}
@media (min-width:44rem){.jn-fieldgrid{grid-template-columns:1fr 1fr}
.jn-field[data-span="full"]{grid-column:1 / -1}}
.jn-field{display:flex;flex-direction:column;gap:.3125rem;min-width:0}
.jn-label{font-size:.8125rem;font-weight:640;letter-spacing:-.005em}
.jn-req{color:var(--jn-danger);margin-left:.125rem}
.jn-inputwrap{display:flex;align-items:stretch;min-width:0;
border:1px solid var(--jn-control-border);border-radius:var(--jn-r1);
background:var(--jn-surface);transition:box-shadow .15s var(--jn-ease),border-color .15s var(--jn-ease)}
.jn-inputwrap:hover{border-color:var(--jn-ink-muted)}
.jn-inputwrap:focus-within{border-color:var(--jn-accent);box-shadow:var(--jn-ring)}
.jn-field[data-invalid="true"] .jn-inputwrap{border-color:var(--jn-danger);border-width:2px}
.jn-adorn{display:flex;align-items:center;padding:0 .625rem;font-size:.8125rem;
font-weight:620;color:var(--jn-ink-muted);background:var(--jn-surface-sunk);
white-space:nowrap}
.jn-adorn[data-side="prefix"]{border-right:1px solid var(--jn-hairline);
border-radius:var(--jn-r1) 0 0 var(--jn-r1)}
.jn-adorn[data-side="suffix"]{border-left:1px solid var(--jn-hairline);
border-radius:0 var(--jn-r1) var(--jn-r1) 0}
.jn-input{font:inherit;font-size:.9375rem;color:var(--jn-ink);width:100%;min-width:0;
padding:.5rem .625rem;border:0;border-radius:var(--jn-r1);background:transparent}
.jn-input:focus{outline:none}
textarea.jn-input{resize:vertical;min-height:5rem;line-height:1.5}
select.jn-input{cursor:pointer}
.jn-help{font-size:.75rem;color:var(--jn-ink-muted)}
.jn-error{font-size:.75rem;font-weight:620;color:var(--jn-danger);
display:flex;align-items:center;gap:.3125rem}
.jn-formfoot{display:flex;align-items:center;gap:var(--jn-s2);flex-wrap:wrap;
margin-top:var(--jn-s3);padding-top:var(--jn-s2);border-top:1px solid var(--jn-hairline)}

.jn-btn{font:inherit;font-size:.875rem;font-weight:640;letter-spacing:-.005em;
display:inline-flex;align-items:center;justify-content:center;gap:.375rem;
padding:.5625rem 1.125rem;border-radius:var(--jn-r1);border:1px solid transparent;
cursor:pointer;color:var(--jn-accent-ink);text-decoration:none;white-space:nowrap;
background:linear-gradient(180deg,var(--jn-accent),var(--jn-accent-strong));
box-shadow:var(--jn-shadow),inset 0 1px 0 rgba(255,255,255,.14);
transition:transform .12s var(--jn-ease),box-shadow .15s var(--jn-ease),filter .15s var(--jn-ease)}
.jn-btn:hover{filter:brightness(1.08);box-shadow:var(--jn-shadow-raised)}
.jn-btn:active{transform:translateY(1px)}
.jn-btn[data-variant="secondary"]{background:var(--jn-surface);color:var(--jn-accent);
border-color:var(--jn-control-border);box-shadow:var(--jn-shadow)}
.jn-btn[data-variant="secondary"]:hover{background:var(--jn-surface-sunk);filter:none}
.jn-btn[data-variant="danger"]{color:var(--jn-danger-ink);
background:linear-gradient(180deg,var(--jn-danger),var(--jn-danger))}
a.jn-btn:hover{color:var(--jn-accent-ink)}
.jn-btn[data-size="sm"]{padding:.3125rem .6875rem;font-size:.75rem;letter-spacing:0}
.jn-btn[disabled]{cursor:not-allowed;background:var(--jn-surface-muted);
color:var(--jn-ink-muted);border-color:var(--jn-hairline);box-shadow:none;filter:none;
transform:none}

/* Hero -------------------------------------------------------------- */
.jn-hero{display:flex;flex-direction:column;gap:.75rem;position:relative;overflow:hidden}
.jn-hero-top{display:flex;align-items:flex-start;justify-content:space-between;
gap:var(--jn-s2);flex-wrap:wrap}
.jn-hero-pay{font-size:1.5rem;font-weight:660;letter-spacing:-.024em;
font-variant-numeric:tabular-nums}
.jn-hero-ids{padding-top:.75rem;border-top:1px solid var(--jn-hairline);color:var(--jn-ink-muted)}

/* Stepper ----------------------------------------------------------- */
.jn-stepper{display:flex;flex-direction:column;gap:0}
.jn-step{position:relative;display:flex;gap:.75rem;padding-bottom:var(--jn-s2)}
.jn-step:last-child{padding-bottom:0}
.jn-step::before{content:"";position:absolute;left:.8125rem;top:1.875rem;bottom:.25rem;
width:2px;background:var(--jn-hairline)}
.jn-step::after{content:"";position:absolute;left:.8125rem;top:1.875rem;
width:2px;height:0;background:var(--jn-accent);transform-origin:top}
.jn-step:last-child::before,.jn-step:last-child::after{display:none}
.jn-step[data-state="done"]::after{height:calc(100% - 2rem);
animation:jn-grow-y .5s var(--jn-ease) both}
.jn-step[data-state="done"]:nth-child(2)::after{animation-delay:.12s}
.jn-step[data-state="done"]:nth-child(3)::after{animation-delay:.24s}
.jn-step[data-state="done"]:nth-child(4)::after{animation-delay:.36s}
.jn-step[data-state="done"]:nth-child(5)::after{animation-delay:.48s}
.jn-stepmark{flex:none;width:1.75rem;height:1.75rem;border-radius:50%;
display:flex;align-items:center;justify-content:center;
border:2px solid var(--jn-control-border);background:var(--jn-surface);
color:var(--jn-ink-muted);font-size:.75rem;font-weight:680;
font-variant-numeric:tabular-nums;position:relative;z-index:1}
.jn-step[data-state="done"] .jn-stepmark{background:var(--jn-accent);
border-color:var(--jn-accent);color:var(--jn-accent-ink)}
.jn-step[data-state="done"] .jn-stepcheck{animation:jn-pop .35s var(--jn-ease) both}
.jn-step[data-state="active"] .jn-stepmark{border-color:var(--jn-accent);
border-width:3px;color:var(--jn-accent);background:var(--jn-accent-soft);
animation:jn-halo 2.4s var(--jn-ease) infinite}
.jn-step[data-state="failed"] .jn-stepmark{background:var(--jn-danger);
border-color:var(--jn-danger);color:var(--jn-danger-ink)}
.jn-stepbody{min-width:0;padding-top:.125rem}
.jn-steplabel{font-size:.875rem;font-weight:640}
.jn-step[data-state="active"] .jn-steplabel{color:var(--jn-accent)}
.jn-step[data-state="upcoming"] .jn-steplabel{color:var(--jn-ink-muted)}
.jn-step[data-state="failed"] .jn-steplabel{color:var(--jn-danger)}
.jn-stepdetail{font-size:.8125rem;color:var(--jn-ink-muted)}
.jn-stepat{font-size:.75rem;color:var(--jn-ink-muted);font-variant-numeric:tabular-nums}
@media (min-width:60rem){
.jn-stepper{flex-direction:row}
.jn-step{flex:1 1 0;flex-direction:column;gap:.5rem;padding-bottom:0;padding-right:var(--jn-s2)}
.jn-step:last-child{padding-right:0}
.jn-step::before{left:2.25rem;right:.5rem;top:.8125rem;bottom:auto;width:auto;height:2px}
.jn-step::after{left:2.25rem;top:.8125rem;height:2px;width:0}
.jn-step[data-state="done"]::after{width:calc(100% - 2.75rem);height:2px;
animation:jn-grow-x .5s var(--jn-ease) both}
.jn-stepbody{padding-top:0}
}

/* Two-column detail layout ------------------------------------------ */
.jn-columns{display:grid;gap:var(--jn-s3);grid-template-columns:minmax(0,1fr);align-items:start}
@media (min-width:64rem){
.jn-columns{grid-template-columns:minmax(0,1fr) 23rem}
.jn-rail{position:static}
}
.jn-col{display:flex;flex-direction:column;gap:var(--jn-s3);min-width:0}
.jn-context-nav{display:flex;flex-wrap:wrap;gap:.5rem 1.25rem;align-items:center}
.jn-context-nav a{font-size:.875rem;font-weight:620;color:var(--jn-accent)}
.jn-context-nav a:first-child::before{content:"\2190\00a0"}
.jn-subsection{margin-top:var(--jn-s3)}
.jn-subsection:first-of-type{margin-top:var(--jn-s2)}
.jn-subhead{margin-bottom:.75rem;color:var(--jn-ink-muted);font-size:.8125rem;
font-weight:660;letter-spacing:.05em;text-transform:uppercase}

/* Facts ------------------------------------------------------------- */
.jn-facts{display:grid;gap:.75rem var(--jn-s2);grid-template-columns:1fr}
@media (min-width:34rem){.jn-facts{grid-template-columns:1fr 1fr}}
.jn-fact{min-width:0}
.jn-fact dt{font-size:.75rem;font-weight:620;letter-spacing:.02em;color:var(--jn-ink-muted)}
.jn-fact dd{margin:0;font-size:.9375rem}
.jn-fact[data-tone="success"] dd{color:var(--jn-success);font-weight:620}
.jn-fact[data-tone="warning"] dd{color:var(--jn-warning);font-weight:620}
.jn-fact[data-tone="danger"] dd{color:var(--jn-danger);font-weight:620}
.jn-fact[data-tone="info"] dd{color:var(--jn-info);font-weight:620}

/* Tables ------------------------------------------------------------ */
.jn-tablewrap{overflow-x:auto;border:1px solid var(--jn-hairline);border-radius:var(--jn-r2)}
table.jn-table{width:100%;border-collapse:collapse;font-size:.875rem}
.jn-table th,.jn-table td{padding:.5625rem .75rem;text-align:left;vertical-align:top;
border-bottom:1px solid var(--jn-hairline)}
.jn-table thead th{background:var(--jn-surface-muted);color:var(--jn-ink-muted);
font-size:.6875rem;font-weight:660;letter-spacing:.06em;text-transform:uppercase;
white-space:nowrap;position:sticky;top:0}
.jn-table tbody tr:last-child th,.jn-table tbody tr:last-child td{border-bottom:0}
.jn-table td.jn-num,.jn-table th.jn-num{font-variant-numeric:tabular-nums}
.jn-table tbody th{font-weight:600}
.jn-table th.jn-mono{white-space:nowrap;overflow-wrap:normal}
.jn-zebra tbody tr:nth-child(even){background:var(--jn-surface-sunk)}
.jn-table tbody tr{transition:background-color .15s var(--jn-ease)}
.jn-table tbody tr:hover{background:var(--jn-accent-soft)}
.jn-table tr[data-changed="true"]{background:var(--jn-accent-soft)}
.jn-table tr[data-changed="true"] td.jn-proposed{font-weight:660;color:var(--jn-accent)}
.jn-delta{font-weight:640;font-variant-numeric:tabular-nums}
.jn-table td.jn-change{white-space:nowrap}

/* Preflight board --------------------------------------------------- */
.jn-board{display:flex;flex-direction:column;gap:.5rem}
.jn-check{display:flex;gap:.75rem;align-items:flex-start;
border:1px solid var(--jn-hairline);border-radius:var(--jn-r2);
padding:.625rem .75rem;background:var(--jn-surface);
animation:jn-slidein .4s var(--jn-ease) both}
.jn-check[data-row="1"]{animation-delay:.06s}
.jn-check[data-row="2"]{animation-delay:.12s}
.jn-check[data-row="3"]{animation-delay:.18s}
.jn-check[data-row="4"]{animation-delay:.24s}
.jn-check[data-row="5"]{animation-delay:.30s}
.jn-check[data-row="6"]{animation-delay:.36s}
.jn-check[data-row="7"]{animation-delay:.42s}
.jn-check[data-severity="blocking"]{border-left:3px solid var(--jn-danger)}
.jn-check[data-severity="warning"]{border-left:3px solid var(--jn-warning)}
.jn-check[data-severity="success"]{border-left:3px solid var(--jn-success)}
.jn-check[data-severity="info"]{border-left:3px solid var(--jn-info)}
.jn-checkpill{flex:none;display:inline-flex;align-items:center;gap:.3125rem;
min-width:6.5rem;border-radius:999px;padding:.25rem .625rem;
font-size:.6875rem;font-weight:680;letter-spacing:.03em;text-transform:uppercase;
background:var(--jn-neutral-soft);color:var(--jn-neutral);
box-shadow:inset 0 0 0 1px rgba(22,25,42,.05);position:relative;overflow:hidden}
.jn-checkpill[data-tone="info"]{background:var(--jn-info-soft);color:var(--jn-info)}
.jn-checkpill[data-tone="success"]{background:var(--jn-success-soft);color:var(--jn-success)}
.jn-checkpill[data-tone="warning"]{background:var(--jn-warning-soft);color:var(--jn-warning)}
.jn-checkpill[data-tone="danger"]{background:var(--jn-danger-soft);color:var(--jn-danger)}
.jn-checkpill::after{content:"";position:absolute;inset:0;border-radius:inherit;
background:linear-gradient(100deg,transparent 20%,rgba(255,255,255,.55) 50%,transparent 80%);
transform:translateX(-100%)}
.jn-checkpill[data-tone="success"]::after{animation:jn-sweep .9s var(--jn-ease) .35s both}
.jn-checkpill[data-tone="warning"]{animation:jn-amber 2.8s ease-in-out .4s infinite}
.jn-checkpill[data-tone="danger"]{animation:jn-alert 2.2s ease-in-out .3s infinite}
.jn-checkpill[data-tone="info"]{animation:jn-breathe 3.2s ease-in-out .5s infinite}
.jn-checkpill-icon{flex:none}
.jn-check-body{min-width:0}
.jn-check-msg{font-size:.875rem}
.jn-check-code{margin-top:.125rem;color:var(--jn-ink-muted)}

/* Gauges ------------------------------------------------------------ */
.jn-gauges{display:grid;gap:var(--jn-s2);grid-template-columns:1fr;margin-top:var(--jn-s3)}
@media (min-width:52rem){.jn-gauges{grid-template-columns:1fr 1fr}}
.jn-gauge{border:1px solid var(--jn-hairline);border-radius:var(--jn-r2);
padding:.875rem 1rem;background:var(--jn-surface-sunk);min-width:0}
.jn-gauge .jn-subhead{margin-bottom:.625rem}
.jn-band,.jn-meter{width:100%;height:auto;overflow:visible}
.jn-band-track{fill:var(--jn-surface-muted);stroke:var(--jn-control-border);stroke-width:1}
.jn-band-fill{fill:var(--jn-accent);opacity:.85;
transform-box:fill-box;transform-origin:left center;
animation:jn-grow-x-svg .6s var(--jn-ease) .15s both}
.jn-band-tick{stroke:var(--jn-control-border);stroke-width:1;stroke-dasharray:2 2}
.jn-band-marker{stroke-width:3;stroke-linecap:round;
animation:jn-drop .45s var(--jn-ease) .5s both}
.jn-band-marker[data-which="current"]{stroke:var(--jn-neutral)}
.jn-band-marker[data-which="proposed"]{stroke:var(--jn-accent-strong);animation-delay:.65s}
.jn-meter-track{fill:var(--jn-surface-muted);stroke:var(--jn-control-border);stroke-width:1}
.jn-meter-fill{transform-box:fill-box;transform-origin:left center;
animation:jn-grow-x-svg .7s var(--jn-ease) .2s both}
.jn-meter[data-tone="success"] .jn-meter-fill{fill:var(--jn-success)}
.jn-meter[data-tone="warning"] .jn-meter-fill{fill:var(--jn-warning)}
.jn-meter[data-tone="danger"] .jn-meter-fill{fill:var(--jn-danger);
animation:jn-grow-x-svg .7s var(--jn-ease) .2s both,jn-alert 2.2s ease-in-out 1s infinite}
.jn-gauge-scale{display:flex;justify-content:space-between;gap:.5rem;
font-size:.6875rem;color:var(--jn-ink-muted);font-variant-numeric:tabular-nums;
margin-top:.125rem}
.jn-gauge-legend{display:grid;grid-template-columns:auto 1fr;gap:.125rem .625rem;
margin-top:.625rem;font-size:.8125rem;align-items:baseline}
.jn-gauge-legend dt{color:var(--jn-ink-muted);display:flex;align-items:center;gap:.375rem}
.jn-gauge-legend dt[data-which]::before{content:"";width:.5rem;height:.5rem;
border-radius:2px;background:var(--jn-neutral)}
.jn-gauge-legend dt[data-which="proposed"]::before{background:var(--jn-accent-strong)}
.jn-gauge-legend dd{margin:0;text-align:right;font-weight:620}
.jn-gauge-note{margin-top:.625rem;font-size:.75rem;color:var(--jn-ink-muted)}

/* Effective window strip -------------------------------------------- */
.jn-window{margin-top:var(--jn-s3);border:1px solid var(--jn-hairline);
border-radius:var(--jn-r2);padding:.875rem 1rem;background:var(--jn-surface-sunk)}
.jn-strip{display:flex;flex-direction:column;gap:.5rem}
@media (min-width:44rem){.jn-strip{flex-direction:row;gap:0}}
.jn-stop{position:relative;flex:1 1 0;display:flex;flex-direction:column;gap:.0625rem;
padding-left:1.125rem}
@media (min-width:44rem){.jn-stop{padding-left:0;padding-top:1.125rem}}
.jn-stop-dot{position:absolute;left:0;top:.375rem;width:.625rem;height:.625rem;
border-radius:50%;border:2px solid var(--jn-control-border);background:var(--jn-surface)}
@media (min-width:44rem){.jn-stop-dot{left:0;top:0}
.jn-stop::before{content:"";position:absolute;left:.625rem;right:0;top:.25rem;height:2px;
background:var(--jn-hairline)}
.jn-stop:last-child::before{display:none}}
.jn-stop[data-which="effective"] .jn-stop-dot{border-color:var(--jn-accent);
background:var(--jn-accent)}
.jn-stop-label{font-size:.6875rem;font-weight:660;letter-spacing:.05em;
text-transform:uppercase;color:var(--jn-ink-muted)}
.jn-stop-value{font-size:.875rem;font-weight:620;font-variant-numeric:tabular-nums}
.jn-stop[data-which="effective"] .jn-stop-value{color:var(--jn-accent)}

/* Work items, evidence, quiet states -------------------------------- */
.jn-workitems{display:flex;flex-direction:column;gap:.75rem}
.jn-workitem{border:1px solid var(--jn-hairline);border-radius:var(--jn-r2);
padding:.75rem .875rem;background:var(--jn-surface-sunk)}
.jn-workitem-top{display:flex;align-items:flex-start;justify-content:space-between;
gap:.75rem;margin-bottom:.375rem}
.jn-workitem-kind{font-size:.875rem;font-weight:640;display:flex;align-items:center;gap:.375rem}
.jn-workitem-icon{color:var(--jn-ink-muted)}
.jn-workitem-lines{display:flex;flex-direction:column;gap:.125rem;
font-size:.8125rem;color:var(--jn-ink-muted)}
.jn-evidence{display:flex;flex-wrap:wrap;gap:.375rem}
.jn-evidence li{background:var(--jn-surface-sunk);border:1px solid var(--jn-hairline);
border-radius:var(--jn-r1);padding:.1875rem .5rem;font-family:var(--jn-mono);
font-size:.75rem;overflow-wrap:anywhere}
.jn-quiet{display:flex;gap:.625rem;align-items:flex-start;
border:1px dashed var(--jn-control-border);border-radius:var(--jn-r2);
padding:.875rem 1rem;color:var(--jn-ink-muted);background:var(--jn-surface-sunk)}
.jn-quiet-icon{flex:none;margin-top:.125rem}

/* Actions rail ------------------------------------------------------ */
.jn-actions{display:flex;flex-direction:column;gap:var(--jn-s2)}
.jn-action{display:flex;flex-direction:column;gap:.625rem;
border-top:3px solid var(--jn-hairline)}
.jn-action[data-variant="primary"]{border-top-color:var(--jn-accent)}
.jn-action[data-variant="danger"]{border-top-color:var(--jn-danger)}
.jn-action h3{letter-spacing:-.014em}
.jn-action-desc{font-size:.8125rem;color:var(--jn-ink-muted)}
.jn-actsas{display:flex;align-items:center;gap:.375rem;font-size:.75rem;
color:var(--jn-info);background:var(--jn-info-soft);border-radius:var(--jn-r1);
padding:.3125rem .5rem}
.jn-blocked{display:flex;align-items:flex-start;gap:.375rem;font-size:.75rem;
color:var(--jn-warning);background:var(--jn-warning-soft);border-radius:var(--jn-r1);
padding:.3125rem .5rem}
.jn-confirm{border-top:1px solid var(--jn-hairline);padding-top:.625rem}
.jn-confirm>summary{list-style:none;width:100%;text-align:center;cursor:pointer}
.jn-confirm>summary::-webkit-details-marker{display:none}
.jn-confirm[open]>summary{display:none}
.jn-confirm-body{display:flex;flex-direction:column;gap:.625rem;padding-top:.125rem}
.jn-confirm-title{font-size:.875rem;font-weight:680}
.jn-confirm-facts{display:grid;grid-template-columns:1fr;gap:.5rem}
.jn-confirm-facts .jn-fact dd{font-size:.8125rem}
.jn-confirm-note{display:flex;align-items:flex-start;gap:.375rem;padding:.5rem;
border-radius:var(--jn-r1);background:var(--jn-warning-soft);color:var(--jn-warning);
font-size:.75rem}
.jn-confirm-icon{flex:none;margin-top:.0625rem}

/* Timeline ---------------------------------------------------------- */
.jn-timeline{display:flex;flex-direction:column}
.jn-tl{position:relative;display:flex;gap:.75rem;padding-bottom:var(--jn-s2)}
.jn-tl:last-child{padding-bottom:0}
.jn-tl::before{content:"";position:absolute;left:.4375rem;top:1.125rem;bottom:0;
width:2px;background:var(--jn-hairline)}
.jn-tl:last-child::before{display:none}
.jn-tldot{flex:none;width:1rem;height:1rem;border-radius:50%;margin-top:.1875rem;
border:3px solid var(--jn-neutral);background:var(--jn-surface);position:relative;z-index:1}
.jn-tl[data-tone="info"] .jn-tldot{border-color:var(--jn-info)}
.jn-tl[data-tone="success"] .jn-tldot{border-color:var(--jn-success)}
.jn-tl[data-tone="warning"] .jn-tldot{border-color:var(--jn-warning)}
.jn-tl[data-tone="danger"] .jn-tldot{border-color:var(--jn-danger)}
.jn-tl:first-child .jn-tldot{animation:jn-halo 2.6s var(--jn-ease) infinite}
.jn-tlbody{min-width:0;display:flex;flex-direction:column;gap:.125rem}
.jn-tlat{font-size:.75rem;color:var(--jn-ink-muted);font-variant-numeric:tabular-nums}
.jn-tltitle{font-size:.875rem;font-weight:620}
.jn-tldetail{font-size:.8125rem;color:var(--jn-ink-muted)}

/* Footer ------------------------------------------------------------ */
.jn-footer{border-top:1px solid var(--jn-hairline);background:var(--jn-surface)}
.jn-footer-inner{padding-top:var(--jn-s3);padding-bottom:var(--jn-s3);
display:flex;flex-direction:column;gap:.25rem;
font-size:.75rem;color:var(--jn-ink-muted)}
.jn-provenance{display:flex;flex-wrap:wrap;gap:.25rem 1rem}

/* Motion ------------------------------------------------------------ */
.jn-network-stage{position:relative;min-height:24rem}
.jn-network-stale{opacity:.22;pointer-events:none;user-select:none}
.jn-network-proxy{position:absolute;inset:0;z-index:2;padding:var(--jn-s3);overflow:hidden}
.jn-proxy-toolbar,.jn-proxy-row{display:flex;align-items:center;gap:var(--jn-s2)}
.jn-proxy-toolbar{justify-content:space-between;min-height:3rem;padding-bottom:var(--jn-s2);border-bottom:1px solid var(--jn-hairline)}
.jn-proxy-rows{display:grid}
.jn-proxy-row{min-height:4.75rem;border-bottom:1px solid var(--jn-hairline)}
.jn-proxy-block{position:relative;display:block;overflow:hidden;border-radius:var(--jn-r1);background:var(--jn-surface-muted)}
.jn-proxy-heading{width:min(18rem,54%);height:1rem}
.jn-proxy-control{width:9rem;height:2.5rem}
.jn-proxy-avatar{flex:none;width:2.5rem;height:2.5rem;border-radius:50%}
.jn-proxy-copy{display:grid;flex:1;gap:.625rem;min-width:0}
.jn-proxy-line{width:72%;height:.75rem}
.jn-proxy-line-short{width:42%}
.jn-proxy-chip{display:none;flex:none;width:5.5rem;height:1.625rem;border-radius:999px}
.jn-proxy-control{width:6rem}
@media (prefers-reduced-motion:no-preference){
:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .jn-proxy-block::after,
:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .jn-loading::after{
content:"";position:absolute;inset:0;background:linear-gradient(100deg,transparent 18%,rgba(255,255,255,.58) 48%,transparent 78%);animation:jn-sweep 1.25s linear infinite;pointer-events:none}
}
@media(min-width:40rem){.jn-proxy-chip{display:block}.jn-proxy-control{width:9rem}}
@keyframes jn-slidein{from{opacity:0;transform:translateY(6px)}to{opacity:1;transform:none}}
@keyframes jn-pop{from{opacity:0;transform:scale(.4)}60%{opacity:1;transform:scale(1.15)}to{transform:scale(1)}}
@keyframes jn-grow-y{from{height:0}to{height:calc(100% - 2rem)}}
@keyframes jn-grow-x{from{width:0}to{width:calc(100% - 2.75rem)}}
@keyframes jn-grow-x-svg{from{transform:scaleX(0)}to{transform:scaleX(1)}}
@keyframes jn-drop{from{opacity:0;transform:translateY(-6px)}to{opacity:1;transform:none}}
@keyframes jn-sweep{from{transform:translateX(-100%)}to{transform:translateX(140%)}}
@keyframes jn-halo{0%,100%{box-shadow:0 0 0 0 rgba(43,58,143,.30)}50%{box-shadow:0 0 0 6px rgba(43,58,143,0)}}
@keyframes jn-amber{0%,100%{box-shadow:inset 0 0 0 1px rgba(122,74,0,.20)}50%{box-shadow:inset 0 0 0 1px rgba(122,74,0,.20),0 0 0 4px rgba(122,74,0,.10)}}
@keyframes jn-alert{0%,100%{box-shadow:inset 0 0 0 1px rgba(155,17,48,.25)}50%{box-shadow:inset 0 0 0 1px rgba(155,17,48,.25),0 0 0 4px rgba(155,17,48,.12)}}
@keyframes jn-breathe{0%,100%{opacity:1}50%{opacity:.82}}

@media print{
body{background:var(--jn-surface)}
.jn-masthead,.jn-skip,.jn-actions,.jn-btn{display:none}
.jn-card,.jn-panel{box-shadow:none;break-inside:avoid}
.jn-columns{grid-template-columns:minmax(0,1fr)}
.jn-rail{position:static;max-height:none;overflow:visible}
}
@media (prefers-reduced-motion:reduce){
*,*::before,*::after{animation-duration:.001ms !important;animation-iteration-count:1 !important;
transition-duration:.001ms !important;scroll-behavior:auto !important}
.jn-journey:hover{transform:none}
.jn-checkpill::after{display:none}
}
`
