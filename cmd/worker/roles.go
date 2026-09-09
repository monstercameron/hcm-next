package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// WorkerRole is a role inside the shared worker binary. These are narrower
// than the attested process role (which remains workload.RoleWorker): a
// process may host more than one role, but a role never inherits another
// role's authority merely because they share a process.
type WorkerRole string

const (
	WorkerRoleCapabilityActivity WorkerRole = "capability-activity"
	WorkerRoleReconciliation     WorkerRole = "reconciliation"
	WorkerRoleRepair             WorkerRole = "repair"
)

// WorkerAction is the closed action vocabulary used by the in-process role
// authorizer. An absent role/action pair is a deny, not an implicit allow.
type WorkerAction string

const (
	WorkerActionResolveCapability  WorkerAction = "resolve-capability"
	WorkerActionRevalidateActivity WorkerAction = "revalidate-activity"
	WorkerActionExecuteActivity    WorkerAction = "execute-activity"
	WorkerActionDiagnose           WorkerAction = "diagnose-reconciliation"
	WorkerActionCreateRepairPlan   WorkerAction = "create-repair-plan"
	WorkerActionExecuteRepair      WorkerAction = "execute-repair"
	WorkerActionVerifyRepair       WorkerAction = "verify-repair"
)

var workerRoleActions = map[WorkerRole]map[WorkerAction]string{
	WorkerRoleCapabilityActivity: {
		WorkerActionResolveCapability:  "worker.capability_activity.resolve",
		WorkerActionRevalidateActivity: "worker.capability_activity.revalidate",
		WorkerActionExecuteActivity:    "worker.capability_activity.execute",
	},
	WorkerRoleReconciliation: {
		WorkerActionDiagnose:         "worker.reconciliation.diagnose",
		WorkerActionCreateRepairPlan: "worker.reconciliation.create_repair_plan",
	},
	WorkerRoleRepair: {
		WorkerActionExecuteRepair: "worker.repair.execute",
		WorkerActionVerifyRepair:  "worker.repair.verify",
	},
}

// WorkerRoleDecision is a stable, redaction-safe role authorization result.
type WorkerRoleDecision struct {
	Allowed bool
	Role    WorkerRole
	Action  WorkerAction
	RuleID  string
	Reason  string
}

func (d WorkerRoleDecision) Explain() string {
	return fmt.Sprintf("worker role=%s action=%s allowed=%t rule=%s reason=%s",
		d.Role, d.Action, d.Allowed, d.RuleID, d.Reason)
}

var (
	ErrInvalidWorkerRole = errors.New("worker: invalid role")
	ErrRoleDenied        = errors.New("worker: role action denied")
	ErrInvalidActivity   = errors.New("worker: invalid capability activity")
	ErrCapabilityMissing = errors.New("worker: capability version is not registered")
	ErrDescriptorStale   = errors.New("worker: immutable capability descriptor digest mismatch")
	ErrStaleActivity     = errors.New("worker: capability activity lease or descriptor is stale")
	ErrActivityAmbiguous = errors.New("worker: capability activity outcome is ambiguous")
	ErrStaleObservation  = errors.New("worker: reconciliation observation is stale")
	ErrInvalidRepairPlan = errors.New("worker: invalid repair plan")
	ErrRepairAmbiguous   = errors.New("worker: repair outcome is ambiguous")
	ErrRepairNotFresh    = errors.New("worker: repair closure lacks a fresh verification")
)

// AuthorizeWorkerRole applies the worker's deny-by-default sub-role policy.
func AuthorizeWorkerRole(role WorkerRole, action WorkerAction) (WorkerRoleDecision, error) {
	d := WorkerRoleDecision{Role: role, Action: action}
	if !role.Valid() {
		d.Reason = "unknown worker role"
		return d, fmt.Errorf("%w: %q", ErrInvalidWorkerRole, role)
	}
	if rule, ok := workerRoleActions[role][action]; ok {
		d.Allowed = true
		d.RuleID = rule
		return d, nil
	}
	d.Reason = "role does not own this action"
	return d, fmt.Errorf("%w: %s cannot %s", ErrRoleDenied, role, action)
}

