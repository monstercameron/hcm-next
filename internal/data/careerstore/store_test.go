package careerstore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/careerstore"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/career"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
	store  *careerstore.Store
}

func newFixture(t *testing.T, key string) fixture {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 74); err != nil {
		t.Fatalf("apply migrations through 00074: %v", err)
	}
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-career',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant, key, key)
	return fixture{db: db, tenant: tenant, store: careerstore.New(appConn(t, db))}
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func tenantRef(tenant uuid.UUID, kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(tenant.String()), Kind: values.Kind(kind), Id: id}
}

func revision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	value, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func interval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(2026, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2027, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	value, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type careerValues struct {
	preference career.CareerPreferenceProfileRevision
	target     career.TargetRoleProfileRevision
	objective  career.DevelopmentObjectiveProfileRevision
	assessment career.CareerAssessmentRevision
}

func valuesFor(t *testing.T, tenant uuid.UUID, suffix string) careerValues {
	t.Helper()
	worker := tenantRef(tenant, "worker", "00000000-0000-0000-0000-000000000101")
	role := tenantRef(tenant, "career_target_role", "00000000-0000-0000-0000-000000000102")
	skill := tenantRef(tenant, "skill", "00000000-0000-0000-0000-000000000103")
	iv := interval(t)
	pref, err := career.NewCareerPreference(career.CareerPreferenceProfileRevision{
		PreferenceID: tenantRef(tenant, "career_preference", "00000000-0000-0000-0000-000000000104"), Revision: revision(t, "preference-"+suffix, 1), Worker: worker, Mobility: career.MobilityAny, TargetRoleRefs: []values.EntityRef{role}, Timeframe: iv, Visibility: career.VisibilityWorkerOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := career.NewTargetRoleProfile(career.TargetRoleProfileRevision{
		TargetRoleID: role, Revision: revision(t, "target-"+suffix, 1), Worker: worker, JobProfile: tenantRef(tenant, "job_profile", "00000000-0000-0000-0000-000000000105"), JobProfileRevision: revision(t, "job-profile-"+suffix, 8), Requirements: []career.RoleSkillRequirement{{SkillRef: skill, MinimumLevel: 3}}, Visibility: career.VisibilityWorkerAndAuthorized, Effective: iv,
	})
	if err != nil {
		t.Fatal(err)
	}
	objective, err := career.NewDevelopmentObjective(career.DevelopmentObjectiveProfileRevision{
		ObjectiveID: tenantRef(tenant, "development_objective", "00000000-0000-0000-0000-000000000106"), Revision: revision(t, "objective-"+suffix, 1), Worker: worker, TargetRole: role, SkillRefs: []values.EntityRef{skill}, Description: "Build capability", Owner: tenantRef(tenant, "career_owner", "00000000-0000-0000-0000-000000000107"), State: career.ObjectiveComplete, CompletionEvidenceRefs: []string{"evidence-1"}, Visibility: career.VisibilityWorkerOnly, Effective: iv,
	})
	if err != nil {
		t.Fatal(err)
	}
	assessment := career.CareerAssessmentRevision{AssessmentID: tenantRef(tenant, "career_assessment", "00000000-0000-0000-0000-000000000108"), Revision: revision(t, "assessment-"+suffix, 1), Worker: worker, TargetRole: role, Source: tenantRef(tenant, "source", "00000000-0000-0000-0000-000000000109"), Epistemic: career.EpistemicHumanOpinion, Visibility: career.VisibilityWorkerAndAuthorized, Summary: "Human assessment", Effective: iv}
	if err := assessment.Validate(); err != nil {
		t.Fatal(err)
	}
	return careerValues{preference: pref, target: target, objective: objective, assessment: assessment}
}

func TestTodo_PERSIST_CAREER_001(t *testing.T) {
	f := newFixture(t, "career-primary")
	v := valuesFor(t, f.tenant, "primary")
	ctx := context.Background()
	if err := f.store.SavePreference(ctx, f.tenant.String(), v.preference, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveTargetRole(ctx, f.tenant.String(), v.target, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveDevelopmentObjective(ctx, f.tenant.String(), v.objective, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveAssessment(ctx, f.tenant.String(), v.assessment, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	if got, err := f.store.LoadPreference(ctx, f.tenant.String(), v.preference.PreferenceID.Id, v.preference.Revision); err != nil || got.CanonicalDigest != v.preference.CanonicalDigest {
		t.Fatalf("preference=%+v err=%v", got, err)
	}
	if got, err := f.store.LoadTargetRole(ctx, f.tenant.String(), v.target.TargetRoleID.Id, v.target.Revision); err != nil || got.CanonicalDigest != v.target.CanonicalDigest {
		t.Fatalf("target=%+v err=%v", got, err)
	}
	if got, err := f.store.LoadDevelopmentObjective(ctx, f.tenant.String(), v.objective.ObjectiveID.Id, v.objective.Revision); err != nil || got.CanonicalDigest != v.objective.CanonicalDigest {
		t.Fatalf("objective=%+v err=%v", got, err)
	}
	if got, err := f.store.LoadAssessment(ctx, f.tenant.String(), v.assessment.AssessmentID.Id, v.assessment.Revision); err != nil || got.Summary != v.assessment.Summary {
		t.Fatalf("assessment=%+v err=%v", got, err)
	}
}

func TestTodo_PERSIST_CAREER_001_Fault(t *testing.T) {
	f := newFixture(t, "career-fault")
	v := valuesFor(t, f.tenant, "fault")
	ctx := context.Background()
	if err := f.store.SavePreference(ctx, f.tenant.String(), v.preference, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SavePreference(ctx, f.tenant.String(), v.preference, values.UnspecifiedRevision()); !errors.Is(err, career.ErrStoreDuplicate) {
		t.Fatalf("duplicate=%v", err)
	}
	next := v.preference
	next.Revision = revision(t, "preference-fault", 2)
	next.Supersedes = revision(t, "different-stream", 1)
	next, err := career.NewCareerPreference(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SavePreference(ctx, f.tenant.String(), next, v.preference.Revision); !errors.Is(err, career.ErrStoreStaleCAS) {
		t.Fatalf("stale=%v", err)
	}
}

func TestTodo_PERSIST_CAREER_001_Integration(t *testing.T) {
	f := newFixture(t, "career-integration")
	v := valuesFor(t, f.tenant, "integration")
	ctx := context.Background()
	if err := f.store.SavePreference(ctx, f.tenant.String(), v.preference, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	next := v.preference
	next.Revision = revision(t, "preference-integration", 2)
	next.Supersedes = v.preference.Revision
	next.Mobility = career.MobilityInternational
	next, _ = career.NewCareerPreference(next)
	if err := f.store.SavePreference(ctx, f.tenant.String(), next, v.preference.Revision); err != nil {
		t.Fatal(err)
	}
	current, err := f.store.CurrentPreference(ctx, f.tenant.String(), v.preference.PreferenceID.Id)
	if err != nil || !current.Revision.Equal(next.Revision) {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	history, err := f.store.ListPreferences(ctx, f.tenant.String(), v.preference.PreferenceID.Id)
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%v err=%v", history, err)
	}
}

func inTenant(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	t.Helper()
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

func TestTodo_PERSIST_CAREER_001_Security(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 74); err != nil {
		t.Fatal(err)
	}
	alpha, beta := uuid.New(), uuid.New()
	for _, item := range []uuid.UUID{alpha, beta} {
		db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-career',$3,'ACTIVE',now())`, item, item.String(), item.String())
	}
	store := careerstore.New(appConn(t, db))
	va, vb := valuesFor(t, alpha, "alpha"), valuesFor(t, beta, "beta")
	if err := store.SavePreference(context.Background(), alpha.String(), va.preference, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreference(context.Background(), beta.String(), vb.preference, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	var count int
	err := inTenant(t, appConn(t, db), alpha, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM career_preference_revision WHERE tenant_id=$1`, beta).Scan(&count)
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cross-tenant rows visible: %d", count)
	}
}

func TestTodo_PERSIST_CAREER_001_Recovery(t *testing.T) {
	f := newFixture(t, "career-recovery")
	v := valuesFor(t, f.tenant, "recovery")
	if err := f.store.SaveTargetRole(context.Background(), f.tenant.String(), v.target, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	fresh := careerstore.New(appConn(t, f.db))
	got, err := fresh.LoadTargetRole(context.Background(), f.tenant.String(), v.target.TargetRoleID.Id, v.target.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != v.target.CanonicalDigest || len(got.Requirements) != 1 {
		t.Fatalf("fresh load=%+v", got)
	}
}

func TestTodo_PERSIST_CAREER_001_Mutation(t *testing.T) {
	f := newFixture(t, "career-mutation")
	v := valuesFor(t, f.tenant, "mutation")
	ctx := context.Background()
	if err := f.store.SavePreference(ctx, f.tenant.String(), v.preference, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	if err := inTenant(t, appConn(t, f.db), f.tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE career_preference_revision SET visibility=visibility WHERE tenant_id=$1`, f.tenant)
		return err
	}); err == nil {
		t.Fatal("immutable career preference accepted UPDATE")
	}
}
