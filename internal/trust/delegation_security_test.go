package trust

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func delegationScopeForSecurity() AuthorityScope {
	return AuthorityScope{
		Tenant: "tenant-a", OrganizationScopeID: "org-a",
		Capabilities: []string{"read", "write"}, Resources: []string{"case", "profile"},
		Fields: []string{"owner", "status"}, Purposes: []string{"operations", "audit"},
		Assurance: AssuranceHigh, NotBefore: time.Unix(90, 0), ExpiresAt: time.Unix(300, 0),
	}
}

func delegationGrantForSecurity() DelegationGrant {
	return DelegationGrant{
		GrantID: "grant-1", RootID: "root-1", Delegator: "manager", Delegate: "delegate",
		Tenant: "tenant-a", OrganizationScopeID: "org-a", Capabilities: []string{"read"},
		Resources: []string{"case"}, Fields: []string{"status"}, Purposes: []string{"operations"},
		NotBefore: time.Unix(100, 0), ExpiresAt: time.Unix(250, 0), RequiredAssurance: AssuranceSubstantial,
		RevocationEpoch: 2,
	}
}

func TestValidateDelegation_RejectsMalformedBounds(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*DelegationGrant)
		want   error
	}{
		{"blank grant id", func(g *DelegationGrant) { g.GrantID = " " }, ErrInvalidDelegation},
		{"self delegation", func(g *DelegationGrant) { g.Delegate = g.Delegator }, ErrInvalidDelegation},
		{"grant cycle", func(g *DelegationGrant) { g.ParentGrantID = g.GrantID }, ErrDelegationCycle},
		{"root cycle", func(g *DelegationGrant) { g.ParentGrantID = g.RootID }, ErrDelegationCycle},
		{"bad tenant", func(g *DelegationGrant) { g.Tenant = values.TenantId("bad tenant") }, ErrInvalidDelegation},
		{"missing organization", func(g *DelegationGrant) { g.OrganizationScopeID = "" }, ErrInvalidDelegation},
		{"missing validity", func(g *DelegationGrant) { g.NotBefore = time.Time{} }, ErrInvalidDelegation},
		{"inverted validity", func(g *DelegationGrant) { g.ExpiresAt = g.NotBefore }, ErrInvalidDelegation},
		{"unspecified assurance", func(g *DelegationGrant) { g.RequiredAssurance = AssuranceUnspecified }, ErrInvalidDelegation},
		{"assurance above maximum", func(g *DelegationGrant) { g.RequiredAssurance = AssuranceHigh + 1 }, ErrInvalidDelegation},
		{"empty capabilities", func(g *DelegationGrant) { g.Capabilities = nil }, ErrInvalidDelegation},
		{"empty resources", func(g *DelegationGrant) { g.Resources = nil }, ErrInvalidDelegation},
		{"invalid field", func(g *DelegationGrant) { g.Fields = []string{"status\nowner"} }, ErrInvalidDelegation},
		{"empty purpose", func(g *DelegationGrant) { g.Purposes = nil }, ErrInvalidDelegation},
		{"redelegation without depth", func(g *DelegationGrant) { g.ParentGrantID, g.MaxDepth = "parent", 0 }, ErrInvalidDelegation},
		{"unknown kind", func(g *DelegationGrant) { g.Kind = "unknown" }, ErrInvalidDelegation},
		{"coverage redelegation", func(g *DelegationGrant) { g.Kind, g.AllowRedelegation = GrantKindCoverage, true }, ErrRedelegationNotPermitted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := delegationGrantForSecurity()
			tc.mutate(&g)
			if err := ValidateDelegation(g); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateDelegation = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestEvaluateDelegation_DeniesRevocationTenantTimeAndAssurance(t *testing.T) {
	base := DelegationRequest{Grant: delegationGrantForSecurity(), Delegator: delegationScopeForSecurity(), Delegate: delegationScopeForSecurity(), EvaluatedAt: time.Unix(150, 0), CurrentRevocationEpoch: 2}
	cases := []struct {
		name   string
		mutate func(*DelegationRequest)
		want   error
	}{
		{"missing evaluation time", func(r *DelegationRequest) { r.EvaluatedAt = time.Time{} }, ErrInvalidDelegation},
		{"revoked flag", func(r *DelegationRequest) { r.Grant.Revoked = true }, ErrDelegationRevoked},
		{"revoked epoch", func(r *DelegationRequest) { r.CurrentRevocationEpoch = 3 }, ErrDelegationRevoked},
		{"before grant", func(r *DelegationRequest) { r.EvaluatedAt = time.Unix(99, 0) }, ErrDelegationExpired},
		{"at grant expiry", func(r *DelegationRequest) { r.EvaluatedAt = time.Unix(250, 0) }, ErrDelegationExpired},
		{"tenant mismatch", func(r *DelegationRequest) { r.Delegate.Tenant = "tenant-b" }, ErrDelegationTenant},
		{"organization mismatch", func(r *DelegationRequest) { r.Delegate.OrganizationScopeID = "org-b" }, ErrDelegationExpanded},
		{"delegator assurance too low", func(r *DelegationRequest) { r.Delegator.Assurance = AssuranceLow }, ErrDelegationExpanded},
		{"delegate assurance too low", func(r *DelegationRequest) { r.Delegate.Assurance = AssuranceLow }, ErrDelegationExpanded},
		{"no capability intersection", func(r *DelegationRequest) { r.Delegate.Capabilities = []string{"write"} }, ErrDelegationExpanded},
		{"no resource intersection", func(r *DelegationRequest) { r.Delegate.Resources = []string{"profile"} }, ErrDelegationExpanded},
		{"no purpose intersection", func(r *DelegationRequest) { r.Delegate.Purposes = []string{"audit"} }, ErrDelegationExpanded},
		{"effective time inverted", func(r *DelegationRequest) {
			r.Delegator.NotBefore = time.Unix(200, 0)
			r.Delegate.ExpiresAt = time.Unix(199, 0)
		}, ErrDelegationExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mutate(&r)
			if _, err := EvaluateDelegation(r); !errors.Is(err, tc.want) {
				t.Fatalf("EvaluateDelegation = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestEvaluateDelegation_RedelegationChainIsBoundedAndCopied(t *testing.T) {
	scope := delegationScopeForSecurity()
	firstGrant := delegationGrantForSecurity()
	firstGrant.AllowRedelegation, firstGrant.MaxDepth = true, 2
	first, err := EvaluateDelegation(DelegationRequest{Grant: firstGrant, Delegator: scope, Delegate: scope, EvaluatedAt: time.Unix(150, 0), CurrentRevocationEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	secondGrant := delegationGrantForSecurity()
	secondGrant.GrantID, secondGrant.RootID, secondGrant.ParentGrantID = "grant-2", firstGrant.RootID, firstGrant.GrantID
	secondGrant.Delegator, secondGrant.Delegate = "delegate", "leaf"
	secondGrant.AllowRedelegation, secondGrant.MaxDepth = true, 2
	delegateScope := scope
	delegateScope.Assurance = AssuranceSubstantial
	second, err := EvaluateDelegation(DelegationRequest{Grant: secondGrant, Delegator: scope, Delegate: delegateScope, Parent: &first, EvaluatedAt: time.Unix(150, 0), CurrentRevocationEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.Chain, []string{"grant-1", "grant-2"}) || second.Kind != GrantKindDirect || second.DecisionID == "" {
		t.Fatalf("effective redelegation = %+v", second)
	}
	if second.Assurance != AssuranceSubstantial || !second.NotBefore.Equal(time.Unix(100, 0)) || !second.ExpiresAt.Equal(time.Unix(250, 0)) {
		t.Fatalf("effective bounds = %+v", second)
	}
	first.Chain[0] = "tampered"
	if second.Chain[0] == "tampered" {
		t.Fatal("redelegation result aliased parent chain")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*DelegationGrant, *EffectiveAuthority)
	}{
		{"grant disallows", func(g *DelegationGrant, _ *EffectiveAuthority) { g.AllowRedelegation = false }},
		{"parent disallows", func(_ *DelegationGrant, p *EffectiveAuthority) { p.AllowRedelegation = false }},
		{"wrong parent actor", func(_ *DelegationGrant, p *EffectiveAuthority) { p.Delegate = "someone-else" }},
		{"wrong root", func(g *DelegationGrant, _ *EffectiveAuthority) { g.RootID = "other-root" }},
		{"wrong parent id", func(g *DelegationGrant, _ *EffectiveAuthority) { g.ParentGrantID = "other-grant" }},
		{"empty chain", func(_ *DelegationGrant, p *EffectiveAuthority) { p.Chain = nil }},
		{"depth breach", func(g *DelegationGrant, _ *EffectiveAuthority) { g.MaxDepth = 1 }},
		{"parent depth breach", func(_ *DelegationGrant, p *EffectiveAuthority) { p.MaxDepth = 1 }},
		{"cycle", func(g *DelegationGrant, p *EffectiveAuthority) {
			g.MaxDepth = 3
			p.MaxDepth = 3
			p.Chain = []string{g.GrantID, p.GrantID}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, p := secondGrant, first
			tc.mutate(&g, &p)
			want := ErrRedelegationNotPermitted
			if tc.name == "cycle" {
				want = ErrDelegationCycle
			}
			if _, err := EvaluateDelegation(DelegationRequest{Grant: g, Delegator: scope, Delegate: scope, Parent: &p, EvaluatedAt: time.Unix(150, 0), CurrentRevocationEpoch: 2}); !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestEvaluateDelegation_NarrowsEffectiveTimeAndDropsEmptyOptionalFields(t *testing.T) {
	delegator := delegationScopeForSecurity()
	delegate := delegationScopeForSecurity()
	delegator.NotBefore = time.Unix(120, 0)
	delegate.ExpiresAt = time.Unix(200, 0)
	grant := delegationGrantForSecurity()
	grant.Fields = []string{"field-not-held"}
	got, err := EvaluateDelegation(DelegationRequest{Grant: grant, Delegator: delegator, Delegate: delegate, EvaluatedAt: time.Unix(150, 0), CurrentRevocationEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Fields != nil || !got.NotBefore.Equal(time.Unix(120, 0)) || !got.ExpiresAt.Equal(time.Unix(200, 0)) {
		t.Fatalf("effective optional/time bounds = %+v", got)
	}
}
