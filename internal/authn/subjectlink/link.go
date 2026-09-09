// Package subjectlink implements AUTHN-003: governed federation subject
// linking. It stores only an issuer reference, a digest of the federated
// subject identifier, and a tenant-scoped workforce identity reference.
//
// A link can be created only from a validated, digested linking decision and
// an issuer revision that is currently ACTIVE. Link lifecycle changes are
// append-only, digested events. Lookup returns WITHHELD without an explicit
// authorization scope so a caller cannot turn the link store into an
// existence oracle.
package subjectlink

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/sod"
)

// MatchingRule is the closed set of evidence paths that may establish a
// federated subject link.
type MatchingRule string

const (
	RuleVerifiedEmail      MatchingRule = "VERIFIED_EMAIL_MATCH"
	RuleHRProvidedExternal MatchingRule = "HR_PROVIDED_EXTERNAL_ID"
	RuleManualReview       MatchingRule = "MANUAL_REVIEW"

	// Descriptive aliases keep the vocabulary readable at call sites.
	MatchVerifiedEmail      = RuleVerifiedEmail
	MatchHRProvidedExternal = RuleHRProvidedExternal
	MatchManualReview       = RuleManualReview
)

func (r MatchingRule) valid() bool {
	switch r {
	case RuleVerifiedEmail, RuleHRProvidedExternal, RuleManualReview:
		return true
	default:
		return false
	}
}

// Lifecycle is the closed lifecycle of a subject link.
type Lifecycle string

const (
	LifecycleLinked    Lifecycle = "LINKED"
	LifecycleSuspended Lifecycle = "SUSPENDED"
	LifecycleUnlinked  Lifecycle = "UNLINKED"

	Linked    = LifecycleLinked
	Suspended = LifecycleSuspended
	Unlinked  = LifecycleUnlinked
)

func (s Lifecycle) valid() bool {
	return s == LifecycleLinked || s == LifecycleSuspended || s == LifecycleUnlinked
}

var (
	ErrInvalidLink       = errors.New("subjectlink: invalid subject link")
	ErrInvalidDecision   = errors.New("subjectlink: invalid linking decision")
	ErrInvalidEvent      = errors.New("subjectlink: invalid lifecycle event")
	ErrIssuerNotActive   = errors.New("subjectlink: issuer is not ACTIVE")
	ErrIssuerRevision    = errors.New("subjectlink: issuer reference is not the ACTIVE revision")
	ErrSubjectDigest     = errors.New("subjectlink: subject identifier must be a SHA-256 digest")
	ErrDuplicateSubject  = errors.New("subjectlink: subject already has an active link for this issuer")
	ErrDuplicateIdentity = errors.New("subjectlink: workforce identity already has an active link for this issuer")
	ErrLinkExists        = errors.New("subjectlink: link id already exists")
	ErrLinkNotFound      = errors.New("subjectlink: link not found")
	ErrInvalidTransition = errors.New("subjectlink: lifecycle transition is not permitted")
	ErrScopeRequired     = errors.New("subjectlink: lookup scope is required")
	ErrStore             = errors.New("subjectlink: store is required")
)

// WorkforceIdentityRef identifies an ACCESS-001 WorkforceIdentity without
// copying the identity's subject or any other access-domain attributes.
type WorkforceIdentityRef struct {
	Tenant values.TenantId
	ID     string
}

func (r WorkforceIdentityRef) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: workforce identity tenant: %v", ErrInvalidLink, err)
	}
	if strings.TrimSpace(r.ID) == "" || r.ID != strings.TrimSpace(r.ID) || strings.ContainsAny(r.ID, "\x00\r\n\t") {
		return fmt.Errorf("%w: workforce identity id is required and must be log-safe", ErrInvalidLink)
	}
	return nil
}

