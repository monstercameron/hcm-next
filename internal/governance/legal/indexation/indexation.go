// Package indexation owns the pure, versioned schedule of effective legal
// parameter changes. It consumes declared index-series facts; it never reads
// a clock, a database, or an external index provider.
package indexation

import (
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

//go:embed testdata/indexation.yaml
var fixtureFS embed.FS

const schemaVersion = 1

// Version is the schema version for this package's canonical records.
func Version() int { return schemaVersion }

var (
	ErrValidation       = errors.New("indexation: PACK_VALIDATION_FAILED")
	ErrCoverageUnknown  = errors.New("indexation: RULE_COVERAGE_UNKNOWN")
	ErrFixtureMalformed = errors.New("indexation: YAML fixture is malformed")
)

// Refusal is a typed, field-addressed refusal. Field is always populated for
// input validation and stale-index failures.
type Refusal struct {
	Code   string
	Field  string
	Detail string
	Cause  error
}

func (e *Refusal) Error() string {
	return fmt.Sprintf("indexation: %s [%s]: %s", e.Code, e.Field, e.Detail)
}

func (e *Refusal) Unwrap() error { return e.Cause }

func refuse(cause error, code, field, detail string) error {
	return &Refusal{Code: code, Field: field, Detail: detail, Cause: cause}
}

// IndexSource identifies the declared source series. The spelling is stable
// because it is part of the parameter digest and audit explanation.
type IndexSource string

const (
	IndexSourceNone                   IndexSource = "NONE"
	IndexSourceCPIW                   IndexSource = "CPI_W"
	IndexSourceCPIU                   IndexSource = "CPI_U"
	IndexSourceECI                    IndexSource = "ECI"
	IndexSourceFixedSchedule          IndexSource = "FIXED_SCHEDULE"
	IndexSourceVoterInitiativeFormula IndexSource = "VOTER_INITIATIVE_FORMULA"
)

func (s IndexSource) valid() bool {
	switch s {
	case IndexSourceNone, IndexSourceCPIW, IndexSourceCPIU, IndexSourceECI,
		IndexSourceFixedSchedule, IndexSourceVoterInitiativeFormula:
		return true
	default:
		return false
	}
}

// ReviewFlag records whether a state row has been reviewed in the research
// corpus. It is intentionally not a legal approval claim.
type ReviewFlag string

const (
	ReviewReviewed   ReviewFlag = "REVIEWED"
	ReviewUnreviewed ReviewFlag = "UNREVIEWED"
)

// StateRow is the indexation registry row for one US state.
type StateRow struct {
	State           string
	StatuteCitation string
	Review          ReviewFlag
}

// IndexPoint is a declared observation or fixed step effective on Date.
// Value is an index reading for CPI/ECI sources; Amount is used by fixed
// schedules. The two are kept separate so a missing field cannot silently
// become zero.
type IndexPoint struct {
	ParameterID string
	Date        values.LocalDate
	Source      IndexSource
	Value       values.Decimal
	Amount      values.Money
}

// Parameter is one typed legal amount and its effective-dating rule.
type Parameter struct {
	ID                 string
	Jurisdiction       string
	EffectiveDate      values.LocalDate
	KnownAt            values.KnownAt
	Amount             values.Money
	IndexSource        IndexSource
	IndexFormula       string
	BaseIndex          values.Decimal
	NextAdjustmentDate values.LocalDate
	Citation           string
}

// ParameterSet is a versioned and digested schema loaded from a YAML fixture.
type ParameterSet struct {
	SchemaVersion int
	ID            string
	Version       string
	Parameters    []Parameter
	IndexPoints   []IndexPoint
	Registry      []StateRow
}

// ScheduleOccurrenceReference is the additive hand-off to the scheduling
// engine. The indexation package creates no trigger and performs no dispatch;
// it gives a caller a stable reference that can be attached to a published
// internal/engines/schedule occurrence.
type ScheduleOccurrenceReference struct {
	EnginePackage string
	ParameterID   string
	OccurrenceKey string
	EffectiveDate values.LocalDate
}

// ScheduledValue is one deterministic next effective value.
type ScheduledValue struct {
	ParameterID   string
	EffectiveDate values.LocalDate
	Amount        values.Money
	Source        IndexSource
	Formula       string
	Occurrence    ScheduleOccurrenceReference
}

// ScheduleResult is immutable by convention and carries its canonical digest.
type ScheduleResult struct {
	ParameterID string
	AsOf        values.LocalDate
	Values      []ScheduledValue
	Digest      string
}

// Validate validates every typed field and every state registry row.
func (p ParameterSet) Validate() error {
	if p.SchemaVersion != schemaVersion {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", "schema_version", "must be 1")
	}
	if strings.TrimSpace(p.ID) == "" {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", "id", "is required")
	}
	if strings.TrimSpace(p.Version) == "" {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", "version", "is required")
	}
	if len(p.Registry) != 50 {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", "registry", "must contain one row per US state")
	}
	seenStates := map[string]bool{}
	for i, row := range p.Registry {
		if len(row.State) != 2 || row.State != strings.ToUpper(row.State) {
			return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("registry[%d].state", i), "must be an uppercase state code")
		}
		if seenStates[row.State] {
			return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("registry[%d].state", i), "is duplicated")
		}
		seenStates[row.State] = true
		if strings.TrimSpace(row.StatuteCitation) == "" {
			return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("registry[%d].statute_citation", i), "is required")
		}
		if row.Review != ReviewReviewed && row.Review != ReviewUnreviewed {
			return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("registry[%d].review", i), "must be REVIEWED or UNREVIEWED")
		}
	}
	seen := map[string]bool{}
	for i, param := range p.Parameters {
		if err := param.validate(i); err != nil {
			return err
		}
		if seen[param.ID] {
			return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("parameters[%d].id", i), "is duplicated")
		}
		seen[param.ID] = true
	}
	for i, point := range p.IndexPoints {
		if strings.TrimSpace(point.ParameterID) == "" {
			return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("index_points[%d].parameter_id", i), "is required")
		}
		if err := point.Date.Validate(); err != nil {
			return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("index_points[%d].date", i), err.Error())
		}
		if !point.Source.valid() {
			return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("index_points[%d].source", i), "is unknown")
		}
		if point.Source == IndexSourceFixedSchedule {
			if err := point.Amount.Validate(); err != nil {
				return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("index_points[%d].amount", i), "is required for FIXED_SCHEDULE")
			}
		} else if point.Source != IndexSourceNone {
			if err := point.Value.Validate(); err != nil {
				return refuse(ErrValidation, "PACK_VALIDATION_FAILED", fmt.Sprintf("index_points[%d].value", i), "is required for indexed sources")
			}
		}
	}
	return nil
}

