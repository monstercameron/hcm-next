package workload_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// TestTodo_TRUST_006_Integration is the TRUST-006 integration test. It wires
// one authority through a key rotation: identities issued under the old key
// remain verifiable (via a [workload.KeySource] that still resolves it)
// until they naturally expire, while newly issued identities carry the new
// key id -- proving rotation evidence is real, not just a field name.
func TestTodo_TRUST_006_Integration(t *testing.T) {
	ctx := context.Background()
	pub1, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key 1: %v", err)
	}
	pub2, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key 2: %v", err)
	}

	issuer1, err := workload.NewIssuer(workload.IssuerConfig{Name: authorityName, KeyID: "key-1", Private: priv1, Now: func() time.Time { return baseTime }})
	if err != nil {
		t.Fatalf("NewIssuer(key-1): %v", err)
	}
	oldToken, err := issuer1.Issue(validSpec(workload.RoleWorker))
	if err != nil {
		t.Fatalf("Issue(key-1): %v", err)
	}

	rotationTime := baseTime.Add(2 * time.Minute)
	issuer2, err := workload.NewIssuer(workload.IssuerConfig{Name: authorityName, KeyID: "key-2", Private: priv2, Now: func() time.Time { return rotationTime }})
	if err != nil {
		t.Fatalf("NewIssuer(key-2): %v", err)
	}
	newToken, err := issuer2.Issue(validSpec(workload.RoleWorker))
	if err != nil {
		t.Fatalf("Issue(key-2): %v", err)
	}

	// The verifier's KeySource resolves both keys during the rotation
	// window, exactly as a live key set would during a real rotation.
	source := workload.NewStaticKeySource().
		WithKey(authorityName, "key-1", pub1).
		WithKey(authorityName, "key-2", pub2)
	verifier := newVerifier(t, source, cellPrimary, rotationTime)

	oldID, err := verifier.Verify(ctx, oldToken)
	if err != nil {
		t.Fatalf("Verify(old key still within lifetime): %v", err)
	}
	if oldID.KeyID() != "key-1" {
		t.Fatalf("old identity key id = %q, want key-1", oldID.KeyID())
	}

	newID, err := verifier.Verify(ctx, newToken)
	if err != nil {
		t.Fatalf("Verify(new key): %v", err)
	}
	if newID.KeyID() != "key-2" {
		t.Fatalf("new identity key id = %q, want key-2", newID.KeyID())
	}
	if oldID.Fingerprint() == newID.Fingerprint() {
		t.Fatal("two identities signed under different keys must not collide on fingerprint")
	}

	// Once every credential issued under key-1 has expired, retiring it
	// from the KeySource (a real rotation's final step) denies a new
	// presentation of that same old token outright.
	afterKey1Retired := workload.NewStaticKeySource().WithKey(authorityName, "key-2", pub2)
	retiredVerifier := newVerifier(t, afterKey1Retired, cellPrimary, rotationTime)
	if _, err := retiredVerifier.Verify(ctx, oldToken); err == nil {
		t.Fatal("Verify(old token after its key was retired) succeeded, want a failure")
	}
}
