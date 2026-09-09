// Package dataclass owns the government-data overlay on TRUST-010's field
// registry. It is deliberately a policy kernel: records contain classifications,
// digests and opaque custody references, never plaintext secrets.
package dataclass

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

const schemaVersion = 1

// Version is the version of the government-data classification contract.
func Version() int { return schemaVersion }

// GovernmentDataClass is the closed government-data dimension attached to a
// field. NONE means that no government overlay is asserted.
type GovernmentDataClass string

const (
	GovernmentClassNone GovernmentDataClass = "NONE"
	GovernmentClassFTI  GovernmentDataClass = "FTI"
	GovernmentClassCJI  GovernmentDataClass = "CJI"
	GovernmentClassCUI  GovernmentDataClass = "CUI"
	GovernmentClassACA  GovernmentDataClass = "ACA"

	// Short aliases make policy tables read naturally while retaining the
	// explicit GovernmentClass-prefixed names for serialized contracts.
	ClassNone = GovernmentClassNone
	ClassFTI  = GovernmentClassFTI
	ClassCJI  = GovernmentClassCJI
	ClassCUI  = GovernmentClassCUI
	ClassACA  = GovernmentClassACA
	NONE      = GovernmentClassNone
	FTI       = GovernmentClassFTI
	CJI       = GovernmentClassCJI
	CUI       = GovernmentClassCUI
	ACA       = GovernmentClassACA
)

func (c GovernmentDataClass) Valid() bool {
	switch c {
	case GovernmentClassNone, GovernmentClassFTI, GovernmentClassCJI, GovernmentClassCUI, GovernmentClassACA:
		return true
	default:
		return false
	}
}

// String returns the stable wire token.
func (c GovernmentDataClass) String() string { return string(c) }

// DataClass is a concise alias for GovernmentDataClass.
type DataClass = GovernmentDataClass

// Sensitivity is the lower-environment threshold used by a field policy.
type Sensitivity string

const (
	SensitivityPublic           Sensitivity = "PUBLIC"
	SensitivityInternal         Sensitivity = "INTERNAL"
	SensitivityRestricted       Sensitivity = "RESTRICTED"
	SensitivityHighlyRestricted Sensitivity = "HIGHLY_RESTRICTED"
)

func (s Sensitivity) restricted() bool {
	return s == SensitivityRestricted || s == SensitivityHighlyRestricted
}

// HandlingRule is the required treatment before a restricted field is copied
// into a non-production environment.
type HandlingRule string

const (
	HandlingIrreversibleTokenize HandlingRule = "IRREVERSIBLE_TOKENIZE"
	HandlingDeterministicMask    HandlingRule = "DETERMINISTIC_MASK"
	HandlingExclude              HandlingRule = "EXCLUDE"
	HandlingUnchanged            HandlingRule = "UNCHANGED"

	IRREVERSIBLE_TOKENIZE = HandlingIrreversibleTokenize
	DETERMINISTIC_MASK    = HandlingDeterministicMask
	EXCLUDE               = HandlingExclude
)

func (r HandlingRule) Valid() bool {
	switch r {
	case HandlingIrreversibleTokenize, HandlingDeterministicMask, HandlingExclude, HandlingUnchanged:
		return true
	default:
		return false
	}
}

// The six behavior dimensions are separate types so callers cannot
// accidentally use a retention token as an access token.
type StorageBehavior string
type KeyBehavior string
type AccessBehavior string
type DisclosureBehavior string
type PersonnelBehavior string
type RetentionBehavior string

// ClassPolicy declares the behavior that is recorded with every field
// receipt. The values are policy tokens, not prose supplied by a caller.
type ClassPolicy struct {
	Storage    StorageBehavior    `json:"storage"`
	Key        KeyBehavior        `json:"key"`
	Access     AccessBehavior     `json:"access"`
	Disclosure DisclosureBehavior `json:"disclosure"`
	Personnel  PersonnelBehavior  `json:"personnel"`
	Retention  RetentionBehavior  `json:"retention"`
}

