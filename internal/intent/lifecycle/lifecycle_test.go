package lifecycle_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

func goldenJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden %s: %v", name, err)
	}
	b = append(b, '\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(b, want) {
		t.Fatalf("golden %s drifted\n got:\n%s\nwant:\n%s", path, b, want)
	}
}

func at(t *testing.T) values.Instant {
	t.Helper()
	return values.NewInstant(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
}

// legal is a tuple that satisfies all six rules for a persisted instance with
// no approval requirement.
func legal() lifecycle.Dimensions {
	return lifecycle.Dimensions{
		Request:     lifecycle.RequestDraft,
		Execution:   lifecycle.ExecutionNotPlanned,
		Business:    lifecycle.BusinessNotStarted,
		Consistency: lifecycle.ConsistencyNotApplicable,
		Obligation:  lifecycle.ObligationNotApplicable,
	}
}

func persisted() lifecycle.Context { return lifecycle.Context{Persisted: true} }

// TestTodo_INTENT_003 is the PRIMARY test for the five-dimension lifecycle and
// its six fixed legality rules.
//
// RED: an illegal tuple under the six rules, a collapsed universal status, a
// sixth dimension, or the loss of a prior dimension returns a typed violation
// and mutates nothing.
//
// GREEN: the five dimensions append independently; proposal revisions, approval
// bindings, closure records, incidents and outcome tracking are linked records
// rather than dimensions; P1A never leaves ExecutionState NOT_PLANNED.
func TestTodo_INTENT_003(t *testing.T) {
	t.Run("there are exactly five dimensions and no universal status", func(t *testing.T) {
		typ := reflect.TypeOf(lifecycle.Dimensions{})
		if typ.NumField() != 5 {
			t.Fatalf("Dimensions has %d fields; the kernel carries exactly five dimensions",
				typ.NumField())
		}
		want := map[string]bool{
			"Request": true, "Execution": true, "Business": true,
			"Consistency": true, "Obligation": true,
		}
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			if !want[name] {
				t.Fatalf("Dimensions carries an undeclared field %q; a sixth dimension is a "+
					"scope exchange, not a struct field", name)
			}
			delete(want, name)
		}
		if len(want) != 0 {
			t.Fatalf("Dimensions is missing %v", want)
		}
		// A collapsed universal status is what the five dimensions exist to
		// prevent, so no field or method may offer one.
		for _, banned := range []string{"Status", "State", "Resolved", "Overall"} {
			if _, ok := typ.FieldByName(banned); ok {
				t.Fatalf("Dimensions carries a collapsed %q field", banned)
			}
		}
		for _, banned := range []string{"Status", "Resolved", "Overall"} {
			if _, ok := typ.MethodByName(banned); ok {
				t.Fatalf("Dimensions exposes a collapsed %q method", banned)
			}
		}
		// State(dimension) is an accessor, not a collapsed status: it requires
		// the caller to name which of the five it wants, so it cannot stand in
		// for the set.
		if m, ok := typ.MethodByName("State"); ok && m.Type.NumIn() != 2 {
			t.Fatalf("Dimensions.State takes no dimension argument; that is a collapsed status")
		}
		if got := len(lifecycle.AllDimensions()); got != 5 {
			t.Fatalf("AllDimensions returns %d entries", got)
		}
	})

	t.Run("there are exactly six legality rules", func(t *testing.T) {
		rules := lifecycle.Rules()
		if len(rules) != 6 {
			t.Fatalf("Rules() returns %d rules; the kernel rule set is fixed at six", len(rules))
		}
		seen := map[lifecycle.Rule]bool{}
		for _, r := range rules {
			if seen[r] {
				t.Fatalf("rule %q is listed twice", r)
			}
			seen[r] = true
		}
	})

	t.Run("RED", func(t *testing.T) {
		digest := "material-digest-1"
		cases := []struct {
			name string
			dims lifecycle.Dimensions
			ctx  lifecycle.Context
			rule lifecycle.Rule
		}{
			{
				name: "CANCELLED entering EXECUTING",
				dims: lifecycle.Dimensions{
					Request: lifecycle.RequestCancelled, Execution: lifecycle.ExecutionExecuting,
					Business: lifecycle.BusinessInProgress, Consistency: lifecycle.ConsistencyNotApplicable,
					Obligation: lifecycle.ObligationNotApplicable,
				},
				ctx:  persisted(),
				rule: lifecycle.RuleTerminalCannotExecute,
			},
			{
				name: "SUPERSEDED entering EXECUTING",
				dims: lifecycle.Dimensions{
					Request: lifecycle.RequestSuperseded, Execution: lifecycle.ExecutionExecuting,
					Business: lifecycle.BusinessInProgress, Consistency: lifecycle.ConsistencyNotApplicable,
					Obligation: lifecycle.ObligationNotApplicable,
				},
				ctx:  persisted(),
				rule: lifecycle.RuleTerminalCannotExecute,
			},
			{
				name: "APPROVED with no binding for the current revision",
				dims: lifecycle.Dimensions{
					Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionNotPlanned,
					Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyNotApplicable,
					Obligation: lifecycle.ObligationNotApplicable,
				},
				ctx: lifecycle.Context{
					Persisted: true, ApprovalRequired: true, CurrentMaterialDigest: digest,
					Bindings: []lifecycle.ApprovalBindingState{{
						RequirementID: "req-1", RequirementLevel: true, Approved: true,
						ApprovedProposalDigest: digest, Invalidated: true,
					}},
				},
				rule: lifecycle.RuleApprovedRequiresBinding,
			},
			{
				name: "a binding still standing against a superseded material digest",
				dims: legal(),
				ctx: lifecycle.Context{
					Persisted: true, CurrentMaterialDigest: "material-digest-2",
					Bindings: []lifecycle.ApprovalBindingState{{
						RequirementID: "req-1", RequirementLevel: true, Approved: true,
						ApprovedProposalDigest: digest,
					}},
				},
				rule: lifecycle.RuleMaterialRevisionInvalidatesApproval,
			},
			{
				name: "COMMITTED without a receipt",
				dims: lifecycle.Dimensions{
					Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted,
					Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent,
					Obligation: lifecycle.ObligationSatisfied,
				},
				ctx:  lifecycle.Context{Persisted: true},
				rule: lifecycle.RuleCommittedRequiresReceipt,
			},
			{
				name: "REPAIR_REQUIRED without a repair plan or incident",
				dims: lifecycle.Dimensions{
					Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionRepairRequired,
					Business: lifecycle.BusinessUnknown, Consistency: lifecycle.ConsistencyDegraded,
					Obligation: lifecycle.ObligationNotApplicable,
				},
				ctx:  lifecycle.Context{Persisted: true},
				rule: lifecycle.RuleCommittedRequiresReceipt,
			},
			{
				name: "CLOSED with ObligationState PENDING",
				dims: lifecycle.Dimensions{
					Request: lifecycle.RequestClosed, Execution: lifecycle.ExecutionNotPlanned,
					Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyNotApplicable,
					Obligation: lifecycle.ObligationPending,
				},
				ctx:  persisted(),
				rule: lifecycle.RuleClosureRequiresObligations,
			},
			{
				name: "CLOSED while consistency is still REPAIRING",
				dims: lifecycle.Dimensions{
					Request: lifecycle.RequestClosed, Execution: lifecycle.ExecutionNotPlanned,
					Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyRepairing,
					Obligation: lifecycle.ObligationSatisfied,
				},
				ctx:  persisted(),
				rule: lifecycle.RuleClosureRequiresObligations,
			},
			{
				name: "UNSPECIFIED on a persisted active instance",
				dims: lifecycle.Dimensions{
					Request: lifecycle.RequestDraft, Execution: lifecycle.ExecutionNotPlanned,
					Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyNotApplicable,
					Obligation: lifecycle.ObligationUnspecified,
				},
				ctx:  persisted(),
				rule: lifecycle.RuleUnspecifiedInvalid,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := lifecycle.Check(tc.dims, tc.ctx)
				if err == nil {
					t.Fatalf("illegal tuple accepted: %s", tc.dims)
				}
				if !errors.Is(err, lifecycle.ErrIllegalTuple) {
					t.Fatalf("error %v is not a typed illegal-tuple violation", err)
				}
				var vs *lifecycle.ViolationSet
				if !errors.As(err, &vs) {
					t.Fatalf("error %v is not a *ViolationSet", err)
				}
				if !vs.Has(tc.rule) {
					t.Fatalf("violation set %v does not name rule %q", vs.Violations, tc.rule)
				}
			})
		}

		t.Run("a transition that loses a prior dimension is rejected with no mutation", func(t *testing.T) {
			m, err := lifecycle.NewMachine(nil, legal(), persisted())
			if err != nil {
				t.Fatalf("new machine: %v", err)
			}
			before := m.Current()
			lost := before
			lost.Business = lifecycle.BusinessUnspecified
			err = m.Apply(lost, persisted(), lifecycle.TransitionRecord{
				At: at(t), Actor: "principal:1", RetentionClass: "INTENT_LIFECYCLE",
			})
			if !errors.Is(err, lifecycle.ErrDimensionLoss) {
				t.Fatalf("a lost dimension was accepted: %v", err)
			}
			if m.Current() != before {
				t.Fatalf("machine mutated on a rejected transition: %v", m.Current())
			}
			if len(m.History()) != 0 {
				t.Fatalf("a rejected transition appended %d record(s)", len(m.History()))
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		t.Run("dimensions append independently", func(t *testing.T) {
			// Committed runtime, incomplete business outcome, unobserved
			// external state and a pending obligation are simultaneously true
			// and simultaneously legal: no single status could express this.
			dims := lifecycle.Dimensions{
				Request:     lifecycle.RequestApproved,
				Execution:   lifecycle.ExecutionCommitted,
				Business:    lifecycle.BusinessInProgress,
				Consistency: lifecycle.ConsistencyPendingObservation,
				Obligation:  lifecycle.ObligationPending,
			}
			ctx := lifecycle.Context{
				Persisted:             true,
				ApprovalRequired:      true,
				CurrentMaterialDigest: "d1",
				CommitReceiptRef:      "receipt:1",
				Bindings: []lifecycle.ApprovalBindingState{{
					RequirementID: "req-1", RequirementLevel: true,
					Approved: true, ApprovedProposalDigest: "d1",
				}},
			}
			if err := lifecycle.Check(dims, ctx); err != nil {
				t.Fatalf("a legitimate divergent tuple was rejected: %v", err)
			}
		})

		t.Run("P1A never leaves ExecutionState NOT_PLANNED", func(t *testing.T) {
			m, err := lifecycle.NewMachine(nil, legal(), persisted())
			if err != nil {
				t.Fatalf("new machine: %v", err)
			}
			steps := []lifecycle.RequestState{
				lifecycle.RequestPreflighted,
				lifecycle.RequestSimulated,
				lifecycle.RequestSubmitted,
			}
			for _, next := range steps {
				dims := m.Current()
				dims.Request = next
				if next == lifecycle.RequestPreflighted {
					dims.Business = lifecycle.BusinessInProgress
				}
				if err := m.Apply(dims, persisted(), lifecycle.TransitionRecord{
					At: at(t), Actor: "principal:1", ReasonRef: "reason.p1a/v1",
					RetentionClass: "INTENT_LIFECYCLE",
				}); err != nil {
					t.Fatalf("advance to %s: %v", next, err)
				}
				if m.Current().Execution != lifecycle.ExecutionNotPlanned {
					t.Fatalf("ExecutionState left NOT_PLANNED in P1A: %s", m.Current().Execution)
				}
			}
			if got := len(m.History()); got != len(steps) {
				t.Fatalf("history has %d records for %d transitions", got, len(steps))
			}
		})

		t.Run("linked records are not dimensions", func(t *testing.T) {
			ctxType := reflect.TypeOf(lifecycle.Context{})
			// Approvals, receipts and repair links live on the context as
			// records the rules read, never as a sixth dimension.
			for _, field := range []string{"Bindings", "CommitReceiptRef", "RepairRef"} {
				if _, ok := ctxType.FieldByName(field); !ok {
					t.Fatalf("Context has no %q; linked records must be readable by the rules", field)
				}
			}
			if reflect.TypeOf(lifecycle.Dimensions{}).NumField() != 5 {
				t.Fatalf("a linked record leaked into Dimensions")
			}
		})
	})
}

// TestTodo_INTENT_003_Security proves the rules cannot be talked out of an
// approval: an approval is only valid for the exact current material digest,
// from a requirement-level binding that was not invalidated and was actually an
// approval rather than a rejection or abstention.
func TestTodo_INTENT_003_Security(t *testing.T) {
	approved := lifecycle.Dimensions{
		Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionNotPlanned,
		Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyNotApplicable,
		Obligation: lifecycle.ObligationNotApplicable,
	}
	const current = "material-current"

	forgeries := []struct {
		name    string
		binding lifecycle.ApprovalBindingState
	}{
		{"advisory rather than requirement-level", lifecycle.ApprovalBindingState{
			RequirementID: "r", RequirementLevel: false, Approved: true,
			ApprovedProposalDigest: current,
		}},
		{"a rejection presented as an approval", lifecycle.ApprovalBindingState{
			RequirementID: "r", RequirementLevel: true, Approved: false,
			ApprovedProposalDigest: current,
		}},
		{"an already-invalidated binding", lifecycle.ApprovalBindingState{
			RequirementID: "r", RequirementLevel: true, Approved: true,
			ApprovedProposalDigest: current, Invalidated: true,
		}},
		{"a binding with an empty digest", lifecycle.ApprovalBindingState{
			RequirementID: "r", RequirementLevel: true, Approved: true,
		}},
	}
	for _, tc := range forgeries {
		t.Run(tc.name, func(t *testing.T) {
			ctx := lifecycle.Context{
				Persisted: true, ApprovalRequired: true, CurrentMaterialDigest: current,
				Bindings: []lifecycle.ApprovalBindingState{tc.binding},
			}
			err := lifecycle.Check(approved, ctx)
			if err == nil {
				t.Fatalf("APPROVED accepted on the strength of %s", tc.name)
			}
			var vs *lifecycle.ViolationSet
			if !errors.As(err, &vs) {
				t.Fatalf("error %v is not a *ViolationSet", err)
			}
			if !vs.Has(lifecycle.RuleApprovedRequiresBinding) &&
				!vs.Has(lifecycle.RuleMaterialRevisionInvalidatesApproval) {
				t.Fatalf("violations %v name neither approval rule", vs.Violations)
			}
		})
	}

	t.Run("approval not required needs no binding", func(t *testing.T) {
		ctx := lifecycle.Context{Persisted: true, ApprovalRequired: false}
		if err := lifecycle.Check(approved, ctx); err != nil {
			t.Fatalf("a definition declaring approval not required was still forced to have one: %v", err)
		}
	})
}

// TestTodo_INTENT_003_Mutation perturbs the rule set's inputs one at a time and
// requires each perturbation to be caught. A rule that never fires is not a
// rule.
func TestTodo_INTENT_003_Mutation(t *testing.T) {
	base := lifecycle.Dimensions{
		Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted,
		Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent,
		Obligation: lifecycle.ObligationSatisfied,
	}
	baseCtx := lifecycle.Context{
		Persisted: true, ApprovalRequired: true, CurrentMaterialDigest: "d1",
		CommitReceiptRef: "receipt:1",
		Bindings: []lifecycle.ApprovalBindingState{{
			RequirementID: "req-1", RequirementLevel: true, Approved: true,
			ApprovedProposalDigest: "d1",
		}},
	}
	if err := lifecycle.Check(base, baseCtx); err != nil {
		t.Fatalf("the baseline tuple must be legal: %v", err)
	}

	mutations := []struct {
		name string
		dims func(lifecycle.Dimensions) lifecycle.Dimensions
		ctx  func(lifecycle.Context) lifecycle.Context
		rule lifecycle.Rule
	}{
		{
			name: "cancel the request while it is executing",
			dims: func(d lifecycle.Dimensions) lifecycle.Dimensions {
				d.Request = lifecycle.RequestCancelled
				d.Execution = lifecycle.ExecutionExecuting
				return d
			},
			rule: lifecycle.RuleTerminalCannotExecute,
		},
		{
			name: "remove the approval binding",
			ctx: func(c lifecycle.Context) lifecycle.Context {
				c.Bindings = nil
				return c
			},
			rule: lifecycle.RuleApprovedRequiresBinding,
		},
		{
			name: "move the material digest without invalidating the binding",
			ctx: func(c lifecycle.Context) lifecycle.Context {
				c.CurrentMaterialDigest = "d2"
				return c
			},
			rule: lifecycle.RuleMaterialRevisionInvalidatesApproval,
		},
		{
			name: "remove the commit receipt",
			ctx: func(c lifecycle.Context) lifecycle.Context {
				c.CommitReceiptRef = ""
				return c
			},
			rule: lifecycle.RuleCommittedRequiresReceipt,
		},
		{
			name: "close with an outstanding obligation",
			dims: func(d lifecycle.Dimensions) lifecycle.Dimensions {
				d.Request = lifecycle.RequestClosed
				d.Obligation = lifecycle.ObligationOverdue
				return d
			},
			rule: lifecycle.RuleClosureRequiresObligations,
		},
		{
			name: "blank one dimension",
			dims: func(d lifecycle.Dimensions) lifecycle.Dimensions {
				d.Consistency = lifecycle.ConsistencyUnspecified
				return d
			},
			rule: lifecycle.RuleUnspecifiedInvalid,
		},
	}

	fired := map[lifecycle.Rule]bool{}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			dims, ctx := base, baseCtx
			if m.dims != nil {
				dims = m.dims(dims)
			}
			if m.ctx != nil {
				ctx = m.ctx(ctx)
			}
			err := lifecycle.Check(dims, ctx)
			if err == nil {
				t.Fatalf("mutation survived the rule set")
			}
			var vs *lifecycle.ViolationSet
			if !errors.As(err, &vs) {
				t.Fatalf("error %v is not a *ViolationSet", err)
			}
			if !vs.Has(m.rule) {
				t.Fatalf("mutation reported %v, expected rule %q", vs.Violations, m.rule)
			}
			fired[m.rule] = true
		})
	}
	for _, rule := range lifecycle.Rules() {
		if !fired[rule] {
			t.Fatalf("rule %q was never exercised by a mutation", rule)
		}
	}
}

