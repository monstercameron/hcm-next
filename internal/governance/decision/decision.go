package decision

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

const contractVersion = 1

// Version reports the canonical contract version of this engine.
func Version() int { return contractVersion }

// State is the fail-closed result of governance composition.
type State string

const (
	Allow                     State = "ALLOW"
	AllowWithObligations      State = "ALLOW_WITH_OBLIGATIONS"
	Deny                      State = "DENY"
	ContradictoryRequirements State = "CONTRADICTORY_REQUIREMENTS"
	UnknownFailClosed         State = "UNKNOWN_FAIL_CLOSED"
)

// DecisionState is a descriptive alias for callers that prefer the longer
// name used by the governance contract.
type DecisionState = State

// Stable aliases for the wire states.
const (
	DecisionAllow                     = Allow
	DecisionAllowWithObligations      = AllowWithObligations
	DecisionDeny                      = Deny
	DecisionContradictoryRequirements = ContradictoryRequirements
	DecisionUnknownFailClosed         = UnknownFailClosed
)

func (s State) valid() bool {
	switch s {
	case Allow, AllowWithObligations, Deny, ContradictoryRequirements, UnknownFailClosed:
		return true
	default:
		return false
	}
}

// ApprovalState records whether a required approval is complete.
type ApprovalState string

const (
	ApprovalSatisfied ApprovalState = "SATISFIED"
	ApprovalPending   ApprovalState = "PENDING"
	ApprovalRejected  ApprovalState = "REJECTED"
	ApprovalUnknown   ApprovalState = "UNKNOWN"
)

// RuleRef identifies the exact rule that fired. Version is mandatory because
// a rule id without its published version is not reproducible evidence.
type RuleRef struct {
	ID      string
	Version string
}

// FiredRule is an expressive alias for RuleRef.
type FiredRule = RuleRef

// RulePackVersion pins one applicable rule-pack release.
type RulePackVersion struct {
	ID      string
	Version string
	Digest  string
}

// ControlSnapshot is the revalidated control context carried by a decision.
// Digest is the snapshot's own canonical identity; the descriptive references
// make the receipt useful even when a reader cannot fetch that snapshot.
type ControlSnapshot struct {
	Digest           string
	Capability       string
	PolicyBundle     string
	LegalContext     string
	Entitlement      string
	ReferenceData    string
	Workflow         string
	Connector        string
	Classification   string
	DLP              string
	DestinationTrust string
	PurposeResidency string
	SourceWatermark  string
}

// Context contains the material governance facts whose absence must never
// resolve to an allow. Values are references and labels, not protected data.
type Context struct {
	Principal           string
	Delegation          string
	Capability          string
	Resource            string
	Fields              []string
	CurrentOrganization string
	TargetOrganization  string
	Purpose             string
	Risk                string
	Authority           string
	Legal               string
}

// ApprovalRequirement is one proposal-bound approval slot and its satisfaction
// state. RuleID/RuleVersion optionally name the rule which required the slot.
type ApprovalRequirement struct {
	ID           string
	Version      string
	Satisfaction ApprovalState
	RuleID       string
	RuleVersion  string
}

// SoDVerdict is a versioned separation-of-duties finding.
type SoDVerdict struct {
	RuleID    string
	Version   string
	State     State
	Satisfied bool
	Reason    string
}

// Obligation is a typed duty. ID, Version and Scope form its identity for
// union/deduplication; Owner is evidence about who implements it.
type Obligation struct {
	ID      string
	Version string
	Scope   string
	Owner   string
}

// Subdecision is one source-authority result. Stale and Incomplete are
// explicit negative facts; absent context is handled as UNKNOWN_FAIL_CLOSED.
type Subdecision struct {
	ID           string
	Source       string
	State        State
	FiredRules   []RuleRef
	Restrictions []string
	Obligations  []Obligation
	Stale        bool
	Incomplete   bool
}

// PrecedenceRule gives a declared rank to a conflict key. Keys are normally
// decision states, but rule ids may also be used by a caller's policy table.
type PrecedenceRule struct {
	Key      string
	RuleID   string
	Version  string
	Priority int
}

// PrecedenceTable is deliberately a slice: its canonical meaning is the
// keyed set of rules, never the order in which rules happen to be evaluated.
type PrecedenceTable []PrecedenceRule

