package journey

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/uxqual/tokens"
)

// TestTextPairsMeetAA scores every foreground/background combination the
// page declares against WCAG 2.2 AA for normal text (1.4.3, 4.5:1) using
// tokens.ContrastRatio -- the same relative-luminance math the workspace
// palette is gated with, so both surfaces are held to one standard.
//
// The list is only as honest as it is complete: a renderer that paints a
// combination not in TextPairs is a gap this test cannot see, which is why
// TestStylesheetUsesOnlyPaletteHexes exists beside it.
func TestTextPairsMeetAA(t *testing.T) {
	for _, pair := range TextPairs() {
		t.Run(pair.Purpose, func(t *testing.T) {
			ratio, err := tokens.ContrastRatio(pair.Foreground.Hex, pair.Background.Hex)
			if err != nil {
				t.Fatalf("ContrastRatio(%s, %s): %v", pair.Foreground.Hex, pair.Background.Hex, err)
			}
			if ratio < tokens.MinRatioNormalText {
				t.Errorf("%s: %s (%s) on %s (%s) = %.2f:1, want at least %.1f:1",
					pair.Purpose, pair.Foreground.Name, pair.Foreground.Hex,
					pair.Background.Name, pair.Background.Hex, ratio, tokens.MinRatioNormalText)
			}
		})
	}
}

// TestUIPairsMeetNonTextAA holds the boundaries that carry meaning without
// text -- control edges, the focus ring, step markers -- to WCAG 2.2 AA
// non-text contrast (1.4.11, 3:1).
func TestUIPairsMeetNonTextAA(t *testing.T) {
	for _, pair := range UIPairs() {
		t.Run(pair.Purpose, func(t *testing.T) {
			ratio, err := tokens.ContrastRatio(pair.Foreground.Hex, pair.Background.Hex)
			if err != nil {
				t.Fatalf("ContrastRatio(%s, %s): %v", pair.Foreground.Hex, pair.Background.Hex, err)
			}
			if ratio < tokens.MinRatioLargeText {
				t.Errorf("%s: %s (%s) on %s (%s) = %.2f:1, want at least %.1f:1",
					pair.Purpose, pair.Foreground.Name, pair.Foreground.Hex,
					pair.Background.Name, pair.Background.Hex, ratio, tokens.MinRatioLargeText)
			}
		})
	}
}

func TestSwatchesAreWellFormedAndUnique(t *testing.T) {
	hexPattern := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	byName := make(map[string]string)
	for _, s := range Swatches() {
		if !hexPattern.MatchString(s.Hex) {
			t.Errorf("swatch %q has hex %q, want lowercase #rrggbb", s.Name, s.Hex)
		}
		if prev, dup := byName[s.Name]; dup {
			t.Errorf("swatch name %q declared twice (%s and %s)", s.Name, prev, s.Hex)
		}
		byName[s.Name] = s.Hex
	}
	if len(byName) == 0 {
		t.Fatal("Swatches() is empty")
	}
}

// TestStylesheetDeclaresEverySwatch is the join between the two
// representations of the palette: the Go values the contrast tests score
// and the CSS custom properties the browser actually paints with. Without
// it a swatch could be corrected in Go and left stale in the CSS, and every
// contrast assertion above would still pass while the page failed.
func TestStylesheetDeclaresEverySwatch(t *testing.T) {
	css := Stylesheet()
	for _, s := range Swatches() {
		decl := fmt.Sprintf("--jn-%s:%s;", s.Name, s.Hex)
		if !strings.Contains(css, decl) {
			t.Errorf("stylesheet does not declare %q", decl)
		}
	}
}

// TestStylesheetUsesOnlyPaletteHexes catches the other direction: a color
// hard-coded into a rule instead of taken from the palette would never be
// scored for contrast at all.
func TestStylesheetUsesOnlyPaletteHexes(t *testing.T) {
	declared := make(map[string]bool, len(Swatches()))
	for _, s := range Swatches() {
		declared[strings.ToLower(s.Hex)] = true
	}
	hexPattern := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	for _, match := range hexPattern.FindAllString(Stylesheet(), -1) {
		if !declared[strings.ToLower(match)] {
			t.Errorf("stylesheet uses hex %s, which is not a palette swatch", match)
		}
	}
}

// TestStylesheetIsSafeToInline guards the two things that would break the
// document Document writes: a closing style tag ends the block early, and a
// backtick cannot appear inside the Go raw string that holds it.
func TestStylesheetIsSafeToInline(t *testing.T) {
	css := Stylesheet()
	if css == "" {
		t.Fatal("Stylesheet() is empty")
	}
	if strings.Contains(strings.ToLower(css), "</style") {
		t.Error("stylesheet contains a closing style tag")
	}
	if strings.Contains(css, "`") {
		t.Error("stylesheet contains a backtick")
	}
	if strings.Contains(css, "\r") {
		t.Error("stylesheet contains a carriage return; the CSP hash is over the LF form")
	}
}

// TestStylesheetLoadsNothingExternal restates the CSP in a test: under
// `default-src 'none'` any url(), @import or @font-face would resolve to a
// blocked request and a silently missing asset.
func TestStylesheetLoadsNothingExternal(t *testing.T) {
	css := strings.ToLower(Stylesheet())
	for _, forbidden := range []string{"url(", "@import", "@font-face", "http://", "https://"} {
		if strings.Contains(css, forbidden) {
			t.Errorf("stylesheet contains %q; the content-security-policy blocks every external load", forbidden)
		}
	}
}

// TestStylesheetUsesTheSystemFontStacks checks that the two declared faces
// are the system stacks the brief pins, not a webfont name that would
// silently fall back.
func TestStylesheetUsesTheSystemFontStacks(t *testing.T) {
	css := Stylesheet()
	want := []string{
		`--jn-font:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;`,
		`--jn-mono:ui-monospace,"Cascadia Mono",Consolas,monospace;`,
	}
	for _, decl := range want {
		if !strings.Contains(css, decl) {
			t.Errorf("stylesheet does not declare %q", decl)
		}
	}
}

// TestBreakpointsOnlyAddColumns keeps the narrow layout the base case. A
// max-width query would mean the wide layout is written first and undone
// below the breakpoint, which is how 320px reflow regressions get in.
func TestBreakpointsOnlyAddColumns(t *testing.T) {
	queries := regexp.MustCompile(`@media\s*\(([^)]*)\)`).FindAllStringSubmatch(Stylesheet(), -1)
	if len(queries) == 0 {
		t.Fatal("stylesheet declares no media queries; the page cannot be responsive")
	}
	for _, q := range queries {
		condition := q[1]
		switch {
		case strings.Contains(condition, "min-width"):
		case strings.Contains(condition, "prefers-reduced-motion"):
		default:
			t.Errorf("media query (%s) is neither a min-width nor a motion preference", condition)
		}
	}
}

// TestStylesheetSizesInRelativeUnits checks the type scale is zoomable: a
// font-size in px does not respond to the browser's text-size setting
// (WCAG 1.4.4).
func TestStylesheetSizesInRelativeUnits(t *testing.T) {
	pxFontSize := regexp.MustCompile(`font-size:\s*[0-9.]+px`)
	if match := pxFontSize.FindString(Stylesheet()); match != "" {
		t.Errorf("stylesheet sets a pixel font size (%s); text zoom would not reach it", match)
	}
}
