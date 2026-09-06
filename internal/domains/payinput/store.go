package payinput

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is the tenant-aware persistence port for pay-input definition
// revisions and worker assignments. Definitions are appended through a
// compare-and-swap lineage; assignments are immutable bindings to one exact
// definition revision.
type Store interface {
	SaveDefinition(context.Context, string, Definition, uint64) error
	LoadDefinition(context.Context, string, string, uint64) (Definition, error)
	CurrentDefinition(context.Context, string, string) (Definition, error)
	ListDefinitionRevisions(context.Context, string, string) ([]Definition, error)
	SaveAssignment(context.Context, string, WorkerAssignment) error
	LoadAssignment(context.Context, string, string) (WorkerAssignment, error)
	ListAssignments(context.Context, string, string) ([]WorkerAssignment, error)
}

var (
	ErrStoreInvalid   = errors.New("payinput: invalid store input")
	ErrStoreNotFound  = errors.New("payinput: stored input not found")
	ErrStoreDuplicate = errors.New("payinput: duplicate revision")
	ErrStoreStaleCAS  = errors.New("payinput: stale compare-and-swap")
	ErrStoreReference = errors.New("payinput: definition reference not found")
)

// StoreErrorCode is the stable machine-readable classification of a store
// refusal. Adapters return these codes so callers do not inspect driver text.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
	StoreReferenceCode StoreErrorCode = "REFERENCE_NOT_FOUND"
)

// StoreError is a typed persistence failure. Expected and Actual are set for
// stale compare-and-swap failures.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "payinput: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("payinput: %s", e.Code)
	}
	return fmt.Sprintf("payinput: %s: %s", e.Code, e.Detail)
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
	case StoreReferenceCode:
		return ErrStoreReference
	default:
		return nil
	}
}

func storeError(code StoreErrorCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: detail}
}

// MemoryStore is the pure in-memory implementation of Store used by domain
// callers that do not need PostgreSQL. Its tenant key is deliberately part of
// every map path so it exercises the same port shape as the durable adapter.
type MemoryStore struct {
	mu          sync.RWMutex
	definitions map[string]map[string]map[uint64]Definition
	assignments map[string]map[string]WorkerAssignment
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		definitions: make(map[string]map[string]map[uint64]Definition),
		assignments: make(map[string]map[string]WorkerAssignment),
	}
}

func (s *MemoryStore) SaveDefinition(_ context.Context, tenantID string, definition Definition, expectedRevision uint64) error {
	if s == nil || strings.TrimSpace(tenantID) == "" {
		return storeError(StoreInvalidCode, "tenant id is required")
	}
	var err error
	if definition.CanonicalDigest == "" {
		definition, err = NewDefinition(definition)
	} else {
		err = definition.Validate()
	}
	if err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.definitions[tenantID]
	if byID == nil {
		byID = make(map[string]map[uint64]Definition)
		s.definitions[tenantID] = byID
	}
	byRevision := byID[definition.id()]
	if byRevision == nil {
		byRevision = make(map[uint64]Definition)
		byID[definition.id()] = byRevision
	}
	if _, exists := byRevision[definition.Revision]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("definition %s/%d", definition.id(), definition.Revision))
	}
	current, hasCurrent := latestDefinition(byRevision)
	if expectedRevision == 0 {
		if hasCurrent {
			return staleMemory("", current)
		}
		if definition.Revision != 1 || definition.SupersedesRevision != 0 {
			return storeError(StoreInvalidCode, "initial definition must be revision 1 without a predecessor")
		}
	} else if !hasCurrent || current.Revision != expectedRevision || definition.Revision != expectedRevision+1 || definition.SupersedesRevision != expectedRevision || definition.SupersedesDigest != current.CanonicalDigest {
		actual := ""
		if hasCurrent {
			actual = fmt.Sprint(current.Revision)
		}
		return staleMemory(fmt.Sprint(expectedRevision), currentWithRevision(actual, current, hasCurrent))
	}
	byRevision[definition.Revision] = definition
	return nil
}

func staleMemory(expected string, actual Definition) *StoreError {
	err := &StoreError{Code: StoreStaleCASCode, Detail: fmt.Sprintf("expected %q, actual %q", expected, actualRevision(actual)), Expected: expected, Actual: actualRevision(actual)}
	return err
}

func currentWithRevision(_ string, current Definition, ok bool) Definition {
	if !ok {
		return Definition{}
	}
	return current
}

