// Package stateparams contains kernel-pure, versioned parameters extracted
// from state employment-law research. It is deliberately data-only: it does
// not evaluate payroll, select a jurisdiction, or perform I/O beyond loading
// an explicitly supplied YAML fixture.
package stateparams

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	// SchemaVersion is the version of the YAML and canonical parameter shape.
	SchemaVersion uint32 = 1
	// RegistrySchemaVersion is reserved independently so adding a state row
	// does not change the meaning of a parameter body.
	RegistrySchemaVersion uint32 = 1
)

var (
	ErrInvalidParameterSet = errors.New("stateparams: parameter set is invalid")
	ErrInvalidRegistry     = errors.New("stateparams: state registry is invalid")
	ErrYAML                = errors.New("stateparams: YAML fixture is invalid")
)

// FieldError is a typed refusal. Field always names the offending wire or
// schema field so callers can route the refusal without parsing prose.
type FieldError struct {
	Field  string
	Reason string
	root   error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%v: field %s: %s", e.root, e.Field, e.Reason)
}

func (e *FieldError) Unwrap() error { return e.root }

func field(root error, name, reason string) error {
	return &FieldError{Field: name, Reason: reason, root: root}
}

// ReviewStatus is the review state of one registry row or parameter.
type ReviewStatus string

const (
	Reviewed   ReviewStatus = "REVIEWED"
	Unreviewed ReviewStatus = "UNREVIEWED"
)

// Citation identifies the research file and the exact statutory or regulatory
// section from which a parameter was taken.
type Citation struct {
	SourceFile   string       `json:"source_file" yaml:"source_file"`
	Statute      string       `json:"statute" yaml:"statute"`
	ReviewStatus ReviewStatus `json:"review_status" yaml:"review_status"`
}

func (c Citation) validate(fieldName string) error {
	if strings.TrimSpace(c.SourceFile) == "" {
		return field(ErrInvalidParameterSet, fieldName+".source_file", "is required")
	}
	if strings.TrimSpace(c.Statute) == "" {
		return field(ErrInvalidParameterSet, fieldName+".statute", "is required")
	}
	if c.ReviewStatus != Reviewed && c.ReviewStatus != Unreviewed {
		return field(ErrInvalidParameterSet, fieldName+".review_status", "must be REVIEWED or UNREVIEWED")
	}
	return nil
}

// Version is a stable major/minor version for one parameter set.
type Version struct {
	Major uint32 `json:"major" yaml:"major"`
	Minor uint32 `json:"minor" yaml:"minor"`
}

// WageFloorRule is a typed wage-floor row.
type WageFloorRule struct {
	ID                 string            `json:"id" yaml:"id"`
	FloorAmount        values.Money      `json:"-" yaml:"-"`
	WorkerClass        string            `json:"worker_class" yaml:"worker_class"`
	Basis              string            `json:"basis" yaml:"basis"`
	Indexation         string            `json:"indexation" yaml:"indexation"`
	NextAdjustmentDate *values.LocalDate `json:"-" yaml:"-"`
	Citation           Citation          `json:"citation" yaml:"citation"`
}

// OvertimeThreshold models daily, weekly and consecutive-day triggers plus
// exact tipped and subminimum values. Decimal arithmetic is delegated to the
// kernel values package; this package never converts through float64.
type OvertimeThreshold struct {
	ID                    string         `json:"id" yaml:"id"`
	DailyThresholdHours   *int           `json:"daily_threshold_hours,omitempty" yaml:"daily_threshold_hours,omitempty"`
	WeeklyThresholdHours  int            `json:"weekly_threshold_hours" yaml:"weekly_threshold_hours"`
	ConsecutiveDayTrigger bool           `json:"consecutive_day_trigger" yaml:"consecutive_day_trigger"`
	Multiplier            values.Decimal `json:"-" yaml:"-"`
	TippedRate            *values.Money  `json:"-" yaml:"-"`
	TipCreditMax          *values.Money  `json:"-" yaml:"-"`
	SubminimumClass       []string       `json:"subminimum_class,omitempty" yaml:"subminimum_class,omitempty"`
	Citation              Citation       `json:"citation" yaml:"citation"`
}

