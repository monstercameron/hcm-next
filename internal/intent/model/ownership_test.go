package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func ref(kind, id string) values.EntityRef {
	ids := map[string]string{
		"p1": "00000000-0000-0000-0000-000000000001",
		"e1": "00000000-0000-0000-0000-000000000002",
		"c1": "00000000-0000-0000-0000-000000000003",
	}
	if canonical, ok := ids[id]; ok {
		id = canonical
	}
	return values.EntityRef{Tenant: "tenant-a", Kind: values.Kind(kind), Id: id}
}
func ownershipPolicy() Policy {
	return Policy{Revision: "contact-address/v1", Bindings: []OwnershipBinding{
		{Kind: FactContactPoint, Scope: ScopePersonalContact, Customer: "customer-a", Country: "US", OwnerKind: values.Kind("person"), Role: "personal-contact", AuthorityRef: "person-authority", Classification: "CONFIDENTIAL", CorrectionPolicy: "append-revision", ExportPolicy: "subject-authorized", MergePolicy: "review-lineage"},
		{Kind: FactContactPoint, Scope: ScopeWorkContact, Customer: "customer-a", Country: "US", OwnerKind: values.Kind("employment"), Role: "employment-contact", AuthorityRef: "employment-authority", Classification: "RESTRICTED", CorrectionPolicy: "append-revision", ExportPolicy: "employment-authorized", MergePolicy: "review-lineage"},
		{Kind: FactAddress, Scope: ScopeHomeAddress, Customer: "customer-a", Country: "US", OwnerKind: values.Kind("person"), Role: "home-address", AuthorityRef: "person-authority", Classification: "RESTRICTED", CorrectionPolicy: "append-revision", ExportPolicy: "subject-authorized", MergePolicy: "review-lineage"},
		{Kind: FactAddress, Scope: ScopeWorkAddress, Customer: "customer-a", Country: "US", OwnerKind: values.Kind("employment"), Role: "work-location", AuthorityRef: "employment-authority", Classification: "INTERNAL", PermittedDerivations: []string{"directory-location"}, CorrectionPolicy: "append-revision", ExportPolicy: "employment-authorized", MergePolicy: "review-lineage"},
	}}
}
func selector(kind FactKind, scope Scope) FactSelector {
	return FactSelector{Kind: kind, Scope: scope, PersonRef: ref("person", "p1"), EmploymentRef: ref("employment", "e1"), Customer: "customer-a", Country: "US"}
}
func interval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	a, _ := values.NewLocalDate(2026, time.January, 1)
	b, _ := values.NewLocalDate(2027, time.January, 1)
	iv, err := values.NewLocalDateInterval(a, b, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}
func TestContactAndAddressOwnershipPreservesScopeAuthorityAndHistory(t *testing.T) {
	p := ownershipPolicy()
	personal, err := p.Resolve(selector(FactContactPoint, ScopePersonalContact))
	if err != nil || personal.OwnerRef.Kind != "person" || !personal.WriteAllowed {
		t.Fatalf("personal=%#v err=%v", personal, err)
	}
	work, err := p.Resolve(selector(FactContactPoint, ScopeWorkContact))
	if err != nil || work.OwnerRef != ref("employment", "e1") {
		t.Fatalf("work=%#v err=%v", work, err)
	}
	if personal.OwnerRef == work.OwnerRef {
		t.Fatal("personal and employment scope collapsed")
	}
}
func TestTodo_MODEL_031_Property(t *testing.T) {
	p := ownershipPolicy()
	s := selector(FactKind("bad"), ScopePersonalContact)
	got, err := p.Resolve(s)
	if !errors.Is(err, ErrInvalidOwnership) || got.WriteAllowed {
		t.Fatalf("invalid selector=%#v err=%v", got, err)
	}
}
func TestTodo_MODEL_031_Golden(t *testing.T) {
	p := ownershipPolicy()
	if len(p.Digest()) != 64 || p.Digest() != p.Digest() {
		t.Fatalf("digest=%q", p.Digest())
	}
	got, _ := p.Resolve(selector(FactAddress, ScopeWorkAddress))
	if !strings.Contains(got.Explain(), "scope=WORK_ADDRESS") {
		t.Fatalf("explain=%q", got.Explain())
	}
}
func TestTodo_MODEL_031_Security(t *testing.T) {
	p := ownershipPolicy()
	s := selector(FactContactPoint, ScopeWorkContact)
	s.Country = "CA"
	got, err := p.Resolve(s)
	if !errors.Is(err, ErrUnknownScope) || got.Status != UnknownScope || got.WriteAllowed || got.OwnerRef.Id != "" {
		t.Fatalf("unknown=%#v err=%v", got, err)
	}
	p.Bindings = append(p.Bindings,
		OwnershipBinding{Kind: FactContactPoint, Scope: ScopePersonalContact, Customer: "*", Country: "US", OwnerKind: values.Kind("person"), Role: "generic", AuthorityRef: "person-authority", Classification: "CONFIDENTIAL", CorrectionPolicy: "review", ExportPolicy: "review", MergePolicy: "review"},
		OwnershipBinding{Kind: FactContactPoint, Scope: ScopePersonalContact, Customer: "customer-b", Country: "*", OwnerKind: values.Kind("person"), Role: "customer", AuthorityRef: "person-authority", Classification: "CONFIDENTIAL", CorrectionPolicy: "review", ExportPolicy: "review", MergePolicy: "review"},
	)
	s = selector(FactContactPoint, ScopePersonalContact)
	s.Customer = "customer-b"
	got, err = p.Resolve(s)
	if !errors.Is(err, ErrUnknownScope) || got.Status != UnknownScope || got.WriteAllowed {
		t.Fatalf("ambiguous=%#v err=%v", got, err)
	}
}
func TestTodo_MODEL_031_Conformance(t *testing.T) {
	rev := ContactPointRevision{ContactPointRef: ref("contact_point", "c1"), PersonRef: ref("person", "p1"), EmploymentRef: ref("employment", "e1"), Scope: ScopeWorkContact, Channel: "EMAIL", ValueRef: "provider-value-1", PurposeTags: []string{"WORK"}, AuthorityRef: "employment-authority", Classification: "RESTRICTED", Effective: interval(t), KnownAt: values.NewInstant(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)), Revision: 1}
	if err := rev.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_MODEL_031_Mutation(t *testing.T) {
	p := ownershipPolicy()
	if out, err := p.MigrateLegacy(LegacyFact{ID: "legacy-1", Selector: selector(FactAddress, ScopeHomeAddress)}); err != nil || out.Disposition != LegacyLink {
		t.Fatalf("migration=%#v err=%v", out, err)
	}
	unknown, _ := p.MigrateLegacy(LegacyFact{ID: "legacy-2", Selector: FactSelector{Kind: FactAddress, Scope: ScopeHomeAddress, PersonRef: ref("person", "p1"), Customer: "customer-a", Country: "CA"}})
	if unknown.Disposition != LegacyReview || unknown.Resolution.WriteAllowed {
		t.Fatalf("unknown migration=%#v", unknown)
	}
}
