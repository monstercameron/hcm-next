package asset

import "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"

// Repository is the persistence-neutral boundary for the asset inventory and
// custody streams. Inventory revisions are immutable; custody revisions are
// append-only and use the expected token as their compare-and-set boundary.
// The error-returning read methods let database adapters report storage and
// decode failures without making the kernel depend on a driver.
type Repository interface {
	RegisterInventory(InventoryRevision) error
	AppendCustody(values.EntityRef, values.RevisionToken, CustodyRevision) error
	CurrentCustody(values.EntityRef) (CustodyRevision, bool, error)
	HistoryCustody(values.EntityRef) ([]CustodyRevision, error)
}

var _ Repository = (*CustodyStore)(nil)

func (s *CustodyStore) RegisterInventory(inventory InventoryRevision) error {
	return s.Register(inventory)
}

func (s *CustodyStore) AppendCustody(asset values.EntityRef, expected values.RevisionToken, next CustodyRevision) error {
	return s.Apply(asset, expected, next)
}

func (s *CustodyStore) CurrentCustody(asset values.EntityRef) (CustodyRevision, bool, error) {
	current, ok := s.Current(asset)
	return current, ok, nil
}

func (s *CustodyStore) HistoryCustody(asset values.EntityRef) ([]CustodyRevision, error) {
	return s.History(asset), nil
}
