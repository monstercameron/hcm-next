package authz

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func internalRepositoryScope() RepositoryScope {
	at := values.NewInstant(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	subject := values.EntityRef{Tenant: values.TenantId("acme-corp"), Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000001"}
	return RepositoryScope{
		evaluated: true, effect: EffectAllow, ruleID: "p1a.repository.scope.test", reason: "allowed",
		tenant: values.TenantId("acme-corp"), purpose: "test", evaluatedAt: at,
		organizations:   []OrgUnitRef{{Tenant: values.TenantId("acme-corp"), ID: "org-a"}},
		mandatoryDenies: []string{"deny.rule"}, subjects: []values.EntityRef{subject},
		fields: map[FieldID]FieldRuling{
			FieldWorkerNumber:        {Effect: EffectAllow, RuleID: "allow"},
			FieldBaseSalary:          {Effect: EffectRedacted, RuleID: "redact", Obligations: []string{"logged"}},
			FieldCaseNotes:           {Effect: EffectDenied, RuleID: "deny", Reason: "restricted"},
			FieldMedicalAccomodation: {Effect: EffectWithheld, RuleID: "withhold", Reason: "hidden"},
		},
		policyVersion: PolicyVersion, inputsDigest: "digest", evidenceID: "evidence",
	}
}

func TestRepositoryScope_ValidateRejectsIncompleteEvidence(t *testing.T) {
	base := internalRepositoryScope()
	cases := []struct {
		name   string
		mutate func(*RepositoryScope)
	}{
		{"never evaluated", func(s *RepositoryScope) { s.evaluated = false }},
		{"invalid effect", func(s *RepositoryScope) { s.effect = EffectUnspecified }},
		{"missing rule id", func(s *RepositoryScope) { s.ruleID = "" }},
		{"denial missing reason", func(s *RepositoryScope) { s.effect = EffectDenied; s.reason = "" }},
		{"invalid tenant", func(s *RepositoryScope) { s.tenant = values.TenantId("") }},
		{"missing evaluated instant", func(s *RepositoryScope) { s.evaluatedAt = values.Instant{} }},
		{"missing policy evidence", func(s *RepositoryScope) { s.policyVersion = "" }},
		{"foreign subject", func(s *RepositoryScope) { s.subjects[0].Tenant = values.TenantId("vendor-corp") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := base
			mutated.subjects = append([]values.EntityRef(nil), base.subjects...)
			tc.mutate(&mutated)
			if err := mutated.Validate(); !errors.Is(err, ErrScopeRequired) {
				t.Fatalf("Validate() = %v, want ErrScopeRequired", err)
			}
		})
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid scope Validate() = %v", err)
	}
}

func TestRepositoryGate_QueryIntersectsScopeAndStoredFields(t *testing.T) {
	scope := internalRepositoryScope()
	subject := scope.subjects[0]
	foreign := values.EntityRef{Tenant: values.TenantId("vendor-corp"), Kind: values.Kind("worker"), Id: subject.Id}
	gate := NewRepositoryGate(map[values.EntityRef]map[FieldID]string{
		subject: {
			FieldWorkerNumber: "W-1",
			FieldBaseSalary:   "secret-salary",
		},
		foreign: {FieldWorkerNumber: "V-1"},
	})

	if !scope.Valid() || scope.Zero() || scope.Effect() != EffectAllow || scope.RuleID() == "" || scope.Reason() == "" || scope.Tenant() == "" || scope.Purpose() != "test" || !scope.EvaluatedAt().IsSet() {
		t.Fatalf("scope accessors do not expose the evaluated scope: %+v", scope)
	}
	if got := scope.FieldRuling("not-requested"); got.Effect != EffectDenied || got.Reason != "field_not_in_scope" {
		t.Fatalf("unknown FieldRuling = %+v, want closed deny", got)
	}
	fields := scope.Fields()
	fields[FieldWorkerNumber] = FieldRuling{Effect: EffectDenied, Reason: "forged"}
	if scope.FieldRuling(FieldWorkerNumber).Effect != EffectAllow {
		t.Fatal("Fields returned a map that widened the scope")
	}
	subjects := scope.AllowedSubjects()
	subjects[0] = foreign
	if scope.AllowedSubjects()[0] != subject {
		t.Fatal("AllowedSubjects returned a slice backed by scope state")
	}
	orgs := scope.Organizations()
	orgs[0].ID = "forged"
	if scope.Organizations()[0].ID != "org-a" {
		t.Fatal("Organizations returned a slice backed by scope state")
	}
	denies := scope.MandatoryDenies()
	denies[0] = "forged"
	if scope.MandatoryDenies()[0] != "deny.rule" {
		t.Fatal("MandatoryDenies returned a slice backed by scope state")
	}

	if !scope.AuthorizesRecord(subject, scope.EvaluatedAt()) || scope.AuthorizesRecord(foreign, scope.EvaluatedAt()) || scope.AuthorizesRecord(subject, values.NewInstant(time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC))) {
		t.Fatal("AuthorizesRecord did not enforce the subject and exact-time intersection")
	}
	projections, err := gate.Query(scope, []values.EntityRef{subject, foreign}, scope.EvaluatedAt())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(projections) != 1 || projections[0].Subject != subject || len(projections[0].Fields) != 2 {
		t.Fatalf("Query projections = %+v, want one projection with two disclosed fields", projections)
	}
	if projections[0].Fields[0].FieldID != FieldBaseSalary || projections[0].Fields[0].Value != RedactedPlaceholder || projections[0].Fields[1].FieldID != FieldWorkerNumber || projections[0].Fields[1].Value != "W-1" {
		t.Fatalf("sorted projected fields = %+v, want redacted salary then raw worker number", projections[0].Fields)
	}
}
