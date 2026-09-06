package journeyclient

import "testing"

func TestParseRoutes(t *testing.T) {
	cases := []struct {
		name string
		hash string
		want Route
	}{
		{"the list", "#/journeys", Route{Kind: RouteList}},
		{"an empty fragment is the list", "", Route{Kind: RouteList}},
		{"a bare hash is the list", "#", Route{Kind: RouteList}},
		{"a focused proposal", "#/journeys/new?worker=jane-doe", Route{Kind: RouteProposal, WorkerRef: "jane-doe"}},
		{"a focused proposal without a worker", "#/journeys/new", Route{Kind: RouteProposal}},
		{"a detail", "#/journeys/int_01JX6Y8B2C7D9EFG", Route{Kind: RouteDetail, IntentID: "int_01JX6Y8B2C7D9EFG"}},
		{"a fragment written without its hash", "/journeys/int_01", Route{Kind: RouteDetail, IntentID: "int_01"}},
		{"a trailing slash with no id is the list", "#/journeys/", Route{Kind: RouteList}},
		{"anything after the id is ignored", "#/journeys/int_01/timeline", Route{Kind: RouteDetail, IntentID: "int_01"}},
		{"an unrecognised fragment is the list", "#/receipts", Route{Kind: RouteList}},
		{"a hand-edited fragment is the list", "#nonsense", Route{Kind: RouteList}},
		{"surrounding whitespace", "  #/journeys/int_02  ", Route{Kind: RouteDetail, IntentID: "int_02"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Parse(c.hash)
			if got != c.want {
				t.Errorf("Parse(%q) = %+v, want %+v", c.hash, got, c.want)
			}
		})
	}
}

// TestHrefParseRoundTrip is the property that matters: every address the
// client writes into the browser is one it can read back.
func TestHrefParseRoundTrip(t *testing.T) {
	routes := []Route{
		{Kind: RouteList},
		{Kind: RouteProposal, WorkerRef: "jane-doe"},
		{Kind: RouteProposal},
		{Kind: RouteDetail, IntentID: "int_01JX6Y8B2C7D9EFG"},
		{Kind: RouteDetail, IntentID: "int_02"},
		{Kind: RouteProposal, WorkerRef: "worker/a+b&c%25"},
		{Kind: RouteList, WorkerRef: "worker/a+b&c%25"},
	}
	for _, r := range routes {
		href := Href(r)
		back := Parse(href)
		if back != r {
			t.Errorf("Parse(Href(%+v)) = %+v, want %+v (href %q)", r, back, r, href)
		}
	}
}

func TestHrefOfADetailWithNoIntentIsTheList(t *testing.T) {
	if got, want := Href(Route{Kind: RouteDetail}), ListHref(); got != want {
		t.Errorf("Href of an id-less detail = %q, want the list %q", got, want)
	}
}

func TestHrefsAreFragments(t *testing.T) {
	if got, want := ListHref(), "#/journeys"; got != want {
		t.Errorf("ListHref() = %q, want %q", got, want)
	}
	if got, want := DetailHref("int_01"), "#/journeys/int_01"; got != want {
		t.Errorf("DetailHref = %q, want %q", got, want)
	}
	if got, want := ProposalHref("jane-doe"), "#/journeys/new?worker=jane-doe"; got != want {
		t.Errorf("ProposalHref = %q, want %q", got, want)
	}
}

func TestProposalRouteDoesNotMasqueradeAsJourneyDetail(t *testing.T) {
	got := Parse("#/journeys/new?worker=worker:NW-40118")
	if got.Kind != RouteProposal || got.IntentID != "" || got.WorkerRef != "worker:NW-40118" {
		t.Fatalf("proposal route = %+v", got)
	}
}

// TestParseSelectsAnEmployee covers the one query parameter this client
// reads. It is the address the People table's rows link to, so it has to
// survive a reload, a copied link and a client that supplied no callbacks.
func TestParseSelectsAnEmployee(t *testing.T) {
	cases := []struct {
		name string
		hash string
		want Route
	}{
		{"a selection", "#/journeys?worker=worker:NW-40118",
			Route{Kind: RouteList, WorkerRef: "worker:NW-40118"}},
		{"a corpus key", "#/journeys?worker=jane-doe",
			Route{Kind: RouteList, WorkerRef: "jane-doe"}},
		{"no selection", "#/journeys?worker=", Route{Kind: RouteList}},
		{"a parameter this client does not read", "#/journeys?sort=name", Route{Kind: RouteList}},
		{"among others", "#/journeys?sort=name&worker=jane-doe",
			Route{Kind: RouteList, WorkerRef: "jane-doe"}},
		{"written without its hash", "/journeys?worker=jane-doe",
			Route{Kind: RouteList, WorkerRef: "jane-doe"}},
		// A detail address carries no selection: there is no People table on
		// that page for one to mean anything on.
		{"a detail ignores it", "#/journeys/int_01?worker=jane-doe",
			Route{Kind: RouteDetail, IntentID: "int_01"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Parse(c.hash); got != c.want {
				t.Errorf("Parse(%q) = %+v, want %+v", c.hash, got, c.want)
			}
		})
	}
}

func TestSelectionHrefsRoundTrip(t *testing.T) {
	routes := []Route{
		{Kind: RouteList, WorkerRef: "jane-doe"},
		{Kind: RouteList, WorkerRef: "worker:NW-40118"},
		{Kind: RouteList},
	}
	for _, r := range routes {
		href := Href(r)
		if back := Parse(href); back != r {
			t.Errorf("Parse(Href(%+v)) = %+v (href %q)", r, back, href)
		}
	}
	if got, want := WorkerHref("jane-doe"), "#/journeys?worker=jane-doe"; got != want {
		t.Errorf("WorkerHref = %q, want %q", got, want)
	}
	// The address the renderer writes for a client that bound no selection
	// callback is the one this client reads back.
	if got := Parse(WorkerHref("jane-doe")).WorkerRef; got != "jane-doe" {
		t.Errorf("the renderer's own selection address parsed to %q", got)
	}
	if got, want := WorkerHref(""), ListHref(); got != want {
		t.Errorf("WorkerHref(\"\") = %q, want the plain list %q", got, want)
	}
}
