package career

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Store is the tenant-aware persistence port for the canonical career profile
// revisions. Every Save appends a revision and compares the caller's expected
// current revision; an unspecified expectation means that the stream must not
// exist yet.
type Store interface {
	SavePreference(context.Context, string, CareerPreferenceProfileRevision, values.RevisionToken) error
	LoadPreference(context.Context, string, string, values.RevisionToken) (CareerPreferenceProfileRevision, error)
	CurrentPreference(context.Context, string, string) (CareerPreferenceProfileRevision, error)
	ListPreferences(context.Context, string, string) ([]CareerPreferenceProfileRevision, error)

	SaveTargetRole(context.Context, string, TargetRoleProfileRevision, values.RevisionToken) error
	LoadTargetRole(context.Context, string, string, values.RevisionToken) (TargetRoleProfileRevision, error)
	CurrentTargetRole(context.Context, string, string) (TargetRoleProfileRevision, error)
	ListTargetRoles(context.Context, string, string) ([]TargetRoleProfileRevision, error)

	SaveDevelopmentObjective(context.Context, string, DevelopmentObjectiveProfileRevision, values.RevisionToken) error
	LoadDevelopmentObjective(context.Context, string, string, values.RevisionToken) (DevelopmentObjectiveProfileRevision, error)
	CurrentDevelopmentObjective(context.Context, string, string) (DevelopmentObjectiveProfileRevision, error)
	ListDevelopmentObjectives(context.Context, string, string) ([]DevelopmentObjectiveProfileRevision, error)

	SaveAssessment(context.Context, string, CareerAssessmentRevision, values.RevisionToken) error
	LoadAssessment(context.Context, string, string, values.RevisionToken) (CareerAssessmentRevision, error)
	CurrentAssessment(context.Context, string, string) (CareerAssessmentRevision, error)
	ListAssessments(context.Context, string, string) ([]CareerAssessmentRevision, error)
}

var (
	ErrStoreInvalid   = errors.New("career: invalid store input")
	ErrStoreNotFound  = errors.New("career: stored revision not found")
	ErrStoreDuplicate = errors.New("career: duplicate revision")
	ErrStoreStaleCAS  = errors.New("career: stale compare-and-swap")
)

// StoreErrorCode is the stable classification of a persistence refusal.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
)

// StoreError is returned for validation, lookup, duplicate and CAS failures.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "career: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("career: %s", e.Code)
	}
	return fmt.Sprintf("career: %s: %s", e.Code, e.Detail)
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

func NewStoreError(code StoreErrorCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: detail}
}

func StoreCodeOf(err error) StoreErrorCode {
	var typed *StoreError
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

// MemoryStore is the kernel-pure reference implementation of Store. It is
// useful to domain callers that do not need a database and applies the same
// append, duplicate and stale-CAS rules as the PostgreSQL adapter.
type MemoryStore struct {
	mu          sync.RWMutex
	preferences map[string]map[string]map[uint64]CareerPreferenceProfileRevision
	targets     map[string]map[string]map[uint64]TargetRoleProfileRevision
	objectives  map[string]map[string]map[uint64]DevelopmentObjectiveProfileRevision
	assessments map[string]map[string]map[uint64]CareerAssessmentRevision
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		preferences: make(map[string]map[string]map[uint64]CareerPreferenceProfileRevision),
		targets:     make(map[string]map[string]map[uint64]TargetRoleProfileRevision),
		objectives:  make(map[string]map[string]map[uint64]DevelopmentObjectiveProfileRevision),
		assessments: make(map[string]map[string]map[uint64]CareerAssessmentRevision),
	}
}

func revisionNumber(revision values.RevisionToken) (uint64, error) {
	sequence, ok := revision.Sequence()
	if !ok || sequence == 0 {
		return 0, NewStoreError(StoreInvalidCode, "revision must be a positive sequence token")
	}
	return sequence, nil
}

