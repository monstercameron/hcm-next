package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
)

// CICD-006 signed release decision manifest.
//
// A release decision is the single document that answers "may this
// candidate proceed" by gathering eleven independently required evidence
// classes - source, toolchain, artifact, schema, config, test, conformance,
// rollout, rollback, owner and blocker - rather than trusting any one of
// them alone. It deliberately composes the packages that already prove the
// hard parts instead of re-deriving them: artifact evidence is a
// Verification from VerifyBundle (CICD-003), rollback evidence is an
// AdmissionDecision from Admit (CICD-004), and rollout evidence is a
// Snapshot from a Rollout (CICD-005). This file adds no second digest or
// health-evaluation mechanism; it only decides what the already-computed
// evidence means together.
//
// The four verdicts are deliberately not synonyms for "not proceed":
//
//   - PROCEED: every evidence class is present, and none of them reports a
//     QUARANTINE- or REMEDIATE-level failure.
//   - REMEDIATE: every evidence class is present and the candidate itself
//     is trustworthy, but a specific, ordinarily-fixable quality signal is
//     failing: unresolved tests, invalid config, stale conformance
//     evidence, or a rollout stage that is merely paused. The fix is to
//     correct that named signal and re-evaluate; the candidate is not
//     discarded.
//   - QUARANTINE: every evidence class is present, but the evidence shows
//     the candidate or its deployment is not trustworthy for further
//     rollout: an unsupported manifest schema version, no currently
//     admitted rollback target (there is no safe way back), or a rollout
//     that has already been fenced or rolled back. Quarantine means
//     isolate this candidate, not merely "try again".
//   - STOP: evaluation cannot or must not proceed at all: any one of the
//     eleven evidence classes is absent (its Go zero value), or an
//     explicit, open release blocker is recorded. STOP is the only verdict
//     a missing or zero-valued input can ever produce - it can never be
//     shadowed into PROCEED by the other ten classes happening to pass.
const DecisionSchemaVersion = 1

// DecisionVerdict is the outcome of evaluating a DecisionEvidence set.
type DecisionVerdict string

const (
	DecisionProceed    DecisionVerdict = "PROCEED"
	DecisionRemediate  DecisionVerdict = "REMEDIATE"
	DecisionQuarantine DecisionVerdict = "QUARANTINE"
	DecisionStop       DecisionVerdict = "STOP"
)

// Sentinel decision reasons. Each is distinct so a caller can branch with
// errors.Is, exactly as Admit's and Rollout's sentinels allow.
var (
	// ErrDecisionEvidenceIncomplete means at least one of the eleven
	// required evidence classes is absent or its Go zero value. It is
	// checked before any positive verification, so it can never be
	// shadowed by the other ten classes happening to pass.
	ErrDecisionEvidenceIncomplete = errors.New("release: decision evidence is incomplete")
	// ErrDecisionBlocked means an explicit, open release blocker is
	// recorded. A declared blocker is a veto, not something the ordinary
	// remediation loop fixes.
	ErrDecisionBlocked = errors.New("release: decision stopped: an open release blocker is present")
	// ErrDecisionQuarantined means every evidence class is present, but at
	// least one of them shows the candidate itself, or its deployment, is
	// not trustworthy for further rollout.
	ErrDecisionQuarantined = errors.New("release: decision quarantined: candidate is not trustworthy for further rollout")
	// ErrDecisionNeedsRemediation means every evidence class is present
	// and the candidate is trustworthy, but a specific, fixable quality
	// signal is failing.
	ErrDecisionNeedsRemediation = errors.New("release: decision requires remediation before it can proceed")
)

// SourceEvidence is the source-identity evidence class: the exact commit a
// candidate was built from. A zero-valued Ref (any of its three fields
// empty) is absence, never "some source".
type SourceEvidence struct {
	Ref provenance.SourceRef
}

func (e SourceEvidence) present() bool {
	return e.Ref.Repository != "" && e.Ref.Ref != "" && e.Ref.Commit != ""
}

// ToolchainEvidence is the toolchain evidence class: the exact build
// configuration a candidate was produced with.
type ToolchainEvidence struct {
	Config provenance.BuildConfig
}

