package pseudonym

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

var (
	ErrQueryRefused       = errors.New("pseudonym: query refused")
	ErrExistenceInference = errors.New("pseudonym: membership query would reveal existence")
	ErrCohortTooSmall     = errors.New("pseudonym: aggregate cohort is below minimum size")
	ErrCrossScopeJoin     = errors.New("pseudonym: cross-scope join refused")
	ErrQueryBudget        = errors.New("pseudonym: query budget exceeded")
)

// QueryKind is a closed query vocabulary. Membership is intentionally
// refusal-only because even a boolean answer is an existence oracle.
type QueryKind string

const (
	QueryMembership QueryKind = "membership"
	QueryAggregate  QueryKind = "aggregate"
	QueryJoin       QueryKind = "join"
)

// QueryConfig declares the privacy and abuse bounds for one query surface.
// The alias fields make the policy explicit at call sites without changing
// the single effective minimum cohort and per-principal budget.
type QueryConfig struct {
	MinCohortSize       int
	MinimumCohortSize   int
	BudgetPerPrincipal  int
	QueriesPerPrincipal int
	BudgetWindow        time.Duration
	Window              time.Duration
	Clock               func() time.Time
}

// QueryRequest contains no subject identity. Cohort members are scoped
// pseudonym identifiers and are never echoed in a result or refusal event.
type QueryRequest struct {
	Kind      QueryKind `json:"kind"`
	Principal string    `json:"principal"`
	Scope     string    `json:"scope"`
	Pseudonym string    `json:"pseudonym,omitempty"`
	Cohort    []string  `json:"cohort,omitempty"`
	Scopes    []string  `json:"scopes,omitempty"`
}

type QueryDecision string

const (
	QueryAllowed QueryDecision = "allowed"
	QueryRefused QueryDecision = "refused"
)

// QueryResult deliberately omits the requested pseudonym and all subject
// identifiers. Refused membership requests have the same result shape for
// present and absent pseudonyms.
type QueryResult struct {
	Decision   QueryDecision `json:"decision"`
	Kind       QueryKind     `json:"kind"`
	Count      int           `json:"count,omitempty"`
	CohortSize int           `json:"cohort_size,omitempty"`
	Reason     string        `json:"reason,omitempty"`
}

// QueryEvidence is safe refusal evidence. RequestDigest is not a subject
// value and no target pseudonym is retained.
type QueryEvidence struct {
	Kind            QueryKind `json:"kind"`
	PrincipalDigest string    `json:"principal_digest"`
	Scope           string    `json:"scope,omitempty"`
	Reason          string    `json:"reason"`
	At              time.Time `json:"at"`
	RequestDigest   string    `json:"request_digest"`
	Digest          string    `json:"digest"`
}

type queryBudget struct {
	StartedAt time.Time
	Count     int
}

// QuerySurface is an in-memory semantic boundary for pseudonymous analytics.
// It stores only scoped pseudonym membership, never a subject-to-pseudonym
// mapping or plaintext identity.
type QuerySurface struct {
	minimumCohort int
	budget        int
	window        time.Duration
	clock         func() time.Time

	mu       sync.Mutex
	subjects map[string]map[string]struct{}
	budgets  map[string]queryBudget
	evidence []QueryEvidence
}

// NewQuerySurface creates a cohort- and budget-bounded query surface.
func NewQuerySurface(config QueryConfig) (*QuerySurface, error) {
	minimum := config.MinCohortSize
	if minimum == 0 {
		minimum = config.MinimumCohortSize
	}
	budget := config.BudgetPerPrincipal
	if budget == 0 {
		budget = config.QueriesPerPrincipal
	}
	window := config.BudgetWindow
	if window == 0 {
		window = config.Window
	}
	if minimum < 2 || budget <= 0 || window <= 0 {
		return nil, fmt.Errorf("%w: minimum cohort must be at least two, and budget/window must be positive", ErrQueryRefused)
	}
	clock := config.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &QuerySurface{minimumCohort: minimum, budget: budget, window: window, clock: clock, subjects: make(map[string]map[string]struct{}), budgets: make(map[string]queryBudget)}, nil
}

// NewCorrelationSurface is a semantic alias for NewQuerySurface.
func NewCorrelationSurface(config QueryConfig) (*QuerySurface, error) {
	return NewQuerySurface(config)
}

// Register adds one scoped pseudonym to the query corpus. It does not accept
// or retain a subject value.
func (q *QuerySurface) Register(p Pseudonym) error {
	if q == nil || pseudonymID(p) == "" || strings.TrimSpace(p.Tenant) == "" || strings.TrimSpace(p.Scope) == "" || p.Generation <= 0 {
		return fmt.Errorf("%w: invalid scoped pseudonym", ErrQueryRefused)
	}
	key := tenantScope(p.Tenant, p.Scope)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.subjects[key] == nil {
		q.subjects[key] = make(map[string]struct{})
	}
	q.subjects[key][pseudonymID(p)] = struct{}{}
	return nil
}

// AddSubject is an alias that emphasizes that only a pseudonymous subject is
// accepted by this surface.
func (q *QuerySurface) AddSubject(p Pseudonym) error { return q.Register(p) }

