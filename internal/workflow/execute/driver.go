package execute

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// Status is the bounded driver's outcome.
type Status string

const (
	StatusParked   Status = "PARKED"
	StatusComplete Status = "COMPLETE"
)

// Options are the ports and explicit policies a Driver needs. MaxSteps bounds
// one synchronous call; zero uses a conservative plan-sized bound.
type Options struct {
	DB        Beginner
	Steps     StepRunner
	WorkItems WorkItemFactory
	Terminal  TerminalWriter
	Guard     idempotency.Store
	Retention idempotency.RetentionPolicy
	Clock     func() time.Time
	MaxSteps  int
	Advance   AdvanceFunc
}

// Driver synchronously runs the READY frontier of one newly started workflow.
type Driver struct {
	opts    Options
	advance AdvanceFunc
}

// New validates immutable driver wiring. StepRunner is required because every
// supported execution starts with READY work; the other ports are checked when
// their corresponding continuation is actually reached.
func New(opts Options) (*Driver, error) {
	if opts.DB == nil {
		return nil, invalid("database Beginner is required")
	}
	if opts.Steps == nil {
		return nil, invalid("StepRunner is required")
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Now().UTC() }
	}
	advance := opts.Advance
	if advance == nil {
		advance = runtime.Advance
	}
	return &Driver{opts: opts, advance: advance}, nil
}

// ExecuteRequest starts from an already-materialized immutable proposal. The
// embedded StartRequest carries the policy resolver, exact version store and
// all proposal-alignment assertions runtime.Start validates.
type ExecuteRequest struct {
	Start runtime.StartRequest
}

// Result is either COMPLETE or PARKED on the WorkItems returned here. Every
// receipt is from a committed transaction.
type Result struct {
	Status          Status
	Start           runtime.StartReceipt
	Advances        []runtime.AdvanceReceipt
	WorkItems       []workitem.WorkItem
	InstanceVersion int64
	Frontier        []string
}

// Execute resolves the workflow once, starts it atomically, then drains its
// READY continuations. It returns as soon as human work is durably created.
func (d *Driver) Execute(ctx context.Context, req ExecuteRequest) (Result, error) {
	if req.Start.Resolver == nil {
		return Result{}, invalid("StartRequest has no WorkflowResolver")
	}
	selection, err := req.Start.Resolver.ResolveWorkflow(ctx, req.Start)
	if err != nil {
		return Result{}, fmt.Errorf("workflow execute: resolve workflow: %w", err)
	}
	if selection.Plan == nil || selection.WorkflowID == "" {
		return Result{}, invalid("WorkflowResolver returned no workflow id or plan")
	}
	startReq := req.Start
	startReq.Resolver = fixedResolver{selection: selection}

	tx, err := d.opts.DB.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("workflow execute: begin start: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, startReq.TenantID); err != nil {
		return Result{}, err
	}
	started, err := runtime.Start(ctx, tx, startReq)
	if err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("workflow execute: commit start: %w", err)
	}

	result := Result{
		Start: started, InstanceVersion: started.InstanceVersion,
		Frontier: append([]string(nil), started.Frontier...),
	}
	ready := append([]string(nil), started.Frontier...)
	return d.drainReady(ctx, runContext{
		start: startReq, selection: selection, instanceID: started.InstanceID,
	}, result, ready)
}

type runContext struct {
	start      runtime.StartRequest
	selection  runtime.WorkflowSelection
	instanceID uuid.UUID
}

