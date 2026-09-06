// Package proofing owns identity-proofing sessions and work-authorization
// evidence. It records references and digests only; document images and other
// protected payloads remain in their governed custody boundary.
package proofing

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports the proofing vocabulary version.
func Version() int { return schemaVersion }

// FieldError identifies the field that made a typed record invalid.
type FieldError struct {
	Field   string
	Problem string
}

func (e FieldError) Error() string { return "proofing: " + e.Field + ": " + e.Problem }

var (
	ErrInvalidSession        = errors.New("proofing: invalid identity-proofing session")
	ErrInvalidEvidence       = errors.New("proofing: invalid evidence item")
	ErrInvalidAuthorization  = errors.New("proofing: invalid work-authorization evidence")
	ErrInvalidResult         = errors.New("proofing: invalid work-authorization result")
	ErrUnsupportedAssurance  = errors.New("proofing: evidence does not support requested assurance")
	ErrRevisionLineage       = errors.New("proofing: revision lineage is invalid")
	ErrSessionNotFound       = errors.New("proofing: session not found")
	ErrAuthorizationNotFound = errors.New("proofing: authorization evidence not found")
)

// AssuranceLevel is the closed identity assurance vocabulary used by this
// domain. It is intentionally descriptive and does not itself grant access.
type AssuranceLevel string

const (
	AssuranceIAL1 AssuranceLevel = "IAL1"
	AssuranceIAL2 AssuranceLevel = "IAL2"
	AssuranceIAL3 AssuranceLevel = "IAL3"

	IAL1 = AssuranceIAL1
	IAL2 = AssuranceIAL2
	IAL3 = AssuranceIAL3
)

func (a AssuranceLevel) Valid() bool {
	switch a {
	case AssuranceIAL1, AssuranceIAL2, AssuranceIAL3:
		return true
	default:
		return false
	}
}

func (a AssuranceLevel) rank() int {
	switch a {
	case AssuranceIAL1:
		return 1
	case AssuranceIAL2:
		return 2
	case AssuranceIAL3:
		return 3
	default:
		return 0
	}
}

// EvidenceKind is the closed set of proofing evidence descriptors. The
// descriptor is safe to project; EvidenceDigest refers to protected custody.
type EvidenceKind string

const (
	EvidenceGovernmentID      EvidenceKind = "GOVERNMENT_ID"
	EvidencePassport          EvidenceKind = "PASSPORT"
	EvidenceDriversLicense    EvidenceKind = "DRIVERS_LICENSE"
	EvidenceNationalID        EvidenceKind = "NATIONAL_ID"
	EvidenceSelfieLiveness    EvidenceKind = "SELFIE_LIVENESS"
	EvidenceKnowledgeCheck    EvidenceKind = "KNOWLEDGE_CHECK"
	EvidenceProviderAssertion EvidenceKind = "PROVIDER_ASSERTION"
	EvidenceWorkAuthorization EvidenceKind = "WORK_AUTHORIZATION"
)

func (k EvidenceKind) Valid() bool {
	switch k {
	case EvidenceGovernmentID, EvidencePassport, EvidenceDriversLicense,
		EvidenceNationalID, EvidenceSelfieLiveness, EvidenceKnowledgeCheck,
		EvidenceProviderAssertion, EvidenceWorkAuthorization:
		return true
	default:
		return false
	}
}

// ProofingOutcome is the closed lifecycle result for a proofing session.
type ProofingOutcome string

const (
	OutcomeVerified       ProofingOutcome = "VERIFIED"
	OutcomeReviewRequired ProofingOutcome = "REVIEW_REQUIRED"
	OutcomeRejected       ProofingOutcome = "REJECTED"
	OutcomeExpired        ProofingOutcome = "EXPIRED"
	OutcomeUnknown        ProofingOutcome = "UNKNOWN"

	Verified       = OutcomeVerified
	ReviewRequired = OutcomeReviewRequired
	Rejected       = OutcomeRejected
	Expired        = OutcomeExpired
	Unknown        = OutcomeUnknown
)

