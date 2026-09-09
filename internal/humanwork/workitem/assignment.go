// Package workitem: this file is WORK-002. [ResolveAssignment] runs the
// existing resolver -- [humanwork.Resolve] over a compiled
// [humanwork.ApprovalRequirement] and a [humanwork.Directory] -- and records
// what it decided. Nothing about candidate selection is reimplemented here.
package workitem

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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
// [Assignment.MarshalJSON]: a zero [humanwork.Resolution] carries no
// resolution instant at all, and the empty object says exactly that -- a
// CREATED item that has never been routed.
func marshalAssignment(a Assignment) ([]byte, error) {
	if a.IsZero() {
		return []byte("{}"), nil
	}
	return json.Marshal(a)
}

// assignmentWire is [Assignment]'s JSON shape. It exists because
// [humanwork.Resolution] carries [values.Instant] and other kernel value
// fields whose encoding/json path calls MarshalText unconditionally --
// including on a [humanwork.Candidate.DelegationExpiry] that is legitimately
// unset for a DIRECT candidate, which is not an encoding defect but a
// business fact this package still needs to store. Every instant here is
// carried as its canonical RFC 3339 text (empty string when unset) instead,
// which keeps the encoding total over every value [humanwork.Resolve] can
// produce and keeps the field order -- and therefore [Assignment.Digest] --
// fixed by this struct's declaration rather than by encoding/json's default
// reflection order.
type assignmentWire struct {
	RequirementID       string          `json:"requirement_id"`
	RequirementRevision uint64          `json:"requirement_revision"`
	Outcome             string          `json:"outcome"`
	Candidates          []candidateWire `json:"candidates"`
	Excluded            []exclusionWire `json:"excluded"`
	FallbackUsed        bool            `json:"fallback_used"`
	ResolvedAt          string          `json:"resolved_at"`
	EffectiveAt         string          `json:"effective_at"`
	DirectoryVersion    string          `json:"directory_version"`
	ExpressionDigest    string          `json:"expression_digest"`
	RequirementDigest   string          `json:"requirement_digest"`
	QuorumRequired      uint32          `json:"quorum_required"`

	GovernancePolicyRef string `json:"governance_policy_ref"`
	Trigger             string `json:"trigger"`
	ChosenOwner         string `json:"chosen_owner"`
}

type candidateWire struct {
	PrincipalID          string `json:"principal_id"`
	Via                  string `json:"via"`
	TermRef              string `json:"term_ref"`
	DelegationID         string `json:"delegation_id,omitempty"`
	DelegatedFrom        string `json:"delegated_from,omitempty"`
	DelegationExpiry     string `json:"delegation_expiry,omitempty"`
	IdentityAssuranceRef string `json:"identity_assurance_ref,omitempty"`
}

type exclusionWire struct {
	PrincipalID string `json:"principal_id"`
	RuleID      string `json:"rule_id"`
	Reason      string `json:"reason"`
}

// MarshalJSON implements [json.Marshaler] through [assignmentWire].
func (a Assignment) MarshalJSON() ([]byte, error) {
	w := assignmentWire{
		RequirementID:       a.Resolution.RequirementID,
		RequirementRevision: a.Resolution.RequirementRevision,
		Outcome:             string(a.Resolution.Outcome),
		FallbackUsed:        a.Resolution.FallbackUsed,
		ResolvedAt:          a.Resolution.ResolvedAt.String(),
		EffectiveAt:         a.Resolution.EffectiveAt.String(),
		DirectoryVersion:    a.Resolution.DirectoryVersion,
		ExpressionDigest:    a.Resolution.ExpressionDigest,
		RequirementDigest:   a.Resolution.RequirementDigest,
		QuorumRequired:      a.Resolution.QuorumRequired,
		GovernancePolicyRef: a.GovernancePolicyRef,
		Trigger:             string(a.Trigger),
		ChosenOwner:         a.ChosenOwner,
	}
	w.Candidates = make([]candidateWire, len(a.Resolution.Candidates))
	for i, c := range a.Resolution.Candidates {
		w.Candidates[i] = candidateWire{
			PrincipalID:          c.PrincipalID,
			Via:                  string(c.Via),
			TermRef:              c.TermRef,
			DelegationID:         c.DelegationID,
			DelegatedFrom:        c.DelegatedFrom,
			DelegationExpiry:     c.DelegationExpiry.String(),
			IdentityAssuranceRef: c.IdentityAssuranceRef,
		}
	}
	w.Excluded = make([]exclusionWire, len(a.Resolution.Excluded))
	for i, e := range a.Resolution.Excluded {
		w.Excluded[i] = exclusionWire{PrincipalID: e.PrincipalID, RuleID: e.RuleID, Reason: e.Reason}
	}
	return json.Marshal(w)
}

// UnmarshalJSON implements [json.Unmarshaler] through [assignmentWire].
func (a *Assignment) UnmarshalJSON(data []byte) error {
	var w assignmentWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*a = Assignment{
		Resolution: humanwork.Resolution{
			RequirementID:       w.RequirementID,
			RequirementRevision: w.RequirementRevision,
			Outcome:             humanwork.ResolutionOutcome(w.Outcome),
			FallbackUsed:        w.FallbackUsed,
			DirectoryVersion:    w.DirectoryVersion,
			ExpressionDigest:    w.ExpressionDigest,
			RequirementDigest:   w.RequirementDigest,
			QuorumRequired:      w.QuorumRequired,
		},
		GovernancePolicyRef: w.GovernancePolicyRef,
		Trigger:             ReResolutionTrigger(w.Trigger),
		ChosenOwner:         w.ChosenOwner,
	}
	if w.ResolvedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, w.ResolvedAt); err == nil {
			a.Resolution.ResolvedAt = values.NewInstant(t)
		}
	}
	if w.EffectiveAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, w.EffectiveAt); err == nil {
			a.Resolution.EffectiveAt = values.NewInstant(t)
		}
	}
	if len(w.Candidates) > 0 {
		a.Resolution.Candidates = make([]humanwork.Candidate, len(w.Candidates))
		for i, c := range w.Candidates {
			cand := humanwork.Candidate{
				PrincipalID:          c.PrincipalID,
				Via:                  humanwork.CandidateSource(c.Via),
				TermRef:              c.TermRef,
				DelegationID:         c.DelegationID,
				DelegatedFrom:        c.DelegatedFrom,
				IdentityAssuranceRef: c.IdentityAssuranceRef,
			}
			if c.DelegationExpiry != "" {
				if t, err := time.Parse(time.RFC3339Nano, c.DelegationExpiry); err == nil {
					cand.DelegationExpiry = values.NewInstant(t)
				}
			}
			a.Resolution.Candidates[i] = cand
		}
	}
	if len(w.Excluded) > 0 {
		a.Resolution.Excluded = make([]humanwork.Exclusion, len(w.Excluded))
		for i, e := range w.Excluded {
			a.Resolution.Excluded[i] = humanwork.Exclusion{PrincipalID: e.PrincipalID, RuleID: e.RuleID, Reason: e.Reason}
		}
	}
	return nil
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
