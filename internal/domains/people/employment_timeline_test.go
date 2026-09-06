package people_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type employmentTimelineReader struct {
	set    people.EmploymentTimelineFactSet
	calls  int
	fields [][]people.FieldID
}

func (r *employmentTimelineReader) EmploymentTimelineAt(_ context.Context, q people.EmploymentTimelineQuery) (people.EmploymentTimelineFactSet, error) {
	r.calls++
	r.fields = append(r.fields, append([]people.FieldID(nil), q.Fields...))
	return r.set, nil
}

func timelineKnown(t *testing.T, text string) values.KnownAt {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func timelineRecorded(t *testing.T, text string) values.RecordedAt {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(at))
	if err != nil {
		t.Fatal(err)
	}
	return recorded
}

func timelineDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func timelineInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	cal := values.CalendarRef{Ref: "people.employment.test", Version: "1"}
	var (
		interval values.EffectiveInterval
		err      error
	)
	if end == "" {
		interval, err = values.NewOpenLocalDateInterval(timelineDate(t, start), cal)
	} else {
		interval, err = values.NewLocalDateInterval(timelineDate(t, start), timelineDate(t, end), cal)
	}
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func employmentPeriod(t *testing.T, id, employmentID, kind, start, end, known string) people.EmploymentPeriodFact {
	t.Helper()
	revision, err := values.NewSequenceRevision("employment."+employmentID, uint64(len(id)+1))
	if err != nil {
		t.Fatal(err)
	}
	return people.EmploymentPeriodFact{
		PeriodID: id, EmploymentID: employmentID, LegalEntity: "HarborCare US Inc.",
		WorkerType: "EMPLOYEE", HireDate: "2026-01-01", Status: kind, Kind: people.EmploymentPeriodKind(kind),
		Effective: timelineInterval(t, start, end), KnownAt: timelineKnown(t, known), Revision: revision,
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcmnext.people", PolicyRef: "people.source/2026.1"},
		Provenance: evidence.Provenance{Source: "hcmnext.people", EvidenceRef: "employment/" + id, RecordedAt: timelineRecorded(t, known)},
	}
}

func timelineRequest(t *testing.T, auth people.AuthorizationDecision) people.PersonWorkerReadRequest {
	t.Helper()
	worker, err := fixtures.WorkerRef("jane-doe")
	if err != nil {
		t.Fatal(err)
	}
	return people.PersonWorkerReadRequest{Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t), Authorization: auth}
}

func timelineReader(t *testing.T) *employmentTimelineReader {
	t.Helper()
	worker, err := fixtures.WorkerRef("jane-doe")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("employment.timeline", 17)
	if err != nil {
		t.Fatal(err)
	}
	periods := []people.EmploymentPeriodFact{
		employmentPeriod(t, "revision-active", "employment-1", "ACTIVE", "2026-01-01", "2026-04-01", "2026-01-02T00:00:00Z"),
		employmentPeriod(t, "revision-suspended", "employment-1", "SUSPENDED", "2026-04-01", "2026-06-01", "2026-04-02T00:00:00Z"),
		employmentPeriod(t, "second-employment", "employment-2", "ACTIVE", "2026-03-01", "2026-05-01", "2026-03-02T00:00:00Z"),
		employmentPeriod(t, "future-employment", "employment-3", "PROPOSED", "2026-08-01", "", "2026-04-02T00:00:00Z"),
	}
	return &employmentTimelineReader{set: people.EmploymentTimelineFactSet{Worker: worker, Exists: true, Periods: periods, Watermark: revision}}
}

