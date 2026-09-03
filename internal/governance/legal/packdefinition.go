package legal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// This file implements the contract's section 3 release family:
//
//	PackDefinition   authoring artifact; mutable; not evaluable
//	      | author + cite + type
//	PackCandidate    complete, validated, unsigned; review mode only
//	      | review + sign
//	PackRelease      immutable, digested, signed; the only thing Evaluate reads
//
// [PackRelease] is an alias for [RulePack] (see rulepack.go): the contract
// states that RulePack is the release shape, so there is exactly one release
// struct and the loader's job is to produce it from a checked-in file rather
// than from a Go literal.

// Pack-definition errors. All are matchable with errors.Is.
var (
	ErrPackDefinitionSchema  = errors.New("legal: PACK_VALIDATION_FAILED: definition file is malformed")
	ErrPackDefinitionField   = errors.New("legal: PACK_VALIDATION_FAILED: definition field is missing or invalid")
	ErrPackNotSigned         = errors.New("legal: release carries no signature")
	ErrPackSignatureRoleDupe = errors.New("legal: release carries two signatures for the same role")
)

// definitionSchemaVersion is the definition-file schema this build reads. It
// is separate from [VocabularyVersion]: the schema describes the file's
// shape, the vocabulary describes which obligation kinds the author could
// have typed against.
const definitionSchemaVersion = 1

// SigningRole names which pipeline role a signature comes from. The
// contract's section 7.1 separates duties: the Rule Author may not review the
// same pack, and the Release Publisher's key is held by neither the author
// nor a reviewer.
type SigningRole string

// Signing roles.
const (
	SigningRoleReleasePublisher    SigningRole = "RELEASE_PUBLISHER"
	SigningRoleVendorLegalReviewer SigningRole = "VENDOR_LEGAL_REVIEWER"
	SigningRoleCustomerCounsel     SigningRole = "CUSTOMER_COUNSEL"
)

// RoleSignature is one role's detached ed25519 signature over a release
// digest. A release carries one per role that approved it, all over the same
// digest.
type RoleSignature struct {
	Role      SigningRole
	Signature Signature
}

// --- definition file shape --------------------------------------------------

// PackDefinition is the checked-in authoring artifact under
// definitions/legal/packs. It is mutable, it is not evaluable, and it is the
// only place a rule is written by hand or by an extractor.
type PackDefinition struct {
	SchemaVersion     int                 `json:"schema_version"`
	PackID            string              `json:"pack_id"`
	Version           PackVersion         `json:"version"`
	VocabularyVersion uint32              `json:"vocabulary_version"`
	Jurisdiction      JurisdictionJSON    `json:"jurisdiction"`
	Window            WindowJSON          `json:"window"`
	SourceType        string              `json:"source_type"`
	ReviewStatus      string              `json:"review_status"`
	Obligations       []ObligationJSON    `json:"obligations"`
	Preemptions       []PreemptionJSON    `json:"preemption_assertions"`
	Supersedes        *PackReleaseRefJSON `json:"supersedes,omitempty"`
	// Provenance is authoring metadata. The contract's section 3.2 excludes
	// it from the digest deliberately, so re-extracting a file and recording
	// a new extractor version can never invalidate a signed release.
	Provenance ProvenanceJSON `json:"provenance"`
}

// PackVersion is the contract's major.minor release version.
type PackVersion struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
}

// JurisdictionJSON is the contract's JurisdictionRef. LocalityPath is ordered
// coarse to fine; [Jurisdiction.Locality] is its last element, which is the
// compatibility rule section 2.1 states.
type JurisdictionJSON struct {
	Country      string   `json:"country"`
	Subdivision  string   `json:"subdivision"`
	LocalityPath []string `json:"locality_path"`
	Level        string   `json:"level"`
}

// WindowJSON is an EffectiveWindow in ISO calendar dates.
type WindowJSON struct {
	Start string `json:"start"`
	End   string `json:"end,omitempty"`
}

// PackReleaseRefJSON pins another release of the same pack.
type PackReleaseRefJSON struct {
	PackID       string           `json:"pack_id"`
	Version      PackVersion      `json:"version"`
	Jurisdiction JurisdictionJSON `json:"jurisdiction"`
}

// ProvenanceJSON records how the definition was produced. It is outside the
// digest.
type ProvenanceJSON struct {
	// Generator names what wrote the file, e.g. the extractor package.
	Generator string `json:"generator"`
	// SourceFiles lists the research files the definition was extracted from.
	SourceFiles []string `json:"source_files"`
	// Notes carries any human-facing caveat about the extraction.
	Notes string `json:"notes,omitempty"`
}

// CitationJSON is a Citation on the wire. `note` is present in the file and
// absent from the digest, exactly as the contract's section 3.2 requires.
type CitationJSON struct {
	SourceFile       string `json:"source_file"`
	Section          string `json:"section"`
	Note             string `json:"note,omitempty"`
	ReviewStatus     string `json:"review_status"`
	ConfidenceMarker string `json:"confidence_marker"`
}