func (p Parameter) validate(i int) error {
	field := func(name string) string { return fmt.Sprintf("parameters[%d].%s", i, name) }
	if strings.TrimSpace(p.ID) == "" {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", field("id"), "is required")
	}
	if strings.TrimSpace(p.Jurisdiction) == "" {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", field("jurisdiction"), "is required")
	}
	if err := p.EffectiveDate.Validate(); err != nil {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", field("effective_date"), err.Error())
	}
	if err := p.KnownAt.Instant().Validate(); err != nil {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", field("known_at"), err.Error())
	}
	if err := p.Amount.Validate(); err != nil {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", field("amount"), err.Error())
	}
	if !p.IndexSource.valid() {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", field("index_source"), "is unknown")
	}
	if p.IndexSource != IndexSourceNone && strings.TrimSpace(p.IndexFormula) == "" {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", field("index_formula"), "is required for an indexed parameter")
	}
	if p.NextAdjustmentDate.IsSet() {
		if p.NextAdjustmentDate.Compare(p.EffectiveDate) <= 0 {
			return refuse(ErrValidation, "PACK_VALIDATION_FAILED", field("next_adjustment_date"), "must be after effective_date")
		}
	}
	if strings.TrimSpace(p.Citation) == "" {
		return refuse(ErrValidation, "PACK_VALIDATION_FAILED", field("citation"), "is required")
	}
	return nil
}