var classPolicies = map[GovernmentDataClass]ClassPolicy{
	GovernmentClassNone: {
		Storage: "TENANT_STANDARD", Key: "TENANT_STANDARD", Access: "FIELD_AUTHZ",
		Disclosure: "PURPOSE_BOUND", Personnel: "STANDARD_SCREENING", Retention: "TENANT_SCHEDULE",
	},
	GovernmentClassFTI: {
		Storage: "FTI_SEGMENT", Key: "CUSTODY_FTI", Access: "NEED_TO_KNOW_FTI",
		Disclosure: "PUB_1075_APPROVED", Personnel: "FTI_SCREENED", Retention: "FTI_SCHEDULE",
	},
	GovernmentClassCJI: {
		Storage: "CJI_SEGMENT", Key: "CUSTODY_CJI", Access: "CJIS_AUTHORIZED",
		Disclosure: "CJIS_AGENCY_APPROVED", Personnel: "CJIS_SCREENED", Retention: "CJIS_SCHEDULE",
	},
	GovernmentClassCUI: {
		Storage: "CUI_SEGMENT", Key: "CUSTODY_CUI", Access: "CUI_LEAST_PRIVILEGE",
		Disclosure: "CUI_CONTRACT_BOUND", Personnel: "CMMC_SCREENED", Retention: "CUI_SCHEDULE",
	},
	GovernmentClassACA: {
		Storage: "ACA_SEGMENT", Key: "CUSTODY_ACA", Access: "ACA_MINIMUM_NECESSARY",
		Disclosure: "ACA_DUA_BOUND", Personnel: "ACA_TRAINED", Retention: "ACA_SCHEDULE",
	},
}

// ClassPolicyFor returns a copy of the closed class policy table.
func ClassPolicyFor(classification GovernmentDataClass) (ClassPolicy, error) {
	policy, ok := classPolicies[classification]
	if !ok {
		return ClassPolicy{}, refusal("government_class", ErrInvalidClassification, "unknown government-data class")
	}
	return policy, nil
}

// FieldPolicy is the complete government overlay for one authz registry
// field. Field is an authz policy token; it is not a Go struct field name.
type FieldPolicy struct {
	Field            authz.FieldID       `json:"field"`
	Domain           authz.DataDomain    `json:"domain"`
	GovernmentClass  GovernmentDataClass `json:"government_class"`
	Classification   GovernmentDataClass `json:"classification"`
	Sensitivity      Sensitivity         `json:"sensitivity"`
	Controls         ClassPolicy         `json:"controls"`
	LowerEnvironment HandlingRule        `json:"lower_environment"`
}

// fieldPolicies is private and authoritative. ResolveField additionally
// checks authz.FieldRegistry so a missing or drifted TRUST-010 field fails
// closed rather than acquiring an implicit classification.
var fieldPolicies = map[authz.FieldID]FieldPolicy{
	authz.FieldWorkerNumber: {
		Field: authz.FieldWorkerNumber, Domain: authz.DomainCore, GovernmentClass: GovernmentClassCUI, Classification: GovernmentClassCUI,
		Sensitivity: SensitivityRestricted, LowerEnvironment: HandlingIrreversibleTokenize,
	},
	authz.FieldJobTitle: {
		Field: authz.FieldJobTitle, Domain: authz.DomainCore, GovernmentClass: GovernmentClassNone, Classification: GovernmentClassNone,
		Sensitivity: SensitivityInternal, LowerEnvironment: HandlingUnchanged,
	},
	authz.FieldWorkEmail: {
		Field: authz.FieldWorkEmail, Domain: authz.DomainContact, GovernmentClass: GovernmentClassNone, Classification: GovernmentClassNone,
		Sensitivity: SensitivityInternal, LowerEnvironment: HandlingUnchanged,
	},
	authz.FieldHomeAddress: {
		Field: authz.FieldHomeAddress, Domain: authz.DomainContact, GovernmentClass: GovernmentClassNone, Classification: GovernmentClassNone,
		Sensitivity: SensitivityRestricted, LowerEnvironment: HandlingDeterministicMask,
	},
	authz.FieldBaseSalary: {
		Field: authz.FieldBaseSalary, Domain: authz.DomainCompensation, GovernmentClass: GovernmentClassNone, Classification: GovernmentClassNone,
		Sensitivity: SensitivityRestricted, LowerEnvironment: HandlingDeterministicMask,
	},
	authz.FieldBonusTarget: {
		Field: authz.FieldBonusTarget, Domain: authz.DomainCompensation, GovernmentClass: GovernmentClassNone, Classification: GovernmentClassNone,
		Sensitivity: SensitivityRestricted, LowerEnvironment: HandlingDeterministicMask,
	},
	authz.FieldTaxID: {
		Field: authz.FieldTaxID, Domain: authz.DomainTax, GovernmentClass: GovernmentClassFTI, Classification: GovernmentClassFTI,
		Sensitivity: SensitivityHighlyRestricted, LowerEnvironment: HandlingExclude,
	},
	authz.FieldBankAccountNumber: {
		Field: authz.FieldBankAccountNumber, Domain: authz.DomainBank, GovernmentClass: GovernmentClassNone, Classification: GovernmentClassNone,
		Sensitivity: SensitivityHighlyRestricted, LowerEnvironment: HandlingExclude,
	},
	authz.FieldPerformanceRating: {
		Field: authz.FieldPerformanceRating, Domain: authz.DomainPerformance, GovernmentClass: GovernmentClassNone, Classification: GovernmentClassNone,
		Sensitivity: SensitivityRestricted, LowerEnvironment: HandlingDeterministicMask,
	},
	authz.FieldMedicalAccomodation: {
		Field: authz.FieldMedicalAccomodation, Domain: authz.DomainMedical, GovernmentClass: GovernmentClassACA, Classification: GovernmentClassACA,
		Sensitivity: SensitivityHighlyRestricted, LowerEnvironment: HandlingExclude,
	},
	authz.FieldCaseNotes: {
		Field: authz.FieldCaseNotes, Domain: authz.DomainEmployeeRelations, GovernmentClass: GovernmentClassCJI, Classification: GovernmentClassCJI,
		Sensitivity: SensitivityHighlyRestricted, LowerEnvironment: HandlingIrreversibleTokenize,
	},
	authz.FieldVisaStatus: {
		Field: authz.FieldVisaStatus, Domain: authz.DomainImmigration, GovernmentClass: GovernmentClassNone, Classification: GovernmentClassNone,
		Sensitivity: SensitivityRestricted, LowerEnvironment: HandlingDeterministicMask,
	},
}

