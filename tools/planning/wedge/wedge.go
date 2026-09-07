// Package wedge contains the kernel-pure decision records used to qualify the
// Gate A Promotion wedge. The records deliberately keep partner facts out of
// runtime domain contracts: a future customer replaces the placeholder
// values, while the shape and evidence rules remain stable.
package wedge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 1

// QualificationStatus is the result of structural wedge evaluation.
type QualificationStatus string

const (
	Qualified QualificationStatus = "QUALIFIED"
	Rejected  QualificationStatus = "REJECTED"
)

// FieldOperation is intentionally closed: an omitted operation is not a
// read, and therefore cannot acquire authority by inference.
type FieldOperation string

const (
	OperationRead     FieldOperation = "READ"
	OperationSimulate FieldOperation = "SIMULATE"
	OperationWrite    FieldOperation = "WRITE"
)

// GateDepth describes how much of a field is admitted at a phase gate.
type GateDepth string

const (
	DepthImplement    GateDepth = "IMPLEMENT"
	DepthContractOnly GateDepth = "CONTRACT_ONLY"
	DepthConformance  GateDepth = "CONFORMANCE_ONLY"
	DepthOutOfPhase   GateDepth = "OUT_OF_PHASE"
)

// Signature is the evidence that a decision record was signed. The signing
// service is intentionally outside this package; this package validates the
// signed-record envelope and its content digest.
type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	SignedAt  string `json:"signed_at"`
	Value     string `json:"value"`
}

// Violation is one deterministic validation finding.
type Violation struct {
	Record string
	Field  string
	Issue  string
}

func (v Violation) Error() string {
	if v.Field == "" {
		return fmt.Sprintf("%s: %s", v.Record, v.Issue)
	}
	return fmt.Sprintf("%s: %s: %s", v.Record, v.Field, v.Issue)
}

// Valid reports whether a validation result contains no findings.
func Valid(violations []Violation) bool { return len(violations) == 0 }

type requiredValue struct {
	field string
	value string
}

type ICPProfile struct {
	MinEmployees                  int  `json:"min_employees"`
	MaxEmployees                  int  `json:"max_employees"`
	RequiresMultipleHRIS          bool `json:"requires_multiple_hris"`
	DisqualifySingleSuiteWorkday  bool `json:"disqualify_single_suite_workday"`
	RequiresIndependentDownstream bool `json:"requires_independent_downstream"`
}

type IncumbentProfile struct {
	System         string `json:"system"`
	Product        string `json:"product"`
	Edition        string `json:"edition"`
	Version        string `json:"version"`
	Region         string `json:"region"`
	Topology       string `json:"topology"`
	APIEntitlement string `json:"api_entitlement"`
}

type FieldReference struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

type EligibleVolume struct {
	Population  string `json:"population"`
	Minimum     int    `json:"minimum"`
	Maximum     int    `json:"maximum"`
	Unit        string `json:"unit"`
	SourceRef   string `json:"source_ref"`
	Placeholder bool   `json:"placeholder"`
}

type DownstreamBoundary struct {
	System          string `json:"system"`
	Owner           string `json:"owner"`
	ObservationPath string `json:"observation_path"`
	Independent     bool   `json:"independent"`
	Placeholder     bool   `json:"placeholder"`
}

type PricePoint struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Basis       string `json:"basis"`
	ReadOnly    bool   `json:"read_only"`
	Placeholder bool   `json:"placeholder"`
}

type StopCriterion struct {
	Name        string `json:"name"`
	Comparator  string `json:"comparator"`
	Threshold   string `json:"threshold"`
	Unit        string `json:"unit"`
	EvidenceRef string `json:"evidence_ref"`
	Placeholder bool   `json:"placeholder"`
}

// PartnerManifest is WEDGE-001's signed, partner-specific qualification
// record. It is a planning artifact, not a customer or provider selection.
type PartnerManifest struct {
	SchemaVersion                 int                `json:"schema_version"`
	ManifestID                    string             `json:"manifest_id"`
	Revision                      int                `json:"revision"`
	ProblemClass                  string             `json:"problem_class"`
	ICP                           ICPProfile         `json:"icp"`
	Incumbent                     IncumbentProfile   `json:"incumbent"`
	Fields                        []FieldReference   `json:"fields"`
	EligibleVolume                EligibleVolume     `json:"eligible_volume"`
	Downstream                    DownstreamBoundary `json:"downstream"`
	NativeCapabilityAssessmentRef string             `json:"native_capability_assessment_ref"`
	Price                         PricePoint         `json:"price"`
	StopCriteria                  []StopCriterion    `json:"stop_criteria"`
	Signature                     Signature          `json:"signature"`
	Digest                        string             `json:"digest"`
}

