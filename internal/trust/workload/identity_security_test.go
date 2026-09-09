package workload_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// TestTodo_TRUST_006_Security is the TRUST-006 security test. It attacks
// the credential itself: a signature forged or substituted from another
// issuer's key, a claims payload tampered after signing, an oversized
// credential and a credential with an unknown claim must all fail, and no
// failure discloses the raw credential.
func TestTodo_TRUST_006_Security(t *testing.T) {
	authority := newTestAuthority(t, baseTime)
	verifier := newVerifier(t, authority.source, cellPrimary, baseTime)
	ctx := context.Background()
	valid, err := authority.issuer.Issue(validSpec(workload.RoleWorker))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	t.Run("a credential signed by a foreign key does not verify", func(t *testing.T) {
		foreignPub, foreignPriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate foreign key: %v", err)
		}
		_ = foreignPub
		foreignIssuer, err := workload.NewIssuer(workload.IssuerConfig{
			Name: authorityName, KeyID: "key-1", Private: foreignPriv, Now: func() time.Time { return baseTime },
		})
		if err != nil {
			t.Fatalf("NewIssuer: %v", err)
		}
		forged, err := foreignIssuer.Issue(validSpec(workload.RoleWorker))
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if _, err := verifier.Verify(ctx, forged); err == nil {
			t.Fatal("Verify(foreign key, claimed key-1) succeeded, want a failure")
		}
	})

	t.Run("a tampered payload does not verify", func(t *testing.T) {
		parts := strings.Split(valid, ".")
		if len(parts) != 3 {
			t.Fatalf("token has %d parts, want 3", len(parts))
		}
		tamperedClaims := `{"iss":"` + authorityName + `","sub":"instance-worker-1","role":"hcmnext","cell":"` + cellPrimary + `","kid":"key-1","iat":0,"exp":9999999999}`
		forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(tamperedClaims)) + "." + parts[2]
		if _, err := verifier.Verify(ctx, forged); err == nil {
			t.Fatal("Verify(tampered) succeeded, want a failure")
		}
	})

	t.Run("an unknown claim key is rejected", func(t *testing.T) {
		claims := `{"iss":"` + authorityName + `","sub":"x","role":"worker","cell":"` + cellPrimary + `","kid":"key-1","iat":1,"exp":9999999999,"escalate":true}`
		body := base64.RawURLEncoding.EncodeToString([]byte(claims))
		signingInput := "wlid1." + body
		sig := ed25519.Sign(authority.private, []byte(signingInput))
		token := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
		if _, err := verifier.Verify(ctx, token); err == nil {
			t.Fatal("Verify(unknown claim) succeeded, want a failure")
		}
	})

	t.Run("oversized credential is rejected before parsing", func(t *testing.T) {
		bounded, err := workload.NewVerifier(workload.VerifierConfig{
			Keys: authority.source, Cell: cellPrimary, Now: func() time.Time { return baseTime }, MaxTokenBytes: 32,
		})
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		if _, err := bounded.Verify(ctx, valid); err == nil {
			t.Fatal("Verify(oversized) succeeded, want a failure")
		}
	})

	t.Run("malformed credentials never panic", func(t *testing.T) {
		for _, token := range []string{"", "not-a-token", "wlid1..", "wlid2." + strings.SplitN(valid, ".", 2)[1], valid + ".extra"} {
			if _, err := verifier.Verify(ctx, token); err == nil {
				t.Errorf("Verify(%q) succeeded, want a failure", token)
			}
		}
	})

	t.Run("a verified identity never carries the raw credential", func(t *testing.T) {
		id, err := verifier.Verify(ctx, valid)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		rendered := id.String() + "|" + id.Fingerprint()
		if strings.Contains(rendered, valid) {
			t.Error("the identity's rendered form leaks the raw credential")
		}
	})
}
