package ssrshell

import (
	"testing"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

// promotionListGoldenHTML and promotionDetailGoldenHTML pin the exact bytes
// [Render] produces for the two real Promotion PageDefinitions
// (tools/uxqual/pagedef.PromotionListPageDefinition and
// PromotionDetailPageDefinition). A silent change to the template, the
// landmark mapping, or either PageDefinition's own shape shows up here as a
// byte-for-byte mismatch, the same way tools/uxqual/pagedef's own
// TestTodo_WEB_002_Golden pins Digest() rather than trusting that "it still
// validates" is enough.
const promotionListGoldenHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Promotion journeys</title>
<style>.visually-hidden{border:0;clip:rect(0,0,0,0);height:1px;margin:-1px;overflow:hidden;padding:0;position:absolute;white-space:nowrap;width:1px;}</style>
</head>
<body>
<a class="visually-hidden" href="#main-content">Skip to main content</a>
<div id="live-region" role="status" aria-live="polite" aria-atomic="true"></div>
<header id="region-shell" aria-label="Application shell: shell">
</header>
<section id="region-page-identity" aria-label="Page identity: page-identity">
<h1 id="heading-page-identity">Promotion journeys</h1>
</section>
<main id="main-content" aria-label="Primary: workforce">
<h2 id="heading-workforce">Workforce</h2>
<div class="widget-slot" data-slot-id="workforce-table" data-widget-ref="widget.table.workforce.v1" role="presentation"></div>
<div class="widget-slot" data-slot-id="create-worker-form" data-widget-ref="widget.form.create-worker.v1" role="presentation"></div>
</main>
<aside id="region-journeys" aria-label="Supporting: journeys">
<h2 id="heading-journeys">Promotion journeys</h2>
<div class="widget-slot" data-slot-id="journeys-list" data-widget-ref="widget.list.journeys.v1" role="presentation"></div>
<div class="widget-slot" data-slot-id="proposal-form" data-widget-ref="widget.form.propose-journey.v1" role="presentation"></div>
</aside>
<script type="application/json" id="page-definition">{"page_id":"promotion.journeys.list","version":1,"digest":"sha256:9d223d335352fad485c1327799b704964f20a3c5d3955b843a9116998743a28a"}</script>
</body>
</html>
`

const promotionDetailGoldenHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Promotion journey</title>
<style>.visually-hidden{border:0;clip:rect(0,0,0,0);height:1px;margin:-1px;overflow:hidden;padding:0;position:absolute;white-space:nowrap;width:1px;}</style>
</head>
<body>
<a class="visually-hidden" href="#main-content">Skip to main content</a>
<div id="live-region" role="status" aria-live="polite" aria-atomic="true"></div>
<header id="region-shell" aria-label="Application shell: shell">
</header>
<section id="region-page-identity" aria-label="Page identity: page-identity">
<h1 id="heading-page-identity">Promotion journey</h1>
</section>
<nav id="region-steps" aria-label="Local navigation: steps">
<div class="widget-slot" data-slot-id="stepper" data-widget-ref="widget.stepper.journey-stage.v1" role="presentation"></div>
</nav>
<main id="main-content" aria-label="Primary: comparison">
<h2 id="heading-comparison">Comparison</h2>
<div class="widget-slot" data-slot-id="comparison-table" data-widget-ref="widget.table.comparison.v1" role="presentation"></div>
<div class="widget-slot" data-slot-id="pay-band-gauge" data-widget-ref="widget.gauge.pay-band.v1" role="presentation"></div>
<div class="widget-slot" data-slot-id="budget-gauge" data-widget-ref="widget.gauge.budget.v1" role="presentation"></div>
</main>
<aside id="region-engine" aria-label="Supporting: engine">
<h2 id="heading-engine">Engine and work items</h2>
<div class="widget-slot" data-slot-id="engine-facts" data-widget-ref="widget.factlist.v1" role="presentation"></div>
<div class="widget-slot" data-slot-id="work-items" data-widget-ref="widget.table.work-items.v1" role="presentation"></div>
<div class="widget-slot" data-slot-id="timeline" data-widget-ref="widget.timeline.v1" role="presentation"></div>
</aside>
<footer id="region-decision" aria-label="Completion: decision">
<h2 id="heading-decision">Decision</h2>
</footer>
<script type="application/json" id="page-definition">{"page_id":"promotion.journeys.detail","version":1,"digest":"sha256:afca82af0138a7f9a3f7db815de034b820355f473596d1f824da3c925c25bf11"}</script>
</body>
</html>
`

// TestTodo_WEB_025_Golden pins both the exact rendered bytes and the
// RenderedShell.Digest for the two real Promotion PageDefinitions.
func TestTodo_WEB_025_Golden(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		rs, err := Render(pagedef.PromotionListPageDefinition())
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if rs.HTML != promotionListGoldenHTML {
			t.Fatalf("rendered HTML does not match pinned golden bytes.\n--- got ---\n%s\n--- want ---\n%s", rs.HTML, promotionListGoldenHTML)
		}
		const wantDigest = "sha256:fb85a4ac7cbbca487e32ae0956420e15d2a21377f92214b84b6dbb1052210a3a"
		if rs.Digest != wantDigest {
			t.Fatalf("RenderedShell.Digest = %q, want pinned golden %q", rs.Digest, wantDigest)
		}
	})

	t.Run("detail", func(t *testing.T) {
		rs, err := Render(pagedef.PromotionDetailPageDefinition())
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if rs.HTML != promotionDetailGoldenHTML {
			t.Fatalf("rendered HTML does not match pinned golden bytes.\n--- got ---\n%s\n--- want ---\n%s", rs.HTML, promotionDetailGoldenHTML)
		}
		const wantDigest = "sha256:1ac893ac96477b6d9ab53e60952028de27827b253f5960083ef05b5486f06235"
		if rs.Digest != wantDigest {
			t.Fatalf("RenderedShell.Digest = %q, want pinned golden %q", rs.Digest, wantDigest)
		}
	})
}