// DefaultPrecedence is the package's declared fail-closed precedence. A
// caller may provide a narrower table; a conflict involving an unranked key is
// then refused rather than guessed from input order.
var DefaultPrecedence = PrecedenceTable{
	{Key: string(Deny), Priority: 400},
	{Key: string(ContradictoryRequirements), Priority: 300},
	{Key: string(UnknownFailClosed), Priority: 200},
	{Key: string(AllowWithObligations), Priority: 100},
	{Key: string(Allow), Priority: 0},
}

// Inputs is the complete typed input set for one governance decision.
type Inputs struct {
	ProposalRevisionDigest string
	Context                Context
	ControlSnapshot        ControlSnapshot
	RulePackVersions       []RulePackVersion
	ApprovalRequirements   []ApprovalRequirement
	SoDVerdicts            []SoDVerdict
	Obligations            []Obligation
	Subdecisions           []Subdecision
	FiredRules             []RuleRef
	Precedence             PrecedenceTable
}

// GovernanceInputs and GovernanceRequest are compatibility names for the
// same pure composition input.
type GovernanceInputs = Inputs
type GovernanceRequest = Inputs

// Decision is the immutable governance decision record. Its Digest is over
// Canonical(), excluding the Digest and derived Explanation fields.
type Decision struct {
	State                  State
	ProposalRevisionDigest string
	Context                Context
	ControlSnapshot        ControlSnapshot
	RulePackVersions       []RulePackVersion
	ApprovalRequirements   []ApprovalRequirement
	SoDVerdicts            []SoDVerdict
	FiredRules             []RuleRef
	Restrictions           []string
	Obligations            []Obligation
	Precedence             PrecedenceTable
	Refusal                *Refusal
	Explanation            string
	Digest                 string
}

// GovernanceDecision is a compatibility name for Decision.
type GovernanceDecision = Decision

var (
	// ErrInvalidInput means a required typed identity or version is absent.
	ErrInvalidInput = errors.New("governance decision: invalid input")
	// ErrUnresolvedConflict is returned with a typed Refusal when a conflict
	// cannot be resolved by the declared precedence table.
	ErrUnresolvedConflict = errors.New("governance decision: unresolved conflict")
)

// Refusal is the typed refusal for a composition that cannot safely produce a
// decision. It intentionally contains only rule keys and states, never input
// values.
type Refusal struct {
	Code   string
	Keys   []string
	States []State
	Reason string
}

func (r Refusal) Error() string {
	return fmt.Sprintf("%s: %s (%s)", ErrUnresolvedConflict, r.Reason, strings.Join(r.Keys, ","))
}

func (r Refusal) Unwrap() error { return ErrUnresolvedConflict }

// Compose evaluates the complete input set as a pure, deterministic function.
func Compose(in Inputs) (Decision, error) {
	normalized, err := normalize(in)
	if err != nil {
		return Decision{}, err
	}

	d := Decision{
		State:                  UnknownFailClosed,
		ProposalRevisionDigest: normalized.ProposalRevisionDigest,
		Context:                normalized.Context,
		ControlSnapshot:        normalized.ControlSnapshot,
		RulePackVersions:       slices.Clone(normalized.RulePackVersions),
		ApprovalRequirements:   slices.Clone(normalized.ApprovalRequirements),
		SoDVerdicts:            slices.Clone(normalized.SoDVerdicts),
		FiredRules:             slices.Clone(normalized.FiredRules),
		Precedence:             slices.Clone(normalized.Precedence),
		Obligations:            slices.Clone(normalized.Obligations),
	}

	var outcomes []State
	for _, sub := range normalized.Subdecisions {
		outcomes = append(outcomes, sub.State)
		d.Restrictions = intersectRestrictions(d.Restrictions, sub.Restrictions)
		d.Obligations = append(d.Obligations, sub.Obligations...)
		if sub.Stale || sub.Incomplete {
			outcomes = append(outcomes, UnknownFailClosed)
		}
	}

	for _, approval := range normalized.ApprovalRequirements {
		switch approval.Satisfaction {
		case ApprovalRejected:
			outcomes = append(outcomes, Deny)
		case ApprovalUnknown:
			outcomes = append(outcomes, UnknownFailClosed)
		case ApprovalPending:
			d.Obligations = append(d.Obligations, Obligation{ID: approval.ID, Version: approval.Version, Scope: "approval"})
		}
	}
	for _, verdict := range normalized.SoDVerdicts {
		outcomes = append(outcomes, verdict.State)
	}

	d.Obligations = dedupeObligations(d.Obligations)
	state, refusal := resolve(outcomes, normalized.Precedence, normalized.FiredRules)
	if refusal != nil {
		d.State = ContradictoryRequirements
		d.Refusal = refusal
	} else {
		d.State = state
	}
	if d.State == Allow && len(d.Obligations) > 0 {
		d.State = AllowWithObligations
	}
	if !contextComplete(normalized.Context) && (d.State == Allow || d.State == AllowWithObligations) {
		d.State = UnknownFailClosed
	}
	d.Explanation = d.Explain()
	d.Digest = d.CanonicalDigest()
	if refusal != nil {
		return d, *refusal
	}
	return d, nil
}

