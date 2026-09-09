package sandbox

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/transport"
)

var emulatorNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func emulatorScript() []EmulatorStep {
	return []EmulatorStep{
		{Scenario: EmulatorPage, Body: []byte(`{"page":1}`)},
		{Scenario: EmulatorPartial, Body: []byte(`{"page":2,"partial":true`)},
		{Scenario: EmulatorRateLimited, Status: http.StatusTooManyRequests, Body: []byte(`rate limited`)},
		{Scenario: EmulatorTimeoutAfterSend},
		{Scenario: EmulatorSchemaDrift, Body: []byte(`{"schema":"v2"}`), SchemaVersion: "v2"},
		{Scenario: EmulatorObservation, Body: []byte(`{"observed":true`)},
	}
}

func newEmulator(t *testing.T) *Emulator {
	t.Helper()
	e, err := NewEmulatorWithClock(emulatorScript(), func() time.Time { return emulatorNow })
	if err != nil {
		t.Fatalf("NewEmulator: %v", err)
	}
	return e
}

func emulatorClient(e *Emulator) transport.REST {
	return transport.NewREST(transport.Client{
		Limits: transport.Limits{MaxRequestBytes: 128, MaxResponseBytes: 128, Timeout: time.Second},
		Trust:  transport.DestinationTrust{Schemes: []string{"https"}, Hosts: []string{"sandbox.example.test"}},
		HTTP:   e,
	})
}

func emulatorRequest(t *testing.T, client transport.REST) (transport.Response, error) {
	t.Helper()
	return client.Call(context.Background(), transport.Request{Method: http.MethodGet, URL: "https://sandbox.example.test/connector"})
}

func TestTodo_CONN_RT_006(t *testing.T) {
	e := newEmulator(t)
	client := emulatorClient(e)
	page, err := emulatorRequest(t, client)
	if err != nil || string(page.Body) != `{"page":1}` {
		t.Fatalf("first scripted page=%q err=%v", page.Body, err)
	}
	partial, err := emulatorRequest(t, client)
	if err != nil || partial.Headers.Get("X-Sandbox-Partial") != "true" {
		t.Fatalf("partial response=%+v err=%v", partial, err)
	}
	if _, err := emulatorRequest(t, client); err == nil {
		t.Fatal("429 script step was accepted")
	}
	if _, err := emulatorRequest(t, client); !errors.Is(err, ErrEmulatorTimeoutAfterSend) {
		t.Fatalf("timeout-after-send err=%v", err)
	}
	if _, err := emulatorRequest(t, client); err != nil {
		t.Fatalf("schema drift response should remain transport-readable: %v", err)
	}
	if _, err := emulatorRequest(t, client); err != nil {
		t.Fatalf("observation response: %v", err)
	}
	if len(e.Calls()) != 6 {
		t.Fatalf("calls=%d, want 6", len(e.Calls()))
	}
}

func TestTodo_CONN_RT_006_Golden(t *testing.T) {
	a := newEmulator(t)
	b := newEmulator(t)
	if a.StepDigest() == "" || a.StepDigest() != b.StepDigest() {
		t.Fatalf("script digest is not deterministic: %q vs %q", a.StepDigest(), b.StepDigest())
	}
	first := a.Reset()
	second := a.Reset()
	if first.StepDigest != second.StepDigest || first.Generation != 1 || second.Generation != 2 {
		t.Fatalf("reset evidence drifted: first=%+v second=%+v", first, second)
	}
}

func TestTodo_CONN_RT_006_Integration(t *testing.T) {
	e := newEmulator(t)
	client := emulatorClient(e)
	for i, want := range []string{"{\"page\":1}", "{\"page\":2,\"partial\":true"} {
		got, err := emulatorRequest(t, client)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if string(got.Body) != want {
			t.Fatalf("step %d body=%q want=%q", i, got.Body, want)
		}
	}
	if e.Calls()[1].Scenario != EmulatorPartial {
		t.Fatalf("second call scenario=%s", e.Calls()[1].Scenario)
	}
}

func FuzzTodo_CONN_RT_006(f *testing.F) {
	f.Add("fixture")
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 1<<20 {
			t.Skip()
		}
		e, err := NewEmulator([]EmulatorStep{{Scenario: EmulatorPage, Body: []byte(body)}})
		if err != nil {
			t.Fatal(err)
		}
		client := emulatorClient(e)
		_, _ = emulatorRequest(t, client)
		_ = e.Reset()
	})
}

func TestTodo_CONN_RT_006_Fault(t *testing.T) {
	e := newEmulator(t)
	client := emulatorClient(e)
	for range 3 {
		_, _ = emulatorRequest(t, client)
	}
	if _, err := emulatorRequest(t, client); err == nil {
		t.Fatal("timeout step did not return an error")
	}
	call := e.Calls()[3]
	if !call.Sent || call.Scenario != EmulatorTimeoutAfterSend {
		t.Fatalf("timeout call=%+v", call)
	}
	for range 3 {
		_, _ = emulatorRequest(t, client)
	}
	if _, err := emulatorRequest(t, client); !errors.Is(err, ErrEmulatorScriptExhausted) {
		t.Fatalf("exhausted script err=%v", err)
	}
}

func TestTodo_CONN_RT_006_Security(t *testing.T) {
	e := newEmulator(t)
	client := emulatorClient(e)
	_, _ = client.Call(context.Background(), transport.Request{URL: "https://sandbox.example.test/fixture?token=secret"})
	if strings.Contains(e.Calls()[0].URL, "secret") || strings.Contains(e.Calls()[0].URL, "/fixture") {
		t.Fatalf("call evidence disclosed request detail: %q", e.Calls()[0].URL)
	}
	_, err := client.Call(context.Background(), transport.Request{URL: "https://production.example.test/real"})
	var typed *transport.Error
	if !errors.As(err, &typed) || typed.Kind != transport.ErrDestination {
		t.Fatalf("untrusted destination err=%v", err)
	}
	if len(e.Calls()) != 1 {
		t.Fatal("untrusted destination reached emulator")
	}
	if strings.Contains(e.StepDigest(), "production") {
		t.Fatal("step digest contains an external destination")
	}
}
