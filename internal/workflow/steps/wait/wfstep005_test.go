package wait_test

import (
	"errors"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

var updateGolden = flag.Bool("update", false, "update golden fixtures in testdata/")

// --- fixture helpers -------------------------------------------------------

func mustInstant(t *testing.T, s string) values.Instant {
	t.Helper()
	var i values.Instant
	if err := i.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("instant %q: %v", s, err)
	}
	return i
}

func mustLocalDate(t *testing.T, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatalf("local date %q: %v", s, err)
	}
	return d
}

func mustLocalTime(t *testing.T, hour, minute int) values.LocalTime {
	t.Helper()
	lt, err := values.NewLocalTime(hour, minute, 0, 0)
	if err != nil {
		t.Fatalf("local time %d:%d: %v", hour, minute, err)
	}
	return lt
}

// testZone is America/New_York on the same tzdb release and DST transition
// dates internal/kernel/values' own timer tests use, so the gap (2026-03-08
// 02:30) and fold (2026-11-01 01:30) fixtures are known-good.
var testZone = values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}
var testCalendar = values.CalendarRef{Ref: "us-federal", Version: "2026.1"}
var testDataset = values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}

func fixedInstantNode(t *testing.T, fireAt values.Instant) wait.CompiledWaitNode {
	t.Helper()
	return wait.CompiledWaitNode{
		WorkflowID:      "wf.promotion",
		WorkflowVersion: 1,
		NodeID:          "wait.effective_date",
		WakeInstant:     fireAt,
		Zone:            testZone,
		Calendar:        testCalendar,
		Policy:          values.ReferenceUpdatePin,
	}
}

func localDateNode(t *testing.T, date values.LocalDate, policy values.ReferenceUpdatePolicy) wait.CompiledWaitNode {
	t.Helper()
	return wait.CompiledWaitNode{
		WorkflowID:      "wf.promotion",
		WorkflowVersion: 1,
		NodeID:          "wait.effective_date",
		WakeLocalDate:   date,
		Disambiguation:  values.DisambiguationRejectGap,
		Zone:            testZone,
		Calendar:        testCalendar,
		Policy:          policy,
	}
}

// --- TestTodo_WF_STEP_005 ---------------------------------------------------

