package intelligence

import (
	"context"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Intent identity for INTEL-001.
const (
	// ExplainTransactionIntentType is the catalog identifier this function
	// implements.
	ExplainTransactionIntentType = "hcmnext.intelligence.explain_transaction"
	// ExplainTransactionIntentVersion is the contract version.
	ExplainTransactionIntentVersion = "v1"
	// ExplainRulePackVersion versions the assembly, ordering and redaction
	// rules below. A stored explanation must remain interpretable by the rules
	// that produced it.
	ExplainRulePackVersion = "intelligence.explain.rules/1.0.0"
)

// EvidencePrecedence is the fixed rule this package applies when a derived
// projection and the ledger disagree. It is a constant rather than a policy
// input because a configurable answer to "which one is true" is a
// configuration option for being wrong.
const EvidencePrecedence = "LEDGER_OVER_PROJECTION"

// MaxNarrativeLines bounds the narrative so an explanation cannot become an
// unbounded export channel.
const MaxNarrativeLines = 64

// Effect is one authorization ruling.
type Effect uint8

// Effects.
const (
	// EffectUnspecified is the zero value and is never legal in a decision.
	EffectUnspecified Effect = iota
	// EffectAllow permits disclosing the section or field.
	EffectAllow
	// EffectDeny withholds it. The section or field is still named in the
	// result so a refusal stays distinguishable from an absence.
	EffectDeny
)

var effectWire = map[Effect]string{
	EffectAllow: "ALLOW",
	EffectDeny:  "DENY",
}

// String returns the stable wire token, or "EFFECT_UNSPECIFIED".
func (e Effect) String() string {
	if s, ok := effectWire[e]; ok {
		return s
	}
	return "EFFECT_UNSPECIFIED"
}

// Valid reports whether e is a legal effect.
func (e Effect) Valid() bool { _, ok := effectWire[e]; return ok }

// Ruling is one decision plus the policy token explaining it.
type Ruling struct {
	Effect Effect
	Reason string
}

// Validate reports whether the ruling is usable.
func (r Ruling) Validate() error {
	if !r.Effect.Valid() {
		return fmt.Errorf("%w: effect is unspecified", ErrAuthorizationInvalid)
	}
	if r.Effect == EffectDeny && r.Reason == "" {
		return fmt.Errorf("%w: a denial must carry a reason token", ErrAuthorizationInvalid)
	}
	return nil
}

// AuthorizationDecision is an already-evaluated authorization result handed to
// this package as an input.
//
// TransactionDisclosable is separate from the section rulings because the
// existence of a transaction is itself information. A caller who may not know
// that a promotion was ever requested must not be able to infer it from a
// pattern of section denials.
type AuthorizationDecision struct {
	PolicyVersion string
	Purpose       string
	// TransactionDisclosable reports whether the caller may learn that this
	// transaction exists at all.
	TransactionDisclosable bool
	// DenialReason is the policy token for a non-disclosable transaction.
	DenialReason string
	// Sections is the per-section ruling. Every requested section must appear.
	Sections map[Section]Ruling
	// Fields is the per-field ruling for the redactable field tokens. A field
	// the decision is silent about is treated as allowed only when its section
	// is allowed; the explanation never invents a denial it was not told
	// about, and never invents an allowance either.
	Fields map[string]Ruling
}

// Validate reports whether the decision is well formed on its own terms.
func (d AuthorizationDecision) Validate() error {
	if d.PolicyVersion == "" {
		return fmt.Errorf("%w: policy version is required", ErrAuthorizationInvalid)
	}
	if d.Purpose == "" {
		return fmt.Errorf("%w: purpose of use is required", ErrAuthorizationInvalid)
	}
	if !d.TransactionDisclosable && d.DenialReason == "" {
		return fmt.Errorf("%w: a non-disclosable transaction must carry a reason token",
			ErrAuthorizationInvalid)
	}
	for section, ruling := range d.Sections {
		if err := section.Validate(); err != nil {
			return err
		}
		if err := ruling.Validate(); err != nil {
			return fmt.Errorf("%w (section %s)", err, section)
		}
	}
	for field, ruling := range d.Fields {
		if _, ok := redactableFields[field]; !ok {
			return fmt.Errorf("%w: %q is not a redactable field", ErrAuthorizationInvalid, field)
		}
		if err := ruling.Validate(); err != nil {
			return fmt.Errorf("%w (field %s)", err, field)
		}
	}
	return nil
}

// AllowsSection reports whether the decision permits a section.
func (d AuthorizationDecision) AllowsSection(s Section) bool {
	r, ok := d.Sections[s]
	return ok && r.Effect == EffectAllow
}

// AllowsField reports whether the decision permits a redactable field.
func (d AuthorizationDecision) AllowsField(field string) bool {
	r, ok := d.Fields[field]
	if !ok {
		return true
	}
	return r.Effect == EffectAllow
}

// fieldReason returns the denial token for a field.
func (d AuthorizationDecision) fieldReason(field string) string {
	if r, ok := d.Fields[field]; ok {
		return r.Reason
	}
	return ""
}

// SectionRuling returns the ruling for a section.
func (d AuthorizationDecision) SectionRuling(s Section) (Ruling, bool) {
	r, ok := d.Sections[s]
	return r, ok
}

// Covers reports whether the decision rules on every requested section.
func (d AuthorizationDecision) Covers(sections []Section) error {
	for _, s := range sections {
		if _, ok := d.Sections[s]; !ok {
			return fmt.Errorf("%w: no ruling for %s", ErrAuthorizationIncomplete, s)
		}
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (d AuthorizationDecision) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	sections := make([]Section, 0, len(d.Sections))
	for s := range d.Sections {
		sections = append(sections, s)
	}
	sort.Slice(sections, func(i, j int) bool { return sections[i] < sections[j] })
	fields := make([]string, 0, len(d.Fields))
	for f := range d.Fields {
		fields = append(fields, f)
	}
	sort.Strings(fields)

	w := canonicalbytes.New("hcmnext.domains.intelligence.AuthorizationDecision", intelSchemaVer).
		String("policy_version", d.PolicyVersion).
		String("purpose", d.Purpose).
		Bool("transaction_disclosable", d.TransactionDisclosable).
		String("denial_reason", d.DenialReason).
		Count("sections", len(sections))
	for _, s := range sections {
		r := d.Sections[s]
		w.String("section", s.String()).
			String("section.effect", r.Effect.String()).
			String("section.reason", r.Reason)
	}
	w.Count("fields", len(fields))
	for _, f := range fields {
		r := d.Fields[f]
		w.String("field", f).
			String("field.effect", r.Effect.String()).
			String("field.reason", r.Reason)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Access is the per-section disclosure outcome.
type Access uint8

// Access states.
const (
	// AccessUnspecified is the zero value and is never a legal result.
	AccessUnspecified Access = iota
	// AccessAuthorized means the section is disclosed.
	AccessAuthorized
	// AccessDenied means the section was requested and withheld.
	AccessDenied
)

var accessWire = map[Access]string{
	AccessAuthorized: "AUTHORIZED",
	AccessDenied:     "DENIED",
}

// String returns the stable wire token, or "ACCESS_UNSPECIFIED".
func (a Access) String() string {
	if s, ok := accessWire[a]; ok {
		return s
	}
	return "ACCESS_UNSPECIFIED"
}

// Disclosure is the whole-transaction disclosure outcome.
type Disclosure uint8

// Disclosure states.
const (
	// DisclosureUnspecified is the zero value and is never a legal result.
	DisclosureUnspecified Disclosure = iota
	// DisclosureFull means every requested section and field was authorized.
	DisclosureFull
	// DisclosurePartial means at least one was denied.
	DisclosurePartial
	// DisclosureWithheld means the caller may not learn anything, including
	// whether the transaction exists.
	DisclosureWithheld
)

var disclosureWire = map[Disclosure]string{
	DisclosureFull:     "FULL",
	DisclosurePartial:  "PARTIAL",
	DisclosureWithheld: "WITHHELD",
}

// String returns the stable wire token, or "DISCLOSURE_UNSPECIFIED".
func (d Disclosure) String() string {
	if s, ok := disclosureWire[d]; ok {
		return s
	}
	return "DISCLOSURE_UNSPECIFIED"
}

// Presence is what the record says about the transaction's existence.
type Presence uint8

// Presence states.
const (
	// PresenceUnspecified is what a withheld explanation carries.
	PresenceUnspecified Presence = iota
	// PresencePresent means the transaction exists.
	PresencePresent
	// PresenceAbsent means it does not.
	PresenceAbsent
)

var presenceWire = map[Presence]string{
	PresencePresent: "PRESENT",
	PresenceAbsent:  "ABSENT",
}

// String returns the stable wire token, or "PRESENCE_UNSPECIFIED".
func (p Presence) String() string {
	if s, ok := presenceWire[p]; ok {
		return s
	}
	return "PRESENCE_UNSPECIFIED"
}

// SectionDisclosure is one section's outcome.
type SectionDisclosure struct {
	Section Section
	Access  Access
	// DenialReason is the policy token for a denied section.
	DenialReason string
	// Entries is how many elements the section disclosed. It is zero for a
	// denied section and for an authorized-but-empty one; Access tells the two
	// apart.
	Entries int
}

// Canonical returns the canonical byte encoding.
func (s SectionDisclosure) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.SectionDisclosure", intelSchemaVer).
		String("section", s.Section.String()).
		String("access", s.Access.String()).
		String("denial_reason", s.DenialReason).
		Int("entries", int64(s.Entries)).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ExplainedRequest is the REQUEST section as disclosed, with its redactable
// fields resolved to presences.
type ExplainedRequest struct {
	IntentType    string
	IntentVersion string
	Family        string
	Mode          string
	// Purpose, Requester and Subjects are presences because each is
	// independently redactable.
	Purpose   values.Presence[string]
	Requester values.Presence[string]
	Subjects  values.Presence[string]
	// SubjectRefs carries the actual references when they are disclosed.
	SubjectRefs    []values.EntityRef
	RequestedAt    values.RecordedAt
	RequestDigest  string
	ProposalDigest string
	Epistemic      Epistemic
}

// absentRequest is the REQUEST section of an explanation that did not
// disclose one. Its presences are explicitly ABSENT rather than zero values,
// so that a withheld or undisclosed request still has one canonical encoding.
func absentRequest() ExplainedRequest {
	return ExplainedRequest{
		Purpose:   values.Absent[string](),
		Requester: values.Absent[string](),
		Subjects:  values.Absent[string](),
		Epistemic: EpistemicAbsent,
	}
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (r ExplainedRequest) Canonical() []byte {
	purpose, err := values.MarshalPresence(r.Purpose, values.StringCodec{})
	if err != nil {
		return nil
	}
	requester, err := values.MarshalPresence(r.Requester, values.StringCodec{})
	if err != nil {
		return nil
	}
	subjects, err := values.MarshalPresence(r.Subjects, values.StringCodec{})
	if err != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.intelligence.ExplainedRequest", intelSchemaVer).
		String("intent_type", r.IntentType).
		String("intent_version", r.IntentVersion).
		String("family", r.Family).
		String("mode", r.Mode).
		Field("purpose", purpose).
		Field("requester", requester).
		Field("subjects", subjects).
		Count("subject_refs", len(r.SubjectRefs))
	for _, s := range r.SubjectRefs {
		w.Value("subject_ref", s)
	}
	w.Bool("requested_at?", r.RequestedAt.Canonical() != nil)
	if r.RequestedAt.Canonical() != nil {
		w.Value("requested_at", r.RequestedAt)
	}
	raw, err := w.
		String("request_digest", r.RequestDigest).
		String("proposal_digest", r.ProposalDigest).
		String("epistemic", r.Epistemic.String()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ExplainedProjection is one projection with its freshness resolved against
// the ledger head.
type ExplainedProjection struct {
	ProjectionLink
	// Stale reports whether this projection is behind the ledger head.
	Stale bool
	// Comparable reports whether the two revisions were on the same stream. A
	// projection on a different stream cannot be ordered against the head, and
	// the explanation says so rather than assuming it is current.
	Comparable bool
	Epistemic  Epistemic
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p ExplainedProjection) Canonical() []byte {
	if p.ProjectionLink.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.ExplainedProjection", intelSchemaVer).
		String("name", p.Name).
		String("version", p.Version).
		Value("revision", p.Revision).
		String("digest", p.Digest).
		Bool("stale", p.Stale).
		Bool("comparable", p.Comparable).
		String("epistemic", p.Epistemic.String()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ChronologyEntry is one element on the merged timeline.
//
// Every entry names the section it came from, the recorded time it happened
// at, the digest that pins it, its epistemic label and the basis of its link
// to this transaction. Adjacency on this timeline means "recorded in this
// order", never "caused by".
type ChronologyEntry struct {
	At      values.RecordedAt
	Section Section
	// Kind is the element's own type token within its section.
	Kind string
	// Ref is the element's identifier within its section.
	Ref       string
	Digest    string
	Epistemic Epistemic
	Basis     LinkBasis
}

// Canonical returns the canonical byte encoding.
func (c ChronologyEntry) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.ChronologyEntry", intelSchemaVer).
		Value("at", c.At).
		String("section", c.Section.String()).
		String("kind", c.Kind).
		String("ref", c.Ref).
		String("digest", c.Digest).
		String("epistemic", c.Epistemic.String()).
		String("basis", c.Basis.String()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Completeness says whether the explanation is whole, and where it is not.
type Completeness struct {
	Complete bool
	// Gaps are the sections the record could not fully establish, and
	// Redactions the sections and fields withheld from this caller. They are
	// separate lists because "we do not have it" and "you may not see it" are
	// different answers.
	Gaps       []Gap
	Redactions []string
}

// Canonical returns the canonical byte encoding.
func (c Completeness) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.intelligence.Completeness", intelSchemaVer).
		Bool("complete", c.Complete).
		Count("gaps", len(c.Gaps))
	for _, g := range c.Gaps {
		w.Value("gap", g)
	}
	raw, err := w.SortedStrings("redactions", c.Redactions).Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Explanation is the INTEL-001 result.
type Explanation struct {
	IntentType    string
	IntentVersion string

	Transaction values.EntityRef
	Disclosure  Disclosure
	Presence    Presence
	// WithheldReason is the policy token when Disclosure is WITHHELD.
	WithheldReason string
	AsKnownAt      values.KnownAt

	// Sections is one entry per requested section, in sorted section order.
	Sections []SectionDisclosure

	Request         ExplainedRequest
	Controls        []evidence.ControlVersion
	Transitions     []Transition
	WriteSet        []WriteSetEntry
	Events          []LedgerEvent
	Effects         []EffectRecord
	Observations    []ObservationLink
	Reconciliations []ReconciliationLink
	Repairs         []RepairLink
	Projections     []ExplainedProjection
	EvidenceRefs    []EvidenceLink

	// Chronology is the merged, deterministically ordered timeline over every
	// disclosed section.
	Chronology []ChronologyEntry
	LedgerHead values.RevisionToken
	// EvidencePrecedence names the fixed rule applied when a projection and
	// the ledger disagree.
	EvidencePrecedence string
	Completeness       Completeness
	Watermark          values.RevisionToken
	Narrative          []string

	PolicyVersion   string
	RulePackVersion string
	InputsDigest    string
	ResultDigest    string
	// Effects counted by this intent. Always zero.
	EffectCounters evidence.EffectCounters
	Receipt        evidence.ZeroEffectReceipt
}

// SectionAccess returns the disclosure outcome for a section.
func (e Explanation) SectionAccess(s Section) (SectionDisclosure, bool) {
	for _, d := range e.Sections {
		if d.Section == s {
			return d, true
		}
	}
	return SectionDisclosure{}, false
}

// DeniedSections returns the sections that were requested and refused.
func (e Explanation) DeniedSections() []Section {
	out := make([]Section, 0, len(e.Sections))
	for _, d := range e.Sections {
		if d.Access == AccessDenied {
			out = append(out, d.Section)
		}
	}
	return out
}

// canonicalBody encodes everything except the receipt.
func (e Explanation) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.intelligence.Explanation", intelSchemaVer).
		String("intent_type", e.IntentType).
		String("intent_version", e.IntentVersion).
		Value("transaction", e.Transaction).
		String("disclosure", e.Disclosure.String()).
		String("presence", e.Presence.String()).
		String("withheld_reason", e.WithheldReason)
	if e.Disclosure != DisclosureWithheld {
		w.Value("as_known_at", e.AsKnownAt)
	}
	w.Count("sections", len(e.Sections))
	for _, s := range e.Sections {
		w.Value("section", s)
	}
	w.Value("request", e.Request).
		Count("controls", len(e.Controls))
	for _, c := range e.Controls {
		w.String("control.name", c.Name).String("control.version", c.Version)
	}
	w.Count("transitions", len(e.Transitions))
	for _, t := range e.Transitions {
		w.Value("transition", t)
	}
	w.Count("write_set", len(e.WriteSet))
	for _, ws := range e.WriteSet {
		w.Value("write_set", ws)
	}
	w.Count("events", len(e.Events))
	for _, ev := range e.Events {
		w.Value("event", ev)
	}
	w.Count("effects", len(e.Effects))
	for _, ef := range e.Effects {
		w.Value("effect", ef)
	}
	w.Count("observations", len(e.Observations))
	for _, o := range e.Observations {
		w.Value("observation", o)
	}
	w.Count("reconciliations", len(e.Reconciliations))
	for _, rc := range e.Reconciliations {
		w.Value("reconciliation", rc)
	}
	w.Count("repairs", len(e.Repairs))
	for _, rp := range e.Repairs {
		w.Value("repair", rp)
	}
	w.Count("projections", len(e.Projections))
	for _, p := range e.Projections {
		w.Value("projection", p)
	}
	w.Count("evidence_refs", len(e.EvidenceRefs))
	for _, ev := range e.EvidenceRefs {
		w.Value("evidence_ref", ev)
	}
	w.Count("chronology", len(e.Chronology))
	for _, c := range e.Chronology {
		w.Value("chronology", c)
	}
	w.Bool("ledger_head?", e.LedgerHead.IsSpecified())
	if e.LedgerHead.IsSpecified() {
		w.Value("ledger_head", e.LedgerHead)
	}
	w.String("evidence_precedence", e.EvidencePrecedence).
		Value("completeness", e.Completeness).
		Bool("watermark?", e.Watermark.IsSpecified())
	if e.Watermark.IsSpecified() {
		w.Value("watermark", e.Watermark)
	}
	w.Count("narrative", len(e.Narrative))
	for _, line := range e.Narrative {
		w.String("narrative", line)
	}
	return w.
		String("policy_version", e.PolicyVersion).
		String("rule_pack_version", e.RulePackVersion).
		Value("effects", e.EffectCounters).
		Bytes()
}

// Canonical returns the canonical byte encoding including the receipt, or nil
// when the explanation is incoherent.
func (e Explanation) Canonical() []byte {
	body, err := e.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.intelligence.ExplanationEnvelope", intelSchemaVer).
		Field("body", body).
		String("inputs_digest", e.InputsDigest).
		Value("receipt", e.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ExplainTransactionRequest is the governed read request.
type ExplainTransactionRequest struct {
	Tenant      values.TenantId
	Transaction values.EntityRef
	// Sections is the requested projection. Empty means AllSections.
	Sections []Section
	// AsKnownAt is the knowledge cut-off the record is reconstructed at.
	AsKnownAt values.KnownAt
	// Authorization is the already-evaluated decision for this caller,
	// purpose and transaction.
	Authorization AuthorizationDecision
}

// projection returns the sorted, deduplicated section list to read.
func (r ExplainTransactionRequest) projection() []Section {
	sections := r.Sections
	if len(sections) == 0 {
		sections = AllSections()
	}
	out := append([]Section(nil), sections...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Validate reports whether the request is well formed.
func (r ExplainTransactionRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrRequestInvalid, err)
	}
	if err := r.Transaction.Validate(); err != nil {
		return fmt.Errorf("%w: transaction: %w", ErrRequestInvalid, err)
	}
	if r.Transaction.Tenant != r.Tenant {
		return fmt.Errorf("%w: transaction %s is outside tenant %s",
			ErrRequestInvalid, r.Transaction, r.Tenant)
	}
	if r.Transaction.Kind != KindTransaction {
		return fmt.Errorf("%w: subject kind is %q, want %q",
			ErrRequestInvalid, r.Transaction.Kind, KindTransaction)
	}
	if r.AsKnownAt.Canonical() == nil {
		return fmt.Errorf("%w: as-known-at is unset", ErrRequestInvalid)
	}
	seen := make(map[Section]struct{}, len(r.Sections))
	for _, s := range r.Sections {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrRequestInvalid, err)
		}
		if _, dup := seen[s]; dup {
			return fmt.Errorf("%w: section %s requested twice", ErrRequestInvalid, s)
		}
		seen[s] = struct{}{}
	}
	return r.Authorization.Validate()
}

// inputsDigest digests exactly what the answer depends on.
func (r ExplainTransactionRequest) inputsDigest() (string, error) {
	sections := r.projection()
	w := canonicalbytes.New("hcmnext.domains.intelligence.ExplainTransactionRequest", intelSchemaVer).
		String("intent_type", ExplainTransactionIntentType).
		String("intent_version", ExplainTransactionIntentVersion).
		String("tenant", string(r.Tenant)).
		Value("transaction", r.Transaction).
		Value("as_known_at", r.AsKnownAt).
		Count("sections", len(sections))
	for _, s := range sections {
		w.String("section", s.String())
	}
	return w.Value("authorization", r.Authorization).Digest()
}

// ExplainTransaction is the INTEL-001 entry point: one governed read that
// reconstructs the authorized chronology of a business transaction from
// recorded evidence, and writes nothing.
//
// Five refusals are load-bearing:
//
//   - A transaction the caller may not know about returns a WITHHELD
//     explanation with no presence and no sections. "Not found" and "not
//     yours" must be indistinguishable, or existence leaks.
//   - A section the decision does not rule on is an error, not a default.
//   - A denied section is named and marked DENIED with no contents at all.
//   - An external observation that claims local authority is rejected outright
//     rather than published as a domain fact.
//   - A link with no recorded basis is rejected. This package cannot express a
//     causal claim, so it cannot accidentally publish one.
func ExplainTransaction(
	ctx context.Context,
	reader TransactionHistory,
	req ExplainTransactionRequest,
) (Explanation, error) {
	if reader == nil {
		return Explanation{}, fmt.Errorf("%w: no transaction history reader", ErrRequestInvalid)
	}
	if err := req.Validate(); err != nil {
		return Explanation{}, err
	}
	sections := req.projection()
	if err := req.Authorization.Covers(sections); err != nil {
		return Explanation{}, err
	}
	inputsDigest, err := req.inputsDigest()
	if err != nil {
		return Explanation{}, err
	}

	if !req.Authorization.TransactionDisclosable {
		return finishExplanation(Explanation{
			IntentType:         ExplainTransactionIntentType,
			IntentVersion:      ExplainTransactionIntentVersion,
			Transaction:        req.Transaction,
			Disclosure:         DisclosureWithheld,
			Presence:           PresenceUnspecified,
			WithheldReason:     req.Authorization.DenialReason,
			AsKnownAt:          req.AsKnownAt,
			Request:            absentRequest(),
			EvidencePrecedence: EvidencePrecedence,
			Completeness:       Completeness{Complete: false},
			Narrative: []string{
				"transaction is not disclosable to this caller under the evaluated policy",
			},
			PolicyVersion:   req.Authorization.PolicyVersion,
			RulePackVersion: ExplainRulePackVersion,
			InputsDigest:    inputsDigest,
			EffectCounters:  evidence.ZeroEffects(),
		})
	}

	// Only sections the caller may see are ever read. An authorized-read port
	// handed the full projection would have to be trusted to filter, and the
	// denied contents would already be in this process.
	allowed := make([]Section, 0, len(sections))
	for _, s := range sections {
		if req.Authorization.AllowsSection(s) {
			allowed = append(allowed, s)
		}
	}

	record := TransactionRecord{Transaction: req.Transaction, Exists: true, Watermark: values.UnspecifiedRevision()}
	if len(allowed) > 0 {
		record, err = reader.TransactionRecordAt(ctx, TransactionQuery{
			Tenant:      req.Tenant,
			Transaction: req.Transaction,
			Sections:    allowed,
			KnownAt:     req.AsKnownAt,
		})
		if err != nil {
			return Explanation{}, fmt.Errorf("%w: %w", ErrReaderFailed, err)
		}
		if err := record.Validate(); err != nil {
			return Explanation{}, err
		}
		if record.Transaction != req.Transaction {
			return Explanation{}, fmt.Errorf("%w: asked %s, answered %s",
				ErrPortWidenedProjection, req.Transaction, record.Transaction)
		}
		if err := checkProjection(record, allowed); err != nil {
			return Explanation{}, err
		}
	}

	presence := PresencePresent
	if !record.Exists {
		presence = PresenceAbsent
	}

	e := Explanation{
		IntentType:         ExplainTransactionIntentType,
		IntentVersion:      ExplainTransactionIntentVersion,
		Transaction:        req.Transaction,
		Presence:           presence,
		AsKnownAt:          req.AsKnownAt,
		Request:            absentRequest(),
		LedgerHead:         record.LedgerHead,
		EvidencePrecedence: EvidencePrecedence,
		Watermark:          record.Watermark,
		PolicyVersion:      req.Authorization.PolicyVersion,
		RulePackVersion:    ExplainRulePackVersion,
		InputsDigest:       inputsDigest,
		EffectCounters:     evidence.ZeroEffects(),
	}

	denied := 0
	redactions := make([]string, 0, len(sections))
	for _, section := range sections {
		ruling, _ := req.Authorization.SectionRuling(section)
		if ruling.Effect == EffectDeny {
			denied++
			redactions = append(redactions, section.String())
			e.Sections = append(e.Sections, SectionDisclosure{
				Section:      section,
				Access:       AccessDenied,
				DenialReason: ruling.Reason,
			})
			continue
		}
		entries := fillSection(&e, section, record, req.Authorization, &redactions)
		e.Sections = append(e.Sections, SectionDisclosure{
			Section: section,
			Access:  AccessAuthorized,
			Entries: entries,
		})
	}

	e.Chronology = buildChronology(e)
	sort.Strings(redactions)
	e.Completeness = Completeness{
		Complete:   denied == 0 && len(record.Gaps) == 0 && len(redactions) == 0,
		Gaps:       append([]Gap(nil), record.Gaps...),
		Redactions: redactions,
	}
	sort.Slice(e.Completeness.Gaps, func(i, j int) bool {
		if e.Completeness.Gaps[i].Section != e.Completeness.Gaps[j].Section {
			return e.Completeness.Gaps[i].Section < e.Completeness.Gaps[j].Section
		}
		return e.Completeness.Gaps[i].Reason < e.Completeness.Gaps[j].Reason
	})

	e.Disclosure = DisclosureFull
	if denied > 0 || len(redactions) > 0 {
		e.Disclosure = DisclosurePartial
	}
	e.Narrative = narrate(e, denied, len(record.Gaps))
	return finishExplanation(e)
}

// checkProjection refuses a record that carries a section nobody asked for.
func checkProjection(record TransactionRecord, allowed []Section) error {
	granted := make(map[Section]struct{}, len(allowed))
	for _, s := range allowed {
		granted[s] = struct{}{}
	}
	carried := map[Section]bool{
		SectionRequest:        record.Request.IntentType != "",
		SectionGovernance:     len(record.Controls) > 0,
		SectionLifecycle:      len(record.Transitions) > 0,
		SectionWriteSet:       len(record.WriteSet) > 0,
		SectionEvents:         len(record.Events) > 0,
		SectionEffects:        len(record.Effects) > 0,
		SectionObservations:   len(record.Observations) > 0,
		SectionReconciliation: len(record.Reconciliations) > 0,
		SectionRepair:         len(record.Repairs) > 0,
		SectionProjections:    len(record.Projections) > 0,
		SectionEvidence:       len(record.EvidenceRefs) > 0,
	}
	for section, present := range carried {
		if !present {
			continue
		}
		if _, ok := granted[section]; !ok {
			return fmt.Errorf("%w: record carries the %s section", ErrPortWidenedProjection, section)
		}
	}
	return nil
}

// fillSection copies one authorized section into the explanation, applying
// field-level redaction, and returns how many elements it disclosed.
func fillSection(
	e *Explanation,
	section Section,
	record TransactionRecord,
	auth AuthorizationDecision,
	redactions *[]string,
) int {
	switch section {
	case SectionRequest:
		if record.Request.IntentType == "" {
			return 0
		}
		e.Request = explainRequest(record.Request, auth, redactions)
		return 1
	case SectionGovernance:
		e.Controls = append([]evidence.ControlVersion(nil), record.Controls...)
		sort.Slice(e.Controls, func(i, j int) bool {
			if e.Controls[i].Name != e.Controls[j].Name {
				return e.Controls[i].Name < e.Controls[j].Name
			}
			return e.Controls[i].Version < e.Controls[j].Version
		})
		return len(e.Controls)
	case SectionLifecycle:
		e.Transitions = append([]Transition(nil), record.Transitions...)
		sort.SliceStable(e.Transitions, func(i, j int) bool {
			a, b := e.Transitions[i], e.Transitions[j]
			if a.Sequence != b.Sequence {
				return a.Sequence < b.Sequence
			}
			if cmp := a.At.Instant().Compare(b.At.Instant()); cmp != 0 {
				return cmp < 0
			}
			return a.Digest < b.Digest
		})
		return len(e.Transitions)
	case SectionWriteSet:
		e.WriteSet = append([]WriteSetEntry(nil), record.WriteSet...)
		sort.SliceStable(e.WriteSet, func(i, j int) bool {
			if e.WriteSet[i].Resource != e.WriteSet[j].Resource {
				return e.WriteSet[i].Resource < e.WriteSet[j].Resource
			}
			return e.WriteSet[i].Digest < e.WriteSet[j].Digest
		})
		return len(e.WriteSet)
	case SectionEvents:
		e.Events = append([]LedgerEvent(nil), record.Events...)
		sort.SliceStable(e.Events, func(i, j int) bool {
			a, b := e.Events[i], e.Events[j]
			if cmp := a.At.Instant().Compare(b.At.Instant()); cmp != 0 {
				return cmp < 0
			}
			return a.ID < b.ID
		})
		return len(e.Events)
	case SectionEffects:
		e.Effects = append([]EffectRecord(nil), record.Effects...)
		sort.SliceStable(e.Effects, func(i, j int) bool {
			a, b := e.Effects[i], e.Effects[j]
			if cmp := a.At.Instant().Compare(b.At.Instant()); cmp != 0 {
				return cmp < 0
			}
			return a.ID < b.ID
		})
		return len(e.Effects)
	case SectionObservations:
		e.Observations = append([]ObservationLink(nil), record.Observations...)
		sort.SliceStable(e.Observations, func(i, j int) bool {
			a, b := e.Observations[i], e.Observations[j]
			if cmp := a.At.Instant().Compare(b.At.Instant()); cmp != 0 {
				return cmp < 0
			}
			return a.ID < b.ID
		})
		return len(e.Observations)
	case SectionReconciliation:
		e.Reconciliations = append([]ReconciliationLink(nil), record.Reconciliations...)
		sort.SliceStable(e.Reconciliations, func(i, j int) bool {
			a, b := e.Reconciliations[i], e.Reconciliations[j]
			if cmp := a.At.Instant().Compare(b.At.Instant()); cmp != 0 {
				return cmp < 0
			}
			return a.ID < b.ID
		})
		return len(e.Reconciliations)
	case SectionRepair:
		e.Repairs = append([]RepairLink(nil), record.Repairs...)
		sort.SliceStable(e.Repairs, func(i, j int) bool {
			a, b := e.Repairs[i], e.Repairs[j]
			if cmp := a.At.Instant().Compare(b.At.Instant()); cmp != 0 {
				return cmp < 0
			}
			return a.PlanID < b.PlanID
		})
		return len(e.Repairs)
	case SectionProjections:
		e.Projections = explainProjections(record)
		return len(e.Projections)
	case SectionEvidence:
		e.EvidenceRefs = append([]EvidenceLink(nil), record.EvidenceRefs...)
		sort.SliceStable(e.EvidenceRefs, func(i, j int) bool {
			if e.EvidenceRefs[i].Ref != e.EvidenceRefs[j].Ref {
				return e.EvidenceRefs[i].Ref < e.EvidenceRefs[j].Ref
			}
			return e.EvidenceRefs[i].Kind < e.EvidenceRefs[j].Kind
		})
		return len(e.EvidenceRefs)
	}
	return 0
}

// explainRequest applies field-level redaction to the REQUEST section.
func explainRequest(r Request, auth AuthorizationDecision, redactions *[]string) ExplainedRequest {
	out := ExplainedRequest{
		IntentType:     r.IntentType,
		IntentVersion:  r.IntentVersion,
		Family:         r.Family,
		Mode:           r.Mode,
		RequestedAt:    r.RequestedAt,
		RequestDigest:  r.RequestDigest,
		ProposalDigest: r.ProposalDigest,
		Epistemic:      EpistemicAsserted,
	}

	if auth.AllowsField(FieldRequestPurpose) {
		out.Purpose = values.Value(r.Purpose)
	} else {
		out.Purpose = values.Redacted[string](auth.fieldReason(FieldRequestPurpose))
		*redactions = append(*redactions, FieldRequestPurpose)
	}
	if auth.AllowsField(FieldRequestRequester) {
		out.Requester = values.Value(r.Requester.String())
	} else {
		out.Requester = values.Redacted[string](auth.fieldReason(FieldRequestRequester))
		*redactions = append(*redactions, FieldRequestRequester)
	}
	if auth.AllowsField(FieldRequestSubjects) {
		out.SubjectRefs = append([]values.EntityRef(nil), r.Subjects...)
		sort.Slice(out.SubjectRefs, func(i, j int) bool {
			return out.SubjectRefs[i].String() < out.SubjectRefs[j].String()
		})
		out.Subjects = values.Value(fmt.Sprintf("%d subject(s)", len(out.SubjectRefs)))
	} else {
		out.Subjects = values.Redacted[string](auth.fieldReason(FieldRequestSubjects))
		*redactions = append(*redactions, FieldRequestSubjects)
	}
	return out
}

// explainProjections resolves each projection's freshness against the ledger
// head. A projection that cannot be ordered against the head is reported as
// not comparable rather than assumed current: the ledger is the authority, and
// a projection that cannot prove it caught up has not proved it.
func explainProjections(record TransactionRecord) []ExplainedProjection {
	out := make([]ExplainedProjection, 0, len(record.Projections))
	for _, p := range record.Projections {
		explained := ExplainedProjection{ProjectionLink: p, Epistemic: EpistemicDerived}
		if record.LedgerHead.IsSpecified() {
			if cmp, err := p.Revision.CompareInStream(record.LedgerHead); err == nil {
				explained.Comparable = true
				explained.Stale = cmp < 0
			}
		}
		if !explained.Comparable {
			explained.Epistemic = EpistemicUnknown
		}
		out = append(out, explained)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// buildChronology merges every disclosed section into one ordered timeline.
//
// The order is recorded time, then section, then reference. It is total, so
// two runs over the same evidence produce the same timeline and therefore the
// same digest.
func buildChronology(e Explanation) []ChronologyEntry {
	var entries []ChronologyEntry
	add := func(at values.RecordedAt, section Section, kind, ref, digest string, ep Epistemic, basis LinkBasis) {
		entries = append(entries, ChronologyEntry{
			At: at, Section: section, Kind: kind, Ref: ref,
			Digest: digest, Epistemic: ep, Basis: basis,
		})
	}

	if e.Request.RequestDigest != "" {
		add(e.Request.RequestedAt, SectionRequest, "REQUESTED",
			e.Request.RequestDigest, e.Request.RequestDigest,
			EpistemicAsserted, BasisRecordedReference)
	}
	for _, t := range e.Transitions {
		add(t.At, SectionLifecycle, t.Dimension+":"+t.To, t.Digest, t.Digest,
			EpistemicAsserted, BasisRecordedReference)
	}
	for _, ev := range e.Events {
		add(ev.At, SectionEvents, ev.Type, ev.ID, ev.Digest,
			EpistemicAsserted, BasisRecordedReference)
	}
	for _, ef := range e.Effects {
		add(ef.At, SectionEffects, ef.Channel+":"+ef.State, ef.ID, ef.Digest,
			EpistemicAsserted, BasisRecordedReference)
	}
	for _, o := range e.Observations {
		// An observation is labelled OBSERVED on the timeline, never ASSERTED.
		// It sits beside domain facts; it does not become one.
		add(o.At, SectionObservations, o.Source, o.ID, o.Digest,
			EpistemicObserved, o.Basis)
	}
	for _, rc := range e.Reconciliations {
		add(rc.At, SectionReconciliation, rc.Outcome, rc.ID, rc.Digest,
			EpistemicDerived, rc.Basis)
	}
	for _, rp := range e.Repairs {
		add(rp.At, SectionRepair, "REPAIR_PLAN", rp.PlanID, rp.Digest,
			EpistemicDerived, rp.Basis)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if cmp := a.At.Instant().Compare(b.At.Instant()); cmp != 0 {
			return cmp < 0
		}
		if a.Section != b.Section {
			return a.Section < b.Section
		}
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		return a.Digest < b.Digest
	})
	return entries
}

// narrate builds the bounded, value-free narrative. It describes shape,
// coordinates and completeness only.
func narrate(e Explanation, denied, gaps int) []string {
	lines := []string{
		fmt.Sprintf("transaction presence as known at %s: %s", e.AsKnownAt, e.Presence),
		fmt.Sprintf("%d section(s) requested, %d authorized, %d denied",
			len(e.Sections), len(e.Sections)-denied, denied),
		fmt.Sprintf("%d chronology entr(ies) reconstructed from recorded references only",
			len(e.Chronology)),
		fmt.Sprintf("%d ledger event(s), %d effect record(s), %d external observation(s)",
			len(e.Events), len(e.Effects), len(e.Observations)),
		"external observations are labelled OBSERVED and are never reported as domain facts",
		"timeline adjacency records order, never causation",
		"evidence precedence: " + e.EvidencePrecedence,
	}
	if gaps > 0 {
		lines = append(lines, fmt.Sprintf("%d section gap(s) reported explicitly rather than omitted", gaps))
	}
	if denied > 0 || len(e.Completeness.Redactions) > 0 {
		lines = append(lines, "denied sections and fields are named with no contents disclosed")
	}
	stale := 0
	for _, p := range e.Projections {
		if p.Stale {
			stale++
		}
	}
	if stale > 0 {
		lines = append(lines, fmt.Sprintf(
			"%d projection(s) are behind the ledger head and do not override ledger evidence", stale))
	}
	return lines
}

// finishExplanation digests the body, mints the zero-effect receipt and
// returns the completed explanation.
func finishExplanation(e Explanation) (Explanation, error) {
	if len(e.Narrative) > MaxNarrativeLines {
		return Explanation{}, fmt.Errorf("intelligence: narrative exceeds its bound: %d lines",
			len(e.Narrative))
	}
	body, err := e.canonicalBody()
	if err != nil {
		return Explanation{}, err
	}
	e.ResultDigest = canonicalbytes.Digest(body)
	receipt, err := evidence.NewZeroEffectReceipt(
		e.IntentType, e.IntentVersion,
		evidence.ModeSimulate,
		evidence.RequestStateSimulated,
		[]evidence.ControlVersion{
			{Name: "authorization_policy", Version: e.PolicyVersion},
			{Name: "explain_rule_pack", Version: e.RulePackVersion},
		},
		e.InputsDigest, e.ResultDigest, e.EffectCounters,
	)
	if err != nil {
		return Explanation{}, err
	}
	e.Receipt = receipt
	return e, nil
}
