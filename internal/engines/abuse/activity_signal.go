package abuse

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrSignalIdentity    = errors.New("abuse: activity signal id is required")
	ErrSignalKind        = errors.New("abuse: activity signal kind is not in the governed vocabulary")
	ErrSignalRef         = errors.New("abuse: activity signal subject and actor references are required")
	ErrSignalTenant      = errors.New("abuse: activity signal tenant is required")
	ErrSignalObservedAt  = errors.New("abuse: activity signal observed-at is required")
	ErrSignalSourceEmpty = errors.New("abuse: activity signal source system is required")
	// ErrSignalRawContent is the security boundary: an ActivitySignal must
	// never carry the raw content of the activity it observed, only a
	// closed-vocabulary kind, opaque references, and governance metadata.
	ErrSignalRawContent = errors.New("abuse: activity signal must not carry raw activity content")
)

// Ref is an opaque reference to a subject or actor entity -- a type and an
// id, never the entity's data. An ActivitySignal names who was acted upon
// and who acted only by reference; resolving a Ref to an actual worker,
// account, or record is a concern of whatever governed store owns that
// entity, not of this engine.
type Ref struct {
	Type string
	ID   string
}

// Valid reports whether r names a concrete entity by reference.
func (r Ref) Valid() bool {
	return strings.TrimSpace(r.Type) != "" && strings.TrimSpace(r.ID) != ""
}

// ActivitySignal is one governed, minimized observation of activity. Its
// fields are deliberately closed: a signal kind drawn from the governed
// vocabulary, subject/actor references, a tenant, an observation instant,
// a source system, and (derived, never stored ad hoc) a classification and
// canonical digest. RawContent exists only so the publish/ingest boundary
// has something concrete to refuse -- a populated RawContent is always
// invalid; the field must stay empty in every real signal.
type ActivitySignal struct {
	ID           string
	Kind         SignalKind
	Subject      Ref
	Actor        Ref
	Tenant       string
	ObservedAt   time.Time
	SourceSystem string

	// RawContent MUST remain empty. It is not a supported channel for
	// activity content -- its presence is refused by Validate and Digest.
	RawContent string
}

// Validate enforces the ActivitySignal contract: identity, a governed
// kind, valid subject/actor references, tenant, observation instant,
// source system, and the absence of raw content.
func (s ActivitySignal) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return ErrSignalIdentity
	}
	if !s.Kind.Valid() {
		return ErrSignalKind
	}
	if !s.Subject.Valid() || !s.Actor.Valid() {
		return ErrSignalRef
	}
	if strings.TrimSpace(s.Tenant) == "" {
		return ErrSignalTenant
	}
	if s.ObservedAt.IsZero() {
		return ErrSignalObservedAt
	}
	if strings.TrimSpace(s.SourceSystem) == "" {
		return ErrSignalSourceEmpty
	}
	if strings.TrimSpace(s.RawContent) != "" {
		return ErrSignalRawContent
	}
	return nil
}

// Classification returns the closed-vocabulary classification of s's kind.
// Every governed kind has exactly one classification (see the
// TestTodo_ABUSE_001_Conformance test), so a valid signal's classification
// is never ambiguous or freely chosen by a caller.
func (s ActivitySignal) Classification() (string, bool) {
	return s.Kind.Classification()
}

// Digest returns a canonical, deterministic digest over s's governed
// content: kind, subject/actor references, tenant, observation instant
// (normalized to UTC RFC3339Nano so equal instants in different
// monotonic/location representations digest identically), and source
// system. RawContent is never included -- a signal carrying it is refused
// outright rather than digested.
func (s ActivitySignal) Digest() (string, error) {
	if strings.TrimSpace(s.RawContent) != "" {
		return "", ErrSignalRawContent
	}
	canon := struct {
		Kind         SignalKind
		Subject      Ref
		Actor        Ref
		Tenant       string
		ObservedAt   string
		SourceSystem string
	}{
		Kind:         s.Kind,
		Subject:      s.Subject,
		Actor:        s.Actor,
		Tenant:       s.Tenant,
		ObservedAt:   s.ObservedAt.UTC().Format(time.RFC3339Nano),
		SourceSystem: s.SourceSystem,
	}
	return digest(canon)
}
