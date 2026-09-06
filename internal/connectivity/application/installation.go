// Package application owns the connectivity-side lifecycle adapter for
// partner installations. It stores only references, digests, and lifecycle
// evidence; application declarations remain owned by partnerapp.
package application

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const contractVersion = 1

// Version returns the installation lifecycle contract version.
func Version() int { return contractVersion }

// Explain describes the fail-closed installation lifecycle boundary.
func Explain() string {
	return "partner installation lifecycle with exact-scope upgrades, quarantine, revocation, and retained evidence"
}

type State string

const (
	Active      State = "ACTIVE"
	Quarantined State = "QUARANTINED"
	Revoked     State = "REVOKED"
)

var (
	ErrInvalidInstallation = errors.New("application: invalid installation")
	ErrNotFound            = errors.New("application: installation not found")
	ErrUpgradeRejected     = errors.New("application: installation upgrade rejected")
	ErrScopeExpansion      = errors.New("application: installation upgrade expands scope")
	ErrLifecycleTransition = errors.New("application: invalid installation lifecycle transition")
	ErrPropagationSLO      = errors.New("application: installation control exceeded propagation SLO")
	ErrEvidenceRequired    = errors.New("application: lifecycle evidence is required")
)

// Scope is the exact authority granted to an installation. Slices are
// canonicalized before they are digested; no empty value is a wildcard.
type Scope struct {
	TenantID       string
	OrganizationID string
	Population     string
	DataClasses    []string
	Fields         []string
	Purpose        string
	Capabilities   []string
}

// Equal reports exact semantic scope equality, including set-valued fields.
func (s Scope) Equal(other Scope) bool {
	return s.TenantID == other.TenantID && s.OrganizationID == other.OrganizationID && s.Population == other.Population && s.Purpose == other.Purpose && equalSet(s.DataClasses, other.DataClasses) && equalSet(s.Fields, other.Fields) && equalSet(s.Capabilities, other.Capabilities)
}

func (s Scope) validate() error {
	for _, value := range []string{s.TenantID, s.OrganizationID, s.Population, s.Purpose} {
		if !exact(value) {
			return ErrInvalidInstallation
		}
	}
	for _, values := range [][]string{s.DataClasses, s.Fields, s.Capabilities} {
		if len(values) == 0 {
			return ErrInvalidInstallation
		}
		seen := map[string]struct{}{}
		for _, value := range values {
			if !exact(value) {
				return ErrInvalidInstallation
			}
			if _, ok := seen[value]; ok {
				return ErrInvalidInstallation
			}
			seen[value] = struct{}{}
		}
	}
	return nil
}

// Installation is the adapter's immutable current image. Token and
// subscription values are references only; raw credentials are never stored.
type Installation struct {
	ID               string
	TenantID         string
	CellID           string
	ApplicationID    string
	Version          string
	VersionDigest    string
	Revision         uint64
	State            State
	Scope            Scope
	TokenRefs        []string
	SubscriptionRefs []string
	EffectRefs       []string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	PreviousDigest   string
	Digest           string
}

// LifecycleEvent is append-only evidence for a state or version change.
type LifecycleEvent struct {
	InstallationID string
	Revision       uint64
	From           State
	To             State
	Actor          string
	Approver       string
	Reason         string
	EvidenceRef    string
	PreviousDigest string
	NewDigest      string
	At             time.Time
	Digest         string
}

// ControlReceipt proves that token, subscription, and effect references were
// placed under the requested control. It preserves evidence on late delivery.
type ControlReceipt struct {
	InstallationID   string
	Action           State
	Revision         uint64
	TokenRefs        []string
	SubscriptionRefs []string
	EffectRefs       []string
	RequestedAt      time.Time
	AppliedAt        time.Time
	WithinSLO        bool
	Status           string
	EvidenceRef      string
	Digest           string
}

// UpgradeRequest is explicit about the candidate scope. Candidate scope is
// compared exactly, so an upgrade cannot silently add data or authority.
type UpgradeRequest struct {
	InstallationID string
	ApplicationID  string
	Version        string
	VersionDigest  string
	Scope          Scope
	Actor          string
	Approver       string
	EvidenceRef    string
	Reason         string
	At             time.Time
}

// UpgradeResult contains the prior image, the new immutable image, and its
// evidence event.
type UpgradeResult struct {
	Previous Installation
	Current  Installation
	Event    LifecycleEvent
}

