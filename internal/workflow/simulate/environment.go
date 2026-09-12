package simulate

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Capability identities the promotion reference workflow binds.
const (
	CapExplainWorkerState = "hcmnext.people.explain_worker_state"
	CapSimulateComp       = "hcmnext.rewards.simulate_compensation"
	CapEvaluatePayBand    = "hcmnext.rewards.evaluate_pay_band_position"
	CapDetectDrift        = "hcmnext.operations.detect_drift"
)

// Governance constants the environment presents to the People domain's
// already-evaluated authorization decision. This package never computes an
// authorization; it presents one an AuthZ evaluator made.
const (
	promotionPolicyVersion = "authz.people.promotion_simulation/2026.1"
	promotionPurpose       = "promotion_simulation"
)

// Environment is the wired, zero-effect execution environment the promotion
// reference workflow is simulated in.
//
// Everything it holds is a pinned, in-memory projection: the shared P1A domain
// corpus for worker facts and pay bands, a declared current compensation
// snapshot, a declared target placement, and an observed budget authority.
// Nothing reads a clock, a database or a network, which is what makes a
// simulation over it reproducible.
//
// The handlers it binds are the real domain calculations, not stubs. Each one
// already carries its own evidence.EffectCounters and refuses to produce a
// receipt over a non-zero count, so the interpreter's zero-effect claim rests
// on the domains' own contracts rather than on a promise made here.
type Environment struct {
	Tenant values.TenantId
	Worker values.EntityRef

	Facts   people.WorkerFacts
	Catalog rewards.PayBandCatalog

	// KnownAt is the knowledge cut-off every governed read is taken at.
	KnownAt values.KnownAt
	// EffectiveDate is the business date the promotion would take effect.
	EffectiveDate values.LocalDate
	// EvaluationDate is "today" as the caller declares it. The domains never
	// read a clock, so a preflight replays to the same verdict.
	EvaluationDate values.LocalDate

	Target promotion.TargetPlacement

	// Current is the worker's pinned compensation baseline.
	Current rewards.CompensationSnapshot
	// PayBasis and BonusTarget describe the proposed side, whose base amount
	// comes from the workflow's own input.
	ProposedPayBasis    rewards.PayBasis
	ProposedBonusTarget values.Presence[values.Percentage]

	BusinessReason string
	Budget         *promotion.BudgetAuthorityRef
	Policy         promotion.Policy
	Annualization  rewards.AnnualizationRule

	// CostCenter is the cost center the raise lands in, reported by the
	// compensation simulation so a finance approval can be scoped to it.
	CostCenter string
	// Watermark is the source position the observation port cites.
	Watermark string
}

