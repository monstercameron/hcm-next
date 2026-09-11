package lease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// The adapter is two mappings and a delegation, and all three are worth
// pinning: a field dropped in the mapping would silently widen what a fence
// accepts, and a refusal that lost its classification on the way out would
// leave a caller unable to tell a stale fence from a lost lease.

func TestRuntimeFence_MapsEveryFieldAndStampsTheCallersInstant(t *testing.T) {
	fence := Fence{
		TenantID: testTenant,
		Resource: Resource{Kind: ResourceNodeExecution, ID: "node:alpha#1"},
		LeaseID:  testLeaseID,
		Holder:   testHolder,
		Token:    7,
	}
	at := testNow.Add(90 * time.Second)
	got := fence.RuntimeFence(at)

	want := runtime.Fence{
		ResourceKind: ResourceNodeExecution,
		ResourceID:   "node:alpha#1",
		LeaseID:      testLeaseID,
		HolderID:     testHolder.HolderID(),
		Token:        7,
		At:           at,
	}
	if got != want {
		t.Fatalf("RuntimeFence() = %+v, want %+v", got, want)
	}
	// The tenant is deliberately not part of the runtime fence:
	// runtime.AdvanceFenced passes the tenant separately, from the
	// advancement request, so the fence cannot disagree with it.
	if fence.TenantID == uuid.Nil {
		t.Fatal("the fixture fence carries no tenant, so this case proves nothing")
	}
}

func TestFenced_RefusesAHolderIdThatIsNotAWorkloadIdentity(t *testing.T) {
	ex := refusingExecutor{t: t}
	adapter := Fenced{}

	for name, holderID := range map[string]string{
		"a bare hostname":       "worker-07",
		"an empty holder":       "",
		"an unqualified string": "runtime#replica:1",
	} {
		err := adapter.VerifyFence(context.Background(), ex, testTenant, runtime.Fence{
			ResourceKind: ResourceWorkflowInstance, ResourceID: "instance:1",
			LeaseID: testLeaseID, HolderID: holderID, Token: 1, At: testNow,
		})
		if err == nil {
			t.Fatalf("%s was accepted as a fence holder", name)
		}
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

// The adapter satisfies the port structurally, which is what lets
// internal/workflow/runtime state what it needs without depending on this
// package. A compile-time assertion is the honest test for that.
func TestFenced_SatisfiesTheRuntimePort(t *testing.T) {
	var _ runtime.FenceVerifier = Fenced{Manager: Manager{}}
}