// PreemptionJSON is a PreemptionAssertion on the wire.
type PreemptionJSON struct {
	Kind     string       `json:"kind"`
	Scope    string       `json:"scope"`
	Citation CitationJSON `json:"citation"`
}

// ObligationJSON is one obligation: its kind, its id, its citation and its
// typed body.
type ObligationJSON struct {
	Kind     string       `json:"kind"`
	ID       string       `json:"id"`
	Citation CitationJSON `json:"citation"`
	Body     BodyJSON     `json:"body"`
}

// MoneyJSON is a Money on the wire.
type MoneyJSON struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// BodyJSON is the union of every typed body's fields. One flat struct with
// omitempty keeps the checked-in files small and their key order fixed by Go
// struct order, which is what makes regeneration byte-identical. The loader
// reads only the fields the declared kind owns and rejects the rest, so a
// stray key can never smuggle a value into a body that does not have it.
type BodyJSON struct {
	// NOTICE
	Who             string   `json:"who,omitempty"`
	TimingDirection string   `json:"timing_direction,omitempty"`
	TimingDays      int      `json:"timing_days,omitempty"`
	UnlessCondition string   `json:"unless_condition,omitempty"`
	Channel         string   `json:"channel,omitempty"`
	ContentFields   []string `json:"content_fields,omitempty"`
	// FIELD_RESTRICTION
	RestrictedFields []string `json:"restricted_fields,omitempty"`
	Context          string   `json:"context,omitempty"`
	// RETENTION
	RecordClass          string `json:"record_class,omitempty"`
	DurationYears        int    `json:"duration_years,omitempty"`
	DurationBasis        string `json:"duration_basis,omitempty"`
	JurisdictionOverride bool   `json:"jurisdiction_override,omitempty"`
	// LEAVE_INTERACTION
	LeaveType       string `json:"leave_type,omitempty"`
	InteractionRule string `json:"interaction_rule,omitempty"`
	// PAY_FREQUENCY
	MinimumFrequency     string `json:"minimum_frequency,omitempty"`
	AppliesToWorkerClass string `json:"applies_to_worker_class,omitempty"`
	// FINAL_PAY_DEADLINE and PAY_TRANSPARENCY
	Trigger             string `json:"trigger,omitempty"`
	DeadlineDescription string `json:"deadline_description,omitempty"`
	RequiredDisclosure  string `json:"required_disclosure,omitempty"`
	// NON_COMPETE
	RecheckOnPayChange bool   `json:"recheck_on_pay_change,omitempty"`
	Rule               string `json:"rule,omitempty"`
	// E_VERIFY and MINI_WARN
	RequiredOnNewHireOnly bool   `json:"required_on_new_hire_only,omitempty"`
	Note                  string `json:"note,omitempty"`
	EmployeeThreshold     int    `json:"employee_threshold,omitempty"`
	LayoffWindowDays      int    `json:"layoff_window_days,omitempty"`
	NoticeDays            int    `json:"notice_days,omitempty"`
	// WAGE_FLOOR
	FloorAmount        *MoneyJSON `json:"floor_amount,omitempty"`
	WorkerClass        string     `json:"worker_class,omitempty"`
	Basis              string     `json:"basis,omitempty"`
	Indexation         string     `json:"indexation,omitempty"`
	NextAdjustmentDate string     `json:"next_adjustment_date,omitempty"`
	// PAY_EQUITY_REVIEW
	ProtectedBases         []string `json:"protected_bases,omitempty"`
	ComparatorStandard     string   `json:"comparator_standard,omitempty"`
	EmployerSizeFloor      int      `json:"employer_size_floor,omitempty"`
	PermittedDifferentials []string `json:"permitted_differentials,omitempty"`
	DocumentationRequired  bool     `json:"documentation_required,omitempty"`
	// PAY_STATEMENT
	RequiredFields  []string `json:"required_fields,omitempty"`
	Delivery        string   `json:"delivery,omitempty"`
	ConsentRequired bool     `json:"consent_required,omitempty"`
	// CLASSIFICATION
	Dimension       string     `json:"dimension,omitempty"`
	TestDescription string     `json:"test_description,omitempty"`
	SalaryThreshold *MoneyJSON `json:"salary_threshold,omitempty"`
	OvertimeTrigger string     `json:"overtime_trigger,omitempty"`
	// PERSONNEL_FILE
	ResponseDays        int  `json:"response_days,omitempty"`
	FrequencyCapPerYear int  `json:"frequency_cap_per_year,omitempty"`
	CopyFeePermitted    bool `json:"copy_fee_permitted,omitempty"`
	// ANTI_RETALIATION
	ProtectedActivities []string `json:"protected_activities,omitempty"`
	LookbackDays        int      `json:"lookback_days,omitempty"`
	Disposition         string   `json:"disposition,omitempty"`
	// JOB_SECURITY
	StandardKind          string `json:"standard_kind,omitempty"`
	ProbationDays         int    `json:"probation_days,omitempty"`
	JustificationRequired bool   `json:"justification_required,omitempty"`
	// SEPARATION_FILING
	FormName           string   `json:"form_name,omitempty"`
	RecipientAuthority string   `json:"recipient_authority,omitempty"`
	DeadlineDays       int      `json:"deadline_days,omitempty"`
	ContentFieldsFiled []string `json:"content_fields_filed,omitempty"`
	// DRUG_TESTING
	PermittedBases        []string `json:"permitted_bases,omitempty"`
	WrittenPolicyRequired bool     `json:"written_policy_required,omitempty"`
	AdvanceNoticeDays     int      `json:"advance_notice_days,omitempty"`
	ProtectedStatus       []string `json:"protected_status,omitempty"`
	// BREACH_NOTIFICATION
	SubjectDeadlineDays      int  `json:"subject_deadline_days,omitempty"`
	AuthorityThresholdCount  int  `json:"authority_threshold_count,omitempty"`
	AuthorityDeadlineDays    int  `json:"authority_deadline_days,omitempty"`
	CreditMonitoringRequired bool `json:"credit_monitoring_required,omitempty"`
	// AUTOMATED_DECISION
	CoveredUses         []string `json:"covered_uses,omitempty"`
	BiasAuditRequired   bool     `json:"bias_audit_required,omitempty"`
	AuditPeriodMonths   int      `json:"audit_period_months,omitempty"`
	CandidateNoticeDays int      `json:"candidate_notice_days,omitempty"`
	DisclosureRequired  bool     `json:"disclosure_required,omitempty"`
	// MONITORING_CONSENT
	DataCategories       []string `json:"data_categories,omitempty"`
	ConsentForm          string   `json:"consent_form,omitempty"`
	RetentionLimitMonths int      `json:"retention_limit_months,omitempty"`
	DeletionDeadlineDays int      `json:"deletion_deadline_days,omitempty"`
	// PERSONNEL_FILE, SEPARATION_FILING and BREACH_NOTIFICATION share a day
	// basis; the twelve added kinds share the recommendation qualifier.
	DayBasis string `json:"day_basis,omitempty"`
	Standard string `json:"standard,omitempty"`
}

