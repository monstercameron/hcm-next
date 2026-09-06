package cycle

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func mustLocalDate(t *testing.T, y int, m time.Month, d int) values.LocalDate {
	t.Helper()
	ld, err := values.NewLocalDate(y, m, d)
	if err != nil {
		t.Fatal(err)
	}
	return ld
}

func mustLocalTime(t *testing.T, h, m, s int) values.LocalTime {
	t.Helper()
	lt, err := values.NewLocalTime(h, m, s, 0)
	if err != nil {
		t.Fatal(err)
	}
	return lt
}

func testTenantCalendar() TenantCalendar {
	return TenantCalendar{
		Zone:     values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		Calendar: values.CalendarRef{Ref: "us-business", Version: "1"},
		WorkingWeekdays: map[time.Weekday]bool{
			time.Monday: true, time.Tuesday: true, time.Wednesday: true,
			time.Thursday: true, time.Friday: true,
		},
	}
}

// TestTodo_CYCLE_003 is the primary acceptance case: a nominal cutoff on a
// plain working day resolves to an exact instant with an Explain naming the
// deciding rule; a nominal cutoff that lands on a jurisdiction holiday with
// a roll-forward policy resolves to the next working day's instant instead
// of silently keeping the holiday date; a nominal cutoff whose local time
// falls inside a DST spring-forward gap reports REVIEW_REQUIRED rather than
// guessing; and a calendar with no reachable working day reports
// REVIEW_REQUIRED instead of looping forever.
func TestTodo_CYCLE_003(t *testing.T) {
	cal := testTenantCalendar()

	// 2026-01-05 is a Monday: an ordinary working day.
	rule := CutoffRule{
		PhaseID:         "open",
		NominalDate:     mustLocalDate(t, 2026, time.January, 5),
		NominalTime:     mustLocalTime(t, 17, 0, 0),
		JurisdictionRef: "us-federal",
		Adjustment:      AdjustmentNone,
	}
	res, err := ResolveCutoff(rule, cal)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != CutoffResolved || res.Instant.IsZero() || res.Rule == "" {
		t.Fatalf("expected a resolved, explained cutoff, got %+v", res)
	}
	if res.EffectiveDate != rule.NominalDate {
		t.Fatalf("no adjustment requested but effective date changed: %s", res.EffectiveDate)
	}

	// The same nominal date declared a holiday, with roll-forward: moves to
	// 2026-01-06 (Tuesday) instead of silently keeping the holiday date.
	holidayCal := cal
	holidayCal.Holidays = []Holiday{{
		Date:            mustLocalDate(t, 2026, time.January, 5),
		JurisdictionRef: "us-federal",
		Name:            "Test Holiday",
	}}
	rolled := rule
	rolled.Adjustment = AdjustmentNextBusinessDay
	res2, err := ResolveCutoff(rolled, holidayCal)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != CutoffResolved {
		t.Fatalf("expected resolved after roll-forward, got %+v", res2)
	}
	wantDate := mustLocalDate(t, 2026, time.January, 6)
	if res2.EffectiveDate != wantDate {
		t.Fatalf("expected roll-forward to %s, got %s", wantDate, res2.EffectiveDate)
	}
	if res2.Instant.Equal(res.Instant) {
		t.Fatal("holiday roll-forward produced the same instant as the unadjusted cutoff")
	}

	// 2026-03-08 02:30 America/New_York falls inside the spring-forward
	// gap (02:00 -> 03:00 does not exist that day).
	gapCal := cal
	gapCal.WorkingWeekdays[time.Sunday] = true // isolate the DST failure from the weekday adjustment
	dstRule := CutoffRule{
		PhaseID:         "open",
		NominalDate:     mustLocalDate(t, 2026, time.March, 8),
		NominalTime:     mustLocalTime(t, 2, 30, 0),
		JurisdictionRef: "us-federal",
		Adjustment:      AdjustmentNone,
	}
	res3, err := ResolveCutoff(dstRule, gapCal)
	if err != nil {
		t.Fatal(err)
	}
	if res3.Status != CutoffReviewRequired || res3.Rule == "" {
		t.Fatalf("expected REVIEW_REQUIRED for a DST gap, got %+v", res3)
	}

	// A calendar with no working weekday at all fails Validate outright --
	// there is nothing to roll forward to.
	noWorkingDays := cal
	noWorkingDays.WorkingWeekdays = map[time.Weekday]bool{}
	if err := noWorkingDays.Validate(); err == nil {
		t.Fatal("a calendar with no working weekday must fail Validate")
	}

	// A calendar that has a working weekday but blocks every occurrence of
	// it with a holiday for a full year cannot roll forward to any working
	// day either; the resolver must give up and report REVIEW_REQUIRED
	// rather than loop forever.
	mondayOnly := TenantCalendar{
		Zone:            cal.Zone,
		Calendar:        cal.Calendar,
		WorkingWeekdays: map[time.Weekday]bool{time.Monday: true},
	}
	mondayStart := mustLocalDate(t, 2026, time.January, 5)
	for i := 0; i <= maxCutoffAdjustmentDays; i++ {
		d := mondayStart.AddDays(i)
		if civilWeekday(d) == time.Monday {
			mondayOnly.Holidays = append(mondayOnly.Holidays, Holiday{Date: d, JurisdictionRef: "us-federal", Name: "Blocked Monday"})
		}
	}
	impossible := CutoffRule{
		PhaseID:         "open",
		NominalDate:     mondayStart,
		NominalTime:     mustLocalTime(t, 17, 0, 0),
		JurisdictionRef: "us-federal",
		Adjustment:      AdjustmentNextBusinessDay,
	}
	horizonRes, err := ResolveCutoff(impossible, mondayOnly)
	if err != nil {
		t.Fatal(err)
	}
	if horizonRes.Status != CutoffReviewRequired {
		t.Fatalf("expected REVIEW_REQUIRED when no working day is reachable, got %+v", horizonRes)
	}

	// Structural failures never produce a silent resolution.
	badRule := rule
	badRule.JurisdictionRef = ""
	if _, err := ResolveCutoff(badRule, cal); err == nil {
		t.Fatal("expected an error for a cutoff rule with no jurisdiction reference")
	}
	badCal := cal
	badCal.Calendar = values.CalendarRef{}
	if _, err := ResolveCutoff(rule, badCal); err == nil {
		t.Fatal("expected an error for a calendar with no calendar reference")
	}
}

