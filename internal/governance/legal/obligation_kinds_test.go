package legal

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// This file holds LEGAL-011's test matrix. Its backlog TEST field is
// TestTodo_LEGAL_011; see the note at the top of packrelease_test.go about
// the one-step offset between ticket ids and test names in section 20 of
// planning/todos.md.

// vocabularyTwoRegistry registers one pack carrying exactly one obligation of
// every kind in the vocabulary, so a single evaluation exercises all
// twenty-two trigger predicates.
func vocabularyTwoRegistry(t *testing.T) (*Registry, RulePack) {
	t.Helper()
	pack := allKindsPack(t)
	reg := NewRegistry()
	if err := reg.Register(pack); err != nil {
		t.Fatalf("Register(all kinds): %v", err)
	}
	return reg, pack
}

func allKindsCitation() Citation {
	return Citation{
		SourceFile:       californiaSourceFile,
		Section:          "fixture section",
		Note:             "fixture paraphrase; outside the digest by design",
		Status:           ReviewStatusUnreviewed,
		ConfidenceMarker: ConfidenceMarkerVerify,
	}
}

func allKindsPack(t *testing.T) RulePack {
	t.Helper()
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		t.Fatalf("NewLocalDate: %v", err)
	}
	window, err := NewOpenEffectiveWindow(start)
	if err != nil {
		t.Fatalf("NewOpenEffectiveWindow: %v", err)
	}
	floor, err := values.NewMoney("15.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	c := allKindsCitation()
	return RulePack{
		PackID:            "us-zz-all-kinds",
		Version:           1,
		VocabularyVersion: VocabularyVersion2,
		SourceType:        SourceTypeStatute,
		ReviewStatus:      ReviewStatusUnreviewed,
		Jurisdiction:      Jurisdiction{Country: "US", State: "ZZ"},
		Window:            window,

		Notices: []NoticeObligation{{
			ID: "zz-notice", Who: "employer_to_worker", TimingDirection: "AFTER",
			TimingDays: 7, Channel: "written", ContentFields: []string{"pay_rate"}, Citation: c,
		}},
		FieldRestrictions: []FieldRestriction{{
			ID: "zz-field-restriction", RestrictedFields: []string{"salary_history"},
			Context: "setting compensation", Citation: c,
		}},
		RetentionRules: []RetentionRule{{
			ID: "zz-retention", RecordClass: "payroll_records", DurationYears: 3,
			DurationBasis: "from_record_date", Citation: c,
		}},
		LeaveInteractions: []LeaveInteraction{{
			ID: "zz-leave", LeaveType: "accrued paid sick leave",
			InteractionRule: "the balance carries to the new role", Citation: c,
		}},
		PayFrequencyConstraints: []PayFrequencyConstraint{{
			ID: "zz-pay-frequency", MinimumFrequency: "SEMIMONTHLY",
			AppliesToWorkerClass: "covered employees", Citation: c,
		}},
		FinalPayDeadlines: []FinalPayDeadline{{
			ID: "zz-final-pay", Trigger: "termination_any",
			DeadlineDescription: "due on the next regular payday", Citation: c,
		}},
		PayTransparencyDuties: []PayTransparencyDuty{{
			ID: "zz-pay-transparency", Trigger: "internal_promotion",
			RequiredDisclosure: "wage range for the role", Citation: c,
		}},
		NonCompeteThresholds: []NonCompeteThreshold{{
			ID: "zz-non-compete", ReCheckOnPayChange: true,
			Rule: "re-check the covenant on a material pay change", Citation: c,
		}},
		EVerifyChecks: []EVerifyStatusCheck{{
			ID: "zz-e-verify", RequiredOnNewHireOnly: true, Note: "new-hire check", Citation: c,
		}},
		MiniWARNTriggers: []MiniWARNTrigger{{
			ID: "zz-mini-warn", EmployeeThreshold: 25, LayoffWindowDays: 30, NoticeDays: 90,
			Note: "mass-layoff notice", Citation: c,
		}},

		WageFloors: []WageFloorRule{{
			ID: "zz-wage-floor", FloorAmount: floor, WorkerClass: "covered employees",
			Basis: "HOURLY", Indexation: "CPI", Standard: RuleStandardRequired, Citation: c,
		}},
		PayEquityReviews: []PayEquityReviewRule{{
			ID: "zz-pay-equity", ProtectedBases: []string{"sex"},
			ComparatorStandard: "substantially similar work", DocumentationRequired: true,
			Standard: RuleStandardRequired, Citation: c,
		}},
		PayStatements: []PayStatementRule{{
			ID: "zz-pay-statement", RequiredFields: []string{"gross_wages"}, Delivery: "EITHER",
			Standard: RuleStandardRequired, Citation: c,
		}},
		Classifications: []ClassificationRule{{
			ID: "zz-classification", Dimension: "EXEMPTION", TestDescription: "duties and salary test",
			Standard: RuleStandardRequired, Citation: c,
		}},
		PersonnelFileRules: []PersonnelFileRule{{
			ID: "zz-personnel-file", ResponseDays: 30, DayBasis: "CALENDAR",
			Standard: RuleStandardRequired, Citation: c,
		}},
		AntiRetaliationRules: []AntiRetaliationRule{{
			ID: "zz-anti-retaliation", ProtectedActivities: []string{"wage_claim"},
			LookbackDays: 90, Disposition: "FLAG", Standard: RuleStandardRequired, Citation: c,
		}},
		JobSecurityRules: []JobSecurityRule{{
			ID: "zz-job-security", StandardKind: "GOOD_CAUSE_AFTER_PROBATION", ProbationDays: 180,
			JustificationRequired: true, Standard: RuleStandardRequired, Citation: c,
		}},
		SeparationFilings: []SeparationFilingRule{{
			ID: "zz-separation-filing", FormName: "BC-10", RecipientAuthority: "state unemployment insurance agency",
			DeadlineDays: 5, DayBasis: "BUSINESS", ContentFields: []string{"separation_reason"},
			Standard: RuleStandardRequired, Citation: c,
		}},
		DrugTestingRules: []DrugTestingRule{{
			ID: "zz-drug-testing", PermittedBases: []string{"reasonable_suspicion"},
			WrittenPolicyRequired: true, Standard: RuleStandardRequired, Citation: c,
		}},
		BreachNotifications: []BreachNotificationRule{{
			ID: "zz-breach", SubjectDeadlineDays: 45, DayBasis: "CALENDAR",
			Standard: RuleStandardRequired, Citation: c,
		}},
		AutomatedDecisions: []AutomatedDecisionRule{{
			ID: "zz-automated-decision", CoveredUses: []string{"promotion_decision"},
			BiasAuditRequired: true, Standard: RuleStandardRequired, Citation: c,
		}},
		MonitoringConsents: []MonitoringConsentRule{{
			ID: "zz-monitoring-consent", DataCategories: []string{"biometric_identifiers"},
			ConsentForm: "WRITTEN", Standard: RuleStandardRequired, Citation: c,
		}},
	}
}

