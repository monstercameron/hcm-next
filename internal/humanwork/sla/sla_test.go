package sla_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/humanwork/sla"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	slaZone     = values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}
	slaCalendar = values.CalendarRef{Ref: "us-federal", Version: "2026.1"}
)

func slaClock() sla.SLAClock {
	return sla.SLAClock{
		ClockID: "approval-sla",
		Version: 1,
		Zone:    slaZone, Calendar: slaCalendar,
		ReferenceUpdatePolicy: values.ReferenceUpdatePin,
		ReminderOffset:        sla.CalendarDuration{CalendarDays: 1},
		EscalationOffset:      sla.CalendarDuration{CalendarDays: 2},
		ExpiryOffset:          sla.CalendarDuration{CalendarDays: 3},
	}
}

func instant(t *testing.T, s string) values.Instant {
	t.Helper()
	var out values.Instant
	if err := out.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("instant %q: %v", s, err)
	}
	return out
}

// TestTodo_WORK_005 proves the primary WORK-005 contract: explicit calendar
// inputs produce durable wait requirements, milestones are idempotent, and a
// recorded pause shifts deadlines without deleting the quiet wait.
func TestTodo_WORK_005(t *testing.T) {
	clock := slaClock()
	start := instant(t, "2026-03-07T14:00:00Z") // 09:00 in New York.
	instance, err := sla.NewClockInstance(clock, "work-005", start)
	if err != nil {
		t.Fatalf("NewClockInstance: %v", err)
	}

	requirements, err := instance.Requirements()
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if len(requirements) != 3 {
		t.Fatalf("requirement count = %d, want 3", len(requirements))
	}
	want := []string{
		"2026-03-08T13:00:00Z", // one civil day later, after spring DST.
		"2026-03-09T13:00:00Z",
		"2026-03-10T13:00:00Z",
	}
	for n, requirement := range requirements {
		if got := requirement.FireAtText(); got != want[n] {
			t.Errorf("requirement %d FireAt = %q, want %q", n, got, want[n])
		}
		if requirement.Digest == "" {
			t.Errorf("requirement %d has no digest", n)
		}
		if requirement.Reference.Zone != slaZone || requirement.Reference.Calendar != (values.CalendarRef{Ref: "us-federal", Version: "2026.1"}) {
			t.Errorf("requirement %d lost its versioned references: %+v", n, requirement.Reference)
		}
	}

	signals, err := instance.Signals()
	if err != nil {
		t.Fatalf("Signals: %v", err)
	}
	retry, err := instance.Signals()
	if err != nil {
		t.Fatalf("Signals retry: %v", err)
	}
	seen := map[string]bool{}
	for n := range signals {
		if signals[n].SignalID == "" || signals[n].SignalID != signals[n].ID {
			t.Errorf("signal %d has inconsistent identity: %+v", n, signals[n])
		}
		if seen[signals[n].SignalID] {
			t.Errorf("signal %d duplicated its semantic id", n)
		}
		seen[signals[n].SignalID] = true
		if signals[n].SignalID != retry[n].SignalID {
			t.Errorf("retry changed signal %d id: %q -> %q", n, signals[n].SignalID, retry[n].SignalID)
		}
	}

	paused, err := instance.Pause(instant(t, "2026-03-07T16:00:00Z"), "worker unavailable")
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if !paused.IsPaused() || len(paused.Pauses) != 1 || paused.Pauses[0].PauseReason != "worker unavailable" {
		t.Fatalf("pause evidence = %+v", paused.Pauses)
	}
	quietRequirements, err := paused.Requirements()
	if err != nil {
		t.Fatalf("Requirements while paused: %v", err)
	}
	if len(quietRequirements) != 3 {
		t.Fatalf("paused requirement count = %d, want 3", len(quietRequirements))
	}

	resumed, err := paused.Resume(instant(t, "2026-03-09T16:00:00Z"), "worker restored")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.IsPaused() || resumed.Pauses[0].ResumeReason != "worker restored" {
		t.Fatalf("resume evidence = %+v", resumed.Pauses)
	}
	shifted, err := resumed.Deadline(sla.SignalExpiry)
	if err != nil {
		t.Fatalf("shifted expiry: %v", err)
	}
	if got, want := shifted.String(), "2026-03-12T13:00:00Z"; got != want {
		t.Fatalf("shifted expiry = %q, want %q", got, want)
	}
	shiftedSignal, err := resumed.Emit(sla.SignalExpiry)
	if err != nil {
		t.Fatalf("shifted signal: %v", err)
	}
	if shiftedSignal.SignalID != signals[2].SignalID {
		t.Fatalf("pause/resume changed semantic signal id: %q -> %q", signals[2].SignalID, shiftedSignal.SignalID)
	}
}

