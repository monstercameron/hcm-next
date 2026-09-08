package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-108: responsive My Work list-detail behavior.
// The collection hardcodes its list-detail flag, so nothing
// decides which panes a narrow viewport shows: both panes
// render regardless of width and selection, and the
// pixels-to-panes rule lives nowhere. CSS keeps owning
// pixels; the compiler needs the governed pane rule — wide
// shows both, narrow shows detail on selection and list
// otherwise, unknown layouts fail closed to the list.
func TestTodo_WEB_108(t *testing.T) {
	showList, showDetail := ResolveListDetailPanes(ListDetailWide, false)
	if !showList || !showDetail {
		t.Fatalf("wide without selection = %t/%t", showList, showDetail)
	}
	showList, showDetail = ResolveListDetailPanes(ListDetailWide, true)
	if !showList || !showDetail {
		t.Fatalf("wide with selection = %t/%t", showList, showDetail)
	}
	showList, showDetail = ResolveListDetailPanes(ListDetailNarrow, false)
	if !showList || showDetail {
		t.Fatalf("narrow without selection = %t/%t", showList, showDetail)
	}
	showList, showDetail = ResolveListDetailPanes(ListDetailNarrow, true)
	if showList || !showDetail {
		t.Fatalf("narrow with selection = %t/%t", showList, showDetail)
	}
	showList, showDetail = ResolveListDetailPanes(ListDetailLayout(0), true)
	if !showList || showDetail {
		t.Fatalf("unknown layout = %t/%t", showList, showDetail)
	}
}

// Golden: pane outcomes over layout/selection pairs.
func TestTodo_WEB_108_Golden(t *testing.T) {
	layouts := []ListDetailLayout{ListDetailLayout(0), ListDetailWide, ListDetailNarrow, ListDetailLayout(99)}
	var builder strings.Builder
	for _, layout := range layouts {
		for _, selected := range []bool{false, true} {
			showList, showDetail := ResolveListDetailPanes(layout, selected)
			if showList {
				builder.WriteString("L")
			}
			if showDetail {
				builder.WriteString("D")
			}
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "d2a8b6e93ff3144c144a3a0f3f32fc1ac10199a09ac5cbe05d5ccc1a96aba806"
	if got != want {
		t.Fatalf("list-detail digest = %s, want %s", got, want)
	}
}

// Browser: pane rules hold across repeated resolution.
func TestTodo_WEB_108_Browser(t *testing.T) {
	layouts := []ListDetailLayout{ListDetailLayout(0), ListDetailWide, ListDetailNarrow, ListDetailLayout(7)}
	for _, layout := range layouts {
		for _, selected := range []bool{false, true} {
			firstList, firstDetail := ResolveListDetailPanes(layout, selected)
			secondList, secondDetail := ResolveListDetailPanes(layout, selected)
			if firstList != secondList || firstDetail != secondDetail {
				t.Fatal("pane resolution is nondeterministic")
			}
			if !firstList && !firstDetail {
				t.Fatalf("layout %d selection %t shows nothing", layout, selected)
			}
		}
	}
}

// Conformance: wide always shows both; narrow shows exactly
// one; unknown fails closed to the list.
func TestTodo_WEB_108_Conformance(t *testing.T) {
	for _, selected := range []bool{false, true} {
		showList, showDetail := ResolveListDetailPanes(ListDetailWide, selected)
		if !showList || !showDetail {
			t.Fatalf("wide hides a pane: %t/%t", showList, showDetail)
		}
		narrowList, narrowDetail := ResolveListDetailPanes(ListDetailNarrow, selected)
		if narrowList == narrowDetail {
			t.Fatalf("narrow shows both or neither: %t/%t", narrowList, narrowDetail)
		}
		if narrowDetail != selected || narrowList == selected {
			t.Fatalf("narrow ignores selection: %t/%t", narrowList, narrowDetail)
		}
		unknownList, unknownDetail := ResolveListDetailPanes(ListDetailLayout(0), selected)
		if !unknownList || unknownDetail {
			t.Fatalf("unknown layout leaks detail: %t/%t", unknownList, unknownDetail)
		}
	}
	if !reflect.DeepEqual(
		func() []bool { l, d := ResolveListDetailPanes(ListDetailNarrow, true); return []bool{l, d} }(),
		[]bool{false, true},
	) {
		t.Fatal("narrow selection unstable")
	}
}
