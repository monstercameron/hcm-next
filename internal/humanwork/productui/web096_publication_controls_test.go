package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-096: governed page publication controls. PublishDraft
// is purely mechanical — versions, bindings, staleness — so any
// draft content publishes with no validation, no preview
// evidence, and no review: the asset pipeline's "versioned,
// previewed with fixtures, reviewed, and published" is
// unenforced. The lifecycle needs a governed control wrapping
// the mechanical publish: validation must pass, preview
// evidence must equal its plan's expansion for the draft page,
// and a named reviewer must approve — failing closed at each
// gate in pipeline order.
func TestTodo_WEB_096(t *testing.T) {
	draft := NewPageDraftFromScratch("studio")
	draft.Composition = PageComposition{
		Purpose: "Track promotion journeys", Audience: "managers",
		Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
		Regions: []string{"primary"},
		Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
			Classification: "internal", SourceType: "projection", SourceID: "journeys"}},
		Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
	}
	plan := PreviewPlan{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: []string{"en-US"}},
	}}
	cases, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	request := PublicationRequest{Draft: draft, Version: 1,
		Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
		Preview: plan, PreviewCases: cases,
		Review: PublicationReview{Reviewer: "a.muster", Approved: true}}
	var log PageRevisionLog
	published, err := PublishGoverned(&log, request)
	if err != nil {
		t.Fatalf("governed publication refuses: %v", err)
	}
	if published.Version != 1 || published.Snapshot.Page != "studio" || published.Digest == "" {
		t.Fatalf("published revision = %+v", published)
	}

	// Each gate fails closed in pipeline order.
	invalidDraft := request
	invalidDraft.Draft.Composition.Purpose = ""
	if _, err := PublishGoverned(&PageRevisionLog{}, invalidDraft); err == nil ||
		!strings.HasPrefix(err.Error(), "productui: publication blocked by validation: purpose: missing page purpose") {
		t.Fatalf("validation gate = %v", err)
	}
	noPreview := request
	noPreview.Preview = PreviewPlan{}
	if _, err := PublishGoverned(&PageRevisionLog{}, noPreview); err == nil ||
		!strings.HasPrefix(err.Error(), "productui: publication preview invalid: ") {
		t.Fatalf("preview gate = %v", err)
	}
	foreignPlan := request
	foreignPlan.Preview = PreviewPlan{Page: "people", Fixture: "f"}
	foreignCases, err := ExpandPreviewPlan(foreignPlan.Preview)
	if err != nil {
		t.Fatal(err)
	}
	foreignPlan.PreviewCases = foreignCases
	if _, err := PublishGoverned(&PageRevisionLog{}, foreignPlan); err == nil ||
		!strings.HasPrefix(err.Error(), `productui: publication preview targets page "people", draft binds page "studio"`) {
		t.Fatalf("page binding gate = %v", err)
	}
	staleEvidence := request
	staleEvidence.PreviewCases = nil
	if _, err := PublishGoverned(&PageRevisionLog{}, staleEvidence); err == nil ||
		err.Error() != "productui: publication preview evidence does not match the plan" {
		t.Fatalf("evidence gate = %v", err)
	}
	unapproved := request
	unapproved.Review = PublicationReview{Reviewer: "a.muster"}
	if _, err := PublishGoverned(&PageRevisionLog{}, unapproved); err == nil ||
		err.Error() != "productui: publication has no approved review" {
		t.Fatalf("review gate = %v", err)
	}
	anonymous := request
	anonymous.Review = PublicationReview{Approved: true}
	if _, err := PublishGoverned(&PageRevisionLog{}, anonymous); err == nil ||
		err.Error() != "productui: publication review names no reviewer" {
		t.Fatalf("reviewer gate = %v", err)
	}
}

