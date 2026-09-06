package migrationpreview

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

func promotionPlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	plan, err := workflow.CompilePromotionReference(registry)
	if err != nil {
		t.Fatalf("compile Promotion fixture: %v", err)
	}
	return plan
}

func promotionPlanWithoutBuildProposal(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	def.Version++
	nodes := make([]workflow.Node, 0, len(def.Nodes)-1)
	for _, node := range def.Nodes {
		if node.ID == workflow.PromotionNodeBuildProposal {
			continue
		}
		nodes = append(nodes, node)
	}
	def.Nodes = nodes
	var edges []workflow.Edge
	for _, edge := range def.Edges {
		if edge.From == workflow.PromotionNodeBuildProposal {
			continue
		}
		if edge.To == workflow.PromotionNodeBuildProposal {
			edge.To = workflow.PromotionNodeRaiseThreshold
		}
		edges = append(edges, edge)
	}
	def.Edges = edges
	for i := range def.Nodes {
		if def.Nodes[i].ID != workflow.PromotionNodeRaiseThreshold {
			continue
		}
		for j := range def.Nodes[i].InputMappings {
			switch def.Nodes[i].InputMappings[j].Target {
			case "raise_ratio":
				def.Nodes[i].InputMappings[j].Source.NodeID = workflow.PromotionNodeSimulateComp
			case "band_position":
				def.Nodes[i].InputMappings[j].Source.NodeID = workflow.PromotionNodeEvaluateBand
			}
		}
	}
	for i := range def.Nodes {
		if def.Nodes[i].ID != workflow.PromotionNodeEndApproval {
			continue
		}
		for j := range def.Nodes[i].InputMappings {
			if def.Nodes[i].InputMappings[j].Target == "proposal_digest" {
				def.Nodes[i].InputMappings[j].Source = workflow.Source{
					Kind:     workflow.SourceConstant,
					Constant: "migrated-proposal",
					Type:     workflow.ValueType{Kind: workflow.KindString},
				}
			}
		}
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry})
	if err != nil {
		t.Fatalf("compile modified Promotion fixture: %v", err)
	}
	return plan
}

func promotionPlanWithChangedEvaluate(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	def.Version++
	for i := range def.Nodes {
		if def.Nodes[i].ID == workflow.PromotionNodeEvaluateBand {
			def.Nodes[i].Governance.Purpose = "SIMULATE_MANAGEMENT_PROMOTION_REVISED"
		}
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry})
	if err != nil {
		t.Fatalf("compile changed Promotion fixture: %v", err)
	}
	return plan
}

func bridgeForBuildProposal() Bridge {
	return Bridge{
		FromStepID: workflow.PromotionNodeBuildProposal,
		FromStage:  "RUNNING",
		ToStepID:   workflow.PromotionNodeRaiseThreshold,
		ToStage:    "READY",
		Ref:        "migration.promotion.build-proposal-to-threshold/v1",
	}
}

func TestTodo_WF_RUN_017(t *testing.T) {
	source := promotionPlan(t)
	target := promotionPlanWithoutBuildProposal(t)
	instances := []LiveInstanceState{
		{InstanceID: "instance-safe", StepID: workflow.PromotionNodeSnapshotWorker, Stage: "WAITING"},
		{InstanceID: "instance-bridge", StepID: workflow.PromotionNodeBuildProposal, Stage: "RUNNING"},
	}
	result, err := Preview(Request{Source: source, Target: target, Instances: instances, Bridges: []Bridge{bridgeForBuildProposal()}})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(result.Continue) != 1 || result.Continue[0].InstanceID != "instance-safe" {
		t.Fatalf("continue = %+v, want the unchanged live step", result.Continue)
	}
	if len(result.NeedsBridge) != 1 || result.NeedsBridge[0].InstanceID != "instance-bridge" {
		t.Fatalf("needs bridge = %+v, want the removed live step", result.NeedsBridge)
	}
	if got := result.Assessments[0].Outcome; got != OutcomeTransformable {
		t.Fatalf("bridged outcome = %q, want %q", got, OutcomeTransformable)
	}
}