// PartnerEvaluation is deliberately separate from the manifest. This keeps
// human-supplied placeholders visible to an operator without treating them as
// silently selected business facts.
type PartnerEvaluation struct {
	Status              QualificationStatus
	Violations          []Violation
	HumanInputsRequired []string
}

func ValidatePartnerManifest(m PartnerManifest) []Violation {
	var v []Violation
	add := func(field, issue string) { v = append(v, Violation{"PartnerManifest", field, issue}) }
	if m.SchemaVersion != SchemaVersion {
		add("schema_version", "unsupported schema version")
	}
	if strings.TrimSpace(m.ManifestID) == "" {
		add("manifest_id", "manifest id is required")
	}
	if m.Revision < 1 {
		add("revision", "revision must be positive")
	}
	problem := strings.ToLower(m.ProblemClass)
	if !strings.Contains(problem, "promotion") || !strings.Contains(problem, "compensation") {
		add("problem_class", "must identify a non-duplicative promotion/compensation failure class")
	}
	if m.ICP.MinEmployees < 1 || m.ICP.MaxEmployees < m.ICP.MinEmployees {
		add("icp", "employee range is invalid")
	}
	if !m.ICP.RequiresMultipleHRIS {
		add("icp.requires_multiple_hris", "the revised ICP requires more than one HR system of record")
	}
	if !m.ICP.DisqualifySingleSuiteWorkday {
		add("icp.disqualify_single_suite_workday", "single-suite Workday estates must be disqualified")
	}
	if !m.ICP.RequiresIndependentDownstream {
		add("icp.requires_independent_downstream", "an independently owned downstream system is required")
	}
	for _, value := range []requiredValue{
		{field: "incumbent.system", value: m.Incumbent.System}, {field: "incumbent.product", value: m.Incumbent.Product},
		{field: "incumbent.edition", value: m.Incumbent.Edition}, {field: "incumbent.version", value: m.Incumbent.Version},
		{field: "incumbent.region", value: m.Incumbent.Region}, {field: "incumbent.topology", value: m.Incumbent.Topology},
		{field: "incumbent.api_entitlement", value: m.Incumbent.APIEntitlement},
	} {
		if strings.TrimSpace(value.value) == "" {
			add(value.field, "exact incumbent value is required")
		}
	}
	if len(m.Fields) == 0 {
		add("fields", "exact pilot field set is required")
	}
	seenFields := map[string]bool{}
	for i, f := range m.Fields {
		if strings.TrimSpace(f.Path) == "" {
			add(fmt.Sprintf("fields[%d].path", i), "field path is required")
		}
		if strings.TrimSpace(f.Purpose) == "" {
			add(fmt.Sprintf("fields[%d].purpose", i), "field purpose is required")
		}
		if seenFields[f.Path] {
			add(fmt.Sprintf("fields[%d].path", i), "implicit duplicate field")
		}
		seenFields[f.Path] = true
	}
	if strings.TrimSpace(m.EligibleVolume.Population) == "" {
		add("eligible_volume.population", "eligible population is required")
	}
	if m.EligibleVolume.Minimum < 1 || m.EligibleVolume.Maximum < m.EligibleVolume.Minimum {
		add("eligible_volume", "eligible volume range is invalid")
	}
	if strings.TrimSpace(m.EligibleVolume.Unit) == "" {
		add("eligible_volume.unit", "eligible volume unit is required")
	}
	if strings.TrimSpace(m.EligibleVolume.SourceRef) == "" {
		add("eligible_volume.source_ref", "eligible volume source is required")
	}
	for _, value := range []requiredValue{
		{field: "downstream.system", value: m.Downstream.System}, {field: "downstream.owner", value: m.Downstream.Owner},
		{field: "downstream.observation_path", value: m.Downstream.ObservationPath},
	} {
		if strings.TrimSpace(value.value) == "" {
			add(value.field, "downstream boundary value is required")
		}
	}
	if !m.Downstream.Independent {
		add("downstream.independent", "downstream boundary must be independently owned")
	}
	if strings.TrimSpace(m.NativeCapabilityAssessmentRef) == "" {
		add("native_capability_assessment_ref", "native-capability assessment is required")
	}
	if m.Price.AmountMinor < 1 {
		add("price.amount_minor", "positive price point is required")
	}
	if strings.TrimSpace(m.Price.Currency) == "" || len(m.Price.Currency) != 3 {
		add("price.currency", "ISO-like three-letter currency is required")
	}
	if strings.TrimSpace(m.Price.Basis) == "" {
		add("price.basis", "price basis is required")
	}
	if !m.Price.ReadOnly {
		add("price.read_only", "Gate A price must be explicitly read-only")
	}
	if len(m.StopCriteria) == 0 {
		add("stop_criteria", "at least one stop criterion is required")
	}
	for i, c := range m.StopCriteria {
		for _, value := range []requiredValue{
			{field: "name", value: c.Name}, {field: "comparator", value: c.Comparator}, {field: "threshold", value: c.Threshold},
			{field: "unit", value: c.Unit}, {field: "evidence_ref", value: c.EvidenceRef},
		} {
			if strings.TrimSpace(value.value) == "" {
				add(fmt.Sprintf("stop_criteria[%d].%s", i, value.field), "stop criterion value is required")
			}
		}
	}
	v = append(v, validateSignature(m.Signature, "PartnerManifest", m.Digest)...)
	if strings.TrimSpace(m.Digest) != "" && m.Digest != DigestPartnerManifest(m) {
		add("digest", "content digest does not match the manifest")
	}
	return v
}

