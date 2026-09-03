package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
)

func validTimerReference() TimerReference {
	return TimerReference{
		Zone:     testZone,
		Calendar: testCalendar,
		Policy:   ReferenceUpdatePin,
	}
}

// TestTimerDatasetChangePolicy is the primary test for MODEL-005. It proves a
// future timer must declare its timezone, tzdb version, business calendar
// version and dataset-update policy, and that a dataset refresh never silently
// rewrites an existing deadline.
func TestTimerDatasetChangePolicy(t *testing.T) {
	t.Parallel()

	t.Run("FutureTimerMustCarryDatasetVersionsAndPolicy", func(t *testing.T) {
		cases := []struct {
			name string
			ref  TimerReference
			want error
		}{
			{
				name: "no zone",
				ref:  TimerReference{Calendar: testCalendar, Policy: ReferenceUpdatePin},
				want: ErrZoneRequired,
			},
			{
				name: "no tzdb version",
				ref: TimerReference{
					Zone:     ZoneRef{ID: "America/New_York"},
					Calendar: testCalendar,
					Policy:   ReferenceUpdatePin,
				},
				want: ErrTzdbVersionRequired,
			},
			{
				name: "unknown zone",
				ref: TimerReference{
					Zone:     ZoneRef{ID: "Mars/Olympus", TzdbVersion: "2026a"},
					Calendar: testCalendar,
					Policy:   ReferenceUpdatePin,
				},
				want: ErrUnknownZone,
			},
			{
				name: "no calendar",
				ref:  TimerReference{Zone: testZone, Policy: ReferenceUpdatePin},
				want: ErrCalendarRequired,
			},
			{
				name: "no calendar version",
				ref: TimerReference{
					Zone:     testZone,
					Calendar: CalendarRef{Ref: "us-federal"},
					Policy:   ReferenceUpdatePin,
				},
				want: ErrCalendarVersionRequired,
			},
			{
				name: "no policy",
				ref:  TimerReference{Zone: testZone, Calendar: testCalendar},
				want: ErrReferenceUpdatePolicyRequired,
			},
			{
				name: "unknown policy",
				ref:  TimerReference{Zone: testZone, Calendar: testCalendar, Policy: ReferenceUpdatePolicy(200)},
				want: ErrReferenceUpdatePolicy,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if err := tc.ref.Validate(); !errors.Is(err, tc.want) {
					t.Fatalf("Validate() = %v, want %v", err, tc.want)
				}
				if got := tc.ref.Canonical(); got != nil {
					t.Fatalf("Canonical() = %q, want nil for an invalid reference", got)
				}
			})
		}
		if err := validTimerReference().Validate(); err != nil {
			t.Fatalf("valid reference Validate() = %v", err)
		}
	})

	t.Run("PolicyValuesAreExactlyThree", func(t *testing.T) {
		want := []string{"PIN", "RECALCULATE", "REVIEW_REQUIRED"}
		got := AllReferenceUpdatePolicies()
		if len(got) != len(want) {
			t.Fatalf("policies = %v, want %v", got, want)
		}
		for i, policy := range got {
			if policy.String() != want[i] {
				t.Fatalf("policy %d = %q, want %q", i, policy.String(), want[i])
			}
			parsed, err := ParseReferenceUpdatePolicy(want[i])
			if err != nil || parsed != policy {
				t.Fatalf("ParseReferenceUpdatePolicy(%q) = %v, %v", want[i], parsed, err)
			}
		}
		if _, err := ParseReferenceUpdatePolicy("RECALCULATE_SILENTLY"); !errors.Is(err, ErrReferenceUpdatePolicy) {
			t.Fatalf("unknown policy token = %v, want ErrReferenceUpdatePolicy", err)
		}
		if _, err := ParseReferenceUpdatePolicy(ReferenceUpdateUnspecified.String()); err == nil {
			t.Fatal("the unspecified token parsed as a policy")
		}
	})

	t.Run("PinReplaysWithTheHistoricalDataset", func(t *testing.T) {
		ref := validTimerReference()
		pinned := MustInstant(t, "2026-11-01T05:30:00Z")
		current := DatasetVersions{TzdbVersion: "2027b", CalendarVersion: "2027.1"}

		var asked []DatasetVersions
		resolve := func(ds DatasetVersions) (Instant, error) {
			asked = append(asked, ds)
			if ds.TzdbVersion == "2026a" {
				return pinned, nil
			}
			return MustInstant(t, "2026-11-01T06:30:00Z"), nil
		}

		outcome, err := ReplayTimer(ref, pinned, current, resolve)
		if err != nil {
			t.Fatalf("ReplayTimer error = %v", err)
		}
		if len(asked) != 2 {
			t.Fatalf("resolver called %d times, want 2 (historical and current)", len(asked))
		}
		if asked[0] != (DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}) {
			t.Fatalf("first resolve used %+v, want the historical dataset", asked[0])
		}
		if asked[1] != current {
			t.Fatalf("second resolve used %+v, want the current dataset", asked[1])
		}
		if outcome.Effective != pinned {
			t.Fatalf("PIN effective = %s, want the pinned deadline %s", outcome.Effective, pinned)
		}
		if !outcome.DatasetChanged {
			t.Fatal("PIN outcome did not record that the dataset changed")
		}
		if !outcome.WouldChange {
			t.Fatal("PIN outcome did not record that recomputation would move the deadline")
		}
		if outcome.ReviewRequired {
			t.Fatal("PIN outcome demanded review")
		}
		// The evidence records both datasets and both answers.
		if outcome.Evidence.HistoricalDataset.TzdbVersion != "2026a" ||
			outcome.Evidence.CurrentDataset != current ||
			outcome.Evidence.Pinned != pinned ||
			outcome.Evidence.Recomputed != MustInstant(t, "2026-11-01T06:30:00Z") {
			t.Fatalf("evidence = %+v", outcome.Evidence)
		}
		if len(outcome.Evidence.Canonical()) == 0 {
			t.Fatal("evidence produced no canonical bytes")
		}
	})

	t.Run("RecalculateAdoptsTheCurrentDataset", func(t *testing.T) {
		ref := validTimerReference()
		ref.Policy = ReferenceUpdateRecalculate
		pinned := MustInstant(t, "2026-11-01T05:30:00Z")
		recomputed := MustInstant(t, "2026-11-01T06:30:00Z")
		current := DatasetVersions{TzdbVersion: "2027b", CalendarVersion: "2027.1"}

		outcome, err := ReplayTimer(ref, pinned, current, func(ds DatasetVersions) (Instant, error) {
			if ds == current {
				return recomputed, nil
			}
			return pinned, nil
		})
		if err != nil {
			t.Fatalf("ReplayTimer error = %v", err)
		}
		if outcome.Effective != recomputed {
			t.Fatalf("RECALCULATE effective = %s, want %s", outcome.Effective, recomputed)
		}
		if outcome.ReviewRequired {
			t.Fatal("RECALCULATE demanded review")
		}
		if !outcome.WouldChange {
			t.Fatal("RECALCULATE did not record the change")
		}
	})

	t.Run("ReviewRequiredHoldsTheDeadlineAndFlags", func(t *testing.T) {
		ref := validTimerReference()
		ref.Policy = ReferenceUpdateReviewRequired
		pinned := MustInstant(t, "2026-11-01T05:30:00Z")
		recomputed := MustInstant(t, "2026-11-01T06:30:00Z")
		current := DatasetVersions{TzdbVersion: "2027b", CalendarVersion: "2027.1"}

		outcome, err := ReplayTimer(ref, pinned, current, func(ds DatasetVersions) (Instant, error) {
			if ds == current {
				return recomputed, nil
			}
			return pinned, nil
		})
		if err != nil {
			t.Fatalf("ReplayTimer error = %v", err)
		}
		if outcome.Effective != pinned {
			t.Fatalf("REVIEW_REQUIRED effective = %s, want the untouched deadline %s", outcome.Effective, pinned)
		}
		if !outcome.ReviewRequired {
			t.Fatal("REVIEW_REQUIRED did not demand review after a change")
		}

		// No change means no review.
		steady, err := ReplayTimer(ref, pinned, current, func(DatasetVersions) (Instant, error) {
			return pinned, nil
		})
		if err != nil {
			t.Fatalf("ReplayTimer error = %v", err)
		}
		if steady.ReviewRequired || steady.WouldChange {
			t.Fatalf("unchanged deadline demanded review: %+v", steady)
		}
	})

	t.Run("DatasetRefreshNeverSilentlyRewritesADeadline", func(t *testing.T) {
		pinned := MustInstant(t, "2026-11-01T05:30:00Z")
		recomputed := MustInstant(t, "2026-11-01T06:30:00Z")
		current := DatasetVersions{TzdbVersion: "2027b", CalendarVersion: "2027.1"}
		resolve := func(ds DatasetVersions) (Instant, error) {
			if ds == current {
				return recomputed, nil
			}
			return pinned, nil
		}
		for _, policy := range AllReferenceUpdatePolicies() {
			ref := validTimerReference()
			ref.Policy = policy
			outcome, err := ReplayTimer(ref, pinned, current, resolve)
			if err != nil {
				t.Fatalf("%s ReplayTimer error = %v", policy, err)
			}
			// Either the deadline stands, or the move is explicit: a moved
			// deadline only ever comes from RECALCULATE.
			if outcome.Effective != pinned && policy != ReferenceUpdateRecalculate {
				t.Fatalf("%s moved the deadline to %s without RECALCULATE", policy, outcome.Effective)
			}
			if outcome.Effective != pinned && !outcome.WouldChange {
				t.Fatalf("%s moved the deadline without recording the change", policy)
			}
			if outcome.Policy != policy {
				t.Fatalf("outcome policy = %s, want %s", outcome.Policy, policy)
			}
		}
	})

	t.Run("ReplayRejectsBadInput", func(t *testing.T) {
		ref := validTimerReference()
		pinned := MustInstant(t, "2026-11-01T05:30:00Z")
		current := DatasetVersions{TzdbVersion: "2027b", CalendarVersion: "2027.1"}
		resolve := func(DatasetVersions) (Instant, error) { return pinned, nil }

		if _, err := ReplayTimer(TimerReference{}, pinned, current, resolve); err == nil {
			t.Fatal("replay accepted an invalid timer reference")
		}
		if _, err := ReplayTimer(ref, Instant{}, current, resolve); !errors.Is(err, ErrInstantUnset) {
			t.Fatalf("replay with an unset deadline = %v, want ErrInstantUnset", err)
		}
		if _, err := ReplayTimer(ref, pinned, DatasetVersions{}, resolve); !errors.Is(err, ErrDatasetVersionRequired) {
			t.Fatalf("replay with an unversioned dataset = %v, want ErrDatasetVersionRequired", err)
		}
		if _, err := ReplayTimer(ref, pinned, current, nil); !errors.Is(err, ErrResolverRequired) {
			t.Fatalf("replay without a resolver = %v, want ErrResolverRequired", err)
		}
	})
}

