// Package attribution resolves the governing employment-law subdivision for a
// worker from explicit physical-work facts. Residence is retained as evidence
// but never silently substituted for work location.
package attribution

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

//go:embed testdata/remote-workers.yaml
var fixtureFS embed.FS

const schemaVersion = 1

// Version is the schema version for attribution records.
func Version() int { return schemaVersion }

var (
	ErrValidation           = errors.New("attribution: PACK_VALIDATION_FAILED")
	ErrLegalContextUnknown  = errors.New("attribution: LEGAL_CONTEXT_UNKNOWN")
	ErrMultiStateUnresolved = errors.New("attribution: MULTI_STATE_UNRESOLVED")
	ErrFactsPort            = errors.New("attribution: worker facts port failed")
)

// Refusal is a typed, field-addressed fail-closed result.
type Refusal struct {
	Code   string
	Field  string
	Reason string
	Detail string
	Cause  error
}

func (e *Refusal) Error() string {
	return fmt.Sprintf("attribution: %s [%s] %s: %s", e.Code, e.Field, e.Reason, e.Detail)
}

func (e *Refusal) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrLegalContextUnknown}
	}
	return []error{ErrLegalContextUnknown, e.Cause}
}

func validation(field, detail string) error {
	return &Refusal{Code: "PACK_VALIDATION_FAILED", Field: field, Detail: detail, Cause: ErrValidation}
}

func unknown(field, reason, detail string) error {
	return &Refusal{Code: "LEGAL_CONTEXT_UNKNOWN", Field: field, Reason: reason, Detail: detail, Cause: ErrMultiStateUnresolved}
}

// ReviewFlag is the registry review state.
type ReviewFlag string

const (
	ReviewReviewed   ReviewFlag = "REVIEWED"
	ReviewUnreviewed ReviewFlag = "UNREVIEWED"
)

// StateRow keeps one attribution citation row for each US state.
type StateRow struct {
	State           string
	StatuteCitation string
	Review          ReviewFlag
}

// DateWindow is the explicit attribution window; it has no relationship to
// wall-clock execution time.
type DateWindow struct {
	Start values.LocalDate
	End   values.LocalDate
}

func (w DateWindow) validate() error {
	if err := w.Start.Validate(); err != nil {
		return validation("window.start", err.Error())
	}
	if err := w.End.Validate(); err != nil {
		return validation("window.end", err.Error())
	}
	if w.Start.Compare(w.End) >= 0 {
		return validation("window", "start must be before end")
	}
	return nil
}

// WorkLocation is a physical work subdivision and its scheduled-time share.
type WorkLocation struct {
	Jurisdiction legal.Jurisdiction
	Share        values.Decimal
}

// WorkerFacts are facts supplied by a caller or a port. Residence is included
// for evidence and tax workflows but is not an attribution selector in A3-A5.
type WorkerFacts struct {
	WorkerID              string
	Remote                bool
	Residence             legal.Jurisdiction
	AssertedEmployment    legal.Jurisdiction
	KnownAt               values.KnownAt
	PhysicalWorkLocations []WorkLocation
}

// TenantPolicy is the customer-controlled A4/A5 threshold. Nil is deliberate:
// there is no platform default.
type TenantPolicy struct {
	PrimaryWorkThreshold *values.Decimal
	KnownAt              values.KnownAt
}

// WorkerFactsPort is the only data boundary used by Checker. An adapter may
// read a service or a database outside this kernel package; the checker itself
// remains pure once facts are returned.
type WorkerFactsPort interface {
	WorkerFacts(context.Context, string, DateWindow) (WorkerFacts, error)
}

// AllocationInput is the pure input form for Resolve.
type AllocationInput struct {
	Window DateWindow
	Facts  WorkerFacts
	Policy TenantPolicy
}

// Exposure records a non-primary physical work subdivision under A4.
type Exposure struct {
	Jurisdiction legal.Jurisdiction
	Share        values.Decimal
}

// Result is the signed-input-ready attribution evidence. It does not itself
// claim that a rule pack exists for the selected jurisdiction.
type Result struct {
	SchemaVersion int
	Primary       legal.Jurisdiction
	Residence     legal.Jurisdiction
	Confidence    legal.Confidence
	Rule          string
	Exposures     []Exposure
	KnownAt       values.KnownAt
	Digest        string
}

// Configuration is the checked-in, versioned registry and remote fixture set.
type Configuration struct {
	SchemaVersion int
	Version       string
	KnownAt       values.KnownAt
	Registry      []StateRow
	Fixtures      []RemoteFixture
}

