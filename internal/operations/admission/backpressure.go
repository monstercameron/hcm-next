package admission

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// BackpressureState is the observed state of a downstream resource.
type BackpressureState string

const (
	BackpressureHealthy     BackpressureState = "HEALTHY"
	BackpressureSlow        BackpressureState = "SLOW"
	BackpressureThrottled   BackpressureState = "THROTTLED"
	BackpressureUnavailable BackpressureState = "UNAVAILABLE"
)

// BackpressureSignal is a point-in-time, caller-supplied observation. It is
// intentionally a value, so the same observation always produces the same
// upstream action.
type BackpressureSignal struct {
	Source           string
	Dependency       string
	State            BackpressureState
	QueueDepth       int
	QueueLimit       int
	Capacity         int
	RecommendedRate  int
	RetryAfter       int
	PropagationScope string
}

type BackpressureAction string

const (
	BackpressureContinue     BackpressureAction = "CONTINUE"
	BackpressureSlowUpstream BackpressureAction = "SLOW"
	BackpressureQueue        BackpressureAction = "QUEUE"
	BackpressureDefer        BackpressureAction = "DEFER"
	BackpressureStop         BackpressureAction = "STOP"
)

// BackpressureDecision is the bounded propagation receipt for one signal.
type BackpressureDecision struct {
	Action          BackpressureAction
	Reason          string
	RetryAfter      int
	RecommendedRate int
	Targets         []string
	Signal          BackpressureSignal
}

// DecideBackpressure converts one downstream signal to the action an
// upstream scheduler must take. Invalid observations stop propagation rather
// than being treated as healthy.
func DecideBackpressure(signal BackpressureSignal, targets []string) BackpressureDecision {
	targets = sortedUnique(targets)
	d := BackpressureDecision{Action: BackpressureContinue, Targets: targets, Signal: signal}
	if strings.TrimSpace(signal.Source) == "" || strings.TrimSpace(signal.Dependency) == "" || signal.QueueDepth < 0 || signal.QueueLimit < 0 || signal.Capacity < 0 || signal.RecommendedRate < 0 || signal.RetryAfter < 0 {
		d.Action, d.Reason = BackpressureStop, "INVALID_SIGNAL"
		return d
	}
	if signal.QueueLimit > 0 && signal.QueueDepth > signal.QueueLimit {
		d.Action, d.Reason = BackpressureDefer, "QUEUE_WATERMARK_EXCEEDED"
		d.RetryAfter = positive(signal.RetryAfter, 1)
		return d
	}
	switch signal.State {
	case "", BackpressureHealthy:
		d.Reason = "DOWNSTREAM_HEALTHY"
	case BackpressureSlow:
		d.Action, d.Reason = BackpressureSlowUpstream, "DOWNSTREAM_SLOW"
		d.RecommendedRate = signal.RecommendedRate
	case BackpressureThrottled:
		d.Action, d.Reason = BackpressureQueue, "DOWNSTREAM_THROTTLED"
		d.RetryAfter = positive(signal.RetryAfter, 1)
	case BackpressureUnavailable:
		d.Action, d.Reason = BackpressureDefer, "DOWNSTREAM_UNAVAILABLE"
		d.RetryAfter = positive(signal.RetryAfter, 1)
	default:
		d.Action, d.Reason = BackpressureStop, "UNKNOWN_DOWNSTREAM_STATE"
	}
	return d
}

// FailureClass is a stable class, never a raw downstream payload.
type FailureClass string

const (
	FailureUnavailable FailureClass = "UNAVAILABLE"
	FailureThrottled   FailureClass = "THROTTLED"
	FailureTimeout     FailureClass = "TIMEOUT"
	FailureTransient   FailureClass = "TRANSIENT"
)

// RetryBudget is the immutable snapshot of one logical operation's budget.
// Consumed and Refunded are supplied by the owning durable counter; this
// package only makes the next decision and receipt.
type RetryBudget struct {
	ID         string
	TenantID   string
	Service    string
	Dependency string
	Operation  string
	Allowed    int
	Consumed   int
	Refunded   int
	Retryable  []FailureClass
	Version    string
}

type RetryAttempt struct {
	LogicalOperationID string
	TenantID           string
	Dependency         string
	Failure            FailureClass
	Attempt            int
}

type RetryDisposition string

const (
	RetryNotAllowed      RetryDisposition = "NOT_ALLOWED"
	RetryAllowed         RetryDisposition = "RETRY"
	RetryBudgetExhausted RetryDisposition = "RETRY_BUDGET_EXHAUSTED"
	RetryRepairRequired  RetryDisposition = "REPAIR_REQUIRED"
)

type RetryReceipt struct {
	Disposition RetryDisposition
	Reason      string
	Remaining   int
	Consumed    int
	RepairRoute string
	BudgetID    string
	TenantID    string
	Dependency  string
}

var ErrInvalidRetryInput = errors.New("admission: invalid retry input")

// ConsumeRetry evaluates one retry without mutating the caller's budget.
// The durable owner records the returned Consumed increment exactly once for
// the logical operation.
func ConsumeRetry(budget RetryBudget, attempt RetryAttempt) (RetryReceipt, error) {
	receipt := RetryReceipt{BudgetID: budget.ID, TenantID: budget.TenantID, Dependency: budget.Dependency}
	if strings.TrimSpace(budget.ID) == "" || strings.TrimSpace(budget.TenantID) == "" || strings.TrimSpace(budget.Dependency) == "" || budget.Allowed < 0 || budget.Consumed < 0 || budget.Refunded < 0 || budget.Consumed > budget.Allowed+budget.Refunded || strings.TrimSpace(attempt.LogicalOperationID) == "" || attempt.TenantID != budget.TenantID || attempt.Dependency != budget.Dependency || attempt.Attempt <= 0 {
		return RetryReceipt{}, fmt.Errorf("%w: invalid budget or attempt", ErrInvalidRetryInput)
	}
	if !containsFailure(budget.Retryable, attempt.Failure) {
		receipt.Disposition, receipt.Reason, receipt.Remaining = RetryNotAllowed, "FAILURE_NOT_RETRYABLE", remaining(budget)
		return receipt, nil
	}
	receipt.Remaining = remaining(budget)
	if receipt.Remaining <= 0 {
		receipt.Disposition, receipt.Reason, receipt.RepairRoute = RetryBudgetExhausted, "RETRY_BUDGET_EXHAUSTED", "operations.repair.retry_budget"
		return receipt, nil
	}
	receipt.Disposition, receipt.Reason, receipt.Consumed = RetryAllowed, "RETRY_TOKEN_GRANTED", 1
	receipt.Remaining--
	return receipt, nil
}

func remaining(b RetryBudget) int { return b.Allowed - b.Consumed + b.Refunded }

func containsFailure(values []FailureClass, want FailureClass) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func positive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
