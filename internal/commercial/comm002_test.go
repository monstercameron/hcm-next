package commercial_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/commercial"
)

func invoiceRequest(artifact string) commercial.InvoiceEvidenceRequest {
	return commercial.InvoiceEvidenceRequest{
		TenantID:          "tenant-a",
		ExternalInvoiceID: "manual-invoice-001",
		AmountCents:       2500000,
		Currency:          "USD",
		ServiceFrom:       pilotAt,
		ServiceTo:         pilotAt.Add(90 * 24 * time.Hour),
		IssuedAt:          pilotAt.Add(24 * time.Hour),
		DueAt:             pilotAt.Add(54 * 24 * time.Hour),
		Status:            commercial.InvoiceIssued,
		Reconciliation:    commercial.ReconciliationReconciled,
		Artifact:          []byte(artifact),
		RecordedAt:        pilotAt.Add(25 * time.Hour),
	}
}

func TestTodo_COMM_002(t *testing.T) {
	snapshot, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "tenant-a", ContractID: "pilot-a", Revision: 1,
		EffectiveFrom: pilotAt, EffectiveTo: pilotAt.Add(90 * 24 * time.Hour),
		Capabilities: []string{commercial.PromotionEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 25}, PriceCents: 2500000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := commercial.NewInvoiceEvidenceStore()
	got, created, err := store.Record(snapshot, invoiceRequest("invoice-pdf-placeholder"))
	if err != nil {
		t.Fatal(err)
	}
	if !created || got.ContractFingerprint != snapshot.Fingerprint() || got.AmountCents != 2500000 || got.Currency != "USD" || got.ArtifactHash == "" {
		t.Fatalf("evidence=%+v created=%v", got, created)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Record(snapshot, func() commercial.InvoiceEvidenceRequest {
		request := invoiceRequest("different-placeholder")
		request.ExternalInvoiceID = "manual-invoice-002"
		request.AmountCents++
		return request
	}()); !errors.Is(err, commercial.ErrInvoiceAmountMismatch) {
		t.Fatalf("amount mismatch error=%v", err)
	}
}

func TestTodo_COMM_002_Security(t *testing.T) {
	snapshot, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "tenant-a", ContractID: "pilot-a", Revision: 1,
		EffectiveFrom: pilotAt, EffectiveTo: pilotAt.Add(90 * 24 * time.Hour),
		Capabilities: []string{commercial.PromotionEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 25}, PriceCents: 2500000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := commercial.NewInvoiceEvidenceStore()
	request := invoiceRequest("tenant-bound-artifact-placeholder")
	got, _, err := store.Record(snapshot, request)
	if err != nil {
		t.Fatal(err)
	}
	if !commercial.VerifyArtifactHash("tenant-a", request.Artifact, got.ArtifactHash) {
		t.Fatal("artifact hash did not verify for owning tenant")
	}
	if commercial.VerifyArtifactHash("tenant-b", request.Artifact, got.ArtifactHash) {
		t.Fatal("artifact hash verified for another tenant")
	}
	if _, _, err := store.Record(snapshot, func() commercial.InvoiceEvidenceRequest {
		wrongTenant := request
		wrongTenant.TenantID = "tenant-b"
		return wrongTenant
	}()); !errors.Is(err, commercial.ErrInvoiceTenantMismatch) {
		t.Fatalf("tenant mismatch error=%v", err)
	}
	if records := store.List("tenant-b"); len(records) != 0 {
		t.Fatalf("cross-tenant list returned %d records", len(records))
	}
}

func TestTodo_COMM_002_Recovery(t *testing.T) {
	snapshot, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "tenant-a", ContractID: "pilot-a", Revision: 1,
		EffectiveFrom: pilotAt, EffectiveTo: pilotAt.Add(90 * 24 * time.Hour),
		Capabilities: []string{commercial.PromotionEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 25}, PriceCents: 2500000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := commercial.NewInvoiceEvidenceStore()
	first, created, err := store.Record(snapshot, invoiceRequest("recovery-artifact-placeholder"))
	if err != nil || !created {
		t.Fatalf("first record=%+v created=%v err=%v", first, created, err)
	}
	replay := invoiceRequest("a-retried-artifact-with-no-history-mutation")
	replay.IssuedAt = replay.IssuedAt.Add(time.Hour)
	replayed, created, err := store.Record(snapshot, replay)
	if err != nil {
		t.Fatal(err)
	}
	if created || replayed != first {
		t.Fatalf("replay=%+v created=%v, want original immutable record", replayed, created)
	}
	if records := store.List("tenant-a"); len(records) != 1 || records[0] != first {
		t.Fatalf("history=%+v, want one original record", records)
	}
}
