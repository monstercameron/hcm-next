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

func TestAttestationStore_ResponseAliasesCorrectionsAndHistory(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 128); err != nil {
		t.Fatalf("apply migrations through 00128: %v", err)
	}
	id := tenant(t, db, "attestation-response-history")
	key := uuidTenant(id)
	store := New(appConn(t, db))
	clock := trustattest.TrustedClockFunc(func() (trustattest.TrustedTime, error) {
		return trustattest.FixedTrustedTime(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)), nil
	})
	recorder, err := trustattest.NewRecorder(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	request := trustattest.ResponseRequest{Tenant: key, ResponseID: "response-history", StatementID: "statement-1", StatementVersion: 1, StatementDigest: "1111111111111111111111111111111111111111111111111111111111111111", BindingDigest: "2222222222222222222222222222222222222222222222222222222222222222", Status: trustattest.ResponseAccepted, EvidenceReceipt: "3333333333333333333333333333333333333333333333333333333333333333", IdempotencyKey: "history-1"}
	first, err := recorder.RecordResponse(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	correction, err := recorder.RecordResponse(context.Background(), trustattest.ResponseRequest{Tenant: key, ResponseID: request.ResponseID, StatementID: request.StatementID, StatementVersion: 1, StatementDigest: request.StatementDigest, BindingDigest: request.BindingDigest, Status: trustattest.ResponseRefused, Kind: trustattest.AssertionCorrection, Reason: "evidence corrected", EvidenceReceipt: request.EvidenceReceipt, IdempotencyKey: "history-2", CorrectsResponseID: request.ResponseID, Authority: "auditor"})
	if err != nil {
		t.Fatal(err)
	}
	if correction.Revision != 2 || correction.Kind != trustattest.AssertionCorrection {
		t.Fatalf("correction = %+v", correction)
	}
	history, err := store.ListResponseHistory(context.Background(), key, request.ResponseID)
	if err != nil || len(history) != 2 || history[0].Revision != 1 || history[1].Revision != 2 {
		t.Fatalf("history = %+v, err=%v", history, err)
	}
	loaded, err := store.LoadResponse(context.Background(), key, request.ResponseID, 2)
	if err != nil || loaded.Digest != correction.Digest {
		t.Fatalf("loaded correction = %+v, err=%v", loaded, err)
	}
	byKey, err := store.GetResponseByIdempotency(context.Background(), key, "history-1")
	if err != nil || byKey.Digest != first.Digest {
		t.Fatalf("idempotency lookup = %+v, err=%v", byKey, err)
	}
	replayed, err := store.SaveResponse(context.Background(), first)
	if err != nil || !replayed.Replayed || replayed.Digest != first.Digest {
		t.Fatalf("SaveResponse replay = %+v, err=%v", replayed, err)
	}
	if _, err := store.GetResponseByIdempotency(context.Background(), key, "missing"); !errors.Is(err, trustattest.ErrResponseNotFound) {
		t.Fatalf("missing idempotency response = %v", err)
	}
	if _, err := store.ListResponseHistory(context.Background(), key, "missing"); CodeOf(err) != CodeNotFound || !errors.Is(err, trustattest.ErrResponseNotFound) {
		t.Fatalf("missing response history = %v", err)
	}
}

func uuidTenant(id uuid.UUID) values.TenantId { return values.TenantId(id.String()) }
