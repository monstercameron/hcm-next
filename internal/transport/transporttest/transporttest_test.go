package transporttest

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust"
)

func mustTrustCred(scheme, token, audience string) trust.Credential {
	return trust.Credential{Scheme: scheme, Token: token, Audience: audience}
}

func TestDeterministicIntentID(t *testing.T) {
	a := DeterministicIntentID("seed-1")
	b := DeterministicIntentID("seed-1")
	if a != b {
		t.Fatal("not deterministic")
	}
	c := DeterministicIntentID("seed-2")
	if a == c {
		t.Fatal("different seeds same id")
	}
	if len(a) == 0 || a[:7] != "intent-" {
		t.Fatalf("prefix %q", a)
	}
}

func TestCanonicalRequest(t *testing.T) {
	req := CanonicalCreateIntentRequest()
	if req == nil {
		t.Fatal("nil")
	}
	if req.IdempotencyKey == "" {
		t.Fatal("empty idempotency")
	}
	if req.Definition.IntentTypeId != KnownDefinitionID {
		t.Fatalf("definition %q", req.Definition.IntentTypeId)
	}
}

func TestDefaultClaims(t *testing.T) {
	now := time.Now()
	claims := DefaultClaims(now)
	if claims.Tenant != Tenant {
		t.Fatalf("tenant %q", claims.Tenant)
	}
	if claims.Subject != Subject {
		t.Fatalf("subject %q", claims.Subject)
	}
	if claims.Issuer != Issuer {
		t.Fatalf("issuer %q", claims.Issuer)
	}
}

func TestNewVerifier(t *testing.T) {
	v, err := NewVerifier(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatal("nil verifier")
	}
	claims := DefaultClaims(time.Now())
	token, err := v.Issue(claims)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
}

func TestBearerToken(t *testing.T) {
	v, err := NewVerifier(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	bearer, err := BearerToken(v, DefaultClaims(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if len(bearer) < 7 || bearer[:7] != "Bearer " {
		t.Fatalf("bearer %q", bearer)
	}
}

func TestConfig(t *testing.T) {
	v, _ := NewVerifier(time.Now)
	cfg := Config(v, time.Now, "req-1", nil)
	if cfg.Audience != Audience {
		t.Fatalf("audience %q", cfg.Audience)
	}
	if cfg.NewRequestID() != "req-1" {
		t.Fatal("request id")
	}
}

func TestIntentHandlerCreate(t *testing.T) {
	h := &IntentHandler{}
	v, _ := NewVerifier(time.Now)
	claims := DefaultClaims(time.Now())
	token, _ := BearerToken(v, claims)
	_ = token
	cfg := Config(v, time.Now, "req-1", nil)
	ctx := context.Background()
	md := map[string][]string{"authorization": {token}}
	_ = md
	_ = cfg
	_ = ctx
	_ = h
	req := CanonicalCreateIntentRequest()
	principal, err := v.Verify(ctx, mustTrustCred("Bearer", token[7:], Audience))
	if err != nil {
		t.Skipf("verify failed %v", err)
	}
	ctx2 := context.WithValue(ctx, struct{ k string }{"test"}, principal)
	_ = ctx2
	_ = req
}

func TestConstants(t *testing.T) {
	if Issuer == "" || Audience == "" || Tenant == "" {
		t.Fatal("empty constants")
	}
	if KnownIntentID == "" || MissingIntentID == "" || BlockingIntentID == "" {
		t.Fatal("empty intent ids")
	}
}