// --- loading ----------------------------------------------------------------

// LoadPackDefinition parses a definition file's bytes. It rejects unknown
// keys: a typo in a rule file must fail loudly rather than silently drop a
// statutory field.
func LoadPackDefinition(data []byte) (PackDefinition, error) {
	var def PackDefinition
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&def); err != nil {
		return PackDefinition{}, fmt.Errorf("%w: %v", ErrPackDefinitionSchema, err)
	}
	if dec.More() {
		return PackDefinition{}, fmt.Errorf("%w: trailing content after the definition object", ErrPackDefinitionSchema)
	}
	if def.SchemaVersion != definitionSchemaVersion {
		return PackDefinition{}, fmt.Errorf("%w: schema_version = %d, this build reads %d",
			ErrPackDefinitionSchema, def.SchemaVersion, definitionSchemaVersion)
	}
	return def, nil
}

// LoadPackDefinitionFile reads and parses a definition file from disk.
func LoadPackDefinitionFile(path string) (PackDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PackDefinition{}, fmt.Errorf("%w: %v", ErrPackDefinitionSchema, err)
	}
	return LoadPackDefinition(data)
}

// PackCandidate is a complete, validated, unsigned pack. It is evaluable only
// in review mode: [PackCandidate.Unsigned] hands out the [RulePack] so a
// reviewer can simulate against it, and nothing else in this package accepts
// a candidate where a release is required.
type PackCandidate struct {
	pack RulePack
}

// Pack returns the candidate's unsigned rule pack. The name says what it is:
// a candidate, not a release. Registering one is legal — the two seed packs
// are registered unsigned — but a caller that requires a signed release
// checks [RulePack.Digest] and [RulePack.Signatures].
func (c PackCandidate) Pack() RulePack { return c.pack }

// Unsigned is a spelling of [PackCandidate.Pack] that reads correctly at a
// review-mode call site.
func (c PackCandidate) Unsigned() RulePack { return c.pack }

