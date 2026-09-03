// Package workitem: this file is WORK-002. [ResolveAssignment] runs the
// existing resolver -- [humanwork.Resolve] over a compiled
// [humanwork.ApprovalRequirement] and a [humanwork.Directory] -- and records
// what it decided. Nothing about candidate selection is reimplemented here.
package workitem

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/monstercameron/hcm-next/internal/humanwork"
)

// ReResolutionTrigger names why a resolution ran. It is part of the recorded
// evidence, not a fact resolution itself can see.
type ReResolutionTrigger string

// Declared triggers.
const (
	// TriggerUnspecified is the zero value and is never legal.
	TriggerUnspecified ReResolutionTrigger = ""
	// TriggerInitialRouting is the first resolution a work item ever gets, run
	// from [Store.Route] against a CREATED item.
	TriggerInitialRouting ReResolutionTrigger = "INITIAL_ROUTING"
	// TriggerReassignment is a re-resolution requested because the previously
	// assigned or available candidate set became unavailable or unauthorized.
	TriggerReassignment ReResolutionTrigger = "REASSIGNMENT"
	// TriggerReturnedReroute is a re-resolution after a RETURNED item goes back
	// through ROUTED.
	TriggerReturnedReroute ReResolutionTrigger = "RETURNED_REROUTE"
	// TriggerEscalatedReroute is a re-resolution after an ESCALATED item goes
	// back through ROUTED, once escalation attention produced a wider policy.
	TriggerEscalatedReroute ReResolutionTrigger = "ESCALATED_REROUTE"
)

var triggerWire = map[ReResolutionTrigger]bool{
	TriggerInitialRouting:   true,
	TriggerReassignment:     true,
	TriggerReturnedReroute:  true,
	TriggerEscalatedReroute: true,
}

// Valid reports whether t is a declared trigger.
func (t ReResolutionTrigger) Valid() bool { return triggerWire[t] }

// Assignment is the whole of WORK-002's recorded evidence: the resolution
// expression and its digest, the surviving candidate set, every exclusion
// with the rule id that produced it, the directory and governance-policy
// versions the answer came from, and the trigger that made this resolution
// run.
//
// Recording an owner grants that owner nothing -- WORK-002's REFACTOR clause.
// There is no authority field here at all: [Assignment.IsCandidate] answers
// "was this principal in the set when it was resolved", which is evidence,
// not a grant. Authority at decision time is re-established by resolving
// again against the directory as it is then.
type Assignment struct {
	// Resolution is [humanwork.Resolve]'s full answer: outcome, candidates,
	// exclusions, expression and requirement digests, directory version,
	// effective and resolved-at instants, and the required quorum.
	Resolution humanwork.Resolution
	// GovernancePolicyRef is the governance configuration version the
	// requirement's compiled deadlines, quorum, separation and escalation came
	// from. It is carried separately from Resolution because a Resolution does
	// not itself retain the requirement's provenance, only its digest.
	GovernancePolicyRef string
	// Trigger is why this resolution ran.
	Trigger ReResolutionTrigger
	// ChosenOwner is the principal [RouteFromAssignment] named directly, set
	// only when resolution produced exactly one candidate. It is empty when
	// the item was left open as a candidate set, or when resolution found
	// nobody at all -- an unfilled slot is never "chosen" to anyone.
	ChosenOwner string
}

// IsZero reports whether a is the not-yet-resolved zero value: a CREATED item
// carries exactly this before [Store.Route] has run once. It is checked by
// field rather than by direct struct comparison because [humanwork.Resolution]
// holds slices, which Go will not compare with ==.
func (a Assignment) IsZero() bool {
	return a.Trigger == TriggerUnspecified &&
		a.GovernancePolicyRef == "" &&
		a.ChosenOwner == "" &&
		a.Resolution.RequirementID == "" &&
		len(a.Resolution.Candidates) == 0 &&
		len(a.Resolution.Excluded) == 0
}

