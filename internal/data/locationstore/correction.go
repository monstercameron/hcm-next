package locationstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/location"
)

// CorrectWorkLocation plans a governed successor against the durable current
// head and appends that successor in the caller's transaction. Downstream
// outcomes are never mutated; the returned plan contains only reevaluation
// intents for those domains.
func (s Store) CorrectWorkLocation(ctx context.Context, ex Executor, tenantID uuid.UUID, req location.WorkLocationCorrectionRequest) (location.CorrectionPlan, error) {
	if tenantID == uuid.Nil {
		return location.CorrectionPlan{}, invalid("tenant_id", "tenant is required")
	}
	id := workLocationID(req.Current)
	current, err := s.LoadWorkLocation(ctx, ex, tenantID, id, req.Current.Revision)
	if err != nil {
		return location.CorrectionPlan{}, err
	}
	if current.CanonicalDigest != req.Current.CanonicalDigest {
		return location.CorrectionPlan{}, refusal(CodeVersionConflict, ErrVersionConflict, "correction current revision is stale")
	}
	req.Current = current
	plan, err := location.CorrectWorkLocation(req)
	if err != nil {
		return location.CorrectionPlan{}, fmt.Errorf("locationstore: plan correction: %w", err)
	}
	if _, err := s.PutWorkLocation(ctx, ex, tenantID, plan.Successor); err != nil {
		return location.CorrectionPlan{}, err
	}
	return plan, nil
}
