package execute

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	// Instrumentation is OBS-023's span/log port. Nil means
	// [NoopInstrumentation]: no spans, no log lines, every composition that
	// predates OBS-023 keeps running unchanged.
	Instrumentation Instrumentation
	// Evidence is OBS-024's execution-evidence port. Nil means
	// [NoopExecutionEvidence].
	Evidence ExecutionEvidence
	// Items loads the durable WorkItem a Resume advances from. Required only
	// once a caller actually calls Resume (WF-RUN-028); Execute never reads
	// it.
	Items WorkItemReader
	// Currency revalidates the pinned proposal, its approval and its control
	// snapshots before every advancement this driver attempts, including the
	// one that reaches a terminal write (WF-RUN-029). Nil runs no currency
	// check at all, exactly reproducing this driver's pre-WF-RUN-029
	// behavior.
	Currency *CurrencyGuard
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
	if opts.Instrumentation == nil {
		opts.Instrumentation = NoopInstrumentation{}
	}
	if opts.Evidence == nil {
		opts.Evidence = NoopExecutionEvidence{}
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
	// EvidenceIDs are the OBS-024 execution-evidence ids recorded while
	// producing this result (APPROVAL_COMPLETED/TASK_SUBMITTED on a
	// [Driver.Resume], TERMINAL_WRITTEN on any call that reaches COMPLETE),
	// in recording order.
	EvidenceIDs []string
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
		traceID: d.opts.Instrumentation.TraceID(ctx),
	}, result, ready)
}

