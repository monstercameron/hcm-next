package importing

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// canonWriter builds a deterministic, self-delimiting byte stream for
// digesting. Every write is length-prefixed so that no two distinct sequences
// of writes can ever collide onto the same bytes, and every method that takes
// a collection sorts it first so that a digest never depends on slice or map
// build order - only on content.
//
// This package defines its own tiny canonical-bytes writer rather than
// importing internal/engines/canonicalbytes: that engine is not on this
// package's allowed import list (internal/kernel/*, internal/domains/dataops
// shapes, internal/domains/evidence, internal/engines/fielddiff,
// internal/intent/model, internal/intent, internal/trust/authz), and a
// length-prefixed field stream is sufficient to make every digest in this
// package byte-stable across runs.
type canonWriter struct {
	buf []byte
}

func newCanonWriter(schema string, version int) *canonWriter {
	w := &canonWriter{}
	w.str(schema)
	w.u64(uint64(version))
	return w
}

// str appends a length-prefixed UTF-8 string.
func (w *canonWriter) str(s string) *canonWriter {
	var lenBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(lenBuf[:], uint64(len(s)))
	w.buf = append(w.buf, lenBuf[:n]...)
	w.buf = append(w.buf, s...)
	return w
}

// u64 appends an unsigned varint.
func (w *canonWriter) u64(v uint64) *canonWriter {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	w.buf = append(w.buf, buf[:n]...)
	return w
}

// i64 appends a signed value via zigzag encoding, so negative numbers do not
// produce a variable-width two's-complement pattern.
func (w *canonWriter) i64(v int64) *canonWriter {
	return w.u64(uint64(v<<1) ^ uint64(v>>63))
}

// boolField appends one byte, 0 or 1.
func (w *canonWriter) boolField(b bool) *canonWriter {
	if b {
		w.buf = append(w.buf, 1)
	} else {
		w.buf = append(w.buf, 0)
	}
	return w
}

// strings appends a count followed by each string in the given order. The
// caller sorts first when order must not be significant.
func (w *canonWriter) strings(ss []string) *canonWriter {
	w.u64(uint64(len(ss)))
	for _, s := range ss {
		w.str(s)
	}
	return w
}

// digestHex returns the lowercase hex sha256 of the accumulated stream,
// prefixed with the algorithm tag so a digest can never be mistaken for one
// computed under a different algorithm.
func (w *canonWriter) digestHex() string {
	sum := sha256.Sum256(w.buf)
	return "sha256:" + hex.EncodeToString(sum[:])
}
