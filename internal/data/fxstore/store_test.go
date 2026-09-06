package fxstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/domains/fx"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_PERSIST_FX_001(t *testing.T) {
	db, store, tenant := testStore(t)
	source := testSource(t, "primary-source")
	profile := testProfile(t, source.SourceID)
	quote := testQuote(t, "primary-quote", source.SourceID)
	if err := store.SaveRateSource(context.Background(), tenant, source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConversionProfile(context.Background(), tenant, profile); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveQuote(context.Background(), tenant, quote); err != nil {
		t.Fatal(err)
	}
	var sources, quotes, profiles int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM fx_rate_source_revision`).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM fx_quote_revision`).Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM fx_conversion_profile_revision`).Scan(&profiles); err != nil {
		t.Fatal(err)
	}
	if sources != 1 || quotes != 1 || profiles != 1 {
		t.Fatalf("stored rows = source %d quote %d profile %d", sources, quotes, profiles)
	}
}

func TestTodo_PERSIST_FX_001_Fault(t *testing.T) {
	_, store, tenant := testStore(t)
	source := testSource(t, "fault-source")
	if err := store.SaveRateSource(context.Background(), tenant, source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRateSource(context.Background(), tenant, source); !errors.Is(err, fx.ErrStoreDuplicateRevision) {
		t.Fatalf("duplicate revision = %v, want typed duplicate", err)
	}
	stale, err := fx.NewRateSourceRevision(fx.RateSourceRevision{
		SourceID: source.SourceID, Revision: 2, ParentRevision: 1, ParentDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		ProviderRef: source.ProviderRef, Pairs: source.Pairs, QuoteCadence: source.QuoteCadence,
		AuthorityClass: source.AuthorityClass, Effective: source.Effective,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRateSource(context.Background(), tenant, stale); !errors.Is(err, fx.ErrStoreStaleCAS) {
		t.Fatalf("stale revision = %v, want typed stale CAS", err)
	}
	missing := testQuote(t, "missing-source-quote", "not-recorded")
	if err := store.SaveQuote(context.Background(), tenant, missing); !errors.Is(err, fx.ErrStoreReferenceNotFound) {
		t.Fatalf("missing source = %v, want typed reference refusal", err)
	}
}

func TestTodo_PERSIST_FX_001_Integration(t *testing.T) {
	_, store, tenant := testStore(t)
	source := testSource(t, "integration-source")
	profile := testProfile(t, source.SourceID)
	quote := testQuote(t, "integration-quote", source.SourceID)
	if err := store.SaveRateSource(context.Background(), tenant, source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConversionProfile(context.Background(), tenant, profile); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveQuote(context.Background(), tenant, quote); err != nil {
		t.Fatal(err)
	}
	gotSource, err := store.LoadRateSource(context.Background(), tenant, source.SourceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotSource.SourceID != source.SourceID || gotSource.Revision != source.Revision || gotSource.CanonicalDigest != source.CanonicalDigest {
		t.Fatalf("source projection = %+v", gotSource)
	}
	gotQuote, err := store.LoadQuote(context.Background(), tenant, quote.QuoteID)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuote.QuoteID != quote.QuoteID || gotQuote.SourceID != quote.SourceID || gotQuote.CanonicalDigest != quote.CanonicalDigest {
		t.Fatalf("quote projection = %+v", gotQuote)
	}
	gotProfile, err := store.LoadConversionProfile(context.Background(), tenant, profile.ProfileID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotProfile.ProfileID != profile.ProfileID || gotProfile.CanonicalDigest != profile.CanonicalDigest {
		t.Fatalf("profile projection = %+v", gotProfile)
	}
}

func TestTodo_PERSIST_FX_001_Security(t *testing.T) {
	db, store, tenantA := testStore(t)
	tenantB := uuid.NewString()
	insertTenant(t, db.Conn, tenantB)
	source := testSource(t, "security-source")
	if err := store.SaveRateSource(context.Background(), tenantA, source); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadRateSource(context.Background(), tenantB, source.SourceID, source.Revision); !errors.Is(err, fx.ErrStoreNotFound) {
		t.Fatalf("cross-tenant source read = %v, want not found under RLS", err)
	}
	if _, err := store.LoadQuote(context.Background(), tenantB, "security-quote"); !errors.Is(err, fx.ErrStoreNotFound) {
		t.Fatalf("cross-tenant quote read = %v, want not found under RLS", err)
	}
}

func TestTodo_PERSIST_FX_001_Recovery(t *testing.T) {
	db, store, tenant := testStore(t)
	source := testSource(t, "recovery-source")
	if err := store.SaveRateSource(context.Background(), tenant, source); err != nil {
		t.Fatal(err)
	}
	freshConn := db.NewConn(t)
	if _, err := freshConn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	fresh := New(freshConn)
	got, err := fresh.LoadRateSource(context.Background(), tenant, source.SourceID, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != source.CanonicalDigest {
		t.Fatalf("fresh connection digest = %q, want %q", got.CanonicalDigest, source.CanonicalDigest)
	}
}

func TestTodo_PERSIST_FX_001_Mutation(t *testing.T) {
	db, store, tenant := testStore(t)
	source := testSource(t, "mutation-source")
	profile := testProfile(t, source.SourceID)
	if err := store.SaveRateSource(context.Background(), tenant, source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConversionProfile(context.Background(), tenant, profile); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Conn.Exec(context.Background(), `UPDATE fx_rate_source_revision SET revision=99 WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("source revision update succeeded")
	}
	if _, err := db.Conn.Exec(context.Background(), `DELETE FROM fx_conversion_profile_revision WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("conversion profile revision delete succeeded")
	}
}

func testStore(t *testing.T) (*pgtest.DB, *Store, string) {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.NewString()
	insertTenant(t, db.Conn, tenant)
	app := db.NewConn(t)
	if _, err := app.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	return db, New(app), tenant
}

func insertTenant(t *testing.T, exec interface {
	Exec(context.Context, string, ...any) (int64, error)
}, tenant string) {
	t.Helper()
	if _, err := exec.Exec(context.Background(), `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-fx', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "fx-"+tenant, "fx tenant "+tenant); err != nil {
		t.Fatal(err)
	}
}

func testInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(parsed)
}

func testInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewOpenInstantInterval(testInstant(t, "2026-01-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func testSource(t *testing.T, id string) fx.RateSourceRevision {
	t.Helper()
	pair, err := fx.NewCurrencyPair("USD", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	source, err := fx.NewRateSourceRevision(fx.RateSourceRevision{
		SourceID: id, Revision: 1, ProviderRef: "provider:" + id, Pairs: []fx.CurrencyPair{pair},
		QuoteCadence: fx.CadenceHourly, AuthorityClass: fx.AuthorityPrimary, Effective: testInterval(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func testProfile(t *testing.T, source string) fx.ConversionProfileRevision {
	t.Helper()
	profile, err := fx.NewConversionProfileRevision(fx.ConversionProfileRevision{
		ProfileID: "profile-" + source, Revision: 1,
		RoundingRule: fx.RoundingRule{Scale: 2, Mode: values.RoundingHalfEven}, Tolerance: 2 * time.Hour,
		FallbackSourceOrder: []string{source}, Effective: testInterval(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func testQuote(t *testing.T, id, source string) fx.FXQuoteRevision {
	t.Helper()
	rate, err := values.NewDecimal("0.923456", 6, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	quote, err := fx.NewFXQuoteRevision(fx.FXQuoteRevision{
		QuoteID: id, SourceID: source, SourceRevision: 1, BaseCurrency: "USD", QuoteCurrency: "EUR",
		Rate: rate, AsOf: testInstant(t, "2026-06-01T10:00:00Z"), KnownAt: testInstant(t, "2026-06-01T11:00:00Z"),
		MarketConvention: fx.ConventionSpot, Confidence: fx.ConfidenceHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	return quote
}