func (o ProofingOutcome) Valid() bool {
	switch o {
	case OutcomeVerified, OutcomeReviewRequired, OutcomeRejected, OutcomeExpired, OutcomeUnknown:
		return true
	default:
		return false
	}
}

func digestString(value string) bool {
	return strings.HasPrefix(value, canonicalbytes.DigestAlgorithm+":") && len(value) > len(canonicalbytes.DigestAlgorithm)+1
}

// EvidenceItem is a typed pointer to protected evidence. It deliberately has
// no raw-value or image field.
type EvidenceItem struct {
	Kind                      EvidenceKind
	EvidenceDigest            string
	ProviderObservationDigest string
	CustodyRef                string
	Supports                  AssuranceLevel
	CollectedAt               values.Instant
}

func NewEvidenceItem(kind EvidenceKind, evidenceDigest, providerObservationDigest, custodyRef string, supports AssuranceLevel, collectedAt values.Instant) (EvidenceItem, error) {
	e := EvidenceItem{Kind: kind, EvidenceDigest: evidenceDigest, ProviderObservationDigest: providerObservationDigest, CustodyRef: custodyRef, Supports: supports, CollectedAt: collectedAt}
	return e, e.Validate()
}

func (e EvidenceItem) Validate() error {
	if !e.Kind.Valid() {
		return fmt.Errorf("%w: kind is not declared", ErrInvalidEvidence)
	}
	if !digestString(e.EvidenceDigest) {
		return fmt.Errorf("%w: evidence digest is required", ErrInvalidEvidence)
	}
	if strings.TrimSpace(e.ProviderObservationDigest) != "" && !digestString(e.ProviderObservationDigest) {
		return fmt.Errorf("%w: provider observation digest is invalid", ErrInvalidEvidence)
	}
	if strings.TrimSpace(e.CustodyRef) == "" {
		return fmt.Errorf("%w: custody ref is required", ErrInvalidEvidence)
	}
	if !e.Supports.Valid() {
		return fmt.Errorf("%w: supports assurance is not declared", ErrInvalidEvidence)
	}
	if err := e.CollectedAt.Validate(); err != nil {
		return fmt.Errorf("%w: collected at: %v", ErrInvalidEvidence, err)
	}
	return nil
}

