package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Query runs one tenant-scoped lexical search and returns the subjects the
// caller may be told matched, ranked and deduplicated, and nothing wider.
//
// Three refusals run before, and independently of, the full-text match
// itself:
//
//   - No [RequiredScope] in in.Authorization: [ErrScopeDenied], no statement
//     runs at all.
//   - No [Discloser] supplied: [ErrNoDiscloser], for the same reason -- a
//     query that could not test disclosure would have to return every match
//     unfiltered.
//   - Blank query text: [ErrEmptyQueryText].
//
// tenantID scopes the physical row-level-security read; in.Tenant is the
// logical [values.TenantId] every returned [Result.Subject] is minted under.
// Every candidate hit is then passed through in.Discloser exactly once
// before it is added to the result: a subject the discloser refuses, or
// errors on, is silently absent from the returned slice. Query never returns
// a field value or a snippet of search_text -- only the subject reference,
// its source revision and its rank -- so the only thing a caller learns
// about a withheld subject is nothing at all.
func Query(ctx context.Context, ex Executor, tenantID uuid.UUID, in QueryInput) ([]Result, error) {
	if !in.Authorization.ScopeGranted {
		reason := in.Authorization.Reason
		if reason == "" {
			reason = "authorization does not grant " + RequiredScope
		}
		return nil, fmt.Errorf("%w: %s", ErrScopeDenied, reason)
	}
	if in.Discloser == nil {
		return nil, ErrNoDiscloser
	}
	if err := in.Tenant.Validate(); err != nil {
		return nil, fmt.Errorf("search: query tenant: %w", err)
	}
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("search: query has no physical tenant id")
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, ErrEmptyQueryText
	}
	if in.Kind != "" {
		if err := in.Kind.Validate(); err != nil {
			return nil, err
		}
	}

	limit := in.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	hits, err := (Store{}).search(ctx, ex, tenantID, string(in.Kind), text, limit)
	if err != nil {
		return nil, err
	}

	out := make([]Result, 0, len(hits))
	for _, h := range hits {
		var subject values.EntityRef
		if err := subject.UnmarshalText([]byte(h.SubjectRef)); err != nil {
			return nil, fmt.Errorf("search: projected subject ref %q does not decode: %w", h.SubjectRef, err)
		}
		if subject.Tenant != in.Tenant {
			// A row-level-security escape would be a far more serious bug
			// than a bad decode, so it is refused rather than silently
			// dropped: the caller learns the query could not be trusted,
			// not that it matched nothing.
			return nil, fmt.Errorf("search: projected subject %s is outside queried tenant %s", subject, in.Tenant)
		}
		disclosable, err := in.Discloser.Disclosable(ctx, subject)
		if err != nil {
			// Fail closed: an error deciding disclosure is treated as
			// non-disclosure, never as a reason to include the subject.
			continue
		}
		if !disclosable {
			continue
		}
		out = append(out, Result{
			Subject:        subject,
			Kind:           EntityKind(h.SubjectKind),
			SourceRevision: h.SourceRevision,
			ProjectedAt:    h.ProjectedAt,
			Rank:           h.Rank,
		})
	}
	return out, nil
}
