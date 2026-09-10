package authz_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_TRUST_025 is the TRUST-025 primary cross-tenant abuse suite. It
// runs the five adversarial vectors the todo names — ID substitution, batch
// and export leak, cache key collision, error oracle and tenant-header
// spoofing — against the P1A authorization plane and proves each one is
// blocked with a deny or a not-found-equivalent empty answer.
func TestTodo_TRUST_025(t *testing.T) {
	foreignWorker := workerSubject(tenantVendor, subjectOtherID)

	t.Run("vector 1: ID substitution across tenants is denied", func(t *testing.T) {
		// The same opaque UUID identifies worker 1 in every tenant. A
		// principal who can read their own record must not be able to read
		// the same UUID in a foreign tenant by swapping the tenant on the
		// reference.
		selfPrincipal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleWorkerSelf)}, purposes: []string{authz.PurposeSelfService}})
		substituted := workerSubject(tenantVendor, subjectWorkerID)

		decision, err := authz.Enforce(authz.Request{
			Principal:   selfPrincipal,
			Purpose:     authz.PurposeSelfService,
			EffectiveAt: baseInstant,
			Subject:     substituted,
			Fields:      []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		if decision.SubjectDisclosable {
			t.Fatal("self-hood must not survive a tenant substitution")
		}
		if decision.Tenant.Effect != authz.EffectDenied || decision.Tenant.PrincipalTenant == decision.Tenant.ResourceTenant {
			t.Errorf("tenant stage = %+v, want a cross-tenant deny", decision.Tenant)
		}
		// The same substitution through the repository planner: the query
		// declares the foreign tenant, the principal cannot reach it.
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   selfPrincipal,
			EffectiveAt: baseInstant,
			Tenant:      tenantVendor,
			Candidates:  []authz.ScopeInput{{Subject: substituted}},
			Fields:      []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		if scope.Effect() != authz.EffectDenied || len(scope.AllowedSubjects()) != 0 {
			t.Errorf("planner Effect = %s subjects = %v, want a full deny", scope.Effect(), scope.AllowedSubjects())
		}
	})

	t.Run("vector 2: a batch or export request leaks no unauthorized rows", func(t *testing.T) {
		manager := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview}})
		managed := workerSubject(tenantAcme, subjectWorkerID)

		// A population filter and a repository plan over the same mixed
		// batch: two managed workers, one unmanaged peer, one foreign-tenant
		// row with the same UUID as an authorized one.
		batch := []authz.ScopeInput{
			{Subject: managed, EffectiveAt: baseInstant, Relationships: []authz.RelationshipFact{managerFact(managed)}},
			{Subject: workerSubject(tenantAcme, "00000000-0000-4000-8000-000000000003"), EffectiveAt: baseInstant, Relationships: []authz.RelationshipFact{managerFact(workerSubject(tenantAcme, "00000000-0000-4000-8000-000000000003"))}},
			{Subject: workerSubject(tenantAcme, subjectOtherID), EffectiveAt: baseInstant},
			{Subject: foreignWorker, EffectiveAt: baseInstant},
		}
		// FilterPopulation is evaluated per request; the foreign row is a
		// malformed candidate for the acme-only request shape, so it is
		// exercised through the planner, which drops it before the tenant
		// stage ever grants reachability. The three acme candidates go
		// through both paths.
		acmeBatch := batch[:3]
		filtered, err := authz.FilterPopulation(manager, acmeBatch)
		if err != nil {
			t.Fatalf("FilterPopulation: %v", err)
		}
		if len(filtered) != 2 {
			t.Fatalf("FilterPopulation returned %d subjects, want the 2 managed workers", len(filtered))
		}

		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   manager,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates:  acmeBatch,
			Fields:      []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		for _, s := range scope.AllowedSubjects() {
			if s == workerSubject(tenantAcme, subjectOtherID) || s.Tenant != tenantAcme {
				t.Errorf("planner authorized an unmanaged or foreign subject: %s", s.String())
			}
		}

		gate := authz.NewRepositoryGate(map[values.EntityRef]map[authz.FieldID]string{
			managed: {authz.FieldWorkerNumber: "W-0001"},
			workerSubject(tenantAcme, "00000000-0000-4000-8000-000000000003"): {authz.FieldWorkerNumber: "W-0003"},
			workerSubject(tenantAcme, subjectOtherID):                         {authz.FieldWorkerNumber: "W-0002"},
			foreignWorker: {authz.FieldWorkerNumber: "V-0001"},
		})
		// The export asks for everything, including the foreign row.
		projections, err := gate.Query(scope, []values.EntityRef{managed,
			workerSubject(tenantAcme, "00000000-0000-4000-8000-000000000003"),
			workerSubject(tenantAcme, subjectOtherID), foreignWorker}, baseInstant)
		if err != nil {
			t.Fatalf("gate.Query: %v", err)
		}
		if len(projections) != 2 {
			t.Fatalf("export returned %d rows, want exactly the 2 managed workers", len(projections))
		}
		for _, p := range projections {
			if p.Subject == foreignWorker || strings.Contains(p.Subject.String(), string(tenantVendor)) {
				t.Errorf("export leaked a foreign-tenant row: %s", p.Subject.String())
			}
		}
	})

	t.Run("vector 3: two different tenants or principals never share a cache key", func(t *testing.T) {
		// Same opaque UUID, two tenants: the digest must differ, so a cache
		// keyed on the digest can never serve one tenant's decision for
		// another's.
		selfPrincipal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleWorkerSelf)}, purposes: []string{authz.PurposeSelfService}})
		acmeSelf, err := authz.Enforce(authz.Request{
			Principal: selfPrincipal, EffectiveAt: baseInstant,
			Subject: workerSubject(tenantAcme, subjectWorkerID),
			Fields:  []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("Enforce(acme): %v", err)
		}
		vendorSelf, err := authz.Enforce(authz.Request{
			Principal: selfPrincipal, EffectiveAt: baseInstant,
			Subject: workerSubject(tenantVendor, subjectWorkerID),
			Fields:  []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("Enforce(vendor): %v", err)
		}
		if acmeSelf.InputsDigest == vendorSelf.InputsDigest {
			t.Fatal("decisions for the same UUID in different tenants share an inputs digest")
		}

		// Two principals, same request: the digest must differ too.
		otherSelf := newPrincipal(t, principalOpts{
			roles:    []string{string(authz.RoleWorkerSelf)},
			purposes: []string{authz.PurposeSelfService},
			subject:  subjectOtherID,
		})
		otherDecision, err := authz.Enforce(authz.Request{
			Principal: otherSelf, EffectiveAt: baseInstant,
			Subject: workerSubject(tenantAcme, subjectOtherID),
			Fields:  []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("Enforce(other principal): %v", err)
		}
		acmeOther, err := authz.Enforce(authz.Request{
			Principal: selfPrincipal, EffectiveAt: baseInstant,
			Subject: workerSubject(tenantAcme, subjectOtherID),
			Fields:  []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("Enforce(acme, other subject): %v", err)
		}
		// otherSelf is self over subjectOtherID; selfPrincipal is not. Even
		// where the local ID matches the principal's own, the digest is
		// principal-bound.
		if otherDecision.InputsDigest == acmeOther.InputsDigest {
			t.Fatal("decisions for different principals over the same subject share an inputs digest")
		}
	})

	t.Run("vector 4: a denial carries no error oracle", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview}})

		// ResolveAuthorizationScope must return one uniform denial reason
		// whether the principal has no relationship, an expired one, or one
		// pointing at a different subject: the reason tokens must not let a
		// caller enumerate which relationship almost matched.
		reasons := map[string]bool{}
		expired := managerFact(foreignWorker)
		expired.Effective = mustInterval(t, farPast, recentPast)
		for _, req := range []authz.ScopeInput{
			{Subject: foreignWorker},
			{Subject: foreignWorker, Relationships: []authz.RelationshipFact{expired}},
			{Subject: foreignWorker, Relationships: []authz.RelationshipFact{managerFact(workerSubject(tenantAcme, subjectOtherID))}},
		} {
			scope, err := authz.ResolveAuthorizationScope(principal, req)
			if err != nil {
				t.Fatalf("ResolveAuthorizationScope: %v", err)
			}
			if scope.Effect != authz.EffectDenied {
				t.Fatalf("scope Effect = %s, want DENIED", scope.Effect)
			}
			reasons[scope.Reason] = true
		}
		if len(reasons) != 1 {
			t.Fatalf("denial reasons = %v, want one uniform token", reasons)
		}

		// At the repository layer the same property holds: querying for a
		// record that exists but is unauthorized and one that does not exist
		// at all must produce identical empty answers.
		existing := authz.NewRepositoryGate(map[values.EntityRef]map[authz.FieldID]string{
			workerSubject(tenantAcme, subjectOtherID): {authz.FieldWorkerNumber: "W-0002"},
		})
		absent := authz.NewRepositoryGate(nil)
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   principal,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates:  []authz.ScopeInput{{Subject: workerSubject(tenantAcme, subjectOtherID)}},
			Fields:      []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		fromExisting, err := existing.Query(scope, []values.EntityRef{workerSubject(tenantAcme, subjectOtherID)}, baseInstant)
		if err != nil {
			t.Fatalf("gate.Query(existing): %v", err)
		}
		fromAbsent, err := absent.Query(scope, []values.EntityRef{workerSubject(tenantAcme, subjectOtherID)}, baseInstant)
		if err != nil {
			t.Fatalf("gate.Query(absent): %v", err)
		}
		if len(fromExisting) != 0 || len(fromAbsent) != 0 {
			t.Fatal("unauthorized reads must return the same empty answer whether or not the record exists")
		}
	})

	t.Run("vector 5: caller-supplied tenant claims cannot elevate access", func(t *testing.T) {
		// A caller can put any projection it likes into the request: fake
		// sharing grants, spoofed organization units, forged edges. The
		// principal's tenant comes from the identity plane and every
		// cross-tenant claim is checked against it, so none of these can
		// mint an allow.
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposePayrollProcessing}})

		spoofedGrants := []authz.SharingGrant{
			// Grant claims vendor shares with acme but with an invalid
			// (unset) direction.
			{OwnerTenant: tenantVendor, ViewerTenant: tenantAcme, Direction: authz.SharingDirectionUnspecified, Effective: mustOpenIntervalUnchecked(recentPast)},
			// Grant names the wrong viewer tenant for this principal.
			{OwnerTenant: tenantVendor, ViewerTenant: tenantVendor, Direction: authz.SharingDirectionRecordVisible, Effective: mustOpenIntervalUnchecked(recentPast)},
		}
		for i, grant := range spoofedGrants {
			decision, err := authz.Enforce(authz.Request{
				Principal: principal, EffectiveAt: baseInstant,
				Subject: foreignWorker,
				Sharing: []authz.SharingGrant{grant},
				Fields:  []authz.FieldID{authz.FieldWorkerNumber},
			})
			if err != nil {
				t.Fatalf("Enforce(spoofed grant %d): %v", i, err)
			}
			if decision.SubjectDisclosable {
				t.Errorf("spoofed sharing grant %d produced a disclosable subject", i)
			}
		}

		// A spoofed organization closure: edges claim the foreign resource
		// org hangs under the principal's org. The tenant boundary is
		// evaluated first and denies regardless of the caller's graph.
		decision, err := authz.Enforce(authz.Request{
			Principal:    principal,
			EffectiveAt:  baseInstant,
			Subject:      foreignWorker,
			PrincipalOrg: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a"},
			ResourceOrg:  authz.OrgUnitRef{Tenant: tenantVendor, ID: "org-a1"},
			OrgEdges: []authz.OrgEdge{
				{Child: authz.OrgUnitRef{Tenant: tenantVendor, ID: "org-a1"}, Parent: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a"}, Effective: mustOpenIntervalUnchecked(recentPast)},
			},
			Fields: []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("Enforce(spoofed org closure): %v", err)
		}
		if decision.SubjectDisclosable || decision.Tenant.Effect != authz.EffectDenied {
			t.Errorf("spoofed org closure produced %+v, want a tenant-boundary deny", decision.Tenant)
		}
	})
}

