package benefits

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Store is the tenant-aware persistence port for the immutable benefit-plan
// revision catalogue. An empty expected revision admits the first revision;
// every later save must name the current revision and its superseded row.
type Store interface {
	Save(context.Context, string, PlanRevision, string) error
	Load(context.Context, string, string, string) (PlanRevision, error)
	Current(context.Context, string, string) (PlanRevision, error)
	List(context.Context, string, string) ([]PlanRevision, error)
}

var (
	ErrStoreInvalid   = errors.New("benefits: invalid store input")
	ErrStoreNotFound  = errors.New("benefits: stored plan revision not found")
	ErrStoreDuplicate = errors.New("benefits: duplicate revision")
	ErrStoreStaleCAS  = errors.New("benefits: stale compare-and-swap")
)

// StoreErrorCode is the stable machine-readable classification of a store
// refusal. Adapters return these codes instead of exposing database errors.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
	StoreDatabaseCode  StoreErrorCode = "DATABASE"
)

// StoreError is a typed persistence failure. Expected and Actual are filled
// for stale-CAS refusals and contain only revision selectors.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "benefits: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("benefits: %s", e.Code)
	}
	return fmt.Sprintf("benefits: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	switch e.Code {
	case StoreInvalidCode:
		return ErrStoreInvalid
	case StoreNotFoundCode:
		return ErrStoreNotFound
	case StoreDuplicateCode:
		return ErrStoreDuplicate
	case StoreStaleCASCode:
		return ErrStoreStaleCAS
	default:
		return nil
	}
}

