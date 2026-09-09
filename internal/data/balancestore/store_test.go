package balancestore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/balancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/balance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var at = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func TestTodo_PERSIST_BALANCE_001(t *testing.T) {
	t.Run("adapter constructs", func(t *testing.T) {
		if balancestore.New() == (balancestore.Store{}) {
			t.Log("stateless store")
		}
	})
}

func TestTodo_PERSIST_BALANCE_001_Integration(t *testing.T) {
	t.Parallel()
	db := newBalanceDB(t)
	tenant := insertTenant(t, db, "balance-integration")
	conn := appConn(t, db)
	store := balancestore.New()
	def := definition()
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutDefinition(context.Background(), tx, tenant, def, 1)
		return err
	})
	entry := entry(def, "first", "1.2500")
	var receipt balance.PostReceipt
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		receipt, err = store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: entry, ExpectedHead: 0})
		return err
	})
	if receipt.Head != 1 || receipt.Entry.RecordedAt.String() == "" {
		t.Fatalf("receipt = %+v", receipt)
	}
	var entries []balance.BalanceEntry
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		entries, err = store.Entries(context.Background(), tx, tenant, entry.AccountID)
		return err
	})
	if len(entries) != 1 || entries[0].Amount.String() != "1.2500" {
		t.Fatalf("entries = %+v", entries)
	}
	var entryRef uuid.UUID
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		entryRef, err = store.EntryRef(context.Background(), tx, tenant, entry.AccountID, entry.IdempotencyKey)
		return err
	})
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.AppendLifecycle(context.Background(), tx, tenant, balancestore.LifecycleRecord{
			EntryRef: entryRef, EventSequence: 1,
			Entry: balance.LifecycleEntry{Operation: balance.LifecycleCarryover, Reason: "carry", PolicyID: "policy", PolicyVersion: "1", SourcePeriod: balance.PeriodDefinition{ID: "period-a"}, TargetPeriod: balance.PeriodDefinition{ID: "period-b"}},
		})
		return err
	})
}

func TestTodo_PERSIST_BALANCE_001_Fault(t *testing.T) {
	t.Parallel()
	db := newBalanceDB(t)
	tenant := insertTenant(t, db, "balance-fault")
	conn := appConn(t, db)
	store := balancestore.New()
	def := definition()
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutDefinition(context.Background(), tx, tenant, def, 1)
		return err
	})
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutDefinition(context.Background(), tx, tenant, def, 1)
		if !errors.Is(err, balancestore.ErrDuplicateRevision) || balancestore.CodeOf(err) != balancestore.CodeDuplicateRevision {
			t.Fatalf("duplicate = %v", err)
		}
		return nil
	})
	e := entry(def, "fault", "1.0000")
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: e, ExpectedHead: 0})
		return err
	})
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: entry(def, "stale", "1.0000"), ExpectedHead: 0})
		if !errors.Is(err, balancestore.ErrStaleCAS) || balancestore.CodeOf(err) != balancestore.CodeStaleCAS {
			t.Fatalf("stale = %v", err)
		}
		return nil
	})
}

func TestTodo_PERSIST_BALANCE_001_Security(t *testing.T) {
	t.Parallel()
	db := newBalanceDB(t)
	a, b := insertTenant(t, db, "balance-a"), insertTenant(t, db, "balance-b")
	conn := appConn(t, db)
	store := balancestore.New()
	def := definition()
	withTenant(t, conn, a, func(tx dbport.Tx) error {
		_, err := store.PutDefinition(context.Background(), tx, a, def, 1)
		return err
	})
	withTenant(t, conn, b, func(tx dbport.Tx) error {
		got, err := store.LoadDefinition(context.Background(), tx, b, def.ID, def.Version)
		if !errors.Is(err, balancestore.ErrDefinitionNotFound) || got.Definition.ID != "" {
			t.Fatalf("cross tenant definition = %+v %v", got, err)
		}
		return nil
	})
}

