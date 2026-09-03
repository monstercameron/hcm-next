package continuity

import (
	"errors"
	"testing"
	"time"
)

func fixture(t *testing.T, route Route) (FlowSpec, Request) {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	f := FlowSpec{ID: "promotion", Version: "v3", Owner: "experience", Stages: []Stage{{ID: "discover", Required: true}, {ID: "details", Required: true}, {ID: "review", Required: true}, {ID: "submit", Required: true}}}
	r := Request{FlowID: f.ID, FlowVersion: f.Version, Subject: "worker:1", Actor: "worker:1", Meaning: "promotion.request.v1", Intent: map[string]string{"effective_date": "2026-10-01", "proposed_amount": "100.00", "reason": "scope"}, Deadline: now.Add(time.Hour), PrivacyClass: "workforce-restricted", Locale: "en-US", TemplateVersion: "promotion.v3", Route: route, At: now}
	// Manual continuity uses its governed receipt as the bounded evidence
	// artifact; browser-only focus/live-region evidence is not available when
	// an inaccessible upload is handed to an operator.
	if route != RouteManual {
		r.WCAGEvidence = "wcag-2.2-aa"
		r.FocusEvidence = "focus-order"
		r.AnnouncementEvidence = "live-region"
	}
	if route == RouteAssisted {
		r.AssistedBy = "support:7"
	}
	if route == RouteManual {
		_, d, _ := NormalizeIntent(r.Intent)
		r.ManualReceipt = d
	}
	return f, r
}

func TestCompleteUserFlowPreservesSemanticsDeadlinesAndEvidenceAcrossAccessibleRoutes(t *testing.T) {
	flow, base := fixture(t, RouteBrowser)
	want, err := Execute(flow, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []Route{RouteKeyboard, RouteScreenReader, RouteZoom, RouteRTL, RouteAssisted, RouteManual} {
		_, r := fixture(t, route)
		got, e := Execute(flow, r)
		if e != nil {
			t.Fatalf("%s: %v", route, e)
		}
		if !Equivalent(want, got) {
			t.Fatalf("%s changed semantic outcome", route)
		}
		if got.IntentDigest != want.IntentDigest {
			t.Fatalf("%s changed digest", route)
		}
	}
}

func TestTodo_UXFLOW_008_Browser(t *testing.T) {
	f, r := fixture(t, RouteScreenReader)
	if _, e := Execute(f, r); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_UXFLOW_008_Conformance(t *testing.T) {
	f, r := fixture(t, RouteBrowser)
	o, e := Execute(f, r)
	if e != nil || len(o.Evidence.Stages) != 4 {
		t.Fatalf("conformance: %+v %v", o, e)
	}
}
func TestTodo_UXFLOW_008_Golden(t *testing.T) {
	f, r := fixture(t, RouteBrowser)
	o, e := Execute(f, r)
	if e != nil {
		t.Fatal(e)
	}
	if o.Result != "COMPLETE" || o.IntentDigest == "" {
		t.Fatalf("golden: %+v", o)
	}
}
func TestTodo_UXFLOW_008_Integration(t *testing.T) {
	f, r := fixture(t, RouteManual)
	if _, e := Execute(f, r); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_UXFLOW_008_Property(t *testing.T) {
	f, r := fixture(t, RouteBrowser)
	a, e := Execute(f, r)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 10; i++ {
		b, e := Execute(f, r)
		if e != nil || !Equivalent(a, b) {
			t.Fatal("execution is not deterministic")
		}
	}
}
func TestTodo_UXFLOW_008_Security(t *testing.T) {
	f, r := fixture(t, RouteAssisted)
	r.AssistedBy = ""
	if _, e := Execute(f, r); !errors.Is(e, ErrAttribution) {
		t.Fatalf("missing attribution: %v", e)
	}
}
func TestTodo_UXFLOW_008_Fault(t *testing.T) {
	f, r := fixture(t, RouteManual)
	r.ManualReceipt = "tampered"
	if _, e := Execute(f, r); !errors.Is(e, ErrSemanticDrift) {
		t.Fatalf("tampered receipt: %v", e)
	}
}
func TestTodo_UXFLOW_008_Mutation(t *testing.T) {
	f, r := fixture(t, RouteRTL)
	r.At = r.Deadline.Add(time.Nanosecond)
	if _, e := Execute(f, r); !errors.Is(e, ErrDeadline) {
		t.Fatalf("expired deadline: %v", e)
	}
}
