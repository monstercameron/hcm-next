// Package partnerapp owns the typed, immutable vocabulary for partner
// applications and their reviewed versions. It is deliberately a pure
// domain package: no persistence, transport, credentials, or provider calls
// are performed here.
package partnerapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const version = 1

// Version is the partner-application domain contract version.
func Version() int { return version }

var (
	ErrInvalidApplication = errors.New("partnerapp: invalid partner application")
	ErrInvalidVersion     = errors.New("partnerapp: invalid application version")
	ErrReviewRequired     = errors.New("partnerapp: an approved distinct review is required")
	ErrAgreementMismatch  = errors.New("partnerapp: declaration is not covered by the agreement")
	ErrInvalidLifecycle   = errors.New("partnerapp: invalid lifecycle transition")
	ErrInvalidRevocation  = errors.New("partnerapp: invalid revocation event")
)

// LifecycleState is the immutable state of an application version.
type LifecycleState string

const (
	StateDraft      LifecycleState = "DRAFT"
	StateReviewed   LifecycleState = "REVIEWED"
	StateActive     LifecycleState = "ACTIVE"
	StateDeprecated LifecycleState = "DEPRECATED"
	StateRevoked    LifecycleState = "REVOKED"
)

func (s LifecycleState) valid() bool {
	return s == StateDraft || s == StateReviewed || s == StateActive || s == StateDeprecated || s == StateRevoked
}

// EndpointRef identifies a redirect or callback endpoint by a stable
// reference and its content digest. URL parsing and network verification are
// intentionally outside this vocabulary package.
type EndpointRef struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}

func (e EndpointRef) validate() error {
	if strings.TrimSpace(e.Ref) == "" || strings.TrimSpace(e.Ref) != e.Ref || strings.TrimSpace(e.Digest) == "" || strings.TrimSpace(e.Digest) != e.Digest {
		return fmt.Errorf("%w: endpoint reference and digest are required", ErrInvalidApplication)
	}
	return nil
}

// PartnerAgreement is the read-only agreement coverage supplied to
// declaration validation. Its Ref is the identity a version records.
type PartnerAgreement struct {
	Ref          string   `json:"ref"`
	Capabilities []string `json:"capabilities"`
	DataClasses  []string `json:"data_classes"`
}

func (a PartnerAgreement) validate() error {
	if strings.TrimSpace(a.Ref) == "" || strings.TrimSpace(a.Ref) != a.Ref {
		return fmt.Errorf("%w: agreement ref is required", ErrAgreementMismatch)
	}
	if err := validateSet(a.Capabilities, "agreement capabilities", ErrAgreementMismatch); err != nil {
		return err
	}
	return validateSet(a.DataClasses, "agreement data classes", ErrAgreementMismatch)
}

// PartnerApplication is stable identity and declaration metadata shared by
// all immutable versions.
type PartnerApplication struct {
	PartnerRef           string        `json:"partner_ref"`
	ApplicationID        string        `json:"application_id"`
	AgreementRef         string        `json:"agreement_ref"`
	DeclaredCapabilities []string      `json:"declared_capabilities"`
	DataClasses          []string      `json:"data_classes"`
	RedirectEndpoints    []EndpointRef `json:"redirect_endpoints"`
	CallbackEndpoints    []EndpointRef `json:"callback_endpoints"`
	ContactRef           string        `json:"contact_ref"`
	LegalRef             string        `json:"legal_ref"`
}

// Validate checks the complete stable application identity.
func (a PartnerApplication) Validate() error {
	for name, value := range map[string]string{
		"partner_ref": a.PartnerRef, "application_id": a.ApplicationID,
		"agreement_ref": a.AgreementRef, "contact_ref": a.ContactRef, "legal_ref": a.LegalRef,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required", ErrInvalidApplication, name)
		}
	}
	if err := validateSet(a.DeclaredCapabilities, "declared capabilities", ErrInvalidApplication); err != nil {
		return err
	}
	if err := validateSet(a.DataClasses, "data classes", ErrInvalidApplication); err != nil {
		return err
	}
	if err := validateEndpoints(a.RedirectEndpoints, "redirect endpoints"); err != nil {
		return err
	}
	return validateEndpoints(a.CallbackEndpoints, "callback endpoints")
}