func TestTodo_PERSIST_BALANCE_001_Recovery(t *testing.T) {
	t.Parallel()
	db := newBalanceDB(t)
	tenant := insertTenant(t, db, "balance-recovery")
	conn := appConn(t, db)
	store := balancestore.New()
	def := definition()
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutDefinition(context.Background(), tx, tenant, def, 1)
		return err
	})
	e := entry(def, "recovery", "2.0000")
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: e, ExpectedHead: 0})
		return err
	})
	fresh := db.NewConn(t)
	defer fresh.Close(context.Background())
	withTenant(t, fresh, tenant, func(tx dbport.Tx) error {
		got, err := store.LoadDefinition(context.Background(), tx, tenant, def.ID, def.Version)
		if err != nil || got.Revision != 1 {
			t.Fatalf("definition recovery = %+v %v", got, err)
		}
		rows, err := store.Entries(context.Background(), tx, tenant, e.AccountID)
		if err != nil || len(rows) != 1 {
			t.Fatalf("entry recovery = %+v %v", rows, err)
		}
		return nil
	})
}

func TestTodo_PERSIST_BALANCE_001_Mutation(t *testing.T) {
	t.Parallel()
	db := newBalanceDB(t)
	tenant := insertTenant(t, db, "balance-mutation")
	conn := appConn(t, db)
	store := balancestore.New()
	def := definition()
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutDefinition(context.Background(), tx, tenant, def, 1)
		return err
	})
	entry := entry(def, "mutation", "1.0000")
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.Post(context.Background(), tx, tenant, balance.PostRequest{Entry: entry, ExpectedHead: 0})
		return err
	})
	err := withTenantErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `DELETE FROM accumulator_definition WHERE tenant_id=$1`, tenant)
		return err
	})
	if err == nil {
		t.Fatal("definition delete was accepted")
	}
	err = withTenantErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE balance_entry SET amount=2 WHERE tenant_id=$1`, tenant)
		return err
	})
	if err == nil {
		t.Fatal("balance entry update was accepted")
	}
}

func newBalanceDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 51); err != nil {
		t.Fatalf("apply migrations through 00051: %v", err)
	}
	return db
}

func definition() balance.AccumulatorDefinition {
	return balance.AccumulatorDefinition{ID: "vacation", Version: "2026.1", Name: "Vacation", Unit: "HOURS", Currency: "HOUR", Subject: "WORKER", Period: balance.PeriodPayPeriod, Dimensions: []balance.BalanceDimension{{Name: "worker_id", ValueType: "uuid", Required: true}}, EntryTypes: []string{"ACCRUAL"}, Authority: balance.Authority{SourceID: "policy", Version: "1"}, Floor: balance.PolicyRule{Kind: balance.RuleNone, Version: "1"}, Cap: balance.PolicyRule{Kind: balance.RuleNone, Version: "1"}, Expiry: balance.PolicyRule{Kind: balance.RuleNone, Version: "1"}, Rollover: balance.PolicyRule{Kind: balance.RuleNone, Version: "1"}, Correction: balance.PolicyRule{Kind: balance.RuleNone, Version: "1"}}
}
func entry(d balance.AccumulatorDefinition, key, amount string) balance.BalanceEntry {
	dec, _ := values.NewDecimal(amount, 4, values.RoundingExactRequired)
	return balance.BalanceEntry{AccountID: "acct-1", DefinitionID: d.ID, DefinitionVersion: d.Version, Unit: d.Unit, Currency: d.Currency, Subject: d.Subject, Period: string(d.Period), Dimensions: map[string]string{"worker_id": uuid.NewString()}, Kind: balance.Credit, Amount: dec, EntryType: "ACCRUAL", SourceTransactionID: "txn-" + key, IdempotencyKey: key}
}
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	c := db.NewConn(t)
	if _, err := c.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	return c
}
func withTenant(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	if err := withTenantErr(conn, tenant, fn); err != nil {
		t.Fatal(err)
	}
}
func withTenantErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