// Decision is the evidence presented to create a link. Digest is computed by
// [NewDecision] and must verify before the decision is accepted. EvidenceRef
// is an opaque reference to the source evidence; it is never a raw subject
// identifier.
type Decision struct {
	Rule        MatchingRule
	DecidedBy   string
	Approver    string
	Authority   string
	EvidenceRef string
	At          time.Time
	Digest      string
}

// LinkingDecision is the descriptive name for Decision.
type LinkingDecision = Decision

// NewDecision validates and digests one linking decision. Manual review uses
// the existing SOD evaluator to require a distinct approver.
func NewDecision(rule MatchingRule, decidedBy, approver, authority, evidenceRef string, at time.Time) (Decision, error) {
	d := Decision{Rule: rule, DecidedBy: decidedBy, Approver: approver, Authority: authority, EvidenceRef: evidenceRef, At: at.UTC()}
	if err := d.validateFields(); err != nil {
		return Decision{}, err
	}
	d.Digest = digestDecision(d)
	return d, nil
}

func (d Decision) validateFields() error {
	if !d.Rule.valid() {
		return fmt.Errorf("%w: matching rule %q is not in the closed set", ErrInvalidDecision, d.Rule)
	}
	if safeRequired(d.DecidedBy) == "" || safeRequired(d.EvidenceRef) == "" || d.At.IsZero() {
		return fmt.Errorf("%w: decision actor, evidence reference and time are required", ErrInvalidDecision)
	}
	if d.Rule == RuleManualReview {
		if safeRequired(d.Approver) == "" {
			return fmt.Errorf("%w: manual review requires an approver", ErrInvalidDecision)
		}
		_, err := sod.Evaluate(
			sod.DecisionContext{Requester: sod.Actor{Subject: d.DecidedBy}, Approvers: []sod.Actor{{Subject: d.Approver}}},
			sod.Constraints{RequesterMayNotApprove: true, RuleID: "AUTHN-003/manual-approver"}, 1,
		)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDecision, err)
		}
	} else if d.Approver != "" && d.Approver == d.DecidedBy {
		return fmt.Errorf("%w: decision actor and approver must be distinct", ErrInvalidDecision)
	}
	return nil
}

// Validate verifies the closed rule, required evidence and digest.
func (d Decision) Validate() error {
	if err := d.validateFields(); err != nil {
		return err
	}
	if d.Digest == "" || d.Digest != digestDecision(d) {
		return fmt.Errorf("%w: decision digest does not verify", ErrInvalidDecision)
	}
	return nil
}

// CreateRequest contains no raw federated subject identifier.
type CreateRequest struct {
	LinkID               string
	IssuerRef            issuerregistry.Ref
	SubjectDigest        string
	WorkforceIdentityRef WorkforceIdentityRef
	Decision             Decision
}

// Request is a descriptive alias for CreateRequest.
type Request = CreateRequest

// Link is the current projection of a governed subject link. SubjectDigest
// is the only subject value retained; raw federated identifiers are not part
// of this type, its events, or its explanations.
type Link struct {
	ID                   string
	IssuerRef            issuerregistry.Ref
	SubjectDigest        string
	WorkforceIdentityRef WorkforceIdentityRef
	Rule                 MatchingRule
	DecisionDigest       string
	Lifecycle            Lifecycle
	Revision             uint64
	CreatedAt            time.Time
	Digest               string
}

// LifecycleEvidence is the evidence for a lifecycle transition. It contains
// no subject identifier.
type LifecycleEvidence struct {
	ActedBy   string
	Authority string
	Reason    string
	At        time.Time
}

func (e LifecycleEvidence) validate() error {
	if safeRequired(e.ActedBy) == "" || e.At.IsZero() {
		return fmt.Errorf("%w: lifecycle actor and time are required", ErrInvalidEvent)
	}
	return nil
}