// ReviewRecord is approval evidence attached to a version. An approver must
// be distinct from the version requester before the version can be ACTIVE.
type ReviewRecord struct {
	Approver string `json:"approver"`
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// ApplicationVersion is one immutable application version revision. Lifecycle
// helpers return the next Revision and preserve the source value unchanged.
type ApplicationVersion struct {
	ApplicationID        string         `json:"application_id"`
	Version              string         `json:"version"`
	Revision             uint64         `json:"revision"`
	State                LifecycleState `json:"state"`
	Requester            string         `json:"requester"`
	Review               ReviewRecord   `json:"review"`
	AgreementRef         string         `json:"agreement_ref"`
	DeclaredCapabilities []string       `json:"declared_capabilities"`
	DataClasses          []string       `json:"data_classes"`
	RedirectEndpoints    []EndpointRef  `json:"redirect_endpoints"`
	CallbackEndpoints    []EndpointRef  `json:"callback_endpoints"`
	ContactRef           string         `json:"contact_ref"`
	LegalRef             string         `json:"legal_ref"`
	SuccessorVersion     string         `json:"successor_version,omitempty"`
	Digest               string         `json:"digest"`
}

// VersionRequest is the content of an initial DRAFT version. The stable
// application's declarations are copied explicitly so a version cannot
// silently widen when the application value is changed by a caller.
type VersionRequest struct {
	Version              string
	Requester            string
	AgreementRef         string
	DeclaredCapabilities []string
	DataClasses          []string
	RedirectEndpoints    []EndpointRef
	CallbackEndpoints    []EndpointRef
	ContactRef           string
	LegalRef             string
}

// NewDraft creates the first immutable DRAFT version for app.
func NewDraft(app PartnerApplication, req VersionRequest, agreement PartnerAgreement) (ApplicationVersion, error) {
	if err := app.Validate(); err != nil {
		return ApplicationVersion{}, err
	}
	if req.AgreementRef == "" {
		req.AgreementRef = app.AgreementRef
	}
	if req.ContactRef == "" {
		req.ContactRef = app.ContactRef
	}
	if req.LegalRef == "" {
		req.LegalRef = app.LegalRef
	}
	if len(req.DeclaredCapabilities) == 0 {
		req.DeclaredCapabilities = app.DeclaredCapabilities
	}
	if len(req.DataClasses) == 0 {
		req.DataClasses = app.DataClasses
	}
	if len(req.RedirectEndpoints) == 0 {
		req.RedirectEndpoints = app.RedirectEndpoints
	}
	if len(req.CallbackEndpoints) == 0 {
		req.CallbackEndpoints = app.CallbackEndpoints
	}
	v := ApplicationVersion{
		ApplicationID: app.ApplicationID, Version: req.Version, Revision: 1, State: StateDraft,
		Requester: req.Requester, AgreementRef: req.AgreementRef,
		DeclaredCapabilities: append([]string(nil), req.DeclaredCapabilities...), DataClasses: append([]string(nil), req.DataClasses...),
		RedirectEndpoints: cloneEndpoints(req.RedirectEndpoints), CallbackEndpoints: cloneEndpoints(req.CallbackEndpoints),
		ContactRef: req.ContactRef, LegalRef: req.LegalRef,
	}
	if err := v.validateAgainst(app, agreement); err != nil {
		return ApplicationVersion{}, err
	}
	v.Digest = computeDigest(v)
	return v, nil
}

// NewVersion is the vocabulary-oriented alias for NewDraft.
func NewVersion(app PartnerApplication, req VersionRequest, agreement PartnerAgreement) (ApplicationVersion, error) {
	return NewDraft(app, req, agreement)
}

// RecordReview creates a REVIEWED revision with approval evidence. It does
// not mutate v and refuses self-review.
func (v ApplicationVersion) RecordReview(approver string, approved bool, reason string) (ApplicationVersion, error) {
	if err := v.Verify(); err != nil {
		return ApplicationVersion{}, err
	}
	if v.State == StateRevoked || v.State == StateDeprecated || strings.TrimSpace(approver) == "" || strings.TrimSpace(approver) != approver || approver == v.Requester {
		return ApplicationVersion{}, fmt.Errorf("%w: reviewer must be distinct and version must be reviewable", ErrReviewRequired)
	}
	if !approved {
		return ApplicationVersion{}, fmt.Errorf("%w: review did not approve version", ErrReviewRequired)
	}
	next := v.next(StateReviewed)
	next.Review = ReviewRecord{Approver: approver, Approved: approved, Reason: reason}
	next.Digest = computeDigest(next)
	return next, nil
}

// WithReview is a descriptive alias for RecordReview.
func (v ApplicationVersion) WithReview(approver string, approved bool, reason string) (ApplicationVersion, error) {
	return v.RecordReview(approver, approved, reason)
}

// Approve is the concise alias for recording an approved review.
func (v ApplicationVersion) Approve(approver, reason string) (ApplicationVersion, error) {
	return v.RecordReview(approver, true, reason)
}

// Activate creates an ACTIVE revision only after a distinct approved review.
func (v ApplicationVersion) Activate() (ApplicationVersion, error) {
	if err := v.Verify(); err != nil {
		return ApplicationVersion{}, err
	}
	if v.State != StateReviewed || !v.Review.Approved || strings.TrimSpace(v.Review.Approver) == "" || v.Review.Approver == v.Requester {
		return ApplicationVersion{}, ErrReviewRequired
	}
	next := v.next(StateActive)
	next.Digest = computeDigest(next)
	return next, nil
}

// Deprecate creates a DEPRECATED revision naming the required successor.
func (v ApplicationVersion) Deprecate(requester, successor string) (ApplicationVersion, error) {
	if err := v.Verify(); err != nil {
		return ApplicationVersion{}, err
	}
	if v.State == StateRevoked || v.State == StateDeprecated || strings.TrimSpace(requester) == "" || strings.TrimSpace(requester) != requester || strings.TrimSpace(successor) == "" || successor == v.Version {
		return ApplicationVersion{}, fmt.Errorf("%w: deprecation requires a distinct successor", ErrInvalidLifecycle)
	}
	next := v.next(StateDeprecated)
	next.Requester = requester
	next.SuccessorVersion = successor
	next.Digest = computeDigest(next)
	return next, nil
}

// RevocationEvent is a digest-addressed record of the subscriptions and
// leases invalidated by one application-version revocation.
type RevocationEvent struct {
	ApplicationRef   string   `json:"application_ref"`
	VersionRef       string   `json:"version_ref"`
	Requester        string   `json:"requester"`
	SubscriptionRefs []string `json:"subscription_refs"`
	LeaseRefs        []string `json:"lease_refs"`
	Digest           string   `json:"digest"`
}

// Revoke creates a terminal REVOKED revision and its immutable invalidation
// event. Both lists are copied, deduplicated and sorted before digesting.
func (v ApplicationVersion) Revoke(requester string, subscriptionRefs, leaseRefs []string) (ApplicationVersion, RevocationEvent, error) {
	if err := v.Verify(); err != nil {
		return ApplicationVersion{}, RevocationEvent{}, err
	}
	if v.State == StateRevoked || strings.TrimSpace(requester) == "" || strings.TrimSpace(requester) != requester {
		return ApplicationVersion{}, RevocationEvent{}, fmt.Errorf("%w: version is already revoked or requester is invalid", ErrInvalidRevocation)
	}
	subs, err := normalizeRefs(subscriptionRefs, "subscription")
	if err != nil {
		return ApplicationVersion{}, RevocationEvent{}, err
	}
	leases, err := normalizeRefs(leaseRefs, "lease")
	if err != nil {
		return ApplicationVersion{}, RevocationEvent{}, err
	}
	next := v.next(StateRevoked)
	next.Requester = requester
	next.Digest = computeDigest(next)
	event := RevocationEvent{ApplicationRef: v.ApplicationID, VersionRef: v.Version, Requester: requester, SubscriptionRefs: subs, LeaseRefs: leases}
	event.Digest = computeRevocationDigest(event)
	return next, event, nil
}

// Validate checks v against the stable identity and agreement coverage.
func (v ApplicationVersion) Validate(app PartnerApplication, agreement PartnerAgreement) error {
	return v.validateAgainst(app, agreement)
}

func (v ApplicationVersion) validateAgainst(app PartnerApplication, agreement PartnerAgreement) error {
	if err := app.Validate(); err != nil {
		return err
	}
	if err := agreement.validate(); err != nil {
		return err
	}
	if v.ApplicationID != app.ApplicationID || strings.TrimSpace(v.Version) == "" || strings.TrimSpace(v.Version) != v.Version || v.Revision == 0 || !v.State.valid() {
		return fmt.Errorf("%w: application, version, revision and state are required", ErrInvalidVersion)
	}
	if strings.TrimSpace(v.Requester) == "" || strings.TrimSpace(v.Requester) != v.Requester {
		return fmt.Errorf("%w: requester is required", ErrInvalidVersion)
	}
	if v.AgreementRef != app.AgreementRef || v.AgreementRef != agreement.Ref {
		return fmt.Errorf("%w: agreement ref mismatch", ErrAgreementMismatch)
	}
	if err := validateSet(v.DeclaredCapabilities, "declared capabilities", ErrInvalidVersion); err != nil {
		return err
	}
	if err := validateSet(v.DataClasses, "data classes", ErrInvalidVersion); err != nil {
		return err
	}
	for _, capability := range v.DeclaredCapabilities {
		if !contains(agreement.Capabilities, capability) {
			return fmt.Errorf("%w: capability %q", ErrAgreementMismatch, capability)
		}
	}
	for _, dataClass := range v.DataClasses {
		if !contains(agreement.DataClasses, dataClass) {
			return fmt.Errorf("%w: data class %q", ErrAgreementMismatch, dataClass)
		}
	}
	if err := validateEndpoints(v.RedirectEndpoints, "redirect endpoints"); err != nil {
		return err
	}
	if err := validateEndpoints(v.CallbackEndpoints, "callback endpoints"); err != nil {
		return err
	}
	for name, value := range map[string]string{"contact_ref": v.ContactRef, "legal_ref": v.LegalRef} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required", ErrInvalidVersion, name)
		}
	}
	if v.State == StateActive && (!v.Review.Approved || strings.TrimSpace(v.Review.Approver) == "" || v.Review.Approver == v.Requester) {
		return ErrReviewRequired
	}
	if v.State == StateDeprecated && (strings.TrimSpace(v.SuccessorVersion) == "" || v.SuccessorVersion == v.Version) {
		return fmt.Errorf("%w: deprecated version must name a successor", ErrInvalidLifecycle)
	}
	return nil
}

