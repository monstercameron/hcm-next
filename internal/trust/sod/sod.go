// Package sod implements TRUST-014: a pure separation-of-duties evaluator
// over a decision context of requester, approver candidates and (optionally)
// an executor, each identified by their own subject id plus, when they act
// under delegated authority (an acting role, a coverage grant, or an
// ordinary delegation - see [trust.GrantKind]), the chain of subject ids
// that authority passed through.
//
// Evaluate never widens a candidate set: it only removes candidates the
// declared [Constraints] forbid, and it reports exactly which rule removed
// each one. When too few distinct candidates remain to meet the caller's
// quorum, it returns [ErrUnsatisfiable] rather than silently accepting a
// smaller quorum.
//
// # Relationship to internal/humanwork
//
// [internal/humanwork.SeparationConstraints] (see requirement.go) is the
// separation-of-duties contract for an approval requirement's candidate
// resolution. This package mirrors that shape by hand rather than importing
// it: internal/trust is a foundational identity/authority package, and
// internal/humanwork is a business-process package built on top of it, so a
// trust -> humanwork edge would invert that layering (nothing else in
// internal/trust imports internal/humanwork, and a foundational package
// taking a dependency on one business process's forms-and-rules model is
// exactly the kind of edge the platform's boundary policy exists to
// prevent). The field-by-field mapping is:
//
//	humanwork.SeparationConstraints field | sod.Constraints field
//	---------------------------------------|--------------------------------
//	RequesterMayNotApprove                 | RequesterMayNotApprove
//	SubjectMayNotApprove                   | (no analogue: TRUST-014's
//	                                        |  decision context has no
//	                                        |  "subject of record" distinct
//	                                        |  from requester/approver/
//	                                        |  executor)
//	OneRequirementPerPrincipal              | OneApprovalPerPrincipal
//	ForbidRequesterManagerChain             | (no analogue: this package
//	                                        |  evaluates delegation chains,
//	                                        |  not manager-chain facts,
//	                                        |  which belong to a scope
//	                                        |  resolver such as
//	                                        |  internal/trust/authz)
//	RuleID                                  | RuleID
//	(n/a)                                   | RequesterDelegateMayNotApprove
//	                                        |  (delegation-specific: a
//	                                        |  delegate acting under
//	                                        |  authority the requester
//	                                        |  delegated is, in effect, the
//	                                        |  requester)
//	(n/a)                                   | ExecutorMayNotApprove
//	                                        |  (execution-specific: the
//	                                        |  principal who will execute a
//	                                        |  decision cannot also be the
//	                                        |  one who approved it)
//	(n/a)                                   | RequesterMayNotExecute
//	                                        |  (authorship-specific: the
//	                                        |  principal who authored the
//	                                        |  proposal cannot be the one
//	                                        |  who executes it - "the
//	                                        |  repair author executing the
//	                                        |  repair")
//
// A future change to internal/humanwork's shape must be reflected here by
// hand; there is no compiler to catch drift between the two.
package sod

import (
	"errors"
	"fmt"
	"slices"
)

// ErrInvalidConstraints is returned when a [Constraints] value cannot be
// evaluated: no rule id, or no exclusion rule enabled. A separation-of-duties
// contract that constrains nobody is not a contract; it is the caller
// forgetting to declare one.
var ErrInvalidConstraints = errors.New("sod: invalid separation-of-duties constraints")

// ErrInvalidContext is returned when a [DecisionContext] is missing the
// identity information Evaluate needs: an empty requester or approver
// subject.
var ErrInvalidContext = errors.New("sod: invalid decision context")

// ErrUnsatisfiable is returned when, after every separation-of-duties
// exclusion in [Constraints] has been applied, fewer distinct approver
// subjects remain than the caller's quorum requires. It is the SOD_UNSATISFIABLE
// outcome TRUST-014 requires: the evaluator never lowers the quorum to make
// a decision proceed.
var ErrUnsatisfiable = errors.New("sod: SOD_UNSATISFIABLE - quorum cannot be met under separation of duties")

