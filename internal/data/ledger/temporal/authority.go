package temporal

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// authorityRow is one authority_assignment as read back for labelling.
type authorityRow struct {
	Ref           string
	Kind          string
	DomainScope   string
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
}

// covers reports whether the assignment's half-open business interval
// contains at. The interval is [EffectiveFrom, EffectiveTo), matching what
// internal/data/ledger.Appender.checkAuthority proved at append time: an
// assignment that ends exactly at the instant does not cover it.
func (r authorityRow) covers(at time.Time) bool {
	if at.Before(r.EffectiveFrom) {
		return false
	}
	return r.EffectiveTo == nil || at.Before(*r.EffectiveTo)
}

// loadAuthorityLabels resolves every distinct authority_ref cited by the
// given assertions in one query and writes the resolved label back onto
// each. An assertion citing no authority keeps its zero label; an assertion
// citing a ref this tenant has no assignment row for keeps the ref with
// Resolved false, so a dangling citation is visible rather than silently
// dropped.
//
// It is one query for the whole page rather than one per row: labelling is a
// presentation concern and must not turn a single-statement answer into an
// N+1 read.
func loadAuthorityLabels(ctx context.Context, q Querier, tenant uuid.UUID, assertions []Assertion) error {
	refs := distinctAuthorityRefs(assertions)
	if len(refs) == 0 {
		return nil
	}

	rows, err := q.Query(ctx, `
		SELECT authority_ref, authority_kind, domain_scope, effective_from, effective_to
		FROM authority_assignment
		WHERE tenant_id = $1 AND authority_ref = ANY($2::text[])`, tenant, refs)
	if err != nil {
		return fmt.Errorf("temporal: read authority assignments: %w", err)
	}
	defer rows.Close()

	byRef := make(map[string]authorityRow, len(refs))
	for rows.Next() {
		var r authorityRow
		if err := rows.Scan(&r.Ref, &r.Kind, &r.DomainScope, &r.EffectiveFrom, &r.EffectiveTo); err != nil {
			return fmt.Errorf("temporal: scan authority assignment: %w", err)
		}
		byRef[r.Ref] = r
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("temporal: read authority assignments: %w", err)
	}

	for i := range assertions {
		label := &assertions[i].Authority
		if !label.Present {
			continue
		}
		row, ok := byRef[label.Ref]
		if !ok {
			continue
		}
		label.Resolved = true
		label.Kind = row.Kind
		label.DomainScope = row.DomainScope
		label.EffectiveFrom = row.EffectiveFrom
		label.EffectiveTo = row.EffectiveTo
		label.CoversEffectiveAt = row.covers(assertions[i].EffectiveAt)
	}
	return nil
}

// distinctAuthorityRefs returns the sorted, deduplicated set of authority
// references the assertions cite. Sorting keeps the query parameter stable
// so two identical pages issue byte-identical statements.
func distinctAuthorityRefs(assertions []Assertion) []string {
	seen := make(map[string]struct{}, len(assertions))
	for _, a := range assertions {
		if a.Authority.Present && a.Authority.Ref != "" {
			seen[a.Authority.Ref] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}
