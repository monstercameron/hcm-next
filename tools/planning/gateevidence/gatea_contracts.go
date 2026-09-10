package gateevidence

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// GateAContractVersion is the version of the pure Gate A decision/evidence
// records in this file. These records deliberately do not grant authority or
// persist customer data; they describe and evaluate evidence only.
const GateAContractVersion = 1

// ContractViolation is a deterministic validation diagnostic for a Gate A
// contract. Code is stable enough for policy tooling; Field is a JSON-shaped
// path suitable for an operator report.
type ContractViolation struct {
	Code   string
	Field  string
	Detail string
}

func (v ContractViolation) String() string {
	return fmt.Sprintf("%s: %s: %s", v.Code, v.Field, v.Detail)
}

func violation(code, field, detail string) ContractViolation {
	return ContractViolation{Code: code, Field: field, Detail: detail}
}

func digestValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func placeholder(value string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(value)), "PLACEHOLDER_")
}

// DownstreamTopology describes the independently owned observation handoff
// required by WEDGE-005. It is a typed boundary, not a prose claim.
type DownstreamTopology struct {
	SchemaVersion             int    `json:"schema_version" yaml:"schema_version"`
	SourceSystem              string `json:"source_system" yaml:"source_system"`
	SourceAuthority           string `json:"source_authority" yaml:"source_authority"`
	DownstreamSystem          string `json:"downstream_system" yaml:"downstream_system"`
	DownstreamAuthority       string `json:"downstream_authority" yaml:"downstream_authority"`
	HandoffRef                string `json:"handoff_ref" yaml:"handoff_ref"`
	AcknowledgementMechanism  string `json:"acknowledgement_mechanism" yaml:"acknowledgement_mechanism"`
	ObservationMechanism      string `json:"observation_mechanism" yaml:"observation_mechanism"`
	AccountableOwner          string `json:"accountable_owner" yaml:"accountable_owner"`
	NarrowedSingleSystemClaim bool   `json:"narrowed_single_system_claim" yaml:"narrowed_single_system_claim"`
}

// Digest returns the stable topology identity used by field and privacy
// approvals. The topology contains no digest field, so it cannot self-sign.
func (t DownstreamTopology) Digest() string { return digestValue(t) }

// ValidateTopology is the WEDGE-005 evaluator. A same-system claim is valid
// only when it explicitly narrows the claim instead of pretending to be an
// independent downstream handoff.
func ValidateTopology(t DownstreamTopology) []ContractViolation {
	var out []ContractViolation
	add := func(field, detail string) { out = append(out, violation("TOPOLOGY_INCOMPLETE", field, detail)) }
	if t.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	for _, fieldValue := range []struct{ field, value string }{
		{"source_system", t.SourceSystem}, {"source_authority", t.SourceAuthority},
		{"downstream_system", t.DownstreamSystem}, {"downstream_authority", t.DownstreamAuthority},
		{"handoff_ref", t.HandoffRef}, {"acknowledgement_mechanism", t.AcknowledgementMechanism},
		{"observation_mechanism", t.ObservationMechanism}, {"accountable_owner", t.AccountableOwner},
	} {
		if strings.TrimSpace(fieldValue.value) == "" {
			add(fieldValue.field, "missing")
		}
	}
	if t.SourceSystem == t.DownstreamSystem || t.SourceAuthority == t.DownstreamAuthority {
		if !t.NarrowedSingleSystemClaim {
			out = append(out, violation("SINGLE_SYSTEM_CROSS_SYSTEM_CLAIM", "downstream_system", "source and downstream share an authority boundary without an explicit narrowed claim"))
		}
	}
	if t.NarrowedSingleSystemClaim && strings.TrimSpace(t.HandoffRef) == "" {
		out = append(out, violation("NARROWED_CLAIM_MISSING_SCOPE", "handoff_ref", "a narrowed single-system claim must still name its bounded observation scope"))
	}
	return out
}

// FieldSpec is one exact pilot read field. Every property is part of the
// field-manifest digest and must be repeated in the approved treatment.
type FieldSpec struct {
	Path           string `json:"path" yaml:"path"`
	Authority      string `json:"authority" yaml:"authority"`
	Classification string `json:"classification" yaml:"classification"`
	Purpose        string `json:"purpose" yaml:"purpose"`
	Source         string `json:"source" yaml:"source"`
	EffectiveTime  string `json:"effective_time" yaml:"effective_time"`
	PhaseDepth     string `json:"phase_depth" yaml:"phase_depth"`
	Access         string `json:"access" yaml:"access"`
}

// FieldManifest is the WEDGE-004 exact field boundary. Gate A is read-only;
// Gate B fields can be represented later without widening this manifest.
type FieldManifest struct {
	SchemaVersion  int         `json:"schema_version" yaml:"schema_version"`
	ManifestID     string      `json:"manifest_id" yaml:"manifest_id"`
	TopologyDigest string      `json:"topology_digest" yaml:"topology_digest"`
	Fields         []FieldSpec `json:"fields" yaml:"fields"`
	GateAReadOnly  bool        `json:"gate_a_read_only" yaml:"gate_a_read_only"`
}

// PilotFieldManifest is an explicit name for callers that use the todo's
// terminology; it is an alias so both names retain one contract identity.
type PilotFieldManifest = FieldManifest

func (m FieldManifest) Digest() string { return digestValue(m) }

func ValidateFieldManifest(m FieldManifest, topology DownstreamTopology) []ContractViolation {
	var out []ContractViolation
	add := func(code, field, detail string) { out = append(out, violation(code, field, detail)) }
	if m.SchemaVersion == 0 {
		add("FIELD_MANIFEST_INCOMPLETE", "schema_version", "missing")
	}
	if strings.TrimSpace(m.ManifestID) == "" {
		add("FIELD_MANIFEST_INCOMPLETE", "manifest_id", "missing")
	}
	if m.TopologyDigest == "" || m.TopologyDigest != topology.Digest() {
		add("FIELD_TOPOLOGY_MISMATCH", "topology_digest", "field manifest is not bound to the supplied topology")
	}
	if !m.GateAReadOnly {
		add("FIELD_WRITE_AUTHORITY", "gate_a_read_only", "Gate A field manifests must be read-only")
	}
	if len(m.Fields) == 0 {
		add("FIELD_MANIFEST_INCOMPLETE", "fields", "missing")
	}
	seen := make(map[string]bool, len(m.Fields))
	for i, f := range m.Fields {
		prefix := fmt.Sprintf("fields[%d]", i)
		for _, fieldValue := range []struct{ name, value string }{
			{"path", f.Path}, {"authority", f.Authority}, {"classification", f.Classification},
			{"purpose", f.Purpose}, {"source", f.Source}, {"effective_time", f.EffectiveTime},
			{"phase_depth", f.PhaseDepth}, {"access", f.Access},
		} {
			if strings.TrimSpace(fieldValue.value) == "" {
				add("FIELD_UNCLASSIFIED", prefix+"."+fieldValue.name, "missing")
			}
		}
		if seen[f.Path] {
			add("FIELD_DUPLICATE", prefix+".path", "field path is repeated")
		}
		seen[f.Path] = true
		if strings.ToUpper(f.Access) != "READ" {
			add("FIELD_WRITE_AUTHORITY", prefix+".access", "Gate A permits READ only")
		}
	}
	return out
}

