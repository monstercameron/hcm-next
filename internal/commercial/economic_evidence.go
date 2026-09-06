package commercial

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// EconomicDecision is the closed Gate A commercial vocabulary. A compiler
// never returns an untyped or qualitative outcome.
type EconomicDecision string

const (
	DecisionProceed  EconomicDecision = "PROCEED"
	DecisionReselect EconomicDecision = "RESELECT"
	DecisionStop     EconomicDecision = "STOP"
)

var (
	ErrInvalidEconomicThresholds = errors.New("commercial: invalid economic thresholds")
	ErrInvalidEconomicEvidence   = errors.New("commercial: invalid economic evidence")
)

// EconomicThresholds is a human-supplied, versioned decision record. The
// repository deliberately does not provide production defaults: a product or
// design-partner owner must select these values and record the version.
type EconomicThresholds struct {
	SchemaVersion           int    `json:"schema_version"`
	Version                 string `json:"version"`
	MinEligibleTransactions int64  `json:"min_eligible_transactions"`
	MinAdoptionBPS          int64  `json:"min_adoption_bps"`
	MaxBypassBPS            int64  `json:"max_bypass_bps"`
	MinGrossMarginBPS       int64  `json:"min_gross_margin_bps"`
	MaxCustomerLaborCents   int64  `json:"max_customer_labor_cents"`
}

func (t EconomicThresholds) Validate() error {
	if t.SchemaVersion != 1 || !validReference(t.Version) {
		return fmt.Errorf("%w: schema or version", ErrInvalidEconomicThresholds)
	}
	if t.MinEligibleTransactions <= 0 || t.MinAdoptionBPS < 0 || t.MinAdoptionBPS > 10000 || t.MaxBypassBPS < 0 || t.MaxBypassBPS > 10000 || t.MinGrossMarginBPS < 0 || t.MinGrossMarginBPS > 10000 || t.MaxCustomerLaborCents < 0 {
		return fmt.Errorf("%w: numeric bounds", ErrInvalidEconomicThresholds)
	}
	return nil
}

// ParseEconomicThresholds rejects malformed or nonnumeric threshold values
// before they can reach the typed evaluator.
func ParseEconomicThresholds(data []byte) (EconomicThresholds, error) {
	var thresholds EconomicThresholds
	if err := json.Unmarshal(data, &thresholds); err != nil {
		return EconomicThresholds{}, fmt.Errorf("%w: %v", ErrInvalidEconomicThresholds, err)
	}
	if err := thresholds.Validate(); err != nil {
		return EconomicThresholds{}, err
	}
	return thresholds, nil
}

func (t EconomicThresholds) digest() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("%w: encode thresholds: %v", ErrInvalidEconomicThresholds, err)
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

// PaidUseEvidence is the minimal, redaction-safe part of WEDGE-013 consumed
// by commercial evidence: references identify the authorized customer actor,
// eligible transaction, completed workflow stage, and linked outcome without
// copying actor or worker payloads into this package.
type PaidUseEvidence struct {
	SchemaVersion                  int    `json:"schema_version"`
	AuthorizedActorRef             string `json:"authorized_actor_ref"`
	EligibleTransactionEvidenceRef string `json:"eligible_transaction_evidence_ref"`
	WorkflowStageEvidenceRef       string `json:"workflow_stage_evidence_ref"`
	OutcomeEvidenceRef             string `json:"outcome_evidence_ref"`
	EligibleTransactions           int64  `json:"eligible_transactions"`
	AuthorizedCustomerUses         int64  `json:"authorized_customer_uses"`
	CompletedWorkflowUses          int64  `json:"completed_workflow_uses"`
	OutcomeLinkedUses              int64  `json:"outcome_linked_uses"`
}

