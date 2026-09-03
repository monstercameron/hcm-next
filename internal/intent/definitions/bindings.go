package definitions

import "github.com/monstercameron/hcm-next/internal/intent"

// Bindings returns one IntentEntityBinding per catalog definition. A binding is
// what turns "this definition exists" into "this definition is bound to exact
// model behaviour": which covered aggregate roots it touches, which properties
// it reads and writes, which decisions it makes, which evidence it must
// produce, which effects it may cause, which lifecycle transitions it uses on
// each of the five dimensions, which negative-state policy decides its
// uncertain facts, and which conformance scenarios exercise it.
//
// Every reference is a stable id or schema path. A binding never names a prose
// label, so renaming a display label cannot break a binding.
func Bindings() []intent.Binding {
	return []intent.Binding{
		changeManagerBinding(),
		explainWorkerStateBinding(),
		promoteWorkerBinding(),
		changeBasePayBinding(),
		simulateCompensationBinding(),
		evaluatePayBandPositionBinding(),
		reserveCompensationBudgetBinding(),
		releaseCompensationBudgetBinding(),
		approveProposalBinding(),
		rejectProposalBinding(),
		explainTransactionBinding(),
		detectDriftBinding(),
		createRepairPlanBinding(),
		simulateRepairBinding(),
	}
}

// Coverage returns the MODEL-016 coverage report for the compiled catalog.
func Coverage(reg *intent.Registry) intent.CoverageReport {
	return intent.CheckCoverage(reg, Bindings())
}

func changeManagerBinding() intent.Binding {
	return intent.Binding{
		Definition:     ref("hcmnext.people.change_manager", 1),
		AggregateRoots: []string{"Person", "Employment", "Assignment", "OrganizationUnit", "OrganizationRelationship"},
		ReadProperties: []string{
			"employment.status",
			"assignment.manager_relationship",
			"organization_unit.hierarchy_path",
			"person.identity",
		},
		WriteProperties: []string{
			"organization_relationship.manager_ref",
			"organization_relationship.effective_interval",
		},
		DecisionRefs: []string{
			"decision.manager_eligibility/v1",
			"decision.organization_cycle/v1",
		},
		EvidenceRefs: []string{
			"evidence.manager_change_business_story/v1",
			"evidence.approval_decision/v1",
		},
		EffectRefs:             nil,
		Transitions:            simulateOnlyChangeTransitions(),
		NegativeStatePolicyRef: PolicyChangeTransaction,
		ScenarioRefs:           []string{"conformance.manager_change.no_cycle/v1"},
	}
}

func explainWorkerStateBinding() intent.Binding {
	return intent.Binding{
		Definition:     ref("hcmnext.people.explain_worker_state", 1),
		AggregateRoots: []string{"Person", "Worker", "Employment", "Assignment"},
		ReadProperties: []string{
			"worker.status",
			"employment.status",
			"assignment.position_ref",
			"assignment.effective_interval",
		},
		DecisionRefs: []string{"decision.field_visibility/v1"},
		EvidenceRefs: []string{"evidence.analytical_request_and_source_edges/v1"},
		EffectRefs:   nil,
		Transitions:  zeroEffectTransitions(),

		NegativeStatePolicyRef: PolicyAnalyticalRead,
		ScenarioRefs:           []string{"conformance.explain_worker_state.lineage_complete/v1"},
	}
}

func promoteWorkerBinding() intent.Binding {
	return intent.Binding{
		Definition: ref("hcmnext.people.promote_worker", 1),
		AggregateRoots: []string{
			"Person", "Worker", "Employment", "Assignment", "Position",
			"PositionOccupancy", "OrganizationUnit",
		},
		ReadProperties: []string{
			"employment.status",
			"assignment.position_ref",
			"position.capacity",
			"compensation_package.base_component_ref",
		},
		WriteProperties: []string{
			"assignment.position_ref",
			"assignment.effective_interval",
			"position_occupancy.occupant_ref",
		},
		ChildDefinitions: []intent.Ref{
			ref("hcmnext.rewards.change_base_pay", 1),
			ref("hcmnext.rewards.reserve_compensation_budget", 1),
		},
		DecisionRefs: []string{
			"decision.promotion_eligibility/v1",
			"decision.position_capacity/v1",
			"decision.source_authority_by_field/v1",
		},
		EvidenceRefs: []string{
			"evidence.promotion_complete_business_story/v1",
			"evidence.simulation_input_and_result/v1",
		},
		EffectRefs:             nil,
		Transitions:            simulateOnlyChangeTransitions(),
		NegativeStatePolicyRef: PolicyChangeTransaction,
		ScenarioRefs: []string{
			"conformance.promotion.no_effect_simulation/v1",
			"conformance.promotion.stale_baseline/v1",
		},
	}
}

