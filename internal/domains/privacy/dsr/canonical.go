package dsr

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// appendField appends a length-prefixed (label, value) pair to dst, mirroring
// the anti-ambiguity framing internal/governance/legal, internal/governance/
// privacy and internal/trust/authz each carry their own copy of for their
// own canonical digests: two adjacent fields can never be reparsed as a
// different split, and a field that is empty still contributes its label
// and a zero length rather than vanishing from the encoding.
func appendField(dst []byte, label, value string) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(label)))
	dst = append(dst, label...)
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}

// appendFields appends every (label, value) pair in pairs, in the order
// given. It panics on an odd argument count, which is a caller bug, not a
// runtime condition this package needs to recover from.
func appendFields(dst []byte, pairs ...string) []byte {
	if len(pairs)%2 != 0 {
		panic("dsr: appendFields needs an even number of label/value arguments")
	}
	for i := 0; i < len(pairs); i += 2 {
		dst = appendField(dst, pairs[i], pairs[i+1])
	}
	return dst
}

// digestHex returns the lowercase hex sha256 digest of canonicalBytes.
func digestHex(canonicalBytes []byte) string {
	sum := sha256.Sum256(canonicalBytes)
	return hex.EncodeToString(sum[:])
}
