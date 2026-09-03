package execute

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	"github.com/monstercameron/hcm-next/internal/workflow/version"
)

// ResumeRequest presents the completed human-work evidence and the typed
// step resolution that evidence produced. Start is context only: Resume does
// not create another instance. Its resolver and version store re-resolve the
// exact plan, while its proposal, tenant, correlation and subject fields are
// checked against WorkItem before the outcome may enter runtime.Advance.
//
// Outcome.OutputDigest is the digest of the typed step resolution, not
// necessarily WorkItem.CompletedOutputDigest. For an approval quorum, for
// example, the former binds the aggregate resolution while the latter binds
// one approver's decision.
type ResumeRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	WorkItem                workitem.WorkItem
	Outcome                 frontier.NodeOutcome
	Refs                    runtime.GovernanceRefs
	RecordedAt              time.Time
}

// Resume advances a WAITING APPROVAL or TASK from completed WorkItem
// evidence, then drains any ordinary READY successors exactly as Execute
// does. It never polls and it does not complete the WorkItem itself.
func (d *Driver) Resume(ctx context.Context, req ResumeRequest) (Result, error) {
	selection, outcome, refs, err := validateResume(ctx, req)
	if err != nil {
		return Result{}, err
	}
	run := runContext{start: req.Start, selection: selection, instanceID: req.InstanceID}
	at := req.RecordedAt.UTC()
	if req.RecordedAt.IsZero() {
		at = d.opts.Clock().UTC()
	}
	advanced, created, err := d.advanceOnce(ctx, run, req.ExpectedInstanceVersion, outcome, refs, at)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Advances:        []runtime.AdvanceReceipt{advanced},
		WorkItems:       created,
		InstanceVersion: advanced.NewInstanceVersion,
		Frontier:        append([]string(nil), advanced.Frontier...),
	}
	if advanced.Complete {
		result.Status = StatusComplete
		return result, nil
	}
	ready, parked := readyAndParked(advanced.Continuations)
	if parked {
		result.Status = StatusParked
		return result, nil
	}
	if len(ready) == 0 {
		return Result{}, fmt.Errorf("%w: resumed instance %s has no READY continuation", ErrNoProgress, req.InstanceID)
	}
	return d.drainReady(ctx, run, result, ready)
}

func validateResume(ctx context.Context, req ResumeRequest) (runtime.WorkflowSelection, frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if req.Start.Resolver == nil {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("resume has no WorkflowResolver")
	}
	if req.Start.Versions == nil {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("resume has no exact version Store")
	}
	if req.Start.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.ExpectedInstanceVersion < 1 {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("resume requires tenant, instance and positive expected instance version")
	}
	selection, err := req.Start.Resolver.ResolveWorkflow(ctx, req.Start)
	if err != nil {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("workflow execute: resolve resume workflow: %w", err)
	}
	if selection.Plan == nil || selection.WorkflowID == "" {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("WorkflowResolver returned no workflow id or plan for resume")
	}
	published, err := version.Resolve(req.Start.Versions, selection.WorkflowID, selection.Pin)
	if err != nil {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("workflow execute: resolve resume version: %w", err)
	}
	if published.Status != version.StatusActive || published.CompiledPlanDigest != selection.Plan.Digest() {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("resume plan is not the exact active published version")
	}

	item := req.WorkItem
	if err := item.Validate(); err != nil {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("workflow execute: invalid resume WorkItem: %w", err)
	}
	if item.Status != workitem.StatusCompleted {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("WorkItem %s is %s, not COMPLETED", item.WorkItemID, item.Status)
	}
	if item.TenantID != req.Start.TenantID || item.WorkflowInstanceID != req.InstanceID {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("completed WorkItem is bound to another tenant or instance")
	}
	if item.CorrelationID != req.Start.CorrelationID || item.ProposalRef != req.Start.Proposal.Revision.MaterialDigest.Digest {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("completed WorkItem is bound to another correlation or proposal")
	}
	if !sameStrings(item.SubjectRefs, req.Start.BusinessSubjectRefs) {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("completed WorkItem subject binding differs from the workflow")
	}
	node, ok := selection.Plan.Node(item.NodeID)
	if !ok || (node.Type != workflow.StepApproval && node.Type != workflow.StepTask) {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("completed WorkItem node %s is not an APPROVAL or TASK in the pinned plan", item.NodeID)
	}
	outcome := req.Outcome
	if outcome.NodeID == "" {
		outcome.NodeID = item.NodeID
	}
	if outcome.NodeID != item.NodeID || outcome.Await != frontier.AwaitNone || outcome.Failed || outcome.Outcome == "" || outcome.OutputDigest == "" {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("resume requires a completed typed outcome for WorkItem node %s", item.NodeID)
	}
	refs := req.Refs
	if refs.HumanTaskID != "" && refs.HumanTaskID != item.WorkItemID.String() {
		return runtime.WorkflowSelection{}, frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("typed outcome names another human task")
	}
	refs.HumanTaskID = item.WorkItemID.String()
	return selection, outcome, refs, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func readyAndParked(records []runtime.ContinuationRecord) ([]string, bool) {
	ready := []string{}
	parked := false
	for _, rec := range records {
		switch rec.Kind {
		case frontier.IntentReady:
			ready = append(ready, rec.TargetNodeID)
		case frontier.IntentWorkItemRequired:
			parked = true
		}
	}
	sort.Strings(ready)
	return ready, parked
}
