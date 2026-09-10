package runtime

// Immediate-caller retry policy (WF-RUN-006): one classifier, max
// attempts, capped exponential backoff with injected jitter, deadline and
// a propagated shared budget produce the exact next attempt or a terminal
// route. Nested layers pass the same budget, so attempts never multiply.
// Domain and provider error mapping stays with the caller: the policy
// schedules stable failure kinds only, and honors caller-mapped
// DO_NOT_RETRY above everything except validation.

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// FailureKind is the stable caller-mapped class.
type FailureKind string

const (
	FailureTransient   FailureKind = "TRANSIENT"
	FailureTimeout     FailureKind = "TIMEOUT"
	FailureThrottled   FailureKind = "THROTTLED"
	FailureUnavailable FailureKind = "UNAVAILABLE"
	FailurePermanent   FailureKind = "PERMANENT"
)

// Route decisions and terminal reasons.
const (
	RouteRetry    = "RETRY"
	RouteTerminal = "TERMINAL"

	ReasonNonretryable      = "NONRETRYABLE"
	ReasonDoNotRetry        = "DO_NOT_RETRY"
	ReasonBudgetExhausted   = "BUDGET_EXHAUSTED"
	ReasonDeadlineExceeded  = "DEADLINE_EXCEEDED"
	ReasonAttemptsExhausted = "ATTEMPTS_EXHAUSTED"
)

// RetryPolicy bounds one immediate caller's retries.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Deadline    time.Duration
	Retryable   []FailureKind
	// Jitter shapes the backoff deterministically in tests; nil keeps the
	// capped exponential delay unchanged.
	Jitter func(attempt int, delay time.Duration) time.Duration
}

// BudgetRef propagates the shared admission budget.
type BudgetRef struct {
	Provisioner        *admission.Provisioner
	BudgetID           string
	TenantID           string
	LogicalOperationID string
	OperationKind      string
	Dependency         string
}

// RetryAttempt is one completed failure awaiting routing.
type RetryAttempt struct {
	N          int
	Failure    FailureKind
	DoNotRetry bool
	Elapsed    time.Duration
	AttemptID  string
	Layer      string
}

// RetryRoute is the exact next step or the terminal reason.
type RetryRoute struct {
	Decision        string
	Reason          string
	NextDelay       time.Duration
	NextAttempt     int
	RepairRoute     string
	BudgetRemaining int
	Layer           string
}

// CappedExponential returns base doubled per attempt, capped at max.
func CappedExponential(n int, base, max time.Duration) time.Duration {
	delay := base
	for i := 1; i < n; i++ {
		delay *= 2
		if delay >= max || delay <= 0 {
			return max
		}
	}
	if delay > max {
		return max
	}
	return delay
}

func retryable(policy RetryPolicy, failure FailureKind) bool {
	for _, kind := range policy.Retryable {
		if kind == failure {
			return true
		}
	}
	return false
}

func budgetClass(failure FailureKind) (admission.FailureClass, bool) {
	switch failure {
	case FailureTransient:
		return admission.FailureTransient, true
	case FailureTimeout:
		return admission.FailureTimeout, true
	case FailureThrottled:
		return admission.FailureThrottled, true
	case FailureUnavailable:
		return admission.FailureUnavailable, true
	default:
		return "", false
	}
}

func terminal(reason string) (RetryRoute, error) {
	return RetryRoute{Decision: RouteTerminal, Reason: reason}, nil
}

// Decide routes one failed attempt: the exact next delay or a terminal
// reason. The shared budget spends one token per scheduled retry under
// the attempt identity, so replays and nested layers never multiply
// attempts.
func Decide(policy RetryPolicy, budget BudgetRef, attempt RetryAttempt) (RetryRoute, error) {
	if policy.MaxAttempts <= 0 || policy.BaseDelay <= 0 || policy.MaxDelay <= 0 || len(policy.Retryable) == 0 {
		return RetryRoute{}, fmt.Errorf("runtime: Decide: invalid retry policy")
	}
	if attempt.AttemptID == "" {
		return RetryRoute{}, fmt.Errorf("runtime: Decide: attempt identity is required")
	}
	if budget.Provisioner == nil || budget.BudgetID == "" {
		return RetryRoute{}, fmt.Errorf("runtime: Decide: propagated budget is required")
	}
	if attempt.DoNotRetry {
		return terminal(ReasonDoNotRetry)
	}
	if !retryable(policy, attempt.Failure) {
		return terminal(ReasonNonretryable)
	}
	next := attempt.N + 1
	if next > policy.MaxAttempts {
		return terminal(ReasonAttemptsExhausted)
	}
	// Retry k waits base*2^(k-1): the upcoming attempt number minus the
	// initial try, so the first retry waits exactly the base delay.
	delay := CappedExponential(next-1, policy.BaseDelay, policy.MaxDelay)
	if policy.Jitter != nil {
		delay = policy.Jitter(next, delay)
		if delay < 0 {
			delay = 0
		}
		if delay > policy.MaxDelay {
			delay = policy.MaxDelay
		}
	}
	if policy.Deadline > 0 && attempt.Elapsed+delay > policy.Deadline {
		return terminal(ReasonDeadlineExceeded)
	}
	class, ok := budgetClass(attempt.Failure)
	if !ok {
		return terminal(ReasonNonretryable)
	}
	receipt, err := budget.Provisioner.Consume(budget.BudgetID, admission.AttemptInput{
		AttemptID: attempt.AttemptID,
		Attempt: admission.RetryAttempt{
			LogicalOperationID: budget.LogicalOperationID, OperationKind: budget.OperationKind,
			TenantID: budget.TenantID, Dependency: budget.Dependency,
			Failure: class, Attempt: next,
		},
	})
	if err != nil {
		return RetryRoute{}, fmt.Errorf("runtime: Decide: %w", err)
	}
	switch receipt.Disposition {
	case admission.RetryAllowed:
		return RetryRoute{
			Decision: RouteRetry, NextDelay: delay, NextAttempt: next,
			BudgetRemaining: receipt.Remaining, Layer: attempt.Layer,
		}, nil
	case admission.RetryBudgetExhausted:
		return RetryRoute{Decision: RouteTerminal, Reason: ReasonBudgetExhausted, RepairRoute: receipt.RepairRoute}, nil
	default:
		return terminal(ReasonNonretryable)
	}
}
