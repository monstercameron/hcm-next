package jobarchstore_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/jobarchstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/jobarch"
)

func TestJobArchGovernedRequirementsPersistThroughMigration00048(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "jobarch-governed-requirements")
	store := jobarchstore.New(appConn(t, db))
	a := architectureFixture(t, "architecture-governed-requirements")
	a.Profiles = append([]jobarch.JobProfileRevision(nil), a.Profiles...)
	a.Profiles[0].Requirements = jobarch.JobProfileRequirements{
		Classification: jobarch.VersionedReference{Ref: "classification:engineering", Revision: "v3", Authority: "authority:taxonomy", EffectiveFrom: a.Profiles[0].EffectiveFrom},
		Qualifications: []jobarch.QualificationReference{{VersionedReference: jobarch.VersionedReference{Ref: "qualification:engineering", Revision: "v2", Authority: "authority:qualification", EffectiveFrom: a.Profiles[0].EffectiveFrom}}},
		Skills:         []jobarch.SkillRequirementReference{{VersionedReference: jobarch.VersionedReference{Ref: "skill:systems", Revision: "v4", Authority: "authority:skills", EffectiveFrom: a.Profiles[0].EffectiveFrom}, Proficiency: 3}},
		Credentials:    []jobarch.CredentialRequirementReference{{VersionedReference: jobarch.VersionedReference{Ref: "credential:license", Revision: "v1", Authority: "authority:credentials", EffectiveFrom: a.Profiles[0].EffectiveFrom}, Level: 1, ValidFrom: a.Profiles[0].EffectiveFrom, ValidTo: a.Profiles[0].EffectiveFrom.AddDate(1, 0, 0)}},
		Compensation:   jobarch.CompensationReference{GradeRef: "grade-1", GradeRevision: "g1", BandRef: "band:engineering", BandRevision: "v7", Currency: "USD", Authority: "authority:rewards", EffectiveFrom: a.Profiles[0].EffectiveFrom},
	}
	var err error
	a, err = jobarch.NewArchitectureRevision(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), tenant.String(), a, ""); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background(), tenant.String(), a.ID, a.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profiles[0].Requirements.Compensation.BandRevision != "v7" || got.Profiles[0].Requirements.Skills[0].Proficiency != 3 || got.CanonicalDigest != a.CanonicalDigest {
		t.Fatalf("governed requirements did not round-trip: got=%+v want=%+v", got.Profiles[0].Requirements, a.Profiles[0].Requirements)
	}
}
