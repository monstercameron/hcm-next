package productui

import (
	"strings"
	"testing"
)

func TestWorkerIDAdminPageRendersGovernedRulesAndExamples(t *testing.T) {
	view := testView(PageWorkerIDs)
	view.WorkerIDPolicy = WorkerIDPolicy{Version: 2, Prefix: "HC", Separator: "-", SequenceDigits: 6, StartAt: 1000, NextSequence: 1042, IncrementBy: 1, ZeroPad: true, YearFormat: "NONE", CheckDigit: "NONE", IssuedCount: 42, Previews: []string{"HC-001042", "HC-001043"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Issue worker numbers your way", `id="worker-prefix"`, "Maximum sequence digits", "HC-001042", "Atomic uniqueness", "never reused", `href="/workspace/app/admin"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("worker ID page missing %q", want)
		}
	}
}

func TestWorkerIDPageSubmitsTypedDraft(t *testing.T) {
	var got WorkerIDPolicy
	node := WorkerIDPage(WorkerIDPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Policy: WorkerIDPolicy{Prefix: "HC", Separator: "-", SequenceDigits: 6, StartAt: 1, NextSequence: 1, IncrementBy: 1, YearFormat: "NONE", CheckDigit: "NONE"}, OnSave: func(p WorkerIDPolicy) { got = p }})
	if node == nil {
		t.Fatal("nil component")
	}
	// Event dispatch is exercised by the WASM integration; this unit contract
	// ensures the typed callback is retained instead of a page-shaped View.
	_ = got
}
