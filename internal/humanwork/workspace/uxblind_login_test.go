package workspace

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestUXBLIND001PersonaLoginUsesServerOwnedCredential(t *testing.T) {
	h, token := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{
		"admin": {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Token: token},
	}
	form := url.Values{paramLoginPersona: {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != PathProductHome {
		t.Fatalf("persona login = %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Fatal("persona credential was disclosed")
	}
}

func TestUXBLIND002LoginFailureOffersSafeRecovery(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Token: "wrong"}}
	form := url.Values{paramLoginPersona: {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"sign you in right now", "Try again", "workspace administrator", "Use a bearer credential"} {
		if !strings.Contains(body, want) {
			t.Errorf("login recovery missing %q", want)
		}
	}
	if strings.Contains(body, "wrong") || strings.Contains(body, "credential was not accepted") {
		t.Fatal("login failure exposed credential-specific diagnostics")
	}
}

func TestUXBLIND003LoginFailureKeepsPersonaActionsBeforeFeedback(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Token: "wrong"}}
	form := url.Values{paramLoginPersona: {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Index(body, `class="persona-grid"`) > strings.Index(body, `class="status-banner"`) {
		t.Fatal("failure feedback was rendered before the stable persona grid")
	}
	if !strings.Contains(body, `role="alert"`) {
		t.Fatal("login failure was not announced")
	}
}

func TestUXBLIND004LoginFailureOpensCredentialRecovery(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Token: "wrong"}}
	form := url.Values{paramLoginPersona: {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `<details class="advanced" id="credential-sign-in" open>`) || !strings.Contains(body, `href="#credential-sign-in"`) {
		t.Fatal("failed login did not expose a discoverable credential recovery path")
	}
}

func TestUXBLIND005PersonaCopyNamesUsefulTasks(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{
		"admin":                  {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Description: "governed workflow workspaces", Token: "token"},
		"hiring-manager":         {ID: "hiring-manager", Name: "Dominic Collins", Access: "Hiring manager", Token: "token"},
		"payroll-manager":        {ID: "payroll-manager", Name: "Thomas Baker", Access: "Payroll manager", Token: "token"},
		"individual-contributor": {ID: "individual-contributor", Name: "Samuel Rivera", Access: "Individual contributor", Token: "token"},
	}
	req := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathLogin, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"approve compensation changes", "Review hiring", "Review payroll", "View your employment details"} {
		if !strings.Contains(body, want) {
			t.Errorf("persona task copy missing %q", want)
		}
	}
	if strings.Contains(body, "governed workflow workspaces") {
		t.Fatal("implementation language remained in persona copy")
	}
}
