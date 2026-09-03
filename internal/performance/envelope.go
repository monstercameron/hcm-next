// Package performance owns versioned workload envelopes and admission limits.
// It is intentionally independent of runtime, transport, and generated code.
package performance

import (
	"errors"
	"fmt"
	"math"
)

const SchemaVersion = 1

type Tier string

const (
	TierSmall  Tier = "small"
	TierMedium Tier = "medium"
	TierLarge  Tier = "large"
	TierPeak   Tier = "peak"
)

type DegradationPolicy string

const (
	DegradeReject   DegradationPolicy = "reject"
	DegradeQueue    DegradationPolicy = "queue"
	DegradeReadOnly DegradationPolicy = "read_only"
)

type ResourceBudget struct {
	CPUMillicores int
	MemoryMiB     int
	DBConnections int
	QueueDepth    int
	StorageMBps   int
}

type CorrectnessBudget struct {
	ErrorRatePPM          int
	DuplicateEffects      int
	LostTransactions      int
	MaxRetryAmplification int
}

// Envelope describes an admitted workload. All quantities are per tenant
// unless explicitly named otherwise; limits are immutable for a version.
type Envelope struct {
	SchemaVersion     int
	ID                string
	Version           int
	Tier              Tier
	Tenants           int
	ConcurrentUsers   int
	CommandsPerSecond int
	Fanout            int
	PayloadBytes      int
	Objects           int
	Integrations      int
	AgentRuns         int
	PeakMultiplier    float64
	Degradation       DegradationPolicy
	Resources         ResourceBudget
	Correctness       CorrectnessBudget
}

// WorkloadEnvelope is the explicit contract name used by callers. Envelope
// remains a concise alias for code that handles several envelope kinds.
type WorkloadEnvelope = Envelope

var (
	ErrInvalidEnvelope = errors.New("performance: invalid workload envelope")
	ErrHardLimit       = errors.New("performance: hard limit exceeded")
)

func (e Envelope) Validate() error {
	if e.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schema_version=%d", ErrInvalidEnvelope, e.SchemaVersion)
	}
	if e.ID == "" || e.Version < 1 {
		return fmt.Errorf("%w: id and positive version are required", ErrInvalidEnvelope)
	}
	if !validTier(e.Tier) || !validDegradation(e.Degradation) {
		return fmt.Errorf("%w: tier or degradation policy is invalid", ErrInvalidEnvelope)
	}
	positive := map[string]int{"tenants": e.Tenants, "concurrent_users": e.ConcurrentUsers, "commands_per_second": e.CommandsPerSecond, "fanout": e.Fanout, "payload_bytes": e.PayloadBytes, "objects": e.Objects, "integrations": e.Integrations, "agent_runs": e.AgentRuns, "cpu_millicores": e.Resources.CPUMillicores, "memory_mib": e.Resources.MemoryMiB, "db_connections": e.Resources.DBConnections, "queue_depth": e.Resources.QueueDepth, "storage_mbps": e.Resources.StorageMBps}
	for field, value := range positive {
		if value <= 0 {
			return fmt.Errorf("%w: %s must be positive", ErrInvalidEnvelope, field)
		}
	}
	if math.IsNaN(e.PeakMultiplier) || math.IsInf(e.PeakMultiplier, 0) || e.PeakMultiplier < 1 {
		return fmt.Errorf("%w: peak_multiplier must be finite and >= 1", ErrInvalidEnvelope)
	}
	if e.Correctness.ErrorRatePPM < 0 || e.Correctness.DuplicateEffects < 0 || e.Correctness.LostTransactions < 0 || e.Correctness.MaxRetryAmplification < 1 {
		return fmt.Errorf("%w: correctness budget is invalid", ErrInvalidEnvelope)
	}
	return nil
}

func validTier(t Tier) bool {
	return t == TierSmall || t == TierMedium || t == TierLarge || t == TierPeak
}
func validDegradation(p DegradationPolicy) bool {
	return p == DegradeReject || p == DegradeQueue || p == DegradeReadOnly
}

