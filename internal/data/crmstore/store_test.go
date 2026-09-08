package crmstore_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/crmstore"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/crm"
	"github.com/monstercameron/hcm-next/internal/engines/population"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type storedCampaignAudienceOwner struct {
	pool       crm.PoolRevision
	definition population.Definition
	snapshot   population.Snapshot
}

func (v storedCampaignAudienceOwner) VerifyCampaignAudience(_ context.Context, claim crm.CampaignAudienceClaim) error {
	if claim.Tenant != v.pool.PoolID.Tenant || claim.Purpose != v.pool.Purpose || claim.PoolID != v.pool.PoolID || !claim.PoolRevision.Equal(v.pool.Revision) || claim.PopulationOwner != v.definition.Owner || claim.PopulationDefinitionID != v.definition.ID || claim.PopulationDefinitionDigest != v.snapshot.DefinitionDigest || claim.PopulationRevision != v.snapshot.RevisionVersion || claim.PopulationDigest != v.snapshot.Digest || claim.CampaignDigest == "" || !reflect.DeepEqual(claim.Pool, v.pool) || !reflect.DeepEqual(claim.Definition, v.definition) || !reflect.DeepEqual(claim.Snapshot, v.snapshot) {
		return errors.New("stored audience basis mismatch")
	}
	return nil
}

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	// Migrations 00071-00083 are outside this lane; 00082 currently has a
	// pre-existing unterminated dollar-quoted function. Apply the known-good
	// substrate through 00070, then exercise this lane's numbered migration
	// directly so the CRM tests remain isolated from that unrelated failure.
	if _, err := db.Provider(t).UpTo(context.Background(), 70); err != nil {
		t.Fatalf("apply migrations through 00070: %v", err)
	}
	if _, err := db.Provider(t).ApplyVersion(context.Background(), 84, true); err != nil {
		t.Fatalf("apply migration 00084: %v", err)
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
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func inTenant(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func tenantValue(id uuid.UUID) values.TenantId { return values.TenantId(id.String()) }

func ref(tenant values.TenantId, kind string, id uuid.UUID) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: values.Kind(kind), Id: id.String()}
}

