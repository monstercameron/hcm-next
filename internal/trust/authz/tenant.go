package authz

import (
	"fmt"
	"slices"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// mandatoryDenyCrossTenantSensitive is the rule ID for the non-delegable
// restriction attached to every cross-tenant grant: sharing a record across
// a tenant boundary never carries compensation, tax, bank, performance,
// medical, employee-relations or immigration data, no matter what role the
// viewing principal holds or what a sharing grant otherwise claims. A parent
// policy this restrictive cannot be weakened by a subsidiary allow.
const mandatoryDenyCrossTenantSensitive = "p1a.mandatory.cross_tenant_sensitive_domains"

// OrgUnitRef identifies one organization unit within a tenant. The zero
// value is the "no organization scoping declared" sentinel: a single-company
// customer that never assigns organization units resolves through the same
// [ResolveTenantScope] as a deep hierarchy, just with an always-empty graph.
type OrgUnitRef struct {
	Tenant values.TenantId
	ID     string
}

// IsZero reports whether o is the "no organization scoping" sentinel.
func (o OrgUnitRef) IsZero() bool { return o == OrgUnitRef{} }

// OrgEdge is one effective-dated part_of edge: Child is part_of Parent. Only
// part_of edges participate in scope closure; the wider organization
// relationship vocabulary belongs to the Organization domain, not to this
// package.
type OrgEdge struct {
	Child     OrgUnitRef
	Parent    OrgUnitRef
	Effective values.EffectiveInterval
}

func (e OrgEdge) active(at values.Instant) bool {
	if e.Child.IsZero() || e.Parent.IsZero() {
		return false
	}
	ok, err := e.Effective.ContainsInstant(at)
	return err == nil && ok
}

// SharingDirection names how a cross-tenant sharing grant permits access.
// SharingDirectionUnspecified is the zero value and, like any value this
// package does not recognize, never satisfies a record-level access check:
// an unrecognized direction fails closed rather than being interpreted
// permissively.
type SharingDirection uint8

const (
	SharingDirectionUnspecified SharingDirection = iota
	// SharingDirectionRecordVisible grants record-level visibility of the
	// owner tenant's resource to the viewer tenant. It is the only direction
	// [ResolveTenantScope] treats as a valid record-level grant in P1A;
	// aggregate-only or execution-scope sharing is deferred.
	SharingDirectionRecordVisible
)

// Valid reports whether d is a direction [ResolveTenantScope] recognizes as
// a record-level grant.
func (d SharingDirection) Valid() bool { return d == SharingDirectionRecordVisible }

// String returns the stable wire token, or "SHARING_DIRECTION_INVALID" for
// any value this package does not recognize, including the zero value.
func (d SharingDirection) String() string {
	if d == SharingDirectionRecordVisible {
		return "RECORD_VISIBLE"
	}
	return "SHARING_DIRECTION_INVALID"
}

// SharingGrant declares that OwnerTenant's resources are visible to
// ViewerTenant, in Direction, for the duration of Effective.
type SharingGrant struct {
	OwnerTenant  values.TenantId
	ViewerTenant values.TenantId
	Direction    SharingDirection
	Effective    values.EffectiveInterval
}

func (g SharingGrant) matches(owner, viewer values.TenantId) bool {
	return g.OwnerTenant == owner && g.ViewerTenant == viewer
}

func (g SharingGrant) active(at values.Instant) bool {
	ok, err := g.Effective.ContainsInstant(at)
	return err == nil && ok
}

// TenantScopeInput is the caller-supplied projection [ResolveTenantScope]
// evaluates. Every field is an already-resolved fact (an organization edge,
// a sharing grant): this package walks them, it does not fetch them.
type TenantScopeInput struct {
	// ResourceTenant is the tenant that owns the resource being accessed.
	ResourceTenant values.TenantId
	// PrincipalOrg is the organization unit the principal's authority is
	// scoped to. The zero value means the principal is not organization-
	// scoped: a single-company default, not a deny.
	PrincipalOrg OrgUnitRef
	// ResourceOrg is the organization unit the resource belongs to. The zero
	// value means the resource carries no organization tag.
	ResourceOrg OrgUnitRef
	// Edges is the relevant slice of the organization part_of graph.
	Edges []OrgEdge
	// Sharing is the set of cross-tenant sharing grants that might cover
	// this resource tenant.
	Sharing []SharingGrant
	// EffectiveAt is the instant the scope is evaluated at.
	EffectiveAt values.Instant
}

// TenantScopeDecision is the TRUST-008 result: whether the tenant boundary
// and organization scope permit reaching the resource at all, and under what
// mandatory restrictions.
type TenantScopeDecision struct {
	Effect Effect
	// RuleID is the single rule that decided the outcome.
	RuleID string
	// Reason is the policy reason token. It is safe to log and safe to
	// return to the caller: it never names the resource or discloses
	// whether a specific record exists, only which boundary rule fired.
	Reason string
	// PrincipalTenant and ResourceTenant record the tenants compared.
	PrincipalTenant values.TenantId
	ResourceTenant  values.TenantId
	// AllowedOrganizations is the resolved closure (the scoped organization
	// and its descendants) when the principal is organization-scoped. It is
	// nil for a tenant-wide default and for a denied decision.
	AllowedOrganizations []OrgUnitRef
	// MandatoryDenies lists non-delegable restriction rule IDs that remain
	// in force even though the decision is otherwise EffectAllow. A
	// downstream field or record grant can never override one of these.
	MandatoryDenies []string
	// SharingPath is the cross-tenant grant(s) that justified access. It is
	// empty for a same-tenant decision.
	SharingPath []SharingGrant
	// PolicyVersion pins the policy table this decision was evaluated
	// against.
	PolicyVersion string
}

func denyTenant(principal, resource values.TenantId, ruleID, reason string) TenantScopeDecision {
	return TenantScopeDecision{
		Effect:          EffectDenied,
		RuleID:          ruleID,
		Reason:          reason,
		PrincipalTenant: principal,
		ResourceTenant:  resource,
		PolicyVersion:   PolicyVersion,
	}
}

// ResolveTenantScope implements TRUST-008: it resolves whether principal may
// reach a resource in req.ResourceTenant at all, before any record,
// relationship or field question is asked. A cross-tenant resource, an
// organization outside the principal's scope, and a sharing grant with an
// unrecognized direction all deny without revealing which of those applied
// beyond the returned reason token, and the deny never depends on whether
// the specific resource exists.
func ResolveTenantScope(principal *trust.Principal, req TenantScopeInput) (TenantScopeDecision, error) {
	if principal == nil {
		return TenantScopeDecision{}, fmt.Errorf("%w: nil principal", ErrInvalidPolicyInput)
	}
	if err := req.ResourceTenant.Validate(); err != nil {
		return TenantScopeDecision{}, fmt.Errorf("%w: resource tenant: %v", ErrInvalidPolicyInput, err)
	}

	principalTenant := principal.Tenant()

	if principalTenant == req.ResourceTenant {
		return resolveSameTenantScope(principalTenant, req), nil
	}
	return resolveCrossTenantScope(principalTenant, req), nil
}

func resolveSameTenantScope(tenant values.TenantId, req TenantScopeInput) TenantScopeDecision {
	if req.PrincipalOrg.IsZero() {
		// Single-company default: no organization scoping was declared, so
		// the tenant boundary is the whole of the authority. This is the
		// same resolver a deep hierarchy uses, just with an empty graph.
		return TenantScopeDecision{
			Effect:          EffectAllow,
			RuleID:          "p1a.tenant.same_tenant_default",
			Reason:          "tenant_wide_default",
			PrincipalTenant: tenant,
			ResourceTenant:  tenant,
			PolicyVersion:   PolicyVersion,
		}
	}

	if req.ResourceOrg.IsZero() {
		// The principal is organization-scoped but the resource carries no
		// organization tag to test membership against: fail closed rather
		// than guess.
		return denyTenant(tenant, tenant, "p1a.tenant.resource_org_unresolved", "resource_organization_unresolved")
	}

	closure := resolveOrgClosure(req.PrincipalOrg, req.Edges, req.EffectiveAt)
	if !slices.Contains(closure, req.ResourceOrg) {
		return denyTenant(tenant, tenant, "p1a.tenant.org_out_of_scope", "organization_out_of_scope")
	}

	return TenantScopeDecision{
		Effect:               EffectAllow,
		RuleID:               "p1a.tenant.org_in_scope",
		Reason:               "organization_in_scope",
		PrincipalTenant:      tenant,
		ResourceTenant:       tenant,
		AllowedOrganizations: closure,
		PolicyVersion:        PolicyVersion,
	}
}

func resolveCrossTenantScope(principalTenant values.TenantId, req TenantScopeInput) TenantScopeDecision {
	sawInvalidDirection := false

	for _, g := range req.Sharing {
		if !g.matches(req.ResourceTenant, principalTenant) {
			continue
		}
		if !g.active(req.EffectiveAt) {
			continue
		}
		if !g.Direction.Valid() {
			sawInvalidDirection = true
			continue
		}
		// A valid, active, matching grant still carries the non-delegable
		// cross-tenant restriction: it can widen reachability, it cannot
		// widen which data domains are visible once reached.
		return TenantScopeDecision{
			Effect:          EffectAllow,
			RuleID:          "p1a.tenant.cross_tenant_shared",
			Reason:          "cross_tenant_shared",
			PrincipalTenant: principalTenant,
			ResourceTenant:  req.ResourceTenant,
			MandatoryDenies: []string{mandatoryDenyCrossTenantSensitive},
			SharingPath:     []SharingGrant{g},
			PolicyVersion:   PolicyVersion,
		}
	}

	if sawInvalidDirection {
		return denyTenant(principalTenant, req.ResourceTenant, "p1a.tenant.invalid_sharing_direction", "invalid_sharing_direction")
	}
	return denyTenant(principalTenant, req.ResourceTenant, "p1a.tenant.cross_tenant_denied", "cross_tenant_denied")
}

// resolveOrgClosure returns root and every descendant reachable through
// edges active at "at": the organization-plus-descendants scope expression
// from the organization vocabulary. Traversal tracks visited nodes, so a
// matrix graph with multiple parents is handled and a cyclic graph (which is
// someone else's validation to reject, not this package's) can never loop
// forever; it just closes over whatever it reaches once.
func resolveOrgClosure(root OrgUnitRef, edges []OrgEdge, at values.Instant) []OrgUnitRef {
	children := make(map[OrgUnitRef][]OrgUnitRef)
	for _, e := range edges {
		if !e.active(at) {
			continue
		}
		children[e.Parent] = append(children[e.Parent], e.Child)
	}

	visited := map[OrgUnitRef]struct{}{root: {}}
	closure := []OrgUnitRef{root}
	queue := []OrgUnitRef{root}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, child := range children[node] {
			if _, seen := visited[child]; seen {
				continue
			}
			visited[child] = struct{}{}
			closure = append(closure, child)
			queue = append(queue, child)
		}
	}
	return closure
}