// BreakRule is the parameter registry body for the MEAL_REST_BREAK kind.
type BreakRule struct {
	ID              string        `json:"id" yaml:"id"`
	BreakType       string        `json:"break_type" yaml:"break_type"`
	TriggerHours    int           `json:"trigger_hours" yaml:"trigger_hours"`
	DurationMinutes int           `json:"duration_minutes" yaml:"duration_minutes"`
	Paid            bool          `json:"paid" yaml:"paid"`
	PenaltyAmount   *values.Money `json:"-" yaml:"-"`
	Citation        Citation      `json:"citation" yaml:"citation"`
}

// SeparationKind identifies the separation event whose final-pay rule is
// being represented.
type SeparationKind string

const (
	Discharge   SeparationKind = "DISCHARGE"
	Resignation SeparationKind = "RESIGNATION"
	Layoff      SeparationKind = "LAYOFF"
	MassLayoff  SeparationKind = "MASS_LAYOFF"
)

// DeadlineUnit identifies the unit of a final-pay deadline.
type DeadlineUnit string

const (
	CalendarDays DeadlineUnit = "CALENDAR_DAYS"
	BusinessDays DeadlineUnit = "BUSINESS_DAYS"
	WorkingHours DeadlineUnit = "WORKING_HOURS"
)

// DeadlineComparator describes how a deadline is resolved against an
// alternate trigger such as the next regular payday.
type DeadlineComparator string

const (
	EarlierOf DeadlineComparator = "EARLIER_OF"
	LaterOf   DeadlineComparator = "LATER_OF"
	Fixed     DeadlineComparator = "FIXED"
)

// FinalPayDeadline is a separation-kind-specific final-pay parameter.
type FinalPayDeadline struct {
	ID                  string             `json:"id" yaml:"id"`
	SeparationKind      SeparationKind     `json:"separation_kind" yaml:"separation_kind"`
	DeadlineDaysOrHours int                `json:"deadline_days_or_hours" yaml:"deadline_days_or_hours"`
	Unit                DeadlineUnit       `json:"unit" yaml:"unit"`
	Comparator          DeadlineComparator `json:"comparator" yaml:"comparator"`
	AlternateTrigger    string             `json:"alternate_trigger,omitempty" yaml:"alternate_trigger,omitempty"`
	Citation            Citation           `json:"citation" yaml:"citation"`
}

// EffectiveWindow is the business-effective interval for a parameter set.
// EffectiveTo is exclusive when present.
type EffectiveWindow struct {
	From values.LocalDate
	To   *values.LocalDate
}

// Contains reports whether a valid business-effective date is in the window.
func (w EffectiveWindow) Contains(date values.LocalDate) bool {
	if w.From.Validate() != nil || date.Validate() != nil || date.Compare(w.From) < 0 {
		return false
	}
	return w.To == nil || (w.To.Validate() == nil && date.Compare(*w.To) < 0)
}

// ParameterSet is an immutable value after loading. Digest is the SHA-256 of
// its canonical typed contents and excludes Digest itself.
type ParameterSet struct {
	SchemaVersion uint32
	StateCode     string
	Version       Version
	Effective     EffectiveWindow
	KnownAt       values.KnownAt
	WageFloors    []WageFloorRule
	Overtime      []OvertimeThreshold
	Breaks        []BreakRule
	FinalPay      []FinalPayDeadline
	ReviewStatus  ReviewStatus
	Digest        string
}

// StateParameters is the descriptive name for ParameterSet.
type StateParameters = ParameterSet

// OvertimeRule is a compatibility name for OvertimeThreshold.
type OvertimeRule = OvertimeThreshold

// BreakParameter is a compatibility name for BreakRule.
type BreakParameter = BreakRule

// FinalPayRule is a compatibility name for FinalPayDeadline.
type FinalPayRule = FinalPayDeadline