// ControlResult contains the new image, lifecycle event, and propagation
// receipt for quarantine or revocation.
type ControlResult struct {
	Previous Installation
	Current  Installation
	Event    LifecycleEvent
	Receipt  ControlReceipt
}

// Manager is a pure concurrent lifecycle adapter. It is intentionally not a
// credential, subscription, or provider implementation.
type Manager struct {
	now            func() time.Time
	propagationSLO time.Duration
	mu             sync.RWMutex
	current        map[string]Installation
	history        map[string][]Installation
	events         map[string][]LifecycleEvent
	receipts       map[string][]ControlReceipt
}

// NewManager creates a lifecycle manager. A positive propagation SLO is
// required so quarantine and revocation have an observable bound.
func NewManager(propagationSLO time.Duration, clocks ...func() time.Time) (*Manager, error) {
	if propagationSLO <= 0 {
		return nil, ErrPropagationSLO
	}
	now := func() time.Time { return time.Now().UTC() }
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &Manager{now: now, propagationSLO: propagationSLO, current: make(map[string]Installation), history: make(map[string][]Installation), events: make(map[string][]LifecycleEvent), receipts: make(map[string][]ControlReceipt)}, nil
}

// Register stores the first ACTIVE image. The manager never replaces a
// revision and refuses already controlled or invalid installations.
func (m *Manager) Register(in Installation) error {
	if m == nil || in.State != Active {
		return ErrInvalidInstallation
	}
	if err := validateInstallation(in); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.current[in.ID]; exists {
		return ErrLifecycleTransition
	}
	in.Digest = installationDigest(in)
	m.current[in.ID] = cloneInstallation(in)
	m.history[in.ID] = []Installation{cloneInstallation(in)}
	event := LifecycleEvent{InstallationID: in.ID, Revision: in.Revision, To: Active, Actor: in.ID, NewDigest: in.Digest, At: in.UpdatedAt.UTC()}
	event.Digest = eventDigest(event)
	m.events[in.ID] = []LifecycleEvent{event}
	return nil
}

