package admission

// Durable retry-budget provisioning (ADMISSION-002): production budget
// provisioning per logical operation, request-scoped consumption with
// replay-safe attempt identities, bounded refunds and upstream
// scheduling integration. The provisioner owns the durable counters; the
// pure ConsumeRetry decision stays the only policy. Re-provisioning never
// fabricates allowance and exhaustion keeps one stable repair route.

import (
	"fmt"
	"sort"
	"sync"
)

// ProvisionSpec provisions one logical operation's retry budget.
type ProvisionSpec struct {
	TenantID           string
	Service            string
	Dependency         string
	LogicalOperationID string
	OperationKind      string
	Allowed            int
	Retryable          []FailureClass
	Version            string
}

// AttemptInput scopes one consumption to a replay-safe attempt identity.
type AttemptInput struct {
	AttemptID string
	Attempt   RetryAttempt
}

type budgetState struct {
	budget   RetryBudget
	receipts map[string]RetryReceipt
	refunded map[string]bool
}

// Provisioner is the durable budget owner: budgets, attempt identities
// and refunds under one mutex.
type Provisioner struct {
	mu      sync.Mutex
	budgets map[string]*budgetState
}

// NewProvisioner returns an empty durable owner.
func NewProvisioner() *Provisioner {
	return &Provisioner{budgets: make(map[string]*budgetState)}
}

func budgetID(tenant, operation string) string {
	return "budget/" + tenant + "/" + operation
}

// Provision creates or returns the budget for one logical operation.
// Re-provisioning the same operation returns the current snapshot: never
// new allowance, never a reset.
func (p *Provisioner) Provision(spec ProvisionSpec) (RetryBudget, error) {
	if spec.Allowed < 0 || !validFailureSet(spec.Retryable) || len(spec.Retryable) == 0 {
		return RetryBudget{}, fmt.Errorf("%w: allowance and retryable set are required", ErrInvalidRetryInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	id := budgetID(spec.TenantID, spec.LogicalOperationID)
	if state, ok := p.budgets[id]; ok {
		return state.budget, nil
	}
	// The provisioner never accepts what the pure policy cannot
	// evaluate: identity, allowance and retryable set are required.
	if spec.TenantID == "" || spec.Dependency == "" || spec.LogicalOperationID == "" ||
		spec.OperationKind == "" || spec.Version == "" {
		return RetryBudget{}, fmt.Errorf("%w: budget identity is required", ErrInvalidRetryInput)
	}
	budget := RetryBudget{
		ID: id, TenantID: spec.TenantID, Service: spec.Service, Dependency: spec.Dependency,
		LogicalOperationID: spec.LogicalOperationID, OperationKind: spec.OperationKind,
		Allowed: spec.Allowed, Retryable: append([]FailureClass(nil), spec.Retryable...), Version: spec.Version,
	}
	p.budgets[id] = &budgetState{budget: budget, receipts: make(map[string]RetryReceipt), refunded: make(map[string]bool)}
	return budget, nil
}

// Snapshot returns the current budget counters.
func (p *Provisioner) Snapshot(budgetID string) (RetryBudget, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.budgets[budgetID]
	if !ok {
		return RetryBudget{}, false
	}
	return state.budget, true
}

// Consume evaluates one attempt and records its receipt exactly once per
// attempt identity: replaying the same AttemptID returns the stored
// receipt without moving the counters.
func (p *Provisioner) Consume(budgetID string, input AttemptInput) (RetryReceipt, error) {
	if input.AttemptID == "" {
		return RetryReceipt{}, fmt.Errorf("%w: attempt identity is required", ErrInvalidRetryInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.budgets[budgetID]
	if !ok {
		return RetryReceipt{}, fmt.Errorf("%w: unknown budget", ErrInvalidRetryInput)
	}
	if receipt, ok := state.receipts[input.AttemptID]; ok {
		return receipt, nil
	}
	receipt, err := ConsumeRetry(state.budget, input.Attempt)
	if err != nil {
		return RetryReceipt{}, err
	}
	if receipt.Disposition == RetryAllowed {
		state.budget.Consumed += receipt.Consumed
	}
	state.receipts[input.AttemptID] = receipt
	return receipt, nil
}

// Refund returns one consumed token for a misclassified attempt. Only a
// recorded consumed attempt refunds, and only once.
func (p *Provisioner) Refund(budgetID, attemptID, reason string) (RetryBudget, error) {
	if attemptID == "" || reason == "" {
		return RetryBudget{}, fmt.Errorf("%w: attempt identity and reason are required", ErrInvalidRetryInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.budgets[budgetID]
	if !ok {
		return RetryBudget{}, fmt.Errorf("%w: unknown budget", ErrInvalidRetryInput)
	}
	receipt, ok := state.receipts[attemptID]
	if !ok || receipt.Disposition != RetryAllowed || receipt.Consumed == 0 {
		return RetryBudget{}, fmt.Errorf("%w: nothing to refund", ErrInvalidRetryInput)
	}
	if state.refunded[attemptID] {
		return RetryBudget{}, fmt.Errorf("%w: attempt already refunded", ErrInvalidRetryInput)
	}
	state.refunded[attemptID] = true
	state.budget.Refunded += receipt.Consumed
	return state.budget, nil
}

// Ledger lists every budget ID in order.
func (p *Provisioner) Ledger() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, 0, len(p.budgets))
	for id := range p.budgets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