func (r WorkerRole) Valid() bool {
	return r == WorkerRoleCapabilityActivity || r == WorkerRoleReconciliation || r == WorkerRoleRepair
}

// ParseWorkerRoles parses the comma-separated role configuration. It returns
// a sorted, duplicate-free list so the same deployment has the same workload
// order regardless of how operators wrote the flag.
func ParseWorkerRoles(raw string) ([]WorkerRole, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%w: no roles configured", ErrInvalidWorkerRole)
	}
	seen := make(map[WorkerRole]bool)
	roles := make([]WorkerRole, 0, 3)
	for _, part := range strings.Split(raw, ",") {
		role := WorkerRole(strings.TrimSpace(part))
		if !role.Valid() {
			return nil, fmt.Errorf("%w: %q", ErrInvalidWorkerRole, role)
		}
		if seen[role] {
			return nil, fmt.Errorf("%w: duplicate role %q", ErrInvalidWorkerRole, role)
		}
		seen[role] = true
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	return roles, nil
}

// workerRoleWorkloads gives each selected role its own run-group member and
// therefore its own shutdown/failure boundary. The semantic ports below are
// invoked by role consumers; an unconfigured consumer stays quiescent until
// application wiring supplies its queue, rather than inventing domain logic
// in this command package.
func workerRoleWorkloads(logger bootstrap.Logger, roles []WorkerRole) []bootstrap.Workload {
	workloads := make([]bootstrap.Workload, 0, len(roles))
	for _, role := range roles {
		role := role
		workloads = append(workloads, bootstrap.Workload{
			Name: "role-" + string(role),
			Run: func(ctx context.Context) error {
				logger.Info("worker.role_selected", "worker_role", string(role))
				<-ctx.Done()
				return nil
			},
		})
	}
	return workloads
}

// CapabilityDescriptorResolver is the application seam through which this
// command resolves capability descriptors (see internal/application).
type CapabilityDescriptorResolver = application.CapabilityDescriptorResolver

// ActivityLease is the execution lease/fence presented by a caller. The
// worker checks it before execution and lets the durable lease port recheck it
// under its own transaction when one is wired.
type ActivityLease struct {
	ID        string
	Fence     uint64
	ExpiresAt time.Time
}

func (l ActivityLease) Validate(at time.Time) error {
	if l.ID == "" || l.Fence == 0 || l.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: lease id, fence and expiry are required", ErrInvalidActivity)
	}
	if !l.ExpiresAt.After(at) {
		return fmt.Errorf("%w: lease expired at %s", ErrStaleActivity, l.ExpiresAt.UTC().Format(time.RFC3339Nano))
	}
	return nil
}

// ActivityLeaseVerifier is the durable fence port. A nil verifier is allowed
// for pure semantic tests, but the local lease validation still always runs.
type ActivityLeaseVerifier interface {
	VerifyActivityLease(context.Context, uuid.UUID, string, ActivityLease, time.Time) error
}

// CapabilityActivityRequest is the immutable input to one activity attempt.
type CapabilityActivityRequest struct {
	TenantID         uuid.UUID
	ActivityID       string
	Capability       application.CapabilityKey
	DescriptorDigest string
	IdempotencyKey   string
	Attempt          int
	Lease            ActivityLease
	Payload          any
}

func (r CapabilityActivityRequest) validate(at time.Time) error {
	if r.TenantID == uuid.Nil || r.ActivityID == "" || r.Capability.ID == "" || r.Capability.Version == 0 {
		return fmt.Errorf("%w: tenant, activity and exact capability identity are required", ErrInvalidActivity)
	}
	if r.DescriptorDigest == "" || r.IdempotencyKey == "" {
		return fmt.Errorf("%w: descriptor digest and idempotency key are required", ErrInvalidActivity)
	}
	if r.Attempt < 1 {
		return fmt.Errorf("%w: attempt must be positive", ErrInvalidActivity)
	}
	return r.Lease.Validate(at)
}

// CapabilityActivityExecution is what application wiring receives after all
// worker-owned guards pass. The executor owns domain semantics; cmd/worker
// only supplies the exact immutable descriptor and execution coordinates.
type CapabilityActivityExecution struct {
	Request    CapabilityActivityRequest
	Definition application.CapabilityDefinition
	Digest     string
}

