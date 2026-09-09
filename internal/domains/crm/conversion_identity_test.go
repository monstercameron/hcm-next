package crm

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	identity "github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type identityOwnerFixture struct {
	links  []identity.IdentityLink
	err    error
	got    identityOwnerRequest
	called bool
}

type identityOwnerRequest struct {
	tenant                         values.TenantId
	purpose, externalSystem, rawID string
}

func (o *identityOwnerFixture) LoadIdentityLinks(_ context.Context, tenant values.TenantId, purpose, system, id string) ([]identity.IdentityLink, error) {
	o.called = true
	o.got = identityOwnerRequest{tenant: tenant, purpose: purpose, externalSystem: system, rawID: id}
	return o.links, o.err
}

func TestCRM005IdentityResolutionRejectsDuplicateLinkRefProvenance(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	person := crm005Person(tenant, "11111111-1111-4111-8111-111111111111")
	active := crm005Link(t, tenant, person)
	expired := active
	expired.SourceAuthorityRef = "authority-expired"
	var err error
	expired.Effective, err = values.NewInstantInterval(crm005At(2023), crm005At(2024))
	if err != nil {
		t.Fatal(err)
	}
	owner := &identityOwnerFixture{links: []identity.IdentityLink{expired, active}}

	_, err = ResolveConversionIdentity(context.Background(), owner, tenant, "candidate_conversion", "ats", active.ExternalID, crm005At(2026))
	if !errors.Is(err, ErrCRM005Rejected) || strings.Contains(err.Error(), expired.SourceAuthorityRef) {
		t.Fatalf("duplicate link reference was not safely rejected: %v", err)
	}
}

func TestCRM005InvalidResolutionRequestDoesNotCallOwner(t *testing.T) {
	validAt := crm005At(2026)
	tests := []struct {
		name, tenant, purpose, system, id string
		at                                values.Instant
	}{
		{name: "invalid tenant", tenant: "Tenant A", purpose: "candidate_conversion", system: "ats", id: "external", at: validAt},
		{name: "missing purpose", tenant: "tenant-a", system: "ats", id: "external", at: validAt},
		{name: "missing system", tenant: "tenant-a", purpose: "candidate_conversion", id: "external", at: validAt},
		{name: "missing external id", tenant: "tenant-a", purpose: "candidate_conversion", system: "ats", at: validAt},
		{name: "missing instant", tenant: "tenant-a", purpose: "candidate_conversion", system: "ats", id: "external"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner := &identityOwnerFixture{}
			_, err := ResolveConversionIdentity(context.Background(), owner, values.TenantId(tc.tenant), tc.purpose, tc.system, tc.id, tc.at)
			if !errors.Is(err, ErrCRM005Rejected) || owner.called {
				t.Fatalf("error=%v owner_called=%v", err, owner.called)
			}
		})
	}
}

func crm005At(year int) values.Instant {
	return values.NewInstant(time.Date(year, time.June, 1, 0, 0, 0, 0, time.UTC))
}

func crm005Person(tenant values.TenantId, id string) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: values.Kind("person"), Id: id}
}

func crm005Link(t *testing.T, tenant values.TenantId, person values.EntityRef) identity.IdentityLink {
	t.Helper()
	effective, err := values.NewInstantInterval(crm005At(2025), crm005At(2027))
	if err != nil {
		t.Fatal(err)
	}
	return identity.IdentityLink{
		LinkRef: "identity-link-7", ExternalSystem: "ats", ExternalID: "protected-applicant-481",
		CanonicalRef: person.String(), MatchKey: "ATS_PERSON_ID", Confidence: 1, Effective: effective,
		TenantRef: string(tenant), PurposeScope: "candidate_conversion", EvidenceRef: "evidence-7",
		SourceAuthorityRef: "authority-hris",
	}
}

func TestCRM005IdentityResolutionUsesMODEL022AndBindsCanonicalPerson(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	person := crm005Person(tenant, "11111111-1111-4111-8111-111111111111")
	link := crm005Link(t, tenant, person)
	owner := &identityOwnerFixture{links: []identity.IdentityLink{link}}

	got, err := ResolveConversionIdentity(context.Background(), owner, tenant, "candidate_conversion", "ats", link.ExternalID, crm005At(2026))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := CanonicalPersonBinding{Person: person, LinkRef: link.LinkRef, EvidenceRef: link.EvidenceRef, SourceAuthorityRef: link.SourceAuthorityRef, Tenant: tenant, Purpose: link.PurposeScope}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("binding = %+v, want %+v", got, want)
	}
	if owner.got != (identityOwnerRequest{tenant: tenant, purpose: link.PurposeScope, externalSystem: link.ExternalSystem, rawID: link.ExternalID}) {
		t.Fatalf("owner request = %+v", owner.got)
	}

	owner.links[0].CanonicalRef = crm005Person(tenant, "22222222-2222-4222-8222-222222222222").String()
	owner.links[0].SourceAuthorityRef = "malicious-rewrite"
	if got.Person != person || got.SourceAuthorityRef != "authority-hris" {
		t.Fatalf("owner mutation changed binding: %+v", got)
	}
}

