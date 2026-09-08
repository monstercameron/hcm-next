package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-128: proposal confirmation review. Guided
// collection, field comparison, and the validation summary
// each resolve alone, but no governed review composes
// them into the confirmation a submitter reads: the first
// surface assembles change, readiness, and missing facts
// by convention and an incomplete proposal can present as
// ready to confirm. The compiler needs the governed
// review — the values comparison, the guide verdict, and
// the missing-fact summary from one point — so
// confirmation resolves today and ready means reviewable,
// never authorized.
func TestTodo_WEB_128(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	input := ProposalCollection{WorkerRef: "worker-avery", Current: "$118,000", Proposed: "$121,000", EffectiveDate: "2026-10-01"}

	review := ResolveProposalConfirmation(locale, input)
	if !review.Ready {
		t.Fatal("a filled proposal does not present as ready")
	}
	if !review.Change.Changed || review.Change.Current != "$118,000" || review.Change.Proposed != "$121,000" {
		t.Fatalf("review change = %+v", review.Change)
	}
	if review.EffectiveDate != "2026-10-01" || review.WorkerRef != "worker-avery" {
		t.Fatalf("review facts = %+v", review)
	}
	if !review.Guide.Complete {
		t.Fatal("review guide incomplete on a filled proposal")
	}
	if !review.Missing.Empty {
		t.Fatalf("ready review lists missing facts: %+v", review.Missing.Entries)
	}

	empty := ResolveProposalConfirmation(locale, ProposalCollection{})
	if empty.Ready {
		t.Fatal("an empty proposal presents as ready")
	}
	if empty.Change.Changed {
		t.Fatal("empty values present as changed")
	}
	if empty.Missing.Empty || len(empty.Missing.Entries) != 3 {
		t.Fatalf("empty review misses %+v", empty.Missing.Entries)
	}
}

// Golden: reviews over collection inputs.
func TestTodo_WEB_128_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	inputs := []ProposalCollection{
		{},
		{WorkerRef: "w-1"},
		{WorkerRef: "w-1", Current: "a", Proposed: "b", EffectiveDate: "d"},
		{WorkerRef: "w-1", Current: "a", Proposed: "a", EffectiveDate: "d"},
	}
	var builder strings.Builder
	for _, input := range inputs {
		review := ResolveProposalConfirmation(locale, input)
		fmt.Fprintf(&builder, "%t|%t|%s|%s|%s|%t", review.Ready,
			review.Change.Changed, review.Change.Marker, review.EffectiveDate, review.WorkerRef, review.Missing.Empty)
		for _, entry := range review.Missing.Entries {
			fmt.Fprintf(&builder, "|%s", entry.Text)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "8d0fc1177ff59d0d8d1e7cf2954367aedd675bbdcec0006fe1c73587f5ead007"
	if got != want {
		t.Fatalf("proposal review digest = %s, want %s", got, want)
	}
}

// Browser: review resolution is deterministic and never
// mutates its inputs.
func TestTodo_WEB_128_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	input := ProposalCollection{WorkerRef: "w-1", Current: "a", Proposed: "b", EffectiveDate: "d"}
	before := input
	first := ResolveProposalConfirmation(locale, input)
	second := ResolveProposalConfirmation(locale, input)
	if !reflect.DeepEqual(input, before) {
		t.Fatal("review resolution mutates its inputs")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("review resolution is nondeterministic")
	}
}

// Conformance: the review reuses its governors — same
// change verdict, same guide, same missing copy — and is
// stable.
func TestTodo_WEB_128_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	input := ProposalCollection{WorkerRef: "w-1", Current: "a", Proposed: "b"}
	review := ResolveProposalConfirmation(locale, input)
	if !reflect.DeepEqual(review.Change, CompareFieldValue(locale, locale.Text("work.proposal_step_values"), "a", "b")) {
		t.Fatalf("review change diverges: %+v", review.Change)
	}
	if !reflect.DeepEqual(review.Guide, ResolveProposalGuide(locale, input)) {
		t.Fatal("review guide diverges")
	}
	if review.Ready {
		t.Fatal("a dateless proposal presents as ready")
	}
	if !reflect.DeepEqual(review, ResolveProposalConfirmation(locale, input)) {
		t.Fatal("resolution is unstable")
	}
}

// Integration: completing the guide flips the review to
// ready with an empty missing summary.
func TestTodo_WEB_128_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	input := ProposalCollection{WorkerRef: "w-1", Current: "a", Proposed: "b"}
	if got := ResolveProposalConfirmation(locale, input); got.Ready || got.Missing.Empty {
		t.Fatalf("dateless review = %+v", got)
	}
	input.EffectiveDate = "2026-10-01"
	ready := ResolveProposalConfirmation(locale, input)
	if !ready.Ready || !ready.Missing.Empty || !ready.Guide.Complete {
		t.Fatalf("filled review = %+v", ready)
	}
}

// Fault: half-filled values and whitespace facts never
// present as ready.
func TestTodo_WEB_128_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	faults := []ProposalCollection{
		{WorkerRef: "w-1", Current: "a", EffectiveDate: "d"},
		{WorkerRef: "   ", Current: "a", Proposed: "b", EffectiveDate: "d"},
		{WorkerRef: "w-1", Current: "  ", Proposed: "b", EffectiveDate: "d"},
	}
	for i, input := range faults {
		if got := ResolveProposalConfirmation(locale, input); got.Ready {
			t.Fatalf("fault %d presents as ready", i)
		}
	}
}
