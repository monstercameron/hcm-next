package stepup_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

const highRiskCapability = stepup.ActionApprove

// movableClock is the gate/issuer clock for obligation tests: recency and
// expiry are the whole point here, so the clock has to move.
type movableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movableClock) time() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movableClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type obligationFixture struct {
	clock  *movableClock
	issuer *stepup.Issuer
	gate   *stepup.Gate
	exec   *execCapture
	policy *stepup.ObligationPolicy
}

func newObligationFixture(t testing.TB) *obligationFixture {
	t.Helper()
	key := keyFrom("stepup-issuer")
	clock := &movableClock{now: baseTime}
	exec := &execCapture{}
	return &obligationFixture{
		clock:  clock,
		issuer: stepup.NewIssuer(key, clock.time, nil),
		gate:   stepup.NewGate(key, stepup.NewMemoryStore(), stepup.NewStaticSession(sessionRef), exec.run, clock.time),
		exec:   exec,
		policy: stepup.DefaultObligationPolicy(),
	}
}

// highRiskWrite is the governed action under test: a critical-risk approval
// of a payroll proposal.
func highRiskWrite() stepup.Operation {
	return stepup.Operation{
		Action:     stepup.ActionApprove,
		ProposalID: proposalID,
		Scopes:     testScopes,
		Tenant:     tenantAcme,
		Purpose:    stepup.PurposeHCMOperations,
		Capability: highRiskCapability,
		Risk:       stepup.RiskCritical,
	}
}

func obligationRequest(op stepup.Operation, stage stepup.Stage, at time.Time) stepup.ObligationRequest {
	return stepup.ObligationRequest{Operation: op, Stage: stage, At: at}
}

