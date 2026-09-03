package model

import (
	"errors"
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Sentinel causes for external-code crosswalks (MODEL-019). Classify with
// [errors.Is]; never by matching strings.
var (
	// ErrInvalidCrosswalkMapping reports a CrosswalkMapping that cannot be
	// published: no mapping reference, no external identity, no canonical
	// reference, no effective interval or no evidence.
	ErrInvalidCrosswalkMapping = errors.New("model: invalid crosswalk mapping")

	// ErrCrosswalkUnknown reports an external system/object/code with no
	// declared mapping at all: [ResolveCrosswalk] never guesses a canonical
	// value for a code it has never seen.
	ErrCrosswalkUnknown = errors.New("model: UNKNOWN crosswalk mapping")

	// ErrCrosswalkAmbiguous reports two or more mappings for the same
	// external identity, effective at the same instant, that disagree on the
	// canonical resource and carry no distinguishing precedence.
	ErrCrosswalkAmbiguous = errors.New("model: AMBIGUOUS crosswalk mapping")

	// ErrCrosswalkOutOfRange reports an external identity with declared
	// mappings, none of which is effective at the requested instant.
	ErrCrosswalkOutOfRange = errors.New("model: crosswalk mapping is OUT_OF_EFFECTIVE_RANGE")

	// ErrInvalidCrosswalkRevision reports a correction whose current mapping
	// does not share the previous mapping's external identity, or whose
	// mapping reference does not advance.
	ErrInvalidCrosswalkRevision = errors.New("model: invalid crosswalk revision")
)

// CrosswalkStatus is the typed outcome of resolving one external code, matching
// the MODEL-019 RED vocabulary exactly: a caller receives UNKNOWN, AMBIGUOUS
// or OUT_OF_EFFECTIVE_RANGE instead of a guessed canonical value.
type CrosswalkStatus string

// Crosswalk resolution statuses.
const (
	CrosswalkResolved   CrosswalkStatus = "RESOLVED"
	CrosswalkUnknown    CrosswalkStatus = "UNKNOWN"
	CrosswalkAmbiguous  CrosswalkStatus = "AMBIGUOUS"
	CrosswalkOutOfRange CrosswalkStatus = "OUT_OF_EFFECTIVE_RANGE"
)

// Valid reports whether s is one of the four declared statuses.
func (s CrosswalkStatus) Valid() bool {
	switch s {
	case CrosswalkResolved, CrosswalkUnknown, CrosswalkAmbiguous, CrosswalkOutOfRange:
		return true
	default:
		return false
	}
}

// CrosswalkMapping is one versioned, effective-dated mapping from an external
// system's code to a canonical resource (MODEL-019). Many mappings may name
// the same canonical resource (many-to-one); when two mappings for the same
// external identity are simultaneously effective, Precedence breaks the tie
// explicitly rather than by guessing.
type CrosswalkMapping struct {
	MappingRef string

	ExternalSystem string
	ObjectType     string
	ExternalCode   string

	CanonicalRef string

	Effective values.EffectiveInterval

	// Precedence orders competing mappings for the same external identity
	// when more than one canonical resource is simultaneously effective.
	// Higher wins. Two mappings to *different* canonical resources tied at
	// the highest precedence are ambiguous, not resolvable by luck.
	Precedence int

	EvidenceRef   string
	ProvenanceRef string
}

// Validate rejects a crosswalk mapping missing an identity, external
// identity, canonical target, effective interval or evidence.
func (m CrosswalkMapping) Validate() error {
	if m.MappingRef == "" {
		return newError("CrosswalkMapping.Validate", "mapping_ref", ErrInvalidCrosswalkMapping,
			"mapping carries no reference")
	}
	if m.ExternalSystem == "" || m.ObjectType == "" || m.ExternalCode == "" {
		return newError("CrosswalkMapping.Validate", "external_identity", ErrInvalidCrosswalkMapping,
			"%s declares an incomplete external identity (system=%q object=%q code=%q)",
			m.MappingRef, m.ExternalSystem, m.ObjectType, m.ExternalCode)
	}
	if m.CanonicalRef == "" {
		return newError("CrosswalkMapping.Validate", "canonical_ref", ErrInvalidCrosswalkMapping,
			"%s names no canonical resource", m.MappingRef)
	}
	if err := m.Effective.Validate(); err != nil {
		return newError("CrosswalkMapping.Validate", "effective", ErrInvalidCrosswalkMapping,
			"%s has no valid effective interval: %v", m.MappingRef, err)
	}
	if m.EvidenceRef == "" {
		return newError("CrosswalkMapping.Validate", "evidence_ref", ErrInvalidCrosswalkMapping,
			"%s names no evidence", m.MappingRef)
	}
	return nil
}

func externalIdentityKey(system, objectType, code string) string {
	return system + "\x00" + objectType + "\x00" + code
}

// CrosswalkResolution is the answer to "what canonical resource does this
// external code mean, as of this instant?"
type CrosswalkResolution struct {
	Status       CrosswalkStatus
	CanonicalRef string
	MappingRef   string

	// Candidates lists the distinct canonical references tied for the
	// resolution when Status is AMBIGUOUS, sorted for determinism.
	Candidates []string
}

// ResolveCrosswalk resolves one external (system, objectType, code) to
// exactly one canonical resource at asOf.
//
// It rejects, by returning a typed status alongside a classifiable error
// rather than a guess: a code with no declared mapping at all
// ([ErrCrosswalkUnknown]); a code whose declared mappings are all outside
// their effective window at asOf ([ErrCrosswalkOutOfRange]); and two or more
// simultaneously effective mappings for the code that disagree on the
// canonical resource with no distinguishing precedence
// ([ErrCrosswalkAmbiguous]). Exact match only: system, object type and code
// must match byte-for-byte.
func ResolveCrosswalk(mappings []CrosswalkMapping, externalSystem, objectType, externalCode string, asOf values.Instant) (CrosswalkResolution, error) {
	key := externalIdentityKey(externalSystem, objectType, externalCode)
	var any []CrosswalkMapping
	for _, m := range mappings {
		if externalIdentityKey(m.ExternalSystem, m.ObjectType, m.ExternalCode) == key {
			any = append(any, m)
		}
	}
	if len(any) == 0 {
		return CrosswalkResolution{Status: CrosswalkUnknown}, newError("ResolveCrosswalk", "external_identity", ErrCrosswalkUnknown,
			"%s/%s/%s has no declared mapping", externalSystem, objectType, externalCode)
	}

	var inWindow []CrosswalkMapping
	for _, m := range any {
		if ok, err := m.Effective.ContainsInstant(asOf); err == nil && ok {
			inWindow = append(inWindow, m)
		}
	}
	if len(inWindow) == 0 {
		return CrosswalkResolution{Status: CrosswalkOutOfRange}, newError("ResolveCrosswalk", "effective", ErrCrosswalkOutOfRange,
			"%s/%s/%s has no mapping effective at %s", externalSystem, objectType, externalCode, asOf)
	}

	maxPrecedence := inWindow[0].Precedence
	for _, m := range inWindow {
		if m.Precedence > maxPrecedence {
			maxPrecedence = m.Precedence
		}
	}
	top := map[string]CrosswalkMapping{}
	for _, m := range inWindow {
		if m.Precedence != maxPrecedence {
			continue
		}
		if existing, ok := top[m.CanonicalRef]; !ok || m.MappingRef < existing.MappingRef {
			top[m.CanonicalRef] = m
		}
	}
	if len(top) > 1 {
		refs := make([]string, 0, len(top))
		for ref := range top {
			refs = append(refs, ref)
		}
		sort.Strings(refs)
		return CrosswalkResolution{Status: CrosswalkAmbiguous, Candidates: refs}, newError("ResolveCrosswalk", "canonical_ref", ErrCrosswalkAmbiguous,
			"%s/%s/%s resolves to %d canonical resources at equal precedence: %v",
			externalSystem, objectType, externalCode, len(top), refs)
	}

	var winner CrosswalkMapping
	for _, m := range top {
		winner = m
	}
	return CrosswalkResolution{
		Status:       CrosswalkResolved,
		CanonicalRef: winner.CanonicalRef,
		MappingRef:   winner.MappingRef,
	}, nil
}

// ReverseLookupCrosswalk returns every mapping targeting canonicalRef and
// effective at asOf, sorted by external system, object type, then code. It
// returns [ErrCrosswalkUnknown] when no mapping ever named canonicalRef.
func ReverseLookupCrosswalk(mappings []CrosswalkMapping, canonicalRef string, asOf values.Instant) ([]CrosswalkMapping, error) {
	var everMapped bool
	var out []CrosswalkMapping
	for _, m := range mappings {
		if m.CanonicalRef != canonicalRef {
			continue
		}
		everMapped = true
		if ok, err := m.Effective.ContainsInstant(asOf); err == nil && ok {
			out = append(out, m)
		}
	}
	if !everMapped {
		return nil, newError("ReverseLookupCrosswalk", "canonical_ref", ErrCrosswalkUnknown,
			"%q is never a crosswalk target", canonicalRef)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ExternalSystem != out[j].ExternalSystem {
			return out[i].ExternalSystem < out[j].ExternalSystem
		}
		if out[i].ObjectType != out[j].ObjectType {
			return out[i].ObjectType < out[j].ObjectType
		}
		return out[i].ExternalCode < out[j].ExternalCode
	})
	return out, nil
}

