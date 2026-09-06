package intent

import (
	"fmt"
	"sort"
	"strings"
)

// Environment is the plane an intent runs against. It is a separate axis from
// [Mode] because the two answer different questions - "what is this run trying
// to do" and "which world is it doing it in" - and the effect guarantees come
// from the pair, never from either alone. EXECUTE in a test environment is not
// EXECUTE in production, and SIMULATE in production is still zero-effect.
type Environment uint8

// Environment values.
const (
	EnvironmentUnspecified Environment = iota
	// EnvironmentProduction is the tenant's real world.
	EnvironmentProduction
	// EnvironmentSandbox is a tenant-facing copy with its own domain truth and
	// no path to a real external destination.
	EnvironmentSandbox
	// EnvironmentTest is an automated test plane: pinned clock, no domain
	// commit, no external effect.
	EnvironmentTest
	// EnvironmentConformance is the conformance-suite plane. It is
	// deliberately distinct from TEST so that a conformance receipt can never
	// be confused with an ordinary test run.
	EnvironmentConformance
)

var environmentNames = map[Environment]string{
	EnvironmentUnspecified: "UNSPECIFIED",
	EnvironmentProduction:  "PRODUCTION",
	EnvironmentSandbox:     "SANDBOX",
	EnvironmentTest:        "TEST",
	EnvironmentConformance: "CONFORMANCE",
}

func (e Environment) String() string { return enumName(environmentNames, e, "Environment") }

// Valid reports whether e is a declared environment other than UNSPECIFIED.
func (e Environment) Valid() bool {
	_, ok := environmentNames[e]
	return ok && e != EnvironmentUnspecified
}

// Environments returns the four declared environments.
func Environments() []Environment {
	return []Environment{
		EnvironmentProduction, EnvironmentSandbox, EnvironmentTest, EnvironmentConformance,
	}
}

// Modes returns the five declared execution modes.
func Modes() []Mode {
	return []Mode{ModeSimulate, ModeExecute, ModeReplay, ModeRepair, ModeShadow}
}

// AdapterProfile is the adapter set a run is bound to. It is part of the
// contract rather than a composition detail because it is the mechanism that
// makes the zero-effect guarantee true rather than merely intended: a
// zero-effect contract is bound to adapters that cannot reach a destination,
// so a code path that forgets to check the mode still sends nothing.
type AdapterProfile uint8

// AdapterProfile values.
const (
	AdapterUnspecified AdapterProfile = iota
	// AdapterLive reaches real destinations.
	AdapterLive
	// AdapterRecording answers from live reads and records what an effect
	// would have been, without performing it.
	AdapterRecording
	// AdapterNull answers from recorded material only and reaches nothing.
	AdapterNull
)

var adapterNames = map[AdapterProfile]string{
	AdapterUnspecified: "UNSPECIFIED",
	AdapterLive:        "LIVE",
	AdapterRecording:   "RECORDING",
	AdapterNull:        "NULL",
}

func (a AdapterProfile) String() string { return enumName(adapterNames, a, "AdapterProfile") }

// ClockSource is where a run reads time from.
type ClockSource uint8

// ClockSource values.
const (
	ClockUnspecified ClockSource = iota
	// ClockWall is the real clock.
	ClockWall
	// ClockPinned is a fixed instant supplied by the harness.
	ClockPinned
	// ClockHistorical is the recorded time of the run being replayed.
	ClockHistorical
)

var clockSourceNames = map[ClockSource]string{
	ClockUnspecified: "UNSPECIFIED",
	ClockWall:        "WALL",
	ClockPinned:      "PINNED",
	ClockHistorical:  "HISTORICAL",
}

func (c ClockSource) String() string { return enumName(clockSourceNames, c, "ClockSource") }

// Attempt is something a run may try to do. It is what [ModeContract.Permit]
// decides on, so that "may I write this" is one typed question with one answer
// rather than a mode comparison repeated at every call site.
type Attempt uint8