// RemoteFixture is a loader-friendly golden input. WorkLocations is encoded
// as state code to share, e.g. "CO:0.7500,AL:0.2500".
type RemoteFixture struct {
	ID            string
	Remote        bool
	Residence     string
	Employment    string
	WorkLocations string
	Threshold     string
	ExpectedRule  string
	ExpectedState string
}

// Check obtains facts through port and then invokes Resolve.
func Check(ctx context.Context, workerID string, window DateWindow, policy TenantPolicy, port WorkerFactsPort) (Result, error) {
	if port == nil {
		return Result{}, &Refusal{Code: "LEGAL_CONTEXT_UNKNOWN", Field: "facts_port", Reason: "MISSING_FACT", Detail: "worker facts port is required", Cause: ErrFactsPort}
	}
	facts, err := port.WorkerFacts(ctx, workerID, window)
	if err != nil {
		return Result{}, &Refusal{Code: "LEGAL_CONTEXT_UNKNOWN", Field: "worker_facts", Reason: "MISSING_FACT", Detail: err.Error(), Cause: ErrFactsPort}
	}
	return Resolve(AllocationInput{Window: window, Facts: facts, Policy: policy})
}

// Resolve applies A3, A4, and A5 in order. It never uses residence as a
// substitute for physical work location and never invents a threshold.
func Resolve(input AllocationInput) (Result, error) {
	if err := input.Window.validate(); err != nil {
		return Result{}, err
	}
	if err := validateFacts(input.Facts); err != nil {
		return Result{}, err
	}
	if err := input.Policy.KnownAt.Instant().Validate(); err != nil {
		return Result{}, validation("policy.known_at", err.Error())
	}
	if input.Policy.KnownAt.Instant().Compare(input.Facts.KnownAt.Instant()) < 0 {
		return Result{}, validation("policy.known_at", "must not precede the worker facts known_at")
	}
	if !input.Facts.Remote {
		if len(uniqueSubdivisions(input.Facts.PhysicalWorkLocations)) != 1 {
			return Result{}, unknown("physical_work_locations", "JURISDICTION_DISAGREEMENT", "non-remote work must have one physical subdivision")
		}
		primary := subdivision(input.Facts.PhysicalWorkLocations[0].Jurisdiction)
		if !primary.Equal(subdivision(input.Facts.AssertedEmployment)) {
			return Result{}, unknown("asserted_employment", "JURISDICTION_DISAGREEMENT", "A1 requires work and asserted employment subdivisions to agree")
		}
		return makeResult(primary, input.Facts, "A1", legal.ConfidenceVerified, nil), nil
	}

	byState := aggregate(input.Facts.PhysicalWorkLocations)
	if len(byState) == 1 {
		primary := onlyJurisdiction(byState)
		confidence := legal.ConfidenceAsserted
		if primary.Equal(subdivision(input.Facts.AssertedEmployment)) {
			confidence = legal.ConfidenceVerified
		}
		return makeResult(primary, input.Facts, "A3", confidence, nil), nil
	}
	if input.Policy.PrimaryWorkThreshold == nil {
		return Result{}, unknown("primary_work_threshold", "MULTI_STATE_UNRESOLVED", "A4/A5 require tenant primary_work_threshold; no platform default exists")
	}
	threshold := *input.Policy.PrimaryWorkThreshold
	if err := threshold.Validate(); err != nil || threshold.Cmp(mustDecimal("0.0001")) < 0 || threshold.Cmp(mustDecimal("1.0000")) > 0 {
		return Result{}, validation("primary_work_threshold", "must be greater than zero and no greater than 1")
	}
	var winners []string
	for state, share := range byState {
		if share.Cmp(threshold) >= 0 {
			winners = append(winners, state)
		}
	}
	if len(winners) != 1 {
		return Result{}, unknown("primary_work_threshold", "MULTI_STATE_UNRESOLVED", "A5 found no unique subdivision at or above the configured threshold")
	}
	sort.Strings(winners)
	primary := parseSubdivision(winners[0])
	exposures := make([]Exposure, 0, len(byState)-1)
	for state, share := range byState {
		if state != winners[0] && share.Cmp(mustDecimal("0.0000")) > 0 {
			exposures = append(exposures, Exposure{Jurisdiction: parseSubdivision(state), Share: share})
		}
	}
	sort.Slice(exposures, func(i, j int) bool { return exposures[i].Jurisdiction.String() < exposures[j].Jurisdiction.String() })
	return makeResult(primary, input.Facts, "A4", legal.ConfidenceAsserted, exposures), nil
}

