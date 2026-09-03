package legal

import "strings"

// This file holds the two tables LEGAL-011 turns on: the canonical body
// encoding for the original ten kinds (the twelve added ones carry their own
// in obligation_added.go), and the single [ObligationKindSpec] registry that
// says, for every kind in the vocabulary, what its trigger means and which
// lifecycle steps it binds to.
//
// The registry exists so that no call site switches on kind to decide a
// binding. The contract assigns the steps in sections 4.1 and 4.2; if a
// binding is wrong it is wrong in exactly one place.

// BindingSpec pairs one lifecycle step with the binding kind the contract
// assigns the obligation at that step.
type BindingSpec struct {
	Step LifecycleStep
	Kind ObligationBindingKind
}

// ObligationKindSpec is the contract's row for one obligation kind.
type ObligationKindSpec struct {
	Type ObligationType
	// Vocabulary is the vocabulary version that first declared the kind.
	Vocabulary VocabularyVersion
	// Trigger is the contract's trigger predicate, in words. The executable
	// predicate lives on the typed body, because most triggers read body
	// fields (a named leave program, a lookback window, a layoff threshold).
	Trigger string
	// Bindings is every step the kind binds to, in lifecycle order. It is
	// never empty and every binding it produces is non-removable.
	Bindings []BindingSpec
}

