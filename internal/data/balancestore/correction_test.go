package balancestore_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/balancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/balance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func newBalanceCorrectionDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 279); err != nil {
		t.Fatalf("apply migrations through 00279: %v", err)
	}
	return db
}

func correctionDefinition() balance.AccumulatorDefinition {
	d := definition()
	d.EntryTypes = append(d.EntryTypes, balance.AdjustmentEntryType)
	return d
}

func correctionEntries(t *testing.T, def balance.AccumulatorDefinition) (balance.BalanceEntry, balance.BalanceEntry) {
	t.Helper()
	original := entry(def, "original", "10.0000")
	original.EffectiveAt = values.NewInstant(time.Date(2026, 7, 1, 12, 0, 0, 123456000, time.UTC))
	original.AuthorizedAt = values.NewInstant(time.Date(2026, 7, 2, 12, 0, 0, 654321000, time.UTC))
	correction := entry(def, "correction", "2.5000")
	correction.EntryType = balance.AdjustmentEntryType
	correction.EffectiveAt = original.EffectiveAt
	correction.AuthorizedAt = values.NewInstant(time.Date(2026, 7, 3, 12, 0, 0, 111222000, time.UTC))
	return original, correction
}

func prepareCorrection(t *testing.T, db *pgtest.DB, tenant uuid.UUID) (*pgxadapter.Conn, balancestore.Store, balance.BalanceEntry, balance.BalanceEntry, balance.PostReceipt) {
	t.Helper()
	conn := appConn(t, db)
	store := balancestore.New()
	def := correctionDefinition()
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutDefinition(context.Background(), tx, tenant, def, 1)
		return err
	})
	original, correction := correctionEntries(t, def)
	var originalReceipt balance.PostReceipt
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		originalReceipt, err = store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: original, ExpectedHead: 0})
		return err
	})
	correction.SupersedesDigest = originalReceipt.Digest
	return conn, store, original, correction, originalReceipt
}

func TestTodo_BAL_006_Integration(t *testing.T) {
	db := newBalanceCorrectionDB(t)
	tenant := insertTenant(t, db, "balance-correction-integration")
	conn, store, original, correction, originalReceipt := prepareCorrection(t, db, tenant)
	defer conn.Close(context.Background())
	var correctionReceipt balance.PostReceipt
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		correctionReceipt, err = store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: correction, ExpectedHead: 1})
		return err
	})
	if correctionReceipt.Digest != correction.Digest() || correctionReceipt.Entry.SupersedesDigest != originalReceipt.Digest {
		t.Fatalf("correction receipt = %+v", correctionReceipt)
	}
	fresh := appConn(t, db)
	defer fresh.Close(context.Background())
	withTenant(t, fresh, tenant, func(tx dbport.Tx) error {
		entries, err := store.Entries(context.Background(), tx, tenant, original.AccountID)
		if err != nil || len(entries) != 2 {
			t.Fatalf("fresh entries = %+v, %v", entries, err)
		}
		if entries[1].SupersedesDigest != originalReceipt.Digest || entries[1].Digest() != correctionReceipt.Digest || entries[1].EffectiveAt.Compare(correction.EffectiveAt) != 0 || entries[1].AuthorizedAt.Compare(correction.AuthorizedAt) != 0 {
			t.Fatalf("lineage/material changed after restart: %+v", entries[1])
		}
		return nil
	})
	withTenant(t, fresh, tenant, func(tx dbport.Tx) error {
		replay, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: correction, ExpectedHead: 1})
		if err != nil || !replay.Replay || replay.Digest != correctionReceipt.Digest || replay.Entry.SupersedesDigest != originalReceipt.Digest {
			t.Fatalf("replay = %+v, %v", replay, err)
		}
		return nil
	})
	conflict := correction
	conflict.Amount = values.MustDecimal("2.5001", 4, values.RoundingExactRequired)
	err := withTenantErr(fresh, tenant, func(tx dbport.Tx) error {
		_, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: conflict, ExpectedHead: 2})
		return err
	})
	if !errors.Is(err, balancestore.ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay err=%v", err)
	}
}

func TestTodo_BAL_006_RecoveryLegacyFailsClosed(t *testing.T) {
	db := newBalanceDB(t)
	tenant := insertTenant(t, db, "balance-correction-legacy")
	conn, store, _, correction, _ := prepareCorrection(t, db, tenant)
	defer conn.Close(context.Background())
	correction.SupersedesDigest = "sha256:legacy-parent"
	err := withTenantErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: correction, ExpectedHead: 1})
		return err
	})
	if !errors.Is(err, balancestore.ErrInvalid) {
		t.Fatalf("legacy correction err=%v", err)
	}
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		head, err := store.Head(context.Background(), tx, tenant, correction.AccountID)
		if err != nil || head != 1 {
			t.Fatalf("legacy rejection head=%d err=%v", head, err)
		}
		return nil
	})
}

