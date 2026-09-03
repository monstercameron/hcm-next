package definitions

import (
	"strings"

	"github.com/monstercameron/hcm-next/internal/intent"
)

// schema builds a schema reference from the canonical "<full name>/v<version>"
// spelling used in the SchemaFlux sources. The Protobuf full name and the
// schema id are the same string: the schema is the message.
func schema(ref string) intent.SchemaRef {
	name, version, ok := strings.Cut(ref, "/v")
	if !ok {
		panic("definitions: schema reference " + ref + " is not <full name>/v<version>")
	}
	var v uint32
	for i := 0; i < len(version); i++ {
		v = v*10 + uint32(version[i]-'0')
	}
	return intent.SchemaRef{SchemaID: name, Version: v, ProtobufFullName: name}
}

func ref(typeID string, version uint32) intent.Ref {
	return intent.Ref{TypeID: typeID, Version: version}
}

// mandatoryNegativeStates is the applicable set every catalog definition
// declares. All fourteen read facts that can be unknown, partial, degraded,
// ambiguous, redacted, unavailable or stale, so all fourteen must decide them.
func mandatoryNegativeStates() []intent.NegativeState {
	return intent.MandatoryNegativeStates()
}

// All returns the fourteen drafted definitions, in catalog order.
//
// The source YAML under schema/schemaflux/business_intents/v1 and this table
// agree on every family: CreateRepairPlan is ANALYTICAL_REQUEST with a
// READ_ONLY side effect (the YAML briefly declared the retired PROCESS_REQUEST
// family and was corrected on 2026-09-03; tools/gen/schemaflux cross-checks
// the two and reports any future divergence).
func All() []intent.Definition {
	return []intent.Definition{
		changeManager(),
		explainWorkerState(),
		promoteWorker(),
		changeBasePay(),
		simulateCompensation(),
		evaluatePayBandPosition(),
		reserveCompensationBudget(),
		releaseCompensationBudget(),
		approveProposal(),
		rejectProposal(),
		explainTransaction(),
		detectDrift(),
		createRepairPlan(),
		simulateRepair(),
	}
}

// Catalog returns the schema and capability references the definitions are
// allowed to name. Under the BOOTSTRAP profile this list is compiled in beside
// the definitions, so an unknown schema or capability fails the build rather
// than a request.
func Catalog() intent.Catalog {
	var c intent.Catalog
	seenSchema := map[string]bool{}
	seenCap := map[string]bool{}
	for _, d := range All() {
		for _, s := range []intent.SchemaRef{d.InputSchema, d.ResultSchema} {
			if !seenSchema[s.String()] {
				seenSchema[s.String()] = true
				c.Schemas = append(c.Schemas, s)
			}
		}
		for _, cap := range d.RequiredCapabilities {
			if !seenCap[cap] {
				seenCap[cap] = true
				c.Capabilities = append(c.Capabilities, cap)
			}
		}
	}
	return c
}

// NewRegistry compiles the catalog into an immutable BOOTSTRAP registry.
func NewRegistry() (*intent.Registry, error) {
	return intent.NewRegistry(intent.ProfileBootstrap, All(), Policies(), Catalog())
}

// ---------------------------------------------------------------------------
// People
// ---------------------------------------------------------------------------

func changeManager() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.people.change_manager", 1),
		DisplayName:  "ChangeManager",
		Description:  "Change the effective-dated manager relationship for one employment assignment without creating an organization cycle.",
		OwnerPlane:   "DOMAIN",
		OwnerDomain:  "PEOPLE",
		Family:       intent.FamilyChangeRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectInternalMutation,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseConformance,
		InputSchema:  schema("hcmnext.people.v1.ChangeManagerRequest/v1"),
		ResultSchema: schema("hcmnext.people.v1.ChangeManagerResult/v1"),
		PhaseDepth:   "DESIGN_CONFORMANCE_ONLY",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeReplay, intent.ModeShadow,
		},
		RiskClass:               "R2",
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "WORKER_TRANSACTION",
		SLOClass:                "EMPLOYEE_TRANSACTION",
		SubjectKinds: []string{
			"PERSON", "EMPLOYMENT", "ASSIGNMENT", "MANAGER_RELATIONSHIP", "ORGANIZATION",
		},
		RequiredCapabilities: []string{
			"people.manager.change.plan/v1", "people.manager.change.execute/v1",
		},
		GovernanceRequirements: []string{
			"governance.manager_change/v1", "approval.manager_change/v1",
		},
		Preconditions: []string{
			"people.worker_active_at_effective_time/v1",
			"people.proposed_manager_active_and_eligible/v1",
			"organization.scope_permits_relationship/v1",
		},
		Invariants: []string{
			"organization.no_self_management/v1",
			"organization.no_manager_cycle/v1",
			"people.non_overlapping_primary_manager_intervals/v1",
		},
		RequiredInputs: []intent.RequiredInput{
			{Path: "employment_ref", Kind: intent.InputKindSubjectRef, Required: true},
			{Path: "proposed_manager_ref", Kind: intent.InputKindSubjectRef, Required: true},
			{Path: "effective_time", Kind: intent.InputKindEffectiveTime, Required: true},
			{Path: "reason_ref", Kind: intent.InputKindReference, Required: true},
		},
		AllowedTransitions:             simulateOnlyChangeTransitions(),
		ApplicableNegativeStates:       mandatoryNegativeStates(),
		NegativeStatePolicyRef:         PolicyChangeTransaction,
		ApprovalRequired:               true,
		ClosurePolicyPermitsOpenRepair: false,
		IdempotencyScope:               "tenant+employment+effective_at+proposed_manager+canonical_request",
		ConflictFootprintRule:          "manager_relationship_and_context_interval/v1",
		ProposalBindingRule:            "exact_manager_change_proposal_digest/v1",
		RevalidationRule:               "manager_change_execution_revalidation/v1",
		CancellationRule:               "manager_change_phase_aware_cancel/v1",
		CompensationRule:               "append_only_manager_relationship_correction/v1",
		EvidenceRule:                   "manager_change_complete_business_story/v1",
		OutcomeContract:                "expected_manager_observed_and_reconciled/v1",
		AvailabilityPolicy:             "FAIL_CLOSED_QUEUE_AFTER_ACCEPTED/v1",
	}
}

