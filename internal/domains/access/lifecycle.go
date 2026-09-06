package access

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

// LifecycleVersion is the version of the access decision contract.
const LifecycleVersion = 1

var (
	ErrInvalidLifecycle      = errors.New("access: invalid lifecycle request")
	ErrReapprovalRequired    = errors.New("access: current approval or risk is stale")
	ErrReplanRequired        = errors.New("access: current entitlement or proposal is stale")
	ErrDecisionConflict      = errors.New("access: decision conflicts with an existing decision")
	ErrInsufficientApproval  = errors.New("access: approval quorum is not satisfied")
	ErrCertificationMismatch = errors.New("access: certification scope is not the reviewed scope")
	ErrIdempotencyConflict   = errors.New("access: idempotency key was reused with different input")
)

// LifecycleState is the closed state vocabulary for an access request.
type LifecycleState string

const (
	StateRequested LifecycleState = "REQUESTED"
	StateApproved  LifecycleState = "APPROVED"
	StateGranted   LifecycleState = "GRANTED"
	StateRevoked   LifecycleState = "REVOKED"
	StateCertified LifecycleState = "CERTIFIED"
	StateRejected  LifecycleState = "REJECTED"
)

func (s LifecycleState) Valid() bool {
	switch s {
	case StateRequested, StateApproved, StateGranted, StateRevoked, StateCertified, StateRejected:
		return true
	default:
		return false
	}
}

// DecisionAction identifies the access-owned intended state transition.
type DecisionAction string

const (
	ActionApprove DecisionAction = "APPROVE"
	ActionGrant   DecisionAction = "GRANT"
	ActionRevoke  DecisionAction = "REVOKE"
	ActionCertify DecisionAction = "CERTIFY"
)

func (a DecisionAction) Valid() bool {
	switch a {
	case ActionApprove, ActionGrant, ActionRevoke, ActionCertify:
		return true
	default:
		return false
	}
}

// LifecycleSnapshot is the exact current-truth fence used by a decision.
// Revisions are opaque references; access does not interpret another domain's
// revision format.
type LifecycleSnapshot struct {
	RiskRevision        string
	ManagerRevision     string
	EntitlementRevision string
}

func (s LifecycleSnapshot) complete() bool {
	return strings.TrimSpace(s.RiskRevision) != "" &&
		strings.TrimSpace(s.ManagerRevision) != "" &&
		strings.TrimSpace(s.EntitlementRevision) != ""
}

func (s LifecycleSnapshot) equal(other LifecycleSnapshot) bool { return s == other }

// AccessRequestInput is the caller's proposed request. It contains no
// caller-selected authority; the lifecycle binds the supplied values to a
// server-resolved proposal and current control snapshot.
type AccessRequestInput struct {
	RequestID      string
	Tenant         string
	Requester      string
	Beneficiary    string
	Application    string
	AccountID      string
	EntitlementID  string
	Scope          string
	Purpose        string
	Justification  string
	Risk           RiskClass
	ProposalDigest string
	Snapshot       LifecycleSnapshot
	RequestedAt    time.Time
	IdempotencyKey string
}

// AccessRequest is the immutable request fact recorded before approval.
type AccessRequest struct {
	AccessRequestInput
	State          LifecycleState
	RequiredQuorum int
	Digest         string
}

// Approval is one proposal-bound approval vote. A provider result is not an
// approval and cannot be used in its place.
type Approval struct {
	ApprovalID     string
	RequestID      string
	Approver       string
	Role           string
	ProposalDigest string
	Snapshot       LifecycleSnapshot
	Approved       bool
	At             time.Time
}

// DecisionRequest asks Access to append one intended state transition.
type DecisionRequest struct {
	Request         AccessRequest
	Action          DecisionAction
	Actor           string
	CurrentSnapshot LifecycleSnapshot
	Approvals       []Approval
	CertifiedScope  string
	At              time.Time
	IdempotencyKey  string
}

