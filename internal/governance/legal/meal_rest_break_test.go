package legal

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_LEGAL_TOOL_002_MealRestBreakKindExtension(t *testing.T) {
	money, err := values.NewMoney("16.90", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	rule := MealRestBreakObligation{
		ID:              "ca-meal",
		BreakType:       MealRestBreakTypeMeal,
		TriggerHours:    5,
		DurationMinutes: 30,
		PenaltyAmount:   money,
		HasPenalty:      true,
		Citation: Citation{
			SourceFile:       "planning/research/state-employment-law/california.md",
			Section:          "Lab. Code §§ 512, 226.7",
			Status:           ReviewStatusUnreviewed,
			ConfidenceMarker: ConfidenceMarkerConfirmed,
		},
	}
	if err := rule.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !strings.Contains(rule.Explain(), "type=MEAL") || !strings.Contains(rule.Explain(), "duration_minutes=30") {
		t.Fatalf("Explain() = %q, want typed break metadata", rule.Explain())
	}
	spec, ok := MealRestBreakKindSpec()
	if !ok || spec.Type != ObligationTypeMealRestBreak || spec.Vocabulary != MealRestBreakVocabulary || len(spec.Bindings) != 2 {
		t.Fatalf("MealRestBreakKindSpec() = %+v, %t", spec, ok)
	}
	if got, err := ParseMealRestBreakKind(MealRestBreakWireToken); err != nil || got != ObligationTypeMealRestBreak {
		t.Fatalf("ParseMealRestBreakKind() = %v, %v", got, err)
	}
}
