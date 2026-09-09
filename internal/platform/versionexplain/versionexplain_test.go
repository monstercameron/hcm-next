package versionexplain_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	ve "github.com/monstercameron/human-capital-management-suite/internal/platform/versionexplain"
)

var (
	t0    = time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	known = values.NewInstant(t0)

	effective = values.NewInstant(t0.Add(5 * time.Minute))
	resolveAt = values.NewInstant(t0.Add(10 * time.Minute))
)

// consistentSnapshot is the GREEN fixture: three cohorts on one epoch head,
// one default, named membership, one override.
func consistentSnapshot() ve.Snapshot {
	return ve.Snapshot{
		TargetID: "svc-payroll",
		Epoch:    7,
		Cohorts: map[string]ve.CohortState{
			"pilot":   {Stage: ve.StagePilot, Bundle: "payroll-2026.09-rc1", Epoch: 7, Receipt: "rc:pilot:7"},
			"canary":  {Stage: ve.StageCanary, Bundle: "payroll-2026.09-rc2", Epoch: 7, Receipt: "rc:canary:7"},
			"general": {Stage: ve.StageStaged, Bundle: "payroll-2026.09", Epoch: 7, Receipt: "rc:general:7"},
		},
		Default: "general",
		Membership: map[string]string{
			"emp-1": "pilot",
			"emp-2": "canary",
			"emp-3": "general",
		},
		Overrides: map[string]ve.Override{
			"emp-9": {CohortID: "canary"},
		},
		KnownAt:     known,
		EffectiveAt: effective,
	}
}

type recordingSink struct {
	mu   sync.Mutex
	recs []ve.Explanation
}

func (s *recordingSink) Record(e ve.Explanation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append(s.recs, e)
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.recs)
}

func (s *recordingSink) last() ve.Explanation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.recs) == 0 {
		return ve.Explanation{}
	}
	return s.recs[len(s.recs)-1]
}

func mustExplanation(t *testing.T, sink *recordingSink, snap ve.Snapshot, subject string) ve.Explanation {
	t.Helper()
	explainer := ve.NewExplainer(sink)
	expl, err := explainer.Explain(snap, subject, resolveAt)
	if err != nil {
		t.Fatalf("Explain(%s): %v", subject, err)
	}
	return expl
}

// TestTodo_ROLLOUT_007 is the PRIMARY clause: a consistent snapshot resolves
// each subject to its target, cohort, stage, bundle, epoch and receipt with
// the override and kill state, at the snapshot's known and effective time;
// every seeded inconsistency is rejected as ROLLOUT_007_REJECTED with the
// offending field, state and version, and records nothing; and a resolution
// never exposes the membership of any cohort but the resolved subject's own.
func TestTodo_ROLLOUT_007(t *testing.T) {
	sink := &recordingSink{}
	snap := consistentSnapshot()

	t.Run("membership resolves the subject's own cohort", func(t *testing.T) {
		expl := mustExplanation(t, sink, snap, "emp-1")
		want := ve.Explanation{
			Subject:     "emp-1",
			TargetID:    "svc-payroll",
			CohortID:    "pilot",
			Stage:       ve.StagePilot,
			Bundle:      "payroll-2026.09-rc1",
			Epoch:       7,
			Receipt:     "rc:pilot:7",
			Override:    false,
			Pause:       false,
			Kill:        false,
			KnownAt:     known,
			EffectiveAt: effective,
		}
		if expl != want {
			t.Fatalf("got %+v, want %+v", expl, want)
		}
		if sink.last() != expl {
			t.Fatalf("the recorded resolution differs from the returned one: %+v vs %+v", sink.last(), expl)
		}
	})

	t.Run("an override beats membership", func(t *testing.T) {
		expl := mustExplanation(t, sink, snap, "emp-9")
		if expl.CohortID != "canary" || expl.Override != true || expl.Stage != ve.StageCanary {
			t.Fatalf("override not honored: %+v", expl)
		}
	})

	t.Run("an unknown subject falls into the default like any other non-member", func(t *testing.T) {
		expl := mustExplanation(t, sink, snap, "emp-never-seen")
		if expl.CohortID != "general" || expl.Stage != ve.StageStaged {
			t.Fatalf("default fallback wrong: %+v", expl)
		}
	})

	t.Run("a resolution never exposes other cohort membership", func(t *testing.T) {
		expl := mustExplanation(t, sink, snap, "emp-1")
		raw, err := json.Marshal(expl)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		for _, leaked := range []string{"canary", "general", "emp-2", "emp-3"} {
			if contains(raw, leaked) {
				t.Fatalf("resolution for emp-1 exposes %q: %s", leaked, raw)
			}
		}
		if len(snap.CohortIDs()) != 3 {
			t.Fatalf("fixture sanity: %v", snap.CohortIDs())
		}
	})

	t.Run("the kill state is one state for every subject", func(t *testing.T) {
		killed := snap
		killed.KillSwitch = true
		first := mustExplanation(t, sink, killed, "emp-1")
		second := mustExplanation(t, sink, killed, "emp-2")
		if first.Stage != ve.StageKilled || first.Kill != true || first.Pause != true {
			t.Fatalf("emp-1 not in the kill state: %+v", first)
		}
		if second.Stage != ve.StageKilled || second.Kill != true {
			t.Fatalf("emp-2 not in the kill state: %+v", second)
		}
		if first.Bundle != "payroll-2026.09-rc1" || second.Bundle != "payroll-2026.09-rc2" {
			t.Fatalf("kill state lost the subjects' bundles: %+v / %+v", first, second)
		}
	})
}

