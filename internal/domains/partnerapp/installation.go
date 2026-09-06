package partnerapp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidInstallation = errors.New("partnerapp: invalid installation")
	ErrInvalidGrant        = errors.New("partnerapp: invalid installation grant")
	ErrGrantTransition     = errors.New("partnerapp: invalid installation grant transition")
	ErrGrantApproval       = errors.New("partnerapp: installation grant needs distinct approval")
	ErrInstallationReview  = errors.New("partnerapp: installation requires its application review")
	ErrReviewScope         = errors.New("partnerapp: installation exceeds reviewed scope")
	ErrInstallationEvent   = errors.New("partnerapp: invalid installation event")
	ErrInstallationStore   = errors.New("partnerapp: invalid installation store append")
	ErrInstallationScope   = errors.New("partnerapp: invalid installation scope")
)

// GrantState is the append-only lifecycle of an installation grant. A
// REQUESTED revision is not an approval and cannot be used as an ACTIVE
// authorization.
type GrantState string

const (
	GrantRequested GrantState = "REQUESTED"
	GrantApproved  GrantState = "APPROVED"
	GrantActive    GrantState = "ACTIVE"
	GrantSuspended GrantState = "SUSPENDED"
	GrantRevoked   GrantState = "REVOKED"
)

// InstallationState is a descriptive alias for callers that use the
// installation vocabulary rather than the grant vocabulary.
type InstallationState = GrantState

const (
	InstallationStateRequested = GrantRequested
	InstallationStateApproved  = GrantApproved
	InstallationStateActive    = GrantActive
	InstallationStateSuspended = GrantSuspended
	InstallationStateRevoked   = GrantRevoked
)

func (s GrantState) valid() bool {
	switch s {
	case GrantRequested, GrantApproved, GrantActive, GrantSuspended, GrantRevoked:
		return true
	default:
		return false
	}
}

// ApplicationVersionBinding freezes the exact application revision an
// installation is allowed to use. A semantic version without its revision
// and digest is not an acceptable binding.
type ApplicationVersionBinding struct {
	ApplicationID string
	Version       string
	Revision      uint64
	Digest        string
}

// InstallationRequest is the explicit scope of a requested installation.
// Empty values are not wildcards; all dimensions are required and exact.
type InstallationRequest struct {
	InstallationID    string
	TenantScope       string
	OrganizationScope string
	PopulationScope   string
	DataClasses       []string
	FieldScopes       []string
	Purpose           string
	Capabilities      []string
	ReviewRef         string
	Requester         string
}

// ApproverEvidence identifies the distinct human or service authority that
// approved an installation grant. Evidence is reference-only.
type ApproverEvidence struct {
	Requester   string
	Approver    string
	EvidenceRef string
	ReviewRef   string
}

// Installation is one immutable installation revision. Lifecycle methods
// return a successor and a digested event; the receiver is never changed.
type Installation struct {
	InstallationID string
	Revision       uint64
	State          GrantState

	VersionBinding    ApplicationVersionBinding
	TenantScope       string
	OrganizationScope string
	PopulationScope   string
	DataClasses       []string
	FieldScopes       []string
	Purpose           string
	Capabilities      []string

	Requester        string
	ApproverEvidence ApproverEvidence
	ReviewRef        string
	Digest           string
}

// NewInstallation creates the first REQUESTED revision. The application
// version must already be ACTIVE; this prevents an installation from binding
// to an unreviewed or merely drafted application version.
func NewInstallation(version ApplicationVersion, req InstallationRequest) (Installation, error) {
	installation, _, err := RequestGrant(version, req)
	return installation, err
}

