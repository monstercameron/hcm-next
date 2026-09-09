package taxprofile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrStoreInvalid   = errors.New("taxprofile: invalid store input")
	ErrStoreNotFound  = errors.New("taxprofile: stored revision not found")
	ErrStoreDuplicate = errors.New("taxprofile: stored revision already exists")
	ErrStoreStaleCAS  = errors.New("taxprofile: stale revision predecessor")
)

// StoreCode is the stable machine-readable classification of a persistence
// refusal. It is shared by the memory implementation and database adapter.
type StoreCode string

const (
	StoreCodeInvalid   StoreCode = "INVALID"
	StoreCodeNotFound  StoreCode = "NOT_FOUND"
	StoreCodeDuplicate StoreCode = "DUPLICATE_REVISION"
	StoreCodeStaleCAS  StoreCode = "STALE_CAS"
)

// StoreError is safe for callers to classify without depending on SQL error
// text. Expected and actual are populated for revision-chain conflicts.
type StoreError struct {
	Code             StoreCode
	Detail           string
	Expected, Actual uint64
}

func (e *StoreError) Error() string {
	if e == nil {
		return "taxprofile: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("taxprofile: %s", e.Code)
	}
	return fmt.Sprintf("taxprofile: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	switch e.Code {
	case StoreCodeInvalid:
		return ErrStoreInvalid
	case StoreCodeNotFound:
		return ErrStoreNotFound
	case StoreCodeDuplicate:
		return ErrStoreDuplicate
	case StoreCodeStaleCAS:
		return ErrStoreStaleCAS
	default:
		return nil
	}
}

// Store is the tenant-aware persistence port for tax-profile revisions. A
// profile save also stores its nested registration, election and exemption
// values; the individual methods are available to callers that ingest those
// revisions independently.
type Store interface {
	SaveProfile(context.Context, string, WorkerTaxProfileRevision) error
	LoadProfile(context.Context, string, string, uint64) (WorkerTaxProfileRevision, error)
	SaveRegistration(context.Context, string, TaxRegistrationRevision) error
	LoadRegistration(context.Context, string, string, uint64) (TaxRegistrationRevision, error)
	SaveElection(context.Context, string, WithholdingElectionRevision) error
	LoadElection(context.Context, string, string) (WithholdingElectionRevision, error)
	SaveExemption(context.Context, string, TaxExemptionRevision) error
	LoadExemption(context.Context, string, string) (TaxExemptionRevision, error)
}

// ElectionSuccessorStore is the persistence boundary required by callers
// that append corrections. Implementations must compare and advance the head
// atomically; a read followed by SaveElection is not an equivalent CAS.
type ElectionSuccessorStore interface {
	Store
	SaveElectionSuccessor(context.Context, string, string, WithholdingElectionRevision) error
}

// MemoryStore is the kernel-pure reference implementation of Store. It keeps
// detached immutable values and applies the same tenant and revision rules as
// the PostgreSQL adapter.
type MemoryStore struct {
	mu            sync.RWMutex
	profiles      map[string]WorkerTaxProfileRevision
	registrations map[string]TaxRegistrationRevision
	elections     map[string]WithholdingElectionRevision
	electionHeads map[string]string
	exemptions    map[string]TaxExemptionRevision
}

var _ Store = (*MemoryStore)(nil)
var _ ElectionSuccessorStore = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		profiles:      make(map[string]WorkerTaxProfileRevision),
		registrations: make(map[string]TaxRegistrationRevision),
		elections:     make(map[string]WithholdingElectionRevision),
		electionHeads: make(map[string]string),
		exemptions:    make(map[string]TaxExemptionRevision),
	}
}