// TestTodo_WORK_005_Race proves the pure projection is safe to retry from
// concurrent workers: every reader gets the same signal identities.
func TestTodo_WORK_005_Race(t *testing.T) {
	instance, err := sla.NewInstance(slaClock(), "work-race", instant(t, "2026-06-01T13:00:00Z"))
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	const readers = 8
	results := make([][]sla.Signal, readers)
	errs := make([]error, readers)
	var wg sync.WaitGroup
	for n := 0; n < readers; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			results[n], errs[n] = instance.Signals()
		}(n)
	}
	wg.Wait()
	for n := 0; n < readers; n++ {
		if errs[n] != nil {
			t.Fatalf("reader %d: %v", n, errs[n])
		}
		for m := range results[0] {
			if results[n][m].SignalID != results[0][m].SignalID {
				t.Fatalf("reader %d signal %d = %q, want %q", n, m, results[n][m].SignalID, results[0][m].SignalID)
			}
		}
	}
}

// TestTodo_WORK_005_Fault proves the refusal paths named by WORK-005: a
// missing zone/calendar, an unversioned policy, malformed pause history, and
// unknown signal kinds never produce a wake or signal.
func TestTodo_WORK_005_Fault(t *testing.T) {
	tests := []struct {
		name  string
		clock sla.SLAClock
		want  error
	}{
		{"missing zone", func() sla.SLAClock { c := slaClock(); c.Zone = values.ZoneRef{}; return c }(), values.ErrZoneRequired},
		{"missing calendar", func() sla.SLAClock { c := slaClock(); c.Calendar = values.CalendarRef{}; return c }(), values.ErrCalendarRequired},
		{"missing policy", func() sla.SLAClock {
			c := slaClock()
			c.ReferenceUpdatePolicy = values.ReferenceUpdateUnspecified
			return c
		}(), values.ErrReferenceUpdatePolicyRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := sla.NewInstance(test.clock, "work-fault", instant(t, "2026-06-01T13:00:00Z")); !errors.Is(err, test.want) {
				t.Fatalf("NewInstance error = %v, want %v", err, test.want)
			}
		})
	}

	instance, err := sla.NewInstance(slaClock(), "work-fault", instant(t, "2026-06-01T13:00:00Z"))
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	if _, err := instance.Requirement(sla.SignalKind("UNKNOWN")); !errors.Is(err, sla.ErrSignalKind) {
		t.Fatalf("unknown signal error = %v, want ErrSignalKind", err)
	}
	if _, err := instance.Pause(instant(t, "2026-06-01T14:00:00Z"), ""); !errors.Is(err, sla.ErrPauseReason) {
		t.Fatalf("empty pause reason = %v, want ErrPauseReason", err)
	}
	if _, err := instance.Resume(instant(t, "2026-06-01T14:00:00Z"), "resume"); !errors.Is(err, sla.ErrNotPaused) {
		t.Fatalf("resume without pause = %v, want ErrNotPaused", err)
	}
	paused, err := instance.Pause(instant(t, "2026-06-01T14:00:00Z"), "incident")
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := paused.Pause(instant(t, "2026-06-01T15:00:00Z"), "again"); !errors.Is(err, sla.ErrAlreadyPaused) {
		t.Fatalf("double pause = %v, want ErrAlreadyPaused", err)
	}
	if _, err := paused.Requirements(values.DatasetVersions{}, values.DatasetVersions{}); !errors.Is(err, sla.ErrTooManyDatasets) {
		t.Fatalf("two dataset overrides = %v, want ErrTooManyDatasets", err)
	}
}

func TestVersionAndExplain(t *testing.T) {
	if sla.Version() != 1 {
		t.Fatalf("Version() = %d, want 1", sla.Version())
	}
	if got := sla.Explain(); got == "" {
		t.Fatal("Explain() is empty")
	}
	if got := slaClock().Explain(); got == "" {
		t.Fatal("SLAClock.Explain() is empty")
	}
}
