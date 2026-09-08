package cycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PublicationEvidence is the identity and timing projection of control-plane
// evidence. It is untrusted until PublicationEvidenceVerifier authenticates
// the source receipts from which it was projected.
type PublicationEvidence struct {
	RevisionDigest   string
	BundleDigest     string
	TenantID         string
	ActivationEpoch  uint64
	ApprovedAt       time.Time
	ImpactAnalyzedAt time.Time
	CanaryWindowEnd  time.Time
	CanaryDecidedAt  time.Time
}

// PublicationVerification is the complete claim authenticated by the
// verifier, including the authoritative current lifecycle projection.
type PublicationVerification struct {
	CurrentRevisionDigest string
	CurrentState          LifecycleState
	CandidateDigest       string
	Evidence              PublicationEvidence
}

// PublicationEvidenceVerifier is a trusted composition-root port. Its
// implementation must authenticate the approved bundle, impact receipt, and
// promoted CP-007 canary decision rather than trusting request flags.
type PublicationEvidenceVerifier interface {
	VerifyFuturePublication(PublicationVerification) error
}

// FuturePublicationRequest is a pure value request. Now is injected so this
// engine remains deterministic and does not depend on a wall clock.
type FuturePublicationRequest struct {
	Revision Revision
	Current  GovernedCycle
	Evidence PublicationEvidence
	Verifier PublicationEvidenceVerifier
	Now      time.Time
}

// FuturePublication is immutable evidence that one exact revision passed the
// future-cycle publication gate.
type FuturePublication struct {
	RevisionDigest  string
	RevisionID      string
	RevisionVersion string
	EffectiveFrom   time.Time
	PublishedAt     time.Time
	BundleDigest    string
	ActivationEpoch uint64
	ReceiptDigest   string
}

var (
	ErrPublicationInvalid   = errors.New("cycle: future publication request is invalid")
	ErrPublicationNotFuture = errors.New("cycle: cycle revision is not future-effective")
	ErrPublicationLifecycle = errors.New("cycle: opened or locked cycle cannot be altered")
	ErrPublicationApproval  = errors.New("cycle: approved bundle evidence is required")
	ErrPublicationImpact    = errors.New("cycle: impact analysis is required")
	ErrPublicationCanary    = errors.New("cycle: canary evidence is required")
	ErrPublicationEvidence  = errors.New("cycle: publication evidence is not authentic")
)

// PublishFuture validates all evidence before returning a receipt. It never
// mutates Current or Revision, and rejects publication that could change an
// already-open, closed, or locked cycle.
func PublishFuture(request FuturePublicationRequest) (FuturePublication, error) {
	if request.Now.IsZero() || request.Revision.Digest == "" || request.Revision.computeDigest() != request.Revision.Digest || request.Verifier == nil {
		return FuturePublication{}, ErrPublicationInvalid
	}
	if request.Current.Revision.Digest == "" || request.Current.Revision.computeDigest() != request.Current.Revision.Digest || request.Current.Revision.Digest == request.Revision.Digest {
		return FuturePublication{}, ErrPublicationInvalid
	}
	if request.Current.State != StateUnopened {
		return FuturePublication{}, ErrPublicationLifecycle
	}
	if !request.Revision.ValidAt(request.Revision.EffectiveFrom) || !request.Revision.EffectiveFrom.After(request.Now.UTC()) {
		return FuturePublication{}, ErrPublicationNotFuture
	}
	evidence := request.Evidence
	if !sameDigest(evidence.RevisionDigest, request.Revision.Digest) || !validDigest(evidence.BundleDigest) ||
		strings.TrimSpace(evidence.TenantID) != request.Revision.Definition.Scope.TenantID || evidence.ActivationEpoch == 0 {
		return FuturePublication{}, ErrPublicationApproval
	}
	now := request.Now.UTC()
	if evidence.ApprovedAt.IsZero() || evidence.ApprovedAt.After(now) || evidence.ImpactAnalyzedAt.IsZero() ||
		evidence.ImpactAnalyzedAt.Before(evidence.ApprovedAt) || evidence.ImpactAnalyzedAt.After(now) {
		return FuturePublication{}, ErrPublicationImpact
	}
	if evidence.CanaryWindowEnd.IsZero() || evidence.CanaryDecidedAt.IsZero() || evidence.CanaryWindowEnd.After(evidence.CanaryDecidedAt) ||
		evidence.CanaryDecidedAt.Before(evidence.ImpactAnalyzedAt) || evidence.CanaryDecidedAt.After(now) {
		return FuturePublication{}, ErrPublicationCanary
	}
	verification := PublicationVerification{CurrentRevisionDigest: request.Current.Revision.Digest, CurrentState: request.Current.State, CandidateDigest: request.Revision.Digest, Evidence: evidence}
	if err := request.Verifier.VerifyFuturePublication(verification); err != nil {
		return FuturePublication{}, fmt.Errorf("%w: %v", ErrPublicationEvidence, err)
	}
	out := FuturePublication{RevisionDigest: request.Revision.Digest, RevisionID: request.Revision.ID, RevisionVersion: request.Revision.Version, EffectiveFrom: request.Revision.EffectiveFrom, PublishedAt: now, BundleDigest: evidence.BundleDigest, ActivationEpoch: evidence.ActivationEpoch}
	b, _ := json.Marshal(out)
	d := sha256.Sum256(b)
	out.ReceiptDigest = "sha256:" + hex.EncodeToString(d[:])
	return out, nil
}

func sameDigest(a, b string) bool {
	return strings.TrimSpace(a) != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func validDigest(value string) bool {
	algorithm, encoded, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || algorithm != "sha256" || len(encoded) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(encoded)
	return err == nil
}