// Digest returns the lowercase SHA-256 digest of the validated typed content.
func (p ParameterSet) Digest() string {
	if p.Validate() != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "IX1|%d|%s|%s|", p.SchemaVersion, p.ID, p.Version)
	for _, row := range p.Registry {
		fmt.Fprintf(&b, "state=%s|citation=%s|review=%s|", row.State, row.StatuteCitation, row.Review)
	}
	for _, param := range p.Parameters {
		fmt.Fprintf(&b, "param=%s|jurisdiction=%s|effective=%s|known=%s|amount=%s|source=%s|formula=%s|base=%s|next=%s|citation=%s|",
			param.ID, param.Jurisdiction, param.EffectiveDate, param.KnownAt, param.Amount, param.IndexSource, param.IndexFormula, param.BaseIndex, param.NextAdjustmentDate, param.Citation)
	}
	points := append([]IndexPoint(nil), p.IndexPoints...)
	sort.Slice(points, func(i, j int) bool {
		if points[i].ParameterID != points[j].ParameterID {
			return points[i].ParameterID < points[j].ParameterID
		}
		return points[i].Date.Compare(points[j].Date) < 0
	})
	for _, point := range points {
		fmt.Fprintf(&b, "point=%s|date=%s|source=%s|value=%s|amount=%s|", point.ParameterID, point.Date, point.Source, point.Value, point.Amount)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum[:])
}

// Explain returns a deterministic audit summary.
func (p ParameterSet) Explain() string {
	return fmt.Sprintf("indexation set=%s v%s schema=%d parameters=%d registry=%d digest=%s", p.ID, p.Version, p.SchemaVersion, len(p.Parameters), len(p.Registry), p.Digest())
}

// Schedule computes every next declared step after asOf. It uses only series
// points that are known by the parameter's KnownAt time and never uses time.Now.
func (p ParameterSet) Schedule(parameterID string, asOf values.LocalDate) (ScheduleResult, error) {
	if err := p.Validate(); err != nil {
		return ScheduleResult{}, err
	}
	if err := asOf.Validate(); err != nil {
		return ScheduleResult{}, refuse(ErrValidation, "PACK_VALIDATION_FAILED", "as_of", err.Error())
	}
	var param *Parameter
	for i := range p.Parameters {
		if p.Parameters[i].ID == parameterID {
			param = &p.Parameters[i]
			break
		}
	}
	if param == nil {
		return ScheduleResult{}, refuse(ErrCoverageUnknown, "RULE_COVERAGE_UNKNOWN", "parameter_id", "parameter is not registered")
	}
	points := make([]IndexPoint, 0)
	for _, point := range p.IndexPoints {
		if point.ParameterID == parameterID {
			points = append(points, point)
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Date.Compare(points[j].Date) < 0 })
	if param.NextAdjustmentDate.IsSet() {
		found := false
		for _, point := range points {
			if point.Date.Compare(param.NextAdjustmentDate) == 0 {
				found = true
				break
			}
		}
		if !found && asOf.Compare(param.NextAdjustmentDate) >= 0 {
			return ScheduleResult{}, refuse(ErrCoverageUnknown, "RULE_COVERAGE_UNKNOWN", "next_adjustment_date", "index date has lapsed without a superseding effective value")
		}
	}
	current := param.Amount
	currentIndex := param.BaseIndex
	if param.IndexSource != IndexSourceNone && param.IndexSource != IndexSourceFixedSchedule {
		if err := currentIndex.Validate(); err != nil {
			return ScheduleResult{}, refuse(ErrValidation, "PACK_VALIDATION_FAILED", "base_index", "is required for indexed formulas")
		}
	}
	result := ScheduleResult{ParameterID: parameterID, AsOf: asOf}
	for _, point := range points {
		if point.Date.Compare(param.EffectiveDate) <= 0 {
			continue
		}
		amount := current
		switch point.Source {
		case IndexSourceFixedSchedule:
			amount = point.Amount
		case IndexSourceCPIW, IndexSourceCPIU, IndexSourceECI, IndexSourceVoterInitiativeFormula:
			if point.Source != param.IndexSource {
				continue
			}
			if err := point.Value.Validate(); err != nil {
				return ScheduleResult{}, refuse(ErrCoverageUnknown, "RULE_COVERAGE_UNKNOWN", "index_series.value", "declared index point is invalid")
			}
			ratio, err := point.Value.Div(currentIndex, 8, values.RoundingHalfUp)
			if err != nil {
				return ScheduleResult{}, refuse(ErrCoverageUnknown, "RULE_COVERAGE_UNKNOWN", "index_series.value", err.Error())
			}
			updated, err := current.Amount().Mul(ratio, 2, values.RoundingHalfUp)
			if err != nil {
				return ScheduleResult{}, refuse(ErrCoverageUnknown, "RULE_COVERAGE_UNKNOWN", "index_formula", err.Error())
			}
			amount, err = values.NewMoneyFromDecimal(updated, current.Currency())
			if err != nil {
				return ScheduleResult{}, refuse(ErrCoverageUnknown, "RULE_COVERAGE_UNKNOWN", "index_formula", err.Error())
			}
			currentIndex = point.Value
		case IndexSourceNone:
			continue
		default:
			return ScheduleResult{}, refuse(ErrValidation, "PACK_VALIDATION_FAILED", "index_points.source", "unsupported source")
		}
		current = amount
		if point.Date.Compare(asOf) <= 0 {
			continue
		}
		result.Values = append(result.Values, ScheduledValue{
			ParameterID: parameterID, EffectiveDate: point.Date, Amount: amount,
			Source: point.Source, Formula: param.IndexFormula,
			Occurrence: ScheduleOccurrenceReference{
				EnginePackage: "internal/engines/schedule", ParameterID: parameterID,
				OccurrenceKey: parameterID + "@" + point.Date.String(), EffectiveDate: point.Date,
			},
		})
	}
	result.Digest = result.digest()
	return result, nil
}