// CrosswalkRevision is the append-only correction record MODEL-019's
// REFACTOR clause requires: a correction never edits Previous in place, it
// appends a new mapping version and carries both states forward.
type CrosswalkRevision struct {
	Previous CrosswalkMapping
	Current  CrosswalkMapping
	At       values.Instant
	ActorRef string
}

// Validate rejects a revision whose current mapping does not validate, does
// not share the previous mapping's external identity, or does not advance the
// mapping reference.
func (r CrosswalkRevision) Validate() error {
	if err := r.Previous.Validate(); err != nil {
		return newError("CrosswalkRevision.Validate", "previous", ErrInvalidCrosswalkRevision, "%v", err)
	}
	if err := r.Current.Validate(); err != nil {
		return newError("CrosswalkRevision.Validate", "current", ErrInvalidCrosswalkRevision, "%v", err)
	}
	prevKey := externalIdentityKey(r.Previous.ExternalSystem, r.Previous.ObjectType, r.Previous.ExternalCode)
	curKey := externalIdentityKey(r.Current.ExternalSystem, r.Current.ObjectType, r.Current.ExternalCode)
	if prevKey != curKey {
		return newError("CrosswalkRevision.Validate", "external_identity", ErrInvalidCrosswalkRevision,
			"revision changes external identity from %s to %s", r.Previous.MappingRef, r.Current.MappingRef)
	}
	if r.Current.MappingRef == r.Previous.MappingRef {
		return newError("CrosswalkRevision.Validate", "mapping_ref", ErrInvalidCrosswalkRevision,
			"%s does not advance the mapping reference", r.Current.MappingRef)
	}
	if !r.At.IsSet() {
		return newError("CrosswalkRevision.Validate", "at", ErrInvalidCrosswalkRevision,
			"%s revision carries no timestamp", r.Current.MappingRef)
	}
	if r.ActorRef == "" {
		return newError("CrosswalkRevision.Validate", "actor_ref", ErrInvalidCrosswalkRevision,
			"%s revision names no actor", r.Current.MappingRef)
	}
	return nil
}

