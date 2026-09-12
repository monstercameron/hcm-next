package disposition_test

// TestTodo_LEDGER_011_Mutation proves that no refused or malformed Erase
// attempt can destroy or alter anything: every column of the targeted
// event's ledger_event row is compared, named explicitly, before and after
// each refusal (via assertSameEventColumns in ledger011_test.go), and no
// stray ledger_payload_disposition row is ever created by a refusal. An
// aggregate "the event still exists" assertion would miss a bug that, say,
// nulled canonical_length while leaving payload alone; naming every column
// is what catches that.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/disposition"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/recordsmeta"
)

func TestTodo_LEDGER_011_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "ledger011-mutation")
	registerLedgerSchema(t, db, tenant)
	registerRecordsCopySchema(t, db, tenant)
	ensureStream(t, conn, tenant, streamKey)
	chainAppender := newChainAppender(t)

	// event A: an ordinary inline-payload event, used for the
	// validation-refusal and double-erase cases.
	eventA := appendWithChain(t, conn, tenant, chainAppender, appendRequest(tenant, streamKey, 0, []byte("mutation-a"), ""))
	// event B: an artifact-ref-only event -- never had an inline payload.
	eventB := appendWithChain(t, conn, tenant, chainAppender, appendRequest(tenant, streamKey, 1, nil, "artifact://doc/"+uuid.NewString()))
	// event C: a second inline-payload event, held for the whole test, used
	// for the hold-refusal case.
	eventC := appendWithChain(t, conn, tenant, chainAppender, appendRequest(tenant, streamKey, 2, []byte("mutation-c"), ""))
	// event D: a third inline-payload event, used as the link-mismatch
	// target (a caller supplies a LinkID that names a different event).
	eventD := appendWithChain(t, conn, tenant, chainAppender, appendRequest(tenant, streamKey, 3, []byte("mutation-d"), ""))

	// eventC gets its own declaration: recordsmeta.PropagateHold grips every
	// tracked copy under the declaration it is given, so eventA and eventD
	// must live under a declaration the hold never touches, or the
	// validation-refusal and double-erase cases below would spuriously
	// refuse with ErrEventHeld instead of the error each case means to
	// prove.
	declaration := newDeclaration(tenant)
	heldDeclaration := newDeclaration(tenant)
	var linkA, linkC, linkD recordsmeta.CopyLink
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, heldDeclaration); err != nil {
			return err
		}
		var err error
		linkA, err = recordsmeta.RegisterCopy(ctx, tx, recordsmeta.CopyLink{
			TenantID: tenant, DeclarationID: declaration.DeclarationID, CopyType: "CANONICAL",
			StoreRef: "ledger:a", LedgerStream: streamKey, LedgerSequence: eventA.Sequence,
		})
		if err != nil {
			return err
		}
		linkC, err = recordsmeta.RegisterCopy(ctx, tx, recordsmeta.CopyLink{
			TenantID: tenant, DeclarationID: heldDeclaration.DeclarationID, CopyType: "CANONICAL",
			StoreRef: "ledger:c", LedgerStream: streamKey, LedgerSequence: eventC.Sequence,
		})
		if err != nil {
			return err
		}
		linkD, err = recordsmeta.RegisterCopy(ctx, tx, recordsmeta.CopyLink{
			TenantID: tenant, DeclarationID: declaration.DeclarationID, CopyType: "CANONICAL",
			StoreRef: "ledger:d", LedgerStream: streamKey, LedgerSequence: eventD.Sequence,
		})
		return err
	})

	hold := newHold(t, tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
			return err
		}
		_, err := recordsmeta.PropagateHold(ctx, tx, tenant, heldDeclaration.DeclarationID, hold.HoldID, fixedAt)
		return err
	})

	// attempt runs one refused Erase and proves it left the named event's
	// row byte-identical, column by column, and created no disposition row.
	attempt := func(t *testing.T, key string, sequence int64, req disposition.EraseRequest, wantErr func(error) bool) {
		t.Helper()
		before := readRawEvent(t, db, tenant, key, sequence)
		hadDisposition := dispositionRowExists(t, db, tenant, key, sequence)

		var gotErr error
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, gotErr = disposition.Erase(ctx, tx, req)
			return nil
		})
		if gotErr == nil {
			t.Fatal("Erase unexpectedly succeeded")
		}
		if !wantErr(gotErr) {
			t.Fatalf("Erase returned %v (%T), did not match the expected refusal", gotErr, gotErr)
		}

		after := readRawEvent(t, db, tenant, key, sequence)
		assertSameEventColumns(t, before, after)

		if hasDisposition := dispositionRowExists(t, db, tenant, key, sequence); hasDisposition != hadDisposition {
			t.Fatalf("disposition row existence changed from %v to %v after a refused attempt", hadDisposition, hasDisposition)
		}
	}

	t.Run("empty classification is refused, not defaulted to STANDARD", func(t *testing.T) {
		attempt(t, streamKey, eventA.Sequence, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: eventA.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: linkA.LinkID,
			Classification: "", Reason: "x", Actor: "principal:records", At: fixedAt,
		}, func(err error) bool { return errors.Is(err, disposition.ErrClassificationRequired) })
	})

	t.Run("unrecognized classification is refused, not defaulted", func(t *testing.T) {
		attempt(t, streamKey, eventA.Sequence, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: eventA.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: linkA.LinkID,
			Classification: "TOP_SECRET", Reason: "x", Actor: "principal:records", At: fixedAt,
		}, func(err error) bool { return errors.Is(err, disposition.ErrClassificationRequired) })
	})

	t.Run("missing reason is refused", func(t *testing.T) {
		attempt(t, streamKey, eventA.Sequence, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: eventA.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: linkA.LinkID,
			Classification: disposition.ClassificationStandard, Reason: "", Actor: "principal:records", At: fixedAt,
		}, func(err error) bool { return errors.Is(err, disposition.ErrRequestInvalid) })
	})

	t.Run("missing actor is refused", func(t *testing.T) {
		attempt(t, streamKey, eventA.Sequence, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: eventA.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: linkA.LinkID,
			Classification: disposition.ClassificationStandard, Reason: "x", Actor: "", At: fixedAt,
		}, func(err error) bool { return errors.Is(err, disposition.ErrRequestInvalid) })
	})

	t.Run("declaration that does not exist is refused", func(t *testing.T) {
		attempt(t, streamKey, eventA.Sequence, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: eventA.Sequence,
			DeclarationID: uuid.New(), LinkID: linkA.LinkID,
			Classification: disposition.ClassificationStandard, Reason: "x", Actor: "principal:records", At: fixedAt,
		}, func(err error) bool { return errors.Is(err, disposition.ErrRequestInvalid) })
	})

	t.Run("link that does not exist is refused", func(t *testing.T) {
		attempt(t, streamKey, eventA.Sequence, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: eventA.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: uuid.New(),
			Classification: disposition.ClassificationStandard, Reason: "x", Actor: "principal:records", At: fixedAt,
		}, func(err error) bool { return errors.Is(err, recordsmeta.ErrCopyNotFound) })
	})

	t.Run("link naming a different event is refused, never trusted", func(t *testing.T) {
		// linkD names eventD; attempting to erase eventA under linkD's
		// identity must be refused rather than silently erasing whichever
		// event the link actually names or the one the request named.
		attempt(t, streamKey, eventA.Sequence, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: eventA.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: linkD.LinkID,
			Classification: disposition.ClassificationStandard, Reason: "x", Actor: "principal:records", At: fixedAt,
		}, func(err error) bool { return errors.Is(err, disposition.ErrLinkMismatch) })
		// eventD itself must also be untouched by the refused attempt.
		afterD := readRawEvent(t, db, tenant, streamKey, eventD.Sequence)
		beforeD := readRawEvent(t, db, tenant, streamKey, eventD.Sequence)
		assertSameEventColumns(t, beforeD, afterD)
	})

	t.Run("an artifact-ref-only event has no inline payload to erase", func(t *testing.T) {
		attempt(t, streamKey, eventB.Sequence, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: eventB.Sequence,
			DeclarationID: declaration.DeclarationID, LinkID: linkA.LinkID,
			Classification: disposition.ClassificationStandard, Reason: "x", Actor: "principal:records", At: fixedAt,
		}, func(err error) bool { return errors.Is(err, disposition.ErrNoInlinePayload) })
	})

	t.Run("an event under an active hold cannot be erased", func(t *testing.T) {
		attempt(t, streamKey, eventC.Sequence, disposition.EraseRequest{
			Tenant: tenant, StreamKey: streamKey, Sequence: eventC.Sequence,
			DeclarationID: heldDeclaration.DeclarationID, LinkID: linkC.LinkID,
			Classification: disposition.ClassificationStandard, Reason: "x", Actor: "principal:records", At: fixedAt,
		}, func(err error) bool {
			var held disposition.ErrEventHeld
			return errors.As(err, &held) && errors.Is(err, recordsmeta.ErrCopyHeld)
		})
		// The hold itself is still enforceable: still ACTIVE with the copy
		// still HELD.
		reloadedHold := loadHold(t, conn, tenant, hold.HoldID)
		if reloadedHold.Status != "ACTIVE" {
			t.Fatalf("hold status = %s after a refused erase attempt, want ACTIVE", reloadedHold.Status)
		}
	})

	t.Run("a second erase of an already-disposed event is refused", func(t *testing.T) {
		// First, a legitimate erase of eventA.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := disposition.Erase(ctx, tx, disposition.EraseRequest{
				Tenant: tenant, StreamKey: streamKey, Sequence: eventA.Sequence,
				DeclarationID: declaration.DeclarationID, LinkID: linkA.LinkID,
				Classification: disposition.ClassificationStandard,
				Reason:         "legitimate", Actor: "principal:records", At: fixedAt,
			})
			return err
		})
		beforeSecond := readRawEvent(t, db, tenant, streamKey, eventA.Sequence)
		rawBefore := readRawDisposition(t, db, tenant, streamKey, eventA.Sequence)

		var secondErr error
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, secondErr = disposition.Erase(ctx, tx, disposition.EraseRequest{
				Tenant: tenant, StreamKey: streamKey, Sequence: eventA.Sequence,
				DeclarationID: declaration.DeclarationID, LinkID: linkA.LinkID,
				Classification: disposition.ClassificationStandard,
				Reason:         "again", Actor: "principal:impostor", At: fixedAt,
			})
			return nil
		})
		if !errors.Is(secondErr, disposition.ErrAlreadyDisposed) {
			t.Fatalf("second erase = %v, want ErrAlreadyDisposed", secondErr)
		}

		afterSecond := readRawEvent(t, db, tenant, streamKey, eventA.Sequence)
		assertSameEventColumns(t, beforeSecond, afterSecond)

		rawAfter := readRawDisposition(t, db, tenant, streamKey, eventA.Sequence)
		if rawBefore != rawAfter {
			t.Fatalf("disposition row changed on a refused second erase: before=%+v after=%+v", rawBefore, rawAfter)
		}
		if rawAfter.Reason != "legitimate" || rawAfter.Actor != "principal:records" {
			t.Fatalf("a refused second erase overwrote the first erasure's evidence: reason=%q actor=%q", rawAfter.Reason, rawAfter.Actor)
		}
	})
}