func TestTodo_ROLLOUT_007_Rejects(t *testing.T) {
	sink := &recordingSink{}
	explainer := ve.NewExplainer(sink)

	type rejectCase struct {
		name  string
		mut   func(*ve.Snapshot)
		field string
	}
	cases := []rejectCase{
		{
			name:  "an override points at a cohort the snapshot does not contain",
			mut:   func(s *ve.Snapshot) { s.Overrides["emp-9"] = ve.Override{CohortID: "ghost"} },
			field: "override.emp-9",
		},
		{
			name:  "the default cohort is not in the cohort set",
			mut:   func(s *ve.Snapshot) { delete(s.Cohorts, "general") },
			field: "default_cohort",
		},
		{
			name: "a cohort epoch does not match the snapshot head",
			mut: func(s *ve.Snapshot) {
				row := s.Cohorts["canary"]
				row.Epoch = 8
				s.Cohorts["canary"] = row
			},
			field: "cohort.canary.epoch",
		},
		{
			name: "a live pause also carries a scheduled advance",
			mut: func(s *ve.Snapshot) {
				s.Pause = true
				s.Advance = &ve.Advance{Stage: ve.StageStaged, At: values.NewInstant(t0.Add(time.Hour))}
			},
			field: "pause",
		},
		{
			name: "a cohort row is missing its bundle",
			mut: func(s *ve.Snapshot) {
				row := s.Cohorts["pilot"]
				row.Bundle = ""
				s.Cohorts["pilot"] = row
			},
			field: "cohort.pilot.bundle",
		},
		{
			name: "a cohort row names an unknown stage",
			mut: func(s *ve.Snapshot) {
				row := s.Cohorts["pilot"]
				row.Stage = ve.Stage("warp")
				s.Cohorts["pilot"] = row
			},
			field: "cohort.pilot.stage",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := consistentSnapshot()
			tc.mut(&snap)
			before := sink.count()
			_, err := explainer.Explain(snap, "emp-1", resolveAt)
			var rej *ve.Rejection
			if !errors.As(err, &rej) {
				t.Fatalf("got %v, want a *Rejection", err)
			}
			if !errors.Is(err, ve.ErrRejected) {
				t.Fatalf("rejection does not wrap the ROLLOUT_007 token: %v", err)
			}
			if rej.Field != tc.field {
				t.Fatalf("rejection names field %q, want %q (state=%s version=%s)", rej.Field, tc.field, rej.State, rej.Version)
			}
			if sink.count() != before {
				t.Fatalf("a rejected resolution recorded %d row(s)", sink.count()-before)
			}
		})
	}

	t.Run("a resolution before the effective time is rejected", func(t *testing.T) {
		snap := consistentSnapshot()
		var rej *ve.Rejection
		_, err := explainer.Explain(snap, "emp-1", known)
		if !errors.As(err, &rej) || rej.Field != "resolve_time" {
			t.Fatalf("got %v, want resolve_time rejection", err)
		}
	})

	t.Run("a membership that points outside the snapshot is rejected", func(t *testing.T) {
		snap := consistentSnapshot()
		snap.Membership["emp-5"] = "ghost"
		var rej *ve.Rejection
		_, err := explainer.Explain(snap, "emp-5", resolveAt)
		if !errors.As(err, &rej) || rej.Field != "membership.emp-5" {
			t.Fatalf("got %v, want membership.emp-5 rejection", err)
		}
	})
}

// TestTodo_ROLLOUT_007_Mutation is the MUTATION clause: mutating one part of
// a consistent snapshot - or resnapshotting after a fix - never changes the
// resolved row of an unaffected subject, and the same snapshot resolved
// twice gives the same resolution.
func TestTodo_ROLLOUT_007_Mutation(t *testing.T) {
	sink := &recordingSink{}
	baseline := mustExplanation(t, sink, consistentSnapshot(), "emp-1")

	t.Run("mutating an unrelated cohort does not move emp-1", func(t *testing.T) {
		snap := consistentSnapshot()
		row := snap.Cohorts["canary"]
		row.Bundle = "payroll-2026.09-rc2.1"
		row.Receipt = "rc:canary:7.1"
		snap.Cohorts["canary"] = row
		expl := mustExplanation(t, sink, snap, "emp-1")
		if expl != baseline {
			t.Fatalf("emp-1 moved under a canary mutation:\n got %+v\nwant %+v", expl, baseline)
		}
	})

	t.Run("the same snapshot resolved twice resolves identically", func(t *testing.T) {
		snap := consistentSnapshot()
		first := mustExplanation(t, sink, snap, "emp-2")
		second := mustExplanation(t, sink, snap, "emp-2")
		if first != second {
			t.Fatalf("resolution is not deterministic:\n got %+v\nwant %+v", second, first)
		}
	})

	t.Run("a fixed snapshot resolves to the same values as the baseline", func(t *testing.T) {
		snap := consistentSnapshot()
		snap.Overrides["emp-9"] = ve.Override{CohortID: "ghost"}
		explainer := ve.NewExplainer(sink)
		if _, err := explainer.Explain(snap, "emp-1", resolveAt); err == nil {
			t.Fatal("a broken snapshot resolved")
		}
		snap.Overrides["emp-9"] = ve.Override{CohortID: "canary"}
		expl, err := explainer.Explain(snap, "emp-1", resolveAt)
		if err != nil {
			t.Fatalf("fixed snapshot does not resolve: %v", err)
		}
		if expl != baseline {
			t.Fatalf("fixed snapshot resolves differently:\n got %+v\nwant %+v", expl, baseline)
		}
	})
}

func contains(raw []byte, needle string) bool {
	needleBytes := []byte(`"` + needle + `"`)
	for i := 0; i+len(needleBytes) <= len(raw); i++ {
		match := true
		for j := 0; j < len(needleBytes); j++ {
			if raw[i+j] != needleBytes[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