func explainWorkerState() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.people.explain_worker_state", 1),
		DisplayName:  "ExplainWorkerState",
		Description:  "Explain the authorized current or historical worker state and its provenance.",
		OwnerPlane:   "DOMAIN",
		OwnerDomain:  "PEOPLE",
		Family:       intent.FamilyAnalyticalRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectReadOnly,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  schema("hcmnext.people.v1.ExplainWorkerStateRequest/v1"),
		ResultSchema: schema("hcmnext.people.v1.ExplainWorkerStateResult/v1"),
		PhaseDepth:   "GATE_A_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
		},
		AllowedModes:            []intent.Mode{intent.ModeExecute, intent.ModeReplay},
		RiskClass:               "R1",
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "ANALYTICAL_EXPLANATION",
		SLOClass:                "INTERACTIVE_READ",
		SubjectKinds:            []string{"WORKER"},
		RequiredCapabilities:    []string{"people.worker.history.explain/v1"},
		GovernanceRequirements: []string{
			"governance.standard_read/v1", "provenance.field_lineage/v1",
		},
		Preconditions: []string{"people.worker_reference_resolves/v1"},
		Invariants:    []string{"privacy.minimum_necessary/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "worker_ref", Kind: intent.InputKindSubjectRef, Required: true},
			{Path: "as_of", Kind: intent.InputKindEffectiveTime, Required: true},
		},
		AllowedTransitions:       zeroEffectTransitions(),
		ApplicableNegativeStates: mandatoryNegativeStates(),
		NegativeStatePolicyRef:   PolicyAnalyticalRead,
		IdempotencyScope:         "tenant+definition+canonical_request",
		ConflictFootprintRule:    "NONE",
		ProposalBindingRule:      "NOT_APPLICABLE",
		RevalidationRule:         "authorization_and_source_visibility_at_read_time/v1",
		CancellationRule:         "cancel_before_result_commit/v1",
		CompensationRule:         "NOT_APPLICABLE",
		EvidenceRule:             "analytical_request_and_source_edges/v1",
		OutcomeContract:          "explanation_answered_with_complete_visible_lineage/v1",
		AvailabilityPolicy:       "FAIL_CLOSED_ON_GOVERNANCE_USE_STALE_ALLOWED_PROVENANCE/v1",
	}
}

