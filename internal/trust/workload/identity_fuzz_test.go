package workload_test

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// FuzzTodo_TRUST_006 is the TRUST-006 fuzz target.
//
// The invariant under test is one sentence: for any input whatsoever,
// Verify either returns an error and a zero Identity, or returns a fully
// valid identity whose role, cell, key id and validity window all pass
// their own rules, and it never panics.
func FuzzTodo_TRUST_006(f *testing.F) {
	authority := newTestAuthority(f, baseTime)
	good, err := authority.issuer.Issue(validSpec(workload.RoleWorker))
	if err != nil {
		f.Fatalf("Issue: %v", err)
	}

	f.Add(good)
	f.Add("")
	f.Add("not-a-token")
	f.Add(strings.Repeat("A", 4096))
	f.Add(good + ".extra")
	f.Add(strings.ToUpper(good))

	f.Fuzz(func(t *testing.T, token string) {
		verifier := newVerifier(t, authority.source, cellPrimary, baseTime)
		id, err := verifier.Verify(context.Background(), token)
		zero := workload.Identity{}
		if err != nil {
			if id != zero {
				t.Fatalf("Verify returned both an error (%v) and a non-zero identity", err)
			}
			return
		}
		if id == zero {
			t.Fatal("Verify returned neither an error nor an identity")
		}
		if !id.Role().Valid() {
			t.Fatalf("verified identity has an invalid role %q", id.Role())
		}
		if id.Cell() != cellPrimary {
			t.Fatalf("verified identity cell = %q, want %q", id.Cell(), cellPrimary)
		}
		if !id.ExpiresAt().After(id.IssuedAt()) {
			t.Fatalf("verified identity has an empty validity window [%s, %s)", id.IssuedAt(), id.ExpiresAt())
		}
		if id.ExpiresAt().Sub(id.IssuedAt()) > workload.MaxIdentityLifetime {
			t.Fatalf("verified identity lifetime %s exceeds the maximum", id.ExpiresAt().Sub(id.IssuedAt()))
		}
		if !id.ValidAt(baseTime) {
			t.Fatalf("verified identity is not valid at the verification instant %s", baseTime)
		}
		if strings.Contains(id.String(), token) && token != "" {
			t.Fatal("the identity's rendered form leaks the credential")
		}
	})
}
