package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-129: approval progress presentation. The
// governed publication pipeline gates validation, preview
// evidence, and review approval in order, but no presenter
// shows progress through those gates: the first surface
// re-implements the gate checks by convention and a
// half-approved publication can present as ready. The
// compiler needs the governed progress — each gate's
// verdict with its requirement, read-only, in pipeline
// order — so approval progress resolves today and
// complete means every gate passes.
func TestTodo_WEB_129(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	plan := PreviewPlan{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: []string{"en-US"}},
	}}
	cases, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	approved := PublicationRequest{Draft: governedApprovalDraft(), Version: 1,
		Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
		Preview: plan, PreviewCases: cases,
		Review: PublicationReview{Reviewer: "a.muster", Approved: true}}

	progress := ResolveApprovalProgress(locale, approved)
	if !progress.Complete {
		t.Fatal("an approved publication does not present as complete")
	}
	if progress.Current != -1 {
		t.Fatalf("complete progress points at stage %d", progress.Current)
	}
	if len(progress.Stages) != 3 {
		t.Fatalf("progress has %d stages", len(progress.Stages))
	}
	for i, stage := range progress.Stages {
		if !stage.Complete || stage.Title == "" {
			t.Fatalf("stage %d = %+v", i, stage)
		}
	}

	unreviewed := approved
	unreviewed.Review.Approved = false
	pending := ResolveApprovalProgress(locale, unreviewed)
	if pending.Complete {
		t.Fatal("an unreviewed publication presents as complete")
	}
	if pending.Current != 2 {
		t.Fatalf("unreviewed progress points at stage %d", pending.Current)
	}
	if pending.Stages[2].Detail == "" {
		t.Fatal("the blocking stage states no requirement")
	}
}

// Golden: progress over request variants.
func TestTodo_WEB_129_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	plan := PreviewPlan{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: []string{"en-US"}},
	}}
	cases, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	base := PublicationRequest{Draft: governedApprovalDraft(), Version: 1,
		Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
		Preview: plan, PreviewCases: cases,
		Review: PublicationReview{Reviewer: "a.muster", Approved: true}}
	invalid := base
	invalid.Draft.Composition.Purpose = ""
	stale := base
	stale.PreviewCases = nil
	anonymous := base
	anonymous.Review.Reviewer = ""
	requests := []PublicationRequest{{}, base, invalid, stale, anonymous}
	var builder strings.Builder
	for _, request := range requests {
		progress := ResolveApprovalProgress(locale, request)
		fmt.Fprintf(&builder, "%d|%t", progress.Current, progress.Complete)
		for _, stage := range progress.Stages {
			fmt.Fprintf(&builder, "|%s|%t|%s", stage.Title, stage.Complete, stage.Detail)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "e64d04f791d55b522e1152e636e9aeb85bb79c36cfeddf667c221418a32e51b1"
	if got != want {
		t.Fatalf("approval progress digest = %s, want %s", got, want)
	}
}

// Browser: progress resolution is deterministic, pure,
// and never publishes.
func TestTodo_WEB_129_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	request := PublicationRequest{Draft: governedApprovalDraft(), Version: 1}
	before := request
	first := ResolveApprovalProgress(locale, request)
	second := ResolveApprovalProgress(locale, request)
	if !reflect.DeepEqual(request, before) {
		t.Fatal("progress resolution mutates its request")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("progress resolution is nondeterministic")
	}
	_, beforeErr := PublishGoverned(&PageRevisionLog{}, request)
	ResolveApprovalProgress(locale, request)
	_, afterErr := PublishGoverned(&PageRevisionLog{}, request)
	if (beforeErr == nil) != (afterErr == nil) || (beforeErr != nil && beforeErr.Error() != afterErr.Error()) {
		t.Fatal("progress resolution changes pipeline behavior")
	}
}

// Conformance: stage order is fixed, titles are distinct
// catalog copy, resolution is stable.
func TestTodo_WEB_129_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	progress := ResolveApprovalProgress(locale, PublicationRequest{})
	seen := map[string]bool{}
	for i, stage := range progress.Stages {
		if stage.Title == "" {
			t.Fatalf("stage %d untitled", i)
		}
		if seen[stage.Title] {
			t.Fatalf("stage title repeats: %q", stage.Title)
		}
		seen[stage.Title] = true
		if !stage.Complete && stage.Detail == "" {
			t.Fatalf("blocked stage %d states no requirement", i)
		}
	}
	if progress.Complete || progress.Current != 0 {
		t.Fatal("an empty request does not stall at the first gate")
	}
	if !reflect.DeepEqual(progress, ResolveApprovalProgress(locale, PublicationRequest{})) {
		t.Fatal("resolution is unstable")
	}
}

// Integration: complete progress agrees with the governed
// pipeline, and each blocked stage matches its gate
// error.
func TestTodo_WEB_129_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	plan := PreviewPlan{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: []string{"en-US"}},
	}}
	cases, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	request := PublicationRequest{Draft: governedApprovalDraft(), Version: 1,
		Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
		Preview: plan, PreviewCases: cases,
		Review: PublicationReview{Reviewer: "a.muster", Approved: true}}
	if progress := ResolveApprovalProgress(locale, request); !progress.Complete {
		t.Fatal("progress disagrees with a publishable request")
	}
	if _, err := PublishGoverned(&PageRevisionLog{}, request); err != nil {
		t.Fatalf("pipeline disagrees with complete progress: %v", err)
	}
	blocked := request
	blocked.Review.Approved = false
	if _, err := PublishGoverned(&PageRevisionLog{}, blocked); err == nil ||
		!strings.Contains(err.Error(), "no approved review") {
		t.Fatalf("review gate = %v", err)
	}
	if progress := ResolveApprovalProgress(locale, blocked); progress.Complete || progress.Current != 2 {
		t.Fatalf("progress disagrees with the review gate: %+v", progress)
	}
}

// Fault: foreign preview pages, cleared evidence, blank
// reviewers, and invalid versions stall at their gate.
func TestTodo_WEB_129_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	plan := PreviewPlan{Page: "people", Fixture: "f", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: []string{"en-US"}},
	}}
	foreign, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	base := PublicationRequest{Draft: governedApprovalDraft(), Version: 1,
		Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
		Preview: plan, PreviewCases: foreign,
		Review: PublicationReview{Reviewer: "a.muster", Approved: true}}
	faults := []PublicationRequest{base, {}}
	cleared := base
	cleared.Preview = PreviewPlan{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: []string{"en-US"}},
	}}
	cleared.PreviewCases = nil
	nameless := base
	nameless.Review.Reviewer = "  "
	faults = append(faults, cleared, nameless)
	for i, request := range faults {
		if progress := ResolveApprovalProgress(locale, request); progress.Complete {
			t.Fatalf("fault %d presents as complete", i)
		}
	}
	if progress := ResolveApprovalProgress(locale, cleared); progress.Current != 1 {
		t.Fatalf("cleared evidence stalls at stage %d", progress.Current)
	}
}

// governedApprovalDraft opens the scratch draft the
// approval progress matrices publish through the
// pipeline gates.
func governedApprovalDraft() PageDraft {
	draft := NewPageDraftFromScratch("studio")
	draft.Composition = PageComposition{
		Purpose: "Track promotion journeys", Audience: "managers",
		Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
		Regions: []string{"primary"},
		Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
			Classification: "internal", SourceType: "projection", SourceID: "journeys"}},
		Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
	}
	return draft
}
