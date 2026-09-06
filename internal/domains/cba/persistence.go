package cba

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Store is the tenant-aware persistence port for CBA revisions. A save adds
// an immutable revision and compares the caller's expected current revision;
// an empty expected revision means that the identity must not exist yet.
//
// Tenant is an opaque identifier at this boundary. The PostgreSQL adapter
// resolves it to the database tenant UUID, while the kernel remains unaware
// of database identifiers.
type Store interface {
	SaveAgreement(context.Context, string, AgreementRevision, string) error
	LoadAgreement(context.Context, string, string, string) (AgreementRevision, error)
	CurrentAgreement(context.Context, string, string) (AgreementRevision, error)
	ListAgreementRevisions(context.Context, string, string) ([]AgreementRevision, error)

	SaveBargainingUnit(context.Context, string, BargainingUnitRevision, string) error
	LoadBargainingUnit(context.Context, string, string, string) (BargainingUnitRevision, error)
	CurrentBargainingUnit(context.Context, string, string) (BargainingUnitRevision, error)
	ListBargainingUnitRevisions(context.Context, string, string) ([]BargainingUnitRevision, error)

	SaveMembership(context.Context, string, MembershipRevision, string) error
	LoadMembership(context.Context, string, string, string) (MembershipRevision, error)
	CurrentMembership(context.Context, string, string) (MembershipRevision, error)
	ListMembershipRevisions(context.Context, string, string) ([]MembershipRevision, error)

	SaveAgreementClause(context.Context, string, AgreementClauseRevision, string) error
	LoadAgreementClause(context.Context, string, string, string) (AgreementClauseRevision, error)
	CurrentAgreementClause(context.Context, string, string) (AgreementClauseRevision, error)
	ListAgreementClauseRevisions(context.Context, string, string) ([]AgreementClauseRevision, error)
}

var (
	// ErrStoreInvalid identifies a malformed tenant, revision, or domain row.
	ErrStoreInvalid = errors.New("cba: invalid store input")
	// ErrStoreNotFound identifies a missing tenant-scoped revision.
	ErrStoreNotFound = errors.New("cba: stored revision not found")
	// ErrStoreDuplicate identifies reuse of an immutable revision identity.
	ErrStoreDuplicate = errors.New("cba: duplicate revision")
	// ErrStoreStaleCAS identifies a stale expected current revision.
	ErrStoreStaleCAS = errors.New("cba: stale compare-and-swap")
)

// StoreErrorCode is the stable machine-readable classification of a store
// refusal.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
)

// StoreError is a typed store failure. Expected and Actual are populated for
// stale-CAS failures and contain only safe revision identifiers.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "cba: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("cba: %s", e.Code)
	}
	return fmt.Sprintf("cba: %s: %s", e.Code, e.Detail)
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

func storeInvalid(detail string) *StoreError {
	return &StoreError{Code: StoreInvalidCode, Detail: detail}
}

func storeNotFound(detail string) *StoreError {
	return &StoreError{Code: StoreNotFoundCode, Detail: detail}
}

func storeDuplicate(detail string) *StoreError {
	return &StoreError{Code: StoreDuplicateCode, Detail: detail}
}

