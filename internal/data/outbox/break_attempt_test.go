package outbox_test

// Break attempts against the transactional outbox (DATA-007/DATA-008).
// Each test asserts the DESIRED contract; a failure is a confirmed break,
// not a bad test. Do not "fix" these by weakening the assertion: fix the
// code, or record a deliberate design decision in the test.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
)

func breakEnqueue(t *testing.T, f event001Fixture, effect string, causal *outbox.CausalMetadata) outbox.Record {
	t.Helper()
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	rec, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant:         f.tenant,
		OutboxID:       uuid.New(),
		EffectIdentity: effect,
		OrderingKey:    "break",
		SchemaRef:      event001SchemaRef,
		Payload:        []byte("break:" + effect),
		Causal:         causal,
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("enqueue %s: %v", effect, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit %s: %v", effect, err)
	}
	return rec
}

func breakPollOne(t *testing.T, c *outbox.Consumer, tenant uuid.UUID) outbox.Record {
	t.Helper()
	batch, err := c.Poll(context.Background(), tenant)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(batch) != 1 {
		t.Fatalf("poll returned %d records, want exactly 1", len(batch))
	}
	return batch[0]
}

// TestBreak_PoisonMessageRedeliversImmediately proves there is no backoff:
// a message whose handler deterministically fails is PENDING and available
// again the instant Fail commits, so the next Poll reclaims it with zero
// delay. A poison message therefore spins the sweep at full speed
// (sweep reports didWork and never sleeps while any message is returned).
func TestBreak_PoisonMessageRedeliversImmediately(t *testing.T) {
	f := newEvent001Fixture(t)
	ctx := context.Background()
	rec := breakEnqueue(t, f, "poison-effect", nil)
	c := outbox.NewConsumer(f.db.Conn)

	const cycles = 3
	for i := 0; i < cycles; i++ {
		got := breakPollOne(t, c, f.tenant)
		if got.OutboxID != rec.OutboxID {
			t.Fatalf("cycle %d: polled %s, want poison message %s", i, got.OutboxID, rec.OutboxID)
		}
		if err := c.Fail(ctx, f.tenant, got.OutboxID, errors.New("deterministic handler failure")); err != nil {
			t.Fatalf("cycle %d: fail: %v", i, err)
		}
	}
	reread, err := outbox.Read(ctx, f.db.Conn, f.tenant, rec.OutboxID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if reread.Attempts != cycles {
		t.Fatalf("attempts = %d, want %d (one claim per cycle)", reread.Attempts, cycles)
	}
	if reread.Status != outbox.StatusPending {
		t.Fatalf("status = %q, want PENDING and immediately reclaimable", reread.Status)
	}
	// Characterization (passes): Fail documents "available immediately",
	// so a poison message is re-polled with zero delay. The missing
	// backoff/abandonment mechanism is proven at the sweep level instead
	// (cmd/worker TestBreak_PoisonMessageSpinsSweepWithoutSleep).
	again := breakPollOne(t, c, f.tenant)
	if again.OutboxID != rec.OutboxID {
		t.Fatalf("immediate re-poll = %s, want poison message %s", again.OutboxID, rec.OutboxID)
	}
}

// TestBreak_MaxAttemptsAbandonsPoison proves a poison message stops
// spinning: with WithMaxAttempts(3) the third failed delivery parks the
// message ABANDONED — with its last error as evidence — instead of
// returning it to PENDING, and later polls never see it again.
func TestBreak_MaxAttemptsAbandonsPoison(t *testing.T) {
	f := newEvent001Fixture(t)
	ctx := context.Background()
	rec := breakEnqueue(t, f, "abandon-effect", nil)
	c := outbox.NewConsumer(f.db.Conn, outbox.WithMaxAttempts(3))

	for i := 0; i < 3; i++ {
		got := breakPollOne(t, c, f.tenant)
		if got.OutboxID != rec.OutboxID {
			t.Fatalf("delivery %d: polled %s, want %s", i+1, got.OutboxID, rec.OutboxID)
		}
		if err := c.Fail(ctx, f.tenant, got.OutboxID, errors.New("still poison")); err != nil {
			t.Fatalf("delivery %d: fail: %v", i+1, err)
		}
	}
	parked, err := outbox.Read(ctx, f.db.Conn, f.tenant, rec.OutboxID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if parked.Status != outbox.StatusAbandoned {
		t.Fatalf("status = %q, want ABANDONED after 3 failed deliveries", parked.Status)
	}
	if parked.LastError == nil || *parked.LastError != "still poison" {
		t.Fatalf("last error = %+v, want the final failure as evidence", parked.LastError)
	}
	empty, err := c.Poll(ctx, f.tenant)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("poll returned %d records, want none (abandoned rows are never re-polled)", len(empty))
	}
}

// TestBreak_RunHaltsWhenLeaseExpiresMidHandler proves one slow dispatch no
// longer halts the whole consumer loop. Formerly, a handler that outran
// its own lease made AckLease hit the lease fence and Run RETURNED that
// error, stopping all dispatch for the tenant. Now the loop settles an
// expired-but-still-owned lease under its token fence and continues: both
// messages are delivered and Run never reports ErrLeaseFence.
func TestBreak_RunHaltsWhenLeaseExpiresMidHandler(t *testing.T) {
	f := newEvent001Fixture(t)
	ctx := context.Background()
	first := breakEnqueue(t, f, "slow-effect", nil)
	second := breakEnqueue(t, f, "second-effect", nil)

	c := outbox.NewConsumer(f.db.Conn, outbox.WithLease(100*time.Millisecond))
	handlerCalls := 0
	handler := func(ctx context.Context, msg outbox.Record) error {
		handlerCalls++
		time.Sleep(300 * time.Millisecond) // outruns the 100ms lease
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- c.Run(runCtx, f.tenant, handler, 20*time.Millisecond) }()

	delivered := func(id uuid.UUID) bool {
		rec, err := outbox.Read(ctx, f.db.Conn, f.tenant, id)
		if err != nil {
			return false
		}
		return rec.Status == outbox.StatusDelivered
	}
	deadline := time.Now().Add(15 * time.Second)
	for !(delivered(first.OutboxID) && delivered(second.OutboxID)) {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("timed out waiting for both deliveries (handler calls = %d)", handlerCalls)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-runErr; errors.Is(err, outbox.ErrLeaseFence) {
		t.Fatalf("Run = %v; a slow handler must not halt dispatch for the tenant", err)
	}
	if handlerCalls != 2 {
		t.Fatalf("handler calls = %d, want exactly 2 (each message delivered once, no hot redelivery)", handlerCalls)
	}
}

// TestBreak_UnfencedAckStealsForeignClaim pins the ownership boundary of
// the ID-only settle primitives. A token-less Ack cannot tell owner from
// thief on a LIVE claim, so plain Ack settles any live claim it names —
// concurrent-worker call sites must use the token-fenced AckClaim instead,
// and ID-only Ack/Fail are operator-grade repair primitives. What IS
// enforced: settling anything but a live claim is ErrLeaseFence, and a
// stolen row still defeats the victim's fenced acknowledgement.
func TestBreak_UnfencedAckStealsForeignClaim(t *testing.T) {
	f := newEvent001Fixture(t)
	ctx := context.Background()
	rec := breakEnqueue(t, f, "steal-effect", nil)
	owner := outbox.NewConsumer(f.db.Conn)
	thief := outbox.NewConsumer(f.db.Conn)

	claimed := breakPollOne(t, owner, f.tenant)
	if claimed.LeaseToken == uuid.Nil {
		t.Fatal("claimed record carries no lease token")
	}
	// Accepted residual, documented: a token-less Ack settles the live
	// foreign claim. Owner and thief are indistinguishable without the
	// lease token, so worker paths must use AckClaim.
	if err := thief.Ack(ctx, f.tenant, rec.OutboxID); err != nil {
		t.Fatalf("live-claim Ack = %v; ID-only settle applies to any live claim", err)
	}
	stolen, err := outbox.Read(ctx, f.db.Conn, f.tenant, rec.OutboxID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if stolen.Status != outbox.StatusDelivered {
		t.Fatalf("status after foreign Ack = %q, want DELIVERED", stolen.Status)
	}
	// The victim's fenced acknowledgement still fails against the stolen row.
	if err := owner.AckClaim(ctx, claimed); !errors.Is(err, outbox.ErrLeaseFence) {
		t.Fatalf("victim AckClaim = %v, want ErrLeaseFence", err)
	}
	// And the thief cannot settle twice: the row is no longer a live claim.
	if err := thief.Ack(ctx, f.tenant, rec.OutboxID); !errors.Is(err, outbox.ErrLeaseFence) {
		t.Fatalf("second Ack = %v, want ErrLeaseFence (row already settled)", err)
	}
}

// TestBreak_UnfencedSettleOnWrongStateIsSilentNoOp proves Ack and Fail
// report success when they changed nothing: Ack on a never-claimed PENDING
// message, and Fail on an already-DELIVERED one, both return nil while the
// row is untouched. Callers cannot distinguish "settled" from "no such
// claim".
func TestBreak_UnfencedSettleOnWrongStateIsSilentNoOp(t *testing.T) {
	f := newEvent001Fixture(t)
	ctx := context.Background()
	c := outbox.NewConsumer(f.db.Conn)

	rec := breakEnqueue(t, f, "noop-effect", nil)
	if err := c.Ack(ctx, f.tenant, rec.OutboxID); err == nil {
		t.Error("Ack on PENDING returned nil; want an error distinguishing no-op from settlement")
	}
	still, err := outbox.Read(ctx, f.db.Conn, f.tenant, rec.OutboxID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if still.Status != outbox.StatusPending {
		t.Fatalf("status = %q, want PENDING (Ack must not have applied)", still.Status)
	}

	claimed := breakPollOne(t, c, f.tenant)
	if err := c.AckClaim(ctx, claimed); err != nil {
		t.Fatalf("AckClaim: %v", err)
	}
	if err := c.Fail(ctx, f.tenant, rec.OutboxID, errors.New("too late")); err == nil {
		t.Fatal("Fail on DELIVERED returned nil; want an error distinguishing no-op from settlement")
	}
}

// TestBreak_ZeroExpiryTraceLinkRoundTrip characterizes zero-expiry storage
// (passes: verified resilient). Enqueue accepts ExpiresAt.IsZero(), pgx
// stores its zero-time encoding, and Read returns the identical zero
// expiry — the outbox round-trips expiry-less links faithfully. This is
// the control for jobs TestBreak_ExpiryLessTraceLinkEscapesValidation,
// where the same link detonates or vanishes.
func TestBreak_ZeroExpiryTraceLinkRoundTrip(t *testing.T) {
	f := newEvent001Fixture(t)
	causal := &outbox.CausalMetadata{
		CorrelationID: "corr-zero", CausationID: "cause-zero",
		LogicalOperationID: "logical-zero", AttemptID: "attempt-zero",
		TraceLink: &outbox.TraceLink{
			TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7",
			TraceFlags: 1,
		},
	}
	rec := breakEnqueue(t, f, "zero-expiry-effect", causal)
	read, err := outbox.Read(context.Background(), f.db.Conn, f.tenant, rec.OutboxID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read.Causal == nil || read.Causal.TraceLink == nil {
		t.Fatalf("read causal = %#v; want the stored trace link back", read.Causal)
	}
	if !read.Causal.TraceLink.ExpiresAt.IsZero() {
		t.Fatalf("read ExpiresAt = %v; want zero (stored link had no expiry)", read.Causal.TraceLink.ExpiresAt)
	}
}

// TestBreak_DuplicateOutboxIDCollides proves an explicit OutboxID that hits
// the primary key under a DIFFERENT effect identity escapes the typed
// contract: Enqueue must report ErrIdentityConflict, not a raw driver
// error, so callers can distinguish "replay" from "bug".
func TestBreak_DuplicateOutboxIDCollides(t *testing.T) {
	f := newEvent001Fixture(t)
	ctx := context.Background()
	id := uuid.New()

	enqueueWithID := func(id uuid.UUID, effect string) error {
		tx, err := f.db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		_, err = outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
			Tenant:         f.tenant,
			OutboxID:       id,
			EffectIdentity: effect,
			OrderingKey:    "break",
			SchemaRef:      event001SchemaRef,
			Payload:        []byte("break:" + effect),
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		return tx.Commit(ctx)
	}
	if err := enqueueWithID(id, "effect-a"); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	err := enqueueWithID(id, "effect-b")
	if !errors.Is(err, outbox.ErrIdentityConflict) {
		t.Fatalf("colliding OutboxID error = %v; want ErrIdentityConflict, not a raw driver error", err)
	}
}
