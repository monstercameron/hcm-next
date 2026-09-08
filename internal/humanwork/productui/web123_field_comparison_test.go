package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-123: current-versus-proposed field
// presentation. The work preview renders current and
// proposed base pay as adjacent facts with no governed
// comparison: the first surface decides changed-versus-
// unchanged by convention and equal values can present as
// a change. The compiler needs the governed comparison —
// already-formatted values compared exactly with a
// localized change marker — so proposal fields resolve
// today without a second formatting authority.
func TestTodo_WEB_123(t *testing.T) {
	locale := ResolveProductLocale("en-US")

	changed := CompareFieldValue(locale, "Base pay", "$118,000", "$121,000")
	if !changed.Changed {
		t.Fatal("differing values present as unchanged")
	}
	if changed.Marker != locale.Text("work.field_changed") {
		t.Fatalf("change marker = %q", changed.Marker)
	}
	if changed.Label != "Base pay" || changed.Current != "$118,000" || changed.Proposed != "$121,000" {
		t.Fatalf("comparison drops its values: %+v", changed)
	}

	same := CompareFieldValue(locale, "Base pay", "$118,000", "$118,000")
	if same.Changed {
		t.Fatal("equal values present as changed")
	}
	if same.Marker != "" {
		t.Fatalf("unchanged comparison carries a marker: %q", same.Marker)
	}
}

// Golden: comparisons over value pairs.
func TestTodo_WEB_123_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	pairs := [][2]string{
		{"a", "b"}, {"a", "a"}, {"", ""}, {"", "b"}, {"b", ""}, {" a", "a"},
	}
	var builder strings.Builder
	for _, pair := range pairs {
		compared := CompareFieldValue(locale, "Field", pair[0], pair[1])
		fmt.Fprintf(&builder, "%s|%s|%t|%s\x00", compared.Current, compared.Proposed, compared.Changed, compared.Marker)
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "afba58094c52b1a2a63b6fae49212d23b628ae986b50051d16f8cad0728065bb"
	if got != want {
		t.Fatalf("field comparison digest = %s, want %s", got, want)
	}
}

// Browser: comparison is deterministic and pure.
func TestTodo_WEB_123_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	inputs := [][2]string{{"x", "y"}, {"x", "x"}, {"", ""}}
	for _, input := range inputs {
		first := CompareFieldValue(locale, "F", input[0], input[1])
		second := CompareFieldValue(locale, "F", input[0], input[1])
		if !reflect.DeepEqual(first, second) {
			t.Fatal("comparison is nondeterministic")
		}
	}
}

// Conformance: comparison is exact — whitespace counts —
// and the marker comes from the catalog.
func TestTodo_WEB_123_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	if !CompareFieldValue(locale, "F", " a", "a").Changed {
		t.Fatal("comparison folds whitespace by convention")
	}
	if CompareFieldValue(locale, "F", "", "").Changed {
		t.Fatal("two unreported values present as changed")
	}
	changed := CompareFieldValue(locale, "F", "1", "2")
	if changed.Marker == "" || changed.Marker != ResolveProductLocale("en-US").Text("work.field_changed") {
		t.Fatal("change marker is not catalog copy")
	}
	if !reflect.DeepEqual(changed, CompareFieldValue(locale, "F", "1", "2")) {
		t.Fatal("comparison is unstable")
	}
}

// Integration: money-formatted proposal values track the
// numeric difference through the real formatter.
func TestTodo_WEB_123_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	current := money(locale, testMoney("118000", "CAD"))
	raised := money(locale, testMoney("121000", "CAD"))
	if got := CompareFieldValue(locale, "Base pay", current, raised); !got.Changed {
		t.Fatal("a raise presents as unchanged")
	}
	if got := CompareFieldValue(locale, "Base pay", current, current); got.Changed {
		t.Fatal("an unchanged base presents as changed")
	}
}

// Fault: undisclosed values compare as their presented
// text, never as raw amounts.
func TestTodo_WEB_123_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	hidden := locale.Text("common.not_disclosed")
	if got := CompareFieldValue(locale, "Base pay", hidden, hidden); got.Changed {
		t.Fatal("two withheld values present as changed")
	}
	if got := CompareFieldValue(locale, "Base pay", hidden, "$1"); !got.Changed {
		t.Fatal("withheld-versus-value presents as unchanged")
	}
}
