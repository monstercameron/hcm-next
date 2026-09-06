package inventory

import (
	"strings"
	"testing"
)

func TestExecutableReleaseIsVersionedAndOrderIndependent(t *testing.T) {
	a := validInventory()
	b := validInventory()
	b.Activities[0].DataCategories = []string{"salary"}
	b.Flows[0].Operations = []string{"transmit"}
	releaseA, err := ValidateExecutable(a)
	if err != nil {
		t.Fatal(err)
	}
	releaseB, err := ValidateExecutable(b)
	if err != nil {
		t.Fatal(err)
	}
	if releaseA.Digest != releaseB.Digest || !strings.Contains(releaseA.Explain(), releaseA.Digest) {
		t.Fatalf("release is not canonical: %+v %+v", releaseA, releaseB)
	}
}

func TestExecutableReleaseRejectsApprovedActivityWithoutFlow(t *testing.T) {
	i := validInventory()
	i.Flows = nil
	if _, err := ValidateExecutable(i); err == nil {
		t.Fatal("expected an approved inventory without a flow to be rejected")
	}
}
