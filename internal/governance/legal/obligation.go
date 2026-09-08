package legal

import (
	"errors"
	"fmt"
	"sort"
)

// Citation and obligation errors. All are matchable with errors.Is.
var (
	ErrCitationSourceFile = errors.New("legal: citation source file is required")
	ErrCitationSection    = errors.New("legal: citation statutory section is required")
	ErrCitationStatus     = errors.New("legal: citation review status is unspecified")
	ErrObligationID       = errors.New("legal: obligation id is required")
)

// ReviewStatus is the interpretation status of one cited rule. It exists so
// that no rule this package carries can be mistaken for counsel-approved
// content; see planning/specs/platform-architecture-catalog.md "Rule
// Provenance" and "Customer Counsel Control".
type ReviewStatus uint8

// Review statuses. Every seed pack in this package uses
// [ReviewStatusUnreviewed]: an agent drafted the underlying research, and no
// counsel has approved it as an interpretation this package may rely on.
const (
	// ReviewStatusUnspecified is the zero value and is never legal on a
	// registered citation.
	ReviewStatusUnspecified ReviewStatus = iota
	// ReviewStatusUnreviewed means the rule reflects drafted research with no
	// legal review. This is a fixture status, never a compliance claim.
	ReviewStatusUnreviewed
	// ReviewStatusCounselApproved means the customer's authorized legal team
	// reviewed and approved the interpretation. No pack in this package uses
	// it yet.
	ReviewStatusCounselApproved
)

var reviewStatusWire = map[ReviewStatus]string{
	ReviewStatusUnreviewed:                           "UNREVIEWED",
	ReviewStatusCounselApproved:                      "COUNSEL_APPROVED",
	ReviewStatusVendorBaseline:                       "VENDOR_BASELINE",
	ReviewStatusCustomerDefined:                      "CUSTOMER_DEFINED",
	ReviewStatusRequiresCustomerCounselConfiguration: "REQUIRES_CUSTOMER_COUNSEL_CONFIGURATION",
}

// String returns the stable wire token.
func (s ReviewStatus) String() string {
	if w, ok := reviewStatusWire[s]; ok {
		return w
	}
	return "REVIEW_STATUS_UNSPECIFIED"
}

// Citation records where one rule came from: a research file, the statutory
// section it summarizes, and whether counsel has reviewed the interpretation.
// Every obligation in this package carries one; a rule pack without citations
// on every rule is a layout the tests reject.
type Citation struct {
	// SourceFile is the repository-relative research file the rule was drawn
	// from, e.g. "planning/research/state-employment-law/california.md".
	SourceFile string
	// Section names the statutory or regulatory section, e.g. "Labor Code
	// § 2810.5".
	Section string
	// Note is a short, non-verbatim paraphrase of what the section requires.
	// It is not a quotation of the statute or of the research file.
	//
	// Note is deliberately outside the release digest (contract section
	// 3.2): rewrapping a research file or correcting a paraphrase must not
	// invalidate a signed release. Nothing in evaluation may ever read it.
	Note string
	// Status is the interpretation's review status.
	Status ReviewStatus
	// ConfidenceMarker records whether the citation was checked against the
	// primary source, is still to be verified, or is disputed between two
	// sources in the corpus. It is inside the digest.
	//
	// It is optional on a [VocabularyVersion1] pack, because LEGAL-001's
	// hand-built fixtures predate it; [Citation.ValidateForVocabulary]
	// requires it from vocabulary 2 onwards.
	ConfidenceMarker ConfidenceMarker
}

// Validate reports whether the citation is complete.
func (c Citation) Validate() error {
	if c.SourceFile == "" {
		return ErrCitationSourceFile
	}
	if c.Section == "" {
		return ErrCitationSection
	}
	if c.Status == ReviewStatusUnspecified {
		return ErrCitationStatus
	}
	return nil
}