// Candidate turns a parsed definition into a validated, unsigned candidate.
// It never invents a value: every field the release needs must be present in
// the file, and an unknown enum token is an error rather than a default.
func (d PackDefinition) Candidate() (PackCandidate, error) {
	pack := RulePack{
		PackID:            d.PackID,
		Version:           d.Version.Major,
		MinorVersion:      d.Version.Minor,
		VocabularyVersion: VocabularyVersion(d.VocabularyVersion),
	}
	if pack.PackID == "" {
		return PackCandidate{}, fmt.Errorf("%w: pack_id is required", ErrPackDefinitionField)
	}
	if pack.Version == 0 {
		return PackCandidate{}, fmt.Errorf("%w: %s version.major must be positive", ErrPackDefinitionField, pack.PackID)
	}
	if pack.VocabularyVersion == VocabularyVersionUnspecified {
		return PackCandidate{}, fmt.Errorf("%w: %s declares no vocabulary_version", ErrPackDefinitionField, pack.PackID)
	}
	if pack.VocabularyVersion > SupportedVocabularyVersion {
		return PackCandidate{}, fmt.Errorf("%w: %s declares vocabulary %d, this build supports %d",
			ErrVocabularyVersionUnsupported, pack.PackID, pack.VocabularyVersion, SupportedVocabularyVersion)
	}

	jur, err := d.Jurisdiction.toJurisdiction()
	if err != nil {
		return PackCandidate{}, err
	}
	pack.Jurisdiction = jur

	window, err := d.Window.toWindow()
	if err != nil {
		return PackCandidate{}, err
	}
	pack.Window = window

	if pack.SourceType, err = ParseSourceType(d.SourceType); err != nil {
		return PackCandidate{}, fmt.Errorf("%w: %s: %w", ErrPackDefinitionField, pack.PackID, err)
	}
	if pack.ReviewStatus, err = ParseReviewStatus(d.ReviewStatus); err != nil {
		return PackCandidate{}, fmt.Errorf("%w: %s: %w", ErrPackDefinitionField, pack.PackID, err)
	}

	for _, o := range d.Obligations {
		if err := o.appendTo(&pack); err != nil {
			return PackCandidate{}, fmt.Errorf("%w: %s: %w", ErrPackDefinitionField, pack.PackID, err)
		}
	}
	for _, p := range d.Preemptions {
		kind, err := ParseObligationType(p.Kind)
		if err != nil {
			return PackCandidate{}, fmt.Errorf("%w: %s: %w", ErrPackDefinitionField, pack.PackID, err)
		}
		cit, err := p.Citation.toCitation()
		if err != nil {
			return PackCandidate{}, fmt.Errorf("%w: %s: %w", ErrPackDefinitionField, pack.PackID, err)
		}
		pack.PreemptionAssertions = append(pack.PreemptionAssertions, PreemptionAssertion{
			Kind: kind, Scope: p.Scope, Citation: cit,
		})
	}
	if d.Supersedes != nil {
		refJur, err := d.Supersedes.Jurisdiction.toJurisdiction()
		if err != nil {
			return PackCandidate{}, err
		}
		pack.Supersedes = &RulePackRelease{
			PackID:       d.Supersedes.PackID,
			Version:      d.Supersedes.Version.Major,
			MinorVersion: d.Supersedes.Version.Minor,
			Jurisdiction: refJur,
		}
	}

	if err := pack.ValidateForRelease(); err != nil {
		return PackCandidate{}, err
	}
	return PackCandidate{pack: pack}, nil
}

func (j JurisdictionJSON) toJurisdiction() (Jurisdiction, error) {
	out := Jurisdiction{Country: j.Country, State: j.Subdivision}
	if n := len(j.LocalityPath); n > 0 {
		out.Locality = j.LocalityPath[n-1]
	}
	switch j.Level {
	case "COUNTRY", "SUBDIVISION", "LOCALITY":
	default:
		return Jurisdiction{}, fmt.Errorf("%w: jurisdiction level %q", ErrPackDefinitionField, j.Level)
	}
	if j.Level == "LOCALITY" && len(j.LocalityPath) == 0 {
		return Jurisdiction{}, fmt.Errorf("%w: LOCALITY jurisdiction with an empty locality_path", ErrPackDefinitionField)
	}
	if j.Level == "SUBDIVISION" && len(j.LocalityPath) != 0 {
		return Jurisdiction{}, fmt.Errorf("%w: SUBDIVISION jurisdiction with a locality_path", ErrPackDefinitionField)
	}
	if err := out.Validate(); err != nil {
		return Jurisdiction{}, fmt.Errorf("%w: %w", ErrPackDefinitionField, err)
	}
	return out, nil
}

func (w WindowJSON) toWindow() (EffectiveWindow, error) {
	start, err := values.ParseLocalDate(w.Start)
	if err != nil {
		return EffectiveWindow{}, fmt.Errorf("%w: window.start %q: %w", ErrPackDefinitionField, w.Start, err)
	}
	if w.End == "" {
		return NewOpenEffectiveWindow(start)
	}
	end, err := values.ParseLocalDate(w.End)
	if err != nil {
		return EffectiveWindow{}, fmt.Errorf("%w: window.end %q: %w", ErrPackDefinitionField, w.End, err)
	}
	return NewClosedEffectiveWindow(start, end)
}

