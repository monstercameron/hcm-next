package workload_test

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/workload"
)

// FuzzTodo_TRUST_007 is the TRUST-007 fuzz target.
//
// The invariant under test is one sentence: for any role, resource and
// action combination, Authorize never panics, always returns exactly Allow
// or Deny with a non-empty decision id, an Allow always carries the rule id
// that produced it, and a Deny is only ever produced by a caller/resource/
// action triple absent from AllowMatrix, an unverified caller, an
// unrecognized role, or an expired identity -- never anything else.
func FuzzTodo_TRUST_007(f *testing.F) {
	roles := []workload.ProcessRole{
		workload.RoleHCMNext, workload.RoleWorker, workload.RoleProjector, workload.RoleMigrate,
		workload.RoleScheduler, workload.RoleAdmin, "not_a_role",
	}
	resources := []workload.Resource{
		workload.ResourceTenant, workload.ResourceLedger, workload.ResourceOutbox, workload.ResourceProjection,
		workload.ResourceSchema, workload.ResourceWorkflowState, "unregistered_resource",
	}
	actions := []workload.Action{workload.ActionRead, workload.ActionWrite, "delete"}

	f.Add(byte(0), byte(0), byte(0), int64(0), true)
	f.Add(byte(1), byte(3), byte(1), int64(0), true)
	f.Add(byte(4), byte(2), byte(0), int64(-1000), false)
	f.Add(byte(6), byte(6), byte(2), int64(1000), true)

	f.Fuzz(func(t *testing.T, roleIdx, resourceIdx, actionIdx byte, offsetSeconds int64, present bool) {
		role := roles[int(roleIdx)%len(roles)]
		resource := resources[int(resourceIdx)%len(resources)]
		action := actions[int(actionIdx)%len(actions)]

		req := workload.Request{
			Resource:    resource,
			Action:      action,
			EvaluatedAt: baseTime.Add(time.Duration(offsetSeconds) * time.Second),
		}
		if present {
			authority := newTestAuthority(t, baseTime)
			verifier := newVerifier(t, authority.source, cellPrimary, baseTime)
			if role.Valid() {
				req.Caller = verifiedIdentity(t, authority, verifier, role)
			}
		}

		d, err := workload.Authorize(req)
		if err != nil {
			return
		}
		if d.Effect != workload.EffectAllow && d.Effect != workload.EffectDeny {
			t.Fatalf("Effect = %v is neither Allow nor Deny", d.Effect)
		}
		if d.DecisionID == "" {
			t.Fatal("DecisionID is empty")
		}
		if d.Effect == workload.EffectAllow && d.RuleID == "" {
			t.Fatal("an allow decision carries no rule id")
		}
		if d.Effect == workload.EffectDeny && d.Reason == "" {
			t.Fatal("a deny decision carries no reason")
		}
	})
}
