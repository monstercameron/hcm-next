package disposition_test

// LEDGER-011: apply retention, holds and crypto-erasure without falsifying
// chronology.
//
// This file proves the three numbered clauses from the todo directly:
//
//  1. distinguishability -- an event that never carried an inline payload
//     (StateReferenced) and one whose payload was erased (StatePayloadErased)
//     read as different, explicit states through the same disposition.ReadView
//     accessor. Neither is a nil-looking zero value.
//  2. chronology/digest survive -- every column of ledger_event's row is
//     compared, named explicitly, before and after erasure, and the real
//     internal/data/ledger/hashchain verifier reproduces the identical chain
//     head before and after.
//  3. hold blocks erasure -- an event under an ACTIVE legal hold refuses
//     Erase (composing recordsmeta.DisposeCopy, not reimplementing the
//     check), and the hold's own evidence (status, ACTIVE intersections)
//     remains exactly as it was after the refused attempt.
//
// TestMain and every shared fixture live in fixtures_test.go.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/disposition"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/recordsmeta"
)

// assertSameEventColumns compares every column ledger_event carries by name,
// so a mismatch names exactly which column changed rather than reporting an
// aggregate "the row differs".
func assertSameEventColumns(t *testing.T, before, after rawEventRow) {
	t.Helper()
	type column struct {
		name        string
		beforeValue any
		afterValue  any
	}
	columns := []column{
		{"tenant_id", before.Tenant, after.Tenant},
		{"stream_key", before.StreamKey, after.StreamKey},
		{"sequence", before.Sequence, after.Sequence},
		{"event_id", before.EventID, after.EventID},
		{"assertion_class", before.AssertionClass, after.AssertionClass},
		{"authority_ref", before.Authority, after.Authority},
		{"source_ref", before.SourceRef, after.SourceRef},
		{"schema_ref", before.SchemaRef, after.SchemaRef},
		{"payload", before.Payload, after.Payload},
		{"artifact_ref", before.ArtifactRef, after.ArtifactRef},
		{"canonical_length", before.CanonicalLength, after.CanonicalLength},
		{"digest", before.Digest, after.Digest},
		{"digest_algorithm", before.DigestAlgorithm, after.DigestAlgorithm},
		{"occurred_at", before.OccurredAt, after.OccurredAt},
		{"effective_at", before.EffectiveAt, after.EffectiveAt},
		{"recorded_at", before.RecordedAt, after.RecordedAt},
		{"correlation_id", before.CorrelationID, after.CorrelationID},
		{"causation_id", before.CausationID, after.CausationID},
		{"idempotency_key", before.IdempotencyKey, after.IdempotencyKey},
		{"corrects_stream_key", before.CorrectsStreamKey, after.CorrectsStreamKey},
		{"corrects_sequence", before.CorrectsSequence, after.CorrectsSequence},
	}
	changed := 0
	for _, c := range columns {
		if !reflect.DeepEqual(c.beforeValue, c.afterValue) {
			changed++
			t.Errorf("ledger_event.%s changed by disposition: before=%v after=%v", c.name, c.beforeValue, c.afterValue)
		}
	}
	if changed > 0 {
		t.Fatalf("%d ledger_event column(s) changed by disposition; a compliance erasure must touch none of them at the storage layer -- this package withholds access at the read layer instead", changed)
	}
}

