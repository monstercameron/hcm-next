package workspace_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestWorkspaceBrowserLoginIsOffByDefaultAndWorksWhenEnabled is the
// conformance test for the dev-only pasted-token sign-in flow: the route does
// not exist unless an operator turns it on, a bad credential never renders as
// a working session, and a good one lets the workspace's own cookie stand in
// for the Authorization header every other route requires.
func TestWorkspaceBrowserLoginIsOffByDefaultAndWorksWhenEnabled(t *testing.T) {
	t.Parallel()

	t.Run("off by default", func(t *testing.T) {
		off := newCell(t, true)
		// Authenticated, so a 404 here proves the route is absent, not that
		// admission refused an anonymous caller the way it would for any
		// other unknown workspace address.
		res := off.get(workspace.PathLogin, compAdmin.name)
		if res.Status != http.StatusNotFound {
			t.Fatalf("GET %s (login disabled) = %d, want 404\n%s", workspace.PathLogin, res.Status, res.Body)
		}
	})

	on := newCell(t, true, true)
	rawToken := strings.TrimPrefix(on.tokens[compAdmin.name], "Bearer ")
	if rawToken == on.tokens[compAdmin.name] || rawToken == "" {
		t.Fatalf("could not derive a raw bearer token from %q", on.tokens[compAdmin.name])
	}

	t.Run("the form renders without a credential", func(t *testing.T) {
		res := on.get(workspace.PathLogin, "")
		if res.Status != http.StatusOK {
			t.Fatalf("GET %s (anonymous) = %d, want 200\n%s", workspace.PathLogin, res.Status, res.Body)
		}
		if !strings.Contains(res.Body, `name="token"`) {
			t.Error("the rendered sign-in form carries no token field")
		}
	})

	t.Run("a bad credential answers 401 without echoing it", func(t *testing.T) {
		const bogus = "this-credential-does-not-verify-anywhere"
		res := on.post(workspace.PathLogin, "", map[string]string{"token": bogus})
		if res.Status != http.StatusUnauthorized {
			t.Fatalf("POST %s (bad token) = %d, want 401\n%s", workspace.PathLogin, res.Status, res.Body)
		}
		if strings.Contains(res.Body, bogus) {
			t.Error("the refusal page echoes the rejected credential")
		}
	})

	var sessionCookie *http.Cookie
	t.Run("a good credential sets the session cookie and redirects", func(t *testing.T) {
		res := on.post(workspace.PathLogin, "", map[string]string{"token": rawToken})
		if res.Status != http.StatusSeeOther {
			t.Fatalf("POST %s (good token) = %d, want 303\n%s", workspace.PathLogin, res.Status, res.Body)
		}
		if loc := res.Header.Get("Location"); loc != workspace.PathProductHome {
			t.Errorf("POST %s redirected to %q, want %q", workspace.PathLogin, loc, workspace.PathProductHome)
		}
		for _, c := range res.Cookies {
			if c.Name == "hcmnext_session" {
				sessionCookie = c
			}
		}
		if sessionCookie == nil {
			t.Fatal("a good login set no hcmnext_session cookie")
		}
		if sessionCookie.Value != rawToken {
			t.Errorf("hcmnext_session = %q, want the presented token %q", sessionCookie.Value, rawToken)
		}
		if !sessionCookie.HttpOnly {
			t.Error("hcmnext_session is not HttpOnly")
		}
		if sessionCookie.SameSite != http.SameSiteStrictMode {
			t.Errorf("hcmnext_session SameSite = %v, want Strict", sessionCookie.SameSite)
		}
	})

	t.Run("the session cookie is accepted as the bearer source", func(t *testing.T) {
		if sessionCookie == nil {
			t.Skip("no session cookie from the prior subtest")
		}
		res := on.get(promotionURL, "", sessionCookie)
		if res.Status != http.StatusOK {
			t.Fatalf("GET %s with session cookie only = %d, want 200\n%s", promotionURL, res.Status, res.Body)
		}
		if !strings.Contains(res.Body, "Omar") {
			t.Error("the workspace did not render the fixture worker for the cookie-authenticated session")
		}
	})

	t.Run("logout clears the session cookie", func(t *testing.T) {
		if sessionCookie == nil {
			t.Skip("no session cookie from the prior subtest")
		}
		res := on.get(workspace.PathLogout, "", sessionCookie)
		if res.Status != http.StatusSeeOther {
			t.Fatalf("GET %s = %d, want 303\n%s", workspace.PathLogout, res.Status, res.Body)
		}
		var cleared *http.Cookie
		for _, c := range res.Cookies {
			if c.Name == "hcmnext_session" {
				cleared = c
			}
		}
		if cleared == nil {
			t.Fatal("logout set no hcmnext_session cookie at all")
		}
		if cleared.Value != "" {
			t.Errorf("logout's cleared cookie carries value %q, want empty", cleared.Value)
		}
		if cleared.MaxAge >= 0 {
			t.Errorf("logout's cleared cookie MaxAge = %d, want negative (delete)", cleared.MaxAge)
		}
	})
}
