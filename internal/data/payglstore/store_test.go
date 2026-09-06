package payglstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/domains/labor"
	"github.com/monstercameron/hcm-next/internal/domains/paygl"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_PERSIST_PAYGL_001(t *testing.T) {
	db, store, tenant := testStore(t)
	rule := testAccountingRule(t)
	if err := store.PutAccountingRule(context.Background(), tenant, rule); err != nil {
		t.Fatal(err)
	}
	laborRule := testLaborRule(t)
	if err := store.PutLaborRule(context.Background(), tenant, laborRule); err != nil {
		t.Fatal(err)
	}
	result := testMappingResult(t)
	if err := store.AppendMappingResult(context.Background(), tenant, result, 1); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM labor_rule`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("labor_rule count = %d, want 1", count)
	}
}

func TestTodo_PERSIST_PAYGL_001_Fault(t *testing.T) {
	_, store, tenant := testStore(t)
	rule := testAccountingRule(t)
	if err := store.PutAccountingRule(context.Background(), tenant, rule); err != nil {
		t.Fatal(err)
	}
	err := store.PutAccountingRule(context.Background(), tenant, rule)
	var conflict paygl.ConflictError
	if !errors.As(err, &conflict) || conflict.Code != paygl.ConflictDuplicateRevision || !errors.Is(err, paygl.ErrPersistenceDuplicate) {
		t.Fatalf("duplicate revision error = %v, want typed duplicate revision", err)
	}
	stale, err := rule.NewVersion("v2", rule.Effective)
	if err != nil {
		t.Fatal(err)
	}
	stale.Supersedes = "never-recorded"
	stale.CanonicalDigest = ""
	stale, err = paygl.NewAccountingRule(stale)
	if err != nil {
		t.Fatal(err)
	}
	err = store.PutAccountingRule(context.Background(), tenant, stale)
	if !errors.As(err, &conflict) || conflict.Code != paygl.ConflictStaleRevision || !errors.Is(err, paygl.ErrPersistenceVersionConflict) {
		t.Fatalf("stale revision error = %v, want typed stale revision", err)
	}
}

func TestTodo_PERSIST_PAYGL_001_Integration(t *testing.T) {
	_, store, tenant := testStore(t)
	rule := testAccountingRule(t)
	if err := store.PutAccountingRule(context.Background(), tenant, rule); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetAccountingRule(context.Background(), tenant, rule.ID, rule.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != rule.ID || got.CanonicalDigest != rule.CanonicalDigest || got.ComponentCode != rule.ComponentCode {
		t.Fatalf("accounting rule round trip = %+v, want digest %q", got, rule.CanonicalDigest)
	}
	laborRule := testLaborRule(t)
	if err := store.PutLaborRule(context.Background(), tenant, laborRule); err != nil {
		t.Fatal(err)
	}
	gotLabor, err := store.GetLaborRule(context.Background(), tenant, laborRule.ID, laborRule.Version)
	if err != nil {
		t.Fatal(err)
	}
	if gotLabor.CanonicalDigest != laborRule.CanonicalDigest || len(gotLabor.Dimensions) != len(laborRule.Dimensions) {
		t.Fatalf("labor rule round trip = %+v", gotLabor)
	}
	result := testMappingResult(t)
	if err := store.AppendMappingResult(context.Background(), tenant, result, 1); err != nil {
		t.Fatal(err)
	}
	gotResult, err := store.GetMappingResult(context.Background(), tenant, result.RunID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotResult.Digest != result.Digest || len(gotResult.Mappings) != 1 {
		t.Fatalf("mapping result round trip = %+v", gotResult)
	}
}

func TestTodo_PERSIST_PAYGL_001_Security(t *testing.T) {
	db, store, tenantA := testStore(t)
	tenantB := uuid.NewString()
	admin := db.NewConn(t)
	insertTenant(t, admin, tenantB)
	rule := testAccountingRule(t)
	if err := store.PutAccountingRule(context.Background(), tenantA, rule); err != nil {
		t.Fatal(err)
	}
	_, err := store.GetAccountingRule(context.Background(), tenantB, rule.ID, rule.Version)
	if !errors.Is(err, paygl.ErrPersistenceNotFound) {
		t.Fatalf("cross-tenant read error = %v, want not found under RLS", err)
	}
}

func TestTodo_PERSIST_PAYGL_001_Recovery(t *testing.T) {
	db, store, tenant := testStore(t)
	rule := testAccountingRule(t)
	if err := store.PutAccountingRule(context.Background(), tenant, rule); err != nil {
		t.Fatal(err)
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	fresh := New(conn)
	got, err := fresh.GetAccountingRule(context.Background(), tenant, rule.ID, rule.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != rule.CanonicalDigest {
		t.Fatalf("fresh-connection digest = %q, want %q", got.CanonicalDigest, rule.CanonicalDigest)
	}
}

func TestTodo_PERSIST_PAYGL_001_Mutation(t *testing.T) {
	db, store, tenant := testStore(t)
	app := db.NewConn(t)
	if _, err := app.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	rule := testAccountingRule(t)
	if err := store.PutAccountingRule(context.Background(), tenant, rule); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Exec(context.Background(), `UPDATE paygl_accounting_rule SET debit_account='9999' WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("accounting rule update succeeded")
	}
	result := testMappingResult(t)
	if err := store.AppendMappingResult(context.Background(), tenant, result, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Exec(context.Background(), `DELETE FROM paygl_mapping_result WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("mapping result delete succeeded")
	}
}

func testStore(t *testing.T) (*pgtest.DB, *Store, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 47); err != nil {
		t.Fatalf("apply migrations through 00047: %v", err)
	}
	tenant := uuid.NewString()
	insertTenant(t, db.Conn, tenant)
	app := db.NewConn(t)
	if _, err := app.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	return db, New(app), tenant
}

type execer interface {
	Exec(context.Context, string, ...any) (int64, error)
}

func insertTenant(t *testing.T, exec execer, tenant string) {
	t.Helper()
	_, err := exec.Exec(context.Background(), `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-test', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, uuid.MustParse(tenant), "tenant-"+tenant, "tenant-"+tenant)
	if err != nil {
		t.Fatal(err)
	}
}

func testAccountingRule(t *testing.T) paygl.AccountingRule {
	t.Helper()
	start, _ := values.NewLocalDate(2026, time.January, 1)
	end, _ := values.NewLocalDate(2027, time.January, 1)
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "calendar.us", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	rule, err := paygl.NewAccountingRule(paygl.AccountingRule{
		ID: "salary", Version: "v1", Effective: interval, ComponentKind: paygl.ComponentEarning, ComponentCode: "SALARY",
		Dimension: labor.Dimension{Kind: labor.DimensionCostCenter, Value: "cc-1", Version: "v1"}, DebitAccount: "6000", CreditAccount: "2100", Currency: "USD",
		Rounding: values.RoundingExactRequired, SuspensePolicy: paygl.SuspensePolicyReject,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func testLaborRule(t *testing.T) labor.LaborRule {
	t.Helper()
	start, _ := values.NewLocalDate(2026, time.January, 1)
	end, _ := values.NewLocalDate(2027, time.January, 1)
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "calendar.us", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	dec := func(text string) values.Decimal {
		value, e := values.NewDecimal(text, 2, values.RoundingExactRequired)
		if e != nil {
			t.Fatal(e)
		}
		return value
	}
	ref := func(name string) labor.RuleRef {
		return labor.RuleRef{ID: name, Version: "v1", Digest: "sha256:" + name}
	}
	rule, err := labor.NewLaborRule(labor.LaborRule{ID: "labor-cost", Version: "v1", Effective: interval, WorkerRef: ref("worker"), TimeRef: ref("time"), EntityRef: ref("entity"), JobRef: ref("job"), EarningRef: ref("earning"), Dimensions: []labor.DimensionKind{labor.DimensionCostCenter}, Currency: "USD", BaseRate: dec("40.00"), DifferentialRate: dec("2.50"), OvertimeMultiplier: dec("1.50"), EmployerBurdenRate: dec("0.25")})
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func testMappingResult(t *testing.T) paygl.MappingResult {
	t.Helper()
	amount, _ := values.NewDecimal("100.00", 2, values.RoundingExactRequired)
	percent, _ := values.NewDecimal("100.00", 2, values.RoundingExactRequired)
	result := paygl.MappingResult{RunID: "run-1", RunRevision: 1, RunDigest: canonicalbytes.Digest([]byte("run")), TotalComponentAmount: amount, TotalSplitAmount: amount, Mappings: []paygl.ComponentMapping{{ComponentID: "line-1", ComponentOrdinal: 1, ComponentKind: paygl.ComponentEarning, ComponentCode: "SALARY", ComponentAmount: amount, SplitOrdinal: 1, Percent: percent, Amount: amount, Currency: "USD", Dimension: labor.Dimension{Kind: labor.DimensionCostCenter, Value: "cc-1", Version: "v1"}, DebitAccount: "6000", CreditAccount: "2100", RuleID: "salary", RuleVersion: "v1", RuleDigest: canonicalbytes.Digest([]byte("rule")), SourceDigest: canonicalbytes.Digest([]byte("source"))}}}
	digest, err := result.DigestValue()
	if err != nil {
		t.Fatal(err)
	}
	result.Digest = digest
	return result
}
