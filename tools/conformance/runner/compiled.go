// This file adds the second Runner implementation the package doc anticipated:
// one that executes a compiled workflow in real SIMULATE mode instead of
// reading the document as data.
//
// It deliberately does not change CONF-001's report shape. The executed
// receipt arrives as additional checks.Result entries, which the report
// already renders in both JSON and Markdown, so a compiled engine adds
// evidence to the report without every consumer of the report having to learn
// a new schema.

package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/timeauth"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/checks"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/model"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

// PromotionDocumentID is the parsed workflow id of
// planning/reference-workflows/promote-into-management.md. parse derives a
// standalone document's id from its file name, so this is the one string that
// ties the document to the compiled plan built from
// workflow.CompilePromotionReference.
const PromotionDocumentID = "promote-into-management"

// Executed-receipt check ids. They sort after every CHK-0NN document check, so
// the executed section is a contiguous block at the end of a workflow's check
// list rather than interleaved with the document analysis.
const (
	CheckExecuted      = "CHK-EXE-001"
	CheckExecutedTrace = "CHK-EXE-002"
	CheckZeroEffects   = "CHK-EXE-003"
	CheckLifecycle     = "CHK-EXE-004"
	CheckAwaitedWork   = "CHK-EXE-005"
	CheckReceiptDigest = "CHK-EXE-006"
)

// specRefs are the clauses the executed section is evidence for.
var specRefs = []string{
	"planning/specs/workflow-runtime.md#execution-modes",
	"planning/specs/workflow-runtime.md#phase-1-acceptance-contract",
	"definitions/runtime/durable-runtime-decision.yaml#WF-RUN-000",
}

// Executor produces the executed-receipt section for one parsed workflow
// document, or reports that it has no compiled plan for it.
//
// It is an interface so a second reference workflow can be executed later
// without this file growing a switch: the seam is "which document does this
// executor have a plan for", not "which documents exist".
type Executor interface {
	// Execute returns the executed-receipt checks for wf. found is false when
	// this executor has no compiled plan bound to that document, which is a
	// stated UNKNOWN in the report rather than a silent omission.
	Execute(ctx context.Context, wf *model.Workflow, clock Clock) (results []checks.Result, found bool)
}

// PlanRunner is a Runner that reads the document like DocumentRunner does and
// then, when a compiled plan is bound to it, actually executes that plan in
// SIMULATE mode and reports the receipt.
//
// The two halves are kept together on purpose. A conformance report that
// replaced its document analysis with an execution would stop noticing the
// document drifting away from the plan, which is exactly the failure CONF-001
// exists to catch.
type PlanRunner struct {
	Vocab *vocab.Vocabulary
	// Executor supplies the executed section. Nil means PromotionExecutor.
	Executor Executor
}

var _ Runner = PlanRunner{}

// Simulate implements Runner. Like DocumentRunner it never returns an error:
// an execution that refuses is a FAIL check, because a batch of many documents
// must always produce a complete report.
func (r PlanRunner) Simulate(wf *model.Workflow, clock Clock) (Receipt, error) {
	receipt := Receipt{
		WorkflowID:    wf.ID,
		ExecutionMode: "SIMULATE",
		ClockAt:       clock.Now(),
		Effects:       nil,
		Checks:        checks.Evaluate(wf, r.Vocab),
	}

	executor := r.Executor
	if executor == nil {
		executor = PromotionExecutor{}
	}
	executed, found := executor.Execute(context.Background(), wf, clock)
	if !found {
		receipt.Checks = append(receipt.Checks, checks.Result{
			ID:     CheckExecuted,
			Name:   "CompiledPlanExecuted",
			Status: checks.Unknown,
			Detail: "no compiled plan is bound to this workflow, so nothing was executed; " +
				"the document checks above are the whole result",
			Refs: specRefs,
		})
		return receipt, nil
	}
	receipt.Checks = append(receipt.Checks, executed...)
	return receipt, nil
}

// PromotionExecutor executes the promote-into-management reference workflow
// through the in-memory SIMULATE interpreter.
type PromotionExecutor struct {
	// ProposedBasePay overrides the reference proposal. Empty means the
	// interpreter's own reference amount.
	ProposedBasePay string
}

// Execute implements Executor.
func (e PromotionExecutor) Execute(ctx context.Context, wf *model.Workflow, clock Clock) ([]checks.Result, bool) {
	if wf == nil || wf.ID != PromotionDocumentID || wf.Kind != model.KindDocument {
		return nil, false
	}
	pay := e.ProposedBasePay
	if pay == "" {
		pay = simulate.PromotionWithinThresholdPay
	}

	setup, err := simulate.NewPromotionSetup(pay)
	if err != nil {
		return []checks.Result{{
			ID:     CheckExecuted,
			Name:   "CompiledPlanExecuted",
			Status: checks.Fail,
			Detail: "the promotion reference workflow could not be compiled: " + err.Error(),
			Refs:   specRefs,
		}}, true
	}

	// The report's fixed clock drives the simulation's virtual clock, so the
	// executed section is stamped from the same instant the rest of the report
	// is and two runs stay byte-identical.
	setup.Options.Clock = timeauth.NewFakeClock("conformance.simulate", clock.Now().UTC())

	receipt, err := simulate.Run(ctx, setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		return []checks.Result{{
			ID:     CheckExecuted,
			Name:   "CompiledPlanExecuted",
			Status: checks.Fail,
			Detail: "SIMULATE run refused: " + err.Error(),
			Refs:   specRefs,
		}}, true
	}
	return ExecutedReceiptChecks(receipt), true
}