func TestTodo_WF_RUN_017_Golden(t *testing.T) {
	result, err := Preview(Request{
		Source:    promotionPlan(t),
		Target:    promotionPlanWithoutBuildProposal(t),
		Instances: []LiveInstanceState{{InstanceID: "instance-safe", StepID: workflow.PromotionNodeSnapshotWorker, Stage: "WAITING"}, {InstanceID: "instance-bridge", StepID: workflow.PromotionNodeBuildProposal, Stage: "RUNNING"}},
		Bridges:   []Bridge{bridgeForBuildProposal()},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "wfrun017_promotion_preview.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal preview: %v", err)
	}
	if string(got)+"\n" != string(want) {
		t.Fatalf("preview golden mismatch\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestTodo_WF_RUN_017_Race(t *testing.T) {
	source := promotionPlan(t)
	target := promotionPlanWithoutBuildProposal(t)
	instances := []LiveInstanceState{{InstanceID: "instance-safe", StepID: workflow.PromotionNodeSnapshotWorker, Stage: "WAITING"}, {InstanceID: "instance-bridge", StepID: workflow.PromotionNodeBuildProposal, Stage: "RUNNING"}}
	request := Request{Source: source, Target: target, Instances: instances, Bridges: []Bridge{bridgeForBuildProposal()}}
	before := append([]LiveInstanceState(nil), request.Instances...)
	const workers = 24
	results := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := Preview(request)
			errs[i] = err
			if err == nil {
				data, marshalErr := json.Marshal(result)
				errs[i] = marshalErr
				results[i] = string(data)
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("preview %d: %v", i, err)
		}
		if results[i] != results[0] {
			t.Fatalf("preview %d differs from preview 0", i)
		}
	}
	if !reflect.DeepEqual(request.Instances, before) {
		t.Fatalf("Preview mutated live instance input: before=%+v after=%+v", before, request.Instances)
	}
}

func TestTodo_WF_RUN_017_Fault(t *testing.T) {
	t.Run("removed live step without bridge is refused", func(t *testing.T) {
		result, err := Preview(Request{
			Source:    promotionPlan(t),
			Target:    promotionPlanWithoutBuildProposal(t),
			Instances: []LiveInstanceState{{InstanceID: "instance-stranded", StepID: workflow.PromotionNodeBuildProposal, Stage: "RUNNING"}},
		})
		if result.Assessments[0].Outcome != OutcomeImpossible {
			t.Fatalf("stranded outcome = %q, want %q", result.Assessments[0].Outcome, OutcomeImpossible)
		}
		var refusal *ErrRemovedStepWithoutBridge
		if !errors.As(err, &refusal) {
			t.Fatalf("error = %v, want ErrRemovedStepWithoutBridge", err)
		}
		if refusal.InstanceID != "instance-stranded" || refusal.StepID != workflow.PromotionNodeBuildProposal {
			t.Fatalf("refusal = %+v", refusal)
		}
	})
	t.Run("malformed request is refused", func(t *testing.T) {
		_, err := Preview(Request{})
		var invalid *ErrInvalidRequest
		if !errors.As(err, &invalid) {
			t.Fatalf("error = %v, want ErrInvalidRequest", err)
		}
	})
}

func TestTodo_WF_RUN_017_Security(t *testing.T) {
	_, err := Preview(Request{
		Source:    promotionPlan(t),
		Target:    promotionPlanWithoutBuildProposal(t),
		Instances: []LiveInstanceState{{InstanceID: "instance-wrong-stage", StepID: workflow.PromotionNodeBuildProposal, Stage: "WAITING"}},
		Bridges:   []Bridge{bridgeForBuildProposal()},
	})
	var refusal *ErrRemovedStepWithoutBridge
	if !errors.As(err, &refusal) {
		t.Fatalf("wrong-stage bridge error = %v, want typed refusal", err)
	}
	if refusal.InstanceID != "instance-wrong-stage" {
		t.Fatalf("wrong-stage refusal = %+v", refusal)
	}
}

func TestTodo_WF_RUN_017_Conformance(t *testing.T) {
	if OutcomeSafe != "SAFE" || OutcomeTransformable != "TRANSFORMABLE" || OutcomeRequiresRepair != "REQUIRES_REPAIR" || OutcomeImpossible != "IMPOSSIBLE" {
		t.Fatalf("outcome vocabulary drifted: %q %q %q %q", OutcomeSafe, OutcomeTransformable, OutcomeRequiresRepair, OutcomeImpossible)
	}
	result, err := Preview(Request{
		Source:    promotionPlan(t),
		Target:    promotionPlanWithoutBuildProposal(t),
		Instances: []LiveInstanceState{{InstanceID: "z", StepID: workflow.PromotionNodeSnapshotWorker, Stage: "READY"}, {InstanceID: "a", StepID: workflow.PromotionNodeSnapshotWorker, Stage: "READY"}},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if result.Assessments[0].InstanceID != "a" || result.Assessments[1].InstanceID != "z" {
		t.Fatalf("assessments are not stable-sorted: %+v", result.Assessments)
	}
}

func TestTodo_WF_RUN_017_Mutation(t *testing.T) {
	result, err := Preview(Request{
		Source:    promotionPlan(t),
		Target:    promotionPlanWithChangedEvaluate(t),
		Instances: []LiveInstanceState{{InstanceID: "instance-changed", StepID: workflow.PromotionNodeEvaluateBand, Stage: "RUNNING"}},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(result.RequiresRepair) != 1 || result.Assessments[0].Outcome != OutcomeRequiresRepair {
		t.Fatalf("changed-step result = %+v, want REQUIRES_REPAIR", result)
	}
}