func storeStale(expected, actual, detail string) *StoreError {
	return &StoreError{Code: StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func validateStoreContext(ctx context.Context, tenant string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(tenant) == "" {
		return storeInvalid("tenant id is required")
	}
	return nil
}

func parseStoreRevision(revision string) (int64, error) {
	value := strings.TrimSpace(revision)
	if value == "" {
		return 0, storeInvalid("revision is required")
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 1 {
		return 0, storeInvalid(fmt.Sprintf("revision %q must be a positive decimal integer", revision))
	}
	return number, nil
}

// MemoryStore is the kernel-pure reference implementation of Store. It
// preserves each tenant's complete immutable revision history.
type MemoryStore struct {
	mu          sync.RWMutex
	agreements  map[string]map[string]AgreementRevision
	units       map[string]map[string]BargainingUnitRevision
	memberships map[string]map[string]MembershipRevision
	clauses     map[string]map[string]AgreementClauseRevision
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore returns an empty in-memory CBA store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		agreements:  make(map[string]map[string]AgreementRevision),
		units:       make(map[string]map[string]BargainingUnitRevision),
		memberships: make(map[string]map[string]MembershipRevision),
		clauses:     make(map[string]map[string]AgreementClauseRevision),
	}
}

func revisionKey(tenant, id, revision string) string {
	return tenant + "\x00" + id + "\x00" + revision
}

func splitRevisionKey(key string) (string, int64, bool) {
	parts := strings.Split(key, "\x00")
	if len(parts) != 3 {
		return "", 0, false
	}
	number, err := strconv.ParseInt(parts[2], 10, 64)
	return parts[1], number, err == nil
}

func saveMemoryRevision[T any](ctx context.Context, store map[string]map[string]T, tenant, id, revision, expected string, value T, validate func(T) error) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	if id == "" {
		return storeInvalid("revision identity is required")
	}
	currentRevision, err := parseStoreRevision(revision)
	if err != nil {
		return err
	}
	if err := validate(value); err != nil {
		return storeInvalid(err.Error())
	}

	byRevision := store[tenant]
	if byRevision == nil {
		byRevision = make(map[string]T)
		store[tenant] = byRevision
	}
	key := revisionKey(tenant, id, strconv.FormatInt(currentRevision, 10))
	if _, exists := byRevision[key]; exists {
		return storeDuplicate(fmt.Sprintf("%s revision %d", id, currentRevision))
	}
	actual, hasCurrent := latestMemoryRevision(byRevision, id)
	if expected == "" {
		if hasCurrent {
			return storeStale(expected, strconv.FormatInt(actual, 10), fmt.Sprintf("%s already has a current revision", id))
		}
	} else if !hasCurrent || expected != strconv.FormatInt(actual, 10) || currentRevision <= actual {
		actualText := ""
		if hasCurrent {
			actualText = strconv.FormatInt(actual, 10)
		}
		return storeStale(expected, actualText, fmt.Sprintf("%s current revision is %s", id, actualText))
	}
	byRevision[key] = value
	return nil
}

func latestMemoryRevision[T any](values map[string]T, id string) (int64, bool) {
	var latest int64
	found := false
	for key := range values {
		storedID, revision, ok := splitRevisionKey(key)
		if ok && storedID == id && (!found || revision > latest) {
			latest, found = revision, true
		}
	}
	return latest, found
}

func loadMemoryRevision[T any](ctx context.Context, store map[string]map[string]T, tenant, id, revision string) (T, error) {
	var zero T
	if err := validateStoreContext(ctx, tenant); err != nil {
		return zero, err
	}
	number, err := parseStoreRevision(revision)
	if err != nil {
		return zero, err
	}
	value, ok := store[tenant][revisionKey(tenant, id, strconv.FormatInt(number, 10))]
	if !ok {
		return zero, storeNotFound(fmt.Sprintf("%s revision %s", id, revision))
	}
	return value, nil
}

func listMemoryRevisions[T any](ctx context.Context, store map[string]map[string]T, tenant, id string) ([]T, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return nil, err
	}
	type entry struct {
		revision int64
		value    T
	}
	var entries []entry
	for key, value := range store[tenant] {
		storedID, revision, ok := splitRevisionKey(key)
		if ok && storedID == id {
			entries = append(entries, entry{revision: revision, value: value})
		}
	}
	if len(entries) == 0 {
		return nil, storeNotFound(fmt.Sprintf("%s revisions", id))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].revision < entries[j].revision })
	out := make([]T, len(entries))
	for i := range entries {
		out[i] = entries[i].value
	}
	return out, nil
}

