package snapshot

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/domains/budget"
	"github.com/monstercameron/hcm-next/internal/domains/org"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/domains/position"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	enginesnapshot "github.com/monstercameron/hcm-next/internal/engines/snapshot"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// The eight semantic input names a Promotion input snapshot binds.
//
// They are semantic business inputs, not storage locations: "the subject's
// worker facts" is a question the People domain answers, and it stays the same
// name whether the answer came from a local repository or from an incumbent
// system observed through a connector. Which of those it was is carried by the
// entry's authority class and source reference, never by its name.
const (
	// InputSubjectWorkerFacts is the governed worker read: lifecycle,
	// employment status and hire date for the subject being promoted.
	InputSubjectWorkerFacts = "promotion.subject_worker_facts"
	// InputCurrentPlacement is the job, grade, organizational unit, position
	// and pay zone the subject occupies today -- what the promotion moves them
	// away from.
	InputCurrentPlacement = "promotion.current_placement"
	// InputManagerChain is the subject's resolved manager relationship chain
	// (ORG-002), which the approval graph and the "no cycle" check both read.
	InputManagerChain = "promotion.manager_chain"
	// InputTargetPositionCapacity is the target position's capacity and
	// consumption at the effective date (POSITION-002).
	InputTargetPositionCapacity = "promotion.target_position_capacity"
	// InputTargetPositionVacancy is the earliest date the target position is
	// known to sit vacant with no successor lined up. It is required only when
	// the position has no head available at the effective date; see
	// [Specification].
	InputTargetPositionVacancy = "promotion.target_position_vacancy"
	// InputPayBandPositionCurrent is where the subject's current annualized
	// pay sits in the target band (COMP-002/COMP-003).
	InputPayBandPositionCurrent = "promotion.pay_band_position_current"
	// InputPayBandPositionDesired is where the desired annualized pay sits in
	// the target band.
	InputPayBandPositionDesired = "promotion.pay_band_position_desired"
	// InputBudgetAvailability is the workforce compensation-pool observation
	// the promotion's cost is weighed against (BUDGET-002).
	InputBudgetAvailability = "promotion.budget_availability"
)

// inputOwners names the domain that owns each input's meaning. It is written
// out rather than derived from the name so that renaming an input cannot
// silently reassign its ownership.
var inputOwners = map[string]string{
	InputSubjectWorkerFacts:     "people",
	InputCurrentPlacement:       "people",
	InputManagerChain:           "org",
	InputTargetPositionCapacity: "position",
	InputTargetPositionVacancy:  "position",
	InputPayBandPositionCurrent: "rewards",
	InputPayBandPositionDesired: "rewards",
	InputBudgetAvailability:     "budget",
}

// inputOrder is the declaration order every result reports in. It is the
// reading order of the promotion question -- who, where, under whom, into
// what, paid how, funded from where -- and it is stable so a golden verdict
// table stays diffable.
var inputOrder = []string{
	InputSubjectWorkerFacts,
	InputCurrentPlacement,
	InputManagerChain,
	InputTargetPositionCapacity,
	InputTargetPositionVacancy,
	InputPayBandPositionCurrent,
	InputPayBandPositionDesired,
	InputBudgetAvailability,
}

// InputNames returns the declared input names in declaration order.
func InputNames() []string {
	return append([]string(nil), inputOrder...)
}

// OwnerOf returns the domain that owns a declared input's meaning, or "" for
// an undeclared name.
func OwnerOf(name string) string { return inputOwners[name] }

// CapacityExhausted is the exact capacity observation value that activates the
// conditional vacancy requirement. It is a summary token rather than the
// capacity numbers themselves: the completeness condition needs one exact
// value to compare, and the numbers belong in the input's own canonical text.
const CapacityExhausted = "AT_CAPACITY"

// CapacityAvailable is the capacity observation value for a target position
// that still has a head available at the effective date.
const CapacityAvailable = "HAS_AVAILABLE_HEAD"

