package payroll

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	domainpayroll "github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/cycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// EffectiveDateStatus is the payroll consequence of comparing a requested
// effective date with the frozen period and its already-resolved cutoff.
type EffectiveDateStatus string

const (
	EffectiveDateCurrent     EffectiveDateStatus = "CURRENT"
	EffectiveDateRetroactive EffectiveDateStatus = "RETROACTIVE"
	EffectiveDateInvalid     EffectiveDateStatus = "INVALID"

	// RetroExpectation names the governed follow-up for a date in a period
	// whose payroll cutoff has passed. It is intentionally not CURRENT.
	RetroExpectation = "RETROACTIVE_PAYROLL_CORRECTION_REQUIRED"
)

var (
	ErrInvalidCutoffFixture = errors.New("payroll conformance: invalid cutoff fixture")
	ErrEffectiveDateOutside = errors.New("payroll conformance: effective date is outside pay period")
)

// PayPeriod is the small, immutable period declaration needed by this
// conformance fixture. The payroll domain remains the owner of the period
// identity and digest; this package owns only the date interval used to test
// promotion compatibility.
type PayPeriod struct {
	Ref   domainpayroll.PeriodRef
	Start values.LocalDate
	End   values.LocalDate
}

// CutoffFixture is a version-pinned payroll period and its single resolved
// cutoff instant. NewCutoffFixture resolves the local cutoff once and stores
// that result; classification never re-resolves a wall-clock boundary.
type CutoffFixture struct {
	Name     string
	Period   PayPeriod
	Calendar cycle.TenantCalendar
	Rule     cycle.CutoffRule
	CutoffAt values.Instant
}

// PayrollCutoffFact is the typed fact passed by the conformance adapter to
// governance revalidation. Its evidence identity includes the period,
// cutoff, requested date, and classification so a changed payroll boundary
// cannot reproduce the historical approval.
type PayrollCutoffFact struct {
	PeriodID      string
	PeriodVersion string
	EffectiveDate values.LocalDate
	CutoffAt      values.Instant
	ObservedAt    values.Instant
	Status        EffectiveDateStatus
	EvidenceRef   string
}

// CutoffScenario is one fixed, clock-free acceptance case.
type CutoffScenario struct {
	Fixture      CutoffFixture
	EffectiveOn  values.LocalDate
	ObservedAt   values.Instant
	WantStatus   EffectiveDateStatus
	WantExpected string
}

// NewCutoffFixture validates the payroll period and resolves its declared
// local cutoff exactly once against the supplied versioned calendar.
func NewCutoffFixture(name string, period PayPeriod, calendar cycle.TenantCalendar, rule cycle.CutoffRule) (CutoffFixture, error) {
	if name == "" {
		return CutoffFixture{}, fmt.Errorf("%w: name is required", ErrInvalidCutoffFixture)
	}
	if err := period.Ref.Validate(); err != nil {
		return CutoffFixture{}, fmt.Errorf("%w: period ref: %v", ErrInvalidCutoffFixture, err)
	}
	if err := period.Start.Validate(); err != nil {
		return CutoffFixture{}, fmt.Errorf("%w: period start: %v", ErrInvalidCutoffFixture, err)
	}
	if err := period.End.Validate(); err != nil {
		return CutoffFixture{}, fmt.Errorf("%w: period end: %v", ErrInvalidCutoffFixture, err)
	}
	if period.Start.Compare(period.End) >= 0 {
		return CutoffFixture{}, fmt.Errorf("%w: period must be half-open and non-empty", ErrInvalidCutoffFixture)
	}
	resolution, err := cycle.ResolveCutoff(rule, calendar)
	if err != nil {
		return CutoffFixture{}, fmt.Errorf("%w: resolve cutoff: %v", ErrInvalidCutoffFixture, err)
	}
	if resolution.Status != cycle.CutoffResolved {
		return CutoffFixture{}, fmt.Errorf("%w: cutoff resolution requires review: %s", ErrInvalidCutoffFixture, resolution.Rule)
	}

	return CutoffFixture{
		Name: name, Period: period, Calendar: calendar, Rule: rule,
		CutoffAt: values.NewInstant(resolution.Instant),
	}, nil
}

// Classify compares a requested date and an explicit observation instant.
// Equality with the cutoff is deterministic and remains CURRENT; only an
// observation strictly after the cutoff requires a retroactive correction.
func (f CutoffFixture) Classify(effectiveDate values.LocalDate, observedAt values.Instant) (EffectiveDateStatus, string, error) {
	if err := f.Validate(); err != nil {
		return "", "", err
	}
	if err := effectiveDate.Validate(); err != nil {
		return "", "", fmt.Errorf("%w: effective date: %v", ErrInvalidCutoffFixture, err)
	}
	if err := observedAt.Validate(); err != nil {
		return "", "", fmt.Errorf("%w: observed-at: %v", ErrInvalidCutoffFixture, err)
	}
	if effectiveDate.Compare(f.Period.Start) < 0 || effectiveDate.Compare(f.Period.End) >= 0 {
		return EffectiveDateInvalid, "PAYROLL_DATE_OUTSIDE_PERIOD", nil
	}
	if observedAt.Compare(f.CutoffAt) > 0 {
		return EffectiveDateRetroactive, RetroExpectation, nil
	}
	return EffectiveDateCurrent, "", nil
}

