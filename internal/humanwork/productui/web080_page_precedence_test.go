package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-080: page configuration precedence. Page configuration
// arrives layered across scopes — platform default, tenant default,
// enterprise-group default, company override — and nothing resolves
// which layer wins: a tenant floorplan, a company ceiling, and a
// platform primitive set compose today only by caller convention.
// The catalog's configuration rule is most-specific-valid-scope
// wins, while mandatory security restrictions accumulate (a narrower
// scope cannot weaken a broader denial). The lifecycle needs a pure
// precedence resolution with an explainable result — winning scope
// per field, overridden sources, denied weakenings — failing closed
// on unknown scopes, duplicate scopes, and unranked ceilings.
func TestTodo_WEB_080(t *testing.T) {
	// Scope ranks follow the catalog chain, broad to specific.
	for _, ranked := range []struct {
		scope string
		rank  int
	}{
		{"platform", 1},
		{"tenant", 2},
		{"enterprise-group", 3},
		{"company", 4},
	} {
		rank, ok := ConfigurationScopeRank(ranked.scope)
		if !ok || rank != ranked.rank {
			t.Fatalf("scope rank(%q) = (%d, %t), want (%d, true)", ranked.scope, rank, ok, ranked.rank)
		}
	}
	if _, ok := ConfigurationScopeRank("organization"); ok {
		t.Fatal("unlisted scope ranks")
	}

	platform := PageConfigurationSource{Scope: "platform", Version: 7, Composition: PageComposition{
		Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
		Primitives: []string{"stack", "table"}, Regions: []string{"header", "main"},
		Widgets: []WidgetBinding{{WidgetType: "metric-display", Classification: "internal"}},
		Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
	}}
	resolved := ResolvePageConfiguration([]PageConfigurationSource{platform})
	if !resolved.Compatible {
		t.Fatalf("single source refuses: %q", resolved.Reasons)
	}
	if !reflect.DeepEqual(resolved.Composition, platform.Composition) {
		t.Fatalf("single source composition = %+v, want %+v", resolved.Composition, platform.Composition)
	}
	if !reflect.DeepEqual(resolved.Sources, []string{"platform"}) {
		t.Fatalf("sources = %q", resolved.Sources)
	}
	for _, field := range []string{"floorplan", "floorplan_version", "classification_ceiling", "primitives", "regions", "widgets", "actions"} {
		found := false
		for _, winner := range resolved.Winners {
			if winner.Field == field && winner.Scope == "platform" && winner.Version == 7 {
				found = true
			}
		}
		if !found {
			t.Fatalf("no platform win recorded for %q: %+v", field, resolved.Winners)
		}
	}

	fullTenant := PageConfigurationSource{Scope: "tenant", Version: 3, Composition: PageComposition{
		Floorplan: "object", FloorplanVersion: 2, ClassificationCeiling: "confidential",
		Primitives: []string{"tabs"}, Regions: []string{"header", "summary_rail"},
		Widgets: []WidgetBinding{{WidgetType: "proposal-form", Classification: "confidential"}},
		Actions: []ActionBinding{{Capability: "journeys.approve", Token: "u", ExpectedVersion: 1, IdempotencyKey: "j", InputType: "ApproveInput"}},
	}}
	// Most specific wins per field; the tenant ceiling weakens the
	// platform ceiling, so the platform restriction survives as a
	// denial and the effective ceiling stays internal.
	overlap := ResolvePageConfiguration([]PageConfigurationSource{platform, fullTenant})
	if !overlap.Compatible {
		t.Fatalf("layered sources refuse: %q", overlap.Reasons)
	}
	if overlap.Composition.Floorplan != "object" || overlap.Composition.FloorplanVersion != 2 {
		t.Fatalf("tenant floorplan loses: %+v", overlap.Composition)
	}
	if overlap.Composition.ClassificationCeiling != "internal" {
		t.Fatalf("weakened ceiling wins: %q", overlap.Composition.ClassificationCeiling)
	}
	if len(overlap.Denied) != 1 || overlap.Denied[0] != `ceiling "confidential" from tenant denied by platform "internal"` {
		t.Fatalf("denials = %q", overlap.Denied)
	}
	for _, want := range []string{`floorplan from platform overridden by tenant`, `primitives from platform overridden by tenant`} {
		found := false
		for _, override := range overlap.Overrides {
			if override == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing override %q in %q", want, overlap.Overrides)
		}
	}

	// Tightening is allowed: the narrower scope may strengthen the
	// ceiling, and restating a value moves the win without an
	// override entry.
	tighten := ResolvePageConfiguration([]PageConfigurationSource{platform,
		{Scope: "company", Version: 1, Composition: PageComposition{
			Floorplan: "collection", ClassificationCeiling: "public"}}})
	if !tighten.Compatible {
		t.Fatalf("tightening refuses: %q", tighten.Reasons)
	}
	if tighten.Composition.ClassificationCeiling != "public" {
		t.Fatalf("tightened ceiling loses: %q", tighten.Composition.ClassificationCeiling)
	}
	if len(tighten.Denied) != 0 {
		t.Fatalf("tightening denies: %q", tighten.Denied)
	}
	for _, override := range tighten.Overrides {
		if strings.Contains(override, "floorplan") {
			t.Fatalf("restatement recorded as override: %q", tighten.Overrides)
		}
	}

	// Fail-closed inputs: unknown scopes, duplicate scopes, and
	// unranked ceilings refuse the resolution.
	for _, bad := range []struct {
		name    string
		sources []PageConfigurationSource
		reason  string
	}{
		{"unknown scope", []PageConfigurationSource{platform, {Scope: "organization", Composition: PageComposition{Floorplan: "object"}}},
			`unknown configuration scope "organization"`},
		{"duplicate scope", []PageConfigurationSource{platform, {Scope: "platform", Composition: PageComposition{Floorplan: "object"}}},
			`duplicate configuration scope "platform"`},
		{"unranked ceiling", []PageConfigurationSource{platform, {Scope: "tenant", Composition: PageComposition{ClassificationCeiling: "topsecret"}}},
			`unranked classification ceiling "topsecret" at scope "tenant"`},
	} {
		resolved := ResolvePageConfiguration(bad.sources)
		if resolved.Compatible {
			t.Fatalf("%s resolves", bad.name)
		}
		found := false
		for _, reason := range resolved.Reasons {
			if reason == bad.reason {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s reasons = %q, want %q", bad.name, resolved.Reasons, bad.reason)
		}
	}

	// Empty input resolves empty.
	if resolved := ResolvePageConfiguration(nil); !resolved.Compatible || len(resolved.Winners) != 0 {
		t.Fatalf("empty input = (%t, %+v)", resolved.Compatible, resolved.Winners)
	}
}

// Golden: precedence outcomes over layered-source scenarios.
func TestTodo_WEB_080_Golden(t *testing.T) {
	scenarios := [][]PageConfigurationSource{
		{{Scope: "platform", Version: 1, Composition: PageComposition{Floorplan: "collection", ClassificationCeiling: "internal"}}},
		{
			{Scope: "platform", Version: 1, Composition: PageComposition{Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal", Primitives: []string{"stack"}}},
			{Scope: "tenant", Version: 2, Composition: PageComposition{Floorplan: "object", ClassificationCeiling: "confidential"}},
		},
		{
			{Scope: "platform", Version: 1, Composition: PageComposition{ClassificationCeiling: "confidential"}},
			{Scope: "tenant", Version: 2, Composition: PageComposition{ClassificationCeiling: "internal"}},
			{Scope: "company", Version: 3, Composition: PageComposition{ClassificationCeiling: "internal"}},
		},
		{
			{Scope: "company", Version: 9, Composition: PageComposition{Floorplan: "monitor", ClassificationCeiling: "public"}},
			{Scope: "platform", Version: 1, Composition: PageComposition{Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "restricted", Primitives: []string{"table"}}},
			{Scope: "enterprise-group", Version: 4, Composition: PageComposition{FloorplanVersion: 2}},
		},
		{{Scope: "tenant", Composition: PageComposition{ClassificationCeiling: "topsecret"}}},
		{{Scope: "neighborhood", Composition: PageComposition{Floorplan: "object"}}},
	}
	var builder strings.Builder
	for _, sources := range scenarios {
		resolved := ResolvePageConfiguration(sources)
		builder.WriteString(resolved.Composition.Floorplan)
		builder.WriteString("\x00")
		builder.WriteString(resolved.Composition.ClassificationCeiling)
		builder.WriteString("\x00")
		if resolved.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		for _, winner := range resolved.Winners {
			builder.WriteString(winner.Field + "=" + winner.Scope)
			builder.WriteString("\x00")
		}
		builder.WriteString(strings.Join(resolved.Overrides, ";"))
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(resolved.Denied, ";"))
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(resolved.Reasons, ";"))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "1a923dc4aa0c01e69b3ea8dcab50d6732baa6ae9dc2609a525cbbce50d8dbaaf"
	if got != want {
		t.Fatalf("precedence digest = %s, want %s", got, want)
	}
}

// Browser: every registered floorplan resolves as a platform
// default — deterministically — and every registered widget limit
// composes against a public platform ceiling exactly as the ladder
// orders it.
func TestTodo_WEB_080_Browser(t *testing.T) {
	for _, floorplan := range RegisteredFloorplans().Floorplans {
		sources := []PageConfigurationSource{{Scope: "platform", Version: floorplan.Version,
			Composition: PageComposition{Floorplan: floorplan.ID, ClassificationCeiling: "internal"}}}
		first := ResolvePageConfiguration(sources)
		second := ResolvePageConfiguration(sources)
		if !first.Compatible || first.Composition.Floorplan != floorplan.ID {
			t.Fatalf("floorplan %q does not resolve: %+v", floorplan.ID, first)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("floorplan %q resolution is nondeterministic", floorplan.ID)
		}
	}
	for _, widget := range RegisteredWidgets().Widgets {
		resolved := ResolvePageConfiguration([]PageConfigurationSource{
			{Scope: "platform", Composition: PageComposition{ClassificationCeiling: "public"}},
			{Scope: "company", Composition: PageComposition{ClassificationCeiling: widget.ClassificationLimit}},
		})
		limitRank, _ := ClassificationRank(widget.ClassificationLimit)
		publicRank, _ := ClassificationRank("public")
		switch {
		case limitRank > publicRank && len(resolved.Denied) != 1:
			t.Fatalf("widget %q limit %q escapes denial: %+v", widget.ID, widget.ClassificationLimit, resolved)
		case limitRank == publicRank && (len(resolved.Denied) != 0 || resolved.Composition.ClassificationCeiling != "public"):
			t.Fatalf("widget %q public limit mishandled: %+v", widget.ID, resolved)
		}
		if !resolved.Compatible {
			t.Fatalf("widget %q ranked ceilings refuse: %q", widget.ID, resolved.Reasons)
		}
	}
}

// Conformance: source order independence, resolution determinism,
// version propagation.
func TestTodo_WEB_080_Conformance(t *testing.T) {
	specific := PageConfigurationSource{Scope: "company", Version: 5, Composition: PageComposition{Floorplan: "monitor", ClassificationCeiling: "public"}}
	broad := PageConfigurationSource{Scope: "platform", Version: 1, Composition: PageComposition{Floorplan: "collection", ClassificationCeiling: "internal"}}
	forward := ResolvePageConfiguration([]PageConfigurationSource{broad, specific})
	reverse := ResolvePageConfiguration([]PageConfigurationSource{specific, broad})
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("source order changes the resolution:\n%+v\n%+v", forward, reverse)
	}
	again := ResolvePageConfiguration([]PageConfigurationSource{broad, specific})
	if !reflect.DeepEqual(forward, again) {
		t.Fatal("resolution is nondeterministic")
	}
	for _, winner := range forward.Winners {
		if winner.Field == "floorplan" && (winner.Scope != "company" || winner.Version != 5) {
			t.Fatalf("floorplan win loses version lineage: %+v", winner)
		}
	}
	if !reflect.DeepEqual(forward.Sources, []string{"platform", "company"}) {
		t.Fatalf("sources = %q, want broad-to-specific order", forward.Sources)
	}
}
