package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Projection is one subject's search row as [Project] left it.
type Projection struct {
	Subject values.EntityRef
	Kind    EntityKind
	// SearchText is exposed so a caller (and this package's own tests) can
	// prove exactly which values entered the projection -- and, just as
	// importantly, which did not.
	SearchText     string
	SourceRevision string
}

// Project (re)builds one subject's search_projection row from its source
// facts and records one search_projection_event evidence row for the
// revision it was projected from.
//
// tenantID is the physical tenant uuid the row-level-security session is
// already scoped to; in.Tenant is the logical [values.TenantId] the subject
// reference is validated against. Splitting them is the same seam
// internal/data/workforce.Facts draws between its TenantUUID resolver and
// people.FactQuery.Tenant: this package never resolves one from the other,
// because doing so would make it a second place a tenant mapping could
// drift.
//
// Project is a pure function of in.Fields restricted to
// [ClassificationCleared] fields, plus in.Revision: the same cleared values
// at the same revision always produce the same [Projection.SearchText],
// regardless of how many times Project is called or what wall-clock time it
// is called at. That is the rebuild proof RETRIEVAL-001 requires -- a
// caller that reprojects from scratch after a failure, or replays an older
// revision to audit it, gets back the exact projection that revision always
// produces, never one that depends on projection history.
func Project(ctx context.Context, ex Executor, tenantID uuid.UUID, in ProjectionInput) (Projection, error) {
	if tenantID == uuid.Nil {
		return Projection{}, fmt.Errorf("%w: no physical tenant id", ErrInvalidProjectionInput)
	}
	if err := in.Validate(); err != nil {
		return Projection{}, err
	}

	text := BuildSearchText(in.Fields)
	stored, err := (Store{}).upsert(ctx, ex, row{
		TenantID:       tenantID,
		SubjectKind:    string(in.Kind),
		SubjectRef:     in.Subject.String(),
		SearchText:     text,
		SourceRevision: in.Revision.String(),
	})
	if err != nil {
		return Projection{}, err
	}
	return Projection{
		Subject:        in.Subject,
		Kind:           EntityKind(stored.SubjectKind),
		SearchText:     stored.SearchText,
		SourceRevision: stored.SourceRevision,
	}, nil
}

// BuildSearchText deterministically renders the search text [Project]
// stores: exactly the [ClassificationCleared] fields present in fields,
// walked in [ClearedFields]'s fixed order (never Go's unordered map
// iteration) and joined by a single space. A field fields does not carry, an
// empty value, or a field this package has not declared cleared are all
// silently absent from the result -- never a placeholder, never the raw
// value slipping through unexamined.
func BuildSearchText(fields map[people.FieldID]string) string {
	parts := make([]string, 0, len(fields))
	for _, field := range ClearedFields() {
		value, ok := fields[field]
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, " ")
}