type CapabilityActivityExecutor interface {
	ExecuteCapability(context.Context, CapabilityActivityExecution) (any, error)
}

type ActivityAttemptEvidence struct {
	TenantID         uuid.UUID
	ActivityID       string
	Capability       application.CapabilityKey
	DescriptorDigest string
	IdempotencyKey   string
	LeaseID          string
	Fence            uint64
	Attempt          int
	Outcome          string
	Route            string
	Reason           string
	OccurredAt       time.Time
}

func (e ActivityAttemptEvidence) Explain() string {
	return fmt.Sprintf("activity=%s capability=%s attempt=%d outcome=%s route=%s reason=%s",
		e.ActivityID, e.Capability, e.Attempt, e.Outcome, e.Route, e.Reason)
}

type ActivityEvidenceStore interface {
	RecordActivityAttempt(context.Context, ActivityAttemptEvidence) (string, error)
}

type ActivityAmbiguity struct {
	Request CapabilityActivityRequest
	Cause   error
}

type ActivityAmbiguityRouter interface {
	RouteActivityAmbiguity(context.Context, ActivityAmbiguity) error
}

type CapabilityActivity struct {
	Resolver  CapabilityDescriptorResolver
	Executor  CapabilityActivityExecutor
	Evidence  ActivityEvidenceStore
	Ambiguity ActivityAmbiguityRouter
	Fence     ActivityLeaseVerifier
	Now       func() time.Time
}

func (a CapabilityActivity) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

// Execute resolves, revalidates, fences, and executes exactly one activity.
// It deliberately does not retry: retry scheduling belongs to the immediate
// caller policy (WF-RUN-006), and an ambiguous effect is routed for
// observation/repair rather than blindly repeated.
func (a CapabilityActivity) Execute(ctx context.Context, req CapabilityActivityRequest) (any, string, error) {
	at := a.now()
	if _, err := AuthorizeWorkerRole(WorkerRoleCapabilityActivity, WorkerActionExecuteActivity); err != nil {
		return nil, "", err
	}
	if a.Resolver == nil || a.Executor == nil || a.Evidence == nil {
		return nil, "", fmt.Errorf("%w: resolver, executor and evidence store are required", ErrInvalidActivity)
	}
	if err := req.validate(at); err != nil {
		return nil, "", err
	}
	rec, ok := a.Resolver.ResolveCapability(ctx, req.Capability)
	if !ok {
		return nil, "", a.refuse(ctx, req, ErrCapabilityMissing, "capability version is not registered", at)
	}
	if rec.Definition.Key() != req.Capability || rec.Digest != req.DescriptorDigest {
		return nil, "", a.refuse(ctx, req, ErrDescriptorStale, "immutable descriptor digest mismatch", at)
	}
	if !rec.Definition.EffectClass.Valid() {
		return nil, "", a.refuse(ctx, req, ErrInvalidActivity, "descriptor effect class is invalid", at)
	}
	if err := req.Lease.Validate(at); err != nil {
		if a.Evidence != nil {
			_, _ = a.record(ctx, req, "REFUSED", "", err.Error(), at)
		}
		return nil, "", err
	}
	if a.Fence != nil {
		if err := a.Fence.VerifyActivityLease(ctx, req.TenantID, req.ActivityID, req.Lease, at); err != nil {
			if a.Evidence != nil {
				_, _ = a.record(ctx, req, "REFUSED", "", err.Error(), at)
			}
			return nil, "", err
		}
	}
	response, err := a.Executor.ExecuteCapability(ctx, CapabilityActivityExecution{Request: req, Definition: rec.Definition, Digest: rec.Digest})
	if err != nil {
		if errors.Is(err, ErrActivityAmbiguous) {
			if a.Ambiguity == nil {
				if _, evidenceErr := a.record(ctx, req, "AMBIGUOUS", "OBSERVATION_OR_REPAIR", err.Error(), at); evidenceErr != nil {
					return nil, "", evidenceErr
				}
				return nil, "", err
			}
			if routeErr := a.Ambiguity.RouteActivityAmbiguity(ctx, ActivityAmbiguity{Request: req, Cause: err}); routeErr != nil {
				return nil, "", fmt.Errorf("%w: route ambiguity: %v", ErrActivityAmbiguous, routeErr)
			}
			if _, evidenceErr := a.record(ctx, req, "AMBIGUOUS", "OBSERVATION_OR_REPAIR", err.Error(), at); evidenceErr != nil {
				return nil, "", evidenceErr
			}
			return nil, "", err
		}
		return nil, "", a.refuse(ctx, req, err, err.Error(), at)
	}
	evidenceID, evidenceErr := a.record(ctx, req, "SUCCEEDED", "", "", at)
	if evidenceErr != nil {
		return nil, "", evidenceErr
	}
	return response, evidenceID, nil
}

