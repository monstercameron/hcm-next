package boundary

import "testing"

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