func (r ScheduleResult) digest() string {
	var b strings.Builder
	fmt.Fprintf(&b, "IXS1|%s|%s|", r.ParameterID, r.AsOf)
	for _, value := range r.Values {
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|", value.ParameterID, value.EffectiveDate, value.Amount, value.Source, value.Occurrence.OccurrenceKey)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum[:])
}

// LoadFixture loads a checked-in YAML fixture using embed, keeping the normal
// loader deterministic and independent of the process working directory.
func LoadFixture(name string) (ParameterSet, error) {
	b, err := fixtureFS.ReadFile("testdata/" + name)
	if err != nil {
		return ParameterSet{}, err
	}
	return LoadYAML(strings.NewReader(string(b)))
}

// LoadYAML loads the deliberately small, dependency-free YAML subset used by
// the checked-in fixture: top-level scalars and lists of flat maps. Quoted
// scalar values, comments, and blank lines are supported.
func LoadYAML(r io.Reader) (ParameterSet, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return ParameterSet{}, err
	}
	top, sections, err := parseFlatYAML(string(b))
	if err != nil {
		return ParameterSet{}, err
	}
	set := ParameterSet{SchemaVersion: atoi(top["schema_version"]), ID: top["id"], Version: top["version"]}
	for _, raw := range sections["registry"] {
		set.Registry = append(set.Registry, StateRow{State: raw["state"], StatuteCitation: raw["citation"], Review: ReviewFlag(raw["review"])})
	}
	knownAt, err := parseKnownAt(top["known_at"])
	if err != nil {
		return ParameterSet{}, refuse(ErrFixtureMalformed, "PACK_VALIDATION_FAILED", "known_at", err.Error())
	}
	for _, raw := range sections["parameters"] {
		amount, err := values.NewMoney(raw["amount"], defaultCurrency(raw["currency"]), 2, values.RoundingHalfUp)
		if err != nil {
			return ParameterSet{}, refuse(ErrFixtureMalformed, "PACK_VALIDATION_FAILED", "parameters.amount", err.Error())
		}
		effective, err := values.ParseLocalDate(raw["effective_date"])
		if err != nil {
			return ParameterSet{}, refuse(ErrFixtureMalformed, "PACK_VALIDATION_FAILED", "parameters.effective_date", err.Error())
		}
		paramKnown := knownAt
		if raw["known_at"] != "" {
			paramKnown, err = parseKnownAt(raw["known_at"])
			if err != nil {
				return ParameterSet{}, refuse(ErrFixtureMalformed, "PACK_VALIDATION_FAILED", "parameters.known_at", err.Error())
			}
		}
		param := Parameter{ID: raw["id"], Jurisdiction: raw["jurisdiction"], EffectiveDate: effective, KnownAt: paramKnown, Amount: amount, IndexSource: IndexSource(raw["index_source"]), IndexFormula: raw["index_formula"], Citation: raw["citation"]}
		if raw["base_index"] != "" {
			param.BaseIndex, err = values.NewDecimal(raw["base_index"], 4, values.RoundingHalfUp)
			if err != nil {
				return ParameterSet{}, refuse(ErrFixtureMalformed, "PACK_VALIDATION_FAILED", "parameters.base_index", err.Error())
			}
		}
		if raw["next_adjustment_date"] != "" {
			param.NextAdjustmentDate, err = values.ParseLocalDate(raw["next_adjustment_date"])
			if err != nil {
				return ParameterSet{}, refuse(ErrFixtureMalformed, "PACK_VALIDATION_FAILED", "parameters.next_adjustment_date", err.Error())
			}
		}
		set.Parameters = append(set.Parameters, param)
	}
	for _, raw := range sections["index_points"] {
		date, err := values.ParseLocalDate(raw["date"])
		if err != nil {
			return ParameterSet{}, refuse(ErrFixtureMalformed, "PACK_VALIDATION_FAILED", "index_points.date", err.Error())
		}
		point := IndexPoint{ParameterID: raw["parameter_id"], Date: date, Source: IndexSource(raw["source"])}
		if raw["value"] != "" {
			point.Value, err = values.NewDecimal(raw["value"], 4, values.RoundingHalfUp)
		} else if raw["amount"] != "" {
			point.Amount, err = values.NewMoney(raw["amount"], defaultCurrency(raw["currency"]), 2, values.RoundingHalfUp)
		}
		if err != nil {
			return ParameterSet{}, refuse(ErrFixtureMalformed, "PACK_VALIDATION_FAILED", "index_points.value", err.Error())
		}
		set.IndexPoints = append(set.IndexPoints, point)
	}
	if err := set.Validate(); err != nil {
		return ParameterSet{}, err
	}
	return set, nil
}

