package conflict

import (
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ProposalState is the coarse status of the proposal side of a candidate
// under conflict evaluation. Per the conflict contract, "[o]verlap is
// evaluated against pending, approved, future-dated, and executed requests":
// all four are live inputs to classification, and none may be skipped.
type ProposalState string

// Declared proposal states. ProposalStateUnspecified is the zero value and
// never legal on a resolvable candidate.
const (
	ProposalStateUnspecified ProposalState = ""
	ProposalPending          ProposalState = "PENDING"
	ProposalApproved         ProposalState = "APPROVED"
	ProposalFutureDated      ProposalState = "FUTURE_DATED"
	ProposalExecuted         ProposalState = "EXECUTED"
)

var validProposalStates = map[ProposalState]bool{
	ProposalPending: true, ProposalApproved: true, ProposalFutureDated: true, ProposalExecuted: true,
}

// Valid reports whether s is a declared proposal state.
func (s ProposalState) Valid() bool { return validProposalStates[s] }

// Candidate is one side of a pair of overlapping write footprints under
// evaluation: the footprint itself, the proposal it belongs to, that
// proposal's declared conflict state and priority, and the declared
// supersession/dependency relationships that make classification a fact the
// proposal asserts rather than a guess this package makes from timing.
type Candidate struct {
	ProposalRevisionID string
	Footprint          WriteFootprint
	State              ProposalState
	Priority           int
	RecordedAt         values.Instant

	// SupersedesRevisionID, when set, names the proposal revision this
	// candidate explicitly replaces.
	SupersedesRevisionID string
	// DependsOnRevisionID, when set, names the proposal revision this
	// candidate must be ordered after rather than conflict with.
	DependsOnRevisionID string
}

// Validate rejects a candidate missing its identity, footprint, conflict
// state or recorded time.
func (c Candidate) Validate() error {
	if c.ProposalRevisionID == "" {
		return fmt.Errorf("%w: candidate has no proposal revision id", ErrInvalidCandidate)
	}
	if err := c.Footprint.Validate(); err != nil {
		return err
	}
	if !c.State.Valid() {
		return fmt.Errorf("%w: proposal %s declares no conflict state", ErrInvalidCandidate, c.ProposalRevisionID)
	}
	if !c.RecordedAt.IsSet() {
		return fmt.Errorf("%w: proposal %s carries no recorded time", ErrInvalidCandidate, c.ProposalRevisionID)
	}
	return nil
}

// Decision is the typed conflict classification. See the conflict contract
// (plan.md §9.2): "Compatible merge", "Ordered dependency", "Supersession",
// "Hard conflict requiring a decision".
type Decision string

// Declared decisions. DecisionUnspecified is the zero value and is never
// returned by [ClassifyConflict].
const (
	DecisionUnspecified       Decision = ""
	DecisionCompatibleMerge   Decision = "COMPATIBLE_MERGE"
	DecisionOrderedDependency Decision = "ORDERED_DEPENDENCY"
	DecisionSupersession      Decision = "SUPERSESSION"
	DecisionHardConflict      Decision = "HARD_CONFLICT"
)

// MergeRule is a versioned, domain-supplied deterministic merge proof. This
// package never merges arbitrary writes on its own: a pair classifies as
// COMPATIBLE_MERGE only when some rule proves it and names the proof, per
// "MERGEABLE requires a domain-supplied deterministic merge proof ... the
// registry never merges arbitrary writes."
type MergeRule struct {
	RuleRef string
	Version string
	// Proves reports whether the rule proves a and b compatible, and if so
	// the reference to the deterministic proof it produced.
	Proves func(a, b Candidate) (proofRef string, ok bool)
}

// Classification is the outcome of classifying one candidate pair: the
// decision, the evidence for it, which side "wins" ownership when that is
// meaningful, and a human-readable explanation.
type Classification struct {
	Decision                Decision
	OwnerProposalRevisionID string
	EvidenceRef             string
	Explanation             string
}

// ClassifyConflict classifies one pair of overlapping write-footprint
// candidates. It is symmetric: classifying (a, b) and (b, a) always returns
// an identical Classification, because the pair is canonically ordered by
// proposal revision id before any rule runs, closing the exact
// nondeterminism the CONFLICT-002 RED case names.
//
// Overlap must already be established by [WriteFootprint.Overlaps];
// ClassifyConflict re-checks it and fails closed with [ErrNoOverlap] rather
// than silently classifying disjoint writes.
func ClassifyConflict(a, b Candidate, rules []MergeRule) (Classification, error) {
	if err := a.Validate(); err != nil {
		return Classification{}, err
	}
	if err := b.Validate(); err != nil {
		return Classification{}, err
	}
	overlap, err := a.Footprint.Overlaps(b.Footprint)
	if err != nil {
		return Classification{}, err
	}
	if !overlap {
		return Classification{}, fmt.Errorf("%w: %s and %s do not overlap",
			ErrNoOverlap, a.ProposalRevisionID, b.ProposalRevisionID)
	}

	// Canonical pair order: symmetric in the caller's argument order, so
	// (a, b) and (b, a) always resolve the same evaluation direction.
	x, y := a, b
	if y.ProposalRevisionID < x.ProposalRevisionID {
		x, y = y, x
	}

	if y.SupersedesRevisionID == x.ProposalRevisionID {
		return supersession(y, x), nil
	}
	if x.SupersedesRevisionID == y.ProposalRevisionID {
		return supersession(x, y), nil
	}
	if y.DependsOnRevisionID == x.ProposalRevisionID {
		return orderedDependency(x, y), nil
	}
	if x.DependsOnRevisionID == y.ProposalRevisionID {
		return orderedDependency(y, x), nil
	}

	sortedRules := append([]MergeRule(nil), rules...)
	sort.Slice(sortedRules, func(i, j int) bool { return sortedRules[i].RuleRef < sortedRules[j].RuleRef })
	for _, rule := range sortedRules {
		if rule.Proves == nil {
			continue
		}
		if proof, ok := rule.Proves(x, y); ok {
			return Classification{
				Decision:    DecisionCompatibleMerge,
				EvidenceRef: proof,
				Explanation: fmt.Sprintf("merge rule %s@%s proved %s and %s compatible",
					rule.RuleRef, rule.Version, x.ProposalRevisionID, y.ProposalRevisionID),
			}, nil
		}
	}

	return Classification{
		Decision:    DecisionHardConflict,
		EvidenceRef: fmt.Sprintf("overlap:%s,%s", x.ProposalRevisionID, y.ProposalRevisionID),
		Explanation: fmt.Sprintf("%s (%s) and %s (%s) write overlapping %s with no merge proof, dependency or supersession",
			x.ProposalRevisionID, x.State, y.ProposalRevisionID, y.State, x.Footprint.Field),
	}, nil
}

// supersession builds the Classification for winner replacing loser.
func supersession(winner, loser Candidate) Classification {
	return Classification{
		Decision:                DecisionSupersession,
		OwnerProposalRevisionID: winner.ProposalRevisionID,
		EvidenceRef:             "supersedes:" + loser.ProposalRevisionID,
		Explanation: fmt.Sprintf("%s declares it supersedes %s",
			winner.ProposalRevisionID, loser.ProposalRevisionID),
	}
}

// orderedDependency builds the Classification for dependent committing after
// prerequisite.
func orderedDependency(prerequisite, dependent Candidate) Classification {
	return Classification{
		Decision:                DecisionOrderedDependency,
		OwnerProposalRevisionID: prerequisite.ProposalRevisionID,
		EvidenceRef:             "depends_on:" + prerequisite.ProposalRevisionID,
		Explanation: fmt.Sprintf("%s must commit after %s",
			dependent.ProposalRevisionID, prerequisite.ProposalRevisionID),
	}
}

// ClassifyAll classifies every overlapping pair in candidates. A pair whose
// footprints do not overlap is skipped, never silently misclassified.
// Every candidate must be independently valid, so a proposal cannot be
// "ignored" by omitting its conflict state.
func ClassifyAll(candidates []Candidate, rules []MergeRule) ([]Classification, error) {
	if len(candidates) < 2 {
		return nil, nil
	}
	for _, c := range candidates {
		if err := c.Validate(); err != nil {
			return nil, err
		}
	}
	var out []Classification
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			overlap, err := candidates[i].Footprint.Overlaps(candidates[j].Footprint)
			if err != nil {
				return nil, err
			}
			if !overlap {
				continue
			}
			c, err := ClassifyConflict(candidates[i], candidates[j], rules)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}
