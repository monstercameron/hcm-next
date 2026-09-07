package bootstrap

import (
	"net/http/httptest"
	"testing"
)

func TestHealthEndpointsDistinguishProcessLifeFromAdmissionReadiness(t *testing.T) {
	h := NewHealth()
	for _, test := range []struct {
		name string
		path string
		code int
	}{
		{name: "starting is live", path: "/healthz", code: 200},
		{name: "starting is not ready", path: "/readyz", code: 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			h.EndpointHandler().ServeHTTP(recorder, httptest.NewRequest("GET", test.path, nil))
			if recorder.Code != test.code {
				t.Fatalf("%s status = %d, want %d", test.path, recorder.Code, test.code)
			}
			if body := recorder.Body.String(); body != "{\"ok\":true}\n" && body != "{\"ok\":false}\n" {
				t.Fatalf("%s body = %q, want the minimal probe shape", test.path, body)
			}
		})
	}

	if err := h.Set(StateReady); err != nil {
		t.Fatalf("Set READY: %v", err)
	}
	ready := httptest.NewRecorder()
	h.EndpointHandler().ServeHTTP(ready, httptest.NewRequest("GET", "/readyz", nil))
	if ready.Code != 200 || ready.Body.String() != "{\"ok\":true}\n" {
		t.Fatalf("readyz after startup = %d %q, want 200 and minimal ready body", ready.Code, ready.Body.String())
	}

	if err := h.Set(StateDraining); err != nil {
		t.Fatalf("Set DRAINING: %v", err)
	}
	live := httptest.NewRecorder()
	h.EndpointHandler().ServeHTTP(live, httptest.NewRequest("GET", "/healthz", nil))
	if live.Code != 200 {
		t.Fatalf("healthz while draining = %d, want 200", live.Code)
	}
	ready = httptest.NewRecorder()
	h.EndpointHandler().ServeHTTP(ready, httptest.NewRequest("GET", "/readyz", nil))
	if ready.Code != 503 {
		t.Fatalf("readyz while draining = %d, want 503", ready.Code)
	}
}