// TestTodo_MODEL_005_Property asserts replay invariants across every policy and
// both the changed and unchanged dataset cases.
func TestTodo_MODEL_005_Property(t *testing.T) {
	t.Parallel()

	pinned := MustInstant(t, "2026-11-01T05:30:00Z")
	moved := MustInstant(t, "2026-11-01T06:30:00Z")
	historical := DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}

	for _, policy := range AllReferenceUpdatePolicies() {
		for _, changed := range []bool{false, true} {
			current := historical
			if changed {
				current = DatasetVersions{TzdbVersion: "2027b", CalendarVersion: "2027.1"}
			}
			ref := validTimerReference()
			ref.Policy = policy
			outcome, err := ReplayTimer(ref, pinned, current, func(ds DatasetVersions) (Instant, error) {
				if changed && ds == current {
					return moved, nil
				}
				return pinned, nil
			})
			if err != nil {
				t.Fatalf("%s/%v ReplayTimer error = %v", policy, changed, err)
			}
			// Invariant: the effective deadline is always one of the two
			// computed answers, never a third value.
			if outcome.Effective != pinned && outcome.Effective != moved {
				t.Fatalf("%s/%v invented a deadline %s", policy, changed, outcome.Effective)
			}
			// Invariant: an unchanged dataset never moves anything and never
			// demands review.
			if !changed && (outcome.Effective != pinned || outcome.ReviewRequired || outcome.WouldChange) {
				t.Fatalf("%s reacted to an unchanged dataset: %+v", policy, outcome)
			}
			// Invariant: the evidence always names both datasets.
			if outcome.Evidence.HistoricalDataset != historical || outcome.Evidence.CurrentDataset != current {
				t.Fatalf("%s/%v evidence datasets = %+v", policy, changed, outcome.Evidence)
			}
			// Invariant: replay is deterministic.
			again, err := ReplayTimer(ref, pinned, current, func(ds DatasetVersions) (Instant, error) {
				if changed && ds == current {
					return moved, nil
				}
				return pinned, nil
			})
			if err != nil {
				t.Fatalf("%s/%v second ReplayTimer error = %v", policy, changed, err)
			}
			if !bytes.Equal(outcome.Evidence.Canonical(), again.Evidence.Canonical()) {
				t.Fatalf("%s/%v replay is not deterministic", policy, changed)
			}
		}
	}
}

