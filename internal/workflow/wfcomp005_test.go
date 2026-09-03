package workflow_test

import (
	"reflect"
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

const fxAnalyze = "analyze_worker"

// withAgentAnalysis inserts an agent-eligible capability between the read and
// the payroll effect, so the effect's idempotency key is derived from agent
// output. validated decides whether a typed output validator stands between
// the agent and the effect.
func withAgentAnalysis(def workflow.Definition, validated bool) workflow.Definition {
	analyze := workflow.Node{
		ID:           fxAnalyze,
		Type:         workflow.StepCapability,
		InputSchema:  capSchema(capAgentAnalyze, "request"),
		OutputSchema: capSchema(capAgentAnalyze, "response"),
		Inputs:       []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
		Outputs:      []workflow.Field{{Path: "recommended_effect_key", Type: stringType()}},
		InputMappings: []workflow.Mapping{
			{Target: "worker_id", Source: nodeSource(fxRead, "worker_id")},
		},
		Capability: &workflow.CapabilityRef{
			ID: capAgentAnalyze, Version: 1,
			OperationMode:   workflow.ModeExecute,
			AuthorityScopes: []string{"scope:intelligence.read"},
		},
		Governance: governedNode(workflow.RevalidatePreExecution),
	}
	def.Nodes = append(def.Nodes, analyze)

	for i := range def.Nodes {
		if def.Nodes[i].ID != fxSync {
			continue
		}
		for j := range def.Nodes[i].InputMappings {
			if def.Nodes[i].InputMappings[j].Target == "effect_key" {
				def.Nodes[i].InputMappings[j].Source = nodeSource(fxAnalyze, "recommended_effect_key")
			}
		}
		if validated {
			def.Nodes[i].Governance.OutputValidatorRef = "validators.payroll.effect_key/v1"
		}
	}

	edges := def.Edges[:0:0]
	for _, e := range def.Edges {
		if e.From == fxRead && e.RouteKey == "SUCCEEDED" {
			e.To = fxAnalyze
		}
		edges = append(edges, e)
	}
	def.Edges = append(edges,
		workflow.Edge{From: fxAnalyze, To: fxSync, RouteKey: "SUCCEEDED"},
		workflow.Edge{From: fxAnalyze, To: fxEndRepair, RouteKey: "REJECTED"},
		workflow.Edge{From: fxAnalyze, To: fxEndRepair, RouteKey: "UNKNOWN"},
		workflow.Edge{From: fxAnalyze, To: fxEndRepair, RouteKey: "AMBIGUOUS"},
	)
	return def
}

// TestTodo_WF_COMP_005 proves planning/todos.md WF-COMP-005: a governed action
// with a missing AuthZ, legal, purpose or risk evaluation, an approval scope
// that cannot resolve, an obligation nothing inserts, or agent output routed
// into an effect without validation, all fail publication; a compiled plan
// declares its required contexts, decisions, obligations, approvals and
// revalidation boundaries.
func TestTodo_WF_COMP_005(t *testing.T) {
	t.Run("RED_missing_governance_evaluation", func(t *testing.T) {
		drop := func(kind workflow.GovernanceKind) func(*workflow.Definition) {
			return func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeSnapshotWorker)
				kept := n.Governance.RequiredDecisions[:0]
				for _, k := range n.Governance.RequiredDecisions {
					if k == kind {
						continue
					}
					kept = append(kept, k)
				}
				n.Governance.RequiredDecisions = kept
			}
		}
		cases := []struct {
			name   string
			mutate func(*workflow.Definition)
		}{
			{"authz", drop(workflow.GovernanceAuthZ)},
			{"legal", drop(workflow.GovernanceLegal)},
			{"purpose", drop(workflow.GovernancePurpose)},
			{"risk", drop(workflow.GovernanceRisk)},
			{"no_declared_purpose", func(def *workflow.Definition) {
				nodeRef(t, def, workflow.PromotionNodeSnapshotWorker).Governance.Purpose = ""
			}},
			{"no_data_access_manifest", func(def *workflow.Definition) {
				nodeRef(t, def, workflow.PromotionNodeObserveDrift).Governance.DataAccessManifestRef = ""
			}},
			{"undeclared_revalidation_boundary", func(def *workflow.Definition) {
				nodeRef(t, def, workflow.PromotionNodeObserveDrift).Governance.RevalidationBoundary = "SOMETIME"
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := workflow.PromotionReferenceDefinition()
				tc.mutate(&def)
				mustReject(t, def, promotionOptions(t), workflow.CodeMissingGovernanceEvaluation)
			})
		}
	})

	t.Run("RED_mutating_invocation_must_revalidate", func(t *testing.T) {
		def := effectsDefinition()
		nodeRef(t, &def, fxSync).Governance.RevalidationBoundary = workflow.RevalidateNone
		mustReject(t, def, effectsOptions(t), workflow.CodeMissingGovernanceEvaluation)
	})

	t.Run("RED_unresolved_approval_scope", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Definition)
		}{
			{"reference_to_an_undeclared_requirement", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeEndApproval)
				n.Governance.ApprovalRequirements = append(n.Governance.ApprovalRequirements, "approval.ghost")
			}},
			{"scope_outside_the_workflow_organization", func(def *workflow.Definition) {
				def.ApprovalRequirements[1].Scope = "globex/finance"
			}},
			{"no_resolver_expression", func(def *workflow.Definition) {
				def.ApprovalRequirements[0].ResolverExpression = ""
			}},
			{"no_quorum", func(def *workflow.Definition) {
				def.ApprovalRequirements[0].Quorum = 0
			}},
			{"no_effective_as_of_policy", func(def *workflow.Definition) {
				def.ApprovalRequirements[1].EffectiveAsOfPolicy = ""
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := workflow.PromotionReferenceDefinition()
				tc.mutate(&def)
				mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedApprovalScope)
			})
		}
	})

	t.Run("RED_unresolved_obligation", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Definition)
		}{
			{"reference_to_an_undeclared_obligation", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeSimulateComp)
				n.Governance.ObligationRefs = append(n.Governance.ObligationRefs, "obligation.ghost")
			}},
			{"declared_but_never_inserted", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeSimulateComp)
				n.Governance.ObligationRefs = nil
			}},
			{"no_reevaluation_policy", func(def *workflow.Definition) {
				def.Obligations[0].ReevaluationPolicy = ""
			}},
			{"no_satisfaction_condition", func(def *workflow.Definition) {
				def.Obligations[1].SatisfactionCondition = ""
			}},
			{"no_source_rule_version", func(def *workflow.Definition) {
				def.Obligations[2].SourceVersion = ""
			}},
			{"mandatory_closure_obligation_not_proved_at_a_terminal", func(def *workflow.Definition) {
				for i := range def.Nodes {
					if def.Nodes[i].Type != workflow.StepEnd {
						continue
					}
					refs := def.Nodes[i].Governance.ObligationRefs[:0]
					for _, ref := range def.Nodes[i].Governance.ObligationRefs {
						if ref == workflow.PromotionObligationEvidence {
							continue
						}
						refs = append(refs, ref)
					}
					def.Nodes[i].Governance.ObligationRefs = refs
				}
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := workflow.PromotionReferenceDefinition()
				tc.mutate(&def)
				mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedObligation)
			})
		}
	})

	t.Run("RED_agent_output_reaches_an_effect_without_validation", func(t *testing.T) {
		def := withAgentAnalysis(effectsDefinition(), false)
		mustReject(t, def, effectsOptions(t), workflow.CodeUnvalidatedAgentOutput)
	})

	t.Run("GREEN_typed_validator_admits_agent_output", func(t *testing.T) {
		def := withAgentAnalysis(effectsDefinition(), true)
		if _, err := workflow.Compile(def, effectsOptions(t)); err != nil {
			t.Fatalf("a validated agent path compiles: %v", err)
		}
	})

	t.Run("GREEN_plan_declares_the_governance_surface", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		def := workflow.PromotionReferenceDefinition()

		wantDecisions := []workflow.GovernanceKind{
			workflow.GovernanceAuthZ,
			workflow.GovernanceLegal,
			workflow.GovernancePurpose,
			workflow.GovernanceRisk,
		}
		if !reflect.DeepEqual(plan.Governance.RequiredDecisions, wantDecisions) {
			t.Fatalf("required decisions\n got %v\nwant %v", plan.Governance.RequiredDecisions, wantDecisions)
		}
		if len(plan.Governance.RequiredContexts) != 1 ||
			plan.Governance.RequiredContexts[0].Kind != "LegalContext" {
			t.Fatalf("required contexts\n got %v", plan.Governance.RequiredContexts)
		}
		if len(plan.Governance.ApprovalRequirements) != len(def.ApprovalRequirements) {
			t.Fatalf("compiled approvals\n got %v", plan.Governance.ApprovalRequirements)
		}
		wantInsertions := map[string][]string{
			string(workflow.InsertApproval):   {workflow.PromotionNodeEndApproval},
			string(workflow.InsertSimulation): {workflow.PromotionNodeSimulateComp},
			string(workflow.InsertClosure): {
				workflow.PromotionNodeEndDegraded,
				workflow.PromotionNodeEndRejected,
				workflow.PromotionNodeEndApproval,
				workflow.PromotionNodeEndSimulated,
				workflow.PromotionNodeEndUnknown,
			},
		}
		if !reflect.DeepEqual(plan.Governance.InsertionPoints, wantInsertions) {
			t.Fatalf("obligation insertion points\n got %v\nwant %v",
				plan.Governance.InsertionPoints, wantInsertions)
		}
		for _, id := range []string{
			workflow.PromotionNodeSnapshotWorker,
			workflow.PromotionNodeSimulateComp,
			workflow.PromotionNodeEvaluateBand,
			workflow.PromotionNodeObserveDrift,
		} {
			if plan.Governance.RevalidationPoints[id] != workflow.RevalidatePreExecution {
				t.Fatalf("node %s must revalidate before execution, got %q",
					id, plan.Governance.RevalidationPoints[id])
			}
		}
	})

	t.Run("REFACTOR_workflow_records_legal_results_without_interpreting_them", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		def := workflow.PromotionReferenceDefinition()

		byID := map[string]workflow.ObligationRequirement{}
		for _, o := range def.Obligations {
			byID[o.ID] = o
		}
		if len(plan.Governance.Obligations) != len(byID) {
			t.Fatalf("compiled obligations\n got %v", plan.Governance.Obligations)
		}
		for _, got := range plan.Governance.Obligations {
			want, ok := byID[got.ID]
			if !ok {
				t.Fatalf("compiler invented obligation %q", got.ID)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("obligation %q was reinterpreted\n got %+v\nwant %+v", got.ID, got, want)
			}
		}
	})
}

