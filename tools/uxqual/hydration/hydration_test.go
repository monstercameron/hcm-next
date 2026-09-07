package hydration

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/floorplan"
	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
	page "github.com/monstercameron/hcm-next/tools/uxqual/render/page"
	"github.com/monstercameron/hcm-next/tools/uxqual/ssrshell"
)

func testPage() pagedef.PageDefinition {
	return pagedef.PageDefinition{
		PageID: "hydration.test", Version: 1, FloorplanRef: "floorplan.hydration.v1",
		Regions: []pagedef.Region{
			{ID: "shell", Kind: pagedef.RegionShell},
			{ID: "identity", Kind: pagedef.RegionPageIdentity, Heading: &pagedef.Heading{Level: 1, Text: "Hydration test"}},
			{ID: "primary", Kind: pagedef.RegionPrimary, Heading: &pagedef.Heading{Level: 2, Text: "Primary"}, Widgets: []pagedef.WidgetSlot{{ID: "slot-a", WidgetRef: "widget.a.v1"}, {ID: "slot-b", WidgetRef: "widget.b.v1"}}},
			{ID: "completion", Kind: pagedef.RegionCompletion, Heading: &pagedef.Heading{Level: 2, Text: "Completion"}},
		},
		Accessibility: pagedef.Accessibility{Landmarks: []string{"banner", "main", "contentinfo"}, LiveRegion: pagedef.LiveRegionPolite},
		BrandTokens:   []string{"brand.color.primary"},
	}
}

func testFloorplan() floorplan.Floorplan {
	flow := floorplan.LayoutConstraint{Mode: floorplan.LayoutFlow, MinColumns: 1, MaxColumns: 1, Gap: "space.2"}
	regions := []floorplan.Region{
		{Name: "shell", Kind: pagedef.RegionShell, Layout: flow}, {Name: "identity", Kind: pagedef.RegionPageIdentity, Layout: flow},
		{Name: "primary", Kind: pagedef.RegionPrimary, Layout: flow}, {Name: "completion", Kind: pagedef.RegionCompletion, Layout: flow},
	}
	return floorplan.Floorplan{ID: "floorplan.hydration", Version: 1, Breakpoints: floorplan.Breakpoints(), Regions: regions}
}

