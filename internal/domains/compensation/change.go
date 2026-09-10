package compensation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrBudgetFenceExpired reports a reservation against a dead budget fence.
	ErrBudgetFenceExpired = errors.New("compensation: budget fence expired")

	// ErrStaleContext reports a budget fence whose band, payroll or legal
	// context no longer matches the reservation.
	ErrStaleContext = errors.New("compensation: budget context is stale")

	// ErrOverBudget reports a reservation whose annualized total exceeds
	// the fence amount.
	ErrOverBudget = errors.New("compensation: over budget")

	// ErrUnknownRepairTarget reports a repair naming an effect the commit
	// never produced: repair never invents work.
	ErrUnknownRepairTarget = errors.New("compensation: unknown repair target")

	// ErrObservationMismatch reports a repair observation that does not
	// match its failed effect.
	ErrObservationMismatch = errors.New("compensation: repair observation mismatch")
)

// ChangeLine is one fixture line: salary, hourly, allowance, bonus,
// correction or retro money with its operation and presence.
type ChangeLine struct {
	Type              ComponentType
	Op                ComponentOp
	Revision          uint64
	Amount            values.Decimal
	HasAmount         bool
	Currency          string
	Frequency         string
	EndCondition      string
	CorrectionOf      string
	RetroactiveReason string
}

// BudgetFence is one live budget bound with its context digests.
type BudgetFence struct {
	BudgetID      string
	Amount        values.Decimal
	ExpiresAt     time.Time
	BandDigest    string
	PayrollDigest string
	LegalDigest   string
}

// ChangeRequest is one standalone compensation change.
type ChangeRequest struct {
	PackageRef      string
	EffectiveDate   string
	SnapshotDigest  string
	Approvals       []string
	TransactionPlan string
	Fence           uint64
	Lines           []ChangeLine
	Budget          BudgetFence
	Disclosure      AuthorizationDecision
	HoursPerYear    values.Decimal
	Now             time.Time
}

// AnnualizedLine is one line annualized to exact decimals.
type AnnualizedLine struct {
	Name   string
	Annual values.Decimal
}

// ChangeSimulation is one zero-effect simulation: composition, exact
// annualization and totals under one digest. BudgetDigest freezes the
// fence context the simulation priced against.
type ChangeSimulation struct {
	Composed     ComposedPackage
	Annual       []AnnualizedLine
	TotalAnnual  values.Decimal
	BudgetDigest string
	Digest       string
}

// ChangeReservation binds one simulation to one live budget fence.
type ChangeReservation struct {
	SimulationDigest string
	BudgetID         string
	ReservedAt       time.Time
}

// CommittedEffect is one atomically applied child effect.
type CommittedEffect struct {
	EffectID string
	Digest   string
	Applied  bool
}

// CommittedChange is one atomic package commit.
type CommittedChange struct {
	PackageDigest string
	Effects       []CommittedEffect
}

// RepairOutcome redrives failed effects only.
type RepairOutcome struct {
	Redriven  []string
	Untouched []string
}

func annualize(amount values.Decimal, frequency string, hours values.Decimal) (values.Decimal, error) {
	rounding, err := values.ParseRoundingMode("HALF_UP")
	if err != nil {
		return values.Decimal{}, err
	}
	twelve, err := values.NewDecimal("12", 2, rounding)
	if err != nil {
		return values.Decimal{}, err
	}
	switch strings.ToLower(frequency) {
	case "annual":
		return amount, nil
	case "monthly":
		return amount.Mul(twelve, 2, rounding)
	case "hourly":
		return amount.Mul(hours, 2, rounding)
	default:
		return values.Decimal{}, fmt.Errorf("compensation: annualize: %w", ErrInvalidPackage)
	}
}

// SimulateChange composes one change and annualizes every line with exact
// decimals. Simulation commits nothing: it returns evidence, never effects.
func SimulateChange(current []CurrentComponent, req ChangeRequest, policy CompositionPolicy) (ChangeSimulation, error) {
	proposals := make([]ComponentProposal, 0, len(req.Lines))
	for _, line := range req.Lines {
		proposals = append(proposals, ComponentProposal(line))
	}
	composed, err := ComposePackage(current, PackageProposal{
		PackageRef: req.PackageRef, EffectiveDate: req.EffectiveDate,
		SnapshotDigest: req.SnapshotDigest, Approvals: req.Approvals,
		TransactionPlan: req.TransactionPlan, Fence: req.Fence, Components: proposals,
	}, policy)
	if err != nil {
		return ChangeSimulation{}, err
	}
	rounding, err := values.ParseRoundingMode("HALF_UP")
	if err != nil {
		return ChangeSimulation{}, err
	}
	zero, err := values.NewDecimal("0", 2, rounding)
	if err != nil {
		return ChangeSimulation{}, err
	}
	simulation := ChangeSimulation{Composed: composed, TotalAnnual: zero}
	for _, line := range req.Lines {
		if !line.HasAmount {
			continue
		}
		annual, err := annualize(line.Amount, line.Frequency, req.HoursPerYear)
		if err != nil {
			return ChangeSimulation{}, err
		}
		simulation.Annual = append(simulation.Annual, AnnualizedLine{
			Name: string(line.Type), Annual: annual,
		})
		simulation.TotalAnnual, err = simulation.TotalAnnual.Add(annual)
		if err != nil {
			return ChangeSimulation{}, err
		}
	}
	simulation.BudgetDigest = budgetDigest(req.Budget)
	parts := []string{"change", composed.Digest, simulation.TotalAnnual.String(), simulation.BudgetDigest}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	simulation.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return simulation, nil
}