func (c CitationJSON) toCitation() (Citation, error) {
	status, err := ParseReviewStatus(c.ReviewStatus)
	if err != nil {
		return Citation{}, err
	}
	marker, err := ParseConfidenceMarker(c.ConfidenceMarker)
	if err != nil {
		return Citation{}, err
	}
	return Citation{
		SourceFile:       c.SourceFile,
		Section:          c.Section,
		Note:             c.Note,
		Status:           status,
		ConfidenceMarker: marker,
	}, nil
}

func (m *MoneyJSON) toMoney() (values.Money, error) {
	if m == nil {
		return values.Money{}, nil
	}
	return values.NewMoney(m.Amount, m.Currency, 2, values.RoundingHalfEven)
}

func parseOptionalDate(s string) (values.LocalDate, error) {
	if s == "" {
		return values.LocalDate{}, nil
	}
	return values.ParseLocalDate(s)
}

// appendTo decodes one obligation entry onto the pack's kind-specific slice.
//
// one-to-one mapping between a wire token and a typed body.
//
//nolint:gocyclo // one arm per obligation kind; splitting it would hide the
func (o ObligationJSON) appendTo(pack *RulePack) error {
	kind, err := ParseObligationType(o.Kind)
	if err != nil {
		return err
	}
	if o.ID == "" {
		return fmt.Errorf("obligation of kind %s has no id", o.Kind)
	}
	cit, err := o.Citation.toCitation()
	if err != nil {
		return fmt.Errorf("obligation %q: %v", o.ID, err)
	}
	std, err := ParseRuleStandard(o.Body.Standard)
	if err != nil {
		return fmt.Errorf("obligation %q: %v", o.ID, err)
	}
	b := o.Body

	switch kind {
	case ObligationTypeNotice:
		pack.Notices = append(pack.Notices, NoticeObligation{
			ID: o.ID, Who: b.Who, TimingDirection: b.TimingDirection, TimingDays: b.TimingDays,
			UnlessCondition: b.UnlessCondition, Channel: b.Channel, ContentFields: b.ContentFields,
			Citation: cit,
		})
	case ObligationTypeFieldRestriction:
		pack.FieldRestrictions = append(pack.FieldRestrictions, FieldRestriction{
			ID: o.ID, RestrictedFields: b.RestrictedFields, Context: b.Context, Citation: cit,
		})
	case ObligationTypeRetention:
		pack.RetentionRules = append(pack.RetentionRules, RetentionRule{
			ID: o.ID, RecordClass: b.RecordClass, DurationYears: b.DurationYears,
			DurationBasis: b.DurationBasis, JurisdictionOverride: b.JurisdictionOverride, Citation: cit,
		})
	case ObligationTypeLeaveInteraction:
		pack.LeaveInteractions = append(pack.LeaveInteractions, LeaveInteraction{
			ID: o.ID, LeaveType: b.LeaveType, InteractionRule: b.InteractionRule, Citation: cit,
		})
	case ObligationTypePayFrequency:
		pack.PayFrequencyConstraints = append(pack.PayFrequencyConstraints, PayFrequencyConstraint{
			ID: o.ID, MinimumFrequency: b.MinimumFrequency,
			AppliesToWorkerClass: b.AppliesToWorkerClass, Citation: cit,
		})
	case ObligationTypeFinalPayDeadline:
		pack.FinalPayDeadlines = append(pack.FinalPayDeadlines, FinalPayDeadline{
			ID: o.ID, Trigger: b.Trigger, DeadlineDescription: b.DeadlineDescription, Citation: cit,
		})
	case ObligationTypePayTransparency:
		pack.PayTransparencyDuties = append(pack.PayTransparencyDuties, PayTransparencyDuty{
			ID: o.ID, Trigger: b.Trigger, RequiredDisclosure: b.RequiredDisclosure, Citation: cit,
		})
	case ObligationTypeNonCompete:
		pack.NonCompeteThresholds = append(pack.NonCompeteThresholds, NonCompeteThreshold{
			ID: o.ID, ReCheckOnPayChange: b.RecheckOnPayChange, Rule: b.Rule, Citation: cit,
		})
	case ObligationTypeEVerify:
		pack.EVerifyChecks = append(pack.EVerifyChecks, EVerifyStatusCheck{
			ID: o.ID, RequiredOnNewHireOnly: b.RequiredOnNewHireOnly, Note: b.Note, Citation: cit,
		})
	case ObligationTypeMiniWARN:
		pack.MiniWARNTriggers = append(pack.MiniWARNTriggers, MiniWARNTrigger{
			ID: o.ID, EmployeeThreshold: b.EmployeeThreshold, LayoffWindowDays: b.LayoffWindowDays,
			NoticeDays: b.NoticeDays, Note: b.Note, Citation: cit,
		})
	case ObligationTypeWageFloor:
		floor, err := b.FloorAmount.toMoney()
		if err != nil {
			return fmt.Errorf("obligation %q floor_amount: %v", o.ID, err)
		}
		next, err := parseOptionalDate(b.NextAdjustmentDate)
		if err != nil {
			return fmt.Errorf("obligation %q next_adjustment_date: %v", o.ID, err)
		}
		pack.WageFloors = append(pack.WageFloors, WageFloorRule{
			ID: o.ID, FloorAmount: floor, WorkerClass: b.WorkerClass, Basis: b.Basis,
			Indexation: b.Indexation, NextAdjustmentDate: next, Standard: std, Citation: cit,
		})
	case ObligationTypePayEquityReview:
		pack.PayEquityReviews = append(pack.PayEquityReviews, PayEquityReviewRule{
			ID: o.ID, ProtectedBases: b.ProtectedBases, ComparatorStandard: b.ComparatorStandard,
			EmployerSizeFloor: b.EmployerSizeFloor, PermittedDifferentials: b.PermittedDifferentials,
			DocumentationRequired: b.DocumentationRequired, Standard: std, Citation: cit,
		})
	case ObligationTypePayStatement:
		pack.PayStatements = append(pack.PayStatements, PayStatementRule{
			ID: o.ID, RequiredFields: b.RequiredFields, Delivery: b.Delivery,
			ConsentRequired: b.ConsentRequired, Standard: std, Citation: cit,
		})
	case ObligationTypeClassification:
		threshold, err := b.SalaryThreshold.toMoney()
		if err != nil {
			return fmt.Errorf("obligation %q salary_threshold: %v", o.ID, err)
		}
		pack.Classifications = append(pack.Classifications, ClassificationRule{
			ID: o.ID, Dimension: b.Dimension, TestDescription: b.TestDescription,
			SalaryThreshold: threshold, OvertimeTrigger: b.OvertimeTrigger, Standard: std, Citation: cit,
		})
	case ObligationTypePersonnelFile:
		pack.PersonnelFileRules = append(pack.PersonnelFileRules, PersonnelFileRule{
			ID: o.ID, ResponseDays: b.ResponseDays, DayBasis: b.DayBasis,
			FrequencyCapPerYear: b.FrequencyCapPerYear, CopyFeePermitted: b.CopyFeePermitted,
			Standard: std, Citation: cit,
		})
	case ObligationTypeAntiRetaliation:
		pack.AntiRetaliationRules = append(pack.AntiRetaliationRules, AntiRetaliationRule{
			ID: o.ID, ProtectedActivities: b.ProtectedActivities, LookbackDays: b.LookbackDays,
			Disposition: b.Disposition, Standard: std, Citation: cit,
		})
	case ObligationTypeJobSecurity:
		pack.JobSecurityRules = append(pack.JobSecurityRules, JobSecurityRule{
			ID: o.ID, StandardKind: b.StandardKind, ProbationDays: b.ProbationDays,
			JustificationRequired: b.JustificationRequired, Standard: std, Citation: cit,
		})
	case ObligationTypeSeparationFiling:
		pack.SeparationFilings = append(pack.SeparationFilings, SeparationFilingRule{
			ID: o.ID, FormName: b.FormName, RecipientAuthority: b.RecipientAuthority,
			DeadlineDays: b.DeadlineDays, DayBasis: b.DayBasis, ContentFields: b.ContentFieldsFiled,
			Standard: std, Citation: cit,
		})
	case ObligationTypeDrugTesting:
		pack.DrugTestingRules = append(pack.DrugTestingRules, DrugTestingRule{
			ID: o.ID, PermittedBases: b.PermittedBases, WrittenPolicyRequired: b.WrittenPolicyRequired,
			AdvanceNoticeDays: b.AdvanceNoticeDays, ProtectedStatus: b.ProtectedStatus,
			Standard: std, Citation: cit,
		})
	case ObligationTypeBreachNotification:
		pack.BreachNotifications = append(pack.BreachNotifications, BreachNotificationRule{
			ID: o.ID, SubjectDeadlineDays: b.SubjectDeadlineDays, DayBasis: b.DayBasis,
			AuthorityThresholdCount: b.AuthorityThresholdCount, AuthorityDeadlineDays: b.AuthorityDeadlineDays,
			CreditMonitoringRequired: b.CreditMonitoringRequired, Standard: std, Citation: cit,
		})
	case ObligationTypeAutomatedDecision:
		pack.AutomatedDecisions = append(pack.AutomatedDecisions, AutomatedDecisionRule{
			ID: o.ID, CoveredUses: b.CoveredUses, BiasAuditRequired: b.BiasAuditRequired,
			AuditPeriodMonths: b.AuditPeriodMonths, CandidateNoticeDays: b.CandidateNoticeDays,
			DisclosureRequired: b.DisclosureRequired, Standard: std, Citation: cit,
		})
	case ObligationTypeMonitoringConsent:
		pack.MonitoringConsents = append(pack.MonitoringConsents, MonitoringConsentRule{
			ID: o.ID, DataCategories: b.DataCategories, ConsentForm: b.ConsentForm,
			RetentionLimitMonths: b.RetentionLimitMonths, DeletionDeadlineDays: b.DeletionDeadlineDays,
			Standard: std, Citation: cit,
		})
	default:
		return fmt.Errorf("obligation %q declares kind %s, which has no typed body", o.ID, o.Kind)
	}
	return nil
}

