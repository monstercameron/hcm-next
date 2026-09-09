// Package intelligence owns the governed reconstruction of what happened to a
// business transaction: which intent was requested, by whom, what the platform
// decided, what it wrote, what it emitted, what was observed afterwards and
// what any of that was reconciled or repaired against.
//
// Semantic owner: intelligence and provenance. Phase: P1A.
//
// The explanation is assembled from recorded evidence and nothing else. It
// does not narrate, it does not infer, and it does not fill gaps. Every link
// it reports was written down by the system that owned it; every element
// carries an epistemic label saying whether it is an asserted fact, an
// external observation or a derived value; and every link carries a basis
// which has exactly one legal value, RECORDED_REFERENCE. There is no way in
// this package to express "A probably caused B", which is the whole point: an
// explanation that can express a causal guess will eventually be read as if it
// had proved one.
package intelligence

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	intelSchemaVer = 1
)

// KindTransaction is the entity kind of a business transaction reference.
const KindTransaction values.Kind = "business_transaction"

// Intelligence errors. All are matchable with errors.Is.
var (
	// ErrRecordInvalid is returned for a malformed transaction record.
	ErrRecordInvalid = errors.New("intelligence: transaction record is malformed")
	// ErrSection is returned for an unknown section identifier.
	ErrSection = errors.New("intelligence: unknown explanation section")
	// ErrLinkBasis is returned for a link whose basis is not a recorded
	// reference. There is no other legal basis: a link that exists because
	// something looked related is a correlation, and this package does not
	// publish correlations as evidence.
	ErrLinkBasis = errors.New("intelligence: link basis must be a recorded reference")
	// ErrObservationAuthority is returned when an external observation claims
	// local authority. An observed incumbent value is reported, never owned;
	// promoting one to a domain fact is how a platform starts asserting things
	// no one told it.
	ErrObservationAuthority = errors.New("intelligence: external observation must carry EXTERNAL_OBSERVATION authority")
	// ErrAuthorizationInvalid is returned for a malformed authorization input.
	ErrAuthorizationInvalid = errors.New("intelligence: authorization decision is malformed")
	// ErrAuthorizationIncomplete is returned when the decision does not rule
	// on every requested section.
	ErrAuthorizationIncomplete = errors.New("intelligence: authorization decision does not rule on every requested section")
	// ErrRequestInvalid is returned for a malformed explanation request.
	ErrRequestInvalid = errors.New("intelligence: explain request is invalid")
	// ErrReaderFailed wraps a failure from the transaction history port.
	ErrReaderFailed = errors.New("intelligence: transaction history reader failed")
	// ErrPortWidenedProjection is returned when the port answers about a
	// different transaction or returns a section that was not requested.
	ErrPortWidenedProjection = errors.New("intelligence: history port answered outside the requested projection")
)

// Section is one part of a transaction explanation. Sections are the unit of
// authorization: a caller is granted the chronology without the write set, or
// the governance record without the payload, and the explanation reports which
// parts it withheld rather than silently shrinking.
type Section string

// Sections.
const (
	// SectionRequest is what was asked for, by whom, under what purpose.
	SectionRequest Section = "REQUEST"
	// SectionGovernance is the control versions and policy snapshot.
	SectionGovernance Section = "GOVERNANCE"
	// SectionLifecycle is the ordered lifecycle transitions.
	SectionLifecycle Section = "LIFECYCLE"
	// SectionWriteSet is what the transaction wrote.
	SectionWriteSet Section = "WRITE_SET"
	// SectionEvents is the ledger events it appended.
	SectionEvents Section = "EVENTS"
	// SectionEffects is the outbox entries and provider calls it produced.
	SectionEffects Section = "EFFECTS"
	// SectionObservations is what was observed externally afterwards.
	SectionObservations Section = "OBSERVATIONS"
	// SectionReconciliation is the reconciliation outcomes.
	SectionReconciliation Section = "RECONCILIATION"
	// SectionRepair is the repair plans raised against it.
	SectionRepair Section = "REPAIR"
	// SectionProjections is the derived projections and their freshness.
	SectionProjections Section = "PROJECTIONS"
	// SectionEvidence is the immutable evidence artifact references.
	SectionEvidence Section = "EVIDENCE"
)