// DataTreatment is one approved system/field/region/purpose/processor/
// retention path required by WEDGE-010.
type DataTreatment struct {
	System         string `json:"system" yaml:"system"`
	FieldPath      string `json:"field_path" yaml:"field_path"`
	Region         string `json:"region" yaml:"region"`
	Purpose        string `json:"purpose" yaml:"purpose"`
	Processor      string `json:"processor" yaml:"processor"`
	Retention      string `json:"retention" yaml:"retention"`
	Classification string `json:"classification" yaml:"classification"`
}

// DataProcessingApproval is the decision-shaped WEDGE-010 record. A
// placeholder approval is structurally representable for planning, but
// Qualify/Validate still requires a human-approved decision and exact binds.
type DataProcessingApproval struct {
	SchemaVersion       int             `json:"schema_version" yaml:"schema_version"`
	ApprovalID          string          `json:"approval_id" yaml:"approval_id"`
	Decision            string          `json:"decision" yaml:"decision"`
	TopologyDigest      string          `json:"topology_digest" yaml:"topology_digest"`
	FieldManifestDigest string          `json:"field_manifest_digest" yaml:"field_manifest_digest"`
	Treatments          []DataTreatment `json:"treatments" yaml:"treatments"`
	ApprovedBy          string          `json:"approved_by" yaml:"approved_by"`
	ApprovedAt          string          `json:"approved_at" yaml:"approved_at"`
	ExpiresAt           string          `json:"expires_at" yaml:"expires_at"`
}

func (a DataProcessingApproval) Digest() string { return digestValue(a) }

func ValidateDataProcessingApproval(a DataProcessingApproval, topology DownstreamTopology, fields FieldManifest, now time.Time) []ContractViolation {
	var out []ContractViolation
	add := func(code, field, detail string) { out = append(out, violation(code, field, detail)) }
	out = append(out, ValidateTopology(topology)...)
	if a.SchemaVersion == 0 || strings.TrimSpace(a.ApprovalID) == "" {
		add("APPROVAL_INCOMPLETE", "schema_version/approval_id", "schema version and approval id are required")
	}
	if a.Decision != "APPROVED" {
		add("APPROVAL_NOT_APPROVED", "decision", "data processing approval must be APPROVED")
	}
	if a.TopologyDigest == "" || a.TopologyDigest != topology.Digest() {
		add("APPROVAL_TOPOLOGY_MISMATCH", "topology_digest", "approval does not bind the supplied topology")
	}
	if a.FieldManifestDigest == "" || a.FieldManifestDigest != fields.Digest() {
		add("APPROVAL_FIELD_MANIFEST_MISMATCH", "field_manifest_digest", "approval does not bind the supplied field manifest")
	}
	for _, fieldValue := range []struct{ field, value string }{{"approved_by", a.ApprovedBy}, {"approved_at", a.ApprovedAt}, {"expires_at", a.ExpiresAt}} {
		if strings.TrimSpace(fieldValue.value) == "" {
			add("APPROVAL_INCOMPLETE", fieldValue.field, "missing")
		}
	}
	if expiry, err := time.Parse(dateLayout, a.ExpiresAt); err == nil && !now.Before(expiry.Add(24*time.Hour)) {
		add("APPROVAL_EXPIRED", "expires_at", "approval is expired at the evaluation time")
	}
	matched := make([]bool, len(fields.Fields))
	for i, treatment := range a.Treatments {
		prefix := fmt.Sprintf("treatments[%d]", i)
		for _, fieldValue := range []struct{ name, value string }{
			{"system", treatment.System}, {"field_path", treatment.FieldPath}, {"region", treatment.Region},
			{"purpose", treatment.Purpose}, {"processor", treatment.Processor}, {"retention", treatment.Retention},
			{"classification", treatment.Classification},
		} {
			if strings.TrimSpace(fieldValue.value) == "" {
				add("UNCLASSIFIED_FLOW", prefix+"."+fieldValue.name, "missing approved treatment value")
			}
		}
		matches := 0
		for fieldIndex, field := range fields.Fields {
			if treatment.System == field.Source && treatment.FieldPath == field.Path && treatment.Purpose == field.Purpose && treatment.Classification == field.Classification {
				matched[fieldIndex] = true
				matches++
			}
		}
		if matches == 0 {
			add("UNBOUND_FLOW", fmt.Sprintf("treatments[%d]", i), "approval contains a treatment outside the exact field manifest")
		}
	}
	for fieldIndex, isMatched := range matched {
		if !isMatched {
			add("UNCLASSIFIED_FLOW", fmt.Sprintf("fields[%d]", fieldIndex), "an exact field path has no approved system/region/purpose/processor/retention treatment")
		}
	}
	return out
}

// IncumbentAssessment captures the decision-shaped native-capability inputs
// without pretending a vendor, edition, license, or topology was selected.
type IncumbentAssessment struct {
	System             string `json:"system" yaml:"system"`
	Edition            string `json:"edition" yaml:"edition"`
	Version            string `json:"version" yaml:"version"`
	License            string `json:"license" yaml:"license"`
	ConfiguredWorkflow string `json:"configured_workflow" yaml:"configured_workflow"`
	NativeCoverage     string `json:"native_coverage" yaml:"native_coverage"`
	Gap                string `json:"gap" yaml:"gap"`
	EvidenceDate       string `json:"evidence_date" yaml:"evidence_date"`
}

type CommercialTerms struct {
	PriceMinor                int64  `json:"price_minor" yaml:"price_minor"`
	Currency                  string `json:"currency" yaml:"currency"`
	BillingBasis              string `json:"billing_basis" yaml:"billing_basis"`
	EligibleVolume            int64  `json:"eligible_volume" yaml:"eligible_volume"`
	AdoptionDenominator       int64  `json:"adoption_denominator" yaml:"adoption_denominator"`
	BypassSources             string `json:"bypass_sources" yaml:"bypass_sources"`
	CustomerLaborByRole       string `json:"customer_labor_by_role" yaml:"customer_labor_by_role"`
	CostToServeMinor          int64  `json:"cost_to_serve_minor" yaml:"cost_to_serve_minor"`
	ValuesPendingHumanSignoff bool   `json:"values_pending_human_signoff" yaml:"values_pending_human_signoff"`
}

