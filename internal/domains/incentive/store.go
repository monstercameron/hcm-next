package incentive

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Store is the tenant-aware persistence port for incentive metadata. The
// returned records are the durable projections defined by the data contract;
// the full plan and award inputs remain kernel values owned by the caller.
type Store interface {
	SavePlan(context.Context, string, IncentivePlanRevision) error
	LoadPlan(context.Context, string, string, uint64) (PlanRevisionRecord, error)
	AppendObservation(context.Context, string, AttainmentObservation, uint64) error
	ListObservations(context.Context, string, string, string) ([]AttainmentObservationRecord, error)
	SaveAward(context.Context, string, AwardCalculation) error
	LoadAward(context.Context, string, string, uint64) (AwardCalculationRecord, error)
}

var (
	ErrStoreRefused      = errors.New("incentive: store operation refused")
	ErrStoreInvalid      = errors.New("incentive: invalid store input")
	ErrStoreNotFound     = errors.New("incentive: stored record not found")
	ErrStoreDuplicate    = errors.New("incentive: duplicate revision")
	ErrStoreStaleCAS     = errors.New("incentive: stale compare-and-swap")
	ErrStoreSequence     = errors.New("incentive: duplicate event sequence")
	ErrStorePlanMismatch = errors.New("incentive: plan digest does not match revision")
	ErrStoreStorage      = errors.New("incentive: storage failure")
)

// StoreCode is the stable machine-readable classification of a store refusal.
type StoreCode string

const (
	StoreInvalidCode      StoreCode = "INVALID"
	StoreNotFoundCode     StoreCode = "NOT_FOUND"
	StoreDuplicateCode    StoreCode = "DUPLICATE_REVISION"
	StoreStaleCASCode     StoreCode = "STALE_CAS"
	StoreSequenceCode     StoreCode = "DUPLICATE_EVENT_SEQUENCE"
	StorePlanMismatchCode StoreCode = "PLAN_DIGEST_MISMATCH"
	StoreStorageCode      StoreCode = "STORAGE"
)

// StoreError carries a stable code and preserves a domain sentinel through
// errors.Is. Expected and Actual are populated for CAS failures.
type StoreError struct {
	Code             StoreCode
	Detail           string
	Expected, Actual string
	cause            error
}

func (e *StoreError) Error() string {
	if e == nil {
		return "incentive: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("incentive: %s", e.Code)
	}
	return fmt.Sprintf("incentive: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.cause != nil {
		return e.cause
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
	case StoreSequenceCode:
		return ErrStoreSequence
	case StorePlanMismatchCode:
		return ErrStorePlanMismatch
	case StoreStorageCode:
		return ErrStoreStorage
	default:
		return ErrStoreRefused
	}
}

func storeError(code StoreCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: detail}
}

func staleStoreError(expected, actual, detail string) *StoreError {
	return &StoreError{Code: StoreStaleCASCode, Detail: detail, Expected: expected, Actual: actual}
}

// PlanRevisionRecord is the durable projection of an incentive plan revision.
type PlanRevisionRecord struct {
	RowID              string
	TenantID           string
	PlanID             string
	Revision           uint64
	SupersedesRevision uint64
	ParentDigest       string
	CanonicalDigest    string
	Digest             string
}

// AttainmentObservationRecord is the durable projection of an observation.
type AttainmentObservationRecord struct {
	RowID         string
	TenantID      string
	ObservationID string
	PlanID        string
	PlanRevision  uint64
	WorkerRef     string
	Value         string
	AsOf          string
	KnownAt       string
	SourceRef     string
	Digest        string
	EventSequence uint64
}

// AwardCalculationRecord is the durable projection of an award calculation.
type AwardCalculationRecord struct {
	RowID              string
	TenantID           string
	CalculationID      string
	WorkerRef          string
	PlanDigest         string
	PlanRevision       uint64
	State              AwardState
	Revision           uint64
	SupersedesRevision uint64
	CanonicalDigest    string
	Digest             string
}

// MemoryStore is the kernel-pure reference implementation of Store. It keeps
// the same immutable revision, observation-sequence and plan-digest rules as
// the PostgreSQL adapter.
type MemoryStore struct {
	mu           sync.RWMutex
	plans        map[string]map[string]map[uint64]PlanRevisionRecord
	observations map[string]AttainmentObservationRecord
	awards       map[string]map[string]map[uint64]AwardCalculationRecord
}

var _ Store = (*MemoryStore)(nil)

var memoryRowID uint64

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		plans:        make(map[string]map[string]map[uint64]PlanRevisionRecord),
		observations: make(map[string]AttainmentObservationRecord),
		awards:       make(map[string]map[string]map[uint64]AwardCalculationRecord),
	}
}

