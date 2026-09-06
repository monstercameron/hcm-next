package binding

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/intent/modelbinding"
)

func TestBuildOverTheLiveTreeIsDeterministic(t *testing.T) {
	index := liveHandlerIndex(t)
	first, err := Build(index)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	second, err := Build(index)
	if err != nil {
		t.Fatalf("Build (second): %v", err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("two builds of the same tree disagreed:\n %s\n %s", first.Digest(), second.Digest())
	}
	if len(first.Entries) != len(second.Entries) || len(first.Gaps) != len(second.Gaps) {
		t.Fatalf("two builds produced different shapes: %d/%d vs %d/%d",
			len(first.Entries), len(first.Gaps), len(second.Entries), len(second.Gaps))
	}
}

// TestBuildWithoutAnIndexDoesNotVerifySymbols: a nil index must not be read
// as "every claimed symbol exists".
func TestBuildWithoutAnIndexDoesNotVerifySymbols(t *testing.T) {
	table := BuildFrom(
		[]capability.Record{fixtureRecord(fixtureCapabilityID)},
		fixtureWire(),
		[]Claim{{
			CapabilityID:  fixtureCapabilityID,
			DefinitionRef: fixtureDefinition,
			WireMethods:   []string{fixtureService + "/ExplainWorkerState"},
			Handlers:      []HandlerSymbol{fixtureHandler("thisDoesNotExistAnywhere")},
		}},
		fixtureModels(),
		nil,
	)
	if table.SymbolsChecked {
		t.Error("SymbolsChecked is true with a nil index")
	}
	if len(table.GapsOfKind(GapMissingHandlerSymbol)) != 0 {
		t.Error("a nil index produced a MISSING_HANDLER_SYMBOL gap; nothing was scanned to say so")
	}
	if len(table.Entries) != 1 {
		t.Fatalf("without symbol verification the claim should still bind; got %d entries:\n%s", len(table.Entries), table.Explain())
	}
	// ...and the table says out loud that nothing was verified.
	if !strings.Contains(table.Explain(), "NOT verified") {
		t.Error("the table did not disclose that symbols were unverified")
	}
}

// TestBuildFromNeverFails: every problem must be a typed gap, because "the
// join could not be computed" and "the join found a hole" must not look the
// same to a caller.
func TestBuildFromNeverFails(t *testing.T) {
	// The most hostile input available: no capabilities, no methods, a
	// claim for something unpublished, and no model bindings.
	table := BuildFrom(nil, nil, []Claim{{CapabilityID: "ghost"}}, modelbinding.Table{}, HandlerIndex{})
	if len(table.Gaps) != 1 || table.Gaps[0].Kind != GapClaimWithoutCapability {
		t.Fatalf("expected exactly one CLAIM_WITHOUT_CAPABILITY gap, got:\n%s", table.Explain())
	}
	if len(table.Entries) != 0 {
		t.Errorf("an empty world produced %d entries", len(table.Entries))
	}
}

// TestBuildFromReportsEveryFailureForOneCapability: a capability with three
// problems must report three gaps, not stop at the first. A checker that
// reports one hole at a time turns a review into a queue.
func TestBuildFromReportsEveryFailureForOneCapability(t *testing.T) {
	table := BuildFrom(
		[]capability.Record{fixtureRecord(fixtureCapabilityID)},
		fixtureWire(),
		[]Claim{{
			CapabilityID:  fixtureCapabilityID,
			DefinitionRef: "fixture.people.nonexistent/v1",
			WireMethods:   []string{fixtureService + "/DoesNotExist"},
			Handlers:      nil,
		}},
		fixtureModels(),
		HandlerIndex{},
	)
	for _, want := range []GapKind{GapUnknownWireMethod, GapNoWireMethod, GapNoHandler, GapNoModelBinding} {
		if !hasGap(table, want, fixtureCapabilityID) {
			t.Errorf("expected a %s gap:\n%s", want, table.Explain())
		}
	}
}

// TestSharedHandlerGapNamesEveryOwner so a reviewer can see the collision
// without re-deriving it.
func TestSharedHandlerGapNamesEveryOwner(t *testing.T) {
	claims := []Claim{
		{CapabilityID: "a", DefinitionRef: fixtureDefinition, WireMethods: []string{fixtureService + "/ExplainWorkerState"}, Handlers: []HandlerSymbol{fixtureHandler("shared")}},
		{CapabilityID: "b", DefinitionRef: fixtureDefinition, WireMethods: []string{fixtureService + "/OtherMethod"}, Handlers: []HandlerSymbol{fixtureHandler("shared")}},
		{CapabilityID: "c", DefinitionRef: fixtureDefinition, WireMethods: []string{fixtureService + "/OtherMethod"}, Handlers: []HandlerSymbol{fixtureHandler("shared")}},
	}
	table := BuildFrom(
		[]capability.Record{fixtureRecord("a"), fixtureRecord("b"), fixtureRecord("c")},
		fixtureWire(),
		claims,
		fixtureModels(),
		fixtureIndex("shared"),
	)
	shared := table.GapsOfKind(GapHandlerBoundTwice)
	if len(shared) != 1 {
		t.Fatalf("expected one shared-handler gap, got %d:\n%s", len(shared), table.Explain())
	}
	if !strings.Contains(shared[0].Detail, "a, b, c") {
		t.Errorf("the shared-handler gap does not name every owner: %q", shared[0].Detail)
	}
}

// TestUnknownWireMethodDoesNotCountAsClaimed: a claim naming a nonexistent
// route must not make a real route look claimed, nor suppress the unbound
// report for the routes nobody claimed.
func TestUnknownWireMethodDoesNotCountAsClaimed(t *testing.T) {
	table := BuildFrom(
		[]capability.Record{fixtureRecord(fixtureCapabilityID)},
		fixtureWire(),
		[]Claim{{
			CapabilityID:  fixtureCapabilityID,
			DefinitionRef: fixtureDefinition,
			WireMethods:   []string{fixtureService + "/Ghost"},
			Handlers:      []HandlerSymbol{fixtureHandler("explainWorkerState")},
		}},
		fixtureModels(),
		fixtureIndex("explainWorkerState"),
	)
	if got := len(table.GapsOfKind(GapWireMethodUnbound)); got != 3 {
		t.Fatalf("all three fixture methods should be unbound, got %d:\n%s", got, table.Explain())
	}
}

func TestDigestCoversEveryPartOfTheTable(t *testing.T) {
	base := fixtureBuild(t, fixtureClaim(), fixtureIndex("explainWorkerState"))

	variants := map[string]func(Table) Table{
		"symbols-checked flag": func(tb Table) Table { tb.SymbolsChecked = !tb.SymbolsChecked; return tb },
		"entry wire method": func(tb Table) Table {
			entries := append([]Entry(nil), tb.Entries...)
			entries[0].Wire.MethodName = "Different"
			tb.Entries = entries
			return tb
		},
		"entry handler": func(tb Table) Table {
			entries := append([]Entry(nil), tb.Entries...)
			entries[0].Handler.Name = "different"
			tb.Entries = entries
			return tb
		},
		"entry read property": func(tb Table) Table {
			entries := append([]Entry(nil), tb.Entries...)
			entries[0].ReadProperties = []string{"worker.identity"}
			tb.Entries = entries
			return tb
		},
		"gap detail": func(tb Table) Table {
			gaps := append([]Gap(nil), tb.Gaps...)
			gaps[0].Detail = "reworded"
			tb.Gaps = gaps
			return tb
		},
	}
	for name, mutate := range variants {
		if mutate(base).Digest() == base.Digest() {
			t.Errorf("changing the %s did not change the digest", name)
		}
	}
}

// TestDigestIsNotConfusableByFieldConcatenation guards the length-tagging:
// two different tables whose fields concatenate to the same bytes must not
// hash the same.
func TestDigestIsNotConfusableByFieldConcatenation(t *testing.T) {
	left := Table{Gaps: []Gap{{Kind: GapNoHandler, Capability: "ab", Subject: "c"}}}
	right := Table{Gaps: []Gap{{Kind: GapNoHandler, Capability: "a", Subject: "bc"}}}
	if left.Digest() == right.Digest() {
		t.Error("two distinct gap identities produced the same digest; the parts are not length-tagged")
	}
}

func TestResolveModelUsesTheGeneratedRegistryFootprint(t *testing.T) {
	table := fixtureBuild(t, fixtureClaim(), fixtureIndex("explainWorkerState"))
	if len(table.Entries) != 1 {
		t.Fatalf("fixture claim did not bind:\n%s", table.Explain())
	}
	entry := table.Entries[0]
	if len(entry.Entities) != 2 || len(entry.ReadProperties) != 2 || len(entry.WriteProperties) != 1 {
		t.Fatalf("model footprint = %d entities / %d reads / %d writes, want 2/2/1",
			len(entry.Entities), len(entry.ReadProperties), len(entry.WriteProperties))
	}
	// The footprint is copied from the model binding, not restated: an
	// entity ref carries the version the generated registry published.
	if entry.Entities[0] != "Worker/v1" {
		t.Errorf("entity ref = %q, want the generated Ref() form Worker/v1", entry.Entities[0])
	}
}