func promoteWorker() intent.Definition {
	return intent.Definition{
		Ref:         ref("hcmnext.people.promote_worker", 1),
		DisplayName: "PromoteWorker",
		Description: "Propose and execute a governed upward job or position change for one employment assignment.",
		OwnerPlane:  "DOMAIN",
		OwnerDomain: "PEOPLE",
		Family:      intent.FamilyChangeRequest,
		Maturity:    intent.MaturityDraftContract,
		SideEffect:  intent.SideEffectInternalMutation,
		// P1A simulates PromoteWorker and never executes it, so its P1A effect
		// class is zero even though its permanent profile mutates.
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  schema("hcmnext.people.v1.PromoteWorkerRequest/v1"),
		ResultSchema: schema("hcmnext.people.v1.PromoteWorkerResult/v1"),
		PhaseDepth:   "GATE_A_CONTRACT_GATE_B_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeReplay, intent.ModeShadow,
		},
		RiskClass:               "R3",
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "WORKER_TRANSACTION",
		SLOClass:                "EMPLOYEE_TRANSACTION",
		SubjectKinds: []string{
			"PERSON", "EMPLOYMENT", "ASSIGNMENT", "POSITION", "ORGANIZATION",
		},
		RequiredCapabilities: []string{
			"people.promote.plan/v1", "people.promote.execute/v1",
		},
		GovernanceRequirements: []string{
			"governance.promotion/v1", "approval.promotion/v1",
		},
		Preconditions: []string{
			"people.active_employment/v1", "position.target_valid/v1",
		},
		Invariants: []string{
			"people.single_primary_assignment_when_required/v1",
			"position.capacity_not_exceeded/v1",
		},
		RequiredInputs: []intent.RequiredInput{
			{Path: "employment_ref", Kind: intent.InputKindSubjectRef, Required: true},
			{Path: "target_position_ref", Kind: intent.InputKindReference, Required: true},
			{Path: "effective_time", Kind: intent.InputKindEffectiveTime, Required: true},
			{Path: "proposed_base_pay", Kind: intent.InputKindMoney, Required: false},
			{Path: "reason_ref", Kind: intent.InputKindReference, Required: true},
		},
		AllowedTransitions:             simulateOnlyChangeTransitions(),
		ApplicableNegativeStates:       mandatoryNegativeStates(),
		NegativeStatePolicyRef:         PolicyChangeTransaction,
		ApprovalRequired:               true,
		ClosurePolicyPermitsOpenRepair: false,
		IdempotencyScope:               "tenant+employment+requested_effective_at+canonical_request",
		ConflictFootprintRule:          "promotion_affected_fields_and_effective_interval/v1",
		ProposalBindingRule:            "exact_material_proposal_digest/v1",
		RevalidationRule:               "promotion_execution_revalidation/v1",
		CancellationRule:               "promotion_phase_aware_cancel/v1",
		CompensationRule:               "promotion_repair_plan/v1",
		EvidenceRule:                   "promotion_complete_business_story/v1",
		OutcomeContract:                "promotion_executed_reconciled_and_later_outcome_linkable/v1",
		AvailabilityPolicy:             "FAIL_CLOSED_QUEUE_AFTER_ACCEPTED/v1",
	}
}

// ---------------------------------------------------------------------------
// Rewards and workforce budget
// ---------------------------------------------------------------------------

func changeBasePay() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.rewards.change_base_pay", 1),
		DisplayName:  "ChangeBasePay",
		Description:  "Propose and execute a governed base salary or hourly-rate change.",
		OwnerPlane:   "DOMAIN",
		OwnerDomain:  "REWARDS",
		Family:       intent.FamilyChangeRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectInternalMutation,
		EffectClass:  intent.EffectClassInternalMutation,
		Release:      intent.ReleaseP1B,
		InputSchema:  schema("hcmnext.rewards.v1.ChangeBasePayRequest/v1"),
		ResultSchema: schema("hcmnext.rewards.v1.ChangeBasePayResult/v1"),
		PhaseDepth:   "GATE_A_CONTRACT_GATE_B_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeExecute, intent.ModeReplay,
			intent.ModeRepair, intent.ModeShadow,
		},
		RiskClass:               "R4",
		DataClassificationFloor: "RESTRICTED_COMPENSATION",
		RetentionClass:          "COMPENSATION_TRANSACTION",
		SLOClass:                "EMPLOYEE_TRANSACTION",
		SubjectKinds:            []string{"EMPLOYMENT", "ASSIGNMENT", "COMPENSATION"},
		RequiredCapabilities: []string{
			"rewards.compensation.change.plan/v1", "rewards.compensation.change.execute/v1",
		},
		GovernanceRequirements: []string{
			"governance.compensation_change/v1", "approval.compensation_change/v1",
		},
		Preconditions: []string{"rewards.currency_and_pay_basis_valid/v1"},
		Invariants:    []string{"rewards.non_overlapping_base_pay_intervals/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "employment_ref", Kind: intent.InputKindSubjectRef, Required: true},
			{Path: "pay_component_ref", Kind: intent.InputKindReference, Required: true},
			{Path: "proposed_amount", Kind: intent.InputKindMoney, Required: true},
			{Path: "effective_time", Kind: intent.InputKindEffectiveTime, Required: true},
		},
		AllowedTransitions:             writeChangeTransitions(),
		ApplicableNegativeStates:       mandatoryNegativeStates(),
		NegativeStatePolicyRef:         PolicyChangeTransaction,
		ApprovalRequired:               true,
		ClosurePolicyPermitsOpenRepair: false,
		IdempotencyScope:               "tenant+employment+pay_component+effective_at+canonical_request",
		ConflictFootprintRule:          "base_pay_effective_interval/v1",
		ProposalBindingRule:            "exact_material_proposal_digest/v1",
		RevalidationRule:               "compensation_execution_revalidation/v1",
		CancellationRule:               "compensation_phase_aware_cancel/v1",
		CompensationRule:               "append_only_compensation_correction/v1",
		EvidenceRule:                   "compensation_change_and_calculation_trace/v1",
		OutcomeContract:                "expected_base_pay_observed_and_reconciled/v1",
		AvailabilityPolicy:             "FAIL_CLOSED_QUEUE_AFTER_ACCEPTED/v1",
	}
}