func TestCRM005IdentityResolutionRejectsMODEL022AmbiguityScopeAndExpiry(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	base := crm005Link(t, tenant, crm005Person(tenant, "11111111-1111-4111-8111-111111111111"))
	other := base
	other.LinkRef = "identity-link-8"
	other.CanonicalRef = crm005Person(tenant, "22222222-2222-4222-8222-222222222222").String()
	tests := []struct {
		name    string
		links   []identity.IdentityLink
		at      values.Instant
		wantErr error
	}{
		{name: "two canonical targets are ambiguous", links: []identity.IdentityLink{base, other}, at: crm005At(2026), wantErr: identity.ErrIdentityAmbiguous},
		{name: "wrong tenant", links: func() []identity.IdentityLink { l := base; l.TenantRef = "tenant-b"; return []identity.IdentityLink{l} }(), at: crm005At(2026), wantErr: identity.ErrIdentityCrossTenantPolicy},
		{name: "wrong purpose", links: func() []identity.IdentityLink {
			l := base
			l.PurposeScope = "payroll"
			return []identity.IdentityLink{l}
		}(), at: crm005At(2026), wantErr: identity.ErrIdentityCrossTenantPolicy},
		{name: "expired link", links: []identity.IdentityLink{base}, at: crm005At(2028), wantErr: identity.ErrIdentityOutOfEffectiveRange},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner := &identityOwnerFixture{links: tc.links}
			_, err := ResolveConversionIdentity(context.Background(), owner, tenant, "candidate_conversion", "ats", base.ExternalID, tc.at)
			if !errors.Is(err, ErrCRM005Rejected) || !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want CRM rejection and %v", err, tc.wantErr)
			}
			for _, protected := range []string{base.ExternalID, base.CanonicalRef, other.CanonicalRef} {
				if strings.Contains(err.Error(), protected) {
					t.Fatalf("error disclosed protected identity %q: %v", protected, err)
				}
			}
		})
	}
}

func TestCRM005IdentityResolutionRejectsNonPersonOrForeignCanonicalRef(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	refs := []values.EntityRef{
		{Tenant: tenant, Kind: values.Kind("candidate"), Id: "11111111-1111-4111-8111-111111111111"},
		crm005Person("tenant-b", "11111111-1111-4111-8111-111111111111"),
	}
	for _, ref := range refs {
		owner := &identityOwnerFixture{links: []identity.IdentityLink{crm005Link(t, tenant, ref)}}
		_, err := ResolveConversionIdentity(context.Background(), owner, tenant, "candidate_conversion", "ats", owner.links[0].ExternalID, crm005At(2026))
		if !errors.Is(err, ErrCRM005Rejected) || strings.Contains(err.Error(), ref.String()) {
			t.Fatalf("unsafe canonical ref error = %v", err)
		}
	}
}

func TestCRM005IdentityResolutionRejectsMissingTypedNilAndFailingOwnerWithoutDisclosure(t *testing.T) {
	var typedNil *identityOwnerFixture
	for _, owner := range []IdentityLinkOwner{nil, typedNil} {
		_, err := ResolveConversionIdentity(context.Background(), owner, "tenant-a", "candidate_conversion", "ats", "protected-applicant-481", crm005At(2026))
		if !errors.Is(err, ErrCRM005Rejected) {
			t.Fatalf("nil owner error = %v", err)
		}
	}
	const secret = "protected-applicant-481 at postgres://identity-secret"
	owner := &identityOwnerFixture{err: errors.New(secret)}
	_, err := ResolveConversionIdentity(context.Background(), owner, "tenant-a", "candidate_conversion", "ats", "protected-applicant-481", crm005At(2026))
	if !errors.Is(err, ErrCRM005Rejected) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "protected-applicant-481") {
		t.Fatalf("owner failure was not safely redacted: %v", err)
	}
}
