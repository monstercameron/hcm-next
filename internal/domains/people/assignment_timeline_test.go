package people_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type assignmentTimelineReader struct {
	set    people.AssignmentTimelineFactSet
	calls  int
	fields [][]people.FieldID
}

func (r *assignmentTimelineReader) AssignmentTimelineAt(_ context.Context, q people.AssignmentTimelineQuery) (people.AssignmentTimelineFactSet, error) {
	r.calls++
	r.fields = append(r.fields, append([]people.FieldID(nil), q.Fields...))
	return r.set, nil
}

func assignmentPeriod(t *testing.T, id, assignmentID, kind, start, end, known string) people.AssignmentPeriodFact {
	t.Helper()
	revision, err := values.NewSequenceRevision("assignment."+assignmentID, uint64(len(id)+1))
	if err != nil {
		t.Fatal(err)
	}
	return people.AssignmentPeriodFact{
		PeriodID: id, AssignmentID: assignmentID, EmploymentID: "employment-1",
		JobCode: "ENG-SWE3", PositionID: "position-" + assignmentID,
		OrgUnit: "eng-platform", Location: "New York", ManagerRelationshipRef: "manager-rel-1",
		CostCenter: "cc-eng", Status: kind, Kind: people.AssignmentPeriodKind(kind),
		Effective: timelineInterval(t, start, end), KnownAt: timelineKnown(t, known), Revision: revision,
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcmnext.people", PolicyRef: "people.source/2026.1"},
		Provenance: evidence.Provenance{Source: "hcmnext.people", EvidenceRef: "assignment/" + id, RecordedAt: timelineRecorded(t, known)},
	}
}

func assignmentTimelineReaderFixture(t *testing.T) *assignmentTimelineReader {
	t.Helper()
	worker, err := fixtures.WorkerRef("jane-doe")
	if err != nil {
		t.Fatal(err)
	}
	watermark, err := values.NewSequenceRevision("assignment.timeline", 22)
	if err != nil {
		t.Fatal(err)
	}
	return &assignmentTimelineReader{set: people.AssignmentTimelineFactSet{
		Worker: worker, Exists: true, Watermark: watermark,
		Periods: []people.AssignmentPeriodFact{
			assignmentPeriod(t, "primary", "assignment-1", "ACTIVE", "2026-01-01", "2026-04-01", "2026-01-02T00:00:00Z"),
			assignmentPeriod(t, "temporary", "assignment-2", "PLANNED", "2026-02-01", "2026-03-01", "2026-01-03T00:00:00Z"),
			assignmentPeriod(t, "secondary", "assignment-3", "ACTIVE", "2026-06-01", "", "2026-04-01T00:00:00Z"),
		},
	}}
}

func assignmentTimelineRequest(t *testing.T, auth people.AuthorizationDecision) people.PersonWorkerReadRequest {
	t.Helper()
	worker, err := fixtures.WorkerRef("jane-doe")
	if err != nil {
		t.Fatal(err)
	}
	return people.PersonWorkerReadRequest{Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t), Authorization: auth}
}

// TestTodo_PEOPLE_003 is the registry's exact primary acceptance symbol.
func TestTodo_PEOPLE_003(t *testing.T) {
	fields := people.AssignmentTimelineFields()
	auth := fixtures.AllowAll(testPolicyVersion, testPurpose, fields)
	reader := assignmentTimelineReaderFixture(t)
	result, err := people.ReadAssignmentTimeline(context.Background(), reader, assignmentTimelineRequest(t, auth))
	if err != nil {
		t.Fatalf("ReadAssignmentTimeline: %v", err)
	}
	if result.Disclosure != people.DisclosureFull || result.Presence != people.SubjectPresent {
		t.Fatalf("disclosure/presence = %s/%s, want FULL/PRESENT", result.Disclosure, result.Presence)
	}
	if len(result.Periods) != 3 || result.Periods[0].PeriodID != "primary" || result.Periods[1].PeriodID != "temporary" || result.Periods[2].PeriodID != "secondary" {
		t.Fatalf("periods = %+v, want every primary/temporary/secondary assignment in effective order", result.Periods)
	}
	if result.Periods[1].Kind != people.AssignmentPeriodPlanned {
		t.Fatalf("temporary assignment kind = %s, want PLANNED", result.Periods[1].Kind)
	}
	for _, field := range []people.FieldID{people.FieldJobCode, people.FieldPositionID, people.FieldOrgUnit, people.FieldLocation, people.FieldManagerRelation, people.FieldCostCenter} {
		if got, ok := result.Periods[0].Value(field); !ok || got == "" {
			t.Fatalf("field %s = %q/%v, want an independently disclosed fact", field, got, ok)
		}
	}
	if len(result.Gaps) != 1 || result.Gaps[0].From.Compare(timelineDate(t, "2026-04-01")) != 0 || result.Gaps[0].To.Compare(timelineDate(t, "2026-06-01")) != 0 {
		t.Fatalf("gaps = %+v, want the explicit April-to-June gap", result.Gaps)
	}
	if len(result.Overlaps) != 1 || result.Overlaps[0].FirstPeriodID != "primary" || result.Overlaps[0].SecondPeriodID != "temporary" {
		t.Fatalf("overlaps = %+v, want the temporary assignment overlap", result.Overlaps)
	}
	if len(result.Findings) != len(result.Gaps)+len(result.Overlaps) {
		t.Fatalf("findings = %+v, want one typed finding per gap/overlap", result.Findings)
	}
	if len(reader.fields) != 1 || len(reader.fields[0]) != len(fields) {
		t.Fatalf("reader field mask = %v, want fixed assignment mask %v", reader.fields, fields)
	}
	if result.Canonical() == nil || result.Receipt.Validate() != nil || len(result.Effects.NonZero()) != 0 {
		t.Fatal("assignment timeline did not produce a canonical zero-effect receipt")
	}
}

