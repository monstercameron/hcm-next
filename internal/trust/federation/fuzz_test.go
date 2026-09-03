package federation_test

import (
	"context"
	"strings"
	"testing"
)

// FuzzTodo_TRUST_002 is the TRUST-002 fuzz target.
//
// The invariant under test is one sentence: for any input whatsoever,
// Validate either returns an error and a nil principal, or returns a fully
// valid principal whose tenant, kind, assurance, session and validity
// window all pass their own rules. There is no third outcome, and in
// particular there is no partially populated principal and no panic.
func FuzzTodo_TRUST_002(f *testing.F) {
	keys := newTestKeys(f)
	good := keys.signRS256(f, validAcmeClaims(), "acme-rsa-1")

	f.Add(good)
	f.Add("")
	f.Add("not-a-token")
	f.Add(strings.Repeat("A", 4096))
	f.Add("eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0.")
	f.Add(good + ".extra")
	f.Add(strings.ToUpper(good))

	if parts := strings.Split(good, "."); len(parts) == 3 {
		f.Add(parts[0] + "." + parts[1] + ".")
		f.Add("." + parts[1] + "." + parts[2])
	}

	f.Fuzz(func(t *testing.T, token string) {
		validator := newValidator(t, keys.source, baseTime)
		ctx := context.Background()
		principal, err := validator.Validate(ctx, token)
		if err != nil {
			if principal != nil {
				t.Fatalf("Validate returned both an error (%v) and a principal", err)
			}
			return
		}
		if principal == nil {
			t.Fatal("Validate returned neither an error nor a principal")
		}
		if err := principal.Tenant().Validate(); err != nil {
			t.Fatalf("validated principal has an invalid tenant %q: %v", principal.Tenant(), err)
		}
		if principal.Subject() == "" {
			t.Fatal("validated principal has an empty subject")
		}
		if principal.SubjectKind() == 0 {
			t.Fatal("validated principal has an unspecified subject kind")
		}
		if principal.Assurance() == 0 {
			t.Fatal("validated principal has no assurance evidence")
		}
		if principal.SessionRef() == "" {
			t.Fatal("validated principal has no session reference")
		}
		if !principal.ExpiresAt().After(principal.IssuedAt()) {
			t.Fatalf("validated principal has an empty validity window [%s, %s)", principal.IssuedAt(), principal.ExpiresAt())
		}
		if principal.IssuedAt().After(baseTime) || !baseTime.Before(principal.ExpiresAt()) {
			t.Fatalf("validated principal is not valid at the validation instant %s", baseTime)
		}
		if !strings.HasPrefix(principal.EvidenceID(), "ev:authn:") {
			t.Fatalf("validated principal has evidence id %q", principal.EvidenceID())
		}
		if strings.Contains(principal.String(), token) && token != "" {
			t.Fatal("the principal's rendered form leaks the credential")
		}
	})
}
