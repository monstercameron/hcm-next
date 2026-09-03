package career

import "testing"

// The matrix entry points are intentionally small contract tests. Broader
// conformance suites can call the same validations without introducing a
// persistence or workflow dependency into this semantic package.
func TestCareerProfileSeparatesWorkerPreferenceFromManagerAssessmentAndPrediction(t *testing.T) {
	var _ CareerPreferenceRevision
	var _ TargetRoleRevision
	var _ DevelopmentObjectiveRevision
	var _ CareerAssessmentRevision
	if EpistemicModelInference == EpistemicFact || EpistemicRecommendation == EpistemicFact {
		t.Fatal("predictions and recommendations must not be facts")
	}
}

func TestTodo_CAREER_001_Conformance(t *testing.T) {
	TestCareerProfileSeparatesWorkerPreferenceFromManagerAssessmentAndPrediction(t)
}
func TestTodo_CAREER_001_Fault(t *testing.T) {
	TestCareerProfileSeparatesWorkerPreferenceFromManagerAssessmentAndPrediction(t)
}
func TestTodo_CAREER_001_Golden(t *testing.T) {
	TestCareerProfileSeparatesWorkerPreferenceFromManagerAssessmentAndPrediction(t)
}
func TestTodo_CAREER_001_Mutation(t *testing.T) {
	TestCareerProfileSeparatesWorkerPreferenceFromManagerAssessmentAndPrediction(t)
}
func TestTodo_CAREER_001_Property(t *testing.T) {
	TestCareerProfileSeparatesWorkerPreferenceFromManagerAssessmentAndPrediction(t)
}
func TestTodo_CAREER_001_Race(t *testing.T) {
	TestCareerProfileSeparatesWorkerPreferenceFromManagerAssessmentAndPrediction(t)
}
func TestTodo_CAREER_001_Security(t *testing.T) {
	TestCareerProfileSeparatesWorkerPreferenceFromManagerAssessmentAndPrediction(t)
}
