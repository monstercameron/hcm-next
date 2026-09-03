package definitions

import "github.com/monstercameron/hcm-next/internal/intent/lifecycle"

// The transition sets below narrow the shared kernel lifecycle profiles. A
// definition may forbid what the kernel allows; it may never allow what the
// kernel forbids, and the registry rejects any set that tries.

const (
	retentionIntentLifecycle = "INTENT_LIFECYCLE"
	retentionBusinessOutcome = "BUSINESS_OUTCOME"
	retentionReconciliation  = "RECONCILIATION_EVIDENCE"
	retentionObligation      = "OBLIGATION_EVIDENCE"
)

func edge(from, to lifecycle.StateID, retention string, governed bool) lifecycle.TransitionRule {
	return lifecycle.TransitionRule{
		From:               from,
		To:                 to,
		RequiresGovernance: governed,
		RetentionClass:     retention,
	}
}

// p1aReadRequestEdges is the request path a P1A analytical or calculation
// intent takes: draft, preflight, simulate, submit, close. It never passes
// through APPROVED, because these definitions require no approval.
func p1aReadRequestEdges() []lifecycle.TransitionRule {
	return []lifecycle.TransitionRule{
		edge("UNSPECIFIED", "DRAFT", retentionIntentLifecycle, false),
		edge("DRAFT", "PREFLIGHTED", retentionIntentLifecycle, false),
		edge("DRAFT", "WITHDRAWN", retentionIntentLifecycle, false),
		edge("DRAFT", "CANCELLED", retentionIntentLifecycle, false),
		edge("PREFLIGHTED", "SIMULATED", retentionIntentLifecycle, false),
		edge("PREFLIGHTED", "CANCELLED", retentionIntentLifecycle, false),
		edge("SIMULATED", "SUBMITTED", retentionIntentLifecycle, false),
		edge("SIMULATED", "CANCELLED", retentionIntentLifecycle, false),
		edge("SUBMITTED", "CLOSED", retentionIntentLifecycle, false),
		edge("SUBMITTED", "CANCELLED", retentionIntentLifecycle, false),
	}
}

// p1aChangeRequestEdges adds the revision and supersession paths a simulate-only
// change request needs, and still stops short of APPROVED.
func p1aChangeRequestEdges() []lifecycle.TransitionRule {
	return append(p1aReadRequestEdges(),
		edge("SIMULATED", "SIMULATED", retentionIntentLifecycle, false),
		edge("SIMULATED", "SUPERSEDED", retentionIntentLifecycle, false),
		edge("SUBMITTED", "SIMULATED", retentionIntentLifecycle, false),
		edge("SUBMITTED", "SUPERSEDED", retentionIntentLifecycle, false),
	)
}

// p1bChangeRequestEdges adds approval, closure and reopen for the bounded write
// release.
func p1bChangeRequestEdges() []lifecycle.TransitionRule {
	return append(p1aChangeRequestEdges(),
		edge("SUBMITTED", "APPROVED", retentionIntentLifecycle, true),
		edge("SUBMITTED", "REJECTED", retentionIntentLifecycle, true),
		edge("APPROVED", "SIMULATED", retentionIntentLifecycle, false),
		edge("APPROVED", "CANCELLED", retentionIntentLifecycle, true),
		edge("APPROVED", "SUPERSEDED", retentionIntentLifecycle, false),
		edge("APPROVED", "CLOSED", retentionIntentLifecycle, true),
		edge("CLOSED", "REOPENED", retentionIntentLifecycle, true),
		edge("REOPENED", "SIMULATED", retentionIntentLifecycle, false),
		edge("REOPENED", "CLOSED", retentionIntentLifecycle, false),
	)
}

// zeroEffectExecutionEdges is the whole execution path available in P1A:
// ExecutionState reaches NOT_PLANNED and BLOCKED and nothing else. Nothing
// here can schedule, revalidate, execute or commit.
func zeroEffectExecutionEdges() []lifecycle.TransitionRule {
	return []lifecycle.TransitionRule{
		edge("UNSPECIFIED", "NOT_PLANNED", retentionIntentLifecycle, false),
		edge("NOT_PLANNED", "BLOCKED", retentionIntentLifecycle, false),
		edge("BLOCKED", "NOT_PLANNED", retentionIntentLifecycle, false),
	}
}

// writeExecutionEdges is the P1B execution path.
func writeExecutionEdges() []lifecycle.TransitionRule {
	return append(zeroEffectExecutionEdges(),
		edge("NOT_PLANNED", "SCHEDULED", retentionIntentLifecycle, false),
		edge("SCHEDULED", "REVALIDATING", retentionIntentLifecycle, false),
		edge("SCHEDULED", "NOT_PLANNED", retentionIntentLifecycle, false),
		edge("SCHEDULED", "BLOCKED", retentionIntentLifecycle, false),
		edge("REVALIDATING", "EXECUTING", retentionIntentLifecycle, true),
		edge("REVALIDATING", "NOT_PLANNED", retentionIntentLifecycle, false),
		edge("REVALIDATING", "BLOCKED", retentionIntentLifecycle, false),
		edge("EXECUTING", "COMMITTED", retentionIntentLifecycle, false),
		edge("EXECUTING", "BLOCKED", retentionIntentLifecycle, false),
		edge("EXECUTING", "REPAIR_REQUIRED", retentionIntentLifecycle, false),
		edge("COMMITTED", "REPAIR_REQUIRED", retentionIntentLifecycle, false),
		edge("BLOCKED", "SCHEDULED", retentionIntentLifecycle, false),
		edge("REPAIR_REQUIRED", "SCHEDULED", retentionIntentLifecycle, false),
		edge("REPAIR_REQUIRED", "COMMITTED", retentionIntentLifecycle, false),
	)
}