func (m PartnerManifest) Validate() []Violation { return ValidatePartnerManifest(m) }

func EvaluatePartnerManifest(m PartnerManifest) PartnerEvaluation {
	violations := ValidatePartnerManifest(m)
	result := PartnerEvaluation{Status: Qualified, Violations: violations}
	if len(violations) > 0 {
		result.Status = Rejected
	}
	if m.ICP.MinEmployees == 0 || m.ICP.MaxEmployees == 0 {
		result.HumanInputsRequired = append(result.HumanInputsRequired, "ICP employee range")
	}
	if m.EligibleVolume.Placeholder {
		result.HumanInputsRequired = append(result.HumanInputsRequired, "eligible volume")
	}
	if m.Downstream.Placeholder {
		result.HumanInputsRequired = append(result.HumanInputsRequired, "independent downstream system and owner")
	}
	if m.Price.Placeholder {
		result.HumanInputsRequired = append(result.HumanInputsRequired, "read-only P1A price point")
	}
	for _, c := range m.StopCriteria {
		if c.Placeholder {
			result.HumanInputsRequired = append(result.HumanInputsRequired, "stop criterion: "+c.Name)
		}
	}
	return result
}

type MetricDefinition struct {
	Name               string   `json:"name"`
	Numerator          string   `json:"numerator"`
	Denominator        string   `json:"denominator"`
	Population         string   `json:"population"`
	WindowStart        string   `json:"window_start"`
	WindowEnd          string   `json:"window_end"`
	SourceRef          string   `json:"source_ref"`
	Exclusions         []string `json:"exclusions"`
	ExclusionsDeclared bool     `json:"exclusions_declared"`
	Unit               string   `json:"unit"`
}

type MetricObservation struct {
	MetricName  string `json:"metric_name"`
	Numerator   int64  `json:"numerator"`
	Denominator int64  `json:"denominator"`
	Population  string `json:"population"`
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
	SourceRef   string `json:"source_ref"`
}

type Baseline struct {
	SchemaVersion      int                `json:"schema_version"`
	BaselineID         string             `json:"baseline_id"`
	PartnerManifestRef string             `json:"partner_manifest_ref"`
	AsOf               string             `json:"as_of"`
	Metrics            []MetricDefinition `json:"metrics"`
	Signature          Signature          `json:"signature"`
	Digest             string             `json:"digest"`
}

type CompiledMetric struct {
	Name        string `json:"name"`
	Numerator   int64  `json:"numerator"`
	Denominator int64  `json:"denominator"`
	RateBP      int64  `json:"rate_basis_points"`
}

type CompiledBaseline struct {
	BaselineDigest string           `json:"baseline_digest"`
	Metrics        []CompiledMetric `json:"metrics"`
}