// TestTodo_WF_STEP_005 proves planning/todos.md WF-STEP-005: a durable timer
// requirement replaces an in-memory sleep, early and duplicate wakes are
// refused/deduplicated rather than guessed, and a DST-ambiguous local time or
// a dataset/policy change without a declared policy cannot advance.
func TestTodo_WF_STEP_005(t *testing.T) {
	t.Run("RED_no_in_memory_sleep_durable_requirement_instead", func(t *testing.T) {
		fireAt := mustInstant(t, "2026-06-17T14:00:00Z")
		node := fixedInstantNode(t, fireAt)
		req, err := wait.ComputeTimerRequirement(node, testDataset)
		if err != nil {
			t.Fatalf("ComputeTimerRequirement: %v", err)
		}
		if req.Digest == "" {
			t.Fatal("a computed requirement must carry a content digest")
		}
		if req.FireAtText() != fireAt.String() {
			t.Fatalf("FireAt = %s, want %s", req.FireAtText(), fireAt.String())
		}
		// The requirement is data; nothing about calling ComputeTimerRequirement
		// blocks, sleeps or schedules anything. Resolving it requires an
		// explicit caller-supplied wake, proving there is no background wheel.
		res, err := wait.Resolve(req, fireAt, wait.WakeEvent{Kind: wait.EventWake})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Outcome != wait.OutcomeFired {
			t.Fatalf("Outcome = %s, want FIRED", res.Outcome)
		}
	})

	t.Run("RED_early_wake_refused", func(t *testing.T) {
		fireAt := mustInstant(t, "2026-06-17T14:00:00Z")
		early := mustInstant(t, "2026-06-17T13:59:59Z")
		node := fixedInstantNode(t, fireAt)
		req, err := wait.ComputeTimerRequirement(node, testDataset)
		if err != nil {
			t.Fatalf("ComputeTimerRequirement: %v", err)
		}
		if _, err := wait.Resolve(req, early, wait.WakeEvent{Kind: wait.EventWake}); !errors.Is(err, wait.ErrEarlyWake) {
			t.Fatalf("Resolve(early) error = %v, want ErrEarlyWake", err)
		}
	})

	t.Run("RED_duplicate_wake_returns_original_resolution", func(t *testing.T) {
		fireAt := mustInstant(t, "2026-06-17T14:00:00Z")
		node := fixedInstantNode(t, fireAt)
		req, err := wait.ComputeTimerRequirement(node, testDataset)
		if err != nil {
			t.Fatalf("ComputeTimerRequirement: %v", err)
		}
		first, err := wait.Resolve(req, fireAt, wait.WakeEvent{Kind: wait.EventWake})
		if err != nil {
			t.Fatalf("first Resolve: %v", err)
		}
		// A duplicate wake arrives much later, with a "now" that would change
		// the answer if Resolve recomputed instead of replaying.
		muchLater := mustInstant(t, "2027-01-01T00:00:00Z")
		second, err := wait.Resolve(req, muchLater, wait.WakeEvent{Kind: wait.EventWake, Prior: &first})
		if err != nil {
			t.Fatalf("duplicate Resolve: %v", err)
		}
		if second.Digest != first.Digest || second.Outcome != first.Outcome || !second.ResolvedAt.Time().Equal(first.ResolvedAt.Time()) {
			t.Fatalf("duplicate wake did not replay the original resolution: first=%+v second=%+v", first, second)
		}
	})

	t.Run("RED_dst_ambiguous_local_time_refused_not_guessed", func(t *testing.T) {
		// 2026-11-01 01:30 happens twice in America/New_York.
		foldDate := mustLocalDate(t, "2026-11-01")
		node := wait.CompiledWaitNode{
			WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: "wait.effective_date",
			WakeLocalDate: foldDate, WakeLocalTime: mustLocalTime(t, 1, 30),
			Disambiguation: values.DisambiguationRejectGap,
			Zone:           testZone, Calendar: testCalendar, Policy: values.ReferenceUpdatePin,
		}
		req, err := wait.ComputeTimerRequirement(node, testDataset)
		if err != nil {
			t.Fatalf("a DST-ambiguous local time must mint a reviewable requirement, not error: %v", err)
		}
		if !req.ReviewRequired {
			t.Fatal("ReviewRequired = false, want true for a DST fold under REJECT_GAP")
		}
		if req.FireAt.IsSet() {
			t.Fatal("a review-required requirement must not carry a guessed fire_at")
		}
		res, err := wait.Resolve(req, mustInstant(t, "2026-11-01T12:00:00Z"), wait.WakeEvent{Kind: wait.EventWake})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Outcome != wait.OutcomeTimerReviewRequired {
			t.Fatalf("Outcome = %s, want TIMER_REVIEW_REQUIRED", res.Outcome)
		}
	})

	t.Run("RED_dst_gap_local_time_refused_not_guessed", func(t *testing.T) {
		// 2026-03-08 02:30 does not exist in America/New_York.
		gapDate := mustLocalDate(t, "2026-03-08")
		node := wait.CompiledWaitNode{
			WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: "wait.effective_date",
			WakeLocalDate: gapDate, WakeLocalTime: mustLocalTime(t, 2, 30),
			Disambiguation: values.DisambiguationRejectGap,
			Zone:           testZone, Calendar: testCalendar, Policy: values.ReferenceUpdatePin,
		}
		req, err := wait.ComputeTimerRequirement(node, testDataset)
		if err != nil {
			t.Fatalf("a DST-gap local time must mint a reviewable requirement, not error: %v", err)
		}
		if !req.ReviewRequired {
			t.Fatal("ReviewRequired = false, want true for a DST gap under REJECT_GAP")
		}
	})

	t.Run("RED_dataset_change_without_declared_policy_cannot_advance", func(t *testing.T) {
		node := localDateNode(t, mustLocalDate(t, "2026-06-17"), values.ReferenceUpdateUnspecified)
		if _, err := wait.ComputeTimerRequirement(node, testDataset); !errors.Is(err, values.ErrReferenceUpdatePolicyRequired) {
			t.Fatalf("ComputeTimerRequirement with no policy error = %v, want ErrReferenceUpdatePolicyRequired", err)
		}
	})

	t.Run("GREEN_fired_timer_reviewed_superseded_cancelled_exactly_once", func(t *testing.T) {
		fireAt := mustInstant(t, "2026-06-17T14:00:00Z")

		t.Run("FIRED", func(t *testing.T) {
			node := fixedInstantNode(t, fireAt)
			req, err := wait.ComputeTimerRequirement(node, testDataset)
			if err != nil {
				t.Fatalf("ComputeTimerRequirement: %v", err)
			}
			res, err := wait.Resolve(req, fireAt, wait.WakeEvent{Kind: wait.EventWake})
			if err != nil || res.Outcome != wait.OutcomeFired {
				t.Fatalf("Resolve = %+v, %v, want FIRED", res, err)
			}
			out := res.ToNodeOutcome(node.NodeID)
			if out.Outcome != workflow.OutcomeSucceeded {
				t.Fatalf("ToNodeOutcome = %+v, want SUCCEEDED route", out)
			}
		})

		t.Run("CANCELLED", func(t *testing.T) {
			node := fixedInstantNode(t, fireAt)
			req, err := wait.ComputeTimerRequirement(node, testDataset)
			if err != nil {
				t.Fatalf("ComputeTimerRequirement: %v", err)
			}
			res, err := wait.Resolve(req, mustInstant(t, "2026-06-17T13:00:00Z"), wait.WakeEvent{Kind: wait.EventCancel, Reason: "workflow cancelled"})
			if err != nil || res.Outcome != wait.OutcomeCancelled {
				t.Fatalf("Resolve = %+v, %v, want CANCELLED", res, err)
			}
			out := res.ToNodeOutcome(node.NodeID)
			if out.Outcome != workflow.Outcome("CANCELLED") {
				t.Fatalf("ToNodeOutcome = %+v, want CANCELLED route", out)
			}
		})

		t.Run("SUPERSEDED", func(t *testing.T) {
			node := fixedInstantNode(t, fireAt)
			req, err := wait.ComputeTimerRequirement(node, testDataset)
			if err != nil {
				t.Fatalf("ComputeTimerRequirement: %v", err)
			}
			res, err := wait.Resolve(req, mustInstant(t, "2026-06-17T13:00:00Z"), wait.WakeEvent{Kind: wait.EventSupersede, Reason: "dataset republished"})
			if err != nil || res.Outcome != wait.OutcomeSuperseded {
				t.Fatalf("Resolve = %+v, %v, want SUPERSEDED", res, err)
			}
			out := res.ToNodeOutcome(node.NodeID)
			if out.Await != frontier.AwaitTimer || out.AwaitRef != req.Digest {
				t.Fatalf("ToNodeOutcome = %+v, want AwaitTimer keyed on the stale requirement digest", out)
			}
		})

		t.Run("exactly_once_across_repeated_wakes", func(t *testing.T) {
			node := fixedInstantNode(t, fireAt)
			req, err := wait.ComputeTimerRequirement(node, testDataset)
			if err != nil {
				t.Fatalf("ComputeTimerRequirement: %v", err)
			}
			first, err := wait.Resolve(req, fireAt, wait.WakeEvent{Kind: wait.EventWake})
			if err != nil {
				t.Fatalf("first wake: %v", err)
			}
			for i := 0; i < 5; i++ {
				got, err := wait.Resolve(req, fireAt, wait.WakeEvent{Kind: wait.EventWake, Prior: &first})
				if err != nil {
					t.Fatalf("replay %d: %v", i, err)
				}
				if got != first {
					t.Fatalf("replay %d = %+v, want the original resolution %+v", i, got, first)
				}
			}
		})

		t.Run("requirement_is_byte_stable_against_a_golden_fixture", func(t *testing.T) {
			node := fixedInstantNode(t, fireAt)
			req, err := wait.ComputeTimerRequirement(node, testDataset)
			if err != nil {
				t.Fatalf("ComputeTimerRequirement: %v", err)
			}
			got, err := req.JSON()
			if err != nil {
				t.Fatalf("JSON: %v", err)
			}
			goldenPath := filepath.Join("testdata", "fixed_instant_requirement_golden.json")
			if *updateGolden {
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					t.Fatalf("writing golden: %v", err)
				}
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("requirement does not match golden fixture.\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	})
}

// TestTodo_WF_STEP_005_Race proves ComputeTimerRequirement and Resolve are
// safe under concurrent use and produce byte-identical results: they are pure
// functions of their arguments, so nothing here should ever race or diverge.
func TestTodo_WF_STEP_005_Race(t *testing.T) {
	t.Parallel()

	fireAt := mustInstant(t, "2026-06-17T14:00:00Z")
	node := fixedInstantNode(t, fireAt)
	wantReq, err := wait.ComputeTimerRequirement(node, testDataset)
	if err != nil {
		t.Fatalf("ComputeTimerRequirement: %v", err)
	}
	wantRes, err := wait.Resolve(wantReq, fireAt, wait.WakeEvent{Kind: wait.EventWake})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	const workers = 32
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				req, err := wait.ComputeTimerRequirement(node, testDataset)
				if err != nil {
					errCh <- err
					return
				}
				if req.Digest != wantReq.Digest {
					errCh <- errors.New("concurrent ComputeTimerRequirement produced a different digest")
					return
				}
				res, err := wait.Resolve(req, fireAt, wait.WakeEvent{Kind: wait.EventWake})
				if err != nil {
					errCh <- err
					return
				}
				if res.Digest != wantRes.Digest || res.Outcome != wantRes.Outcome {
					errCh <- errors.New("concurrent Resolve produced a different resolution")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent use failed: %v", err)
	}
}

// TestTodo_WF_STEP_005_Conformance asserts the two structural conformance
// requirements the gate ruling imposes: the package's own source contains no
// time.Sleep, no time.Now and no goroutine, and every Resolution outcome maps
// onto a frontier.NodeOutcome shape frontier.Advance actually accepts for a
// StepWait node (a declared route outcome, or an Await/Failed marker — never
// an outcome string StepWait's conformance table does not declare).
func TestTodo_WF_STEP_005_Conformance(t *testing.T) {
	t.Run("no_sleep_now_or_goroutines_in_package_source", func(t *testing.T) {
		fset := token.NewFileSet()
		entries, err := os.ReadDir(".")
		if err != nil {
			t.Fatalf("reading package dir: %v", err)
		}
		found := false
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			found = true
			file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", name, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.GoStmt:
					t.Errorf("%s: goroutine launch found; WAIT conformance forbids background execution", name)
				case *ast.SelectorExpr:
					if id, ok := v.X.(*ast.Ident); ok && id.Name == "time" && (v.Sel.Name == "Sleep" || v.Sel.Name == "Now") {
						t.Errorf("%s: time.%s found; WAIT conformance forbids reading the ambient clock or sleeping", name, v.Sel.Name)
					}
				case *ast.ImportSpec:
					if strings.Trim(v.Path.Value, `"`) == "time" {
						t.Errorf("%s: imports \"time\" directly; every temporal value must come through internal/kernel/values", name)
					}
				}
				return true
			})
		}
		if !found {
			t.Fatal("no non-test .go files found in package directory; the scan found nothing to check")
		}
	})

	t.Run("resolution_outcomes_map_onto_declared_stepwait_routes_or_await_failed", func(t *testing.T) {
		conf, ok := workflow.ConformanceFor(workflow.StepWait)
		if !ok {
			t.Fatal("StepWait has no declared conformance table entry")
		}
		declared := map[workflow.Outcome]bool{}
		for _, o := range conf.Outcomes {
			declared[o] = true
		}
		for _, outcome := range []wait.Outcome{wait.OutcomeFired, wait.OutcomeTimerReviewRequired, wait.OutcomeSuperseded, wait.OutcomeCancelled} {
			res := wait.Resolution{RequirementDigest: "irrelevant-for-this-check", Outcome: outcome}
			no := res.ToNodeOutcome("n1")
			switch {
			case no.Outcome != "":
				if !declared[no.Outcome] {
					t.Errorf("%s maps to outcome %q, which StepWait's conformance table does not declare", outcome, no.Outcome)
				}
				if no.Await != frontier.AwaitNone || no.Failed {
					t.Errorf("%s: a completed route outcome must not also set Await or Failed", outcome)
				}
			case no.Await != frontier.AwaitNone:
				if no.Await != frontier.AwaitTimer {
					t.Errorf("%s: a WAIT node may only await a timer, got %s", outcome, no.Await)
				}
			case !no.Failed:
				t.Errorf("%s maps to neither a declared route, an Await, nor Failed; frontier.Advance would reject it", outcome)
			}
		}
	})
}

// TestTodo_WF_STEP_005_Mutation flips one field of an otherwise-identical
// requirement/resolution at a time and proves the digest — and, where it
// matters, the resolved outcome — changes. A digest that survives a mutated
// field is a digest that cannot be trusted as a content identity.
func TestTodo_WF_STEP_005_Mutation(t *testing.T) {
	base := fixedInstantNode(t, mustInstant(t, "2026-06-17T14:00:00Z"))
	baseReq, err := wait.ComputeTimerRequirement(base, testDataset)
	if err != nil {
		t.Fatalf("ComputeTimerRequirement: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(wait.CompiledWaitNode) wait.CompiledWaitNode
	}{
		{"node_id", func(n wait.CompiledWaitNode) wait.CompiledWaitNode { n.NodeID = "wait.other"; return n }},
		{"fire_at", func(n wait.CompiledWaitNode) wait.CompiledWaitNode {
			n.WakeInstant = mustInstant(t, "2026-06-18T14:00:00Z")
			return n
		}},
		{"policy", func(n wait.CompiledWaitNode) wait.CompiledWaitNode {
			n.Policy = values.ReferenceUpdateRecalculate
			return n
		}},
		{"zone", func(n wait.CompiledWaitNode) wait.CompiledWaitNode {
			n.Zone = values.ZoneRef{ID: "UTC", TzdbVersion: "2026a"}
			return n
		}},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			mutated := m.mutate(base)
			req, err := wait.ComputeTimerRequirement(mutated, testDataset)
			if err != nil {
				t.Fatalf("ComputeTimerRequirement(mutated): %v", err)
			}
			if req.Digest == baseReq.Digest {
				t.Fatalf("mutating %s did not change the requirement digest", m.name)
			}
		})
	}

	t.Run("dataset_mutation_changes_digest", func(t *testing.T) {
		other := values.DatasetVersions{TzdbVersion: "2027b", CalendarVersion: "2027.1"}
		req, err := wait.ComputeTimerRequirement(base, other)
		if err != nil {
			t.Fatalf("ComputeTimerRequirement(other dataset): %v", err)
		}
		if req.Digest == baseReq.Digest {
			t.Fatal("mutating the dataset versions did not change the requirement digest")
		}
	})

	t.Run("resolution_outcome_mutation_changes_digest", func(t *testing.T) {
		fireAt := mustInstant(t, "2026-06-17T14:00:00Z")
		fired, err := wait.Resolve(baseReq, fireAt, wait.WakeEvent{Kind: wait.EventWake})
		if err != nil {
			t.Fatalf("Resolve(FIRED): %v", err)
		}
		cancelled, err := wait.Resolve(baseReq, fireAt, wait.WakeEvent{Kind: wait.EventCancel})
		if err != nil {
			t.Fatalf("Resolve(CANCELLED): %v", err)
		}
		if fired.Digest == cancelled.Digest {
			t.Fatal("FIRED and CANCELLED resolutions of the same requirement/time produced the same digest")
		}
	})
}