// RequestGrant creates a REQUESTED installation and its immutable event.
func RequestGrant(version ApplicationVersion, req InstallationRequest) (Installation, InstallationEvent, error) {
	if err := version.Verify(); err != nil {
		return Installation{}, InstallationEvent{}, fmt.Errorf("%w: application version: %v", ErrInstallationReview, err)
	}
	if version.State != StateActive {
		return Installation{}, InstallationEvent{}, fmt.Errorf("%w: application version must be ACTIVE", ErrInstallationReview)
	}
	if err := validateInstallationRequest(req, version); err != nil {
		return Installation{}, InstallationEvent{}, err
	}
	i := Installation{
		InstallationID: req.InstallationID,
		Revision:       1,
		State:          GrantRequested,
		VersionBinding: ApplicationVersionBinding{ApplicationID: version.ApplicationID, Version: version.Version, Revision: version.Revision, Digest: version.Digest},
		TenantScope:    req.TenantScope, OrganizationScope: req.OrganizationScope, PopulationScope: req.PopulationScope,
		DataClasses: sortedCopy(req.DataClasses), FieldScopes: sortedCopy(req.FieldScopes), Purpose: req.Purpose,
		Capabilities: sortedCopy(req.Capabilities), Requester: req.Requester, ReviewRef: req.ReviewRef,
	}
	i.Digest = installationDigest(i)
	event := installationEvent(i, "", "", i.Requester, "")
	return i, event, nil
}

// NewRequestedGrant is a descriptive alias for RequestGrant.
func NewRequestedGrant(version ApplicationVersion, req InstallationRequest) (Installation, InstallationEvent, error) {
	return RequestGrant(version, req)
}

// Validate checks an installation revision without requiring the application
// registry. Verify additionally checks the immutable digest.
func (i Installation) Validate() error {
	if strings.TrimSpace(i.InstallationID) == "" || strings.TrimSpace(i.InstallationID) != i.InstallationID {
		return fmt.Errorf("%w: installation id is required", ErrInvalidInstallation)
	}
	if i.Revision == 0 || !i.State.valid() {
		return fmt.Errorf("%w: revision and state are required", ErrInvalidInstallation)
	}
	if err := i.VersionBinding.validate(); err != nil {
		return err
	}
	if err := validateExactSet([]string{i.TenantScope}, "tenant scope", ErrInvalidInstallation); err != nil {
		return err
	}
	if err := validateExactSet([]string{i.OrganizationScope}, "organization scope", ErrInvalidInstallation); err != nil {
		return err
	}
	if err := validateExactSet([]string{i.PopulationScope}, "population scope", ErrInvalidInstallation); err != nil {
		return err
	}
	if err := validateExactSet(i.DataClasses, "data class", ErrInvalidInstallation); err != nil {
		return err
	}
	if err := validateExactSet(i.FieldScopes, "field scope", ErrInvalidInstallation); err != nil {
		return err
	}
	if err := validateExactSet([]string{i.Purpose}, "purpose", ErrInvalidInstallation); err != nil {
		return err
	}
	if err := validateExactSet(i.Capabilities, "capability", ErrInvalidInstallation); err != nil {
		return err
	}
	if strings.TrimSpace(i.Requester) == "" || strings.TrimSpace(i.Requester) != i.Requester {
		return fmt.Errorf("%w: requester is required", ErrInvalidInstallation)
	}
	if i.ReviewRef != "" && !exactRef(i.ReviewRef) {
		return fmt.Errorf("%w: review reference is not exact", ErrInvalidInstallation)
	}
	if i.ApproverEvidence.Approver != "" && (strings.TrimSpace(i.ApproverEvidence.Approver) != i.ApproverEvidence.Approver || i.ApproverEvidence.Approver == i.Requester) {
		return ErrGrantApproval
	}
	if i.ApproverEvidence.EvidenceRef != "" && !exactRef(i.ApproverEvidence.EvidenceRef) {
		return fmt.Errorf("%w: approver evidence reference is not exact", ErrGrantApproval)
	}
	if i.State == GrantApproved || i.State == GrantActive || i.State == GrantSuspended || i.State == GrantRevoked {
		if i.ApproverEvidence.Approver == "" || i.ApproverEvidence.EvidenceRef == "" {
			return ErrGrantApproval
		}
	}
	if i.State == GrantActive && i.ReviewRef == "" {
		return ErrInstallationReview
	}
	return nil
}

// Verify detects mutation after an installation revision was minted.
func (i Installation) Verify() error {
	if err := i.Validate(); err != nil {
		return err
	}
	if i.Digest == "" || installationDigest(i) != i.Digest {
		return fmt.Errorf("%w: installation digest does not match revision %d", ErrInvalidInstallation, i.Revision)
	}
	return nil
}

