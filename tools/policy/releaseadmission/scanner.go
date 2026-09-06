package releaseadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ScannerEvidenceSchemaVersion is the immutable scanner-record schema.
const ScannerEvidenceSchemaVersion = 1

// ScannerKind identifies the required release security scan.
type ScannerKind string

const (
	ScannerSAST       ScannerKind = "SAST"
	ScannerSecret     ScannerKind = "SECRET_SCAN"
	ScannerIaC        ScannerKind = "IAC_SCAN"
	ScannerDAST       ScannerKind = "DAST"
	ScannerSASTReport ScannerKind = ScannerSAST
	ScannerSecretScan ScannerKind = ScannerSecret
	ScannerIaCScan    ScannerKind = ScannerIaC
	ScannerDASTReport ScannerKind = ScannerDAST
)

// RequiredScannerPolicyReports maps the required evidence records to their
// deterministic bundle filenames.
var RequiredScannerPolicyReports = []string{"sast", "secret_scan", "iac_scan", "dast"}

// RequiredScannerKinds returns a fresh copy of the four required scan kinds.
func RequiredScannerKinds() []ScannerKind {
	return []ScannerKind{ScannerSAST, ScannerSecret, ScannerIaC, ScannerDAST}
}

// ScannerPolicyReportName returns the bundle report name for kind.
func ScannerPolicyReportName(kind ScannerKind) string {
	switch kind {
	case ScannerSAST:
		return "sast"
	case ScannerSecret:
		return "secret_scan"
	case ScannerIaC:
		return "iac_scan"
	case ScannerDAST:
		return "dast"
	default:
		return ""
	}
}

// FindingSeverity is the scanner severity vocabulary used by admission.
type FindingSeverity string

const (
	SeverityInfo     FindingSeverity = "INFO"
	SeverityLow      FindingSeverity = "LOW"
	SeverityMedium   FindingSeverity = "MEDIUM"
	SeverityHigh     FindingSeverity = "HIGH"
	SeverityCritical FindingSeverity = "CRITICAL"
)

// TriageState records the disposition of one scanner finding.
type TriageState string

const (
	TriageOpen     TriageState = "OPEN"
	TriageReview   TriageState = "IN_REVIEW"
	TriageResolved TriageState = "RESOLVED"
	TriageExcepted TriageState = "EXCEPTED"
)