// TestTodo_TRUST_004 is the PRIMARY clause. The RED half: a high-risk write
// whose principal holds insufficient assurance, or whose step-up has gone
// stale, is refused with STEP_UP_REQUIRED and approves no proposal and runs
// no effect. The GREEN half: a qualifying step-up binds principal, session,
// purpose, capability, risk and its issued/expiry times, and the obligation
// is recomputed - not carried forward - at execution.
func TestTodo_TRUST_004(t *testing.T) {
	ctx := context.Background()
	op := highRiskWrite()

	t.Run("insufficient assurance returns STEP_UP_REQUIRED and changes nothing", func(t *testing.T) {
		fx := newObligationFixture(t)
		low := mustPrincipal(t, trust.AssuranceLow, sessionRef)
		if _, err := fx.issuer.Issue(low, op, stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute}); !errors.Is(err, stepup.ErrIssuerAssurance) {
			t.Fatalf("a low-assurance principal was issued a step-up: %v", err)
		}
		ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageDecision, fx.clock.time()), low, nil)
		if ob.Code != stepup.CodeStepUpRequired || !ob.Required || ob.Satisfied {
			t.Fatalf("obligation = %+v, want %s", ob, stepup.CodeStepUpRequired)
		}
		if !errors.Is(ob.Err(), stepup.ErrStepUpRequired) {
			t.Fatalf("Err() = %v, want ErrStepUpRequired", ob.Err())
		}
		if ob.Requirement.MinAssurance != trust.AssuranceHigh {
			t.Fatalf("policy selected assurance %s, want high for a critical-risk approval", ob.Requirement.MinAssurance)
		}
		// The gate is never reached, so nothing is approved and nothing runs.
		var none stepup.Proof
		_, gotOb, err := fx.gate.PresentUnderObligation(ctx, fx.policy, none, op, low, obligationRequest(op, stepup.StageExecution, fx.clock.time()))
		if !errors.Is(err, stepup.ErrStepUpRequired) {
			t.Fatalf("PresentUnderObligation = %v, want ErrStepUpRequired", err)
		}
		if gotOb.Satisfied || fx.exec.count() != 0 {
			t.Fatalf("an unmet obligation still executed: obligation=%+v calls=%d", gotOb, fx.exec.count())
		}
	})

	t.Run("a qualifying step-up binds every coordinate and executes once", func(t *testing.T) {
		fx := newObligationFixture(t)
		p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
		proof, err := fx.issuer.Issue(p, op, stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if proof.Purpose != op.Purpose || proof.Capability != op.Capability || proof.Risk != op.Risk {
			t.Fatalf("the proof does not bind purpose, capability and risk: %+v", proof)
		}

		decision := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageDecision, fx.clock.time()), p, &proof)
		if decision.Code != stepup.CodeStepUpSatisfied || !decision.Satisfied {
			t.Fatalf("decision-time obligation = %+v", decision)
		}
		for name, got := range map[string]string{
			"tenant": decision.Tenant.String(), "subject": decision.Subject, "session": decision.SessionRef,
			"purpose": decision.Purpose, "capability": decision.Capability, "proposal": decision.ProposalID,
			"proof": decision.ProofID, "binding digest": decision.BindingDigest, "rule": decision.RuleID,
		} {
			if got == "" {
				t.Fatalf("the satisfied obligation does not record %s: %+v", name, decision)
			}
		}
		if decision.Risk != op.Risk || decision.IssuedAt.IsZero() || decision.ExpiresAt.IsZero() {
			t.Fatalf("the satisfied obligation lost the risk or the validity window: %+v", decision)
		}

		out, execOb, err := fx.gate.PresentUnderObligation(ctx, fx.policy, proof, op, p, obligationRequest(op, stepup.StageExecution, fx.clock.time()))
		if err != nil {
			t.Fatalf("PresentUnderObligation: %v", err)
		}
		if out != stepup.OutcomeExecuted || fx.exec.count() != 1 {
			t.Fatalf("outcome = %s after %d executions, want one execution", out, fx.exec.count())
		}
		if execOb.Stage != stepup.StageExecution || execOb.BindingDigest != decision.BindingDigest {
			t.Fatalf("the execution recheck did not reproduce the decision binding: %+v vs %+v", execOb, decision)
		}
	})

	t.Run("a step-up that passed at decision time is refused when it goes stale before execution", func(t *testing.T) {
		fx := newObligationFixture(t)
		p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
		proof, err := fx.issuer.Issue(p, op, stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageDecision, fx.clock.time()), p, &proof); !ob.Satisfied {
			t.Fatalf("decision-time obligation was not satisfied: %+v", ob)
		}

		// The critical-risk rule allows a two-minute recency window.
		fx.clock.advance(3 * time.Minute)
		_, ob, err := fx.gate.PresentUnderObligation(ctx, fx.policy, proof, op, p, obligationRequest(op, stepup.StageExecution, fx.clock.time()))
		if !errors.Is(err, stepup.ErrStepUpRequired) {
			t.Fatalf("a stale step-up executed: %v", err)
		}
		if ob.Reason != stepup.ReasonProofStale || ob.Code != stepup.CodeStepUpRequired {
			t.Fatalf("obligation = %+v, want a stale refusal", ob)
		}
		if fx.exec.count() != 0 {
			t.Fatalf("a stale step-up ran the effect %d times", fx.exec.count())
		}
	})
}

