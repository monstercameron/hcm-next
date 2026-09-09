package approval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactionplan "github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

// ApprovalState is the lifecycle state read by the execution boundary. A
// decision remains immutable; revocation and supersession are separate ledger
// facts that make an old decision ineligible.
type ApprovalState string

const (
	ApprovalActive     ApprovalState = "ACTIVE"
	ApprovalRevoked    ApprovalState = "REVOKED"
	ApprovalSuperseded ApprovalState = "SUPERSEDED"
)

// ApprovalRecord pairs immutable decision evidence with the current lifecycle
// fact needed by execution eligibility.
type ApprovalRecord struct {
	Decision  ApprovalDecision
	State     ApprovalState
	ExpiresAt values.Instant
}

// ConsumeRequest is the complete input to the atomic approval/plan boundary.
// The plan is the exact immutable plan elected for execution; no plan is
// prepared or inferred inside this package.
type ConsumeRequest struct {
	Plan      transactionplan.TransactionPlan
	Approvals []ApprovalRecord
	Now       values.Instant
}

// ExecutionEligibility is the durable-shaped result of consuming approvals
// and electing one executable plan. ConsumedDecisionIDs and PlanDigest are
// the binding a transaction adapter must persist in one local transaction.
type ExecutionEligibility struct {
	PlanDigest          string
	ApprovalSetDigest   string
	ConsumedDecisionIDs []string
	ConsumedAt          values.Instant
	Executable          bool
	Digest              string
}

// Explain returns an audit-safe summary of the approval execution boundary.
func (e ExecutionEligibility) Explain() string {
	return fmt.Sprintf("approval eligibility executable=%t plan=%s approvals=%d consumed_at=%s digest=%s",
		e.Executable, e.PlanDigest, len(e.ConsumedDecisionIDs), e.ConsumedAt, e.Digest)
}

// AtomicPlanConsumer is the narrow persistence port for APPROVAL-003. Its
// implementation must consume the approval rows and elect the executable plan
// in one local transaction, rolling both back on any refusal.
type AtomicPlanConsumer interface {
	ConsumeAndElect(context.Context, ConsumeRequest) (ExecutionEligibility, error)
}

// MemoryExecutionStore is a concurrency-safe kernel adapter and a reference
// implementation of the atomic port. A database adapter can persist the same
// state with SELECT ... FOR UPDATE and one commit without changing callers.
type MemoryExecutionStore struct {
	mu       sync.Mutex
	consumed map[string]string
	results  map[string]ExecutionEligibility
}

// NewMemoryExecutionStore returns an empty approval execution store.
func NewMemoryExecutionStore() *MemoryExecutionStore {
	return &MemoryExecutionStore{
		consumed: make(map[string]string),
		results:  make(map[string]ExecutionEligibility),
	}
}

// ConsumeAndElect validates the whole set before mutating state, then commits
// every decision marker and the elected plan while holding one mutex. The
// operation is idempotent for the exact same plan and approval set.
func (s *MemoryExecutionStore) ConsumeAndElect(ctx context.Context, req ConsumeRequest) (ExecutionEligibility, error) {
	if err := contextError(ctx); err != nil {
		return ExecutionEligibility{}, err
	}
	if s == nil {
		return ExecutionEligibility{}, refusal("ConsumeAndElect", "store", CodeAtomicityFailure, ErrAtomicityFailure, "store is nil")
	}
	if err := validateConsumeRequest(req); err != nil {
		return ExecutionEligibility{}, err
	}
	decisionIDs := make([]string, 0, len(req.Approvals))
	decisionBindings := make([]string, 0, len(req.Approvals))
	for _, record := range req.Approvals {
		decisionIDs = append(decisionIDs, record.Decision.DecisionID)
		decisionBindings = append(decisionBindings, record.Decision.DecisionID+"@"+record.Decision.Digest())
	}
	sort.Strings(decisionIDs)
	sort.Strings(decisionBindings)
	setDigest := approvalSetDigest(req.Plan.Digest, decisionBindings)

	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.results[setDigest]; ok {
		return cloneEligibility(prior), nil
	}
	for _, id := range decisionIDs {
		if priorPlan, ok := s.consumed[id]; ok {
			return ExecutionEligibility{}, refusal("ConsumeAndElect", "approvals", CodeApprovalAlreadyUsed, ErrApprovalAlreadyUsed,
				"decision %q was already consumed for plan %q", id, priorPlan)
		}
	}

	result := ExecutionEligibility{
		PlanDigest: req.Plan.Digest, ApprovalSetDigest: setDigest,
		ConsumedDecisionIDs: append([]string(nil), decisionIDs...), ConsumedAt: req.Now,
		Executable: true,
	}
	result.Digest = eligibilityDigest(result)
	// No mutation occurs before every refusal point above. These assignments
	// therefore model one commit boundary rather than a sequence of effects.
	for _, id := range decisionIDs {
		s.consumed[id] = req.Plan.Digest
	}
	s.results[setDigest] = result
	return cloneEligibility(result), nil
}

