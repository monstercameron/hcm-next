package admin_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/intelligence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/operations/admin"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// newPrincipal builds a hermetic *trust.Principal directly through
// trust.NewPrincipal - no verifier, no credential, no network - carrying
// exactly roles. It exists so this package's tests exercise
// [admin.RequireOperator] as the pure function it is, without depending on
// internal/transport at all.
func newPrincipal(t *testing.T, roles ...string) *trust.Principal {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               "acme-corp",
		Subject:              "subject-fixture",
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                roles,
		Purposes:             []string{"operator_diagnostics"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-fixture",
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest-fixture",
	})
	if err != nil {
		t.Fatalf("newPrincipal: %v", err)
	}
	return p
}

// TestTodo_SVC_011 is the SVC-011 primary test, scoped to this package's
// mandate: this is the pure half of the governed admin surface, so it
// proves the two RED conditions that are structural properties of this
// package's own code rather than of a running server. "CLI contains
// business/store logic" cannot happen here because there is no CLI in this
// package and no store dependency this package could reach - it imports
// only internal/trust and internal/domains, never internal/data,
// internal/ledger, a SQL driver, gRPC or Protobuf (tools/policy/libfirewall
// and tools/policy/importgraph enforce that repository-wide; this test
// documents the specific functional claim). "Operator methods inherit
// ordinary-user authority" cannot happen because [admin.RequireOperator] is
// the one predicate every AdminService method calls (via
// internal/transport/admin), and it is exercised here directly against
// every principal shape that must fail.
func TestTodo_SVC_011(t *testing.T) {
	t.Run("a nil principal is refused", func(t *testing.T) {
		if err := admin.RequireOperator(nil); err != admin.ErrNoPrincipal {
			t.Fatalf("RequireOperator(nil) = %v, want ErrNoPrincipal", err)
		}
	})

	t.Run("an authenticated principal without the operator role is refused", func(t *testing.T) {
		p := newPrincipal(t, "intent_author", "workforce_reader")
		if err := admin.RequireOperator(p); err != admin.ErrOperatorRoleRequired {
			t.Fatalf("RequireOperator(ordinary) = %v, want ErrOperatorRoleRequired", err)
		}
	})

	t.Run("a principal with no roles at all is refused", func(t *testing.T) {
		p := newPrincipal(t)
		if err := admin.RequireOperator(p); err != admin.ErrOperatorRoleRequired {
			t.Fatalf("RequireOperator(no roles) = %v, want ErrOperatorRoleRequired", err)
		}
	})

	t.Run("a principal carrying the operator role among others is accepted", func(t *testing.T) {
		p := newPrincipal(t, "intent_author", admin.OperatorRole)
		if err := admin.RequireOperator(p); err != nil {
			t.Fatalf("RequireOperator(operator) = %v, want nil", err)
		}
	})

	t.Run("ParseFields and ParseSections reject unknown and duplicate tokens", func(t *testing.T) {
		if _, err := admin.ParseFields([]string{"not.a.real.field"}); err == nil {
			t.Fatal("expected an unknown field token to be rejected")
		}
		if _, err := admin.ParseFields([]string{"worker.worker_number", "worker.worker_number"}); err == nil {
			t.Fatal("expected a duplicate field token to be rejected")
		}
		if _, err := admin.ParseSections([]string{"NOT_A_SECTION"}); err == nil {
			t.Fatal("expected an unknown section token to be rejected")
		}
		if _, err := admin.ParseSections([]string{"REQUEST", "REQUEST"}); err == nil {
			t.Fatal("expected a duplicate section token to be rejected")
		}
	})
}

