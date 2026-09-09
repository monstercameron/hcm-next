package readiness_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/readiness"
)

func resolveForEvaluation(t *testing.T, r readiness.ReadinessRequirement, d readiness.EvidenceDescriptor) readiness.Resolution {
	t.Helper()
	got, err := readiness.Resolve(context.Background(), &fakeReader{descriptors: []readiness.EvidenceDescriptor{d}}, request(t, r))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTodo_READINESS_003(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	cases := []struct {
		name   string
		state  readiness.EvidenceState
		access readiness.EvidenceAccess
		want   readiness.ReadinessStatus
	}{
		{"ready", readiness.EvidenceSatisfied, readiness.EvidenceAuthorized, readiness.StatusReady},
		{"conditional", readiness.EvidenceConditional, readiness.EvidenceAuthorized, readiness.StatusConditional},
		{"unknown", readiness.EvidenceSatisfied, readiness.EvidenceDenied, readiness.StatusUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := descriptor(t, tc.state)
			e.Access = tc.access
			got, err := readiness.Evaluate(r, resolveForEvaluation(t, r, e))
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want || got.Digest == "" || got.ExplanationDigest == "" {
				t.Fatalf("evaluation = %+v, want %s", got, tc.want)
			}
			if strings.Contains(got.RequirementID, e.EvidenceRef) {
				t.Fatal("evaluation mixed evidence reference into identity")
			}
			if tc.want == readiness.StatusUnknown && got.Status == readiness.StatusReady {
				t.Fatal("unknown promoted to ready")
			}
		})
	}
	unsatisfied := descriptor(t, readiness.EvidenceSatisfied)
	unsatisfied.ObservedAt = instant(t, 1)
	unsatisfied.FreshUntil = instant(t, 1)
	got, err := readiness.Evaluate(r, resolveForEvaluation(t, r, unsatisfied))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != readiness.StatusNotReady || len(got.Blockers) == 0 {
		t.Fatalf("stale evaluation = %+v, want NOT_READY blocker", got)
	}
}

func TestTodo_READINESS_003_Golden(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	e := descriptor(t, readiness.EvidenceConditional)
	first, err := readiness.Evaluate(r, resolveForEvaluation(t, r, e))
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:3010211b3accdd8a9520392afde1957ddc28fe55a3d68ec6b8457bd45438e065"
	const wantExplanationDigest = "sha256:965930b30a25c3b967a7b732ba568ac857a5fdf629d8294bcd285b3bef154809"
	if first.Digest != wantDigest || first.ExplanationDigest != wantExplanationDigest {
		t.Fatalf("golden digests = %q, %q", first.Digest, first.ExplanationDigest)
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_READINESS_003_Property(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	cases := []struct {
		state  readiness.EvidenceState
		access readiness.EvidenceAccess
		stale  bool
		want   readiness.ReadinessStatus
	}{
		{readiness.EvidenceSatisfied, readiness.EvidenceAuthorized, false, readiness.StatusReady},
		{readiness.EvidenceConditional, readiness.EvidenceAuthorized, false, readiness.StatusConditional},
		{readiness.EvidenceSatisfied, readiness.EvidenceAuthorized, true, readiness.StatusNotReady},
		{readiness.EvidenceSatisfied, readiness.EvidenceDenied, false, readiness.StatusUnknown},
	}
	for _, tc := range cases {
		d := descriptor(t, tc.state)
		d.Access = tc.access
		if tc.stale {
			d.ObservedAt = instant(t, 1)
			d.FreshUntil = instant(t, 1)
		}
		resolution := resolveForEvaluation(t, r, d)
		got, err := readiness.Evaluate(r, resolution)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != tc.want {
			t.Fatalf("resolution %s evaluated as %s, want %s", resolution.Status, got.Status, tc.want)
		}
	}
}

func TestTodo_READINESS_003_RejectsUnpinnedAndUnsafeInputs(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	denied := descriptor(t, readiness.EvidenceSatisfied)
	denied.Access = readiness.EvidenceDenied
	resolution := resolveForEvaluation(t, r, denied)

	unpinned := r
	unpinned.CanonicalDigest = ""
	if _, err := readiness.Evaluate(unpinned, resolution); !errors.Is(err, readiness.ErrInvalidEvaluation) {
		t.Fatalf("unpinned requirement error = %v, want ErrInvalidEvaluation", err)
	}

	unsafe := resolution
	unsafe.Reasons = []string{"medical diagnosis: restricted"}
	if _, err := readiness.Evaluate(r, unsafe); !errors.Is(err, readiness.ErrInvalidEvaluation) {
		t.Fatalf("unsafe reason error = %v, want ErrInvalidEvaluation", err)
	}
}

func TestTodo_READINESS_003_DeduplicatesSafeReasons(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	e := descriptor(t, readiness.EvidenceSatisfied)
	e.ObservedAt = instant(t, 1)
	e.FreshUntil = instant(t, 1)
	resolution := resolveForEvaluation(t, r, e)
	resolution.Reasons = append(resolution.Reasons, resolution.Reasons...)
	got, err := readiness.Evaluate(r, resolution)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blockers) != 2 || got.Blockers[0] != "evidence_stale" || got.Blockers[1] != "requirement_not_satisfied" {
		t.Fatalf("blockers = %v, want two distinct safe reasons", got.Blockers)
	}
}

func TestTodo_READINESS_003_ValidatesDigestIntegrity(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	got, err := readiness.Evaluate(r, resolveForEvaluation(t, r, descriptor(t, readiness.EvidenceSatisfied)))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*readiness.Evaluation){
		"explanation": func(e *readiness.Evaluation) { e.ExplanationDigest = "sha256:tampered" },
		"evaluation":  func(e *readiness.Evaluation) { e.Digest = "sha256:tampered" },
		"resolution":  func(e *readiness.Evaluation) { e.ResolutionDigest = "sha256:tampered" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := got
			mutate(&candidate)
			if err := candidate.Validate(); !errors.Is(err, readiness.ErrInvalidEvaluation) {
				t.Fatalf("Validate() = %v, want ErrInvalidEvaluation", err)
			}
		})
	}
}
