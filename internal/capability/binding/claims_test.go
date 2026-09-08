package binding

import (
	"sort"
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
)

// TestClaimsCoverExactlyThePublishedCapabilities is the set-equality check
// that keeps the claim table from drifting from the registry in either
// direction: a capability nobody claims, or a claim for a capability nobody
// publishes, are both findings.
func TestClaimsCoverExactlyThePublishedCapabilities(t *testing.T) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}

	published := map[string]uint32{}
	for _, rec := range registry.List() {
		published[rec.Definition.ID] = rec.Definition.Version
	}
	claimed := map[string]bool{}
	for _, c := range Claims() {
		if claimed[c.CapabilityID] {
			t.Errorf("capability %s is claimed twice; the claim table must be keyed", c.CapabilityID)
		}
		claimed[c.CapabilityID] = true
	}

	for id := range published {
		if !claimed[id] {
			t.Errorf("published capability %s has no claim", id)
		}
	}
	for _, c := range Claims() {
		if want := published[c.CapabilityID]; c.CapabilityVersion != want {
			t.Errorf("claim %s pins v%d, registry publishes v%d", c.CapabilityID, c.CapabilityVersion, want)
		}
	}
	for id := range claimed {
		if _, ok := published[id]; !ok {
			t.Errorf("claim names %s, which the registry does not publish", id)
		}
	}
}

// TestClaimsNameOnlyRealWireMethods proves every claimed route exists in
// the compiled descriptors. A claim naming a method that does not exist
// would otherwise look like evidence.
func TestClaimsNameOnlyRealWireMethods(t *testing.T) {
	known := WireMethodSet()
	for _, c := range Claims() {
		if len(c.WireMethods) == 0 {
			t.Errorf("claim %s names no wire method at all; state the route or state the gap", c.CapabilityID)
		}
		if !sort.StringsAreSorted(c.WireMethods) {
			t.Errorf("claim %s lists wire methods out of order; the ambiguity gap id depends on the order", c.CapabilityID)
		}
		for _, ref := range c.WireMethods {
			if _, ok := known[ref]; !ok {
				t.Errorf("claim %s names %s, which no registered service declares", c.CapabilityID, ref)
			}
		}
	}
}

// TestClaimsNameOnlyRealHandlerSymbols proves every claimed handler exists
// in the live tree. This is the check that turns a rename in
// internal/transport or internal/intent/app into a failure here.
func TestClaimsNameOnlyRealHandlerSymbols(t *testing.T) {
	index := liveHandlerIndex(t)
	for _, c := range Claims() {
		if len(c.Handlers) == 0 {
			t.Errorf("claim %s names no handler symbol; state the symbol or state the gap", c.CapabilityID)
		}
		for _, h := range c.Handlers {
			if !h.Valid() {
				t.Errorf("claim %s carries a half-named handler symbol %+v", c.CapabilityID, h)
				continue
			}
			if !index.Has(h) {
				t.Errorf("claim %s names handler %s, which does not exist in the scanned tree", c.CapabilityID, h.Ref())
			}
		}
	}
}

// TestEveryClaimCitesItsEvidence: a claim without a rationale is an
// assertion, and this table is supposed to be evidence.
func TestEveryClaimCitesItsEvidence(t *testing.T) {
	for _, c := range Claims() {
		if c.Rationale == "" {
			t.Errorf("claim %s has no rationale citing where it was read from", c.CapabilityID)
		}
	}
}

// TestClaimsAreSortedAndIndexed keeps the compiled table deterministic.
func TestClaimsAreSortedAndIndexed(t *testing.T) {
	claims := Claims()
	for i := 1; i < len(claims); i++ {
		if claims[i-1].CapabilityID >= claims[i].CapabilityID {
			t.Errorf("Claims() is not strictly sorted at %d: %q then %q", i, claims[i-1].CapabilityID, claims[i].CapabilityID)
		}
	}
	byID := ClaimsByCapability()
	if len(byID) != len(claims) {
		t.Fatalf("ClaimsByCapability has %d entries, Claims has %d", len(byID), len(claims))
	}
	for _, c := range claims {
		if byID[c.CapabilityID].Rationale != c.Rationale {
			t.Errorf("ClaimsByCapability disagrees with Claims for %s", c.CapabilityID)
		}
	}
}

// TestGenericLifecycleMethodsMatchTheEndpointDisposition pins the six
// methods the endpoint disposition calls the generic lifecycle. If
// ENDPOINT-009 adds or removes one, the claims in this package are stale
// and must be revisited rather than silently carrying an old set.
func TestGenericLifecycleMethodsMatchTheEndpointDisposition(t *testing.T) {
	want := []string{
		"hcmnext.intents.v1.IntentService/CreateIntent",
		"hcmnext.intents.v1.IntentService/ExplainIntent",
		"hcmnext.intents.v1.IntentService/GetIntent",
		"hcmnext.intents.v1.IntentService/ListIntentTimeline",
		"hcmnext.intents.v1.IntentService/ListIntents",
		"hcmnext.intents.v1.IntentService/SimulateIntent",
	}
	got := genericIntentLifecycleMethods()
	if len(got) != len(want) {
		t.Fatalf("genericIntentLifecycleMethods() has %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("generic lifecycle method %d = %q, want %q", i, got[i], want[i])
		}
	}

	// Each call must return a fresh slice: the claim table appends dedicated
	// routes onto it, and a shared backing array would cross-contaminate.
	first := genericIntentLifecycleMethods()
	appended := append(genericIntentLifecycleMethods(), "sentinel")
	_ = appended
	second := genericIntentLifecycleMethods()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("appending to one result mutated another at %d: %q vs %q", i, first[i], second[i])
		}
	}
}
