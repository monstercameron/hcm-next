package configbundle

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/cycle"
)

// ImpactAnalysis is the signed control-plane record for one exact candidate
// revision. It intentionally contains identities and timing only; the
// analysis payload remains outside this pure adapter.
type ImpactAnalysis struct {
	Scope          Scope
	RevisionDigest string
	BundleDigest   string
	Epoch          uint64
	AnalyzedAt     time.Time
	Digest         string
	Signature      BundleSignature
}

// ImpactReceipt is the descriptive spelling used by publication callers.
type ImpactReceipt = ImpactAnalysis

func (i ImpactAnalysis) DigestValue() (string, error) {
	if strings.TrimSpace(i.Scope.TenantID) == "" || i.Epoch == 0 || i.AnalyzedAt.IsZero() {
		return "", ErrImpactInvalid
	}
	if _, err := digestBytes(i.RevisionDigest); err != nil {
		return "", err
	}
	if _, err := digestBytes(i.BundleDigest); err != nil {
		return "", err
	}
	return canonicalbytes.New("hcmnext.platform.configbundle.ImpactAnalysis", 1).
		String("tenant_id", i.Scope.TenantID).
		String("cell_id", i.Scope.CellID).
		String("revision_digest", i.RevisionDigest).
		String("bundle_digest", i.BundleDigest).
		Int("epoch", int64(i.Epoch)).
		String("analyzed_at", i.AnalyzedAt.UTC().Format(time.RFC3339Nano)).Digest()
}

// Verify authenticates the immutable impact record with the configured key.
func (i ImpactAnalysis) Verify(publicKey ed25519.PublicKey) error {
	if err := validateSignatureIdentity(i.Signature); err != nil {
		return err
	}
	digest, err := i.DigestValue()
	if err != nil || digest != i.Digest {
		return ErrImpactInvalid
	}
	raw, err := digestBytes(i.Digest)
	if err != nil {
		return err
	}
	sig, err := decodeSignature(i.Signature.Value)
	if err != nil || !ed25519.Verify(publicKey, raw, sig) {
		return ErrImpactInvalid
	}
	return nil
}

// SignImpact creates signed evidence using the same detached-signature
// convention as activation and CP-007 decisions.
func SignImpact(impact ImpactAnalysis, signer ReceiptSigner) (ImpactAnalysis, error) {
	if signer == nil {
		return ImpactAnalysis{}, ErrImpactSigner
	}
	digest, err := impact.DigestValue()
	if err != nil {
		return ImpactAnalysis{}, err
	}
	impact.Digest = digest
	impact.Signature, err = signer.SignDigest(digest)
	if err != nil {
		return ImpactAnalysis{}, err
	}
	return impact, nil
}

// FuturePublicationEvidence is the authoritative evidence set for one future
// cycle publication. ApprovedRevisionDigest binds the control-plane bundle to
// the exact cycle revision; it is never inferred from request flags.
type FuturePublicationEvidence struct {
	ApprovedBundle         SignedBundle
	Activation             ActivationReceipt
	Impact                 ImpactAnalysis
	Canary                 CanaryDecision
	ApprovedRevisionDigest string
}

// FuturePublicationVerifier adapts configbundle's signed records to the pure
// cycle publication port. Keys must come from the configured trust directory;
// no public key embedded in evidence is accepted.
type FuturePublicationVerifier struct {
	evidence FuturePublicationEvidence
	keys     KeyResolver
}

func NewFuturePublicationVerifier(evidence FuturePublicationEvidence, keys KeyResolver) *FuturePublicationVerifier {
	return &FuturePublicationVerifier{evidence: evidence, keys: keys}
}

// NewFutureCycleVerifier is an explicit compatibility spelling.
func NewFutureCycleVerifier(evidence FuturePublicationEvidence, keys KeyResolver) *FuturePublicationVerifier {
	return NewFuturePublicationVerifier(evidence, keys)
}