// TestTodo_PEOPLE_002 is the registry's exact primary matrix symbol.
func TestTodo_PEOPLE_002(t *testing.T) {
	fields := people.EmploymentTimelineFields()
	auth := fixtures.AllowAll(testPolicyVersion, testPurpose, fields)
	reader := timelineReader(t)
	result, err := people.ReadEmploymentTimeline(context.Background(), reader, timelineRequest(t, auth))
	if err != nil {
		t.Fatalf("ReadEmploymentTimeline: %v", err)
	}
	if result.Disclosure != people.DisclosureFull || result.Presence != people.SubjectPresent {
		t.Fatalf("disclosure/presence = %s/%s, want FULL/PRESENT", result.Disclosure, result.Presence)
	}
	if len(result.Periods) != 4 {
		t.Fatalf("period count = %d, want four independent periods/revisions", len(result.Periods))
	}
	if result.Periods[0].PeriodID != "revision-active" || result.Periods[1].PeriodID != "second-employment" {
		t.Fatalf("period order = %q, %q, want effective-date order", result.Periods[0].PeriodID, result.Periods[1].PeriodID)
	}
	if result.Periods[1].Kind != people.EmploymentPeriodActive || result.Periods[2].Kind != people.EmploymentPeriodSuspended {
		t.Fatalf("typed period kinds were lost: %#v", result.Periods)
	}
	if result.Periods[3].Effective.String() != "[2026-08-01,)" {
		t.Fatalf("future period effective interval = %s", result.Periods[3].Effective)
	}
	if got, ok := result.Periods[0].Value(people.FieldEmploymentStatus); !ok || got != "ACTIVE" {
		t.Fatalf("period status = %q/%v, want ACTIVE/true", got, ok)
	}
	if len(result.Gaps) != 1 || result.Gaps[0].From.Compare(timelineDate(t, "2026-06-01")) != 0 || result.Gaps[0].To.Compare(timelineDate(t, "2026-08-01")) != 0 {
		t.Fatalf("gaps = %+v, want the explicit June-to-August gap", result.Gaps)
	}
	if len(result.Overlaps) < 2 {
		t.Fatalf("overlaps = %+v, want simultaneous employment and revision overlaps", result.Overlaps)
	}
	if len(result.Findings) != len(result.Gaps)+len(result.Overlaps) {
		t.Fatalf("typed findings = %+v, want one per gap/overlap", result.Findings)
	}
	if len(reader.fields) != 1 || len(reader.fields[0]) != len(fields) {
		t.Fatalf("reader field mask = %v, want the fixed employment mask %v", reader.fields, fields)
	}
	if result.Canonical() == nil || result.Receipt.Validate() != nil {
		t.Fatal("valid timeline did not produce a canonical zero-effect receipt")
	}
	if len(result.Effects.NonZero()) != 0 {
		t.Fatal("timeline read must have zero effects")
	}
}

// TestTodo_PEOPLE_002_Security is the registry's exact security matrix.
func TestTodo_PEOPLE_002_Security(t *testing.T) {
	fields := people.EmploymentTimelineFields()
	allow := fixtures.AllowAll(testPolicyVersion, testPurpose, fields)

	t.Run("denied field is redacted without dropping the period", func(t *testing.T) {
		reader := timelineReader(t)
		decision := fixtures.DenyFields(allow, "employment_restricted", people.FieldLegalEntity)
		result, err := people.ReadEmploymentTimeline(context.Background(), reader, timelineRequest(t, decision))
		if err != nil {
			t.Fatal(err)
		}
		if result.Disclosure != people.DisclosurePartial || len(result.Periods) != 4 {
			t.Fatalf("disclosure/periods = %s/%d, want PARTIAL/4", result.Disclosure, len(result.Periods))
		}
		for _, period := range result.Periods {
			var denied people.ExplainedFact
			for _, field := range period.Fields {
				if field.Field == people.FieldLegalEntity {
					denied = field
				}
			}
			if denied.Access != people.AccessDenied || denied.Value.State() != values.PresenceRedacted || denied.Provenance.Validate() == nil {
				t.Fatalf("denied legal entity = %+v", denied)
			}
		}
		if len(reader.fields) != 1 {
			t.Fatal("reader should still be called for the authorized part of a partial mask")
		}
		for _, field := range reader.fields[0] {
			if field == people.FieldLegalEntity {
				t.Fatal("denied field crossed the fixed reader projection")
			}
		}
	})

	t.Run("withheld subject does not load or disclose timeline periods", func(t *testing.T) {
		reader := timelineReader(t)
		decision := fixtures.WithheldSubject(allow, "outside_population_scope")
		result, err := people.ReadEmploymentTimeline(context.Background(), reader, timelineRequest(t, decision))
		if err != nil {
			t.Fatal(err)
		}
		if result.Disclosure != people.DisclosureWithheld || result.Presence != people.SubjectPresenceUnspecified || len(result.Periods) != 0 {
			t.Fatalf("withheld result = %+v", result)
		}
		if reader.calls != 0 {
			t.Fatal("withheld subject must not reach the employment reader")
		}
	})

	t.Run("future knowledge is not silently accepted", func(t *testing.T) {
		reader := timelineReader(t)
		reader.set.Periods[0].KnownAt = timelineKnown(t, "2026-12-01T00:00:00Z")
		reader.set.Periods[0].Provenance.RecordedAt = timelineRecorded(t, "2026-12-01T00:00:00Z")
		if _, err := people.ReadEmploymentTimeline(context.Background(), reader, timelineRequest(t, allow)); !errors.Is(err, people.ErrFutureKnownPeriod) {
			t.Fatalf("future-known period error = %v, want ErrFutureKnownPeriod", err)
		}
	})
}
