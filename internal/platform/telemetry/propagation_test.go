package telemetry_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
)

func TestTraceContextBoundaryRejectsAuthorityInjectionAndFiltersBaggage(t *testing.T) {
	valid := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tc, ok := telemetry.ParseTraceParent(valid)
	if !ok || !tc.Valid || tc.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("valid trace rejected %+v %v", tc, ok)
	}
	if _, ok := telemetry.ParseTraceParent("00-00000000000000000000000000000000-00f067aa0ba902b7-01"); ok {
		t.Fatal("zero trace id accepted")
	}
	if _, ok := telemetry.ParseTraceParent("00-zzzz-00f067aa0ba902b7-01"); ok {
		t.Fatal("malformed trace accepted")
	}
	oversized := "00-" + string(make([]byte, 600)) + "-00f067aa0ba902b7-01"
	if _, ok := telemetry.ParseTraceParent(oversized); ok {
		t.Fatal("oversized trace accepted")
	}
	res := telemetry.ExtractPropagation("bad-header", "correlation_id=abc,tenant=evil")
	if !res.FreshTrace || res.SecuritySignal == "" {
		t.Fatalf("invalid context must start fresh with signal %+v", res)
	}
	bag, ok := telemetry.ParseBaggage("tenant=evil,correlation_id=abc")
	if ok {
		t.Fatalf("baggage with forbidden key must be rejected got %+v", bag)
	}
	okBag, _ := telemetry.ParseBaggage("correlation_id=abc,request_id=req-1")
	if len(okBag) != 2 {
		t.Fatalf("allowed baggage filtered %+v", okBag)
	}
	egress := telemetry.FilterBaggageForEgress([]telemetry.BaggageEntry{{Key: "correlation_id", Value: "abc"}, {Key: "tenant", Value: "evil"}})
	if len(egress) != 1 || egress[0].Key != "correlation_id" {
		t.Fatalf("egress not filtered %+v", egress)
	}
	for _, tc := range []telemetry.TraceContext{{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", Sampled: false}, {TraceID: "4bf92f3577b34da6a3ce929d0e0e4737", SpanID: "00f067aa0ba902b8", Sampled: true}} {
		h := telemetry.InjectTraceParent(tc)
		parsed, ok := telemetry.ParseTraceParent(h)
		if !ok || parsed.Sampled != tc.Sampled {
			t.Fatalf("round trip failed %v", tc)
		}
	}
	if telemetry.InjectBaggage([]telemetry.BaggageEntry{{Key: "tenant", Value: "evil"}, {Key: "correlation_id", Value: "abc"}}) != "correlation_id=abc" {
		t.Fatal("inject must filter")
	}
}

func TestTodo_OBS_011_Property(t *testing.T) {
	for i := 0; i < 50; i++ {
		tc := telemetry.FreshTraceContext()
		if !tc.Valid || len(tc.TraceID) != 32 || len(tc.SpanID) != 16 {
			t.Fatalf("fresh trace invalid %+v", tc)
		}
	}
}

func TestTodo_OBS_011_Golden(t *testing.T) {
	res := telemetry.ExtractPropagation("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "correlation_id=corr-1,request_id=req-1")
	got, _ := json.MarshalIndent(res, "", "  ")
	got = append(got, '\n')
	want, err := os.ReadFile(filepath.Join(repoRootTel(t), "internal", "platform", "telemetry", "testdata", "propagation.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		h := sha256.Sum256(got)
		t.Fatalf("golden mismatch %x got %s", h, got)
	}
}

func TestTodo_OBS_011_Security(t *testing.T) {
	if _, ok := telemetry.ParseBaggage("actor=bob"); ok {
		t.Fatal("actor baggage must be rejected")
	}
	if _, ok := telemetry.ParseBaggage("purpose=payroll"); ok {
		t.Fatal("purpose baggage must be rejected")
	}
	if _, ok := telemetry.ParseBaggage("correlation_id=" + string(make([]byte, 500))); ok {
		t.Fatal("oversized value accepted")
	}
}

func TestTodo_OBS_011_Conformance(t *testing.T) {
	tc := telemetry.TraceContext{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", Sampled: true, Valid: true}
	h := telemetry.InjectTraceParent(tc)
	if h != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("inject mismatch %q", h)
	}
}

func repoRootTel(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

func FuzzTodo_OBS_011(f *testing.F) {
	f.Add("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "correlation_id=abc")
	f.Add("bad", "tenant=evil")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, trace, baggage string) {
		tc, _ := telemetry.ParseTraceParent(trace)
		_ = tc
		b, _ := telemetry.ParseBaggage(baggage)
		_ = telemetry.FilterBaggageForEgress(b)
		res := telemetry.ExtractPropagation(trace, baggage)
		_ = res
		if len(baggage) > 10000 && len(b) > 16 {
			t.Fatal("oversized baggage not bounded")
		}
	})
}

func TestTodo_OBS_011_Mutation(t *testing.T) {
	if _, ok := telemetry.ParseTraceParent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"); !ok {
		t.Fatal("unsampled valid trace must parse")
	}
	res := telemetry.ExtractPropagation("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00", "correlation_id=x")
	if res.Trace.Sampled {
		t.Fatal("unsampled flag leaked")
	}
}