// Constraints is the separation-of-duties contract one [Evaluate] call
// applies. See the package doc comment for how this mirrors
// internal/humanwork's SeparationConstraints shape without importing it.
//
// Every flag is an exclusion rule; RuleID is the explanation every excluded
// candidate is reported against, and is required so a caller can always
// trace an exclusion back to the policy that produced it.
type Constraints struct {
	// RequesterMayNotApprove excludes an approver candidate whose own
	// subject id equals the requester's: the classic "requester approving
	// their own proposal".
	RequesterMayNotApprove bool
	// RequesterDelegateMayNotApprove excludes an approver candidate acting
	// under authority whose delegation chain traces back to the requester:
	// a delegate approving on behalf of the person who raised the request
	// is the requester approving themself at one remove.
	RequesterDelegateMayNotApprove bool
	// ExecutorMayNotApprove excludes an approver candidate whose subject id
	// equals the decision context's executor: the principal who will carry
	// out a decision cannot also be the one who authorized it.
	ExecutorMayNotApprove bool
	// RequesterMayNotExecute rejects a decision context outright (via
	// [Evaluate]'s error return) when the requester's subject id equals the
	// executor's: the author of a proposal executing their own repair.
	// Unlike the other flags, this is not an approver exclusion - there is
	// no approver candidate to drop - so it fails the whole evaluation.
	RequesterMayNotExecute bool
	// OneApprovalPerPrincipal collapses duplicate approver candidates (the
	// same subject id offered more than once) to a single eligible entry,
	// so one person cannot satisfy two distinct approval slots in the same
	// quorum.
	OneApprovalPerPrincipal bool
	// RuleID names the policy rule these constraints implement. It is
	// echoed on every exclusion and on the overall result so a durable
	// evidence record can cite exactly which rule fired.
	RuleID string
}

// Validate rejects a [Constraints] value that cannot be evaluated: no rule
// id, or a contract that excludes nobody and rejects nothing. A stated
// separation-of-duties requirement that constrains no one is a
// configuration mistake, not a permissive policy.
func (c Constraints) Validate() error {
	if c.RuleID == "" {
		return fmt.Errorf("%w: no rule id", ErrInvalidConstraints)
	}
	if !c.RequesterMayNotApprove && !c.RequesterDelegateMayNotApprove &&
		!c.ExecutorMayNotApprove && !c.RequesterMayNotExecute && !c.OneApprovalPerPrincipal {
		return fmt.Errorf("%w: rule %q constrains nobody", ErrInvalidConstraints, c.RuleID)
	}
	return nil
}

// Actor is one principal's decision-relevant identity: its own subject id
// and, when it is acting under delegated authority, the ordered chain of
// subject ids that authority passed through, root delegator first, ending
// with Subject itself.
//
// A principal acting on its own directly held authority leaves
// DelegationChain empty; [Evaluate] treats that the same as a chain
// containing only Subject.
type Actor struct {
	Subject         string
	DelegationChain []string
}

// chain returns the actor's delegation chain, defaulting to a single-element
// chain of just the actor's own subject when none was supplied.
func (a Actor) chain() []string {
	if len(a.DelegationChain) == 0 {
		return []string{a.Subject}
	}
	return a.DelegationChain
}

// delegatesFrom reports whether a's effective authority passed through
// subject anywhere before reaching a itself - i.e. whether a is acting as a
// delegate of subject. It never reports true for subject == a.Subject: that
// is self-identity, not delegation.
func (a Actor) delegatesFrom(subject string) bool {
	if subject == "" {
		return false
	}
	chain := a.chain()
	return slices.Contains(chain[:len(chain)-1], subject)
}

// DecisionContext is everything [Evaluate] needs: the requester, the
// candidate approvers to filter, and (when known) the executor who will
// carry out the decision. Executor is the zero [Actor] when no executor has
// been identified yet; every executor-dependent rule is then a no-op rather
// than an exclusion, since there is nothing to compare against.
type DecisionContext struct {
	Requester Actor
	Approvers []Actor
	Executor  Actor
}

// Exclusion records one approver candidate Evaluate removed and the rule
// that removed it.
type Exclusion struct {
	Subject string
	RuleID  string
	Reason  string
}

// Reason tokens. These are stable, redaction-safe explanations: they name
// which relationship triggered the exclusion, never anything about the
// underlying proposal.
const (
	ReasonSelfApproval       = "self_approval"
	ReasonRequesterDelegate  = "requester_delegate_approval"
	ReasonExecutorIsApprover = "executor_is_approver"
	ReasonDuplicatePrincipal = "duplicate_principal"
)

