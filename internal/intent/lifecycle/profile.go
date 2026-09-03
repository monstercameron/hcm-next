package lifecycle

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Lifecycle-definition validation causes.
var (
	// ErrInvalidProfile reports a lifecycle profile that cannot be published:
	// an undeclared state reference, an unreachable state, a terminal state
	// with an outgoing transition, or a transition without a retention class.
	ErrInvalidProfile = errors.New("lifecycle: invalid lifecycle profile")

	// ErrProfileWiden reports a domain profile that adds a state or transition
	// the shared kernel profile does not declare. Domain profiles may narrow
	// the shared restrictions; they may never bypass them.
	ErrProfileWiden = errors.New("lifecycle: domain profile widens the kernel profile")

	// ErrHistoryImmutable reports an attempt to rewrite recorded history: a
	// non-sequential record, or one whose From does not continue the last
	// recorded To.
	ErrHistoryImmutable = errors.New("lifecycle: lifecycle history is append-only")

	// ErrGovernanceBypassed reports an authoritative transition recorded
	// without the governance decision or retention class its transition rule
	// requires.
	ErrGovernanceBypassed = errors.New("lifecycle: transition bypasses governance or retention")
)

// TransitionRule is one declared edge of a dimension's lifecycle.
type TransitionRule struct {
	From StateID
	To   StateID

	// RequiresGovernance marks an edge that may only be recorded with a
	// governance decision reference.
	RequiresGovernance bool

	// RetentionClass names the retention class the resulting record inherits.
	// Every declared edge must name one: a transition with no retention class
	// produces evidence nobody owns.
	RetentionClass string
}

// Profile is the declared lifecycle of one dimension: its states, its initial
// state, its terminal states and its declared transitions. It is the
// lifecycle-definition layer that [Check] then constrains further.
type Profile struct {
	ID        string
	Dimension Dimension
	States    []StateID
	Initial   StateID
	Terminal  []StateID

	Transitions []TransitionRule
}

// Validate rejects a profile that cannot be published.
func (p Profile) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("%w: profile has no id", ErrInvalidProfile)
	}
	if p.Dimension == "" {
		return fmt.Errorf("%w: %s names no dimension", ErrInvalidProfile, p.ID)
	}
	declared := map[StateID]bool{}
	for _, s := range p.States {
		if s == "" {
			return fmt.Errorf("%w: %s declares an empty state id", ErrInvalidProfile, p.ID)
		}
		if declared[s] {
			return fmt.Errorf("%w: %s declares state %q twice", ErrInvalidProfile, p.ID, s)
		}
		declared[s] = true
	}
	if len(declared) == 0 {
		return fmt.Errorf("%w: %s declares no states", ErrInvalidProfile, p.ID)
	}
	if !declared[p.Initial] {
		return fmt.Errorf("%w: %s initial state %q is not declared",
			ErrInvalidProfile, p.ID, p.Initial)
	}
	terminal := map[StateID]bool{}
	for _, s := range p.Terminal {
		if !declared[s] {
			return fmt.Errorf("%w: %s terminal state %q is not declared",
				ErrInvalidProfile, p.ID, s)
		}
		terminal[s] = true
	}

	reachable := map[StateID]bool{p.Initial: true}
	adjacency := map[StateID][]StateID{}
	seen := map[TransitionRule]bool{}
	for _, t := range p.Transitions {
		if !declared[t.From] {
			return fmt.Errorf("%w: %s transition from undeclared state %q",
				ErrInvalidProfile, p.ID, t.From)
		}
		if !declared[t.To] {
			return fmt.Errorf("%w: %s transition to undeclared state %q",
				ErrInvalidProfile, p.ID, t.To)
		}
		if terminal[t.From] {
			return fmt.Errorf("%w: %s declares an outgoing transition from terminal state %q",
				ErrInvalidProfile, p.ID, t.From)
		}
		if t.RetentionClass == "" {
			return fmt.Errorf("%w: %s transition %s->%s declares no retention class",
				ErrInvalidProfile, p.ID, t.From, t.To)
		}
		key := TransitionRule{From: t.From, To: t.To}
		if seen[key] {
			return fmt.Errorf("%w: %s declares transition %s->%s twice",
				ErrInvalidProfile, p.ID, t.From, t.To)
		}
		seen[key] = true
		adjacency[t.From] = append(adjacency[t.From], t.To)
	}

	// Reachability: a state nobody can arrive at is dead configuration and
	// almost always a rename that was only half applied.
	queue := []StateID{p.Initial}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adjacency[cur] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}
	unreachable := make([]string, 0)
	for s := range declared {
		if !reachable[s] {
			unreachable = append(unreachable, string(s))
		}
	}
	if len(unreachable) > 0 {
		sort.Strings(unreachable)
		return fmt.Errorf("%w: %s declares unreachable states %v", ErrInvalidProfile, p.ID, unreachable)
	}
	return nil
}

