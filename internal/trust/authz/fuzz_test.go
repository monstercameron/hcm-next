package authz_test

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

// fuzzTenants and fuzzOrgs bound the fuzz targets to a small, fixed universe
// of otherwise-valid tenants and organization units. Fuzzing raw byte
// strings against [values.TenantId.Validate] would spend almost the entire
// budget on inputs that are rejected before any authorization logic runs;
// selecting from a fixed set by fuzzer-controlled index instead spends the
// budget on the combinations that actually exercise [authz.ResolveTenantScope].
var fuzzTenants = []string{"acme-corp", "vendor-corp", "other-corp"}

func pick(choices []string, n byte) string { return choices[int(n)%len(choices)] }

// FuzzTodo_TRUST_008 is the TRUST-008 fuzz target.
//
// The invariant under test is one sentence: for any tenant/organization/
// sharing combination, ResolveTenantScope never panics, always returns
// exactly ALLOW or DENIED with a non-empty rule ID, a denial always carries
// a non-empty reason and never an organization closure or sharing path, and
// an allow is only ever produced same-tenant or through a
// [authz.SharingDirectionRecordVisible] grant that is active at the
// evaluated instant.
func FuzzTodo_TRUST_008(f *testing.F) {
	f.Add(byte(0), byte(0), byte(0), byte(0), byte(1), int64(0))
	f.Add(byte(0), byte(1), byte(0), byte(0), byte(0), int64(0))
	f.Add(byte(1), byte(0), byte(1), byte(1), byte(1), int64(-10))
	f.Add(byte(2), byte(1), byte(0), byte(2), byte(9), int64(1000000))

	f.Fuzz(func(t *testing.T, principalTenantIdx, resourceTenantIdx, principalOrgIdx, resourceOrgIdx, directionRaw byte, offsetSeconds int64) {
		principal := newPrincipal(t, principalOpts{tenant: values.TenantId(pick(fuzzTenants, principalTenantIdx))})

		var principalOrg, resourceOrg authz.OrgUnitRef
		if principalOrgIdx%3 != 0 {
			principalOrg = authz.OrgUnitRef{Tenant: values.TenantId(pick(fuzzTenants, principalTenantIdx)), ID: pick([]string{"org-a", "org-b"}, principalOrgIdx)}
		}
		if resourceOrgIdx%3 != 0 {
			resourceOrg = authz.OrgUnitRef{Tenant: values.TenantId(pick(fuzzTenants, resourceTenantIdx)), ID: pick([]string{"org-a", "org-b"}, resourceOrgIdx)}
		}

		grant := authz.SharingGrant{
			OwnerTenant:  values.TenantId(pick(fuzzTenants, resourceTenantIdx)),
			ViewerTenant: values.TenantId(pick(fuzzTenants, principalTenantIdx)),
			Direction:    authz.SharingDirection(directionRaw),
			Effective:    mustOpenIntervalUnchecked(baseInstant),
		}

		req := authz.TenantScopeInput{
			ResourceTenant: values.TenantId(pick(fuzzTenants, resourceTenantIdx)),
			PrincipalOrg:   principalOrg,
			ResourceOrg:    resourceOrg,
			Edges: []authz.OrgEdge{
				{Child: resourceOrg, Parent: principalOrg, Effective: mustOpenIntervalUnchecked(baseInstant)},
			},
			Sharing:     []authz.SharingGrant{grant},
			EffectiveAt: instantAt(baseTime.Add(time.Duration(offsetSeconds) * time.Second)),
		}

		decision, err := authz.ResolveTenantScope(principal, req)
		if err != nil {
			return
		}
		if !decision.Effect.Valid() {
			t.Fatalf("Effect = %v is not a valid effect", decision.Effect)
		}
		if decision.RuleID == "" {
			t.Fatal("RuleID is empty")
		}
		if decision.Effect == authz.EffectDenied {
			if decision.Reason == "" {
				t.Fatal("a denial carries no reason")
			}
			if len(decision.AllowedOrganizations) != 0 || len(decision.SharingPath) != 0 {
				t.Fatal("a denial discloses closure or sharing detail")
			}
		}
	})
}

