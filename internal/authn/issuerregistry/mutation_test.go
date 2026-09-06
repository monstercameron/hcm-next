package issuerregistry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
)

// TestTodo_AUTHN_001_Mutation is this todo's MUTATION matrix test: it pins
// down exact boundary values a mutation of a comparison operator (< vs <=,
// == vs !=) would flip without any other test in this package noticing.
// Each subtest asserts both sides of one boundary.
func TestTodo_AUTHN_001_Mutation(t *testing.T) {
	t.Run("revision_strictly_greater_not_greater_or_equal", func(t *testing.T) {
		store := issuerregistry.NewMemoryStore()
		i := validIssuer(t)
		i.Revision = 5
		if _, err := issuerregistry.Publish(store, i); err != nil {
			t.Fatalf("Publish revision 5: %v", err)
		}
		// Equal to the current revision: refused. A mutant that turned the
		// package's "<=" conflict check into "<" would accept this.
		same := i
		same.PublishedAt = baseTime.Add(time.Minute)
		if _, err := issuerregistry.Publish(store, same); !errors.Is(err, issuerregistry.ErrRevisionConflict) {
			t.Fatalf("Publish same revision 5 again error = %v, want ErrRevisionConflict", err)
		}
		// One less than the current revision: still refused.
		lower := i
		lower.Revision = 4
		lower.PublishedAt = baseTime.Add(time.Minute)
		if _, err := issuerregistry.Publish(store, lower); !errors.Is(err, issuerregistry.ErrRevisionConflict) {
			t.Fatalf("Publish revision 4 after 5 error = %v, want ErrRevisionConflict", err)
		}
		// Exactly one more: the only value that must succeed.
		higher := i
		higher.Revision = 6
		higher.PublishedAt = baseTime.Add(time.Minute)
		if _, err := issuerregistry.Publish(store, higher); err != nil {
			t.Fatalf("Publish revision 6 after 5: %v", err)
		}
	})

	t.Run("clock_skew_zero_is_valid_negative_is_not", func(t *testing.T) {
		store := issuerregistry.NewMemoryStore()
		zero := validIssuer(t)
		zero.ClockSkew = 0
		if _, err := issuerregistry.Publish(store, zero); err != nil {
			t.Fatalf("Publish with zero clock skew: %v", err)
		}
		neg := validIssuer(t)
		neg.IssuerURL = "https://login.acme-neg.invalid/"
		neg.ClockSkew = -1 * time.Nanosecond
		if _, err := issuerregistry.Publish(store, neg); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
			t.Fatalf("Publish with -1ns clock skew error = %v, want ErrInvalidIssuer", err)
		}
	})

	t.Run("staleness_positive_not_non_negative", func(t *testing.T) {
		store := issuerregistry.NewMemoryStore()
		zero := validIssuer(t)
		zero.MetadataStaleness = 0
		if _, err := issuerregistry.Publish(store, zero); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
			t.Fatalf("Publish with zero staleness error = %v, want ErrInvalidIssuer", err)
		}
		one := validIssuer(t)
		one.MetadataStaleness = time.Nanosecond
		if _, err := issuerregistry.Publish(store, one); err != nil {
			t.Fatalf("Publish with 1ns staleness: %v", err)
		}
	})

	t.Run("pinned_key_validity_window_boundary", func(t *testing.T) {
		store := issuerregistry.NewMemoryStore()
		// NotBefore == NotAfter is an empty (not merely inverted) window:
		// refused. A mutant that turned "!Before" into "!After" would
		// accept this.
		i := validIssuer(t)
		i.JWKS.PinnedKeys[0].NotBefore = baseTime
		i.JWKS.PinnedKeys[0].NotAfter = baseTime
		if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
			t.Fatalf("Publish with an empty key validity window error = %v, want ErrInvalidIssuer", err)
		}
		// NotBefore one nanosecond before NotAfter: the narrowest valid
		// window, must succeed.
		i2 := validIssuer(t)
		i2.IssuerURL = "https://login.acme-narrow.invalid/"
		i2.JWKS.PinnedKeys[0].NotBefore = baseTime
		i2.JWKS.PinnedKeys[0].NotAfter = baseTime.Add(time.Nanosecond)
		if _, err := issuerregistry.Publish(store, i2); err != nil {
			t.Fatalf("Publish with a 1ns key validity window: %v", err)
		}
	})

	t.Run("activate_same_revision_refused_different_revision_allowed", func(t *testing.T) {
		store := issuerregistry.NewMemoryStore()
		v1 := validIssuer(t)
		published, err := issuerregistry.Publish(store, v1)
		if err != nil {
			t.Fatalf("Publish v1: %v", err)
		}
		if _, err := issuerregistry.Activate(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
			t.Fatalf("Activate v1: %v", err)
		}
		// Re-activating the exact same, already-active revision: refused.
		// A mutant that dropped the revision-equality half of
		// sameRevisionActive would wrongly permit this as "from == ACTIVE
		// so it must be a rollback".
		if _, err := issuerregistry.Activate(store, published.Ref(), evidence("carol-approver", baseTime.Add(2*time.Minute))); !errors.Is(err, issuerregistry.ErrInvalidTransition) {
			t.Fatalf("re-Activate the same active revision error = %v, want ErrInvalidTransition", err)
		}

		v2 := v1
		v2.Revision = 2
		v2.PublishedAt = baseTime.Add(3 * time.Minute)
		published2, err := issuerregistry.Publish(store, v2)
		if err != nil {
			t.Fatalf("Publish v2: %v", err)
		}
		// Activating a *different* revision while v1 is active: allowed
		// (roll-forward).
		if _, err := issuerregistry.Activate(store, published2.Ref(), evidence("carol-approver", baseTime.Add(4*time.Minute))); err != nil {
			t.Fatalf("Activate v2 while v1 is active: %v", err)
		}
	})

	t.Run("suspend_requires_exact_active_revision", func(t *testing.T) {
		store := issuerregistry.NewMemoryStore()
		v1 := validIssuer(t)
		p1, err := issuerregistry.Publish(store, v1)
		if err != nil {
			t.Fatalf("Publish v1: %v", err)
		}
		v2 := v1
		v2.Revision = 2
		v2.PublishedAt = baseTime.Add(time.Minute)
		p2, err := issuerregistry.Publish(store, v2)
		if err != nil {
			t.Fatalf("Publish v2: %v", err)
		}
		if _, err := issuerregistry.Activate(store, p2.Ref(), evidence("bob-approver", baseTime.Add(2*time.Minute))); err != nil {
			t.Fatalf("Activate v2: %v", err)
		}
		// v2 is active; suspending v1's (stale) ref is refused even though
		// *some* revision of this issuer is ACTIVE.
		if _, err := issuerregistry.Suspend(store, p1.Ref(), evidence("bob-approver", baseTime.Add(3*time.Minute))); !errors.Is(err, issuerregistry.ErrInvalidTransition) {
			t.Fatalf("Suspend a stale revision ref error = %v, want ErrInvalidTransition", err)
		}
		// Suspending the actually-active revision succeeds.
		if _, err := issuerregistry.Suspend(store, p2.Ref(), evidence("bob-approver", baseTime.Add(4*time.Minute))); err != nil {
			t.Fatalf("Suspend the active revision: %v", err)
		}
	})
}