func ValidateBaseline(b Baseline) []Violation {
	var v []Violation
	add := func(field, issue string) { v = append(v, Violation{"Baseline", field, issue}) }
	if b.SchemaVersion != SchemaVersion {
		add("schema_version", "unsupported schema version")
	}
	if strings.TrimSpace(b.BaselineID) == "" {
		add("baseline_id", "baseline id is required")
	}
	if strings.TrimSpace(b.PartnerManifestRef) == "" {
		add("partner_manifest_ref", "baseline must bind to WEDGE-001")
	}
	if !validDate(b.AsOf) {
		add("as_of", "as-of date must be YYYY-MM-DD")
	}
	if len(b.Metrics) == 0 {
		add("metrics", "at least one metric definition is required")
	}
	seen := map[string]bool{}
	for i, m := range b.Metrics {
		prefix := fmt.Sprintf("metrics[%d]", i)
		for _, value := range []requiredValue{
			{field: "name", value: m.Name}, {field: "numerator", value: m.Numerator}, {field: "denominator", value: m.Denominator},
			{field: "population", value: m.Population}, {field: "window_start", value: m.WindowStart}, {field: "window_end", value: m.WindowEnd},
			{field: "source_ref", value: m.SourceRef}, {field: "unit", value: m.Unit},
		} {
			if strings.TrimSpace(value.value) == "" {
				add(prefix+"."+value.field, "metric definition value is required")
			}
		}
		if !validDate(m.WindowStart) || !validDate(m.WindowEnd) || (validDate(m.WindowStart) && validDate(m.WindowEnd) && m.WindowEnd < m.WindowStart) {
			add(prefix+".window", "measurement window is invalid")
		}
		if !m.ExclusionsDeclared {
			add(prefix+".exclusions_declared", "exclusions must be explicitly declared, even when empty")
		}
		if seen[m.Name] {
			add(prefix+".name", "duplicate metric definition")
		}
		seen[m.Name] = true
	}
	v = append(v, validateSignature(b.Signature, "Baseline", b.Digest)...)
	if strings.TrimSpace(b.Digest) != "" && b.Digest != DigestBaseline(b) {
		add("digest", "content digest does not match the baseline")
	}
	return v
}

func (b Baseline) Validate() []Violation { return ValidateBaseline(b) }

// CompileBaseline binds source observations to the frozen metric definitions
// and calculates integer basis points, avoiding floating-point drift.
func CompileBaseline(b Baseline, observations []MetricObservation) (CompiledBaseline, []Violation) {
	violations := ValidateBaseline(b)
	byName := make(map[string]MetricObservation, len(observations))
	for _, o := range observations {
		byName[o.MetricName] = o
	}
	compiled := CompiledBaseline{BaselineDigest: b.Digest, Metrics: make([]CompiledMetric, 0, len(b.Metrics))}
	for i, d := range b.Metrics {
		o, ok := byName[d.Name]
		if !ok {
			violations = append(violations, Violation{"Baseline", fmt.Sprintf("metrics[%d]", i), "source observation is missing"})
			continue
		}
		if o.Denominator <= 0 {
			violations = append(violations, Violation{"Baseline", d.Name, "denominator must be positive"})
			continue
		}
		if o.Numerator < 0 || o.Numerator > o.Denominator {
			violations = append(violations, Violation{"Baseline", d.Name, "numerator must be between zero and denominator"})
			continue
		}
		if o.Population != d.Population || o.WindowStart != d.WindowStart || o.WindowEnd != d.WindowEnd || o.SourceRef != d.SourceRef {
			violations = append(violations, Violation{"Baseline", d.Name, "observation does not match the frozen population, window, or source"})
			continue
		}
		if o.Numerator > math.MaxInt64/10000 {
			violations = append(violations, Violation{"Baseline", d.Name, "numerator is too large"})
			continue
		}
		compiled.Metrics = append(compiled.Metrics, CompiledMetric{Name: d.Name, Numerator: o.Numerator, Denominator: o.Denominator, RateBP: (o.Numerator*10000 + o.Denominator/2) / o.Denominator})
	}
	sort.Slice(compiled.Metrics, func(i, j int) bool { return compiled.Metrics[i].Name < compiled.Metrics[j].Name })
	return compiled, violations
}

type NativeCapabilityClaim struct {
	Capability      string `json:"capability"`
	Native          bool   `json:"native"`
	LicenseState    string `json:"license_state"`
	Configured      bool   `json:"configured"`
	EvidenceRef     string `json:"evidence_ref"`
	EvidenceDate    string `json:"evidence_date"`
	EvidenceVersion string `json:"evidence_version"`
}