func actualRevision(definition Definition) string {
	if definition.Revision == 0 {
		return ""
	}
	return fmt.Sprint(definition.Revision)
}

func latestDefinition(revisions map[uint64]Definition) (Definition, bool) {
	var out Definition
	var found bool
	for revision, candidate := range revisions {
		if !found || revision > out.Revision {
			out, found = candidate, true
		}
	}
	return out, found
}

func (s *MemoryStore) LoadDefinition(_ context.Context, tenantID, definitionID string, revision uint64) (Definition, error) {
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(definitionID) == "" || revision == 0 {
		return Definition{}, storeError(StoreInvalidCode, "tenant, definition id and positive revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	definition, ok := s.definitions[tenantID][definitionID][revision]
	if !ok {
		return Definition{}, storeError(StoreNotFoundCode, fmt.Sprintf("definition %s/%d", definitionID, revision))
	}
	return definition, nil
}

func (s *MemoryStore) CurrentDefinition(_ context.Context, tenantID, definitionID string) (Definition, error) {
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(definitionID) == "" {
		return Definition{}, storeError(StoreInvalidCode, "tenant and definition id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	definition, ok := latestDefinition(s.definitions[tenantID][definitionID])
	if !ok {
		return Definition{}, storeError(StoreNotFoundCode, fmt.Sprintf("definition %s", definitionID))
	}
	return definition, nil
}

func (s *MemoryStore) ListDefinitionRevisions(_ context.Context, tenantID, definitionID string) ([]Definition, error) {
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(definitionID) == "" {
		return nil, storeError(StoreInvalidCode, "tenant and definition id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byRevision := s.definitions[tenantID][definitionID]
	if len(byRevision) == 0 {
		return nil, storeError(StoreNotFoundCode, fmt.Sprintf("definition %s", definitionID))
	}
	out := make([]Definition, 0, len(byRevision))
	for _, definition := range byRevision {
		out = append(out, definition)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out, nil
}

func (s *MemoryStore) SaveAssignment(_ context.Context, tenantID string, assignment WorkerAssignment) error {
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(assignment.id()) == "" {
		return storeError(StoreInvalidCode, "tenant and assignment id are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ref := assignment.DefinitionRef
	if ref.ID == "" {
		ref = DefinitionRef{ID: assignment.DefinitionID, Version: assignment.DefinitionVersion, Digest: assignment.DefinitionDigest}
	}
	definition, found := findMemoryDefinition(s.definitions[tenantID], ref)
	if !found {
		return storeError(StoreReferenceCode, fmt.Sprintf("definition %s/%s is not cataloged", ref.ID, ref.Version))
	}
	bound, err := NewWorkerAssignment(definition, assignment)
	if err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	byID := s.assignments[tenantID]
	if byID == nil {
		byID = make(map[string]WorkerAssignment)
		s.assignments[tenantID] = byID
	}
	if _, exists := byID[assignment.id()]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("assignment %s", assignment.id()))
	}
	byID[bound.id()] = bound
	return nil
}

func findMemoryDefinition(byID map[string]map[uint64]Definition, ref DefinitionRef) (Definition, bool) {
	for _, definition := range byID[ref.ID] {
		if definition.Version == ref.Version && (ref.Digest == "" || definition.CanonicalDigest == ref.Digest) {
			return definition, true
		}
	}
	return Definition{}, false
}

func (s *MemoryStore) LoadAssignment(_ context.Context, tenantID, assignmentID string) (WorkerAssignment, error) {
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(assignmentID) == "" {
		return WorkerAssignment{}, storeError(StoreInvalidCode, "tenant and assignment id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	assignment, ok := s.assignments[tenantID][assignmentID]
	if !ok {
		return WorkerAssignment{}, storeError(StoreNotFoundCode, fmt.Sprintf("assignment %s", assignmentID))
	}
	return assignment, nil
}

func (s *MemoryStore) ListAssignments(_ context.Context, tenantID, workerRef string) ([]WorkerAssignment, error) {
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(workerRef) == "" {
		return nil, storeError(StoreInvalidCode, "tenant and worker reference are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []WorkerAssignment
	for _, assignment := range s.assignments[tenantID] {
		if assignment.WorkerRef == workerRef {
			out = append(out, assignment)
		}
	}
	if len(out) == 0 {
		return nil, storeError(StoreNotFoundCode, fmt.Sprintf("assignments for worker %s", workerRef))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id() < out[j].id() })
	return out, nil
}
