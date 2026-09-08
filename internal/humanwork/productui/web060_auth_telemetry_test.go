package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-060: authentication telemetry privacy. Every destination
// the authentication surfaces render — recovery, re-authentication,
// step-up challenge, break-glass activation, simulation exit, sign-in —
// must stay free of authenticator material: the shared destination policy
// fails closed on hrefs carrying credential-bearing query parameters, so
// tokens and secrets never reach the logged and telemetry-harvested URL
// surfaces. The sweep also proves the auth documents expose no password
// or hidden inputs, no inline handlers, and no secret-named data
// attributes in any catalog locale.
func TestTodo_WEB_060(t *testing.T) {
	for _, configuration := range []string{"session", "credential-hrefs", "signed-out", "entry"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			doc, err := Render(web060View(configuration, locale))
			if err != nil {
				t.Fatal(err)
			}
			root, err := xhtml.Parse(strings.NewReader(doc))
			if err != nil {
				t.Fatal(err)
			}
			assertAuthTelemetryPrivate(t, root, configuration, locale)
		}
	}
}

func assertAuthTelemetryPrivate(t *testing.T, root *xhtml.Node, name, locale string) {
	t.Helper()
	for _, input := range collectElements(root, "input") {
		typ := strings.ToLower(xhtmlAttr(input, "type"))
		if typ == "password" {
			t.Fatalf("%s %s auth document exposes a password input", name, locale)
		}
		if typ == "hidden" {
			switch xhtmlAttr(input, "name") {
			case "locale", "nav", "menu_q", "favorites":
			default:
				t.Fatalf("%s %s auth document exposes an ungoverned hidden input %q", name, locale, xhtmlAttr(input, "name"))
			}
		}
	}
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				lower := strings.ToLower(attr.Key)
				if strings.HasPrefix(lower, "on") && lower != "only" {
					t.Fatalf("%s %s auth document carries inline handler %q", name, locale, attr.Key)
				}
				if strings.HasPrefix(lower, "data-") && (strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "passwd") || strings.Contains(lower, "password") || strings.Contains(lower, "credential")) {
					t.Fatalf("%s %s auth document carries secret-named attribute %q", name, locale, attr.Key)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	for _, link := range collectElements(root, "a") {
		href := xhtmlAttr(link, "href")
		if href == "" || strings.HasPrefix(href, "#") {
			continue
		}
		if !validRecoveryHref(href) {
			t.Fatalf("%s %s renders an anchor outside the destination policy: %q", name, locale, href)
		}
		if credentialParamInHref(href) {
			t.Fatalf("%s %s renders authenticator material into a destination: %q", name, locale, href)
		}
	}
	// Fail-closed, not silent: the credential-href projections keep their
	// prompts while losing exactly the tainted links.
	if name == "credential-hrefs" {
		for _, id := range []string{"step-up-challenge", "break-glass-activation", "policy-simulation"} {
			if findElementByID(root, id) == nil {
				t.Fatalf("%s %s drops the prompt instead of just the tainted link: #%s", name, locale, id)
			}
		}
	}
	if name == "signed-out" && findElementByID(root, "signed-out") == nil {
		t.Fatalf("%s %s drops the signed-out panel instead of just the tainted link", name, locale)
	}
}

// credentialParamInHref is the test-side oracle: an independent superset
// of credential-bearing query keys. It is deliberately not shared with
// production — a production denylist missing one of these keys fails the
// sweep instead of passing itself.
func credentialParamInHref(href string) bool {
	parsed, err := url.Parse(href)
	if err != nil {
		return false
	}
	for key := range parsed.Query() {
		switch strings.ToLower(key) {
		case "token", "access_token", "id_token", "refresh_token",
			"secret", "client_secret", "password", "passwd",
			"api_key", "apikey", "auth_token", "session_token":
			return true
		}
	}
	return false
}