func memoryID() string { return fmt.Sprintf("memory-%d", atomic.AddUint64(&memoryRowID, 1)) }

func validStoreContext(ctx context.Context, tenantID string) error {
	if ctx == nil {
		return storeError(StoreInvalidCode, "context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(tenantID) == "" {
		return storeError(StoreInvalidCode, "tenant id is required")
	}
	return nil
}

func planRecord(tenantID string, plan IncentivePlanRevision) PlanRevisionRecord {
	return PlanRevisionRecord{
		RowID: memoryID(), TenantID: tenantID, PlanID: plan.PlanID, Revision: plan.Revision,
		SupersedesRevision: plan.SupersedesRevision, ParentDigest: plan.ParentDigest,
		CanonicalDigest: plan.CanonicalDigest, Digest: plan.Digest,
	}
}

func validatePlan(plan IncentivePlanRevision) (IncentivePlanRevision, error) {
	if plan.CanonicalDigest == "" {
		normalized, err := NewIncentivePlanRevision(plan)
		if err != nil {
			return IncentivePlanRevision{}, storeError(StoreInvalidCode, err.Error())
		}
		return normalized, nil
	}
	if err := plan.Validate(); err != nil {
		return IncentivePlanRevision{}, storeError(StoreInvalidCode, err.Error())
	}
	return plan, nil
}

func validateAward(award AwardCalculation) (AwardCalculation, error) {
	if award.CanonicalDigest == "" {
		normalized, err := NewAwardCalculation(award)
		if err != nil {
			return AwardCalculation{}, storeError(StoreInvalidCode, err.Error())
		}
		return normalized, nil
	}
	if err := award.Validate(); err != nil {
		return AwardCalculation{}, storeError(StoreInvalidCode, err.Error())
	}
	return award, nil
}
func validateNextRevision[T any](revisions map[uint64]T, revision, supersedes uint64, currentRevision func(T) uint64, currentDigest func(T) string, parentDigest string) error {
	if _, exists := revisions[revision]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("revision %d is already stored", revision))
	}
	if len(revisions) == 0 {
		if revision != 1 || supersedes != 0 || parentDigest != "" {
			return staleStoreError("", "", "the first revision must be revision 1 without lineage")
		}
		return nil
	}
	var current T
	var found bool
	for _, candidate := range revisions {
		if !found || currentRevision(candidate) > currentRevision(current) {
			current, found = candidate, true
		}
	}
	actual := fmt.Sprintf("%d", currentRevision(current))
	if revision != currentRevision(current)+1 || supersedes != currentRevision(current) {
		return staleStoreError(actual, actual, fmt.Sprintf("revision %d does not extend current revision %d", revision, currentRevision(current)))
	}
	// Award revisions do not expose a parent-digest field; their complete
	// canonical digest and revision CAS still prevent rewrite. Plan callers
	// always supply parentDigest and therefore also enforce digest lineage.
	if parentDigest != "" && parentDigest != currentDigest(current) {
		return staleStoreError(actual, actual, "parent digest does not match the current revision")
	}
	return nil
}

func (s *MemoryStore) SavePlan(ctx context.Context, tenantID string, plan IncentivePlanRevision) error {
	if err := validStoreContext(ctx, tenantID); err != nil {
		return err
	}
	plan, err := validatePlan(plan)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plans == nil {
		s.plans = make(map[string]map[string]map[uint64]PlanRevisionRecord)
	}
	byPlan := s.plans[tenantID]
	if byPlan == nil {
		byPlan = make(map[string]map[uint64]PlanRevisionRecord)
		s.plans[tenantID] = byPlan
	}
	revisions := byPlan[plan.PlanID]
	if revisions == nil {
		revisions = make(map[uint64]PlanRevisionRecord)
		byPlan[plan.PlanID] = revisions
	}
	if err := validateNextRevision(revisions, plan.Revision, plan.SupersedesRevision,
		func(r PlanRevisionRecord) uint64 { return r.Revision }, func(r PlanRevisionRecord) string { return r.Digest }, plan.ParentDigest); err != nil {
		return err
	}
	revisions[plan.Revision] = planRecord(tenantID, plan)
	return nil
}

