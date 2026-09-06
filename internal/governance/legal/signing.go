package legal

// This file exports the generic sign/verify primitive [PackCandidate.Sign]
// and [LegalContext] resolution already use internally, for LEGAL-015's
// authoring and review pipeline. That pipeline signs two artifacts that are
// never a [PackRelease]: an author signs a draft candidate's digest before
// any reviewer has acted, and a reviewer signs a [ReviewRecord]'s digest.
// Both need the same "sha256 digest, detached ed25519 over it, dev fixture
// key convention" signing shape this package already applies to releases,
// without duplicating ed25519 plumbing in internal/governance/legal/pipeline.

// SignDigest signs data (an artifact's own canonical encoding) with s and
// returns the lowercase hex sha256 digest and the detached ed25519 signature
// over it. Unlike [PackCandidate.Sign], the caller supplies the canonical
// bytes directly, so this works for any digested artifact this package
// defines, not only a [RulePack].
func (s *Signer) SignDigest(data []byte) (digest string, sig Signature) {
	d, raw := s.sign(data)
	return d, Signature{PublicKey: s.PublicKey(), Bytes: raw}
}

// VerifySignature recomputes the sha256 digest of data and checks both that
// it equals digest and that sig verifies over it, exactly as
// [RulePack.Verify] does for a release. It fails closed the same two ways:
// a forged signature over a matching digest fails ed25519 verification, and
// a valid signature over content that no longer matches digest fails the
// digest comparison.
func VerifySignature(data []byte, digest string, sig Signature) error {
	return verifyDigest(data, digest, sig)
}
