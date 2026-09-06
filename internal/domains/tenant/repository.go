package tenant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/monstercameron/hcm-next/internal/domains/tenant/govauth"
)

// StoreCode is the stable classification of a tenant persistence result.
type StoreCode string

const (
	StoreCodeInvalid   StoreCode = "INVALID"
	StoreCodeDuplicate StoreCode = "DUPLICATE"
	StoreCodeNotFound  StoreCode = "NOT_FOUND"
	StoreCodeStaleCAS  StoreCode = "STALE_CAS"
)

// StoreError is a typed persistence error. Callers can use errors.Is with the
// exported sentinels or inspect CodeOf when they need the stable code.
type StoreError struct {
	Code   StoreCode
	Detail string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "tenant: persistence error"
	}
	if e.Detail == "" {
		return "tenant: " + string(e.Code)
	}
	return fmt.Sprintf("tenant: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Is(target error) bool {
	t, ok := target.(*StoreError)
	return ok && e != nil && t != nil && e.Code == t.Code
}

var (
	ErrInvalid   = &StoreError{Code: StoreCodeInvalid}
	ErrDuplicate = &StoreError{Code: StoreCodeDuplicate}
	ErrNotFound  = &StoreError{Code: StoreCodeNotFound}
	ErrStaleCAS  = &StoreError{Code: StoreCodeStaleCAS}
)

func storeError(code StoreCode, detail string) error {
	return &StoreError{Code: code, Detail: detail}
}

// CodeOf returns the stable persistence code carried by err, if any.
func CodeOf(err error) StoreCode {
	var typed *StoreError
	if errors.As(err, &typed) && typed != nil {
		return typed.Code
	}
	return ""
}

// ProvisioningEventRecord preserves the database sequence alongside the
// domain event. The sequence is storage ordering, not part of the event's
// semantic digest.
type ProvisioningEventRecord struct {
	TenantID string
	Sequence uint64
	Event    ProvisioningEvent
}

// Repository is the persistence port for tenant placement, provisioning
// evidence and government-authorization profile revisions. Implementations
// receive a tenant UUID separately from domain values because the database
// identity is not the same thing as a tenant's human-readable key.
type Repository interface {
	SavePlacement(ctx context.Context, tenantID string, placement Placement, expectedEpoch uint64) error
	LoadPlacement(ctx context.Context, tenantID string) (Placement, error)
	AppendProvisioningEvent(ctx context.Context, tenantID string, event ProvisioningEvent, sequence uint64) error
	ListProvisioningEvents(ctx context.Context, tenantID string) ([]ProvisioningEventRecord, error)
	SaveGovernmentAuthorizationProfile(ctx context.Context, tenantID string, profile govauth.GovernmentAuthorizationProfile) error
	LoadGovernmentAuthorizationProfile(ctx context.Context, tenantID string, integrationID string, revision uint64) (govauth.GovernmentAuthorizationProfile, error)
	LatestGovernmentAuthorizationProfile(ctx context.Context, tenantID string, integrationID string) (govauth.GovernmentAuthorizationProfile, error)
}

// MemoryStore is the kernel-pure implementation used by callers that do not
// need PostgreSQL. Its write rules mirror the durable repository: placements
// are epoch-fenced, events are append-only by sequence, and profile revisions
// cannot be replaced.
type MemoryStore struct {
	mu         sync.RWMutex
	placements map[string]Placement
	events     map[string][]ProvisioningEventRecord
	profiles   map[profileKey]govauth.GovernmentAuthorizationProfile
}

type profileKey struct {
	tenantID      string
	integrationID string
	revision      uint64
}

// NewMemoryStore returns an empty repository.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		placements: make(map[string]Placement),
		events:     make(map[string][]ProvisioningEventRecord),
		profiles:   make(map[profileKey]govauth.GovernmentAuthorizationProfile),
	}
}

func (s *MemoryStore) SavePlacement(_ context.Context, tenantID string, placement Placement, expectedEpoch uint64) error {
	if s == nil || tenantID == "" {
		return storeError(StoreCodeInvalid, "tenant id is required")
	}
	if err := placement.Validate(); err != nil {
		return err
	}
	if err := validSignature(placement.Signature); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.placements[tenantID]
	if !exists {
		if expectedEpoch != 0 {
			return storeError(StoreCodeStaleCAS, "placement does not exist")
		}
		s.placements[tenantID] = placement
		return nil
	}
	if expectedEpoch == 0 {
		return storeError(StoreCodeDuplicate, "placement already exists")
	}
	if current.Epoch != expectedEpoch || placement.Epoch <= current.Epoch {
		return storeError(StoreCodeStaleCAS, "placement epoch is stale")
	}
	s.placements[tenantID] = placement
	return nil
}

