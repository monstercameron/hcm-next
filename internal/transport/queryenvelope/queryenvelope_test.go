package queryenvelope_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/queryenvelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var (
	tenant = values.TenantId("tenant-acme")
	now    = values.NewInstant(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
)

func principal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: "00000000-0000-0000-0000-000000000001", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{string(authz.RoleCompAdmin)}, Purposes: []string{authz.PurposeCompensationReview},
		Assurance: trust.AssuranceHigh, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		SessionRef: "session-1", IssuedAt: now.Time().Add(-time.Minute), ExpiresAt: now.Time().Add(time.Hour),
		CredentialDigest: "credential-digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func scope(t *testing.T) authz.RepositoryScope {
	t.Helper()
	s, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
		Principal: principal(t), Purpose: authz.PurposeCompensationReview, EffectiveAt: now,
		Tenant: tenant, Candidates: []authz.ScopeInput{{Subject: values.EntityRef{Tenant: tenant, Kind: "worker", Id: "00000000-0000-0000-0000-000000000002"}, EffectiveAt: now}},
		Fields: []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func request(t *testing.T) queryenvelope.Request {
	t.Helper()
	return queryenvelope.Request{
		SliceID: "promotion", SliceVersion: 1, QueryID: "promotion.worker.list", Resource: "worker",
		ReadAt: now, MaxRows: 50, Scope: scope(t),
		Freshness: queryenvelope.Freshness{State: queryenvelope.FreshnessCurrent, SourceVersion: "ledger:v1", Watermark: "42"},
	}
}

func TestTodo_ALIGN_017(t *testing.T) {
	envelope, err := queryenvelope.New(request(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := envelope.Validate(); err != nil {
		t.Fatal(err)
	}
	if !envelope.Authorized() || len(envelope.AllowedSubjects()) != 1 {
		t.Fatalf("envelope authorization = %+v", envelope)
	}
	if envelope.Explain() == "" || envelope.Digest() == "" {
		t.Fatal("envelope has no bounded explanation or digest")
	}
}

func TestTodo_ALIGN_017_Property(t *testing.T) {
	envelope, err := queryenvelope.New(request(t))
	if err != nil {
		t.Fatal(err)
	}
	fields := envelope.Fields
	if len(fields) != 2 || string(fields[0].Field) >= string(fields[1].Field) {
		t.Fatalf("field dispositions are not canonical: %+v", fields)
	}
	subjects := envelope.AllowedSubjects()
	subjects[0].Id = "mutated"
	if envelope.AllowedSubjects()[0].Id == "mutated" {
		t.Fatal("allowed subject slice aliases envelope storage")
	}
}

func TestTodo_ALIGN_017_Golden(t *testing.T) {
	first, err := queryenvelope.New(request(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := queryenvelope.New(request(t))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() || first.Explain() != second.Explain() {
		t.Fatalf("same authorized query is not deterministic: %s / %s", first.Digest(), second.Digest())
	}
}

func TestTodo_ALIGN_017_Security(t *testing.T) {
	bad := request(t)
	bad.MaxRows = 0
	if _, err := queryenvelope.New(bad); err == nil {
		t.Fatal("unbounded query was accepted")
	}
	bad = request(t)
	bad.ReadAt = values.NewInstant(now.Time().Add(time.Second))
	if _, err := queryenvelope.New(bad); err == nil {
		t.Fatal("query with a stale authorization instant was accepted")
	}
}

func TestTodo_ALIGN_017_Conformance(t *testing.T) {
	if queryenvelope.Version() != 1 {
		t.Fatalf("envelope version = %d, want 1", queryenvelope.Version())
	}
}
