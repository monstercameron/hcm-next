package promotion

import (
	"context"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// PlacementChange is one projected before/after pair.
type PlacementChange struct {
	Field   string
	Before  string
	After   string
	Changed bool
}

// ProjectedWorkerState is the worker as the promotion would leave them. It is
// a projection, not a plan: nothing here names a write, an event or a stream,
// because P1A produces no write plan at all.
type ProjectedWorkerState struct {
	Worker        values.EntityRef
	EffectiveDate values.LocalDate
	Changes       []PlacementChange
}

// Canonical returns the canonical byte encoding.
func (p ProjectedWorkerState) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.promotion.ProjectedWorkerState", promotionSchemaVer).
		Value("worker", p.Worker).
		Value("effective_date", p.EffectiveDate).
		Count("changes", len(p.Changes))
	for _, c := range p.Changes {
		w.String("change.field", c.Field).
			String("change.before", c.Before).
			String("change.after", c.After).
			Bool("change.changed", c.Changed)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// CompensationState says whether the compensation section was computed.
type CompensationState uint8

// Compensation states.
const (
	// CompensationNotEvaluated means blocking findings made the compensation
	// arithmetic meaningless, so it was not attempted.
	CompensationNotEvaluated CompensationState = iota
	// CompensationEvaluated means the compensation simulation ran.
	CompensationEvaluated
)

var compensationStateWire = map[CompensationState]string{
	CompensationNotEvaluated: "NOT_EVALUATED",
	CompensationEvaluated:    "EVALUATED",
}

// String returns the stable wire token.
func (s CompensationState) String() string {
	if v, ok := compensationStateWire[s]; ok {
		return v
	}
	return "NOT_EVALUATED"
}

// SimulationResult is the deterministic, zero-effect projection of a
// promotion: what the worker's placement would become, what the compensation
// change would cost, and the receipt proving nothing happened.
type SimulationResult struct {
	IntentType    string
	IntentVersion string

	Preflight PreflightResult
	Projected ProjectedWorkerState

	CompensationState  CompensationState
	CompensationReason string
	Compensation       rewards.SimulateCompensationResult

	// Executable reports whether the promotion could proceed to a proposal as
	// simulated. It is false whenever preflight is not READY: a simulation of a
	// blocked promotion is useful to look at and must never be mistaken for a
	// green light.
	Executable bool

	InputsDigest string
	ResultDigest string
	Effects      evidence.EffectCounters
	Receipt      evidence.ZeroEffectReceipt
}

// canonicalBody encodes everything the result digest covers.
func (r SimulationResult) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.promotion.SimulationResult", promotionSchemaVer).
		String("intent_type", r.IntentType).
		String("intent_version", r.IntentVersion).
		Value("preflight", r.Preflight).
		Value("projected", r.Projected).
		String("compensation_state", r.CompensationState.String()).
		String("compensation_reason", r.CompensationReason)
	if r.CompensationState == CompensationEvaluated {
		w.Value("compensation", r.Compensation)
	}
	return w.
		Bool("executable", r.Executable).
		Value("effects", r.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding of the whole result.
func (r SimulationResult) Canonical() []byte {
	body, err := r.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.SimulationEnvelope", promotionSchemaVer).
		Field("body", body).
		String("inputs_digest", r.InputsDigest).
		Value("receipt", r.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// project builds the before/after placement view.
func project(req PreflightRequest, baseline WorkerBaseline) ProjectedWorkerState {
	targetPayZone := req.Target.PayZone
	if targetPayZone == "" {
		targetPayZone = baseline.PayZone
	}
	pairs := []struct {
		field  string
		before string
		after  string
	}{
		{people.FieldJobCode.String(), baseline.JobCode, req.Target.JobCode},
		{people.FieldGrade.String(), baseline.Grade, req.Target.Grade},
		{people.FieldOrgUnit.String(), baseline.OrgUnit, orDefault(req.Target.OrgUnit, baseline.OrgUnit)},
		{people.FieldPositionID.String(), "", req.Target.PositionID},
		{people.FieldPayZone.String(), baseline.PayZone, targetPayZone},
	}
	changes := make([]PlacementChange, 0, len(pairs))
	for _, p := range pairs {
		changes = append(changes, PlacementChange{
			Field:   p.field,
			Before:  p.before,
			After:   p.after,
			Changed: p.before != p.after,
		})
	}
	return ProjectedWorkerState{
		Worker:        req.Subject,
		EffectiveDate: req.EffectiveDate,
		Changes:       changes,
	}
}

// orDefault returns v when it is set, otherwise fallback. A target that does
// not name an organization leaves the worker where they are; it does not move
// them to an empty one.
func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// SimulatePromotion runs the preflight and, when the inputs are coherent
// enough to compute on, the compensation simulation, and returns the projected
// worker state, the compensation deltas and a zero-effect receipt.
//
// A DENIED preflight returns ErrPreflightDenied: a caller who may not see the
// subject may not see a simulation of it either, and returning an empty
// simulation would still confirm the subject exists.
func SimulatePromotion(ctx context.Context, catalog rewards.PayBandCatalog, req PreflightRequest) (SimulationResult, error) {
	preflight, err := PreflightPromotion(ctx, catalog, req)
	if err != nil {
		return SimulationResult{}, err
	}
	if preflight.Status == StatusDenied {
		return SimulationResult{}, ErrPreflightDenied
	}

	projected := project(req, preflight.Input.Baseline)

	compensationState := CompensationNotEvaluated
	compensationReason := ""
	var compensation rewards.SimulateCompensationResult

	compInput := rewards.SimulateCompensationInput{
		Tenant:        req.Tenant,
		Subject:       req.Subject,
		Current:       req.Current,
		Proposed:      req.Proposed,
		Annualization: req.Annualization,
		EffectiveDate: req.EffectiveDate,
	}
	if preflight.Input.BandQuery != nil {
		q := *preflight.Input.BandQuery
		compInput.Band = &q
	}

	if err := compInput.Validate(); err != nil {
		// The preflight already reported these as typed findings; recording the
		// reason here keeps the simulation honest about why the numbers are
		// absent rather than presenting zeros.
		compensationReason = err.Error()
	} else {
		compensation, err = rewards.SimulateCompensation(ctx, catalog, compInput)
		if err != nil {
			return SimulationResult{}, err
		}
		compensationState = CompensationEvaluated
	}

	result := SimulationResult{
		IntentType:         IntentType,
		IntentVersion:      IntentVersion,
		Preflight:          preflight,
		Projected:          projected,
		CompensationState:  compensationState,
		CompensationReason: compensationReason,
		Compensation:       compensation,
		Executable:         preflight.Status == StatusReady,
		Effects:            evidence.ZeroEffects(),
	}

	inputsDigest, err := canonicalbytes.New("hcmnext.domains.promotion.SimulateRequest", promotionSchemaVer).
		String("intent_type", IntentType).
		String("intent_version", IntentVersion).
		String("preflight_inputs_digest", preflight.InputsDigest).
		Digest()
	if err != nil {
		return SimulationResult{}, err
	}
	result.InputsDigest = inputsDigest

	body, err := result.canonicalBody()
	if err != nil {
		return SimulationResult{}, err
	}
	result.ResultDigest = canonicalbytes.Digest(body)

	controls := append([]evidence.ControlVersion(nil), preflight.Receipt.Controls...)
	if compensationState == CompensationEvaluated {
		controls = append(controls, evidence.ControlVersion{
			Name:    "compensation_rule_pack",
			Version: compensation.RulePackVersion,
		})
	}
	receipt, err := evidence.NewZeroEffectReceipt(
		IntentType, IntentVersion,
		evidence.ModeSimulate, evidence.RequestStateSimulated,
		controls, result.InputsDigest, result.ResultDigest, result.Effects,
	)
	if err != nil {
		return SimulationResult{}, err
	}
	result.Receipt = receipt
	return result, nil
}