func checkMemorySave[T any](tenant, id string, revision, supersedes, expected values.RevisionToken, rows map[uint64]T) (uint64, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(id) == "" {
		return 0, NewStoreError(StoreInvalidCode, "tenant and entity id are required")
	}
	sequence, err := revisionNumber(revision)
	if err != nil {
		return 0, err
	}
	if _, exists := rows[sequence]; exists {
		return 0, NewStoreError(StoreDuplicateCode, fmt.Sprintf("%s revision %d", id, sequence))
	}
	var current values.RevisionToken
	var currentSequence uint64
	for candidate := range rows {
		if candidate > currentSequence {
			currentSequence = candidate
		}
	}
	if currentSequence > 0 {
		var makeCurrent error
		current, makeCurrent = values.NewSequenceRevision(revision.Stream(), currentSequence)
		if makeCurrent != nil {
			return 0, NewStoreError(StoreInvalidCode, makeCurrent.Error())
		}
	}
	if !expected.IsSpecified() {
		if currentSequence > 0 {
			return 0, staleMemory(expected, current)
		}
		if supersedes.IsSpecified() {
			return 0, NewStoreError(StoreInvalidCode, "initial revision cannot supersede a revision")
		}
		return sequence, nil
	}
	if currentSequence == 0 || !expected.Equal(current) || !supersedes.Equal(expected) {
		return 0, staleMemory(expected, current)
	}
	if sequence <= currentSequence {
		return 0, staleMemory(expected, current)
	}
	return sequence, nil
}

func staleMemory(expected, actual values.RevisionToken) *StoreError {
	err := NewStoreError(StoreStaleCASCode, fmt.Sprintf("expected %s, actual %s", expected.String(), actual.String()))
	err.Expected, err.Actual = expected.String(), actual.String()
	return err
}

func ensurePreference(value CareerPreferenceProfileRevision) (CareerPreferenceProfileRevision, error) {
	if value.CanonicalDigest == "" {
		return NewCareerPreference(value)
	}
	return value, value.Validate()
}

func ensureTarget(value TargetRoleProfileRevision) (TargetRoleProfileRevision, error) {
	if value.CanonicalDigest == "" {
		return NewTargetRoleProfile(value)
	}
	return value, value.Validate()
}

func ensureObjective(value DevelopmentObjectiveProfileRevision) (DevelopmentObjectiveProfileRevision, error) {
	if value.CanonicalDigest == "" {
		return NewDevelopmentObjective(value)
	}
	return value, value.Validate()
}