// AppliesOn reports whether the set governs a business-effective date.
func (p ParameterSet) AppliesOn(date values.LocalDate) bool { return p.Effective.Contains(date) }

// Validate reports a typed refusal naming the first invalid field.
func (p ParameterSet) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return field(ErrInvalidParameterSet, "schema_version", fmt.Sprintf("must be %d", SchemaVersion))
	}
	if len(p.StateCode) != 2 || p.StateCode != strings.ToUpper(p.StateCode) {
		return field(ErrInvalidParameterSet, "state_code", "must be an uppercase ISO two-letter code")
	}
	if p.Version.Major == 0 {
		return field(ErrInvalidParameterSet, "version.major", "must be positive")
	}
	if err := p.Effective.From.Validate(); err != nil {
		return field(ErrInvalidParameterSet, "effective_from", err.Error())
	}
	if p.Effective.To != nil {
		if err := p.Effective.To.Validate(); err != nil {
			return field(ErrInvalidParameterSet, "effective_to", err.Error())
		}
		if p.Effective.From.Compare(*p.Effective.To) >= 0 {
			return field(ErrInvalidParameterSet, "effective_to", "must be after effective_from")
		}
	}
	if err := p.KnownAt.Instant().Validate(); err != nil {
		return field(ErrInvalidParameterSet, "known_at", err.Error())
	}
	if p.ReviewStatus != Reviewed && p.ReviewStatus != Unreviewed {
		return field(ErrInvalidParameterSet, "review_status", "must be REVIEWED or UNREVIEWED")
	}
	for i := range p.WageFloors {
		if err := p.WageFloors[i].validate(i); err != nil {
			return err
		}
	}
	for i := range p.Overtime {
		if err := p.Overtime[i].validate(i); err != nil {
			return err
		}
	}
	for i := range p.Breaks {
		if err := p.Breaks[i].validate(i); err != nil {
			return err
		}
	}
	for i := range p.FinalPay {
		if err := p.FinalPay[i].validate(i); err != nil {
			return err
		}
	}
	return nil
}

func (w WageFloorRule) validate(i int) error {
	prefix := fmt.Sprintf("wage_floors[%d]", i)
	if w.ID == "" {
		return field(ErrInvalidParameterSet, prefix+".id", "is required")
	}
	if err := w.FloorAmount.Validate(); err != nil {
		return field(ErrInvalidParameterSet, prefix+".floor_amount", err.Error())
	}
	if w.WorkerClass == "" {
		return field(ErrInvalidParameterSet, prefix+".worker_class", "is required")
	}
	if !oneOf(w.Basis, "HOURLY", "WEEKLY", "ANNUAL") {
		return field(ErrInvalidParameterSet, prefix+".basis", "must be HOURLY, WEEKLY, or ANNUAL")
	}
	if !oneOf(w.Indexation, "NONE", "CPI", "SCHEDULE") {
		return field(ErrInvalidParameterSet, prefix+".indexation", "must be NONE, CPI, or SCHEDULE")
	}
	return w.Citation.validate(prefix + ".citation")
}

func (o OvertimeThreshold) validate(i int) error {
	prefix := fmt.Sprintf("overtime[%d]", i)
	if o.ID == "" {
		return field(ErrInvalidParameterSet, prefix+".id", "is required")
	}
	if o.WeeklyThresholdHours <= 0 {
		return field(ErrInvalidParameterSet, prefix+".weekly_threshold_hours", "must be positive")
	}
	if o.DailyThresholdHours != nil && *o.DailyThresholdHours <= 0 {
		return field(ErrInvalidParameterSet, prefix+".daily_threshold_hours", "must be positive when present")
	}
	if err := o.Multiplier.Validate(); err != nil {
		return field(ErrInvalidParameterSet, prefix+".multiplier", err.Error())
	}
	if o.Multiplier.Cmp(zeroDecimal()) <= 0 {
		return field(ErrInvalidParameterSet, prefix+".multiplier", "must be positive")
	}
	if o.TippedRate != nil {
		if err := o.TippedRate.Validate(); err != nil {
			return field(ErrInvalidParameterSet, prefix+".tipped_rate", err.Error())
		}
	}
	if o.TipCreditMax != nil {
		if err := o.TipCreditMax.Validate(); err != nil {
			return field(ErrInvalidParameterSet, prefix+".tip_credit_max", err.Error())
		}
	}
	return o.Citation.validate(prefix + ".citation")
}

