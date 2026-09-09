package definitions

import "github.com/monstercameron/human-capital-management-suite/internal/intent"

// Negative-state policy references. Definitions reference a policy by id;
// common policies are shared, never copied into each definition.
const (
	PolicyAnalyticalRead    = "hcmnext.negative_state.analytical_read/v1"
	PolicyPureCalculation   = "hcmnext.negative_state.pure_calculation/v1"
	PolicyChangeTransaction = "hcmnext.negative_state.change_transaction/v1"
)

const (
	oneHour = 3600
	oneDay  = 86400
)

// Policies returns the shared negative-state policies the catalog references.
//
// The three policies differ where the answer genuinely differs. An analytical
// read may return a partial answer with explicit gaps; a pure calculation may
// not, because a calculation that quietly drops an input is wrong rather than
// incomplete; and a change transaction blocks on anything that would let a
// write execute under an uncertain fact.
func Policies() []intent.NegativeStatePolicy {
	return []intent.NegativeStatePolicy{
		analyticalReadPolicy(),
		pureCalculationPolicy(),
		changeTransactionPolicy(),
	}
}

func analyticalReadPolicy() intent.NegativeStatePolicy {
	return intent.NegativeStatePolicy{
		ID:      "hcmnext.negative_state.analytical_read",
		Version: 1,
		Rules: map[intent.NegativeState]intent.NegativeStateRule{
			intent.NegativeUnknown: {
				Action:          intent.ActionAllowWithWarning,
				EvidenceRef:     "evidence.explanation_gap/v1",
				AuthorityRef:    "authority.analytical_read/v1",
				ExpirySeconds:   oneHour,
				RevalidationRef: "revalidate.authorization_at_each_edge_traversal/v1",
			},
			intent.NegativePartial: {
				Action:          intent.ActionAllowWithWarning,
				EvidenceRef:     "evidence.explanation_gap/v1",
				AuthorityRef:    "authority.analytical_read/v1",
				ExpirySeconds:   oneHour,
				RevalidationRef: "revalidate.authorization_at_each_edge_traversal/v1",
			},
			intent.NegativeDegraded: {
				Action:          intent.ActionDegrade,
				EvidenceRef:     "evidence.degraded_source/v1",
				AuthorityRef:    "authority.analytical_read/v1",
				ExpirySeconds:   oneHour,
				RevalidationRef: "revalidate.source_freshness/v1",
			},
			intent.NegativeAmbiguous: {
				Action:          intent.ActionRouteHuman,
				EvidenceRef:     "evidence.ambiguous_resolution/v1",
				AuthorityRef:    "authority.identity_resolution_review/v1",
				ExpirySeconds:   oneDay,
				RevalidationRef: "revalidate.identity_resolution/v1",
			},
			intent.NegativeRedacted: {
				Action:          intent.ActionAllowWithWarning,
				EvidenceRef:     "evidence.redaction_decision/v1",
				AuthorityRef:    "authority.classification_read/v1",
				ExpirySeconds:   oneHour,
				RevalidationRef: "revalidate.classification_labels/v1",
			},
			intent.NegativeUnavailable: {
				Action:          intent.ActionAllowWithWarning,
				EvidenceRef:     "evidence.source_unavailable/v1",
				AuthorityRef:    "authority.analytical_read/v1",
				ExpirySeconds:   oneHour,
				RevalidationRef: "revalidate.source_availability/v1",
			},
			intent.NegativeStale: {
				Action:          intent.ActionUseStale,
				EvidenceRef:     "evidence.staleness_watermark/v1",
				AuthorityRef:    "authority.analytical_read/v1",
				ExpirySeconds:   oneHour,
				RevalidationRef: "revalidate.source_freshness/v1",
			},
		},
	}
}

