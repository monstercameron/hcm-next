package pagedef

import "testing"

// TestTodo_WEB_002_Conformance proves every region kind in the closed
// vocabulary is documented (regionKindDocs has a non-empty entry for each),
// that the vocabulary is exactly the eight-step page anatomy the frontend
// plan defines (no more, no fewer), and that Validate accepts every
// documented kind in isolation while refusing one outside it.
func TestTodo_WEB_002_Conformance(t *testing.T) {
	want := []RegionKind{
		RegionShell,
		RegionPageIdentity,
		RegionAuthorityContext,
		RegionLocalNavigation,
		RegionPrimary,
		RegionSupporting,
		RegionUtility,
		RegionCompletion,
	}

	t.Run("every region kind is documented", func(t *testing.T) {
		if len(regionKindDocs) != len(want) {
			t.Fatalf("regionKindDocs has %d entries, want %d", len(regionKindDocs), len(want))
		}
		for _, k := range want {
			doc, ok := regionKindDocs[k]
			if !ok {
				t.Errorf("region kind %q has no documentation entry", k)
				continue
			}
			if len(doc) < 10 {
				t.Errorf("region kind %q has a suspiciously short documentation entry: %q", k, doc)
			}
		}
	})

	t.Run("RegionKinds() names exactly the documented vocabulary, no more and no fewer", func(t *testing.T) {
		got := RegionKinds()
		if len(got) != len(want) {
			t.Fatalf("RegionKinds() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("RegionKinds()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("every documented kind is individually accepted", func(t *testing.T) {
		for _, k := range want {
			pd := validMinimalPage()
			// Keep exactly one RegionPrimary region so this only ever tests
			// the kind-vocabulary rule, not the separate "needs a primary
			// region" rule.
			if k != RegionPrimary {
				pd.Regions[1].Kind = RegionPrimary
				pd.Regions[0].Kind = k
				if pd.Regions[0].Heading != nil && k != RegionPageIdentity {
					// Headings are only asserted to belong to any particular
					// kind by convention, not by rule; leave it in place.
					_ = pd.Regions[0].Heading
				}
			}
			if v := pd.Validate(); hasViolationOn(v, "regions[0].kind") {
				t.Errorf("documented region kind %q was refused: %v", k, v)
			}
		}
	})

	t.Run("an undocumented kind is refused", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions[0].Kind = RegionKind("not_a_real_kind")
		if !hasViolationOn(pd.Validate(), "regions[0].kind") {
			t.Fatalf("Validate() did not refuse an undocumented region kind")
		}
	})
}

func hasViolationOn(v []Violation, path string) bool {
	for _, x := range v {
		if x.Path == path {
			return true
		}
	}
	return false
}
