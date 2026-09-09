// Package asset owns the canonical equipment inventory and physical custody
// contract. Revisions are immutable facts; adapters are responsible for
// persisting them and enforcing the same compare-and-set boundary.
package asset

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidAsset      = errors.New("asset: invalid asset")
	ErrUnknownInventory  = errors.New("asset: unknown inventory")
	ErrAlreadyAssigned   = errors.New("asset: already assigned")
	ErrNotAssigned       = errors.New("asset: not assigned")
	ErrUnverifiedReturn  = errors.New("asset: return is not verified")
	ErrRevisionConflict  = errors.New("asset: custody revision conflict")
	ErrInvalidTransition = errors.New("asset: invalid custody transition")
	ErrTenantBoundary    = errors.New("asset: tenant boundary violation")
	ErrChronology        = errors.New("asset: custody chronology violation")
	ErrOffboardingOpen   = errors.New("asset: offboarding obligation remains open")
	ErrOperationConflict = errors.New("asset: idempotency operation payload conflict")
)

type Status string

const (
	Available     Status = "AVAILABLE"
	Assigned      Status = "ASSIGNED"
	Lost          Status = "LOST"
	ReturnPending Status = "RETURN_PENDING"
	Returned      Status = "RETURNED"
	Retired       Status = "RETIRED"
)

func (s Status) Valid() bool {
	return s == Available || s == Assigned || s == Lost || s == ReturnPending || s == Returned || s == Retired
}

// Receipt is an independently attributable handoff or return receipt. A
// return is not complete merely because the employee reported it.
type Receipt struct {
	ID       values.EntityRef
	Issuer   values.EntityRef
	IssuedAt time.Time
	Verified bool
	Evidence string
}

func (r Receipt) Validate() error {
	if err := r.ID.Validate(); err != nil {
		return fmt.Errorf("receipt id: %w", err)
	}
	if err := r.Issuer.Validate(); err != nil {
		return fmt.Errorf("receipt issuer: %w", err)
	}
	if r.IssuedAt.IsZero() || r.Evidence == "" {
		return errors.New("receipt issued time and evidence are required")
	}
	return nil
}

// InventoryRevision identifies an immutable, tenant-owned piece of equipment.
type InventoryRevision struct {
	InventoryID    values.EntityRef
	Owner          values.EntityRef
	Classification string
	SerialNumber   string
	Revision       values.RevisionToken
	EffectiveAt    time.Time
	Status         Status
}

func (i InventoryRevision) Validate() error {
	if err := requireKind(i.InventoryID, "inventory", "asset"); err != nil {
		return err
	}
	if err := i.Owner.Validate(); err != nil {
		return fmt.Errorf("owner: %w", err)
	}
	if i.InventoryID.Tenant != i.Owner.Tenant {
		return ErrTenantBoundary
	}
	if i.Classification == "" || i.SerialNumber == "" || i.EffectiveAt.IsZero() {
		return ErrInvalidAsset
	}
	if !i.Revision.IsSpecified() {
		return errors.New("asset revision is required")
	}
	if !i.Status.Valid() {
		return fmt.Errorf("invalid asset status %q", i.Status)
	}
	return nil
}

// CustodyRevision is the append-only physical custody stream for an asset.
type CustodyRevision struct {
	Asset           values.EntityRef
	Worker          values.EntityRef
	Location        string
	Condition       string
	AssigneeReceipt Receipt
	IssuerReceipt   Receipt
	Revision        values.RevisionToken
	EffectiveAt     time.Time
	Status          Status
}

// ExternalObservation is a provider's logical view. It is evidence for
// reconciliation only; it never changes physical custody or asset authority.
type ExternalObservation struct {
	Asset          values.EntityRef
	Provider       string
	ObservedStatus Status
	ObservedAt     time.Time
	ObservationID  string
}

type ProviderDrift struct {
	Asset         values.EntityRef
	Provider      string
	Authority     Status
	Observed      Status
	ObservationID string
	ObservedAt    time.Time
}

