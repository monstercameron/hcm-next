package operations

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultOperatingEvidencePath is OPS-010's checked-in operating-evidence
	// register. It supplements the OPS-007 ownership and dependency registries
	// with the independent runbook, restore, drill, vendor, and control proof
	// needed for an operating gate.
	DefaultOperatingEvidencePath = "definitions/operations/operating-evidence.yaml"

	// StatusEvidenceRejected is an operating gate result, distinct from
	// OPS-007's structural registry result.
	StatusEvidenceRejected = "OPS_010_REJECTED"

	EvidenceCurrent = "CURRENT"
	EvidenceDue     = "DUE"
	EvidenceExpired = "EXPIRED"
)

var requiredEvidenceKinds = []string{
	"OWNERSHIP", "RUNBOOK", "DEPENDENCY", "RESTORE", "DRILL", "VENDOR", "CONTROL",
}

// OperatingEvidenceRegistry is the versioned, checked-in proof inventory for
// an operating readiness gate. It is evidence consumed by planning/release
// checks, not runtime configuration and not an authority to execute work.
type OperatingEvidenceRegistry struct {
	Version     int                 `yaml:"version"`
	Module      string              `yaml:"module"`
	EffectiveAt string              `yaml:"effective_at"`
	Evidence    []OperatingEvidence `yaml:"evidence"`
}

// OperatingEvidence is one independently renewable operating proof. A proof
// goes DUE before it expires, making the need for renewal observable before it
// silently becomes a gate failure.
type OperatingEvidence struct {
	ID          string          `yaml:"id"`
	Kind        string          `yaml:"kind"`
	Gate        string          `yaml:"gate"`
	Scope       string          `yaml:"scope"`
	Owner       string          `yaml:"owner"`
	Version     string          `yaml:"version"`
	EvidenceRef string          `yaml:"evidence_ref"`
	VerifiedAt  string          `yaml:"verified_at"`
	DueAt       string          `yaml:"due_at"`
	ExpiresAt   string          `yaml:"expires_at"`
	Waiver      *EvidenceWaiver `yaml:"waiver"`
}

// EvidenceWaiver is the narrow, temporary compensating-control path for
// evidence that is due or expired. A waiver does not make stale proof current:
// the assessment remains DUE or EXPIRED, but the named gate may proceed only
// while the signed waiver itself is complete and unexpired.
type EvidenceWaiver struct {
	ID                  string `yaml:"id"`
	SignedBy            string `yaml:"signed_by"`
	SignedAt            string `yaml:"signed_at"`
	SignatureRef        string `yaml:"signature_ref"`
	Reason              string `yaml:"reason"`
	CompensatingControl string `yaml:"compensating_control"`
	ExpiresAt           string `yaml:"expires_at"`
}

// CurrencyDiagnostic identifies the exact evidence field and state blocking
// a gate. Callers never need to parse prose to determine which record needs
// renewal or waiver review.
type CurrencyDiagnostic struct {
	EvidenceID string
	Kind       string
	Gate       string
	Field      string
	State      string
	Version    string
	Reason     string
}

func (d CurrencyDiagnostic) String() string {
	return fmt.Sprintf("evidence=%s kind=%s gate=%s field=%s state=%s version=%s: %s",
		d.EvidenceID, d.Kind, d.Gate, d.Field, d.State, d.Version, d.Reason)
}

// EvidenceAssessment records the currency state of one proof even when a
// valid waiver permits its gate. This prevents a waived stale proof from being
// reported as CURRENT.
type EvidenceAssessment struct {
	EvidenceID  string
	Kind        string
	Gate        string
	Version     string
	State       string
	GateAllowed bool
	WaiverID    string
}

// ZeroEffects makes the zero-effect contract explicit in a form release
// tooling can assert. ValidateEvidenceCurrency has no store, event, outbox,
// human-work, or provider port, so every result is necessarily all zero.
type ZeroEffects struct {
	AuthoritativeRows int
	BusinessEvents    int
	OutboxEntries     int
	HumanWorkItems    int
	ProviderRequests  int
}

// Empty reports whether the admission check had no effects.
func (e ZeroEffects) Empty() bool {
	return e.AuthoritativeRows == 0 && e.BusinessEvents == 0 && e.OutboxEntries == 0 &&
		e.HumanWorkItems == 0 && e.ProviderRequests == 0
}

// CurrencyResult is the side-effect-free OPS-010 readiness result.
type CurrencyResult struct {
	Status      string
	Assessments []EvidenceAssessment
	Diagnostics []CurrencyDiagnostic
	Effects     ZeroEffects
}

// Ready reports whether every required operating proof is current or covered
// by a complete, active, signed, compensating-control waiver.
func (r CurrencyResult) Ready() bool { return r.Status == StatusReady }