func TestTodo_BAL_006_RecoveryPreupgradeEntryCanBeCorrectedAfter279(t *testing.T) {
	db := newBalanceDB(t)
	tenant := insertTenant(t, db, "balance-correction-upgrade")
	conn := appConn(t, db)
	store := balancestore.New()
	def := correctionDefinition()
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutDefinition(context.Background(), tx, tenant, def, 1)
		return err
	})
	original, correction := correctionEntries(t, def)
	var originalReceipt balance.PostReceipt
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		originalReceipt, err = store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: original, ExpectedHead: 0})
		return err
	})
	conn.Close(context.Background())
	if _, err := db.Provider(t).UpTo(context.Background(), 279); err != nil {
		t.Fatalf("upgrade populated ledger through 00279: %v", err)
	}
	fresh := appConn(t, db)
	defer fresh.Close(context.Background())
	withTenant(t, fresh, tenant, func(tx dbport.Tx) error {
		entries, err := store.Entries(context.Background(), tx, tenant, original.AccountID)
		if err != nil || len(entries) != 1 || entries[0].Digest() != originalReceipt.Digest {
			t.Fatalf("preupgrade original = %+v, %v", entries, err)
		}
		return nil
	})
	correction.SupersedesDigest = originalReceipt.Digest
	withTenant(t, fresh, tenant, func(tx dbport.Tx) error {
		posted, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: correction, ExpectedHead: 1})
		if err != nil || posted.Entry.SupersedesDigest != originalReceipt.Digest {
			t.Fatalf("post-upgrade correction = %+v, %v", posted, err)
		}
		return err
	})
}

func TestTodo_BAL_006_RaceDurableOneWinner(t *testing.T) {
	db := newBalanceCorrectionDB(t)
	tenant := insertTenant(t, db, "balance-correction-race")
	setup, store, _, correction, _ := prepareCorrection(t, db, tenant)
	setup.Close(context.Background())
	other := correction
	other.IdempotencyKey = "correction-other"
	other.SourceTransactionID = "txn-correction-other"
	other.Amount = values.MustDecimal("3.5000", 4, values.RoundingExactRequired)
	requests := []balance.BalanceEntry{correction, other}
	errs := make([]error, 2)
	conns := []*pgxadapter.Conn{appConn(t, db), appConn(t, db)}
	defer conns[0].Close(context.Background())
	defer conns[1].Close(context.Background())
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = withTenantErr(conns[i], tenant, func(tx dbport.Tx) error {
				_, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: requests[i], ExpectedHead: 1})
				return err
			})
		}(i)
	}
	wg.Wait()
	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
		} else if !errors.Is(err, balancestore.ErrStaleCAS) && !errors.Is(err, balancestore.ErrCorrectionConflict) {
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("race winners=%d errors=%v", winners, errs)
	}
}

func TestTodo_BAL_006_FaultRollbackAndExactPrecision(t *testing.T) {
	db := newBalanceCorrectionDB(t)
	tenant := insertTenant(t, db, "balance-correction-fault")
	conn, store, _, correction, _ := prepareCorrection(t, db, tenant)
	defer conn.Close(context.Background())
	rollback := errors.New("force rollback")
	err := withTenantErr(conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: correction, ExpectedHead: 1}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("rollback err=%v", err)
	}
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		head, err := store.Head(context.Background(), tx, tenant, correction.AccountID)
		if err != nil || head != 1 {
			t.Fatalf("rollback head=%d err=%v", head, err)
		}
		return nil
	})
	correction.Amount = values.MustDecimal("12.3400", 4, values.RoundingExactRequired)
	exact := correction
	exact.Amount = values.MustDecimal("0.00001", 5, values.RoundingExactRequired)
	exact.AuthorizedAt = values.NewInstant(time.Date(2026, 7, 3, 12, 0, 0, 111222333, time.UTC))
	sameNumericOtherScale := correction
	sameNumericOtherScale.Amount = values.MustDecimal("12.34", 2, values.RoundingExactRequired)
	if sameNumericOtherScale.Digest() == correction.Digest() || exact.Digest() == correction.Digest() {
		t.Fatal("entry digest does not distinguish declared decimal scale, value and timestamp nanos")
	}
	var receipt balance.PostReceipt
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		receipt, err = store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: exact, ExpectedHead: 1})
		return err
	})
	if receipt.Entry.Amount.Scale() != 5 || receipt.Entry.Amount.String() != "0.00001" || receipt.Entry.AuthorizedAt.Compare(exact.AuthorizedAt) != 0 || receipt.Digest != exact.Digest() {
		t.Fatalf("exact receipt = %+v", receipt)
	}
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		entries, err := store.Entries(context.Background(), tx, tenant, exact.AccountID)
		if err != nil || len(entries) != 2 || entries[1].Amount.Scale() != 5 || entries[1].AuthorizedAt.Compare(exact.AuthorizedAt) != 0 || entries[1].Digest() != receipt.Digest {
			t.Fatalf("exact stored entries = %+v, %v", entries, err)
		}
		replay, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: exact, ExpectedHead: 1})
		if err != nil || !replay.Replay || replay.Digest != receipt.Digest {
			t.Fatalf("exact replay = %+v, %v", replay, err)
		}
		return nil
	})
	maximum := correction
	maximum.AccountID = "maximum-account"
	maximum.SupersedesDigest = ""
	maximum.EntryType = "ACCRUAL"
	maximum.IdempotencyKey = "maximum-decimal"
	maximum.SourceTransactionID = "txn-maximum-decimal"
	maximum.Amount = values.MustDecimal("99999999999999999999999999999999999999", 0, values.RoundingExactRequired)
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		posted, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: maximum, ExpectedHead: 0})
		if err != nil || posted.Entry.Amount.String() != maximum.Amount.String() || posted.Digest != maximum.Digest() {
			t.Fatalf("maximum decimal = %+v, %v", posted, err)
		}
		return err
	})
}

