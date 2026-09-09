package snapshot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	sourceRefSchema   = "hcmnext.engines.snapshot.SourceRef"
	provenanceSchema  = "hcmnext.engines.snapshot.Provenance"
	entrySchema       = "hcmnext.engines.snapshot.InputEntry"
	snapshotSchemaVer = 1
)

// ErrEntryIncomplete is returned when an InputEntry is missing any of the
// descriptors SNAPSHOT-001's RED clause names: semantic input name, owner,
// tenant, authority class, source, effective-at, known-at, revision, head,
// watermark, freshness, classification, provenance or reference/config
// version. It is matchable with errors.Is.
var ErrEntryIncomplete = errors.New("snapshot: entry is missing a required descriptor")

// AuthorityClass says who may assert an input entry's value: Human Capital Management Suite's own
// state, another system's reported observation, or a versioned reference or
// configuration artifact neither system owns as live business state.
//
// The set is closed and is the only field that distinguishes a native input
// from an external one -- InputEntry has one shape for every authority
// class, so a caller can never infer authority from which fields are present.
type AuthorityClass string

// Declared authority classes. AuthorityUnspecified is the zero value and is
// never a legal entry authority, though it is a legal (unconstrained)
// InputRequest.RequiredAuthority.
const (
	AuthorityUnspecified         AuthorityClass = ""
	AuthorityNativeState         AuthorityClass = "NATIVE_STATE"
	AuthorityExternalObservation AuthorityClass = "EXTERNAL_OBSERVATION"
	AuthorityReferenceConfig     AuthorityClass = "REFERENCE_CONFIG"
)

// Valid reports whether a is a declared, non-empty authority class.
func (a AuthorityClass) Valid() bool {
	switch a {
	case AuthorityNativeState, AuthorityExternalObservation, AuthorityReferenceConfig:
		return true
	default:
		return false
	}
}

// String returns the wire token, or "AUTHORITY_UNSPECIFIED" for the zero value.
func (a AuthorityClass) String() string {
	if a == AuthorityUnspecified {
		return "AUTHORITY_UNSPECIFIED"
	}
	return string(a)
}

// Classification is the disclosure-sensitivity label an input entry carries,
// independent of its authority class: a REFERENCE_CONFIG input can be
// PUBLIC, and a NATIVE_STATE input can be RESTRICTED. Resolve does not act on
// it; it only refuses an entry that omits it, so a downstream disclosure
// decision always has one to read.
type Classification string

// Declared classifications. ClassificationUnspecified is the zero value and
// is never legal on a resolved entry.
const (
	ClassificationUnspecified  Classification = ""
	ClassificationPublic       Classification = "PUBLIC"
	ClassificationInternal     Classification = "INTERNAL"
	ClassificationConfidential Classification = "CONFIDENTIAL"
	ClassificationRestricted   Classification = "RESTRICTED"
)

// Valid reports whether c is a declared, non-empty classification.
func (c Classification) Valid() bool {
	switch c {
	case ClassificationPublic, ClassificationInternal, ClassificationConfidential, ClassificationRestricted:
		return true
	default:
		return false
	}
}

// String returns the wire token, or "CLASSIFICATION_UNSPECIFIED" for the zero value.
func (c Classification) String() string {
	if c == ClassificationUnspecified {
		return "CLASSIFICATION_UNSPECIFIED"
	}
	return string(c)
}

// SourceRef names the system, connection and locator an input entry's value
// came from. It is deliberately one shape for every AuthorityClass: a native
// domain repository, an external connector and a reference-config catalog
// each fill System/Connection/Ref with their own vocabulary, but nothing
// about the struct itself says which kind of source it names.
type SourceRef struct {
	// System is the source system identifier, local or external (e.g.
	// "hcmnext.people", "workday", "hcmnext.reference.pay_bands").
	System string
	// Connection is the specific connection or instance within System.
	Connection string
	// Ref is the source-local locator for this exact entry (a table/query
	// name, an endpoint, a catalog key).
	Ref string
}

