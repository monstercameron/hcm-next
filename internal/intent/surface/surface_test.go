package surface_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/surface"
)

func intentView() *surface.IntentView {
	return &surface.IntentView{
		ID: "intent/promo-21", Revision: 3, Status: "OPEN",
		Fields: map[string]surface.Field{
			"worker":            {Value: "jane"},
			"manager":           {Value: "bob"},
			"salary":            {Value: "95000.00", Restricted: true},
			"state/feasibility": {Value: "UNKNOWN"},
			"state/freshness":   {Value: "DEGRADED"},
		},
		History: []surface.RevisionRecord{
			{Revision: 1, Digest: "sha256:r1", Note: "created"},
			{Revision: 2, Digest: "sha256:r2", Note: "approved"},
			{Revision: 3, Digest: "sha256:r3", Note: "revalidated"},
		},
	}
}

func analyst() surface.Authority {
	return surface.Authority{Principal: "analyst-1", Purposes: []string{"review"}}
}

func cleared() surface.Authority {
	return surface.Authority{Principal: "hrbp-1", Purposes: []string{"review", "export"}, Clearance: true}
}

// TestIntentLifecycleSurfaceAuthorizationAndTruth is the INTENT-021
// primary test: every surface filters by current authority, timelines
// explain exact versions without collapsing unknown/degraded dimensions,
// actions version-chain into successors, and subscriptions emit redacted
// versioned transitions.
func TestIntentLifecycleSurfaceAuthorizationAndTruth(t *testing.T) {
	view := intentView()

	// Deep link and search work under authority and hide existence
	// without it: anonymous reads match a missing intent exactly.
	link, err := surface.DeepLink(view, analyst())
	if err != nil || link == "" {
		t.Fatalf("deep link = %q, %v", link, err)
	}
	if _, err := surface.DeepLink(view, surface.Authority{}); !errors.Is(err, surface.ErrNotFound) {
		t.Fatalf("anonymous deep link = %v, want ErrNotFound", err)
	}
	if _, err := surface.DeepLink(nil, analyst()); !errors.Is(err, surface.ErrNotFound) {
		t.Fatalf("missing deep link = %v, want ErrNotFound", err)
	}
	hits := surface.Search([]*surface.IntentView{view, nil}, analyst(), "jane")
	if len(hits) != 1 {
		t.Fatalf("search hits = %d, want exactly the authorized view", len(hits))
	}

	// Timeline explains every revision digest with unknown and degraded
	// dimensions stated, never collapsed.
	timeline, err := surface.Timeline(view, analyst())
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 3 || timeline[2].Digest != "sha256:r3" {
		t.Fatalf("timeline = %+v, want the exact three-revision chain", timeline)
	}
	fields, digest, err := surface.Inspector(view, analyst())
	if err != nil || digest == "" {
		t.Fatalf("inspector = %+v, %q, %v", fields, digest, err)
	}
	names := map[string]bool{}
	for _, field := range fields {
		names[field.Name] = true
	}
	for _, want := range []string{"worker", "manager", "state/feasibility", "state/freshness"} {
		if !names[want] {
			t.Fatalf("inspector hides %s: %+v", want, fields)
		}
	}
	if names["salary"] {
		t.Fatalf("inspector leaks restricted salary without clearance: %+v", fields)
	}
	clearedFields, _, err := surface.Inspector(view, cleared())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, field := range clearedFields {
		if field.Name == "salary" && field.Restricted && field.Value == "95000.00" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cleared inspector misses marked salary: %+v", clearedFields)
	}

	// Export binds purpose: granted purposes filter, ungranted refuse.
	exported, err := surface.Export(view, cleared(), "export")
	if err != nil {
		t.Fatal(err)
	}
	if exported["salary"] != "95000.00" || exported["worker"] != "jane" {
		t.Fatalf("export = %v, want the cleared field set", exported)
	}
	if _, err := surface.Export(view, analyst(), "export"); !errors.Is(err, surface.ErrNotAuthorized) {
		t.Fatalf("purpose-less export = %v, want ErrNotAuthorized", err)
	}

	// Lifecycle actions chain revisions: correct appends rev 4 with
	// history sealed, then cancel closes rev 5.
	corrected, err := surface.Correct(view, cleared(), 3, map[string]string{"manager": "carol"})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Revision != 4 || corrected.Fields["manager"].Value != "carol" || len(corrected.History) != 4 {
		t.Fatalf("corrected = %+v, want rev 4 with sealed history", corrected)
	}
	if view.Revision != 3 || view.Fields["manager"].Value != "bob" {
		t.Fatal("correct mutated the input view instead of versioning")
	}
	cancelled, err := surface.Cancel(corrected, cleared(), 4, "reorg paused")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "CANCELLED" || cancelled.Revision != 5 {
		t.Fatalf("cancelled = %+v, want terminal rev 5", cancelled)
	}
	// Stale revisions and closed intents refuse further action.
	if _, err := surface.Correct(view, cleared(), 2, map[string]string{"manager": "x"}); !errors.Is(err, surface.ErrStaleRevision) {
		t.Fatalf("stale correct = %v, want ErrStaleRevision", err)
	}
	if _, err := surface.Cancel(cancelled, cleared(), 5, "again"); !errors.Is(err, surface.ErrClosedIntent) {
		t.Fatalf("close-closed = %v, want ErrClosedIntent", err)
	}

	// Supersede and escalate link related intents without rewriting.
	superseded, err := surface.Supersede(intentView(), cleared(), 3, "intent/promo-22")
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Status != "SUPERSEDED" || len(superseded.Related) != 1 {
		t.Fatalf("superseded = %+v, want the successor link", superseded)
	}
	escalated, err := surface.Escalate(intentView(), cleared(), 3, "intent/escalation-1")
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Status != "OPEN" || len(escalated.Related) != 1 || escalated.Revision != 4 {
		t.Fatalf("escalated = %+v, want an OPEN rev 4 with the link", escalated)
	}

	// Subscriptions emit redacted versioned transitions: states only,
	// chained revisions, nothing after authority lapses.
	sub, err := surface.Subscribe(view, analyst())
	if err != nil || sub.IntentID != "intent/promo-21" || sub.Revision != 3 {
		t.Fatalf("subscribe = %+v, %v", sub, err)
	}
	if err := sub.Emit(view, analyst(), surface.Transition{Revision: 4, From: "OPEN", To: "DEGRADED"}); err != nil {
		t.Fatal(err)
	}
	if len(sub.Events) != 1 || sub.Events[0].To != "DEGRADED" {
		t.Fatalf("events = %+v, want the single versioned transition", sub.Events)
	}
	if err := sub.Emit(view, analyst(), surface.Transition{Revision: 4, From: "OPEN", To: "DEGRADED"}); !errors.Is(err, surface.ErrStaleRevision) {
		t.Fatalf("replay emit = %v, want ErrStaleRevision", err)
	}
	if err := sub.Emit(view, surface.Authority{}, surface.Transition{Revision: 5, From: "DEGRADED", To: "OPEN"}); !errors.Is(err, surface.ErrNotFound) {
		t.Fatalf("post-authority emit = %v, want ErrNotFound", err)
	}
}

