package approval

import (
	"context"
	"errors"
	"fmt"
)

// ErrElectionMismatch reports an [AtomicPlanConsumer] result that does not
// bind the plan the caller presented.
var ErrElectionMismatch = errors.New("approval: elected eligibility does not bind the presented plan")

// ElectExecutablePlan delegates the atomic consume-and-elect to consumer and
// refuses an eligibility that names a different plan digest than req.Plan, or
// that claims to be executable without consuming any decision, so a port
// implementation cannot elect a plan the caller did not present. It is the
// in-process consumer of [AtomicPlanConsumer]; request validation stays inside
// the atomic port because it must happen in the same transaction as the
// election.
func ElectExecutablePlan(ctx context.Context, consumer AtomicPlanConsumer, req ConsumeRequest) (ExecutionEligibility, error) {
	if consumer == nil {
		return ExecutionEligibility{}, errors.New("approval: atomic plan consumer is required")
	}
	out, err := consumer.ConsumeAndElect(ctx, req)
	if err != nil {
		return ExecutionEligibility{}, err
	}
	if out.PlanDigest != req.Plan.Digest {
		return ExecutionEligibility{}, fmt.Errorf("%w: elected %s, presented %s", ErrElectionMismatch, out.PlanDigest, req.Plan.Digest)
	}
	if out.Executable && len(out.ConsumedDecisionIDs) == 0 {
		return ExecutionEligibility{}, fmt.Errorf("%w: executable without any consumed decision", ErrElectionMismatch)
	}
	return out, nil
}
