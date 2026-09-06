package custom

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is the tenant-aware persistence port for custom definitions and
// effective-dated records. The tenant identity is explicit because the
// domain's value types intentionally do not carry a database tenant UUID.
// Implementations must append immutable definition versions and must never
// replace an existing version in place.
type Store interface {
	SaveObjectDefinition(context.Context, string, CustomObjectDefinition, uint64) error
	LoadObjectDefinition(context.Context, string, string, string, uint64) (CustomObjectDefinition, error)
	SaveRelationshipDefinition(context.Context, string, CustomRelationshipDefinition, uint64) error
	LoadRelationshipDefinition(context.Context, string, string, string, uint64) (CustomRelationshipDefinition, error)
	SaveRecordRevision(context.Context, string, CustomRecordRevision) error
	ListRecordRevisions(context.Context, string, string) ([]CustomRecordRevision, error)
}

var (
	ErrStoreInvalid   = errors.New("custom: invalid store input")
	ErrStoreNotFound  = errors.New("custom: stored value not found")
	ErrStoreDuplicate = errors.New("custom: duplicate revision")
	ErrStoreStaleCAS  = errors.New("custom: stale compare-and-swap")
	ErrStoreReference = errors.New("custom: stored reference not found")
	ErrStoreIntegrity = errors.New("custom: stored value failed integrity validation")
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
	StoreIntegrityCode StoreErrorCode = "INTEGRITY_VIOLATION"
)

// StoreError is a typed persistence refusal. Expected and Actual are filled
// for stale compare-and-set failures and are safe revision identifiers.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual uint64
}

func (e *StoreError) Error() string {
	if e == nil {
		return "custom: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("custom: %s", e.Code)
	}
	return fmt.Sprintf("custom: %s: %s", e.Code, e.Detail)
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
	case StoreIntegrityCode:
		return ErrStoreIntegrity
	default:
		return nil
	}
}

func newStoreError(code StoreErrorCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: detail}
}

func staleStoreError(expected, actual uint64, detail string) *StoreError {
	return &StoreError{Code: StoreStaleCASCode, Detail: detail, Expected: expected, Actual: actual}
}

// ValidatePersistableRecord applies the definition-side policy checks that
// must happen before a record reaches a persistence adapter. The database
// stores field values, but it is not the policy engine: classifications,
// residency and retention remain the definition's governed kernel data.
func ValidatePersistableRecord(def CustomObjectDefinition, record CustomRecordRevision) error {
	if err := def.Validate(); err != nil {
		return fmt.Errorf("%w: definition: %v", ErrStoreInvalid, err)
	}
	if _, err := ResolvePolicy(def); err != nil {
		return fmt.Errorf("%w: policy: %v", ErrStoreInvalid, err)
	}
	if err := record.Validate(def); err != nil {
		return fmt.Errorf("%w: record: %v", ErrStoreInvalid, err)
	}
	if !record.Known.Instant().IsSet() {
		return fmt.Errorf("%w: known_at is required for persistence", ErrStoreInvalid)
	}
	for name, value := range record.FieldValues {
		field, ok := def.Fields[name]
		if !ok {
			return fmt.Errorf("%w: field %q is not declared", ErrStoreInvalid, name)
		}
		if value.FieldName != "" && value.FieldName != name {
			return fmt.Errorf("%w: field value %q names %q", ErrStoreInvalid, name, value.FieldName)
		}
		if value.Type != field.Type {
			return fmt.Errorf("%w: field %q type %q does not match %q", ErrStoreInvalid, name, value.Type, field.Type)
		}
	}
	return nil
}

// MemoryStore is the kernel-pure reference implementation of Store. It keeps
// tenant partitions separate and mirrors the durable adapter's CAS rules.
type MemoryStore struct {
	mu            sync.RWMutex
	objects       map[string]CustomObjectDefinition
	relationships map[string]CustomRelationshipDefinition
	records       map[string][]CustomRecordRevision
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore returns an empty in-memory custom persistence store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		objects:       make(map[string]CustomObjectDefinition),
		relationships: make(map[string]CustomRelationshipDefinition),
		records:       make(map[string][]CustomRecordRevision),
	}
}

func (s *MemoryStore) SaveObjectDefinition(ctx context.Context, tenant string, definition CustomObjectDefinition, expectedVersion uint64) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	if err := definition.Validate(); err != nil {
		return newStoreError(StoreInvalidCode, err.Error())
	}
	key := objectKey(tenant, definition.Kind, definition.Namespace, definition.Version)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.objects[key]; exists {
		return newStoreError(StoreDuplicateCode, fmt.Sprintf("object definition %s/%s/%d", definition.Kind, definition.Namespace, definition.Version))
	}
	actual := s.currentObjectVersionLocked(tenant, definition.Kind, definition.Namespace)
	if err := checkNextVersion(expectedVersion, actual, definition.Version, "object definition"); err != nil {
		return err
	}
	s.objects[key] = cloneObjectDefinition(definition)
	return nil
}

