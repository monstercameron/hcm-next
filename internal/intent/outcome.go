package intent

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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
	RepairRef     string
	EvidenceRefs  []string
	LegalEvidence *LegalObligationEvidence
	RecordedAt    time.Time
}

// LegalObligationEvidence binds a trusted legal-evaluation receipt to the
// exact proposal whose obligation state is being projected. Evaluation says
// which exact duties attach; Discharges separately prove which duty each
// item of evidence discharges. Counts and unbound evidence references are not
// sufficient to establish SATISFIED.
type LegalObligationEvidence struct {
	ReceiptRef         string
	ReceiptDigest      string
	BindingDigest      string
	ProposalRevisionID string
	MaterialDigest     string
	AppliedObligations []LegalBoundObligation
	Discharges         []LegalObligationDischarge
}

type LegalBoundObligation struct {
	Type       string
	ID         string
	BodyDigest string
}

type LegalObligationDischarge struct {
	Obligation   LegalBoundObligation
	EvidenceRefs []string
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
	if r.LegalEvidence != nil {
		e := r.LegalEvidence
		if e.ReceiptRef == "" || e.ReceiptDigest == "" || e.BindingDigest == "" || e.ProposalRevisionID == "" || e.MaterialDigest == "" {
			return fmt.Errorf("%w: legal obligation evidence is incomplete", ErrInvalidOutcome)
		}
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
	current, hasRevision := instance.CurrentRevision()
	if hasRevision && len(current.Obligations) > 0 {
		if err := validateLegalObligationEvidence(receipt, current); err != nil {
			return err
		}
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
	instance.LegalEvidence = cloneLegalEvidence(receipt.LegalEvidence)
	instance.InstanceVersion++
	instance.RecordedAt = values.NewInstant(receipt.RecordedAt)
	instance.LastTransitionAt = values.NewInstant(receipt.RecordedAt)
	return nil
}

func validateLegalObligationEvidence(receipt OutcomeReceipt, revision ProposalRevision) error {
	e := receipt.LegalEvidence
	if e == nil {
		return fmt.Errorf("%w: proposal declares legal obligations but carries no verified evaluation", ErrInvalidOutcome)
	}
	if e.ProposalRevisionID != revision.ProposalRevisionID || e.MaterialDigest != revision.MaterialDigest.Digest ||
		receipt.ProposalRevisionID != revision.ProposalRevisionID || receipt.MaterialDigest != revision.MaterialDigest.Digest {
		return fmt.Errorf("%w: legal evidence does not bind the current proposal", ErrOutcomeStaleRevision)
	}
	if !legalObligationsMatchProposal(e.AppliedObligations, revision.Obligations) {
		return fmt.Errorf("%w: legal evidence obligation set differs from the proposal", ErrInvalidOutcome)
	}
	switch receipt.Dimensions.Obligation {
	case lifecycle.ObligationPending:
		if len(e.AppliedObligations) == 0 {
			return fmt.Errorf("%w: PENDING requires an applied legal obligation", ErrInvalidOutcome)
		}
	case lifecycle.ObligationNotApplicable:
		if len(e.AppliedObligations) != 0 {
			return fmt.Errorf("%w: NOT_APPLICABLE conflicts with applied legal obligations", ErrInvalidOutcome)
		}
	case lifecycle.ObligationSatisfied:
		if !allAppliedObligationsDischarged(e) {
			return fmt.Errorf("%w: SATISFIED requires evidence discharging every applied obligation", ErrInvalidOutcome)
		}
	case lifecycle.ObligationWaived, lifecycle.ObligationOverdue:
		if len(e.AppliedObligations) == 0 {
			return fmt.Errorf("%w: %s requires an applied legal obligation", ErrInvalidOutcome, receipt.Dimensions.Obligation)
		}
	}
	return nil
}

func legalObligationsMatchProposal(applied []LegalBoundObligation, declared []Obligation) bool {
	if len(applied) != len(declared) {
		return false
	}
	seen := make(map[string]struct{}, len(applied))
	for _, obligation := range applied {
		if obligation.Type == "" || obligation.ID == "" || obligation.BodyDigest == "" {
			return false
		}
		key := obligation.Type + "\x00" + obligation.ID
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	for _, obligation := range declared {
		if _, ok := seen[obligation.Kind+"\x00"+obligation.ObligationID]; !ok {
			return false
		}
	}
	return true
}

func allAppliedObligationsDischarged(e *LegalObligationEvidence) bool {
	if len(e.AppliedObligations) == 0 || len(e.Discharges) != len(e.AppliedObligations) {
		return false
	}
	applied := make(map[LegalBoundObligation]struct{}, len(e.AppliedObligations))
	for _, obligation := range e.AppliedObligations {
		if obligation.Type == "" || obligation.ID == "" || obligation.BodyDigest == "" {
			return false
		}
		applied[obligation] = struct{}{}
	}
	if len(applied) != len(e.AppliedObligations) {
		return false
	}
	for _, discharge := range e.Discharges {
		if _, ok := applied[discharge.Obligation]; !ok || len(discharge.EvidenceRefs) == 0 {
			return false
		}
		delete(applied, discharge.Obligation)
	}
	return len(applied) == 0
}

func cloneLegalEvidence(in *LegalObligationEvidence) *LegalObligationEvidence {
	if in == nil {
		return nil
	}
	out := *in
	out.AppliedObligations = append([]LegalBoundObligation(nil), in.AppliedObligations...)
	out.Discharges = make([]LegalObligationDischarge, len(in.Discharges))
	for i := range in.Discharges {
		out.Discharges[i] = in.Discharges[i]
		out.Discharges[i].EvidenceRefs = append([]string(nil), in.Discharges[i].EvidenceRefs...)
	}
	return &out
}
