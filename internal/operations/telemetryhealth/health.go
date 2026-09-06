// Package telemetryhealth evaluates telemetry as an explicit dependency of
// reliability evidence. It never turns missing observations into success.
package telemetryhealth

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const contractVersion = 1

func Version() int { return contractVersion }

type Status string

const (
	StatusHealthy Status = "HEALTHY"
	StatusUnknown Status = "UNKNOWN"
)

type EvidenceQuality string

const (
	EvidenceComplete   EvidenceQuality = "COMPLETE"
	EvidenceIncomplete EvidenceQuality = "INCOMPLETE"
)

type Policy struct {
	ID              string
	Version         string
	StalenessBound  time.Duration
	IncidentAfter   time.Duration
	MaxQueueDepth   int64
	MaxDropCount    int64
	MaxExportErrors int64
}

type Observation struct {
	CollectorOK  bool
	ExporterOK   bool
	PolicyOK     bool
	Watermark    time.Time
	ObservedAt   time.Time
	QueueDepth   int64
	DropCount    int64
	ExportErrors int64
	FailureSince time.Time
}

type Incident struct {
	Scope         string
	Reason        string
	OpenedAt      time.Time
	Bounded       bool
	Owner         string
	PolicyID      string
	PolicyVersion string
}

type Result struct {
	Status          Status
	EvidenceQuality EvidenceQuality
	Reason          string
	Incident        *Incident
}

var ErrInvalidPolicy = errors.New("telemetry health: invalid policy")

func (p Policy) Validate() error {
	for name, value := range map[string]string{"id": p.ID, "version": p.Version} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidPolicy, name)
		}
	}
	if p.StalenessBound <= 0 || p.IncidentAfter <= 0 || p.MaxQueueDepth < 0 || p.MaxDropCount < 0 || p.MaxExportErrors < 0 {
		return fmt.Errorf("%w: durations and limits must be positive or zero", ErrInvalidPolicy)
	}
	return nil
}

func unhealthy(p Policy, o Observation, now time.Time) (bool, string) {
	if o.ObservedAt.IsZero() || now.Before(o.ObservedAt) || now.Sub(o.ObservedAt) > p.StalenessBound {
		return true, "telemetry pipeline observation is stale or missing"
	}
	if o.Watermark.IsZero() || now.Before(o.Watermark) || now.Sub(o.Watermark) > p.StalenessBound {
		return true, "telemetry pipeline watermark is stale or missing"
	}
	if o.QueueDepth < 0 || o.DropCount < 0 || o.ExportErrors < 0 {
		return true, "telemetry pipeline reported an invalid negative value"
	}
	if !o.CollectorOK {
		return true, "telemetry collector is unhealthy"
	}
	if !o.ExporterOK {
		return true, "telemetry exporter is unhealthy"
	}
	if !o.PolicyOK {
		return true, "telemetry policy application failed"
	}
	if o.QueueDepth > p.MaxQueueDepth {
		return true, "telemetry queue exceeds its bound"
	}
	if o.DropCount > p.MaxDropCount {
		return true, "telemetry drops exceed their bound"
	}
	if o.ExportErrors > p.MaxExportErrors {
		return true, "telemetry export errors exceed their bound"
	}
	return false, "telemetry pipeline is healthy"
}

// Evaluate is a pure dependency check. A failure opens one bounded incident
// only after the configured interval has elapsed since FailureSince.
func Evaluate(p Policy, o Observation, now time.Time) (Result, error) {
	if err := p.Validate(); err != nil {
		return Result{}, err
	}
	bad, reason := unhealthy(p, o, now)
	if !bad {
		return Result{Status: StatusHealthy, EvidenceQuality: EvidenceComplete, Reason: reason}, nil
	}
	result := Result{Status: StatusUnknown, EvidenceQuality: EvidenceIncomplete, Reason: reason}
	failureSince := o.FailureSince
	if failureSince.IsZero() {
		failureSince = o.ObservedAt
	}
	if !failureSince.IsZero() && !now.Before(failureSince) && now.Sub(failureSince) >= p.IncidentAfter {
		result.Incident = &Incident{Scope: "telemetry-pipeline", Reason: reason, OpenedAt: now, Bounded: true, Owner: "telemetry-operations", PolicyID: p.ID, PolicyVersion: p.Version}
	}
	return result, nil
}

func Explain(result Result) string {
	incident := "none"
	if result.Incident != nil {
		incident = result.Incident.Scope
	}
	return fmt.Sprintf("telemetry health status=%s evidence=%s incident=%s: %s", result.Status, result.EvidenceQuality, incident, result.Reason)
}
