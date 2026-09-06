package schedopt

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/domains/demand"
	"github.com/monstercameron/hcm-next/internal/domains/matching"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const instanceSchemaVersion = 1

var (
	ErrInvalidProblemImport         = errors.New("schedopt: invalid problem import")
	ErrDemandAggregationUnfrozen    = errors.New("schedopt: demand aggregation is not frozen")
	ErrDemandAggregationSuperseded  = errors.New("schedopt: demand aggregation is superseded")
	ErrProblemInstanceBinding       = errors.New("schedopt: problem instance input binding mismatch")
	ErrProblemInstanceAsOf          = errors.New("schedopt: problem instance as-of is outside input validity")
	ErrProblemInstanceVariableBound = errors.New("schedopt: problem instance decision variable bound exceeded")
)

// ProblemImport is the pure input boundary for one schedopt instance. The
// canonical fields are DemandAggregation and CandidatePopulation. Demand and
// Population are compatibility aliases for callers that use the shorter
// names; exactly one field from each pair must be supplied.
type ProblemImport struct {
	Problem             WorkforceOptimizationProblem
	DemandAggregation   demand.DemandAggregation
	Demand              demand.DemandAggregation
	Aggregation         demand.DemandAggregation
	CandidatePopulation matching.CandidatePopulation
	Population          matching.CandidatePopulation
	Candidate           matching.CandidatePopulation
	AsOf                values.Instant

	// These optional intervals make validity explicit when the owning input
	// boundary has one. CandidatePopulation itself always has a point validity
	// at its AsOf; demand.DemandAggregation predates a snapshot field, so its
	// interval is supplied here when one is available.
	DemandValidity    values.EffectiveInterval
	CandidateValidity values.EffectiveInterval

	// This flag lets a snapshot owner reject a demand revision that has been
	// superseded without adding lifecycle state to demand.DemandAggregation.
	DemandSuperseded bool
}

// ImportRequest and ProblemInstanceImport are descriptive aliases for the
// import boundary used by adapters.
type ImportRequest = ProblemImport
type ProblemInstanceImport = ProblemImport

// ProblemInstance is an immutable, digested projection of a bounded problem
// against exactly one frozen demand aggregation and one frozen population.
// It grants no solver or assignment authority. A partial population remains
// visible through CandidateCompleteness and cannot be finalized.
type ProblemInstance struct {
	ProblemDigest             string
	DemandAggregationDigest   string
	CandidatePopulationDigest string
	Tenant                    values.TenantId
	AsOf                      values.Instant
	DecisionVariables         []DecisionVariable
	CandidateCount            int
	DemandWindowCount         int
	CandidateCompleteness     matching.CandidateCompleteness
	FinalOptimizationAllowed  bool
	CanonicalDigest           string

	frozen bool
}

// DecisionVariableList returns a defensive copy of the imported variables.
func (p ProblemInstance) DecisionVariableList() []DecisionVariable {
	return append([]DecisionVariable(nil), p.DecisionVariables...)
}

// CanFinalizeOptimization reports whether the frozen candidate boundary was
// complete. Import remains descriptive for partial populations, but a later
// optimization authority must not finalize them.
func (p ProblemInstance) CanFinalizeOptimization() bool {
	return p.FinalOptimizationAllowed
}

func demandAggregationPresent(a demand.DemandAggregation) bool {
	return a.CanonicalDigest != "" || a.Rule.ID != "" || len(a.Aggregates) != 0
}

func candidatePopulationPresent(p matching.CandidatePopulation) bool {
	return p.CanonicalDigest != "" || p.RequestID != "" || len(p.Candidates) != 0
}

func chooseDemandAggregation(req ProblemImport) (demand.DemandAggregation, error) {
	candidates := []demand.DemandAggregation{req.DemandAggregation, req.Demand, req.Aggregation}
	present := 0
	var selected demand.DemandAggregation
	for _, item := range candidates {
		if !demandAggregationPresent(item) {
			continue
		}
		present++
		selected = item
	}
	if present > 1 {
		return demand.DemandAggregation{}, fmt.Errorf("%w: exactly one frozen demand aggregation is required", ErrProblemInstanceBinding)
	}
	return selected, nil
}

func chooseCandidatePopulation(req ProblemImport) (matching.CandidatePopulation, error) {
	candidates := []matching.CandidatePopulation{req.CandidatePopulation, req.Population, req.Candidate}
	present := 0
	var selected matching.CandidatePopulation
	for _, item := range candidates {
		if !candidatePopulationPresent(item) {
			continue
		}
		present++
		selected = item
	}
	if present > 1 {
		return matching.CandidatePopulation{}, fmt.Errorf("%w: exactly one frozen candidate population is required", ErrProblemInstanceBinding)
	}
	return selected, nil
}

