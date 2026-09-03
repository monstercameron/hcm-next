// Package canonicalbytes is the deterministic byte-stream writer that pure
// engines and domain calculations use to produce a reproducible inputs digest
// for a result that must be byte-for-byte identical on every process,
// architecture and run.
//
// Semantic owner: shared-engines. Phase: P1A.
//
// It is deliberately not a second canonical encoding of the kernel value
// types. Every value written here is written through the kernel's own
// Canonical() encoding; this package only frames those encodings into a
// tagged, length-prefixed stream so that a struct made of kernel values has
// one stable serialization. When a kernel value fails to encode, the writer
// records the failure instead of emitting a partial stream: a digest is never
// produced over an incomplete input.
//
// The writer has no map iteration and no wall-clock reads. Field order is
// program order, which makes the stream a property of the code that wrote it
// rather than of the runtime that ran it.
package canonicalbytes

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

// Writer errors. All are matchable with errors.Is.
var (
	// ErrSchemaRequired is returned when a stream is opened without a schema tag.
	ErrSchemaRequired = errors.New("canonicalbytes: stream schema is required")
	// ErrUnencodable is returned when a value's Canonical() yields nil, which the
	// kernel uses to mean "this value is invalid". The stream is abandoned.
	ErrUnencodable = errors.New("canonicalbytes: value has no canonical encoding")
	// ErrTagRequired is returned for an empty field tag.
	ErrTagRequired = errors.New("canonicalbytes: field tag is required")
)

// DigestAlgorithm is the algorithm identifier that prefixes every digest this
// package produces. It is part of the digest string so a stored digest never
// has to be interpreted by convention.
const DigestAlgorithm = "sha256"

// contractVersion is this package's own wire-format contract version: the
// tagged length-prefixed framing New/Field/Digest implement. It is bumped
// only when that framing itself changes in a way that would change the
// bytes produced for an unchanged set of fields; it is independent of the
// schema/version pair a caller passes to New, which versions the caller's
// own type shape rather than canonicalbytes' wire format.
const contractVersion = 1

// Version reports canonicalbytes' own wire-format contract version. It is
// part of the ARCH-GO-009 engine package contract, not a value used by any
// caller's encoding.
func Version() int { return contractVersion }

// Explain describes, in one line, the wire encoding profile a digest from
// this package is computed over: for audit logs and documentation, not for
// programmatic branching.
func Explain() string {
	return fmt.Sprintf(
		"canonicalbytes v%d: tagged length-prefixed stream (uvarint tag length + tag + uvarint payload length + payload); "+
			"opened by a $schema/$schema_version field pair; values written through each kernel type's own Canonical() encoding; "+
			"sets framed via a sorted order plus their own count so map iteration order can never affect the bytes; "+
			"digest is %s over the completed stream",
		contractVersion, DigestAlgorithm,
	)
}

// Canonicalizer is any value carrying exactly one canonical byte encoding.
// Every kernel value type in internal/kernel/values satisfies it.
type Canonicalizer interface {
	Canonical() []byte
}

// Writer accumulates a tagged, length-prefixed byte stream. The zero Writer is
// not usable; construct one with New. A Writer is not safe for concurrent use.
type Writer struct {
	buf []byte
	err error
}

// New opens a stream for the named schema at the given schema version. The
// schema tag and version are the first two fields of every stream, so two
// structurally similar payloads written under different schemas can never
// collide on the same digest.
func New(schema string, version int) *Writer {
	w := &Writer{buf: make([]byte, 0, 256)}
	if schema == "" {
		w.err = ErrSchemaRequired
		return w
	}
	w.String("$schema", schema)
	w.Int("$schema_version", int64(version))
	return w
}

// fail records the first error and stops further accumulation.
func (w *Writer) fail(err error) *Writer {
	if w.err == nil {
		w.err = err
	}
	return w
}

// Field appends one raw field. It is the primitive every other method uses.
func (w *Writer) Field(tag string, payload []byte) *Writer {
	if w.err != nil {
		return w
	}
	if tag == "" {
		return w.fail(ErrTagRequired)
	}
	w.buf = binary.AppendUvarint(w.buf, uint64(len(tag)))
	w.buf = append(w.buf, tag...)
	w.buf = binary.AppendUvarint(w.buf, uint64(len(payload)))
	w.buf = append(w.buf, payload...)
	return w
}

// String appends a string field.
func (w *Writer) String(tag, v string) *Writer { return w.Field(tag, []byte(v)) }

// Bool appends a boolean field as a single 0x00/0x01 byte.
func (w *Writer) Bool(tag string, v bool) *Writer {
	b := byte(0)
	if v {
		b = 1
	}
	return w.Field(tag, []byte{b})
}

// Int appends a signed integer field in zigzag uvarint form.
func (w *Writer) Int(tag string, v int64) *Writer {
	var enc []byte
	enc = binary.AppendUvarint(enc, uint64(v<<1)^uint64(v>>63))
	return w.Field(tag, enc)
}

// Value appends a kernel value through its own canonical encoding. A value
// that fails to encode abandons the stream rather than contributing nothing.
func (w *Writer) Value(tag string, c Canonicalizer) *Writer {
	if w.err != nil {
		return w
	}
	payload := c.Canonical()
	if payload == nil {
		return w.fail(fmt.Errorf("%w: field %q", ErrUnencodable, tag))
	}
	return w.Field(tag, payload)
}

// Optional appends a present/absent marker followed, when present, by the
// value. It exists so that "field absent" and "field present but empty" are
// distinguishable in the stream.
func (w *Writer) Optional(tag string, present bool, c Canonicalizer) *Writer {
	if w.err != nil {
		return w
	}
	w.Bool(tag+"?", present)
	if !present {
		return w
	}
	return w.Value(tag, c)
}

// Nested appends another writer's completed stream as one field. A failed
// nested writer propagates its error.
func (w *Writer) Nested(tag string, n *Writer) *Writer {
	if w.err != nil {
		return w
	}
	payload, err := n.Bytes()
	if err != nil {
		return w.fail(fmt.Errorf("canonicalbytes: nested field %q: %w", tag, err))
	}
	return w.Field(tag, payload)
}

// SortedStrings appends a set of strings in ascending byte order together with
// its own count, so the caller cannot make the stream depend on map iteration
// order. The input slice is not modified.
func (w *Writer) SortedStrings(tag string, values []string) *Writer {
	if w.err != nil {
		return w
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	w.Int(tag+"#", int64(len(sorted)))
	for _, v := range sorted {
		w.String(tag, v)
	}
	return w
}

// Count appends an element count. Pair it with a repeated field so that a
// truncated list cannot collide with a shorter one.
func (w *Writer) Count(tag string, n int) *Writer { return w.Int(tag+"#", int64(n)) }

// Err reports the first error the writer recorded, if any.
func (w *Writer) Err() error { return w.err }

// Bytes returns the completed stream, or the first recorded error. It never
// returns a partial stream.
func (w *Writer) Bytes() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	out := make([]byte, len(w.buf))
	copy(out, w.buf)
	return out, nil
}

// Digest returns "sha256:<lowercase hex>" over the completed stream.
func (w *Writer) Digest() (string, error) {
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return Digest(raw), nil
}

// Digest returns "sha256:<lowercase hex>" over raw.
func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return DigestAlgorithm + ":" + hex.EncodeToString(sum[:])
}