func renderedPair(t *testing.T) (string, string, Contract) {
	t.Helper()
	pd := testPage()
	c, err := FromPage(pd, []AuthorizationDisposition{{ID: "primary.view", Disposition: DispositionAllow}}, Preservation{
		FocusID: "amount", FormValues: map[string]string{"amount": "1000"}, FieldErrors: map[string]string{"amount": "valid"}, IdempotencyKeys: map[string]string{"submit": "idem-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ssr, err := ssrshell.Render(pd)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := floorplan.NewRegistry(testFloorplan())
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Resolve(pd)
	if err != nil {
		t.Fatal(err)
	}
	widgets := page.NewRegistry()
	if err := widgets.Register("widget.a.v1", func(WidgetContext page.WidgetContext) ui.Node {
		return html.P(html.Props{}, ui.Text("SSR differs here"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := widgets.Register("widget.b.v1", func(WidgetContext page.WidgetContext) ui.Node { return html.P(html.Props{}, ui.Text("mounted widget")) }); err != nil {
		t.Fatal(err)
	}
	node, err := page.Render(res, widgets)
	if err != nil {
		t.Fatal(err)
	}
	gwc, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	// State evidence is attached to real controls inside a governed widget
	// slot. No detached data-* value/error marker can satisfy the contract.
	evidence := `<form data-authz-id="primary.view" data-authz-disposition="allow"><label for="amount">Amount</label><input id="amount" name="amount" value="1000" data-hydration-focus="amount" aria-invalid="true" aria-describedby="amount-error"><p id="amount-error" role="alert">valid</p><input id="submit" name="idempotency_key" type="hidden" value="idem-1"></form>`
	ssr.HTML = injectIntoSlot(t, ssr.HTML, "slot-a", evidence)
	gwc = injectIntoSlot(t, gwc, "slot-a", evidence)
	return ssr.HTML, gwc, c
}

func injectIntoSlot(t *testing.T, document, slotID, fragment string) string {
	t.Helper()
	marker := `data-slot-id="` + slotID + `"`
	attributeAt := strings.Index(document, marker)
	if attributeAt < 0 {
		t.Fatalf("slot %q not found", slotID)
	}
	openEnd := strings.Index(document[attributeAt:], ">")
	if openEnd < 0 {
		t.Fatalf("slot %q has no opening-tag terminator", slotID)
	}
	openEnd += attributeAt + 1
	return document[:openEnd] + fragment + document[openEnd:]
}

func TestTodo_WEB_028(t *testing.T) {
	ssr, gwc, c := renderedPair(t)
	report, err := Compare(ssr, gwc, c)
	if err != nil {
		t.Fatalf("semantic hydration parity: %v", err)
	}
	if !report.Equal || report.Digest == "" {
		t.Fatalf("report=%+v, want equal semantic evidence", report)
	}
}

func TestTodo_WEB_028_Golden(t *testing.T) {
	ssr, gwc, c := renderedPair(t)
	one, err := Compare(ssr, gwc, c)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Compare(ssr, gwc, c)
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest != two.Digest {
		t.Fatalf("semantic digest is nondeterministic: %q vs %q", one.Digest, two.Digest)
	}
	if one.Digest != "sha256:750c437c3de591caa0d9e249294c9c4c61b5f2dae413a44022a7c04650cd6273" {
		t.Fatalf("semantic digest = %q; update the pinned WEB-028 golden", one.Digest)
	}
}

func TestTodo_WEB_028_Browser(t *testing.T) {
	ssr, gwc, c := renderedPair(t)
	report, err := Compare(ssr, gwc, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.SSR.Regions) != len(c.Regions) || report.SSR.LiveRegion.Role != "status" {
		t.Fatalf("browser semantic projection=%+v", report.SSR)
	}
}

func TestTodo_WEB_028_Conformance(t *testing.T) {
	ssr, gwc, c := renderedPair(t)
	if _, err := Compare(ssr, gwc, c); err != nil {
		t.Fatal(err)
	}
	// Safe widget interiors are intentionally not part of the projection.
	if _, err := Compare(ssr, strings.Replace(gwc, "mounted widget", `<strong>different widget interior</strong>`, 1), c); err != nil {
		t.Fatalf("widget interior should be unconstrained: %v", err)
	}
	// Ignoring ordinary widget semantics must not make the gate an executable
	// content bypass.
	if _, err := Compare(ssr, strings.Replace(gwc, "mounted widget", `<script>alert(1)</script>`, 1), c); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("active widget content error=%v, want ErrUntrusted", err)
	}
	if _, err := Compare(ssr, strings.Replace(gwc, `data-slot-id="slot-a"`, `data-slot-id="slot-b"`, 1), c); !errors.Is(err, ErrMismatch) && !errors.Is(err, ErrDuplicate) {
		t.Fatalf("slot identity drift error=%v, want typed mismatch/duplicate", err)
	}
}

func TestTodo_WEB_028_Security(t *testing.T) {
	ssr, gwc, c := renderedPair(t)
	secret := `salary=999 bearer=secret <script>alert(1)</script>`
	_, err := Compare(strings.Replace(ssr, `value="1000"`, `value="`+secret+`"`, 1), gwc, c)
	if !errors.Is(err, ErrMismatch) && !errors.Is(err, ErrUntrusted) {
		t.Fatalf("malicious state error=%v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("sanitized error leaked untrusted value: %v", err)
	}
	if _, err := FromPage(testPage(), []AuthorizationDisposition{{ID: "a", Disposition: "grant-all"}}, Preservation{}); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("untrusted disposition error=%v", err)
	}

	for name, mutation := range map[string]func(string) string{
		"script in ignored widget interior": func(document string) string {
			return strings.Replace(document, "mounted widget", `<script>alert(1)</script>`, 1)
		},
		"event handler in ignored widget interior": func(document string) string {
			return strings.Replace(document, "mounted widget", `<button onclick="alert(1)">open</button>`, 1)
		},
		"javascript URL in ignored widget interior": func(document string) string {
			return strings.Replace(document, "mounted widget", `<a href="java&#x09;script:alert(1)">open</a>`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Compare(ssr, mutation(gwc), c); !errors.Is(err, ErrUntrusted) {
				t.Fatalf("error=%v, want ErrUntrusted", err)
			}
		})
	}
}

func TestHydrationEvidenceIsScopedAndBoundToRealDOM(t *testing.T) {
	ssr, gwc, c := renderedPair(t)

	t.Run("authorization outside governed root cannot satisfy contract", func(t *testing.T) {
		withoutInside := strings.Replace(gwc, ` data-authz-id="primary.view" data-authz-disposition="allow"`, "", 1)
		outside := withoutInside + `<div data-authz-id="primary.view" data-authz-disposition="allow"></div>`
		if _, err := Compare(ssr, outside, c); !errors.Is(err, ErrMissing) {
			t.Fatalf("error=%v, want ErrMissing", err)
		}
	})

	t.Run("detached self asserted form value is refused", func(t *testing.T) {
		mutation := injectIntoSlot(t, gwc, "slot-b", `<div data-hydration-field="detached" data-hydration-value="1000"></div>`)
		if _, err := Compare(ssr, mutation, c); !errors.Is(err, ErrUntrusted) {
			t.Fatalf("error=%v, want ErrUntrusted", err)
		}
	})

	t.Run("actual control value drift is detected", func(t *testing.T) {
		mutation := strings.Replace(gwc, `value="1000"`, `value="1001"`, 1)
		if _, err := Compare(ssr, mutation, c); !errors.Is(err, ErrMismatch) {
			t.Fatalf("error=%v, want ErrMismatch", err)
		}
	})

	t.Run("focus marker must identify its focusable node", func(t *testing.T) {
		mutation := strings.Replace(gwc, `data-hydration-focus="amount"`, `data-hydration-focus="detached"`, 1)
		if _, err := Compare(ssr, mutation, c); !errors.Is(err, ErrMismatch) {
			t.Fatalf("error=%v, want ErrMismatch", err)
		}
	})

	t.Run("error must retain accessible control relationship", func(t *testing.T) {
		mutation := strings.Replace(gwc, ` aria-describedby="amount-error"`, "", 1)
		if _, err := Compare(ssr, mutation, c); !errors.Is(err, ErrMismatch) {
			t.Fatalf("error=%v, want ErrMismatch", err)
		}
	})

	t.Run("idempotency must be a hidden input", func(t *testing.T) {
		mutation := strings.Replace(gwc, `name="idempotency_key" type="hidden"`, `name="idempotency_key" type="text"`, 1)
		if _, err := Compare(ssr, mutation, c); !errors.Is(err, ErrUntrusted) {
			t.Fatalf("error=%v, want ErrUntrusted", err)
		}
	})

	t.Run("unexpected stateful control is semantic drift", func(t *testing.T) {
		mutation := injectIntoSlot(t, gwc, "slot-b", `<input id="unexpected" value="state">`)
		if _, err := Compare(ssr, mutation, c); !errors.Is(err, ErrMismatch) {
			t.Fatalf("error=%v, want ErrMismatch", err)
		}
	})

	t.Run("localized punctuation and angle text are real values", func(t *testing.T) {
		value := `١٬٠٠٠ <مراجعة>`
		c.Preservation.FormValues["amount"] = value
		encoded := `value="١٬٠٠٠ &lt;مراجعة&gt;"`
		localizedSSR := strings.Replace(ssr, `value="1000"`, encoded, 1)
		localizedGWC := strings.Replace(gwc, `value="1000"`, encoded, 1)
		if _, err := Compare(localizedSSR, localizedGWC, c); err != nil {
			t.Fatalf("localized value parity: %v", err)
		}
	})
}

func TestHydrationSemanticSkeletonIsExact(t *testing.T) {
	ssr, gwc, c := renderedPair(t)
	tests := []struct {
		name     string
		mutate   func(string) string
		wantKind error
	}{
		{
			name: "accessible region name",
			mutate: func(document string) string {
				return strings.Replace(document, `aria-label="Primary: primary"`, `aria-label="Other"`, 1)
			},
			wantKind: ErrMismatch,
		},
		{
			name: "heading level",
			mutate: func(document string) string {
				return strings.Replace(document, `<h2 id="heading-primary">`, `<h3 id="heading-primary">`, 1)
			},
			wantKind: ErrMismatch,
		},
		{
			name: "live region atomicity",
			mutate: func(document string) string {
				return strings.Replace(document, `aria-atomic="true"`, `aria-atomic="false"`, 1)
			},
			wantKind: ErrMismatch,
		},
		{
			name: "duplicate identity JSON key",
			mutate: func(document string) string {
				return strings.Replace(document, `{"page_id":"hydration.test",`, `{"page_id":"hydration.test","page_id":"hydration.test",`, 1)
			},
			wantKind: ErrDuplicate,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Compare(tc.mutate(ssr), gwc, c); !errors.Is(err, tc.wantKind) {
				t.Fatalf("error=%v, want %v", err, tc.wantKind)
			}
		})
	}

	t.Run("duplicate HTML attribute", func(t *testing.T) {
		if err := preflightTokens(`<div a="1" a="2"></div>`); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("token preflight error=%v, want ErrDuplicate", err)
		}
		mutation := strings.Replace(gwc, `data-page-version="1"`, `data-page-version="1" data-page-version="1"`, 1)
		if mutation == gwc {
			t.Fatalf("fixture does not contain expected page-version attribute: %s", gwc)
		}
		if _, err := Compare(ssr, mutation, c); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("error=%v, want ErrDuplicate", err)
		}
	})
}

func TestTodo_WEB_028_Integration(t *testing.T) {
	for _, pd := range []pagedef.PageDefinition{pagedef.PromotionListPageDefinition(), pagedef.PromotionDetailPageDefinition()} {
		c, err := FromPage(pd, nil, Preservation{})
		if err != nil {
			t.Fatal(err)
		}
		ssr, err := ssrshell.Render(pd)
		if err != nil {
			t.Fatal(err)
		}
		fp, err := floorplan.PromotionRegistry().Resolve(pd)
		if err != nil {
			t.Fatal(err)
		}
		node, err := page.Render(fp, page.PromotionWidgetRegistry())
		if err != nil {
			t.Fatal(err)
		}
		gwc, err := ui.RenderToString(node)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compare(ssr.HTML, gwc, c); err != nil {
			t.Fatalf("%s integration: %v", pd.PageID, err)
		}
	}
}

func TestTodo_WEB_028_Fault(t *testing.T) {
	ssr, gwc, c := renderedPair(t)
	if _, err := Compare(strings.Replace(ssr, `id="page-definition"`, `id="other"`, 1), gwc, c); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing island error=%v", err)
	}
	if _, err := Compare(ssr, strings.Replace(gwc, `data-page-digest="`, `data-page-digest="sha256:`, 1), c); err == nil {
		t.Fatal("mismatched digest should refuse")
	}
	if _, err := Compare(ssr, gwc+`<div id="page-hydration.test"></div>`, c); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate page root error=%v", err)
	}
}

func BenchmarkHydrationParity(b *testing.B) {
	pd := testPage()
	c, err := FromPage(pd, nil, Preservation{})
	if err != nil {
		b.Fatal(err)
	}
	ssr, err := ssrshell.Render(pd)
	if err != nil {
		b.Fatal(err)
	}
	reg, err := floorplan.NewRegistry(testFloorplan())
	if err != nil {
		b.Fatal(err)
	}
	res, err := reg.Resolve(pd)
	if err != nil {
		b.Fatal(err)
	}
	widgets := page.NewRegistry()
	_ = widgets.Register("widget.a.v1", func(WidgetContext page.WidgetContext) ui.Node { return html.P(html.Props{}, ui.Text("a")) })
	_ = widgets.Register("widget.b.v1", func(WidgetContext page.WidgetContext) ui.Node { return html.P(html.Props{}, ui.Text("b")) })
	node, err := page.Render(res, widgets)
	if err != nil {
		b.Fatal(err)
	}
	gwc, err := ui.RenderToString(node)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := Compare(ssr.HTML, gwc, c); err != nil {
			b.Fatal(err)
		}
	}
}

// TestHydrationAllocationBudget keeps semantic verification bounded enough
// for a once-per-mount gate. It includes two browser-equivalent parses plus a
// lossless token preflight, active-content walk, and state/property checks.
// It is intentionally an allocation budget rather than a wall-clock assertion,
// so CI scheduling noise cannot make the gate flaky.
func TestHydrationAllocationBudget(t *testing.T) {
	ssr, gwc, c := renderedPair(t)
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := Compare(ssr, gwc, c); err != nil {
			panic(err)
		}
	})
	if allocs > 2000 {
		t.Fatalf("hydration parity allocations=%.1f, budget=2000", allocs)
	}
}