// TestTodo_TRUST_004_Security is the SECURITY clause. Every way an attacker
// could try to reach a governed write without a current, correctly bound
// step-up is refused, and nothing is consumed or executed on the way.
func TestTodo_TRUST_004_Security(t *testing.T) {
	ctx := context.Background()
	op := highRiskWrite()

	t.Run("a valid bearer session alone never satisfies a governed write", func(t *testing.T) {
		fx := newObligationFixture(t)
		p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
		ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageDecision, fx.clock.time()), p, nil)
		if ob.Satisfied || ob.Reason != stepup.ReasonNoProof {
			t.Fatalf("obligation = %+v, want a refusal for a missing proof", ob)
		}
	})

	t.Run("a proof issued for a different governance coordinate does not transfer", func(t *testing.T) {
		fx := newObligationFixture(t)
		p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
		proof, err := fx.issuer.Issue(p, op, stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		targets := []struct {
			name   string
			mutate func(*stepup.Operation)
			reason string
		}{
			{"another capability", func(o *stepup.Operation) { o.Capability = stepup.ActionExport }, stepup.ReasonBindingMismatch},
			{"another purpose", func(o *stepup.Operation) { o.Purpose = "audit_review" }, stepup.ReasonBindingMismatch},
			{"another action", func(o *stepup.Operation) { o.Action = stepup.ActionRepair }, stepup.ReasonBindingMismatch},
			{"another proposal", func(o *stepup.Operation) { o.ProposalID = "00000000-0000-4000-8000-00000000000f" }, stepup.ReasonBindingMismatch},
			{"another scope set", func(o *stepup.Operation) { o.Scopes = []string{"payroll:2027-01"} }, stepup.ReasonBindingMismatch},
			{"another tenant", func(o *stepup.Operation) { o.Tenant = values.TenantId("vendor-corp") }, stepup.ReasonBindingMismatch},
			{"a lower risk claim", func(o *stepup.Operation) { o.Risk = stepup.RiskElevated }, stepup.ReasonBindingMismatch},
		}
		for _, tc := range targets {
			t.Run(tc.name, func(t *testing.T) {
				other := highRiskWrite()
				tc.mutate(&other)
				ob := stepup.EvaluateObligation(fx.policy, obligationRequest(other, stepup.StageDecision, fx.clock.time()), p, &proof)
				if ob.Satisfied || ob.Reason != tc.reason {
					t.Fatalf("obligation = %+v, want %s", ob, tc.reason)
				}
			})
		}
	})

	t.Run("a proof bound to another subject or session is refused", func(t *testing.T) {
		fx := newObligationFixture(t)
		p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
		proof, err := fx.issuer.Issue(p, op, stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		other := mustPrincipal(t, trust.AssuranceHigh, "session-stepup-2")
		if ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageDecision, fx.clock.time()), other, &proof); ob.Satisfied || ob.Reason != stepup.ReasonSessionMismatch {
			t.Fatalf("obligation = %+v, want a session refusal", ob)
		}
	})

	t.Run("a currently downgraded principal cannot spend an earlier high step-up", func(t *testing.T) {
		fx := newObligationFixture(t)
		p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
		proof, err := fx.issuer.Issue(p, op, stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		downgraded := mustPrincipal(t, trust.AssuranceSubstantial, sessionRef)
		ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageExecution, fx.clock.time()), downgraded, &proof)
		if ob.Satisfied || ob.Reason != stepup.ReasonCurrentAssuranceLow {
			t.Fatalf("obligation = %+v, want a current-assurance refusal", ob)
		}
		if _, _, err := fx.gate.PresentUnderObligation(ctx, fx.policy, proof, op, downgraded, obligationRequest(op, stepup.StageExecution, fx.clock.time())); !errors.Is(err, stepup.ErrStepUpRequired) {
			t.Fatalf("PresentUnderObligation = %v, want ErrStepUpRequired", err)
		}
		if fx.exec.count() != 0 {
			t.Fatalf("a downgraded principal executed the effect %d times", fx.exec.count())
		}
	})

	t.Run("missing or unusable policy input fails closed", func(t *testing.T) {
		fx := newObligationFixture(t)
		p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
		cases := []struct {
			name   string
			pol    *stepup.ObligationPolicy
			mutate func(*stepup.ObligationRequest)
			reason string
		}{
			{"no policy", nil, func(*stepup.ObligationRequest) {}, stepup.ReasonNoPolicy},
			{"unspecified risk", fx.policy, func(r *stepup.ObligationRequest) { r.Operation.Risk = stepup.RiskUnspecified }, stepup.ReasonRiskUnspecified},
			{"out-of-range risk", fx.policy, func(r *stepup.ObligationRequest) { r.Operation.Risk = stepup.RiskCritical + 7 }, stepup.ReasonRiskUnspecified},
			{"no capability", fx.policy, func(r *stepup.ObligationRequest) { r.Operation.Capability = "" }, stepup.ReasonMalformedRequest},
			{"no purpose", fx.policy, func(r *stepup.ObligationRequest) { r.Operation.Purpose = "" }, stepup.ReasonMalformedRequest},
			{"no tenant", fx.policy, func(r *stepup.ObligationRequest) { r.Operation.Tenant = "" }, stepup.ReasonMalformedRequest},
			{"no stage", fx.policy, func(r *stepup.ObligationRequest) { r.Stage = "" }, stepup.ReasonMalformedRequest},
			{"no evaluation time", fx.policy, func(r *stepup.ObligationRequest) { r.At = time.Time{} }, stepup.ReasonMalformedRequest},
			{"a capability policy never heard of", fx.policy, func(r *stepup.ObligationRequest) { r.Operation.Capability = "payroll.disburse" }, stepup.ReasonPolicyGap},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := obligationRequest(op, stepup.StageDecision, fx.clock.time())
				tc.mutate(&req)
				ob := stepup.EvaluateObligation(tc.pol, req, p, nil)
				if ob.Satisfied || ob.Code != stepup.CodeStepUpRequired || ob.Reason != tc.reason {
					t.Fatalf("obligation = %+v, want %s", ob, tc.reason)
				}
			})
		}
		if ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageDecision, fx.clock.time()), nil, nil); ob.Satisfied {
			t.Fatalf("a nil principal satisfied an obligation: %+v", ob)
		}
	})

	t.Run("a policy that would silently allow anything cannot be built", func(t *testing.T) {
		bad := []stepup.ObligationRule{
			{RuleID: "", Capability: stepup.ActionApprove, Purposes: []string{stepup.PurposeHCMOperations}, MinRisk: stepup.RiskElevated, MinAssurance: trust.AssuranceHigh, Recency: time.Minute},
			{RuleID: "no-purpose", Capability: stepup.ActionApprove, MinRisk: stepup.RiskElevated, MinAssurance: trust.AssuranceHigh, Recency: time.Minute},
			{RuleID: "no-assurance", Capability: stepup.ActionApprove, Purposes: []string{stepup.PurposeHCMOperations}, MinRisk: stepup.RiskElevated, Recency: time.Minute},
			{RuleID: "no-recency", Capability: stepup.ActionApprove, Purposes: []string{stepup.PurposeHCMOperations}, MinRisk: stepup.RiskElevated, MinAssurance: trust.AssuranceHigh},
			{RuleID: "no-risk-floor", Capability: stepup.ActionApprove, Purposes: []string{stepup.PurposeHCMOperations}, MinAssurance: trust.AssuranceHigh, Recency: time.Minute},
		}
		for _, rule := range bad {
			if _, err := stepup.NewObligationPolicy(rule); !errors.Is(err, stepup.ErrInvalidObligationPolicy) {
				t.Fatalf("rule %+v was accepted: %v", rule, err)
			}
		}
		good := stepup.ObligationRule{RuleID: "dup", Capability: stepup.ActionApprove, Purposes: []string{stepup.PurposeHCMOperations}, MinRisk: stepup.RiskElevated, MinAssurance: trust.AssuranceHigh, Recency: time.Minute}
		if _, err := stepup.NewObligationPolicy(good, good); !errors.Is(err, stepup.ErrInvalidObligationPolicy) {
			t.Fatalf("a duplicated rule id was accepted: %v", err)
		}
	})
}

