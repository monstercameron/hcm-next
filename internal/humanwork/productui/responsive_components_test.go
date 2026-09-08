package productui

import (
	"strings"
	"testing"
)

func TestSharedComponentsOwnResponsiveSizingContracts(t *testing.T) {
	css := Stylesheet()
	contracts := []string{
		`:where(.main,.page-stack,.surface,.panel,.section-head`,
		`:where(input,select,textarea,button){max-width:100%;}`,
		`.section-head,.panel-foot,.directory-tools,.toolbar{flex-wrap:wrap;}`,
		`.tabs{max-width:100%;overflow-x:auto`,
		`.notifications .popover,.locale-menu .popover{max-width:calc(100vw - 24px);}`,
		`.jn-embedded .jn-tablewrap{overflow-x:auto`,
	}
	for _, contract := range contracts {
		if !strings.Contains(css, contract) {
			t.Errorf("shared responsive contract missing %q", contract)
		}
	}
}

func TestMobileShellKeepsOneBoundedNavigableTree(t *testing.T) {
	css := Stylesheet()
	contracts := []string{
		`.shell-grid,.app-shell.nav-collapsed .shell-grid{grid-template-columns:minmax(0,1fr);grid-template-rows:auto minmax(0,1fr);}`,
		`.brand-cluster,.app-shell.nav-collapsed .brand-cluster{align-items:center;border-right:0;display:grid!important;grid-template-columns:minmax(0,1fr) 44px!important;`,
		`.header-nav-toggle,.app-shell.nav-collapsed .header-nav-toggle{display:grid`,
		`@media (max-width:760px){.sidebar,.sidebar.collapsed{border-bottom:1px solid var(--line);border-right:0;display:flex;height:auto;max-height:min(44dvh,420px);`,
		`.app-shell.nav-collapsed .sidebar{border-bottom:0;max-height:0;opacity:0;padding-block:0;pointer-events:none;visibility:hidden;}`,
		`overflow-x:hidden!important;overflow-y:auto!important`,
		`.primary-nav>ul,.sidebar nav:first-of-type>ul{display:grid!important;max-width:100%!important;width:100%!important;}`,
	}
	for _, contract := range contracts {
		if !strings.Contains(css, contract) {
			t.Errorf("mobile navigation contract missing %q", contract)
		}
	}
	if strings.Count(css, `id="workspace-navigation"`) != 0 {
		t.Fatal("stylesheet unexpectedly contains markup")
	}
}

func TestNarrowContentReflowsInsteadOfClipping(t *testing.T) {
	css := Stylesheet()
	contracts := []string{
		`@media (max-width:760px){.work-row{column-gap:12px;display:grid;grid-template-columns:auto minmax(0,1fr);`,
		`.facts>div{display:grid;grid-template-columns:minmax(0,0.8fr) minmax(0,1.2fr);}`,
		`.settings-nav{flex-wrap:nowrap;overflow-x:auto;overscroll-behavior-inline:contain;}`,
		`.people-row-actions{flex-direction:column;}`,
		`.topbar>.avatar{display:none;}`,
	}
	for _, contract := range contracts {
		if !strings.Contains(css, contract) {
			t.Errorf("narrow component contract missing %q", contract)
		}
	}
}

func TestPeopleHeadersStickToTheMainScrollport(t *testing.T) {
	css := Stylesheet()
	for _, contract := range []string{
		`.people-directory{isolation:isolate;overflow:visible;position:relative;}`,
		`@supports(overflow:clip){.people-directory{overflow:clip;}}`,
		`.people-directory .people-columns{background-color:var(--surface-subtle);box-shadow:0 1px 0 var(--line),0 10px 18px color-mix(in srgb,var(--ink) 8%,transparent);position:sticky;top:0;z-index:4;}`,
		`@media (print){.people-directory .people-columns{box-shadow:none;position:static;}`,
	} {
		if !strings.Contains(css, contract) {
			t.Errorf("sticky people header contract missing %q", contract)
		}
	}
}
