package transaction

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"

	"github.com/monstercameron/hcm-next/internal/intent"
)

// resolutionMagic prefixes every canonical byte stream this file produces.
// Changing it is a new encoding generation, never an edit in place.
const resolutionMagic = "hcmnext.transaction.consistency_boundary_resolution.v1"

// enc is the length-framed canonical byte builder. It mirrors the pattern
// internal/intent uses for its own material and plan digests: every element
// is length-framed before its bytes, and a set sorts by its own encoded
// bytes so that Go map iteration and slice construction order can never
// change the digest.
type enc struct{ buf []byte }

func newEnc(magic string) *enc {
	e := &enc{}
	e.str(magic)
	return e
}

func (e *enc) bytes() []byte { return e.buf }

func (e *enc) uvarint(v uint64) *enc {
	e.buf = binary.AppendUvarint(e.buf, v)
	return e
}

func (e *enc) str(s string) *enc {
	e.buf = binary.AppendUvarint(e.buf, uint64(len(s)))
	e.buf = append(e.buf, s...)
	return e
}

func (e *enc) raw(b []byte) *enc {
	e.buf = binary.AppendUvarint(e.buf, uint64(len(b)))
	e.buf = append(e.buf, b...)
	return e
}

func (e *enc) boolean(b bool) *enc {
	if b {
		return e.uvarint(1)
	}
	return e.uvarint(0)
}

// encodeParticipantSet encodes a participant slice as an unordered set:
// elements sort by their own encoded bytes, so the order the resolver
// happened to append participants in is immaterial to the digest.
func encodeParticipantSet(e *enc, items []intent.PlanParticipant) {
	encoded := make([][]byte, 0, len(items))
	for _, p := range items {
		sub := &enc{}
		sub.str(p.ParticipantID).str(p.StreamID).str(p.StorageClass).boolean(p.Local)
		encoded = append(encoded, sub.bytes())
	}
	sort.Slice(encoded, func(i, j int) bool { return string(encoded[i]) < string(encoded[j]) })
	e.uvarint(uint64(len(encoded)))
	for _, b := range encoded {
		e.raw(b)
	}
}

// encodeStringList encodes an ordered sequence: order is material, because
// LockOrder's whole purpose is to record a sequence, not a set.
func encodeStringList(e *enc, items []string) {
	e.uvarint(uint64(len(items)))
	for _, s := range items {
		e.str(s)
	}
}

// canonicalBytes returns the resolution's canonical semantic encoding. The
// resolution's own digest is excluded: a digest never hashes itself.
func (r Resolution) canonicalBytes() []byte {
	e := newEnc(resolutionMagic)
	e.str(r.PlanID)
	e.str(r.Fence.BoundaryID).str(r.Fence.CoordinatorID).uvarint(r.Fence.Epoch)
	encodeParticipantSet(e, r.Admitted)
	encodeParticipantSet(e, r.Effects)
	encodeStringList(e, r.LockOrder)
	e.uvarint(uint64(r.CrossBoundaryDisposition))
	return e.bytes()
}

// computeDigest hashes the canonical semantic content.
func (r Resolution) computeDigest() string {
	sum := sha256.Sum256(r.canonicalBytes())
	return hex.EncodeToString(sum[:])
}
