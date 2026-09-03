package workload_test

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/workload"
)

// TestTodo_TRUST_006_Mutation is the TRUST-006 mutation test. It asserts
// that [workload.NewIssuer] and [workload.NewVerifier] fail closed on
// incomplete configuration, that mutating the caller's private key slice
// after constructing an [workload.Issuer] has no effect on what it signs
// with, and that every trusted claim participates in the resulting
// identity's fingerprint.
func TestTodo_TRUST_006_Mutation(t *testing.T) {
	ctx := context.Background()

	t.Run("NewIssuer fails closed on an incomplete configuration", func(t *testing.T) {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		valid := workload.IssuerConfig{Name: authorityName, KeyID: "key-1", Private: priv}
		if _, err := workload.NewIssuer(valid); err != nil {
			t.Fatalf("NewIssuer(valid) = %v", err)
		}
		for name, mutate := range map[string]func(*workload.IssuerConfig){
			"no name":        func(c *workload.IssuerConfig) { c.Name = "" },
			"no key id":      func(c *workload.IssuerConfig) { c.KeyID = "" },
			"no private key": func(c *workload.IssuerConfig) { c.Private = nil },
			"short key":      func(c *workload.IssuerConfig) { c.Private = priv[:16] },
		} {
			cfg := valid
			mutate(&cfg)
			if _, err := workload.NewIssuer(cfg); err == nil {
				t.Errorf("NewIssuer(%s) succeeded, want a failure", name)
			}
		}
	})

	t.Run("NewVerifier fails closed on an incomplete configuration", func(t *testing.T) {
		source := workload.NewStaticKeySource()
		if _, err := workload.NewVerifier(workload.VerifierConfig{Keys: nil, Cell: cellPrimary}); err == nil {
			t.Error("NewVerifier(no keys) succeeded, want a failure")
		}
		if _, err := workload.NewVerifier(workload.VerifierConfig{Keys: source, Cell: ""}); err == nil {
			t.Error("NewVerifier(no cell) succeeded, want a failure")
		}
	})

	t.Run("mutating the caller's private key after construction does not change what Issuer signs with", func(t *testing.T) {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		original := make(ed25519.PrivateKey, len(priv))
		copy(original, priv)

		issuer, err := workload.NewIssuer(workload.IssuerConfig{Name: authorityName, KeyID: "key-1", Private: priv, Now: func() time.Time { return baseTime }})
		if err != nil {
			t.Fatalf("NewIssuer: %v", err)
		}
		for i := range priv {
			priv[i] ^= 0xFF
		}

		token, err := issuer.Issue(validSpec(workload.RoleWorker))
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		pub := original.Public().(ed25519.PublicKey)
		source := workload.NewStaticKeySource().WithKey(authorityName, "key-1", pub)
		verifier := newVerifier(t, source, cellPrimary, baseTime)
		if _, err := verifier.Verify(ctx, token); err != nil {
			t.Fatalf("Verify after caller mutated its own key slice = %v, want success (issuer copied the key)", err)
		}
	})

	t.Run("every trusted claim participates in the identity fingerprint", func(t *testing.T) {
		authority := newTestAuthority(t, baseTime)
		verifier := newVerifier(t, authority.source, cellPrimary, baseTime)

		baseToken, err := authority.issuer.Issue(validSpec(workload.RoleWorker))
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		baseline, err := verifier.Verify(ctx, baseToken)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}

		otherRoleToken, err := authority.issuer.Issue(validSpec(workload.RoleProjector))
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		otherRole, err := verifier.Verify(ctx, otherRoleToken)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if otherRole.Fingerprint() == baseline.Fingerprint() {
			t.Error("changing the role left the fingerprint unchanged")
		}

		spec := validSpec(workload.RoleWorker)
		spec.Subject = "instance-worker-2"
		otherSubjectToken, err := authority.issuer.Issue(spec)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		otherSubject, err := verifier.Verify(ctx, otherSubjectToken)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if otherSubject.Fingerprint() == baseline.Fingerprint() {
			t.Error("changing the subject left the fingerprint unchanged")
		}
	})
}
