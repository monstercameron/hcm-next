package qualification

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	QualificationReadScope    = "qualification.read"
	ResolutionIntentType      = "hcmnext.qualification.resolve_worker"
	ResolutionIntentVersion   = "v1"
	ResolutionRulePackVersion = "qualification.resolve.rules/1.0.0"
)

var (
	ErrResolutionInvalid      = errors.New("qualification: resolution request is invalid")
	ErrCredentialFactInvalid  = errors.New("qualification: credential fact is invalid")
	ErrCredentialReaderFailed = errors.New("qualification: credential facts reader failed")
	ErrCredentialSubject      = errors.New("qualification: credential reader answered about another worker")
	ErrCredentialNotFresh     = errors.New("qualification: credential fact is not fresh")
)

// CredentialFact is the typed, non-content assertion read by QUAL-002. Raw
// certificates and other evidence contents never cross this port; EvidenceRef
// is an opaque reference to such content.
type CredentialFact struct {
	Worker        values.EntityRef
	CredentialRef string
	SkillRef      string
	Issuer        string
	Level         int
	EvidenceKind  EvidenceKind
	EvidenceRef   string
	Validity      values.EffectiveInterval
	Verified      bool
	Trusted       bool
	VerifiedAt    values.Instant
	FreshUntil    values.Instant
	KnownAt       values.KnownAt
	Revision      values.RevisionToken
	Authority     evidence.SourceAuthority
	Provenance    evidence.Provenance
}

func (f CredentialFact) Validate() error {
	if err := f.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrCredentialFactInvalid, err)
	}
	if f.Worker.Kind != people.KindWorker {
		return fmt.Errorf("%w: subject is not a worker", ErrCredentialFactInvalid)
	}
	if (strings.TrimSpace(f.CredentialRef) == "") == (strings.TrimSpace(f.SkillRef) == "") {
		return fmt.Errorf("%w: exactly one credential or skill ref is required", ErrCredentialFactInvalid)
	}
	if strings.TrimSpace(f.Issuer) == "" || f.Level <= 0 || strings.TrimSpace(f.EvidenceRef) == "" {
		return fmt.Errorf("%w: issuer, positive level and evidence ref are required", ErrCredentialFactInvalid)
	}
	if !f.EvidenceKind.Valid() {
		return fmt.Errorf("%w: evidence kind %q", ErrCredentialFactInvalid, f.EvidenceKind)
	}
	if err := f.Validity.Validate(); err != nil {
		return fmt.Errorf("%w: validity: %v", ErrCredentialFactInvalid, err)
	}
	if f.Validity.Kind() != values.IntervalKindInstant && f.Validity.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: validity must be an instant or local-date interval", ErrCredentialFactInvalid)
	}
	if err := f.VerifiedAt.Validate(); err != nil {
		return fmt.Errorf("%w: verified at: %v", ErrCredentialFactInvalid, err)
	}
	if err := f.FreshUntil.Validate(); err != nil {
		return fmt.Errorf("%w: fresh until: %v", ErrCredentialFactInvalid, err)
	}
	if !f.FreshUntil.After(f.VerifiedAt) {
		return fmt.Errorf("%w: fresh until must follow verified at", ErrCredentialFactInvalid)
	}
	if f.KnownAt.Canonical() == nil || !f.Revision.IsSpecified() {
		return fmt.Errorf("%w: known-at and revision are required", ErrCredentialFactInvalid)
	}
	if err := f.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: authority: %v", ErrCredentialFactInvalid, err)
	}
	if err := f.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: provenance: %v", ErrCredentialFactInvalid, err)
	}
	if err := values.ValidateKnowledgeOrder(f.KnownAt, f.Provenance.RecordedAt, false); err != nil {
		return fmt.Errorf("%w: knowledge order: %v", ErrCredentialFactInvalid, err)
	}
	return nil
}

func (f CredentialFact) Canonical() []byte {
	if f.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.qualification.CredentialFact", schemaVersion).
		Value("worker", f.Worker).String("credential_ref", f.CredentialRef).String("skill_ref", f.SkillRef).
		String("issuer", f.Issuer).Int("level", int64(f.Level)).String("evidence_kind", string(f.EvidenceKind)).
		String("evidence_ref", f.EvidenceRef).Value("validity", f.Validity).Bool("verified", f.Verified).
		Bool("trusted", f.Trusted).Value("verified_at", f.VerifiedAt).Value("fresh_until", f.FreshUntil).
		Value("known_at", f.KnownAt).Value("revision", f.Revision).Value("authority", f.Authority).Value("provenance", f.Provenance)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// CredentialFactsQuery is the closed as-of-instant request for qualification
// facts. There is no caller-provided projection: this port returns only typed
// credential descriptors.
type CredentialFactsQuery struct {
	Tenant values.TenantId
	Worker values.EntityRef
	AsOf   values.Instant
}

func (q CredentialFactsQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrResolutionInvalid, err)
	}
	if err := q.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrResolutionInvalid, err)
	}
	if q.Worker.Tenant != q.Tenant || q.Worker.Kind != people.KindWorker {
		return fmt.Errorf("%w: worker is outside tenant or is not a worker", ErrResolutionInvalid)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrResolutionInvalid, err)
	}
	return nil
}