// OffboardingObligation remains open until every listed asset has an
// authoritative returned or retired custody revision.
type OffboardingObligation struct {
	Worker   values.EntityRef
	Assets   []values.EntityRef
	OpenedAt time.Time
	ClosedAt time.Time
}

type RecoveryResult struct {
	OperationID string
	Asset       values.EntityRef
	Applied     bool
}

type recoveryRecord struct {
	asset               values.EntityRef
	location, condition string
	expected, revision  values.RevisionToken
	at                  time.Time
	assignee, issuer    Receipt
	result              RecoveryResult
}

func (c CustodyRevision) Validate() error {
	if err := requireKind(c.Asset, "asset", "asset"); err != nil {
		return err
	}
	if err := c.Revision.Validate(); err != nil {
		return fmt.Errorf("custody revision: %w", err)
	}
	if c.Location == "" || c.Condition == "" || c.EffectiveAt.IsZero() || !c.Status.Valid() {
		return ErrInvalidAsset
	}
	if c.Status == Assigned || c.Status == ReturnPending || c.Status == Lost {
		if err := requireKind(c.Worker, "worker", "worker"); err != nil {
			return err
		}
		if c.Worker.Tenant != c.Asset.Tenant {
			return ErrTenantBoundary
		}
	}
	if c.Status == Assigned {
		if err := c.AssigneeReceipt.Validate(); err != nil {
			return fmt.Errorf("assignee receipt: %w", err)
		}
		if err := c.IssuerReceipt.Validate(); err != nil {
			return fmt.Errorf("issuer receipt: %w", err)
		}
	}
	if c.Status == Returned {
		if err := c.AssigneeReceipt.Validate(); err != nil || !c.AssigneeReceipt.Verified {
			return ErrUnverifiedReturn
		}
		if err := c.IssuerReceipt.Validate(); err != nil || !c.IssuerReceipt.Verified {
			return ErrUnverifiedReturn
		}
	}
	return nil
}

func requireKind(ref values.EntityRef, label, kind string) error {
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%s reference: %w", label, err)
	}
	if string(ref.Kind) != kind {
		return fmt.Errorf("%s reference kind is %q, want %q", label, ref.Kind, kind)
	}
	return nil
}

// CustodyStore is a small in-memory reference implementation of the CAS
// boundary. It is useful to adapters and tests; each successful mutation
// appends a revision and never overwrites an earlier fact.
type CustodyStore struct {
	mu          sync.Mutex
	assets      map[string]InventoryRevision
	current     map[string]CustodyRevision
	history     map[string][]CustodyRevision
	offboarding map[string]OffboardingObligation
	recoveries  map[string]recoveryRecord
}

func NewCustodyStore() *CustodyStore {
	return &CustodyStore{assets: map[string]InventoryRevision{}, current: map[string]CustodyRevision{}, history: map[string][]CustodyRevision{}, offboarding: map[string]OffboardingObligation{}, recoveries: map[string]recoveryRecord{}}
}

func (s *CustodyStore) Register(i InventoryRevision) error {
	if err := i.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := i.InventoryID.String()
	if _, ok := s.assets[key]; ok {
		return ErrInvalidAsset
	}
	s.assets[key] = i
	return nil
}

func (s *CustodyStore) Apply(asset values.EntityRef, expected values.RevisionToken, next CustodyRevision) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if next.Asset != asset {
		return ErrInvalidAsset
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyLocked(asset, expected, next)
}