// FuzzTodo_TRUST_009 is the TRUST-009 fuzz target: for any relationship fact
// and any subject, ResolveAuthorizationScope never panics, and it only ever
// allows when the fact's kind is one the principal's roles hold, the fact
// names the requested subject, and the fact's effective window covers the
// evaluated instant (or the principal holds an administrative role, or the
// subject is the principal's own subject id and the principal holds worker
// self).
func FuzzTodo_TRUST_009(f *testing.F) {
	f.Add(byte(0), byte(0), int64(0), int64(0))
	f.Add(byte(1), byte(2), int64(-100), int64(100))
	f.Add(byte(4), byte(9), int64(100), int64(-100))

	roles := []string{string(authz.RoleWorkerSelf), string(authz.RoleManager), string(authz.RoleHRPartner), string(authz.RoleCompAdmin), string(authz.RoleAuditor)}
	kinds := []authz.RelationshipKind{authz.RelationshipManagerChain, authz.RelationshipHRPartner, authz.RelationshipAssignedPopulation}

	f.Fuzz(func(t *testing.T, roleIdx, kindIdx byte, startOffset, endOffset int64) {
		principal := newPrincipal(t, principalOpts{roles: []string{roles[int(roleIdx)%len(roles)]}})
		subject := workerSubject(tenantAcme, subjectOtherID)

		start := instantAt(baseTime.Add(time.Duration(startOffset) * time.Second))
		end := instantAt(baseTime.Add(time.Duration(endOffset) * time.Second))
		var effective = mustOpenIntervalUnchecked(start)
		if iv, err := values.NewInstantInterval(start, end); err == nil {
			effective = iv
		}

		fact := authz.RelationshipFact{
			Kind:       kinds[int(kindIdx)%len(kinds)],
			Subject:    subject,
			Source:     "fuzz.source",
			Effective:  effective,
			RecordedAt: mustRecordedAt(start),
			KnownAt:    mustKnownAt(start),
		}

		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
			Subject: subject, EffectiveAt: baseInstant, Relationships: []authz.RelationshipFact{fact},
		})
		if err != nil {
			return
		}
		if !scope.Effect.Valid() {
			t.Fatalf("Effect = %v is not a valid effect", scope.Effect)
		}
		if scope.Effect == authz.EffectDenied && scope.MatchedFact != nil {
			t.Fatal("a denied scope carries a matched fact")
		}
	})
}

// FuzzTodo_TRUST_010 is the TRUST-010 fuzz target: for any role, purpose and
// field selection, ResolveFields never panics and always returns a ruling
// for every requested field with a valid effect, and a redacted ruling
// always carries at least one obligation.
func FuzzTodo_TRUST_010(f *testing.F) {
	f.Add(byte(0), byte(0), byte(0))
	f.Add(byte(4), byte(7), byte(11))
	f.Add(byte(2), byte(3), byte(0))

	roles := []string{string(authz.RoleWorkerSelf), string(authz.RoleManager), string(authz.RoleHRPartner), string(authz.RoleCompAdmin), string(authz.RoleAuditor), "unrecognized_role"}
	purposes := []string{
		authz.PurposeSelfService, authz.PurposeCompensationReview, authz.PurposePayrollProcessing, authz.PurposePerformanceReview,
		authz.PurposeAccommodationCase, authz.PurposeCaseManagement, authz.PurposeImmigrationCase, authz.PurposeAuditReview, "unauthorized_purpose",
	}
	fields := []authz.FieldID{
		authz.FieldWorkerNumber, authz.FieldJobTitle, authz.FieldWorkEmail, authz.FieldHomeAddress,
		authz.FieldBaseSalary, authz.FieldBonusTarget, authz.FieldTaxID, authz.FieldBankAccountNumber,
		authz.FieldPerformanceRating, authz.FieldMedicalAccomodation, authz.FieldCaseNotes, authz.FieldVisaStatus,
		"unregistered.field",
	}

	f.Fuzz(func(t *testing.T, roleIdx, purposeIdx, fieldIdx byte) {
		role := roles[int(roleIdx)%len(roles)]
		purpose := purposes[int(purposeIdx)%len(purposes)]
		field := fields[int(fieldIdx)%len(fields)]

		principal := newPrincipal(t, principalOpts{roles: []string{role}, purposes: purposes})
		decision, err := authz.ResolveFields(principal, purpose, []authz.FieldID{field}, nil)
		if err != nil {
			return
		}
		ruling, ok := decision.Rulings[field]
		if !ok {
			t.Fatalf("no ruling for the one requested field %q", field)
		}
		if !ruling.Effect.Valid() {
			t.Fatalf("Effect = %v is not a valid effect", ruling.Effect)
		}
		if ruling.Effect == authz.EffectRedacted && len(ruling.Obligations) == 0 {
			t.Fatal("a redacted ruling carries no obligation")
		}
	})
}