// obligationKindSpecs is the contract's sections 4.1 and 4.2, transcribed
// once. Where a kind lists more lifecycle steps than binding kinds, the
// pairing below is the one the contract's own wording implies: a NOTICE is a
// workflow node while the transaction is in flight and a delivery-deadline
// timer once it has committed; a CLASSIFICATION guards preflight and becomes
// a human task at approval, because the platform surfaces the test and never
// decides it (contract section 10, non-goal 3).
var obligationKindSpecs = map[ObligationType]ObligationKindSpec{
	ObligationTypeNotice: {
		Type: ObligationTypeNotice, Vocabulary: VocabularyVersion1,
		Trigger: "base pay rate changed",
		Bindings: []BindingSpec{
			{LifecycleStepSimulate, ObligationBindingKindNode},
			{LifecycleStepExecute, ObligationBindingKindNode},
			{LifecycleStepPostCommit, ObligationBindingKindTimer},
		},
	},
	ObligationTypeFieldRestriction: {
		Type: ObligationTypeFieldRestriction, Vocabulary: VocabularyVersion1,
		Trigger:  "compensation-setting consulted a restricted field",
		Bindings: []BindingSpec{{LifecycleStepDraft, ObligationBindingKindFieldMask}},
	},
	ObligationTypeRetention: {
		Type: ObligationTypeRetention, Vocabulary: VocabularyVersion1,
		Trigger:  "unconditional",
		Bindings: []BindingSpec{{LifecycleStepPostCommit, ObligationBindingKindNode}},
	},
	ObligationTypeLeaveInteraction: {
		Type: ObligationTypeLeaveInteraction, Vocabulary: VocabularyVersion1,
		Trigger: "worker holds a balance in a named program",
		Bindings: []BindingSpec{
			{LifecycleStepPreflight, ObligationBindingKindGuard},
			{LifecycleStepExecute, ObligationBindingKindGuard},
		},
	},
	ObligationTypePayFrequency: {
		Type: ObligationTypePayFrequency, Vocabulary: VocabularyVersion1,
		Trigger:  "unconditional",
		Bindings: []BindingSpec{{LifecycleStepPreflight, ObligationBindingKindGuard}},
	},
	ObligationTypeFinalPayDeadline: {
		Type: ObligationTypeFinalPayDeadline, Vocabulary: VocabularyVersion1,
		Trigger:  "concurrent separation",
		Bindings: []BindingSpec{{LifecycleStepExecute, ObligationBindingKindTimer}},
	},
	ObligationTypePayTransparency: {
		Type: ObligationTypePayTransparency, Vocabulary: VocabularyVersion1,
		Trigger: "internal promotion",
		Bindings: []BindingSpec{
			{LifecycleStepDraft, ObligationBindingKindNode},
			{LifecycleStepPreflight, ObligationBindingKindNode},
		},
	},
	ObligationTypeNonCompete: {
		Type: ObligationTypeNonCompete, Vocabulary: VocabularyVersion1,
		Trigger: "worker has an existing covenant",
		Bindings: []BindingSpec{
			{LifecycleStepSimulate, ObligationBindingKindHumanTask},
			{LifecycleStepApproval, ObligationBindingKindHumanTask},
			{LifecycleStepPostCommit, ObligationBindingKindHumanTask},
		},
	},
	ObligationTypeEVerify: {
		Type: ObligationTypeEVerify, Vocabulary: VocabularyVersion1,
		Trigger:  "new hire",
		Bindings: []BindingSpec{{LifecycleStepDraft, ObligationBindingKindGuard}},
	},
	ObligationTypeMiniWARN: {
		Type: ObligationTypeMiniWARN, Vocabulary: VocabularyVersion1,
		Trigger:  "concurrent reduction at or above threshold",
		Bindings: []BindingSpec{{LifecycleStepExecute, ObligationBindingKindTimer}},
	},
	ObligationTypeWageFloor: {
		Type: ObligationTypeWageFloor, Vocabulary: VocabularyVersion2,
		Trigger: "unconditional",
		Bindings: []BindingSpec{
			{LifecycleStepPreflight, ObligationBindingKindGuard},
			{LifecycleStepExecute, ObligationBindingKindGuard},
		},
	},
	ObligationTypePayEquityReview: {
		Type: ObligationTypePayEquityReview, Vocabulary: VocabularyVersion2,
		Trigger: "base pay rate changed",
		Bindings: []BindingSpec{
			{LifecycleStepSimulate, ObligationBindingKindHumanTask},
			{LifecycleStepApproval, ObligationBindingKindHumanTask},
		},
	},
	ObligationTypePayStatement: {
		Type: ObligationTypePayStatement, Vocabulary: VocabularyVersion2,
		Trigger:  "base pay rate changed",
		Bindings: []BindingSpec{{LifecycleStepPostCommit, ObligationBindingKindNode}},
	},
	ObligationTypeClassification: {
		Type: ObligationTypeClassification, Vocabulary: VocabularyVersion2,
		Trigger: "role, hours, pay basis or pay rate changed",
		Bindings: []BindingSpec{
			{LifecycleStepPreflight, ObligationBindingKindGuard},
			{LifecycleStepApproval, ObligationBindingKindHumanTask},
		},
	},
	ObligationTypePersonnelFile: {
		Type: ObligationTypePersonnelFile, Vocabulary: VocabularyVersion2,
		Trigger:  "unconditional",
		Bindings: []BindingSpec{{LifecycleStepPostCommit, ObligationBindingKindNode}},
	},
	ObligationTypeAntiRetaliation: {
		Type: ObligationTypeAntiRetaliation, Vocabulary: VocabularyVersion2,
		Trigger: "a protected activity is recorded inside the lookback",
		Bindings: []BindingSpec{
			{LifecycleStepPreflight, ObligationBindingKindGuard},
			{LifecycleStepApproval, ObligationBindingKindHumanTask},
		},
	},
	ObligationTypeJobSecurity: {
		Type: ObligationTypeJobSecurity, Vocabulary: VocabularyVersion2,
		Trigger:  "adverse change (pay decrease, demotion, separation)",
		Bindings: []BindingSpec{{LifecycleStepApproval, ObligationBindingKindHumanTask}},
	},
	ObligationTypeSeparationFiling: {
		Type: ObligationTypeSeparationFiling, Vocabulary: VocabularyVersion2,
		Trigger:  "concurrent separation",
		Bindings: []BindingSpec{{LifecycleStepPostCommit, ObligationBindingKindNode}},
	},
	ObligationTypeDrugTesting: {
		Type: ObligationTypeDrugTesting, Vocabulary: VocabularyVersion2,
		Trigger:  "role becomes safety-sensitive, or a test is ordered",
		Bindings: []BindingSpec{{LifecycleStepPreflight, ObligationBindingKindGuard}},
	},
	ObligationTypeBreachNotification: {
		Type: ObligationTypeBreachNotification, Vocabulary: VocabularyVersion2,
		Trigger:  "a personal-data breach incident is opened",
		Bindings: []BindingSpec{{LifecycleStepPostCommit, ObligationBindingKindTimer}},
	},
	ObligationTypeAutomatedDecision: {
		Type: ObligationTypeAutomatedDecision, Vocabulary: VocabularyVersion2,
		Trigger: "a model scored, ranked or recommended the subject",
		Bindings: []BindingSpec{
			{LifecycleStepDraft, ObligationBindingKindGuard},
			{LifecycleStepApproval, ObligationBindingKindHumanTask},
		},
	},
	ObligationTypeMonitoringConsent: {
		Type: ObligationTypeMonitoringConsent, Vocabulary: VocabularyVersion2,
		Trigger:  "the transaction reads or writes a covered data category",
		Bindings: []BindingSpec{{LifecycleStepDraft, ObligationBindingKindFieldMask}},
	},
}