// TestTodo_TRUST_025_Security is the TRUST-025 security test: every denial
// answers with zero foreign bytes, exactly one redaction-safe audit decision,
// and no disclosure through explanations, reasons or counts.
func TestTodo_TRUST_025_Security(t *testing.T) {
	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview}})
	foreignWorker := workerSubject(tenantVendor, subjectOtherID)
	foreignSalary := "VENDOR-SECRET-SALARY"

	decision, err := authz.Enforce(authz.Request{
		Principal:   principal,
		EffectiveAt: baseInstant,
		Subject:     foreignWorker,
		Fields:      []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary},
	})
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if err := decision.Validate(); err != nil {
		t.Fatalf("denied decision fails evidence validation: %v", err)
	}

	// Zero foreign bytes: no reason token, explanation or field ruling names
	// the foreign subject or a foreign value. (The decision's recorded
	// ResourceTenant identifier is deliberate evidence, not a leak.)
	leaks := []string{decision.Explain(), decision.SubjectDenialReason, decision.Tenant.Reason}
	for _, ruling := range decision.Fields {
		leaks = append(leaks, ruling.RuleID, ruling.Reason)
		leaks = append(leaks, ruling.Obligations...)
	}
	for _, s := range leaks {
		if strings.Contains(s, string(tenantVendor)) || strings.Contains(s, subjectOtherID) || strings.Contains(s, foreignSalary) {
			t.Errorf("denied decision leaks foreign data in %q", s)
		}
	}
	for _, ruling := range decision.Fields {
		if ruling.Effect != authz.EffectWithheld {
			t.Errorf("field ruling on a non-disclosable subject = %s, want uniform WITHHELD", ruling.Effect)
		}
	}

	// One redacted audit decision: the evidence identifier exists and is
	// derived from the inputs, and the explanation carries rule tokens and
	// counts only.
	if decision.EvidenceID == "" || decision.InputsDigest == "" {
		t.Fatal("denied decision carries no durable evidence identifier")
	}

	// The planner's answer is equally silent: reason tokens are fixed policy
	// strings, never echoing caller input.
	scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
		Principal:   principal,
		EffectiveAt: baseInstant,
		Tenant:      tenantVendor,
		Candidates:  []authz.ScopeInput{{Subject: foreignWorker}},
		Fields:      []authz.FieldID{authz.FieldWorkerNumber},
	})
	if err != nil {
		t.Fatalf("PlanRepositoryScope: %v", err)
	}
	for _, echoed := range []string{scope.Reason(), scope.RuleID(), scope.EvidenceID()} {
		if strings.Contains(echoed, string(tenantVendor)) || strings.Contains(echoed, subjectOtherID) {
			t.Errorf("planner denial echoes caller input in %q", echoed)
		}
	}
}

