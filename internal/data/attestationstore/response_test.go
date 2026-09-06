package attestationstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	trustattest "github.com/monstercameron/hcm-next/internal/trust/attest"
)

func TestTodo_ATTEST_004_DurableResponses(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 128); err != nil {
		t.Fatalf("apply migrations through 00128: %v", err)
	}
	id := tenant(t, db, "attestation-response-durable")
	conn := appConn(t, db)
	key := uuidTenant(id)
	store := New(conn)
	clock := trustattest.TrustedClockFunc(func() (trustattest.TrustedTime, error) {
		return trustattest.FixedTrustedTime(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)), nil
	})
	recorder, err := trustattest.NewRecorder(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	response, err := recorder.RecordResponse(context.Background(), trustattest.ResponseRequest{Tenant: key, ResponseID: "response-1", StatementID: "statement-1", StatementVersion: 1, StatementDigest: "1111111111111111111111111111111111111111111111111111111111111111", BindingDigest: "2222222222222222222222222222222222222222222222222222222222222222", Status: trustattest.ResponseAccepted, EvidenceReceipt: "3333333333333333333333333333333333333333333333333333333333333333", IdempotencyKey: "idem-1"})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := recorder.RecordResponse(context.Background(), trustattest.ResponseRequest{Tenant: key, ResponseID: "response-1", StatementID: "statement-1", StatementVersion: 1, StatementDigest: "1111111111111111111111111111111111111111111111111111111111111111", BindingDigest: "2222222222222222222222222222222222222222222222222222222222222222", Status: trustattest.ResponseAccepted, EvidenceReceipt: "3333333333333333333333333333333333333333333333333333333333333333", IdempotencyKey: "idem-1"})
	if err != nil || !replay.Replayed || replay.Digest != response.Digest {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	loaded, err := store.GetResponse(context.Background(), key, "response-1", 1)
	if err != nil || loaded.Digest != response.Digest || loaded.RecordedAt.EvidenceID == "" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if _, err := store.GetResponse(context.Background(), key, "response-1", 2); !errors.Is(err, trustattest.ErrResponseNotFound) {
		t.Fatalf("unexpected second row: %v", err)
	}
	if err := db.ExecErr(`UPDATE attestation_response SET status='REFUSED' WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("append-only response accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM attestation_response WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("append-only response accepted DELETE")
	}
}

func uuidTenant(id uuid.UUID) values.TenantId { return values.TenantId(id.String()) }