// Verify detects mutation after a version was minted. It checks content
// shape without requiring a registry or agreement lookup.
func (v ApplicationVersion) Verify() error {
	if strings.TrimSpace(v.ApplicationID) == "" || strings.TrimSpace(v.Version) == "" || v.Revision == 0 || !v.State.valid() || v.Digest == "" {
		return ErrInvalidVersion
	}
	if v.State == StateActive && (!v.Review.Approved || strings.TrimSpace(v.Review.Approver) == "" || v.Review.Approver == v.Requester) {
		return ErrReviewRequired
	}
	if v.State == StateDeprecated && (v.SuccessorVersion == "" || v.SuccessorVersion == v.Version) {
		return ErrInvalidLifecycle
	}
	if computeDigest(v) != v.Digest {
		return fmt.Errorf("%w: digest does not match %s@%d", ErrInvalidVersion, v.Version, v.Revision)
	}
	return nil
}

func (v ApplicationVersion) next(state LifecycleState) ApplicationVersion {
	next := v
	next.Revision++
	next.State = state
	next.Digest = ""
	next.DeclaredCapabilities = append([]string(nil), v.DeclaredCapabilities...)
	next.DataClasses = append([]string(nil), v.DataClasses...)
	next.RedirectEndpoints = cloneEndpoints(v.RedirectEndpoints)
	next.CallbackEndpoints = cloneEndpoints(v.CallbackEndpoints)
	return next
}