// Golden: governed publication outcomes over request variants.
func TestTodo_WEB_096_Golden(t *testing.T) {
	draft := NewPageDraftFromScratch("studio")
	draft.Composition = PageComposition{
		Purpose: "Track promotion journeys", Audience: "managers",
		Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
		Regions: []string{"primary"},
		Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
			Classification: "internal", SourceType: "projection", SourceID: "journeys"}},
		Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
	}
	plan := PreviewPlan{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: []string{"en-US", "ar"}},
	}}
	cases, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	base := PublicationRequest{Draft: draft, Version: 1,
		Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
		Preview: plan, PreviewCases: cases,
		Review: PublicationReview{Reviewer: "a.muster", Approved: true}}
	unvalidated := base
	unvalidated.Draft.Composition.Audience = ""
	unpreviewed := base
	unpreviewed.Preview = PreviewPlan{Page: "studio", Fixture: ""}
	unreviewed := base
	unreviewed.Review = PublicationReview{}
	requests := []PublicationRequest{base, unvalidated, unpreviewed, unreviewed}
	var builder strings.Builder
	for _, request := range requests {
		var log PageRevisionLog
		published, err := PublishGoverned(&log, request)
		if err != nil {
			builder.WriteString("refused")
			builder.WriteString("\x00")
			builder.WriteString(err.Error())
		} else {
			builder.WriteString("published")
			builder.WriteString("\x00")
			builder.WriteString(string(published.Snapshot.Page))
			builder.WriteString("\x00")
			builder.WriteString(published.Digest)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "5a7bf3b38e0de462f22b45624a4daebd15899e313c74afd5e89bed37a0f1fdde"
	if got != want {
		t.Fatalf("publication digest = %s, want %s", got, want)
	}
}

// Browser: every registered page publishes a governed v1 —
// deterministically.
func TestTodo_WEB_096_Browser(t *testing.T) {
	for _, definition := range PageDefinitions() {
		draft := NewPageDraftFromScratch(definition.ID)
		draft.Composition = PageComposition{
			Purpose: definition.Title, Audience: definition.Label,
			Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
			Regions: []string{"primary"},
			Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
				Classification: "internal", SourceType: "projection", SourceID: "journeys"}},
			Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
		}
		plan := PreviewPlan{Page: definition.ID, Fixture: "catalog", Dimensions: []PreviewDimension{
			{Axis: PreviewAxisLocale, Cases: []string{"en-US"}},
		}}
		cases, err := ExpandPreviewPlan(plan)
		if err != nil {
			t.Fatal(err)
		}
		request := PublicationRequest{Draft: draft, Version: 1,
			Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
			Preview: plan, PreviewCases: cases,
			Review: PublicationReview{Reviewer: "studio", Approved: true}}
		var firstLog, secondLog PageRevisionLog
		first, err := PublishGoverned(&firstLog, request)
		if err != nil {
			t.Fatalf("page %q governed publication refuses: %v", definition.ID, err)
		}
		second, err := PublishGoverned(&secondLog, request)
		if err != nil {
			t.Fatal(err)
		}
		if first.Digest == "" || !reflect.DeepEqual(first, second) {
			t.Fatalf("page %q publication unstable: %+v vs %+v", definition.ID, first, second)
		}
	}
}

// Conformance: gate order pinned, retry idempotent, mechanical
// conflicts still surface.
func TestTodo_WEB_096_Conformance(t *testing.T) {
	draft := NewPageDraftFromScratch("studio")
	draft.Composition = PageComposition{
		Purpose: "Track promotion journeys", Audience: "managers",
		Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
		Regions: []string{"primary"},
		Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
			Classification: "internal", SourceType: "projection", SourceID: "journeys"}},
		Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
	}
	plan := PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: []string{"en-US"}},
	}}
	cases, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	// Everything broken at once still reports the first gate.
	everything := PublicationRequest{Draft: draft, Version: 1,
		Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
		Preview: PreviewPlan{}, PreviewCases: nil, Review: PublicationReview{}}
	everything.Draft.Composition.Purpose = ""
	if _, err := PublishGoverned(&PageRevisionLog{}, everything); err == nil ||
		!strings.HasPrefix(err.Error(), "productui: publication blocked by validation: ") {
		t.Fatalf("gate order = %v", err)
	}
	// Retry publishes idempotently onto the same log.
	request := PublicationRequest{Draft: draft, Version: 1,
		Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
		Preview: plan, PreviewCases: cases,
		Review: PublicationReview{Reviewer: "a.muster", Approved: true}}
	var log PageRevisionLog
	first, err := PublishGoverned(&log, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PublishGoverned(&log, request)
	if err != nil {
		t.Fatalf("retry refuses: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("retry diverges")
	}
}
