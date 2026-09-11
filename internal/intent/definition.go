package intent

import (
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// PopulationScope is the declared population a CHANGE_REQUEST operates on. It
// is what used to be the BATCH_OPERATION family; it is an attribute, and it is
// legal on CHANGE_REQUEST definitions only.
type PopulationScope struct {
	// ScopeRef names the population selector definition.
	ScopeRef string

	// ItemSubjectKind is the subject kind of one resolved population item.
	ItemSubjectKind string

	// FrozenSnapshotRequired demands an immutable audience snapshot before any
	// per-item child intent is created. Without it, per-item identities and
	// retry safety cannot be preserved.
	FrozenSnapshotRequired bool

	// PerItemChildIntent declares one child intent per item, so a per-recipient
	// failure can never be hidden by an aggregate result.
	PerItemChildIntent bool
}

// Validate rejects a population scope that cannot preserve per-item identity.
func (p PopulationScope) Validate() error {
	if p.ScopeRef == "" {
		return newError("Validate", "population_scope.scope_ref", ErrInvalidDefinition,
			"population scope names no selector")
	}
	if p.ItemSubjectKind == "" {
		return newError("Validate", "population_scope.item_subject_kind", ErrInvalidDefinition,
			"population scope names no per-item subject kind")
	}
	if !p.FrozenSnapshotRequired {
		return newError("Validate", "population_scope.frozen_snapshot_required", ErrInvalidDefinition,
			"a population scope must freeze its audience before creating child intents")
	}
	if !p.PerItemChildIntent {
		return newError("Validate", "population_scope.per_item_child_intent", ErrInvalidDefinition,
			"a population scope must preserve per-item identities and failures")
	}
	return nil
}

// InputKind classifies a required input so that a missing one produces a typed
// NEEDS_DATA finding rather than an untyped nil.
type InputKind uint8

// InputKind values.
const (
	InputKindUnspecified InputKind = iota
	InputKindSubjectRef
	InputKindEffectiveTime
	InputKindScalar
	InputKindMoney
	InputKindReference
	InputKindDocument
)

var inputKindNames = map[InputKind]string{
	InputKindUnspecified:   "UNSPECIFIED",
	InputKindSubjectRef:    "SUBJECT_REF",
	InputKindEffectiveTime: "EFFECTIVE_TIME",
	InputKindScalar:        "SCALAR",
	InputKindMoney:         "MONEY",
	InputKindReference:     "REFERENCE",
	InputKindDocument:      "DOCUMENT",
}

func (k InputKind) String() string { return enumName(inputKindNames, k, "InputKind") }

// Valid reports whether k is a declared input kind other than UNSPECIFIED.
func (k InputKind) Valid() bool { _, ok := inputKindNames[k]; return ok && k != InputKindUnspecified }

// RequiredInput is one field of the definition's input schema that preflight
// must find populated. The path is a schema path, never a prose name.
type RequiredInput struct {
	Path     string
	Kind     InputKind
	Required bool
}

// Definition is one versioned semantic intent definition. Nothing here is
// inferred from the definition's name: family, side effects, authorization,
// evidence and lifecycle are all declared.
//
// Fields marked "DRAFT_CONTRACT" in the comments are the ones required at that
// maturity; the rest become required at CONTRACTED.
type Definition struct {
	// Ref is the identity: intent_type_id plus version. DRAFT_CONTRACT.
	Ref Ref

	// DisplayName is presentation only. Renaming it never changes identity.
	// DRAFT_CONTRACT.
	DisplayName string

	// Description states what the definition does. DRAFT_CONTRACT.
	Description string

	// OwnerDomain is the domain that owns the semantics. DRAFT_CONTRACT.
	OwnerDomain string

	// OwnerPlane is the platform plane the definition belongs to.
	OwnerPlane string

	// Family is one of the three kernel families. DRAFT_CONTRACT.
	Family Family

	// Maturity is explicit; nothing below DRAFT_CONTRACT is in the catalog.
	// DRAFT_CONTRACT.
	Maturity Maturity

	// SideEffect is the permanent declared side-effect profile. DRAFT_CONTRACT.
	SideEffect SideEffect

	// EffectClass is the effect ceiling for the scheduled release.
	EffectClass EffectClass

	// Release schedules the definition.
	Release Release

	// InputSchema and ResultSchema pin the typed contracts. DRAFT_CONTRACT.
	InputSchema  SchemaRef
	ResultSchema SchemaRef

	// PhaseDepth records how deep the phase plan takes this definition.
	// DRAFT_CONTRACT.
	PhaseDepth string

	AllowedInitiators []Initiator
	AllowedModes      []Mode

	// PopulationScope is present only on a CHANGE_REQUEST that operates on a
	// resolved population.
	PopulationScope *PopulationScope

	RiskClass               string
	DataClassificationFloor string
	RetentionClass          string
	SLOClass                string

	SubjectKinds           []string
	RequiredCapabilities   []string
	GovernanceRequirements []string
	Preconditions          []string
	Invariants             []string

	// RequiredInputs is the exact input contract preflight checks.
	RequiredInputs []RequiredInput

	// AllowedTransitions narrows the kernel lifecycle profiles. It may never
	// widen them; the registry checks that.
	AllowedTransitions map[lifecycle.Dimension][]lifecycle.TransitionRule

	// ApplicableNegativeStates are the negative states this definition can
	// actually encounter. Every one must be decided by NegativeStatePolicyRef.
	ApplicableNegativeStates []NegativeState

	// NegativeStatePolicyRef references a shared policy by "id/vN".
	NegativeStatePolicyRef string

	// ApprovalRequired feeds the kernel legality rule for APPROVED.
	ApprovalRequired bool

	// ClosurePolicyPermitsOpenRepair feeds the kernel legality rule for CLOSED.
	ClosurePolicyPermitsOpenRepair bool

	// CorrectionRule is required when SideEffect is irreversible.
	CorrectionRule string

	IdempotencyScope      string
	ConflictFootprintRule string
	ProposalBindingRule   string
	RevalidationRule      string
	CancellationRule      string
	CompensationRule      string
	EvidenceRule          string
	OutcomeContract       string
	AvailabilityPolicy    string
}

// clone returns a Definition that shares no mutable memory with d: every
// slice, the AllowedTransitions map and its per-dimension slices, and the
// PopulationScope pointer are copied.
//
// The registry hands definitions out by value, which copies the struct but not
// what its slice headers point at. Before this existed, a caller that wrote to
// a returned definition's SubjectKinds (or any other slice field) wrote
// straight into the registry's own storage, and two callers doing it at once
// were a genuine data race — which is exactly what `go test -race` reported on
// Linux CI for TestTodo_MODEL_010_Race, whose own comment already stated the
// intended contract: "Mutating the returned copies must not reach the
// registry." Nothing inside the copied element types holds further references
// (Initiator, Mode, NegativeState and RequiredInput are scalars or
// all-scalar structs; PopulationScope is all-scalar; lifecycle.TransitionRule
// is copied by value), so one level of copying is the whole job.
func (d Definition) clone() Definition {
	out := d
	out.AllowedInitiators = slices.Clone(d.AllowedInitiators)
	out.AllowedModes = slices.Clone(d.AllowedModes)
	out.SubjectKinds = slices.Clone(d.SubjectKinds)
	out.RequiredCapabilities = slices.Clone(d.RequiredCapabilities)
	out.GovernanceRequirements = slices.Clone(d.GovernanceRequirements)
	out.Preconditions = slices.Clone(d.Preconditions)
	out.Invariants = slices.Clone(d.Invariants)
	out.RequiredInputs = slices.Clone(d.RequiredInputs)
	out.ApplicableNegativeStates = slices.Clone(d.ApplicableNegativeStates)
	if d.PopulationScope != nil {
		scope := *d.PopulationScope
		out.PopulationScope = &scope
	}
	if d.AllowedTransitions != nil {
		transitions := make(map[lifecycle.Dimension][]lifecycle.TransitionRule, len(d.AllowedTransitions))
		for dimension, rules := range d.AllowedTransitions {
			transitions[dimension] = slices.Clone(rules)
		}
		out.AllowedTransitions = transitions
	}
	return out
}

// AllowsMode reports whether the definition permits an execution mode.
func (d Definition) AllowsMode(m Mode) bool { return slices.Contains(d.AllowedModes, m) }

// AllowsInitiator reports whether the definition permits an initiator kind.
func (d Definition) AllowsInitiator(i Initiator) bool {
	return slices.Contains(d.AllowedInitiators, i)
}

// AllowsSubjectKind reports whether the definition accepts a subject kind.
func (d Definition) AllowsSubjectKind(kind string) bool {
	return slices.Contains(d.SubjectKinds, kind)
}

// ZeroEffect reports whether the definition may cause no domain mutation and no
// external effect in its scheduled release.
func (d Definition) ZeroEffect() bool { return d.EffectClass == EffectClassZero }

// Validate checks everything a definition can assert about itself. Cross-
// definition facts (duplicate versions, unknown schemas and capabilities,
// unresolved negative-state policies, lifecycle narrowing) are the registry's
// job; see [NewRegistry].
func (d Definition) Validate() error {
	if err := d.Ref.Validate(); err != nil {
		return err
	}
	for _, req := range []struct {
		field, value string
	}{
		{"display_name", d.DisplayName},
		{"description", d.Description},
		{"owner_domain", d.OwnerDomain},
		{"phase_depth", d.PhaseDepth},
	} {
		if req.value == "" {
			return newError("Validate", req.field, ErrInvalidDefinition,
				"%s: required DRAFT_CONTRACT field is empty", d.Ref)
		}
	}
	if !d.Family.Valid() {
		return newError("Validate", "kernel_family", ErrInvalidDefinition,
			"%s declares %s, which is outside CHANGE_REQUEST|CALCULATION_REQUEST|ANALYTICAL_REQUEST",
			d.Ref, d.Family)
	}
	if !d.Maturity.Valid() {
		return newError("Validate", "maturity", ErrInvalidDefinition,
			"%s declares no maturity", d.Ref)
	}
	if !d.Maturity.InCatalog() {
		return newError("Validate", "maturity", ErrInvalidDefinition,
			"%s is below DRAFT_CONTRACT and is therefore not in the catalog", d.Ref)
	}
	if !d.SideEffect.Valid() {
		return newError("Validate", "side_effect_profile", ErrInvalidDefinition,
			"%s declares no side-effect profile", d.Ref)
	}
	if err := d.validateFamilyEffectPairing(); err != nil {
		return err
	}
	if err := d.InputSchema.Validate(); err != nil {
		return newError("Validate", "input_schema_ref", ErrInvalidDefinition,
			"%s: %v", d.Ref, err)
	}
	if err := d.ResultSchema.Validate(); err != nil {
		return newError("Validate", "result_schema_ref", ErrInvalidDefinition,
			"%s: %v", d.Ref, err)
	}
	if len(d.AllowedInitiators) == 0 {
		return newError("Validate", "allowed_initiators", ErrInvalidDefinition,
			"%s allows no initiator", d.Ref)
	}
	for _, i := range d.AllowedInitiators {
		if !i.Valid() {
			return newError("Validate", "allowed_initiators", ErrInvalidDefinition,
				"%s allows an unspecified initiator", d.Ref)
		}
	}
	if len(d.AllowedModes) == 0 {
		return newError("Validate", "allowed_execution_modes", ErrInvalidDefinition,
			"%s allows no execution mode", d.Ref)
	}
	for _, m := range d.AllowedModes {
		if !m.Valid() {
			return newError("Validate", "allowed_execution_modes", ErrInvalidDefinition,
				"%s allows an unspecified execution mode", d.Ref)
		}
	}
	if len(d.SubjectKinds) == 0 {
		return newError("Validate", "subject_kinds", ErrInvalidDefinition,
			"%s names no subject kind", d.Ref)
	}
	if err := d.validateRelease(); err != nil {
		return err
	}
	if d.SideEffect.Irreversible() && d.CorrectionRule == "" {
		return newError("Validate", "correction_rule", ErrInvalidDefinition,
			"%s declares an irreversible external effect without correction semantics", d.Ref)
	}
	if d.NegativeStatePolicyRef == "" && len(d.ApplicableNegativeStates) > 0 {
		return newError("Validate", "negative_state_policy_ref", ErrMissingNegativeStatePolicy,
			"%s declares applicable negative states without a policy", d.Ref)
	}
	for _, in := range d.RequiredInputs {
		if in.Path == "" {
			return newError("Validate", "required_inputs", ErrInvalidDefinition,
				"%s declares an input with no schema path", d.Ref)
		}
		if !in.Kind.Valid() {
			return newError("Validate", "required_inputs", ErrInvalidDefinition,
				"%s declares input %q with no kind", d.Ref, in.Path)
		}
	}
	return nil
}

// validateFamilyEffectPairing enforces the two rules a family/effect pair must
// satisfy, and the population-scope placement rule.
func (d Definition) validateFamilyEffectPairing() error {
	switch d.Family {
	case FamilyCalculationRequest:
		if d.SideEffect != SideEffectPure {
			return newError("Validate", "side_effect_profile", ErrInvalidDefinition,
				"%s is a CALCULATION_REQUEST and must be PURE, not %s", d.Ref, d.SideEffect)
		}
	case FamilyAnalyticalRequest:
		if d.SideEffect != SideEffectReadOnly {
			return newError("Validate", "side_effect_profile", ErrInvalidDefinition,
				"%s is an ANALYTICAL_REQUEST and must be READ_ONLY, not %s", d.Ref, d.SideEffect)
		}
	case FamilyChangeRequest:
		if !d.SideEffect.Mutates() {
			return newError("Validate", "side_effect_profile", ErrInvalidDefinition,
				"%s is a CHANGE_REQUEST and must declare a mutation profile, not %s",
				d.Ref, d.SideEffect)
		}
	}
	if d.PopulationScope != nil {
		if d.Family != FamilyChangeRequest {
			return newError("Validate", "population_scope", ErrInvalidDefinition,
				"%s declares a population scope but is a %s; population scope is a "+
					"CHANGE_REQUEST attribute", d.Ref, d.Family)
		}
		if err := d.PopulationScope.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// validateRelease enforces the P1A effect ceiling. P1A is read-only observe,
// preflight and simulate: a P1A definition may not carry a non-zero effect
// class and may not allow EXECUTE, whatever its permanent side-effect profile
// says about later releases.
func (d Definition) validateRelease() error {
	if !d.Release.Valid() {
		return newError("Validate", "release", ErrInvalidDefinition,
			"%s is scheduled for no release", d.Ref)
	}
	if !d.EffectClass.Valid() {
		return newError("Validate", "effect_class", ErrInvalidDefinition,
			"%s declares no effect class", d.Ref)
	}
	if d.Release == ReleaseP1A || d.Release == ReleaseConformance {
		if d.EffectClass != EffectClassZero {
			return newError("Validate", "effect_class", ErrInvalidDefinition,
				"%s is scheduled for %s and must be ZERO_EFFECT, not %s",
				d.Ref, d.Release, d.EffectClass)
		}
		// EXECUTE on a read-only or pure definition means "run the read or the
		// calculation" and is harmless. EXECUTE on a definition whose declared
		// profile mutates is what P1A forbids: PromoteWorker is DRAFT,
		// PREFLIGHT and SIMULATE only until a signed Gate A PROCEED.
		if d.SideEffect.Mutates() && d.AllowsMode(ModeExecute) {
			return newError("Validate", "allowed_execution_modes", ErrModeNotAllowed,
				"%s declares %s and is scheduled for %s; it must not allow EXECUTE",
				d.Ref, d.SideEffect, d.Release)
		}
	}
	if d.EffectClass != EffectClassZero && !d.SideEffect.Mutates() {
		return newError("Validate", "effect_class", ErrInvalidDefinition,
			"%s declares effect class %s but a non-mutating side-effect profile %s",
			d.Ref, d.EffectClass, d.SideEffect)
	}
	return nil
}

// LifecycleProfiles returns the lifecycle profiles that apply to this
// definition: the kernel profiles, narrowed by AllowedTransitions where the
// definition declares a narrower set.
func (d Definition) LifecycleProfiles() map[lifecycle.Dimension]lifecycle.Profile {
	profiles := lifecycle.KernelProfiles()
	for dim, rules := range d.AllowedTransitions {
		base, ok := profiles[dim]
		if !ok {
			continue
		}
		narrowed := base
		narrowed.ID = base.ID + "#" + d.Ref.String()
		narrowed.Transitions = append([]lifecycle.TransitionRule(nil), rules...)
		profiles[dim] = narrowed
	}
	return profiles
}