func simulateCompensation() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.rewards.simulate_compensation", 1),
		DisplayName:  "SimulateCompensation",
		Description:  "Calculate the effects and warnings of a proposed compensation change without side effects.",
		OwnerPlane:   "DOMAIN",
		OwnerDomain:  "REWARDS",
		Family:       intent.FamilyCalculationRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectPure,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  schema("hcmnext.rewards.v1.SimulateCompensationRequest/v1"),
		ResultSchema: schema("hcmnext.rewards.v1.CompensationSimulation/v1"),
		PhaseDepth:   "GATE_A_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeExecute, intent.ModeReplay, intent.ModeShadow,
		},
		RiskClass:               "R2",
		DataClassificationFloor: "RESTRICTED_COMPENSATION",
		RetentionClass:          "COMPENSATION_ANALYSIS",
		SLOClass:                "INTERACTIVE_CALCULATION",
		SubjectKinds:            []string{"EMPLOYMENT", "COMPENSATION", "POSITION"},
		RequiredCapabilities:    []string{"rewards.compensation.simulate/v1"},
		GovernanceRequirements:  []string{"governance.compensation_analysis/v1"},
		Preconditions:           []string{"rewards.complete_simulation_inputs/v1"},
		Invariants:              []string{"calculation.deterministic_for_pinned_inputs/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "employment_ref", Kind: intent.InputKindSubjectRef, Required: true},
			{Path: "proposed_amount", Kind: intent.InputKindMoney, Required: true},
			{Path: "effective_time", Kind: intent.InputKindEffectiveTime, Required: true},
		},
		AllowedTransitions:       zeroEffectTransitions(),
		ApplicableNegativeStates: mandatoryNegativeStates(),
		NegativeStatePolicyRef:   PolicyPureCalculation,
		IdempotencyScope:         "tenant+definition+canonical_request+control_snapshots",
		ConflictFootprintRule:    "NONE",
		ProposalBindingRule:      "simulation_digest_may_become_proposal_input/v1",
		RevalidationRule:         "recalculate_when_material_input_changes/v1",
		CancellationRule:         "cancel_before_result_commit/v1",
		CompensationRule:         "NOT_APPLICABLE",
		EvidenceRule:             "input_rule_and_result_digest/v1",
		OutcomeContract:          "reproducible_simulation_with_warnings/v1",
		AvailabilityPolicy:       "FAIL_CLOSED/v1",
	}
}

func evaluatePayBandPosition() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.rewards.evaluate_pay_band_position", 1),
		DisplayName:  "EvaluatePayBandPosition",
		Description:  "Calculate a proposed or current pay position against the applicable versioned range.",
		OwnerPlane:   "DOMAIN",
		OwnerDomain:  "REWARDS",
		Family:       intent.FamilyCalculationRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectPure,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  schema("hcmnext.rewards.v1.EvaluatePayBandPositionRequest/v1"),
		ResultSchema: schema("hcmnext.rewards.v1.PayBandPositionResult/v1"),
		PhaseDepth:   "GATE_A_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeExecute, intent.ModeReplay,
		},
		RiskClass:               "R2",
		DataClassificationFloor: "RESTRICTED_COMPENSATION",
		RetentionClass:          "COMPENSATION_ANALYSIS",
		SLOClass:                "INTERACTIVE_CALCULATION",
		SubjectKinds:            []string{"EMPLOYMENT", "COMPENSATION", "PAY_BAND"},
		RequiredCapabilities:    []string{"rewards.pay_band.position.evaluate/v1"},
		GovernanceRequirements:  []string{"governance.compensation_analysis/v1"},
		Preconditions:           []string{"rewards.pay_band_and_currency_resolve/v1"},
		Invariants:              []string{"calculation.deterministic_for_pinned_inputs/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "employment_ref", Kind: intent.InputKindSubjectRef, Required: true},
			{Path: "pay_band_ref", Kind: intent.InputKindReference, Required: true},
			{Path: "amount", Kind: intent.InputKindMoney, Required: true},
		},
		AllowedTransitions:       zeroEffectTransitions(),
		ApplicableNegativeStates: mandatoryNegativeStates(),
		NegativeStatePolicyRef:   PolicyPureCalculation,
		IdempotencyScope:         "tenant+definition+canonical_request+reference_data_digest",
		ConflictFootprintRule:    "NONE",
		ProposalBindingRule:      "result_digest_may_become_proposal_input/v1",
		RevalidationRule:         "recalculate_when_band_or_pay_changes/v1",
		CancellationRule:         "cancel_before_result_commit/v1",
		CompensationRule:         "NOT_APPLICABLE",
		EvidenceRule:             "band_version_input_and_result_digest/v1",
		OutcomeContract:          "reproducible_band_position/v1",
		AvailabilityPolicy:       "FAIL_CLOSED/v1",
	}
}

