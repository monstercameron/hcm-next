package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// ErrOutcomeProjectionConflict means the durable intent projection already
// contains a different terminal tuple or was advanced by another writer.
var ErrOutcomeProjectionConflict = errors.New("app: intent outcome projection conflict")

// OutcomeBinding is the application-to-store contract for one terminal fact.
// The store owns the transaction and the compare-and-swap; the application
// owns correlation and typed receipt validation.
type OutcomeBinding struct {
	Tenant                  string
	ExpectedInstanceVersion uint64
	Receipt                 intent.OutcomeReceipt
}

// OutcomeBinder is optional so in-memory and pre-outcome adapters remain
// source-compatible. Production stores implement it to update the durable
// intent_instance projection.
type OutcomeBinder interface {
	BindOutcome(context.Context, OutcomeBinding) error
}

type LegalEvidenceRequest struct {
	Tenant, IntentID, ProposalRevisionID, MaterialDigest, LegalContextDigest string
}

// LegalEvidenceVerifier resolves immutable receipt evidence and authenticates
// both it and its proposal binding against configured trusted issuer keys.
type LegalEvidenceVerifier interface {
	VerifyLegalEvidence(context.Context, LegalEvidenceRequest) (legal.EvaluationBinding, error)
}

// OutcomeReceiptFromExecution converts the driver's terminal result into the
// kernel receipt. The compiled END tuple is preferred; the bounded terminal
// node fallback keeps older adapters correct until they expose the tuple
// directly through ExecutionResult.
func OutcomeReceiptFromExecution(instance intent.Instance, result ExecutionResult, now time.Time) (intent.OutcomeReceipt, bool, error) {
	if err := result.validate(); err != nil {
		return intent.OutcomeReceipt{}, false, err
	}
	if result.effectiveStatus() == ExecutionResultParked || result.effectiveStatus() == ExecutionResultResolved {
		return intent.OutcomeReceipt{}, false, nil
	}
	dimensions := result.TerminalDimensions
	terminalCode := result.TerminalCode
	if !declaredDimensions(dimensions) {
		var ok bool
		dimensions, terminalCode, ok = terminalOutcome(result.VisitedNodes)
		if !ok {
			return intent.OutcomeReceipt{}, false, nil
		}
	}
	if terminalCode == "" {
		terminalCode = terminalCodeFor(result.VisitedNodes)
	}
	if terminalCode == "" {
		return intent.OutcomeReceipt{}, false, fmt.Errorf("app: terminal dimensions have no terminal code")
	}
	reconciliation := result.Reconciliation
	if reconciliation == "" {
		reconciliation = reconciliationFor(dimensions.Consistency)
	}
	if now.IsZero() {
		now = result.RecordedAt
	}
	commitRef, repairRef := terminalRefs(result)
	var proposalID, materialDigest string
	if revision, ok := instance.CurrentRevision(); ok {
		proposalID, materialDigest = revision.ProposalRevisionID, revision.MaterialDigest.Digest
	}
	return intent.OutcomeReceipt{
		IntentID: instance.IntentID, WorkflowInstanceID: result.InstanceID,
		TerminalCode: terminalCode, Dimensions: dimensions, Reconciliation: reconciliation,
		CommitReceiptRef: commitRef, RepairRef: repairRef,
		ProposalRevisionID: proposalID, MaterialDigest: materialDigest,
		EvidenceRefs: append([]string(nil), result.EvidenceIDs...), RecordedAt: now.UTC(),
	}, true, nil
}

// terminalRefs returns the commit receipt and repair references for the
// terminal the driver reached. Adapter-supplied values win; otherwise the
// compiled promotion plan's END node for the last visited node supplies its
// declared references, qualified by the workflow instance so the receipt
// names one execution rather than the plan in general.
func terminalRefs(result ExecutionResult) (commitRef, repairRef string) {
	commitRef, repairRef = result.CommitReceiptRef, result.RepairRef
	if commitRef != "" || repairRef != "" || len(result.VisitedNodes) == 0 {
		return commitRef, repairRef
	}
	last := result.VisitedNodes[len(result.VisitedNodes)-1]
	for _, node := range promotionexec.Definition().Nodes {
		if node.ID != last || node.End == nil {
			continue
		}
		if node.End.CommitReceiptRef != "" {
			commitRef = node.End.CommitReceiptRef + ":" + result.InstanceID
		}
		if len(node.End.RepairRefs) > 0 {
			repairRef = node.End.RepairRefs[0] + ":" + result.InstanceID
		} else if len(node.End.IncidentRefs) > 0 {
			repairRef = node.End.IncidentRefs[0] + ":" + result.InstanceID
		}
		break
	}
	return commitRef, repairRef
}

func declaredDimensions(dimensions lifecycle.Dimensions) bool {
	if dimensions.Validate() != nil {
		return false
	}
	for _, dimension := range lifecycle.AllDimensions() {
		if dimensions.State(dimension) == "UNSPECIFIED" {
			return false
		}
	}
	return true
}

