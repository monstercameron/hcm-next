package contentregistrystore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/contentregistrystore"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/industrypack"
	"github.com/monstercameron/hcm-next/internal/domains/knowledge"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var contentRegistryTime = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 82); err != nil {
		t.Fatalf("apply migrations through 00082: %v", err)
	}
	return db
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func tenantTxErr(conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func tenantTx(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	if err := tenantTxErr(conn, tenantID, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func manifest(t *testing.T, id string, version int) industrypack.IndustryPack {
	t.Helper()
	pack, err := industrypack.NewIndustryPack(industrypack.IndustryPack{
		PackID: id, Version: version, Industry: industrypack.IndustryHealthcare,
		Owner: "hcmnext", Scope: "healthcare-us", Support: "maintained",
		Compatibility: []industrypack.CompatibilityDeclaration{{Component: "hcmnext", MinimumVersion: "1", MaximumVersion: "2"}},
	})
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return pack
}

func contentFixture(t *testing.T) industrypack.Content {
	t.Helper()
	start, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	return industrypack.Content{
		Ref:       industrypack.ContentRef{Kind: industrypack.ContentReferenceData, Namespace: "healthcare", ID: "job-family", Version: "1"},
		Effective: industrypack.OpenWindow(start), Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
	}
}

func bindingFixture(t *testing.T, pack industrypack.IndustryPack, content industrypack.Content) industrypack.Binding {
	t.Helper()
	binding, err := industrypack.Bind(industrypack.BindingSpec{Packs: []industrypack.Pack{{
		Manifest: pack, References: []industrypack.ContentRef{content.Ref}, Contents: []industrypack.Content{content},
	}}})
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	return binding
}

func articleFixture(t *testing.T, id string) knowledge.ArticleRevision {
	t.Helper()
	instant := func(text string) values.Instant {
		at, err := time.Parse(time.RFC3339, text)
		if err != nil {
			t.Fatal(err)
		}
		return values.NewInstant(at)
	}
	return knowledge.ArticleRevision{
		ArticleID: id, Revision: 1, Locale: "en-US", AudienceScope: "EMPLOYEES",
		Classification: "INTERNAL", Owner: "policy-owner", SourceAuthority: "policy-authority",
		SourceRefs:   []knowledge.SourceRef{{System: "policy", Identifier: "source-1", Authority: "authority-1"}},
		Jurisdiction: "US", EffectiveInterval: knowledge.EffectiveInterval{EffectiveFrom: instant("2026-01-01T00:00:00Z"), EffectiveTo: instant("2027-01-01T00:00:00Z")},
		KnownInterval: knowledge.KnownInterval{KnownFrom: instant("2025-12-01T00:00:00Z"), KnownTo: instant("2026-01-01T00:00:00Z")},
		BodyDigest:    "sha256:2222222222222222222222222222222222222222222222222222222222222222", Title: "Leave policy", Summary: "Summary",
		Review: knowledge.ReviewMetadata{ReviewedBy: "reviewer", ReviewedAt: instant("2026-01-02T00:00:00Z"), ExpiresAt: instant("2027-01-02T00:00:00Z"), ApprovalRef: "approval-1"},
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001(t *testing.T) {
	db := newDB(t)
	adminStore := contentregistrystore.New(db.Conn)
	pack := manifest(t, "healthcare-primary", 1)
	content := contentFixture(t)
	if err := adminStore.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	if err := adminStore.SaveContent(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	if _, err := adminStore.LoadManifest(context.Background(), pack.PackID, pack.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := adminStore.LoadContent(context.Background(), content.Ref); err != nil {
		t.Fatal(err)
	}

	tenantID := insertTenant(t, db, "content-primary")
	store := contentregistrystore.New(appConn(t, db))
	binding := bindingFixture(t, pack, content)
	article := articleFixture(t, "article-primary")
	if err := store.SaveBinding(context.Background(), tenantID.String(), binding); err != nil {
		t.Fatalf("save binding: %v", err)
	}
	if err := store.SaveArticle(context.Background(), tenantID.String(), article); err != nil {
		t.Fatalf("save article: %v", err)
	}
	localized := knowledge.LocalizedRevision{ArticleID: article.ArticleID, Revision: 1, Locale: "es-US", Reviewer: "translator", SourceRevisionDigest: article.Digest(), TranslationProvenance: knowledge.ProvenanceHuman, BodyDigest: "sha256:3333333333333333333333333333333333333333333333333333333333333333", Title: "Política de licencia", Summary: "Resumen", CreatedAt: contentRegistryTime}
	if err := store.SaveLocalized(context.Background(), tenantID.String(), localized); err != nil {
		t.Fatalf("save localized: %v", err)
	}
	event := knowledge.LifecycleEvent{EventID: "event-primary", Kind: knowledge.EventDrafted, State: knowledge.StateDraft, ArticleID: article.ArticleID, Revision: 1, Locale: article.Locale, At: contentRegistryTime}
	if err := store.AppendLifecycleEvent(context.Background(), tenantID.String(), event, 1); err != nil {
		t.Fatalf("append event: %v", err)
	}
	activation := knowledge.ActivationBinding{ArticleID: article.ArticleID, Revision: 1, Locale: article.Locale, BundleID: "bundle-1", BundleDigest: "sha256:4444444444444444444444444444444444444444444444444444444444444444", ActivationEpoch: 1, ActivatedAt: contentRegistryTime}
	if err := store.SaveActivation(context.Background(), tenantID.String(), activation); err != nil {
		t.Fatalf("save activation: %v", err)
	}
	gotBinding, err := store.LoadBinding(context.Background(), tenantID.String(), binding.CanonicalDigest)
	if err != nil || gotBinding.CanonicalDigest != binding.CanonicalDigest {
		t.Fatalf("load binding=%+v err=%v", gotBinding, err)
	}
	gotArticle, err := store.LoadArticle(context.Background(), tenantID.String(), article.ArticleID, 1)
	if err != nil || gotArticle.Digest() != article.Digest() {
		t.Fatalf("load article digest=%s err=%v want=%s", gotArticle.Digest(), err, article.Digest())
	}
	gotEvents, err := store.ListLifecycleEvents(context.Background(), tenantID.String(), article.ArticleID)
	if err != nil || len(gotEvents) != 1 {
		t.Fatalf("events=%+v err=%v", gotEvents, err)
	}
	gotActivation, err := store.LoadActivation(context.Background(), tenantID.String(), article.ArticleID, article.Locale)
	if err != nil || gotActivation.ActivationEpoch != 1 {
		t.Fatalf("activation=%+v err=%v", gotActivation, err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Integration(t *testing.T) {
	db := newDB(t)
	pack := manifest(t, "healthcare-integration", 1)
	content := contentFixture(t)
	admin := contentregistrystore.New(db.Conn)
	if err := admin.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	if err := admin.SaveContent(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	tenantID := insertTenant(t, db, "content-integration")
	store := contentregistrystore.New(appConn(t, db))
	if err := store.SaveBinding(context.Background(), tenantID.String(), bindingFixture(t, pack, content)); err != nil {
		t.Fatal(err)
	}
	fresh := contentregistrystore.New(appConn(t, db))
	if _, err := fresh.LoadBinding(context.Background(), tenantID.String(), bindingFixture(t, pack, content).CanonicalDigest); err != nil {
		t.Fatalf("fresh connection load: %v", err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Security(t *testing.T) {
	db := newDB(t)
	pack := manifest(t, "healthcare-security", 1)
	content := contentFixture(t)
	admin := contentregistrystore.New(db.Conn)
	if err := admin.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	if err := admin.SaveContent(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	alpha, beta := insertTenant(t, db, "content-alpha"), insertTenant(t, db, "content-beta")
	binding := bindingFixture(t, pack, content)
	store := contentregistrystore.New(appConn(t, db))
	if err := store.SaveBinding(context.Background(), alpha.String(), binding); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tenantTxErr(appConn(t, db), beta, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM industry_pack_binding WHERE tenant_id=$1`, alpha).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cross-tenant binding count=%d", count)
	}
	if _, err := store.LoadBinding(context.Background(), beta.String(), binding.CanonicalDigest); !errors.Is(err, contentregistrystore.ErrNotFound) {
		t.Fatalf("cross-tenant load=%v", err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Recovery(t *testing.T) {
	db := newDB(t)
	pack := manifest(t, "healthcare-recovery", 1)
	content := contentFixture(t)
	admin := contentregistrystore.New(db.Conn)
	if err := admin.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	if err := admin.SaveContent(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	tenantID := insertTenant(t, db, "content-recovery")
	binding := bindingFixture(t, pack, content)
	if err := contentregistrystore.New(appConn(t, db)).SaveBinding(context.Background(), tenantID.String(), binding); err != nil {
		t.Fatal(err)
	}
	if _, err := contentregistrystore.New(appConn(t, db)).LoadBinding(context.Background(), tenantID.String(), binding.CanonicalDigest); err != nil {
		t.Fatalf("reload after fresh connection: %v", err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Fault(t *testing.T) {
	db := newDB(t)
	pack := manifest(t, "healthcare-fault", 1)
	admin := contentregistrystore.New(db.Conn)
	if err := admin.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	tenantID := insertTenant(t, db, "content-fault")
	store := contentregistrystore.New(appConn(t, db))
	article := articleFixture(t, "article-fault")
	if err := store.SaveArticle(context.Background(), tenantID.String(), article); err != nil {
		t.Fatal(err)
	}
	err := store.SaveArticle(context.Background(), tenantID.String(), article)
	var typed *contentregistrystore.Error
	if !errors.Is(err, contentregistrystore.ErrDuplicate) || !errors.As(err, &typed) || typed.Code != contentregistrystore.CodeDuplicate {
		t.Fatalf("duplicate=%v typed=%+v", err, typed)
	}
	activation := knowledge.ActivationBinding{ArticleID: article.ArticleID, Revision: 1, Locale: article.Locale, ActivationEpoch: 2, ActivatedAt: contentRegistryTime}
	if err := store.SaveActivation(context.Background(), tenantID.String(), activation); err != nil {
		t.Fatal(err)
	}
	activation.ActivationEpoch = 1
	err = store.SaveActivation(context.Background(), tenantID.String(), activation)
	if !errors.Is(err, contentregistrystore.ErrStaleCAS) {
		t.Fatalf("stale activation=%v", err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "content-mutation")
	store := contentregistrystore.New(appConn(t, db))
	event := knowledge.LifecycleEvent{EventID: "event-mutation", Kind: knowledge.EventDrafted, State: knowledge.StateDraft, ArticleID: "article-mutation", Revision: 1, At: contentRegistryTime}
	if err := store.AppendLifecycleEvent(context.Background(), tenantID.String(), event, 1); err != nil {
		t.Fatal(err)
	}
	conn := appConn(t, db)
	for _, statement := range []string{
		`UPDATE knowledge_lifecycle_event SET state='RETIRED' WHERE tenant_id=$1`,
		`DELETE FROM knowledge_lifecycle_event WHERE tenant_id=$1`,
	} {
		if err := tenantTxErr(conn, tenantID, func(tx dbport.Tx) error { _, err := tx.Exec(context.Background(), statement, tenantID); return err }); err == nil {
			t.Fatalf("mutation accepted: %s", statement)
		}
	}
}