func TestTodo_INTENT_021_Security(t *testing.T) {
	view := intentView()

	// Existence oracle: missing, anonymous and wrong-principal reads are
	// identical. (Wrong principal still resolves here: field filtering is
	// the boundary, and the analyst sees no restricted fields.)
	if _, err := surface.Timeline(nil, cleared()); !errors.Is(err, surface.ErrNotFound) {
		t.Fatalf("missing timeline = %v, want ErrNotFound", err)
	}
	// Restricted correction without clearance is refused even when the
	// field exists.
	if _, err := surface.Correct(view, analyst(), 3, map[string]string{"salary": "1.00"}); !errors.Is(err, surface.ErrNotAuthorized) {
		t.Fatalf("restricted correct = %v, want ErrNotAuthorized", err)
	}
	// Unknown-field correction is refused, never created.
	if _, err := surface.Correct(view, cleared(), 3, map[string]string{"backdoor": "x"}); !errors.Is(err, surface.ErrNotAuthorized) {
		t.Fatalf("unknown-field correct = %v, want ErrNotAuthorized", err)
	}
	// Empty successor and escalation targets are refused.
	if _, err := surface.Supersede(view, cleared(), 3, ""); !errors.Is(err, surface.ErrNotAuthorized) {
		t.Fatalf("empty supersede = %v, want ErrNotAuthorized", err)
	}
	if _, err := surface.Escalate(view, cleared(), 3, "  "); !errors.Is(err, surface.ErrNotAuthorized) {
		t.Fatalf("empty escalate = %v, want ErrNotAuthorized", err)
	}
	// Anonymous subscription and export are indistinguishable from
	// missing: not found, never unauthorized-with-existence.
	if _, err := surface.Subscribe(view, surface.Authority{}); !errors.Is(err, surface.ErrNotFound) {
		t.Fatalf("anonymous subscribe = %v, want ErrNotFound", err)
	}
	if _, err := surface.Export(nil, cleared(), "export"); !errors.Is(err, surface.ErrNotFound) {
		t.Fatalf("missing export = %v, want ErrNotFound", err)
	}
	// Search never admits skips: an analyst-only corpus returns only
	// matches with no count of hidden views.
	hits := surface.Search([]*surface.IntentView{nil, nil}, analyst(), "")
	if len(hits) != 0 {
		t.Fatalf("search over hidden corpus = %d hits", len(hits))
	}
}

