package search

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// RequiredScope is the search-specific authorization scope token a caller's
// [Authorization] must carry for [Query] to run at all. It names a scope the
// same way capability.Definition.AuthZScopeRef names one for the governed
// capability gateway: this package neither authenticates nor computes the
// grant, it only requires that an already-made decision names this scope.
const RequiredScope = "search.lexical.query"

// Authorization is the already-evaluated scope decision [Query]'s caller
// supplies. Package search does not authenticate a principal or compute a
// scope grant -- that is internal/trust/authz's job -- it only enforces the
// decision it is handed, the same boundary internal/capability.Authorization
// and people.AuthorizationDecision both declare for their own governed
// surfaces.
type Authorization struct {
	// ScopeGranted reports whether the caller's principal already holds
	// [RequiredScope].
	ScopeGranted bool
	// Reason is the policy token explaining a false ScopeGranted. It is
	// carried for evidence; [Query] never discloses it to anything other
	// than the returned error.
	Reason string
}

// Discloser answers, for one candidate result, whether the querying caller
// may learn that the subject exists at all. It is exactly the question
// people.AuthorizationDecision.SubjectDisclosable answers for a single-
// subject governed read, asked once per search hit instead of once per
// request, because one lexical query can touch subjects the caller has never
// been individually authorized against. [Query] drops -- never redacts,
// never marks WITHHELD -- every subject this reports false or errors for:
// a search result carries no field values to redact, so the only thing left
// to protect is whether the subject appears at all.
type Discloser interface {
	// Disclosable reports whether subject may be named in a search result for
	// this caller. An error is treated as non-disclosable: [Query] fails
	// closed rather than guessing.
	Disclosable(ctx context.Context, subject values.EntityRef) (bool, error)
}

// DiscloserFunc adapts a plain function to [Discloser].
type DiscloserFunc func(ctx context.Context, subject values.EntityRef) (bool, error)

// Disclosable implements [Discloser].
func (f DiscloserFunc) Disclosable(ctx context.Context, subject values.EntityRef) (bool, error) {
	return f(ctx, subject)
}

// AllowAll is a [Discloser] that discloses every subject. It exists for
// tests and for a caller whose own authorization model has already narrowed
// the tenant scope to exactly what may be disclosed (for example, a
// principal with an unconditional tenant-wide search grant); production
// callers with per-subject rules pass their own [Discloser] instead.
var AllowAll Discloser = DiscloserFunc(func(context.Context, values.EntityRef) (bool, error) { return true, nil })

// ProjectionInput is exactly what [Project] needs to (re)build one subject's
// search row: its identity, the source revision the facts were read at, and
// the raw field values the source reader returned.
//
// Fields is deliberately allowed to carry every field the source reader
// knows about, cleared or not: [Project] itself decides which of them are
// eligible (fields.go), so a caller does not have to remember to pre-filter
// before calling it, and a field it excludes is guaranteed excluded rather
// than "excluded provided every caller remembered to leave it out."
type ProjectionInput struct {
	Tenant   values.TenantId
	Subject  values.EntityRef
	Kind     EntityKind
	Revision values.RevisionToken
	Fields   map[people.FieldID]string
}

// Validate reports whether the input is complete enough to project.
func (in ProjectionInput) Validate() error {
	if err := in.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrInvalidProjectionInput, err)
	}
	if err := in.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %w", ErrInvalidProjectionInput, err)
	}
	if in.Subject.Tenant != in.Tenant {
		return fmt.Errorf("%w: subject %s is outside tenant %s", ErrInvalidProjectionInput, in.Subject, in.Tenant)
	}
	if err := in.Kind.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidProjectionInput, err)
	}
	if string(in.Subject.Kind) != string(in.Kind) {
		return fmt.Errorf("%w: subject kind %q does not match declared kind %q",
			ErrInvalidProjectionInput, in.Subject.Kind, in.Kind)
	}
	if !in.Revision.IsSpecified() {
		return fmt.Errorf("%w: source revision is unspecified", ErrInvalidProjectionInput)
	}
	if err := in.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: source revision: %w", ErrInvalidProjectionInput, err)
	}
	return nil
}

// QueryInput is what [Query] is asked for.
type QueryInput struct {
	Tenant values.TenantId
	// Kind narrows the search to one subject kind. Empty means every
	// declared kind.
	Kind EntityKind
	// Text is the caller's search text, matched with PostgreSQL's
	// websearch_to_tsquery.
	Text string
	// Limit bounds the result count. Zero or negative is treated as
	// [DefaultLimit].
	Limit int

	Authorization Authorization
	Discloser     Discloser
}

// DefaultLimit is the result bound a zero [QueryInput.Limit] falls back to.
const DefaultLimit = 25

// MaxLimit is the hard ceiling no caller may exceed, so a search cannot be
// used as an unbounded data-export channel the way
// people.MaxNarrativeLines bounds an explanation for the same reason.
const MaxLimit = 200

// Result is one disclosed search hit: a subject reference and enough
// evidence to judge its freshness, and nothing else. A caller that wants a
// field value makes a second, separately authorized governed read.
type Result struct {
	Subject values.EntityRef
	Kind    EntityKind
	// SourceRevision is the projected revision's own canonical text form.
	// Comparing it against a freshly read fact's current revision is how a
	// caller tells this hit is stale; Query does not compute staleness
	// itself because it has no access to the authoritative reader.
	SourceRevision string
	ProjectedAt    time.Time
	// Rank is PostgreSQL's own ts_rank score, descending. It is a ranking
	// signal only, never a probability or a confidence figure.
	Rank float64
}
