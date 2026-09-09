package model_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

func workerClass() model.RetentionClass {
	return model.RetentionClass{
		ClassRef:          "WORKER_TRANSACTION",
		DefaultPeriodDays: 2555,
		JurisdictionOverrides: map[string]uint32{
			"US-CA": 1460,
		},
		TriggerEvent:     "EMPLOYMENT_END",
		DispositionOwner: "records-management",
		AuthorityRef:     "authority.employment/v1",
	}
}

// TestTodo_MODEL_026 is the PRIMARY test for records declarations and
// retention assignment.
//
// RED: material evidence/artifact without record class, retention schedule,
// authority and disposition owner fails persistence.
//
// GREEN: declaration derives an explainable effective retention schedule and
// next action date.
func TestTodo_MODEL_026(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		base := model.RecordsDeclaration{
			RecordRef: "evidence.1", RecordClassRef: "WORKER_TRANSACTION",
			AuthorityRef: "authority.employment/v1", DispositionOwner: "records-management",
			Jurisdiction: "US-CA", CreatedAt: instant(2026, time.January, 1),
		}
		cases := []struct {
			name   string
			break_ func(*model.RecordsDeclaration)
		}{
			{"no record class", func(d *model.RecordsDeclaration) { d.RecordClassRef = "" }},
			{"no authority", func(d *model.RecordsDeclaration) { d.AuthorityRef = "" }},
			{"no disposition owner", func(d *model.RecordsDeclaration) { d.DispositionOwner = "" }},
			{"no jurisdiction", func(d *model.RecordsDeclaration) { d.Jurisdiction = "" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				d := base
				tc.break_(&d)
				if err := d.Validate(); !errors.Is(err, model.ErrInvalidRetention) {
					t.Fatalf("validated a declaration missing %s: %v", tc.name, err)
				}
			})
		}

		t.Run("unknown record class", func(t *testing.T) {
			d := base
			d.RecordClassRef = "NONEXISTENT"
			_, err := model.Declare(map[string]model.RetentionClass{"WORKER_TRANSACTION": workerClass()}, d)
			if !errors.Is(err, model.ErrInvalidRetention) {
				t.Fatalf("declared against an unknown record class: %v", err)
			}
		})

		t.Run("class with no default and no override", func(t *testing.T) {
			c := model.RetentionClass{
				ClassRef: "BROKEN", TriggerEvent: "X", DispositionOwner: "owner", AuthorityRef: "authority.x/v1",
			}
			if err := c.Validate(); !errors.Is(err, model.ErrInvalidRetention) {
				t.Fatalf("validated a class with no usable period: %v", err)
			}
		})

		t.Run("jurisdiction with no override and no default", func(t *testing.T) {
			c := model.RetentionClass{
				ClassRef: "OVERRIDE_ONLY", JurisdictionOverrides: map[string]uint32{"US-CA": 1460},
				TriggerEvent: "X", DispositionOwner: "owner", AuthorityRef: "authority.x/v1",
			}
			_, err := c.EffectiveRetention("US-NY", instant(2026, time.January, 1))
			if !errors.Is(err, model.ErrNoJurisdictionOverride) {
				t.Fatalf("resolved a jurisdiction with no override and no default: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		classes := map[string]model.RetentionClass{"WORKER_TRANSACTION": workerClass()}
		d := model.RecordsDeclaration{
			RecordRef: "evidence.1", RecordClassRef: "WORKER_TRANSACTION",
			AuthorityRef: "authority.employment/v1", DispositionOwner: "records-management",
			Jurisdiction: "US-CA", CreatedAt: instant(2026, time.January, 1),
		}
		schedule, err := model.Declare(classes, d)
		if err != nil {
			t.Fatalf("declare: %v", err)
		}
		if schedule.PeriodDays != 1460 {
			t.Fatalf("period = %d, want the US-CA override 1460", schedule.PeriodDays)
		}
		if schedule.Explanation == "" {
			t.Fatalf("schedule carries no explanation")
		}
		wantNext := instant(2026, time.January, 1).Time().AddDate(0, 0, 1460)
		if !schedule.NextActionAt.Time().Equal(wantNext) {
			t.Fatalf("next action = %s, want %s", schedule.NextActionAt.Time(), wantNext)
		}

		// A jurisdiction with no override falls back to the class default.
		d.Jurisdiction = "US-TX"
		fallback, err := model.Declare(classes, d)
		if err != nil {
			t.Fatalf("declare fallback: %v", err)
		}
		if fallback.PeriodDays != 2555 {
			t.Fatalf("fallback period = %d, want class default 2555", fallback.PeriodDays)
		}
	})
}

// TestTodo_MODEL_026_Property asserts that every retention class in the
// compiled catalog resolves a schedule for every jurisdiction it explicitly
// overrides, and that the resolved period always matches the declared
// override exactly (never silently falls back).
func TestTodo_MODEL_026_Property(t *testing.T) {
	reg := mustRegistry(t)
	for _, c := range reg.RetentionClasses() {
		for jurisdiction, want := range c.JurisdictionOverrides {
			schedule, err := c.EffectiveRetention(jurisdiction, instant(2026, time.January, 1))
			if err != nil {
				t.Fatalf("%s/%s: %v", c.ClassRef, jurisdiction, err)
			}
			if schedule.PeriodDays != want {
				t.Fatalf("%s/%s resolved %d days, want override %d", c.ClassRef, jurisdiction, schedule.PeriodDays, want)
			}
		}
	}
}

// TestTodo_MODEL_026_Golden pins the compiled retention-class table.
func TestTodo_MODEL_026_Golden(t *testing.T) {
	reg := mustRegistry(t)
	type row struct {
		ClassRef          string
		DefaultPeriodDays uint32
		Overrides         map[string]uint32
		TriggerEvent      string
		DispositionOwner  string
		AuthorityRef      string
	}
	var rows []row
	for _, c := range reg.RetentionClasses() {
		rows = append(rows, row{
			ClassRef: c.ClassRef, DefaultPeriodDays: c.DefaultPeriodDays, Overrides: c.JurisdictionOverrides,
			TriggerEvent: c.TriggerEvent, DispositionOwner: c.DispositionOwner, AuthorityRef: c.AuthorityRef,
		})
	}
	goldenJSON(t, "model_026_retention.json", rows)
}

// FuzzTodo_MODEL_026 fuzzes EffectiveRetention: the resolved period must
// always be the jurisdiction override when one is declared, and the class
// default otherwise, and must always fail when neither exists.
func FuzzTodo_MODEL_026(f *testing.F) {
	f.Add(uint32(2555), uint32(1460), true)
	f.Add(uint32(0), uint32(1460), true)
	f.Add(uint32(0), uint32(0), false)
	f.Fuzz(func(t *testing.T, defaultDays, overrideDays uint32, hasOverride bool) {
		c := model.RetentionClass{
			ClassRef: "FUZZ", DefaultPeriodDays: defaultDays,
			TriggerEvent: "X", DispositionOwner: "owner", AuthorityRef: "authority.x/v1",
		}
		if hasOverride {
			c.JurisdictionOverrides = map[string]uint32{"US-CA": overrideDays}
		}
		schedule, err := c.EffectiveRetention("US-CA", instant(2026, time.January, 1))
		usable := hasOverride || defaultDays > 0
		if !usable {
			if err == nil {
				t.Fatalf("resolved a class with no default and no override")
			}
			return
		}
		if err != nil {
			t.Fatalf("a usable class failed to resolve: %v", err)
		}
		want := defaultDays
		if hasOverride {
			want = overrideDays
		}
		if schedule.PeriodDays != want {
			t.Fatalf("resolved %d days, want %d", schedule.PeriodDays, want)
		}
	})
}
