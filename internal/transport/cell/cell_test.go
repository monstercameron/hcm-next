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