func (s *CustodyStore) applyLocked(asset values.EntityRef, expected values.RevisionToken, next CustodyRevision) error {
	inv, ok := s.assets[asset.String()]
	if !ok {
		return ErrUnknownInventory
	}
	cur, has := s.current[asset.String()]
	if has && !cur.Revision.Equal(expected) {
		return ErrRevisionConflict
	}
	if !has && expected.IsSpecified() && !expected.Equal(inv.Revision) {
		return ErrRevisionConflict
	}
	if next.Status == Returned && (!next.AssigneeReceipt.Verified || !next.IssuerReceipt.Verified) {
		return ErrUnverifiedReturn
	}
	// Inventory and custody are separate append-only streams. A custody token
	// therefore need not share the inventory registration stream.
	if !next.Revision.IsSpecified() {
		return ErrRevisionConflict
	}
	if !has && next.Status != Assigned {
		return ErrInvalidTransition
	}
	if has {
		if cur.Status == Assigned && next.Status == Assigned {
			return ErrAlreadyAssigned
		}
		if !cur.EffectiveAt.Before(next.EffectiveAt) {
			return ErrChronology
		}
		order, err := cur.Revision.CompareInStream(next.Revision)
		if err != nil || order >= 0 {
			return ErrRevisionConflict
		}
		if (next.Status == Lost || next.Status == ReturnPending) && next.Worker != cur.Worker {
			return ErrNotAssigned
		}
		switch {
		case cur.Status != Assigned && next.Status == ReturnPending:
			return ErrNotAssigned
		case cur.Status != ReturnPending && cur.Status != Lost && next.Status == Returned:
			return ErrInvalidTransition
		case next.Status == Lost && cur.Status != Assigned:
			return ErrInvalidTransition
		case cur.Status == Lost && next.Status != Returned && next.Status != Lost:
			return ErrInvalidTransition
		}
	}
	if next.Status == Assigned && inv.Status == Retired {
		return ErrInvalidTransition
	}
	s.current[asset.String()] = next
	s.history[asset.String()] = append(s.history[asset.String()], next)
	return nil
}

// ReportLost appends an authoritative physical loss fact.
func (s *CustodyStore) ReportLost(asset, worker values.EntityRef, location, condition string, expected, revision values.RevisionToken, at time.Time) error {
	return s.Apply(asset, expected, CustodyRevision{Asset: asset, Worker: worker, Location: location, Condition: condition, Revision: revision, EffectiveAt: at, Status: Lost})
}

// RecoverAsset appends an authoritative physical recovery fact. The operation
// id makes retries safe while retaining the original chronology.
func (s *CustodyStore) RecoverAsset(operationID string, asset values.EntityRef, location, condition string, assignee, issuer Receipt, expected, revision values.RevisionToken, at time.Time) (RecoveryResult, error) {
	if operationID == "" {
		return RecoveryResult{}, ErrInvalidAsset
	}
	next := CustodyRevision{Asset: asset, Location: location, Condition: condition, AssigneeReceipt: assignee, IssuerReceipt: issuer, Revision: revision, EffectiveAt: at, Status: Returned}
	if err := next.Validate(); err != nil {
		return RecoveryResult{}, err
	}
	key := string(asset.Tenant) + "\x00" + operationID
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.recoveries[key]; ok {
		if prior.asset != asset || prior.location != location || prior.condition != condition || !prior.expected.Equal(expected) || !prior.revision.Equal(revision) || !prior.at.Equal(at) || prior.assignee != assignee || prior.issuer != issuer {
			return RecoveryResult{}, ErrOperationConflict
		}
		return prior.result, nil
	}
	if err := s.applyLocked(asset, expected, next); err != nil {
		return RecoveryResult{}, err
	}
	r := RecoveryResult{OperationID: operationID, Asset: asset, Applied: true}
	s.recoveries[key] = recoveryRecord{asset: asset, location: location, condition: condition, assignee: assignee, issuer: issuer, expected: expected, revision: revision, at: at, result: r}
	return r, nil
}