// TestTodo_TRUST_004_Mutation is the MUTATION clause. Each bound coordinate,
// each clock boundary and each strictness choice is mutated one at a time; a
// mutant that drops a check, loosens a boundary or picks the weaker of two
// matching rules is caught here.
func TestTodo_TRUST_004_Mutation(t *testing.T) {
	fx := newObligationFixture(t)
	op := highRiskWrite()
	p := mustPrincipal(t, trust.AssuranceHigh, sessionRef)
	proof, err := fx.issuer.Issue(p, op, stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	t.Run("the strictest matching rule wins", func(t *testing.T) {
		// Both the elevated and the critical approval rules match a critical
		// request. A mutant taking the first or the weakest match selects
		// substantial; the contract selects high.
		req, rule, required := stepup.DefaultObligationPolicy().Select(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical)
		if !required || req.MinAssurance != trust.AssuranceHigh || req.Recency != 2*time.Minute || rule != "p1a.approve.critical" {
			t.Fatalf("selected %+v via %q, want the critical rule", req, rule)
		}
		// At elevated risk only the elevated rule applies.
		req, rule, required = stepup.DefaultObligationPolicy().Select(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskElevated)
		if !required || req.MinAssurance != trust.AssuranceSubstantial || rule != "p1a.approve.elevated" {
			t.Fatalf("selected %+v via %q, want the elevated rule", req, rule)
		}
		// A routine action of a governed capability owes nothing, and says so
		// rather than being silently satisfied by an absent proof.
		if _, _, required = stepup.DefaultObligationPolicy().Select(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskRoutine); required {
			t.Fatal("a routine action owes a step-up")
		}
		routine := highRiskWrite()
		routine.Risk = stepup.RiskRoutine
		ob := stepup.EvaluateObligation(fx.policy, obligationRequest(routine, stepup.StageDecision, fx.clock.time()), p, nil)
		if ob.Required || !ob.Satisfied || ob.Code != stepup.CodeStepUpNotRequired || ob.Err() != nil {
			t.Fatalf("routine obligation = %+v", ob)
		}
	})

	t.Run("clock boundaries are exact", func(t *testing.T) {
		cases := []struct {
			name   string
			at     time.Time
			reason string
			ok     bool
		}{
			{"one nanosecond before issuance", proof.IssuedAt.Add(-time.Nanosecond), stepup.ReasonProofPremature, false},
			{"at issuance", proof.IssuedAt, stepup.ReasonSatisfied, true},
			{"at the last instant inside the recency window", proof.IssuedAt.Add(2 * time.Minute), stepup.ReasonSatisfied, true},
			{"one nanosecond past the recency window", proof.IssuedAt.Add(2*time.Minute + time.Nanosecond), stepup.ReasonProofStale, false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageExecution, tc.at), p, &proof)
				if ob.Satisfied != tc.ok || ob.Reason != tc.reason {
					t.Fatalf("obligation = %+v, want satisfied=%v reason=%s", ob, tc.ok, tc.reason)
				}
			})
		}
		// Expiry is checked before recency, so a proof whose lifetime ran out
		// is refused as expired even under a generous window.
		wide, err := stepup.NewObligationPolicy(stepup.ObligationRule{
			RuleID: "wide", Capability: highRiskCapability, Purposes: []string{stepup.PurposeHCMOperations},
			MinRisk: stepup.RiskElevated, MinAssurance: trust.AssuranceHigh, Recency: time.Hour,
		})
		if err != nil {
			t.Fatalf("NewObligationPolicy: %v", err)
		}
		ob := stepup.EvaluateObligation(wide, obligationRequest(op, stepup.StageExecution, proof.ExpiresAt), p, &proof)
		if ob.Satisfied || ob.Reason != stepup.ReasonProofExpired {
			t.Fatalf("obligation = %+v, want an expiry refusal", ob)
		}
	})

	t.Run("an expired credential cannot spend a live step-up", func(t *testing.T) {
		expiring, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant: tenantAcme, Subject: subjectID, SubjectKind: trust.SubjectKindHuman,
			AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
			SessionRef: sessionRef, IssuedAt: baseTime.Add(-time.Hour), ExpiresAt: baseTime.Add(time.Second),
			CredentialDigest: "digest-" + subjectID,
		})
		if err != nil {
			t.Fatalf("NewPrincipal: %v", err)
		}
		ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageExecution, baseTime.Add(time.Second)), expiring, &proof)
		if ob.Satisfied || ob.Reason != stepup.ReasonPrincipalExpired {
			t.Fatalf("obligation = %+v, want a credential-expiry refusal", ob)
		}
	})

	t.Run("a weaker step-up than policy selected is refused", func(t *testing.T) {
		substantial := mustPrincipal(t, trust.AssuranceSubstantial, sessionRef)
		weak, err := fx.issuer.Issue(substantial, op, stepup.Requirement{MinAssurance: trust.AssuranceSubstantial, Recency: time.Minute})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageDecision, fx.clock.time()), p, &weak)
		if ob.Satisfied || ob.Reason != stepup.ReasonProofAssuranceLow {
			t.Fatalf("obligation = %+v, want a proof-assurance refusal", ob)
		}
	})

	t.Run("the binding digest changes with every bound coordinate", func(t *testing.T) {
		base := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageDecision, fx.clock.time()), p, &proof)
		if !base.Satisfied {
			t.Fatalf("baseline obligation was not satisfied: %+v", base)
		}
		// Same coordinates, other stage: the binding is the binding, so the
		// digest must be identical - that is what makes the execution-time
		// recheck comparable to the decision.
		again := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageExecution, fx.clock.time()), p, &proof)
		if again.BindingDigest != base.BindingDigest {
			t.Fatalf("the same binding produced two digests: %s vs %s", base.BindingDigest, again.BindingDigest)
		}
		elevated := highRiskWrite()
		elevated.Risk = stepup.RiskElevated
		lower, err := fx.issuer.Issue(p, elevated, stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		other := stepup.EvaluateObligation(fx.policy, obligationRequest(elevated, stepup.StageDecision, fx.clock.time()), p, &lower)
		if !other.Satisfied || other.BindingDigest == base.BindingDigest {
			t.Fatalf("a different risk tier reused the binding digest: %+v", other)
		}
	})

	t.Run("an unsatisfied obligation records no binding digest", func(t *testing.T) {
		ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageDecision, fx.clock.time()), p, nil)
		if ob.BindingDigest != "" {
			t.Fatalf("a refusal produced a binding digest: %+v", ob)
		}
	})
}

