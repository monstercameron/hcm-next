// Package repair owns the execution-time boundary for a RepairPlan.
//
// The P1A domain plan remains a non-executable recommendation. This package
// is the P1B kernel contract that revalidates that recommendation against
// current evidence and issues a separately fenced, idempotent repair attempt.
package repair

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	domainrepair "github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
)

// Version is the REPAIR-002 contract version.
func Version() int { return 1 }

// Status is the typed result of repair-plan revalidation.
type Status string

const (
	StatusReady              Status = "READY"
	StatusNoLongerRequired   Status = "NO_LONGER_REQUIRED"
	StatusReplanRequired     Status = "REPLAN_REQUIRED"
	StatusReapprovalRequired Status = "REAPPROVAL_REQUIRED"
	StatusBlocked            Status = "BLOCKED"
	StatusUnknown            Status = "UNKNOWN"
)

func (s Status) Valid() bool {
	switch s {
	case StatusReady, StatusNoLongerRequired, StatusReplanRequired,
		StatusReapprovalRequired, StatusBlocked, StatusUnknown:
		return true
	default:
		return false
	}
}

var (
	ErrInvalidPlan      = errors.New("repair: invalid plan")
	ErrInvalidEvidence  = errors.New("repair: invalid current evidence")
	ErrContextCancelled = errors.New("repair: context cancelled")
	ErrFenceConflict    = errors.New("repair: execution fence conflicts with an existing attempt")
)

// Step is one bounded corrective effect. EffectKey is the immutable semantic
// identity of the failed effect selected for redrive; it is not regenerated
// for a repair attempt.
type Step struct {
	Ordinal          int
	EffectKey        string
	EffectRef        string
	Target           string
	ExpectedVersion  string
	MaxAttempts      int
	RequiresApproval bool
	Blocked          bool
	Preconditions    map[string]string
}

func (s Step) validate() error {
	if s.Ordinal < 1 || strings.TrimSpace(s.EffectKey) == "" || strings.TrimSpace(s.EffectRef) == "" || strings.TrimSpace(s.Target) == "" {
		return fmt.Errorf("%w: step identity is incomplete", ErrInvalidPlan)
	}
	if s.MaxAttempts < 1 || s.MaxAttempts > 3 {
		return fmt.Errorf("%w: step %d attempt budget is %d", ErrInvalidPlan, s.Ordinal, s.MaxAttempts)
	}
	if s.ExpectedVersion == "" {
		return fmt.Errorf("%w: step %d has no expected target version", ErrInvalidPlan, s.Ordinal)
	}
	return nil
}

// RepairPlan is an immutable-at-boundary execution snapshot. Its digest is
// supplied by the durable plan record; this type never changes the original
// business transaction or invents a new semantic idempotency identity.
type RepairPlan struct {
	ID                  string
	Digest              string
	FindingDigest       string
	ObservationDigest   string
	AuthorityPolicy     string
	MappingVersion      string
	CredentialRef       string
	TargetVersion       string
	OriginalSemanticKey string
	FailedEffectKey     string
	ApprovalDigest      string
	RequiresApproval    bool
	Steps               []Step
}

// PlanBinding supplies the execution-only facts that are intentionally not
// part of the P1A non-executable domain plan: the failed operation identity,
// its original semantic key, and the current connector binding.
type PlanBinding struct {
	ObservationDigest   string
	AuthorityPolicy     string
	MappingVersion      string
	CredentialRef       string
	TargetVersion       string
	OriginalSemanticKey string
	FailedEffectKey     string
	ApprovalDigest      string
}

// FromDomainPlan adapts the canonical P1A plan into an execution snapshot.
// It does not set the domain plan executable; it merely carries its immutable
// digest and step identities into the separately governed P1B boundary.
func FromDomainPlan(plan domainrepair.RepairPlan, binding PlanBinding) (RepairPlan, error) {
	if err := plan.Validate(); err != nil {
		return RepairPlan{}, fmt.Errorf("repair: domain plan: %w", err)
	}
	steps := make([]Step, 0, len(plan.Steps))
	for _, source := range plan.Steps {
		effectRef := source.IdempotencyKey
		if len(source.WriteSet) > 0 && source.WriteSet[0] != "" {
			effectRef = source.WriteSet[0]
		}
		steps = append(steps, Step{
			Ordinal: source.Ordinal, EffectKey: source.IdempotencyKey, EffectRef: effectRef,
			Target: source.Target.System + ":" + string(source.Target.Field), ExpectedVersion: binding.TargetVersion,
			MaxAttempts: source.MaxAttempts, RequiresApproval: source.RequiresApproval,
			Blocked: source.Action == domainrepair.ActionHumanReview || source.Action == domainrepair.ActionMappingReview,
		})
	}
	adapted := RepairPlan{
		ID: plan.ID, Digest: plan.Digest, FindingDigest: plan.DiffDigest,
		ObservationDigest: binding.ObservationDigest, AuthorityPolicy: binding.AuthorityPolicy,
		MappingVersion: binding.MappingVersion, CredentialRef: binding.CredentialRef,
		TargetVersion: binding.TargetVersion, OriginalSemanticKey: binding.OriginalSemanticKey,
		FailedEffectKey: binding.FailedEffectKey, ApprovalDigest: binding.ApprovalDigest,
		RequiresApproval: plan.RequiresApproval, Steps: steps,
	}
	if err := adapted.Validate(); err != nil {
		return RepairPlan{}, err
	}
	return adapted, nil
}