func (s *MemoryStore) SaveAgreement(ctx context.Context, tenant string, value AgreementRevision, expectedRevision string) error {
	if s == nil {
		return storeInvalid("nil memory store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveMemoryRevision(ctx, s.agreements, tenant, value.AgreementID, value.Revision, expectedRevision, value, ValidateAgreement)
}

func (s *MemoryStore) LoadAgreement(ctx context.Context, tenant, id, revision string) (AgreementRevision, error) {
	if s == nil {
		return AgreementRevision{}, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return loadMemoryRevision(ctx, s.agreements, tenant, id, revision)
}

func (s *MemoryStore) CurrentAgreement(ctx context.Context, tenant, id string) (AgreementRevision, error) {
	values, err := s.ListAgreementRevisions(ctx, tenant, id)
	if err != nil {
		return AgreementRevision{}, err
	}
	return values[len(values)-1], nil
}

func (s *MemoryStore) ListAgreementRevisions(ctx context.Context, tenant, id string) ([]AgreementRevision, error) {
	if s == nil {
		return nil, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return listMemoryRevisions(ctx, s.agreements, tenant, id)
}

func (s *MemoryStore) SaveBargainingUnit(ctx context.Context, tenant string, value BargainingUnitRevision, expectedRevision string) error {
	if s == nil {
		return storeInvalid("nil memory store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveMemoryRevision(ctx, s.units, tenant, value.UnitID, value.Revision, expectedRevision, value, ValidateUnit)
}

func (s *MemoryStore) LoadBargainingUnit(ctx context.Context, tenant, id, revision string) (BargainingUnitRevision, error) {
	if s == nil {
		return BargainingUnitRevision{}, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return loadMemoryRevision(ctx, s.units, tenant, id, revision)
}

func (s *MemoryStore) CurrentBargainingUnit(ctx context.Context, tenant, id string) (BargainingUnitRevision, error) {
	values, err := s.ListBargainingUnitRevisions(ctx, tenant, id)
	if err != nil {
		return BargainingUnitRevision{}, err
	}
	return values[len(values)-1], nil
}

func (s *MemoryStore) ListBargainingUnitRevisions(ctx context.Context, tenant, id string) ([]BargainingUnitRevision, error) {
	if s == nil {
		return nil, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return listMemoryRevisions(ctx, s.units, tenant, id)
}

func (s *MemoryStore) SaveMembership(ctx context.Context, tenant string, value MembershipRevision, expectedRevision string) error {
	if s == nil {
		return storeInvalid("nil memory store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveMemoryRevision(ctx, s.memberships, tenant, value.MembershipID, value.Revision, expectedRevision, value, ValidateMembership)
}

func (s *MemoryStore) LoadMembership(ctx context.Context, tenant, id, revision string) (MembershipRevision, error) {
	if s == nil {
		return MembershipRevision{}, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return loadMemoryRevision(ctx, s.memberships, tenant, id, revision)
}

func (s *MemoryStore) CurrentMembership(ctx context.Context, tenant, id string) (MembershipRevision, error) {
	values, err := s.ListMembershipRevisions(ctx, tenant, id)
	if err != nil {
		return MembershipRevision{}, err
	}
	return values[len(values)-1], nil
}

func (s *MemoryStore) ListMembershipRevisions(ctx context.Context, tenant, id string) ([]MembershipRevision, error) {
	if s == nil {
		return nil, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return listMemoryRevisions(ctx, s.memberships, tenant, id)
}

func (s *MemoryStore) SaveAgreementClause(ctx context.Context, tenant string, value AgreementClauseRevision, expectedRevision string) error {
	if s == nil {
		return storeInvalid("nil memory store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveMemoryRevision(ctx, s.clauses, tenant, value.ID, value.Revision, expectedRevision, cloneClause(value), func(value AgreementClauseRevision) error { return value.Validate() })
}

func (s *MemoryStore) LoadAgreementClause(ctx context.Context, tenant, id, revision string) (AgreementClauseRevision, error) {
	if s == nil {
		return AgreementClauseRevision{}, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, err := loadMemoryRevision(ctx, s.clauses, tenant, id, revision)
	if err != nil {
		return AgreementClauseRevision{}, err
	}
	return cloneClause(value), nil
}

func (s *MemoryStore) CurrentAgreementClause(ctx context.Context, tenant, id string) (AgreementClauseRevision, error) {
	values, err := s.ListAgreementClauseRevisions(ctx, tenant, id)
	if err != nil {
		return AgreementClauseRevision{}, err
	}
	return values[len(values)-1], nil
}

func (s *MemoryStore) ListAgreementClauseRevisions(ctx context.Context, tenant, id string) ([]AgreementClauseRevision, error) {
	if s == nil {
		return nil, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	values, err := listMemoryRevisions(ctx, s.clauses, tenant, id)
	if err != nil {
		return nil, err
	}
	for i := range values {
		values[i] = cloneClause(values[i])
	}
	return values, nil
}

func cloneClause(value AgreementClauseRevision) AgreementClauseRevision {
	value.JobCodes = append([]string(nil), value.JobCodes...)
	value.LocationIDs = append([]string(nil), value.LocationIDs...)
	return value
}

// The Revision suffix aliases keep the persistence vocabulary aligned with
// the physical table names while the shorter methods remain the primary port.
func (s *MemoryStore) SaveAgreementRevision(ctx context.Context, tenant string, value AgreementRevision, expected string) error {
	return s.SaveAgreement(ctx, tenant, value, expected)
}
func (s *MemoryStore) LoadAgreementRevision(ctx context.Context, tenant, id, revision string) (AgreementRevision, error) {
	return s.LoadAgreement(ctx, tenant, id, revision)
}
func (s *MemoryStore) CurrentAgreementRevision(ctx context.Context, tenant, id string) (AgreementRevision, error) {
	return s.CurrentAgreement(ctx, tenant, id)
}
func (s *MemoryStore) ListAgreementRevision(ctx context.Context, tenant, id string) ([]AgreementRevision, error) {
	return s.ListAgreementRevisions(ctx, tenant, id)
}
func (s *MemoryStore) SaveBargainingUnitRevision(ctx context.Context, tenant string, value BargainingUnitRevision, expected string) error {
	return s.SaveBargainingUnit(ctx, tenant, value, expected)
}
func (s *MemoryStore) LoadBargainingUnitRevision(ctx context.Context, tenant, id, revision string) (BargainingUnitRevision, error) {
	return s.LoadBargainingUnit(ctx, tenant, id, revision)
}
func (s *MemoryStore) CurrentBargainingUnitRevision(ctx context.Context, tenant, id string) (BargainingUnitRevision, error) {
	return s.CurrentBargainingUnit(ctx, tenant, id)
}
func (s *MemoryStore) ListBargainingUnitRevision(ctx context.Context, tenant, id string) ([]BargainingUnitRevision, error) {
	return s.ListBargainingUnitRevisions(ctx, tenant, id)
}
func (s *MemoryStore) SaveMembershipRevision(ctx context.Context, tenant string, value MembershipRevision, expected string) error {
	return s.SaveMembership(ctx, tenant, value, expected)
}
func (s *MemoryStore) LoadMembershipRevision(ctx context.Context, tenant, id, revision string) (MembershipRevision, error) {
	return s.LoadMembership(ctx, tenant, id, revision)
}
func (s *MemoryStore) CurrentMembershipRevision(ctx context.Context, tenant, id string) (MembershipRevision, error) {
	return s.CurrentMembership(ctx, tenant, id)
}
func (s *MemoryStore) ListMembershipRevision(ctx context.Context, tenant, id string) ([]MembershipRevision, error) {
	return s.ListMembershipRevisions(ctx, tenant, id)
}
func (s *MemoryStore) SaveAgreementClauseRevision(ctx context.Context, tenant string, value AgreementClauseRevision, expected string) error {
	return s.SaveAgreementClause(ctx, tenant, value, expected)
}
func (s *MemoryStore) LoadAgreementClauseRevision(ctx context.Context, tenant, id, revision string) (AgreementClauseRevision, error) {
	return s.LoadAgreementClause(ctx, tenant, id, revision)
}
func (s *MemoryStore) CurrentAgreementClauseRevision(ctx context.Context, tenant, id string) (AgreementClauseRevision, error) {
	return s.CurrentAgreementClause(ctx, tenant, id)
}
func (s *MemoryStore) ListAgreementClauseRevision(ctx context.Context, tenant, id string) ([]AgreementClauseRevision, error) {
	return s.ListAgreementClauseRevisions(ctx, tenant, id)
}

func expectedRevision(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Put* aliases provide insertion-oriented names for callers that do not use
// the Store interface's Save* vocabulary.
func (s *MemoryStore) PutAgreement(ctx context.Context, tenant string, value AgreementRevision, expected ...string) error {
	return s.SaveAgreement(ctx, tenant, value, expectedRevision(expected))
}
func (s *MemoryStore) PutBargainingUnit(ctx context.Context, tenant string, value BargainingUnitRevision, expected ...string) error {
	return s.SaveBargainingUnit(ctx, tenant, value, expectedRevision(expected))
}
func (s *MemoryStore) PutMembership(ctx context.Context, tenant string, value MembershipRevision, expected ...string) error {
	return s.SaveMembership(ctx, tenant, value, expectedRevision(expected))
}
func (s *MemoryStore) PutAgreementClause(ctx context.Context, tenant string, value AgreementClauseRevision, expected ...string) error {
	return s.SaveAgreementClause(ctx, tenant, value, expectedRevision(expected))
}
