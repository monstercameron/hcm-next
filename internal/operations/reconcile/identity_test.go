package reconcile

import (
	"testing"

	"github.com/google/uuid"
)

func TestJobID_DeterministicAcrossCalls(t *testing.T) {
	tenant := uuid.New()
	a := JobID(tenant, "effect-1", "policy-1")
	b := JobID(tenant, "effect-1", "policy-1")
	if a != b {
		t.Fatalf("JobID is not deterministic: %s != %s", a, b)
	}
}

func TestJobID_DistinctForEveryComponent(t *testing.T) {
	tenant1, tenant2 := uuid.New(), uuid.New()
	base := JobID(tenant1, "effect-1", "policy-1")
	byTenant := JobID(tenant2, "effect-1", "policy-1")
	byEffect := JobID(tenant1, "effect-2", "policy-1")
	byPolicy := JobID(tenant1, "effect-1", "policy-2")

	ids := []uuid.UUID{base, byTenant, byEffect, byPolicy}
	for i := range ids {
		for j := i + 1; j < len(ids); j++ {
			if ids[i] == ids[j] {
				t.Fatalf("two different (tenant, effect, policy) triples produced the same job id: index %d and %d", i, j)
			}
		}
	}
}

// TestJobID_NoDelimiterConfusion proves the separator byte prevents
// "effect1"+"policy2" from colliding with "effect12"+"policy" or similar
// concatenation ambiguity -- the same class of bug PageIdentity.ObservationID
// guards against in internal/connectivity/observe.
func TestJobID_NoDelimiterConfusion(t *testing.T) {
	tenant := uuid.New()
	a := JobID(tenant, "eff", "ectpolicy")
	b := JobID(tenant, "effect", "policy")
	if a == b {
		t.Fatal("concatenation without a delimiter would collide these two distinct triples")
	}
}
