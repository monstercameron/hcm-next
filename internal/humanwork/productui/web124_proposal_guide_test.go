package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-124: guided proposal collection. The proposal
// launcher links into a new-proposal mode, but no governed
// guide collects the proposal's facts: the first surface
// invents its own step order and completion rule by
// convention, and a half-filled proposal can present as
// ready. The compiler needs the governed guide — the
// proposal's own facts (worker, current/proposed values,
// effective date) as ordered steps with completion derived
// from the supplied inputs — so collection resolves today
// and readiness means every fact present.
func TestTodo_WEB_124(t *testing.T) {
	locale := ResolveProductLocale("en-US")

	empty := ResolveProposalGuide(locale, ProposalCollection{})
	if empty.Complete {
		t.Fatal("an empty collection presents as ready")
	}
	if empty.Current != 0 {
		t.Fatalf("empty collection starts at step %d", empty.Current)
	}
	if len(empty.Steps) != 3 {
		t.Fatalf("guide has %d steps", len(empty.Steps))
	}

	full := ResolveProposalGuide(locale, ProposalCollection{
		WorkerRef: "worker-avery", Current: "$118,000", Proposed: "$121,000", EffectiveDate: "2026-10-01",
	})
	if !full.Complete {
		t.Fatal("a filled collection does not present as ready")
	}
	if full.Current != -1 {
		t.Fatalf("ready guide points at step %d", full.Current)
	}
	for i, step := range full.Steps {
		if !step.Complete {
			t.Fatalf("step %d incomplete on a filled collection", i)
		}
		if step.Title == "" {
			t.Fatalf("step %d untitled", i)
		}
	}

	partial := ResolveProposalGuide(locale, ProposalCollection{WorkerRef: "worker-avery"})
	if partial.Complete {
		t.Fatal("a worker-only collection presents as ready")
	}
	if partial.Current != 1 {
		t.Fatalf("worker-only collection points at step %d", partial.Current)
	}
	if !partial.Steps[0].Complete || partial.Steps[1].Complete || partial.Steps[2].Complete {
		t.Fatalf("partial completion misreported: %+v", partial.Steps)
	}
}

// Golden: guides over collection inputs.
func TestTodo_WEB_124_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	inputs := []ProposalCollection{
		{},
		{WorkerRef: "w-1"},
		{WorkerRef: "w-1", Current: "a", Proposed: "b"},
		{WorkerRef: "w-1", Current: "a", Proposed: "b", EffectiveDate: "2026-10-01"},
		{WorkerRef: "  ", Current: "a", Proposed: "a", EffectiveDate: "2026-10-01"},
	}
	var builder strings.Builder
	for _, input := range inputs {
		guide := ResolveProposalGuide(locale, input)
		fmt.Fprintf(&builder, "%d|%t", guide.Current, guide.Complete)
		for _, step := range guide.Steps {
			fmt.Fprintf(&builder, "|%s|%t|%s", step.Title, step.Complete, step.Detail)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "d8879378da38f8d8b9f1db519f108964ed9a80fff93a291b4b5b6c9e0f1e836c"
	if got != want {
		t.Fatalf("proposal guide digest = %s, want %s", got, want)
	}
}

// Browser: guide resolution is deterministic and pure.
func TestTodo_WEB_124_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	inputs := []ProposalCollection{
		{},
		{WorkerRef: "w-1", Current: "a", Proposed: "b", EffectiveDate: "d"},
	}
	for _, input := range inputs {
		before := input
		first := ResolveProposalGuide(locale, input)
		second := ResolveProposalGuide(locale, input)
		if !reflect.DeepEqual(input, before) {
			t.Fatal("guide resolution mutates its inputs")
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("guide resolution is nondeterministic")
		}
	}
}

// Conformance: step order is fixed, titles are distinct
// catalog copy, resolution is stable.
func TestTodo_WEB_124_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	guide := ResolveProposalGuide(locale, ProposalCollection{})
	seen := map[string]bool{}
	for i, step := range guide.Steps {
		if step.Title == "" {
			t.Fatalf("step %d untitled", i)
		}
		if seen[step.Title] {
			t.Fatalf("step title repeats: %q", step.Title)
		}
		seen[step.Title] = true
		if !step.Complete && step.Detail == "" {
			t.Fatalf("incomplete step %d states no requirement", i)
		}
	}
	if !reflect.DeepEqual(guide, ResolveProposalGuide(locale, ProposalCollection{})) {
		t.Fatal("resolution is unstable")
	}
}

// Integration: filling the collection in order advances
// the guide one step at a time to ready.
func TestTodo_WEB_124_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	input := ProposalCollection{}
	if got := ResolveProposalGuide(locale, input); got.Current != 0 || got.Complete {
		t.Fatalf("empty guide = %+v", got)
	}
	input.WorkerRef = "worker-avery"
	if got := ResolveProposalGuide(locale, input); got.Current != 1 || got.Complete {
		t.Fatalf("worker guide = %+v", got)
	}
	input.Current = money(locale, testMoney("118000", "CAD"))
	input.Proposed = money(locale, testMoney("121000", "CAD"))
	if got := ResolveProposalGuide(locale, input); got.Current != 2 || got.Complete {
		t.Fatalf("valued guide = %+v", got)
	}
	input.EffectiveDate = "2026-10-01"
	if got := ResolveProposalGuide(locale, input); got.Current != -1 || !got.Complete {
		t.Fatalf("filled guide = %+v", got)
	}
}

// Fault: whitespace-only facts, half-filled values, and
// date-only input never present as ready.
func TestTodo_WEB_124_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	faults := []ProposalCollection{
		{WorkerRef: "   ", Current: "a", Proposed: "b", EffectiveDate: "d"},
		{WorkerRef: "w-1", Current: "a", EffectiveDate: "d"},
		{WorkerRef: "w-1", Proposed: "b", EffectiveDate: "d"},
		{EffectiveDate: "2026-10-01"},
	}
	for i, input := range faults {
		if got := ResolveProposalGuide(locale, input); got.Complete {
			t.Fatalf("fault %d presents as ready", i)
		}
	}
	if got := ResolveProposalGuide(locale, ProposalCollection{WorkerRef: "w-1", Current: "a", EffectiveDate: "d"}); got.Current != 1 {
		t.Fatalf("half-valued collection points at step %d", got.Current)
	}
}
