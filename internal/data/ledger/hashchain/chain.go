package hashchain

import (
	"encoding/binary"
	"fmt"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/hcm-next/internal/engines/wire/canonical"
	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
)

// GenesisHash is the prev_hash of a stream's first chain link (sequence 1).
// It is a value no real digest ever produces (digest.Reference rejects an
// empty digest), so genesis can never be mistaken for a recorded chain hash.
const GenesisHash = ""

// ChainLinkProfileID names the canonicalization profile chain-link digests
// are minted and verified under. It is registered only by [NewRegistry], on
// a registry private to this package, so it can never collide with the
// profile internal/ledger.KernelDigester registers for the event digest
// itself even though both project the same TypedPayload message.
const ChainLinkProfileID = "hcmnext.ledger_event_hash_chain_link"

// chainLinkSchemaRef is the fixed schema identifier bound into every
// chain-link digest. It names no real payload_schema row - it exists only to
// keep the chain-link preimage distinct from every other TypedPayload this
// platform ever hashes, the same role SchemaReference.SchemaId plays for
// internal/ledger.KernelDigester.
const chainLinkSchemaRef = "hcmnext.ledger_hash_chain_link.v1"

// ChainLinkProfileV1 is the canonicalization profile for a chain link's
// digest input: the previous chain hash and the event's own digest, framed
// unambiguously (see [frameLinkInput]) and carried as TypedPayload wire
// bytes so the digest is minted through internal/engines/wire/digest rather than
// an ad hoc hash.
func ChainLinkProfileV1() canonical.Profile {
	return canonical.Profile{
		ID:                  ChainLinkProfileID,
		Version:             1,
		SchemaID:            digest.SchemaIntentsV1,
		SchemaVersion:       digest.SchemaIntentsV1Version,
		MessageName:         "hcmnext.intents.v1.TypedPayload",
		Material:            []string{"schema.schema_id", "protobuf_wire_bytes"},
		RejectUnknownFields: true,
	}
}

// NewRegistry returns a digest registry publishing sha256 and
// ChainLinkProfileV1. Each caller gets its own registry, matching
// internal/engines/wire/digest's own "no shared mutable global" discipline.
func NewRegistry() (*digest.Registry, error) {
	r := digest.NewRegistry()
	if err := r.RegisterProfile(ChainLinkProfileV1(), digest.ScopeSpec{}); err != nil {
		return nil, fmt.Errorf("hashchain: register chain link profile: %w", err)
	}
	return r, nil
}

// Digester computes chain-link digests through a registered
// internal/engines/wire/digest profile. It holds no mutable state beyond the
// registry it was built with, so one Digester is safe to share across
// streams and goroutines.
type Digester struct {
	registry *digest.Registry
}

// NewDigester wraps a registry that publishes ChainLinkProfileV1 - ordinarily
// one built by [NewRegistry].
func NewDigester(registry *digest.Registry) *Digester {
	return &Digester{registry: registry}
}

// frameLinkInput concatenates prevHash and eventDigest with an explicit
// 64-bit big-endian length prefix on each field, the same discipline
// internal/data/ledger.SHA256Digester and internal/engines/wire/digest's own
// scopeDigest use. Length framing is what makes the two-field concatenation
// unambiguous: without it, prevHash="ab"+eventDigest="cd" would hash
// identically to prevHash="a"+eventDigest="bcd".
func frameLinkInput(prevHash, eventDigest string) []byte {
	var out []byte
	for _, field := range [...]string{prevHash, eventDigest} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		out = append(out, length[:]...)
		out = append(out, field...)
	}
	return out
}

// Link computes chain_hash = digest(prevHash || eventDigest) and returns the
// algorithm identifier alongside the hex digest. prevHash is [GenesisHash]
// for a stream's first event.
func (d *Digester) Link(prevHash, eventDigest string) (algorithm, chainHash string, err error) {
	if d == nil || d.registry == nil {
		return "", "", fmt.Errorf("hashchain: digest registry is required")
	}
	if eventDigest == "" {
		return "", "", fmt.Errorf("hashchain: event digest is required to extend a chain")
	}
	msg := &intentsv1.TypedPayload{
		Schema:            &intentsv1.SchemaReference{SchemaId: chainLinkSchemaRef},
		ProtobufWireBytes: frameLinkInput(prevHash, eventDigest),
	}
	ref, _, err := d.registry.Compute(msg, ChainLinkProfileID)
	if err != nil {
		return "", "", fmt.Errorf("hashchain: compute chain link digest: %w", err)
	}
	return ref.AlgorithmID, ref.Digest, nil
}