func validateFacts(f WorkerFacts) error {
	if strings.TrimSpace(f.WorkerID) == "" {
		return validation("worker_id", "is required")
	}
	if err := f.Residence.Validate(); err != nil {
		return validation("residence", err.Error())
	}
	if err := f.AssertedEmployment.Validate(); err != nil {
		return validation("asserted_employment", err.Error())
	}
	if err := f.KnownAt.Instant().Validate(); err != nil {
		return validation("known_at", err.Error())
	}
	if len(f.PhysicalWorkLocations) == 0 {
		return validation("physical_work_locations", "at least one location is required")
	}
	for i, location := range f.PhysicalWorkLocations {
		if err := location.Jurisdiction.Validate(); err != nil || location.Jurisdiction.State == "" {
			return validation(fmt.Sprintf("physical_work_locations[%d].jurisdiction", i), "must be a state jurisdiction")
		}
		if err := location.Share.Validate(); err != nil {
			return validation(fmt.Sprintf("physical_work_locations[%d].share", i), err.Error())
		}
		if location.Share.Cmp(mustDecimal("0.0000")) < 0 || location.Share.Cmp(mustDecimal("1.0000")) > 0 {
			return validation(fmt.Sprintf("physical_work_locations[%d].share", i), "must be in [0,1]")
		}
	}
	return nil
}

func aggregate(locations []WorkLocation) map[string]values.Decimal {
	result := map[string]values.Decimal{}
	for _, location := range locations {
		key := subdivision(location.Jurisdiction).String()
		if prior, ok := result[key]; ok {
			sum, _ := prior.Add(location.Share)
			result[key] = sum
		} else {
			result[key] = location.Share
		}
	}
	return result
}

func uniqueSubdivisions(locations []WorkLocation) []legal.Jurisdiction {
	seen := map[string]legal.Jurisdiction{}
	for _, location := range locations {
		j := subdivision(location.Jurisdiction)
		seen[j.String()] = j
	}
	result := make([]legal.Jurisdiction, 0, len(seen))
	for _, j := range seen {
		result = append(result, j)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result
}

func subdivision(j legal.Jurisdiction) legal.Jurisdiction {
	return legal.Jurisdiction{Country: j.Country, State: j.State}
}

func onlyJurisdiction(shares map[string]values.Decimal) legal.Jurisdiction {
	for state := range shares {
		return parseSubdivision(state)
	}
	return legal.Jurisdiction{}
}

func parseSubdivision(s string) legal.Jurisdiction {
	parts := strings.SplitN(s, "-", 2)
	return legal.Jurisdiction{Country: parts[0], State: parts[1]}
}

func makeResult(primary legal.Jurisdiction, facts WorkerFacts, rule string, confidence legal.Confidence, exposures []Exposure) Result {
	result := Result{SchemaVersion: schemaVersion, Primary: primary, Residence: facts.Residence, Confidence: confidence, Rule: rule, Exposures: exposures, KnownAt: facts.KnownAt}
	result.Digest = result.digest()
	return result
}

func (r Result) digest() string {
	var b strings.Builder
	fmt.Fprintf(&b, "AT1|%d|%s|%s|%s|%s|%s|", r.SchemaVersion, r.Primary, r.Residence, r.Confidence, r.Rule, r.KnownAt)
	for _, exposure := range r.Exposures {
		fmt.Fprintf(&b, "%s|%s|", exposure.Jurisdiction, exposure.Share)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum[:])
}

// Explain returns deterministic audit evidence and explicitly names the rule.
func (r Result) Explain() string {
	return fmt.Sprintf("attribution primary=%s residence=%s rule=%s confidence=%s exposures=%d known_at=%s digest=%s", r.Primary, r.Residence, r.Rule, r.Confidence, len(r.Exposures), r.KnownAt, r.Digest)
}

// Validate validates the versioned registry fixture.
func (c Configuration) Validate() error {
	if c.SchemaVersion != schemaVersion {
		return validation("schema_version", "must be 1")
	}
	if strings.TrimSpace(c.Version) == "" {
		return validation("version", "is required")
	}
	if err := c.KnownAt.Instant().Validate(); err != nil {
		return validation("known_at", err.Error())
	}
	if len(c.Registry) != 50 {
		return validation("registry", "must contain one row per US state")
	}
	seen := map[string]bool{}
	for i, row := range c.Registry {
		if len(row.State) != 2 || row.State != strings.ToUpper(row.State) {
			return validation(fmt.Sprintf("registry[%d].state", i), "must be an uppercase state code")
		}
		if seen[row.State] {
			return validation(fmt.Sprintf("registry[%d].state", i), "is duplicated")
		}
		seen[row.State] = true
		if strings.TrimSpace(row.StatuteCitation) == "" {
			return validation(fmt.Sprintf("registry[%d].statute_citation", i), "is required")
		}
		if row.Review != ReviewReviewed && row.Review != ReviewUnreviewed {
			return validation(fmt.Sprintf("registry[%d].review", i), "must be REVIEWED or UNREVIEWED")
		}
	}
	return nil
}

// LoadFixture loads a checked-in remote-worker YAML fixture.
func LoadFixture(name string) (Configuration, error) {
	b, err := fixtureFS.ReadFile("testdata/" + name)
	if err != nil {
		return Configuration{}, err
	}
	return LoadYAML(strings.NewReader(string(b)))
}

// LoadYAML parses the dependency-free flat YAML subset used by the fixture.
func LoadYAML(r io.Reader) (Configuration, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return Configuration{}, err
	}
	top, sections, err := parseFlatYAML(string(b))
	if err != nil {
		return Configuration{}, err
	}
	known, err := parseKnownAt(top["known_at"])
	if err != nil {
		return Configuration{}, validation("known_at", err.Error())
	}
	config := Configuration{SchemaVersion: atoi(top["schema_version"]), Version: top["version"], KnownAt: known}
	for _, raw := range sections["registry"] {
		config.Registry = append(config.Registry, StateRow{State: raw["state"], StatuteCitation: raw["citation"], Review: ReviewFlag(raw["review"])})
	}
	for _, raw := range sections["fixtures"] {
		config.Fixtures = append(config.Fixtures, RemoteFixture{ID: raw["id"], Remote: raw["remote"] == "true", Residence: raw["residence"], Employment: raw["employment"], WorkLocations: raw["work_locations"], Threshold: raw["threshold"], ExpectedRule: raw["expected_rule"], ExpectedState: raw["expected_state"]})
	}
	if err := config.Validate(); err != nil {
		return Configuration{}, err
	}
	return config, nil
}

