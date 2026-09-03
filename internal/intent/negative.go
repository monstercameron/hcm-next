package intent

import "sort"

// NegativeState is a way a fact can fail to be a plain concrete value. A
// definition declares which of these it can actually encounter; every declared
// state must resolve to exactly one [NegativeAction].
type NegativeState uint8

// NegativeState values.
const (
	NegativeUnspecified NegativeState = iota
	NegativeUnknown
	NegativePartial
	NegativeDegraded
	NegativeAmbiguous
	NegativeRedacted
	NegativeUnavailable
	NegativeStale
	NegativeNotApplicable
	NegativeRepairRequired
	NegativeQuarantined
	NegativeWaived
	NegativeDisputed
)

var negativeStateNames = map[NegativeState]string{
	NegativeUnspecified:    "UNSPECIFIED",
	NegativeUnknown:        "UNKNOWN",
	NegativePartial:        "PARTIAL",
	NegativeDegraded:       "DEGRADED",
	NegativeAmbiguous:      "AMBIGUOUS",
	NegativeRedacted:       "REDACTED",
	NegativeUnavailable:    "UNAVAILABLE",
	NegativeStale:          "STALE",
	NegativeNotApplicable:  "NOT_APPLICABLE",
	NegativeRepairRequired: "REPAIR_REQUIRED",
	NegativeQuarantined:    "QUARANTINED",
	NegativeWaived:         "WAIVED",
	NegativeDisputed:       "DISPUTED",
}

func (s NegativeState) String() string { return enumName(negativeStateNames, s, "NegativeState") }

// Valid reports whether s is a declared negative state other than UNSPECIFIED.
func (s NegativeState) Valid() bool {
	_, ok := negativeStateNames[s]
	return ok && s != NegativeUnspecified
}

// MandatoryNegativeStates are the seven states a definition can never leave
// undecided once it declares them applicable. They are the ones where silently
// treating the value as absent would change a business answer.
func MandatoryNegativeStates() []NegativeState {
	return []NegativeState{
		NegativeUnknown,
		NegativePartial,
		NegativeDegraded,
		NegativeAmbiguous,
		NegativeRedacted,
		NegativeUnavailable,
		NegativeStale,
	}
}