func (b BreakRule) validate(i int) error {
	prefix := fmt.Sprintf("breaks[%d]", i)
	if b.ID == "" {
		return field(ErrInvalidParameterSet, prefix+".id", "is required")
	}
	if !oneOf(b.BreakType, "MEAL", "REST") {
		return field(ErrInvalidParameterSet, prefix+".break_type", "must be MEAL or REST")
	}
	if b.TriggerHours <= 0 {
		return field(ErrInvalidParameterSet, prefix+".trigger_hours", "must be positive")
	}
	if b.DurationMinutes <= 0 {
		return field(ErrInvalidParameterSet, prefix+".duration_minutes", "must be positive")
	}
	if b.PenaltyAmount != nil {
		if err := b.PenaltyAmount.Validate(); err != nil {
			return field(ErrInvalidParameterSet, prefix+".penalty_amount", err.Error())
		}
	}
	return b.Citation.validate(prefix + ".citation")
}

func (f FinalPayDeadline) validate(i int) error {
	prefix := fmt.Sprintf("final_pay[%d]", i)
	if f.ID == "" {
		return field(ErrInvalidParameterSet, prefix+".id", "is required")
	}
	if !oneOf(string(f.SeparationKind), string(Discharge), string(Resignation), string(Layoff), string(MassLayoff)) {
		return field(ErrInvalidParameterSet, prefix+".separation_kind", "is not a supported separation kind")
	}
	if f.DeadlineDaysOrHours < 0 {
		return field(ErrInvalidParameterSet, prefix+".deadline_days_or_hours", "cannot be negative")
	}
	if !oneOf(string(f.Unit), string(CalendarDays), string(BusinessDays), string(WorkingHours)) {
		return field(ErrInvalidParameterSet, prefix+".unit", "is not a supported deadline unit")
	}
	if !oneOf(string(f.Comparator), string(EarlierOf), string(LaterOf), string(Fixed)) {
		return field(ErrInvalidParameterSet, prefix+".comparator", "is not a supported comparator")
	}
	if f.Comparator != Fixed && strings.TrimSpace(f.AlternateTrigger) == "" {
		return field(ErrInvalidParameterSet, prefix+".alternate_trigger", "is required for EARLIER_OF or LATER_OF")
	}
	return f.Citation.validate(prefix + ".citation")
}

func oneOf(got string, allowed ...string) bool {
	for _, want := range allowed {
		if got == want {
			return true
		}
	}
	return false
}

func zeroDecimal() values.Decimal {
	d, _ := values.NewDecimal("0", 4, values.RoundingHalfEven)
	return d
}

