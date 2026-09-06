package provenance

import "fmt"

// VerifyOptions configures Verify's admission decision.
type VerifyOptions struct {
	// TrustedPublicKeys is the set of hex-encoded Ed25519 public keys
	// Verify accepts as a valid signer. A Statement signed by any other
	// key - including a well-formed, internally-consistent signature - is
	// rejected as "unknown builder/signer". A nil or empty set trusts no
	// one (Verify always rejects).
	TrustedPublicKeys map[string]bool
	// SBOMDigest, when non-empty, is the hex sha256 of the SBOM file
	// actually present on disk (e.g. sha256 of
	// definitions/supply-chain/sbom.cdx.json). Verify rejects a Statement
	// whose SBOM.SHA256 does not match. Leave empty to skip this
	// cross-check (e.g. when the caller has no SBOM file available and
	// only wants to check signature/trust/completeness).
	SBOMDigest string
}

// Verify is the deployment-admission gate TOOL-018 describes: it accepts a
// Statement only when every one of the following holds, and otherwise
// returns a descriptive error naming exactly which check failed:
//
//   - s.Validate() reports no structural violation (subjects, builder,
//     source, build config and SBOM reference are all present) -
//     "policy-incomplete".
//   - s.Signature is present, uses the ed25519 algorithm, and its public
//     key is a member of opts.TrustedPublicKeys - "unknown builder/signer".
//   - the signature verifies against s.CanonicalDigest() - a Statement
//     tampered with after signing (any field edited) fails here, because
//     the recomputed digest no longer matches what was signed -
//     "signature does not verify (tampered statement or wrong key)".
//   - when opts.SBOMDigest is non-empty, it matches s.SBOM.SHA256 exactly -
//     "SBOM digest mismatch".
//
// A nil error means s is admitted: only an approved source/build/toolchain
// /SBOM provenance chain reaches the caller.
func Verify(s Statement, opts VerifyOptions) error {
	if violations := s.Validate(); len(violations) != 0 {
		return fmt.Errorf("provenance: statement is policy-incomplete: %v", violations)
	}

	if s.Signature == nil {
		return fmt.Errorf("provenance: statement is unsigned")
	}
	if s.Signature.Algorithm != AlgorithmEd25519 {
		return fmt.Errorf("provenance: unsupported signature algorithm %q", s.Signature.Algorithm)
	}
	if !opts.TrustedPublicKeys[s.Signature.PublicKey] {
		return fmt.Errorf("provenance: unknown builder/signer key %s is not in the trusted set", s.Signature.PublicKey)
	}

	ok, err := VerifyStatementSignature(s)
	if err != nil {
		return fmt.Errorf("provenance: signature check failed: %w", err)
	}
	if !ok {
		return fmt.Errorf("provenance: signature does not verify (tampered statement or wrong key)")
	}

	if opts.SBOMDigest != "" && s.SBOM.SHA256 != opts.SBOMDigest {
		return fmt.Errorf("provenance: SBOM digest mismatch: statement references %s, actual SBOM file is %s", s.SBOM.SHA256, opts.SBOMDigest)
	}

	return nil
}
