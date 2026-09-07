// Package latencygate provides a small, deterministic contract for measured
// UI interaction budgets. It deliberately reports percentiles rather than a
// single run so the gate tolerates isolated scheduler noise while still
// rejecting consistently slow interactions.
package latencygate

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
)

// Budget describes one named interaction and its maximum allowed p95.
type Budget struct {
	Name    string
	P95     time.Duration
	Warmups int
	Samples int
}

// Result is the compact evidence emitted by a measured interaction gate.
type Result struct {
	Name    string
	Samples int
	P50     time.Duration
	P95     time.Duration
	Max     time.Duration
}

func (result Result) String() string {
	return fmt.Sprintf("%s: n=%d p50=%s p95=%s max=%s", result.Name, result.Samples, result.P50, result.P95, result.Max)
}

// Measure warms the operation, records the requested samples, and returns a
// percentile result. Operation errors stop the measurement immediately.
func Measure(budget Budget, operation func() error) (Result, error) {
	if err := validate(budget, operation); err != nil {
		return Result{}, err
	}
	for index := 0; index < budget.Warmups; index++ {
		if err := operation(); err != nil {
			return Result{}, fmt.Errorf("latencygate: %s warmup %d: %w", budget.Name, index+1, err)
		}
	}

	durations := make([]time.Duration, budget.Samples)
	for index := range durations {
		started := time.Now()
		if err := operation(); err != nil {
			return Result{}, fmt.Errorf("latencygate: %s sample %d: %w", budget.Name, index+1, err)
		}
		durations[index] = time.Since(started)
	}
	return Evaluate(budget.Name, durations), nil
}

// Evaluate summarizes an existing duration corpus. It is exported so callers
// with browser or telemetry timings can apply the same nearest-rank policy.
func Evaluate(name string, durations []time.Duration) Result {
	if len(durations) == 0 {
		return Result{Name: name}
	}
	ordered := slices.Clone(durations)
	slices.Sort(ordered)
	return Result{
		Name: name, Samples: len(ordered),
		P50: nearestRank(ordered, 0.50),
		P95: nearestRank(ordered, 0.95),
		Max: ordered[len(ordered)-1],
	}
}

// Check rejects a result whose p95 exceeds the declared budget.
func Check(budget Budget, result Result) error {
	if err := validateBudget(budget); err != nil {
		return err
	}
	if result.Samples != budget.Samples {
		return fmt.Errorf("latencygate: %s collected %d samples, want %d", budget.Name, result.Samples, budget.Samples)
	}
	if result.P95 > budget.P95 {
		return fmt.Errorf("latencygate: %s p95 %s exceeds budget %s (p50=%s max=%s n=%d)",
			budget.Name, result.P95, budget.P95, result.P50, result.Max, result.Samples)
	}
	return nil
}

func validate(budget Budget, operation func() error) error {
	if err := validateBudget(budget); err != nil {
		return err
	}
	if operation == nil {
		return errors.New("latencygate: operation is required")
	}
	return nil
}

func validateBudget(budget Budget) error {
	if budget.Name == "" {
		return errors.New("latencygate: budget name is required")
	}
	if budget.P95 <= 0 {
		return fmt.Errorf("latencygate: %s p95 budget must be positive", budget.Name)
	}
	if budget.Samples < 20 {
		return fmt.Errorf("latencygate: %s needs at least 20 samples", budget.Name)
	}
	if budget.Warmups < 0 {
		return fmt.Errorf("latencygate: %s warmups cannot be negative", budget.Name)
	}
	return nil
}

func nearestRank(ordered []time.Duration, percentile float64) time.Duration {
	index := int(math.Ceil(percentile*float64(len(ordered)))) - 1
	return ordered[max(0, min(index, len(ordered)-1))]
}