// Fact returns the immutable payroll cutoff fact for one requested date and
// one caller-supplied observation instant.
func (f CutoffFixture) Fact(effectiveDate values.LocalDate, observedAt values.Instant) (PayrollCutoffFact, error) {
	status, expectation, err := f.Classify(effectiveDate, observedAt)
	if err != nil {
		return PayrollCutoffFact{}, err
	}
	return PayrollCutoffFact{
		PeriodID:      f.Period.Ref.ID,
		PeriodVersion: f.Period.Ref.Version,
		EffectiveDate: effectiveDate,
		CutoffAt:      f.CutoffAt,
		ObservedAt:    observedAt,
		Status:        status,
		EvidenceRef:   cutoffEvidence(f, effectiveDate, observedAt, status, expectation),
	}, nil
}

// Validate verifies the stored resolution and all version identities. It does
// not resolve the local cutoff again.
func (f CutoffFixture) Validate() error {
	if f.Name == "" || f.Period.Ref.Validate() != nil || f.Period.Start.Validate() != nil || f.Period.End.Validate() != nil {
		return ErrInvalidCutoffFixture
	}
	if f.Period.Start.Compare(f.Period.End) >= 0 {
		return ErrInvalidCutoffFixture
	}
	if err := f.Calendar.Validate(); err != nil {
		return fmt.Errorf("%w: calendar: %v", ErrInvalidCutoffFixture, err)
	}
	if err := f.Rule.Validate(); err != nil {
		return fmt.Errorf("%w: cutoff rule: %v", ErrInvalidCutoffFixture, err)
	}
	if err := f.CutoffAt.Validate(); err != nil {
		return fmt.Errorf("%w: cutoff instant: %v", ErrInvalidCutoffFixture, err)
	}
	return nil
}

func cutoffEvidence(f CutoffFixture, effectiveDate values.LocalDate, observedAt values.Instant, status EffectiveDateStatus, expectation string) string {
	h := sha256.New()
	for _, part := range []string{
		f.Period.Ref.ID, f.Period.Ref.Version, effectiveDate.String(), f.CutoffAt.String(), observedAt.String(), string(status), expectation,
	} {
		fmt.Fprintf(h, "%d:%s;", len(part), part)
	}
	return "payroll-cutoff:" + hex.EncodeToString(h.Sum(nil))
}

func fixedCalendar() (cycle.TenantCalendar, error) {
	zone := values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}
	return cycle.TenantCalendar{
		Zone: zone, Calendar: values.CalendarRef{Ref: "us-payroll", Version: "2026.1"},
		WorkingWeekdays: map[time.Weekday]bool{
			time.Monday: true, time.Tuesday: true, time.Wednesday: true, time.Thursday: true, time.Friday: true,
		},
	}, nil
}

func fixedInstant(year int, month time.Month, day, hour int) values.Instant {
	return values.NewInstant(time.Date(year, month, day, hour, 0, 0, 0, time.UTC))
}

// GoldenCutoffScenarios returns the three required fixed payroll cutoff
// fixtures: after-cutoff retroactive, before-cutoff current, and exact
// boundary current. No scenario consults the ambient clock.
func GoldenCutoffScenarios() ([]CutoffScenario, error) {
	calendar, err := fixedCalendar()
	if err != nil {
		return nil, err
	}
	date := func(text string) values.LocalDate {
		parsed, parseErr := values.ParseLocalDate(text)
		if parseErr != nil {
			panic(parseErr)
		}
		return parsed
	}
	clock := func(hour int) values.LocalTime {
		parsed, parseErr := values.NewLocalTime(hour, 0, 0, 0)
		if parseErr != nil {
			panic(parseErr)
		}
		return parsed
	}
	makeFixture := func(name, periodID, start, end string, cutoffDate values.LocalDate, cutoffHour int) (CutoffFixture, error) {
		return NewCutoffFixture(name, PayPeriod{
			Ref:   domainpayroll.PeriodRef{ID: periodID, Version: "v1", Digest: "sha256:" + periodID},
			Start: date(start), End: date(end),
		}, calendar, cycle.CutoffRule{
			PhaseID: "payroll-cutoff", NominalDate: cutoffDate, NominalTime: clock(cutoffHour),
			JurisdictionRef: "US-NY", Adjustment: cycle.AdjustmentNone,
		})
	}

	after, err := makeFixture("after-cutoff", "period-2026-10", "2026-10-01", "2026-11-01", date("2026-10-15"), 17)
	if err != nil {
		return nil, err
	}
	before, err := makeFixture("before-cutoff", "period-2026-11", "2026-11-01", "2026-12-01", date("2026-11-15"), 17)
	if err != nil {
		return nil, err
	}
	boundary, err := makeFixture("cutoff-boundary", "period-2026-12", "2026-12-01", "2027-01-01", date("2026-12-15"), 17)
	if err != nil {
		return nil, err
	}

	return []CutoffScenario{
		{Fixture: after, EffectiveOn: date("2026-10-10"), ObservedAt: fixedInstant(2026, time.October, 16, 12), WantStatus: EffectiveDateRetroactive, WantExpected: RetroExpectation},
		{Fixture: before, EffectiveOn: date("2026-11-20"), ObservedAt: fixedInstant(2026, time.November, 15, 21), WantStatus: EffectiveDateCurrent},
		{Fixture: boundary, EffectiveOn: date("2026-12-05"), ObservedAt: boundary.CutoffAt, WantStatus: EffectiveDateCurrent},
	}, nil
}