// Decision is the authoritative, provider-independent access outcome. The
// EffectKey is an idempotency key for a later external effect; it is not proof
// that a provider accepted or performed that effect.
type Decision struct {
	DecisionID     string
	RequestID      string
	Tenant         string
	Action         DecisionAction
	State          LifecycleState
	Requester      string
	Beneficiary    string
	Application    string
	AccountID      string
	EntitlementID  string
	Scope          string
	Risk           RiskClass
	ProposalDigest string
	Snapshot       LifecycleSnapshot
	ApprovalIDs    []string
	EffectKey      string
	At             time.Time
	Digest         string
}

func requiredQuorum(risk RiskClass) int {
	if risk == RiskPrivileged {
		return 2
	}
	return 1
}

func validateRequest(in AccessRequestInput) error {
	fields := []struct{ name, value string }{
		{"request_id", in.RequestID}, {"tenant", in.Tenant}, {"requester", in.Requester},
		{"beneficiary", in.Beneficiary}, {"application", in.Application}, {"account_id", in.AccountID},
		{"entitlement_id", in.EntitlementID}, {"scope", in.Scope}, {"purpose", in.Purpose},
		{"justification", in.Justification}, {"proposal_digest", in.ProposalDigest}, {"idempotency_key", in.IdempotencyKey},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" || strings.TrimSpace(field.value) != field.value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidLifecycle, field.name)
		}
	}
	if !in.Risk.Valid() {
		return fmt.Errorf("%w: risk is not declared", ErrInvalidLifecycle)
	}
	if !in.Snapshot.complete() {
		return fmt.Errorf("%w: risk, manager and entitlement revisions are required", ErrInvalidLifecycle)
	}
	if in.RequestedAt.IsZero() {
		return fmt.Errorf("%w: requested_at is required", ErrInvalidLifecycle)
	}
	return nil
}

// RequestAccess validates and freezes the access request. It performs no
// external work and does not infer any grant from an incomplete request.
func RequestAccess(in AccessRequestInput) (AccessRequest, error) {
	if err := validateRequest(in); err != nil {
		return AccessRequest{}, err
	}
	req := AccessRequest{AccessRequestInput: in, State: StateRequested, RequiredQuorum: requiredQuorum(in.Risk)}
	req.Digest = digestRequest(req)
	return req, nil
}

func validateApproval(req AccessRequest, approval Approval) error {
	if strings.TrimSpace(approval.ApprovalID) == "" || strings.TrimSpace(approval.RequestID) == "" ||
		strings.TrimSpace(approval.Approver) == "" || strings.TrimSpace(approval.Role) == "" {
		return fmt.Errorf("%w: approval identity is incomplete", ErrInvalidLifecycle)
	}
	if approval.RequestID != req.RequestID || approval.ProposalDigest != req.ProposalDigest ||
		!approval.Snapshot.equal(req.Snapshot) {
		return ErrReapprovalRequired
	}
	if approval.Approver == req.Requester || approval.Approver == req.Beneficiary {
		return fmt.Errorf("%w: requester and beneficiary may not approve their own access", ErrInvalidLifecycle)
	}
	if approval.At.IsZero() {
		return fmt.Errorf("%w: approval time is required", ErrInvalidLifecycle)
	}
	if !approval.Approved {
		return fmt.Errorf("%w: approval %s is not approved", ErrInsufficientApproval, approval.ApprovalID)
	}
	return nil
}

