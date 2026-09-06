// Package slo owns the small, versioned reliability contract consumed by
// operational admission. It evaluates caller-supplied aggregates and never
// queries telemetry or a database itself.
package slo

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const contractVersion = 1

// Version returns the SLI/SLO contract version.
func Version() int { return contractVersion }

// Explain returns a bounded description suitable for policy diagnostics.
func Explain() string { return "versioned SLI/SLO evaluation with explicit unknown telemetry state" }

type Status string

const (
	Healthy  Status = "HEALTHY"
	AtRisk   Status = "AT_RISK"
	Breached Status = "BREACHED"
	Unknown  Status = "UNKNOWN"
)

type Target struct {
	ID             string
	Capability     string
	Indicator      string
	Query          string
	Window         time.Duration
	StalenessBound time.Duration
	Threshold      float64
	Owner          string
	Version        string
}

type Observation struct {
	ObservedAt time.Time
	WindowFrom time.Time
	WindowTo   time.Time
	Total      int64
	Good       int64
	Complete   bool
}

type Result struct {
	Status       Status
	TargetID     string
	Capability   string
	Version      string
	Value        float64
	Reason       string
	Denominator  int64
	EvidenceGood bool
}

type Manifest struct {
	Version string
	Targets []Target
}

var ErrInvalidTarget = errors.New("slo: invalid target")

func (t Target) Validate() error {
	fields := map[string]string{"id": t.ID, "capability": t.Capability, "indicator": t.Indicator, "query": t.Query, "owner": t.Owner, "version": t.Version}
	for field, value := range fields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidTarget, field)
		}
	}
	if t.Window <= 0 || t.StalenessBound <= 0 || t.Threshold < 0 || t.Threshold > 1 {
		return fmt.Errorf("%w: window, staleness and threshold are invalid", ErrInvalidTarget)
	}
	return nil
}

func (m Manifest) Validate() error {
	if strings.TrimSpace(m.Version) == "" || len(m.Targets) == 0 {
		return fmt.Errorf("%w: manifest version and targets are required", ErrInvalidTarget)
	}
	seen := make(map[string]struct{}, len(m.Targets))
	for _, target := range m.Targets {
		if err := target.Validate(); err != nil {
			return err
		}
		if _, ok := seen[target.ID]; ok {
			return fmt.Errorf("%w: duplicate target %q", ErrInvalidTarget, target.ID)
		}
		seen[target.ID] = struct{}{}
	}
	return nil
}

// Evaluate classifies one aggregate. Missing, stale, incomplete or malformed
// telemetry is UNKNOWN and can never accidentally satisfy the target.
func Evaluate(target Target, observation Observation, now time.Time) (Result, error) {
	if err := target.Validate(); err != nil {
		return Result{}, err
	}
	result := Result{TargetID: target.ID, Capability: target.Capability, Version: target.Version, Denominator: observation.Total}
	if now.IsZero() || observation.ObservedAt.IsZero() || observation.WindowFrom.IsZero() || observation.WindowTo.IsZero() || observation.Total <= 0 || observation.Good < 0 || observation.Good > observation.Total || !observation.Complete || now.Before(observation.ObservedAt) || now.Sub(observation.ObservedAt) > target.StalenessBound || observation.WindowTo.Before(observation.WindowFrom) || observation.WindowTo.Sub(observation.WindowFrom) != target.Window {
		result.Status, result.Reason = Unknown, "MEASUREMENT_UNKNOWN"
		return result, nil
	}
	result.Value, result.EvidenceGood = float64(observation.Good)/float64(observation.Total), true
	switch {
	case result.Value >= target.Threshold:
		result.Status, result.Reason = Healthy, "TARGET_MET"
	case result.Value >= target.Threshold*0.95:
		result.Status, result.Reason = AtRisk, "TARGET_NEAR_BREACH"
	default:
		result.Status, result.Reason = Breached, "TARGET_BREACHED"
	}
	return result, nil
}