// ValidateForVocabulary reports whether the citation is complete for a pack
// typed against vocabulary v. From [VocabularyVersion2] onwards a confidence
// marker is mandatory: LEGAL-011's rule is that every rule states how sure
// the pipeline is, and the absence of a marker is not "confident".
func (c Citation) ValidateForVocabulary(v VocabularyVersion) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if v >= VocabularyVersion2 && c.ConfidenceMarker == ConfidenceMarkerUnspecified {
		return fmt.Errorf("%w: %s %s", ErrConfidenceMarker, c.SourceFile, c.Section)
	}
	return nil
}

// ObligationType names which typed obligation shape an [AppliedObligation]
// carries. It exists so that evaluation output can be sorted, logged, and
// matched without a type switch at every call site.
type ObligationType uint8

// Obligation types. These are exactly the ten shapes LEGAL-001 defines for a
// promotion-and-base-pay-change rule pack.
const (
	ObligationTypeUnspecified ObligationType = iota
	ObligationTypeNotice
	ObligationTypeFieldRestriction
	ObligationTypeRetention
	ObligationTypeLeaveInteraction
	ObligationTypePayFrequency
	ObligationTypeFinalPayDeadline
	ObligationTypePayTransparency
	ObligationTypeNonCompete
	ObligationTypeEVerify
	ObligationTypeMiniWARN
)

// The twelve kinds LEGAL-011 adds, from the contract's section 4.2. They take
// fresh ordinals after the original ten, which keep theirs: an ordinal is
// part of the release digest through the kind's wire token, and the ten
// existing tokens and ordinals are frozen by the contract's section 4.1.
const (
	ObligationTypeWageFloor ObligationType = iota + 11
	ObligationTypePayEquityReview
	ObligationTypePayStatement
	ObligationTypeClassification
	ObligationTypePersonnelFile
	ObligationTypeAntiRetaliation
	ObligationTypeJobSecurity
	ObligationTypeSeparationFiling
	ObligationTypeDrugTesting
	ObligationTypeBreachNotification
	ObligationTypeAutomatedDecision
	ObligationTypeMonitoringConsent
)

var obligationTypeWire = map[ObligationType]string{
	ObligationTypeNotice:             "NOTICE",
	ObligationTypeFieldRestriction:   "FIELD_RESTRICTION",
	ObligationTypeRetention:          "RETENTION",
	ObligationTypeLeaveInteraction:   "LEAVE_INTERACTION",
	ObligationTypePayFrequency:       "PAY_FREQUENCY",
	ObligationTypeFinalPayDeadline:   "FINAL_PAY_DEADLINE",
	ObligationTypePayTransparency:    "PAY_TRANSPARENCY",
	ObligationTypeNonCompete:         "NON_COMPETE",
	ObligationTypeEVerify:            "E_VERIFY",
	ObligationTypeMiniWARN:           "MINI_WARN",
	ObligationTypeWageFloor:          "WAGE_FLOOR",
	ObligationTypePayEquityReview:    "PAY_EQUITY_REVIEW",
	ObligationTypePayStatement:       "PAY_STATEMENT",
	ObligationTypeClassification:     "CLASSIFICATION",
	ObligationTypePersonnelFile:      "PERSONNEL_FILE",
	ObligationTypeAntiRetaliation:    "ANTI_RETALIATION",
	ObligationTypeJobSecurity:        "JOB_SECURITY",
	ObligationTypeSeparationFiling:   "SEPARATION_FILING",
	ObligationTypeDrugTesting:        "DRUG_TESTING",
	ObligationTypeBreachNotification: "BREACH_NOTIFICATION",
	ObligationTypeAutomatedDecision:  "AUTOMATED_DECISION",
	ObligationTypeMonitoringConsent:  "MONITORING_CONSENT",
}

// String returns the stable wire token.
func (t ObligationType) String() string {
	if w, ok := obligationTypeWire[t]; ok {
		return w
	}
	return "OBLIGATION_TYPE_UNSPECIFIED"
}