func (s *MemoryStore) LoadPlacement(_ context.Context, tenantID string) (Placement, error) {
	if s == nil || tenantID == "" {
		return Placement{}, storeError(StoreCodeInvalid, "tenant id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	placement, ok := s.placements[tenantID]
	if !ok {
		return Placement{}, storeError(StoreCodeNotFound, "placement does not exist")
	}
	return placement, nil
}

func (s *MemoryStore) AppendProvisioningEvent(_ context.Context, tenantID string, event ProvisioningEvent, sequence uint64) error {
	if s == nil || tenantID == "" || event.Tenant == "" || sequence == 0 {
		return storeError(StoreCodeInvalid, "tenant, event tenant and positive sequence are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.events[tenantID] {
		if existing.Sequence == sequence {
			return storeError(StoreCodeDuplicate, "provisioning event sequence already exists")
		}
	}
	s.events[tenantID] = append(s.events[tenantID], ProvisioningEventRecord{
		TenantID: tenantID,
		Sequence: sequence,
		Event:    event,
	})
	return nil
}

func (s *MemoryStore) ListProvisioningEvents(_ context.Context, tenantID string) ([]ProvisioningEventRecord, error) {
	if s == nil || tenantID == "" {
		return nil, storeError(StoreCodeInvalid, "tenant id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]ProvisioningEventRecord(nil), s.events[tenantID]...)
	return result, nil
}

func (s *MemoryStore) SaveGovernmentAuthorizationProfile(_ context.Context, tenantID string, profile govauth.GovernmentAuthorizationProfile) error {
	if s == nil || tenantID == "" {
		return storeError(StoreCodeInvalid, "tenant id is required")
	}
	sealed, err := sealProfile(tenantID, profile)
	if err != nil {
		return err
	}
	key := profileKey{tenantID: tenantID, integrationID: sealed.IntegrationID, revision: sealed.Revision}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.profiles[key]; exists {
		return storeError(StoreCodeDuplicate, "government authorization revision already exists")
	}
	s.profiles[key] = sealed
	return nil
}

func (s *MemoryStore) LoadGovernmentAuthorizationProfile(_ context.Context, tenantID string, integrationID string, revision uint64) (govauth.GovernmentAuthorizationProfile, error) {
	if s == nil || tenantID == "" || integrationID == "" || revision == 0 {
		return govauth.GovernmentAuthorizationProfile{}, storeError(StoreCodeInvalid, "tenant, integration and revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.profiles[profileKey{tenantID: tenantID, integrationID: integrationID, revision: revision}]
	if !ok {
		return govauth.GovernmentAuthorizationProfile{}, storeError(StoreCodeNotFound, "government authorization revision does not exist")
	}
	return profile, nil
}

func (s *MemoryStore) LatestGovernmentAuthorizationProfile(_ context.Context, tenantID string, integrationID string) (govauth.GovernmentAuthorizationProfile, error) {
	if s == nil || tenantID == "" || integrationID == "" {
		return govauth.GovernmentAuthorizationProfile{}, storeError(StoreCodeInvalid, "tenant and integration are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest govauth.GovernmentAuthorizationProfile
	var found bool
	for key, profile := range s.profiles {
		if key.tenantID == tenantID && key.integrationID == integrationID && (!found || key.revision > latest.Revision) {
			latest, found = profile, true
		}
	}
	if !found {
		return govauth.GovernmentAuthorizationProfile{}, storeError(StoreCodeNotFound, "government authorization revision does not exist")
	}
	return latest, nil
}

func validSignature(signature string) error {
	decoded, err := hex.DecodeString(signature)
	if err != nil || len(decoded) != sha256.Size {
		return storeError(StoreCodeInvalid, "placement signature must be a 32-byte hexadecimal signature")
	}
	return nil
}

func sealProfile(tenantID string, profile govauth.GovernmentAuthorizationProfile) (govauth.GovernmentAuthorizationProfile, error) {
	if profile.TenantID != tenantID {
		return govauth.GovernmentAuthorizationProfile{}, storeError(StoreCodeInvalid, "profile tenant_id must match the storage tenant")
	}
	if profile.Revision == 0 {
		profile.Revision = profile.Version
	}
	if profile.IntegrationID == "" {
		profile.IntegrationID = profile.IntegrationRef
	}
	if len(profile.ApplicablePrograms) == 0 {
		profile.ApplicablePrograms = append([]govauth.Program(nil), profile.Programs...)
	}
	if len(profile.InheritedControls) == 0 {
		profile.InheritedControls = append([]govauth.ControlInheritanceReference(nil), profile.InheritedControlReferences...)
	}
	if len(profile.Evidence) == 0 {
		profile.Evidence = append([]govauth.EvidencePointer(nil), profile.EvidenceCatalog...)
	}
	if profile.FedRAMP == nil {
		profile.FedRAMP = profile.FedRAMPPath
	}
	if profile.CMS == nil {
		profile.CMS = profile.MARSEResources
	}
	if !profile.ReviewDate.IsZero() && (profile.ReviewDate.Hour() != 0 || profile.ReviewDate.Minute() != 0 || profile.ReviewDate.Second() != 0 || profile.ReviewDate.Nanosecond() != 0) {
		return govauth.GovernmentAuthorizationProfile{}, storeError(StoreCodeInvalid, "profile review_date must be midnight because storage preserves a date")
	}
	sealed, err := govauth.NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		return govauth.GovernmentAuthorizationProfile{}, err
	}
	return sealed, nil
}

var _ Repository = (*MemoryStore)(nil)