// Attempt values.
const (
	AttemptUnspecified Attempt = iota
	// AttemptGovernedRead is a governed read. Every mode permits it.
	AttemptGovernedRead
	// AttemptDomainCommit writes domain truth.
	AttemptDomainCommit
	// AttemptExternalEffect sends a message, writes a file, moves money or
	// calls a webhook.
	AttemptExternalEffect
	// AttemptApprovalConsumption consumes a live human approval.
	AttemptApprovalConsumption
)

var attemptNames = map[Attempt]string{
	AttemptUnspecified:         "UNSPECIFIED",
	AttemptGovernedRead:        "GOVERNED_READ",
	AttemptDomainCommit:        "DOMAIN_COMMIT",
	AttemptExternalEffect:      "EXTERNAL_EFFECT",
	AttemptApprovalConsumption: "APPROVAL_CONSUMPTION",
}

func (a Attempt) String() string { return enumName(attemptNames, a, "Attempt") }

// Attempts returns the four declared attempts.
func Attempts() []Attempt {
	return []Attempt{
		AttemptGovernedRead, AttemptDomainCommit, AttemptExternalEffect,
		AttemptApprovalConsumption,
	}
}

// ModeContract is the immutable binding of one execution mode in one
// environment: what it may do, which adapters and clock it is bound to, the
// evidence namespace its receipts land in, and whether it needs an
// authorization of its own on top of the intent's.
//
// It is a value with no constructor other than [ModeContractFor], and
// [ModeContractFor] reads a fixed table. There is no per-definition, per-tenant
// or per-caller override: an escalation would have to be a change to that
// table, reviewed as one.
type ModeContract struct {
	Mode        Mode
	Environment Environment

	AllowsDomainCommit        bool
	AllowsExternalEffect      bool
	AllowsApprovalConsumption bool

	// RequiresSeparateAuthorization says this contract is not reachable from
	// the intent's own authorization alone: EXECUTE and REPAIR each need an
	// authority decision of their own, taken at the moment of the run.
	RequiresSeparateAuthorization bool

	// RequiresHistoricalCausation says the run must name the historical intent
	// it is re-deriving. It is what separates a replay from a new action that
	// happens to carry the same request.
	RequiresHistoricalCausation bool

	Adapters      AdapterProfile
	Clock         ClockSource
	EffectCeiling EffectClass

	// EvidenceNamespace keeps each mode's receipts in their own namespace, so
	// a simulation receipt can never be read back as an execution receipt.
	EvidenceNamespace string
}

// ZeroEffect reports whether the contract guarantees no domain mutation and no
// external effect.
func (c ModeContract) ZeroEffect() bool {
	return !c.AllowsDomainCommit && !c.AllowsExternalEffect
}

// modeContracts is the fixed table. Every pair of a declared mode and a
// declared environment has exactly one row, and the only rows that commit
// domain truth or cause an external effect are the ones named here.
var modeContracts = func() map[Mode]map[Environment]ModeContract {
	table := map[Mode]map[Environment]ModeContract{}
	for _, mode := range Modes() {
		table[mode] = map[Environment]ModeContract{}
		for _, env := range Environments() {
			table[mode][env] = contractFor(mode, env)
		}
	}
	return table
}()

