package evolution

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

func TestCompatibilityCheck_OptionalInputAdded_IsCompatible(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1(), promotionV2CompatibleOptionalAdded())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.OK() || r.Verdict != VerdictCompatible {
		t.Fatalf("want COMPATIBLE, got %s: %+v", r.Verdict, r.Violations)
	}
	if len(r.Violations) != 0 {
		t.Fatalf("want no violations, got %+v", r.Violations)
	}
	if len(r.Changes) != 1 || r.Changes[0].Code != ChangeOptionalInputAdded || r.Changes[0].Field != "notes" {
		t.Fatalf("want one optional_input_added change on notes, got %+v", r.Changes)
	}
}

func TestCompatibilityCheck_RequiredInputAdded_IsIncompatible(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1(), promotionV2RequiredInputAdded())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.OK() {
		t.Fatal("want INCOMPATIBLE")
	}
	if len(r.Violations) != 1 {
		t.Fatalf("want exactly one violation, got %+v", r.Violations)
	}
	v := r.Violations[0]
	if v.Code != ChangeRequiredInputAdded || v.Field != "compensation_committee_approval_ref" {
		t.Fatalf("want required_input_added naming the field, got %+v", v)
	}
}

func TestCompatibilityCheck_OptionalBecameRequired_IsIncompatible(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1WithOptionalPay(), promotionV2RequiredInputBecameRequired())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.OK() {
		t.Fatal("want INCOMPATIBLE")
	}
	found := false
	for _, v := range r.Violations {
		if v.Code == ChangeRequiredInputAdded && v.Field == "proposed_base_pay" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want required_input_added on proposed_base_pay, got %+v", r.Violations)
	}
}

func TestCompatibilityCheck_RequiredInputRemoved_IsIncompatible(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1(), promotionV2RequiredInputRemoved())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.OK() {
		t.Fatal("want INCOMPATIBLE")
	}
	if len(r.Violations) != 1 || r.Violations[0].Code != ChangeRequiredInputRemoved || r.Violations[0].Field != "reason_ref" {
		t.Fatalf("want required_input_removed naming reason_ref, got %+v", r.Violations)
	}
}

func TestCompatibilityCheck_InputTypeChanged_IsIncompatible(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1(), promotionV2InputTypeChanged())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.OK() {
		t.Fatal("want INCOMPATIBLE")
	}
	if len(r.Violations) != 1 || r.Violations[0].Code != ChangeInputTypeChanged || r.Violations[0].Field != "target_position_ref" {
		t.Fatalf("want input_type_changed naming target_position_ref, got %+v", r.Violations)
	}
}

func TestCompatibilityCheck_EffectClassRaised_IsIncompatible(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1EffectInternal(), promotionV2EffectRaised())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.OK() {
		t.Fatal("want INCOMPATIBLE")
	}
	if len(r.Violations) != 1 || r.Violations[0].Code != ChangeEffectClassRaised || r.Violations[0].Field != "effect_class" {
		t.Fatalf("want effect_class_raised naming effect_class, got %+v", r.Violations)
	}
}

func TestCompatibilityCheck_EffectClassLowered_IsCompatible(t *testing.T) {
	r, err := CompatibilityCheck(promotionV2EffectRaised(), promotionEffectBase(3, intent.EffectClassInternalMutation))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.OK() {
		t.Fatalf("want COMPATIBLE, got violations %+v", r.Violations)
	}
	if len(r.Changes) != 1 || r.Changes[0].Code != ChangeEffectClassLowered {
		t.Fatalf("want one effect_class_lowered change, got %+v", r.Changes)
	}
}

func TestCompatibilityCheck_ApprovalRequirementRemoved_IsIncompatible(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1(), promotionV2ApprovalRemoved())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.OK() {
		t.Fatal("want INCOMPATIBLE")
	}
	if len(r.Violations) != 1 || r.Violations[0].Code != ChangeApprovalRequirementRemoved || r.Violations[0].Field != "approval_required" {
		t.Fatalf("want approval_requirement_removed naming approval_required, got %+v", r.Violations)
	}
}

func TestCompatibilityCheck_ApprovalRequirementAdded_IsCompatible(t *testing.T) {
	withoutApproval := promotionV1()
	withoutApproval.ApprovalRequired = false
	withApproval := promotionBase(2)
	r, err := CompatibilityCheck(withoutApproval, withApproval)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.OK() {
		t.Fatalf("want COMPATIBLE, got violations %+v", r.Violations)
	}
	if len(r.Changes) != 1 || r.Changes[0].Code != ChangeApprovalAdded {
		t.Fatalf("want one approval_requirement_added change, got %+v", r.Changes)
	}
}

func TestCompatibilityCheck_DifferentIntentType_Errors(t *testing.T) {
	other := promotionV1()
	other.Ref = intent.Ref{TypeID: "hcmnext.people.something_else", Version: 1}
	_, err := CompatibilityCheck(promotionV1(), other)
	if !errors.Is(err, ErrDifferentIntentType) {
		t.Fatalf("want ErrDifferentIntentType, got %v", err)
	}
}

func TestCompatibilityCheck_VersionNotAdvancing_Errors(t *testing.T) {
	_, err := CompatibilityCheck(promotionBase(2), promotionBase(1))
	if !errors.Is(err, ErrVersionNotAdvancing) {
		t.Fatalf("want ErrVersionNotAdvancing, got %v", err)
	}
	_, err = CompatibilityCheck(promotionBase(2), promotionBase(2))
	if !errors.Is(err, ErrVersionNotAdvancing) {
		t.Fatalf("want ErrVersionNotAdvancing for an equal version, got %v", err)
	}
}

func TestCompatibilityCheck_InvalidDefinition_Errors(t *testing.T) {
	broken := promotionV1()
	broken.DisplayName = ""
	if _, err := CompatibilityCheck(broken, promotionBase(2)); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("want ErrInvalidDefinition for previous, got %v", err)
	}
	brokenCurrent := promotionBase(2)
	brokenCurrent.DisplayName = ""
	if _, err := CompatibilityCheck(promotionV1(), brokenCurrent); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("want ErrInvalidDefinition for current, got %v", err)
	}
}

func TestCompatibilityCheck_NoDifference_IsCompatible(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1(), promotionBase(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.OK() || len(r.Changes) != 0 || len(r.Violations) != 0 {
		t.Fatalf("want a silent COMPATIBLE report, got %+v", r)
	}
}
