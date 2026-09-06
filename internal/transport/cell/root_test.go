package cell

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/hcm-next/internal/humanwork/workspace"
)

// The root route is exact: a browser at the origin is sent to the journey
// page with a 303, and nothing under the root is touched by it.
func TestRootRedirectSendsTheOriginToTheJourneyPage(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	rootRedirect(workspace.PathProductHome).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != workspace.PathProductHome {
		t.Fatalf("Location = %q, want %q", got, workspace.PathProductHome)
	}
}

// Mounted on a mux beside a catch-all, the pattern claims only "GET /": a
// POST to the root and any GET below it still reach the catch-all, which is
// what keeps every Connect procedure reachable.
func TestRootPatternDoesNotShadowTheCatchAll(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.Handle(RootPattern, rootRedirect(workspace.PathProductHome))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })

	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/", http.StatusSeeOther},
		{http.MethodPost, "/", http.StatusTeapot},
		{http.MethodGet, "/hcmnext.intents.v1.IntentService/GetIntent", http.StatusTeapot},
		{http.MethodPost, "/hcmnext.intents.v1.IntentService/GetIntent", http.StatusTeapot},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Fatalf("%s %s: status = %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
}