// CodeOf returns the nearest stable store error code.
func CodeOf(err error) StoreErrorCode {
	var typed *StoreError
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

// NewPlan validates and defensively copies a logical plan identity.
func NewPlan(p Plan) (Plan, error) {
	p.PlanYears = append([]PlanYear(nil), p.PlanYears...)
	if err := p.Validate(); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// NewPlanRevision validates and mints the canonical digest for one immutable
// plan-year revision. It copies slices so callers cannot mutate a published
// value through their original backing arrays.
func NewPlanRevision(r PlanRevision) (PlanRevision, error) {
	r.CoverageTiers = append([]string(nil), r.CoverageTiers...)
	r.Options = append([]string(nil), r.Options...)
	r.CanonicalDigest = ""
	if err := validateRevisionContract(r); err != nil {
		return PlanRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

// ValidatePlanRevision applies the complete BEN-001 contract, including the
// plan-year fields that the original value validator intentionally treats as
// optional while a revision is being assembled.
func ValidatePlanRevision(r PlanRevision) error { return validateRevisionContract(r) }

func validateRevisionContract(r PlanRevision) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.PlanYear.PlanID.Id == "" {
		return fmt.Errorf("%w: plan year is required", ErrInvalidRevision)
	}
	if err := r.PlanYear.Validate(); err != nil {
		return fmt.Errorf("%w: plan year: %v", ErrInvalidRevision, err)
	}
	if r.PlanYear.PlanID != r.PlanID {
		return fmt.Errorf("%w: plan year belongs to another plan", ErrInvalidRevision)
	}
	return nil
}

// PlanExplanation is an audit-safe projection of a plan revision. It omits
// carrier, sponsor and rule payloads while retaining the facts needed to cite
// the selected immutable revision.
type PlanExplanation struct {
	PlanID          string
	Revision        string
	PlanYear        int32
	CoverageTiers   int
	Options         int
	HasRateSchedule bool
	HasEligibility  bool
	HasEnrollment   bool
	HasContribution bool
	Digest          string
}

// Explain returns the safe citation projection of a valid revision.
func (r PlanRevision) Explain() (PlanExplanation, error) {
	if err := validateRevisionContract(r); err != nil {
		return PlanExplanation{}, err
	}
	return PlanExplanation{
		PlanID:          r.PlanID.String(),
		Revision:        r.Revision.String(),
		PlanYear:        r.PlanYear.Year,
		CoverageTiers:   len(r.CoverageTiers),
		Options:         len(r.Options),
		HasRateSchedule: r.RateScheduleRef.Id != "",
		HasEligibility:  r.EligibilityRulesRef.Id != "",
		HasEnrollment:   r.EnrollmentRulesRef.Id != "",
		HasContribution: r.ContributionRulesRef.Id != "",
		Digest:          r.computedDigest(),
	}, nil
}

// ExplainPlanRevision is the package-level explanation helper used by
// consumers that do not keep a method value.
func ExplainPlanRevision(r PlanRevision) (PlanExplanation, error) { return r.Explain() }

// MemoryStore is the kernel-pure reference implementation of Store. It keeps
// immutable revisions and applies the same duplicate and stale-CAS rules as
// the PostgreSQL adapter.
type MemoryStore struct {
	mu    sync.RWMutex
	plans map[string]map[string]map[string]PlanRevision
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{plans: make(map[string]map[string]map[string]PlanRevision)}
}

func (s *MemoryStore) Save(_ context.Context, tenantID string, revision PlanRevision, expectedRevision string) error {
	if s == nil || !validTenantString(tenantID) {
		return storeError(StoreInvalidCode, "tenant id is required")
	}
	if revision.PlanID.Tenant.String() != tenantID {
		return storeError(StoreInvalidCode, "plan tenant does not match store tenant")
	}
	if revision.CanonicalDigest == "" {
		var err error
		revision, err = NewPlanRevision(revision)
		if err != nil {
			return storeError(StoreInvalidCode, err.Error())
		}
	} else if err := validateRevisionContract(revision); err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	if _, ok := revision.Revision.Sequence(); !ok {
		return storeError(StoreInvalidCode, "benefit plan revisions require ordered sequence tokens")
	}
	revisionKey := revision.Revision.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	byPlan := s.plans[tenantID]
	if byPlan == nil {
		byPlan = make(map[string]map[string]PlanRevision)
		s.plans[tenantID] = byPlan
	}
	byRevision := byPlan[revision.PlanID.Id]
	if byRevision == nil {
		byRevision = make(map[string]PlanRevision)
		byPlan[revision.PlanID.Id] = byRevision
	}
	if _, exists := byRevision[revisionKey]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("plan %s revision %s", revision.PlanID.Id, revisionKey))
	}
	current, found := currentRevision(byRevision)
	if expectedRevision == "" {
		if found {
			return staleStore(expectedRevision, current.Revision.String(), "plan already has a current revision")
		}
		if revision.Supersedes.Id != "" {
			return storeError(StoreInvalidCode, "initial plan revision cannot supersede another revision")
		}
	} else {
		if !found || current.Revision.String() != expectedRevision || revision.Supersedes != current.RevisionID {
			actual := ""
			if found {
				actual = current.Revision.String()
			}
			return staleStore(expectedRevision, actual, "plan current revision changed")
		}
	}
	byRevision[revisionKey] = cloneRevision(revision)
	return nil
}

func (s *MemoryStore) Load(_ context.Context, tenantID, planID, revision string) (PlanRevision, error) {
	if s == nil || !validTenantString(tenantID) || planID == "" || revision == "" {
		return PlanRevision{}, storeError(StoreInvalidCode, "tenant, plan id and revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	revisions := s.plans[tenantID][planID]
	item, ok := revisions[revision]
	if !ok {
		return PlanRevision{}, storeError(StoreNotFoundCode, fmt.Sprintf("plan %s revision %s", planID, revision))
	}
	return cloneRevision(item), nil
}

func (s *MemoryStore) Current(_ context.Context, tenantID, planID string) (PlanRevision, error) {
	if s == nil || !validTenantString(tenantID) || planID == "" {
		return PlanRevision{}, storeError(StoreInvalidCode, "tenant and plan id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := currentRevision(s.plans[tenantID][planID])
	if !ok {
		return PlanRevision{}, storeError(StoreNotFoundCode, fmt.Sprintf("plan %s", planID))
	}
	return cloneRevision(item), nil
}

func (s *MemoryStore) List(_ context.Context, tenantID, planID string) ([]PlanRevision, error) {
	if s == nil || !validTenantString(tenantID) || planID == "" {
		return nil, storeError(StoreInvalidCode, "tenant and plan id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	revisions := s.plans[tenantID][planID]
	out := make([]PlanRevision, 0, len(revisions))
	for _, item := range revisions {
		out = append(out, cloneRevision(item))
	}
	if len(out) == 0 {
		return nil, storeError(StoreNotFoundCode, fmt.Sprintf("plan %s", planID))
	}
	sort.Slice(out, func(i, j int) bool {
		left, _ := out[i].Revision.Sequence()
		right, _ := out[j].Revision.Sequence()
		return left < right
	})
	return out, nil
}

func validTenantString(tenantID string) bool { return values.TenantId(tenantID).Validate() == nil }

func cloneRevision(r PlanRevision) PlanRevision {
	r.CoverageTiers = append([]string(nil), r.CoverageTiers...)
	r.Options = append([]string(nil), r.Options...)
	return r
}

func currentRevision(revisions map[string]PlanRevision) (PlanRevision, bool) {
	var current PlanRevision
	found := false
	for _, candidate := range revisions {
		isHead := true
		for _, other := range revisions {
			if other.Supersedes == candidate.RevisionID {
				isHead = false
				break
			}
		}
		if !isHead {
			continue
		}
		candidateSequence, ok := candidate.Revision.Sequence()
		if !ok {
			continue
		}
		currentSequence, _ := current.Revision.Sequence()
		if !found || candidateSequence > currentSequence {
			current, found = candidate, true
		}
	}
	return current, found
}

func storeError(code StoreErrorCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: strings.TrimSpace(detail)}
}

func staleStore(expected, actual, detail string) *StoreError {
	return &StoreError{Code: StoreStaleCASCode, Detail: detail, Expected: expected, Actual: actual}
}