// NewPromotionEnvironment assembles the reference environment: Jane Doe of the
// shared P1A corpus, promoted from ENG-SWE3/P3 into the ENG-MGR1/M1 management
// band in US-WEST, effective 2026-10-01.
//
// The numbers are the corpus's own. Nothing here is invented at run time.
func NewPromotionEnvironment() (*Environment, error) {
	facts, err := fixtures.NewMemoryWorkerFacts()
	if err != nil {
		return nil, fmt.Errorf("simulate: worker facts fixture: %w", err)
	}
	catalog, err := fixtures.NewMemoryBandCatalog()
	if err != nil {
		return nil, fmt.Errorf("simulate: pay band fixture: %w", err)
	}
	worker, err := fixtures.WorkerRef("jane-doe")
	if err != nil {
		return nil, fmt.Errorf("simulate: worker reference: %w", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(DefaultClockStart))
	if err != nil {
		return nil, fmt.Errorf("simulate: known-at: %w", err)
	}
	effective, err := values.ParseLocalDate("2026-10-01")
	if err != nil {
		return nil, fmt.Errorf("simulate: effective date: %w", err)
	}
	evaluation, err := values.ParseLocalDate("2026-09-03")
	if err != nil {
		return nil, fmt.Errorf("simulate: evaluation date: %w", err)
	}
	currentBase, err := fixtures.Money(fixtures.JanePromotionBase, "USD")
	if err != nil {
		return nil, fmt.Errorf("simulate: current base: %w", err)
	}
	bonus, err := fixtures.Percent(fixtures.JanePromotionBonus)
	if err != nil {
		return nil, fmt.Errorf("simulate: bonus target: %w", err)
	}
	watermark, err := values.NewSequenceRevision("people.worker.11111111", 42)
	if err != nil {
		return nil, fmt.Errorf("simulate: watermark: %w", err)
	}
	available, err := fixtures.Money("240000.00", "USD")
	if err != nil {
		return nil, fmt.Errorf("simulate: budget pool: %w", err)
	}

	return &Environment{
		Tenant:         fixtures.Tenant,
		Worker:         worker,
		Facts:          facts,
		Catalog:        catalog,
		KnownAt:        knownAt,
		EffectiveDate:  effective,
		EvaluationDate: evaluation,
		// PositionID is deliberately absent: PROMOUX-004 checks a non-empty
		// value against the real Position domain, and this reference
		// workflow environment wires no PositionReader (it exercises
		// workflow simulation mechanics, not position selection).
		// JobCode/Grade/OrgUnit alone already satisfy checkPlacement's
		// placement requirement.
		Target: promotion.TargetPlacement{
			JobCode: "ENG-MGR1",
			Grade:   "M1",
			OrgUnit: "eng-platform",
			PayZone: "US-WEST",
		},
		Current: rewards.CompensationSnapshot{
			Base:               values.Value(currentBase),
			PayBasis:           rewards.PayBasisAnnualSalary,
			BonusTargetPercent: values.Value(bonus),
			EffectiveDate:      effective,
			Watermark:          watermark,
			Complete:           true,
		},
		ProposedPayBasis:    rewards.PayBasisAnnualSalary,
		ProposedBonusTarget: values.Value(bonus),
		BusinessReason:      "Promotion into first-line engineering management for Team Phoenix",
		Budget: &promotion.BudgetAuthorityRef{
			BudgetType:      promotion.BudgetTypeCompensationPool,
			OwnerSystem:     "finance.incumbent.erp",
			PolicyRef:       "finance.budget_authority/2026.1",
			Scope:           "cost-center:eng-platform",
			Period:          "FY2026",
			Currency:        "USD",
			Unit:            "AMOUNT",
			BaselineVersion: "finance.budget.baseline/2026.09",
			AvailableAmount: values.Value(available),
			ObservationID:   "obs_budget_eng_platform_2026_09",
		},
		Policy:        promotion.DefaultPolicy(),
		Annualization: rewards.DefaultAnnualization(),
		CostCenter:    "cost-center:eng-platform",
		Watermark:     "people.employment.stream_head@42",
	}, nil
}

// asOf is the bitemporal coordinate every governed read in this environment is
// taken at.
func (e *Environment) asOf(effective values.LocalDate) people.AsOf {
	return people.AsOf{EffectiveOn: effective, KnownAt: e.KnownAt}
}

// workerFields is the projection the promotion reads: exactly what preflight
// needs, plus the manager relationship the workflow's snapshot node declares.
// Nothing compartmentalised is loaded merely because the subject is being
// promoted.
func workerFields() []people.FieldID {
	return append(promotion.RequiredWorkerFields(), people.FieldManagerRelation)
}

// explain runs the governed worker read. It is shared by the snapshot
// capability and the proposal transform, so both see one baseline.
func (e *Environment) explain(ctx context.Context, effective values.LocalDate) (people.Explanation, error) {
	fields := workerFields()
	return people.ExplainWorkerState(ctx, e.Facts, people.ExplainWorkerStateRequest{
		Tenant:        e.Tenant,
		Worker:        e.Worker,
		AsOf:          e.asOf(effective),
		Fields:        fields,
		Authorization: fixtures.AllowAll(promotionPolicyVersion, promotionPurpose, fields),
	})
}

// proposedSnapshot builds the proposed side of a compensation change from the
// workflow's declared input amount and the environment's pinned basis.
func (e *Environment) proposedSnapshot(base values.Money, effective values.LocalDate) rewards.CompensationSnapshot {
	return rewards.CompensationSnapshot{
		Base:               values.Value(base),
		PayBasis:           e.ProposedPayBasis,
		BonusTargetPercent: e.ProposedBonusTarget,
		EffectiveDate:      effective,
		Watermark:          e.Current.Watermark,
		Complete:           true,
	}
}

func (e *Environment) bandQuery(base values.Money, effective values.LocalDate) rewards.BandQuery {
	return rewards.BandQuery{
		Tenant:   e.Tenant,
		JobCode:  e.Target.JobCode,
		Grade:    e.Target.Grade,
		PayZone:  e.Target.PayZone,
		Currency: base.Currency(),
		AsOf:     effective,
	}
}

// Registry publishes the four capability versions the promotion reference
// binds, each bound to the real zero-effect domain calculation behind it.
//
// Every definition is READ_ONLY, which is what lets the governed gateway
// invoke it at all: capability.Gateway refuses every write effect class in
// P1A, so a handler that wanted to mutate could not be reached through this
// registry even if one were written.
func (e *Environment) Registry() (*capability.Registry, error) {
	r := capability.NewRegistry()
	entries := []struct {
		id      string
		domain  string
		handler capability.Handler
	}{
		{CapExplainWorkerState, "people", e.handleExplainWorkerState},
		{CapSimulateComp, "rewards", e.handleSimulateCompensation},
		{CapEvaluatePayBand, "rewards", e.handleEvaluatePayBand},
		{CapDetectDrift, "operations", e.handleDetectDrift},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("simulate: publish %s: %w", entry.id, err)
		}
	}
	return r, nil
}