// Rule resolves the declared transition from -> to.
func (p Profile) Rule(from, to StateID) (TransitionRule, bool) {
	for _, t := range p.Transitions {
		if t.From == from && t.To == to {
			return t, true
		}
	}
	return TransitionRule{}, false
}

// Narrows reports whether p is a legal narrowing of base: every state and every
// transition p declares must also exist in base. A domain profile may forbid
// what the kernel allows; it may never allow what the kernel forbids.
func (p Profile) Narrows(base Profile) error {
	if p.Dimension != base.Dimension {
		return fmt.Errorf("%w: %s narrows %s across dimensions %s and %s",
			ErrProfileWiden, p.ID, base.ID, p.Dimension, base.Dimension)
	}
	declared := map[StateID]bool{}
	for _, s := range base.States {
		declared[s] = true
	}
	for _, s := range p.States {
		if !declared[s] {
			return fmt.Errorf("%w: %s adds state %q", ErrProfileWiden, p.ID, s)
		}
	}
	for _, t := range p.Transitions {
		if _, ok := base.Rule(t.From, t.To); !ok {
			return fmt.Errorf("%w: %s adds transition %s->%s", ErrProfileWiden, p.ID, t.From, t.To)
		}
	}
	return nil
}

// TransitionRecord is the LifecycleTransitionRecord every authoritative
// transition appends. Records are immutable; correction is a further append.
type TransitionRecord struct {
	Sequence uint64
	From     Dimensions
	To       Dimensions
	At       values.Instant

	// Actor is the principal reference that caused the transition.
	Actor string

	// ReasonRef names the governed reason, never free prose.
	ReasonRef string

	// GovernanceDecisionRef is required for any dimension whose declared
	// transition rule sets RequiresGovernance.
	GovernanceDecisionRef string

	// RetentionClass is the retention class of the record itself.
	RetentionClass string
}

// History is the append-only sequence of transition records for one instance.
type History struct {
	records []TransitionRecord
}

// Len returns the number of recorded transitions.
func (h *History) Len() int { return len(h.records) }

// Records returns a copy of the recorded transitions. Mutating the returned
// slice cannot rewrite history.
func (h *History) Records() []TransitionRecord {
	return append([]TransitionRecord(nil), h.records...)
}

// Latest returns the most recent record.
func (h *History) Latest() (TransitionRecord, bool) {
	if len(h.records) == 0 {
		return TransitionRecord{}, false
	}
	return h.records[len(h.records)-1], true
}

// Append records one transition. It rejects a non-sequential record and one
// whose From does not continue the last recorded To: history is corrected by a
// further append, never edited in place.
func (h *History) Append(rec TransitionRecord) error {
	want := uint64(len(h.records)) + 1
	if rec.Sequence != want {
		return fmt.Errorf("%w: record sequence %d, next is %d",
			ErrHistoryImmutable, rec.Sequence, want)
	}
	if last, ok := h.Latest(); ok && last.To != rec.From {
		return fmt.Errorf("%w: record %d starts at %v but history stands at %v",
			ErrHistoryImmutable, rec.Sequence, rec.From, last.To)
	}
	if !rec.At.IsSet() {
		return fmt.Errorf("%w: record %d has no timestamp", ErrHistoryImmutable, rec.Sequence)
	}
	if rec.RetentionClass == "" {
		return fmt.Errorf("%w: record %d declares no retention class",
			ErrGovernanceBypassed, rec.Sequence)
	}
	h.records = append(h.records, rec)
	return nil
}

// KernelProfiles returns the canonical lifecycle profile of every dimension.
// Lifecycle assignment resolves through this function so that no caller invents
// its own state list.
func KernelProfiles() map[Dimension]Profile {
	return map[Dimension]Profile{
		DimensionRequest:     requestProfile(),
		DimensionExecution:   executionProfile(),
		DimensionBusiness:    businessProfile(),
		DimensionConsistency: consistencyProfile(),
		DimensionObligation:  obligationProfile(),
	}
}

const (
	retentionIntentLifecycle = "INTENT_LIFECYCLE"
	retentionBusinessOutcome = "BUSINESS_OUTCOME"
	retentionReconciliation  = "RECONCILIATION_EVIDENCE"
	retentionObligation      = "OBLIGATION_EVIDENCE"
)

func edges(retention string, requiresGovernance map[string]bool, pairs ...[2]StateID) []TransitionRule {
	out := make([]TransitionRule, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, TransitionRule{
			From:               p[0],
			To:                 p[1],
			RequiresGovernance: requiresGovernance[string(p[0])+"->"+string(p[1])],
			RetentionClass:     retention,
		})
	}
	return out
}