// budgetDigest binds one fence to its amount and context.
func budgetDigest(budget BudgetFence) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		budget.BudgetID, budget.Amount.String(), budget.BandDigest,
		budget.PayrollDigest, budget.LegalDigest,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ReserveChange binds one simulation to one live budget fence.
func ReserveChange(simulation ChangeSimulation, req ChangeRequest) (ChangeReservation, error) {
	if !req.Now.Before(req.Budget.ExpiresAt) {
		return ChangeReservation{}, fmt.Errorf("compensation: ReserveChange: %w", ErrBudgetFenceExpired)
	}
	if strings.TrimSpace(req.Budget.BandDigest) == "" || strings.TrimSpace(req.Budget.PayrollDigest) == "" ||
		strings.TrimSpace(req.Budget.LegalDigest) == "" || strings.TrimSpace(req.Budget.BudgetID) == "" {
		return ChangeReservation{}, fmt.Errorf("compensation: ReserveChange: %w", ErrStaleContext)
	}
	if budgetDigest(req.Budget) != simulation.BudgetDigest {
		return ChangeReservation{}, fmt.Errorf("compensation: ReserveChange: %w", ErrStaleContext)
	}
	if simulation.TotalAnnual.Cmp(req.Budget.Amount) > 0 {
		return ChangeReservation{}, fmt.Errorf("compensation: ReserveChange: %w", ErrOverBudget)
	}
	return ChangeReservation{
		SimulationDigest: simulation.Digest,
		BudgetID:         req.Budget.BudgetID,
		ReservedAt:       req.Now,
	}, nil
}

var committedFields = []FieldID{FieldAmount, FieldComponentType, FieldFrequency, FieldEffectiveInterval}

// CommitChange applies one reservation atomically: disclosure first, then
// the whole effect set together. Partial package writes cannot exist:
// there is no partial path.
func CommitChange(reservation ChangeReservation, simulation ChangeSimulation, req ChangeRequest) (CommittedChange, error) {
	if reservation.SimulationDigest != simulation.Digest {
		return CommittedChange{}, fmt.Errorf("compensation: CommitChange: %w", ErrInvalidPackage)
	}
	if err := req.Disclosure.Covers(committedFields); err != nil {
		return CommittedChange{}, fmt.Errorf("compensation: CommitChange: %w", ErrUnauthorizedFact)
	}
	for _, field := range committedFields {
		if req.Disclosure.Fields[field].Effect != EffectAllow {
			return CommittedChange{}, fmt.Errorf("compensation: CommitChange %s: %w", field, ErrUnauthorizedFact)
		}
	}
	committed := CommittedChange{PackageDigest: simulation.Digest}
	for i, child := range simulation.Composed.Children {
		id := fmt.Sprintf("effect/%s/%d", child.Component, i)
		sum := sha256.Sum256([]byte(simulation.Digest + "\x00" + id))
		committed.Effects = append(committed.Effects, CommittedEffect{
			EffectID: id,
			Digest:   "sha256:" + hex.EncodeToString(sum[:]),
			Applied:  true,
		})
	}
	return committed, nil
}

// RepairChange redrives failed effects only: every target must come from
// the commit with a matching observation, and healthy effects stay
// untouched.
func RepairChange(committed CommittedChange, failed []string, observations map[string]string) (RepairOutcome, error) {
	known := make(map[string]CommittedEffect, len(committed.Effects))
	for _, effect := range committed.Effects {
		known[effect.EffectID] = effect
	}
	var outcome RepairOutcome
	for _, id := range failed {
		effect, ok := known[id]
		if !ok {
			return RepairOutcome{}, fmt.Errorf("compensation: RepairChange %s: %w", id, ErrUnknownRepairTarget)
		}
		if observations[id] != effect.Digest {
			return RepairOutcome{}, fmt.Errorf("compensation: RepairChange %s: %w", id, ErrObservationMismatch)
		}
		outcome.Redriven = append(outcome.Redriven, id)
	}
	redriven := make(map[string]bool, len(outcome.Redriven))
	for _, id := range outcome.Redriven {
		redriven[id] = true
	}
	for _, effect := range committed.Effects {
		if !redriven[effect.EffectID] {
			outcome.Untouched = append(outcome.Untouched, effect.EffectID)
		}
	}
	sort.Strings(outcome.Redriven)
	sort.Strings(outcome.Untouched)
	return outcome, nil
}