type StopCriterion struct {
	Metric    string  `json:"metric" yaml:"metric"`
	Operator  string  `json:"operator" yaml:"operator"`
	Threshold float64 `json:"threshold" yaml:"threshold"`
	Unit      string  `json:"unit" yaml:"unit"`
}

// PartnerManifest is the typed WEDGE-001 selection record and the shared
// input for the downstream Gate A contracts. Human selections may remain
// placeholders until a real paid partner is approved.
type PartnerManifest struct {
	SchemaVersion             int                 `json:"schema_version" yaml:"schema_version"`
	ManifestID                string              `json:"manifest_id" yaml:"manifest_id"`
	ProblemClass              string              `json:"problem_class" yaml:"problem_class"`
	Incumbent                 IncumbentAssessment `json:"incumbent" yaml:"incumbent"`
	Topology                  DownstreamTopology  `json:"topology" yaml:"topology"`
	Fields                    FieldManifest       `json:"fields" yaml:"fields"`
	NativeCapability          IncumbentAssessment `json:"native_capability" yaml:"native_capability"`
	Commercial                CommercialTerms     `json:"commercial" yaml:"commercial"`
	StopCriteria              []StopCriterion     `json:"stop_criteria" yaml:"stop_criteria"`
	SignedBy                  string              `json:"signed_by" yaml:"signed_by"`
	SignedAt                  string              `json:"signed_at" yaml:"signed_at"`
	SignatureDigest           string              `json:"signature_digest" yaml:"signature_digest"`
	ValuesPendingHumanSignoff bool                `json:"values_pending_human_signoff" yaml:"values_pending_human_signoff"`
}

func (m PartnerManifest) Digest() string { return digestValue(m) }

// PlaceholderPartnerManifest is a complete schema fixture whose external
// selections are intentionally unresolved. It is useful for contract tests
// and planning review; QualifyPartner returns UNDECIDED for it.
func PlaceholderPartnerManifest() PartnerManifest {
	topology := DownstreamTopology{
		SchemaVersion: 1, SourceSystem: "PLACEHOLDER_INCUMBENT_SYSTEM", SourceAuthority: "PLACEHOLDER_SOURCE_AUTHORITY",
		DownstreamSystem: "PLACEHOLDER_DOWNSTREAM_SYSTEM", DownstreamAuthority: "PLACEHOLDER_DOWNSTREAM_AUTHORITY",
		HandoffRef: "PLACEHOLDER_HANDOFF", AcknowledgementMechanism: "PLACEHOLDER_ACK", ObservationMechanism: "PLACEHOLDER_OBSERVATION",
		AccountableOwner: "PLACEHOLDER_ACCOUNTABLE_OWNER",
	}
	fields := FieldManifest{SchemaVersion: 1, ManifestID: "PLACEHOLDER_FIELD_MANIFEST", TopologyDigest: topology.Digest(), GateAReadOnly: true, Fields: []FieldSpec{{
		Path: "PLACEHOLDER_FIELD_PATH", Authority: "PLACEHOLDER_FIELD_AUTHORITY", Classification: "PLACEHOLDER_CLASSIFICATION", Purpose: "PLACEHOLDER_PURPOSE", Source: topology.SourceSystem, EffectiveTime: "PLACEHOLDER_EFFECTIVE_TIME", PhaseDepth: "GATE_A", Access: "READ",
	}}}
	assessment := IncumbentAssessment{System: topology.SourceSystem, Edition: "PLACEHOLDER_EDITION", Version: "PLACEHOLDER_VERSION", License: "PLACEHOLDER_LICENSE", ConfiguredWorkflow: "PLACEHOLDER_WORKFLOW", NativeCoverage: "PLACEHOLDER_NATIVE_COVERAGE", Gap: "PLACEHOLDER_GAP", EvidenceDate: "2026-09-06"}
	return PartnerManifest{
		SchemaVersion: 1, ManifestID: "PLACEHOLDER_PARTNER_MANIFEST", ProblemClass: "PLACEHOLDER_FAILURE_CLASS", Incumbent: assessment, Topology: topology, Fields: fields, NativeCapability: assessment,
		Commercial:   CommercialTerms{Currency: "PLACEHOLDER_CURRENCY", BillingBasis: "PLACEHOLDER_BILLING_BASIS", ValuesPendingHumanSignoff: true},
		StopCriteria: []StopCriterion{{Metric: "PLACEHOLDER_METRIC", Operator: "PLACEHOLDER_OPERATOR", Unit: "PLACEHOLDER_UNIT"}},
		SignedBy:     "PLACEHOLDER_SIGNER", SignedAt: "2026-09-06", SignatureDigest: "sha256:PLACEHOLDER_SIGNATURE", ValuesPendingHumanSignoff: true,
	}
}

func ValidatePartnerManifest(m PartnerManifest) []ContractViolation {
	var out []ContractViolation
	if m.SchemaVersion == 0 || strings.TrimSpace(m.ManifestID) == "" || strings.TrimSpace(m.ProblemClass) == "" {
		out = append(out, violation("PARTNER_MANIFEST_INCOMPLETE", "identity", "schema version, manifest id, and problem class are required"))
	}
	out = append(out, ValidateTopology(m.Topology)...)
	out = append(out, ValidateFieldManifest(m.Fields, m.Topology)...)
	out = append(out, validateIncumbentAssessment("incumbent", m.Incumbent)...)
	out = append(out, validateIncumbentAssessment("native_capability", m.NativeCapability)...)
	for _, fieldValue := range []struct{ field, value string }{
		{"signed_by", m.SignedBy}, {"signed_at", m.SignedAt}, {"signature_digest", m.SignatureDigest},
	} {
		if strings.TrimSpace(fieldValue.value) == "" {
			out = append(out, violation("PARTNER_MANIFEST_INCOMPLETE", fieldValue.field, "missing"))
		}
	}
	if m.Commercial.AdoptionDenominator <= 0 && !m.ValuesPendingHumanSignoff {
		out = append(out, violation("COMMERCIAL_INCOMPLETE", "commercial.adoption_denominator", "must be positive"))
	}
	if m.Commercial.EligibleVolume <= 0 && !m.ValuesPendingHumanSignoff {
		out = append(out, violation("COMMERCIAL_INCOMPLETE", "commercial.eligible_volume", "must be positive"))
	}
	if len(m.StopCriteria) == 0 {
		out = append(out, violation("STOP_CRITERIA_MISSING", "stop_criteria", "at least one numeric stop criterion is required"))
	}
	for i, criterion := range m.StopCriteria {
		if strings.TrimSpace(criterion.Metric) == "" || strings.TrimSpace(criterion.Operator) == "" || strings.TrimSpace(criterion.Unit) == "" {
			out = append(out, violation("STOP_CRITERIA_INCOMPLETE", fmt.Sprintf("stop_criteria[%d]", i), "metric, operator, threshold, and unit are required"))
		}
	}
	return out
}

