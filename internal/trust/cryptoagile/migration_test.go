package cryptoagile

import (
	"errors"
	"testing"
	"time"
)

func day(n int) time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
}

func TestMigrationPlan_ValidateAccepts(t *testing.T) {
	plan := MigrationPlan{Windows: []Window{
		{Start: day(0), End: day(10), ActiveSuiteID: "a"},
		{Start: day(10), End: day(20), ActiveSuiteID: "b", DualSuiteID: "a"},
		{Start: day(20), ActiveSuiteID: "b"},
	}}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestMigrationPlan_ValidateRefuses(t *testing.T) {
	cases := []struct {
		name    string
		plan    MigrationPlan
		wantErr error
	}{
		{"empty", MigrationPlan{}, ErrPlanEmpty},
		{
			"no active suite",
			MigrationPlan{Windows: []Window{{Start: day(0), End: day(10)}}},
			ErrPlanBadWindow,
		},
		{
			"backwards window",
			MigrationPlan{Windows: []Window{{Start: day(10), End: day(0), ActiveSuiteID: "a"}}},
			ErrPlanBadWindow,
		},
		{
			"open-ended not last",
			MigrationPlan{Windows: []Window{
				{Start: day(0), ActiveSuiteID: "a"},
				{Start: day(10), End: day(20), ActiveSuiteID: "b"},
			}},
			ErrPlanOpenNotLast,
		},
		{
			"unsorted",
			MigrationPlan{Windows: []Window{
				{Start: day(10), End: day(20), ActiveSuiteID: "a"},
				{Start: day(0), End: day(10), ActiveSuiteID: "b"},
			}},
			ErrPlanUnsorted,
		},
		{
			// The RED case named explicitly by CRYPTO-001: a gap where no
			// suite is declared active.
			"gap",
			MigrationPlan{Windows: []Window{
				{Start: day(0), End: day(10), ActiveSuiteID: "a"},
				{Start: day(15), End: day(20), ActiveSuiteID: "b"},
			}},
			ErrPlanGap,
		},
		{
			// The other RED case: two windows both claim the same instant.
			"overlap",
			MigrationPlan{Windows: []Window{
				{Start: day(0), End: day(10), ActiveSuiteID: "a"},
				{Start: day(5), End: day(20), ActiveSuiteID: "b"},
			}},
			ErrPlanOverlap,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.plan.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestMigrationPlan_WindowAt(t *testing.T) {
	plan := MigrationPlan{Windows: []Window{
		{Start: day(0), End: day(10), ActiveSuiteID: "a"},
		{Start: day(10), End: day(20), ActiveSuiteID: "b", DualSuiteID: "a"},
		{Start: day(20), ActiveSuiteID: "b"},
	}}

	w, idx, ok := plan.WindowAt(day(5))
	if !ok || idx != 0 || w.ActiveSuiteID != "a" {
		t.Fatalf("WindowAt(day5) = %+v idx=%d ok=%v", w, idx, ok)
	}

	w, idx, ok = plan.WindowAt(day(10))
	if !ok || idx != 1 || w.ActiveSuiteID != "b" || w.DualSuiteID != "a" {
		t.Fatalf("WindowAt(day10) = %+v idx=%d ok=%v", w, idx, ok)
	}

	w, idx, ok = plan.WindowAt(day(1000))
	if !ok || idx != 2 {
		t.Fatalf("WindowAt(far future) = %+v idx=%d ok=%v, want open-ended last window", w, idx, ok)
	}

	if _, _, ok := plan.WindowAt(day(-1)); ok {
		t.Fatalf("WindowAt(before plan start) = ok, want false")
	}
}
