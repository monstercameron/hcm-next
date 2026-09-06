package productslice

import (
	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
	"github.com/monstercameron/hcm-next/tools/uxqual/widgetreg"
)

// PromotionSliceDefinition is ALIGN-001's projection proof: the admitted
// Gate B Promotion execution slice (planning/specs/
// default-product-slice-alignment.md "does not broaden ... the separately
// admitted Gate B Promotion execution slice") expressed as one
// ProductSliceDefinition. Pages and Widgets are read directly off the real
// pagedef/widgetreg packages so a rename there breaks this file's compile
// nowhere -- it breaks [ProductSliceDefinition.Validate] against a live
// [Registries] snapshot, and TestPromotionProductSliceRegistryNoDrift.
//
// BusinessIntents, Features, Capabilities, Packages and Todos name ids from
// definitions/governance/feature-intent-coverage.yaml, internal/capability,
// the live Phase 1 production graph (tools/policy/phaseonegate) and
// definitions/planning/todo-registry.json respectively. They are literal
// here, the same way tools/uxqual/pagedef/promotion.go and
// tools/uxqual/widgetreg.PromotionRegistry hard-code their own fixtures;
// [LoadLiveRegistries] plus Validate is what proves each literal still
// resolves against the real registry it names.
func PromotionSliceDefinition() ProductSliceDefinition {
	return ProductSliceDefinition{
		SliceID: "promotion",
		Version: 1,
		BusinessIntents: []string{
			"hcmnext.people.promote_worker/v1",
			"hcmnext.rewards.simulate_compensation/v1",
			"hcmnext.rewards.evaluate_pay_band_position/v1",
		},
		Features: []string{
			"promotion_execute",
			"promotion_request_submit",
			"proposal_simulate_and_validate",
			"change_request_submit_and_track",
			"payband_position_evaluate",
		},
		Pages: []string{
			pagedef.PromotionListPageDefinition().PageID,
			pagedef.PromotionDetailPageDefinition().PageID,
		},
		Widgets: widgetreg.PromotionRegistry().Refs(),
		Capabilities: []string{
			"hcmnext.people.promote_worker",
			"hcmnext.rewards.simulate_compensation",
			"hcmnext.rewards.evaluate_pay_band_position",
		},
		Packages: []string{
			"github.com/monstercameron/hcm-next/cmd/hcmnext",
			"github.com/monstercameron/hcm-next/internal/capability",
			"github.com/monstercameron/hcm-next/internal/humanwork/workspace",
			"github.com/monstercameron/hcm-next/internal/transport/journey",
			"github.com/monstercameron/hcm-next/tools/uxqual/render/journey",
		},
		Todos: []string{
			"WEB-001", "WEB-002", "WEB-003", "WEB-005",
			"BIND-001", "ARCH-GO-018", "INTENT-010",
			"PROMO-001", "PROMO-005", "PROMO-007", "PROMO-008", "PROMO-009",
			"PEOPLE-004", "UX-001", "UX-002", "UX-009",
		},
		Jurisdictions: []string{"US-ALL"},
		Personas:      []string{"manager", "compensation.approver", "hr.workforce.editor"},
		ExitCriteria: []string{
			"a permitted principal (manager or compensation.approver) discovers the Promotion workspace and a denied principal learns nothing about it",
			"the SSR page and the enhanced WASM/GWC page produce the same semantic state for promotion.journeys.list and promotion.journeys.detail",
			"ProposeJourney and ExecuteJourney invoke the canonical JourneyService RPCs with trusted context, expected version and idempotency",
			"DecideJourney records an approval-bound decision that names its ledger event",
			"the projection reads exposed to the two Promotion pages name their source version and freshness",
		},
	}
}
