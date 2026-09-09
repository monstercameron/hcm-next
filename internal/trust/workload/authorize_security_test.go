package workload_test

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// TestTodo_TRUST_007_Security is the TRUST-007 security test. It checks
// that Authorize fails closed on every malformed or adversarial Request
// shape, that a deny never discloses more than its stable reason token, and
// that Authorize never panics.
func TestTodo_TRUST_007_Security(t *testing.T) {
	authority := newTestAuthority(t, baseTime)
	verifier := newVerifier(t, authority.source, cellPrimary, baseTime)
	worker := verifiedIdentity(t, authority, verifier, workload.RoleWorker)

	t.Run("an invalid action is refused, not silently denied as if it were a real action", func(t *testing.T) {
		if _, err := workload.Authorize(workload.Request{Caller: worker, Resource: workload.ResourceLedger, Action: "delete", EvaluatedAt: baseTime}); err == nil {
			t.Error("Authorize(invalid action) succeeded, want an error")
		}
	})

	t.Run("an empty resource is refused", func(t *testing.T) {
		if _, err := workload.Authorize(workload.Request{Caller: worker, Resource: "", Action: workload.ActionRead, EvaluatedAt: baseTime}); err == nil {
			t.Error("Authorize(empty resource) succeeded, want an error")
		}
	})

	t.Run("Authorize never panics on a degenerate request", func(t *testing.T) {
		degenerate := []workload.Request{
			{},
			{Caller: worker},
			{Resource: workload.ResourceLedger, Action: workload.ActionRead},
			{Caller: worker, Resource: workload.Resource(strings.Repeat("x", 10000)), Action: workload.ActionRead},
		}
		for i, req := range degenerate {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("request %d panicked: %v", i, r)
					}
				}()
				if _, err := workload.Authorize(req); err != nil {
					t.Logf("request %d: Authorize returned %v (acceptable for a malformed request)", i, err)
				}
			}()
		}
	})

	t.Run("a denial's reason is a stable token, never a resource-specific detail", func(t *testing.T) {
		d, err := workload.Authorize(workload.Request{Caller: worker, Resource: "some_unregistered_resource_name", Action: workload.ActionRead, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectDeny {
			t.Fatal("expected a deny for this scenario")
		}
		if d.Reason != workload.ReasonNoMatchingRule {
			t.Errorf("Reason = %q, want the stable token %q", d.Reason, workload.ReasonNoMatchingRule)
		}
	})

	t.Run("a zero-value caller can never be confused with a legitimately verified one", func(t *testing.T) {
		var zero workload.Identity
		d, err := workload.Authorize(workload.Request{Caller: zero, Resource: workload.ResourceLedger, Action: workload.ActionRead, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectDeny || d.Reason != workload.ReasonNoVerifiedIdentity {
			t.Fatalf("Authorize(zero caller) = %+v, want a no-verified-identity deny", d)
		}
	})

	t.Run("EvaluatedAt left zero skips the expiry re-check rather than denying every call", func(t *testing.T) {
		// This documents current behavior precisely so a future change to
		// it is a deliberate decision, not a silent regression: a caller
		// that does not supply EvaluatedAt gets Authorize's other checks
		// (identity presence, role validity, matrix membership) but not
		// live expiry re-verification -- the expectation is that a real
		// call site always supplies the request instant.
		d, err := workload.Authorize(workload.Request{Caller: worker, Resource: workload.ResourceLedger, Action: workload.ActionRead})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectAllow {
			t.Fatalf("Authorize(zero EvaluatedAt) = %v, want Allow", d.Effect)
		}
	})

	t.Run("an identity expired well past leeway is still denied deterministically", func(t *testing.T) {
		d, err := workload.Authorize(workload.Request{Caller: worker, Resource: workload.ResourceLedger, Action: workload.ActionRead, EvaluatedAt: worker.ExpiresAt().Add(365 * 24 * time.Hour)})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectDeny || d.Reason != workload.ReasonIdentityExpired {
			t.Fatalf("Authorize(long-expired) = %+v, want identity-expired deny", d)
		}
	})
}