func (p PaidUseEvidence) Validate() error {
	if p.SchemaVersion != 1 || !validReference(p.AuthorizedActorRef) || !validReference(p.EligibleTransactionEvidenceRef) || !validReference(p.WorkflowStageEvidenceRef) || !validReference(p.OutcomeEvidenceRef) {
		return fmt.Errorf("%w: paid-use references", ErrInvalidEconomicEvidence)
	}
	if p.EligibleTransactions < 0 || p.AuthorizedCustomerUses < 0 || p.CompletedWorkflowUses < 0 || p.OutcomeLinkedUses < 0 || p.AuthorizedCustomerUses > p.EligibleTransactions || p.CompletedWorkflowUses > p.AuthorizedCustomerUses || p.OutcomeLinkedUses > p.CompletedWorkflowUses {
		return fmt.Errorf("%w: paid-use counts", ErrInvalidEconomicEvidence)
	}
	return nil
}

// BypassEvidence is the versioned taxonomy required to keep manual,
// incumbent, spreadsheet, emergency, and unexplained paths in the denominator.
type BypassEvidence struct {
	SchemaVersion int   `json:"schema_version"`
	Manual        int64 `json:"manual"`
	Incumbent     int64 `json:"incumbent"`
	Spreadsheet   int64 `json:"spreadsheet"`
	Emergency     int64 `json:"emergency"`
	Unexplained   int64 `json:"unexplained"`
}

func (b BypassEvidence) Validate(eligible int64) error {
	if b.SchemaVersion != 1 || b.Manual < 0 || b.Incumbent < 0 || b.Spreadsheet < 0 || b.Emergency < 0 || b.Unexplained < 0 {
		return fmt.Errorf("%w: bypass taxonomy", ErrInvalidEconomicEvidence)
	}
	total, ok := sumCounts(b.Manual, b.Incumbent, b.Spreadsheet, b.Emergency, b.Unexplained)
	if !ok || total > eligible {
		return fmt.Errorf("%w: bypass denominator", ErrInvalidEconomicEvidence)
	}
	return nil
}

func (b BypassEvidence) total() int64 {
	total, _ := sumCounts(b.Manual, b.Incumbent, b.Spreadsheet, b.Emergency, b.Unexplained)
	return total
}

// PilotEconomicInput is an immutable compilation input assembled from the
// COMM-002 invoice receipt, WEDGE-013 paid-use evidence, and the human-owned
// cost observations for one pilot interval.
type PilotEconomicInput struct {
	TenantID            string          `json:"tenant_id"`
	PilotRef            string          `json:"pilot_ref"`
	IntervalFrom        time.Time       `json:"interval_from"`
	IntervalTo          time.Time       `json:"interval_to"`
	Invoice             InvoiceEvidence `json:"invoice"`
	PriceCents          int64           `json:"price_cents"`
	Currency            string          `json:"currency"`
	CustomerLaborCents  int64           `json:"customer_labor_cents"`
	ProviderCostCents   int64           `json:"provider_cost_cents"`
	InfrastructureCents int64           `json:"infrastructure_cost_cents"`
	Adoption            PaidUseEvidence `json:"adoption"`
	Bypass              BypassEvidence  `json:"bypass"`
}

func (i PilotEconomicInput) Validate() error {
	if !validReference(i.TenantID) || !validReference(i.PilotRef) || i.IntervalFrom.IsZero() || i.IntervalTo.IsZero() || !i.IntervalTo.After(i.IntervalFrom) {
		return fmt.Errorf("%w: pilot identity or interval", ErrInvalidEconomicEvidence)
	}
	if err := i.Invoice.Validate(); err != nil {
		return err
	}
	if i.Invoice.TenantID != i.TenantID || i.Invoice.Status == InvoiceVoided || i.Invoice.Reconciliation != ReconciliationReconciled || !i.Invoice.ServiceFrom.Equal(i.IntervalFrom.UTC()) || !i.Invoice.ServiceTo.Equal(i.IntervalTo.UTC()) {
		return fmt.Errorf("%w: invoice does not reconcile pilot interval", ErrInvalidEconomicEvidence)
	}
	if i.PriceCents <= 0 || i.Currency == "" || i.Invoice.AmountCents != i.PriceCents || i.Invoice.Currency != i.Currency {
		return fmt.Errorf("%w: price does not reconcile invoice", ErrInvalidEconomicEvidence)
	}
	if i.CustomerLaborCents < 0 || i.ProviderCostCents < 0 || i.InfrastructureCents < 0 {
		return fmt.Errorf("%w: costs", ErrInvalidEconomicEvidence)
	}
	if _, ok := sumCounts(i.ProviderCostCents, i.InfrastructureCents); !ok {
		return fmt.Errorf("%w: cost overflow", ErrInvalidEconomicEvidence)
	}
	if err := i.Adoption.Validate(); err != nil {
		return err
	}
	if err := i.Bypass.Validate(i.Adoption.EligibleTransactions); err != nil {
		return err
	}
	return nil
}

