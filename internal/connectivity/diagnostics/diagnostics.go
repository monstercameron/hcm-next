package diagnostics

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
)

// Status is the normalized outcome of one diagnostic check.
type Status string

const (
	Pass    Status = "PASS"
	Fail    Status = "FAIL"
	Unknown Status = "UNKNOWN"
	// Conventional prefixed aliases make call sites self-documenting.
	StatusPass    = Pass
	StatusFail    = Fail
	StatusUnknown = Unknown
)

// Check identifies a shared diagnostic check, independent of a provider.
type Check string

const (
	CheckAuthentication Check = "authentication"
	CheckReachability   Check = "reachability"
	CheckScopes         Check = "scopes"
	CheckCapability     Check = "capability"
)

// Probe is the read-only provider port used by Runner. Implementations must
// not mutate provider state. They may perform authentication and metadata
// reads, but must not return tokens or other credential material.
type Probe interface {
	Authenticate(context.Context) error
	Reachable(context.Context) error
	GrantedScopes(context.Context) ([]string, error)
	CheckCapability(context.Context, connectivity.Capability) error
}

// CapabilityImpact connects a requested capability to the workflows that
// depend on it. Workflow references are copied and sorted in the result.
type CapabilityImpact struct {
	Capability connectivity.Capability
	Workflows  []string
}

// Request describes one bounded, read-only diagnosis.
type Request struct {
	Connection      *connectivity.ConnectorConnection
	RequiredScopes  []string
	Capabilities    []CapabilityImpact
	HCMAuthZRef     string
	OrganizationRef string
	LegalPolicyRef  string
	EgressPolicyRef string
}

// Finding is one normalized check outcome. Detail is deliberately generated
// from stable, non-secret vocabulary; provider error strings never cross the
// diagnostic boundary.
type Finding struct {
	Check      Check
	Status     Status
	Code       string
	Detail     string
	Capability connectivity.Capability
	Required   []string
	Actual     []string
	Workflows  []string
}

// Report is the immutable result of a diagnosis. Findings are ordered by
// check and capability, and contain no credential material.
type Report struct {
	ConnectionID string
	TenantID     string
	Findings     []Finding
}

// Healthy reports whether all checks passed. Empty reports are not healthy.
func (r Report) Healthy() bool {
	if len(r.Findings) == 0 {
		return false
	}
	for _, f := range r.Findings {
		if f.Status != Pass {
			return false
		}
	}
	return true
}

// Runner executes a complete, read-only diagnosis.
type Runner struct{ probe Probe }

// NewRunner binds a provider probe. A nil probe is rejected to keep failures
// explicit rather than appearing as a healthy no-op.
func NewRunner(p Probe) (*Runner, error) {
	if p == nil {
		return nil, errors.New("diagnostics: nil probe")
	}
	return &Runner{probe: p}, nil
}

// Diagnose is the one-shot form of NewRunner(...).Run(...).
func Diagnose(ctx context.Context, p Probe, req Request) (Report, error) {
	runner, err := NewRunner(p)
	if err != nil {
		return Report{}, err
	}
	return runner.Run(ctx, req)
}

// Run performs authentication, reachability, scope and capability checks. It
// never transitions or otherwise mutates the supplied connection.
func (r *Runner) Run(ctx context.Context, req Request) (Report, error) {
	if r == nil || r.probe == nil {
		return Report{}, errors.New("diagnostics: nil probe")
	}
	if req.Connection == nil {
		return Report{}, errors.New("diagnostics: nil connection")
	}
	if strings.TrimSpace(req.Connection.ID()) == "" {
		return Report{}, errors.New("diagnostics: connection has no id")
	}
	required := normalize(req.RequiredScopes)
	impacts := append([]CapabilityImpact(nil), req.Capabilities...)
	for i := range impacts {
		if !impacts[i].Capability.Valid() {
			return Report{}, errors.New("diagnostics: invalid capability")
		}
		impacts[i].Workflows = normalize(impacts[i].Workflows)
	}
	sort.Slice(impacts, func(i, j int) bool { return impacts[i].Capability.String() < impacts[j].Capability.String() })
	out := Report{ConnectionID: req.Connection.ID(), TenantID: req.Connection.TenantID()}
	out.Findings = append(out.Findings, result(CheckAuthentication, r.probe.Authenticate(ctx), "authentication"))
	out.Findings = append(out.Findings, result(CheckReachability, r.probe.Reachable(ctx), "reachability"))
	actual, scopeErr := r.probe.GrantedScopes(ctx)
	actual = normalize(actual)
	f := Finding{Check: CheckScopes, Actual: actual, Required: append([]string(nil), required...)}
	if scopeErr != nil {
		f.Status, f.Code, f.Detail = classify(scopeErr, "scope")
	} else if missing := difference(required, actual); len(missing) != 0 {
		f.Status, f.Code, f.Detail = Fail, "MISSING_SCOPE", "required provider scope is not granted"
		f.Required = missing
	} else {
		f.Status, f.Code, f.Detail = Pass, "SCOPES_OK", "required provider scopes are granted"
	}
	out.Findings = append(out.Findings, f)
	for _, impact := range impacts {
		f := Finding{Check: CheckCapability, Capability: impact.Capability, Workflows: append([]string(nil), impact.Workflows...)}
		f.Status, f.Code, f.Detail = classify(r.probe.CheckCapability(ctx, impact.Capability), "capability")
		out.Findings = append(out.Findings, f)
	}
	return out, nil
}

func result(check Check, err error, noun string) Finding {
	f := Finding{Check: check}
	f.Status, f.Code, f.Detail = classify(err, noun)
	return f
}

func classify(err error, noun string) (Status, string, string) {
	if err == nil {
		return Pass, strings.ToUpper(noun) + "_OK", noun + " check passed"
	}
	if c, ok := connectivity.ClassOf(err); ok {
		switch c {
		case connectivity.ClassCredential:
			return Fail, "AUTHENTICATION_FAILED", "provider credential was rejected"
		case connectivity.ClassPermission:
			return Fail, "PERMISSION_DENIED", "provider permission is insufficient"
		case connectivity.ClassTransient:
			return Unknown, "PROVIDER_UNAVAILABLE", "provider check could not be completed"
		}
	}
	return Unknown, "PROVIDER_CHECK_FAILED", "provider check could not be completed"
}

func normalize(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
func difference(required, actual []string) []string {
	have := map[string]bool{}
	for _, v := range actual {
		have[v] = true
	}
	var out []string
	for _, v := range required {
		if !have[v] {
			out = append(out, v)
		}
	}
	return out
}
