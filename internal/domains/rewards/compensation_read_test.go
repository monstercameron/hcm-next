package rewards

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type memoryCompensationFacts struct {
	set     CompensationFactSet
	queries []CompensationFactsQuery
}

func (m *memoryCompensationFacts) CompensationFactsAt(_ context.Context, q CompensationFactsQuery) (CompensationFactSet, error) {
	m.queries = append(m.queries, q)
	return m.set, nil
}

func compensationTestRef(t *testing.T) values.EntityRef {
	t.Helper()
	return values.EntityRef{Tenant: "tenant-a", Kind: people.KindWorker, Id: "00000000-0000-4000-8000-000000000001"}
}

func compensationTestFact(t *testing.T) CompensationFact {
	t.Helper()
	instant := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	effective, err := values.NewOpenInstantInterval(instant)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("compensation", 7)
	if err != nil {
		t.Fatal(err)
	}
	base, err := values.NewMoney("125000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	bonus, err := values.NewMoney("10000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return CompensationFact{
		Worker: compensationTestRef(t), BasePay: base, PayBandRef: "band-p3", Currency: "USD",
		PayBasis: PayBasisAnnualSalary, Frequency: "ANNUAL", Components: []CompensationComponent{{ID: "bonus-target", Kind: "BONUS_TARGET", Amount: bonus}},
		Effective: effective, KnownAt: known, Revision: revision,
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityExternalObservation, System: "incumbent.hr", PolicyRef: "compensation.authority/v1"},
		Provenance: evidence.Provenance{Source: "incumbent.hr", EvidenceRef: "evidence-comp-1", RecordedAt: recorded},
	}
}

func compensationAuth(scoped bool) CompensationAuthorization {
	a := CompensationAuthorization{PolicyVersion: "authz/v1", Purpose: "compensation-review", SubjectDisclosable: true}
	if scoped {
		a.Scopes = []string{CompensationReadScope}
	}
	return a
}

func compensationRequest(t *testing.T, auth CompensationAuthorization) CompensationReadRequest {
	t.Helper()
	return CompensationReadRequest{Tenant: "tenant-a", Worker: compensationTestRef(t), AsOf: values.NewInstant(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)), Authorization: auth}
}

func compensationSet(t *testing.T) CompensationFactSet {
	revision, err := values.NewSequenceRevision("compensation", 7)
	if err != nil {
		t.Fatal(err)
	}
	fact := compensationTestFact(t)
	return CompensationFactSet{Worker: fact.Worker, Exists: true, Fact: fact, Watermark: revision, PolicyVersion: "compensation-policy/v1"}
}

// TestTodo_COMP_001 is the primary COMP-001 contract test.
func TestTodo_COMP_001(t *testing.T) {
	reader := &memoryCompensationFacts{set: compensationSet(t)}
	result, err := ReadCompensation(context.Background(), reader, compensationRequest(t, compensationAuth(true)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disclosure != people.DisclosureFull || result.BasePay.Access != people.AccessAuthorized {
		t.Fatalf("result = %+v", result)
	}
	if got, ok := result.Value(FieldBasePay); !ok || got != "125000.00 USD" {
		t.Fatalf("base pay = %q, %v", got, ok)
	}
	if len(reader.queries) != 1 || len(reader.queries[0].Fields) != len(compensationFields) {
		t.Fatalf("queries = %+v", reader.queries)
	}
	if result.Watermark.String() == "" || result.PolicyVersion == "" || result.Receipt.Counters.IsZero() == false {
		t.Fatalf("missing evidence = %+v", result)
	}
}

func TestTodo_COMP_001_Property(t *testing.T) {
	auth := compensationAuth(true)
	auth.Fields = map[CompensationField]CompensationFieldRuling{}
	for _, field := range compensationFields {
		auth.Fields[field] = CompensationFieldRuling{Effect: people.EffectAllow}
	}
	auth.Fields[FieldBasePay] = CompensationFieldRuling{Effect: people.EffectDeny, Reason: "scope.base_pay.absent"}
	reader := &memoryCompensationFacts{set: compensationSet(t)}
	result, err := ReadCompensation(context.Background(), reader, compensationRequest(t, auth))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disclosure != people.DisclosurePartial || result.BasePay.Access != people.AccessDenied {
		t.Fatalf("denied result = %+v", result)
	}
	if _, ok := result.Value(FieldBasePay); ok || result.BasePay.Value.State() != values.PresenceRedacted {
		t.Fatalf("denied base pay leaked: %+v", result.BasePay)
	}
	for _, field := range reader.queries[0].Fields {
		if field == FieldBasePay {
			t.Fatal("denied base pay was requested from the facts port")
		}
	}
}

func TestTodo_COMP_001_Security(t *testing.T) {
	reader := &memoryCompensationFacts{set: compensationSet(t)}
	result, err := ReadCompensation(context.Background(), reader, compensationRequest(t, compensationAuth(false)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disclosure != people.DisclosureWithheld || result.WithheldReason != "missing_scope:compensation.read" || len(reader.queries) != 0 {
		t.Fatalf("missing-scope result = %+v queries=%d", result, len(reader.queries))
	}
	if _, ok := result.Value(FieldBasePay); ok || result.BasePay.Value.State() != values.PresenceUnspecified {
		t.Fatalf("missing scope exposed base pay: %+v", result.BasePay)
	}
	explanation := result.Explain()
	if strings.Contains(strings.ToLower(explanation.WithheldReason), "125000") || len(explanation.Fields) != 0 {
		t.Fatalf("explanation leaked amount or fields: %+v", explanation)
	}
}

func TestTodo_COMP_001_Mutation(t *testing.T) {
	reader := &memoryCompensationFacts{set: compensationSet(t)}
	auth := compensationAuth(true)
	auth.Fields = map[CompensationField]CompensationFieldRuling{}
	for _, field := range compensationFields {
		auth.Fields[field] = CompensationFieldRuling{Effect: people.EffectAllow}
	}
	auth.Fields[FieldComponents] = CompensationFieldRuling{Effect: people.EffectDeny, Reason: "scope.components.absent"}
	result, err := ReadAuthorizedCompensation(context.Background(), reader, compensationRequest(t, auth))
	if err != nil {
		t.Fatal(err)
	}
	if result.Components.Access != people.AccessDenied || len(result.Components.Value) != 0 {
		t.Fatalf("component disclosure = %+v", result.Components)
	}
	if strings.Contains(string(result.Explain().Inputs[0]), "amount") {
		t.Fatal("explanation named a protected amount")
	}
}
