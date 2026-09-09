package benefits

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func enrollmentDate(t *testing.T, y int, m time.Month, d int) values.LocalDate {
	t.Helper()
	x, err := values.NewLocalDate(y, m, d)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func enrollmentPlan(t *testing.T) values.EntityRef {
	return benefitRef("tenant-benefits", "benefit_plan", uuid.MustParse("10000000-0000-4000-8000-000000000001"))
}

func enrollmentWindow(t *testing.T, id string, kind WindowKind, start, end values.LocalDate) EnrollmentWindow {
	iv, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026.1"})
	if err != nil {
		t.Fatal(err)
	}
	return EnrollmentWindow{ID: id, Kind: kind, PlanID: enrollmentPlan(t), PlanRevision: "rev-1", Interval: iv, Reason: "annual plan election"}
}

func TestTodo_BEN_002(t *testing.T) {
	open := enrollmentWindow(t, "oe-2026", WindowOpenEnrollment, enrollmentDate(t, 2026, time.November, 1), enrollmentDate(t, 2026, time.December, 1))
	newHire := enrollmentWindow(t, "hire-2026", WindowNewHire, enrollmentDate(t, 2026, time.January, 1), enrollmentDate(t, 2026, time.February, 1))
	set, err := NewEnrollmentWindowSet([]EnrollmentWindow{open, newHire})
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveEnrollmentWindow(EnrollmentWindowRequest{Set: set, PlanID: enrollmentPlan(t), PlanRevision: "rev-1", Kind: WindowOpenEnrollment, ElectionDate: enrollmentDate(t, 2026, time.November, 15)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != WindowOpen || res.Start != openMustStart(t, open) || res.End != openMustEnd(t, open) || res.Calendar.Version != "2026.1" || res.Reason == "" {
		t.Fatalf("expected exact open resolution, got %+v", res)
	}
	closed, err := ResolveEnrollmentWindow(EnrollmentWindowRequest{Set: set, PlanID: enrollmentPlan(t), PlanRevision: "rev-1", Kind: WindowOpenEnrollment, ElectionDate: enrollmentDate(t, 2026, time.December, 1)})
	if err != nil || closed.Status != WindowClosed {
		t.Fatalf("out-of-window election = %+v, %v", closed, err)
	}
	conditional, err := ResolveEnrollmentWindow(EnrollmentWindowRequest{Set: set, PlanID: enrollmentPlan(t), PlanRevision: "rev-1", Kind: WindowNewHire, ElectionDate: enrollmentDate(t, 2026, time.January, 15)})
	if err != nil || conditional.Status != WindowConditional {
		t.Fatalf("missing life-event fact = %+v, %v", conditional, err)
	}
	unknown, err := ResolveEnrollmentWindow(EnrollmentWindowRequest{Set: set, PlanID: enrollmentPlan(t), PlanRevision: "rev-2", Kind: WindowOpenEnrollment, ElectionDate: enrollmentDate(t, 2026, time.November, 15)})
	if err != nil || unknown.Status != WindowUnknown {
		t.Fatalf("unpublished revision = %+v, %v", unknown, err)
	}
}

func openMustStart(t *testing.T, w EnrollmentWindow) values.LocalDate {
	x, _ := w.Interval.StartDate()
	return x
}
func openMustEnd(t *testing.T, w EnrollmentWindow) values.LocalDate {
	x, _ := w.Interval.EndDate()
	return x
}

func TestTodo_BEN_002_Property(t *testing.T) {
	start, end := enrollmentDate(t, 2026, time.January, 1), enrollmentDate(t, 2026, time.February, 1)
	w := enrollmentWindow(t, "oe", WindowOpenEnrollment, start, end)
	input := []EnrollmentWindow{w}
	set, err := NewEnrollmentWindowSet(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Reason = "mutated by caller"

	for _, tc := range []struct {
		name string
		date values.LocalDate
		want WindowStatus
	}{
		{"day_before", enrollmentDate(t, 2025, time.December, 31), WindowClosed},
		{"inclusive_start", start, WindowOpen},
		{"last_included_day", enrollmentDate(t, 2026, time.January, 31), WindowOpen},
		{"exclusive_end", end, WindowClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveEnrollmentWindow(EnrollmentWindowRequest{Set: set, PlanID: enrollmentPlan(t), PlanRevision: "rev-1", Kind: WindowOpenEnrollment, ElectionDate: tc.date})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want {
				t.Fatalf("status on %s = %s, want %s", tc.date, got.Status, tc.want)
			}
			if got.WindowID != w.ID || got.Start != start || got.End != end || got.Calendar != w.Interval.Calendar() {
				t.Fatalf("resolution lost pinned window facts: %+v", got)
			}
			if tc.want == WindowOpen && got.Reason != w.Reason {
				t.Fatalf("published reason was aliased through caller input: %q", got.Reason)
			}
		})
	}
}

func TestTodo_BEN_002_Mutation(t *testing.T) {
	w1 := enrollmentWindow(t, "a", WindowOpenEnrollment, enrollmentDate(t, 2026, time.January, 1), enrollmentDate(t, 2026, time.February, 1))
	w2 := enrollmentWindow(t, "b", WindowNewHire, enrollmentDate(t, 2026, time.January, 15), enrollmentDate(t, 2026, time.March, 1))
	if _, err := NewEnrollmentWindowSet([]EnrollmentWindow{w1, w2}); err != nil {
		t.Fatalf("distinct window kinds must be allowed to coexist: %v", err)
	}
	// The same kind and plan revision cannot publish overlapping windows that
	// make an election ambiguous.
	w2.Kind = WindowOpenEnrollment
	_, err := NewEnrollmentWindowSet([]EnrollmentWindow{w1, w2})
	if !errors.Is(err, ErrOverlappingWindows) || !errors.Is(err, ErrBEN002Rejected) {
		t.Fatalf("overlap error = %v", err)
	}
	var rejection *EnrollmentWindowRejection
	if !errors.As(err, &rejection) || rejection.Code != "BEN_002_REJECTED" || rejection.OffendingField != "interval" || rejection.OffendingState != "a,b" || rejection.Version != "rev-1" {
		t.Fatalf("overlap rejection lacks exact context: %#v", rejection)
	}
	if rejection.Error() != "BEN_002_REJECTED: field=interval state=a,b version=rev-1" || errors.Unwrap(rejection) != ErrOverlappingWindows {
		t.Fatalf("unstable rejection representation: %q, unwrap=%v", rejection.Error(), errors.Unwrap(rejection))
	}
	bad := w1
	bad.Interval = values.EffectiveInterval{}
	if _, err := NewEnrollmentWindowSet([]EnrollmentWindow{bad}); !errors.Is(err, ErrInvalidEnrollmentWindow) {
		t.Fatalf("invalid interval error = %v", err)
	}

	otherTenant := w2
	otherTenant.PlanID = benefitRef("other-benefits", "benefit_plan", uuid.MustParse("10000000-0000-4000-8000-000000000001"))
	if _, err := NewEnrollmentWindowSet([]EnrollmentWindow{w1, otherTenant}); err != nil {
		t.Fatalf("overlap must be scoped by tenant-aware plan identity: %v", err)
	}
	otherRevision := w2
	otherRevision.PlanRevision = "rev-2"
	if _, err := NewEnrollmentWindowSet([]EnrollmentWindow{w1, otherRevision}); err != nil {
		t.Fatalf("overlap must be scoped by plan revision: %v", err)
	}
}

func TestEnrollmentWindowValidationAndCanonical(t *testing.T) {
	w := enrollmentWindow(t, "oe", WindowCorrection, enrollmentDate(t, 2026, time.January, 1), enrollmentDate(t, 2026, time.February, 1))
	if len(w.Canonical()) == 0 {
		t.Fatal("valid window has no canonical encoding")
	}
	set, err := NewEnrollmentWindowSet([]EnrollmentWindow{w})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Canonical()) == 0 {
		t.Fatal("valid set has no canonical encoding")
	}

	for _, mutate := range []func(*EnrollmentWindow){
		func(x *EnrollmentWindow) { x.ID = " " },
		func(x *EnrollmentWindow) { x.Kind = "invented" },
		func(x *EnrollmentWindow) { x.PlanID = values.EntityRef{} },
		func(x *EnrollmentWindow) { x.PlanRevision = " " },
		func(x *EnrollmentWindow) { x.Reason = " " },
		func(x *EnrollmentWindow) { x.Interval = values.EffectiveInterval{} },
	} {
		bad := w
		mutate(&bad)
		if !errors.Is(bad.Validate(), ErrInvalidEnrollmentWindow) {
			t.Fatalf("invalid window accepted: %+v", bad)
		}
		if bad.Canonical() != nil {
			t.Fatal("invalid window produced canonical bytes")
		}
	}
}

func TestEnrollmentWindowResolutionRequiresExactScopeAndEvent(t *testing.T) {
	start := enrollmentDate(t, 2026, time.January, 1)
	end := enrollmentDate(t, 2026, time.February, 1)
	w := enrollmentWindow(t, "hire", WindowNewHire, start, end)
	set, err := NewEnrollmentWindowSet([]EnrollmentWindow{w})
	if err != nil {
		t.Fatal(err)
	}
	base := EnrollmentWindowRequest{Set: set, PlanID: enrollmentPlan(t), PlanRevision: "rev-1", Kind: WindowNewHire, ElectionDate: enrollmentDate(t, 2026, time.January, 20), EventDate: enrollmentDate(t, 2026, time.January, 2)}
	open, err := ResolveEnrollmentWindow(base)
	if err != nil || open.Status != WindowOpen || open.Reason != w.Reason {
		t.Fatalf("exact event resolution = %+v, %v", open, err)
	}

	outside := base
	outside.EventDate = enrollmentDate(t, 2025, time.December, 31)
	closed, err := ResolveEnrollmentWindow(outside)
	if err != nil || closed.Status != WindowClosed || closed.Reason != "qualifying event date is outside the declared window" {
		t.Fatalf("out-of-window event = %+v, %v", closed, err)
	}

	missing := base
	missing.EventDate = values.LocalDate{}
	conditional, err := ResolveEnrollmentWindow(missing)
	if err != nil || conditional.Status != WindowConditional || conditional.WindowID != w.ID {
		t.Fatalf("missing event = %+v, %v", conditional, err)
	}
	missing.ElectionDate = end
	closed, err = ResolveEnrollmentWindow(missing)
	if err != nil || closed.Status != WindowClosed {
		t.Fatalf("known out-of-window election must be closed despite missing event: %+v, %v", closed, err)
	}

	wrongTenant := base
	wrongTenant.PlanID = benefitRef("other-benefits", "benefit_plan", uuid.MustParse("10000000-0000-4000-8000-000000000001"))
	unknown, err := ResolveEnrollmentWindow(wrongTenant)
	if err != nil || unknown.Status != WindowUnknown || unknown.WindowID != "" {
		t.Fatalf("cross-tenant lookup = %+v, %v", unknown, err)
	}

	invalid := base
	invalid.PlanRevision = " "
	if _, err := ResolveEnrollmentWindow(invalid); !errors.Is(err, ErrInvalidEnrollmentWindow) {
		t.Fatalf("invalid request error = %v", err)
	}
}