// LoadOperatingEvidence loads OPS-010's versioned operating evidence
// registry. Parsing has no policy effect; callers must pass the parsed record
// to ValidateEvidenceCurrency at an explicit, deterministic clock instant.
func LoadOperatingEvidence(path string) (*OperatingEvidenceRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("operations: reading operating evidence %s: %w", path, err)
	}
	var registry OperatingEvidenceRegistry
	if err := yaml.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("operations: parsing operating evidence %s: %w", path, err)
	}
	return &registry, nil
}

// ValidateEvidenceCurrency evaluates operating-evidence freshness at now. now
// is deliberately passed by the caller rather than sampled from time.Now, so a
// release decision is reproducible and testable. The function is pure: it
// neither persists nor dispatches anything, and returns zero Effects.
func ValidateEvidenceCurrency(registry *OperatingEvidenceRegistry, now time.Time) CurrencyResult {
	result := CurrencyResult{Effects: ZeroEffects{}}
	if registry == nil {
		result.Diagnostics = append(result.Diagnostics, CurrencyDiagnostic{
			Field: "registry", State: "MISSING", Reason: "operating evidence registry is required",
		})
		result.Status = StatusEvidenceRejected
		return result
	}

	if registry.Version != 1 {
		result.Diagnostics = append(result.Diagnostics, CurrencyDiagnostic{
			Field: "version", State: "UNSUPPORTED", Version: fmt.Sprint(registry.Version), Reason: "registry version must be 1",
		})
	}
	if strings.TrimSpace(registry.Module) == "" {
		result.Diagnostics = append(result.Diagnostics, CurrencyDiagnostic{
			Field: "module", State: "MISSING", Reason: "module is required",
		})
	}
	if _, err := parseRFC3339(registry.EffectiveAt); err != nil {
		result.Diagnostics = append(result.Diagnostics, CurrencyDiagnostic{
			Field: "effective_at", State: evidenceTimeState(registry.EffectiveAt, err), Reason: "effective_at must be RFC3339",
		})
	}
	if len(registry.Evidence) == 0 {
		result.Diagnostics = append(result.Diagnostics, CurrencyDiagnostic{
			Field: "evidence", State: "MISSING", Reason: "at least one operating-evidence row is required",
		})
	}

	evidence := append([]OperatingEvidence(nil), registry.Evidence...)
	sort.SliceStable(evidence, func(i, j int) bool { return evidence[i].ID < evidence[j].ID })
	seenIDs := make(map[string]bool, len(evidence))
	seenKinds := make(map[string]bool, len(requiredEvidenceKinds))
	for _, entry := range evidence {
		assessment, diagnostics := validateOperatingEvidence(entry, seenIDs, now)
		result.Assessments = append(result.Assessments, assessment)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		if validEvidenceKind(entry.Kind) {
			seenKinds[entry.Kind] = true
		}
	}
	for _, kind := range requiredEvidenceKinds {
		if !seenKinds[kind] {
			result.Diagnostics = append(result.Diagnostics, CurrencyDiagnostic{
				Kind: kind, Field: "kind", State: "MISSING", Reason: "required operating-evidence kind is not registered",
			})
		}
	}

	if len(result.Diagnostics) == 0 {
		result.Status = StatusReady
	} else {
		result.Status = StatusEvidenceRejected
	}
	return result
}