// FuzzTodo_TRUST_004 is the FUZZ clause: capability, purpose, risk tier and
// the evaluation offset are all attacker-chosen. Evaluation must never panic,
// must never satisfy an obligation whose requirement the presented proof does
// not actually meet, and must never leave a required obligation unmarked.
func FuzzTodo_TRUST_004(f *testing.F) {
	f.Add(stepup.ActionApprove, "hcm_operations", uint8(3), int64(0))
	f.Add("payroll.disburse", "", uint8(9), int64(-1))
	f.Add("", "audit_review", uint8(0), int64(1_000_000_000))

	fx := newObligationFixture(f)
	p := mustPrincipal(f, trust.AssuranceHigh, sessionRef)
	proof, err := fx.issuer.Issue(p, highRiskWrite(), stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute})
	if err != nil {
		f.Fatalf("Issue: %v", err)
	}

	f.Fuzz(func(t *testing.T, capability, purpose string, risk uint8, offset int64) {
		if len(capability) > 256 || len(purpose) > 256 {
			return
		}
		op := highRiskWrite()
		op.Capability, op.Purpose, op.Risk = capability, purpose, stepup.Risk(risk)
		at := baseTime.Add(time.Duration(offset))

		for _, presented := range []*stepup.Proof{nil, &proof} {
			ob := stepup.EvaluateObligation(fx.policy, obligationRequest(op, stepup.StageExecution, at), p, presented)
			switch ob.Code {
			case stepup.CodeStepUpRequired, stepup.CodeStepUpSatisfied, stepup.CodeStepUpNotRequired:
			default:
				t.Fatalf("unrecognized obligation code %q", ob.Code)
			}
			if ob.Required && ob.Satisfied != (ob.Code == stepup.CodeStepUpSatisfied) {
				t.Fatalf("code and satisfaction disagree: %+v", ob)
			}
			if (ob.Err() != nil) != (ob.Required && !ob.Satisfied) {
				t.Fatalf("Err() disagrees with the obligation: %v / %+v", ob.Err(), ob)
			}
			if !ob.Satisfied || !ob.Required {
				continue
			}
			// A satisfied obligation must actually hold: the presented proof
			// bound this exact coordinate, was current, and reached the
			// assurance policy selected.
			if presented == nil {
				t.Fatalf("an absent proof satisfied a required obligation: %+v", ob)
			}
			if ob.BindingDigest == "" || ob.ProofID != proof.ID {
				t.Fatalf("a satisfied obligation is not bound to the proof: %+v", ob)
			}
			if proof.Capability != capability || proof.Purpose != purpose || proof.Risk != stepup.Risk(risk) {
				t.Fatalf("a proof for another coordinate satisfied the obligation: %+v", ob)
			}
			if !proof.Assurance.AtLeast(ob.Requirement.MinAssurance) || at.Sub(proof.IssuedAt) > ob.Requirement.Recency {
				t.Fatalf("a stale or weak proof satisfied the obligation: %+v", ob)
			}
		}
	})
}