func changeBasePayBinding() intent.Binding {
	return intent.Binding{
		Definition: ref("hcmnext.rewards.change_base_pay", 1),
		AggregateRoots: []string{
			"Employment", "Assignment", "CompensationPackage", "CompensationComponent",
		},
		ReadProperties: []string{
			"compensation_package.currency",
			"compensation_component.amount",
			"compensation_component.effective_interval",
		},
		WriteProperties: []string{
			"compensation_component.amount",
			"compensation_component.effective_interval",
		},
		DecisionRefs: []string{
			"decision.pay_basis_valid/v1",
			"decision.source_authority_by_field/v1",
		},
		EvidenceRefs: []string{
			"evidence.compensation_change_and_calculation_trace/v1",
		},
		EffectRefs:             []string{"effect.compensation_component_append/v1"},
		Transitions:            writeChangeTransitions(),
		NegativeStatePolicyRef: PolicyChangeTransaction,
		ScenarioRefs:           []string{"conformance.base_pay.no_overlapping_intervals/v1"},
	}
}

func simulateCompensationBinding() intent.Binding {
	return intent.Binding{
		Definition: ref("hcmnext.rewards.simulate_compensation", 1),
		AggregateRoots: []string{
			"Employment", "CompensationPackage", "CompensationComponent", "Position",
		},
		ReadProperties: []string{
			"compensation_component.amount",
			"compensation_package.currency",
			"position.pay_band_ref",
		},
		DecisionRefs:           []string{"decision.compensation_simulation_warnings/v1"},
		EvidenceRefs:           []string{"evidence.input_rule_and_result_digest/v1"},
		EffectRefs:             nil,
		Transitions:            zeroEffectTransitions(),
		NegativeStatePolicyRef: PolicyPureCalculation,
		ScenarioRefs:           []string{"conformance.simulate_compensation.deterministic/v1"},
	}
}

func evaluatePayBandPositionBinding() intent.Binding {
	return intent.Binding{
		Definition: ref("hcmnext.rewards.evaluate_pay_band_position", 1),
		AggregateRoots: []string{
			"Employment", "CompensationPackage", "CompensationComponent",
		},
		ReadProperties: []string{
			"compensation_component.amount",
			"compensation_package.currency",
		},
		DecisionRefs:           []string{"decision.pay_band_position/v1"},
		EvidenceRefs:           []string{"evidence.band_version_input_and_result_digest/v1"},
		EffectRefs:             nil,
		Transitions:            zeroEffectTransitions(),
		NegativeStatePolicyRef: PolicyPureCalculation,
		ScenarioRefs:           []string{"conformance.pay_band.reproducible_position/v1"},
	}
}

func reserveCompensationBudgetBinding() intent.Binding {
	return intent.Binding{
		Definition:     ref("hcmnext.rewards.reserve_compensation_budget", 1),
		AggregateRoots: []string{"BudgetReservation", "ProposalRevision"},
		ReadProperties: []string{
			"budget_reservation.available_balance",
			"proposal_revision.material_digest",
		},
		WriteProperties: []string{
			"budget_reservation.amount",
			"budget_reservation.expiry",
		},
		DecisionRefs:           []string{"decision.budget_authority_and_balance/v1"},
		EvidenceRefs:           []string{"evidence.budget_reservation_lifecycle/v1"},
		EffectRefs:             []string{"effect.budget_reservation_append/v1"},
		Transitions:            writeChangeTransitions(),
		NegativeStatePolicyRef: PolicyChangeTransaction,
		ScenarioRefs:           []string{"conformance.budget.no_over_reservation/v1"},
	}
}

func releaseCompensationBudgetBinding() intent.Binding {
	return intent.Binding{
		Definition:     ref("hcmnext.rewards.release_compensation_budget", 1),
		AggregateRoots: []string{"BudgetReservation", "ProposalRevision"},
		ReadProperties: []string{
			"budget_reservation.state",
			"budget_reservation.amount",
		},
		WriteProperties:        []string{"budget_reservation.state"},
		DecisionRefs:           []string{"decision.reservation_unconsumed/v1"},
		EvidenceRefs:           []string{"evidence.budget_reservation_lifecycle/v1"},
		EffectRefs:             []string{"effect.budget_release_append/v1"},
		Transitions:            writeChangeTransitions(),
		NegativeStatePolicyRef: PolicyChangeTransaction,
		ScenarioRefs:           []string{"conformance.budget.release_at_most_once/v1"},
	}
}