func validateIncumbentAssessment(prefix string, assessment IncumbentAssessment) []ContractViolation {
	var out []ContractViolation
	for _, fieldValue := range []struct{ field, value string }{
		{"system", assessment.System}, {"edition", assessment.Edition}, {"version", assessment.Version},
		{"license", assessment.License}, {"configured_workflow", assessment.ConfiguredWorkflow},
		{"native_coverage", assessment.NativeCoverage}, {"gap", assessment.Gap}, {"evidence_date", assessment.EvidenceDate},
	} {
		if strings.TrimSpace(fieldValue.value) == "" {
			out = append(out, violation("NATIVE_CAPABILITY_INCOMPLETE", prefix+"."+fieldValue.field, "missing"))
		}
	}
	if assessment.EvidenceDate != "" {
		if _, err := time.Parse(dateLayout, assessment.EvidenceDate); err != nil {
			out = append(out, violation("NATIVE_CAPABILITY_DATE_INVALID", prefix+".evidence_date", "must be YYYY-MM-DD"))
		}
	}
	return out
}

type PartnerQualificationStatus string

const (
	PartnerQualified PartnerQualificationStatus = "QUALIFIED"
	PartnerUndecided PartnerQualificationStatus = "UNDECIDED"
	PartnerReselect  PartnerQualificationStatus = "RESELECT"
	PartnerStop      PartnerQualificationStatus = "STOP"
)

type PartnerQualification struct {
	Status         PartnerQualificationStatus `json:"status"`
	ManifestDigest string                     `json:"manifest_digest"`
	Reasons        []string                   `json:"reasons,omitempty"`
}

// QualifyPartner is deliberately an evaluator. It cannot select a real
// partner; placeholder-bearing records remain UNDECIDED until a human signs
// the concrete values.
func QualifyPartner(m PartnerManifest) PartnerQualification {
	result := PartnerQualification{ManifestDigest: m.Digest()}
	if violations := ValidatePartnerManifest(m); len(violations) != 0 {
		result.Status = PartnerReselect
		for _, v := range violations {
			result.Reasons = append(result.Reasons, v.String())
		}
		return result
	}
	if m.ValuesPendingHumanSignoff || placeholder(m.Incumbent.System) || placeholder(m.Topology.SourceSystem) || placeholder(m.Commercial.Currency) {
		result.Status = PartnerUndecided
		result.Reasons = []string{"human must supply the partner, topology, commercial, licensing, and stop-threshold decisions"}
		return result
	}
	result.Status = PartnerQualified
	return result
}

// FixtureKind is the closed WEDGE-011 boundary-case vocabulary.
type FixtureKind string

const (
	FixtureFutureEffective    FixtureKind = "FUTURE_EFFECTIVE"
	FixtureRetroactive        FixtureKind = "RETROACTIVE"
	FixtureNullRedacted       FixtureKind = "NULL_REDACTED"
	FixtureMultiEmployment    FixtureKind = "MULTI_EMPLOYMENT"
	FixtureStale              FixtureKind = "STALE"
	FixtureConflict           FixtureKind = "CONFLICT"
	FixturePartialObservation FixtureKind = "PARTIAL_OBSERVATION"
)

var RequiredFixtureKinds = []FixtureKind{
	FixtureFutureEffective, FixtureRetroactive, FixtureNullRedacted,
	FixtureMultiEmployment, FixtureStale, FixtureConflict, FixturePartialObservation,
}

type FixtureFact struct {
	Field    string `json:"field" yaml:"field"`
	Value    string `json:"value" yaml:"value"`
	Redacted bool   `json:"redacted" yaml:"redacted"`
}

type PartnerFixture struct {
	ID                   string        `json:"id" yaml:"id"`
	Kind                 FixtureKind   `json:"kind" yaml:"kind"`
	Provenance           string        `json:"provenance" yaml:"provenance"`
	ExpectedResultDigest string        `json:"expected_result_digest" yaml:"expected_result_digest"`
	Facts                []FixtureFact `json:"facts" yaml:"facts"`
	Synthetic            bool          `json:"synthetic" yaml:"synthetic"`
	ContainsPII          bool          `json:"contains_pii" yaml:"contains_pii"`
}

type FixtureSet struct {
	SchemaVersion  int              `json:"schema_version" yaml:"schema_version"`
	SetID          string           `json:"set_id" yaml:"set_id"`
	Provenance     string           `json:"provenance" yaml:"provenance"`
	ManifestDigest string           `json:"manifest_digest" yaml:"manifest_digest"`
	Fixtures       []PartnerFixture `json:"fixtures" yaml:"fixtures"`
}

func (s FixtureSet) Digest() string { return digestValue(s) }

func ValidateFixtureSet(s FixtureSet) []ContractViolation {
	var out []ContractViolation
	if s.SchemaVersion == 0 || strings.TrimSpace(s.SetID) == "" || strings.TrimSpace(s.Provenance) == "" {
		out = append(out, violation("FIXTURE_SET_INCOMPLETE", "identity", "schema version, set id, and provenance are required"))
	}
	seen := make(map[string]bool, len(s.Fixtures))
	for i, f := range s.Fixtures {
		prefix := fmt.Sprintf("fixtures[%d]", i)
		if strings.TrimSpace(f.ID) == "" || f.Kind == "" || strings.TrimSpace(f.Provenance) == "" || strings.TrimSpace(f.ExpectedResultDigest) == "" {
			out = append(out, violation("FIXTURE_INCOMPLETE", prefix, "id, kind, provenance, and expected-result digest are required"))
		}
		knownKind := false
		for _, requiredKind := range RequiredFixtureKinds {
			if f.Kind == requiredKind {
				knownKind = true
				break
			}
		}
		if !knownKind {
			out = append(out, violation("FIXTURE_KIND_UNKNOWN", prefix+".kind", "fixture kind is outside the required boundary-case vocabulary"))
		}
		if seen[f.ID] {
			out = append(out, violation("FIXTURE_DUPLICATE", prefix+".id", "fixture id is repeated"))
		}
		seen[f.ID] = true
		if !f.Synthetic {
			out = append(out, violation("FIXTURE_PII_RISK", prefix+".synthetic", "Gate A fixture must be synthetic or de-identified"))
		}
		if f.ContainsPII {
			out = append(out, violation("FIXTURE_PII_RISK", prefix+".contains_pii", "PII is not permitted in the fixture corpus"))
		}
	}
	return out
}

type FixtureCoverage struct {
	SetDigest string        `json:"set_digest"`
	Present   []FixtureKind `json:"present"`
	Missing   []FixtureKind `json:"missing"`
}