// LifecycleEvent is immutable append-only evidence for one lifecycle change.
type LifecycleEvent struct {
	LinkID               string
	IssuerRef            issuerregistry.Ref
	SubjectDigest        string
	WorkforceIdentityRef WorkforceIdentityRef
	From                 Lifecycle
	To                   Lifecycle
	Rule                 MatchingRule
	DecisionDigest       string
	ActedBy              string
	Authority            string
	Reason               string
	At                   time.Time
	PreviousDigest       string
	Digest               string
}

// Store is the persistence port for governed links. MemoryStore is the
// package's kernel-pure adapter; a database adapter can implement this port
// without changing the linking contract.
type Store interface {
	Create(Link, LifecycleEvent) error
	Get(string) (Link, bool, error)
	Find(issuerregistry.Ref, string) (Link, bool, error)
	Transition(Link, LifecycleEvent) error
	Events(string) ([]LifecycleEvent, error)
}

// MemoryStore is an atomic in-memory store. Its indexes contain only active
// links, so an UNLINKED or SUSPENDED link does not block a later fresh link.
type MemoryStore struct {
	mu             sync.RWMutex
	links          map[string]Link
	events         map[string][]LifecycleEvent
	bySubject      map[string]string
	activeSubjects map[string]string
	activeIdent    map[string]string
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		links:          make(map[string]Link),
		events:         make(map[string][]LifecycleEvent),
		bySubject:      make(map[string]string),
		activeSubjects: make(map[string]string),
		activeIdent:    make(map[string]string),
	}
}

var _ Store = (*MemoryStore)(nil)

// Service composes an issuer registry with a subject-link store.
type Service struct {
	Store   Store
	Issuers issuerregistry.Store
}

// New returns a governed subject-link service.
func New(store Store, issuers issuerregistry.Store) *Service {
	return &Service{Store: store, Issuers: issuers}
}

// Create creates a LINKED record after confirming the exact issuer revision
// is currently ACTIVE and the linking decision is valid.
func (s *Service) Create(req CreateRequest) (Link, LifecycleEvent, error) {
	if s == nil || s.Store == nil || s.Issuers == nil {
		return Link{}, LifecycleEvent{}, ErrStore
	}
	if err := validateCreateRequest(req); err != nil {
		return Link{}, LifecycleEvent{}, err
	}
	active, err := issuerregistry.Lookup(s.Issuers, req.IssuerRef.Tenant, req.IssuerRef.IssuerURL)
	if err != nil {
		return Link{}, LifecycleEvent{}, fmt.Errorf("%w: %w", ErrIssuerNotActive, err)
	}
	if active.Ref() != req.IssuerRef {
		return Link{}, LifecycleEvent{}, ErrIssuerRevision
	}
	link := Link{
		ID: req.LinkID, IssuerRef: req.IssuerRef, SubjectDigest: req.SubjectDigest,
		WorkforceIdentityRef: req.WorkforceIdentityRef, Rule: req.Decision.Rule,
		DecisionDigest: req.Decision.Digest, Lifecycle: LifecycleLinked, Revision: 1,
		CreatedAt: req.Decision.At.UTC(),
	}
	event := newEvent(link, LifecycleEvidence{ActedBy: req.Decision.DecidedBy, Authority: req.Decision.Authority, Reason: "link created", At: req.Decision.At}, "", LifecycleLinked)
	event.From = ""
	event.Digest = digestEvent(event)
	link.Digest = event.Digest
	if err := s.Store.Create(link, event); err != nil {
		return Link{}, LifecycleEvent{}, err
	}
	return cloneLink(link), cloneEvent(event), nil
}

// Create is the package-level command form for callers that do not need a
// retained Service value.
func Create(store Store, issuers issuerregistry.Store, req CreateRequest) (Link, LifecycleEvent, error) {
	return New(store, issuers).Create(req)
}

// LookupStatus describes a scoped lookup result. WITHHELD and NOT_FOUND are
// deliberately distinct only to callers that hold the required scope.
type LookupStatus string