// zzContext resolves a signed context pinned to the all-kinds pack.
func zzContext(t *testing.T, reg *Registry) *LegalContext {
	t.Helper()
	j := Jurisdiction{Country: "US", State: "ZZ"}
	ctx, err := Resolve(validInput(t, j), reg, fixedSigner(t, 0x41), mustInstant(t, 1_770_800_000))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return ctx
}

// everythingFiresProposal turns on every fact the twenty-two triggers read.
func everythingFiresProposal(t *testing.T) PromotionProposalSnapshot {
	t.Helper()
	current, err := values.NewMoney("8000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	next, err := values.NewMoney("9500.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return PromotionProposalSnapshot{
		WorkerID:                    "worker-all-kinds",
		LegalEntityID:               "legal-entity-1",
		EffectiveDate:               mustDate(t, 2026, time.March, 1),
		CurrentBasePay:              current,
		NewBasePay:                  next,
		PayFrequency:                "SEMIMONTHLY",
		IsInternalPromotion:         true,
		CollectsSalaryHistory:       true,
		HasExistingNonCompete:       true,
		IsNewHire:                   true,
		SeparationConcurrent:        true,
		WorkforceReductionCount:     30,
		LeaveProgramBalances:        []string{"accrued paid sick leave"},
		RoleChanged:                 true,
		HoursChanged:                true,
		PayBasisChanged:             true,
		IsDemotion:                  true,
		RecordedProtectedActivities: []RecordedProtectedActivity{{Kind: "wage_claim", DaysBeforeEffectiveDate: 30}},
		RoleBecomesSafetySensitive:  true,
		DrugTestOrdered:             true,
		BreachIncidentOpened:        true,
		AutomatedDecisionApplied:    true,
		DataCategoriesTouched:       []string{"biometric_identifiers"},
	}
}

// --- TestTodo_LEGAL_011 (PRIMARY) -------------------------------------------

// TestTodo_LEGAL_011 is the contract's section 4 in executable form: twenty-two
// kinds, each with a typed body, a complete citation carrying a confidence
// marker, a non-removable binding at the lifecycle step the contract assigns,
// and an answer in exactly one of applied or CONSIDERED_NOT_APPLICABLE.
func TestTodo_LEGAL_011(t *testing.T) {
	t.Run("the vocabulary is twenty-two kinds and the original ten did not move", func(t *testing.T) {
		kinds := AllObligationTypes()
		if len(kinds) != 22 {
			t.Fatalf("vocabulary has %d kinds, want 22", len(kinds))
		}
		// The contract's section 4.1 freezes the ten existing wire tokens and
		// ordinal values.
		frozen := map[ObligationType]string{
			1: "NOTICE", 2: "FIELD_RESTRICTION", 3: "RETENTION", 4: "LEAVE_INTERACTION",
			5: "PAY_FREQUENCY", 6: "FINAL_PAY_DEADLINE", 7: "PAY_TRANSPARENCY",
			8: "NON_COMPETE", 9: "E_VERIFY", 10: "MINI_WARN",
		}
		for ordinal, token := range frozen {
			if got := ordinal.String(); got != token {
				t.Errorf("ordinal %d is %s, want the frozen token %s", ordinal, got, token)
			}
			if VocabularyOf(ordinal) != VocabularyVersion1 {
				t.Errorf("%s moved out of vocabulary 1", token)
			}
		}
		added := map[ObligationType]string{
			11: "WAGE_FLOOR", 12: "PAY_EQUITY_REVIEW", 13: "PAY_STATEMENT", 14: "CLASSIFICATION",
			15: "PERSONNEL_FILE", 16: "ANTI_RETALIATION", 17: "JOB_SECURITY",
			18: "SEPARATION_FILING", 19: "DRUG_TESTING", 20: "BREACH_NOTIFICATION",
			21: "AUTOMATED_DECISION", 22: "MONITORING_CONSENT",
		}
		for ordinal, token := range added {
			if got := ordinal.String(); got != token {
				t.Errorf("ordinal %d is %s, want %s", ordinal, got, token)
			}
			parsed, err := ParseObligationType(token)
			if err != nil || parsed != ordinal {
				t.Errorf("ParseObligationType(%s) = %v, %v; want ordinal %d", token, parsed, err, ordinal)
			}
			if VocabularyOf(ordinal) != VocabularyVersion2 {
				t.Errorf("%s is not attributed to vocabulary 2", token)
			}
		}
	})

	t.Run("every kind binds to the lifecycle step the contract assigns it", func(t *testing.T) {
		// Transcribed from the contract's sections 4.1 and 4.2. If a binding
		// moves, this table is the thing that has to change with it.
		want := map[ObligationType][]LifecycleStep{
			ObligationTypeNotice:             {LifecycleStepSimulate, LifecycleStepExecute, LifecycleStepPostCommit},
			ObligationTypeFieldRestriction:   {LifecycleStepDraft},
			ObligationTypeRetention:          {LifecycleStepPostCommit},
			ObligationTypeLeaveInteraction:   {LifecycleStepPreflight, LifecycleStepExecute},
			ObligationTypePayFrequency:       {LifecycleStepPreflight},
			ObligationTypeFinalPayDeadline:   {LifecycleStepExecute},
			ObligationTypePayTransparency:    {LifecycleStepDraft, LifecycleStepPreflight},
			ObligationTypeNonCompete:         {LifecycleStepSimulate, LifecycleStepApproval, LifecycleStepPostCommit},
			ObligationTypeEVerify:            {LifecycleStepDraft},
			ObligationTypeMiniWARN:           {LifecycleStepExecute},
			ObligationTypeWageFloor:          {LifecycleStepPreflight, LifecycleStepExecute},
			ObligationTypePayEquityReview:    {LifecycleStepSimulate, LifecycleStepApproval},
			ObligationTypePayStatement:       {LifecycleStepPostCommit},
			ObligationTypeClassification:     {LifecycleStepPreflight, LifecycleStepApproval},
			ObligationTypePersonnelFile:      {LifecycleStepPostCommit},
			ObligationTypeAntiRetaliation:    {LifecycleStepPreflight, LifecycleStepApproval},
			ObligationTypeJobSecurity:        {LifecycleStepApproval},
			ObligationTypeSeparationFiling:   {LifecycleStepPostCommit},
			ObligationTypeDrugTesting:        {LifecycleStepPreflight},
			ObligationTypeBreachNotification: {LifecycleStepPostCommit},
			ObligationTypeAutomatedDecision:  {LifecycleStepDraft, LifecycleStepApproval},
			ObligationTypeMonitoringConsent:  {LifecycleStepDraft},
		}
		wantKind := map[ObligationType]ObligationBindingKind{
			ObligationTypeNotice:             ObligationBindingKindNode,
			ObligationTypeFieldRestriction:   ObligationBindingKindFieldMask,
			ObligationTypeRetention:          ObligationBindingKindNode,
			ObligationTypeLeaveInteraction:   ObligationBindingKindGuard,
			ObligationTypePayFrequency:       ObligationBindingKindGuard,
			ObligationTypeFinalPayDeadline:   ObligationBindingKindTimer,
			ObligationTypePayTransparency:    ObligationBindingKindNode,
			ObligationTypeNonCompete:         ObligationBindingKindHumanTask,
			ObligationTypeEVerify:            ObligationBindingKindGuard,
			ObligationTypeMiniWARN:           ObligationBindingKindTimer,
			ObligationTypeWageFloor:          ObligationBindingKindGuard,
			ObligationTypePayEquityReview:    ObligationBindingKindHumanTask,
			ObligationTypePayStatement:       ObligationBindingKindNode,
			ObligationTypeClassification:     ObligationBindingKindGuard,
			ObligationTypePersonnelFile:      ObligationBindingKindNode,
			ObligationTypeAntiRetaliation:    ObligationBindingKindGuard,
			ObligationTypeJobSecurity:        ObligationBindingKindHumanTask,
			ObligationTypeSeparationFiling:   ObligationBindingKindNode,
			ObligationTypeDrugTesting:        ObligationBindingKindGuard,
			ObligationTypeBreachNotification: ObligationBindingKindTimer,
			ObligationTypeAutomatedDecision:  ObligationBindingKindGuard,
			ObligationTypeMonitoringConsent:  ObligationBindingKindFieldMask,
		}
		for _, kind := range AllObligationTypes() {
			spec, ok := ObligationKindSpecFor(kind)
			if !ok {
				t.Errorf("%s has no ObligationKindSpec row", kind)
				continue
			}
			var got []LifecycleStep
			for _, b := range spec.Bindings {
				got = append(got, b.Step)
			}
			if len(got) != len(want[kind]) {
				t.Errorf("%s binds to %v, want %v", kind, got, want[kind])
				continue
			}
			for i := range got {
				if got[i] != want[kind][i] {
					t.Errorf("%s binds to %v, want %v", kind, got, want[kind])
					break
				}
			}
			if spec.Bindings[0].Kind != wantKind[kind] {
				t.Errorf("%s primary binding kind = %s, want %s", kind, spec.Bindings[0].Kind, wantKind[kind])
			}
		}
	})

	t.Run("every declared obligation is answered exactly once", func(t *testing.T) {
		reg, pack := vocabularyTwoRegistry(t)
		ctx := zzContext(t, reg)
		for name, proposal := range map[string]PromotionProposalSnapshot{
			"everything fires": everythingFiresProposal(t),
			"nothing fires":    {EffectiveDate: mustDate(t, 2026, time.March, 1)},
		} {
			result, err := Evaluate(ctx, proposal, reg)
			if err != nil {
				t.Fatalf("%s: Evaluate: %v", name, err)
			}
			answered := map[string]int{}
			for _, o := range result.Obligations {
				answered[o.ID]++
			}
			for _, o := range result.NotApplicable {
				answered[o.ID]++
			}
			declared := pack.obligations()
			if len(answered) != len(declared) {
				t.Errorf("%s: %d obligations answered, %d declared", name, len(answered), len(declared))
			}
			for _, o := range declared {
				switch answered[o.Rule.obligationID()] {
				case 1:
				case 0:
					t.Errorf("%s: %s %q is in neither applied nor CONSIDERED_NOT_APPLICABLE",
						name, o.Type, o.Rule.obligationID())
				default:
					t.Errorf("%s: %s %q was answered twice", name, o.Type, o.Rule.obligationID())
				}
			}
			if len(result.NotConsidered) != 0 {
				t.Errorf("%s: a vocabulary-2 pack reported %d not-considered kinds", name, len(result.NotConsidered))
			}
		}
	})

	t.Run("a false trigger records the fact that made it false", func(t *testing.T) {
		reg, _ := vocabularyTwoRegistry(t)
		ctx := zzContext(t, reg)
		result, err := Evaluate(ctx, PromotionProposalSnapshot{EffectiveDate: mustDate(t, 2026, time.March, 1)}, reg)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		// The three unconditional kinds still apply; everything else is
		// answered as not applicable, each with a reason.
		unconditional := map[ObligationType]bool{
			ObligationTypeRetention:     true,
			ObligationTypePayFrequency:  true,
			ObligationTypeWageFloor:     true,
			ObligationTypePersonnelFile: true,
		}
		for _, o := range result.Obligations {
			if !unconditional[o.Type] {
				t.Errorf("%s applied against an empty proposal", o.Type)
			}
		}
		if len(result.NotApplicable) != 22-len(unconditional) {
			t.Errorf("NotApplicable = %d, want %d", len(result.NotApplicable), 22-len(unconditional))
		}
		for _, o := range result.NotApplicable {
			if strings.TrimSpace(string(o.Reason)) == "" {
				t.Errorf("%s %q is recorded as not applicable with no reason", o.Type, o.ID)
			}
			if err := o.Citation.Validate(); err != nil {
				t.Errorf("%s %q carries an incomplete citation: %v", o.Type, o.ID, err)
			}
		}
	})

	t.Run("LEAVE_INTERACTION fires on a balance, not only on being on leave", func(t *testing.T) {
		reg, _ := vocabularyTwoRegistry(t)
		ctx := zzContext(t, reg)

		// The contract's section 4.1 correction: twenty-eight states impose an
		// accrual-carry rule that binds on every promotion. A worker who is
		// not on leave but holds a balance must fire the trigger.
		holdsBalance := PromotionProposalSnapshot{
			EffectiveDate:        mustDate(t, 2026, time.March, 1),
			OnProtectedLeave:     false,
			LeaveProgramBalances: []string{"Accrued Paid Sick Leave"},
		}
		result, err := Evaluate(ctx, holdsBalance, reg)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !hasObligation(result, "zz-leave") {
			t.Fatal("LEAVE_INTERACTION did not fire for a worker holding a balance and not on leave")
		}

		// OnProtectedLeave remains a fact and still fires it.
		onLeave := PromotionProposalSnapshot{
			EffectiveDate:    mustDate(t, 2026, time.March, 1),
			OnProtectedLeave: true,
		}
		result, err = Evaluate(ctx, onLeave, reg)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !hasObligation(result, "zz-leave") {
			t.Fatal("LEAVE_INTERACTION stopped firing for a worker on protected leave")
		}

		// No balance and not on leave: answered, and answered as not
		// applicable with the balance as the reason.
		none := PromotionProposalSnapshot{EffectiveDate: mustDate(t, 2026, time.March, 1)}
		result, err = Evaluate(ctx, none, reg)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if hasObligation(result, "zz-leave") {
			t.Fatal("LEAVE_INTERACTION fired with no balance and no leave")
		}
		found := false
		for _, o := range result.NotApplicable {
			if o.ID == "zz-leave" {
				found = true
				if !strings.Contains(string(o.Reason), "balance") {
					t.Errorf("LEAVE_INTERACTION reason = %q, want it to name the missing balance", o.Reason)
				}
			}
		}
		if !found {
			t.Error("LEAVE_INTERACTION vanished instead of being recorded as not applicable")
		}
	})

	t.Run("every binding is non-removable and names a valid step", func(t *testing.T) {
		reg, _ := vocabularyTwoRegistry(t)
		ctx := zzContext(t, reg)
		result, err := Evaluate(ctx, everythingFiresProposal(t), reg)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(result.Obligations) != 22 {
			t.Fatalf("applied obligations = %d, want all 22 to fire", len(result.Obligations))
		}
		for _, o := range result.Obligations {
			if len(o.Bindings) == 0 {
				t.Errorf("%s %q produced no binding", o.Type, o.ID)
				continue
			}
			if o.Binding != o.Bindings[0] {
				t.Errorf("%s %q: Binding is not Bindings[0]", o.Type, o.ID)
			}
			for _, b := range o.Bindings {
				if err := b.Validate(); err != nil {
					t.Errorf("%s %q binding: %v", o.Type, o.ID, err)
				}
				if !b.NonRemovable {
					t.Errorf("%s %q produced a removable binding", o.Type, o.ID)
				}
			}
			if o.Citation.ConfidenceMarker == ConfidenceMarkerUnspecified {
				t.Errorf("%s %q carries no confidence marker", o.Type, o.ID)
			}
			if err := o.Citation.ValidateForVocabulary(VocabularyVersion2); err != nil {
				t.Errorf("%s %q citation: %v", o.Type, o.ID, err)
			}
		}
	})
}

func hasObligation(result EvaluationResult, id string) bool {
	for _, o := range result.Obligations {
		if o.ID == id {
			return true
		}
	}
	return false
}

// --- TestTodo_LEGAL_011_Property --------------------------------------------

// TestTodo_LEGAL_011_Property asserts the properties the REFACTOR clause
// demands: trigger predicates are pure functions of the proposal snapshot and
// the typed body, with no registry or clock access, and evaluation output is
// deterministic.
func TestTodo_LEGAL_011_Property(t *testing.T) {
	reg, pack := vocabularyTwoRegistry(t)
	ctx := zzContext(t, reg)

	t.Run("triggers are pure: same input, same answer, no registry", func(t *testing.T) {
		proposal := everythingFiresProposal(t)
		for _, o := range pack.obligations() {
			first, firstReason := o.Rule.trigger(proposal)
			for i := 0; i < 32; i++ {
				got, reason := o.Rule.trigger(proposal)
				if got != first || reason != firstReason {
					t.Fatalf("%s %q trigger is not a pure function of its inputs",
						o.Type, o.Rule.obligationID())
				}
			}
		}
	})

	t.Run("evaluation is deterministic and stably ordered", func(t *testing.T) {
		proposal := everythingFiresProposal(t)
		baseline, err := Evaluate(ctx, proposal, reg)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		for i := 0; i < 16; i++ {
			got, err := Evaluate(ctx, proposal, reg)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if fingerprint(got) != fingerprint(baseline) {
				t.Fatal("two evaluations of identical input produced different output")
			}
		}
		// Sorted by kind ordinal, then by id.
		for i := 1; i < len(baseline.Obligations); i++ {
			prev, cur := baseline.Obligations[i-1], baseline.Obligations[i]
			if prev.Type > cur.Type || (prev.Type == cur.Type && prev.ID > cur.ID) {
				t.Fatalf("obligations are not sorted at index %d: %s/%s then %s/%s",
					i, prev.Type, prev.ID, cur.Type, cur.ID)
			}
		}
	})

	t.Run("trigger totality holds for every generated proposal", func(t *testing.T) {
		// The contract's section 8.2 "trigger totality" property: for every
		// obligation in every registered release, the result contains it in
		// exactly one of the two lists, for any input.
		declared := len(pack.obligations())
		for mask := 0; mask < 64; mask++ {
			proposal := proposalFromMask(t, mask)
			result, err := Evaluate(ctx, proposal, reg)
			if err != nil {
				t.Fatalf("mask %d: Evaluate: %v", mask, err)
			}
			if len(result.Obligations)+len(result.NotApplicable) != declared {
				t.Fatalf("mask %d: %d applied + %d not applicable != %d declared",
					mask, len(result.Obligations), len(result.NotApplicable), declared)
			}
		}
	})

	t.Run("the typed body digest ignores the citation note", func(t *testing.T) {
		reworded := pack
		reworded.WageFloors = append([]WageFloorRule(nil), pack.WageFloors...)
		reworded.WageFloors[0].Citation.Note = "a different paraphrase entirely"
		if reworded.ComputeDigest() != pack.ComputeDigest() {
			t.Fatal("rewording a citation note changed the release digest")
		}
		changed := pack
		changed.WageFloors = append([]WageFloorRule(nil), pack.WageFloors...)
		changed.WageFloors[0].Indexation = "NONE"
		if changed.ComputeDigest() == pack.ComputeDigest() {
			t.Fatal("changing a typed body field left the release digest unchanged")
		}
	})
}

// proposalFromMask builds a proposal from six independent fact bits, so the
// totality property is checked across the whole trigger surface rather than
// on one happy path.
func proposalFromMask(t *testing.T, mask int) PromotionProposalSnapshot {
	t.Helper()
	p := PromotionProposalSnapshot{EffectiveDate: mustDate(t, 2026, time.March, 1)}
	if mask&1 != 0 {
		current, _ := values.NewMoney("100.00", "USD", 2, values.RoundingHalfEven)
		next, _ := values.NewMoney("120.00", "USD", 2, values.RoundingHalfEven)
		p.CurrentBasePay, p.NewBasePay = current, next
	}
	if mask&2 != 0 {
		p.SeparationConcurrent = true
	}
	if mask&4 != 0 {
		p.LeaveProgramBalances = []string{"accrued paid sick leave"}
	}
	if mask&8 != 0 {
		p.RecordedProtectedActivities = []RecordedProtectedActivity{{Kind: "wage_claim", DaysBeforeEffectiveDate: 10}}
	}
	if mask&16 != 0 {
		p.DataCategoriesTouched = []string{"biometric_identifiers"}
	}
	if mask&32 != 0 {
		p.IsInternalPromotion, p.CollectsSalaryHistory, p.RoleChanged = true, true, true
	}
	return p
}

func fingerprint(r EvaluationResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "status=%s\n", r.Status)
	for _, o := range r.Obligations {
		fmt.Fprintf(&b, "A %s %s %s %s\n", o.Type, o.ID, o.Binding.Kind, o.Binding.LifecycleStep)
	}
	for _, o := range r.NotApplicable {
		fmt.Fprintf(&b, "N %s %s %s\n", o.Type, o.ID, o.Reason)
	}
	for _, o := range r.NotConsidered {
		fmt.Fprintf(&b, "U %s %s\n", o.Type, o.PackID)
	}
	return b.String()
}

