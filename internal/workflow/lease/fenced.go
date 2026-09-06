package lease

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// Fenced adapts a [Manager] to internal/workflow/runtime's FenceVerifier
// port, so runtime.AdvanceFenced can refuse a stale or foreign fence without
// runtime depending on this package.
//
// The adapter lives here rather than in runtime because the port is
// consumer-owned: runtime states what it needs, and the package that knows
// how a fence is compared supplies it. A production composition root wires
// this value into execute.Options; nothing about it is test-only.
type Fenced struct{ Manager Manager }

// VerifyFence checks the presented fence against the durable lease and
// returns this package's typed refusal unchanged, so a caller reading the
// wrapped error still gets LEASE_LOST, FENCE_STALE or FENCE_FOREIGN from
// [CodeOf] and still classifies it with [ErrLeaseLost] or [ErrFenceStale].
func (f Fenced) VerifyFence(ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, fence runtime.Fence) error {
	holder, err := ParseHolder(fence.HolderID)
	if err != nil {
		return err
	}
	_, err = f.Manager.Verify(ctx, ex, Fence{
		TenantID: tenantID,
		Resource: Resource{Kind: fence.ResourceKind, ID: fence.ResourceID},
		LeaseID:  fence.LeaseID,
		Holder:   holder,
		Token:    fence.Token,
	}, fence.At)
	return err
}

// RuntimeFence renders a [Fence] as the value runtime.AdvanceFenced takes,
// stamped with the caller's own instant. It is the one place the two
// representations are mapped, so the field-by-field correspondence is stated
// once rather than at every call site.
func (f Fence) RuntimeFence(at time.Time) runtime.Fence {
	return runtime.Fence{
		ResourceKind: f.Resource.Kind,
		ResourceID:   f.Resource.ID,
		LeaseID:      f.LeaseID,
		HolderID:     f.Holder.HolderID(),
		Token:        f.Token,
		At:           at,
	}
}

var _ runtime.FenceVerifier = Fenced{}