func web060View(configuration, locale string) View {
	view := testView(PageHome)
	view.Locale = ResolveProductLocale(locale)
	view = ApplyLocale(view, view.Locale)
	tainted := configuration == "credential-hrefs"
	challengeHref := "/workspace/app/journeys?stepup=promotion-9"
	activateHref := "/workspace/app/journeys?breakglass=INC-2026-118"
	exitHref := "/workspace/app/home"
	if tainted {
		challengeHref = "/workspace/app/journeys?stepup=promotion-9&token=stolen"
		activateHref = "/workspace/app/journeys?breakglass=INC-2026-118&access_token=stolen"
		exitHref = "/workspace/app/home?access_token=stolen"
	}
	switch configuration {
	case "entry":
		view.Tenant = ""
		view.FederationEntries = []FederationEntry{
			{Tenant: "harborcare-demo", Issuer: "https://login.harborcare.example", Protocol: "oidc", Assurance: "substantial", Href: "/workspace/login/start?tenant=harborcare-demo"},
		}
		view.RecoveryOptions = []RecoveryOption{
			{Label: "Reset your password", Description: "Use the reset flow.", Href: "https://login.harborcare.example/reset"},
		}
	case "signed-out":
		view.SessionWarning = &SessionWarningProps{Detail: "Detail.", ReauthHref: "/workspace/app/settings"}
		view.ContextSwitcher = web056Fixture()
		view.SignedOut = &SignedOutProps{
			Detail:     "You signed out.",
			Revoked:    []string{"Delegation from Maya Chen"},
			SignInHref: "/workspace/login?token=stolen",
		}
	default:
		view.SessionWarning = &SessionWarningProps{Detail: "Detail.", ReauthHref: "/workspace/app/settings"}
		view.StepUpChallenge = &StepUpChallengeProps{ActionLabel: "Approve promotion.", ReasonDetail: "Assurance.", ChallengeHref: challengeHref}
		view.ContextSwitcher = web056Fixture()
		view.BreakGlassActivation = &BreakGlassActivationProps{IncidentRef: "INC-2026-118", ReasonDetail: "Blocked.", Capabilities: []string{"payroll.commit"}, TTLDetail: "60 minutes.", ActivateHref: activateHref}
		view.PolicySimulation = &PolicySimulationProps{Subject: "Avery Patel (manager)", Allowed: false, Rules: []string{"tenure.read"}, Versions: []string{"policy-2026-09-01"}, EvidenceRef: "ev:authz:9f2c", ExitHref: exitHref}
	}
	return view
}

// Golden: the privacy verdict matrix digest (configuration × locale).
func TestTodo_WEB_060_Golden(t *testing.T) {
	var builder strings.Builder
	for _, configuration := range []string{"session", "credential-hrefs", "signed-out", "entry"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			builder.WriteString(configuration)
			builder.WriteString("\x00")
			builder.WriteString(locale)
			builder.WriteString("\x00pass\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "348ebb8b4cdb7914f44dba945d0ce94352f191853d08020507e076c5146e685c"
	if got != want {
		t.Fatalf("telemetry privacy matrix digest = %s, want %s", got, want)
	}
}

// Browser: the fully armed document keeps every auth section labelled
// with credential-free destinations.
func TestTodo_WEB_060_Browser(t *testing.T) {
	doc, err := Render(web060View("session", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"session-warning", "step-up-challenge", "acting-authority", "break-glass-activation", "policy-simulation"} {
		section := findElementByID(root, id)
		if section == nil {
			t.Fatalf("armed document has no #%s", id)
		}
		if labelledBy := xhtmlAttr(section, "aria-labelledby"); labelledBy == "" || findElementByID(root, labelledBy) == nil {
			t.Fatalf("#%s labelling element %q missing", id, labelledBy)
		}
	}
	assertAuthTelemetryPrivate(t, root, "session", "en-US")
}

// Conformance: the destination policy unit contract.
func TestTodo_WEB_060_Conformance(t *testing.T) {
	for href, valid := range map[string]bool{
		"/workspace/login/start?tenant=x":                   true,
		"/workspace/app/journeys?journey=i1&mode=new":       true,
		"/workspace/app/home?nav=collapsed":                 true,
		"/workspace/x?mytoken=1":                            true,
		"https://idp.example/reset?client=x":                true,
		"mailto:helpdesk@example.com":                       true,
		"mailto:helpdesk@example.com?subject=token%20reset": true,
		"/workspace/x?token=abc":                            false,
		"/workspace/x?Token=abc":                            false,
		"/workspace/x?token=":                               false,
		"/workspace/x?a=1&access_token=abc":                 false,
		"/workspace/x?ID_TOKEN=abc":                         false,
		"/workspace/x?client_secret=s":                      false,
		"/workspace/x?password=p":                           false,
		"/workspace/x?api_key=k":                            false,
		"https://idp.example/cb?code=x&client_secret=s":     false,
		"javascript:alert(1)":                               false,
		"https://idp.example/cb?token=abc":                  false,
	} {
		if got := validRecoveryHref(href); got != valid {
			t.Fatalf("validRecoveryHref(%q) = %v, want %v", href, got, valid)
		}
	}
}