// TestTodo_WF_COMP_005_Security drives the paths where a governance gap would
// hand out authority nobody granted.
func TestTodo_WF_COMP_005_Security(t *testing.T) {
	t.Run("agent_eligible_capability_cannot_mutate_directly", func(t *testing.T) {
		registry := effectsRegistry(t)
		agentWriter := fixtureCapability("fixture.intelligence.apply_change", "intelligence",
			capability.EffectExternalMutation)
		agentWriter.AgentEligible = true
		if err := registry.Register(agentWriter, echoHandler); err != nil {
			t.Fatalf("publish %s: %v", agentWriter.Key(), err)
		}

		def := effectsDefinition()
		sync := nodeRef(t, &def, fxSync)
		sync.Capability.ID = agentWriter.ID
		sync.InputSchema = capSchema(agentWriter.ID, "request")
		sync.OutputSchema = capSchema(agentWriter.ID, "response")
		sync.Capability.AuthorityScopes = []string{"scope:intelligence.write"}
		sync.Governance.OutputValidatorRef = "validators.intelligence.output/v1"

		opts := workflow.Options{Phase: workflow.PhaseP1B, Capabilities: registry}
		mustReject(t, def, opts, workflow.CodeUnvalidatedAgentOutput)
	})

	t.Run("approval_cannot_escalate_outside_the_authorized_scope", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		def.ApprovalRequirements[1].Scope = "acme/engineering-adjacent"
		mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedApprovalScope)
	})

	t.Run("undeclared_context_read_fails", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		n := nodeRef(t, &def, workflow.PromotionNodeSnapshotWorker)
		n.InputMappings[0].Source = workflow.Source{
			Kind:        workflow.SourceContext,
			ContextKind: "PrincipalContext",
			Path:        "principal_id",
			Type:        brandedString("WorkerID"),
		}
		mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedRef)
	})

	t.Run("declared_context_read_outside_the_declared_field_paths_fails", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		n := nodeRef(t, &def, workflow.PromotionNodeSnapshotWorker)
		n.InputMappings[0].Source = workflow.Source{
			Kind:        workflow.SourceContext,
			ContextKind: "LegalContext",
			Path:        "unlisted_field",
			Type:        brandedString("WorkerID"),
		}
		mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedRef)
	})

	t.Run("node_authority_must_cover_the_capability_scope", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeSimulateComp).Capability.AuthorityScopes =
			[]string{"scope:people.read"}
		mustReject(t, def, promotionOptions(t), workflow.CodeUnauthorizedScope)
	})
}