func reserveCompensationBudget() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.rewards.reserve_compensation_budget", 1),
		DisplayName:  "ReserveCompensationBudget",
		Description:  "Reserve a bounded amount from an authoritative workforce budget for a proposal.",
		OwnerPlane:   "DOMAIN",
		OwnerDomain:  "WORKFORCE_BUDGET",
		Family:       intent.FamilyChangeRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectInternalMutation,
		EffectClass:  intent.EffectClassInternalMutation,
		Release:      intent.ReleaseP1B,
		InputSchema:  schema("hcmnext.budget.v1.ReserveCompensationBudgetRequest/v1"),
		ResultSchema: schema("hcmnext.budget.v1.BudgetReservation/v1"),
		PhaseDepth:   "GATE_B_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorService,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeExecute, intent.ModeReplay, intent.ModeRepair,
		},
		RiskClass:               "R3",
		DataClassificationFloor: "RESTRICTED_FINANCIAL",
		RetentionClass:          "FINANCIAL_CONTROL",
		SLOClass:                "EMPLOYEE_TRANSACTION",
		SubjectKinds:            []string{"BUDGET", "PROPOSAL"},
		RequiredCapabilities:    []string{"budget.compensation.reserve/v1"},
		GovernanceRequirements:  []string{"governance.budget_reservation/v1"},
		Preconditions:           []string{"budget.authority_and_available_balance/v1"},
		Invariants:              []string{"budget.no_over_reservation/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "budget_ref", Kind: intent.InputKindReference, Required: true},
			{Path: "proposal_digest", Kind: intent.InputKindReference, Required: true},
			{Path: "amount", Kind: intent.InputKindMoney, Required: true},
		},
		AllowedTransitions:             writeChangeTransitions(),
		ApplicableNegativeStates:       mandatoryNegativeStates(),
		NegativeStatePolicyRef:         PolicyChangeTransaction,
		ApprovalRequired:               false,
		ClosurePolicyPermitsOpenRepair: false,
		IdempotencyScope:               "tenant+budget+proposal_digest",
		ConflictFootprintRule:          "budget_amount_and_effective_interval/v1",
		ProposalBindingRule:            "reservation_bound_to_proposal_digest/v1",
		RevalidationRule:               "budget_authority_and_balance_at_commit/v1",
		CancellationRule:               "release_unconsumed_reservation/v1",
		CompensationRule:               "release_or_repair_reservation/v1",
		EvidenceRule:                   "budget_reservation_lifecycle/v1",
		OutcomeContract:                "reservation_consumed_or_released_exactly_once/v1",
		AvailabilityPolicy:             "FAIL_CLOSED/v1",
	}
}

func releaseCompensationBudget() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.rewards.release_compensation_budget", 1),
		DisplayName:  "ReleaseCompensationBudget",
		Description:  "Release an unconsumed compensation budget reservation append-only.",
		OwnerPlane:   "DOMAIN",
		OwnerDomain:  "WORKFORCE_BUDGET",
		Family:       intent.FamilyChangeRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectInternalMutation,
		EffectClass:  intent.EffectClassInternalMutation,
		Release:      intent.ReleaseP1B,
		InputSchema:  schema("hcmnext.budget.v1.ReleaseCompensationBudgetRequest/v1"),
		ResultSchema: schema("hcmnext.budget.v1.BudgetReservationRelease/v1"),
		PhaseDepth:   "GATE_B_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorService, intent.InitiatorHuman,
		},
		AllowedModes: []intent.Mode{
			intent.ModeExecute, intent.ModeReplay, intent.ModeRepair,
		},
		RiskClass:               "R3",
		DataClassificationFloor: "RESTRICTED_FINANCIAL",
		RetentionClass:          "FINANCIAL_CONTROL",
		SLOClass:                "EMPLOYEE_TRANSACTION",
		SubjectKinds:            []string{"BUDGET", "PROPOSAL"},
		RequiredCapabilities:    []string{"budget.compensation.release/v1"},
		GovernanceRequirements:  []string{"governance.budget_release/v1"},
		Preconditions:           []string{"budget.reservation_exists_and_unconsumed/v1"},
		Invariants:              []string{"budget.release_at_most_once/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "reservation_ref", Kind: intent.InputKindReference, Required: true},
			{Path: "release_reason_ref", Kind: intent.InputKindReference, Required: true},
		},
		AllowedTransitions:             writeChangeTransitions(),
		ApplicableNegativeStates:       mandatoryNegativeStates(),
		NegativeStatePolicyRef:         PolicyChangeTransaction,
		ApprovalRequired:               false,
		ClosurePolicyPermitsOpenRepair: false,
		IdempotencyScope:               "tenant+reservation+release_reason",
		ConflictFootprintRule:          "reservation_identity/v1",
		ProposalBindingRule:            "original_reservation_binding/v1",
		RevalidationRule:               "reservation_unconsumed_at_commit/v1",
		CancellationRule:               "NOT_CANCELLABLE_AFTER_COMMIT",
		CompensationRule:               "append_corrective_budget_event/v1",
		EvidenceRule:                   "budget_reservation_lifecycle/v1",
		OutcomeContract:                "reservation_released_exactly_once/v1",
		AvailabilityPolicy:             "FAIL_CLOSED/v1",
	}
}