func TestTodo_BAL_006_SecurityDirectSQLConstraints(t *testing.T) {
	db := newBalanceCorrectionDB(t)
	tenant := insertTenant(t, db, "balance-correction-sql")
	conn, store, _, correction, receipt := prepareCorrection(t, db, tenant)
	defer conn.Close(context.Background())
	parent, err := store.EntryRef(context.Background(), conn, tenant, correction.AccountID, "original")
	if err == nil || parent != uuid.Nil {
		// App reads require transaction-local tenant context; this assertion also
		// ensures this helper is not accidentally an RLS bypass.
		t.Fatalf("unscoped parent read = %s, %v", parent, err)
	}
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		parent, err = store.EntryRef(context.Background(), tx, tenant, correction.AccountID, "original")
		return err
	})
	insert := func(account string, digest any, parentID any) error {
		return withTenantErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), `INSERT INTO balance_entry
				(row_id,tenant_id,account_id,definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,source_transaction_id,idempotency_key,effective_at,authorized_at,event_sequence,supersedes_digest,supersedes_row_id,amount_exact,amount_scale,amount_rounding,effective_at_submicro,authorized_at_submicro)
				SELECT $1,tenant_id,$2,definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,'CORRECTION','direct-sql',$3,effective_at,authorized_at,2,$4,$5,amount_exact,amount_scale,amount_rounding,effective_at_submicro,authorized_at_submicro
				FROM balance_entry WHERE tenant_id=$6 AND row_id=$7`, uuid.New(), account, uuid.NewString(), digest, parentID, tenant, parent)
			return err
		})
	}
	if err := insert(correction.AccountID, receipt.Digest, nil); err == nil {
		t.Fatal("partial correction lineage accepted")
	}
	if err := insert("different-account", receipt.Digest, parent); err == nil {
		t.Fatal("cross-account correction parent accepted")
	}
	otherTenant := insertTenant(t, db, "balance-correction-sql-other")
	otherConn, _, _, otherCorrection, _ := prepareCorrection(t, db, otherTenant)
	defer otherConn.Close(context.Background())
	var otherParent uuid.UUID
	withTenant(t, otherConn, otherTenant, func(tx dbport.Tx) error {
		var err error
		otherParent, err = store.EntryRef(context.Background(), tx, otherTenant, otherCorrection.AccountID, "original")
		return err
	})
	if err := insert(correction.AccountID, receipt.Digest, otherParent); err == nil {
		t.Fatal("cross-tenant correction parent accepted")
	}
	partialErr := withTenantErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO balance_entry
			(row_id,tenant_id,account_id,definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,source_transaction_id,idempotency_key,event_sequence,amount_exact)
			SELECT $1,tenant_id,'partial-metadata',definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,'direct-partial',$2,1,amount_exact
			FROM balance_entry WHERE tenant_id=$3 AND row_id=$4`, uuid.New(), uuid.NewString(), tenant, parent)
		return err
	})
	if partialErr == nil {
		t.Fatal("partial exact amount metadata accepted")
	}
	missingRemainderErr := withTenantErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO balance_entry
			(row_id,tenant_id,account_id,definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,source_transaction_id,idempotency_key,effective_at,event_sequence,amount_exact,amount_scale,amount_rounding)
			SELECT $1,tenant_id,'partial-time',definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,'direct-time',$2,effective_at,1,amount_exact,amount_scale,amount_rounding
			FROM balance_entry WHERE tenant_id=$3 AND row_id=$4`, uuid.New(), uuid.NewString(), tenant, parent)
		return err
	})
	if missingRemainderErr == nil {
		t.Fatal("exact timestamp without submicro metadata accepted")
	}
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO balance_entry
			(row_id,tenant_id,account_id,definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,source_transaction_id,idempotency_key,effective_at,authorized_at,event_sequence,amount_exact,amount_scale,amount_rounding,effective_at_submicro,authorized_at_submicro)
			SELECT $1,tenant_id,'corrupt-metadata',definition_id,definition_version,unit,currency,subject,period,dimensions,kind,amount,entry_type,'direct-corrupt',$2,effective_at,authorized_at,1,'11.0000',4,'EXACT_REQUIRED',effective_at_submicro,authorized_at_submicro
			FROM balance_entry WHERE tenant_id=$3 AND row_id=$4`, uuid.New(), uuid.NewString(), tenant, parent)
		return err
	})
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.Entries(context.Background(), tx, tenant, "corrupt-metadata"); err == nil {
			t.Fatal("inconsistent exact amount metadata decoded")
		}
		return nil
	})
}
