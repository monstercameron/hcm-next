package balance

import "testing"

func TestEntryStoreSatisfiesEntryRepository(t *testing.T) {
	// NewEntryStore returns a struct value, so a runtime nil comparison
	// could never fail; the assignment itself is the assertion.
	var _ EntryRepository = NewEntryStore()
}