func TestTodo_INTENT_021_Mutation(t *testing.T) {
	// Mutant 1: a field allowlist cannot smuggle restricted values.
	scoped := surface.Authority{Principal: "analyst-1", Fields: []string{"salary"}, Purposes: []string{"review"}}
	fields, _, err := surface.Inspector(intentView(), scoped)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Name == "salary" {
			t.Fatalf("allowlisted restricted field disclosed without clearance: %+v", fields)
		}
	}
	// Mutant 2: history is append-only across actions.
	view := intentView()
	corrected, err := surface.Correct(view, cleared(), 3, map[string]string{"manager": "carol"})
	if err != nil {
		t.Fatal(err)
	}
	for i, record := range corrected.History[:3] {
		if record != view.History[i] {
			t.Fatalf("history[%d] rewritten: %+v", i, record)
		}
	}
	// Mutant 3: unknown and degraded dimension states survive inspection.
	exported, err := surface.Export(intentView(), cleared(), "export")
	if err != nil {
		t.Fatal(err)
	}
	if exported["state/feasibility"] != "UNKNOWN" || exported["state/freshness"] != "DEGRADED" {
		t.Fatalf("dimensions collapsed in export: %v", exported)
	}
	// Mutant 4: subscription events carry states, never fields.
	sub, err := surface.Subscribe(view, analyst())
	if err != nil {
		t.Fatal(err)
	}
	if err := sub.Emit(view, analyst(), surface.Transition{Revision: 4, From: "OPEN", To: "CLOSED"}); err != nil {
		t.Fatal(err)
	}
	event := sub.Events[0]
	if event.Revision != 4 || event.From != "OPEN" || event.To != "CLOSED" {
		t.Fatalf("event carries more than its versioned states: %+v", event)
	}
	// Mutant 5: gap revisions are refused, not skipped.
	if err := sub.Emit(view, analyst(), surface.Transition{Revision: 6, From: "CLOSED", To: "OPEN"}); !errors.Is(err, surface.ErrStaleRevision) {
		t.Fatalf("gap emit = %v, want ErrStaleRevision", err)
	}
}
