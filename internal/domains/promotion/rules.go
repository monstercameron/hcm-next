package promotion

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/payband"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// PreflightPromotion evaluates a proposed promotion against the governed
// worker read, the pay-band catalog and the tenant policy, and returns the
// typed verdict, the findings and the exact input snapshot.
//
// It writes nothing, reserves nothing and calls nothing outbound. The catalog
// argument may be nil; the band check then reports UNKNOWN rather than
// silently passing, because an unevaluated band and an in-band amount must
// never look the same to a reviewer.
func PreflightPromotion(ctx context.Context, catalog rewards.PayBandCatalog, req PreflightRequest) (PreflightResult, error) {
	if err := req.Validate(); err != nil {
		return PreflightResult{}, err
	}

	baseline, findings := baselineFrom(req.WorkerState)

	// A withheld subject stops here. Running the remaining rules would let a
	// caller infer the worker's grade and pay from which findings came back.
	if statusFor(findings) == StatusDenied && req.WorkerState.Disclosure == people.DisclosureWithheld {
		return assemble(req, baseline, findings, rewards.BandResult{State: rewards.BandResultNotRequested}, nil)
	}

	findings = append(findings, checkPlacement(req, baseline)...)
	compFindings, comparable := checkCompensation(req)
	findings = append(findings, compFindings...)
	findings = append(findings, checkEffectiveDate(req, baseline)...)

	band := rewards.BandResult{State: rewards.BandResultNotRequested}
	var bandQuery *rewards.BandQuery
	if q, ok := req.bandQuery(baseline); ok {
		bandQuery = &q
		var err error
		band, err = evaluateBand(ctx, catalog, q, req)
		if err != nil {
			return PreflightResult{}, err
		}
		findings = append(findings, bandFindings(band)...)
	}

	if comparable {
		increase, err := increasePercent(req)
		if err != nil {
			return PreflightResult{}, err
		}
		findings = append(findings, checkIncrease(req, increase)...)
		findings = append(findings, checkBudget(req)...)
	} else if req.Policy.RequireBudgetAuthority && req.Budget == nil {
		findings = append(findings, Finding{
			Code:     CodeBudgetAuthorityMissing,
			Severity: SeverityBlocking,
			Field:    "budget",
			Message:  "policy requires a workforce budget authority reference for a promotion",
		})
	}

	return assemble(req, baseline, findings, band, bandQuery)
}

// checkPlacement validates the target job, grade and the worker's eligibility
// to be moved at all.
func checkPlacement(req PreflightRequest, baseline WorkerBaseline) []Finding {
	var findings []Finding
	if req.Target.JobCode == "" {
		findings = append(findings, Finding{
			Code:     CodeTargetJobRequired,
			Severity: SeverityBlocking,
			Field:    "target.job_code",
			Message:  "a promotion must name the target job",
		})
	}
	if req.Target.Grade == "" {
		findings = append(findings, Finding{
			Code:     CodeTargetGradeRequired,
			Severity: SeverityBlocking,
			Field:    "target.grade",
			Message:  "a promotion must name the target grade",
		})
	}
	// Only judge activity when the read actually produced a status; an empty
	// string here means a NEEDS_DATA finding was already raised for it.
	if baseline.LifecycleStatus != "" && baseline.LifecycleStatus != lifecycleActive {
		findings = append(findings, Finding{
			Code:     CodeWorkerNotActive,
			Severity: SeverityBlocking,
			Field:    people.FieldLifecycleStatus.String(),
			Message:  "worker lifecycle status is " + baseline.LifecycleStatus + "; a promotion requires an active worker",
		})
	}
	if baseline.EmploymentStatus != "" && baseline.EmploymentStatus != lifecycleActive {
		findings = append(findings, Finding{
			Code:     CodeWorkerNotActive,
			Severity: SeverityBlocking,
			Field:    people.FieldEmploymentStatus.String(),
			Message:  "employment status is " + baseline.EmploymentStatus + "; a promotion requires an active employment",
		})
	}
	if !req.Policy.AllowSameGrade && req.Target.Grade != "" && baseline.Grade != "" &&
		req.Target.Grade == baseline.Grade {
		findings = append(findings, Finding{
			Code:     CodeSameGrade,
			Severity: SeverityBlocking,
			Field:    "target.grade",
			Message:  "target grade equals the current grade; this is a lateral move, not a promotion",
		})
	}
	return findings
}

