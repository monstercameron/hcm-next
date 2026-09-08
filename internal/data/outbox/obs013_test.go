package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/outbox"
)

func TestTodo_OBS_013_DurableCausalMetadataRoundTripReplayAndTenantIsolation(t *testing.T) {
	f := newEvent001Fixture(t)
	ctx := context.Background()
	causal := &outbox.CausalMetadata{
		CorrelationID: "corr-013", CausationID: "cause-013",
		LogicalOperationID: "logical-013", AttemptID: "enqueue-013",
		TraceLink: &outbox.TraceLink{
			TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7",
			TraceFlags: 1, TraceState: "", ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond),
		},
	}
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant: f.tenant, OutboxID: uuid.New(), EffectIdentity: "obs-013-effect",
		OrderingKey: "obs-013", SchemaRef: event001SchemaRef, Payload: []byte("same"), Causal: causal,
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if first.Causal == nil || first.Causal.LogicalOperationID != causal.LogicalOperationID || first.Causal.TraceLink == nil {
		t.Fatalf("round-trip causal metadata = %#v", first.Causal)
	}
	read, err := outbox.Read(ctx, f.db.Conn, f.tenant, first.OutboxID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Causal == nil || read.Causal.CorrelationID != "corr-013" {
		t.Fatalf("read causal = %#v", read.Causal)
	}

	tx, err = f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replay := *causal
	replay.AttemptID = "different-enqueue-attempt"
	got, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{Tenant: f.tenant, OutboxID: uuid.New(), EffectIdentity: "obs-013-effect", OrderingKey: "obs-013", SchemaRef: event001SchemaRef, Payload: []byte("same"), Causal: &replay})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got.OutboxID != first.OutboxID {
		t.Fatalf("replay id = %s, want %s", got.OutboxID, first.OutboxID)
	}

	now := time.Now().UTC().Add(time.Minute)
	consumer := outbox.NewConsumer(f.db.Conn, outbox.WithClock(func() time.Time { return now }))
	claimed, err := consumer.Poll(ctx, f.tenant)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	if claimed[0].Causal == nil || claimed[0].Causal.LogicalOperationID != "logical-013" || claimed[0].Causal.AttemptID == "enqueue-013" {
		t.Fatalf("claim attempt metadata = %#v", claimed[0].Causal)
	}
	firstAttempt := claimed[0].Causal.AttemptID
	if err := consumer.FailClaim(ctx, claimed[0], errors.New("retry")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	redelivered, err := consumer.Poll(ctx, f.tenant)
	if err != nil || len(redelivered) != 1 {
		t.Fatalf("redelivery = %#v, %v", redelivered, err)
	}
	if redelivered[0].Causal == nil || redelivered[0].Causal.LogicalOperationID != "logical-013" || redelivered[0].Causal.CausationID != "cause-013" || redelivered[0].Causal.AttemptID == firstAttempt {
		t.Fatalf("redelivery causal metadata = %#v", redelivered[0].Causal)
	}
	redeliveryLink := redelivered[0].Causal.TraceLink
	if redeliveryLink == nil || redeliveryLink.TraceID != causal.TraceLink.TraceID || redeliveryLink.SpanID != causal.TraceLink.SpanID || redeliveryLink.TraceFlags != causal.TraceLink.TraceFlags || redeliveryLink.TraceState != causal.TraceLink.TraceState || !redeliveryLink.ExpiresAt.Equal(causal.TraceLink.ExpiresAt) {
		t.Fatalf("redelivery trace link = %#v", redelivered[0].Causal.TraceLink)
	}
	if _, err := outbox.Read(ctx, f.db.Conn, uuid.New(), first.OutboxID); err == nil {
		t.Fatal("cross-tenant read unexpectedly succeeded")
	}
	for name, sql := range map[string]string{
		"partial":   `UPDATE outbox SET trace_id = NULL WHERE tenant_id = $1 AND outbox_id = $2`,
		"malformed": `UPDATE outbox SET trace_id = '00000000000000000000000000000000' WHERE tenant_id = $1 AND outbox_id = $2`,
	} {
		t.Run("database rejects "+name+" trace metadata", func(t *testing.T) {
			tx, err := f.db.Conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, sql, f.tenant, first.OutboxID); err == nil {
				_ = tx.Rollback(ctx)
				t.Fatal("database accepted invalid trace metadata")
			}
			_ = tx.Rollback(ctx)
		})
	}
}

func TestTodo_OBS_013_InvalidTraceMetadataIsDiagnosticOnly(t *testing.T) {
	f := newEvent001Fixture(t)
	ctx := context.Background()
	for i, link := range []*outbox.TraceLink{
		{TraceID: "bad", SpanID: "bad"},
		{TraceID: strings.Repeat("0", 32), SpanID: "00f067aa0ba902b7"},
		{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: strings.Repeat("0", 16)},
		{TraceID: "4BF92F3577B34DA6A3CE929D0E0E4736", SpanID: "00f067aa0ba902b7"},
		{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", TraceState: "invalid"},
	} {
		tx, err := f.db.Conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rec, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{Tenant: f.tenant, EffectIdentity: fmt.Sprintf("obs-013-invalid-trace-%d", i), OrderingKey: "obs-013", SchemaRef: event001SchemaRef, Payload: []byte("business"), Causal: &outbox.CausalMetadata{CorrelationID: "c", CausationID: "x", LogicalOperationID: "o", AttemptID: "a", TraceLink: link}})
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if rec.Causal == nil || rec.Causal.TraceLink != nil {
			t.Fatalf("invalid trace %d was retained: %#v", i, rec.Causal)
		}
	}
}
