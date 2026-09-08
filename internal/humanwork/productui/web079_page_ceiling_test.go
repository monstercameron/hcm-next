package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-079: page classification ceilings. A page definition
// declares a classification ceiling, and publication must refuse any
// composed widget binding whose data classification sits above it.
// Widget registration pins opaque classification limits (see
// widget.go) but nothing compares them: a confidential binding
// composes onto a public-ceiling page today. The lifecycle's next
// validate step needs a pure ceiling verdict with stable reasons,
// failing closed on undeclared ceilings and unranked labels — the
// Classification service taxonomy stays authoritative server-side.
func TestTodo_WEB_079(t *testing.T) {
	// The publication ladder ranks the registry's pinned labels plus
	// the platform catalog's restricted tier, least to most sensitive.
	for _, ranked := range []struct {
		label string
		rank  int
	}{
		{"public", 1},
		{"internal", 2},
		{"confidential", 3},
		{"restricted", 4},
	} {
		rank, ok := ClassificationRank(ranked.label)
		if !ok || rank != ranked.rank {
			t.Fatalf("rank(%q) = (%d, %t), want (%d, true)", ranked.label, rank, ok, ranked.rank)
		}
	}
	if _, ok := ClassificationRank("topsecret"); ok {
		t.Fatal("unknown label ranks")
	}

	binding := func(classification string) WidgetBinding {
		return WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
			Classification: classification, SourceType: "projection", SourceID: "journeys"}
	}
	for _, ceiling := range []struct {
		name     string
		ceiling  string
		bindings []WidgetBinding
		valid    bool
		reasons  []string
	}{
		{"at-and-below ceiling validates", "internal",
			[]WidgetBinding{binding("public"), binding("internal")}, true, nil},
		{"above ceiling refuses", "internal",
			[]WidgetBinding{binding("public"), {WidgetType: "proposal-form", WidgetVersion: 1, AuthorityClass: "manager",
				Classification: "confidential", SourceType: "workflow", SourceID: "intent-1"}},
			false, []string{`widget "proposal-form" classification "confidential" exceeds page ceiling "internal"`}},
		{"restricted ceiling admits the ladder", "restricted",
			[]WidgetBinding{binding("public"), binding("internal"), binding("confidential"), binding("restricted")}, true, nil},
		{"missing ceiling refuses", "",
			[]WidgetBinding{binding("public")}, false, []string{"missing classification ceiling"}},
		{"unknown ceiling refuses", "topsecret",
			[]WidgetBinding{binding("public")}, false, []string{`unknown classification ceiling "topsecret"`}},
		{"unclassified binding refuses", "internal",
			[]WidgetBinding{binding("")}, false, []string{`unclassified binding for widget "metric-display" clears no ceiling`}},
		{"unranked binding refuses", "restricted",
			[]WidgetBinding{binding("topsecret")}, false, []string{`unranked classification "topsecret" for widget "metric-display" clears no ceiling`}},
		{"empty composition validates under a declared ceiling", "public", nil, true, nil},
	} {
		draft := PageDraft{Page: "studio", Composition: PageComposition{ClassificationCeiling: ceiling.ceiling, Widgets: ceiling.bindings}}
		verdict := ValidateDraftCeiling(draft)
		if verdict.Compatible != ceiling.valid || !reflect.DeepEqual(verdict.Reasons, ceiling.reasons) {
			t.Fatalf("%s = (%t, %q), want (%t, %q)", ceiling.name, verdict.Compatible, verdict.Reasons, ceiling.valid, ceiling.reasons)
		}
	}
}

// Golden: ceiling verdicts over a ceiling-by-classification matrix.
func TestTodo_WEB_079_Golden(t *testing.T) {
	ceilings := []string{"public", "internal", "confidential", "restricted", "topsecret", ""}
	classifications := []string{"public", "internal", "confidential", "restricted", "", "topsecret"}
	var builder strings.Builder
	for _, ceiling := range ceilings {
		for _, classification := range classifications {
			draft := PageDraft{Page: "studio", Composition: PageComposition{ClassificationCeiling: ceiling,
				Widgets: []WidgetBinding{{WidgetType: "metric-display", Classification: classification}}}}
			verdict := ValidateDraftCeiling(draft)
			builder.WriteString(ceiling)
			builder.WriteString("\x00")
			builder.WriteString(classification)
			builder.WriteString("\x00")
			if verdict.Compatible {
				builder.WriteString("compatible")
			} else {
				builder.WriteString("incompatible")
			}
			builder.WriteString("\x00")
			builder.WriteString(strings.Join(verdict.Reasons, ";"))
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "0c08b5f23b452f2ae7311a29ae129f73d58f89d3224edb30a2a8c1214a56e908"
	if got != want {
		t.Fatalf("ceiling matrix digest = %s, want %s", got, want)
	}
}

// Browser: every registered widget limit ranks, and a ceiling at the
// top of the ladder admits a binding carrying each widget's own
// limit — deterministically.
func TestTodo_WEB_079_Browser(t *testing.T) {
	for _, widget := range RegisteredWidgets().Widgets {
		if _, ok := ClassificationRank(widget.ClassificationLimit); !ok {
			t.Fatalf("registered widget %q carries an unranked limit %q", widget.ID, widget.ClassificationLimit)
		}
		draft := PageDraft{Page: "studio", Composition: PageComposition{ClassificationCeiling: "restricted",
			Widgets: []WidgetBinding{{WidgetType: widget.ID, Classification: widget.ClassificationLimit}}}}
		first := ValidateDraftCeiling(draft)
		second := ValidateDraftCeiling(draft)
		if !first.Compatible {
			t.Fatalf("restricted ceiling refuses widget %q at its own limit: %q", widget.ID, first.Reasons)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("ceiling verdict for widget %q is nondeterministic", widget.ID)
		}
	}
}

// Conformance: ladder determinism, stable reasons, ceiling-gated
// edits across drafts.
func TestTodo_WEB_079_Conformance(t *testing.T) {
	for _, label := range []string{"public", "internal", "confidential", "restricted"} {
		first, firstOK := ClassificationRank(label)
		second, secondOK := ClassificationRank(label)
		if !firstOK || !secondOK || first != second {
			t.Fatalf("ladder rank for %q is unstable", label)
		}
	}
	draft := PageDraft{Page: "studio", Composition: PageComposition{ClassificationCeiling: "internal",
		Widgets: []WidgetBinding{{WidgetType: "metric-display", Classification: "confidential"}}}}
	first := ValidateDraftCeiling(draft)
	second := ValidateDraftCeiling(draft)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("ceiling reasons are unstable")
	}
	// Lowering the binding classification clears the same ceiling.
	draft.Composition.Widgets[0].Classification = "internal"
	if verdict := ValidateDraftCeiling(draft); !verdict.Compatible {
		t.Fatalf("reclassified binding still refused: %q", verdict.Reasons)
	}
}
