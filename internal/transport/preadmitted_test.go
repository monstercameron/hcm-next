package transport

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type preAdmissionTestVerifier struct {
	principals map[string]*trust.Principal
	calls      atomic.Int64
}

func (v *preAdmissionTestVerifier) Verify(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
	v.calls.Add(1)
	if principal, ok := v.principals[credential.Token]; ok {
		return principal, nil
	}
	return nil, trust.ErrInvalidCredential
}

func newPreAdmissionTestPrincipal(t *testing.T, subject string) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               values.TenantId("acme-corp"),
		Subject:              subject,
		SubjectKind:          trust.SubjectKindHuman,
		OrganizationScopeID:  "org-north-america",
		Roles:                []string{"intent_author"},
		AuthorityRefs:        []string{"authority:position:vp-engineering"},
		Purposes:             []string{"hcm_operations"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceSubstantial,
		SessionRef:           "session-8fb2c1",
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest-" + subject,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return principal
}

func preAdmissionTestConfig(verifier trust.Verifier) Config {
	return Config{
		Verifier: verifier,
		Now:      func() time.Time { return time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) },
		NewRequestID: func() string {
			return "req-preadmitted"
		},
	}
}

func TestPreAdmissionFingerprintMismatch(t *testing.T) {
	verifier := &preAdmissionTestVerifier{principals: map[string]*trust.Principal{
		"one": newPreAdmissionTestPrincipal(t, "user-one"),
		"two": newPreAdmissionTestPrincipal(t, "user-two"),
	}}
	cfg := preAdmissionTestConfig(verifier)
	ctx, first, _, err := WithPreAdmission(context.Background(), cfg,
		MapMetadata{AuthorizationMetadataKey: {"Bearer one"}}, "/method")
	if err != nil {
		t.Fatalf("WithPreAdmission: %v", err)
	}
	second, _, err := PreAdmit(ctx, cfg,
		MapMetadata{AuthorizationMetadataKey: {"Bearer two"}}, "/method")
	if err != nil {
		t.Fatalf("PreAdmit: %v", err)
	}
	if second != verifier.principals["two"] || first == second {
		t.Fatalf("fingerprint mismatch reused principal: first=%v second=%v", first, second)
	}
	if got := verifier.calls.Load(); got != 2 {
		t.Fatalf("Verify calls = %d, want 2", got)
	}
}

func TestPreAdmissionMethodMismatch(t *testing.T) {
	principal := newPreAdmissionTestPrincipal(t, "user-one")
	verifier := &preAdmissionTestVerifier{principals: map[string]*trust.Principal{"one": principal}}
	cfg := preAdmissionTestConfig(verifier)
	ctx, _, _, err := WithPreAdmission(context.Background(), cfg,
		MapMetadata{AuthorizationMetadataKey: {"Bearer one"}}, "/method-a")
	if err != nil {
		t.Fatalf("WithPreAdmission: %v", err)
	}
	got, _, err := PreAdmit(ctx, cfg,
		MapMetadata{AuthorizationMetadataKey: {"Bearer one"}}, "/method-b")
	if err != nil {
		t.Fatalf("PreAdmit: %v", err)
	}
	if got != principal {
		t.Fatal("method mismatch did not authenticate through the verifier")
	}
	if calls := verifier.calls.Load(); calls != 2 {
		t.Fatalf("Verify calls = %d, want 2", calls)
	}
}

func TestPreAdmissionAbsentCarrier(t *testing.T) {
	principal := newPreAdmissionTestPrincipal(t, "user-one")
	verifier := &preAdmissionTestVerifier{principals: map[string]*trust.Principal{"one": principal}}
	cfg := preAdmissionTestConfig(verifier)
	got, requestID, err := PreAdmit(context.Background(), cfg,
		MapMetadata{AuthorizationMetadataKey: {"Bearer one"}}, "/method")
	if err != nil {
		t.Fatalf("PreAdmit: %v", err)
	}
	if got != principal || requestID != "req-preadmitted" {
		t.Fatalf("got principal=%v requestID=%q", got, requestID)
	}
	if calls := verifier.calls.Load(); calls != 1 {
		t.Fatalf("Verify calls = %d, want 1", calls)
	}
}
