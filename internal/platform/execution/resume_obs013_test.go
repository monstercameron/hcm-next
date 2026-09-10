package execution

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var resumeTestAt = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

const (
	resumeTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	resumeSpanID  = "00f067aa0ba902b7"
)

func resumeCausal(traceID, spanID string, expires time.Time) *runtime.CausalMetadata {
	return &runtime.CausalMetadata{
		CorrelationID: "corr-r", CausationID: "cause-r",
		LogicalOperationID: "logical-r", AttemptID: "parked-attempt",
		TraceLink: &runtime.TraceLinkMetadata{
			TraceID: traceID, SpanID: spanID, TraceFlags: 1,
			ExpiresAt: expires,
		},
	}
}

func resumeRequest(causal *runtime.CausalMetadata) execute.ResumeSpanRequest {
	return execute.ResumeSpanRequest{
		InstanceID: "11111111-1111-4111-8111-111111111111",
		NodeID:     "wait_for_effective_date",
		Attempt:    2,
		TimerID:    "33333333-3333-4333-8333-333333333333",
		Causal:     causal,
		At:         resumeTestAt,
	}
}

func flushSpans(t *testing.T, clock func() time.Time, run func(*OTelInstrumentation)) []spanSnapshot {
	t.Helper()
	inst, provider, recorder, logs := newTestInstrumentation(t, clock)
	run(inst)
	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	_ = logs
	out := make([]spanSnapshot, 0)
	for _, s := range recorder.Spans() {
		attrs := map[string]string{}
		for _, kv := range s.Attributes() {
			attrs[string(kv.Key)] = kv.Value.AsString()
		}
		links := make([]string, 0)
		for _, l := range s.Links() {
			links = append(links, l.SpanContext.TraceID().String()+"/"+l.SpanContext.SpanID().String())
		}
		sort.Strings(links)
		out = append(out, spanSnapshot{name: s.Name(), attrs: attrs, links: links})
	}
	return out
}

type spanSnapshot struct {
	name  string
	attrs map[string]string
	links []string
}

func attrKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestResumeSpanLinksToStoredParkedTrace proves the OBS-013 production
// path: a valid, unexpired stored trace link produces a
// hcmnext.timer.resume span carrying exactly one link to the parked trace,
// with the contract attribute set and a fresh attempt identity reusing the
// stored logical operation.
func TestResumeSpanLinksToStoredParkedTrace(t *testing.T) {
	clock := func() time.Time { return resumeTestAt }
	spans := flushSpans(t, clock, func(inst *OTelInstrumentation) {
		_, span := inst.StartResumeSpan(context.Background(),
			resumeRequest(resumeCausal(resumeTraceID, resumeSpanID, resumeTestAt.Add(time.Hour))))
		span.End(execute.OutcomeSuccess, nil)
	})
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want exactly the resume span", len(spans))
	}
	got := spans[0]
	if got.name != "hcmnext.timer.resume" {
		t.Fatalf("span name = %q, want the OBS-012 contract name hcmnext.timer.resume", got.name)
	}
	if len(got.links) != 1 || got.links[0] != resumeTraceID+"/"+resumeSpanID {
		t.Fatalf("span links = %v, want exactly the parked trace", got.links)
	}
	// The contract attribute set must be present; outcome is added by the
	// shared span End machinery on every span and is orthogonal to it.
	for _, want := range []string{"attempt_id", "correlation_id", "logical_operation_id", "timer_id"} {
		if _, ok := got.attrs[want]; !ok {
			t.Fatalf("span attrs = %v, missing contract attribute %q", attrKeys(got.attrs), want)
		}
	}
	if got.attrs["logical_operation_id"] != "logical-r" || got.attrs["correlation_id"] != "corr-r" {
		t.Fatalf("stored identities not preserved: %v", got.attrs)
	}
	if got.attrs["attempt_id"] == "parked-attempt" {
		t.Fatal("resume span reused the parked attempt identity instead of minting a fresh one")
	}
	if got.attrs["timer_id"] != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("timer_id = %q", got.attrs["timer_id"])
	}
}

// TestResumeSpanWithoutUsableLinkAdvancesUnlinked is the OBS-013
// fault/recovery matrix: expired, tampered, missing-link and missing-causal
// resumes all open the same span with no links, so business behavior is
// identical with or without usable link metadata.
func TestResumeSpanWithoutUsableLinkAdvancesUnlinked(t *testing.T) {
	cases := map[string]*runtime.CausalMetadata{
		"expired link":  resumeCausal(resumeTraceID, resumeSpanID, resumeTestAt.Add(-time.Hour)),
		"tampered link": resumeCausal("zzf92f3577b34da6a3ce929d0e0e47zz", resumeSpanID, resumeTestAt.Add(time.Hour)),
		"missing link": {
			CorrelationID: "corr-r", CausationID: "cause-r",
			LogicalOperationID: "logical-r", AttemptID: "parked-attempt",
		},
		"missing causal": nil,
	}
	for name, causal := range cases {
		t.Run(name, func(t *testing.T) {
			clock := func() time.Time { return resumeTestAt }
			spans := flushSpans(t, clock, func(inst *OTelInstrumentation) {
				_, span := inst.StartResumeSpan(context.Background(), resumeRequest(causal))
				span.End(execute.OutcomeSuccess, nil)
			})
			if len(spans) != 1 {
				t.Fatalf("spans = %d, want exactly the resume span", len(spans))
			}
			if spans[0].name != "hcmnext.timer.resume" {
				t.Fatalf("span name = %q, want hcmnext.timer.resume even without a usable link", spans[0].name)
			}
			if len(spans[0].links) != 0 {
				t.Fatalf("span links = %v, want none without a usable stored link", spans[0].links)
			}
		})
	}
}

// TestResumeSpanEmitsNoLogLine proves the OBS-023 one-line rule survives
// OBS-013: the resume span records no structured log of its own; the
// nested advancement span still owns the per-advancement line.
func TestResumeSpanEmitsNoLogLine(t *testing.T) {
	clock := func() time.Time { return resumeTestAt }
	inst, provider, _, logs := newTestInstrumentation(t, clock)
	_, span := inst.StartResumeSpan(context.Background(),
		resumeRequest(resumeCausal(resumeTraceID, resumeSpanID, resumeTestAt.Add(time.Hour))))
	span.End(execute.OutcomeSuccess, nil)
	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	if lines := logs.Lines(); len(lines) != 0 {
		t.Fatalf("resume span emitted %d log lines, want none", len(lines))
	}
}
