package boundary

import (
	"errors"
	"strings"
	"testing"
)

func testPolicy() Policy {
	return Policy{AllowKeys: map[string]map[string]bool{"out": {"correlation_id": true}, "inbound": {"correlation_id": true, "request_id": true}}}
}

func TestTodoOBS011RejectsAuthorityAndMalformedPropagation(t *testing.T) {
	p := testPolicy()
	for _, tc := range []struct{ name, trace, baggage, signal string }{
		{"bad trace", "bad", "correlation_id=c", "invalid_trace_context"},
		{"authority baggage", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "tenant=evil", "invalid_baggage"},
		{"uppercase trace", "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01", "", "invalid_trace_context"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Inbound(tc.trace, tc.baggage)
			if !got.FreshTrace || got.SecuritySignal != tc.signal {
				t.Fatalf("decision=%+v", got)
			}
		})
	}
}

func TestTodoOBS011EgressIsDestinationBoundAndInputImmutable(t *testing.T) {
	p := testPolicy()
	in := []Baggage{{Key: "correlation_id", Value: "c"}, {Key: "request_id", Value: "r"}}
	if got := p.Egress("out", in); got != "correlation_id=c" {
		t.Fatalf("egress=%q", got)
	}
	if len(in) != 2 || in[0].Value != "c" {
		t.Fatal("filter mutated input")
	}
}

func TestTodoOBS011EgressRechecksAuthorityAndSensitiveKeys(t *testing.T) {
	p := Policy{AllowKeys: map[string]map[string]bool{"provider": {
		"correlation_id": true,
		"tenant_id":      true, // an over-broad configuration must fail closed
		"employee_email": true,
	}}}
	in := []Baggage{
		{Key: "correlation_id", Value: "c"},
		{Key: "tenant_id", Value: "tenant"},
		{Key: "employee_email", Value: "person@example.test"},
	}
	if got := p.Egress("provider", in); got != "correlation_id=c" {
		t.Fatalf("forbidden baggage crossed egress: %q", got)
	}
}

func TestTodoOBS011ConformanceTraceParentAndBounds(t *testing.T) {
	p := testPolicy()
	decision := p.Inbound("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "correlation_id=c,request_id=r")
	if decision.FreshTrace || !decision.Trace.Sampled || len(decision.Baggage) != 2 {
		t.Fatalf("decision=%+v", decision)
	}
	if _, err := ParseBaggage("correlation_id=" + string(make([]byte, MaxValueLen+1))); err == nil {
		t.Fatal("oversized baggage accepted")
	}
}

func TestBoundary_ParseTraceParentRejectsMalformedAndFormatsValid(t *testing.T) {
	valid := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	for _, header := range []string{
		"", strings.Repeat("a", MaxTraceParentLen+1), string([]byte{0xff}),
		"01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-4bf9-00f067aa0ba902b7-01", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-02",
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e473g-00f067aa0ba902b7-01",
	} {
		t.Run(header, func(t *testing.T) {
			if _, err := ParseTraceParent(header); !errors.Is(err, ErrRejected) {
				t.Fatalf("ParseTraceParent(%q) = %v, want ErrRejected", header, err)
			}
		})
	}
	trace, err := ParseTraceParent(valid)
	if err != nil || !trace.Sampled || trace.TraceID == "" || trace.SpanID == "" {
		t.Fatalf("valid trace = %+v, %v", trace, err)
	}
	notSampled, err := ParseTraceParent(strings.TrimSuffix(valid, "01") + "00")
	if err != nil || notSampled.Sampled {
		t.Fatalf("not-sampled trace = %+v, %v", notSampled, err)
	}
	formatted, err := FormatTraceParent(trace)
	if err != nil || formatted != valid {
		t.Fatalf("FormatTraceParent = %q, %v", formatted, err)
	}
	for _, bad := range []Trace{{TraceID: "", SpanID: trace.SpanID}, {TraceID: trace.TraceID, SpanID: "bad"}} {
		if _, err := FormatTraceParent(bad); !errors.Is(err, ErrRejected) {
			t.Fatalf("FormatTraceParent(%+v) = %v", bad, err)
		}
	}
}

func TestBoundary_ParseBaggageAndFilterFailClosed(t *testing.T) {
	for _, header := range []string{
		strings.Repeat("a", MaxBaggageLen+1), string([]byte{0xff}), "missing-equals",
		"=value", "key=" + strings.Repeat("x", MaxValueLen+1), strings.Repeat("k=v,", MaxEntries) + "k=v",
		"bad key=ok", "tenant_id=evil", "Principal=evil",
	} {
		t.Run(header, func(t *testing.T) {
			if _, err := ParseBaggage(header); !errors.Is(err, ErrRejected) {
				t.Fatalf("ParseBaggage(%q) = %v", header, err)
			}
		})
	}
	if got, err := ParseBaggage(" \t"); err != nil || got != nil {
		t.Fatalf("blank baggage = %#v, %v", got, err)
	}
	entries, err := ParseBaggage("correlation_id=c, request_id=r")
	if err != nil || len(entries) != 2 || entries[1].Key != "request_id" {
		t.Fatalf("valid baggage = %#v, %v", entries, err)
	}
	p := Policy{AllowKeys: map[string]map[string]bool{"out": {"correlation_id": true, "request_id": true, "nameplate": true}}}
	input := append([]Baggage(nil), entries...)
	input = append(input, Baggage{Key: "tenant_id", Value: "evil"}, Baggage{Key: "employee_name", Value: "evil"}, Baggage{Key: "x", Value: string([]byte{0xff})})
	if got := p.Filter("out", input); len(got) != 2 {
		t.Fatalf("filtered baggage = %#v", got)
	}
	if got := p.Filter("unknown", input); got == nil || len(got) != 0 {
		t.Fatalf("unknown destination filter = %#v, want non-nil empty", got)
	}
	if len(input) != 5 || input[0].Value != "c" {
		t.Fatal("Filter changed input")
	}
	tooMany := make([]Baggage, MaxEntries+2)
	for i := range tooMany {
		tooMany[i] = Baggage{Key: "correlation_id", Value: "x"}
	}
	if got := p.Filter("out", tooMany); len(got) != MaxEntries {
		t.Fatalf("Filter count = %d, want %d", len(got), MaxEntries)
	}
	if got := p.Egress("out", entries); got != "correlation_id=c,request_id=r" {
		t.Fatalf("Egress = %q", got)
	}
}

func TestBoundary_InboundSignalsEachInvalidComponent(t *testing.T) {
	p := testPolicy()
	for _, tc := range []struct{ name, trace, baggage, want string }{
		{"both", "bad", "bad", "invalid_trace_context_and_baggage"},
		{"trace", "bad", "correlation_id=c", "invalid_trace_context"},
		{"baggage", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "bad", "invalid_baggage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Inbound(tc.trace, tc.baggage)
			if !got.FreshTrace || got.SecuritySignal != tc.want || got.Trace.TraceID != "" || got.Baggage != nil {
				t.Fatalf("Inbound = %+v", got)
			}
		})
	}
	got := p.Inbound("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00", "correlation_id=c,request_id=r")
	if got.FreshTrace || got.SecuritySignal != "" || len(got.Baggage) != 2 {
		t.Fatalf("valid Inbound = %+v", got)
	}
}
