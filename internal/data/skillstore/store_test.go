package skillstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/domains/skill"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_PERSIST_SKILL_001(t *testing.T) {
	db, store, tenant := testStore(t)
	ontology, _, _, _ := testOntology(t, tenant, uuid.NewString(), 1)
	if err := store.SaveOntology(context.Background(), values.TenantId(tenant), ontology); err != nil {
		t.Fatal(err)
	}
	evidence := testEvidence(t, tenant, ontology.Skills[0].SkillRef, uuid.NewString())
	if err := store.AppendEvidence(context.Background(), values.TenantId(tenant), evidence, 1); err != nil {
		t.Fatal(err)
	}
	var ontologyCount, definitionCount, evidenceCount int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM skill_ontology_revision`).Scan(&ontologyCount); err != nil {
		t.Fatal(err)
	}
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM skill_definition_revision`).Scan(&definitionCount); err != nil {
		t.Fatal(err)
	}
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM worker_skill_evidence`).Scan(&evidenceCount); err != nil {
		t.Fatal(err)
	}
	if ontologyCount != 1 || definitionCount != 2 || evidenceCount != 1 {
		t.Fatalf("counts = ontology %d definitions %d evidence %d, want 1/2/1", ontologyCount, definitionCount, evidenceCount)
	}
}

func TestTodo_PERSIST_SKILL_001_Fault(t *testing.T) {
	_, store, tenant := testStore(t)
	ontology, parent, child, _ := testOntology(t, tenant, uuid.NewString(), 1)
	if err := store.SaveOntology(context.Background(), values.TenantId(tenant), ontology); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOntology(context.Background(), values.TenantId(tenant), ontology); !errors.Is(err, skill.ErrDuplicateRevision) {
		t.Fatalf("duplicate ontology = %v, want ErrDuplicateRevision", err)
	}
	stale, _, _, _ := testOntology(t, tenant, uuid.NewString(), 3)
	if err := store.SaveOntology(context.Background(), values.TenantId(tenant), stale); !errors.Is(err, skill.ErrStaleRevision) {
		t.Fatalf("stale ontology = %v, want ErrStaleRevision", err)
	}
	wrongPredecessor, err := values.NewSequenceRevision("skill/"+parent.Id, 1)
	if err != nil {
		t.Fatal(err)
	}
	childRevision, err := values.NewSequenceRevision("skill/"+child.Id, 2)
	if err != nil {
		t.Fatal(err)
	}
	badChild, err := skill.NewSkillDefinition(skill.SkillDefinitionRevision{SkillRef: child, Revision: childRevision, Supersedes: wrongPredecessor, Name: "Child", ProficiencyScale: skill.DefaultProficiencyScale()})
	if err != nil {
		t.Fatal(err)
	}
	badOntology, err := skill.NewSkillOntology(skill.SkillOntologyRevision{OntologyID: ontology.OntologyID, Revision: mustRevision(t, "ontology", 2), Skills: []skill.SkillDefinitionRevision{badChild}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOntology(context.Background(), values.TenantId(tenant), badOntology); !errors.Is(err, skill.ErrStaleRevision) {
		t.Fatalf("cross-skill supersedes = %v, want ErrStaleRevision", err)
	}
}

func TestTodo_PERSIST_SKILL_001_Integration(t *testing.T) {
	_, store, tenant := testStore(t)
	ontology, _, _, _ := testOntology(t, tenant, uuid.NewString(), 1)
	if err := store.SaveOntology(context.Background(), values.TenantId(tenant), ontology); err != nil {
		t.Fatal(err)
	}
	evidence := testEvidence(t, tenant, ontology.Skills[0].SkillRef, uuid.NewString())
	if err := store.AppendEvidence(context.Background(), values.TenantId(tenant), evidence, 1); err != nil {
		t.Fatal(err)
	}
	gotOntology, err := store.LoadOntology(context.Background(), values.TenantId(tenant), ontology.OntologyID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotOntology.CanonicalDigest != ontology.CanonicalDigest || len(gotOntology.Skills) != len(ontology.Skills) {
		t.Fatalf("ontology = %+v, want digest %q and %d definitions", gotOntology, ontology.CanonicalDigest, len(ontology.Skills))
	}
	gotEvidence, err := store.EvidenceAt(context.Background(), skill.EvidenceQuery{Worker: evidence.Worker, AsOf: mustDate(t, "2026-06-01")})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotEvidence) != 1 || gotEvidence[0].CanonicalDigest != evidence.CanonicalDigest || gotEvidence[0].Level != evidence.Level {
		t.Fatalf("evidence = %+v, want digest %q and level %d", gotEvidence, evidence.CanonicalDigest, evidence.Level)
	}
}

func TestTodo_PERSIST_SKILL_001_Security(t *testing.T) {
	db, store, tenantA := testStore(t)
	tenantB := uuid.NewString()
	insertTenant(t, db.Conn, tenantB)
	ontology, _, _, _ := testOntology(t, tenantA, uuid.NewString(), 1)
	if err := store.SaveOntology(context.Background(), values.TenantId(tenantA), ontology); err != nil {
		t.Fatal(err)
	}
	evidence := testEvidence(t, tenantA, ontology.Skills[0].SkillRef, uuid.NewString())
	if err := store.AppendEvidence(context.Background(), values.TenantId(tenantA), evidence, 1); err != nil {
		t.Fatal(err)
	}
	crossTenantOntology := ontology.OntologyID
	crossTenantOntology.Tenant = values.TenantId(tenantB)
	if _, err := store.LoadOntology(context.Background(), values.TenantId(tenantB), crossTenantOntology, 1); !errors.Is(err, skill.ErrNotFound) {
		t.Fatalf("cross-tenant ontology = %v, want ErrNotFound", err)
	}
	crossTenantWorker := evidence.Worker
	crossTenantWorker.Tenant = values.TenantId(tenantB)
	got, err := store.EvidenceAt(context.Background(), skill.EvidenceQuery{Worker: crossTenantWorker, AsOf: mustDate(t, "2026-06-01")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("cross-tenant evidence = %+v, want no rows", got)
	}
}

func TestTodo_PERSIST_SKILL_001_Recovery(t *testing.T) {
	db, store, tenant := testStore(t)
	ontology, _, _, _ := testOntology(t, tenant, uuid.NewString(), 1)
	if err := store.SaveOntology(context.Background(), values.TenantId(tenant), ontology); err != nil {
		t.Fatal(err)
	}
	evidence := testEvidence(t, tenant, ontology.Skills[0].SkillRef, uuid.NewString())
	if err := store.AppendEvidence(context.Background(), values.TenantId(tenant), evidence, 1); err != nil {
		t.Fatal(err)
	}
	freshConn := db.NewConn(t)
	if _, err := freshConn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	fresh := New(freshConn)
	got, err := fresh.LoadOntology(context.Background(), values.TenantId(tenant), ontology.OntologyID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != ontology.CanonicalDigest {
		t.Fatalf("fresh connection digest = %q, want %q", got.CanonicalDigest, ontology.CanonicalDigest)
	}
}

func TestTodo_PERSIST_SKILL_001_Mutation(t *testing.T) {
	db, store, tenant := testStore(t)
	ontology, _, _, _ := testOntology(t, tenant, uuid.NewString(), 1)
	if err := store.SaveOntology(context.Background(), values.TenantId(tenant), ontology); err != nil {
		t.Fatal(err)
	}
	evidence := testEvidence(t, tenant, ontology.Skills[0].SkillRef, uuid.NewString())
	if err := store.AppendEvidence(context.Background(), values.TenantId(tenant), evidence, 1); err != nil {
		t.Fatal(err)
	}
	app := db.NewConn(t)
	if _, err := app.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Exec(context.Background(), `UPDATE worker_skill_evidence SET evidence_ref='rewritten' WHERE tenant_id=$1`, uuid.MustParse(tenant)); err == nil {
		t.Fatal("worker evidence update succeeded")
	}
	if _, err := app.Exec(context.Background(), `DELETE FROM worker_skill_evidence WHERE tenant_id=$1`, uuid.MustParse(tenant)); err == nil {
		t.Fatal("worker evidence delete succeeded")
	}
}

func testStore(t *testing.T) (*pgtest.DB, *Store, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 70); err != nil {
		t.Fatalf("apply predecessor migrations through 00070: %v", err)
	}
	body, err := migrations.FS.ReadFile("00116_skill.sql")
	if err != nil {
		t.Fatalf("read skill migration: %v", err)
	}
	_, up, ok := strings.Cut(string(body), "-- +goose Up")
	if !ok {
		t.Fatal("skill migration has no goose up marker")
	}
	up, _, ok = strings.Cut(up, "-- +goose Down")
	if !ok {
		t.Fatal("skill migration has no goose down marker")
	}
	if _, err := db.Conn.Exec(context.Background(), up); err != nil {
		t.Fatalf("apply migration 00116: %v", err)
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
		VALUES ($1, $2, 'cell-skill', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, uuid.MustParse(tenant), "tenant-"+tenant, "tenant-"+tenant)
	if err != nil {
		t.Fatal(err)
	}
}

func testOntology(t *testing.T, tenant, ontologyID string, ontologyRevision uint64) (skill.SkillOntologyRevision, values.EntityRef, values.EntityRef, values.EntityRef) {
	t.Helper()
	tenantID := values.TenantId(tenant)
	parent := values.EntityRef{Tenant: tenantID, Kind: skill.KindSkill, Id: uuid.NewString()}
	child := values.EntityRef{Tenant: tenantID, Kind: skill.KindSkill, Id: uuid.NewString()}
	makeDefinition := func(ref values.EntityRef, parents []values.EntityRef) skill.SkillDefinitionRevision {
		definition, err := skill.NewSkillDefinition(skill.SkillDefinitionRevision{SkillRef: ref, Revision: mustRevision(t, "skill/"+ref.Id, 1), Name: ref.Id, ParentRefs: parents, Aliases: []string{"alias-" + ref.Id}, ProficiencyScale: skill.DefaultProficiencyScale()})
		if err != nil {
			t.Fatal(err)
		}
		return definition
	}
	ontology, err := skill.NewSkillOntology(skill.SkillOntologyRevision{OntologyID: values.EntityRef{Tenant: tenantID, Kind: values.Kind("skill_ontology"), Id: ontologyID}, Revision: mustRevision(t, "ontology", ontologyRevision), Skills: []skill.SkillDefinitionRevision{makeDefinition(parent, nil), makeDefinition(child, []values.EntityRef{parent})}})
	if err != nil {
		t.Fatal(err)
	}
	return ontology, parent, child, tenantIDRef(tenantID)
}

func tenantIDRef(tenant values.TenantId) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: values.Kind("tenant"), Id: string(tenant)}
}

func testEvidence(t *testing.T, tenant string, skillRef values.EntityRef, evidenceID string) skill.WorkerSkillEvidence {
	t.Helper()
	tenantID := values.TenantId(tenant)
	worker := values.EntityRef{Tenant: tenantID, Kind: skill.KindWorker, Id: uuid.NewString()}
	evidence, err := skill.NewWorkerSkillEvidence(skill.WorkerSkillEvidence{EvidenceID: values.EntityRef{Tenant: tenantID, Kind: values.Kind("skill_evidence"), Id: evidenceID}, Worker: worker, SkillRef: skillRef, Level: 4, EvidenceKind: skill.EvidenceAssessment, EvidenceRef: "assessment-ref", Verified: true, Effective: testInterval(t)})
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

func testInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	return mustInterval(t, "2026-01-01", "2027-01-01")
}

func mustInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewLocalDateInterval(mustDate(t, start), mustDate(t, end), values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func mustDate(t *testing.T, value string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func mustRevision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}