// TestTodo_WF_STEP_005_Fault proves malformed or impossible inputs are
// refused with typed errors rather than panicking or silently producing a
// requirement/resolution.
func TestTodo_WF_STEP_005_Fault(t *testing.T) {
	t.Run("missing_workflow_identity", func(t *testing.T) {
		node := fixedInstantNode(t, mustInstant(t, "2026-06-17T14:00:00Z"))
		node.WorkflowID = ""
		if _, err := wait.ComputeTimerRequirement(node, testDataset); !errors.Is(err, wait.ErrWorkflowIdentityRequired) {
			t.Fatalf("error = %v, want ErrWorkflowIdentityRequired", err)
		}
	})

	t.Run("no_wake_condition", func(t *testing.T) {
		node := fixedInstantNode(t, mustInstant(t, "2026-06-17T14:00:00Z"))
		node.WakeInstant = values.Instant{}
		if _, err := wait.ComputeTimerRequirement(node, testDataset); !errors.Is(err, wait.ErrNoWakeCondition) {
			t.Fatalf("error = %v, want ErrNoWakeCondition", err)
		}
	})

	t.Run("conflicting_wake_conditions", func(t *testing.T) {
		node := fixedInstantNode(t, mustInstant(t, "2026-06-17T14:00:00Z"))
		node.WakeLocalDate = mustLocalDate(t, "2026-06-17")
		if _, err := wait.ComputeTimerRequirement(node, testDataset); !errors.Is(err, wait.ErrConflictingWakeCondition) {
			t.Fatalf("error = %v, want ErrConflictingWakeCondition", err)
		}
	})

	t.Run("unloadable_zone", func(t *testing.T) {
		node := fixedInstantNode(t, mustInstant(t, "2026-06-17T14:00:00Z"))
		node.Zone = values.ZoneRef{ID: "Not/AZone", TzdbVersion: "2026a"}
		if _, err := wait.ComputeTimerRequirement(node, testDataset); !errors.Is(err, values.ErrUnknownZone) {
			t.Fatalf("error = %v, want ErrUnknownZone", err)
		}
	})

	t.Run("invalid_dataset", func(t *testing.T) {
		node := fixedInstantNode(t, mustInstant(t, "2026-06-17T14:00:00Z"))
		if _, err := wait.ComputeTimerRequirement(node, values.DatasetVersions{}); !errors.Is(err, values.ErrDatasetVersionRequired) {
			t.Fatalf("error = %v, want ErrDatasetVersionRequired", err)
		}
	})

	t.Run("resolve_zero_value_requirement", func(t *testing.T) {
		if _, err := wait.Resolve(wait.TimerRequirement{}, mustInstant(t, "2026-06-17T14:00:00Z"), wait.WakeEvent{Kind: wait.EventWake}); !errors.Is(err, wait.ErrInvalidRequirement) {
			t.Fatalf("error = %v, want ErrInvalidRequirement", err)
		}
	})

	t.Run("resolve_wake_without_now", func(t *testing.T) {
		node := fixedInstantNode(t, mustInstant(t, "2026-06-17T14:00:00Z"))
		req, err := wait.ComputeTimerRequirement(node, testDataset)
		if err != nil {
			t.Fatalf("ComputeTimerRequirement: %v", err)
		}
		if _, err := wait.Resolve(req, values.Instant{}, wait.WakeEvent{Kind: wait.EventWake}); !errors.Is(err, wait.ErrNowRequired) {
			t.Fatalf("error = %v, want ErrNowRequired", err)
		}
	})

	t.Run("resolve_prior_from_a_different_requirement", func(t *testing.T) {
		nodeA := fixedInstantNode(t, mustInstant(t, "2026-06-17T14:00:00Z"))
		nodeB := fixedInstantNode(t, mustInstant(t, "2026-06-18T14:00:00Z"))
		reqA, err := wait.ComputeTimerRequirement(nodeA, testDataset)
		if err != nil {
			t.Fatalf("ComputeTimerRequirement A: %v", err)
		}
		reqB, err := wait.ComputeTimerRequirement(nodeB, testDataset)
		if err != nil {
			t.Fatalf("ComputeTimerRequirement B: %v", err)
		}
		resB, err := wait.Resolve(reqB, mustInstant(t, "2026-06-18T14:00:00Z"), wait.WakeEvent{Kind: wait.EventWake})
		if err != nil {
			t.Fatalf("Resolve B: %v", err)
		}
		if _, err := wait.Resolve(reqA, mustInstant(t, "2026-06-17T14:00:00Z"), wait.WakeEvent{Kind: wait.EventWake, Prior: &resB}); !errors.Is(err, wait.ErrDigestMismatch) {
			t.Fatalf("error = %v, want ErrDigestMismatch", err)
		}
	})

	t.Run("resolve_unknown_event_kind", func(t *testing.T) {
		node := fixedInstantNode(t, mustInstant(t, "2026-06-17T14:00:00Z"))
		req, err := wait.ComputeTimerRequirement(node, testDataset)
		if err != nil {
			t.Fatalf("ComputeTimerRequirement: %v", err)
		}
		if _, err := wait.Resolve(req, mustInstant(t, "2026-06-17T14:00:00Z"), wait.WakeEvent{Kind: "BOGUS"}); !errors.Is(err, wait.ErrUnknownEventKind) {
			t.Fatalf("error = %v, want ErrUnknownEventKind", err)
		}
	})
}