// checkCompensation ports the legacy compensation preflight rules. The second
// result reports whether the two sides are comparable enough for the relative
// checks (increase threshold, budget) to mean anything.
func checkCompensation(req PreflightRequest) ([]Finding, bool) {
	var findings []Finding

	currentBase, currentOK := req.Current.Base.Get()
	proposedBase, proposedOK := req.Proposed.Base.Get()

	if !currentOK || currentBase.Amount().Sign() <= 0 {
		findings = append(findings, Finding{
			Code:     CodeCurrentAmountInvalid,
			Severity: SeverityBlocking,
			Field:    "current.base",
			Message:  "current base pay must be a disclosed amount greater than zero",
		})
	}
	if !proposedOK || proposedBase.Amount().Sign() <= 0 {
		findings = append(findings, Finding{
			Code:     CodeProposedAmountInvalid,
			Severity: SeverityBlocking,
			Field:    "proposed.base",
			Message:  "proposed base pay must be a disclosed amount greater than zero",
		})
	}
	if proposedOK && proposedBase.Currency() == "" {
		findings = append(findings, Finding{
			Code:     CodeCurrencyRequired,
			Severity: SeverityBlocking,
			Field:    "proposed.base.currency",
			Message:  "proposed compensation currency is required",
		})
	}
	if !req.Proposed.PayBasis.Valid() {
		findings = append(findings, Finding{
			Code:     CodePayBasisRequired,
			Severity: SeverityBlocking,
			Field:    "proposed.pay_basis",
			Message:  "proposed compensation pay basis is required",
		})
	}
	if req.Policy.RequireBusinessReason && req.BusinessReason == "" {
		findings = append(findings, Finding{
			Code:     CodeBusinessReasonRequired,
			Severity: SeverityBlocking,
			Field:    "business_reason",
			Message:  "business reason is required",
		})
	}

	if !currentOK || !proposedOK {
		return findings, false
	}
	if currentBase.Currency() != proposedBase.Currency() {
		findings = append(findings, Finding{
			Code:     CodeCurrencyChangeNotV0,
			Severity: SeverityBlocking,
			Field:    "proposed.base.currency",
			Message: fmt.Sprintf("currency changes are not supported: current %s, proposed %s",
				currentBase.Currency(), proposedBase.Currency()),
		})
		return findings, false
	}
	if req.Policy.RequireIncrease {
		cmp, err := proposedBase.Cmp(currentBase)
		if err == nil && cmp <= 0 && req.Current.PayBasis == req.Proposed.PayBasis {
			findings = append(findings, Finding{
				Code:     CodeNotARaise,
				Severity: SeverityBlocking,
				Field:    "proposed.base",
				Message:  "the proposed amount must be greater than the current amount",
			})
		}
	}
	return findings, currentBase.Amount().Sign() > 0 && proposedBase.Amount().Sign() > 0
}

// checkEffectiveDate ports the legacy retroactivity window and adds the
// hire-date floor, which the legacy block never had because it never read
// employment facts.
func checkEffectiveDate(req PreflightRequest, baseline WorkerBaseline) []Finding {
	var findings []Finding
	if req.EffectiveDate.Validate() != nil {
		return append(findings, Finding{
			Code:     CodeEffectiveAtRequired,
			Severity: SeverityBlocking,
			Field:    "effective_date",
			Message:  "effective date is required and must be a valid business date",
		})
	}
	earliest := req.EvaluationDate.AddDays(-req.Policy.MaxRetroactiveDays)
	if req.EffectiveDate.Compare(earliest) < 0 {
		findings = append(findings, Finding{
			Code:     CodeEffectiveAtTooFarPast,
			Severity: SeverityBlocking,
			Field:    "effective_date",
			Message: fmt.Sprintf("effective date %s is more than %d days before the evaluation date %s",
				req.EffectiveDate, req.Policy.MaxRetroactiveDays, req.EvaluationDate),
		})
	}
	if baseline.HireDate.IsSet() && req.EffectiveDate.Compare(baseline.HireDate) < 0 {
		findings = append(findings, Finding{
			Code:     CodeEffectiveBeforeHire,
			Severity: SeverityBlocking,
			Field:    "effective_date",
			Message: fmt.Sprintf("effective date %s precedes the hire date %s",
				req.EffectiveDate, baseline.HireDate),
		})
	}
	return findings
}

// increasePercent computes the annualized relative increase using the same
// rewards arithmetic the simulation will use, so the advisory threshold and
// the simulated delta can never disagree.
func increasePercent(req PreflightRequest) (values.Decimal, error) {
	currentAnnual, err := req.Annualization.Annualize(req.Current.BaseAmount(), req.Current.PayBasis)
	if err != nil {
		return values.Decimal{}, nil
	}
	proposedAnnual, err := req.Annualization.Annualize(req.Proposed.BaseAmount(), req.Proposed.PayBasis)
	if err != nil {
		return values.Decimal{}, nil
	}
	delta, err := proposedAnnual.Sub(currentAnnual)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("promotion: annualized delta: %w", err)
	}
	if currentAnnual.Amount().IsZero() {
		return values.NewDecimal("0", rewards.IncreasePercentScale, rewards.IncreasePercentRounding)
	}
	fraction, err := delta.Amount().Div(currentAnnual.Amount(), 8, rewards.IncreasePercentRounding)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("promotion: increase fraction: %w", err)
	}
	hundred := values.MustDecimal("100", 0, rewards.IncreasePercentRounding)
	return fraction.Mul(hundred, rewards.IncreasePercentScale, rewards.IncreasePercentRounding)
}

