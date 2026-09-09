package tenant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// TenantStatus is the lifecycle state owned by the tenant domain.
type TenantStatus string

const (
	TenantActive    TenantStatus = "ACTIVE"
	TenantSuspended TenantStatus = "SUSPENDED"
	TenantClosed    TenantStatus = "CLOSED"

	// Short aliases keep the lifecycle vocabulary convenient at call sites.
	StatusActive    = TenantActive
	StatusSuspended = TenantSuspended
	StatusClosed    = TenantClosed
)

// SuspensionReason is deliberately closed: commercial, security and legal
// suspension are distinct facts and must not collapse into one free-form flag.
type SuspensionReason string

const (
	SuspensionCommercial SuspensionReason = "COMMERCIAL"
	SuspensionSecurity   SuspensionReason = "SECURITY"
	SuspensionLegal      SuspensionReason = "LEGAL"

	ReasonCommercial = SuspensionCommercial
	ReasonSecurity   = SuspensionSecurity
	ReasonLegal      = SuspensionLegal
)

// Capability names the tenant-facing surfaces whose availability changes with
// lifecycle state. AllCapabilities is the complete, deterministic output set.
type Capability string

const (
	CapabilityInteractive Capability = "INTERACTIVE_ACCESS"
	CapabilityConnector   Capability = "CONNECTOR_ACCESS"
	CapabilityMutation    Capability = "DOMAIN_MUTATION"
	CapabilityPayroll     Capability = "PAYROLL_ACCESS"
	CapabilityLegal       Capability = "LEGAL_ACCESS"
	CapabilityAudit       Capability = "AUDIT_ACCESS"
	CapabilityBilling     Capability = "BILLING_ACCESS"
)

var AllCapabilities = []Capability{
	CapabilityInteractive, CapabilityConnector, CapabilityMutation,
	CapabilityPayroll, CapabilityLegal, CapabilityAudit, CapabilityBilling,
}

// CapabilityDecision is the exact allow/deny answer for one capability.
type CapabilityDecision struct {
	Capability Capability `json:"capability"`
	Allowed    bool       `json:"allowed"`
	Reason     string     `json:"reason"`
}

// SessionRef identifies a session present when a lifecycle transition is
// requested. RequiredForAccess sessions are retained only when a transition's
// contract says legally required access must remain available.
type SessionRef struct {
	ID                session.ID `json:"id"`
	RequiredForAccess bool       `json:"required_for_access,omitempty"`
}

// SessionRevoker is the TRUST-005 session control port. Tenant lifecycle owns
// the decision to revoke; the trust package owns the session state change.
type SessionRevoker interface {
	Revoke(context.Context, session.ID, string) (session.Record, error)
}

// PendingWorkAction states what happens to work already queued for the tenant.
type PendingWorkAction string

const (
	PendingWorkFreeze PendingWorkAction = "FREEZE"
	PendingWorkRetain PendingWorkAction = "RETAIN"
)

// PendingWorkItem is an opaque queued item with one legal-preservation bit.
type PendingWorkItem struct {
	ID                string `json:"id"`
	RequiredForAccess bool   `json:"required_for_access,omitempty"`
}

// PendingWorkDisposition is recorded even when the queue is empty.
type PendingWorkDisposition struct {
	Action       PendingWorkAction `json:"action"`
	Count        int               `json:"count"`
	PreservedIDs []string          `json:"preserved_ids,omitempty"`
}

// RevocationReceipt is the tenant's evidence that a forbidden session was
// handed to the trust boundary. It does not copy the session's private record.
type RevocationReceipt struct {
	SessionID string    `json:"session_id"`
	Reason    string    `json:"reason"`
	At        time.Time `json:"at"`
}

// LifecycleEvent is an append-only, digested state transition fact.
type LifecycleEvent struct {
	Kind         string                 `json:"kind"`
	Tenant       string                 `json:"tenant"`
	From         TenantStatus           `json:"from"`
	To           TenantStatus           `json:"to"`
	Reason       SuspensionReason       `json:"reason,omitempty"`
	RequestedBy  string                 `json:"requested_by"`
	At           time.Time              `json:"at"`
	Capabilities []CapabilityDecision   `json:"capabilities"`
	Pending      PendingWorkDisposition `json:"pending"`
	Revocations  []RevocationReceipt    `json:"revocations,omitempty"`
	Digest       string                 `json:"digest"`
}

