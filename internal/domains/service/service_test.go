package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func serviceDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func serviceInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	calendar := values.CalendarRef{Ref: "service.test", Version: "1"}
	var (
		interval values.EffectiveInterval
		err      error
	)
	if end == "" {
		interval, err = values.NewOpenLocalDateInterval(serviceDate(t, start), calendar)
	} else {
		interval, err = values.NewLocalDateInterval(serviceDate(t, start), serviceDate(t, end), calendar)
	}
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func serviceKnown(t *testing.T) values.KnownAt {
	t.Helper()
	at, err := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	instant := values.NewInstant(at)
	known, err := values.NewKnownAt(instant)
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func serviceRevision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func employmentCredit(t *testing.T, kind CreditSourceKind, evidence, authority string) CreditSource {
	t.Helper()
	return CreditSource{Kind: kind, EvidenceRef: evidence, AuthorityRef: authority, Revision: serviceRevision(t, "service.credit", 1)}
}

func servicePeriod(t *testing.T, id, start, end string, source CreditSource) ServicePeriod {
	t.Helper()
	return ServicePeriod{
		ID: id, EmploymentID: "employment-1", Interval: serviceInterval(t, start, end),
		Credit: source, Break: BreakNone, Dimensions: []SeniorityDimension{DimensionGeneral},
		KnownAt: serviceKnown(t), Revision: serviceRevision(t, "service.period."+id, 1),
	}
}

func serviceRule(t *testing.T) SeniorityRule {
	t.Helper()
	return SeniorityRule{
		Dimension: DimensionGeneral, Unit: UnitMonths, DaysPerUnit: 30, Rounding: RoundDown,
		Bridge:   BridgeRule{MaxGapDays: 5, BreakTypes: []BreakType{BreakUncredited}, EvidenceRef: "policy/bridge-1"},
		Revision: serviceRevision(t, "service.rule.general", 1), EvidenceRef: "policy/seniority-1",
	}
}

func validServiceModel(t *testing.T) ServiceModel {
	t.Helper()
	periods := []ServicePeriod{
		servicePeriod(t, "period-a", "2026-01-01", "2026-03-01", employmentCredit(t, CreditEmployment, "evidence/employment-a", "")),
		servicePeriod(t, "period-b", "2026-03-05", "2026-05-01", employmentCredit(t, CreditEmployment, "evidence/employment-b", "")),
	}
	model, err := NewServiceModel(periods, []SeniorityRule{serviceRule(t)})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func TestServiceModelRejectsOverlappingUnattributedPeriodsAndImplicitSeniority(t *testing.T) {
	first := servicePeriod(t, "period-a", "2026-01-01", "2026-03-01", employmentCredit(t, CreditEmployment, "evidence/a", ""))
	overlap := servicePeriod(t, "period-overlap", "2026-02-01", "2026-04-01", employmentCredit(t, CreditEmployment, "evidence/overlap", ""))
	if _, err := NewServiceModel([]ServicePeriod{first, overlap}, []SeniorityRule{serviceRule(t)}); !errors.Is(err, ErrOverlappingPeriods) {
		t.Fatalf("overlap error = %v, want ErrOverlappingPeriods", err)
	}

	unattributed := employmentCredit(t, CreditAcquiredService, "evidence/acquired", "")
	if _, err := NewServiceModel([]ServicePeriod{servicePeriod(t, "period-acquired", "2026-01-01", "2026-02-01", unattributed)}, []SeniorityRule{serviceRule(t)}); !errors.Is(err, ErrInvalidCreditSource) || !strings.Contains(err.Error(), "authority_ref") {
		t.Fatalf("unattributed credit error = %v, want authority_ref validation", err)
	}

	if _, err := NewServiceModel([]ServicePeriod{first}, nil); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("implicit seniority error = %v, want ErrInvalidService", err)
	}

	model := validServiceModel(t)
	asOf, err := NewAsOf(serviceDate(t, "2026-05-01"), serviceKnown(t))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Measures) != 1 || snapshot.Measures[0].BridgedDays != 4 || snapshot.Measures[0].RoundedUnits != 4 {
		t.Fatalf("seniority measure = %+v, want four bridged days and four rounded months", snapshot.Measures)
	}
	if snapshot.Canonical() == nil || snapshot.CanonicalDigest == "" {
		t.Fatal("valid seniority snapshot did not produce canonical bytes and digest")
	}
	if explanation, err := model.Explain(); err != nil || explanation.CanonicalDigest == "" || explanation.PeriodCount != 2 {
		t.Fatalf("service explanation = %+v, err=%v", explanation, err)
	}
}

func TestTodo_SERVICE_001_Property(t *testing.T) {
	TestServiceModelRejectsOverlappingUnattributedPeriodsAndImplicitSeniority(t)
}
func TestTodo_SERVICE_001_Golden(t *testing.T) {
	TestServiceModelRejectsOverlappingUnattributedPeriodsAndImplicitSeniority(t)
}
func TestTodo_SERVICE_001_Race(t *testing.T) {
	TestServiceModelRejectsOverlappingUnattributedPeriodsAndImplicitSeniority(t)
}
func TestTodo_SERVICE_001_Fault(t *testing.T) {
	TestServiceModelRejectsOverlappingUnattributedPeriodsAndImplicitSeniority(t)
}
func TestTodo_SERVICE_001_Security(t *testing.T) {
	TestServiceModelRejectsOverlappingUnattributedPeriodsAndImplicitSeniority(t)
}
func TestTodo_SERVICE_001_Conformance(t *testing.T) {
	TestServiceModelRejectsOverlappingUnattributedPeriodsAndImplicitSeniority(t)
}
func TestTodo_SERVICE_001_Mutation(t *testing.T) {
	TestServiceModelRejectsOverlappingUnattributedPeriodsAndImplicitSeniority(t)
}