// ParseObligationType maps a wire token back to a kind.
func ParseObligationType(token string) (ObligationType, error) {
	for t, w := range obligationTypeWire {
		if w == token {
			return t, nil
		}
	}
	return ObligationTypeUnspecified, fmt.Errorf("legal: unknown obligation kind %q", token)
}

// AllObligationTypes returns every kind in the vocabulary, in ordinal order.
// It is the completeness oracle's iteration order and the receipt's sort key,
// so it is derived from the wire table rather than restated by hand.
func AllObligationTypes() []ObligationType {
	out := make([]ObligationType, 0, len(obligationTypeWire))
	for t := range obligationTypeWire {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// VocabularyOf returns the vocabulary version that first declared t. It is
// how a receipt distinguishes "the author considered this kind and it did not
// apply" from "the author could not have considered this kind".
func VocabularyOf(t ObligationType) VocabularyVersion {
	if t >= ObligationTypeWageFloor {
		return VocabularyVersion2
	}
	return VocabularyVersion1
}

// NoticeObligation is a duty to notify someone, in a channel, within a
// declared timing window relative to the transaction's effective date, e.g.
// California Labor Code § 2810.5's 7-calendar-day post-change notice or New
// York Labor Law § 195(2)'s 7-day pre-change notice.
type NoticeObligation struct {
	ID string
	// Who names the notice direction, e.g. "employer_to_worker".
	Who string
	// TimingDirection is "BEFORE" or "AFTER" the effective date.
	TimingDirection string
	// TimingDays is the number of calendar days in the notice window.
	TimingDays int
	// UnlessCondition, when non-empty, names the condition under which the
	// timing requirement does not apply, e.g. "increase reflected on the next
	// regular pay statement".
	UnlessCondition string
	// Channel is how notice must be delivered, e.g. "written".
	Channel string
	// ContentFields lists what the notice must contain.
	ContentFields []string
	Citation      Citation
}

func (o NoticeObligation) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.TimingDirection != "" && o.TimingDirection != "BEFORE" && o.TimingDirection != "AFTER" {
		return fmt.Errorf("legal: notice %q has an unknown timing direction %q", o.ID, o.TimingDirection)
	}
	if o.TimingDays < 0 {
		return fmt.Errorf("legal: notice %q has a negative timing window", o.ID)
	}
	return o.Citation.Validate()
}

// FieldRestriction is a duty not to collect or use a named field for a
// declared purpose, e.g. California Labor Code § 432.3's and New York Labor
// Law § 194-a's salary-history bans.
type FieldRestriction struct {
	ID               string
	RestrictedFields []string
	Context          string
	Citation         Citation
}

func (o FieldRestriction) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	return o.Citation.Validate()
}

// RetentionRule binds one record class to a retention duration, e.g.
// California Labor Code § 432.3's 3-year wage/job-title history requirement
// or New York Labor Law § 195(4)'s 6-year records requirement.
type RetentionRule struct {
	ID            string
	RecordClass   string
	DurationYears int
	// DurationBasis names what the duration is measured from, e.g.
	// "employment_plus_years" or "from_record_date".
	DurationBasis string
	// JurisdictionOverride marks that this duration overrides a shorter
	// tenant- or platform-wide default retention policy.
	JurisdictionOverride bool
	Citation             Citation
}

func (o RetentionRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.DurationYears < 0 {
		return fmt.Errorf("legal: retention rule %q has a negative duration", o.ID)
	}
	return o.Citation.Validate()
}

// LeaveInteraction describes how a pay change interacts with a worker's
// protected leave, e.g. that accrued paid sick leave carries to a new role at
// its new rate without forfeiture.
type LeaveInteraction struct {
	ID              string
	LeaveType       string
	InteractionRule string
	Citation        Citation
}

func (o LeaveInteraction) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	return o.Citation.Validate()
}

