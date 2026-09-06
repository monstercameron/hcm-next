package lease

import (
	"errors"
	"testing"
)

// WF-RUN-002's REFACTOR clause: "lease owner is workload identity, not
// process hostname alone". These tests are what makes that a refusal rather
// than a comment.

func TestIdentity_BareHostnameIsRefused(t *testing.T) {
	res := Resource{Kind: ResourceWorkflowInstance, ID: "instance:1"}
	for name, id := range map[string]Identity{
		"a bare hostname":            {WorkloadRef: "worker-07", InstanceRef: "replica:1"},
		"an unqualified workload":    {WorkloadRef: "hcmnext-runtime", InstanceRef: "replica:1"},
		"a scheme with no name":      {WorkloadRef: "workload:", InstanceRef: "replica:1"},
		"no workload at all":         {WorkloadRef: "", InstanceRef: "replica:1"},
		"no replica":                 {WorkloadRef: "workload:hcmnext-runtime", InstanceRef: ""},
		"a padded workload":          {WorkloadRef: " workload:hcmnext-runtime", InstanceRef: "replica:1"},
		"a separator in the halves":  {WorkloadRef: "workload:a#b", InstanceRef: "replica:1"},
		"a separator in the replica": {WorkloadRef: "workload:a", InstanceRef: "replica#1"},
	} {
		if err := id.validate(res); err == nil {
			t.Fatalf("%s was accepted as a lease holder", name)
		} else if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestIdentity_WorkloadAndReplicaRoundTrip(t *testing.T) {
	id := Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:cell-local-1"}
	if err := id.validate(Resource{Kind: ResourceQueue, ID: "q"}); err != nil {
		t.Fatalf("a scheme-qualified workload with a replica was refused: %v", err)
	}
	stored := id.HolderID()
	if stored != "workload:hcmnext-workflow-runtime#replica:cell-local-1" {
		t.Fatalf("HolderID = %q", stored)
	}
	back, err := ParseHolder(stored)
	if err != nil {
		t.Fatalf("ParseHolder(%q): %v", stored, err)
	}
	if back != id {
		t.Fatalf("ParseHolder round trip = %+v, want %+v", back, id)
	}
}

func TestIdentity_ParseHolderRefusesAStringThatIsNotOne(t *testing.T) {
	for _, bad := range []string{"", "worker-07", "workload:only"} {
		if _, err := ParseHolder(bad); err == nil {
			t.Fatalf("ParseHolder(%q) was accepted", bad)
		}
	}
}

func TestResource_OnlyDeclaredKindsAreLeasable(t *testing.T) {
	for _, kind := range []string{ResourceWorkflowInstance, ResourceNodeExecution, ResourceWorkItem, ResourceQueue} {
		if err := (Resource{Kind: kind, ID: "x"}).validate(); err != nil {
			t.Fatalf("declared kind %q was refused: %v", kind, err)
		}
	}
	for name, res := range map[string]Resource{
		"an undeclared kind": {Kind: "TENANT", ID: "x"},
		"an empty kind":      {Kind: "", ID: "x"},
		"an empty id":        {Kind: ResourceQueue, ID: ""},
		"a padded id":        {Kind: ResourceQueue, ID: " q "},
	} {
		if err := res.validate(); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestResource_StringNamesKindAndID(t *testing.T) {
	got := Resource{Kind: ResourceWorkItem, ID: "item:9"}.String()
	if got != "WORK_ITEM item:9" {
		t.Fatalf("Resource.String = %q", got)
	}
}