// Approve turns REQUESTED into APPROVED. It does not activate the grant and
// does not infer that the reviewed data-processing scope has been accepted.
func (i Installation) Approve(approver, evidenceRef string) (Installation, InstallationEvent, error) {
	if err := i.Verify(); err != nil {
		return Installation{}, InstallationEvent{}, err
	}
	if i.State != GrantRequested {
		return Installation{}, InstallationEvent{}, fmt.Errorf("%w: only REQUESTED grants can be approved", ErrGrantTransition)
	}
	if strings.TrimSpace(approver) == "" || strings.TrimSpace(approver) != approver || approver == i.Requester {
		return Installation{}, InstallationEvent{}, ErrGrantApproval
	}
	if !exactRef(evidenceRef) {
		return Installation{}, InstallationEvent{}, fmt.Errorf("%w: approval evidence reference is required", ErrGrantApproval)
	}
	next := i.next(GrantApproved)
	next.ApproverEvidence = ApproverEvidence{Requester: i.Requester, Approver: approver, EvidenceRef: evidenceRef, ReviewRef: i.ReviewRef}
	next.Digest = installationDigest(next)
	return next, installationEvent(next, i.Digest, GrantRequested, approver, evidenceRef), nil
}

// ActivateWithReview is the only path from APPROVED to ACTIVE. It verifies
// the exact APP-002 review, its controls, validity window and admitted scope.
func (i Installation) ActivateWithReview(review DataProcessingReview, now time.Time) (Installation, InstallationEvent, error) {
	if err := i.Verify(); err != nil {
		return Installation{}, InstallationEvent{}, err
	}
	if i.State != GrantApproved {
		return Installation{}, InstallationEvent{}, fmt.Errorf("%w: only APPROVED grants can activate", ErrGrantTransition)
	}
	if err := validateInstallationReview(i, review, now); err != nil {
		return Installation{}, InstallationEvent{}, err
	}
	reviewRef := i.ReviewRef
	if reviewRef == "" {
		reviewRef = review.Digest
	}
	next := i.next(GrantActive)
	next.ReviewRef = reviewRef
	next.ApproverEvidence.ReviewRef = reviewRef
	next.Digest = installationDigest(next)
	return next, installationEvent(next, i.Digest, GrantApproved, i.ApproverEvidence.Approver, i.ApproverEvidence.EvidenceRef), nil
}

// Activate is the concise method form of ActivateWithReview.
func (i Installation) Activate(review DataProcessingReview, now time.Time) (Installation, InstallationEvent, error) {
	return i.ActivateWithReview(review, now)
}

// Suspend creates a SUSPENDED revision. Suspension never deletes evidence.
func (i Installation) Suspend(requester, evidenceRef string) (Installation, InstallationEvent, error) {
	return i.operatorTransition(GrantSuspended, requester, evidenceRef)
}

// Revoke creates a terminal REVOKED revision. Revocation never deletes prior
// revisions or their approval evidence.
func (i Installation) Revoke(requester, evidenceRef string) (Installation, InstallationEvent, error) {
	return i.operatorTransition(GrantRevoked, requester, evidenceRef)
}

func (i Installation) operatorTransition(state GrantState, requester, evidenceRef string) (Installation, InstallationEvent, error) {
	if err := i.Verify(); err != nil {
		return Installation{}, InstallationEvent{}, err
	}
	if (state == GrantSuspended && i.State != GrantActive) || (state == GrantRevoked && i.State != GrantActive && i.State != GrantSuspended) {
		return Installation{}, InstallationEvent{}, fmt.Errorf("%w: %s cannot transition to %s", ErrGrantTransition, i.State, state)
	}
	if !exactRef(requester) || !exactRef(evidenceRef) {
		return Installation{}, InstallationEvent{}, fmt.Errorf("%w: operator and evidence reference are required", ErrGrantApproval)
	}
	next := i.next(state)
	next.Digest = installationDigest(next)
	return next, installationEvent(next, i.Digest, i.State, requester, evidenceRef), nil
}

func (i Installation) next(state GrantState) Installation {
	next := i
	next.Revision++
	next.State = state
	next.DataClasses = sortedCopy(i.DataClasses)
	next.FieldScopes = sortedCopy(i.FieldScopes)
	next.Capabilities = sortedCopy(i.Capabilities)
	next.Digest = ""
	return next
}

