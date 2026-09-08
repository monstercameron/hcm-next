package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-052: session-expiry warning. When the server projects an
// expiring session, the shell must surface a labelled warning banner with
// the server's detail text and a re-authentication link; without the
// projection the shell renders nothing. Timing truth stays server-side:
// the frontend never computes, thresholds, or invents expiry.
func TestTodo_WEB_052(t *testing.T) {
	view := testView(PageHome)
	view.SessionWarning = &SessionWarningProps{
		Detail:     "Your session ends in 10 minutes.",
		ReauthHref: "/workspace/app/settings",
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	banner := findElementByID(root, "session-warning")
	if banner == nil {
		t.Fatal("projected session warning renders no banner")
	}
	if textContent(findFirst(banner, "h2")) == "" {
		t.Fatal("session warning has no heading")
	}
	if !strings.Contains(textContent(banner), "Your session ends in 10 minutes.") {
		t.Fatal("session warning drops the server detail text")
	}
	reauth := findFirst(banner, "a")
	reauthParsed, reauthErr := url.Parse(xhtmlAttr(reauth, "href"))
	if reauth == nil || reauthErr != nil || reauthParsed.Path != "/workspace/app/settings" {
		t.Fatal("session warning has no re-authentication link to the projected destination")
	}
	dismiss := findElementByID(root, "session-warning-dismiss")
	if dismiss == nil || xhtmlAttr(dismiss, "aria-label") == "" {
		t.Fatal("session warning has no labelled dismiss control")
	}

	// No projection means no banner and no shell shift.
	quietDoc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	quietRoot, err := xhtml.Parse(strings.NewReader(quietDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(quietRoot, "session-warning") != nil {
		t.Fatal("shell renders a session warning without a projection")
	}
}

// Golden: the warning banner for a fixed projection.
func TestTodo_WEB_052_Golden(t *testing.T) {
	props := SessionWarningProps{
		I18nProps:  I18nProps{Locale: ResolveProductLocale("")},
		Detail:     "Your session ends in 10 minutes.",
		ReauthHref: "/workspace/app/settings",
	}
	node, err := ui.RenderToString(SessionWarning(props))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	got := hex.EncodeToString(digest[:])
	const want = "93253689621cb5e8ef3f79933a8713622fa7e535b83eebb3430a26bbf1743957"
	if got != want {
		t.Fatalf("session warning golden digest = %s, want %s", got, want)
	}
}

// Browser: banner placement between topbar and content.
func TestTodo_WEB_052_Browser(t *testing.T) {
	view := testView(PageHistory)
	view.SessionWarning = &SessionWarningProps{
		Detail:     "Your session ends in 10 minutes.",
		ReauthHref: "/workspace/app/settings",
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
	banner := findElementByID(root, "session-warning")
	if banner == nil {
		t.Fatal("document renders no session warning")
	}
	header := findFirst(shell, "header")
	grid := findClassNode(shell, "shell-grid")
	if header == nil || grid == nil {
		t.Fatal("shell chrome incomplete")
	}
	if !isBeforeIn(shell, header, banner) || !isBeforeIn(shell, banner, grid) {
		t.Fatal("session warning is not placed between topbar and content")
	}
}

// Conformance: locales, stylesheet, no time logic in presentation.
func TestTodo_WEB_052_Conformance(t *testing.T) {
	for locale, title := range map[string]string{"en-US": "Your session is expiring", "de-DE": "Ihre Sitzung läuft bald ab", "ar": "جلستك على وشك الانتهاء"} {
		props := SessionWarningProps{
			I18nProps:  I18nProps{Locale: ResolveProductLocale(locale)},
			Detail:     "Detail.",
			ReauthHref: "/workspace/app/settings",
		}
		node, err := ui.RenderToString(SessionWarning(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, title) {
			t.Fatalf("%s warning missing title %q: %s", locale, title, node)
		}
		if strings.Contains(node, "⟦") {
			t.Fatalf("%s warning leaks an unresolved key: %s", locale, node)
		}
	}
	css := Stylesheet()
	for _, want := range []string{".session-warning", ".session-warning-dismiss"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func findAppShell(root *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "div" {
			for _, attr := range node.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, "app-shell") {
					found = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findClassNode(root *xhtml.Node, class string) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, class) {
					found = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}