// ExecutedReceiptChecks renders a simulation receipt as the report's
// executed-receipt section.
//
// Every entry is a fact the run produced, not a restatement of the plan: the
// node trace is what actually ran, the counters are what the run counted, and
// the digest is the identity another run has to reproduce.
func ExecutedReceiptChecks(receipt simulate.Receipt) []checks.Result {
	counters := receipt.EffectCounters()
	zeroStatus, zeroDetail := checks.Pass, "every effect counter is zero"
	if !counters.IsZero() {
		zeroStatus = checks.Fail
		zeroDetail = "the run counted effects: " + strings.Join(counters.NonZero(), ", ")
	}

	hops := make([]string, 0, len(receipt.Trace))
	for _, entry := range receipt.Trace {
		hop := fmt.Sprintf("%d. %s (%s) -> %s", entry.Order, entry.NodeID, entry.Type, entry.Outcome)
		if entry.NextNodeID != "" {
			hop += " -> " + entry.NextNodeID
		}
		hops = append(hops, hop)
	}

	awaited := make([]string, 0, len(receipt.WorkItems))
	for _, item := range receipt.WorkItems {
		awaited = append(awaited, fmt.Sprintf("%s [%s] stage %d quorum %d, %d candidate(s)",
			item.RequirementID, item.Outcome, item.Stage, item.QuorumMin, len(item.Candidates)))
	}
	sort.Strings(awaited)
	awaitedStatus, awaitedDetail := checks.Pass, "would await: "+strings.Join(awaited, "; ")
	if len(awaited) == 0 {
		awaitedStatus = checks.Unknown
		awaitedDetail = "the terminal this run reached declares no approval requirement, so no work item would be awaited"
	}

	lifecycle := receipt.Lifecycle
	obligations := "none"
	if len(receipt.Terminal.OutstandingObligationRefs) > 0 {
		obligations = strings.Join(receipt.Terminal.OutstandingObligationRefs, ", ")
	}

	return []checks.Result{
		{
			ID:     CheckExecuted,
			Name:   "CompiledPlanExecuted",
			Status: checks.Pass,
			Detail: fmt.Sprintf("executed %s v%d (plan %s) in %s under %s; %d node(s), %s virtual time",
				receipt.WorkflowID, receipt.WorkflowVersion, receipt.PlanDigest, receipt.Mode,
				receipt.InterpreterVersion, len(receipt.Trace), receipt.Elapsed),
			Refs: specRefs,
		},
		{
			ID:     CheckExecutedTrace,
			Name:   "ExecutedNodeTrace",
			Status: checks.Pass,
			Detail: strings.Join(hops, "; "),
			Refs:   specRefs,
		},
		{
			ID:     CheckZeroEffects,
			Name:   "ExecutedZeroEffects",
			Status: zeroStatus,
			Detail: zeroDetail,
			Refs:   specRefs,
		},
		{
			ID:     CheckLifecycle,
			Name:   "ExecutedTerminalLifecycle",
			Status: checks.Pass,
			Detail: fmt.Sprintf(
				"terminal %s (%s, runtime %s): RequestState=%s ExecutionState=%s BusinessState=%s ConsistencyState=%s ObligationState=%s; outstanding obligations: %s",
				receipt.Terminal.NodeID, receipt.Terminal.TerminalCode, receipt.Terminal.RuntimeStatus,
				lifecycle.RequestState, lifecycle.ExecutionState, lifecycle.BusinessState,
				lifecycle.ConsistencyState, lifecycle.ObligationState, obligations),
			Refs: specRefs,
		},
		{
			ID:     CheckAwaitedWork,
			Name:   "ExecutedWorkItemsWouldAwait",
			Status: awaitedStatus,
			Detail: awaitedDetail,
			Refs:   specRefs,
		},
		{
			ID:     CheckReceiptDigest,
			Name:   "ExecutedReceiptDigest",
			Status: checks.Pass,
			Detail: "receipt " + receipt.Digest() + "; inputs " + receipt.InputsDigest +
				"; zero-effect receipt " + receipt.ZeroEffectDigest,
			Refs: specRefs,
		},
	}
}

// ExecutedCheckIDs lists the executed-receipt section's check ids in report
// order, so a consumer can locate the section without matching on prefixes.
func ExecutedCheckIDs() []string {
	return []string{
		CheckExecuted,
		CheckExecutedTrace,
		CheckZeroEffects,
		CheckLifecycle,
		CheckAwaitedWork,
		CheckReceiptDigest,
	}
}