// IsCandidate reports whether principalID was in the surviving candidate set
// when this assignment was resolved. It answers a question about the past
// resolution, not about current authority: a caller deciding whether
// principalID may act now resolves again.
func (a Assignment) IsCandidate(principalID string) bool {
	_, ok := a.Resolution.Authorizes(principalID)
	return ok
}

// Digest returns the assignment's content digest in migration 00017's
// sha256:<hex> shape. Two assignments with the same resolution, policy
// reference, trigger and chosen owner produce the same digest, because
// [humanwork.Resolution]'s own slices are already sorted deterministically by
// the resolver and every field here marshals through encoding/json in fixed
// struct-declaration order.
func (a Assignment) Digest() (string, error) {
	raw, err := marshalAssignment(a)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// marshalAssignment encodes a as JSON for the work_item.assignment column. The
// zero value is encoded as the literal empty object rather than run through
// encoding/json: a zero [humanwork.Resolution] carries unset
// [values.Instant] fields, and their MarshalText refuses to encode "unset"
// at all -- correctly, since an unset instant is not a business fact this
// package should ever claim to have. A CREATED item that has never been
// routed has recorded no resolution instant, which is exactly what the empty
// object says.
func marshalAssignment(a Assignment) ([]byte, error) {
	if a.IsZero() {
		return []byte("{}"), nil
	}
	return json.Marshal(a)
}

// ResolveAssignment runs [humanwork.Resolve] over req and records the answer
// as an [Assignment], attaching req's governance policy reference and the
// caller-stated trigger. It reimplements no part of candidate selection: this
// function is the entire seam between this package and internal/humanwork.
func ResolveAssignment(
	req humanwork.ApprovalRequirement,
	in humanwork.ResolutionInput,
	dir humanwork.Directory,
	clock humanwork.Clock,
	trigger ReResolutionTrigger,
) (Assignment, error) {
	if !trigger.Valid() {
		return Assignment{}, refuse(CodeInvalidRecord, "", "re-resolution trigger %q is not declared", string(trigger))
	}
	res, err := humanwork.Resolve(req, in, dir, clock)
	if err != nil {
		return Assignment{}, wrap(CodeInvalidRecord, "", err, "resolve assignment")
	}
	a := Assignment{
		Resolution:          res,
		GovernancePolicyRef: req.Source.GovernancePolicyRef,
		Trigger:             trigger,
	}
	if res.Outcome == humanwork.OutcomeResolved && len(res.Candidates) == 1 {
		a.ChosenOwner = res.Candidates[0].PrincipalID
	}
	return a, nil
}

// RouteFromAssignment derives the owner kind, owner reference and target
// status a [Store.Route] call moves a work item to, from an already-resolved
// [Assignment]. policyRouteRef is the item's own immutable policy route,
// reused as the owner reference when resolution found nobody: an item that
// cannot be filled is escalated, not stranded, and it is escalated back to
// the same route that produced the attempt.
//
//   - No authorized approver: OwnerPolicyRoute / policyRouteRef / ESCALATED.
//   - Exactly one candidate: OwnerPrincipal / that principal / ASSIGNED.
//   - More than one candidate: OwnerCandidateSet / a stable reference naming
//     the requirement and expression that produced the set / AVAILABLE.
func RouteFromAssignment(a Assignment, policyRouteRef string) (OwnerKind, string, Status) {
	if a.Resolution.Outcome != humanwork.OutcomeResolved || len(a.Resolution.Candidates) == 0 {
		return OwnerPolicyRoute, policyRouteRef, StatusEscalated
	}
	if len(a.Resolution.Candidates) == 1 {
		return OwnerPrincipal, a.Resolution.Candidates[0].PrincipalID, StatusAssigned
	}
	return OwnerCandidateSet, "candidates:" + a.Resolution.RequirementID + "@" + a.Resolution.ExpressionDigest, StatusAvailable
}