// CredentialFactSet is one consistent answer from a credential adapter.
type CredentialFactSet struct {
	Worker    values.EntityRef
	Exists    bool
	Facts     []CredentialFact
	Watermark values.RevisionToken
}

func (s CredentialFactSet) Validate() error {
	if err := s.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrCredentialFactInvalid, err)
	}
	if !s.Exists {
		if len(s.Facts) != 0 {
			return fmt.Errorf("%w: absent worker carries credential facts", ErrCredentialFactInvalid)
		}
		return nil
	}
	if !s.Watermark.IsSpecified() {
		return fmt.Errorf("%w: existing worker needs a read watermark", ErrCredentialFactInvalid)
	}
	seen := make(map[string]struct{}, len(s.Facts))
	for i, fact := range s.Facts {
		if err := fact.Validate(); err != nil {
			return fmt.Errorf("%w: fact %d: %v", ErrCredentialFactInvalid, i, err)
		}
		if fact.Worker != s.Worker {
			return ErrCredentialSubject
		}
		key := string(fact.Canonical())
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate credential fact", ErrCredentialFactInvalid)
		}
		seen[key] = struct{}{}
	}
	return nil
}

type CredentialFacts interface {
	CredentialFactsAt(context.Context, CredentialFactsQuery) (CredentialFactSet, error)
}

// QualificationAuthorization is the caller's already-evaluated scope result.
// This package checks the declared capability scope but never computes policy.
type QualificationAuthorization struct {
	PolicyVersion       string
	Purpose             string
	SubjectDisclosable  bool
	SubjectDenialReason string
	Scopes              []string
}

func (a QualificationAuthorization) HasScope(scope string) bool {
	for _, declared := range a.Scopes {
		if declared == scope {
			return true
		}
	}
	return false
}

func (a QualificationAuthorization) Validate() error {
	if strings.TrimSpace(a.PolicyVersion) == "" || strings.TrimSpace(a.Purpose) == "" {
		return fmt.Errorf("%w: policy version and purpose are required", ErrResolutionInvalid)
	}
	if !a.SubjectDisclosable && a.HasScope(QualificationReadScope) && strings.TrimSpace(a.SubjectDenialReason) == "" {
		return fmt.Errorf("%w: non-disclosable subject needs a reason", ErrResolutionInvalid)
	}
	seen := make(map[string]struct{}, len(a.Scopes))
	for _, scope := range a.Scopes {
		if strings.TrimSpace(scope) == "" {
			return fmt.Errorf("%w: empty authorization scope", ErrResolutionInvalid)
		}
		if _, ok := seen[scope]; ok {
			return fmt.Errorf("%w: duplicate authorization scope %q", ErrResolutionInvalid, scope)
		}
		seen[scope] = struct{}{}
	}
	return nil
}

type QualificationResolutionRequest struct {
	Tenant        values.TenantId
	Worker        values.EntityRef
	AsOf          values.Instant
	Requirement   QualificationRequirement
	Authorization QualificationAuthorization
}

// ResolutionRequest is a concise alias for callers composing the resolver.
type ResolutionRequest = QualificationResolutionRequest

func (r QualificationResolutionRequest) Validate() error {
	if err := (CredentialFactsQuery{Tenant: r.Tenant, Worker: r.Worker, AsOf: r.AsOf}).Validate(); err != nil {
		return err
	}
	if err := r.Requirement.Validate(); err != nil {
		return fmt.Errorf("%w: requirement: %v", ErrResolutionInvalid, err)
	}
	return r.Authorization.Validate()
}

// QualificationResolution is the authorized, detached answer. Credentials
// are present only when the caller has qualification.read and the descriptor
// was trusted, verified, valid and fresh at AsOf.
type QualificationResolution struct {
	Worker          values.EntityRef
	RequirementID   string
	Revision        uint64
	AsOf            values.Instant
	Disclosure      people.Disclosure
	Presence        people.SubjectPresence
	WithheldReason  string
	Credentials     []CredentialFact
	Evaluation      Evaluation
	Watermark       values.RevisionToken
	PolicyVersion   string
	ExpiredCount    int
	RestrictedCount int
	InputsDigest    string
	ResultDigest    string
}