func (e ToolchainEvidence) present() bool {
	return e.Config.GoVersion != "" && e.Config.GOOS != "" && e.Config.GOARCH != "" && e.Config.ConfigDigest != ""
}

// ArtifactEvidence is the artifact evidence class: the offline verification
// receipt VerifyBundle already produced for the candidate bundle. This is
// composed, not re-derived - the digest and signature checks live in
// VerifyBundle alone.
type ArtifactEvidence struct {
	Verification Verification
}

func (e ArtifactEvidence) present() bool {
	return e.Verification.ManifestDigest != "" && len(e.Verification.Artifacts) > 0
}

// SchemaEvidence is the schema evidence class: the release bundle manifest
// schema version this decision was validated against.
type SchemaEvidence struct {
	BundleSchemaVersion int
	ValidatedAt         time.Time
}

func (e SchemaEvidence) present() bool {
	return e.BundleSchemaVersion != 0 && !e.ValidatedAt.IsZero()
}

func (e SchemaEvidence) valid() bool { return e.BundleSchemaVersion == SchemaVersion }

// ConfigEvidence is the config evidence class: whether the candidate's
// configuration was validated, and what that validation found.
type ConfigEvidence struct {
	Digest      string
	Valid       bool
	ValidatedAt time.Time
}

func (e ConfigEvidence) present() bool {
	return e.Digest != "" && !e.ValidatedAt.IsZero()
}

// TestEvidence is the test evidence class: the result of the test suite run
// against the candidate. A zero RanAt, or a run that recorded neither a
// pass nor a failure, is absence, never a passing run.
type TestEvidence struct {
	RanAt  time.Time
	Passed int
	Failed int
}

func (e TestEvidence) present() bool {
	return !e.RanAt.IsZero() && (e.Passed+e.Failed) > 0
}

// RolloutEvidence is the rollout evidence class: a Snapshot taken directly
// from a CICD-005 Rollout. It is composed, not re-derived - the health
// evaluation that produced this status already ran in evaluateStageHealth.
type RolloutEvidence struct {
	Snapshot Snapshot
}

func (e RolloutEvidence) present() bool {
	return e.Snapshot.ID != "" && len(e.Snapshot.History) > 0
}

// RollbackEvidence is the rollback evidence class: an AdmissionDecision,
// from CICD-004's Admit, for the designated rollback target. It is
// composed, not re-derived - whether a safe way back exists is exactly the
// question Admit already answers. A nil Decision, or one with no computed
// Digest, is absence: Admit was never actually run for a rollback target.
type RollbackEvidence struct {
	Decision *AdmissionDecision
}

func (e RollbackEvidence) present() bool {
	return e.Decision != nil && e.Decision.Digest != ""
}

// OwnerEvidence is the owner evidence class: the accountable owner who
// confirmed this release.
type OwnerEvidence struct {
	Name        string
	ConfirmedAt time.Time
}

func (e OwnerEvidence) present() bool {
	return e.Name != "" && !e.ConfirmedAt.IsZero()
}

// BlockerEvidence is the blocker evidence class: proof that a blocker scan
// actually ran, and what it found. A zero CheckedAt means the scan was
// never performed, which is never treated as "no blockers".
type BlockerEvidence struct {
	CheckedAt time.Time
	Open      []string
}

func (e BlockerEvidence) present() bool { return !e.CheckedAt.IsZero() }

// DecisionEvidence bundles the eleven independently required evidence
// classes a release decision evaluates. Every field's Go zero value is
// absence, never a passing observation.
type DecisionEvidence struct {
	Source      SourceEvidence
	Toolchain   ToolchainEvidence
	Artifact    ArtifactEvidence
	Schema      SchemaEvidence
	Config      ConfigEvidence
	Test        TestEvidence
	Conformance ConformanceEvidence
	Rollout     RolloutEvidence
	Rollback    RollbackEvidence
	Owner       OwnerEvidence
	Blocker     BlockerEvidence
}