func validateFrozenAggregation(aggregation demand.DemandAggregation, superseded bool) (string, error) {
	if superseded {
		return "", ErrDemandAggregationSuperseded
	}
	if err := aggregation.Validate(); err != nil {
		return "", fmt.Errorf("%w: demand aggregation: %v", ErrInvalidProblemImport, err)
	}
	if aggregation.CanonicalDigest == "" {
		return "", ErrDemandAggregationUnfrozen
	}
	digest, err := aggregation.Digest()
	if err != nil {
		return "", fmt.Errorf("%w: demand aggregation digest: %v", ErrInvalidProblemImport, err)
	}
	if digest != aggregation.CanonicalDigest {
		return "", fmt.Errorf("%w: demand aggregation digest mismatch", ErrDemandAggregationUnfrozen)
	}
	seen := make(map[string]struct{}, len(aggregation.Aggregates))
	for i, item := range aggregation.Aggregates {
		if item.CanonicalDigest == "" || item.CanonicalDigest != canonicalbytes.Digest(item.Canonical()) {
			return "", fmt.Errorf("%w: aggregate %d is not frozen", ErrDemandAggregationUnfrozen, i)
		}
		if strings.TrimSpace(item.Bucket.Key) == "" {
			return "", fmt.Errorf("%w: aggregate %d has no bucket key", ErrInvalidProblemImport, i)
		}
		identity := aggregateIdentity(item)
		if _, ok := seen[identity]; ok {
			return "", fmt.Errorf("%w: duplicate demand window %q", ErrInvalidProblemImport, identity)
		}
		seen[identity] = struct{}{}
		if err := item.Bucket.Window.Validate(); err != nil || item.Bucket.Window.Kind() != values.IntervalKindInstant || item.Bucket.Window.IsOpenEnded() {
			return "", fmt.Errorf("%w: aggregate %d has invalid closed instant window", ErrInvalidProblemImport, i)
		}
		if strings.TrimSpace(item.OrgUnit) == "" || strings.TrimSpace(item.RoleOrSkillRef) == "" || strings.TrimSpace(item.Unit) == "" {
			return "", fmt.Errorf("%w: aggregate %d is missing a scheduling dimension", ErrInvalidProblemImport, i)
		}
	}
	return digest, nil
}

func validateAsOf(asOf values.Instant, population matching.CandidatePopulation, demandValidity, candidateValidity values.EffectiveInterval) error {
	if err := asOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrInvalidProblemImport, err)
	}
	if population.AsOf != asOf {
		return fmt.Errorf("%w: as-of %s is outside candidate population validity at %s", ErrProblemInstanceAsOf, asOf, population.AsOf)
	}
	if demandValidity.Kind() != 0 {
		if err := demandValidity.Validate(); err != nil || demandValidity.Kind() != values.IntervalKindInstant {
			return fmt.Errorf("%w: demand validity must be an instant interval", ErrProblemInstanceAsOf)
		}
		valid, err := demandValidity.ContainsInstant(asOf)
		if err != nil || !valid {
			return fmt.Errorf("%w: as-of %s is outside demand aggregation validity %s", ErrProblemInstanceAsOf, asOf, demandValidity)
		}
	}
	if candidateValidity.Kind() != 0 {
		if err := candidateValidity.Validate(); err != nil || candidateValidity.Kind() != values.IntervalKindInstant {
			return fmt.Errorf("%w: candidate validity must be an instant interval", ErrProblemInstanceAsOf)
		}
		valid, err := candidateValidity.ContainsInstant(asOf)
		if err != nil || !valid {
			return fmt.Errorf("%w: as-of %s is outside candidate population validity %s", ErrProblemInstanceAsOf, asOf, candidateValidity)
		}
	}
	return nil
}

func availabilityCovers(window values.EffectiveInterval, candidate matching.CandidateFacts) bool {
	for _, available := range candidate.Availability {
		if available.Validate() != nil || available.Kind() != values.IntervalKindInstant {
			continue
		}
		start, _ := available.StartInstant()
		windowStart, _ := window.StartInstant()
		if start.After(windowStart) {
			continue
		}
		end, hasEnd := available.EndInstant()
		windowEnd, hasWindowEnd := window.EndInstant()
		if !hasWindowEnd || hasEnd && !end.Before(windowEnd) {
			return true
		}
	}
	return false
}