func (s *CustodyStore) OpenOffboarding(worker values.EntityRef, assets []values.EntityRef, at time.Time) (OffboardingObligation, error) {
	if err := requireKind(worker, "worker", "worker"); err != nil || at.IsZero() || len(assets) == 0 {
		return OffboardingObligation{}, ErrInvalidAsset
	}
	copyAssets := append([]values.EntityRef(nil), assets...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.offboarding[worker.String()]; ok && existing.ClosedAt.IsZero() {
		return OffboardingObligation{}, ErrOffboardingOpen
	}
	for _, a := range copyAssets {
		if a.Tenant != worker.Tenant {
			return OffboardingObligation{}, ErrTenantBoundary
		}
		if _, ok := s.assets[a.String()]; !ok {
			return OffboardingObligation{}, ErrUnknownInventory
		}
		custody, assigned := s.current[a.String()]
		if !assigned || custody.Worker != worker || (custody.Status != Assigned && custody.Status != Lost && custody.Status != ReturnPending) {
			return OffboardingObligation{}, ErrNotAssigned
		}
	}
	o := OffboardingObligation{Worker: worker, Assets: copyAssets, OpenedAt: at}
	s.offboarding[worker.String()] = o
	o.Assets = append([]values.EntityRef(nil), o.Assets...)
	return o, nil
}

func (s *CustodyStore) CloseOffboarding(worker values.EntityRef, at time.Time) error {
	if at.IsZero() {
		return ErrInvalidAsset
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.offboarding[worker.String()]
	if !ok || !o.ClosedAt.IsZero() || !o.OpenedAt.Before(at) {
		return ErrOffboardingOpen
	}
	for _, a := range o.Assets {
		c, exists := s.current[a.String()]
		if !exists || (c.Status != Returned && c.Status != Retired) || c.EffectiveAt.After(at) {
			return ErrOffboardingOpen
		}
	}
	o.ClosedAt = at
	s.offboarding[worker.String()] = o
	return nil
}

func (s *CustodyStore) Offboarding(worker values.EntityRef) (OffboardingObligation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.offboarding[worker.String()]
	o.Assets = append([]values.EntityRef(nil), o.Assets...)
	return o, ok
}

// ReconcileExternal compares provider evidence with the authoritative stream;
// it deliberately does not append custody revisions or alter inventory state.
func (s *CustodyStore) ReconcileExternal(o ExternalObservation) (ProviderDrift, bool, error) {
	if err := requireKind(o.Asset, "asset", "asset"); err != nil || o.Provider == "" || o.ObservationID == "" || o.ObservedAt.IsZero() || !o.ObservedStatus.Valid() {
		return ProviderDrift{}, false, ErrInvalidAsset
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.assets[o.Asset.String()]; !ok {
		return ProviderDrift{}, false, ErrUnknownInventory
	}
	c, ok := s.current[o.Asset.String()]
	authority := Available
	if ok {
		authority = c.Status
	}
	if authority == o.ObservedStatus {
		return ProviderDrift{}, false, nil
	}
	return ProviderDrift{Asset: o.Asset, Provider: o.Provider, Authority: authority, Observed: o.ObservedStatus, ObservationID: o.ObservationID, ObservedAt: o.ObservedAt}, true, nil
}

func (s *CustodyStore) Current(asset values.EntityRef) (CustodyRevision, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.current[asset.String()]
	return c, ok
}
func (s *CustodyStore) History(asset values.EntityRef) []CustodyRevision {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CustodyRevision(nil), s.history[asset.String()]...)
}

func (s *CustodyStore) Assign(asset, worker values.EntityRef, location, condition string, assignee, issuer Receipt, expected, revision values.RevisionToken, at time.Time) error {
	return s.Apply(asset, expected, CustodyRevision{Asset: asset, Worker: worker, Location: location, Condition: condition, AssigneeReceipt: assignee, IssuerReceipt: issuer, Revision: revision, EffectiveAt: at, Status: Assigned})
}

func (s *CustodyStore) BeginReturn(asset, worker values.EntityRef, location, condition string, expected, revision values.RevisionToken, at time.Time) error {
	return s.Apply(asset, expected, CustodyRevision{Asset: asset, Worker: worker, Location: location, Condition: condition, Revision: revision, EffectiveAt: at, Status: ReturnPending})
}

func (s *CustodyStore) CompleteReturn(asset values.EntityRef, location, condition string, assignee, issuer Receipt, expected, revision values.RevisionToken, at time.Time) error {
	if !assignee.Verified || !issuer.Verified {
		return ErrUnverifiedReturn
	}
	return s.Apply(asset, expected, CustodyRevision{Asset: asset, Location: location, Condition: condition, AssigneeReceipt: assignee, IssuerReceipt: issuer, Revision: revision, EffectiveAt: at, Status: Returned})
}
