package workspace

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDevPersonaLoginKeepsCredentialServerSide(t *testing.T) {
	h, token := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Jane Doe", Access: "HCM administrator", Description: "All areas.", Token: token}}
	form := url.Values{paramLoginPersona: {"admin"}}
	request := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != PathProductHome {
		t.Fatalf("persona login = %d location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if strings.Contains(recorder.Body.String(), token) {
		t.Fatal("persona credential entered the response body")
	}
	var found bool
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == loginSessionCookie {
			found = cookie.HttpOnly && cookie.SameSite == http.SameSiteStrictMode && cookie.Value == token
		}
	}
	if !found {
		t.Fatal("persona login did not set the hardened session cookie")
	}
}

func TestDevPersonaLoginPageRendersIdentityNotCredential(t *testing.T) {
	h, token := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Jane Doe", Access: "HCM administrator", Description: "All areas.", Token: token}}
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathLogin, nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	for _, want := range []string{"Choose a workspace persona", "Jane Doe", "HCM administrator"} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %q", want)
		}
	}
	if !strings.Contains(body, `<button type="submit" name="persona" value="admin">Continue as Jane Doe</button>`) {
		t.Fatal("persona choice is not carried by its submit button")
	}
	if strings.Contains(body, `type="hidden" name="persona"`) {
		t.Fatal("persona login still depends on a hidden selector")
	}
	if strings.Contains(body, token) {
		t.Fatal("login page disclosed a persona credential")
	}
	if got := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(got, "style-src '"+sha256Source(loginStylesheet())+"'") {
		t.Fatalf("login stylesheet is not CSP-pinned: %q", got)
	}
}
