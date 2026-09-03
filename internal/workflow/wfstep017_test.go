package workflow_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// TestTodo_WF_STEP_017 proves planning/todos.md WF-STEP-017: a terminal
// records the instance's runtime status beside the intent's five lifecycle
// dimensions, never collapses an unresolved obligation or a degraded external
// state into a generic success, and can never write a sixth dimension.
func TestTodo_WF_STEP_017(t *testing.T) {
	t.Run("RED_sixth_dimension", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeEndSimulated).End.CompletionMapping["OutcomeState"] = "GOOD"
		d := mustReject(t, def, promotionOptions(t), workflow.CodeSixthDimension)
		if !d.HasAt(workflow.CodeSixthDimension, workflow.PromotionNodeEndSimulated) {
			t.Fatalf("the sixth dimension must be reported at its terminal, got %v", d.Errors)
		}
	})

	t.Run("RED_missing_dimension", func(t *testing.T) {
		for _, dim := range lifecycle.AllDimensions() {
			t.Run(string(dim), func(t *testing.T) {
				def := workflow.PromotionReferenceDefinition()
				delete(nodeRef(t, &def, workflow.PromotionNodeEndUnknown).End.CompletionMapping, string(dim))
				mustReject(t, def, promotionOptions(t), workflow.CodeMissingDimension)
			})
		}
	})

	t.Run("RED_state_id_is_not_declared", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeEndDegraded).End.CompletionMapping["BusinessState"] = "FINE"
		mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedRef)
	})

	t.Run("RED_illegal_tuple_under_the_fixed_kernel_rules", func(t *testing.T) {
		t.Run("committed_without_a_receipt", func(t *testing.T) {
			def := effectsDefinition()
			nodeRef(t, &def, fxEndCommit).End.CommitReceiptRef = ""
			mustReject(t, def, effectsOptions(t), workflow.CodeIllegalTerminalTuple)
		})
		t.Run("repair_required_without_a_repair_plan", func(t *testing.T) {
			def := effectsDefinition()
			nodeRef(t, &def, fxEndRepair).End.RepairRefs = nil
			mustReject(t, def, effectsOptions(t), workflow.CodeIllegalTerminalTuple)
		})
		t.Run("closed_with_an_open_obligation", func(t *testing.T) {
			def := effectsDefinition()
			end := nodeRef(t, &def, fxEndCommit).End
			end.CompletionMapping["RequestState"] = "CLOSED"
			end.CompletionMapping["ObligationState"] = "PENDING"
			mustReject(t, def, effectsOptions(t), workflow.CodeIllegalTerminalTuple)
		})
	})

	t.Run("RED_unresolved_obligation_collapsed_into_success", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeEndApproval).End.CompletionMapping["ObligationState"] = "SATISFIED"
		mustReject(t, def, promotionOptions(t), workflow.CodeObligationCollapsed)
	})

	t.Run("RED_degraded_state_collapsed_into_success", func(t *testing.T) {
		t.Run("terminal_reached_by_a_degraded_route", func(t *testing.T) {
			def := effectsDefinition()
			end := nodeRef(t, &def, fxEndDegraded).End
			end.CompletionMapping["ConsistencyState"] = "CONSISTENT"
			end.RepairRefs = nil
			mustReject(t, def, effectsOptions(t), workflow.CodeDegradedCollapsedToSuccess)
		})
		t.Run("runtime_completed_while_repair_is_required", func(t *testing.T) {
			def := effectsDefinition()
			nodeRef(t, &def, fxEndRepair).End.RuntimeStatus = workflow.RuntimeCompleted
			mustReject(t, def, effectsOptions(t), workflow.CodeDegradedCollapsedToSuccess)
		})
	})

	t.Run("RED_terminal_outside_the_declared_profile", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		end := nodeRef(t, &def, workflow.PromotionNodeEndSimulated).End
		end.CompletionMapping["ExecutionState"] = "COMMITTED"
		end.CommitReceiptRef = "receipt.simulated/v1"
		mustReject(t, def, promotionOptions(t), workflow.CodeTerminalNotInProfile)
	})

	t.Run("GREEN_terminal_records_runtime_status_and_five_dimensions", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		if len(plan.Terminals) != 5 {
			t.Fatalf("expected five terminals, got %d", len(plan.Terminals))
		}
		byNode := map[string]workflow.Terminal{}
		for _, term := range plan.Terminals {
			byNode[term.NodeID] = term
		}

		simulated, ok := byNode[workflow.PromotionNodeEndSimulated]
		if !ok {
			t.Fatal("plan has no simulated-complete terminal")
		}
		if simulated.RuntimeStatus != workflow.RuntimeCompleted {
			t.Fatalf("runtime status %q", simulated.RuntimeStatus)
		}
		wantSimulated := lifecycle.Dimensions{
			Request:     lifecycle.RequestSimulated,
			Execution:   lifecycle.ExecutionNotPlanned,
			Business:    lifecycle.BusinessNotStarted,
			Consistency: lifecycle.ConsistencyNotApplicable,
			Obligation:  lifecycle.ObligationSatisfied,
		}
		if simulated.Dimensions != wantSimulated {
			t.Fatalf("dimensions\n got %s\nwant %s", simulated.Dimensions, wantSimulated)
		}

		approval := byNode[workflow.PromotionNodeEndApproval]
		if approval.Dimensions.Obligation != lifecycle.ObligationPending {
			t.Fatalf("a terminal with an outstanding approval obligation reports PENDING, got %s",
				approval.Dimensions.Obligation)
		}
		if len(approval.OutstandingObligationRefs) == 0 {
			t.Fatal("the approval terminal names the obligation it leaves open")
		}

		degraded := byNode[workflow.PromotionNodeEndDegraded]
		if degraded.RuntimeStatus != workflow.RuntimeBlocked {
			t.Fatalf("the degraded terminal reports runtime status %q", degraded.RuntimeStatus)
		}
		if degraded.Dimensions.Consistency != lifecycle.ConsistencyUnknown {
			t.Fatalf("the degraded terminal reports consistency %s", degraded.Dimensions.Consistency)
		}

		for _, term := range plan.Terminals {
			if err := term.Dimensions.Validate(); err != nil {
				t.Fatalf("terminal %s: %v", term.NodeID, err)
			}
			if term.TerminalCode == "" {
				t.Fatalf("terminal %s has no terminal code", term.NodeID)
			}
		}
	})

	t.Run("GREEN_execute_profile_reports_committed_but_degraded_honestly", func(t *testing.T) {
		plan, err := workflow.Compile(effectsDefinition(), effectsOptions(t))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		var degraded workflow.Terminal
		for _, term := range plan.Terminals {
			if term.NodeID == fxEndDegraded {
				degraded = term
			}
		}
		if degraded.NodeID == "" {
			t.Fatal("plan has no degraded terminal")
		}
		// The whole point of five dimensions: the business action committed and
		// completed while external consistency is degraded with a linked repair.
		if degraded.Dimensions.Execution != lifecycle.ExecutionCommitted ||
			degraded.Dimensions.Business != lifecycle.BusinessCompleted ||
			degraded.Dimensions.Consistency != lifecycle.ConsistencyDegraded {
			t.Fatalf("degraded terminal dimensions: %s", degraded.Dimensions)
		}
		if len(degraded.RepairRefs) == 0 {
			t.Fatal("a degraded terminal links its repair plan")
		}
	})

	t.Run("REFACTOR_profiles_select_from_fixed_kernel_rules", func(t *testing.T) {
		profiles := workflow.TerminalProfiles()
		if len(profiles) != 2 {
			t.Fatalf("terminal profiles are a closed set, got %v", profiles)
		}
		for _, p := range profiles {
			if !p.Valid() {
				t.Fatalf("profile %q is listed but not valid", p)
			}
		}
		if workflow.TerminalProfile("ANYTHING_GOES").Valid() {
			t.Fatal("a definition cannot invent a terminal profile")
		}

		// A profile may only restrict what the kernel rules already allow. An
		// illegal tuple stays illegal under the most permissive profile.
		def := effectsDefinition()
		def.TerminalProfile = workflow.TerminalProfileExecute
		nodeRef(t, &def, fxEndCommit).End.CommitReceiptRef = ""
		mustReject(t, def, effectsOptions(t), workflow.CodeIllegalTerminalTuple)

		unknownProfile := workflow.PromotionReferenceDefinition()
		unknownProfile.TerminalProfile = "ANYTHING_GOES"
		mustReject(t, unknownProfile, promotionOptions(t), workflow.CodeInvalidDefinition)
	})
}

