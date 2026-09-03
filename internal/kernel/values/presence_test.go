package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// recordingCodec proves that an unauthorized serialization never touches the
// value: EncodeValue records every call and returns the raw secret if invoked.
type recordingCodec struct {
	calls *int
}

func (c recordingCodec) EncodeValue(v string) ([]byte, error) {
	*c.calls++
	return []byte(v), nil
}

func (c recordingCodec) DecodeValue(b []byte) (string, error) {
	return string(b), nil
}

// TestPresenceNeverCollapsesUnknown is the primary test for MODEL-002. It proves
// that every non-VALUE presence state stays distinguishable from a value and
// from every other state, and that none of them can be read as a zero value.
func TestPresenceNeverCollapsesUnknown(t *testing.T) {
	t.Parallel()

	t.Run("ZeroPresenceIsNotAState", func(t *testing.T) {
		var p Presence[string]
		if p.State() != PresenceUnspecified {
			t.Fatalf("zero Presence state = %v, want PresenceUnspecified", p.State())
		}
		if err := p.Validate(); !errors.Is(err, ErrPresenceUnspecified) {
			t.Fatalf("zero Presence Validate() = %v, want ErrPresenceUnspecified", err)
		}
		if _, err := MarshalPresence(p, StringCodec{}); !errors.Is(err, ErrPresenceUnspecified) {
			t.Fatalf("marshal zero Presence = %v, want ErrPresenceUnspecified", err)
		}
	})

	t.Run("NonValueStatesNeverDecodeAsZeroValue", func(t *testing.T) {
		nonValue := []struct {
			wire  string
			state PresenceState
		}{
			{"ABSENT", PresenceAbsent},
			{"NULL", PresenceNull},
			{"UNKNOWN", PresenceUnknown},
			{"REDACTED", PresenceRedacted},
			{"UNAVAILABLE", PresenceUnavailable},
			{"NOT_APPLICABLE", PresenceNotApplicable},
		}
		for _, tc := range nonValue {
			t.Run(tc.wire, func(t *testing.T) {
				p, err := UnmarshalPresence([]byte(tc.wire), StringCodec{})
				if err != nil {
					t.Fatalf("UnmarshalPresence(%q) error = %v", tc.wire, err)
				}
				if p.State() != tc.state {
					t.Fatalf("state = %v, want %v", p.State(), tc.state)
				}
				if p.IsValue() {
					t.Fatalf("%s reported IsValue", tc.wire)
				}
				if got, ok := p.Get(); ok {
					t.Fatalf("Get() = (%q, true), want ok=false", got)
				}
				// The zero-valued VALUE presence is a different value.
				zeroValue := Value("")
				if p.State() == zeroValue.State() {
					t.Fatalf("%s collapsed into a zero VALUE", tc.wire)
				}
				encoded, err := MarshalPresence(p, StringCodec{})
				if err != nil {
					t.Fatalf("MarshalPresence error = %v", err)
				}
				if !bytes.Equal(encoded, []byte(tc.wire)) {
					t.Fatalf("re-encode = %q, want %q", encoded, tc.wire)
				}
				zeroEncoded, err := MarshalPresence(zeroValue, StringCodec{})
				if err != nil {
					t.Fatalf("MarshalPresence(zero VALUE) error = %v", err)
				}
				if bytes.Equal(encoded, zeroEncoded) {
					t.Fatalf("%s and a zero VALUE share wire bytes %q", tc.wire, encoded)
				}
			})
		}
	})

	t.Run("MustValuePanicsOnNonValue", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("MustValue on UNKNOWN did not panic")
			}
		}()
		_ = Unknown[string]("source unreachable").MustValue()
	})

	t.Run("EveryStateRoundTripsDistinctly", func(t *testing.T) {
		cases := []Presence[string]{
			Value("gross-pay"),
			Value(""),
			Absent[string](),
			Null[string](),
			Unknown[string]("upstream connector offline"),
			Unknown[string](""),
			Redacted[string]("classification: HIGHLY_RESTRICTED"),
			Unavailable[string]("legal hold"),
			NotApplicable[string]("worker is not on a pay cycle"),
		}
		seen := map[string]int{}
		for i, p := range cases {
			encoded, err := MarshalPresence(p, StringCodec{})
			if err != nil {
				t.Fatalf("case %d MarshalPresence error = %v", i, err)
			}
			if prev, dup := seen[string(encoded)]; dup {
				t.Fatalf("cases %d and %d share wire bytes %q", prev, i, encoded)
			}
			seen[string(encoded)] = i

			back, err := UnmarshalPresence(encoded, StringCodec{})
			if err != nil {
				t.Fatalf("case %d UnmarshalPresence(%q) error = %v", i, encoded, err)
			}
			if back.State() != p.State() || back.Reason() != p.Reason() {
				t.Fatalf("case %d round trip = %v/%q, want %v/%q", i, back.State(), back.Reason(), p.State(), p.Reason())
			}
			gotV, gotOK := back.Get()
			wantV, wantOK := p.Get()
			if gotOK != wantOK || gotV != wantV {
				t.Fatalf("case %d value round trip = (%q,%v), want (%q,%v)", i, gotV, gotOK, wantV, wantOK)
			}
		}
	})

	t.Run("UnauthorizedSerializationRedactsWithoutExposing", func(t *testing.T) {
		const secret = "123-45-6789"
		calls := 0
		codec := recordingCodec{calls: &calls}
		p := Value(secret)

		encoded, err := MarshalPresenceFor(p, codec, false)
		if err != nil {
			t.Fatalf("MarshalPresenceFor error = %v", err)
		}
		if got := string(encoded); got != "REDACTED" {
			t.Fatalf("unauthorized wire form = %q, want %q", got, "REDACTED")
		}
		if calls != 0 {
			t.Fatalf("value codec was invoked %d times for an unauthorized value", calls)
		}
		if strings.Contains(string(encoded), secret) {
			t.Fatal("secret leaked into the unauthorized wire form")
		}

		authorized, err := MarshalPresenceFor(p, codec, true)
		if err != nil {
			t.Fatalf("MarshalPresenceFor(authorized) error = %v", err)
		}
		if !strings.HasPrefix(string(authorized), "VALUE:") {
			t.Fatalf("authorized wire form = %q, want a VALUE", authorized)
		}
		if calls != 1 {
			t.Fatalf("value codec invoked %d times for an authorized value, want 1", calls)
		}

		// The redacted form decodes as REDACTED, never as the value and never
		// as a zero value.
		back, err := UnmarshalPresence(encoded, codec)
		if err != nil {
			t.Fatalf("UnmarshalPresence error = %v", err)
		}
		if back.State() != PresenceRedacted {
			t.Fatalf("decoded state = %v, want PresenceRedacted", back.State())
		}
		if _, ok := back.Get(); ok {
			t.Fatal("redacted presence exposed a value")
		}
	})

	t.Run("MalformedWireFailsClosed", func(t *testing.T) {
		bad := []string{
			"", "PRESENT", "VALUE", "value:aGk", "ABSENT:", "VALUE:!!!",
			"PRESENCE_UNSPECIFIED", "UNSPECIFIED", "NULL:x:y", "STALE",
		}
		for _, s := range bad {
			if _, err := UnmarshalPresence([]byte(s), StringCodec{}); err == nil {
				t.Errorf("UnmarshalPresence(%q) = nil error, want failure", s)
			}
		}
	})

	t.Run("ValueMayNotCarryAReason", func(t *testing.T) {
		p := Presence[string]{}
		if err := p.Validate(); err == nil {
			t.Fatal("zero presence validated")
		}
		if _, err := UnmarshalPresence([]byte("VALUE:aGk:why"), StringCodec{}); err == nil {
			t.Fatal("VALUE with a reason decoded successfully")
		}
	})
}

