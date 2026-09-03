package trust_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust"
)

func delegationScope() trust.AuthorityScope {
	return trust.AuthorityScope{
		Tenant: "acme-corp", OrganizationScopeID: "org-na",
		Capabilities: []string{"case.read", "case.write"}, Resources: []string{"case"},
		Fields: []string{"status", "owner"}, Purposes: []string{"hcm_operations"},
		Assurance: trust.AssuranceHigh, NotBefore: baseTime.Add(-time.Hour), ExpiresAt: baseTime.Add(time.Hour),
	}
}

func delegationGrant() trust.DelegationGrant {
	return trust.DelegationGrant{
		GrantID: "grant-1", RootID: "root-1", Delegator: "manager-1", Delegate: "delegate-1",
		Tenant: "acme-corp", OrganizationScopeID: "org-na",
		Capabilities: []string{"case.read"}, Resources: []string{"case"}, Fields: []string{"status"},
		Purposes: []string{"hcm_operations"}, NotBefore: baseTime.Add(-time.Minute), ExpiresAt: baseTime.Add(time.Hour),
		RequiredAssurance: trust.AssuranceSubstantial, RevocationEpoch: 2,
	}
}

// TestTodo_TRUST_013 covers the primary bounded-delegation contract.
func TestTodo_TRUST_013(t *testing.T) {
	delegator, delegate, grant := delegationScope(), delegationScope(), delegationGrant()
	got, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: grant, Delegator: delegator, Delegate: delegate, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2})
	if err != nil {
		t.Fatalf("EvaluateDelegation: %v", err)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0] != "case.read" {
		t.Fatalf("capabilities = %v", got.Capabilities)
	}
}

func TestTodo_TRUST_013_Bounds(t *testing.T) {
	base := trust.DelegationRequest{Grant: delegationGrant(), Delegator: delegationScope(), Delegate: delegationScope(), EvaluatedAt: baseTime, CurrentRevocationEpoch: 2}
	checks := []struct {
		name   string
		mutate func(*trust.DelegationRequest)
		want   error
	}{
		{"tenant crossing", func(r *trust.DelegationRequest) { r.Delegate.Tenant = "other" }, trust.ErrDelegationTenant},
		{"revoked epoch", func(r *trust.DelegationRequest) { r.CurrentRevocationEpoch = 3 }, trust.ErrDelegationRevoked},
		{"expired", func(r *trust.DelegationRequest) { r.EvaluatedAt = baseTime.Add(2 * time.Hour) }, trust.ErrDelegationExpired},
		{"expansion", func(r *trust.DelegationRequest) { r.Grant.Capabilities = []string{"admin.delete"} }, trust.ErrDelegationExpanded},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			_, err := trust.EvaluateDelegation(base)
			if tc.name == "tenant crossing" || tc.name == "revoked epoch" || tc.name == "expired" || tc.name == "expansion" {
				r := base
				tc.mutate(&r)
				_, err = trust.EvaluateDelegation(r)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_TRUST_013_RedelegationIsBoundedAndAttributable(t *testing.T) {
	p := delegationScope()
	first := delegationGrant()
	first.AllowRedelegation, first.MaxDepth = true, 2
	a, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: first, Delegator: p, Delegate: p, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	second := delegationGrant()
	second.GrantID, second.ParentGrantID, second.Delegator, second.Delegate = "grant-2", first.GrantID, "delegate-1", "delegate-2"
	second.AllowRedelegation, second.MaxDepth = true, 2
	b, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: second, Delegator: p, Delegate: p, Parent: &a, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Chain) != 2 || b.Chain[0] != first.GrantID || b.Chain[1] != second.GrantID {
		t.Fatalf("chain = %v", b.Chain)
	}
	second.AllowRedelegation = false
	if _, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: second, Delegator: p, Delegate: p, Parent: &a, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2}); !errors.Is(err, trust.ErrRedelegationNotPermitted) {
		t.Fatalf("error = %v", err)
	}
}

func TestTodo_TRUST_013_RedelegationCannotExceedParentBounds(t *testing.T) {
	p := delegationScope()
	first := delegationGrant()
	first.AllowRedelegation, first.MaxDepth = true, 2
	a, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: first, Delegator: p, Delegate: p, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	second := delegationGrant()
	second.GrantID, second.ParentGrantID, second.Delegator, second.Delegate = "grant-2", first.GrantID, "delegate-1", "delegate-2"
	second.AllowRedelegation, second.MaxDepth = true, 2
	b, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: second, Delegator: p, Delegate: p, Parent: &a, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	third := delegationGrant()
	third.GrantID, third.ParentGrantID, third.Delegator, third.Delegate = "grant-3", second.GrantID, "delegate-2", "delegate-3"
	third.AllowRedelegation, third.MaxDepth = true, 3
	if _, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: third, Delegator: p, Delegate: p, Parent: &b, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2}); !errors.Is(err, trust.ErrRedelegationNotPermitted) {
		t.Fatalf("depth error = %v, want ErrRedelegationNotPermitted", err)
	}
	a.AllowRedelegation = false
	if _, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: second, Delegator: p, Delegate: p, Parent: &a, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2}); !errors.Is(err, trust.ErrRedelegationNotPermitted) {
		t.Fatalf("parent permission error = %v, want ErrRedelegationNotPermitted", err)
	}
	second.ParentGrantID = "wrong-parent"
	if _, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: second, Delegator: p, Delegate: p, Parent: &a, EvaluatedAt: baseTime, CurrentRevocationEpoch: 2}); !errors.Is(err, trust.ErrRedelegationNotPermitted) {
		t.Fatalf("parent linkage error = %v, want ErrRedelegationNotPermitted", err)
	}
}