// --- TestTodo_LEGAL_011_Golden ----------------------------------------------

// TestTodo_LEGAL_011_Golden pins the whole twenty-two-kind evaluation: which
// obligations applied, at which binding and lifecycle step, and which were
// recorded as CONSIDERED_NOT_APPLICABLE with which reason.
func TestTodo_LEGAL_011_Golden(t *testing.T) {
	reg, _ := vocabularyTwoRegistry(t)
	ctx := zzContext(t, reg)
	result, err := Evaluate(ctx, PromotionProposalSnapshot{
		EffectiveDate:        mustDate(t, 2026, time.March, 1),
		IsInternalPromotion:  true,
		LeaveProgramBalances: []string{"accrued paid sick leave"},
	}, reg)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	wantApplied := []string{
		"RETENTION/zz-retention/GUARD?",
	}
	_ = wantApplied

	gotApplied := make([]string, 0, len(result.Obligations))
	for _, o := range result.Obligations {
		gotApplied = append(gotApplied, fmt.Sprintf("%s/%s/%s@%s",
			o.Type, o.ID, o.Binding.Kind, o.Binding.LifecycleStep))
	}
	want := []string{
		"RETENTION/zz-retention/NODE@POST-COMMIT",
		"LEAVE_INTERACTION/zz-leave/GUARD@PREFLIGHT",
		"PAY_FREQUENCY/zz-pay-frequency/GUARD@PREFLIGHT",
		"PAY_TRANSPARENCY/zz-pay-transparency/NODE@DRAFT",
		"WAGE_FLOOR/zz-wage-floor/GUARD@PREFLIGHT",
		"PERSONNEL_FILE/zz-personnel-file/NODE@POST-COMMIT",
	}
	if len(gotApplied) != len(want) {
		t.Fatalf("applied = %v, want %v", gotApplied, want)
	}
	for i := range want {
		if gotApplied[i] != want[i] {
			t.Fatalf("applied[%d] = %s, want %s", i, gotApplied[i], want[i])
		}
	}

	// The other sixteen are answered, not omitted.
	if len(result.NotApplicable) != 16 {
		t.Fatalf("CONSIDERED_NOT_APPLICABLE = %d, want 16", len(result.NotApplicable))
	}
	wantReasons := map[string]NotApplicableReason{
		"zz-notice":             reasonPayRateUnchanged,
		"zz-field-restriction":  reasonNoSalaryHistory,
		"zz-final-pay":          reasonNoSeparation,
		"zz-non-compete":        reasonNoCovenant,
		"zz-e-verify":           reasonNotNewHire,
		"zz-mini-warn":          reasonBelowWARN,
		"zz-pay-equity":         reasonPayRateUnchanged,
		"zz-pay-statement":      reasonPayRateUnchanged,
		"zz-classification":     reasonNoClassChange,
		"zz-anti-retaliation":   reasonNoProtectedAct,
		"zz-job-security":       reasonNoAdverseChange,
		"zz-separation-filing":  reasonNoSeparation,
		"zz-drug-testing":       reasonNoTestTrigger,
		"zz-breach":             reasonNoBreach,
		"zz-automated-decision": reasonNoModel,
		"zz-monitoring-consent": reasonNoCoveredCategory,
	}
	for _, o := range result.NotApplicable {
		want, ok := wantReasons[o.ID]
		if !ok {
			t.Errorf("unexpected not-applicable obligation %q", o.ID)
			continue
		}
		if o.Reason != want {
			t.Errorf("%s reason = %q, want %q", o.ID, o.Reason, want)
		}
		delete(wantReasons, o.ID)
	}
	if len(wantReasons) != 0 {
		keys := make([]string, 0, len(wantReasons))
		for k := range wantReasons {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("obligations neither applied nor recorded as not applicable: %v", keys)
	}
}

// --- FuzzTodo_LEGAL_011 -----------------------------------------------------

// FuzzTodo_LEGAL_011 drives arbitrary proposal facts through all twenty-two
// triggers. Two invariants must hold for any input: no predicate panics, and
// every declared obligation is answered exactly once.
func FuzzTodo_LEGAL_011(f *testing.F) {
	f.Add(uint32(0), "", "")
	f.Add(uint32(0xFFFFFFFF), "accrued paid sick leave", "biometric_identifiers")
	f.Add(uint32(5), "unmatched program", "unmatched category")

	f.Fuzz(func(t *testing.T, bits uint32, leaveProgram, dataCategory string) {
		pack := allKindsPack(t)
		proposal := PromotionProposalSnapshot{
			IsInternalPromotion:        bits&1 != 0,
			CollectsSalaryHistory:      bits&2 != 0,
			OnProtectedLeave:           bits&4 != 0,
			HasExistingNonCompete:      bits&8 != 0,
			IsNewHire:                  bits&16 != 0,
			SeparationConcurrent:       bits&32 != 0,
			RoleChanged:                bits&64 != 0,
			HoursChanged:               bits&128 != 0,
			PayBasisChanged:            bits&256 != 0,
			IsDemotion:                 bits&512 != 0,
			RoleBecomesSafetySensitive: bits&1024 != 0,
			DrugTestOrdered:            bits&2048 != 0,
			BreachIncidentOpened:       bits&4096 != 0,
			AutomatedDecisionApplied:   bits&8192 != 0,
			WorkforceReductionCount:    int(bits % 200),
			LeaveProgramBalances:       []string{leaveProgram},
			DataCategoriesTouched:      []string{dataCategory},
			RecordedProtectedActivities: []RecordedProtectedActivity{
				{Kind: "generated", DaysBeforeEffectiveDate: int(bits % 400)},
			},
		}
		applied, considered := applicableObligations(pack, proposal)
		if len(applied)+len(considered) != len(pack.obligations()) {
			t.Fatalf("%d applied + %d considered != %d declared",
				len(applied), len(considered), len(pack.obligations()))
		}
		for _, o := range applied {
			if len(o.Bindings) == 0 || !o.Binding.NonRemovable {
				t.Fatalf("%s %q produced a missing or removable binding", o.Type, o.ID)
			}
		}
		for _, o := range considered {
			if o.Reason == "" {
				t.Fatalf("%s %q recorded as not applicable with no reason", o.Type, o.ID)
			}
		}
	})
}

// --- TestTodo_LEGAL_011_Security --------------------------------------------

// TestTodo_LEGAL_011_Security fixes the things a caller must not be able to do
// to the twenty-two kinds: remove a binding, publish an uncited rule, or
// publish a rule whose typed body is incomplete.
func TestTodo_LEGAL_011_Security(t *testing.T) {
	t.Run("a binding cannot be constructed removable", func(t *testing.T) {
		for _, kind := range AllObligationTypes() {
			for _, b := range bindingsFor(kind, "id", "description") {
				if !b.NonRemovable {
					t.Errorf("%s produced a removable binding", kind)
				}
			}
		}
		removable := ObligationBinding{
			ObligationID: "x", Kind: ObligationBindingKindGuard,
			LifecycleStep: LifecycleStepPreflight, NonRemovable: false,
		}
		if err := removable.Validate(); err == nil {
			t.Error("a removable binding validated")
		}
	})

	t.Run("a binding cannot name a step the contract does not define", func(t *testing.T) {
		invented := ObligationBinding{
			ObligationID: "x", Kind: ObligationBindingKindGuard,
			LifecycleStep: "WHENEVER", NonRemovable: true,
		}
		if err := invented.Validate(); err == nil {
			t.Error("a binding to an undefined lifecycle step validated")
		}
	})

	t.Run("a vocabulary-2 rule without a confidence marker cannot validate", func(t *testing.T) {
		pack := allKindsPack(t)
		pack.WageFloors[0].Citation.ConfidenceMarker = ConfidenceMarkerUnspecified
		if err := pack.Validate(); !errors.Is(err, ErrConfidenceMarker) {
			t.Fatalf("Validate = %v, want ErrConfidenceMarker", err)
		}
	})

	t.Run("a vocabulary-1 pack cannot smuggle in a vocabulary-2 kind", func(t *testing.T) {
		pack := allKindsPack(t)
		pack.VocabularyVersion = VocabularyVersion1
		if err := pack.Validate(); err == nil {
			t.Fatal("a pack typed against vocabulary 1 accepted twelve vocabulary-2 obligations")
		}
	})

	t.Run("a recommendation never presents as a statutory requirement", func(t *testing.T) {
		pack := allKindsPack(t)
		pack.WageFloors[0].Standard = RuleStandardRecommended
		applied, _ := applicableObligations(pack, everythingFiresProposal(t))
		for _, o := range applied {
			if o.ID != "zz-wage-floor" {
				continue
			}
			if !strings.Contains(o.Description, "RECOMMENDED") {
				t.Errorf("a RECOMMENDED rule reads as a requirement: %q", o.Description)
			}
		}
		// And the qualifier is inside the digest, so it cannot be edited away
		// without a new release.
		base := allKindsPack(t)
		if pack.ComputeDigest() == base.ComputeDigest() {
			t.Error("the RECOMMENDED qualifier is outside the release digest")
		}
	})

	t.Run("an incomplete typed body cannot be released above UNREVIEWED", func(t *testing.T) {
		pack := allKindsPack(t)
		pack.BreachNotifications[0].SubjectDeadlineDays = 0
		if err := pack.Validate(); err != nil {
			t.Fatalf("an unstated deadline must be legal on an UNREVIEWED draft: %v", err)
		}
		pack.ReviewStatus = ReviewStatusVendorBaseline
		if err := pack.ValidateForRelease(); err == nil {
			t.Fatal("a release above UNREVIEWED accepted an unstated breach-notification deadline")
		}
	})
}

// --- TestTodo_LEGAL_011_Mutation --------------------------------------------

// TestTodo_LEGAL_011_Mutation seeds a mutant into every trigger predicate and
// asserts the evaluation notices. A surviving mutant means the predicate is
// not actually consulted.
func TestTodo_LEGAL_011_Mutation(t *testing.T) {
	reg, _ := vocabularyTwoRegistry(t)
	ctx := zzContext(t, reg)
	all := everythingFiresProposal(t)

	// Each entry removes exactly one fact and names the obligation that must
	// stop firing as a result. If dropping the fact changes nothing, the
	// trigger is ignoring its input.
	mutants := []struct {
		name        string
		drop        func(*PromotionProposalSnapshot)
		stopsFiring []string
		keepsFiring []string
	}{
		{"pay rate unchanged", func(p *PromotionProposalSnapshot) {
			p.CurrentBasePay, p.NewBasePay = values.Money{}, values.Money{}
			p.RoleChanged, p.HoursChanged, p.PayBasisChanged = false, false, false
		}, []string{"zz-notice", "zz-pay-equity", "zz-pay-statement", "zz-classification"}, []string{"zz-retention"}},
		{"no salary history", func(p *PromotionProposalSnapshot) { p.CollectsSalaryHistory = false },
			[]string{"zz-field-restriction"}, []string{"zz-notice"}},
		{"no leave balance", func(p *PromotionProposalSnapshot) {
			p.LeaveProgramBalances = nil
			p.OnProtectedLeave = false
		}, []string{"zz-leave"}, []string{"zz-pay-frequency"}},
		{"no separation", func(p *PromotionProposalSnapshot) { p.SeparationConcurrent = false },
			[]string{"zz-final-pay", "zz-separation-filing"}, []string{"zz-retention"}},
		{"not a promotion", func(p *PromotionProposalSnapshot) { p.IsInternalPromotion = false },
			[]string{"zz-pay-transparency"}, []string{"zz-notice"}},
		{"no covenant", func(p *PromotionProposalSnapshot) { p.HasExistingNonCompete = false },
			[]string{"zz-non-compete"}, []string{"zz-notice"}},
		{"not a new hire", func(p *PromotionProposalSnapshot) { p.IsNewHire = false },
			[]string{"zz-e-verify"}, []string{"zz-notice"}},
		{"reduction below threshold", func(p *PromotionProposalSnapshot) { p.WorkforceReductionCount = 24 },
			[]string{"zz-mini-warn"}, []string{"zz-notice"}},
		{"no protected activity in the lookback", func(p *PromotionProposalSnapshot) {
			p.RecordedProtectedActivities = []RecordedProtectedActivity{{Kind: "wage_claim", DaysBeforeEffectiveDate: 400}}
		}, []string{"zz-anti-retaliation"}, []string{"zz-notice"}},
		{"no adverse change", func(p *PromotionProposalSnapshot) {
			p.IsDemotion, p.SeparationConcurrent = false, false
		}, []string{"zz-job-security"}, []string{"zz-notice"}},
		{"no test trigger", func(p *PromotionProposalSnapshot) {
			p.RoleBecomesSafetySensitive, p.DrugTestOrdered = false, false
		}, []string{"zz-drug-testing"}, []string{"zz-notice"}},
		{"no breach incident", func(p *PromotionProposalSnapshot) { p.BreachIncidentOpened = false },
			[]string{"zz-breach"}, []string{"zz-notice"}},
		{"no model applied", func(p *PromotionProposalSnapshot) { p.AutomatedDecisionApplied = false },
			[]string{"zz-automated-decision"}, []string{"zz-notice"}},
		{"no covered data category", func(p *PromotionProposalSnapshot) {
			p.DataCategoriesTouched = []string{"something_else"}
		}, []string{"zz-monitoring-consent"}, []string{"zz-notice"}},
	}

	baseline, err := Evaluate(ctx, all, reg)
	if err != nil {
		t.Fatalf("Evaluate(baseline): %v", err)
	}
	if len(baseline.Obligations) != 22 {
		t.Fatalf("baseline applied %d obligations, want 22", len(baseline.Obligations))
	}

	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			proposal := all
			m.drop(&proposal)
			result, err := Evaluate(ctx, proposal, reg)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			for _, id := range m.stopsFiring {
				if hasObligation(result, id) {
					t.Errorf("mutant survived: %s still fires after %q", id, m.name)
				}
			}
			for _, id := range m.keepsFiring {
				if !hasObligation(result, id) {
					t.Errorf("over-broad mutant: %s stopped firing after %q", id, m.name)
				}
			}
		})
	}
}
