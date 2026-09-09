package crm

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	identity "github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// IdentityLinkOwner is the authority boundary for identity links. CRM never
// accepts a caller-supplied link slice as truth; the owner loads links scoped
// to the tenant and purpose requested by the conversion.
type IdentityLinkOwner interface {
	LoadIdentityLinks(context.Context, values.TenantId, string, string, string) ([]identity.IdentityLink, error)
}

type CanonicalPersonBinding struct {
	Person             values.EntityRef
	LinkRef            string
	EvidenceRef        string
	SourceAuthorityRef string
	Tenant             values.TenantId
	Purpose            string
}

func ResolveConversionIdentity(ctx context.Context, owner IdentityLinkOwner, tenant values.TenantId, purpose, externalSystem, externalID string, at values.Instant) (CanonicalPersonBinding, error) {
	if tenant.Validate() != nil || purpose == "" || externalSystem == "" || externalID == "" || at.Validate() != nil {
		return CanonicalPersonBinding{}, fmt.Errorf("%w: invalid identity resolution request", ErrCRM005Rejected)
	}
	if owner == nil || isNilIdentityLinkOwner(owner) {
		return CanonicalPersonBinding{}, fmt.Errorf("%w: identity owner is required", ErrCRM005Rejected)
	}
	links, err := owner.LoadIdentityLinks(ctx, tenant, purpose, externalSystem, externalID)
	if err != nil {
		// The owner may include a protected external identifier or a storage
		// locator in its error. Neither is part of CRM's caller-facing contract.
		return CanonicalPersonBinding{}, fmt.Errorf("%w: identity owner failed", ErrCRM005Rejected)
	}
	links = append([]identity.IdentityLink(nil), links...)
	linkRefs := make(map[string]struct{}, len(links))
	for _, link := range links {
		if err := link.Validate(); err != nil {
			return CanonicalPersonBinding{}, fmt.Errorf("%w: invalid identity-owner result", ErrCRM005Rejected)
		}
		if _, duplicate := linkRefs[link.LinkRef]; duplicate {
			return CanonicalPersonBinding{}, fmt.Errorf("%w: identity owner returned duplicate link references", ErrCRM005Rejected)
		}
		linkRefs[link.LinkRef] = struct{}{}
	}
	resolved, err := identity.ResolveIdentity(links, externalSystem, externalID, at, string(tenant), purpose)
	if err != nil {
		return CanonicalPersonBinding{}, redactIdentityResolutionError(err)
	}

	var person values.EntityRef
	if err := person.UnmarshalText([]byte(resolved.CanonicalRef)); err != nil || person.Kind != values.Kind("person") || person.Tenant != tenant {
		return CanonicalPersonBinding{}, fmt.Errorf("%w: identity owner returned a non-canonical person", ErrCRM005Rejected)
	}
	for _, link := range links {
		if link.LinkRef == resolved.LinkRef && link.CanonicalRef == resolved.CanonicalRef && link.EvidenceRef == resolved.EvidenceRef {
			return CanonicalPersonBinding{
				Person:             person,
				LinkRef:            resolved.LinkRef,
				EvidenceRef:        resolved.EvidenceRef,
				SourceAuthorityRef: link.SourceAuthorityRef,
				Tenant:             tenant,
				Purpose:            purpose,
			}, nil
		}
	}
	return CanonicalPersonBinding{}, fmt.Errorf("%w: identity owner returned an inconsistent resolution", ErrCRM005Rejected)
}

func isNilIdentityLinkOwner(owner IdentityLinkOwner) bool {
	v := reflect.ValueOf(owner)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

func redactIdentityResolutionError(err error) error {
	for _, classified := range []error{
		identity.ErrIdentityAmbiguous,
		identity.ErrIdentityCrossTenantPolicy,
		identity.ErrIdentityOutOfEffectiveRange,
		identity.ErrUnknownIdentityLink,
		identity.ErrInvalidIdentityLink,
	} {
		if errors.Is(err, classified) {
			return fmt.Errorf("%w: identity resolution rejected: %w", ErrCRM005Rejected, classified)
		}
	}
	return fmt.Errorf("%w: identity resolution rejected", ErrCRM005Rejected)
}
