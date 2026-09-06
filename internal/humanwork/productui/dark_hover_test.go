package productui

import (
	"strings"
	"testing"
)

func TestPointerFeedbackUsesSemanticThemeTokens(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`--hcm-hover-surface:color-mix(in srgb,var(--accent) 10%,var(--surface))`,
		`--hcm-hover-border:color-mix(in srgb,var(--accent) 38%,var(--line))`,
		`.nav-favorite{color:var(--muted)}`,
		`.people-workflow-option):hover{border-color:var(--hcm-hover-border);background-color:var(--hcm-hover-surface);color:var(--accent)}`,
		`.global-search-result):hover{background-color:var(--hcm-hover-surface);color:var(--ink)}`,
		`.org-node):hover{border-color:var(--hcm-hover-border)}`,
		`.button.primary:hover{background:var(--accent-hover);color:var(--on-brand)}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("semantic hover coverage missing %q", want)
		}
	}
}

func TestPointerFeedbackRetainsForcedColorMeaning(t *testing.T) {
	css := Stylesheet()
	want := `@media(forced-colors:active){:where(.app-shell) :is(.nav-link,.nav-group-summary,.nav-favorite`
	if !strings.Contains(css, want) || !strings.Contains(css, `:hover{border-color:Highlight;background:Highlight;color:HighlightText}`) {
		t.Fatal("hover feedback does not retain a forced-color boundary")
	}
}