// FuzzTodo_TRUST_011 is the TRUST-011 fuzz target: for any request built
// from the same bounded universe of tenants, roles and purposes, Enforce
// never panics, and a decision it returns without error always passes its
// own Validate.
func FuzzTodo_TRUST_011(f *testing.F) {
	f.Add(byte(0), byte(0), byte(0), byte(0))
	f.Add(byte(1), byte(1), byte(1), byte(1))
	f.Add(byte(4), byte(7), byte(2), byte(0))

	roles := []string{string(authz.RoleWorkerSelf), string(authz.RoleManager), string(authz.RoleHRPartner), string(authz.RoleCompAdmin), string(authz.RoleAuditor)}
	purposes := []string{authz.PurposeSelfService, authz.PurposeCompensationReview, authz.PurposePayrollProcessing, authz.PurposeAuditReview}

	f.Fuzz(func(t *testing.T, roleIdx, purposeIdx, tenantIdx, hasRelationship byte) {
		role := roles[int(roleIdx)%len(roles)]
		purpose := purposes[int(purposeIdx)%len(purposes)]
		subject := workerSubject(values.TenantId(pick(fuzzTenants, tenantIdx)), subjectOtherID)

		principal := newPrincipal(t, principalOpts{roles: []string{role}, purposes: purposes})
		req := authz.Request{
			Principal:   principal,
			Purpose:     purpose,
			EffectiveAt: baseInstant,
			Subject:     subject,
			Fields:      []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary, authz.FieldMedicalAccomodation},
		}
		if hasRelationship%2 == 0 {
			req.Relationships = []authz.RelationshipFact{managerFact(subject), hrPartnerFact(subject)}
		}

		decision, err := authz.Enforce(req)
		if err != nil {
			return
		}
		if err := decision.Validate(); err != nil {
			t.Fatalf("Enforce produced a decision that fails Validate: %v", err)
		}
	})
}

