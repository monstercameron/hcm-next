package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-126: simulation comparison presentation. The
// view-as panel renders one server-projected simulation,
// but no governed comparison presents two projections
// against each other: the first surface diffs outcomes
// and rule sets by convention and a flipped outcome can
// present as no change. The compiler needs the governed
// comparison — localized outcomes with a change verdict
// plus added, removed, and kept rules in projection order
// — so simulated deltas resolve today without evaluating
// policy.
func TestTodo_WEB_126(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	before := ComparedSimulation{Subject: "Avery", Allowed: true, Rules: []string{"rule-time", "rule-pay"}, Versions: []string{"v3"}}
	after := ComparedSimulation{Subject: "Avery", Allowed: false, Rules: []string{"rule-pay", "rule-leave"}, Versions: []string{"v4"}}

	compared := CompareSimulations(locale, before, after)
	if !compared.OutcomeChanged {
		t.Fatal("a flipped outcome presents as no change")
	}
	if compared.OutcomeBefore != locale.Text("view_as.allowed") || compared.OutcomeAfter != locale.Text("view_as.denied") {
		t.Fatalf("outcomes = %q -> %q", compared.OutcomeBefore, compared.OutcomeAfter)
	}
	if len(compared.AddedRules) != 1 || compared.AddedRules[0] != "rule-leave" {
		t.Fatalf("added = %+v", compared.AddedRules)
	}
	if len(compared.RemovedRules) != 1 || compared.RemovedRules[0] != "rule-time" {
		t.Fatalf("removed = %+v", compared.RemovedRules)
	}
	if len(compared.KeptRules) != 1 || compared.KeptRules[0] != "rule-pay" {
		t.Fatalf("kept = %+v", compared.KeptRules)
	}
	if compared.VersionsBefore != "v3" || compared.VersionsAfter != "v4" {
		t.Fatalf("versions = %q -> %q", compared.VersionsBefore, compared.VersionsAfter)
	}
	if compared.Notice != locale.Text("view_as.notice") {
		t.Fatalf("notice = %q", compared.Notice)
	}

	same := CompareSimulations(locale, before, before)
	if same.OutcomeChanged || len(same.AddedRules) != 0 || len(same.RemovedRules) != 0 {
		t.Fatalf("identical simulations compare dirty: %+v", same)
	}
	if len(same.KeptRules) != 2 {
		t.Fatalf("identical simulations keep %+v", same.KeptRules)
	}
}

// Golden: comparisons over projection pairs.
func TestTodo_WEB_126_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	a := ComparedSimulation{Subject: "A", Allowed: true, Rules: []string{"r1", "r2"}, Versions: []string{"v1"}}
	b := ComparedSimulation{Subject: "A", Allowed: false, Rules: []string{"r2", "r3"}, Versions: []string{"v1", "v2"}}
	c := ComparedSimulation{}
	pairs := [][2]ComparedSimulation{{a, b}, {b, a}, {a, a}, {c, c}, {c, a}}
	var builder strings.Builder
	for _, pair := range pairs {
		compared := CompareSimulations(locale, pair[0], pair[1])
		fmt.Fprintf(&builder, "%t|%s|%s|%s|%s\x00",
			compared.OutcomeChanged, compared.OutcomeBefore, compared.OutcomeAfter,
			compared.VersionsBefore, compared.VersionsAfter)
		fmt.Fprintf(&builder, "added:%s\x00", strings.Join(compared.AddedRules, ","))
		fmt.Fprintf(&builder, "removed:%s\x00", strings.Join(compared.RemovedRules, ","))
		fmt.Fprintf(&builder, "kept:%s\x00", strings.Join(compared.KeptRules, ","))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "c0a3a91d8ca23f12ccd2d3f2419eff3d78b82c7b3e839c21ef95fca1289b2934"
	if got != want {
		t.Fatalf("simulation comparison digest = %s, want %s", got, want)
	}
}

// Browser: comparison is deterministic and never mutates
// its projections.
func TestTodo_WEB_126_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	before := ComparedSimulation{Subject: "A", Allowed: true, Rules: []string{"r1"}}
	after := ComparedSimulation{Subject: "A", Allowed: false, Rules: []string{"r2"}}
	first := CompareSimulations(locale, before, after)
	second := CompareSimulations(locale, before, after)
	if !reflect.DeepEqual(before, ComparedSimulation{Subject: "A", Allowed: true, Rules: []string{"r1"}}) {
		t.Fatal("comparison mutates its projections")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("comparison is nondeterministic")
	}
}

// Conformance: rule diffs keep projection order,
// duplicates collapse, resolution is stable.
func TestTodo_WEB_126_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	before := ComparedSimulation{Rules: []string{"r2", "r1", "r1", "  "}}
	after := ComparedSimulation{Rules: []string{"r3", "r1", "r3"}}
	compared := CompareSimulations(locale, before, after)
	if !reflect.DeepEqual(compared.RemovedRules, []string{"r2"}) {
		t.Fatalf("removed = %+v", compared.RemovedRules)
	}
	if !reflect.DeepEqual(compared.AddedRules, []string{"r3"}) {
		t.Fatalf("added = %+v", compared.AddedRules)
	}
	if !reflect.DeepEqual(compared.KeptRules, []string{"r1"}) {
		t.Fatalf("kept = %+v", compared.KeptRules)
	}
	if !reflect.DeepEqual(compared, CompareSimulations(locale, before, after)) {
		t.Fatal("comparison is unstable")
	}
}

// Integration: two server-shaped simulation panels
// compare through their projections.
func TestTodo_WEB_126_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	panelBefore := PolicySimulationProps{Subject: "Avery", Allowed: true, Rules: []string{"rule-time"}, Versions: []string{"v3"}}
	panelAfter := PolicySimulationProps{Subject: "Avery", Allowed: false, Rules: []string{"rule-time", "rule-leave"}, Versions: []string{"v3"}}
	compared := CompareSimulations(locale,
		ComparedSimulation{Subject: panelBefore.Subject, Allowed: panelBefore.Allowed, Rules: panelBefore.Rules, Versions: panelBefore.Versions},
		ComparedSimulation{Subject: panelAfter.Subject, Allowed: panelAfter.Allowed, Rules: panelAfter.Rules, Versions: panelAfter.Versions})
	if !compared.OutcomeChanged {
		t.Fatal("panel outcome flip presents as no change")
	}
	if !reflect.DeepEqual(compared.AddedRules, []string{"rule-leave"}) {
		t.Fatalf("added = %+v", compared.AddedRules)
	}
	if len(compared.RemovedRules) != 0 {
		t.Fatalf("removed = %+v", compared.RemovedRules)
	}
}

// Fault: blank and nil projections compare clean —
// nothing invented, nothing changed.
func TestTodo_WEB_126_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	compared := CompareSimulations(locale, ComparedSimulation{}, ComparedSimulation{})
	if compared.OutcomeChanged {
		t.Fatal("blank projections flip the outcome")
	}
	if compared.OutcomeBefore != locale.Text("view_as.denied") || compared.OutcomeAfter != locale.Text("view_as.denied") {
		t.Fatalf("blank outcomes = %q -> %q", compared.OutcomeBefore, compared.OutcomeAfter)
	}
	for _, list := range [][]string{compared.AddedRules, compared.RemovedRules, compared.KeptRules} {
		if list == nil || len(list) != 0 {
			t.Fatalf("blank projections diff to %+v", list)
		}
	}
	if compared.VersionsBefore != "" || compared.VersionsAfter != "" {
		t.Fatal("blank projections version themselves")
	}
}