// readOnlyDefinition mirrors the BOOTSTRAP registry's manifest shape for one
// zero-effect P1A capability. The manifest is the source of effect truth, so
// declaring READ_ONLY here is what the gateway and the compiler both enforce
// against.
func readOnlyDefinition(id, domain string) capability.Definition {
	schema := func(slot string) capability.SchemaRef {
		return capability.SchemaRef{
			SchemaID:         id + "." + slot + "/v1",
			Version:          1,
			ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
		}
	}
	return capability.Definition{
		ID:                   id,
		Version:              1,
		OwnerDomain:          domain,
		RequestSchema:        schema("request"),
		ResponseSchema:       schema("response"),
		ErrorSchema:          schema("error"),
		EffectClass:          capability.EffectReadOnly,
		ReadData:             capability.DataDomainFieldSet{DataDomains: []string{domain}},
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        "scope:" + domain + ".read",
		LegalBasisRef:        "legal.p1a.observation-only.v1",
		EntitlementRef:       "entitlement.pilot.p1a.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:" + id + "/v1",
	}
}

// request unwraps the interpreter's typed payload.
func request(payload any) (CapabilityRequest, error) {
	req, ok := payload.(CapabilityRequest)
	if !ok {
		return CapabilityRequest{}, fmt.Errorf("simulate: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

// handleExplainWorkerState answers the snapshot node through
// people.ExplainWorkerState.
//
// A withheld or partial disclosure is not silently downgraded to an empty
// snapshot: it produces the REJECTED outcome, which the workflow routes to its
// own rejected terminal.
func (e *Environment) handleExplainWorkerState(ctx context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	effective, err := localDateInput(req.Inputs, "effective_date")
	if err != nil {
		return nil, err
	}
	explanation, err := e.explain(ctx, effective)
	if err != nil {
		return nil, err
	}
	if explanation.Disclosure != people.DisclosureFull || explanation.Presence != people.SubjectPresent {
		return CapabilityResponse{
			Outcome: workflow.OutcomeRejected,
			Outputs: Bag{},
			Detail: "worker state disclosure is " + explanation.Disclosure.String() +
				", presence " + explanation.Presence.String(),
		}, nil
	}
	value := func(field people.FieldID) string {
		v, _ := explanation.Value(field)
		return v
	}
	status := value(people.FieldEmploymentStatus)
	return CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: Bag{
			"worker_id":      NewBranded("WorkerID", e.Worker.Id),
			"current_job_id": NewBranded("JobID", value(people.FieldJobCode)),
			"current_level":  NewString(value(people.FieldGrade)),
			// The corpus discloses the manager relationship reference rather
			// than the manager's own worker id; it is reported as read, not
			// resolved into an identity the read never returned.
			"manager_id":        NewBranded("WorkerID", value(people.FieldManagerRelation)),
			"employment_active": NewBool(status == "active"),
		},
		Detail: "explained " + itoa(uint64(len(explanation.Fields))) + " authorized worker fields as of " +
			effective.String() + " under " + explanation.RulePackVersion,
	}, nil
}

// handleSimulateCompensation answers the compensation node through
// rewards.SimulateCompensation.
func (e *Environment) handleSimulateCompensation(ctx context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	effective, err := localDateInput(req.Inputs, "effective_date")
	if err != nil {
		return nil, err
	}
	base, err := moneyInput(req.Inputs, "proposed_base_pay")
	if err != nil {
		return nil, err
	}
	result, err := rewards.SimulateCompensation(ctx, e.Catalog, rewards.SimulateCompensationInput{
		Tenant:        e.Tenant,
		Subject:       e.Worker,
		Current:       e.Current,
		Proposed:      e.proposedSnapshot(base, effective),
		Annualization: e.Annualization,
		EffectiveDate: effective,
	})
	if err != nil {
		return nil, err
	}
	return CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: Bag{
			"annualized_delta": NewMoney(result.Delta.AnnualizedBase),
			// The reference workflow names this field raise_ratio; the value
			// is the annualized base increase in percent, which is the unit
			// the raise-threshold table compares against.
			"raise_ratio": NewDecimal(result.Delta.IncreasePercent),
			"cost_center": NewString(e.CostCenter),
		},
		Detail: "annualized base moves by " + result.Delta.AnnualizedBase.String() +
			" (" + result.Delta.IncreasePercent.String() + "%) under " + result.AnnualizationVersion,
	}, nil
}

// handleEvaluatePayBand answers the band node through
// rewards.EvaluatePayBandPosition.
//
// A band the catalog cannot resolve is AMBIGUOUS, never "within band": an
// unanswered band question is not a passed one.
func (e *Environment) handleEvaluatePayBand(ctx context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	base, err := moneyInput(req.Inputs, "proposed_base_pay")
	if err != nil {
		return nil, err
	}
	evaluation, err := rewards.EvaluatePayBandPosition(ctx, e.Catalog, e.bandQuery(base, e.EffectiveDate), base)
	if err != nil {
		return CapabilityResponse{
			Outcome: workflow.OutcomeAmbiguous,
			Outputs: Bag{},
			Detail:  "pay band could not be resolved: " + err.Error(),
		}, nil
	}
	placement := evaluation.Position.Placement.String()
	return CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: Bag{
			"band_position": NewString(placement),
			"within_band":   NewBool(evaluation.Outcome == rewards.BandOutcomeWithin),
		},
		Detail: "band " + evaluation.Position.BandID + "@" + evaluation.Position.BandVersion +
			" places " + base.String() + " " + placement +
			" (compa-ratio " + evaluation.Position.CompaRatio.String() + ", outcome " + evaluation.Outcome.String() + ")",
	}, nil
}