// Evaluate is the explicit evaluator spelling used by governance callers.
func Evaluate(in Inputs) (Decision, error) { return Compose(in) }

func normalize(in Inputs) (Inputs, error) {
	if strings.TrimSpace(in.ProposalRevisionDigest) == "" {
		return Inputs{}, fmt.Errorf("%w: proposal revision digest is required", ErrInvalidInput)
	}
	if strings.TrimSpace(in.ControlSnapshot.Digest) == "" {
		return Inputs{}, fmt.Errorf("%w: control snapshot digest is required", ErrInvalidInput)
	}
	out := in
	out.Context.Fields = sortedStrings(in.Context.Fields)
	out.RulePackVersions = slices.Clone(in.RulePackVersions)
	sort.Slice(out.RulePackVersions, func(i, j int) bool {
		return rulePackKey(out.RulePackVersions[i]) < rulePackKey(out.RulePackVersions[j])
	})
	for _, p := range out.RulePackVersions {
		if p.ID == "" || p.Version == "" || p.Digest == "" {
			return Inputs{}, fmt.Errorf("%w: every rule-pack release needs id, version and digest", ErrInvalidInput)
		}
	}
	out.ApprovalRequirements = slices.Clone(in.ApprovalRequirements)
	for i := range out.ApprovalRequirements {
		a := &out.ApprovalRequirements[i]
		if a.ID == "" || a.Version == "" {
			return Inputs{}, fmt.Errorf("%w: every approval requirement needs id and version", ErrInvalidInput)
		}
		if a.Satisfaction == "" {
			a.Satisfaction = ApprovalUnknown
		}
		if a.Satisfaction != ApprovalSatisfied && a.Satisfaction != ApprovalPending && a.Satisfaction != ApprovalRejected && a.Satisfaction != ApprovalUnknown {
			return Inputs{}, fmt.Errorf("%w: approval %q has invalid satisfaction %q", ErrInvalidInput, a.ID, a.Satisfaction)
		}
	}
	sort.Slice(out.ApprovalRequirements, func(i, j int) bool {
		return approvalKey(out.ApprovalRequirements[i]) < approvalKey(out.ApprovalRequirements[j])
	})
	out.SoDVerdicts = slices.Clone(in.SoDVerdicts)
	for i := range out.SoDVerdicts {
		v := &out.SoDVerdicts[i]
		if v.RuleID == "" || v.Version == "" {
			return Inputs{}, fmt.Errorf("%w: every SoD verdict needs rule id and version", ErrInvalidInput)
		}
		if v.State == "" {
			if v.Satisfied {
				v.State = Allow
			} else {
				v.State = UnknownFailClosed
			}
		}
		if !v.State.valid() {
			return Inputs{}, fmt.Errorf("%w: SoD rule %q has invalid state %q", ErrInvalidInput, v.RuleID, v.State)
		}
	}
	sort.Slice(out.SoDVerdicts, func(i, j int) bool { return sodKey(out.SoDVerdicts[i]) < sodKey(out.SoDVerdicts[j]) })
	out.Obligations = dedupeObligations(in.Obligations)
	for _, o := range in.Obligations {
		if o.ID == "" || o.Version == "" {
			return Inputs{}, fmt.Errorf("%w: every obligation needs id and version", ErrInvalidInput)
		}
	}
	out.Subdecisions = slices.Clone(in.Subdecisions)
	for i := range out.Subdecisions {
		sub := &out.Subdecisions[i]
		if sub.ID == "" || sub.Source == "" || !sub.State.valid() {
			return Inputs{}, fmt.Errorf("%w: subdecision needs source, id and valid state", ErrInvalidInput)
		}
		for _, r := range sub.FiredRules {
			if r.ID == "" || r.Version == "" {
				return Inputs{}, fmt.Errorf("%w: subdecision %q has an incomplete fired rule", ErrInvalidInput, sub.ID)
			}
		}
		sub.FiredRules = normalizeRules(sub.FiredRules)
		sub.Restrictions = sortedStrings(sub.Restrictions)
		for _, o := range sub.Obligations {
			if o.ID == "" || o.Version == "" {
				return Inputs{}, fmt.Errorf("%w: subdecision %q has an incomplete obligation", ErrInvalidInput, sub.ID)
			}
		}
		sub.Obligations = dedupeObligations(sub.Obligations)
	}
	sort.Slice(out.Subdecisions, func(i, j int) bool { return out.Subdecisions[i].ID < out.Subdecisions[j].ID })
	for _, r := range in.FiredRules {
		if r.ID == "" || r.Version == "" {
			return Inputs{}, fmt.Errorf("%w: every fired rule needs id and version", ErrInvalidInput)
		}
	}
	out.FiredRules = normalizeRules(in.FiredRules)
	for _, a := range out.ApprovalRequirements {
		if a.RuleID != "" || a.RuleVersion != "" {
			if a.RuleID == "" || a.RuleVersion == "" {
				return Inputs{}, fmt.Errorf("%w: approval %q has incomplete rule reference", ErrInvalidInput, a.ID)
			}
			out.FiredRules = append(out.FiredRules, RuleRef{ID: a.RuleID, Version: a.RuleVersion})
		}
	}
	for _, v := range out.SoDVerdicts {
		out.FiredRules = append(out.FiredRules, RuleRef{ID: v.RuleID, Version: v.Version})
	}
	for _, sub := range out.Subdecisions {
		out.FiredRules = append(out.FiredRules, sub.FiredRules...)
	}
	out.FiredRules = normalizeRules(out.FiredRules)
	if len(out.Precedence) == 0 {
		out.Precedence = slices.Clone(DefaultPrecedence)
	} else {
		out.Precedence = slices.Clone(out.Precedence)
	}
	for _, p := range out.Precedence {
		if p.Key == "" && p.RuleID == "" {
			return Inputs{}, fmt.Errorf("%w: precedence entry has no key", ErrInvalidInput)
		}
	}
	sort.Slice(out.Precedence, func(i, j int) bool {
		ki, kj := precedenceKey(out.Precedence[i]), precedenceKey(out.Precedence[j])
		if ki != kj {
			return ki < kj
		}
		return out.Precedence[i].Priority > out.Precedence[j].Priority
	})
	return out, nil
}