// Validate reports whether the source reference is complete.
func (s SourceRef) Validate() error {
	switch {
	case strings.TrimSpace(s.System) == "":
		return fmt.Errorf("%w: source has no system", ErrEntryIncomplete)
	case strings.TrimSpace(s.Connection) == "":
		return fmt.Errorf("%w: source %q has no connection", ErrEntryIncomplete, s.System)
	case strings.TrimSpace(s.Ref) == "":
		return fmt.Errorf("%w: source %q/%q has no ref", ErrEntryIncomplete, s.System, s.Connection)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (s SourceRef) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(sourceRefSchema, snapshotSchemaVer).
		String("system", s.System).
		String("connection", s.Connection).
		String("ref", s.Ref).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Provenance is where an input entry's assertion came from and when it
// entered the record. It mirrors the shape of a domain's own evidence
// vocabulary deliberately: this package cannot import internal/domains, so it
// keeps its own copy of the same three coordinates rather than reaching for
// one it is not allowed to depend on.
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
	switch {
	case strings.TrimSpace(p.Source) == "":
		return fmt.Errorf("%w: provenance has no source", ErrEntryIncomplete)
	case strings.TrimSpace(p.EvidenceRef) == "":
		return fmt.Errorf("%w: provenance %q has no evidence ref", ErrEntryIncomplete, p.Source)
	case p.RecordedAt.Canonical() == nil:
		return fmt.Errorf("%w: provenance %q has no recorded-at", ErrEntryIncomplete, p.Source)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p Provenance) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(provenanceSchema, snapshotSchemaVer).
		String("source", p.Source).
		String("evidence_ref", p.EvidenceRef).
		Value("recorded_at", p.RecordedAt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// InputEntry is one typed, source-neutral read: exactly one business input,
// named and owned, resolved from exactly one source under exactly one
// authority class, at one effective-at/known-at bitemporal coordinate, with
// its own revision, head, watermark and freshness evidence, a disclosure
// classification, a provenance and the reference/config version its meaning
// depends on.
//
// Every field is mandatory; see Validate. The struct has one shape for every
// AuthorityClass on purpose -- see the package doc.
type InputEntry struct {
	// Name is the stable, semantic input identifier (e.g.
	// "people.worker_facts", "workforce.budget_authority"), not a storage
	// table or column name.
	Name string
	// Owner is the domain or team that owns this input's meaning.
	Owner string
	// Tenant is the tenant this entry's value belongs to.
	Tenant values.TenantId
	// Authority says which kind of source produced this entry.
	Authority AuthorityClass
	// Source names the system, connection and locator the value came from.
	Source SourceRef
	// EffectiveAt is the business date this entry's value is asserted as of.
	EffectiveAt values.LocalDate
	// KnownAt is the knowledge cut-off this entry was resolved under.
	KnownAt values.KnownAt
	// Revision names this entry's own stream position.
	Revision values.RevisionToken
	// Head is the source-declared head/version identifier this entry was
	// read at (an opaque token the source itself defines: a changefeed
	// offset, a catalog etag, a dataset release tag).
	Head string
	// Watermark is the read position this entry's value was resolved
	// against, which a ConsistencyRequirement's per-input minimum is
	// evaluated against.
	Watermark values.RevisionToken
	// Freshness is when the underlying value was captured or observed,
	// distinct from KnownAt (when it became available to this resolve).
	Freshness values.RecordedAt
	// Classification is this entry's disclosure-sensitivity label.
	Classification Classification
	// Provenance records where the assertion came from and when it was
	// recorded.
	Provenance Provenance
	// ReferenceVersion names the reference/config dataset version this
	// entry's meaning depends on (a policy pack, a calendar release, a
	// classification taxonomy version -- whatever version pins how the
	// value should be interpreted).
	ReferenceVersion string
}

// Validate reports whether every required descriptor is present and
// well-formed. A defect anywhere returns ErrEntryIncomplete (or a wrapped
// kernel-value error), never a partial pass.
func (e InputEntry) Validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("%w: entry has no semantic input name", ErrEntryIncomplete)
	}
	if strings.TrimSpace(e.Owner) == "" {
		return fmt.Errorf("%w: entry %q has no owner", ErrEntryIncomplete, e.Name)
	}
	if err := e.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: entry %q tenant: %w", ErrEntryIncomplete, e.Name, err)
	}
	if !e.Authority.Valid() {
		return fmt.Errorf("%w: entry %q has no authority class", ErrEntryIncomplete, e.Name)
	}
	if err := e.Source.Validate(); err != nil {
		return fmt.Errorf("%w: entry %q: %w", ErrEntryIncomplete, e.Name, err)
	}
	if err := e.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: entry %q has no effective-at: %w", ErrEntryIncomplete, e.Name, err)
	}
	if e.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: entry %q has no known-at", ErrEntryIncomplete, e.Name)
	}
	if err := e.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: entry %q revision: %w", ErrEntryIncomplete, e.Name, err)
	}
	if !e.Revision.IsSpecified() {
		return fmt.Errorf("%w: entry %q has no revision", ErrEntryIncomplete, e.Name)
	}
	if strings.TrimSpace(e.Head) == "" {
		return fmt.Errorf("%w: entry %q has no head", ErrEntryIncomplete, e.Name)
	}
	if err := e.Watermark.Validate(); err != nil {
		return fmt.Errorf("%w: entry %q watermark: %w", ErrEntryIncomplete, e.Name, err)
	}
	if !e.Watermark.IsSpecified() {
		return fmt.Errorf("%w: entry %q has no watermark", ErrEntryIncomplete, e.Name)
	}
	if e.Freshness.Canonical() == nil {
		return fmt.Errorf("%w: entry %q has no freshness", ErrEntryIncomplete, e.Name)
	}
	if !e.Classification.Valid() {
		return fmt.Errorf("%w: entry %q has no classification", ErrEntryIncomplete, e.Name)
	}
	if err := e.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: entry %q: %w", ErrEntryIncomplete, e.Name, err)
	}
	if strings.TrimSpace(e.ReferenceVersion) == "" {
		return fmt.Errorf("%w: entry %q has no reference/config version", ErrEntryIncomplete, e.Name)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (e InputEntry) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(entrySchema, snapshotSchemaVer).
		String("name", e.Name).
		String("owner", e.Owner).
		String("tenant", string(e.Tenant)).
		String("authority", e.Authority.String()).
		Value("source", e.Source).
		Value("effective_at", e.EffectiveAt).
		Value("known_at", e.KnownAt).
		Value("revision", e.Revision).
		String("head", e.Head).
		Value("watermark", e.Watermark).
		Value("freshness", e.Freshness).
		String("classification", e.Classification.String()).
		Value("provenance", e.Provenance).
		String("reference_version", e.ReferenceVersion).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}