// PilotEconomicEvidence is the deterministic, auditable output of the
// economic compiler. Costs remain in this internal evidence record and are
// intentionally absent from CustomerView.
type PilotEconomicEvidence struct {
	SchemaVersion       int              `json:"schema_version"`
	ThresholdVersion    string           `json:"threshold_version"`
	ThresholdDigest     string           `json:"threshold_digest"`
	InputDigest         string           `json:"input_digest"`
	TenantID            string           `json:"tenant_id"`
	PilotRef            string           `json:"pilot_ref"`
	IntervalFrom        time.Time        `json:"interval_from"`
	IntervalTo          time.Time        `json:"interval_to"`
	InvoiceRecordID     string           `json:"invoice_record_id"`
	PriceCents          int64            `json:"price_cents"`
	Currency            string           `json:"currency"`
	CustomerLaborCents  int64            `json:"customer_labor_cents"`
	ProviderCostCents   int64            `json:"provider_cost_cents"`
	InfrastructureCents int64            `json:"infrastructure_cost_cents"`
	AdoptionBPS         int64            `json:"adoption_bps"`
	BypassBPS           int64            `json:"bypass_bps"`
	GrossMarginBPS      int64            `json:"gross_margin_bps"`
	Decision            EconomicDecision `json:"decision"`
	DecisionReason      string           `json:"decision_reason"`
}

// CustomerEconomicView contains decision and outcome evidence only. Provider,
// infrastructure, labor and gross-margin amounts are confidential fields and
// cannot be serialized through this projection.
type CustomerEconomicView struct {
	SchemaVersion         int              `json:"schema_version"`
	Decision              EconomicDecision `json:"decision"`
	TenantID              string           `json:"tenant_id"`
	PilotRef              string           `json:"pilot_ref"`
	IntervalFrom          time.Time        `json:"interval_from"`
	IntervalTo            time.Time        `json:"interval_to"`
	PriceCents            int64            `json:"price_cents"`
	Currency              string           `json:"currency"`
	EligibleTransactions  int64            `json:"eligible_transactions"`
	CompletedWorkflowUses int64            `json:"completed_workflow_uses"`
	AdoptionBPS           int64            `json:"adoption_bps"`
	BypassBPS             int64            `json:"bypass_bps"`
}

func (e PilotEconomicEvidence) CustomerView(input PilotEconomicInput) CustomerEconomicView {
	return CustomerEconomicView{
		SchemaVersion:         e.SchemaVersion,
		Decision:              e.Decision,
		TenantID:              e.TenantID,
		PilotRef:              e.PilotRef,
		IntervalFrom:          e.IntervalFrom,
		IntervalTo:            e.IntervalTo,
		PriceCents:            e.PriceCents,
		Currency:              e.Currency,
		EligibleTransactions:  input.Adoption.EligibleTransactions,
		CompletedWorkflowUses: input.Adoption.CompletedWorkflowUses,
		AdoptionBPS:           e.AdoptionBPS,
		BypassBPS:             e.BypassBPS,
	}
}

