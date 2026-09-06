package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A browser embedded under a foreign top-level document reports every
// navigation as cross-site through Fetch Metadata even when the page, its
// Origin header and its token cookie are all this origin's own. Fetch
// Metadata is therefore advisory: a cross-site report is refused only when
// no Origin accompanies it, because a matching Origin cannot be forged by
// another site and the SameSite token check still applies.
func TestBrowserPolicyTreatsFetchMetadataAsAdvisoryWhenOriginMatches(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /workspace/login", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := BrowserPolicy(mux, BrowserPolicyOptions{})

	// Take the token cookie the way a browser does: from a GET under the
	// workspace prefix.
	issued := httptest.NewRecorder()
	handler.ServeHTTP(issued, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/workspace/login", nil))
	var token *http.Cookie
	for _, c := range issued.Result().Cookies() {
		if c.Name == BrowserCSRFCookieName {
			token = c
		}
	}
	if token == nil {
		t.Fatal("the workspace GET issued no browser token cookie")
	}

	post := func(origin, fetchSite string, withCookie bool) int {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/workspace/login", strings.NewReader("token=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if fetchSite != "" {
			req.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		if withCookie {
			req.AddCookie(token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := post("http://127.0.0.1:8080", "cross-site", true); got != http.StatusNoContent {
		t.Fatalf("embedded same-origin form POST: status %d, want %d", got, http.StatusNoContent)
	}
	if got := post("", "cross-site", true); got != http.StatusForbidden {
		t.Fatalf("cross-site report with no Origin: status %d, want %d", got, http.StatusForbidden)
	}
	if got := post("http://evil.example", "cross-site", true); got != http.StatusForbidden {
		t.Fatalf("foreign Origin: status %d, want %d", got, http.StatusForbidden)
	}
	if got := post("http://127.0.0.1:8080", "same-origin", false); got != http.StatusForbidden {
		t.Fatalf("matching Origin without the token cookie: status %d, want %d", got, http.StatusForbidden)
	}
}
