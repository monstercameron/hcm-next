package productui

import (
	"net/url"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-053: reauthentication work restoration. The session
// warning's re-authentication link must carry the current address as a
// resume target so signing in again restores the user's place, composed
// from the shell's own current-address primitive — never a second record
// of where the user was.
func TestTodo_WEB_053(t *testing.T) {
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
	banner := findElementByID(root, "session-warning")
	if banner == nil {
		t.Fatal("document renders no session warning")
	}
	reauth := findFirst(banner, "a")
	if reauth == nil {
		t.Fatal("warning has no re-authentication link")
	}
	parsed, err := url.Parse(xhtmlAttr(reauth, "href"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/workspace/app/settings" {
		t.Fatalf("re-auth path = %q, want the projected destination", parsed.Path)
	}
	resume := parsed.Query().Get("resume")
	if resume == "" {
		t.Fatal("re-auth link carries no resume target")
	}
	resumeParsed, err := url.Parse(resume)
	if err != nil || !strings.HasPrefix(resumeParsed.Path, "/workspace/app/history") {
		t.Fatalf("resume target = %q, want the current history address", resume)
	}

	// Existing destination query survives alongside the resume target.
	withQuery := testView(PageHistory)
	withQuery.SessionWarning = &SessionWarningProps{
		Detail:     "Ending.",
		ReauthHref: "/workspace/app/settings?tab=session",
	}
	if got := reauthResumeHref(withQuery, withQuery.SessionWarning.ReauthHref); !strings.Contains(got, "tab=session") || !strings.Contains(got, "resume=") {
		t.Fatalf("re-auth href with query = %q, want both parameters", got)
	}

	// An unparsable or out-of-shell base passes through untouched: the
	// warning still renders its link instead of inventing a destination.
	if got := reauthResumeHref(testView(PageHome), "https://evil.example/phish"); got != "https://evil.example/phish" {
		t.Fatalf("out-of-shell base rewritten to %q", got)
	}
	if got := reauthResumeHref(testView(PageHome), "://bad"); got != "://bad" {
		t.Fatalf("unparsable base rewritten to %q", got)
	}
}

// Golden: resume composition for a fixed view is deterministic.
func TestTodo_WEB_053_Golden(t *testing.T) {
	view := testView(PageHistory)
	first := reauthResumeHref(view, "/workspace/app/settings")
	second := reauthResumeHref(view, "/workspace/app/settings")
	if first == "" || first != second {
		t.Fatalf("resume hrefs not deterministic: %q vs %q", first, second)
	}
	if !strings.HasPrefix(first, "/workspace/app/settings?") || !strings.Contains(first, "resume=") {
		t.Fatalf("resume href = %q, want destination plus resume target", first)
	}
}

// Browser: every projected warning links back into the shell.
func TestTodo_WEB_053_Browser(t *testing.T) {
	for _, page := range []PageID{PageHistory, PagePerson, PageRoles} {
		roles := []string{"manager"}
		if page == PageRoles {
			roles = []string{RoleHCMAdmin}
		}
		view := ApplyRoleVisibility(testView(page), roles)
		view.SessionWarning = &SessionWarningProps{Detail: "Ending.", ReauthHref: "/workspace/app/settings"}
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
			t.Fatalf("page %s renders no warning", page)
		}
		reauth := findFirst(banner, "a")
		if reauth == nil {
			t.Fatalf("page %s warning has no re-auth link", page)
		}
		parsed, err := url.Parse(xhtmlAttr(reauth, "href"))
		if err != nil || parsed.IsAbs() || parsed.Host != "" {
			t.Fatalf("page %s re-auth link escapes the shell", page)
		}
		resume, err := url.Parse(parsed.Query().Get("resume"))
		if err != nil || resume.Path != pageHref(page) {
			t.Fatalf("page %s resume target = %q, want %q", page, parsed.Query().Get("resume"), pageHref(page))
		}
	}
}

// Conformance: resume never escapes the shell and never invents state.
func TestTodo_WEB_053_Conformance(t *testing.T) {
	for _, definition := range PageDefinitions() {
		roles := []string{"manager"}
		if !PageVisible(definition.ID, roles) {
			roles = []string{RoleHCMAdmin}
		}
		view := ApplyRoleVisibility(testView(definition.ID), roles)
		got := reauthResumeHref(view, "/workspace/app/settings")
		parsed, err := url.Parse(got)
		if err != nil || parsed.IsAbs() || parsed.Host != "" {
			t.Fatalf("page %s resume href escapes the shell: %q", definition.ID, got)
		}
		resume, err := url.Parse(parsed.Query().Get("resume"))
		if err != nil {
			t.Fatalf("page %s resume target unparsable: %q", definition.ID, got)
		}
		if _, ok := LookupRoute(resume.Path); !ok {
			t.Fatalf("page %s resume path leaves the registry: %q", definition.ID, resume.Path)
		}
	}
}
