package runtime

import (
	"strings"
	"testing"
	"time"
)

func TestTodo_OBS_013_RuntimeContinuationCausalMetadataIsOptionalAndFanoutSafe(t *testing.T) {
	source := &CausalMetadata{
		CorrelationID: "corr", CausationID: "cause", LogicalOperationID: "logical", AttemptID: "attempt",
		TraceLink: &TraceLinkMetadata{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", TraceFlags: 1, TraceState: "vendor=value", ExpiresAt: time.Unix(100, 0)},
	}
	left, right := normalizeCausal(source), normalizeCausal(source)
	left.AttemptID = "fanout-left"
	left.TraceLink.TraceState = "left=value"
	if right.AttemptID != "attempt" || right.TraceLink.TraceState != "vendor=value" || source.AttemptID != "attempt" || source.TraceLink.TraceState != "vendor=value" {
		t.Fatal("fan-out continuations aliased mutable causal metadata")
	}
	for _, invalid := range []*CausalMetadata{
		{CorrelationID: "corr", CausationID: "", LogicalOperationID: "logical", AttemptID: "attempt"},
		{CorrelationID: strings.Repeat("x", 129), CausationID: "cause", LogicalOperationID: "logical", AttemptID: "attempt"},
	} {
		if got := normalizeCausal(invalid); got != nil {
			t.Fatalf("partial/oversized optional causal metadata survived: %#v", got)
		}
	}
	badTrace := *source
	badTrace.TraceLink = &TraceLinkMetadata{TraceID: strings.Repeat("0", 32), SpanID: "00f067aa0ba902b7"}
	if got := normalizeCausal(&badTrace); got == nil || got.TraceLink != nil {
		t.Fatalf("invalid diagnostic trace link changed business causal metadata: %#v", got)
	}
}