func EvaluateFixtureCoverage(s FixtureSet) FixtureCoverage {
	present := make(map[FixtureKind]bool, len(s.Fixtures))
	for _, f := range s.Fixtures {
		present[f.Kind] = true
	}
	coverage := FixtureCoverage{SetDigest: s.Digest()}
	for _, kind := range RequiredFixtureKinds {
		if present[kind] {
			coverage.Present = append(coverage.Present, kind)
		} else {
			coverage.Missing = append(coverage.Missing, kind)
		}
	}
	return coverage
}

// SimulationItem is a declared read, planned effect, approval, obligation,
// or result. A planned write is descriptive only in P1A.
type SimulationItem struct {
	ID         string `json:"id"`
	Summary    string `json:"summary"`
	Expected   string `json:"expected"`
	EffectMode string `json:"effect_mode,omitempty"`
}

// WorkflowSimulationContract is the complete WEDGE-012 acceptance story.
// Explicit sections prevent a caller from hiding material simulation facts in
// an untyped blob or omitting them from the canonical digest.
type WorkflowSimulationContract struct {
	SchemaVersion          int              `json:"schema_version"`
	WorkflowRef            string           `json:"workflow_ref"`
	ScenarioRef            string           `json:"scenario_ref"`
	PolicyDigest           string           `json:"policy_digest"`
	Reads                  []SimulationItem `json:"reads"`
	PlannedWrites          []SimulationItem `json:"planned_writes"`
	Approvals              []SimulationItem `json:"approvals"`
	Conflicts              []SimulationItem `json:"conflicts"`
	Authority              []SimulationItem `json:"authority"`
	Obligations            []SimulationItem `json:"obligations"`
	Costs                  []SimulationItem `json:"costs"`
	SideEffects            []SimulationItem `json:"side_effects"`
	Completion             []SimulationItem `json:"completion"`
	Revalidation           []SimulationItem `json:"revalidation"`
	Repair                 []SimulationItem `json:"repair"`
	ActualEffects          []SimulationItem `json:"actual_effects,omitempty"`
	AuthoritativeMutations int              `json:"authoritative_mutations"`
	ProviderCalls          int              `json:"provider_calls"`
}

func (c WorkflowSimulationContract) Digest() string { return digestValue(c) }

func ValidateWorkflowSimulation(c WorkflowSimulationContract) []ContractViolation {
	var out []ContractViolation
	if c.SchemaVersion == 0 || strings.TrimSpace(c.WorkflowRef) == "" || strings.TrimSpace(c.ScenarioRef) == "" || strings.TrimSpace(c.PolicyDigest) == "" {
		out = append(out, violation("SIMULATION_INCOMPLETE", "identity", "schema version, workflow, scenario, and policy digest are required"))
	}
	sections := []struct {
		name  string
		items []SimulationItem
	}{
		{"reads", c.Reads}, {"planned_writes", c.PlannedWrites}, {"approvals", c.Approvals},
		{"conflicts", c.Conflicts}, {"authority", c.Authority}, {"obligations", c.Obligations},
		{"costs", c.Costs}, {"side_effects", c.SideEffects}, {"completion", c.Completion},
		{"revalidation", c.Revalidation}, {"repair", c.Repair},
	}
	for _, section := range sections {
		if len(section.items) == 0 {
			out = append(out, violation("SIMULATION_SECTION_MISSING", section.name, "required section is empty"))
			continue
		}
		for i, item := range section.items {
			if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Summary) == "" || strings.TrimSpace(item.Expected) == "" {
				out = append(out, violation("SIMULATION_ITEM_INCOMPLETE", fmt.Sprintf("%s[%d]", section.name, i), "id, summary, and expected result are required"))
			}
		}
	}
	if len(c.ActualEffects) != 0 || c.AuthoritativeMutations != 0 || c.ProviderCalls != 0 {
		out = append(out, violation("HIDDEN_EFFECT", "actual_effects", "P1A simulation must report zero actual authoritative and provider effects"))
	}
	for i, item := range c.PlannedWrites {
		if strings.ToUpper(item.EffectMode) != "PLANNED_NOT_EXECUTED" {
			out = append(out, violation("UNDECLARED_EFFECT_MODE", fmt.Sprintf("planned_writes[%d].effect_mode", i), "planned writes must be explicitly non-executing in P1A"))
		}
	}
	return out
}

// PromotionSimulationFixture returns a complete, reusable synthetic story.
// Customer policy parameters are represented by the policy digest and are not
// silently selected here.
func PromotionSimulationFixture() WorkflowSimulationContract {
	item := func(id, summary, expected string) SimulationItem {
		return SimulationItem{ID: id, Summary: summary, Expected: expected}
	}
	planned := func(id, summary, expected string) SimulationItem {
		return SimulationItem{ID: id, Summary: summary, Expected: expected, EffectMode: "PLANNED_NOT_EXECUTED"}
	}
	return WorkflowSimulationContract{
		SchemaVersion: 1, WorkflowRef: "hcmnext.people.promote_worker/v1", ScenarioRef: "fixture:promotion-simulation",
		PolicyDigest:  "sha256:PLACEHOLDER_CUSTOMER_POLICY_DIGEST",
		Reads:         []SimulationItem{item("read-people", "job, level, position, manager, organization, direct reports", "source and freshness are visible")},
		PlannedWrites: []SimulationItem{planned("write-people", "promotion change to job, level, position and manager", "proposal-only; no mutation")},
		Approvals:     []SimulationItem{item("approval", "approval graph and exact proposal binding", "approval requirement is explicit")},
		Conflicts:     []SimulationItem{item("conflict", "affected resources and effective-time overlap", "conflict result is blocking or clear")},
		Authority:     []SimulationItem{item("authority", "tenant, field, capability and time scope", "server-derived authority is visible")},
		Obligations:   []SimulationItem{item("obligation", "jurisdiction, notice and training obligations", "unknown obligations block completion")},
		Costs:         []SimulationItem{item("cost", "annualized delta, cost center and budget exposure", "cost result is visible")},
		SideEffects:   []SimulationItem{item("side-effect", "payroll, access, talent, learning, documents and messaging plans", "downstream effects are planned and observed, never executed")},
		Completion:    []SimulationItem{item("completion", "business, external consistency, reconciliation and incident dimensions", "each completion dimension is independent")},
		Revalidation:  []SimulationItem{item("revalidation", "authority, freshness, policy and conflict recheck", "stale or changed evidence blocks")},
		Repair:        []SimulationItem{item("repair", "intended-versus-observed diff and repair recommendation", "repair is non-executable in P1A")},
	}
}

type PaidUseActorClass string

