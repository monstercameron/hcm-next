package canonical

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/big"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// streamMagic prefixes every canonical byte stream. Bumping it is a breaking
// change to every historical digest and therefore never done in place: a new
// encoding generation gets a new profile version and a DigestMigration.
const streamMagic = "hcmnext.canonical.v1"

// Canonical value kind tags. These numbers are frozen: they are inside every
// digest ever minted.
const (
	kindAbsent   byte = 0x00
	kindBool     byte = 0x02
	kindSint     byte = 0x03
	kindUint     byte = 0x04
	kindFloat64  byte = 0x05
	kindString   byte = 0x06
	kindVerbatim byte = 0x07
	kindBytes    byte = 0x08
	kindEnum     byte = 0x09
	kindMessage  byte = 0x0A
	kindList     byte = 0x0B
	kindSet      byte = 0x0C
	kindMap      byte = 0x0D
	kindInstant  byte = 0x0E
	kindDuration byte = 0x0F
	kindDecimal  byte = 0x10
	kindCurrency byte = 0x11
	kindFloat32  byte = 0x12
)

var kindNames = map[byte]string{
	kindAbsent: "absent", kindBool: "bool", kindSint: "sint", kindUint: "uint",
	kindFloat64: "float64", kindString: "string", kindVerbatim: "verbatim",
	kindBytes: "bytes", kindEnum: "enum", kindMessage: "message", kindList: "list",
	kindSet: "set", kindMap: "map", kindInstant: "instant", kindDuration: "duration",
	kindDecimal: "decimal", kindCurrency: "currency", kindFloat32: "float32",
}

const (
	timestampName = "google.protobuf.Timestamp"
	durationName  = "google.protobuf.Duration"

	// Normalized instant range, matching google.protobuf.Timestamp:
	// 0001-01-01T00:00:00Z through 9999-12-31T23:59:59Z.
	minInstantSeconds = -62135596800
	maxInstantSeconds = 253402300799

	maxDurationSeconds = 315576000000
)

// subtreeNode selects every field beneath it. One shared instance is enough
// because the node carries no per-site state.
var subtreeNode = &pathNode{terminal: true}

// Encode projects msg into profile's canonical model and returns its canonical
// bytes. The result is deterministic across processes, architectures, locales,
// and timezones for identical material meaning.
func Encode(msg proto.Message, profile Profile) ([]byte, error) {
	plan, err := Compile(profile)
	if err != nil {
		return nil, err
	}
	return plan.Encode(msg)
}

// Encode runs a precompiled plan. Registries hold plans so that profile
// validation happens at publication rather than on every digest.
func (c *Plan) Encode(msg proto.Message) ([]byte, error) {
	out, _, err := c.encode(msg, false)
	return out, err
}

// Profile returns the profile this plan was compiled from.
func (c *Plan) Profile() Profile { return c.profile }

func (c *Plan) encode(msg proto.Message, trace bool) ([]byte, []Contribution, error) {
	if msg == nil {
		return nil, nil, newError("encode", "", ErrSchemaMismatch, "nil message")
	}
	m := msg.ProtoReflect()
	if got := m.Descriptor().FullName(); got != c.profile.MessageName {
		return nil, nil, newError("encode", "", ErrSchemaMismatch,
			"profile %s v%d canonicalizes %s, got %s",
			c.profile.ID, c.profile.Version, c.profile.MessageName, got)
	}
	e := &enc{plan: c, tracing: trace}
	body, err := e.message(m, c.root, "")
	if err != nil {
		return nil, nil, err
	}
	out := make([]byte, 0, len(body)+96)
	out = appendString(out, streamMagic)
	out = appendString(out, c.profile.ID)
	out = appendUvarint(out, uint64(c.profile.Version))
	out = appendString(out, c.profile.SchemaID)
	out = appendUvarint(out, uint64(c.profile.SchemaVersion))
	out = appendString(out, string(c.profile.MessageName))
	out = append(out, body...)
	return out, e.trace, nil
}

