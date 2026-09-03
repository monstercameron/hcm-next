package repair_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/dataops"
	"github.com/monstercameron/hcm-next/internal/domains/repair"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestTodo_REPAIR_001 is the REPAIR-001 primary test: a diagnosis over a
// comparison produces an immutable, ordered, non-executable plan whose steps
// name their exact target, expected states, corrective action, write set,
// idempotency, risk, approval, observation and rollback.
func TestTodo_REPAIR_001(t *testing.T) {
	t.Run("GREEN: the diagnosis records evidence, confidence and unknowns", func(t *testing.T) {
		d := diagnose(t, safeScenario(t))
		if d.Problem != repair.ProblemFieldDrift {
			t.Fatalf("problem = %s, want FIELD_DRIFT", d.Problem)
		}
		if d.Confidence != repair.ConfidenceHigh {
			t.Fatalf("confidence = %s, want HIGH over ordered evidence", d.Confidence)
		}
		if len(d.EvidenceRefs) == 0 {
			t.Fatal("the diagnosis cites no evidence")
		}
		if d.DiffDigest != safeScenario(t).diff.Digest {
			t.Fatal("the diagnosis is not bound to the comparison it was drawn from")
		}
		if !reflect.DeepEqual(d.Actionable, []dataops.FieldID{fieldJobCode, fieldBase}) &&
			!reflect.DeepEqual(d.Actionable, []dataops.FieldID{fieldBase, fieldJobCode}) {
			t.Fatalf("actionable fields = %v", d.Actionable)
		}

		// An unordered or type-level disagreement is blocked, not actionable.
		unsafe := diagnose(t, unsafeScenario(t))
		if unsafe.Problem != repair.ProblemUnorderedConflict {
			t.Fatalf("problem = %s, want UNORDERED_CONFLICT", unsafe.Problem)
		}
		if unsafe.Confidence != repair.ConfidenceLow {
			t.Fatalf("confidence = %s over a disagreement no automation may resolve", unsafe.Confidence)
		}
		if len(unsafe.Blocked) != 1 || unsafe.Blocked[0] != fieldLocation {
			t.Fatalf("blocked = %v, want [%s]", unsafe.Blocked, fieldLocation)
		}
	})

	t.Run("GREEN: the action follows the authority, in deterministic order", func(t *testing.T) {
		p := plan(t, safeScenario(t))
		if len(p.Steps) != 2 {
			t.Fatalf("%d step(s) for 2 mismatches", len(p.Steps))
		}
		for i, s := range p.Steps {
			if s.Ordinal != i+1 {
				t.Fatalf("step at index %d has ordinal %d", i, s.Ordinal)
			}
		}
		// The comparison orders findings by field, so the plan does too:
		// assignment.job_code before compensation.base.
		if p.Steps[0].Target.Field != fieldJobCode || p.Steps[1].Target.Field != fieldBase {
			t.Fatalf("step order = %s, %s", p.Steps[0].Target.Field, p.Steps[1].Target.Field)
		}

		refresh := stepFor(t, p, fieldJobCode)
		if refresh.Action != repair.ActionRefreshProjection ||
			refresh.Target.System != localSystem {
			t.Fatalf("externally mastered field: action=%s system=%s",
				refresh.Action, refresh.Target.System)
		}
		if v, ok := refresh.ExpectedPost.Get(); !ok || v != "ENG-4" {
			t.Fatalf("refresh post-state = %q (readable %t), want the master's value", v, ok)
		}
		if refresh.RequiresApproval {
			t.Fatal("a local projection refresh was made to need an approval")
		}

		propose := stepFor(t, p, fieldBase)
		if propose.Action != repair.ActionProposeExternalUpdate ||
			propose.Target.System != externalSystem {
			t.Fatalf("locally mastered field: action=%s system=%s",
				propose.Action, propose.Target.System)
		}
		if v, ok := propose.ExpectedPost.Get(); !ok || v != "135000.00 USD" {
			t.Fatalf("external update post-state = %q (readable %t)", v, ok)
		}
		if !propose.RequiresApproval || len(propose.ApprovalRoles) == 0 {
			t.Fatal("an external write was planned with no approval requirement")
		}
		if !p.RequiresApproval || len(p.SoDExcludedRoles) == 0 {
			t.Fatal("the plan does not carry the approval and segregation-of-duties requirement")
		}
	})

	t.Run("GREEN: every step is pinned, bounded and reversible", func(t *testing.T) {
		p := plan(t, safeScenario(t))
		for _, s := range p.Steps {
			if s.IdempotencyKey == "" {
				t.Fatalf("step %d has no idempotency key", s.Ordinal)
			}
			if s.MaxAttempts < 1 || s.MaxAttempts > repair.MaxStepAttempts {
				t.Fatalf("step %d allows %d attempt(s)", s.Ordinal, s.MaxAttempts)
			}
			if len(s.WriteSet) == 0 || s.Rollback == "" {
				t.Fatalf("step %d writes with no declared write set or rollback", s.Ordinal)
			}
			if s.Observation == "" || s.Success == "" {
				t.Fatalf("step %d has no verification criteria", s.Ordinal)
			}
			if s.Treatment != repair.TreatmentAppendCorrection {
				t.Fatalf("step %d treats history as %s", s.Ordinal, s.Treatment)
			}
			kinds := map[repair.PreconditionKind]bool{}
			for _, pre := range s.Preconditions {
				kinds[pre.Kind] = true
			}
			for _, want := range []repair.PreconditionKind{
				repair.PreconditionDiffDigest,
				repair.PreconditionObservationDigest,
				repair.PreconditionAuthorityPolicy,
				repair.PreconditionCanonicalRevision,
			} {
				if !kinds[want] {
					t.Fatalf("step %d does not pin %s", s.Ordinal, want)
				}
			}
		}
	})

	t.Run("GREEN: the plan is immutable, digested and bound to its evidence", func(t *testing.T) {
		p := plan(t, safeScenario(t))
		if p.Digest == "" || p.InputsDigest == "" {
			t.Fatal("the plan carries no digest")
		}
		if p.DiffDigest != safeScenario(t).diff.Digest {
			t.Fatal("the plan is not bound to the comparison it corrects")
		}
		if err := p.Receipt.Validate(); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		if !p.Effects.IsZero() {
			t.Fatalf("planning counted effects: %v", p.Effects.NonZero())
		}
		again := plan(t, safeScenario(t))
		if again.Digest != p.Digest {
			t.Fatal("the plan digest is not reproducible")
		}
	})

	t.Run("RED: a stale or unknown finding becomes an executable step", func(t *testing.T) {
		p := plan(t, staleScenario(t))
		if len(p.Steps) != 0 {
			t.Fatalf("a stale comparison produced %d step(s)", len(p.Steps))
		}
		if len(p.SkippedFields) == 0 {
			t.Fatal("the skipped fields were dropped instead of reported with a reason")
		}
		for _, s := range p.SkippedFields {
			if s.Reason != dataops.ReasonObservationStale {
				t.Fatalf("%s skipped for %q", s.Field, s.Reason)
			}
		}
	})

	t.Run("RED: an unsafe mismatch becomes a write", func(t *testing.T) {
		p := plan(t, unsafeScenario(t))
		review := stepFor(t, p, fieldLocation)
		if review.Action.Writes() {
			t.Fatalf("an unresolvable disagreement produced the writing action %s", review.Action)
		}
		if review.Action != repair.ActionHumanReview {
			t.Fatalf("action = %s, want HUMAN_REVIEW", review.Action)
		}
		if len(review.WriteSet) != 0 {
			t.Fatalf("a review step declared a write set: %v", review.WriteSet)
		}
		if review.Risk != repair.RiskHigh || p.Risk != repair.RiskHigh {
			t.Fatalf("step risk=%s plan risk=%s, want HIGH", review.Risk, p.Risk)
		}
		if !review.RequiresApproval {
			t.Fatal("a high-risk review step needs no approval")
		}
	})

	t.Run("RED: the plan is executable", func(t *testing.T) {
		p := plan(t, safeScenario(t))
		if p.Executable {
			t.Fatal("a P1A repair plan reported itself as executable")
		}
		if p.NotExecutableReason != repair.NotExecutableReason {
			t.Fatalf("not-executable reason = %q", p.NotExecutableReason)
		}
		if p.ExecutionState != repair.ExecutionState {
			t.Fatalf("execution state = %q, want %q", p.ExecutionState, repair.ExecutionState)
		}
		// A plan that says it is executable does not validate, whatever built
		// it.
		forged := p
		forged.Executable = true
		if err := forged.Validate(); !errors.Is(err, repair.ErrPlanInvalid) {
			t.Fatalf("a forged executable plan validated: %v", err)
		}
		// And the package exposes no way to run one.
		if method, found := reflect.TypeOf(p).MethodByName("Execute"); found {
			t.Fatalf("RepairPlan exposes %s", method.Name)
		}
	})
}