func (e EvidenceItem) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.proofing.EvidenceItem", schemaVersion).
		String("kind", string(e.Kind)).String("evidence_digest", e.EvidenceDigest).
		String("provider_observation_digest", e.ProviderObservationDigest).String("custody_ref", e.CustodyRef).
		String("supports", string(e.Supports)).Value("collected_at", e.CollectedAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func maxSupported(items []EvidenceItem) AssuranceLevel {
	best := AssuranceLevel("")
	for _, item := range items {
		if item.Supports.rank() > best.rank() {
			best = item.Supports
		}
	}
	return best
}

// ProofingSession is an immutable revision of an identity-proofing attempt.
// Outcome UNKNOWN is the initial state; changing it returns a successor.
type ProofingSession struct {
	SessionID          string
	Subject            values.EntityRef
	Purpose            string
	Target             AssuranceLevel
	Evidence           []EvidenceItem
	VerifierPrincipal  string
	Outcome            ProofingOutcome
	ExpiresAt          values.Instant
	Revision           uint64
	SupersedesRevision uint64
	CanonicalDigest    string
}

// IdentityProofSession is the domain-facing name used by workflow adapters.
type IdentityProofSession = ProofingSession

func NewProofingSession(sessionID string, subject values.EntityRef, purpose string, target AssuranceLevel, evidence []EvidenceItem, verifierPrincipal string, expiresAt values.Instant) (ProofingSession, error) {
	s := ProofingSession{SessionID: sessionID, Subject: subject, Purpose: purpose, Target: target, Evidence: cloneEvidence(evidence), VerifierPrincipal: verifierPrincipal, Outcome: OutcomeUnknown, ExpiresAt: expiresAt, Revision: 1}
	s.CanonicalDigest = s.computedDigest()
	if err := s.Validate(); err != nil {
		return ProofingSession{}, err
	}
	return s, nil
}

func NewIdentityProofSession(sessionID string, subject values.EntityRef, purpose string, target AssuranceLevel, evidence []EvidenceItem, verifierPrincipal string, expiresAt values.Instant) (IdentityProofSession, error) {
	return NewProofingSession(sessionID, subject, purpose, target, evidence, verifierPrincipal, expiresAt)
}

func cloneEvidence(in []EvidenceItem) []EvidenceItem { return append([]EvidenceItem(nil), in...) }

func (s ProofingSession) Validate() error {
	if strings.TrimSpace(s.SessionID) == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidSession)
	}
	if err := s.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalidSession, err)
	}
	if strings.TrimSpace(s.Purpose) == "" {
		return fmt.Errorf("%w: purpose is required", ErrInvalidSession)
	}
	if !s.Target.Valid() {
		return fmt.Errorf("%w: target assurance is not declared", ErrInvalidSession)
	}
	if strings.TrimSpace(s.VerifierPrincipal) == "" {
		return fmt.Errorf("%w: verifier principal is required", ErrInvalidSession)
	}
	if !s.Outcome.Valid() {
		return fmt.Errorf("%w: outcome is not declared", ErrInvalidSession)
	}
	if err := s.ExpiresAt.Validate(); err != nil {
		return fmt.Errorf("%w: expiry: %v", ErrInvalidSession, err)
	}
	if s.Revision == 0 || s.SupersedesRevision >= s.Revision && s.SupersedesRevision != 0 {
		return fmt.Errorf("%w: revision lineage is invalid", ErrInvalidSession)
	}
	if len(s.Evidence) == 0 {
		return fmt.Errorf("%w: evidence is required", ErrInvalidSession)
	}
	seen := make(map[string]struct{}, len(s.Evidence))
	for _, item := range s.Evidence {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("%w: evidence: %v", ErrInvalidSession, err)
		}
		if _, ok := seen[item.EvidenceDigest]; ok {
			return fmt.Errorf("%w: duplicate evidence digest", ErrInvalidSession)
		}
		seen[item.EvidenceDigest] = struct{}{}
	}
	if s.CanonicalDigest == "" || s.CanonicalDigest != s.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidSession)
	}
	return nil
}

