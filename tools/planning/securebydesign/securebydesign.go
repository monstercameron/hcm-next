// Package securebydesign records the product-security commitments that make
// the CISA Secure by Design pledge reviewable at release time.
//
// The package is deliberately kernel-pure: it reads fixtures and source
// files, computes deterministic values, and has no database or clock
// dependency. A Record is a versioned, content-addressed revision. Its
// Explain method is intentionally bounded and contains no contact details,
// finding identifiers, or other protected values.
package securebydesign

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	Schema        = "hcmnext.planning.securebydesign"
	SchemaVersion = 1
	Pledge        = "CISA Secure by Design Pledge"
)

// Version is the schema version of this package's record.
func Version() int { return SchemaVersion }

// Explain is a package-level description suitable for registry discovery.
func Explain() string { return "versioned CISA secure-by-design product-goal records" }

// Goal is one of the seven CISA pledge goals and the tests that prove the
// product's corresponding default configuration or evidence capability.
type Goal struct {
	Number       int      `json:"number" yaml:"number"`
	Name         string   `json:"name" yaml:"name"`
	DefaultTests []string `json:"default_tests" yaml:"default_tests"`
}

// Timeline is the public handling clock in the vulnerability-disclosure
// policy. Values are calendar days and are intentionally numeric so they can
// be evaluated without a wall clock.
type Timeline struct {
	AcknowledgementDays       int `json:"acknowledgement_days" yaml:"acknowledgement_days"`
	InitialTriageDays         int `json:"initial_triage_days" yaml:"initial_triage_days"`
	StatusUpdateDays          int `json:"status_update_days" yaml:"status_update_days"`
	CoordinatedDisclosureDays int `json:"coordinated_disclosure_days" yaml:"coordinated_disclosure_days"`
}

// DisclosurePolicy is the public vulnerability-disclosure policy record.
// Contact is retained in the record but never rendered by Explain.
type DisclosurePolicy struct {
	Contact    string   `json:"contact" yaml:"contact"`
	Scope      []string `json:"scope" yaml:"scope"`
	SafeHarbor string   `json:"safe_harbour" yaml:"safe_harbour"`
	Timelines  Timeline `json:"timelines" yaml:"timelines"`
}

// Finding is one fixture row used to compute a release trend. The fixture
// intentionally does not require a vulnerability identifier; the aggregate
// evidence is sufficient for this governance record and avoids carrying
// unnecessary identifiers into audit explanations.
type Finding struct {
	Release     string `json:"release" yaml:"release"`
	Status      string `json:"status" yaml:"status"`
	DaysToClose int    `json:"days_to_close" yaml:"days_to_close"`
}

// Trend is the deterministic per-release vulnerability trend metric.
type Trend struct {
	Release        string  `json:"release" yaml:"release"`
	FindingsOpened int     `json:"findings_opened" yaml:"findings_opened"`
	FindingsClosed int     `json:"findings_closed" yaml:"findings_closed"`
	MedianDays     float64 `json:"median_days" yaml:"median_days"`
}

// Exception is an explicit, reviewed, time-bounded departure from a goal.
// Dates use YYYY-MM-DD so the record remains stable across time zones.
type Exception struct {
	GoalNumber int    `json:"goal_number" yaml:"goal_number"`
	Reason     string `json:"reason" yaml:"reason"`
	Owner      string `json:"owner" yaml:"owner"`
	ApprovedBy string `json:"approved_by" yaml:"approved_by"`
	StartsOn   string `json:"starts_on" yaml:"starts_on"`
	ExpiresOn  string `json:"expires_on" yaml:"expires_on"`
}

// Fixture is the YAML-backed disclosure and finding input used to assemble a
// Record.
type Fixture struct {
	Version                 int              `yaml:"version" json:"version"`
	VulnerabilityDisclosure DisclosurePolicy `yaml:"vulnerability_disclosure_policy" json:"vulnerability_disclosure_policy"`
	Findings                []Finding        `yaml:"findings" json:"findings"`
}

// Record is one immutable-by-construction secure-by-design revision. Slices
// are cloned by NewRecord and by canonicalization; callers can therefore
// safely retain and mutate their input values after construction.
type Record struct {
	Schema           string           `json:"schema" yaml:"schema"`
	SchemaVersion    int              `json:"schema_version" yaml:"schema_version"`
	Revision         int              `json:"revision" yaml:"revision"`
	Pledge           string           `json:"pledge" yaml:"pledge"`
	Goals            []Goal           `json:"goals" yaml:"goals"`
	DisclosurePolicy DisclosurePolicy `json:"vulnerability_disclosure_policy" yaml:"vulnerability_disclosure_policy"`
	Trends           []Trend          `json:"trends" yaml:"trends"`
	Exceptions       []Exception      `json:"exceptions" yaml:"exceptions"`
	Digest           string           `json:"digest" yaml:"digest"`
}

