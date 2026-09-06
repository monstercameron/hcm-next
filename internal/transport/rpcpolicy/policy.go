// Package rpcpolicy projects the reviewed endpoint manifest into executable
// gRPC replay policy.  It owns transport timing and replay classification;
// it never decides a capability's business authorization or outcome.
package rpcpolicy

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/transport/manifest"
)

const ContractVersion = "hcmnext.grpc-replay-policy/v1"

type HedgingPolicy string

const (
	HedgingProhibited HedgingPolicy = "PROHIBITED"
)

type Outcome string

const (
	OutcomeUnknown      Outcome = "UNKNOWN"
	OutcomeNotCommitted Outcome = "NOT_COMMITTED"
	OutcomeCommitted    Outcome = "COMMITTED"
)

type Decision string

const (
	DecisionReturn           Decision = "RETURN"
	DecisionRetry            Decision = "RETRY"
	DecisionResolveAmbiguous Decision = "RESOLVE_AMBIGUOUS"
)

// MethodPolicy is the complete replay contract for one public capability
// method. Status names use canonical gRPC names so the policy can be consumed
// by grpc-go, grpcbridge, or another transport without leaking their types.
type MethodPolicy struct {
	Procedure            string
	Deadline             time.Duration
	WaitForReady         bool
	Idempotency          manifest.IdempotencyClass
	RetryableStatusCodes []string
	MaxAttempts          int
	Hedging              HedgingPolicy
}

func (p MethodPolicy) Explain() string {
	return fmt.Sprintf("procedure=%s deadline=%s idempotency=%s attempts=%d hedging=%s", p.Procedure, p.Deadline, p.Idempotency, p.MaxAttempts, p.Hedging)
}

type Manifest struct {
	Version int
	Methods []MethodPolicy
}

func (p MethodPolicy) Validate() error {
	if strings.TrimSpace(p.Procedure) == "" || p.Deadline <= 0 || !p.Idempotency.Valid() || p.MaxAttempts < 1 || p.Hedging != HedgingProhibited {
		return fmt.Errorf("rpc policy: incomplete method policy for %q", p.Procedure)
	}
	seen := make(map[string]struct{}, len(p.RetryableStatusCodes))
	for _, code := range p.RetryableStatusCodes {
		if strings.TrimSpace(code) == "" {
			return fmt.Errorf("rpc policy: duplicate or empty retry status for %q", p.Procedure)
		}
		if _, ok := seen[code]; ok {
			return fmt.Errorf("rpc policy: duplicate or empty retry status for %q", p.Procedure)
		}
		seen[code] = struct{}{}
	}
	if p.Idempotency == manifest.IdempotencyReadSafe && !p.WaitForReady {
		return fmt.Errorf("rpc policy: read-safe method %q must declare wait-for-ready", p.Procedure)
	}
	if p.Idempotency != manifest.IdempotencyReadSafe && p.WaitForReady {
		return fmt.Errorf("rpc policy: effect-bearing method %q must not wait-for-ready", p.Procedure)
	}
	return nil
}

// Decide is deliberately outcome-aware.  A deadline or unavailable result
// does not prove that an effect was absent; callers must resolve UNKNOWN or
// COMMITTED before replaying an effect-bearing method.
func (p MethodPolicy) Decide(status string, outcome Outcome, attempt int) Decision {
	if attempt < 1 || status == "OK" {
		return DecisionReturn
	}
	if p.Idempotency != manifest.IdempotencyReadSafe {
		switch outcome {
		case OutcomeCommitted, OutcomeUnknown:
			return DecisionResolveAmbiguous
		}
	}
	if attempt >= p.MaxAttempts || !contains(p.RetryableStatusCodes, status) {
		return DecisionReturn
	}
	return DecisionRetry
}

func Build() (Manifest, error) {
	endpoints, err := manifest.Build()
	if err != nil {
		return Manifest{}, err
	}
	return BuildFromEndpointManifest(endpoints)
}

func BuildFromEndpointManifest(endpoints *manifest.EndpointManifest) (Manifest, error) {
	if endpoints == nil || len(endpoints.Endpoints) == 0 {
		return Manifest{}, fmt.Errorf("rpc policy: endpoint manifest is empty")
	}
	out := Manifest{Version: 1, Methods: make([]MethodPolicy, 0, len(endpoints.Endpoints))}
	for _, endpoint := range endpoints.Endpoints {
		policy, err := Classify(endpoint)
		if err != nil {
			return Manifest{}, err
		}
		out.Methods = append(out.Methods, policy)
	}
	sort.Slice(out.Methods, func(i, j int) bool { return out.Methods[i].Procedure < out.Methods[j].Procedure })
	return out, nil
}

func Classify(endpoint manifest.EndpointDefinition) (MethodPolicy, error) {
	if endpoint.DeadlineBudgetMillis <= 0 || strings.TrimSpace(endpoint.GRPCProcedure) == "" {
		return MethodPolicy{}, fmt.Errorf("rpc policy: endpoint %q has no bounded deadline or procedure", endpoint.EndpointID)
	}
	p := MethodPolicy{Procedure: endpoint.GRPCProcedure, Deadline: time.Duration(endpoint.DeadlineBudgetMillis) * time.Millisecond, Idempotency: endpoint.IdempotencyClass, Hedging: HedgingProhibited}
	switch endpoint.IdempotencyClass {
	case manifest.IdempotencyReadSafe:
		p.WaitForReady = true
		p.RetryableStatusCodes = []string{"RESOURCE_EXHAUSTED", "UNAVAILABLE"}
		p.MaxAttempts = 3
	case manifest.IdempotencyKey:
		p.WaitForReady = false
		p.RetryableStatusCodes = []string{"UNAVAILABLE"}
		p.MaxAttempts = 2
	case manifest.IdempotencyNone:
		p.WaitForReady = false
		p.MaxAttempts = 1
	default:
		return MethodPolicy{}, fmt.Errorf("rpc policy: endpoint %q has invalid idempotency class %q", endpoint.EndpointID, endpoint.IdempotencyClass)
	}
	if err := p.Validate(); err != nil {
		return MethodPolicy{}, err
	}
	return p, nil
}

func (m Manifest) Validate() error {
	if m.Version != 1 || len(m.Methods) == 0 {
		return fmt.Errorf("rpc policy: invalid manifest")
	}
	seen := make(map[string]struct{}, len(m.Methods))
	for _, method := range m.Methods {
		if err := method.Validate(); err != nil {
			return err
		}
		if _, ok := seen[method.Procedure]; ok {
			return fmt.Errorf("rpc policy: duplicate procedure %q", method.Procedure)
		}
		seen[method.Procedure] = struct{}{}
	}
	return nil
}

func (m Manifest) Lookup(procedure string) (MethodPolicy, bool) {
	for _, method := range m.Methods {
		if method.Procedure == procedure {
			return method, true
		}
	}
	return MethodPolicy{}, false
}

func (m Manifest) Explain() string {
	return fmt.Sprintf("%s methods=%d hedging=%s", ContractVersion, len(m.Methods), HedgingProhibited)
}

func Explain(m Manifest) string { return m.Explain() }

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