func (s ProofingSession) body() []byte {
	items := cloneEvidence(s.Evidence)
	sort.Slice(items, func(i, j int) bool { return items[i].EvidenceDigest < items[j].EvidenceDigest })
	w := canonicalbytes.New("hcmnext.domains.proofing.ProofingSession", schemaVersion).
		String("session_id", s.SessionID).Value("subject", s.Subject).String("purpose", s.Purpose).
		String("target", string(s.Target)).String("verifier_principal", s.VerifierPrincipal).
		String("outcome", string(s.Outcome)).Value("expires_at", s.ExpiresAt).
		Int("revision", int64(s.Revision)).Int("supersedes_revision", int64(s.SupersedesRevision)).Count("evidence", len(items))
	for _, item := range items {
		w.Value("evidence", item)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (s ProofingSession) computedDigest() string { return canonicalbytes.Digest(s.body()) }
func (s ProofingSession) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	return s.body()
}

// Outcome returns a successor session. VERIFIED is refused unless the
// declared evidence supports the target assurance level.
func (s ProofingSession) OutcomeRevision(outcome ProofingOutcome) (ProofingSession, error) {
	if err := s.Validate(); err != nil {
		return ProofingSession{}, err
	}
	if !outcome.Valid() {
		return ProofingSession{}, fmt.Errorf("%w: outcome is not declared", ErrInvalidSession)
	}
	if outcome == OutcomeVerified && maxSupported(s.Evidence).rank() < s.Target.rank() {
		return ProofingSession{}, fmt.Errorf("%w: target %s exceeds evidence ceiling %s", ErrUnsupportedAssurance, s.Target, maxSupported(s.Evidence))
	}
	next := s
	next.Evidence = cloneEvidence(s.Evidence)
	next.Outcome = outcome
	next.Revision = s.Revision + 1
	next.SupersedesRevision = s.Revision
	next.CanonicalDigest = next.computedDigest()
	return next, next.Validate()
}

func (s ProofingSession) RecordOutcome(outcome ProofingOutcome) (ProofingSession, error) {
	return s.OutcomeRevision(outcome)
}

// Evaluate expires a session at the closed expiry boundary and otherwise
// returns the immutable session unchanged.
func (s ProofingSession) Evaluate(now values.Instant) (ProofingSession, error) {
	if err := s.Validate(); err != nil {
		return ProofingSession{}, err
	}
	if err := now.Validate(); err != nil {
		return ProofingSession{}, fmt.Errorf("%w: evaluation time: %v", ErrInvalidSession, err)
	}
	if !now.Before(s.ExpiresAt) && s.Outcome != OutcomeExpired {
		return s.OutcomeRevision(OutcomeExpired)
	}
	return s, nil
}

type ProofingExplanation struct {
	SessionID       string
	Subject         values.EntityRef
	Purpose         string
	Target          AssuranceLevel
	Outcome         ProofingOutcome
	Revision        uint64
	EvidenceKinds   []EvidenceKind
	EvidenceCount   int
	EvidenceCeiling AssuranceLevel
	ExpiresAt       values.Instant
	CanonicalDigest string
}

func (s ProofingSession) Explain() (ProofingExplanation, error) {
	if err := s.Validate(); err != nil {
		return ProofingExplanation{}, err
	}
	kinds := make([]EvidenceKind, 0, len(s.Evidence))
	for _, item := range s.Evidence {
		kinds = append(kinds, item.Kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return ProofingExplanation{SessionID: s.SessionID, Subject: s.Subject, Purpose: s.Purpose, Target: s.Target, Outcome: s.Outcome, Revision: s.Revision, EvidenceKinds: kinds, EvidenceCount: len(s.Evidence), EvidenceCeiling: maxSupported(s.Evidence), ExpiresAt: s.ExpiresAt, CanonicalDigest: s.CanonicalDigest}, nil
}

// Explain is the package-level proofing explanation contract.
func Explain(s ProofingSession) (ProofingExplanation, error) { return s.Explain() }

// DocumentClass is the closed vocabulary for work-authorization documents.
type DocumentClass string

const (
	DocumentPassport              DocumentClass = "PASSPORT"
	DocumentNationalIdentity      DocumentClass = "NATIONAL_ID"
	DocumentResidencePermit       DocumentClass = "RESIDENCE_PERMIT"
	DocumentWorkPermit            DocumentClass = "WORK_PERMIT"
	DocumentEmploymentCertificate DocumentClass = "EMPLOYMENT_CERTIFICATE"
)

func (d DocumentClass) Valid() bool {
	switch d {
	case DocumentPassport, DocumentNationalIdentity, DocumentResidencePermit, DocumentWorkPermit, DocumentEmploymentCertificate:
		return true
	default:
		return false
	}
}

// VerificationMethod is a closed set of declared work-authorization checks.
type VerificationMethod string

const (
	VerificationManualReview     VerificationMethod = "MANUAL_REVIEW"
	VerificationGovernmentSource VerificationMethod = "GOVERNMENT_SOURCE"
	VerificationEmployerReview   VerificationMethod = "EMPLOYER_REVIEW"
	VerificationProviderCheck    VerificationMethod = "PROVIDER_CHECK"
)

func (m VerificationMethod) Valid() bool {
	switch m {
	case VerificationManualReview, VerificationGovernmentSource, VerificationEmployerReview, VerificationProviderCheck:
		return true
	default:
		return false
	}
}

// WorkAuthorizationEvidence is an immutable, effective-dated evidence
// revision. EvidenceDigest points to protected custody and never to an image
// embedded in this record.
type WorkAuthorizationEvidence struct {
	EvidenceID         string
	Subject            values.EntityRef
	Revision           uint64
	SupersedesRevision uint64
	DocumentClass      DocumentClass
	VerificationMethod VerificationMethod
	Jurisdiction       string
	Category           string
	ValidFrom          values.LocalDate
	ValidUntil         values.LocalDate
	ReverificationDue  values.LocalDate
	EvidenceDigest     string
	SourceRef          string
	CanonicalDigest    string
}

// WorkAuthorizationRevision emphasizes that corrections append a successor.
type WorkAuthorizationRevision = WorkAuthorizationEvidence

func NewWorkAuthorizationEvidence(e WorkAuthorizationEvidence) (WorkAuthorizationEvidence, error) {
	e.CanonicalDigest = e.computedDigest()
	if err := e.Validate(); err != nil {
		return WorkAuthorizationEvidence{}, err
	}
	return e, nil
}

func (e WorkAuthorizationEvidence) Validate() error {
	if strings.TrimSpace(e.EvidenceID) == "" {
		return fmt.Errorf("%w: evidence id is required", ErrInvalidAuthorization)
	}
	if err := e.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalidAuthorization, err)
	}
	if e.Revision == 0 || e.SupersedesRevision >= e.Revision && e.SupersedesRevision != 0 {
		return fmt.Errorf("%w: revision lineage is invalid", ErrInvalidAuthorization)
	}
	if !e.DocumentClass.Valid() {
		return fmt.Errorf("%w: document class is not declared", ErrInvalidAuthorization)
	}
	if !e.VerificationMethod.Valid() {
		return fmt.Errorf("%w: verification method is not declared", ErrInvalidAuthorization)
	}
	if strings.TrimSpace(e.Jurisdiction) == "" || strings.TrimSpace(e.Category) == "" {
		return fmt.Errorf("%w: jurisdiction and category are required", ErrInvalidAuthorization)
	}
	if err := e.ValidFrom.Validate(); err != nil {
		return fmt.Errorf("%w: valid from: %v", ErrInvalidAuthorization, err)
	}
	if err := e.ValidUntil.Validate(); err != nil {
		return fmt.Errorf("%w: valid until: %v", ErrInvalidAuthorization, err)
	}
	if e.ValidFrom.Compare(e.ValidUntil) >= 0 {
		return fmt.Errorf("%w: valid until must follow valid from", ErrInvalidAuthorization)
	}
	if err := e.ReverificationDue.Validate(); err != nil {
		return fmt.Errorf("%w: re-verification due: %v", ErrInvalidAuthorization, err)
	}
	if e.ReverificationDue.Compare(e.ValidFrom) < 0 || e.ReverificationDue.Compare(e.ValidUntil) >= 0 {
		return fmt.Errorf("%w: re-verification due must be within validity", ErrInvalidAuthorization)
	}
	if !digestString(e.EvidenceDigest) {
		return fmt.Errorf("%w: evidence digest is required", ErrInvalidAuthorization)
	}
	if strings.TrimSpace(e.SourceRef) == "" {
		return fmt.Errorf("%w: source ref is required", ErrInvalidAuthorization)
	}
	if e.CanonicalDigest == "" || e.CanonicalDigest != e.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidAuthorization)
	}
	return nil
}