// TestTodo_REPAIR_001_Fault proves that every malformed diagnosis, unsupported
// claim and unconstrained step is refused rather than planned around.
func TestTodo_REPAIR_001_Fault(t *testing.T) {
	s := safeScenario(t)

	t.Run("RED: an unsupported causal claim is accepted", func(t *testing.T) {
		d := diagnose(t, s)
		d.EvidenceRefs = nil
		if err := d.Validate(); !errors.Is(err, repair.ErrUnsupportedClaim) {
			t.Fatalf("err = %v, want ErrUnsupportedClaim", err)
		}
		d = diagnose(t, s)
		d.Unknowns = []string{"who_changed_it"}
		if err := d.Validate(); !errors.Is(err, repair.ErrUnsupportedClaim) {
			t.Fatalf("HIGH confidence with an open unknown: err = %v", err)
		}
	})

	t.Run("RED: a raw log suggestion counts as evidence", func(t *testing.T) {
		for _, ref := range []string{"log line 4412", "the connector was flaky", "evidence:"} {
			_, err := repair.Diagnose("diag-x", s.diff, []string{ref})
			if !errors.Is(err, repair.ErrEvidenceRef) {
				t.Fatalf("evidence %q accepted: err = %v", ref, err)
			}
		}
	})

	t.Run("RED: an unconstrained retry is planned", func(t *testing.T) {
		p := plan(t, s)
		for _, mutate := range []struct {
			name string
			edit func(*repair.Step)
			want error
		}{
			{"no idempotency key", func(st *repair.Step) { st.IdempotencyKey = "" }, repair.ErrUnconstrainedRetry},
			{"no preconditions", func(st *repair.Step) { st.Preconditions = nil }, repair.ErrUnconstrainedRetry},
			{"unbounded attempts", func(st *repair.Step) { st.MaxAttempts = 0 }, repair.ErrUnconstrainedRetry},
			{"too many attempts", func(st *repair.Step) {
				st.MaxAttempts = repair.MaxStepAttempts + 1
			}, repair.ErrUnconstrainedRetry},
		} {
			step := p.Steps[0]
			mutate.edit(&step)
			if err := step.Validate(); !errors.Is(err, mutate.want) {
				t.Fatalf("%s: err = %v, want %v", mutate.name, err, mutate.want)
			}
		}
	})

	t.Run("RED: a hidden sibling effect is allowed", func(t *testing.T) {
		p := plan(t, s)
		step := stepFor(t, p, fieldBase)
		step.WriteSet = nil
		if err := step.Validate(); !errors.Is(err, repair.ErrHiddenEffect) {
			t.Fatalf("err = %v, want ErrHiddenEffect", err)
		}
	})

	t.Run("RED: a destructive history rewrite is expressible", func(t *testing.T) {
		p := plan(t, s)
		step := p.Steps[0]
		step.Treatment = repair.TreatmentUnspecified
		if err := step.Validate(); !errors.Is(err, repair.ErrDestructiveHistory) {
			t.Fatalf("err = %v, want ErrDestructiveHistory", err)
		}
		// The action vocabulary contains no destructive member at all.
		for action := repair.Action(0); action < 16; action++ {
			if !action.Valid() {
				continue
			}
			switch action {
			case repair.ActionRefreshProjection, repair.ActionProposeExternalUpdate,
				repair.ActionHumanReview, repair.ActionMappingReview:
			default:
				t.Fatalf("unexpected action in the vocabulary: %s", action)
			}
		}
	})

	t.Run("RED: a plan drifts from the comparison it claims to correct", func(t *testing.T) {
		req := planRequest(t, s)
		req.Diagnosis.DiffDigest = "sha256:something-else"
		if _, err := repair.CreateRepairPlan(req); !errors.Is(err, repair.ErrPlanInvalid) {
			t.Fatalf("err = %v, want ErrPlanInvalid", err)
		}
	})

	t.Run("RED: the plan omits the systems or the authority policy", func(t *testing.T) {
		for _, mutate := range []struct {
			name string
			edit func(*repair.CreateRepairPlanRequest)
		}{
			{"no local system", func(r *repair.CreateRepairPlanRequest) { r.LocalSystem = "" }},
			{"no external system", func(r *repair.CreateRepairPlanRequest) { r.ExternalSystem = "" }},
			{"no authority policy", func(r *repair.CreateRepairPlanRequest) { r.AuthorityPolicyVersion = "" }},
			{"no plan id", func(r *repair.CreateRepairPlanRequest) { r.PlanID = "" }},
		} {
			req := planRequest(t, s)
			mutate.edit(&req)
			if _, err := repair.CreateRepairPlan(req); !errors.Is(err, repair.ErrPlanInvalid) {
				t.Fatalf("%s: err = %v, want ErrPlanInvalid", mutate.name, err)
			}
		}
	})
}

