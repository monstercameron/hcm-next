package productui

// FocusIndicator is the platform-owned visible-focus contract shared by every
// product page and reusable component. The dimensions are intentionally not
// customer-overridable: the platform selects a mode-aware protected focus
// color, while the indicator's visibility and geometry remain an
// accessibility boundary.
type FocusIndicator struct {
	Selector       string
	ColorVariable  string
	RingWidth      string
	SeparatorWidth string
	RingOffset     string
}

// VisibleFocusIndicator returns the immutable focus indicator contract used by
// the platform stylesheet. Returning a value (rather than exposing mutable
// package state) keeps SSR and browser publication deterministic.
func VisibleFocusIndicator() FocusIndicator {
	return FocusIndicator{
		Selector:       `:where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible`,
		ColorVariable:  "--hcm-color-focus",
		RingWidth:      "2px",
		SeparatorWidth: "2px",
		RingOffset:     "4px",
	}
}

// focusStyles is appended after customer and component styles. This makes
// visible focus a platform boundary: component states can style borders and
// surfaces, but cannot accidentally remove the keyboard indicator.
const focusStyles = `:root{--hcm-focus-ring-width:2px;--hcm-focus-ring-gap:2px;--hcm-focus-ring-offset:4px}:where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:var(--hcm-focus-ring-offset);box-shadow:0 0 0 var(--hcm-focus-ring-gap) var(--surface)}.wordmark:focus-visible{outline-offset:calc(var(--hcm-focus-ring-offset) * -1);box-shadow:none}.jn-embedded :where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:var(--hcm-focus-ring-offset);box-shadow:0 0 0 var(--hcm-focus-ring-gap) var(--surface)}.jn-embedded .jn-masthead :where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible{outline-color:var(--hcm-color-on-brand)}@media(prefers-reduced-motion:reduce){:where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible{animation:none!important;transition:none!important}}@media(forced-colors:active){:where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible{outline:2px solid Highlight!important;outline-offset:2px;box-shadow:0 0 0 2px Canvas!important;forced-color-adjust:auto}.wordmark:focus-visible{outline-offset:-4px!important;box-shadow:none!important}}`

// journeyFocusBridgeStyles gives the embedded Journey renderer the same
// semantic focus token and ring geometry as the product shell. Standalone
// Journey documents still use their own accent fallback; embedded documents
// inherit the protected product focus color and gap token.
const journeyFocusBridgeStyles = `.jn-embedded{--jn-focus-color:var(--hcm-color-focus);--jn-focus-ring-width:var(--hcm-focus-ring-width);--jn-focus-ring-offset:var(--hcm-focus-ring-offset);--jn-ring:0 0 0 var(--hcm-focus-ring-gap) var(--hcm-color-focus)}@media(forced-colors:active){.jn-embedded{--jn-focus-color:Highlight;--jn-ring:0 0 0 2px Highlight}}`