func (r QualificationResolution) canonicalBody() []byte {
	w := canonicalbytes.New("hcmnext.domains.qualification.QualificationResolution", schemaVersion).
		Value("worker", r.Worker).String("requirement_id", r.RequirementID).Int("revision", int64(r.Revision)).
		Value("as_of", r.AsOf).String("disclosure", r.Disclosure.String()).String("presence", r.Presence.String()).
		String("withheld_reason", r.WithheldReason).Count("credentials", len(r.Credentials))
	for _, fact := range r.Credentials {
		w.Field("credential", fact.Canonical())
	}
	w.Count("evaluation", len(r.Evaluation.Results))
	for _, result := range r.Evaluation.Results {
		w.String("result.kind", string(result.Kind)).String("result.ref", result.Ref).
			Int("result.required_level", int64(result.RequiredLevel)).String("result.status", string(result.Status)).
			String("result.gap", result.Gap).String("result.evidence_ref", result.EvidenceRef)
	}
	w.Bool("watermark?", r.Watermark.IsSpecified())
	if r.Watermark.IsSpecified() {
		w.Value("watermark", r.Watermark)
	}
	raw, err := w.String("policy_version", r.PolicyVersion).String("inputs_digest", r.InputsDigest).
		Int("expired_count", int64(r.ExpiredCount)).
		Int("restricted_count", int64(r.RestrictedCount)).Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r QualificationResolution) Canonical() []byte {
	body := r.canonicalBody()
	if len(body) == 0 {
		return nil
	}
	return body
}

func (r QualificationResolution) Digest() (string, error) {
	body := r.Canonical()
	if len(body) == 0 {
		return "", ErrResolutionInvalid
	}
	return canonicalbytes.Digest(body), nil
}

func qualificationInputDigest(r QualificationResolutionRequest) (string, error) {
	scopes := append([]string(nil), r.Authorization.Scopes...)
	sort.Strings(scopes)
	w := canonicalbytes.New("hcmnext.domains.qualification.QualificationResolutionRequest", schemaVersion).
		String("tenant", string(r.Tenant)).Value("worker", r.Worker).Value("as_of", r.AsOf).
		Value("requirement", r.Requirement).String("policy_version", r.Authorization.PolicyVersion).
		String("purpose", r.Authorization.Purpose).Bool("subject_disclosable", r.Authorization.SubjectDisclosable).
		String("subject_denial_reason", r.Authorization.SubjectDenialReason).SortedStrings("scope", scopes)
	return w.Digest()
}

func (r QualificationResolution) Explain() (QualificationResolutionExplanation, error) {
	if r.ResultDigest == "" || r.RequirementID == "" || r.ResultDigest != canonicalbytes.Digest(r.canonicalBody()) {
		return QualificationResolutionExplanation{}, ErrResolutionInvalid
	}
	return QualificationResolutionExplanation{
		Worker: r.Worker, RequirementID: r.RequirementID, Revision: r.Revision, AsOf: r.AsOf,
		Disclosure: r.Disclosure, Presence: r.Presence, WithheldReason: r.WithheldReason,
		CredentialCount: len(r.Credentials), ExpiredCount: r.ExpiredCount, RestrictedCount: r.RestrictedCount,
		EvaluationDigest: r.Evaluation.CanonicalDigest, ResultDigest: r.ResultDigest,
		Watermark: r.Watermark, PolicyVersion: r.PolicyVersion,
	}, nil
}

type QualificationResolutionExplanation struct {
	Worker           values.EntityRef
	RequirementID    string
	Revision         uint64
	AsOf             values.Instant
	Disclosure       people.Disclosure
	Presence         people.SubjectPresence
	WithheldReason   string
	CredentialCount  int
	ExpiredCount     int
	RestrictedCount  int
	EvaluationDigest string
	ResultDigest     string
	Watermark        values.RevisionToken
	PolicyVersion    string
}

func ExplainResolution(r QualificationResolution) (QualificationResolutionExplanation, error) {
	return r.Explain()
}

func eligibleCredential(f CredentialFact, asOf values.Instant) (bool, bool, error) {
	if err := f.Validate(); err != nil {
		return false, false, err
	}
	if !f.Trusted || !f.Verified {
		return false, false, nil
	}
	var valid bool
	var err error
	if f.Validity.Kind() == values.IntervalKindInstant {
		valid, err = f.Validity.ContainsInstant(asOf)
	} else {
		at := asOf.Time()
		date, dateErr := values.NewLocalDate(at.Year(), at.Month(), at.Day())
		if dateErr != nil {
			return false, false, dateErr
		}
		valid, err = f.Validity.ContainsDate(date)
	}
	if err != nil {
		return false, false, err
	}
	fresh := !asOf.After(f.FreshUntil) && !f.KnownAt.Instant().After(asOf) && !f.VerifiedAt.After(asOf)
	if !valid || !fresh {
		return false, true, nil
	}
	return true, false, nil
}

