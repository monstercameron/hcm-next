package incentive

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidRestatement = errors.New("incentive: invalid restatement")
	ErrInvalidPayroll     = errors.New("incentive: invalid payroll input")
	ErrInvalidReconcile   = errors.New("incentive: invalid measure reconciliation")
)

func digestWriter(w interface{ Bytes() ([]byte, error) }) string {
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Restatement is an append-only successor calculation. Delta is new minus
// prior and may be negative; no prior award is rewritten or deleted.
type Restatement struct {
	RestatementID string
	Prior         AwardCalculation
	Successor     AwardCalculation
	Reason        string
	EvidenceRef   string
}

type RestatementResult struct {
	RestatementID string
	PriorDigest   string
	Successor     AwardCalculation
	Delta         values.Money
	EvidenceRef   string
	Digest        string
}

func copyAward(a AwardCalculation) AwardCalculation {
	a.Inputs = append([]AwardInput(nil), a.Inputs...)
	a.MeasureInputs = append([]AwardInput(nil), a.MeasureInputs...)
	a.Thresholds = append([]AwardThreshold(nil), a.Thresholds...)
	return a
}

func (r Restatement) Validate() error {
	if strings.TrimSpace(r.RestatementID) == "" || strings.TrimSpace(r.Reason) == "" || strings.TrimSpace(r.EvidenceRef) == "" {
		return fmt.Errorf("%w: id, reason and evidence_ref are required", ErrInvalidRestatement)
	}
	if err := r.Prior.Validate(); err != nil {
		return fmt.Errorf("%w: prior: %v", ErrInvalidRestatement, err)
	}
	if err := r.Successor.Validate(); err != nil {
		return fmt.Errorf("%w: successor: %v", ErrInvalidRestatement, err)
	}
	if r.Prior.State != AwardFinalized {
		return fmt.Errorf("%w: prior award must be finalized", ErrInvalidRestatement)
	}
	if r.Prior.CalculationID != r.Successor.CalculationID || r.Prior.WorkerRef != r.Successor.WorkerRef || r.Prior.PlanDigest != r.Successor.PlanDigest || r.Prior.PlanRevision != r.Successor.PlanRevision || r.Prior.Currency != r.Successor.Currency || r.Prior.PeriodRef != r.Successor.PeriodRef {
		return fmt.Errorf("%w: successor changes award identity or currency", ErrInvalidRestatement)
	}
	if r.Successor.Revision != r.Prior.Revision+1 || r.Successor.SupersedesRevision != r.Prior.Revision {
		return fmt.Errorf("%w: successor must extend prior revision", ErrInvalidRestatement)
	}
	if r.Successor.State != AwardCalculated {
		return fmt.Errorf("%w: successor must be calculated before correction approval", ErrInvalidRestatement)
	}
	return nil
}

func (r Restatement) Apply() (RestatementResult, error) {
	if err := r.Validate(); err != nil {
		return RestatementResult{}, err
	}
	old, err := values.NewMoneyFromDecimal(r.Prior.Amount, r.Prior.Currency)
	if err != nil {
		return RestatementResult{}, err
	}
	next, err := values.NewMoneyFromDecimal(r.Successor.Amount, r.Successor.Currency)
	if err != nil {
		return RestatementResult{}, err
	}
	delta, err := next.Sub(old)
	if err != nil {
		return RestatementResult{}, err
	}
	digest := digestWriter(canonicalbytes.New("hcmnext.domains.incentive.Restatement", schemaVersion).
		String("restatement_id", r.RestatementID).String("prior_digest", r.Prior.Digest).
		String("successor_digest", r.Successor.Digest).String("reason", r.Reason).String("evidence_ref", r.EvidenceRef).
		Value("delta", delta))
	return RestatementResult{RestatementID: r.RestatementID, PriorDigest: r.Prior.Digest, Successor: copyAward(r.Successor), Delta: delta, EvidenceRef: r.EvidenceRef, Digest: digest}, nil
}

// ClawbackCalculation is a governed recovery proposal, never a mutation of
// the paid award. Amount is always positive and carries the award currency.
// RecoveredToDate is an evidence-bound assertion by the caller, not a durable
// global reservation; an adapter must serialize proposals against its ledger.
type ClawbackCalculation struct {
	ClawbackID           string
	Award                AwardCalculation
	Rule                 ClawbackRule
	Amount               values.Money
	RecoveredToDate      values.Money
	PriorClawbackDigests []string
	EvidenceRef          string
}

func (c ClawbackCalculation) Validate() error {
	if strings.TrimSpace(c.ClawbackID) == "" || strings.TrimSpace(c.EvidenceRef) == "" {
		return fmt.Errorf("%w: id and evidence_ref are required", ErrInvalidClawback)
	}
	if err := c.Award.Validate(); err != nil {
		return fmt.Errorf("%w: award: %v", ErrInvalidClawback, err)
	}
	if c.Award.State != AwardFinalized {
		return fmt.Errorf("%w: only finalized awards can be clawed back", ErrInvalidClawback)
	}
	if err := c.Rule.Validate(); err != nil {
		return err
	}
	if err := c.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: amount: %v", ErrInvalidClawback, err)
	}
	if err := c.RecoveredToDate.Validate(); err != nil || c.RecoveredToDate.Currency() != c.Award.Currency || c.RecoveredToDate.Amount().Sign() < 0 {
		return fmt.Errorf("%w: recovered_to_date must be non-negative in award currency", ErrInvalidClawback)
	}
	if c.RecoveredToDate.Amount().Sign() > 0 && len(c.PriorClawbackDigests) == 0 {
		return fmt.Errorf("%w: prior clawback evidence is required for recovered amount", ErrInvalidClawback)
	}
	seenPrior := make(map[string]struct{}, len(c.PriorClawbackDigests))
	for _, digest := range c.PriorClawbackDigests {
		if strings.TrimSpace(digest) == "" {
			return fmt.Errorf("%w: prior clawback digest is required", ErrInvalidClawback)
		}
		if _, exists := seenPrior[digest]; exists {
			return fmt.Errorf("%w: duplicate prior clawback digest", ErrInvalidClawback)
		}
		seenPrior[digest] = struct{}{}
	}
	if c.Amount.Currency() != c.Award.Currency || c.Amount.Amount().Sign() <= 0 {
		return fmt.Errorf("%w: amount must be positive in award currency", ErrInvalidClawback)
	}
	awardAmount, err := values.NewMoneyFromDecimal(c.Award.Amount, c.Award.Currency)
	if err != nil {
		return fmt.Errorf("%w: award amount: %v", ErrInvalidClawback, err)
	}
	cumulative, err := c.RecoveredToDate.Add(c.Amount)
	if err != nil {
		return fmt.Errorf("%w: cumulative amount: %v", ErrInvalidClawback, err)
	}
	if cmp, err := cumulative.Cmp(awardAmount); err != nil || cmp > 0 {
		return fmt.Errorf("%w: amount exceeds finalized award", ErrInvalidClawback)
	}
	if c.Rule.EvidenceRequired && strings.TrimSpace(c.EvidenceRef) == "" {
		return fmt.Errorf("%w: evidence is required", ErrInvalidClawback)
	}
	return nil
}