const (
	PaidUseCustomerActor PaidUseActorClass = "CUSTOMER"
	PaidUseDemoActor     PaidUseActorClass = "DEMO"
	PaidUseInternalActor PaidUseActorClass = "INTERNAL"
)

// PaidUseEvent is the minimized WEDGE-013 event. ActorRefHash is used instead
// of a person name or raw customer identifier.
type PaidUseEvent struct {
	SchemaVersion       int               `json:"schema_version"`
	EventID             string            `json:"event_id"`
	TenantRefHash       string            `json:"tenant_ref_hash"`
	ActorRefHash        string            `json:"actor_ref_hash"`
	ActorClass          PaidUseActorClass `json:"actor_class"`
	CustomerAuthorized  bool              `json:"customer_authorized"`
	Licensed            bool              `json:"licensed"`
	EligibleTransaction bool              `json:"eligible_transaction"`
	TransactionRef      string            `json:"transaction_ref"`
	WorkflowStage       string            `json:"workflow_stage"`
	OutcomeRef          string            `json:"outcome_ref"`
	ObservedAt          string            `json:"observed_at"`
	SimulationDigest    string            `json:"simulation_digest"`
}

func ValidatePaidUseEvent(e PaidUseEvent) []ContractViolation {
	var out []ContractViolation
	if e.SchemaVersion == 0 || strings.TrimSpace(e.EventID) == "" {
		out = append(out, violation("PAID_USE_INCOMPLETE", "identity", "schema version and event id are required"))
	}
	if e.ActorClass != PaidUseCustomerActor || !e.CustomerAuthorized || !e.Licensed {
		out = append(out, violation("INELIGIBLE_ACTOR", "actor", "paid use requires an authorized licensed customer actor"))
	}
	if e.ActorClass == PaidUseDemoActor || e.ActorClass == PaidUseInternalActor {
		out = append(out, violation("INELIGIBLE_ACTOR", "actor_class", "demo and Human Capital Management Suite internal actors never count as paid use"))
	}
	for _, fieldValue := range []struct{ field, value string }{
		{"tenant_ref_hash", e.TenantRefHash}, {"actor_ref_hash", e.ActorRefHash}, {"transaction_ref", e.TransactionRef},
		{"workflow_stage", e.WorkflowStage}, {"outcome_ref", e.OutcomeRef}, {"observed_at", e.ObservedAt}, {"simulation_digest", e.SimulationDigest},
	} {
		if strings.TrimSpace(fieldValue.value) == "" {
			out = append(out, violation("PAID_USE_INCOMPLETE", fieldValue.field, "missing"))
		}
	}
	if len(e.ActorRefHash) != 64 {
		out = append(out, violation("ACTOR_DATA_NOT_MINIMIZED", "actor_ref_hash", "actor reference must be a SHA-256-sized hash"))
	}
	if !e.EligibleTransaction {
		out = append(out, violation("INELIGIBLE_TRANSACTION", "eligible_transaction", "transaction is not eligible for paid-use evidence"))
	}
	return out
}

type PaidUseEvidence struct {
	SchemaVersion    int            `json:"schema_version"`
	EvidenceID       string         `json:"evidence_id"`
	SimulationDigest string         `json:"simulation_digest"`
	Events           []PaidUseEvent `json:"events"`
}

func (e PaidUseEvidence) Digest() string { return digestValue(e) }

func ValidatePaidUseEvidence(e PaidUseEvidence) []ContractViolation {
	var out []ContractViolation
	if e.SchemaVersion == 0 || strings.TrimSpace(e.EvidenceID) == "" || strings.TrimSpace(e.SimulationDigest) == "" {
		out = append(out, violation("PAID_USE_EVIDENCE_INCOMPLETE", "identity", "schema version, evidence id, and simulation digest are required"))
	}
	if len(e.Events) == 0 {
		out = append(out, violation("PAID_USE_EVIDENCE_INCOMPLETE", "events", "at least one event is required"))
	}
	for i, event := range e.Events {
		for _, eventViolation := range ValidatePaidUseEvent(event) {
			eventViolation.Field = fmt.Sprintf("events[%d].%s", i, eventViolation.Field)
			out = append(out, eventViolation)
		}
		if event.SimulationDigest != e.SimulationDigest {
			out = append(out, violation("PAID_USE_SIMULATION_MISMATCH", fmt.Sprintf("events[%d].simulation_digest", i), "event is not linked to the evidence simulation"))
		}
	}
	return out
}

func (e PaidUseEvidence) EligibleEvents() []PaidUseEvent {
	var out []PaidUseEvent
	for _, event := range e.Events {
		if len(ValidatePaidUseEvent(event)) == 0 {
			out = append(out, event)
		}
	}
	return out
}

// PilotExitPlan is the WEDGE-009 portability/exit record.
type PilotExitPlan struct {
	SchemaVersion             int    `json:"schema_version"`
	TenantRefHash             string `json:"tenant_ref_hash"`
	CredentialRevocation      string `json:"credential_revocation"`
	PendingWorkDisposition    string `json:"pending_work_disposition"`
	EvidenceExport            string `json:"evidence_export"`
	RetentionAndHolds         string `json:"retention_and_holds"`
	DestructionResponsibility string `json:"destruction_responsibility"`
	ConnectorAuthority        string `json:"connector_authority"`
}

func ValidatePilotExitPlan(p PilotExitPlan) []ContractViolation {
	var out []ContractViolation
	for _, fieldValue := range []struct{ field, value string }{
		{"tenant_ref_hash", p.TenantRefHash}, {"credential_revocation", p.CredentialRevocation},
		{"pending_work_disposition", p.PendingWorkDisposition}, {"evidence_export", p.EvidenceExport},
		{"retention_and_holds", p.RetentionAndHolds}, {"destruction_responsibility", p.DestructionResponsibility},
		{"connector_authority", p.ConnectorAuthority},
	} {
		if strings.TrimSpace(fieldValue.value) == "" {
			out = append(out, violation("EXIT_PLAN_INCOMPLETE", fieldValue.field, "missing"))
		}
	}
	if !strings.Contains(strings.ToLower(p.ConnectorAuthority), "none") && !strings.Contains(strings.ToLower(p.ConnectorAuthority), "revok") {
		out = append(out, violation("EXIT_AUTHORITY_ACTIVE", "connector_authority", "exit must revoke or leave no active connector authority"))
	}
	return out
}

type PilotExitResult struct {
	ExportDigest             string `json:"export_digest"`
	ActiveConnectorAuthority int    `json:"active_connector_authority"`
	RevocationRecorded       bool   `json:"revocation_recorded"`
}