// ObligationKindSpecFor returns the contract's row for t.
func ObligationKindSpecFor(t ObligationType) (ObligationKindSpec, bool) {
	spec, ok := obligationKindSpecs[t]
	return spec, ok
}

// bindingsFor builds the non-removable [ObligationBinding] set for one
// obligation. The first binding carries description, so LEGAL-001 callers
// reading only AppliedObligation.Binding keep seeing what they saw.
func bindingsFor(t ObligationType, obligationID, description string) []ObligationBinding {
	spec, ok := obligationKindSpecs[t]
	if !ok {
		return nil
	}
	out := make([]ObligationBinding, 0, len(spec.Bindings))
	for _, b := range spec.Bindings {
		out = append(out, ObligationBinding{
			ObligationID:  obligationID,
			Kind:          b.Kind,
			NonRemovable:  true,
			Description:   description,
			LifecycleStep: b.Step,
		})
	}
	return out
}

// --- trigger predicates -----------------------------------------------------
//
// Every predicate below is a pure function of the proposal snapshot and the
// obligation's own typed body. None of them reads the registry, a clock, or
// package state, which is what makes an evaluation replayable from a receipt.
// Each returns the fact that made it false, because the contract's section
// 4.4 forbids silence: an obligation whose trigger is false is recorded as
// CONSIDERED_NOT_APPLICABLE with that fact.

const (
	reasonPayRateUnchanged  NotApplicableReason = "base pay rate did not change on this proposal"
	reasonNoSalaryHistory   NotApplicableReason = "compensation-setting did not consult a restricted field"
	reasonNoLeaveBalance    NotApplicableReason = "worker holds no balance in a leave program this rule names"
	reasonNoSeparation      NotApplicableReason = "the transaction does not concurrently separate the worker"
	reasonNotPromotion      NotApplicableReason = "the transaction is not an internal promotion"
	reasonNoCovenant        NotApplicableReason = "the worker is not subject to an existing covenant"
	reasonNotNewHire        NotApplicableReason = "the transaction is not a new hire"
	reasonBelowWARN         NotApplicableReason = "the concurrent workforce reduction is below this rule's employee threshold"
	reasonNoProtectedAct    NotApplicableReason = "no protected activity is recorded inside this rule's lookback window"
	reasonNoAdverseChange   NotApplicableReason = "the transaction is not an adverse change (no pay decrease, demotion or separation)"
	reasonNoTestTrigger     NotApplicableReason = "the role does not become safety-sensitive and no test was ordered"
	reasonNoBreach          NotApplicableReason = "no personal-data breach incident is open on this transaction"
	reasonNoModel           NotApplicableReason = "no model scored, ranked or recommended the subject"
	reasonNoCoveredCategory NotApplicableReason = "the transaction touches no data category this rule covers"
	reasonNoClassChange     NotApplicableReason = "role, hours, pay basis and pay rate are all unchanged"
)

func (o NoticeObligation) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.payRateChanged(), reasonPayRateUnchanged
}

func (o FieldRestriction) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.CollectsSalaryHistory, reasonNoSalaryHistory
}

func (o RetentionRule) trigger(PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return true, ""
}

// trigger implements the contract's section 4.1 correction. LEAVE_INTERACTION
// used to fire only when the worker was already on protected leave, which
// missed the accrual-carry rule twenty-eight states impose on *every*
// promotion. It now fires when the worker holds a balance in a leave program
// this rule names. OnProtectedLeave stays a fact — it selects the
// job-restoration sub-rule downstream, and it still fires the trigger on its
// own, because a worker on a protected leave is by construction inside a
// mandated program even when the caller did not enumerate balances.
func (o LeaveInteraction) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	if p.OnProtectedLeave {
		return true, ""
	}
	return p.holdsLeaveBalanceIn(o.LeaveType), reasonNoLeaveBalance
}

func (o PayFrequencyConstraint) trigger(PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return true, ""
}

func (o FinalPayDeadline) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.SeparationConcurrent, reasonNoSeparation
}

func (o PayTransparencyDuty) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.IsInternalPromotion, reasonNotPromotion
}