func contextComplete(c Context) bool {
	for _, v := range []string{c.Principal, c.Delegation, c.Capability, c.Resource, c.CurrentOrganization, c.TargetOrganization, c.Purpose, c.Risk, c.Authority, c.Legal} {
		if strings.TrimSpace(v) == "" {
			return false
		}
	}
	return len(c.Fields) > 0
}

func resolve(outcomes []State, precedence PrecedenceTable, rules []RuleRef) (State, *Refusal) {
	if len(outcomes) == 0 {
		return UnknownFailClosed, nil
	}
	unique := make(map[State]bool)
	for _, s := range outcomes {
		unique[s] = true
	}
	if len(unique) == 1 {
		for s := range unique {
			return s, nil
		}
	}

	rank := func(key string) (int, bool) {
		found := false
		value := 0
		for _, p := range precedence {
			matches := p.Key == key || p.RuleID == key
			if !matches {
				for _, r := range rules {
					if r.ID == p.RuleID && r.Version == p.Version && r.ID == key {
						matches = true
						break
					}
				}
			}
			if matches {
				if found && value != p.Priority {
					return 0, false
				}
				found, value = true, p.Priority
			}
		}
		return value, found
	}

	keys := make([]string, 0, len(unique))
	for s := range unique {
		keys = append(keys, string(s))
	}
	sort.Strings(keys)
	winner := keys[0]
	winnerRank, ok := rank(winner)
	if !ok {
		return ContradictoryRequirements, newRefusal(keys, "conflict has no declared precedence")
	}
	for _, key := range keys[1:] {
		candidateRank, ranked := rank(key)
		if !ranked {
			return ContradictoryRequirements, newRefusal(keys, "conflict has no declared precedence")
		}
		if candidateRank > winnerRank {
			winner, winnerRank = key, candidateRank
		} else if candidateRank == winnerRank {
			return ContradictoryRequirements, newRefusal(keys, "conflict has tied precedence")
		}
	}
	return State(winner), nil
}