func validateOperatingEvidence(entry OperatingEvidence, seenIDs map[string]bool, now time.Time) (EvidenceAssessment, []CurrencyDiagnostic) {
	assessment := EvidenceAssessment{
		EvidenceID: entry.ID, Kind: entry.Kind, Gate: entry.Gate, Version: entry.Version,
		State: "INVALID",
	}
	var diagnostics []CurrencyDiagnostic
	add := func(field, state, reason string) {
		diagnostics = append(diagnostics, CurrencyDiagnostic{
			EvidenceID: entry.ID, Kind: entry.Kind, Gate: entry.Gate, Field: field,
			State: state, Version: entry.Version, Reason: reason,
		})
	}

	if strings.TrimSpace(entry.ID) == "" {
		add("id", "MISSING", "evidence id is required")
	} else if seenIDs[entry.ID] {
		add("id", "DUPLICATE", "evidence id is registered more than once")
	} else {
		seenIDs[entry.ID] = true
	}
	if !validEvidenceKind(entry.Kind) {
		add("kind", "INVALID", "kind must be OWNERSHIP, RUNBOOK, DEPENDENCY, RESTORE, DRILL, VENDOR or CONTROL")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"gate", entry.Gate},
		{"scope", entry.Scope},
		{"owner", entry.Owner},
		{"version", entry.Version},
		{"evidence_ref", entry.EvidenceRef},
		{"verified_at", entry.VerifiedAt},
		{"due_at", entry.DueAt},
		{"expires_at", entry.ExpiresAt},
	} {
		if strings.TrimSpace(field.value) == "" {
			add(field.name, "MISSING", field.name+" is required")
		}
	}

	verified, verifiedErr := parseRFC3339(entry.VerifiedAt)
	due, dueErr := parseRFC3339(entry.DueAt)
	expires, expiresErr := parseRFC3339(entry.ExpiresAt)
	if verifiedErr != nil {
		add("verified_at", evidenceTimeState(entry.VerifiedAt, verifiedErr), "verified_at must be RFC3339")
	}
	if dueErr != nil {
		add("due_at", evidenceTimeState(entry.DueAt, dueErr), "due_at must be RFC3339")
	}
	if expiresErr != nil {
		add("expires_at", evidenceTimeState(entry.ExpiresAt, expiresErr), "expires_at must be RFC3339")
	}
	if verifiedErr == nil && verified.After(now) {
		add("verified_at", "FUTURE", "verification cannot be in the future")
	}
	if verifiedErr == nil && dueErr == nil && !due.After(verified) {
		add("due_at", "INVALID", "due_at must be after verified_at")
	}
	if dueErr == nil && expiresErr == nil && !expires.After(due) {
		add("expires_at", "INVALID", "expires_at must be after due_at")
	}
	if len(diagnostics) > 0 {
		return assessment, diagnostics
	}

	switch {
	case !expires.After(now):
		assessment.State = EvidenceExpired
	case !due.After(now):
		assessment.State = EvidenceDue
	default:
		assessment.State = EvidenceCurrent
		assessment.GateAllowed = true
	}
	if assessment.State == EvidenceCurrent {
		if entry.Waiver != nil {
			add("waiver", "INVALID", "a waiver may only cover DUE or EXPIRED evidence")
			assessment.GateAllowed = false
		}
		return assessment, diagnostics
	}

	if entry.Waiver == nil {
		add("expires_at", assessment.State, "operating evidence is no longer current and has no active signed waiver")
		return assessment, diagnostics
	}
	waiverID, waiverDiagnostics := validateActiveWaiver(entry, now)
	assessment.WaiverID = waiverID
	if len(waiverDiagnostics) > 0 {
		assessment.GateAllowed = false
		return assessment, append(diagnostics, waiverDiagnostics...)
	}
	assessment.GateAllowed = true
	return assessment, diagnostics
}

func validateActiveWaiver(entry OperatingEvidence, now time.Time) (string, []CurrencyDiagnostic) {
	w := entry.Waiver
	var diagnostics []CurrencyDiagnostic
	add := func(field, state, reason string) {
		diagnostics = append(diagnostics, CurrencyDiagnostic{
			EvidenceID: entry.ID, Kind: entry.Kind, Gate: entry.Gate, Field: field,
			State: state, Version: entry.Version, Reason: reason,
		})
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"waiver.id", w.ID},
		{"waiver.signed_by", w.SignedBy},
		{"waiver.signed_at", w.SignedAt},
		{"waiver.signature_ref", w.SignatureRef},
		{"waiver.reason", w.Reason},
		{"waiver.compensating_control", w.CompensatingControl},
		{"waiver.expires_at", w.ExpiresAt},
	} {
		if strings.TrimSpace(field.value) == "" {
			add(field.name, "MISSING", field.name+" is required")
		}
	}
	signed, signedErr := parseRFC3339(w.SignedAt)
	expires, expiresErr := parseRFC3339(w.ExpiresAt)
	if signedErr != nil {
		add("waiver.signed_at", evidenceTimeState(w.SignedAt, signedErr), "waiver.signed_at must be RFC3339")
	}
	if expiresErr != nil {
		add("waiver.expires_at", evidenceTimeState(w.ExpiresAt, expiresErr), "waiver.expires_at must be RFC3339")
	}
	if signedErr == nil && signed.After(now) {
		add("waiver.signed_at", "FUTURE", "waiver signature cannot be in the future")
	}
	if signedErr == nil && expiresErr == nil && !expires.After(signed) {
		add("waiver.expires_at", "INVALID", "waiver.expires_at must be after waiver.signed_at")
	}
	if expiresErr == nil && !expires.After(now) {
		add("waiver.expires_at", EvidenceExpired, "waiver has expired")
	}
	return w.ID, diagnostics
}

func parseRFC3339(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	return time.Parse(time.RFC3339, value)
}

func evidenceTimeState(value string, err error) string {
	if strings.TrimSpace(value) == "" {
		return "MISSING"
	}
	return "INVALID"
}

func validEvidenceKind(kind string) bool {
	for _, candidate := range requiredEvidenceKinds {
		if kind == candidate {
			return true
		}
	}
	return false
}