func (s *MemoryStore) LoadObjectDefinition(ctx context.Context, tenant, kind, namespace string, version uint64) (CustomObjectDefinition, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return CustomObjectDefinition{}, err
	}
	if kind == "" || namespace == "" || version == 0 {
		return CustomObjectDefinition{}, newStoreError(StoreInvalidCode, "kind, namespace and positive version are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	definition, ok := s.objects[objectKey(tenant, kind, namespace, version)]
	if !ok {
		return CustomObjectDefinition{}, newStoreError(StoreNotFoundCode, fmt.Sprintf("object definition %s/%s/%d", kind, namespace, version))
	}
	return cloneObjectDefinition(definition), nil
}

func (s *MemoryStore) SaveRelationshipDefinition(ctx context.Context, tenant string, definition CustomRelationshipDefinition, expectedVersion uint64) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	if err := definition.Validate(); err != nil {
		return newStoreError(StoreInvalidCode, err.Error())
	}
	key := relationshipKey(tenant, definition.Name, definition.Namespace, definition.Version)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.relationships[key]; exists {
		return newStoreError(StoreDuplicateCode, fmt.Sprintf("relationship definition %s/%s/%d", definition.Name, definition.Namespace, definition.Version))
	}
	actual := s.currentRelationshipVersionLocked(tenant, definition.Name, definition.Namespace)
	if err := checkNextVersion(expectedVersion, actual, definition.Version, "relationship definition"); err != nil {
		return err
	}
	s.relationships[key] = definition
	return nil
}

func (s *MemoryStore) LoadRelationshipDefinition(ctx context.Context, tenant, name, namespace string, version uint64) (CustomRelationshipDefinition, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return CustomRelationshipDefinition{}, err
	}
	if name == "" || namespace == "" || version == 0 {
		return CustomRelationshipDefinition{}, newStoreError(StoreInvalidCode, "name, namespace and positive version are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	definition, ok := s.relationships[relationshipKey(tenant, name, namespace, version)]
	if !ok {
		return CustomRelationshipDefinition{}, newStoreError(StoreNotFoundCode, fmt.Sprintf("relationship definition %s/%s/%d", name, namespace, version))
	}
	return definition, nil
}

func (s *MemoryStore) SaveRecordRevision(ctx context.Context, tenant string, record CustomRecordRevision) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	definition, err := s.LoadObjectDefinition(ctx, tenant, record.ObjectKind, record.Namespace, record.DefinitionVersion)
	if err != nil {
		if errors.Is(err, ErrStoreNotFound) {
			return &StoreError{Code: StoreReferenceCode, Detail: err.Error()}
		}
		return err
	}
	if err := ValidatePersistableRecord(definition, record); err != nil {
		return err
	}
	key := persistenceRecordKey(tenant, record)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, prior := range s.records[key] {
		if prior.Digest() == record.Digest() {
			return newStoreError(StoreDuplicateCode, "record revision already exists")
		}
	}
	s.records[key] = append(s.records[key], clonePersistedRecord(record))
	return nil
}

func (s *MemoryStore) ListRecordRevisions(ctx context.Context, tenant, objectID string) ([]CustomRecordRevision, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return nil, err
	}
	if objectID == "" {
		return nil, newStoreError(StoreInvalidCode, "object id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []CustomRecordRevision
	prefix := tenant + "\x00" + objectID + "\x00"
	for key, records := range s.records {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		for _, record := range records {
			out = append(out, clonePersistedRecord(record))
		}
	}
	if len(out) == 0 {
		return nil, newStoreError(StoreNotFoundCode, fmt.Sprintf("record %s", objectID))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Effective.String() < out[j].Effective.String() })
	return out, nil
}

func validateStoreContext(ctx context.Context, tenant string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(tenant) == "" {
		return newStoreError(StoreInvalidCode, "tenant id is required")
	}
	return nil
}

func checkNextVersion(expected, actual, version uint64, kind string) error {
	switch {
	case expected == 0 && actual != 0:
		return staleStoreError(expected, actual, fmt.Sprintf("%s already has current version %d", kind, actual))
	case expected != 0 && actual != expected:
		return staleStoreError(expected, actual, fmt.Sprintf("%s current version is %d", kind, actual))
	case expected != 0 && version != expected+1:
		return staleStoreError(expected, actual, fmt.Sprintf("%s version %d does not supersede %d", kind, version, expected))
	}
	return nil
}

func objectKey(tenant, kind, namespace string, version uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", tenant, kind, namespace, version)
}

func relationshipKey(tenant, name, namespace string, version uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", tenant, name, namespace, version)
}

func persistenceRecordKey(tenant string, record CustomRecordRevision) string {
	return tenant + "\x00" + record.ObjectID + "\x00" + record.ObjectKind + "\x00" + record.Namespace + "\x00" + record.Effective.String()
}

func (s *MemoryStore) currentObjectVersionLocked(tenant, kind, namespace string) uint64 {
	var current uint64
	for key, definition := range s.objects {
		if strings.HasPrefix(key, tenant+"\x00"+kind+"\x00"+namespace+"\x00") && definition.Version > current {
			current = definition.Version
		}
	}
	return current
}

func (s *MemoryStore) currentRelationshipVersionLocked(tenant, name, namespace string) uint64 {
	var current uint64
	for key, definition := range s.relationships {
		if strings.HasPrefix(key, tenant+"\x00"+name+"\x00"+namespace+"\x00") && definition.Version > current {
			current = definition.Version
		}
	}
	return current
}

func cloneObjectDefinition(definition CustomObjectDefinition) CustomObjectDefinition {
	fields := definition.Fields
	definition.Fields = make(map[string]FieldDefinition, len(definition.Fields))
	for name, field := range fields {
		definition.Fields[name] = field
	}
	return definition
}

func clonePersistedRecord(record CustomRecordRevision) CustomRecordRevision {
	fieldValues := record.FieldValues
	record.FieldValues = make(map[string]TypedValue, len(record.FieldValues))
	for name, value := range fieldValues {
		record.FieldValues[name] = value
	}
	return record
}