func (e WorkAuthorizationEvidence) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.proofing.WorkAuthorizationEvidence", schemaVersion).
		String("evidence_id", e.EvidenceID).Value("subject", e.Subject).Int("revision", int64(e.Revision)).Int("supersedes_revision", int64(e.SupersedesRevision)).
		String("document_class", string(e.DocumentClass)).String("verification_method", string(e.VerificationMethod)).String("jurisdiction", e.Jurisdiction).String("category", e.Category).
		Value("valid_from", e.ValidFrom).Value("valid_until", e.ValidUntil).Value("reverification_due", e.ReverificationDue).
		String("evidence_digest", e.EvidenceDigest).String("source_ref", e.SourceRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (e WorkAuthorizationEvidence) computedDigest() string { return canonicalbytes.Digest(e.body()) }
func (e WorkAuthorizationEvidence) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	return e.body()
}

// Successor appends a corrected evidence revision without rewriting history.
func (e WorkAuthorizationEvidence) Successor(next WorkAuthorizationEvidence) (WorkAuthorizationEvidence, error) {
	if err := e.Validate(); err != nil {
		return WorkAuthorizationEvidence{}, err
	}
	if next.EvidenceID != e.EvidenceID || next.Subject != e.Subject || next.Revision != e.Revision+1 || next.SupersedesRevision != e.Revision {
		return WorkAuthorizationEvidence{}, fmt.Errorf("%w: successor must point to the prior evidence revision", ErrRevisionLineage)
	}
	return NewWorkAuthorizationEvidence(next)
}

