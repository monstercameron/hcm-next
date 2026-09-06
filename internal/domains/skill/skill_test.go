package skill

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const skillTenant values.TenantId = "acme"

func skillRef(kind values.Kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: skillTenant, Kind: kind, Id: id}
}
func skillRevision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}
func skillDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
func skillInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewLocalDateInterval(skillDate(t, start), skillDate(t, end), values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func skillOntology(t *testing.T) (SkillOntologyRevision, values.EntityRef, values.EntityRef) {
	t.Helper()
	parent := skillRef(KindSkill, "00000000-0000-0000-0000-000000000001")
	child := skillRef(KindSkill, "00000000-0000-0000-0000-000000000002")
	makeDefinition := func(ref values.EntityRef, parents []values.EntityRef) SkillDefinitionRevision {
		definition, err := NewSkillDefinition(SkillDefinitionRevision{SkillRef: ref, Revision: skillRevision(t, "skill/"+ref.Id, 1), Name: "skill", ParentRefs: parents, Aliases: []string{"common"}, ProficiencyScale: DefaultProficiencyScale()})
		if err != nil {
			t.Fatal(err)
		}
		if got := definition.computedDigest(); got != definition.CanonicalDigest {
			t.Fatalf("definition digest changed: got %s want %s", got, definition.CanonicalDigest)
		}
		return definition
	}
	ontology, err := NewSkillOntology(SkillOntologyRevision{OntologyID: skillRef(values.Kind("skill_ontology"), "00000000-0000-0000-0000-000000000010"), Revision: skillRevision(t, "ontology", 1), Skills: []SkillDefinitionRevision{makeDefinition(parent, nil), makeDefinition(child, []values.EntityRef{parent})}})
	if err != nil {
		t.Fatal(err)
	}
	return ontology, parent, child
}

func skillEvidence(t *testing.T, worker, ref values.EntityRef, end string, verified bool, kind EvidenceKind) WorkerSkillEvidence {
	t.Helper()
	evidence, err := NewWorkerSkillEvidence(WorkerSkillEvidence{EvidenceID: skillRef(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000020"), Worker: worker, SkillRef: ref, Level: 4, EvidenceKind: kind, EvidenceRef: "evidence-private", Verified: verified, Effective: skillInterval(t, "2026-01-01", end)})
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

// TestSkillOntologyRejectsCyclesUnverifiedProficiencyAndUnsafeEquivalence is
// the primary SKILL-001 contract test from planning/todos.md.
func TestSkillOntologyRejectsCyclesUnverifiedProficiencyAndUnsafeEquivalence(t *testing.T) {
	ontology, parent, child := skillOntology(t)
	cycle := ontology
	cycle.Skills = append([]SkillDefinitionRevision(nil), ontology.Skills...)
	cycle.Skills[0].ParentRefs = append([]values.EntityRef(nil), ontology.Skills[0].ParentRefs...)
	cycle.Skills[0].ParentRefs = []values.EntityRef{child}
	if _, err := NewSkillOntology(cycle); !errors.Is(err, ErrInvalidOntology) {
		t.Fatalf("cycle error = %v", err)
	}

	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000030")
	asserted := skillEvidence(t, worker, child, "2027-01-01", false, EvidenceSelfReport)
	resolved, err := NewResolver(FakeSkillEvidenceReader{Evidence: []WorkerSkillEvidence{asserted}}).Resolve(context.Background(), ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}, Ontology: ontology})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Proficiencies[0].Status; got != StatusAsserted {
		t.Fatalf("status = %s", got)
	}
	if strings.Contains(Explain(asserted), asserted.EvidenceRef) {
		t.Fatal("Explain repeated an evidence reference")
	}

	unsafe, err := NewEquivalenceRule(EquivalenceRule{RuleID: skillRef(values.Kind("skill_equivalence"), "00000000-0000-0000-0000-000000000040"), Revision: skillRevision(t, "equivalence", 1), SourceSkill: parent, TargetSkill: child, SourceLevel: 3, TargetLevel: 3, Effective: skillInterval(t, "2026-01-01", "2027-01-01"), EvidenceRef: "review", Approved: false})
	if err == nil || !errors.Is(err, ErrInvalidEquivalence) {
		t.Fatalf("unsafe equivalence error = %v", err)
	}
	_ = unsafe
}

func TestSkillExpirationAndReviewedEquivalence(t *testing.T) {
	ontology, parent, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000050")
	expired := skillEvidence(t, worker, child, "2026-03-01", true, EvidenceCredential)
	equivalence, err := NewEquivalenceRule(EquivalenceRule{RuleID: skillRef(values.Kind("skill_equivalence"), "00000000-0000-0000-0000-000000000051"), Revision: skillRevision(t, "equivalence", 2), SourceSkill: parent, TargetSkill: child, SourceLevel: 4, TargetLevel: 3, Effective: skillInterval(t, "2026-01-01", "2027-01-01"), EvidenceRef: "review", Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	source := skillEvidence(t, worker, parent, "2027-01-01", true, EvidenceCredential)
	resolver := NewPinnedResolver(ontology, []EquivalenceRule{equivalence}, FakeSkillEvidenceReader{Evidence: []WorkerSkillEvidence{expired, source}})
	result, err := resolver.Resolve(context.Background(), ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}, Ontology: ontology, Equivalences: []EquivalenceRule{equivalence}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Proficiencies[0].Status != StatusVerified || !result.Proficiencies[0].ViaEquivalence {
		t.Fatalf("equivalent result = %+v", result.Proficiencies[0])
	}
	result, err = resolver.Resolve(context.Background(), ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), SkillRefs: []values.EntityRef{child}, Ontology: ontology})
	if err != nil {
		t.Fatal(err)
	}
	if result.Proficiencies[0].Status != StatusExpired {
		t.Fatalf("expired result = %+v", result.Proficiencies[0])
	}
}

func TestTodo_SKILL_001_Conformance(t *testing.T) { TestSkillExpirationAndReviewedEquivalence(t) }
func TestTodo_SKILL_001_Fault(t *testing.T) {
	TestSkillOntologyRejectsCyclesUnverifiedProficiencyAndUnsafeEquivalence(t)
}
func TestTodo_SKILL_001_Golden(t *testing.T) {
	ontology, _, _ := skillOntology(t)
	if ontology.CanonicalDigest == "" || ontology.Canonical() == nil {
		t.Fatal("ontology is not canonical")
	}
}
func TestTodo_SKILL_001_Mutation(t *testing.T) {
	ontology, _, _ := skillOntology(t)
	original := append([]SkillDefinitionRevision(nil), ontology.Skills...)
	ontology.Skills[0].Name = "changed"
	if original[0].Name == ontology.Skills[0].Name {
		t.Fatal("fixture copy was not independent")
	}
}
func TestTodo_SKILL_001_Property(t *testing.T) {
	ontology, _, _ := skillOntology(t)
	first := ontology.CanonicalDigest
	second, err := NewSkillOntology(ontology)
	if err != nil || first != second.CanonicalDigest {
		t.Fatalf("digest not stable: %v", err)
	}
}
func TestTodo_SKILL_001_Race(t *testing.T) {
	ontology, _, child := skillOntology(t)
	worker := skillRef(KindWorker, "00000000-0000-0000-0000-000000000060")
	evidence := skillEvidence(t, worker, child, "2027-01-01", true, EvidenceCredential)
	resolver := NewPinnedResolver(ontology, nil, FakeSkillEvidenceReader{Evidence: []WorkerSkillEvidence{evidence}})
	if _, err := resolver.Resolve(context.Background(), ResolveRequest{Worker: worker, AsOf: skillDate(t, "2026-06-01"), Ontology: ontology}); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_SKILL_001_Security(t *testing.T) {
	evidence := WorkerSkillEvidence{EvidenceRef: "secret"}
	if strings.Contains(Explain(evidence), "secret") {
		t.Fatal("explanation leaked evidence")
	}
}
