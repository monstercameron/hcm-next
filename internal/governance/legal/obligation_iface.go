package legal

import (
	"fmt"
	"strings"
)

// This file wires all twenty-two typed bodies onto the obligationRule
// interface. The methods are mechanical on purpose: identity, citation and a
// one-line human description. Validation, canonical encoding and the trigger
// predicate live next to each type, in obligation.go, obligation_added.go and
// obligation_spec.go respectively.

func (o NoticeObligation) obligationID() string       { return o.ID }
func (o FieldRestriction) obligationID() string       { return o.ID }
func (o RetentionRule) obligationID() string          { return o.ID }
func (o LeaveInteraction) obligationID() string       { return o.ID }
func (o PayFrequencyConstraint) obligationID() string { return o.ID }
func (o FinalPayDeadline) obligationID() string       { return o.ID }
func (o PayTransparencyDuty) obligationID() string    { return o.ID }
func (o NonCompeteThreshold) obligationID() string    { return o.ID }
func (o EVerifyStatusCheck) obligationID() string     { return o.ID }
func (o MiniWARNTrigger) obligationID() string        { return o.ID }
func (o WageFloorRule) obligationID() string          { return o.ID }
func (o PayEquityReviewRule) obligationID() string    { return o.ID }
func (o PayStatementRule) obligationID() string       { return o.ID }
func (o ClassificationRule) obligationID() string     { return o.ID }
func (o PersonnelFileRule) obligationID() string      { return o.ID }
func (o AntiRetaliationRule) obligationID() string    { return o.ID }
func (o JobSecurityRule) obligationID() string        { return o.ID }
func (o SeparationFilingRule) obligationID() string   { return o.ID }
func (o DrugTestingRule) obligationID() string        { return o.ID }
func (o BreachNotificationRule) obligationID() string { return o.ID }
func (o AutomatedDecisionRule) obligationID() string  { return o.ID }
func (o MonitoringConsentRule) obligationID() string  { return o.ID }

func (o NoticeObligation) obligationCitation() Citation       { return o.Citation }
func (o FieldRestriction) obligationCitation() Citation       { return o.Citation }
func (o RetentionRule) obligationCitation() Citation          { return o.Citation }
func (o LeaveInteraction) obligationCitation() Citation       { return o.Citation }
func (o PayFrequencyConstraint) obligationCitation() Citation { return o.Citation }
func (o FinalPayDeadline) obligationCitation() Citation       { return o.Citation }
func (o PayTransparencyDuty) obligationCitation() Citation    { return o.Citation }
func (o NonCompeteThreshold) obligationCitation() Citation    { return o.Citation }
func (o EVerifyStatusCheck) obligationCitation() Citation     { return o.Citation }
func (o MiniWARNTrigger) obligationCitation() Citation        { return o.Citation }
func (o WageFloorRule) obligationCitation() Citation          { return o.Citation }
func (o PayEquityReviewRule) obligationCitation() Citation    { return o.Citation }
func (o PayStatementRule) obligationCitation() Citation       { return o.Citation }
func (o ClassificationRule) obligationCitation() Citation     { return o.Citation }
func (o PersonnelFileRule) obligationCitation() Citation      { return o.Citation }
func (o AntiRetaliationRule) obligationCitation() Citation    { return o.Citation }
func (o JobSecurityRule) obligationCitation() Citation        { return o.Citation }
func (o SeparationFilingRule) obligationCitation() Citation   { return o.Citation }
func (o DrugTestingRule) obligationCitation() Citation        { return o.Citation }
func (o BreachNotificationRule) obligationCitation() Citation { return o.Citation }
func (o AutomatedDecisionRule) obligationCitation() Citation  { return o.Citation }
func (o MonitoringConsentRule) obligationCitation() Citation  { return o.Citation }

// describe renders the one-line summary an [AppliedObligation] carries. The
// ten original wordings are preserved verbatim so LEGAL-001's evidence keeps
// reading the same.

func (o NoticeObligation) describe() string {
	return fmt.Sprintf("%s notice %s the effective date within %d day(s), channel=%s",
		o.Who, timingWord(o.TimingDirection), o.TimingDays, o.Channel)
}

func (o FieldRestriction) describe() string {
	return fmt.Sprintf("restricted fields %v in context %q", o.RestrictedFields, o.Context)
}

func (o RetentionRule) describe() string {
	return fmt.Sprintf("retain %s for %d year(s) (%s)", o.RecordClass, o.DurationYears, o.DurationBasis)
}

func (o LeaveInteraction) describe() string {
	return fmt.Sprintf("%s: %s", o.LeaveType, o.InteractionRule)
}

func (o PayFrequencyConstraint) describe() string {
	return fmt.Sprintf("minimum frequency %s for %s", o.MinimumFrequency, o.AppliesToWorkerClass)
}

func (o FinalPayDeadline) describe() string {
	return fmt.Sprintf("%s: %s", o.Trigger, o.DeadlineDescription)
}

func (o PayTransparencyDuty) describe() string {
	return fmt.Sprintf("%s: %s", o.Trigger, o.RequiredDisclosure)
}

func (o NonCompeteThreshold) describe() string { return o.Rule }

func (o EVerifyStatusCheck) describe() string { return o.Note }

