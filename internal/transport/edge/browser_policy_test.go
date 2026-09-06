package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTodo_EDGE_004(t *testing.T) {
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), BrowserPolicyOptions{
		AllowedOrigins: []string{"https://trusted.example"},
		AllowedHosts:   []string{"trusted.example"},
	})

	for _, tc := range []struct {
		name   string
		origin string
		host   string
	}{
		{name: "cross-origin", origin: "https://evil.example", host: "trusted.example"},
		{name: "forged-host", origin: "https://trusted.example", host: "evil.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/promotion/simulate", nil)
			req.Host = tc.host
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			policy.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "edge.browser_") {
				t.Fatalf("typed refusal body = %q", rec.Body.String())
			}
		})
	}
}

func FuzzTodo_EDGE_004(f *testing.F) {
	f.Add("https://trusted.example", "trusted.example")
	f.Add("null", "trusted.example")
	f.Fuzz(func(t *testing.T, origin, host string) {
		policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}), BrowserPolicyOptions{
			AllowedOrigins: []string{"https://trusted.example"},
			AllowedHosts:   []string{"trusted.example"},
		})
		req := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/promotion/simulate", nil)
		req.Host = host
		req.Header.Set("Origin", origin)
		policy.ServeHTTP(httptest.NewRecorder(), req)
	})
}

func TestTodo_EDGE_004_Integration(t *testing.T) {
	var called bool
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}), BrowserPolicyOptions{
		AllowedOrigins: []string{"https://trusted.example"},
		AllowedHosts:   []string{"trusted.example"},
	})

	get := httptest.NewRequest(http.MethodGet, "https://trusted.example/workspace/promotion?worker=w-1", nil)
	get.Host = "trusted.example"
	getRec := httptest.NewRecorder()
	policy.ServeHTTP(getRec, get)
	cookies := getRec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != BrowserCSRFCookieName {
		t.Fatalf("GET cookies = %v, want one browser CSRF cookie", cookies)
	}
	if !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie security attributes = %+v", cookies[0])
	}

	post := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/promotion/simulate", nil)
	post.Host = "trusted.example"
	post.Header.Set("Origin", "https://trusted.example")
	post.AddCookie(cookies[0])
	postRec := httptest.NewRecorder()
	policy.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusNoContent || !called {
		t.Fatalf("same-origin journey POST = %d, called=%v; want 204 and called", postRec.Code, called)
	}

	login := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/login", nil)
	login.Host = "trusted.example"
	login.Header.Set("Origin", "https://trusted.example")
	login.AddCookie(cookies[0])
	loginRec := httptest.NewRecorder()
	policy.ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusNoContent {
		t.Fatalf("same-origin dev-login POST = %d, want 204", loginRec.Code)
	}
}

func TestTodo_EDGE_004_Security(t *testing.T) {
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/steal", http.StatusSeeOther)
	}), BrowserPolicyOptions{})
	req := httptest.NewRequest(http.MethodGet, "https://trusted.example/", nil)
	rec := httptest.NewRecorder()
	policy.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign redirect status = %d, want 403", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "" {
		t.Fatalf("foreign redirect leaked Location %q", got)
	}
}

func TestTodo_EDGE_004_Browser(t *testing.T) {
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/workspace/app/home", http.StatusSeeOther)
	}), BrowserPolicyOptions{})
	req := httptest.NewRequest(http.MethodGet, "https://trusted.example/", nil)
	rec := httptest.NewRecorder()
	policy.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/workspace/app/home" {
		t.Fatalf("same-origin relative redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestTodo_EDGE_004_Recovery(t *testing.T) {
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		w.WriteHeader(http.StatusInternalServerError)
	}), BrowserPolicyOptions{})
	req := httptest.NewRequest(http.MethodGet, "https://trusted.example/workspace/promotion", nil)
	rec := httptest.NewRecorder()
	policy.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("duplicate WriteHeader changed status to %d", rec.Code)
	}
}