func newRefusal(keys []string, reason string) *Refusal {
	return &Refusal{Code: "UNRESOLVED_CONFLICT", Keys: slices.Clone(keys), States: statesFromKeys(keys), Reason: reason}
}

func statesFromKeys(keys []string) []State {
	out := make([]State, 0, len(keys))
	for _, key := range keys {
		out = append(out, State(key))
	}
	return out
}

func intersectRestrictions(current, next []string) []string {
	if len(next) == 0 {
		return slices.Clone(current)
	}
	if len(current) == 0 {
		return slices.Clone(next)
	}
	set := make(map[string]bool, len(current))
	for _, value := range current {
		set[value] = true
	}
	out := make([]string, 0, len(current))
	for _, value := range next {
		if set[value] {
			out = append(out, value)
		}
	}
	return sortedStrings(out)
}

func dedupeObligations(values []Obligation) []Obligation {
	out := slices.Clone(values)
	sort.Slice(out, func(i, j int) bool { return obligationKey(out[i]) < obligationKey(out[j]) })
	result := out[:0]
	for _, value := range out {
		if value.ID == "" && value.Version == "" && value.Scope == "" && value.Owner == "" {
			continue
		}
		if len(result) == 0 || obligationKey(result[len(result)-1]) != obligationKey(value) {
			result = append(result, value)
		}
	}
	return slices.Clone(result)
}

func normalizeRules(values []RuleRef) []RuleRef {
	out := slices.Clone(values)
	sort.Slice(out, func(i, j int) bool { return ruleKey(out[i]) < ruleKey(out[j]) })
	result := out[:0]
	for _, value := range out {
		if value.ID == "" && value.Version == "" {
			continue
		}
		if len(result) == 0 || ruleKey(result[len(result)-1]) != ruleKey(value) {
			result = append(result, value)
		}
	}
	return slices.Clone(result)
}

func sortedStrings(values []string) []string {
	out := slices.Clone(values)
	sort.Strings(out)
	return slices.Compact(out)
}

func ruleKey(v RuleRef) string             { return v.ID + "\x00" + v.Version }
func rulePackKey(v RulePackVersion) string { return v.ID + "\x00" + v.Version + "\x00" + v.Digest }
func approvalKey(v ApprovalRequirement) string {
	return v.ID + "\x00" + v.Version + "\x00" + string(v.Satisfaction) + "\x00" + v.RuleID + "\x00" + v.RuleVersion
}
func sodKey(v SoDVerdict) string { return v.RuleID + "\x00" + v.Version + "\x00" + string(v.State) }
func obligationKey(v Obligation) string {
	return v.ID + "\x00" + v.Version + "\x00" + v.Scope + "\x00" + v.Owner
}
func precedenceKey(v PrecedenceRule) string { return v.Key + "\x00" + v.RuleID + "\x00" + v.Version }

