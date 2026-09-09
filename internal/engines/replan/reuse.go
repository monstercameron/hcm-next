package replan

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// ReuseStatus is the disposition of an approval when a successor proposal is
// being assembled.  A retained approval is never copied into, or edited for,
// the successor; it is referred to by an ApplicabilityLink instead.
type ReuseStatus string

const (
	RetainedByReference ReuseStatus = "RETAINED_BY_REFERENCE"
	ReapprovalRequired  ReuseStatus = "REAPPROVAL_REQUIRED"
	ReuseUnknown        ReuseStatus = "UNKNOWN"
	// Verbose aliases make the wire tokens convenient for callers.
	ReuseRetainedByReference    = RetainedByReference
	ReuseReapprovalRequired     = ReapprovalRequired
	ReuseUnknownStatus          = ReuseUnknown
	DecisionRetainedByReference = RetainedByReference
	DecisionReapprovalRequired  = ReapprovalRequired
	DecisionUnknown             = ReuseUnknown
)

// ChangeKind identifies a proposal change relevant to approval reuse.
type ChangeKind string

const (
	ChangeAmount       ChangeKind = "AMOUNT"
	ChangeInterval     ChangeKind = "INTERVAL"
	ChangeObligation   ChangeKind = "OBLIGATION"
	ChangeView         ChangeKind = "VIEW"
	ChangePresentation ChangeKind = "PRESENTATION"
)

// ProposalChange is a value-free description of a difference between the old
// and successor proposal. Unknown kinds are fail-closed.
type ProposalChange struct {
	Kind ChangeKind
	// DecisionID optionally scopes the change. Empty means all decisions.
	DecisionID string
}

// DecisionReceipt is the immutable receipt originally minted for a decision.
type DecisionReceipt struct {
	TenantID           string
	DecisionID         string
	ProposalRevisionID string
	ProposalDigest     string
	ReceiptDigest      string
}

// Decision is the minimum immutable evidence needed by this pure engine.
// Receipt is checked against the binding and then returned byte-for-byte.
type Decision struct {
	TenantID           string
	DecisionID         string
	ProposalRevisionID string
	ProposalDigest     string
	Receipt            DecisionReceipt
}

// SuccessorProposal identifies the proposal to which applicability may be
// linked. It is deliberately not a mutable replacement for Decision.
type SuccessorProposal struct {
	TenantID   string
	RevisionID string
	Digest     string
}

// ApplicabilityLink records where an original decision may be used next.
type ApplicabilityLink struct {
	TenantID           string
	DecisionID         string
	FromRevisionID     string
	FromProposalDigest string
	ToRevisionID       string
	ToProposalDigest   string
	PolicyVersion      int
}

// ReusePolicy is versioned policy. A kind must be explicitly permitted to be
// retained. Material changes always require reapproval, regardless of policy.
type ReusePolicy struct {
	PolicyRef            string
	PolicyDigest         string
	Version              int
	PermittedChangeKinds map[ChangeKind]bool
}

// DecisionReuse is one decision's immutable reuse result.
type DecisionReuse struct {
	DecisionID      string
	Status          ReuseStatus
	OriginalReceipt DecisionReceipt
	// Receipt is an alias-shaped copy for consumers that use receipt as the
	// primary evidence field; neither field is a successor binding.
	Receipt       DecisionReceipt
	Applicability *ApplicabilityLink
	Reason        string
}

// ReuseResult is deterministic evidence for all supplied decisions.
type ReuseResult struct {
	TenantID       string
	PolicyRef      string
	PolicyDigest   string
	FromRevisionID string
	ToRevisionID   string
	PolicyVersion  int
	Decisions      []DecisionReuse
	Digest         string
}

var (
	ErrInvalidReusePolicy = errors.New("replan: invalid reuse policy")
	ErrInvalidDecision    = errors.New("replan: invalid approval decision")
	ErrInvalidSuccessor   = errors.New("replan: invalid successor proposal")
)