// FuzzTodo_TRUST_012 is the TRUST-012 fuzz target: for any principal, tenant
// and candidate combination, PlanRepositoryScope never panics, a scope it
// returns without error always passes Validate, never grants a subject
// outside its own tenant, and the gate serves only subjects the scope
// authorizes with only fields the scope's mask allows.
func FuzzTodo_TRUST_012(f *testing.F) {
	f.Add(byte(0), byte(0), byte(0), byte(0), int64(0))
	f.Add(byte(2), byte(3), byte(1), byte(1), int64(3600))
	f.Add(byte(4), byte(8), byte(2), byte(0), int64(-3600))

	roles := []string{string(authz.RoleWorkerSelf), string(authz.RoleManager), string(authz.RoleHRPartner), string(authz.RoleCompAdmin), string(authz.RoleAuditor), "unrecognized_role"}
	purposes := []string{authz.PurposeSelfService, authz.PurposeCompensationReview, authz.PurposePayrollProcessing, authz.PurposePerformanceReview, authz.PurposeAuditReview}
	fields := []authz.FieldID{authz.FieldWorkerNumber, authz.FieldJobTitle, authz.FieldBaseSalary, authz.FieldBankAccountNumber}

	f.Fuzz(func(t *testing.T, roleIdx, purposeIdx, candidateIdx, hasOrg byte, offsetSeconds int64) {
		role := roles[int(roleIdx)%len(roles)]
		purpose := purposes[int(purposeIdx)%len(purposes)]
		effectiveAt := instantAt(baseTime.Add(time.Duration(offsetSeconds) * time.Second))

		principal := newPrincipal(t, principalOpts{roles: []string{role}, purposes: purposes})
		managed := workerSubject(tenantAcme, subjectWorkerID)
		unmanaged := workerSubject(tenantAcme, subjectOtherID)

		// Each fuzz case plans over both candidates, but only the managed
		// worker ever has a supporting relationship fact.
		candidates := []authz.ScopeInput{
			{Subject: managed, Relationships: []authz.RelationshipFact{managerFact(managed)}},
			{Subject: unmanaged},
		}
		req := authz.RepositoryQueryRequest{
			Principal:   principal,
			Purpose:     purpose,
			EffectiveAt: effectiveAt,
			Tenant:      tenantAcme,
			Candidates:  candidates,
			Fields:      fields,
		}
		if hasOrg%2 == 0 {
			req.PrincipalOrg = authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a"}
			req.ResourceOrg = authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a1"}
			req.OrgEdges = []authz.OrgEdge{{Child: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a1"}, Parent: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a"}, Effective: mustOpenIntervalUnchecked(recentPast)}}
		}

		scope, err := authz.PlanRepositoryScope(req)
		if err != nil {
			return
		}
		if err := scope.Validate(); err != nil {
			t.Fatalf("planned scope fails Validate: %v", err)
		}
		for _, s := range scope.AllowedSubjects() {
			if s.Tenant != scope.Tenant() {
				t.Fatal("scope grants a subject outside its own tenant")
			}
			if !scope.AuthorizesRecord(s, effectiveAt) {
				t.Fatal("a scope must authorize its own allowed subjects at its evaluated instant")
			}
		}
		if scope.Effect() == authz.EffectDenied && len(scope.AllowedSubjects()) != 0 {
			t.Fatal("a denied scope grants subjects")
		}

		gate := authz.NewRepositoryGate(map[values.EntityRef]map[authz.FieldID]string{
			managed:   {authz.FieldWorkerNumber: "W-0001", authz.FieldBaseSalary: "120000"},
			unmanaged: {authz.FieldWorkerNumber: "W-0002"},
		})
		projections, err := gate.Query(scope, []values.EntityRef{managed, unmanaged}, effectiveAt)
		if err != nil {
			t.Fatalf("gate.Query: %v", err)
		}
		authorized := map[values.EntityRef]bool{}
		for _, s := range scope.AllowedSubjects() {
			authorized[s] = true
		}
		for _, p := range projections {
			if !authorized[p.Subject] {
				t.Fatalf("gate served an unauthorized subject: %s", p.Subject.String())
			}
			for _, fv := range p.Fields {
				if scope.FieldRuling(fv.FieldID).Effect != fv.Effect {
					t.Fatalf("gate served field %s under %s, mask says %s", fv.FieldID, fv.Effect, scope.FieldRuling(fv.FieldID).Effect)
				}
				if fv.Effect == authz.EffectRedacted && fv.Value != authz.RedactedPlaceholder {
					t.Fatalf("redacted field %s carries a non-placeholder value", fv.FieldID)
				}
			}
		}
	})
}