// Refusal is a typed fail-closed diagnostic. Field always names the exact
// record field that was refused.
type Refusal struct {
	Field  string
	Reason string
}

func (r Refusal) Error() string {
	return fmt.Sprintf("securebydesign: refusal field %s: %s", r.Field, r.Reason)
}

// InvalidRecord groups all structural refusals so a caller receives a useful
// complete diagnostic in one pass.
type InvalidRecord struct{ Refusals []Refusal }

func (e InvalidRecord) Error() string {
	if len(e.Refusals) == 0 {
		return "securebydesign: invalid record"
	}
	parts := make([]string, len(e.Refusals))
	for i, refusal := range e.Refusals {
		parts[i] = refusal.Error()
	}
	return strings.Join(parts, "; ")
}

// GoalNames is the pinned CISA goal vocabulary used by DefaultGoals.
var GoalNames = [...]string{
	"Multi-factor authentication",
	"Default passwords",
	"Reducing entire classes of vulnerabilities",
	"Security patches",
	"Vulnerability disclosure policy",
	"CVE records",
	"Evidence of intrusions",
}

// DefaultGoals returns the product's seven goal bindings. Each name is a
// repository test function, so conformance can verify the binding is live.
func DefaultGoals() []Goal {
	return []Goal{
		{Number: 1, Name: GoalNames[0], DefaultTests: []string{"TestSecureByDesignDefaultAuthZ", "TestSecureByDesignMFAAndStepUpAvailable"}},
		{Number: 2, Name: GoalNames[1], DefaultTests: []string{"TestSecureByDesignNoDefaultPasswords"}},
		{Number: 3, Name: GoalNames[2], DefaultTests: []string{"TestTodo_SUPPLY_002"}},
		{Number: 4, Name: GoalNames[3], DefaultTests: []string{"TestSecureByDesignSecurityPatchEvidence"}},
		{Number: 5, Name: GoalNames[4], DefaultTests: []string{"TestSecureByDesignDisclosurePolicy"}},
		{Number: 6, Name: GoalNames[5], DefaultTests: []string{"TestTodo_SUPPLY_002"}},
		{Number: 7, Name: GoalNames[6], DefaultTests: []string{"TestTodo_LEDGER_012", "TestSecureByDesignAuditEvidenceOn"}},
	}
}

// LoadFixture loads the YAML fixture used by NewRecord.
func LoadFixture(path string) (Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, fmt.Errorf("securebydesign: read fixture %s: %w", path, err)
	}
	var fixture Fixture
	if err := yaml.Unmarshal(data, &fixture); err != nil {
		return Fixture{}, fmt.Errorf("securebydesign: parse fixture %s: %w", path, err)
	}
	if fixture.Version != SchemaVersion {
		return Fixture{}, Refusal{Field: "fixture.version", Reason: fmt.Sprintf("want %d, got %d", SchemaVersion, fixture.Version)}
	}
	return fixture, nil
}

// LoadDisclosurePolicyYAML loads just the public policy section from a YAML
// file. It is useful to callers that publish the policy independently.
func LoadDisclosurePolicyYAML(path string) (DisclosurePolicy, error) {
	fixture, err := LoadFixture(path)
	if err != nil {
		return DisclosurePolicy{}, err
	}
	return fixture.VulnerabilityDisclosure, nil
}

