package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-054: risk-bound step-up authentication. When the server
// projects a step-up challenge, the shell must surface a prompt naming the
// exact sensitive action, the server's reason, and the challenge
// destination — bound to that action, never a blanket re-login, and never
// authorizing anything itself.
func TestTodo_WEB_054(t *testing.T) {
	view := testView(PageHome)
	view.StepUpChallenge = &StepUpChallengeProps{
		ActionLabel:   "Approve promotion for Avery Patel",
		ReasonDetail:  "Approving compensation changes needs substantial assurance.",
		ChallengeHref: "/workspace/app/journeys?stepup=promotion-9",
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	prompt := findElementByID(root, "step-up-challenge")
	if prompt == nil {
		t.Fatal("projected step-up challenge renders no prompt")
	}
	body := textContent(prompt)
	for _, want := range []string{"Approve promotion for Avery Patel", "Approving compensation changes needs substantial assurance."} {
		if !strings.Contains(body, want) {
			t.Fatalf("step-up prompt missing %q", want)
		}
	}
	challenge := findFirst(prompt, "a")
	if challenge == nil {
		t.Fatal("step-up prompt has no challenge link")
	}
	if href := xhtmlAttr(challenge, "href"); !strings.HasPrefix(href, "/workspace/app/journeys") {
		t.Fatalf("challenge link = %q, want the projected challenge destination", href)
	}
	if dismiss := findElementByID(root, "step-up-dismiss"); dismiss == nil || xhtmlAttr(dismiss, "aria-label") == "" {
		t.Fatal("step-up prompt has no labelled dismiss control")
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
	if findElementByID(quietRoot, "step-up-challenge") != nil {
		t.Fatal("shell renders a step-up prompt without a projection")
	}
}

// Golden: the step-up prompt for a fixed projection.
func TestTodo_WEB_054_Golden(t *testing.T) {
	props := StepUpChallengeProps{
		I18nProps:     I18nProps{Locale: ResolveProductLocale("")},
		ActionLabel:   "Approve promotion for Avery Patel",
		ReasonDetail:  "Approving compensation changes needs substantial assurance.",
		ChallengeHref: "/workspace/app/journeys?stepup=promotion-9",
	}
	node, err := ui.RenderToString(StepUpChallenge(props))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	got := hex.EncodeToString(digest[:])
	const want = "24c5c9f1bfeb1e6da44c2e655be83a62d29a8ead0725e757ebeb58d8fb0f44a9"
	if got != want {
		t.Fatalf("step-up prompt golden digest = %s, want %s", got, want)
	}
}

// Browser: prompt placement after the session warning slot.
func TestTodo_WEB_054_Browser(t *testing.T) {
	view := testView(PageHome)
	view.SessionWarning = &SessionWarningProps{Detail: "Ending.", ReauthHref: "/workspace/app/settings"}
	view.StepUpChallenge = &StepUpChallengeProps{
		ActionLabel:   "Approve promotion for Avery Patel",
		ReasonDetail:  "Reason.",
		ChallengeHref: "/workspace/app/journeys?stepup=promotion-9",
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
	warning := findElementByID(root, "session-warning")
	prompt := findElementByID(root, "step-up-challenge")
	if shell == nil || warning == nil || prompt == nil {
		t.Fatal("shell chrome incomplete for banner ordering")
	}
	if !isBeforeIn(shell, warning, prompt) {
		t.Fatal("step-up prompt is not placed after the session warning")
	}
	grid := findClassNode(shell, "shell-grid")
	if grid == nil || !isBeforeIn(shell, prompt, grid) {
		t.Fatal("step-up prompt is not placed before content")
	}
}

// Conformance: locales, stylesheet, destination policy.
func TestTodo_WEB_054_Conformance(t *testing.T) {
	for locale, title := range map[string]string{"en-US": "Additional verification needed", "de-DE": "Zusätzliche Verifizierung erforderlich", "ar": "يلزم تحقق إضافي"} {
		props := StepUpChallengeProps{
			I18nProps:     I18nProps{Locale: ResolveProductLocale(locale)},
			ActionLabel:   "Action.",
			ReasonDetail:  "Reason.",
			ChallengeHref: "/workspace/app/journeys?stepup=1",
		}
		node, err := ui.RenderToString(StepUpChallenge(props))
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
	eval := StepUpChallengeProps{ChallengeHref: "javascript:alert(1)"}
	if validRecoveryHref(eval.ChallengeHref) {
		t.Fatal("step-up challenge accepts a script destination")
	}
	css := Stylesheet()
	for _, want := range []string{".step-up-challenge", ".step-up-dismiss"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}