// TestTodo_MODEL_014 is the PRIMARY test for lifecycle definition validation.
//
// RED: the checker rejects an undeclared transition, an unreachable state, an
// in-place historical mutation and a transition that bypasses governance or
// retention.
//
// GREEN: every authoritative transition appends a LifecycleTransitionRecord and
// lifecycle assignment resolves canonically.
func TestTodo_MODEL_014(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		t.Run("unreachable state", func(t *testing.T) {
			p := lifecycle.Profile{
				ID: "test.unreachable/v1", Dimension: lifecycle.DimensionRequest,
				States:  []lifecycle.StateID{"DRAFT", "PREFLIGHTED", "ORPHAN"},
				Initial: "DRAFT",
				Transitions: []lifecycle.TransitionRule{
					{From: "DRAFT", To: "PREFLIGHTED", RetentionClass: "R"},
				},
			}
			err := p.Validate()
			if !errors.Is(err, lifecycle.ErrInvalidProfile) {
				t.Fatalf("an unreachable state was accepted: %v", err)
			}
			if !strings.Contains(err.Error(), "ORPHAN") {
				t.Fatalf("the error does not name the unreachable state: %v", err)
			}
		})

		t.Run("transition referencing an undeclared state", func(t *testing.T) {
			p := lifecycle.Profile{
				ID: "test.undeclared/v1", Dimension: lifecycle.DimensionRequest,
				States:  []lifecycle.StateID{"DRAFT"},
				Initial: "DRAFT",
				Transitions: []lifecycle.TransitionRule{
					{From: "DRAFT", To: "NOWHERE", RetentionClass: "R"},
				},
			}
			if err := p.Validate(); !errors.Is(err, lifecycle.ErrInvalidProfile) {
				t.Fatalf("a transition to an undeclared state was accepted: %v", err)
			}
		})

		t.Run("transition with no retention class", func(t *testing.T) {
			p := lifecycle.Profile{
				ID: "test.no-retention/v1", Dimension: lifecycle.DimensionRequest,
				States:      []lifecycle.StateID{"DRAFT", "PREFLIGHTED"},
				Initial:     "DRAFT",
				Transitions: []lifecycle.TransitionRule{{From: "DRAFT", To: "PREFLIGHTED"}},
			}
			if err := p.Validate(); !errors.Is(err, lifecycle.ErrInvalidProfile) {
				t.Fatalf("a transition with no retention class was accepted: %v", err)
			}
		})

		t.Run("outgoing transition from a terminal state", func(t *testing.T) {
			p := lifecycle.Profile{
				ID: "test.terminal/v1", Dimension: lifecycle.DimensionRequest,
				States:   []lifecycle.StateID{"DRAFT", "CANCELLED", "PREFLIGHTED"},
				Initial:  "DRAFT",
				Terminal: []lifecycle.StateID{"CANCELLED"},
				Transitions: []lifecycle.TransitionRule{
					{From: "DRAFT", To: "CANCELLED", RetentionClass: "R"},
					{From: "CANCELLED", To: "PREFLIGHTED", RetentionClass: "R"},
				},
			}
			if err := p.Validate(); !errors.Is(err, lifecycle.ErrInvalidProfile) {
				t.Fatalf("a terminal state with an outgoing edge was accepted: %v", err)
			}
		})

		t.Run("undeclared transition at apply time", func(t *testing.T) {
			m, err := lifecycle.NewMachine(nil, legal(), persisted())
			if err != nil {
				t.Fatalf("new machine: %v", err)
			}
			next := m.Current()
			// DRAFT -> APPROVED is not a declared kernel edge.
			next.Request = lifecycle.RequestApproved
			err = m.Apply(next, persisted(), lifecycle.TransitionRecord{
				At: at(t), RetentionClass: "INTENT_LIFECYCLE",
			})
			if !errors.Is(err, lifecycle.ErrUndeclaredTransition) {
				t.Fatalf("an undeclared transition was applied: %v", err)
			}
			if m.Current() != legal() {
				t.Fatalf("machine moved on a rejected transition")
			}
		})

		t.Run("in-place historical mutation", func(t *testing.T) {
			var h lifecycle.History
			first := lifecycle.TransitionRecord{
				Sequence: 1, From: legal(), To: legal(), At: at(t), RetentionClass: "R",
			}
			if err := h.Append(first); err != nil {
				t.Fatalf("append first record: %v", err)
			}
			// Rewriting sequence 1 is refused.
			if err := h.Append(first); !errors.Is(err, lifecycle.ErrHistoryImmutable) {
				t.Fatalf("history accepted a rewrite of sequence 1: %v", err)
			}
			// Skipping a sequence is refused.
			skip := first
			skip.Sequence = 3
			if err := h.Append(skip); !errors.Is(err, lifecycle.ErrHistoryImmutable) {
				t.Fatalf("history accepted a skipped sequence: %v", err)
			}
			// A record that does not continue the last recorded To is refused.
			discontinuous := first
			discontinuous.Sequence = 2
			discontinuous.From = lifecycle.Dimensions{Request: lifecycle.RequestClosed}
			if err := h.Append(discontinuous); !errors.Is(err, lifecycle.ErrHistoryImmutable) {
				t.Fatalf("history accepted a discontinuous record: %v", err)
			}
			// Mutating a returned copy must not reach the history.
			records := h.Records()
			records[0].Actor = "clobbered"
			if got, _ := h.Latest(); got.Actor == "clobbered" {
				t.Fatalf("history was mutated through a returned copy")
			}
			if h.Len() != 1 {
				t.Fatalf("history length changed to %d after rejected appends", h.Len())
			}
		})

		t.Run("transition bypassing governance or retention", func(t *testing.T) {
			// SUBMITTED -> APPROVED requires a governance decision reference.
			start := lifecycle.Dimensions{
				Request: lifecycle.RequestSubmitted, Execution: lifecycle.ExecutionNotPlanned,
				Business: lifecycle.BusinessInProgress, Consistency: lifecycle.ConsistencyNotApplicable,
				Obligation: lifecycle.ObligationNotApplicable,
			}
			ctx := lifecycle.Context{Persisted: true}
			m, err := lifecycle.NewMachine(nil, start, ctx)
			if err != nil {
				t.Fatalf("new machine: %v", err)
			}
			next := start
			next.Request = lifecycle.RequestApproved
			err = m.Apply(next, ctx, lifecycle.TransitionRecord{
				At: at(t), RetentionClass: "INTENT_LIFECYCLE",
			})
			if !errors.Is(err, lifecycle.ErrGovernanceBypassed) {
				t.Fatalf("a governed transition was applied with no governance decision: %v", err)
			}

			var h lifecycle.History
			if err := h.Append(lifecycle.TransitionRecord{
				Sequence: 1, From: start, To: next, At: at(t),
			}); !errors.Is(err, lifecycle.ErrGovernanceBypassed) {
				t.Fatalf("a record with no retention class was appended: %v", err)
			}
		})

		t.Run("a domain profile may not widen the kernel", func(t *testing.T) {
			base := lifecycle.KernelProfiles()[lifecycle.DimensionRequest]
			widened := base
			widened.ID = "domain.widened/v1"
			widened.Transitions = append(append([]lifecycle.TransitionRule(nil), base.Transitions...),
				lifecycle.TransitionRule{From: "DRAFT", To: "CLOSED", RetentionClass: "R"})
			if err := widened.Narrows(base); !errors.Is(err, lifecycle.ErrProfileWiden) {
				t.Fatalf("a widened domain profile was accepted: %v", err)
			}
			narrowed := base
			narrowed.ID = "domain.narrowed/v1"
			narrowed.Transitions = base.Transitions[:3]
			if err := narrowed.Narrows(base); err != nil {
				t.Fatalf("a legitimate narrowing was rejected: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		t.Run("every kernel profile validates and resolves canonically", func(t *testing.T) {
			profiles := lifecycle.KernelProfiles()
			if len(profiles) != 5 {
				t.Fatalf("kernel publishes %d lifecycle profiles, want 5", len(profiles))
			}
			for _, dim := range lifecycle.AllDimensions() {
				p, ok := profiles[dim]
				if !ok {
					t.Fatalf("no kernel profile for %s", dim)
				}
				if err := p.Validate(); err != nil {
					t.Fatalf("kernel profile %s: %v", p.ID, err)
				}
				// The profile's state list must be exactly the dimension's
				// declared states: a profile is not free to invent one.
				declared := lifecycle.StatesOf(dim)
				if len(declared) != len(p.States) {
					t.Fatalf("%s declares %d states, the dimension has %d",
						p.ID, len(p.States), len(declared))
				}
				known := map[lifecycle.StateID]bool{}
				for _, s := range declared {
					known[s] = true
				}
				for _, s := range p.States {
					if !known[s] {
						t.Fatalf("%s declares state %q that %s does not have", p.ID, s, dim)
					}
				}
			}
		})

		t.Run("every authoritative transition appends a record", func(t *testing.T) {
			m, err := lifecycle.NewMachine(nil, legal(), persisted())
			if err != nil {
				t.Fatalf("new machine: %v", err)
			}
			next := m.Current()
			next.Request = lifecycle.RequestPreflighted
			rec := lifecycle.TransitionRecord{
				At: at(t), Actor: "principal:1", ReasonRef: "reason.preflight/v1",
			}
			if err := m.Apply(next, persisted(), rec); err != nil {
				t.Fatalf("apply: %v", err)
			}
			history := m.History()
			if len(history) != 1 {
				t.Fatalf("history has %d records", len(history))
			}
			got := history[0]
			if got.Sequence != 1 || got.From != legal() || got.To != next {
				t.Fatalf("record does not describe the transition: %+v", got)
			}
			if got.RetentionClass == "" {
				t.Fatalf("record inherited no retention class")
			}
		})

		t.Run("replay re-checks every intermediate tuple", func(t *testing.T) {
			m, err := lifecycle.NewMachine(nil, legal(), persisted())
			if err != nil {
				t.Fatalf("new machine: %v", err)
			}
			for _, next := range []lifecycle.RequestState{
				lifecycle.RequestPreflighted, lifecycle.RequestSimulated,
			} {
				dims := m.Current()
				dims.Request = next
				if err := m.Apply(dims, persisted(), lifecycle.TransitionRecord{
					At: at(t), RetentionClass: "INTENT_LIFECYCLE",
				}); err != nil {
					t.Fatalf("apply %s: %v", next, err)
				}
			}
			final, err := lifecycle.Replay(nil, legal(), m.History(),
				func(lifecycle.TransitionRecord) lifecycle.Context { return persisted() })
			if err != nil {
				t.Fatalf("replay: %v", err)
			}
			if final != m.Current() {
				t.Fatalf("replay produced %v, live machine is %v", final, m.Current())
			}

			// A recorded history containing an illegal tuple fails replay
			// rather than being repaired into a preferred status.
			tampered := m.History()
			tampered[len(tampered)-1].To.Request = lifecycle.RequestApproved
			if _, err := lifecycle.Replay(nil, legal(), tampered,
				func(lifecycle.TransitionRecord) lifecycle.Context {
					return lifecycle.Context{Persisted: true, ApprovalRequired: true}
				}); err == nil {
				t.Fatalf("replay silently accepted a tampered history")
			}
		})
	})
}