func businessEdges() []lifecycle.TransitionRule {
	return []lifecycle.TransitionRule{
		edge("UNSPECIFIED", "NOT_STARTED", retentionBusinessOutcome, false),
		edge("NOT_STARTED", "IN_PROGRESS", retentionBusinessOutcome, false),
		edge("NOT_STARTED", "UNKNOWN", retentionBusinessOutcome, false),
		edge("IN_PROGRESS", "COMPLETED", retentionBusinessOutcome, false),
		edge("IN_PROGRESS", "NOT_ACHIEVED", retentionBusinessOutcome, false),
		edge("IN_PROGRESS", "UNKNOWN", retentionBusinessOutcome, false),
	}
}

// notApplicableConsistencyEdges is the consistency path of an intent that
// causes no external effect: it is NOT_APPLICABLE and stays there.
func notApplicableConsistencyEdges() []lifecycle.TransitionRule {
	return []lifecycle.TransitionRule{
		edge("UNSPECIFIED", "NOT_APPLICABLE", retentionReconciliation, false),
	}
}

func observedConsistencyEdges() []lifecycle.TransitionRule {
	return append(notApplicableConsistencyEdges(),
		edge("UNSPECIFIED", "PENDING_OBSERVATION", retentionReconciliation, false),
		edge("NOT_APPLICABLE", "PENDING_OBSERVATION", retentionReconciliation, false),
		edge("PENDING_OBSERVATION", "CONSISTENT", retentionReconciliation, false),
		edge("PENDING_OBSERVATION", "DEGRADED", retentionReconciliation, false),
		edge("PENDING_OBSERVATION", "UNKNOWN", retentionReconciliation, false),
		edge("DEGRADED", "REPAIRING", retentionReconciliation, false),
		edge("REPAIRING", "CONSISTENT", retentionReconciliation, false),
	)
}

func notApplicableObligationEdges() []lifecycle.TransitionRule {
	return []lifecycle.TransitionRule{
		edge("UNSPECIFIED", "NOT_APPLICABLE", retentionObligation, false),
	}
}

func obligationEdges() []lifecycle.TransitionRule {
	return append(notApplicableObligationEdges(),
		edge("UNSPECIFIED", "PENDING", retentionObligation, false),
		edge("NOT_APPLICABLE", "PENDING", retentionObligation, false),
		edge("PENDING", "SATISFIED", retentionObligation, false),
		edge("PENDING", "OVERDUE", retentionObligation, false),
		edge("PENDING", "WAIVED", retentionObligation, true),
		edge("OVERDUE", "SATISFIED", retentionObligation, false),
	)
}

// zeroEffectTransitions is the full five-dimension transition set of a P1A
// analytical or calculation definition.
func zeroEffectTransitions() map[lifecycle.Dimension][]lifecycle.TransitionRule {
	return map[lifecycle.Dimension][]lifecycle.TransitionRule{
		lifecycle.DimensionRequest:     p1aReadRequestEdges(),
		lifecycle.DimensionExecution:   zeroEffectExecutionEdges(),
		lifecycle.DimensionBusiness:    businessEdges(),
		lifecycle.DimensionConsistency: notApplicableConsistencyEdges(),
		lifecycle.DimensionObligation:  notApplicableObligationEdges(),
	}
}

// simulateOnlyChangeTransitions is the transition set of a change request that
// P1A only simulates.
func simulateOnlyChangeTransitions() map[lifecycle.Dimension][]lifecycle.TransitionRule {
	return map[lifecycle.Dimension][]lifecycle.TransitionRule{
		lifecycle.DimensionRequest:     p1aChangeRequestEdges(),
		lifecycle.DimensionExecution:   zeroEffectExecutionEdges(),
		lifecycle.DimensionBusiness:    businessEdges(),
		lifecycle.DimensionConsistency: notApplicableConsistencyEdges(),
		lifecycle.DimensionObligation:  notApplicableObligationEdges(),
	}
}

// writeChangeTransitions is the transition set of a P1B change request.
func writeChangeTransitions() map[lifecycle.Dimension][]lifecycle.TransitionRule {
	return map[lifecycle.Dimension][]lifecycle.TransitionRule{
		lifecycle.DimensionRequest:     p1bChangeRequestEdges(),
		lifecycle.DimensionExecution:   writeExecutionEdges(),
		lifecycle.DimensionBusiness:    businessEdges(),
		lifecycle.DimensionConsistency: observedConsistencyEdges(),
		lifecycle.DimensionObligation:  obligationEdges(),
	}
}