// InstallationEvent is the digest-addressed lifecycle evidence for one
// installation revision. It contains references and counts only; payloads
// and data values never enter the event.
type InstallationEvent struct {
	InstallationID     string
	Revision           uint64
	PreviousDigest     string
	PreviousState      GrantState
	State              GrantState
	Actor              string
	EvidenceRef        string
	ReviewRef          string
	InstallationDigest string
	Digest             string
}

func installationEvent(i Installation, previousDigest string, previousState GrantState, actor, evidenceRef string) InstallationEvent {
	e := InstallationEvent{InstallationID: i.InstallationID, Revision: i.Revision, PreviousDigest: previousDigest, PreviousState: previousState, State: i.State, Actor: actor, EvidenceRef: evidenceRef, ReviewRef: i.ReviewRef, InstallationDigest: i.Digest}
	e.Digest = eventDigest(e)
	return e
}

// Verify checks the event's shape and digest.
func (e InstallationEvent) Verify() error {
	if strings.TrimSpace(e.InstallationID) == "" || e.Revision == 0 || !e.State.valid() || !exactRef(e.Actor) || e.InstallationDigest == "" || e.Digest == "" {
		return ErrInstallationEvent
	}
	if e.Revision == 1 && e.PreviousDigest != "" {
		return ErrInstallationEvent
	}
	if e.Digest != eventDigest(e) {
		return ErrInstallationEvent
	}
	return nil
}

// InstallationStore is a concurrent in-memory append-only adapter. It is
// intentionally not durable authority.
type InstallationStore struct {
	mu        sync.RWMutex
	revisions map[string][]Installation
	events    map[string][]InstallationEvent
}

// NewInstallationStore creates an empty store.
func NewInstallationStore() *InstallationStore {
	return &InstallationStore{revisions: make(map[string][]Installation), events: make(map[string][]InstallationEvent)}
}

// NewStore is a concise alias for NewInstallationStore.
func NewStore() *InstallationStore { return NewInstallationStore() }

// Append records one revision and its event without replacing history. The
// variadic form permits a design adapter to append a generated event for a
// trusted first revision, while normal callers provide the event explicitly.
func (s *InstallationStore) Append(i Installation, supplied ...InstallationEvent) error {
	if s == nil {
		return ErrInstallationStore
	}
	if err := i.Verify(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.revisions[i.InstallationID]
	if len(history) == 0 && i.Revision != 1 {
		return fmt.Errorf("%w: first revision must be 1", ErrInstallationStore)
	}
	if len(history) > 0 {
		previous := history[len(history)-1]
		if i.Revision != previous.Revision+1 {
			return fmt.Errorf("%w: revisions must append in order", ErrInstallationStore)
		}
	}
	var event InstallationEvent
	if len(supplied) > 1 {
		return fmt.Errorf("%w: one event is required", ErrInstallationStore)
	}
	if len(supplied) == 1 {
		event = supplied[0]
	} else {
		previousDigest, previousState := "", GrantState("")
		if len(history) > 0 {
			previousDigest, previousState = history[len(history)-1].Digest, history[len(history)-1].State
		}
		event = installationEvent(i, previousDigest, previousState, i.Requester, i.ApproverEvidence.EvidenceRef)
	}
	if err := event.Verify(); err != nil || event.InstallationID != i.InstallationID || event.Revision != i.Revision || event.State != i.State || event.InstallationDigest != i.Digest {
		return fmt.Errorf("%w: event does not bind revision", ErrInstallationStore)
	}
	if len(history) > 0 && event.PreviousDigest != history[len(history)-1].Digest {
		return fmt.Errorf("%w: event previous digest does not bind history", ErrInstallationStore)
	}
	s.revisions[i.InstallationID] = append(history, cloneInstallation(i))
	s.events[i.InstallationID] = append(s.events[i.InstallationID], event)
	return nil
}

// Register is an alias for Append.
func (s *InstallationStore) Register(i Installation, event ...InstallationEvent) error {
	return s.Append(i, event...)
}

// Revisions returns a copy of all immutable revisions for an installation.
func (s *InstallationStore) Revisions(id string) []Installation {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := s.revisions[id]
	out := make([]Installation, len(history))
	for n := range history {
		out[n] = cloneInstallation(history[n])
	}
	return out
}

// Events returns a copy of the digested lifecycle evidence.
func (s *InstallationStore) Events(id string) []InstallationEvent {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]InstallationEvent(nil), s.events[id]...)
}