// PackDefinitionFrom renders a pack back into its definition-file shape. It
// is the inverse of [PackDefinition.Candidate] for every field the digest
// covers, which is what lets a test prove that loading a file and rendering
// it again is a fixed point.
func PackDefinitionFrom(pack RulePack, provenance ProvenanceJSON) PackDefinition {
	def := PackDefinition{
		SchemaVersion:     definitionSchemaVersion,
		PackID:            pack.PackID,
		Version:           PackVersion{Major: pack.Version, Minor: pack.MinorVersion},
		VocabularyVersion: uint32(pack.EffectiveVocabulary()),
		Jurisdiction:      jurisdictionJSONFrom(pack.Jurisdiction),
		Window:            WindowJSON{Start: pack.Window.Start.String()},
		SourceType:        pack.SourceType.String(),
		ReviewStatus:      pack.ReviewStatus.String(),
		Obligations:       []ObligationJSON{},
		Preemptions:       []PreemptionJSON{},
		Provenance:        provenance,
	}
	if pack.Window.HasEnd {
		def.Window.End = pack.Window.End.String()
	}
	for _, o := range pack.obligations() {
		def.Obligations = append(def.Obligations, ObligationJSON{
			Kind:     o.Type.String(),
			ID:       o.Rule.obligationID(),
			Citation: citationJSONFrom(o.Rule.obligationCitation()),
			Body:     bodyJSONFrom(o),
		})
	}
	for _, a := range pack.PreemptionAssertions {
		def.Preemptions = append(def.Preemptions, PreemptionJSON{
			Kind:     a.Kind.String(),
			Scope:    a.Scope,
			Citation: citationJSONFrom(a.Citation),
		})
	}
	if pack.Supersedes != nil {
		def.Supersedes = &PackReleaseRefJSON{
			PackID:       pack.Supersedes.PackID,
			Version:      PackVersion{Major: pack.Supersedes.Version, Minor: pack.Supersedes.MinorVersion},
			Jurisdiction: jurisdictionJSONFrom(pack.Supersedes.Jurisdiction),
		}
	}
	return def
}

