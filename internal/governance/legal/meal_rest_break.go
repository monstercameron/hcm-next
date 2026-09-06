package legal

import (
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Meal-and-rest breaks are an additive vocabulary extension. The existing
// LEGAL-011 vocabulary tables remain frozen at version 2 so old releases keep
// their wire meanings and existing conformance tests remain byte-compatible.
// A future pack-definition release can promote this extension into the core
// RulePack union when the release schema is versioned for vocabulary 3.
const (
	ObligationTypeMealRestBreak ObligationType    = 23
	MealRestBreakVocabulary     VocabularyVersion = 3
	MealRestBreakWireToken                        = "MEAL_REST_BREAK"
)

// MealRestBreakType identifies the break duty carried by a meal/rest rule.
type MealRestBreakType string

// Meal/rest break types.
const (
	MealRestBreakTypeMeal MealRestBreakType = "MEAL"
	MealRestBreakTypeRest MealRestBreakType = "REST"
)

// MealRestBreakBinding describes the lifecycle attachment reserved for the
// vocabulary-3 kind. It mirrors WAGE_FLOOR's guard binding without changing
// the frozen vocabulary-2 binding table.
type MealRestBreakBinding struct {
	LifecycleStep LifecycleStep
	Kind          ObligationBindingKind
}

// MealRestBreakObligation is the typed body for MEAL_REST_BREAK.
type MealRestBreakObligation struct {
	ID              string
	BreakType       MealRestBreakType
	TriggerHours    int
	DurationMinutes int
	Paid            bool
	PenaltyAmount   values.Money
	HasPenalty      bool
	Citation        Citation
}

var (
	ErrMealRestBreakID       = errors.New("legal: meal/rest break id is required")
	ErrMealRestBreakType     = errors.New("legal: meal/rest break type is invalid")
	ErrMealRestBreakTrigger  = errors.New("legal: meal/rest break trigger hours are invalid")
	ErrMealRestBreakDuration = errors.New("legal: meal/rest break duration is invalid")
	ErrMealRestBreakPenalty  = errors.New("legal: meal/rest break penalty is invalid")
)

// Validate reports whether the typed break body is complete.
func (o MealRestBreakObligation) Validate() error {
	if o.ID == "" {
		return ErrMealRestBreakID
	}
	if o.BreakType != MealRestBreakTypeMeal && o.BreakType != MealRestBreakTypeRest {
		return fmt.Errorf("%w: %q", ErrMealRestBreakType, o.BreakType)
	}
	if o.TriggerHours <= 0 {
		return fmt.Errorf("%w: got %d", ErrMealRestBreakTrigger, o.TriggerHours)
	}
	if o.DurationMinutes <= 0 {
		return fmt.Errorf("%w: got %d", ErrMealRestBreakDuration, o.DurationMinutes)
	}
	if o.HasPenalty {
		if err := o.PenaltyAmount.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrMealRestBreakPenalty, err)
		}
	}
	if err := o.Citation.Validate(); err != nil {
		return err
	}
	return nil
}

// Explain returns a deterministic, redaction-safe summary of the break rule.
func (o MealRestBreakObligation) Explain() string {
	penalty := "none"
	if o.HasPenalty {
		penalty = o.PenaltyAmount.String()
	}
	return fmt.Sprintf("meal_rest_break id=%s type=%s trigger_hours=%d duration_minutes=%d paid=%t penalty=%s",
		o.ID, o.BreakType, o.TriggerHours, o.DurationMinutes, o.Paid, penalty)
}

// MealRestBreakKindSpec is the extension registry row for the new kind.
func MealRestBreakKindSpec() (ObligationKindSpec, bool) {
	return ObligationKindSpec{
		Type:       ObligationTypeMealRestBreak,
		Vocabulary: MealRestBreakVocabulary,
		Trigger:    "worker reaches the declared hours trigger",
		Bindings: []BindingSpec{
			{Step: LifecycleStepPreflight, Kind: ObligationBindingKindGuard},
			{Step: LifecycleStepExecute, Kind: ObligationBindingKindGuard},
		},
	}, true
}

// ParseMealRestBreakKind accepts the vocabulary-3 wire token without changing
// ParseObligationType's vocabulary-2 contract.
func ParseMealRestBreakKind(token string) (ObligationType, error) {
	if token == MealRestBreakWireToken {
		return ObligationTypeMealRestBreak, nil
	}
	return ObligationTypeUnspecified, fmt.Errorf("legal: unknown meal/rest break kind %q", token)
}

// MealRestBreakKindString returns the stable wire token for the extension.
func MealRestBreakKindString() string { return MealRestBreakWireToken }