// checkIncrease ports the legacy ten-percent advisory. It stays advisory: the
// legacy block warned and continued, and turning a warning into a block is a
// behaviour change a regression fixture would not catch.
func checkIncrease(req PreflightRequest, increase values.Decimal) []Finding {
	if increase.Validate() != nil {
		return nil
	}
	if increase.Cmp(req.Policy.LargeIncreasePercent) <= 0 {
		return nil
	}
	return []Finding{{
		Code:     CodeIncreaseOverThreshold,
		Severity: SeverityAdvisory,
		Field:    "proposed.base",
		Message: fmt.Sprintf("annualized increase of %s%% exceeds the %s%% review threshold",
			increase, req.Policy.LargeIncreasePercent),
	}}
}

// checkBudget evaluates the workforce budget observation.
//
// Every path here is careful about one thing: a P1A budget figure is an
// observation of an incumbent finance system, never a reservation. Even a
// comfortably sufficient pool produces an advisory saying so, because the
// alternative is a reviewer reading silence as an authorization.
func checkBudget(req PreflightRequest) []Finding {
	if req.Budget == nil {
		if !req.Policy.RequireBudgetAuthority {
			return nil
		}
		return []Finding{{
			Code:     CodeBudgetAuthorityMissing,
			Severity: SeverityBlocking,
			Field:    "budget",
			Message:  "policy requires a workforce budget authority reference for a promotion",
		}}
	}
	if err := req.Budget.Validate(); err != nil {
		return []Finding{{
			Code:     CodeBudgetAuthorityMissing,
			Severity: SeverityBlocking,
			Field:    "budget",
			Message:  "budget authority reference is incomplete: " + err.Error(),
		}}
	}

	findings := []Finding{{
		Code:     CodeBudgetObservationOnly,
		Severity: SeverityAdvisory,
		Field:    "budget",
		Message: fmt.Sprintf("budget %s from %s is an observation at baseline %s, not a reservation",
			req.Budget.Scope, req.Budget.OwnerSystem, req.Budget.BaselineVersion),
	}}

	available, ok := req.Budget.AvailableAmount.Get()
	if !ok {
		return append(findings, Finding{
			Code:     CodeBudgetObservedShort,
			Severity: SeverityNeedsData,
			Field:    "budget.available_amount",
			Message:  "available budget is " + req.Budget.AvailableAmount.State().String() + "; the promotion cost cannot be compared against it",
		})
	}

	currentAnnual, err := req.Annualization.Annualize(req.Current.BaseAmount(), req.Current.PayBasis)
	if err != nil {
		return findings
	}
	proposedAnnual, err := req.Annualization.Annualize(req.Proposed.BaseAmount(), req.Proposed.PayBasis)
	if err != nil {
		return findings
	}
	cost, err := proposedAnnual.Sub(currentAnnual)
	if err != nil {
		return findings
	}
	cmp, err := available.Cmp(cost)
	if err != nil {
		return append(findings, Finding{
			Code:     CodeBudgetObservedShort,
			Severity: SeverityNeedsData,
			Field:    "budget.available_amount",
			Message:  "observed budget cannot be compared with the promotion cost: " + err.Error(),
		})
	}
	if cmp < 0 {
		findings = append(findings, Finding{
			Code:     CodeBudgetObservedShort,
			Severity: SeverityAdvisory,
			Field:    "budget.available_amount",
			Message: fmt.Sprintf("observed available budget %s is less than the annualized cost %s",
				available, cost),
		})
	}
	return findings
}

// evaluateBand resolves the band and turns a miss into an UNKNOWN result. A
// catalog fault, by contrast, is an error: a promotion evaluated against a
// catalog that failed is not a promotion that had no band.
func evaluateBand(ctx context.Context, catalog rewards.PayBandCatalog, q rewards.BandQuery, req PreflightRequest) (rewards.BandResult, error) {
	if catalog == nil {
		return rewards.BandResult{State: rewards.BandResultUnknown, Reason: "no pay band catalog was wired"}, nil
	}
	annualized, err := req.Annualization.Annualize(req.Proposed.BaseAmount(), req.Proposed.PayBasis)
	if err != nil {
		return rewards.BandResult{State: rewards.BandResultUnknown, Reason: err.Error()}, nil
	}
	evaluation, err := rewards.EvaluatePayBandPosition(ctx, catalog, q, annualized)
	switch {
	case err == nil:
		return rewards.BandResult{State: rewards.BandResultEvaluated, Evaluation: evaluation}, nil
	case errors.Is(err, rewards.ErrBandNotFound):
		return rewards.BandResult{State: rewards.BandResultUnknown, Reason: rewards.ErrBandNotFound.Error()}, nil
	default:
		return rewards.BandResult{}, fmt.Errorf("%w: %w", ErrCatalogFailed, err)
	}
}

