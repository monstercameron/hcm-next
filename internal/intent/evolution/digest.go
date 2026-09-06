package evolution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/intent"
)

// definitionDigestMagic namespaces the digest so it is never mistaken for a
// digest of the same bytes produced by a different encoding generation.
// Changing the encoding below is a new magic value, never a silent edit.
const definitionDigestMagic = "hcmnext.intent.evolution.definition_digest.v1"

// DefinitionDigest returns the deterministic content digest of one intent
// definition: every exported field, canonically JSON-encoded — struct
// fields in declaration order and map keys sorted, both of which
// encoding/json already guarantees — and hashed with SHA-256.
//
// It exists to prove, rather than merely assert, whether two [intent.Definition]
// values that carry the same [intent.Ref] are the same content or a
// mutation of it: see [RefuseInPlaceEdit].
func DefinitionDigest(def intent.Definition) (string, error) {
	if err := def.Validate(); err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrInvalidDefinition, def.Ref, err)
	}
	encoded, err := json.Marshal(def)
	if err != nil {
		return "", fmt.Errorf("evolution: encode definition %s for digest: %w", def.Ref, err)
	}
	sum := sha256.Sum256(append([]byte(definitionDigestMagic), encoded...))
	return hex.EncodeToString(sum[:]), nil
}

// RefuseInPlaceEdit compares a published definition against a candidate.
//
// A candidate naming a different [intent.Ref] is not this function's
// concern: it may be a legitimate new version, whose content is judged by
// [CompatibilityCheck], or an unrelated definition entirely. A candidate
// naming the SAME ref as published must be byte-for-byte the same content.
// [intent.NewRegistry] already refuses re-registering a ref outright; this
// function exists for the moment before that — a proposed republication —
// and proves exactly what changed, by digest, rather than only recording
// that a duplicate ref was seen.
func RefuseInPlaceEdit(published, candidate intent.Definition) error {
	if published.Ref != candidate.Ref {
		return nil
	}
	publishedDigest, err := DefinitionDigest(published)
	if err != nil {
		return err
	}
	candidateDigest, err := DefinitionDigest(candidate)
	if err != nil {
		return err
	}
	if publishedDigest != candidateDigest {
		return fmt.Errorf("%w: %s published digest %s, candidate digest %s",
			ErrInPlaceEdit, published.Ref, publishedDigest, candidateDigest)
	}
	return nil
}