func (c ClawbackCalculation) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	priorDigests := append([]string(nil), c.PriorClawbackDigests...)
	sort.Strings(priorDigests)
	w := canonicalbytes.New("hcmnext.domains.incentive.ClawbackCalculation", schemaVersion).
		String("clawback_id", c.ClawbackID).String("award_digest", c.Award.Digest).Value("rule", c.Rule).Value("amount", c.Amount).
		Value("recovered_to_date", c.RecoveredToDate).Count("prior_clawbacks", len(priorDigests))
	for _, digest := range priorDigests {
		w.String("prior_clawback_digest", digest)
	}
	b, err := w.String("evidence_ref", c.EvidenceRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// PayrollInput is the sole immutable projection consumed by payroll. Its
// source digest makes retries idempotent and prevents an unapproved award.
type PayrollInput struct {
	InputID       string
	WorkerRef     string
	PeriodRef     string
	Amount        values.Money
	SourceDigest  string
	SourceKind    string
	ApprovalRef   string
	authoritySeal string
}

func (p PayrollInput) Validate() error {
	for name, value := range map[string]string{"input_id": p.InputID, "worker_ref": p.WorkerRef, "period_ref": p.PeriodRef, "source_digest": p.SourceDigest, "source_kind": p.SourceKind, "approval_ref": p.ApprovalRef} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidPayroll, name)
		}
	}
	if err := p.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: amount: %v", ErrInvalidPayroll, err)
	}
	if p.SourceKind != "AWARD" || p.authoritySeal == "" || p.authoritySeal != p.computedAuthoritySeal() {
		return fmt.Errorf("%w: payroll authority is not minted from the approved award", ErrInvalidPayroll)
	}
	return nil
}