func defaultCurrency(currency string) string {
	if currency == "" {
		return "USD"
	}
	return currency
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func parseKnownAt(s string) (values.KnownAt, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return values.KnownAt{}, err
	}
	return values.NewKnownAt(values.NewInstant(t))
}

func parseFlatYAML(input string) (map[string]string, map[string][]map[string]string, error) {
	top := map[string]string{}
	sections := map[string][]map[string]string{}
	section := ""
	var current map[string]string
	for lineNo, line := range strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n") {
		if i := strings.Index(line, "#"); i >= 0 && (i == 0 || !strings.Contains(line[:i], "\"")) {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)
		if indent == 0 {
			key, value, ok := splitYAMLKey(trimmed)
			if !ok {
				return nil, nil, fmt.Errorf("line %d: expected key", lineNo+1)
			}
			if value == "" {
				section = key
				current = nil
			} else {
				top[key] = unquote(value)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "-") {
			if section == "" {
				return nil, nil, fmt.Errorf("line %d: list has no section", lineNo+1)
			}
			current = map[string]string{}
			sections[section] = append(sections[section], current)
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			if trimmed != "" {
				key, value, ok := splitYAMLKey(trimmed)
				if !ok {
					return nil, nil, fmt.Errorf("line %d: expected list key", lineNo+1)
				}
				current[key] = unquote(value)
			}
			continue
		}
		if current == nil {
			return nil, nil, fmt.Errorf("line %d: mapping has no list item", lineNo+1)
		}
		key, value, ok := splitYAMLKey(trimmed)
		if !ok {
			return nil, nil, fmt.Errorf("line %d: expected mapping key", lineNo+1)
		}
		current[key] = unquote(value)
	}
	return top, sections, nil
}

func splitYAMLKey(s string) (string, string, bool) {
	i := strings.IndexByte(s, ':')
	if i < 1 {
		return "", "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
}

func unquote(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}
