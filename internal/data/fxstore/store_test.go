package fxstore

import (
	"context"
	"errors"
	"strings"
	"sync"
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
	if gotProfile.RoundingRule != profile.RoundingRule || gotProfile.Tolerance != profile.Tolerance || strings.Join(gotProfile.FallbackSourceOrder, ",") != strings.Join(profile.FallbackSourceOrder, ",") || strings.Join(gotProfile.TriangulationCurrencies, ",") != "GBP" || gotProfile.Effective.Canonical() == nil {
		t.Fatalf("full profile policy projection = %+v", gotProfile)
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

func TestTodo_FX_002_LegacyProfileFailsClosed(t *testing.T) {
	db, store, tenant := testStore(t)
	profileID := "legacy-profile"
	if _, err := db.Conn.Exec(context.Background(), `INSERT INTO fx_conversion_profile_revision (row_id, tenant_id, profile_id, revision, canonical_digest) VALUES ($1,$2,$3,1,$4)`, uuid.New(), tenant, profileID, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadConversionProfile(context.Background(), tenant, profileID, 1); !errors.Is(err, fx.ErrStoreInvalid) {
		t.Fatalf("legacy incomplete profile = %v", err)
	}
	var scale *int32
	if err := db.QueryRow(context.Background(), `SELECT rounding_scale FROM fx_conversion_profile_revision WHERE tenant_id=$1 AND profile_id=$2`, tenant, profileID).Scan(&scale); err != nil {
		t.Fatal(err)
	}
	if scale != nil {
		t.Fatal("legacy row was backfilled")
	}
}

func TestTodo_FX_002_ProfileCanonicalTamperFailsClosed(t *testing.T) {
	db, store, tenant := testStore(t)
	profile := testProfile(t, "tamper-source")
	if _, err := db.Conn.Exec(context.Background(), `
		INSERT INTO fx_conversion_profile_revision
		(row_id, tenant_id, profile_id, revision, canonical_digest, rounding_scale, rounding_mode,
		 tolerance_nanoseconds, fallback_source_order, triangulation_currencies, effective_from, effective_from_submicrosecond)
		VALUES ($1,$2,$3,1,$4,2,'HALF_EVEN',$5,$6,$7,$8,0)`, uuid.New(), tenant, profile.ProfileID,
		strings.Repeat("0", 64), profile.Tolerance.Nanoseconds(), profile.FallbackSourceOrder,
		profile.TriangulationCurrencies, intervalStart(profile.Effective)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadConversionProfile(context.Background(), tenant, profile.ProfileID, 1); !errors.Is(err, fx.ErrStoreInvalid) {
		t.Fatalf("tampered canonical digest = %v", err)
	}
	invalidModeID := "invalid-rounding-mode"
	if _, err := db.Conn.Exec(context.Background(), `
		INSERT INTO fx_conversion_profile_revision
		(row_id, tenant_id, profile_id, revision, canonical_digest, rounding_scale, rounding_mode,
		 tolerance_nanoseconds, fallback_source_order, triangulation_currencies, effective_from, effective_from_submicrosecond)
		VALUES ($1,$2,$3,1,$4,2,'NOT_A_MODE',$5,$6,$7,$8,0)`, uuid.New(), tenant, invalidModeID,
		strings.Repeat("1", 64), profile.Tolerance.Nanoseconds(), profile.FallbackSourceOrder,
		profile.TriangulationCurrencies, intervalStart(profile.Effective)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadConversionProfile(context.Background(), tenant, invalidModeID, 1); !errors.Is(err, fx.ErrStoreInvalid) {
		t.Fatalf("invalid stored rounding mode = %v", err)
	}
}

func TestTodo_FX_002_ProfileSuccessorConcurrencyAndTenantIsolation(t *testing.T) {
	db, firstStore, tenantA := testStore(t)
	tenantB := uuid.NewString()
	insertTenant(t, db.Conn, tenantB)
	base := testProfile(t, "concurrent-source")
	if err := firstStore.SaveConversionProfile(context.Background(), tenantA, base); err != nil {
		t.Fatal(err)
	}
	if _, err := firstStore.LoadConversionProfile(context.Background(), tenantB, base.ProfileID, 1); !errors.Is(err, fx.ErrStoreNotFound) {
		t.Fatalf("cross-tenant profile read = %v", err)
	}

	makeSuccessor := func(revision uint64, scale int32) fx.ConversionProfileRevision {
		candidate := base
		candidate.Revision = revision
		candidate.ParentRevision = 1
		candidate.ParentDigest = base.CanonicalDigest
		candidate.RoundingRule.Scale = scale
		candidate.CanonicalDigest = ""
		sealed, err := fx.NewConversionProfileRevision(candidate)
		if err != nil {
			t.Fatal(err)
		}
		return sealed
	}
	secondConn := db.NewConn(t)
	if _, err := secondConn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	stores := []*Store{firstStore, New(secondConn)}
	candidates := []fx.ConversionProfileRevision{makeSuccessor(2, 3), makeSuccessor(3, 4)}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range stores {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = stores[i].SaveConversionProfile(context.Background(), tenantA, candidates[i])
		}(i)
	}
	wg.Wait()
	successes, stale := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, fx.ErrStoreStaleCAS):
			stale++
		default:
			t.Fatalf("unexpected concurrent successor result: %v", err)
		}
	}
	if successes != 1 || stale != 1 {
		t.Fatalf("concurrent successors: successes=%d stale=%d errors=%v", successes, stale, errs)
	}
	for i, candidate := range candidates {
		if errs[i] != nil {
			continue
		}
		loaded, err := firstStore.LoadConversionProfile(context.Background(), tenantA, candidate.ProfileID, candidate.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.ParentDigest != base.CanonicalDigest || loaded.CanonicalDigest != candidate.CanonicalDigest || loaded.Effective.Kind() != values.IntervalKindInstant {
			t.Fatalf("successor round trip = %+v", loaded)
		}
	}
}

func TestTodo_FX_002_ProfileExactPolicyRoundTrip(t *testing.T) {
	_, store, tenant := testStore(t)
	start := testInstant(t, "2026-01-01T00:00:00.123456789Z")
	end := testInstant(t, "2026-12-31T23:59:59.987654321Z")
	interval, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := fx.NewConversionProfileRevision(fx.ConversionProfileRevision{
		ProfileID: "exact-policy", Revision: 1,
		RoundingRule: fx.RoundingRule{Scale: 18, Mode: values.RoundingHalfAwayFromZero},
		Tolerance:    123456789 * time.Nanosecond, FallbackSourceOrder: []string{"second", "first"},
		TriangulationCurrencies: []string{"JPY", "GBP"}, Effective: interval,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConversionProfile(context.Background(), tenant, profile); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadConversionProfile(context.Background(), tenant, profile.ProfileID, profile.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != profile.CanonicalDigest || got.Tolerance != profile.Tolerance || got.RoundingRule != profile.RoundingRule || got.Effective.String() != profile.Effective.String() || strings.Join(got.FallbackSourceOrder, ",") != "second,first" || strings.Join(got.TriangulationCurrencies, ",") != "JPY,GBP" {
		t.Fatalf("exact policy round trip = %+v", got)
	}
	if err := (*Store)(nil).SaveConversionProfile(context.Background(), tenant, profile); !errors.Is(err, fx.ErrStoreInvalid) {
		t.Fatalf("nil store = %v", err)
	}
	if err := store.SaveConversionProfile(context.Background(), "not-a-tenant", profile); !errors.Is(err, fx.ErrStoreInvalid) {
		t.Fatalf("invalid tenant = %v", err)
	}
	if _, err := store.LoadConversionProfile(context.Background(), tenant, "missing", 1); !errors.Is(err, fx.ErrStoreNotFound) {
		t.Fatalf("missing profile = %v", err)
	}
	if err := store.SaveConversionProfile(context.Background(), tenant, fx.ConversionProfileRevision{}); !errors.Is(err, fx.ErrStoreInvalid) {
		t.Fatalf("invalid profile = %v", err)
	}
}

func TestTodo_FX_002_ProfileSubmicrosecondIntervalOrdering(t *testing.T) {
	db, store, tenant := testStore(t)
	start := testInstant(t, "2026-01-01T00:00:00.123456100Z")
	end := testInstant(t, "2026-01-01T00:00:00.123456900Z")
	interval, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := fx.NewConversionProfileRevision(fx.ConversionProfileRevision{
		ProfileID: "submicrosecond-policy", Revision: 1,
		RoundingRule: fx.RoundingRule{Scale: 2, Mode: values.RoundingHalfEven},
		Tolerance:    time.Nanosecond, FallbackSourceOrder: []string{"source"}, Effective: interval,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConversionProfile(context.Background(), tenant, profile); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadConversionProfile(context.Background(), tenant, profile.ProfileID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Effective.String() != profile.Effective.String() || got.CanonicalDigest != profile.CanonicalDigest {
		t.Fatalf("same-microsecond interval round trip = %+v", got)
	}

	_, err = db.Conn.Exec(context.Background(), `
		INSERT INTO fx_conversion_profile_revision
		(row_id, tenant_id, profile_id, revision, canonical_digest, rounding_scale, rounding_mode,
		 tolerance_nanoseconds, fallback_source_order, triangulation_currencies, effective_from, effective_to,
		 effective_from_submicrosecond, effective_to_submicrosecond)
		VALUES ($1,$2,'reversed-submicrosecond',1,$3,2,'HALF_EVEN',1,$4,$5,$6,$6,900,100)`,
		uuid.New(), tenant, strings.Repeat("2", 64), []string{"source"}, []string{},
		time.Date(2026, 1, 1, 0, 0, 0, 123456000, time.UTC))
	if err == nil {
		t.Fatal("reversed same-microsecond interval was accepted")
	}
}

func TestTodo_FX_003_PostgresSuccessorCASRecoveryAndTenantIsolation(t *testing.T) {
	db, firstStore, tenantA := testStore(t)
	tenantB := uuid.NewString()
	insertTenant(t, db.Conn, tenantB)
	source := testSource(t, "fx003-source")
	if err := firstStore.SaveRateSource(context.Background(), tenantA, source); err != nil {
		t.Fatal(err)
	}
	if err := firstStore.SaveRateSource(context.Background(), tenantB, source); err != nil {
		t.Fatal(err)
	}
	base := testQuote(t, "fx003-base", source.SourceID)
	base.AsOf = testInstant(t, "2026-06-01T10:00:00.123456789Z")
	base.EffectiveAt = base.AsOf
	base.ObservedAt = base.AsOf
	base.KnownAt = testInstant(t, "2026-06-01T11:00:00.987654321Z")
	base.CanonicalDigest = ""
	var sealErr error
	base, sealErr = fx.NewFXQuoteRevision(base)
	if sealErr != nil {
		t.Fatal(sealErr)
	}
	if err := firstStore.SaveQuote(context.Background(), tenantA, base); err != nil {
		t.Fatal(err)
	}
	loaded, err := firstStore.LoadQuote(context.Background(), tenantA, base.QuoteID)
	if err != nil || loaded.CanonicalDigest != base.CanonicalDigest || loaded.Rate.String() != base.Rate.String() || loaded.AsOf.Compare(base.AsOf) != 0 || loaded.KnownAt.Compare(base.KnownAt) != 0 {
		t.Fatalf("exact base recovery = %+v, err=%v", loaded, err)
	}

	makeSuccessor := func(id, rateText string) fx.FXQuoteRevision {
		rate, parseErr := values.NewDecimal(rateText, 6, values.RoundingExactRequired)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		candidate := base
		candidate.QuoteID = id
		candidate.Revision = 2
		candidate.ParentQuoteID = base.QuoteID
		candidate.ParentDigest = base.CanonicalDigest
		candidate.Rate = rate
		candidate.KnownAt = testInstant(t, "2026-06-01T12:00:00.111222333Z")
		candidate.CanonicalDigest = ""
		sealed, sealErr := fx.NewFXQuoteRevision(candidate)
		if sealErr != nil {
			t.Fatal(sealErr)
		}
		return sealed
	}
	if err := firstStore.SaveQuote(context.Background(), tenantA, makeSuccessor("fx003-bypass", "0.900000")); !errors.Is(err, fx.ErrStoreStaleCAS) {
		t.Fatalf("generic successor bypass = %v", err)
	}
	if err := firstStore.SaveQuoteSuccessor(context.Background(), tenantB, makeSuccessor("fx003-cross-tenant", "0.910000")); !errors.Is(err, fx.ErrStoreStaleCAS) {
		t.Fatalf("cross-tenant predecessor = %v", err)
	}

	secondConn := db.NewConn(t)
	if _, err := secondConn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	candidates := []fx.FXQuoteRevision{makeSuccessor("fx003-next-a", "0.923400"), makeSuccessor("fx003-next-b", "0.923300")}
	stores := []*Store{firstStore, New(secondConn)}
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for i := range stores {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = stores[i].SaveQuoteSuccessor(context.Background(), tenantA, candidates[i])
		}(i)
	}
	wg.Wait()
	winners, stale := 0, 0
	for _, saveErr := range errs {
		if saveErr == nil {
			winners++
		} else if errors.Is(saveErr, fx.ErrStoreStaleCAS) {
			stale++
		} else {
			t.Fatalf("unexpected successor error: %v", saveErr)
		}
	}
	if winners != 1 || stale != 1 {
		t.Fatalf("successor CAS winners=%d stale=%d errors=%v", winners, stale, errs)
	}
	freshConn := db.NewConn(t)
	if _, err := freshConn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	fresh := New(freshConn)
	for i, candidate := range candidates {
		if errs[i] != nil {
			continue
		}
		got, loadErr := fresh.LoadQuote(context.Background(), tenantA, candidate.QuoteID)
		if loadErr != nil || got.CanonicalDigest != candidate.CanonicalDigest || got.ParentDigest != base.CanonicalDigest || got.Rate.String() != candidate.Rate.String() || got.KnownAt.Compare(candidate.KnownAt) != 0 {
			t.Fatalf("successor recovery = %+v, err=%v", got, loadErr)
		}
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
		FallbackSourceOrder: []string{source}, TriangulationCurrencies: []string{"GBP"}, Effective: testInterval(t),
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
