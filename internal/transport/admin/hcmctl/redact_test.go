package hcmctl

import (
	"errors"
	"strings"
	"testing"
)

// TestRedactScrubsBearerCredentials proves hcmctl's one redaction choke
// point actually removes a bearer credential from a diagnostic string,
// case-insensitively and regardless of what surrounds it - the shape a
// wrapped dial error or gRPC status message would carry it in.
func TestRedactScrubsBearerCredentials(t *testing.T) {
	cases := []string{
		"dial tcp: authorization: Bearer super-secret-token failed",
		"context deadline exceeded while sending BEARER ABC123.DEF456.GHI789",
		"bearer   tok3n-with-weird-spacing",
	}
	for _, in := range cases {
		out := redact(in)
		if strings.Contains(strings.ToLower(out), "secret") || strings.Contains(out, "ABC123") || strings.Contains(out, "tok3n") {
			t.Fatalf("redact(%q) = %q, still leaks the credential", in, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Fatalf("redact(%q) = %q, expected a [REDACTED] marker", in, out)
		}
	}
}

// TestRedactErrorNeverEchoesTheUnderlyingCredential proves redactError
// applies the same scrub to a wrapped error's message.
func TestRedactErrorNeverEchoesTheUnderlyingCredential(t *testing.T) {
	err := errors.New("dial: authorization: Bearer hcmn1.super.secret.jwt-like-token rejected")
	got := redactError(err)
	if strings.Contains(got, "super.secret.jwt-like-token") {
		t.Fatalf("redactError leaked the credential: %q", got)
	}
	if redactError(nil) != "" {
		t.Fatal("redactError(nil) must be empty")
	}
}
