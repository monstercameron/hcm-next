package values

import (
	"errors"
	"fmt"
	"strings"
)

// Presence errors. All are matchable with errors.Is.
var (
	ErrPresenceUnspecified = errors.New("values: presence state is unspecified")
	ErrPresenceState       = errors.New("values: unknown presence state")
	ErrPresenceReason      = errors.New("values: presence reason is not allowed in this state")
	ErrPresenceWire        = errors.New("values: presence wire form is malformed")
	ErrPresenceNoValue     = errors.New("values: presence carries no value")
)

// PresenceState names why a property does or does not carry a value. The zero
// value is deliberately not a state: a struct that was never populated, or a
// message field that was never set, decodes as PresenceUnspecified and fails
// Validate rather than silently reading as ABSENT or as a zero value.
type PresenceState uint8

// Presence states. VALUE is the only state that carries a readable value; the
// wire vocabulary calls it PRESENT, and this package uses VALUE so that the
// state name and the accessor agree.
const (
	// PresenceUnspecified is the zero value and is never a legal state.
	PresenceUnspecified PresenceState = iota
	// PresenceAbsent means the property was not supplied at all.
	PresenceAbsent
	// PresenceNull means the property was explicitly supplied as null.
	PresenceNull
	// PresenceUnknown means the value exists but is not known here.
	PresenceUnknown
	// PresenceRedacted means a value exists and the caller may not see it.
	PresenceRedacted
	// PresenceUnavailable means the value could not be retrieved right now.
	PresenceUnavailable
	// PresenceNotApplicable means the property does not apply to this subject.
	PresenceNotApplicable
	// PresenceValue means the property carries a readable value.
	PresenceValue
)

// presenceWire maps each state to its stable wire token.
var presenceWire = map[PresenceState]string{
	PresenceAbsent:        "ABSENT",
	PresenceNull:          "NULL",
	PresenceUnknown:       "UNKNOWN",
	PresenceRedacted:      "REDACTED",
	PresenceUnavailable:   "UNAVAILABLE",
	PresenceNotApplicable: "NOT_APPLICABLE",
	PresenceValue:         "VALUE",
}

// AllPresenceStates returns every legal state in canonical order. It never
// includes PresenceUnspecified.
func AllPresenceStates() []PresenceState {
	return []PresenceState{
		PresenceAbsent,
		PresenceNull,
		PresenceUnknown,
		PresenceRedacted,
		PresenceUnavailable,
		PresenceNotApplicable,
		PresenceValue,
	}
}

// String returns the stable wire token, or PRESENCE_UNSPECIFIED for the zero
// value. PRESENCE_UNSPECIFIED never parses back, by design.
func (s PresenceState) String() string {
	if w, ok := presenceWire[s]; ok {
		return w
	}
	return "PRESENCE_UNSPECIFIED"
}

// Valid reports whether s is a legal state.
func (s PresenceState) Valid() bool {
	_, ok := presenceWire[s]
	return ok
}

// CarriesValue reports whether s is the one state that holds a readable value.
func (s PresenceState) CarriesValue() bool { return s == PresenceValue }

// ParsePresenceState decodes a wire token. The unspecified token is rejected.
func ParsePresenceState(token string) (PresenceState, error) {
	for state, wire := range presenceWire {
		if wire == token {
			return state, nil
		}
	}
	return PresenceUnspecified, fmt.Errorf("%w: %q", ErrPresenceState, token)
}

// Presence[T] is a property that explicitly reports why it does or does not
// carry a value. It replaces pointer-and-zero-value conventions: a nil pointer
// cannot distinguish "not supplied" from "explicitly null" from "you are not
// allowed to see this", and a zero T cannot distinguish "zero" from "unknown".
//
// The zero Presence is unspecified and fails Validate.
type Presence[T any] struct {
	state  PresenceState
	value  T
	reason string
}

