package pagedef

import (
	"strings"
	"testing"
)

// TestPromotionPagesProjectOntoJourneyService is WEB-002's projection proof:
// the Promotion journey's real list and detail pages
// (tools/uxqual/render/journey.Page's ListView and DetailView, as
// tools/uxqual/journeyclient projects them) can be expressed as
// PageDefinitions, both pass Validate() outright, and every one of their
// data bindings and actions resolves to a method
// hcmnext.journey.v1.JourneyService actually registers -- not merely to
// "some known RPC" of any of the four services this package's registry
// covers.
func TestPromotionPagesProjectOntoJourneyService(t *testing.T) {
	pages := map[string]PageDefinition{
		"list":   PromotionListPageDefinition(),
		"detail": PromotionDetailPageDefinition(),
	}

	for name, pd := range pages {
		t.Run(name, func(t *testing.T) {
			if v := pd.Validate(); len(v) != 0 {
				t.Fatalf("PromotionPageDefinition(%s).Validate() = %v, want no violations", name, v)
			}

			var refs []string
			for _, r := range pd.Regions {
				for _, b := range r.Bindings {
					refs = append(refs, b.RPC)
				}
				for _, a := range r.Actions {
					refs = append(refs, a.RPC)
				}
			}
			if len(refs) == 0 {
				t.Fatalf("PromotionPageDefinition(%s) names no RPC refs at all", name)
			}

			prefix := JourneyServiceName + "/"
			for _, ref := range refs {
				if !strings.HasPrefix(ref, prefix) {
					t.Errorf("PromotionPageDefinition(%s) binds/acts on %q, which is not a %s method", name, ref, JourneyServiceName)
				}
			}
		})
	}

	t.Run("the two pages bind and act on the JourneyService's actual read/write surface", func(t *testing.T) {
		list := PromotionListPageDefinition()
		detail := PromotionDetailPageDefinition()

		want := map[string]bool{
			RPCRef(JourneyServiceName, "ListWorkers"):    false,
			RPCRef(JourneyServiceName, "CreateWorker"):   false,
			RPCRef(JourneyServiceName, "ListJourneys"):   false,
			RPCRef(JourneyServiceName, "ProposeJourney"): false,
			RPCRef(JourneyServiceName, "InspectJourney"): false,
			RPCRef(JourneyServiceName, "ExecuteJourney"): false,
			RPCRef(JourneyServiceName, "DecideJourney"):  false,
		}
		for _, pd := range []PageDefinition{list, detail} {
			for _, r := range pd.Regions {
				for _, b := range r.Bindings {
					if _, ok := want[b.RPC]; ok {
						want[b.RPC] = true
					}
				}
				for _, a := range r.Actions {
					if _, ok := want[a.RPC]; ok {
						want[a.RPC] = true
					}
				}
			}
		}
		for rpc, found := range want {
			if !found {
				t.Errorf("neither Promotion page definition names %q", rpc)
			}
		}
	})
}
