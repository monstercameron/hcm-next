package inventory

import (
	"errors"
	"testing"
)

func TestTodo_PRIV_001(t *testing.T) {
	release, err := ValidateExecutable(validInventory())
	if err != nil {
		t.Fatalf("ValidateExecutable: %v", err)
	}
	if release.Digest == "" || len(release.Inventory.Activities) != 1 || len(release.Inventory.Flows) != 1 {
		t.Fatalf("incomplete executable inventory: %+v", release)
	}
	if len(release.Inventory.Activities[0].Obligations) != 2 {
		t.Fatal("approved activity must carry both jurisdiction-scoped notification obligations")
	}
}

func TestTodo_PRIV_001_Integration(t *testing.T) {
	release, err := ValidateExecutable(validInventory())
	if err != nil {
		t.Fatal(err)
	}
	if release.Inventory.Occurrences[0].Recipient != release.Inventory.Flows[0].Recipient || release.Inventory.Occurrences[0].Region == "" {
		t.Fatal("receipt did not retain recipient and region evidence")
	}
}

func TestTodo_PRIV_001_Security(t *testing.T) {
	i := validInventory()
	i.Flows[0].TransferRegions = []string{"APAC"}
	if _, err := ValidateExecutable(i); !errors.Is(err, ErrInvalid) {
		t.Fatalf("out-of-scope region = %v, want ErrInvalid", err)
	}
}

func TestTodo_PRIV_001_Mutation(t *testing.T) {
	base, err := ValidateExecutable(validInventory())
	if err != nil {
		t.Fatal(err)
	}
	mutated := validInventory()
	mutated.Activities[0].Obligations[1].DeadlineHours++
	changed, err := ValidateExecutable(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if base.Digest == changed.Digest {
		t.Fatal("obligation mutation did not change release digest")
	}
}