// AllNegativeStates returns every declared negative state, in numeric order.
func AllNegativeStates() []NegativeState {
	out := make([]NegativeState, 0, len(negativeStateNames)-1)
	for s := range negativeStateNames {
		if s != NegativeUnspecified {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// NegativeAction is what the policy decides for one negative state. The set is
// closed: a policy that yields anything else is not a policy, it is a default.
type NegativeAction uint8

// NegativeAction values.
const (
	ActionUnspecified NegativeAction = iota
	ActionBlock
	ActionRouteHuman
	ActionAllowWithWarning
	ActionUseStale
	ActionCreateObligation
	ActionCreateRepair
	ActionDegrade
	ActionPermitClosure
)

var negativeActionNames = map[NegativeAction]string{
	ActionUnspecified:      "UNSPECIFIED",
	ActionBlock:            "BLOCK",
	ActionRouteHuman:       "ROUTE_HUMAN",
	ActionAllowWithWarning: "ALLOW_WITH_WARNING",
	ActionUseStale:         "USE_STALE",
	ActionCreateObligation: "CREATE_OBLIGATION",
	ActionCreateRepair:     "CREATE_REPAIR",
	ActionDegrade:          "DEGRADE",
	ActionPermitClosure:    "PERMIT_CLOSURE",
}

func (a NegativeAction) String() string { return enumName(negativeActionNames, a, "NegativeAction") }

// Valid reports whether a is one of the eight declared actions.
func (a NegativeAction) Valid() bool {
	return a != ActionUnspecified && a <= ActionPermitClosure
}

// NegativeActions returns the eight declared actions, in numeric order.
func NegativeActions() []NegativeAction {
	return []NegativeAction{
		ActionBlock, ActionRouteHuman, ActionAllowWithWarning, ActionUseStale,
		ActionCreateObligation, ActionCreateRepair, ActionDegrade, ActionPermitClosure,
	}
}

// NegativeStateRule is the decision for one negative state, together with the
// evidence, expiry and revalidation semantics that make the decision auditable.
type NegativeStateRule struct {
	Action NegativeAction

	// EvidenceRef names the evidence the action must record.
	EvidenceRef string

	// AuthorityRef names the authority that may take the action. An action that
	// nobody is authorized to take is not a policy.
	AuthorityRef string

	// ExpirySeconds bounds how long the decision holds. Zero means the decision
	// does not expire on its own, which is only legal for BLOCK and
	// NOT_APPLICABLE-style terminal decisions.
	ExpirySeconds uint32

	// RevalidationRef names the rule that re-decides the state later.
	RevalidationRef string
}

// NegativeStatePolicy is a reusable, referenced decision table. Definitions
// reference a policy by id; common policies are shared, never copied.
type NegativeStatePolicy struct {
	ID      string
	Version uint32
	Rules   map[NegativeState]NegativeStateRule
}

// Ref returns the canonical policy reference text.
func (p NegativeStatePolicy) Ref() string {
	return p.ID + "/v" + itoa(uint64(p.Version))
}

// Validate rejects a policy whose action is outside the eight declared actions
// or that omits the evidence, authority or revalidation semantics an action
// needs to be auditable.
func (p NegativeStatePolicy) Validate() error {
	if p.ID == "" {
		return newError("Validate", "policy_id", ErrInvalidNegativeStatePolicy, "policy has no id")
	}
	if p.Version == 0 {
		return newError("Validate", "policy_version", ErrInvalidNegativeStatePolicy,
			"policy %q has no version", p.ID)
	}
	if len(p.Rules) == 0 {
		return newError("Validate", "rules", ErrInvalidNegativeStatePolicy,
			"policy %q decides nothing", p.ID)
	}
	for _, state := range sortedStates(p.Rules) {
		rule := p.Rules[state]
		field := "rules[" + state.String() + "]"
		if !state.Valid() {
			return newError("Validate", field, ErrInvalidNegativeStatePolicy,
				"policy %q decides an unspecified state", p.ID)
		}
		if !rule.Action.Valid() {
			return newError("Validate", field, ErrInvalidNegativeStatePolicy,
				"policy %q yields %s, which is not one of the eight declared actions",
				p.ID, rule.Action)
		}
		if rule.EvidenceRef == "" {
			return newError("Validate", field, ErrInvalidNegativeStatePolicy,
				"policy %q decides %s without evidence", p.ID, state)
		}
		if rule.AuthorityRef == "" {
			return newError("Validate", field, ErrInvalidNegativeStatePolicy,
				"policy %q decides %s without an authority", p.ID, state)
		}
		// A decision that lets work continue must say when it is revisited.
		// BLOCK is the one action that needs no revalidation clock: nothing
		// proceeds on it.
		if rule.Action != ActionBlock && rule.RevalidationRef == "" {
			return newError("Validate", field, ErrInvalidNegativeStatePolicy,
				"policy %q decides %s as %s without a revalidation rule",
				p.ID, state, rule.Action)
		}
		if rule.Action == ActionUseStale && rule.ExpirySeconds == 0 {
			return newError("Validate", field, ErrInvalidNegativeStatePolicy,
				"policy %q allows USE_STALE for %s with no expiry", p.ID, state)
		}
	}
	return nil
}

// Decide resolves the action for one negative state.
func (p NegativeStatePolicy) Decide(state NegativeState) (NegativeStateRule, error) {
	rule, ok := p.Rules[state]
	if !ok {
		return NegativeStateRule{}, newError("Decide", "state", ErrMissingNegativeStatePolicy,
			"policy %q does not decide %s", p.ID, state)
	}
	return rule, nil
}

// States lists the states the policy decides, in numeric order.
func (p NegativeStatePolicy) States() []NegativeState { return sortedStates(p.Rules) }

func sortedStates(rules map[NegativeState]NegativeStateRule) []NegativeState {
	out := make([]NegativeState, 0, len(rules))
	for s := range rules {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
