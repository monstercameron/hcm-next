package schedule_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/cycle"
	"github.com/monstercameron/hcm-next/internal/engines/schedule"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const publisherType = "hcmnext.scheduling.publish_triggers"

func fixture() (schedule.TriggerDefinition, []schedule.AuthorizedTarget) {
	target := intent.Ref{TypeID: "hcmnext.people.review_worker", Version: 3}
	def := schedule.TriggerDefinition{
		ID:                  "monthly-review",
		Version:             "1",
		TenantID:            "tenant-1",
		Target:              target,
		InputTemplateDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Purpose:             "workforce.review",
		Owner:               "workforce-operations",
		Source: schedule.TriggerSource{
			Kind: schedule.SourceCron,
			Cron: schedule.CronSource{Expression: "*/15 9-17 * * 1-5"},
		},
		Overlap:              schedule.OverlapQueue,
		Storm:                schedule.StormPolicy{MaxFiringsPerWindow: 20, Window: time.Hour, JitterBound: 5 * time.Minute},
		ExecutionMode:        intent.ModeExecute,
		ExecutionEnvironment: intent.EnvironmentProduction,
	}
	targets := []schedule.AuthorizedTarget{{
		Ref:          target,
		Purposes:     []string{def.Purpose},
		AllowedModes: []intent.Mode{intent.ModeExecute},
		PublisherRef: intent.Ref{TypeID: publisherType, Version: 1},
	}}
	return def, targets
}

func assertRefusal(t *testing.T, err error, field string, cause error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected typed refusal, got nil")
	}
	if cause != nil && !errors.Is(err, cause) {
		t.Fatalf("error = %v, want errors.Is(..., %v)", err, cause)
	}
	if got := schedule.FieldOf(err); !strings.Contains(got, field) {
		t.Fatalf("field = %q, want it to contain %q (error %v)", got, field, err)
	}
}

func TestTodo_SCHED_001(t *testing.T) {
	def, targets := fixture()
	for _, expression := range []string{"* * * * *", "*/5 0-23 1,15 1-12 1-5"} {
		if _, err := schedule.ParseCron(expression); err != nil {
			t.Fatalf("ParseCron(%q): %v", expression, err)
		}
	}
	reg := schedule.NewRegistry()
	published, err := schedule.Publish(reg, def, targets)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if published.Digest == "" || !strings.HasPrefix(published.Digest, "sha256:") {
		t.Fatalf("digest = %q, want canonical sha256 digest", published.Digest)
	}
	if err := published.Verify(); err != nil {
		t.Fatalf("published.Verify: %v", err)
	}
	if !strings.Contains(published.Explain(), def.Target.TypeID) || !strings.Contains(published.Explain(), def.Purpose) {
		t.Fatalf("Explain omitted target or purpose: %s", published.Explain())
	}

	// Publication owns a frozen copy of caller-owned source data.
	def.Source.Cron.Expression = "0 0 1 1 0"
	again, found, err := reg.Get(published.Ref())
	if err != nil || !found {
		t.Fatalf("Get: found=%v err=%v", found, err)
	}
	if again.Definition.Source.Cron.Expression != "*/15 9-17 * * 1-5" {
		t.Fatalf("published source changed through caller mutation: %q", again.Definition.Source.Cron.Expression)
	}

	activation, err := reg.Activate(published.Ref(), schedule.ActivationEvidence{
		ActivatedBy: "scheduler-admin",
		Authority:   "configuration-publisher",
		ActivatedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	})
	if err != nil || activation.Ref != published.Ref() {
		t.Fatalf("Activate: record=%+v err=%v", activation, err)
	}
	active, ok := reg.Active(def.TenantID, def.ID)
	if !ok || active.Digest != published.Digest {
		t.Fatalf("Active = %+v, want published digest %q", active, published.Digest)
	}

	date, err := values.NewLocalDate(2026, time.September, 5)
	if err != nil {
		t.Fatal(err)
	}
	clock, err := values.NewLocalTime(9, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	calendarDefinition := def
	calendarDefinition.ID = "calendar-review"
	calendarDefinition.Source = schedule.TriggerSource{Kind: schedule.SourceCalendar, Calendar: schedule.CalendarSource{
		CalendarRef: values.CalendarRef{Ref: "tenant-calendar", Version: "4"},
		Cutoff:      cycle.CutoffRule{PhaseID: "review", NominalDate: date, NominalTime: clock, JurisdictionRef: "US-NY", Adjustment: cycle.AdjustmentNextBusinessDay},
	}}
	if _, err := reg.Publish(calendarDefinition, targets); err != nil {
		t.Fatalf("valid calendar Publish: %v", err)
	}
}

func TestTodo_SCHED_001_Golden(t *testing.T) {
	def, targets := fixture()
	def.ID = "golden-trigger"
	def.Version = "7"
	def.Source.Cron.Expression = "0 8 * * 1-5"
	published, err := schedule.Publish(schedule.NewRegistry(), def, targets)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	const want = "sha256:bd7627a950dd4dec1d0e7de39a7d96f7a0944a97b614f991db2654fd88d28f61"
	if published.Digest != want {
		t.Fatalf("golden digest = %q, want %q", published.Digest, want)
	}
}

func TestTodo_SCHED_001_Race(t *testing.T) {
	def, targets := fixture()
	reg := schedule.NewRegistry()
	const workers = 32
	results := make([]schedule.PublishedTrigger, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = reg.Publish(def, targets)
		}(i)
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("Publish[%d]: %v", i, errs[i])
		}
		if results[i].Digest != results[0].Digest || string(results[i].Canonical()) != string(results[0].Canonical()) {
			t.Fatalf("Publish[%d] differs from first: digest=%q", i, results[i].Digest)
		}
	}
}

