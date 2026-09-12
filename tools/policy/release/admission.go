package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// CICD-004 fail-closed deployment admission.
//
// This decision sits one layer above VerifyBundle: verifying a bundle's
// signature only proves the bundle was not tampered with and was signed by
// a trusted key. It says nothing about whether that particular, exact
// build is the one a human or automated approval actually authorized for
// this deployment scope. Admit answers that second, narrower question, and
// every one of its reject paths fails closed: an absent, empty, or
// zero-valued input is never treated as "permitted".

// Sentinel reject reasons. Each is distinct so a caller can branch with
// errors.Is; the error Admit returns always wraps exactly one of these via
// %w, so a caller never has to parse message text to know why a candidate
// was rejected.
var (
	// ErrCandidateUnsigned covers both an unsigned bundle and one signed by
	// a key outside the configured trust set, or one whose signature no
	// longer verifies (for example because an artifact was altered after
	// signing). VerifyBundle is the single source of truth for all of
	// these; from the admission decision's point of view they are the same
	// failure: the candidate is not a trustworthy artifact.
	ErrCandidateUnsigned = errors.New("release: candidate is unsigned or its signature is not trusted")
	// ErrCandidateVulnerable means the candidate carries unresolved
	// vulnerability findings after scanning.
	ErrCandidateVulnerable = errors.New("release: candidate has unresolved vulnerability findings")
	// ErrCandidateIncompatible means the candidate does not match the exact
	// approved deployment target: either its verified manifest digest is
	// not the one digest that was approved, or it was offered for a scope
	// it was not approved for. A candidate that verifies cleanly is not
	// thereby admissible — only the one exact approved digest/scope pair
	// is.
	ErrCandidateIncompatible = errors.New("release: candidate does not match the exact approved deployment target")
	// ErrConformanceStale means the candidate's conformance evidence is
	// older than the policy's allowed window, measured against the
	// injected evaluation time.
	ErrConformanceStale = errors.New("release: candidate conformance evidence is older than the allowed window")
	// ErrEvidenceIncomplete means some required input to the decision is
	// absent or its Go zero value: an empty trusted-key set, an unset
	// approval target, an unconfigured staleness window, absent
	// vulnerability-scan evidence, or a conformance record with no
	// timestamp. None of these count as "nothing to complain about";
	// missing evidence is not passing evidence.
	ErrEvidenceIncomplete = errors.New("release: admission evidence is incomplete")
)

// AdmissionStatus is the outcome recorded on an AdmissionDecision.
type AdmissionStatus string

const (
	AdmissionAdmitted AdmissionStatus = "ADMIT"
	AdmissionRejected AdmissionStatus = "REJECT"
)

// ApprovedTarget is the single admissible deployment target for a scope: the
// exact manifest digest a human or automated approval authorized. Both
// fields are required; ApprovedTarget{} is the zero value and Admit treats
// it as an absent approval, never as "anything is approved".
type ApprovedTarget struct {
	ManifestDigest string
	Scope          string
}

func (t ApprovedTarget) incomplete() bool {
	return t.ManifestDigest == "" || t.Scope == ""
}

// VulnerabilityEvidence is the admission-relevant projection of a
// dependency/release-surface vulnerability scan (see
// tools/policy/depadmission and tools/policy/releaseadmission's
// ScannerEvidence for the pipelines that produce it). Scanned reports
// whether any scan evidence reached the decision at all; UnresolvedFindings
// is the count of findings that remain blocking after that pipeline's own
// exception handling. A Scanned=false value is absence, not a clean scan,
// and fails closed accordingly.
type VulnerabilityEvidence struct {
	Scanned            bool
	UnresolvedFindings int
}

// ConformanceEvidence is the admission-relevant projection of a CONF-001
// conformance run: the time the evidence was generated. A zero GeneratedAt
// is treated as no evidence at all, not as "always fresh".
type ConformanceEvidence struct {
	GeneratedAt time.Time
}

// DeploymentCandidate is one build offered for admission to a scope.
type DeploymentCandidate struct {
	// Bundle is the path to a release bundle directory produced by Build or
	// BuildWithKeySource.
	Bundle        string
	Scope         string
	Vulnerability VulnerabilityEvidence
	Conformance   ConformanceEvidence
}

// AdmissionPolicy is the fully specified configuration an admission
// decision is evaluated against. Every field is required; Admit rejects
// with ErrEvidenceIncomplete before performing any positive check if any of
// them is absent or its Go zero value.
type AdmissionPolicy struct {
	TrustedPublicKeys map[string]bool
	Target            ApprovedTarget
	MaxConformanceAge time.Duration
}

