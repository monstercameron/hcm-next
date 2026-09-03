package signal

import (
	"fmt"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// InstanceContext carries the per-workflow-instance values a compiled SIGNAL
// node needs but a definition cannot know: which tenant and running instance
// is waiting, the correlation value the node's declared correlation key
// expression resolved to for that instance, and when the subscription
// closes. A compiled plan is shared across every instance of a workflow
// version; these values are not.
type InstanceContext struct {
	Tenant             values.TenantId
	WorkflowInstanceID string
	// CorrelationValue is the value an inbound signal must carry on
	// [SignalSubscription.CorrelationKey] to match this instance. Evaluating
	// the node's declared correlation key expression against the instance's
	// data is the caller's job; this package only carries the result.
	CorrelationValue string
	// ClosesAt, when set, is the instant after which a signal is late. Unset
	// means the subscription never closes on its own. A caller that has
	// [workflow.CompiledSignal.CloseAfterSeconds] and the node's entry
	// instant computes this before calling [FromCompiled]; resolving it here
	// would mean reading the clock, which this package's conformance
	// forbids.
	ClosesAt values.Instant
}

// FromCompiled builds a [SignalSubscription] from a compiled SIGNAL node and
// the per-instance context a definition cannot carry on its own.
//
// internal/workflow's compiler now carries a SIGNAL-specific binding
// (internal/workflow/definition.go's SignalSpec, compiled onto
// [workflow.CompiledNode.Signal] by internal/workflow/compile.go); this is
// the one place that combines it with instanceCtx into the durable
// subscription record [Accept] evaluates signals against.
func FromCompiled(node *workflow.CompiledNode, instanceCtx InstanceContext) (SignalSubscription, error) {
	if node == nil {
		return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node is nil")
	}
	if node.Type != workflow.StepSignal {
		return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node %q is %s, not SIGNAL", node.ID, node.Type)
	}
	s := node.Signal
	if s == nil {
		return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node %q carries no SIGNAL binding", node.ID)
	}

	sub := SignalSubscription{
		Tenant:             instanceCtx.Tenant,
		WorkflowInstanceID: instanceCtx.WorkflowInstanceID,
		NodeID:             node.ID,
		EventType:          s.EventType,
		CorrelationKey:     s.CorrelationKeyExpression,
		CorrelationValue:   instanceCtx.CorrelationValue,
		ExpectedSchemaRef:  s.ExpectedSchemaRef.String(),
		AcceptedSources:    append([]string(nil), s.AcceptedSources...),
		Ordering:           OrderingExpectation(s.Ordering),
		ClosesAt:           instanceCtx.ClosesAt,
	}
	if err := sub.Validate(); err != nil {
		return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node %q: %w", node.ID, err)
	}
	return sub, nil
}