func terminalOutcome(visited []string) (lifecycle.Dimensions, string, bool) {
	for i := len(visited) - 1; i >= 0; i-- {
		switch visited[i] {
		case "end_complete":
			return lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted,
				Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent, Obligation: lifecycle.ObligationSatisfied}, "PROMOTION_COMPLETE", true
		case "end_repair_plan":
			return lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionRepairRequired,
				Business: lifecycle.BusinessUnknown, Consistency: lifecycle.ConsistencyDegraded, Obligation: lifecycle.ObligationPending}, "PROMOTION_REPAIR_REQUIRED", true
		case "end_blocked":
			return lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionBlocked,
				Business: lifecycle.BusinessNotAchieved, Consistency: lifecycle.ConsistencyUnknown, Obligation: lifecycle.ObligationPending}, "PROMOTION_BLOCKED", true
		case "end_rejected":
			return lifecycle.Dimensions{Request: lifecycle.RequestRejected, Execution: lifecycle.ExecutionNotPlanned,
				Business: lifecycle.BusinessNotAchieved, Consistency: lifecycle.ConsistencyNotApplicable, Obligation: lifecycle.ObligationNotApplicable}, "PROMOTION_REJECTED", true
		case "end_invalidated":
			return lifecycle.Dimensions{Request: lifecycle.RequestSuperseded, Execution: lifecycle.ExecutionNotPlanned,
				Business: lifecycle.BusinessNotAchieved, Consistency: lifecycle.ConsistencyNotApplicable, Obligation: lifecycle.ObligationNotApplicable}, "PROMOTION_INVALIDATED", true
		case "end_expired", "end_cancelled":
			return lifecycle.Dimensions{Request: lifecycle.RequestCancelled, Execution: lifecycle.ExecutionNotPlanned,
				Business: lifecycle.BusinessNotAchieved, Consistency: lifecycle.ConsistencyNotApplicable, Obligation: lifecycle.ObligationNotApplicable}, "PROMOTION_CANCELLED", true
		}
	}
	return lifecycle.Dimensions{}, "", false
}

func terminalCodeFor(visited []string) string {
	_, code, _ := terminalOutcome(visited)
	return code
}

func reconciliationFor(state lifecycle.ConsistencyState) intent.ReconciliationVerdict {
	switch state {
	case lifecycle.ConsistencyConsistent:
		return intent.ReconciliationPass
	case lifecycle.ConsistencyDegraded:
		return intent.ReconciliationRepairRequired
	case lifecycle.ConsistencyUnknown:
		return intent.ReconciliationUnknown
	default:
		return intent.ReconciliationWaived
	}
}

// consumeExecutionResult validates and durably forwards a terminal receipt.
// It is intentionally a no-op for parked results and for adapters that do not
// yet implement OutcomeBinder.
func (s *IntentService) consumeExecutionResult(ctx context.Context, instance intent.Instance, def intent.Definition, rec IntentRecord, result ExecutionResult) error {
	receipt, found, err := OutcomeReceiptFromExecution(instance, result, s.clock().Time())
	if err != nil || !found {
		return err
	}
	if revision, ok := instance.CurrentRevision(); ok && len(revision.Obligations) > 0 {
		if s.legalEvidence == nil {
			return fmt.Errorf("app: legal obligations require a trusted evidence verifier")
		}
		binding, verifyErr := s.legalEvidence.VerifyLegalEvidence(ctx, LegalEvidenceRequest{Tenant: rec.Tenant, IntentID: instance.IntentID, ProposalRevisionID: revision.ProposalRevisionID, MaterialDigest: revision.MaterialDigest.Digest, LegalContextDigest: instance.ControlSnapshots.LegalContextDigest})
		if verifyErr != nil {
			return fmt.Errorf("app: verify legal obligation evidence: %w", verifyErr)
		}
		if binding.Tenant != rec.Tenant || binding.IntentID != instance.IntentID || binding.ProposalRevisionID != revision.ProposalRevisionID || binding.MaterialDigest != revision.MaterialDigest.Digest || binding.LegalContextDigest != instance.ControlSnapshots.LegalContextDigest {
			return fmt.Errorf("app: verified legal evidence does not bind the current proposal")
		}
		evidence := &intent.LegalObligationEvidence{ReceiptRef: binding.ReceiptRef, ReceiptDigest: binding.ReceiptDigest, BindingDigest: binding.Digest, ProposalRevisionID: binding.ProposalRevisionID, MaterialDigest: binding.MaterialDigest}
		for _, obligation := range binding.AppliedObligations {
			evidence.AppliedObligations = append(evidence.AppliedObligations, intent.LegalBoundObligation{Type: obligation.Type.String(), ID: obligation.ID, BodyDigest: obligation.BodyDigest})
		}
		for _, discharge := range binding.Discharges {
			evidence.Discharges = append(evidence.Discharges, intent.LegalObligationDischarge{
				Obligation:   intent.LegalBoundObligation{Type: discharge.Obligation.Type.String(), ID: discharge.Obligation.ID, BodyDigest: discharge.Obligation.BodyDigest},
				EvidenceRefs: append([]string(nil), discharge.EvidenceRefs...),
			})
		}
		receipt.LegalEvidence = evidence
	}
	candidate := instance
	if err := intent.BindOutcome(&candidate, def, receipt); err != nil {
		return err
	}
	binder, ok := s.store.(OutcomeBinder)
	if !ok {
		return nil
	}
	return binder.BindOutcome(ctx, OutcomeBinding{Tenant: rec.Tenant, ExpectedInstanceVersion: rec.InstanceVersion, Receipt: receipt})
}