// ComputeTrends derives one metric per release from finding fixture rows.
func ComputeTrends(findings []Finding) ([]Trend, error) {
	byRelease := make(map[string][]Finding)
	for i, finding := range findings {
		if strings.TrimSpace(finding.Release) == "" {
			return nil, Refusal{Field: fmt.Sprintf("findings[%d].release", i), Reason: "release is required"}
		}
		status := strings.ToUpper(strings.TrimSpace(finding.Status))
		if status != "OPEN" && status != "CLOSED" {
			return nil, Refusal{Field: fmt.Sprintf("findings[%d].status", i), Reason: "status must be OPEN or CLOSED"}
		}
		if finding.DaysToClose < 0 {
			return nil, Refusal{Field: fmt.Sprintf("findings[%d].days_to_close", i), Reason: "days_to_close cannot be negative"}
		}
		finding.Status = status
		byRelease[finding.Release] = append(byRelease[finding.Release], finding)
	}

	releases := make([]string, 0, len(byRelease))
	for release := range byRelease {
		releases = append(releases, release)
	}
	sort.Strings(releases)
	trends := make([]Trend, 0, len(releases))
	for _, release := range releases {
		rows := byRelease[release]
		closedDays := make([]int, 0, len(rows))
		for _, row := range rows {
			if row.Status == "CLOSED" {
				closedDays = append(closedDays, row.DaysToClose)
			}
		}
		sort.Ints(closedDays)
		var median float64
		if len(closedDays) > 0 {
			middle := len(closedDays) / 2
			if len(closedDays)%2 == 0 {
				median = float64(closedDays[middle-1]+closedDays[middle]) / 2
			} else {
				median = float64(closedDays[middle])
			}
		}
		closed := len(closedDays)
		trends = append(trends, Trend{Release: release, FindingsOpened: len(rows), FindingsClosed: closed, MedianDays: median})
	}
	return trends, nil
}

// NewRecord validates and freezes a secure-by-design revision.
func NewRecord(revision int, goals []Goal, policy DisclosurePolicy, trends []Trend, exceptions []Exception) (Record, error) {
	record := Record{
		Schema: Schema, SchemaVersion: SchemaVersion, Revision: revision, Pledge: Pledge,
		Goals: cloneGoals(goals), DisclosurePolicy: clonePolicy(policy), Trends: append([]Trend(nil), trends...), Exceptions: append([]Exception(nil), exceptions...),
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	record.Digest = record.ComputeDigest()
	return record, nil
}

// NewRecordFromFixture computes trends from the YAML-backed finding rows and
// then builds the versioned record.
func NewRecordFromFixture(revision int, fixture Fixture, goals []Goal, exceptions []Exception) (Record, error) {
	trends, err := ComputeTrends(fixture.Findings)
	if err != nil {
		return Record{}, err
	}
	return NewRecord(revision, goals, fixture.VulnerabilityDisclosure, trends, exceptions)
}

// Validate checks record structure without consulting the current date.
func (r Record) Validate() error {
	var refusals []Refusal
	if r.Schema != Schema {
		refusals = append(refusals, Refusal{Field: "schema", Reason: "unsupported schema"})
	}
	if r.SchemaVersion != SchemaVersion {
		refusals = append(refusals, Refusal{Field: "schema_version", Reason: fmt.Sprintf("want %d", SchemaVersion)})
	}
	if r.Revision < 1 {
		refusals = append(refusals, Refusal{Field: "revision", Reason: "must be positive"})
	}
	if r.Pledge != Pledge {
		refusals = append(refusals, Refusal{Field: "pledge", Reason: "must identify the CISA pledge"})
	}
	if len(r.Goals) != len(GoalNames) {
		refusals = append(refusals, Refusal{Field: "goals", Reason: "exactly seven goals are required"})
	}
	seenGoals := make(map[int]bool, len(r.Goals))
	for i, goal := range r.Goals {
		field := fmt.Sprintf("goals[%d]", i)
		if goal.Number < 1 || goal.Number > len(GoalNames) || seenGoals[goal.Number] {
			refusals = append(refusals, Refusal{Field: field + ".number", Reason: "goal number must be unique and in 1..7"})
			continue
		}
		seenGoals[goal.Number] = true
		if goal.Name != GoalNames[goal.Number-1] {
			refusals = append(refusals, Refusal{Field: field + ".name", Reason: "does not match the pinned CISA goal name"})
		}
		if len(goal.DefaultTests) == 0 {
			refusals = append(refusals, Refusal{Field: field + ".default_tests", Reason: "at least one repository test is required"})
		}
		seenTests := map[string]bool{}
		for j, testName := range goal.DefaultTests {
			if strings.TrimSpace(testName) == "" || seenTests[testName] {
				refusals = append(refusals, Refusal{Field: fmt.Sprintf("%s.default_tests[%d]", field, j), Reason: "test name must be non-empty and unique"})
			}
			seenTests[testName] = true
		}
	}
	refusals = append(refusals, validatePolicy(r.DisclosurePolicy)...)
	seenReleases := map[string]bool{}
	for i, trend := range r.Trends {
		field := fmt.Sprintf("trends[%d]", i)
		if strings.TrimSpace(trend.Release) == "" || seenReleases[trend.Release] {
			refusals = append(refusals, Refusal{Field: field + ".release", Reason: "release must be non-empty and unique"})
		}
		seenReleases[trend.Release] = true
		if trend.FindingsOpened < 0 {
			refusals = append(refusals, Refusal{Field: field + ".findings_opened", Reason: "cannot be negative"})
		}
		if trend.FindingsClosed < 0 || trend.FindingsClosed > trend.FindingsOpened {
			refusals = append(refusals, Refusal{Field: field + ".findings_closed", Reason: "must be between zero and findings_opened"})
		}
		if trend.MedianDays < 0 {
			refusals = append(refusals, Refusal{Field: field + ".median_days", Reason: "cannot be negative"})
		}
	}
	for i, exception := range r.Exceptions {
		field := fmt.Sprintf("exceptions[%d]", i)
		if exception.GoalNumber < 1 || exception.GoalNumber > len(GoalNames) {
			refusals = append(refusals, Refusal{Field: field + ".goal_number", Reason: "must name a goal in 1..7"})
		}
		if strings.TrimSpace(exception.Reason) == "" {
			refusals = append(refusals, Refusal{Field: field + ".reason", Reason: "reason is required"})
		}
		if strings.TrimSpace(exception.Owner) == "" {
			refusals = append(refusals, Refusal{Field: field + ".owner", Reason: "owner is required"})
		}
		if strings.TrimSpace(exception.ApprovedBy) == "" {
			refusals = append(refusals, Refusal{Field: field + ".approved_by", Reason: "independent approval is required"})
		}
		start, startOK := parseDate(exception.StartsOn)
		end, endOK := parseDate(exception.ExpiresOn)
		if !startOK {
			refusals = append(refusals, Refusal{Field: field + ".starts_on", Reason: "must be YYYY-MM-DD"})
		}
		if !endOK {
			refusals = append(refusals, Refusal{Field: field + ".expires_on", Reason: "must be YYYY-MM-DD"})
		}
		if startOK && endOK && !end.After(start) {
			refusals = append(refusals, Refusal{Field: field + ".expires_on", Reason: "must be after starts_on"})
		}
	}
	if len(refusals) != 0 {
		return InvalidRecord{Refusals: refusals}
	}
	return nil
}

// ValidateAt additionally refuses exceptions that are no longer active at
// at. An empty at skips the date-bound check and is equivalent to Validate.
func (r Record) ValidateAt(at time.Time) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if at.IsZero() {
		return nil
	}
	day := at.UTC().Truncate(24 * time.Hour)
	for i, exception := range r.Exceptions {
		expires, _ := parseDate(exception.ExpiresOn)
		if !day.Before(expires) {
			return InvalidRecord{Refusals: []Refusal{{Field: fmt.Sprintf("exceptions[%d].expires_on", i), Reason: "exception is expired at evaluation time"}}}
		}
	}
	return nil
}

