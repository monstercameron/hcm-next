package simcontract

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Errors. All are matchable with errors.Is; a business refusal is never one
// of these -- it is a Refusal on the contract's own Findings/Refusals.
var (
	// ErrInvalidInput is returned for a malformed constructor input. It is
	// reserved for contract failures a caller must fix.
	ErrInvalidInput = errors.New("promotion/simcontract: input is invalid")
	// ErrIncompleteContract is the sentinel every [Refusal] matches with
	// errors.Is, raised when a mandatory section is absent.
	ErrIncompleteContract = errors.New("promotion/simcontract: contract is missing a required section")
	// ErrDigestConflict is returned by a [Persist] implementation when a
	// caller presents two different artifacts under the same digest.
	ErrDigestConflict = errors.New("promotion/simcontract: digest is already recorded for a different artifact")
)

// SectionKind names one of the twelve mandatory contract sections. RED
// requires that a contract missing any one of them refuses with a typed
// refusal naming exactly which.
type SectionKind string

// The twelve declared sections, in the order [Validate] checks them.
const (
	SectionReads            SectionKind = "READS"
	SectionWrites           SectionKind = "WRITES"
	SectionStreams          SectionKind = "STREAMS"
	SectionConflicts        SectionKind = "CONFLICTS"
	SectionApprovals        SectionKind = "APPROVALS"
	SectionAuthority        SectionKind = "AUTHORITY"
	SectionLegalObligations SectionKind = "LEGAL_OBLIGATIONS"
	SectionSideEffects      SectionKind = "SIDE_EFFECTS"
	SectionCost             SectionKind = "COST"
	SectionCompletion       SectionKind = "COMPLETION"
	SectionRevalidation     SectionKind = "REVALIDATION"
	SectionRepair           SectionKind = "REPAIR"
)

// Sections returns the twelve mandatory sections in declaration order.
func Sections() []SectionKind {
	return []SectionKind{
		SectionReads, SectionWrites, SectionStreams, SectionConflicts,
		SectionApprovals, SectionAuthority, SectionLegalObligations, SectionSideEffects,
		SectionCost, SectionCompletion, SectionRevalidation, SectionRepair,
	}
}

// String returns the wire token.
func (k SectionKind) String() string { return string(k) }

// Refusal is the typed refusal [Validate] returns for one missing or
// incoherent section. It names the section and a value-free reason, never the
// content that was rejected.
type Refusal struct {
	Section SectionKind
	Reason  string
}

// Error implements error.
func (r Refusal) Error() string {
	return fmt.Sprintf("promotion/simcontract: %s section: %s", r.Section, r.Reason)
}

// Unwrap makes every Refusal match [ErrIncompleteContract].
func (r Refusal) Unwrap() error { return ErrIncompleteContract }

// refuse builds a section [Refusal].
func refuse(section SectionKind, format string, args ...any) error {
	return Refusal{Section: section, Reason: fmt.Sprintf(format, args...)}
}

// ResultStatus is the contract's overall, derived verdict. It is never
// caller-supplied: [Assemble] always computes it from Findings and Refusals,
// so a contract can never assert an EXECUTABLE status over a blocked finding.
type ResultStatus string

// Declared result statuses. ResultStatusUnspecified is the zero value and is
// never legal on an assembled contract.
const (
	ResultStatusUnspecified     ResultStatus = ""
	ResultExecutableAsSimulated ResultStatus = "EXECUTABLE_AS_SIMULATED"
	ResultBlocked               ResultStatus = "BLOCKED"
)

// Valid reports whether s is a declared result status.
func (s ResultStatus) Valid() bool {
	return s == ResultExecutableAsSimulated || s == ResultBlocked
}

// String returns the wire token.
func (s ResultStatus) String() string {
	if s == "" {
		return "RESULT_STATUS_UNSPECIFIED"
	}
	return string(s)
}

// CostState says whether the cost section was computed, and if so whether the
// candidate has a cost at all. It exists so that "this change costs nothing"
// (the Manager Change fixture) and "cost was never evaluated" can never be
// confused with each other.
type CostState string

// Declared cost states. CostStateUnspecified is the zero value and is never
// legal on a validated section.
const (
	CostStateUnspecified CostState = ""
	// CostNone means the candidate consumes no budget: there is nothing to
	// reserve, and Amount is nil.
	CostNone CostState = "NO_COST"
	// CostEvaluated means an annualized cost delta was computed. Amount is
	// non-nil.
	CostEvaluated CostState = "EVALUATED"
)