type enc struct {
	plan    *Plan
	tracing bool
	trace   []Contribution
}

func (e *enc) record(path string, kind byte, n int) {
	if !e.tracing {
		return
	}
	e.trace = append(e.trace, Contribution{
		Index: len(e.trace), Path: path, Kind: kindNames[kind], Length: n,
	})
}

// message encodes a message body: a field count followed by the selected fields
// in ascending field-number order.
func (e *enc) message(m protoreflect.Message, node *pathNode, prefix string) ([]byte, error) {
	if e.plan.profile.RejectUnknownFields && len(m.GetUnknown()) > 0 {
		return nil, newError("encode", prefix, ErrUnknownField,
			"%d unknown bytes retained on %s", len(m.GetUnknown()), m.Descriptor().FullName())
	}
	fields, err := e.selectFields(m.Descriptor(), node, prefix)
	if err != nil {
		return nil, err
	}
	var body []byte
	count := 0
	for _, sel := range fields {
		chunk, ok, err := e.field(m, sel)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		body = appendUvarint(body, uint64(sel.fd.Number()))
		body = append(body, chunk...)
		count++
	}
	out := appendUvarint(nil, uint64(count))
	return append(out, body...), nil
}

type selection struct {
	fd   protoreflect.FieldDescriptor
	node *pathNode
	path string
}

// selectFields resolves the material paths at this node into descriptors,
// ordered by field number. Ordering by tag, never by name or map iteration, is
// what makes the stream reproducible.
func (e *enc) selectFields(md protoreflect.MessageDescriptor, node *pathNode, prefix string) ([]selection, error) {
	var out []selection
	join := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + "." + name
	}
	if node.subtree() {
		fs := md.Fields()
		for i := range fs.Len() {
			fd := fs.Get(i)
			out = append(out, selection{fd: fd, node: subtreeNode, path: join(string(fd.Name()))})
		}
	} else {
		for name, child := range node.children {
			fd := md.Fields().ByName(protoreflect.Name(name))
			if fd == nil {
				return nil, newError("encode", join(name), ErrInvalidProfile,
					"field %q is not defined on %s", name, md.FullName())
			}
			out = append(out, selection{fd: fd, node: child, path: join(name)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].fd.Number() < out[j].fd.Number() })
	return out, nil
}

// field encodes one selected field, returning ok=false when the field
// contributes nothing at all (unset with non-distinct presence).
func (e *enc) field(m protoreflect.Message, sel selection) ([]byte, bool, error) {
	fd, path := sel.fd, sel.path
	if !m.Has(fd) {
		if fd.HasPresence() || e.plan.presence[path] {
			e.record(path, kindAbsent, 1)
			return []byte{kindAbsent}, true, nil
		}
		return nil, false, nil
	}
	chunk, err := e.value(fd, m.Get(fd), sel.node, path)
	if err != nil {
		return nil, false, err
	}
	return chunk, true, nil
}

func (e *enc) value(fd protoreflect.FieldDescriptor, v protoreflect.Value, node *pathNode, path string) ([]byte, error) {
	switch {
	case fd.IsMap():
		return e.mapValue(fd, v.Map(), node, path)
	case fd.IsList():
		return e.listValue(fd, v.List(), node, path)
	default:
		return e.singular(fd, v, node, path)
	}
}