// PayFrequencyConstraint names the minimum lawful pay frequency for a class
// of worker, e.g. California Labor Code § 204's semimonthly floor.
type PayFrequencyConstraint struct {
	ID                   string
	MinimumFrequency     string
	AppliesToWorkerClass string
	Citation             Citation
}

func (o PayFrequencyConstraint) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	return o.Citation.Validate()
}

// FinalPayDeadline names when final wages are due on separation, e.g.
// California Labor Code § 201's immediate-on-discharge rule.
type FinalPayDeadline struct {
	ID                  string
	Trigger             string
	DeadlineDescription string
	Citation            Citation
}

func (o FinalPayDeadline) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.Trigger == "" || o.DeadlineDescription == "" {
		return fmt.Errorf("legal: final pay deadline %q is missing trigger or deadline", o.ID)
	}
	return o.Citation.Validate()
}

// PayTransparencyDuty is a duty to disclose pay-range information, e.g.
// California Labor Code § 432.3's pay-scale-on-request rule or New York
// Labor Law § 194-b's internal-promotion wage-range disclosure.
type PayTransparencyDuty struct {
	ID                 string
	Trigger            string
	RequiredDisclosure string
	Citation           Citation
}

func (o PayTransparencyDuty) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.Trigger == "" || o.RequiredDisclosure == "" {
		return fmt.Errorf("legal: pay transparency duty %q is missing trigger or disclosure", o.ID)
	}
	return o.Citation.Validate()
}

// NonCompeteThreshold names a re-check required on a pay change, e.g.
// California's Business & Professions Code § 16600 voiding non-competes and
// requiring notice that the clause is void.
type NonCompeteThreshold struct {
	ID                 string
	ReCheckOnPayChange bool
	Rule               string
	Citation           Citation
}

func (o NonCompeteThreshold) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.Rule == "" {
		return fmt.Errorf("legal: non-compete threshold %q names no rule", o.ID)
	}
	return o.Citation.Validate()
}

// EVerifyStatusCheck names whether work-authorization verification status
// must be re-checked. No state pack seeded in this package populates one for
// a promotion (E-Verify is a hiring-time check), but the shape is part of the
// skeleton because a later hiring-adjacent rule pack needs it.
type EVerifyStatusCheck struct {
	ID                    string
	RequiredOnNewHireOnly bool
	Note                  string
	Citation              Citation
}

func (o EVerifyStatusCheck) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	return o.Citation.Validate()
}

// MiniWARNTrigger names a state mass-layoff notice threshold, e.g. New York
// Labor Law Article 25-A's 50-employee/25-layoff/90-day-notice rule. No state
// pack seeded in this package populates one for a single-worker promotion,
// but the shape is part of the skeleton because a workforce-reduction rule
// pack needs it.
type MiniWARNTrigger struct {
	ID                string
	EmployeeThreshold int
	LayoffWindowDays  int
	NoticeDays        int
	Note              string
	Citation          Citation
}

func (o MiniWARNTrigger) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	return o.Citation.Validate()
}

// ObligationBindingKind names how an obligation attaches to workflow
// execution, mirroring the vocabulary in
// planning/data/models/kernel-governance-and-evidence.md's ObligationBinding.
// This package does not integrate with internal/workflow; it only produces
// the typed binding fact for a later ticket to consume.
type ObligationBindingKind uint8

// Obligation binding kinds.
const (
	ObligationBindingKindUnspecified ObligationBindingKind = iota
	ObligationBindingKindNode
	ObligationBindingKindGuard
	ObligationBindingKindFieldMask
	ObligationBindingKindDestinationGate
	ObligationBindingKindHumanTask
	ObligationBindingKindTimer
	ObligationBindingKindChildIntent
)

var obligationBindingKindWire = map[ObligationBindingKind]string{
	ObligationBindingKindNode:            "NODE",
	ObligationBindingKindGuard:           "GUARD",
	ObligationBindingKindFieldMask:       "FIELD_MASK",
	ObligationBindingKindDestinationGate: "DESTINATION_GATE",
	ObligationBindingKindHumanTask:       "HUMAN_TASK",
	ObligationBindingKindTimer:           "TIMER",
	ObligationBindingKindChildIntent:     "CHILD_INTENT",
}