// Cost is the contract's cost section.
type Cost struct {
	State  CostState
	Amount *values.Money
}

// Validate rejects an unstated state or an amount inconsistent with it.
func (c Cost) Validate() error {
	switch c.State {
	case CostNone:
		if c.Amount != nil {
			return fmt.Errorf("%w: cost is NO_COST but carries an amount", ErrInvalidInput)
		}
	case CostEvaluated:
		if c.Amount == nil {
			return fmt.Errorf("%w: cost is EVALUATED but carries no amount", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: cost state is unstated", ErrInvalidInput)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (c Cost) Canonical() []byte {
	if err := c.Validate(); err != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.promotion.simcontract.Cost", schemaVersion).
		String("state", string(c.State)).
		Bool("amount?", c.Amount != nil)
	if c.Amount != nil {
		w.Value("amount", *c.Amount)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// CompletionState is what the contract can say about whether the candidate
// could reach completion once (and if) it is proposed.
type CompletionState string

// Declared completion states. CompletionStateUnspecified is the zero value
// and is never legal on a validated section.
const (
	CompletionStateUnspecified CompletionState = ""
	// CompletionReady means nothing outstanding stands between this candidate
	// and execution once it is approved as a proposal: no approval is
	// outstanding and no finding blocks it. It never grants execution rights;
	// this is still a simulation.
	CompletionReady CompletionState = "READY"
	// CompletionPendingApproval means the candidate could complete once its
	// outstanding approvals are satisfied.
	CompletionPendingApproval CompletionState = "PENDING_APPROVAL"
	// CompletionBlocked means a blocking finding or refusal governs the
	// candidate and no approval could clear it.
	CompletionBlocked CompletionState = "BLOCKED"
)

// Completion is the contract's completion section.
type Completion struct {
	State CompletionState
	// OutstandingApprovals are the approval requirement ids still needed for
	// completion, sorted. Empty when State is READY or BLOCKED.
	OutstandingApprovals []string
	// Detail is a short, value-free explanation.
	Detail string
}

// Validate rejects an unstated state or a detail-free section.
func (c Completion) Validate() error {
	switch c.State {
	case CompletionReady, CompletionPendingApproval, CompletionBlocked:
	default:
		return fmt.Errorf("%w: completion state is unstated", ErrInvalidInput)
	}
	if c.Detail == "" {
		return fmt.Errorf("%w: completion carries no detail", ErrInvalidInput)
	}
	if c.State != CompletionPendingApproval && len(c.OutstandingApprovals) > 0 {
		return fmt.Errorf("%w: completion state %s carries outstanding approvals", ErrInvalidInput, c.State)
	}
	if c.State == CompletionPendingApproval && len(c.OutstandingApprovals) == 0 {
		return fmt.Errorf("%w: completion is PENDING_APPROVAL but names no outstanding approval", ErrInvalidInput)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (c Completion) Canonical() []byte {
	if err := c.Validate(); err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.simcontract.Completion", schemaVersion).
		String("state", string(c.State)).
		SortedStrings("outstanding_approvals", c.OutstandingApprovals).
		String("detail", c.Detail).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Revalidation is the contract's revalidation section: the rules a later
// execution-time check would rerun, and the control-snapshot digest the
// candidate was simulated under. It is material in the same sense
// [github.com/monstercameron/human-capital-management-suite/internal/intent.ProposalRevision]'s own
// RevalidationPlan is: dropping a rule changes what would be checked before
// anything runs.
type Revalidation struct {
	Rules                 []string
	ControlSnapshotDigest string
}

// Validate rejects an unstated rule set or a missing control-snapshot digest.
func (r Revalidation) Validate() error {
	if r.Rules == nil {
		return fmt.Errorf("%w: revalidation names no rules", ErrInvalidInput)
	}
	for _, rule := range r.Rules {
		if rule == "" {
			return fmt.Errorf("%w: revalidation carries an empty rule reference", ErrInvalidInput)
		}
	}
	if r.ControlSnapshotDigest == "" {
		return fmt.Errorf("%w: revalidation carries no control-snapshot digest", ErrInvalidInput)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r Revalidation) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.simcontract.Revalidation", schemaVersion).
		SortedStrings("rules", r.Rules).
		String("control_snapshot_digest", r.ControlSnapshotDigest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}
