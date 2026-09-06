package employeerelations

import (
	"context"
	"errors"
	"fmt"
)

// Repository is the tenant-aware persistence port for employee-relations
// revisions. expectedRevision is zero for the first revision and otherwise
// must name the current tip; a successful save always appends revision
// expectedRevision+1.
type Repository interface {
	SaveAllegation(context.Context, string, AllegationRevision, uint64) error
	LoadAllegation(context.Context, string, string, uint64) (AllegationRevision, error)
	SaveInvestigation(context.Context, string, InvestigationRevision, uint64) error
	LoadInvestigation(context.Context, string, string, uint64) (InvestigationRevision, error)
	SaveInterview(context.Context, string, InterviewRevision, uint64) error
	LoadInterview(context.Context, string, string, uint64) (InterviewRevision, error)
	SaveFinding(context.Context, string, FindingRevision, uint64) error
	LoadFinding(context.Context, string, string, uint64) (FindingRevision, error)
	SaveDiscipline(context.Context, string, DisciplineRevision, uint64) error
	LoadDiscipline(context.Context, string, string, uint64) (DisciplineRevision, error)
	SaveGrievance(context.Context, string, GrievanceRevision, uint64) error
	LoadGrievance(context.Context, string, string, uint64) (GrievanceRevision, error)
}

// TenantStore is the descriptive alias used by composition roots.
type TenantStore = Repository

var (
	ErrStoreInvalid   = errors.New("employeerelations: invalid store input")
	ErrStoreNotFound  = errors.New("employeerelations: stored revision not found")
	ErrStoreDuplicate = errors.New("employeerelations: duplicate revision")
	ErrStoreStaleCAS  = errors.New("employeerelations: stale compare-and-swap")
)

// StoreErrorCode is the stable machine-readable classification of a
// persistence refusal. Callers must branch on this code, not PostgreSQL text.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
)

// StoreError is returned by tenant-aware adapters for validation, missing-row,
// duplicate-revision and compare-and-swap failures.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual uint64
}

func (e *StoreError) Error() string {
	if e == nil {
		return "employeerelations: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("employeerelations: %s", e.Code)
	}
	return fmt.Sprintf("employeerelations: %s: %s", e.Code, e.Detail)
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