// CrosswalkImpactAnalysis is what a correction triggers: whether the
// canonical target actually changed, and which declared consumers are
// affected by that change (MODEL-019 REFACTOR — "corrections append mapping
// revisions and trigger impact analysis").
type CrosswalkImpactAnalysis struct {
	MappingRef           string
	CanonicalRefChanged  bool
	PreviousCanonicalRef string
	CurrentCanonicalRef  string
	AffectedConsumers    []string
}

// ReviseCrosswalk appends a correction revision to a mapping and computes its
// impact analysis. consumers is the caller's declared, explicit list of
// consumers bound to the previous canonical resource; it is echoed back in
// AffectedConsumers only when the canonical target actually changed —
// impact analysis is never inferred from guesswork.
func ReviseCrosswalk(previous, current CrosswalkMapping, at values.Instant, actorRef string, consumers []string) (CrosswalkRevision, CrosswalkImpactAnalysis, error) {
	rev := CrosswalkRevision{Previous: previous, Current: current, At: at, ActorRef: actorRef}
	if err := rev.Validate(); err != nil {
		return CrosswalkRevision{}, CrosswalkImpactAnalysis{}, err
	}
	changed := previous.CanonicalRef != current.CanonicalRef
	impact := CrosswalkImpactAnalysis{
		MappingRef:           current.MappingRef,
		CanonicalRefChanged:  changed,
		PreviousCanonicalRef: previous.CanonicalRef,
		CurrentCanonicalRef:  current.CanonicalRef,
	}
	if changed {
		affected := append([]string(nil), consumers...)
		sort.Strings(affected)
		impact.AffectedConsumers = affected
	}
	return rev, impact, nil
}
