package trust

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func principalSpecForTest() PrincipalSpec {
	return PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "subject-1", SubjectKind: SubjectKindAgent,
		OrganizationScopeID: "org-a", Roles: []string{"z-role", "", "a-role", "a-role"},
		AuthorityRefs: []string{"grant-2", "grant-1"}, Purposes: []string{"z-purpose", "a-purpose"},
		AuthenticationMethod: AuthenticationMethodMutualTLS, Assurance: AssuranceSubstantial,
		SessionRef: "session-1", DelegationRefs: []string{"d-2", "d-1"},
		IssuedAt:         time.Date(2026, 9, 6, 12, 0, 0, 0, time.FixedZone("test", -5*60*60)),
		ExpiresAt:        time.Date(2026, 9, 6, 13, 0, 0, 0, time.FixedZone("test", -5*60*60)),
		CredentialDigest: "cred-digest",
	}
}

func TestNewPrincipal_RejectsMalformedTrustedFieldsWithSentinels(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PrincipalSpec)
		want   error
	}{
		{"tenant", func(s *PrincipalSpec) { s.Tenant = "bad tenant" }, ErrPrincipalTenant},
		{"subject", func(s *PrincipalSpec) { s.Subject = "bad\nsubject" }, ErrPrincipalSubject},
		{"subject kind", func(s *PrincipalSpec) { s.SubjectKind = SubjectKindUnspecified }, ErrPrincipalSubjectKind},
		{"authn method", func(s *PrincipalSpec) { s.AuthenticationMethod = AuthenticationMethodUnspecified }, ErrPrincipalAuthnMethod},
		{"assurance", func(s *PrincipalSpec) { s.Assurance = AssuranceUnspecified }, ErrPrincipalAssurance},
		{"assurance above maximum", func(s *PrincipalSpec) { s.Assurance = AssuranceHigh + 1 }, ErrPrincipalAssurance},
		{"session", func(s *PrincipalSpec) { s.SessionRef = " " }, ErrPrincipalSession},
		{"validity", func(s *PrincipalSpec) { s.ExpiresAt = s.IssuedAt }, ErrPrincipalValidity},
		{"credential binding", func(s *PrincipalSpec) { s.CredentialDigest = "" }, ErrPrincipalCredentialBound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := principalSpecForTest()
			tc.mutate(&spec)
			if p, err := NewPrincipal(spec); p != nil || !errors.Is(err, tc.want) {
				t.Fatalf("NewPrincipal = (%v, %v), want nil/%v", p, err, tc.want)
			}
		})
	}
}

func TestPrincipal_AccessorsNormalizeCopyAndRedact(t *testing.T) {
	spec := principalSpecForTest()
	p, err := NewPrincipal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Roles(); !reflect.DeepEqual(got, []string{"a-role", "z-role"}) || !p.HasRole("a-role") || p.HasRole("missing") {
		t.Fatalf("roles = %v", got)
	}
	if got := p.AuthorityRefs(); !reflect.DeepEqual(got, []string{"grant-1", "grant-2"}) {
		t.Fatalf("authority refs = %v", got)
	}
	if got := p.Purposes(); !reflect.DeepEqual(got, []string{"a-purpose", "z-purpose"}) || p.DefaultPurpose() != "a-purpose" || !p.AuthorizesPurpose("z-purpose") || p.AuthorizesPurpose("other") {
		t.Fatalf("purposes = %v", got)
	}
	if got := p.DelegationRefs(); !reflect.DeepEqual(got, []string{"d-1", "d-2"}) {
		t.Fatalf("delegation refs = %v", got)
	}
	roles := p.Roles()
	roles[0] = "tampered"
	if p.Roles()[0] == "tampered" {
		t.Fatal("Roles exposed internal state")
	}
	if p.Tenant() != "tenant-a" || p.Subject() != "subject-1" || p.SubjectKind() != SubjectKindAgent || p.OrganizationScopeID() != "org-a" || p.AuthenticationMethod() != AuthenticationMethodMutualTLS || p.Assurance() != AssuranceSubstantial || p.SessionRef() != "session-1" || p.CredentialDigest() != "cred-digest" {
		t.Fatalf("basic accessors returned unexpected values: %s", p)
	}
	if !p.IssuedAt().Equal(spec.IssuedAt.UTC()) || !p.ExpiresAt().Equal(spec.ExpiresAt.UTC()) {
		t.Fatalf("times were not normalized to UTC: %v / %v", p.IssuedAt(), p.ExpiresAt())
	}
	if p.Fingerprint() == "" || !strings.HasPrefix(p.EvidenceID(), "ev:authn:") || len(p.Fingerprint()) != 64 {
		t.Fatalf("derived identifiers invalid: fingerprint=%q evidence=%q", p.Fingerprint(), p.EvidenceID())
	}
	redacted := p.String()
	if !strings.Contains(redacted, "tenant=tenant-a") || strings.Contains(redacted, p.CredentialDigest()) || strings.Contains(redacted, "a-role") {
		t.Fatalf("String leaked sensitive data: %q", redacted)
	}
}

func TestPrincipal_EmptyPurposesAndEnumBoundariesFailClosed(t *testing.T) {
	spec := principalSpecForTest()
	spec.Purposes = nil
	p, err := NewPrincipal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultPurpose() != "" || p.AuthorizesPurpose("anything") {
		t.Fatal("empty purposes unexpectedly authorized a purpose")
	}
	for _, tc := range []struct {
		got, want string
	}{
		{SubjectKindUnspecified.String(), "unspecified"},
		{SubjectKindHuman.String(), "human"},
		{SubjectKindService.String(), "service"},
		{SubjectKindAgent.String(), "agent"},
		{SubjectKindIntegration.String(), "integration"},
		{SubjectKind(99).String(), "invalid(99)"},
		{AuthenticationMethodUnspecified.String(), "unspecified"},
		{AuthenticationMethodBearerToken.String(), "bearer_token"},
		{AuthenticationMethodMutualTLS.String(), "mutual_tls"},
		{AuthenticationMethod(99).String(), "invalid(99)"},
		{AssuranceUnspecified.String(), "unspecified"},
		{AssuranceLow.String(), "low"},
		{AssuranceSubstantial.String(), "substantial"},
		{AssuranceHigh.String(), "high"},
		{Assurance(99).String(), "invalid(99)"},
	} {
		if tc.got != tc.want {
			t.Errorf("String() = %q, want %q", tc.got, tc.want)
		}
	}
	for _, tc := range []struct {
		actual, required Assurance
		want             bool
	}{{AssuranceHigh, AssuranceHigh, true}, {AssuranceHigh, AssuranceLow, true}, {AssuranceLow, AssuranceHigh, false}, {AssuranceHigh, AssuranceUnspecified, false}, {AssuranceUnspecified, AssuranceLow, false}} {
		if got := tc.actual.AtLeast(tc.required); got != tc.want {
			t.Errorf("%v.AtLeast(%v) = %v, want %v", tc.actual, tc.required, got, tc.want)
		}
	}
}
