package productui

import (
	"strings"
	"testing"
)

func TestNavigationScrollbarUsesSemanticTokensAndNativeFallbacks(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`--hcm-nav-scrollbar-track:color-mix(in srgb,var(--surface)`,
		`.sidebar{scrollbar-width:thin;scrollbar-color:var(--hcm-nav-scrollbar-thumb)`,
		`.sidebar::-webkit-scrollbar{width:10px}`,
		`.sidebar::-webkit-scrollbar-thumb{min-height:44px`,
		`.sidebar::-webkit-scrollbar-thumb:hover{background:var(--hcm-nav-scrollbar-thumb-hover)`,
		`.sidebar::-webkit-scrollbar-thumb:active{background:var(--hcm-nav-scrollbar-thumb-active)`,
		`@media(forced-colors:active){.sidebar{scrollbar-color:ButtonText Canvas}`,
		`.sidebar{overflow:hidden}.primary-nav{flex:1;min-height:0;overflow-y:auto`,
		`.primary-nav::-webkit-scrollbar-thumb{min-height:44px`,
		`.primary-nav{overflow-x:hidden}`,
		`@media(forced-colors:active){.primary-nav{scrollbar-color:ButtonText Canvas}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("navigation scrollbar is missing %q", want)
		}
	}
	if strings.Contains(css, `.main-scroll::-webkit-scrollbar`) {
		t.Fatal("navigation scrollbar styling leaked into the page scroll region")
	}
}