func (p RepairPlan) Validate() error {
	for field, value := range map[string]string{
		"id": p.ID, "digest": p.Digest, "finding_digest": p.FindingDigest,
		"observation_digest": p.ObservationDigest, "authority_policy": p.AuthorityPolicy,
		"mapping_version": p.MappingVersion, "credential_ref": p.CredentialRef,
		"target_version": p.TargetVersion, "original_semantic_key": p.OriginalSemanticKey,
		"failed_effect_key": p.FailedEffectKey,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidPlan, field)
		}
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("%w: no repair steps", ErrInvalidPlan)
	}
	seen := make(map[string]bool, len(p.Steps))
	found := false
	for i, step := range p.Steps {
		if err := step.validate(); err != nil {
			return err
		}
		if step.Ordinal != i+1 {
			return fmt.Errorf("%w: step order is not contiguous", ErrInvalidPlan)
		}
		if seen[step.EffectKey] {
			return fmt.Errorf("%w: duplicate effect key %q", ErrInvalidPlan, step.EffectKey)
		}
		seen[step.EffectKey] = true
		if step.EffectKey == p.FailedEffectKey {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%w: failed effect %q is not in the plan", ErrInvalidPlan, p.FailedEffectKey)
	}
	if p.RequiresApproval && p.ApprovalDigest == "" {
		return fmt.Errorf("%w: approval digest is required", ErrInvalidPlan)
	}
	return nil
}

// CurrentEvidence is the current, independently loaded truth used at the
// execution boundary. Empty required values are unknown, not permission to
// proceed.
type CurrentEvidence struct {
	PlanDigest             string
	FindingDigest          string
	ObservationDigest      string
	AuthorityPolicy        string
	MappingVersion         string
	CredentialRef          string
	TargetVersion          string
	ApprovalDigest         string
	ApprovalValid          bool
	ApprovalExpiresAt      time.Time
	TargetAlreadySatisfied bool
	SupersedingTransaction string
	Unknowns               []string
	Facts                  map[string]string
}

// RevalidationRequest is evaluated immediately before a repair effect. Now
// is supplied by the caller so this kernel remains deterministic and pure.
type RevalidationRequest struct {
	Plan    RepairPlan
	Current CurrentEvidence
	Now     time.Time
	Actor   string
}

// Fence is separate from the parent transaction fence. The original semantic
// key is retained for provider idempotency while FenceKey protects this repair
// execution record from duplicate admission.
type Fence struct {
	FenceID             string
	FenceKey            string
	PlanDigest          string
	OriginalSemanticKey string
	FailedEffectKey     string
	IssuedAt            time.Time
}

// Result is the evidence-backed revalidation answer. A non-READY result has
// no fence and therefore cannot cause a corrective effect.
type Result struct {
	Status       Status
	PlanDigest   string
	Reason       string
	EvidenceHash string
	Fence        Fence
}

// Admitter is the narrow execution-time admission seam consumed by the
// workflow REPAIR mode.
type Admitter interface {
	RevalidateAndFence(context.Context, RevalidationRequest) (Result, error)
}

// Revalidate performs all current-truth checks without creating a fence.
func Revalidate(req RevalidationRequest) (Result, error) {
	if err := req.Plan.Validate(); err != nil {
		return Result{}, err
	}
	if req.Now.IsZero() {
		return Result{}, fmt.Errorf("%w: evaluation time is required", ErrInvalidEvidence)
	}
	if req.Current.PlanDigest == "" || req.Current.FindingDigest == "" || req.Current.ObservationDigest == "" ||
		req.Current.AuthorityPolicy == "" || req.Current.MappingVersion == "" || req.Current.CredentialRef == "" || req.Current.TargetVersion == "" {
		return Result{Status: StatusUnknown, PlanDigest: req.Plan.Digest, Reason: "current evidence is incomplete"}, nil
	}
	if req.Current.PlanDigest != req.Plan.Digest {
		return readyRefusal(req, StatusReplanRequired, "the durable plan digest changed")
	}
	if req.Current.TargetAlreadySatisfied {
		return readyRefusal(req, StatusNoLongerRequired, "the failed effect is already satisfied")
	}
	if req.Current.SupersedingTransaction != "" {
		return readyRefusal(req, StatusReplanRequired, "a superseding transaction exists")
	}
	if req.Current.FindingDigest != req.Plan.FindingDigest {
		return readyRefusal(req, StatusReplanRequired, "the finding changed")
	}
	if req.Current.ObservationDigest != req.Plan.ObservationDigest {
		return readyRefusal(req, StatusReplanRequired, "the observation changed")
	}
	if req.Current.AuthorityPolicy != req.Plan.AuthorityPolicy {
		return readyRefusal(req, StatusReplanRequired, "the authority policy changed")
	}
	if req.Current.MappingVersion != req.Plan.MappingVersion {
		return readyRefusal(req, StatusReplanRequired, "the mapping version changed")
	}
	if req.Current.CredentialRef != req.Plan.CredentialRef {
		return readyRefusal(req, StatusReplanRequired, "the credential changed")
	}
	if req.Current.TargetVersion != req.Plan.TargetVersion {
		return readyRefusal(req, StatusReplanRequired, "the target version changed")
	}
	if len(req.Current.Unknowns) != 0 {
		return readyRefusal(req, StatusUnknown, "current evidence contains unknown values")
	}
	step := req.Plan.failedStep()
	if step.Blocked {
		return readyRefusal(req, StatusBlocked, "the failed effect requires a human ruling")
	}
	if req.Plan.RequiresApproval || step.RequiresApproval {
		if !req.Current.ApprovalValid || req.Current.ApprovalDigest != req.Plan.ApprovalDigest {
			return readyRefusal(req, StatusReapprovalRequired, "repair approval is absent or stale")
		}
		if !req.Current.ApprovalExpiresAt.IsZero() && !req.Now.Before(req.Current.ApprovalExpiresAt) {
			return readyRefusal(req, StatusReapprovalRequired, "repair approval expired")
		}
	}
	return Result{Status: StatusReady, PlanDigest: req.Plan.Digest, Reason: "current evidence confirms the exact repair plan", EvidenceHash: evidenceHash(req)}, nil
}

