package model_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

func baseIdentityLink(t *testing.T) model.IdentityLink {
	t.Helper()
	return model.IdentityLink{
		LinkRef:            "identity.link.workday_employee/1",
		ExternalSystem:     "WORKDAY",
		ExternalID:         "WD-1001",
		CanonicalRef:       "Person/v1#p-1",
		MatchKey:           "EMPLOYEE_NUMBER",
		Confidence:         1.0,
		Effective:          mustOpenInterval(t, 2025, time.January, 1),
		TenantRef:          "tenant-a",
		PurposeScope:       "PAYROLL",
		EvidenceRef:        "evidence:link-1",
		SourceAuthorityRef: "authority.hris/v1",
	}
}

// TestTodo_MODEL_022 is the PRIMARY test for exact identity linkage.
//
// RED: lookup returns AMBIGUOUS for collision, refuses false auto-merge and
// prevents merged redirect from crossing tenant/purpose policy.
//
// GREEN: approved external identity resolves to one canonical identity with
// confidence/evidence; merge and separation preserve lineage.
func TestTodo_MODEL_022(t *testing.T) {
	linkA := baseIdentityLink(t)

	collidingB := baseIdentityLink(t)
	collidingB.LinkRef = "identity.link.workday_employee/2"
	collidingB.CanonicalRef = "Person/v1#p-2"

	crossTenant := baseIdentityLink(t)
	crossTenant.LinkRef = "identity.link.cross_tenant/1"
	crossTenant.ExternalID = "WD-2002"
	crossTenant.TenantRef = "tenant-b"

	links := []model.IdentityLink{linkA, collidingB, crossTenant}

	t.Run("RED", func(t *testing.T) {
		t.Run("ambiguous collision", func(t *testing.T) {
			_, err := model.ResolveIdentity(links, "WORKDAY", "WD-1001", instant(2026, time.June, 1), "tenant-a", "PAYROLL")
			if !errors.Is(err, model.ErrIdentityAmbiguous) {
				t.Fatalf("resolved an ambiguous collision: %v", err)
			}
		})

		t.Run("cross-tenant policy violation", func(t *testing.T) {
			_, err := model.ResolveIdentity(links, "WORKDAY", "WD-2002", instant(2026, time.June, 1), "tenant-a", "PAYROLL")
			if !errors.Is(err, model.ErrIdentityCrossTenantPolicy) {
				t.Fatalf("resolved a link across its declared tenant: %v", err)
			}
		})

		t.Run("refuses false auto-merge", func(t *testing.T) {
			constraints := []model.DoNotMergeConstraint{{
				ConstraintRef: "dnm.1",
				RefA:          "Person/v1#p-1",
				RefB:          "Person/v1#p-2",
				Reason:        "confirmed distinct twins sharing an employee number typo",
				AuthorityRef:  "authority.governance/v1",
			}}
			_, err := model.MergeIdentities(constraints, "Person/v1#p-1", "Person/v1#p-2",
				"tenant-a", "tenant-a", "PAYROLL", "PAYROLL",
				instant(2026, time.June, 1), "evidence:merge-1", "prov:merge-1")
			if !errors.Is(err, model.ErrIdentityDoNotMerge) {
				t.Fatalf("merged two identities a do-not-merge constraint protects: %v", err)
			}
		})

		t.Run("prevents merged redirect from crossing tenant/purpose policy", func(t *testing.T) {
			_, err := model.MergeIdentities(nil, "Person/v1#p-1", "Person/v1#p-3",
				"tenant-a", "tenant-b", "PAYROLL", "PAYROLL",
				instant(2026, time.June, 1), "evidence:merge-2", "prov:merge-2")
			if !errors.Is(err, model.ErrIdentityCrossTenantPolicy) {
				t.Fatalf("merged across tenants: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		t.Run("resolves to one canonical identity with confidence and evidence", func(t *testing.T) {
			res, err := model.ResolveIdentity([]model.IdentityLink{linkA}, "WORKDAY", "WD-1001", instant(2026, time.June, 1), "tenant-a", "PAYROLL")
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if res.CanonicalRef != linkA.CanonicalRef {
				t.Fatalf("canonical = %s, want %s", res.CanonicalRef, linkA.CanonicalRef)
			}
			if res.Confidence != 1.0 {
				t.Fatalf("confidence = %v, want an exact match of 1.0", res.Confidence)
			}
			if res.EvidenceRef == "" {
				t.Fatalf("resolution carries no evidence")
			}
		})

		t.Run("merge and separation preserve lineage", func(t *testing.T) {
			merge, err := model.MergeIdentities(nil, "Person/v1#p-1", "Person/v1#p-4",
				"tenant-a", "tenant-a", "PAYROLL", "PAYROLL",
				instant(2026, time.June, 1), "evidence:merge-3", "prov:merge-3")
			if err != nil {
				t.Fatalf("merge: %v", err)
			}
			if merge.Redirect.FormerRef != "Person/v1#p-4" || merge.Redirect.RedirectsTo != "Person/v1#p-1" {
				t.Fatalf("redirect = %+v", merge.Redirect)
			}
			if merge.ProvenanceRef == "" {
				t.Fatalf("merge carries no provenance")
			}

			sep, err := model.SeparateIdentities(merge, instant(2026, time.July, 1), "evidence:sep-1", "prov:sep-1")
			if err != nil {
				t.Fatalf("separate: %v", err)
			}
			if sep.RestoredRef != "Person/v1#p-4" {
				t.Fatalf("restored ref = %s, want Person/v1#p-4", sep.RestoredRef)
			}
			if sep.ProvenanceRef == "" {
				t.Fatalf("separation carries no provenance")
			}
			// The original merge record is never mutated: separation is an
			// append-only supersession, not an edit.
			if merge.SurvivingRef != "Person/v1#p-1" || merge.RetiredRef != "Person/v1#p-4" {
				t.Fatalf("separation mutated the original merge record: %+v", merge)
			}
		})
	})
}

// TestTodo_MODEL_022_Property asserts a general invariant: every
// [IdentityLink] this package considers valid carries the exact-match
// confidence value, never a fuzzy score.
func TestTodo_MODEL_022_Property(t *testing.T) {
	link := baseIdentityLink(t)
	if err := link.Validate(); err != nil {
		t.Fatalf("a well-formed exact link failed to validate: %v", err)
	}
	for _, score := range []float64{0, 0.5, 0.99, 1.01, 2} {
		bad := link
		bad.Confidence = score
		if err := bad.Validate(); !errors.Is(err, model.ErrInvalidIdentityLink) {
			t.Fatalf("confidence %v validated as an exact match", score)
		}
	}
}

// TestTodo_MODEL_022_Golden pins one identity resolution's shape.
func TestTodo_MODEL_022_Golden(t *testing.T) {
	link := baseIdentityLink(t)
	res, err := model.ResolveIdentity([]model.IdentityLink{link}, link.ExternalSystem, link.ExternalID, instant(2026, time.June, 1), link.TenantRef, link.PurposeScope)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	goldenJSON(t, "model_022_identity_resolution.json", res)
}

// TestTodo_MODEL_022_Fault proves a rejected merge returns a zero-value
// result and never leaves a half-built redirect behind.
func TestTodo_MODEL_022_Fault(t *testing.T) {
	constraints := []model.DoNotMergeConstraint{{
		ConstraintRef: "dnm.1",
		RefA:          "Person/v1#p-1",
		RefB:          "Person/v1#p-2",
		Reason:        "confirmed distinct people",
		AuthorityRef:  "authority.governance/v1",
	}}
	before := append([]model.DoNotMergeConstraint(nil), constraints...)

	merge, err := model.MergeIdentities(constraints, "Person/v1#p-1", "Person/v1#p-2",
		"tenant-a", "tenant-a", "PAYROLL", "PAYROLL",
		instant(2026, time.June, 1), "evidence:merge-1", "prov:merge-1")
	if err == nil {
		t.Fatalf("expected the do-not-merge constraint to block this merge")
	}
	if !reflect.DeepEqual(merge, model.IdentityMergeResult{}) {
		t.Fatalf("a rejected merge returned a non-zero result: %+v", merge)
	}
	if !reflect.DeepEqual(constraints, before) {
		t.Fatalf("a rejected merge mutated its constraint input")
	}
}

// TestTodo_MODEL_022_Security proves a resolution scoped to one tenant or
// purpose can never surface a link declared under a different tenant or
// purpose, even when the external identity would otherwise resolve cleanly:
// a redirect must never cross tenant or purpose policy.
func TestTodo_MODEL_022_Security(t *testing.T) {
	link := baseIdentityLink(t)
	links := []model.IdentityLink{link}

	cases := []struct {
		name         string
		tenantRef    string
		purposeScope string
	}{
		{"wrong tenant", "tenant-z", link.PurposeScope},
		{"wrong purpose", link.TenantRef, "BENEFITS"},
		{"wrong tenant and purpose", "tenant-z", "BENEFITS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := model.ResolveIdentity(links, link.ExternalSystem, link.ExternalID, instant(2026, time.June, 1), tc.tenantRef, tc.purposeScope)
			if !errors.Is(err, model.ErrIdentityCrossTenantPolicy) {
				t.Fatalf("resolved %s across policy: %v", tc.name, err)
			}
		})
	}
}

// FuzzTodo_MODEL_022 fuzzes identity resolution: ResolveIdentity must never
// return a canonical reference for a tenant/purpose pair that does not match
// every candidate link's declared scope.
func FuzzTodo_MODEL_022(f *testing.F) {
	f.Add("tenant-a", "PAYROLL")
	f.Add("tenant-a", "BENEFITS")
	f.Add("tenant-z", "PAYROLL")
	f.Fuzz(func(t *testing.T, tenantRef, purposeScope string) {
		link := model.IdentityLink{
			LinkRef:            "identity.link.fuzz/1",
			ExternalSystem:     "WORKDAY",
			ExternalID:         "WD-1001",
			CanonicalRef:       "Person/v1#p-1",
			MatchKey:           "EMPLOYEE_NUMBER",
			Confidence:         1.0,
			Effective:          mustOpenInterval(t, 2025, time.January, 1),
			TenantRef:          "tenant-a",
			PurposeScope:       "PAYROLL",
			EvidenceRef:        "evidence:link-1",
			SourceAuthorityRef: "authority.hris/v1",
		}
		res, err := model.ResolveIdentity([]model.IdentityLink{link}, "WORKDAY", "WD-1001", instant(2026, time.June, 1), tenantRef, purposeScope)
		matches := tenantRef == link.TenantRef && purposeScope == link.PurposeScope
		if matches && err != nil {
			t.Fatalf("matching tenant/purpose failed to resolve: %v", err)
		}
		if !matches && err == nil {
			t.Fatalf("mismatched tenant/purpose (%q,%q) resolved to %+v", tenantRef, purposeScope, res)
		}
		if !matches && !errors.Is(err, model.ErrIdentityCrossTenantPolicy) {
			t.Fatalf("mismatched tenant/purpose failed with the wrong error: %v", err)
		}
	})
}