const (
	EventSuspended = "TENANT_SUSPENDED"
	EventResumed   = "TENANT_RESUMED"
	EventClosed    = "TENANT_CLOSED"
)

var (
	ErrInvalidLifecycle  = errors.New("tenant: invalid lifecycle request")
	ErrLifecycleConflict = errors.New("tenant: lifecycle transition conflict")
	ErrTenantClosed      = errors.New("tenant: tenant is closed")
)

// SuspendRequest is the complete, caller-supplied evidence for suspension.
type SuspendRequest struct {
	Reason         SuspensionReason
	RequestedBy    string
	IdempotencyKey string
	At             time.Time
	Sessions       []SessionRef
	PendingWork    []PendingWorkItem
	Revoker        SessionRevoker
}

// ResumeRequest is the complete evidence for resumption.
type ResumeRequest struct {
	RequestedBy    string
	IdempotencyKey string
	At             time.Time
}

// CloseRequest is the complete evidence for irreversible closure.
type CloseRequest struct {
	RequestedBy    string
	IdempotencyKey string
	At             time.Time
	Sessions       []SessionRef
	PendingWork    []PendingWorkItem
	Revoker        SessionRevoker
}

// TransitionResult is returned by Suspend, Resume and Close. A NOOP result
// repeats the original result and performs no second revocation or append.
type TransitionResult struct {
	Decision     string
	Previous     TenantStatus
	Status       TenantStatus
	Capabilities []CapabilityDecision
	Revocations  []RevocationReceipt
	Pending      PendingWorkDisposition
	Event        LifecycleEvent
}

// Lifecycle is a concurrency-safe, kernel-pure tenant lifecycle ledger.
type Lifecycle struct {
	mu      sync.Mutex
	tenant  string
	status  TenantStatus
	events  []LifecycleEvent
	lastKey string
	last    TransitionResult
}

// Manager is the lifecycle service name used by application composition.
type Manager = Lifecycle

// NewLifecycle returns an active tenant lifecycle.
func NewLifecycle(tenant string) (*Lifecycle, error) {
	if strings.TrimSpace(tenant) == "" {
		return nil, fmt.Errorf("%w: tenant is required", ErrInvalidLifecycle)
	}
	return &Lifecycle{tenant: tenant, status: TenantActive}, nil
}

// NewManager is an alias for NewLifecycle.
func NewManager(tenant string) (*Lifecycle, error) { return NewLifecycle(tenant) }

func (l *Lifecycle) Tenant() string {
	if l == nil {
		return ""
	}
	return l.tenant
}
func (l *Lifecycle) Status() TenantStatus {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status
}
func (l *Lifecycle) Events() []LifecycleEvent {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return cloneEvents(l.events)
}