// AdmissionDecision is the immutable record of one admission evaluation. It
// contains no private key or artifact bytes, only identifiers and the
// evaluation outcome, so it is safe to log and to pin as golden evidence.
type AdmissionDecision struct {
	Status         AdmissionStatus `json:"status"`
	Admitted       bool            `json:"admitted"`
	Scope          string          `json:"scope"`
	ManifestDigest string          `json:"manifest_digest,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	EvaluatedAt    string          `json:"evaluated_at"`
	Digest         string          `json:"digest"`
}

// Explain renders an audit-safe, deterministic description of the decision.
func (d AdmissionDecision) Explain() string {
	if d.Reason == "" {
		return fmt.Sprintf("release admission %s (scope %s, manifest %s, decision %s)", d.Status, d.Scope, d.ManifestDigest, d.Digest)
	}
	return fmt.Sprintf("release admission %s (scope %s, manifest %s, decision %s; reason: %s)", d.Status, d.Scope, d.ManifestDigest, d.Digest, d.Reason)
}

// Admit evaluates candidate against policy as of now and returns the
// recorded decision. now is a parameter, never time.Now(), so staleness is
// deterministic and testable against a pinned clock.
//
// Admit returns a non-nil error, wrapping exactly one of the package's
// sentinel reject reasons, whenever the candidate is not admitted. The
// returned AdmissionDecision is populated on both the admit and the reject
// path so a caller always has a complete, digestible record to log.
func Admit(candidate DeploymentCandidate, policy AdmissionPolicy, now time.Time) (AdmissionDecision, error) {
	decision := AdmissionDecision{
		Scope:       candidate.Scope,
		EvaluatedAt: now.UTC().Format(time.RFC3339),
	}

	// Fail closed on absence first: every required input is checked before
	// any positive verification runs, so a missing or zero-valued input can
	// never be shadowed by a check that happens to pass anyway.
	switch {
	case len(policy.TrustedPublicKeys) == 0:
		return reject(decision, fmt.Errorf("%w: policy has no trusted public keys", ErrEvidenceIncomplete))
	case policy.Target.incomplete():
		return reject(decision, fmt.Errorf("%w: no approved deployment target is configured", ErrEvidenceIncomplete))
	case policy.MaxConformanceAge <= 0:
		return reject(decision, fmt.Errorf("%w: no conformance staleness window is configured", ErrEvidenceIncomplete))
	case candidate.Scope == "":
		return reject(decision, fmt.Errorf("%w: candidate declares no deployment scope", ErrEvidenceIncomplete))
	case !candidate.Vulnerability.Scanned:
		return reject(decision, fmt.Errorf("%w: no vulnerability scan evidence for candidate", ErrEvidenceIncomplete))
	case candidate.Conformance.GeneratedAt.IsZero():
		return reject(decision, fmt.Errorf("%w: conformance evidence has no timestamp", ErrEvidenceIncomplete))
	}

	verification, err := VerifyBundle(candidate.Bundle, VerifyOptions{TrustedPublicKeys: policy.TrustedPublicKeys})
	if err != nil {
		return reject(decision, fmt.Errorf("%w: %v", ErrCandidateUnsigned, err))
	}
	decision.ManifestDigest = verification.ManifestDigest

	// GREEN: only the exact approved digest/scope admits. A candidate that
	// merely verifies its own signature — even a different, validly signed
	// build, or this exact build offered for an unapproved scope — is not
	// thereby admissible.
	if verification.ManifestDigest != policy.Target.ManifestDigest || candidate.Scope != policy.Target.Scope {
		return reject(decision, fmt.Errorf("%w: candidate %s for scope %q is not the approved target %s for scope %q",
			ErrCandidateIncompatible, verification.ManifestDigest, candidate.Scope, policy.Target.ManifestDigest, policy.Target.Scope))
	}

	if candidate.Vulnerability.UnresolvedFindings > 0 {
		return reject(decision, fmt.Errorf("%w: %d unresolved finding(s)", ErrCandidateVulnerable, candidate.Vulnerability.UnresolvedFindings))
	}

	age := now.Sub(candidate.Conformance.GeneratedAt)
	if age < 0 || age > policy.MaxConformanceAge {
		return reject(decision, fmt.Errorf("%w: conformance evidence age %s exceeds the allowed window %s", ErrConformanceStale, age, policy.MaxConformanceAge))
	}

	decision.Status = AdmissionAdmitted
	decision.Admitted = true
	decision.Digest = decisionDigest(decision)
	return decision, nil
}

func reject(decision AdmissionDecision, cause error) (AdmissionDecision, error) {
	decision.Status = AdmissionRejected
	decision.Admitted = false
	decision.Reason = cause.Error()
	decision.Digest = decisionDigest(decision)
	return decision, cause
}

func decisionDigest(d AdmissionDecision) string {
	payload := struct {
		Status         AdmissionStatus `json:"status"`
		Admitted       bool            `json:"admitted"`
		Scope          string          `json:"scope"`
		ManifestDigest string          `json:"manifest_digest,omitempty"`
		Reason         string          `json:"reason,omitempty"`
		EvaluatedAt    string          `json:"evaluated_at"`
	}{d.Status, d.Admitted, d.Scope, d.ManifestDigest, d.Reason, d.EvaluatedAt}
	b, err := json.Marshal(payload)
	if err != nil {
		// payload is entirely plain strings/bools; Marshal cannot fail.
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