// TestTodo_MODEL_014_Property asserts profile invariants across all five
// dimensions rather than one hand-picked profile.
func TestTodo_MODEL_014_Property(t *testing.T) {
	for dim, p := range lifecycle.KernelProfiles() {
		if p.Dimension != dim {
			t.Fatalf("profile %s is assigned to %s but declares %s", p.ID, dim, p.Dimension)
		}
		if err := p.Validate(); err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
		// Every declared edge resolves back to itself.
		for _, edge := range p.Transitions {
			rule, ok := p.Rule(edge.From, edge.To)
			if !ok {
				t.Fatalf("%s cannot resolve its own edge %s->%s", p.ID, edge.From, edge.To)
			}
			if rule.RetentionClass == "" {
				t.Fatalf("%s edge %s->%s has no retention class", p.ID, edge.From, edge.To)
			}
		}
		// A profile always narrows itself.
		if err := p.Narrows(p); err != nil {
			t.Fatalf("%s does not narrow itself: %v", p.ID, err)
		}
		// No edge may leave a declared terminal state.
		terminal := map[lifecycle.StateID]bool{}
		for _, s := range p.Terminal {
			terminal[s] = true
		}
		for _, edge := range p.Transitions {
			if terminal[edge.From] {
				t.Fatalf("%s leaves terminal state %s", p.ID, edge.From)
			}
		}
	}
}