func TestTodo_LEDGER_011(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "ledger011-primary")
	registerLedgerSchema(t, db, tenant)
	registerRecordsCopySchema(t, db, tenant)
	ensureStream(t, conn, tenant, streamKey)
	chainAppender := newChainAppender(t)
	chainDigester := newChainDigester(t)

	// Two events on the same stream: sequence 1 carries an inline payload
	// (the erasure target); sequence 2 references governed bytes and never
	// carried an inline payload at all -- the exact pair LEDGER-011's RED
	// names as indistinguishable through the raw reader once an erasure
	// exists ("redacted payload is returned as ordinary present value").
	receiptInline := appendWithChain(t, conn, tenant, chainAppender, appendRequest(tenant, streamKey, 0, []byte("promotion-proposed"), ""))
	receiptReferenced := appendWithChain(t, conn, tenant, chainAppender, appendRequest(tenant, streamKey, 1, nil, "artifact://doc/"+uuid.NewString()))

	beforeAnything := readRawEvent(t, db, tenant, streamKey, receiptInline.Sequence)

	// Register the inline event as a tracked CANONICAL copy so recordsmeta
	// has something to check (and later refuse) a hold against.
	declaration := newDeclaration(tenant)
	var link recordsmeta.CopyLink
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		var err error
		link, err = recordsmeta.RegisterCopy(ctx, tx, recordsmeta.CopyLink{
			TenantID: tenant, DeclarationID: declaration.DeclarationID, CopyType: "CANONICAL",
			StoreRef: "ledger:" + streamKey, LedgerStream: streamKey, LedgerSequence: receiptInline.Sequence,
		})
		return err
	})

	// --- baseline: before any disposition, both events read as ordinary
	// present/referenced states, never anything erasure-shaped.
	baselineInline := readView(t, conn, tenant, streamKey, receiptInline.Sequence)
	if baselineInline.State != disposition.StatePresent || string(baselineInline.Bytes) != "promotion-proposed" {
		t.Fatalf("baseline inline view = %+v, want StatePresent with the original bytes", baselineInline)
	}
	baselineReferenced := readView(t, conn, tenant, streamKey, receiptReferenced.Sequence)
	if baselineReferenced.State != disposition.StateReferenced || baselineReferenced.ArtifactRef == "" {
		t.Fatalf("baseline referenced view = %+v, want StateReferenced with its artifact ref", baselineReferenced)
	}

	// --- clause 3: an active hold refuses erasure before any bytes are ever
	// touched, and the refusal composes recordsmeta rather than
	// reimplementing hold semantics.
	hold := newHold(t, tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
			return err
		}
		_, err := recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, hold.HoldID, fixedAt)
		return err
	})

	var heldErr error
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, heldErr = disposition.Erase(ctx, tx, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: receiptInline.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: link.LinkID,
			Classification: disposition.ClassificationStandard,
			Reason:         "retention schedule hr-100 expired", Actor: "principal:records",
			At: fixedAt,
		})
		return nil // commit regardless: a refused Erase must leave nothing to roll back
	})
	var eventHeld disposition.ErrEventHeld
	if !errors.As(heldErr, &eventHeld) {
		t.Fatalf("Erase while held = %v (%T), want ErrEventHeld", heldErr, heldErr)
	}
	if !errors.Is(heldErr, recordsmeta.ErrCopyHeld) {
		t.Fatalf("Erase-while-held error does not unwrap to recordsmeta.ErrCopyHeld: %v", heldErr)
	}

	// Hold evidence remains fully verifiable after the refused attempt: the
	// hold is still ACTIVE and its intersection is still ACTIVE.
	reloadedHold := loadHold(t, conn, tenant, hold.HoldID)
	if reloadedHold.Status != "ACTIVE" {
		t.Fatalf("hold status = %s after a refused erase attempt, want ACTIVE", reloadedHold.Status)
	}
	intersections := listIntersections(t, conn, tenant, hold.HoldID)
	activeCount := 0
	for _, i := range intersections {
		if i.State == "ACTIVE" {
			activeCount++
		}
	}
	if activeCount == 0 {
		t.Fatal("no ACTIVE hold_intersection survives a refused erase attempt")
	}

	// The event row itself is untouched and no disposition row was created.
	afterHeldAttempt := readRawEvent(t, db, tenant, streamKey, receiptInline.Sequence)
	assertSameEventColumns(t, beforeAnything, afterHeldAttempt)
	if dispositionRowExists(t, db, tenant, streamKey, receiptInline.Sequence) {
		t.Fatal("a refused erase attempt recorded a disposition row")
	}
	viewWhileHeld := readView(t, conn, tenant, streamKey, receiptInline.Sequence)
	if viewWhileHeld.State != disposition.StateHeld {
		t.Fatalf("view while held = %+v, want StateHeld", viewWhileHeld)
	}
	if viewWhileHeld.HoldID != hold.HoldID {
		t.Fatalf("view while held names hold %s, want %s", viewWhileHeld.HoldID, hold.HoldID)
	}

	// Release the hold so the unblocked path can proceed.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := recordsmeta.ReleaseHold(ctx, tx, tenant, hold.HoldID, uuid.Nil, "principal:legal", "litigation resolved", fixedAt.Add(time.Hour))
		return err
	})

	// --- clauses 1 and 2: erase, then prove chronology/digest survive and
	// the two events remain distinguishable.
	beforeChainHead := verifyChain(t, conn, tenant, streamKey, chainDigester)

	var erasedResult disposition.View
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		erasedResult, err = disposition.Erase(ctx, tx, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: receiptInline.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: link.LinkID,
			Classification: disposition.ClassificationStandard,
			Reason:         "retention schedule hr-100 expired", Actor: "principal:records",
			At: fixedAt.Add(2 * time.Hour),
		})
		return err
	})
	if erasedResult.State != disposition.StatePayloadErased {
		t.Fatalf("Erase result = %+v, want StatePayloadErased", erasedResult)
	}

	// Clause 1, proven directly: erased and never-inline read through the
	// exact same accessor and are never confusable.
	viewErased := readView(t, conn, tenant, streamKey, receiptInline.Sequence)
	viewReferenced := readView(t, conn, tenant, streamKey, receiptReferenced.Sequence)
	if viewErased.State == viewReferenced.State {
		t.Fatalf("erased event and never-inline event both report %q -- indistinguishable", viewErased.State)
	}
	if viewErased.State != disposition.StatePayloadErased || len(viewErased.Bytes) != 0 {
		t.Fatalf("viewErased = %+v, want StatePayloadErased with no bytes", viewErased)
	}
	if viewReferenced.State != disposition.StateReferenced || viewReferenced.ArtifactRef != baselineReferenced.ArtifactRef {
		t.Fatalf("viewReferenced = %+v, want unchanged StateReferenced", viewReferenced)
	}
	if viewErased.State == disposition.PayloadState("") || viewReferenced.State == disposition.PayloadState("") {
		t.Fatal("a returned View carried the zero-value PayloadState")
	}

	// Withholding is the default, not something a caller opts into by using
	// disposition.ReadView: the PLAIN internal/data/ledger.Reader -- the one
	// every other caller in this codebase already uses (projection rebuild,
	// evidence export, provenance, query plans, the intent store) -- must
	// itself refuse to return the erased bytes and must say why through the
	// new PayloadState field, with no code in this test file reaching into
	// the disposition package to make that happen.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		plain, err := ledger.NewReader().ReadEvent(ctx, tx, tenant, streamKey, receiptInline.Sequence)
		if err != nil {
			return err
		}
		if plain.Payload != nil {
			t.Fatalf("ledger.Reader.ReadEvent returned the erased payload verbatim: %q", plain.Payload)
		}
		if plain.PayloadState != ledger.PayloadErased {
			t.Fatalf("ledger.Reader.ReadEvent reports PayloadState %q for an erased event, want PayloadErased", plain.PayloadState)
		}
		return nil
	})
	// The same default applies to ReadStream, and to the crypto-erased
	// event once it exists later in this test is checked again below.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		stream, err := ledger.NewReader().ReadStream(ctx, tx, tenant, streamKey)
		if err != nil {
			return err
		}
		for _, rec := range stream {
			if rec.Sequence != receiptInline.Sequence {
				continue
			}
			if rec.Payload != nil || rec.PayloadState != ledger.PayloadErased {
				t.Fatalf("ledger.Reader.ReadStream withheld nothing for the erased event: payload=%q state=%q", rec.Payload, rec.PayloadState)
			}
		}
		return nil
	})

	// Clause 2, column-by-column: every column of ledger_event's row is
	// byte-identical before and after erasure.
	afterErase := readRawEvent(t, db, tenant, streamKey, receiptInline.Sequence)
	assertSameEventColumns(t, beforeAnything, afterErase)

	// Clause 2, chain verification: the real hashchain verifier reproduces
	// the identical head after erasure.
	afterChainHead := verifyChain(t, conn, tenant, streamKey, chainDigester)
	if afterChainHead != beforeChainHead {
		t.Fatalf("chain head changed by erasure: before=%+v after=%+v", beforeChainHead, afterChainHead)
	}

	// A second erasure attempt is refused and changes nothing further.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := disposition.Erase(ctx, tx, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: receiptInline.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: link.LinkID,
			Classification: disposition.ClassificationStandard,
			Reason:         "again", Actor: "principal:records", At: fixedAt.Add(3 * time.Hour),
		})
		if !errors.Is(err, disposition.ErrAlreadyDisposed) {
			t.Fatalf("second erase = %v, want ErrAlreadyDisposed", err)
		}
		return nil
	})

	// --- REFACTOR: the classification-driven encryption boundary. A second
	// event, classified ENCRYPTED_AT_REST, erases by crypto-erasure into
	// StateRestricted rather than StatePayloadErased, and the disposition
	// row names the symbolic destroyed key.
	receiptEncrypted := appendWithChain(t, conn, tenant, chainAppender, appendRequest(tenant, streamKey, receiptReferenced.Sequence, []byte("bank-account-detail"), ""))
	var encryptedLink recordsmeta.CopyLink
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		encryptedLink, err = recordsmeta.RegisterCopy(ctx, tx, recordsmeta.CopyLink{
			TenantID: tenant, DeclarationID: declaration.DeclarationID, CopyType: "CANONICAL",
			StoreRef: "ledger:" + streamKey + ":encrypted", LedgerStream: streamKey, LedgerSequence: receiptEncrypted.Sequence,
		})
		return err
	})
	var encryptedResult disposition.View
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		encryptedResult, err = disposition.Erase(ctx, tx, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: receiptEncrypted.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: encryptedLink.LinkID,
			Classification: disposition.ClassificationEncryptedAtRest,
			Reason:         "bank detail retention expired", Actor: "principal:records",
			At: fixedAt.Add(4 * time.Hour),
		})
		return err
	})
	if encryptedResult.State != disposition.StateRestricted || encryptedResult.Mechanism != disposition.MechanismCryptoErasure {
		t.Fatalf("crypto-erasure result = %+v, want StateRestricted/MechanismCryptoErasure", encryptedResult)
	}
	rawEncrypted := readRawDisposition(t, db, tenant, streamKey, receiptEncrypted.Sequence)
	if !rawEncrypted.HasKeyRef || rawEncrypted.KeyRef == "" {
		t.Fatal("crypto-erasure disposition row has no key_ref recorded")
	}
	if !rawEncrypted.HasKeyDestroyedAt {
		t.Fatal("crypto-erasure disposition row has no key_destroyed_at recorded")
	}
	viewEncrypted := readView(t, conn, tenant, streamKey, receiptEncrypted.Sequence)
	if viewEncrypted.State != disposition.StateRestricted {
		t.Fatalf("ReadView after crypto-erasure = %+v, want StateRestricted", viewEncrypted)
	}
	// The default path withholds a crypto-erased payload too, not only an
	// overwritten one.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		plain, err := ledger.NewReader().ReadEvent(ctx, tx, tenant, streamKey, receiptEncrypted.Sequence)
		if err != nil {
			return err
		}
		if plain.Payload != nil || plain.PayloadState != ledger.PayloadRestricted {
			t.Fatalf("ledger.Reader.ReadEvent for a crypto-erased event = payload=%q state=%q, want nil/PayloadRestricted", plain.Payload, plain.PayloadState)
		}
		return nil
	})
	// The chain still verifies after a crypto-erasure too.
	if head := verifyChain(t, conn, tenant, streamKey, chainDigester); head.Sequence != receiptEncrypted.Sequence {
		t.Fatalf("chain head after appending+erasing the encrypted event = %+v, want sequence %d", head, receiptEncrypted.Sequence)
	}
}