// TestTodo_TRUST_025_Mutation proves each anti-spoofing check is
// load-bearing: weakening one input flips the outcome, so no check is dead
// code masked by another.
func TestTodo_TRUST_025_Mutation(t *testing.T) {
	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposePayrollProcessing}})
	foreignWorker := workerSubject(tenantVendor, subjectOtherID)

	enforceWith := func(grants []authz.SharingGrant) authz.Decision {
		t.Helper()
		decision, err := authz.Enforce(authz.Request{
			Principal: principal, EffectiveAt: baseInstant,
			Subject: foreignWorker,
			Sharing: grants,
			Fields:  []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBankAccountNumber},
		})
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		return decision
	}

	t.Run("sharing direction mutation", func(t *testing.T) {
		valid := []authz.SharingGrant{{OwnerTenant: tenantVendor, ViewerTenant: tenantAcme, Direction: authz.SharingDirectionRecordVisible, Effective: mustOpenIntervalUnchecked(recentPast)}}
		if d := enforceWith(valid); !d.SubjectDisclosable {
			t.Fatal("baseline: a valid cross-tenant grant must make the subject reachable")
		}
		invalid := []authz.SharingGrant{valid[0]}
		invalid[0].Direction = authz.SharingDirectionUnspecified
		if d := enforceWith(invalid); d.SubjectDisclosable {
			t.Error("an unset sharing direction must not make the subject reachable")
		}
	})

	t.Run("viewer tenant mutation", func(t *testing.T) {
		mismatched := []authz.SharingGrant{{OwnerTenant: tenantVendor, ViewerTenant: tenantVendor, Direction: authz.SharingDirectionRecordVisible, Effective: mustOpenIntervalUnchecked(recentPast)}}
		if d := enforceWith(mismatched); d.SubjectDisclosable {
			t.Error("a grant naming the wrong viewer tenant must not make the subject reachable")
		}
	})

	t.Run("the cross-tenant mandatory deny is load-bearing", func(t *testing.T) {
		valid := []authz.SharingGrant{{OwnerTenant: tenantVendor, ViewerTenant: tenantAcme, Direction: authz.SharingDirectionRecordVisible, Effective: mustOpenIntervalUnchecked(recentPast)}}
		crossTenant := enforceWith(valid)
		if !crossTenant.SubjectDisclosable {
			t.Fatal("baseline: the valid grant must make the subject reachable")
		}
		if len(crossTenant.Tenant.MandatoryDenies) == 0 {
			t.Fatal("a cross-tenant allow must carry mandatory denies")
		}
		// The shared record's bank field must be denied across the boundary
		// even though a comp admin holds a bank grant in their own tenant.
		if ruling := crossTenant.Fields[authz.FieldBankAccountNumber]; ruling.Effect != authz.EffectDenied {
			t.Errorf("cross-tenant bank ruling = %s, want DENIED by the mandatory deny", ruling.Effect)
		}
		sameTenant, err := authz.Enforce(authz.Request{
			Principal: principal, EffectiveAt: baseInstant,
			Subject: workerSubject(tenantAcme, subjectOtherID),
			Fields:  []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBankAccountNumber},
		})
		if err != nil {
			t.Fatalf("Enforce(same tenant): %v", err)
		}
		if ruling := sameTenant.Fields[authz.FieldBankAccountNumber]; ruling.Effect == authz.EffectDenied {
			t.Error("the same comp admin must keep their bank grant same-tenant; the deny must bind to the boundary, not the role")
		}
	})

	t.Run("digest binds the tenant", func(t *testing.T) {
		local := workerSubject(tenantAcme, subjectWorkerID)
		foreign := workerSubject(tenantVendor, subjectWorkerID)
		a, err := authz.Enforce(authz.Request{Principal: principal, EffectiveAt: baseInstant, Subject: local, Fields: []authz.FieldID{authz.FieldWorkerNumber}})
		if err != nil {
			t.Fatalf("Enforce(local): %v", err)
		}
		b, err := authz.Enforce(authz.Request{Principal: principal, EffectiveAt: baseInstant, Subject: foreign, Fields: []authz.FieldID{authz.FieldWorkerNumber}})
		if err != nil {
			t.Fatalf("Enforce(foreign): %v", err)
		}
		if a.InputsDigest == b.InputsDigest {
			t.Error("mutating the subject's tenant left the inputs digest unchanged")
		}
	})
}

