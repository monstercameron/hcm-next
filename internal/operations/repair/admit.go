package repair

import (
	"context"
	"errors"
	"fmt"
)

// AdmitRepair re-runs the pure [Revalidate] check first and only asks a for a
// fence when current evidence still confirms the plan; a fence that does not
// bind the revalidated plan digest, or that lacks an identity, is refused. It
// is the in-process consumer of [Admitter], so the workflow REPAIR mode never
// admits a plan the evidence already rejected.
func AdmitRepair(ctx context.Context, a Admitter, req RevalidationRequest) (Result, error) {
	if a == nil {
		return Result{}, errors.New("repair: admitter is required")
	}
	pre, err := Revalidate(req)
	if err != nil {
		return Result{}, err
	}
	if pre.Status != StatusReady {
		return pre, nil
	}
	out, err := a.RevalidateAndFence(ctx, req)
	if err != nil {
		return Result{}, err
	}
	if out.Status == StatusReady && (out.Fence.FenceID == "" || out.Fence.PlanDigest != req.Plan.Digest) {
		return Result{}, fmt.Errorf("%w: admitter fence does not bind the revalidated plan", ErrInvalidEvidence)
	}
	return out, nil
}