func validateDecision(in DecisionRequest) error {
	if in.Request.State != StateRequested && in.Request.State != StateApproved && in.Request.State != StateGranted {
		return fmt.Errorf("%w: request is not actionable", ErrDecisionConflict)
	}
	if !in.Action.Valid() || strings.TrimSpace(in.Actor) == "" || in.At.IsZero() || strings.TrimSpace(in.IdempotencyKey) == "" {
		return fmt.Errorf("%w: action, actor, time and idempotency key are required", ErrInvalidLifecycle)
	}
	if !in.CurrentSnapshot.complete() {
		return fmt.Errorf("%w: current control snapshot is incomplete", ErrReapprovalRequired)
	}
	if !in.CurrentSnapshot.equal(in.Request.Snapshot) {
		return ErrReplanRequired
	}
	if in.Action == ActionCertify && in.CertifiedScope != in.Request.Scope {
		return ErrCertificationMismatch
	}
	seen := make(map[string]struct{}, len(in.Approvals))
	for _, approval := range in.Approvals {
		if err := validateApproval(in.Request, approval); err != nil {
			return err
		}
		if _, ok := seen[approval.Approver]; ok {
			return fmt.Errorf("%w: approver cannot fill two approval slots", ErrInvalidLifecycle)
		}
		seen[approval.Approver] = struct{}{}
	}
	if in.Action == ActionApprove || in.Action == ActionGrant || in.Action == ActionCertify {
		if len(seen) < in.Request.RequiredQuorum {
			return fmt.Errorf("%w: need %d distinct approvers, got %d", ErrInsufficientApproval, in.Request.RequiredQuorum, len(seen))
		}
	}
	return nil
}

// Decide appends the intended access decision only after all current fences,
// risk and separation-of-duties checks pass.
func Decide(in DecisionRequest) (Decision, error) {
	if err := validateDecision(in); err != nil {
		return Decision{}, err
	}
	state := StateApproved
	switch in.Action {
	case ActionGrant:
		state = StateGranted
	case ActionRevoke:
		state = StateRevoked
	case ActionCertify:
		state = StateCertified
	}
	ids := make([]string, 0, len(in.Approvals))
	for _, approval := range in.Approvals {
		ids = append(ids, approval.ApprovalID)
	}
	sort.Strings(ids)
	decision := Decision{
		DecisionID: "access-decision/" + in.Request.RequestID + "/" + string(in.Action),
		RequestID:  in.Request.RequestID, Tenant: in.Request.Tenant, Action: in.Action, State: state,
		Requester: in.Request.Requester, Beneficiary: in.Request.Beneficiary, Application: in.Request.Application,
		AccountID: in.Request.AccountID, EntitlementID: in.Request.EntitlementID, Scope: in.Request.Scope,
		Risk: in.Request.Risk, ProposalDigest: in.Request.ProposalDigest, Snapshot: in.Request.Snapshot,
		ApprovalIDs: ids, EffectKey: effectKey(in.Request, in.Action), At: in.At.UTC(),
	}
	decision.Digest = digestDecision(decision)
	return decision, nil
}

// AccessDecisionLifecycle is a concurrency-safe in-memory append-only
// coordinator for pure callers and tests. A database adapter may persist the
// returned immutable request, approvals and decisions without changing the
// decision rules here.
type AccessDecisionLifecycle struct {
	mu            sync.Mutex
	requests      map[string]AccessRequest
	decisions     map[string]Decision
	idempotencies map[string]string
}

// NewAccessDecisionLifecycle constructs an empty lifecycle coordinator.
func NewAccessDecisionLifecycle() *AccessDecisionLifecycle {
	return &AccessDecisionLifecycle{requests: make(map[string]AccessRequest), decisions: make(map[string]Decision), idempotencies: make(map[string]string)}
}

