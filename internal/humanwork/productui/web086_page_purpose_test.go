package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-086: new-page purpose and audience setup. The studio
// authoring flow opens with purpose/audience, but the composition
// document carries neither: a new page cannot declare why it
// exists or who it serves, so later validate steps gate a
// definition with no stated intent. The lifecycle needs purpose
// and audience fields on the composition plus a single-concern
// setup verdict, failing closed on blank declarations. Audiences
// stay declared strings: any audience registry would duplicate
// the authorization sources that own who may open what.
func TestTodo_WEB_086(t *testing.T) {
	setup := PageComposition{Purpose: "Track promotion journeys", Audience: "managers"}
	if verdict := ValidatePagePurpose(setup); !verdict.Compatible {
		t.Fatalf("declared setup refuses: %q", verdict.Reasons)
	}
	for _, bad := range []struct {
		name        string
		composition PageComposition
		reasons     []string
	}{
		{"missing purpose", PageComposition{Audience: "managers"}, []string{"missing page purpose"}},
		{"missing audience", PageComposition{Purpose: "Track promotion journeys"}, []string{"missing page audience"}},
		{"missing both", PageComposition{}, []string{"missing page purpose", "missing page audience"}},
		{"whitespace purpose", PageComposition{Purpose: "  ", Audience: "managers"}, []string{"missing page purpose"}},
		{"whitespace audience", PageComposition{Purpose: "Track promotion journeys", Audience: " "}, []string{"missing page audience"}},
	} {
		verdict := ValidatePagePurpose(bad.composition)
		if verdict.Compatible || !reflect.DeepEqual(verdict.Reasons, bad.reasons) {
			t.Fatalf("%s = (%t, %q), want (false, %q)", bad.name, verdict.Compatible, verdict.Reasons, bad.reasons)
		}
	}

	// Setup is single-concern: the rest of the composition never
	// affects the verdict.
	rest := PageComposition{Purpose: "Track promotion journeys", Audience: "managers",
		Floorplan: "teleporter", ClassificationCeiling: "topsecret",
		Widgets: []WidgetBinding{{WidgetType: "teleporter"}}}
	if verdict := ValidatePagePurpose(rest); !verdict.Compatible {
		t.Fatalf("setup verdict leaks into other concerns: %q", verdict.Reasons)
	}
}

// Golden: setup verdicts over a declaration matrix.
func TestTodo_WEB_086_Golden(t *testing.T) {
	declarations := []PageComposition{
		{Purpose: "Track promotion journeys", Audience: "managers"},
		{Purpose: "", Audience: "managers"},
		{Purpose: "Track promotion journeys", Audience: ""},
		{},
		{Purpose: "  ", Audience: "  "},
		{Purpose: "Review completed work", Audience: "auditors"},
	}
	var builder strings.Builder
	for _, declaration := range declarations {
		verdict := ValidatePagePurpose(declaration)
		builder.WriteString(declaration.Purpose)
		builder.WriteString("\x00")
		builder.WriteString(declaration.Audience)
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
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "f59aa73e4386b75b83b9b9c510ce7cfe25f2b46b2e6659a54a0048f52bf98190"
	if got != want {
		t.Fatalf("setup digest = %s, want %s", got, want)
	}
}

// Browser: every registered page states its purpose and audience
// from its catalog identity — deterministically.
func TestTodo_WEB_086_Browser(t *testing.T) {
	for _, definition := range PageDefinitions() {
		setup := PageComposition{Purpose: definition.Title, Audience: definition.Label}
		first := ValidatePagePurpose(setup)
		second := ValidatePagePurpose(setup)
		if !first.Compatible {
			t.Fatalf("page %q catalog setup refuses: %q", definition.ID, first.Reasons)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("page %q setup verdict is nondeterministic", definition.ID)
		}
	}
}

// Conformance: verdict stability, declaration retention, reason
// order.
func TestTodo_WEB_086_Conformance(t *testing.T) {
	setup := PageComposition{Purpose: "Track promotion journeys", Audience: "managers"}
	if first, second := ValidatePagePurpose(setup), ValidatePagePurpose(setup); !reflect.DeepEqual(first, second) {
		t.Fatal("setup verdict is unstable")
	}
	if verdict := ValidatePagePurpose(PageComposition{Audience: "managers", Purpose: "x"}); !verdict.Compatible {
		t.Fatalf("short purpose refused without a stated length rule: %q", verdict.Reasons)
	}
	empty := ValidatePagePurpose(PageComposition{})
	if len(empty.Reasons) != 2 || empty.Reasons[0] != "missing page purpose" || empty.Reasons[1] != "missing page audience" {
		t.Fatalf("reason order = %q", empty.Reasons)
	}
}