func sortCredentialFacts(facts []CredentialFact) {
	sort.Slice(facts, func(i, j int) bool {
		left, right := facts[i], facts[j]
		lk, rk := left.CredentialRef+"\x00"+left.SkillRef, right.CredentialRef+"\x00"+right.SkillRef
		if lk != rk {
			return lk < rk
		}
		if left.Issuer != right.Issuer {
			return left.Issuer < right.Issuer
		}
		return left.EvidenceRef < right.EvidenceRef
	})
}

func finishQualificationResolution(r QualificationResolution) (QualificationResolution, error) {
	body := r.canonicalBody()
	if len(body) == 0 {
		return QualificationResolution{}, ErrResolutionInvalid
	}
	r.ResultDigest = canonicalbytes.Digest(body)
	return r, nil
}

// ResolveWorkerQualification performs the QUAL-002 read, filters facts using
// the as-of instant, and evaluates QUAL-001 only against disclosed facts.
func ResolveWorkerQualification(ctx context.Context, reader CredentialFacts, req QualificationResolutionRequest) (QualificationResolution, error) {
	if err := req.Validate(); err != nil {
		return QualificationResolution{}, err
	}
	inputDigest, err := qualificationInputDigest(req)
	if err != nil {
		return QualificationResolution{}, err
	}
	result := QualificationResolution{
		Worker: req.Worker, RequirementID: req.Requirement.RequirementID, Revision: req.Requirement.Revision,
		AsOf: req.AsOf, PolicyVersion: req.Authorization.PolicyVersion, Presence: people.SubjectPresent,
		Disclosure: people.DisclosureFull, Watermark: values.UnspecifiedRevision(), InputsDigest: inputDigest,
	}
	if !req.Authorization.HasScope(QualificationReadScope) || !req.Authorization.SubjectDisclosable {
		result.Disclosure = people.DisclosureWithheld
		result.Presence = people.SubjectPresenceUnspecified
		result.WithheldReason = req.Authorization.SubjectDenialReason
		if result.WithheldReason == "" {
			result.WithheldReason = "missing_scope:" + QualificationReadScope
		}
		result.Evaluation, err = req.Requirement.Evaluate(nil)
		if err != nil {
			return QualificationResolution{}, err
		}
		return finishQualificationResolution(result)
	}
	if reader == nil {
		return QualificationResolution{}, fmt.Errorf("%w: no credential facts reader", ErrResolutionInvalid)
	}
	set, readErr := reader.CredentialFactsAt(ctx, CredentialFactsQuery{Tenant: req.Tenant, Worker: req.Worker, AsOf: req.AsOf})
	if readErr != nil {
		return QualificationResolution{}, fmt.Errorf("%w: %v", ErrCredentialReaderFailed, readErr)
	}
	if err := set.Validate(); err != nil {
		return QualificationResolution{}, err
	}
	if set.Worker != req.Worker {
		return QualificationResolution{}, fmt.Errorf("%w: asked %s, answered %s", ErrCredentialSubject, req.Worker, set.Worker)
	}
	result.Watermark = set.Watermark
	if !set.Exists {
		result.Presence = people.SubjectAbsent
	}
	for _, fact := range set.Facts {
		include, excluded, factErr := eligibleCredential(fact, req.AsOf)
		if factErr != nil {
			return QualificationResolution{}, factErr
		}
		if !include {
			if excluded {
				result.ExpiredCount++
			} else {
				result.RestrictedCount++
			}
			continue
		}
		result.Credentials = append(result.Credentials, fact)
	}
	sortCredentialFacts(result.Credentials)
	held := make([]HeldCredential, 0, len(result.Credentials))
	for _, fact := range result.Credentials {
		held = append(held, HeldCredential{
			CredentialRef: fact.CredentialRef, SkillRef: fact.SkillRef, Level: fact.Level,
			EvidenceKind: fact.EvidenceKind, EvidenceRef: fact.EvidenceRef, Validity: fact.Validity,
		})
	}
	result.Evaluation, err = req.Requirement.Evaluate(held)
	if err != nil {
		return QualificationResolution{}, err
	}
	return finishQualificationResolution(result)
}

func Resolve(ctx context.Context, reader CredentialFacts, req QualificationResolutionRequest) (QualificationResolution, error) {
	return ResolveWorkerQualification(ctx, reader, req)
}

func ResolveAuthorizedQualification(ctx context.Context, reader CredentialFacts, req QualificationResolutionRequest) (QualificationResolution, error) {
	return ResolveWorkerQualification(ctx, reader, req)
}
