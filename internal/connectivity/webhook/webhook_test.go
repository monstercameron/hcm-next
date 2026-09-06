package webhook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"
)

func testEndpoint(t *testing.T) (Endpoint, Request, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	ep := Endpoint{ID: "ep-1", TenantID: "tenant-1", ConnectionID: "conn-1", Secret: []byte("secret"), AllowedSchemas: []string{"promotion.v1"}, MaxPayloadBytes: 1024, ReplayWindow: 5 * time.Minute}
	req := Request{EndpointID: ep.ID, TenantID: ep.TenantID, EventID: "evt-1", EventType: "promotion.updated", Schema: "promotion.v1", Timestamp: now, Payload: []byte(`{"worker":"w-1","version":2}`), Correlation: "corr-1"}
	req.Signature = Sign(ep.Secret, req)
	return ep, req, now
}

func TestTodo_INTG_018(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	r, err := s.Receive(req, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.SignatureState != "VALID" || r.PayloadRef == "" || r.PayloadHash == "" {
		t.Fatalf("invalid receipt: %#v", r)
	}
	if _, err := s.Replay(r.ID, ReplayApproval{Actor: "ops", Purpose: "recovery", Approved: true}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_INTG_018_Golden(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	r1, err := s.Receive(req, now)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Receive(req, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if r1.ID != r2.ID || Explain(r1) != Explain(r2) {
		t.Fatalf("duplicate changed identity: %#v %#v", r1, r2)
	}
}

func TestTodo_INTG_018_Integration(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		req.EventID = "evt-" + string(rune('1'+i))
		req.Signature = Sign(ep.Secret, req)
		if _, err := s.Receive(req, now); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_INTG_018_Fault(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	bad := req
	bad.Timestamp = now.Add(-time.Hour)
	bad.Signature = Sign(ep.Secret, bad)
	if _, err := s.Receive(bad, now); !errors.Is(err, ErrOutsideReplayWindow) {
		t.Fatalf("got %v", err)
	}
	bad = req
	bad.Payload = make([]byte, ep.MaxPayloadBytes+1)
	bad.Signature = Sign(ep.Secret, bad)
	if _, err := s.Receive(bad, now); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestTodo_INTG_018_Security(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	bad := req
	bad.TenantID = "tenant-other"
	bad.Signature = Sign(ep.Secret, bad)
	if _, err := s.Receive(bad, now); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("got %v", err)
	}
	if _, err := s.Receive(req, now); err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(digestText(req.EndpointID + ":" + req.EventID + "|" + hexPayloadHash(req.Payload)))
	if err != nil || r.PayloadRef == "" {
		t.Fatalf("metadata receipt was not retained: %#v err=%v", r, err)
	}
}

func hexPayloadHash(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func TestTodo_INTG_018_Recovery(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	r, err := s.Receive(req, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Replay(r.ID, ReplayApproval{Actor: "recovery", Purpose: "redelivery", Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.EventID != req.EventID || got.ReplayOf != r.ID || !bytes.Equal(got.Payload, req.Payload) {
		t.Fatalf("recovery changed event: %#v", got)
	}
}

func TestTodo_INTG_018_Mutation(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Receive(req, now); err != nil {
		t.Fatal(err)
	}
	changed := req
	changed.Payload = []byte("different")
	changed.Signature = Sign(ep.Secret, changed)
	if _, err := s.Receive(changed, now); !errors.Is(err, ErrDuplicateDifferent) {
		t.Fatalf("got %v", err)
	}
}

func TestTodo_INTG_018_Race(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan Receipt, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, err := s.Receive(req, now); results <- r; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	var id string
	for r := range results {
		if id == "" {
			id = r.ID
		}
		if r.ID != id {
			t.Fatal("concurrent receipt identity changed")
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzTodo_INTG_018(f *testing.F) {
	f.Add([]byte("payload"), "promotion.v1")
	f.Fuzz(func(t *testing.T, payload []byte, schema string) {
		ep, req, now := testEndpoint(t)
		req.Payload = payload
		req.Schema = schema
		req.Signature = Sign(ep.Secret, req)
		s := NewStore()
		if err := s.RegisterEndpoint(ep); err != nil {
			t.Fatal(err)
		}
		_, _ = s.Receive(req, now)
	})
}
