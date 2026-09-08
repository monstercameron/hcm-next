package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-125: the accessible validation summary. The
// summary component computes its title, entries, and field
// links inline at render time: the first consumer
// re-implements the error filter, the fail-closed copy
// rule, and the link allowlist by convention, and an
// unresolved key can reach a screen reader. The compiler
// needs the governed resolution — errors only, reviewed
// copy or the generic fallback, links only to registered
// fields — so every summary resolves today from one point.
func TestTodo_WEB_125(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	state := ValidationState{Issues: []ValidationIssue{
		{FieldID: "worker", MessageKey: "work.proposal_need_worker"},
		{FieldID: "elsewhere", Message: "Fix the date."},
		{FieldID: "", MessageKey: "missing.key", Message: "Use this."},
		{FieldID: "note", Message: "Just a note.", Severity: ValidationSeverityWarning},
	}}

	summary := ResolveValidationSummary(locale, state, []string{"worker"})
	if summary.Empty {
		t.Fatal("a failing state resolves to an empty summary")
	}
	if summary.Title != locale.Text("validation.summary_title") {
		t.Fatalf("title = %q", summary.Title)
	}
	if summary.Intro != locale.Text("validation.summary_intro") {
		t.Fatalf("intro = %q", summary.Intro)
	}
	if len(summary.Entries) != 3 {
		t.Fatalf("summary has %d entries", len(summary.Entries))
	}
	if summary.Entries[0].Text != locale.Text("work.proposal_need_worker") || !summary.Entries[0].Linked || summary.Entries[0].FieldID != "worker" {
		t.Fatalf("first entry = %+v", summary.Entries[0])
	}
	if summary.Entries[1].Text != "Fix the date." || summary.Entries[1].Linked {
		t.Fatalf("unregistered field links: %+v", summary.Entries[1])
	}
	if summary.Entries[2].Text != "Use this." || strings.Contains(summary.Entries[2].Text, "missing.key") {
		t.Fatalf("unreviewed key reaches the summary: %q", summary.Entries[2].Text)
	}

	if empty := ResolveValidationSummary(locale, ValidationState{}, nil); !empty.Empty || len(empty.Entries) != 0 {
		t.Fatalf("clean state resolves to %+v", empty)
	}
	warnings := ResolveValidationSummary(locale, ValidationState{Issues: []ValidationIssue{
		{FieldID: "note", Message: "Just a note.", Severity: ValidationSeverityWarning},
	}}, nil)
	if !warnings.Empty {
		t.Fatal("warnings resolve to an announced summary")
	}
}

// Golden: summaries over issue states.
func TestTodo_WEB_125_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	states := []ValidationState{
		{},
		{Issues: []ValidationIssue{{FieldID: "a", Message: "Fix a."}}},
		{Issues: []ValidationIssue{{FieldID: "a", MessageKey: "work.proposal_need_worker"}, {FieldID: "b", MessageKey: "missing.deep.key"}}},
		{Issues: []ValidationIssue{{FieldID: "w", Message: "Note.", Severity: ValidationSeverityWarning}}},
	}
	var builder strings.Builder
	for _, state := range states {
		summary := ResolveValidationSummary(locale, state, []string{"a"})
		fmt.Fprintf(&builder, "%t|%s|%s", summary.Empty, summary.Title, summary.Intro)
		for _, entry := range summary.Entries {
			fmt.Fprintf(&builder, "|%s|%s|%t", entry.FieldID, entry.Text, entry.Linked)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "eb3667f3dcd7e8148e534e786d0853caf7977a6fc9f7973faf545728510a0566"
	if got != want {
		t.Fatalf("validation summary digest = %s, want %s", got, want)
	}
}

// Browser: summary resolution is deterministic and never
// mutates its state.
func TestTodo_WEB_125_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	state := ValidationState{Issues: []ValidationIssue{{FieldID: "a", Message: "Fix a."}}}
	before := state
	first := ResolveValidationSummary(locale, state, []string{"a"})
	second := ResolveValidationSummary(locale, state, []string{"a"})
	if !reflect.DeepEqual(state, before) {
		t.Fatal("resolution mutates its state")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("resolution is nondeterministic")
	}
}

// Conformance: entries keep state order, links need
// registration, resolution is stable.
func TestTodo_WEB_125_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	state := ValidationState{Issues: []ValidationIssue{
		{FieldID: "b", Message: "Second."},
		{FieldID: "a", Message: "First."},
	}}
	summary := ResolveValidationSummary(locale, state, []string{"a", "b"})
	if len(summary.Entries) != 2 || summary.Entries[0].Text != "Second." || summary.Entries[1].Text != "First." {
		t.Fatalf("entries reorder: %+v", summary.Entries)
	}
	for _, entry := range summary.Entries {
		if !entry.Linked {
			t.Fatalf("registered field unlinked: %+v", entry)
		}
	}
	if !reflect.DeepEqual(summary, ResolveValidationSummary(locale, state, []string{"a", "b"})) {
		t.Fatal("resolution is unstable")
	}
}

// Integration: an incomplete proposal guide resolves to
// an announced summary naming every missing fact.
func TestTodo_WEB_125_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	guide := ResolveProposalGuide(locale, ProposalCollection{WorkerRef: "worker-avery"})
	var issues []ValidationIssue
	for _, step := range guide.Steps {
		if !step.Complete {
			issues = append(issues, ValidationIssue{Message: step.Title + ": " + step.Detail})
		}
	}
	summary := ResolveValidationSummary(locale, ValidationState{Issues: issues}, nil)
	if summary.Empty {
		t.Fatal("missing proposal facts resolve to silence")
	}
	if len(summary.Entries) != 2 {
		t.Fatalf("summary names %d facts", len(summary.Entries))
	}
	for _, entry := range summary.Entries {
		if entry.Linked {
			t.Fatalf("guide text links nowhere: %+v", entry)
		}
	}
}

// Fault: key-shaped messages, blank messages, and blank
// field IDs never leak keys or links.
func TestTodo_WEB_125_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	summary := ResolveValidationSummary(locale, ValidationState{Issues: []ValidationIssue{
		{Message: "work.proposal_need_worker"},
		{Message: "   "},
		{FieldID: "   ", Message: "Fix it."},
	}}, []string{"   "})
	if summary.Empty || len(summary.Entries) != 3 {
		t.Fatalf("faults resolve to %+v", summary)
	}
	for _, entry := range summary.Entries {
		if strings.Contains(entry.Text, "work.proposal_need_worker") || strings.HasPrefix(entry.Text, "⟦") {
			t.Fatalf("key reaches the summary: %q", entry.Text)
		}
		if entry.Linked {
			t.Fatalf("blank field links: %+v", entry)
		}
	}
	if summary.Entries[1].Text != locale.Text("validation.generic_error") {
		t.Fatalf("blank message = %q", summary.Entries[1].Text)
	}
}
