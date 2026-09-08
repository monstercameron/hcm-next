package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// canonicalBytes returns the deterministic encoding the release digest is
// computed over. It is exactly the field list in the contract's section 3.2,
// in that order, using the length-prefixed framing in canonical.go.
//
// What is deliberately NOT here: the citation Note paraphrase, any display or
// localization string, authoring metadata (generator, source-file list,
// research-file line numbers), registry indexes, and the digest and
// signatures themselves. Rewrapping a research file or correcting a
// paraphrase must not invalidate a signed release; changing any typed field
// must. That asymmetry is the reason Note may never carry a value evaluation
// reads.
func (p RulePack) canonicalBytes() []byte {
	var b []byte
	b = append(b, 0x52, 0x50, 0x31) // "RP1" — PackRelease, encoding version 1.
	b = appendField(b, "pack_id", p.PackID)
	b = appendUint32Field(b, "version_major", p.Version)
	b = appendUint32Field(b, "version_minor", p.MinorVersion)
	b = appendUint32Field(b, "vocabulary_version", uint32(p.EffectiveVocabulary()))
	b = appendJurisdictionRef(b, "jurisdiction", p.Jurisdiction)
	b = appendField(b, "window_start", p.Window.Start.String())
	b = appendField(b, "window_end", p.Window.End.String())
	b = appendFieldBool(b, "window_has_end", p.Window.HasEnd)
	b = appendField(b, "source_type", p.SourceType.String())
	b = appendField(b, "review_status", p.ReviewStatus.String())

	obligations := p.obligations()
	b = appendUint32Field(b, "obligation_count", uint32(len(obligations)))
	for _, o := range obligations {
		b = appendField(b, "kind", o.Type.String())
		b = appendField(b, "obligation_id", o.Rule.obligationID())
		b = o.Rule.canonicalBody(b)
		b = appendCitationRef(b, o.Rule.obligationCitation())
	}

	b = appendUint32Field(b, "preemption_assertion_count", uint32(len(p.PreemptionAssertions)))
	for _, a := range p.PreemptionAssertions {
		b = appendField(b, "preemption_kind", a.Kind.String())
		b = appendField(b, "preemption_scope", a.Scope)
		b = appendCitationRef(b, a.Citation)
	}

	// The supersedes reference, or the empty triple. An absent predecessor
	// and a predecessor with empty fields must not encode identically, so
	// presence is its own field.
	b = appendFieldBool(b, "has_supersedes", p.Supersedes != nil)
	if p.Supersedes != nil {
		b = appendField(b, "supersedes_pack_id", p.Supersedes.PackID)
		b = appendUint32Field(b, "supersedes_version_major", p.Supersedes.Version)
		b = appendUint32Field(b, "supersedes_version_minor", p.Supersedes.MinorVersion)
		b = appendJurisdictionRef(b, "supersedes_jurisdiction", p.Supersedes.Jurisdiction)
	} else {
		b = appendField(b, "supersedes_pack_id", "")
		b = appendUint32Field(b, "supersedes_version_major", 0)
		b = appendUint32Field(b, "supersedes_version_minor", 0)
		b = appendJurisdictionRef(b, "supersedes_jurisdiction", Jurisdiction{})
	}
	return b
}

// appendJurisdictionRef encodes the contract's JurisdictionRef: country,
// subdivision, then the ordered locality path as a length field followed by
// each element. [Jurisdiction.Locality] is the path's last element, so an
// empty path and a one-element empty-string path never encode identically.
func appendJurisdictionRef(dst []byte, label string, j Jurisdiction) []byte {
	dst = appendField(dst, label+".country", j.Country)
	dst = appendField(dst, label+".subdivision", j.State)
	var path []string
	if j.Locality != "" {
		path = []string{j.Locality}
	}
	return appendStringSlice(dst, label+".locality_path", path)
}

// appendCitationRef encodes exactly the four citation fields section 3.2
// covers. Note is absent by design.
func appendCitationRef(dst []byte, c Citation) []byte {
	dst = appendField(dst, "citation.source_file", c.SourceFile)
	dst = appendField(dst, "citation.section", c.Section)
	dst = appendField(dst, "citation.status", c.Status.String())
	return appendField(dst, "citation.confidence_marker", c.ConfidenceMarker.String())
}