// ComputeDigest returns the SHA-256 digest over the canonical record content,
// excluding Digest itself.
func (r Record) ComputeDigest() string {
	canonical, _ := json.Marshal(r.canonicalValue())
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest refuses a record whose content no longer matches its
// recorded revision digest.
func (r Record) VerifyDigest() error {
	want := r.ComputeDigest()
	if r.Digest == "" || r.Digest != want {
		return Refusal{Field: "digest", Reason: "does not match the immutable revision content"}
	}
	return nil
}

// Canonical returns the stable JSON bytes used for the revision digest.
func (r Record) Canonical() []byte {
	data, _ := json.Marshal(r.canonicalValue())
	return data
}

// Explain returns a short audit-safe summary. It contains only aggregate
// counts and the record digest; it never includes policy contact, owners,
// approval references, release labels, or finding identifiers.
func (r Record) Explain() string {
	return fmt.Sprintf("secure-by-design revision %d (%d goals, %d releases, %d exceptions; %s)", r.Revision, len(r.Goals), len(r.Trends), len(r.Exceptions), r.Digest)
}

// ScanTestNames parses repository test files and returns declared Go test
// function names. AST parsing avoids false positives from comments and
// strings, and is independent of the packages' build tags.
func ScanTestNames(root string) (map[string]bool, error) {
	names := make(map[string]bool)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return fmt.Errorf("securebydesign: parse test file %s: %w", path, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || function.Name == nil {
				continue
			}
			name := function.Name.Name
			if strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Benchmark") {
				names[name] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

// Conformance returns typed refusals for every named goal test absent from
// the parsed repository test set.
func (r Record) Conformance(testNames map[string]bool) []Refusal {
	var refusals []Refusal
	for i, goal := range r.Goals {
		for j, testName := range goal.DefaultTests {
			if !testNames[testName] {
				refusals = append(refusals, Refusal{Field: fmt.Sprintf("goals[%d].default_tests[%d]", i, j), Reason: "named test function is absent from repository: " + testName})
			}
		}
	}
	return refusals
}

// ValidateRepository combines structural, digest, and test-name conformance
// checks for a record against a repository root.
func (r Record) ValidateRepository(root string) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := r.VerifyDigest(); err != nil {
		return err
	}
	names, err := ScanTestNames(root)
	if err != nil {
		return err
	}
	if refusals := r.Conformance(names); len(refusals) != 0 {
		return InvalidRecord{Refusals: refusals}
	}
	return nil
}

func validatePolicy(policy DisclosurePolicy) []Refusal {
	var refusals []Refusal
	if strings.TrimSpace(policy.Contact) == "" {
		refusals = append(refusals, Refusal{Field: "vulnerability_disclosure_policy.contact", Reason: "contact is required"})
	}
	if len(policy.Scope) == 0 {
		refusals = append(refusals, Refusal{Field: "vulnerability_disclosure_policy.scope", Reason: "scope is required"})
	}
	for i, scope := range policy.Scope {
		if strings.TrimSpace(scope) == "" {
			refusals = append(refusals, Refusal{Field: fmt.Sprintf("vulnerability_disclosure_policy.scope[%d]", i), Reason: "scope entry is empty"})
		}
	}
	if strings.TrimSpace(policy.SafeHarbor) == "" {
		refusals = append(refusals, Refusal{Field: "vulnerability_disclosure_policy.safe_harbour", Reason: "safe-harbour terms are required"})
	}
	if policy.Timelines.AcknowledgementDays <= 0 {
		refusals = append(refusals, Refusal{Field: "vulnerability_disclosure_policy.timelines.acknowledgement_days", Reason: "must be positive"})
	}
	if policy.Timelines.InitialTriageDays <= 0 {
		refusals = append(refusals, Refusal{Field: "vulnerability_disclosure_policy.timelines.initial_triage_days", Reason: "must be positive"})
	}
	if policy.Timelines.StatusUpdateDays <= 0 {
		refusals = append(refusals, Refusal{Field: "vulnerability_disclosure_policy.timelines.status_update_days", Reason: "must be positive"})
	}
	if policy.Timelines.CoordinatedDisclosureDays <= 0 {
		refusals = append(refusals, Refusal{Field: "vulnerability_disclosure_policy.timelines.coordinated_disclosure_days", Reason: "must be positive"})
	}
	return refusals
}

func parseDate(value string) (time.Time, bool) {
	parsed, err := time.Parse("2006-01-02", value)
	return parsed, err == nil
}

func cloneGoals(goals []Goal) []Goal {
	out := make([]Goal, len(goals))
	for i, goal := range goals {
		out[i] = goal
		out[i].DefaultTests = append([]string(nil), goal.DefaultTests...)
	}
	return out
}

func clonePolicy(policy DisclosurePolicy) DisclosurePolicy {
	policy.Scope = append([]string(nil), policy.Scope...)
	return policy
}

func (r Record) canonicalValue() any {
	goals := cloneGoals(r.Goals)
	sort.Slice(goals, func(i, j int) bool { return goals[i].Number < goals[j].Number })
	for i := range goals {
		sort.Strings(goals[i].DefaultTests)
	}
	policy := clonePolicy(r.DisclosurePolicy)
	sort.Strings(policy.Scope)
	trends := append([]Trend(nil), r.Trends...)
	sort.Slice(trends, func(i, j int) bool { return trends[i].Release < trends[j].Release })
	exceptions := append([]Exception(nil), r.Exceptions...)
	sort.Slice(exceptions, func(i, j int) bool {
		if exceptions[i].GoalNumber != exceptions[j].GoalNumber {
			return exceptions[i].GoalNumber < exceptions[j].GoalNumber
		}
		return exceptions[i].ExpiresOn < exceptions[j].ExpiresOn
	})
	return struct {
		Schema           string           `json:"schema"`
		SchemaVersion    int              `json:"schema_version"`
		Revision         int              `json:"revision"`
		Pledge           string           `json:"pledge"`
		Goals            []Goal           `json:"goals"`
		DisclosurePolicy DisclosurePolicy `json:"vulnerability_disclosure_policy"`
		Trends           []Trend          `json:"trends"`
		Exceptions       []Exception      `json:"exceptions"`
	}{r.Schema, r.SchemaVersion, r.Revision, r.Pledge, goals, policy, trends, exceptions}
}