// TestTodo_CYCLE_003_Property verifies ResolveCutoff is a pure function of
// its inputs: identical rule/calendar values always resolve identically,
// and a calendar revision (a changed Calendar.Version) always changes the
// explain text even when it changes nothing else, so a resolution never
// hides which calendar revision actually produced it.
func TestTodo_CYCLE_003_Property(t *testing.T) {
	cal := testTenantCalendar()
	rule := CutoffRule{
		PhaseID:         "open",
		NominalDate:     mustLocalDate(t, 2026, time.January, 5),
		NominalTime:     mustLocalTime(t, 17, 0, 0),
		JurisdictionRef: "us-federal",
		Adjustment:      AdjustmentNone,
	}

	a, err := ResolveCutoff(rule, cal)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ResolveCutoff(rule, cal)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != b.Status || !a.Instant.Equal(b.Instant) || a.Rule != b.Rule || a.EffectiveDate != b.EffectiveDate {
		t.Fatalf("ResolveCutoff is not deterministic: %+v vs %+v", a, b)
	}

	revised := cal
	revised.Calendar = values.CalendarRef{Ref: cal.Calendar.Ref, Version: "2"}
	c, err := ResolveCutoff(rule, revised)
	if err != nil {
		t.Fatal(err)
	}
	if c.Rule == a.Rule {
		t.Fatal("a calendar revision change did not change the explain text")
	}
	if !c.Instant.Equal(a.Instant) {
		t.Fatal("an unrelated calendar revision changed the resolved instant")
	}

	// ResolveCycleCutoffs resolves every rule in order and fails closed on
	// the first structural error rather than silently skipping it.
	rules := []CutoffRule{rule, {PhaseID: "", NominalDate: rule.NominalDate, NominalTime: rule.NominalTime, JurisdictionRef: "us-federal", Adjustment: AdjustmentNone}}
	if _, err := ResolveCycleCutoffs(rules, cal); err == nil {
		t.Fatal("expected ResolveCycleCutoffs to fail on the invalid second rule")
	}
	good := []CutoffRule{rule, {PhaseID: "close", NominalDate: mustLocalDate(t, 2026, time.January, 6), NominalTime: rule.NominalTime, JurisdictionRef: "us-federal", Adjustment: AdjustmentNone}}
	resolved, err := ResolveCycleCutoffs(good, cal)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != len(good) || resolved[0].PhaseID != "open" || resolved[1].PhaseID != "close" {
		t.Fatalf("expected one resolution per rule in order, got %+v", resolved)
	}
}
