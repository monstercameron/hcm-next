package evolution

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The fixtures below are modelled on the catalog's PromoteWorker definition
// (internal/intent/definitions/definitions.go) but are deliberately
// self-contained rather than imported from that package: this package's
// tests, and in particular its golden verdict table, must never drift
// because an unrelated lane edited the real catalog fixture.

func promotionRef(version uint32) intent.Ref {
	return intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: version}
}

func promotionSchema(name string) intent.SchemaRef {
	return intent.SchemaRef{SchemaID: name, Version: 1, ProtobufFullName: name}
}

// promotionBase returns a valid, minimal PromoteWorker-shaped definition at
// the given version. Every scenario fixture below starts here and edits
// exactly the fields its scenario is about.
func promotionBase(version uint32) intent.Definition {
	return intent.Definition{
		Ref:          promotionRef(version),
		DisplayName:  "PromoteWorker",
		Description:  "Propose and execute a governed upward job or position change for one employment assignment.",
		OwnerDomain:  "PEOPLE",
		OwnerPlane:   "DOMAIN",
		Family:       intent.FamilyChangeRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectInternalMutation,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  promotionSchema("hcmnext.people.v1.PromoteWorkerRequest"),
		ResultSchema: promotionSchema("hcmnext.people.v1.PromoteWorkerResult"),
		PhaseDepth:   "GATE_A_CONTRACT_GATE_B_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeReplay,
		},
		SubjectKinds: []string{"PERSON", "EMPLOYMENT", "POSITION"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "employment_ref", Kind: intent.InputKindSubjectRef, Required: true},
			{Path: "target_position_ref", Kind: intent.InputKindReference, Required: true},
			{Path: "effective_time", Kind: intent.InputKindEffectiveTime, Required: true},
			{Path: "reason_ref", Kind: intent.InputKindReference, Required: true},
		},
		ApprovalRequired: true,
	}
}

// promotionV1 is the published predecessor every scenario below compares
// against.
func promotionV1() intent.Definition { return promotionBase(1) }

// promotionV2CompatibleOptionalAdded adds one optional input and changes
// nothing else: the only difference this package classifies as COMPATIBLE.
func promotionV2CompatibleOptionalAdded() intent.Definition {
	d := promotionBase(2)
	d.RequiredInputs = append(append([]intent.RequiredInput(nil), d.RequiredInputs...),
		intent.RequiredInput{Path: "notes", Kind: intent.InputKindDocument, Required: false})
	return d
}

// promotionV2RequiredInputAdded adds a brand-new required input.
func promotionV2RequiredInputAdded() intent.Definition {
	d := promotionBase(2)
	d.RequiredInputs = append(append([]intent.RequiredInput(nil), d.RequiredInputs...),
		intent.RequiredInput{Path: "compensation_committee_approval_ref", Kind: intent.InputKindReference, Required: true})
	return d
}

// promotionV1WithOptionalPay is the predecessor promotionV2RequiredInputBecameRequired
// is compared against: identical inputs, plus proposed_base_pay optional.
func promotionV1WithOptionalPay() intent.Definition {
	d := promotionBase(1)
	d.RequiredInputs = append(append([]intent.RequiredInput(nil), d.RequiredInputs...),
		intent.RequiredInput{Path: "proposed_base_pay", Kind: intent.InputKindMoney, Required: false})
	return d
}

// promotionV2RequiredInputBecameRequired keeps the same path but flips it
// from optional to required, which this package treats identically to a
// brand-new required input: an existing caller that omitted it stops being
// able to submit.
func promotionV2RequiredInputBecameRequired() intent.Definition {
	d := promotionBase(2)
	d.RequiredInputs = append(append([]intent.RequiredInput(nil), d.RequiredInputs...),
		intent.RequiredInput{Path: "proposed_base_pay", Kind: intent.InputKindMoney, Required: true})
	return d
}

// promotionV2RequiredInputRemoved drops a required input entirely.
func promotionV2RequiredInputRemoved() intent.Definition {
	d := promotionBase(2)
	var kept []intent.RequiredInput
	for _, in := range d.RequiredInputs {
		if in.Path == "reason_ref" {
			continue
		}
		kept = append(kept, in)
	}
	d.RequiredInputs = kept
	return d
}

// promotionV2InputTypeChanged keeps the path and required flag but changes
// the declared kind.
func promotionV2InputTypeChanged() intent.Definition {
	d := promotionBase(2)
	inputs := make([]intent.RequiredInput, len(d.RequiredInputs))
	copy(inputs, d.RequiredInputs)
	for i, in := range inputs {
		if in.Path == "target_position_ref" {
			inputs[i].Kind = intent.InputKindScalar
		}
	}
	d.RequiredInputs = inputs
	return d
}

// promotionEffectBase returns a P1B-scheduled variant: P1A forces
// EffectClassZero, so raising the effect class can only be exercised on a
// release that allows a non-zero ceiling.
func promotionEffectBase(version uint32, class intent.EffectClass) intent.Definition {
	d := promotionBase(version)
	d.Release = intent.ReleaseP1B
	d.EffectClass = class
	return d
}

func promotionV1EffectInternal() intent.Definition {
	return promotionEffectBase(1, intent.EffectClassInternalMutation)
}

func promotionV2EffectRaised() intent.Definition {
	return promotionEffectBase(2, intent.EffectClassExternalMutation)
}

// promotionV2ApprovalRemoved drops the approval requirement.
func promotionV2ApprovalRemoved() intent.Definition {
	d := promotionBase(2)
	d.ApprovalRequired = false
	return d
}

// promotionGoldenV1 and promotionGoldenV2 are the one fixture pair the
// golden test pins: they combine every named rule (one compatible addition,
// one required addition, one removal, one type change, one raised effect
// class and one removed approval requirement) into a single verdict table.
func promotionGoldenV1() intent.Definition {
	return promotionEffectBase(1, intent.EffectClassInternalMutation)
}

func promotionGoldenV2() intent.Definition {
	d := promotionEffectBase(2, intent.EffectClassExternalMutation)
	inputs := make([]intent.RequiredInput, 0, len(d.RequiredInputs))
	for _, in := range d.RequiredInputs {
		switch in.Path {
		case "reason_ref":
			continue // required_input_removed
		case "target_position_ref":
			in.Kind = intent.InputKindScalar // input_type_changed
		}
		inputs = append(inputs, in)
	}
	inputs = append(inputs,
		intent.RequiredInput{Path: "compensation_committee_approval_ref", Kind: intent.InputKindReference, Required: true}, // required_input_added
		intent.RequiredInput{Path: "notes", Kind: intent.InputKindDocument, Required: false},                               // optional_input_added
	)
	d.RequiredInputs = inputs
	d.ApprovalRequired = false // approval_requirement_removed
	return d
}

func mustInstant(year int, month time.Month, day int) values.Instant {
	return values.NewInstant(time.Date(year, month, day, 0, 0, 0, 0, time.UTC))
}