// Current returns the latest revision, if any.
func (s *InstallationStore) Current(id string) (Installation, bool) {
	history := s.Revisions(id)
	if len(history) == 0 {
		return Installation{}, false
	}
	return history[len(history)-1], true
}

// ScopeQuery is the exact scope requested for one use of an installation.
type ScopeQuery struct {
	TenantScope       string
	OrganizationScope string
	PopulationScope   string
	DataClasses       []string
	FieldScopes       []string
	Purpose           string
	Capabilities      []string
}

// ScopeDecision is an allow/deny answer bound to the installation digest.
// Denials use fixed reason tokens and never echo the requested scope.
type ScopeDecision struct {
	Allowed              bool
	Rule                 string
	Reason               string
	InstallationID       string
	InstallationDigest   string
	InstallationRevision uint64
	QueryDigest          string
	Digest               string
}

// CheckScope evaluates a query against one installation revision.
func CheckScope(i Installation, q ScopeQuery) (ScopeDecision, error) {
	if err := i.Verify(); err != nil {
		return ScopeDecision{}, err
	}
	if err := q.validate(); err != nil {
		return ScopeDecision{}, err
	}
	d := ScopeDecision{InstallationID: i.InstallationID, InstallationDigest: i.Digest, InstallationRevision: i.Revision, QueryDigest: scopeQueryDigest(q), Rule: "partnerapp.installation.scope.exact"}
	deny := func(reason string) (ScopeDecision, error) {
		d.Allowed, d.Reason = false, reason
		d.Digest = scopeDecisionDigest(d)
		return d, nil
	}
	if i.State != GrantActive {
		return deny("installation_not_active")
	}
	if q.TenantScope != i.TenantScope {
		return deny("tenant_scope_not_granted")
	}
	if q.OrganizationScope != i.OrganizationScope {
		return deny("organization_scope_not_granted")
	}
	if q.PopulationScope != i.PopulationScope {
		return deny("population_scope_not_granted")
	}
	if q.Purpose != i.Purpose {
		return deny("purpose_not_granted")
	}
	if !subset(q.DataClasses, i.DataClasses) || !subset(q.FieldScopes, i.FieldScopes) || !subset(q.Capabilities, i.Capabilities) {
		return deny("data_scope_not_granted")
	}
	d.Allowed, d.Reason = true, "scope_granted"
	d.Digest = scopeDecisionDigest(d)
	return d, nil
}

// CheckScope is also available as a method for semantic ownership at the
// installation boundary.
func (i Installation) CheckScope(q ScopeQuery) (ScopeDecision, error) {
	return CheckScope(i, q)
}

// InstallationExplanation is bounded metadata suitable for audit output.
// It contains no field names, data values, approver evidence contents or raw
// review material.
type InstallationExplanation struct {
	InstallationID           string
	Revision                 uint64
	State                    GrantState
	ApplicationID            string
	ApplicationVersion       string
	ApplicationDigest        string
	TenantScopePresent       bool
	OrganizationScopePresent bool
	PopulationScopePresent   bool
	DataClassCount           int
	FieldScopeCount          int
	PurposePresent           bool
	CapabilityCount          int
	ReviewRefPresent         bool
	InstallationDigest       string
}

// Explain returns audit-safe installation metadata.
func (i Installation) Explain() InstallationExplanation {
	return InstallationExplanation{InstallationID: i.InstallationID, Revision: i.Revision, State: i.State, ApplicationID: i.VersionBinding.ApplicationID, ApplicationVersion: i.VersionBinding.Version, ApplicationDigest: i.VersionBinding.Digest, TenantScopePresent: i.TenantScope != "", OrganizationScopePresent: i.OrganizationScope != "", PopulationScopePresent: i.PopulationScope != "", DataClassCount: len(i.DataClasses), FieldScopeCount: len(i.FieldScopes), PurposePresent: i.Purpose != "", CapabilityCount: len(i.Capabilities), ReviewRefPresent: i.ReviewRef != "", InstallationDigest: i.Digest}
}