func (a CapabilityActivity) record(ctx context.Context, req CapabilityActivityRequest, outcome, route, reason string, at time.Time) (string, error) {
	return a.Evidence.RecordActivityAttempt(ctx, ActivityAttemptEvidence{
		TenantID: req.TenantID, ActivityID: req.ActivityID, Capability: req.Capability,
		DescriptorDigest: req.DescriptorDigest, IdempotencyKey: req.IdempotencyKey,
		LeaseID: req.Lease.ID, Fence: req.Lease.Fence, Attempt: req.Attempt,
		Outcome: outcome, Route: route, Reason: reason, OccurredAt: at,
	})
}

func (a CapabilityActivity) refuse(ctx context.Context, req CapabilityActivityRequest, cause error, reason string, at time.Time) error {
	if a.Evidence == nil {
		return cause
	}
	_, evidenceErr := a.record(ctx, req, "REFUSED", "", reason, at)
	if evidenceErr != nil {
		return fmt.Errorf("%w: record refusal evidence: %v", cause, evidenceErr)
	}
	return fmt.Errorf("%w: %s", cause, reason)
}

// RepairObservation is the source observation that a reconciliation role
// used. It is intentionally digest-only: repair workers never receive a
// second copy of protected domain payload merely to authorize a plan.
type RepairObservation struct {
	TenantID    uuid.UUID
	ResourceKey string
	Digest      string
	ObservedAt  time.Time
	FreshUntil  time.Time
}

func (o RepairObservation) validate(at time.Time) error {
	if o.TenantID == uuid.Nil || o.ResourceKey == "" || o.Digest == "" || o.ObservedAt.IsZero() || o.FreshUntil.IsZero() {
		return fmt.Errorf("%w: observation coordinates, digest and freshness are required", ErrStaleObservation)
	}
	if !o.FreshUntil.After(o.ObservedAt) || !o.FreshUntil.After(at) {
		return fmt.Errorf("%w: observation freshness expired", ErrStaleObservation)
	}
	return nil
}

type RepairPlan struct {
	PlanID            string
	TenantID          uuid.UUID
	ResourceKey       string
	ExpectedDigest    string
	ObservationDigest string
	ObservationAt     time.Time
	CreatedBy         string
	CreatedAt         time.Time
	IdempotencyKey    string
	Digest            string
}

func (p RepairPlan) Validate(at time.Time) error {
	if p.PlanID == "" || p.TenantID == uuid.Nil || p.ResourceKey == "" || p.ExpectedDigest == "" || p.ObservationDigest == "" || p.CreatedBy == "" || p.IdempotencyKey == "" {
		return ErrInvalidRepairPlan
	}
	if p.CreatedAt.IsZero() || p.CreatedAt.After(at) || p.Digest == "" {
		return ErrInvalidRepairPlan
	}
	if p.ExpectedDigest == p.ObservationDigest {
		return fmt.Errorf("%w: plan does not describe drift", ErrInvalidRepairPlan)
	}
	if p.Digest != digestRepairPlan(p) {
		return fmt.Errorf("%w: immutable plan digest mismatch", ErrInvalidRepairPlan)
	}
	return nil
}