// bandFindings turns a band result into findings. An unresolved band is
// NEEDS_DATA rather than a pass; an out-of-band amount takes its severity from
// the band's own advisory/blocking flag.
func bandFindings(band rewards.BandResult) []Finding {
	switch band.State {
	case rewards.BandResultUnknown:
		return []Finding{{
			Code:     CodePayBandNotFound,
			Severity: SeverityNeedsData,
			Field:    "target.grade",
			Message:  "no pay band could be evaluated for the target placement: " + band.Reason,
		}}
	case rewards.BandResultEvaluated:
		severity := SeverityAdvisory
		if band.Evaluation.Outcome == rewards.BandOutcomeExceptionBlocking {
			severity = SeverityBlocking
		}
		switch band.Evaluation.Position.Placement {
		case payband.PlacementBelowMinimum:
			return []Finding{{
				Code:     CodeBelowBandMinimum,
				Severity: severity,
				Field:    "proposed.base",
				Message: fmt.Sprintf("annualized base %s is below the band minimum (compa-ratio %s)",
					band.Evaluation.Position.Amount, band.Evaluation.Position.CompaRatio),
			}}
		case payband.PlacementAboveMaximum:
			return []Finding{{
				Code:     CodeAboveBandMaximum,
				Severity: severity,
				Field:    "proposed.base",
				Message: fmt.Sprintf("annualized base %s is above the band maximum (compa-ratio %s)",
					band.Evaluation.Position.Amount, band.Evaluation.Position.CompaRatio),
			}}
		}
	}
	return nil
}

// assemble sorts the findings, computes the verdict, digests the input and the
// result and mints the zero-effect receipt.
func assemble(req PreflightRequest, baseline WorkerBaseline, findings []Finding, band rewards.BandResult, bandQuery *rewards.BandQuery) (PreflightResult, error) {
	sortFindings(findings)
	if findings == nil {
		findings = []Finding{}
	}

	snapshot := InputSnapshot{
		Tenant:         req.Tenant,
		Subject:        req.Subject,
		Baseline:       baseline,
		Target:         req.Target,
		Current:        req.Current,
		Proposed:       req.Proposed,
		EffectiveDate:  req.EffectiveDate,
		EvaluationDate: req.EvaluationDate,
		BusinessReason: req.BusinessReason,
		Budget:         req.Budget,
		Policy:         req.Policy,
		Annualization:  req.Annualization,
		BandQuery:      bandQuery,
	}

	inputsDigest, err := canonicalbytes.New("hcmnext.domains.promotion.PreflightRequest", promotionSchemaVer).
		String("intent_type", IntentType).
		String("intent_version", IntentVersion).
		Value("input", snapshot).
		String("worker_state_digest", req.WorkerState.ResultDigest).
		Digest()
	if err != nil {
		return PreflightResult{}, err
	}

	result := PreflightResult{
		IntentType:      IntentType,
		IntentVersion:   IntentVersion,
		Status:          statusFor(findings),
		Findings:        findings,
		Input:           snapshot,
		Band:            band,
		PolicyVersion:   req.Policy.Version,
		RulePackVersion: RulePackVersion,
		InputsDigest:    inputsDigest,
		Effects:         evidence.ZeroEffects(),
	}
	body, err := result.canonicalBody()
	if err != nil {
		return PreflightResult{}, err
	}
	result.ResultDigest = canonicalbytes.Digest(body)

	controls := []evidence.ControlVersion{
		{Name: "promotion_policy", Version: req.Policy.Version},
		{Name: "promotion_rule_pack", Version: RulePackVersion},
		{Name: "annualization_rule", Version: req.Annualization.Version},
	}
	if band.State == rewards.BandResultEvaluated {
		controls = append(controls,
			evidence.ControlVersion{Name: "pay_band_catalog", Version: band.Evaluation.CatalogVersion},
			evidence.ControlVersion{Name: "pay_band_rule_pack", Version: rewards.BandRulePackVersion})
	}
	receipt, err := evidence.NewZeroEffectReceipt(
		IntentType, IntentVersion,
		evidence.ModePreflight, evidence.RequestStatePreflighted,
		controls, inputsDigest, result.ResultDigest, result.Effects,
	)
	if err != nil {
		return PreflightResult{}, err
	}
	result.Receipt = receipt
	return result, nil
}
