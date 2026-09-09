package auditpack_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/auditpack"
)

func TestEncodeLineRoundTripsThroughResolveFromContent(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	runID := "run-encode-1"
	streamKey := auditpack.StreamKey(runID)

	line := func(seq int64, kind auditpack.TotalKind, amount string) evidence.Event {
		payload, err := auditpack.EncodeLine(runID, kind, decimal(t, amount), "USD")
		if err != nil {
			t.Fatalf("encode line %s: %v", kind, err)
		}
		return evidence.Event{
			Tenant: tenant, StreamKey: streamKey, Sequence: seq,
			SchemaRef: auditpack.LineSchemaRef, Payload: payload,
		}
	}
	content := evidence.Content{
		Tenant: tenant,
		Streams: []evidence.Stream{{StreamKey: streamKey, Events: []evidence.Event{
			line(1, auditpack.KindRegister, "1000.00"),
			line(2, auditpack.KindBankFile, "800.00"),
			line(3, auditpack.KindTaxLiability, "200.00"),
			line(4, auditpack.KindFilingAcknowledgment, "200.00"),
		}}},
	}

	totals, err := auditpack.ResolveFromContent(content, tenant, runID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for kind, want := range map[auditpack.TotalKind]string{
		auditpack.KindRegister: "1000.00", auditpack.KindBankFile: "800.00",
		auditpack.KindTaxLiability: "200.00", auditpack.KindFilingAcknowledgment: "200.00",
	} {
		got, ok := totals.Total(kind)
		if !ok || got.String() != want {
			t.Errorf("%s = %v/%v, want %s/true", kind, got, ok, want)
		}
	}
	if len(totals.Lines) != 4 {
		t.Fatalf("resolved %d contributing lines, want 4", len(totals.Lines))
	}
}

func TestResolveFromContentSumsMultipleLinesPerKind(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	runID := "run-sum-1"
	streamKey := auditpack.StreamKey(runID)

	mk := func(seq int64, kind auditpack.TotalKind, amount string) evidence.Event {
		payload, err := auditpack.EncodeLine(runID, kind, decimal(t, amount), "USD")
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		return evidence.Event{Tenant: tenant, StreamKey: streamKey, Sequence: seq, SchemaRef: auditpack.LineSchemaRef, Payload: payload}
	}
	content := evidence.Content{
		Tenant: tenant,
		Streams: []evidence.Stream{{StreamKey: streamKey, Events: []evidence.Event{
			mk(1, auditpack.KindRegister, "600.00"),
			mk(2, auditpack.KindRegister, "400.00"),
			mk(3, auditpack.KindBankFile, "800.00"),
			mk(4, auditpack.KindTaxLiability, "200.00"),
			mk(5, auditpack.KindFilingAcknowledgment, "200.00"),
		}}},
	}
	totals, err := auditpack.ResolveFromContent(content, tenant, runID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, ok := totals.Total(auditpack.KindRegister)
	if !ok || got.String() != "1000.00" {
		t.Fatalf("summed register total = %v/%v, want 1000.00/true", got, ok)
	}
}

func TestResolveFromContentIgnoresOtherSchemasAndOtherRuns(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	runID := "run-mine"
	streamKey := auditpack.StreamKey(runID)

	registerPayload, err := auditpack.EncodeLine(runID, auditpack.KindRegister, decimal(t, "1000.00"), "USD")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	otherRunPayload, err := auditpack.EncodeLine("run-theirs", auditpack.KindRegister, decimal(t, "5.00"), "USD")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	bankFilePayload, err := auditpack.EncodeLine(runID, auditpack.KindBankFile, decimal(t, "800.00"), "USD")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	taxPayload, err := auditpack.EncodeLine(runID, auditpack.KindTaxLiability, decimal(t, "200.00"), "USD")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	filingPayload, err := auditpack.EncodeLine(runID, auditpack.KindFilingAcknowledgment, decimal(t, "200.00"), "USD")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	content := evidence.Content{
		Tenant: tenant,
		Streams: []evidence.Stream{{StreamKey: streamKey, Events: []evidence.Event{
			{Tenant: tenant, StreamKey: streamKey, Sequence: 1, SchemaRef: "some.other.schema.v1", Payload: []byte("irrelevant")},
			{Tenant: tenant, StreamKey: streamKey, Sequence: 2, SchemaRef: auditpack.LineSchemaRef, Payload: otherRunPayload},
			{Tenant: tenant, StreamKey: streamKey, Sequence: 3, SchemaRef: auditpack.LineSchemaRef, Payload: registerPayload},
			{Tenant: tenant, StreamKey: streamKey, Sequence: 4, SchemaRef: auditpack.LineSchemaRef, Payload: bankFilePayload},
			{Tenant: tenant, StreamKey: streamKey, Sequence: 5, SchemaRef: auditpack.LineSchemaRef, Payload: taxPayload},
			{Tenant: tenant, StreamKey: streamKey, Sequence: 6, SchemaRef: auditpack.LineSchemaRef, Payload: filingPayload},
		}}},
	}
	totals, err := auditpack.ResolveFromContent(content, tenant, runID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, ok := totals.Total(auditpack.KindRegister)
	if !ok || got.String() != "1000.00" {
		t.Fatalf("register total = %v/%v, want 1000.00/true (the other run's line must not contribute)", got, ok)
	}
}

func TestResolveFromContentRefusesAMissingTotal(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	runID := "run-incomplete"
	streamKey := auditpack.StreamKey(runID)
	payload, err := auditpack.EncodeLine(runID, auditpack.KindRegister, decimal(t, "1000.00"), "USD")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	content := evidence.Content{
		Tenant: tenant,
		Streams: []evidence.Stream{{StreamKey: streamKey, Events: []evidence.Event{
			{Tenant: tenant, StreamKey: streamKey, Sequence: 1, SchemaRef: auditpack.LineSchemaRef, Payload: payload},
		}}},
	}
	_, err = auditpack.ResolveFromContent(content, tenant, runID)
	var missing auditpack.ErrMissingTotal
	if got, ok := err.(auditpack.ErrMissingTotal); !ok {
		t.Fatalf("resolving an incomplete run = %v, want ErrMissingTotal", err)
	} else {
		missing = got
	}
	if missing.Kind == auditpack.KindRegister {
		t.Fatalf("ErrMissingTotal named REGISTER, which was present: %+v", missing)
	}
}

func TestResolveFromContentRefusesATenantMismatch(t *testing.T) {
	t.Parallel()
	content := evidence.Content{Tenant: uuid.New()}
	_, err := auditpack.ResolveFromContent(content, uuid.New(), "run-1")
	if _, ok := err.(auditpack.ErrTenantLeak); !ok {
		t.Fatalf("resolving content for a different tenant = %v, want ErrTenantLeak", err)
	}
}

func TestResolveFromContentRefusesAForeignEventInsideAStream(t *testing.T) {
	t.Parallel()
	tenant, other := uuid.New(), uuid.New()
	runID := "run-leak"
	streamKey := auditpack.StreamKey(runID)
	payload, err := auditpack.EncodeLine(runID, auditpack.KindRegister, decimal(t, "1.00"), "USD")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	content := evidence.Content{
		Tenant: tenant,
		Streams: []evidence.Stream{{StreamKey: streamKey, Events: []evidence.Event{
			{Tenant: other, StreamKey: streamKey, Sequence: 1, SchemaRef: auditpack.LineSchemaRef, Payload: payload},
		}}},
	}
	_, err = auditpack.ResolveFromContent(content, tenant, runID)
	if _, ok := err.(auditpack.ErrTenantLeak); !ok {
		t.Fatalf("resolving a foreign event = %v, want ErrTenantLeak", err)
	}
}