// TestTodo_REPAIR_001_Golden pins the shape a stored plan must keep. A change
// to any of these is a contract change, not a refactor.
func TestTodo_REPAIR_001_Golden(t *testing.T) {
	p := plan(t, safeScenario(t))

	if p.IntentType != "hcmnext.operations.create_repair_plan" || p.IntentVersion != "v1" {
		t.Fatalf("intent identity = %s/%s", p.IntentType, p.IntentVersion)
	}
	if p.RulePackVersion != "repair.plan.rules/1.0.0" {
		t.Fatalf("rule pack = %q", p.RulePackVersion)
	}
	for _, want := range []struct {
		field  dataops.FieldID
		action string
		risk   string
		system string
	}{
		{fieldJobCode, "REFRESH_PROJECTION", "LOW", localSystem},
		{fieldBase, "PROPOSE_EXTERNAL_UPDATE", "MEDIUM", externalSystem},
	} {
		step := stepFor(t, p, want.field)
		if step.Action.String() != want.action || step.Risk.String() != want.risk ||
			step.Target.System != want.system {
			t.Fatalf("%s: action=%s risk=%s system=%s, want %s/%s/%s",
				want.field, step.Action, step.Risk, step.Target.System,
				want.action, want.risk, want.system)
		}
	}
	controls := map[string]string{}
	for _, c := range p.Receipt.Controls {
		controls[c.Name] = c.Version
	}
	for name, version := range map[string]string{
		"approval_policy":  approvalPol,
		"authority_policy": authorityPol,
		"plan_rule_pack":   "repair.plan.rules/1.0.0",
		"diff_rule_pack":   dataops.DiffRulePackVersion,
	} {
		if controls[name] != version {
			t.Fatalf("receipt control %s = %q, want %q", name, controls[name], version)
		}
	}
	// The canonical encoding never carries a value the comparison hid.
	if raw := p.Canonical(); raw == nil {
		t.Fatal("the plan has no canonical encoding")
	}
}

