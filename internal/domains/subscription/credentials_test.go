package subscription

import (
	"errors"
	"testing"
	"time"
)

func credentialFixture(t *testing.T, version, keyRef string, provider SignatureProvider, from, until time.Time) SigningCredential {
	t.Helper()
	return SigningCredential{Destination: "endpoint:webhook", Profile: "ed25519-webhook", Version: version, KeyRef: keyRef, NotBefore: from, NotAfter: until, Provider: provider}
}

func TestTodo_SUB_007(t *testing.T) {
	clock := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	oldProvider, err := NewHMACProvider("key-1", []byte("old-secret"))
	if err != nil {
		t.Fatal(err)
	}
	newProvider, err := NewHMACProvider("key-2", []byte("new-secret"))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := NewCredentialRing("endpoint:webhook", func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(credentialFixture(t, "v1", "key-1", oldProvider, clock.Add(-time.Hour), clock.Add(24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", clock); err != nil {
		t.Fatal(err)
	}
	old, err := ring.Sign("sha256:event")
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Verify(old, "sha256:event"); err != nil {
		t.Fatal(err)
	}
	next := credentialFixture(t, "v2", "key-2", newProvider, clock, clock.Add(48*time.Hour))
	evidence, err := ring.Rotate(next, clock.Add(time.Hour), 2*time.Hour)
	if err != nil || evidence.Previous != "v1" || evidence.Current != "v2" {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
	if ring.CurrentVersion() != "v2" {
		t.Fatal("rotation did not activate new version")
	}
	if err := ring.Verify(old, "sha256:event", clock.Add(2*time.Hour)); err != nil {
		t.Fatalf("overlap rejected old signature: %v", err)
	}
	newSigned, err := ring.Sign("sha256:event", clock.Add(2*time.Hour))
	if err != nil || newSigned.Version != "v2" || newSigned.Profile != "ed25519-webhook" {
		t.Fatalf("new signature=%+v err=%v", newSigned, err)
	}
	if err := ring.Verify(newSigned, "sha256:event", clock.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_SUB_007_Race(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	provider, err := NewHMACProvider("key", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := NewCredentialRing("endpoint:webhook", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(credentialFixture(t, "v1", "key", provider, now.Add(-time.Hour), now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", now); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() {
			signed, err := ring.Sign("sha256:event")
			if err == nil {
				err = ring.Verify(signed, "sha256:event")
			}
			ch <- err
		}()
	}
	for i := 0; i < 32; i++ {
		if err := <-ch; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_SUB_007_Integration(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	oldProvider, _ := NewHMACProvider("old", []byte("old"))
	newProvider, _ := NewHMACProvider("new", []byte("new"))
	ring, err := NewCredentialRing("endpoint:webhook")
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(credentialFixture(t, "v1", "old", oldProvider, now.Add(-time.Hour), now.Add(24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", now); err != nil {
		t.Fatal(err)
	}
	next := credentialFixture(t, "v2", "new", newProvider, now, now.Add(24*time.Hour))
	if _, err := ring.Rotate(next, now, time.Hour); err != nil {
		t.Fatal(err)
	}
	signed, err := ring.Sign("sha256:event", now)
	if err != nil {
		t.Fatal(err)
	}
	signed.Destination = "endpoint:other"
	if !errors.Is(ring.Verify(signed, "sha256:event", now), ErrCredentialDestination) {
		t.Fatal("wrong destination signature was accepted")
	}
}

func TestTodo_SUB_007_Fault(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	provider, _ := NewHMACProvider("key", []byte("secret"))
	ring, err := NewCredentialRing("endpoint:webhook")
	if err != nil {
		t.Fatal(err)
	}
	credential := credentialFixture(t, "v1", "key", provider, now.Add(-time.Hour), now.Add(time.Hour))
	if err := ring.Add(credential); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", now); err != nil {
		t.Fatal(err)
	}
	signed, err := ring.Sign("sha256:event", now)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ring.Verify(signed, "sha256:event", now.Add(2*time.Hour)), ErrCredentialExpired) {
		t.Fatal("expired credential verified")
	}
	if err := ring.Revoke("v1", now); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ring.Verify(signed, "sha256:event", now), ErrCredentialRevoked) {
		t.Fatal("revoked credential verified")
	}
}

func TestTodo_SUB_007_Security(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	provider, _ := NewHMACProvider("key", []byte("secret"))
	ring, err := NewCredentialRing("endpoint:webhook")
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(credentialFixture(t, "v1", "key", provider, now.Add(-time.Hour), now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", now); err != nil {
		t.Fatal(err)
	}
	signed, err := ring.Sign("sha256:event", now)
	if err != nil {
		t.Fatal(err)
	}
	signed.Version = "v2"
	if !errors.Is(ring.Verify(signed, "sha256:event", now), ErrCredentialNotFound) {
		t.Fatal("signature crossed an undeclared version")
	}
	signed.Version = "v1"
	signed.MessageDigest = "sha256:other"
	if !errors.Is(ring.Verify(signed, "sha256:event", now), ErrSignatureInvalid) {
		t.Fatal("signature verified for a different message")
	}
}