// NewPresence builds a non-VALUE presence in the given state with an optional
// reason, authority or evidence reference. Use Value to build a VALUE presence;
// NewPresence deliberately cannot construct one, so a value never arrives
// without a caller that meant to supply it.
func NewPresence[T any](state PresenceState, reason string) Presence[T] {
	if state == PresenceValue {
		return Presence[T]{}
	}
	return Presence[T]{state: state, reason: reason}
}

// Value returns a presence carrying v.
func Value[T any](v T) Presence[T] { return Presence[T]{state: PresenceValue, value: v} }

// Absent returns a presence for a property that was not supplied.
func Absent[T any]() Presence[T] { return Presence[T]{state: PresenceAbsent} }

// Null returns a presence for a property explicitly supplied as null.
func Null[T any]() Presence[T] { return Presence[T]{state: PresenceNull} }

// Unknown returns a presence for a value that exists but is not known here.
func Unknown[T any](reason string) Presence[T] {
	return Presence[T]{state: PresenceUnknown, reason: reason}
}

// Redacted returns a presence for a value the caller may not see.
func Redacted[T any](reason string) Presence[T] {
	return Presence[T]{state: PresenceRedacted, reason: reason}
}

// Unavailable returns a presence for a value that could not be retrieved.
func Unavailable[T any](reason string) Presence[T] {
	return Presence[T]{state: PresenceUnavailable, reason: reason}
}

// NotApplicable returns a presence for a property that does not apply.
func NotApplicable[T any](reason string) Presence[T] {
	return Presence[T]{state: PresenceNotApplicable, reason: reason}
}

// State returns the presence state.
func (p Presence[T]) State() PresenceState { return p.state }

// IsValue reports whether the presence carries a readable value.
func (p Presence[T]) IsValue() bool { return p.state == PresenceValue }

// Reason returns the reason, authority or evidence reference recorded for a
// non-VALUE state. It is always empty for a VALUE.
func (p Presence[T]) Reason() string { return p.reason }

// Get returns the value and whether one is readable. Every non-VALUE state
// returns ok=false; no state ever hands back a zero T as if it were data.
func (p Presence[T]) Get() (T, bool) {
	if p.state != PresenceValue {
		var zero T
		return zero, false
	}
	return p.value, true
}

// ValueOr returns the value, or the caller-supplied fallback for every
// non-VALUE state. The fallback is explicit precisely because the package never
// invents one.
func (p Presence[T]) ValueOr(fallback T) T {
	if v, ok := p.Get(); ok {
		return v
	}
	return fallback
}

// MustValue returns the value and panics for every non-VALUE state. Use it only
// where a prior check has already established the state.
func (p Presence[T]) MustValue() T {
	v, ok := p.Get()
	if !ok {
		panic(fmt.Sprintf("values: MustValue on presence state %s", p.state))
	}
	return v
}

// Validate reports whether the presence is internally consistent.
func (p Presence[T]) Validate() error {
	if p.state == PresenceUnspecified {
		return ErrPresenceUnspecified
	}
	if !p.state.Valid() {
		return fmt.Errorf("%w: %d", ErrPresenceState, uint8(p.state))
	}
	if p.state == PresenceValue && p.reason != "" {
		return fmt.Errorf("%w: VALUE carries reason %q", ErrPresenceReason, p.reason)
	}
	return nil
}

// ValueCodec encodes and decodes the payload of a VALUE presence. Domains
// supply their own codec; the presence wrapper never guesses an encoding.
type ValueCodec[T any] interface {
	EncodeValue(T) ([]byte, error)
	DecodeValue([]byte) (T, error)
}

// StringCodec is the identity codec for string-valued presences.
type StringCodec struct{}

// EncodeValue implements ValueCodec.
func (StringCodec) EncodeValue(v string) ([]byte, error) { return []byte(v), nil }

// DecodeValue implements ValueCodec.
func (StringCodec) DecodeValue(b []byte) (string, error) { return string(b), nil }

// BytesCodec is the identity codec for byte-slice-valued presences.
type BytesCodec struct{}

// EncodeValue implements ValueCodec.
func (BytesCodec) EncodeValue(v []byte) ([]byte, error) { return v, nil }