// Registry is a concurrent in-memory design adapter. It retains every
// application version revision and never replaces a stored value.
type Registry struct {
	mu           sync.RWMutex
	applications map[string]PartnerApplication
	agreements   map[string]PartnerAgreement
	versions     map[string][]ApplicationVersion
}

// NewRegistry creates a registry with the agreements needed for coverage
// checks. Agreements may also be added with RegisterAgreement.
func NewRegistry(agreements ...PartnerAgreement) (*Registry, error) {
	r := &Registry{applications: make(map[string]PartnerApplication), agreements: make(map[string]PartnerAgreement), versions: make(map[string][]ApplicationVersion)}
	for _, agreement := range agreements {
		if err := r.RegisterAgreement(agreement); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// RegisterAgreement adds one immutable agreement fact.
func (r *Registry) RegisterAgreement(agreement PartnerAgreement) error {
	if r == nil {
		return ErrAgreementMismatch
	}
	if err := agreement.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.agreements[agreement.Ref]; exists {
		return fmt.Errorf("%w: duplicate agreement %q", ErrAgreementMismatch, agreement.Ref)
	}
	r.agreements[agreement.Ref] = cloneAgreement(agreement)
	return nil
}

// RegisterApplication adds stable identity once.
func (r *Registry) RegisterApplication(app PartnerApplication) error {
	if r == nil {
		return ErrInvalidApplication
	}
	if err := app.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.applications[app.ApplicationID]; exists {
		return fmt.Errorf("%w: duplicate application %q", ErrInvalidApplication, app.ApplicationID)
	}
	r.applications[app.ApplicationID] = cloneApplication(app)
	return nil
}

// Register is an alias for RegisterApplication.
func (r *Registry) Register(app PartnerApplication) error { return r.RegisterApplication(app) }

// RegisterVersion validates agreement coverage and appends an immutable
// version revision. A key may never be overwritten.
func (r *Registry) RegisterVersion(v ApplicationVersion) error {
	if r == nil {
		return ErrInvalidVersion
	}
	if err := v.Verify(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	app, ok := r.applications[v.ApplicationID]
	if !ok {
		return fmt.Errorf("%w: unknown application %q", ErrInvalidVersion, v.ApplicationID)
	}
	agreement, ok := r.agreements[v.AgreementRef]
	if !ok {
		return fmt.Errorf("%w: unknown agreement %q", ErrAgreementMismatch, v.AgreementRef)
	}
	if err := v.validateAgainst(app, agreement); err != nil {
		return err
	}
	history := r.versions[v.ApplicationID+"\x00"+v.Version]
	if len(history) > 0 && v.Revision != history[len(history)-1].Revision+1 {
		return fmt.Errorf("%w: version revision must append in order", ErrInvalidVersion)
	}
	if len(history) == 0 && v.Revision != 1 {
		return fmt.Errorf("%w: first version revision must be 1", ErrInvalidVersion)
	}
	r.versions[v.ApplicationID+"\x00"+v.Version] = append(history, cloneVersion(v))
	return nil
}

// RegisterApplicationVersion is an alias for RegisterVersion.
func (r *Registry) RegisterApplicationVersion(v ApplicationVersion) error {
	return r.RegisterVersion(v)
}

// Revisions returns the retained immutable history for a semantic version.
func (r *Registry) Revisions(applicationID, semanticVersion string) []ApplicationVersion {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	history := r.versions[applicationID+"\x00"+semanticVersion]
	out := make([]ApplicationVersion, len(history))
	for i := range history {
		out[i] = cloneVersion(history[i])
	}
	return out
}

type digestEndpoint struct{ Ref, Digest string }

type versionDigestView struct {
	ApplicationID, Version, Requester, AgreementRef, ContactRef, LegalRef, SuccessorVersion string
	Revision                                                                                uint64
	State                                                                                   LifecycleState
	Review                                                                                  ReviewRecord
	Capabilities, DataClasses                                                               []string
	Redirects, Callbacks                                                                    []digestEndpoint
}

func computeDigest(v ApplicationVersion) string {
	view := versionDigestView{ApplicationID: v.ApplicationID, Version: v.Version, Revision: v.Revision, State: v.State, Requester: v.Requester, Review: v.Review, AgreementRef: v.AgreementRef, ContactRef: v.ContactRef, LegalRef: v.LegalRef, SuccessorVersion: v.SuccessorVersion, Capabilities: sortedCopy(v.DeclaredCapabilities), DataClasses: sortedCopy(v.DataClasses), Redirects: digestEndpoints(v.RedirectEndpoints), Callbacks: digestEndpoints(v.CallbackEndpoints)}
	return digestJSON("hcmnext.domains.partnerapp.ApplicationVersion/v1", view)
}

func computeRevocationDigest(event RevocationEvent) string {
	view := event
	view.Digest = ""
	return digestJSON("hcmnext.domains.partnerapp.RevocationEvent/v1", view)
}

func digestJSON(profile string, value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	h := sha256.New()
	_, _ = h.Write([]byte(profile + "\x00"))
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// Explain is bounded metadata for inspection; it excludes declarations and
// endpoint details that are not needed to identify a version.
type ApplicationExplanation struct {
	PartnerRef      string
	ApplicationID   string
	AgreementRef    string
	ContactRef      string
	LegalRef        string
	CapabilityCount int
	DataClassCount  int
	RevisionCount   int
}

// Explain returns a bounded explanation of a stable application.
func (a PartnerApplication) Explain() ApplicationExplanation {
	return ApplicationExplanation{PartnerRef: a.PartnerRef, ApplicationID: a.ApplicationID, AgreementRef: a.AgreementRef, ContactRef: a.ContactRef, LegalRef: a.LegalRef, CapabilityCount: len(a.DeclaredCapabilities), DataClassCount: len(a.DataClasses)}
}

// Explain is the package-level explanation symbol required by the domain
// contract.
func Explain(a PartnerApplication) ApplicationExplanation { return a.Explain() }

func validateSet(values []string, label string, sentinel error) error {
	if len(values) == 0 {
		return fmt.Errorf("%w: %s cannot be empty", sentinel, label)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s contains an empty or padded value", sentinel, label)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%w: %s contains duplicate %q", sentinel, label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateEndpoints(endpoints []EndpointRef, label string) error {
	if len(endpoints) == 0 {
		return fmt.Errorf("%w: %s cannot be empty", ErrInvalidApplication, label)
	}
	seen := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if err := endpoint.validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidApplication, label, err)
		}
		if _, exists := seen[endpoint.Ref]; exists {
			return fmt.Errorf("%w: duplicate %s %q", ErrInvalidApplication, label, endpoint.Ref)
		}
		seen[endpoint.Ref] = struct{}{}
	}
	return nil
}

func normalizeRefs(refs []string, label string) ([]string, error) {
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" || strings.TrimSpace(ref) != ref {
			return nil, fmt.Errorf("%w: %s reference is empty or padded", ErrInvalidRevocation, label)
		}
		if _, exists := seen[ref]; exists {
			return nil, fmt.Errorf("%w: duplicate %s reference %q", ErrInvalidRevocation, label, ref)
		}
		seen[ref] = struct{}{}
	}
	out := append([]string(nil), refs...)
	sort.Strings(out)
	return out, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func digestEndpoints(values []EndpointRef) []digestEndpoint {
	out := make([]digestEndpoint, 0, len(values))
	for _, value := range values {
		out = append(out, digestEndpoint(value))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

func cloneEndpoints(values []EndpointRef) []EndpointRef { return append([]EndpointRef(nil), values...) }

func cloneAgreement(a PartnerAgreement) PartnerAgreement {
	a.Capabilities = append([]string(nil), a.Capabilities...)
	a.DataClasses = append([]string(nil), a.DataClasses...)
	return a
}

func cloneApplication(a PartnerApplication) PartnerApplication {
	a.DeclaredCapabilities = append([]string(nil), a.DeclaredCapabilities...)
	a.DataClasses = append([]string(nil), a.DataClasses...)
	a.RedirectEndpoints = cloneEndpoints(a.RedirectEndpoints)
	a.CallbackEndpoints = cloneEndpoints(a.CallbackEndpoints)
	return a
}

func cloneVersion(v ApplicationVersion) ApplicationVersion {
	v.DeclaredCapabilities = append([]string(nil), v.DeclaredCapabilities...)
	v.DataClasses = append([]string(nil), v.DataClasses...)
	v.RedirectEndpoints = cloneEndpoints(v.RedirectEndpoints)
	v.CallbackEndpoints = cloneEndpoints(v.CallbackEndpoints)
	return v
}