type runContext struct {
	start      runtime.StartRequest
	selection  runtime.WorkflowSelection
	instanceID uuid.UUID
	// traceID is the ambient trace id read off the call's own incoming span
	// context once, at Execute/Resume entry (OBS-023), and threaded onto
	// every StepRequest and runtime.AdvanceRequest this run produces.
	traceID string
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
		nodeCtx, nodeSpan := d.opts.Instrumentation.StartNodeSpan(ctx, SpanAttributes{
			InstanceID: run.instanceID.String(), NodeID: nodeID, Attempt: 1,
		})
		outcome, refs, err := d.opts.Steps.Run(nodeCtx, StepRequest{
			TenantID: run.start.TenantID, InstanceID: run.instanceID,
			InstanceVersion: result.InstanceVersion, Attempt: 1,
			Node: node, Plan: run.selection.Plan, Proposal: run.start.Proposal,
			CorrelationID: run.start.CorrelationID, RecordedAt: at,
			TraceID: run.traceID,
		})
		if err != nil {
			nodeSpan.End(OutcomeFailure, err)
			return Result{}, fmt.Errorf("workflow execute: run node %s: %w", nodeID, err)
		}
		if outcome.NodeID == "" {
			outcome.NodeID = nodeID
		} else if outcome.NodeID != nodeID {
			nodeSpan.End(OutcomeFailure, nil)
			return Result{}, invalid("StepRunner returned outcome for %s while running %s", outcome.NodeID, nodeID)
		}
		nodeOutcome := OutcomeSuccess
		if outcome.Failed {
			nodeOutcome = OutcomeFailure
		}
		nodeSpan.End(nodeOutcome, nil)

		advanced, created, evidenceIDs, err := d.advanceOnce(ctx, run, result.InstanceVersion, at,
			func(context.Context, runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
				return outcome, refs, nil
			})
		if err != nil {
			return Result{}, err
		}
		result.Advances = append(result.Advances, advanced)
		result.WorkItems = append(result.WorkItems, created...)
		result.EvidenceIDs = append(result.EvidenceIDs, evidenceIDs...)
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

// advanceInputsFunc produces the [frontier.NodeOutcome] and
// [runtime.GovernanceRefs] one [Driver.advanceOnce] call feeds to
// [Driver.advance], from inside the same transaction advanceOnce opens.
// [drainReady] supplies a trivial constant closure over what
// [StepRunner.Run] already computed outside the transaction; [Driver.Resume]
// supplies one that loads the durable WorkItem through [WorkItemReader] and
// derives the outcome from that stored row (WF-RUN-028).
type advanceInputsFunc func(context.Context, runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, error)

func (d *Driver) advanceOnce(
	ctx context.Context,
	run runContext,
	expectedVersion int64,
	at time.Time,
	inputs advanceInputsFunc,
) (runtime.AdvanceReceipt, []workitem.WorkItem, []string, error) {
	// The advancement's own node id is not known until inputs(...) runs
	// inside the transaction below (Resume derives it from the durable
	// WorkItem it loads there), so the OBS-023 advance span opens with only
	// the instance attribute and gains node_id once outcome is known.
	advCtx, advSpan := d.opts.Instrumentation.StartAdvanceSpan(ctx, SpanAttributes{
		InstanceID: run.instanceID.String(),
	})

	tx, err := d.opts.DB.Begin(advCtx)
	if err != nil {
		advSpan.End(OutcomeFailure, err)
		return runtime.AdvanceReceipt{}, nil, nil, fmt.Errorf("workflow execute: begin advance: %w", err)
	}
	defer func() { _ = tx.Rollback(advCtx) }()
	if err := tenancy.WithTenant(advCtx, tx, run.start.TenantID); err != nil {
		advSpan.End(OutcomeFailure, err)
		return runtime.AdvanceReceipt{}, nil, nil, err
	}

	outcome, refs, err := inputs(advCtx, tx)
	if err != nil {
		advSpan.End(OutcomeFailure, err)
		return runtime.AdvanceReceipt{}, nil, nil, err
	}

	if d.opts.Currency != nil {
		verdict, cerr := d.opts.Currency.Check(advCtx, tx, CurrencyCheckRequest{
			TenantID: run.start.TenantID, InstanceID: run.instanceID,
			Proposal: run.start.Proposal, CheckedAt: at,
		})
		if cerr != nil {
			advSpan.End(OutcomeFailure, cerr)
			return runtime.AdvanceReceipt{}, nil, nil, cerr
		}
		if verdict.Blocked {
			if err := blockInstance(advCtx, tx, run.start.TenantID, run.instanceID, verdict); err != nil {
				advSpan.End(OutcomeFailure, err)
				return runtime.AdvanceReceipt{}, nil, nil, err
			}
			if err := tx.Commit(advCtx); err != nil {
				advSpan.End(OutcomeFailure, err)
				return runtime.AdvanceReceipt{}, nil, nil, fmt.Errorf("workflow execute: commit currency block: %w", err)
			}
			blockedErr := fmt.Errorf("%w: %s (%s)",
				ErrCurrencyBlocked, verdict.Reason, strings.Join(verdict.Explanation, "; "))
			advSpan.End(OutcomeDenied, blockedErr)
			return runtime.AdvanceReceipt{}, nil, nil, blockedErr
		}
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
		// WF-RUN-030: the END node's own outcome, so a COMPLETE intent's
		// terminal write never has to discard it.
		endNodeID: outcome.NodeID, endOutputDigest: outcome.OutputDigest,
		// OBS-023/OBS-024: the terminal write this sink may perform opens
		// its own span and records its own evidence entry.
		instrumentation: d.opts.Instrumentation, evidence: d.opts.Evidence,
	}
	advanced, err := d.advance(advCtx, tx, runtime.AdvanceRequest{
		TenantID: run.start.TenantID, InstanceID: run.instanceID,
		ExpectedInstanceVersion: expectedVersion, Attempt: 1,
		Plan: run.selection.Plan, Outcome: outcome, Refs: refs,
		RecordedAt: at, Sink: sink, TraceID: run.traceID,
	})
	if err != nil {
		advSpan.End(OutcomeFailure, err)
		return runtime.AdvanceReceipt{}, nil, nil, err
	}
	if err := tx.Commit(advCtx); err != nil {
		advSpan.End(OutcomeFailure, err)
		return runtime.AdvanceReceipt{}, nil, nil, fmt.Errorf("workflow execute: commit advance of %s: %w", outcome.NodeID, err)
	}
	advOutcome := OutcomeSuccess
	if !advanced.Complete && len(advanced.Continuations) > 0 {
		for _, rec := range advanced.Continuations {
			if rec.Kind == frontier.IntentWorkItemRequired {
				advOutcome = OutcomeParked
				break
			}
		}
	}
	advSpan.End(advOutcome, nil)
	return advanced, append([]workitem.WorkItem(nil), sink.created...), append([]string(nil), sink.evidenceIDs...), nil
}

type fixedResolver struct{ selection runtime.WorkflowSelection }

func (r fixedResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return r.selection, nil
}

var _ runtime.WorkflowResolver = fixedResolver{}