// TestTodo_TRUST_004_PolicySelectsAssuranceAndNamesNoIdPMethod is the
// REFACTOR clause: the obligation vocabulary talks about assurance, recency,
// capability, purpose and risk. Nothing in it names a factor, an enrollment
// or an identity-provider method, so domain code has no way to demand one.
func TestTodo_TRUST_004_PolicySelectsAssuranceAndNamesNoIdPMethod(t *testing.T) {
	forbidden := []string{
		"idp", "method", "factor", "otp", "totp", "sms", "webauthn", "fido",
		"passkey", "password", "push", "biometric", "yubikey", "mfa", "2fa",
	}
	mentions := func(s string) string {
		lower := strings.ToLower(s)
		for _, word := range forbidden {
			if strings.Contains(lower, word) {
				return word
			}
		}
		return ""
	}

	types := []reflect.Type{
		reflect.TypeOf(stepup.ObligationRule{}),
		reflect.TypeOf(stepup.ObligationRequest{}),
		reflect.TypeOf(stepup.Obligation{}),
		reflect.TypeOf(stepup.Requirement{}),
		reflect.TypeOf(stepup.Operation{}),
	}
	authnMethod := reflect.TypeOf(trust.AuthenticationMethodBearerToken)
	for _, typ := range types {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if word := mentions(field.Name); word != "" {
				t.Fatalf("%s.%s names an authentication mechanism (%q)", typ.Name(), field.Name, word)
			}
			if field.Type == authnMethod {
				t.Fatalf("%s.%s carries an authentication method; policy selects assurance, not a mechanism", typ.Name(), field.Name)
			}
		}
	}

	// The shipped policy and its stable tokens are equally mechanism-free.
	for _, risk := range []stepup.Risk{stepup.RiskElevated, stepup.RiskCritical} {
		for _, capability := range stepup.AllSensitiveActions {
			req, rule, required := stepup.DefaultObligationPolicy().Select(capability, stepup.PurposeHCMOperations, risk)
			if !required {
				continue
			}
			if word := mentions(rule); word != "" {
				t.Fatalf("rule %q names an authentication mechanism (%q)", rule, word)
			}
			if req.MinAssurance == trust.AssuranceUnspecified || req.Recency <= 0 {
				t.Fatalf("rule %q selected no assurance or no recency: %+v", rule, req)
			}
		}
	}
	for _, token := range []string{
		stepup.ReasonSatisfied, stepup.ReasonNotRequired, stepup.ReasonNoPolicy,
		stepup.ReasonMalformedRequest, stepup.ReasonRiskUnspecified, stepup.ReasonPolicyGap,
		stepup.ReasonNoProof, stepup.ReasonBindingMismatch, stepup.ReasonProofPremature,
		stepup.ReasonProofExpired, stepup.ReasonProofStale, stepup.ReasonProofAssuranceLow,
		stepup.ReasonCurrentAssuranceLow, stepup.ReasonPrincipalExpired, stepup.ReasonSessionMismatch,
	} {
		if word := mentions(token); word != "" {
			t.Fatalf("reason token %q names an authentication mechanism (%q)", token, word)
		}
	}

	// The caller-visible code is the one the contract names.
	if stepup.CodeStepUpRequired != "STEP_UP_REQUIRED" {
		t.Fatalf("the refusal code is %q", stepup.CodeStepUpRequired)
	}
	if ob := (stepup.Obligation{Required: true, Reason: stepup.ReasonNoProof}); !strings.Contains(ob.Err().Error(), "STEP_UP_REQUIRED") {
		t.Fatalf("the error does not carry the code: %v", ob.Err())
	}
	if s := (stepup.Obligation{Code: stepup.CodeStepUpRequired, Capability: "x"}).String(); !strings.Contains(s, "STEP_UP_REQUIRED") {
		t.Fatalf("the log line does not carry the code: %s", s)
	}
}

