package binding

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
)

func TestWireMethodRefAndValidity(t *testing.T) {
	m := WireMethod{ServiceFullName: "hcmnext.intents.v1.IntentService", MethodName: "SimulateIntent"}
	if got, want := m.Ref(), "hcmnext.intents.v1.IntentService/SimulateIntent"; got != want {
		t.Errorf("Ref() = %q, want %q", got, want)
	}
	if !m.Valid() {
		t.Error("a method with both halves present reported invalid")
	}
	for _, bad := range []WireMethod{
		{MethodName: "SimulateIntent"},
		{ServiceFullName: "hcmnext.intents.v1.IntentService"},
		{},
	} {
		if bad.Valid() {
			t.Errorf("%+v reported valid; a half-named method can never identify a route", bad)
		}
	}
}

func TestHandlerSymbolRefDistinguishesMethodsFromFunctions(t *testing.T) {
	method := HandlerSymbol{PackagePath: "internal/intent/app", Receiver: "*domainHandlers", Name: "detectDrift"}
	if got, want := method.Ref(), "internal/intent/app.(*domainHandlers).detectDrift"; got != want {
		t.Errorf("method Ref() = %q, want %q", got, want)
	}
	fn := HandlerSymbol{PackagePath: "internal/intent/app", Name: "NewCell"}
	if got, want := fn.Ref(), "internal/intent/app.NewCell"; got != want {
		t.Errorf("function Ref() = %q, want %q", got, want)
	}
	// A method and a function of the same name in the same package are
	// different symbols and must not collide.
	same := HandlerSymbol{PackagePath: "internal/intent/app", Name: "detectDrift"}
	if same.Ref() == method.Ref() {
		t.Error("a package-level function and a method of the same name produced the same reference")
	}
	if !method.Valid() || !fn.Valid() {
		t.Error("a fully named symbol reported invalid")
	}
	if (HandlerSymbol{Name: "x"}).Valid() || (HandlerSymbol{PackagePath: "p"}).Valid() {
		t.Error("a half-named symbol reported valid")
	}
}

func TestHandlerIndexHasAndRefs(t *testing.T) {
	sym := HandlerSymbol{PackagePath: "internal/fixture", Receiver: "*h", Name: "a"}
	other := HandlerSymbol{PackagePath: "internal/fixture", Receiver: "*h", Name: "b"}
	index := HandlerIndex{sym.Ref(): true}
	if !index.Has(sym) {
		t.Error("Has reported a present symbol absent")
	}
	if index.Has(other) {
		t.Error("Has reported an absent symbol present")
	}
	if got := index.Refs(); len(got) != 1 || got[0] != sym.Ref() {
		t.Errorf("Refs() = %v, want [%s]", got, sym.Ref())
	}
	var nilIndex HandlerIndex
	if nilIndex.Has(sym) {
		t.Error("a nil index reported a symbol present")
	}
}

func TestGapKindValidityIsClosed(t *testing.T) {
	declared := []GapKind{
		GapUnclaimedCapability, GapNoWireMethod, GapAmbiguousWireMethod,
		GapUnknownWireMethod, GapStreamingWireMethod, GapNoHandler,
		GapAmbiguousHandler, GapMissingHandlerSymbol, GapHandlerBoundTwice,
		GapNoModelBinding, GapWireMethodUnbound, GapClaimWithoutCapability,
	}
	if len(declared) != 12 {
		t.Fatalf("the declared kind list has %d entries; the doc says twelve", len(declared))
	}
	seen := map[GapKind]bool{}
	for _, k := range declared {
		if !k.Valid() {
			t.Errorf("declared kind %q reported invalid", k)
		}
		if seen[k] {
			t.Errorf("kind %q is declared twice", k)
		}
		seen[k] = true
	}
	for _, bad := range []GapKind{"", "OTHER", "no_wire_method", "NO_WIRE_METHODS"} {
		if bad.Valid() {
			t.Errorf("undeclared kind %q reported valid; the set must be closed", bad)
		}
	}
}

func TestGapIDIsStableAgainstDetailRewording(t *testing.T) {
	base := Gap{Kind: GapNoHandler, Capability: "c", Subject: "s", Detail: "first wording"}
	reworded := base
	reworded.Detail = "a completely different explanation"
	if base.ID() != reworded.ID() {
		t.Error("re-wording Detail changed the gap identity; the allowlist would silently re-open")
	}
	changedSubject := base
	changedSubject.Subject = "s2"
	if base.ID() == changedSubject.ID() {
		t.Error("changing the subject did not change the gap identity")
	}
	if !strings.Contains(base.String(), base.ID()) {
		t.Error("String() does not carry the gap id")
	}
}

func TestTableAccessorsAndExplain(t *testing.T) {
	table := Table{
		SymbolsChecked: true,
		Entries: []Entry{{
			Capability:      capability.Key{ID: "cap.one", Version: 1},
			OwnerDomain:     "people",
			EffectClass:     capability.EffectReadOnly,
			DefinitionRef:   "cap.one/v1",
			Wire:            WireMethod{ServiceFullName: "svc", MethodName: "M"},
			Handler:         HandlerSymbol{PackagePath: "p", Receiver: "*h", Name: "f"},
			Entities:        []string{"Worker/v1"},
			ReadProperties:  []string{"worker.identity"},
			WriteProperties: nil,
		}},
		Gaps: []Gap{
			{Kind: GapNoHandler, Capability: "cap.two", Detail: "no handler"},
			{Kind: GapNoHandler, Capability: "cap.two", Detail: "duplicate row, same identity"},
			{Kind: GapWireMethodUnbound, Subject: "svc/Other", Detail: "unbound"},
		},
	}

	if table.FullyBound() {
		t.Error("a table with gaps reported fully bound")
	}
	if ids := table.GapIDs(); len(ids) != 2 {
		t.Errorf("GapIDs() = %v; the two identical-identity gaps must collapse to one", ids)
	}
	if got := table.GapsOfKind(GapNoHandler); len(got) != 2 {
		t.Errorf("GapsOfKind(NO_HANDLER) returned %d, want 2", len(got))
	}
	if got := table.GapsOfKind(GapStreamingWireMethod); len(got) != 0 {
		t.Errorf("GapsOfKind for an absent kind returned %d rows", len(got))
	}

	explain := table.Explain()
	for _, want := range []string{
		"1 bound, 3 gaps",
		"verified against the source tree",
		"BOUND cap.one/v1 -> wire svc/M -> handler p.(*h).f",
		"entities [Worker/v1]",
		"GAP   NO_HANDLER|cap.two|",
	} {
		if !strings.Contains(explain, want) {
			t.Errorf("Explain() is missing %q:\n%s", want, explain)
		}
	}

	unchecked := table
	unchecked.SymbolsChecked = false
	if !strings.Contains(unchecked.Explain(), "NOT verified") {
		t.Error("an unverified table did not say so; a silent nil index would read as a clean tree")
	}

	empty := Table{}
	if !empty.FullyBound() {
		t.Error("a table with no gaps did not report fully bound")
	}
}

func TestEntryWithNoDefinitionRefExplainsAsNone(t *testing.T) {
	table := Table{Entries: []Entry{{
		Capability: capability.Key{ID: "cap", Version: 1},
		Wire:       WireMethod{ServiceFullName: "svc", MethodName: "M"},
		Handler:    HandlerSymbol{PackagePath: "p", Name: "f"},
	}}}
	if !strings.Contains(table.Explain(), "definition (none)") {
		t.Errorf("an entry with no definition ref did not render as (none):\n%s", table.Explain())
	}
}
