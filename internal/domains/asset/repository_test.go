package asset

import "testing"

func TestCustodyStoreSatisfiesRepository(t *testing.T) {
	var repository Repository = NewCustodyStore()
	if repository == nil {
		t.Fatal("repository is nil")
	}
}