type NativeCapabilityAssessment struct {
	SchemaVersion      int                     `json:"schema_version"`
	AssessmentID       string                  `json:"assessment_id"`
	PartnerManifestRef string                  `json:"partner_manifest_ref"`
	Product            string                  `json:"product"`
	Edition            string                  `json:"edition"`
	Version            string                  `json:"version"`
	LicenseRef         string                  `json:"license_ref"`
	ConfigurationRef   string                  `json:"configuration_ref"`
	Gaps               []string                `json:"gaps"`
	AsOf               string                  `json:"as_of"`
	FreshnessDays      int                     `json:"freshness_days"`
	Claims             []NativeCapabilityClaim `json:"claims"`
	Signature          Signature               `json:"signature"`
	Digest             string                  `json:"digest"`
}

func ValidateNativeCapabilityAssessment(a NativeCapabilityAssessment) []Violation {
	var v []Violation
	add := func(field, issue string) { v = append(v, Violation{"NativeCapabilityAssessment", field, issue}) }
	if a.SchemaVersion != SchemaVersion {
		add("schema_version", "unsupported schema version")
	}
	for _, value := range []requiredValue{
		{field: "assessment_id", value: a.AssessmentID}, {field: "partner_manifest_ref", value: a.PartnerManifestRef},
		{field: "product", value: a.Product}, {field: "edition", value: a.Edition}, {field: "version", value: a.Version},
		{field: "license_ref", value: a.LicenseRef}, {field: "configuration_ref", value: a.ConfigurationRef},
	} {
		if strings.TrimSpace(value.value) == "" {
			add(value.field, "assessment value is required")
		}
	}
	if !validDate(a.AsOf) {
		add("as_of", "as-of date must be YYYY-MM-DD")
	}
	if a.FreshnessDays < 0 {
		add("freshness_days", "freshness window cannot be negative")
	}
	if a.Gaps == nil {
		add("gaps", "capability gaps must be explicitly recorded, even when empty")
	}
	for i, gap := range a.Gaps {
		if strings.TrimSpace(gap) == "" {
			add(fmt.Sprintf("gaps[%d]", i), "capability gap must not be empty")
		}
	}
	asOf, _ := time.Parse("2006-01-02", a.AsOf)
	if len(a.Claims) == 0 {
		add("claims", "at least one native capability claim is required")
	}
	for i, c := range a.Claims {
		prefix := fmt.Sprintf("claims[%d]", i)
		for _, value := range []requiredValue{
			{field: "capability", value: c.Capability}, {field: "license_state", value: c.LicenseState},
			{field: "evidence_ref", value: c.EvidenceRef}, {field: "evidence_date", value: c.EvidenceDate},
			{field: "evidence_version", value: c.EvidenceVersion},
		} {
			if strings.TrimSpace(value.value) == "" {
				add(prefix+"."+value.field, "claim evidence value is required")
			}
		}
		if c.LicenseState != "LICENSED" && c.Native {
			add(prefix+".license_state", "native claim is not licensed by the partner")
		}
		if !c.Configured && c.Native {
			add(prefix+".configured", "native claim is not configured by the partner")
		}
		if strings.TrimSpace(c.EvidenceDate) == "" {
			continue
		}
		if !validDate(c.EvidenceDate) {
			add(prefix+".evidence_date", "evidence date must be YYYY-MM-DD")
			continue
		}
		observed, _ := time.Parse("2006-01-02", c.EvidenceDate)
		if validDate(a.AsOf) && observed.After(asOf) {
			add(prefix+".evidence_date", "evidence is newer than assessment as-of date")
		} else if validDate(a.AsOf) && asOf.Sub(observed) > time.Duration(a.FreshnessDays)*24*time.Hour {
			add(prefix+".evidence_date", "native capability claim is stale")
		}
	}
	v = append(v, validateSignature(a.Signature, "NativeCapabilityAssessment", a.Digest)...)
	if strings.TrimSpace(a.Digest) != "" && a.Digest != DigestNativeCapabilityAssessment(a) {
		add("digest", "content digest does not match the assessment")
	}
	return v
}

func (a NativeCapabilityAssessment) Validate() []Violation {
	return ValidateNativeCapabilityAssessment(a)
}

