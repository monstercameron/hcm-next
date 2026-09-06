package stepup_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/trust/stepup"
)

// TestMain starts the shared test PostgreSQL server for this package.
func TestMain(m *testing.M) { pgtest.RunMain(m) }

const proofLogDDL = `
-- Migration 00126 now owns the durable stepup_proof_log (internal/data/truststore);
-- this legacy sqlstore test keeps its private minimal shape inside the test schema.
DROP TABLE IF EXISTS stepup_proof_log CASCADE;
CREATE TABLE stepup_proof_log (
  proof_id    text primary key,
  outcome     text   not null,
  consumed_at timestamptz not null
);`

// TestTodo_AUTHN_005_Integration is the INTEGRATION clause: the
// single-use guarantee holds across separate gate instances sharing one
// consumption store on the real database, and the proof survives the wire
// form round trip unchanged.
func TestTodo_AUTHN_005_Integration(t *testing.T) {
	db := pgtest.New(t)
	db.Exec(t, proofLogDDL)

	fx := newFixture(t)
	p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// The proof must survive its wire form: encode, decode, and the decoded
	// proof must verify against the original's signature.
	enc, err := p.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	dec, err := stepup.DecodeProof(enc)
	if err != nil {
		t.Fatalf("DecodeProof: %v", err)
	}
	if dec.Digest() != p.Digest() {
		t.Fatalf("wire round trip changed the proof: %s vs %s", dec.Digest(), p.Digest())
	}

	store := stepup.NewSQLProofStore(db.SQL)
	gate1 := stepup.NewGate(fx.key, store, fx.sessions, fx.exec.run, func() time.Time { return baseTime })
	gate2 := stepup.NewGate(fx.key, stepup.NewSQLProofStore(db.SQL), fx.sessions, fx.exec.run, func() time.Time { return baseTime })

	out, err := gate1.Present(context.Background(), dec, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
	if err != nil {
		t.Fatalf("gate1 Present: %v", err)
	}
	if out != stepup.OutcomeExecuted {
		t.Fatalf("gate1 outcome %s, want executed", out)
	}

	// The consumption is visible to the database itself, not just the gate.
	var count int
	var outcome sql.NullString
	if err := db.SQL.QueryRowContext(context.Background(), `SELECT count(*), min(outcome) FROM stepup_proof_log WHERE proof_id = $1`, dec.ID).
		Scan(&count, &outcome); err != nil {
		t.Fatalf("read consumption log: %v", err)
	}
	if count != 1 || !outcome.Valid || outcome.String != string(stepup.OutcomeExecuted) {
		t.Fatalf("consumption log holds %d row(s) with outcome %q", count, outcome.String)
	}

	// A second gate on a second handle over the same store must see the
	// same consumption.
	out, err = gate2.Present(context.Background(), dec, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
	if err != nil {
		t.Fatalf("gate2 Present: %v", err)
	}
	if out != stepup.OutcomeReplayed {
		t.Fatalf("gate2 outcome %s, want replayed", out)
	}
	if fx.exec.count() != 1 {
		t.Fatalf("the operation ran %d times across two gate instances", fx.exec.count())
	}

	// Direct store access reports the same state as the gates.
	consumed, err := store.Consume(context.Background(), dec.ID, string(stepup.OutcomeReplayed))
	if err != nil {
		t.Fatalf("store Consume: %v", err)
	}
	if !consumed {
		t.Fatal("the store does not report an already-consumed proof as consumed")
	}
}

// TestTodo_AUTHN_005_Recovery_SharedAuthority is the RECOVERY clause over the
// shared store: an ambiguous commit from one gate instance never leads to a
// second execution when the recovery gate reads the same store, and the
// store continues to serve fresh proofs afterwards.
func TestTodo_AUTHN_005_Recovery_SharedAuthority(t *testing.T) {
	db := pgtest.New(t)
	db.Exec(t, proofLogDDL)

	fx := newFixture(t)
	p, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionApprove), fx.req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	inner := stepup.NewSQLProofStore(db.SQL)
	flaky := &flakyStore{inner: inner}
	gate1 := stepup.NewGate(fx.key, flaky, fx.sessions, fx.exec.run, func() time.Time { return baseTime })

	out, err := gate1.Present(context.Background(), p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
	if err != nil {
		t.Fatalf("ambiguous Present: %v", err)
	}
	if out != stepup.OutcomeUnknownCommit {
		t.Fatalf("ambiguous commit reported %s, want unknown_commit", out)
	}
	if fx.exec.count() != 0 {
		t.Fatalf("the operation ran %d times on an ambiguous commit", fx.exec.count())
	}

	// The recovery gate over the same database converges: the row the first
	// gate committed is visible, so the proof is replayed, never executed.
	gate2 := stepup.NewGate(fx.key, stepup.NewSQLProofStore(db.SQL), fx.sessions, fx.exec.run, func() time.Time { return baseTime })
	out, err = gate2.Present(context.Background(), p, opFor(stepup.ActionApprove), highPrincipal(t), fx.req)
	if err != nil {
		t.Fatalf("recovery Present: %v", err)
	}
	if out != stepup.OutcomeReplayed {
		t.Fatalf("recovery gate reported %s, want replayed", out)
	}
	if fx.exec.count() != 0 {
		t.Fatalf("recovery executed the operation: %d calls", fx.exec.count())
	}

	// The store continues to serve: a fresh proof for a fresh operation
	// executes normally after the ambiguous event.
	q, err := fx.issuer.Issue(highPrincipal(t), opFor(stepup.ActionExport), fx.req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// The second proof was issued by the same issuer at the same instant;
	// its content (action and scopes) differ, so its id and digest differ.
	if q.ID == p.ID {
		t.Fatalf("two distinct issues share a proof id: %s", q.ID)
	}
	out, err = gate2.Present(context.Background(), q, opFor(stepup.ActionExport), highPrincipal(t), fx.req)
	if err != nil {
		t.Fatalf("fresh Present after recovery: %v", err)
	}
	if out != stepup.OutcomeExecuted {
		t.Fatalf("fresh proof after recovery reported %s, want executed", out)
	}
	if fx.exec.count() != 1 {
		t.Fatalf("the fresh operation ran %d times, want one", fx.exec.count())
	}
}
