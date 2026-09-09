package artifacts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// SweepCandidates lists tenant's artifacts that are, as of asOf, both
// unreferenced (no owner's most recent reference event is ADD -- see
// [ReferenceCount]) and past the retention window their own retention_class
// declares (MODEL-026's [model.RetentionClass], resolved for jurisdiction
// from each artifact's own created_at as the retention trigger instant).
//
// SweepCandidates never deletes anything and this package implements no
// deletion path at all: it only reports which artifacts a later capability
// (DATA-016 REFACTOR, DATA-018) would be entitled to act on. An artifact
// whose retention_class is not a key of classes, or whose class cannot
// resolve a period for jurisdiction, is treated as not yet eligible rather
// than an error: a retention window this call cannot compute is never
// treated as already elapsed.
func SweepCandidates(ctx context.Context, q Querier, schema string, tenant uuid.UUID, jurisdiction string, classes map[string]model.RetentionClass, asOf time.Time) ([]Record, error) {
	if tenant == uuid.Nil {
		return nil, ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}

	table := pgx.Identifier{schema, "artifact"}.Sanitize()
	refTable := pgx.Identifier{schema, "artifact_reference_event"}.Sanitize()
	rows, err := q.Query(ctx, fmt.Sprintf(`
		SELECT %s FROM %s a
		WHERE a.tenant_id = $1
		AND NOT EXISTS (
			SELECT 1 FROM (
				SELECT DISTINCT ON (owner_kind, owner_id) action
				FROM %s e
				WHERE e.tenant_id = a.tenant_id AND e.content_id = a.content_id
				ORDER BY owner_kind, owner_id, recorded_at DESC, event_id DESC
			) latest
			WHERE latest.action = 'ADD'
		)
		ORDER BY a.content_id`, recordColumns, table, refTable), tenant)
	if err != nil {
		return nil, fmt.Errorf("artifacts: list unreferenced artifacts: %w", err)
	}
	defer rows.Close()

	var unreferenced []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("artifacts: scan unreferenced artifact: %w", err)
		}
		unreferenced = append(unreferenced, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artifacts: list unreferenced artifacts: %w", err)
	}

	var eligible []Record
	for _, rec := range unreferenced {
		class, ok := classes[rec.RetentionClass]
		if !ok {
			continue
		}
		schedule, err := class.EffectiveRetention(jurisdiction, values.NewInstant(rec.CreatedAt))
		if err != nil {
			continue
		}
		if !asOf.Before(schedule.NextActionAt.Time()) {
			eligible = append(eligible, rec)
		}
	}
	return eligible, nil
}