// ComputeDigest returns the lowercase hex sha256 over the release's canonical
// encoding. It ignores whatever is already in [RulePack.Digest], which is
// what makes it usable as a tamper check.
func (p RulePack) ComputeDigest() string {
	sum := sha256.Sum256(p.canonicalBytes())
	return hex.EncodeToString(sum[:])
}

// Sign digests the candidate and attaches one role's ed25519 signature over
// that digest, producing the immutable release. It reuses the same signing
// path [LegalContext] uses: sha256 over a length-prefixed canonical encoding,
// detached ed25519 over the digest bytes.
//
// Signing does not mutate the candidate. Publishing a second role's approval
// means calling [PackRelease.AddSignature] on the returned release, and every
// role signs the same digest.
func (c PackCandidate) Sign(role SigningRole, signer *Signer) (PackRelease, error) {
	if signer == nil {
		return PackRelease{}, fmt.Errorf("%w: no signer supplied", ErrSignerKey)
	}
	if role == "" {
		return PackRelease{}, fmt.Errorf("%w: signature role is required", ErrPackNotSigned)
	}
	release := c.pack
	digest, signature, err := signer.SignDigestChecked(release.canonicalBytes())
	if err != nil {
		return PackRelease{}, fmt.Errorf("%w: signing release: %w", ErrPackNotSigned, err)
	}
	release.Digest = digest
	release.Signatures = []RoleSignature{{
		Role:      role,
		Signature: signature,
	}}
	return release, nil
}

// AddSignature attaches a second role's approval over the same digest. It
// refuses a duplicate role and refuses to sign a release whose recorded
// digest no longer matches its content.
func AddSignature(release PackRelease, role SigningRole, signer *Signer) (PackRelease, error) {
	if signer == nil {
		return PackRelease{}, fmt.Errorf("%w: no signer supplied", ErrSignerKey)
	}
	if release.Digest == "" {
		return PackRelease{}, ErrPackNotSigned
	}
	if got := release.ComputeDigest(); got != release.Digest {
		return PackRelease{}, fmt.Errorf("%w: recomputed %s, recorded %s", ErrDigestMismatch, got, release.Digest)
	}
	for _, s := range release.Signatures {
		if s.Role == role {
			return PackRelease{}, fmt.Errorf("%w: %s", ErrPackSignatureRoleDupe, role)
		}
	}
	_, signature, err := signer.SignDigestChecked(release.canonicalBytes())
	if err != nil {
		return PackRelease{}, fmt.Errorf("%w: signing release: %w", ErrPackNotSigned, err)
	}
	out := release
	out.Signatures = append(append([]RoleSignature(nil), release.Signatures...), RoleSignature{
		Role:      role,
		Signature: signature,
	})
	return out, nil
}

// Verify recomputes the release digest and checks every attached signature
// against it. It fails closed twice over: a forged signature over a valid
// digest is rejected because ed25519 verification fails, and a valid
// signature over stale content is rejected because the recomputed digest no
// longer matches.
func (p RulePack) Verify() error {
	if p.Digest == "" || len(p.Signatures) == 0 {
		return ErrPackNotSigned
	}
	canonical := p.canonicalBytes()
	for _, s := range p.Signatures {
		if err := verifyDigest(canonical, p.Digest, s.Signature); err != nil {
			return fmt.Errorf("signature role %s: %w", s.Role, err)
		}
	}
	return nil
}

// VerifyWithKey behaves like [RulePack.Verify] and additionally requires that
// the named role's signature was made by trustedKey. Verify alone proves
// internal self-consistency; only this proves a trusted publisher signed it.
func (p RulePack) VerifyWithKey(role SigningRole, trustedKey []byte) error {
	if err := p.Verify(); err != nil {
		return err
	}
	for _, s := range p.Signatures {
		if s.Role != role {
			continue
		}
		if len(trustedKey) != len(s.Signature.PublicKey) || string(trustedKey) != string(s.Signature.PublicKey) {
			return fmt.Errorf("%w: role %s was signed by an untrusted key", ErrSignatureInvalid, role)
		}
		return nil
	}
	return fmt.Errorf("%w: no signature for role %s", ErrPackNotSigned, role)
}