type PilotField struct {
	Path           string         `json:"path"`
	Domain         string         `json:"domain"`
	Operation      FieldOperation `json:"operation"`
	Authority      string         `json:"authority"`
	Classification string         `json:"classification"`
	Purpose        string         `json:"purpose"`
	Source         string         `json:"source"`
	EffectiveTime  string         `json:"effective_time"`
	GateADepth     GateDepth      `json:"gate_a_depth"`
	GateBDepth     GateDepth      `json:"gate_b_depth"`
	GateAOperation FieldOperation `json:"gate_a_operation"`
	GateBOperation FieldOperation `json:"gate_b_operation"`
}

type PilotFieldManifest struct {
	SchemaVersion      int          `json:"schema_version"`
	ManifestID         string       `json:"manifest_id"`
	PartnerManifestRef string       `json:"partner_manifest_ref"`
	GateAAuthority     string       `json:"gate_a_authority"`
	Fields             []PilotField `json:"fields"`
	Signature          Signature    `json:"signature"`
	Digest             string       `json:"digest"`
}

var allowedFieldDomains = map[string]bool{"job": true, "manager": true, "organization": true, "position": true, "compensation": true}

func ValidatePilotFieldManifest(m PilotFieldManifest) []Violation {
	var v []Violation
	add := func(field, issue string) { v = append(v, Violation{"PilotFieldManifest", field, issue}) }
	if m.SchemaVersion != SchemaVersion {
		add("schema_version", "unsupported schema version")
	}
	if strings.TrimSpace(m.ManifestID) == "" {
		add("manifest_id", "manifest id is required")
	}
	if strings.TrimSpace(m.PartnerManifestRef) == "" {
		add("partner_manifest_ref", "field manifest must bind to WEDGE-001")
	}
	if m.GateAAuthority != "READ_ONLY" {
		add("gate_a_authority", "Gate A field authority must be READ_ONLY")
	}
	if len(m.Fields) == 0 {
		add("fields", "explicit field set is required")
	}
	seen := map[string]bool{}
	for i, f := range m.Fields {
		prefix := fmt.Sprintf("fields[%d]", i)
		for _, value := range []requiredValue{
			{field: "path", value: f.Path}, {field: "domain", value: f.Domain}, {field: "authority", value: f.Authority},
			{field: "classification", value: f.Classification}, {field: "purpose", value: f.Purpose},
			{field: "source", value: f.Source}, {field: "effective_time", value: f.EffectiveTime},
		} {
			if strings.TrimSpace(value.value) == "" {
				add(prefix+"."+value.field, "field metadata is required")
			}
		}
		if !allowedFieldDomains[f.Domain] {
			add(prefix+".domain", "field domain is outside the Promotion family")
		}
		if strings.Contains(f.Path, "..") || strings.HasPrefix(f.Path, "/") || strings.ContainsAny(f.Path, "*[]") {
			add(prefix+".path", "field path must be a literal typed path")
		}
		if seen[f.Path] {
			add(prefix+".path", "duplicate or implicit field")
		}
		seen[f.Path] = true
		if f.Operation != OperationRead && f.Operation != OperationSimulate && f.Operation != OperationWrite {
			add(prefix+".operation", "unknown field operation")
		}
		if f.GateAOperation == OperationWrite {
			add(prefix+".gate_a_operation", "Gate A cannot grant mutation authority")
		}
		if f.Operation == OperationWrite && f.GateBOperation != OperationWrite {
			add(prefix+".gate_b_operation", "write field must be explicitly bound at Gate B")
		}
		if f.GateADepth == "" || f.GateBDepth == "" {
			add(prefix+".depth", "Gate A and Gate B depth are both required")
		}
	}
	v = append(v, validateSignature(m.Signature, "PilotFieldManifest", m.Digest)...)
	if strings.TrimSpace(m.Digest) != "" && m.Digest != DigestPilotFieldManifest(m) {
		add("digest", "content digest does not match the field manifest")
	}
	return v
}

func (m PilotFieldManifest) Validate() []Violation { return ValidatePilotFieldManifest(m) }

func validateSignature(s Signature, record, digest string) []Violation {
	var v []Violation
	add := func(field, issue string) { v = append(v, Violation{record, field, issue}) }
	if strings.TrimSpace(s.Algorithm) == "" {
		add("signature.algorithm", "signature algorithm is required")
	}
	if strings.TrimSpace(s.KeyID) == "" {
		add("signature.key_id", "signing key id is required")
	}
	if !validDate(s.SignedAt) {
		add("signature.signed_at", "signature date must be YYYY-MM-DD")
	}
	if strings.TrimSpace(s.Value) == "" {
		add("signature.value", "signature value is required")
	}
	if strings.TrimSpace(digest) == "" {
		add("digest", "content digest is required")
	}
	return v
}

