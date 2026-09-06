package reconcile

import "github.com/google/uuid"

// jobNamespace seeds the deterministic job identity, following the same
// UUIDv5-derivation convention internal/workflow/timer uses for TimerID: a
// fixed namespace so that the same (tenant, effect, policy) always yields the
// same job id, on every machine and every rerun.
var jobNamespace = uuid.MustParse("9d3b6e2a-1c4f-4d7a-8b2e-5f0c1a9e3b7d")

// JobID returns the derived identity of the reconciliation job for one
// effect under one comparison policy. Two callers that computed it from the
// same triple address the same row, which is what makes triggering the same
// effect and policy twice idempotent rather than merely detectable.
func JobID(tenantID uuid.UUID, effectRef, policyRef string) uuid.UUID {
	return uuid.NewSHA1(jobNamespace, []byte(tenantID.String()+"\x1f"+effectRef+"\x1f"+policyRef))
}