func (o MiniWARNTrigger) describe() string {
	return fmt.Sprintf("%d+ affected within %d day(s) requires %d day(s) notice",
		o.EmployeeThreshold, o.LayoffWindowDays, o.NoticeDays)
}

func (o WageFloorRule) describe() string {
	return fmt.Sprintf("%s floor of %s for %s, indexation=%s%s",
		o.Basis, o.FloorAmount, o.WorkerClass, o.Indexation, standardSuffix(o.Standard))
}

func (o PayEquityReviewRule) describe() string {
	return fmt.Sprintf("pay-equity review against %s for bases %s%s",
		o.ComparatorStandard, strings.Join(o.ProtectedBases, ", "), standardSuffix(o.Standard))
}

func (o PayStatementRule) describe() string {
	return fmt.Sprintf("pay statement delivered %s must carry %s%s",
		o.Delivery, strings.Join(o.RequiredFields, ", "), standardSuffix(o.Standard))
}

func (o ClassificationRule) describe() string {
	return fmt.Sprintf("%s test: %s%s", o.Dimension, o.TestDescription, standardSuffix(o.Standard))
}

func (o PersonnelFileRule) describe() string {
	return fmt.Sprintf("personnel-file request answered within %d %s day(s)%s",
		o.ResponseDays, strings.ToLower(o.DayBasis), standardSuffix(o.Standard))
}

func (o AntiRetaliationRule) describe() string {
	return fmt.Sprintf("%s an adverse change within %d day(s) of %s%s",
		strings.ToLower(o.Disposition), o.LookbackDays, strings.Join(o.ProtectedActivities, ", "),
		standardSuffix(o.Standard))
}

func (o JobSecurityRule) describe() string {
	return fmt.Sprintf("job-security standard %s%s", o.StandardKind, standardSuffix(o.Standard))
}

func (o SeparationFilingRule) describe() string {
	return fmt.Sprintf("file %s with %s within %d %s day(s)%s",
		o.FormName, o.RecipientAuthority, o.DeadlineDays, strings.ToLower(o.DayBasis),
		standardSuffix(o.Standard))
}

func (o DrugTestingRule) describe() string {
	return fmt.Sprintf("testing permitted on %s%s",
		strings.Join(o.PermittedBases, ", "), standardSuffix(o.Standard))
}

func (o BreachNotificationRule) describe() string {
	return fmt.Sprintf("notify affected subjects within %d %s day(s)%s",
		o.SubjectDeadlineDays, strings.ToLower(o.DayBasis), standardSuffix(o.Standard))
}

func (o AutomatedDecisionRule) describe() string {
	return fmt.Sprintf("automated decision covering %s%s",
		strings.Join(o.CoveredUses, ", "), standardSuffix(o.Standard))
}

func (o MonitoringConsentRule) describe() string {
	return fmt.Sprintf("%s consent for %s%s",
		o.ConsentForm, strings.Join(o.DataCategories, ", "), standardSuffix(o.Standard))
}

// standardSuffix marks a rule the research stated as a recommendation, so a
// reader of an obligation description can never mistake it for a statutory
// requirement (contract section 7.1).
func standardSuffix(s RuleStandard) string {
	if s == RuleStandardRecommended {
		return " [RECOMMENDED, not a statutory requirement]"
	}
	return ""
}

// bindingDescriptions is the workflow-facing wording for each kind's primary
// binding. LEGAL-001's ten keep the exact strings they had.
var bindingDescriptions = map[ObligationType]string{
	ObligationTypeNotice:             "workflow node: send pay-rate-change notice",
	ObligationTypeFieldRestriction:   "field mask: forbid salary-history collection",
	ObligationTypeRetention:          "record-retention schedule",
	ObligationTypeLeaveInteraction:   "guard: preserve leave balance/accrual across the pay change",
	ObligationTypePayFrequency:       "guard: pay frequency floor",
	ObligationTypeFinalPayDeadline:   "timer: final pay deadline",
	ObligationTypePayTransparency:    "workflow node: pay-range disclosure",
	ObligationTypeNonCompete:         "human task: non-compete re-check",
	ObligationTypeEVerify:            "guard: work-authorization status check",
	ObligationTypeMiniWARN:           "timer: mini-WARN notice deadline",
	ObligationTypeWageFloor:          "guard: statutory wage floor",
	ObligationTypePayEquityReview:    "human task: pay-equity review of the proposed rate",
	ObligationTypePayStatement:       "workflow node: post-change pay statement",
	ObligationTypeClassification:     "guard and human task: classification test",
	ObligationTypePersonnelFile:      "workflow node: personnel-file access duty",
	ObligationTypeAntiRetaliation:    "guard and human task: protected-activity check",
	ObligationTypeJobSecurity:        "human task: job-security standard review",
	ObligationTypeSeparationFiling:   "workflow node: separation filing package (no transmission)",
	ObligationTypeDrugTesting:        "guard: drug-testing preconditions",
	ObligationTypeBreachNotification: "timer: breach notification deadline",
	ObligationTypeAutomatedDecision:  "guard and human task: automated-decision disclosure and audit",
	ObligationTypeMonitoringConsent:  "field mask: monitoring consent for covered data categories",
}
