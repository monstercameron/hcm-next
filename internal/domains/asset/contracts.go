// Package asset owns the canonical equipment inventory and physical custody
// contract. Revisions are immutable facts; adapters are responsible for
// persisting them and enforcing the same compare-and-set boundary.
package asset

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
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
)

type Status string

type AssetStatus = Status

const (
	Available     Status = "AVAILABLE"
	Assigned      Status = "ASSIGNED"
	Lost          Status = "LOST"
	ReturnPending Status = "RETURN_PENDING"
	Returned      Status = "RETURNED"
	Retired       Status = "RETIRED"
)

// Descriptive aliases make the wire vocabulary explicit to callers that use
// the domain as a contract package.
const (
	AssetAvailable     = Available
	AssetAssigned      = Assigned
	AssetLost          = Lost
	AssetReturnPending = ReturnPending
	AssetReturned      = Returned
	AssetRetired       = Retired
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

type Inventory = InventoryRevision
type AssetInventory = InventoryRevision

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

type Custody = CustodyRevision
type AssignmentRevision = CustodyRevision
type ReturnRevision = CustodyRevision

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
	if c.Status == Assigned || c.Status == ReturnPending {
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
	if c.Status == Returned && (!c.AssigneeReceipt.Verified || !c.IssuerReceipt.Verified) {
		return ErrUnverifiedReturn
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
	mu      sync.Mutex
	assets  map[string]InventoryRevision
	current map[string]CustodyRevision
	history map[string][]CustodyRevision
}

func NewCustodyStore() *CustodyStore {
	return &CustodyStore{assets: map[string]InventoryRevision{}, current: map[string]CustodyRevision{}, history: map[string][]CustodyRevision{}}
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
	if has && cur.Status == Assigned && next.Status == Assigned {
		return ErrAlreadyAssigned
	}
	if next.Status == Returned && (!next.AssigneeReceipt.Verified || !next.IssuerReceipt.Verified) {
		return ErrUnverifiedReturn
	}
	// Inventory and custody are separate append-only streams. A custody token
	// therefore need not share the inventory registration stream.
	if !next.Revision.IsSpecified() {
		return ErrRevisionConflict
	}
	if has {
		switch {
		case cur.Status == Assigned && next.Status == Assigned:
			return ErrAlreadyAssigned
		case cur.Status != Assigned && next.Status == ReturnPending:
			return ErrNotAssigned
		case cur.Status != ReturnPending && next.Status == Returned:
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
