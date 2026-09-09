package rebuild

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

// PromotionOutcomeRebuilder is the read-only, side-effect-free replay seam
// for workflow.promotion_outcome. It reads ledger rows only and delegates the
// semantic fold/digest to the projection package.
type PromotionOutcomeRebuilder struct {
	reader *ledger.Reader
}

// NewPromotionOutcomeRebuilder constructs a promotion-outcome replay service.
func NewPromotionOutcomeRebuilder(reader *ledger.Reader) *PromotionOutcomeRebuilder {
	if reader == nil {
		reader = ledger.NewReader()
	}
	return &PromotionOutcomeRebuilder{reader: reader}
}

// Replay reads the authoritative ledger stream and rebuilds the critical
// projection's semantic rows without writing a shadow table or dispatching an
// external effect.
func (r *PromotionOutcomeRebuilder) Replay(ctx context.Context, q dbport.Querier, tenant uuid.UUID, streamKey string) (projection.PromotionOutcomeReport, error) {
	if r == nil || r.reader == nil {
		return projection.PromotionOutcomeReport{}, fmt.Errorf("promotion outcome rebuild: reader is required")
	}
	events, err := r.reader.ReadStream(ctx, q, tenant, streamKey)
	if err != nil {
		return projection.PromotionOutcomeReport{}, fmt.Errorf("promotion outcome rebuild: read ledger: %w", err)
	}
	report, err := projection.RebuildPromotionOutcome(events, tenant, streamKey)
	if err != nil {
		return projection.PromotionOutcomeReport{}, err
	}
	return report, nil
}

// ComparePromotionOutcome is the named promotion gate: only a digest-for-
// digest match with the live semantic report is compatible.
func ComparePromotionOutcome(rebuilt, live projection.PromotionOutcomeReport) error {
	return projection.ComparePromotionOutcome(rebuilt, live)
}

// Version is the package contract version used by architecture tooling.
func Version() int { return 1 }

// Explain returns bounded replay metadata without row payloads.
func Explain() string {
	return fmt.Sprintf("rebuild v%d: workflow.promotion_outcome replays ledger rows and refuses the first differing row", Version())
}