func TestObligationPolicy_SelectionAndWireValues(t *testing.T) {
	for _, tc := range []struct {
		risk stepup.Risk
		text string
	}{
		{stepup.RiskUnspecified, "unspecified"},
		{stepup.RiskRoutine, "routine"},
		{stepup.RiskElevated, "elevated"},
		{stepup.RiskCritical, "critical"},
		{stepup.Risk(99), "invalid(99)"},
	} {
		if got := tc.risk.String(); got != tc.text {
			t.Fatalf("Risk(%d).String() = %q, want %q", tc.risk, got, tc.text)
		}
	}
	policy := stepup.DefaultObligationPolicy()
	if policy == nil {
		t.Fatal("DefaultObligationPolicy returned nil")
	}
	if _, _, ok := policy.Select(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskUnspecified); ok {
		t.Fatal("unspecified risk selected a requirement")
	}
	var nilPolicy *stepup.ObligationPolicy
	if _, _, ok := nilPolicy.Select(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical); ok {
		t.Fatal("nil policy selected a requirement")
	}
	if _, _, ok := policy.Select("unknown", stepup.PurposeHCMOperations, stepup.RiskRoutine); ok {
		t.Fatal("routine unknown capability selected a requirement")
	}
	gap, rule, ok := policy.Select("unknown", stepup.PurposeHCMOperations, stepup.RiskCritical)
	if !ok || rule != "policy.gap" || gap.MinAssurance != trust.AssuranceHigh || gap.Recency <= 0 {
		t.Fatalf("critical policy gap = %+v, %q, %v", gap, rule, ok)
	}

	for _, tc := range []struct {
		ob       stepup.Obligation
		wantErr  bool
		contains string
	}{
		{stepup.Obligation{Required: false, Satisfied: true}, false, ""},
		{stepup.Obligation{Required: true, Satisfied: true}, false, ""},
		{stepup.Obligation{Required: true, Satisfied: false, Reason: stepup.ReasonNoProof}, true, "STEP_UP_REQUIRED"},
	} {
		if (tc.ob.Err() != nil) != tc.wantErr {
			t.Fatalf("Obligation.Err() = %v, want error=%v", tc.ob.Err(), tc.wantErr)
		}
		if tc.contains != "" && !strings.Contains(tc.ob.Err().Error(), tc.contains) {
			t.Fatalf("Obligation.Err() = %v, missing %q", tc.ob.Err(), tc.contains)
		}
	}
	if got := (stepup.Obligation{Code: stepup.CodeStepUpSatisfied, Stage: stepup.StageExecution, Capability: "cap", Purpose: "purpose", Risk: stepup.RiskElevated, Reason: stepup.ReasonSatisfied}).String(); !strings.Contains(got, "STEP_UP_SATISFIED") || !strings.Contains(got, "cap") {
		t.Fatalf("redacted obligation String = %q", got)
	}
}