func revision(t *testing.T, number uint64) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision("crm", number)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func effective(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func pool(t *testing.T, tenant values.TenantId, id uuid.UUID, version uint64) crm.TalentPoolRevision {
	t.Helper()
	poolID := ref(tenant, "talent_pool", id)
	return crm.TalentPoolRevision{
		PoolID: poolID, Revision: revision(t, version), Purpose: "future recruiting", Criteria: "skills:go",
		Source:  crm.SourceAttribution{System: "ats", Reference: "import-1", RecordedBy: ref(tenant, "principal", uuid.New())},
		Consent: crm.ConsentAuthority{Authority: ref(tenant, "processing_authority", uuid.New()), Basis: "consent", Evidence: ref(tenant, "evidence", uuid.New())},
		Scope:   crm.Scope{Organization: ref(tenant, "organization", uuid.New())}, Effective: effective(t),
		Owner: ref(tenant, "owner", uuid.New()), RemovalPolicy: crm.RemovalAtIntervalEnd,
	}
}

func membership(t *testing.T, tenant values.TenantId, membershipID, poolID uuid.UUID, version uint64) crm.TalentPoolMembershipRevision {
	t.Helper()
	return crm.TalentPoolMembershipRevision{
		MembershipID: ref(tenant, "talent_pool_membership", membershipID), Revision: revision(t, version),
		Pool: ref(tenant, "talent_pool", poolID), Subject: ref(tenant, "candidate", uuid.New()), Role: crm.MembershipCandidate,
		Purpose: "future recruiting", Source: crm.SourceAttribution{System: "ats", Reference: "import-1", RecordedBy: ref(tenant, "principal", uuid.New())},
		Consent: crm.ConsentAuthority{Authority: ref(tenant, "processing_authority", uuid.New()), Basis: "consent", Evidence: ref(tenant, "evidence", uuid.New())},
		Scope:   crm.Scope{Organization: ref(tenant, "organization", uuid.New())}, Effective: effective(t),
		Owner: ref(tenant, "owner", uuid.New()), RemovalPolicy: crm.RemovalAtIntervalEnd,
	}
}

func TestTodo_PERSIST_CRM_001(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "crm-primary")
	key := tenantValue(id)
	store := crmstore.New(appConn(t, db))
	poolID, membershipID := uuid.New(), uuid.New()
	p := pool(t, key, poolID, 1)
	m := membership(t, key, membershipID, poolID, 1)
	if err := store.PutPool(context.Background(), key, p); err != nil {
		t.Fatal(err)
	}
	if err := store.PutMembership(context.Background(), key, m); err != nil {
		t.Fatal(err)
	}
	gotPool, err := store.GetPool(context.Background(), key, poolID.String(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotPool.Criteria != p.Criteria || gotPool.Consent.Basis != p.Consent.Basis || gotPool.Effective.String() != p.Effective.String() {
		t.Fatalf("pool round trip lost governed fields: got=%+v want=%+v", gotPool, p)
	}
	gotMembership, err := store.GetMembership(context.Background(), key, membershipID.String(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotMembership.Pool.Id != m.Pool.Id || gotMembership.Subject.Kind != m.Subject.Kind || gotMembership.Effective.String() != m.Effective.String() {
		t.Fatalf("membership round trip lost references: got=%+v want=%+v", gotMembership, m)
	}
}

func TestTodo_PERSIST_CRM_001_Fault(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "crm-fault")
	key := tenantValue(id)
	store := crmstore.New(appConn(t, db))
	p := pool(t, key, uuid.New(), 1)
	if err := store.PutPool(context.Background(), key, p); err != nil {
		t.Fatal(err)
	}
	duplicateErr := store.PutPool(context.Background(), key, p)
	var duplicate *crm.StoreError
	if !errors.As(duplicateErr, &duplicate) || duplicate.Code != crm.StoreDuplicateCode {
		t.Fatalf("duplicate error=%v, want typed duplicate revision", duplicateErr)
	}
	next := p
	next.Revision = revision(t, 2)
	staleErr := store.PutPool(context.Background(), key, next, 0)
	var stale *crm.StoreError
	if !errors.As(staleErr, &stale) || stale.Code != crm.StoreStaleCASCode {
		t.Fatalf("stale error=%v, want typed stale CAS", staleErr)
	}
}

func TestTodo_PERSIST_CRM_001_Integration(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "crm-integration")
	key := tenantValue(id)
	store := crmstore.New(appConn(t, db))
	p := pool(t, key, uuid.New(), 1)
	if err := store.PutPool(context.Background(), key, p); err != nil {
		t.Fatal(err)
	}
	second := p
	second.Revision = revision(t, 2)
	second.Criteria = "skills:go,sql"
	if err := store.PutPool(context.Background(), key, second, 1); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListPoolVersions(context.Background(), key, p.PoolID.Id)
	if err != nil || len(versions) != 2 || versions[1].Criteria != second.Criteria {
		t.Fatalf("pool versions=%v err=%v, want two immutable revisions", versions, err)
	}
	if err := store.PutMembership(context.Background(), key, membership(t, key, uuid.New(), uuid.MustParse(p.PoolID.Id), 1)); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_CRM_001_Security(t *testing.T) {
	db := newDB(t)
	a, b := insertTenant(t, db, "crm-alpha"), insertTenant(t, db, "crm-beta")
	store := crmstore.New(appConn(t, db))
	if err := store.PutPool(context.Background(), tenantValue(a), pool(t, tenantValue(a), uuid.New(), 1)); err != nil {
		t.Fatal(err)
	}
	betaPool := pool(t, tenantValue(b), uuid.New(), 1)
	if err := store.PutPool(context.Background(), tenantValue(b), betaPool); err != nil {
		t.Fatal(err)
	}
	conn := appConn(t, db)
	var count int
	inTenant(t, conn, a, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM talent_pool_revision WHERE tenant_id=$1`, b).Scan(&count)
	})
	if count != 0 {
		t.Fatalf("tenant A saw %d tenant B pool rows under RLS", count)
	}
}

func TestTodo_PERSIST_CRM_001_Recovery(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "crm-recovery")
	key := tenantValue(id)
	p := pool(t, key, uuid.New(), 1)
	if err := crmstore.New(appConn(t, db)).PutPool(context.Background(), key, p); err != nil {
		t.Fatal(err)
	}
	got, err := crmstore.New(appConn(t, db)).GetPool(context.Background(), key, p.PoolID.Id, 1)
	if err != nil || got.Criteria != p.Criteria || got.PoolID.Id != p.PoolID.Id {
		t.Fatalf("fresh connection load=%+v err=%v", got, err)
	}
}

func TestTodo_PERSIST_CRM_001_Mutation(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "crm-mutation")
	key := tenantValue(id)
	store := crmstore.New(appConn(t, db))
	p := pool(t, key, uuid.New(), 1)
	if err := store.PutPool(context.Background(), key, p); err != nil {
		t.Fatal(err)
	}
	m := membership(t, key, uuid.New(), uuid.MustParse(p.PoolID.Id), 1)
	if err := store.PutMembership(context.Background(), key, m); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE talent_pool_revision SET purpose='changed' WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("talent pool revision accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM talent_pool_revision WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("talent pool revision accepted DELETE")
	}
	if err := db.ExecErr(`UPDATE talent_pool_membership_revision SET purpose='changed' WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("membership revision accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM talent_pool_membership_revision WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("membership revision accepted DELETE")
	}
}

func TestTodo_CRM_003_Integration(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "crm-campaign")
	tenant := tenantValue(tenantID)
	store := crmstore.New(appConn(t, db))
	persisted := pool(t, tenant, uuid.New(), 1)
	if err := store.PutPool(context.Background(), tenant, persisted); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetPool(context.Background(), tenant, persisted.PoolID.Id, 1)
	if err != nil {
		t.Fatal(err)
	}
	definition := population.Definition{ID: "recruiting-audience", Owner: "population-owner", Subject: population.SubjectWorker, Scope: population.Scope{Tenant: tenant, OrganizationScopeRef: loaded.Scope.Organization.String(), Purpose: loaded.Purpose}, TemporalBasis: population.TemporalBasisAsOfCaller, UnknownDisclosure: population.UnknownDisclosureBlock, CountDisclosure: population.CountDisclosureExact, Criteria: population.Criteria{Root: population.Predicate{Kind: population.PredicateEquals, Field: "consent", Values: []string{"active"}}}}
	asOf := values.NewInstant(time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))
	known, err := values.NewKnownAt(asOf)
	if err != nil {
		t.Fatal(err)
	}
	versions := population.PolicyVersions{AuthZVersion: "authz/v1", PrivacyVersion: "privacy/v1", OrganizationVersion: "org/v1", PurposeVersion: "purpose/v1"}
	resolved := population.Result{AsOf: asOf, KnownAt: known, Members: []population.Member{{Subject: ref(tenant, "worker", uuid.New()), Outcome: population.OutcomeIncluded}}, Completeness: population.CompletenessComplete}
	restricted, err := population.ApplyRestrictions(resolved, population.RestrictionDecision{Versions: versions, DiscloseMembership: true, DiscloseCount: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := population.Freeze(definition, "population/revision/1", restricted, asOf, known, map[population.SubjectKind]values.Instant{population.SubjectWorker: asOf})
	if err != nil {
		t.Fatal(err)
	}
	cost, err := values.NewMoney("10.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := values.NewInstantInterval(values.NewInstant(time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)), values.NewInstant(time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	campaign := crm.CampaignRevision{CampaignID: ref(tenant, "campaign", uuid.New()), Revision: revision(t, 1), Pool: loaded, Population: snapshot, PopulationDefinition: definition, Purpose: loaded.Purpose, ContentRef: ref(tenant, "content", uuid.New()), Channels: []values.EntityRef{ref(tenant, "channel", uuid.New())}, Schedule: schedule, Frequency: crm.FrequencyPolicy{MaxContacts: 1, WindowDays: 7}, Cost: cost, Suppression: crm.SuppressExpired}
	got, err := crm.NewCampaign(context.Background(), campaign, storedCampaignAudienceOwner{pool: loaded, definition: definition, snapshot: snapshot})
	if err != nil || got.CanonicalDigest == "" {
		t.Fatalf("governed campaign=%+v err=%v", got, err)
	}
	forged := campaign
	forged.Population.RevisionVersion = "population/revision/forged"
	forged.Population.Digest = "sha256:forged"
	if _, err := crm.NewCampaign(context.Background(), forged, storedCampaignAudienceOwner{pool: loaded, definition: definition, snapshot: snapshot}); !errors.Is(err, crm.ErrAudienceBlocked) {
		t.Fatalf("forged audience accepted: %v", err)
	}
}
