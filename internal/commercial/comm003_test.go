package commercial_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
)

// placeholderEconomicThresholds is a test fixture only. A human must supply
// and approve the production threshold record before Gate A evidence is used.
func placeholderEconomicThresholds() commercial.EconomicThresholds {
	return commercial.EconomicThresholds{
		SchemaVersion: 1, Version: "placeholder-thresholds-v1",
		MinEligibleTransactions: 5, MinAdoptionBPS: 6000, MaxBypassBPS: 3000,
		MinGrossMarginBPS: 5000, MaxCustomerLaborCents: 200000,
	}
}

func economicInput(t *testing.T) commercial.PilotEconomicInput {
	t.Helper()
	snapshot, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "tenant-a", ContractID: "pilot-a", Revision: 1,
		EffectiveFrom: pilotAt, EffectiveTo: pilotAt.Add(90 * 24 * time.Hour),
		Capabilities: []string{commercial.PromotionEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 25}, PriceCents: 2500000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := commercial.NewInvoiceEvidenceStore()
	invoice, _, err := store.Record(snapshot, invoiceRequest("economic-invoice-artifact-placeholder"))
	if err != nil {
		t.Fatal(err)
	}
	return commercial.PilotEconomicInput{
		TenantID: "tenant-a", PilotRef: "pilot-a-placeholder",
		IntervalFrom: pilotAt, IntervalTo: pilotAt.Add(90 * 24 * time.Hour), Invoice: invoice,
		PriceCents: 2500000, Currency: "USD", CustomerLaborCents: 100000,
		ProviderCostCents: 300000, InfrastructureCents: 200000,
		Adoption: commercial.PaidUseEvidence{
			SchemaVersion: 1, AuthorizedActorRef: "actor:customer-placeholder",
			EligibleTransactionEvidenceRef: "evidence:eligible-transactions-placeholder",
			WorkflowStageEvidenceRef:       "evidence:workflow-stage-placeholder",
			OutcomeEvidenceRef:             "evidence:outcome-placeholder",
			EligibleTransactions:           10, AuthorizedCustomerUses: 8,
			CompletedWorkflowUses: 7, OutcomeLinkedUses: 7,
		},
		Bypass: commercial.BypassEvidence{SchemaVersion: 1, Manual: 1, Incumbent: 1},
	}
}

func TestTodo_COMM_003(t *testing.T) {
	input := economicInput(t)
	thresholds := placeholderEconomicThresholds()
	got, err := commercial.CompilePilotEconomicEvidence(input, thresholds)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != commercial.DecisionProceed || got.AdoptionBPS != 7000 || got.BypassBPS != 2000 || got.GrossMarginBPS != 8000 {
		t.Fatalf("evidence=%+v, want placeholder fixture to proceed", got)
	}
	for name, mutate := range map[string]func(*commercial.PilotEconomicInput, *commercial.EconomicThresholds){
		"insufficient_sample": func(i *commercial.PilotEconomicInput, _ *commercial.EconomicThresholds) {
			i.Adoption.EligibleTransactions = 2
			i.Adoption.AuthorizedCustomerUses = 2
			i.Adoption.CompletedWorkflowUses = 2
			i.Adoption.OutcomeLinkedUses = 2
			i.Bypass = commercial.BypassEvidence{SchemaVersion: 1}
		},
		"negative_unit_economics": func(i *commercial.PilotEconomicInput, _ *commercial.EconomicThresholds) {
			i.ProviderCostCents = 2500000
			i.InfrastructureCents = 1
		},
		"high_customer_labor": func(i *commercial.PilotEconomicInput, _ *commercial.EconomicThresholds) {
			i.CustomerLaborCents = 200001
		},
		"low_adoption": func(i *commercial.PilotEconomicInput, _ *commercial.EconomicThresholds) {
			i.Adoption.CompletedWorkflowUses = 5
			i.Adoption.OutcomeLinkedUses = 5
		},
		"high_bypass": func(i *commercial.PilotEconomicInput, _ *commercial.EconomicThresholds) {
			i.Bypass = commercial.BypassEvidence{SchemaVersion: 1, Manual: 4}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := input
			candidate.Adoption = input.Adoption
			candidate.Bypass = input.Bypass
			candidateThresholds := thresholds
			mutate(&candidate, &candidateThresholds)
			got, err := commercial.CompilePilotEconomicEvidence(candidate, candidateThresholds)
			if err != nil {
				t.Fatal(err)
			}
			want := commercial.DecisionReselect
			if name == "negative_unit_economics" {
				want = commercial.DecisionStop
			}
			if got.Decision != want {
				t.Fatalf("decision=%q, want %q", got.Decision, want)
			}
		})
	}
}

func TestTodo_COMM_003_Property(t *testing.T) {
	input := economicInput(t)
	thresholds := placeholderEconomicThresholds()
	first, err := commercial.CompilePilotEconomicEvidence(input, thresholds)
	if err != nil {
		t.Fatal(err)
	}
	second, err := commercial.CompilePilotEconomicEvidence(input, thresholds)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.InputDigest == "" || first.ThresholdDigest == "" {
		t.Fatalf("non-deterministic evidence first=%+v second=%+v", first, second)
	}
	if _, err := commercial.ParseEconomicThresholds([]byte(`{"schema_version":1,"version":"placeholder","min_eligible_transactions":"five","min_adoption_bps":1,"max_bypass_bps":1,"min_gross_margin_bps":1,"max_customer_labor_cents":1}`)); !errors.Is(err, commercial.ErrInvalidEconomicThresholds) {
		t.Fatalf("nonnumeric threshold error=%v", err)
	}
	invalid := thresholds
	invalid.MaxBypassBPS = 10001
	if _, err := commercial.CompilePilotEconomicEvidence(input, invalid); !errors.Is(err, commercial.ErrInvalidEconomicThresholds) {
		t.Fatalf("invalid threshold error=%v", err)
	}
}

func TestTodo_COMM_003_Integration(t *testing.T) {
	input := economicInput(t)
	got, err := commercial.CompilePilotEconomicEvidence(input, placeholderEconomicThresholds())
	if err != nil {
		t.Fatal(err)
	}
	view := got.CustomerView(input)
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, confidential := range []string{"customer_labor_cents", "provider_cost_cents", "infrastructure_cost_cents", "gross_margin_bps", "threshold_digest"} {
		if strings.Contains(text, confidential) {
			t.Fatalf("customer view exposed confidential field %q: %s", confidential, text)
		}
	}
	if view.Decision != commercial.DecisionProceed || view.AdoptionBPS != got.AdoptionBPS || view.BypassBPS != got.BypassBPS {
		t.Fatalf("customer view=%+v, evidence=%+v", view, got)
	}
	input.IntervalTo = input.IntervalTo.Add(time.Hour)
	if _, err := commercial.CompilePilotEconomicEvidence(input, placeholderEconomicThresholds()); !errors.Is(err, commercial.ErrInvalidEconomicEvidence) {
		t.Fatalf("unreconciled interval error=%v", err)
	}
}