// TestTodo_MODEL_005_Golden pins timer-reference and replay vectors.
func TestTodo_MODEL_005_Golden(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/model_005_timers.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		Replays []struct {
			Policy          string `json:"policy"`
			Pinned          string `json:"pinned"`
			Recomputed      string `json:"recomputed"`
			CurrentTzdb     string `json:"current_tzdb_version"`
			CurrentCalendar string `json:"current_calendar_version"`
			WantEffective   string `json:"want_effective"`
			WantReview      bool   `json:"want_review_required"`
			WantWouldChange bool   `json:"want_would_change"`
		} `json:"replays"`
		References []struct {
			Zone      string `json:"zone"`
			Tzdb      string `json:"tzdb_version"`
			Calendar  string `json:"calendar_ref"`
			Version   string `json:"calendar_version"`
			Policy    string `json:"policy"`
			WantValid bool   `json:"want_valid"`
		} `json:"references"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}

	for _, tc := range golden.Replays {
		policy, err := ParseReferenceUpdatePolicy(tc.Policy)
		if err != nil {
			t.Errorf("ParseReferenceUpdatePolicy(%q) error = %v", tc.Policy, err)
			continue
		}
		ref := validTimerReference()
		ref.Policy = policy
		pinned := MustInstant(t, tc.Pinned)
		recomputed := MustInstant(t, tc.Recomputed)
		current := DatasetVersions{TzdbVersion: tc.CurrentTzdb, CalendarVersion: tc.CurrentCalendar}
		outcome, err := ReplayTimer(ref, pinned, current, func(ds DatasetVersions) (Instant, error) {
			if ds == current {
				return recomputed, nil
			}
			return pinned, nil
		})
		if err != nil {
			t.Errorf("%s ReplayTimer error = %v", tc.Policy, err)
			continue
		}
		if got := outcome.Effective.String(); got != tc.WantEffective {
			t.Errorf("%s effective = %q, want %q", tc.Policy, got, tc.WantEffective)
		}
		if outcome.ReviewRequired != tc.WantReview {
			t.Errorf("%s review = %v, want %v", tc.Policy, outcome.ReviewRequired, tc.WantReview)
		}
		if outcome.WouldChange != tc.WantWouldChange {
			t.Errorf("%s wouldChange = %v, want %v", tc.Policy, outcome.WouldChange, tc.WantWouldChange)
		}
	}
	for _, tc := range golden.References {
		policy := ReferenceUpdateUnspecified
		if tc.Policy != "" {
			parsed, err := ParseReferenceUpdatePolicy(tc.Policy)
			if err != nil && tc.WantValid {
				t.Errorf("ParseReferenceUpdatePolicy(%q) error = %v", tc.Policy, err)
				continue
			}
			policy = parsed
		}
		ref := TimerReference{
			Zone:     ZoneRef{ID: tc.Zone, TzdbVersion: tc.Tzdb},
			Calendar: CalendarRef{Ref: tc.Calendar, Version: tc.Version},
			Policy:   policy,
		}
		err := ref.Validate()
		if tc.WantValid != (err == nil) {
			t.Errorf("reference %+v Validate() = %v, want valid=%v", tc, err, tc.WantValid)
		}
	}
}

// TestTodo_MODEL_005_Recovery proves that when the current dataset can no
// longer resolve a timer, a PIN policy still recovers the historical deadline
// instead of losing it.
func TestTodo_MODEL_005_Recovery(t *testing.T) {
	t.Parallel()

	ref := validTimerReference()
	pinned := MustInstant(t, "2026-11-01T05:30:00Z")
	current := DatasetVersions{TzdbVersion: "2027b", CalendarVersion: "2027.1"}
	boom := errors.New("calendar dataset 2027.1 dropped the region")

	resolve := func(ds DatasetVersions) (Instant, error) {
		if ds == current {
			return Instant{}, boom
		}
		return pinned, nil
	}

	outcome, err := ReplayTimer(ref, pinned, current, resolve)
	if err != nil {
		t.Fatalf("PIN replay error = %v, want recovery", err)
	}
	if outcome.Effective != pinned {
		t.Fatalf("recovered effective = %s, want %s", outcome.Effective, pinned)
	}
	if outcome.Evidence.RecomputeError == "" {
		t.Fatal("recovery did not record why recomputation failed")
	}
	if outcome.WouldChange {
		t.Fatal("a failed recomputation must not be reported as a change")
	}

	// The other two policies cannot pretend to know the new answer.
	for _, policy := range []ReferenceUpdatePolicy{ReferenceUpdateRecalculate, ReferenceUpdateReviewRequired} {
		bad := ref
		bad.Policy = policy
		out, err := ReplayTimer(bad, pinned, current, resolve)
		if policy == ReferenceUpdateRecalculate {
			if !errors.Is(err, boom) {
				t.Fatalf("RECALCULATE hid the resolver failure: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("REVIEW_REQUIRED replay error = %v", err)
		}
		if out.Effective != pinned || !out.ReviewRequired {
			t.Fatalf("REVIEW_REQUIRED recovery = %+v", out)
		}
	}

	// A historical replay that itself fails is fatal: there is nothing to
	// fall back to.
	if _, err := ReplayTimer(ref, pinned, current, func(DatasetVersions) (Instant, error) {
		return Instant{}, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("failed historical replay = %v, want the resolver error", err)
	}
}

// TestTodo_MODEL_005_Race proves replay holds no shared mutable state.
func TestTodo_MODEL_005_Race(t *testing.T) {
	t.Parallel()

	ref := validTimerReference()
	pinned := MustInstant(t, "2026-11-01T05:30:00Z")
	moved := MustInstant(t, "2026-11-01T06:30:00Z")
	current := DatasetVersions{TzdbVersion: "2027b", CalendarVersion: "2027.1"}
	resolve := func(ds DatasetVersions) (Instant, error) {
		if ds == current {
			return moved, nil
		}
		return pinned, nil
	}
	want, err := ReplayTimer(ref, pinned, current, resolve)
	if err != nil {
		t.Fatalf("ReplayTimer error = %v", err)
	}
	wantBytes := want.Evidence.Canonical()

	const workers = 32
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				got, err := ReplayTimer(ref, pinned, current, resolve)
				if err != nil {
					errCh <- err
					return
				}
				if !bytes.Equal(got.Evidence.Canonical(), wantBytes) {
					errCh <- errors.New("concurrent replay produced different evidence")
					return
				}
				if got.Effective != pinned {
					errCh <- errors.New("concurrent replay moved a pinned deadline")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent replay failed: %v", err)
	}
}

// FuzzTodo_MODEL_005 checks that timer validation and replay never panic on
// arbitrary dataset version strings and policy values.
func FuzzTodo_MODEL_005(f *testing.F) {
	for _, seed := range []struct {
		zone, tzdb, cal, ver string
		policy               uint8
	}{
		{"America/New_York", "2026a", "us-federal", "2026.1", 1},
		{"UTC", "", "us-federal", "2026.1", 2},
		{"", "2026a", "", "", 3},
		{"Mars/Olympus", "2026a", "x", "y", 0},
		{"Australia/Sydney", "2026a", "au-nsw", "2026.2", 200},
	} {
		f.Add(seed.zone, seed.tzdb, seed.cal, seed.ver, seed.policy)
	}
	f.Fuzz(func(t *testing.T, zone, tzdb, cal, ver string, policy uint8) {
		ref := TimerReference{
			Zone:     ZoneRef{ID: zone, TzdbVersion: tzdb},
			Calendar: CalendarRef{Ref: cal, Version: ver},
			Policy:   ReferenceUpdatePolicy(policy),
		}
		err := ref.Validate()
		if err != nil {
			if ref.Canonical() != nil {
				t.Fatalf("invalid reference produced canonical bytes")
			}
			return
		}
		if len(ref.Canonical()) == 0 {
			t.Fatal("valid reference produced no canonical bytes")
		}
		pinned, err := NewInstantFromUnix(1_700_000_000, 0)
		if err != nil {
			t.Fatalf("NewInstantFromUnix error = %v", err)
		}
		outcome, err := ReplayTimer(ref, pinned, DatasetVersions{TzdbVersion: "z", CalendarVersion: "z"},
			func(DatasetVersions) (Instant, error) { return pinned, nil })
		if err != nil {
			t.Fatalf("replay of a valid reference failed: %v", err)
		}
		if outcome.Effective != pinned {
			t.Fatalf("stable resolver moved the deadline")
		}
	})
}
