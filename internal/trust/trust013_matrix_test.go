package trust_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// FuzzTodo_TRUST_013 is the FUZZ matrix test for bounded delegation. Every
// input is treated as untrusted grant data: evaluation may reject it, but it
// must never panic or produce an unbounded successful authority.
func FuzzTodo_TRUST_013(f *testing.F) {
	f.Add([]byte("ordinary-grant"), uint8(0), uint8(0))
	f.Add([]byte{0, '\n', 0xff}, uint8(3), uint8(9))
	f.Fuzz(func(t *testing.T, raw []byte, depth, epoch uint8) {
		if len(raw) > 256 {
			raw = raw[:256]
		}
		id := string(raw)
		g := delegationGrant()
		g.GrantID = id
		g.MaxDepth = depth
		g.RevocationEpoch = uint64(epoch)
		got, err := trust.EvaluateDelegation(trust.DelegationRequest{
			Grant: g, Delegator: delegationScope(), Delegate: delegationScope(),
			EvaluatedAt: baseTime, CurrentRevocationEpoch: 2,
		})
		if err != nil {
			return
		}
		if got.GrantID != id || len(got.Capabilities) == 0 || len(got.Resources) == 0 || len(got.Purposes) == 0 {
			t.Fatalf("successful delegation escaped its bounds: %+v", got)
		}
	})
}

// TestTodo_TRUST_013_Security is the SECURITY matrix test for bounded
// delegation. It proves identity, tenancy, scope, validity and cycle checks
// fail closed when an attacker tampers with trusted-looking inputs.
func TestTodo_TRUST_013_Security(t *testing.T) {
	base := trust.DelegationRequest{Grant: delegationGrant(), Delegator: delegationScope(), Delegate: delegationScope(), EvaluatedAt: baseTime, CurrentRevocationEpoch: 2}
	cases := []struct {
		name   string
		mutate func(*trust.DelegationRequest)
		want   error
	}{
		{"tenant", func(r *trust.DelegationRequest) { r.Delegate.Tenant = "attacker-tenant" }, trust.ErrDelegationTenant},
		{"organization", func(r *trust.DelegationRequest) { r.Delegate.OrganizationScopeID = "other-org" }, trust.ErrDelegationExpanded},
		{"assurance", func(r *trust.DelegationRequest) { r.Delegate.Assurance = trust.AssuranceLow }, trust.ErrDelegationExpanded},
		{"revocation", func(r *trust.DelegationRequest) { r.CurrentRevocationEpoch = 3 }, trust.ErrDelegationRevoked},
		{"control character in grant identity", func(r *trust.DelegationRequest) { r.Grant.GrantID = "grant\nforged" }, trust.ErrInvalidDelegation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mutate(&r)
			if _, err := trust.EvaluateDelegation(r); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("redelegation cycle", func(t *testing.T) {
		parent := delegationGrant()
		parent.AllowRedelegation, parent.MaxDepth = true, 3
		a, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: parent, Delegator: delegationScope(), Delegate: delegationScope(), EvaluatedAt: baseTime, CurrentRevocationEpoch: 2})
		if err != nil {
			t.Fatal(err)
		}
		child := delegationGrant()
		child.GrantID, child.ParentGrantID, child.Delegator, child.Delegate = "grant-2", parent.GrantID, "delegate-1", "delegate-2"
		child.AllowRedelegation, child.MaxDepth = true, 3
		a.Chain = append(a.Chain, child.GrantID)
		child.ParentGrantID = child.GrantID
		if _, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: child, Delegator: delegationScope(), Delegate: delegationScope(), Parent: &a, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2}); !errors.Is(err, trust.ErrDelegationCycle) {
			t.Fatalf("error = %v, want %v", err, trust.ErrDelegationCycle)
		}
	})
}

// TestTodo_TRUST_013_Mutation is the MUTATION matrix test for bounded
// delegation. It catches mutants that drop one intersection dimension, widen
// validity, or reuse caller-owned slices.
func TestTodo_TRUST_013_Mutation(t *testing.T) {
	base := trust.DelegationRequest{Grant: delegationGrant(), Delegator: delegationScope(), Delegate: delegationScope(), EvaluatedAt: baseTime, CurrentRevocationEpoch: 2}
	original, err := trust.EvaluateDelegation(base)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*trust.DelegationRequest)
		check  func(trust.EffectiveAuthority) bool
	}{
		{"grant capability", func(r *trust.DelegationRequest) { r.Grant.Capabilities = []string{"admin.delete"} }, func(e trust.EffectiveAuthority) bool { return false }},
		{"grant resource", func(r *trust.DelegationRequest) { r.Grant.Resources = []string{"other"} }, func(e trust.EffectiveAuthority) bool { return false }},
		{"grant purpose", func(r *trust.DelegationRequest) { r.Grant.Purposes = []string{"other"} }, func(e trust.EffectiveAuthority) bool { return false }},
		{"delegate field", func(r *trust.DelegationRequest) { r.Delegate.Fields = []string{"status"} }, func(e trust.EffectiveAuthority) bool { return len(e.Fields) == 1 && e.Fields[0] == "status" }},
		{"grant validity", func(r *trust.DelegationRequest) { r.Grant.NotBefore = baseTime.Add(time.Minute) }, func(e trust.EffectiveAuthority) bool { return false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mutate(&r)
			got, err := trust.EvaluateDelegation(r)
			if tc.name == "grant validity" {
				if !errors.Is(err, trust.ErrDelegationExpired) {
					t.Fatalf("error = %v, want expiry", err)
				}
				return
			}
			if tc.name == "grant capability" || tc.name == "grant resource" || tc.name == "grant purpose" {
				if !errors.Is(err, trust.ErrDelegationExpanded) {
					t.Fatalf("error = %v, want expansion", err)
				}
				return
			}
			if err != nil || !tc.check(got) {
				t.Fatalf("mutation was not enforced: got=%+v err=%v", got, err)
			}
		})
	}

	// The result owns its slices; mutating request data after evaluation must
	// not retroactively widen or alter the decision.
	base.Grant.Capabilities[0] = "tampered"
	if original.Capabilities[0] != "case.read" || original.DecisionID == "" {
		t.Fatalf("result retained mutable input or missing decision id: %+v", original)
	}
}