// handleDetectDrift exists so the drift capability is publishable and
// invocable, but the promotion reference reaches its projection through the
// OBSERVE node's injected read port instead. An observation is a read of an
// authoritative source, and routing it through a port rather than a capability
// handler keeps "what did the source say" an injected fact.
func (e *Environment) handleDetectDrift(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return CapabilityResponse{
		Outcome: workflow.OutcomeUnknown,
		Outputs: Bag{},
		Detail:  "drift detection for node " + req.NodeID + " is served by the injected read port",
	}, nil
}

func moneyInput(b Bag, path string) (values.Money, error) {
	v, err := b.Get(path)
	if err != nil {
		return values.Money{}, err
	}
	return v.Money()
}

func localDateInput(b Bag, path string) (values.LocalDate, error) {
	v, err := b.Get(path)
	if err != nil {
		return values.LocalDate{}, err
	}
	return v.LocalDate()
}

// budgetAuthority classifies the environment's observed budget authority in
// the rules engine's vocabulary. An unread pool is UNKNOWN, which the
// threshold table blocks on rather than treating as funded.
func (e *Environment) budgetAuthority() rules.BudgetAuthority {
	if e.Budget == nil {
		return rules.BudgetAuthorityUnknown
	}
	if _, ok := e.Budget.AvailableAmount.Get(); !ok {
		return rules.BudgetAuthorityUnknown
	}
	return rules.BudgetAuthoritySufficient
}