const (
	LookupFound    LookupStatus = "FOUND"
	LookupWithheld LookupStatus = "WITHHELD"
	LookupNotFound LookupStatus = "NOT_FOUND"
)

// LookupScope is the explicit authorization scope needed to learn whether a
// link exists. Granted=false always produces WITHHELD.
type LookupScope struct {
	Tenant  values.TenantId
	Purpose string
	Granted bool
}

// Scope is a short alias for LookupScope.
type Scope = LookupScope

type LookupRequest struct {
	IssuerRef     issuerregistry.Ref
	SubjectDigest string
	Scope         LookupScope
}

type LookupResult struct {
	Status LookupStatus
	Link   Link
}

// Lookup answers WITHHELD without scope, regardless of whether a matching
// record exists. An authorized caller may distinguish FOUND from NOT_FOUND.
func (s *Service) Lookup(req LookupRequest) (LookupResult, error) {
	if s == nil || s.Store == nil {
		return LookupResult{}, ErrStore
	}
	if err := validateIssuerRef(req.IssuerRef); err != nil {
		return LookupResult{}, err
	}
	if err := validateSubjectDigest(req.SubjectDigest); err != nil {
		return LookupResult{}, err
	}
	if !req.Scope.Granted {
		return LookupResult{Status: LookupWithheld}, nil
	}
	if req.Scope.Tenant != req.IssuerRef.Tenant || strings.TrimSpace(req.Scope.Purpose) == "" {
		return LookupResult{}, ErrScopeRequired
	}
	link, found, err := s.Store.Find(req.IssuerRef, req.SubjectDigest)
	if err != nil {
		return LookupResult{}, err
	}
	if !found {
		return LookupResult{Status: LookupNotFound}, nil
	}
	return LookupResult{Status: LookupFound, Link: cloneLink(link)}, nil
}

// Lookup is the package-level query form.
func Lookup(store Store, req LookupRequest) (LookupResult, error) {
	return (&Service{Store: store}).Lookup(req)
}

// Suspend appends a SUSPENDED event.
func (s *Service) Suspend(linkID string, evidence LifecycleEvidence) (Link, LifecycleEvent, error) {
	return s.transition(linkID, LifecycleSuspended, evidence)
}

// Resume appends a LINKED event for a suspended link after rechecking active
// uniqueness. It is the only lifecycle operation that reactivates a link.
func (s *Service) Resume(linkID string, evidence LifecycleEvidence) (Link, LifecycleEvent, error) {
	return s.transition(linkID, LifecycleLinked, evidence)
}

// Unlink appends an UNLINKED event.
func (s *Service) Unlink(linkID string, evidence LifecycleEvidence) (Link, LifecycleEvent, error) {
	return s.transition(linkID, LifecycleUnlinked, evidence)
}

func (s *Service) transition(linkID string, to Lifecycle, evidence LifecycleEvidence) (Link, LifecycleEvent, error) {
	if s == nil || s.Store == nil {
		return Link{}, LifecycleEvent{}, ErrStore
	}
	if err := evidence.validate(); err != nil {
		return Link{}, LifecycleEvent{}, err
	}
	link, found, err := s.Store.Get(linkID)
	if err != nil {
		return Link{}, LifecycleEvent{}, err
	}
	if !found {
		return Link{}, LifecycleEvent{}, ErrLinkNotFound
	}
	if !allowedTransition(link.Lifecycle, to) {
		return Link{}, LifecycleEvent{}, fmt.Errorf("%w: %s to %s", ErrInvalidTransition, link.Lifecycle, to)
	}
	updated := cloneLink(link)
	updated.Lifecycle = to
	updated.Revision++
	event := newEvent(link, evidence, link.Digest, to)
	updated.Digest = event.Digest
	if err := s.Store.Transition(updated, event); err != nil {
		return Link{}, LifecycleEvent{}, err
	}
	return cloneLink(updated), cloneEvent(event), nil
}