// ComputeDigest returns the canonical SHA-256 digest of the typed set.
func (p ParameterSet) ComputeDigest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(p.canonical())
	if err != nil {
		return "", fmt.Errorf("stateparams: canonical parameter set: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Explain renders metadata only; it never includes the research prose note.
func (p ParameterSet) Explain() string {
	return fmt.Sprintf("state_parameters state=%s version=%d.%d effective=%s known_at=%s wage_floors=%d overtime=%d breaks=%d final_pay=%d review_status=%s digest=%s",
		p.StateCode, p.Version.Major, p.Version.Minor, p.Effective.From, p.KnownAt,
		len(p.WageFloors), len(p.Overtime), len(p.Breaks), len(p.FinalPay), p.ReviewStatus, p.Digest)
}

// StateRegistryRow records one state, its research citation and review flag.
type StateRegistryRow struct {
	StateCode       string       `json:"state_code" yaml:"state_code"`
	StateName       string       `json:"state_name" yaml:"state_name"`
	SourceFile      string       `json:"source_file" yaml:"source_file"`
	StatuteCitation string       `json:"statute_citation" yaml:"statute_citation"`
	ReviewStatus    ReviewStatus `json:"review_status" yaml:"review_status"`
}

// StateRegistry is the 50-state citation and review registry.
type StateRegistry struct {
	SchemaVersion uint32
	Rows          []StateRegistryRow
	Digest        string
}

func (r StateRegistry) Validate() error {
	if r.SchemaVersion != RegistrySchemaVersion {
		return field(ErrInvalidRegistry, "schema_version", fmt.Sprintf("must be %d", RegistrySchemaVersion))
	}
	if len(r.Rows) != 50 {
		return field(ErrInvalidRegistry, "rows", fmt.Sprintf("must contain one row per state (got %d)", len(r.Rows)))
	}
	seen := map[string]bool{}
	for i, row := range r.Rows {
		prefix := fmt.Sprintf("rows[%d]", i)
		if len(row.StateCode) != 2 || row.StateCode != strings.ToUpper(row.StateCode) {
			return field(ErrInvalidRegistry, prefix+".state_code", "must be uppercase ISO two-letter code")
		}
		if seen[row.StateCode] {
			return field(ErrInvalidRegistry, prefix+".state_code", "is duplicated")
		}
		seen[row.StateCode] = true
		if row.StateName == "" {
			return field(ErrInvalidRegistry, prefix+".state_name", "is required")
		}
		if row.SourceFile == "" {
			return field(ErrInvalidRegistry, prefix+".source_file", "is required")
		}
		if row.StatuteCitation == "" {
			return field(ErrInvalidRegistry, prefix+".statute_citation", "is required")
		}
		if row.ReviewStatus != Reviewed && row.ReviewStatus != Unreviewed {
			return field(ErrInvalidRegistry, prefix+".review_status", "must be REVIEWED or UNREVIEWED")
		}
	}
	return nil
}

// Lookup returns one registry row by state code.
func (r StateRegistry) Lookup(stateCode string) (StateRegistryRow, bool) {
	for _, row := range r.Rows {
		if row.StateCode == stateCode {
			return row, true
		}
	}
	return StateRegistryRow{}, false
}

// ComputeDigest returns the canonical digest of the registry rows sorted by
// state code, so fixture row order cannot change the registry identity.
func (r StateRegistry) ComputeDigest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	rows := append([]StateRegistryRow(nil), r.Rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].StateCode < rows[j].StateCode })
	raw, err := json.Marshal(struct {
		SchemaVersion uint32             `json:"schema_version"`
		Rows          []StateRegistryRow `json:"rows"`
	}{r.SchemaVersion, rows})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Fixture is the complete checked-in YAML artifact consumed by tests and
// extraction tooling.
type Fixture struct {
	Registry      StateRegistry
	ParameterSets []ParameterSet
}