// mapValue encodes a map as sorted key/value entries. Go map iteration order is
// immaterial by construction: entries are sorted by their canonical key bytes.
func (e *enc) mapValue(fd protoreflect.FieldDescriptor, mp protoreflect.Map, node *pathNode, path string) ([]byte, error) {
	type entry struct{ key, val []byte }
	entries := make([]entry, 0, mp.Len())
	var rangeErr error
	mp.Range(func(mk protoreflect.MapKey, mv protoreflect.Value) bool {
		kb, err := e.singular(fd.MapKey(), mk.Value(), subtreeNode, path+".<key>")
		if err != nil {
			rangeErr = err
			return false
		}
		vb, err := e.singular(fd.MapValue(), mv, node, path)
		if err != nil {
			rangeErr = err
			return false
		}
		entries = append(entries, entry{key: kb, val: vb})
		return true
	})
	if rangeErr != nil {
		return nil, rangeErr
	}
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].key, entries[j].key) < 0 })
	for i := 1; i < len(entries); i++ {
		if bytes.Equal(entries[i-1].key, entries[i].key) {
			return nil, newError("encode", path, ErrDuplicateSetMember,
				"two map keys share canonical bytes")
		}
	}
	out := []byte{kindMap}
	out = appendUvarint(out, uint64(len(entries)))
	for _, en := range entries {
		out = append(out, en.key...)
		out = append(out, en.val...)
	}
	e.record(path, kindMap, len(out))
	return out, nil
}

// listValue encodes a repeated field. Declared sets sort by canonical element
// bytes and reject duplicates; everything else retains its declared order.
func (e *enc) listValue(fd protoreflect.FieldDescriptor, list protoreflect.List, node *pathNode, path string) ([]byte, error) {
	elems := make([][]byte, 0, list.Len())
	for i := range list.Len() {
		b, err := e.singular(fd, list.Get(i), node, path)
		if err != nil {
			return nil, err
		}
		elems = append(elems, b)
	}
	kind := kindList
	if e.plan.sets[path] {
		kind = kindSet
		sort.Slice(elems, func(i, j int) bool { return bytes.Compare(elems[i], elems[j]) < 0 })
		for i := 1; i < len(elems); i++ {
			if bytes.Equal(elems[i-1], elems[i]) {
				return nil, newError("encode", path, ErrDuplicateSetMember,
					"two members share canonical bytes")
			}
		}
	}
	out := []byte{kind}
	out = appendUvarint(out, uint64(len(elems)))
	for _, b := range elems {
		out = append(out, b...)
	}
	e.record(path, kind, len(out))
	return out, nil
}

func (e *enc) singular(fd protoreflect.FieldDescriptor, v protoreflect.Value, node *pathNode, path string) ([]byte, error) {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		b := byte(0)
		if v.Bool() {
			b = 1
		}
		e.record(path, kindBool, 2)
		return []byte{kindBool, b}, nil

	case protoreflect.Int32Kind, protoreflect.Int64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
		out := appendUvarint([]byte{kindSint}, zigzag(v.Int()))
		e.record(path, kindSint, len(out))
		return out, nil

	case protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		out := appendUvarint([]byte{kindUint}, v.Uint())
		e.record(path, kindUint, len(out))
		return out, nil

	case protoreflect.FloatKind:
		f := v.Float()
		if err := checkFloat(f, path); err != nil {
			return nil, err
		}
		out := make([]byte, 5)
		out[0] = kindFloat32
		binary.BigEndian.PutUint32(out[1:], math.Float32bits(float32(normalizeZero(f))))
		e.record(path, kindFloat32, len(out))
		return out, nil

	case protoreflect.DoubleKind:
		f := v.Float()
		if err := checkFloat(f, path); err != nil {
			return nil, err
		}
		out := make([]byte, 9)
		out[0] = kindFloat64
		binary.BigEndian.PutUint64(out[1:], math.Float64bits(normalizeZero(f)))
		e.record(path, kindFloat64, len(out))
		return out, nil

	case protoreflect.StringKind:
		return e.stringValue(v.String(), path)

	case protoreflect.BytesKind:
		b := v.Bytes()
		out := appendUvarint([]byte{kindBytes}, uint64(len(b)))
		out = append(out, b...)
		e.record(path, kindBytes, len(out))
		return out, nil

	case protoreflect.EnumKind:
		out := appendUvarint([]byte{kindEnum}, zigzag(int64(v.Enum())))
		out = appendUvarint(out, uint64(e.plan.profile.SchemaVersion))
		e.record(path, kindEnum, len(out))
		return out, nil

	case protoreflect.MessageKind, protoreflect.GroupKind:
		return e.messageValue(fd, v.Message(), node, path)

	default:
		return nil, newError("encode", path, ErrUnrepresentable,
			"protobuf kind %v has no canonical form", fd.Kind())
	}
}