func pureCalculationPolicy() intent.NegativeStatePolicy {
	return intent.NegativeStatePolicy{
		ID:      "hcmnext.negative_state.pure_calculation",
		Version: 1,
		Rules: map[intent.NegativeState]intent.NegativeStateRule{
			intent.NegativeUnknown: {
				Action:       intent.ActionBlock,
				EvidenceRef:  "evidence.missing_calculation_input/v1",
				AuthorityRef: "authority.calculation/v1",
			},
			intent.NegativePartial: {
				Action:       intent.ActionBlock,
				EvidenceRef:  "evidence.missing_calculation_input/v1",
				AuthorityRef: "authority.calculation/v1",
			},
			intent.NegativeDegraded: {
				Action:          intent.ActionAllowWithWarning,
				EvidenceRef:     "evidence.degraded_source/v1",
				AuthorityRef:    "authority.calculation/v1",
				ExpirySeconds:   oneHour,
				RevalidationRef: "revalidate.recalculate_when_material_input_changes/v1",
			},
			intent.NegativeAmbiguous: {
				Action:       intent.ActionBlock,
				EvidenceRef:  "evidence.ambiguous_calculation_input/v1",
				AuthorityRef: "authority.calculation/v1",
			},
			intent.NegativeRedacted: {
				Action:       intent.ActionBlock,
				EvidenceRef:  "evidence.redaction_decision/v1",
				AuthorityRef: "authority.classification_read/v1",
			},
			intent.NegativeUnavailable: {
				Action:          intent.ActionRouteHuman,
				EvidenceRef:     "evidence.source_unavailable/v1",
				AuthorityRef:    "authority.calculation/v1",
				ExpirySeconds:   oneDay,
				RevalidationRef: "revalidate.source_availability/v1",
			},
			intent.NegativeStale: {
				Action:          intent.ActionUseStale,
				EvidenceRef:     "evidence.staleness_watermark/v1",
				AuthorityRef:    "authority.calculation/v1",
				ExpirySeconds:   oneHour,
				RevalidationRef: "revalidate.recalculate_when_material_input_changes/v1",
			},
		},
	}
}

func changeTransactionPolicy() intent.NegativeStatePolicy {
	return intent.NegativeStatePolicy{
		ID:      "hcmnext.negative_state.change_transaction",
		Version: 1,
		Rules: map[intent.NegativeState]intent.NegativeStateRule{
			intent.NegativeUnknown: {
				Action:       intent.ActionBlock,
				EvidenceRef:  "evidence.unknown_baseline_fact/v1",
				AuthorityRef: "authority.change_request/v1",
			},
			intent.NegativePartial: {
				Action:          intent.ActionRouteHuman,
				EvidenceRef:     "evidence.partial_baseline/v1",
				AuthorityRef:    "authority.change_request_review/v1",
				ExpirySeconds:   oneDay,
				RevalidationRef: "revalidate.baseline_completeness/v1",
			},
			intent.NegativeDegraded: {
				Action:          intent.ActionRouteHuman,
				EvidenceRef:     "evidence.degraded_source/v1",
				AuthorityRef:    "authority.change_request_review/v1",
				ExpirySeconds:   oneDay,
				RevalidationRef: "revalidate.source_freshness/v1",
			},
			intent.NegativeAmbiguous: {
				Action:       intent.ActionBlock,
				EvidenceRef:  "evidence.ambiguous_resolution/v1",
				AuthorityRef: "authority.identity_resolution_review/v1",
			},
			intent.NegativeRedacted: {
				Action:       intent.ActionBlock,
				EvidenceRef:  "evidence.redaction_decision/v1",
				AuthorityRef: "authority.classification_read/v1",
			},
			intent.NegativeUnavailable: {
				Action:          intent.ActionCreateObligation,
				EvidenceRef:     "evidence.source_unavailable/v1",
				AuthorityRef:    "authority.change_request_review/v1",
				ExpirySeconds:   oneDay,
				RevalidationRef: "revalidate.source_availability/v1",
			},
			intent.NegativeStale: {
				Action:       intent.ActionBlock,
				EvidenceRef:  "evidence.staleness_watermark/v1",
				AuthorityRef: "authority.change_request/v1",
			},
		},
	}
}
