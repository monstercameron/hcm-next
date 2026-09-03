package workload_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/workload"
)

func verifiedIdentity(t *testing.T, authority *testAuthority, verifier *workload.Verifier, role workload.ProcessRole) workload.Identity {
	t.Helper()
	token, err := authority.issuer.Issue(validSpec(role))
	if err != nil {
		t.Fatalf("Issue(%s): %v", role, err)
	}
	id, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify(%s): %v", role, err)
	}
	return id
}

// TestTodo_TRUST_007 is the TRUST-007 primary test.
//
// GREEN: worker may read and write the ledger, projector may write
// projection, and hcmnext may read across the resources P1A's read/query
// dispatch covers -- and every allow carries the rule id that produced it.
//
// RED: a projector invoking a write, a connector-shaped role reading an
// unrelated resource, and an unknown workload/capability pair all return
// DENY before any domain access, with an explainable, stable reason. Each
// is a separate subtest so a regression names itself.
func TestTodo_TRUST_007(t *testing.T) {
	authority := newTestAuthority(t, baseTime)
	verifier := newVerifier(t, authority.source, cellPrimary, baseTime)
	worker := verifiedIdentity(t, authority, verifier, workload.RoleWorker)
	projector := verifiedIdentity(t, authority, verifier, workload.RoleProjector)
	hcmnext := verifiedIdentity(t, authority, verifier, workload.RoleHCMNext)
	migrate := verifiedIdentity(t, authority, verifier, workload.RoleMigrate)

	t.Run("GREEN: worker reads and writes the ledger", func(t *testing.T) {
		for _, action := range []workload.Action{workload.ActionRead, workload.ActionWrite} {
			d, err := workload.Authorize(workload.Request{Caller: worker, Resource: workload.ResourceLedger, Action: action, EvaluatedAt: baseTime})
			if err != nil {
				t.Fatalf("Authorize: %v", err)
			}
			if d.Effect != workload.EffectAllow {
				t.Fatalf("worker.ledger.%s = %v, want Allow", action, d.Effect)
			}
			if d.RuleID == "" {
				t.Error("an allow decision must carry the rule id that produced it")
			}
		}
	})

	t.Run("GREEN: projector writes projection", func(t *testing.T) {
		d, err := workload.Authorize(workload.Request{Caller: projector, Resource: workload.ResourceProjection, Action: workload.ActionWrite, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectAllow {
			t.Fatalf("projector.projection.write = %v, want Allow", d.Effect)
		}
	})

	t.Run("GREEN: hcmnext reads across P1A's read/query resources", func(t *testing.T) {
		for _, resource := range []workload.Resource{
			workload.ResourceTenant, workload.ResourceIntent, workload.ResourceCapability,
			workload.ResourceSnapshot, workload.ResourceProposal, workload.ResourceObservation,
			workload.ResourceWorkflowState, workload.ResourceLedger, workload.ResourceOutbox, workload.ResourceProjection,
		} {
			d, err := workload.Authorize(workload.Request{Caller: hcmnext, Resource: resource, Action: workload.ActionRead, EvaluatedAt: baseTime})
			if err != nil {
				t.Fatalf("Authorize(%s): %v", resource, err)
			}
			if d.Effect != workload.EffectAllow {
				t.Errorf("hcmnext.%s.read = %v, want Allow", resource, d.Effect)
			}
		}
	})

	t.Run("RED: hcmnext is read-only in P1A -- every write is denied", func(t *testing.T) {
		for _, resource := range []workload.Resource{
			workload.ResourceTenant, workload.ResourceLedger, workload.ResourceProjection, workload.ResourceWorkflowState,
		} {
			d, err := workload.Authorize(workload.Request{Caller: hcmnext, Resource: resource, Action: workload.ActionWrite, EvaluatedAt: baseTime})
			if err != nil {
				t.Fatalf("Authorize(%s): %v", resource, err)
			}
			if d.Effect != workload.EffectDeny {
				t.Errorf("hcmnext.%s.write = %v, want Deny", resource, d.Effect)
			}
			if d.Reason != workload.ReasonNoMatchingRule {
				t.Errorf("hcmnext.%s.write reason = %q, want %q", resource, d.Reason, workload.ReasonNoMatchingRule)
			}
		}
	})

	t.Run("RED: projector invoking a domain-of-record write is denied before domain access", func(t *testing.T) {
		d, err := workload.Authorize(workload.Request{Caller: projector, Resource: workload.ResourceLedger, Action: workload.ActionWrite, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectDeny {
			t.Fatalf("projector.ledger.write = %v, want Deny (projector never writes the domain of record)", d.Effect)
		}
	})

	t.Run("RED: a caller reading a resource unrelated to its role is denied", func(t *testing.T) {
		d, err := workload.Authorize(workload.Request{Caller: migrate, Resource: workload.ResourceLedger, Action: workload.ActionRead, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectDeny {
			t.Fatalf("migrate.ledger.read = %v, want Deny", d.Effect)
		}
	})

	t.Run("RED: an unknown workload/capability pair is denied", func(t *testing.T) {
		d, err := workload.Authorize(workload.Request{Caller: worker, Resource: workload.Resource("nonexistent_resource"), Action: workload.ActionRead, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectDeny || d.Reason != workload.ReasonNoMatchingRule {
			t.Fatalf("Authorize(unknown resource) = %+v, want a no-matching-rule deny", d)
		}
	})

	t.Run("RED: a caller with no verified identity is denied, not treated as an anonymous allow", func(t *testing.T) {
		d, err := workload.Authorize(workload.Request{Resource: workload.ResourceLedger, Action: workload.ActionRead, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectDeny || d.Reason != workload.ReasonNoVerifiedIdentity {
			t.Fatalf("Authorize(no identity) = %+v, want no-verified-identity deny", d)
		}
	})

	t.Run("RED: an expired identity is denied even though it once verified", func(t *testing.T) {
		token, err := authority.issuer.Issue(validSpec(workload.RoleWorker))
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		// Verify at issuance (still valid) so we hold a real Identity, then
		// evaluate Authorize as of a later instant past its expiry -- this
		// is the re-check a long-lived RPC handler must perform, not a
		// property Verify itself re-derives.
		id, err := verifier.Verify(context.Background(), token)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		d, err := workload.Authorize(workload.Request{Caller: id, Resource: workload.ResourceLedger, Action: workload.ActionRead, EvaluatedAt: id.ExpiresAt().Add(time.Second)})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectDeny || d.Reason != workload.ReasonIdentityExpired {
			t.Fatalf("Authorize(expired identity) = %+v, want identity-expired deny", d)
		}
	})

	t.Run("scheduler and admin carry no rule yet: any use is denied", func(t *testing.T) {
		schedToken, err := authority.issuer.Issue(workload.IssueSpec{Subject: "sched-1", Role: workload.RoleScheduler, Cell: cellPrimary, Lifetime: time.Minute})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		sched, err := verifier.Verify(context.Background(), schedToken)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		d, err := workload.Authorize(workload.Request{Caller: sched, Resource: workload.ResourceWorkflowState, Action: workload.ActionRead, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if d.Effect != workload.EffectDeny {
			t.Fatalf("scheduler.workflow_state.read = %v, want Deny (no P1B rule yet)", d.Effect)
		}
	})
}
