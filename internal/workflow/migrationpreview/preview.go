// Package migrationpreview classifies live workflow instances against a new
// compiled workflow version without changing either version or any instance.
//
// A published workflow remains immutable. A preview therefore compares the
// exact compiled node at an instance's current step and only follows a
// different step when the caller supplies an explicit, named bridge.
package migrationpreview

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Outcome is the compatibility classification for one live instance.
type Outcome string

const (
	// OutcomeSafe means the current step has a direct equivalent in the target
	// plan and its compiled semantics are unchanged.
	OutcomeSafe Outcome = "SAFE"
	// OutcomeTransformable means an explicit bridge maps the current step to a
	// target step, or the step's compiled contract changed under a bridge.
	OutcomeTransformable Outcome = "TRANSFORMABLE"
	// OutcomeRequiresRepair means the current step still exists but its
	// semantics changed without a deterministic bridge.
	OutcomeRequiresRepair Outcome = "REQUIRES_REPAIR"
	// OutcomeImpossible means the current state has no legal target equivalent.
	OutcomeImpossible Outcome = "IMPOSSIBLE"
)

// LiveInstanceState is the in-memory state needed by a preview. Stage is an
// opaque persisted stage label: this package does not infer business meaning
// from it or read a clock, database, or runtime scheduler.
type LiveInstanceState struct {
	InstanceID string `json:"instance_id"`
	StepID     string `json:"step_id"`
	Stage      string `json:"stage"`
}

func (s LiveInstanceState) validate() error {
	switch {
	case s.InstanceID == "":
		return fmt.Errorf("instance id is required")
	case s.StepID == "":
		return fmt.Errorf("step id is required")
	case s.Stage == "":
		return fmt.Errorf("stage is required")
	default:
		return nil
	}
}

// Bridge is an explicit state mapping from a source step to a target step.
// FromStage must match a live instance exactly. ToStage is the stage the
// bridged instance will carry after the migration. Ref is the reviewed
// transform/repair identity recorded with the migration plan.
type Bridge struct {
	FromStepID string `json:"from_step_id"`
	FromStage  string `json:"from_stage"`
	ToStepID   string `json:"to_step_id"`
	ToStage    string `json:"to_stage"`
	Ref        string `json:"ref"`
}

func (b Bridge) validate() error {
	switch {
	case b.FromStepID == "":
		return fmt.Errorf("bridge source step id is required")
	case b.FromStage == "":
		return fmt.Errorf("bridge source stage is required")
	case b.ToStepID == "":
		return fmt.Errorf("bridge target step id is required")
	case b.ToStage == "":
		return fmt.Errorf("bridge target stage is required")
	case b.Ref == "":
		return fmt.Errorf("bridge ref is required")
	default:
		return nil
	}
}

// Request supplies the immutable compiled source and target plans and the
// current in-memory instance states to classify.
type Request struct {
	Source    *workflow.CompiledWorkflow
	Target    *workflow.CompiledWorkflow
	Instances []LiveInstanceState
	Bridges   []Bridge
}

// ErrInvalidRequest reports a malformed preview request or an invalid
// compiled plan. It is typed so callers can refuse the preview before acting
// on a partial result.
type ErrInvalidRequest struct{ Detail string }

func (e *ErrInvalidRequest) Error() string { return "migration preview: invalid request: " + e.Detail }

// ErrRemovedStepWithoutBridge is the typed refusal required when a live step
// disappears from the target plan without a declared bridge.
type ErrRemovedStepWithoutBridge struct {
	InstanceID string
	StepID     string
}

func (e *ErrRemovedStepWithoutBridge) Error() string {
	return fmt.Sprintf("migration preview: live step %q for instance %q was removed without a bridge", e.StepID, e.InstanceID)
}

// Blocker is an exact reason an instance is not directly safe to continue.
type Blocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Assessment is the deterministic classification of one live instance.
type Assessment struct {
	InstanceID string             `json:"instance_id"`
	Source     LiveInstanceState  `json:"source"`
	Outcome    Outcome            `json:"outcome"`
	Target     *LiveInstanceState `json:"target,omitempty"`
	Bridge     *Bridge            `json:"bridge,omitempty"`
	Blockers   []Blocker          `json:"blockers,omitempty"`
}