func (p PayrollInput) computedAuthoritySeal() string {
	return digestWriter(canonicalbytes.New("hcmnext.domains.incentive.PayrollInputAuthority", schemaVersion).
		String("input_id", p.InputID).String("worker_ref", p.WorkerRef).String("period_ref", p.PeriodRef).
		Value("amount", p.Amount).String("source_digest", p.SourceDigest).String("source_kind", p.SourceKind).String("approval_ref", p.ApprovalRef))
}

func PayrollInputForAward(a AwardCalculation, verifier ApprovalVerifier) (PayrollInput, error) {
	if err := a.Validate(); err != nil {
		return PayrollInput{}, err
	}
	if a.State != AwardApproved && a.State != AwardFinalized {
		return PayrollInput{}, fmt.Errorf("%w: award must be approved or finalized", ErrInvalidPayroll)
	}
	if a.CanonicalDigest == "" || a.Digest == "" {
		return PayrollInput{}, fmt.Errorf("%w: award must carry its minted canonical digest", ErrInvalidPayroll)
	}
	if verifier == nil {
		return PayrollInput{}, fmt.Errorf("%w: approval verifier is required", ErrInvalidPayroll)
	}
	if err := verifier.VerifyApproval(a.approvalClaim(), a.ApprovalRef); err != nil {
		return PayrollInput{}, fmt.Errorf("%w: approval receipt refused: %v", ErrInvalidPayroll, err)
	}
	m, err := values.NewMoneyFromDecimal(a.Amount, a.Currency)
	if err != nil {
		return PayrollInput{}, err
	}
	p := PayrollInput{InputID: "payroll:" + a.Digest, WorkerRef: a.WorkerRef, PeriodRef: a.PeriodRef, Amount: m, SourceDigest: a.Digest, SourceKind: "AWARD", ApprovalRef: a.ApprovalRef}
	p.authoritySeal = p.computedAuthoritySeal()
	if err := p.Validate(); err != nil {
		return PayrollInput{}, err
	}
	return p, nil
}

// ExternalMeasureObservation is provider evidence only; it cannot authorize
// or replace an internal attainment observation.
type ExternalMeasureObservation struct {
	SourceRef, Watermark, ObservationDigest string
	Value                                   values.Decimal
}

type ReconciliationStatus string

const (
	Reconciled                   ReconciliationStatus = "RECONCILED"
	ReconciliationRepairRequired ReconciliationStatus = "REPAIR_REQUIRED"
	ReconciliationUnknown        ReconciliationStatus = "UNKNOWN"
)

type MeasureReconciliation struct {
	Status                                                                  ReconciliationStatus
	InternalDigest, ExternalDigest, SourceRef, Watermark, RepairRef, Digest string
}

func ReconcileExternalMeasure(internal AttainmentObservation, external ExternalMeasureObservation) (MeasureReconciliation, error) {
	if err := internal.Validate(); err != nil {
		return MeasureReconciliation{}, fmt.Errorf("%w: internal: %v", ErrInvalidReconcile, err)
	}
	if strings.TrimSpace(internal.Digest) == "" {
		return MeasureReconciliation{}, fmt.Errorf("%w: internal digest is required", ErrInvalidReconcile)
	}
	if strings.TrimSpace(external.SourceRef) == "" || strings.TrimSpace(external.Watermark) == "" || strings.TrimSpace(external.ObservationDigest) == "" {
		return MeasureReconciliation{}, fmt.Errorf("%w: external provenance is required", ErrInvalidReconcile)
	}
	if err := external.Value.Validate(); err != nil {
		return MeasureReconciliation{}, fmt.Errorf("%w: external value: %v", ErrInvalidReconcile, err)
	}
	status := Reconciled
	repair := ""
	cmp := internal.Value.Cmp(external.Value)
	if cmp != 0 {
		status = ReconciliationRepairRequired
		repair = "repair:measure:" + internal.ObservationID
	}
	d := digestWriter(canonicalbytes.New("hcmnext.domains.incentive.MeasureReconciliation", schemaVersion).String("internal_digest", internal.Digest).String("external_digest", external.ObservationDigest).String("source_ref", external.SourceRef).String("watermark", external.Watermark).String("status", string(status)))
	return MeasureReconciliation{Status: status, InternalDigest: internal.Digest, ExternalDigest: external.ObservationDigest, SourceRef: external.SourceRef, Watermark: external.Watermark, RepairRef: repair, Digest: d}, nil
}
