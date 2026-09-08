package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-058: safe view-as policy simulation. When the server
// projects a policy simulation, the shell must surface a panel naming the
// simulated subject, the simulated outcome, the server's redaction-safe
// reason, the matched rules, policy versions, and evidence reference —
// headlined as a simulation that assumes no authority, with an exit back
// to the viewer's own view. Presentation never evaluates policy itself:
// no subject means no panel, and an unsafe exit destination means no link.
// A simulation is never locally dismissible: hiding the panel must not
// pretend the simulation ended.
func TestTodo_WEB_058(t *testing.T) {
	view := testView(PageHome)
	view.PolicySimulation = &PolicySimulationProps{
		Subject:      "Avery Patel (manager)",
		Allowed:      false,
		ReasonDetail: "Compensation fields are withheld for this purpose.",
		Rules:        []string{"compensation.withhold", "tenure.read"},
		Versions:     []string{"policy-2026-09-01"},
		EvidenceRef:  "ev:authz:9f2c",
		ExitHref:     "/workspace/app/home",
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	shell := findAppShell(root)
	if shell == nil {
		t.Fatal("document renders no app shell")
	}
	panel := findElementByID(root, "policy-simulation")
	if panel == nil {
		t.Fatal("projected policy simulation renders no panel")
	}
	body := textContent(panel)
	for _, want := range []string{"Avery Patel (manager)", "Compensation fields are withheld", "compensation.withhold", "policy-2026-09-01", "ev:authz:9f2c"} {
		if !strings.Contains(body, want) {
			t.Fatalf("simulation panel missing %q: %q", want, body)
		}
	}
	if findElementByID(root, "policy-simulation-dismiss") != nil {
		t.Fatal("simulation panel offers a local dismiss that would lie about the simulation ending")
	}
	exit := findFirst(panel, "a")
	if exit == nil {
		t.Fatal("simulation panel has no exit link")
	}
	if href := xhtmlAttr(exit, "href"); href != "/workspace/app/home" {
		t.Fatalf("simulation exit = %q, want the projected exit destination", href)
	}
	header := findFirst(shell, "header")
	grid := findClassNode(shell, "shell-grid")
	if header == nil || grid == nil {
		t.Fatal("shell chrome incomplete")
	}
	if !isBeforeIn(shell, header, panel) || !isBeforeIn(shell, panel, grid) {
		t.Fatal("simulation panel is not placed between topbar and content")
	}

	// No projection means no panel.
	quietDoc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	quietRoot, err := xhtml.Parse(strings.NewReader(quietDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(quietRoot, "policy-simulation") != nil {
		t.Fatal("shell renders a simulation panel without a projection")
	}
}

// Golden: the simulation panel for a fixed projection.
func TestTodo_WEB_058_Golden(t *testing.T) {
	node, err := ui.RenderToString(PolicySimulation(web058Fixture()))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	if got := hex.EncodeToString(digest[:]); got != "21058a5df3f9edb790ca7fb8109e782a5e92b31844485868c8bab0b109e8df3d" {
		t.Fatalf("simulation golden mismatch:\n%s\nwant digest 21058a5df3f9edb790ca7fb8109e782a5e92b31844485868c8bab0b109e8df3d", node)
	}
}

// Browser: the panel parses as a labelled section with a projected exit
// destination and no positive tabindex stops.
func TestTodo_WEB_058_Browser(t *testing.T) {
	view := testView(PageHome)
	view.PolicySimulation = &PolicySimulationProps{
		Subject:     "Avery Patel (manager)",
		Allowed:     true,
		Rules:       []string{"tenure.read"},
		Versions:    []string{"policy-2026-09-01"},
		EvidenceRef: "ev:authz:9f2c",
		ExitHref:    "/workspace/app/home",
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	panel := findElementByID(root, "policy-simulation")
	if panel == nil {
		t.Fatal("rendered document has no simulation panel")
	}
	labelledBy := xhtmlAttr(panel, "aria-labelledby")
	if labelledBy == "" || findElementByID(root, labelledBy) == nil {
		t.Fatalf("simulation panel labelling element %q missing", labelledBy)
	}
	var positive int
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "tabindex" && strings.TrimSpace(attr.Val) != "" && attr.Val != "0" && !strings.HasPrefix(attr.Val, "-") {
					positive++
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(panel)
	if positive != 0 {
		t.Fatalf("simulation panel carries %d positive tabindex stops", positive)
	}
}

// Conformance: locales, stylesheet, fail-closed projections.
func TestTodo_WEB_058_Conformance(t *testing.T) {
	for locale, title := range map[string]string{"en-US": "Policy simulation", "de-DE": "Richtliniensimulation", "ar": "محاكاة السياسة"} {
		props := web058Fixture()
		props.I18nProps = I18nProps{Locale: ResolveProductLocale(locale)}
		node, err := ui.RenderToString(PolicySimulation(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, title) {
			t.Fatalf("%s panel missing title %q: %s", locale, title, node)
		}
		if strings.Contains(node, "⟦") {
			t.Fatalf("%s panel leaks an unresolved key: %s", locale, node)
		}
	}
	for locale, notice := range map[string]string{
		"en-US": "Simulation — no authority is assumed.",
		"de-DE": "Simulation – es wird keine Berechtigung übernommen.",
		"ar":    "محاكاة — لا يتم تولي أي سلطة.",
	} {
		props := web058Fixture()
		props.I18nProps = I18nProps{Locale: ResolveProductLocale(locale)}
		node, err := ui.RenderToString(PolicySimulation(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, notice) {
			t.Fatalf("%s panel missing simulation notice %q: %s", locale, notice, node)
		}
	}
	// No subject means no panel; an unsafe exit means no link.
	empty := web058Fixture()
	empty.Subject = "  "
	node, err := ui.RenderToString(PolicySimulation(empty))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(node, "policy-simulation") {
		t.Fatalf("subject-less projection renders a panel: %s", node)
	}
	unsafe := web058Fixture()
	unsafe.ExitHref = "javascript:exit()"
	unsafeNode, err := ui.RenderToString(PolicySimulation(unsafe))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unsafeNode, "javascript:") || strings.Contains(unsafeNode, "policy-simulation-exit") {
		t.Fatalf("unsafe exit survives: %s", unsafeNode)
	}
	css := Stylesheet()
	for _, want := range []string{".policy-simulation", ".policy-simulation-title", ".policy-simulation-notice", ".policy-simulation-detail", ".policy-simulation-rules", ".policy-simulation-exit"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func web058Fixture() PolicySimulationProps {
	return PolicySimulationProps{
		I18nProps:    I18nProps{Locale: ResolveProductLocale("en-US")},
		Subject:      "Avery Patel (manager)",
		Allowed:      false,
		ReasonDetail: "Compensation fields are withheld for this purpose.",
		Rules:        []string{"compensation.withhold", "tenure.read"},
		Versions:     []string{"policy-2026-09-01"},
		EvidenceRef:  "ev:authz:9f2c",
		ExitHref:     "/workspace/app/home",
	}
}