// AuthorizationDecision is separate from the evidence record and is always
// bound to an explicit effective interval.
type AuthorizationDecision string

const (
	DecisionAuthorized AuthorizationDecision = "AUTHORIZED"
	DecisionReview     AuthorizationDecision = "REVIEW_REQUIRED"
	DecisionRejected   AuthorizationDecision = "REJECTED"
	DecisionExpired    AuthorizationDecision = "EXPIRED"
	DecisionUnknown    AuthorizationDecision = "UNKNOWN"
)

func (d AuthorizationDecision) Valid() bool {
	switch d {
	case DecisionAuthorized, DecisionReview, DecisionRejected, DecisionExpired, DecisionUnknown:
		return true
	default:
		return false
	}
}

type WorkAuthorizationResult struct {
	ResultID               string
	Subject                values.EntityRef
	EvidenceRevisionDigest string
	Jurisdiction           string
	Category               string
	EffectiveFrom          values.LocalDate
	EffectiveUntil         values.LocalDate
	Decision               AuthorizationDecision
	CanonicalDigest        string
}

func NewWorkAuthorizationResult(result WorkAuthorizationResult) (WorkAuthorizationResult, error) {
	result.CanonicalDigest = result.computedDigest()
	if err := result.Validate(); err != nil {
		return WorkAuthorizationResult{}, err
	}
	return result, nil
}

func (r WorkAuthorizationResult) Validate() error {
	if strings.TrimSpace(r.ResultID) == "" {
		return fmt.Errorf("%w: result id is required", ErrInvalidResult)
	}
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalidResult, err)
	}
	if !digestString(r.EvidenceRevisionDigest) {
		return fmt.Errorf("%w: evidence revision digest is required", ErrInvalidResult)
	}
	if strings.TrimSpace(r.Jurisdiction) == "" || strings.TrimSpace(r.Category) == "" {
		return fmt.Errorf("%w: jurisdiction and category are required", ErrInvalidResult)
	}
	if err := r.EffectiveFrom.Validate(); err != nil {
		return fmt.Errorf("%w: effective from: %v", ErrInvalidResult, err)
	}
	if err := r.EffectiveUntil.Validate(); err != nil {
		return fmt.Errorf("%w: effective until: %v", ErrInvalidResult, err)
	}
	if r.EffectiveFrom.Compare(r.EffectiveUntil) >= 0 {
		return fmt.Errorf("%w: effective until must follow effective from", ErrInvalidResult)
	}
	if !r.Decision.Valid() {
		return fmt.Errorf("%w: decision is not declared", ErrInvalidResult)
	}
	if r.CanonicalDigest == "" || r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidResult)
	}
	return nil
}

func (r WorkAuthorizationResult) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.proofing.WorkAuthorizationResult", schemaVersion).
		String("result_id", r.ResultID).Value("subject", r.Subject).String("evidence_revision_digest", r.EvidenceRevisionDigest).
		String("jurisdiction", r.Jurisdiction).String("category", r.Category).Value("effective_from", r.EffectiveFrom).Value("effective_until", r.EffectiveUntil).
		String("decision", string(r.Decision)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r WorkAuthorizationResult) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r WorkAuthorizationResult) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

type WorkAuthorizationExplanation struct {
	ResultID               string
	Subject                values.EntityRef
	EvidenceRevisionDigest string
	Jurisdiction           string
	Category               string
	EffectiveFrom          values.LocalDate
	EffectiveUntil         values.LocalDate
	Decision               AuthorizationDecision
	Digest                 string
}