// TestTodo_MODEL_002_Property asserts presence invariants across every state.
func TestTodo_MODEL_002_Property(t *testing.T) {
	t.Parallel()

	all := []Presence[string]{
		Absent[string](),
		Null[string](),
		Unknown[string]("r1"),
		Redacted[string]("r2"),
		Unavailable[string]("r3"),
		NotApplicable[string]("r4"),
		Value("v"),
	}
	if len(all) != len(AllPresenceStates()) {
		t.Fatalf("covered %d states, package declares %d", len(all), len(AllPresenceStates()))
	}
	for _, p := range all {
		if err := p.Validate(); err != nil {
			t.Fatalf("%v Validate() = %v", p.State(), err)
		}
		// Property: exactly one state holds a readable value.
		_, ok := p.Get()
		if ok != (p.State() == PresenceValue) {
			t.Fatalf("%v Get() ok = %v", p.State(), ok)
		}
		// Property: encoding is idempotent and stable.
		a, err := MarshalPresence(p, StringCodec{})
		if err != nil {
			t.Fatalf("%v MarshalPresence error = %v", p.State(), err)
		}
		b, err := MarshalPresence(p, StringCodec{})
		if err != nil {
			t.Fatalf("%v MarshalPresence error = %v", p.State(), err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("%v encoding is not stable", p.State())
		}
		// Property: a state name round-trips through its wire token.
		parsed, err := ParsePresenceState(p.State().String())
		if err != nil || parsed != p.State() {
			t.Fatalf("ParsePresenceState(%q) = %v, %v", p.State().String(), parsed, err)
		}
	}
	if _, err := ParsePresenceState(PresenceUnspecified.String()); err == nil {
		t.Fatal("PresenceUnspecified parsed as a wire state")
	}
}

// TestTodo_MODEL_002_Golden pins presence wire vectors from testdata.
func TestTodo_MODEL_002_Golden(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/model_002_presence.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		Vectors []struct {
			State  string  `json:"state"`
			Reason string  `json:"reason"`
			Value  *string `json:"value"`
			Wire   string  `json:"wire"`
		} `json:"vectors"`
		Invalid []string `json:"invalid_wire"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(golden.Vectors) == 0 {
		t.Fatal("golden file has no vectors")
	}
	for _, tc := range golden.Vectors {
		state, err := ParsePresenceState(tc.State)
		if err != nil {
			t.Errorf("ParsePresenceState(%q) error = %v", tc.State, err)
			continue
		}
		var p Presence[string]
		if state == PresenceValue {
			p = Value(*tc.Value)
		} else {
			p = NewPresence[string](state, tc.Reason)
		}
		got, err := MarshalPresence(p, StringCodec{})
		if err != nil {
			t.Errorf("MarshalPresence(%v) error = %v", state, err)
			continue
		}
		if string(got) != tc.Wire {
			t.Errorf("state %s wire = %q, want %q", tc.State, got, tc.Wire)
		}
		back, err := UnmarshalPresence([]byte(tc.Wire), StringCodec{})
		if err != nil {
			t.Errorf("UnmarshalPresence(%q) error = %v", tc.Wire, err)
			continue
		}
		if back.State() != state || back.Reason() != tc.Reason {
			t.Errorf("decode %q = %v/%q, want %v/%q", tc.Wire, back.State(), back.Reason(), state, tc.Reason)
		}
	}
	for _, bad := range golden.Invalid {
		if _, err := UnmarshalPresence([]byte(bad), StringCodec{}); err == nil {
			t.Errorf("invalid golden vector %q decoded successfully", bad)
		}
	}
}

// TestTodo_MODEL_002_Security proves that redaction is not reversible from the
// wire form and that an unauthorized encode never runs the value codec.
func TestTodo_MODEL_002_Security(t *testing.T) {
	t.Parallel()

	const secret = "sensitive-salary-482000"
	calls := 0
	codec := recordingCodec{calls: &calls}

	for _, p := range []Presence[string]{
		Value(secret),
		Redacted[string](secret),
	} {
		encoded, err := MarshalPresenceFor(p, codec, false)
		if err != nil {
			t.Fatalf("MarshalPresenceFor error = %v", err)
		}
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("secret leaked in %q", encoded)
		}
		if string(encoded) != "REDACTED" {
			t.Fatalf("unauthorized encoding = %q, want REDACTED", encoded)
		}
	}
	if calls != 0 {
		t.Fatalf("value codec ran %d times under an unauthorized encode", calls)
	}

	// A reason is evidence, not the value: an authorized encode of a REDACTED
	// presence still never carries a value.
	encoded, err := MarshalPresenceFor(Redacted[string]("policy DLP-9"), codec, true)
	if err != nil {
		t.Fatalf("MarshalPresenceFor error = %v", err)
	}
	if strings.HasPrefix(string(encoded), "VALUE:") {
		t.Fatalf("REDACTED presence encoded as a VALUE: %q", encoded)
	}
}

// FuzzTodo_MODEL_002 checks that presence decoding never panics and that any
// wire form that decodes re-encodes to the exact input bytes.
func FuzzTodo_MODEL_002(f *testing.F) {
	for _, s := range []string{
		"ABSENT", "NULL", "UNKNOWN", "REDACTED", "UNAVAILABLE", "NOT_APPLICABLE",
		"VALUE:", "VALUE:aGk", "UNKNOWN:connector%20offline", "", "VALUE:aGk:x",
		"REDACTED:%00", "NULL:", "value:aGk",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := UnmarshalPresence([]byte(s), StringCodec{})
		if err != nil {
			return
		}
		if err := p.Validate(); err != nil {
			t.Fatalf("decoded %q into an invalid presence: %v", s, err)
		}
		if p.State() == PresenceUnspecified {
			t.Fatalf("decoded %q as an unspecified presence", s)
		}
		if _, ok := p.Get(); ok != (p.State() == PresenceValue) {
			t.Fatalf("decoded %q exposes a value in state %v", s, p.State())
		}
		again, err := MarshalPresence(p, StringCodec{})
		if err != nil {
			t.Fatalf("re-encode of %q failed: %v", s, err)
		}
		if string(again) != s {
			t.Fatalf("re-encode = %q, want %q", again, s)
		}
	})
}
