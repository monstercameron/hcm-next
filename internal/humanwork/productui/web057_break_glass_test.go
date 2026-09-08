package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-057: break-glass activation experience. When the server
// projects a break-glass activation, the shell must surface a prompt naming
// the incident, the reason, the named capabilities, and the bounded TTL —
// with an activation link to the server's destination and a labelled
// dismiss. The prompt authorizes nothing itself and never mints a grant:
// no incident reference means no prompt, and an unsafe destination means
// no activation link.
func TestTodo_WEB_057(t *testing.T) {
	view := testView(PageHome)
	view.BreakGlassActivation = &BreakGlassActivationProps{
		IncidentRef:  "INC-2026-118",
		ReasonDetail: "Payroll commit is blocked and the on-call approver is unreachable.",
		Capabilities: []string{"payroll.commit", "ledger.read"},
		TTLDetail:    "Expires 60 minutes after activation.",
		ActivateHref: "/workspace/app/journeys?breakglass=INC-2026-118",
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
	prompt := findElementByID(root, "break-glass-activation")
	if prompt == nil {
		t.Fatal("projected break-glass activation renders no prompt")
	}
	body := textContent(prompt)
	for _, want := range []string{"INC-2026-118", "Payroll commit is blocked", "payroll.commit", "ledger.read", "60 minutes"} {
		if !strings.Contains(body, want) {
			t.Fatalf("break-glass prompt missing %q: %q", want, body)
		}
	}
	activate := findFirst(prompt, "a")
	if activate == nil {
		t.Fatal("break-glass prompt has no activation link")
	}
	if href := xhtmlAttr(activate, "href"); !strings.HasPrefix(href, "/workspace/app/journeys") {
		t.Fatalf("activation link = %q, want the projected activation destination", href)
	}
	if dismiss := findElementByID(root, "break-glass-dismiss"); dismiss == nil || xhtmlAttr(dismiss, "aria-label") == "" {
		t.Fatal("break-glass prompt has no labelled dismiss control")
	}
	header := findFirst(shell, "header")
	grid := findClassNode(shell, "shell-grid")
	if header == nil || grid == nil {
		t.Fatal("shell chrome incomplete")
	}
	if !isBeforeIn(shell, header, prompt) || !isBeforeIn(shell, prompt, grid) {
		t.Fatal("break-glass prompt is not placed between topbar and content")
	}

	// No projection means no prompt.
	quietDoc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	quietRoot, err := xhtml.Parse(strings.NewReader(quietDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(quietRoot, "break-glass-activation") != nil {
		t.Fatal("shell renders a break-glass prompt without a projection")
	}
}

// Golden: the break-glass prompt for a fixed projection.
func TestTodo_WEB_057_Golden(t *testing.T) {
	node, err := ui.RenderToString(BreakGlassActivation(web057Fixture()))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	if got := hex.EncodeToString(digest[:]); got != "3504c15cd2c284914a926bb4a71f9d39b7a1b237913e781afbfc7e35e6d0bcec" {
		t.Fatalf("break-glass golden mismatch:\n%s\nwant digest 3504c15cd2c284914a926bb4a71f9d39b7a1b237913e781afbfc7e35e6d0bcec", node)
	}
}

// Browser: the prompt parses as a labelled section with a projected
// activation destination and no positive tabindex stops.
func TestTodo_WEB_057_Browser(t *testing.T) {
	view := testView(PageHome)
	view.BreakGlassActivation = &BreakGlassActivationProps{
		IncidentRef:  "INC-2026-118",
		ReasonDetail: "Payroll commit is blocked.",
		Capabilities: []string{"payroll.commit"},
		TTLDetail:    "Expires 60 minutes after activation.",
		ActivateHref: "/workspace/app/journeys?breakglass=INC-2026-118",
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	prompt := findElementByID(root, "break-glass-activation")
	if prompt == nil {
		t.Fatal("rendered document has no break-glass prompt")
	}
	labelledBy := xhtmlAttr(prompt, "aria-labelledby")
	if labelledBy == "" || findElementByID(root, labelledBy) == nil {
		t.Fatalf("break-glass prompt labelling element %q missing", labelledBy)
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
	walk(prompt)
	if positive != 0 {
		t.Fatalf("break-glass prompt carries %d positive tabindex stops", positive)
	}
}

// Conformance: locales, stylesheet, fail-closed projections.
func TestTodo_WEB_057_Conformance(t *testing.T) {
	for locale, title := range map[string]string{"en-US": "Emergency access", "de-DE": "Notfallzugriff", "ar": "الوصول الطارئ"} {
		props := web057Fixture()
		props.I18nProps = I18nProps{Locale: ResolveProductLocale(locale)}
		node, err := ui.RenderToString(BreakGlassActivation(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, title) {
			t.Fatalf("%s prompt missing title %q: %s", locale, title, node)
		}
		if strings.Contains(node, "⟦") {
			t.Fatalf("%s prompt leaks an unresolved key: %s", locale, node)
		}
	}
	// No incident reference means no prompt; an unsafe destination means
	// no activation link.
	empty := web057Fixture()
	empty.IncidentRef = "  "
	node, err := ui.RenderToString(BreakGlassActivation(empty))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(node, "break-glass") {
		t.Fatalf("incident-less projection renders a prompt: %s", node)
	}
	unsafe := web057Fixture()
	unsafe.ActivateHref = "javascript:grant()"
	unsafeNode, err := ui.RenderToString(BreakGlassActivation(unsafe))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unsafeNode, "javascript:") {
		t.Fatalf("unsafe activation destination survives: %s", unsafeNode)
	}
	if strings.Contains(unsafeNode, "break-glass-activate") {
		t.Fatalf("unsafe projection keeps an activation link: %s", unsafeNode)
	}
	css := Stylesheet()
	for _, want := range []string{".break-glass-activation", ".break-glass-title", ".break-glass-detail", ".break-glass-capabilities", ".break-glass-activate", ".break-glass-dismiss"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func web057Fixture() BreakGlassActivationProps {
	return BreakGlassActivationProps{
		I18nProps:    I18nProps{Locale: ResolveProductLocale("en-US")},
		IncidentRef:  "INC-2026-118",
		ReasonDetail: "Payroll commit is blocked and the on-call approver is unreachable.",
		Capabilities: []string{"payroll.commit", "ledger.read"},
		TTLDetail:    "Expires 60 minutes after activation.",
		ActivateHref: "/workspace/app/journeys?breakglass=INC-2026-118",
	}
}