func (e *enc) messageValue(fd protoreflect.FieldDescriptor, m protoreflect.Message, node *pathNode, path string) ([]byte, error) {
	switch fd.Message().FullName() {
	case timestampName:
		return e.instant(m, path)
	case durationName:
		return e.duration(m, path)
	}
	body, err := e.message(m, node, path)
	if err != nil {
		return nil, err
	}
	out := appendUvarint([]byte{kindMessage}, uint64(len(body)))
	out = append(out, body...)
	e.record(path, kindMessage, len(out))
	return out, nil
}

// instant encodes google.protobuf.Timestamp as UTC seconds and nanoseconds.
// No timezone database, host locale, or wall clock is consulted.
func (e *enc) instant(m protoreflect.Message, path string) ([]byte, error) {
	secs, nanos := m.Get(m.Descriptor().Fields().ByName("seconds")).Int(),
		m.Get(m.Descriptor().Fields().ByName("nanos")).Int()
	if secs < minInstantSeconds || secs > maxInstantSeconds {
		return nil, newError("encode", path, ErrUnrepresentable,
			"instant seconds %d outside the normalized range", secs)
	}
	if nanos < 0 || nanos > 999999999 {
		return nil, newError("encode", path, ErrUnrepresentable,
			"instant nanos %d outside [0,1e9)", nanos)
	}
	out := appendUvarint([]byte{kindInstant}, zigzag(secs))
	out = appendUvarint(out, uint64(nanos))
	e.record(path, kindInstant, len(out))
	return out, nil
}

func (e *enc) duration(m protoreflect.Message, path string) ([]byte, error) {
	secs, nanos := m.Get(m.Descriptor().Fields().ByName("seconds")).Int(),
		m.Get(m.Descriptor().Fields().ByName("nanos")).Int()
	if secs < -maxDurationSeconds || secs > maxDurationSeconds {
		return nil, newError("encode", path, ErrUnrepresentable,
			"duration seconds %d outside the normalized range", secs)
	}
	if nanos <= -1000000000 || nanos >= 1000000000 {
		return nil, newError("encode", path, ErrUnrepresentable,
			"duration nanos %d outside (-1e9,1e9)", nanos)
	}
	if (secs < 0 && nanos > 0) || (secs > 0 && nanos < 0) {
		return nil, newError("encode", path, ErrUnrepresentable,
			"duration seconds and nanos disagree in sign")
	}
	out := appendUvarint([]byte{kindDuration}, zigzag(secs))
	out = appendUvarint(out, zigzag(nanos))
	e.record(path, kindDuration, len(out))
	return out, nil
}

// stringValue applies the declared string discipline: fixed decimal, currency
// code, verbatim bytes, or NFC-normalized text. Invalid UTF-8 always fails.
func (e *enc) stringValue(s, path string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, newError("encode", path, ErrInvalidUTF8, "string is not valid UTF-8")
	}
	if dec, ok := e.plan.decimals[path]; ok {
		return e.decimal(s, dec, path)
	}
	if e.plan.currency[path] {
		return e.currency(s, path)
	}
	kind := kindString
	if e.plan.verbatim[path] {
		kind = kindVerbatim
	} else {
		s = norm.NFC.String(s)
	}
	out := appendUvarint([]byte{kind}, uint64(len(s)))
	out = append(out, s...)
	e.record(path, kind, len(out))
	return out, nil
}