// Canonical returns the deterministic evidence encoding of d. The Digest and
// Explanation are derived and therefore are intentionally excluded.
func (d Decision) Canonical() []byte {
	e := canonicalEncoder{}
	e.field("schema", "hcmnext.governance.decision/v1")
	e.field("state", string(d.State))
	e.field("proposal_revision_digest", d.ProposalRevisionDigest)
	encodeContext(&e, d.Context)
	encodeControlSnapshot(&e, d.ControlSnapshot)
	for _, p := range d.RulePackVersions {
		e.field("rule_pack", rulePackKey(p))
	}
	for _, a := range d.ApprovalRequirements {
		e.field("approval", approvalKey(a))
	}
	for _, v := range d.SoDVerdicts {
		e.field("sod", sodKey(v)+"\x00"+fmt.Sprint(v.Satisfied)+"\x00"+v.Reason)
	}
	for _, r := range d.FiredRules {
		e.field("fired_rule", ruleKey(r))
	}
	for _, r := range d.Restrictions {
		e.field("restriction", r)
	}
	for _, o := range d.Obligations {
		e.field("obligation", obligationKey(o))
	}
	for _, p := range d.Precedence {
		e.field("precedence", precedenceKey(p)+"\x00"+fmt.Sprint(p.Priority))
	}
	if d.Refusal != nil {
		e.field("refusal.code", d.Refusal.Code)
		e.field("refusal.reason", d.Refusal.Reason)
		for _, key := range d.Refusal.Keys {
			e.field("refusal.key", key)
		}
	}
	return e.bytes()
}

// CanonicalDigest returns the sha256 digest of Canonical.
func (d Decision) CanonicalDigest() string {
	sum := sha256.Sum256(d.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CanonicalDigest returns the canonical digest for a decision value.
func CanonicalDigest(d Decision) string { return d.CanonicalDigest() }

// Digest returns the canonical digest for a decision value. It is a concise
// package-level spelling for callers that keep the decision as an interface.
func Digest(d Decision) string { return d.CanonicalDigest() }

// Explain renders a redaction-safe summary containing only decision metadata.
func (d Decision) Explain() string {
	if d.Refusal != nil {
		return fmt.Sprintf("governance decision state=%s refusal=%s rules=%d packs=%d obligations=%d",
			d.State, d.Refusal.Code, len(d.FiredRules), len(d.RulePackVersions), len(d.Obligations))
	}
	return fmt.Sprintf("governance decision state=%s rules=%d packs=%d approvals=%d sod=%d restrictions=%d obligations=%d",
		d.State, len(d.FiredRules), len(d.RulePackVersions), len(d.ApprovalRequirements), len(d.SoDVerdicts), len(d.Restrictions), len(d.Obligations))
}

// Explain is the package-level Explain-shaped symbol required of engine-like
// packages; it delegates to the decision record's method.
func Explain(d Decision) string { return d.Explain() }

type canonicalEncoder struct{ data []byte }

func (e *canonicalEncoder) field(label, value string) {
	e.data = append(e.data, byte(len(label)>>24), byte(len(label)>>16), byte(len(label)>>8), byte(len(label)))
	e.data = append(e.data, label...)
	e.data = append(e.data, byte(len(value)>>24), byte(len(value)>>16), byte(len(value)>>8), byte(len(value)))
	e.data = append(e.data, value...)
}

func (e *canonicalEncoder) bytes() []byte { return slices.Clone(e.data) }

func encodeContext(e *canonicalEncoder, c Context) {
	e.field("context.principal", c.Principal)
	e.field("context.delegation", c.Delegation)
	e.field("context.capability", c.Capability)
	e.field("context.resource", c.Resource)
	for _, f := range sortedStrings(c.Fields) {
		e.field("context.field", f)
	}
	e.field("context.current_organization", c.CurrentOrganization)
	e.field("context.target_organization", c.TargetOrganization)
	e.field("context.purpose", c.Purpose)
	e.field("context.risk", c.Risk)
	e.field("context.authority", c.Authority)
	e.field("context.legal", c.Legal)
}

func encodeControlSnapshot(e *canonicalEncoder, c ControlSnapshot) {
	for _, value := range []struct{ label, value string }{
		{"control.digest", c.Digest},
		{"control.capability", c.Capability},
		{"control.policy_bundle", c.PolicyBundle},
		{"control.legal_context", c.LegalContext},
		{"control.entitlement", c.Entitlement},
		{"control.reference_data", c.ReferenceData},
		{"control.workflow", c.Workflow},
		{"control.connector", c.Connector},
		{"control.classification", c.Classification},
		{"control.dlp", c.DLP},
		{"control.destination_trust", c.DestinationTrust},
		{"control.purpose_residency", c.PurposeResidency},
		{"control.source_watermark", c.SourceWatermark},
	} {
		e.field(value.label, value.value)
	}
}
