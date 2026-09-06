package configbundle

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

func cp006State(t *testing.T) (SignedFeatureState, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	var seed [ed25519.SeedSize]byte
	for i := range seed {
		seed[i] = byte(81 + i)
	}
	private := ed25519.NewKeyFromSeed(seed[:])
	now := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)
	state := FeatureState{
		TenantID: "cp006-tenant", OrgID: "org-a", Version: 9,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		Features: map[string]FeatureRule{
			"promotion.v2": {Version: 3, Enabled: true, RolloutPercent: 100, Salt: "promotion-v2", Variants: []FeatureVariant{{Name: "control", Weight: 30}, {Name: "new-flow", Weight: 70}}},
			"disabled":     {Version: 1, Enabled: false, RolloutPercent: 100},
		},
	}
	signed, err := SignFeatureState(state, "cp006-signer", "v1", private)
	if err != nil {
		t.Fatal(err)
	}
	return signed, private.Public().(ed25519.PublicKey), private
}

func cp006Request(feature string) FeatureRequest {
	return FeatureRequest{TenantID: "cp006-tenant", OrgID: "org-a", UserID: "user-17", Feature: feature}
}

// TestTodo_CP_006 proves signed local evaluation, stable bucketing, variants,
// and the kill-switch precedence rule.
func TestTodo_CP_006(t *testing.T) {
	signed, publicKey, private := cp006State(t)
	now := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)
	evaluator := FeatureEvaluator{PublicKey: publicKey, Now: func() time.Time { return now }}
	first, err := evaluator.Evaluate(signed, cp006Request("promotion.v2"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := evaluator.Evaluate(signed, cp006Request("promotion.v2"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Decision != FeatureAllow || first.Variant == "" || first != second || first.StateVersion != 9 || first.RuleVersion != 3 {
		t.Fatalf("decision=%+v repeat=%+v", first, second)
	}
	disabled, err := evaluator.Evaluate(signed, cp006Request("disabled"))
	if err != nil || disabled.Decision != FeatureDeny || disabled.Reason != "FEATURE_DISABLED" {
		t.Fatalf("disabled decision=%+v err=%v", disabled, err)
	}
	killedState := signed.State
	killedState.KillSwitches = map[string]bool{"promotion.v2": true}
	killed, err := SignFeatureState(killedState, "cp006-signer", "v1", private)
	if err != nil {
		t.Fatal(err)
	}
	killedDecision, err := evaluator.Evaluate(killed, cp006Request("promotion.v2"))
	if err != nil || killedDecision.Decision != FeatureDeny || killedDecision.Reason != "KILL_SWITCH" {
		t.Fatalf("kill-switch decision=%+v err=%v", killedDecision, err)
	}
}

func TestTodo_CP_006_Golden(t *testing.T) {
	signed, publicKey, _ := cp006State(t)
	now := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)
	decision, err := EvaluateFeatureAt(signed, publicKey, cp006Request("promotion.v2"), now)
	if err != nil {
		t.Fatal(err)
	}
	if decision != (FeatureDecision{Decision: FeatureAllow, Variant: "control", StateVersion: 9, RuleVersion: 3, Reason: "ROLLOUT_INCLUDED"}) {
		t.Fatalf("decision=%+v, want stable golden decision", decision)
	}
	if signed.Digest == "" || signed.Signature.Value == "" {
		t.Fatal("signed feature state is missing digest or signature")
	}
}

func TestTodo_CP_006_Security(t *testing.T) {
	signed, publicKey, private := cp006State(t)
	now := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)
	if _, err := EvaluateFeatureAt(signed, publicKey, FeatureRequest{OrgID: "org-a", UserID: "user-17", Feature: "promotion.v2"}, now); !errors.Is(err, ErrFeatureScopeRequired) {
		t.Fatalf("missing tenant error=%v", err)
	}
	wrongScope := cp006Request("promotion.v2")
	wrongScope.OrgID = "other-org"
	if _, err := EvaluateFeatureAt(signed, publicKey, wrongScope, now); !errors.Is(err, ErrFeatureScopeMismatch) {
		t.Fatalf("wrong org error=%v", err)
	}
	expired := signed
	expired.State.ExpiresAt = now.Add(-time.Minute)
	if _, err := EvaluateFeatureAt(expired, publicKey, cp006Request("promotion.v2"), now); !errors.Is(err, ErrFeatureStateInvalid) {
		t.Fatalf("unsigned expiry mutation error=%v", err)
	}
	_ = private
}

func TestTodo_CP_006_Mutation(t *testing.T) {
	signed, publicKey, _ := cp006State(t)
	signed.State.Features["promotion.v2"] = FeatureRule{Version: 3, Enabled: false, RolloutPercent: 100}
	if _, err := EvaluateFeatureAt(signed, publicKey, cp006Request("promotion.v2"), time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)); !errors.Is(err, ErrFeatureStateInvalid) {
		t.Fatalf("mutated state error=%v", err)
	}
	// The mutation above changes the canonical state. A change to the signed
	// digest itself is rejected before a business decision is returned.
	if signed.Verify(publicKey) == nil {
		t.Fatal("mutated signed state verified")
	}
}