type importedWindow struct {
	bucketKey string
	identity  string
	key       string
	interval  values.EffectiveInterval
}

func aggregateIdentity(item demand.DemandAggregate) string {
	return item.Bucket.Key + "\x00" + item.OrgUnit + "\x00" + item.RoleOrSkillRef + "\x00" + item.Unit
}

func importedWindows(aggregation demand.DemandAggregation) []importedWindow {
	windows := make([]importedWindow, 0, len(aggregation.Aggregates))
	for _, item := range aggregation.Aggregates {
		windows = append(windows, importedWindow{bucketKey: item.Bucket.Key, identity: aggregateIdentity(item), interval: item.Bucket.Window})
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].identity < windows[j].identity })
	bucketCounts := make(map[string]int, len(windows))
	for _, window := range windows {
		bucketCounts[window.bucketKey]++
	}
	for i := range windows {
		windows[i].key = windows[i].bucketKey
		if bucketCounts[windows[i].bucketKey] > 1 {
			windows[i].key = windows[i].identity
		}
	}
	return windows
}

func importedVariables(population matching.CandidatePopulation, aggregation demand.DemandAggregation) []DecisionVariable {
	candidates := population.CandidateFactsList()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CandidateRef.String() < candidates[j].CandidateRef.String() })
	windows := importedWindows(aggregation)
	variables := make([]DecisionVariable, 0, len(candidates)*len(windows))
	for _, candidate := range candidates {
		for _, window := range windows {
			if availabilityCovers(window.interval, candidate) {
				variables = append(variables, DecisionVariable{CandidateRef: candidate.CandidateRef, DemandWindowID: window.key})
			}
		}
	}
	return variables
}

func validateImportedVariables(p ProblemInstance) error {
	if !p.frozen || p.CanonicalDigest == "" {
		return ErrInvalidProblemImport
	}
	if err := p.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrInvalidProblemImport, err)
	}
	if p.CandidateCount < 0 || p.DemandWindowCount < 0 {
		return fmt.Errorf("%w: instance counts do not match variables", ErrInvalidProblemImport)
	}
	for i, variable := range p.DecisionVariables {
		if err := variable.validate(p.tenant()); err != nil {
			return fmt.Errorf("%w: variable %d: %v", ErrInvalidProblemImport, i, err)
		}
		if i > 0 {
			previous := p.DecisionVariables[i-1]
			if previous.CandidateRef.String() > variable.CandidateRef.String() || previous.CandidateRef.String() == variable.CandidateRef.String() && previous.DemandWindowID >= variable.DemandWindowID {
				return fmt.Errorf("%w: variables are not canonical", ErrInvalidProblemImport)
			}
		}
	}
	return nil
}

func (p ProblemInstance) tenant() values.TenantId {
	return p.Tenant
}

