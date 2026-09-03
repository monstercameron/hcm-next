package telemetry

import (
	"errors"
	"fmt"
)

// AttributeClass says how an attribute value is permitted to be treated.
// The zero value, ClassUnspecified, classifies as ClassProhibited
// everywhere this package checks it: an attribute this package has never
// heard of fails closed rather than passing through as if it were safe
// (OBS-001 RED: "salary/medical/bank/case/prompt/payload data or
// worker/request identifiers are accepted as metric labels" must never
// happen for an unregistered key).
type AttributeClass string

// Published attribute classes.
const (
	ClassUnspecified           AttributeClass = ""
	ClassOperationalPublic     AttributeClass = "OPERATIONAL_PUBLIC"
	ClassOperationalRestricted AttributeClass = "OPERATIONAL_RESTRICTED"
	ClassProhibited            AttributeClass = "PROHIBITED"
)

func (c AttributeClass) valid() bool {
	switch c {
	case ClassOperationalPublic, ClassOperationalRestricted, ClassProhibited:
		return true
	default:
		return false
	}
}

// SignalKind names which telemetry signal an attribute definition applies
// to.
type SignalKind string

// Published signal kinds.
const (
	SignalResource SignalKind = "resource"
	SignalLog      SignalKind = "log"
	SignalSpan     SignalKind = "span"
	SignalMetric   SignalKind = "metric"
)

func (k SignalKind) valid() bool {
	switch k {
	case SignalResource, SignalLog, SignalSpan, SignalMetric:
		return true
	default:
		return false
	}
}

// AttributeDefinition is one registry-owned, allow-listed attribute key.
type AttributeDefinition struct {
	Key     string
	Class   AttributeClass
	Signals []SignalKind
	// MaxCardinality bounds distinct values for this key. It is required
	// (> 0) for any key allowed on SignalMetric: an unbounded label on a
	// metric is exactly the cardinality-explosion defect OBS-004 exists to
	// stop. It may be 0 (unbounded) for a key that is never allowed on
	// SignalMetric.
	MaxCardinality int
	Description    string
}

// Allow-list validation errors.
var (
	ErrAttributeKeyEmpty        = errors.New("telemetry: attribute key is empty")
	ErrAttributeDuplicateKey    = errors.New("telemetry: attribute key is registered twice")
	ErrAttributeInvalidClass    = errors.New("telemetry: attribute class is not published")
	ErrAttributeInvalidSignal   = errors.New("telemetry: attribute signal is not published")
	ErrAttributeProhibitedClass = errors.New("telemetry: attribute class must not be PROHIBITED in an allow-list entry")
	ErrAttributeUnboundedMetric = errors.New("telemetry: attribute is allowed on metric signals but has no cardinality bound")
	ErrAttributeUnknown         = errors.New("telemetry: attribute key is not in the allow-list")
)

// Allowlist is the registry-owned set of attribute keys telemetry may
// carry. It is the "destination allowlist" and classification registry
// OBS-001 requires: any key not explicitly registered classifies as
// ClassProhibited.
type Allowlist struct {
	defs map[string]AttributeDefinition
}

// NewAllowlist validates and compiles a set of attribute definitions. Two
// entries for the same key, an unpublished class or signal, an entry
// explicitly registered as PROHIBITED (denial is the *absence* of an
// entry, not a class value — an explicit PROHIBITED row would let a typo'd
// duplicate accidentally widen access instead of narrowing it) and a
// metric-eligible key with no cardinality bound are all rejected before the
// allow-list can be used to classify anything.
func NewAllowlist(defs ...AttributeDefinition) (*Allowlist, error) {
	compiled := make(map[string]AttributeDefinition, len(defs))
	for _, d := range defs {
		if d.Key == "" {
			return nil, ErrAttributeKeyEmpty
		}
		if _, exists := compiled[d.Key]; exists {
			return nil, fmt.Errorf("%w: %q", ErrAttributeDuplicateKey, d.Key)
		}
		if !d.Class.valid() {
			return nil, fmt.Errorf("%w: %q for key %q", ErrAttributeInvalidClass, d.Class, d.Key)
		}
		if d.Class == ClassProhibited {
			return nil, fmt.Errorf("%w: key %q", ErrAttributeProhibitedClass, d.Key)
		}
		if len(d.Signals) == 0 {
			return nil, fmt.Errorf("telemetry: attribute %q declares no allowed signals", d.Key)
		}
		allowsMetric := false
		for _, s := range d.Signals {
			if !s.valid() {
				return nil, fmt.Errorf("%w: %q for key %q", ErrAttributeInvalidSignal, s, d.Key)
			}
			if s == SignalMetric {
				allowsMetric = true
			}
		}
		if allowsMetric && d.MaxCardinality <= 0 {
			return nil, fmt.Errorf("%w: key %q", ErrAttributeUnboundedMetric, d.Key)
		}
		compiled[d.Key] = d
	}
	return &Allowlist{defs: compiled}, nil
}

// Lookup returns the published definition for key, if any.
func (a *Allowlist) Lookup(key string) (AttributeDefinition, bool) {
	if a == nil {
		return AttributeDefinition{}, false
	}
	d, ok := a.defs[key]
	return d, ok
}

// Classify returns the class of key. A nil Allowlist, or a key that was
// never registered, classifies as ClassProhibited: classification fails
// closed by construction, never by a caller remembering to check "ok".
func (a *Allowlist) Classify(key string) AttributeClass {
	d, ok := a.Lookup(key)
	if !ok {
		return ClassProhibited
	}
	return d.Class
}

// AllowsSignal reports whether key may attach to signal kind. An
// unregistered key never allows any signal.
func (a *Allowlist) AllowsSignal(key string, kind SignalKind) bool {
	d, ok := a.Lookup(key)
	if !ok {
		return false
	}
	for _, s := range d.Signals {
		if s == kind {
			return true
		}
	}
	return false
}

// Keys returns every registered key, for consistency checks against the
// published YAML contract.
func (a *Allowlist) Keys() []string {
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a.defs))
	for k := range a.defs {
		out = append(out, k)
	}
	return out
}
