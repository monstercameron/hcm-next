package simcomp

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/budget"
	"github.com/monstercameron/hcm-next/internal/domains/promotion/simassign"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// schemaVersion pins the canonical encoding this package digests under.
const schemaVersion = 1

// The Rewards and Finance effect kinds a promotion implies. They are typed
// with simassign's [simassign.EffectKind] so the union of the two halves of one
// promotion simulation is one set, not two sets that have to be reconciled.
const (
	// EffectCompensationRevision is the base-pay change as a proposed
	// effective-dated compensation revision.
	EffectCompensationRevision simassign.EffectKind = "rewards.compensation.revision"
	// EffectBudgetReservation is the compensation-pool reservation the change
	// consumes.
	EffectBudgetReservation simassign.EffectKind = "budget.compensation_pool.reservation"
)

// EffectKinds returns the kinds this package can propose, in declaration
// order.
func EffectKinds() []simassign.EffectKind {
	return []simassign.EffectKind{EffectCompensationRevision, EffectBudgetReservation}
}

// Refusal reasons this package adds to simassign's shared vocabulary. They are
// typed with the same [simassign.Reason] so a caller reading the union of both
// halves' refusals reads one closed vocabulary.
const (
	// ReasonInsufficientBudget means the observed pool cannot fund the
	// reservation the change would need.
	ReasonInsufficientBudget simassign.Reason = "INSUFFICIENT_BUDGET"
	// ReasonCurrencyMismatch means the pool and the compensation change are
	// denominated in different currencies, which is an FX decision this
	// package has no authority to take.
	ReasonCurrencyMismatch simassign.Reason = "CURRENCY_MISMATCH"
	// ReasonBudgetUnitMismatch means the observed pool is not measured in
	// money and therefore cannot fund a pay change.
	ReasonBudgetUnitMismatch simassign.Reason = "BUDGET_UNIT_MISMATCH"
	// ReasonEffectiveDateOutsidePayPeriod means the promotion's effective date
	// does not fall inside the declared pay period, so there is nothing to
	// prorate across.
	ReasonEffectiveDateOutsidePayPeriod simassign.Reason = "EFFECTIVE_DATE_OUTSIDE_PAY_PERIOD"
)

// Approval requirement identifiers a band finding can trigger.
//
// They are the same tokens the promote-into-management reference workflow's
// approval graph names (internal/workflow's PromotionApprovalFinance and the
// compensation partner step). They are restated here rather than imported so a
// domain simulation does not depend on the workflow layer; the joint
// conformance test proves the two lists agree.
const (
	// ApprovalFinancePartner is required when the desired pay sits above the
	// band.
	ApprovalFinancePartner = "approval.finance_partner"
	// ApprovalCompensationPartner is required when it sits below the band.
	ApprovalCompensationPartner = "approval.compensation_partner"
)

// BandFinding is where the desired pay sits in the target band, and what that
// position requires of the approval graph.
type BandFinding struct {
	// Evaluated is false when a band-position input was not disclosed, in
	// which case every other field is zero and a [simassign.Refusal] says why.
	Evaluated bool
	// BandID and BandVersion identify the band both positions were placed
	// against.
	BandID      string
	BandVersion string
	// CurrentClass and DesiredClass are the typed BELOW/WITHIN/ABOVE placements
	// COMP-003 produced.
	CurrentClass string
	DesiredClass string
	// DesiredBoundary is the bound the desired placement sits against.
	DesiredBoundary string
	// ApprovalRequirementID is the approval the desired placement triggers, or
	// empty when it triggers none.
	ApprovalRequirementID string
}

// canonical encodes the finding.
func (f BandFinding) canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.simcomp.BandFinding", schemaVersion).
		Bool("evaluated", f.Evaluated).
		String("band_id", f.BandID).
		String("band_version", f.BandVersion).
		String("current_class", f.CurrentClass).
		String("desired_class", f.DesiredClass).
		String("desired_boundary", f.DesiredBoundary).
		String("approval_requirement_id", f.ApprovalRequirementID).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// approvalFor maps a COMP-003 placement class onto the approval it triggers.
// WITHIN triggers none: the base approval graph already covers an in-band
// promotion, and adding one here would double-approve every ordinary raise.
func approvalFor(class string) string {
	switch class {
	case rewards.PositionClassAbove.String():
		return ApprovalFinancePartner
	case rewards.PositionClassBelow.String():
		return ApprovalCompensationPartner
	default:
		return ""
	}
}

// BasePayChange is the compensation movement in exact decimal, on the COMP-002
// annualized basis the snapshot's band-position inputs were computed on.
type BasePayChange struct {
	// Evaluated is false when the current pay was not disclosed.
	Evaluated bool
	// CurrentAnnualized and DesiredAnnualized are the two annualized amounts
	// the snapshot disclosed.
	CurrentAnnualized values.Money
	DesiredAnnualized values.Money
	// Delta is desired minus current: positive for a raise, negative for a cut,
	// zero for a lateral move.
	Delta values.Money
	// Direction is INCREASE, DECREASE or UNCHANGED.
	Direction string
}

// Pay-change directions.
const (
	DirectionIncrease  = "INCREASE"
	DirectionDecrease  = "DECREASE"
	DirectionUnchanged = "UNCHANGED"
)

