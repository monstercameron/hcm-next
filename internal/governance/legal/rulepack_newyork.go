package legal

import "github.com/monstercameron/hcm-next/internal/kernel/values"

// newYorkSourceFile is the research file every New York citation in this
// package points back to. It is drafted, unreviewed research, not legal
// advice; see [ReviewStatusUnreviewed].
const newYorkSourceFile = "planning/research/state-employment-law/new-york.md"

// NewYorkPromotionPack builds the seed New York rule pack for a
// promotion-and-base-pay-change transaction, version 1, open-ended from
// 2026-01-01. Every rule cites newYorkSourceFile and carries
// [ReviewStatusUnreviewed]: this is a fixture proving the [RulePack]
// skeleton, not a reviewed legal interpretation.
func NewYorkPromotionPack() (RulePack, error) {
	start, err := values.NewLocalDate(2026, 1, 1)
	if err != nil {
		return RulePack{}, err
	}
	window, err := NewOpenEffectiveWindow(start)
	if err != nil {
		return RulePack{}, err
	}
	cite := func(section, note string) Citation {
		return Citation{SourceFile: newYorkSourceFile, Section: section, Note: note, Status: ReviewStatusUnreviewed}
	}
	return RulePack{
		PackID:       "us-ny-promotion-base-pay-change",
		Version:      1,
		Jurisdiction: Jurisdiction{Country: "US", State: "NY"},
		Window:       window,
		Notices: []NoticeObligation{
			{
				ID:              "ny-notice-pay-rate-change",
				Who:             "employer_to_worker",
				TimingDirection: "BEFORE",
				TimingDays:      7,
				UnlessCondition: "an increase may instead be reflected on the next regular pay statement rather than preceded by notice",
				Channel:         "written",
				ContentFields:   []string{"pay_rate", "pay_frequency"},
				Citation: cite("Labor Law § 195(2)",
					"written notice of a pay-rate change is due at least 7 days before the change, with an exception for increases"),
			},
		},
		FieldRestrictions: []FieldRestriction{
			{
				ID:               "ny-field-restriction-salary-history",
				RestrictedFields: []string{"salary_history"},
				Context:          "inquiring about an applicant's or employee's wage history, directly or through a third party",
				Citation:         cite("Labor Law § 194-a", "salary history inquiries prohibited; voluntary disclosure only"),
			},
		},
		RetentionRules: []RetentionRule{
			{
				ID:                   "ny-retention-wage-hour-records",
				RecordClass:          "wage_hour_records",
				DurationYears:        6,
				DurationBasis:        "from_record_date",
				JurisdictionOverride: true,
				Citation:             cite("Labor Law § 195(4)", "hours, rates, deductions, and pay records retained 6 years"),
			},
		},
		LeaveInteractions: []LeaveInteraction{
			{
				ID:        "ny-leave-interaction-sick-leave",
				LeaveType: "earned sick and safe leave",
				InteractionRule: "accrued sick leave balance carries to the promoted role; an employee returning from Paid Family " +
					"Leave resumes the promoted role and rate on return",
				Citation: cite("Labor Law § 196-b", "accrued leave is not forfeited on a role or pay change"),
			},
		},
		PayFrequencyConstraints: []PayFrequencyConstraint{
			{
				ID:                   "ny-pay-frequency-manual-weekly",
				MinimumFrequency:     "WEEKLY",
				AppliesToWorkerClass: "manual workers",
				Citation:             cite("Labor Law § 191", "manual workers must be paid weekly"),
			},
			{
				ID:                   "ny-pay-frequency-clerical-semimonthly",
				MinimumFrequency:     "SEMIMONTHLY",
				AppliesToWorkerClass: "clerical and other workers",
				Citation:             cite("Labor Law § 191", "clerical and other workers must be paid at least semimonthly"),
			},
		},
		FinalPayDeadlines: []FinalPayDeadline{
			{
				ID:                  "ny-final-pay-next-payday",
				Trigger:             "termination_any",
				DeadlineDescription: "all wages due by the next regular payday following termination, whether voluntary or involuntary",
				Citation:            cite("Labor Law § 195", "final wages due by next regular payday"),
			},
		},
		PayTransparencyDuties: []PayTransparencyDuty{
			{
				ID:      "ny-pay-transparency-internal-promotion",
				Trigger: "internal_promotion",
				RequiredDisclosure: "a good-faith wage range (minimum and maximum, or minimum and midpoint) and job description " +
					"must be disclosed for the promotion opportunity",
				Citation: cite("Labor Law § 194-b", "wage-range disclosure for promotions and transfers"),
			},
		},
		NonCompeteThresholds: []NonCompeteThreshold{
			{
				ID:                 "ny-noncompete-reasonableness-recheck",
				ReCheckOnPayChange: true,
				Rule: "non-competes remain enforceable only if reasonable in scope, duration, and geography under the common-law " +
					"BDO Seidman standard; a material pay or role change should trigger re-review of continued reasonableness",
				Citation: cite("BDO Seidman v. Hirshberg, 93 N.Y.2d 382 (1999)", "common-law reasonableness standard for non-competes"),
			},
		},
		MiniWARNTriggers: []MiniWARNTrigger{
			{
				ID:                "ny-warn-mass-layoff",
				EmployeeThreshold: 25,
				LayoffWindowDays:  30,
				NoticeDays:        90,
				Note: "NY WARN applies to employers with 50+ employees for a layoff of 25+ employees (or 1/3 of workforce if under " +
					"75 employees) within 30 days",
				Citation: cite("Labor Law Art. 25-A", "90-day mass-layoff notice, stricter than federal WARN"),
			},
		},
	}, nil
}
