package fxstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fx"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// reversibleSchema returns a fresh schema migrated to the newest reversible
// version with a tenant, the app role and a store bound to it. The stack tip
// may hold declared-irreversible evidence migrations, which refuse goose
// Down by design, so down/up cycles run here instead of at the tip, which
// goose cannot skip past.
func reversibleSchema(t *testing.T) (*pgtest.DB, *Store, string, int64) {
	t.Helper()
	reversible, err := migrations.NewestReversibleVersion()
	if err != nil {
		t.Fatalf("newest reversible version: %v", err)
	}
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), reversible); err != nil {
		t.Fatalf("migrate fresh schema to %d: %v", reversible, err)
	}
	tenant := uuid.NewString()
	insertTenant(t, db.Conn, tenant)
	app := db.NewConn(t)
	if _, err := app.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	return db, New(app), tenant, reversible
}

// TestTodo_FX_003_Integration proves that a row written with the pre-00276
// column set is upgraded with only the safe legacy defaults: revision one and
// no fabricated predecessor lineage.
func TestTodo_FX_003_Integration(t *testing.T) {
	db, store, tenant, _ := reversibleSchema(t)
	source := testSource(t, "legacy-upgrade-source")
	if err := store.SaveRateSource(context.Background(), tenant, source); err != nil {
		t.Fatal(err)
	}
	provider := db.Provider(t)
	if _, err := provider.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	legacy := testQuote(t, "legacy-upgrade-quote", source.SourceID)
	db.Exec(t, `INSERT INTO fx_quote_revision (row_id,tenant_id,quote_id,source_id,source_revision,as_of,effective_at,observed_at,known_at,canonical_digest) VALUES ($1,$2,$3,$4,$5,$6,$6,$6,$7,$8)`, uuid.New(), tenant, legacy.QuoteID, legacy.SourceID, legacy.SourceRevision, legacy.AsOf.Time(), legacy.KnownAt.Time(), strings.TrimPrefix(legacy.CanonicalDigest, "sha256:"))
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	var revision int64
	var parentID, parentDigest *string
	if err := db.QueryRow(context.Background(), `SELECT quote_revision,parent_quote_id,parent_digest FROM fx_quote_revision WHERE tenant_id=$1 AND quote_id=$2`, tenant, legacy.QuoteID).Scan(&revision, &parentID, &parentDigest); err != nil {
		t.Fatal(err)
	}
	if revision != 1 || parentID != nil || parentDigest != nil {
		t.Fatalf("legacy lineage revision=%d parent_id=%v parent_digest=%v", revision, parentID, parentDigest)
	}
	if _, err := store.LoadQuote(context.Background(), tenant, legacy.QuoteID); !errors.Is(err, fx.ErrStoreInvalid) {
		t.Fatalf("legacy incomplete policy err=%v, want fail-closed invalid", err)
	}
}

func TestTodo_FX_003_Recovery(t *testing.T) {
	db, store, tenant := testStore(t)
	source := testSource(t, "successor-recovery-source")
	if err := store.SaveRateSource(context.Background(), tenant, source); err != nil {
		t.Fatal(err)
	}
	previous := testQuote(t, "successor-parent", source.SourceID)
	if err := store.SaveQuote(context.Background(), tenant, previous); err != nil {
		t.Fatal(err)
	}
	successor := testQuote(t, "successor-child", source.SourceID)
	successor.KnownAt = testInstant(t, "2026-06-01T12:00:00Z")
	successor.Revision = 2
	successor.ParentQuoteID = previous.QuoteID
	successor.ParentDigest = previous.CanonicalDigest
	sealed, err := fx.NewFXQuoteRevision(successor)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveQuoteSuccessor(context.Background(), tenant, sealed); err != nil {
		t.Fatal(err)
	}
	freshConn := db.NewConn(t)
	if _, err := freshConn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	fresh := New(freshConn)
	got, err := fresh.LoadQuote(context.Background(), tenant, sealed.QuoteID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || got.ParentQuoteID != previous.QuoteID || got.ParentDigest != previous.CanonicalDigest || got.CanonicalDigest != sealed.CanonicalDigest {
		t.Fatalf("successor recovery=%+v", got)
	}
}

func TestTodo_FX_003_Fault(t *testing.T) {
	db, _, tenant, _ := reversibleSchema(t)
	provider := db.Provider(t)
	if _, err := provider.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	var columns, constraints, triggers, policies int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='fx_quote_revision' AND column_name IN ('quote_revision','parent_quote_id','parent_digest','base_currency','quote_currency','rate','rate_scale','market_convention','confidence','as_of_submicrosecond','known_at_submicrosecond')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM pg_constraint WHERE conrelid='fx_quote_revision'::regclass AND conname IN ('fx_quote_revision_parent_pair','fx_quote_revision_parent_order','fx_quote_revision_parent_fk')`).Scan(&constraints); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM pg_trigger WHERE tgrelid='fx_quote_revision'::regclass AND NOT tgisinternal AND tgname IN ('fx_quote_revision_append_only','fx_quote_revision_successor_revision')`).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM pg_policies WHERE schemaname=current_schema() AND tablename='fx_quote_revision' AND policyname='tenant_isolation'`).Scan(&policies); err != nil {
		t.Fatal(err)
	}
	var rls, forceRLS bool
	if err := db.QueryRow(context.Background(), `SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='fx_quote_revision'::regclass`).Scan(&rls, &forceRLS); err != nil {
		t.Fatal(err)
	}
	if columns != 11 || constraints != 3 || triggers != 2 || policies != 1 || !rls || !forceRLS {
		t.Fatalf("restored schema columns=%d constraints=%d triggers=%d policies=%d rls=%t force_rls=%t", columns, constraints, triggers, policies, rls, forceRLS)
	}

	source := testSource(t, "rollback-source")
	appConn := db.NewConn(t)
	if _, err := appConn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	store := New(appConn)
	if err := store.SaveRateSource(context.Background(), tenant, source); err != nil {
		t.Fatal(err)
	}
	previous := testQuote(t, "rollback-parent", source.SourceID)
	if err := store.SaveQuote(context.Background(), tenant, previous); err != nil {
		t.Fatal(err)
	}
	successor := testQuote(t, "rollback-child", source.SourceID)
	successor.Revision = 2
	successor.ParentQuoteID = previous.QuoteID
	successor.ParentDigest = previous.CanonicalDigest
	successor.KnownAt = testInstant(t, "2026-06-01T12:00:00Z")
	if err := store.SaveQuoteSuccessor(context.Background(), tenant, successor); err != nil {
		t.Fatalf("successor after migration reapply: %v", err)
	}
	if _, err := db.Conn.Exec(context.Background(), `UPDATE fx_quote_revision SET quote_revision=99 WHERE tenant_id=$1 AND quote_id=$2`, tenant, previous.QuoteID); err == nil {
		t.Fatal("append-only trigger missing after migration reapply")
	}
}
