package readiness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// RequirementField names one proven common-kernel field. The closed type
// prevents a conformance fixture from smuggling an untyped predicate into the
// shared engine.
type RequirementField string

const (
	FieldOrigin        RequirementField = "ORIGIN"
	FieldOwner         RequirementField = "OWNER"
	FieldEvidence      RequirementField = "EVIDENCE"
	FieldEffectiveTime RequirementField = "EFFECTIVE_TIME"
	FieldPolicy        RequirementField = "BLOCKING_CONDITIONAL_POLICY"
)

var commonRequirementFields = []RequirementField{
	FieldOrigin, FieldOwner, FieldEvidence, FieldEffectiveTime, FieldPolicy,
}

// ConformanceProfile is a code-shaped declaration from a domain owner. It
// proves the shared fields/statuses are usable while retaining a typed domain
// extension. It does not certify that a domain's production evaluator exists.
type ConformanceProfile struct {
	Domain            Domain
	Capability        string
	Version           string
	RequirementFields []RequirementField
	Statuses          []ReadinessStatus
	Extension         DomainExtension
}

func (p ConformanceProfile) Validate() error {
	if !p.Domain.Valid() || strings.TrimSpace(p.Capability) == "" || strings.TrimSpace(p.Version) == "" {
		return fmt.Errorf("readiness: conformance profile has invalid origin")
	}
	if err := p.Extension.Validate(); err != nil {
		return err
	}
	if !sameFields(p.RequirementFields, commonRequirementFields) {
		return fmt.Errorf("readiness: %s does not prove the common requirement fields", p.Domain)
	}
	if !sameStatuses(p.Statuses) {
		return fmt.Errorf("readiness: %s does not prove the four common statuses", p.Domain)
	}
	return nil
}

// ConformanceReport records the exact shared kernel and each domain's typed
// extension. Its digest makes the proof artifact replayable.
type ConformanceReport struct {
	StableKernel      bool
	RequirementFields []RequirementField
	Statuses          []RequirementStatus
	Profiles          []ConformanceProfile
	Digest            string
}

// DefaultConformanceProfiles returns the four design-proof profiles in a
// deterministic order. These are intentionally explicit fixtures, not hidden
// domain rules.
func DefaultConformanceProfiles() []ConformanceProfile {
	fields := append([]RequirementField(nil), commonRequirementFields...)
	statuses := []ReadinessStatus{StatusReady, StatusConditional, StatusNotReady, StatusUnknown}
	profile := func(domain Domain, capability, extensionKind, extensionRef string) ConformanceProfile {
		return ConformanceProfile{
			Domain: domain, Capability: capability, Version: "v1",
			RequirementFields: append([]RequirementField(nil), fields...),
			Statuses:          append([]ReadinessStatus(nil), statuses...),
			Extension:         DomainExtension{Kind: extensionKind, Version: "v1", Ref: extensionRef},
		}
	}
	return []ConformanceProfile{
		profile(DomainReturnToWork, "workforce.return_to_work", "leave_restriction", "workforce.return_to_work"),
		profile(DomainOnboarding, "lifecycle.onboarding", "onboarding_context", "lifecycle.onboarding"),
		profile(DomainPayrollRelease, "payroll.release", "payroll_cutoff", "payroll.release"),
		profile(DomainRecoveryGoLive, "recovery.go_live", "recovery_health", "recovery.go_live"),
	}
}

// ProveConformance validates that exactly the four profiled domains share the
// common requirement/status kernel without erasing their extension points.
func ProveConformance(profiles []ConformanceProfile) (ConformanceReport, error) {
	if len(profiles) != 4 {
		return ConformanceReport{}, fmt.Errorf("readiness: want four conformance profiles, got %d", len(profiles))
	}
	ordered := append([]ConformanceProfile(nil), profiles...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Domain < ordered[j].Domain })
	seen := make(map[Domain]struct{}, len(ordered))
	for i, profile := range ordered {
		if err := profile.Validate(); err != nil {
			return ConformanceReport{}, err
		}
		if _, ok := seen[profile.Domain]; ok {
			return ConformanceReport{}, fmt.Errorf("readiness: duplicate conformance domain %s", profile.Domain)
		}
		seen[profile.Domain] = struct{}{}
		ordered[i].RequirementFields = append([]RequirementField(nil), profile.RequirementFields...)
		ordered[i].Statuses = append([]ReadinessStatus(nil), profile.Statuses...)
	}
	for domain := range domains {
		if _, ok := seen[domain]; !ok {
			return ConformanceReport{}, fmt.Errorf("readiness: missing conformance domain %s", domain)
		}
	}
	report := ConformanceReport{
		StableKernel:      true,
		RequirementFields: append([]RequirementField(nil), commonRequirementFields...),
		Statuses:          []RequirementStatus{StatusReady, StatusConditional, StatusNotReady, StatusUnknown},
		Profiles:          ordered,
	}
	w := canonicalbytes.New("hcmnext.engines.readiness.ConformanceReport", schemaVersion).
		Bool("stable_kernel", report.StableKernel).Count("fields", len(report.RequirementFields))
	for _, field := range report.RequirementFields {
		w.String("field", string(field))
	}
	for _, status := range report.Statuses {
		w.String("status", status.String())
	}
	for _, profile := range report.Profiles {
		w.String("domain", profile.Domain.String()).String("capability", profile.Capability).
			String("version", profile.Version).Value("extension", profile.Extension)
	}
	var err error
	report.Digest, err = w.Digest()
	if err != nil {
		return ConformanceReport{}, err
	}
	return report, nil
}

func (r ConformanceReport) Explain() string {
	return fmt.Sprintf("readiness conformance %s: stable common kernel across %d domains", r.Digest, len(r.Profiles))
}

func sameFields(got, want []RequirementField) bool {
	if len(got) != len(want) {
		return false
	}
	left, right := append([]RequirementField(nil), got...), append([]RequirementField(nil), want...)
	sort.Slice(left, func(i, j int) bool { return left[i] < left[j] })
	sort.Slice(right, func(i, j int) bool { return right[i] < right[j] })
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sameStatuses(got []ReadinessStatus) bool {
	want := []ReadinessStatus{StatusReady, StatusConditional, StatusNotReady, StatusUnknown}
	if len(got) != len(want) {
		return false
	}
	left, right := append([]ReadinessStatus(nil), got...), append([]ReadinessStatus(nil), want...)
	sort.Slice(left, func(i, j int) bool { return left[i] < left[j] })
	sort.Slice(right, func(i, j int) bool { return right[i] < right[j] })
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