func DryRunPilotExit(p PilotExitPlan, evidenceDigest string) (PilotExitResult, []ContractViolation) {
	if violations := ValidatePilotExitPlan(p); len(violations) != 0 {
		return PilotExitResult{}, violations
	}
	return PilotExitResult{ExportDigest: digestValue(struct {
		Tenant, Evidence string
	}{p.TenantRefHash, evidenceDigest}), ActiveConnectorAuthority: 0, RevocationRecorded: true}, nil
}

// P1AEvidenceRequirement extends the signed P1A manifest's exact test tuple
// with the metadata NEXT-003 requires for a truthful Gate A receipt.
type P1AEvidenceRequirement struct {
	TodoID                string `json:"todo_id"`
	Test                  string `json:"test"`
	Package               string `json:"package"`
	Owner                 string `json:"owner"`
	Command               string `json:"command"`
	FixtureArtifact       string `json:"fixture_artifact"`
	RestoreArtifact       string `json:"restore_artifact"`
	SLOArtifact           string `json:"slo_artifact"`
	AccessibilityArtifact string `json:"accessibility_artifact"`
	PrivacyArtifact       string `json:"privacy_artifact"`
	RetentionDays         int    `json:"retention_days"`
	ExpiresAt             string `json:"expires_at"`
}

type P1AEvidenceResult struct {
	TodoID                 string `json:"todo_id"`
	Test                   string `json:"test"`
	Package                string `json:"package"`
	ManifestDigest         string `json:"manifest_digest"`
	Status                 string `json:"status"`
	ResultDigest           string `json:"result_digest"`
	Timestamp              string `json:"timestamp"`
	Owner                  string `json:"owner"`
	Command                string `json:"command"`
	FixtureArtifact        string `json:"fixture_artifact"`
	RestoreArtifact        string `json:"restore_artifact"`
	SLOArtifact            string `json:"slo_artifact"`
	AccessibilityArtifact  string `json:"accessibility_artifact"`
	PrivacyArtifact        string `json:"privacy_artifact"`
	RetentionDays          int    `json:"retention_days"`
	ExpiresAt              string `json:"expires_at"`
	RestoreVerified        bool   `json:"restore_verified"`
	Waived                 bool   `json:"waived"`
	AuthoritativeMutations int    `json:"authoritative_mutations"`
	WorkerMutations        int    `json:"worker_mutations"`
	ProviderEffects        int    `json:"provider_effects"`
	MessageIntents         int    `json:"message_intents"`
}

