package intent

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// OutcomeReceipt is the typed hand-off from a workflow terminal to an Intent.
// It carries the terminal tuple as one fact, while Reconciliation names the
// independent RECON-002 verdict that explains the consistency dimension.
type OutcomeReceipt struct {
	IntentID            string
	WorkflowInstanceID  string
	WorkflowInstanceVer int64
	ProposalRevisionID  string
	MaterialDigest      string
	TerminalCode        string
	Dimensions          lifecycle.Dimensions
	Reconciliation      ReconciliationVerdict
	// CommitReceiptRef is the receipt the terminal's commit produced; required
	// when Dimensions.Execution is COMMITTED.
	CommitReceiptRef string
	// RepairRef links the RepairPlan or incident a REPAIR_REQUIRED terminal
	// raised; required when Dimensions.Execution is REPAIR_REQUIRED.
	RepairRef    string
	EvidenceRefs []string
	RecordedAt   time.Time
}

// ReconciliationVerdict is the bounded RECON-002 answer carried beside the
// five lifecycle dimensions. It is not a sixth lifecycle dimension.
type ReconciliationVerdict string

const (
	ReconciliationPass           ReconciliationVerdict = "PASS"
	ReconciliationMismatch       ReconciliationVerdict = "MISMATCH"
	ReconciliationPartial        ReconciliationVerdict = "PARTIAL"
	ReconciliationUnknown        ReconciliationVerdict = "UNKNOWN"
	ReconciliationRepairRequired ReconciliationVerdict = "REPAIR_REQUIRED"
	ReconciliationWaived         ReconciliationVerdict = "WAIVED"
)

var (
	// ErrOutcomeConflict reports a second terminal fact that disagrees with
	// the tuple already bound to the intent.
	ErrOutcomeConflict = errors.New("intent: conflicting workflow outcome")
	// ErrOutcomeStaleRevision reports a terminal from an obsolete proposal.
	ErrOutcomeStaleRevision = errors.New("intent: stale workflow outcome revision")
	// ErrInvalidOutcome reports an outcome receipt that cannot be authoritative.
	ErrInvalidOutcome = errors.New("intent: invalid workflow outcome")
)

// Validate rejects an outcome that cannot be correlated or replayed safely.
func (r OutcomeReceipt) Validate() error {
	if r.IntentID == "" {
		return fmt.Errorf("%w: intent id is required", ErrInvalidOutcome)
	}
	if r.WorkflowInstanceID == "" {
		return fmt.Errorf("%w: workflow instance id is required", ErrInvalidOutcome)
	}
	if r.TerminalCode == "" {
		return fmt.Errorf("%w: terminal code is required", ErrInvalidOutcome)
	}
	if err := r.Dimensions.Validate(); err != nil {
		return fmt.Errorf("%w: terminal dimensions: %v", ErrInvalidOutcome, err)
	}
	switch r.Reconciliation {
	case ReconciliationPass, ReconciliationMismatch, ReconciliationPartial,
		ReconciliationUnknown, ReconciliationRepairRequired, ReconciliationWaived:
	default:
		return fmt.Errorf("%w: reconciliation verdict %q is not declared", ErrInvalidOutcome, r.Reconciliation)
	}
	if r.RecordedAt.IsZero() {
		return fmt.Errorf("%w: recorded_at is required", ErrInvalidOutcome)
	}
	if r.Dimensions.Execution == lifecycle.ExecutionCommitted && r.CommitReceiptRef == "" {
		return fmt.Errorf("%w: COMMITTED requires a commit receipt ref", ErrInvalidOutcome)
	}
	if r.Dimensions.Execution == lifecycle.ExecutionRepairRequired && r.RepairRef == "" {
		return fmt.Errorf("%w: REPAIR_REQUIRED requires a repair ref", ErrInvalidOutcome)
	}
	return nil
}

// BindOutcome applies one authoritative terminal tuple to an instance. It is
// idempotent for the exact same tuple and refuses an obsolete proposal or a
// conflicting terminal; it never rewrites a superseding revision.
func BindOutcome(instance *Instance, def Definition, receipt OutcomeReceipt) error {
	if instance == nil {
		return fmt.Errorf("%w: instance is nil", ErrInvalidOutcome)
	}
	if err := receipt.Validate(); err != nil {
		return err
	}
	if err := instance.Lifecycle.Validate(); err != nil {
		return fmt.Errorf("%w: current lifecycle: %v", ErrInvalidOutcome, err)
	}
	for _, dimension := range lifecycle.AllDimensions() {
		if instance.Lifecycle.State(dimension) == "UNSPECIFIED" {
			return fmt.Errorf("%w: current lifecycle has UNSPECIFIED %s", ErrInvalidOutcome, dimension)
		}
	}
	if receipt.IntentID != instance.IntentID {
		return fmt.Errorf("%w: receipt names %s, instance is %s", ErrOutcomeConflict, receipt.IntentID, instance.IntentID)
	}
	if current, ok := instance.CurrentRevision(); ok && receipt.ProposalRevisionID != "" &&
		current.ProposalRevisionID != receipt.ProposalRevisionID {
		return fmt.Errorf("%w: receipt names proposal %s, current revision is %s",
			ErrOutcomeStaleRevision, receipt.ProposalRevisionID, current.ProposalRevisionID)
	}
	if instance.Lifecycle == receipt.Dimensions {
		return nil
	}
	if instance.Lifecycle.Execution == lifecycle.ExecutionCommitted ||
		instance.Lifecycle.Execution == lifecycle.ExecutionRepairRequired ||
		instance.Lifecycle.Execution == lifecycle.ExecutionBlocked {
		return fmt.Errorf("%w: intent already carries a terminal execution state", ErrOutcomeConflict)
	}
	if receipt.Reconciliation == ReconciliationPass && receipt.Dimensions.Consistency != lifecycle.ConsistencyConsistent {
		return fmt.Errorf("%w: PASS requires CONSISTENT", ErrInvalidOutcome)
	}
	if receipt.Reconciliation == ReconciliationRepairRequired && receipt.Dimensions.Consistency != lifecycle.ConsistencyDegraded {
		return fmt.Errorf("%w: REPAIR_REQUIRED requires DEGRADED", ErrInvalidOutcome)
	}
	if instance.Definition != def.Ref {
		return fmt.Errorf("%w: definition mismatch", ErrOutcomeConflict)
	}
	instance.Lifecycle = receipt.Dimensions
	instance.CommitReceiptRef = receipt.CommitReceiptRef
	instance.RepairRef = receipt.RepairRef
	instance.InstanceVersion++
	instance.RecordedAt = values.NewInstant(receipt.RecordedAt)
	instance.LastTransitionAt = values.NewInstant(receipt.RecordedAt)
	return nil
}
