package timer

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
)

var misfireBase = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// Decide must be SCHED-002's answer, not this package's. These cases pin the
// decision for every declared policy on both sides of the grace boundary, so
// a change to the schedule engine's semantics shows up here rather than in a
// silently different execution date.

func TestDecide_WithinGraceIsAlwaysOnTimeWhateverThePolicy(t *testing.T) {
	for _, policy := range []schedule.MisfirePolicy{
		schedule.MisfireFireNow, schedule.MisfireSkip, schedule.MisfireCatchUpOnce,
		schedule.MisfireCatchUpAll, schedule.MisfireReview,
	} {
		config := schedule.MisfireConfig{Policy: policy, Grace: time.Hour, MaxCatchUp: 5}
		decision, eligible, err := Decide(misfireBase, misfireBase.Add(30*time.Minute), config)
		if err != nil {
			t.Fatalf("policy %s inside grace: %v", policy, err)
		}
		if decision != DecisionOnTime {
			t.Fatalf("policy %s inside grace decided %q, want ON_TIME", policy, decision)
		}
		if eligible != misfireBase {
			t.Fatalf("policy %s inside grace made work eligible at %s, want the timer's own instant %s",
				policy, eligible, misfireBase)
		}
	}
}

func TestDecide_PastGraceFollowsTheDeclaredPolicy(t *testing.T) {
	late := misfireBase.Add(48 * time.Hour)
	cases := map[schedule.MisfirePolicy]struct {
		decision Decision
		eligible time.Time
	}{
		schedule.MisfireFireNow:     {DecisionFireNow, late},
		schedule.MisfireSkip:        {DecisionSkipped, time.Time{}},
		schedule.MisfireCatchUpOnce: {DecisionCatchUp, misfireBase},
		schedule.MisfireCatchUpAll:  {DecisionCatchUp, misfireBase},
		schedule.MisfireCatchUp:     {DecisionCatchUp, misfireBase},
		schedule.MisfireReview:      {DecisionReview, time.Time{}},
	}
	for policy, want := range cases {
		config := schedule.MisfireConfig{Policy: policy, Grace: time.Hour, MaxCatchUp: 5}
		decision, eligible, err := Decide(misfireBase, late, config)
		if err != nil {
			t.Fatalf("policy %s past grace: %v", policy, err)
		}
		if decision != want.decision {
			t.Fatalf("policy %s past grace decided %q, want %q", policy, decision, want.decision)
		}
		if !eligible.Equal(want.eligible) {
			t.Fatalf("policy %s past grace made work eligible at %s, want %s", policy, eligible, want.eligible)
		}
	}
}

// FIRE_NOW and CATCH_UP differ in exactly one way that matters downstream:
// which instant the woken work becomes eligible at. Losing that distinction
// would silently change an execution date, which is WF-RUN-004's RED clause.
func TestDecide_FireNowAndCatchUpChooseDifferentEligibilityInstants(t *testing.T) {
	late := misfireBase.Add(48 * time.Hour)
	_, fireNowAt, err := Decide(misfireBase, late,
		schedule.MisfireConfig{Policy: schedule.MisfireFireNow, Grace: time.Hour})
	if err != nil {
		t.Fatalf("FIRE_NOW: %v", err)
	}
	_, catchUpAt, err := Decide(misfireBase, late,
		schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour})
	if err != nil {
		t.Fatalf("CATCH_UP_ONCE: %v", err)
	}
	if fireNowAt.Equal(catchUpAt) {
		t.Fatalf("FIRE_NOW and CATCH_UP produced the same eligibility instant %s", fireNowAt)
	}
	if !fireNowAt.Equal(late) || !catchUpAt.Equal(misfireBase) {
		t.Fatalf("FIRE_NOW = %s (want %s), CATCH_UP = %s (want %s)", fireNowAt, late, catchUpAt, misfireBase)
	}
}

func TestDecide_RefusesAnUndeclaredPolicy(t *testing.T) {
	_, _, err := Decide(misfireBase, misfireBase, schedule.MisfireConfig{})
	if !errors.Is(err, ErrMisfirePolicyRequired) {
		t.Fatalf("an unspecified policy: err = %v, want ErrMisfirePolicyRequired", err)
	}
}

// A CATCH_UP policy with no bound is refused by SCHED-002's own validation,
// and Decide surfaces that rather than quietly catching up without a limit.
func TestDecide_SurfacesTheScheduleEnginesOwnConfigRefusal(t *testing.T) {
	_, _, err := Decide(misfireBase, misfireBase.Add(48*time.Hour),
		schedule.MisfireConfig{Policy: schedule.MisfireCatchUp, Grace: time.Hour})
	if err == nil {
		t.Fatal("an unbounded CATCH_UP policy was accepted")
	}
	if errors.Is(err, ErrMisfirePolicyRequired) {
		t.Fatalf("a bound error was reported as a missing policy: %v", err)
	}
	_, _, err = Decide(misfireBase, misfireBase.Add(time.Hour),
		schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: -time.Second})
	if err == nil {
		t.Fatal("a negative grace was accepted")
	}
}

func TestDecide_RefusesAMissingInstant(t *testing.T) {
	config := schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour}
	if _, _, err := Decide(time.Time{}, misfireBase, config); err == nil {
		t.Fatal("a timer with no instant was accepted")
	}
	if _, _, err := Decide(misfireBase, time.Time{}, config); err == nil {
		t.Fatal("a decision with no caller instant was accepted")
	}
}