// contractFor derives one row. It is written as rules rather than as twenty
// literals so that the reason for each row is visible: production is the only
// environment where anything reaches the real world, and only EXECUTE and
// REPAIR reach it there.
func contractFor(mode Mode, env Environment) ModeContract {
	c := ModeContract{
		Mode:              mode,
		Environment:       env,
		Adapters:          AdapterNull,
		Clock:             ClockWall,
		EffectCeiling:     EffectClassZero,
		EvidenceNamespace: strings.ToLower(env.String() + "." + mode.String()),
	}
	switch mode {
	case ModeSimulate, ModeShadow:
		// Simulation and shadow read live truth and record what they would
		// have done. They never commit and never send, in any environment.
		if env == EnvironmentProduction || env == EnvironmentSandbox {
			c.Adapters = AdapterRecording
		}
	case ModeReplay:
		// Replay re-derives a historical run from recorded material. It reaches
		// nothing at all and reads the historical clock, which is why a replay
		// of a run that sent a message does not send it again.
		c.Clock = ClockHistorical
		c.RequiresHistoricalCausation = true
	case ModeExecute:
		c.RequiresSeparateAuthorization = true
		switch env {
		case EnvironmentProduction:
			c.AllowsDomainCommit = true
			c.AllowsExternalEffect = true
			c.AllowsApprovalConsumption = true
			c.Adapters = AdapterLive
			c.EffectCeiling = EffectClassIrreversibleExternal
		case EnvironmentSandbox:
			// A sandbox has domain truth of its own, so EXECUTE commits to it.
			// It has no path to a real destination, so no external effect and
			// no live approval: an approval consumed in a sandbox would be a
			// real person's real decision spent on a rehearsal.
			c.AllowsDomainCommit = true
			c.Adapters = AdapterRecording
			c.EffectCeiling = EffectClassInternalMutation
		}
	case ModeRepair:
		// Repair fixes consistency. It may write and it may send a
		// compensating effect, and it never consumes a business approval:
		// repairing an inconsistency is not the same decision a human approved.
		c.RequiresSeparateAuthorization = true
		if env == EnvironmentProduction {
			c.AllowsDomainCommit = true
			c.AllowsExternalEffect = true
			c.Adapters = AdapterLive
			c.EffectCeiling = EffectClassExternalMutation
		}
	}
	if env == EnvironmentTest || env == EnvironmentConformance {
		c.Clock = ClockPinned
	}
	return c
}

// ModeContractFor returns the contract for one mode in one environment.
func ModeContractFor(mode Mode, env Environment) (ModeContract, error) {
	if !mode.Valid() {
		return ModeContract{}, newError("ModeContractFor", "execution_mode", ErrInvalidModeContract,
			"%s is not a declared execution mode", mode)
	}
	if !env.Valid() {
		return ModeContract{}, newError("ModeContractFor", "environment", ErrInvalidModeContract,
			"%s is not a declared environment", env)
	}
	return modeContracts[mode][env], nil
}

// ModeContracts returns every row of the table, ordered by mode then
// environment. The order is fixed so that a golden vector of the matrix is
// stable.
func ModeContracts() []ModeContract {
	out := make([]ModeContract, 0, len(Modes())*len(Environments()))
	for _, mode := range Modes() {
		for _, env := range Environments() {
			out = append(out, modeContracts[mode][env])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Mode != out[j].Mode {
			return out[i].Mode < out[j].Mode
		}
		return out[i].Environment < out[j].Environment
	})
	return out
}

// EffectiveCeiling is the lower of the contract's ceiling and the definition's
// own effect class. A definition that is zero-effect in its scheduled release
// stays zero-effect in production EXECUTE; a mode that guarantees zero effect
// holds a definition that mutates to zero.
func (c ModeContract) EffectiveCeiling(def Definition) EffectClass {
	if def.EffectClass < c.EffectCeiling {
		return def.EffectClass
	}
	return c.EffectCeiling
}

// Permit decides one attempt under this contract and this definition.
//
// Both halves matter and neither substitutes for the other: the contract says
// what the mode may ever do, and the definition's effect class says what this
// intent may do in the release it is scheduled for. An attempt is permitted
// only when both allow it.
func (c ModeContract) Permit(def Definition, a Attempt) error {
	if !def.AllowsMode(c.Mode) {
		return newError("Permit", "execution_mode", ErrModeNotAllowed,
			"%s does not allow %s", def.Ref, c.Mode)
	}
	ceiling := c.EffectiveCeiling(def)
	switch a {
	case AttemptGovernedRead:
		return nil
	case AttemptDomainCommit:
		if !c.AllowsDomainCommit {
			return newError("Permit", "attempt", ErrEffectEscalation,
				"%s in %s guarantees no domain commit", c.Mode, c.Environment)
		}
		if ceiling == EffectClassZero {
			return newError("Permit", "attempt", ErrEffectEscalation,
				"%s is %s in its scheduled release and may not commit under %s",
				def.Ref, def.EffectClass, c.Mode)
		}
		return nil
	case AttemptExternalEffect:
		if !c.AllowsExternalEffect {
			return newError("Permit", "attempt", ErrEffectEscalation,
				"%s in %s guarantees no external effect", c.Mode, c.Environment)
		}
		if ceiling < EffectClassExternalMutation {
			return newError("Permit", "attempt", ErrEffectEscalation,
				"%s has effect ceiling %s and may not cause an external effect", def.Ref, ceiling)
		}
		return nil
	case AttemptApprovalConsumption:
		if !c.AllowsApprovalConsumption {
			return newError("Permit", "attempt", ErrEffectEscalation,
				"%s in %s may not consume a live approval", c.Mode, c.Environment)
		}
		return nil
	default:
		return newError("Permit", "attempt", ErrInvalidModeContract,
			"%s is not a declared attempt", a)
	}
}