func digestRepairPlan(p RepairPlan) string {
	h := sha256.New()
	for _, part := range []string{p.PlanID, p.TenantID.String(), p.ResourceKey, p.ExpectedDigest, p.ObservationDigest, p.ObservationAt.UTC().Format(time.RFC3339Nano), p.CreatedBy, p.CreatedAt.UTC().Format(time.RFC3339Nano), p.IdempotencyKey} {
		fmt.Fprintf(h, "%d:", len(part))
		_, _ = h.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

type ReconciliationPlanStore interface {
	StoreRepairPlan(context.Context, RepairPlan) error
}

type ReconciliationRequest struct {
	TenantID       uuid.UUID
	ResourceKey    string
	ExpectedDigest string
	RequestedBy    string
	IdempotencyKey string
}

type ReconciliationSource interface {
	ObserveReconciliation(context.Context, ReconciliationRequest) (RepairObservation, error)
}

type ReconciliationRole struct {
	Plans  ReconciliationPlanStore
	Source ReconciliationSource
	Now    func() time.Time
}

// Diagnose reads the current authoritative observation through the
// application-owned source and immediately hands it to the immutable-plan
// path. The source is read-only; only CreateRepairPlan may persist a plan.
func (r ReconciliationRole) Diagnose(ctx context.Context, req ReconciliationRequest) (RepairPlan, error) {
	if _, err := AuthorizeWorkerRole(WorkerRoleReconciliation, WorkerActionDiagnose); err != nil {
		return RepairPlan{}, err
	}
	if r.Source == nil {
		return RepairPlan{}, fmt.Errorf("%w: reconciliation source is required", ErrInvalidRepairPlan)
	}
	observation, err := r.Source.ObserveReconciliation(ctx, req)
	if err != nil {
		return RepairPlan{}, err
	}
	return r.CreateRepairPlan(ctx, observation, req.ExpectedDigest, req.RequestedBy, req.IdempotencyKey)
}

func (r ReconciliationRole) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// CreateRepairPlan turns a fresh diagnosed mismatch into one immutable plan.
// Only the reconciliation role can call this path; the repair role has no
// action in the role matrix that creates or mutates a plan.
func (r ReconciliationRole) CreateRepairPlan(ctx context.Context, observation RepairObservation, expectedDigest, requestedBy, idempotencyKey string) (RepairPlan, error) {
	if _, err := AuthorizeWorkerRole(WorkerRoleReconciliation, WorkerActionCreateRepairPlan); err != nil {
		return RepairPlan{}, err
	}
	at := r.now()
	if err := observation.validate(at); err != nil {
		return RepairPlan{}, err
	}
	if expectedDigest == "" || requestedBy == "" || idempotencyKey == "" || expectedDigest == observation.Digest {
		return RepairPlan{}, fmt.Errorf("%w: expected digest, requester, idempotency key and drift are required", ErrInvalidRepairPlan)
	}
	planID := "repair-" + strings.ReplaceAll(observation.TenantID.String()+"-"+observation.ResourceKey+"-"+idempotencyKey, " ", "_")
	plan := RepairPlan{PlanID: planID, TenantID: observation.TenantID, ResourceKey: observation.ResourceKey,
		ExpectedDigest: expectedDigest, ObservationDigest: observation.Digest, ObservationAt: observation.ObservedAt.UTC(),
		CreatedBy: requestedBy, CreatedAt: at, IdempotencyKey: idempotencyKey}
	plan.Digest = digestRepairPlan(plan)
	if err := plan.Validate(at); err != nil {
		return RepairPlan{}, err
	}
	if r.Plans == nil {
		return plan, nil
	}
	if err := r.Plans.StoreRepairPlan(ctx, plan); err != nil {
		return RepairPlan{}, err
	}
	return plan, nil
}

type RepairApproval struct {
	ApprovedBy string
	ApprovedAt time.Time
	PlanDigest string
}

type RepairVerification struct {
	Digest     string
	VerifiedAt time.Time
	Fresh      bool
}

type RepairVerifier interface {
	VerifyRepairFresh(context.Context, RepairPlan) (RepairVerification, error)
}

type RepairExecution struct {
	Plan       RepairPlan
	Approval   RepairApproval
	ExecutorID string
}

type RepairExecutor interface {
	ExecuteRepair(context.Context, RepairExecution) (string, error)
}

type RepairReceipt struct {
	PlanID        string
	PlanDigest    string
	EffectID      string
	VerifiedAt    time.Time
	ReceiptDigest string
}

type RepairReceiptStore interface {
	FindRepairReceipt(context.Context, string, string) (RepairReceipt, bool, error)
	StoreRepairReceipt(context.Context, RepairReceipt) error
}

type RepairRole struct {
	Verifier RepairVerifier
	Executor RepairExecutor
	Receipts RepairReceiptStore
	// ExecutorID is the attested sub-role subject executing this repair. It is
	// separate from both the requester in the plan and the human approver.
	ExecutorID string
	Now        func() time.Time
}

func (r RepairRole) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// ExecuteRepair enforces dual control, a fresh observation before and after
// the effect, and idempotent receipt replay. Approval is never inferred from
// plan authorship and a reconciliation identity cannot invoke this method.
func (r RepairRole) ExecuteRepair(ctx context.Context, plan RepairPlan, approval RepairApproval) (RepairReceipt, error) {
	if _, err := AuthorizeWorkerRole(WorkerRoleRepair, WorkerActionExecuteRepair); err != nil {
		return RepairReceipt{}, err
	}
	at := r.now()
	if err := plan.Validate(at); err != nil {
		return RepairReceipt{}, err
	}
	if r.ExecutorID == "" || approval.ApprovedBy == "" || approval.ApprovedBy == plan.CreatedBy || approval.ApprovedBy == r.ExecutorID || approval.PlanDigest != plan.Digest || approval.ApprovedAt.IsZero() || approval.ApprovedAt.After(at) {
		return RepairReceipt{}, fmt.Errorf("%w: approval must be distinct and bound to the immutable plan", ErrInvalidRepairPlan)
	}
	if r.Receipts != nil {
		if receipt, found, err := r.Receipts.FindRepairReceipt(ctx, plan.PlanID, plan.IdempotencyKey); err != nil {
			return RepairReceipt{}, err
		} else if found {
			return receipt, nil
		}
	}
	if r.Verifier == nil || r.Executor == nil {
		return RepairReceipt{}, fmt.Errorf("%w: verifier and executor are required", ErrInvalidRepairPlan)
	}
	before, err := r.Verifier.VerifyRepairFresh(ctx, plan)
	if err != nil || !validFreshVerification(before, at) || before.Digest != plan.ObservationDigest {
		if err != nil {
			return RepairReceipt{}, err
		}
		return RepairReceipt{}, fmt.Errorf("%w: pre-execution observation changed", ErrStaleObservation)
	}
	effectID, err := r.Executor.ExecuteRepair(ctx, RepairExecution{Plan: plan, Approval: approval, ExecutorID: r.ExecutorID})
	if err != nil {
		if errors.Is(err, ErrRepairAmbiguous) {
			return RepairReceipt{}, fmt.Errorf("%w: execute: %v", ErrRepairAmbiguous, err)
		}
		return RepairReceipt{}, err
	}
	after, err := r.Verifier.VerifyRepairFresh(ctx, plan)
	if err != nil {
		return RepairReceipt{}, err
	}
	if !validFreshVerification(after, at) || after.Digest != plan.ExpectedDigest {
		return RepairReceipt{}, fmt.Errorf("%w: post-execution digest=%s want=%s", ErrRepairNotFresh, after.Digest, plan.ExpectedDigest)
	}
	receipt := RepairReceipt{PlanID: plan.PlanID, PlanDigest: plan.Digest, EffectID: effectID, VerifiedAt: after.VerifiedAt}
	receipt.ReceiptDigest = digestRepairReceipt(receipt)
	if r.Receipts != nil {
		if err := r.Receipts.StoreRepairReceipt(ctx, receipt); err != nil {
			return RepairReceipt{}, err
		}
	}
	return receipt, nil
}

func validFreshVerification(v RepairVerification, at time.Time) bool {
	return v.Fresh && v.Digest != "" && !v.VerifiedAt.IsZero() && !v.VerifiedAt.After(at)
}

func digestRepairReceipt(r RepairReceipt) string {
	h := sha256.Sum256([]byte(r.PlanID + "\x00" + r.PlanDigest + "\x00" + r.EffectID + "\x00" + r.VerifiedAt.UTC().Format(time.RFC3339Nano)))
	return "sha256:" + hex.EncodeToString(h[:])
}
