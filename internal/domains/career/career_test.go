package career

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestCareerProfileSeparatesWorkerPreferenceFromManagerAssessmentAndPrediction(t *testing.T) {
	worker := careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000301")
	role := careerRef(values.Kind("career_target_role"), "00000000-0000-0000-0000-000000000302")
	p, err := NewCareerPreference(CareerPreferenceProfileRevision{PreferenceID: careerRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000303"), Revision: careerRevision(t, "p", 1), Worker: worker, Mobility: MobilityInternational, TargetRoleRefs: []values.EntityRef{role}, Timeframe: careerInterval(t), Visibility: VisibilityWorkerOnly})
	if err != nil {
		t.Fatal(err)
	}
	a := CareerAssessmentRevision{AssessmentID: careerRef(values.Kind("career_assessment"), "00000000-0000-0000-0000-000000000304"), Revision: careerRevision(t, "a", 1), Worker: worker, TargetRole: role, Source: careerRef(values.Kind("source"), "00000000-0000-0000-0000-000000000305"), Epistemic: EpistemicRecommendation, Visibility: VisibilityWorkerAndAuthorized, Summary: "possible fit", Effective: careerInterval(t)}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Worker != a.Worker || p.Mobility == MobilityUnspecified || a.Epistemic == EpistemicFact {
		t.Fatalf("preference/assessment separation lost: p=%+v a=%+v", p, a)
	}
}

func TestTodo_CAREER_001_Conformance(t *testing.T) {
	worker := careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000311")
	role := careerRef(values.Kind("career_target_role"), "00000000-0000-0000-0000-000000000312")
	assessment := CareerAssessmentRevision{AssessmentID: careerRef(values.Kind("career_assessment"), "00000000-0000-0000-0000-000000000313"), Revision: careerRevision(t, "assessment", 2), Worker: worker, TargetRole: role, Source: careerRef(values.Kind("source"), "00000000-0000-0000-0000-000000000314"), Epistemic: EpistemicModelInference, Visibility: VisibilityWorkerOnly, Summary: "inferred fit", Effective: careerInterval(t)}
	if err := assessment.Validate(); err != nil {
		t.Fatal(err)
	}
	if assessment.Epistemic == EpistemicFact || assessment.Worker != worker {
		t.Fatalf("invalid assessment semantics: %+v", assessment)
	}
}
func TestTodo_CAREER_001_Fault(t *testing.T) {
	worker := careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000321")
	base := CareerAssessmentRevision{AssessmentID: careerRef(values.Kind("career_assessment"), "00000000-0000-0000-0000-000000000322"), Revision: careerRevision(t, "fault", 1), Worker: worker, TargetRole: careerRef(values.Kind("career_target_role"), "00000000-0000-0000-0000-000000000323"), Source: careerRef(values.Kind("source"), "00000000-0000-0000-0000-000000000324"), Epistemic: EpistemicHumanOpinion, Visibility: VisibilityWorkerOnly, Summary: "opinion", Effective: careerInterval(t)}
	for name, mutate := range map[string]func(*CareerAssessmentRevision){"fact": func(a *CareerAssessmentRevision) { a.Epistemic = EpistemicFact }, "no summary": func(a *CareerAssessmentRevision) { a.Summary = "" }, "no revision": func(a *CareerAssessmentRevision) { a.Revision = values.RevisionToken{} }} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid assessment was accepted")
			}
		})
	}
}
func TestTodo_CAREER_001_Golden(t *testing.T) {
	p, err := NewCareerPreference(CareerPreferenceProfileRevision{PreferenceID: careerRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000331"), Revision: careerRevision(t, "golden", 1), Worker: careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000332"), Mobility: MobilityAny, TargetRoleRefs: []values.EntityRef{careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000333")}, Timeframe: careerInterval(t), Visibility: VisibilityWorkerOnly})
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:0ee4df718e56f916d3b0b0765db9d0bba02c194d4f01b2b6a680b6b5c20b5020"
	if p.CanonicalDigest != want {
		t.Fatalf("canonical digest = %q, want %q", p.CanonicalDigest, want)
	}
}
func TestTodo_CAREER_001_Mutation(t *testing.T) {
	p, err := NewCareerPreference(CareerPreferenceProfileRevision{PreferenceID: careerRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000341"), Revision: careerRevision(t, "mutation", 1), Worker: careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000342"), Mobility: MobilityAny, TargetRoleRefs: []values.EntityRef{careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000343")}, Timeframe: careerInterval(t), Visibility: VisibilityWorkerOnly})
	if err != nil {
		t.Fatal(err)
	}
	p.CanonicalDigest = "tampered"
	if err := p.Validate(); err == nil {
		t.Fatal("tampered digest accepted")
	}
}
func TestTodo_CAREER_001_Property(t *testing.T) {
	digests := make(map[string]MobilityWillingness)
	for _, mobility := range []MobilityWillingness{MobilityNotWilling, MobilityWithinCountry, MobilityInternational, MobilityAny} {
		p, err := NewCareerPreference(CareerPreferenceProfileRevision{PreferenceID: careerRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000351"), Revision: careerRevision(t, "property", 1), Worker: careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000352"), Mobility: mobility, TargetRoleRefs: []values.EntityRef{careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000353")}, Timeframe: careerInterval(t), Visibility: VisibilityWorkerOnly})
		if err != nil {
			t.Fatalf("%s: %v", mobility, err)
		}
		if !mobility.Valid() || p.Mobility != mobility {
			t.Fatalf("got %s want %s", p.Mobility, mobility)
		}
		if prior, exists := digests[p.CanonicalDigest]; exists {
			t.Fatalf("mobility values %s and %s alias to digest %s", prior, mobility, p.CanonicalDigest)
		}
		digests[p.CanonicalDigest] = mobility
	}
	if MobilityUnspecified.Valid() || MobilityWillingness("REMOTE_ONLY").Valid() {
		t.Fatal("unspecified or unknown willingness became meaningful")
	}
}
func TestTodo_CAREER_001_Race(t *testing.T) {
	p, err := NewCareerPreference(CareerPreferenceProfileRevision{PreferenceID: careerRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000361"), Revision: careerRevision(t, "race", 1), Worker: careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000362"), Mobility: MobilityAny, TargetRoleRefs: []values.EntityRef{careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000363")}, Timeframe: careerInterval(t), Visibility: VisibilityWorkerOnly})
	if err != nil {
		t.Fatal(err)
	}
	parentDigest := p.CanonicalDigest
	parentRole := p.TargetRoleRefs[0]
	type result struct {
		digest string
		err    error
	}
	var wg sync.WaitGroup
	results := make(chan result, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Validate(); err != nil {
				results <- result{err: err}
				return
			}
			digest, err := p.Digest()
			results <- result{digest: digest, err: err}
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got.err != nil || got.digest != parentDigest {
			t.Fatalf("concurrent digest=%q err=%v, want %q", got.digest, got.err, parentDigest)
		}
	}
	if p.CanonicalDigest != parentDigest || len(p.TargetRoleRefs) != 1 || p.TargetRoleRefs[0] != parentRole {
		t.Fatalf("shared immutable fixture mutated: %+v", p)
	}
}
func TestTodo_CAREER_001_Security(t *testing.T) {
	p, err := NewCareerPreference(CareerPreferenceProfileRevision{PreferenceID: careerRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000371"), Revision: careerRevision(t, "security", 1), Worker: careerRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000372"), Mobility: MobilityInternational, TargetRoleRefs: []values.EntityRef{careerRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000373")}, Timeframe: careerInterval(t), Visibility: VisibilityWorkerOnly})
	if err != nil {
		t.Fatal(err)
	}
	explanation := Explain(p)
	if strings.Contains(explanation, string(MobilityInternational)) {
		t.Fatalf("sensitive preference leaked: %s", explanation)
	}
	if errors.Is(p.Validate(), ErrInvalidReference) {
		t.Fatal("valid worker preference had reference error")
	}
}
