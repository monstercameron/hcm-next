package endpoint

import (
	"context"
	"fmt"
	"sort"
)

// SideEffects is the durable-effect cardinality vector compared by parity
// tests. Status-code equality alone is intentionally insufficient.
type SideEffects struct {
	Intents int
	Events  int
	Outbox  int
	Work    int
	Effects int
}

// Outcome is the canonical semantic result of one transport execution.
type Outcome struct {
	RequestDigest  string
	ResultDigest   string
	ErrorDigest    string
	EvidenceDigest string
	Status         string
	SideEffects    SideEffects
}

// Vector supplies the direct owner, gRPC and HTTP executions for one request.
// The functions are intentionally injected so callers can use real in-memory
// gRPC/grpcbridge servers while this package remains transport-library free.
type Vector struct {
	Name   string
	Direct func(context.Context) Outcome
	GRPC   func(context.Context) Outcome
	HTTP   func(context.Context) Outcome
}

// Mismatch identifies one semantic divergence, including durable side effects.
type Mismatch struct {
	Transport string
	Field     string
	Want      string
	Got       string
}

// Report is the result of comparing all three paths.
type Report struct {
	Vector     string
	Outcomes   map[string]Outcome
	Mismatches []Mismatch
}

// Compare runs one versioned vector and compares every canonical field.
func Compare(ctx context.Context, v Vector) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	out := map[string]Outcome{
		"direct": invoke(v.Direct, ctx),
		"grpc":   invoke(v.GRPC, ctx),
		"http":   invoke(v.HTTP, ctx),
	}
	r := Report{Vector: v.Name, Outcomes: out}
	base := out["direct"]
	for _, transport := range []string{"grpc", "http"} {
		candidate := out[transport]
		compareOutcome(&r.Mismatches, transport, "request_digest", base.RequestDigest, candidate.RequestDigest)
		compareOutcome(&r.Mismatches, transport, "result_digest", base.ResultDigest, candidate.ResultDigest)
		compareOutcome(&r.Mismatches, transport, "error_digest", base.ErrorDigest, candidate.ErrorDigest)
		compareOutcome(&r.Mismatches, transport, "evidence_digest", base.EvidenceDigest, candidate.EvidenceDigest)
		compareOutcome(&r.Mismatches, transport, "status", base.Status, candidate.Status)
		if base.SideEffects != candidate.SideEffects {
			compareOutcome(&r.Mismatches, transport, "side_effects", fmt.Sprintf("%+v", base.SideEffects), fmt.Sprintf("%+v", candidate.SideEffects))
		}
	}
	return r
}

func invoke(fn func(context.Context) Outcome, ctx context.Context) Outcome {
	if fn == nil {
		return Outcome{ErrorDigest: "transport_not_configured"}
	}
	return fn(ctx)
}

func compareOutcome(mismatches *[]Mismatch, transport, field, want, got string) {
	if want != got {
		*mismatches = append(*mismatches, Mismatch{Transport: transport, Field: field, Want: want, Got: got})
	}
}

// Assert returns a deterministic error suitable for a test or release gate.
func (r Report) Assert() error {
	if len(r.Mismatches) == 0 {
		return nil
	}
	sort.Slice(r.Mismatches, func(i, j int) bool {
		if r.Mismatches[i].Transport != r.Mismatches[j].Transport {
			return r.Mismatches[i].Transport < r.Mismatches[j].Transport
		}
		return r.Mismatches[i].Field < r.Mismatches[j].Field
	})
	return fmt.Errorf("endpoint %s semantic divergence: %s %s want %q got %q", r.Vector, r.Mismatches[0].Transport, r.Mismatches[0].Field, r.Mismatches[0].Want, r.Mismatches[0].Got)
}
