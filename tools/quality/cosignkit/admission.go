// Package cosignkit owns the provider-neutral admission contract for release
// signatures and attestations. Cosign/Sigstore is an implementation detail:
// callers supply a Verifier and this package decides whether evidence satisfies
// the Human Capital Management Suite release policy.
package cosignkit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrRejected      = errors.New("release admission rejected")
	ErrInvalidPolicy = errors.New("invalid admission policy")
	ErrVerification  = errors.New("external verification failed")
)

// Artifact identifies the immutable subject being admitted.
type Artifact struct {
	// Digest must be a complete sha256:<64 lowercase hexadecimal> reference.
	Digest string
}

// Policy is the pinned release-admission contract. Empty required fields are
// intentional policy errors, rather than wildcards, so a missing policy can
// never become an allow decision.
type Policy struct {
	Version             string
	SubjectDigest       string
	BuilderIdentity     string
	Issuer              string
	SourceRepository    string
	WorkflowRef         string
	PredicateType       string
	SBOMDigest          string
	MaxAttestationAge   time.Duration
	RequireTransparency bool
	AllowOfflineBundle  bool
}

// Evidence is the normalized output of an external Cosign/Sigstore verifier.
// The verifier must set Verified true only after cryptographic verification.
type Evidence struct {
	PolicyVersion     string
	Verified          bool
	SubjectDigest     string
	BuilderIdentity   string
	Issuer            string
	SourceRepository  string
	WorkflowRef       string
	PredicateType     string
	SBOMDigest        string
	TransparencyValid bool
	AttestedAt        time.Time
	Replay            bool
}

// Verifier is the only external-tool boundary. Implementations may invoke
// cosign, Rekor, or an offline bundle verifier; they must not make admission
// decisions or call HCM business services.
type Verifier interface {
	Verify(context.Context, Artifact, Policy) (Evidence, error)
}

// Decision is a stable, inspectable admission result.
type Decision struct {
	Allowed bool
	Code    string
	Field   string
	Reason  string
}

// Checker evaluates normalized verifier evidence against a pinned policy.
type Checker struct {
	Verifier Verifier
	Now      func() time.Time
}

func (c Checker) Check(ctx context.Context, artifact Artifact, policy Policy) (Decision, error) {
	if err := validatePolicy(policy); err != nil {
		return reject("INVALID_POLICY", "policy", err), fmt.Errorf("%w: %v", ErrRejected, err)
	}
	if !validDigest(artifact.Digest) {
		return reject("INVALID_ARTIFACT_DIGEST", "digest", errors.New("artifact digest is not a sha256 reference")), fmt.Errorf("%w: artifact digest", ErrRejected)
	}
	if artifact.Digest != policy.SubjectDigest {
		return reject("WRONG_SUBJECT_DIGEST", "digest", errors.New("artifact does not match pinned policy subject")), fmt.Errorf("%w: subject digest", ErrRejected)
	}
	if c.Verifier == nil {
		return reject("VERIFIER_UNAVAILABLE", "verifier", ErrVerification), fmt.Errorf("%w: verifier is nil", ErrRejected)
	}
	evidence, err := c.Verifier.Verify(ctx, artifact, policy)
	if err != nil {
		return reject("VERIFICATION_FAILED", "verifier", err), fmt.Errorf("%w: %v", ErrRejected, err)
	}
	checks := []struct{ code, field, got, want string }{
		{"UNSIGNED", "verified", fmt.Sprint(evidence.Verified), "true"},
		{"STALE_POLICY", "policyVersion", evidence.PolicyVersion, policy.Version},
		{"WRONG_SUBJECT_DIGEST", "subjectDigest", evidence.SubjectDigest, policy.SubjectDigest},
		{"UNTRUSTED_BUILDER", "builderIdentity", evidence.BuilderIdentity, policy.BuilderIdentity},
		{"WRONG_ISSUER", "issuer", evidence.Issuer, policy.Issuer},
		{"WRONG_SOURCE", "sourceRepository", evidence.SourceRepository, policy.SourceRepository},
		{"WRONG_WORKFLOW", "workflowRef", evidence.WorkflowRef, policy.WorkflowRef},
		{"WRONG_PREDICATE", "predicateType", evidence.PredicateType, policy.PredicateType},
		{"DETACHED_SBOM", "sbomDigest", evidence.SBOMDigest, policy.SBOMDigest},
	}
	for _, check := range checks {
		if check.got != check.want {
			return reject(check.code, check.field, fmt.Errorf("got %q, want %q", check.got, check.want)), fmt.Errorf("%w: %s", ErrRejected, check.code)
		}
	}
	if policy.RequireTransparency && !evidence.TransparencyValid {
		return reject("INVALID_TRANSPARENCY", "transparency", errors.New("transparency proof is absent or invalid")), fmt.Errorf("%w: transparency", ErrRejected)
	}
	if evidence.Replay {
		return reject("REPLAYED_ATTESTATION", "attestation", errors.New("attestation was already consumed")), fmt.Errorf("%w: replay", ErrRejected)
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	checkedAt := now()
	if checkedAt.Sub(evidence.AttestedAt) > policy.MaxAttestationAge || evidence.AttestedAt.After(checkedAt.Add(time.Minute)) {
		return reject("STALE_ATTESTATION", "attestedAt", errors.New("attestation is outside the freshness window")), fmt.Errorf("%w: stale attestation", ErrRejected)
	}
	return Decision{Allowed: true, Code: "ADMITTED", Reason: "verified release evidence satisfies pinned policy"}, nil
}

func reject(code, field string, err error) Decision {
	return Decision{Code: code, Field: field, Reason: err.Error()}
}

func validatePolicy(p Policy) error {
	if strings.TrimSpace(p.Version) == "" || !validDigest(p.SubjectDigest) || p.MaxAttestationAge <= 0 ||
		p.BuilderIdentity == "" || p.Issuer == "" || p.SourceRepository == "" || p.WorkflowRef == "" ||
		p.PredicateType == "" || !validDigest(p.SBOMDigest) {
		return ErrInvalidPolicy
	}
	return nil
}

func validDigest(s string) bool {
	if len(s) != len("sha256:")+64 || !strings.HasPrefix(s, "sha256:") {
		return false
	}
	for _, r := range s[len("sha256:"):] {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
