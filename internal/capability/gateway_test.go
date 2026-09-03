package capability

import (
	"context"
	"testing"
	"time"
)

type stubSink struct {
	called int
}

func (s *stubSink) RecordInvocation(_ context.Context, evt InvocationEvidence) (string, error) {
	s.called++
	return "ev-1", nil
}

func TestGateway_NewGateway(t *testing.T) {
	r, _ := NewBootstrapRegistry()
	g := NewGateway(r, &stubSink{})
	if g == nil {
		t.Fatal("nil gateway")
	}
}

func TestGateway_WithClock(t *testing.T) {
	r := NewRegistry()
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	g := NewGateway(r, &stubSink{}, WithClock(func() time.Time { return fixed }))
	if g.now().UnixNano() != fixed.UnixNano() {
		t.Fatal("clock not set")
	}
}

func TestGateway_InvokeUnknown(t *testing.T) {
	r := NewRegistry()
	sink := &stubSink{}
	g := NewGateway(r, sink)
	_, err := g.Invoke(context.Background(), InvokeRequest{
		Capability:    Key{ID: "unknown.cap", Version: 1},
		Authorization: Authorization{Decision: Allow, Scopes: []string{"*"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	ge, ok := err.(*GatewayError)
	if !ok {
		t.Fatalf("not gateway error %T", err)
	}
	if ge.Code != CodeUnknownCapability {
		t.Fatalf("code %s", ge.Code)
	}
}

func TestGateway_InvokeDeniedAuth(t *testing.T) {
	r, _ := NewBootstrapRegistry()
	sink := &stubSink{}
	g := NewGateway(r, sink)
	list := r.List()
	if len(list) == 0 {
		t.Fatal("empty registry")
	}
	key := list[0].Definition.Key()
	_, err := g.Invoke(context.Background(), InvokeRequest{
		Capability:    key,
		Authorization: Authorization{Decision: Deny, Reason: "test deny"},
	})
	if err == nil {
		t.Fatal("expected deny")
	}
	ge, _ := err.(*GatewayError)
	if ge.Code != CodeUnauthorized {
		t.Fatalf("code %s", ge.Code)
	}
}

func TestGateway_InvokeAllowed(t *testing.T) {
	r, _ := NewBootstrapRegistry()
	sink := &stubSink{}
	g := NewGateway(r, sink)
	rec := r.List()[0]
	key := rec.Definition.Key()
	res, err := g.Invoke(context.Background(), InvokeRequest{
		Capability:    key,
		Payload:       map[string]string{"hello": "world"},
		Authorization: Authorization{Decision: Allow, Scopes: []string{"*"}, SubjectRef: "user:1"},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if res.EvidenceID == "" {
		t.Fatal("empty evidence")
	}
	if m, ok := res.Response.(map[string]any); !ok || m["capability"] != key.ID {
		t.Fatalf("response %+v", res.Response)
	}
}