func (e *enc) currency(s, path string) ([]byte, error) {
	if len(s) != 3 {
		return nil, newError("encode", path, ErrUnrepresentable,
			"currency code %q is not three characters", s)
	}
	up := make([]byte, 3)
	for i := range 3 {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z':
			up[i] = ch - 'a' + 'A'
		case ch >= 'A' && ch <= 'Z':
			up[i] = ch
		default:
			return nil, newError("encode", path, ErrUnrepresentable,
				"currency code %q is not ISO 4217 alphabetic", s)
		}
	}
	out := appendUvarint([]byte{kindCurrency}, 3)
	out = append(out, up...)
	e.record(path, kindCurrency, len(out))
	return out, nil
}

// decimal encodes sign, unscaled magnitude, and scale. Semantically equal
// values do not acquire different trailing-scale forms unless the schema
// declares scale itself material.
func (e *enc) decimal(s string, dec Decimal, path string) ([]byte, error) {
	neg, unscaled, scale, err := parseDecimal(s)
	if err != nil {
		return nil, newError("encode", path, ErrUnrepresentable, "fixed decimal %q: %s", s, err.Error())
	}
	if !dec.ScaleMaterial {
		ten := big.NewInt(10)
		q, r := new(big.Int), new(big.Int)
		for scale > 0 && unscaled.Sign() != 0 {
			q.QuoRem(unscaled, ten, r)
			if r.Sign() != 0 {
				break
			}
			unscaled.Set(q)
			scale--
		}
		if unscaled.Sign() == 0 {
			scale = 0
		}
	}
	if unscaled.Sign() == 0 {
		neg = false
	}
	sign := byte(0)
	if neg {
		sign = 1
	}
	mag := unscaled.Bytes()
	out := []byte{kindDecimal, sign}
	out = appendUvarint(out, uint64(len(mag)))
	out = append(out, mag...)
	out = appendUvarint(out, zigzag(int64(scale)))
	e.record(path, kindDecimal, len(out))
	return out, nil
}

type decimalError string

func (d decimalError) Error() string { return string(d) }

func parseDecimal(s string) (neg bool, unscaled *big.Int, scale int32, err error) {
	if s == "" {
		return false, nil, 0, decimalError("empty literal")
	}
	body := s
	switch body[0] {
	case '-':
		neg, body = true, body[1:]
	case '+':
		body = body[1:]
	}
	intPart, fracPart := body, ""
	if i := strings.IndexByte(body, '.'); i >= 0 {
		intPart, fracPart = body[:i], body[i+1:]
	}
	if intPart == "" && fracPart == "" {
		return false, nil, 0, decimalError("no digits")
	}
	digits := intPart + fracPart
	if digits == "" {
		return false, nil, 0, decimalError("no digits")
	}
	for i := range len(digits) {
		if digits[i] < '0' || digits[i] > '9' {
			return false, nil, 0, decimalError("non-digit character")
		}
	}
	if len(fracPart) > math.MaxInt32 {
		return false, nil, 0, decimalError("scale overflow")
	}
	n, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return false, nil, 0, decimalError("not a base-10 integer")
	}
	return neg, n, int32(len(fracPart)), nil
}

// normalizeZero collapses negative zero onto positive zero so that two values
// that compare equal never produce different canonical bytes.
func normalizeZero(f float64) float64 {
	if f == 0 {
		return 0
	}
	return f
}

func checkFloat(f float64, path string) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return newError("encode", path, ErrUnrepresentable,
			"floating point value has no canonical form")
	}
	return nil
}

// zigzag maps signed integers onto unsigned so that small magnitudes of either
// sign stay short under minimal-width varint encoding.
func zigzag(v int64) uint64 { return uint64(v<<1) ^ uint64(v>>63) }

func appendUvarint(b []byte, v uint64) []byte { return binary.AppendUvarint(b, v) }

func appendString(b []byte, s string) []byte {
	b = appendUvarint(b, uint64(len(s)))
	return append(b, s...)
}