func requestProfile() Profile {
	governed := map[string]bool{
		"SUBMITTED->APPROVED":  true,
		"SUBMITTED->REJECTED":  true,
		"SIMULATED->CANCELLED": true,
		"APPROVED->CANCELLED":  true,
		"APPROVED->CLOSED":     true,
		"CLOSED->REOPENED":     true,
	}
	return Profile{
		ID:        "hcmnext.lifecycle.request/v1",
		Dimension: DimensionRequest,
		States: []StateID{
			"UNSPECIFIED", "DRAFT", "PREFLIGHTED", "SIMULATED", "SUBMITTED", "APPROVED",
			"REJECTED", "WITHDRAWN", "CANCELLED", "SUPERSEDED", "CLOSED", "REOPENED",
		},
		Initial:  "UNSPECIFIED",
		Terminal: []StateID{"REJECTED", "WITHDRAWN", "CANCELLED", "SUPERSEDED"},
		Transitions: edges(retentionIntentLifecycle, governed,
			[2]StateID{"UNSPECIFIED", "DRAFT"},
			[2]StateID{"DRAFT", "PREFLIGHTED"},
			[2]StateID{"DRAFT", "WITHDRAWN"},
			[2]StateID{"DRAFT", "CANCELLED"},
			[2]StateID{"PREFLIGHTED", "DRAFT"},
			[2]StateID{"PREFLIGHTED", "SIMULATED"},
			[2]StateID{"PREFLIGHTED", "WITHDRAWN"},
			[2]StateID{"PREFLIGHTED", "CANCELLED"},
			[2]StateID{"SIMULATED", "SIMULATED"},
			[2]StateID{"SIMULATED", "SUBMITTED"},
			[2]StateID{"SIMULATED", "WITHDRAWN"},
			[2]StateID{"SIMULATED", "CANCELLED"},
			[2]StateID{"SIMULATED", "SUPERSEDED"},
			[2]StateID{"SUBMITTED", "APPROVED"},
			[2]StateID{"SUBMITTED", "REJECTED"},
			[2]StateID{"SUBMITTED", "SIMULATED"},
			[2]StateID{"SUBMITTED", "WITHDRAWN"},
			[2]StateID{"SUBMITTED", "CANCELLED"},
			[2]StateID{"SUBMITTED", "SUPERSEDED"},
			// A definition that requires no approval closes from SUBMITTED:
			// an analytical request completes with ExecutionState NOT_PLANNED
			// and never passes through APPROVED.
			[2]StateID{"SUBMITTED", "CLOSED"},
			[2]StateID{"APPROVED", "SIMULATED"},
			[2]StateID{"APPROVED", "CANCELLED"},
			[2]StateID{"APPROVED", "SUPERSEDED"},
			[2]StateID{"APPROVED", "CLOSED"},
			[2]StateID{"REOPENED", "SIMULATED"},
			[2]StateID{"REOPENED", "CLOSED"},
			[2]StateID{"CLOSED", "REOPENED"},
		),
	}
}

func executionProfile() Profile {
	governed := map[string]bool{
		"REVALIDATING->EXECUTING": true,
	}
	return Profile{
		ID:        "hcmnext.lifecycle.execution/v1",
		Dimension: DimensionExecution,
		States: []StateID{
			"UNSPECIFIED", "NOT_PLANNED", "SCHEDULED", "REVALIDATING", "EXECUTING",
			"COMMITTED", "BLOCKED", "REPAIR_REQUIRED",
		},
		Initial: "UNSPECIFIED",
		Transitions: edges(retentionIntentLifecycle, governed,
			[2]StateID{"UNSPECIFIED", "NOT_PLANNED"},
			[2]StateID{"NOT_PLANNED", "SCHEDULED"},
			[2]StateID{"NOT_PLANNED", "BLOCKED"},
			[2]StateID{"SCHEDULED", "REVALIDATING"},
			[2]StateID{"SCHEDULED", "NOT_PLANNED"},
			[2]StateID{"SCHEDULED", "BLOCKED"},
			[2]StateID{"REVALIDATING", "EXECUTING"},
			[2]StateID{"REVALIDATING", "NOT_PLANNED"},
			[2]StateID{"REVALIDATING", "BLOCKED"},
			[2]StateID{"EXECUTING", "COMMITTED"},
			[2]StateID{"EXECUTING", "BLOCKED"},
			[2]StateID{"EXECUTING", "REPAIR_REQUIRED"},
			[2]StateID{"COMMITTED", "REPAIR_REQUIRED"},
			[2]StateID{"BLOCKED", "SCHEDULED"},
			[2]StateID{"BLOCKED", "NOT_PLANNED"},
			[2]StateID{"REPAIR_REQUIRED", "SCHEDULED"},
			[2]StateID{"REPAIR_REQUIRED", "COMMITTED"},
		),
	}
}