func validateCreateRequest(req CreateRequest) error {
	if safeRequired(req.LinkID) == "" {
		return fmt.Errorf("%w: link id is required", ErrInvalidLink)
	}
	if err := validateIssuerRef(req.IssuerRef); err != nil {
		return err
	}
	if err := validateSubjectDigest(req.SubjectDigest); err != nil {
		return err
	}
	if err := req.WorkforceIdentityRef.Validate(); err != nil {
		return err
	}
	if req.WorkforceIdentityRef.Tenant != req.IssuerRef.Tenant {
		return fmt.Errorf("%w: workforce identity crosses tenant boundary", ErrInvalidLink)
	}
	return req.Decision.Validate()
}

func validateIssuerRef(ref issuerregistry.Ref) error {
	if err := ref.Tenant.Validate(); err != nil || strings.TrimSpace(ref.IssuerURL) == "" || ref.Revision == 0 {
		return fmt.Errorf("%w: issuer reference is incomplete", ErrInvalidLink)
	}
	if _, err := url.ParseRequestURI(ref.IssuerURL); err != nil {
		return fmt.Errorf("%w: issuer reference URL is invalid", ErrInvalidLink)
	}
	return nil
}

func validateSubjectDigest(digest string) error {
	if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return ErrSubjectDigest
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return ErrSubjectDigest
	}
	return nil
}

func safeRequired(value string) string {
	if value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n\t") {
		return ""
	}
	return strings.TrimSpace(value)
}

func allowedTransition(from, to Lifecycle) bool {
	switch {
	case from == LifecycleLinked && (to == LifecycleSuspended || to == LifecycleUnlinked):
		return true
	case from == LifecycleSuspended && (to == LifecycleLinked || to == LifecycleUnlinked):
		return true
	default:
		return false
	}
}

func subjectKey(ref issuerregistry.Ref, digest string) string {
	return ref.Tenant.String() + "\x00" + ref.IssuerURL + "\x00" + digest
}

func identityKey(ref issuerregistry.Ref, identity WorkforceIdentityRef) string {
	return ref.Tenant.String() + "\x00" + ref.IssuerURL + "\x00" + identity.ID
}

func digestDecision(d Decision) string {
	return digestParts("decision", string(d.Rule), d.DecidedBy, d.Approver, d.Authority, d.EvidenceRef, d.At.UTC().Format(time.RFC3339Nano))
}

func digestEvent(e LifecycleEvent) string {
	return digestParts("event", e.LinkID, e.IssuerRef.Tenant.String(), e.IssuerRef.IssuerURL, fmt.Sprint(e.IssuerRef.Revision), e.SubjectDigest, e.WorkforceIdentityRef.Tenant.String(), e.WorkforceIdentityRef.ID, string(e.From), string(e.To), string(e.Rule), e.DecisionDigest, e.ActedBy, e.Authority, e.Reason, e.At.UTC().Format(time.RFC3339Nano), e.PreviousDigest)
}

func digestParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%d:", len(part))
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validateDigest(digest string) error {
	if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return ErrInvalidLink
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return ErrInvalidLink
	}
	return nil
}

func newEvent(link Link, evidence LifecycleEvidence, previous string, to Lifecycle) LifecycleEvent {
	e := LifecycleEvent{
		LinkID: link.ID, IssuerRef: link.IssuerRef, SubjectDigest: link.SubjectDigest,
		WorkforceIdentityRef: link.WorkforceIdentityRef, From: link.Lifecycle, To: to,
		Rule: link.Rule, DecisionDigest: link.DecisionDigest, ActedBy: evidence.ActedBy,
		Authority: evidence.Authority, Reason: evidence.Reason, At: evidence.At.UTC(), PreviousDigest: previous,
	}
	e.Digest = digestEvent(e)
	return e
}