func (r WorkAuthorizationResult) Explain() (WorkAuthorizationExplanation, error) {
	if err := r.Validate(); err != nil {
		return WorkAuthorizationExplanation{}, err
	}
	return WorkAuthorizationExplanation{ResultID: r.ResultID, Subject: r.Subject, EvidenceRevisionDigest: r.EvidenceRevisionDigest, Jurisdiction: r.Jurisdiction, Category: r.Category, EffectiveFrom: r.EffectiveFrom, EffectiveUntil: r.EffectiveUntil, Decision: r.Decision, Digest: r.CanonicalDigest}, nil
}

// ExplainWorkAuthorization is the explicit result explanation helper.
func ExplainWorkAuthorization(r WorkAuthorizationResult) (WorkAuthorizationExplanation, error) {
	return r.Explain()
}

// SessionStore is the in-memory-friendly port for immutable proofing records.
type SessionStore interface {
	Put(ProofingSession) error
	Get(string) (ProofingSession, bool)
	History(string) ([]ProofingSession, error)
}

// InMemorySessionStore is a concurrency-safe fake; it stores detached value
// records and never exposes a mutation or a document payload.
type InMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string][]ProofingSession
}

func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{sessions: make(map[string][]ProofingSession)}
}

func (s *InMemorySessionStore) Put(session ProofingSession) error {
	if s == nil {
		return ErrInvalidSession
	}
	if err := session.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.sessions[session.SessionID]
	if len(history) == 0 {
		if session.Revision != 1 || session.SupersedesRevision != 0 {
			return ErrRevisionLineage
		}
	} else {
		prior := history[len(history)-1]
		if session.Revision != prior.Revision+1 || session.SupersedesRevision != prior.Revision {
			return ErrRevisionLineage
		}
	}
	s.sessions[session.SessionID] = append(history, session)
	return nil
}

func (s *InMemorySessionStore) Get(id string) (ProofingSession, bool) {
	if s == nil {
		return ProofingSession{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := s.sessions[id]
	if len(history) == 0 {
		return ProofingSession{}, false
	}
	return history[len(history)-1], true
}

func (s *InMemorySessionStore) History(id string) ([]ProofingSession, error) {
	if s == nil {
		return nil, ErrSessionNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	history, ok := s.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return append([]ProofingSession(nil), history...), nil
}

// AuthorizationStore is the analogous in-memory port for work-authorization
// evidence revisions.
type AuthorizationStore interface {
	Put(WorkAuthorizationEvidence) error
	Get(string) (WorkAuthorizationEvidence, bool)
}

type InMemoryAuthorizationStore struct {
	mu   sync.RWMutex
	byID map[string][]WorkAuthorizationEvidence
}

func NewInMemoryAuthorizationStore() *InMemoryAuthorizationStore {
	return &InMemoryAuthorizationStore{byID: make(map[string][]WorkAuthorizationEvidence)}
}

func (s *InMemoryAuthorizationStore) Put(evidence WorkAuthorizationEvidence) error {
	if s == nil {
		return ErrInvalidAuthorization
	}
	if err := evidence.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.byID[evidence.EvidenceID]
	if len(history) == 0 {
		if evidence.Revision != 1 || evidence.SupersedesRevision != 0 {
			return ErrRevisionLineage
		}
	} else {
		prior := history[len(history)-1]
		if evidence.Revision != prior.Revision+1 || evidence.SupersedesRevision != prior.Revision {
			return ErrRevisionLineage
		}
	}
	s.byID[evidence.EvidenceID] = append(history, evidence)
	return nil
}

func (s *InMemoryAuthorizationStore) Get(id string) (WorkAuthorizationEvidence, bool) {
	if s == nil {
		return WorkAuthorizationEvidence{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := s.byID[id]
	if len(history) == 0 {
		return WorkAuthorizationEvidence{}, false
	}
	return history[len(history)-1], true
}