func validateConsumeRequest(req ConsumeRequest) error {
	if req.Now.Validate() != nil {
		return refusal("ConsumeAndElect", "now", CodeInvalidExecutionInput, ErrInvalidExecutionInput, "now is unset")
	}
	if err := req.Plan.VerifyDigest(); err != nil {
		return refusal("ConsumeAndElect", "plan", CodePlanNotExecutable, ErrPlanNotExecutable, "%v", err)
	}
	if req.Plan.ExpiresAt.Validate() != nil || req.Now.Compare(req.Plan.ExpiresAt) >= 0 {
		return refusal("ConsumeAndElect", "plan.expires_at", CodePlanNotExecutable, ErrPlanNotExecutable,
			"plan %q is expired", req.Plan.PlanID)
	}
	if strings.TrimSpace(req.Plan.ProposalRevisionID) == "" || strings.TrimSpace(req.Plan.ProposalDigest) == "" {
		return refusal("ConsumeAndElect", "plan.binding", CodePlanMismatch, ErrPlanMismatch, "plan has no proposal binding")
	}
	if len(req.Approvals) == 0 {
		return refusal("ConsumeAndElect", "approvals", CodePlanNotExecutable, ErrPlanNotExecutable, "no approvals supplied")
	}
	seen := make(map[string]bool, len(req.Approvals))
	for _, record := range req.Approvals {
		d := record.Decision
		if d.DecisionID == "" || seen[d.DecisionID] {
			return refusal("ConsumeAndElect", "approvals.decision_id", CodePlanNotExecutable, ErrPlanNotExecutable,
				"decision ids must be present and unique")
		}
		seen[d.DecisionID] = true
		if d.Outcome != OutcomeApproved {
			return refusal("ConsumeAndElect", "approvals.outcome", CodePlanNotExecutable, ErrPlanNotExecutable,
				"decision %q is not approved", d.DecisionID)
		}
		if d.Digest() == "" || d.Binding.RenderedProjectionDigest == "" || d.Binding.TaskVersion == 0 {
			return refusal("ConsumeAndElect", "approvals.binding", CodePlanNotExecutable, ErrPlanNotExecutable,
				"decision %q has incomplete immutable binding evidence", d.DecisionID)
		}
		if d.Binding.ProposalRevisionID != req.Plan.ProposalRevisionID || d.Binding.ProposalDigest.Digest != req.Plan.ProposalDigest {
			return refusal("ConsumeAndElect", "approvals.binding", CodePlanMismatch, ErrPlanMismatch,
				"decision %q is bound to another proposal or plan", d.DecisionID)
		}
		switch record.State {
		case "", ApprovalActive:
		case ApprovalRevoked:
			return refusal("ConsumeAndElect", "approvals.state", CodeApprovalRevoked, ErrApprovalRevoked, "decision %q is revoked", d.DecisionID)
		case ApprovalSuperseded:
			return refusal("ConsumeAndElect", "approvals.state", CodeApprovalSuperseded, ErrApprovalSuperseded, "decision %q is superseded", d.DecisionID)
		default:
			return refusal("ConsumeAndElect", "approvals.state", CodePlanNotExecutable, ErrPlanNotExecutable, "decision %q has unknown state %q", d.DecisionID, record.State)
		}
		if record.ExpiresAt.IsSet() && req.Now.Compare(record.ExpiresAt) >= 0 {
			return refusal("ConsumeAndElect", "approvals.expires_at", CodeApprovalExpired, ErrApprovalExpired, "decision %q is expired", d.DecisionID)
		}
	}
	return nil
}

func approvalSetDigest(planDigest string, decisionBindings []string) string {
	w := canonicalbytes.New("hcmnext.intent.approval.execution-set", 1).String("plan_digest", planDigest)
	w.SortedStrings("decision_bindings", decisionBindings)
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func eligibilityDigest(e ExecutionEligibility) string {
	w := canonicalbytes.New("hcmnext.intent.approval.execution-eligibility", 1).
		String("plan_digest", e.PlanDigest).
		String("approval_set_digest", e.ApprovalSetDigest).
		SortedStrings("decision_ids", e.ConsumedDecisionIDs).
		Value("consumed_at", e.ConsumedAt).
		Bool("executable", e.Executable)
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func cloneEligibility(e ExecutionEligibility) ExecutionEligibility {
	e.ConsumedDecisionIDs = append([]string(nil), e.ConsumedDecisionIDs...)
	return e
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return refusal("ConsumeAndElect", "context", CodeAtomicityFailure, ErrAtomicityFailure, "%v", ctx.Err())
	default:
		return nil
	}
}

func refusal(op, field, code string, cause error, format string, args ...any) error {
	return newError(op, field, code, cause, format, args...)
}

// Explain is the package-level symbol consumed by documentation and policy
// tooling for this approval execution contract.
func Explain() string {
	return "approval execution consumes the exact approved decision set and elects its exact transaction-plan digest at one local commit boundary"
}