// Suspend freezes queued work, denies unsafe capabilities and revokes every
// listed non-required session through TRUST-005's port.
func (l *Lifecycle) Suspend(ctx context.Context, req SuspendRequest) (TransitionResult, error) {
	if err := validateRequest(req.Reason, req.RequestedBy, req.IdempotencyKey, req.At); err != nil {
		return TransitionResult{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.status == TenantClosed {
		return TransitionResult{}, ErrTenantClosed
	}
	if out, ok, err := l.replayLocked(req.IdempotencyKey, req.At); ok || err != nil {
		return out, err
	}
	if l.status == TenantSuspended {
		return TransitionResult{}, fmt.Errorf("%w: tenant is already suspended", ErrLifecycleConflict)
	}
	decisions := decisionsFor(req.Reason, false)
	revocations, err := revokeSessions(ctx, req.Sessions, req.Revoker, string(req.Reason), req.At, true)
	if err != nil {
		return TransitionResult{}, err
	}
	pending := pendingFor(PendingWorkFreeze, req.PendingWork)
	result := l.appendLocked(EventSuspended, TenantSuspended, req.Reason, req.RequestedBy, req.At, decisions, pending, revocations)
	l.rememberLocked(req.IdempotencyKey, result)
	return result, nil
}

// Resume restores ordinary tenant capabilities. It is idempotent for the
// same request key and refuses a closed tenant.
func (l *Lifecycle) Resume(_ context.Context, req ResumeRequest) (TransitionResult, error) {
	if err := validateRequest("", req.RequestedBy, req.IdempotencyKey, req.At); err != nil {
		return TransitionResult{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.status == TenantClosed {
		return TransitionResult{}, ErrTenantClosed
	}
	if out, ok, err := l.replayLocked(req.IdempotencyKey, req.At); ok || err != nil {
		return out, err
	}
	if l.status == TenantActive {
		return TransitionResult{}, fmt.Errorf("%w: tenant is already active", ErrLifecycleConflict)
	}
	result := l.appendLocked(EventResumed, TenantActive, "", req.RequestedBy, req.At, decisionsFor("", true), pendingFor(PendingWorkRetain, nil), nil)
	l.rememberLocked(req.IdempotencyKey, result)
	return result, nil
}

// Close irreversibly closes the tenant and retains a complete disposition for
// queued work so closure never silently drops operational or legal obligations.
func (l *Lifecycle) Close(ctx context.Context, req CloseRequest) (TransitionResult, error) {
	if err := validateRequest("", req.RequestedBy, req.IdempotencyKey, req.At); err != nil {
		return TransitionResult{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if out, ok, err := l.replayLocked(req.IdempotencyKey, req.At); ok || err != nil {
		return out, err
	}
	if l.status == TenantClosed {
		return TransitionResult{}, fmt.Errorf("%w: tenant is already closed", ErrLifecycleConflict)
	}
	revocations, err := revokeSessions(ctx, req.Sessions, req.Revoker, "CLOSE", req.At, false)
	if err != nil {
		return TransitionResult{}, err
	}
	result := l.appendLocked(EventClosed, TenantClosed, "", req.RequestedBy, req.At, decisionsFor("", false), pendingFor(PendingWorkRetain, req.PendingWork), revocations)
	l.rememberLocked(req.IdempotencyKey, result)
	return result, nil
}

// Explain returns a stable, human-readable lifecycle summary.
func (l *Lifecycle) Explain() string { return fmt.Sprintf("tenant %s is %s", l.Tenant(), l.Status()) }

func validateRequest(reason SuspensionReason, by, key string, at time.Time) error {
	if reason != "" && reason != SuspensionCommercial && reason != SuspensionSecurity && reason != SuspensionLegal {
		return fmt.Errorf("%w: unknown suspension reason %q", ErrInvalidLifecycle, reason)
	}
	if strings.TrimSpace(by) == "" || strings.TrimSpace(key) == "" || at.IsZero() {
		return fmt.Errorf("%w: requester, idempotency key and timestamp are required", ErrInvalidLifecycle)
	}
	return nil
}

func decisionsFor(reason SuspensionReason, active bool) []CapabilityDecision {
	out := make([]CapabilityDecision, 0, len(AllCapabilities))
	for _, capability := range AllCapabilities {
		allowed := active
		detail := "tenant is active"
		if !active {
			allowed = capability == CapabilityPayroll || capability == CapabilityLegal || capability == CapabilityAudit
			detail = "suspension denies this capability"
			if allowed {
				detail = "preserved for legally required access"
			}
			if reason == SuspensionSecurity && capability == CapabilityInteractive {
				allowed = false
				detail = "security suspension revokes interactive access"
			}
		}
		out = append(out, CapabilityDecision{Capability: capability, Allowed: allowed, Reason: detail})
	}
	return out
}

func pendingFor(action PendingWorkAction, items []PendingWorkItem) PendingWorkDisposition {
	out := PendingWorkDisposition{Action: action, Count: len(items)}
	for _, item := range items {
		if item.RequiredForAccess && strings.TrimSpace(item.ID) != "" {
			out.PreservedIDs = append(out.PreservedIDs, item.ID)
		}
	}
	sort.Strings(out.PreservedIDs)
	return out
}

func revokeSessions(ctx context.Context, sessions []SessionRef, revoker SessionRevoker, reason string, at time.Time, preserveRequired bool) ([]RevocationReceipt, error) {
	if len(sessions) == 0 {
		return nil, nil
	}
	if revoker == nil {
		return nil, fmt.Errorf("%w: session revocation port is required", ErrInvalidLifecycle)
	}
	ordered := append([]SessionRef(nil), sessions...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	out := make([]RevocationReceipt, 0, len(ordered))
	for _, ref := range ordered {
		if ref.ID == "" {
			return nil, fmt.Errorf("%w: session id is required", ErrInvalidLifecycle)
		}
		if preserveRequired && ref.RequiredForAccess {
			continue
		}
		if _, err := revoker.Revoke(ctx, ref.ID, "TENANT_"+string(reason)); err != nil {
			return nil, fmt.Errorf("tenant: revoke session %s: %w", ref.ID, err)
		}
		out = append(out, RevocationReceipt{SessionID: string(ref.ID), Reason: "TENANT_" + string(reason), At: at.UTC()})
	}
	return out, nil
}

func (l *Lifecycle) appendLocked(kind string, to TenantStatus, reason SuspensionReason, by string, at time.Time, decisions []CapabilityDecision, pending PendingWorkDisposition, revocations []RevocationReceipt) TransitionResult {
	event := LifecycleEvent{Kind: kind, Tenant: l.tenant, From: l.status, To: to, Reason: reason, RequestedBy: by, At: at.UTC(), Capabilities: cloneCapabilityDecisions(decisions), Pending: pending, Revocations: append([]RevocationReceipt(nil), revocations...)}
	event.Digest = digestLifecycleEvent(event)
	l.events = append(l.events, event)
	result := TransitionResult{Decision: "APPLY", Previous: l.status, Status: to, Capabilities: cloneCapabilityDecisions(decisions), Revocations: append([]RevocationReceipt(nil), revocations...), Pending: pending, Event: event}
	result.Event = cloneEvents([]LifecycleEvent{event})[0]
	l.status = to
	return result
}

func (l *Lifecycle) replayLocked(key string, at time.Time) (TransitionResult, bool, error) {
	if key != l.lastKey {
		return TransitionResult{}, false, nil
	}
	if len(l.events) == 0 || !l.events[len(l.events)-1].At.Equal(at.UTC()) {
		return TransitionResult{}, false, fmt.Errorf("%w: idempotency key was reused with different evidence", ErrLifecycleConflict)
	}
	out := cloneResult(l.last)
	out.Decision = "NOOP"
	return out, true, nil
}

func (l *Lifecycle) rememberLocked(key string, result TransitionResult) {
	l.lastKey, l.last = key, cloneResult(result)
}

func digestLifecycleEvent(event LifecycleEvent) string {
	event.Digest = ""
	b, _ := json.Marshal(event)
	sum := sha256.Sum256(append([]byte("hcmnext.tenant.LifecycleEvent/v1\x00"), b...))
	return hex.EncodeToString(sum[:])
}

func cloneCapabilityDecisions(in []CapabilityDecision) []CapabilityDecision {
	return append([]CapabilityDecision(nil), in...)
}
func cloneEvents(in []LifecycleEvent) []LifecycleEvent {
	out := make([]LifecycleEvent, len(in))
	copy(out, in)
	for i := range out {
		out[i].Capabilities = cloneCapabilityDecisions(out[i].Capabilities)
		out[i].Pending.PreservedIDs = append([]string(nil), out[i].Pending.PreservedIDs...)
		out[i].Revocations = append([]RevocationReceipt(nil), out[i].Revocations...)
	}
	return out
}
func cloneResult(in TransitionResult) TransitionResult {
	in.Capabilities = cloneCapabilityDecisions(in.Capabilities)
	in.Revocations = append([]RevocationReceipt(nil), in.Revocations...)
	in.Pending.PreservedIDs = append([]string(nil), in.Pending.PreservedIDs...)
	in.Event = cloneEvents([]LifecycleEvent{in.Event})[0]
	return in
}