// TestTodo_WF_STEP_017_Mutation kills one mutant per terminal rule.
func TestTodo_WF_STEP_017_Mutation(t *testing.T) {
	runMutations(t, workflow.PromotionReferenceDefinition, promotionOptions, []mutationCase{
		{"add_a_sixth_dimension", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeEndRejected).End.CompletionMapping["OutcomeState"] = "DONE"
		}, workflow.CodeSixthDimension},
		{"drop_the_consistency_dimension", func(t *testing.T, def *workflow.Definition) {
			delete(nodeRef(t, def, workflow.PromotionNodeEndDegraded).End.CompletionMapping, "ConsistencyState")
		}, workflow.CodeMissingDimension},
		{"collapse_an_outstanding_obligation", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeEndApproval).End.CompletionMapping["ObligationState"] = "WAIVED"
		}, workflow.CodeObligationCollapsed},
		{"claim_a_committed_execution_in_a_simulation", func(t *testing.T, def *workflow.Definition) {
			end := nodeRef(t, def, workflow.PromotionNodeEndUnknown).End
			end.CompletionMapping["ExecutionState"] = "COMMITTED"
			end.CommitReceiptRef = "receipt.fake/v1"
		}, workflow.CodeTerminalNotInProfile},
		{"blank_the_terminal_code", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeEndRejected).End.TerminalCode = ""
		}, workflow.CodeInvalidDefinition},
		{"undeclared_runtime_status", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeEndSimulated).End.RuntimeStatus = "FINISHED"
		}, workflow.CodeInvalidDefinition},
		{"drop_the_end_specification", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeEndUnknown).End = nil
		}, workflow.CodeInvalidDefinition},
	})
}