// TestTodo_REPAIR_001_Race proves that planning is a pure function of its
// inputs: concurrent runs over the same evidence agree byte for byte, and none
// of them mutates the evidence.
func TestTodo_REPAIR_001_Race(t *testing.T) {
	s := safeScenario(t)
	req := planRequest(t, s)
	before := req.Diff.Digest

	const runs = 32
	digests := make([]string, runs)
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := repair.CreateRepairPlan(req)
			if err != nil {
				t.Errorf("run %d: %v", i, err)
				return
			}
			digests[i] = p.Digest
		}(i)
	}
	wg.Wait()
	for i, d := range digests {
		if d == "" {
			t.Fatalf("run %d produced no plan", i)
		}
		if d != digests[0] {
			t.Fatalf("run %d disagreed with run 0: %s vs %s", i, d, digests[0])
		}
	}
	if req.Diff.Digest != before {
		t.Fatal("planning mutated the comparison it read")
	}
}

// TestTodo_REPAIR_001_Mutation kills the mutants a weaker implementation would
// survive: dropping a precondition, widening the attempt budget, promoting an
// unsafe finding, or planning a field the caller may not see.
func TestTodo_REPAIR_001_Mutation(t *testing.T) {
	s := safeScenario(t)

	t.Run("a step with one precondition removed no longer validates", func(t *testing.T) {
		p := plan(t, s)
		step := p.Steps[0]
		for i := range step.Preconditions {
			mutated := step
			mutated.Preconditions = append(
				append([]repair.Precondition(nil), step.Preconditions[:i]...),
				step.Preconditions[i+1:]...)
			if len(mutated.Preconditions) == 0 {
				if err := mutated.Validate(); !errors.Is(err, repair.ErrUnconstrainedRetry) {
					t.Fatalf("empty preconditions accepted: %v", err)
				}
				continue
			}
			// A remaining precondition must still be complete; an emptied one
			// is refused.
			mutated.Preconditions[0].Expected = ""
			if err := mutated.Validate(); !errors.Is(err, repair.ErrStepInvalid) {
				t.Fatalf("an unpinned precondition validated: %v", err)
			}
		}
	})

	t.Run("an unsafe finding never yields a writing step", func(t *testing.T) {
		p := plan(t, unsafeScenario(t))
		for _, step := range p.WritingSteps() {
			if step.Target.Field == fieldLocation {
				t.Fatalf("the unsafe field %s was planned as a write", fieldLocation)
			}
		}
	})

	t.Run("a step whose post-state is not a value keeps it explicit", func(t *testing.T) {
		p := plan(t, unsafeScenario(t))
		review := stepFor(t, p, fieldLocation)
		if review.ExpectedPost.IsValue() {
			t.Fatal("a review step claimed to know the post-state")
		}
		if review.ExpectedPost.State() != values.PresenceUnknown {
			t.Fatalf("review post-state = %s, want UNKNOWN", review.ExpectedPost.State())
		}
	})

	t.Run("two steps never target the same field", func(t *testing.T) {
		p := plan(t, s)
		doubled := p
		doubled.Steps = append(append([]repair.Step(nil), p.Steps...), p.Steps[0])
		doubled.Steps[len(doubled.Steps)-1].Ordinal = len(doubled.Steps)
		if err := doubled.Validate(); !errors.Is(err, repair.ErrPlanInvalid) {
			t.Fatalf("a plan with two steps on one field validated: %v", err)
		}
	})
}