// TestTodo_SVC_011_Golden pins the operator profile's authorization-decision
// shape: the fixed policy version and purpose, unconditional
// disclosability, and an ALLOW ruling for every projected field/section and
// no others. A change to any of these is a reviewed diff, not a silent
// widening or narrowing of what the operator profile discloses.
func TestTodo_SVC_011_Golden(t *testing.T) {
	fields := []people.FieldID{people.FieldWorkerNumber, people.FieldLegalName}
	decision := admin.OperatorWorkerAuthorization(fields)
	if decision.PolicyVersion != "hcmnext.admin.operator-profile/1" {
		t.Fatalf("PolicyVersion = %q", decision.PolicyVersion)
	}
	if decision.Purpose != "operator_diagnostics" {
		t.Fatalf("Purpose = %q", decision.Purpose)
	}
	if !decision.SubjectDisclosable {
		t.Fatal("SubjectDisclosable = false, want true")
	}
	if len(decision.Fields) != len(fields) {
		t.Fatalf("Fields has %d entries, want exactly %d", len(decision.Fields), len(fields))
	}
	for _, f := range fields {
		ruling, ok := decision.RulingFor(f)
		if !ok || ruling.Effect != people.EffectAllow {
			t.Fatalf("field %s ruling = %+v (ok=%v), want ALLOW", f, ruling, ok)
		}
	}

	sections := []intelligence.Section{intelligence.SectionRequest, intelligence.SectionEvents}
	txnDecision := admin.OperatorTransactionAuthorization(sections)
	if txnDecision.PolicyVersion != "hcmnext.admin.operator-profile/1" || txnDecision.Purpose != "operator_diagnostics" {
		t.Fatalf("txn decision = %+v", txnDecision)
	}
	if !txnDecision.TransactionDisclosable {
		t.Fatal("TransactionDisclosable = false, want true")
	}
	for _, s := range sections {
		ruling, ok := txnDecision.SectionRuling(s)
		if !ok || ruling.Effect != intelligence.EffectAllow {
			t.Fatalf("section %s ruling = %+v (ok=%v), want ALLOW", s, ruling, ok)
		}
	}
}

// TestTodo_SVC_011_Security proves [admin.RequireOperator] fails closed in
// every shape an attacker or a misconfigured caller could present: no
// principal, an empty role set, every ordinary role at once but never the
// operator one, and a role string that merely contains "operator" as a
// substring (a caller cannot get in by naming a role that looks close).
func TestTodo_SVC_011_Security(t *testing.T) {
	cases := []struct {
		name  string
		roles []string
	}{
		{"no roles", nil},
		{"one ordinary role", []string{"intent_author"}},
		{"many ordinary roles at once", []string{"intent_author", "workforce_reader", "billing_admin", "support_agent"}},
		{"a role that merely looks like the operator role", []string{"operator", "hcmnext.trust.role.operator.readonly"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPrincipal(t, tc.roles...)
			if err := admin.RequireOperator(p); err != admin.ErrOperatorRoleRequired {
				t.Fatalf("RequireOperator(%v) = %v, want ErrOperatorRoleRequired", tc.roles, err)
			}
		})
	}
}

// TestTodo_SVC_011_Mutation proves every exported function in this package
// is a pure, side-effect-free function of its arguments: calling the same
// function twice with equal inputs (including a shared input slice) yields
// deep-equal output, and the input slice itself is left unmodified. A
// capability package that could not make this guarantee would not be safe
// to call from the concurrent handlers internal/transport/admin runs it
// under.
func TestTodo_SVC_011_Mutation(t *testing.T) {
	fields := []people.FieldID{people.FieldGrade, people.FieldOrgUnit, people.FieldHireDate}
	fieldsCopy := append([]people.FieldID(nil), fields...)

	first := admin.OperatorWorkerAuthorization(fields)
	second := admin.OperatorWorkerAuthorization(fields)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("OperatorWorkerAuthorization is not deterministic:\n%+v\n%+v", first, second)
	}
	if !reflect.DeepEqual(fields, fieldsCopy) {
		t.Fatalf("OperatorWorkerAuthorization mutated its input slice: %v", fields)
	}

	sections := []intelligence.Section{intelligence.SectionGovernance, intelligence.SectionEffects}
	sectionsCopy := append([]intelligence.Section(nil), sections...)
	firstTxn := admin.OperatorTransactionAuthorization(sections)
	secondTxn := admin.OperatorTransactionAuthorization(sections)
	if !reflect.DeepEqual(firstTxn, secondTxn) {
		t.Fatalf("OperatorTransactionAuthorization is not deterministic:\n%+v\n%+v", firstTxn, secondTxn)
	}
	if !reflect.DeepEqual(sections, sectionsCopy) {
		t.Fatalf("OperatorTransactionAuthorization mutated its input slice: %v", sections)
	}

	p := newPrincipal(t, admin.OperatorRole)
	if err := admin.RequireOperator(p); err != nil {
		t.Fatalf("first RequireOperator: %v", err)
	}
	if err := admin.RequireOperator(p); err != nil {
		t.Fatalf("second RequireOperator on the same principal: %v", err)
	}
	if !p.HasRole(admin.OperatorRole) {
		t.Fatal("RequireOperator mutated the principal's own role set")
	}
}
