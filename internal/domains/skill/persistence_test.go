package skill

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMemoryStoreSatisfiesPersistencePort(t *testing.T) {
	tenant := uuid.NewString()
	ontologyID := values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind("skill_ontology"), Id: uuid.NewString()}
	skillRef := values.EntityRef{Tenant: values.TenantId(tenant), Kind: KindSkill, Id: uuid.NewString()}
	worker := values.EntityRef{Tenant: values.TenantId(tenant), Kind: KindWorker, Id: uuid.NewString()}
	revision, err := values.NewSequenceRevision("ontology", 1)
	if err != nil {
		t.Fatal(err)
	}
	definitionRevision, err := values.NewSequenceRevision("skill/skill-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := NewSkillDefinition(SkillDefinitionRevision{SkillRef: skillRef, Revision: definitionRevision, Name: "Skill", ProficiencyScale: DefaultProficiencyScale()})
	if err != nil {
		t.Fatal(err)
	}
	ontology, err := NewSkillOntology(SkillOntologyRevision{OntologyID: ontologyID, Revision: revision, Skills: []SkillDefinitionRevision{definition}})
	if err != nil {
		t.Fatal(err)
	}
	date, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2027-01-01")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(date, end, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := NewWorkerSkillEvidence(WorkerSkillEvidence{EvidenceID: values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind("skill_evidence"), Id: uuid.NewString()}, Worker: worker, SkillRef: skillRef, Level: 3, EvidenceKind: EvidenceAssessment, EvidenceRef: "assessment-1", Verified: true, Effective: interval})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	if err := store.SaveOntology(context.Background(), values.TenantId(tenant), ontology); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvidence(context.Background(), values.TenantId(tenant), evidence, 1); err != nil {
		t.Fatal(err)
	}
	got, err := store.EvidenceAt(context.Background(), EvidenceQuery{Worker: worker, AsOf: date})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CanonicalDigest != evidence.CanonicalDigest {
		t.Fatalf("evidence = %+v, want one matching record", got)
	}
	if _, err := store.LoadOntology(context.Background(), values.TenantId(tenant), ontologyID, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvidence(context.Background(), values.TenantId(tenant), evidence, 1); !errors.Is(err, ErrDuplicateEvidence) {
		t.Fatalf("duplicate evidence = %v, want ErrDuplicateEvidence", err)
	}
}
