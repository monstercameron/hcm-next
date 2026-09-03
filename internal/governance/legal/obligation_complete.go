package legal

import "fmt"

// releaseCompleter is implemented by the typed bodies that carry a field the
// contract's section 4.2 declares mandatory but that a drafted research file
// may leave unstated — a day count, a floor amount, an enumerated list.
//
// The split exists because the two failure modes are not equally bad. An
// extractor that invents a thirty-day deadline to satisfy a validator has
// broadened a research claim into a statutory requirement, which the
// contract's section 7.1 forbids outright. An extractor that records "this
// state has a personnel-file duty and the research does not state the
// deadline" has narrowed, which is allowed. So an UNREVIEWED draft may carry
// the gap and say so, and [RulePack.ValidateForRelease] closes it the moment
// a pack claims a releasable review status.
type releaseCompleter interface {
	validateComplete() error
}

// requireStated reports which fields a releasable rule still leaves unstated.
func requireStated(kind, id string, missing []string) error {
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("legal: %s %q cannot be released: %v unstated in the source", kind, id, missing)
}

// The five original kinds below also gained an "unstated is legal on a draft"
// relaxation, for the same reason as the twelve: an extracted Missouri notice
// rule that says "a notice duty exists and the research does not state the
// window" is narrower, and therefore safer, than one that invents a
// seven-day window because a validator demanded a positive integer.

func (o NoticeObligation) validateComplete() error {
	var missing []string
	if o.TimingDirection == "" {
		missing = append(missing, "timing_direction")
	}
	if o.TimingDays == 0 {
		missing = append(missing, "timing_days")
	}
	if o.Channel == "" {
		missing = append(missing, "channel")
	}
	return requireStated("notice", o.ID, missing)
}

func (o FieldRestriction) validateComplete() error {
	var missing []string
	if len(o.RestrictedFields) == 0 {
		missing = append(missing, "restricted_fields")
	}
	return requireStated("field restriction", o.ID, missing)
}

func (o RetentionRule) validateComplete() error {
	var missing []string
	if o.RecordClass == "" {
		missing = append(missing, "record_class")
	}
	if o.DurationYears == 0 {
		missing = append(missing, "duration_years")
	}
	return requireStated("retention rule", o.ID, missing)
}

func (o LeaveInteraction) validateComplete() error {
	var missing []string
	if o.LeaveType == "" {
		missing = append(missing, "leave_type")
	}
	if o.InteractionRule == "" {
		missing = append(missing, "interaction_rule")
	}
	return requireStated("leave interaction", o.ID, missing)
}

func (o PayFrequencyConstraint) validateComplete() error {
	var missing []string
	if o.MinimumFrequency == "" {
		missing = append(missing, "minimum_frequency")
	}
	return requireStated("pay frequency constraint", o.ID, missing)
}

func (o WageFloorRule) validateComplete() error {
	var missing []string
	if o.FloorAmount.Validate() != nil {
		missing = append(missing, "floor_amount")
	}
	if o.Basis == "" {
		missing = append(missing, "basis")
	}
	if o.Indexation == "" {
		missing = append(missing, "indexation")
	}
	return requireStated("wage floor", o.ID, missing)
}

func (o PayEquityReviewRule) validateComplete() error {
	var missing []string
	if len(o.ProtectedBases) == 0 {
		missing = append(missing, "protected_bases")
	}
	if o.ComparatorStandard == "" {
		missing = append(missing, "comparator_standard")
	}
	return requireStated("pay equity review", o.ID, missing)
}

func (o PayStatementRule) validateComplete() error {
	var missing []string
	if len(o.RequiredFields) == 0 {
		missing = append(missing, "required_fields")
	}
	if o.Delivery == "" {
		missing = append(missing, "delivery")
	}
	return requireStated("pay statement", o.ID, missing)
}

func (o ClassificationRule) validateComplete() error {
	var missing []string
	if o.Dimension == "" {
		missing = append(missing, "dimension")
	}
	if o.TestDescription == "" {
		missing = append(missing, "test_description")
	}
	return requireStated("classification", o.ID, missing)
}

func (o PersonnelFileRule) validateComplete() error {
	var missing []string
	if o.ResponseDays == 0 {
		missing = append(missing, "response_days")
	}
	if o.DayBasis == "" {
		missing = append(missing, "day_basis")
	}
	return requireStated("personnel file", o.ID, missing)
}

func (o AntiRetaliationRule) validateComplete() error {
	var missing []string
	if len(o.ProtectedActivities) == 0 {
		missing = append(missing, "protected_activities")
	}
	if o.LookbackDays == 0 {
		missing = append(missing, "lookback_days")
	}
	if o.Disposition == "" {
		missing = append(missing, "disposition")
	}
	return requireStated("anti-retaliation", o.ID, missing)
}

func (o JobSecurityRule) validateComplete() error {
	var missing []string
	if o.StandardKind == "" {
		missing = append(missing, "standard")
	}
	return requireStated("job security", o.ID, missing)
}

func (o SeparationFilingRule) validateComplete() error {
	var missing []string
	if o.FormName == "" {
		missing = append(missing, "form_name")
	}
	if o.RecipientAuthority == "" {
		missing = append(missing, "recipient_authority")
	}
	if o.DeadlineDays == 0 {
		missing = append(missing, "deadline_days")
	}
	if o.DayBasis == "" {
		missing = append(missing, "day_basis")
	}
	return requireStated("separation filing", o.ID, missing)
}

func (o DrugTestingRule) validateComplete() error {
	var missing []string
	if len(o.PermittedBases) == 0 {
		missing = append(missing, "permitted_bases")
	}
	return requireStated("drug testing", o.ID, missing)
}

func (o BreachNotificationRule) validateComplete() error {
	var missing []string
	if o.SubjectDeadlineDays == 0 {
		missing = append(missing, "subject_deadline_days")
	}
	if o.DayBasis == "" {
		missing = append(missing, "day_basis")
	}
	return requireStated("breach notification", o.ID, missing)
}

func (o AutomatedDecisionRule) validateComplete() error {
	var missing []string
	if len(o.CoveredUses) == 0 {
		missing = append(missing, "covered_uses")
	}
	return requireStated("automated decision", o.ID, missing)
}

func (o MonitoringConsentRule) validateComplete() error {
	var missing []string
	if len(o.DataCategories) == 0 {
		missing = append(missing, "data_categories")
	}
	if o.ConsentForm == "" {
		missing = append(missing, "consent_form")
	}
	return requireStated("monitoring consent", o.ID, missing)
}