// Specification is the declared input contract a Promotion input snapshot is
// evaluated against (SNAPSHOT-003).
//
// Seven inputs are REQUIRED. The eighth, the target position's vacancy date,
// is CONDITIONAL on the capacity input reporting [CapacityExhausted]: when the
// position still has a head, when the position frees up is not a fact the
// promotion needs, and demanding it would turn an irrelevant unknown into a
// blocked proposal. When the position is full, it is exactly the fact that
// decides whether the promotion is possible at all.
func Specification() enginesnapshot.InputSpecification {
	return enginesnapshot.InputSpecification{Inputs: []enginesnapshot.CompletenessRequirement{
		{Name: InputSubjectWorkerFacts, Policy: enginesnapshot.InputRequired},
		{Name: InputCurrentPlacement, Policy: enginesnapshot.InputRequired},
		{Name: InputManagerChain, Policy: enginesnapshot.InputRequired},
		{Name: InputTargetPositionCapacity, Policy: enginesnapshot.InputRequired},
		{
			Name:   InputTargetPositionVacancy,
			Policy: enginesnapshot.InputConditional,
			Condition: &enginesnapshot.CompletenessCondition{
				InputName:     InputTargetPositionCapacity,
				ExpectedValue: CapacityExhausted,
			},
		},
		{Name: InputPayBandPositionCurrent, Policy: enginesnapshot.InputRequired},
		{Name: InputPayBandPositionDesired, Policy: enginesnapshot.InputRequired},
		{Name: InputBudgetAvailability, Policy: enginesnapshot.InputRequired},
	}}
}

// BudgetQuery is the one question the budget observation port is asked: what
// does the compensation pool for this scope and period say, as known at this
// instant.
type BudgetQuery struct {
	Tenant values.TenantId
	// Scope is the cost-center or org-unit scope the pool is held against.
	Scope string
	// Period is the budget period (e.g. "FY2026").
	Period string
	// AsOf is the instant the observation is read at.
	AsOf values.Instant
}

// Validate reports whether the query is well formed.
func (q BudgetQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: budget query tenant: %w", ErrRequestInvalid, err)
	}
	if q.Scope == "" || q.Period == "" {
		return fmt.Errorf("%w: budget query needs a scope and a period", ErrRequestInvalid)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: budget query as-of: %w", ErrRequestInvalid, err)
	}
	return nil
}

// BudgetFacts is the read port for the workforce budget observation.
//
// It is defined here rather than in internal/domains/budget because that
// package models what an observation *is* and what a reservation does with
// one; where a pool observation comes from is a read shape, and this is the
// only reader of it in the promotion slice. Like every other port in this
// package, a pool this reader has no record for is a false, not an error:
// non-existence is an answer, and the completeness verdict is what decides
// what it means.
type BudgetFacts interface {
	CompensationBudgetAt(ctx context.Context, q BudgetQuery) (ref budget.BudgetAuthorityRef, exists bool, err error)
}

// Readers is the set of authorized domain read ports a Promotion input
// snapshot is assembled through. Every field is mandatory: an absent reader is
// refused at Build rather than turned into a silently missing input, because
// "nobody asked" and "the record says nothing" are different answers and only
// one of them is a business fact.
type Readers struct {
	// Worker answers people.ExplainWorkerState for the subject.
	Worker people.WorkerFacts
	// Org answers org.ResolveManagerRelationships for the subject.
	Org org.WorkerFacts
	// Position answers position.CalculateCapacity for the target position.
	Position position.PositionFacts
	// Compensation answers rewards.ReadAuthorizedCompensation for the subject.
	Compensation rewards.CompensationFacts
	// Bands resolves the target pay band both band-position reads evaluate
	// against.
	Bands rewards.PayBandCatalog
	// Budget answers the compensation-pool observation.
	Budget BudgetFacts
}

// ErrReaderMissing is returned when Readers omits a port. Matchable with
// errors.Is.
var ErrReaderMissing = errors.New("promotion/snapshot: a required read port is not configured")

// Validate reports whether every read port is configured.
func (r Readers) Validate() error {
	for _, port := range []struct {
		name    string
		present bool
	}{
		{"worker facts", r.Worker != nil},
		{"organization facts", r.Org != nil},
		{"position facts", r.Position != nil},
		{"compensation facts", r.Compensation != nil},
		{"pay band catalog", r.Bands != nil},
		{"budget facts", r.Budget != nil},
	} {
		if !port.present {
			return fmt.Errorf("%w: %s", ErrReaderMissing, port.name)
		}
	}
	return nil
}