// Registry returns the complete closed field overlay in stable field-token
// order. The returned slice is independent of package state.
func Registry() []FieldPolicy {
	fields := make([]authz.FieldID, 0, len(fieldPolicies))
	for field := range fieldPolicies {
		fields = append(fields, field)
	}
	slices.Sort(fields)
	out := make([]FieldPolicy, 0, len(fields))
	for _, field := range fields {
		policy := fieldPolicies[field]
		policy.Controls, _ = ClassPolicyFor(policy.GovernmentClass)
		out = append(out, policy)
	}
	return out
}

// Fields is a semantic alias for Registry.
func Fields() []FieldPolicy { return Registry() }

// ResolveField checks both sides of the shared TRUST-010 registry and returns
// a complete field policy. The refusal names the offending field.
func ResolveField(field authz.FieldID) (FieldPolicy, error) {
	definition, ok := authz.FieldRegistry[field]
	if !ok {
		return FieldPolicy{}, refusal(string(field), ErrUnknownField, "field is absent from TRUST-010 registry")
	}
	policy, ok := fieldPolicies[field]
	if !ok || policy.Field != definition.ID || policy.Domain != definition.Domain {
		return FieldPolicy{}, refusal(string(field), ErrRegistryDrift, "field overlay does not match TRUST-010 registry")
	}
	if !policy.GovernmentClass.Valid() {
		return FieldPolicy{}, refusal(string(field), ErrInvalidClassification, "field has an invalid government-data class")
	}
	controls, err := ClassPolicyFor(policy.GovernmentClass)
	if err != nil {
		return FieldPolicy{}, err
	}
	policy.Controls = controls
	if policy.Sensitivity.restricted() && !policy.LowerEnvironment.Valid() || !policy.Sensitivity.restricted() && policy.LowerEnvironment != HandlingUnchanged {
		return FieldPolicy{}, refusal(string(field), ErrInvalidHandlingRule, "field lower-environment rule is inconsistent with sensitivity")
	}
	return policy, nil
}

