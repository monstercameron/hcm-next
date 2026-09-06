package auditpack

import (
	"crypto/rand"
	"testing"
)

func randomID(t *testing.T) [16]byte {
	t.Helper()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	return id
}

func TestFormatUUIDAndParseUUIDRoundTrip(t *testing.T) {
	t.Parallel()
	for i := 0; i < 20; i++ {
		id := randomID(t)
		text := formatUUID(id)
		if len(text) != 36 {
			t.Fatalf("formatUUID(%v) = %q, want 36 characters", id, text)
		}
		for _, dash := range []int{8, 13, 18, 23} {
			if text[dash] != '-' {
				t.Fatalf("formatUUID(%v) = %q, want a dash at position %d", id, text, dash)
			}
		}
		back, err := parseUUID(text)
		if err != nil {
			t.Fatalf("parseUUID(%q): %v", text, err)
		}
		if back != id {
			t.Fatalf("round trip through %q = %v, want %v", text, back, id)
		}
	}
}

func TestFormatUUIDMatchesTheCanonicalNilRendering(t *testing.T) {
	t.Parallel()
	if got, want := formatUUID([16]byte{}), "00000000-0000-0000-0000-000000000000"; got != want {
		t.Fatalf("formatUUID(zero) = %q, want %q", got, want)
	}
}

func TestParseUUIDRefusesMalformedText(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"not-a-uuid",
		"00000000-0000-0000-0000-00000000000",  // one character short
		"00000000:0000-0000-0000-000000000000", // wrong separator
		"gggggggg-0000-0000-0000-000000000000", // non-hex digits
	}
	for _, c := range cases {
		if _, err := parseUUID(c); err == nil {
			t.Errorf("parseUUID(%q) succeeded, want a refusal", c)
		}
	}
}

// TestTenantIDIsAssignableFromAnArbitrarySixteenByteValue proves the
// property this whole file exists for: TenantID and CorrelationID are
// genuine aliases for [16]byte, so a value whose static type is some other
// [16]byte-based named type (standing in for github.com/google/uuid.UUID,
// which this package never imports) assigns with no explicit conversion.
func TestTenantIDIsAssignableFromAnArbitrarySixteenByteValue(t *testing.T) {
	t.Parallel()
	type otherSixteenByteType [16]byte
	var other otherSixteenByteType
	other[0] = 0xAB

	var tenant TenantID = other // no conversion syntax
	if tenant != TenantID(other) {
		t.Fatalf("TenantID assignment lost bytes: got %v, want %v", tenant, other)
	}
}
