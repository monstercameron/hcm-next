package workload_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// TestTodo_TRUST_007_Integration is the TRUST-007 integration test. It
// chains TRUST-006 into TRUST-007 end to end: a credential an [workload.Issuer]
// mints, verified into an [workload.Identity] by a [workload.Verifier], is the
// only kind of value [workload.Authorize] ever sees as a real caller in this
// package -- there is no shortcut that hands Authorize a role without a
// verified credential behind it, matching the "network reachability never
// implies authority" invariant for every one of the four P1A processes.
func TestTodo_TRUST_007_Integration(t *testing.T) {
	authority := newTestAuthority(t, baseTime)
	verifier := newVerifier(t, authority.source, cellPrimary, baseTime)
	ctx := context.Background()

	scenarios := []struct {
		role     workload.ProcessRole
		resource workload.Resource
		action   workload.Action
		want     workload.Effect
	}{
		{workload.RoleWorker, workload.ResourceLedger, workload.ActionWrite, workload.EffectAllow},
		{workload.RoleWorker, workload.ResourceProjection, workload.ActionWrite, workload.EffectDeny},
		{workload.RoleProjector, workload.ResourceOutbox, workload.ActionRead, workload.EffectAllow},
		{workload.RoleProjector, workload.ResourceLedger, workload.ActionWrite, workload.EffectDeny},
		{workload.RoleHCMNext, workload.ResourceIntent, workload.ActionRead, workload.EffectAllow},
		{workload.RoleHCMNext, workload.ResourceIntent, workload.ActionWrite, workload.EffectDeny},
		{workload.RoleMigrate, workload.ResourceSchema, workload.ActionWrite, workload.EffectAllow},
		{workload.RoleMigrate, workload.ResourceLedger, workload.ActionRead, workload.EffectDeny},
	}

	for _, sc := range scenarios {
		token, err := authority.issuer.Issue(validSpec(sc.role))
		if err != nil {
			t.Fatalf("Issue(%s): %v", sc.role, err)
		}
		id, err := verifier.Verify(ctx, token)
		if err != nil {
			t.Fatalf("Verify(%s): %v", sc.role, err)
		}
		decision, err := workload.Authorize(workload.Request{Caller: id, Resource: sc.resource, Action: sc.action, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize(%s,%s,%s): %v", sc.role, sc.resource, sc.action, err)
		}
		if decision.Effect != sc.want {
			t.Errorf("%s.%s.%s = %v, want %v", sc.role, sc.resource, sc.action, decision.Effect, sc.want)
		}
	}

	// A credential verified for the wrong cell never reaches Authorize as
	// an Identity at all: the chain is broken one step earlier, in
	// TRUST-006's own Verify.
	wrongCellVerifier := newVerifier(t, authority.source, cellOther, baseTime)
	token, err := authority.issuer.Issue(validSpec(workload.RoleWorker))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := wrongCellVerifier.Verify(ctx, token); err == nil {
		t.Fatal("a cross-cell credential must not verify, so it can never reach Authorize")
	}
}