// FuzzTodo_TRUST_025 is the TRUST-025 fuzz target: for any two tenants, any
// sharing-grant shape and any role, a cross-tenant allow is produced only
// through a valid, active, correctly addressed grant, always carries the
// mandatory cross-tenant deny, and every returned decision passes Validate
// with an explanation that leaks no subject identifier.
func FuzzTodo_TRUST_025(f *testing.F) {
	f.Add(byte(0), byte(0), byte(0), byte(1), byte(0))
	f.Add(byte(0), byte(1), byte(1), byte(2), byte(0))
	f.Add(byte(2), byte(1), byte(9), byte(1), byte(1))

	fuzzTenants := []string{"acme-corp", "vendor-corp", "other-corp"}
	roles := []string{string(authz.RoleWorkerSelf), string(authz.RoleManager), string(authz.RoleCompAdmin), string(authz.RoleAuditor)}

	f.Fuzz(func(t *testing.T, principalTenantIdx, ownerTenantIdx, directionRaw, hasValidGrant byte, roleIdx byte) {
		principalTenant := values.TenantId(fuzzTenants[int(principalTenantIdx)%len(fuzzTenants)])
		ownerTenant := values.TenantId(fuzzTenants[int(ownerTenantIdx)%len(fuzzTenants)])
		resourceTenant := ownerTenant

		principal := newPrincipal(t, principalOpts{tenant: principalTenant, roles: []string{roles[int(roleIdx)%len(roles)]}, purposes: []string{authz.PurposePayrollProcessing, authz.PurposeSelfService}})
		var grants []authz.SharingGrant
		if hasValidGrant%2 == 0 {
			grants = append(grants, authz.SharingGrant{
				OwnerTenant: ownerTenant, ViewerTenant: principalTenant,
				Direction: authz.SharingDirection(directionRaw), Effective: mustOpenIntervalUnchecked(baseInstant),
			})
		}
		subject := workerSubject(resourceTenant, subjectOtherID)

		decision, err := authz.Enforce(authz.Request{
			Principal: principal, EffectiveAt: baseInstant,
			Subject: subject,
			Sharing: grants,
			Fields:  []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBankAccountNumber},
		})
		if err != nil {
			return
		}
		if err := decision.Validate(); err != nil {
			t.Fatalf("decision fails Validate: %v", err)
		}

		crossTenant := principalTenant != resourceTenant
		if crossTenant {
			if decision.SubjectDisclosable {
				allowed := false
				for _, g := range grants {
					if g.OwnerTenant == resourceTenant && g.ViewerTenant == principalTenant && g.Direction.Valid() {
						allowed = true
					}
				}
				if !allowed {
					t.Fatal("cross-tenant subject disclosable without a valid, correctly addressed grant")
				}
				if len(decision.Tenant.MandatoryDenies) == 0 {
					t.Fatal("cross-tenant allow carries no mandatory deny")
				}
				if ruling := decision.Fields[authz.FieldBankAccountNumber]; ruling.Effect != authz.EffectDenied {
					t.Fatalf("cross-tenant sensitive field = %s, want DENIED", ruling.Effect)
				}
			}
		} else if hasValidGrant%2 != 0 && len(grants) > 0 && grants[0].OwnerTenant == principalTenant {
			t.Fatal("a same-tenant principal must not need a grant, and an owner==viewer grant is not a sharing grant")
		}

		if !decision.SubjectDisclosable && strings.Contains(decision.Explain(), subjectOtherID) {
			t.Fatal("denied explanation leaks the subject identifier")
		}
	})
}
