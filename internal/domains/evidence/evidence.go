// Package evidence holds the small vocabulary that every P1A domain
// calculation must carry with its result: how many effects it caused (always
// zero in P1A), which authority a fact came from, where the fact was observed,
// and the versioned zero-effect receipt that proves a preflight or simulation
// wrote nothing.
//
// Semantic owner: domains (shared). Phase: P1A.
//
// P1A is a paid observation, preflight and simulation release. Its whole
// commercial claim is that running it changes nothing. That claim is only
// worth something if it is a counted, digestable artifact rather than a
// sentence in a document, which is why EffectCounters is a struct on every
// result and Receipt refuses to be constructed over a non-zero count.
package evidence

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	countersSchema    = "hcmnext.domains.evidence.EffectCounters"
	authoritySchema   = "hcmnext.domains.evidence.SourceAuthority"
	provenanceSchema  = "hcmnext.domains.evidence.Provenance"
	receiptSchema     = "hcmnext.domains.evidence.ZeroEffectReceipt"
	evidenceSchemaVer = 1
)

// Evidence errors. All are matchable with errors.Is.
var (
	// ErrEffectsNotZero is returned when a zero-effect receipt is asked to
	// certify a result that counted at least one effect.
	ErrEffectsNotZero = errors.New("evidence: zero-effect receipt requires every effect count to be zero")
	// ErrNegativeCount is returned for a negative effect count.
	ErrNegativeCount = errors.New("evidence: effect counts cannot be negative")
	// ErrSourceAuthority is returned for an unspecified or incomplete authority.
	ErrSourceAuthority = errors.New("evidence: source authority is incomplete")
	// ErrProvenance is returned for provenance missing a source or evidence ref.
	ErrProvenance = errors.New("evidence: provenance requires a source, an evidence ref and a recorded time")
	// ErrReceipt is returned for a receipt missing required identity or versions.
	ErrReceipt = errors.New("evidence: receipt is missing required identity or control versions")
)

// EffectCounters counts everything a P1A intent is forbidden to do. Each field
// is a separate count rather than one boolean because "we wrote nothing" and
// "we enqueued nothing" are different promises to a design partner, and a
// failure of one should not be reported as a failure of the other.
type EffectCounters struct {
	// DomainWrites counts authoritative worker, employment, assignment,
	// organization, position, compensation and budget appends.
	DomainWrites int
	// Reservations counts budget or position holds acquired.
	Reservations int
	// WorkItems counts human work items created.
	WorkItems int
	// Timers counts durable timers armed.
	Timers int
	// Messages counts MessageIntents raised.
	Messages int
	// OutboxEntries counts durable external-effect intents enqueued.
	OutboxEntries int
	// ProviderCalls counts outbound calls made to an external system.
	ProviderCalls int
	// ApprovalBindings counts approval bindings recorded.
	ApprovalBindings int
}

// ZeroEffects returns the counters of a calculation that did nothing.
func ZeroEffects() EffectCounters { return EffectCounters{} }

// counts returns the fields in a fixed order for validation and encoding.
func (c EffectCounters) counts() [8]struct {
	name string
	n    int
} {
	return [8]struct {
		name string
		n    int
	}{
		{"domain_writes", c.DomainWrites},
		{"reservations", c.Reservations},
		{"work_items", c.WorkItems},
		{"timers", c.Timers},
		{"messages", c.Messages},
		{"outbox_entries", c.OutboxEntries},
		{"provider_calls", c.ProviderCalls},
		{"approval_bindings", c.ApprovalBindings},
	}
}

// Validate reports whether the counters are self-consistent.
func (c EffectCounters) Validate() error {
	for _, f := range c.counts() {
		if f.n < 0 {
			return fmt.Errorf("%w: %s = %d", ErrNegativeCount, f.name, f.n)
		}
	}
	return nil
}

// IsZero reports whether every count is zero.
func (c EffectCounters) IsZero() bool {
	for _, f := range c.counts() {
		if f.n != 0 {
			return false
		}
	}
	return true
}

