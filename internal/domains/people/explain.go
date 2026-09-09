package people

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Intent identity for PEOPLE-005.
const (
	// ExplainWorkerStateIntentType is the catalog identifier this function
	// implements.
	ExplainWorkerStateIntentType = "hcmnext.people.explain_worker_state"
	// ExplainWorkerStateIntentVersion is the contract version.
	ExplainWorkerStateIntentVersion = "v1"
	// ExplainRulePackVersion is the version of the disclosure rules below. It
	// changes whenever the shape or the redaction behaviour of an explanation
	// changes, because a stored explanation must be interpretable later.
	ExplainRulePackVersion = "people.explain.rules/1.0.0"
)

// Explanation errors. All are matchable with errors.Is.
var (
	// ErrExplainRequestInvalid is returned for a malformed request.
	ErrExplainRequestInvalid = errors.New("people: explain request is invalid")
	// ErrNarrativeOverflow is returned when the bounded explanation would
	// exceed MaxNarrativeLines. The bound exists so an explanation cannot
	// become an unbounded data-export channel.
	ErrNarrativeOverflow = errors.New("people: explanation narrative exceeds its bound")
)

// MaxNarrativeLines bounds the human-readable explanation.
const MaxNarrativeLines = 64

// Disclosure is what the caller is being told at the whole-subject level.
type Disclosure uint8