func (e LifecycleEvent) Validate() error {
	if safeRequired(e.LinkID) == "" || validateIssuerRef(e.IssuerRef) != nil || !e.To.valid() || (e.From != "" && !e.From.valid()) || e.From == e.To || e.At.IsZero() || validateSubjectDigest(e.SubjectDigest) != nil || e.WorkforceIdentityRef.Validate() != nil || e.WorkforceIdentityRef.Tenant != e.IssuerRef.Tenant || !e.Rule.valid() || e.DecisionDigest == "" || safeRequired(e.ActedBy) == "" || e.Digest == "" || e.Digest != digestEvent(e) {
		return ErrInvalidEvent
	}
	return nil
}

func (l Link) Validate() error {
	if safeRequired(l.ID) == "" || validateIssuerRef(l.IssuerRef) != nil || validateSubjectDigest(l.SubjectDigest) != nil || l.WorkforceIdentityRef.Validate() != nil || l.WorkforceIdentityRef.Tenant != l.IssuerRef.Tenant || !l.Rule.valid() || l.DecisionDigest == "" || !l.Lifecycle.valid() || l.Revision == 0 || l.CreatedAt.IsZero() || validateDigest(l.Digest) != nil {
		return ErrInvalidLink
	}
	return nil
}

func (m *MemoryStore) Create(link Link, event LifecycleEvent) error {
	if m == nil {
		return ErrStore
	}
	if err := link.Validate(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil || event.From != "" || event.To != LifecycleLinked || event.LinkID != link.ID || event.IssuerRef != link.IssuerRef || event.SubjectDigest != link.SubjectDigest || event.WorkforceIdentityRef != link.WorkforceIdentityRef || event.Rule != link.Rule || event.DecisionDigest != link.DecisionDigest || event.Digest != link.Digest {
		return ErrInvalidEvent
	}
	if link.Lifecycle != LifecycleLinked || link.Revision != 1 {
		return ErrInvalidLink
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.links == nil {
		m.links = make(map[string]Link)
		m.events = make(map[string][]LifecycleEvent)
		m.bySubject = make(map[string]string)
		m.activeSubjects = make(map[string]string)
		m.activeIdent = make(map[string]string)
	}
	if _, exists := m.links[link.ID]; exists {
		return ErrLinkExists
	}
	if _, exists := m.activeSubjects[subjectKey(link.IssuerRef, link.SubjectDigest)]; exists {
		return ErrDuplicateSubject
	}
	if _, exists := m.activeIdent[identityKey(link.IssuerRef, link.WorkforceIdentityRef)]; exists {
		return ErrDuplicateIdentity
	}
	m.links[link.ID] = cloneLink(link)
	m.events[link.ID] = []LifecycleEvent{cloneEvent(event)}
	m.bySubject[subjectKey(link.IssuerRef, link.SubjectDigest)] = link.ID
	m.activeSubjects[subjectKey(link.IssuerRef, link.SubjectDigest)] = link.ID
	m.activeIdent[identityKey(link.IssuerRef, link.WorkforceIdentityRef)] = link.ID
	return nil
}

func (m *MemoryStore) Get(id string) (Link, bool, error) {
	if m == nil {
		return Link{}, false, ErrStore
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	link, ok := m.links[id]
	return cloneLink(link), ok, nil
}

func (m *MemoryStore) Find(ref issuerregistry.Ref, subjectDigest string) (Link, bool, error) {
	if m == nil {
		return Link{}, false, ErrStore
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.bySubject[subjectKey(ref, subjectDigest)]
	if !ok {
		return Link{}, false, nil
	}
	link, ok := m.links[id]
	return cloneLink(link), ok, nil
}

func (m *MemoryStore) Transition(updated Link, event LifecycleEvent) error {
	if m == nil {
		return ErrStore
	}
	if err := updated.Validate(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil || event.LinkID != updated.ID || event.To != updated.Lifecycle || event.Digest != updated.Digest {
		return ErrInvalidEvent
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.links[updated.ID]
	if !ok {
		return ErrLinkNotFound
	}
	if current.Digest != event.PreviousDigest || current.Revision+1 != updated.Revision || current.Lifecycle != event.From {
		return ErrInvalidEvent
	}
	if current.IssuerRef != updated.IssuerRef || current.SubjectDigest != updated.SubjectDigest || current.WorkforceIdentityRef != updated.WorkforceIdentityRef || current.Rule != updated.Rule || current.DecisionDigest != updated.DecisionDigest || event.IssuerRef != updated.IssuerRef || event.SubjectDigest != updated.SubjectDigest || event.WorkforceIdentityRef != updated.WorkforceIdentityRef || event.Rule != updated.Rule || event.DecisionDigest != updated.DecisionDigest {
		return ErrInvalidEvent
	}
	if updated.Lifecycle == LifecycleLinked {
		if owner, exists := m.activeSubjects[subjectKey(updated.IssuerRef, updated.SubjectDigest)]; exists && owner != updated.ID {
			return ErrDuplicateSubject
		}
		if owner, exists := m.activeIdent[identityKey(updated.IssuerRef, updated.WorkforceIdentityRef)]; exists && owner != updated.ID {
			return ErrDuplicateIdentity
		}
	}
	if current.Lifecycle == LifecycleLinked {
		delete(m.activeSubjects, subjectKey(current.IssuerRef, current.SubjectDigest))
		delete(m.activeIdent, identityKey(current.IssuerRef, current.WorkforceIdentityRef))
	}
	if updated.Lifecycle == LifecycleLinked {
		m.activeSubjects[subjectKey(updated.IssuerRef, updated.SubjectDigest)] = updated.ID
		m.activeIdent[identityKey(updated.IssuerRef, updated.WorkforceIdentityRef)] = updated.ID
	}
	m.links[updated.ID] = cloneLink(updated)
	m.events[updated.ID] = append(m.events[updated.ID], cloneEvent(event))
	return nil
}

func (m *MemoryStore) Events(id string) ([]LifecycleEvent, error) {
	if m == nil {
		return nil, ErrStore
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	events, ok := m.events[id]
	if !ok {
		return nil, ErrLinkNotFound
	}
	out := make([]LifecycleEvent, len(events))
	for i, event := range events {
		out[i] = cloneEvent(event)
	}
	return out, nil
}

func cloneLink(l Link) Link { return l }

func cloneEvent(e LifecycleEvent) LifecycleEvent { return e }

// DigestSubject returns a SHA-256 digest suitable for SubjectDigest. The raw
// identifier is consumed only to derive the digest and is never returned or
// retained by this package.
func DigestSubject(identifier string) string {
	sum := sha256.Sum256([]byte(identifier))
	return hex.EncodeToString(sum[:])
}

// DigestSubjectForIssuer derives a namespaced subject digest. Namespacing
// makes accidental reuse of a digest across issuer domains less likely while
// the issuer reference remains an independent part of every link key.
func DigestSubjectForIssuer(ref issuerregistry.Ref, identifier string) string {
	return digestParts("subject", ref.Tenant.String(), ref.IssuerURL, fmt.Sprint(ref.Revision), identifier)
}

// Explain returns a redaction-safe description of the contract. It carries no
// issuer subject identifier, subject digest, workforce identity, or evidence
// value.
func Explain() string {
	return "Federated subject links require an ACTIVE issuer revision, a closed matching rule, digested evidence, tenant-scoped workforce identity, distinct manual approval, append-only lifecycle events, and scoped lookup with WITHHELD responses."
}

// Explain returns a redaction-safe description of this link's lifecycle and
// rule without carrying the federated subject identifier or its digest.
func (l Link) Explain() string {
	return fmt.Sprintf("subject link lifecycle=%s rule=%s revision=%d", l.Lifecycle, l.Rule, l.Revision)
}

// Explain returns only the lookup disposition.
func (r LookupResult) Explain() string { return fmt.Sprintf("subject link lookup=%s", r.Status) }