// ExplainInstallation is the package-level explanation helper for callers
// that prefer functional inspection.
func ExplainInstallation(i Installation) InstallationExplanation { return i.Explain() }

// Explain returns a bounded scope-decision summary without requested scope
// values.
func (d ScopeDecision) Explain() string {
	return fmt.Sprintf("partnerapp installation scope allowed=%t rule=%s reason=%s installation_digest=%s decision_digest=%s", d.Allowed, d.Rule, d.Reason, d.InstallationDigest, d.Digest)
}

func (b ApplicationVersionBinding) validate() error {
	if strings.TrimSpace(b.ApplicationID) == "" || strings.TrimSpace(b.Version) != b.Version || strings.TrimSpace(b.Version) == "" || b.Revision == 0 || !exactRef(b.Digest) {
		return fmt.Errorf("%w: exact application version binding is required", ErrInstallationReview)
	}
	return nil
}

func validateInstallationRequest(req InstallationRequest, v ApplicationVersion) error {
	if !exactRef(req.InstallationID) || !exactRef(req.Requester) {
		return fmt.Errorf("%w: installation id and requester are required", ErrInvalidInstallation)
	}
	for label, value := range map[string]string{"tenant": req.TenantScope, "organization": req.OrganizationScope, "population": req.PopulationScope, "purpose": req.Purpose} {
		if err := validateExactSet([]string{value}, label, ErrInvalidInstallation); err != nil {
			return err
		}
	}
	for label, values := range map[string][]string{"data class": req.DataClasses, "field scope": req.FieldScopes, "capability": req.Capabilities} {
		if err := validateExactSet(values, label, ErrInvalidInstallation); err != nil {
			return err
		}
	}
	for _, capability := range req.Capabilities {
		if !contains(v.DeclaredCapabilities, capability) {
			return fmt.Errorf("%w: capability %q is not declared by the exact application version", ErrInvalidInstallation, capability)
		}
	}
	if req.ReviewRef != "" && !exactRef(req.ReviewRef) {
		return fmt.Errorf("%w: review reference is not exact", ErrInstallationReview)
	}
	return nil
}

func validateInstallationReview(i Installation, review DataProcessingReview, now time.Time) error {
	if err := review.ValidAt(now); err != nil {
		return fmt.Errorf("%w: %v", ErrInstallationReview, err)
	}
	if review.ApplicationID != i.VersionBinding.ApplicationID || review.Version != i.VersionBinding.Version {
		return fmt.Errorf("%w: review is bound to a different application version", ErrInstallationReview)
	}
	for _, item := range review.Items {
		if item.Finding != FindingPass {
			return fmt.Errorf("%w: review control did not pass", ErrInstallationReview)
		}
	}
	if i.ReviewRef != "" && !reviewReferenceMatches(i.ReviewRef, review.Digest) {
		return fmt.Errorf("%w: review reference does not match the signed review", ErrInstallationReview)
	}
	for _, ref := range installationReviewRefs(i) {
		if !reviewAdmits(review.ScopeRefs, ref) {
			return fmt.Errorf("%w: installation scope is not admitted", ErrReviewScope)
		}
	}
	for _, dataClass := range i.DataClasses {
		if !contains(review.DataClasses, dataClass) {
			return fmt.Errorf("%w: data class is not admitted", ErrReviewScope)
		}
	}
	return nil
}

func installationReviewRefs(i Installation) []string {
	refs := []string{i.TenantScope, i.OrganizationScope, i.PopulationScope, i.Purpose}
	refs = append(refs, i.FieldScopes...)
	refs = append(refs, i.Capabilities...)
	return uniqueSorted(refs)
}

func reviewAdmits(admitted []string, wanted string) bool {
	for _, ref := range admitted {
		if ref == wanted || strings.HasPrefix(wanted, ref+"/") {
			return true
		}
		for _, prefix := range []string{"tenant:", "org:", "population:", "field:", "data:", "purpose:", "capability:"} {
			if ref == prefix+wanted || ref == prefix+strings.TrimPrefix(wanted, prefix) {
				return true
			}
		}
	}
	return false
}