// Disclosure states.
const (
	// DisclosureUnspecified is the zero value and is never a legal result.
	DisclosureUnspecified Disclosure = iota
	// DisclosureFull means every requested field was authorized.
	DisclosureFull
	// DisclosurePartial means at least one requested field was denied. The
	// denied fields are named; their values are not.
	DisclosurePartial
	// DisclosureWithheld means the caller may not learn anything about this
	// subject, including whether it exists.
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

// Valid reports whether d is a legal disclosure state.
func (d Disclosure) Valid() bool { _, ok := disclosureWire[d]; return ok }

// SubjectPresence is what the record says about the subject's existence. It is
// only meaningful when the disclosure is not WITHHELD.
type SubjectPresence uint8

// Subject presence states.
const (
	// SubjectPresenceUnspecified is the value carried by a withheld explanation.
	SubjectPresenceUnspecified SubjectPresence = iota
	// SubjectPresent means the worker exists at the requested coordinate.
	SubjectPresent
	// SubjectAbsent means the worker does not exist at the requested coordinate.
	SubjectAbsent
)

var subjectPresenceWire = map[SubjectPresence]string{
	SubjectPresent: "PRESENT",
	SubjectAbsent:  "ABSENT",
}

// String returns the stable wire token, or "SUBJECT_PRESENCE_UNSPECIFIED".
func (p SubjectPresence) String() string {
	if s, ok := subjectPresenceWire[p]; ok {
		return s
	}
	return "SUBJECT_PRESENCE_UNSPECIFIED"
}

// Access is the per-field disclosure outcome.
type Access uint8

// Field access states.
const (
	// AccessUnspecified is the zero value and is never a legal result.
	AccessUnspecified Access = iota
	// AccessAuthorized means the field's value and provenance are disclosed.
	AccessAuthorized
	// AccessDenied means the field was requested, exists as a concept, and is
	// withheld. The field appears in the result so the caller knows it was
	// refused rather than missing.
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

// ExplainedFact is one field as disclosed to this caller.
//
// A denied field carries Access DENIED, a REDACTED presence and no provenance
// at all. Provenance names systems and evidence artifacts, which is itself
// information about the withheld value, so it travels with the value or not
// at all.
type ExplainedFact struct {
	Field  FieldID
	Access Access
	// DenialReason is the policy token for a denied field, empty otherwise.
	DenialReason string
	// Value is the disclosed presence. REDACTED for a denied field.
	Value values.Presence[string]

	// Effective, KnownAt, Revision, Authority and Provenance are populated only
	// for an authorized field.
	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Revision   values.RevisionToken
	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (f ExplainedFact) Canonical() []byte {
	encoded, err := values.MarshalPresence(f.Value, values.StringCodec{})
	if err != nil {
		return nil
	}
	// Evidence travels with a disclosed assertion. An authorized field that the
	// record does not assert has a presence but no evidence, and a denied field
	// has neither; both encode the absence explicitly rather than by omission.
	hasEvidence := f.Access == AccessAuthorized &&
		f.Effective.Validate() == nil &&
		f.KnownAt.Canonical() != nil &&
		f.Revision.IsSpecified() &&
		f.Authority.Validate() == nil &&
		f.Provenance.Validate() == nil
	w := canonicalbytes.New("hcmnext.domains.people.ExplainedFact", peopleSchemaVer).
		String("field", string(f.Field)).
		String("access", f.Access.String()).
		String("denial_reason", f.DenialReason).
		Field("value", encoded).
		Bool("evidence?", hasEvidence)
	if hasEvidence {
		w.Value("effective", f.Effective).
			Value("known_at", f.KnownAt).
			Value("revision", f.Revision).
			Value("authority", f.Authority).
			Value("provenance", f.Provenance)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Explanation is the result of ExplainWorkerState: typed authorized facts with
// their bitemporal coordinates, source authority and provenance, plus a
// bounded narrative and a zero-effect receipt.
type Explanation struct {
	IntentType    string
	IntentVersion string

	Worker     values.EntityRef
	Disclosure Disclosure
	Presence   SubjectPresence
	// WithheldReason is the policy token when Disclosure is WITHHELD.
	WithheldReason string
	AsOf           AsOf

	// Fields is one entry per requested field, in sorted field order. Denied
	// fields are present and marked, never dropped.
	Fields []ExplainedFact
	// Watermark is the read position every disclosed fact was taken at.
	Watermark values.RevisionToken
	// Narrative is a bounded, value-free description of what was answered.
	Narrative []string

	PolicyVersion   string
	RulePackVersion string
	InputsDigest    string
	ResultDigest    string
	Effects         evidence.EffectCounters
	Receipt         evidence.ZeroEffectReceipt
}

// AuthorizedFields returns the fields that were actually disclosed.
func (e Explanation) AuthorizedFields() []ExplainedFact {
	out := make([]ExplainedFact, 0, len(e.Fields))
	for _, f := range e.Fields {
		if f.Access == AccessAuthorized {
			out = append(out, f)
		}
	}
	return out
}

// DeniedFields returns the fields that were requested and refused.
func (e Explanation) DeniedFields() []ExplainedFact {
	out := make([]ExplainedFact, 0, len(e.Fields))
	for _, f := range e.Fields {
		if f.Access == AccessDenied {
			out = append(out, f)
		}
	}
	return out
}

// Value returns the disclosed string value of an authorized field.
func (e Explanation) Value(field FieldID) (string, bool) {
	for _, f := range e.Fields {
		if f.Field != field {
			continue
		}
		if f.Access != AccessAuthorized {
			return "", false
		}
		return f.Value.Get()
	}
	return "", false
}

// canonicalBody encodes everything except the receipt, which cites the digest
// of this body and therefore cannot be inside it.
func (e Explanation) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.people.Explanation", peopleSchemaVer).
		String("intent_type", e.IntentType).
		String("intent_version", e.IntentVersion).
		Value("worker", e.Worker).
		String("disclosure", e.Disclosure.String()).
		String("presence", e.Presence.String()).
		String("withheld_reason", e.WithheldReason)
	if e.Disclosure != DisclosureWithheld {
		w.Value("as_of", e.AsOf)
	}
	w.Count("fields", len(e.Fields))
	for _, f := range e.Fields {
		w.Value("field", f)
	}
	w.Bool("watermark?", e.Watermark.IsSpecified())
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
		Value("effects", e.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding of the whole explanation
// including its receipt, or nil when the explanation is incoherent.
func (e Explanation) Canonical() []byte {
	body, err := e.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.people.ExplanationEnvelope", peopleSchemaVer).
		Field("body", body).
		String("inputs_digest", e.InputsDigest).
		Value("receipt", e.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ExplainWorkerStateRequest is the governed read request.
//
// It carries references and an authorization decision, never facts. A caller
// cannot assert "this worker's grade is P4" and have the explanation agree.
type ExplainWorkerStateRequest struct {
	Tenant values.TenantId
	Worker values.EntityRef
	AsOf   AsOf
	// Fields is the requested projection. Empty means AllFields.
	Fields []FieldID
	// Authorization is the already-evaluated AuthZ result for this caller,
	// purpose and subject.
	Authorization AuthorizationDecision
}

// projection returns the sorted, deduplicated field list to read.
func (r ExplainWorkerStateRequest) projection() []FieldID {
	fields := r.Fields
	if len(fields) == 0 {
		fields = AllFields()
	}
	out := append([]FieldID(nil), fields...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Validate reports whether the request is well formed.
func (r ExplainWorkerStateRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrExplainRequestInvalid, err)
	}
	if err := r.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %w", ErrExplainRequestInvalid, err)
	}
	if r.Worker.Tenant != r.Tenant {
		return fmt.Errorf("%w: worker %s is outside tenant %s", ErrExplainRequestInvalid, r.Worker, r.Tenant)
	}
	if r.Worker.Kind != KindWorker {
		return fmt.Errorf("%w: subject kind is %q, want %q", ErrExplainRequestInvalid, r.Worker.Kind, KindWorker)
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrExplainRequestInvalid, err)
	}
	seen := make(map[FieldID]struct{}, len(r.Fields))
	for _, f := range r.Fields {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrExplainRequestInvalid, err)
		}
		if _, dup := seen[f]; dup {
			return fmt.Errorf("%w: %w: %s", ErrExplainRequestInvalid, ErrDuplicateField, f)
		}
		seen[f] = struct{}{}
	}
	return r.Authorization.Validate()
}

// inputsDigest digests exactly what the answer depends on: the subject, the
// bitemporal coordinate, the projection and the authorization decision.
func (r ExplainWorkerStateRequest) inputsDigest() (string, error) {
	fields := r.projection()
	w := canonicalbytes.New("hcmnext.domains.people.ExplainWorkerStateRequest", peopleSchemaVer).
		String("intent_type", ExplainWorkerStateIntentType).
		String("intent_version", ExplainWorkerStateIntentVersion).
		String("tenant", string(r.Tenant)).
		Value("worker", r.Worker).
		Value("as_of", r.AsOf).
		Count("fields", len(fields))
	for _, f := range fields {
		w.String("field", string(f))
	}
	return w.Value("authorization", r.Authorization).Digest()
}

// ExplainWorkerState is the PEOPLE-005 entry point: one governed read that
// returns typed authorized worker, employment and assignment facts with
// presence, revision, effective and known time, source authority, provenance
// and a bounded explanation - and writes nothing.
//
// Three refusals are load-bearing:
//
//   - A subject the caller may not know about returns a WITHHELD explanation
//     with no presence and no fields. It does not return "not found", because
//     "not found" and "not yours" would be distinguishable and existence would
//     leak.
//   - A field the decision does not rule on is an error, not a default. A read
//     model that guesses is a read model that will eventually guess ALLOW.
//   - A denied field is returned, named, and marked DENIED with a REDACTED
//     presence. Dropping it would make a denial indistinguishable from an
//     absent fact.
func ExplainWorkerState(ctx context.Context, reader WorkerFacts, req ExplainWorkerStateRequest) (Explanation, error) {
	if reader == nil {
		return Explanation{}, fmt.Errorf("%w: no worker facts reader", ErrExplainRequestInvalid)
	}
	if err := req.Validate(); err != nil {
		return Explanation{}, err
	}
	fields := req.projection()
	if err := req.Authorization.Covers(fields); err != nil {
		return Explanation{}, err
	}
	inputsDigest, err := req.inputsDigest()
	if err != nil {
		return Explanation{}, err
	}

	if !req.Authorization.SubjectDisclosable {
		return finish(Explanation{
			IntentType:      ExplainWorkerStateIntentType,
			IntentVersion:   ExplainWorkerStateIntentVersion,
			Worker:          req.Worker,
			Disclosure:      DisclosureWithheld,
			Presence:        SubjectPresenceUnspecified,
			WithheldReason:  req.Authorization.SubjectDenialReason,
			AsOf:            req.AsOf,
			Narrative:       []string{"subject is not disclosable to this caller under the evaluated policy"},
			PolicyVersion:   req.Authorization.PolicyVersion,
			RulePackVersion: ExplainRulePackVersion,
			InputsDigest:    inputsDigest,
			Effects:         evidence.ZeroEffects(),
		})
	}

	// Only fields the caller may see are ever read. An authorized-read port
	// that receives the full projection would have to be trusted to filter,
	// and a denied field would still have been loaded into this process.
	authorized := make([]FieldID, 0, len(fields))
	for _, f := range fields {
		if r, _ := req.Authorization.RulingFor(f); r.Effect == EffectAllow {
			authorized = append(authorized, f)
		}
	}

	var set FactSet
	if len(authorized) > 0 {
		set, err = reader.WorkerFactsAt(ctx, FactQuery{
			Tenant: req.Tenant,
			Worker: req.Worker,
			AsOf:   req.AsOf,
			Fields: authorized,
		})
		if err != nil {
			return Explanation{}, fmt.Errorf("%w: %w", ErrReaderFailed, err)
		}
		if err := set.Validate(); err != nil {
			return Explanation{}, err
		}
		if set.Worker != req.Worker {
			return Explanation{}, fmt.Errorf("%w: asked %s, answered %s",
				ErrFactSubjectMismatch, req.Worker, set.Worker)
		}
		for _, f := range set.Facts {
			if _, ok := req.Authorization.RulingFor(f.Field); !ok {
				return Explanation{}, fmt.Errorf("%w: reader widened the projection with %s",
					ErrAuthorizationIncomplete, f.Field)
			}
			if r, _ := req.Authorization.RulingFor(f.Field); r.Effect != EffectAllow {
				return Explanation{}, fmt.Errorf("%w: reader returned denied field %s",
					ErrAuthorizationIncomplete, f.Field)
			}
		}
	} else {
		// Every requested field was denied. Existence is disclosable, but there
		// is nothing to read, so the subject is reported as present-unknown
		// without touching the repository at all.
		set = FactSet{Worker: req.Worker, Exists: true, Watermark: values.UnspecifiedRevision()}
	}

	presence := SubjectPresent
	if !set.Exists {
		presence = SubjectAbsent
	}

	explained := make([]ExplainedFact, 0, len(fields))
	denied := 0
	for _, field := range fields {
		ruling, _ := req.Authorization.RulingFor(field)
		if ruling.Effect == EffectDeny {
			denied++
			explained = append(explained, ExplainedFact{
				Field:        field,
				Access:       AccessDenied,
				DenialReason: ruling.Reason,
				Value:        values.Redacted[string](ruling.Reason),
			})
			continue
		}
		fact, ok := set.Lookup(field)
		if !ok {
			// The reader had nothing for an authorized field. That is reported
			// as an explicit UNKNOWN with the reason, never as an empty value
			// and never by dropping the field.
			reason := "not_asserted_at_requested_coordinate"
			if !set.Exists {
				reason = "subject_absent_at_requested_coordinate"
			}
			explained = append(explained, ExplainedFact{
				Field:  field,
				Access: AccessAuthorized,
				Value:  values.Unknown[string](reason),
			})
			continue
		}
		explained = append(explained, ExplainedFact{
			Field:      field,
			Access:     AccessAuthorized,
			Value:      fact.Value,
			Effective:  fact.Effective,
			KnownAt:    fact.KnownAt,
			Revision:   fact.Revision,
			Authority:  fact.Authority,
			Provenance: fact.Provenance,
		})
	}

	disclosure := DisclosureFull
	if denied > 0 {
		disclosure = DisclosurePartial
	}

	return finish(Explanation{
		IntentType:      ExplainWorkerStateIntentType,
		IntentVersion:   ExplainWorkerStateIntentVersion,
		Worker:          req.Worker,
		Disclosure:      disclosure,
		Presence:        presence,
		AsOf:            req.AsOf,
		Fields:          explained,
		Watermark:       set.Watermark,
		Narrative:       narrate(presence, req.AsOf, len(explained), denied),
		PolicyVersion:   req.Authorization.PolicyVersion,
		RulePackVersion: ExplainRulePackVersion,
		InputsDigest:    inputsDigest,
		Effects:         evidence.ZeroEffects(),
	})
}

// narrate builds the bounded, value-free explanation. It describes shape and
// coordinates only: no narrative line ever contains a field value, because the
// narrative is not subject to the per-field rulings.
func narrate(presence SubjectPresence, asOf AsOf, total, denied int) []string {
	lines := []string{
		fmt.Sprintf("subject presence at %s known as of %s: %s",
			asOf.EffectiveOn, asOf.KnownAt, presence),
		fmt.Sprintf("%d field(s) requested, %d authorized, %d denied", total, total-denied, denied),
	}
	if denied > 0 {
		lines = append(lines, "denied fields are reported by name with a redacted value and no provenance")
	}
	if presence == SubjectAbsent {
		lines = append(lines, "no assertion exists for this subject at the requested coordinate")
	}
	return lines
}

// finish digests the body, mints the zero-effect receipt and returns the
// completed explanation.
func finish(e Explanation) (Explanation, error) {
	if len(e.Narrative) > MaxNarrativeLines {
		return Explanation{}, fmt.Errorf("%w: %d lines", ErrNarrativeOverflow, len(e.Narrative))
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
		e.InputsDigest, e.ResultDigest, e.Effects,
	)
	if err != nil {
		return Explanation{}, err
	}
	e.Receipt = receipt
	return e, nil
}
