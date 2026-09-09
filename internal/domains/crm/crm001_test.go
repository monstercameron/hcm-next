package crm

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func crmRef(kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: "tenant-1", Kind: values.Kind(kind), Id: "00000000-0000-4000-8000-000000000001"}
}
func crmRevision(t *testing.T) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision("crm", 1)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func crmInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	d, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewOpenLocalDateInterval(d, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}
func validPool(t *testing.T) TalentPoolRevision {
	return TalentPoolRevision{PoolID: crmRef("talent_pool", "pool-1"), Revision: crmRevision(t), Purpose: "future recruiting", Criteria: "skills:go", Source: SourceAttribution{System: "ats", Reference: "import-1", RecordedBy: crmRef("principal", "p-1")}, Consent: ConsentAuthority{Authority: crmRef("processing_authority", "pa-1"), Basis: "consent", Evidence: crmRef("evidence", "ev-1")}, Scope: Scope{Organization: crmRef("organization", "org-1")}, Effective: crmInterval(t), Owner: crmRef("owner", "o-1"), RemovalPolicy: RemovalAtIntervalEnd}
}
func validMembership(t *testing.T) TalentPoolMembershipRevision {
	return TalentPoolMembershipRevision{MembershipID: crmRef("talent_pool_membership", "m-1"), Revision: crmRevision(t), Pool: crmRef("talent_pool", "pool-1"), Subject: crmRef("candidate", "c-1"), Role: MembershipCandidate, Purpose: "future recruiting", Source: SourceAttribution{System: "ats", Reference: "import-1", RecordedBy: crmRef("principal", "p-1")}, Consent: ConsentAuthority{Authority: crmRef("processing_authority", "pa-1"), Basis: "consent", Evidence: crmRef("evidence", "ev-1")}, Scope: Scope{Organization: crmRef("organization", "org-1")}, Effective: crmInterval(t), Owner: crmRef("owner", "o-1"), RemovalPolicy: RemovalAtIntervalEnd}
}
func TestTodo_CRM_001(t *testing.T) {
	if err := validPool(t).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := validMembership(t).Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_CRM_001_Conformance(t *testing.T) {
	m := validMembership(t)
	m.Subject = crmRef("worker", "w-1")
	m.Role = MembershipWorker
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_CRM_001_Mutation(t *testing.T) {
	p := validPool(t)
	p.Purpose = " "
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "purpose") {
		t.Fatalf("got %v, want purpose rejection", err)
	}
}
func TestTodo_CRM_001_Security(t *testing.T) {
	m := validMembership(t)
	m.Subject = values.EntityRef{Tenant: "tenant-2", Kind: "candidate", Id: "00000000-0000-4000-8000-000000000001"}
	if err := m.Validate(); err == nil {
		t.Fatal("cross-tenant membership accepted")
	}
}