// DecisionPolicy configures the decision's own conformance freshness check.
// MaxConformanceAge is required; EvaluateDecision rejects with
// ErrDecisionEvidenceIncomplete before evaluating any evidence class if it
// is absent or its Go zero value, exactly as AdmissionPolicy and
// RolloutPolicy require for their own conformance windows.
type DecisionPolicy struct {
	MaxConformanceAge time.Duration
}

// DecisionClassStatus records, per evidence class and in a fixed field
// order, whether that class was present and passing. It is never a map, so
// its JSON encoding is byte-identical for identical input regardless of any
// map iteration order elsewhere in the process.
type DecisionClassStatus struct {
	Source      bool `json:"source"`
	Toolchain   bool `json:"toolchain"`
	Artifact    bool `json:"artifact"`
	Schema      bool `json:"schema"`
	Config      bool `json:"config"`
	Test        bool `json:"test"`
	Conformance bool `json:"conformance"`
	Rollout     bool `json:"rollout"`
	Rollback    bool `json:"rollback"`
	Owner       bool `json:"owner"`
	Blocker     bool `json:"blocker"`
}

// DecisionManifest is the canonical, signed release-decision document.
// Signature is excluded from CanonicalDigest, exactly as release.Manifest
// excludes its own, so signing is never a circular input.
type DecisionManifest struct {
	SchemaVersion int                     `json:"schema_version"`
	ID            string                  `json:"id"`
	Verdict       DecisionVerdict         `json:"verdict"`
	Classes       DecisionClassStatus     `json:"classes"`
	Reasons       []string                `json:"reasons,omitempty"`
	EvaluatedAt   string                  `json:"evaluated_at"`
	Signature     *gateevidence.Signature `json:"signature,omitempty"`
}

// Explain renders an audit-safe, deterministic description of the decision.
func (m DecisionManifest) Explain() string {
	if len(m.Reasons) == 0 {
		return fmt.Sprintf("release decision %s (id %s)", m.Verdict, m.ID)
	}
	return fmt.Sprintf("release decision %s (id %s; reasons: %s)", m.Verdict, m.ID, strings.Join(m.Reasons, "; "))
}