func instanceBody(p ProblemInstance) []byte {
	w := canonicalbytes.New("hcmnext.domains.schedopt.ProblemInstance", instanceSchemaVersion).
		String("problem_digest", p.ProblemDigest).String("demand_aggregation_digest", p.DemandAggregationDigest).
		String("candidate_population_digest", p.CandidatePopulationDigest).String("tenant", string(p.Tenant)).Value("as_of", p.AsOf).
		Int("candidate_count", int64(p.CandidateCount)).Int("demand_window_count", int64(p.DemandWindowCount)).
		String("candidate_completeness", string(p.CandidateCompleteness)).Bool("final_optimization_allowed", p.FinalOptimizationAllowed).
		Count("decision_variables", len(p.DecisionVariables))
	for _, variable := range p.DecisionVariables {
		w.Value("decision_candidate", variable.CandidateRef).String("decision_window", variable.DemandWindowID)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (p ProblemInstance) computedDigest() string {
	raw := instanceBody(p)
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Validate verifies the instance's immutable digest and canonical variable
// order. It does not consult a store or recalculate a schedule.
func (p ProblemInstance) Validate() error {
	if !p.frozen || p.CanonicalDigest == "" {
		return ErrInvalidProblemImport
	}
	if p.ProblemDigest == "" || p.DemandAggregationDigest == "" || p.CandidatePopulationDigest == "" {
		return fmt.Errorf("%w: input digests are required", ErrInvalidProblemImport)
	}
	if err := validateImportedVariables(p); err != nil {
		return err
	}
	if p.computedDigest() != p.CanonicalDigest {
		return fmt.Errorf("%w: instance digest mismatch", ErrInvalidProblemImport)
	}
	return nil
}

// Canonical returns the canonical bytes of a valid imported instance.
func (p ProblemInstance) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return instanceBody(p)
}

// Digest returns the immutable instance digest.
func (p ProblemInstance) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.CanonicalDigest, nil
}

// ImportProblem imports one bounded problem against frozen demand and
// workforce snapshots. It performs no persistence and invokes no solver.
func ImportProblem(req ProblemImport) (ProblemInstance, error) {
	aggregation, err := chooseDemandAggregation(req)
	if err != nil {
		return ProblemInstance{}, err
	}
	population, err := chooseCandidatePopulation(req)
	if err != nil {
		return ProblemInstance{}, err
	}
	if req.DemandSuperseded {
		return ProblemInstance{}, ErrDemandAggregationSuperseded
	}
	aggregationDigest, err := validateFrozenAggregation(aggregation, false)
	if err != nil {
		return ProblemInstance{}, err
	}
	if err := population.Validate(); err != nil {
		return ProblemInstance{}, err
	}
	populationDigest, err := population.Digest()
	if err != nil {
		return ProblemInstance{}, err
	}
	if err := validateAsOf(req.AsOf, population, req.DemandValidity, req.CandidateValidity); err != nil {
		return ProblemInstance{}, err
	}
	problemDigest, err := req.Problem.Digest()
	if err != nil {
		return ProblemInstance{}, fmt.Errorf("%w: problem definition: %v", ErrInvalidProblemImport, err)
	}
	problemPopulationDigest, err := req.Problem.Population.Digest()
	if err != nil || problemPopulationDigest != populationDigest {
		return ProblemInstance{}, fmt.Errorf("%w: problem definition is not bound to candidate population digest %s", ErrProblemInstanceBinding, populationDigest)
	}
	if req.Problem.Population.RequesterScope.Tenant != population.RequesterScope.Tenant {
		return ProblemInstance{}, fmt.Errorf("%w: problem and candidate population tenants differ", ErrProblemInstanceBinding)
	}
	variables := importedVariables(population, aggregation)
	if len(variables) > req.Problem.Bounds.MaxDecisionVariables {
		return ProblemInstance{}, fmt.Errorf("%w: %w: %d variables exceed MaxDecisionVariables=%d", ErrProblemInstanceVariableBound, ErrInvalidProblem, len(variables), req.Problem.Bounds.MaxDecisionVariables)
	}
	instance := ProblemInstance{
		ProblemDigest: problemDigest, DemandAggregationDigest: aggregationDigest, CandidatePopulationDigest: populationDigest,
		Tenant: population.RequesterScope.Tenant, AsOf: req.AsOf, DecisionVariables: variables,
		CandidateCount: len(population.Candidates), DemandWindowCount: len(aggregation.Aggregates),
		CandidateCompleteness: population.Completeness, FinalOptimizationAllowed: population.Completeness == matching.CandidateCompletenessComplete, frozen: true,
	}
	instance.CanonicalDigest = instance.computedDigest()
	return instance, nil
}

// ImportProblemInstance is the explicit function name for callers that want
// the result type reflected in the call site.
func ImportProblemInstance(req ProblemImport) (ProblemInstance, error) {
	return ImportProblem(req)
}

// NewProblemInstance is a constructor alias for ImportProblem.
func NewProblemInstance(req ProblemImport) (ProblemInstance, error) {
	return ImportProblem(req)
}

// Import is the concise adapter-facing alias for ImportProblem.
func Import(req ProblemImport) (ProblemInstance, error) { return ImportProblem(req) }

// ProblemInstanceExplanation contains only the two input digests and counts
// needed to explain an import. It deliberately omits workers, windows, as-of,
// completeness and any protected facts.
type ProblemInstanceExplanation struct {
	ProblemDigest             string
	DemandAggregationDigest   string
	CandidatePopulationDigest string
	CandidateCount            int
	DemandWindowCount         int
	DecisionVariableCount     int
}

// Explain returns the bounded import explanation.
func (p ProblemInstance) Explain() (ProblemInstanceExplanation, error) {
	if err := p.Validate(); err != nil {
		return ProblemInstanceExplanation{}, err
	}
	return ProblemInstanceExplanation{
		ProblemDigest: p.ProblemDigest, DemandAggregationDigest: p.DemandAggregationDigest,
		CandidatePopulationDigest: p.CandidatePopulationDigest, CandidateCount: p.CandidateCount,
		DemandWindowCount: p.DemandWindowCount, DecisionVariableCount: len(p.DecisionVariables),
	}, nil
}

// ExplainProblemInstance is the package-level explanation form.
func ExplainProblemInstance(p ProblemInstance) (ProblemInstanceExplanation, error) {
	return p.Explain()
}