// CompilePilotEconomicEvidence deterministically evaluates one reconciled
// pilot interval against one versioned threshold record.
func CompilePilotEconomicEvidence(input PilotEconomicInput, thresholds EconomicThresholds) (PilotEconomicEvidence, error) {
	if err := thresholds.Validate(); err != nil {
		return PilotEconomicEvidence{}, err
	}
	if err := input.Validate(); err != nil {
		return PilotEconomicEvidence{}, err
	}
	thresholdDigest, err := thresholds.digest()
	if err != nil {
		return PilotEconomicEvidence{}, err
	}
	adoptionBPS := ratioBPS(input.Adoption.CompletedWorkflowUses, input.Adoption.EligibleTransactions)
	bypassBPS := ratioBPS(input.Bypass.total(), input.Adoption.EligibleTransactions)
	costs, _ := sumCounts(input.ProviderCostCents, input.InfrastructureCents)
	grossMarginBPS := marginBPS(input.PriceCents, costs)
	decision := DecisionProceed
	reason := "all versioned thresholds met"
	switch {
	case input.Adoption.EligibleTransactions < thresholds.MinEligibleTransactions:
		decision, reason = DecisionReselect, "eligible sample is below the threshold"
	case grossMarginBPS < thresholds.MinGrossMarginBPS:
		decision, reason = DecisionStop, "gross margin is below the threshold"
	case input.CustomerLaborCents > thresholds.MaxCustomerLaborCents:
		decision, reason = DecisionReselect, "customer labor is above the threshold"
	case adoptionBPS < thresholds.MinAdoptionBPS:
		decision, reason = DecisionReselect, "adoption is below the threshold"
	case bypassBPS > thresholds.MaxBypassBPS:
		decision, reason = DecisionReselect, "bypass rate is above the threshold"
	}
	inputDigest, err := economicInputDigest(input, thresholds)
	if err != nil {
		return PilotEconomicEvidence{}, err
	}
	return PilotEconomicEvidence{
		SchemaVersion: 1, ThresholdVersion: thresholds.Version, ThresholdDigest: thresholdDigest,
		InputDigest: inputDigest, TenantID: input.TenantID, PilotRef: input.PilotRef,
		IntervalFrom: input.IntervalFrom.UTC(), IntervalTo: input.IntervalTo.UTC(),
		InvoiceRecordID: input.Invoice.RecordID, PriceCents: input.PriceCents, Currency: input.Currency,
		CustomerLaborCents: input.CustomerLaborCents, ProviderCostCents: input.ProviderCostCents,
		InfrastructureCents: input.InfrastructureCents, AdoptionBPS: adoptionBPS, BypassBPS: bypassBPS,
		GrossMarginBPS: grossMarginBPS, Decision: decision, DecisionReason: reason,
	}, nil
}

func economicInputDigest(input PilotEconomicInput, thresholds EconomicThresholds) (string, error) {
	b, err := json.Marshal(struct {
		Input      PilotEconomicInput `json:"input"`
		Thresholds EconomicThresholds `json:"thresholds"`
	}{input, thresholds})
	if err != nil {
		return "", fmt.Errorf("%w: encode input: %v", ErrInvalidEconomicEvidence, err)
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

func ratioBPS(numerator, denominator int64) int64 {
	if denominator == 0 {
		return 0
	}
	return bigRatio(numerator, denominator, 10000)
}

func marginBPS(price, costs int64) int64 {
	if costs >= price {
		return -10000
	}
	return bigRatio(price-costs, price, 10000)
}

func bigRatio(numerator, denominator, scale int64) int64 {
	value := new(big.Int).Mul(big.NewInt(numerator), big.NewInt(scale))
	value.Quo(value, big.NewInt(denominator))
	return value.Int64()
}

func sumCounts(values ...int64) (int64, bool) {
	var total int64
	for _, value := range values {
		if value < 0 || total > int64(^uint64(0)>>1)-value {
			return 0, false
		}
		total += value
	}
	return total, true
}

func (e PilotEconomicEvidence) Explain() string {
	return fmt.Sprintf("pilot economic evidence tenant=%s pilot=%s interval=[%s,%s) decision=%s adoption_bps=%d bypass_bps=%d input_digest=%s threshold_version=%s",
		e.TenantID, e.PilotRef, e.IntervalFrom.UTC().Format(time.RFC3339Nano), e.IntervalTo.UTC().Format(time.RFC3339Nano), e.Decision, e.AdoptionBPS, e.BypassBPS, e.InputDigest, e.ThresholdVersion)
}