// ---------------------------------------------------------------------------
// Human work
// ---------------------------------------------------------------------------

func approveProposal() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.work.approve_proposal", 1),
		DisplayName:  "ApproveProposal",
		Description:  "Record an authorized approval bound to one immutable material proposal digest.",
		OwnerPlane:   "WORKFLOW",
		OwnerDomain:  "HUMAN_WORK",
		Family:       intent.FamilyChangeRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectInternalMutation,
		EffectClass:  intent.EffectClassInternalMutation,
		Release:      intent.ReleaseP1B,
		InputSchema:  schema("hcmnext.work.v1.ApproveProposalRequest/v1"),
		ResultSchema: schema("hcmnext.work.v1.ApprovalDecision/v1"),
		PhaseDepth:   "GATE_B_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman,
		},
		AllowedModes:            []intent.Mode{intent.ModeExecute, intent.ModeReplay},
		RiskClass:               "R3",
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "BUSINESS_DECISION",
		SLOClass:                "HUMAN_INTERACTION",
		SubjectKinds:            []string{"PROPOSAL", "WORK_ITEM"},
		RequiredCapabilities:    []string{"work.proposal.approve/v1"},
		GovernanceRequirements: []string{
			"governance.approval_authority/v1", "governance.separation_of_duties/v1",
		},
		Preconditions: []string{"work.proposal_digest_current/v1"},
		Invariants:    []string{"approval.exact_digest_binding/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "requirement_id", Kind: intent.InputKindReference, Required: true},
			{Path: "proposal_digest", Kind: intent.InputKindReference, Required: true},
		},
		AllowedTransitions:             writeChangeTransitions(),
		ApplicableNegativeStates:       mandatoryNegativeStates(),
		NegativeStatePolicyRef:         PolicyChangeTransaction,
		ApprovalRequired:               false,
		ClosurePolicyPermitsOpenRepair: false,
		IdempotencyScope:               "tenant+requirement+proposal_digest+principal",
		ConflictFootprintRule:          "approval_requirement_and_proposal_digest/v1",
		ProposalBindingRule:            "exact_material_proposal_digest/v1",
		RevalidationRule:               "approver_authority_at_decision_time/v1",
		CancellationRule:               "invalidate_not_delete/v1",
		CompensationRule:               "append_approval_invalidation/v1",
		EvidenceRule:                   "signed_approval_decision/v1",
		OutcomeContract:                "approval_available_only_for_bound_proposal/v1",
		AvailabilityPolicy:             "FAIL_CLOSED_QUEUE_TASK/v1",
	}
}

func rejectProposal() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.work.reject_proposal", 1),
		DisplayName:  "RejectProposal",
		Description:  "Record an authorized rejection of one immutable proposal with a governed reason.",
		OwnerPlane:   "WORKFLOW",
		OwnerDomain:  "HUMAN_WORK",
		Family:       intent.FamilyChangeRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectInternalMutation,
		EffectClass:  intent.EffectClassInternalMutation,
		Release:      intent.ReleaseP1B,
		InputSchema:  schema("hcmnext.work.v1.RejectProposalRequest/v1"),
		ResultSchema: schema("hcmnext.work.v1.ApprovalDecision/v1"),
		PhaseDepth:   "GATE_B_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman,
		},
		AllowedModes:            []intent.Mode{intent.ModeExecute, intent.ModeReplay},
		RiskClass:               "R3",
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "BUSINESS_DECISION",
		SLOClass:                "HUMAN_INTERACTION",
		SubjectKinds:            []string{"PROPOSAL", "WORK_ITEM"},
		RequiredCapabilities:    []string{"work.proposal.reject/v1"},
		GovernanceRequirements:  []string{"governance.approval_authority/v1"},
		Preconditions:           []string{"work.proposal_digest_current/v1"},
		Invariants:              []string{"approval.exact_digest_binding/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "requirement_id", Kind: intent.InputKindReference, Required: true},
			{Path: "proposal_digest", Kind: intent.InputKindReference, Required: true},
			{Path: "reason_ref", Kind: intent.InputKindReference, Required: true},
		},
		AllowedTransitions:             writeChangeTransitions(),
		ApplicableNegativeStates:       mandatoryNegativeStates(),
		NegativeStatePolicyRef:         PolicyChangeTransaction,
		ApprovalRequired:               false,
		ClosurePolicyPermitsOpenRepair: false,
		IdempotencyScope:               "tenant+requirement+proposal_digest+principal",
		ConflictFootprintRule:          "approval_requirement_and_proposal_digest/v1",
		ProposalBindingRule:            "exact_material_proposal_digest/v1",
		RevalidationRule:               "approver_authority_at_decision_time/v1",
		CancellationRule:               "invalidate_not_delete/v1",
		CompensationRule:               "append_decision_correction/v1",
		EvidenceRule:                   "signed_rejection_decision_and_reason/v1",
		OutcomeContract:                "rejected_proposal_cannot_execute/v1",
		AvailabilityPolicy:             "FAIL_CLOSED_QUEUE_TASK/v1",
	}
}