// ResolveFields resolves a stable ordered set of field policies and rejects
// duplicates or unknown fields by naming the offending field.
func ResolveFields(fields []authz.FieldID) ([]FieldPolicy, error) {
	seen := make(map[authz.FieldID]struct{}, len(fields))
	out := make([]FieldPolicy, 0, len(fields))
	for _, field := range fields {
		if _, exists := seen[field]; exists {
			return nil, refusal(string(field), ErrDuplicateField, "field is repeated")
		}
		seen[field] = struct{}{}
		policy, err := ResolveField(field)
		if err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, nil
}

// Environment identifies where a fixture copy will be used.
type Environment string

const (
	EnvironmentDevelopment Environment = "DEVELOPMENT"
	EnvironmentStaging     Environment = "STAGING"
	EnvironmentSandbox     Environment = "SANDBOX"
	EnvironmentProduction  Environment = "PRODUCTION"

	Development = EnvironmentDevelopment
	Staging     = EnvironmentStaging
	Sandbox     = EnvironmentSandbox
	Production  = EnvironmentProduction
)

func (e Environment) valid() bool {
	switch e {
	case EnvironmentDevelopment, EnvironmentStaging, EnvironmentSandbox, EnvironmentProduction:
		return true
	default:
		return false
	}
}

func (e Environment) lower() bool {
	return e == EnvironmentDevelopment || e == EnvironmentStaging || e == EnvironmentSandbox
}

var (
	ErrInvalidClassification = errors.New("dataclass: invalid government-data classification")
	ErrUnknownField          = errors.New("dataclass: unknown field")
	ErrRegistryDrift         = errors.New("dataclass: field registry drift")
	ErrDuplicateField        = errors.New("dataclass: duplicate field")
	ErrInvalidHandlingRule   = errors.New("dataclass: invalid lower-environment handling rule")
	ErrInvalidReceipt        = errors.New("dataclass: invalid policy receipt")
	ErrCustodyUnavailable    = errors.New("dataclass: custody derivation unavailable")
	ErrIrreversible          = errors.New("dataclass: lower-environment value is irreversible")
	ErrInvalidProfile        = errors.New("dataclass: invalid contract profile")
	ErrClaimRefused          = errors.New("dataclass: compliance claim refused")
)

// RefusalError is a typed fail-closed refusal. Field identifies the exact
// policy input that could not be safely resolved.
type RefusalError struct {
	Field  string
	Reason string
	Err    error
}

func (e *RefusalError) Error() string {
	return fmt.Sprintf("dataclass: field %q refused: %s", e.Field, e.Reason)
}
func (e *RefusalError) Unwrap() error { return e.Err }

func refusal(field string, err error, reason string) error {
	return &RefusalError{Field: field, Reason: reason, Err: err}
}

// PolicyReceipt is an immutable, digest-bound record of one field's complete
// classification and handling decision. It contains no field value.
type PolicyReceipt struct {
	SchemaVersion    int                 `json:"schema_version"`
	Revision         uint64              `json:"revision"`
	Field            authz.FieldID       `json:"field"`
	Domain           authz.DataDomain    `json:"domain"`
	GovernmentClass  GovernmentDataClass `json:"government_class"`
	Sensitivity      Sensitivity         `json:"sensitivity"`
	Controls         ClassPolicy         `json:"controls"`
	Environment      Environment         `json:"environment"`
	LowerEnvironment HandlingRule        `json:"lower_environment"`
	FixtureDigest    string              `json:"fixture_digest,omitempty"`
	AuthZPolicy      string              `json:"authz_policy,omitempty"`
	AuthZEffect      authz.Effect        `json:"authz_effect,omitempty"`
	AuthZRule        string              `json:"authz_rule,omitempty"`
	Purpose          string              `json:"purpose,omitempty"`
	Digest           string              `json:"digest"`
}

func (r PolicyReceipt) canonical() string {
	return fmt.Sprintf("schema=%d;revision=%d;field=%s;domain=%s;class=%s;sensitivity=%s;storage=%s;key=%s;access=%s;disclosure=%s;personnel=%s;retention=%s;environment=%s;handling=%s;fixture=%s;authz_policy=%s;authz_effect=%d;authz_rule=%s;purpose=%s",
		r.SchemaVersion, r.Revision, r.Field, r.Domain, r.GovernmentClass, r.Sensitivity, r.Controls.Storage, r.Controls.Key, r.Controls.Access, r.Controls.Disclosure, r.Controls.Personnel, r.Controls.Retention, r.Environment, r.LowerEnvironment, r.FixtureDigest, r.AuthZPolicy, r.AuthZEffect, r.AuthZRule, r.Purpose)
}

func digestText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verify checks the immutable receipt digest and the field overlay.
func (r PolicyReceipt) Verify() error {
	if r.SchemaVersion != schemaVersion || r.Revision == 0 || r.Digest == "" {
		return ErrInvalidReceipt
	}
	if _, err := ResolveField(r.Field); err != nil {
		return err
	}
	if digestText(r.canonical()) != r.Digest {
		return refusal(string(r.Field), ErrInvalidReceipt, "policy receipt digest does not match its revision")
	}
	return nil
}

func receiptFor(policy FieldPolicy, environment Environment, fixtureDigest string, ruling *authz.FieldRuling, purpose, authzPolicy string) (PolicyReceipt, error) {
	if !environment.valid() {
		return PolicyReceipt{}, refusal(string(policy.Field), ErrInvalidReceipt, "environment is not closed")
	}
	if environment.lower() && fixtureDigest == "" {
		return PolicyReceipt{}, refusal(string(policy.Field), ErrInvalidReceipt, "lower-environment fixture digest is required")
	}
	receipt := PolicyReceipt{
		SchemaVersion: schemaVersion, Revision: 1, Field: policy.Field, Domain: policy.Domain,
		GovernmentClass: policy.GovernmentClass, Sensitivity: policy.Sensitivity, Controls: policy.Controls,
		Environment: environment, LowerEnvironment: policy.LowerEnvironment, FixtureDigest: fixtureDigest,
		Purpose: purpose, AuthZPolicy: authzPolicy,
	}
	if ruling != nil {
		receipt.AuthZEffect, receipt.AuthZRule = ruling.Effect, ruling.RuleID
	}
	receipt.Digest = digestText(receipt.canonical())
	return receipt, nil
}

// NewPolicyReceipt records a field policy before a copy is created. A
// non-production receipt must name the resulting fixture digest.
func NewPolicyReceipt(field authz.FieldID, environment Environment, fixtureDigest string) (PolicyReceipt, error) {
	policy, err := ResolveField(field)
	if err != nil {
		return PolicyReceipt{}, err
	}
	return receiptFor(policy, environment, fixtureDigest, nil, "", "")
}

// ResolvePolicyReceipts combines the real TRUST-010 field decision with the
// government overlay. A receipt is emitted for every requested field,
// including denied fields, so absence is never confused with a grant.
func ResolvePolicyReceipts(decision authz.FieldDecision, environment Environment, fixtureDigests map[authz.FieldID]string) ([]PolicyReceipt, error) {
	fields := make([]authz.FieldID, 0, len(decision.Rulings))
	for field := range decision.Rulings {
		fields = append(fields, field)
	}
	slices.Sort(fields)
	result := make([]PolicyReceipt, 0, len(fields))
	for _, field := range fields {
		policy, err := ResolveField(field)
		if err != nil {
			return nil, err
		}
		ruling := decision.Rulings[field]
		fixture := ""
		if fixtureDigests != nil {
			fixture = fixtureDigests[field]
		}
		receipt, err := receiptFor(policy, environment, fixture, &ruling, decision.Purpose, decision.PolicyVersion)
		if err != nil {
			return nil, err
		}
		result = append(result, receipt)
	}
	return result, nil
}

// Deriver is the only dependency needed to transform lower-environment
// values. Production key material remains inside the custody provider.
type Deriver interface {
	Derive(custody.Context, custody.Handle, []byte) (custody.DerivedValue, custody.Receipt, error)
}

// Transformer prepares lower-environment values with a custody-backed PRF.
// It stores no source value or mapping and therefore has no re-identification
// path.
type Transformer struct {
	deriver Deriver
	key     custody.Handle
}

// New creates a lower-environment transformer backed by an opaque production
// custody key handle.
func New(deriver Deriver, key custody.Handle) (*Transformer, error) {
	if deriver == nil {
		return nil, ErrCustodyUnavailable
	}
	if err := key.Validate(); err != nil || key.Kind != custody.Key {
		return nil, refusal("production_custody_key", ErrCustodyUnavailable, "a valid KEY handle is required")
	}
	return &Transformer{deriver: deriver, key: key}, nil
}

// NewTransformer is a descriptive alias for New.
func NewTransformer(deriver Deriver, key custody.Handle) (*Transformer, error) {
	return New(deriver, key)
}

// Fixture is the sanitized result of one field copy. Value is either a
// custody-derived token/mask or nil for EXCLUDE; SourceDigest is only a
// digest and cannot be used to recover the source value.
type Fixture struct {
	SchemaVersion int           `json:"schema_version"`
	Revision      uint64        `json:"revision"`
	Field         authz.FieldID `json:"field"`
	Environment   Environment   `json:"environment"`
	Handling      HandlingRule  `json:"handling"`
	Value         []byte        `json:"value,omitempty"`
	SourceDigest  string        `json:"source_digest"`
	OutputDigest  string        `json:"output_digest,omitempty"`
	PolicyReceipt PolicyReceipt `json:"policy_receipt"`
	Digest        string        `json:"digest"`
}

func (f Fixture) canonical() string {
	// The fixture digest binds the policy coordinates but deliberately does
	// not include PolicyReceipt.Digest or PolicyReceipt.FixtureDigest; those
	// fields point back to this digest and would create a circular revision.
	return fmt.Sprintf("schema=%d;revision=%d;field=%s;environment=%s;handling=%s;source=%s;output=%s;policy_class=%s;policy_sensitivity=%s;policy_lower=%s;value=%x",
		f.SchemaVersion, f.Revision, f.Field, f.Environment, f.Handling, f.SourceDigest, f.OutputDigest, f.PolicyReceipt.GovernmentClass, f.PolicyReceipt.Sensitivity, f.PolicyReceipt.LowerEnvironment, f.Value)
}

// Verify checks the fixture and its embedded policy receipt without needing
// the source value or custody key.
func (f Fixture) Verify() error {
	if f.SchemaVersion != schemaVersion || f.Revision == 0 || f.Digest == "" || f.SourceDigest == "" {
		return ErrInvalidReceipt
	}
	if err := f.PolicyReceipt.Verify(); err != nil {
		return err
	}
	if f.PolicyReceipt.FixtureDigest != f.Digest {
		return refusal(string(f.Field), ErrInvalidReceipt, "fixture digest is not recorded in the policy receipt")
	}
	if digestText(f.canonical()) != f.Digest {
		return refusal(string(f.Field), ErrInvalidReceipt, "fixture revision digest does not match")
	}
	return nil
}

// PrepareCopyRequest is the complete input to a lower-environment copy.
type PrepareCopyRequest struct {
	Context     custody.Context
	Field       authz.FieldID
	Value       []byte
	Environment Environment
}

// PrepareCopy resolves the policy before transforming the value. It refuses
// production targets and refuses to return a raw restricted value.
func (t *Transformer) PrepareCopy(request PrepareCopyRequest) (Fixture, error) {
	if t == nil || t.deriver == nil {
		return Fixture{}, refusal(string(request.Field), ErrCustodyUnavailable, "production custody derivation is unavailable")
	}
	policy, err := ResolveField(request.Field)
	if err != nil {
		return Fixture{}, err
	}
	if !request.Environment.valid() || !request.Environment.lower() {
		return Fixture{}, refusal(string(request.Field), ErrInvalidHandlingRule, "copy target must be a closed non-production environment")
	}
	if err := request.Context.Validate(); err != nil {
		return Fixture{}, refusal(string(request.Field), ErrCustodyUnavailable, "custody context is invalid")
	}
	if request.Context.Tenant != t.key.Tenant || request.Context.Region != t.key.Region {
		return Fixture{}, refusal(string(request.Field), ErrCustodyUnavailable, "custody context does not match production key scope")
	}
	sourceDigest := digestText(string(request.Value))
	var output []byte
	if policy.LowerEnvironment != HandlingExclude {
		derived, _, deriveErr := t.deriver.Derive(request.Context, t.key, []byte(strings.Join([]string{"hcm-next/dataclass", string(request.Field), string(policy.GovernmentClass), string(policy.LowerEnvironment), string(request.Environment)}, "\x00")))
		if deriveErr != nil || len(derived.Output) < sha256.Size {
			return Fixture{}, refusal(string(request.Field), ErrCustodyUnavailable, "production custody derivation failed")
		}
		mac := hmac.New(sha256.New, derived.Output)
		_, _ = mac.Write(request.Value)
		prefix := "mask:v1:"
		if policy.LowerEnvironment == HandlingIrreversibleTokenize {
			prefix = "token:v1:"
		}
		output = []byte(prefix + hex.EncodeToString(mac.Sum(nil)))
	}
	outputDigest := ""
	if len(output) > 0 {
		outputDigest = digestText(string(output))
	}
	fixture := Fixture{
		SchemaVersion: schemaVersion, Revision: 1, Field: request.Field, Environment: request.Environment,
		Handling: policy.LowerEnvironment, Value: slices.Clone(output), SourceDigest: sourceDigest, OutputDigest: outputDigest,
	}
	fixture.PolicyReceipt, err = receiptFor(policy, request.Environment, "pending", nil, "lower_environment_copy", "")
	if err != nil {
		return Fixture{}, err
	}
	// The receipt must contain the final fixture digest. Compute the fixture
	// digest once with a placeholder, then bind both records to the final value.
	fixture.PolicyReceipt.FixtureDigest = ""
	fixture.Digest = digestText(fixture.canonical())
	fixture.PolicyReceipt.FixtureDigest = fixture.Digest
	fixture.PolicyReceipt.Digest = digestText(fixture.PolicyReceipt.canonical())
	fixture.Digest = digestText(fixture.canonical())
	return fixture, nil
}

// Reverse always refuses. No mapping is retained and the output is a PRF
// result, so a lower-environment fixture cannot be used to recover source
// data even if a fixture record is copied elsewhere.
func (t *Transformer) Reverse(_ custody.Context, _ Fixture) ([]byte, error) {
	return nil, ErrIrreversible
}

// FixtureDigest returns the reproducible digest of a sanitized fixture's
// public fields. It is useful to compare independently generated fixtures.
func FixtureDigest(f Fixture) string { return f.Digest }

// EvidenceRecord is digest-only evidence admitted to a DoD contract profile.
type EvidenceRecord struct {
	Ref     string `json:"ref"`
	Digest  string `json:"digest"`
	Control string `json:"control"`
	Status  string `json:"status"`
}

const (
	EvidenceAccepted = "ACCEPTED"
	EvidencePOAM     = "POAM"
	EvidenceRejected = "REJECTED"
)

func (e EvidenceRecord) valid() bool {
	return strings.TrimSpace(e.Ref) != "" && strings.TrimSpace(e.Digest) != "" && strings.TrimSpace(e.Control) != "" && (e.Status == EvidenceAccepted || e.Status == EvidencePOAM || e.Status == EvidenceRejected)
}

// CMMCLevel is the contractually applicable CMMC level.
type CMMCLevel uint8

const (
	CMMCLevelUnspecified CMMCLevel = iota
	CMMCLevel1
	CMMCLevel2
	CMMCLevel3
)

func (l CMMCLevel) valid() bool { return l >= CMMCLevel1 && l <= CMMCLevel3 }

// NISTRevision distinguishes the publication version named by a contract.
type NISTRevision string

const (
	NIST800171Rev2 NISTRevision = "NIST SP 800-171 Rev. 2"
	NIST800171Rev3 NISTRevision = "NIST SP 800-171 Rev. 3"
	NISTRev2                    = NIST800171Rev2
	NISTRev3                    = NIST800171Rev3
)

func (r NISTRevision) valid() bool { return r == NIST800171Rev2 || r == NIST800171Rev3 }

// AssessmentMethod records who and how assessed the engagement.
type AssessmentMethod string

const (
	AssessmentSelf   AssessmentMethod = "SELF_ASSESSMENT"
	AssessmentC3PAO  AssessmentMethod = "C3PAO"
	AssessmentDIBCAC AssessmentMethod = "DIBCAC"
)

func (m AssessmentMethod) valid() bool {
	return m == AssessmentSelf || m == AssessmentC3PAO || m == AssessmentDIBCAC
}

// POAMRule records whether a claim may rely on an open POA&M item.
type POAMRule string

const (
	POAMProhibited POAMRule = "PROHIBITED"
	POAMAllowed    POAMRule = "ALLOWED"
	POAMNoPOAM              = POAMProhibited
)

func (r POAMRule) valid() bool { return r == POAMProhibited || r == POAMAllowed }

// ContractProfileSpec is the constructor input for one DoD-touching
// engagement. Evidence is copied and sorted into the immutable profile.
type ContractProfileSpec struct {
	EngagementRef    string
	Clause           string
	CMMCLevel        CMMCLevel
	NISTRevision     NISTRevision
	AssessmentMethod AssessmentMethod
	POAMRule         POAMRule
	Evidence         []EvidenceRecord
}

// ContractProfile is a versioned immutable contract record. AddEvidence
// returns a new revision instead of mutating an existing record.
type ContractProfile struct {
	SchemaVersion    int              `json:"schema_version"`
	Revision         uint64           `json:"revision"`
	EngagementRef    string           `json:"engagement_ref"`
	Clause           string           `json:"clause"`
	CMMCLevel        CMMCLevel        `json:"cmmc_level"`
	NISTRevision     NISTRevision     `json:"nist_revision"`
	AssessmentMethod AssessmentMethod `json:"assessment_method"`
	POAMRule         POAMRule         `json:"poam_rule"`
	Evidence         []EvidenceRecord `json:"evidence"`
	Digest           string           `json:"digest"`
}

func canonicalEvidence(evidence []EvidenceRecord) string {
	var b strings.Builder
	for _, e := range evidence {
		fmt.Fprintf(&b, "%d:%s;%d:%s;%d:%s;%d:%s;", len(e.Ref), e.Ref, len(e.Digest), e.Digest, len(e.Control), e.Control, len(e.Status), e.Status)
	}
	return b.String()
}

func (p ContractProfile) canonical() string {
	return fmt.Sprintf("schema=%d;revision=%d;engagement=%d:%s;clause=%d:%s;level=%d;nist=%s;assessment=%s;poam=%s;evidence=%s",
		p.SchemaVersion, p.Revision, len(p.EngagementRef), p.EngagementRef, len(p.Clause), p.Clause, p.CMMCLevel, p.NISTRevision, p.AssessmentMethod, p.POAMRule, canonicalEvidence(p.Evidence))
}

func validateEvidence(evidence []EvidenceRecord) error {
	if len(evidence) == 0 {
		return refusal("evidence", ErrInvalidProfile, "at least one evidence record is required")
	}
	seen := make(map[string]struct{}, len(evidence))
	for _, item := range evidence {
		if !item.valid() {
			return refusal(item.Ref, ErrInvalidProfile, "evidence must contain ref, digest, control and a closed status")
		}
		key := item.Ref + "\x00" + item.Digest
		if _, ok := seen[key]; ok {
			return refusal(item.Ref, ErrInvalidProfile, "evidence record is duplicated")
		}
		seen[key] = struct{}{}
	}
	return nil
}

// NewContractProfile validates and returns revision one of a DoD contract
// profile.
func NewContractProfile(spec ContractProfileSpec) (ContractProfile, error) {
	if strings.TrimSpace(spec.EngagementRef) == "" {
		return ContractProfile{}, refusal("engagement_ref", ErrInvalidProfile, "engagement reference is required")
	}
	if strings.TrimSpace(spec.Clause) == "" {
		return ContractProfile{}, refusal("clause", ErrInvalidProfile, "exact contract clause is required")
	}
	if !spec.CMMCLevel.valid() {
		return ContractProfile{}, refusal("cmmc_level", ErrInvalidProfile, "CMMC level must be 1, 2 or 3")
	}
	if !spec.NISTRevision.valid() {
		return ContractProfile{}, refusal("nist_revision", ErrInvalidProfile, "NIST 800-171 revision must be 2 or 3")
	}
	if !spec.AssessmentMethod.valid() {
		return ContractProfile{}, refusal("assessment_method", ErrInvalidProfile, "assessment method is not closed")
	}
	if !spec.POAMRule.valid() {
		return ContractProfile{}, refusal("poam_rule", ErrInvalidProfile, "POA&M rule is not closed")
	}
	evidence := slices.Clone(spec.Evidence)
	slices.SortFunc(evidence, func(a, b EvidenceRecord) int {
		if a.Ref != b.Ref {
			return strings.Compare(a.Ref, b.Ref)
		}
		return strings.Compare(a.Digest, b.Digest)
	})
	if err := validateEvidence(evidence); err != nil {
		return ContractProfile{}, err
	}
	profile := ContractProfile{SchemaVersion: schemaVersion, Revision: 1, EngagementRef: spec.EngagementRef, Clause: spec.Clause, CMMCLevel: spec.CMMCLevel, NISTRevision: spec.NISTRevision, AssessmentMethod: spec.AssessmentMethod, POAMRule: spec.POAMRule, Evidence: evidence}
	profile.Digest = digestText(profile.canonical())
	return profile, nil
}

// Verify checks the profile's immutable revision digest.
func (p ContractProfile) Verify() error {
	if p.SchemaVersion != schemaVersion || p.Revision == 0 || p.Digest == "" {
		return ErrInvalidProfile
	}
	if err := validateEvidence(p.Evidence); err != nil {
		return err
	}
	if digestText(p.canonical()) != p.Digest {
		return ErrInvalidProfile
	}
	return nil
}

// AddEvidence returns the next immutable profile revision.
func (p ContractProfile) AddEvidence(item EvidenceRecord) (ContractProfile, error) {
	if err := p.Verify(); err != nil {
		return ContractProfile{}, err
	}
	evidence := append(slices.Clone(p.Evidence), item)
	next, err := NewContractProfile(ContractProfileSpec{EngagementRef: p.EngagementRef, Clause: p.Clause, CMMCLevel: p.CMMCLevel, NISTRevision: p.NISTRevision, AssessmentMethod: p.AssessmentMethod, POAMRule: p.POAMRule, Evidence: evidence})
	if err != nil {
		return ContractProfile{}, err
	}
	next.Revision = p.Revision + 1
	next.Digest = digestText(next.canonical())
	return next, nil
}

// ComplianceClaim is the evidence-backed assertion a contract or
// questionnaire is allowed to make.
type ComplianceClaim struct {
	Clause           string
	CMMCLevel        CMMCLevel
	NISTRevision     NISTRevision
	AssessmentMethod AssessmentMethod
	POAMRule         POAMRule
	Evidence         []EvidenceRecord
}

// ValidateClaim blocks a claim whose contract fields or evidence set do not
// exactly match the recorded profile.
func (p ContractProfile) ValidateClaim(claim ComplianceClaim) error {
	if err := p.Verify(); err != nil {
		return err
	}
	if claim.Clause != p.Clause || claim.CMMCLevel != p.CMMCLevel || claim.NISTRevision != p.NISTRevision || claim.AssessmentMethod != p.AssessmentMethod || claim.POAMRule != p.POAMRule {
		return refusal("contract_profile", ErrClaimRefused, "claim does not match recorded clause, CMMC level, NIST revision, assessment method or POA&M rule")
	}
	if len(claim.Evidence) == 0 {
		return refusal("evidence", ErrClaimRefused, "claim has no evidence set")
	}
	available := make(map[string]EvidenceRecord, len(p.Evidence))
	for _, item := range p.Evidence {
		available[item.Ref+"\x00"+item.Digest] = item
	}
	for _, item := range claim.Evidence {
		recorded, ok := available[item.Ref+"\x00"+item.Digest]
		if !ok || item.Control != recorded.Control || item.Status != recorded.Status {
			return refusal("evidence", ErrClaimRefused, "claim evidence is not the recorded evidence revision")
		}
		if recorded.Status != EvidenceAccepted && !(recorded.Status == EvidencePOAM && p.POAMRule == POAMAllowed) {
			return refusal("evidence", ErrClaimRefused, "claim includes evidence that is not admissible under the recorded POA&M rule")
		}
	}
	return nil
}

// EvaluateComplianceClaim is the functional form of ContractProfile's
// validation method.
func EvaluateComplianceClaim(profile ContractProfile, claim ComplianceClaim) error {
	return profile.ValidateClaim(claim)
}

// Explain is a fixed, audit-safe description. It intentionally contains no
// field values, account numbers, tenant identifiers, custody material or
// evidence references supplied by a caller.
func Explain() string {
	return "government data classes drive field-specific storage, custody key, access, disclosure, personnel, retention, lower-environment handling, and evidence-bound DoD contract claims"
}
