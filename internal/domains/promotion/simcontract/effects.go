package simcontract

import (
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

// EffectStatus is the execution status every [SideEffect] in a simulation
// contract carries.
//
// It is a closed vocabulary of exactly one legal value.  A contract effect is
// always both SIMULATED (it was computed as a candidate the way PROMO-002 and
// PROMO-003 compute one) and NOT_EXECUTED (nothing ran, nothing was written,
// nothing was called).  There is no exported constructor path, setter or
// struct literal available to a caller outside this package that can produce
// any other value: [SideEffect.status] is unexported, so a caller can name a
// [SideEffect] only through [NewSideEffect], which always stamps this
// constant.  [TestTodo_PROMO_004_Security] proves that even a value built by
// this package's own tests is rejected by [SideEffect.Validate] unless it
// carries exactly this status.
type EffectStatus string

// EffectSimulatedNotExecuted is the sole legal [EffectStatus].
const EffectSimulatedNotExecuted EffectStatus = "SIMULATED/NOT_EXECUTED"

// String returns the wire token.
func (s EffectStatus) String() string { return string(s) }

// SideEffect is one typed change a promotion or manager-change candidate
// would make, carried by the contract for review -- and, by construction,
// never executed.
//
// Its vocabulary mirrors PROMO-002/003's own
// [github.com/monstercameron/hcm-next/internal/domains/promotion/simassign.ProposedEffect]:
// a contract composes what simassign and simcomp already decided, it does not
// invent a second effect shape for the same fact.
type SideEffect struct {
	EffectID        string
	Kind            string
	Participant     string
	DestinationRef  string
	Reversibility   string
	CompensationRef string
	ObservationRef  string

	// status is unexported. NewSideEffect is the only place that sets it, and
	// it always sets it to [EffectSimulatedNotExecuted].
	status EffectStatus
}

// NewSideEffect builds a fully stated side effect, always stamped
// [EffectSimulatedNotExecuted]. It is the only constructor: there is no other
// way, from outside this package, to produce a [SideEffect] whose Status
// validates.
func NewSideEffect(effectID, kind, participant, destinationRef, reversibility, compensationRef, observationRef string) (SideEffect, error) {
	e := SideEffect{
		EffectID:        effectID,
		Kind:            kind,
		Participant:     participant,
		DestinationRef:  destinationRef,
		Reversibility:   reversibility,
		CompensationRef: compensationRef,
		ObservationRef:  observationRef,
		status:          EffectSimulatedNotExecuted,
	}
	if err := e.Validate(); err != nil {
		return SideEffect{}, err
	}
	return e, nil
}

// Status returns the effect's execution status. It is always
// [EffectSimulatedNotExecuted] for a value built through [NewSideEffect].
func (e SideEffect) Status() EffectStatus { return e.status }

// Validate rejects an underdeclared effect or one whose status is not the
// sole legal value -- which is what makes an effect built by any means other
// than [NewSideEffect] (for example a bare struct literal inside this
// package's own tests, the one place the unexported field is reachable at
// all) fail closed rather than silently pass as "executed".
func (e SideEffect) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"effect_id", e.EffectID},
		{"kind", e.Kind},
		{"participant", e.Participant},
		{"destination_ref", e.DestinationRef},
		{"reversibility", e.Reversibility},
		{"compensation_ref", e.CompensationRef},
		{"observation_ref", e.ObservationRef},
	} {
		if strings.TrimSpace(req.value) == "" {
			return fmt.Errorf("%w: side effect %q declares no %s", ErrInvalidInput, e.EffectID, req.field)
		}
	}
	if e.status != EffectSimulatedNotExecuted {
		return fmt.Errorf("%w: side effect %q carries status %q, want %q",
			ErrInvalidInput, e.EffectID, e.status, EffectSimulatedNotExecuted)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (e SideEffect) Canonical() []byte {
	if err := e.Validate(); err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.simcontract.SideEffect", schemaVersion).
		String("effect_id", e.EffectID).
		String("kind", e.Kind).
		String("participant", e.Participant).
		String("destination_ref", e.DestinationRef).
		String("reversibility", e.Reversibility).
		String("compensation_ref", e.CompensationRef).
		String("observation_ref", e.ObservationRef).
		String("status", string(e.status)).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}