// ---------------------------------------------------------------------------
// Intelligence and operations assurance
// ---------------------------------------------------------------------------

func explainTransaction() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.intelligence.explain_transaction", 1),
		DisplayName:  "ExplainTransaction",
		Description:  "Explain an authorized business transaction through intent, decision, event, effect, and observation evidence.",
		OwnerPlane:   "INTELLIGENCE",
		OwnerDomain:  "PROVENANCE",
		Family:       intent.FamilyAnalyticalRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectReadOnly,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  schema("hcmnext.intelligence.v1.ExplainTransactionRequest/v1"),
		ResultSchema: schema("hcmnext.intelligence.v1.TransactionExplanation/v1"),
		PhaseDepth:   "GATE_A_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
		},
		AllowedModes:            []intent.Mode{intent.ModeExecute, intent.ModeReplay},
		RiskClass:               "R2",
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "ANALYTICAL_EXPLANATION",
		SLOClass:                "INTERACTIVE_READ",
		SubjectKinds:            []string{"BUSINESS_TRANSACTION"},
		RequiredCapabilities:    []string{"provenance.transaction.explain/v1"},
		GovernanceRequirements:  []string{"governance.provenance_read/v1"},
		Preconditions:           []string{"provenance.transaction_reference_resolves/v1"},
		Invariants: []string{
			"privacy.minimum_necessary/v1", "provenance.no_hidden_edge_inference/v1",
		},
		RequiredInputs: []intent.RequiredInput{
			{Path: "transaction_ref", Kind: intent.InputKindSubjectRef, Required: true},
		},
		AllowedTransitions:       zeroEffectTransitions(),
		ApplicableNegativeStates: mandatoryNegativeStates(),
		NegativeStatePolicyRef:   PolicyAnalyticalRead,
		IdempotencyScope:         "tenant+definition+canonical_request+visibility_snapshot",
		ConflictFootprintRule:    "NONE",
		ProposalBindingRule:      "NOT_APPLICABLE",
		RevalidationRule:         "authorization_at_each_edge_traversal/v1",
		CancellationRule:         "cancel_before_result_commit/v1",
		CompensationRule:         "NOT_APPLICABLE",
		EvidenceRule:             "explanation_query_and_visible_edge_set/v1",
		OutcomeContract:          "complete_authorized_explanation_or_explicit_gap/v1",
		AvailabilityPolicy:       "FAIL_CLOSED_ON_GOVERNANCE_USE_PARTIAL_WITH_GAPS/v1",
	}
}

func detectDrift() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.operations.detect_drift", 1),
		DisplayName:  "DetectDrift",
		Description:  "Compare intended, authoritative, derived, and observed state and classify mismatches.",
		OwnerPlane:   "OPERATIONS_ASSURANCE",
		OwnerDomain:  "RECONCILIATION",
		Family:       intent.FamilyAnalyticalRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectReadOnly,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  schema("hcmnext.operations.v1.DetectDriftRequest/v1"),
		ResultSchema: schema("hcmnext.operations.v1.DriftReport/v1"),
		PhaseDepth:   "GATE_A_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
			intent.InitiatorSchedule, intent.InitiatorSystemEvent,
		},
		AllowedModes: []intent.Mode{
			intent.ModeExecute, intent.ModeReplay, intent.ModeShadow,
		},
		RiskClass:               "R2",
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "RECONCILIATION_EVIDENCE",
		SLOClass:                "OPERATIONAL_DIAGNOSTIC",
		SubjectKinds:            []string{"BUSINESS_TRANSACTION", "EXTERNAL_RESOURCE"},
		RequiredCapabilities:    []string{"reconciliation.drift.detect/v1"},
		GovernanceRequirements:  []string{"governance.reconciliation_read/v1"},
		Preconditions:           []string{"reconciliation.comparable_snapshots_exist/v1"},
		Invariants:              []string{"reconciliation.source_authority_respected/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "comparison_set_ref", Kind: intent.InputKindReference, Required: true},
			{Path: "watermarks", Kind: intent.InputKindReference, Required: true},
		},
		AllowedTransitions:       zeroEffectTransitions(),
		ApplicableNegativeStates: mandatoryNegativeStates(),
		NegativeStatePolicyRef:   PolicyAnalyticalRead,
		IdempotencyScope:         "tenant+comparison_set+watermarks",
		ConflictFootprintRule:    "NONE",
		ProposalBindingRule:      "NOT_APPLICABLE",
		RevalidationRule:         "report_watermarks_and_authority_versions/v1",
		CancellationRule:         "cancel_before_result_commit/v1",
		CompensationRule:         "NOT_APPLICABLE",
		EvidenceRule:             "intended_observed_diff_with_watermarks/v1",
		OutcomeContract:          "every_mismatch_classified_or_explicitly_unknown/v1",
		AvailabilityPolicy:       "QUEUE_FOR_LATER/v1",
	}
}