// TestTodo_PEOPLE_003_Property covers deterministic ordering and independent
// preservation of concurrent/future assignment periods.
func TestTodo_PEOPLE_003_Property(t *testing.T) {
	fields := people.AssignmentTimelineFields()
	auth := fixtures.AllowAll(testPolicyVersion, testPurpose, fields)
	a := assignmentTimelineReaderFixture(t)
	b := assignmentTimelineReaderFixture(t)
	one, err := people.ReadAssignmentTimeline(context.Background(), a, assignmentTimelineRequest(t, auth))
	if err != nil {
		t.Fatal(err)
	}
	two, err := people.ReadAssignmentTimeline(context.Background(), b, assignmentTimelineRequest(t, auth))
	if err != nil {
		t.Fatal(err)
	}
	if string(one.Canonical()) != string(two.Canonical()) || one.ResultDigest != two.ResultDigest {
		t.Fatal("identical assignment inputs did not produce a byte-identical result")
	}
	if one.Periods[2].Effective.String() != "[2026-06-01,)" {
		t.Fatalf("future secondary assignment = %s, want it retained rather than invented/dropped", one.Periods[2].Effective)
	}
	a.set.Periods[0].JobCode = "MUTATED"
	if got, _ := one.Periods[0].Value(people.FieldJobCode); got != "ENG-SWE3" {
		t.Fatalf("result aliases reader facts: %q", got)
	}
}

// TestTodo_PEOPLE_003_Fault covers non-disclosure and future-known rejection.
func TestTodo_PEOPLE_003_Fault(t *testing.T) {
	fields := people.AssignmentTimelineFields()
	auth := fixtures.AllowAll(testPolicyVersion, testPurpose, fields)
	withheld := assignmentTimelineReaderFixture(t)
	decision := fixtures.WithheldSubject(auth, "outside_population_scope")
	result, err := people.ReadAssignmentTimeline(context.Background(), withheld, assignmentTimelineRequest(t, decision))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disclosure != people.DisclosureWithheld || result.Presence != people.SubjectPresenceUnspecified || len(result.Periods) != 0 || withheld.calls != 0 {
		t.Fatalf("withheld assignment timeline = %+v; reader calls=%d", result, withheld.calls)
	}
	future := assignmentTimelineReaderFixture(t)
	future.set.Periods[0].KnownAt = timelineKnown(t, "2026-12-01T00:00:00Z")
	future.set.Periods[0].Provenance.RecordedAt = timelineRecorded(t, "2026-12-01T00:00:00Z")
	if _, err := people.ReadAssignmentTimeline(context.Background(), future, assignmentTimelineRequest(t, auth)); !errors.Is(err, people.ErrFutureKnownAssignment) {
		t.Fatalf("future-known assignment error = %v, want ErrFutureKnownAssignment", err)
	}

	wrong := assignmentTimelineReaderFixture(t)
	wrong.set.Worker = values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker, Id: "22222222-2222-4222-8222-222222222222"}
	if _, err := people.ReadAssignmentTimeline(context.Background(), wrong, assignmentTimelineRequest(t, auth)); !errors.Is(err, people.ErrFactSubjectMismatch) {
		t.Fatalf("wrong-subject error = %v, want ErrFactSubjectMismatch", err)
	}
}
