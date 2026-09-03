package legal

import "github.com/monstercameron/hcm-next/internal/kernel/values"

// californiaSourceFile is the research file every California citation in this
// package points back to. It is drafted, unreviewed research, not legal
// advice; see [ReviewStatusUnreviewed].
const californiaSourceFile = "planning/research/state-employment-law/california.md"

// CaliforniaPromotionPack builds the seed California rule pack for a
// promotion-and-base-pay-change transaction, version 1, open-ended from
// 2026-01-01. Every rule cites californiaSourceFile and carries
// [ReviewStatusUnreviewed]: this is a fixture proving the [RulePack]
// skeleton, not a reviewed legal interpretation.
func CaliforniaPromotionPack() (RulePack, error) {
	start, err := values.NewLocalDate(2026, 1, 1)
	if err != nil {
		return RulePack{}, err
	}
	window, err := NewOpenEffectiveWindow(start)
	if err != nil {
		return RulePack{}, err
	}
	cite := func(section, note string) Citation {
		return Citation{SourceFile: californiaSourceFile, Section: section, Note: note, Status: ReviewStatusUnreviewed}
	}
	return RulePack{
		PackID:       "us-ca-promotion-base-pay-change",
		Version:      1,
		Jurisdiction: Jurisdiction{Country: "US", State: "CA"},
		Window:       window,
		Notices: []NoticeObligation{
			{
				ID:              "ca-notice-pay-rate-change",
				Who:             "employer_to_worker",
				TimingDirection: "AFTER",
				TimingDays:      7,
				Channel:         "written",
				ContentFields:   []string{"pay_rate", "pay_basis", "payday", "allowances"},
				Citation: cite("Labor Code § 2810.5",
					"written notice of a pay-rate, pay-basis, payday, or allowance change is due within 7 calendar days of the change"),
			},
		},
		FieldRestrictions: []FieldRestriction{
			{
				ID:               "ca-field-restriction-salary-history",
				RestrictedFields: []string{"salary_history"},
				Context:          "setting compensation for an applicant or an internal promotion candidate",
				Citation: cite("Labor Code § 432.3",
					"employers may not seek an applicant's salary history or rely on it, even voluntarily disclosed, to set pay"),
			},
		},
		RetentionRules: []RetentionRule{
			{
				ID:                   "ca-retention-wage-job-title-history",
				RecordClass:          "wage_rate_and_job_title_history",
				DurationYears:        3,
				DurationBasis:        "employment_plus_years",
				JurisdictionOverride: true,
				Citation: cite("Labor Code § 432.3",
					"job title and wage rate history must be retained for the duration of employment plus 3 years"),
			},
			{
				ID:                   "ca-retention-wage-statements",
				RecordClass:          "itemized_wage_statements",
				DurationYears:        3,
				DurationBasis:        "from_record_date",
				JurisdictionOverride: true,
				Citation: cite("Labor Code § 226",
					"itemized wage statements and deduction records must be retained 3 years"),
			},
		},
		LeaveInteractions: []LeaveInteraction{
			{
				ID:        "ca-leave-interaction-paid-sick-leave",
				LeaveType: "accrued paid sick leave",
				InteractionRule: "accrued but unused paid sick leave is not forfeited or reduced on a promotion or demotion; " +
					"the balance carries to the new role",
				Citation: cite("Labor Code § 246", "accrued PSL does not reset on role change or demotion"),
			},
		},
		PayFrequencyConstraints: []PayFrequencyConstraint{
			{
				ID:                   "ca-pay-frequency-semimonthly",
				MinimumFrequency:     "SEMIMONTHLY",
				AppliesToWorkerClass: "covered employees generally",
				Citation:             cite("Labor Code § 204", "wages are due at least semimonthly on designated paydays"),
			},
		},
		FinalPayDeadlines: []FinalPayDeadline{
			{
				ID:                  "ca-final-pay-discharge",
				Trigger:             "involuntary_discharge",
				DeadlineDescription: "all earned wages, including accrued vacation, are due immediately at the time of discharge",
				Citation:            cite("Labor Code § 201", "final wages due immediately upon discharge or mass layoff"),
			},
			{
				ID:                  "ca-final-pay-resignation",
				Trigger:             "resignation",
				DeadlineDescription: "wages are due within 72 hours of resignation, or immediately if the employee gave 72 hours' notice",
				Citation:            cite("Labor Code § 202", "72-hour final-pay window on resignation"),
			},
		},
		PayTransparencyDuties: []PayTransparencyDuty{
			{
				ID:      "ca-pay-transparency-scale-on-request",
				Trigger: "internal_promotion",
				RequiredDisclosure: "the pay scale for the position must be provided upon reasonable request; employers with 15+ " +
					"employees must also include it in job postings for the position",
				Citation: cite("Labor Code § 432.3", "pay-scale disclosure on request and in job postings"),
			},
		},
		NonCompeteThresholds: []NonCompeteThreshold{
			{
				ID:                 "ca-noncompete-void",
				ReCheckOnPayChange: true,
				Rule: "non-compete and non-solicit clauses are void outside the sale/dissolution-of-business exception; " +
					"affected current and certain former employees must be notified in writing that the clause is void",
				Citation: cite("Business and Professions Code §§ 16600, 16600.1 (AB 1076)", "non-competes void; void-clause notice required"),
			},
		},
		MiniWARNTriggers: []MiniWARNTrigger{
			{
				ID:                "ca-warn-mass-layoff",
				EmployeeThreshold: 50,
				LayoffWindowDays:  30,
				NoticeDays:        60,
				Note:              "Cal-WARN applies to a covered establishment of 75+ employees ordering a mass layoff of 50+ employees within 30 days",
				Citation:          cite("Labor Code §§ 1400-1408", "60-day mass-layoff notice"),
			},
		},
	}, nil
}