// CausalSeparation checks that an instance's identity matches its mode.
//
// A replay must name the historical intent it re-derives and must not be that
// intent, so a replay and the original are two records with one causal link. A
// new action must not claim to be its own cause. Without this, the same request
// run twice - once as history, once for real - is indistinguishable afterwards.
func CausalSeparation(c ModeContract, inst Instance) error {
	if inst.ExecutionMode != c.Mode {
		return newError("CausalSeparation", "execution_mode", ErrCausalSeparation,
			"instance declares %s and is being run under %s", inst.ExecutionMode, c.Mode)
	}
	if inst.IntentID == "" {
		return newError("CausalSeparation", "intent_id", ErrCausalSeparation,
			"instance has no identity of its own")
	}
	if c.RequiresHistoricalCausation {
		if inst.CausationID == nil || *inst.CausationID == "" {
			return newError("CausalSeparation", "causation_id", ErrCausalSeparation,
				"%s names no historical intent to re-derive", c.Mode)
		}
		if *inst.CausationID == inst.IntentID {
			return newError("CausalSeparation", "causation_id", ErrCausalSeparation,
				"%s names itself as the run it re-derives", c.Mode)
		}
		return nil
	}
	if inst.CausationID != nil && *inst.CausationID == inst.IntentID {
		return newError("CausalSeparation", "causation_id", ErrCausalSeparation,
			"a new action names itself as its own cause")
	}
	return nil
}

// ModeDifference is one semantic difference between two mode contracts.
type ModeDifference struct {
	Field string
	Left  string
	Right string
}

func (d ModeDifference) String() string {
	return d.Field + ": " + d.Left + " vs " + d.Right
}

// CompareContracts reports the semantic differences between two contracts, in a
// fixed field order.
//
// It exists so that "what would change if this ran for real" is a deterministic
// answer rather than a reading of two structs side by side: a simulation and
// the execution it stands in for differ in exactly these ways and no others.
func CompareContracts(left, right ModeContract) []ModeDifference {
	var out []ModeDifference
	add := func(field string, l, r any) {
		ls, rs := fmt.Sprint(l), fmt.Sprint(r)
		if ls != rs {
			out = append(out, ModeDifference{Field: field, Left: ls, Right: rs})
		}
	}
	add("mode", left.Mode, right.Mode)
	add("environment", left.Environment, right.Environment)
	add("allows_domain_commit", left.AllowsDomainCommit, right.AllowsDomainCommit)
	add("allows_external_effect", left.AllowsExternalEffect, right.AllowsExternalEffect)
	add("allows_approval_consumption", left.AllowsApprovalConsumption, right.AllowsApprovalConsumption)
	add("requires_separate_authorization", left.RequiresSeparateAuthorization, right.RequiresSeparateAuthorization)
	add("requires_historical_causation", left.RequiresHistoricalCausation, right.RequiresHistoricalCausation)
	add("adapters", left.Adapters, right.Adapters)
	add("clock", left.Clock, right.Clock)
	add("effect_ceiling", left.EffectCeiling, right.EffectCeiling)
	add("evidence_namespace", left.EvidenceNamespace, right.EvidenceNamespace)
	return out
}