// HardLimits are absolute safety ceilings, independent of tier budgets.
type HardLimits struct {
	Tenants, ConcurrentUsers, CommandsPerSecond, Fanout, PayloadBytes, Objects, Integrations, AgentRuns int
	PeakMultiplier                                                                                      float64
	Resources                                                                                           ResourceBudget
	Correctness                                                                                         CorrectnessBudget
}

type Violation struct {
	Field         string
	Actual, Limit float64
}
type CheckResult struct{ Violations []Violation }

func (r CheckResult) OK() bool { return len(r.Violations) == 0 }
func (r CheckResult) Err() error {
	if r.OK() {
		return nil
	}
	return fmt.Errorf("%w: %d violation(s)", ErrHardLimit, len(r.Violations))
}

func (h HardLimits) Validate() error {
	if h.PeakMultiplier < 1 || math.IsNaN(h.PeakMultiplier) || math.IsInf(h.PeakMultiplier, 0) {
		return fmt.Errorf("%w: invalid peak multiplier", ErrHardLimit)
	}
	vals := []int{h.Tenants, h.ConcurrentUsers, h.CommandsPerSecond, h.Fanout, h.PayloadBytes, h.Objects, h.Integrations, h.AgentRuns, h.Resources.CPUMillicores, h.Resources.MemoryMiB, h.Resources.DBConnections, h.Resources.QueueDepth, h.Resources.StorageMBps}
	for _, v := range vals {
		if v <= 0 {
			return fmt.Errorf("%w: all limits must be positive", ErrHardLimit)
		}
	}
	return nil
}

func (h HardLimits) Check(e Envelope) CheckResult {
	r := CheckResult{}
	add := func(f string, a, l float64) {
		if a > l {
			r.Violations = append(r.Violations, Violation{f, a, l})
		}
	}
	add("tenants", float64(e.Tenants), float64(h.Tenants))
	add("concurrent_users", float64(e.ConcurrentUsers), float64(h.ConcurrentUsers))
	add("commands_per_second", float64(e.CommandsPerSecond), float64(h.CommandsPerSecond))
	add("fanout", float64(e.Fanout), float64(h.Fanout))
	add("payload_bytes", float64(e.PayloadBytes), float64(h.PayloadBytes))
	add("objects", float64(e.Objects), float64(h.Objects))
	add("integrations", float64(e.Integrations), float64(h.Integrations))
	add("agent_runs", float64(e.AgentRuns), float64(h.AgentRuns))
	add("peak_multiplier", e.PeakMultiplier, h.PeakMultiplier)
	add("resources.cpu_millicores", float64(e.Resources.CPUMillicores), float64(h.Resources.CPUMillicores))
	add("resources.memory_mib", float64(e.Resources.MemoryMiB), float64(h.Resources.MemoryMiB))
	add("resources.db_connections", float64(e.Resources.DBConnections), float64(h.Resources.DBConnections))
	add("resources.queue_depth", float64(e.Resources.QueueDepth), float64(h.Resources.QueueDepth))
	add("resources.storage_mbps", float64(e.Resources.StorageMBps), float64(h.Resources.StorageMBps))
	add("correctness.error_rate_ppm", float64(e.Correctness.ErrorRatePPM), float64(h.Correctness.ErrorRatePPM))
	add("correctness.duplicate_effects", float64(e.Correctness.DuplicateEffects), float64(h.Correctness.DuplicateEffects))
	add("correctness.lost_transactions", float64(e.Correctness.LostTransactions), float64(h.Correctness.LostTransactions))
	add("correctness.max_retry_amplification", float64(e.Correctness.MaxRetryAmplification), float64(h.Correctness.MaxRetryAmplification))
	return r
}

func Check(e Envelope, h HardLimits) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := h.Validate(); err != nil {
		return err
	}
	return h.Check(e).Err()
}

// Checker binds a published hard-limit set for repeated admission checks.
type Checker struct{ Limits HardLimits }

func NewChecker(limits HardLimits) (Checker, error) {
	if err := limits.Validate(); err != nil {
		return Checker{}, err
	}
	return Checker{Limits: limits}, nil
}

func (c Checker) Check(e Envelope) error { return Check(e, c.Limits) }