// canonical encodes the change.
func (c BasePayChange) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.promotion.simcomp.BasePayChange", schemaVersion).
		Bool("evaluated", c.Evaluated).
		String("direction", c.Direction)
	if c.Evaluated {
		w.Value("current_annualized", c.CurrentAnnualized).
			Value("desired_annualized", c.DesiredAnnualized).
			Value("delta", c.Delta)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Proration is the day-weighted split of the pay period the effective date
// falls in. Every divisor is declared by the caller and carried here, so a
// reader can recompute every number without knowing a convention.
type Proration struct {
	// Evaluated is false when the pay period or the pay change could not be
	// established.
	Evaluated bool

	PeriodID    string
	PeriodStart values.LocalDate
	PeriodEnd   values.LocalDate

	// DaysInPeriod is the half-open [start, end) day count.
	DaysInPeriod int
	// DaysAtPriorRate and DaysAtNewRate split it at the effective date.
	DaysAtPriorRate int
	DaysAtNewRate   int

	// DaysPerYear is the declared divisor both daily rates were computed with.
	DaysPerYear values.Decimal
	// PriorDailyRate and NewDailyRate are the annualized amounts divided by
	// that divisor, at the declared rate scale.
	PriorDailyRate values.Money
	NewDailyRate   values.Money

	// PriorPortion and NewPortion are the two sides of the period.
	PriorPortion values.Money
	NewPortion   values.Money
	// PeriodAmount is their sum: what the period costs with the change.
	PeriodAmount values.Money
	// UnproratedPeriodAmount is what it would have cost without the change.
	UnproratedPeriodAmount values.Money
	// PeriodDelta is PeriodAmount minus UnproratedPeriodAmount.
	PeriodDelta values.Money
}

// canonical encodes the proration.
func (p Proration) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.promotion.simcomp.Proration", schemaVersion).
		Bool("evaluated", p.Evaluated).
		String("period_id", p.PeriodID)
	if p.Evaluated {
		w.Value("period_start", p.PeriodStart).
			Value("period_end", p.PeriodEnd).
			Int("days_in_period", int64(p.DaysInPeriod)).
			Int("days_at_prior_rate", int64(p.DaysAtPriorRate)).
			Int("days_at_new_rate", int64(p.DaysAtNewRate)).
			Value("days_per_year", p.DaysPerYear).
			Value("prior_daily_rate", p.PriorDailyRate).
			Value("new_daily_rate", p.NewDailyRate).
			Value("prior_portion", p.PriorPortion).
			Value("new_portion", p.NewPortion).
			Value("period_amount", p.PeriodAmount).
			Value("unprorated_period_amount", p.UnproratedPeriodAmount).
			Value("period_delta", p.PeriodDelta)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// BudgetPlan is the BUDGET-002 reservation the compensation change would need,
// weighed against the pool the snapshot observed.
type BudgetPlan struct {
	// Evaluated is false when the pool observation was not disclosed.
	Evaluated bool
	// Planned is false when no reservation is needed (the change frees money
	// or costs nothing) or when the pool refused it.
	Planned bool
	// Reason states why a reservation was not planned, when Planned is false
	// and Evaluated is true.
	Reason string

	Scope           string
	Period          string
	BudgetType      string
	Unit            string
	BaselineVersion string

	// Available is the pool's observed available quantity.
	Available values.Money
	// Amount is the reservation the change would take: the annualized delta.
	Amount values.Money
	// Remaining is Available minus Amount, when the pool can fund it.
	Remaining values.Money
	// Sufficient reports whether the pool covers the amount.
	Sufficient bool

	// Request is the exact reservation request that would be issued. It is
	// built and validated, and never submitted.
	Request budget.CompensationReservationRequest
}

// canonical encodes the plan.
func (p BudgetPlan) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.promotion.simcomp.BudgetPlan", schemaVersion).
		Bool("evaluated", p.Evaluated).
		Bool("planned", p.Planned).
		String("reason", p.Reason).
		String("scope", p.Scope).
		String("period", p.Period).
		String("budget_type", p.BudgetType).
		String("unit", p.Unit).
		String("baseline_version", p.BaselineVersion).
		Bool("sufficient", p.Sufficient)
	if p.Evaluated {
		w.Value("available", p.Available).Value("amount", p.Amount)
		if p.Sufficient {
			w.Value("remaining", p.Remaining)
		}
	}
	if p.Planned {
		w.String("request.budget_id", p.Request.BudgetID).
			String("request.tenant_id", p.Request.TenantID).
			String("request.proposal_digest", p.Request.ProposalDigest).
			String("request.authority_digest", p.Request.AuthorityDigest).
			String("request.idempotency_key", p.Request.IdempotencyKey).
			Value("request.amount", p.Request.Amount).
			String("request.currency", p.Request.Currency).
			String("request.expires_at", p.Request.ExpiresAt.UTC().Format(time.RFC3339Nano))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Explain renders the plan in one line, with no value the pool observation did
// not already disclose to this caller.
func (p BudgetPlan) Explain() string {
	if !p.Evaluated {
		return "budget: not evaluated"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "budget %s/%s (%s, %s, baseline %s): available %s, reservation %s, sufficient=%t",
		p.Scope, p.Period, p.BudgetType, p.Unit, p.BaselineVersion, p.Available, p.Amount, p.Sufficient)
	if p.Sufficient {
		fmt.Fprintf(&b, ", remaining %s", p.Remaining)
	}
	if !p.Planned {
		fmt.Fprintf(&b, " (not planned: %s)", p.Reason)
	}
	return b.String()
}