func (v *FuturePublicationVerifier) VerifyFuturePublication(p cycle.PublicationVerification) error {
	if v == nil || v.keys == nil {
		return ErrPublicationEvidenceUnavailable
	}
	e := v.evidence
	if !samePublicationDigest(e.ApprovedRevisionDigest, p.CandidateDigest) || !samePublicationDigest(e.Impact.RevisionDigest, p.CandidateDigest) || !samePublicationDigest(e.Canary.BundleDigest, e.ApprovedBundle.Bundle.Digest) {
		return ErrPublicationEvidenceMismatch
	}
	if strings.TrimSpace(e.ApprovedBundle.Bundle.TargetScope.TenantID) != p.Evidence.TenantID || e.Activation.Scope.TenantID != p.Evidence.TenantID || e.Impact.Scope.TenantID != p.Evidence.TenantID || e.Canary.Scope.TenantID != p.Evidence.TenantID {
		return ErrPublicationEvidenceMismatch
	}
	if e.Activation.BundleDigest != e.ApprovedBundle.Bundle.Digest || e.Impact.BundleDigest != e.ApprovedBundle.Bundle.Digest || e.Activation.Epoch != e.Impact.Epoch || e.Canary.Epoch != e.Activation.Epoch {
		return ErrPublicationEvidenceMismatch
	}
	if e.Canary.Outcome != CanaryPromoted || e.Canary.CohortSize <= 0 {
		return ErrPublicationCanaryNotPromoted
	}
	if !e.Activation.ActivatedAt.Equal(p.Evidence.ApprovedAt) || !e.Impact.AnalyzedAt.Equal(p.Evidence.ImpactAnalyzedAt) || !e.Canary.WindowTo.Equal(p.Evidence.CanaryWindowEnd) || !e.Canary.DecidedAt.Equal(p.Evidence.CanaryDecidedAt) {
		return ErrPublicationEvidenceMismatch
	}
	if e.Canary.WindowTo.After(e.Canary.DecidedAt) || e.Canary.DecidedAt.Before(e.Impact.AnalyzedAt) {
		return ErrPublicationEvidenceMismatch
	}
	if err := v.verifySigned(e.ApprovedBundle.Signature, e.ApprovedBundle.Bundle.Digest, func(key ed25519.PublicKey) error { return VerifyBundleSignature(e.ApprovedBundle, key) }); err != nil {
		return err
	}
	if err := v.verifySigned(e.Activation.Signature, e.Activation.Digest, func(key ed25519.PublicKey) error { return e.Activation.Verify(key) }); err != nil {
		return err
	}
	if err := v.verifySigned(e.Impact.Signature, e.Impact.Digest, func(key ed25519.PublicKey) error { return e.Impact.Verify(key) }); err != nil {
		return err
	}
	if err := v.verifySigned(e.Canary.Signature, e.Canary.Digest, func(key ed25519.PublicKey) error { return e.Canary.Verify(key) }); err != nil {
		return err
	}
	return nil
}

func samePublicationDigest(a, b string) bool {
	return strings.TrimSpace(a) != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func (v *FuturePublicationVerifier) verifySigned(sig BundleSignature, _ string, verify func(ed25519.PublicKey) error) error {
	handle := sig.KeyHandle
	if handle == "" {
		handle = sig.KeyID
	}
	key, found, err := v.keys.ResolveBundleKey(handle, sig.KeyVersion)
	if err != nil || !found || key.Handle != handle || key.Version != sig.KeyVersion || key.Revoked {
		return ErrPublicationEvidenceUnauthenticated
	}
	if err := verify(key.PublicKey); err != nil {
		return ErrPublicationEvidenceUnauthenticated
	}
	return nil
}

var (
	ErrImpactInvalid                      = errors.New("configbundle: impact analysis is invalid")
	ErrImpactSigner                       = errors.New("configbundle: impact analysis signer is required")
	ErrPublicationEvidenceUnavailable     = errors.New("configbundle: publication evidence verifier is unavailable")
	ErrPublicationEvidenceMismatch        = errors.New("configbundle: publication evidence does not bind exact identities")
	ErrPublicationEvidenceUnauthenticated = errors.New("configbundle: publication evidence is not authenticated")
	ErrPublicationCanaryNotPromoted       = errors.New("configbundle: canary evidence is not promoted")
)
