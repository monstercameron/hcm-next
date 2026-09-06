// Package synthetic evaluates production synthetic journeys without granting
// the journey a write-capable identity or a route to an external effect.
package synthetic

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func Version() int { return 1 }

var (
	ErrInvalidJourney = errors.New("synthetic: invalid journey")
	ErrEffectFenced   = errors.New("synthetic: effect fenced")
)

type Status string

const (
	StatusReady    Status = "READY"
	StatusDegraded Status = "DEGRADED"
	StatusBlocked  Status = "BLOCKED"
)

type Layer string

const (
	LayerEdge       Layer = "EDGE"
	LayerAuth       Layer = "AUTH"
	LayerConfig     Layer = "CONFIG"
	LayerRead       Layer = "READ"
	LayerSimulation Layer = "SIMULATION"
	LayerTelemetry  Layer = "TELEMETRY"
)

var requiredLayers = []Layer{LayerEdge, LayerAuth, LayerConfig, LayerRead, LayerSimulation, LayerTelemetry}

type Effect string

const (
	EffectDomainWrite    Effect = "DOMAIN_WRITE"
	EffectIntentCreate   Effect = "INTENT_CREATE"
	EffectWorkItem       Effect = "WORK_ITEM"
	EffectMessageSend    Effect = "MESSAGE_SEND"
	EffectOutboxPublish  Effect = "OUTBOX_PUBLISH"
	EffectProviderCall   Effect = "PROVIDER_CALL"
	EffectTelemetryWrite Effect = "CUSTOMER_TELEMETRY_WRITE"
)

type Step struct {
	Name     string
	Layer    Layer
	TenantID string
	ReadOnly bool
}

type Dependency struct {
	Name      string
	Reachable bool
	Detail    string
}

// Journey is the typed production-synthetic contract. It deliberately has no
// payload or callback field: evaluation consumes observations and can never
// execute a business or provider operation.
type Journey struct {
	TenantID                string
	PrincipalID             string
	ExecutionMode           string
	Purpose                 string
	CredentialReadOnly      bool
	CustomerMetricsExcluded bool
	Steps                   []Step
	Dependencies            []Dependency
	AttemptedEffects        []Effect
}

type Result struct {
	Status                  Status
	TenantID                string
	PrincipalID             string
	DependencyFailures      []string
	FencedEffects           []Effect
	ObservedLayers          []Layer
	CustomerMetricsExcluded bool
	Digest                  string
}

func (j Journey) Validate() error {
	if !strings.HasPrefix(j.TenantID, "synthetic-") || strings.TrimSpace(j.TenantID) == "synthetic-" {
		return fmt.Errorf("%w: tenant must use the synthetic- prefix", ErrInvalidJourney)
	}
	if !strings.HasPrefix(j.PrincipalID, "synthetic-") || strings.TrimSpace(j.PrincipalID) == "synthetic-" {
		return fmt.Errorf("%w: principal must use the synthetic- prefix", ErrInvalidJourney)
	}
	if j.ExecutionMode != "PRODUCTION_SYNTHETIC" {
		return fmt.Errorf("%w: execution mode must be PRODUCTION_SYNTHETIC", ErrInvalidJourney)
	}
	if strings.TrimSpace(j.Purpose) == "" || !j.CredentialReadOnly || !j.CustomerMetricsExcluded {
		return fmt.Errorf("%w: purpose, read-only credential and metric exclusion are required", ErrInvalidJourney)
	}
	seen := make(map[Layer]bool, len(j.Steps))
	for _, step := range j.Steps {
		if strings.TrimSpace(step.Name) == "" || step.TenantID != j.TenantID || !step.ReadOnly {
			return fmt.Errorf("%w: step %q is not a tenant-scoped read", ErrInvalidJourney, step.Name)
		}
		seen[step.Layer] = true
	}
	for _, layer := range requiredLayers {
		if !seen[layer] {
			return fmt.Errorf("%w: required layer %s is absent", ErrInvalidJourney, layer)
		}
	}
	for _, effect := range j.AttemptedEffects {
		if effect == "" {
			return fmt.Errorf("%w: empty effect class", ErrInvalidJourney)
		}
	}
	return nil
}

// Evaluate compiles the observed path into bounded operational evidence. A
// dependency failure degrades readiness; every attempted effect is recorded as
// fenced and is never forwarded.
func Evaluate(j Journey) (Result, error) {
	if err := j.Validate(); err != nil {
		return Result{}, err
	}
	r := Result{
		Status: StatusReady, TenantID: j.TenantID, PrincipalID: j.PrincipalID,
		CustomerMetricsExcluded: j.CustomerMetricsExcluded,
		FencedEffects:           append([]Effect(nil), j.AttemptedEffects...),
	}
	for _, step := range j.Steps {
		r.ObservedLayers = append(r.ObservedLayers, step.Layer)
	}
	for _, dependency := range j.Dependencies {
		if !dependency.Reachable {
			r.Status = StatusDegraded
			r.DependencyFailures = append(r.DependencyFailures, dependency.Name)
		}
	}
	sort.Strings(r.DependencyFailures)
	if len(r.FencedEffects) > 0 {
		// The fence is evidence of a safe probe, not a failed journey.
		for _, effect := range r.FencedEffects {
			if effect == EffectDomainWrite || effect == EffectIntentCreate || effect == EffectProviderCall {
				continue
			}
		}
	}
	r.Digest = digestResult(r)
	return r, nil
}

// Refuse is the explicit adapter seam for callers that want to model an
// attempted effect. It always returns ErrEffectFenced.
func Refuse(effect Effect) error {
	if effect == "" {
		return fmt.Errorf("%w: empty effect", ErrInvalidJourney)
	}
	return fmt.Errorf("%w: %s", ErrEffectFenced, effect)
}

func (r Result) Explain() string {
	return fmt.Sprintf("synthetic status=%s tenant=%s dependencies_failed=%d fenced_effects=%d metrics_excluded=%t digest=%s", r.Status, r.TenantID, len(r.DependencyFailures), len(r.FencedEffects), r.CustomerMetricsExcluded, r.Digest)
}

func Explain(r Result) string { return r.Explain() }

func digestResult(r Result) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1|%s|%s|%s|%t|", r.Status, r.TenantID, r.PrincipalID, r.CustomerMetricsExcluded)
	for _, layer := range r.ObservedLayers {
		fmt.Fprintf(h, "%s|", layer)
	}
	for _, failure := range r.DependencyFailures {
		fmt.Fprintf(h, "failure:%s|", failure)
	}
	for _, effect := range r.FencedEffects {
		fmt.Fprintf(h, "fenced:%s|", effect)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