// Result is the outcome of one [Evaluate] call: the distinct, eligible
// approver subjects that survived every exclusion, in first-seen order, and
// the exclusions that removed everyone else.
type Result struct {
	RuleID    string
	Eligible  []string
	Excluded  []Exclusion
	Satisfied bool
}

// Explain renders a deterministic, redaction-safe one-line summary: which
// rule was evaluated, how many candidates were eligible versus excluded, and
// whether the quorum was met. It never names a specific proposal or subject
// beyond what the caller already supplied through Result's own fields.
func (r Result) Explain() string {
	return fmt.Sprintf("sod decision rule=%s eligible=%d excluded=%d satisfied=%t",
		r.RuleID, len(r.Eligible), len(r.Excluded), r.Satisfied)
}

// Evaluate applies constraints to ctx and returns which approver candidates
// remain eligible after every exclusion rule has run, in the fixed order
// RequesterMayNotApprove, RequesterDelegateMayNotApprove,
// ExecutorMayNotApprove, then OneApprovalPerPrincipal deduplication. Fixed
// order keeps the first-fired rule for a candidate that would match more
// than one deterministic.
//
// It first rejects the whole context, before looking at any approver, when
// RequesterMayNotExecute is set and the requester is the executor: there is
// no approver-level fix for the author of a proposal executing it
// themselves.
//
// quorum is the minimum number of distinct eligible approvers the caller
// requires. Evaluate returns (Result, [ErrUnsatisfiable]) - never a Result
// with a silently lowered quorum - when fewer remain; the returned Result is
// still populated so a caller can explain the refusal.
func Evaluate(ctx DecisionContext, constraints Constraints, quorum int) (Result, error) {
	if err := constraints.Validate(); err != nil {
		return Result{}, err
	}
	if quorum < 1 {
		return Result{}, fmt.Errorf("%w: quorum must be at least 1, got %d", ErrInvalidContext, quorum)
	}
	if ctx.Requester.Subject == "" {
		return Result{}, fmt.Errorf("%w: no requester subject", ErrInvalidContext)
	}
	for _, a := range ctx.Approvers {
		if a.Subject == "" {
			return Result{}, fmt.Errorf("%w: an approver candidate has no subject", ErrInvalidContext)
		}
	}

	if constraints.RequesterMayNotExecute && ctx.Executor.Subject != "" && ctx.Executor.Subject == ctx.Requester.Subject {
		return Result{RuleID: constraints.RuleID}, fmt.Errorf(
			"%w: rule %q: requester %q is also the executor", ErrUnsatisfiable, constraints.RuleID, ctx.Requester.Subject)
	}

	seen := make(map[string]bool, len(ctx.Approvers))
	eligible := make([]string, 0, len(ctx.Approvers))
	var excluded []Exclusion

	exclude := func(subject, reason string) {
		excluded = append(excluded, Exclusion{Subject: subject, RuleID: constraints.RuleID, Reason: reason})
	}

	for _, appr := range ctx.Approvers {
		switch {
		case constraints.RequesterMayNotApprove && appr.Subject == ctx.Requester.Subject:
			exclude(appr.Subject, ReasonSelfApproval)
		case constraints.RequesterDelegateMayNotApprove && appr.delegatesFrom(ctx.Requester.Subject):
			exclude(appr.Subject, ReasonRequesterDelegate)
		case constraints.ExecutorMayNotApprove && ctx.Executor.Subject != "" && appr.Subject == ctx.Executor.Subject:
			exclude(appr.Subject, ReasonExecutorIsApprover)
		case constraints.OneApprovalPerPrincipal && seen[appr.Subject]:
			exclude(appr.Subject, ReasonDuplicatePrincipal)
		default:
			seen[appr.Subject] = true
			eligible = append(eligible, appr.Subject)
		}
	}

	result := Result{
		RuleID:    constraints.RuleID,
		Eligible:  eligible,
		Excluded:  excluded,
		Satisfied: len(eligible) >= quorum,
	}
	if !result.Satisfied {
		return result, fmt.Errorf("%w: rule %q: have %d eligible, need %d", ErrUnsatisfiable, constraints.RuleID, len(eligible), quorum)
	}
	return result, nil
}