// CanonicalDigest hashes exactly the decision fields that matter to a
// verifier, excluding Signature, using fixed struct field order rather than
// a map, so the digest is identical for identical input independent of any
// map iteration order.
func (m DecisionManifest) CanonicalDigest() (string, error) {
	payload := struct {
		SchemaVersion int                 `json:"schema_version"`
		ID            string              `json:"id"`
		Verdict       DecisionVerdict     `json:"verdict"`
		Classes       DecisionClassStatus `json:"classes"`
		Reasons       []string            `json:"reasons,omitempty"`
		EvaluatedAt   string              `json:"evaluated_at"`
	}{m.SchemaVersion, m.ID, m.Verdict, m.Classes, m.Reasons, m.EvaluatedAt}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("release: marshal canonical decision manifest: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// EvaluateDecision computes the verdict for evidence as of now, without
// signing. now is a parameter, never time.Now(), so every evaluation is
// deterministic and testable against a pinned clock, exactly as Admit and
// Advance require. The returned DecisionManifest is populated on every
// path, admitted or not, so a caller always has a complete record; the
// returned error is nil only for PROCEED and otherwise wraps exactly one of
// this file's sentinel reasons.
func EvaluateDecision(id string, evidence DecisionEvidence, policy DecisionPolicy, now time.Time) (DecisionManifest, error) {
	manifest := DecisionManifest{
		SchemaVersion: DecisionSchemaVersion,
		ID:            id,
		EvaluatedAt:   now.UTC().Format(time.RFC3339),
	}

	if policy.MaxConformanceAge <= 0 {
		return stopDecision(manifest, ErrDecisionEvidenceIncomplete, []string{"no conformance staleness window is configured"})
	}

	// Fail closed on absence first: every one of the eleven evidence
	// classes is checked for presence before any of them is trusted for
	// its pass/fail value, so a missing class can never be shadowed by the
	// other ten happening to pass. The order below is fixed, not derived
	// from a map, so the resulting reason list is deterministic.
	presence := []struct {
		name    string
		present bool
	}{
		{"source", evidence.Source.present()},
		{"toolchain", evidence.Toolchain.present()},
		{"artifact", evidence.Artifact.present()},
		{"schema", evidence.Schema.present()},
		{"config", evidence.Config.present()},
		{"test", evidence.Test.present()},
		{"conformance", !evidence.Conformance.GeneratedAt.IsZero()},
		{"rollout", evidence.Rollout.present()},
		{"rollback", evidence.Rollback.present()},
		{"owner", evidence.Owner.present()},
		{"blocker", evidence.Blocker.present()},
	}
	var missing []string
	for _, class := range presence {
		if !class.present {
			missing = append(missing, fmt.Sprintf("missing %s evidence", class.name))
		}
	}
	if len(missing) > 0 {
		return stopDecision(manifest, ErrDecisionEvidenceIncomplete, missing)
	}

	// Every class is present. Source, toolchain, artifact and owner are
	// presence-only classes: this package has nothing further to verify
	// about them without re-deriving checks that other packages already
	// own (provenance signing, VerifyBundle).
	manifest.Classes.Source = true
	manifest.Classes.Toolchain = true
	manifest.Classes.Artifact = true
	manifest.Classes.Owner = true

	// An explicit, open blocker is an outright veto, not a remediation
	// item: STOP, not REMEDIATE.
	if len(evidence.Blocker.Open) > 0 {
		reason := fmt.Sprintf("open blocker(s): %s", strings.Join(evidence.Blocker.Open, ", "))
		return stopDecisionWithCause(manifest, fmt.Errorf("%w: %s", ErrDecisionBlocked, reason), []string{reason})
	}
	manifest.Classes.Blocker = true

	var quarantineReasons []string
	var remediateReasons []string
	conformanceStale := false

	if evidence.Schema.valid() {
		manifest.Classes.Schema = true
	} else {
		quarantineReasons = append(quarantineReasons, fmt.Sprintf("bundle schema version %d is not the supported version %d", evidence.Schema.BundleSchemaVersion, SchemaVersion))
	}

	if evidence.Rollback.Decision.Admitted {
		manifest.Classes.Rollback = true
	} else {
		quarantineReasons = append(quarantineReasons, fmt.Sprintf("designated rollback target is not admitted: %s", evidence.Rollback.Decision.Reason))
	}

	switch evidence.Rollout.Snapshot.Status {
	case RolloutInProgress, RolloutCompleted:
		manifest.Classes.Rollout = true
	case RolloutPaused:
		remediateReasons = append(remediateReasons, "rollout is paused pending a healthy Resume")
	default:
		// RolloutFenced, RolloutRolledBack, or any status this decision
		// engine does not recognize: the rollout has already needed a
		// rollback, or reports an unknown state. Neither is healthy
		// progress, and an unrecognized value must never read as passing.
		quarantineReasons = append(quarantineReasons, fmt.Sprintf("rollout status %s is not a healthy progressing state", evidence.Rollout.Snapshot.Status))
	}

	if evidence.Config.Valid {
		manifest.Classes.Config = true
	} else {
		remediateReasons = append(remediateReasons, "config evidence reports an invalid configuration")
	}

	if evidence.Test.Failed == 0 {
		manifest.Classes.Test = true
	} else {
		remediateReasons = append(remediateReasons, fmt.Sprintf("%d test(s) failed", evidence.Test.Failed))
	}

	age := now.Sub(evidence.Conformance.GeneratedAt)
	if age >= 0 && age <= policy.MaxConformanceAge {
		manifest.Classes.Conformance = true
	} else {
		conformanceStale = true
		remediateReasons = append(remediateReasons, fmt.Sprintf("conformance evidence age %s exceeds the allowed window %s", age, policy.MaxConformanceAge))
	}

	if len(quarantineReasons) > 0 {
		manifest.Verdict = DecisionQuarantine
		manifest.Reasons = quarantineReasons
		return manifest, fmt.Errorf("%w: %s", ErrDecisionQuarantined, strings.Join(quarantineReasons, "; "))
	}
	if len(remediateReasons) > 0 {
		manifest.Verdict = DecisionRemediate
		manifest.Reasons = remediateReasons
		if conformanceStale {
			return manifest, fmt.Errorf("%w: %w: %s", ErrDecisionNeedsRemediation, ErrConformanceStale, strings.Join(remediateReasons, "; "))
		}
		return manifest, fmt.Errorf("%w: %s", ErrDecisionNeedsRemediation, strings.Join(remediateReasons, "; "))
	}

	manifest.Verdict = DecisionProceed
	return manifest, nil
}

func stopDecision(manifest DecisionManifest, sentinel error, reasons []string) (DecisionManifest, error) {
	return stopDecisionWithCause(manifest, fmt.Errorf("%w: %s", sentinel, strings.Join(reasons, "; ")), reasons)
}

func stopDecisionWithCause(manifest DecisionManifest, cause error, reasons []string) (DecisionManifest, error) {
	manifest.Verdict = DecisionStop
	manifest.Reasons = reasons
	return manifest, cause
}

// SignDecisionManifest signs manifest using source, the same KeySource port
// BuildWithKeySource uses to sign release bundles, so decision manifests and
// release bundles share exactly one signing mechanism rather than a second,
// parallel one. Signature is excluded from CanonicalDigest, so signing is
// never a circular input.
func SignDecisionManifest(manifest DecisionManifest, source KeySource) (DecisionManifest, error) {
	if source == nil {
		return DecisionManifest{}, fmt.Errorf("release: decision key source is required")
	}
	publicKey := strings.ToLower(strings.TrimSpace(source.PublicKey()))
	digest, err := manifest.CanonicalDigest()
	if err != nil {
		return DecisionManifest{}, err
	}
	signature, err := source.SignDigest(digest)
	if err != nil {
		return DecisionManifest{}, fmt.Errorf("release: sign decision manifest through key source: %w", err)
	}
	manifest.Signature = &gateevidence.Signature{Algorithm: "ed25519", PublicKey: publicKey, Value: signature}
	return manifest, nil
}

// VerifyDecisionManifest checks manifest's signature against trusted (the
// pinned development fixture key when trusted is empty), reusing
// gateevidence's digest-signature primitive exactly as VerifyBundle does
// rather than a second verification path.
func VerifyDecisionManifest(manifest DecisionManifest, trusted map[string]bool) error {
	if len(trusted) == 0 {
		trusted = map[string]bool{DevFixturePublicKey: true}
	}
	signerKey := "<none>"
	if manifest.Signature != nil {
		signerKey = manifest.Signature.PublicKey
	}
	if manifest.Signature == nil || !trusted[manifest.Signature.PublicKey] {
		return fmt.Errorf("release: decision manifest signer %q is not trusted", signerKey)
	}
	if manifest.Signature.Algorithm != "ed25519" {
		return fmt.Errorf("release: unsupported decision manifest signature algorithm %q", manifest.Signature.Algorithm)
	}
	digest, err := manifest.CanonicalDigest()
	if err != nil {
		return err
	}
	ok, err := gateevidence.VerifyDigestSignature(manifest.Signature.PublicKey, digest, manifest.Signature.Value)
	if err != nil {
		return fmt.Errorf("release: decision manifest signature: %w", err)
	}
	if !ok {
		return fmt.Errorf("release: decision manifest signature does not verify")
	}
	return nil
}

// BuildDecisionManifest evaluates evidence as of now and signs the result
// with source regardless of verdict, so even a STOP or QUARANTINE decision
// is a tamper-evident audit record rather than an unsigned, disputable one.
// It mirrors Admit and Advance: the returned manifest is populated on both
// the proceed and non-proceed paths, and the returned error is nil only for
// PROCEED and otherwise wraps exactly one of this file's sentinels.
func BuildDecisionManifest(id string, evidence DecisionEvidence, policy DecisionPolicy, now time.Time, source KeySource) (DecisionManifest, error) {
	manifest, evalErr := EvaluateDecision(id, evidence, policy, now)
	signed, signErr := SignDecisionManifest(manifest, source)
	if signErr != nil {
		return DecisionManifest{}, signErr
	}
	return signed, evalErr
}