// TestTodo_WF_COMP_005_Mutation kills one mutant per governance rule.
func TestTodo_WF_COMP_005_Mutation(t *testing.T) {
	runMutations(t, workflow.PromotionReferenceDefinition, promotionOptions, []mutationCase{
		{"drop_the_legal_evaluation", func(t *testing.T, def *workflow.Definition) {
			n := nodeRef(t, def, workflow.PromotionNodeEvaluateBand)
			n.Governance.RequiredDecisions = []workflow.GovernanceKind{
				workflow.GovernanceAuthZ, workflow.GovernancePurpose, workflow.GovernanceRisk,
			}
		}, workflow.CodeMissingGovernanceEvaluation},
		{"blank_the_data_access_manifest", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeSnapshotWorker).Governance.DataAccessManifestRef = ""
		}, workflow.CodeMissingGovernanceEvaluation},
		{"approval_scope_escapes_the_organization", func(t *testing.T, def *workflow.Definition) {
			def.ApprovalRequirements[0].Scope = "acme"
		}, workflow.CodeUnresolvedApprovalScope},
		{"blank_an_obligation_authority", func(t *testing.T, def *workflow.Definition) {
			def.Obligations[0].Authority = ""
		}, workflow.CodeUnresolvedObligation},
		{"undeclared_obligation_insertion_point", func(t *testing.T, def *workflow.Definition) {
			def.Obligations[2].InsertionPoint = "WHENEVER"
		}, workflow.CodeUnresolvedObligation},
		{"context_requirement_without_a_freshness_bound", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeSnapshotWorker).RequiredContext[0].MaxAgeSeconds = 0
		}, workflow.CodeStaleObservationAccepted},
		{"context_requirement_treats_missing_as_absent", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeSnapshotWorker).RequiredContext[0].MissingBehavior = "ABSENT"
		}, workflow.CodeInvalidDefinition},
	})
}