// String returns the stable wire token.
func (k ObligationBindingKind) String() string {
	if w, ok := obligationBindingKindWire[k]; ok {
		return w
	}
	return "OBLIGATION_BINDING_KIND_UNSPECIFIED"
}

// ObligationBinding is where an obligation attaches and whether it may be
// removed. Every binding this package produces is NonRemovable: every
// obligation type here derives from statute, and
// "[m]andatory legal effects cannot be removed by a workflow author, agent,
// subsidiary override, or ordinary administrator"
// (platform-architecture-catalog.md, "Legal Rule Effects").
type ObligationBinding struct {
	ObligationID string
	Kind         ObligationBindingKind
	NonRemovable bool
	Description  string
	// LifecycleStep names the transaction-lifecycle step this binding
	// attaches to. The contract's sections 4.1 and 4.2 assign the steps and
	// [ObligationKindSpec] is the only place the assignment lives, so an
	// obligation can never bind to a step the contract does not give its
	// kind.
	LifecycleStep LifecycleStep
}

// Validate reports whether the binding names a known step and binding kind
// and is non-removable. Every binding this package produces is statutory, so
// a removable one is a construction bug rather than a configuration choice.
func (b ObligationBinding) Validate() error {
	if b.ObligationID == "" {
		return ErrObligationID
	}
	if b.Kind == ObligationBindingKindUnspecified {
		return fmt.Errorf("legal: obligation %q binding names no binding kind", b.ObligationID)
	}
	if !validLifecycleSteps[b.LifecycleStep] {
		return fmt.Errorf("legal: obligation %q binds to unknown lifecycle step %q", b.ObligationID, b.LifecycleStep)
	}
	if !b.NonRemovable {
		return fmt.Errorf("legal: obligation %q produced a removable binding", b.ObligationID)
	}
	return nil
}

// AppliedObligation is one obligation [Evaluate] found applicable to a
// proposal, together with its citation and how it binds.
type AppliedObligation struct {
	Type         ObligationType
	ID           string
	Description  string
	Citation     Citation
	Jurisdiction Jurisdiction
	PackID       string
	PackVersion  uint32
	// Binding is the primary binding, kept for LEGAL-001 callers that
	// predate multi-step binding. It is always Bindings[0].
	Binding ObligationBinding
	// Bindings is every (lifecycle step, binding kind) pair the contract
	// assigns this obligation's kind. A kind such as CLASSIFICATION binds at
	// PREFLIGHT as a GUARD and at APPROVAL as a HUMAN_TASK, and dropping
	// either one would silently delete a statutory effect.
	Bindings []ObligationBinding
}

// NotApplicableReason names why an obligation's trigger predicate evaluated
// false. It is the fact that made the trigger false, not an explanation of
// the rule: the contract's section 4.4 makes silence illegal, so every
// declared obligation is answered either way.
type NotApplicableReason string

// ConsideredObligation is one obligation whose trigger predicate evaluated
// false, recorded as CONSIDERED_NOT_APPLICABLE. Its presence is what makes an
// evaluation receipt an audit artifact rather than a list of hits.
type ConsideredObligation struct {
	Type ObligationType
	ID   string
	// Reason is the proposal fact that made the trigger false.
	Reason   NotApplicableReason
	Citation Citation
}

// NotConsideredKind is one obligation kind a release could not have answered,
// because the release was typed against an older vocabulary than this engine
// knows. It is never the same thing as "the kind does not apply".
type NotConsideredKind struct {
	Type ObligationType
	// PackID and PackVersion name the release whose vocabulary predates the
	// kind.
	PackID      string
	PackVersion uint32
	// ReleaseVocabulary is the vocabulary the release declared.
	ReleaseVocabulary VocabularyVersion
}
