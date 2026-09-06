package trust_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust"
)

func actingAssignment() trust.AuthorityAssignment {
	return trust.AuthorityAssignment{
		AssignmentID: "assign-1", Kind: trust.GrantKindActingRole,
		From: "manager-1", To: "delegate-1", Role: "regional_hr_manager",
		Tenant: "acme-corp", OrganizationScopeID: "org-na",
		Capabilities: []string{"case.read"}, Resources: []string{"case"},
		Fields: []string{"status"}, Purposes: []string{"hcm_operations"},
		StartsAt: baseTime.Add(-time.Minute), EndsAt: baseTime.Add(time.Hour),
		RequiredAssurance: trust.AssuranceSubstantial, RevocationEpoch: 2,
	}
}

func coverageAssignment() trust.AuthorityAssignment {
	a := actingAssignment()
	a.AssignmentID, a.Kind, a.Role = "assign-2", trust.GrantKindCoverage, ""
	return a
}

// TestTodo_TRUST_013_ActingRoleAndCoverageUseTheSamePrimitive is the REFACTOR
// clause of TRUST-013: an acting role and vacation coverage are not a second
// authority path. Both lower onto a DelegationGrant and are evaluated by
// EvaluateDelegation, so every bound the primitive enforces applies to them
// without being restated.
func TestTodo_TRUST_013_ActingRoleAndCoverageUseTheSamePrimitive(t *testing.T) {
	scope := delegationScope()

	acting, err := actingAssignment().Grant()
	if err != nil {
		t.Fatalf("acting role grant: %v", err)
	}
	coverage, err := coverageAssignment().Grant()
	if err != nil {
		t.Fatalf("coverage grant: %v", err)
	}
	if acting.Kind != trust.GrantKindActingRole || coverage.Kind != trust.GrantKindCoverage {
		t.Fatalf("kinds were not carried: %q / %q", acting.Kind, coverage.Kind)
	}
	// Apart from their identity and kind the two grants are the same shape,
	// which is what "the same primitive" means here.
	acting.GrantID, acting.RootID, acting.Kind = coverage.GrantID, coverage.RootID, coverage.Kind
	if acting.Tenant != coverage.Tenant || acting.OrganizationScopeID != coverage.OrganizationScopeID ||
		acting.NotBefore != coverage.NotBefore || acting.ExpiresAt != coverage.ExpiresAt ||
		acting.RequiredAssurance != coverage.RequiredAssurance {
		t.Fatalf("assignments lowered onto different shapes:\n%+v\n%+v", acting, coverage)
	}

	for _, a := range []trust.AuthorityAssignment{actingAssignment(), coverageAssignment()} {
		got, err := a.Evaluate(scope, scope, baseTime, 2)
		if err != nil {
			t.Fatalf("%s: Evaluate: %v", a.Kind, err)
		}
		if got.GrantID != a.AssignmentID || got.Kind != a.Kind {
			t.Fatalf("%s: attribution lost: %+v", a.Kind, got)
		}
		if len(got.Capabilities) != 1 || got.Capabilities[0] != "case.read" {
			t.Fatalf("%s: capabilities = %v", a.Kind, got.Capabilities)
		}
		if len(got.Chain) != 1 || got.Chain[0] != a.AssignmentID || got.DecisionID == "" {
			t.Fatalf("%s: chain or decision id missing: %+v", a.Kind, got)
		}
	}
}

