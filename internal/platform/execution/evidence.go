package execution

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// capabilityEvidenceAdapter adapts a [capability.EvidenceSink] — the same
// in-memory sink CAP-002's gateway already writes every invocation/refusal
// through (internal/intent/app.MemoryEvidenceSink in every P1A composition
// today) — into [execute.ExecutionEvidence] (OBS-024 GREEN: "through the
// existing capability evidence sink mechanism").
//
// [capability.InvocationEvidence] has no instance/node/ref/digest fields of
// its own; this adapter packs OBS-024's richer vocabulary into the fields
// that port does declare rather than widening a shared CAP-002 contract for
// one caller: Decision carries the OBS-024 EvidenceKind, SubjectRef carries
// "<instanceID>|<nodeID>", and ReasonCode carries "<refID>|<digest>". A
// reader that knows this convention decodes it back from the same
// app.EvidenceRecord [app.MemoryEvidenceSink.Records] already returns (see
// this package's own evidence_test.go for the reference decode). A later
// durable evidence store need not keep this packing — it is an adapter
// detail, not part of the [execute.ExecutionEvidence] contract itself.
type capabilityEvidenceAdapter struct {
	sink capability.EvidenceSink
	now  func() time.Time
}

var _ execute.ExecutionEvidence = capabilityEvidenceAdapter{}

// RecordExecutionEvidence implements execute.ExecutionEvidence.
func (a capabilityEvidenceAdapter) RecordExecutionEvidence(
	ctx context.Context, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time,
) (string, error) {
	if occurredAt.IsZero() {
		occurredAt = a.now()
	}
	return a.sink.RecordInvocation(ctx, capability.InvocationEvidence{
		CapabilityID:      "workflow.execution.evidence",
		CapabilityVersion: 1,
		SubjectRef:        instanceID + "|" + nodeID,
		Decision:          kind,
		ReasonCode:        refID + "|" + digest,
		OccurredAt:        occurredAt,
	})
}

// NewCapabilityEvidenceAdapter builds an [execute.ExecutionEvidence] over
// sink and clock, for a caller that composes its own [execute.Driver]
// outside [NewPromotionExecution] (a test wiring its own database/plan, a
// second workflow composition) but still wants OBS-024 evidence recorded
// through the same capability evidence sink mechanism this package's own
// composition uses. A nil clock defaults to time.Now in UTC.
func NewCapabilityEvidenceAdapter(sink capability.EvidenceSink, clock func() time.Time) execute.ExecutionEvidence {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return capabilityEvidenceAdapter{sink: sink, now: clock}
}