func approveProposalBinding() intent.Binding {
	return intent.Binding{
		Definition:     ref("hcmnext.work.approve_proposal", 1),
		AggregateRoots: []string{"ProposalRevision", "ApprovalBinding", "IntentInstance"},
		ReadProperties: []string{
			"proposal_revision.material_digest",
			"intent_instance.lifecycle",
		},
		WriteProperties: []string{
			"approval_binding.decision",
			"approval_binding.approved_proposal_digest",
		},
		DecisionRefs: []string{
			"decision.approval_authority/v1",
			"decision.separation_of_duties/v1",
		},
		EvidenceRefs:           []string{"evidence.signed_approval_decision/v1"},
		EffectRefs:             []string{"effect.approval_binding_append/v1"},
		Transitions:            writeChangeTransitions(),
		NegativeStatePolicyRef: PolicyChangeTransaction,
		ScenarioRefs:           []string{"conformance.approval.exact_digest_binding/v1"},
	}
}

func rejectProposalBinding() intent.Binding {
	return intent.Binding{
		Definition:     ref("hcmnext.work.reject_proposal", 1),
		AggregateRoots: []string{"ProposalRevision", "ApprovalBinding", "IntentInstance"},
		ReadProperties: []string{
			"proposal_revision.material_digest",
			"intent_instance.lifecycle",
		},
		WriteProperties: []string{
			"approval_binding.decision",
			"approval_binding.reason_ref",
		},
		DecisionRefs:           []string{"decision.approval_authority/v1"},
		EvidenceRefs:           []string{"evidence.signed_rejection_decision_and_reason/v1"},
		EffectRefs:             []string{"effect.approval_binding_append/v1"},
		Transitions:            writeChangeTransitions(),
		NegativeStatePolicyRef: PolicyChangeTransaction,
		ScenarioRefs:           []string{"conformance.rejection.cannot_execute/v1"},
	}
}

func explainTransactionBinding() intent.Binding {
	return intent.Binding{
		Definition: ref("hcmnext.intelligence.explain_transaction", 1),
		AggregateRoots: []string{
			"IntentInstance", "ProposalRevision", "ApprovalBinding",
			"ExecutionBinding", "Observation",
		},
		ReadProperties: []string{
			"intent_instance.lifecycle",
			"proposal_revision.material_digest",
			"approval_binding.decision",
			"execution_binding.transaction_plan_id",
			"observation.observed_state",
		},
		DecisionRefs:           []string{"decision.edge_visibility/v1"},
		EvidenceRefs:           []string{"evidence.explanation_query_and_visible_edge_set/v1"},
		EffectRefs:             nil,
		Transitions:            zeroEffectTransitions(),
		NegativeStatePolicyRef: PolicyAnalyticalRead,
		ScenarioRefs:           []string{"conformance.explain_transaction.explicit_gaps/v1"},
	}
}

func detectDriftBinding() intent.Binding {
	return intent.Binding{
		Definition: ref("hcmnext.operations.detect_drift", 1),
		AggregateRoots: []string{
			"IntentInstance", "ConnectorOperation", "Observation",
		},
		ReadProperties: []string{
			"observation.observed_state",
			"connector_operation.watermark",
			"intent_instance.lifecycle",
		},
		DecisionRefs:           []string{"decision.mismatch_classification/v1"},
		EvidenceRefs:           []string{"evidence.intended_observed_diff_with_watermarks/v1"},
		EffectRefs:             nil,
		Transitions:            zeroEffectTransitions(),
		NegativeStatePolicyRef: PolicyAnalyticalRead,
		ScenarioRefs:           []string{"conformance.drift.every_mismatch_classified/v1"},
	}
}

func createRepairPlanBinding() intent.Binding {
	return intent.Binding{
		Definition: ref("hcmnext.operations.create_repair_plan", 1),
		AggregateRoots: []string{
			"RepairPlan", "Observation", "IntentInstance",
		},
		ReadProperties: []string{
			"observation.observed_state",
			"intent_instance.lifecycle",
			"repair_plan.targets",
		},
		DecisionRefs:           []string{"decision.repair_target_bounds/v1"},
		EvidenceRefs:           []string{"evidence.diagnosis_to_action_provenance/v1"},
		EffectRefs:             nil,
		Transitions:            zeroEffectTransitions(),
		NegativeStatePolicyRef: PolicyAnalyticalRead,
		ScenarioRefs:           []string{"conformance.repair_plan.recommendation_only/v1"},
	}
}

func simulateRepairBinding() intent.Binding {
	return intent.Binding{
		Definition:     ref("hcmnext.operations.simulate_repair", 1),
		AggregateRoots: []string{"RepairPlan", "Observation"},
		ReadProperties: []string{
			"repair_plan.targets",
			"observation.observed_state",
		},
		DecisionRefs:           []string{"decision.repair_effect_preview/v1"},
		EvidenceRefs:           []string{"evidence.repair_input_output_and_warning_digest/v1"},
		EffectRefs:             nil,
		Transitions:            zeroEffectTransitions(),
		NegativeStatePolicyRef: PolicyPureCalculation,
		ScenarioRefs:           []string{"conformance.repair.no_business_side_effect/v1"},
	}
}
