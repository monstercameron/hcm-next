// Package reciprocity owns the reviewed/unreviewed state-pair registry used
// when payroll tax or wage-law allocation needs a residence/work-state
// carve-out. It is deliberately a registry and port, not a tax calculator.
package reciprocity

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

//go:embed testdata/reciprocity.yaml
var fixtureFS embed.FS

const schemaVersion = 1

// ReviewFlag records whether the registry row has passed the research review
// gate. UNREVIEWED is intentionally not equivalent to no agreement.
type ReviewFlag string

const (
	ReviewUnreviewed ReviewFlag = "UNREVIEWED"
	ReviewReviewed   ReviewFlag = "REVIEWED"
)

// Status is the result of the reciprocity research question.
type Status string

const (
	StatusNoReciprocityConfirmed Status = "NO_RECIPROCITY_CONFIRMED"
	StatusReciprocityAgreement   Status = "RECIPROCITY_AGREEMENT"
	StatusUnresearched           Status = "UNRESEARCHED"
)

// Scope identifies which allocation question an agreement answers.
type Scope string

const (
	ScopeIncomeTax    Scope = "INCOME_TAX"
	ScopeUnemployment Scope = "UNEMPLOYMENT_INSURANCE"
	ScopeWageLaw      Scope = "WAGE_LAW"
)

// Citation is the provenance for one registry row. Authority is copied from
// the research wording or explicitly says RESEARCH_MISSING; it is never
// treated as a legal conclusion by this package.
type Citation struct {
	SourceFile string `yaml:"source_file" json:"source_file"`
	Section    string `yaml:"section" json:"section"`
	Authority  string `yaml:"authority" json:"authority"`
}

// StateRow is one state-level research row. There is one row for every state
// plus the District of Columbia because DC participates in real tax
// reciprocity arrangements.
type StateRow struct {
	Code     string     `yaml:"code" json:"code"`
	Name     string     `yaml:"name" json:"name"`
	Status   Status     `yaml:"status" json:"status"`
	Review   ReviewFlag `yaml:"review" json:"review"`
	Citation Citation   `yaml:"citation" json:"citation"`
}

// Agreement is an unordered state pair. Residence/work direction is retained
// in the resolution request because forms can differ by the work state.
type Agreement struct {
	Residence  string     `yaml:"residence" json:"residence"`
	Work       string     `yaml:"work" json:"work"`
	Scopes     []Scope    `yaml:"scopes" json:"scopes"`
	Conditions []string   `yaml:"conditions" json:"conditions"`
	Forms      []string   `yaml:"forms" json:"forms"`
	Review     ReviewFlag `yaml:"review" json:"review"`
	Citation   Citation   `yaml:"citation" json:"citation"`
}

// StatePair names a worker's residence and work states.
type StatePair struct {
	Residence string `yaml:"residence" json:"residence"`
	Work      string `yaml:"work" json:"work"`
}

// ResolveRequest is the bitemporal query accepted by Port.
type ResolveRequest struct {
	Residence     legal.Jurisdiction
	Work          legal.Jurisdiction
	EffectiveDate values.LocalDate
	KnownAt       values.KnownAt
}

// Resolution is the immutable answer to one residence/work query.
type Resolution struct {
	Residence      string
	Work           string
	Status         Status
	Scopes         []Scope
	Conditions     []string
	Forms          []string
	Citations      []Citation
	EffectiveDate  values.LocalDate
	KnownAt        values.KnownAt
	RegistryDigest string
}

// Port is the narrow resolver port consumed by payroll allocation callers.
type Port struct{ registry Registry }

// NewPort returns a defensive port over registry.
func NewPort(registry Registry) Port { return Port{registry: registry} }

// Resolve delegates to the versioned registry.
func (p Port) Resolve(request ResolveRequest) (Resolution, error) {
	return p.registry.Resolve(request)
}

// Registry is a versioned, effective-dated, digested reciprocity schema.
type Registry struct {
	SchemaVersion int
	Version       int
	Window        legal.EffectiveWindow
	KnownAt       values.KnownAt
	States        []StateRow
	Agreements    []Agreement
	ActivePairs   []StatePair
	Digest        string
}

// Errors are typed so callers can distinguish malformed registry data from a
// deliberate research gate.
var (
	ErrValidation       = errors.New("reciprocity: VALIDATION_FAILED")
	ErrField            = errors.New("reciprocity: invalid field")
	ErrUnresearchedPair = errors.New("reciprocity: UNRESEARCHED_PAIR")
	ErrEffectiveDate    = errors.New("reciprocity: effective date is outside registry window")
	ErrKnownAt          = errors.New("reciprocity: known-at is earlier than registry knowledge")
	ErrJurisdiction     = errors.New("reciprocity: residence/work jurisdiction is invalid")
	ErrUnknownState     = errors.New("reciprocity: state is not registered")
	ErrDuplicatePair    = errors.New("reciprocity: duplicate state pair")
)

