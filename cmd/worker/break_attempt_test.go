package main

// Break attempts against the worker sweep loop. A failure is a confirmed
// break, not a bad test. Unlike the other sweep tests, this file drives a
// real database-backed outbox Consumer through the sweep so the poison
// proof exercises the actual redelivery path, not a scripted fake.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/outbox"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

const breakWorkerSchemaRef = "hcmnext.events.v1.OutboxEvent@1"

// TestBreak_PoisonMessageAbandonedStopsSweep proves a poison message no
// longer spins the sweep forever. Formerly, a deterministically-failing
// message was re-polled hot on every pass — Fail returned it to PENDING
// available immediately, the sweep reported didWork and never slept, and
// nothing counted consecutive failures. Now the Consumer's max-attempts
// policy parks the message ABANDONED after its deliveries are exhausted,
// later sweeps find no work, and the loop idles as designed.
func TestBreak_PoisonMessageAbandonedStopsSweep(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-worker-break', 'Break', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "worker-break-"+tenant.String())
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.events.v1.OutboxEvent', 1,
			'hcmnext.events.v1.OutboxEvent', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, breakWorkerSchemaRef)

	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	rec, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant:         tenant,
		OutboxID:       uuid.New(),
		EffectIdentity: "worker-poison-effect",
		OrderingKey:    "worker:break",
		SchemaRef:      breakWorkerSchemaRef,
		Payload:        []byte("poison"),
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	disp := outbox.NewConsumer(db.Conn, outbox.WithMaxAttempts(3))
	tenants := fakeTenantLister{tenants: []uuid.UUID{tenant}}
	handlerCalls := 0
	alwaysFail := func(context.Context, outbox.Record) error {
		handlerCalls++
		return errors.New("deterministic handler failure")
	}

	const passes = 6
	idlePasses := 0
	for i := 0; i < passes; i++ {
		didWork, err := sweepWithHandler(ctx, discardLogger(), tenants, disp, alwaysFail)
		if err != nil {
			t.Fatalf("pass %d: sweep: %v", i, err)
		}
		if !didWork {
			idlePasses++
		}
	}
	if handlerCalls != 3 {
		t.Fatalf("handler calls = %d, want 3: poison delivered exactly maxAttempts times, then parked", handlerCalls)
	}
	if idlePasses != passes-3 {
		t.Fatalf("idle passes = %d, want %d: sweeps after abandonment must find no work", idlePasses, passes-3)
	}
	parked, err := outbox.Read(ctx, db.Conn, tenant, rec.OutboxID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if parked.Status != outbox.StatusAbandoned {
		t.Fatalf("status = %q, want ABANDONED", parked.Status)
	}
}
