package career

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/skill"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func careerRef(kind values.Kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: "acme", Kind: kind, Id: id}
}
func careerRevision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}
func careerDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
func careerInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewLocalDateInterval(careerDate(t, "2026-01-01"), careerDate(t, "2027-01-01"), values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func careerSkillSetup(t *testing.T) (skill.SkillOntologyRevision, values.EntityRef, values.EntityRef, skill.WorkerSkillEvidence) {
	t.Helper()
	parent := careerRef(skill.KindSkill, "00000000-0000-0000-0000-000000000101")
	definition, err := skill.NewSkillDefinition(skill.SkillDefinitionRevision{SkillRef: parent, Revision: careerRevision(t, "skill", 1), Name: "planning", ProficiencyScale: skill.DefaultProficiencyScale()})
	if err != nil {
		t.Fatal(err)
	}
	otype, err := skill.NewSkillOntology(skill.SkillOntologyRevision{OntologyID: careerRef(values.Kind("skill_ontology"), "00000000-0000-0000-0000-000000000102"), Revision: careerRevision(t, "ontology", 1), Skills: []skill.SkillDefinitionRevision{definition}})
	if err != nil {
		t.Fatal(err)
	}
	worker := careerRef(skill.KindWorker, "00000000-0000-0000-0000-000000000103")
	evidence, err := skill.NewWorkerSkillEvidence(skill.WorkerSkillEvidence{EvidenceID: careerRef(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000104"), Worker: worker, SkillRef: parent, Level: 4, EvidenceKind: skill.EvidenceCredential, EvidenceRef: "credential-ref", Verified: true, Effective: careerInterval(t)})
	if err != nil {
		t.Fatal(err)
	}
	return otype, worker, parent, evidence
}

func TestCareerAdditiveProfileUsesPinnedSkillReadinessAndEvidenceCompletion(t *testing.T) {
	ontology, worker, skillRef, evidence := careerSkillSetup(t)
	role := TargetRoleProfileRevision{TargetRoleID: careerRef(values.Kind("career_target_role"), "00000000-0000-0000-0000-000000000105"), Revision: careerRevision(t, "target-role", 1), Worker: worker, JobProfile: careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000106"), JobProfileRevision: careerRevision(t, "job-profile", 8), Requirements: []RoleSkillRequirement{{SkillRef: skillRef, MinimumLevel: 3}}, Visibility: VisibilityWorkerOnly, Effective: careerInterval(t)}
	role, err := NewTargetRoleProfile(role)
	if err != nil {
		t.Fatal(err)
	}
	resolver := skill.NewPinnedResolver(ontology, nil, skill.FakeSkillEvidenceReader{Evidence: []skill.WorkerSkillEvidence{evidence}})
	readiness, err := ComputeReadinessGaps(context.Background(), resolver, role, careerDate(t, "2026-06-01"))
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Status != ReadinessReady || readiness.Gaps[0].Gap != 0 {
		t.Fatalf("readiness = %+v", readiness)
	}

	preference, err := NewCareerPreference(CareerPreferenceProfileRevision{PreferenceID: careerRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000107"), Revision: careerRevision(t, "preference", 1), Worker: worker, Mobility: MobilityInternational, TargetRoleRefs: []values.EntityRef{role.TargetRoleID}, Timeframe: careerInterval(t), Visibility: VisibilityWorkerOnly})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(Explain(preference), "INTERNATIONAL") {
		t.Fatal("preference explanation leaked a sensitive choice")
	}

	objective, err := NewDevelopmentObjective(DevelopmentObjectiveProfileRevision{ObjectiveID: careerRef(values.Kind("development_objective"), "00000000-0000-0000-0000-000000000108"), Revision: careerRevision(t, "objective", 1), Worker: worker, TargetRole: role.TargetRoleID, SkillRefs: []values.EntityRef{skillRef}, Description: "Build planning capability", Owner: careerRef(values.Kind("career_owner"), "00000000-0000-0000-0000-000000000109"), State: ObjectiveComplete, CompletionEvidenceRefs: []string{"completion-evidence"}, Visibility: VisibilityWorkerOnly, Effective: careerInterval(t)})
	if err != nil {
		t.Fatal(err)
	}
	if objective.CanonicalDigest == "" || objective.Canonical() == nil {
		t.Fatal("objective was not canonical")
	}
}

func TestCareerCompletionRequiresEvidence(t *testing.T) {
	_, worker, skillRef, _ := careerSkillSetup(t)
	_, err := NewDevelopmentObjective(DevelopmentObjectiveProfileRevision{ObjectiveID: careerRef(values.Kind("development_objective"), "00000000-0000-0000-0000-000000000110"), Revision: careerRevision(t, "objective", 1), Worker: worker, TargetRole: careerRef(values.Kind("career_target_role"), "00000000-0000-0000-0000-000000000111"), SkillRefs: []values.EntityRef{skillRef}, Description: "Objective", Owner: careerRef(values.Kind("career_owner"), "00000000-0000-0000-0000-000000000112"), State: ObjectiveComplete, Visibility: VisibilityWorkerOnly, Effective: careerInterval(t)})
	if err == nil || !strings.Contains(err.Error(), "completion_evidence_refs") {
		t.Fatalf("completion error = %v", err)
	}
}