func jurisdictionJSONFrom(j Jurisdiction) JurisdictionJSON {
	out := JurisdictionJSON{
		Country:      j.Country,
		Subdivision:  j.State,
		LocalityPath: []string{},
		Level:        "SUBDIVISION",
	}
	if j.Locality != "" {
		out.LocalityPath = []string{j.Locality}
		out.Level = "LOCALITY"
	} else if j.State == "" {
		out.Level = "COUNTRY"
	}
	return out
}

func citationJSONFrom(c Citation) CitationJSON {
	return CitationJSON{
		SourceFile:       c.SourceFile,
		Section:          c.Section,
		Note:             c.Note,
		ReviewStatus:     c.Status.String(),
		ConfidenceMarker: c.ConfidenceMarker.String(),
	}
}

func moneyJSONFrom(m values.Money) *MoneyJSON {
	if m.Validate() != nil {
		return nil
	}
	return &MoneyJSON{Amount: m.Amount().String(), Currency: m.Currency()}
}

// bodyJSONFrom renders one typed body back to the wire union.
//
//nolint:gocyclo // one arm per obligation kind, mirroring appendTo.
func bodyJSONFrom(o typedObligation) BodyJSON {
	switch v := o.Rule.(type) {
	case NoticeObligation:
		return BodyJSON{
			Who: v.Who, TimingDirection: v.TimingDirection, TimingDays: v.TimingDays,
			UnlessCondition: v.UnlessCondition, Channel: v.Channel, ContentFields: v.ContentFields,
		}
	case FieldRestriction:
		return BodyJSON{RestrictedFields: v.RestrictedFields, Context: v.Context}
	case RetentionRule:
		return BodyJSON{
			RecordClass: v.RecordClass, DurationYears: v.DurationYears,
			DurationBasis: v.DurationBasis, JurisdictionOverride: v.JurisdictionOverride,
		}
	case LeaveInteraction:
		return BodyJSON{LeaveType: v.LeaveType, InteractionRule: v.InteractionRule}
	case PayFrequencyConstraint:
		return BodyJSON{MinimumFrequency: v.MinimumFrequency, AppliesToWorkerClass: v.AppliesToWorkerClass}
	case FinalPayDeadline:
		return BodyJSON{Trigger: v.Trigger, DeadlineDescription: v.DeadlineDescription}
	case PayTransparencyDuty:
		return BodyJSON{Trigger: v.Trigger, RequiredDisclosure: v.RequiredDisclosure}
	case NonCompeteThreshold:
		return BodyJSON{RecheckOnPayChange: v.ReCheckOnPayChange, Rule: v.Rule}
	case EVerifyStatusCheck:
		return BodyJSON{RequiredOnNewHireOnly: v.RequiredOnNewHireOnly, Note: v.Note}
	case MiniWARNTrigger:
		return BodyJSON{
			Note: v.Note, EmployeeThreshold: v.EmployeeThreshold,
			LayoffWindowDays: v.LayoffWindowDays, NoticeDays: v.NoticeDays,
		}
	case WageFloorRule:
		return BodyJSON{
			FloorAmount: moneyJSONFrom(v.FloorAmount), WorkerClass: v.WorkerClass, Basis: v.Basis,
			Indexation: v.Indexation, NextAdjustmentDate: v.NextAdjustmentDate.String(),
			Standard: v.Standard.String(),
		}
	case PayEquityReviewRule:
		return BodyJSON{
			ProtectedBases: v.ProtectedBases, ComparatorStandard: v.ComparatorStandard,
			EmployerSizeFloor: v.EmployerSizeFloor, PermittedDifferentials: v.PermittedDifferentials,
			DocumentationRequired: v.DocumentationRequired, Standard: v.Standard.String(),
		}
	case PayStatementRule:
		return BodyJSON{
			RequiredFields: v.RequiredFields, Delivery: v.Delivery,
			ConsentRequired: v.ConsentRequired, Standard: v.Standard.String(),
		}
	case ClassificationRule:
		return BodyJSON{
			Dimension: v.Dimension, TestDescription: v.TestDescription,
			SalaryThreshold: moneyJSONFrom(v.SalaryThreshold), OvertimeTrigger: v.OvertimeTrigger,
			Standard: v.Standard.String(),
		}
	case PersonnelFileRule:
		return BodyJSON{
			ResponseDays: v.ResponseDays, FrequencyCapPerYear: v.FrequencyCapPerYear,
			CopyFeePermitted: v.CopyFeePermitted, DayBasis: v.DayBasis, Standard: v.Standard.String(),
		}
	case AntiRetaliationRule:
		return BodyJSON{
			ProtectedActivities: v.ProtectedActivities, LookbackDays: v.LookbackDays,
			Disposition: v.Disposition, Standard: v.Standard.String(),
		}
	case JobSecurityRule:
		return BodyJSON{
			StandardKind: v.StandardKind, ProbationDays: v.ProbationDays,
			JustificationRequired: v.JustificationRequired, Standard: v.Standard.String(),
		}
	case SeparationFilingRule:
		return BodyJSON{
			FormName: v.FormName, RecipientAuthority: v.RecipientAuthority,
			DeadlineDays: v.DeadlineDays, ContentFieldsFiled: v.ContentFields,
			DayBasis: v.DayBasis, Standard: v.Standard.String(),
		}
	case DrugTestingRule:
		return BodyJSON{
			PermittedBases: v.PermittedBases, WrittenPolicyRequired: v.WrittenPolicyRequired,
			AdvanceNoticeDays: v.AdvanceNoticeDays, ProtectedStatus: v.ProtectedStatus,
			Standard: v.Standard.String(),
		}
	case BreachNotificationRule:
		return BodyJSON{
			SubjectDeadlineDays: v.SubjectDeadlineDays, AuthorityThresholdCount: v.AuthorityThresholdCount,
			AuthorityDeadlineDays: v.AuthorityDeadlineDays, CreditMonitoringRequired: v.CreditMonitoringRequired,
			DayBasis: v.DayBasis, Standard: v.Standard.String(),
		}
	case AutomatedDecisionRule:
		return BodyJSON{
			CoveredUses: v.CoveredUses, BiasAuditRequired: v.BiasAuditRequired,
			AuditPeriodMonths: v.AuditPeriodMonths, CandidateNoticeDays: v.CandidateNoticeDays,
			DisclosureRequired: v.DisclosureRequired, Standard: v.Standard.String(),
		}
	case MonitoringConsentRule:
		return BodyJSON{
			DataCategories: v.DataCategories, ConsentForm: v.ConsentForm,
			RetentionLimitMonths: v.RetentionLimitMonths, DeletionDeadlineDays: v.DeletionDeadlineDays,
			Standard: v.Standard.String(),
		}
	default:
		return BodyJSON{}
	}
}

// MarshalPackDefinition renders a definition deterministically, in Prettier's
// own output shape (see prettyjson.go). The checked-in files therefore survive
// `npx prettier --write definitions/legal` untouched, which is what lets the
// regeneration test assert byte-identity against a formatted tree.
func MarshalPackDefinition(def PackDefinition) ([]byte, error) {
	return prettierJSON(def)
}