func reviewReferenceMatches(ref, digest string) bool { return ref == digest || ref == "review:"+digest }

func (b ApplicationVersionBinding) canonical() string {
	return b.ApplicationID + "|" + b.Version + "|" + fmt.Sprint(b.Revision) + "|" + b.Digest
}

func installationDigest(i Installation) string {
	return digestText("hcmnext.domains.partnerapp.Installation/v1", strings.Join([]string{i.InstallationID, fmt.Sprint(i.Revision), string(i.State), i.VersionBinding.canonical(), i.TenantScope, i.OrganizationScope, i.PopulationScope, strings.Join(sortedCopy(i.DataClasses), "\x00"), strings.Join(sortedCopy(i.FieldScopes), "\x00"), i.Purpose, strings.Join(sortedCopy(i.Capabilities), "\x00"), i.Requester, i.ApproverEvidence.Requester, i.ApproverEvidence.Approver, i.ApproverEvidence.EvidenceRef, i.ApproverEvidence.ReviewRef, i.ReviewRef}, "\x01"))
}

func eventDigest(e InstallationEvent) string {
	return digestText("hcmnext.domains.partnerapp.InstallationEvent/v1", strings.Join([]string{e.InstallationID, fmt.Sprint(e.Revision), e.PreviousDigest, string(e.PreviousState), string(e.State), e.Actor, e.EvidenceRef, e.ReviewRef, e.InstallationDigest}, "\x01"))
}

func scopeQueryDigest(q ScopeQuery) string {
	return digestText("hcmnext.domains.partnerapp.ScopeQuery/v1", strings.Join([]string{q.TenantScope, q.OrganizationScope, q.PopulationScope, strings.Join(sortedCopy(q.DataClasses), "\x00"), strings.Join(sortedCopy(q.FieldScopes), "\x00"), q.Purpose, strings.Join(sortedCopy(q.Capabilities), "\x00")}, "\x01"))
}

func scopeDecisionDigest(d ScopeDecision) string {
	return digestText("hcmnext.domains.partnerapp.ScopeDecision/v1", strings.Join([]string{fmt.Sprint(d.Allowed), d.Rule, d.Reason, d.InstallationID, d.InstallationDigest, fmt.Sprint(d.InstallationRevision), d.QueryDigest}, "\x01"))
}

func digestText(profile, value string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(profile + "\x00" + value))
	return hex.EncodeToString(h.Sum(nil))
}

func (q ScopeQuery) validate() error {
	for label, value := range map[string]string{"tenant": q.TenantScope, "organization": q.OrganizationScope, "population": q.PopulationScope, "purpose": q.Purpose} {
		if err := validateExactSet([]string{value}, label, ErrInstallationScope); err != nil {
			return err
		}
	}
	for label, values := range map[string][]string{"data class": q.DataClasses, "field scope": q.FieldScopes, "capability": q.Capabilities} {
		if len(values) > 0 {
			if err := validateExactSet(values, label, ErrInstallationScope); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateExactSet(values []string, label string, sentinel error) error {
	if len(values) == 0 {
		return fmt.Errorf("%w: %s cannot be empty", sentinel, label)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !exactRef(value) || value == "*" {
			return fmt.Errorf("%w: %s contains an inexact value", sentinel, label)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%w: %s contains duplicate %q", sentinel, label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func exactRef(value string) bool {
	return strings.TrimSpace(value) != "" && strings.TrimSpace(value) == value && value != "*"
}

func subset(wanted, granted []string) bool {
	for _, value := range wanted {
		if !contains(granted, value) {
			return false
		}
	}
	return true
}

func uniqueSorted(values []string) []string {
	out := sortedCopy(values)
	return slicesCompact(out)
}

func slicesCompact(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func cloneInstallation(i Installation) Installation {
	i.DataClasses = append([]string(nil), i.DataClasses...)
	i.FieldScopes = append([]string(nil), i.FieldScopes...)
	i.Capabilities = append([]string(nil), i.Capabilities...)
	return i
}