func (d *Driver) drainReady(ctx context.Context, run runContext, result Result, ready []string) (Result, error) {
	sort.Strings(ready)
	max := d.opts.MaxSteps
	if max <= 0 {
		max = 4*len(run.selection.Plan.Nodes) + 8
	}

	for steps := 0; len(ready) > 0; steps++ {
		if steps >= max {
			return Result{}, fmt.Errorf("%w: READY loop exceeded %d steps", ErrNoProgress, max)
		}
		nodeID := ready[0]
		ready = ready[1:]
		node, ok := run.selection.Plan.Node(nodeID)
		if !ok {
			return Result{}, invalid("frontier names node %s absent from resolved plan", nodeID)
		}
		at := d.opts.Clock().UTC()
		outcome, refs, err := d.opts.Steps.Run(ctx, StepRequest{
			TenantID: run.start.TenantID, InstanceID: run.instanceID,
			InstanceVersion: result.InstanceVersion, Attempt: 1,
			Node: node, Plan: run.selection.Plan, Proposal: run.start.Proposal,
			CorrelationID: run.start.CorrelationID, RecordedAt: at,
		})
		if err != nil {
			return Result{}, fmt.Errorf("workflow execute: run node %s: %w", nodeID, err)
		}
		if outcome.NodeID == "" {
			outcome.NodeID = nodeID
		} else if outcome.NodeID != nodeID {
			return Result{}, invalid("StepRunner returned outcome for %s while running %s", outcome.NodeID, nodeID)
		}

		advanced, created, err := d.advanceOnce(ctx, run, result.InstanceVersion, outcome, refs, at)
		if err != nil {
			return Result{}, err
		}
		result.Advances = append(result.Advances, advanced)
		result.WorkItems = append(result.WorkItems, created...)
		result.InstanceVersion = advanced.NewInstanceVersion
		result.Frontier = append([]string(nil), advanced.Frontier...)
		if advanced.Complete {
			result.Status = StatusComplete
			return result, nil
		}

		parked := false
		for _, rec := range advanced.Continuations {
			switch rec.Kind {
			case frontier.IntentReady:
				ready = append(ready, rec.TargetNodeID)
			case frontier.IntentWorkItemRequired:
				parked = true
			}
		}
		sort.Strings(ready)
		if parked {
			result.Status = StatusParked
			return result, nil
		}
	}

	return Result{}, fmt.Errorf("%w: instance %s is not terminal and has no READY continuation or WorkItem", ErrNoProgress, run.instanceID)
}

func (d *Driver) advanceOnce(
	ctx context.Context,
	run runContext,
	expectedVersion int64,
	outcome frontier.NodeOutcome,
	refs runtime.GovernanceRefs,
	at time.Time,
) (runtime.AdvanceReceipt, []workitem.WorkItem, error) {
	tx, err := d.opts.DB.Begin(ctx)
	if err != nil {
		return runtime.AdvanceReceipt{}, nil, fmt.Errorf("workflow execute: begin advance of %s: %w", outcome.NodeID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, run.start.TenantID); err != nil {
		return runtime.AdvanceReceipt{}, nil, err
	}
	sink := &continuationSink{
		tx: tx, durable: runtime.ContinuationStore{},
		factory: d.opts.WorkItems, terminal: d.opts.Terminal,
		guard: d.opts.Guard, policy: d.opts.Retention,
		workflowID: run.selection.WorkflowID, planDigest: run.selection.Plan.Digest(),
		proposal: run.start.Proposal, cellID: run.start.CellID,
		correlationID: run.start.CorrelationID,
		startKey:      run.start.StartIdempotencyKey,
		subjectRefs:   append([]string(nil), run.start.BusinessSubjectRefs...),
	}
	advanced, err := d.advance(ctx, tx, runtime.AdvanceRequest{
		TenantID: run.start.TenantID, InstanceID: run.instanceID,
		ExpectedInstanceVersion: expectedVersion, Attempt: 1,
		Plan: run.selection.Plan, Outcome: outcome, Refs: refs,
		RecordedAt: at, Sink: sink,
	})
	if err != nil {
		return runtime.AdvanceReceipt{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return runtime.AdvanceReceipt{}, nil, fmt.Errorf("workflow execute: commit advance of %s: %w", outcome.NodeID, err)
	}
	return advanced, append([]workitem.WorkItem(nil), sink.created...), nil
}

type fixedResolver struct{ selection runtime.WorkflowSelection }

func (r fixedResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return r.selection, nil
}

var _ runtime.WorkflowResolver = fixedResolver{}