func readyRefusal(req RevalidationRequest, status Status, reason string) (Result, error) {
	return Result{Status: status, PlanDigest: req.Plan.Digest, Reason: reason, EvidenceHash: evidenceHash(req)}, nil
}

func (p RepairPlan) failedStep() Step {
	for _, step := range p.Steps {
		if step.EffectKey == p.FailedEffectKey {
			return step
		}
	}
	return Step{}
}

// Simulate returns the pure pre-execution projection. It deliberately has no
// fence and no effect counter other than zero.
type Simulation struct {
	Status       Status
	PlanDigest   string
	Reason       string
	EffectCount  int
	ResultDigest string
}

func Simulate(req RevalidationRequest) (Simulation, error) {
	result, err := Revalidate(req)
	if err != nil {
		return Simulation{}, err
	}
	return Simulation{Status: result.Status, PlanDigest: result.PlanDigest, Reason: result.Reason, ResultDigest: result.EvidenceHash}, nil
}

// MemoryStore is a concurrency-safe reference implementation of the fenced
// repair admission boundary. It stores only execution identities, never
// business payloads or provider responses.
type MemoryStore struct {
	mu     sync.Mutex
	fences map[string]Fence
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{fences: make(map[string]Fence)} }

func (s *MemoryStore) RevalidateAndFence(ctx context.Context, req RevalidationRequest) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("%w: nil context", ErrInvalidEvidence)
	}
	select {
	case <-ctx.Done():
		return Result{}, fmt.Errorf("%w: %v", ErrContextCancelled, ctx.Err())
	default:
	}
	result, err := Revalidate(req)
	if err != nil || result.Status != StatusReady {
		return result, err
	}
	fence := Fence{
		FenceID:             "repair-fence:" + result.EvidenceHash,
		FenceKey:            "repair:" + req.Plan.ID + ":" + req.Plan.FailedEffectKey,
		PlanDigest:          req.Plan.Digest,
		OriginalSemanticKey: req.Plan.OriginalSemanticKey,
		FailedEffectKey:     req.Plan.FailedEffectKey,
		IssuedAt:            req.Now.UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.fences[fence.FenceKey]; ok {
		if prior.FenceID != fence.FenceID || prior.PlanDigest != fence.PlanDigest {
			return Result{}, fmt.Errorf("%w: %s", ErrFenceConflict, fence.FenceKey)
		}
		result.Fence = prior
		return result, nil
	}
	s.fences[fence.FenceKey] = fence
	result.Fence = fence
	return result, nil
}

func evidenceHash(req RevalidationRequest) string {
	keys := make([]string, 0, len(req.Current.Facts))
	for key := range req.Current.Facts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, value := range []string{
		req.Plan.Digest, req.Current.PlanDigest, req.Current.FindingDigest,
		req.Current.ObservationDigest, req.Current.AuthorityPolicy, req.Current.MappingVersion,
		req.Current.CredentialRef, req.Current.TargetVersion, req.Current.ApprovalDigest,
		req.Current.SupersedingTransaction,
	} {
		h.Write([]byte(value))
		h.Write([]byte{0})
	}
	for _, key := range keys {
		h.Write([]byte(key))
		h.Write([]byte{0})
		h.Write([]byte(req.Current.Facts[key]))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Explain is a bounded, payload-free account of a repair admission result.
func Explain(result Result) string {
	return fmt.Sprintf("repair v%d status=%s plan=%s fence=%s reason=%s", Version(), result.Status, result.PlanDigest, result.Fence.FenceID, result.Reason)
}