func validDate(value string) bool { _, err := time.Parse("2006-01-02", value); return err == nil }

func digestValue(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func DigestPartnerManifest(m PartnerManifest) string { m.Digest = ""; return digestValue(m) }
func DigestBaseline(b Baseline) string               { b.Digest = ""; return digestValue(b) }
func DigestNativeCapabilityAssessment(a NativeCapabilityAssessment) string {
	a.Digest = ""
	return digestValue(a)
}
func DigestPilotFieldManifest(m PilotFieldManifest) string { m.Digest = ""; return digestValue(m) }

func (m PartnerManifest) VerifyDigest() error {
	if m.Digest != DigestPartnerManifest(m) {
		return fmt.Errorf("partner manifest digest mismatch")
	}
	return nil
}
func (b Baseline) VerifyDigest() error {
	if b.Digest != DigestBaseline(b) {
		return fmt.Errorf("baseline digest mismatch")
	}
	return nil
}
func (a NativeCapabilityAssessment) VerifyDigest() error {
	if a.Digest != DigestNativeCapabilityAssessment(a) {
		return fmt.Errorf("native capability assessment digest mismatch")
	}
	return nil
}
func (m PilotFieldManifest) VerifyDigest() error {
	if m.Digest != DigestPilotFieldManifest(m) {
		return fmt.Errorf("pilot field manifest digest mismatch")
	}
	return nil
}

// Placeholder fixtures are intentionally labelled. Their values are useful
// for tests and schema examples only; they are not customer decisions.
func PlaceholderSignature() Signature {
	return Signature{Algorithm: "placeholder-sha256", KeyID: "HUMAN_SUPPLY_SIGNING_KEY", SignedAt: "2026-09-06", Value: "PLACEHOLDER_SIGNATURE"}
}

func PlaceholderPartnerManifest() PartnerManifest {
	m := PartnerManifest{SchemaVersion: SchemaVersion, ManifestID: "partner-manifest:PLACEHOLDER", Revision: 1, ProblemClass: "promotion_compensation_cross_system_gap", ICP: ICPProfile{MinEmployees: 2000, MaxEmployees: 25000, RequiresMultipleHRIS: true, DisqualifySingleSuiteWorkday: true, RequiresIndependentDownstream: true}, Incumbent: IncumbentProfile{System: "PLACEHOLDER_HRIS", Product: "PLACEHOLDER_PRODUCT", Edition: "PLACEHOLDER_EDITION", Version: "PLACEHOLDER_VERSION", Region: "PLACEHOLDER_REGION", Topology: "PLACEHOLDER_TOPOLOGY", APIEntitlement: "PLACEHOLDER_READ_ENTITLEMENT"}, Fields: []FieldReference{{Path: "job.job_code", Purpose: "promotion job identity"}, {Path: "compensation.base", Purpose: "promotion base pay"}}, EligibleVolume: EligibleVolume{Population: "PLACEHOLDER_ELIGIBLE_POPULATION", Minimum: 1, Maximum: 2, Unit: "workers", SourceRef: "fixture:placeholder-volume", Placeholder: true}, Downstream: DownstreamBoundary{System: "PLACEHOLDER_DOWNSTREAM", Owner: "HUMAN_SUPPLY_OWNER", ObservationPath: "PLACEHOLDER_OBSERVATION_PATH", Independent: true, Placeholder: true}, NativeCapabilityAssessmentRef: "native-capability:PLACEHOLDER", Price: PricePoint{AmountMinor: 1, Currency: "USD", Basis: "P1A_READ_ONLY_PILOT", ReadOnly: true, Placeholder: true}, StopCriteria: []StopCriterion{{Name: "PLACEHOLDER_STOP_THRESHOLD", Comparator: "<", Threshold: "HUMAN_SUPPLY_THRESHOLD", Unit: "HUMAN_SUPPLY_UNIT", EvidenceRef: "fixture:placeholder-stop", Placeholder: true}}, Signature: PlaceholderSignature()}
	m.Digest = DigestPartnerManifest(m)
	return m
}

func PlaceholderBaseline() Baseline {
	b := Baseline{SchemaVersion: SchemaVersion, BaselineID: "baseline:PLACEHOLDER", PartnerManifestRef: "partner-manifest:PLACEHOLDER", AsOf: "2026-09-06", Metrics: []MetricDefinition{{Name: "change_completion_time", Numerator: "completed_change_duration_sum", Denominator: "completed_change_count", Population: "PLACEHOLDER_POPULATION", WindowStart: "2026-08-01", WindowEnd: "2026-08-31", SourceRef: "fixture:placeholder-source", Exclusions: []string{"PLACEHOLDER_EXCLUSION_RULE"}, ExclusionsDeclared: true, Unit: "minutes"}}, Signature: PlaceholderSignature()}
	b.Digest = DigestBaseline(b)
	return b
}

func PlaceholderNativeCapabilityAssessment() NativeCapabilityAssessment {
	a := NativeCapabilityAssessment{SchemaVersion: SchemaVersion, AssessmentID: "native-capability:PLACEHOLDER", PartnerManifestRef: "partner-manifest:PLACEHOLDER", Product: "PLACEHOLDER_PRODUCT", Edition: "PLACEHOLDER_EDITION", Version: "PLACEHOLDER_VERSION", LicenseRef: "fixture:placeholder-license", ConfigurationRef: "fixture:placeholder-configuration", Gaps: []string{"PLACEHOLDER_GAP"}, AsOf: "2026-09-06", FreshnessDays: 30, Claims: []NativeCapabilityClaim{{Capability: "promotion_workflow", Native: true, LicenseState: "LICENSED", Configured: true, EvidenceRef: "fixture:placeholder-capability", EvidenceDate: "2026-09-01", EvidenceVersion: "PLACEHOLDER_VERSION"}}, Signature: PlaceholderSignature()}
	a.Digest = DigestNativeCapabilityAssessment(a)
	return a
}

func PlaceholderPilotFieldManifest() PilotFieldManifest {
	fields := []PilotField{{Path: "job.job_code", Domain: "job", Operation: OperationSimulate, Authority: "PLACEHOLDER_HRIS", Classification: "WORKFORCE", Purpose: "promotion job simulation", Source: "fixture:placeholder-worker", EffectiveTime: "effective_date", GateADepth: DepthImplement, GateBDepth: DepthImplement, GateAOperation: OperationSimulate, GateBOperation: OperationWrite}, {Path: "manager.manager_id", Domain: "manager", Operation: OperationRead, Authority: "PLACEHOLDER_HRIS", Classification: "WORKFORCE", Purpose: "manager context", Source: "fixture:placeholder-worker", EffectiveTime: "effective_date", GateADepth: DepthImplement, GateBDepth: DepthContractOnly, GateAOperation: OperationRead, GateBOperation: OperationRead}, {Path: "organization.organization_id", Domain: "organization", Operation: OperationRead, Authority: "PLACEHOLDER_HRIS", Classification: "WORKFORCE", Purpose: "organization context", Source: "fixture:placeholder-worker", EffectiveTime: "effective_date", GateADepth: DepthImplement, GateBDepth: DepthContractOnly, GateAOperation: OperationRead, GateBOperation: OperationRead}, {Path: "position.position_id", Domain: "position", Operation: OperationRead, Authority: "PLACEHOLDER_HRIS", Classification: "WORKFORCE", Purpose: "position context", Source: "fixture:placeholder-position", EffectiveTime: "effective_date", GateADepth: DepthImplement, GateBDepth: DepthContractOnly, GateAOperation: OperationRead, GateBOperation: OperationRead}, {Path: "compensation.base", Domain: "compensation", Operation: OperationSimulate, Authority: "PLACEHOLDER_HRIS", Classification: "COMPENSATION", Purpose: "promotion pay simulation", Source: "fixture:placeholder-compensation", EffectiveTime: "effective_date", GateADepth: DepthImplement, GateBDepth: DepthImplement, GateAOperation: OperationSimulate, GateBOperation: OperationWrite}}
	m := PilotFieldManifest{SchemaVersion: SchemaVersion, ManifestID: "field-manifest:PLACEHOLDER", PartnerManifestRef: "partner-manifest:PLACEHOLDER", GateAAuthority: "READ_ONLY", Fields: fields, Signature: PlaceholderSignature()}
	m.Digest = DigestPilotFieldManifest(m)
	return m
}