// TestTodo_MODEL_014_Golden pins the kernel lifecycle profiles. A state or edge
// added or removed without updating the golden fails here.
func TestTodo_MODEL_014_Golden(t *testing.T) {
	type edge struct {
		From, To  string
		Governed  bool
		Retention string
	}
	type profile struct {
		ID        string
		Dimension string
		States    []string
		Initial   string
		Terminal  []string
		Edges     []edge
	}
	var out []profile
	profiles := lifecycle.KernelProfiles()
	for _, dim := range lifecycle.AllDimensions() {
		p := profiles[dim]
		row := profile{
			ID: p.ID, Dimension: string(p.Dimension), Initial: string(p.Initial),
		}
		for _, s := range p.States {
			row.States = append(row.States, string(s))
		}
		for _, s := range p.Terminal {
			row.Terminal = append(row.Terminal, string(s))
		}
		for _, e := range p.Transitions {
			row.Edges = append(row.Edges, edge{
				From: string(e.From), To: string(e.To),
				Governed: e.RequiresGovernance, Retention: e.RetentionClass,
			})
		}
		out = append(out, row)
	}
	goldenJSON(t, "model_014_profiles.json", out)
}

// FuzzTodo_MODEL_014 fuzzes the transition checker over arbitrary state pairs.
// An applied transition must always be a declared edge, and a rejected one must
// leave the machine untouched.
func FuzzTodo_MODEL_014(f *testing.F) {
	f.Add(uint8(1), uint8(2), uint8(1), uint8(1))
	f.Add(uint8(1), uint8(5), uint8(1), uint8(4))
	f.Add(uint8(8), uint8(4), uint8(1), uint8(4))
	f.Add(uint8(0), uint8(0), uint8(0), uint8(0))
	f.Fuzz(func(t *testing.T, fromReq, toReq, fromExec, toExec uint8) {
		start := lifecycle.Dimensions{
			Request: lifecycle.RequestState(fromReq), Execution: lifecycle.ExecutionState(fromExec),
			Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyNotApplicable,
			Obligation: lifecycle.ObligationNotApplicable,
		}
		ctx := lifecycle.Context{Persisted: true}
		m, err := lifecycle.NewMachine(nil, start, ctx)
		if err != nil {
			// An illegal or undeclared starting tuple must be refused; it must
			// never produce a usable machine.
			if m != nil {
				t.Fatalf("NewMachine returned a machine alongside error %v", err)
			}
			return
		}
		next := lifecycle.Dimensions{
			Request: lifecycle.RequestState(toReq), Execution: lifecycle.ExecutionState(toExec),
			Business: start.Business, Consistency: start.Consistency, Obligation: start.Obligation,
		}
		before := m.Current()
		err = m.Apply(next, ctx, lifecycle.TransitionRecord{
			At: values.NewInstant(time.Unix(1770000000, 0)), RetentionClass: "R",
		})
		if err != nil {
			if m.Current() != before {
				t.Fatalf("a rejected transition mutated the machine")
			}
			if len(m.History()) != 0 {
				t.Fatalf("a rejected transition appended a record")
			}
			return
		}
		if m.Current() != next {
			t.Fatalf("apply succeeded but the machine is at %v, not %v", m.Current(), next)
		}
		if len(m.History()) != 1 {
			t.Fatalf("an applied transition appended %d records", len(m.History()))
		}
		profiles := m.Profiles()
		for _, dim := range lifecycle.AllDimensions() {
			if before.State(dim) == next.State(dim) {
				continue
			}
			if _, ok := profiles[dim].Rule(before.State(dim), next.State(dim)); !ok {
				t.Fatalf("applied an undeclared %s edge %s->%s",
					dim, before.State(dim), next.State(dim))
			}
		}
	})
}