// SubmitRequest records a validated request and replays an identical retry.
func (l *AccessDecisionLifecycle) SubmitRequest(in AccessRequestInput) (AccessRequest, error) {
	if l == nil {
		return AccessRequest{}, ErrInvalidLifecycle
	}
	req, err := RequestAccess(in)
	if err != nil {
		return AccessRequest{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if prior, ok := l.requests[req.RequestID]; ok {
		if prior.Digest != req.Digest {
			return AccessRequest{}, ErrIdempotencyConflict
		}
		return prior, nil
	}
	l.requests[req.RequestID] = req
	return req, nil
}

// AppendDecision records one authoritative decision and replays an identical
// idempotent effect without contacting a provider.
func (l *AccessDecisionLifecycle) AppendDecision(in DecisionRequest) (Decision, error) {
	if l == nil {
		return Decision{}, ErrInvalidLifecycle
	}
	decision, err := Decide(in)
	if err != nil {
		return Decision{}, err
	}
	key := in.Request.Tenant + "\x00" + in.IdempotencyKey
	l.mu.Lock()
	defer l.mu.Unlock()
	if priorID, ok := l.idempotencies[key]; ok {
		prior := l.decisions[priorID]
		if prior.Digest != decision.Digest {
			return Decision{}, ErrIdempotencyConflict
		}
		return prior, nil
	}
	if prior, ok := l.decisions[decision.DecisionID]; ok && prior.Digest != decision.Digest {
		return Decision{}, ErrDecisionConflict
	}
	l.decisions[decision.DecisionID] = decision
	l.idempotencies[key] = decision.DecisionID
	return decision, nil
}

// ApproveAccess, GrantEntitlement, RevokeEntitlement and CertifyAccess are
// named lifecycle entry points used by workflow adapters.
func (l *AccessDecisionLifecycle) ApproveAccess(in DecisionRequest) (Decision, error) {
	in.Action = ActionApprove
	return l.AppendDecision(in)
}
func (l *AccessDecisionLifecycle) GrantEntitlement(in DecisionRequest) (Decision, error) {
	in.Action = ActionGrant
	return l.AppendDecision(in)
}
func (l *AccessDecisionLifecycle) RevokeEntitlement(in DecisionRequest) (Decision, error) {
	in.Action = ActionRevoke
	return l.AppendDecision(in)
}
func (l *AccessDecisionLifecycle) CertifyAccess(in DecisionRequest) (Decision, error) {
	in.Action = ActionCertify
	return l.AppendDecision(in)
}

func digestRequest(req AccessRequest) string {
	return digestParts("access.request.v1", req.RequestID, req.Tenant, req.Requester, req.Beneficiary, req.Application,
		req.AccountID, req.EntitlementID, req.Scope, req.Purpose, req.Justification, req.Risk.String(), req.ProposalDigest,
		req.Snapshot.RiskRevision, req.Snapshot.ManagerRevision, req.Snapshot.EntitlementRevision, req.IdempotencyKey, req.RequestedAt.UTC().Format(time.RFC3339Nano))
}
func effectKey(req AccessRequest, action DecisionAction) string {
	return digestParts("access.effect.v1", req.Digest, string(action))
}
func digestDecision(d Decision) string {
	ids := append([]string(nil), d.ApprovalIDs...)
	sort.Strings(ids)
	return digestParts("access.decision.v1", d.DecisionID, d.RequestID, d.Tenant, string(d.Action), string(d.State), d.Requester,
		d.Beneficiary, d.Application, d.AccountID, d.EntitlementID, d.Scope, d.Risk.String(), d.ProposalDigest,
		d.Snapshot.RiskRevision, d.Snapshot.ManagerRevision, d.Snapshot.EntitlementRevision, strings.Join(ids, "\x00"), d.EffectKey, d.At.Format(time.RFC3339Nano))
}
func digestParts(profile string, parts ...string) string {
	h := sha256.New()
	for _, part := range append([]string{profile}, parts...) {
		fmt.Fprintf(h, "%d:", len(part))
		_, _ = h.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Explain returns a redaction-safe lifecycle summary.
func (d Decision) Explain() string {
	return fmt.Sprintf("access decision action=%s state=%s risk=%s approvals=%d effect=%s digest=%s", d.Action, d.State, d.Risk, len(d.ApprovalIDs), d.EffectKey, d.Digest)
}

// Explain is the package-level explanation entry point.
func Explain(d Decision) string { return d.Explain() }