// EvaluateReuse applies a versioned, fail-closed reuse policy. The policy and
// decisions must be loaded from their authoritative stores by the caller;
// references and digests bind those immutable artifacts but do not authenticate
// caller-constructed values. It never mutates a decision and never treats a
// changed presentation as harmless unless the policy explicitly permits that
// change kind.
func EvaluateReuse(policy ReusePolicy, from, to SuccessorProposal, decisions []Decision, changes []ProposalChange) (ReuseResult, error) {
	if policy.Version <= 0 || policy.PolicyRef == "" || policy.PolicyDigest == "" {
		return ReuseResult{}, fmt.Errorf("%w: immutable reference, digest, and positive version are required", ErrInvalidReusePolicy)
	}
	if from.TenantID == "" || from.RevisionID == "" || from.Digest == "" || to.TenantID == "" || to.RevisionID == "" || to.Digest == "" {
		return ReuseResult{}, ErrInvalidSuccessor
	}
	if from.TenantID != to.TenantID {
		return ReuseResult{}, fmt.Errorf("%w: successor belongs to another tenant", ErrInvalidSuccessor)
	}
	if from.RevisionID == to.RevisionID {
		return ReuseResult{}, fmt.Errorf("%w: successor revision must differ", ErrInvalidSuccessor)
	}
	material := false
	unknown := false
	materialFor := func(id string) bool {
		for _, c := range changes {
			if c.DecisionID != "" && c.DecisionID != id {
				continue
			}
			if c.Kind == ChangeAmount || c.Kind == ChangeInterval || c.Kind == ChangeObligation || ((c.Kind == ChangeView || c.Kind == ChangePresentation) && !policy.PermittedChangeKinds[c.Kind]) {
				return true
			}
		}
		return false
	}
	unknownFor := func(id string) bool {
		for _, c := range changes {
			if (c.DecisionID == "" || c.DecisionID == id) && c.Kind != ChangeAmount && c.Kind != ChangeInterval && c.Kind != ChangeObligation && c.Kind != ChangeView && c.Kind != ChangePresentation {
				return true
			}
		}
		return false
	}
	for _, c := range changes {
		if c.Kind == ChangeAmount || c.Kind == ChangeInterval || c.Kind == ChangeObligation {
			if c.DecisionID == "" {
				material = true
			}
			continue
		}
		if c.Kind == ChangeView || c.Kind == ChangePresentation {
			if !policy.PermittedChangeKinds[c.Kind] && c.DecisionID == "" {
				material = true
			}
			continue
		}
		if c.DecisionID == "" {
			unknown = true
		}
	}
	seen := make(map[string]struct{}, len(decisions))
	result := ReuseResult{TenantID: from.TenantID, PolicyRef: policy.PolicyRef, PolicyDigest: policy.PolicyDigest, FromRevisionID: from.RevisionID, ToRevisionID: to.RevisionID, PolicyVersion: policy.Version}
	for _, d := range decisions {
		if d.TenantID != from.TenantID || d.DecisionID == "" || d.ProposalRevisionID != from.RevisionID || d.ProposalDigest != from.Digest || d.Receipt.TenantID != from.TenantID || d.Receipt.DecisionID != d.DecisionID || d.Receipt.ProposalRevisionID != from.RevisionID || d.Receipt.ProposalDigest != from.Digest || d.Receipt.ReceiptDigest == "" {
			return ReuseResult{}, fmt.Errorf("%w: decision %q is not bound to source proposal", ErrInvalidDecision, d.DecisionID)
		}
		if _, ok := seen[d.DecisionID]; ok {
			return ReuseResult{}, fmt.Errorf("%w: duplicate decision %q", ErrInvalidDecision, d.DecisionID)
		}
		seen[d.DecisionID] = struct{}{}
		item := DecisionReuse{DecisionID: d.DecisionID, OriginalReceipt: d.Receipt, Receipt: d.Receipt}
		if unknown || unknownFor(d.DecisionID) {
			item.Status, item.Reason = ReuseUnknown, "change kind is not covered by the reuse policy"
		} else if material || materialFor(d.DecisionID) {
			item.Status, item.Reason = ReapprovalRequired, "material proposal change or policy-disallowed view change"
		} else {
			item.Status, item.Reason = RetainedByReference, "policy-permitted unaffected decision"
			item.Applicability = &ApplicabilityLink{TenantID: from.TenantID, DecisionID: d.DecisionID, FromRevisionID: from.RevisionID, FromProposalDigest: from.Digest, ToRevisionID: to.RevisionID, ToProposalDigest: to.Digest, PolicyVersion: policy.Version}
		}
		result.Decisions = append(result.Decisions, item)
	}
	sort.Slice(result.Decisions, func(i, j int) bool { return result.Decisions[i].DecisionID < result.Decisions[j].DecisionID })
	result.Digest = reuseDigest(result)
	return result, nil
}

// Reuse is a concise alias for EvaluateReuse.
func Reuse(policy ReusePolicy, from, to SuccessorProposal, decisions []Decision, changes []ProposalChange) (ReuseResult, error) {
	return EvaluateReuse(policy, from, to, decisions, changes)
}

func reuseDigest(r ReuseResult) string {
	w := canonicalbytes.New("hcmnext.engines.replan.DecisionReuse", 1).String("tenant", r.TenantID).String("policy_ref", r.PolicyRef).String("policy_digest", r.PolicyDigest).String("from", r.FromRevisionID).String("to", r.ToRevisionID).Int("policy_version", int64(r.PolicyVersion))
	for _, d := range r.Decisions {
		w.String("decision", d.DecisionID).String("status", string(d.Status)).String("receipt", d.OriginalReceipt.ReceiptDigest)
		if d.Applicability != nil {
			w.String("applicability", d.Applicability.ToRevisionID+"@"+d.Applicability.ToProposalDigest)
		}
	}
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}
