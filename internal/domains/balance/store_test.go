package balance

import "testing"

func TestEntryStoreSatisfiesEntryRepository(t *testing.T) {
	var repository EntryRepository = NewEntryStore()
	if repository == nil {
		t.Fatal("entry store repository is nil")
	}
}