func storeContext(ctx context.Context, tenant string) error {
	if ctx == nil {
		return &StoreError{Code: StoreCodeInvalid, Detail: "context is required"}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(tenant) == "" {
		return &StoreError{Code: StoreCodeInvalid, Detail: "tenant is required"}
	}
	return nil
}

func storeKey(tenant, id string, revision uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%d", tenant, id, revision)
}

func effectiveKey(tenant, id string, effective values.EffectiveInterval) string {
	start, _ := effective.StartInstant()
	return tenant + "\x00" + id + "\x00" + start.String()
}

func effectiveSortKey(effective values.EffectiveInterval) string {
	start, _ := effective.StartInstant()
	return start.String()
}

func invalidStore(detail string) error {
	return &StoreError{Code: StoreCodeInvalid, Detail: detail}
}

func notFoundStore(detail string) error {
	return &StoreError{Code: StoreCodeNotFound, Detail: detail}
}

func duplicateStore(detail string) error {
	return &StoreError{Code: StoreCodeDuplicate, Detail: detail}
}

func staleStore(detail string, expected, actual uint64) error {
	return &StoreError{Code: StoreCodeStaleCAS, Detail: detail, Expected: expected, Actual: actual}
}

func canonicalRegistration(in TaxRegistrationRevision) (TaxRegistrationRevision, error) {
	if in.CanonicalDigest == "" {
		return NewTaxRegistrationRevision(in)
	}
	if err := in.Validate(); err != nil {
		return TaxRegistrationRevision{}, err
	}
	return in, nil
}

func canonicalElection(in WithholdingElectionRevision) (WithholdingElectionRevision, error) {
	if in.CanonicalDigest == "" {
		return NewWithholdingElectionRevision(in)
	}
	if err := in.Validate(); err != nil {
		return WithholdingElectionRevision{}, err
	}
	return in, nil
}

func canonicalExemption(in TaxExemptionRevision) (TaxExemptionRevision, error) {
	if in.CanonicalDigest == "" {
		return NewTaxExemptionRevision(in)
	}
	if err := in.Validate(); err != nil {
		return TaxExemptionRevision{}, err
	}
	return in, nil
}

func canonicalProfile(in WorkerTaxProfileRevision) (WorkerTaxProfileRevision, error) {
	var err error
	if in.CanonicalDigest == "" {
		in, err = NewWorkerTaxProfileRevision(in)
	} else {
		err = in.Validate()
	}
	if err != nil {
		return WorkerTaxProfileRevision{}, err
	}
	for i := range in.Registrations {
		in.Registrations[i], err = canonicalRegistration(in.Registrations[i])
		if err != nil {
			return WorkerTaxProfileRevision{}, err
		}
	}
	for i := range in.Elections {
		in.Elections[i], err = canonicalElection(in.Elections[i])
		if err != nil {
			return WorkerTaxProfileRevision{}, err
		}
	}
	for i := range in.Exemptions {
		in.Exemptions[i], err = canonicalExemption(in.Exemptions[i])
		if err != nil {
			return WorkerTaxProfileRevision{}, err
		}
	}
	return in, nil
}

func cloneProfile(in WorkerTaxProfileRevision) WorkerTaxProfileRevision {
	in.ResidenceJurisdictions = append([]string(nil), in.ResidenceJurisdictions...)
	in.WorkJurisdictions = append([]string(nil), in.WorkJurisdictions...)
	in.Registrations = append([]TaxRegistrationRevision(nil), in.Registrations...)
	in.Elections = append([]WithholdingElectionRevision(nil), in.Elections...)
	in.Exemptions = append([]TaxExemptionRevision(nil), in.Exemptions...)
	for i := range in.Exemptions {
		in.Exemptions[i].EvidenceRefs = append([]string(nil), in.Exemptions[i].EvidenceRefs...)
	}
	return in
}

func cloneExemption(in TaxExemptionRevision) TaxExemptionRevision {
	in.EvidenceRefs = append([]string(nil), in.EvidenceRefs...)
	return in
}

func (s *MemoryStore) SaveProfile(ctx context.Context, tenant string, profile WorkerTaxProfileRevision) error {
	if err := storeContext(ctx, tenant); err != nil {
		return err
	}
	profile, err := canonicalProfile(profile)
	if err != nil {
		return invalidStore(err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := storeKey(tenant, profile.WorkerRef, profile.Revision)
	if _, ok := s.profiles[key]; ok {
		return duplicateStore(key)
	}
	if err := s.checkProfilePredecessor(tenant, profile); err != nil {
		return err
	}
	for _, registration := range profile.Registrations {
		if err := s.saveRegistrationLocked(tenant, registration); err != nil && !errors.Is(err, ErrStoreDuplicate) {
			return err
		}
	}
	for _, election := range profile.Elections {
		if err := s.saveElectionLocked(tenant, election); err != nil && !errors.Is(err, ErrStoreDuplicate) {
			return err
		}
		headKey := tenant + "\x00" + election.ElectionID
		if _, exists := s.electionHeads[headKey]; !exists {
			s.electionHeads[headKey] = election.CanonicalDigest
		}
	}
	for _, exemption := range profile.Exemptions {
		if err := s.saveExemptionLocked(tenant, exemption); err != nil && !errors.Is(err, ErrStoreDuplicate) {
			return err
		}
	}
	s.profiles[key] = cloneProfile(profile)
	return nil
}

func (s *MemoryStore) checkProfilePredecessor(tenant string, profile WorkerTaxProfileRevision) error {
	var latest WorkerTaxProfileRevision
	var found bool
	for key, candidate := range s.profiles {
		if strings.HasPrefix(key, tenant+"\x00"+profile.WorkerRef+"\x00") && (!found || candidate.Revision > latest.Revision) {
			latest, found = candidate, true
		}
	}
	if !found {
		if profile.Revision != 1 || profile.ParentRevision != 0 || profile.ParentDigest != "" {
			return staleStore("initial profile must be revision 1 without a parent", 0, 0)
		}
		return nil
	}
	if profile.Revision != latest.Revision+1 || profile.ParentRevision != latest.Revision || profile.ParentDigest != latest.CanonicalDigest {
		return staleStore("profile does not extend the current revision", latest.Revision, profile.Revision)
	}
	return nil
}

func (s *MemoryStore) LoadProfile(ctx context.Context, tenant, workerRef string, revision uint64) (WorkerTaxProfileRevision, error) {
	if err := storeContext(ctx, tenant); err != nil {
		return WorkerTaxProfileRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.profiles[storeKey(tenant, workerRef, revision)]
	if !ok {
		return WorkerTaxProfileRevision{}, notFoundStore(storeKey(tenant, workerRef, revision))
	}
	return cloneProfile(profile), nil
}

func (s *MemoryStore) SaveRegistration(ctx context.Context, tenant string, registration TaxRegistrationRevision) error {
	if err := storeContext(ctx, tenant); err != nil {
		return err
	}
	registration, err := canonicalRegistration(registration)
	if err != nil {
		return invalidStore(err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveRegistrationLocked(tenant, registration)
}

func (s *MemoryStore) saveRegistrationLocked(tenant string, registration TaxRegistrationRevision) error {
	key := storeKey(tenant, registration.RegistrationIDRef, registration.Revision)
	if _, ok := s.registrations[key]; ok {
		return duplicateStore(key)
	}
	var latest TaxRegistrationRevision
	var found bool
	for candidateKey, candidate := range s.registrations {
		if strings.HasPrefix(candidateKey, tenant+"\x00"+registration.RegistrationIDRef+"\x00") && (!found || candidate.Revision > latest.Revision) {
			latest, found = candidate, true
		}
	}
	if !found {
		if registration.Revision != 1 || registration.ParentRevision != 0 || registration.ParentDigest != "" {
			return staleStore("initial registration must be revision 1 without a parent", 0, registration.Revision)
		}
	} else if registration.Revision != latest.Revision+1 || registration.ParentRevision != latest.Revision || registration.ParentDigest != latest.CanonicalDigest {
		return staleStore("registration does not extend the current revision", latest.Revision, registration.Revision)
	}
	s.registrations[key] = registration
	return nil
}

func (s *MemoryStore) LoadRegistration(ctx context.Context, tenant, registrationID string, revision uint64) (TaxRegistrationRevision, error) {
	if err := storeContext(ctx, tenant); err != nil {
		return TaxRegistrationRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	registration, ok := s.registrations[storeKey(tenant, registrationID, revision)]
	if !ok {
		return TaxRegistrationRevision{}, notFoundStore(storeKey(tenant, registrationID, revision))
	}
	return registration, nil
}

func (s *MemoryStore) SaveElection(ctx context.Context, tenant string, election WithholdingElectionRevision) error {
	if err := storeContext(ctx, tenant); err != nil {
		return err
	}
	election, err := canonicalElection(election)
	if err != nil {
		return invalidStore(err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	headKey := tenant + "\x00" + election.ElectionID
	if _, exists := s.electionHeads[headKey]; exists {
		return &StoreError{Code: StoreCodeStaleCAS, Detail: "existing election requires SaveElectionSuccessor"}
	}
	if err := s.saveElectionLocked(tenant, election); err != nil {
		return err
	}
	s.electionHeads[headKey] = election.CanonicalDigest
	return nil
}

// SaveElectionSuccessor appends a new election only when expectedDigest is
// still the latest predecessor. The predecessor remains addressable and is
// never overwritten, making corrections safe for closed payroll periods.
func (s *MemoryStore) SaveElectionSuccessor(ctx context.Context, tenant, expectedDigest string, successor WithholdingElectionRevision) error {
	if err := storeContext(ctx, tenant); err != nil {
		return err
	}
	if strings.TrimSpace(expectedDigest) == "" {
		return invalidStore("expected predecessor digest is required")
	}
	canonical, err := canonicalElection(successor)
	if err != nil {
		return invalidStore(err.Error())
	}
	successor = canonical
	s.mu.Lock()
	defer s.mu.Unlock()
	headKey := tenant + "\x00" + successor.ElectionID
	if s.electionHeads[headKey] != expectedDigest {
		return &StoreError{Code: StoreCodeStaleCAS, Detail: ErrElectionCAS.Error()}
	}
	var predecessor WithholdingElectionRevision
	var found bool
	for key, candidate := range s.elections {
		if strings.HasPrefix(key, headKey+"\x00") && candidate.CanonicalDigest == expectedDigest {
			predecessor, found = candidate, true
			break
		}
	}
	if !found {
		return &StoreError{Code: StoreCodeStaleCAS, Detail: ErrElectionCAS.Error()}
	}
	if successor.WorkerRef != predecessor.WorkerRef || successor.Jurisdiction != predecessor.Jurisdiction {
		return invalidStore("successor identity does not match predecessor")
	}
	if err := s.saveElectionLocked(tenant, successor); err != nil {
		return err
	}
	s.electionHeads[headKey] = successor.CanonicalDigest
	return nil
}

func (s *MemoryStore) saveElectionLocked(tenant string, election WithholdingElectionRevision) error {
	key := effectiveKey(tenant, election.ElectionID, election.Effective)
	if _, ok := s.elections[key]; ok {
		return duplicateStore(key)
	}
	s.elections[key] = election
	return nil
}

func (s *MemoryStore) LoadElection(ctx context.Context, tenant, electionID string) (WithholdingElectionRevision, error) {
	if err := storeContext(ctx, tenant); err != nil {
		return WithholdingElectionRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	head := s.electionHeads[tenant+"\x00"+electionID]
	for key, election := range s.elections {
		if strings.HasPrefix(key, tenant+"\x00"+electionID+"\x00") && election.CanonicalDigest == head {
			return election, nil
		}
	}
	return WithholdingElectionRevision{}, notFoundStore(electionID)
}

func (s *MemoryStore) SaveExemption(ctx context.Context, tenant string, exemption TaxExemptionRevision) error {
	if err := storeContext(ctx, tenant); err != nil {
		return err
	}
	exemption, err := canonicalExemption(exemption)
	if err != nil {
		return invalidStore(err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveExemptionLocked(tenant, exemption)
}

func (s *MemoryStore) saveExemptionLocked(tenant string, exemption TaxExemptionRevision) error {
	key := effectiveKey(tenant, exemption.ExemptionID, exemption.Effective)
	if _, ok := s.exemptions[key]; ok {
		return duplicateStore(key)
	}
	s.exemptions[key] = cloneExemption(exemption)
	return nil
}

func (s *MemoryStore) LoadExemption(ctx context.Context, tenant, exemptionID string) (TaxExemptionRevision, error) {
	if err := storeContext(ctx, tenant); err != nil {
		return TaxExemptionRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var candidates []TaxExemptionRevision
	for key, exemption := range s.exemptions {
		if strings.HasPrefix(key, tenant+"\x00"+exemptionID+"\x00") {
			candidates = append(candidates, exemption)
		}
	}
	if len(candidates) == 0 {
		return TaxExemptionRevision{}, notFoundStore(exemptionID)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return effectiveSortKey(candidates[i].Effective) < effectiveSortKey(candidates[j].Effective)
	})
	return cloneExemption(candidates[len(candidates)-1]), nil
}