// NonZero returns the names of the counts that are not zero, in field order.
// It exists so a failing assertion can say which promise was broken.
func (c EffectCounters) NonZero() []string {
	var out []string
	for _, f := range c.counts() {
		if f.n != 0 {
			out = append(out, fmt.Sprintf("%s=%d", f.name, f.n))
		}
	}
	return out
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (c EffectCounters) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New(countersSchema, evidenceSchemaVer)
	for _, f := range c.counts() {
		w.Int(f.name, int64(f.n))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// AuthorityKind says whether HCM Next owns a fact or is merely reporting one
// it observed somewhere else. The distinction is the whole point of the
// source-authority contract: an observed incumbent value must never be
// published as a local domain fact.
type AuthorityKind uint8

// Authority kinds.
const (
	// AuthorityUnspecified is the zero value and is never legal.
	AuthorityUnspecified AuthorityKind = iota
	// AuthorityLocal means HCM Next holds effective-dated source authority.
	AuthorityLocal
	// AuthorityExternalObservation means the value was observed from the system
	// of record and is reported, not owned.
	AuthorityExternalObservation
	// AuthorityDerived means the value was computed from other facts and has no
	// independent authority of its own.
	AuthorityDerived
)

var authorityWire = map[AuthorityKind]string{
	AuthorityLocal:               "LOCAL_AUTHORITATIVE",
	AuthorityExternalObservation: "EXTERNAL_OBSERVATION",
	AuthorityDerived:             "DERIVED",
}

// String returns the stable wire token, or "AUTHORITY_UNSPECIFIED".
func (k AuthorityKind) String() string {
	if s, ok := authorityWire[k]; ok {
		return s
	}
	return "AUTHORITY_UNSPECIFIED"
}

// Valid reports whether k is a legal authority kind.
func (k AuthorityKind) Valid() bool { _, ok := authorityWire[k]; return ok }

// SourceAuthority names the system that is authoritative for a fact and the
// effective-dated policy under which that authority was decided.
type SourceAuthority struct {
	Kind AuthorityKind
	// System is the authoritative system identifier, local or external.
	System string
	// PolicyRef identifies the authority-by-field decision that granted it.
	PolicyRef string
}

// Validate reports whether the authority is fully stated.
func (a SourceAuthority) Validate() error {
	if !a.Kind.Valid() {
		return fmt.Errorf("%w: kind is unspecified", ErrSourceAuthority)
	}
	if a.System == "" || a.PolicyRef == "" {
		return fmt.Errorf("%w: system %q policy %q", ErrSourceAuthority, a.System, a.PolicyRef)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (a SourceAuthority) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(authoritySchema, evidenceSchemaVer).
		String("kind", a.Kind.String()).
		String("system", a.System).
		String("policy_ref", a.PolicyRef).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// String returns "<KIND>:<system>@<policy>".
func (a SourceAuthority) String() string {
	if a.Validate() != nil {
		return ""
	}
	return a.Kind.String() + ":" + a.System + "@" + a.PolicyRef
}

// Provenance is where a fact came from and when it was recorded. Every
// disclosed fact carries one; a fact that cannot say where it came from is not
// an explanation, it is an assertion.
type Provenance struct {
	// Source is the observing or asserting system.
	Source string
	// EvidenceRef is the immutable artifact reference backing the assertion.
	EvidenceRef string
	// RecordedAt is when the assertion entered the record.
	RecordedAt values.RecordedAt
}

// Validate reports whether the provenance is complete.
func (p Provenance) Validate() error {
	if p.Source == "" || p.EvidenceRef == "" {
		return fmt.Errorf("%w: source %q evidence %q", ErrProvenance, p.Source, p.EvidenceRef)
	}
	if p.RecordedAt.Canonical() == nil {
		return fmt.Errorf("%w: recorded_at is unset", ErrProvenance)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p Provenance) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(provenanceSchema, evidenceSchemaVer).
		String("source", p.Source).
		String("evidence_ref", p.EvidenceRef).
		Value("recorded_at", p.RecordedAt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ControlVersion is one pinned control artifact: a rule pack, a catalog, a
// calendar dataset, a policy bundle. A P1A result cites every control it read
// so that a later replay can prove it read the same ones.
type ControlVersion struct {
	Name    string
	Version string
}

// Mode is the execution depth a receipt certifies.
type Mode string

// Modes. P1A never emits anything beyond these two.
const (
	// ModePreflight is validation before any simulation.
	ModePreflight Mode = "PREFLIGHT"
	// ModeSimulate is deterministic simulation with no effects.
	ModeSimulate Mode = "SIMULATE"
)

// Lifecycle values a P1A receipt may carry. ExecutionState never leaves
// NOT_PLANNED in this release.
const (
	// RequestStatePreflighted is the RequestState after a successful preflight.
	RequestStatePreflighted = "PREFLIGHTED"
	// RequestStateSimulated is the RequestState after a successful simulation.
	RequestStateSimulated = "SIMULATED"
	// ExecutionStateNotPlanned is the only ExecutionState P1A may record.
	ExecutionStateNotPlanned = "NOT_PLANNED"
)

// ZeroEffectReceipt is the artifact a P1A calculation hands back to prove it
// changed nothing. It binds the intent identity, the mode, the lifecycle
// dimensions it left the intent in, the exact control versions it read, and
// the digests of its input and its result.
//
// Construct one with NewZeroEffectReceipt; the constructor is the only place
// that checks the counters, so a receipt in hand always means zero effects.
type ZeroEffectReceipt struct {
	IntentType     string
	IntentVersion  string
	Mode           Mode
	RequestState   string
	ExecutionState string
	Controls       []ControlVersion
	InputsDigest   string
	ResultDigest   string
	Counters       EffectCounters
}

// NewZeroEffectReceipt validates and returns a receipt. It sorts the control
// versions so that two runs that read the same controls in a different order
// still produce the same canonical bytes, and it refuses outright when any
// effect was counted.
func NewZeroEffectReceipt(
	intentType, intentVersion string,
	mode Mode,
	requestState string,
	controls []ControlVersion,
	inputsDigest, resultDigest string,
	counters EffectCounters,
) (ZeroEffectReceipt, error) {
	if err := counters.Validate(); err != nil {
		return ZeroEffectReceipt{}, err
	}
	if !counters.IsZero() {
		return ZeroEffectReceipt{}, fmt.Errorf("%w: %v", ErrEffectsNotZero, counters.NonZero())
	}
	sorted := append([]ControlVersion(nil), controls...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].Version < sorted[j].Version
	})
	r := ZeroEffectReceipt{
		IntentType:     intentType,
		IntentVersion:  intentVersion,
		Mode:           mode,
		RequestState:   requestState,
		ExecutionState: ExecutionStateNotPlanned,
		Controls:       sorted,
		InputsDigest:   inputsDigest,
		ResultDigest:   resultDigest,
		Counters:       counters,
	}
	if err := r.Validate(); err != nil {
		return ZeroEffectReceipt{}, err
	}
	return r, nil
}

// Validate reports whether the receipt is complete and still zero-effect.
func (r ZeroEffectReceipt) Validate() error {
	switch {
	case r.IntentType == "" || r.IntentVersion == "":
		return fmt.Errorf("%w: intent %q/%q", ErrReceipt, r.IntentType, r.IntentVersion)
	case r.Mode != ModePreflight && r.Mode != ModeSimulate:
		return fmt.Errorf("%w: mode %q is not a P1A mode", ErrReceipt, r.Mode)
	case r.RequestState == "":
		return fmt.Errorf("%w: request state is empty", ErrReceipt)
	case r.ExecutionState != ExecutionStateNotPlanned:
		return fmt.Errorf("%w: execution state %q, P1A never leaves %s",
			ErrReceipt, r.ExecutionState, ExecutionStateNotPlanned)
	case r.InputsDigest == "" || r.ResultDigest == "":
		return fmt.Errorf("%w: receipt must cite an inputs digest and a result digest", ErrReceipt)
	case len(r.Controls) == 0:
		return fmt.Errorf("%w: receipt must pin at least one control version", ErrReceipt)
	}
	for _, c := range r.Controls {
		if c.Name == "" || c.Version == "" {
			return fmt.Errorf("%w: control %q@%q", ErrReceipt, c.Name, c.Version)
		}
	}
	if err := r.Counters.Validate(); err != nil {
		return err
	}
	if !r.Counters.IsZero() {
		return fmt.Errorf("%w: %v", ErrEffectsNotZero, r.Counters.NonZero())
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r ZeroEffectReceipt) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New(receiptSchema, evidenceSchemaVer).
		String("intent_type", r.IntentType).
		String("intent_version", r.IntentVersion).
		String("mode", string(r.Mode)).
		String("request_state", r.RequestState).
		String("execution_state", r.ExecutionState).
		Count("controls", len(r.Controls))
	for _, c := range r.Controls {
		w.String("control.name", c.Name).String("control.version", c.Version)
	}
	raw, err := w.
		String("inputs_digest", r.InputsDigest).
		String("result_digest", r.ResultDigest).
		Value("counters", r.Counters).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}