// Result is the complete preview, with convenient category slices and the
// per-instance evidence from which they were derived. All slices are sorted
// by instance id for stable golden output.
type Result struct {
	SourceWorkflowID string              `json:"source_workflow_id"`
	SourceVersion    uint32              `json:"source_version"`
	TargetWorkflowID string              `json:"target_workflow_id"`
	TargetVersion    uint32              `json:"target_version"`
	Assessments      []Assessment        `json:"assessments"`
	Continue         []LiveInstanceState `json:"continue,omitempty"`
	NeedsBridge      []LiveInstanceState `json:"needs_bridge,omitempty"`
	RequiresRepair   []LiveInstanceState `json:"requires_repair,omitempty"`
	Stranded         []LiveInstanceState `json:"stranded,omitempty"`
}

// Preview computes migration compatibility. It is pure: the plans and input
// states are never mutated, and no storage or runtime service is consulted.
// A removed live step without a matching bridge returns both the partial
// classifications and [ErrRemovedStepWithoutBridge], allowing an operator to
// retain exact evidence while refusing the unsafe migration.
func Preview(req Request) (Result, error) {
	if err := validateRequest(req); err != nil {
		return Result{}, &ErrInvalidRequest{Detail: err.Error()}
	}

	sourceNodes := nodesByID(req.Source)
	targetNodes := nodesByID(req.Target)
	bridges := append([]Bridge(nil), req.Bridges...)
	sort.Slice(bridges, func(i, j int) bool {
		if bridges[i].FromStepID != bridges[j].FromStepID {
			return bridges[i].FromStepID < bridges[j].FromStepID
		}
		return bridges[i].FromStage < bridges[j].FromStage
	})

	result := Result{
		SourceWorkflowID: req.Source.WorkflowID,
		SourceVersion:    req.Source.Version,
		TargetWorkflowID: req.Target.WorkflowID,
		TargetVersion:    req.Target.Version,
	}
	for _, live := range req.Instances {
		sourceNode := sourceNodes[live.StepID]
		targetNode, targetExists := targetNodes[live.StepID]
		if !targetExists {
			bridge, ok := matchingBridge(bridges, live)
			if !ok {
				assessment := Assessment{
					InstanceID: live.InstanceID,
					Source:     live,
					Outcome:    OutcomeImpossible,
					Blockers: []Blocker{{
						Code:   "LIVE_STEP_REMOVED",
						Detail: "the current step is absent from the target and has no declared bridge",
					}},
				}
				result.Assessments = append(result.Assessments, assessment)
				result.Stranded = append(result.Stranded, live)
				continue
			}
			result.Assessments = append(result.Assessments, bridgedAssessment(live, bridge, nil))
			result.NeedsBridge = append(result.NeedsBridge, live)
			continue
		}

		if reflect.DeepEqual(sourceNode, targetNode) {
			result.Assessments = append(result.Assessments, Assessment{
				InstanceID: live.InstanceID,
				Source:     live,
				Outcome:    OutcomeSafe,
				Target:     &LiveInstanceState{InstanceID: live.InstanceID, StepID: live.StepID, Stage: live.Stage},
			})
			result.Continue = append(result.Continue, live)
			continue
		}

		bridge, hasBridge := matchingBridge(bridges, live)
		if hasBridge {
			result.Assessments = append(result.Assessments, bridgedAssessment(live, bridge, &targetNode))
			result.NeedsBridge = append(result.NeedsBridge, live)
			continue
		}

		result.Assessments = append(result.Assessments, Assessment{
			InstanceID: live.InstanceID,
			Source:     live,
			Outcome:    OutcomeRequiresRepair,
			Blockers: []Blocker{{
				Code:   "COMPILED_STEP_CHANGED",
				Detail: "the current step exists in the target but its compiled contract changed without a bridge",
			}},
		})
		result.RequiresRepair = append(result.RequiresRepair, live)
	}
	sortResult(&result)
	if len(result.Stranded) > 0 {
		first := result.Stranded[0]
		return result, &ErrRemovedStepWithoutBridge{InstanceID: first.InstanceID, StepID: first.StepID}
	}
	return result, nil
}