// TestTodo_TRUST_013_AssignmentsCannotEscapeTheDelegationBounds proves the
// lowering is not a bypass: an assignment that names authority the delegator
// does not hold, crosses a tenant, is revoked or has run out is refused by
// exactly the errors the primitive already defines.
func TestTodo_TRUST_013_AssignmentsCannotEscapeTheDelegationBounds(t *testing.T) {
	scope := delegationScope()
	cases := []struct {
		name   string
		mutate func(*trust.AuthorityAssignment, *trust.AuthorityScope)
		want   error
	}{
		{"expands beyond delegator authority", func(a *trust.AuthorityAssignment, _ *trust.AuthorityScope) {
			a.Capabilities = []string{"payroll.disburse"}
		}, trust.ErrDelegationExpanded},
		{"crosses tenant", func(_ *trust.AuthorityAssignment, s *trust.AuthorityScope) {
			s.Tenant = "vendor-corp"
		}, trust.ErrDelegationTenant},
		{"revoked", func(a *trust.AuthorityAssignment, _ *trust.AuthorityScope) {
			a.Revoked = true
		}, trust.ErrDelegationRevoked},
		{"outside its window", func(a *trust.AuthorityAssignment, _ *trust.AuthorityScope) {
			a.EndsAt = baseTime.Add(-time.Second)
			a.StartsAt = baseTime.Add(-time.Hour)
		}, trust.ErrDelegationExpired},
		{"assurance below requirement", func(a *trust.AuthorityAssignment, _ *trust.AuthorityScope) {
			a.RequiredAssurance = trust.AssuranceHigh + 1
		}, trust.ErrInvalidDelegation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, delegate := actingAssignment(), scope
			tc.mutate(&a, &delegate)
			if _, err := a.Evaluate(scope, delegate, baseTime, 2); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestTodo_TRUST_013_CoverageIsNeverRedelegable proves a stand-in cannot
// appoint a further stand-in, at the grant level and at validation, so the
// chain always leads back to the accountable principal.
func TestTodo_TRUST_013_CoverageIsNeverRedelegable(t *testing.T) {
	a := coverageAssignment()
	a.AllowRedelegation, a.MaxDepth = true, 4
	g, err := a.Grant()
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if g.AllowRedelegation || g.MaxDepth != 0 {
		t.Fatalf("coverage kept a re-delegation permission: %+v", g)
	}

	// Even a hand-built coverage grant is refused by the primitive itself.
	forged := delegationGrant()
	forged.Kind, forged.AllowRedelegation, forged.MaxDepth = trust.GrantKindCoverage, true, 3
	if err := trust.ValidateDelegation(forged); !errors.Is(err, trust.ErrRedelegationNotPermitted) {
		t.Fatalf("error = %v, want ErrRedelegationNotPermitted", err)
	}
}

// TestTodo_TRUST_013_AssignmentShapeIsValidatedAtConstruction keeps an
// unusable assignment from reaching evaluation at all.
func TestTodo_TRUST_013_AssignmentShapeIsValidatedAtConstruction(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*trust.AuthorityAssignment)
		want   error
	}{
		{"acting role without a role", func(a *trust.AuthorityAssignment) { a.Role = "" }, trust.ErrInvalidAssignment},
		{"coverage naming a role", func(a *trust.AuthorityAssignment) { a.Kind, a.Role = trust.GrantKindCoverage, "director" }, trust.ErrInvalidAssignment},
		{"direct kind is not an assignment", func(a *trust.AuthorityAssignment) { a.Kind = trust.GrantKindDirect }, trust.ErrInvalidAssignment},
		{"unknown kind", func(a *trust.AuthorityAssignment) { a.Kind = "escalation" }, trust.ErrInvalidAssignment},
		{"self-assignment", func(a *trust.AuthorityAssignment) { a.To = a.From }, trust.ErrInvalidDelegation},
		{"control character in the role", func(a *trust.AuthorityAssignment) { a.Role = "hr\nmanager" }, trust.ErrInvalidAssignment},
		{"inverted window", func(a *trust.AuthorityAssignment) { a.StartsAt, a.EndsAt = a.EndsAt, a.StartsAt }, trust.ErrInvalidDelegation},
		{"no capability", func(a *trust.AuthorityAssignment) { a.Capabilities = nil }, trust.ErrInvalidDelegation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := actingAssignment()
			tc.mutate(&a)
			if _, err := a.Grant(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestTodo_TRUST_013_UnknownGrantKindFailsClosed proves the kind is a closed
// set: an unrecognized kind on a hand-built grant is refused rather than
// treated as a direct grant.
func TestTodo_TRUST_013_UnknownGrantKindFailsClosed(t *testing.T) {
	g := delegationGrant()
	g.Kind = "root"
	if err := trust.ValidateDelegation(g); !errors.Is(err, trust.ErrInvalidDelegation) {
		t.Fatalf("error = %v, want ErrInvalidDelegation", err)
	}
	g.Kind = ""
	got, err := trust.EvaluateDelegation(trust.DelegationRequest{
		Grant: g, Delegator: delegationScope(), Delegate: delegationScope(),
		EvaluatedAt: baseTime, CurrentRevocationEpoch: 2,
	})
	if err != nil {
		t.Fatalf("EvaluateDelegation: %v", err)
	}
	if got.Kind != trust.GrantKindDirect {
		t.Fatalf("kind = %q, want the direct default", got.Kind)
	}
}