// Upgrade moves an active installation to an exact candidate version. It
// rejects a candidate that changes any scope dimension and never auto-mints a
// new grant for a wider scope.
func (m *Manager) Upgrade(request UpgradeRequest) (UpgradeResult, error) {
	if m == nil {
		return UpgradeResult{}, ErrNotFound
	}
	if !exact(request.InstallationID) || !exact(request.ApplicationID) || !exact(request.Version) || !exact(request.VersionDigest) || !exact(request.Actor) || !exact(request.Approver) || request.Actor == request.Approver || !exact(request.EvidenceRef) || !exact(request.Reason) || request.At.IsZero() {
		return UpgradeResult{}, ErrUpgradeRejected
	}
	if err := request.Scope.validate(); err != nil {
		return UpgradeResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	prior, ok := m.current[request.InstallationID]
	if !ok {
		return UpgradeResult{}, ErrNotFound
	}
	if prior.State != Active {
		return UpgradeResult{}, ErrUpgradeRejected
	}
	if !prior.Scope.Equal(request.Scope) {
		return UpgradeResult{}, ErrScopeExpansion
	}
	next := cloneInstallation(prior)
	next.ApplicationID, next.Version, next.VersionDigest = request.ApplicationID, request.Version, request.VersionDigest
	next.Revision++
	next.PreviousDigest = prior.Digest
	next.UpdatedAt = request.At.UTC()
	next.Digest = installationDigest(next)
	event := m.eventLocked(prior, next, request.Actor, request.Approver, request.Reason, request.EvidenceRef, request.At)
	m.commitLocked(next, event)
	return UpgradeResult{Previous: cloneInstallation(prior), Current: cloneInstallation(next), Event: event}, nil
}

// Quarantine immediately denies use while retaining all references and
// returns a receipt proving propagation timing.
func (m *Manager) Quarantine(id, actor, approver, reason, evidenceRef string, at ...time.Time) (ControlResult, error) {
	return m.control(id, Quarantined, actor, approver, reason, evidenceRef, at...)
}

// Revoke is terminal and preserves the complete prior history.
func (m *Manager) Revoke(id, actor, approver, reason, evidenceRef string, at ...time.Time) (ControlResult, error) {
	return m.control(id, Revoked, actor, approver, reason, evidenceRef, at...)
}

func (m *Manager) control(id string, state State, actor, approver, reason, evidenceRef string, supplied ...time.Time) (ControlResult, error) {
	if m == nil {
		return ControlResult{}, ErrNotFound
	}
	if state != Quarantined && state != Revoked || !exact(id) || !exact(actor) || !exact(approver) || actor == approver || !exact(reason) || !exact(evidenceRef) {
		return ControlResult{}, ErrEvidenceRequired
	}
	at := m.now().UTC()
	if len(supplied) > 0 && !supplied[0].IsZero() {
		at = supplied[0].UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	prior, ok := m.current[id]
	if !ok {
		return ControlResult{}, ErrNotFound
	}
	if prior.State == Revoked || (prior.State == Quarantined && state == Quarantined) {
		return ControlResult{}, ErrLifecycleTransition
	}
	next := cloneInstallation(prior)
	next.State, next.Revision, next.PreviousDigest, next.UpdatedAt = state, prior.Revision+1, prior.Digest, at
	next.Digest = installationDigest(next)
	event := m.eventLocked(prior, next, actor, approver, reason, evidenceRef, at)
	requestedAt := prior.UpdatedAt.UTC()
	within := !at.Before(requestedAt) && at.Sub(requestedAt) <= m.propagationSLO
	receipt := ControlReceipt{InstallationID: id, Action: state, Revision: next.Revision, TokenRefs: append([]string(nil), next.TokenRefs...), SubscriptionRefs: append([]string(nil), next.SubscriptionRefs...), EffectRefs: append([]string(nil), next.EffectRefs...), RequestedAt: requestedAt, AppliedAt: at, WithinSLO: within, Status: "APPLIED", EvidenceRef: evidenceRef}
	if !within {
		receipt.Status = "APPLIED_LATE"
	}
	receipt.Digest = receiptDigest(receipt)
	m.commitLocked(next, event)
	m.receipts[id] = append(m.receipts[id], cloneReceipt(receipt))
	return ControlResult{Previous: cloneInstallation(prior), Current: cloneInstallation(next), Event: event, Receipt: receipt}, nil
}

// AuthorizeUse is the local fail-closed gate used before any token,
// subscription, or effect adapter is called.
func (m *Manager) AuthorizeUse(id string, now ...time.Time) (bool, string) {
	in, ok := m.Current(id)
	if !ok {
		return false, "INSTALLATION_NOT_FOUND"
	}
	if in.State != Active {
		return false, "INSTALLATION_CONTROLLED"
	}
	return true, "INSTALLATION_ACTIVE"
}

// Current, History, Events, and Receipts return defensive copies.
func (m *Manager) Current(id string) (Installation, bool) {
	if m == nil {
		return Installation{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	in, ok := m.current[id]
	return cloneInstallation(in), ok
}

func (m *Manager) History(id string) []Installation {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := m.history[id]
	out := make([]Installation, len(items))
	for i := range items {
		out[i] = cloneInstallation(items[i])
	}
	return out
}

func (m *Manager) Events(id string) []LifecycleEvent {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]LifecycleEvent(nil), m.events[id]...)
}

func (m *Manager) Receipts(id string) []ControlReceipt {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ControlReceipt, len(m.receipts[id]))
	for i := range out {
		out[i] = cloneReceipt(m.receipts[id][i])
	}
	return out
}

// Explain returns bounded installation metadata without references to raw
// tokens, subscriptions, effects, or scope values.
func (in Installation) Explain() string {
	return fmt.Sprintf("application installation id=%s revision=%d state=%s app=%s version=%s digest=%s", in.ID, in.Revision, in.State, in.ApplicationID, in.Version, in.Digest)
}

// ExplainInstallation is the package-level explanation helper.
func ExplainInstallation(in Installation) string { return in.Explain() }

func validateInstallation(in Installation) error {
	if !exact(in.ID) || !exact(in.TenantID) || !exact(in.CellID) || !exact(in.ApplicationID) || !exact(in.Version) || !exact(in.VersionDigest) || in.Revision == 0 || in.CreatedAt.IsZero() || in.UpdatedAt.IsZero() || in.State != Active {
		return ErrInvalidInstallation
	}
	if err := in.Scope.validate(); err != nil {
		return err
	}
	if in.Scope.TenantID != in.TenantID {
		return ErrInvalidInstallation
	}
	for _, refs := range [][]string{in.TokenRefs, in.SubscriptionRefs, in.EffectRefs} {
		seen := map[string]struct{}{}
		for _, ref := range refs {
			if !exact(ref) {
				return ErrInvalidInstallation
			}
			if _, ok := seen[ref]; ok {
				return ErrInvalidInstallation
			}
			seen[ref] = struct{}{}
		}
	}
	return nil
}

func (m *Manager) eventLocked(prior, next Installation, actor, approver, reason, evidenceRef string, at time.Time) LifecycleEvent {
	e := LifecycleEvent{InstallationID: next.ID, Revision: next.Revision, From: prior.State, To: next.State, Actor: actor, Approver: approver, Reason: reason, EvidenceRef: evidenceRef, PreviousDigest: prior.Digest, NewDigest: next.Digest, At: at.UTC()}
	e.Digest = eventDigest(e)
	return e
}

func (m *Manager) commitLocked(next Installation, event LifecycleEvent) {
	m.current[next.ID] = cloneInstallation(next)
	m.history[next.ID] = append(m.history[next.ID], cloneInstallation(next))
	m.events[next.ID] = append(m.events[next.ID], event)
}

func installationDigest(in Installation) string {
	return digest(strings.Join([]string{in.ID, in.TenantID, in.CellID, in.ApplicationID, in.Version, in.VersionDigest, fmt.Sprint(in.Revision), string(in.State), scopeDigest(in.Scope), strings.Join(sorted(in.TokenRefs), "\x00"), strings.Join(sorted(in.SubscriptionRefs), "\x00"), strings.Join(sorted(in.EffectRefs), "\x00"), in.CreatedAt.UTC().Format(time.RFC3339Nano), in.UpdatedAt.UTC().Format(time.RFC3339Nano), in.PreviousDigest}, "\x01"))
}

func scopeDigest(s Scope) string {
	return digest(strings.Join([]string{s.TenantID, s.OrganizationID, s.Population, strings.Join(sorted(s.DataClasses), "\x00"), strings.Join(sorted(s.Fields), "\x00"), s.Purpose, strings.Join(sorted(s.Capabilities), "\x00")}, "\x01"))
}
func eventDigest(e LifecycleEvent) string {
	return digest(strings.Join([]string{e.InstallationID, fmt.Sprint(e.Revision), string(e.From), string(e.To), e.Actor, e.Approver, e.Reason, e.EvidenceRef, e.PreviousDigest, e.NewDigest, e.At.UTC().Format(time.RFC3339Nano)}, "\x01"))
}
func receiptDigest(r ControlReceipt) string {
	return digest(strings.Join([]string{r.InstallationID, string(r.Action), fmt.Sprint(r.Revision), strings.Join(sorted(r.TokenRefs), "\x00"), strings.Join(sorted(r.SubscriptionRefs), "\x00"), strings.Join(sorted(r.EffectRefs), "\x00"), r.RequestedAt.UTC().Format(time.RFC3339Nano), r.AppliedAt.UTC().Format(time.RFC3339Nano), fmt.Sprint(r.WithinSLO), r.Status, r.EvidenceRef}, "\x01"))
}
func digest(value string) string {
	sum := sha256.Sum256([]byte("hcmnext.connectivity.application/v1\x00" + value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func exact(value string) bool {
	return strings.TrimSpace(value) != "" && strings.TrimSpace(value) == value && value != "*"
}
func sorted(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
func equalSet(left, right []string) bool {
	a, b := sorted(left), sorted(right)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func cloneInstallation(in Installation) Installation {
	in.Scope.DataClasses = append([]string(nil), in.Scope.DataClasses...)
	in.Scope.Fields = append([]string(nil), in.Scope.Fields...)
	in.Scope.Capabilities = append([]string(nil), in.Scope.Capabilities...)
	in.TokenRefs = append([]string(nil), in.TokenRefs...)
	in.SubscriptionRefs = append([]string(nil), in.SubscriptionRefs...)
	in.EffectRefs = append([]string(nil), in.EffectRefs...)
	return in
}
func cloneReceipt(in ControlReceipt) ControlReceipt {
	in.TokenRefs = append([]string(nil), in.TokenRefs...)
	in.SubscriptionRefs = append([]string(nil), in.SubscriptionRefs...)
	in.EffectRefs = append([]string(nil), in.EffectRefs...)
	return in
}