// ScannerException is a time-bounded, reviewed exception for one finding.
// It contains no finding payload or secret value.
type ScannerException struct {
	Reviewer  string    `json:"reviewer"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ScannerFinding is the digest-safe finding projection required for release
// admission. ID is a tool finding reference, never a raw secret or payload.
type ScannerFinding struct {
	ID        string            `json:"id"`
	Severity  FindingSeverity   `json:"severity"`
	Triage    TriageState       `json:"triage_state"`
	Exception *ScannerException `json:"exception,omitempty"`
}

// ScannerEvidence is an immutable, versioned scanner revision. Digest covers
// every field except Digest itself and is required for durable evidence.
type ScannerEvidence struct {
	SchemaVersion int              `json:"schema_version"`
	Kind          ScannerKind      `json:"kind"`
	Tool          string           `json:"tool"`
	Version       string           `json:"version"`
	ConfigDigest  string           `json:"config_digest"`
	Findings      []ScannerFinding `json:"findings"`
	Digest        string           `json:"digest"`
}

// ScannerValidationError names the exact field that made a record invalid.
type ScannerValidationError struct {
	Field string
	Issue string
}

func (e ScannerValidationError) Error() string {
	return "releaseadmission: scanner evidence " + e.Field + ": " + e.Issue
}

// NewScannerEvidence validates and freezes the input slice into a digested
// scanner revision.
func NewScannerEvidence(kind ScannerKind, tool, version, configDigest string, findings []ScannerFinding) (ScannerEvidence, error) {
	record := ScannerEvidence{
		SchemaVersion: ScannerEvidenceSchemaVersion,
		Kind:          kind,
		Tool:          strings.TrimSpace(tool),
		Version:       strings.TrimSpace(version),
		ConfigDigest:  strings.TrimSpace(configDigest),
		Findings:      cloneScannerFindings(findings),
	}
	sort.Slice(record.Findings, func(i, j int) bool { return record.Findings[i].ID < record.Findings[j].ID })
	if err := record.validateShape(); err != nil {
		return ScannerEvidence{}, err
	}
	digest, err := record.CanonicalDigest()
	if err != nil {
		return ScannerEvidence{}, err
	}
	record.Digest = digest
	return record, nil
}

func cloneScannerFindings(findings []ScannerFinding) []ScannerFinding {
	cloned := make([]ScannerFinding, len(findings))
	copy(cloned, findings)
	for i, finding := range cloned {
		if finding.Exception != nil {
			exception := *finding.Exception
			cloned[i].Exception = &exception
		}
	}
	return cloned
}

// CanonicalDigest returns the digest of the record without its Digest field.
func (e ScannerEvidence) CanonicalDigest() (string, error) {
	payload := struct {
		SchemaVersion int              `json:"schema_version"`
		Kind          ScannerKind      `json:"kind"`
		Tool          string           `json:"tool"`
		Version       string           `json:"version"`
		ConfigDigest  string           `json:"config_digest"`
		Findings      []ScannerFinding `json:"findings"`
	}{e.SchemaVersion, e.Kind, e.Tool, e.Version, e.ConfigDigest, e.Findings}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("releaseadmission: scanner evidence canonical encoding: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Validate verifies the record shape and immutable digest at now.
func (e ScannerEvidence) Validate(now time.Time) error {
	if err := e.validateShape(); err != nil {
		return err
	}
	digest, err := e.CanonicalDigest()
	if err != nil {
		return err
	}
	if e.Digest == "" {
		return ScannerValidationError{Field: "digest", Issue: "missing"}
	}
	if e.Digest != digest {
		return ScannerValidationError{Field: "digest", Issue: "does not match canonical revision"}
	}
	for i, finding := range e.Findings {
		if finding.Exception == nil {
			continue
		}
		if strings.TrimSpace(finding.Exception.Reviewer) == "" {
			return ScannerValidationError{Field: fmt.Sprintf("findings[%d].exception.reviewer", i), Issue: "missing"}
		}
		if finding.Exception.ExpiresAt.IsZero() {
			return ScannerValidationError{Field: fmt.Sprintf("findings[%d].exception.expires_at", i), Issue: "missing"}
		}
		if !finding.Exception.ExpiresAt.After(now) {
			return ScannerValidationError{Field: fmt.Sprintf("findings[%d].exception.expires_at", i), Issue: "must be in the future"}
		}
	}
	return nil
}

func (e ScannerEvidence) validateShape() error {
	if e.SchemaVersion != ScannerEvidenceSchemaVersion {
		return ScannerValidationError{Field: "schema_version", Issue: fmt.Sprintf("got %d, want %d", e.SchemaVersion, ScannerEvidenceSchemaVersion)}
	}
	if ScannerPolicyReportName(e.Kind) == "" {
		return ScannerValidationError{Field: "kind", Issue: "must be SAST, SECRET_SCAN, IAC_SCAN, or DAST"}
	}
	if e.Tool == "" {
		return ScannerValidationError{Field: "tool", Issue: "missing"}
	}
	if e.Version == "" {
		return ScannerValidationError{Field: "version", Issue: "missing"}
	}
	if e.ConfigDigest == "" {
		return ScannerValidationError{Field: "config_digest", Issue: "missing"}
	}
	seen := make(map[string]bool, len(e.Findings))
	for i, finding := range e.Findings {
		field := fmt.Sprintf("findings[%d]", i)
		if strings.TrimSpace(finding.ID) == "" {
			return ScannerValidationError{Field: field + ".id", Issue: "missing"}
		}
		if seen[finding.ID] {
			return ScannerValidationError{Field: field + ".id", Issue: "duplicate"}
		}
		seen[finding.ID] = true
		if !validSeverity(finding.Severity) {
			return ScannerValidationError{Field: field + ".severity", Issue: "unsupported"}
		}
		if !validTriage(finding.Triage) {
			return ScannerValidationError{Field: field + ".triage_state", Issue: "unsupported"}
		}
	}
	return nil
}

func validSeverity(severity FindingSeverity) bool {
	switch severity {
	case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
}

func validTriage(triage TriageState) bool {
	switch triage {
	case TriageOpen, TriageReview, TriageResolved, TriageExcepted:
		return true
	default:
		return false
	}
}

func isBlockingFinding(finding ScannerFinding, now time.Time) bool {
	if finding.Severity != SeverityHigh && finding.Severity != SeverityCritical {
		return false
	}
	if finding.Triage == TriageResolved {
		return false
	}
	return finding.Exception == nil || strings.TrimSpace(finding.Exception.Reviewer) == "" || finding.Exception.ExpiresAt.IsZero() || !finding.Exception.ExpiresAt.After(now)
}

func (e ScannerEvidence) blockingFindings(now time.Time) int {
	count := 0
	for _, finding := range e.Findings {
		if isBlockingFinding(finding, now) {
			count++
		}
	}
	return count
}

func scannerEvidenceDigest(records []ScannerEvidence) (string, error) {
	digests := make([]string, 0, len(records))
	for _, record := range records {
		digest := record.Digest
		if digest == "" {
			var err error
			digest, err = record.CanonicalDigest()
			if err != nil {
				return "", err
			}
		}
		digests = append(digests, string(record.Kind)+":"+digest)
	}
	sort.Strings(digests)
	b, err := json.Marshal(digests)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func evaluateScannerEvidence(records []ScannerEvidence, requireAll bool, now time.Time) []string {
	var reasons []string
	seen := make(map[ScannerKind]bool, len(records))
	for _, record := range records {
		kind := record.Kind
		fieldPrefix := "scanner_evidence." + ScannerPolicyReportName(kind)
		if seen[kind] {
			reasons = append(reasons, fieldPrefix+".kind: duplicate record")
			continue
		}
		seen[kind] = true
		if err := record.Validate(now); err != nil {
			reasons = append(reasons, fieldPrefix+": "+err.Error())
			continue
		}
		if blocked := record.blockingFindings(now); blocked != 0 {
			reasons = append(reasons, fmt.Sprintf("%s.findings: %d unresolved HIGH or CRITICAL finding(s) without a current reviewed exception", fieldPrefix, blocked))
		}
	}
	if requireAll {
		for _, kind := range RequiredScannerKinds() {
			if !seen[kind] {
				reasons = append(reasons, "scanner_evidence."+ScannerPolicyReportName(kind)+": required record is missing")
			}
		}
	}
	return reasons
}

// Explain returns counts and digests only, so raw scanner findings, secrets,
// account numbers, and identifiers never enter audit-facing text.
func (e ScannerEvidence) Explain() string {
	return fmt.Sprintf("scanner evidence %s %s/%s (config %s, findings %d, revision %s)", e.Kind, e.Tool, e.Version, e.ConfigDigest, len(e.Findings), e.Digest)
}