func (f Fixture) Validate() error {
	if err := f.Registry.Validate(); err != nil {
		return err
	}
	for i := range f.ParameterSets {
		if err := f.ParameterSets[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}

// LoadYAML parses a strict, explicit YAML fixture and computes its digests.
func LoadYAML(data []byte) (Fixture, error) {
	var raw yamlFixture
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Fixture{}, fmt.Errorf("%w: %v", ErrYAML, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return Fixture{}, fmt.Errorf("%w: trailing content", ErrYAML)
	}
	fixture, err := raw.convert()
	if err != nil {
		return Fixture{}, err
	}
	if err := fixture.Validate(); err != nil {
		return Fixture{}, err
	}
	registryDigest, err := fixture.Registry.ComputeDigest()
	if err != nil {
		return Fixture{}, err
	}
	fixture.Registry.Digest = registryDigest
	for i := range fixture.ParameterSets {
		digest, err := fixture.ParameterSets[i].ComputeDigest()
		if err != nil {
			return Fixture{}, err
		}
		fixture.ParameterSets[i].Digest = digest
	}
	return fixture, nil
}

// LoadYAMLFile loads a fixture from an explicit path.
func LoadYAMLFile(path string) (Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, fmt.Errorf("%w: %v", ErrYAML, err)
	}
	return LoadYAML(data)
}

// Load is a short spelling of LoadYAML.
func Load(data []byte) (Fixture, error) { return LoadYAML(data) }

// LoadFile is a short spelling of LoadYAMLFile.
func LoadFile(path string) (Fixture, error) { return LoadYAMLFile(path) }

// Explain is the package-level Explain-shaped helper for audit callers.
func Explain(p ParameterSet) string { return p.Explain() }

type yamlMoney struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}
type yamlCitation struct {
	SourceFile   string       `json:"source_file"`
	Statute      string       `json:"statute"`
	ReviewStatus ReviewStatus `json:"review_status"`
}
type yamlWageFloor struct {
	ID                 string       `json:"id"`
	FloorAmount        yamlMoney    `json:"floor_amount"`
	WorkerClass        string       `json:"worker_class"`
	Basis              string       `json:"basis"`
	Indexation         string       `json:"indexation"`
	NextAdjustmentDate string       `json:"next_adjustment_date"`
	Citation           yamlCitation `json:"citation"`
}
type yamlOvertime struct {
	ID                    string       `json:"id"`
	DailyThresholdHours   *int         `json:"daily_threshold_hours"`
	WeeklyThresholdHours  int          `json:"weekly_threshold_hours"`
	ConsecutiveDayTrigger bool         `json:"consecutive_day_trigger"`
	Multiplier            string       `json:"multiplier"`
	TippedRate            *yamlMoney   `json:"tipped_rate"`
	TipCreditMax          *yamlMoney   `json:"tip_credit_max"`
	SubminimumClass       []string     `json:"subminimum_class"`
	Citation              yamlCitation `json:"citation"`
}
type yamlBreak struct {
	ID              string       `json:"id"`
	BreakType       string       `json:"break_type"`
	TriggerHours    int          `json:"trigger_hours"`
	DurationMinutes int          `json:"duration_minutes"`
	Paid            bool         `json:"paid"`
	PenaltyAmount   *yamlMoney   `json:"penalty_amount"`
	Citation        yamlCitation `json:"citation"`
}
type yamlFinalPay struct {
	ID                  string             `json:"id"`
	SeparationKind      SeparationKind     `json:"separation_kind"`
	DeadlineDaysOrHours int                `json:"deadline_days_or_hours"`
	Unit                DeadlineUnit       `json:"unit"`
	Comparator          DeadlineComparator `json:"comparator"`
	AlternateTrigger    string             `json:"alternate_trigger"`
	Citation            yamlCitation       `json:"citation"`
}
type yamlParameterSet struct {
	SchemaVersion uint32          `json:"schema_version"`
	StateCode     string          `json:"state_code"`
	Version       Version         `json:"version"`
	EffectiveFrom string          `json:"effective_from"`
	EffectiveTo   string          `json:"effective_to"`
	KnownAt       string          `json:"known_at"`
	ReviewStatus  ReviewStatus    `json:"review_status"`
	WageFloors    []yamlWageFloor `json:"wage_floors"`
	Overtime      []yamlOvertime  `json:"overtime"`
	Breaks        []yamlBreak     `json:"breaks"`
	FinalPay      []yamlFinalPay  `json:"final_pay"`
}
type yamlRegistryRow struct {
	StateCode       string       `json:"state_code"`
	StateName       string       `json:"state_name"`
	SourceFile      string       `json:"source_file"`
	StatuteCitation string       `json:"statute_citation"`
	ReviewStatus    ReviewStatus `json:"review_status"`
}
type yamlFixture struct {
	SchemaVersion uint32             `json:"schema_version"`
	Registry      []yamlRegistryRow  `json:"registry"`
	ParameterSets []yamlParameterSet `json:"parameter_sets"`
}