var knownSections = map[Section]struct{}{
	SectionRequest:        {},
	SectionGovernance:     {},
	SectionLifecycle:      {},
	SectionWriteSet:       {},
	SectionEvents:         {},
	SectionEffects:        {},
	SectionObservations:   {},
	SectionReconciliation: {},
	SectionRepair:         {},
	SectionProjections:    {},
	SectionEvidence:       {},
}

// Validate reports whether s is a defined section.
func (s Section) Validate() error {
	if _, ok := knownSections[s]; !ok {
		return fmt.Errorf("%w: %q", ErrSection, string(s))
	}
	return nil
}

// String returns the section token.
func (s Section) String() string { return string(s) }

// AllSections returns every defined section in sorted order. It is the default
// projection for a request that does not narrow itself.
func AllSections() []Section {
	out := make([]Section, 0, len(knownSections))
	for s := range knownSections {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Redactable field tokens inside the REQUEST section. Section-level
// authorization is too coarse for these: a caller may legitimately see that a
// promotion was requested without being allowed to see who requested it or on
// whose behalf.
const (
	// FieldRequestPurpose is the declared purpose of use.
	FieldRequestPurpose = "request.purpose"
	// FieldRequestRequester is the requesting principal.
	FieldRequestRequester = "request.requester"
	// FieldRequestSubjects is the set of subjects the request was about.
	FieldRequestSubjects = "request.subjects"
)

var redactableFields = map[string]struct{}{
	FieldRequestPurpose:   {},
	FieldRequestRequester: {},
	FieldRequestSubjects:  {},
}

// RedactableFields returns every redactable field token in sorted order.
func RedactableFields() []string {
	out := make([]string, 0, len(redactableFields))
	for f := range redactableFields {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// LinkBasis is why one record points at another. There is exactly one legal
// value, and it is not "we noticed a pattern".
type LinkBasis uint8

// Link bases.
const (
	// BasisUnspecified is the zero value and is never legal.
	BasisUnspecified LinkBasis = iota
	// BasisRecordedReference means the link was written into the record by the
	// system that owned it.
	BasisRecordedReference
)

// String returns the stable wire token.
func (b LinkBasis) String() string {
	if b == BasisRecordedReference {
		return "RECORDED_REFERENCE"
	}
	return "LINK_BASIS_UNSPECIFIED"
}

// Validate reports whether the basis is legal.
func (b LinkBasis) Validate() error {
	if b != BasisRecordedReference {
		return fmt.Errorf("%w: %s", ErrLinkBasis, b)
	}
	return nil
}

// Epistemic labels what kind of knowledge an element is. It travels with every
// element so that a reader never has to infer from position whether something
// was asserted by this platform or merely seen somewhere else.
type Epistemic uint8

// Epistemic labels.
const (
	// EpistemicUnspecified is the zero value and is never legal in a result.
	EpistemicUnspecified Epistemic = iota
	// EpistemicAsserted is a fact this platform recorded under its own
	// authority.
	EpistemicAsserted
	// EpistemicObserved is a value seen in an external system.
	EpistemicObserved
	// EpistemicDerived is a computed value with no independent authority.
	EpistemicDerived
	// EpistemicUnknown is something the record could not establish.
	EpistemicUnknown
	// EpistemicRedacted is something the caller may not see.
	EpistemicRedacted
	// EpistemicAbsent is something that does not exist for this transaction.
	EpistemicAbsent
)

var epistemicWire = map[Epistemic]string{
	EpistemicAsserted: "ASSERTED",
	EpistemicObserved: "OBSERVED",
	EpistemicDerived:  "DERIVED",
	EpistemicUnknown:  "UNKNOWN",
	EpistemicRedacted: "REDACTED",
	EpistemicAbsent:   "ABSENT",
}

// String returns the stable wire token, or "EPISTEMIC_UNSPECIFIED".
func (e Epistemic) String() string {
	if s, ok := epistemicWire[e]; ok {
		return s
	}
	return "EPISTEMIC_UNSPECIFIED"
}

// Valid reports whether e is a legal label.
func (e Epistemic) Valid() bool { _, ok := epistemicWire[e]; return ok }

// PrincipalRef identifies who acted. OnBehalfOf is separate from Id because a
// delegated action and a direct one are different facts, and an explanation
// that flattened them would let a delegate's action be read as the
// principal's.
type PrincipalRef struct {
	Kind       string
	Id         string
	OnBehalfOf string
}

// Validate reports whether the principal is stated.
func (p PrincipalRef) Validate() error {
	if p.Kind == "" || p.Id == "" {
		return fmt.Errorf("%w: principal kind %q id %q", ErrRecordInvalid, p.Kind, p.Id)
	}
	return nil
}

// String returns "<kind>:<id>" or "<kind>:<id>@<on behalf of>".
func (p PrincipalRef) String() string {
	if p.OnBehalfOf == "" {
		return p.Kind + ":" + p.Id
	}
	return p.Kind + ":" + p.Id + "@" + p.OnBehalfOf
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p PrincipalRef) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.PrincipalRef", intelSchemaVer).
		String("kind", p.Kind).
		String("id", p.Id).
		String("on_behalf_of", p.OnBehalfOf).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Request is what was asked for.
type Request struct {
	IntentType    string
	IntentVersion string
	Family        string
	Mode          string
	Purpose       string
	Requester     PrincipalRef
	RequestedAt   values.RecordedAt
	// RequestDigest and ProposalDigest pin the immutable request and the
	// proposal it bound, when there was one.
	RequestDigest  string
	ProposalDigest string
	Subjects       []values.EntityRef
}

// Validate reports whether the request record is complete.
func (r Request) Validate() error {
	if r.IntentType == "" || r.IntentVersion == "" {
		return fmt.Errorf("%w: request intent %q/%q", ErrRecordInvalid, r.IntentType, r.IntentVersion)
	}
	if r.Family == "" || r.Mode == "" {
		return fmt.Errorf("%w: request family %q mode %q", ErrRecordInvalid, r.Family, r.Mode)
	}
	if r.RequestDigest == "" {
		return fmt.Errorf("%w: request carries no digest", ErrRecordInvalid)
	}
	if r.RequestedAt.Canonical() == nil {
		return fmt.Errorf("%w: request has no recorded time", ErrRecordInvalid)
	}
	if err := r.Requester.Validate(); err != nil {
		return err
	}
	for _, s := range r.Subjects {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("%w: request subject: %w", ErrRecordInvalid, err)
		}
	}
	return nil
}

// Transition is one recorded lifecycle move.
type Transition struct {
	// Dimension names which lifecycle dimension moved.
	Dimension string
	From, To  string
	At        values.RecordedAt
	Actor     PrincipalRef
	// Reason is a stable policy or business token, never prose.
	Reason string
	// Sequence is the position in the transaction's own stream.
	Sequence uint64
	Digest   string
}

// Validate reports whether the transition is complete.
func (t Transition) Validate() error {
	if t.Dimension == "" || t.To == "" {
		return fmt.Errorf("%w: transition dimension %q to %q", ErrRecordInvalid, t.Dimension, t.To)
	}
	if t.At.Canonical() == nil {
		return fmt.Errorf("%w: transition %s has no recorded time", ErrRecordInvalid, t.Dimension)
	}
	if t.Digest == "" {
		return fmt.Errorf("%w: transition %s carries no digest", ErrRecordInvalid, t.Dimension)
	}
	return t.Actor.Validate()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (t Transition) Canonical() []byte {
	if t.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.Transition", intelSchemaVer).
		String("dimension", t.Dimension).
		String("from", t.From).
		String("to", t.To).
		Value("at", t.At).
		Value("actor", t.Actor).
		String("reason", t.Reason).
		Int("sequence", int64(t.Sequence)).
		String("digest", t.Digest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// WriteSetEntry is one thing the transaction wrote.
type WriteSetEntry struct {
	Resource string
	Kind     string
	Revision values.RevisionToken
	Digest   string
}

// Validate reports whether the entry is complete.
func (w WriteSetEntry) Validate() error {
	if w.Resource == "" || w.Kind == "" || w.Digest == "" {
		return fmt.Errorf("%w: write set entry %q/%q/%q",
			ErrRecordInvalid, w.Resource, w.Kind, w.Digest)
	}
	if !w.Revision.IsSpecified() {
		return fmt.Errorf("%w: write set entry %s has no revision", ErrRecordInvalid, w.Resource)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (w WriteSetEntry) Canonical() []byte {
	if w.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.WriteSetEntry", intelSchemaVer).
		String("resource", w.Resource).
		String("kind", w.Kind).
		Value("revision", w.Revision).
		String("digest", w.Digest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// LedgerEvent is one appended ledger event.
type LedgerEvent struct {
	ID       string
	Type     string
	At       values.RecordedAt
	Revision values.RevisionToken
	Digest   string
}

// Validate reports whether the event is complete.
func (e LedgerEvent) Validate() error {
	if e.ID == "" || e.Type == "" || e.Digest == "" {
		return fmt.Errorf("%w: ledger event %q/%q/%q", ErrRecordInvalid, e.ID, e.Type, e.Digest)
	}
	if e.At.Canonical() == nil {
		return fmt.Errorf("%w: ledger event %s has no recorded time", ErrRecordInvalid, e.ID)
	}
	if !e.Revision.IsSpecified() {
		return fmt.Errorf("%w: ledger event %s has no revision", ErrRecordInvalid, e.ID)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (e LedgerEvent) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.LedgerEvent", intelSchemaVer).
		String("id", e.ID).
		String("type", e.Type).
		Value("at", e.At).
		Value("revision", e.Revision).
		String("digest", e.Digest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// EffectRecord is one durable external-effect intent or provider call.
type EffectRecord struct {
	ID      string
	Channel string
	// State is the recorded delivery state, never inferred from silence.
	State  string
	At     values.RecordedAt
	Digest string
}

// Validate reports whether the effect record is complete.
func (e EffectRecord) Validate() error {
	if e.ID == "" || e.Channel == "" || e.State == "" || e.Digest == "" {
		return fmt.Errorf("%w: effect %q/%q/%q", ErrRecordInvalid, e.ID, e.Channel, e.State)
	}
	if e.At.Canonical() == nil {
		return fmt.Errorf("%w: effect %s has no recorded time", ErrRecordInvalid, e.ID)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (e EffectRecord) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.EffectRecord", intelSchemaVer).
		String("id", e.ID).
		String("channel", e.Channel).
		String("state", e.State).
		Value("at", e.At).
		String("digest", e.Digest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ObservationLink is one external observation recorded against this
// transaction. Its authority is checked, not assumed.
type ObservationLink struct {
	ID        string
	Source    string
	At        values.RecordedAt
	Digest    string
	Authority evidence.SourceAuthority
	Basis     LinkBasis
}

// Validate reports whether the link is complete and correctly attributed.
func (o ObservationLink) Validate() error {
	if o.ID == "" || o.Source == "" || o.Digest == "" {
		return fmt.Errorf("%w: observation %q/%q/%q", ErrRecordInvalid, o.ID, o.Source, o.Digest)
	}
	if o.At.Canonical() == nil {
		return fmt.Errorf("%w: observation %s has no recorded time", ErrRecordInvalid, o.ID)
	}
	if err := o.Basis.Validate(); err != nil {
		return err
	}
	if err := o.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: observation %s: %w", ErrRecordInvalid, o.ID, err)
	}
	if o.Authority.Kind != evidence.AuthorityExternalObservation {
		return fmt.Errorf("%w: observation %s claims %s", ErrObservationAuthority, o.ID, o.Authority.Kind)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (o ObservationLink) Canonical() []byte {
	if o.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.ObservationLink", intelSchemaVer).
		String("id", o.ID).
		String("source", o.Source).
		Value("at", o.At).
		String("digest", o.Digest).
		Value("authority", o.Authority).
		String("basis", o.Basis.String()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ReconciliationLink is one recorded reconciliation outcome.
type ReconciliationLink struct {
	ID      string
	Outcome string
	At      values.RecordedAt
	Digest  string
	Basis   LinkBasis
}

// Validate reports whether the link is complete.
func (r ReconciliationLink) Validate() error {
	if r.ID == "" || r.Outcome == "" || r.Digest == "" {
		return fmt.Errorf("%w: reconciliation %q/%q/%q", ErrRecordInvalid, r.ID, r.Outcome, r.Digest)
	}
	if r.At.Canonical() == nil {
		return fmt.Errorf("%w: reconciliation %s has no recorded time", ErrRecordInvalid, r.ID)
	}
	return r.Basis.Validate()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r ReconciliationLink) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.ReconciliationLink", intelSchemaVer).
		String("id", r.ID).
		String("outcome", r.Outcome).
		Value("at", r.At).
		String("digest", r.Digest).
		String("basis", r.Basis.String()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// RepairLink is one repair plan raised against this transaction. Executable is
// carried explicitly so an explanation can state that a plan existed and was
// never executable, which is the whole P1A claim.
type RepairLink struct {
	PlanID     string
	PlanDigest string
	Executable bool
	At         values.RecordedAt
	Digest     string
	Basis      LinkBasis
}

// Validate reports whether the link is complete.
func (r RepairLink) Validate() error {
	if r.PlanID == "" || r.PlanDigest == "" || r.Digest == "" {
		return fmt.Errorf("%w: repair %q/%q/%q", ErrRecordInvalid, r.PlanID, r.PlanDigest, r.Digest)
	}
	if r.At.Canonical() == nil {
		return fmt.Errorf("%w: repair %s has no recorded time", ErrRecordInvalid, r.PlanID)
	}
	return r.Basis.Validate()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r RepairLink) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.RepairLink", intelSchemaVer).
		String("plan_id", r.PlanID).
		String("plan_digest", r.PlanDigest).
		Bool("executable", r.Executable).
		Value("at", r.At).
		String("digest", r.Digest).
		String("basis", r.Basis.String()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ProjectionLink is one derived projection the transaction fed.
type ProjectionLink struct {
	Name     string
	Version  string
	Revision values.RevisionToken
	Digest   string
}

// Validate reports whether the link is complete.
func (p ProjectionLink) Validate() error {
	if p.Name == "" || p.Version == "" || p.Digest == "" {
		return fmt.Errorf("%w: projection %q/%q/%q", ErrRecordInvalid, p.Name, p.Version, p.Digest)
	}
	if !p.Revision.IsSpecified() {
		return fmt.Errorf("%w: projection %s has no revision", ErrRecordInvalid, p.Name)
	}
	return nil
}

// EvidenceLink is one immutable evidence artifact reference.
type EvidenceLink struct {
	Ref  string
	Kind string
}

// Validate reports whether the link is complete.
func (e EvidenceLink) Validate() error {
	if e.Ref == "" || e.Kind == "" {
		return fmt.Errorf("%w: evidence %q/%q", ErrRecordInvalid, e.Ref, e.Kind)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (e EvidenceLink) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.EvidenceLink", intelSchemaVer).
		String("ref", e.Ref).
		String("kind", e.Kind).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Gap is one part of the record that could not be established, with the reason
// token. Gaps are reported rather than smoothed over: an explanation with a
// silent hole in it is worse than one that says where the hole is.
type Gap struct {
	Section Section
	Reason  string
}

// Canonical returns the canonical byte encoding.
func (g Gap) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.Gap", intelSchemaVer).
		String("section", g.Section.String()).
		String("reason", g.Reason).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// TransactionRecord is everything the history port could say about one
// transaction at one knowledge cut-off.
//
// Exists is separate from the contents because "no such transaction" and "a
// transaction you may not see" must be indistinguishable to an unauthorized
// caller, and the explanation is the only place that decides which one to say.
type TransactionRecord struct {
	Transaction values.EntityRef
	Exists      bool

	Request         Request
	Controls        []evidence.ControlVersion
	Transitions     []Transition
	WriteSet        []WriteSetEntry
	Events          []LedgerEvent
	Effects         []EffectRecord
	Observations    []ObservationLink
	Reconciliations []ReconciliationLink
	Repairs         []RepairLink
	Projections     []ProjectionLink
	EvidenceRefs    []EvidenceLink

	// LedgerHead is the authoritative stream position at read time. A
	// projection behind it is stale, and the explanation says so rather than
	// letting the projection speak over the ledger.
	LedgerHead values.RevisionToken
	// Watermark is the read position the whole record was taken at.
	Watermark values.RevisionToken
	// Gaps are the sections the port could not fully establish.
	Gaps []Gap
}

// Validate reports whether the record is internally consistent.
func (r TransactionRecord) Validate() error {
	if err := r.Transaction.Validate(); err != nil {
		return fmt.Errorf("%w: transaction ref: %w", ErrRecordInvalid, err)
	}
	if !r.Exists {
		return nil
	}
	if !r.Watermark.IsSpecified() {
		return fmt.Errorf("%w: record has no read watermark", ErrRecordInvalid)
	}
	// The REQUEST section is optional in a narrowed projection: a caller that
	// asked only for the chronology gets a record with no request in it, which
	// is a projection rather than an incomplete record. A request that is
	// present, however, must be complete.
	if r.Request.IntentType != "" {
		if err := r.Request.Validate(); err != nil {
			return err
		}
	}
	for _, t := range r.Transitions {
		if err := t.Validate(); err != nil {
			return err
		}
	}
	for _, w := range r.WriteSet {
		if err := w.Validate(); err != nil {
			return err
		}
	}
	for _, e := range r.Events {
		if err := e.Validate(); err != nil {
			return err
		}
	}
	for _, e := range r.Effects {
		if err := e.Validate(); err != nil {
			return err
		}
	}
	for _, o := range r.Observations {
		if err := o.Validate(); err != nil {
			return err
		}
	}
	for _, rc := range r.Reconciliations {
		if err := rc.Validate(); err != nil {
			return err
		}
	}
	for _, rp := range r.Repairs {
		if err := rp.Validate(); err != nil {
			return err
		}
	}
	for _, p := range r.Projections {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	for _, e := range r.EvidenceRefs {
		if err := e.Validate(); err != nil {
			return err
		}
	}
	for _, c := range r.Controls {
		if c.Name == "" || c.Version == "" {
			return fmt.Errorf("%w: control %q@%q", ErrRecordInvalid, c.Name, c.Version)
		}
	}
	for _, g := range r.Gaps {
		if err := g.Section.Validate(); err != nil {
			return err
		}
		if g.Reason == "" {
			return fmt.Errorf("%w: gap in %s carries no reason", ErrRecordInvalid, g.Section)
		}
	}
	return nil
}

// TransactionQuery is what the history port is asked for.
type TransactionQuery struct {
	Tenant      values.TenantId
	Transaction values.EntityRef
	// Sections is the exact projection to read. The port must not widen it.
	Sections []Section
	// KnownAt is the knowledge cut-off the record is read at.
	KnownAt values.KnownAt
}

// Validate reports whether the query is well formed.
func (q TransactionQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("intelligence: query tenant: %w", err)
	}
	if err := q.Transaction.Validate(); err != nil {
		return fmt.Errorf("intelligence: query transaction: %w", err)
	}
	if q.Transaction.Tenant != q.Tenant {
		return fmt.Errorf("intelligence: transaction %s is outside tenant %s", q.Transaction, q.Tenant)
	}
	if q.Transaction.Kind != KindTransaction {
		return fmt.Errorf("intelligence: subject kind is %q, want %q", q.Transaction.Kind, KindTransaction)
	}
	if q.KnownAt.Canonical() == nil {
		return fmt.Errorf("intelligence: query known-at is unset")
	}
	if len(q.Sections) == 0 {
		return fmt.Errorf("%w: query requests no sections", ErrRequestInvalid)
	}
	seen := make(map[Section]struct{}, len(q.Sections))
	for _, s := range q.Sections {
		if err := s.Validate(); err != nil {
			return err
		}
		if _, dup := seen[s]; dup {
			return fmt.Errorf("%w: section %s requested twice", ErrRequestInvalid, s)
		}
		seen[s] = struct{}{}
	}
	return nil
}

// TransactionHistory is the read port the intent kernel wires to the real
// ledger, intent and provenance stores.
//
// There is deliberately no variant that accepts a record from the caller. An
// explanation whose evidence the caller supplied certifies nothing, and this
// intent exists precisely to be citable.
type TransactionHistory interface {
	// TransactionRecordAt returns the recorded evidence for the requested
	// sections at the query's knowledge cut-off. A transaction that does not
	// exist is a TransactionRecord with Exists false, because non-existence is
	// an answer, not a fault.
	TransactionRecordAt(ctx context.Context, q TransactionQuery) (TransactionRecord, error)
}
