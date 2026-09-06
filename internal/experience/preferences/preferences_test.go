package preferences

import "testing"

func TestNormalizeUserProvidesBoundedCollectionDefaults(t *testing.T) {
	value := NormalizeUser(User{Tables: map[string]TablePreferences{"custom": {PageSize: 500}}})
	for _, key := range []string{TablePeople, TableHistory, "custom"} {
		if got := value.Tables[key].PageSize; got != 20 {
			t.Fatalf("%s page size = %d, want 20", key, got)
		}
	}
	if value.NavigationGroups == nil || value.WorkflowUses == nil {
		t.Fatal("normalized maps must be ready for mutation")
	}
}

func TestNormalizePageSizeAcceptsOnlySupportedChoices(t *testing.T) {
	for _, value := range []int{10, 20, 50, 100} {
		if got := NormalizePageSize(value); got != value {
			t.Fatalf("NormalizePageSize(%d)=%d", value, got)
		}
	}
	for _, value := range []int{-1, 0, 11, 1000} {
		if got := NormalizePageSize(value); got != 20 {
			t.Fatalf("NormalizePageSize(%d)=%d, want 20", value, got)
		}
	}
}
