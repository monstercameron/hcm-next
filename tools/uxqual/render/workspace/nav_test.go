package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/journeyclient"
)

func renderNode(t *testing.T, n ui.Node) string {
	t.Helper()
	out, err := ui.RenderToString(n)
	if err != nil {
		t.Fatalf("ui.RenderToString: %v", err)
	}
	return out
}

func TestNavListRouteMarksTheJourneysLinkCurrent(t *testing.T) {
	out := renderNode(t, Nav(journeyclient.Parse("")))
	if !strings.Contains(out, `href="#/journeys"`) {
		t.Errorf("Nav does not link to the journeys list: %s", out)
	}
	if !strings.Contains(out, `aria-current="page"`) {
		t.Errorf("Nav on the bare list route marks nothing current: %s", out)
	}
}

func TestNavListRouteWithWorkerAddsASelectionLinkMarkedCurrent(t *testing.T) {
	route := journeyclient.Parse("#/journeys?worker=worker:NW-40118")
	out := renderNode(t, Nav(route))
	if !strings.Contains(out, "worker:NW-40118") {
		t.Errorf("Nav does not name the selected worker: %s", out)
	}
	if !strings.Contains(out, journeyclient.WorkerHref("worker:NW-40118")) {
		t.Errorf("Nav does not link to journeyclient.WorkerHref for the selection: %s", out)
	}
	// The unfiltered journeys link is no longer the current one once a
	// worker is selected.
	if strings.Count(out, `aria-current="page"`) != 1 {
		t.Errorf("Nav with a worker selected should mark exactly one link current: %s", out)
	}
}

func TestNavDetailRouteNamesTheOpenJourneyAndMarksItCurrent(t *testing.T) {
	route := journeyclient.Parse("#/journeys/int_01JX6Y8B2C7D9EFG")
	out := renderNode(t, Nav(route))
	if !strings.Contains(out, "int_01JX6Y8B2C7D9EFG") {
		t.Errorf("Nav does not name the open journey: %s", out)
	}
	if !strings.Contains(out, journeyclient.DetailHref("int_01JX6Y8B2C7D9EFG")) {
		t.Errorf("Nav does not link to journeyclient.DetailHref for the open journey: %s", out)
	}
	if strings.Count(out, `aria-current="page"`) != 1 {
		t.Errorf("Nav on a detail route should mark exactly one link current: %s", out)
	}
}

func TestNavAlwaysCarriesThePrimaryLabel(t *testing.T) {
	out := renderNode(t, Nav(journeyclient.Route{}))
	if !strings.Contains(out, `id="`+NavElementID+`"`) {
		t.Errorf("Nav id = missing %q: %s", NavElementID, out)
	}
	if !strings.Contains(out, `aria-label="Primary"`) {
		t.Errorf("Nav is missing its aria-label: %s", out)
	}
}