// DecodeValue implements ValueCodec.
func (BytesCodec) DecodeValue(b []byte) ([]byte, error) { return b, nil }

// MarshalPresence encodes a presence for a caller that is authorized to see the
// value. It is MarshalPresenceFor with authorized=true.
func MarshalPresence[T any](p Presence[T], codec ValueCodec[T]) ([]byte, error) {
	return MarshalPresenceFor(p, codec, true)
}

// MarshalPresenceFor encodes a presence. When authorized is false and the
// presence carries a value, the result is the bare REDACTED token: the value
// codec is never invoked, so the value cannot reach the wire, a log or an error
// message by accident.
//
// Wire form:
//
//	ABSENT | NULL | UNKNOWN | REDACTED | UNAVAILABLE | NOT_APPLICABLE
//	<STATE>:<escaped reason>        (non-VALUE states with a reason)
//	VALUE:<strict base64url bytes>  (VALUE only)
func MarshalPresenceFor[T any](p Presence[T], codec ValueCodec[T], authorized bool) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.state == PresenceValue {
		if !authorized {
			return []byte(presenceWire[PresenceRedacted]), nil
		}
		if codec == nil {
			return nil, fmt.Errorf("%w: VALUE needs a codec", ErrPresenceWire)
		}
		raw, err := codec.EncodeValue(p.value)
		if err != nil {
			return nil, fmt.Errorf("values: encode presence value: %w", err)
		}
		return []byte(presenceWire[PresenceValue] + ":" + revisionOpaqueEncoding.EncodeToString(raw)), nil
	}
	token := presenceWire[p.state]
	if !authorized {
		// A reason may itself describe protected data. The state stays visible
		// because it is not the value; the reason does not.
		return []byte(token), nil
	}
	if p.reason == "" {
		return []byte(token), nil
	}
	return []byte(token + ":" + escapeSegment(p.reason)), nil
}

// UnmarshalPresence decodes a presence wire form. An empty input, an unknown
// state token, a VALUE without a payload and a non-canonical escape all fail;
// nothing decodes into PresenceUnspecified.
func UnmarshalPresence[T any](text []byte, codec ValueCodec[T]) (Presence[T], error) {
	var zero Presence[T]
	s := string(text)
	if s == "" {
		return zero, fmt.Errorf("%w: empty", ErrPresenceWire)
	}
	token, rest, hasRest := strings.Cut(s, ":")
	state, err := ParsePresenceState(token)
	if err != nil {
		return zero, err
	}
	if state == PresenceValue {
		if !hasRest {
			return zero, fmt.Errorf("%w: VALUE has no payload", ErrPresenceWire)
		}
		if strings.Contains(rest, ":") {
			return zero, fmt.Errorf("%w: VALUE payload has a trailing field", ErrPresenceWire)
		}
		if codec == nil {
			return zero, fmt.Errorf("%w: VALUE needs a codec", ErrPresenceWire)
		}
		for i := 0; i < len(rest); i++ {
			c := rest[i]
			if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return zero, fmt.Errorf("%w: VALUE payload has a non-base64url byte %#x", ErrPresenceWire, c)
			}
		}
		raw, err := revisionOpaqueEncoding.DecodeString(rest)
		if err != nil {
			return zero, fmt.Errorf("%w: VALUE payload is not canonical base64url: %v", ErrPresenceWire, err)
		}
		v, err := codec.DecodeValue(raw)
		if err != nil {
			return zero, fmt.Errorf("values: decode presence value: %w", err)
		}
		return Value(v), nil
	}
	if !hasRest {
		return Presence[T]{state: state}, nil
	}
	if strings.Contains(rest, ":") {
		return zero, fmt.Errorf("%w: reason has a trailing field", ErrPresenceWire)
	}
	reason, err := unescapeSegment(rest)
	if err != nil {
		return zero, fmt.Errorf("%w: reason: %v", ErrPresenceWire, err)
	}
	return Presence[T]{state: state, reason: reason}, nil
}