// DecodeFixtureFacts turns one loaded fixture into pure worker facts and a
// threshold policy, useful to tests and adapter code without adding a second
// interpretation of the fixture format.
func (c Configuration) DecodeFixtureFacts(id string) (AllocationInput, error) {
	var fixture *RemoteFixture
	for i := range c.Fixtures {
		if c.Fixtures[i].ID == id {
			fixture = &c.Fixtures[i]
			break
		}
	}
	if fixture == nil {
		return AllocationInput{}, validation("fixture_id", "is not registered")
	}
	residence, err := parseJurisdiction(fixture.Residence)
	if err != nil {
		return AllocationInput{}, validation("fixtures.residence", err.Error())
	}
	employment, err := parseJurisdiction(fixture.Employment)
	if err != nil {
		return AllocationInput{}, validation("fixtures.employment", err.Error())
	}
	known := c.KnownAt
	facts := WorkerFacts{WorkerID: fixture.ID, Remote: fixture.Remote, Residence: residence, AssertedEmployment: employment, KnownAt: known}
	for _, item := range strings.Split(fixture.WorkLocations, ",") {
		parts := strings.SplitN(strings.TrimSpace(item), ":", 2)
		if len(parts) != 2 {
			return AllocationInput{}, validation("fixtures.work_locations", "must use STATE:SHARE entries")
		}
		j, err := parseJurisdiction("US-" + parts[0])
		if err != nil {
			return AllocationInput{}, validation("fixtures.work_locations", err.Error())
		}
		share, err := values.NewDecimal(parts[1], 4, values.RoundingHalfUp)
		if err != nil {
			return AllocationInput{}, validation("fixtures.work_locations.share", err.Error())
		}
		facts.PhysicalWorkLocations = append(facts.PhysicalWorkLocations, WorkLocation{Jurisdiction: j, Share: share})
	}
	policy := TenantPolicy{KnownAt: known}
	if fixture.Threshold != "" {
		threshold, err := values.NewDecimal(fixture.Threshold, 4, values.RoundingHalfUp)
		if err != nil {
			return AllocationInput{}, validation("fixtures.threshold", err.Error())
		}
		policy.PrimaryWorkThreshold = &threshold
	}
	start, _ := values.ParseLocalDate("2026-01-01")
	end, _ := values.ParseLocalDate("2027-01-01")
	return AllocationInput{Window: DateWindow{Start: start, End: end}, Facts: facts, Policy: policy}, nil
}

func parseJurisdiction(s string) (legal.Jurisdiction, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return legal.Jurisdiction{}, fmt.Errorf("jurisdiction %q must be US-STATE", s)
	}
	j := legal.Jurisdiction{Country: parts[0], State: parts[1]}
	return j, j.Validate()
}

func mustDecimal(s string) values.Decimal {
	d, _ := values.NewDecimal(s, 4, values.RoundingHalfUp)
	return d
}

func atoi(s string) int {
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

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