func (o NonCompeteThreshold) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.HasExistingNonCompete, reasonNoCovenant
}

func (o EVerifyStatusCheck) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.IsNewHire, reasonNotNewHire
}

func (o MiniWARNTrigger) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.WorkforceReductionCount >= o.EmployeeThreshold, reasonBelowWARN
}

func (o WageFloorRule) trigger(PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return true, ""
}

func (o PayEquityReviewRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.payRateChanged(), reasonPayRateUnchanged
}

func (o PayStatementRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.payRateChanged(), reasonPayRateUnchanged
}

func (o ClassificationRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	fired := p.RoleChanged || p.HoursChanged || p.PayBasisChanged || p.payRateChanged()
	return fired, reasonNoClassChange
}

func (o PersonnelFileRule) trigger(PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return true, ""
}

// trigger fires when any protected activity is recorded inside this rule's
// lookback window. Matching the activity's kind against the rule's enumerated
// activities is deliberately not done here: the corpus names protected
// activities in fifty different vocabularies, and narrowing on an unmatched
// string would drop a real retaliation check. The enumerated list travels to
// the human task instead.
func (o AntiRetaliationRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	for _, a := range p.RecordedProtectedActivities {
		if a.DaysBeforeEffectiveDate >= 0 && a.DaysBeforeEffectiveDate <= o.LookbackDays {
			return true, ""
		}
	}
	return false, reasonNoProtectedAct
}

func (o JobSecurityRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.isAdverseChange(), reasonNoAdverseChange
}

func (o SeparationFilingRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.SeparationConcurrent, reasonNoSeparation
}

func (o DrugTestingRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.RoleBecomesSafetySensitive || p.DrugTestOrdered, reasonNoTestTrigger
}

func (o BreachNotificationRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.BreachIncidentOpened, reasonNoBreach
}

func (o AutomatedDecisionRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	return p.AutomatedDecisionApplied, reasonNoModel
}

func (o MonitoringConsentRule) trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason) {
	for _, want := range o.DataCategories {
		for _, got := range p.DataCategoriesTouched {
			if strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(got)) {
				return true, ""
			}
		}
	}
	return false, reasonNoCoveredCategory
}

// --- canonical bodies for the original ten kinds ----------------------------

func (o NoticeObligation) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "who", o.Who)
	dst = appendField(dst, "timing_direction", o.TimingDirection)
	dst = appendUint32Field(dst, "timing_days", uint32(o.TimingDays))
	dst = appendField(dst, "unless_condition", o.UnlessCondition)
	dst = appendField(dst, "channel", o.Channel)
	return appendStringSlice(dst, "content_fields", o.ContentFields)
}

func (o FieldRestriction) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendStringSlice(dst, "restricted_fields", o.RestrictedFields)
	return appendField(dst, "context", o.Context)
}

func (o RetentionRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "record_class", o.RecordClass)
	dst = appendUint32Field(dst, "duration_years", uint32(o.DurationYears))
	dst = appendField(dst, "duration_basis", o.DurationBasis)
	return appendFieldBool(dst, "jurisdiction_override", o.JurisdictionOverride)
}

func (o LeaveInteraction) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "leave_type", o.LeaveType)
	return appendField(dst, "interaction_rule", o.InteractionRule)
}

func (o PayFrequencyConstraint) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "minimum_frequency", o.MinimumFrequency)
	return appendField(dst, "applies_to_worker_class", o.AppliesToWorkerClass)
}

func (o FinalPayDeadline) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "trigger", o.Trigger)
	return appendField(dst, "deadline_description", o.DeadlineDescription)
}

func (o PayTransparencyDuty) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "trigger", o.Trigger)
	return appendField(dst, "required_disclosure", o.RequiredDisclosure)
}

func (o NonCompeteThreshold) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendFieldBool(dst, "recheck_on_pay_change", o.ReCheckOnPayChange)
	return appendField(dst, "rule", o.Rule)
}

func (o EVerifyStatusCheck) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendFieldBool(dst, "required_on_new_hire_only", o.RequiredOnNewHireOnly)
	return appendField(dst, "note", o.Note)
}

func (o MiniWARNTrigger) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendUint32Field(dst, "employee_threshold", uint32(o.EmployeeThreshold))
	dst = appendUint32Field(dst, "layoff_window_days", uint32(o.LayoffWindowDays))
	dst = appendUint32Field(dst, "notice_days", uint32(o.NoticeDays))
	return appendField(dst, "note", o.Note)
}
