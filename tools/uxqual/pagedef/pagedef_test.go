package pagedef

import "testing"

// validMinimalPage is a minimal but fully valid PageDefinition, used
// throughout this package's tests as the "known good" baseline every RED
// case is a single mutation away from.
func validMinimalPage() PageDefinition {
	return PageDefinition{
		PageID:       "test.page",
		Version:      1,
		FloorplanRef: "floorplan.object.v1",
		Regions: []Region{
			{
				ID:      "identity",
				Kind:    RegionPageIdentity,
				Heading: &Heading{Level: 1, Text: "Test page"},
			},
			{
				ID:      "main",
				Kind:    RegionPrimary,
				Heading: &Heading{Level: 2, Text: "Main"},
				Widgets: []WidgetSlot{{ID: "table", WidgetRef: "widget.table.v1"}},
				Bindings: []DataBinding{
					{ID: "journeys", RPC: RPCRef(JourneyServiceName, "ListJourneys")},
				},
				Actions: []ActionRef{
					{ID: "propose", RPC: RPCRef(JourneyServiceName, "ProposeJourney"), RequiredRole: "manager"},
				},
			},
		},
		Accessibility: Accessibility{
			Landmarks:  []string{"banner", "main"},
			LiveRegion: LiveRegionPolite,
		},
		BrandTokens: []string{"brand.color.primary"},
	}
}

// TestTodo_WEB_002 is the PRIMARY test for WEB-002 (planning/todos.md
// section 66): it proves the versioned PageDefinition contract accepts a
// well-formed page (GREEN) and refuses every way the RED case describes --
// missing, stale (unversioned), unauthorized (no required role), non-closed
// vocabulary, inconsistent with the controlling contract (an RPC the real
// generated services do not register), or carrying free HTML/script -- and
// that its Digest is deterministic and versioned.
func TestTodo_WEB_002(t *testing.T) {
	t.Run("GREEN: a well-formed page is accepted", func(t *testing.T) {
		pd := validMinimalPage()
		if v := pd.Validate(); len(v) != 0 {
			t.Fatalf("Validate() = %v, want no violations", v)
		}
	})

	t.Run("RED: missing page id", func(t *testing.T) {
		pd := validMinimalPage()
		pd.PageID = ""
		mustViolate(t, pd, "page_id")
	})

	t.Run("RED: stale/unversioned page", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Version = 0
		mustViolate(t, pd, "version")
	})

	t.Run("RED: no regions at all", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions = nil
		mustViolate(t, pd, "regions")
	})

	t.Run("RED: no primary region", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions[1].Kind = RegionSupporting
		mustViolate(t, pd, "regions")
	})

	t.Run("RED: region kind outside the closed vocabulary", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions[0].Kind = RegionKind("sidebar")
		mustViolate(t, pd, "regions[0].kind")
	})

	t.Run("RED: binding names an RPC no registered service has", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions[1].Bindings[0].RPC = "hcmnext.evil.v1.EvilService/DropTable"
		mustViolate(t, pd, "regions[1].bindings[0].rpc")
	})

	t.Run("RED: action names an RPC no registered service has", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions[1].Actions[0].RPC = "hcmnext.evil.v1.EvilService/DropTable"
		mustViolate(t, pd, "regions[1].actions[0].rpc")
	})

	t.Run("RED: unauthorized action (no required role)", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions[1].Actions[0].RequiredRole = ""
		mustViolate(t, pd, "regions[1].actions[0].required_role")
	})

	t.Run("RED: heading order skips a level", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions[1].Heading.Level = 3 // 1 -> 3, skipping 2
		mustViolate(t, pd, "regions[].heading.level")
	})

	t.Run("RED: inaccessible page (no landmarks, no live-region declaration)", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Accessibility = Accessibility{}
		v := pd.Validate()
		mustViolate(t, pd, "accessibility.landmarks")
		found := false
		for _, x := range v {
			if x.Path == "accessibility.live_region" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Validate() = %v, want a violation on accessibility.live_region", v)
		}
	})

	t.Run("RED: free HTML/script", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions[0].Heading.Text = "<script>alert(1)</script>"
		mustViolate(t, pd, "regions[0].heading.text")
	})

	t.Run("Digest is deterministic", func(t *testing.T) {
		pd := validMinimalPage()
		d1 := pd.Digest()
		d2 := pd.Digest()
		if d1 != d2 {
			t.Fatalf("Digest() is nondeterministic: %q vs %q", d1, d2)
		}
		if d1 == "" {
			t.Fatalf("Digest() returned empty string")
		}
	})

	t.Run("Digest changes with version", func(t *testing.T) {
		a := validMinimalPage()
		b := validMinimalPage()
		b.Version = a.Version + 1
		if a.Digest() == b.Digest() {
			t.Fatalf("Digest() did not change when Version changed")
		}
	})

	t.Run("Digest changes with content", func(t *testing.T) {
		a := validMinimalPage()
		b := validMinimalPage()
		b.Regions[0].Heading.Text = "Different heading"
		if a.Digest() == b.Digest() {
			t.Fatalf("Digest() did not change when content changed")
		}
	})
}

// mustViolate fails the test unless pd.Validate() reports a violation whose
// Path is exactly wantPath.
func mustViolate(t *testing.T, pd PageDefinition, wantPath string) {
	t.Helper()
	v := pd.Validate()
	for _, x := range v {
		if x.Path == wantPath {
			return
		}
	}
	t.Fatalf("Validate() = %v, want a violation on %q", v, wantPath)
}

// TestRegionKinds proves RegionKinds() and regionKindDocs agree exactly:
// every kind RegionKinds() names has documentation, and regionKindDocs
// names no kind RegionKinds() omits.
func TestRegionKinds(t *testing.T) {
	kinds := RegionKinds()
	if len(kinds) != len(regionKindDocs) {
		t.Fatalf("RegionKinds() has %d entries, regionKindDocs has %d", len(kinds), len(regionKindDocs))
	}
	seen := map[RegionKind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("RegionKinds() lists %q more than once", k)
		}
		seen[k] = true
		if RegionKindDoc(k) == "" {
			t.Fatalf("region kind %q has no documentation", k)
		}
	}
}

// TestKnownRPCs proves the RPC registry is non-empty and contains at least
// one method from each of the four named services, so a typo in
// rpcregistry.go that silently emptied one service's list would fail here
// rather than only showing up as a mysteriously-refused binding.
func TestKnownRPCs(t *testing.T) {
	rpcs := KnownRPCs()
	if len(rpcs) == 0 {
		t.Fatalf("KnownRPCs() is empty")
	}
	for _, want := range []string{
		RPCRef(JourneyServiceName, "ListJourneys"),
		RPCRef(IntentServiceName, "CreateIntent"),
		RPCRef(RegistryServiceName, "ListCapabilities"),
		RPCRef(AdminServiceName, "GetReleaseManifest"),
	} {
		if !rpcs[want] {
			t.Fatalf("KnownRPCs() missing %q", want)
		}
	}
	if rpcs["hcmnext.evil.v1.EvilService/DropTable"] {
		t.Fatalf("KnownRPCs() must not contain an RPC no service registers")
	}
}
