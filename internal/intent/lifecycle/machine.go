package lifecycle

import (
	"fmt"
	"maps"
)

// Machine applies transitions to one instance's five dimensions. It is the
// single path a command, a projection rebuild, a replay and a repair all take,
// so none of them can reach a tuple the others would reject.
//
// The zero value is not usable; construct one with [NewMachine].
type Machine struct {
	profiles map[Dimension]Profile
	current  Dimensions
	history  History
}

// NewMachine returns a machine seeded at initial. Passing nil profiles uses
// [KernelProfiles]. The seed tuple is checked for legality under ctx before the
// machine is returned, so a machine never starts in an illegal state.
func NewMachine(profiles map[Dimension]Profile, initial Dimensions, ctx Context) (*Machine, error) {
	if profiles == nil {
		profiles = KernelProfiles()
	}
	for _, dim := range AllDimensions() {
		p, ok := profiles[dim]
		if !ok {
			return nil, fmt.Errorf("%w: no profile assigned for %s", ErrInvalidProfile, dim)
		}
		if err := p.Validate(); err != nil {
			return nil, err
		}
		if p.Dimension != dim {
			return nil, fmt.Errorf("%w: profile %s is assigned to %s but declares %s",
				ErrInvalidProfile, p.ID, dim, p.Dimension)
		}
	}
	if err := Check(initial, ctx); err != nil {
		return nil, err
	}
	return &Machine{profiles: maps.Clone(profiles), current: initial}, nil
}

// Current returns the current dimension tuple.
func (m *Machine) Current() Dimensions { return m.current }

// History returns a copy of the recorded transitions.
func (m *Machine) History() []TransitionRecord { return m.history.Records() }

// Profiles returns a copy of the assigned lifecycle profiles.
func (m *Machine) Profiles() map[Dimension]Profile { return maps.Clone(m.profiles) }

// Apply moves the instance to next and appends a transition record.
//
// Order matters and is deliberate: transition integrity first (no dimension may
// be lost), then the declared per-dimension profile (no undeclared edge), then
// the six fixed legality rules, then governance and retention on the record.
// Any failure leaves the machine and its history untouched.
func (m *Machine) Apply(next Dimensions, ctx Context, rec TransitionRecord) error {
	if err := Preserves(m.current, next); err != nil {
		return err
	}
	rules, err := m.declaredEdges(m.current, next)
	if err != nil {
		return err
	}
	if err := Check(next, ctx); err != nil {
		return err
	}
	for _, r := range rules {
		if r.RequiresGovernance && rec.GovernanceDecisionRef == "" {
			return fmt.Errorf("%w: %s->%s requires a governance decision reference",
				ErrGovernanceBypassed, r.From, r.To)
		}
	}
	rec.Sequence = uint64(m.history.Len()) + 1
	rec.From = m.current
	rec.To = next
	if rec.RetentionClass == "" && len(rules) > 0 {
		rec.RetentionClass = rules[0].RetentionClass
	}
	if err := m.history.Append(rec); err != nil {
		return err
	}
	m.current = next
	return nil
}

// declaredEdges resolves the transition rule for every dimension that actually
// moves. A dimension that does not move needs no declared edge.
func (m *Machine) declaredEdges(from, to Dimensions) ([]TransitionRule, error) {
	var out []TransitionRule
	for _, dim := range AllDimensions() {
		fromState, toState := from.State(dim), to.State(dim)
		if fromState == toState {
			continue
		}
		p := m.profiles[dim]
		rule, ok := p.Rule(fromState, toState)
		if !ok {
			return nil, fmt.Errorf("%w: %s does not declare %s->%s",
				ErrUndeclaredTransition, p.ID, fromState, toState)
		}
		out = append(out, rule)
	}
	return out, nil
}

// ContextAt resolves the linked-record context that applied at one point in a
// recorded history. Replay needs it because the legality of a tuple depends on
// records (approvals, receipts, repairs) that themselves change over time.
type ContextAt func(rec TransitionRecord) Context

// Replay rebuilds a dimension tuple from recorded history and re-checks every
// intermediate tuple under the same rules a live command would face. A recorded
// history that contains an illegal tuple fails here rather than being repaired
// into a preferred status.
func Replay(profiles map[Dimension]Profile, initial Dimensions, records []TransitionRecord, at ContextAt) (Dimensions, error) {
	if at == nil {
		at = func(TransitionRecord) Context { return Context{} }
	}
	m, err := NewMachine(profiles, initial, at(TransitionRecord{To: initial}))
	if err != nil {
		return Dimensions{}, err
	}
	for _, rec := range records {
		if rec.From != m.Current() {
			return Dimensions{}, fmt.Errorf("%w: record %d starts at %v but replay stands at %v",
				ErrHistoryImmutable, rec.Sequence, rec.From, m.Current())
		}
		if err := m.Apply(rec.To, at(rec), rec); err != nil {
			return Dimensions{}, err
		}
	}
	return m.Current(), nil
}