func (s *MemoryStore) LoadPlan(ctx context.Context, tenantID, planID string, revision uint64) (PlanRevisionRecord, error) {
	if err := validStoreContext(ctx, tenantID); err != nil {
		return PlanRevisionRecord{}, err
	}
	if strings.TrimSpace(planID) == "" || revision == 0 {
		return PlanRevisionRecord{}, storeError(StoreInvalidCode, "plan id and revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.plans[tenantID][planID][revision]
	if !ok {
		return PlanRevisionRecord{}, storeError(StoreNotFoundCode, fmt.Sprintf("plan %s revision %d", planID, revision))
	}
	return record, nil
}

func (s *MemoryStore) AppendObservation(ctx context.Context, tenantID string, observation AttainmentObservation, eventSequence uint64) error {
	if err := validStoreContext(ctx, tenantID); err != nil {
		return err
	}
	if eventSequence == 0 {
		return storeError(StoreInvalidCode, "event sequence must be positive")
	}
	observation, err := NewAttainmentObservation(observation)
	if err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observations == nil {
		s.observations = make(map[string]AttainmentObservationRecord)
	}
	key := tenantID + "\x00" + observation.WorkerRef + "\x00" + observation.PlanID + "\x00" + fmt.Sprintf("%d", eventSequence)
	if _, exists := s.observations[key]; exists {
		return storeError(StoreSequenceCode, fmt.Sprintf("observation event sequence %d is already stored", eventSequence))
	}
	s.observations[key] = AttainmentObservationRecord{
		RowID: memoryID(), TenantID: tenantID, ObservationID: observation.ObservationID,
		PlanID: observation.PlanID, PlanRevision: observation.PlanRevision, WorkerRef: observation.WorkerRef,
		Value: observation.Value.String(), AsOf: observation.AsOfEffective.String(), KnownAt: observation.AsKnownAt.String(),
		SourceRef: observation.SourceRef, Digest: observation.Digest, EventSequence: eventSequence,
	}
	return nil
}

func (s *MemoryStore) ListObservations(ctx context.Context, tenantID, workerRef, planID string) ([]AttainmentObservationRecord, error) {
	if err := validStoreContext(ctx, tenantID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(workerRef) == "" || strings.TrimSpace(planID) == "" {
		return nil, storeError(StoreInvalidCode, "worker ref and plan id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := tenantID + "\x00" + workerRef + "\x00" + planID + "\x00"
	out := make([]AttainmentObservationRecord, 0)
	for key, record := range s.observations {
		if strings.HasPrefix(key, prefix) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventSequence < out[j].EventSequence })
	return out, nil
}

func (s *MemoryStore) SaveAward(ctx context.Context, tenantID string, award AwardCalculation) error {
	if err := validStoreContext(ctx, tenantID); err != nil {
		return err
	}
	award, err := validateAward(award)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.awards == nil {
		s.awards = make(map[string]map[string]map[uint64]AwardCalculationRecord)
	}
	byCalculation := s.awards[tenantID]
	if byCalculation == nil {
		byCalculation = make(map[string]map[uint64]AwardCalculationRecord)
		s.awards[tenantID] = byCalculation
	}
	revisions := byCalculation[award.CalculationID]
	if revisions == nil {
		revisions = make(map[uint64]AwardCalculationRecord)
		byCalculation[award.CalculationID] = revisions
	}
	if err := validateNextRevision(revisions, award.Revision, award.SupersedesRevision,
		func(r AwardCalculationRecord) uint64 { return r.Revision }, func(r AwardCalculationRecord) string { return r.Digest }, ""); err != nil {
		return err
	}
	planFound := false
	for _, plans := range s.plans[tenantID] {
		if plan, ok := plans[award.PlanRevision]; ok && plan.Digest == award.PlanDigest {
			planFound = true
			break
		}
	}
	if !planFound {
		return storeError(StorePlanMismatchCode, fmt.Sprintf("plan revision %d has digest %q", award.PlanRevision, award.PlanDigest))
	}
	revisions[award.Revision] = AwardCalculationRecord{
		RowID: memoryID(), TenantID: tenantID, CalculationID: award.CalculationID, WorkerRef: award.WorkerRef,
		PlanDigest: award.PlanDigest, PlanRevision: award.PlanRevision, State: award.State,
		Revision: award.Revision, SupersedesRevision: award.SupersedesRevision,
		CanonicalDigest: award.CanonicalDigest, Digest: award.Digest,
	}
	return nil
}

func (s *MemoryStore) LoadAward(ctx context.Context, tenantID, calculationID string, revision uint64) (AwardCalculationRecord, error) {
	if err := validStoreContext(ctx, tenantID); err != nil {
		return AwardCalculationRecord{}, err
	}
	if strings.TrimSpace(calculationID) == "" || revision == 0 {
		return AwardCalculationRecord{}, storeError(StoreInvalidCode, "calculation id and revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.awards[tenantID][calculationID][revision]
	if !ok {
		return AwardCalculationRecord{}, storeError(StoreNotFoundCode, fmt.Sprintf("calculation %s revision %d", calculationID, revision))
	}
	return record, nil
}