type P1AEvidenceFinding struct {
	TodoID string `json:"todo_id"`
	Test   string `json:"test"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type P1AEvidenceReceipt struct {
	SchemaVersion  int                  `json:"schema_version"`
	ManifestTodoID string               `json:"manifest_todo_id"`
	ManifestDigest string               `json:"manifest_digest"`
	AsOf           string               `json:"as_of"`
	Decision       GateDecision         `json:"decision"`
	Findings       []P1AEvidenceFinding `json:"findings"`
}

func (r P1AEvidenceReceipt) Digest() string { return digestValue(r) }

func CompileP1AEvidence(manifest P1AManifest, requirements []P1AEvidenceRequirement, results []P1AEvidenceResult, now time.Time) P1AEvidenceReceipt {
	digest, _ := manifest.CanonicalDigest()
	receipt := P1AEvidenceReceipt{SchemaVersion: GateAContractVersion, ManifestTodoID: manifest.TodoID, ManifestDigest: digest, AsOf: now.UTC().Format(dateLayout), Decision: GateDecisionClear}
	add := func(todo, test, code, detail string) {
		receipt.Findings = append(receipt.Findings, P1AEvidenceFinding{TodoID: todo, Test: test, Code: code, Detail: detail})
		receipt.Decision = GateDecisionBlocked
	}
	if ok, err := VerifyManifestSignature(manifest); err != nil || !ok {
		add(manifest.TodoID, "", "MANIFEST_NOT_SIGNED", "exact P1A manifest signature is missing or invalid")
	}
	requirementByKey := make(map[string]P1AEvidenceRequirement, len(requirements))
	resultByKey := make(map[string]P1AEvidenceResult, len(results))
	key := func(todo, test, pkg string) string { return strings.Join([]string{todo, test, pkg}, "\x00") }
	for _, requirement := range requirements {
		requirementByKey[key(requirement.TodoID, requirement.Test, requirement.Package)] = requirement
	}
	for _, result := range results {
		resultByKey[key(result.TodoID, result.Test, result.Package)] = result
	}
	manifestKeys := make(map[string]bool, len(manifest.Evidence))
	for _, entry := range manifest.Evidence {
		entryKey := key(entry.TodoID, entry.Test, entry.Package)
		manifestKeys[entryKey] = true
		requirement, ok := requirementByKey[entryKey]
		if !ok {
			add(entry.TodoID, entry.Test, "MISSING_REQUIREMENT", "selected manifest evidence has no owner, command, fixture, restore, SLO, accessibility, privacy, retention, and expiry record")
			continue
		}
		for _, fieldValue := range []struct{ field, value string }{{"owner", requirement.Owner}, {"command", requirement.Command}, {"fixture_artifact", requirement.FixtureArtifact}, {"restore_artifact", requirement.RestoreArtifact}, {"slo_artifact", requirement.SLOArtifact}, {"accessibility_artifact", requirement.AccessibilityArtifact}, {"privacy_artifact", requirement.PrivacyArtifact}, {"expires_at", requirement.ExpiresAt}} {
			if strings.TrimSpace(fieldValue.value) == "" {
				add(entry.TodoID, entry.Test, "MISSING_"+strings.ToUpper(fieldValue.field), "required evidence metadata is empty")
			}
		}
		if requirement.RetentionDays <= 0 {
			add(entry.TodoID, entry.Test, "MISSING_RETENTION", "retention must be positive")
		}
		if expiry, err := time.Parse(dateLayout, requirement.ExpiresAt); err != nil {
			add(entry.TodoID, entry.Test, "EXPIRY_INVALID", "requirement expiry is not YYYY-MM-DD")
		} else if !now.Before(expiry.Add(24 * time.Hour)) {
			add(entry.TodoID, entry.Test, "EVIDENCE_EXPIRED", "requirement expiry has passed")
		}
		result, ok := resultByKey[entryKey]
		if !ok {
			add(entry.TodoID, entry.Test, "MISSING_RESULT", "no result exists for the exact manifest tuple")
			continue
		}
		if result.ManifestDigest != digest || result.Status != "PASS" || result.Waived {
			add(entry.TodoID, entry.Test, "RESULT_NOT_TRUTHFUL", "result is not a non-waived PASS bound to the exact manifest digest")
		}
		if result.ResultDigest == "" || result.Timestamp == "" {
			add(entry.TodoID, entry.Test, "RESULT_INCOMPLETE", "result digest and timestamp are required")
		}
		if ts, err := time.Parse(dateLayout, result.Timestamp); err != nil {
			add(entry.TodoID, entry.Test, "RESULT_TIMESTAMP_INVALID", "result timestamp is not YYYY-MM-DD")
		} else if now.Sub(ts) > time.Duration(manifest.FreshnessWindowDays)*24*time.Hour {
			add(entry.TodoID, entry.Test, "RESULT_STALE", "result exceeds the signed manifest freshness window")
		}
		for _, metadata := range [][3]string{{"owner", requirement.Owner, result.Owner}, {"command", requirement.Command, result.Command}, {"fixture_artifact", requirement.FixtureArtifact, result.FixtureArtifact}, {"restore_artifact", requirement.RestoreArtifact, result.RestoreArtifact}, {"slo_artifact", requirement.SLOArtifact, result.SLOArtifact}, {"accessibility_artifact", requirement.AccessibilityArtifact, result.AccessibilityArtifact}, {"privacy_artifact", requirement.PrivacyArtifact, result.PrivacyArtifact}} {
			field, want, got := metadata[0], metadata[1], metadata[2]
			if want != got {
				add(entry.TodoID, entry.Test, "RESULT_METADATA_MISMATCH", field+" does not match the requirement")
			}
		}
		if result.RetentionDays != requirement.RetentionDays || result.ExpiresAt != requirement.ExpiresAt {
			add(entry.TodoID, entry.Test, "RESULT_METADATA_MISMATCH", "retention or expiry does not match the requirement")
		}
		if !result.RestoreVerified || result.AuthoritativeMutations != 0 || result.WorkerMutations != 0 || result.ProviderEffects != 0 || result.MessageIntents != 0 {
			add(entry.TodoID, entry.Test, "EFFECTFUL_OR_UNRESTORED", "restore must be verified and workforce/provider/message effects must be exactly zero")
		}
	}
	for _, requirement := range requirements {
		if !manifestKeys[key(requirement.TodoID, requirement.Test, requirement.Package)] {
			add(requirement.TodoID, requirement.Test, "OUT_OF_MANIFEST", "requirement is not selected by the exact signed P1A manifest")
		}
	}
	for _, result := range results {
		if !manifestKeys[key(result.TodoID, result.Test, result.Package)] {
			add(result.TodoID, result.Test, "OUT_OF_MANIFEST", "result is not selected by the exact signed P1A manifest")
		}
	}
	return receipt
}

type GateAOutcome string

const (
	GateAProceed   GateAOutcome = "PROCEED"
	GateARemediate GateAOutcome = "REMEDIATE"
	GateANarrow    GateAOutcome = "NARROW"
	GateAReselect  GateAOutcome = "RESELECT"
	GateAStop      GateAOutcome = "STOP"
)

// GateADecisionRecord is the immutable decision-shaped WEDGE-014 record.
// WriteAuthority is permanently false for this contract, including for a
// PROCEED outcome.
type GateADecisionRecord struct {
	SchemaVersion   int          `json:"schema_version"`
	Gate            string       `json:"gate"`
	Decision        GateAOutcome `json:"decision"`
	EvidenceDigest  string       `json:"evidence_digest"`
	SnapshotDigest  string       `json:"snapshot_digest"`
	Signer          string       `json:"signer"`
	SignedAt        string       `json:"signed_at"`
	WriteAuthority  bool         `json:"write_authority"`
	Signature       *Signature   `json:"signature,omitempty"`
	MissingEvidence []string     `json:"missing_evidence,omitempty"`
}

func (d GateADecisionRecord) unsignedDigest() string {
	copy := d
	copy.Signature = nil
	return digestValue(copy)
}

func (d GateADecisionRecord) Digest() string { return d.unsignedDigest() }

func EvaluateGateADecision(receipt P1AEvidenceReceipt) GateADecisionRecord {
	decision := GateAProceed
	if receipt.Decision != GateDecisionClear {
		decision = GateARemediate
	}
	record := GateADecisionRecord{SchemaVersion: GateAContractVersion, Gate: "GATE_A", Decision: decision, EvidenceDigest: receipt.Digest(), SnapshotDigest: receipt.Digest(), WriteAuthority: false}
	for _, finding := range receipt.Findings {
		record.MissingEvidence = append(record.MissingEvidence, finding.Code+":"+finding.Test)
	}
	sort.Strings(record.MissingEvidence)
	return record
}

func ValidateGateADecision(d GateADecisionRecord) []ContractViolation {
	var out []ContractViolation
	if d.SchemaVersion == 0 || d.Gate != "GATE_A" {
		out = append(out, violation("DECISION_INCOMPLETE", "gate", "decision must be a Gate A record"))
	}
	switch d.Decision {
	case GateAProceed, GateARemediate, GateANarrow, GateAReselect, GateAStop:
	default:
		out = append(out, violation("DECISION_INVALID", "decision", "decision is outside PROCEED, REMEDIATE, NARROW, RESELECT, STOP"))
	}
	for _, fieldValue := range []struct{ field, value string }{{"evidence_digest", d.EvidenceDigest}, {"snapshot_digest", d.SnapshotDigest}, {"signer", d.Signer}, {"signed_at", d.SignedAt}} {
		if strings.TrimSpace(fieldValue.value) == "" {
			out = append(out, violation("DECISION_INCOMPLETE", fieldValue.field, "missing"))
		}
	}
	if d.WriteAuthority {
		out = append(out, violation("WRITE_AUTHORITY_FORBIDDEN", "write_authority", "Gate A never grants write authority"))
	}
	if d.Signature == nil {
		out = append(out, violation("DECISION_UNSIGNED", "signature", "decision must carry a signature when recorded"))
	}
	return out
}

// SignGateADecision signs a decision record without changing its authority
// scope. The caller supplies the human-selected outcome and signer.
func SignGateADecision(d GateADecisionRecord, signer, signedAt string, priv ed25519.PrivateKey) (GateADecisionRecord, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return GateADecisionRecord{}, fmt.Errorf("private key has %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	d.Signer, d.SignedAt, d.WriteAuthority = signer, signedAt, false
	digest := d.unsignedDigest()
	signed, err := SignDigest(priv, digest)
	if err != nil {
		return GateADecisionRecord{}, err
	}
	d.Signature = &Signature{Algorithm: "ed25519", PublicKey: hex.EncodeToString(priv.Public().(ed25519.PublicKey)), Value: signed}
	return d, nil
}

func VerifyGateADecision(d GateADecisionRecord) (bool, error) {
	if d.Signature == nil {
		return false, fmt.Errorf("decision has no signature")
	}
	if d.WriteAuthority {
		return false, fmt.Errorf("gate A decision cannot grant write authority")
	}
	ok, err := VerifyDigestSignature(d.Signature.PublicKey, d.unsignedDigest(), d.Signature.Value)
	if err != nil {
		return false, err
	}
	return ok, nil
}