func (c yamlCitation) citation() Citation {
	return Citation{SourceFile: c.SourceFile, Statute: c.Statute, ReviewStatus: c.ReviewStatus}
}
func parseMoney(m *yamlMoney, name string) (values.Money, error) {
	if m == nil {
		return values.Money{}, nil
	}
	out, err := values.NewMoney(m.Amount, m.Currency, 2, values.RoundingHalfEven)
	if err != nil {
		return values.Money{}, field(ErrInvalidParameterSet, name, err.Error())
	}
	return out, nil
}
func parseDate(s, name string) (*values.LocalDate, error) {
	if s == "" {
		return nil, nil
	}
	d, err := values.ParseLocalDate(s)
	if err != nil {
		return nil, field(ErrInvalidParameterSet, name, err.Error())
	}
	return &d, nil
}
func parseKnownAt(s string) (values.KnownAt, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return values.KnownAt{}, field(ErrInvalidParameterSet, "known_at", err.Error())
	}
	out, err := values.NewKnownAt(values.NewInstant(t))
	if err != nil {
		return values.KnownAt{}, field(ErrInvalidParameterSet, "known_at", err.Error())
	}
	return out, nil
}
func (r yamlFixture) convert() (Fixture, error) {
	if r.SchemaVersion != SchemaVersion {
		return Fixture{}, field(ErrYAML, "schema_version", fmt.Sprintf("must be %d", SchemaVersion))
	}
	out := Fixture{Registry: StateRegistry{SchemaVersion: RegistrySchemaVersion}}
	for _, row := range r.Registry {
		out.Registry.Rows = append(out.Registry.Rows, StateRegistryRow{row.StateCode, row.StateName, row.SourceFile, row.StatuteCitation, row.ReviewStatus})
	}
	for _, raw := range r.ParameterSets {
		from, err := values.ParseLocalDate(raw.EffectiveFrom)
		if err != nil {
			return Fixture{}, field(ErrInvalidParameterSet, "effective_from", err.Error())
		}
		to, err := parseDate(raw.EffectiveTo, "effective_to")
		if err != nil {
			return Fixture{}, err
		}
		known, err := parseKnownAt(raw.KnownAt)
		if err != nil {
			return Fixture{}, err
		}
		set := ParameterSet{SchemaVersion: raw.SchemaVersion, StateCode: raw.StateCode, Version: raw.Version, Effective: EffectiveWindow{From: from, To: to}, KnownAt: known, ReviewStatus: raw.ReviewStatus}
		for _, w := range raw.WageFloors {
			amount, e := parseMoney(&w.FloorAmount, "wage_floors.floor_amount")
			if e != nil {
				return Fixture{}, e
			}
			next, e := parseDate(w.NextAdjustmentDate, "wage_floors.next_adjustment_date")
			if e != nil {
				return Fixture{}, e
			}
			set.WageFloors = append(set.WageFloors, WageFloorRule{ID: w.ID, FloorAmount: amount, WorkerClass: w.WorkerClass, Basis: w.Basis, Indexation: w.Indexation, NextAdjustmentDate: next, Citation: w.Citation.citation()})
		}
		for _, o := range raw.Overtime {
			mult, e := values.NewDecimal(o.Multiplier, 4, values.RoundingHalfEven)
			if e != nil {
				return Fixture{}, field(ErrInvalidParameterSet, "overtime.multiplier", e.Error())
			}
			tr, e := parseMoney(o.TippedRate, "overtime.tipped_rate")
			if e != nil {
				return Fixture{}, e
			}
			tc, e := parseMoney(o.TipCreditMax, "overtime.tip_credit_max")
			if e != nil {
				return Fixture{}, e
			}
			item := OvertimeThreshold{ID: o.ID, DailyThresholdHours: o.DailyThresholdHours, WeeklyThresholdHours: o.WeeklyThresholdHours, ConsecutiveDayTrigger: o.ConsecutiveDayTrigger, Multiplier: mult, SubminimumClass: o.SubminimumClass, Citation: o.Citation.citation()}
			if o.TippedRate != nil {
				item.TippedRate = &tr
			}
			if o.TipCreditMax != nil {
				item.TipCreditMax = &tc
			}
			set.Overtime = append(set.Overtime, item)
		}
		for _, b := range raw.Breaks {
			amount, e := parseMoney(b.PenaltyAmount, "breaks.penalty_amount")
			if e != nil {
				return Fixture{}, e
			}
			item := BreakRule{ID: b.ID, BreakType: b.BreakType, TriggerHours: b.TriggerHours, DurationMinutes: b.DurationMinutes, Paid: b.Paid, Citation: b.Citation.citation()}
			if b.PenaltyAmount != nil {
				item.PenaltyAmount = &amount
			}
			set.Breaks = append(set.Breaks, item)
		}
		for _, f := range raw.FinalPay {
			set.FinalPay = append(set.FinalPay, FinalPayDeadline{ID: f.ID, SeparationKind: f.SeparationKind, DeadlineDaysOrHours: f.DeadlineDaysOrHours, Unit: f.Unit, Comparator: f.Comparator, AlternateTrigger: f.AlternateTrigger, Citation: f.Citation.citation()})
		}
		out.ParameterSets = append(out.ParameterSets, set)
	}
	return out, nil
}