// Query evaluates one request. Membership always returns the same refusal
// result without consulting subjects, so present and absent probes cannot
// diverge. Aggregate answers require the configured minimum cohort.
func (q *QuerySurface) Query(ctx custody.Context, request QueryRequest) (QueryResult, error) {
	if q == nil {
		return QueryResult{Decision: QueryRefused, Kind: request.Kind, Reason: "query refused"}, ErrQueryRefused
	}
	if err := ctx.Validate(); err != nil {
		return q.refuse(request, ErrQueryRefused)
	}
	if err := validateQueryRequest(ctx, request); err != nil {
		return q.refuse(request, err)
	}
	if err := q.consumeBudget(request.Principal); err != nil {
		return q.refuse(request, err)
	}
	// This branch intentionally precedes all corpus access. Do not optimize it
	// into a membership lookup: the absence of an oracle is the contract.
	if request.Kind == QueryMembership {
		return q.refuse(request, ErrExistenceInference)
	}
	if request.Kind == QueryJoin && hasCrossScope(request) {
		return q.refuse(request, ErrCrossScopeJoin)
	}
	if request.Kind != QueryAggregate && request.Kind != QueryJoin {
		return q.refuse(request, ErrQueryRefused)
	}
	cohort := uniqueStrings(request.Cohort)
	if len(cohort) < q.minimumCohort {
		return q.refuse(request, ErrCohortTooSmall)
	}
	q.mu.Lock()
	members := q.subjects[tenantScope(ctx.Tenant, request.Scope)]
	count := 0
	for _, id := range cohort {
		if _, ok := members[id]; ok {
			count++
		}
	}
	q.mu.Unlock()
	return QueryResult{Decision: QueryAllowed, Kind: request.Kind, Count: count, CohortSize: len(cohort)}, nil
}

// Execute is an alias for Query.
func (q *QuerySurface) Execute(ctx custody.Context, request QueryRequest) (QueryResult, error) {
	return q.Query(ctx, request)
}

// Evidence returns every refusal event in append order.
func (q *QuerySurface) Evidence() []QueryEvidence {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]QueryEvidence(nil), q.evidence...)
}

func (q *QuerySurface) consumeBudget(principal string) error {
	now := q.clock().UTC()
	q.mu.Lock()
	defer q.mu.Unlock()
	budget := q.budgets[principal]
	if budget.StartedAt.IsZero() || !now.Before(budget.StartedAt.Add(q.window)) {
		budget = queryBudget{StartedAt: now}
	}
	budget.Count++
	q.budgets[principal] = budget
	if budget.Count > q.budget {
		return ErrQueryBudget
	}
	return nil
}

func (q *QuerySurface) refuse(request QueryRequest, reason error) (QueryResult, error) {
	// A generic result reason prevents callers from using response text to
	// distinguish a present/absent target or one refusal class from another.
	result := QueryResult{Decision: QueryRefused, Kind: request.Kind, Reason: "query refused"}
	now := q.clock().UTC()
	evidence := QueryEvidence{Kind: request.Kind, PrincipalDigest: digestText(request.Principal), Scope: request.Scope, Reason: refusalReason(reason), At: now}
	evidence.RequestDigest = digestQueryRequest(request)
	evidence.Digest = digestQueryEvidence(evidence)
	q.mu.Lock()
	q.evidence = append(q.evidence, evidence)
	q.mu.Unlock()
	return result, fmt.Errorf("%w: %w", ErrQueryRefused, reason)
}

func validateQueryRequest(ctx custody.Context, request QueryRequest) error {
	if strings.TrimSpace(request.Principal) == "" || request.Principal != strings.TrimSpace(request.Principal) || strings.TrimSpace(request.Scope) == "" || request.Scope != strings.TrimSpace(request.Scope) {
		return ErrQueryRefused
	}
	if request.Scope != ctx.Purpose && request.Scope == "" {
		return ErrQueryRefused
	}
	if request.Kind == QueryMembership && strings.TrimSpace(request.Pseudonym) == "" {
		return ErrExistenceInference
	}
	if request.Kind != QueryMembership && len(request.Cohort) == 0 {
		return ErrCohortTooSmall
	}
	return nil
}

func hasCrossScope(request QueryRequest) bool {
	scopes := append([]string(nil), request.Scopes...)
	if request.Scope != "" {
		scopes = append(scopes, request.Scope)
	}
	return len(uniqueStrings(scopes)) > 1
}

func tenantScope(tenant, scope string) string { return tenant + "\x00" + scope }

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func refusalReason(err error) string {
	switch {
	case errors.Is(err, ErrExistenceInference):
		return "membership_existence_inference"
	case errors.Is(err, ErrCohortTooSmall):
		return "minimum_cohort_not_met"
	case errors.Is(err, ErrCrossScopeJoin):
		return "cross_scope_join"
	case errors.Is(err, ErrQueryBudget):
		return "principal_budget_exceeded"
	default:
		return "invalid_or_refused_query"
	}
}

func digestQueryRequest(request QueryRequest) string {
	canonical := strings.Join([]string{string(request.Kind), digestText(request.Principal), request.Scope, fmt.Sprint(len(request.Cohort)), fmt.Sprint(len(uniqueStrings(request.Scopes)))}, "\x00")
	return digestText(canonical)
}

func digestQueryEvidence(evidence QueryEvidence) string {
	canonical := strings.Join([]string{string(evidence.Kind), evidence.PrincipalDigest, evidence.Scope, evidence.Reason, evidence.At.UTC().Format(time.RFC3339Nano), evidence.RequestDigest}, "\x00")
	return digestText(canonical)
}

// ExplainCorrelation describes the no-oracle query boundary without carrying
// a pseudonym, subject, or identity mapping.
func ExplainCorrelation() string {
	return "Pseudonymous query surfaces refuse membership existence probes, require a declared minimum cohort for aggregates, reject cross-scope joins, rate-bound each principal, and record every refusal as digest-only evidence."
}

// Explain describes the no-oracle query contract.
func (q *QuerySurface) Explain() string { return ExplainCorrelation() }