func (s *MemoryStore) SavePreference(_ context.Context, tenant string, value CareerPreferenceProfileRevision, expected values.RevisionToken) error {
	var err error
	value, err = ensurePreference(value)
	if err != nil {
		return NewStoreError(StoreInvalidCode, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.preferences[tenant]
	if byID == nil {
		byID = make(map[string]map[uint64]CareerPreferenceProfileRevision)
		s.preferences[tenant] = byID
	}
	rows := byID[value.PreferenceID.Id]
	if rows == nil {
		rows = make(map[uint64]CareerPreferenceProfileRevision)
		byID[value.PreferenceID.Id] = rows
	}
	sequence, err := checkMemorySave(tenant, value.PreferenceID.Id, value.Revision, value.Supersedes, expected, rows)
	if err != nil {
		return err
	}
	rows[sequence] = clonePreference(value)
	return nil
}

func (s *MemoryStore) LoadPreference(_ context.Context, tenant, id string, revision values.RevisionToken) (CareerPreferenceProfileRevision, error) {
	sequence, err := revisionNumber(revision)
	if err != nil {
		return CareerPreferenceProfileRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.preferences[tenant][id][sequence]
	if !ok {
		return CareerPreferenceProfileRevision{}, NewStoreError(StoreNotFoundCode, fmt.Sprintf("preference %s revision %d", id, sequence))
	}
	return clonePreference(value), nil
}

func (s *MemoryStore) CurrentPreference(_ context.Context, tenant, id string) (CareerPreferenceProfileRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.preferences[tenant][id]
	value, ok := latestPreference(rows)
	if !ok {
		return CareerPreferenceProfileRevision{}, NewStoreError(StoreNotFoundCode, fmt.Sprintf("preference %s", id))
	}
	return clonePreference(value), nil
}

func (s *MemoryStore) ListPreferences(_ context.Context, tenant, id string) ([]CareerPreferenceProfileRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.preferences[tenant][id]
	if len(rows) == 0 {
		return nil, NewStoreError(StoreNotFoundCode, fmt.Sprintf("preference %s", id))
	}
	out := make([]CareerPreferenceProfileRevision, 0, len(rows))
	for _, value := range rows {
		out = append(out, clonePreference(value))
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i].Revision.Sequence()
		b, _ := out[j].Revision.Sequence()
		return a < b
	})
	return out, nil
}

func (s *MemoryStore) SaveTargetRole(_ context.Context, tenant string, value TargetRoleProfileRevision, expected values.RevisionToken) error {
	var err error
	value, err = ensureTarget(value)
	if err != nil {
		return NewStoreError(StoreInvalidCode, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.targets[tenant]
	if byID == nil {
		byID = make(map[string]map[uint64]TargetRoleProfileRevision)
		s.targets[tenant] = byID
	}
	rows := byID[value.TargetRoleID.Id]
	if rows == nil {
		rows = make(map[uint64]TargetRoleProfileRevision)
		byID[value.TargetRoleID.Id] = rows
	}
	sequence, err := checkMemorySave(tenant, value.TargetRoleID.Id, value.Revision, value.Supersedes, expected, rows)
	if err != nil {
		return err
	}
	rows[sequence] = cloneTarget(value)
	return nil
}

func (s *MemoryStore) LoadTargetRole(_ context.Context, tenant, id string, revision values.RevisionToken) (TargetRoleProfileRevision, error) {
	sequence, err := revisionNumber(revision)
	if err != nil {
		return TargetRoleProfileRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.targets[tenant][id][sequence]
	if !ok {
		return TargetRoleProfileRevision{}, NewStoreError(StoreNotFoundCode, fmt.Sprintf("target role %s revision %d", id, sequence))
	}
	return cloneTarget(value), nil
}

func (s *MemoryStore) CurrentTargetRole(_ context.Context, tenant, id string) (TargetRoleProfileRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := latestTarget(s.targets[tenant][id])
	if !ok {
		return TargetRoleProfileRevision{}, NewStoreError(StoreNotFoundCode, fmt.Sprintf("target role %s", id))
	}
	return cloneTarget(value), nil
}

func (s *MemoryStore) ListTargetRoles(_ context.Context, tenant, id string) ([]TargetRoleProfileRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.targets[tenant][id]
	if len(rows) == 0 {
		return nil, NewStoreError(StoreNotFoundCode, fmt.Sprintf("target role %s", id))
	}
	out := make([]TargetRoleProfileRevision, 0, len(rows))
	for _, value := range rows {
		out = append(out, cloneTarget(value))
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i].Revision.Sequence()
		b, _ := out[j].Revision.Sequence()
		return a < b
	})
	return out, nil
}

func (s *MemoryStore) SaveDevelopmentObjective(_ context.Context, tenant string, value DevelopmentObjectiveProfileRevision, expected values.RevisionToken) error {
	var err error
	value, err = ensureObjective(value)
	if err != nil {
		return NewStoreError(StoreInvalidCode, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.objectives[tenant]
	if byID == nil {
		byID = make(map[string]map[uint64]DevelopmentObjectiveProfileRevision)
		s.objectives[tenant] = byID
	}
	rows := byID[value.ObjectiveID.Id]
	if rows == nil {
		rows = make(map[uint64]DevelopmentObjectiveProfileRevision)
		byID[value.ObjectiveID.Id] = rows
	}
	sequence, err := checkMemorySave(tenant, value.ObjectiveID.Id, value.Revision, value.Supersedes, expected, rows)
	if err != nil {
		return err
	}
	rows[sequence] = cloneObjective(value)
	return nil
}

func (s *MemoryStore) LoadDevelopmentObjective(_ context.Context, tenant, id string, revision values.RevisionToken) (DevelopmentObjectiveProfileRevision, error) {
	sequence, err := revisionNumber(revision)
	if err != nil {
		return DevelopmentObjectiveProfileRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.objectives[tenant][id][sequence]
	if !ok {
		return DevelopmentObjectiveProfileRevision{}, NewStoreError(StoreNotFoundCode, fmt.Sprintf("objective %s revision %d", id, sequence))
	}
	return cloneObjective(value), nil
}

func (s *MemoryStore) CurrentDevelopmentObjective(_ context.Context, tenant, id string) (DevelopmentObjectiveProfileRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := latestObjective(s.objectives[tenant][id])
	if !ok {
		return DevelopmentObjectiveProfileRevision{}, NewStoreError(StoreNotFoundCode, fmt.Sprintf("objective %s", id))
	}
	return cloneObjective(value), nil
}

func (s *MemoryStore) ListDevelopmentObjectives(_ context.Context, tenant, id string) ([]DevelopmentObjectiveProfileRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.objectives[tenant][id]
	if len(rows) == 0 {
		return nil, NewStoreError(StoreNotFoundCode, fmt.Sprintf("objective %s", id))
	}
	out := make([]DevelopmentObjectiveProfileRevision, 0, len(rows))
	for _, value := range rows {
		out = append(out, cloneObjective(value))
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i].Revision.Sequence()
		b, _ := out[j].Revision.Sequence()
		return a < b
	})
	return out, nil
}

func (s *MemoryStore) SaveAssessment(_ context.Context, tenant string, value CareerAssessmentRevision, expected values.RevisionToken) error {
	if err := value.Validate(); err != nil {
		return NewStoreError(StoreInvalidCode, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.assessments[tenant]
	if byID == nil {
		byID = make(map[string]map[uint64]CareerAssessmentRevision)
		s.assessments[tenant] = byID
	}
	rows := byID[value.AssessmentID.Id]
	if rows == nil {
		rows = make(map[uint64]CareerAssessmentRevision)
		byID[value.AssessmentID.Id] = rows
	}
	sequence, err := checkMemorySave(tenant, value.AssessmentID.Id, value.Revision, expected, expected, rows)
	if err != nil {
		return err
	}
	rows[sequence] = value
	return nil
}

func (s *MemoryStore) LoadAssessment(_ context.Context, tenant, id string, revision values.RevisionToken) (CareerAssessmentRevision, error) {
	sequence, err := revisionNumber(revision)
	if err != nil {
		return CareerAssessmentRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.assessments[tenant][id][sequence]
	if !ok {
		return CareerAssessmentRevision{}, NewStoreError(StoreNotFoundCode, fmt.Sprintf("assessment %s revision %d", id, sequence))
	}
	return value, nil
}

func (s *MemoryStore) CurrentAssessment(_ context.Context, tenant, id string) (CareerAssessmentRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := latestAssessment(s.assessments[tenant][id])
	if !ok {
		return CareerAssessmentRevision{}, NewStoreError(StoreNotFoundCode, fmt.Sprintf("assessment %s", id))
	}
	return value, nil
}

func (s *MemoryStore) ListAssessments(_ context.Context, tenant, id string) ([]CareerAssessmentRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.assessments[tenant][id]
	if len(rows) == 0 {
		return nil, NewStoreError(StoreNotFoundCode, fmt.Sprintf("assessment %s", id))
	}
	out := make([]CareerAssessmentRevision, 0, len(rows))
	for _, value := range rows {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i].Revision.Sequence()
		b, _ := out[j].Revision.Sequence()
		return a < b
	})
	return out, nil
}

func clonePreference(value CareerPreferenceProfileRevision) CareerPreferenceProfileRevision {
	value.TargetRoleRefs = append([]values.EntityRef(nil), value.TargetRoleRefs...)
	return value
}
func cloneTarget(value TargetRoleProfileRevision) TargetRoleProfileRevision {
	value.Requirements = append([]RoleSkillRequirement(nil), value.Requirements...)
	return value
}
func cloneObjective(value DevelopmentObjectiveProfileRevision) DevelopmentObjectiveProfileRevision {
	value.SkillRefs = append([]values.EntityRef(nil), value.SkillRefs...)
	value.CompletionEvidenceRefs = append([]string(nil), value.CompletionEvidenceRefs...)
	return value
}

func latestPreference(rows map[uint64]CareerPreferenceProfileRevision) (CareerPreferenceProfileRevision, bool) {
	var out CareerPreferenceProfileRevision
	var max uint64
	for sequence, value := range rows {
		if sequence > max {
			max, out = sequence, value
		}
	}
	return out, max > 0
}
func latestTarget(rows map[uint64]TargetRoleProfileRevision) (TargetRoleProfileRevision, bool) {
	var out TargetRoleProfileRevision
	var max uint64
	for sequence, value := range rows {
		if sequence > max {
			max, out = sequence, value
		}
	}
	return out, max > 0
}
func latestObjective(rows map[uint64]DevelopmentObjectiveProfileRevision) (DevelopmentObjectiveProfileRevision, bool) {
	var out DevelopmentObjectiveProfileRevision
	var max uint64
	for sequence, value := range rows {
		if sequence > max {
			max, out = sequence, value
		}
	}
	return out, max > 0
}
func latestAssessment(rows map[uint64]CareerAssessmentRevision) (CareerAssessmentRevision, bool) {
	var out CareerAssessmentRevision
	var max uint64
	for sequence, value := range rows {
		if sequence > max {
			max, out = sequence, value
		}
	}
	return out, max > 0
}

// The explicit names make the canonical profile generation discoverable to
// callers that prefer the full aggregate name.
func (s *MemoryStore) SaveCareerPreference(ctx context.Context, tenant string, value CareerPreferenceProfileRevision, expected values.RevisionToken) error {
	return s.SavePreference(ctx, tenant, value, expected)
}
func (s *MemoryStore) SaveTargetRoleProfile(ctx context.Context, tenant string, value TargetRoleProfileRevision, expected values.RevisionToken) error {
	return s.SaveTargetRole(ctx, tenant, value, expected)
}
func (s *MemoryStore) SaveDevelopmentObjectiveProfile(ctx context.Context, tenant string, value DevelopmentObjectiveProfileRevision, expected values.RevisionToken) error {
	return s.SaveDevelopmentObjective(ctx, tenant, value, expected)
}
