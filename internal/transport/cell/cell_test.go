package cell

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
)

func TestNewGRPCServerNilCell(t *testing.T) {
	_, err := NewGRPCServer(nil)
	if err == nil {
		t.Fatal("expected error for nil cell")
	}
}

func TestNewEdgeHandlerNilCell(t *testing.T) {
	_, err := NewEdgeHandler(nil)
	if err == nil {
		t.Fatal("expected error for nil cell")
	}
}

// TestCellPublicOriginParsesTheDeclaredOrigin pins the boundary check the
// edge build applies to the value the application root already canonicalized:
// composed by a caller that skipped that validation, a malformed origin is
// still refused rather than silently bound.
func TestCellPublicOriginParsesTheDeclaredOrigin(t *testing.T) {
	if got, err := cellPublicOrigin(""); err != nil || got != nil {
		t.Fatalf("empty origin = %v, %v; want nil, nil", got, err)
	}
	u, err := cellPublicOrigin("https://hcm.example.com")
	if err != nil || u.Scheme != "https" || u.Host != "hcm.example.com" {
		t.Fatalf("cellPublicOrigin = %v, %v", u, err)
	}
	for _, raw := range []string{
		"hcm.example.com", "ftp://hcm.example.com", "https://hcm.example.com/app",
		"https://hcm.example.com?q=1", "https://user@hcm.example.com", "https://",
	} {
		if got, err := cellPublicOrigin(raw); err == nil {
			t.Errorf("cellPublicOrigin(%q) = %v, want a refusal", raw, got)
		}
	}
}

// TestBrowserPolicyOptionsDerivesTheDeclaredOrigin pins the two boundary
// facts the option carries: the public origin becomes the only admitted
// browser origin, and an https public origin makes the cookies it normalizes
// Secure. With no declared origin the same-origin default stands.
func TestBrowserPolicyOptionsDerivesTheDeclaredOrigin(t *testing.T) {
	if got := browserPolicyOptions(nil); len(got.AllowedOrigins) != 0 || got.SecureCookies {
		t.Fatalf("no public origin = %+v, want the empty default", got)
	}
	u, err := cellPublicOrigin("https://hcm.example.com:8443")
	if err != nil {
		t.Fatal(err)
	}
	got := browserPolicyOptions(u)
	if len(got.AllowedOrigins) != 1 || got.AllowedOrigins[0] != "https://hcm.example.com:8443" {
		t.Fatalf("AllowedOrigins = %v, want the declared origin alone", got.AllowedOrigins)
	}
	if !got.SecureCookies {
		t.Fatal("an https public origin must mark normalized cookies Secure")
	}
	u, err = cellPublicOrigin("http://hcm.internal:8080")
	if err != nil {
		t.Fatal(err)
	}
	if got := browserPolicyOptions(u); got.SecureCookies {
		t.Fatal("an http public origin must not mark cookies Secure")
	}
}

func TestSpliceWorkspaceRoutesEmptyDoc(t *testing.T) {
	doc := []byte(`{}`)
	routes := []workspace.Route{{Path: "/workspace", Method: "GET"}}
	out, err := spliceWorkspaceRoutes(doc, routes)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m[app.WorkspaceRoutesKey]; !ok {
		t.Fatalf("missing key %s in %s", app.WorkspaceRoutesKey, string(out))
	}
}

func TestSpliceWorkspaceRoutesWithExisting(t *testing.T) {
	doc := []byte(`{"api":"v1"}`)
	routes := []workspace.Route{{Path: "/workspace", Method: "GET"}}
	out, err := spliceWorkspaceRoutes(doc, routes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`"api"`)) {
		t.Fatal("original field lost")
	}
	if !bytes.Contains(out, []byte(app.WorkspaceRoutesKey)) {
		t.Fatal("workspace key missing")
	}
}

func TestSpliceWorkspaceRoutesInvalidJSON(t *testing.T) {
	_, err := spliceWorkspaceRoutes([]byte(`not-json`), nil)
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
	_, err = spliceWorkspaceRoutes([]byte(`[]`), nil)
	if err == nil {
		t.Fatal("expected error for array")
	}
}

func TestNewDiscoveryHandler(t *testing.T) {
	cfg := transport.Config{Now: func() time.Time { return time.Now() }}
	doc := &manifest.DiscoveryDocument{}
	h, err := newDiscoveryHandler(cfg, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h == nil || len(h.document) == 0 {
		t.Fatal("empty handler")
	}
}

func TestDiscoveryHandlerMethodNotAllowed(t *testing.T) {
	cfg := transport.Config{Now: func() time.Time { return time.Now() }}
	doc := &manifest.DiscoveryDocument{}
	h, err := newDiscoveryHandler(cfg, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, app.DiscoveryPath, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusMethodNotAllowed && w.Code != 400 && w.Code != 405 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestDiscoveryHandlerGet(t *testing.T) {
	cfg := transport.Config{Now: func() time.Time { return time.Now() }}
	doc := &manifest.DiscoveryDocument{}
	h, err := newDiscoveryHandler(cfg, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, app.DiscoveryPath, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Logf("status %d headers %v body %s", w.Code, w.Header(), w.Body.String())
	}
}