// FieldError identifies the exact rejected field.
type FieldError struct {
	Field string
	Cause error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("reciprocity: field %s: %v", e.Field, e.Cause)
}
func (e *FieldError) Unwrap() error { return e.Cause }

func fieldError(field string, cause error) error {
	return fmt.Errorf("%w: %w: %w", ErrValidation, ErrField, &FieldError{Field: field, Cause: fmt.Errorf("%w", cause)})
}

func validStatus(s Status) bool {
	return s == StatusNoReciprocityConfirmed || s == StatusReciprocityAgreement || s == StatusUnresearched
}

func validReview(r ReviewFlag) bool { return r == ReviewReviewed || r == ReviewUnreviewed }

func validScope(s Scope) bool {
	return s == ScopeIncomeTax || s == ScopeUnemployment || s == ScopeWageLaw
}

func validCode(code string) bool {
	if len(code) != 2 {
		return false
	}
	return code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z'
}

func canonicalPair(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

// Validate refuses malformed or incomplete registry data and names the field
// responsible. It never defaults an unknown pair to work-location rules.
func (r Registry) Validate() error {
	if r.SchemaVersion != schemaVersion {
		return fieldError("schema_version", fmt.Errorf("want %d, got %d", schemaVersion, r.SchemaVersion))
	}
	if r.Version <= 0 {
		return fieldError("version", errors.New("must be positive"))
	}
	if err := r.Window.Validate(); err != nil {
		return fieldError("effective_window", err)
	}
	if err := r.KnownAt.Instant().Validate(); err != nil {
		return fieldError("known_at", err)
	}
	if len(r.States) == 0 {
		return fieldError("states", errors.New("must contain one row per state"))
	}
	stateCodes := make(map[string]struct{}, len(r.States))
	for i, row := range r.States {
		prefix := fmt.Sprintf("states[%d]", i)
		if !validCode(row.Code) {
			return fieldError(prefix+".code", errors.New("must be an uppercase two-letter code"))
		}
		if row.Name == "" {
			return fieldError(prefix+".name", errors.New("is required"))
		}
		if _, exists := stateCodes[row.Code]; exists {
			return fieldError(prefix+".code", errors.New("is duplicated"))
		}
		stateCodes[row.Code] = struct{}{}
		if !validStatus(row.Status) {
			return fieldError(prefix+".status", fmt.Errorf("unknown value %q", row.Status))
		}
		if !validReview(row.Review) {
			return fieldError(prefix+".review", fmt.Errorf("unknown value %q", row.Review))
		}
		if err := validateCitation(row.Citation, prefix+".citation"); err != nil {
			return err
		}
	}
	seen := map[string]struct{}{}
	for i, agreement := range r.Agreements {
		prefix := fmt.Sprintf("agreements[%d]", i)
		if _, ok := stateCodes[agreement.Residence]; !ok {
			return fieldError(prefix+".residence", ErrUnknownState)
		}
		if _, ok := stateCodes[agreement.Work]; !ok {
			return fieldError(prefix+".work", ErrUnknownState)
		}
		if agreement.Residence == agreement.Work {
			return fieldError(prefix+".work", errors.New("must differ from residence"))
		}
		key := canonicalPair(agreement.Residence, agreement.Work)
		if _, exists := seen[key]; exists {
			return fieldError(prefix, ErrDuplicatePair)
		}
		seen[key] = struct{}{}
		if len(agreement.Scopes) == 0 {
			return fieldError(prefix+".scopes", errors.New("must not be empty"))
		}
		for j, scope := range agreement.Scopes {
			if !validScope(scope) {
				return fieldError(fmt.Sprintf("%s.scopes[%d]", prefix, j), fmt.Errorf("unknown value %q", scope))
			}
		}
		if len(agreement.Conditions) == 0 {
			return fieldError(prefix+".conditions", errors.New("must name the eligibility conditions"))
		}
		if len(agreement.Forms) == 0 {
			return fieldError(prefix+".forms", errors.New("must name the required forms or attestations"))
		}
		if !validReview(agreement.Review) {
			return fieldError(prefix+".review", fmt.Errorf("unknown value %q", agreement.Review))
		}
		if err := validateCitation(agreement.Citation, prefix+".citation"); err != nil {
			return err
		}
	}
	for i, pair := range r.ActivePairs {
		prefix := fmt.Sprintf("active_pairs[%d]", i)
		if _, ok := stateCodes[pair.Residence]; !ok {
			return fieldError(prefix+".residence", ErrUnknownState)
		}
		if _, ok := stateCodes[pair.Work]; !ok {
			return fieldError(prefix+".work", ErrUnknownState)
		}
	}
	return nil
}

func validateCitation(c Citation, field string) error {
	if c.SourceFile == "" {
		return fieldError(field+".source_file", errors.New("is required"))
	}
	if c.Section == "" {
		return fieldError(field+".section", errors.New("is required"))
	}
	if c.Authority == "" {
		return fieldError(field+".authority", errors.New("is required"))
	}
	return nil
}

// ComputeDigest returns the SHA-256 digest of schema content, excluding the
// recorded digest itself. JSON is used only for deterministic field framing;
// the YAML representation is never part of the identity.
func (r Registry) ComputeDigest() string {
	payload := struct {
		SchemaVersion int         `json:"schema_version"`
		Version       int         `json:"version"`
		Start         string      `json:"effective_start"`
		End           string      `json:"effective_end"`
		HasEnd        bool        `json:"effective_has_end"`
		KnownAt       string      `json:"known_at"`
		States        []StateRow  `json:"states"`
		Agreements    []Agreement `json:"agreements"`
		ActivePairs   []StatePair `json:"active_pairs"`
	}{r.SchemaVersion, r.Version, r.Window.Start.String(), r.Window.End.String(), r.Window.HasEnd, r.KnownAt.String(), r.States, r.Agreements, r.ActivePairs}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Explain renders a deterministic operator-facing summary.
func (r Registry) Explain() string {
	return fmt.Sprintf("reciprocity schema=%d version=%d window=%s known_at=%s states=%d agreements=%d digest=%s", r.SchemaVersion, r.Version, r.Window, r.KnownAt, len(r.States), len(r.Agreements), r.Digest)
}

type yamlRegistry struct {
	SchemaVersion  int         `yaml:"schema_version"`
	Version        int         `yaml:"version"`
	EffectiveStart string      `yaml:"effective_start"`
	EffectiveEnd   string      `yaml:"effective_end"`
	KnownAt        string      `yaml:"known_at"`
	States         []StateRow  `yaml:"states"`
	Agreements     []Agreement `yaml:"agreements"`
	ActivePairs    []StatePair `yaml:"active_pairs"`
}

func parseScalar(text string) string {
	text = strings.TrimSpace(text)
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		if value, err := strconv.Unquote(text); err == nil {
			return value
		}
	}
	return strings.Trim(text, "'")
}

func splitInline(text string) []string {
	var out []string
	start := 0
	quoted := false
	for i, r := range text {
		switch r {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				out = append(out, strings.TrimSpace(text[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(text[start:]))
	return out
}

func inlineMap(text string) (map[string]string, error) {
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '{' || text[len(text)-1] != '}' {
		return nil, errors.New("expected inline map")
	}
	out := map[string]string{}
	for _, part := range splitInline(text[1 : len(text)-1]) {
		key, value, ok := strings.Cut(part, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid inline map member %q", part)
		}
		out[strings.TrimSpace(key)] = parseScalar(value)
	}
	return out, nil
}

func inlineList(text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '[' || text[len(text)-1] != ']' {
		return nil, errors.New("expected inline list")
	}
	if strings.TrimSpace(text[1:len(text)-1]) == "" {
		return nil, nil
	}
	parts := splitInline(text[1 : len(text)-1])
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = parseScalar(part)
	}
	return out, nil
}

func parseKeyValue(text string) (string, string, error) {
	key, value, ok := strings.Cut(strings.TrimSpace(text), ":")
	if !ok || strings.TrimSpace(key) == "" {
		return "", "", errors.New("expected key/value")
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), nil
}

// parseFixtureYAML reads the deliberately small, checked-in YAML schema
// without importing a semantic YAML dependency. The fixture uses only YAML
// scalars, block sequences, and inline maps/lists; malformed or unknown
// members are rejected before the typed validator runs.
func parseFixtureYAML(data []byte) (yamlRegistry, error) {
	var out yamlRegistry
	section := ""
	var state *StateRow
	var agreement *Agreement
	flush := func() {
		if state != nil {
			out.States = append(out.States, *state)
			state = nil
		}
		if agreement != nil {
			out.Agreements = append(out.Agreements, *agreement)
			agreement = nil
		}
	}
	for lineNumber, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			flush()
			key, value, err := parseKeyValue(trimmed)
			if err != nil {
				return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			if value == "" {
				section = key
				continue
			}
			switch key {
			case "schema_version":
				_, err = fmt.Sscanf(value, "%d", &out.SchemaVersion)
			case "version":
				_, err = fmt.Sscanf(value, "%d", &out.Version)
			case "effective_start":
				out.EffectiveStart = parseScalar(value)
			case "effective_end":
				out.EffectiveEnd = parseScalar(value)
			case "known_at":
				out.KnownAt = parseScalar(value)
			default:
				err = fmt.Errorf("unknown top-level field %q", key)
			}
			if err != nil {
				return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			switch section {
			case "states":
				state = &StateRow{}
				if err := applyStateField(state, item); err != nil {
					return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
				}
			case "agreements":
				agreement = &Agreement{}
				if err := applyAgreementField(agreement, item); err != nil {
					return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
				}
			case "active_pairs":
				members, err := inlineMap(item)
				if err != nil {
					return yamlRegistry{}, fmt.Errorf("line %d: active_pairs: %w", lineNumber+1, err)
				}
				out.ActivePairs = append(out.ActivePairs, StatePair{Residence: members["residence"], Work: members["work"]})
			default:
				return yamlRegistry{}, fmt.Errorf("line %d: unknown sequence %q", lineNumber+1, section)
			}
			continue
		}
		key, value, err := parseKeyValue(trimmed)
		if err != nil {
			return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
		switch section {
		case "states":
			if state == nil {
				return yamlRegistry{}, fmt.Errorf("line %d: state field has no item", lineNumber+1)
			}
			err = applyStateField(state, key+": "+value)
		case "agreements":
			if agreement == nil {
				return yamlRegistry{}, fmt.Errorf("line %d: agreement field has no item", lineNumber+1)
			}
			err = applyAgreementField(agreement, key+": "+value)
		default:
			err = fmt.Errorf("unknown section %q", section)
		}
		if err != nil {
			return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
	}
	flush()
	return out, nil
}

func applyCitation(c *Citation, text string) error {
	members, err := inlineMap(text)
	if err != nil {
		return err
	}
	c.SourceFile, c.Section, c.Authority = members["source_file"], members["section"], members["authority"]
	return nil
}

func applyStateField(row *StateRow, text string) error {
	key, value, err := parseKeyValue(text)
	if err != nil {
		return err
	}
	switch key {
	case "code":
		row.Code = parseScalar(value)
	case "name":
		row.Name = parseScalar(value)
	case "status":
		row.Status = Status(parseScalar(value))
	case "review":
		row.Review = ReviewFlag(parseScalar(value))
	case "citation":
		return applyCitation(&row.Citation, value)
	default:
		return fmt.Errorf("unknown state field %q", key)
	}
	return nil
}

func applyAgreementField(row *Agreement, text string) error {
	key, value, err := parseKeyValue(text)
	if err != nil {
		return err
	}
	switch key {
	case "residence":
		row.Residence = parseScalar(value)
	case "work":
		row.Work = parseScalar(value)
	case "scopes":
		items, err := inlineList(value)
		if err != nil {
			return err
		}
		for _, item := range items {
			row.Scopes = append(row.Scopes, Scope(item))
		}
	case "conditions":
		row.Conditions, err = inlineList(value)
	case "forms":
		row.Forms, err = inlineList(value)
	case "review":
		row.Review = ReviewFlag(parseScalar(value))
	case "citation":
		return applyCitation(&row.Citation, value)
	default:
		return fmt.Errorf("unknown agreement field %q", key)
	}
	return err
}

func parseKnownAt(s string) (values.KnownAt, error) {
	instant, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return values.KnownAt{}, fmt.Errorf("%w: known_at: %v", ErrValidation, err)
	}
	return values.NewKnownAt(values.NewInstant(instant))
}

// Load parses and validates a YAML registry, then computes its content digest.
func Load(data []byte) (Registry, error) {
	wire, err := parseFixtureYAML(data)
	if err != nil {
		return Registry{}, fmt.Errorf("%w: yaml: %v", ErrValidation, err)
	}
	start, err := values.ParseLocalDate(wire.EffectiveStart)
	if err != nil {
		return Registry{}, fieldError("effective_start", err)
	}
	var window legal.EffectiveWindow
	if wire.EffectiveEnd == "" {
		window, err = legal.NewOpenEffectiveWindow(start)
	} else {
		end, endErr := values.ParseLocalDate(wire.EffectiveEnd)
		if endErr != nil {
			return Registry{}, fieldError("effective_end", endErr)
		}
		window, err = legal.NewClosedEffectiveWindow(start, end)
	}
	if err != nil {
		return Registry{}, fieldError("effective_window", err)
	}
	knownAt, err := parseKnownAt(wire.KnownAt)
	if err != nil {
		return Registry{}, fieldError("known_at", err)
	}
	r := Registry{SchemaVersion: wire.SchemaVersion, Version: wire.Version, Window: window, KnownAt: knownAt, States: wire.States, Agreements: wire.Agreements, ActivePairs: wire.ActivePairs}
	if err := r.Validate(); err != nil {
		return Registry{}, err
	}
	r.Digest = r.ComputeDigest()
	return r, nil
}

// LoadFixture loads the checked-in fixture used by the conformance suite.
func LoadFixture() (Registry, error) {
	data, err := fixtureFS.ReadFile("testdata/reciprocity.yaml")
	if err != nil {
		return Registry{}, fmt.Errorf("reciprocity: fixture: %w", err)
	}
	return Load(data)
}

// Resolve answers the residence/work carve-out question using the business
// effective date and the knowledge time supplied by the caller.
func (r Registry) Resolve(request ResolveRequest) (Resolution, error) {
	if err := r.Validate(); err != nil {
		return Resolution{}, err
	}
	if request.EffectiveDate.Validate() != nil || !r.Window.Contains(request.EffectiveDate) {
		return Resolution{}, ErrEffectiveDate
	}
	if request.KnownAt.Instant().Validate() != nil || request.KnownAt.Instant().Before(r.KnownAt.Instant()) {
		return Resolution{}, ErrKnownAt
	}
	if err := request.Residence.Validate(); err != nil {
		return Resolution{}, fmt.Errorf("%w: residence: %v", ErrJurisdiction, err)
	}
	if err := request.Work.Validate(); err != nil {
		return Resolution{}, fmt.Errorf("%w: work: %v", ErrJurisdiction, err)
	}
	if request.Residence.Country != "US" || request.Work.Country != "US" || request.Residence.State == "" || request.Work.State == "" {
		return Resolution{}, ErrJurisdiction
	}
	stateCodes := map[string]StateRow{}
	for _, row := range r.States {
		stateCodes[row.Code] = row
	}
	residence, residenceOK := stateCodes[request.Residence.State]
	work, workOK := stateCodes[request.Work.State]
	if !residenceOK {
		return Resolution{}, fmt.Errorf("%w: %s", ErrUnknownState, request.Residence.State)
	}
	if !workOK {
		return Resolution{}, fmt.Errorf("%w: %s", ErrUnknownState, request.Work.State)
	}
	resolution := Resolution{Residence: request.Residence.State, Work: request.Work.State, EffectiveDate: request.EffectiveDate, KnownAt: request.KnownAt, RegistryDigest: r.Digest}
	if request.Residence.State == request.Work.State {
		resolution.Status = StatusNoReciprocityConfirmed
		resolution.Citations = []Citation{residence.Citation}
		return resolution, nil
	}
	for _, agreement := range r.Agreements {
		if canonicalPair(agreement.Residence, agreement.Work) == canonicalPair(request.Residence.State, request.Work.State) {
			resolution.Status = StatusReciprocityAgreement
			resolution.Scopes = append([]Scope(nil), agreement.Scopes...)
			resolution.Conditions = append([]string(nil), agreement.Conditions...)
			resolution.Forms = append([]string(nil), agreement.Forms...)
			resolution.Citations = []Citation{agreement.Citation}
			return resolution, nil
		}
	}
	if residence.Status == StatusNoReciprocityConfirmed && work.Status == StatusNoReciprocityConfirmed {
		resolution.Status = StatusNoReciprocityConfirmed
		resolution.Citations = []Citation{residence.Citation, work.Citation}
		return resolution, nil
	}
	return Resolution{}, fmt.Errorf("%w: %s/%s", ErrUnresearchedPair, request.Residence.State, request.Work.State)
}

// ValidateActivePairs enforces the tenant-facing gate in the fixture: every
// active pair must resolve without an unresearched fallback.
func (r Registry) ValidateActivePairs(requestKnownAt values.KnownAt, effectiveDate values.LocalDate) error {
	for _, pair := range r.ActivePairs {
		_, err := r.Resolve(ResolveRequest{
			Residence:     legal.Jurisdiction{Country: "US", State: pair.Residence},
			Work:          legal.Jurisdiction{Country: "US", State: pair.Work},
			EffectiveDate: effectiveDate,
			KnownAt:       requestKnownAt,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// StatesSorted returns a deterministic defensive copy for reports.
func (r Registry) StatesSorted() []StateRow {
	out := append([]StateRow(nil), r.States...)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}