func businessProfile() Profile {
	return Profile{
		ID:        "hcmnext.lifecycle.business/v1",
		Dimension: DimensionBusiness,
		States: []StateID{
			"UNSPECIFIED", "NOT_STARTED", "IN_PROGRESS", "COMPLETED", "NOT_ACHIEVED",
			"CORRECTED", "UNKNOWN",
		},
		Initial: "UNSPECIFIED",
		Transitions: edges(retentionBusinessOutcome, nil,
			[2]StateID{"UNSPECIFIED", "NOT_STARTED"},
			[2]StateID{"NOT_STARTED", "IN_PROGRESS"},
			[2]StateID{"NOT_STARTED", "NOT_ACHIEVED"},
			[2]StateID{"NOT_STARTED", "UNKNOWN"},
			[2]StateID{"IN_PROGRESS", "COMPLETED"},
			[2]StateID{"IN_PROGRESS", "NOT_ACHIEVED"},
			[2]StateID{"IN_PROGRESS", "UNKNOWN"},
			[2]StateID{"COMPLETED", "CORRECTED"},
			[2]StateID{"COMPLETED", "UNKNOWN"},
			[2]StateID{"NOT_ACHIEVED", "IN_PROGRESS"},
			[2]StateID{"CORRECTED", "UNKNOWN"},
			[2]StateID{"UNKNOWN", "IN_PROGRESS"},
			[2]StateID{"UNKNOWN", "COMPLETED"},
			[2]StateID{"UNKNOWN", "NOT_ACHIEVED"},
		),
	}
}

func consistencyProfile() Profile {
	return Profile{
		ID:        "hcmnext.lifecycle.consistency/v1",
		Dimension: DimensionConsistency,
		States: []StateID{
			"UNSPECIFIED", "NOT_APPLICABLE", "PENDING_OBSERVATION", "CONSISTENT",
			"DEGRADED", "REPAIRING", "UNKNOWN",
		},
		Initial: "UNSPECIFIED",
		Transitions: edges(retentionReconciliation, nil,
			[2]StateID{"UNSPECIFIED", "NOT_APPLICABLE"},
			[2]StateID{"UNSPECIFIED", "PENDING_OBSERVATION"},
			[2]StateID{"NOT_APPLICABLE", "PENDING_OBSERVATION"},
			[2]StateID{"PENDING_OBSERVATION", "CONSISTENT"},
			[2]StateID{"PENDING_OBSERVATION", "DEGRADED"},
			[2]StateID{"PENDING_OBSERVATION", "UNKNOWN"},
			[2]StateID{"CONSISTENT", "DEGRADED"},
			[2]StateID{"CONSISTENT", "UNKNOWN"},
			[2]StateID{"DEGRADED", "REPAIRING"},
			[2]StateID{"DEGRADED", "UNKNOWN"},
			[2]StateID{"REPAIRING", "CONSISTENT"},
			[2]StateID{"REPAIRING", "DEGRADED"},
			[2]StateID{"REPAIRING", "UNKNOWN"},
			[2]StateID{"UNKNOWN", "PENDING_OBSERVATION"},
			[2]StateID{"UNKNOWN", "CONSISTENT"},
			[2]StateID{"UNKNOWN", "DEGRADED"},
		),
	}
}

func obligationProfile() Profile {
	governed := map[string]bool{
		"PENDING->WAIVED": true,
		"OVERDUE->WAIVED": true,
	}
	return Profile{
		ID:        "hcmnext.lifecycle.obligation/v1",
		Dimension: DimensionObligation,
		States: []StateID{
			"UNSPECIFIED", "NOT_APPLICABLE", "PENDING", "SATISFIED", "OVERDUE",
			"WAIVED", "UNKNOWN",
		},
		Initial: "UNSPECIFIED",
		Transitions: edges(retentionObligation, governed,
			[2]StateID{"UNSPECIFIED", "NOT_APPLICABLE"},
			[2]StateID{"UNSPECIFIED", "PENDING"},
			[2]StateID{"NOT_APPLICABLE", "PENDING"},
			[2]StateID{"PENDING", "SATISFIED"},
			[2]StateID{"PENDING", "OVERDUE"},
			[2]StateID{"PENDING", "WAIVED"},
			[2]StateID{"PENDING", "UNKNOWN"},
			[2]StateID{"OVERDUE", "SATISFIED"},
			[2]StateID{"OVERDUE", "WAIVED"},
			[2]StateID{"OVERDUE", "UNKNOWN"},
			[2]StateID{"SATISFIED", "UNKNOWN"},
			[2]StateID{"WAIVED", "UNKNOWN"},
			[2]StateID{"UNKNOWN", "PENDING"},
			[2]StateID{"UNKNOWN", "SATISFIED"},
			[2]StateID{"UNKNOWN", "OVERDUE"},
		),
	}
}
