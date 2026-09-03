package inspector_test

import (
	"time"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/operations/inspector"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// A small three-node plan: a CAPABILITY node feeding an END node, with a
// third, unreached node the trace has nothing for. It exercises exactly the
// shapes BuildWorkflowView projects: safe points, effect class, governance
// purpose, a terminal's lifecycle dimensions, and a node the trace never
// touched.
func fixturePlan() *workflow.CompiledWorkflow {
	dims := lifecycle.Dimensions{
		Request:     lifecycle.RequestSimulated,
		Execution:   lifecycle.ExecutionNotPlanned,
		Business:    lifecycle.BusinessNotStarted,
		Consistency: lifecycle.ConsistencyPendingObservation,
		Obligation:  lifecycle.ObligationNotApplicable,
	}

	return &workflow.CompiledWorkflow{
		WorkflowID:      "wf.promotion",
		Version:         3,
		Phase:           workflow.PhaseP1A,
		TerminalProfile: workflow.TerminalProfileSimulateOnly,
		StartNodeID:     "preflight",
		Nodes: []workflow.CompiledNode{
			{
				ID:           "preflight",
				Type:         workflow.StepCapability,
				Depth:        0,
				SafePoint:    true,
				EffectClass:  "READ_ONLY",
				Routes:       []string{"end"},
				AllowedModes: []workflow.ExecutionMode{"SIMULATE"},
				Governance: workflow.CompiledGovernance{
					Purpose:           "promotion.preflight",
					RequiredDecisions: []workflow.GovernanceKind{"AUTHZ"},
				},
			},
			{
				ID:          "unreached",
				Type:        workflow.StepObserve,
				Depth:       1,
				EffectClass: "READ_ONLY",
				Governance:  workflow.CompiledGovernance{Purpose: "promotion.observe"},
			},
			{
				ID:          "end",
				Type:        workflow.StepEnd,
				Depth:       1,
				EffectClass: "PURE",
				Governance:  workflow.CompiledGovernance{Purpose: "promotion.end"},
				Terminal: &workflow.Terminal{
					NodeID:        "end",
					TerminalCode:  "PROMOTION_SIMULATED",
					RuntimeStatus: workflow.RuntimeCompleted,
					Dimensions:    dims,
				},
			},
		},
		Edges: []workflow.Edge{
			{From: "preflight", To: "end", RouteKey: "SUCCEEDED"},
		},
		Effects: workflow.EffectSummary{
			ZeroEffect:   true,
			NodesByClass: map[string][]string{"READ_ONLY": {"preflight", "unreached"}, "PURE": {"end"}},
			AllowedModes: []workflow.ExecutionMode{"SIMULATE"},
		},
	}
}

func fixtureTrace() inspector.SimulationReceipt {
	observed := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return inspector.SimulationReceipt{
		WorkflowID: "wf.promotion",
		Version:    3,
		Nodes: map[string]inspector.NodeEvidence{
			"preflight": {
				NodeID:       "preflight",
				Status:       inspector.StatusSucceeded,
				Attempt:      1,
				EvidenceRefs: []string{"ev:preflight:1"},
				ObservedAt:   observed,
			},
			"end": {
				NodeID:     "end",
				Status:     inspector.StatusSucceeded,
				Attempt:    1,
				ObservedAt: observed,
			},
		},
		PendingWork: []inspector.HumanWorkItem{
			{NodeID: "approval-1", Kind: "APPROVAL", AssignedTo: "role:hr-partner", RequestedAt: observed},
		},
	}
}

// allowDecision authorizes both gated fields for a disclosable subject.
func allowDecision() *authz.Decision {
	return &authz.Decision{
		SubjectDisclosable: true,
		Fields: map[authz.FieldID]authz.FieldRuling{
			inspector.FieldExecutionTrace: {Effect: authz.EffectAllow, RuleID: "test.allow"},
			inspector.FieldHumanWork:      {Effect: authz.EffectAllow, RuleID: "test.allow"},
		},
	}
}

// denyTraceDecision authorizes human work but denies the execution trace.
func denyTraceDecision() *authz.Decision {
	return &authz.Decision{
		SubjectDisclosable: true,
		Fields: map[authz.FieldID]authz.FieldRuling{
			inspector.FieldExecutionTrace: {Effect: authz.EffectDenied, RuleID: "test.deny", Reason: "no grant"},
			inspector.FieldHumanWork:      {Effect: authz.EffectAllow, RuleID: "test.allow"},
		},
	}
}

// withheldDecision denies the subject outright.
func withheldDecision() *authz.Decision {
	return &authz.Decision{SubjectDisclosable: false, SubjectDenialReason: "no relationship"}
}