func validateRequest(req Request) error {
	if req.Source == nil || req.Target == nil {
		return fmt.Errorf("source and target plans are required")
	}
	if err := req.Source.Verify(); err != nil {
		return fmt.Errorf("source plan: %w", err)
	}
	if err := req.Target.Verify(); err != nil {
		return fmt.Errorf("target plan: %w", err)
	}
	if req.Source.WorkflowID != req.Target.WorkflowID {
		return fmt.Errorf("source workflow %q and target workflow %q differ", req.Source.WorkflowID, req.Target.WorkflowID)
	}
	if req.Target.Version <= req.Source.Version {
		return fmt.Errorf("target version %d must be greater than source version %d", req.Target.Version, req.Source.Version)
	}
	seen := map[string]bool{}
	sourceNodes := nodesByID(req.Source)
	for _, live := range req.Instances {
		if err := live.validate(); err != nil {
			return fmt.Errorf("instance %q: %w", live.InstanceID, err)
		}
		if seen[live.InstanceID] {
			return fmt.Errorf("instance %q is repeated", live.InstanceID)
		}
		if _, ok := sourceNodes[live.StepID]; !ok {
			return fmt.Errorf("instance %q names absent source step %q", live.InstanceID, live.StepID)
		}
		seen[live.InstanceID] = true
	}
	targetNodes := nodesByID(req.Target)
	seenBridges := map[string]bool{}
	for _, bridge := range req.Bridges {
		if err := bridge.validate(); err != nil {
			return err
		}
		if _, ok := targetNodes[bridge.ToStepID]; !ok {
			return fmt.Errorf("bridge %q targets absent step %q", bridge.Ref, bridge.ToStepID)
		}
		key := bridge.FromStepID + "\x00" + bridge.FromStage
		if seenBridges[key] {
			return fmt.Errorf("bridge for %s at stage %s is repeated", bridge.FromStepID, bridge.FromStage)
		}
		seenBridges[key] = true
	}
	return nil
}

func nodesByID(plan *workflow.CompiledWorkflow) map[string]workflow.CompiledNode {
	out := make(map[string]workflow.CompiledNode, len(plan.Nodes))
	for _, node := range plan.Nodes {
		out[node.ID] = node
	}
	return out
}

func matchingBridge(bridges []Bridge, live LiveInstanceState) (*Bridge, bool) {
	for i := range bridges {
		if bridges[i].FromStepID == live.StepID && bridges[i].FromStage == live.Stage {
			bridge := bridges[i]
			return &bridge, true
		}
	}
	return nil, false
}

func bridgedAssessment(live LiveInstanceState, bridge *Bridge, target *workflow.CompiledNode) Assessment {
	targetStep := bridge.ToStepID
	if target != nil {
		targetStep = target.ID
	}
	return Assessment{
		InstanceID: live.InstanceID,
		Source:     live,
		Outcome:    OutcomeTransformable,
		Target:     &LiveInstanceState{InstanceID: live.InstanceID, StepID: targetStep, Stage: bridge.ToStage},
		Bridge:     bridge,
		Blockers: []Blocker{{
			Code:   "DECLARED_BRIDGE",
			Detail: "the instance continues only through the named migration bridge " + bridge.Ref,
		}},
	}
}

func sortResult(result *Result) {
	sort.Slice(result.Assessments, func(i, j int) bool { return result.Assessments[i].InstanceID < result.Assessments[j].InstanceID })
	sort.Slice(result.Continue, func(i, j int) bool { return result.Continue[i].InstanceID < result.Continue[j].InstanceID })
	sort.Slice(result.NeedsBridge, func(i, j int) bool { return result.NeedsBridge[i].InstanceID < result.NeedsBridge[j].InstanceID })
	sort.Slice(result.RequiresRepair, func(i, j int) bool { return result.RequiresRepair[i].InstanceID < result.RequiresRepair[j].InstanceID })
	sort.Slice(result.Stranded, func(i, j int) bool { return result.Stranded[i].InstanceID < result.Stranded[j].InstanceID })
}