func createRepairPlan() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.operations.create_repair_plan", 1),
		DisplayName:  "CreateRepairPlan",
		Description:  "Produce a bounded, governed corrective plan without executing its effects.",
		OwnerPlane:   "OPERATIONS_ASSURANCE",
		OwnerDomain:  "REPAIR",
		Family:       intent.FamilyAnalyticalRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectReadOnly,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  schema("hcmnext.operations.v1.CreateRepairPlanRequest/v1"),
		ResultSchema: schema("hcmnext.operations.v1.RepairPlan/v1"),
		PhaseDepth:   "GATE_A_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
			intent.InitiatorSystemEvent,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeExecute, intent.ModeReplay,
		},
		RiskClass:               "R3",
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "REPAIR_EVIDENCE",
		SLOClass:                "OPERATIONAL_DIAGNOSTIC",
		SubjectKinds:            []string{"DRIFT", "INCIDENT", "BUSINESS_TRANSACTION"},
		RequiredCapabilities:    []string{"repair.plan.create/v1"},
		GovernanceRequirements:  []string{"governance.repair_plan/v1"},
		Preconditions:           []string{"repair.diagnosis_has_evidence/v1"},
		Invariants:              []string{"repair.plan_has_bounded_targets/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "diagnosis_digest", Kind: intent.InputKindReference, Required: true},
			{Path: "policy_snapshot_ref", Kind: intent.InputKindReference, Required: true},
		},
		AllowedTransitions:       zeroEffectTransitions(),
		ApplicableNegativeStates: mandatoryNegativeStates(),
		NegativeStatePolicyRef:   PolicyAnalyticalRead,
		IdempotencyScope:         "tenant+diagnosis_digest+policy_snapshot",
		ConflictFootprintRule:    "planned_only_until_approved/v1",
		ProposalBindingRule:      "repair_plan_digest/v1",
		RevalidationRule:         "repair_targets_revalidated_before_execution/v1",
		CancellationRule:         "cancel_unexecuted_plan/v1",
		CompensationRule:         "repair_plan_must_declare_compensation/v1",
		EvidenceRule:             "diagnosis_to_action_provenance/v1",
		OutcomeContract:          "executable_or_explicitly_blocked_repair_plan/v1",
		AvailabilityPolicy:       "QUEUE_FOR_LATER/v1",
	}
}

func simulateRepair() intent.Definition {
	return intent.Definition{
		Ref:          ref("hcmnext.operations.simulate_repair", 1),
		DisplayName:  "SimulateRepair",
		Description:  "Evaluate a repair plan against pinned state without performing business effects.",
		OwnerPlane:   "OPERATIONS_ASSURANCE",
		OwnerDomain:  "REPAIR",
		Family:       intent.FamilyCalculationRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectPure,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  schema("hcmnext.operations.v1.SimulateRepairRequest/v1"),
		ResultSchema: schema("hcmnext.operations.v1.RepairSimulation/v1"),
		PhaseDepth:   "GATE_A_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent, intent.InitiatorService,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeExecute, intent.ModeReplay, intent.ModeShadow,
		},
		RiskClass:               "R3",
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "REPAIR_EVIDENCE",
		SLOClass:                "OPERATIONAL_DIAGNOSTIC",
		SubjectKinds:            []string{"REPAIR_PLAN"},
		RequiredCapabilities:    []string{"repair.plan.simulate/v1"},
		GovernanceRequirements:  []string{"governance.repair_simulation/v1"},
		Preconditions:           []string{"repair.plan_digest_resolves/v1"},
		Invariants:              []string{"simulation.no_business_side_effect/v1"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "repair_plan_digest", Kind: intent.InputKindReference, Required: true},
			{Path: "state_watermarks", Kind: intent.InputKindReference, Required: true},
		},
		AllowedTransitions:       zeroEffectTransitions(),
		ApplicableNegativeStates: mandatoryNegativeStates(),
		NegativeStatePolicyRef:   PolicyPureCalculation,
		IdempotencyScope:         "tenant+repair_plan_digest+state_watermarks",
		ConflictFootprintRule:    "NONE",
		ProposalBindingRule:      "simulation_bound_to_repair_plan_digest/v1",
		RevalidationRule:         "resimulate_when_target_state_changes/v1",
		CancellationRule:         "cancel_before_result_commit/v1",
		CompensationRule:         "NOT_APPLICABLE",
		EvidenceRule:             "repair_input_output_and_warning_digest/v1",
		OutcomeContract:          "reproducible_effect_and_risk_preview/v1",
		AvailabilityPolicy:       "FAIL_CLOSED/v1",
	}
}