func TestTodo_SCHED_001_Fault(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*schedule.TriggerDefinition)
		field  string
		cause  error
	}{
		{"malformed cron", func(d *schedule.TriggerDefinition) { d.Source.Cron.Expression = "@hourly" }, "cron.expression", schedule.ErrCron},
		{"invalid calendar ref", func(d *schedule.TriggerDefinition) {
			d.Source = schedule.TriggerSource{Kind: schedule.SourceCalendar, Calendar: schedule.CalendarSource{
				CalendarRef: values.CalendarRef{Ref: "tenant-calendar"},
			}}
		}, "calendar_ref", schedule.ErrCalendar},
		{"unknown event", func(d *schedule.TriggerDefinition) {
			d.Source = schedule.TriggerSource{Kind: schedule.SourceEvent, Event: schedule.EventFilter{EventType: "NOT_A_REAL_EVENT"}}
		}, "event_type", schedule.ErrEvent},
		{"missing overlap policy", func(d *schedule.TriggerDefinition) { d.Overlap = schedule.OverlapUnspecified }, "overlap_policy", schedule.ErrOverlap},
		{"missing storm policy", func(d *schedule.TriggerDefinition) { d.Storm = schedule.StormPolicy{} }, "storm_policy", schedule.ErrStorm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, targets := fixture()
			tt.mutate(&def)
			_, err := schedule.Publish(schedule.NewRegistry(), def, targets)
			assertRefusal(t, err, tt.field, tt.cause)
		})
	}
}

func TestTodo_SCHED_001_Security(t *testing.T) {
	t.Run("unauthorized target", func(t *testing.T) {
		def, targets := fixture()
		targets[0].Ref = intent.Ref{TypeID: "hcmnext.other.intent", Version: 1}
		_, err := schedule.Publish(schedule.NewRegistry(), def, targets)
		assertRefusal(t, err, "target", schedule.ErrUnauthorizedTarget)
	})
	t.Run("purpose mismatch", func(t *testing.T) {
		def, targets := fixture()
		targets[0].Purposes = []string{"different.purpose"}
		_, err := schedule.Publish(schedule.NewRegistry(), def, targets)
		assertRefusal(t, err, "purpose", schedule.ErrPurposeMismatch)
	})
	t.Run("direct recursion", func(t *testing.T) {
		def, targets := fixture()
		targets[0].PublisherRef = def.Target
		_, err := schedule.Publish(schedule.NewRegistry(), def, targets)
		assertRefusal(t, err, "target", schedule.ErrRecursion)
	})
	t.Run("event recursion", func(t *testing.T) {
		def, targets := fixture()
		def.Source = schedule.TriggerSource{Kind: schedule.SourceEvent, Event: schedule.EventFilter{EventType: schedule.EventTriggerFired}}
		_, err := schedule.Publish(schedule.NewRegistry(), def, targets)
		assertRefusal(t, err, "event_type", schedule.ErrRecursion)
	})
}