type canonicalSet struct {
	SchemaVersion uint32              `json:"schema_version"`
	StateCode     string              `json:"state_code"`
	Version       Version             `json:"version"`
	EffectiveFrom string              `json:"effective_from"`
	EffectiveTo   string              `json:"effective_to,omitempty"`
	KnownAt       string              `json:"known_at"`
	ReviewStatus  ReviewStatus        `json:"review_status"`
	WageFloors    []canonicalWage     `json:"wage_floors"`
	Overtime      []canonicalOvertime `json:"overtime"`
	Breaks        []canonicalBreak    `json:"breaks"`
	FinalPay      []FinalPayDeadline  `json:"final_pay"`
}
type canonicalWage struct {
	ID, FloorAmount, Currency, WorkerClass, Basis, Indexation, NextAdjustmentDate string
	Citation                                                                      Citation
}
type canonicalOvertime struct {
	ID                       string
	DailyThresholdHours      *int
	WeeklyThresholdHours     int
	ConsecutiveDayTrigger    bool
	Multiplier               string
	TippedRate, TipCreditMax string
	SubminimumClass          []string
	Citation                 Citation
}
type canonicalBreak struct {
	ID, BreakType                 string
	TriggerHours, DurationMinutes int
	Paid                          bool
	PenaltyAmount                 string
	Citation                      Citation
}

func (p ParameterSet) canonical() canonicalSet {
	var to string
	if p.Effective.To != nil {
		to = p.Effective.To.String()
	}
	w := make([]canonicalWage, len(p.WageFloors))
	for i, x := range p.WageFloors {
		next := ""
		if x.NextAdjustmentDate != nil {
			next = x.NextAdjustmentDate.String()
		}
		w[i] = canonicalWage{x.ID, x.FloorAmount.String(), x.FloorAmount.Currency(), x.WorkerClass, x.Basis, x.Indexation, next, x.Citation}
	}
	o := make([]canonicalOvertime, len(p.Overtime))
	for i, x := range p.Overtime {
		tr, tc := "", ""
		if x.TippedRate != nil {
			tr = x.TippedRate.String()
		}
		if x.TipCreditMax != nil {
			tc = x.TipCreditMax.String()
		}
		o[i] = canonicalOvertime{x.ID, x.DailyThresholdHours, x.WeeklyThresholdHours, x.ConsecutiveDayTrigger, x.Multiplier.String(), tr, tc, x.SubminimumClass, x.Citation}
	}
	b := make([]canonicalBreak, len(p.Breaks))
	for i, x := range p.Breaks {
		penalty := ""
		if x.PenaltyAmount != nil {
			penalty = x.PenaltyAmount.String()
		}
		b[i] = canonicalBreak{x.ID, x.BreakType, x.TriggerHours, x.DurationMinutes, x.Paid, penalty, x.Citation}
	}
	return canonicalSet{p.SchemaVersion, p.StateCode, p.Version, p.Effective.From.String(), to, p.KnownAt.String(), p.ReviewStatus, w, o, b, p.FinalPay}
}
