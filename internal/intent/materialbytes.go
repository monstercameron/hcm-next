package intent

import (
	"encoding/binary"
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// The kernel's structured material content — planned writes, effects, child
// bindings, reservations, required approvals, source baselines — has no
// generated Protobuf message yet. Until schema/proto grows one, this file is
// the deterministic encoding that occupies the proposal payload's byte field
// and that the PROPOSAL canonicalization profile then digests.
//
// Two rules make the encoding safe to hash:
//
//  1. Every element is length-framed before its bytes, so no concatenation of
//     two different field lists can produce identical output.
//  2. Sets are sorted by their own encoded bytes, so Go map iteration order,
//     locale and slice construction order are all immaterial.
//
// The stream magic is frozen. Changing any rule below is a new encoding
// generation with a new material schema version, never an edit in place.
const (
	materialMagic          = "hcmnext.intent.material.v1"
	materialSchemaID       = "hcmnext.intents.v1.kernel_material_proposal"
	materialSchemaVersion  = 1
	materialProtobufName   = "hcmnext.intents.v1.kernel_material_proposal"
	planMagic              = "hcmnext.intent.plan.v1"
	planSchemaID           = "hcmnext.transaction.v1.kernel_transaction_plan"
	planSchemaVersionValue = 1
)

// MaterialSchema is the schema reference the kernel's material proposal
// encoding carries. It names the encoding, not a Protobuf descriptor: no
// generated message for this content exists yet.
func MaterialSchema() SchemaRef {
	return SchemaRef{
		SchemaID:         materialSchemaID,
		Version:          materialSchemaVersion,
		ProtobufFullName: materialProtobufName,
		DescriptorDigest: "",
	}
}

// enc is the length-framed canonical byte builder.
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

// optional encodes an absent value distinctly from a present empty one.
func (e *enc) optionalStr(s *string) *enc {
	if s == nil {
		return e.uvarint(0)
	}
	return e.uvarint(1).str(*s)
}

func (e *enc) instant(i values.Instant) *enc {
	if !i.IsSet() {
		return e.uvarint(0)
	}
	sec, nsec := i.Unix()
	return e.uvarint(1).uvarint(uint64(sec + 62135596800)).uvarint(uint64(nsec))
}

func (e *enc) canonical(v interface{ Canonical() []byte }) *enc {
	return e.raw(v.Canonical())
}

// list encodes an ordered sequence: order is material and is preserved.
func encodeList[T any](e *enc, items []T, encode func(*enc, T)) *enc {
	e.uvarint(uint64(len(items)))
	for _, it := range items {
		sub := &enc{}
		encode(sub, it)
		e.raw(sub.bytes())
	}
	return e
}

// set encodes an unordered collection: elements sort by their own canonical
// bytes, so construction order cannot change the digest.
func encodeSet[T any](e *enc, items []T, encode func(*enc, T)) *enc {
	encoded := make([][]byte, 0, len(items))
	for _, it := range items {
		sub := &enc{}
		encode(sub, it)
		encoded = append(encoded, sub.bytes())
	}
	sort.Slice(encoded, func(i, j int) bool {
		return string(encoded[i]) < string(encoded[j])
	})
	e.uvarint(uint64(len(encoded)))
	for _, b := range encoded {
		e.raw(b)
	}
	return e
}
